package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yashok111/wfgate/internal/ws"
)

// The twenty-sixth wave: a bug hunt over the whole toolchain by five vectors,
// every finding reproduced before it was fixed. Each test here pins one of
// them and was checked to FAIL with its fix reverted.

// `wf apply -f` accepted any path that did not escape the WORKSPACE — so a
// header reading `artifacts/../gate-checks.md` rewrote the pinned reference,
// `-closes` closed a BLOCKER with that commit, and freeze and verify (which
// watch `artifacts/` alone) both printed clean over it.
func TestApplyRefusesAPathOutsideArtifacts(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	recordRows(t, wsDir, "| 1 | BLOCKER | artifacts/decisions.md: one |  |\n")
	pin := filepath.Join(wsDir, "gate-checks.md")
	before := read(t, pin)
	first := strings.SplitN(before, "\n", 2)[0]
	head := headOf(t, wsDir)
	for _, spelling := range []string{"artifacts/../gate-checks.md", "gate-checks.md", "rounds/r1/findings.md"} {
		edits := filepath.Join(tmp, "edits.txt")
		body := first
		if spelling == "rounds/r1/findings.md" {
			body = "| 1 | BLOCKER | artifacts/decisions.md: one |  |"
		}
		write(t, edits, "=== "+spelling+"  count=1\n<<<<\n"+body+"\n====\n# TAMPERED\n>>>>\n")
		_, err := runWF(t, wsDir, "apply", "-f", edits, "-closes", "1")
		if err == nil || !strings.Contains(err.Error(), "is not under artifacts/") || !strings.Contains(err.Error(), "nothing was written") {
			t.Fatalf("%s: want the artifacts/ refusal, got %v", spelling, err)
		}
		if read(t, pin) != before {
			t.Fatalf("%s: the pinned reference was rewritten", spelling)
		}
		if headOf(t, wsDir) != head {
			t.Fatalf("%s: a commit was made", spelling)
		}
	}
	rows := ws.ParseFindings([]byte(read(t, filepath.Join(wsDir, "rounds", "r1", "findings.md"))))
	if len(rows) != 1 || strings.TrimSpace(rows[0].ClosedBy) != "" {
		t.Fatalf("the refused batch must not have closed anything, got %+v", rows)
	}
}

// A ledger row written `| #1 |` — the column is headed `#`, and the report a
// lens returns prints `#1` — passed `wf round record` and could then be closed
// by NO spelling: the argument had its `#` stripped, the cell did not. After
// `wf round ingest` numbered a new row `1` beside it (it reads numbers with
// Atoi and skipped `#1`), `wf round close '#1'` closed the WRONG row silently.
func TestHashNumberedRowIsTheSameRow(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	recordRows(t, wsDir, "| #1 | BLOCKER | artifacts/decisions.md:3 the real blocker |  |\n")

	// The refusal lists a spelling the command accepts.
	_, err := runWF(t, wsDir, "round", "close", "9", "-prose", "x")
	if err == nil || !strings.Contains(err.Error(), "(it has: 1)") {
		t.Fatalf("want the refusal to list `1`, not `#1`, got %v", err)
	}
	// Ingest numbers ON from the hash-written row rather than beside it.
	rep := filepath.Join(tmp, "lens-a.md")
	write(t, rep, "| # | level | finding | closed by |\n|---|---|---|---|\n| 1 | MINOR | artifacts/decisions.md: a nit |  |\n")
	_, err = runWF(t, wsDir, "round", "ingest", rep)
	noErr(t, err, "ingest")
	rows := ws.ParseFindings([]byte(read(t, filepath.Join(wsDir, "rounds", "r1", "findings.md"))))
	if len(rows) != 2 || strings.TrimSpace(rows[1].Num) != "2" {
		t.Fatalf("the ingested row must be numbered 2, after #1; got %+v", rows)
	}
	// And `#1` closes the blocker — the row it names, not the ingested one.
	out, err := runWF(t, wsDir, "round", "close", "#1", "-prose", "waived on measurement")
	noErr(t, err, "round close #1")
	mustContain(t, out, "the real blocker")
	rows = ws.ParseFindings([]byte(read(t, filepath.Join(wsDir, "rounds", "r1", "findings.md"))))
	if strings.TrimSpace(rows[0].ClosedBy) != "waived on measurement" || strings.TrimSpace(rows[1].ClosedBy) != "" {
		t.Fatalf("the closure landed on the wrong row: %+v", rows)
	}
}

