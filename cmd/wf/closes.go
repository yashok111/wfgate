package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"git.sumka.site/yakov/wfgate/internal/ws"
)

// ledgerClosure is a validated, NOT YET WRITTEN closure of some rows of one
// round's ledger. `wf round close` plans one row; `wf apply -closes` plans
// every row the batch closes and writes them in ONE ledger commit after the
// batch's own.
//
// Why the batch path exists at all: measured on A4, 70 of the workspace's 143
// commits are `round N: finding K closed by …` — one command per row, each
// preceded by the operator reading `HEAD` to learn the sha the previous
// command had just printed. EIGHT of one round's closures named the ledger
// commit of the PREVIOUS `wf round close` that way, which is the defect
// `commitTouchedAnArtifact` now refuses after the fact. A closure written by
// the command that made the commit cannot name the wrong one: there is no sha
// to read, and nothing to feed forward.
type ledgerClosure struct {
	n      int
	dst    string
	rel    string
	before []byte
	rows   []ws.FindingRow // in the order the numbers were given
}

// planClosure validates finding numbers against the ledger of round n BEFORE
// any byte is written: the ledger reads, every number names exactly ONE row
// that is not the scaffold's empty form, and — unless replace — none of them is
// already closed. The messages are the ones `wf round close` has always
// printed; both commands go through here so the two cannot disagree about the
// same row.
func planClosure(w *ws.Workspace, n int, nums []string, replace bool) (*ledgerClosure, error) {
	if n == 0 {
		return nil, fmt.Errorf("no round is open; run `wf round new` first")
	}
	dst := filepath.Join(w.RoundDir(n), "findings.md")
	b, err := os.ReadFile(dst)
	if err != nil {
		return nil, err
	}
	rows := ws.ParseFindings(b)
	seen := map[string]bool{}
	p := &ledgerClosure{n: n, dst: dst, before: b}
	p.rel, _ = filepath.Rel(w.Root, dst)
	for _, num := range nums {
		// Both callers have already stripped `#` and whitespace; a second
		// strip here survived every mutation and is not repeated. What CAN
		// arrive empty is `wf round close "#" <sha>`.
		if num == "" {
			return nil, fmt.Errorf("an empty finding number; write one the ledger carries (it has: %s)", strings.Join(findingNums(rows), ", "))
		}
		if seen[num] {
			return nil, fmt.Errorf("finding %s is named twice; one closure per row", num)
		}
		seen[num] = true
		var hit []ws.FindingRow
		for _, r := range rows {
			// No `!r.Malformed` test: a malformed row carries no Num at all,
			// so it never equals one. The conjunct survived every mutation.
			if strings.TrimSpace(r.Num) == num {
				hit = append(hit, r)
			}
		}
		if len(hit) == 0 {
			// Naming what IS there, because the usual cause is the wrong
			// round: a finding number is unique inside a ledger and
			// meaningless across two.
			return nil, fmt.Errorf("round %d has no finding %s (it has: %s); `-round N` picks another round's ledger", n, num, strings.Join(findingNums(rows), ", "))
		}
		if len(hit) > 1 {
			return nil, fmt.Errorf("round %d has %d rows numbered %s (lines %s) — a duplicate number makes the closure ambiguous and nothing was written", n, len(hit), num, strings.Join(rowLines(hit), ", "))
		}
		r := hit[0]
		if r.Placeholder() {
			// The scaffold's empty row is a form. Closing it would record a
			// commit against a finding nobody has written down.
			return nil, fmt.Errorf("round %d finding %s has no finding text — it is the scaffold's empty row, not a claim; record the findings first with `wf round record`", n, num)
		}
		if cur := strings.TrimSpace(r.ClosedBy); cur != "" && !replace {
			return nil, fmt.Errorf("round %d finding %s is already closed by %q — `wf round close %s <commit> -replace` overwrites it, and say in the round title why the first closure did not hold", n, num, cur, num)
		}
		p.rows = append(p.rows, r)
	}
	return p, nil
}

// nums lists the planned rows' numbers, for messages and subjects.
func (p *ledgerClosure) nums() []string {
	out := make([]string, 0, len(p.rows))
	for _, r := range p.rows {
		out = append(out, strings.TrimSpace(r.Num))
	}
	return out
}

