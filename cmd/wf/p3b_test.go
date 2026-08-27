// The measurements of the gate that STOPPED instead of closing (mocker P3b).
//
// Each test here pins one thing that gate paid for: a blocker count that could
// not say where its blockers came from, a batch whose every guarantee was
// vacuous, a flag whose list form was refused, and a freeze that treated its
// own reason to run again as an error.
package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.sumka.site/yakov/wfgate/internal/gitx"
)

// Blockers by ORIGIN. Measured: four rounds at 35/9, 37/3, 30/5, 35/5 with
// fix-induced at 16, 14, 16 — a flat blocker count read as a gate still finding
// defects while four of the last round's five had been introduced by the round
// before it.
func TestStatsSplitsBlockersByOrigin(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, ""+
		"| 1 | BLOCKER | [fix-induced] the fix broke the anchor |  |\n"+
		"| 2 | BLOCKER | [fix-induced] and the one beside it |  |\n"+
		"| 3 | BLOCKER | a defect the artifact always had |  |\n"+
		"| 4 | MAJOR | [fix-induced] a major from the same fix |  |\n")

	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out,
		"fix-induced 3 (2 of 3 blocker(s))",
		"fix-induced blockers — 2 of 3 blocker(s)",
		"1 round(s) produced blockers that are MAJORITY fix-induced (r1 2/3)",
		"not evidence the ARTIFACT still has defects")
}

// The other half of the same claim: a round whose blockers are mostly its own
// is a different object from one that merely carries a marked finding, and only
// the first gets the note.
func TestStatsDoesNotCallAMinorityShareSelfInflicted(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, ""+
		"| 1 | BLOCKER | [fix-induced] one from the fix |  |\n"+
		"| 2 | BLOCKER | a defect the artifact always had |  |\n"+
		"| 3 | BLOCKER | and another |  |\n")

	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out, "fix-induced 1 (1 of 3 blocker(s))", "fix-induced blockers — 1 of 3 blocker(s)")
	mustNotContain(t, out, "MAJORITY fix-induced")
}

// A round with a marked finding and no blocker states no share of a population
// it does not have.
func TestStatsOmitsTheBlockerShareWhereThereAreNoBlockers(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 1 | MAJOR | [fix-induced] a major |  |\n")

	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out, "fix-induced 1")
	mustNotContain(t, out, "blocker(s))", "fix-induced blockers —")
}

// One replacement carrying the whole artifact makes `count` and all-or-nothing
// vacuous: its count is one and always matches, so the many changes inside it
// are verified by nothing. Measured: a gate applied eighteen single-replacement
// batches and a later round found 15 of one round's 30 recorded closures
// byte-identical to their pre-fix text (a second lens confirmed 11).
func TestApplyNamesAWholeArtifactReplacement(t *testing.T) {
	wsDir := newGate(t)
	body, err := os.ReadFile(filepath.Join(wsDir, "artifacts", "decisions.md"))
	if err != nil {
		t.Fatal(err)
	}
	edits := filepath.Join(t.TempDir(), "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=restructure\n"+
		"<<<<\n"+string(body)+"====\n"+strings.Replace(string(body), "Entry point", "The entry point", 1)+">>>>\n")

	out, err := runWF(t, wsDir, "apply", "-f", edits)
	noErr(t, err, "apply")
	mustContain(t, out,
		"of artifacts/decisions.md in ONE replacement",
		// How many lines this ONE replacement rewrites is the number of
		// separate fixes it can be hiding, and it is the actionable half.
		"rewriting 1 line(s)",
		"verified by NOTHING",
		"byte-verify each change")
}

