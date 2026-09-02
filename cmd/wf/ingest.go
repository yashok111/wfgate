package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"git.sumka.site/yakov/wfgate/internal/check"
	"git.sumka.site/yakov/wfgate/internal/ws"
)

// cmdRoundIngest appends the rows of each reviewer's LAST table to the round's
// ledger, numbered on from what the ledger holds, each tagged with the lens it
// came from, and commits it. The operator still dedups across lenses and
// decides each level, and `wf round record` is what banks the result.
//
// Measured on A4 from the operator session's own transcript: the reviewers of
// a round return inside 9-12 minutes and the round's next dispatch is 13-30
// minutes later, so between a third and a half of a round is the operator's
// — and the largest single block of it is re-typing three or four returned
// tables into the ledger by hand, on a gate whose brief already asks every lens
// for the ledger's own four columns. What the operator ADDS in that pass is
// judgement — the same defect filed by two lenses becomes one row, a level is
// re-derived under section 0 — and that half stays by hand. The transcription
// half does not need to.
//
// It writes into the round's OWN findings.md, which is the form `wf round new`
// scaffolded to be filled, and commits it the way a banked lens is committed:
// a ledger the freeze reads from the worktree must not sit uncommitted between
// two commands. Why not a registry: the ledger stays the one table it has always been, parsed and
// never generated — this command writes rows in the scaffold's own shape and
// nothing reads them back except the parser everything else already uses.
func cmdRoundIngest(args []string) error {
	fs := flag.NewFlagSet("round ingest", flag.ExitOnError)
	round := fs.Int("round", 0, "the round whose ledger takes the rows (default: the open one)")
	lens := fs.String("lens", "", "the lens name tagged onto every row (default: the report's file name, `lens-` and the extension stripped); one report only")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("usage: wf round ingest <report>... [-round N] [-lens NAME]; each report is one reviewer's output, and its LAST table is what is read")
	}
	if *lens != "" && len(pos) > 1 {
		return fmt.Errorf("-lens names ONE lens and %d reports were given; run once per report, or let the file names label them", len(pos))
	}
	// The tag lands inside a table cell. A pipe in it would split the row —
	// the read-back below would refuse the write, but under a message blaming
	// the read-back rather than the flag (found by the sweep, live).
	if strings.ContainsAny(*lens, "|\n") {
		return fmt.Errorf("-lens %q: the name is written into every row's finding cell and must not carry a pipe or a newline", *lens)
	}
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
		fmt.Printf("round %d (the open one; `-round N` ingests into another)\n", n)
	}
	dst := filepath.Join(w.RoundDir(n), "findings.md")
	before, err := os.ReadFile(dst)
	if err != nil {
		return err
	}
	have := ws.ParseFindings(before)
	if len(have) == 0 {
		return fmt.Errorf("%s carries no findings table; the scaffold `wf round new` writes has one, and rows cannot be appended to a ledger that has none", dst)
	}
	for _, r := range have {
		if r.Malformed {
			return fmt.Errorf("%s line %d is a table row of %d cell(s), not 4 (%s); repair it before rows are appended under it — escape a literal pipe as \\|", dst, r.Line, r.Cells, trunc(r.Raw, 60))
		}
	}

	// Every report is parsed and refused BEFORE a byte is written, like a
	// batch: a second report's malformed row must not leave the first one's
	// rows half-ingested.
	type staged struct {
		label string
		rows  []ws.FindingRow
		drop  int // closure cells the reviewer filled, which are not theirs to fill
		empty int // rows with no finding text
	}
	var all []staged
	for _, rep := range pos {
		label := *lens
		if label == "" {
			label = strings.TrimSuffix(filepath.Base(rep), filepath.Ext(rep))
			label = strings.TrimPrefix(label, "lens-")
		}
		b, err := os.ReadFile(rep)
		if err != nil {
			return err
		}
		block, start := lastTable(string(b))
		if block == "" {
			// The brief's own output contract, restated where it was broken:
			// a lens that returned prose and no table has not filed a
			// findings list, and the manual says to bank that as a vector that
			// DIED rather than as a clean one.
			return fmt.Errorf("%s carries no table; the brief asks the lens to end with the ledger's four-column table, and a report without one is a vector that returned nothing — bank it that way (`wf round lens`), do not read it as clean", rep)
		}
		st := staged{label: label}
		for _, r := range ws.ParseFindings([]byte(block)) {
			line := start + r.Line - 1
			if r.Malformed {
				// Two mistakes reach this line and the patch differs: a
				// summary table appended AFTER the findings (the LAST table
				// is read, so the findings must come last) and a literal pipe
				// inside a cell. The first version named only the pipe, and
				// an operator whose report ended with a two-column metrics
				// table was told to escape a pipe that was not there.
				return fmt.Errorf("%s line %d is a table row of %d cell(s), not 4 (%s) — the LAST table of a report is the one read, so the findings table must come last; inside it a literal pipe is written \\|, and a backtick code span is NOT an escape; nothing was ingested", rep, line, r.Cells, trunc(r.Raw, 60))
			}
			if r.Placeholder() {
				st.empty++
				continue
			}
			// The scale is closed on the WRITE path, and this is a write path:
			// `MAJOR (needs a gate)` or `CONCERN` in a reviewer's cell would
			// otherwise reach `wf round record` as the operator's own claim.
			if !r.KnownLevel() {
				return fmt.Errorf("%s line %d: level %q is not one of %s; the scale is closed, and the row is the reviewer's to re-level before it is ingested — nothing was ingested", rep, line, r.Level, strings.Join(ws.Levels, ", "))
			}
			if strings.TrimSpace(r.ClosedBy) != "" {
				st.drop++
			}
			st.rows = append(st.rows, r)
		}
		if len(st.rows) == 0 {
			return fmt.Errorf("%s: the last table carries no finding row — an empty table is the legitimate clean result and it is banked with `wf round lens`, not ingested; nothing was ingested", rep)
		}
		all = append(all, st)
	}

	// Numbering continues from the ledger; the scaffold's single empty row is
	// the form and is replaced rather than numbered around.
	next := 0
	for _, r := range have {
		if v, err := strconv.Atoi(numKey(r.Num)); err == nil && v > next {
			next = v
		}
	}
	next++
	scaffold := ws.ScaffoldOnly(have)
	if scaffold {
		// The form's `| 1 | BLOCKER |  |  |` is not a finding numbered 1.
		next = 1
	}
	var newLines []string
	added := 0
	for _, st := range all {
		for _, r := range st.rows {
			text := strings.TrimSpace(r.Text) + " [lens: " + st.label + "]"
			newLines = append(newLines, fmt.Sprintf("| %d | %s | %s |  |", next, strings.ToUpper(r.Level), text))
			next++
			added++
		}
	}
	lines := strings.Split(string(before), "\n")
	last := have[len(have)-1].Line // 1-based line of the ledger's last row
	var out []string
	if scaffold {
		// The placeholder row(s) go, and the new rows stand where the first
		// of them stood — directly under the separator.
		ph := map[int]bool{}
		first := 0
		for _, r := range have {
			if r.Placeholder() {
				ph[r.Line] = true
				if first == 0 {
					first = r.Line
				}
			}
		}
		for i, l := range lines {
			if ph[i+1] {
				if i+1 == first {
					out = append(out, newLines...)
				}
				continue
			}
			out = append(out, l)
		}
	} else {
		out = append(out, lines[:last]...)
		out = append(out, newLines...)
		out = append(out, lines[last:]...)
	}
	body := []byte(strings.Join(out, "\n"))
	// Read back from the bytes about to be written: exactly the rows staged,
	// none malformed, numbers unique. The count and the malformed check are
	// UNKILLABLE by mutation now that every input that could trip them is
	// refused earlier (a pipe in `-lens`, a malformed report row): they pin
	// the WRITER above them, which no test of a guard can make wrong — the
	// same standing `wf apply`'s byte-for-byte read-back has. The duplicate
	// check is reachable: a ledger hand-edited to carry one number twice.
	after := ws.ParseFindings(body)
	want := len(have) + added
	if scaffold {
		want = added
	}
	if len(after) != want {
		return fmt.Errorf("the ledger would read back with %d row(s) where %d were expected — nothing was written", len(after), want)
	}
	seen := map[string]int{}
	for _, r := range after {
		if r.Malformed {
			return fmt.Errorf("the ledger would read back with a malformed row at line %d — nothing was written", r.Line)
		}
		seen[numKey(r.Num)]++
	}
	for k, v := range seen {
		if v > 1 {
			return fmt.Errorf("the ledger would carry finding %s twice — nothing was written", k)
		}
	}
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		return err
	}
	rel, _ := filepath.Rel(w.Root, dst)
	// Committed, like a banked lens: bookkeeping that names where each row
	// came from, and a ledger the freeze reads from the worktree must not sit
	// uncommitted between two commands. The operator's dedup pass then edits
	// this file — the ledger is bookkeeping, not an artifact, and hand edits
	// to it have always been the record's shape — and `wf round record` banks
	// the result.
	labels := make([]string, 0, len(all))
	for _, st := range all {
		labels = append(labels, st.label)
	}
	commit, err := w.Git.Commit(fmt.Sprintf("round %d: ingest %d row(s) from lens %s", n, added, strings.Join(labels, ", ")), []string{rel})
	if err != nil {
		rolled := "the ledger has been rolled back"
		if werr := os.WriteFile(dst, before, 0o644); werr != nil {
			rolled = fmt.Sprintf("AND THE ROLLBACK FAILED (%v) — restore it with `git checkout -- %s`", werr, rel)
		} else if _, rerr := w.Git.Run("reset", "-q", "HEAD", "--", rel); rerr != nil {
			rolled = fmt.Sprintf("the ledger has been rolled back but is still STAGED (%v) — `git reset HEAD -- %s`", rerr, rel)
		}
		return fmt.Errorf("the ledger was written and the commit failed (%v) — %s", err, rolled)
	}
	fmt.Printf("ingested %d row(s) from %d report(s) into %s — it now holds %d row(s)\ncommitted %s\n", added, len(all), rel, len(after), short(commit))
	for _, st := range all {
		extra := ""
		if st.drop > 0 {
			extra += fmt.Sprintf("; %d closure cell(s) the lens had filled were dropped — a closure is the operator's claim, made by `wf apply -closes` or `wf round close`", st.drop)
		}
		if st.empty > 0 {
			extra += fmt.Sprintf("; %d empty row(s) skipped", st.empty)
		}
		fmt.Printf("  [lens: %s] %d row(s)%s\n", st.label, len(st.rows), extra)
	}
	// The path rule, at the moment the operator can still act on it — the same
	// count `wf round record` prints, over the rows just written.
	if arts, err := w.ArtifactList(); err == nil && len(arts) >= 2 {
		unnamed := 0
		for _, r := range after {
			if named, amb := check.PathsNamed(r.Text, arts); len(named) == 0 && len(amb) == 0 {
				unnamed++
			}
		}
		if unnamed > 0 {
			fmt.Printf("  %d of %d row(s) name no artifact path in the finding text — `wf check closure` cannot reach them; write the path in while you dedup\n", unnamed, len(after))
		}
	}
	fmt.Printf("  next: dedup across lenses in %s (two lenses filing one defect is one row), re-derive each level under gate-checks.md section 0, then `wf round record %s -round %d`\n", rel, rel, n)
	return nil
}

