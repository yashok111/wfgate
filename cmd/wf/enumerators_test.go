// The enumerators and the read-back, tested where they used to report success
// over something they had not looked at: an artifact the lister skipped, a
// commit that swept in a file nobody reviewed, half a batch, a dirty path the
// parser mis-split, and a target that was not there at all.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitOut runs git in dir and returns its stdout.
func gitOut(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	b, err := cmd.Output()
	return string(b), err
}

func targetOf(wsDir string) string { return filepath.Join(filepath.Dir(wsDir), "target") }

// `artifacts/.rules.md` was tracked, committed, and invisible: the lister
// skipped every dot-prefixed NAME, so the file was absent from the freeze
// manifest and from all of verify's classes. Its content could be rewritten
// after the freeze and verify still printed "clean".
func TestVerifySeesADotNamedArtifact(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 1 | BLOCKER |  |  |\n")

	src := filepath.Join(filepath.Dir(wsDir), "rules.md")
	write(t, src, "# Rules\n\nThe reviewed rule.\n")
	_, err := runWF(t, wsDir, "add", src, "-as", ".rules.md")
	noErr(t, err, "add dot-named artifact")

	out, err := runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze")
	mustContain(t, out, "artifacts/.rules.md")

	write(t, filepath.Join(wsDir, "artifacts", ".rules.md"), "# Rules\n\nSMUGGLED: nobody reviewed this.\n")
	gitIn(t, wsDir, "add", "-A")
	gitIn(t, wsDir, "commit", "-q", "-m", "rewrite the hidden artifact")

	out, err = runWF(t, wsDir, "verify")
	if err == nil {
		t.Fatalf("verify must catch a rewritten dot-named artifact, got:\n%s", out)
	}
	mustContain(t, out, "MISMATCH", "artifacts/.rules.md")
}

// `git commit` with no pathspec commits the whole INDEX. A half-written draft
// the operator had staged by hand rode into the tool's own commit and became
// tracked — which then passed freeze's dirty check and was hashed into the
// manifest as a reviewed artifact.
func TestCommitDoesNotSweepInAPreStagedDraft(t *testing.T) {
	wsDir := newGate(t)

	write(t, filepath.Join(wsDir, "artifacts", "draft.md"), "half-written, not for review\n")
	gitIn(t, wsDir, "add", "artifacts/draft.md")

	src := filepath.Join(filepath.Dir(wsDir), "second.md")
	write(t, src, "# Second\n\nreviewed content\n")
	_, err := runWF(t, wsDir, "add", src)
	noErr(t, err, "add")

	files, err := gitOut(t, wsDir, "show", "--name-only", "--format=", "HEAD")
	noErr(t, err, "show")
	if strings.Contains(files, "artifacts/draft.md") {
		t.Fatalf("the pre-staged draft rode into the tool's commit:\n%s", files)
	}
	if !strings.Contains(files, "artifacts/second.md") {
		t.Fatalf("the added artifact is missing from the commit:\n%s", files)
	}
}

// Decode stops at the end of the first JSON value and discards the rest in
// silence, so a file holding two concatenated arrays applied half the batch and
// reported success.
func TestApplyRefusesTwoConcatenatedArrays(t *testing.T) {
	wsDir := newGate(t)
	art := filepath.Join(wsDir, "artifacts", "decisions.md")
	write(t, art, "AAA and BBB\n")
	gitIn(t, wsDir, "add", "-A")
	gitIn(t, wsDir, "commit", "-q", "-m", "seed")

	batch := filepath.Join(filepath.Dir(wsDir), "edits.json")
	write(t, batch, `[{"file":"artifacts/decisions.md","old":"AAA","new":"XXX"}]
[{"file":"artifacts/decisions.md","old":"BBB","new":"YYY"}]`)

	out, err := runWF(t, wsDir, "apply", "-f", batch)
	if err == nil {
		t.Fatalf("apply must refuse a file holding two batches, got:\n%s", out)
	}
	mustContain(t, err.Error(), "trailing data")
	b, _ := os.ReadFile(art)
	if string(b) != "AAA and BBB\n" {
		t.Fatalf("nothing may be written by a refused batch, got %q", b)
	}
}

