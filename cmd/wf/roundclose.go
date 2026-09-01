package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"git.sumka.site/yakov/wfgate/internal/ws"
)

// cmdRoundClose fills one finding's closure cell with the commit that closed it.
//
// It is the only bookkeeping step of the round loop that had no command behind
// it, and the gap had a cost: measured on the first gate run against a target
// other than mocker, the ledger was corrupted TWICE by the operator's own
// `awk -F'|'` doing this by hand. The findings table requires a literal pipe
// inside a cell to be written `\|` — the escape exists because an unescaped one
// once scored an open BLOCKER as "closed by prose" and froze the round — and
// every field-splitting tool splits on the escape too, writing the commit id
// into the middle of a finding's text. `ws.SetClosedBy` never splits the row.
//
// The sha is validated BEFORE any byte is written, the same order `wf apply`
// and `wf round record` use: a landed lie briefs the next round with fiction,
// and a rejected one announces itself.
func cmdRoundClose(args []string) error {
	fs := flag.NewFlagSet("round close", flag.ExitOnError)
	round := fs.Int("round", 0, "the round whose ledger to edit (default: the open one)")
	// Overwriting a closure is a real event — a finding re-opened and closed
	// again by a second commit — and it is also what a mistyped finding number
	// looks like. It gets a flag rather than a silent overwrite, and the
	// refusal quotes what is already there so the operator can tell the two
	// apart without opening the file.
	replace := fs.Bool("replace", false, "overwrite a closure cell that already names something")
	// A NOTE row asserts no failure and so has nothing a commit could close,
	// and `wf round record` has always reported `closed by prose` as a
	// first-class category — so the harness knew about the closure this
	// command could not write. P4a's write-up reports three round-trips, each
	// ending in the hand ledger edit plus a re-record that this command exists
	// to remove, and each after a refusal about a commit that does not exist.
	// The ROUND NUMBERS it names are not reproducible — a refusal leaves no git
	// trace, and the workspace's hand-edit-plus-re-record pattern sits in other
	// rounds than the three it names — so the count is quoted and the
	// attribution is not.
	prose := fs.String("prose", "", "close the row by PROSE instead of a commit: no code changed, and every reader downstream sees a claim nothing can check")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	// PRESENCE, not emptiness. `-prose ""` read as "no prose" made
	// `wf round close 1 <sha> -prose ""` close by COMMIT in silence — the
	// operator asked for one door and got the other — and `-prose ""` alone
	// fell out as a usage line about a commit they had not typed. A flag that
	// was passed is a claim, and an empty one is a claim with nothing in it.
	byProse := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "prose" {
			byProse = true
		}
	})
	if byProse && strings.TrimSpace(*prose) == "" {
		return fmt.Errorf("`-prose` was passed with nothing in it: the cell would read as an OPEN row to `wf round record` and to the freeze, which is what it already says. Give the reason, or close with the commit")
	}
	switch {
	case byProse && len(pos) == 2:
		// Two claims for one cell. Which one the row would carry is a coin
		// flip, and the cell is what every later reader tests.
		return fmt.Errorf("`-prose` and a commit argument are exclusive: the cell holds ONE closure, and %q would silently lose to the other", pos[1])
	case byProse && len(pos) != 1:
		return fmt.Errorf("usage: wf round close <finding#> -prose \"why this row needed no commit\" [-round N] [-replace]")
	case !byProse && len(pos) != 2:
		return fmt.Errorf("usage: wf round close <finding#> <commit> [-round N] [-replace]\n       wf round close <finding#> -prose \"why this row needed no commit\" — for a row no commit closes")
	}
	num := pos[0]
	sha := ""
	if !byProse {
		sha = strings.TrimSpace(pos[1])
	}
	// `#3` is what the report prints and what an operator copies. Accepting it
	// costs one line; refusing it costs a round-trip that teaches nothing.
	num = strings.TrimPrefix(strings.TrimSpace(num), "#")
	w, err := open()
	if err != nil {
		return err
	}
	n := *round
	if n == 0 {
		n = w.M.Round
	}
	if n == 0 {
		return fmt.Errorf("no round is open; run `wf round new` first")
	}
	if *round == 0 {
		fmt.Printf("round %d (the open one; `-round N` closes in another)\n", n)
	}
	dst := filepath.Join(w.RoundDir(n), "findings.md")
	b, err := os.ReadFile(dst)
	if err != nil {
		return err
	}
	// The cell's value, from whichever of the two doors was used. Everything
	// below this point writes and verifies THIS, so the two closures cannot
	// drift into two write paths.
	value := ""
	if byProse {
		v, err := proseValue(*prose)
		if err != nil {
			return fmt.Errorf("round %d finding %s: %w; nothing was written", n, num, err)
		}
		value = v
	}
	if !byProse && !w.Git.CommitExists(sha) {
		return fmt.Errorf("%w; nothing was written", closedByError(w, fmt.Sprintf("round %d finding %s", n, num), sha))
	}
	// RESOLVED, not stored as typed. `wf round close 1 HEAD` was accepted, wrote
	// the literal `HEAD` into the cell, and every reader downstream then saw a
	// closure "by prose": `ws.FindingRow.Shas()` wants hex with a digit, so
	// `wf check closure` skipped the row entirely and `wf freeze` passed the
	// BLOCKER as prose-closed with a verify-by-hand note. The command whose
	// whole purpose is a checkable commit reference was quietly producing an
	// uncheckable one, and a symbolic ref is the natural thing to type.
	if !byProse {
		sha, err = canonicalSha(w, sha)
		if err != nil {
			return fmt.Errorf("round %d finding %s: %w; nothing was written", n, num, err)
		}
		value = sha
		if err := commitTouchedAnArtifact(w, sha); err != nil {
			return fmt.Errorf("round %d finding %s: %w; nothing was written", n, num, err)
		}
	}
	rows := ws.ParseFindings(b)
	var hit []ws.FindingRow
	for _, r := range rows {
		if !r.Malformed && strings.TrimSpace(r.Num) == num {
			hit = append(hit, r)
		}
	}
	if len(hit) == 0 {
		// Naming what IS there, because the usual cause is the wrong round: a
		// finding number is unique inside a ledger and meaningless across two.
		return fmt.Errorf("round %d has no finding %s (it has: %s); `-round N` picks another round's ledger", n, num, strings.Join(findingNums(rows), ", "))
	}
	if len(hit) > 1 {
		return fmt.Errorf("round %d has %d rows numbered %s (lines %s) — a duplicate number makes the closure ambiguous and nothing was written", n, len(hit), num, strings.Join(rowLines(hit), ", "))
	}
	r := hit[0]
	if r.Placeholder() {
		// The scaffold's empty row is a form. Closing it would record a
		// commit against a finding nobody has written down.
		return fmt.Errorf("round %d finding %s has no finding text — it is the scaffold's empty row, not a claim; record the findings first with `wf round record`", n, num)
	}
	if cur := strings.TrimSpace(r.ClosedBy); cur != "" {
		if !*replace {
			return fmt.Errorf("round %d finding %s is already closed by %q — pass -replace to overwrite it, and say in the round title why the first closure did not hold", n, num, cur)
		}
		// A -replace that names what is already there writes identical bytes,
		// and the commit then fails on `nothing staged` — a git-shaped error
		// for an operator-shaped mistake. `wf apply -whole` refuses an
		// unchanged file for the same reason.
		// Compared as COMMITS, not as strings: the cell may hold an
		// abbreviation, a different case or a full sha of the very commit
		// being named, and each of those would write bytes that differ while
		// the claim does not change.
		// The prose half compares the BYTES, because there is nothing else to
		// compare: two sentences that mean the same thing are two claims, and
		// only an identical one would write identical bytes and die on git's
		// `nothing staged` the way the commit half used to.
		// `!byProse` is a statement of intent and not a behavioural guard: under
		// prose `sha` is empty, so `sameCommit` would answer false anyway and no
		// mutation can make the condition fail. It stays because calling git to
		// compare a commit against the empty string is a question nobody asked.
		if cur == value || (!byProse && sameCommit(w, cur, sha)) {
			what := "names the same commit"
			if byProse {
				what = "names the same prose"
			}
			return fmt.Errorf("round %d finding %s is already closed by %s and -replace %s; nothing to do", n, num, cur, what)
		}
	}
	// No bounds check on r.Line: `ParseFindings` numbers its rows over the same
	// `strings.Split(b, "\n")` this indexes, so it is in range by construction.
	// The check that used to stand here survived every mutation, and this
	// project deletes such a guard rather than shipping it.
	lines := strings.Split(string(b), "\n")
	out, ok := ws.SetClosedBy(lines[r.Line-1], value)
	if !ok {
		return fmt.Errorf("round %d line %d does not carry four unescaped pipes; a row the parser cannot split cannot be edited either — escape a literal pipe as \\|, and a backtick code span is NOT an escape", n, r.Line)
	}
	lines[r.Line-1] = out
	body := []byte(strings.Join(lines, "\n"))
	// Re-parsed from the BYTES that are about to be written, never from the
	// row struct that produced them: this is the same read-back-and-compare
	// `wf apply` does after a write, one layer up. Without it a rewrite that
	// lands the sha in the wrong cell is reported as the closure it was asked
	// for.
	if err := verifyClosureLanded(body, r.Line, num, value, r.Text); err != nil {
		return err
	}
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		return err
	}
	rel, _ := filepath.Rel(w.Root, dst)
	subject := fmt.Sprintf("round %d: finding %s closed by prose", n, num)
	if !byProse {
		subject = fmt.Sprintf("round %d: finding %s closed by %s", n, num, short(sha))
	}
	commit, err := w.Git.Commit(subject, []string{rel})
	if err != nil {
		// All-or-nothing, like `wf apply`: a commit that fails — a hook, a
		// read-only object store — used to leave the ledger REWRITTEN and
		// STAGED under a bare `exit status 1`, and both ways back in then
		// refused (`already closed by …` without -replace, `names the same
		// commit` with it), so the only exit left was the hand `git commit`
		// this command exists to remove.
		// The rollback's own failures are REPORTED, not swallowed: an operator
		// told "rolled back" over a ledger that is still rewritten is worse off
		// than one told nothing.
		rolled := "the edit has been rolled back and nothing is staged"
		if werr := os.WriteFile(dst, b, 0o644); werr != nil {
			rolled = fmt.Sprintf("AND THE ROLLBACK FAILED (%v) — %s still holds the edit; restore it with `git checkout -- %s`", werr, rel, rel)
		} else if _, rerr := w.Git.Run("reset", "-q", "HEAD", "--", rel); rerr != nil {
			rolled = fmt.Sprintf("the edit has been rolled back, but it is still STAGED (%v) — unstage it with `git reset HEAD -- %s`", rerr, rel)
		}
		return fmt.Errorf("round %d finding %s: the ledger was written and the commit failed (%v) — %s", n, num, err, rolled)
	}
	fmt.Printf("round %d finding %s: closed by %s\n  %s\ncommitted %s\n", n, num, value, trunc(r.Text, 90), short(commit))
	if byProse {
		// What a prose closure IS, said where it is written and not left for
		// the freeze to say. `wf check closure` counts this row in the
		// population it is NOT about, and `wf freeze` passes it with a
		// verify-by-hand note: the row now rests on the sentence above and on
		// nothing else.
		fmt.Printf("  a PROSE closure is checked by no command: `wf check closure -round %d` counts it apart, and `wf freeze` passes it with a verify-by-hand note\n", n)
		if strings.EqualFold(strings.TrimSpace(r.Level), "BLOCKER") {
			// The one level where that matters most, named rather than left to
			// the reader of the freeze. A BLOCKER asserts that nothing
			// downstream catches the defect; closing it with a sentence says
			// the assertion was wrong, and the round title is where that
			// belongs.
			fmt.Printf("  this row is a BLOCKER: a blocker closed by prose is the claim that no commit was OWED, so say in the round title why\n")
		}
		return nil
	}
	// The check that reads this cell, named where the cell was just filled.
	// `wf round record` prints the same class of pointer for the same reason:
	// a closure is a claim, and the command that tests it is one line away.
	fmt.Printf("  `wf check closure -round %d` asks whether that commit touched what the finding names\n", n)
	return nil
}

