// End-to-end test of the prescribed sequence, from `wf init` to `wf delta`,
// asserting on OUTPUT rather than on returned errors.
//
// Every step here was once a real blocker: `verify` did not compare what it
// promised, `delta` diffed against the newest tag and printed an empty diff,
// and the first commit of an artifact had no command to make it. The failure
// shape both documents call the worst — a command that exits zero having
// checked nothing — is invisible to an assertion on the error alone, which is
// why each step checks the printed text.
package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"git.sumka.site/yakov/wfgate/internal/gitx"
)

// dispatch is main's own switch, not a copy of it. The copy went stale the
// first time a command was added.
func dispatch(args []string) error { return run(args) }

// runWF runs one subcommand in-process from dir, capturing stdout and stderr
// together. In-process rather than a built binary so `go test -cover` counts
// these paths — cmd/wf carried the whole prescribed sequence at 0.0% coverage
// before this file existed.
func runWF(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	cmdErr := dispatch(args)
	_ = w.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	out := <-done
	t.Logf("$ wf %s\n%s", strings.Join(args, " "), out)
	return out, cmdErr
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

// setGitEnv pins identity and blanks host config, so the commits the tool makes
// succeed on a machine with no global git identity and are not bent by whatever
// the host config carries (signing, hooks, templates).
func setGitEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	for _, who := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		t.Setenv(who+"_NAME", "wf-test")
		t.Setenv(who+"_EMAIL", "wf@example.invalid")
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustContain(t *testing.T, out string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(out, s) {
			t.Fatalf("output must contain %q, got:\n%s", s, out)
		}
	}
}

func mustNotContain(t *testing.T, out string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if strings.Contains(out, s) {
			t.Fatalf("output must not contain %q, got:\n%s", s, out)
		}
	}
}

func noErr(t *testing.T, err error, step string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", step, err)
	}
}

func wantFindings(t *testing.T, err error, step string) {
	t.Helper()
	if !errors.Is(err, errFindings) {
		t.Fatalf("%s: want errFindings, got %v", step, err)
	}
}

// The apply-side cost meter: a note, never a refusal, and no claim without
// history — an unmeasured workspace predicts nothing. The denominator is
// CHANGED LINES, so a batch of few edits over many lines is priced like the many
// lines it is: measured, the most expensive batch of one gate had four edits.
func TestBatchNote(t *testing.T) {
	// 1600 changed lines historically producing 80 findings is 5 per 100; a
	// batch of 400 lines therefore predicts ~20, whether it took 4 edits or 60.
	hist := []histRound{{N: 2, Findings: 40, Lines: 800}, {N: 3, Findings: 40, Lines: 800}}
	note, warn := batchNote(60, 400, hist, nil, nil, nil)
	if !warn || !strings.Contains(note, "predicts ~20 finding(s)") ||
		!strings.Contains(note, "split by RULE") || !strings.Contains(note, "consumers are NOT in this diff") {
		t.Fatalf("want a prediction, the split advice and the rule warning: %q", note)
	}
	few, warn2 := batchNote(4, 400, hist, nil, nil, nil)
	if !warn2 || !strings.Contains(few, "predicts ~20 finding(s)") {
		t.Fatalf("the edit COUNT must not change the prediction: %q", few)
	}
	if _, warn := batchNote(60, 20, hist, nil, nil, nil); warn {
		t.Fatal("a batch changing few lines must pass silently")
	}
	if _, warn := batchNote(60, 400, nil, nil, nil, nil); warn {
		t.Fatal("no history, no claim")
	}
	if _, warn := batchNote(60, 0, hist, nil, nil, nil); warn {
		t.Fatal("a batch git sees as changing nothing predicts nothing")
	}
}

