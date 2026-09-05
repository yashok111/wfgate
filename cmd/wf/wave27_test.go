package main

// The twenty-seventh wave — the A18 feedback (docs/feedbacks/FEEDBACK-a18.md):
// the gate that closed with three blockers standing, on the owner's call, and
// the section-run signal the manual had described and the tool had only
// approximated.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// gateOfRounds records one ledger per round and freezes each, opening delta
// rounds between them. An empty entry opens a `tool` round instead.
func gateOfRounds(t *testing.T, wsDir string, rows []string) {
	t.Helper()
	for n, row := range rows {
		if n > 0 {
			kind := "delta"
			if row == "" {
				kind = "tool"
			}
			if _, err := runWF(t, wsDir, "round", "new", "-title", "next", "-kind", kind); err != nil {
				t.Fatal(err)
			}
		}
		if row != "" {
			ledger(t, wsDir, n+1, row)
			if _, err := runWF(t, wsDir, "round", "record",
				filepath.Join(wsDir, "rounds", "r"+strconv.Itoa(n+1), "findings.md"), "-round", strconv.Itoa(n+1)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := runWF(t, wsDir, "freeze"); err != nil {
			t.Fatalf("freeze r%d: %v", n+1, err)
		}
	}
}

// A18 §1: the manual's signal is "the same artifact SECTION produces a
// blocker in three consecutive rounds", and `wf stats` implemented only the
// run of ones. A18 ran 3, 1, 2, 3 with `decisions.md#D5` drawing a blocker in
// rounds 2-4 while the cited FILES changed every round; nothing fired.
func TestStatsNamesASectionRunWhateverFilesTheBlockersName(t *testing.T) {
	wsDir := newGate(t)
	gateOfRounds(t, wsDir, []string{
		"| 1 | BLOCKER | `decisions.md#A` has no clause for D3's request table | x |\n" +
			"| 2 | BLOCKER | `decisions.md#A.4` leaves the floor open | x |\n" +
			"| 3 | BLOCKER | `decisions.md#D5`'s four sites are ten: internal/bundle/bundle_test.go:187 | x |\n",
		// A bare `#D5` after the row's own `decisions.md#D9` is the same document.
		"| 1 | BLOCKER | `decisions.md#D9`'s list omits `#D5`'s bundle bump (api/openapi.json) | x |\n",
		"| 1 | BLOCKER | `decisions.md#D5` is still not the population: internal/mcp/tools_transfer.go:23 | x |\n" +
			"| 2 | BLOCKER | `decisions.md#D9` closed the instance and not the class | x |\n",
	})
	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out,
		"decisions.md#D5 is cited by a BLOCKER in rounds 1-3 — the same SECTION 3 reviewer rounds running, whatever file each blocker names")
	// D9 drew two rounds, not three; A and A.4 one each; and the blocker
	// counts 3, 1, 2 form no run of ones.
	mustNotContain(t, out, "decisions.md#D9 is cited", "decisions.md#A is cited", "decisions.md#A.4 is cited", "each produced exactly ONE blocker")
}

// A tool round between two reviewer rounds neither breaks the run nor extends
// it — the same reading the run of ones uses.
func TestStatsSectionRunSkipsAToolRound(t *testing.T) {
	wsDir := newGate(t)
	d5 := "| 1 | BLOCKER | `decisions.md#D5` again | x |\n"
	gateOfRounds(t, wsDir, []string{d5, "", d5, d5})
	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out, "decisions.md#D5 is cited by a BLOCKER in rounds 1, 3, 4 — the same SECTION 3 reviewer rounds running")
}

