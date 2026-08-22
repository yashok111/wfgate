package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
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
