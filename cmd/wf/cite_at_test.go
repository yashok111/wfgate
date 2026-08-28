// A citation is verified against a REVISION, and which revision is the whole
// question after a run round. Measured on mocker P3c: the run moved the target
// by the 675 lines the run itself wrote, `wf check cite` reported 16 failures,
// and every one was pure drift — the document was correct at the revision it
// was frozen against. That sentence was written by hand into the round's ledger
// because no command could make it.
package main

import (
	"os"
	"path/filepath"
	"regexp"
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

// The header abbreviates a REVISION only where abbreviating means something. A
// blanket `short()` printed `refs/hea` for `refs/heads/main` and — worse —
// `gate-r10` for `gate-r10^`, a real and different commit, with the note below
// the header then disagreeing with it.
func TestCiteAt_aRefThatIsNotAShaIsNotAbbreviated(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	moveTarget(t, wsDir)
	// The moved target makes the citation fail; the header is the point.
	out, _ := runWF(t, wsDir, "check", "cite", "-at", "refs/heads/main")
	mustContain(t, out, "against target refs/heads/main")
	mustNotContain(t, out, "against target refs/hea ")
}

// `-at frozen` must not be told to re-run with `-at frozen`. The drift note is a
// statement about the WORKING TREE having moved past the freeze; printed over a
// run that already read the frozen revision it is advice to do what was just
// done.
func TestCiteDriftNote_isNotPrintedWhenAlreadyReadingTheFrozenRevision(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	moveTarget(t, wsDir)
	// A citation that is broken at BOTH revisions, so `-at frozen` has findings.
	art := filepath.Join(filepath.Dir(wsDir), "broken.md")
	write(t, art, "# More\n\nNot there at any revision: `src/main.go:999`.\n")
	if _, err := runWF(t, wsDir, "add", art); err != nil {
		t.Fatal(err)
	}
	out, _ := runWF(t, wsDir, "check", "cite", "-at", "frozen")
	mustContain(t, out, "citation covers 999")
	mustNotContain(t, out, "DRIFT rather than error")
}

// A freeze that recorded no resolvable target ref is refused by name, not
// answered with a screenful of read errors against a revision called
// "(unknown)".
func TestCiteAt_anUnresolvableFrozenRefIsRefused(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	fm := filepath.Join(wsDir, "rounds", "r1", "freeze.json")
	b, err := os.ReadFile(fm)
	if err != nil {
		t.Fatal(err)
	}
	// The literal `(unknown)` is what freeze writes when it could not resolve the
	// target's HEAD — a real state, and the one a bare `== ""` check misses.
	re := regexp.MustCompile(`"targetRef":\s*"[^"]*"`)
	if !re.MatchString(string(b)) {
		t.Fatalf("the manifest must carry a targetRef to blank:\n%s", b)
	}
	write(t, fm, re.ReplaceAllString(string(b), `"targetRef": "(unknown)"`))
	out, err := runWF(t, wsDir, "check", "cite", "-at", "frozen")
	if err == nil {
		t.Fatalf("want a refusal:\n%s", out)
	}
	if !strings.Contains(out+err.Error(), "without a resolvable target ref") {
		t.Fatalf("the refusal must name why: %v\n%s", err, out)
	}
	// And the drift note, which reads the same field, stays silent rather than
	// telling the operator to compare against a revision called "(unknown)".
	// It needs a MOVED target and a finding to be reachable at all.
	moveTarget(t, wsDir)
	out, _ = runWF(t, wsDir, "check", "cite")
	mustContain(t, out, "anchor \"NeedleFn\" not in")
	mustNotContain(t, out, "DRIFT rather than error")
}

// ONE revision answers both questions. Resolving the path index against the
// working tree while reading the bytes from a revision reports a file the
// revision does not have as read errors, and vice versa.
func TestCiteAt_indexAndBytesComeFromTheSameRevision(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	// A file that exists ONLY after the freeze, cited by a new artifact.
	target := filepath.Join(filepath.Dir(wsDir), "target")
	write(t, filepath.Join(target, "src", "later.go"), "package main\n\nfunc LaterFn() {}\n")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-q", "-m", "a file the freeze never saw")
	art := filepath.Join(filepath.Dir(wsDir), "later.md")
	write(t, art, "# Later\n\nA file added after the freeze: `src/later.go:3#LaterFn`.\n")
	if _, err := runWF(t, wsDir, "add", art); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "cite", "-at", "frozen")
	if err == nil {
		t.Fatalf("a file the frozen revision does not have cannot resolve there:\n%s", out)
	}
	// It must fail as an UNRESOLVED path — the index is the frozen one — never
	// as a read error against a path the index claimed to have.
	mustContain(t, out, "no tracked file matches src/later.go")
	mustNotContain(t, out, "read src/later.go")
}
