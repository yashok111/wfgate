package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/yashok111/wfgate/internal/ws"
)

// The twenty-fifth wave: what the A4 operator session measured about where a
// round's minutes go, and the five things built from it. Each test pins one
// guard; each guard was verified to fail under mutation before it shipped.

// recordRows writes a ledger with the given rows through `wf round record`,
// the way an operator does, so the rows are committed and numbered.
func recordRows(t *testing.T, wsDir string, rows string) {
	t.Helper()
	tmp := filepath.Dir(wsDir)
	f := filepath.Join(tmp, "findings-in.md")
	write(t, f, "# Findings\n\n| # | level | finding | closed by |\n|---|-------|---------|-----------|\n"+rows)
	_, err := runWF(t, wsDir, "round", "record", f)
	noErr(t, err, "record")
}

func headOf(t *testing.T, wsDir string) string {
	t.Helper()
	raw, err := gitOut(t, wsDir, "rev-parse", "HEAD")
	noErr(t, err, "rev-parse")
	return strings.TrimSpace(raw)
}

// `wf apply -closes` fills the cells with the batch's OWN commit, in one ledger
// commit — no sha is read back and fed forward, which is how eight of one
// round's closures came to name the previous ledger commit.
func TestApplyClosesFillsTheLedgerWithTheBatchCommit(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	recordRows(t, wsDir,
		"| 1 | BLOCKER | artifacts/decisions.md: the entry point is misnamed |  |\n"+
			"| 2 | MAJOR | artifacts/decisions.md: wording |  |\n"+
			"| 3 | MINOR | artifacts/decisions.md: untouched |  |\n")
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=reword\n<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")

	out, err := runWF(t, wsDir, "apply", "-f", edits, "-closes", "#1, 2")
	noErr(t, err, "apply -closes")
	mustContain(t, out, "closed finding(s) 1, 2 in round 1 by ", "ledger committed", "wf check closure -round 1")

	// The ledger's cells name the APPLY commit — the one whose subject is the
	// batch — and not the ledger commit that HEAD now is.
	log, err := gitOut(t, wsDir, "log", "--format=%H %s", "-n", "3")
	noErr(t, err, "log")
	lines := strings.Split(strings.TrimSpace(log), "\n")
	if len(lines) < 2 || !strings.Contains(lines[0], "round 1: finding(s) 1, 2 closed by") || !strings.HasPrefix(strings.SplitN(lines[1], " ", 2)[1], "apply:") {
		t.Fatalf("want the ledger commit on top of the apply commit, got:\n%s", log)
	}
	applySha := strings.Fields(lines[1])[0]
	rows := ws.ParseFindings([]byte(read(t, filepath.Join(wsDir, "rounds", "r1", "findings.md"))))
	got := map[string]string{}
	for _, r := range rows {
		got[r.Num] = strings.TrimSpace(r.ClosedBy)
	}
	// The SHORT form, as `wf round close` stores it — a full sha in the cell
	// passed a prefix test and let the canonicalisation be bypassed.
	if got["1"] == "" || len(got["1"]) != 8 || !strings.HasPrefix(applySha, got["1"]) || got["2"] != got["1"] {
		t.Fatalf("rows 1 and 2 must be closed by the apply commit %s, got %v", applySha, got)
	}
	if got["3"] != "" {
		t.Fatalf("row 3 was not named and must stay open, got %q", got["3"])
	}
	// The closure check reaches them: the commit touched the file each names.
	out, err = runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	mustContain(t, out, "2 checked")
}

// Every refusal fires BEFORE the batch is written: a typo in the list must not
// leave the artifact changed and the closure unwritten.
func TestApplyClosesRefusesBeforeWriting(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	closed := headOf(t, wsDir)[:8]
	recordRows(t, wsDir,
		"| 1 | BLOCKER | artifacts/decisions.md: one |  |\n"+
			"| 2 | MAJOR | artifacts/decisions.md: two | "+closed+" |\n")
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=reword\n<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	before := read(t, filepath.Join(wsDir, "artifacts", "decisions.md"))
	head := headOf(t, wsDir)

	cases := []struct {
		name, closes, want string
	}{
		{"unknown number", "9", "round 1 has no finding 9 (it has: 1, 2)"},
		{"duplicate", "1,1", "finding 1 is named twice"},
		{"bare hash", "#", "passed with nothing in it"},
		{"nothing in it", " ", "passed with nothing in it"},
		{"already closed", "2", "already closed by \"" + closed + "\""},
	}
	for _, c := range cases {
		_, err := runWF(t, wsDir, "apply", "-f", edits, "-closes", c.closes)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: want %q, got %v", c.name, c.want, err)
		}
		if read(t, filepath.Join(wsDir, "artifacts", "decisions.md")) != before {
			t.Fatalf("%s: the artifact was written although the closure was refused", c.name)
		}
		if headOf(t, wsDir) != head {
			t.Fatalf("%s: a commit was made although the closure was refused", c.name)
		}
	}
}