// The boundary is HALF, and it is where a replacement stops being an edit to a
// place in the document and becomes the document. Written through the JSON
// batch so the bytes are exact: the text format trims the newline before its
// marker, which moves the share by a line.
func TestApplyNotesAReplacementAtExactlyHalfTheFile(t *testing.T) {
	wsDir := newGate(t)
	art := filepath.Join(wsDir, "artifacts", "half.md")
	write(t, art, strings.Repeat("x", 49)+"\n"+strings.Repeat("y", 49)+"\n")
	gitIn(t, wsDir, "add", "-A")
	gitIn(t, wsDir, "commit", "-qm", "half")

	edits := filepath.Join(t.TempDir(), "edits.json")
	old := strings.Repeat("x", 49) + "\n"
	b, err := json.Marshal([]map[string]any{{"file": "artifacts/half.md", "old": old, "new": "z\n"}})
	if err != nil {
		t.Fatal(err)
	}
	write(t, edits, string(b))

	out, err := runWF(t, wsDir, "apply", "-f", edits)
	noErr(t, err, "apply")
	mustContain(t, out, "replaces 50% of artifacts/half.md in ONE replacement")
}

// And it must not fire on an edit that is an edit: the note names a shape, not
// a size, and a batch that gets one every time is a batch nobody reads.
func TestApplyIsSilentOnATargetedEdit(t *testing.T) {
	wsDir := newGate(t)
	edits := filepath.Join(t.TempDir(), "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=reword\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")

	out, err := runWF(t, wsDir, "apply", "-f", edits)
	noErr(t, err, "apply")
	mustNotContain(t, out, "in ONE replacement")
}

// The three questions a delta round may never drop are a SET, and every way of
// writing a set down means the same thing. Measured: the natural spelling was
// refused and the working path became a hand edit of the lens file, nineteen times over
// ten rounds in one workspace.
func TestAsksAcceptsEverySpellingOfOneSet(t *testing.T) {
	want := []string{"closure", "fix-induced", "ripple"}
	for _, raw := range []string{
		"closure,fix-induced,ripple",
		"closure+fix-induced+ripple",
		"closure fix-induced ripple",
		"closure\tfix-induced\nripple",
		"closure, fix-induced,ripple",
	} {
		got, err := parseAsks(raw)
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%q parsed to %v", raw, got)
		}
	}
	// A word off the closed set still fails, whichever separator carried it:
	// the point of the set is that one gate's roster can be read against
	// another's.
	if _, err := parseAsks("closure+invented"); err == nil {
		t.Fatal("an unknown question must be refused whatever separates it")
	}
}

// And the repeated flag, which is the same claim written the other way.
func TestRoundLensAcceptsARepeatedAsksFlag(t *testing.T) {
	wsDir := newGate(t)
	prompt := filepath.Join(t.TempDir(), "lens.md")
	write(t, prompt, "Check every closure claim against the diff.\n")

	_, err := runWF(t, wsDir, "round", "lens", prompt, "-name", "CLOSURE", "-model", "opus",
		"-asks", "closure", "-asks", "fix-induced", "-asks", "ripple")
	noErr(t, err, "round lens")
	// The RECORD is what a later round reads, so that is what is asserted on.
	banked, err := os.ReadFile(filepath.Join(wsDir, "rounds", "r1", "lens-closure.md"))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, string(banked), "Asks: closure, fix-induced, ripple")

	// And the roster, which is where `wf stats` and `wf close` read it.
	roster, err := runWF(t, wsDir, "round", "lens", "-round", "1")
	noErr(t, err, "roster")
	mustContain(t, roster, "closure+fix-induced+ripple")
}

