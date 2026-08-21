// Command wf is the gate harness.
//
// A review gate over a large change produces two kinds of cost. One is the
// reviewing itself, which is judgement and is what the rounds are for. The other
// is bookkeeping: which revision of which artifact a reviewer read, what a round
// was asked to close, whether a batch of corrections actually landed, and
// whether the copy of a rule in the second artifact was corrected along with the
// first. That second cost is mechanical, it is where the last gate lost most of
// its cheap rounds, and it is all this tool does.
//
// The workspace is a git repository. Every applied batch is a commit, every
// frozen round is a tag, and the delta a reviewer reads is a diff. Nothing here
// reimplements versioning.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"git.sumka.site/yakov/wfgate/internal/apply"
	"git.sumka.site/yakov/wfgate/internal/check"
	"git.sumka.site/yakov/wfgate/internal/gitx"
	"git.sumka.site/yakov/wfgate/internal/ws"
)

const usage = `wf — gate harness

  wf init <name> -target <repo>   create a workspace (its own git repo)
  wf status                       manifest, round, dirty artifacts
  wf apply -f edits.json          all-or-nothing batch of edits, then commit
  wf round new [-title T]         open the next round, scaffold its findings file
  wf freeze                       hash the artifacts, commit, tag gate-rN
  wf verify [-round N]            the pre-launch check: the tree is exactly what was frozen
  wf delta [-from REF] [-to REF]  the diff a delta round reviews
  wf check [cite|dup] [flags]     deterministic checks over the artifacts

Exit codes: 0 clean, 1 findings or failure, 2 usage.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "init":
		err = cmdInit(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "apply":
		err = cmdApply(os.Args[2:])
	case "round":
		err = cmdRound(os.Args[2:])
	case "freeze":
		err = cmdFreeze(os.Args[2:])
	case "verify":
		err = cmdVerify(os.Args[2:])
	case "delta":
		err = cmdDelta(os.Args[2:])
	case "check":
		err = cmdCheck(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		if errors.Is(err, errFindings) {
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "wf:", err)
		os.Exit(1)
	}
}

// errFindings separates "the check ran and reported problems" from "the tool
// broke". A caller scripting the gate needs the difference: one means read the
// report, the other means the run never happened.
var errFindings = errors.New("findings")

// parseMixed lets flags and positional arguments appear in any order.
//
// flag.FlagSet stops at the first non-flag argument, so `wf init p2c -target X`
// silently drops -target and reports a usage error naming the very flag the
// caller passed. Re-parsing the tail after each positional is the standard
// remedy, and it belongs here rather than in each subcommand.
func parseMixed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

func open() (*ws.Workspace, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return ws.Find(cwd)
}

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	target := fs.String("target", "", "path of the git repository under review (required)")
	dir := fs.String("dir", "", "workspace directory (default ./<name>)")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || *target == "" {
		return fmt.Errorf("usage: wf init <name> -target <repo> [-dir <path>]")
	}
	name := pos[0]
	root := *dir
	if root == "" {
		root = name
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	w, err := ws.Create(root, name, *target, time.Now())
	if err != nil {
		return err
	}
	head, _ := w.Git.Head()
	fmt.Printf("workspace %s\n  root   %s\n  target %s\n  head   %s\n", name, w.Root, w.M.Target, short(head))
	fmt.Printf("\nput the gate's artifacts under %s/ and run `wf apply`.\n", filepath.Join(root, "artifacts"))
	return nil
}

func cmdStatus(args []string) error {
	w, err := open()
	if err != nil {
		return err
	}
	head, _ := w.Git.Head()
	fmt.Printf("workspace %s\n  root   %s\n  target %s\n  round  %d\n  head   %s\n",
		w.M.Name, w.Root, w.M.Target, w.M.Round, short(head))
	arts, err := w.ArtifactList()
	if err != nil {
		return err
	}
	fmt.Printf("  artifacts (%d)\n", len(arts))
	for _, a := range arts {
		sha, subj, _ := w.Git.LastTouch(a)
		fmt.Printf("    %-40s %s %s\n", a, sha, trunc(subj, 50))
	}
	tags, _ := w.Git.Tags("gate-r")
	if len(tags) > 0 {
		fmt.Printf("  frozen rounds: %s\n", strings.Join(tags, " "))
	}
	dirty, err := w.Git.Dirty(".")
	if err != nil {
		return err
	}
	if len(dirty) == 0 {
		fmt.Println("  tree clean")
		return nil
	}
	fmt.Printf("  DIRTY (%d)\n", len(dirty))
	for _, d := range dirty {
		fmt.Println("    " + d)
	}
	return nil
}

func cmdApply(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ExitOnError)
	file := fs.String("f", "", "edits JSON file, or - for stdin (required)")
	msg := fs.String("m", "", "commit subject (default derived from the batch)")
	dry := fs.Bool("dry-run", false, "plan and report, write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("usage: wf apply -f edits.json [-m msg] [--dry-run]")
	}
	w, err := open()
	if err != nil {
		return err
	}
	var raw []byte
	if *file == "-" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(*file)
	}
	if err != nil {
		return err
	}
	var edits []apply.Edit
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&edits); err != nil {
		return fmt.Errorf("edits: %w", err)
	}
	if len(edits) == 0 {
		return fmt.Errorf("edits: empty batch")
	}

	results, bufs, planErr := apply.Plan(w.Root, edits)
	for _, r := range results {
		mark := "ok  "
		if !r.OK {
			mark = "FAIL"
		}
		fmt.Printf("  [%d] %s %s  (%d match) %s\n", r.Index, mark, r.Edit.File, r.Got, r.Reason)
	}
	if planErr != nil {
		fmt.Fprintf(os.Stderr, "\nbatch REJECTED — nothing was written. Fix every FAIL above and re-run.\n")
		return errFindings
	}
	if *dry {
		fmt.Printf("\ndry run: %d file(s) would change\n", len(bufs))
		return nil
	}
	written, err := apply.Write(w.Root, bufs)
	if err != nil {
		return err
	}
	// Read back and search for each change. An apply is not verified by the
	// absence of an error; it is verified by finding every edit on disk.
	bad := 0
	for _, v := range apply.Verify(w.Root, edits) {
		if !v.OK {
			bad++
			fmt.Fprintf(os.Stderr, "  [%d] VERIFY %s: %s\n", v.Index, v.Edit.File, v.Reason)
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d edit(s) not found on disk after write; the tree is inconsistent, inspect `git diff`", bad)
	}
	subject := *msg
	if subject == "" {
		subject = fmt.Sprintf("apply: %d edit(s) across %d file(s)", len(edits), len(written))
	}
	sort.Strings(written)
	body := subject + "\n\n"
	for _, e := range edits {
		body += fmt.Sprintf("- %s: %s\n", e.File, firstLine(e.Note, e.Old))
	}
	sha, err := w.Git.Commit(body, written)
	if err != nil {
		return err
	}
	fmt.Printf("\ncommitted %s  (%d file(s))\n", short(sha), len(written))
	return nil
}

func cmdRound(args []string) error {
	if len(args) == 0 || args[0] != "new" {
		return fmt.Errorf("usage: wf round new [-title T]")
	}
	fs := flag.NewFlagSet("round new", flag.ExitOnError)
	title := fs.String("title", "", "what this round is asked to find or close")
	if _, err := parseMixed(fs, args[1:]); err != nil {
		return err
	}
	w, err := open()
	if err != nil {
		return err
	}
	n := w.M.Round + 1
	dir := w.RoundDir(n)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f := filepath.Join(dir, "findings.md")
	if _, err := os.Stat(f); err == nil {
		return fmt.Errorf("%s already exists", f)
	}
	t := *title
	if t == "" {
		t = "(untitled)"
	}
	tmpl := fmt.Sprintf(`# Round %d — %s

