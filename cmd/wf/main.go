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

	wfgate "git.sumka.site/yakov/wfgate"
	"git.sumka.site/yakov/wfgate/internal/apply"
	"git.sumka.site/yakov/wfgate/internal/check"
	"git.sumka.site/yakov/wfgate/internal/cli"
	"git.sumka.site/yakov/wfgate/internal/gitx"
	"git.sumka.site/yakov/wfgate/internal/ws"
)

const usage = `wf — gate harness

  wf init <name> -target <repo>   create a workspace (its own git repo)
  wf status                       manifest, round, dirty artifacts
  wf apply -f <edits>             all-or-nothing batch (JSON array or === text blocks), then commit
  wf add <file>...                bring a newly authored artifact under the workspace and commit it
  wf round new [-title T] [-kind full|delta|tool|run]  open the next round, scaffold its findings file
  wf round record <file>          fill the open round's findings.md from a file, validate closures, commit
  wf round lens <prompt> -name N -model M -asks Q   bank a reviewer vector against the round (no argument: print the roster)
  wf freeze                       hash the artifacts, commit, tag gate-rN
  wf verify [-round N]            the pre-launch check: the tree is exactly what was frozen
  wf delta [-from REF] [-to REF] [-save]  the delta brief: findings, closure checklist, diff
  wf check [cite|dup|drift|rules|stale|closure] deterministic checks over the artifacts
                                  stale -since REF: sentences still using vocabulary a fix retired
                                  closure [-round N]: does each closing commit touch what the finding names
  wf stats [-time]                per-round findings, closures and batches, from the ledger and git
  wf new-script <name>            emit the orchestration-script harness: wrapper, schemas, gate, ledger
  wf close [-archive DIR]         bank the gate's own record — history, stats, findings, a feedback scaffold
  wf prune [-yes]                 delete artifacts/, rounds/ and .git once the record is banked; not reversible
  wf docs [name]                  print the embedded operating manual (guide, checks, routing, ...)
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

// errUsage is a wrong invocation, which exits 2 — distinct from a command that
// ran and refused, which exits 1.
var errUsage = errors.New("usage")

// run is the ONE dispatch. It used to be a switch in main plus a hand-kept copy
// in the test harness, on the argument that a copy of nine cases was cheaper
// than making the real one testable; the copy then went stale the first time a
// command was added, and every test of it failed with `unknown command`, which
// is this project's most-repeated defect class wearing a different hat.
func run(args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "init":
		return cmdInit(args[1:])
	case "status":
		return cmdStatus(args[1:])
	case "apply":
		return cmdApply(args[1:])
	case "add":
		return cmdAdd(args[1:])
	case "round":
		return cmdRound(args[1:])
	case "freeze":
		return cmdFreeze(args[1:])
	case "verify":
		return cmdVerify(args[1:])
	case "delta":
		return cmdDelta(args[1:])
	case "check":
		return cmdCheck(args[1:])
	case "stats":
		return cmdStats(args[1:])
	case "new-script":
		return cmdNewScript(args[1:])
	case "close":
		return cmdClose(args[1:])
	case "prune":
		return cmdPrune(args[1:])
	case "docs":
		return cmdDocs(args[1:])
	case "version", "-version", "--version":
		fmt.Println(toolID())
		return nil
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	}
	return fmt.Errorf("unknown command %q: %w", args[0], errUsage)
}

func main() {
	err := run(os.Args[1:])
	switch {
	case err == nil:
		return
	case errors.Is(err, errUsage):
		if !errors.Is(err, errFindings) && err != errUsage {
			fmt.Fprintf(os.Stderr, "%v\n\n%s", err, usage)
		} else {
			fmt.Fprint(os.Stderr, usage)
		}
		os.Exit(2)
	case errors.Is(err, errFindings):
		os.Exit(1)
	default:
		fmt.Fprintln(os.Stderr, "wf:", err)
		os.Exit(1)
	}
}

// errFindings separates "the check ran and reported problems" from "the tool
// broke". A caller scripting the gate needs the difference: one means read the
// report, the other means the run never happened.
var errFindings = errors.New("findings")

// parseMixed is internal/cli.ParseMixed; the alias keeps the call sites short.
var parseMixed = cli.ParseMixed

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
	// The gate-checks reference is written into the workspace BEFORE the
	// initial commit sweeps it in: the version of the rules that will brief
	// this gate's reviewers is pinned with the gate, the way the target's
	// HEAD is pinned by a freeze. Generated from the binary, never hand-edited
	// — a snapshot, not a divergent copy.
	if checks, err := wfgate.Docs.ReadFile("skills/workflow-gate/references/gate-checks.md"); err == nil {
		stamped := append([]byte(fmt.Sprintf("<!-- written by %s at init; regenerate with `wf docs checks`, do not edit -->\n", toolID())), checks...)
		if err := os.WriteFile(filepath.Join(root, "gate-checks.md"), stamped, 0o644); err != nil {
			return err
		}
	}
	w, err := ws.Create(root, name, *target, time.Now())
	if err != nil {
		return err
	}
	head, _ := w.Git.Head()
	fmt.Printf("workspace %s\n  root   %s\n  target %s\n  head   %s\n", name, w.Root, w.M.Target, short(head))
	fmt.Printf("  gate-checks.md written — hand it to every reviewer\n")
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
		fmt.Println("    " + d.String())
	}
	return nil
}

func cmdApply(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ExitOnError)
	file := fs.String("f", "", "edits JSON file, or - for stdin (required)")
	msg := fs.String("m", "", "commit subject (default derived from the batch)")
	dry := fs.Bool("dry-run", false, "plan and report, write nothing")
	rule := fs.String("rule", "", "the [RN] rule this batch changes; the note then names the OTHER sites carrying it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("usage: wf apply -f edits.json [-m msg] [-rule RN] [--dry-run]")
	}
	ruleID, err := normaliseRuleID(*rule)
	if err != nil {
		return err
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
		// Decode stops at the end of the FIRST value and the rest is discarded
		// in silence, so a file holding two concatenated arrays applied half the
		// batch and reported success — a partial batch presented as landed, one
		// layer above the failure this whole command exists to prevent.
		if _, err := dec.Token(); err != io.EOF {
			return fmt.Errorf("edits: trailing data after the batch array; a batch is ONE array, not several")
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
		fmt.Printf("  note: the cost estimate is not printed on a dry run — it counts the lines git sees changed, which needs the write\n")
		touched := make([]string, 0, len(bufs))
		for rel := range bufs {
			touched = append(touched, rel)
		}
		if err := printRuleSpread(w, ruleID, touched); err != nil {
			return err
		}
		return nil
	}
	written, err := apply.Write(w.Root, bufs)
	if err != nil {
		return err
	}
	// Read back and compare every written file with the buffer the plan
	// produced. An apply is not verified by the absence of an error; it is
	// verified by reading back what landed.
	bad := 0
	for _, v := range apply.Verify(w.Root, bufs) {
		if !v.OK {
			bad++
			fmt.Fprintf(os.Stderr, "  VERIFY %s: %s\n", v.File, v.Reason)
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d file(s) differ from the planned batch after write; the tree is inconsistent, inspect `git diff`", bad)
	}
	// The population, printed. A read-back nobody can see in the output is a
	// read-back whose absence nobody notices either — deleting the loop above
	// left every bar green.
	fmt.Printf("  verified %d file(s) byte-for-byte against the planned batch\n", len(bufs))
	// The estimate is computed from what GIT sees changed, not from the edit
	// count and not from the declared old/new sizes: the history it is compared
	// against comes from --numstat, and a ratio whose two halves count different
	// things is worse than no ratio. Measured: a P2e window of two edits changed
	// 933 lines, 931 of them in one commit the subject-only meter never saw.
	hist, authoringSkipped := batchHistory(w)
	if note, warn := batchNote(len(edits), worktreeChangedLines(w, written), hist, authoringSkipped); warn {
		fmt.Println(note)
	}
	// Named, not merely remembered. The prediction beside it prices CHANGED
	// LINES; what actually predicted the next round across three script rounds
	// of mocker A3 was whether the batch changed a RULE with consumers
	// elsewhere, and the sentence saying so did more work all gate than the
	// number it was attached to.
	if err := printRuleSpread(w, ruleID, written); err != nil {
		return err
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
	// The batch's size, recorded where NOTHING the operator writes can erase it.
	// It used to live in the default subject alone, and `-m` replaces the
	// subject: P2f applied all 27 of its batches with a written subject, so
	// `wf stats` printed "0 apply batch(es)" on every one of thirteen rounds and
	// the cost meter — the tool's own headline planning number — had no
	// population at all. A trailer is derivation from git like the subject was,
	// only from the half a message cannot take over.
	body += fmt.Sprintf("\n%s %d edit(s) in %d file(s)\n", applyTrailer, len(edits), len(written))
	sha, err := w.Git.Commit(body, written)
	if err != nil {
		return err
	}
	fmt.Printf("\ncommitted %s  (%d file(s))\n", short(sha), len(written))
	return nil
}

// normaliseRuleID accepts what an operator will actually type — `R6`, `r6` or
// `6` — and rejects anything else by NAME rather than sweeping it into "no rule
// declared", which would be a silent opt-out of the check the flag exists for.
func normaliseRuleID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	t := strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	t = strings.TrimPrefix(strings.TrimPrefix(t, "R"), "r")
	if t == "" || strings.TrimLeft(t, "0123456789") != "" {
		return "", fmt.Errorf("-rule %q: a rule id is RN — the marker `[R6]`, `R6` or `6`", s)
	}
	return "R" + t, nil
}

// printRuleSpread names the sites of the declared rule that this batch did NOT
// write. `wf apply`'s own note has advised the sweep since the cost meter
// existed — "if this batch changed a RULE, its consumers are NOT in this diff"
// — and it named nothing, so the operator had to go and look. Measured on
// mocker A3: six of round 10's twelve blockers were one decision repaired in
// the document and left standing in the prompt restating it.
//
// A file the batch wrote is marked rather than dropped: a document holding
// three sites of one rule is exactly where the copy gets left behind, and the
// operator must see them together.
func printRuleSpread(w *ws.Workspace, ruleID string, touched []string) error {
	if ruleID == "" {
		return nil
	}
	arts, err := w.ArtifactList()
	if err != nil {
		return err
	}
	sites, _, notes, err := check.CollectRuleSites(w.Root, arts)
	if err != nil {
		return err
	}
	in := map[string]bool{}
	for _, t := range touched {
		in[filepath.ToSlash(t)] = true
	}
	ss := sites[ruleID]
	// A declared rule that no artifact marks is the loudest outcome here, and
	// it is not an error: the marker discipline is the author's, and a batch
	// changing an unmarked rule is exactly the case `wf check rules` cannot
	// reach at all.
	if len(ss) == 0 {
		fmt.Printf("  rule: NO site carries [%s] — this batch declares a rule nothing marks, so neither `wf check rules` nor this note can reach its other copies. Mark the sites that STATE it\n", ruleID)
		return nil
	}
	outside := 0
	for _, st := range ss {
		if !in[filepath.ToSlash(st.File)] {
			outside++
		}
	}
	fmt.Printf("  rule: [%s] is marked at %d site(s), %d of them outside this batch\n", ruleID, len(ss), outside)
	for _, st := range ss {
		mark := ""
		if in[filepath.ToSlash(st.File)] {
			mark = "  ← a file this batch wrote"
		}
		txt, _ := check.Cut(st.Text, 100)
		fmt.Printf("    %s:%d  %s%s\n", st.File, st.Line, txt, mark)
	}
	for _, n := range notes {
		fmt.Printf("  note: %s — %s\n", n.File, n.Text)
	}
	return nil
}

// closedByError says which repository the sha was looked for in, and whether it
// is a commit in the OTHER one. A closure cites the workspace's own commit —
// the fix that closed the finding — and that rule stands: a sha the workspace
// does not have cannot be verified by anything here, and a landed lie briefs
// the next round with fiction. But the round that records a LAUNCH has the
// target's commit in hand and nothing else, and the refusal used to name
// neither repository, so it read as the tool being unable to see a commit that
// obviously exists.
func closedByError(w *ws.Workspace, prefix, sha string) error {
	msg := fmt.Sprintf("%s: closed by names %s, which is not a commit in this GATE WORKSPACE (%s)", prefix, sha, w.Root)
	if w.M.Target != "" && (gitx.Repo{Dir: w.M.Target}).CommitExists(sha) {
		msg += fmt.Sprintf("; it IS a commit in the TARGET repository (%s) — a closure names the workspace commit that landed the fix, so write the target's sha as prose (\"target %s\") and cite the workspace commit here", w.M.Target, sha)
	}
	return fmt.Errorf("%s", msg)
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
		// -as is operator input and was joined raw: `-as ../../target/x.md`
		// wrote into the repository under review — the one tree no command may
		// touch — and left the file there, untracked and invisible to verify.
		// The same refusal apply has, for the same reason.
		dst, rel, err := apply.SafeJoin(w.Root, filepath.Join("artifacts", name))
		if err != nil {
			return fmt.Errorf("-as %s: %w", name, err)
		}
		// Inside the WORKSPACE is not enough: `-as ../notes/x.md` stayed in the
		// workspace and landed a tracked file outside artifacts/, which the
		// freeze manifest never covers and verify never classes — the same hole
		// a dot-named artifact used to open, through another door.
		if rel != "artifacts" && !strings.HasPrefix(rel, "artifacts"+string(filepath.Separator)) {
			return fmt.Errorf("-as %s: an artifact lives under artifacts/, and only what lives there is frozen and verified", name)
		}
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
			if tracked, _ := w.Git.Run("ls-files", "--", rel); tracked != "" {
				return fmt.Errorf("%s is already tracked in this workspace; change it with `wf apply`, not `wf add`", rel)
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
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return err
		}
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
	if len(args) > 0 && args[0] == "lens" {
		return cmdRoundLens(args[1:])
	}
	if len(args) == 0 || args[0] != "new" {
		return fmt.Errorf("usage: wf round new [-title T] | wf round record <file> | wf round lens <prompt> -name N -model M -asks Q")
	}
	fs := flag.NewFlagSet("round new", flag.ExitOnError)
	title := fs.String("title", "", "what this round is asked to find or close")
	kind := fs.String("kind", "full", "full: re-reads the whole set; delta: reviews the diff only; tool: no reviewer dispatched; run: contains a live run of what the gate built")
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
	// `run` exists because a round that EXECUTES what the gate built is not a
	// long round, and every reader of the timing was told it was. Measured on
	// mocker A3: round 13 spans 288.2 minutes against a median of 45.3 because
	// it contained the launch — 38 agents over 4h36m — and `wf stats -time`
	// answered with its own note, "a span far above the median is a break, not
	// work", which is precisely backwards there. The launch is the one event in
	// a gate's life most worth timing, and it had no name.
	//
	// It is a REVIEWER round everywhere else, deliberately: what a run reports
	// is evidence produced by executing, so its zero is a measured zero and its
	// findings belong in the descent the stop rule reads. Only the clock treats
	// it apart.
	scope := map[string]string{
		"full":  "Kind: full — this round re-reads the whole artifact set.",
		"delta": "Kind: delta — this round reviews the delta only; state below what was NOT re-read.",
		"tool":  "Kind: tool — findings from static/dynamic tooling or the launch itself; no reviewer was dispatched.",
		"run":   "Kind: run — this round CONTAINS a live run of what the gate built; its span is the run, not a break.",
	}[*kind]
	if scope == "" {
		return fmt.Errorf("-kind %q: full, delta, tool or run", *kind)
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

A finding's level is decided by what catches it downstream, never by how bad it
reads [R2]. The scale is closed — wf round record refuses anything else:

- **BLOCKER** — no later gate catches it: if it rides, the run produces a wrong
  result nothing refuses. Vacuity starts here: a finding that makes an
  observation, a guard or a tripwire VACUOUS is classified BLOCKER, and rides
  only where this row can name the gate that catches it — the MAJOR test below.
- **MAJOR** — a real defect, and this row NAMES the later gate that catches it.
  Cannot name one? Then it is a BLOCKER.
- **MINOR** — a real defect whose cost, standing forever, is below the round it
  would take to close.
- **NOTE** — not a defect: an observation or a question with no failure attached.

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
	byCommit, byProse, open, blockers, notes := 0, 0, 0, 0, 0
	for _, r := range rows {
		if r.Malformed {
			return fmt.Errorf("line %d is a table row of %d cell(s), not 4 (%s); a row the parser cannot split cannot be checked for an open blocker — escape a literal pipe as \\| — nothing was recorded",
				r.Line, r.Cells, trunc(r.Raw, 60))
		}
		// The scale is closed on the write path and open on every read path.
		// Measured over the six ledgers on disk: one scale, four vocabularies
		// (MAJOR/CONCERN, MINOR/ADVISORY/NIT), because a markdown cell validates
		// nothing — and neither does a script's `enum`, which validates the
		// VALUE and never the LIST: the scripts these six gates produced carry
		// four enums of their own. A level the gate cannot compare across rounds
		// is a level no report can read, and a rejected record announces itself
		// where a landed one briefs the next round with a word only its author
		// understood.
		//
		// Checked BEFORE the placeholder skip, and that order is the whole
		// point: `Placeholder()` is "the text cell is empty", so the first
		// version let `| 1 | CONCERN |  |  |` through as an unfilled form and
		// the closed scale had a hole exactly the width of the scaffold's own
		// row. An empty LEVEL on an empty row is still a form and stays legal.
		if strings.TrimSpace(r.Level) != "" && !r.KnownLevel() {
			return fmt.Errorf("row %s (line %d): level %q is not one of %s; the scale is closed so a round of one gate can be compared with a round of another — nothing was recorded",
				r.Num, r.Line, r.Level, strings.Join(ws.Levels, ", "))
		}
		if r.Placeholder() {
			continue
		}
		if r.Blocker() {
			blockers++
		}
		if r.Note() {
			notes++
		}
		shas := r.Shas()
		for _, s := range shas {
			if !w.Git.CommitExists(s) {
				return fmt.Errorf("%w; nothing was recorded", closedByError(w, fmt.Sprintf("row %s (line %d)", r.Num, r.Line), s))
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
		// NOTE rows are named, never subtracted: the total stays comparable
		// with a corpus that had nowhere to file a non-defect row.
		note := ""
		if notes > 0 {
			note = fmt.Sprintf("; %d NOTE(s) — counted in the total, asserting no failure", notes)
		}
		fmt.Printf("  findings: %d row(s), %d blocker(s); closed by commit %d, by prose %d, open %d%s\n",
			len(rows), blockers, byCommit, byProse, open, note)
	}
	// The roster, asked for where the round's work is recorded. Measured on
	// mocker A3: ELEVEN of thirteen rounds bank no lens — the operator started
	// at round 11 — so for rounds 1-10 the findings are in the ledger and the
	// PROMPTS that produced them are gone, and the claim that gate most wanted
	// to make from its own numbers (tightly-scoped vectors aimed at the
	// previous round's repairs produced its two best rounds) cannot be checked
	// against anything. Recording findings is the step that feels like the
	// work; this is the other half of the same record, and a note is all it is
	// — a refusal here would be a refusal to write down what was found.
	lenses := roundLenses(w, n)
	if validLenses(lenses) == 0 {
		fmt.Printf("  lenses: NONE banked for round %d — the findings are recorded and the prompts that produced them are not. `wf round lens <prompt> -name N -model M -asks Q -round %d`\n", n, n)
	} else if note := classifyAsks(roundKinds(w)[n], lenses).Note(); note != "" {
		// Which QUESTIONS the round put is the half a roster of names cannot
		// answer: measured over the two gates that banked any roster, ten
		// distinct vector names of which one appears in this manual as a vector
		// name. Through the shared classifier, so this line cannot disagree
		// with the roster and `wf stats` about one round again.
		fmt.Printf("  lenses: %d banked — %s\n", validLenses(lenses), note)
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
	// The corpus this round froze, in lines. A gate whose document doubles while
	// the blocker count holds is a design being ELABORATED rather than settled,
	// and nothing else in the workspace shows it: measured on a live decision
	// gate, 332 -> ~1050 lines over five rounds against blockers 13, 17, 10, 16,
	// 11. One number per freeze is the whole feature.
	ArtifactLines int `json:"artifactLines"`
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
			fmt.Fprintln(os.Stderr, "  "+d.String())
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
	// The kind is read from the opening subject: a `tool` round declares in the
	// one place nothing rewrites that no reviewer was dispatched, so its empty
	// ledger is not a false zero — it is the round saying so.
	kinds := roundKinds(w)
	// A round nobody reviewed must not freeze as a round that found nothing.
	// EVERY closed round is checked, not just the previous one: `wf round new`
	// does not require the current round to be frozen, so a round opened past
	// without a freeze was examined by no refusal at all — reproduced end to
	// end, a gate closed carrying an undispatched round 2 that `wf stats` alone
	// named. The OPEN round is exempt: in the prescribed flow it is frozen right
	// after it is opened, so its scaffold is blank by construction.
	if unread := unreadRounds(w, w.M.Round-1, kinds); len(unread) > 0 {
		return fmt.Errorf("round(s) %s: the findings ledger is the untouched scaffold and no lens is recorded, so this freeze would publish a round nobody is recorded as having read as a round that found nothing.\n  If its vectors ran and found nothing, bank them: `wf round lens <prompt> -name N -model M -asks Q -round N`.\n  If they found something, record it: `wf round record <file> -round N`", roundList(unread))
	}
	for _, rn := range []int{w.M.Round - 1, w.M.Round} {
		if rn < 1 {
			continue
		}
		fp := filepath.Join(w.RoundDir(rn), "findings.md")
		fb, err := os.ReadFile(fp)
		if err != nil {
			// A ledger that cannot be READ is not a ledger with nothing in it.
			// The first version skipped the round on any read error, so deleting
			// findings.md let a round carrying open blockers freeze green — the
			// "exits zero having checked nothing" shape this project names as
			// its worst, sitting inside the check written to prevent it.
			return fmt.Errorf("round %d: %v; freeze publishes that round's closures and cannot validate a ledger it cannot read", rn, err)
		}
		rows := ws.ParseFindings(fb)
		if len(rows) == 0 {
			return fmt.Errorf("round %d: no findings table recognised in %s; freeze validates every blocker against that table and would check nothing", rn, fp)
		}
		open := 0
		for _, r := range rows {
			if r.Malformed {
				return fmt.Errorf("round %d: %s line %d is a table row of %d cell(s), not 4 (%s); a row the parser cannot split cannot be checked for an open blocker — escape a literal pipe as \\|",
					rn, fp, r.Line, r.Cells, trunc(r.Raw, 60))
			}
			if r.Placeholder() {
				continue
			}
			for _, sha := range r.Shas() {
				if !w.Git.CommitExists(sha) {
					return closedByError(w, fmt.Sprintf("round %d findings row %s", rn, r.Num), sha)
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
	tref, err := targetHead(w.M.Target)
	if err != nil {
		return fmt.Errorf("%w; a gate whose citations name no revision has no baseline to verify against", err)
	}
	fm.TargetRef = tref
	modified, untracked, err := targetDirt(w.M.Target)
	if err != nil {
		return err
	}
	if len(modified) > 0 {
		for _, c := range modified {
			fmt.Fprintln(os.Stderr, "  "+c.String())
		}
		return fmt.Errorf("the target has %d modified tracked file(s); the citations name lines in %s, so commit or stash them before freezing", len(modified), tref)
	}
	if len(untracked) > 0 {
		fmt.Printf("  note: %d untracked file(s) in the target — not part of %s, and cited by nothing\n", len(untracked), tref)
	}
	for _, a := range arts {
		b, err := os.ReadFile(filepath.Join(w.Root, a))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		fm.Artifacts[a] = hex.EncodeToString(sum[:])
		fm.ArtifactLines += countLines(b)
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
	// Freeze counted the anchors and never asked whether the citations still
	// RESOLVE. Measured on mocker slice A: the gate shipped its own first slice
	// mid-run, the target moved under a document that was long since frozen, and
	// twenty-six citations pointed at lines that had drifted by a few — read as
	// correct by every producer of the next artifact. The operator had the check
	// and ran it only while the cited document was the artifact under review;
	// the tree it cites keeps moving after that.
	//
	// A note, never a refusal — `wf apply`'s split note is the precedent. A
	// round whose whole purpose is re-aiming these citations must still freeze.
	// "A note, never a refusal" has to hold for the check's own failures too. An
	// unreadable target made freeze EXIT — the one command a round cannot skip,
	// refusing over a check the same paragraph calls advisory.
	var citeFindings []check.Finding
	citeNote := ""
	if ix, err := check.NewIndex(w.M.Target); err != nil {
		citeNote = "the target could not be indexed (" + err.Error() + "), so no citation was resolved for this freeze"
	} else if fs, err := check.VerifyCitations(w.M.Target, ix, cits); err != nil {
		citeNote = "the citations could not be resolved (" + err.Error() + ")"
	} else {
		citeFindings = fs
	}
	// A round whose artifacts were edited by hand costs the gate its cost meter,
	// and nothing said a word: on mocker slice A the operator stopped using
	// `wf apply` at round 18 and found out at close, seven rounds and 35
	// findings later — `wf stats` prints "0 apply batch(es)" for each of them
	// and omits their edit and changed-line figures entirely. Every gate
	// discipline held; only the measurement broke, which is exactly the failure
	// nobody notices in time. Freeze knows the round's commit range, so it is
	// the moment the loss is still one round old.
	handEdited, handAttributable, handUnreadable := handEditedArtifactCommits(w)
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
	// The refusal comes FIRST. It used to sit after the write and the commit,
	// so a re-freeze that correctly errored had already replaced
	// rounds/rN/freeze.json with hashes of the CURRENT tree — and `wf verify`
	// reads that file from the worktree, not from the tag. A post-freeze edit
	// plus a refused `wf freeze` therefore turned MISMATCH into "clean": the
	// command that said no had already blessed what it refused.
	tag := ws.RoundTag(w.M.Round)
	if w.Git.TagExists(tag) {
		return fmt.Errorf("tag %s already exists; a frozen round is not re-frozen, open a new one", tag)
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
	if err := w.Git.Tag(tag, fmt.Sprintf("gate round %d frozen at %s", w.M.Round, short(sha))); err != nil {
		return err
	}
	fmt.Printf("frozen round %d\n  commit %s\n  tag    %s\n  target %s\n  tool   %s\n", w.M.Round, short(sha), tag, fm.TargetRef, fm.Tool)
	fmt.Printf("  cites  %d anchored / %d citation(s)\n", fm.CiteAnchored, fm.CiteTotal)
	size := fmt.Sprintf("  size   %d line(s) across %d artifact(s)", fm.ArtifactLines, len(arts))
	// The DELTA, not only the number: growth is the reading, and a reader who
	// has to open the previous freeze.json to get it will not get it.
	if prev.ArtifactLines > 0 {
		size += fmt.Sprintf(" (%+d since round %d)", fm.ArtifactLines-prev.ArtifactLines, prev.Round)
	}
	fmt.Println(size)
	for _, a := range arts {
		fmt.Printf("  %s  %s\n", fm.Artifacts[a][:12], a)
	}
	if prose > 0 {
		fmt.Printf("  note: %d closure(s) not naming a commit — verify by hand\n", prose)
	}
	if citeNote != "" {
		fmt.Printf("  note: %s — this freeze pins artifacts whose citations were NOT checked against the target\n", citeNote)
	}
	if len(citeFindings) > 0 {
		// "do NOT resolve" is the honest headline for the whole set: a citation
		// naming a path that matches two files, or carrying an anchor the
		// grammar refuses, resolves to nothing a reviewer can follow either.
		// Each finding prints its own class on the line below.
		fmt.Printf("  note: %d citation(s) do NOT resolve against %s — reviewers of this freeze will read them as written:\n", len(citeFindings), fm.TargetRef)
		for i, f := range citeFindings {
			if i == 10 {
				fmt.Printf("    ... and %d more — `wf check cite` prints them all\n", len(citeFindings)-10)
				break
			}
			fmt.Println("    " + f.String())
		}
	}
	if handEdited > 0 {
		fmt.Printf("  note: %d commit(s) in this round changed an artifact without `wf apply` — this round contributes NO edit count to `wf stats`, and the batch-size note in `wf apply` has that much less history to read\n", handEdited)
	}
	// The two ways this check can know nothing, each said out loud. A silent
	// zero here reads exactly like a round that used `wf apply` throughout,
	// which is the reading that cost a live gate seven rounds of its cost meter.
	if !handAttributable {
		fmt.Printf("  note: round %d has no `%s` tag, so its commit window has no lower bound and the hand-edit check above did not run — it says nothing about this round either way\n", w.M.Round, ws.RoundTag(w.M.Round-1))
	}
	if handUnreadable > 0 {
		fmt.Printf("  note: %d commit(s) of this round could not be read by git, so the hand-edit count above is a LOWER BOUND\n", handUnreadable)
	}
	if prev.CiteTotal > 0 && fm.CiteTotal > 0 &&
		fm.CiteAnchored*prev.CiteTotal < prev.CiteAnchored*fm.CiteTotal {
		fmt.Printf("  note: anchored %d/%d, down from %d/%d (round %d) — the share this gate can really verify fell\n",
			fm.CiteAnchored, fm.CiteTotal, prev.CiteAnchored, prev.CiteTotal, prev.Round)
	}
	return nil
}

// countLines is the artifact size the freeze records. A file not ending in a
// newline still has a last line, and counting separators alone would report it
// as one short.
func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := bytes.Count(b, []byte("\n"))
	if b[len(b)-1] != '\n' {
		n++
	}
	return n
}

// handEditedArtifactCommits counts the commits of the OPEN round that changed an
// artifact that already existed, and did not come from `wf apply`.
//
// Modified, never added: `wf add` adopts a draft and is not an edit batch, and a
// round whose only artifact commit is an adoption has lost nothing. An apply
// commit is recognised the way `wf stats` recognises one, so the two derivations
// of "was this batch measured" cannot disagree — a second copy of one rule is
// this project's most repeated defect.
//
// It returns the count, whether the window could be attributed to this round at
// all, and how many of its commits git refused to read. The last two used to be
// folded into the count as a zero, which is the accusation the note exists to
// avoid: an unknown printed as a measured zero. A round with no previous tag has
// no lower bound, so its window is every ancestor and a count over it says
// nothing about THIS round.
func handEditedArtifactCommits(w *ws.Workspace) (n int, attributable bool, unreadable int) {
	rng := "HEAD"
	if r := w.M.Round; r > 1 {
		prev := ws.RoundTag(r - 1)
		if !w.Git.TagExists(prev) {
			return 0, false, 0
		}
		rng = prev + "..HEAD"
	}
	for _, c := range commitLog(w, rng) {
		if _, _, isApply := applyCommitEdits(c.Msg); isApply {
			continue
		}
		out, err := w.Git.Run("show", "--name-only", "--diff-filter=M", "--format=", c.SHA, "--", "artifacts")
		if err != nil {
			// A commit git would not show is not a commit that changed no
			// artifact. Counting it as one turns an unreadable window into a
			// clean one.
			unreadable++
			continue
		}
		if strings.TrimSpace(out) == "" {
			continue
		}
		n++
	}
	return n, true, unreadable
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

// targetHead resolves the revision every citation is a claim ABOUT. The error
// is returned rather than folded into the string "(unknown)": that string went
// into the manifest, and verify then compared "(unknown)" with "(unknown)" and
// printed clean — a frozen gate with no baseline at all verified green forever,
// and only started failing once the target came back. `check drift` already
// refused such a manifest by name; freeze and verify now refuse it too.
func targetHead(dir string) (string, error) {
	sha, err := gitx.Repo{Dir: dir}.Head()
	if err != nil {
		return "", fmt.Errorf("target %s: %w", dir, err)
	}
	return short(sha), nil
}

// targetDirt separates the two states a target's working tree can be in that
// the frozen revision does not describe. A MODIFIED tracked file is the one
// that matters: the citations name lines in HEAD, so a tree whose tracked files
// differ from HEAD is not the tree the reviewers will read — measured, a cited
// line rewritten without a commit left HEAD unchanged, so `wf check drift` said
// "nothing to re-read" and `wf verify` said "clean" over it. Untracked files
// are noted, not refused: build output beside a repository under review is
// normal and cites nothing.
func targetDirt(dir string) (modified, untracked []gitx.Change, err error) {
	cs, err := gitx.Repo{Dir: dir}.Dirty(".")
	if err != nil {
		return nil, nil, err
	}
	for _, c := range cs {
		if c.Untracked() {
			untracked = append(untracked, c)
		} else {
			modified = append(modified, c)
		}
	}
	return modified, untracked, nil
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
	rel := filepath.ToSlash(filepath.Join("rounds", "r"+strconv.Itoa(n), "freeze.json"))
	b, err := os.ReadFile(filepath.Join(w.RoundDir(n), "freeze.json"))
	if err != nil {
		return err
	}
	var fm freezeManifest
	if err := json.Unmarshal(b, &fm); err != nil {
		return err
	}
	manifestDrift := false
	fmt.Printf("## verify round %d\n", n)
	// The manifest is read back from the TAG as well. A manifest read only from
	// the worktree is a record the thing it records can edit: rewrite an
	// artifact, rewrite its hash beside it, commit both, and verify compares the
	// new bytes against the new hash and calls it clean.
	frozenManifest, terr := w.Git.Run("show", ws.RoundTag(n)+":"+rel)
	switch {
	case terr != nil:
		fmt.Printf("  note: the frozen manifest is not readable at tag %s (%v) — verifying against the working copy alone\n", ws.RoundTag(n), terr)
	case strings.TrimSpace(frozenManifest) != strings.TrimSpace(string(b)):
		var fmTag freezeManifest
		if json.Unmarshal([]byte(frozenManifest), &fmTag) == nil {
			fm = fmTag
		}
		fmt.Printf("  MISMATCH %s differs from the copy tagged %s; the tagged manifest is the one reviewers read, and it is what the checks below use\n", rel, ws.RoundTag(n))
		manifestDrift = true
	}
	// Provenance, not a mismatch: a rebuilt binary legitimately verifies an
	// older freeze. What the note kills is the silent version: a stale
	// installed binary whose run is indistinguishable from the new one's.
	if fm.Tool != "" && fm.Tool != toolID() {
		fmt.Printf("  note: frozen by %s, verifying with %s\n", fm.Tool, toolID())
	}
	bad := 0
	if manifestDrift {
		bad++
	}
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
		fail("uncommitted: %s", d.String())
		if !strings.HasPrefix(d.Path, "artifacts/") {
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
	if now, err := targetHead(w.M.Target); err != nil {
		fail("%v (frozen against %s)", err, fm.TargetRef)
	} else if now != fm.TargetRef {
		fail("target is at %s, frozen against %s", now, fm.TargetRef)
	} else if modified, untracked, err := targetDirt(w.M.Target); err != nil {
		return err
	} else {
		// HEAD matching is not the whole question. A cited line rewritten
		// WITHOUT a commit leaves HEAD where it was, so the tag, the manifest
		// and the target ref all still agree while the bytes a reviewer opens
		// have changed underneath them.
		for _, c := range modified {
			fail("target is dirty: %s — %s says what %s does not", c.String(), c.Path, fm.TargetRef)
		}
		if len(untracked) > 0 {
			fmt.Printf("  note: %d untracked file(s) in the target, cited by nothing\n", len(untracked))
		}
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
		if r.Malformed {
			head()
			fprintf(out, "- line %d: UNPARSABLE row (%s) — not four cells; nothing can be checked against it\n", r.Line, trunc(r.Raw, 60))
			continue
		}
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
	since := fs.String("since", "", "dup: show only groups with a copy touched after this ref; stale: the fix range to mine (default the newest freeze)")
	// Calibrated on a live gate's real fix range (450 sentences, 1050 lines):
	// at 8 the report was 26 hits carrying "were", "right", "tell", "stays" —
	// English, not vocabulary — and at 3 it is 8 hits over 5 retired tokens,
	// each judgeable in seconds. `dup`'s history decided the direction: a check
	// that paints most of its output is a check nobody runs twice. The dropped
	// population is printed, so raising it is one flag away.
	maxDF := fs.Int("max-df", 3, "stale: a retired token in more than this many sentences is corpus vocabulary, not a consumer")
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
	// stale is not part of "all" for drift's reason: it is measured FROM a
	// reference, and "all" must keep working on a workspace that has frozen
	// nothing yet.
	if which == "stale" {
		bad, err := runStale(w, arts, *since, *minWords, *maxDF)
		if err != nil {
			return err
		}
		if bad {
			return errFindings
		}
		return nil
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
	// closure IS part of "all": the check exists because a whole live round of
	// hand-written reviewer prompts was spent asking it, and a check nobody runs
	// by default is the prompt again with a shorter name.
	if which == "all" || which == "closure" {
		bad, err := runClosure(w, arts, *round)
		if err != nil {
			return err
		}
		fail = fail || bad
	}
	if which != "all" && which != "cite" && which != "dup" && which != "rules" && which != "closure" {
		return fmt.Errorf("unknown check %q (cite, dup, drift, rules, stale, closure)", which)
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
	now, err := targetHead(w.M.Target)
	if err != nil {
		return err
	}
	if now == fm.TargetRef {
		// "The target has not moved" is a claim about HEAD, and HEAD is not the
		// whole tree: a cited line rewritten without a commit leaves this branch
		// printing "nothing to re-read" over exactly the change drift exists to
		// find.
		modified, _, err := targetDirt(w.M.Target)
		if err != nil {
			return err
		}
		if len(modified) > 0 {
			fmt.Printf("## drift — target still at %s, but %d tracked file(s) differ from it:\n", now, len(modified))
			for _, c := range modified {
				fmt.Printf("  %s\n", c.String())
			}
			return fmt.Errorf("the target's working tree is not %s; commit or stash it, then re-run", now)
		}
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

// printProseScope says which artifacts were read as programs rather than as
// documents, and it prints ALWAYS where any of them exist. A script's prompts
// are compared; its code is not, and the difference decides what a clean line
// means. Measured on mocker A3: a decision that moved in the document and did
// not move in the prompt restating it produced six of one round's twelve
// blockers, and the operator did not know a script could be compared at all.
func printProseScope(arts []string, notes []check.Note) {
	var js []string
	for _, a := range arts {
		if check.IsProseJS(a) {
			js = append(js, a)
		}
	}
	if len(js) > 0 {
		fmt.Printf("  scope: %d JS artifact(s) read as PROSE — comments and string literals only (%s); what the engine executes is not compared\n",
			len(js), strings.Join(js, ", "))
	}
	for _, n := range notes {
		fmt.Printf("  note: %s — %s\n", n.File, n.Text)
	}
}

func runDup(w *ws.Workspace, arts []string, thr float64, minWords int, since string) (bool, error) {
	sts, notes, err := check.CollectStatements(w.Root, arts, minWords)
	if err != nil {
		return false, err
	}
	groups, blind, err := check.FindDuplicates(w.Root, sts, thr)
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
	ops, err := check.FindOperatorConflicts(w.Root, sts)
	if err != nil {
		return false, err
	}
	fmt.Printf("## dup — %d statement(s), %d cross-file group(s), %d divergent, %d numeric, %d operator\n",
		len(sts), len(groups), divergent, len(nums), len(ops))
	printProseScope(arts, notes)
	if hidden > 0 {
		fmt.Printf("  note: %d group(s) predate %s, not shown\n", hidden, since)
	}
	// The near-match pass is candidate-filtered by rare tokens, so a statement
	// built entirely from words the corpus uses often has no candidate at all.
	// Measured: the same pair is reported or not depending on how many unrelated
	// statements elsewhere happen to share its words — so the blind population
	// is named rather than left to read as "nothing there".
	if blind > 0 {
		fmt.Printf("  note: %d statement(s) share no rare token — invisible to the near-match pass, exact matches unaffected\n", blind)
	}
	// An operator conflict prints regardless of -since, for the same reason a
	// numeric one does: two copies that cannot both be followed contradict each
	// other NOW, whatever their history.
	for _, oc := range ops {
		fmt.Printf("  OPERATOR (copies agree except the comparison)\n")
		views := divergenceOf(oc.Sites)
		for i, s := range oc.Sites {
			fmt.Printf("    %s:%d  %s\n", s.File, s.Line, views[i])
		}
	}
	// Numeric conflicts print regardless of -since: a count contradiction is a
	// contradiction NOW, whatever its history.
	for _, nc := range nums {
		fmt.Printf("  NUMERIC (copies agree except the count)\n")
		views := divergenceOf(nc.Sites)
		for i, s := range nc.Sites {
			fmt.Printf("    %s:%d  %s\n", s.File, s.Line, views[i])
		}
	}
	for _, g := range groups {
		flag := ""
		if g.Divergent {
			flag = "  DIVERGENT (copies last touched by different commits)"
		}
		fmt.Printf("  sim %.2f%s\n", g.Similarity, flag)
		views := divergenceOf(g.Sites)
		for i, s := range g.Sites {
			fmt.Printf("    %s:%d  [%s] %s\n", s.File, s.Line, s.Commit, views[i])
		}
	}
	// Only a divergent or numeric group fails the check. Two copies committed
	// together are how a rule is SUPPOSED to look when it legitimately lives in
	// two places; failing on those would make the check noise and it would get
	// turned off.
	return divergent > 0 || len(nums) > 0 || len(ops) > 0, nil
}

// runRules compares every site of each `[RN]` rule marker. Zero markers is a
// corpus that opted out, and the population line says so — dup's structural
// blind spot (copies contradicting in DIFFERENT words: eight on the first live
// gate, all found by reviewers) is only closed where an author marked the rule.
func runRules(w *ws.Workspace, arts []string) (bool, error) {
	sites, markers, notes, err := check.CollectRuleSites(w.Root, arts)
	if err != nil {
		return false, err
	}
	conflicts := check.RuleConflicts(sites)
	compared := check.ComparedIDs(sites)
	hdr := fmt.Sprintf("## rules — %d marker(s), %d id(s), %d compared, %d conflict(s)",
		markers, len(sites), compared, len(conflicts))
	// The note below said this and was still missed, on a gate with eighteen
	// markers and eighteen ids: it printed under a line whose other three
	// numbers read like a pass. The headline carries it now — a reader who reads
	// one line of this section must not read "0 conflict(s)" as coverage.
	// The condition is `compared == 0`, not `markers > 0 && compared == 0`. An
	// UNMARKED corpus compares nothing either, and it is the larger population
	// by far: it printed `0 marker(s), 0 id(s), 0 compared, 0 conflict(s)` with
	// no qualifier at all — four numbers that read like a pass, which is the
	// exact line this wave exists to kill.
	if compared == 0 {
		hdr += " — NOT A PASS, nothing was compared"
	}
	fmt.Println(hdr)
	// WHICH question those four numbers answer, printed with them and always.
	// Measured on a live decision gate: `10 marker(s), 5 id(s), 5 compared, 0
	// conflict(s)` was a true statement about copy agreement and was read as a
	// statement about correctness — while [R5]'s two copies agreed with each
	// other perfectly and both had become FALSE against the four bullets of the
	// section that argues for them, because the fix rewrote the argument and not
	// the rule. A reviewer filed it as a blocker over a check that had just
	// printed a clean line.
	fmt.Printf("  scope: this compares the SITES of one [RN] id against each other. It never asks whether a rule still agrees with the section that ARGUES for it — that class is `wf check stale`, and no count here says anything about it\n")
	printProseScope(arts, notes)
	// A marked corpus where every id sits at ONE site reaches nothing, and the
	// bare conflict count then reads as a pass. Measured on a live gate, after
	// the repair below was done the wrong way round.
	if markers > 0 && compared == 0 {
		fmt.Printf("  note: every id has ONE site, so nothing was compared — this is not a pass. " +
			"The check reaches a rule only where the same id marks two or more sites\n")
	}
	for _, c := range conflicts {
		fmt.Printf("  %s: sites disagree\n", c.ID)
		texts := make([]string, len(c.Sites))
		for i, s := range c.Sites {
			texts[i] = s.Text
		}
		// Each site is printed at the DIVERGENCE, not at its opening: the
		// truncated-from-the-start form printed five identical lines over five
		// sites that differed past the cut, which reads as the check being
		// broken rather than as the corpus being wrong.
		views := check.Divergence(texts, 110)
		for i, s := range c.Sites {
			ops := ""
			if s.Ops != "" {
				ops = "  [ops: " + s.Ops + "]"
			}
			fmt.Printf("    %s:%d  %s%s\n", s.File, s.Line, views[i], ops)
		}
	}
	// The cause is named because the wrong repair is cheap and permanent. A
	// live gate reported four ids in conflict, every one of them a sentence
	// that POINTED at the rule ("Because of [R2]", "forced by [R4]", "so [R8]
	// is satisfied by") sitting beside the sentence that stated it. The
	// operator's repair batch carried fourteen edits, six of them marker work,
	// and it left every id with a single site — so the check then compared
	// nothing at all and printed that as a clean line.
	if len(conflicts) > 0 {
		fmt.Printf("  note: a site that REFERS to a rule states nothing, and disagrees with every " +
			"statement of it. Drop the marker from the reference — do not reword the artifact to " +
			"satisfy the check\n")
	}
	return len(conflicts) > 0, nil
}

