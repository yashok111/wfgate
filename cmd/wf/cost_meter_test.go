package main

import (
	"fmt"
	"git.sumka.site/yakov/wfgate/internal/ws"
	"path/filepath"
	"strings"
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
		name, subj, msg string //nolint:unused // subj documents the message's first line
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
		// The subject is read off the message's first line, never handed in
		// beside it: a caller passing one that disagrees with the message made
		// two readings of one commit possible.
		if got := isApply(c.msg); got != c.want {
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

// The note's denominator comes from git, over the files just written and not yet
// committed. Nothing but a real repository exercises that, and the case it exists
// for is the batch of FEW edits over MANY lines: one edit of P2e rewrote 933
// lines and the per-edit meter printed it as "38.00 finding(s)/edit".
func TestApplyNotePricesLinesNotEdits(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	// A history: one small batch, and findings filed against it.
	first := filepath.Join(tmp, "first.txt")
	write(t, first, "=== artifacts/decisions.md  count=1  note=seed the history\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", first); err != nil {
		t.Fatal(err)
	}
	rows := ""
	for i := 1; i <= 10; i++ {
		rows += fmt.Sprintf("| %d | MAJOR | finding %d | prose |\n", i, i)
	}
	ledger(t, wsDir, 1, rows)
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	// Round 1's window contains the commit that CREATED the artifact, so the
	// meter refuses to predict from it (see the authoring test below). The
	// history the note reads has to come from a round that only fixed.
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "second", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	fix := filepath.Join(tmp, "fix.txt")
	write(t, fix, "=== artifacts/decisions.md  count=1  note=a fix\n"+
		"<<<<\nThe entry point is:\n====\nEntry point (fixed):\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", fix); err != nil {
		t.Fatal(err)
	}
	ledger(t, wsDir, 2, rows)
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r2", "findings.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "third", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}

	// ONE edit, many lines.
	big := "=== artifacts/decisions.md  count=1  note=one edit, many lines\n<<<<\nEntry point (fixed):\n====\n"
	for i := 0; i < 30; i++ {
		big += fmt.Sprintf("Entry point (restated %d):\n", i)
	}
	big += ">>>>\n"
	second := filepath.Join(tmp, "second.txt")
	write(t, second, big)

	out, err := runWF(t, wsDir, "apply", "-f", second)
	noErr(t, err, "apply")
	mustContain(t, out, "1 edit(s) changing 31 line(s)", "predicts ~", "consumers are NOT in this diff")

	// The same population, printed by `wf stats` where the operator can see WHICH
	// rounds the prediction reads and which one it refuses to read. Round 1's
	// window created the artifact: 10 findings against 2 changed lines would put
	// the prediction an order of magnitude out, exactly as a live gate measured.
	out, err = runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out, "prediction basis — rounds 2", "`wf apply` predicts from THIS",
		"round(s) 1 excluded", "window ADDS an artifact")
}

// `wf stats`' gate-wide cost line and `wf apply`'s note are two readings of one
// number and must share a basis. They did not: the note read every batch at
// HEAD while the total summed only attributable windows, and on a live gate the
// two disagreed by 60%. A second derivation of one number is this project's
// most frequent defect.
func TestStatsTotalAndApplyNoteShareABasis(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=seed\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", edits); err != nil {
		t.Fatal(err)
	}
	ledger(t, wsDir, 1, "| 1 | MAJOR | one finding | prose |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	// A round with no tag of its own and no predecessor tag: its window is not
	// attributable, so summing windows would drop this batch from the total.
	if _, err := runWF(t, wsDir, "round", "new", "-title", "second", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(tmp, "second.txt")
	write(t, second, "=== artifacts/decisions.md  count=1  note=again\n"+
		"<<<<\nThe entry point is:\n====\nEntry point:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", second); err != nil {
		t.Fatal(err)
	}

	w, err := ws.Find(wsDir)
	if err != nil {
		t.Fatal(err)
	}
	hist, _, _ := batchHistory(w)
	histLines := 0
	for _, h := range hist {
		histLines += h.Lines
	}
	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	// The gate total is the WHOLE population and the prediction basis is
	// narrower — the two are different questions and they now have different
	// answers. What must never happen again is the two disagreeing SILENTLY, by
	// 60% on a live gate: `wf stats` prints the basis `wf apply` predicts from,
	// derived by the same function the note calls.
	if histLines > 0 {
		want := fmt.Sprintf("over %d changed line(s)", histLines)
		if !strings.Contains(out, want) {
			t.Fatalf("stats must print the population the apply note predicts from (%s), got:\n%s", want, out)
		}
		mustContain(t, out, "prediction basis", "`wf apply` predicts from THIS")
	}
	mustContain(t, out, "apply window not attributable")
}

// The tool's own output is a copy of the spec, and it was the STALE copy: the
// manual retired "the share climbs toward 100% when the gate is exhausted" and
// `wf stats` went on printing it every round. Pinned here because the operator
// reads the output, not the reference.
func TestStatsPrintsTheFixInducedRuleTheManualHas(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 1 | MAJOR | a finding [fix-induced] | prose |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out, "fix-induced — 1 of 1 finding(s)", "CONTINUE signal, never a stop signal")
	mustNotContain(t, out, "climbs toward 100%")
}

