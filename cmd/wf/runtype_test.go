package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wfgate "git.sumka.site/yakov/wfgate"
	"git.sumka.site/yakov/wfgate/internal/ws"
)

func mkTarget(t *testing.T, tmp string) string {
	t.Helper()
	target := filepath.Join(tmp, "target")
	write(t, filepath.Join(target, "main.go"), "package main\n")
	gitIn(t, target, "init", "-q", "-b", "main")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-q", "-m", "initial")
	return target
}

// The type is banked once, at init, and every later command reads it from
// gate.json — so the field must land, and a build manifest must read exactly
// as it did before the field existed.
func TestInitBanksTheRunType(t *testing.T) {
	setGitEnv(t)
	tmp := t.TempDir()
	target := mkTarget(t, tmp)

	rev := filepath.Join(tmp, "rev")
	out, err := runWF(t, tmp, "init", "r", "-target", target, "-dir", rev, "-type", "review")
	noErr(t, err, "init -type review")
	mustContain(t, out, "type   review")
	var m map[string]any
	b, _ := os.ReadFile(filepath.Join(rev, ws.ManifestName))
	if err := json.Unmarshal(b, &m); err != nil || m["type"] != "review" {
		t.Fatalf("gate.json must carry type review: %s", b)
	}
	out, err = runWF(t, rev, "status")
	noErr(t, err, "status")
	mustContain(t, out, "type   review")

	bld := filepath.Join(tmp, "bld")
	_, err = runWF(t, tmp, "init", "b", "-target", target, "-dir", bld)
	noErr(t, err, "init")
	b, _ = os.ReadFile(filepath.Join(bld, ws.ManifestName))
	if strings.Contains(string(b), `"type"`) {
		t.Fatalf("a build manifest is written without the field, as every manifest before it was: %s", b)
	}
}

// A misspelt type is refused BEFORE anything is written: defaulting it to
// build would gate a review with the build rules, and a half-made directory
// would trip the retry.
func TestInitRefusesAnUnknownType(t *testing.T) {
	setGitEnv(t)
	tmp := t.TempDir()
	target := mkTarget(t, tmp)
	dir := filepath.Join(tmp, "x")
	_, err := runWF(t, tmp, "init", "x", "-target", target, "-dir", dir, "-type", "reveiw")
	if err == nil {
		t.Fatalf("an unknown type must be refused")
	}
	mustContain(t, err.Error(), "build, review")
	if _, statErr := os.Stat(dir); statErr == nil {
		t.Fatalf("a refused init left %s behind", dir)
	}
}

// new-script emits the skeleton of the workspace's type and SAYS where the
// type came from; -type overrides it.
func TestNewScriptFollowsTheType(t *testing.T) {
	setGitEnv(t)
	tmp := t.TempDir()
	target := mkTarget(t, tmp)
	rev := filepath.Join(tmp, "rev")
	_, err := runWF(t, tmp, "init", "r", "-target", target, "-dir", rev, "-type", "review")
	noErr(t, err, "init")

	out, err := runWF(t, rev, "new-script", "look")
	noErr(t, err, "new-script")
	mustContain(t, out, "type review — from", "-type overrides", "wfdry --type review", "--owns-diff")
	b, _ := os.ReadFile(filepath.Join(rev, "artifacts", "look.mjs"))
	if !strings.Contains(string(b), "phase('Find')") || strings.Contains(string(b), "PRODUCER_SCHEMA") {
		t.Fatalf("a review workspace must get the review skeleton")
	}

	_, err = runWF(t, rev, "new-script", "make", "-type", "build")
	noErr(t, err, "new-script -type build")
	b, _ = os.ReadFile(filepath.Join(rev, "artifacts", "make.mjs"))
	if !strings.Contains(string(b), "PRODUCER_SCHEMA") {
		t.Fatalf("-type build must override the workspace's type")
	}
}

// Every type in the closed list has a skeleton, and every skeleton exists.
func TestEveryTypeHasATemplate(t *testing.T) {
	for _, typ := range ws.Types {
		p, ok := templateFor[typ]
		if !ok {
			t.Errorf("type %s has no skeleton in templateFor", typ)
			continue
		}
		if _, err := wfgate.Templates.ReadFile(p); err != nil {
			t.Errorf("type %s: %v", typ, err)
		}
	}
}

// The type is read where the skeleton LANDS: `-o` into a review workspace
// from outside any emitted the build skeleton in silence.
func TestNewScriptReadsTheDestinationsType(t *testing.T) {
	setGitEnv(t)
	tmp := t.TempDir()
	target := mkTarget(t, tmp)
	rev := filepath.Join(tmp, "rev")
	_, err := runWF(t, tmp, "init", "r", "-target", target, "-dir", rev, "-type", "review")
	noErr(t, err, "init")
	dst := filepath.Join(rev, "artifacts", "far.mjs")
	out, err := runWF(t, tmp, "new-script", "far", "-target", target, "-o", dst)
	noErr(t, err, "new-script -o")
	mustContain(t, out, "type review — from gate.json at")
	b, _ := os.ReadFile(dst)
	if !strings.Contains(string(b), "phase('Find')") {
		t.Fatalf("-o into a review workspace must emit the review skeleton")
	}
}
