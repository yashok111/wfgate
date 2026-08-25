package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.sumka.site/yakov/wfgate/internal/gitx"
)

// The gate ships its own work mid-run and the target moves under a document
// that was frozen rounds ago. Measured on mocker slice A: 26 citations in the
// decision document pointed at lines that had drifted by a few, every producer
// of the next artifact read them as correct, and the operator found out at
// close — the check existed and was run only while the cited document was the
// artifact under review.
//
// A note, never a refusal: a round whose whole purpose is re-aiming these
// citations must still freeze.
func TestFreezeNamesCitationsThatNoLongerResolve(t *testing.T) {
	wsDir := newGate(t)
	target := filepath.Join(filepath.Dir(wsDir), "target")

	// The target ships a slice: two lines inserted above the cited one, so
	// `src/main.go:3#NeedleFn` now names a line that exists and is wrong — the
	// shape a range check is powerless against and an anchor catches.
	write(t, filepath.Join(target, "src", "main.go"), "package main\n\n// shipped\n// mid-gate\nfunc NeedleFn() {}\n")
	gitIn(t, target, "commit", "-qam", "ship A1")

	out, err := runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze")
	mustContain(t, out,
		"citation(s) do NOT resolve",
		"reviewers of this freeze will read them as written",
		"nearest occurrence is line 5")
}

// A freeze whose citations all resolve says nothing about them — a note that
// fires on correct work is a note the operator stops reading.
func TestFreezeIsSilentWhenTheCitationsHold(t *testing.T) {
	wsDir := newGate(t)
	out, err := runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze")
	mustNotContain(t, out, "do NOT resolve")
}

// The operator stops using `wf apply` mid-gate and nothing says a word:
// measured over seven rounds and 35 findings of a live gate, found at close.
// Freeze knows the round's commit range, so it is the moment the loss is still
// one round old.
func TestFreezeNamesARoundEditedByHand(t *testing.T) {
	wsDir := newGate(t)
	art := filepath.Join(wsDir, "artifacts", "decisions.md")
	body, err := os.ReadFile(art)
	if err != nil {
		t.Fatal(err)
	}
	write(t, art, string(body)+"\nA paragraph a script replaced in place.\n")
	gitIn(t, wsDir, "commit", "-qam", "r1: the fixes, by hand")

	out, err := runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze")
	mustContain(t, out,
		"changed an artifact without `wf apply`",
		"contributes NO edit count")
}

// ...and it stays quiet when the round went through `wf apply`, and when the
// only artifact commit is an adoption: `wf add` is not an edit batch.
func TestFreezeIsSilentWhenTheRoundUsedApply(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=reword\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", edits); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(tmp, "context.md")
	write(t, second, "# Context\n\nDerived, and adopted rather than edited.\n")
	if _, err := runWF(t, wsDir, "add", second); err != nil {
		t.Fatal(err)
	}

	out, err := runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze")
	mustNotContain(t, out, "without `wf apply`")
}

// `wf stats` printed "0 apply batch(es)" for each of seven rounds of a live gate
// without a word, because the whole-gate note fires only when the meter has NO
// population at all. A gate that used `wf apply` for seventeen rounds and
// stopped has a population, a headline ratio, and seven rounds the ratio never
// saw. Same for the fix-induced marker, whose absence is unmeasured and not zero.
func TestStatsNamesTheRoundsItCouldNotMeasure(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	// Round 1 goes through `wf apply` and carries a marked finding.
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=reword\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", edits); err != nil {
		t.Fatal(err)
	}
	ledger(t, wsDir, 1, "| 1 | note | [fix-induced] the reword broke the anchor | prose: re-aimed |\n")
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}

	// Round 2 is edited by hand and its findings are unmarked — both holes, in
	// the round where the live gate opened them.
	if _, err := runWF(t, wsDir, "round", "new", "-title", "second", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	art := filepath.Join(wsDir, "artifacts", "decisions.md")
	body, err := os.ReadFile(art)
	if err != nil {
		t.Fatal(err)
	}
	write(t, art, string(body)+"\nEdited in place by a script.\n")
	gitIn(t, wsDir, "commit", "-qam", "r2: the fixes, by hand")
	ledger(t, wsDir, 2, "| 1 | note | a finding nobody marked | prose: fixed |\n")

	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out,
		"1 round(s) with findings applied no `wf apply` batch (2)",
		"prices only the rounds that used it",
		"1 round(s) with findings carry NO [fix-induced] marker (2)",
		"lower bound")
	// The gate DOES have a population — the stronger claim must not appear.
	mustNotContain(t, out, "the cost meter has NO population")
}

