package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wfgate "git.sumka.site/yakov/wfgate"
	"git.sumka.site/yakov/wfgate/internal/wfdry"
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

func TestNewScriptHunt(t *testing.T) {
	setGitEnv(t)
	tmp := t.TempDir()
	target := mkTarget(t, tmp)
	h := filepath.Join(tmp, "h")
	_, err := runWF(t, tmp, "init", "h", "-target", target, "-dir", h, "-type", "hunt")
	noErr(t, err, "init -type hunt")
	out, err := runWF(t, h, "new-script", "search")
	noErr(t, err, "new-script")
	mustContain(t, out, "type hunt — from", "wfdry --type hunt")
	b, _ := os.ReadFile(filepath.Join(h, "artifacts", "search.mjs"))
	if !strings.Contains(string(b), "REPRO_SCHEMA") {
		t.Fatalf("a hunt workspace must get the hunt skeleton")
	}
	if strings.Contains(string(b), "'GIT_PATH'") || !strings.Contains(string(b), "git'") {
		t.Fatalf("the git path must be substituted at emit time")
	}
	mustContain(t, out, "wfcheck -type hunt --owns-diff", `--args '{"maxAgents":12}'`)
}

// The `wf docs` index names every reference `wf docs <name>` serves: `types`
// shipped servable and unlisted, so nobody reading the index could find it.
func TestDocsIndexListsEveryName(t *testing.T) {
	out, _ := runWF(t, t.TempDir(), "docs")
	for name := range docNames {
		if !strings.Contains(out, "\n  "+name+" ") {
			t.Errorf("wf docs index omits %q", name)
		}
	}
}

// The audit skeleton's citation form is the one `wf check cite` reads: a
// verified-claims line from its report, saved into the workspace, resolves and
// its anchor is found — and an anchor NOT on the cited line is named.
func TestAuditCitationsAreCheckable(t *testing.T) {
	setGitEnv(t)
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	write(t, filepath.Join(target, "pkg", "store.go"), "package pkg\n\nfunc Save() error {\n\treturn nil\n}\n")
	gitIn(t, target, "init", "-q", "-b", "main")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-q", "-m", "initial")
	a := filepath.Join(tmp, "a")
	_, err := runWF(t, tmp, "init", "a", "-target", target, "-dir", a, "-type", "audit")
	noErr(t, err, "init -type audit")
	out, err := runWF(t, a, "new-script", "ask")
	noErr(t, err, "new-script")
	mustContain(t, out, "type audit — from", "wf check cite -at")
	b, _ := os.ReadFile(filepath.Join(a, "artifacts", "ask.mjs"))
	if !strings.Contains(string(b), "path.ext:LINE#anchor") {
		t.Fatalf("an audit workspace must get the audit skeleton")
	}
	// The exact line shape the skeleton's report prints for a verified claim.
	write(t, filepath.Join(tmp, "answer.md"), "# Audit\n\n## Verified claims\n\n- C001 [holds] Save returns no error — pkg/store.go:3-4#Save\n- C002 [violated] wrong anchor — pkg/store.go:4#Save\n")
	_, err = runWF(t, a, "add", filepath.Join(tmp, "answer.md"))
	noErr(t, err, "add")
	out, _ = runWF(t, a, "check", "cite", "-require-anchor")
	mustContain(t, out, "store.go:4")
	if strings.Contains(out, "store.go:3-4") {
		t.Fatalf("a correct citation from the report's line shape must pass `check cite`:\n%s", out)
	}
}

// The real round trip: the `citable` the emitted audit skeleton returns in a
// dry run's `clean` mode, saved as the answer, passes `wf check cite
// -require-anchor` against a target whose cited lines carry the anchors — five
// citations: the answer's one sentence and the four verified claims.
func TestAuditCitableFromTheSkeletonPassesCheckCite(t *testing.T) {
	setGitEnv(t)
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	write(t, filepath.Join(target, "pkg", "store.go"), "func Save() {}\n")
	write(t, filepath.Join(target, "pkg", "load.go"), "package pkg\nfunc Load() {}\n")
	gitIn(t, target, "init", "-q", "-b", "main")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-q", "-m", "initial")
	a := filepath.Join(tmp, "a")
	_, err := runWF(t, tmp, "init", "a", "-target", target, "-dir", a, "-type", "audit")
	noErr(t, err, "init")
	skel, _ := wfgate.Templates.ReadFile("templates/audit.mjs")
	sp := filepath.Join(tmp, "s.mjs")
	write(t, sp, string(skel))
	rs, err := wfdry.Run(sp, []string{"clean"}, wfdry.Options{Type: ws.TypeAudit})
	if err != nil || len(rs) != 1 {
		t.Fatalf("dry run: %v", err)
	}
	citable, _ := rs[0].Return.(map[string]any)["citable"].(string)
	if !strings.Contains(citable, "#Save") {
		t.Fatalf("no verified claim in citable:\n%s", citable)
	}
	write(t, filepath.Join(tmp, "answer.md"), citable)
	_, err = runWF(t, a, "add", filepath.Join(tmp, "answer.md"))
	noErr(t, err, "add")
	out, err := runWF(t, a, "check", "cite", "-require-anchor")
	if err != nil || !strings.Contains(out, " 0 finding(s)") || !strings.Contains(out, "5 citation(s), 5 anchored") {
		t.Fatalf("the skeleton's own citable must pass check cite:\n%s\n%v", out, err)
	}
}
