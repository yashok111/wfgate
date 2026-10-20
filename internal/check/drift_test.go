package check

import (
	"testing"

	"github.com/yashok111/wfgate/internal/gitx"
)

// The drift matrix: one target commit produces every kind at once, because the
// point of the check is classification, not detection — an operator acts
// differently on "re-read" than on "re-aim by +1".
func TestFindDrift(t *testing.T) {
	target := mkrepo(t, map[string]string{
		"src/a.go":     "l1\nl2\nl3\nl4\nl5\n",
		"src/b.go":     "b1\nb2\nb3\n",
		"src/gone.go":  "x\n",
		"src/clean.go": "c1\nc2\n",
	})
	frozen, err := gitx.Repo{Dir: target}.Head()
	if err != nil {
		t.Fatal(err)
	}

	// One move: a.go gains a line on top and an edit at old line 4, b.go gains
	// a line spliced after b1, gone.go is deleted, clean.go stays.
	commit(t, target, "target moves", map[string]string{
		"src/a.go": "l0\nl1\nl2\nl3\nL4!\nl5\n",
		"src/b.go": "b1\nbX\nb2\nb3\n",
	})
	run(t, target, "rm", "-q", "src/gone.go")
	run(t, target, "commit", "-q", "-m", "delete gone")

	cits := []Citation{
		{Artifact: "x.md", AtLine: 1, Raw: "a.go:4", Path: "a.go", Start: 4, End: 4},
		{Artifact: "x.md", AtLine: 2, Raw: "a.go:2-3", Path: "a.go", Start: 2, End: 3},
		{Artifact: "x.md", AtLine: 3, Raw: "b.go:1-3", Path: "b.go", Start: 1, End: 3},
		{Artifact: "x.md", AtLine: 4, Raw: "b.go:3", Path: "b.go", Start: 3, End: 3},
		{Artifact: "x.md", AtLine: 5, Raw: "gone.go:1", Path: "gone.go", Start: 1, End: 1},
		{Artifact: "x.md", AtLine: 6, Raw: "clean.go:2", Path: "clean.go", Start: 2, End: 2},
		{Artifact: "x.md", AtLine: 7, Raw: "nowhere.go:1", Path: "nowhere.go", Start: 1, End: 1},
	}
	ds, clean, err := FindDrift(target, frozen, cits)
	if err != nil {
		t.Fatal(err)
	}
	if clean != 1 {
		t.Fatalf("want 1 clean citation, got %d: %+v", clean, ds)
	}
	want := map[int]string{1: "edited", 2: "shifted", 3: "edited", 4: "shifted", 5: "gone", 7: "unresolved"}
	if len(ds) != len(want) {
		t.Fatalf("want %d drifted, got %d: %+v", len(want), len(ds), ds)
	}
	for _, d := range ds {
		if want[d.AtLine] != d.Kind {
			t.Errorf("citation at line %d: kind %q, want %q (%s)", d.AtLine, d.Kind, want[d.AtLine], d.Msg)
		}
		// A shift must come with the re-aim, or it is a search, not a patch.
		if d.Kind == "shifted" && d.AtLine == 2 && (d.NewStart != 3 || d.NewEnd != 4) {
			t.Errorf("a.go:2-3 must re-aim to 3-4, got %d-%d", d.NewStart, d.NewEnd)
		}
		if d.Kind == "shifted" && d.AtLine == 4 && d.NewStart != 4 {
			t.Errorf("b.go:3 must re-aim to 4, got %d", d.NewStart)
		}
	}
}

// An insertion exactly AFTER a single cited line neither edits nor shifts it.
func TestFindDrift_insertionBelowIsClean(t *testing.T) {
	target := mkrepo(t, map[string]string{"f.go": "l1\nl2\n"})
	frozen, err := gitx.Repo{Dir: target}.Head()
	if err != nil {
		t.Fatal(err)
	}
	commit(t, target, "append", map[string]string{"f.go": "l1\nl2\nl3\n"})
	ds, clean, err := FindDrift(target, frozen, []Citation{
		{Artifact: "x.md", AtLine: 1, Raw: "f.go:2", Path: "f.go", Start: 2, End: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 0 || clean != 1 {
		t.Fatalf("append below the citation must be clean, got %+v", ds)
	}
}