// Round numbers print as ranges: "18-25" is a fact an operator acts on.
func TestRoundListPrintsRanges(t *testing.T) {
	cases := []struct {
		in   []int
		want string
	}{
		{[]int{18, 19, 20, 21, 22, 23, 24, 25}, "18-25"},
		{[]int{3}, "3"},
		{[]int{3, 4}, "3, 4"},
		{[]int{1, 2, 3, 7, 9, 10, 11}, "1-3, 7, 9-11"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := roundList(c.in); got != c.want {
			t.Errorf("roundList(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// B6, mocker slice A: rounds 21-24 each produced exactly ONE blocker, and every
// one of them was the same enumeration floor rewritten. The stop rule handled it
// correctly — it kept the gate open, and each round closed a real escape — but a
// run of one-blocker rounds is the signature of a guard asking the wrong KIND of
// question, and nothing named that signature.
func TestStatsNamesAFlatDescent(t *testing.T) {
	wsDir := newGate(t)
	for n := 1; n <= 4; n++ {
		ledger(t, wsDir, n, "| 1 | BLOCKER | clause 8's floor over artifacts/report.md still counts slots | prose: rewritten |\n")
		if _, err := runWF(t, wsDir, "freeze"); err != nil {
			t.Fatalf("freeze r%d: %v", n, err)
		}
		if n < 4 {
			if _, err := runWF(t, wsDir, "round", "new", "-title", "next", "-kind", "delta"); err != nil {
				t.Fatal(err)
			}
		}
	}
	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out,
		"rounds 1-4 each produced exactly ONE blocker",
		"every blocker in that run cites artifacts/report.md")
}

// A descent that actually descends is not the signal, and a tool round does not
// break or extend the run — the stop rule reads reviewer evidence.
func TestStatsIsSilentOnADescendingGate(t *testing.T) {
	wsDir := newGate(t)
	rows := []string{
		"| 1 | BLOCKER | one |  x |\n| 2 | BLOCKER | two | x |\n| 3 | BLOCKER | three | x |\n",
		"| 1 | BLOCKER | one | x |\n| 2 | BLOCKER | two | x |\n",
		"| 1 | BLOCKER | one | x |\n",
	}
	for n, row := range rows {
		ledger(t, wsDir, n+1, row)
		if _, err := runWF(t, wsDir, "freeze"); err != nil {
			t.Fatalf("freeze r%d: %v", n+1, err)
		}
		if n < len(rows)-1 {
			if _, err := runWF(t, wsDir, "round", "new", "-title", "next", "-kind", "delta"); err != nil {
				t.Fatal(err)
			}
		}
	}
	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustNotContain(t, out, "each produced exactly ONE blocker")
}

// A3: eighteen markers, eighteen ids, zero comparisons — the note said so and
// was still missed, because it printed under a line whose other three numbers
// read like a pass.
func TestRulesHeaderSaysItComparedNothing(t *testing.T) {
	wsDir := newGate(t)
	art := filepath.Join(wsDir, "artifacts", "decisions.md")
	body, err := os.ReadFile(art)
	if err != nil {
		t.Fatal(err)
	}
	write(t, art, string(body)+"\n[R1] A rule stated once, in one place.\n[R2] Another rule, also alone.\n")
	gitIn(t, wsDir, "commit", "-qam", "markers")

	out, err := runWF(t, wsDir, "check", "rules")
	noErr(t, err, "check rules")
	mustContain(t, out, "## rules — 2 marker(s), 2 id(s), 0 compared, 0 conflict(s) — NOT A PASS, nothing was compared")
}

// Every run of three, not the first longest one. The gate this note was derived
// from has three runs of exactly three rounds, so keeping the longest kept the
// EARLIEST: it would have named the decision gate's rounds and attached the
// script gate's story to them.
func TestStatsNamesEveryFlatRun(t *testing.T) {
	wsDir := newGate(t)
	// one, one, one, three, one, one, one — two runs of three around a break.
	rows := []string{
		"| 1 | BLOCKER | a floor over artifacts/report.md | prose: x |\n",
		"| 1 | BLOCKER | a floor over artifacts/report.md | prose: x |\n",
		"| 1 | BLOCKER | a floor over artifacts/report.md | prose: x |\n",
		"| 1 | BLOCKER | one | x |\n| 2 | BLOCKER | two | x |\n| 3 | BLOCKER | three | x |\n",
		"| 1 | BLOCKER | a floor over artifacts/report.md | prose: x |\n",
		"| 1 | BLOCKER | a floor over artifacts/report.md | prose: x |\n",
		"| 1 | BLOCKER | a floor over artifacts/report.md | prose: x |\n",
	}
	for n, row := range rows {
		ledger(t, wsDir, n+1, row)
		if _, err := runWF(t, wsDir, "freeze"); err != nil {
			t.Fatalf("freeze r%d: %v", n+1, err)
		}
		if n < len(rows)-1 {
			if _, err := runWF(t, wsDir, "round", "new", "-title", "next", "-kind", "delta"); err != nil {
				t.Fatal(err)
			}
		}
	}
	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out,
		"rounds 1-3 each produced exactly ONE blocker",
		"rounds 5-7 each produced exactly ONE blocker")
}

// sharedPath's "one region" claim needs exactly one shared path. Two is a run
// touching a PAIR of files, and naming one of them — whichever the map iteration
// reached first — is a claim about a region that does not exist.
func TestSharedPathNeedsExactlyOne(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"one shared", []string{"a/one.go is wrong", "still a/one.go"}, "a/one.go"},
		{"two shared", []string{"a/one.go and b/two.go", "b/two.go beside a/one.go"}, ""},
		{"none shared", []string{"a/one.go", "b/two.go"}, ""},
		// The loose form matched the prose it reads. Four rounds whose findings
		// all say "e.g." made the note read "every blocker cites e.g".
		{"prose is not a path", []string{"the floor, e.g. the count", "again, e.g. here", "e.g. once more"}, ""},
		{"an abbreviation beside a real path", []string{"e.g. a/one.go here", "i.e. a/one.go there"}, "a/one.go"},
	}
	for _, c := range cases {
		if got := sharedPath(c.in); got != c.want {
			t.Errorf("%s: sharedPath = %q, want %q", c.name, got, c.want)
		}
	}
}

// A round whose ledger cannot be read is not a round with no findings. Both
// printed `0 finding(s), 0 blocker(s)` and both fed the totals.
func TestStatsNamesARoundWithNoLedger(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 1 | BLOCKER | one | x |\n")
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "next", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(wsDir, "rounds", "r2", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out, "have no readable findings.md (2)", "ABSENT measurement, not a measured zero")
}

// A tooling round carries no reviewer evidence, so it neither breaks a run of
// one-blocker rounds nor counts as one. Asserted rather than asserted-in-a-
// comment: the exemption survived deletion with every bar green.
func TestAToolRoundDoesNotBreakAFlatRun(t *testing.T) {
	wsDir := newGate(t)
	kinds := []string{"delta", "delta", "tool", "delta"}
	rows := []string{
		"| 1 | BLOCKER | a floor over artifacts/report.md | prose: x |\n",
		"| 1 | BLOCKER | a floor over artifacts/report.md | prose: x |\n",
		"| 1 | BLOCKER | the launch died, no reviewer ran | prose: x |\n",
		"| 1 | BLOCKER | a floor over artifacts/report.md | prose: x |\n",
	}
	for n, row := range rows {
		ledger(t, wsDir, n+1, row)
		if _, err := runWF(t, wsDir, "freeze"); err != nil {
			t.Fatalf("freeze r%d: %v", n+1, err)
		}
		if n < len(rows)-1 {
			if _, err := runWF(t, wsDir, "round", "new", "-title", "next", "-kind", kinds[n+1]); err != nil {
				t.Fatal(err)
			}
		}
	}
	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	// Rounds 1, 2 and 4 are the run; round 3 is a tool round in the middle of it
	// and is neither counted nor a break.
	mustContain(t, out, "rounds 1, 2, 4 each produced exactly ONE blocker")
}

// "This round applied no batch" and "this round's window cannot be attributed"
// are different claims, and only the first accuses the operator of anything. The
// guard that keeps them apart survived deletion with every bar green.
func TestStatsDoesNotAccuseAnUnattributableRound(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	// One real batch, so the workspace has a cost-meter population at all and
	// the per-round naming is armed.
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=reword\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", edits); err != nil {
		t.Fatal(err)
	}
	ledger(t, wsDir, 1, "| 1 | BLOCKER | one | x |\n")
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	// Round 2 is opened, given findings, and never frozen; round 3 is opened on
	// top of it. Round 2 therefore has no upper bound and no window at all,
	// while round 3 is bounded below by nothing it can name.
	for _, n := range []int{2, 3} {
		if _, err := runWF(t, wsDir, "round", "new", "-title", "next", "-kind", "delta"); err != nil {
			t.Fatal(err)
		}
		ledger(t, wsDir, n, "| 1 | BLOCKER | one | x |\n")
	}
	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out, "apply window not attributable")
	// A window nobody can attribute is an unknown. Printing it beside the rounds
	// that really applied nothing turns an unknown into an accusation, and the
	// operator cannot tell which of the two the tool means.
	mustNotContain(t, out, "applied no `wf apply` batch")
}