// `wf check closure` read a commit's diff with `git show`, which prints a
// combined diff for a MERGE (nothing at all for a clean one) that the parser
// does not read — so a merge carrying a real artifact fix was counted as
// CHECKED and reported as a FILE miss, while `wf round close` accepted it.
func TestClosureReadsAMergeCommit(t *testing.T) {
	wsDir := newGate(t)
	recordRows(t, wsDir, "| 1 | BLOCKER | artifacts/decisions.md:3 the entry point is misnamed |  |\n")
	// A branch that fixes the artifact, merged back with --no-ff.
	gitIn(t, wsDir, "checkout", "-q", "-b", "fix")
	art := filepath.Join(wsDir, "artifacts", "decisions.md")
	write(t, art, strings.Replace(read(t, art), "Entry point:", "The entry point is:", 1))
	gitIn(t, wsDir, "commit", "-q", "-am", "fix on a branch")
	gitIn(t, wsDir, "checkout", "-q", "main")
	write(t, filepath.Join(wsDir, "rounds", "r1", "note.md"), "a note on main\n")
	gitIn(t, wsDir, "add", "rounds/r1/note.md")
	gitIn(t, wsDir, "commit", "-q", "-m", "a note on main")
	gitIn(t, wsDir, "merge", "-q", "--no-ff", "-m", "merge the fix", "fix")
	merge := headOf(t, wsDir)

	_, err := runWF(t, wsDir, "round", "close", "1", merge)
	noErr(t, err, "round close accepts the merge")
	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	mustContain(t, out, "1 checked against the artifact they name, 0 not closed by that commit")
	mustNotContain(t, out, "did not touch")
}

// `fileOnly` was counted on a closure that missed the FILE, so the note told
// the operator to "cite the section id or the line" over a finding that cited
// a line. What a row cites is a property of the row, read before the commit
// decides anything.
func TestFileMissIsNotCalledFileOnly(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	other := filepath.Join(tmp, "other.md")
	write(t, other, "# Other\n\nA second artifact with its own sentence.\n")
	_, err := runWF(t, wsDir, "add", other)
	noErr(t, err, "add")
	recordRows(t, wsDir, "| 1 | BLOCKER | artifacts/decisions.md:3 the entry point is misnamed |  |\n")
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/other.md  count=1\n<<<<\nA second artifact\n====\nAnother artifact\n>>>>\n")
	_, err = runWF(t, wsDir, "apply", "-f", edits, "-closes", "1")
	noErr(t, err, "apply -closes")
	out, err := runWF(t, wsDir, "check", "closure")
	wantFindings(t, err, "check closure")
	mustContain(t, out, "did not touch artifacts/decisions.md")
	mustNotContain(t, out, "cite neither")
}

// A fix that rewrote the ONLY sentence the corpus had left every retired token
// "surviving nowhere" — in a corpus of zero untouched sentences — and the
// check printed `swept: 1 of 1` with `stale: clean` in the summary line.
func TestStaleOverNoUntouchedCorpusIsNotAPass(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	art := filepath.Join(tmp, "rule.md")
	write(t, art, "# Rule\n\nThe allocator hands out the next free identifier from the pool every time.\n")
	_, err := runWF(t, wsDir, "add", art)
	noErr(t, err, "add")
	_, err = runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze")
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/rule.md  count=1\n<<<<\nThe allocator hands out the next free identifier from the pool every time.\n====\nA counter increments once per request and the caller keeps the value.\n>>>>\n")
	_, err = runWF(t, wsDir, "apply", "-f", edits)
	noErr(t, err, "apply")
	out, err := runWF(t, wsDir, "check", "stale")
	noErr(t, err, "check stale")
	mustContain(t, out, "0 scanned", "NOT A PASS", "swept against nothing")
	mustNotContain(t, out, "survive NOWHERE")
	out, err = runWF(t, wsDir, "check")
	if err != nil && !strings.Contains(err.Error(), "findings") {
		t.Fatal(err)
	}
	mustContain(t, out, "stale: nothing compared (NOT A PASS)")
}