// commitTouchedAnArtifact refuses a closure whose commit changed no artifact.
//
// A closure says a fix LANDED in the document or the script the finding is
// about, and a commit that touched only `rounds/` is bookkeeping — this
// command's own ledger commit, a freeze, a lens. Measured on the gate that
// found it: EIGHT of one round's nine closures named the ledger commit of the
// PREVIOUS `wf round close`, because the operator read `HEAD` after each call
// and fed it to the next. Every one of those rows passed `wf round record` (the
// sha exists), and `wf check closure` reported all eight as FILE misses after
// the round had shipped — the same evidence, a round too late, in a report the
// operator has to go and run.
//
// The refusal is not about the commit's SUBJECT: a message can say anything,
// and what makes the row uncheckable is the paths. A row no commit closes has
// its own door, and the message names it.
func commitTouchedAnArtifact(w *ws.Workspace, sha string) error {
	arts, err := w.ArtifactList()
	if err != nil {
		return err
	}
	if len(arts) == 0 {
		// No artifact exists to touch. Refusing here would refuse every
		// closure in a workspace whose artifacts were pruned or not yet
		// added, which is a check firing on an ABSENT population rather than
		// a failing one.
		return nil
	}
	_, files, err := w.Git.CommitBrief(sha)
	if err != nil {
		// The commit resolved a moment ago; a read that fails here is git
		// trouble, not an operator mistake, and refusing on it would block a
		// correct closure with a message about the wrong thing.
		return nil
	}
	// The DIRECTORY, not the current artifact list: an artifact the commit
	// touched may have been renamed or removed since, and a refusal computed
	// from today's tree would reject a closure that was right when it was
	// written.
	for _, f := range files {
		if strings.HasPrefix(filepath.ToSlash(strings.TrimSpace(f)), "artifacts/") {
			return nil
		}
	}
	touched := "no file at all"
	if len(files) > 0 {
		touched = strings.Join(files, ", ")
	}
	return fmt.Errorf("%s touched no artifact (it changed %s) — a closure names the commit that FIXED the artifact, and this one is bookkeeping. `wf apply` prints the sha of the batch it committed, and `git log --oneline -- artifacts/` lists them; for a row no commit closes, `-prose \"why\"`", sha, touched)
}