// runStale reports the consumers a fix left standing. `wf apply` has printed
// the advice since the meter existed — "if this batch changed a RULE, its
// consumers are NOT in this diff, sweep for restatements of the OLD rule now" —
// and no command could act on it. This is that sweep, and it is possible only
// here: the fix range records the exact text each replacement removed.
//
// Measured on a live decision gate (5 rounds, 127 findings): roughly a fifth to
// a quarter of every round's claimed closures were incomplete, and the failure
// was almost always this one — the fix corrected one copy of a rule and left
// another standing, in words no `dup` group and no `[RN]` marker reaches.
func runStale(w *ws.Workspace, arts []string, since string, minWords, maxDF int) (bool, error) {
	if since == "" {
		n := newestFrozen(w)
		if n == 0 {
			return false, fmt.Errorf("nothing is frozen yet, so there is no fix range to mine; pass -since <ref>")
		}
		since = ws.RoundTag(n)
	}
	// --unified=0 and forced prefixes for the same reason `check drift` uses
	// them: the parse must not depend on a host diff config, and the OLD text is
	// wanted without context lines around it.
	raw, err := w.Git.Run("-c", "core.quotePath=false", "diff", "--no-renames", "--no-ext-diff",
		"--unified=0", "--src-prefix=a/", "--dst-prefix=b/", since, "HEAD", "--", "artifacts")
	if err != nil {
		return false, err
	}
	sts, notes, err := check.CollectStatements(w.Root, arts, minWords)
	if err != nil {
		return false, err
	}
	rep := check.FindStale(sts, raw, maxDF)
	fmt.Printf("## stale — %s..HEAD: %d retired token(s), %d sentence(s) still carrying them (%d scanned, %d written by the range)\n",
		since, len(rep.Retired), len(rep.Hits), rep.Scanned, rep.Touched)
	printProseScope(arts, notes)
	// The two populations that decide whether the line above is a measurement,
	// and the distinction between them is the whole point. A range that MINED no
	// vocabulary swept nothing — vacuous, and it says so. A range that mined
	// plenty and finds none of it left in the corpus is a COMPLETE sweep, the
	// best outcome this check has; both leave `Retired` empty, and printing the
	// second as the first told an operator who had reached every consumer that
	// nothing had been checked.
	switch {
	case rep.Mined == 0:
		fmt.Printf("  note: NOT A PASS — this range retired no vocabulary at all, so nothing was swept. A fix that only ADDED text, or one whose every removed word survives elsewhere in the diff, is invisible to this check\n")
	case rep.Swept > 0:
		fmt.Printf("  swept: %d of %d retired token(s) survive NOWHERE in the untouched corpus — that fix reached its consumers\n", rep.Swept, rep.Mined)
	}
	if rep.Common > 0 {
		fmt.Printf("  note: %d retired token(s) are used all over the corpus and were dropped rather than reported — the OUTPUT is narrowed, never the check; lower -max-df (now %d) to see them\n", rep.Common, maxDF)
	}
	if len(rep.Retired) > 0 {
		fmt.Printf("  retired: %s\n", strings.Join(trimList(rep.Retired, 12), ", "))
	}
	for i, h := range rep.Hits {
		if i == 25 {
			fmt.Printf("    ... and %d more\n", len(rep.Hits)-25)
			break
		}
		fmt.Printf("  %s:%d  [%s]  %s\n", h.File, h.Line, strings.Join(h.Tokens, " "), trunc(h.Text, 110))
	}
	if len(rep.Hits) > 0 {
		fmt.Printf("  note: a hit is a sentence the fix did not touch that still uses the words the fix retired. Which of them ARGUES from the superseded rule and which merely shares its vocabulary is judgement — this check reports, it never rewrites\n")
	}
	return len(rep.Hits) > 0, nil
}

