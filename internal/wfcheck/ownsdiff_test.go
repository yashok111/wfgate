package wfcheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
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
	// Uncommitted work leaves HEAD where it was; a read-only run's bar reads
	// this field and must not fail a tree whose only change is in the index.
	if rep.HeadMoved {
		t.Errorf("HEAD did not move, the run left its work uncommitted — HeadMoved must be false")
	}
	// The read-only bar names EVERY changed path, owned or not: listing only
	// the unowned ones named nothing over a run whose writes were all owned.
	if len(rep.Paths) != rep.Changed || !hasPath(rep.Paths, "internal/router/router.go") {
		t.Errorf("paths = %v, want all %d changed, the owned ones included", rep.Paths, rep.Changed)
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
	if !rep.HeadMoved {
		t.Errorf("the run committed, so HEAD is no longer the base — HeadMoved must say so")
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

// The literal half of the map, which no test reached: a mutation deleting the
// whole `ownsFromLiterals` contribution survived the first suite. A script can
// declare ownership in a prompt and in no gate config at all.
func TestOwnsDiffReadsYouOwnBlocks(t *testing.T) {
	dir, base := mkRunRepo(t)
	src := "export const meta = { name: 'x', description: 'x', phases: [{ title: 'A' }] }\n" +
		"phase('A')\n" +
		"await agent('do the thing\\n\\nYOU OWN\\n  internal/router/router.go\\n  internal/store/store.go\\n\\nDO NOT\\n  everything else\\n', " +
		"{ label: 'build', phase: 'A', model: 'sonnet', effort: 'medium' })\n"
	p := filepath.Join(t.TempDir(), "s.mjs")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := OwnsDiff(p, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Blocks == 0 || rep.OwnedFiles != 2 {
		t.Fatalf("the YOU OWN block was not read: %d block(s), %d owned file(s)", rep.Blocks, rep.OwnedFiles)
	}
	if hasPath(rep.Unowned, "internal/router/router.go") {
		t.Errorf("a path owned only by a YOU OWN block reads as unowned: %v", rep.Unowned)
	}
}

// A map read in PART must say so. Without the flag the unreadable half's paths
// print as "no section owns this", which is a FALSE row rather than a missed
// one — the difference the report exists to keep.
func TestOwnsDiffPartialMapIsFlagged(t *testing.T) {
	dir, base := mkRunRepo(t)
	src := `export const meta = { name: 'x', description: 'x', phases: [{ title: 'A' }] }
phase('A')
const extra = whateverTheOperatorComputes()
await runGate({
  slug: 'gate1', phase: 'A', goal: 'g',
  owns: ['internal/router/router.go', ...extra],
  reviewers: [{ slug: 'a', vector: 'one', model: 'sonnet' }],
})
`
	p := filepath.Join(t.TempDir(), "s.mjs")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := OwnsDiff(p, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Partial {
		t.Fatalf("an owns array this tool could not read in full is not reported as partial: %+v", rep.Unowned)
	}
}

// git QUOTES a non-ASCII path in both commands unless `-z` is passed, and the
// quotes and octal escapes travel into the comparison — the path then matches
// no entry, prints as unowned, and names a file that exists nowhere.
func TestOwnsDiffNonASCIIPaths(t *testing.T) {
	dir, base := mkRunRepo(t)
	weird := "internal/router/wéird.go"
	if err := os.WriteFile(filepath.Join(dir, weird), []byte("package router\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := mkOwnsScript(t, "internal/router/")

	rep, err := OwnsDiff(script, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range rep.Unowned {
		if strings.Contains(p, `\`) || strings.Contains(p, `"`) {
			t.Fatalf("a quoted/escaped path reached the comparison: %q", p)
		}
	}
	if hasPath(rep.Unowned, weird) {
		t.Errorf("a created non-ASCII path under a declared directory reads as unowned: %v", rep.Unowned)
	}
}

// The directory prefix must be a PATH prefix. `internal/res/` beside
// `internal/resources/` is the case that separates a string prefix from a
// directory: a mutation trimming the trailing slash survived the first suite.
func TestOwnsDiffDirectoryPrefixIsNotAStringPrefix(t *testing.T) {
	dir, base := mkRunRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "internal/res"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := mkOwnsScript(t, "internal/res/")

	rep, err := OwnsDiff(script, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if !hasPath(rep.Unowned, "internal/resources/populate.go") {
		t.Errorf("owning internal/res/ silently owns internal/resources/: %v", rep.Unowned)
	}
	if !hasPath(rep.Untouched, "internal/res/") {
		t.Errorf("a declared directory the run never wrote into is not reported: %v", rep.Untouched)
	}
}

// One path, one row: a file that is both staged-as-deleted and present as
// untracked appears in the diff AND in the status output, and counting it twice
// would inflate the population every ratio is read against.
func TestOwnsDiffCountsEachPathOnce(t *testing.T) {
	dir, base := mkRunRepo(t)
	cmd := exec.Command("git", "rm", "--cached", "-q", "internal/store/store.go")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git rm --cached: %v: %s", err, out)
	}
	script := mkOwnsScript(t, "internal/router/router.go")

	rep, err := OwnsDiff(script, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, p := range rep.Unowned {
		seen[p]++
	}
	if seen["internal/store/store.go"] > 1 {
		t.Errorf("one path counted twice: %v", rep.Unowned)
	}
	if rep.Changed != len(seen)+1 {
		t.Errorf("Changed = %d against %d distinct unowned + 1 owned", rep.Changed, len(seen))
	}
}

// `git diff` REFRESHES THE INDEX of the tree it runs in. The target of a gate is
// read-only by this project's first rule, and an audit that writes into a
// repository another agent is working in breaks it — `--no-optional-locks` is
// git's own way of not doing that, and nothing pinned it.
func TestOwnsDiffDoesNotWriteTheTargetIndex(t *testing.T) {
	dir, base := mkRunRepo(t)
	script := mkOwnsScript(t, "internal/router/router.go")
	index := filepath.Join(dir, ".git", "index")
	// A stale index is the state that makes git want to write: touch the files
	// so their mtimes are newer than the index's recorded ones.
	now := time.Now().Add(2 * time.Second)
	for _, p := range []string{"internal/router/router.go", "internal/store/store.go"} {
		if err := os.Chtimes(filepath.Join(dir, p), now, now); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OwnsDiff(script, dir, base); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		t.Errorf("the audit wrote the target's index: %v/%d -> %v/%d",
			before.ModTime(), before.Size(), after.ModTime(), after.Size())
	}
}

// The header says what was compared. `..HEAD` where the tool read the working
// tree is a lie about the comparison, and it is the label a reader trusts.
func TestOwnsDiffHeaderNamesTheWorkingTree(t *testing.T) {
	dir, base := mkRunRepo(t)
	script := mkOwnsScript(t, "internal/router/router.go")
	rep, err := OwnsDiff(script, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Head != "worktree" {
		t.Errorf("Head = %q, want worktree — the report compares against the tree, not HEAD", rep.Head)
	}
	if rep.Base == "" || !strings.HasPrefix(base, rep.Base) {
		t.Errorf("Base = %q, want the short form of %q", rep.Base, base)
	}
}

// A -root that is not a repository at all must not be reported as a bad BASE:
// the base is merely what the next git call happens to name.
func TestOwnsDiffNonRepoRootNamesTheRoot(t *testing.T) {
	script := mkOwnsScript(t, "internal/router/router.go")
	notRepo := t.TempDir()
	_, err := OwnsDiff(script, notRepo, "HEAD")
	if err == nil {
		t.Fatal("a root that is not a repository was accepted")
	}
	if !strings.Contains(err.Error(), "not a git repository") || !strings.Contains(err.Error(), "-root") {
		t.Errorf("the refusal blames the wrong argument: %v", err)
	}
}

// A RENAME spends a second `-z` field on the old path, with no status of its
// own. Ordinarily that field is harmless — it fails the `XY ` shape test — but
// a file whose own name begins `?? ` passes it and is then added as a path that
// does not exist. The guard survived a mutation until this case existed.
func TestOwnsDiffRenameSecondFieldIsNotAStatus(t *testing.T) {
	dir, base := mkRunRepo(t)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	odd := "?? y.go"
	if err := os.WriteFile(filepath.Join(dir, odd), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "a file whose name looks like a status")
	git("mv", odd, "renamed.go")

	script := mkOwnsScript(t, "internal/router/router.go")
	rep, err := OwnsDiff(script, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if hasPath(rep.Unowned, "y.go") {
		t.Errorf("the old path of a rename was read as an untracked status line: %v", rep.Unowned)
	}
}

// The inferred root is ANNOUNCED. A workspace naming a target that is not there
// makes the audit answer about the workspace instead, and the report has to say
// which tree it read — the same rule the file-existence checks follow.
func TestOwnsDiffAnnouncesAnInferredRoot(t *testing.T) {
	dir, base := mkRunRepo(t)
	// Stand inside a gate workspace whose target does not exist, with no -root.
	wsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(wsDir, "gate.json"), []byte(`{"name":"x","target":"/nope/not/here"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(wsDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatal(err)
		}
	}()

	script := mkOwnsScript(t, "internal/router/router.go")
	// The workspace is not a repository, so the audit refuses — and the refusal
	// plus the note both have to name the tree it chose, not the one it was
	// asked about.
	rep, err := OwnsDiff(script, "", base)
	if err == nil {
		_ = dir
		t.Fatal("a workspace that is not a repository was audited without complaint")
	}
	if rep.RootNote == "" {
		t.Errorf("the root was inferred and the report does not say so")
	}
}

// `--no-renames` makes git report a move as delete+add, which is what the map
// is asked about: the old path is one a section owned and the new one may be
// owned by nobody. Without it the pair collapses into one rename row and the
// old path vanishes from the population.
func TestOwnsDiffRenameIsDeletePlusAdd(t *testing.T) {
	dir, base := mkRunRepo(t)
	cmd := exec.Command("git", "mv", "internal/store/store.go", "internal/store/moved.go")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git mv: %v: %s", err, out)
	}
	script := mkOwnsScript(t, "internal/store/store.go")

	rep, err := OwnsDiff(script, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if !hasPath(rep.Unowned, "internal/store/moved.go") {
		t.Errorf("the new path of a move is not reported: %v", rep.Unowned)
	}
	if hasPath(rep.Untouched, "internal/store/store.go") {
		t.Errorf("an owned file the run MOVED reads as never written: %v", rep.Untouched)
	}
}

// Both lists are sorted. An audit read by eye across two runs is diffed by eye,
// and map iteration order would make every run look different from the last.
func TestOwnsDiffOutputIsSorted(t *testing.T) {
	dir, base := mkRunRepo(t)
	script := mkOwnsScript(t, "internal/a.go", "internal/z.go", "internal/m.go")
	rep, err := OwnsDiff(script, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if !sort.StringsAreSorted(rep.Untouched) {
		t.Errorf("untouched is unsorted: %v", rep.Untouched)
	}
	if !sort.StringsAreSorted(rep.Unowned) {
		t.Errorf("unowned is unsorted: %v", rep.Unowned)
	}
}

// A DOTFILE is a path. Trimming the leading dot as punctuation turned a
// declared `.env.example` into a map entry named `env.example`, which matches
// nothing in the tree — so the file printed as owned by nobody AND as never
// written, and this repository banked those two rows as a typo in the script it
// was auditing. Both directions here, because one strip produced both rows.
func TestOwnsDiffDotfilesAreOwned(t *testing.T) {
	dir, base := mkRunRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".env.example"), []byte("X=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := mkOwnsScript(t, ".env.example — the one variable this section corrects", "internal/router/router.go")

	rep, err := OwnsDiff(script, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if hasPath(rep.Unowned, ".env.example") {
		t.Errorf("a declared dotfile the run wrote reads as owned by no section: %v", rep.Unowned)
	}
	if hasPath(rep.Untouched, "env.example") || hasPath(rep.Untouched, ".env.example") {
		t.Errorf("the declared dotfile reads as never written: %v", rep.Untouched)
	}
}