// No open round: the flag has no ledger to write into, and says so before the
// batch lands.
func TestApplyClosesNeedsAnOpenRound(t *testing.T) {
	setGitEnv(t)
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	write(t, filepath.Join(target, "a.go"), "package a\n")
	gitIn(t, target, "init", "-q", "-b", "main")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-q", "-m", "initial")
	wsDir := filepath.Join(tmp, "ws")
	_, err := runWF(t, tmp, "init", "demo", "-target", target, "-dir", wsDir)
	noErr(t, err, "init")
	art := filepath.Join(tmp, "d.md")
	write(t, art, "Entry point: here.\n")
	_, err = runWF(t, wsDir, "add", art)
	noErr(t, err, "add")
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/d.md  count=1\n<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	_, err = runWF(t, wsDir, "apply", "-f", edits, "-closes", "1")
	if err == nil || !strings.Contains(err.Error(), "no round is open") {
		t.Fatalf("want a refusal naming the open round, got %v", err)
	}
	if read(t, filepath.Join(wsDir, "artifacts", "d.md")) != "Entry point: here.\n" {
		t.Fatal("the batch was written although -closes had no ledger")
	}
}

// --dry-run with -closes plans the closure and writes nothing, and -whole
// takes the same flag.
func TestApplyClosesDryRunAndWhole(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	recordRows(t, wsDir, "| 1 | BLOCKER | artifacts/decisions.md: one |  |\n")
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1\n<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	head := headOf(t, wsDir)
	out, err := runWF(t, wsDir, "apply", "-f", edits, "-closes", "1", "--dry-run")
	noErr(t, err, "dry run")
	mustContain(t, out, "would close finding(s) 1 in round 1")
	if headOf(t, wsDir) != head {
		t.Fatal("a dry run committed something")
	}
	rows := ws.ParseFindings([]byte(read(t, filepath.Join(wsDir, "rounds", "r1", "findings.md"))))
	if strings.TrimSpace(rows[0].ClosedBy) != "" {
		t.Fatal("a dry run filled the closure cell")
	}

	// -whole: rewrite by hand, adopt, and close in the same breath — and its
	// dry run plans the closure without a commit, exactly like -f's.
	art := filepath.Join(wsDir, "artifacts", "decisions.md")
	write(t, art, "# Decisions\n\nRewritten whole. `src/main.go:3#NeedleFn`.\n")
	out, err = runWF(t, wsDir, "apply", "-whole", "artifacts/decisions.md", "-closes", "1", "--dry-run")
	noErr(t, err, "apply -whole --dry-run -closes")
	mustContain(t, out, "would close finding(s) 1 in round 1")
	if headOf(t, wsDir) != head {
		t.Fatal("a -whole dry run committed something")
	}
	out, err = runWF(t, wsDir, "apply", "-whole", "artifacts/decisions.md", "-closes", "1")
	noErr(t, err, "apply -whole -closes")
	mustContain(t, out, "closed finding(s) 1 in round 1 by ")
	rows = ws.ParseFindings([]byte(read(t, filepath.Join(wsDir, "rounds", "r1", "findings.md"))))
	if strings.TrimSpace(rows[0].ClosedBy) == "" {
		t.Fatal("the adoption's commit did not reach the ledger")
	}
	log, _ := gitOut(t, wsDir, "log", "--format=%s", "-n", "2")
	if !strings.Contains(log, "apply -whole:") || !strings.Contains(log, "finding(s) 1 closed by") {
		t.Fatalf("want the adoption commit and its ledger commit, got:\n%s", log)
	}
}