// The larger population by far: a corpus nobody marked compares nothing either,
// and printed `0 marker(s), 0 id(s), 0 compared, 0 conflict(s)` — four numbers
// that read like a pass, with no qualifier anywhere on the line.
func TestRulesHeaderSaysItComparedNothingOnAnUnmarkedCorpus(t *testing.T) {
	wsDir := newGate(t)
	out, err := runWF(t, wsDir, "check", "rules")
	noErr(t, err, "check rules")
	mustContain(t, out, "0 marker(s), 0 id(s), 0 compared, 0 conflict(s) — NOT A PASS, nothing was compared")
}

// mocker A3: nine rounds of blocker descent on the decision document read like
// convergence, the orchestration script was then ADDED to the workspace, and
// round 10 produced 38 findings and 12 blockers over an artifact nobody had
// reviewed. The tool knew — it said so in a parenthesis AFTER the counts, and
// the operator read the counts.
func TestStatsSaysAFirstReviewBeforeTheCounts(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "the script arrives", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	// The A3 shape exactly: a second artifact enters an open gate.
	script := filepath.Join(filepath.Dir(wsDir), "gate.mjs")
	write(t, script, "const P = `\nThe two routes keep their existing envelope beside the new one.\n`\n")
	if _, err := runWF(t, wsDir, "add", script); err != nil {
		t.Fatal(err)
	}
	reviewedRound(t, wsDir, 2)

	out, err := runWF(t, wsDir, "stats")
	if err != nil {
		t.Fatal(err)
	}
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "round 2 [") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no round 2 line in:\n%s", out)
	}
	at := strings.Index(line, "FIRST REVIEW")
	if at < 0 {
		t.Fatalf("round 2 reviewed an artifact for the first time and must say so: %s", line)
	}
	// BEFORE the counts is the whole finding: the parenthesis after them was
	// already there and was read as a footnote.
	if c := strings.Index(line, "finding(s)"); at > c {
		t.Fatalf("the first review must be stated before the counts: %s", line)
	}
}