// git QUOTES a path containing a space, and the caller that split the porcelain
// line on its last space read `draft.md"` as the path — so a dirty ARTIFACT was
// counted as round bookkeeping and verify printed "commit it; do not
// re-freeze" over a file that differs from the freeze.
func TestVerifyClassifiesADirtyArtifactWithASpaceInItsName(t *testing.T) {
	wsDir := newGate(t)
	// The untouched scaffold freezes fine, and freeze commits the manifest, so
	// the ONE piece of dirt below is the whole population verify classifies —
	// which is what makes this test decisive about the classification.
	_, err := runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze")
	if out, err := runWF(t, wsDir, "status"); err != nil || !strings.Contains(out, "tree clean") {
		t.Fatalf("the tree must be clean before the one dirty file is made: %v\n%s", err, out)
	}

	write(t, filepath.Join(wsDir, "artifacts", "my draft.md"), "added after the freeze, never reviewed\n")

	out, err := runWF(t, wsDir, "verify")
	if err == nil {
		t.Fatalf("verify must refuse, got:\n%s", out)
	}
	mustContain(t, out, "my draft.md")
	// The dirt IS an artifact, so the reassuring bookkeeping verdict must not
	// print. git quotes a path containing a space in plain porcelain, and the
	// caller that split on the last space read `draft.md"` — outside
	// artifacts/, hence "commit it; do not re-freeze" over an unreviewed file.
	mustNotContain(t, out, "every mismatch above is uncommitted round bookkeeping")
	// And the path is printed as git means it, not as git quotes it.
	mustNotContain(t, out, `"artifacts/my draft.md"`)
}

// A gate frozen against a target that could not be resolved wrote the literal
// string "(unknown)" into the manifest, and verify then compared "(unknown)"
// with "(unknown)" and printed clean — forever, until the target came back.
func TestFreezeRefusesAnUnresolvableTarget(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 1 | BLOCKER |  |  |\n")
	if err := os.Rename(targetOf(wsDir), targetOf(wsDir)+"-moved"); err != nil {
		t.Fatal(err)
	}
	if out, err := runWF(t, wsDir, "freeze"); err == nil {
		t.Fatalf("freeze must refuse a target it cannot resolve, got:\n%s", out)
	} else {
		mustContain(t, err.Error(), "no baseline to verify against")
	}
}

// HEAD matching is not the whole question: a cited line rewritten WITHOUT a
// commit leaves HEAD, the tag and the manifest all agreeing while the bytes a
// reviewer opens have changed underneath them.
func TestVerifyAndDriftCatchADirtyTarget(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 1 | BLOCKER |  |  |\n")
	_, err := runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze")

	write(t, filepath.Join(targetOf(wsDir), "src", "main.go"), "package main\n\nfunc Rewritten() {}\n")

	out, err := runWF(t, wsDir, "verify")
	if err == nil {
		t.Fatalf("verify must refuse a dirty target, got:\n%s", out)
	}
	mustContain(t, out, "target is dirty", "src/main.go")

	out, err = runWF(t, wsDir, "check", "drift")
	if err == nil {
		t.Fatalf("drift must refuse a dirty target, got:\n%s", out)
	}
	mustContain(t, out, "tracked file(s) differ from it")
	mustNotContain(t, out, "nothing to re-read")
}