func TestBlockerSections(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"path and anchor", "`decisions.md#D5` is short", []string{"decisions.md#D5"}},
		{"suffix outside the anchor", "`decisions.md#A.9(47a)`'s defeat", []string{"decisions.md#A.9"}},
		{"trailing dot trimmed", "see decisions.md#D5.", []string{"decisions.md#D5"}},
		{"bare anchor attributed to the row's document", "`decisions.md#D9` omits `#D5`'s bump", []string{"decisions.md#D9", "decisions.md#D5"}},
		{"bare anchor before the document, still attributed", "`#D5` is named by `decisions.md#D9`", []string{"decisions.md#D5", "decisions.md#D9"}},
		{"a number is not a heading", "closes #12 in decisions.md#D5", []string{"decisions.md#D5"}},
		{"bare anchor with no document is dropped", "`#D5` is short", nil},
		{"no repeats", "decisions.md#D5 and again decisions.md#D5", []string{"decisions.md#D5"}},
	}
	for _, c := range cases {
		got := blockerSections(c.in)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: blockerSections = %v, want %v", c.name, got, c.want)
		}
	}
}

// A18 §1: `wf close` banked a gate that ended at three blockers without a
// word. A bare close over a blocker round is refused, and the refusal names
// both roads; nothing is written.
func TestCloseRefusesABlockerRoundWithoutEarly(t *testing.T) {
	wsDir := newGate(t)
	gateOfRounds(t, wsDir, []string{"| 1 | BLOCKER | `decisions.md#D5`'s guard counts its own statement | prose: re-asked |\n"})
	out, err := runWF(t, wsDir, "close")
	if err == nil {
		t.Fatalf("close must refuse a blocker round without -early, got:\n%s", out)
	}
	mustContain(t, err.Error(), "AGAINST the stop rule", "wf close -early", "wf round new -kind delta", "nothing was written")
	if _, err := os.Stat(filepath.Join(wsDir, closeDir)); !os.IsNotExist(err) {
		t.Fatal("the refusal wrote a snapshot anyway")
	}
}

// The owner's call is banked in gate.json, rides in the close commit, heads
// `wf stats` and the record, and the second close (after filling feedback.md)
// needs no flag repeated.
func TestCloseEarlyBanksTheOwnersCall(t *testing.T) {
	wsDir := newGate(t)
	gateOfRounds(t, wsDir, []string{"| 1 | BLOCKER | `decisions.md#D5`'s guard counts its own statement | prose: re-asked |\n"})
	out, err := runWF(t, wsDir, "close", "-early", "the one blocker is about the guard, not the subject")
	noErr(t, err, "close -early")
	mustContain(t, out, "closed AGAINST the stop rule: round 1 ended with 1 BLOCKER(s) standing and the owner chose to build — the one blocker is about the guard")

	status, err := runWF(t, wsDir, "status")
	noErr(t, err, "status")
	mustContain(t, status, "tree clean")
	var m struct {
		ClosedEarly *struct {
			Round, Blockers int
			Reason          string
		} `json:"closedEarly"`
	}
	b, err := os.ReadFile(filepath.Join(wsDir, "gate.json"))
	noErr(t, err, "read gate.json")
	if err := json.Unmarshal(b, &m); err != nil || m.ClosedEarly == nil || m.ClosedEarly.Round != 1 || m.ClosedEarly.Blockers != 1 {
		t.Fatalf("gate.json must carry the call: %s (%v)", b, err)
	}

	stats, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	// In the header, before the first round line.
	if i, j := strings.Index(stats, "closed AGAINST the stop rule"), strings.Index(stats, "round 1 ["); i < 0 || j < 0 || i > j {
		t.Fatalf("stats must head with the call, got:\n%s", stats)
	}
	mustContain(t, read(t, filepath.Join(wsDir, closeDir, "feedback.md")), "Closed AGAINST the stop rule: round 1 ended with 1 BLOCKER(s) standing")

	out, err = runWF(t, wsDir, "close")
	noErr(t, err, "second close without the flag")
	mustContain(t, out, "closed AGAINST the stop rule")
}

// An absent measurement never prints in the register of a measured one: the
// flag over a round with no BLOCKER would bank a decision nobody made.
func TestCloseEarlyIsRefusedOnARoundThatMetTheRule(t *testing.T) {
	wsDir := newGate(t)
	gateOfRounds(t, wsDir, []string{"| 1 | MAJOR | a real defect the run gate catches | x |\n"})
	out, err := runWF(t, wsDir, "close", "-early", "no reason")
	if err == nil {
		t.Fatalf("close -early must refuse a round with no blocker, got:\n%s", out)
	}
	mustContain(t, err.Error(), "ends ON the stop rule", "nothing was written")
	if _, err := os.Stat(filepath.Join(wsDir, closeDir)); !os.IsNotExist(err) {
		t.Fatal("the refusal wrote a snapshot anyway")
	}
}

