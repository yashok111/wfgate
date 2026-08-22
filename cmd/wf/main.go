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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
  wf apply -f <edits>             all-or-nothing batch (JSON array or === text blocks), then commit
  wf add <file>...                bring a newly authored artifact under the workspace and commit it
  wf round new [-title T] [-kind full|delta]  open the next round, scaffold its findings file
  wf round record <file>          fill the open round's findings.md from a file, validate closures, commit
  wf freeze                       hash the artifacts, commit, tag gate-rN
  wf verify [-round N]            the pre-launch check: the tree is exactly what was frozen
  wf delta [-from REF] [-to REF] [-save]  the delta brief: findings, closure checklist, diff
  wf check [cite|dup|drift|rules] deterministic checks over the artifacts
  wf stats                        per-round findings, closures and batches, from the ledger and git
  wf version                      the stamped build version of this binary

Exit codes: 0 clean, 1 findings or failure, 2 usage.
`

// version is stamped by the Makefile via -ldflags; "(devel)" is a binary built
// outside make — including every `go test` and a bare `go build`.
//
// It exists because of a measured stumble, written down the day it
// happened: after an edit, a run against the STALE installed binary looks
// exactly like a run against the new one, and nothing in the frozen record
// could say which produced it. The version goes into freeze.json for the same
// reason the target's HEAD does — it is part of what "this round was verified"
// was verified WITH.
var version = "(devel)"

func toolID() string { return "wf " + version }

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
	case "add":
		err = cmdAdd(os.Args[2:])
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
	case "stats":
		err = cmdStats(os.Args[2:])
	case "version", "-version", "--version":
		fmt.Println(toolID())
		return
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
	fmt.Printf("workspace %s\n  root   %s\n  target %s\n  round  %d\n  head   %s\n  tool   %s\n",
		w.M.Name, w.Root, w.M.Target, w.M.Round, short(head), toolID())
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
	// Two batch formats, detected by the first byte: a JSON array, or the
	// plain-text block format (see apply.ParseText) that needs no escaping at
	// all — assembling the JSON was the measured bottleneck of the first live
	// gate, three layers of quoting deep.
	var edits []apply.Edit
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '[' {
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&edits); err != nil {
			return fmt.Errorf("edits: %w", err)
		}
	} else {
		if edits, err = apply.ParseText(raw); err != nil {
			return fmt.Errorf("edits: %w", err)
		}
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

// cmdAdd brings an existing file into the workspace as an artifact.
//
// It exists because `wf apply` cannot make the FIRST commit of an artifact: every
// edit must match text that already exists, so an empty `old` is rejected. Without
// this command the only way to seed a workspace is the hand `git commit` the
// workflow otherwise forbids, and `wf freeze` — which refuses while anything is
// uncommitted — is unreachable on the first pass. A rule with one undocumented
// exception is a rule an operator stops believing.
func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	as := fs.String("as", "", "name to store the file under (default its basename)")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("usage: wf add <file>... [-as name]")
	}
	if *as != "" && len(pos) != 1 {
		return fmt.Errorf("-as names one file, got %d", len(pos))
	}
	w, err := open()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(w.Artifacts(), 0o755); err != nil {
		return err
	}
	var added []string
	for _, src := range pos {
		name := filepath.Base(src)
		if *as != "" {
			name = *as
		}
		dst := filepath.Join(w.Artifacts(), name)
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if _, err := os.Stat(dst); err == nil {
			// The refusal guards exactly one thing: overwriting a REVIEWED
			// artifact, which is `wf apply`'s job to change. A file that exists
			// but is untracked is the natural first draft written in place —
			// refusing it forced the hand `git commit` this command exists to
			// remove, three times on the first live gate — so it is adopted:
			// committed as it lies, never overwritten with different bytes.
			rel, _ := filepath.Rel(w.Root, dst)
			if tracked, _ := w.Git.Run("ls-files", "--", rel); tracked != "" {
				return fmt.Errorf("%s is already a reviewed artifact; change it with `wf apply`, not `wf add`", name)
			}
			onDisk, err := os.ReadFile(dst)
			if err != nil {
				return err
			}
			if abs, _ := filepath.Abs(src); abs != dst && !bytes.Equal(onDisk, b) {
				return fmt.Errorf("%s exists untracked and differs from %s; resolve by hand — adding would silently pick one of two drafts", name, src)
			}
			added = append(added, rel)
			fmt.Printf("  added %s (%d bytes, already in place)\n", rel, len(onDisk))
			continue
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return err
		}
		rel, _ := filepath.Rel(w.Root, dst)
		added = append(added, rel)
		fmt.Printf("  added %s (%d bytes)\n", rel, len(b))
	}
	sha, err := w.Git.Commit(fmt.Sprintf("add %d artifact(s)", len(added)), added)
	if err != nil {
		return err
	}
	fmt.Printf("committed %s\n", short(sha))
	return nil
}

func cmdRound(args []string) error {
	if len(args) > 0 && args[0] == "record" {
		return cmdRoundRecord(args[1:])
	}
	if len(args) == 0 || args[0] != "new" {
		return fmt.Errorf("usage: wf round new [-title T] | wf round record <file>")
	}
	fs := flag.NewFlagSet("round new", flag.ExitOnError)
	title := fs.String("title", "", "what this round is asked to find or close")
	kind := fs.String("kind", "full", "full: re-reads the whole set; delta: reviews the diff only; tool: no reviewer dispatched")
	if _, err := parseMixed(fs, args[1:]); err != nil {
		return err
	}
	// The kind is recorded because the audit needs what a round did NOT look
	// at. On the first live gate seven of ten rounds were delta rounds, the
	// scope lived only in prose titles, and twice a round reviewed less than a
	// reader of its findings would assume — with no record of it anywhere.
	// `tool` exists because P2e recorded two reviewer-less rounds (wfcheck/
	// wfdry findings, and a launch death) as ordinary rounds — and the stop
	// rule then read a blocker descent of 15→13→4→1→1 where the REVIEWER
	// descent, the one the rule is about, was 15→13→4→1.
	scope := map[string]string{
		"full":  "Kind: full — this round re-reads the whole artifact set.",
		"delta": "Kind: delta — this round reviews the delta only; state below what was NOT re-read.",
		"tool":  "Kind: tool — findings from static/dynamic tooling or the launch itself; no reviewer was dispatched.",
	}[*kind]
	if scope == "" {
		return fmt.Errorf("-kind %q: full, delta or tool", *kind)
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
%s

Every BLOCKER below must be closed by a commit in this workspace before the
round is frozen, and the next round's first job is to check that claim.

| # | level | finding | closed by |
|---|-------|---------|-----------|
| 1 | BLOCKER |  |  |
`, n, t, time.Now().UTC().Format(time.RFC3339), scope)
	if err := os.WriteFile(f, []byte(tmpl), 0o644); err != nil {
		return err
	}
	w.M.Round = n
	if err := w.Save(); err != nil {
		return err
	}
	rel, _ := filepath.Rel(w.Root, f)
	sha, err := w.Git.Commit(fmt.Sprintf("round %d: open (%s) — %s", n, *kind, t), []string{rel, ws.ManifestName})
	if err != nil {
		return err
	}
	fmt.Printf("round %d opened (%s), %s\ncommitted %s\n", n, *kind, rel, short(sha))
	return nil
}