// runClosure asks of every claimed closure the question `wf round record` does
// not: the sha exists, but does its diff touch what the finding NAMES.
//
// Measured: on a live decision gate the operator wrote that question into two
// reviewer prompts by hand for three rounds running, and the two lenses
// independently reported 6 and 4, 5 and 4, 6 and 4 closures they judged not
// closed, out of 25, 23 and 21 findings. It was the most valuable half of those
// rounds and it was produced entirely by prose instruction over data git holds.
func runClosure(w *ws.Workspace, arts []string, round int) (bool, error) {
	rounds := []int{}
	if round > 0 {
		// A round that does not exist is not a round with no closures. `-round
		// 999` printed `0 closure(s) claiming a commit, 0 checked, 0 not closed`
		// and exited zero: the report of a check that examined nothing, in the
		// register of one that examined everything.
		if round > w.M.Round {
			return false, fmt.Errorf("round %d does not exist (this workspace has %d); a check over a round that is not there would report zero of everything and exit clean", round, w.M.Round)
		}
		rounds = append(rounds, round)
	} else {
		for n := 1; n <= w.M.Round; n++ {
			rounds = append(rounds, n)
		}
	}
	// Cached per sha: one round's findings routinely name the same closing
	// batch a dozen times.
	touched := map[string]map[string][][2]int{}
	ranges := func(sha string) map[string][][2]int {
		if r, ok := touched[sha]; ok {
			return r
		}
		// `-c` is git's, not the subcommand's, so it goes FIRST — as an option of
		// `show` it is an unknown flag and every closure then reads as "a commit
		// git could not read", which the report duly printed as an absent
		// measurement over a workspace where every commit was fine.
		raw, err := w.Git.Run("-c", "core.quotePath=false", "show", "--no-renames", "--no-ext-diff",
			"--unified=0", "--src-prefix=a/", "--dst-prefix=b/", "--format=", sha)
		var r map[string][][2]int
		if err == nil {
			r = check.TouchedRanges(raw)
		}
		touched[sha] = r
		return r
	}
	claims, checked, unnamed, unreadable, implied, fileOnly := 0, 0, 0, 0, 0, 0
	ambiguous, noLedger := 0, []int{}
	// The artifact as it stood BEFORE a commit, which is the coordinate system
	// that commit's own old-side ranges are in. Keyed by BOTH, because a
	// closure naming two commits has two coordinate systems and comparing one
	// commit's ranges against the other's section numbers is arithmetic over
	// two different documents.
	sections := map[string]map[string][2]int{}
	sectionsAt := func(sha, path string) map[string][2]int {
		key := sha + "\x00" + path
		if m, ok := sections[key]; ok {
			return m
		}
		var m map[string][2]int
		if body, err := w.Git.Run("show", sha+"^:"+path); err == nil {
			m = check.SectionRanges([]byte(body))
		}
		sections[key] = m
		return m
	}
	var bad []string
	for _, n := range rounds {
		b, err := os.ReadFile(filepath.Join(w.RoundDir(n), "findings.md"))
		if err != nil {
			// A ledger that cannot be READ is not a ledger with no closures in
			// it — the distinction `wf stats` already draws, and the one this
			// command was missing.
			noLedger = append(noLedger, n)
			continue
		}
		for _, r := range ws.ParseFindings(b) {
			if r.Malformed || r.Placeholder() || len(r.Shas()) == 0 {
				continue
			}
			claims++
			named, amb := check.PathsNamed(r.Text, arts)
			if len(amb) > 0 {
				// The `check cite` precedent: a path matching two artifacts
				// resolves to neither. Guessing the first would report a closure
				// as landed against a file nobody named.
				ambiguous++
				continue
			}
			if len(named) == 0 && len(arts) == 1 {
				// A one-artifact workspace: the finding names the only document
				// there is, and requiring it to say so out loud would make the
				// check unreachable on exactly the gates that need it most.
				named = map[string][]int{arts[0]: {0}}
				implied++
			}
			if len(named) == 0 {
				// Not a pass and not a failure: a finding that names no artifact
				// cannot be checked this way, and the count is printed so the
				// checked share is visible rather than assumed.
				unnamed++
				continue
			}
			// Every sha is read in ITS OWN coordinates and the claims are ORed:
			// a closure carried by two commits is closed by their combined work,
			// and each commit's line numbers mean something only against its own
			// parent.
			readable := true
			for _, sha := range r.Shas() {
				if ranges(sha) == nil {
					readable = false
				}
			}
			if !readable {
				unreadable++
				continue
			}
			checked++
			// One verdict per ROW. The first version appended a line per failed
			// assertion and printed that count as "closures not closed", so a
			// finding naming two paths could be counted twice and the headline
			// could exceed the number of claims it was drawn from.
			var why []string
			sawSection, sawLine := false, false
			for _, p := range sortedKeys(named) {
				touchedAnywhere := false
				for _, sha := range r.Shas() {
					if _, ok := ranges(sha)[p]; ok {
						touchedAnywhere = true
					}
				}
				if !touchedAnywhere {
					why = append(why, fmt.Sprintf("did not touch %s (it changed %s)", p, fileList(unionOf(r.Shas(), ranges))))
					continue
				}
				// The SECTION the finding cites — the form a real ledger uses.
				// Measured on a live decision gate: 126 of 127 closures named no
				// path and nearly every one named a section.
				//
				// ANY cited section, ANY commit. Demanding every one of them, per
				// commit, flagged 89 of 127 closures there: a finding that
				// mentions a neighbouring section in passing is not a finding
				// about it. `dup` paid for that lesson at 161 groups of 176.
				var resolved []string
				hitSection, anySection := false, false
				for _, sha := range r.Shas() {
					rs := ranges(sha)[p]
					secs := sectionsAt(sha, p)
					for _, id := range check.SectionsNamed(r.Text) {
						rng, ok := secs[id]
						if !ok {
							continue
						}
						anySection = true
						resolved = append(resolved, fmt.Sprintf("%s (%d-%d)", id, rng[0], rng[1]))
						if overlaps(rs, rng) {
							hitSection = true
						}
					}
				}
				if anySection {
					sawSection = true
					if !hitSection {
						why = append(why, fmt.Sprintf("changed %s but NOT any section it cites: %s", p, strings.Join(uniqueStrings(resolved), ", ")))
					}
					continue
				}
				// No section: fall back to the line, where the finding cites one.
				for _, ln := range named[p] {
					if ln == 0 {
						continue
					}
					sawLine = true
					covered := false
					for _, sha := range r.Shas() {
						if check.RangesCover(ranges(sha)[p], ln) {
							covered = true
						}
					}
					if !covered {
						why = append(why, fmt.Sprintf("changed %s but not line %d", p, ln))
					}
				}
			}
			// Counted ONCE per closure, and only when neither the section nor
			// the line half ran: it used to be incremented per path and even
			// where a line HAD been checked, so the report called closures
			// file-only that were not.
			if !sawSection && !sawLine {
				fileOnly++
			}
			if len(why) > 0 {
				bad = append(bad, fmt.Sprintf("round %d [%s] closed by %s, which %s",
					n, r.Num, strings.Join(r.Shas(), " "), strings.Join(why, "; ")))
			}
		}
	}
	fmt.Printf("## closure — %d closure(s) claiming a commit, %d checked against the artifact they name, %d not closed by that commit\n",
		claims, checked, len(bad))
	if claims > 0 && checked == 0 {
		fmt.Printf("  note: NOT A PASS — no closure could be checked. `wf round record` proves the sha EXISTS; this check proves the commit touched what the finding names, and it reaches a finding only where the finding names an artifact path\n")
	}
	if implied > 0 {
		fmt.Printf("  note: %d closure(s) name no path and were checked against %s, the workspace's only artifact\n", implied, arts[0])
	}
	if fileOnly > 0 {
		// The weaker half of the check, and it has to say which half it ran. A
		// commit that touched the artifact SOMEWHERE is a much smaller claim
		// than one that touched the section the finding cites.
		fmt.Printf("  note: %d closure(s) cite neither a section this artifact heads nor a line, so they were checked only at FILE level — a commit that touched the document anywhere passes that. Cite the section id (D9) or the line and the check reaches the claim\n", fileOnly)
	}
	if unnamed > 0 {
		fmt.Printf("  note: %d closure(s) name no artifact path in the finding text and were NOT checked — write the path (and the line) into the finding and they become checkable\n", unnamed)
	}
	if ambiguous > 0 {
		fmt.Printf("  note: %d closure(s) name a path matching more than one artifact and were NOT checked — the same class `wf check cite` reports; write the path from the workspace root\n", ambiguous)
	}
	if unreadable > 0 {
		fmt.Printf("  note: %d closure(s) name a commit git could not read here, so they are ABSENT from the count above rather than clean\n", unreadable)
	}
	if len(noLedger) > 0 {
		fmt.Printf("  note: %d round(s) have no readable findings.md (%s) — their closures are an ABSENT measurement, not a measured zero\n", len(noLedger), roundList(noLedger))
	}
	for _, l := range bad {
		fmt.Println("  " + l)
	}
	if len(bad) > 0 {
		fmt.Printf("  note: a FILE miss is a closure that did not land where the finding said it would. A SECTION or LINE miss is a question to re-ask, not a verdict: the fix may have landed in the section that PRODUCES the defect rather than the one the finding cites, and a later batch in the same round shifts the numbers. Verified by hand on a live gate — 12 of 127, and the first three read that way\n")
	}
	return len(bad) > 0, nil
}