// A gate re-opened after an early close is a different ending: the banked call
// is stale and is dropped, and a last round that met the rule closes as one.
func TestCloseEarlyIsDroppedWhenTheGateReopens(t *testing.T) {
	wsDir := newGate(t)
	gateOfRounds(t, wsDir, []string{"| 1 | BLOCKER | `decisions.md#D5` guard | prose: re-asked |\n"})
	_, err := runWF(t, wsDir, "close", "-early", "guards only")
	noErr(t, err, "close -early")
	if _, err := runWF(t, wsDir, "round", "new", "-title", "reopened", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	ledger(t, wsDir, 2, "| 1 | NOTE | checked and clean | |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r2", "findings.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "close")
	noErr(t, err, "close after reopening")
	mustNotContain(t, out, "AGAINST the stop rule")
	mustNotContain(t, read(t, filepath.Join(wsDir, "gate.json")), "closedEarly")
}

// A18 §0: every row of round 1 was recorded with an empty `#`, and a later
// round's brief cannot name a row without one. Numbered in place, continuing
// from the highest id present, and announced only when it happened.
func TestRecordNumbersRowsThatCarryNoId(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 4 | MAJOR | first | x |\n|  | MINOR | second quotes `a \\| b` | x |\n|  | NOTE | third |  |\n")
	out, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")
	mustContain(t, out, "numbered 2 row(s) that carried no #: 5, 6")
	b := read(t, filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	mustContain(t, b, "| 4 | MAJOR | first | x |", "| 5 | MINOR | second quotes `a \\| b` | x |", "| 6 | NOTE | third |  |")

	// Idempotent: a second record over the numbered ledger numbers nothing.
	out, err = runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "second record")
	mustNotContain(t, out, "numbered")
}

// A18 §0 and P4a §0: a closure that names a commit in the TARGET repository
// was refused as a commit claim, and the refusal's advice named a commit the
// workspace does not have. `target:<sha>` is the spelling every reader leaves
// alone, and the refusal now says so.
func TestRoundCloseProseAcceptsATargetSha(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/decisions.md promises a route the tree lacks |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	out, err := runWF(t, wsDir, "round", "close", "1", "-prose", "built in the target at 39f4fc9a")
	if err == nil {
		t.Fatalf("a bare target sha must still be refused, got:\n%s", out)
	}
	mustContain(t, err.Error(), "write it `target:39f4fc9a`")

	out, err = runWF(t, wsDir, "round", "close", "1", "-prose", "built in the target at target:39f4fc9a")
	noErr(t, err, "round close with target: prefix")
	mustContain(t, out, "closed by")
	stats, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, stats, "closed: 0 commit, 1 prose, 0 open")
}

// The stop rule reads the REVIEWER descent: a `tool` round after the blocker
// round dispatched nobody, and a close must look past it to the round that
// did — otherwise a bookkeeping round is the cheapest way to close a gate
// with blockers standing and no record of it.
func TestCloseLooksPastAToolRoundForTheBlockers(t *testing.T) {
	wsDir := newGate(t)
	gateOfRounds(t, wsDir, []string{"| 1 | BLOCKER | `decisions.md#D5` guard | prose: re-asked |\n", ""})
	out, err := runWF(t, wsDir, "close")
	if err == nil {
		t.Fatalf("close must refuse: the last REVIEWER round (1) produced a blocker, got:\n%s", out)
	}
	mustContain(t, err.Error(), "round 1 produced 1 BLOCKER(s)")
	out, err = runWF(t, wsDir, "close", "-early", "guards only")
	noErr(t, err, "close -early past a tool round")
	mustContain(t, out, "round 1 ended with 1 BLOCKER(s) standing")
}
