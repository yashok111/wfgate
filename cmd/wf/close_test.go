package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Everything a close refuses, it refuses BEFORE writing. `wf freeze` once wrote
// and committed its manifest and only then refused to re-freeze, so the command
// that said no had already blessed what it refused.
func TestCloseRefusesBeforeWriting(t *testing.T) {
	t.Run("an unfrozen last round", func(t *testing.T) {
		wsDir := newGate(t)
		_, err := runWF(t, wsDir, "close")
		if err == nil {
			t.Fatal("want a refusal over an open round")
		}
		if _, err := os.Stat(filepath.Join(wsDir, closeDir)); !os.IsNotExist(err) {
			t.Fatal("the refusal wrote a snapshot anyway")
		}
	})

	t.Run("a standing BLOCKER", func(t *testing.T) {
		wsDir := newGate(t)
		if _, err := runWF(t, wsDir, "freeze"); err != nil {
			t.Fatal(err)
		}
		ledger(t, wsDir, 1, "| 1 | BLOCKER | the seam is undecided |  |\n")
		if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
			t.Fatal(err)
		}
		out, err := runWF(t, wsDir, "close")
		if err == nil {
			t.Fatalf("want a refusal over an open blocker, got:\n%s", out)
		}
		if !strings.Contains(err.Error(), "BLOCKER") {
			t.Fatalf("the refusal must name the blocker: %v", err)
		}
	})

	t.Run("a dirty workspace", func(t *testing.T) {
		wsDir := newGate(t)
		if _, err := runWF(t, wsDir, "freeze"); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(wsDir, "artifacts", "stray.md"), "half a draft\n")
		if _, err := runWF(t, wsDir, "close"); err == nil {
			t.Fatal("want a refusal over an uncommitted artifact")
		}
	})
}

// The snapshot is what survives the repository, so it carries what the
// repository is the only holder of: the commit times, the freeze manifests'
// surviving half, and every round's ledger verbatim.
func TestCloseBanksWhatTheRepositoryAloneHolds(t *testing.T) {
	wsDir := newGate(t)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "close")
	noErr(t, err, "close")
	mustContain(t, out, "close/history.tsv", "close/stats.txt", "close/freezes.tsv",
		"close/findings/r1.md", "close/feedback.md", "committed ")

	hist := read(t, filepath.Join(wsDir, closeDir, "history.tsv"))
	if !strings.Contains(hist, "gate: init demo") {
		t.Fatalf("the history must carry the subjects: %s", hist)
	}
	fz := read(t, filepath.Join(wsDir, closeDir, "freezes.tsv"))
	if !strings.HasPrefix(fz, "round\tfrozen\tcommit\ttargetRef\ttool\tciteAnchored\tciteTotal\n") {
		t.Fatalf("freezes.tsv header: %q", fz)
	}
	fb := read(t, filepath.Join(wsDir, closeDir, "feedback.md"))
	for _, s := range closeFeedbackSections {
		if !strings.Contains(fb, "## "+s) {
			t.Fatalf("the scaffold is missing %q", s)
		}
	}
	if !strings.Contains(fb, "## stats — workspace demo") {
		t.Fatal("the scaffold must prefill the derived numbers, not ask for them again")
	}
}

// A form the operator has filled is the one thing a command must never replace,
// and re-closing after a post-gate edit is a normal thing to do.
func TestCloseKeepsAFilledFeedback(t *testing.T) {
	wsDir := newGate(t)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "close"); err != nil {
		t.Fatal(err)
	}
	fb := filepath.Join(wsDir, closeDir, "feedback.md")
	write(t, fb, "# Gate feedback — demo\n\nwhat I actually learned\n")

	out, err := runWF(t, wsDir, "close")
	noErr(t, err, "second close")
	mustContain(t, out, "feedback.md already existed and was kept")
	if got := read(t, fb); !strings.Contains(got, "what I actually learned") {
		t.Fatalf("the filled form was overwritten: %q", got)
	}
}