Opened at %s.

Every BLOCKER below must be closed by a commit in this workspace before the
round is frozen, and the next round's first job is to check that claim.

| # | level | finding | closed by |
|---|-------|---------|-----------|
| 1 | BLOCKER |  |  |
`, n, t, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(f, []byte(tmpl), 0o644); err != nil {
		return err
	}
	w.M.Round = n
	if err := w.Save(); err != nil {
		return err
	}
	rel, _ := filepath.Rel(w.Root, f)
	sha, err := w.Git.Commit(fmt.Sprintf("round %d: open — %s", n, t), []string{rel, ws.ManifestName})
	if err != nil {
		return err
	}
	fmt.Printf("round %d opened, %s\ncommitted %s\n", n, rel, short(sha))
	return nil
}

// freezeManifest is what replaces hand-copied `<sha8>-artifact` files: one
// record, covering every artifact at once, bound to the commit that produced
// them. Verifying a launch becomes "the tree is clean at tag gate-rN" rather
// than a per-file hash comparison an operator can forget to run on one file.
type freezeManifest struct {
	Round     int               `json:"round"`
	Commit    string            `json:"commit"`
	Target    string            `json:"target"`
	TargetRef string            `json:"targetRef"`
	Frozen    string            `json:"frozen"`
	Artifacts map[string]string `json:"artifacts"` // path -> sha256
}

func cmdFreeze(args []string) error {
	fs := flag.NewFlagSet("freeze", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	w, err := open()
	if err != nil {
		return err
	}
	if w.M.Round == 0 {
		return fmt.Errorf("no round is open; run `wf round new` first")
	}
	dirty, err := w.Git.Dirty("artifacts")
	if err != nil {
		return err
	}
	if len(dirty) > 0 {
		for _, d := range dirty {
			fmt.Fprintln(os.Stderr, "  "+d)
		}
		return fmt.Errorf("artifacts are dirty; commit them with `wf apply` before freezing")
	}
	arts, err := w.ArtifactList()
	if err != nil {
		return err
	}
	if len(arts) == 0 {
		return fmt.Errorf("no artifacts to freeze")
	}
	head, err := w.Git.Head()
	if err != nil {
		return err
	}
	fm := freezeManifest{
		Round: w.M.Round, Commit: head, Target: w.M.Target,
		Frozen: time.Now().UTC().Format(time.RFC3339), Artifacts: map[string]string{},
	}
	// The target's HEAD is recorded too. A frozen gate that cites line numbers
	// is only meaningful against one revision of the repository it reviews, and
	// without this the citations silently rot the moment the target moves.
	fm.TargetRef = targetHead(w.M.Target)
	for _, a := range arts {
		b, err := os.ReadFile(filepath.Join(w.Root, a))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		fm.Artifacts[a] = hex.EncodeToString(sum[:])
	}
	out, err := json.MarshalIndent(fm, "", "  ")
	if err != nil {
		return err
	}
	dir := w.RoundDir(w.M.Round)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	p := filepath.Join(dir, "freeze.json")
	if err := os.WriteFile(p, append(out, '\n'), 0o644); err != nil {
		return err
	}
	rel, _ := filepath.Rel(w.Root, p)
	sha, err := w.Git.Commit(fmt.Sprintf("round %d: freeze %d artifact(s)", w.M.Round, len(arts)), []string{rel})
	if err != nil {
		return err
	}
	tag := ws.RoundTag(w.M.Round)
	if w.Git.TagExists(tag) {
		return fmt.Errorf("tag %s already exists; a frozen round is not re-frozen, open a new one", tag)
	}
	if err := w.Git.Tag(tag, fmt.Sprintf("gate round %d frozen at %s", w.M.Round, short(sha))); err != nil {
		return err
	}
	fmt.Printf("frozen round %d\n  commit %s\n  tag    %s\n  target %s\n", w.M.Round, short(sha), tag, fm.TargetRef)
	for _, a := range arts {
		fmt.Printf("  %s  %s\n", fm.Artifacts[a][:12], a)
	}
	return nil
}

func targetHead(dir string) string {
	sha, err := gitx.Repo{Dir: dir}.Head()
	if err != nil {
		return "(unknown)"
	}
	return short(sha)
}

// cmdVerify answers the only question that matters immediately before a launch:
// is what is about to run byte-for-byte what the reviewers read?
//
// It exists because the obvious substitute does not work. A clean working tree
// proves nothing here: artifacts are changed through `wf apply`, which commits
// as it writes, so an edit made after the gate closed leaves the tree exactly
// as clean as one made before it. The check has to compare against the frozen
// record, and it has to notice an artifact that was ADDED after the freeze —
// a manifest that covered three files and a set that now holds four is the
// shape of a launch nobody reviewed.
func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	round := fs.Int("round", 0, "round to verify against (default the newest frozen one)")
	if _, err := parseMixed(fs, args); err != nil {
		return err
	}
	w, err := open()
	if err != nil {
		return err
	}
	n := *round
	if n == 0 {
		for r := w.M.Round; r >= 1; r-- {
			if _, err := os.Stat(filepath.Join(w.RoundDir(r), "freeze.json")); err == nil {
				n = r
				break
			}
		}
	}
	if n == 0 {
		return fmt.Errorf("no frozen round to verify against; run `wf freeze` first")
	}
	b, err := os.ReadFile(filepath.Join(w.RoundDir(n), "freeze.json"))
	if err != nil {
		return err
	}
	var fm freezeManifest
	if err := json.Unmarshal(b, &fm); err != nil {
		return err
	}
	fmt.Printf("## verify round %d\n", n)
	bad := 0
	fail := func(format string, a ...any) {
		bad++
		fmt.Printf("  MISMATCH "+format+"\n", a...)
	}

	dirty, err := w.Git.Dirty(".")
	if err != nil {
		return err
	}
	for _, d := range dirty {
		fail("uncommitted: %s", d)
	}

	tag := ws.RoundTag(n)
	head, err := w.Git.Head()
	if err != nil {
		return err
	}
	tagged, err := w.Git.Run("rev-parse", tag+"^{commit}")
	if err != nil {
		fail("tag %s does not exist", tag)
	} else if tagged != head {
		// HEAD moving past the tag is NOT a mismatch by itself. Recording a
		// round's findings, opening the next round and writing its scaffold all
		// commit without touching an artifact, and a check that failed on those
		// would push an operator to skip the bookkeeping the tool exists for.
		// The authoritative comparison is the artifact hashes below; the distance
		// is printed because it is the number that says whether to re-freeze.
		ahead, _ := w.Git.Run("rev-list", "--count", tag+"..HEAD")
		fmt.Printf("  note: HEAD is %s, %s commit(s) past tag %s (%s)\n", short(head), ahead, tag, short(tagged))
	}

	arts, err := w.ArtifactList()
	if err != nil {
		return err
	}
	onDisk := map[string]bool{}
	for _, a := range arts {
		onDisk[a] = true
		want, known := fm.Artifacts[a]
		if !known {
			fail("%s is not in the manifest (added after the freeze)", a)
			continue
		}
		body, err := os.ReadFile(filepath.Join(w.Root, a))
		if err != nil {
			fail("%s: %v", a, err)
			continue
		}
		sum := sha256.Sum256(body)
		if got := hex.EncodeToString(sum[:]); got != want {
			fail("%s: sha256 %s, frozen %s", a, got[:12], want[:12])
		}
	}
	for a := range fm.Artifacts {
		if !onDisk[a] {
			fail("%s is in the manifest but gone from the tree", a)
		}
	}

	// The target's revision is part of the frozen state, not context around it:
	// every line number the artifacts cite is a claim about one revision of the
	// repository under review, and a target that moved silently invalidates all
	// of them.
	if now := targetHead(w.M.Target); now != fm.TargetRef {
		fail("target is at %s, frozen against %s", now, fm.TargetRef)
	}

	if bad > 0 {
		fmt.Printf("\n%d mismatch(es): what would run is NOT what was reviewed. Do not launch.\n", bad)
		return errFindings
	}
	fmt.Printf("  tag %s, %d artifact(s), target %s — clean\n", tag, len(arts), fm.TargetRef)
	return nil
}

func cmdDelta(args []string) error {
	fs := flag.NewFlagSet("delta", flag.ExitOnError)
	from := fs.String("from", "", "base ref (default the previously frozen round)")
	to := fs.String("to", "HEAD", "target ref")
	if err := fs.Parse(args); err != nil {
		return err
	}
	w, err := open()
	if err != nil {
		return err
	}
	base := *from
	if base == "" {
		for n := w.M.Round; n >= 1; n-- {
			if t := ws.RoundTag(n); w.Git.TagExists(t) {
				base = t
				break
			}
		}
	}
	if base == "" {
		return fmt.Errorf("no frozen round to diff from; pass -from")
	}
	fmt.Printf("# delta %s..%s\n\n", base, *to)
	// The findings the round was asked to close come first. A delta reviewer
	// whose first job is to check a closure claim needs the claim in front of
	// the diff, not in another file.
	if b, err := os.ReadFile(filepath.Join(w.RoundDir(w.M.Round), "findings.md")); err == nil {
		fmt.Printf("## findings this round must close\n\n%s\n", strings.TrimRight(string(b), "\n"))
	}
	d, err := w.Git.Diff(base, *to, "artifacts")
	if err != nil {
		return err
	}
	fmt.Printf("## artifact diff\n\n%s\n", d)
	return nil
}

func cmdCheck(args []string) error {
	which := "all"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		which, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	show := fs.Bool("show", false, "cite: print the cited lines")
	reqAnchor := fs.Bool("require-anchor", false, "cite: fail on a citation written without #anchor")
	thr := fs.Float64("threshold", 0.82, "dup: similarity threshold for a near match")
	minWords := fs.Int("min-words", 8, "dup: shortest statement compared")
	if _, err := parseMixed(fs, args); err != nil {
		return err
	}
	w, err := open()
	if err != nil {
		return err
	}
	arts, err := w.ArtifactList()
	if err != nil {
		return err
	}
	if len(arts) == 0 {
		return fmt.Errorf("no artifacts under %s", w.Artifacts())
	}
	fail := false
	if which == "all" || which == "cite" {
		bad, err := runCite(w, arts, *show, *reqAnchor)
		if err != nil {
			return err
		}
		fail = fail || bad
	}
	if which == "all" || which == "dup" {
		bad, err := runDup(w, arts, *thr, *minWords)
		if err != nil {
			return err
		}
		fail = fail || bad
	}
	if which != "all" && which != "cite" && which != "dup" {
		return fmt.Errorf("unknown check %q (cite, dup)", which)
	}
	if fail {
		return errFindings
	}
	return nil
}

func runCite(w *ws.Workspace, arts []string, show, requireAnchor bool) (bool, error) {
	cits, err := check.ScanCitations(w.Root, arts)
	if err != nil {
		return false, err
	}
	ix, err := check.NewIndex(w.M.Target)
	if err != nil {
		return false, err
	}
	findings, err := check.VerifyCitations(w.M.Target, ix, cits)
	if err != nil {
		return false, err
	}
	// Anchor coverage is printed unconditionally, because the check's real limit
	// has to be visible. Range validation catches a citation that points past the
	// end of a file; it cannot catch one that points four lines above the thing it
	// claims to cite, and off-by-a-few is the shape these citations actually take.
	// Only an `#anchor` catches that, and only where an author wrote one — so the
	// number of citations WITHOUT one is the number this check did not really
	// verify, and printing it keeps a green run from reading as coverage.
	anchored := 0
	for _, c := range cits {
		if c.Anchor != "" {
			anchored++
		}
	}
	fmt.Printf("## cite — %d citation(s), %d anchored (%d unverified beyond range), %d finding(s)\n",
		len(cits), anchored, len(cits)-anchored, len(findings))
	for _, f := range findings {
		fmt.Println("  " + f.String())
	}
	if requireAnchor {
		for _, c := range cits {
			if c.Anchor == "" {
				fmt.Printf("  %s:%d: error: citation without #anchor  [%s]\n", c.Artifact, c.AtLine, c.Raw)
				findings = append(findings, check.Finding{Citation: c})
			}
		}
	}
	if show {
		for _, c := range cits {
			fmt.Printf("  %s:%d  %s\n%s", c.Artifact, c.AtLine, c.Raw, check.Show(w.M.Target, ix, c))
		}
	}
	return len(findings) > 0, nil
}

