// The two things every agent was inventing for itself: what a level MEANS, and
// what a delta round ASKS. Both are closed on the write path and permissive on
// every read path, and both refusals are here with the drift that produced them.
//
// Measured 2026-08-25 with `ws.ParseFindings` over the six gate ledgers on disk,
// every total reconciled against that workspace's own `wf stats`: 1065 findings
// under FOUR vocabularies for one scale (BLOCKER universal; MAJOR on five gates,
// CONCERN on one; MINOR on four, ADVISORY on one, NIT on one), 172 of them —
// 16% — at a level no other gate can be read against. The script side is no
// better: an enum validates the VALUE and never the LIST, so the scripts these
// six gates produced carry three enums of their own plus a drifted field NAME,
// one of the three being the ['blocker','advisory'] this repository's template
// shipped into three gated scripts.
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
		t.Fatal("an uncomparable -asks was banked; ten distinct vector names over 13 banked lens records is the drift this closes")
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

// The coverage rule is a NOTE, never a refusal, and it belongs to DELTA rounds
// alone. The first version printed "required of a DELTA round" on round 1 of
// every gate — a FULL round the manual gives three different vectors — which is
// the sixth wave's own class: a note nobody can clear is a note nobody reads.
func TestRoundLens_deltaCoverageNoteIsKindAwareAndNamesWhatIsMissing(t *testing.T) {
	wsDir := newGate(t)
	prompt := filepath.Join(t.TempDir(), "p.md")
	write(t, prompt, "did the closures land\n")

	// Round 1 is FULL. A round-1 vector must draw no delta note.
	if _, err := runWF(t, wsDir, "round", "lens", prompt, "-name", "FACTS", "-model", "opus", "-asks", "facts"); err != nil {
		t.Fatal(err)
	}
	full, err := runWF(t, wsDir, "round", "lens")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(full, "[R3]") {
		t.Fatalf("a FULL round was held to the delta questions: %s", full)
	}

	// Round 2 is a DELTA round, and it asks two of the three.
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-kind", "delta", "-title", "the r1 fixes"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "lens", prompt, "-name", "REPAIRS", "-model", "opus", "-asks", "closure, fix-induced"); err != nil {
		t.Fatalf("a multi-question -asks was refused: %v", err)
	}
	out, err := runWF(t, wsDir, "round", "lens")
	if err != nil {
		t.Fatal(err)
	}
	// The ACTUALLY missing question, not the required list: a roster asking two
	// of the three was reported as asking "none of" all three.
	if !strings.Contains(out, "no banked lens asks ripple") {
		t.Fatalf("the note does not name the one missing question: %s", out)
	}
	if strings.Contains(out, "closure, fix-induced, ripple") {
		t.Fatalf("the note lists questions the roster DOES ask: %s", out)
	}
	// The comma-separated list round-trips: trimmed, canonical, both kept.
	if !strings.Contains(out, "closure+fix-induced") {
		t.Fatalf("the two questions did not round-trip through the file: %s", out)
	}
}

// A lens whose questions are not on file makes the round's coverage UNRECORDED.
// Reporting it as uncovered is the absent-vs-measured-zero collapse this report
// refuses everywhere else — and the first version committed it in two of the
// three places that answer this question.
func TestRoundLens_oneUnstatedLensMakesCoverageUnrecorded(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-kind", "delta", "-title", "the r1 fixes"); err != nil {
		t.Fatal(err)
	}
	// One lens banked the way every lens on disk was banked before -asks.
	write(t, filepath.Join(wsDir, "rounds", "r2", "lens-legacy.md"),
		"# Lens LEGACY\n\nModel: opus\nRecorded: 2026-08-01T00:00:00Z\n\n---\n\nthe prompt as it was banked\n")
	gitIn(t, wsDir, "add", "-A")
	gitIn(t, wsDir, "commit", "-q", "-m", "round 2: lens LEGACY")

	prompt := filepath.Join(t.TempDir(), "p.md")
	write(t, prompt, "did the closures land\n")
	if _, err := runWF(t, wsDir, "round", "lens", prompt, "-name", "REPAIRS", "-model", "opus", "-asks", "closure"); err != nil {
		t.Fatal(err)
	}

	out, err := runWF(t, wsDir, "round", "lens")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "coverage UNRECORDED") {
		t.Fatalf("a roster holding one unstated lens claimed measured coverage: %s", out)
	}
	if strings.Contains(out, "no banked lens asks") {
		t.Fatalf("a claim the workspace cannot support: %s", out)
	}
	if !strings.Contains(out, "asks not stated") {
		t.Fatalf("the legacy lens is not named as unstated: %s", out)
	}
	// And `wf stats` must say the same thing about the same round — two
	// commands contradicting each other about one round is worse than either.
	stats, err := runWF(t, wsDir, "stats")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stats, "states no questions") {
		t.Fatalf("stats disagrees with the roster about round 2: %s", stats)
	}
}