// proseValue validates and escapes a prose closure. The cell it is about to
// occupy is READ by three later commands, and each of them can be lied to by a
// shape this rejects.
func proseValue(s string) (string, error) {
	v := strings.TrimSpace(s)
	// A row is one line. A newline would end the row mid-cell and the rest
	// would land in the document as text nobody wrote — the corruption class
	// this whole command exists to remove, arriving through its new door.
	if strings.ContainsAny(v, "\n\r") {
		return "", fmt.Errorf("a prose closure is ONE line: the cell is a table row, and a newline ends it mid-cell")
	}
	// A sha-shaped token makes the cell a COMMIT claim to every reader
	// downstream — `ws.FindingRow.Shas()` is what they all use, so it is what
	// this asks, rather than a second detector. `wf freeze` would then demand
	// that commit exist and `wf check closure` would test what it touched,
	// both against a row whose author meant a sentence.
	if shas := (ws.FindingRow{ClosedBy: v}).Shas(); len(shas) > 0 {
		return "", fmt.Errorf("the prose contains %q, which every reader downstream reads as a COMMIT claim (a hex token of 7-40 chars with a digit) — close it with that commit instead, or reword", shas[0])
	}
	// The escape the ledger's own format requires. Written HERE rather than
	// refused: the operator typed a sentence, and a shell pipeline quoted
	// inside it is the ordinary reason a cell holds a pipe at all. The
	// read-back below compares the ESCAPED form, so a mis-escape cannot pass.
	//
	// Only UNESCAPED pipes: an operator who read the ledger's rule and typed
	// `\|` would otherwise get `\\|`, and the fix for following the
	// documentation would be to stop following it.
	var b strings.Builder
	esc := false
	for _, r := range v {
		switch {
		case esc:
			esc = false
		case r == '\\':
			esc = true
		case r == '|':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String(), nil
}

// verifyClosureLanded re-parses the edited ledger and asserts the closure
// landed in the closure cell of the row it was asked for, with the finding text
// untouched. The text comparison is the load-bearing half: the failure this
// command exists to prevent puts a commit id INSIDE the finding text, which a
// check that only looked at the closure cell would report as an empty closure
// rather than as corruption.
func verifyClosureLanded(body []byte, line int, num, sha, text string) error {
	for _, r := range ws.ParseFindings(body) {
		if r.Line != line {
			continue
		}
		if r.Malformed {
			return fmt.Errorf("the edited row (line %d) no longer splits into 4 cells — nothing was written", line)
		}
		if strings.TrimSpace(r.Num) != num || r.Text != text {
			return fmt.Errorf("the edited row (line %d) reads back as finding %q with different text — nothing was written", line, r.Num)
		}
		if strings.TrimSpace(r.ClosedBy) != sha {
			return fmt.Errorf("the edited row (line %d) reads back with closure %q, not %s — nothing was written", line, r.ClosedBy, sha)
		}
		return nil
	}
	return fmt.Errorf("the edited row (line %d) is no longer a table row — nothing was written", line)
}

func findingNums(rows []ws.FindingRow) []string {
	var out []string
	for _, r := range rows {
		if !r.Malformed && !r.Placeholder() {
			out = append(out, r.Num)
		}
	}
	if len(out) == 0 {
		return []string{"no filled rows"}
	}
	return out
}

func rowLines(rows []ws.FindingRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, fmt.Sprint(r.Line))
	}
	return out
}