// The other half of the same population: an ordinary delta round must not carry
// the mark, or it stops meaning anything.
func TestStatsDoesNotCallEveryRoundAFirstReview(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "the fixes", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	edits := filepath.Join(filepath.Dir(wsDir), "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=reword\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", edits); err != nil {
		t.Fatal(err)
	}
	reviewedRound(t, wsDir, 2)

	out, err := runWF(t, wsDir, "stats")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "round 2 [") && strings.Contains(l, "FIRST REVIEW") {
			t.Fatalf("round 2 added nothing: %s", l)
		}
	}
}

// A round that records a LAUNCH has the TARGET's commit in hand and nothing
// else, and the refusal used to name neither repository — so it read as the
// tool being unable to see a commit that obviously exists. The rule stands (a
// closure names the workspace commit that landed the fix); what changed is that
// the error says where it looked and what it found.
func TestRecordSaysWhichRepositoryTheShaIsMissingFrom(t *testing.T) {
	wsDir := newGate(t)
	target := filepath.Join(filepath.Dir(wsDir), "target")
	raw, err := gitOut(t, target, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	head := strings.TrimSpace(raw)

	ledger(t, wsDir, 1, "| 1 | BLOCKER | the slice shipped | "+head[:8]+" |\n")
	out, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	if err == nil {
		t.Fatalf("a sha the workspace does not have must be refused:\n%s", out)
	}
	msg := err.Error()
	if !strings.Contains(msg, "GATE WORKSPACE") || !strings.Contains(msg, wsDir) {
		t.Fatalf("the error must name the repository it looked in: %s", msg)
	}
	if !strings.Contains(msg, "TARGET repository") || !strings.Contains(msg, target) {
		t.Fatalf("this sha IS a commit in the target, and the error must say so: %s", msg)
	}
}

// The other half: a sha that is in NEITHER repository gets no invented claim
// about the target.
func TestRecordDoesNotInventATargetCommit(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 1 | BLOCKER | a claim | 1234567890abcdef |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	if err == nil {
		t.Fatal("a sha nothing has must be refused")
	}
	if strings.Contains(err.Error(), "TARGET repository") {
		t.Fatalf("that sha is in no repository at all: %s", err)
	}
}

// Round 13 of mocker A3 spans 288.2 min against a median of 45.3 because it
// CONTAINED the launch — 38 agents over 4h36m — and `wf stats -time` answered
// with "a span far above the median is a break, not work". The launch is the
// one event in a gate's life most worth timing, and it had no name.
func TestStatsTimeKeepsARunRoundOutOfTheMedian(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "round", "new", "-title", "the launch", "-kind", "run")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "round 2 opened (run)")
	scaffold, err := os.ReadFile(filepath.Join(wsDir, "rounds", "r2", "findings.md"))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, string(scaffold), "Kind: run", "its span is the run, not a break")
	reviewedRound(t, wsDir, 2)

	out, err = runWF(t, wsDir, "stats", "-time")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out,
		"round 2 [open, run]",
		"run — round 2 spans",
		"CONTAINS a run of what the gate built",
		// One round of two is outside the run, and the median is over that
		// one. The population is NOT renamed "review rounds": a tool round is
		// in this median too and always has been, and the medians published
		// from these gates were measured over it.
		"1 round(s) over", "1 run round(s) counted apart")
}

