// A citation is verified against a REVISION, and which revision is the whole
// question after a run round. Measured on mocker P3c: the run moved the target
// by the 675 lines the run itself wrote, `wf check cite` reported 16 failures,
// and every one was pure drift — the document was correct at the revision it
// was frozen against. That sentence was written by hand into the round's ledger
// because no command could make it.
package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// moveTarget rewrites the target so every citation into it resolves late, the
// way a run round's own commits do.
func moveTarget(t *testing.T, wsDir string) {
	t.Helper()
	target := filepath.Join(filepath.Dir(wsDir), "target")
	write(t, filepath.Join(target, "src", "main.go"),
		"package main\n\n// two lines the run wrote\n// and here is the second\n\nfunc NeedleFn() {}\n")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-q", "-m", "the run wrote this")
}

func TestCiteAt_frozenVerifiesTheBytesReviewersWereHanded(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	moveTarget(t, wsDir)

	out, err := runWF(t, wsDir, "check", "cite")
	if err == nil {
		t.Fatalf("the moved target invalidates the anchor:\n%s", out)
	}
	mustContain(t, out, "against the working tree", "anchor \"NeedleFn\" not in")

	out, err = runWF(t, wsDir, "check", "cite", "-at", "frozen")
	noErr(t, err, "check cite -at frozen")
	mustContain(t, out, "0 finding(s)", "against target ", "was FROZEN with")
	mustNotContain(t, out, "anchor \"NeedleFn\" not in")
}

// The note is what an operator acts on, and it must not print where there is
// nothing to explain: a target that has not moved, or a run with no findings.
func TestCiteDriftNote_firesOnlyOnAMovedTargetWithFindings(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "cite")
	noErr(t, err, "check cite")
	mustNotContain(t, out, "DRIFT rather than error")

	// A finding with the target STILL at the frozen revision is an error, not
	// drift, and the note must not offer the drift reading for it: `-at frozen`
	// would read the same bytes and report the same failure.
	art := filepath.Join(filepath.Dir(wsDir), "broken.md")
	write(t, art, "# More\n\nA line that is not there: `src/main.go:999`.\n")
	if _, err := runWF(t, wsDir, "add", art); err != nil {
		t.Fatal(err)
	}
	out, err = runWF(t, wsDir, "check", "cite")
	if err == nil {
		t.Fatalf("a citation past the end of the file is an error:\n%s", out)
	}
	mustContain(t, out, "citation covers 999")
	mustNotContain(t, out, "DRIFT rather than error")

	moveTarget(t, wsDir)
	out, _ = runWF(t, wsDir, "check", "cite")
	mustContain(t, out, "DRIFT rather than error", "-at frozen", "wf check drift")
	// Round 2 is not open yet, so nothing may claim a run round moved anything.
	mustNotContain(t, out, "`-kind run` round")
}

// A run round moves the target BY CONSTRUCTION — its commits are the run's own
// output — so the note names the kind rather than leaving the operator to
// notice it. This is the sentence mocker P3c's operator wrote by hand.
func TestCiteDriftNote_namesARunRound(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-kind", "run", "-title", "the live run"); err != nil {
		t.Fatal(err)
	}
	moveTarget(t, wsDir)
	out, _ := runWF(t, wsDir, "check", "cite")
	mustContain(t, out, "Round 2 is a `-kind run` round", "the run itself wrote the movement")
}

// `-at frozen` with nothing frozen is refused rather than silently falling back
// to the working tree, which would answer a different question than the one on
// the command line.
func TestCiteAt_frozenWithNothingFrozenIsRefused(t *testing.T) {
	wsDir := newGate(t)
	out, err := runWF(t, wsDir, "check", "cite", "-at", "frozen")
	if err == nil {
		t.Fatalf("want a refusal:\n%s", out)
	}
	if !strings.Contains(out+err.Error(), "nothing is frozen yet") {
		t.Fatalf("the refusal must say why: %v\n%s", err, out)
	}
}

// An explicit revision says so in the header AND in a note: a report that
// answered a different question than the working tree without saying which is
// the class this whole tool exists against.
func TestCiteAt_anExplicitRevisionIsNamed(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	moveTarget(t, wsDir)
	out, err := runWF(t, wsDir, "check", "cite", "-at", "HEAD~1")
	noErr(t, err, "check cite -at HEAD~1")
	mustContain(t, out, "against target HEAD~1", "not the working tree")
}