// unionOf is for the REPORT only — "the commits changed these files" — never
// for a verdict: each commit's ranges live in its own parent's coordinates.
func unionOf(shas []string, ranges func(string) map[string][][2]int) map[string][][2]int {
	out := map[string][][2]int{}
	for _, sha := range shas {
		for p, rs := range ranges(sha) {
			out[p] = append(out[p], rs...)
		}
	}
	return out
}

func uniqueStrings(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// overlaps is range-vs-range, where RangesCover is range-vs-line: a fix inside
// a section rarely starts on the heading.
func overlaps(rs [][2]int, r [2]int) bool {
	for _, a := range rs {
		if a[0] <= r[1] && a[1] >= r[0] {
			return true
		}
	}
	return false
}

// trimList caps a printed population and says so, rather than printing a
// hundred tokens nobody reads.
func trimList(xs []string, n int) []string {
	if len(xs) <= n {
		return xs
	}
	return append(append([]string{}, xs[:n]...), fmt.Sprintf("... +%d more", len(xs)-n))
}

func sortedKeys(m map[string][]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func fileList(m map[string][][2]int) string {
	if len(m) == 0 {
		return "nothing under artifacts/"
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(trimList(out, 4), ", ")
}

// cmdStats derives the numbers a close/continue decision needs from what the
// workspace already records — the findings ledger and git — instead of the
// operator's own awk. Measured need: hand-counting on the first live gate
// returned 99/27 against a true 98/26, because the scaffold placeholder of the
// open round was counted as a finding; the ledger parser knows better.
func cmdStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	showTime := fs.Bool("time", false, "per-round wall clock, derived from the workspace's commit times")
	if err := fs.Parse(args); err != nil {
		return err
	}
	w, err := open()
	if err != nil {
		return err
	}
	return statsReport(w, *showTime, os.Stdout)
}

// statsReport writes the report to out. `wf close` banks it verbatim, so there
// is one implementation and not a second that drifts from it — a second copy of
// a derivation is this project's most-repeated defect class.
func statsReport(w *ws.Workspace, showTimeFlag bool, out io.Writer) error {
	if w.M.Round == 0 {
		return fmt.Errorf("no rounds yet")
	}
	_, _ = fmt.Fprintf(out, "## stats — workspace %s, %d round(s)\n", w.M.Name, w.M.Round)
	// Whether the workspace has a population at all is a different question
	// from whether each round's share of it can be attributed. Deriving the
	// first from the per-round totals made a workspace with one unattributable
	// batch report that no apply commit existed, which is a stronger and false
	// claim.
	gateBatches := len(applyBatches(w, "HEAD"))
	// ONE derivation of a round's window, shared with the cost meter.
	windows := roundWindows(w)
	var times map[int]roundTime
	if showTimeFlag {
		times = roundTimes(w)
	}
	kinds := roundKinds(w)
	totalF, totalB, totalFI, totalEdits, totalLines := 0, 0, 0, 0, 0
	unattributed := 0
	// The rounds the two derived numbers cannot see, kept BY NUMBER: "some
	// rounds are missing" is a sentence an operator reads past, "rounds 18-25"
	// is one they act on. Measured on mocker slice A, where both holes opened in
	// the same place — the operator stopped using `wf apply` at round 18, and
	// the whole second script gate then contributed neither an edit count nor a
	// fix-induced marker while the totals above kept reading as measured.
	var handRounds, unmarkedRounds, noLedger []int
	// A round whose ledger parsed and holds nothing but the scaffold's unfilled
	// rows, and a round whose reviewer vectors nobody banked. Both print `0
	// finding(s), 0 blocker(s)` and a bare round line today, which is what a
	// measured zero prints — and a measured zero is a legal close.
	var scaffoldRounds, noRoster, askShort, askUnstated []int
	askMissing := map[int][]string{}
	// The longest run of reviewer rounds carrying exactly ONE blocker, and their
	// texts — see the note where it is printed.
	var oneBlockerRun []int
	var oneBlockerText []string
	var flatRuns [][]int
	var flatTexts [][]string
	revF, revB, toolF, toolB, toolRounds := 0, 0, 0, 0, 0
	unparsable, notes, offScale := 0, 0, 0
	for n := 1; n <= w.M.Round; n++ {
		findings, blockers, byCommit, byProse, open, fixInduced := 0, 0, 0, 0, 0, 0
		soleBlocker := ""
		b, ledgerErr := os.ReadFile(filepath.Join(w.RoundDir(n), "findings.md"))
		if ledgerErr != nil {
			// A round whose ledger cannot be read is not a round with no
			// findings. Both printed `0 finding(s), 0 blocker(s)` and both fed
			// the totals, so a missing file read as a measured zero — the
			// vacuous-pass shape this tool exists to refuse.
			noLedger = append(noLedger, n)
		}
		rows := ws.ParseFindings(b)
		scaffold := ledgerErr == nil && kinds[n] != "tool" && ws.ScaffoldOnly(rows)
		reviewedEmpty := scaffold && validLenses(roundLenses(w, n)) > 0
		scaffoldOnly := scaffold && !reviewedEmpty
		if scaffoldOnly {
			scaffoldRounds = append(scaffoldRounds, n)
		}
		// The roster is what reviewed the round. Absent, every claim about
		// model diversity or brief wording made from this workspace is
		// unfalsifiable — measured on a live gate where the vector's model
		// alone separated the blocker counts by 1.76x and nothing recorded it.
		lenses := roundLenses(w, n)
		if validLenses(lenses) == 0 && kinds[n] != "tool" {
			noRoster = append(noRoster, n)
		} else {
			// Which QUESTIONS a delta round put, separated from whether it
			// stated them at all: a roster banked before `-asks` existed asked
			// something, and reporting it as uncovered would be the same
			// absent-vs-measured-zero collapse this report refuses everywhere
			// else. ONE unstated lens is enough — the first version asked only
			// whether ANY lens stated its questions, so a round with one legacy
			// lens and one new one was reported as measured and short.
			c := classifyAsks(kinds[n], lenses)
			switch {
			case !c.Applicable || c.Valid == 0:
			case c.Unstated > 0:
				askUnstated = append(askUnstated, n)
			case len(c.Missing) > 0:
				askShort = append(askShort, n)
				askMissing[n] = c.Missing
			}
		}
		if ledgerErr == nil {
			for _, r := range rows {
				// A row the parser could not split into four cells is neither a
				// finding nor a form. It is counted apart and named below: a
				// ledger the tool half-read must not be reported as a ledger it
				// read, which is the whole reason the parser returns it at all.
				if r.Malformed {
					unparsable++
					continue
				}
				if r.Placeholder() {
					continue
				}
				findings++
				if r.Blocker() {
					blockers++
					soleBlocker = r.Text
				}
				// Counted, never subtracted. Every banked total in this corpus
				// includes whatever non-defect rows its gate filed under its own
				// name, so removing NOTE from `findings` would move five gates'
				// numbers in silence — the defect this report exists to refuse.
				if r.Note() {
					notes++
				}
				if !r.KnownLevel() {
					offScale++
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
		state := ws.RoundTag(n)
		if !w.Git.TagExists(state) {
			state = "open"
		}
		if k := kinds[n]; k != "" {
			state += ", " + k
		}
		// The window is chronology, not attribution: a batch closing round N's
		// findings lands after gate-rN and is counted where it happened. But it
		// is attributable only when BOTH ends are pinned — below by the previous
		// round's tag, above by its own — and an untagged round that is not the
		// last has neither, so the range degrades to every ancestor. Measured on
		// a live gate: rounds 1, 2 and 3 each printed the workspace total, with
		// ratios of 0.88 and 0.15 fabricated from the same 26 edits. A number
		// that cannot be attributed is omitted, not printed.
		rw := windows[n-1]
		attributable, rng := rw.Attributable, rw.Range
		batches, edits, lines, unknown := 0, 0, 0, 0
		if attributable {
			for _, b := range applyBatches(w, rng) {
				batches++
				edits += b.Edits
				lines += b.Lines
				if !b.Known {
					unknown++
				}
			}
		} else {
			unattributed++
		}
		// An attributable window with zero batches is a round somebody edited by
		// hand — a real population of zero, not an unknown one — and the round
		// still has findings to price. It is the shape that hid seven rounds of
		// a live gate inside a total that read as measured.
		if attributable && batches == 0 && findings > 0 {
			handRounds = append(handRounds, n)
		}
		// The marker is opt-in per FINDING, so a round with none is a round
		// nobody measured. The gate total then silently mixes measured rounds
		// with unmeasured ones, and it reads as a share of the whole.
		if findings > 0 && fixInduced == 0 {
			unmarkedRounds = append(unmarkedRounds, n)
		}
		// The stop rule reads the DESCENT and is right to keep a gate open at one
		// blocker a round. What it cannot say is that four such rounds in a row
		// were the same predicate rewritten — measured on mocker slice A, where
		// rounds 21-24 each closed one real escape from one enumeration floor,
		// because every version of it constrained a property of the REPORT and
		// for every "at least N" there is an N. A run of them is the signature of
		// a guard asking the wrong KIND of question, and only the ROUND SHAPE is
		// mechanical — the reading stays the operator's.
		if kinds[n] != "tool" && blockers == 1 {
			oneBlockerRun = append(oneBlockerRun, n)
			oneBlockerText = append(oneBlockerText, soleBlocker)
		} else if kinds[n] != "tool" {
			// EVERY qualifying run, not the longest one. Keeping only the
			// longest meant keeping the FIRST longest, and the gate this note
			// was derived from has three runs of three: it would have named
			// rounds 5-7 of the decision gate and attached the script gate's
			// enumeration-floor story to them.
			if len(oneBlockerRun) >= 3 {
				flatRuns = append(flatRuns, oneBlockerRun)
				flatTexts = append(flatTexts, oneBlockerText)
			}
			oneBlockerRun, oneBlockerText = nil, nil
		}
		_ = edits
		_ = lines
		// The window's edits precede the window's findings: the fixes land
		// after the previous freeze, the findings come from reviewing this one
		// — so findings/edits on one line IS "what this batch cost", the P2e
		// planning number (~0.25) measured per workspace.
		ed := ""
		switch {
		case !attributable:
			ed = ", apply window not attributable"
		case edits > 0:
			// Both denominators, because the operator controls EDITS and the
			// corpus is predicted by LINES: across the three closed gates a
			// prose fix runs 2.0-4.6 findings per 100 changed lines and a fix
			// that changed a RULE runs 6.3-14.9, a separation the edit count
			// cannot see.
			ed = fmt.Sprintf(", %d edit(s) / %d changed line(s)", edits, lines)
			if unknown > 0 {
				// An unreadable batch is an unknown denominator, and a ratio
				// over a denominator that is short by an unknown amount is not
				// a smaller ratio, it is no ratio.
				ed += fmt.Sprintf(" (%d batch(es) unreadable — no ratio)", unknown)
			}
			if findings > 0 && lines > 0 && unknown == 0 {
				if rw.Authoring {
					// The window that wrote the corpus prices nothing. Printing
					// "377.8 finding(s) per 100 changed line(s)" in the register
					// of a measurement is how the meter came to predict ~355
					// findings for a round that produced 24. WHY there is no
					// ratio is said in the headline above; this says only that
					// there is none, where the number would have stood.
					ed += " (no ratio)"
				} else {
					ed += fmt.Sprintf(" (%.1f finding(s) per 100 changed line(s))",
						100*float64(findings)/float64(lines))
				}
			}
		}
		fi := ""
		if fixInduced > 0 {
			fi = fmt.Sprintf("; fix-induced %d", fixInduced)
		}
		// The corpus each round froze. A document that doubles while the blocker
		// count holds is a design being elaborated rather than settled, and this
		// is the only place the shape is visible: measured 332 -> ~1050 lines
		// over five rounds against blockers 13, 17, 10, 16, 11.
		sz := ""
		if cur, ok := frozenLines(w, n); ok {
			sz = fmt.Sprintf("; artifact %d line(s)", cur)
			for p := n - 1; p >= 1; p-- {
				if prev, ok := frozenLines(w, p); ok {
					sz += fmt.Sprintf(" (%+d)", cur-prev)
					break
				}
			}
		}
		ln := ""
		if len(lenses) > 0 {
			ln = "; lenses: " + lensList(lenses)
		}
		switch {
		case scaffoldOnly:
			// Inline, where the zero is read. A note at the foot of the report
			// is not where an operator decides whether the descent reached zero.
			ln += "; ledger: UNFILLED scaffold, not a measured zero"
		case reviewedEmpty:
			// The other half of the same distinction, said in the same place:
			// this one IS a measured zero, and the banked roster is what makes
			// it one.
			ln += "; ledger: empty, and its lenses are banked — a measured zero"
		}
		tm := ""
		if rt, ok := times[n]; ok {
			tm = fmt.Sprintf("; %.1f min", rt.Span)
			if rt.Split {
				// The first segment is "open to the first fix commit". On a
				// `-kind run` round that window IS the run, and calling it
				// review names the wrong activity in the one round whose
				// minutes are least like a review's.
				head := "review"
				if kinds[n] == "run" {
					head = "run"
				}
				tm += fmt.Sprintf(" (%s %.1f, fix %.1f, tail %.1f)", head, rt.Review, rt.Fix, rt.Tail)
			}
		}
		// BEFORE the counts, because that is where the reading happens.
		// Measured on mocker A3: nine rounds of blocker descent on the decision
		// document — 13, 17, 10, 16, 11, 12, 6, 7, 4 — read exactly like
		// convergence, and under the stop rule one more round with no blocker
		// closes the gate. The script was then added to the workspace, and
		// round 10 produced 38 findings and 12 blockers over an artifact nobody
		// had reviewed at all. The tool KNEW: it printed "(an artifact was
		// ADDED in this window, so there is no ratio)" in a parenthesis after
		// the numbers, and the operator read the numbers. A descent that
		// precedes a first review says nothing about it.
		first := ""
		if rw.Authoring {
			first = "FIRST REVIEW of an artifact ADDED in this window (a descent before it says nothing about it); "
			if kinds[n] == "tool" {
				// A `-kind tool` round dispatched no reviewer by declaration,
				// so "first review" would claim a reading nobody did — the
				// wave's own defect class, committed by the wave. What is true
				// of the WINDOW is still true and still suppresses the ratio.
				first = "an artifact was ADDED in this window and NO reviewer was dispatched — nothing here reviewed it; "
			}
		}
		_, _ = fmt.Fprintf(out, "  round %d [%s] — %s%d finding(s), %d blocker(s); closed: %d commit, %d prose, %d open; %d apply batch(es)%s%s%s%s%s\n",
			n, state, first, findings, blockers, byCommit, byProse, open, batches, ed, sz, fi, ln, tm)
	}
	_, _ = fmt.Fprintf(out, "  total — %d finding(s), %d blocker(s)\n", totalF, totalB)
	if len(oneBlockerRun) >= 3 {
		flatRuns = append(flatRuns, oneBlockerRun)
		flatTexts = append(flatTexts, oneBlockerText)
	}
	for i, run := range flatRuns {
		_, _ = fmt.Fprintf(out, "  note: rounds %s each produced exactly ONE blocker — a flat descent closes a real escape every round and never reaches the question underneath it. Measured: three such rounds were one enumeration floor rewritten, because every version constrained a property of the REPORT and for every \"at least N\" there is an N\n", roundList(run))
		if p := sharedPath(flatTexts[i]); p != "" {
			_, _ = fmt.Fprintf(out, "  note: every blocker in that run cites %s — one region, %d rounds: re-ask what the guard over it is FOR before patching it again\n", p, len(run))
		}
	}
	// Zero over a whole gate is a POPULATION, and it has to say so. P2f applied
	// 27 batches and this meter printed "0 apply batch(es)" thirteen times
	// without a word, so the number that decides the next batch's size looked
	// measured and was not — the operator reconstructed it by counting headers
	// in his own scratch files.
	if gateBatches == 0 {
		_, _ = fmt.Fprintf(out, "  note: no apply commit was found in this workspace — the cost meter has NO population, and the findings/edit numbers above are absent rather than zero. Artifacts changed by hand are invisible to it; a batch applied by `wf apply` records its own size and is counted whatever subject you give it\n")
	}
	// The whole-gate note above fires only when the meter has NO population at
	// all. A gate that used `wf apply` for its first seventeen rounds and stopped
	// has a population, a headline ratio, and seven rounds inside it that the
	// ratio never saw — printed per round as a bare "0 apply batch(es)", which is
	// the same silence with a number in front of it.
	if gateBatches > 0 && len(handRounds) > 0 {
		_, _ = fmt.Fprintf(out, "  note: %d round(s) with findings applied no `wf apply` batch (%s) — their edits and changed lines are absent from every ratio above, which therefore prices only the rounds that used it\n",
			len(handRounds), roundList(handRounds))
	}
	// The gate ends on a REVIEWER round that produces no BLOCKER, and on nothing
	// else [R1] — and this is the one shape in which "produced none" and "was
	// never dispatched" are the same line. Measured: a gate's round 9 was
	// opened, frozen as `gate-r9` and left undispatched, and read as a zero
	// until somebody ran it — at which point it returned four blockers, two of
	// them decisions a scope cut had deleted.
	if len(scaffoldRounds) > 0 {
		_, _ = fmt.Fprintf(out, "  note: %d round(s) carry an UNFILLED findings scaffold (%s) — their 0/0 above is an empty form, not a measured zero, and the stop rule must not read it as one; a round whose vectors ran and found nothing says so with `wf round lens`\n",
			len(scaffoldRounds), roundList(scaffoldRounds))
	}
	if len(noRoster) > 0 {
		_, _ = fmt.Fprintf(out, "  note: %d round(s) record NO lens (%s) — what reviewed them is not in this workspace, so no claim about brief wording or model diversity made from these numbers can be checked against it; bank each vector with `wf round lens <prompt> -name N -model M -asks Q`\n",
			len(noRoster), roundList(noRoster))
	}
	if len(askShort) > 0 {
		// Each round's ACTUAL missing set. Printing `lensDeltaRequired` told a
		// round that asked two of the three it had asked "none of" all three —
		// and this line is what `wf close` banks into close/stats.txt, the one
		// artifact that survives `wf prune`.
		for _, n := range askShort {
			_, _ = fmt.Fprintf(out, "  note: round %d banks a roster that does not ask %s — a delta round asks CLOSURE, FIX-INDUCED and RIPPLE, and may add vectors but never drop those three [R3]\n",
				n, strings.Join(askMissing[n], ", "))
		}
	}
	if len(askUnstated) > 0 {
		_, _ = fmt.Fprintf(out, "  note: %d DELTA round(s) bank a roster that states no questions (%s) — banked before `-asks` existed, so what they covered is UNRECORDED rather than uncovered\n",
			len(askUnstated), roundList(askUnstated))
	}
	if len(noLedger) > 0 {
		_, _ = fmt.Fprintf(out, "  note: %d round(s) have no readable findings.md (%s) — their 0/0 above is an ABSENT measurement, not a measured zero, and the totals do not include them\n", len(noLedger), roundList(noLedger))
	}
	if notes > 0 {
		_, _ = fmt.Fprintf(out, "  note: %d of the findings above are NOTE rows — not defect claims, counted in the totals so they stay comparable with a corpus that had nowhere else to file them\n", notes)
	}
	if offScale > 0 {
		// The measurement that produced the closed scale, run against the
		// workspace reading it: six ledgers, one scale, four vocabularies.
		_, _ = fmt.Fprintf(out, "  note: %d finding(s) carry a level outside %s — recorded before the scale was closed, so a round of this gate cannot be compared level-for-level with a round of another\n",
			offScale, strings.Join(ws.Levels, "/"))
	}
	if unparsable > 0 {
		_, _ = fmt.Fprintf(out, "  note: %d ledger row(s) are not four cells and were counted as nothing — a hand-awk once closed a gate on 99/27 against a true 98/26, and a row the parser cannot read is the same error with a different sign\n", unparsable)
	}
	// A tooling round restarting the stop rule's clock is the P2e distortion:
	// the rule reads REVIEWER evidence, and a round nobody reviewed is not it.
	if toolRounds > 0 {
		_, _ = fmt.Fprintf(out, "  reviewer rounds — %d finding(s), %d blocker(s); tool rounds — %d finding(s), %d blocker(s)\n",
			revF, revB, toolF, toolB)
		_, _ = fmt.Fprintf(out, "  note: the stop rule reads the REVIEWER descent — a tooling round's blocker does not restart it\n")
	}
	// The marker is opt-in, so its absence must read as "unmeasured", never as
	// "no self-inflicted findings" — the two are different claims and only the
	// author of a finding can make the second.
	if totalFI > 0 {
		_, _ = fmt.Fprintf(out, "  fix-induced — %d of %d finding(s); a CONTINUE signal, never a stop signal — it says what these rounds found, not whether the next one will be empty\n", totalFI, totalF)
		// A share computed over rounds where nobody marked is a LOWER BOUND, and
		// its error has a direction: on mocker slice A eight of a script gate's
		// fifteen blockers were induced by the previous round's own fix and none
		// of them were marked, so the ledger read as if the self-inflicted share
		// had tapered off in exactly the rounds where it was everything.
		if len(unmarkedRounds) > 0 {
			_, _ = fmt.Fprintf(out, "  note: %d round(s) with findings carry NO [fix-induced] marker (%s) — the share above is a lower bound over the rounds that were marked, not a measurement of the gate\n",
				len(unmarkedRounds), roundList(unmarkedRounds))
		}
	} else {
		_, _ = fmt.Fprintf(out, "  fix-induced — no [fix-induced] markers; the self-inflicted share was not measured\n")
	}
	// The workspace's own cost meter: what a batch of edits costs in next-round
	// findings, measured here rather than recalled from another gate's anecdote.
	// The gate total is the WHOLE population, not the sum of the attributable
	// windows. Two reasons, and the second is the sharper one: a batch applied
	// after the last round's freeze belongs to no window at all (and `wf close`
	// requires that freeze, so every banked record had the hole), and `wf
	// apply`'s note reads its history HEAD-wide — on a live gate the two
	// derivations of one number disagreed by 60%, which is the second-copy
	// defect this project names as its most frequent.
	totalUnknown := 0
	for _, b := range applyBatches(w, "HEAD") {
		totalEdits += b.Edits
		totalLines += b.Lines
		if !b.Known {
			totalUnknown++
		}
	}
	if tag := ws.RoundTag(w.M.Round); w.Git.TagExists(tag) {
		tailE, tailN := 0, 0
		for _, b := range applyBatches(w, tag+"..HEAD") {
			tailN++
			tailE += b.Edits
		}
		if tailN > 0 {
			_, _ = fmt.Fprintf(out, "  post-gate — %d apply batch(es) after %s, %d edit(s); in the gate total, reviewed by no round\n", tailN, tag, tailE)
		}
	}
	// One of the three sources is INFERRED from the shape of a commit body, and
	// an inferred count must announce itself rather than passing as measured: a
	// hand commit whose body is a single `- path: note` bullet is counted.
	if gateBatches > 0 {
		by := map[string]int{}
		for _, b := range applyBatches(w, "HEAD") {
			by[b.Src]++
		}
		if by["body"] > 0 {
			_, _ = fmt.Fprintf(out, "  note: %d of %d batch(es) were counted by TRAILER, %d by the default SUBJECT and %d INFERRED from the commit body — an inferred one is a commit whose body is entirely `- path: note` lines, which a hand commit can also be\n",
				by["trailer"], gateBatches, by["subject"], by["body"])
		}
	}
	if unattributed > 0 {
		_, _ = fmt.Fprintf(out, "  note: %d round(s) have no attributable apply window and their batch numbers are omitted — a round is bounded below by the previous round's tag and above by its own, and an untagged round earlier than the last has neither. Freeze each round as the guide prescribes and the window closes\n", unattributed)
	}
	if totalEdits > 0 && totalF > 0 {
		_, _ = fmt.Fprintf(out, "  cost — %d edit(s) applied over %d changed line(s), %.2f finding(s)/edit",
			totalEdits, totalLines, float64(totalF)/float64(totalEdits))
		if totalLines > 0 && totalUnknown == 0 {
			_, _ = fmt.Fprintf(out, ", %.1f per 100 changed line(s)", 100*float64(totalF)/float64(totalLines))
		} else if totalUnknown > 0 {
			_, _ = fmt.Fprintf(out, "; %d batch(es) unreadable, so the per-line figure is ABSENT rather than lower", totalUnknown)
		}
		_, _ = fmt.Fprintf(out, " across the gate\n")
	}
	// The cost line above is the WHOLE population — every batch, including the
	// ones after the last freeze, which belong to no round window. `wf apply`
	// predicts from a narrower one, and the two numbers therefore differ. That
	// used to happen silently and by 60% on a live gate, which is why one
	// derivation is shared; where they legitimately differ, the difference is
	// PRINTED rather than reconciled by pretending one of them is the other.
	if hist, skipped := batchHistory(w); len(hist) > 0 {
		hl, hf := 0, 0
		var ns []int
		for _, h := range hist {
			hl += h.Lines
			hf += h.Findings
			ns = append(ns, h.N)
		}
		line := fmt.Sprintf("  prediction basis — rounds %s, %d finding(s) over %d changed line(s), %.1f per 100; `wf apply` predicts from THIS, not from the gate total above",
			roundList(ns), hf, hl, 100*float64(hf)/float64(hl))
		if len(skipped) > 0 {
			line += fmt.Sprintf(" (round(s) %s excluded — their window ADDS an artifact, so it prices a corpus their batches did not write)", roundList(skipped))
		}
		_, _ = fmt.Fprintln(out, line)
	}
	if showTimeFlag {
		spans := make([]float64, 0, len(times))
		longest, longestN := 0.0, 0
		// A round that CONTAINS a live run is not a long round, and the median
		// is a statement about review rounds. Measured on mocker A3: round 13
		// spans 288.2 min against a median of 45.3 because it held the launch
		// — 38 agents over 4h36m — and the note under the line said a span far
		// above the median is a break rather than work, which is exactly
		// backwards there. Its span is printed on its own, because the launch
		// is the one event in a gate's life most worth timing.
		var runs []int
		for n, rt := range times {
			if kinds[n] == "run" {
				runs = append(runs, n)
				continue
			}
			spans = append(spans, rt.Span)
			if rt.Span > longest {
				longest, longestN = rt.Span, n
			}
		}
		sort.Ints(runs)
		sort.Float64s(spans)
		auth := ""
		if m, ok := authoringWindow(w); ok {
			auth = fmt.Sprintf("authoring %.1f min (init -> first artifact); ", m)
		}
		if len(spans) > 0 {
			recs := commitLog(w, "HEAD")
			gate := float64(recs[len(recs)-1].At-recs[0].At) / 3600
			// The population is every round the gate opened MINUS the run
			// rounds, and the line says exactly that rather than calling it
			// "review rounds": a `-kind tool` round is in this median too, it
			// always has been, and the medians this project has published (15.5
			// over 35 rounds of three gates) were measured over it. Narrowing a
			// population silently is the defect one line up.
			apart := ""
			if len(runs) > 0 {
				apart = fmt.Sprintf(" (%d run round(s) counted apart, below)", len(runs))
			}
			_, _ = fmt.Fprintf(out, "  time — %s%d round(s) over %.1f h, median round %.1f min, longest %.1f min (round %d)%s\n",
				auth, len(spans), gate, spans[len(spans)/2], longest, longestN, apart)
			// The median, not the mean: a span of several hours is the operator
			// away from the desk, and three of the four measured gates carry one.
			// A mean over those describes nobody's round.
			_, _ = fmt.Fprintf(out, "  note: read the median — a span far above it is a break, not work, and it is in the total\n")
		}
		if len(spans) == 0 && len(runs) > 0 {
			// Every round of this gate contains a run, so there is no review
			// median at all — said, rather than left as a missing line.
			_, _ = fmt.Fprintf(out, "  time — %sno round to take a median over; every round below CONTAINS a run\n", auth)
		}
		for _, n := range runs {
			_, _ = fmt.Fprintf(out, "  run — round %d spans %.1f min and CONTAINS a run of what the gate built: not a break, not a review, and outside the median above\n",
				n, times[n].Span)
		}
	}
	return nil
}

// frozenLines reads the artifact size a round's freeze recorded. Older freezes
// carry no such field, and a missing one is ABSENT, never zero: a workspace
// frozen by an earlier binary must not report its corpus as having shrunk to
// nothing.
func frozenLines(w *ws.Workspace, n int) (int, bool) {
	b, err := os.ReadFile(filepath.Join(w.RoundDir(n), "freeze.json"))
	if err != nil {
		return 0, false
	}
	var m struct {
		ArtifactLines int `json:"artifactLines"`
	}
	if json.Unmarshal(b, &m) != nil || m.ArtifactLines == 0 {
		return 0, false
	}
	return m.ArtifactLines, true
}

// docNames maps a `wf docs` argument to its embedded reference. `guide` is a
// runtime concatenation of the manual's parts, never a stored second copy —
// two copies of a spec is this project's most-repeated defect class.
var docNames = map[string]string{
	"authoring":    "authoring.md",
	"routing":      "model-routing.md",
	"preflight":    "pre-flight-gate.md",
	"section-gate": "section-gate.md",
	"cost":         "cost-discipline.md",
	"checks":       "gate-checks.md",
}

// guideOrder is the manual's reading order for the concatenated form.
var guideOrder = []string{"authoring", "routing", "preflight", "section-gate", "cost"}

// manualDrift compares the references EMBEDDED in this binary with the copy a
// session actually loads — the installed plugin's cached tree — and returns the
// files that differ.
//
// Measured: the plugin sat at the version it was installed at while the clone
// moved four releases on. Every reference differed, and the SKILL.md sessions
// loaded still described a rule the manual had since replaced. A binary and a
// skill built from one commit are the same document; when they are not, the
// gate is briefed from one and run by the other, and nothing said so. This is
// the freeze manifest's version stamp applied to the spec channel: a NOTE, not
// a refusal — a rebuilt binary legitimately serves an older plugin until the
// next install.
func manualDrift() (string, []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", nil
	}
	// The REGISTERED install path, never a glob over the cache: an update leaves
	// the previous version's directory behind, and a glob found the abandoned
	// one and reported drift no session could see. A check that fires on a
	// correct install is the false red this binary refuses to print elsewhere.
	dirs := registeredPluginDirs(home)
	// The retired user-level symlink. install.sh removes it, and a machine that
	// still has one is loading a manual from a path nothing updates.
	dirs = append(dirs, filepath.Join(home, ".claude", "skills", "workflow-gate", "references"))
	{
		for _, dir := range dirs {
			// A path that is not there is not a stale manual. The legacy symlink
			// is absent on a machine that never had one, and reporting six
			// missing files over it would be a note about nothing.
			if st, err := os.Stat(dir); err != nil || !st.IsDir() {
				continue
			}
			var diffs []string
			cmp := func(name, embedded, installed string) {
				want, err := wfgate.Docs.ReadFile(embedded)
				if err != nil {
					return
				}
				got, err := os.ReadFile(installed)
				switch {
				case err != nil:
					diffs = append(diffs, name+" (missing)")
				case !bytes.Equal(got, want):
					diffs = append(diffs, name)
				}
			}
			// The front door first: a session reads SKILL.md before it reads any
			// reference, and the stale copy this check was written for still
			// described a rule the manual had replaced.
			cmp("SKILL.md", "skills/workflow-gate/SKILL.md", filepath.Join(filepath.Dir(dir), "SKILL.md"))
			for _, file := range docNames {
				cmp(file, "skills/workflow-gate/references/"+file, filepath.Join(dir, file))
			}
			if len(diffs) > 0 {
				sort.Strings(diffs)
				return dir, diffs
			}
		}
	}
	return "", nil
}

// registeredPluginDirs returns the references directory of every wfgate plugin
// the harness has actually INSTALLED, read from its own registry.
func registeredPluginDirs(home string) []string {
	b, err := os.ReadFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	var reg struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if json.Unmarshal(b, &reg) != nil {
		return nil
	}
	var out []string
	for _, entries := range reg.Plugins {
		for _, e := range entries {
			if e.InstallPath == "" {
				continue
			}
			dir := filepath.Join(e.InstallPath, "skills", "workflow-gate", "references")
			if st, err := os.Stat(dir); err == nil && st.IsDir() {
				out = append(out, dir)
			}
		}
	}
	sort.Strings(out)
	return out
}

// noteManualDrift prints the drift to STDERR, so `wf docs guide > file` still
// captures the manual and the warning still reaches a person.
func noteManualDrift() {
	dir, diffs := manualDrift()
	if len(diffs) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "note: the manual a session LOADS differs from the one this binary carries — %s\n", dir)
	fmt.Fprintf(os.Stderr, "      drifted: %s\n", strings.Join(diffs, ", "))
	fmt.Fprintf(os.Stderr, "      run scripts/install.sh (or `claude plugin update wfgate@wfgate-tools`) so both come from one commit\n")
}

func readDoc(name string) ([]byte, error) {
	return wfgate.Docs.ReadFile("skills/workflow-gate/references/" + docNames[name])
}

// cmdDocs serves the operating manual from the binary itself. It exists so the
// shipped tool needs no guide standing in an agent's context: the skill
// triggers, this command delivers, and the version of the text is the version
// of the binary that behaves as it describes.
func cmdDocs(args []string) error {
	defer noteManualDrift()
	if len(args) == 0 {
		fmt.Print(`wf docs <name> — print one reference of the operating manual

  guide         the whole manual (authoring + routing + preflight + section-gate + cost)
  authoring     language rule, companions, the spawn wrapper name
  routing       model and effort routing: explicit per-agent tiers
  preflight     the pre-flight script-review gate and the wf round loop
  section-gate  the per-section dual-review gate inside a script
  cost          cost & context discipline
  checks        the failure-mode checklist every gate reviewer reads
`)
		return nil
	}
	name := args[0]
	if name == "guide" {
		for _, n := range guideOrder {
			b, err := readDoc(n)
			if err != nil {
				return err
			}
			_, _ = os.Stdout.Write(b)
		}
		return nil
	}
	if _, ok := docNames[name]; !ok {
		return fmt.Errorf("unknown doc %q; run `wf docs` for the list", name)
	}
	b, err := readDoc(name)
	if err != nil {
		return err
	}
	_, _ = os.Stdout.Write(b)
	return nil
}

// An apply commit records its own size twice: in the default subject, which
// `-m` replaces, and in a trailer, which nothing does. The subject form is kept
// because workspaces created before the trailer existed still carry it, and a
// meter that stopped reading them would answer "0" for a different reason.
const applyTrailer = "wf-apply:"

var applySubjectRe = regexp.MustCompile(`^apply: (\d+) edit\(s\)`)
var applyTrailerRe = regexp.MustCompile(`(?m)^` + applyTrailer + ` (\d+) edit\(s\)`)

// The per-edit body line `wf apply` writes for every replacement, unconditionally
// and since the command existed. It is the THIRD place a batch records its own
// size, and the only one left in a workspace older than the trailer whose
// operator wrote every subject by hand: p2d (186 edits) and p2f (150 over 27
// batches) carry neither of the other two, and the meter answered "no
// population" over both while the count sat in commit bodies git had all along.
var applyBodyLineRe = regexp.MustCompile(`^- [^ :]+: `)

// applyBatch is one batch as the meter reads it. Lines is the denominator that
// actually predicts the next round: measured across the three closed gates,
// findings correlate with EDITS at +0.26..+0.49 and with CHANGED LINES at
// +0.47..+0.82 — excluding each gate's round 1, whose window is the authoring
// commit and not a fix; lines win on two gates and tie on the third. An edit is
// not a unit: one window of two edits changed 933 lines, 931 of them in a single
// commit, and the meter — which had seen only the other, one-line edit — printed
// the window as "38.00 finding(s)/edit".
type applyBatch struct {
	Edits int
	Lines int
	Known bool   // false when git could not be read: unknown, never zero
	Src   string // trailer | subject | body — the third is INFERRED, and says so
}

// applyBodyEdits counts the per-edit lines of an apply body, and ONLY when every
// non-empty line below the subject is one. A hand commit that happens to open
// its body with "- path: note" must not be read as a batch, and a body with no
// such line at all is not one either.
func applyBodyEdits(msg string) int {
	i := strings.IndexByte(msg, '\n')
	if i < 0 {
		return 0
	}
	n := 0
	for _, l := range strings.Split(msg[i+1:], "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if !applyBodyLineRe.MatchString(l) {
			return 0
		}
		n++
	}
	return n
}

// changedLines is insertions+deletions over one commit. A binary file prints
// "-\t-" and is skipped rather than counted as zero-of-something.
func changedLines(w *ws.Workspace, sha string) (int, bool) {
	out, err := w.Git.Run("show", "--numstat", "--format=", sha)
	if err != nil {
		// Not zero. A denominator nobody could read must not become a measured
		// zero — that is the shape of every false green this project has paid
		// for, turned on the meter itself.
		return 0, false
	}
	n := 0
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		add, err1 := strconv.Atoi(f[0])
		del, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			continue
		}
		n += add + del
	}
	return n, true
}

