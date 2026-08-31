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
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return fmt.Errorf("usage: wf round close <finding#> <commit> [-round N] [-replace]")
	}
	num, sha := pos[0], strings.TrimSpace(pos[1])
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
	if !w.Git.CommitExists(sha) {
		return fmt.Errorf("%w; nothing was written", closedByError(w, fmt.Sprintf("round %d finding %s", n, num), sha))
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
		if cur == sha {
			return fmt.Errorf("round %d finding %s is already closed by %s and -replace names the same commit; nothing to do", n, num, sha)
		}
	}
	lines := strings.Split(string(b), "\n")
	if r.Line < 1 || r.Line > len(lines) {
		return fmt.Errorf("round %d finding %s reports line %d, outside a file of %d lines", n, num, r.Line, len(lines))
	}
	out, ok := ws.SetClosedBy(lines[r.Line-1], sha)
	if !ok {
		return fmt.Errorf("round %d line %d does not carry four unescaped pipes; a row the parser cannot split cannot be edited either — escape a literal pipe as \\|", n, r.Line)
	}
	lines[r.Line-1] = out
	body := []byte(strings.Join(lines, "\n"))
	// Re-parsed from the BYTES that are about to be written, never from the
	// row struct that produced them: this is the same read-back-and-compare
	// `wf apply` does after a write, one layer up. Without it a rewrite that
	// lands the sha in the wrong cell is reported as the closure it was asked
	// for.
	if err := verifyClosureLanded(body, r.Line, num, sha, r.Text); err != nil {
		return err
	}
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		return err
	}
	rel, _ := filepath.Rel(w.Root, dst)
	commit, err := w.Git.Commit(fmt.Sprintf("round %d: finding %s closed by %s", n, num, short(sha)), []string{rel})
	if err != nil {
		return err
	}
	fmt.Printf("round %d finding %s: closed by %s\n  %s\ncommitted %s\n", n, num, sha, trunc(r.Text, 90), short(commit))
	// The check that reads this cell, named where the cell was just filled.
	// `wf round record` prints the same class of pointer for the same reason:
	// a closure is a claim, and the command that tests it is one line away.
	fmt.Printf("  `wf check closure -round %d` asks whether that commit touched what the finding names\n", n)
	return nil
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
