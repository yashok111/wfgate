package main

import (
	"fmt"
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
	mustContain(t, err.Error(), "not in HEAD", "wf add")
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

// The four ways the first version of `-whole` could be defeated, each found by
// an external review vector and each reproduced before it was fixed.
func TestApplyWholeRefusesTheShapesThatDefeatedIt(t *testing.T) {
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
	write(t, filepath.Join(tmp, "a.md"), "# A\n\nalpha\nbeta\ngamma\ndelta\n")
	// The artifact holds the trailer as TEXT — this repository's own README is
	// exactly that file — so the per-edit body line `wf apply -f` writes puts
	// the trailer into the commit MESSAGE of a verified batch.
	write(t, filepath.Join(tmp, "b.md"), "# B\n\nwf-apply-whole: artifacts/x.md is the trailer, documented here\n")
	_, err = runWF(t, wsDir, "add", filepath.Join(tmp, "a.md"), filepath.Join(tmp, "b.md"))
	noErr(t, err, "add")
	_, err = runWF(t, wsDir, "round", "new")
	noErr(t, err, "round new")

	// A DIRECTORY passes every other check — `ls-files` lists its children,
	// `git diff HEAD` counts their hunks, and the commit stages the tree — so
	// `-whole artifacts/` would adopt every artifact under a report saying one
	// file.
	write(t, filepath.Join(wsDir, "artifacts", "a.md"), "# A\n\nALPHA\nbeta\ngamma\nDELTA\n")
	write(t, filepath.Join(wsDir, "artifacts", "b.md"), "# B\n\nwf-apply-whole: artifacts/x.md is the trailer, documented HERE\n")
	_, err = runWF(t, wsDir, "apply", "-whole", "artifacts")
	if err == nil {
		t.Fatal("a directory must be refused")
	}
	mustContain(t, err.Error(), "not a regular file")

	// A brand-new file the operator had already `git add`ed: `ls-files` reads
	// the INDEX and reported it tracked, so the "that is `wf add`" refusal was
	// bypassed and a first draft was priced as a round's edits.
	write(t, filepath.Join(wsDir, "artifacts", "c.md"), "# C\n\nfresh\n")
	gitIn(t, wsDir, "add", "artifacts/c.md")
	_, err = runWF(t, wsDir, "apply", "-whole", "artifacts/c.md")
	if err == nil {
		t.Fatal("a staged but uncommitted new file must be refused")
	}
	mustContain(t, err.Error(), "not in HEAD", "wf add")
	gitIn(t, wsDir, "rm", "-q", "-f", "--cached", "artifacts/c.md")
	noErr(t, os.Remove(filepath.Join(wsDir, "artifacts", "c.md")), "cleanup")

	// The host's own `diff.interHunkContext`, which is what the pin defends
	// against: at 3 the two changes four lines apart merge into ONE hunk, and
	// the round's recorded edit count halves with nothing in the output saying
	// so.
	hostCfg := filepath.Join(tmp, "hostgitconfig")
	write(t, hostCfg, "[diff]\n\tinterHunkContext = 3\n")
	t.Setenv("GIT_CONFIG_GLOBAL", hostCfg)

	// A rewrite the operator had already STAGED: the worktree-vs-index diff is
	// then empty, and the round used to be priced `N hunk(s), 0 changed
	// line(s)` — a zero denominator reaching the cost meter.
	gitIn(t, wsDir, "add", "artifacts/a.md")
	out, err := runWF(t, wsDir, "apply", "-whole", "artifacts/a.md")
	noErr(t, err, "apply -whole over a staged rewrite")
	mustContain(t, out, "2 hunk(s), 4 changed line(s)")
	mustNotContain(t, out, "0 changed line(s)")

	// An adopted artifact whose own text quotes the trailer must not mark a
	// VERIFIED batch as an adoption: the same defence `applyCommitEdits` makes
	// by reading the last match rather than the first.
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/b.md\n<<<<\nwf-apply-whole: artifacts/x.md is the trailer, documented HERE\n====\nwf-apply-whole: artifacts/x.md is the trailer, documented BELOW\n>>>>\n")
	_, err = runWF(t, wsDir, "apply", "-f", edits)
	noErr(t, err, "apply -f quoting the trailer")
	stats, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	// The COUNT, not the presence of the phrase: the note prints once per round
	// line whatever N is, so counting occurrences would pass at N=2.
	mustContain(t, stats, "2 apply batch(es)", "(1 ADOPTED whole, verified by no batch)")
}

// `-rule` behaves the same under both spellings of the command. It printed a
// note and committed for one revision, which is a specification defect by this
// repository's own rule that command semantics ARE specification: the reason
// the refusal exists — a note arrives after the round has already spent its
// edit — is stronger for a restructure, not weaker, because a restructure is
// the batch most likely to move a rule.
func TestApplyWholeRefusesAnUnvisitedRuleSiteLikeApplyF(t *testing.T) {
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
	// One rule, marked in TWO artifacts. The adoption writes one of them.
	rule := "- [R1] The composition stays a pure function, never a branch.\n"
	write(t, filepath.Join(tmp, "one.md"), "# One\n\n"+rule+"\nalpha\nbeta\n")
	write(t, filepath.Join(tmp, "two.md"), "# Two\n\n"+rule)
	_, err = runWF(t, wsDir, "add", filepath.Join(tmp, "one.md"), filepath.Join(tmp, "two.md"))
	noErr(t, err, "add")
	_, err = runWF(t, wsDir, "round", "new")
	noErr(t, err, "round new")

	write(t, filepath.Join(wsDir, "artifacts", "one.md"), "# One\n\n"+rule+"\nALPHA\nBETA\n")
	out, err := runWF(t, wsDir, "apply", "-whole", "artifacts/one.md", "-rule", "R1")
	wantFindings(t, err, "adoption with an unvisited rule site")
	mustContain(t, out, "adoption REFUSED", "artifacts/two.md")
	// REFUSED means nothing was committed, or the refusal is decorative.
	head, err := gitx.Repo{Dir: wsDir}.ShowFile("HEAD", "artifacts/one.md")
	noErr(t, err, "show HEAD copy")
	if strings.Contains(string(head), "ALPHA") {
		t.Fatal("the refusal committed anyway")
	}

	// -rule-ack is the waiver, exactly as it is for -f.
	out, err = runWF(t, wsDir, "apply", "-whole", "artifacts/one.md", "-rule", "R1", "-rule-ack")
	noErr(t, err, "adoption with -rule-ack")
	mustContain(t, out, "committed ")
}

// The guards a mutation sweep found unpinned, each with the case that kills the
// mutation. Four survived the whole suite: the `os.Stat` refusal, the
// absolute-path normalisation, the cost-meter note, and `-m`.
func TestApplyWholePinsTheGuardsTheSweepFound(t *testing.T) {
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
	// Enough history for the cost meter to have a claim to make: the note is
	// silent without one, which is how its suppression went unnoticed.
	body := "# Doc\n\n"
	for i := 0; i < 400; i++ {
		body += fmt.Sprintf("line %d of the document\n", i)
	}
	write(t, filepath.Join(tmp, "doc.md"), body)
	_, err = runWF(t, wsDir, "add", filepath.Join(tmp, "doc.md"))
	noErr(t, err, "add")

	art := filepath.Join(wsDir, "artifacts", "doc.md")

	// A file that is tracked at HEAD and DELETED from the worktree: without the
	// stat refusal the deletion is adopted as an edit and committed.
	noErr(t, os.Remove(art), "remove the artifact")
	_, err = runWF(t, wsDir, "apply", "-whole", "artifacts/doc.md")
	if err == nil {
		t.Fatal("a deleted artifact must be refused, not adopted as its own removal")
	}
	write(t, art, body)

	// The workspace's own bookkeeping is not an artifact: adopting it prices
	// the marker file into the round's edit count.
	write(t, filepath.Join(wsDir, "gate.json"), "{}\n")
	_, err = runWF(t, wsDir, "apply", "-whole", "gate.json")
	if err == nil {
		t.Fatal("a path outside artifacts/ must be refused")
	}
	mustContain(t, err.Error(), "not under artifacts/")
	gitIn(t, wsDir, "checkout", "--", "gate.json")

	// Two rounds of history, so `batchNote` has a corpus to predict from.
	for r := 1; r <= 2; r++ {
		_, err = runWF(t, wsDir, "round", "new")
		noErr(t, err, "round new")
		edits := filepath.Join(tmp, fmt.Sprintf("e%d.txt", r))
		write(t, edits, fmt.Sprintf("=== artifacts/doc.md\n<<<<\nline %d of the document\n====\nline %d of the document, revised\n>>>>\n", r, r))
		_, err = runWF(t, wsDir, "apply", "-f", edits)
		noErr(t, err, "apply -f")
		findings := filepath.Join(tmp, fmt.Sprintf("f%d.md", r))
		write(t, findings, "| # | level | finding | closed by |\n|---|---|---|---|\n| 1 | MINOR | artifacts/doc.md — a finding | prose |\n")
		_, err = runWF(t, wsDir, "round", "record", findings)
		noErr(t, err, "round record")
		_, err = runWF(t, wsDir, "freeze")
		noErr(t, err, "freeze")
	}
	_, err = runWF(t, wsDir, "round", "new")
	noErr(t, err, "round new")

	// The restructure, reached by an ABSOLUTE path — no test passed one, and
	// without the normalisation `SafeJoin` refuses every absolute path by
	// construction.
	rewritten := "# Doc\n\n"
	for i := 0; i < 400; i++ {
		if i%3 == 0 {
			rewritten += fmt.Sprintf("LINE %d of the document, restructured\n", i)
			continue
		}
		rewritten += fmt.Sprintf("line %d of the document\n", i)
	}
	write(t, art, rewritten)
	out, err := runWF(t, wsDir, "apply", "-whole", art, "-m", "restructure: the two sections merged")
	noErr(t, err, "apply -whole by absolute path")
	// The cost meter is the whole reason this command exists; it printed in no
	// test, which is how a zero denominator silencing it went unnoticed.
	mustContain(t, out, "predicts ~", "committed ")
	// -m replaces the subject, and nothing else.
	subject, err := gitx.Repo{Dir: wsDir}.Run("log", "-1", "--format=%s")
	noErr(t, err, "read the subject")
	if strings.TrimSpace(subject) != "restructure: the two sections merged" {
		t.Fatalf("-m must set the subject, got %q", subject)
	}
	stats, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, stats, "ADOPTED whole, verified by no batch")

	// A BINARY artifact that was rewritten end to end reports no `@@` line at
	// all, so the hunk count is zero and the refusal used to assert the file
	// was unchanged. The direction is right; the sentence was false, and an
	// operator acts on it.
	bin := filepath.Join(wsDir, "artifacts", "blob.bin")
	noErr(t, os.WriteFile(bin, []byte{0, 1, 2, 0, 3}, 0o644), "write the binary artifact")
	gitIn(t, wsDir, "add", "artifacts/blob.bin")
	gitIn(t, wsDir, "commit", "-q", "-m", "a binary artifact")
	noErr(t, os.WriteFile(bin, []byte{9, 9, 0, 9, 9, 9}, 0o644), "rewrite the binary artifact")
	_, err = runWF(t, wsDir, "apply", "-whole", "artifacts/blob.bin")
	if err == nil {
		t.Fatal("a binary artifact must be refused")
	}
	mustContain(t, err.Error(), "BINARY")
	mustNotContain(t, err.Error(), "byte-identical to HEAD")
}

// The trailer is a WIRE FORMAT: it is written into git history, and existing
// workspaces carry the literal. Writer and reader both derive from the symbol,
// so the suite cannot see it move — the sixteenth wave's banked class, one
// constant over.
func TestApplyWholeTrailerIsPinnedByValue(t *testing.T) {
	if applyWholeTrailer != "wf-apply-whole:" {
		t.Fatalf("the trailer is written into commits that already exist; changing it silently orphans them, got %q", applyWholeTrailer)
	}
	if applyTrailer != "wf-apply:" {
		t.Fatalf("same for the meter's own trailer, got %q", applyTrailer)
	}
}
