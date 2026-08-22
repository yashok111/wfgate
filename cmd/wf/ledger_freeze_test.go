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

// `wf add -as` takes a name from the operator and joined it raw, so a
// traversing name wrote into the repository under review — the one tree the
// hard rule says no command may touch — and left it there untracked, where
// verify (which only compares the target's HEAD) cannot see it.
func TestAddRefusesAnEscapingName(t *testing.T) {
	wsDir := newGate(t)
	src := filepath.Join(filepath.Dir(wsDir), "src.md")
	write(t, src, "smuggled\n")

	out, err := runWF(t, wsDir, "add", src, "-as", "../../target/PWNED.md")
	if err == nil {
		t.Fatalf("add must refuse a name that escapes the workspace, got:\n%s", out)
	}
	mustContain(t, err.Error(), "escapes the workspace")
	if _, e := os.Stat(filepath.Join(filepath.Dir(wsDir), "target", "PWNED.md")); e == nil {
		t.Fatal("the target repository was written into")
	}
}

// A row the parser cannot split is neither a finding nor a form. `freeze` and
// `record` refuse it; the commands that only COUNT must not quietly score it as
// one or the other — a hand awk once closed a gate on 99/27 against a true
// 98/26 by counting the scaffold placeholder, and this is the same error with a
// different sign.
func TestStatsNamesAnUnparsableRow(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 1 | BLOCKER | a real one | 2344d3d8 |\n| 2 | BLOCKER | three cells only |\n")
	gitIn(t, wsDir, "add", "-A")
	gitIn(t, wsDir, "commit", "-q", "-m", "round 1: record findings")

	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out, "1 ledger row(s) are not four cells")
	// The malformed row must not inflate the count.
	mustContain(t, out, "total — 1 finding(s)")
}

// Guards that existed with nothing proving they fire. Each case below fails if
// its guard is removed — which was the audit's finding about all of them.
func TestRefusals(t *testing.T) {
	t.Run("freeze refuses a dirty artifact", func(t *testing.T) {
		wsDir := newGate(t)
		write(t, filepath.Join(wsDir, "artifacts", "decisions.md"), "edited by hand, never applied\n")
		if out, err := runWF(t, wsDir, "freeze"); err == nil {
			t.Fatalf("want a refusal, got:\n%s", out)
		} else {
			mustContain(t, err.Error(), "artifacts are dirty")
		}
	})

	t.Run("freeze refuses a closure naming a commit that does not exist", func(t *testing.T) {
		wsDir := newGate(t)
		ledger(t, wsDir, 1, "| 1 | BLOCKER | the thing | deadbeef1 |\n")
		if out, err := runWF(t, wsDir, "freeze"); err == nil {
			t.Fatalf("want a refusal, got:\n%s", out)
		} else {
			mustContain(t, err.Error(), "not a commit in this workspace")
		}
	})

	t.Run("round new refuses a kind outside the set", func(t *testing.T) {
		wsDir := newGate(t)
		if out, err := runWF(t, wsDir, "round", "new", "-kind", "quick"); err == nil {
			t.Fatalf("want a refusal, got:\n%s", out)
		}
	})

	t.Run("add refuses two untracked drafts that differ", func(t *testing.T) {
		wsDir := newGate(t)
		write(t, filepath.Join(wsDir, "artifacts", "notes.md"), "the draft in place\n")
		other := filepath.Join(filepath.Dir(wsDir), "notes.md")
		write(t, other, "a DIFFERENT draft with the same name\n")
		if out, err := runWF(t, wsDir, "add", other); err == nil {
			t.Fatalf("want a refusal, got:\n%s", out)
		} else {
			mustContain(t, err.Error(), "differs from")
		}
	})

	t.Run("verify names an artifact gone from the tree", func(t *testing.T) {
		wsDir := newGate(t)
		_, err := runWF(t, wsDir, "freeze")
		noErr(t, err, "freeze")
		gitIn(t, wsDir, "rm", "-q", "artifacts/decisions.md")
		gitIn(t, wsDir, "commit", "-q", "-m", "drop the artifact")
		out, err := runWF(t, wsDir, "verify")
		if err == nil {
			t.Fatalf("want a mismatch, got:\n%s", out)
		}
		mustContain(t, out, "gone from the tree")
	})

	t.Run("cite states the non-ASCII anchor population in its header", func(t *testing.T) {
		wsDir := newGate(t)
		write(t, filepath.Join(wsDir, "artifacts", "decisions.md"),
			"# Decisions\n\nEntry: `src/main.go:3#якорь` and `src/main.go:3#NeedleFn`.\n")
		gitIn(t, wsDir, "add", "-A")
		gitIn(t, wsDir, "commit", "-q", "-m", "cyrillic anchor")
		out, _ := runWF(t, wsDir, "check", "cite")
		mustContain(t, out, "anchor(s) not ASCII")
	})
}
