package main

import (
	"os"
	"path/filepath"
	"testing"
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