// The first segment of the split is "open to the first fix commit", and on a
// run round that window IS the run. Calling it review names the wrong activity
// in the one round whose minutes are least like a review's.
func TestStatsTimeCallsARunRoundsFirstSegmentTheRun(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "the launch", "-kind", "run"); err != nil {
		t.Fatal(err)
	}
	// A fix commit inside the round is what makes the span split at all.
	edits := filepath.Join(filepath.Dir(wsDir), "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=reword\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", edits); err != nil {
		t.Fatal(err)
	}
	reviewedRound(t, wsDir, 2)
	out, err := runWF(t, wsDir, "stats", "-time")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(out, "\n") {
		if !strings.Contains(l, "round 2 [open, run]") {
			continue
		}
		if strings.Contains(l, "(review ") {
			t.Fatalf("a run round's first segment is the run: %s", l)
		}
		if !strings.Contains(l, "(run ") {
			t.Fatalf("the split must name the run: %s", l)
		}
		return
	}
	t.Fatalf("no round 2 line in:\n%s", out)
}

// A kind the tool does not know must be refused by name, not written into a
// commit subject nothing can parse back.
func TestRoundNewRefusesAnUnknownKind(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	_, err := runWF(t, wsDir, "round", "new", "-title", "x", "-kind", "launch")
	if err == nil {
		t.Fatal("an unknown kind must be refused")
	}
	if !strings.Contains(err.Error(), "full, delta, tool or run") {
		t.Fatalf("the refusal must name the kinds: %s", err)
	}
}