// Reading stays permissive while writing is closed, and that asymmetry is
// load-bearing: TWO workspaces — one closed and pruned, one still OPEN — carry
// ADVISORY, CONCERN and NIT rows, 172 findings of the 1065 measured — and a report that refused to read
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

// The header states questions this build cannot compare. That is a THIRD state,
// and the first version collapsed it into "banked before the question set
// existed" — the absent-vs-measured-zero collapse in reverse. It is reachable
// whenever the closed set is NARROWED: every lens banked under a removed word
// would silently claim to predate the flag.
func TestRoundLens_statedButUncomparableIsNotUnstated(t *testing.T) {
	wsDir := newGate(t)
	write(t, filepath.Join(wsDir, "rounds", "r1", "lens-odd.md"),
		"# Lens ODD\n\nModel: opus\nAsks: repairs, tree-truth\nRecorded: 2026-08-01T00:00:00Z\n\n---\n\nthe prompt\n")
	gitIn(t, wsDir, "add", "-A")
	gitIn(t, wsDir, "commit", "-q", "-m", "round 1: lens ODD")

	out, err := runWF(t, wsDir, "round", "lens")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "asks stated, none comparable") {
		t.Fatalf("a header that said something was reported as saying nothing: %s", out)
	}
	if strings.Contains(out, "not stated") {
		t.Fatalf("stated-but-uncomparable collapsed into unstated: %s", out)
	}
	if !strings.Contains(out, "repairs") || !strings.Contains(out, "tree-truth") {
		t.Fatalf("the uncomparable words are dropped in silence: %s", out)
	}
}

// An UNCOMMITTED lens is not a record, so it must not authorise coverage
// either. This is the ninth wave's scar — a file that would not exist in the
// tag cleared a refusal — applied to the question this wave added. The ghost
// supplies exactly the two questions the committed lens lacks, so dropping the
// validity test makes the note disappear instead of merely widening.
func TestRoundLens_anUncommittedLensDoesNotCoverAQuestion(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-kind", "delta", "-title", "the r1 fixes"); err != nil {
		t.Fatal(err)
	}
	prompt := filepath.Join(t.TempDir(), "p.md")
	write(t, prompt, "did the closures land\n")
	if _, err := runWF(t, wsDir, "round", "lens", prompt, "-name", "REPAIRS", "-model", "opus", "-asks", "closure"); err != nil {
		t.Fatal(err)
	}
	// Written, never committed — every field present, so only tracking separates
	// it from a record.
	write(t, filepath.Join(wsDir, "rounds", "r2", "lens-ghost.md"),
		"# Lens GHOST\n\nModel: opus\nAsks: fix-induced, ripple\nRecorded: 2026-08-01T00:00:00Z\n\n---\n\nthe prompt\n")

	out, err := runWF(t, wsDir, "round", "lens")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "NOT a record") {
		t.Fatalf("an uncommitted lens was read as a record: %s", out)
	}
	if !strings.Contains(out, "no banked lens asks fix-induced, ripple") {
		t.Fatalf("an uncommitted file authorised coverage only a committed one may: %s", out)
	}
}

