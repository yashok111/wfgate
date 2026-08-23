package main

import (
	"path/filepath"
	"testing"
)

// The per-edit body line is the meter's third and last source, and the only one
// left in a workspace older than the trailer whose operator wrote every subject
// by hand — p2d (186 edits) and p2f (150 over 27 batches) carry neither of the
// other two. It counts ONLY when every non-empty body line is one: a hand commit
// that opens its body with "- path: note" is not a batch.
func TestApplyBodyEditsCountsOnlyAWholeBatchBody(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want int
	}{
		{"a real batch body", "script-r3: the escaped pipe\n\n" +
			"- artifacts/context.md: closes r12-1\n" +
			"- artifacts/context.md: closes r12-3\n", 2},
		{"one prose line among them", "fix by hand\n\n" +
			"- artifacts/context.md: closes r12-1\n" +
			"and then I reworded the paragraph\n", 0},
		{"subject only", "round 4: open (delta) — the r3 fixes\n", 0},
		{"prose that merely opens like one", "notes\n\n" +
			"- artifacts/context.md: what I still owe\n" +
			"\nSee the round-3 findings for why.\n", 0},
	}
	for _, c := range cases {
		if got := applyBodyEdits(c.msg); got != c.want {
			t.Errorf("%s: want %d, got %d", c.name, c.want, got)
		}
	}
}

// A round's apply window needs BOTH ends pinned. Measured on a live gate: three
// rounds, none of them frozen in sequence, each printed the workspace total —
// ratios of 0.88 and 0.15 fabricated from the same 26 edits. The numbers are
// omitted now, and the reason is named.
func TestStatsOmitsAnUnattributableWindow(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=reword\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", edits); err != nil {
		t.Fatal(err)
	}
	// Two more rounds opened without freezing either predecessor — exactly the
	// shape the live gate was in.
	if _, err := runWF(t, wsDir, "round", "new", "-title", "second", "-kind", "tool"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "third", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}

	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out,
		"round 1 [open, full] — 0 finding(s), 0 blocker(s); closed: 0 commit, 0 prose, 0 open; 0 apply batch(es), apply window not attributable",
		"round 2 [open, tool] — 0 finding(s), 0 blocker(s); closed: 0 commit, 0 prose, 0 open; 0 apply batch(es), apply window not attributable",
		"3 round(s) have no attributable apply window")
	// A batch WAS applied. "No apply commit was found" is a stronger claim than
	// "this round's window cannot be attributed", and only the second is true.
	mustNotContain(t, out, "the cost meter has NO population")
	mustNotContain(t, out, "finding(s)/edit")
}

// The timing and the cost meter must agree about what an apply commit IS, or a
// round's "fix" segment would be measured over commits the meter did not count.
// One classifier, asserted here against all three of its sources.
func TestIsApplyAgreesWithEverySource(t *testing.T) {
	cases := []struct {
		name, subj, msg string
		want            bool
	}{
		{"trailer", "r3: close the seam finding",
			"r3: close the seam finding\n\n- a.md: x\n\nwf-apply: 4 edit(s) in 1 file(s)\n", true},
		{"default subject", "apply: 3 edit(s) across 1 file(s)",
			"apply: 3 edit(s) across 1 file(s)\n", true},
		{"body only", "script-r3: the escaped pipe",
			"script-r3: the escaped pipe\n\n- artifacts/context.md: closes r12-1\n", true},
		{"a round opening", "round 4: open (delta) — the r3 fixes",
			"round 4: open (delta) — the r3 fixes\n", false},
		{"a hand commit that opens like a batch", "notes",
			"notes\n\n- artifacts/context.md: what I owe\n\nand the rest in prose\n", false},
	}
	for _, c := range cases {
		if got := isApply(c.msg, c.subj); got != c.want {
			t.Errorf("%s: want %v, got %v", c.name, c.want, got)
		}
	}
}

// -time is opt-in: the round line and the summary are the same without it. The
// wall clock is derived from git, so it costs a second walk and nobody asking
// for findings should pay for it.
func TestStatsTimeIsOptIn(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	// The round must APPLY something, or its time carries no split and the
	// leak this test exists to catch would not be visible in the round line.
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=reword\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", edits); err != nil {
		t.Fatal(err)
	}

	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustNotContain(t, out, "median round", "time — ", " min (review ")

	out, err = runWF(t, wsDir, "stats", "-time")
	noErr(t, err, "stats -time")
	mustContain(t, out,
		"authoring ",
		"(init -> first artifact)",
		"1 round(s) over ",
		"median round ",
		"read the median")
}