// lastTable returns the last run of consecutive table lines in a report and
// the 1-based line the run starts at. The brief says the operator transcribes
// from the LAST table, because a lens's prose may quote another one on the
// way; this reads the same one.
func lastTable(s string) (string, int) {
	lines := strings.Split(s, "\n")
	// A row inside a fenced block is not a row: a lens quoting the ledger's
	// own scaffold, or a table it cites as an example, inside ``` at the END
	// of its report was ingested as findings. Fences are walked from the top
	// because they only make sense read forward.
	fenced := make([]bool, len(lines))
	fence := ""
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if f := check.FenceOf(t); f != "" {
			switch {
			case fence == "":
				fence = f
			case check.FenceCloses(fence, t):
				fence = ""
			}
			fenced[i] = true
			continue
		}
		fenced[i] = fence != ""
	}
	start, end := -1, -1
	for i := len(lines) - 1; i >= 0; i-- {
		isRow := !fenced[i] && strings.HasPrefix(strings.TrimSpace(lines[i]), "|")
		if end < 0 {
			if isRow {
				end = i
			}
			continue
		}
		if !isRow {
			start = i + 1
			break
		}
	}
	if end < 0 {
		return "", 0
	}
	if start < 0 {
		start = 0
	}
	return strings.Join(lines[start:end+1], "\n"), start + 1
}