// `wf check` (no argument) now runs stale from the newest freeze and ends with
// one summary line; before any freeze it SAYS it skipped stale rather than
// leaving it out.
func TestCheckAllRunsStaleFromTheNewestFreezeAndSummarises(t *testing.T) {
	wsDir := newGate(t)
	out, err := runWF(t, wsDir, "check")
	noErr(t, err, "check before freeze")
	// One artifact, no markers, no closure claimed: three of the five compared
	// NOTHING, and the summary must not call that clean — every header above
	// it says NOT A PASS, and a one-word "clean" under them would be the
	// empty-population defect reintroduced by the line built to save reading
	// the headers.
	mustContain(t, out, "## stale — SKIPPED: nothing is frozen yet",
		"## check — cite: clean, dup: nothing compared (NOT A PASS), rules: nothing compared (NOT A PASS), closure: nothing compared (NOT A PASS), stale: skipped (nothing frozen)")

	// A second artifact the fix never touches: the untouched corpus stale
	// looks for consumers in. The one-sentence fixture scanned ZERO sentences,
	// and `stale: clean` over that was the defect the twenty-sixth wave found
	// this test asserting.
	tmp := filepath.Dir(wsDir)
	notes := filepath.Join(tmp, "notes.md")
	write(t, notes, "# Notes\n\nA paragraph about the storage layout that no fix in this test touches.\n")
	_, err = runWF(t, wsDir, "add", notes)
	noErr(t, err, "add")
	recordRows(t, wsDir, "| 1 | NOTE | artifacts/decisions.md: nothing |  |\n")
	_, err = runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze")
	// A fix after the freeze retires a word, so stale has a population; a
	// closure naming that commit gives closure one too.
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1\n<<<<\nEntry point:\n====\nThe entrypoint is:\n>>>>\n")
	_, err = runWF(t, wsDir, "apply", "-f", edits, "-closes", "1")
	noErr(t, err, "apply -closes")
	out, err = runWF(t, wsDir, "check")
	noErr(t, err, "check after freeze")
	mustContain(t, out, "## stale — gate-r1..HEAD:", "stale: clean", "closure: clean")
	mustNotContain(t, out, "SKIPPED", "closure: nothing compared")
	// A single named check carries no summary: the line is for the set.
	out, err = runWF(t, wsDir, "check", "cite")
	noErr(t, err, "check cite")
	mustNotContain(t, out, "## check —")
}

func TestCheckAllSummaryNamesTheFailingCheck(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	// A citation whose anchor does not exist: cite finds it, the rest are
	// clean, and the summary must say which.
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1\n<<<<\n#NeedleFn\n====\n#NoSuchFn\n>>>>\n")
	_, err := runWF(t, wsDir, "apply", "-f", edits)
	noErr(t, err, "apply")
	out, err := runWF(t, wsDir, "check")
	wantFindings(t, err, "check")
	mustContain(t, out, "cite: FINDINGS", "dup: nothing compared (NOT A PASS)")
}

