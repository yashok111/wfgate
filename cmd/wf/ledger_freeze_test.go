// The ledger and the freeze, tested at the shape both documents call the worst:
// a command that exits zero having checked nothing. Each case here is a defect
// that was live — an open BLOCKER carried past a freeze by a pipe character, a
// deleted ledger skipped in silence, and a REFUSED re-freeze that had already
// rewritten the manifest it refused to rewrite.
package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// newGate builds the smallest workspace the freeze path needs: a target
// repository, one artifact, and round 1 open. It returns the workspace dir.
func newGate(t *testing.T) string {
	t.Helper()
	setGitEnv(t)
	tmp := t.TempDir()

	target := filepath.Join(tmp, "target")
	write(t, filepath.Join(target, "src", "main.go"), "package main\n\nfunc NeedleFn() {}\n")
	gitIn(t, target, "init", "-q", "-b", "main")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-q", "-m", "initial")

	wsDir := filepath.Join(tmp, "ws")
	_, err := runWF(t, tmp, "init", "demo", "-target", target, "-dir", wsDir)
	noErr(t, err, "init")

	art := filepath.Join(tmp, "decisions.md")
	write(t, art, "# Decisions\n\nEntry point: `src/main.go:3#NeedleFn`.\n")
	_, err = runWF(t, wsDir, "add", art)
	noErr(t, err, "add")

	_, err = runWF(t, wsDir, "round", "new", "-title", "initial review")
	noErr(t, err, "round new")
	return wsDir
}

func ledger(t *testing.T, wsDir string, round int, table string) {
	t.Helper()
	write(t, filepath.Join(wsDir, "rounds", "r"+strconv.Itoa(round), "findings.md"),
		"# Findings\n\n| # | level | finding | closed by |\n|---|-------|---------|-----------|\n"+table)
}

// A finding that quotes a pipe escapes it, as markdown requires. The first
// splitter cut on every `|` and read the fourth fragment as the closure, so
// this row scored as "closed by prose" and the round froze with the blocker
// open. The tag is one-way.
func TestFreezeRefusesNothing_escapedPipeKeepsTheBlockerOpen(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, `| 1 | BLOCKER | the regex accepts `+"`a \\| b`"+` and must not |  |`+"\n")

	out, err := runWF(t, wsDir, "freeze")
	if err == nil {
		t.Fatalf("freeze must refuse an open blocker whose text carries an escaped pipe, got:\n%s", out)
	}
	mustContain(t, err.Error(), "blocker(s) without closed by")
	mustContain(t, out, "BLOCKER open: the regex accepts")
	if _, e := os.Stat(filepath.Join(wsDir, "rounds", "r1", "freeze.json")); e == nil {
		t.Fatal("a refused freeze must not leave a manifest behind")
	}
}

// A row the parser cannot split into four cells is refused by NAME. Skipping it
// is how three open blockers froze green: the row was invisible and nothing
// said so.
func TestFreezeRefusesMalformedRow(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 1 | BLOCKER | still open |\n")

	if out, err := runWF(t, wsDir, "freeze"); err == nil {
		t.Fatalf("freeze must refuse a 3-cell row, got:\n%s", out)
	} else {
		mustContain(t, err.Error(), "not 4", "escape a literal pipe")
	}
}

// A ledger with no table at all validates nothing, and freeze — the command
// that IS the enforcement — used to print its ordinary success block over it.
func TestFreezeRefusesUnparsableLedger(t *testing.T) {
	wsDir := newGate(t)
	write(t, filepath.Join(wsDir, "rounds", "r1", "findings.md"),
		"# Findings\n\n1. BLOCKER the thing is broken and nobody closed it\n")

	if out, err := runWF(t, wsDir, "freeze"); err == nil {
		t.Fatalf("freeze must refuse a ledger with no table, got:\n%s", out)
	} else {
		mustContain(t, err.Error(), "no findings table recognised")
	}
}

// Deleting the ledger is not the same as a ledger with nothing in it.
func TestFreezeRefusesMissingLedger(t *testing.T) {
	wsDir := newGate(t)
	if err := os.Remove(filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	if out, err := runWF(t, wsDir, "freeze"); err == nil {
		t.Fatalf("freeze must refuse a missing ledger, got:\n%s", out)
	} else {
		mustContain(t, err.Error(), "cannot validate a ledger it cannot read")
	}
}

// The decisive one. A refused re-freeze must change NOTHING: the manifest it
// refuses to rewrite is what `wf verify` reads, so a version that wrote first
// and refused second converted a MISMATCH over a post-freeze edit into "clean"
// — the command that said no had already blessed what it refused.
func TestRefusedRefreezeLeavesTheManifestIntact(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 1 | BLOCKER |  |  |\n")

	out, err := runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze r1")
	mustContain(t, out, "frozen round 1", "gate-r1")

	manifest := filepath.Join(wsDir, "rounds", "r1", "freeze.json")
	before, err := os.ReadFile(manifest)
	noErr(t, err, "read manifest")

	// Smuggle a change into a frozen artifact and commit it, the way a fix
	// applied after the freeze does.
	art := filepath.Join(wsDir, "artifacts", "decisions.md")
	write(t, art, "# Decisions\n\nSMUGGLED: nobody reviewed this line.\n")
	gitIn(t, wsDir, "add", "artifacts/decisions.md")
	gitIn(t, wsDir, "commit", "-q", "-m", "post-freeze edit")

	out, err = runWF(t, wsDir, "verify")
	if err == nil {
		t.Fatalf("verify must MISMATCH over a post-freeze edit, got:\n%s", out)
	}
	mustContain(t, out, "MISMATCH", "Do not launch")

	if out, err := runWF(t, wsDir, "freeze"); err == nil {
		t.Fatalf("re-freeze must refuse, got:\n%s", out)
	} else {
		mustContain(t, err.Error(), "already exists")
	}

	after, err := os.ReadFile(manifest)
	noErr(t, err, "re-read manifest")
	if string(after) != string(before) {
		t.Fatalf("the refused freeze rewrote the manifest:\nbefore:\n%s\nafter:\n%s", before, after)
	}

	out, err = runWF(t, wsDir, "verify")
	if err == nil {
		t.Fatalf("verify must still MISMATCH after the refused re-freeze, got:\n%s", out)
	}
	mustContain(t, out, "MISMATCH", "Do not launch")
}