// write fills every planned row's closure cell with value, re-parses the bytes
// it is about to write, writes them, and commits under subject. A commit that
// fails rolls the ledger back and says so — the `wf round close` lesson: a
// ledger left rewritten and staged under a bare `exit status 1` had no way
// back in except the hand `git commit` the command exists to remove.
func (p *ledgerClosure) write(w *ws.Workspace, value, subject string) (string, error) {
	lines := strings.Split(string(p.before), "\n")
	for _, r := range p.rows {
		// No bounds check on r.Line: `ParseFindings` numbers its rows over the
		// same split this indexes, so it is in range by construction.
		out, ok := ws.SetClosedBy(lines[r.Line-1], value)
		if !ok {
			return "", fmt.Errorf("round %d line %d does not carry four unescaped pipes; a row the parser cannot split cannot be edited either — escape a literal pipe as \\|, and a backtick code span is NOT an escape", p.n, r.Line)
		}
		lines[r.Line-1] = out
	}
	body := []byte(strings.Join(lines, "\n"))
	// Re-parsed from the BYTES about to be written, never from the row structs
	// that produced them: the same read-back-and-compare `wf apply` does after
	// a write, one layer up. UNKILLABLE by mutation of this call: it pins the
	// WRITER (`ws.SetClosedBy`), which no test of the guard can make wrong;
	// `verifyClosureLanded` itself is pinned directly, and the wiring is
	// proven the day the writer regresses and not before.
	for _, r := range p.rows {
		if err := verifyClosureLanded(body, r.Line, strings.TrimSpace(r.Num), value, r.Text); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(p.dst, body, 0o644); err != nil {
		return "", err
	}
	commit, err := w.Git.Commit(subject, []string{p.rel})
	if err != nil {
		rolled := "the ledger edit has been rolled back and nothing is staged"
		if werr := os.WriteFile(p.dst, p.before, 0o644); werr != nil {
			rolled = fmt.Sprintf("AND THE ROLLBACK FAILED (%v) — %s still holds the edit; restore it with `git checkout -- %s`", werr, p.rel, p.rel)
		} else if _, rerr := w.Git.Run("reset", "-q", "HEAD", "--", p.rel); rerr != nil {
			rolled = fmt.Sprintf("the ledger edit has been rolled back, but it is still STAGED (%v) — unstage it with `git reset HEAD -- %s`", rerr, p.rel)
		}
		return "", fmt.Errorf("round %d finding(s) %s: the ledger was written and the commit failed (%v) — %s", p.n, strings.Join(p.nums(), ", "), err, rolled)
	}
	return commit, nil
}

// parseCloses reads the `-closes` list: commas or spaces, a leading `#`
// tolerated because that is what the report prints and what an operator
// copies. `1,,2` is `1,2` — FieldsFunc drops the empty item, and a refusal for
// it survived every mutation, so it is not here. Validation of the numbers
// against the ledger is planClosure's.
func parseCloses(s string) ([]string, error) {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		if f = strings.TrimPrefix(f, "#"); f != "" {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("-closes was passed with nothing in it; name the finding numbers this batch closes, or drop the flag")
	}
	return out, nil
}

// closeWithBatch fills the planned rows with the batch's own commit and prints
// what `wf round close` prints, once for the set. Called after the batch is
// committed; the sha is the one thing the operator can no longer get wrong.
func closeWithBatch(w *ws.Workspace, p *ledgerClosure, sha string) error {
	value, err := canonicalSha(w, sha)
	if err != nil {
		// The batch is already committed; the message must say so, or the
		// operator re-applies a batch whose edits no longer match anything.
		return fmt.Errorf("the batch is committed as %s and stands; %w — close the rows by hand with `wf round close <finding#> %s`", short(sha), err, short(sha))
	}
	subject := fmt.Sprintf("round %d: finding(s) %s closed by %s", p.n, strings.Join(p.nums(), ", "), short(sha))
	commit, err := p.write(w, value, subject)
	if err != nil {
		return fmt.Errorf("the batch is committed as %s and stands; %w — close the rows by hand with `wf round close <finding#> %s`", short(sha), err, short(sha))
	}
	fmt.Printf("closed finding(s) %s in round %d by %s  (ledger committed %s)\n", strings.Join(p.nums(), ", "), p.n, value, short(commit))
	// The check that reads these cells, named where they were just filled.
	// This commit touched artifacts by construction, so the refusal `wf round
	// close` makes about bookkeeping commits has nothing to test here; what
	// it cannot know is whether the batch reached what EACH finding names.
	fmt.Printf("  `wf check closure -round %d` asks whether that commit touched what each finding names\n", p.n)
	return nil
}
