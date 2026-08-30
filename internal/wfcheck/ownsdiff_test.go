package wfcheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mkRunRepo builds a target that looks like a tree a run has just finished in:
// a base commit, then edits and NEW files left UNCOMMITTED, which is the state
// every post-run audit on record has actually been run against.
func mkRunRepo(t *testing.T) (dir, base string) {
	t.Helper()
	dir = t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(p, body string) {
		t.Helper()
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	write("internal/router/router.go", "package router\n")
	write("internal/store/store.go", "package store\n")
	write("internal/resources/populate.go", "package resources\n")
	write("README.md", "readme\n")
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	base = git("rev-parse", "HEAD")

	// What the run did: edited two owned files, edited one nobody owns, and
	// CREATED a file nobody owns. Nothing is committed — the run leaves the
	// tree dirty and the audit runs there.
	write("internal/router/router.go", "package router\n// changed\n")
	write("internal/store/store.go", "package store\n// changed\n")
	write("internal/resources/populate.go", "package resources\n// changed\n")
	write("internal/domain/main_test.go", "package domain\n")
	return dir, base
}

// script writes a gate-shaped script declaring `owns`, the half every real
// script has carried.
func mkOwnsScript(t *testing.T, owns ...string) string {
	t.Helper()
	quoted := make([]string, 0, len(owns))
	for _, o := range owns {
		quoted = append(quoted, "'"+o+"'")
	}
	src := `export const meta = { name: 'x', description: 'x', phases: [{ title: 'A' }] }
async function spawn(p, o) { try { return await agent(p, o) } catch (e) { return null } }
phase('A')
const build = await spawn('do the thing', { label: 'build', phase: 'A', model: 'sonnet', effort: 'medium' })
await runGate({
  slug: 'gate1',
  phase: 'A',
  goal: 'g',
  owns: [` + strings.Join(quoted, ", ") + `],
  reviewers: [
    { slug: 'a', vector: 'one', model: 'sonnet' },
    { slug: 'b', vector: 'two', model: 'sonnet' },
  ],
})
`
	p := filepath.Join(t.TempDir(), "s.mjs")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func hasPath(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// The two directions at once, over the state a real audit sees: an uncommitted
// tree. Without the working-tree read this whole report is empty and green,
// which is the failure the comment in ownsdiff.go names.
func TestOwnsDiffBothDirections(t *testing.T) {
	dir, base := mkRunRepo(t)
	script := mkOwnsScript(t, "internal/router/router.go", "internal/store/store.go", "internal/never/touched.go")

	rep, err := OwnsDiff(script, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Changed != 4 {
		t.Fatalf("changed = %d, want 4 (two owned edits, one unowned edit, one created file): %v", rep.Changed, rep.Unowned)
	}
	if !hasPath(rep.Unowned, "internal/resources/populate.go") {
		t.Errorf("an edited file no section owns is not reported: %v", rep.Unowned)
	}
	// The created file is the half `git diff` cannot see at all.
	if !hasPath(rep.Unowned, "internal/domain/main_test.go") {
		t.Errorf("a CREATED file no section owns is not reported: %v", rep.Unowned)
	}
	if len(rep.Unowned) != 2 {
		t.Errorf("unowned = %v, want exactly the two above", rep.Unowned)
	}
	if !hasPath(rep.Untouched, "internal/never/touched.go") {
		t.Errorf("an owned path the run never wrote is not reported: %v", rep.Untouched)
	}
	if len(rep.Untouched) != 1 {
		t.Errorf("untouched = %v, want exactly the one above", rep.Untouched)
	}
	if rep.Gates != 1 || rep.OwnedFiles != 3 {
		t.Errorf("population misread: %d gate(s), %d owned file(s)", rep.Gates, rep.OwnedFiles)
	}
}

// A run that committed its work is the OTHER shape, and the same range must
// cover it — base..worktree includes what landed in commits.
func TestOwnsDiffSeesCommittedWork(t *testing.T) {
	dir, base := mkRunRepo(t)
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	cmd = exec.Command("git", "commit", "-q", "-m", "the run's work")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}
	script := mkOwnsScript(t, "internal/router/router.go", "internal/store/store.go")

	rep, err := OwnsDiff(script, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Changed != 4 {
		t.Fatalf("changed = %d, want 4 after the work was committed", rep.Changed)
	}
	if !hasPath(rep.Unowned, "internal/domain/main_test.go") {
		t.Errorf("a committed new file no section owns is not reported: %v", rep.Unowned)
	}
}

// A directory entry owns everything under it, and is only UNTOUCHED when the
// run wrote nothing there. Both halves, because a prefix match that reported an
// owned directory as untouched would print a row for every gate that did its
// job.
func TestOwnsDiffDirectoryEntries(t *testing.T) {
	dir, base := mkRunRepo(t)
	script := mkOwnsScript(t, "internal/resources/", "internal/nothinghere/")

	rep, err := OwnsDiff(script, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OwnedDirs != 2 {
		t.Fatalf("owned dirs = %d, want 2", rep.OwnedDirs)
	}
	if hasPath(rep.Unowned, "internal/resources/populate.go") {
		t.Errorf("a file under a declared directory is reported as unowned: %v", rep.Unowned)
	}
	if !hasPath(rep.Untouched, "internal/nothinghere/") {
		t.Errorf("a declared directory the run never wrote into is not reported: %v", rep.Untouched)
	}
	if hasPath(rep.Untouched, "internal/resources/") {
		t.Errorf("a declared directory the run DID write into is reported untouched: %v", rep.Untouched)
	}
}

// The empty population, which this project treats as its most repeated defect:
// a script whose map this tool cannot read must not produce a clean report.
// The caller decides what to print; the report has to make the emptiness
// visible in the numbers.
func TestOwnsDiffEmptyMapIsVisible(t *testing.T) {
	dir, base := mkRunRepo(t)
	p := filepath.Join(t.TempDir(), "s.mjs")
	src := `export const meta = { name: 'x', description: 'x', phases: [{ title: 'A' }] }
phase('A')
await agent('do', { label: 'build', phase: 'A', model: 'sonnet', effort: 'medium' })
`
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := OwnsDiff(p, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OwnedFiles+rep.OwnedDirs != 0 {
		t.Fatalf("a script with no ownership read %d path(s)", rep.OwnedFiles+rep.OwnedDirs)
	}
	// Every changed file is "unowned" here, which is exactly why the count
	// alone is not a finding and the caller must say NOT A COMPARISON.
	if len(rep.Unowned) != rep.Changed {
		t.Errorf("with an empty map, unowned (%d) must be every changed file (%d)", len(rep.Unowned), rep.Changed)
	}
}

// A base that does not resolve is an error, not an empty report: a typo'd ref
// printing "0 changed" is the false green this check exists to remove.
func TestOwnsDiffBadBaseIsAnError(t *testing.T) {
	dir, _ := mkRunRepo(t)
	script := mkOwnsScript(t, "internal/router/router.go")
	_, err := OwnsDiff(script, dir, "no-such-ref")
	if err == nil {
		t.Fatal("a base ref that does not exist was accepted")
	}
	// git's own words for this are "ambiguous argument", which name neither the
	// base nor what one is. The message has to say what to pass instead.
	if !strings.Contains(err.Error(), "no-such-ref") || !strings.Contains(err.Error(), "STARTED from") {
		t.Errorf("the refusal does not name the bad ref and the patch: %v", err)
	}
}
