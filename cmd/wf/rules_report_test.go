package main

import (
	"path/filepath"
	"testing"
)

// A marked corpus whose ids each sit at ONE site compares nothing, and the bare
// conflict count then reads as a pass. Measured on a live gate:
// `18 marker(s), 18 id(s), 0 conflict(s)` was printed as a clean line after the
// artifact had been reworded until no id had a second site left.
func TestRulesNamesACorpusItComparedNothingIn(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	art := filepath.Join(tmp, "rules.md")
	write(t, art, "# Rules\n\n"+
		"- [R1] The layer is composed at startup.\n"+
		"- [R2] Errors are wrapped, never swallowed.\n"+
		"- [R3] The workspace is never inside the target.\n")
	if _, err := runWF(t, wsDir, "add", art); err != nil {
		t.Fatal(err)
	}

	out, err := runWF(t, wsDir, "check", "rules")
	noErr(t, err, "check rules")
	mustContain(t, out,
		"## rules — 3 marker(s), 3 id(s), 0 compared, 0 conflict(s)",
		"every id has ONE site, so nothing was compared")
}

// The conflict report names its own most likely cause, because the wrong repair
// is cheap and permanent: a live gate reported four ids in conflict, every one
// of them a sentence POINTING at the rule beside the sentence stating it, and
// the operator reworded the artifact — 14 edits — until each id had one site
// left and the check reached nothing at all.
func TestRulesNamesTheReferenceCause(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	art := filepath.Join(tmp, "rules.md")
	write(t, art, "# Rules\n\n"+
		"- [R1] The layer is composed at startup, never per request.\n"+
		"- Because of [R1] the cache cannot be built lazily.\n")
	if _, err := runWF(t, wsDir, "add", art); err != nil {
		t.Fatal(err)
	}

	out, err := runWF(t, wsDir, "check", "rules")
	wantFindings(t, err, "check rules")
	mustContain(t, out,
		"## rules — 2 marker(s), 1 id(s), 1 compared, 1 conflict(s)",
		"R1: sites disagree",
		"a site that REFERS to a rule states nothing",
		"do not reword the artifact")
}