// The LAST table of a report is read — and a fenced example table at the end
// of one was ingested as findings, while a two-column summary table after the
// findings refused the whole report under a message about escaping pipes.
func TestIngestSkipsFencedTablesAndNamesTheLastTableRule(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	rep := filepath.Join(tmp, "lens-fenced.md")
	write(t, rep, "| # | level | finding | closed by |\n|---|---|---|---|\n| 1 | MAJOR | artifacts/decisions.md:3 the real row |  |\n\nFor example, a row is written:\n\n```\n| # | level | finding | closed by |\n|---|---|---|---|\n| 1 | BLOCKER | artifacts/decisions.md an example |  |\n```\n")
	out, err := runWF(t, wsDir, "round", "ingest", rep)
	noErr(t, err, "ingest")
	mustContain(t, out, "1 row(s)")
	rows := ws.ParseFindings([]byte(read(t, filepath.Join(wsDir, "rounds", "r1", "findings.md"))))
	if len(rows) != 1 || !strings.Contains(rows[0].Text, "the real row") {
		t.Fatalf("want the unfenced table's one row, got %+v", rows)
	}

	rep = filepath.Join(tmp, "lens-summary.md")
	write(t, rep, "| # | level | finding | closed by |\n|---|---|---|---|\n| 1 | MAJOR | artifacts/decisions.md:3 x |  |\n\nSummary:\n\n| metric | value |\n|---|---|\n| files | 3 |\n")
	_, err = runWF(t, wsDir, "round", "ingest", rep)
	if err == nil || !strings.Contains(err.Error(), "findings table must come last") {
		t.Fatalf("want the last-table patch named, got %v", err)
	}
}

// The review round over this wave. `artifacts` with no separator is a PREFIX
// of `artifactsx/` and `artifacts.md`, and the sweep's mutation dropping the
// separator survived every test above; the symlink case is the external
// vector's — a lexical prefix test is satisfied by `artifacts/link/x.md` where
// `artifacts/link` points at the target.
func TestApplyBoundaryIsASeparatorAndAResolvedPath(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	target := filepath.Join(tmp, "target")
	write(t, filepath.Join(target, "victim.md"), "untouched\n")
	if err := os.Symlink(target, filepath.Join(wsDir, "artifacts", "link")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	head := headOf(t, wsDir)
	for _, spelling := range []string{"artifactsx/a.md", "artifacts.md", "artifacts/link/victim.md"} {
		edits := filepath.Join(tmp, "edits.txt")
		write(t, edits, "=== "+spelling+"  count=1\n<<<<\nuntouched\n====\nrewritten\n>>>>\n")
		_, err := runWF(t, wsDir, "apply", "-f", edits)
		if err == nil || !strings.Contains(err.Error(), "artifacts/") || !strings.Contains(err.Error(), "nothing was written") {
			t.Fatalf("%s: want the artifacts/ refusal, got %v", spelling, err)
		}
		if headOf(t, wsDir) != head {
			t.Fatalf("%s: a commit was made", spelling)
		}
	}
	if read(t, filepath.Join(target, "victim.md")) != "untouched\n" {
		t.Fatal("the batch wrote through the symlink into the target")
	}
	// A symlink to a directory INSIDE artifacts/ is fine: the resolved path
	// stays under it.
	write(t, filepath.Join(wsDir, "artifacts", "sub", "in.md"), "untouched\n")
	gitIn(t, wsDir, "add", "artifacts/sub")
	gitIn(t, wsDir, "commit", "-q", "-m", "a subdirectory")
	if err := os.Symlink(filepath.Join(wsDir, "artifacts", "sub"), filepath.Join(wsDir, "artifacts", "alias")); err != nil {
		t.Fatal(err)
	}
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/alias/in.md  count=1\n<<<<\nuntouched\n====\nrewritten\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", edits, "-dry-run"); err != nil {
		t.Fatalf("a symlink resolving INSIDE artifacts/ must pass the boundary, got %v", err)
	}
}