// applyBatches reads every apply commit in a revision range and returns each
// one's edit count. A commit counts as an apply when it carries the trailer, or
// — for workspaces older than it — when its subject is the default one.
// commitRec is one commit as this file's derivations read it: the cost meter
// needs the message, the timing needs the time, and both walk the same log.
type commitRec struct {
	SHA  string
	At   int64
	Subj string
	Msg  string
}

func commitLog(w *ws.Workspace, rng string) []commitRec {
	out, err := w.Git.Run("log", "--reverse", "--format=%H%x1f%at%x1f%B%x00", rng)
	if err != nil {
		return nil
	}
	var recs []commitRec
	for _, r := range strings.Split(out, "\x00") {
		f := strings.SplitN(strings.TrimSpace(r), "\x1f", 3)
		if len(f) < 3 {
			continue
		}
		msg := strings.TrimSpace(f[2])
		if msg == "" {
			continue
		}
		at, _ := strconv.ParseInt(f[1], 10, 64)
		subj := msg
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			subj = msg[:i]
		}
		recs = append(recs, commitRec{SHA: f[0], At: at, Subj: subj, Msg: msg})
	}
	return recs
}

// The kind is optional here, unlike roundKinds' pattern: the earliest gates
// opened rounds before `-kind` existed, and their wall clock is data too.
var roundOpenAnyRe = regexp.MustCompile(`^round (\d+): open`)