// Eleven of mocker A3's thirteen rounds bank no lens: the operator started at
// round 11, and for rounds 1-10 the findings are in the ledger while the
// prompts that produced them are gone. A note, never a refusal — refusing here
// would be refusing to write down what was found.
func TestRecordAsksForTheRosterItDoesNotHave(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 1 | MAJOR | a wording this round filed |  |\n")
	out, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "lenses: NONE banked for round 1", "wf round lens")

	// And it stops asking once the roster exists.
	prompt := filepath.Join(filepath.Dir(wsDir), "citations.md")
	write(t, prompt, "Find every citation in the artifact that no longer resolves to the line it names.\n")
	if _, err := runWF(t, wsDir, "round", "lens", "-asks", "other", prompt, "-name", "citations", "-model", "opus"); err != nil {
		t.Fatal(err)
	}
	ledger(t, wsDir, 1, "| 1 | MAJOR | a wording this round filed |  |\n| 2 | MAJOR | and a second one |  |\n")
	out, err = runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	if err != nil {
		t.Fatal(err)
	}
	mustNotContain(t, out, "lenses: NONE banked")
}

// P3a: the workspace pin was written sixteen rounds before it was read, the
// reference had grown two whole sections since — the closed severity scale and
// the three questions a delta round may never drop — and every reviewer of the
// next two rounds was handed the pin as required reading. Nothing was
// misclassified, because the operator had restated the scale by hand; a
// reviewer briefed from a stale reference AGREES with the rules by coincidence,
// and the operator cannot tell that from agreement. It was found by a human
// asking "are you using the new classification?", which is not a check.
func TestRoundNewNamesAnAgedGateChecksPin(t *testing.T) {
	wsDir := newGate(t)
	pin := filepath.Join(wsDir, "gate-checks.md")
	body, err := os.ReadFile(pin)
	if err != nil {
		t.Fatal(err)
	}
	// The pin as it stood before two sections were added to the reference.
	var kept []string
	skip := false
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "## 0. ") {
			skip = true
		}
		if strings.HasPrefix(line, "## A. ") {
			skip = false
		}
		if !skip {
			kept = append(kept, line)
		}
	}
	write(t, pin, strings.Join(kept, "\n"))
	gitIn(t, wsDir, "commit", "-qam", "an older pin")

	out, err := runWF(t, wsDir, "round", "new", "-title", "the next round")
	noErr(t, err, "round new")
	mustContain(t, out,
		"is NOT the reference this binary carries",
		"the pin predates:",
		"0. The severity scale, which is closed",
		"0b. What a delta round asks")
	// The pin is RIGHT and stays: one gate reads one version of the rules.
	mustContain(t, out, "the pin is deliberate")

	// And again at the freeze, which PUBLISHES what reviewers read: the pin is
	// part of what this round was reviewed with, the way the tool version in
	// the manifest is part of what it was verified with.
	reviewedRound(t, wsDir, 1)
	reviewedRound(t, wsDir, 2)
	out, err = runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze")
	mustContain(t, out, "is NOT the reference this binary carries")
}

// A pin that is the reference says nothing. A note that fires on correct work
// is a note the operator stops reading — the `dup` lesson, 161 groups of 176.
func TestRoundNewIsSilentWhenThePinIsCurrent(t *testing.T) {
	wsDir := newGate(t)
	out, err := runWF(t, wsDir, "round", "new", "-title", "the next round")
	noErr(t, err, "round new")
	mustNotContain(t, out, "NOT the reference this binary carries")
}

// The stamp `wf init` writes above the pin is provenance, not content: a pin
// regenerated with `wf docs checks` has none, and neither spelling is drift.
func TestPinAgeIgnoresTheProvenanceStamp(t *testing.T) {
	wsDir := newGate(t)
	pin := filepath.Join(wsDir, "gate-checks.md")
	body, err := os.ReadFile(pin)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(body), "<!--") {
		t.Fatalf("wf init must stamp the pin, got %.40q", body)
	}
	// The `wf docs checks > gate-checks.md` spelling: same rules, no stamp.
	write(t, pin, strings.SplitN(string(body), "\n", 2)[1])
	gitIn(t, wsDir, "commit", "-qam", "regenerated without the stamp")
	out, err := runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze")
	mustNotContain(t, out, "NOT the reference this binary carries")
}

