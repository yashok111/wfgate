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
	// The ANNOUNCEMENT is not the exclusion. Asserting only the suffix passed
	// with the `continue` deleted: the round was named as excluded AND listed
	// in the basis, in one line, with every bar green. What is asserted is the
	// basis itself.
	basis := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "prediction basis") {
			basis = l
		}
	}
	if basis == "" {
		t.Fatalf("no prediction basis line:\n%s", out)
	}
	head, _, _ := strings.Cut(basis, " (round(s)")
	if strings.Contains(head, "2") && !strings.Contains(head, "NOT A PASS") {
		t.Fatalf("round 2 is excluded and still counted in the basis: %q", head)
	}
}

// The freeze-time half must fire on the HAND path, which is the whole measured
// population: twelve tag moves over five rounds, none of them through
// `-refreeze`, so a note armed on `freeze.json`'s `refreezes` printed on none
// of them. That is the field the same commit removes from `wf stats`.
func TestTheFreezeNoteFiresOnAHandMovedTagToo(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "edits.txt")
	write(t, p, "=== artifacts/decisions.md  count=1  note=own repair\n<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", p); err != nil {
		t.Fatal(err)
	}
	if _, err := (gitx.Repo{Dir: wsDir}).Run("tag", "-d", "gate-r1"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "freeze")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "RE-FROZEN") {
		t.Fatal("this is the hand path; `-refreeze` was never run")
	}
	if !strings.Contains(out, "were applied since this round's FIRST publication") {
		t.Fatalf("the operator must be told at freeze time, not only by `wf stats`:\n%s", out)
	}
}

// `wf apply` predicts FROM the basis, so it must name what left it. This needs
// a surviving priceable round beside the excluded one: with none, the note is
// suppressed for want of history and an assertion on it passes vacuously.
func TestApplyNamesTheRepublishedExclusion(t *testing.T) {
	wsDir := newGate(t)
	repl := []string{"Entry point:", "The entry point is:", "The slice entry point is:", "This slice enters at:"}
	fix := func(i int, note string) {
		t.Helper()
		p := filepath.Join(t.TempDir(), note+".txt")
		write(t, p, "=== artifacts/decisions.md  count=1  note="+note+"\n<<<<\n"+repl[i]+"\n====\n"+repl[i+1]+"\n>>>>\n")
		if _, err := runWF(t, wsDir, "apply", "-f", p); err != nil {
			t.Fatal(err)
		}
	}
	// Rounds 1-3, each frozen; round 2's window carries a clean fix, so it is
	// priceable and stays in the basis.
	for n := 1; n <= 3; n++ {
		reviewedRound(t, wsDir, n)
		if _, err := runWF(t, wsDir, "freeze"); err != nil {
			t.Fatal(err)
		}
		if n < 3 {
			fix(n-1, "r"+itoa(n))
			if _, err := runWF(t, wsDir, "round", "new", "-kind", "delta"); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Round 3's tag now moves over a batch of its own.
	fix(2, "own")
	if _, err := runWF(t, wsDir, "freeze", "-refreeze"); err != nil {
		t.Fatal(err)
	}
	stats, _ := runWF(t, wsDir, "stats")
	if !strings.Contains(stats, "round(s) 3 excluded — their tag moved over their OWN repairs") {
		t.Fatalf("round 3 must be excluded:\n%s", stats)
	}
	if !strings.Contains(stats, "prediction basis — rounds 2,") && !strings.Contains(stats, "prediction basis — rounds 2 ") {
		t.Fatalf("round 2 must SURVIVE in the basis, or `wf apply` prints no prediction and this asserts nothing:\n%s", stats)
	}
	// Big enough to predict five findings at the basis's 50 per 100: the note
	// is suppressed below that, and a small batch would have made the assertion
	// below vacuous.
	grown := "The slice begins at:"
	for i := 0; i < 12; i++ {
		grown += "\nline " + itoa(i)
	}
	p := filepath.Join(t.TempDir(), "next.txt")
	write(t, p, "=== artifacts/decisions.md  count=1  note=next\n<<<<\n"+repl[3]+"\n====\n"+grown+"\n>>>>\n")
	ap, err := runWF(t, wsDir, "apply", "-f", p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ap, "predicts ~") {
		t.Fatalf("no prediction, so the exclusion note cannot be asserted:\n%s", ap)
	}
	if !strings.Contains(ap, "moved over their OWN repairs") {
		t.Fatalf("`wf apply` predicts from the basis and must name what left it:\n%s", ap)
	}
}

// The tag can move with no second freeze commit at all — `git tag -f` is one
// command. Counting freeze commits was a blind spot of the same shape as the
// manifest field this function replaced, and deleting that count left `wf
// stats` byte-identical on all six workspaces on disk.
func TestATagForcedOverWithNoSecondFreezeCommitIsStillAMove(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	first := tagCommit(t, wsDir, "gate-r1")
	p := filepath.Join(t.TempDir(), "edits.txt")
	write(t, p, "=== artifacts/decisions.md  count=1  note=own repair\n<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", p); err != nil {
		t.Fatal(err)
	}
	repo := gitx.Repo{Dir: wsDir}
	if _, err := repo.Run("tag", "-f", "-a", "gate-r1", "-m", "moved by hand"); err != nil {
		t.Fatal(err)
	}
	if tagCommit(t, wsDir, "gate-r1") == first {
		t.Fatal("the tag did not move, so this asserts nothing")
	}
	out, _ := runWF(t, wsDir, "stats")
	if !strings.Contains(out, "published more than once (1)") {
		t.Fatalf("a forced tag writes NO freeze commit and still moves the tag:\n%s", out)
	}
}

// Round numbers reach two digits, and the measured population is rounds 13-17.
// A regex of `\d` instead of `\d+` passed every test and erased the whole
// feature on the only corpus that shows it.
func TestARepublishedRoundIsFoundAtTwoDigits(t *testing.T) {
	for _, subj := range []string{"round 13: freeze 2 artifact(s)", "round 7: freeze 1 artifact(s)"} {
		m := freezeSubjectRe.FindStringSubmatch(subj)
		if m == nil {
			t.Fatalf("%q must parse as a freeze commit", subj)
		}
		want := strings.TrimSuffix(strings.TrimPrefix(subj, "round "), ": freeze 2 artifact(s)")
		want = strings.TrimSuffix(want, ": freeze 1 artifact(s)")
		if m[1] != want {
			t.Errorf("%q yielded round %q, want %q", subj, m[1], want)
		}
	}
}