// The findings file is positional and `-` is stdin: a ledger assembled by a
// script is piped, and the previous usage line ruled neither in nor out.
func TestRoundRecordReadsStdin(t *testing.T) {
	wsDir := newGate(t)
	table := "# Findings\n\n| # | level | finding | closed by |\n|---|-------|---------|-----------|\n" +
		"| 1 | MAJOR | piped in |  |\n"

	out, err := withStdin(t, table, func() (string, error) {
		return runWF(t, wsDir, "round", "record", "-")
	})
	noErr(t, err, "round record -")
	mustContain(t, out, "recorded rounds/r1/findings.md", "1 row(s)")
	got, err := os.ReadFile(filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != table {
		t.Fatalf("the recorded ledger is not what was piped:\n%s", got)
	}
}

// An empty pipe is a script that produced nothing, and recording it would
// overwrite a filled ledger with a blank one.
func TestRoundRecordRefusesAnEmptyInput(t *testing.T) {
	wsDir := newGate(t)
	_, err := withStdin(t, "   \n", func() (string, error) {
		return runWF(t, wsDir, "round", "record", "-")
	})
	if err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("want a refusal naming the empty input, got %v", err)
	}
}

// `wf check cite` surfaces a mis-aimed anchor without a freeze; what only
// freeze does is REPUBLISH the corrected bytes under the tag reviewers were
// handed, and fixing an anchor means freezing again: the tool
// treated re-freezing as an error, so five of one gate's twenty-two rounds
// deleted the tag by hand — twelve times in all, round 13 six times.
func TestFreezeRefreezesOnlyWhenSomethingChanged(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}

	// The default is still the refusal, and it is the one-way one: the
	// re-freeze path's own message names the flag too, so asserting on the flag
	// alone let a mutation that removed this refusal pass.
	_, err := runWF(t, wsDir, "freeze")
	if err == nil || !strings.Contains(err.Error(), "a frozen round is not re-frozen") ||
		!strings.Contains(err.Error(), "-refreeze") {
		t.Fatalf("want the one-way refusal, naming the escape, got %v", err)
	}
	// And the escape refuses to record a supersession that superseded nothing.
	_, err = runWF(t, wsDir, "freeze", "-refreeze")
	if err == nil || !strings.Contains(err.Error(), "there are none") {
		t.Fatalf("want a refusal over an unchanged tree, got %v", err)
	}

	before := tagCommit(t, wsDir, "gate-r1")
	edits := filepath.Join(t.TempDir(), "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=re-aim the citation\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", edits); err != nil {
		t.Fatal(err)
	}

	out, err := runWF(t, wsDir, "freeze", "-refreeze")
	noErr(t, err, "freeze -refreeze")
	mustContain(t, out, "RE-FROZEN — this is freeze 2 of round 1", "read different bytes")

	if after := tagCommit(t, wsDir, "gate-r1"); after == before {
		t.Fatal("the tag must name the new commit, or the freeze published bytes nobody tagged")
	}
	var fm freezeManifest
	b, err := os.ReadFile(filepath.Join(wsDir, "rounds", "r1", "freeze.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &fm); err != nil {
		t.Fatal(err)
	}
	if fm.Refreezes != 1 || len(fm.Supersedes) != 1 || !strings.HasPrefix(fm.Supersedes[0], before[:8]) {
		t.Fatalf("the manifest must record what it superseded: %+v (superseded %s)", fm, before)
	}
}

func tagCommit(t *testing.T, wsDir, tag string) string {
	t.Helper()
	out, err := gitx.Repo{Dir: wsDir}.Run("rev-list", "-n", "1", tag)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// withStdin runs fn with os.Stdin replaced by a pipe carrying body.
func withStdin(t *testing.T, body string, fn func() (string, error)) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	go func() {
		_, _ = io.WriteString(w, body)
		_ = w.Close()
	}()
	return fn()
}

// Two freezes cover the same bytes only when they cover the same SET of files:
// an artifact added since is a different set even where every shared hash
// matches, and comparing entries alone would call it unchanged.
func TestSameArtifactsIsAboutTheSetAsWellAsTheHashes(t *testing.T) {
	one := map[string]string{"a.md": "h1"}
	two := map[string]string{"a.md": "h1", "b.md": "h2"}
	if !sameArtifacts(one, map[string]string{"a.md": "h1"}) {
		t.Fatal("the same file at the same hash is the same set")
	}
	if sameArtifacts(one, two) {
		t.Fatal("an artifact ADDED since the last freeze is a different set")
	}
	if sameArtifacts(two, one) {
		t.Fatal("an artifact REMOVED since the last freeze is a different set")
	}
	if sameArtifacts(map[string]string{}, map[string]string{}) {
		// A freeze with no artifacts is not a freeze this comparison can call
		// unchanged: it is the state before anything was pinned.
		t.Fatal("two empty manifests are not evidence of unchanged bytes")
	}
	if sameArtifacts(one, map[string]string{"a.md": "h2"}) {
		t.Fatal("a changed hash is a changed artifact")
	}
}