// The trend, not only the pooled ratio. A live decision gate ran 5.4 -> 7.1 ->
// 8.7 -> 10.3 findings per 100 changed lines while its batches SHRANK, so the
// pooled number predicted 5-6 against actuals of 21-25; a script gate measured
// the same ratio flat across a 12x range of batch sizes. The pooled figure
// cannot say which regime produced it and the series can.
func TestBatchNote_printsTheTrendAndNamesTheRegime(t *testing.T) {
	rising := []histRound{
		{N: 2, Findings: 24, Lines: 447},
		{N: 3, Findings: 25, Lines: 354},
		{N: 4, Findings: 23, Lines: 264},
	}
	note, warn := batchNote(20, 300, rising, nil, nil, nil)
	if !warn {
		t.Fatal("want a note")
	}
	for _, want := range []string{"r2 5.4", "r3 7.1", "r4 8.7", "RISING", "FLOOR"} {
		if !strings.Contains(note, want) {
			t.Fatalf("want %q in the trend line: %q", want, note)
		}
	}
	// Two points are not a direction, and a wandering series is not a regime.
	wobbly := []histRound{
		{N: 2, Findings: 24, Lines: 447},
		{N: 3, Findings: 10, Lines: 354},
		{N: 4, Findings: 23, Lines: 264},
	}
	if n, _ := batchNote(20, 300, wobbly, nil, nil, nil); strings.Contains(n, "RISING") || strings.Contains(n, "falling") {
		t.Fatalf("a non-monotone series must not be named a regime: %q", n)
	}
}

// The authoring window prices a corpus its batches did not write. Measured: one
// such round put the prediction at ~355 findings against an actual 24, printed
// in the register of a measurement.
func TestBatchNote_authoringRoundIsExcludedAndSaidSoOutLoud(t *testing.T) {
	hist := []histRound{{N: 2, Findings: 24, Lines: 447}, {N: 3, Findings: 25, Lines: 354}}
	note, warn := batchNote(20, 300, hist, []int{1}, nil, nil)
	if !warn {
		t.Fatal("want a note")
	}
	if !strings.Contains(note, "round(s) 1 are NOT in that history") || !strings.Contains(note, "window ADDS an artifact") {
		t.Fatalf("the exclusion must be named, not silent: %q", note)
	}
	// The excluded round must not reach the prediction: 49 findings over 801
	// lines is 6.1 per 100, so 300 lines predicts ~18. Round 1's 34 findings
	// over 9 lines would have made it ~600.
	if !strings.Contains(note, "predicts ~18 finding(s)") {
		t.Fatalf("the authoring round leaked into the prediction: %q", note)
	}
}