// P3a: `wf check closure` reached 0 of 350 claims on the first record and 26 of
// 26 after the finding text was given an artifact path. The requirement is in
// the manual and was in the tool's own output nowhere at the moment it could
// still be met — the operator is WRITING the file at record time.
func TestRoundRecordNamesClosuresThatCannotBeChecked(t *testing.T) {
	wsDir := newGate(t)
	// Two artifacts: on a one-artifact workspace the check reads the only
	// document there is, and the note would fire on every correct ledger.
	second := filepath.Join(filepath.Dir(wsDir), "notes.md")
	write(t, second, "# Notes\n\nA second artifact, so a closure has to say which one it landed in.\n")
	_, err := runWF(t, wsDir, "add", second)
	noErr(t, err, "add")
	head, err := gitx.Repo{Dir: wsDir}.Head()
	noErr(t, err, "head")

	ledger(t, wsDir, 1, ""+
		"| 1 | BLOCKER | the envelope is retyped in place |  "+head+" |\n"+
		"| 2 | MAJOR | `artifacts/notes.md:3` says the opposite |  "+head+" |\n")
	out, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")
	mustContain(t, out, "closures: 1 of 2 name no artifact path", "cannot reach them")
}

// A one-artifact workspace: `wf check closure` reads the only document there
// is, so requiring the finding to say so out loud would make this note fire on
// every correct ledger — and a note that fires on correct work stops being
// read. The `dup` lesson, 161 groups of 176.
func TestRoundRecordIsSilentOnAOneArtifactWorkspace(t *testing.T) {
	wsDir := newGate(t)
	head, err := gitx.Repo{Dir: wsDir}.Head()
	noErr(t, err, "head")
	ledger(t, wsDir, 1, "| 1 | BLOCKER | the envelope is retyped in place |  "+head+" |\n")
	out, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")
	mustNotContain(t, out, "name no artifact path")
}

// A ledger whose closures all name their artifact says nothing about them.
func TestRoundRecordIsSilentWhenEveryClosureNamesItsArtifact(t *testing.T) {
	wsDir := newGate(t)
	second := filepath.Join(filepath.Dir(wsDir), "notes.md")
	write(t, second, "# Notes\n\nA second artifact, so a closure has to say which one it landed in.\n")
	_, err := runWF(t, wsDir, "add", second)
	noErr(t, err, "add")
	head, err := gitx.Repo{Dir: wsDir}.Head()
	noErr(t, err, "head")

	ledger(t, wsDir, 1, "| 1 | MAJOR | `artifacts/notes.md:3` says the opposite |  "+head+" |\n")
	out, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")
	mustNotContain(t, out, "name no artifact path")
}

// The pin's section list is the H2 headings and nothing else. A looser prefix
// slices the document TITLE at three characters and reports the garbage as a
// section the pin predates, and it drags every H3 subsection in beside it.
func TestPinHeadingsAreTheSectionsOnly(t *testing.T) {
	doc := "# Workflow gate: known failure modes\n\nprose\n\n## A. Facts\n\n### A subsection\n\n## B. Seams\n"
	got := headings([]byte(doc))
	want := []string{"A. Facts", "B. Seams"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
	// And the comparison is over those: a pin missing one names it, a pin
	// missing none names nothing.
	pin := "# Workflow gate: known failure modes\n\n## B. Seams\n"
	if m := headingsMissing([]byte(pin), []byte(doc)); len(m) != 1 || m[0] != "A. Facts" {
		t.Fatalf("want the one missing section, got %v", m)
	}
	if m := headingsMissing([]byte(doc), []byte(doc)); len(m) != 0 {
		t.Fatalf("a pin that has every section predates nothing, got %v", m)
	}
}

// NO pin is not a current pin. Two workspaces in this project's own corpus
// predate `wf init` writing one, and silence over them would be the class this
// note exists against: an absent measurement in the register of a measured pass.
func TestPinAgeNamesAWorkspaceWithNoPinAtAll(t *testing.T) {
	wsDir := newGate(t)
	if err := os.Remove(filepath.Join(wsDir, "gate-checks.md")); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wsDir, "commit", "-qam", "a workspace from before the pin existed")
	out, err := runWF(t, wsDir, "round", "new", "-title", "the next round")
	noErr(t, err, "round new")
	mustContain(t, out, "no gate-checks.md", "nothing pins the rules")
	// It is the OTHER note's negative: an absent pin has no sections to predate.
	mustNotContain(t, out, "the pin predates:")
}
