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
	"strings"
	"testing"

	wfgate "github.com/yashok111/wfgate"
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

// reviewedRound leaves a round the way a round that actually RAN leaves it:
// one filed finding, recorded through `wf round record` so the ledger is
// committed. It exists because freeze and close now refuse a round whose ledger
// is the untouched scaffold and whose lenses nobody banked — a round nobody
// reviewed must not read as a round that found nothing — and every fixture
// below that closes or re-freezes a gate was taking exactly that shortcut.
func reviewedRound(t *testing.T, wsDir string, round int) {
	t.Helper()
	ledger(t, wsDir, round, "| 1 | MAJOR | a wording this round filed |  |\n")
	if _, err := runWF(t, wsDir, "round", "record",
		filepath.Join(wsDir, "rounds", "r"+strconv.Itoa(round), "findings.md"),
		"-round", strconv.Itoa(round)); err != nil {
		t.Fatal(err)
	}
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
			mustContain(t, err.Error(), "not a commit in this GATE WORKSPACE")
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

// The cost meter counts an apply by what NOTHING can overwrite. P2f applied all
// 27 of its batches with `wf apply -m "<what the batch closes>"`, and `-m`
// replaces the subject the meter used to read: `wf stats` printed "0 apply
// batch(es)" for all thirteen rounds, the findings-per-edit number the tool
// exists to produce had no population at all, and `wf apply`'s split note never
// fired once because it had no history to predict from.
func TestStatsCountsABatchAppliedWithItsOwnSubject(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=reword\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	out, err := runWF(t, wsDir, "apply", "-f", edits, "-m", "r1: close the seam finding (closes 3)")
	noErr(t, err, "apply with a written subject")
	mustContain(t, out, "committed ")

	out, err = runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out, "1 apply batch(es), 1 edit(s)")
}

// And zero over a whole workspace is a POPULATION. P2f's meter printed "0 apply
// batch(es)" on thirteen rounds without a word, so a number that was absent
// looked measured, and the operator rebuilt it by counting headers in his own
// scratch files.
func TestStatsNamesAnEmptyCostMeter(t *testing.T) {
	wsDir := newGate(t)
	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out, "the cost meter has NO population")
}

// The trailer is written at the END of the message, and everything above it —
// the subject the operator wrote, the per-edit list — is text the batch itself
// carries. Reading the first match would let the batch restate its own size.
func TestStatsReadsTheLastTrailer(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=reword\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	_, err := runWF(t, wsDir, "apply", "-f", edits, "-m", "wf-apply: 999 edit(s) in 9 file(s)")
	noErr(t, err, "apply whose subject quotes a trailer")

	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out, "1 apply batch(es), 1 edit(s)")
}