// canonicalSha turns whatever the operator typed into the sha the ledger
// stores. A symbolic ref (`HEAD`, `gate-r3`, a branch) is a commit to git and
// is not a commit to `ws.FindingRow.Shas()`, which wants a hex token of 7 to 40
// characters carrying at least one digit — so an unresolved ref reads as a
// PROSE closure everywhere downstream.
//
// The short form is what `wf apply` prints and what an operator copies, so it
// is what goes in; the full form is the fallback for the ~4e-4 short sha with
// no digit, which the ledger's own detector would refuse for the same reason it
// refuses `deadbeef`.
func canonicalSha(w *ws.Workspace, ref string) (string, error) {
	full, err := w.Git.Run("rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%s names no commit here", ref)
	}
	full = strings.TrimSpace(full)
	if s := short(full); hasDigit(s) {
		return s, nil
	}
	return full, nil
}

func hasDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

// sameCommit reports whether two ledger cells name one commit. A cell that is
// not a resolvable ref at all (prose, a target sha) is not the same commit as
// anything: it is a different KIND of closure, and -replace is exactly the flag
// for turning one into the other.
func sameCommit(w *ws.Workspace, a, b string) bool {
	ra, err1 := w.Git.Run("rev-parse", "--verify", a+"^{commit}")
	rb, err2 := w.Git.Run("rev-parse", "--verify", b+"^{commit}")
	return err1 == nil && err2 == nil && strings.TrimSpace(ra) == strings.TrimSpace(rb)
}