// The scaffold `wf round new` writes carries the scale itself. It is the one
// place a reviewer who reads nothing else still meets the definition.
func TestRoundNew_scaffoldCarriesTheScale(t *testing.T) {
	wsDir := newGate(t)
	b, err := os.ReadFile(filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"BLOCKER", "MAJOR", "MINOR", "NOTE", "[R2]",
		"what catches it downstream", "NAMES the later gate"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("the scaffold does not carry %q — a reviewer who reads nothing else meets the definition here", want)
		}
	}
}

// The level is checked BEFORE the placeholder skip, and that order is the whole
// guard. `Placeholder()` means "the text cell is empty", so the first version
// let `| 1 | CONCERN |  |  |` through as an unfilled form and the closed scale
// had a hole exactly the width of the scaffold's own row — found by an external
// reviewer over a diff whose four bars were green.
func TestRoundRecord_refusesAnOffScaleLevelOnAnEmptyRow(t *testing.T) {
	wsDir := newGate(t)
	src := filepath.Join(t.TempDir(), "findings.md")
	write(t, src, "# Findings\n\n| # | level | finding | closed by |\n|---|-------|---------|-----------|\n"+
		"| 1 | CONCERN |  |  |\n")

	if _, err := runWF(t, wsDir, "round", "record", src); err == nil {
		t.Fatal("an off-scale level rode in on an empty row; a form that teaches the wrong word is how the next round learns it")
	} else if !strings.Contains(err.Error(), "CONCERN") {
		t.Fatalf("the refusal does not name the level: %v", err)
	}

	// And an empty LEVEL on an empty row is still a form, which must stay legal:
	// that is the scaffold nobody has filled yet.
	blank := filepath.Join(t.TempDir(), "blank.md")
	write(t, blank, "# Findings\n\n| # | level | finding | closed by |\n|---|-------|---------|-----------|\n"+
		"| 1 |  |  |  |\n")
	if _, err := runWF(t, wsDir, "round", "record", blank); err != nil {
		t.Fatalf("the untouched form was refused: %v", err)
	}
}

// The refusal that closes the header-only change had NO test at all: the
// mutation sweep killed 25 guards and left this one, the flagship of its own
// claim. Correcting `-asks` on an unchanged prompt used to exit zero as
// "unchanged" and leave the old claim standing as the round's coverage.
func TestRoundLens_refusesAChangedHeaderOnAnUnchangedPrompt(t *testing.T) {
	wsDir := newGate(t)
	prompt := filepath.Join(t.TempDir(), "p.md")
	write(t, prompt, "did the closures land\n")
	if _, err := runWF(t, wsDir, "round", "lens", prompt, "-name", "V", "-model", "opus", "-asks", "closure"); err != nil {
		t.Fatal(err)
	}
	// Same prompt, different questions.
	_, err := runWF(t, wsDir, "round", "lens", prompt, "-name", "V", "-model", "opus", "-asks", "ripple")
	if err == nil {
		t.Fatal("a changed -asks on an unchanged prompt was accepted; the banked claim stands and nothing says so")
	}
	if !strings.Contains(err.Error(), "closure") || !strings.Contains(err.Error(), "ripple") {
		t.Fatalf("the refusal does not name both halves: %v", err)
	}
	// Same prompt, NARROWER questions. Membership alone is not equality: with
	// only the "every given question is banked" half, dropping one from a
	// banked pair reads as unchanged, and the record keeps claiming coverage
	// the operator has just withdrawn. The widening direction is caught by the
	// membership test itself; this one is caught only by the count.
	if _, err := runWF(t, wsDir, "round", "lens", prompt, "-name", "W", "-model", "opus", "-asks", "closure,ripple"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "lens", prompt, "-name", "W", "-model", "opus", "-asks", "closure"); err == nil {
		t.Fatal("a NARROWED -asks on an unchanged prompt was accepted; the record still claims a question nobody asked")
	}
	// Same prompt, different model.
	if _, err := runWF(t, wsDir, "round", "lens", prompt, "-name", "V", "-model", "sonnet", "-asks", "closure"); err == nil {
		t.Fatal("a changed -model on an unchanged prompt was accepted")
	}
	// Same prompt, same header: still the "unchanged" path, still exit 0.
	if _, err := runWF(t, wsDir, "round", "lens", prompt, "-name", "V", "-model", "opus", "-asks", "closure"); err != nil {
		t.Fatalf("an identical re-run was refused: %v", err)
	}
}

