package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	wfgate "git.sumka.site/yakov/wfgate"
	"git.sumka.site/yakov/wfgate/internal/ws"
)

// The scaffold's whole argument is that it POINTS at the pinned rules instead
// of restating them: 26 of the 309 banked lens prompts carry the severity
// scale's NOTE line in a wording the manual has replaced, and no check reaches
// a prompt. A template that grew its own copy of the scale would be that
// defect, shipped from the binary.
func TestNewLensPointsAtThePinnedRulesAndDoesNotRestateTheScale(t *testing.T) {
	wsDir := newGate(t)
	dst := filepath.Join(wsDir, "..", "lens-facts.md")

	out, err := runWF(t, wsDir, "new-lens", "facts", "-o", dst)
	noErr(t, err, "new-lens")
	mustContain(t, out, "wrote ", "round 1", "kind full", "refuses to bank a prompt still carrying it")

	body := read(t, dst)
	if strings.Contains(body, "{{") {
		t.Fatalf("a placeholder survived substitution:\n%s", body)
	}
	// The pinned copy, by PATH, and the three sections aimed.
	for _, want := range []string{
		filepath.Join(wsDir, "gate-checks.md"),
		"sections 0, 0b and 0c",
		filepath.Join(wsDir, "artifacts", "decisions.md"),
		lensAimMarker,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the emitted brief does not carry %q:\n%s", want, body)
		}
	}
	// Written out LITERALLY, not as strings.Join(ws.Levels, ", ") — that is the
	// expression the production line uses, so the assertion moved with it and
	// freezing the substitution to a hard-coded string survived. ws.Levels is
	// the source; this is the independent reading of it, the way the wave-22
	// review made the two enumeration tables literal beside their map.
	const scale = "BLOCKER, MAJOR, MINOR, NOTE"
	if got := strings.Join(ws.Levels, ", "); got != scale {
		t.Fatalf("ws.Levels is %q and this test says %q — one of the two moved, and the emitted brief follows ws.Levels", got, scale)
	}
	if !strings.Contains(body, scale) {
		t.Errorf("the emitted brief does not name the closed scale as %q", scale)
	}
	// And it must restate NOTHING from those sections. The first version
	// forbade only the two spellings "NOTE — not a defect" and "MINOR — a real
	// defect", and shipped section 0's decision rule and section 0c's
	// conclusion verbatim in substance right past it — an unchecked copy of
	// the manual emitted by the command built to stop unchecked copies, in a
	// file `make check-docs` does not stage. These are the sentences, not
	// their labels.
	for _, def := range []string{
		"NOTE — not a defect",
		"MINOR — a real defect",
		"decided by what catches",
		"NAMES the later gate",
		"a BLOCKER, not a NOTE",
		"how bad it reads",
	} {
		if strings.Contains(body, def) {
			t.Errorf("the template restates the manual (%q); it must POINT at sections 0 and 0c, not copy them", def)
		}
	}
}

// A full round has no predecessor, so it gets neither the three asks nor a
// brief path. Printing them there is the absent-measurement-in-the-register-of-
// a-measured-one class one layer up: a lens sent after a file the round is not
// supposed to have.
func TestNewLensGivesTheThreeAsksToADeltaRoundOnly(t *testing.T) {
	wsDir := newGate(t)

	full := filepath.Join(wsDir, "..", "lens-full.md")
	_, err := runWF(t, wsDir, "new-lens", "full", "-o", full)
	noErr(t, err, "new-lens full")
	if b := read(t, full); strings.Contains(b, "FIX-INDUCED") || strings.Contains(b, "brief.md") {
		t.Errorf("round 1 is a FULL round and was given a delta's asks or brief:\n%s", b)
	}

	delta := filepath.Join(wsDir, "..", "lens-delta.md")
	_, err = runWF(t, wsDir, "new-lens", "d", "-kind", "delta", "-o", delta)
	noErr(t, err, "new-lens delta")
	b := read(t, delta)
	for _, want := range []string{"CLOSURE", "FIX-INDUCED", "RIPPLE", "DOES NOT EXIST YET", "wf delta -save"} {
		if !strings.Contains(b, want) {
			t.Errorf("a delta brief must carry %q:\n%s", want, b)
		}
	}
}

