// A command that picks its target round by INFERENCE says which round it
// picked. Measured: a note meant for round 10 was recorded into round 11
// because `wf round new` had advanced the counter between the review and the
// write, and `-round N` stood in a freeze refusal's advice and in no usage
// block. The announcement is the cheap half of that fix; the usage lines are
// the other half and are asserted elsewhere.
//
// Both halves are pinned here, because only the pair is a rule: announcing
// unconditionally would restate a number the operator had just typed on every
// run, which is `dup`'s own 161-groups-of-176 lesson — a line that paints every
// run is read on none.
package main

import (
	"path/filepath"
	"testing"
)

func TestRecordAnnouncesAnInferredRound(t *testing.T) {
	ws := newGate(t)
	ledger(t, ws, 1, "| 1 | MAJOR | a wording this round filed |  |\n")
	led := filepath.Join(ws, "rounds", "r1", "findings.md")

	out, err := runWF(t, ws, "round", "record", led)
	noErr(t, err, "round record")
	mustContain(t, out, "round 1 (the open one", "-round N")
}

func TestRecordSaysNothingWhenTheRoundWasTyped(t *testing.T) {
	ws := newGate(t)
	ledger(t, ws, 1, "| 1 | MAJOR | a wording this round filed |  |\n")
	led := filepath.Join(ws, "rounds", "r1", "findings.md")

	out, err := runWF(t, ws, "round", "record", led, "-round", "1")
	noErr(t, err, "round record -round 1")
	mustNotContain(t, out, "the open one")
}

func TestLensAnnouncesAnInferredRound(t *testing.T) {
	ws := newGate(t)
	prompt := filepath.Join(t.TempDir(), "criterion.md")
	write(t, prompt, "You are the CRITERION lens. Read the decision document.\n")

	out, err := runWF(t, ws, "round", "lens", prompt,
		"-name", "CRITERION", "-model", "sonnet", "-asks", "criterion")
	noErr(t, err, "round lens")
	mustContain(t, out, "round 1 (the open one", "-round N")
}

func TestLensSaysNothingWhenTheRoundWasTyped(t *testing.T) {
	ws := newGate(t)
	prompt := filepath.Join(t.TempDir(), "criterion.md")
	write(t, prompt, "You are the CRITERION lens. Read the decision document.\n")

	out, err := runWF(t, ws, "round", "lens", prompt, "-round", "1",
		"-name", "CRITERION", "-model", "sonnet", "-asks", "criterion")
	noErr(t, err, "round lens -round 1")
	mustNotContain(t, out, "the open one")
}

// The roster branch leads with `round N` of its own, so the banking
// announcement must not reach it: two lines carrying one number is the second
// copy this project spends its rounds removing.
func TestRosterCarriesOneRoundLine(t *testing.T) {
	ws := newGate(t)
	out, err := runWF(t, ws, "round", "lens")
	noErr(t, err, "round lens (roster)")
	mustContain(t, out, "round 1 — NO lens recorded")
	mustNotContain(t, out, "the open one")
}