func TestPruneRefusesWithoutARecord(t *testing.T) {
	wsDir := newGate(t)
	_, err := runWF(t, wsDir, "prune", "-yes")
	if err == nil {
		t.Fatal("want a refusal with no snapshot")
	}
	if !strings.Contains(err.Error(), "wf close") {
		t.Fatalf("the refusal must name the repair: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wsDir, ".git")); err != nil {
		t.Fatal("the refusal deleted something anyway")
	}
}

// Deletion is not reversible and the repository is not recoverable, so the bare
// command reports and stops.
func TestPruneNeedsYes(t *testing.T) {
	wsDir := newGate(t)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "close"); err != nil {
		t.Fatal(err)
	}

	out, err := runWF(t, wsDir, "prune")
	noErr(t, err, "prune dry run")
	mustContain(t, out, "would remove .git", "nothing removed — re-run with -yes")
	if _, err := os.Stat(filepath.Join(wsDir, ".git")); err != nil {
		t.Fatal("the dry run deleted the repository")
	}

	out, err = runWF(t, wsDir, "prune", "-yes")
	noErr(t, err, "prune")
	mustContain(t, out, "removed .git", "removed artifacts", "removed rounds")
	for _, gone := range []string{".git", "artifacts", "rounds"} {
		if _, err := os.Stat(filepath.Join(wsDir, gone)); !os.IsNotExist(err) {
			t.Fatalf("%s survived", gone)
		}
	}
	// The record, and the rules version that briefed the gate, stay.
	for _, kept := range []string{closeDir, "gate.json", "gate-checks.md"} {
		if _, err := os.Stat(filepath.Join(wsDir, kept)); err != nil {
			t.Fatalf("%s was destroyed: %v", kept, err)
		}
	}
}

// A snapshot taken before the last edit is a snapshot of a different gate.
func TestPruneRefusesAStaleRecord(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "close"); err != nil {
		t.Fatal(err)
	}
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=post-gate\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", edits); err != nil {
		t.Fatal(err)
	}

	_, err := runWF(t, wsDir, "prune", "-yes")
	if err == nil {
		t.Fatal("want a refusal over a snapshot older than the artifacts")
	}
	if !strings.Contains(err.Error(), "re-run") {
		t.Fatalf("the refusal must name the repair: %v", err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The skeleton is emitted with the workspace's own target and its own name, and
// never over a file that exists — a skeleton written onto an authored script is
// the whole-file overwrite `wf add` refuses, with the same consequence.
func TestNewScriptSubstitutesAndNeverOverwrites(t *testing.T) {
	wsDir := newGate(t)
	dst := filepath.Join(wsDir, "artifacts", "run.mjs")

	out, err := runWF(t, wsDir, "new-script", "slice-a", "-o", dst)
	noErr(t, err, "new-script")
	mustContain(t, out, "wrote ", "TODO(s)", "that no check can see")

	body := read(t, dst)
	if !strings.Contains(body, "name: 'slice-a'") {
		t.Fatalf("the name was not substituted: %.200s", body)
	}
	target := read(t, filepath.Join(wsDir, "gate.json"))
	if !strings.Contains(target, "target") {
		t.Fatal("the manifest has no target to read")
	}
	if strings.Contains(body, "TARGET_REPO") || strings.Contains(body, "SCRIPT_NAME") {
		t.Fatal("a placeholder survived substitution")
	}
	// The harness the tools cannot check, and which is why this exists at all.
	for _, form := range []string{"async function spawn(", "function live(", "function barFailed(", "YOU OWN", "READ-ONLY"} {
		if !strings.Contains(body, form) {
			t.Fatalf("the skeleton is missing %q", form)
		}
	}

	if _, err := runWF(t, wsDir, "new-script", "slice-a", "-o", dst); err == nil {
		t.Fatal("want a refusal over an existing file")
	}
	if got := read(t, dst); got != body {
		t.Fatal("the refused write changed the file")
	}
}