// roundTime is one round's wall clock, derived from the workspace's own commit
// times — there is no second ledger and none is wanted: a round opens with a
// commit, its fixes are commits, and the next round opens with one.
//
// Measured need: "a round costs 10-40 minutes and the round COUNT is the cost
// function" had to be computed outside the tool, with awk over `git log`, before
// anything about the gate's cost could be argued at all. The split says where
// the minutes are: across 35 rounds of three gates whose span is under two hours
// (the rest are breaks), the review segment is 70-87% of a round, the fix is
// most of the rest, and the bookkeeping tail is about 3%. The pooled median such
// round is 15.5 minutes and none exceeds 40.
type roundTime struct {
	Span   float64 // open -> next open (or -> the last commit, for the open round)
	Review float64 // open -> first apply: the reviewers, plus writing the findings up
	Fix    float64 // first apply -> last apply
	Tail   float64 // last apply -> next open: record, freeze, verify, delta
	Split  bool    // false when the round applied nothing and only Span is real
}

func roundTimes(w *ws.Workspace) map[int]roundTime {
	recs := commitLog(w, "HEAD")
	if len(recs) == 0 {
		return nil
	}
	// Rounds are partitioned by git's own ORDER, not by timestamp: two commits
	// in the same second at a boundary fell into both adjacent rounds.
	opens := map[int]int{}
	order := []int{}
	for i, c := range recs {
		if m := roundOpenAnyRe.FindStringSubmatch(c.Subj); m != nil {
			n, _ := strconv.Atoi(m[1])
			if _, seen := opens[n]; !seen {
				opens[n] = i
				order = append(order, n)
			}
		}
	}
	out := map[int]roundTime{}
	for i, n := range order {
		lo := opens[n]
		hi := len(recs) - 1
		if i+1 < len(order) {
			hi = opens[order[i+1]]
		}
		from, to := recs[lo].At, recs[hi].At
		// `wf close` is PRESCRIBED to run twice — write the scaffold, fill it,
		// bank it — and its commits land after the last round's freeze. Folding
		// the time an operator spends writing the record into that round's span
		// inflates the round, the median and the gate total, in the very file
		// close then banks. Measured: 0.2 min became 93.6.
		if i+1 == len(order) {
			for j := hi; j > lo; j-- {
				if !touchesOnly(w, recs[j].SHA, closeDir) {
					hi = j
					break
				}
			}
			to = recs[hi].At
			if to < from {
				to = from
			}
		}
		rt := roundTime{Span: float64(to-from) / 60}
		var first, last int64
		seen := false
		for j := lo; j <= hi && j < len(recs); j++ {
			if !isApply(recs[j].Msg) {
				continue
			}
			if !seen {
				first, seen = recs[j].At, true
			}
			last = recs[j].At
		}
		if seen {
			rt.Review = float64(first-from) / 60
			rt.Fix = float64(last-first) / 60
			rt.Tail = float64(to-last) / 60
			rt.Split = true
		}
		out[n] = rt
	}
	return out
}