// writeManual writes the embedded SKILL.md and references under dir (the
// references directory), prefixing the file named stale so it differs.
func writeManual(t *testing.T, dir string, stale string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The front door lives beside references/, and the check reads it too.
	skill, err := wfgate.Docs.ReadFile("skills/workflow-gate/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "SKILL.md"), skill, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, file := range docNames {
		b, err := wfgate.Docs.ReadFile("skills/workflow-gate/references/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if file == stale {
			b = append([]byte("# a stale copy\n"), b...)
		}
		if err := os.WriteFile(filepath.Join(dir, file), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A plugin from any marketplace but a folder is a CACHED COPY, not a view of
// the clone, and refreshing the marketplace does not touch it. Measured: the installed plugin sat at the
// version it was installed at while the clone moved four releases on — every
// reference file differed, and the SKILL.md sessions actually loaded still
// described a rule the manual had replaced. A binary and a skill built from one
// commit are the same document; when they are not, a gate is briefed from one
// and run by the other.
func TestDocsNamesAManualThatDrifted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	registry := func(installPath string) {
		t.Helper()
		dir := filepath.Join(home, ".claude", "plugins")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := `{"plugins":{"wfgate@wfgate-tools":[{"scope":"user","installPath":"` + installPath + `","version":"x"}]}}`
		if err := os.WriteFile(filepath.Join(dir, "installed_plugins.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cache := filepath.Join(home, ".claude", "plugins", "cache", "wfgate-tools", "wfgate")
	// Every reference present and identical except one, so the note has to name
	// the file rather than the directory.
	writeManual(t, filepath.Join(cache, "1.0.0", "skills", "workflow-gate", "references"), "authoring.md")
	registry(filepath.Join(cache, "1.0.0"))

	out, err := runWF(t, home, "docs")
	noErr(t, err, "docs")
	mustContain(t, out, "the manual a session LOADS differs", "drifted: authoring.md")
	if strings.Contains(out, "model-routing.md") {
		t.Fatalf("only the drifted file is named:\n%s", out)
	}

	// The front door drifting is the case that actually bit: a session reads
	// SKILL.md before any reference, and the stale one still said "every agent()
	// call carries an explicit model" with no word about effort.
	writeManual(t, filepath.Join(cache, "1.1.0", "skills", "workflow-gate", "references"), "")
	if err := os.WriteFile(filepath.Join(cache, "1.1.0", "skills", "workflow-gate", "SKILL.md"), []byte("# a stale front door\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry(filepath.Join(cache, "1.1.0"))
	out, err = runWF(t, home, "docs")
	noErr(t, err, "docs with a drifted SKILL.md")
	mustContain(t, out, "drifted: SKILL.md")

	// An update leaves the previous version's directory behind. The check reads
	// the REGISTERED path: a glob over the cache found the abandoned copy and
	// reported drift no session could see, which is the false red this binary
	// refuses to print anywhere else.
	writeManual(t, filepath.Join(cache, "2.0.0", "skills", "workflow-gate", "references"), "")
	registry(filepath.Join(cache, "2.0.0"))
	out, err = runWF(t, home, "docs")
	noErr(t, err, "docs after the update")
	if strings.Contains(out, "differs from the one this binary carries") {
		t.Fatalf("an install in step with the binary is not drift, and an abandoned cache is not an install:\n%s", out)
	}
}

// A plugin installed from a FOLDER marketplace is read in place: measured
// 2026-10-04, a session loaded wfgate's skill, agent and hooks.json from the
// clone while the registered installPath pointed at a cache nothing read. The
// check compared that cache, so it named a copy no session loaded and could
// not see the drift that is real in place — a binary behind its clone.
func TestDocsReadsAFolderPluginInPlace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	plugins := filepath.Join(home, ".claude", "plugins")
	clone := filepath.Join(home, "src", "wfgate")
	cache := filepath.Join(plugins, "cache", "wfgate-tools", "wfgate", "1.0.0")
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	market := func(source string) {
		t.Helper()
		write(filepath.Join(clone, ".claude-plugin", "marketplace.json"),
			`{"name":"wfgate-tools","plugins":[{"name":"wfgate","source":`+source+`}]}`)
	}
	write(filepath.Join(plugins, "installed_plugins.json"),
		`{"plugins":{"wfgate@wfgate-tools":[{"scope":"user","installPath":"`+cache+`","version":"1.0.0"}]}}`)
	write(filepath.Join(plugins, "known_marketplaces.json"),
		`{"wfgate-tools":{"source":{"source":"directory","path":"`+clone+`"},"installLocation":"`+clone+`"}}`)
	cloneRefs := filepath.Join(clone, "skills", "workflow-gate", "references")
	cacheRefs := filepath.Join(cache, "skills", "workflow-gate", "references")
	market(`"."`)

	// The clone drifted, the cache is in step: the note names the CLONE and the
	// patch that fixes it there. A reinstall would copy a cache nothing reads.
	writeManual(t, cloneRefs, "authoring.md")
	writeManual(t, cacheRefs, "")
	out, err := runWF(t, home, "docs")
	noErr(t, err, "docs over a drifted clone")
	mustContain(t, out, "LOADS differs from the one this binary carries — "+cloneRefs,
		"drifted: authoring.md", "read in place from "+clone, "`make install`")
	if strings.Contains(out, "install.sh") {
		t.Fatalf("a reinstall changes nothing a session reads in place:\n%s", out)
	}

	// The clone in step, the cache stale: no note. The cache is read by nothing.
	writeManual(t, cloneRefs, "")
	writeManual(t, cacheRefs, "authoring.md")
	out, err = runWF(t, home, "docs")
	noErr(t, err, "docs over a stale cache")
	if strings.Contains(out, "differs from the one this binary carries") {
		t.Fatalf("a stale cache under an in-place plugin is not what a session loads:\n%s", out)
	}

	// A string that does not start with "./" — even an absolute path naming the
	// folder itself — or that leaves the folder is stubbed by the harness as
	// unparseable, and an object source is fetched: either way the session loads
	// the cache (measured with an isolated CLAUDE_CONFIG_DIR, 2.1.289).
	for _, source := range []string{`"../elsewhere"`, `"./../elsewhere"`, `"skills/.."`, `"` + clone + `"`, `{"source":"github","repo":"x/y"}`} {
		market(source)
		out, err = runWF(t, home, "docs")
		noErr(t, err, "docs with source "+source)
		mustContain(t, out, "— "+cacheRefs, "drifted: authoring.md", "install.sh")
	}

	// The entry is matched by NAME: a sibling plugin read in place says nothing
	// about this one.
	write(filepath.Join(clone, ".claude-plugin", "marketplace.json"),
		`{"name":"wfgate-tools","plugins":[{"name":"other","source":"."},{"name":"wfgate","source":{"source":"github","repo":"x/y"}}]}`)
	out, err = runWF(t, home, "docs")
	noErr(t, err, "docs beside a sibling plugin")
	mustContain(t, out, "— "+cacheRefs, "install.sh")

	// A marketplace that is not a folder has a local clone too, and its plugin
	// is still copied into the cache: in place is the FOLDER source only.
	market(`"."`)
	write(filepath.Join(plugins, "known_marketplaces.json"),
		`{"wfgate-tools":{"source":{"source":"github","repo":"x/y"},"installLocation":"`+clone+`"}}`)
	out, err = runWF(t, home, "docs")
	noErr(t, err, "docs from a github marketplace")
	mustContain(t, out, "— "+cacheRefs, "install.sh")

	// A `file` marketplace is a folder too: it names its manifest, and its
	// entries resolve against installLocation, the folder (measured: `plugin
	// list` printed "Read from: <folder>/plug" for source "./plug").
	elsewhere := filepath.Join(home, "manifests", "mk.json")
	write(elsewhere, `{"name":"wfgate-tools","plugins":[{"name":"wfgate","source":"./"}]}`)
	market(`{"source":"github","repo":"x/y"}`)
	write(filepath.Join(plugins, "known_marketplaces.json"),
		`{"wfgate-tools":{"source":{"source":"file","path":"`+elsewhere+`"},"installLocation":"`+clone+`"}}`)
	writeManual(t, cloneRefs, "authoring.md")
	writeManual(t, cacheRefs, "")
	out, err = runWF(t, home, "docs")
	noErr(t, err, "docs from a file marketplace")
	mustContain(t, out, "— "+cloneRefs, "read in place from "+clone)
}

// The refusal points at a CHARACTER. Two gates have paid for an unescaped pipe,
// and on the second it was not even authored: a shell hook rewrote a command
// quoted inside the heredoc that was writing the table and injected an
// alternation pipe into a row's text. The message named the row and the cell
// count, so the offending cell was found with `awk` and `tr` by hand.
func TestRecordRefusalNamesTheOffendingPipe(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 1 | BLOCKER | the flag takes --max-columns|--max-columns-preview | 2344d3d8 |\n")

	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	if err == nil {
		t.Fatal("record must refuse a row that splits into five cells")
	}
	// The COLUMN of the fourth unescaped pipe — where a well-formed row's
	// closed-by cell begins — and the tail it could not place. Which extra pipe
	// is the author's is not knowable from the row, so the tail is printed
	// whole and the injected one is inside it.
	mustContain(t, err.Error(), "the 4th pipe is at column", "--max-columns-preview | 2344d3d8", "nothing was recorded")
}

// A row with too FEW cells has no offending pipe, and a column invented for one
// would point at a character that is not the problem.
func TestRecordRefusalOfAShortRowNamesNoColumn(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 1 | BLOCKER | still open |\n")

	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	if err == nil {
		t.Fatal("record must refuse a three-cell row")
	}
	mustContain(t, err.Error(), "not 4")
	if strings.Contains(err.Error(), "4th pipe") {
		t.Fatalf("a short row must name no pipe: %v", err)
	}
}