func TestPrescribedSequence(t *testing.T) {
	setGitEnv(t)
	tmp := t.TempDir()

	// The repository under review. Three lines, so an anchored citation can
	// target line 3 and an unanchored one line 2.
	target := filepath.Join(tmp, "target")
	write(t, filepath.Join(target, "src", "main.go"), "package main\n\nfunc NeedleFn() {}\n")
	write(t, filepath.Join(target, "Makefile"), "all:\n\tgo build\n")
	gitIn(t, target, "init", "-q", "-b", "main")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-q", "-m", "initial")

	wsDir := filepath.Join(tmp, "ws")
	out, err := runWF(t, tmp, "init", "demo", "-target", target, "-dir", wsDir)
	noErr(t, err, "init")
	mustContain(t, out, "workspace demo", target, "gate-checks.md written")

	// The workspace copy pins the rules version that briefs this gate's
	// reviewers, stamped with the tool that wrote it.
	checks, err := os.ReadFile(filepath.Join(wsDir, "gate-checks.md"))
	noErr(t, err, "read workspace gate-checks")
	mustContain(t, string(checks), "written by wf (devel) at init", "known failure modes")

	// The manual ships inside the binary; `guide` is a concatenation of the
	// parts, never a stored second copy.
	out, err = runWF(t, wsDir, "docs")
	noErr(t, err, "docs list")
	mustContain(t, out, "guide", "checks", "routing")
	out, err = runWF(t, wsDir, "docs", "guide")
	noErr(t, err, "docs guide")
	mustContain(t, out, "# Workflow model and effort routing", "# Workflow cost & context discipline")

	// Two artifacts carrying the same rule. `wf add` is the only way to make an
	// artifact's first commit — apply rejects an empty `old` — so the sequence
	// starts here.
	rule := "- [R1] The composition stays a pure function called from buildRuntime, never a branch inside it.\n"
	write(t, filepath.Join(tmp, "context.md"), "# Context\n\n"+rule+"\nEntry point: `src/main.go:3#NeedleFn`.\nBuild entry: Makefile:2.\n")
	write(t, filepath.Join(tmp, "script.md"), "# Script\n\n"+rule+"\nBase: `src/main.go:2`.\n")
	out, err = runWF(t, wsDir, "add", filepath.Join(tmp, "context.md"), filepath.Join(tmp, "script.md"))
	noErr(t, err, "add")
	mustContain(t, out, "added artifacts/context.md", "added artifacts/script.md", "committed ")

	// Re-adding a TRACKED artifact must refuse: an overwrite-by-add would be
	// an unvalidated, uncounted whole-file replacement, the exact thing apply
	// exists to prevent.
	if _, err = runWF(t, wsDir, "add", filepath.Join(tmp, "context.md")); err == nil || !strings.Contains(err.Error(), "already tracked in this workspace") {
		t.Fatalf("re-add: want the reviewed-artifact refusal, got %v", err)
	}

	out, err = runWF(t, wsDir, "round", "new", "-title", "initial review")
	noErr(t, err, "round new")
	mustContain(t, out, "round 1 opened (full), rounds/r1/findings.md")

	out, err = runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze r1")
	mustContain(t, out, "frozen round 1", "gate-r1", "artifacts/context.md", "artifacts/script.md",
		"tool   wf (devel)", "cites  1 anchored / 2 citation(s)")

	out, err = runWF(t, wsDir, "status")
	noErr(t, err, "status")
	mustContain(t, out, "round  1", "artifacts (2)", "frozen rounds: gate-r1", "tree clean", "tool   wf (devel)")

	out, err = runWF(t, wsDir, "verify")
	noErr(t, err, "verify clean")
	mustContain(t, out, "## verify round 1", "tag gate-r1, 2 artifact(s)", "— clean")
	mustNotContain(t, out, "MISMATCH")

	// Drift before the target moves: nothing to measure, and the check says so
	// instead of printing an empty green report.
	out, err = runWF(t, wsDir, "check", "drift")
	noErr(t, err, "drift still")
	mustContain(t, out, "## drift — target still at", "nothing to re-read")

	// A batch with one matching and one mis-counted edit must reject WHOLE. The
	// measured failure this tool grew from is the matching half landing anyway.
	badEdits := filepath.Join(tmp, "edits-bad.json")
	write(t, badEdits, `[
  {"file": "artifacts/script.md", "old": "stays a pure", "new": "remains a pure", "note": "matches, must still not land"},
  {"file": "artifacts/context.md", "old": "pure function", "new": "total function", "count": 2, "note": "declared twice, present once"}
]`)
	out, err = runWF(t, wsDir, "apply", "-f", badEdits)
	wantFindings(t, err, "apply rejected")
	mustContain(t, out, "[0] ok", "[1] FAIL", "expected 2 occurrence(s), found 1", "batch REJECTED — nothing was written")

	// Byte-for-byte proof nothing landed: the hashes still match the freeze.
	out, err = runWF(t, wsDir, "verify")
	noErr(t, err, "verify after rejection")
	mustNotContain(t, out, "MISMATCH")

	edits := filepath.Join(tmp, "edits.json")
	write(t, edits, `[
  {"file": "artifacts/context.md", "old": "never a branch inside it", "new": "not a branch inside it", "count": 1, "note": "soften the wording"},
  {"file": "artifacts/script.md", "old": "never a branch inside it", "new": "not a branch inside it", "count": 1, "note": "same rule, second copy"}
]`)
	out, err = runWF(t, wsDir, "apply", "-f", edits)
	noErr(t, err, "apply")
	mustContain(t, out, "[0] ok", "[1] ok", "committed ", "(2 file(s))")
	// The apply commit's full sha, for the findings table below: the closure
	// checklist must resolve a real commit, and the full form dodges the
	// (unlikely) all-letter short sha the digit rule would refuse to trust.
	applySha, err := gitx.Repo{Dir: wsDir}.Head()
	noErr(t, err, "apply sha")

	// Both checks in one pass. The cite header must always carry the unverified
	// count — a green run over unanchored citations is not coverage and must not
	// read as such. The dup group is exact and committed together, so it is
	// reported but does not fail the check.
	out, err = runWF(t, wsDir, "check")
	noErr(t, err, "check")
	mustContain(t, out,
		"## cite — 2 citation(s), 1 anchored (1 unverified beyond range), 0 finding(s)",
		"1 citation-like reference(s) to extensionless tracked file(s) — NOT verified",
		"[Makefile:2]",
		"## dup — 2 statement(s), 1 cross-file group(s), 0 divergent",
		"sim 1.00",
		"## rules — 2 marker(s), 1 id(s), 1 compared, 0 conflict(s)")
	mustNotContain(t, out, "DIVERGENT")

	// -require-anchor turns the unanchored citation into a finding, and -show
	// prints the cited target lines so the judgement call it cannot make stays
	// cheap for the reviewer who can.
	out, err = runWF(t, wsDir, "check", "cite", "-show", "-require-anchor")
	wantFindings(t, err, "check cite -require-anchor")
	mustContain(t, out, "citation without #anchor", "[src/main.go:2]",
		"src/main.go:3| func NeedleFn() {}")

	// The target moves. Every line number the frozen artifacts cite is now a
	// claim about a revision that is gone: a line inserted on top shifts both
	// citations, and the NeedleFn line itself is edited under one of them.
	write(t, filepath.Join(target, "src", "main.go"), "// moved\npackage main\n\nfunc NeedleFn() { panic(\"x\") }\n")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-q", "-m", "target moves")

	// Drift names WHICH citations the move invalidated, and how: the cited
	// func line changed (re-read), the empty line merely shifted (re-aim).
	out, err = runWF(t, wsDir, "check", "drift")
	wantFindings(t, err, "drift after move")
	mustContain(t, out,
		"2 citation(s), 1 edited, 1 shifted, 0 gone, 0 unresolved, 0 clean",
		"artifacts/context.md:5: edited: src/main.go:3 changed since the freeze",
		"artifacts/script.md:5: shifted: src/main.go:2 is now 3 (+1)")

	// Exactly three mismatches: both artifact hashes and the target ref. The
	// apply commit past the tag is a note, never a mismatch — a verify that
	// failed on bookkeeping commits would teach the operator to skip the
	// bookkeeping the tool exists for.
	out, err = runWF(t, wsDir, "verify")
	wantFindings(t, err, "verify mismatches")
	mustContain(t, out, "## verify round 1", "note: HEAD is", "past tag gate-r1",
		"artifacts/context.md: sha256", "artifacts/script.md: sha256",
		"target is at", "3 mismatch(es)", "Do not launch")
	if n := strings.Count(out, "MISMATCH"); n != 3 {
		t.Fatalf("want exactly 3 MISMATCH lines, got %d:\n%s", n, out)
	}

	// A record whose closed-by names a commit that does not exist must reject
	// before writing: a landed lie briefs the next round with fiction.
	bogus := filepath.Join(tmp, "r1-bogus.md")
	write(t, bogus, `| # | level | finding | closed by |
|---|-------|---------|-----------|
| 1 | BLOCKER | stale copy | 1234567890abcdef |
`)
	if _, err = runWF(t, wsDir, "round", "record", bogus); err == nil || !strings.Contains(err.Error(), "not a commit in this GATE WORKSPACE") {
		t.Fatalf("bogus record: want 'not a commit in this GATE WORKSPACE', got %v", err)
	}

	// Findings are recorded when FILED, before the fixes — an open blocker is
	// legal at record time.
	recorded := filepath.Join(tmp, "r1-findings.md")
	write(t, recorded, `# Round 1 — findings

| # | level | finding | closed by |
|---|-------|---------|-----------|
| 1 | BLOCKER | stale copy of the composition rule in script.md | |
`)
	out, err = runWF(t, wsDir, "round", "record", recorded)
	noErr(t, err, "round record")
	mustContain(t, out, "recorded rounds/r1/findings.md",
		"findings: 1 row(s), 1 blocker(s); closed by commit 0, by prose 0, open 1",
		"committed ")

	out, err = runWF(t, wsDir, "round", "new", "-title", "delta review", "-kind", "delta")
	noErr(t, err, "round new r2")
	mustContain(t, out, "round 2 opened (delta), rounds/r2/findings.md")

	// The anchor ratchet: dropping the one anchor must surface at the next
	// freeze as a fall against the previous round's share, not pass as just
	// another green freeze.
	// The strip batch uses the plain-text block format — the quoting-hostile
	// content (# and backticks) goes in verbatim, no escaping.
	strip := filepath.Join(tmp, "edits-strip.txt")
	write(t, strip, "=== artifacts/context.md  count=1  note=drop the anchor\n"+
		"<<<<\n`src/main.go:3#NeedleFn`\n====\n`src/main.go:3`\n>>>>\n")
	out, err = runWF(t, wsDir, "apply", "-f", strip)
	noErr(t, err, "apply strip")
	mustContain(t, out, "[0] ok", "committed ")

	// The open round's virgin scaffold must count as ZERO findings — the
	// measured miss: an operator's awk counted the placeholder row and closed
	// the gate on 99/27 against a true 98/26.
	out, err = runWF(t, wsDir, "stats")
	noErr(t, err, "stats mid-run")
	mustContain(t, out,
		// Round 1's window IS the authoring commit, so the line leads with the
		// first review and the counts follow it.
		"round 1 [gate-r1, full] — FIRST REVIEW of an artifact ADDED in this window (a descent before it says nothing about it); 1 finding(s), 1 blocker(s); closed: 0 commit, 0 prose, 1 open",
		"round 2 [open, delta] — 0 finding(s), 0 blocker(s); closed: 0 commit, 0 prose, 0 open; 2 apply batch(es), 3 edit(s)",
		"no [fix-induced] markers; the self-inflicted share was not measured")

	// The prescribed flow freezes a round right after opening it, so the frozen
	// round's own findings are always a blank scaffold — the blockers that must
	// be closed by now are the CLOSED round's (r1), and freeze must read that
	// file, not pass vacuously over the scaffold.
	if _, err = runWF(t, wsDir, "freeze"); err == nil || !strings.Contains(err.Error(), "round 1: 1 blocker(s) without closed by") {
		t.Fatalf("freeze over the closed round's open blocker: want refusal naming round 1, got %v", err)
	}
	write(t, recorded, `# Round 1 — findings

| # | level | finding | closed by |
|---|-------|---------|-----------|
| 1 | BLOCKER | stale copy of the composition rule in script.md | `+applySha+` |
`)
	out, err = runWF(t, wsDir, "round", "record", recorded, "-round", "1")
	noErr(t, err, "re-record r1 closures")
	mustContain(t, out, "closed by commit 1, by prose 0, open 0")

	// Same contract on the OPEN round, for the operator who records into it
	// before freezing.
	r2open := filepath.Join(tmp, "r2-open.md")
	write(t, r2open, `| # | level | finding | closed by |
|---|-------|---------|-----------|
| 1 | BLOCKER | delta briefing was verified by hand only | |
`)
	out, err = runWF(t, wsDir, "round", "record", r2open)
	noErr(t, err, "record r2 open")
	mustContain(t, out, "open 1")
	if _, err = runWF(t, wsDir, "freeze"); err == nil || !strings.Contains(err.Error(), "round 2: 1 blocker(s) without closed by") {
		t.Fatalf("freeze over the open round's open blocker: want refusal naming round 2, got %v", err)
	}

	// A prose closure is a deliberate escape (defer, decision) — it freezes,
	// but the note keeps it from reading as a verified commit.
	r2closed := filepath.Join(tmp, "r2-closed.md")
	write(t, r2closed, `| # | level | finding | closed by |
|---|-------|---------|-----------|
| 1 | BLOCKER | delta briefing was verified by hand only [fix-induced] | deferred to the live mocker run |
`)
	out, err = runWF(t, wsDir, "round", "record", r2closed)
	noErr(t, err, "record r2 closed")
	mustContain(t, out, "by prose 1")

	out, err = runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze r2")
	mustContain(t, out, "frozen round 2", "gate-r2", "1 closure(s) not naming a commit",
		"cites  0 anchored / 2 citation(s)",
		"anchored 0/2, down from 1/2 (round 1)")

	// verify must pick the NEWEST frozen round on its own, and round 2 was
	// frozen against the moved target, so the run is clean again.
	out, err = runWF(t, wsDir, "verify")
	noErr(t, err, "verify r2")
	mustContain(t, out, "## verify round 2", "tag gate-r2, 2 artifact(s)", "— clean")
	mustNotContain(t, out, "MISMATCH")

	// A stray bookkeeping file refuses the launch like any dirt, but the
	// verdict must say the artifacts themselves still match the freeze.
	stray := filepath.Join(wsDir, "rounds", "r2", "stray.txt")
	write(t, stray, "reviewer scratch\n")
	out, err = runWF(t, wsDir, "verify")
	wantFindings(t, err, "verify bookkeeping dirt")
	mustContain(t, out, "uncommitted: ?? rounds/r2/stray.txt",
		"the artifacts match the freeze — every mismatch above is uncommitted round bookkeeping")
	if err := os.Remove(stray); err != nil {
		t.Fatal(err)
	}

	// -since narrows dup to what this round touched: the rule group's copies
	// were last edited before gate-r2, so it is hidden and SAID to be hidden.
	out, err = runWF(t, wsDir, "check", "dup", "-since", "gate-r2")
	noErr(t, err, "dup -since")
	mustContain(t, out, "0 cross-file group(s)", "1 group(s) predate gate-r2, not shown")

	// Delta without flags. The base must be the round BEING CLOSED (r1), not
	// the newest tag — r2 is already frozen, and diffing against it printed an
	// empty diff indistinguishable from a round that changed nothing. The
	// findings printed are round 1's recorded file, never round 2's blank
	// scaffold ("Opened at" is scaffold text).
	out, err = runWF(t, wsDir, "delta")
	noErr(t, err, "delta")
	mustContain(t, out,
		"# delta gate-r1..HEAD",
		"## findings round 1 filed, which this delta must close",
		"stale copy of the composition rule",
		"## closures round 1 claims",
		"[1] BLOCKER closed by "+applySha[:8]+" \"apply: 2 edit(s) across 2 file(s)\" — artifacts/context.md, artifacts/script.md",
		"## artifact diff",
		"artifacts/context.md",
		"artifacts/script.md",
		"never a branch inside it",
		"not a branch inside it")
	mustNotContain(t, out, "Opened at", "freeze.json")

	// A hand-named base must still brief with the findings of the round that
	// tag froze, not the open one — RoundOfTag is what recovers the number.
	out, err = runWF(t, wsDir, "delta", "-from", "gate-r1")
	noErr(t, err, "delta -from")
	mustContain(t, out, "# delta gate-r1..HEAD",
		"## findings round 1 filed, which this delta must close",
		"stale copy of the composition rule")

	// -save banks the brief into the open round's bookkeeping — the hand-saved
	// untracked delta.txt was the very stray file verify learned to name.
	out, err = runWF(t, wsDir, "delta", "-save")
	noErr(t, err, "delta -save")
	mustContain(t, out, "saved rounds/r2/brief.md", "committed ")
	brief, err := os.ReadFile(filepath.Join(wsDir, "rounds", "r2", "brief.md"))
	noErr(t, err, "read brief")
	mustContain(t, string(brief), "# delta gate-r1..HEAD", "## closures round 1 claims", "## artifact diff")
	out, err = runWF(t, wsDir, "delta", "-save")
	noErr(t, err, "delta -save unchanged")
	mustContain(t, out, "unchanged since the last save")

	// A first draft written straight into artifacts/ is adopted, not refused:
	// refusing it forced the hand `git commit` this command exists to remove.
	write(t, filepath.Join(wsDir, "artifacts", "notes.md"), "# Notes\n")
	out, err = runWF(t, wsDir, "add", filepath.Join(wsDir, "artifacts", "notes.md"))
	noErr(t, err, "add in place")
	mustContain(t, out, "added artifacts/notes.md", "already in place", "committed ")

	// Stats come from the ledger and git, not an operator's awk — the scaffold
	// placeholder of an open round must not count as a finding.
	out, err = runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out,
		"## stats — workspace demo, 2 round(s)",
		"round 1 [gate-r1, full] — FIRST REVIEW of an artifact ADDED in this window (a descent before it says nothing about it); 1 finding(s), 1 blocker(s); closed: 1 commit, 0 prose, 0 open; 0 apply batch(es)",
		"round 2 [gate-r2, delta] — 1 finding(s), 1 blocker(s); closed: 0 commit, 1 prose, 0 open; 2 apply batch(es), 3 edit(s) / 6 changed line(s) (16.7 finding(s) per 100 changed line(s)); artifact 11 line(s) (+0); fix-induced 1",
		"total — 2 finding(s), 2 blocker(s)",
		"fix-induced — 1 of 2 finding(s)",
		"cost — 3 edit(s) applied over 6 changed line(s), 0.67 finding(s)/edit, 33.3 per 100 changed line(s) across the gate")

	// A tooling round is its own kind: its blocker must not read as reviewer
	// evidence, or it restarts the stop rule's clock (the P2e distortion).
	out, err = runWF(t, wsDir, "round", "new", "-title", "tooling sweep", "-kind", "tool")
	noErr(t, err, "round new tool")
	mustContain(t, out, "round 3 opened (tool)")
	r3 := filepath.Join(tmp, "r3-findings.md")
	write(t, r3, `| # | level | finding | closed by |
|---|-------|---------|-----------|
| 1 | BLOCKER | dry runner kept a harness-only binding alive | fixed upstream in wfdry |
`)
	_, err = runWF(t, wsDir, "round", "record", r3)
	noErr(t, err, "record r3")
	out, err = runWF(t, wsDir, "stats")
	noErr(t, err, "stats with tool round")
	mustContain(t, out,
		// notes.md was added AFTER gate-r2, so it lands in round 3's window and
		// the round is the first to review it — the mocker A3 shape, in the
		// prescribed sequence's own fixture.
		// A tool round dispatched no reviewer by declaration, so the line must
		// not claim a first REVIEW of the artifact its window added; what is
		// true of the window still is.
		"round 3 [open, tool] — an artifact was ADDED in this window and NO reviewer was dispatched — nothing here reviewed it; 1 finding(s), 1 blocker(s)",
		"reviewer rounds — 2 finding(s), 2 blocker(s); tool rounds — 1 finding(s), 1 blocker(s)",
		"the stop rule reads the REVIEWER descent")
}