// A tool round dispatched no reviewer by declaration, so its blockers are not
// what the stop rule reads and it is not named as a round that re-fixed itself.
func TestStatsKeepsAToolRoundOutOfTheSelfInflictedNote(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "tooling", "-kind", "tool"); err != nil {
		t.Fatal(err)
	}
	ledger(t, wsDir, 2, ""+
		"| 1 | BLOCKER | [fix-induced] a tooling defect |  |\n"+
		"| 2 | BLOCKER | [fix-induced] and another |  |\n")

	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustNotContain(t, out, "MAJORITY fix-induced")
	// The reviewer figure is printed beside the gate total rather than the
	// population being narrowed in silence.
	mustContain(t, out, "over REVIEWER rounds, which is what the stop rule reads")
}

// A majority is MORE than half: two of four is not a round that mostly re-fixed
// the previous round's fix.
func TestStatsMajorityIsStrictlyMoreThanHalf(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, ""+
		"| 1 | BLOCKER | [fix-induced] one |  |\n"+
		"| 2 | BLOCKER | [fix-induced] two |  |\n"+
		"| 3 | BLOCKER | a defect the artifact always had |  |\n"+
		"| 4 | BLOCKER | and another |  |\n")

	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out, "fix-induced 2 (2 of 4 blocker(s))")
	mustNotContain(t, out, "MAJORITY fix-induced")
}

// The rounds named on a denominator of one or two are counted apart: "the fix
// rate matched the introduction rate" is a claim about a RATE and it printed
// identically over 1/1 and over 20/21. Measured: five of one gate's seven named
// rounds and five of another's are at two blockers or fewer.
func TestStatsSaysHowManySelfInflictedRoundsAreTiny(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 1 | BLOCKER | [fix-induced] the only blocker |  |\n")

	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out, "(r1 1/1)", "1 of them are a round with two blockers or fewer")
}

// A second re-freeze accumulates rather than restarting, and the manifest it
// reads is the one under the TAG — not the worktree copy this command rewrites.
// A failed tag move otherwise left a committed manifest describing the current
// tree, and the retry compared it against itself and refused.
func TestFreezeRefreezeAccumulatesFromTheTag(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	first := tagCommit(t, wsDir, "gate-r1")

	edit := func(from, to string) {
		p := filepath.Join(t.TempDir(), "edits.txt")
		write(t, p, "=== artifacts/decisions.md  count=1  note=re-aim\n<<<<\n"+from+"\n====\n"+to+"\n>>>>\n")
		if _, err := runWF(t, wsDir, "apply", "-f", p); err != nil {
			t.Fatal(err)
		}
	}
	edit("Entry point:", "The entry point is:")
	if _, err := runWF(t, wsDir, "freeze", "-refreeze"); err != nil {
		t.Fatal(err)
	}
	second := tagCommit(t, wsDir, "gate-r1")

	edit("The entry point is:", "The entry point:")
	out, err := runWF(t, wsDir, "freeze", "-refreeze")
	noErr(t, err, "third freeze")
	mustContain(t, out, "this is freeze 3 of round 1")

	var fm freezeManifest
	b, err := os.ReadFile(filepath.Join(wsDir, "rounds", "r1", "freeze.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &fm); err != nil {
		t.Fatal(err)
	}
	if fm.Refreezes != 2 {
		t.Fatalf("the count must accumulate, got %d", fm.Refreezes)
	}
	if len(fm.Supersedes) != 2 || fm.Supersedes[0] != first || fm.Supersedes[1] != second {
		t.Fatalf("every superseded publication must survive: %v (want %s, %s)", fm.Supersedes, first, second)
	}
	// And `wf stats` says the round was published more than once, because a
	// moved tag re-attributes every apply batch the window covers.
	st, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, st, "1 round(s) were published more than once (1)")
}