// touchesOnly reports whether every path a commit changed is under prefix.
func touchesOnly(w *ws.Workspace, sha, prefix string) bool {
	out, err := w.Git.Run("show", "--name-only", "--format=", sha)
	if err != nil {
		return false
	}
	any := false
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		any = true
		if !strings.HasPrefix(l, prefix+"/") && l != prefix {
			return false
		}
	}
	return any
}

// authoringWindow is init -> the first commit that puts a file under
// artifacts/: the largest block of a gate that no round covers, because it
// happens before round 1 opens. Measured at 31.7 and 42.2 minutes on two gates
// and reported by nothing. Derived from the TREE rather than a commit subject —
// the earliest workspaces predate `wf add` and seeded artifacts by hand.
func authoringWindow(w *ws.Workspace) (float64, bool) {
	recs := commitLog(w, "HEAD")
	if len(recs) == 0 {
		return 0, false
	}
	out, err := w.Git.Run("log", "--reverse", "--format=%at", "--diff-filter=A", "--", "artifacts")
	if err != nil {
		return 0, false
	}
	f := strings.Fields(out)
	if len(f) == 0 {
		return 0, false
	}
	at, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil || at < recs[0].At {
		return 0, false
	}
	return float64(at-recs[0].At) / 60, true
}

// isApply is the meter's classification, reused by the timing so the two never
// disagree about what an apply commit is.
// isApply delegates to applyCommitEdits so that "was this an apply commit" has
// ONE reading. Its own copy of the three patterns took a caller-supplied
// subject rather than the message's first line, so the two could disagree about
// the same commit — the second copy of one rule, which is this project's most
// repeated defect class.
func isApply(msg string) bool {
	_, _, ok := applyCommitEdits(msg)
	return ok
}

