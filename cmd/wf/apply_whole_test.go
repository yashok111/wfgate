package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.sumka.site/yakov/wfgate/internal/gitx"
)

// `wf apply -whole` adopts a rewrite the operator made by hand, and the reason
// it exists is the METER. Measured on P3g: a structural merge of two sections
// of one script is forty-odd non-adjacent edits in a single file, it was
// applied with `git commit`, and four of thirteen rounds then contributed no
// edit count at all — so the cost meter priced the gate on the rounds that used
// the block format and the prediction came from a corpus those rounds did not
// write.
func TestApplyWholeAdoptsARewriteAndPricesTheRound(t *testing.T) {
	setGitEnv(t)
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	write(t, filepath.Join(target, "src", "main.go"), "package main\n")
	gitIn(t, target, "init", "-q", "-b", "main")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-q", "-m", "initial")

	wsDir := filepath.Join(tmp, "ws")
	_, err := runWF(t, tmp, "init", "demo", "-target", target, "-dir", wsDir)
	noErr(t, err, "init")
	write(t, filepath.Join(tmp, "doc.md"), "# Doc\n\nalpha\nbeta\ngamma\ndelta\n")
	_, err = runWF(t, wsDir, "add", filepath.Join(tmp, "doc.md"))
	noErr(t, err, "add")

	// A round, so the meter has a window to attribute the adoption to — the
	// point of the command is that this window stops reading as a hand-edited
	// round with no edit count at all.
	_, err = runWF(t, wsDir, "round", "new")
	noErr(t, err, "round new")

	art := filepath.Join(wsDir, "artifacts", "doc.md")

	// Nothing rewritten yet: an edit count taken from an unchanged file would
	// price a round that changed nothing.
	out, err := runWF(t, wsDir, "apply", "-whole", "artifacts/doc.md")
	if err == nil {
		t.Fatalf("an unchanged file must be refused, got:\n%s", out)
	}
	mustContain(t, err.Error(), "byte-identical to HEAD")

	// The restructure: two separate regions of one file, which is the shape the
	// block format prices worst and the meter loses entirely.
	write(t, art, "# Doc\n\nALPHA\nbeta\ngamma\nDELTA\n")

	out, err = runWF(t, wsDir, "apply", "-whole", "artifacts/doc.md", "--dry-run")
	noErr(t, err, "dry run")
	mustContain(t, out, "artifacts/doc.md: 2 hunk(s)", "would be committed as 2 edit(s)")
	// A dry run writes nothing, so the rewrite is still uncommitted: HEAD must
	// still carry the pre-rewrite bytes.
	head, err := gitx.Repo{Dir: wsDir}.ShowFile("HEAD", "artifacts/doc.md")
	noErr(t, err, "show HEAD copy")
	if strings.Contains(string(head), "ALPHA") {
		t.Fatal("the dry run committed the rewrite")
	}

	out, err = runWF(t, wsDir, "apply", "-whole", "artifacts/doc.md")
	noErr(t, err, "apply -whole")
	// The count is honest about COST and says nothing about whether each change
	// inside it landed — the sentence is owed on every adoption, not over a
	// threshold, because this command's whole population is that shape.
	mustContain(t, out, "2 hunk(s)", "ADOPTION, not a verified batch", "committed ")

	// The trailer the meter reads, and the second one that keeps the act
	// distinguishable from a batch this command verified.
	log, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, log, "1 apply batch(es)", "2 edit(s)", "ADOPTED whole, verified by no batch")

	// A file that is new to the workspace is `wf add`'s job: routing it here
	// would record a whole first draft as a round's edit count.
	write(t, filepath.Join(wsDir, "artifacts", "new.md"), "# New\n")
	out, err = runWF(t, wsDir, "apply", "-whole", "artifacts/new.md")
	if err == nil {
		t.Fatalf("an untracked file must be refused, got:\n%s", out)
	}
	mustContain(t, err.Error(), "untracked", "wf add")
	noErr(t, os.Remove(filepath.Join(wsDir, "artifacts", "new.md")), "cleanup")

	// The refusal `apply` and `add` share: a path argument written by an agent
	// that has confused the workspace with the repository under review.
	out, err = runWF(t, wsDir, "apply", "-whole", "../target/src/main.go")
	if err == nil {
		t.Fatalf("a path escaping the workspace must be refused, got:\n%s", out)
	}
	mustContain(t, err.Error(), "escapes the workspace")

	// The two flags are different acts, not two spellings of one.
	out, err = runWF(t, wsDir, "apply", "-f", "edits.json", "-whole", "artifacts/doc.md")
	if err == nil {
		t.Fatalf("-f with -whole must be refused, got:\n%s", out)
	}
	mustContain(t, err.Error(), "exclusive")
}