// A FILE in the round directory is what the shape the roster scans
// that demand a round was read by somebody. A scaffold written there clears the
// refusal before the lens has run.
func TestNewLensRefusesToWriteIntoTheRoundDirectory(t *testing.T) {
	wsDir := newGate(t)
	dst := filepath.Join(wsDir, "rounds", "r1", "lens-sneak.md")

	out, err := runWF(t, wsDir, "new-lens", "sneak", "-o", dst)
	if err == nil {
		t.Fatalf("writing into the round directory was accepted:\n%s", out)
	}
	if msg := out + err.Error(); !strings.Contains(msg, "the shape the roster scans") {
		t.Errorf("the refusal does not say why: %s", msg)
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("the refusal left the file behind — a refusal that dirties the tree has committed what it refused")
	}
}

// Banking the scaffold unfilled records that a lens ran and not what it was
// asked -- the claim -name, -model and -asks are each already refused for. The
// prompt was the fourth field and had no such refusal.
func TestRoundLensRefusesTheUnfilledScaffold(t *testing.T) {
	wsDir := newGate(t)
	dst := filepath.Join(wsDir, "..", "lens-unfilled.md")
	_, err := runWF(t, wsDir, "new-lens", "unfilled", "-o", dst)
	noErr(t, err, "new-lens")

	out, err := runWF(t, wsDir, "round", "lens", dst, "-name", "unfilled", "-model", "sonnet", "-asks", "facts")
	if err == nil {
		t.Fatalf("the unfilled scaffold was banked:\n%s", out)
	}
	if msg := out + err.Error(); !strings.Contains(msg, "marker line") || !strings.Contains(msg, lensAimMarker) {
		t.Errorf("the refusal does not name the marker: %s", msg)
	}

	// Filled, it banks. Without this the test above passes over a command that
	// refuses every prompt.
	body := strings.ReplaceAll(read(t, dst), lensAimMarker, "Reproduce every NUMBER in the acceptance section")
	if err := os.WriteFile(dst, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = runWF(t, wsDir, "round", "lens", dst, "-name", "unfilled", "-model", "sonnet", "-asks", "facts")
	noErr(t, err, "round lens after filling: "+out)
}

// The template is embedded for the same reason the manual is: a copy on disk
// beside the binary drifts from the tool that emits it.
func TestLensTemplateIsEmbedded(t *testing.T) {
	b, err := wfgate.Templates.ReadFile("templates/lens.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, ph := range []string{"{{KIND_PHRASE}}", "{{READING}}", "{{ASKS}}", "{{LEVELS}}"} {
		if !strings.Contains(string(b), ph) {
			t.Errorf("the template lost the placeholder %s, so cmdNewLens substitutes nothing into it", ph)
		}
	}
	// The marker LITERALLY, beside the constant. Asserting the template
	// contains lensAimMarker moves both sides together: narrowing the constant
	// to "<<<" passed.
	const marker = "<<< WRITE YOUR VECTOR HERE >>>"
	if lensAimMarker != marker {
		t.Fatalf("lensAimMarker is %q and this test says %q — the refusal and the template must name the same line", lensAimMarker, marker)
	}
	if !strings.Contains(string(b), marker) {
		t.Fatalf("the template lost the aim marker %q, so `wf round lens` can no longer refuse an unfilled scaffold", marker)
	}
}

// The refusal's own subject, by the spelling an operator actually types. The
// first version compared filepath.Dir(dst) against an ABSOLUTE RoundDir, so it
// fired for `-o "$PWD/rounds/r2/x.md"` and not for `-o rounds/r2/x.md` — and
// the test that pinned it built an absolute path, which is a guard armed on the
// one input the defect does not take.
func TestNewLensRefusesEverySpellingOfTheRoundDirectory(t *testing.T) {
	wsDir := newGate(t)
	_, err := runWF(t, wsDir, "round", "new", "-title", "r2", "-kind", "delta")
	noErr(t, err, "round new r2")

	for _, dst := range []string{
		"rounds/lens-loose.md",
		"rounds/r2/lens-rel.md",                             // the natural spelling, relative to the workspace root
		"rounds/r1/lens-other.md",                           // a round OTHER than the one being briefed
		"rounds/r2/../r2/lens-dots.md",                      // the same directory reached through ..
		filepath.Join(wsDir, "rounds", "r2", "lens-abs.md"), // absolute, which is all the first test covered
	} {
		out, err := runWF(t, wsDir, "new-lens", "x", "-o", dst)
		if err == nil {
			t.Errorf("-o %q was accepted into a round directory:\n%s", dst, out)
			continue
		}
		if msg := out + err.Error(); !strings.Contains(msg, "the shape the roster scans") {
			t.Errorf("-o %q: the refusal does not say why: %s", dst, msg)
		}
	}
}

// `target` is read-only: no command writes into it. `-o` was guarded against
// nothing and would create directories on the way.
func TestNewLensRefusesToWriteIntoTheTarget(t *testing.T) {
	wsDir := newGate(t)
	target := filepath.Join(filepath.Dir(wsDir), "target")
	dst := filepath.Join(target, "src", "lens-in-target.md")

	out, err := runWF(t, wsDir, "new-lens", "t", "-o", dst)
	if err == nil {
		t.Fatalf("a path inside the target was accepted:\n%s", out)
	}
	if msg := out + err.Error(); !strings.Contains(msg, "read-only") {
		t.Errorf("the refusal does not name the target: %s", msg)
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("the refusal wrote into the target anyway")
	}
	// And the DEFAULT — beside the operator, outside the workspace — still
	// works: the bound is the target, not the workspace, or the command would
	// refuse its own default.
	beside := filepath.Join(filepath.Dir(wsDir), "lens-beside.md")
	_, err = runWF(t, wsDir, "new-lens", "b", "-o", beside)
	noErr(t, err, "a brief beside the operator")
}

// A given -round must never be reported as absent, and a round the gate has not
// opened is refused the way `wf round lens` refuses it.
func TestNewLensRoundArgument(t *testing.T) {
	wsDir := newGate(t)
	base := filepath.Dir(wsDir)

	out, err := runWF(t, wsDir, "new-lens", "n", "-round", "-3", "-o", filepath.Join(base, "a.md"))
	if err == nil {
		t.Fatalf("-round -3 was accepted:\n%s", out)
	}
	if msg := out + err.Error(); !strings.Contains(msg, "rounds start at 1") || strings.Contains(msg, "was not given") {
		t.Errorf("-round -3 must not be reported as absent: %s", msg)
	}

	out, err = runWF(t, wsDir, "new-lens", "f", "-round", "99", "-o", filepath.Join(base, "b.md"))
	if err == nil {
		t.Fatalf("-round 99 was accepted over a gate with one round:\n%s", out)
	}
	if msg := out + err.Error(); !strings.Contains(msg, "opened 1 round(s)") {
		t.Errorf("the refusal does not say how many rounds exist: %s", msg)
	}
}

// The sweep found 22 of 34 mutations surviving, every one an UNTESTED guard.
// These are them: each case kills one mutation the bars did not.
func TestNewLensGuardsThatNothingProved(t *testing.T) {
	wsDir := newGate(t)
	base := filepath.Dir(wsDir)
	out := func(n string) string { return filepath.Join(base, n+".md") }

	t.Run("two positionals are refused", func(t *testing.T) {
		o, err := runWF(t, wsDir, "new-lens", "a", "b", "-o", out("p2"))
		if err == nil {
			t.Fatalf("`new-lens a b` was accepted:\n%s", o)
		}
		if msg := o + err.Error(); !strings.Contains(msg, "usage:") {
			t.Errorf("the refusal does not name the usage: %s", msg)
		}
	})

	t.Run("a name that is a path is refused", func(t *testing.T) {
		for _, n := range []string{"a/b", `a\b`, ".", ".."} {
			o, err := runWF(t, wsDir, "new-lens", n, "-o", out("path"))
			if err == nil {
				t.Errorf("the name %q was accepted:\n%s", n, o)
			}
		}
	})

	t.Run("a name with no ASCII slug is refused", func(t *testing.T) {
		// `wf round lens` names the banked file lens-<slug>.md and refuses an
		// empty slug; accepting it here prints a follow-up nobody can run.
		o, err := runWF(t, wsDir, "new-lens", "факты", "-o", out("slug"))
		if err == nil {
			t.Fatalf("a name reducing to an empty slug was accepted:\n%s", o)
		}
		if msg := o + err.Error(); !strings.Contains(msg, "empty slug") {
			t.Errorf("the refusal does not say why: %s", msg)
		}
	})

	t.Run("-kind and -asks garbage are refused", func(t *testing.T) {
		if o, err := runWF(t, wsDir, "new-lens", "k", "-kind", "sideways", "-o", out("k")); err == nil {
			t.Errorf("-kind sideways was accepted:\n%s", o)
		}
		if o, err := runWF(t, wsDir, "new-lens", "q", "-asks", "vibes", "-o", out("q")); err == nil {
			t.Errorf("-asks vibes was accepted:\n%s", o)
		}
	})

	t.Run("a round 2 emit defaults to delta", func(t *testing.T) {
		// The wave's own "derive the default from the round" decision was
		// proven for round 1 only: a mutation making every round default to
		// `full` passed every bar.
		_, err := runWF(t, wsDir, "round", "new", "-title", "r2", "-kind", "delta")
		noErr(t, err, "round new r2")
		o, err := runWF(t, wsDir, "new-lens", "auto", "-o", out("auto"))
		noErr(t, err, "new-lens on round 2")
		if !strings.Contains(o, "kind delta") {
			t.Fatalf("round 2 must default to delta:\n%s", o)
		}
		b := read(t, out("auto"))
		if !strings.Contains(b, "This is a DELTA round") || !strings.Contains(b, "FIX-INDUCED") {
			t.Errorf("the delta phrase or the three asks are missing:\n%s", b)
		}
	})

	t.Run("never over a file that exists, by either guard", func(t *testing.T) {
		dst := out("twice")
		_, err := runWF(t, wsDir, "new-lens", "t1", "-o", dst)
		noErr(t, err, "first emit")
		o, err := runWF(t, wsDir, "new-lens", "t2", "-o", dst)
		if err == nil {
			t.Fatalf("a second emit landed on an authored prompt:\n%s", o)
		}
		// And through a DANGLING symlink, which the Stat check passes and only
		// O_EXCL refuses: os.WriteFile would truncate through it.
		link := out("dangling")
		if err := os.Symlink(filepath.Join(base, "nowhere.md"), link); err != nil {
			t.Skip(err)
		}
		o, err = runWF(t, wsDir, "new-lens", "t3", "-o", link)
		if err == nil {
			t.Fatalf("an emit went through a dangling symlink:\n%s", o)
		}
		if _, err := os.Stat(filepath.Join(base, "nowhere.md")); err == nil {
			t.Error("the emit wrote through the dangling symlink's target")
		}
	})

	t.Run("a nested -o creates its directory", func(t *testing.T) {
		dst := filepath.Join(base, "sub", "dir", "lens-n.md")
		_, err := runWF(t, wsDir, "new-lens", "n", "-o", dst)
		noErr(t, err, "nested -o")
		if _, err := os.Stat(dst); err != nil {
			t.Fatalf("MkdirAll did not run: %v", err)
		}
	})

	t.Run("the follow-up line is runnable", func(t *testing.T) {
		// `-asks "closure ripple"` is a spelling parseAsks accepts; echoing it
		// verbatim printed a line whose second word is a positional.
		o, err := runWF(t, wsDir, "new-lens", "e", "-asks", "closure ripple", "-o", out("echo"))
		noErr(t, err, "new-lens with a space-separated -asks")
		// Comma-joined AND quoted: a comma-bearing argument is one word only
		// inside quotes, and the line is printed to be pasted.
		if !strings.Contains(o, `-asks "closure,ripple"`) {
			t.Errorf("the printed follow-up does not comma-join and quote the parsed asks:\n%s", o)
		}
	})

	// The other half of the same rule: with no -asks the clause is DROPPED, never
	// filled with a `<Q,Q>` placeholder. `<` and `>` are shell redirections, so
	// the printed line truncated at `-asks` and left an empty file named `Q,Q` in
	// the operator's working directory — a follow-up the command prints and
	// cannot be run with, which is the defect the quoting above already answers
	// at the other end.
	t.Run("no -asks prints no -asks clause", func(t *testing.T) {
		o, err := runWF(t, wsDir, "new-lens", "noasks", "-o", out("noasks"))
		noErr(t, err, "new-lens with no -asks")
		if strings.Contains(o, "-asks") {
			t.Errorf("an -asks clause was printed for a lens that claims none:\n%s", o)
		}
		if strings.Contains(o, "<Q,Q>") {
			t.Errorf("the placeholder is a shell redirection when pasted:\n%s", o)
		}
	})

	t.Run("a brief inside the workspace says what verify will call it", func(t *testing.T) {
		dst := filepath.Join(wsDir, "lens-inside.md")
		o, err := runWF(t, wsDir, "new-lens", "i", "-o", dst)
		noErr(t, err, "new-lens inside the workspace")
		if !strings.Contains(o, "INSIDE the workspace") || !strings.Contains(o, "not to launch") {
			t.Errorf("a stray file in the workspace is what `wf verify` refuses, and the command did not say so:\n%s", o)
		}
		// And a brief OUTSIDE it must not carry the note.
		o, err = runWF(t, wsDir, "new-lens", "o", "-o", out("outside"))
		noErr(t, err, "new-lens outside")
		if strings.Contains(o, "INSIDE the workspace") {
			t.Errorf("the note fired on a brief outside the workspace:\n%s", o)
		}
	})
}

// A gate with no artifact must not read as "there is nothing to read": the
// absent-population class, in a reading list.
func TestNewLensNamesAnArtifactlessGate(t *testing.T) {
	setGitEnv(t)
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	write(t, filepath.Join(target, "a.go"), "package a\n")
	gitIn(t, target, "init", "-q", "-b", "main")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-q", "-m", "i")
	wsDir := filepath.Join(tmp, "ws")
	_, err := runWF(t, tmp, "init", "empty", "-target", target, "-dir", wsDir)
	noErr(t, err, "init")
	_, err = runWF(t, wsDir, "round", "new", "-title", "r1")
	noErr(t, err, "round new")

	dst := filepath.Join(tmp, "lens-e.md")
	_, err = runWF(t, wsDir, "new-lens", "e", "-o", dst)
	noErr(t, err, "new-lens over an artifactless gate")
	if b := read(t, dst); !strings.Contains(b, "NO ARTIFACT YET") {
		t.Fatalf("a gate with no artifact must say so rather than printing an empty reading list:\n%s", b)
	}
}

// The template and the command drift apart silently otherwise: a placeholder
// the command does not substitute ships inside the brief.
func TestNewLensRefusesAnUnsubstitutedPlaceholder(t *testing.T) {
	b, err := wfgate.Templates.ReadFile("templates/lens.md")
	if err != nil {
		t.Fatal(err)
	}
	// Every {{...}} in the template must be one cmdNewLens replaces. The guard
	// in the command fires at run time; this is the same claim read statically,
	// so a new placeholder fails the bars rather than one operator's emit.
	known := map[string]bool{"{{KIND_PHRASE}}": true, "{{READING}}": true, "{{ASKS}}": true, "{{LEVELS}}": true}
	for _, m := range regexp.MustCompile(`\{\{[^}]*\}\}`).FindAllString(string(b), -1) {
		if !known[m] {
			t.Errorf("templates/lens.md carries %s, which cmdNewLens does not substitute", m)
		}
	}
}

// The external vector's two: a failed write must not leave a brief behind, and
// the marker refusal must not be walked past by an operator who REWORDS the
// marker line instead of replacing it.
func TestNewLensRollsBackAPartialWrite(t *testing.T) {
	wsDir := newGate(t)
	// A directory at dst: O_CREATE|O_EXCL fails outright, so nothing is left.
	// The rollback path itself is reached by a write that fails after the file
	// exists, which is not producible portably — so pin the observable half:
	// no `lens-*.md` survives a refused emit, by any route.
	dst := filepath.Join(filepath.Dir(wsDir), "lens-dir.md")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "new-lens", "d", "-o", dst)
	if err == nil {
		t.Fatalf("an emit onto a directory was accepted:\n%s", out)
	}
	if fi, err := os.Stat(dst); err != nil || !fi.IsDir() {
		t.Fatal("the emit replaced the directory")
	}
}