func applyBatches(w *ws.Workspace, rng string) []applyBatch {
	var out []applyBatch
	for _, c := range commitLog(w, rng) {
		n, src, ok := applyCommitEdits(c.Msg)
		if !ok {
			continue
		}
		lines, known := changedLines(w, c.SHA)
		out = append(out, applyBatch{Edits: n, Lines: lines, Known: known, Src: src})
	}
	return out
}

// applyCommitEdits is the ONE reading of "this commit came from `wf apply`, and
// this is how many edits it carried". The cost meter and the freeze note that
// warns about hand-edited rounds both ask it, because two derivations of one
// number is the defect class this project names as its most frequent — and this
// number has already been wrong once: the meter parsed the default SUBJECT, `-m`
// replaces it, and a whole gate of 27 batches measured zero.
func applyCommitEdits(msg string) (edits int, src string, ok bool) {
	// The LAST match, not the first: the trailer is written at the end of the
	// message, and an edit whose own text quotes one ("wf-apply: 999 edit(s)")
	// lands in the per-edit list ABOVE it. Reading the first would let the
	// batch's own content restate the batch's size.
	if t := applyTrailerRe.FindAllStringSubmatch(msg, -1); t != nil {
		n, _ := strconv.Atoi(t[len(t)-1][1])
		return n, "trailer", true
	}
	subject := msg
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		subject = msg[:i]
	}
	if sm := applySubjectRe.FindStringSubmatch(subject); sm != nil {
		n, _ := strconv.Atoi(sm[1])
		return n, "subject", true
	}
	if n := applyBodyEdits(msg); n > 0 {
		return n, "body", true
	}
	return 0, "", false
}

// roundWindow is one round's apply window as BOTH readers of it see it — the
// per-round report and the cost meter. It is one derivation because the two
// used to compute the window separately, and a second copy of one rule is the
// defect class this project names as its most frequent.
type roundWindow struct {
	N            int
	Range        string
	Attributable bool
	// Authoring is true when the window contains ANY commit that ADDED an
	// artifact — not only the first one. A round that brings a new 800-line
	// document into the gate prices a corpus its batches did not write exactly
	// as round 1 does, so both are excluded; the comment used to say "the first"
	// while the code read every one, which is the text-code divergence this
	// project names as its most frequent defect. Such a window prices a corpus
	// its batches did not produce: measured on a live decision gate, round 1 filed 34 findings
	// against 9 attributable changed lines, the meter read 377.8 per 100, and
	// `wf apply` predicted "~355 finding(s) in the next round" against an actual
	// 24. A prediction from that window is not a large number, it is no number.
	Authoring bool
}

// roundWindows bounds each round below by the previous round's tag and above by
// its own. An untagged round earlier than the last has neither bound, so its
// range would degrade to every ancestor — measured: three rounds each printed
// the workspace total, with two different ratios fabricated from the same 26
// edits.
func roundWindows(w *ws.Workspace) []roundWindow {
	created := map[string]bool{}
	if out, err := w.Git.Run("log", "--format=%H", "--diff-filter=A", "--", "artifacts"); err == nil {
		for _, f := range strings.Fields(out) {
			created[f] = true
		}
	}
	var out []roundWindow
	for n := 1; n <= w.M.Round; n++ {
		tag := ws.RoundTag(n)
		ref := tag
		if !w.Git.TagExists(tag) {
			ref = "HEAD"
		}
		lowerOK := n == 1 || w.Git.TagExists(ws.RoundTag(n-1))
		upperOK := w.Git.TagExists(tag) || n == w.M.Round
		rw := roundWindow{N: n, Range: ref, Attributable: lowerOK && upperOK}
		if n > 1 {
			rw.Range = ws.RoundTag(n-1) + ".." + ref
		}
		if rw.Attributable {
			for _, c := range commitLog(w, rw.Range) {
				if created[c.SHA] {
					rw.Authoring = true
					break
				}
			}
		}
		out = append(out, rw)
	}
	return out
}

// histRound is one round the cost meter can actually learn from.
type histRound struct {
	N        int
	Findings int
	Lines    int
}

// batchHistory sums what this workspace has already measured about itself:
// changed lines (from its apply commits) and findings filed (from the ledger).
// A workspace's OWN ratio is the only form in which the number will be believed.
//
// Rounds whose window contains the authoring commit are EXCLUDED, not weighted:
// their findings were filed against a corpus those batches did not write, and
// including one made the meter predict ~355 findings for a round that produced
// 24. Excluding is honest in a way that any correction factor would not be —
// there is no ratio there to correct.
func batchHistory(w *ws.Workspace) (hist []histRound, authoringSkipped []int) {
	for _, rw := range roundWindows(w) {
		if !rw.Attributable {
			continue
		}
		lines := 0
		for _, b := range applyBatches(w, rw.Range) {
			if !b.Known {
				// An unreadable batch is an unknown denominator, and a ratio over
				// a denominator short by an unknown amount is not a smaller
				// ratio, it is no ratio.
				lines = 0
				break
			}
			lines += b.Lines
		}
		findings := 0
		if b, err := os.ReadFile(filepath.Join(w.RoundDir(rw.N), "findings.md")); err == nil {
			for _, r := range ws.ParseFindings(b) {
				if !r.Placeholder() && !r.Malformed {
					findings++
				}
			}
		}
		if lines == 0 || findings == 0 {
			continue
		}
		if rw.Authoring {
			authoringSkipped = append(authoringSkipped, rw.N)
			continue
		}
		hist = append(hist, histRound{N: rw.N, Findings: findings, Lines: lines})
	}
	return hist, authoringSkipped
}

// batchNote is the apply-side half of the cost meter: a NOTE, never a refusal
// — the operator may have a reason, and a tool that blocks a legal operation
// on a heuristic gets worked around rather than heeded. No history, no claim:
// an unmeasured workspace predicts nothing.
//
// It prints the workspace's own TREND, not only its pooled ratio, and says
// which regime the number came from. Measured on a live decision gate: the
// fixes got steadily smaller (447 -> 354 -> 264 -> 204 changed lines) and
// steadily denser in defects (5.4 -> 7.1 -> 8.7 -> 10.3 per 100), so a pooled
// ratio predicted 5-6 findings against actuals of 21-25. A script gate measured
// the ratio FLAT across a 12x range of batch sizes. The two do not disagree —
// they measure different regimes, and the pooled number cannot say which one it
// came from. The series can, and the operator reads it in one glance.
func batchNote(edits, lines int, hist []histRound, authoringSkipped []int) (string, bool) {
	if len(hist) == 0 {
		return "", false
	}
	histLines, histFindings := 0, 0
	for _, h := range hist {
		histLines += h.Lines
		histFindings += h.Findings
	}
	if histLines == 0 || histFindings == 0 {
		return "", false
	}
	per100 := 100 * float64(histFindings) / float64(histLines)
	predicted := float64(lines) * float64(histFindings) / float64(histLines)
	if predicted < 5 {
		return "", false
	}
	var series []string
	for _, h := range hist {
		series = append(series, fmt.Sprintf("r%d %.1f", h.N, 100*float64(h.Findings)/float64(h.Lines)))
	}
	dir := ""
	// Three rounds is the shortest run in which a direction is a direction and
	// not two points. Only monotone runs are named: the reading is "which regime
	// is this gate in", and a series that wanders is not in one.
	if len(hist) >= 3 {
		up, down := true, true
		for k := 1; k < len(hist); k++ {
			a := float64(hist[k-1].Findings) / float64(hist[k-1].Lines)
			b := float64(hist[k].Findings) / float64(hist[k].Lines)
			if b <= a {
				up = false
			}
			if b >= a {
				down = false
			}
		}
		switch {
		case up:
			dir = " — RISING: the fixes are reaching the coupled core, where every changed line is a rule-changing line and the prediction below is a FLOOR"
		case down:
			dir = " — falling: the remaining work is prose, where batch size does predict cost"
		}
	}
	note := fmt.Sprintf("  note: %d edit(s) changing %d line(s); this workspace's own history (%d finding(s) over %d changed line(s), %.1f per 100) predicts ~%.0f finding(s) in the next round — split by RULE (one rule and all its copies per batch), never mechanically. And if this batch changed a RULE, its consumers are NOT in this diff: `wf check stale` sweeps for restatements of the OLD rule, or the next round will do it for you",
		edits, lines, histFindings, histLines, per100, predicted)
	note += fmt.Sprintf("\n  trend: %s per 100 changed line(s)%s", strings.Join(series, ", "), dir)
	if len(authoringSkipped) > 0 {
		note += fmt.Sprintf("\n  note: round(s) %s are NOT in that history — their window ADDS an artifact, so their findings were filed against a corpus those batches did not write. One such round put the prediction at ~355 against an actual 24", roundList(authoringSkipped))
	}
	return note, true
}

// worktreeChangedLines is what git sees changed in the files just written, before
// they are committed.
func worktreeChangedLines(w *ws.Workspace, paths []string) int {
	args := append([]string{"diff", "--numstat", "--"}, paths...)
	out, err := w.Git.Run(args...)
	if err != nil {
		return 0
	}
	n := 0
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		add, e1 := strconv.Atoi(f[0])
		del, e2 := strconv.Atoi(f[1])
		if e1 != nil || e2 != nil {
			continue
		}
		n += add + del
	}
	return n
}

// roundKinds derives each round's kind from its opening commit subject
// ("round N: open (kind) — title"). Derived, not stored: the manifest is small
// on purpose, and `wf round record` overwrites the scaffold that carries the
// kind line — the commit subject is the copy nothing rewrites.
var roundOpenRe = regexp.MustCompile(`^round (\d+): open \((full|delta|tool|run)\)`)

// unreadRounds names every round up to `upto` that was opened and shows no sign
// of having been reviewed: its ledger parsed and holds nothing but the
// scaffold's unfilled rows, it is not a `-kind tool` round (whose opening
// subject already records that no reviewer was dispatched), and it banks no
// VALID lens. A round whose ledger cannot be read is not this shape and is
// named by its own check.
func unreadRounds(w *ws.Workspace, upto int, kinds map[int]string) []int {
	var out []int
	for n := 1; n <= upto; n++ {
		if kinds[n] == "tool" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(w.RoundDir(n), "findings.md"))
		if err != nil || !ws.ScaffoldOnly(ws.ParseFindings(b)) {
			continue
		}
		if validLenses(roundLenses(w, n)) > 0 {
			continue
		}
		out = append(out, n)
	}
	return out
}

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

// divergenceOf is the dup-side spelling of check.Divergence: every conflict this
// command prints is a set of copies that AGREE almost everywhere, so the shared
// opening is the least informative window there is. A NUMERIC conflict differs
// by one token, which sat past the old truncation as often as not.
func divergenceOf[T check.Site | check.Statement](sites []T) []string {
	texts := make([]string, len(sites))
	for i, s := range sites {
		switch v := any(s).(type) {
		case check.Site:
			texts[i] = v.Text
		case check.Statement:
			texts[i] = v.Text
		}
	}
	return check.Divergence(texts, 110)
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

// roundList prints round numbers as ranges: "18-25" is a fact an operator acts
// on, "18, 19, 20, 21, 22, 23, 24, 25" is a line they skim.
func roundList(ns []int) string {
	if len(ns) == 0 {
		return ""
	}
	var parts []string
	start, prev := ns[0], ns[0]
	flush := func() {
		switch {
		case start == prev:
			parts = append(parts, strconv.Itoa(start))
		case prev == start+1:
			parts = append(parts, strconv.Itoa(start)+", "+strconv.Itoa(prev))
		default:
			parts = append(parts, strconv.Itoa(start)+"-"+strconv.Itoa(prev))
		}
	}
	for _, n := range ns[1:] {
		if n == prev+1 {
			prev = n
			continue
		}
		flush()
		start, prev = n, n
	}
	flush()
	return strings.Join(parts, ", ")
}

// sharedPath returns the file path every one of these finding texts cites, ""
// when they do not agree on one. A run of single-blocker rounds over ONE region
// is the strong form of the signal; over several regions it is just a gate
// converging.
func sharedPath(texts []string) string {
	if len(texts) == 0 {
		return ""
	}
	var common map[string]bool
	for _, t := range texts {
		here := map[string]bool{}
		for _, m := range citePathRe.FindAllStringSubmatch(t, -1) {
			here[m[1]] = true
		}
		if common == nil {
			common = here
			continue
		}
		for p := range common {
			if !here[p] {
				delete(common, p)
			}
		}
	}
	// One path, or none: two shared paths say the run touches a pair of files,
	// which is not the "one region" claim this note makes.
	if len(common) != 1 {
		return ""
	}
	for p := range common {
		return p
	}
	return ""
}

// The citation form the ledger's finding cells carry: `path/file.ext:12` or a
// bare path. The anchor and the line range are irrelevant here — the claim is
// about the REGION, and the file is as fine-grained as a ledger row gets.
// The stem must be at least two characters and the extension alphabetic and at
// least two, because the loose form matched the prose it reads: on four rounds
// whose findings all say "e.g." the note read `every blocker in that run cites
// e.g — one region`. `i.e` and a version number `1.2` are the same class. The
// rule is the one `internal/check/cite.go` already applies to a citation.
var citePathRe = regexp.MustCompile(`\b([\w./-]*[\w-]{2,}\.[A-Za-z]{2,8})(?::\d+)?\b`)
