package main

import (
	"os"
	"path/filepath"
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
	// The four words come from ws.Levels and nowhere else, so the scale cannot
	// drift from the ledger's.
	if !strings.Contains(body, strings.Join(ws.Levels, ", ")) {
		t.Errorf("the emitted brief does not name the closed scale as %q", strings.Join(ws.Levels, ", "))
	}
	// And it must not DEFINE them. A definition here is a second copy of
	// section 0, which is the thing being fixed.
	for _, def := range []string{"NOTE — not a defect", "MINOR — a real defect"} {
		if strings.Contains(body, def) {
			t.Errorf("the template restates the scale (%q); it must point at section 0, not copy it", def)
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

// A FILE in the round directory is what clears the freeze and close refusals
// that demand a round was read by somebody. A scaffold written there clears the
// refusal before the lens has run.
func TestNewLensRefusesToWriteIntoTheRoundDirectory(t *testing.T) {
	wsDir := newGate(t)
	dst := filepath.Join(wsDir, "rounds", "r1", "lens-sneak.md")

	out, err := runWF(t, wsDir, "new-lens", "sneak", "-o", dst)
	if err == nil {
		t.Fatalf("writing into the round directory was accepted:\n%s", out)
	}
	if msg := out + err.Error(); !strings.Contains(msg, "clears the freeze and close refusals") {
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
	body = strings.ReplaceAll(body, "`wf round lens` refuses to bank a prompt that still carries this line. >>>", "")
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
	if !strings.Contains(string(b), lensAimMarker) {
		t.Fatalf("the template lost the aim marker %q, so `wf round lens` can no longer refuse an unfilled scaffold", lensAimMarker)
	}
}
