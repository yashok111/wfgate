package gitx

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repo(t *testing.T) Repo {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@example.invalid"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return Repo{Dir: dir}
}

// git QUOTES a path containing a space (or any non-ASCII byte) in plain
// porcelain: `?? "artifacts/my draft.md"`. A caller that split such a line on
// its last space read `draft.md"` as the path, so a dirty ARTIFACT was
// classified as round bookkeeping and verify printed the reassuring "commit it;
// do not re-freeze" over a file that differs from the freeze.
func TestDirty_pathsWithSpacesAreNotQuoted(t *testing.T) {
	r := repo(t)
	if err := os.MkdirAll(filepath.Join(r.Dir, "artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}
	// git collapses a WHOLLY untracked directory to one `?? artifacts/` entry,
	// so seed a tracked file first — as a real workspace always has.
	if err := os.WriteFile(filepath.Join(r.Dir, "artifacts", "seed.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit("seed", []string{"artifacts/seed.md"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"artifacts/my draft.md", "artifacts/plain.md"} {
		if err := os.WriteFile(filepath.Join(r.Dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cs, err := r.Dirty(".")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range cs {
		got[c.Path] = c.Status
	}
	for _, want := range []string{"artifacts/my draft.md", "artifacts/plain.md"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("path %q must come back raw, got %+v", want, cs)
		}
	}
	if len(cs) != 2 {
		t.Fatalf("want 2 changes, got %d: %+v", len(cs), cs)
	}
	for _, c := range cs {
		if !c.Untracked() {
			t.Fatalf("both files are untracked: %+v", c)
		}
	}
}

// The diff algorithm is an UNDECLARED INPUT unless the wrapper pins it, and it
// moves the numbers this tool reports. Measured 2026-08-25 over the three
// closed gates: `wf stats` priced p2e at 4586 changed lines under myers and
// 4482 under histogram (2.8% apart, and 5.3% on its round 2), which moves a
// round's cost ratio. git's default is myers; the box that produced every
// banked figure carried `diff.algorithm = histogram` in ~/.gitconfig, so the
// corpus and a stock machine did not agree and nothing said so.
//
// The fixture is the real region where they diverge: artifacts/decisions.md of
// the p2e gate at r4 (commit d698b29), lines 520-553 before and 625-656 after —
// markdown prose whose paragraph boundaries the two algorithms attribute
// differently. myers reads it as 9 added / 11 removed, histogram as 8 / 10.
//
// The repo-local config below sets myers deliberately: that is precisely what
// an operator's own gitconfig would do, and the pin must beat it.
func TestRun_pinsTheDiffAlgorithmOverRepoConfig(t *testing.T) {
	r := repo(t)
	if _, err := r.Run("config", "diff.algorithm", "myers"); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"algo-before.md", "algo-after.md"} {
		body, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r.Dir, "d.md"), body, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Commit(fmt.Sprintf("rev %d", i), []string{"d.md"}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := r.Run("show", "--numstat", "--format=", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	const (
		histogram = "8\t10\td.md"
		myers     = "9\t11\td.md"
	)
	if got == myers {
		t.Fatalf("the repo's own diff.algorithm won: got %q (myers), want %q (histogram) — "+
			"the wrapper is reading the operator's config instead of pinning the algorithm", got, histogram)
	}
	if got != histogram {
		t.Fatalf("numstat = %q, want %q; neither is the myers reading %q, so the fixture no longer "+
			"separates the two algorithms and this test proves nothing", got, histogram, myers)
	}
}

// The pin must not disturb what the caller asked for: several call sites pass
// their own `-c` ahead of the subcommand, and the pin now sits in front of it.
//
// The path is Cyrillic because that is the only thing core.quotePath governs —
// a SPACE is quoted in porcelain regardless, which is why `Dirty` reads -z
// rather than trusting the setting. Quoted DATA, per the repository's rule on
// non-Latin text.
func TestRun_pinDoesNotDisplaceCallerFlags(t *testing.T) {
	r := repo(t)
	if err := os.WriteFile(filepath.Join(r.Dir, "тест.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run("-c", "core.quotePath=false", "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "тест.md") || strings.Contains(out, `\321`) {
		t.Fatalf("core.quotePath=false did not survive the pin: %q", out)
	}
}

// File CONTENT must not be trimmed. `Run` strips trailing newlines, so a file
// ending in a blank line lost it and every citation into its last lines shifted
// — a false out-of-range under `wf check cite -at`.
func TestShowFile_keepsTrailingNewlines(t *testing.T) {
	r := repo(t)
	body := "one\ntwo\n\n\n"
	if err := os.WriteFile(filepath.Join(r.Dir, "f.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run("add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit("add f", []string{"f.txt"}); err != nil {
		t.Fatal(err)
	}
	got, err := r.ShowFile("HEAD", "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("ShowFile must return the bytes verbatim: %q, want %q", got, body)
	}
	if n := strings.Count(string(got), "\n"); n != 4 {
		t.Fatalf("a trimmed read moves the last line's number: %d newlines, want 4", n)
	}
}

// `<ref>:<path>` resolves from the REPO ROOT while everything else this package
// reports is relative to Dir. Unreachable through today's callers — a workspace
// refuses a target whose `.git` is not directly under it — and this is the single
// seam, so the helper is right for any Dir.
func TestShowFile_isRelativeToDirNotTheRepoRoot(t *testing.T) {
	r := repo(t)
	sub := filepath.Join(r.Dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "f.txt"), []byte("inside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run("add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit("add sub/f", []string{"sub/f.txt"}); err != nil {
		t.Fatal(err)
	}
	got, err := Repo{Dir: sub}.ShowFile("HEAD", "f.txt")
	if err != nil {
		t.Fatalf("a path relative to Dir must resolve: %v", err)
	}
	if string(got) != "inside\n" {
		t.Fatalf("got %q", got)
	}
}

// CommitPaths answers "what did this commit touch" for the shapes
// `show --name-only` gets wrong. Each was named by a reviewer over the closure
// refusal that reads it: a legitimate closure refused because git said nothing
// is worse than the mistake the refusal exists for.
func TestCommitPaths_theShapesShowNameOnlyGetsWrong(t *testing.T) {
	r := repo(t)
	wr := func(rel, body string) {
		t.Helper()
		p := filepath.Join(r.Dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
		t.Helper()
		out, err := r.Run(args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(out)
	}
	has := func(paths []string, want string) bool {
		for _, p := range paths {
			if p == want {
				return true
			}
		}
		return false
	}

	// The ROOT commit: `show --name-only` prints its files, `diff-tree` without
	// --root prints nothing at all.
	wr("artifacts/decisions.md", "one\n")
	git("add", "-A")
	git("commit", "-q", "-m", "root")
	root := git("rev-parse", "HEAD")
	paths, err := r.CommitPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	if !has(paths, "artifacts/decisions.md") {
		t.Fatalf("the first commit of a repository touched its files: %v", paths)
	}

	// A path git QUOTES in porcelain output.
	wr("artifacts/a draft.md", "two\n")
	git("add", "-A")
	git("commit", "-q", "-m", "quoted")
	paths, err = r.CommitPaths(git("rev-parse", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	if !has(paths, "artifacts/a draft.md") {
		t.Fatalf("a quoted path compares equal to nothing: %v", paths)
	}

	// A RENAME reports both ends: the caller asks whether a path was touched,
	// and half a rename is a path that was.
	git("mv", "artifacts/decisions.md", "artifacts/renamed.md")
	git("commit", "-q", "-m", "rename")
	paths, err = r.CommitPaths(git("rev-parse", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	if !has(paths, "artifacts/decisions.md") || !has(paths, "artifacts/renamed.md") {
		t.Fatalf("a rename touched both paths: %v", paths)
	}

	// A MERGE: `show --name-only` suppresses the diff entirely, so a closure
	// resting on one would be refused as having touched nothing.
	base := git("rev-parse", "HEAD")
	git("checkout", "-q", "-b", "side")
	wr("artifacts/side.md", "side\n")
	git("add", "-A")
	git("commit", "-q", "-m", "side")
	git("checkout", "-q", "main")
	wr("rounds/r1/findings.md", "main\n")
	git("add", "-A")
	git("commit", "-q", "-m", "main")
	git("merge", "-q", "--no-ff", "-m", "merge", "side")
	merge := git("rev-parse", "HEAD")
	if merge == base {
		t.Fatal("the merge did not happen")
	}
	paths, err = r.CommitPaths(merge)
	if err != nil {
		t.Fatal(err)
	}
	if !has(paths, "artifacts/side.md") {
		t.Fatalf("a merge commit touched what it merged: %v", paths)
	}
	// Deduplicated: -m prints one diff per parent, and the caller reads the
	// list as a set.
	seen := map[string]int{}
	for _, p := range paths {
		seen[p]++
	}
	for p, n := range seen {
		if n > 1 {
			t.Fatalf("%s listed %d times: %v", p, n, paths)
		}
	}
}