// The target moving is its own reason to republish: the freeze pins the
// revision the citations name, so identical artifacts against a NEW target
// revision are different bytes to verify against.
func TestFreezeRefreezesWhenOnlyTheTargetMoved(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(filepath.Dir(wsDir), "target")
	write(t, filepath.Join(target, "src", "extra.go"), "package main\n")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-qm", "the target moved under the frozen citations")

	out, err := runWF(t, wsDir, "freeze", "-refreeze")
	noErr(t, err, "refreeze after the target moved")
	mustContain(t, out, "RE-FROZEN")
}

// `-asks ""` is not the flag being absent. The joined form of a one-element
// list holding the empty string is "", so the roster printed instead of the
// refusal.
func TestRoundLensRefusesAnEmptyAsksWithNoFile(t *testing.T) {
	wsDir := newGate(t)
	_, err := runWF(t, wsDir, "round", "lens", "-round", "1", "-asks", "")
	if err == nil || !strings.Contains(err.Error(), "no prompt file given") {
		t.Fatalf("want the mistyped-argument refusal, got %v", err)
	}
}

// `count` > 1 means ReplaceAll changes every occurrence, so what the batch
// replaces is the span TIMES the matches: three edits of a fifth of a file each
// are three fifths of it, and pricing one occurrence hid that.
func TestApplySpanCountsEveryOccurrence(t *testing.T) {
	wsDir := newGate(t)
	block := strings.Repeat("q", 60) + "\n"
	write(t, filepath.Join(wsDir, "artifacts", "twice.md"), block+"middle\n"+block)
	gitIn(t, wsDir, "add", "-A")
	gitIn(t, wsDir, "commit", "-qm", "twice")

	edits := filepath.Join(t.TempDir(), "edits.json")
	b, err := json.Marshal([]map[string]any{{"file": "artifacts/twice.md", "old": block, "new": "z\n", "count": 2}})
	if err != nil {
		t.Fatal(err)
	}
	write(t, edits, string(b))

	out, err := runWF(t, wsDir, "apply", "-f", edits)
	noErr(t, err, "apply")
	// 61 bytes twice against 129: one occurrence is 47%, both are 94%.
	mustContain(t, out, "of artifacts/twice.md in ONE replacement", "rewriting 2 line(s)")
}