// A lens banked before `-asks` existed takes the ADDITION path: the flag is
// written into the record and committed, not discarded at exit 0. Measured, all
// 13 lens records on disk predate the flag, so this is the whole population the
// exemption exists for — and the first version merely SAID it recovered them.
func TestRoundLens_completesALegacyHeaderInsteadOfDiscardingTheFlag(t *testing.T) {
	wsDir := newGate(t)
	write(t, filepath.Join(wsDir, "rounds", "r1", "lens-legacy.md"),
		"# Lens LEGACY\n\nModel: opus\nRecorded: 2026-08-01T00:00:00Z\n\n---\n\nthe prompt as it was banked\n")
	gitIn(t, wsDir, "add", "-A")
	gitIn(t, wsDir, "commit", "-q", "-m", "round 1: lens LEGACY")

	prompt := filepath.Join(t.TempDir(), "p.md")
	write(t, prompt, "the prompt as it was banked\n")
	out, err := runWF(t, wsDir, "round", "lens", prompt, "-name", "LEGACY", "-model", "opus", "-asks", "closure,ripple")
	if err != nil {
		t.Fatalf("the addition path refused a legacy record: %v", err)
	}
	if !strings.Contains(out, "predates -asks") {
		t.Fatalf("the flag was taken without saying what happened: %s", out)
	}
	roster, err := runWF(t, wsDir, "round", "lens")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(roster, "closure+ripple") {
		t.Fatalf("the questions did not reach the record — the flag was discarded at exit 0: %s", roster)
	}
}

// "States nothing" and "states something this build cannot compare" are two
// claims, and folding them made the coverage line assert a provenance the
// classifier cannot know — about a header printed correctly two lines above it
// in the same output.
func TestRoundLens_uncomparableCoverageIsNotReportedAsPredatingTheFlag(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-kind", "delta", "-title", "the r1 fixes"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(wsDir, "rounds", "r2", "lens-odd.md"),
		"# Lens ODD\n\nModel: opus\nAsks: repairs, tree-truth\nRecorded: 2026-08-01T00:00:00Z\n\n---\n\nthe prompt\n")
	gitIn(t, wsDir, "add", "-A")
	gitIn(t, wsDir, "commit", "-q", "-m", "round 2: lens ODD")

	out, err := runWF(t, wsDir, "round", "lens")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "cannot compare") {
		t.Fatalf("a stated-but-uncomparable header was not named as such: %s", out)
	}
	if strings.Contains(out, "do not state their questions") {
		t.Fatalf("a header that states two questions was reported as stating none: %s", out)
	}
	stats, err := runWF(t, wsDir, "stats")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stats, "states no questions") {
		t.Fatalf("stats claims a provenance it cannot know: %s", stats)
	}
	if !strings.Contains(stats, "cannot compare") {
		t.Fatalf("stats does not name the uncomparable roster: %s", stats)
	}
}

// A level cell is already trimmed by the parser, so the `wf stats` report must
// read a lower-case ledger — legal markdown, and three guards depend on it.
func TestStats_readsALowerCaseLedger(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 1 | blocker | one the stop rule must still read |  |\n"+
		"| 2 | note | not a defect |  |\n")
	gitIn(t, wsDir, "add", "-A")
	gitIn(t, wsDir, "commit", "-q", "-m", "round 1: record findings")

	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	if !strings.Contains(out, "2 finding(s), 1 blocker(s)") {
		t.Fatalf("a lower-case BLOCKER stopped being read as one: %s", out)
	}
	if !strings.Contains(out, "1 of the findings above are NOTE rows") {
		t.Fatalf("a lower-case NOTE stopped being read as one: %s", out)
	}
	if strings.Contains(out, "carry a level outside") {
		t.Fatalf("a lower-case ledger was reported as off-scale: %s", out)
	}
}