func runDup(w *ws.Workspace, arts []string, thr float64, minWords int) (bool, error) {
	sts, err := check.CollectStatements(w.Root, arts, minWords)
	if err != nil {
		return false, err
	}
	groups, err := check.FindDuplicates(w.Root, sts, thr)
	if err != nil {
		return false, err
	}
	divergent := 0
	for _, g := range groups {
		if g.Divergent {
			divergent++
		}
	}
	fmt.Printf("## dup — %d statement(s), %d cross-file group(s), %d divergent\n", len(sts), len(groups), divergent)
	for _, g := range groups {
		flag := ""
		if g.Divergent {
			flag = "  DIVERGENT (copies last touched by different commits)"
		}
		fmt.Printf("  sim %.2f%s\n", g.Similarity, flag)
		for _, s := range g.Sites {
			fmt.Printf("    %s:%d  [%s] %s\n", s.File, s.Line, s.Commit, trunc(s.Text, 110))
		}
	}
	// Only a divergent group fails the check. Two copies committed together are
	// how a rule is SUPPOSED to look when it legitimately lives in two places;
	// failing on those would make the check noise and it would get turned off.
	return divergent > 0, nil
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func trunc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func firstLine(note, fallback string) string {
	s := note
	if s == "" {
		s = fallback
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return trunc(strings.TrimSpace(s), 90)
}