// `wf round ingest` appends each report's LAST table to the ledger, numbered on,
// tagged with the lens, closure cells dropped, and commits — the transcription
// half of the operator's pass, and none of the judgement half.
func TestRoundIngestAppendsTheLastTableOfEachReport(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	a := filepath.Join(tmp, "lens-closure.md")
	write(t, a, "I read the brief.\n\n| # | level | finding | closed by |\n|---|---|---|---|\n| | MAJOR | an earlier table the lens quoted | |\n\nThen the real one:\n\n"+
		"| # | level | finding | closed by |\n|---|-------|---------|-----------|\n"+
		"|  | BLOCKER | artifacts/decisions.md:3 names `NeedleFn` and the tree has `Needle` |  |\n"+
		"|  | minor | artifacts/decisions.md: wording | abc1234 |\n")
	b := filepath.Join(tmp, "ripple.txt")
	write(t, b, "| # | level | finding | closed by |\n|---|---|---|---|\n|  | NOTE | nothing else argues from it |  |\n")

	out, err := runWF(t, wsDir, "round", "ingest", a, b)
	noErr(t, err, "ingest")
	mustContain(t, out, "ingested 3 row(s) from 2 report(s) into rounds/r1/findings.md — it now holds 3 row(s)",
		"[lens: closure] 2 row(s); 1 closure cell(s) the lens had filled were dropped",
		"[lens: ripple] 1 row(s)", "committed ", "next: dedup across lenses")

	rows := ws.ParseFindings([]byte(read(t, filepath.Join(wsDir, "rounds", "r1", "findings.md"))))
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	want := []struct{ num, level, tag string }{{"1", "BLOCKER", "[lens: closure]"}, {"2", "MINOR", "[lens: closure]"}, {"3", "NOTE", "[lens: ripple]"}}
	for i, w := range want {
		r := rows[i]
		if r.Num != w.num || r.Level != w.level || !strings.HasSuffix(r.Text, w.tag) || strings.TrimSpace(r.ClosedBy) != "" {
			t.Fatalf("row %d = %+v, want %+v with an empty closure", i, r, w)
		}
	}
	if strings.Contains(rows[0].Text, "earlier table") {
		t.Fatal("the FIRST table was read; the brief says the operator transcribes from the last")
	}
	log, _ := gitOut(t, wsDir, "log", "--format=%s", "-n", "1")
	mustContain(t, log, "round 1: ingest 3 row(s) from lens closure, ripple")

	// A second ingest numbers on from the ledger, and record then banks the
	// dedup pass — identical bytes are a no-op, not a git failure.
	c := filepath.Join(tmp, "lens-facts.md")
	write(t, c, "| # | level | finding | closed by |\n|---|---|---|---|\n|  | MAJOR | one more |  |\n")
	_, err = runWF(t, wsDir, "round", "ingest", c, "-lens", "FACTS")
	noErr(t, err, "second ingest")
	rows = ws.ParseFindings([]byte(read(t, filepath.Join(wsDir, "rounds", "r1", "findings.md"))))
	if len(rows) != 4 || rows[3].Num != "4" || !strings.HasSuffix(rows[3].Text, "[lens: FACTS]") {
		t.Fatalf("want a 4th row tagged FACTS, got %+v", rows)
	}
	out, err = runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "record after ingest")
	mustContain(t, out, "identical to the committed ledger, nothing to commit", "findings: 4 row(s), 1 blocker(s)")
	mustNotContain(t, out, "\ncommitted ")
}

// The ledger-side refusals, the two-placeholder scaffold, the indented table,
// and the path note over a two-artifact workspace — every one a mutation
// survivor of the first sweep.
func TestRoundIngestLedgerShapes(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	ledgerPath := filepath.Join(wsDir, "rounds", "r1", "findings.md")
	rep := filepath.Join(tmp, "lens-a.md")
	// Indented rows are rows: a lens that returns its table inside a list.
	write(t, rep, "  | # | level | finding | closed by |\n  |---|---|---|---|\n  |  | MAJOR | indented |  |\n")

	// No table in the ledger at all.
	write(t, ledgerPath, "# Round 1\n\nprose only\n")
	_, err := runWF(t, wsDir, "round", "ingest", rep)
	if err == nil || !strings.Contains(err.Error(), "carries no findings table") {
		t.Fatalf("want the no-table refusal, got %v", err)
	}
	// A malformed row already in the ledger.
	write(t, ledgerPath, "# Round 1\n\n| # | level | finding | closed by |\n|---|---|---|---|\n| 1 | MAJOR | a | b | c |\n")
	_, err = runWF(t, wsDir, "round", "ingest", rep)
	if err == nil || !strings.Contains(err.Error(), "line 5 is a table row of 5 cell(s), not 4") {
		t.Fatalf("want the malformed-ledger refusal, got %v", err)
	}
	// TWO placeholder rows: the new rows land once, where the first stood.
	write(t, ledgerPath, "# Round 1\n\n| # | level | finding | closed by |\n|---|---|---|---|\n| 1 | BLOCKER |  |  |\n| 2 |  |  |  |\n")
	gitIn(t, wsDir, "add", "--", "rounds/r1/findings.md")
	gitIn(t, wsDir, "commit", "-q", "-m", "test: two-row scaffold")
	out, err := runWF(t, wsDir, "round", "ingest", rep)
	noErr(t, err, "ingest over a two-row scaffold")
	mustContain(t, out, "it now holds 1 row(s)")
	rows := ws.ParseFindings([]byte(read(t, ledgerPath)))
	if len(rows) != 1 || rows[0].Num != "1" || !strings.Contains(rows[0].Text, "indented") {
		t.Fatalf("want exactly one row numbered 1 from the indented table, got %+v", rows)
	}

	// A ledger already carrying one number twice: the read-back refuses the
	// write rather than adding a third row nobody can address.
	write(t, ledgerPath, "# Round 1\n\n| # | level | finding | closed by |\n|---|---|---|---|\n| 2 | MAJOR | a |  |\n| 2 | MAJOR | b |  |\n")
	gitIn(t, wsDir, "add", "--", "rounds/r1/findings.md")
	gitIn(t, wsDir, "commit", "-q", "-m", "test: duplicate number")
	_, err = runWF(t, wsDir, "round", "ingest", rep)
	if err == nil || !strings.Contains(err.Error(), "would carry finding 2 twice") {
		t.Fatalf("want the duplicate-number refusal, got %v", err)
	}
	rows = ws.ParseFindings([]byte(read(t, ledgerPath)))
	if len(rows) != 2 {
		t.Fatalf("the ledger was written although the read-back refused, %d rows", len(rows))
	}
	write(t, ledgerPath, "# Round 1\n\n| # | level | finding | closed by |\n|---|---|---|---|\n| 1 | BLOCKER |  |  |\n")
	gitIn(t, wsDir, "add", "--", "rounds/r1/findings.md")
	gitIn(t, wsDir, "commit", "-q", "-m", "test: scaffold again")
	_, err = runWF(t, wsDir, "round", "ingest", rep)
	noErr(t, err, "ingest over the restored scaffold")

	// A second artifact arms the path note; a row naming no path is counted.
	art2 := filepath.Join(tmp, "context.md")
	write(t, art2, "# Context\n")
	_, err = runWF(t, wsDir, "add", art2)
	noErr(t, err, "add second artifact")
	rep2 := filepath.Join(tmp, "lens-b.md")
	write(t, rep2, "| # | level | finding | closed by |\n|---|---|---|---|\n|  | MINOR | names no file |  |\n")
	out, err = runWF(t, wsDir, "round", "ingest", rep2)
	noErr(t, err, "ingest without a path")
	mustContain(t, out, "2 of 2 row(s) name no artifact path in the finding text")
}

