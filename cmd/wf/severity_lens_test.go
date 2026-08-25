// The two things every agent was inventing for itself: what a level MEANS, and
// what a delta round ASKS. Both are closed on the write path and permissive on
// every read path, and both refusals are here with the drift that produced them.
//
// Measured 2026-08-25 with `ws.ParseFindings` over the six gate ledgers on disk,
// every total reconciled against that workspace's own `wf stats`: 1065 findings
// under FOUR vocabularies for one scale (BLOCKER universal; MAJOR on five gates,
// CONCERN on one; MINOR on four, ADVISORY on one, NIT on one), 172 of them —
// 16% — at a level no other gate can be read against. The same six phases'
// orchestration scripts, whose severity is a JSON Schema enum checked at the
// tool-call layer, declare exactly ['blocker','major','minor'] all six times.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A level off the closed scale is refused BEFORE anything is written. CONCERN is
// not a hypothetical: it is 70 rows of one live gate.
func TestRoundRecord_refusesALevelOffTheClosedScale(t *testing.T) {
	wsDir := newGate(t)
	src := filepath.Join(t.TempDir(), "findings.md")
	write(t, src, "# Findings\n\n| # | level | finding | closed by |\n|---|-------|---------|-----------|\n"+
		"| 1 | CONCERN | the level nobody else can read |  |\n")

	_, err := runWF(t, wsDir, "round", "record", src)
	if err == nil {
		t.Fatal("a level off the closed scale was recorded; the scale is what lets one gate's round be compared with another's")
	}
	for _, want := range []string{"CONCERN", "BLOCKER", "nothing was recorded"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not say %q: %v", want, err)
		}
	}
	// Refused BEFORE the write, like apply: a landed lie briefs the next round.
	if b, readErr := os.ReadFile(filepath.Join(wsDir, "rounds", "r1", "findings.md")); readErr == nil && strings.Contains(string(b), "CONCERN") {
		t.Fatal("the refused row reached rounds/r1/findings.md; the check must run before the write, as it does in apply")
	}
}

// Every level ON the scale is accepted, including the one that asserts no
// failure — and NOTE is named rather than subtracted, because every banked total
// in this corpus counts whatever non-defect rows its gate filed.
func TestRoundRecord_acceptsTheWholeScaleAndNamesNotes(t *testing.T) {
	wsDir := newGate(t)
	src := filepath.Join(t.TempDir(), "findings.md")
	write(t, src, "# Findings\n\n| # | level | finding | closed by |\n|---|-------|---------|-----------|\n"+
		"| 1 | BLOCKER | one |  |\n| 2 | MAJOR | two |  |\n| 3 | MINOR | three |  |\n| 4 | NOTE | four |  |\n")

	out, err := runWF(t, wsDir, "round", "record", src)
	if err != nil {
		t.Fatalf("the closed scale itself was refused: %v", err)
	}
	if !strings.Contains(out, "1 NOTE(s)") {
		t.Fatalf("the NOTE row is not named apart: %s", out)
	}
	if !strings.Contains(out, "4 row(s), 1 blocker(s)") {
		t.Fatalf("NOTE was subtracted from the total instead of counted in it: %s", out)
	}
}

// -asks is the comparable half of a roster beside a free-text -name. An unknown
// question is refused; `other` always exists so a slice-specific vector is never
// refused, which is why the refusal on NAMES was not built at all.
func TestRoundLens_refusesAnUncomparableQuestion(t *testing.T) {
	wsDir := newGate(t)
	prompt := filepath.Join(t.TempDir(), "p.md")
	write(t, prompt, "check the closure claims\n")

	_, err := runWF(t, wsDir, "round", "lens", prompt, "-name", "REPAIRS", "-model", "opus", "-asks", "repairs")
	if err == nil {
		t.Fatal("an uncomparable -asks was banked; ten distinct vector names over 17 banked lenses is the drift this closes")
	}
	if !strings.Contains(err.Error(), "other") || !strings.Contains(err.Error(), "closure") {
		t.Fatalf("the refusal does not offer the closed set or the `other` escape: %v", err)
	}

	// The same vector, banked under its own label with a comparable question.
	if _, err := runWF(t, wsDir, "round", "lens", prompt, "-name", "REPAIRS", "-model", "opus", "-asks", "closure"); err != nil {
		t.Fatalf("a correct lens was refused: %v", err)
	}
	roster, err := runWF(t, wsDir, "round", "lens")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(roster, "REPAIRS") || !strings.Contains(roster, "closure") {
		t.Fatalf("the roster does not carry the label and the question: %s", roster)
	}
}

// -asks is required for the same reason -model is: a roster that does not say
// what the vector asked records that a round had lenses and not which.
func TestRoundLens_requiresAsks(t *testing.T) {
	wsDir := newGate(t)
	prompt := filepath.Join(t.TempDir(), "p.md")
	write(t, prompt, "some vector\n")

	_, err := runWF(t, wsDir, "round", "lens", prompt, "-name", "X", "-model", "opus")
	if err == nil {
		t.Fatal("a lens with no -asks was banked")
	}
	if !strings.Contains(err.Error(), "-asks") {
		t.Fatalf("the refusal does not name the missing flag: %v", err)
	}
}

// The coverage rule is a NOTE, never a refusal: it is new, and every corpus that
// would have to satisfy it is closed. What it must do is name what is missing.
func TestRoundLens_rosterNamesTheUncoveredDeltaQuestions(t *testing.T) {
	wsDir := newGate(t)
	prompt := filepath.Join(t.TempDir(), "p.md")
	write(t, prompt, "did the closures land\n")
	if _, err := runWF(t, wsDir, "round", "lens", prompt, "-name", "REPAIRS", "-model", "opus", "-asks", "closure"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "round", "lens")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "fix-induced") || !strings.Contains(out, "ripple") {
		t.Fatalf("the roster does not name the delta questions nothing asked: %s", out)
	}
	if strings.Contains(out, "no banked lens asks closure") {
		t.Fatalf("a covered question was reported as missing: %s", out)
	}
}

// Reading stays permissive while writing is closed, and that asymmetry is
// load-bearing: three closed workspaces carry CONCERN, ADVISORY and NIT rows —
// 172 findings of the 1065 in the corpus — and a report that refused to read
// them would destroy the only evidence this project reasons from. What the
// report must do instead is say the level cannot be compared.
func TestStats_readsAnOffScaleLedgerAndNamesIt(t *testing.T) {
	wsDir := newGate(t)
	// Written straight into the round, the way a closed gate's ledger already
	// sits on disk — `wf round record` would refuse these two today.
	ledger(t, wsDir, 1, "| 1 | CONCERN | the tier one gate called MAJOR |  |\n"+
		"| 2 | NIT | the tier one gate called MINOR |  |\n"+
		"| 3 | NOTE | not a defect at all |  |\n"+
		"| 4 | BLOCKER | one the stop rule reads |  |\n")
	gitIn(t, wsDir, "add", "-A")
	gitIn(t, wsDir, "commit", "-q", "-m", "round 1: record findings")

	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	if !strings.Contains(out, "4 finding(s), 1 blocker(s)") {
		t.Fatalf("an off-scale ledger stopped being counted: %s", out)
	}
	if !strings.Contains(out, "2 finding(s) carry a level outside") {
		t.Fatalf("the uncomparable levels are not named: %s", out)
	}
	if !strings.Contains(out, "1 of the findings above are NOTE rows") {
		t.Fatalf("the NOTE row is not named apart: %s", out)
	}
}