// The share is priced against the file as it was READ, never the evolving
// buffer: a batch that restructures a document and then touches it again
// reported the second, tiny edit as half the artifact and told the operator its
// verification was vacuous.
func TestApplyPricesAgainstTheFileAsRead(t *testing.T) {
	wsDir := newGate(t)
	big := strings.Repeat("filler line\n", 100)
	write(t, filepath.Join(wsDir, "artifacts", "doc.md"), big+"TAIL\n")
	gitIn(t, wsDir, "add", "-A")
	gitIn(t, wsDir, "commit", "-qm", "doc")

	edits := filepath.Join(t.TempDir(), "edits.json")
	b, err := json.Marshal([]map[string]any{
		{"file": "artifacts/doc.md", "old": big + "TAIL\n", "new": "HEAD\nTAIL\n"},
		{"file": "artifacts/doc.md", "old": "TAIL\n", "new": "TAIL!\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	write(t, edits, string(b))

	out, err := runWF(t, wsDir, "apply", "-f", edits)
	noErr(t, err, "apply")
	mustContain(t, out, "edit [0] replaces 100% of artifacts/doc.md")
	// Five bytes of a 1305-byte file. Against the buffer edit [0] left behind
	// it is half of what remains.
	mustNotContain(t, out, "edit [1] replaces")
}

// The banked table `wf prune` leaves behind must carry the re-freeze, or a
// round published three times reads as frozen once and the reviewers of the
// earlier publications read bytes nothing records.
func TestCloseBanksTheRefreezeCount(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "edits.txt")
	write(t, p, "=== artifacts/decisions.md  count=1  note=re-aim\n<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", p); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "freeze", "-refreeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "close"); err != nil {
		t.Fatal(err)
	}
	fz, err := os.ReadFile(filepath.Join(wsDir, "close", "freezes.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(string(fz)), "\n")
	if len(rows) != 2 {
		t.Fatalf("want a header and one row: %q", fz)
	}
	cells := strings.Split(rows[1], "\t")
	if len(cells) != 10 || cells[8] != "1" || len(cells[9]) < 40 {
		t.Fatalf("the row must carry the refreeze count and what it superseded: %q", rows[1])
	}
}

// The recovery from a tag move that failed. The manifest is committed before
// the tag moves, so that state leaves a NEW manifest under an OLD tag — and
// reading the worktree copy then compared it against itself, refused "nothing
// has changed", and stranded the round in the one state `wf verify` reads as
// clean.
func TestFreezeRefreezeRecoversFromAMovedManifestAndAnOldTag(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	published := tagCommit(t, wsDir, "gate-r1")

	p := filepath.Join(t.TempDir(), "edits.txt")
	write(t, p, "=== artifacts/decisions.md  count=1  note=re-aim\n<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", p); err != nil {
		t.Fatal(err)
	}

	// Exactly what a failed `RetagTo` leaves: the manifest of the CURRENT tree,
	// committed, with the tag still on the previous publication.
	man := filepath.Join(wsDir, "rounds", "r1", "freeze.json")
	b, err := os.ReadFile(man)
	if err != nil {
		t.Fatal(err)
	}
	var fm freezeManifest
	if err := json.Unmarshal(b, &fm); err != nil {
		t.Fatal(err)
	}
	fm.Refreezes = 1
	fm.Supersedes = []string{published}
	for a := range fm.Artifacts {
		fm.Artifacts[a] = strings.Repeat("f", 64)
	}
	out, err := json.MarshalIndent(fm, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write(t, man, string(out)+"\n")
	gitIn(t, wsDir, "commit", "-qam", "a re-freeze whose tag move failed")

	got, err := runWF(t, wsDir, "freeze", "-refreeze")
	noErr(t, err, "the retry must not be stranded by the manifest it wrote")
	mustContain(t, got, "RE-FROZEN — this is freeze 2 of round 1")
	if tagCommit(t, wsDir, "gate-r1") == published {
		t.Fatal("the tag must move on the retry, or the round stays published at the old bytes")
	}
}

// A manifest that does not PARSE is not a manifest with no supersessions in it.
// Discarding the error reset the count and dropped every earlier superseded
// commit — the one thing that field exists to hold, and nothing downstream
// reads it, so nothing would have caught it.
func TestFreezeRefreezeRefusesAnUnreadableManifest(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(wsDir, "rounds", "r1", "freeze.json"), "{ this is not json\n")
	gitIn(t, wsDir, "commit", "-qam", "a manifest nothing can read")
	// The tag is what this command reads, so the damage has to reach it.
	gitIn(t, wsDir, "tag", "-f", "-a", "gate-r1", "-m", "re-pointed at the unreadable manifest")

	p := filepath.Join(t.TempDir(), "edits.txt")
	write(t, p, "=== artifacts/decisions.md  count=1  note=re-aim\n<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", p); err != nil {
		t.Fatal(err)
	}

	_, err := runWF(t, wsDir, "freeze", "-refreeze")
	if err == nil || !strings.Contains(err.Error(), "does not parse") {
		t.Fatalf("want a refusal naming the unreadable manifest, got %v", err)
	}
}