// A ledger already holding `#1` and `1` — one hand edit and one ingest from
// before this wave — is two rows with one number, and an ingest over it must
// refuse in its read-back rather than append a third reading.
func TestIngestRefusesALedgerHoldingOneNumberTwice(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	ledger(t, wsDir, 1,
		"| #1 | BLOCKER | artifacts/decisions.md: one |  |\n"+
			"| 1 | MINOR | artifacts/decisions.md: the same number again |  |\n")
	rep := filepath.Join(tmp, "lens-a.md")
	write(t, rep, "| # | level | finding | closed by |\n|---|---|---|---|\n| 1 | MINOR | artifacts/decisions.md: a nit |  |\n")
	_, err := runWF(t, wsDir, "round", "ingest", rep)
	if err == nil || !strings.Contains(err.Error(), "would carry finding 1 twice") {
		t.Fatalf("want the duplicate-number refusal, got %v", err)
	}
}

// Markdown closes a fence with the SAME character and a run at least as long:
// a four-backtick fence holding a ```go example is one block, and a `~~~` line
// inside a backtick fence closes nothing. Both used to end the fence early and
// hand the example table after it to the ledger.
func TestIngestFenceIsClosedByLengthAndCharacter(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	// Two reports, one per half of the rule: the LENGTH half (a ```go line
	// inside a four-backtick fence) and the CHARACTER half (a ~~~ line inside
	// a backtick fence). One fixture carrying both let a mutant that closed
	// on the inner line re-open on the next and fence the table by accident.
	table := "| # | level | finding | closed by |\n|---|---|---|---|\n| 1 | BLOCKER | artifacts/decisions.md an example |  |\n"
	for name, fenced := range map[string]string{
		"length":    "````\n```go\n" + table + "````\n",
		"character": "```\n~~~\n" + table + "```\n",
	} {
		rep := filepath.Join(tmp, "lens-"+name+".md")
		write(t, rep, "| # | level | finding | closed by |\n|---|---|---|---|\n| 1 | MAJOR | artifacts/decisions.md:3 the real row "+name+" |  |\n\n"+fenced)
		_, err := runWF(t, wsDir, "round", "ingest", rep)
		noErr(t, err, "ingest "+name)
	}
	rows := ws.ParseFindings([]byte(read(t, filepath.Join(wsDir, "rounds", "r1", "findings.md"))))
	if len(rows) != 2 || !strings.Contains(rows[0].Text, "the real row") || !strings.Contains(rows[1].Text, "the real row") {
		t.Fatalf("want the two unfenced rows and no example, got %+v", rows)
	}
}

// A merge is read against its FIRST parent only. `-m` unions the diff against
// every parent, and a second parent's hunks are in that parent's coordinates:
// here the branch inserts three lines at the top, so main's edit to D1's rule
// (line 5) sits at line 8 in the branch's numbering and lands inside D2 (7-9)
// in the first parent's — a closure of a D2 finding read as landed by a commit
// that never touched D2.
func TestClosureReadsAMergeAgainstItsFirstParentOnly(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	doc := filepath.Join(tmp, "doc.md")
	write(t, doc, "# Doc\n\n## D1 first\n\nRule one here.\n\n## D2 second\n\nRule two here.\n")
	_, err := runWF(t, wsDir, "add", doc)
	noErr(t, err, "add")
	recordRows(t, wsDir, "| 1 | BLOCKER | D2 in artifacts/doc.md contradicts the reader |  |\n")
	art := filepath.Join(wsDir, "artifacts", "doc.md")
	gitIn(t, wsDir, "checkout", "-q", "-b", "shift")
	write(t, art, "a\nb\nc\n"+read(t, art))
	gitIn(t, wsDir, "commit", "-q", "-am", "three lines on top")
	gitIn(t, wsDir, "checkout", "-q", "main")
	write(t, art, strings.Replace(read(t, art), "Rule one here.", "Rule one, restated.", 1))
	gitIn(t, wsDir, "commit", "-q", "-am", "edit D1 on main")
	gitIn(t, wsDir, "merge", "-q", "--no-ff", "-m", "merge the shift", "shift")
	merge := headOf(t, wsDir)
	_, err = runWF(t, wsDir, "round", "close", "1", merge)
	noErr(t, err, "round close")
	out, err := runWF(t, wsDir, "check", "closure")
	wantFindings(t, err, "check closure")
	mustContain(t, out, "NOT any section it cites: D2")
}