// cmdRoundRecord fills the open round's findings file from an authored document.
//
// The scaffold `wf round new` writes is a form, and an empty form briefs nobody:
// what a round FOUND is the half of the delta a reviewer checks first. Filling
// it needs a command, because the alternative an operator reaches for is the
// hand `git commit` the workflow forbids everywhere else — and a rule with one
// undocumented exception is a rule that stops being followed.
func cmdRoundRecord(args []string) error {
	fs := flag.NewFlagSet("round record", flag.ExitOnError)
	round := fs.Int("round", 0, "round to record into (default the open one)")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: wf round record <file> [-round N]")
	}
	w, err := open()
	if err != nil {
		return err
	}
	n := *round
	if n == 0 {
		n = w.M.Round
	}
	if n == 0 {
		return fmt.Errorf("no round is open; run `wf round new` first")
	}
	b, err := os.ReadFile(pos[0])
	if err != nil {
		return err
	}
	// Validated BEFORE any byte is written, like apply: a findings file whose
	// closed-by names a commit that does not exist is a lie in the ledger, and
	// a rejected record announces itself where a landed one would brief the
	// next round with fiction. An OPEN blocker is legal here — findings are
	// recorded when filed, closures land later, and freeze is the gate that
	// demands them closed.
	rows := ws.ParseFindings(b)
	byCommit, byProse, open, blockers := 0, 0, 0, 0
	for _, r := range rows {
		if r.Placeholder() {
			continue
		}
		if r.Blocker() {
			blockers++
		}
		shas := r.Shas()
		for _, s := range shas {
			if !w.Git.CommitExists(s) {
				return fmt.Errorf("row %s (line %d): closed by names %s, which is not a commit in this workspace; nothing was recorded", r.Num, r.Line, s)
			}
		}
		switch {
		case len(shas) > 0:
			byCommit++
		case strings.TrimSpace(r.ClosedBy) != "":
			byProse++
		default:
			open++
		}
	}
	dir := w.RoundDir(n)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(dir, "findings.md")
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return err
	}
	rel, _ := filepath.Rel(w.Root, dst)
	sha, err := w.Git.Commit(fmt.Sprintf("round %d: record findings", n), []string{rel})
	if err != nil {
		return err
	}
	fmt.Printf("recorded %s (%d bytes)\n", rel, len(b))
	if len(rows) == 0 {
		// No table means nothing was validated, and saying so is the entire
		// difference between "checked" and "looked green".
		fmt.Printf("  findings: no table recognized — closures not validated\n")
	} else {
		fmt.Printf("  findings: %d row(s), %d blocker(s); closed by commit %d, by prose %d, open %d\n",
			len(rows), blockers, byCommit, byProse, open)
	}
	fmt.Printf("committed %s\n", short(sha))
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
	Tool      string            `json:"tool"` // wf version that wrote this freeze
	Frozen    string            `json:"frozen"`
	Artifacts map[string]string `json:"artifacts"` // path -> sha256
	// The anchor ratchet: a direction, not a threshold. A fixed bar would be
	// arbitrary; what matters is the share not silently falling — an author who
	// stopped anchoring writes citations this gate cannot really verify.
	CiteAnchored int `json:"citeAnchored"`
	CiteTotal    int `json:"citeTotal"`
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
	// The scaffold's own contract, enforced at the moment it matters: every
	// blocker is closed before the round freezes. Freeze is the right gate, not
	// record — findings are recorded when filed, closures land later, and a rule
	// that rejected an honest "still open" at record time would teach operators
	// to write fiction into the ledger. Placeholder rows (empty finding text)
	// are a form, not a claim, so the untouched scaffold freezes fine.
	//
	// TWO rounds are checked, not one. In the prescribed flow a round is frozen
	// right after it is opened, so the round being frozen always carries a blank
	// scaffold — a check on it alone passes vacuously forever, which is this
	// project's named worst failure. The round whose closures this freeze
	// publishes is the one being CLOSED (the previous round): its findings are
	// what `wf delta` will brief the next reviewer with, so its blockers are the
	// ones that must be closed by now. The open round is checked too, for the
	// operator who records into it before freezing.
	prose := 0
	for _, rn := range []int{w.M.Round - 1, w.M.Round} {
		if rn < 1 {
			continue
		}
		fb, err := os.ReadFile(filepath.Join(w.RoundDir(rn), "findings.md"))
		if err != nil {
			continue
		}
		open := 0
		for _, r := range ws.ParseFindings(fb) {
			if r.Placeholder() {
				continue
			}
			for _, sha := range r.Shas() {
				if !w.Git.CommitExists(sha) {
					return fmt.Errorf("round %d findings row %s: closed by names %s, which is not a commit in this workspace", rn, r.Num, sha)
				}
			}
			switch {
			case len(r.Shas()) > 0:
			case strings.TrimSpace(r.ClosedBy) != "":
				prose++
			case r.Blocker():
				open++
				fmt.Fprintf(os.Stderr, "  [%s] BLOCKER open: %s\n", r.Num, trunc(r.Text, 90))
			}
		}
		if open > 0 {
			return fmt.Errorf("round %d: %d blocker(s) without closed by; record closures with `wf round record` before freezing", rn, open)
		}
	}
	head, err := w.Git.Head()
	if err != nil {
		return err
	}
	fm := freezeManifest{
		Round: w.M.Round, Commit: head, Target: w.M.Target, Tool: toolID(),
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
	cits, err := check.ScanCitations(w.Root, arts)
	if err != nil {
		return err
	}
	fm.CiteTotal = len(cits)
	for _, c := range cits {
		if c.Anchor != "" {
			fm.CiteAnchored++
		}
	}
	// The previous freeze is the ratchet's baseline. Compared by cross-product
	// so a share is never computed over zero.
	var prev freezeManifest
	for r := w.M.Round - 1; r >= 1; r-- {
		if pb, err := os.ReadFile(filepath.Join(w.RoundDir(r), "freeze.json")); err == nil {
			if json.Unmarshal(pb, &prev) == nil {
				prev.Round = r
			}
			break
		}
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
	fmt.Printf("frozen round %d\n  commit %s\n  tag    %s\n  target %s\n  tool   %s\n", w.M.Round, short(sha), tag, fm.TargetRef, fm.Tool)
	fmt.Printf("  cites  %d anchored / %d citation(s)\n", fm.CiteAnchored, fm.CiteTotal)
	for _, a := range arts {
		fmt.Printf("  %s  %s\n", fm.Artifacts[a][:12], a)
	}
	if prose > 0 {
		fmt.Printf("  note: %d closure(s) not naming a commit — verify by hand\n", prose)
	}
	if prev.CiteTotal > 0 && fm.CiteTotal > 0 &&
		fm.CiteAnchored*prev.CiteTotal < prev.CiteAnchored*fm.CiteTotal {
		fmt.Printf("  note: anchored %d/%d, down from %d/%d (round %d) — the share this gate can really verify fell\n",
			fm.CiteAnchored, fm.CiteTotal, prev.CiteAnchored, prev.CiteTotal, prev.Round)
	}
	return nil
}

// newestFrozen returns the newest round with a freeze.json, 0 if none. Both
// verify and drift default to it: the newest frozen record is the one a launch
// would run against.
func newestFrozen(w *ws.Workspace) int {
	for r := w.M.Round; r >= 1; r-- {
		if _, err := os.Stat(filepath.Join(w.RoundDir(r), "freeze.json")); err == nil {
			return r
		}
	}
	return 0
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
		n = newestFrozen(w)
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
	// Provenance, not a mismatch: a rebuilt binary legitimately verifies an
	// older freeze. What the note kills is the silent version: a stale
	// installed binary whose run is indistinguishable from the new one's.
	if fm.Tool != "" && fm.Tool != toolID() {
		fmt.Printf("  note: frozen by %s, verifying with %s\n", fm.Tool, toolID())
	}
	bad := 0
	fail := func(format string, a ...any) {
		bad++
		fmt.Printf("  MISMATCH "+format+"\n", a...)
	}

	dirty, err := w.Git.Dirty(".")
	if err != nil {
		return err
	}
	// Bookkeeping dirt is tracked apart from artifact dirt: both refuse the
	// launch, but "a stray file in rounds/" and "an artifact differs from the
	// freeze" deserve different reactions, and the verdict below says which
	// one the operator has.
	bookkeeping := 0
	for _, d := range dirty {
		fail("uncommitted: %s", d)
		p := d
		if i := strings.LastIndexByte(d, ' '); i >= 0 {
			p = d[i+1:]
		}
		if !strings.HasPrefix(p, "artifacts/") {
			bookkeeping++
		}
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
		if bad == bookkeeping {
			fmt.Printf("  note: the artifacts match the freeze — every mismatch above is uncommitted round bookkeeping. Commit it; do not re-freeze.\n")
		}
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
	save := fs.Bool("save", false, "write the brief to rounds/r<open>/brief.md and commit it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	w, err := open()
	if err != nil {
		return err
	}
	// -save exists because the brief IS round bookkeeping: on the first live
	// gate the operator saved it by hand as an untracked rounds/r7/delta.txt —
	// the very stray file verify then had to learn to name. If the brief is
	// wanted as a file, the tool writes and commits it, so the class dies at
	// its source.
	var out io.Writer = os.Stdout
	var buf bytes.Buffer
	if *save {
		if w.M.Round == 0 {
			return fmt.Errorf("no round is open to save the brief into")
		}
		out = &buf
	}
	// The base is the round BEING CLOSED, which is the one before the open round —
	// not simply the newest tag. By the time a delta reviewer is briefed, the
	// current round has itself been frozen, so "newest tag" is the round about to
	// be reviewed and the diff against it is empty. An empty diff looks exactly
	// like a round that changed nothing.
	base, baseRound := *from, 0
	if base == "" {
		for n := w.M.Round - 1; n >= 1; n-- {
			if t := ws.RoundTag(n); w.Git.TagExists(t) {
				base, baseRound = t, n
				break
			}
		}
		if base == "" {
			return fmt.Errorf("no earlier frozen round to diff from; pass -from")
		}
	} else {
		baseRound = ws.RoundOfTag(base)
	}
	fprintf(out, "# delta %s..%s\n\n", base, *to)
	// The findings the round was asked to close come first, and they are the
	// findings of the BASE round — the open round's file is the blank scaffold
	// `wf round new` just wrote, and briefing a reviewer with it briefs nobody.
	if baseRound > 0 {
		p := filepath.Join(w.RoundDir(baseRound), "findings.md")
		if b, err := os.ReadFile(p); err == nil {
			fprintf(out, "## findings round %d filed, which this delta must close\n\n%s\n",
				baseRound, strings.TrimRight(string(b), "\n"))
			printClosures(out, w, baseRound, ws.ParseFindings(b))
		} else {
			fprintf(out, "## findings\n\n(none recorded in %s — record what round %d found there before briefing a reviewer)\n\n",
				p, baseRound)
		}
	}
	d, err := w.Git.Diff(base, *to, "artifacts")
	if err != nil {
		return err
	}
	fprintf(out, "## artifact diff\n\n%s\n", d)
	if *save {
		dir := w.RoundDir(w.M.Round)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		p := filepath.Join(dir, "brief.md")
		if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
			return err
		}
		rel, _ := filepath.Rel(w.Root, p)
		sha, err := w.Git.Commit(fmt.Sprintf("round %d: save delta brief", w.M.Round), []string{rel})
		if errors.Is(err, gitx.ErrNothingStaged) {
			fmt.Printf("saved %s (%d bytes) — unchanged since the last save\n", rel, buf.Len())
			return nil
		}
		if err != nil {
			return err
		}
		fmt.Printf("saved %s (%d bytes)\ncommitted %s\n", rel, buf.Len(), short(sha))
	}
	return nil
}

// printClosures renders the base round's closure claims as a checklist: each
// claimed commit resolved to its subject and files. It makes the delta
// reviewer's first job — "check that claim" is the scaffold's own words —
// START mechanical: claim → commit → files is looked up, and only "does that
// commit actually close the finding" is left, with the artifact diff below as
// the evidence. Nothing here fails the command: a briefing that refused to
// print would brief nobody.
func printClosures(out io.Writer, w *ws.Workspace, round int, rows []ws.FindingRow) {
	printed := false
	head := func() {
		if !printed {
			fprintf(out, "\n## closures round %d claims\n\n", round)
			printed = true
		}
	}
	for _, r := range rows {
		if r.Placeholder() {
			continue
		}
		head()
		shas := r.Shas()
		switch {
		case len(shas) > 0:
			for _, s := range shas {
				subj, files, err := w.Git.CommitBrief(s)
				if err != nil {
					fprintf(out, "  [%s] %s closed by %s — NOT a commit in this workspace\n", r.Num, r.Level, s)
					continue
				}
				fprintf(out, "  [%s] %s closed by %s %q — %s\n", r.Num, r.Level, short(s), subj, strings.Join(files, ", "))
			}
		case strings.TrimSpace(r.ClosedBy) != "":
			fprintf(out, "  [%s] %s closed by %q — not a commit, verify by hand\n", r.Num, r.Level, r.ClosedBy)
		default:
			fprintf(out, "  [%s] %s — OPEN, nothing claims to close it\n", r.Num, r.Level)
		}
	}
	if len(rows) == 0 {
		fprintf(out, "\n## closures round %d claims\n\n  (no findings table recognized — closures not validated)\n", round)
	}
	fprintf(out, "\n")
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
	round := fs.Int("round", 0, "drift: frozen round to measure from (default the newest)")
	since := fs.String("since", "", "dup: show only groups with a copy touched after this ref")
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
	// drift is not part of "all": it is measured from a freeze, and "all" must
	// keep working on a workspace that has not frozen anything yet.
	if which == "drift" {
		return runDrift(w, arts, *round)
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
		bad, err := runDup(w, arts, *thr, *minWords, *since)
		if err != nil {
			return err
		}
		fail = fail || bad
	}
	if which == "all" || which == "rules" {
		bad, err := runRules(w, arts)
		if err != nil {
			return err
		}
		fail = fail || bad
	}
	if which != "all" && which != "cite" && which != "dup" && which != "rules" {
		return fmt.Errorf("unknown check %q (cite, dup, drift, rules)", which)
	}
	if fail {
		return errFindings
	}
	return nil
}

// runDrift intersects the target's movement since a freeze with the cited
// ranges. It exists because verify's "target is at X, frozen against Y" opens a
// question it cannot answer — WHICH citations did the move invalidate — and the
// unanchored majority get no other mechanical signal: both their lines keep
// existing, so the range check in cite stays green forever.
func runDrift(w *ws.Workspace, arts []string, round int) error {
	n := round
	if n == 0 {
		n = newestFrozen(w)
	}
	if n == 0 {
		return fmt.Errorf("no frozen round; drift is measured from a freeze, run `wf freeze` first")
	}
	b, err := os.ReadFile(filepath.Join(w.RoundDir(n), "freeze.json"))
	if err != nil {
		return err
	}
	var fm freezeManifest
	if err := json.Unmarshal(b, &fm); err != nil {
		return err
	}
	if fm.TargetRef == "" || fm.TargetRef == "(unknown)" {
		return fmt.Errorf("round %d froze without a resolvable target ref; drift has no baseline", n)
	}
	now := targetHead(w.M.Target)
	if now == fm.TargetRef {
		fmt.Printf("## drift — target still at %s — nothing to re-read\n", now)
		return nil
	}
	cits, err := check.ScanCitations(w.Root, arts)
	if err != nil {
		return err
	}
	ds, clean, err := check.FindDrift(w.M.Target, fm.TargetRef, cits)
	if err != nil {
		return err
	}
	kinds := map[string]int{}
	for _, d := range ds {
		kinds[d.Kind]++
	}
	fmt.Printf("## drift — %s..%s: %d citation(s), %d edited, %d shifted, %d gone, %d unresolved, %d clean\n",
		fm.TargetRef, now, len(cits), kinds["edited"], kinds["shifted"], kinds["gone"], kinds["unresolved"], clean)
	for _, d := range ds {
		fmt.Println("  " + d.String())
	}
	if len(ds) > 0 {
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
	// Extensionless references are the check's own blind spot made visible: a
	// `Makefile:90` is not recognised as a citation — the extension rule is
	// what keeps prose ratios and dotted hosts out — so it is absent from every
	// count above. Unverified without being reported as unverified is this
	// project's named worst failure, hence the note.
	extless, err := check.ExtensionlessRefs(w.Root, arts, ix)
	if err != nil {
		return false, err
	}
	anchored, badAnchors := 0, 0
	for _, c := range cits {
		if c.Anchor != "" {
			anchored++
		}
		if c.BadAnchor != "" {
			badAnchors++
		}
	}
	hdr := fmt.Sprintf("## cite — %d citation(s), %d anchored (%d unverified beyond range), %d finding(s)",
		len(cits), anchored, len(cits)-anchored, len(findings))
	if badAnchors > 0 {
		hdr += fmt.Sprintf(", %d anchor(s) not ASCII", badAnchors)
	}
	fmt.Println(hdr)
	if len(extless) > 0 {
		fmt.Printf("  note: %d citation-like reference(s) to extensionless tracked file(s) — NOT verified:\n", len(extless))
		for _, c := range extless {
			fmt.Printf("    %s:%d  [%s]\n", c.Artifact, c.AtLine, c.Raw)
		}
	}
	for _, f := range findings {
		fmt.Println("  " + f.String())
	}
	if requireAnchor {
		for _, c := range cits {
			// A non-ASCII anchor already carries its own error above; flagging
			// it as "without #anchor" too would state a falsehood about a
			// citation whose author did write one.
			if c.Anchor == "" && c.BadAnchor == "" {
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

func runDup(w *ws.Workspace, arts []string, thr float64, minWords int, since string) (bool, error) {
	sts, err := check.CollectStatements(w.Root, arts, minWords)
	if err != nil {
		return false, err
	}
	groups, err := check.FindDuplicates(w.Root, sts, thr)
	if err != nil {
		return false, err
	}
	// -since narrows the OUTPUT, never the threshold. On a derived artifact
	// (a context document that deliberately restates the decisions) every
	// restated sentence satisfies both DIVERGENT conditions by construction —
	// measured: 21-25 flagged groups per one real contradiction. A divergence
	// that survived earlier freezes was seen and left standing; what a round
	// needs is the groups IT touched. The hidden count is printed, because a
	// filter that hides silently turns focus into blindness.
	hidden := 0
	if since != "" {
		out, err := w.Git.Run("rev-list", since+"..HEAD")
		if err != nil {
			return false, err
		}
		recent := map[string]bool{}
		for _, sha := range strings.Split(out, "\n") {
			if len(sha) >= 8 {
				recent[sha[:8]] = true
			}
		}
		kept := groups[:0]
		for _, g := range groups {
			touched := false
			for _, s := range g.Sites {
				if recent[s.Commit] {
					touched = true
					break
				}
			}
			if touched {
				kept = append(kept, g)
			} else {
				hidden++
			}
		}
		groups = kept
	}
	divergent := 0
	for _, g := range groups {
		if g.Divergent {
			divergent++
		}
	}
	nums := check.FindNumericDrift(sts)
	fmt.Printf("## dup — %d statement(s), %d cross-file group(s), %d divergent, %d numeric\n", len(sts), len(groups), divergent, len(nums))
	if hidden > 0 {
		fmt.Printf("  note: %d group(s) predate %s, not shown\n", hidden, since)
	}
	// Numeric conflicts print regardless of -since: a count contradiction is a
	// contradiction NOW, whatever its history.
	for _, nc := range nums {
		fmt.Printf("  NUMERIC (copies agree except the count)\n")
		for _, s := range nc.Sites {
			fmt.Printf("    %s:%d  %s\n", s.File, s.Line, trunc(s.Text, 110))
		}
	}
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
	// Only a divergent or numeric group fails the check. Two copies committed
	// together are how a rule is SUPPOSED to look when it legitimately lives in
	// two places; failing on those would make the check noise and it would get
	// turned off.
	return divergent > 0 || len(nums) > 0, nil
}

// runRules compares every site of each `[RN]` rule marker. Zero markers is a
// corpus that opted out, and the population line says so — dup's structural
// blind spot (copies contradicting in DIFFERENT words: eight on the first live
// gate, all found by reviewers) is only closed where an author marked the rule.
func runRules(w *ws.Workspace, arts []string) (bool, error) {
	sites, markers, err := check.CollectRuleSites(w.Root, arts)
	if err != nil {
		return false, err
	}
	conflicts := check.RuleConflicts(sites)
	fmt.Printf("## rules — %d marker(s), %d id(s), %d conflict(s)\n", markers, len(sites), len(conflicts))
	for _, c := range conflicts {
		fmt.Printf("  %s: sites disagree\n", c.ID)
		for _, s := range c.Sites {
			fmt.Printf("    %s:%d  %s\n", s.File, s.Line, trunc(s.Text, 110))
		}
	}
	return len(conflicts) > 0, nil
}

// cmdStats derives the numbers a close/continue decision needs from what the
// workspace already records — the findings ledger and git — instead of the
// operator's own awk. Measured need: hand-counting on the first live gate
// returned 99/27 against a true 98/26, because the scaffold placeholder of the
// open round was counted as a finding; the ledger parser knows better.
func cmdStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	w, err := open()
	if err != nil {
		return err
	}
	if w.M.Round == 0 {
		return fmt.Errorf("no rounds yet")
	}
	fmt.Printf("## stats — workspace %s, %d round(s)\n", w.M.Name, w.M.Round)
	kinds := roundKinds(w)
	totalF, totalB, totalFI := 0, 0, 0
	revF, revB, toolF, toolB, toolRounds := 0, 0, 0, 0, 0
	for n := 1; n <= w.M.Round; n++ {
		findings, blockers, byCommit, byProse, open, fixInduced := 0, 0, 0, 0, 0, 0
		if b, err := os.ReadFile(filepath.Join(w.RoundDir(n), "findings.md")); err == nil {
			for _, r := range ws.ParseFindings(b) {
				if r.Placeholder() {
					continue
				}
				findings++
				if r.Blocker() {
					blockers++
				}
				if r.FixInduced() {
					fixInduced++
				}
				switch {
				case len(r.Shas()) > 0:
					byCommit++
				case strings.TrimSpace(r.ClosedBy) != "":
					byProse++
				default:
					open++
				}
			}
		}
		totalF += findings
		totalB += blockers
		totalFI += fixInduced
		if kinds[n] == "tool" {
			toolRounds++
			toolF += findings
			toolB += blockers
		} else {
			revF += findings
			revB += blockers
		}
		tag := ws.RoundTag(n)
		state := tag
		if !w.Git.TagExists(tag) {
			state, tag = "open", "HEAD"
		}
		if k := kinds[n]; k != "" {
			state += ", " + k
		}
		// Batches are counted by commit subject over the round's tag window.
		// The window is chronology, not attribution: a batch closing round
		// N's findings lands after gate-rN and is counted where it happened.
		rng := tag
		if prev := ws.RoundTag(n - 1); n > 1 && w.Git.TagExists(prev) {
			rng = prev + ".." + tag
		}
		batches := 0
		if subjects, err := w.Git.Run("log", "--format=%s", rng); err == nil && subjects != "" {
			for _, s := range strings.Split(subjects, "\n") {
				if strings.HasPrefix(s, "apply") {
					batches++
				}
			}
		}
		fi := ""
		if fixInduced > 0 {
			fi = fmt.Sprintf("; fix-induced %d", fixInduced)
		}
		fmt.Printf("  round %d [%s] — %d finding(s), %d blocker(s); closed: %d commit, %d prose, %d open; %d apply batch(es)%s\n",
			n, state, findings, blockers, byCommit, byProse, open, batches, fi)
	}
	fmt.Printf("  total — %d finding(s), %d blocker(s)\n", totalF, totalB)
	// A tooling round restarting the stop rule's clock is the P2e distortion:
	// the rule reads REVIEWER evidence, and a round nobody reviewed is not it.
	if toolRounds > 0 {
		fmt.Printf("  reviewer rounds — %d finding(s), %d blocker(s); tool rounds — %d finding(s), %d blocker(s)\n",
			revF, revB, toolF, toolB)
		fmt.Printf("  note: the stop rule reads the REVIEWER descent — a tooling round's blocker does not restart it\n")
	}
	// The marker is opt-in, so its absence must read as "unmeasured", never as
	// "no self-inflicted findings" — the two are different claims and only the
	// author of a finding can make the second.
	if totalFI > 0 {
		fmt.Printf("  fix-induced — %d of %d finding(s); the share climbs toward 100%% when the gate is exhausted\n", totalFI, totalF)
	} else {
		fmt.Printf("  fix-induced — no [fix-induced] markers; the self-inflicted share was not measured\n")
	}
	return nil
}

// roundKinds derives each round's kind from its opening commit subject
// ("round N: open (kind) — title"). Derived, not stored: the manifest is small
// on purpose, and `wf round record` overwrites the scaffold that carries the
// kind line — the commit subject is the copy nothing rewrites.
var roundOpenRe = regexp.MustCompile(`^round (\d+): open \((full|delta|tool)\)`)

func roundKinds(w *ws.Workspace) map[int]string {
	out := map[int]string{}
	subjects, err := w.Git.Run("log", "--format=%s")
	if err != nil {
		return out
	}
	for _, s := range strings.Split(subjects, "\n") {
		if m := roundOpenRe.FindStringSubmatch(s); m != nil {
			n, _ := strconv.Atoi(m[1])
			if _, seen := out[n]; !seen {
				out[n] = m[2]
			}
		}
	}
	return out
}

// fprintf writes the delta brief to stdout or to the -save buffer; neither can
// meaningfully fail, and checking each call would bury the brief's structure.
func fprintf(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
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