// A failing commit rolls the ledger back to its pre-ingest bytes.
func TestRoundIngestRollsBackWhenTheCommitFails(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	ledgerPath := filepath.Join(wsDir, "rounds", "r1", "findings.md")
	before := read(t, ledgerPath)
	rep := filepath.Join(tmp, "lens-a.md")
	write(t, rep, "| # | level | finding | closed by |\n|---|---|---|---|\n|  | MAJOR | a row |  |\n")
	hook := filepath.Join(wsDir, ".git", "hooks", "pre-commit")
	write(t, hook, "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := runWF(t, wsDir, "round", "ingest", rep)
	if err == nil || !strings.Contains(err.Error(), "the ledger has been rolled back") {
		t.Fatalf("want the rollback message, got %v", err)
	}
	if read(t, ledgerPath) != before {
		t.Fatal("the ledger still holds the ingested rows after a failed commit")
	}
	st, _ := gitOut(t, wsDir, "status", "--porcelain")
	if strings.TrimSpace(st) != "" {
		t.Fatalf("something is left staged or dirty:\n%s", st)
	}
}

// `wf round close "#" <sha>` reaches the planner with an empty number.
func TestRoundCloseRefusesAnEmptyNumber(t *testing.T) {
	wsDir := newGate(t)
	recordRows(t, wsDir, "| 1 | MAJOR | artifacts/decisions.md: one |  |\n")
	// A commit that touched an artifact, so the sha passes its own refusal
	// and the number is what gets judged.
	artSha, err := gitOut(t, wsDir, "log", "--format=%H", "-n", "1", "--", "artifacts")
	noErr(t, err, "log")
	_, err = runWF(t, wsDir, "round", "close", "#", strings.TrimSpace(artSha))
	if err == nil || !strings.Contains(err.Error(), "an empty finding number") {
		t.Fatalf("want the empty-number refusal, got %v", err)
	}
}

// Before any freeze, `-since` still runs stale from the named ref; and a stale
// hit moves the exit code of the whole command.
func TestCheckAllStaleSinceAndExitCode(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	// Two sentences long enough to be statements, sharing the word the fix
	// will retire from one of them.
	art := filepath.Join(tmp, "notes.md")
	write(t, art, "# Notes\n\nThe entrypoint of the whole program is the needle function here.\n\nEvery caller reaches the entrypoint of the whole program through the needle function here.\n")
	_, err := runWF(t, wsDir, "add", art)
	noErr(t, err, "add")
	base := headOf(t, wsDir)
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/notes.md  count=1\n<<<<\nThe entrypoint of the whole program\n====\nThe start of the whole program\n>>>>\n")
	_, err = runWF(t, wsDir, "apply", "-f", edits)
	noErr(t, err, "apply")
	out, err := runWF(t, wsDir, "check", "-since", base)
	wantFindings(t, err, "check -since before any freeze")
	mustContain(t, out, "## stale — "+base+"..HEAD:", "stale: FINDINGS")
	mustNotContain(t, out, "SKIPPED")
}

// Every report is refused BEFORE a byte is written, and each refusal names the
// report and the line.
func TestRoundIngestRefusals(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	before := read(t, filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	head := headOf(t, wsDir)
	good := filepath.Join(tmp, "good.md")
	write(t, good, "| # | level | finding | closed by |\n|---|---|---|---|\n|  | MAJOR | fine |  |\n")
	cases := []struct{ name, body, want string }{
		{"no table", "prose only, the lens never concluded\n", "carries no table"},
		{"empty table", "| # | level | finding | closed by |\n|---|---|---|---|\n|  |  |  |  |\n", "the last table carries no finding row"},
		{"malformed", "| # | level | finding | closed by |\n|---|---|---|---|\n|  | MAJOR | a | b | c |\n", "line 3 is a table row of 5 cell(s), not 4"},
		{"off-scale", "| # | level | finding | closed by |\n|---|---|---|---|\n|  | CONCERN | a |  |\n", "line 3: level \"CONCERN\" is not one of BLOCKER, MAJOR, MINOR, NOTE"},
	}
	for _, c := range cases {
		bad := filepath.Join(tmp, "bad.md")
		write(t, bad, c.body)
		// The good report FIRST, so a refusal on the second must not leave
		// the first half-ingested.
		_, err := runWF(t, wsDir, "round", "ingest", good, bad)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: want %q, got %v", c.name, c.want, err)
		}
		if read(t, filepath.Join(wsDir, "rounds", "r1", "findings.md")) != before || headOf(t, wsDir) != head {
			t.Fatalf("%s: the ledger or the history changed although the ingest was refused", c.name)
		}
	}
	_, err := runWF(t, wsDir, "round", "ingest", good, good, "-lens", "x")
	if err == nil || !strings.Contains(err.Error(), "-lens names ONE lens and 2 reports were given") {
		t.Fatalf("want the -lens/multi-report refusal, got %v", err)
	}
	// A pipe in the tag would split every row; refused at the flag, not at
	// the read-back (which blamed the write, not the operator's value).
	_, err = runWF(t, wsDir, "round", "ingest", good, "-lens", "a|b")
	if err == nil || !strings.Contains(err.Error(), "-lens \"a|b\": the name is written into every row's finding cell") {
		t.Fatalf("want the pipe-in-lens refusal, got %v", err)
	}
	if read(t, filepath.Join(wsDir, "rounds", "r1", "findings.md")) != before || headOf(t, wsDir) != head {
		t.Fatal("the ledger or the history changed although -lens was refused")
	}
	// The refusal names the REPORT's line, not the table's: prose above the
	// table shifts them apart, and every other case here starts at line 1.
	shifted := filepath.Join(tmp, "shifted.md")
	write(t, shifted, "I read everything.\n\nSome prose.\n\n| # | level | finding | closed by |\n|---|---|---|---|\n|  | MAJOR | a | b | c |\n")
	_, err = runWF(t, wsDir, "round", "ingest", shifted)
	if err == nil || !strings.Contains(err.Error(), "shifted.md line 7 is a table row of 5 cell(s)") {
		t.Fatalf("want the refusal at the report's line 7, got %v", err)
	}
	_, err = runWF(t, wsDir, "round", "ingest")
	if err == nil || !strings.Contains(err.Error(), "usage: wf round ingest") {
		t.Fatalf("want usage, got %v", err)
	}
}

// lastTable reads the LAST run of table lines and reports where it starts, so
// a refusal can name a line of the REPORT rather than of the block.
func TestLastTable(t *testing.T) {
	block, start := lastTable("a\n| x |\n| y |\nb\n\n| p |\n| q |\n\ntrailing\n")
	if block != "| p |\n| q |" || start != 6 {
		t.Fatalf("got %q at %d", block, start)
	}
	if b, s := lastTable("no table\n"); b != "" || s != 0 {
		t.Fatalf("want nothing, got %q at %d", b, s)
	}
	if b, s := lastTable("| only |"); b != "| only |" || s != 1 {
		t.Fatalf("a table on the first line: got %q at %d", b, s)
	}
	if b, s := lastTable("x\n  | in |\n\t| dented |\n"); b != "  | in |\n\t| dented |" || s != 2 {
		t.Fatalf("indented rows are rows: got %q at %d", b, s)
	}
}

// The reviewer's brief carries the navigation rule and the pinned file's
// section index by LINE, both read off the workspace rather than remembered.
func TestNewLensCarriesTheNavigationRuleAndTheSectionIndex(t *testing.T) {
	wsDir := newGate(t)
	dst := filepath.Join(wsDir, "..", "lens-facts.md")
	_, err := runWF(t, wsDir, "new-lens", "facts", "-o", dst)
	noErr(t, err, "new-lens")
	body := read(t, dst)
	mustContain(t, body, "HOW YOU READ: with Read, Grep and Glob", "a round-trip carrying your whole context")

	// The index, re-derived here from the pinned copy by an independent
	// reading: the line of `## 0.` and the line before `## A.`.
	pinned := strings.Split(read(t, filepath.Join(wsDir, "gate-checks.md")), "\n")
	from, aLine, iLine, appLine := 0, 0, 0, 0
	for i, l := range pinned {
		switch {
		case strings.HasPrefix(l, "## 0. "):
			from = i + 1
		case strings.HasPrefix(l, "## A. "):
			aLine = i + 1
		case strings.HasPrefix(l, "## I. "):
			iLine = i + 1
		case strings.HasPrefix(l, "## Appending"):
			appLine = i + 1
		}
	}
	if from == 0 || aLine == 0 || iLine == 0 || appLine == 0 {
		t.Fatalf("the pinned gate-checks.md lost its headings (0 at %d, A at %d, I at %d, Appending at %d)", from, aLine, iLine, appLine)
	}
	// The stamp line `wf init` prepends shifts every heading by one against
	// the source; the index must follow the PINNED copy.
	if from < 2 {
		t.Fatalf("section 0 at line %d: the pinned copy carries no stamp line", from)
	}
	mustContain(t, body,
		"what a row must carry: lines "+strconv.Itoa(from)+"-"+strconv.Itoa(aLine-1),
		"the other sections by line, so none has to be searched for: A "+strconv.Itoa(aLine)+"-",
		", I "+strconv.Itoa(iLine)+"-"+strconv.Itoa(appLine-1))
	// The closing heading carries no id and is not indexed itself; it BOUNDS
	// section I, which a mutation letting it through reported as running to
	// the end of the file.
	if m := regexp.MustCompile(`I \d+-(\d+)`).FindStringSubmatch(body); m == nil || m[1] != strconv.Itoa(appLine-1) {
		t.Fatalf("section I must end at line %d, the line before the closing heading, in:\n%s", appLine-1, body)
	}

	// No pinned copy: the line says so instead of inventing ranges.
	if err := os.Remove(filepath.Join(wsDir, "gate-checks.md")); err != nil {
		t.Fatal(err)
	}
	dst2 := filepath.Join(wsDir, "..", "lens-facts2.md")
	_, err = runWF(t, wsDir, "new-lens", "facts2", "-o", dst2)
	noErr(t, err, "new-lens without the pinned copy")
	body = read(t, dst2)
	mustContain(t, body, "what a row must carry (no line index: ")
	mustNotContain(t, body, "the other sections by line")
}

func TestGateChecksIndexRefusesAFileWithoutTheHeadings(t *testing.T) {
	// Neither half, and EACH half alone: a file with sections 0-0c and nothing
	// after them has no index to print, and one with A-I and no section 0 has
	// no head range — the refusal is an OR, and a mutation to AND passed the
	// neither-half case alone.
	for name, body := range map[string]string{
		"neither":   "## Something else\n\ntext\n",
		"head only": "## 0. scale\n\n## 0b. asks\n\n## 0c. carry\n",
		"rest only": "## A. facts\n\n## B. seams\n",
	} {
		f := filepath.Join(t.TempDir(), "x.md")
		write(t, f, body)
		if _, _, err := gateChecksIndex(f); err == nil || !strings.Contains(err.Error(), "carries no `## 0.`/`## A.` headings") {
			t.Fatalf("%s: want the missing-headings refusal, got %v", name, err)
		}
	}
}