func TestRoundLensRefusesARewordedMarker(t *testing.T) {
	wsDir := newGate(t)
	dst := filepath.Join(filepath.Dir(wsDir), "lens-reworded.md")
	_, err := runWF(t, wsDir, "new-lens", "rw", "-o", dst)
	noErr(t, err, "new-lens")

	// Not the constant's bytes, and still unfilled. An exact-byte check passes
	// this and banks a brief whose aim is a placeholder.
	body := strings.ReplaceAll(read(t, dst), lensAimMarker, "<<< WRITE YOUR AIM HERE >>>")
	if err := os.WriteFile(dst, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "round", "lens", dst, "-name", "rw", "-model", "sonnet", "-asks", "facts")
	if err == nil {
		t.Fatalf("a reworded marker was banked:\n%s", out)
	}
	if msg := out + err.Error(); !strings.Contains(msg, "<<<") {
		t.Errorf("the refusal does not name the shape it refuses: %s", msg)
	}
}

// The reviewer's table is the LEDGER's table: a separate `where` column keeps
// the path out of the finding text, and `wf check closure` reaches a closure
// only through the path the finding names.
func TestLensTemplateReturnsTheLedgerColumns(t *testing.T) {
	b, err := wfgate.Templates.ReadFile("templates/lens.md")
	if err != nil {
		t.Fatal(err)
	}
	// The literal header the findings scaffold writes, so the two cannot drift
	// apart without this failing.
	const header = "| # | level | finding | closed by |"
	if !strings.Contains(string(b), header) {
		t.Errorf("the brief does not ask for the ledger's columns %q", header)
	}
	if strings.Contains(string(b), "| level | where | finding |") {
		t.Error("the brief asks for a `where` column, which is how the path leaves the finding text")
	}
	if !strings.Contains(string(b), `\|`) {
		t.Error("the brief does not say how to write a literal pipe; the row is split on unescaped ones")
	}
}
