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
// refused and the working path became a hand edit of the lens file, twenty-two
// times in one workspace.
func TestAsksAcceptsEverySpellingOfOneSet(t *testing.T) {
	want := []string{"closure", "fix-induced", "ripple"}
	for _, raw := range []string{
		"closure,fix-induced,ripple",
		"closure+fix-induced+ripple",
		"closure fix-induced ripple",
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

// Freeze's own citation check is what surfaces a mis-aimed anchor, and fixing
// one means freezing again: the check is the reason to re-freeze and the tool
// treated re-freezing as an error, so every round of one gate deleted the tag
// by hand.
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