// The wiring, not the helper. Each of these was a guard whose FUNCTION had a
// test while the line that calls it did not: delete the call and the bars stay
// green, which is a guard that exists only in principle.
func TestWiring(t *testing.T) {
	t.Run("apply reads back what it wrote", func(t *testing.T) {
		wsDir := newGate(t)
		art := filepath.Join(wsDir, "artifacts", "decisions.md")
		write(t, art, "alpha here\n")
		gitIn(t, wsDir, "add", "-A")
		gitIn(t, wsDir, "commit", "-q", "-m", "seed")

		batch := filepath.Join(filepath.Dir(wsDir), "e.json")
		write(t, batch, `[{"file":"artifacts/decisions.md","old":"alpha","new":"beta"}]`)
		out, err := runWF(t, wsDir, "apply", "-f", batch)
		noErr(t, err, "apply")
		// The read-back is asserted by its own report line: asserting only that
		// the edit landed would pass with the read-back deleted.
		mustContain(t, out, "verified 1 file(s) byte-for-byte", "committed")
		b, _ := os.ReadFile(art)
		if string(b) != "beta here\n" {
			t.Fatalf("the edit must land, got %q", b)
		}
	})

	t.Run("an unknown check is refused", func(t *testing.T) {
		wsDir := newGate(t)
		if out, err := runWF(t, wsDir, "check", "nonsense"); err == nil {
			t.Fatalf("want a refusal, got:\n%s", out)
		}
	})

	t.Run("an unknown field in the batch is refused", func(t *testing.T) {
		wsDir := newGate(t)
		batch := filepath.Join(filepath.Dir(wsDir), "bad.json")
		write(t, batch, `[{"file":"artifacts/decisions.md","old":"a","new":"b","reason":"typo'd key"}]`)
		if out, err := runWF(t, wsDir, "apply", "-f", batch); err == nil {
			t.Fatalf("want a refusal, got:\n%s", out)
		}
	})

	t.Run("verify names an artifact added after the freeze", func(t *testing.T) {
		wsDir := newGate(t)
		_, err := runWF(t, wsDir, "freeze")
		noErr(t, err, "freeze")
		write(t, filepath.Join(wsDir, "artifacts", "late.md"), "added after the freeze\n")
		gitIn(t, wsDir, "add", "-A")
		gitIn(t, wsDir, "commit", "-q", "-m", "late artifact")
		out, err := runWF(t, wsDir, "verify")
		if err == nil {
			t.Fatalf("want a mismatch, got:\n%s", out)
		}
		mustContain(t, out, "not in the manifest (added after the freeze)")
	})

	t.Run("a rewritten manifest does not verify against its own tag", func(t *testing.T) {
		wsDir := newGate(t)
		_, err := runWF(t, wsDir, "freeze")
		noErr(t, err, "freeze")
		// Rewrite an artifact AND its recorded hash, then commit both — the
		// shape a manifest read only from the worktree cannot see.
		art := filepath.Join(wsDir, "artifacts", "decisions.md")
		write(t, art, "SMUGGLED\n")
		sum := sha256.Sum256([]byte("SMUGGLED\n"))
		mf := filepath.Join(wsDir, "rounds", "r1", "freeze.json")
		b, err := os.ReadFile(mf)
		noErr(t, err, "read manifest")
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		m["artifacts"].(map[string]any)["artifacts/decisions.md"] = hex.EncodeToString(sum[:])
		nb, _ := json.MarshalIndent(m, "", "  ")
		write(t, mf, string(nb)+"\n")
		gitIn(t, wsDir, "add", "-A")
		gitIn(t, wsDir, "commit", "-q", "-m", "rewrite the artifact and its hash")

		out, err := runWF(t, wsDir, "verify")
		if err == nil {
			t.Fatalf("a manifest that records its own rewrite must not verify, got:\n%s", out)
		}
		mustContain(t, out, "differs from the copy tagged")
	})

	t.Run("an operator conflict fails the dup check", func(t *testing.T) {
		wsDir := newGate(t)
		write(t, filepath.Join(wsDir, "artifacts", "a.md"), "The client retries when retry_count == 3 in every environment.\n")
		write(t, filepath.Join(wsDir, "artifacts", "b.md"), "The client retries when retry_count != 3 in every environment.\n")
		gitIn(t, wsDir, "add", "-A")
		gitIn(t, wsDir, "commit", "-q", "-m", "two copies")
		out, err := runWF(t, wsDir, "check", "dup")
		if err == nil {
			t.Fatalf("an operator conflict must fail the check, got:\n%s", out)
		}
		mustContain(t, out, "OPERATOR")
	})
}