// The note beside the prediction has advised this sweep since the meter existed
// — "if this batch changed a RULE, its consumers are NOT in this diff" — and it
// named nothing, so the operator had to go and look. Measured on mocker A3: six
// of round 10's twelve blockers were one decision repaired in the document and
// left standing in the prompt restating it.
func TestApplyRuleNamesTheSitesOutsideTheBatch(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	doc := filepath.Join(tmp, "rules.md")
	// TWO sites in the file the batch writes and ONE outside it, so the count
	// of sites outside the batch cannot coincide with the count inside.
	write(t, doc, "# Rules\n\n- [R6] The two routes DETACH their envelope from the shared component.\n\n"+
		"## Restated for the reviewer\n\n- [R6] The two routes DETACH their envelope from the shared component.\n")
	if _, err := runWF(t, wsDir, "add", doc); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(tmp, "gate.mjs")
	write(t, script, "const CONTRACT = `\nThe two routes keep their existing envelope beside the new one [R6].\n`\n")
	if _, err := runWF(t, wsDir, "add", script); err != nil {
		t.Fatal(err)
	}

	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/rules.md  count=2  note=R6 repair\n"+
		"<<<<\nfrom the shared component.\n====\nfrom the shared component, and the 409 stays where it is.\n>>>>\n")
	out, err := runWF(t, wsDir, "apply", "-f", edits, "-rule", "6", "-m", "rules: repair R6")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out,
		"rule: [R6] is marked at 3 site(s), 1 of them outside this batch",
		"artifacts/gate.mjs:",
		"← a file this batch wrote")
	// The site the batch did NOT write is the whole point, and it must not be
	// marked as one it did.
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "artifacts/gate.mjs:") && strings.Contains(l, "this batch wrote") {
			t.Fatalf("the script was not in this batch: %s", l)
		}
	}
}

// A batch declaring a rule nothing marks is the loudest outcome of the flag,
// and it is not an error: that is exactly the corpus `wf check rules` cannot
// reach either.
func TestApplyRuleSaysWhenNothingCarriesTheMarker(t *testing.T) {
	wsDir := newGate(t)
	edits := filepath.Join(filepath.Dir(wsDir), "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=reword\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	out, err := runWF(t, wsDir, "apply", "-f", edits, "-rule", "R9")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "rule: NO site carries [R9]")
}

// The id is normalised where an operator will actually type it. Anything that
// is NOT an id is a free-text rule NAME rather than an error: measured on P3a,
// `-rule "bar accounting"` was refused outright and the flag was unusable for a
// whole script gate, whose rules are marked in no document.
func TestParseRuleFlag(t *testing.T) {
	for _, c := range []struct{ in, id, phrase string }{
		{"", "", ""},
		{"R6", "R6", ""}, {"r6", "R6", ""}, {"6", "R6", ""}, {"[R6]", "R6", ""}, {" R12 ", "R12", ""},
		{"bar accounting", "", "bar accounting"},
		{"six", "", "six"},
		// Looks like an id and is not one: it sweeps for the phrase and finds
		// nothing, which announces itself. Refusing it would refuse the form
		// the flag now exists to accept.
		{"R6a", "", "R6a"},
		{"R", "", "R"},
	} {
		id, phrase := parseRuleFlag(c.in)
		if id != c.id || phrase != c.phrase {
			t.Errorf("%q: got id %q phrase %q; want id %q phrase %q", c.in, id, phrase, c.id, c.phrase)
		}
	}
}

// A script gate's rules live in prompts and are marked in no document, so the
// id form of `-rule` reaches nothing there. Measured on P3a: `-rule "bar
// accounting"` was refused outright, the flag was unusable for the whole gate,
// and the sweep it triggers is the one that gate found worth the most.
func TestApplyRuleSweepsAFreeTextRuleName(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	script := filepath.Join(tmp, "gate.mjs")
	write(t, script, "const ACCEPT = `\nEvery bar the section ran is reported in the bar accounting object.\n`\n"+
		"const VERIFY = `\nThe bar accounting is re-derived after the last mutation of the tree.\n`\n")
	if _, err := runWF(t, wsDir, "add", script); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(tmp, "notes.md")
	// The second sentence is the substring trap: `bar` is inside `barrier`, and
	// a phrase matched by substring rather than by whole token would count it.
	write(t, notes, "# Notes\n\nThe bar accounting is a term of green and accumulates over the run.\n\n"+
		"The barrier accounting of a parallel stage is a different subject entirely.\n")
	if _, err := runWF(t, wsDir, "add", notes); err != nil {
		t.Fatal(err)
	}

	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/notes.md  count=1  note=bar accounting\n"+
		"<<<<\naccumulates over the run.\n====\nis re-derived after the last mutation.\n>>>>\n")
	out, err := runWF(t, wsDir, "apply", "-f", edits, "-rule", "bar accounting", "-m", "notes: bar accounting")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out,
		`rule: "bar accounting" appears in 3 sentence(s) of 4 words or more, 2 of them outside this batch`,
		"an UNMARKED rule",
		"artifacts/gate.mjs:")
	// The file the batch wrote is marked, and the two prompts are not.
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "artifacts/gate.mjs:") && strings.Contains(l, "this batch wrote") {
			t.Fatalf("the script was not in this batch: %s", l)
		}
	}
}

// A phrase nothing carries is the loudest outcome of the free-text form, and it
// is not an error — the same shape as a marker no artifact carries.
func TestApplyRuleSaysWhenNoSentenceCarriesThePhrase(t *testing.T) {
	wsDir := newGate(t)
	edits := filepath.Join(filepath.Dir(wsDir), "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=reword\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	out, err := runWF(t, wsDir, "apply", "-f", edits, "-rule", "clause regression channel")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, `NO sentence of 4 words or more in 1 artifact(s) carries every word of "clause regression channel"`)
}
