package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.sumka.site/yakov/wfgate/internal/gitx"
)

// A tag that moved over the round's OWN repairs prices them as the fix that
// produced its findings. Every window in `wf stats` is bounded by `gate-rN`,
// and the guard against it read `freeze.json`'s `refreezes` — a field only `wf
// freeze -refreeze` writes, so a hand `git tag -d` + `wf freeze` moved the tag
// in silence. Measured on the gate that stopped instead of closing: twelve
// extra freeze commits over five rounds, `refreezes` ZERO in all five, the note
// fired not once, and 611 of 8388 changed lines (7.3%) were priced against the
// round that produced them rather than the round they repaired.
func TestAMovedTagIsDerivedFromGitNotFromTheManifest(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	first := tagCommit(t, wsDir, "gate-r1")

	// The round's own repairs, applied after its reviewers were dispatched.
	p := filepath.Join(t.TempDir(), "edits.txt")
	write(t, p, "=== artifacts/decisions.md  count=1  note=fix\n<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", p); err != nil {
		t.Fatal(err)
	}
	// The hand move the manifest cannot see: delete the tag and freeze again,
	// which is what five rounds of that gate did, twelve times in all.
	if _, err := (gitx.Repo{Dir: wsDir}).Run("tag", "-d", "gate-r1"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if moved := tagCommit(t, wsDir, "gate-r1"); moved == first {
		t.Fatal("the tag did not move, so this asserts nothing")
	}

	out, _ := runWF(t, wsDir, "stats")
	if !strings.Contains(out, "published more than once (1)") {
		t.Fatalf("a moved tag must be named whether or not `-refreeze` wrote the manifest:\n%s", out)
	}
	if !strings.Contains(out, "changed line(s) are priced that way here") {
		t.Fatalf("the note must say how many lines the move re-attributed:\n%s", out)
	}
}

// A refused re-freeze used to commit its manifest before refusing, so a
// workspace from before that fix carries TWO freeze commits for a round whose
// tag never moved and whose reviewers all read the same bytes. Counting freeze
// commits would call that a republication; p2e round 11 is exactly that shape
// and is the only one in the six-workspace corpus.
func TestASecondFreezeCommitThatDidNotMoveTheTagIsNotARepublication(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	first := tagCommit(t, wsDir, "gate-r1")
	// The shape as it exists on disk: a SECOND freeze commit for the round,
	// with the tag still on the first. `wf freeze` cannot produce it any more —
	// the refusal comes before the write since the fifth wave — so it is built
	// here the way the corpus carries it, because the guard exists for exactly
	// the workspaces that predate that fix.
	repo := gitx.Repo{Dir: wsDir}
	man := filepath.Join(wsDir, "rounds", "r1", "freeze.json")
	b, err := os.ReadFile(man)
	if err != nil {
		t.Fatal(err)
	}
	write(t, man, strings.Replace(string(b), "{", "{\n  \"note\": \"rewritten by a refusal that wrote first\",", 1))
	if _, err := repo.Commit("round 1: freeze 1 artifact(s)", []string{filepath.Join("rounds", "r1", "freeze.json")}); err != nil {
		t.Fatal(err)
	}
	if tagCommit(t, wsDir, "gate-r1") != first {
		t.Fatal("the tag moved, so this asserts nothing")
	}
	out, _ := runWF(t, wsDir, "stats")
	if strings.Contains(out, "published more than once") {
		t.Fatalf("the tag never moved, so nobody read different bytes:\n%s", out)
	}
}

// The actionable half. A round whose window prices edits that are not the fix
// it measures has no ratio to correct, so it is excluded the way an authoring
// window already is — and named, because "some rounds are missing" is a
// sentence an operator reads past.
func TestARepublishedRoundLeavesThePredictionBasisByName(t *testing.T) {
	wsDir := newGate(t)
	for _, n := range []int{1, 2} {
		reviewedRound(t, wsDir, n)
		if _, err := runWF(t, wsDir, "freeze"); err != nil {
			t.Fatal(err)
		}
		if n == 1 {
			p := filepath.Join(t.TempDir(), "e1.txt")
			write(t, p, "=== artifacts/decisions.md  count=1  note=fix\n<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
			if _, err := runWF(t, wsDir, "apply", "-f", p); err != nil {
				t.Fatal(err)
			}
			if _, err := runWF(t, wsDir, "round", "new", "-kind", "delta"); err != nil {
				t.Fatal(err)
			}
		}
	}
	base, _ := runWF(t, wsDir, "stats")
	if strings.Contains(base, "tag moved over their OWN repairs") {
		t.Fatalf("no tag has moved yet:\n%s", base)
	}

	// Round 2's tag is now moved over an apply batch of its own.
	p := filepath.Join(t.TempDir(), "e2.txt")
	write(t, p, "=== artifacts/decisions.md  count=1  note=own repair\n<<<<\nThe entry point is:\n====\nThe entry point of the slice is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", p); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "freeze", "-refreeze")
	if err != nil {
		t.Fatal(err)
	}
	// The prevention half: said at the moment the operator can still act.
	if !strings.Contains(out, "the tag now covers them") {
		t.Fatalf("`-refreeze` must say what the tag moved over:\n%s", out)
	}

	out, _ = runWF(t, wsDir, "stats")
	if !strings.Contains(out, "round(s) 2 excluded — their tag moved over their OWN repairs") {
		t.Fatalf("a contaminated window must leave the prediction basis BY NUMBER:\n%s", out)
	}
}
