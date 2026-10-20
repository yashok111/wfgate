package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	wfgate "github.com/yashok111/wfgate"
	"github.com/yashok111/wfgate/internal/ws"
)

// lensAimMarker is the one line the operator MUST replace. `wf round lens`
// refuses to bank a prompt still carrying it: a scaffold banked unfilled
// records that a round had a lens and not what it was asked, which is the claim
// the roster exists to make and the one `-name`/`-model`/`-asks` already refuse
// in their own halves.
const lensAimMarker = "<<< WRITE YOUR VECTOR HERE >>>"

// cmdNewLens emits the fixed half of a reviewer's brief.
//
// Measured 2026-09-01 over the lens prompts banked in the gate workspaces on
// this box — 313 prompts, 16 380 non-blank lines, median 44 non-blank each: the
// lines recurring in five or more DISTINCT prompts hold 29.9% of them, and in
// ten or more, 19.9%. The reading is in `wf docs measured` [M-37] and the
// corpus is live, so re-run it rather than quoting these. Not the same
// measurement `wf new-script` was justified by, and the comment there says why:
// that one counts BLOCK NAMES recurring in five of six scripts and records that
// the blocks are re-authored rather than copied, at 0.05 textual similarity.
// This one counts byte-identical line text at five of 313. Same conclusion,
// different quantity, and the lens case does not need to borrow the other's.
//
// What makes it the same FAILURE is the rest: 200 of the prompts name
// `gate-checks` and exactly ONE names `wf docs`, so the recurring third is the
// manual RE-AUTHORED by hand rather than pointed at, and it goes stale in
// place. 26 carry the severity scale's NOTE line in a wording the manual has
// replaced, and no check can see it: `wf check dup` runs over artifacts, and a
// lens prompt is not one.
//
// Hence what this template does NOT do: restate the scale, or what a row must
// carry. It POINTS at the `gate-checks.md` that `wf init` pinned into the
// workspace — the copy that gate's reviewers were actually briefed from — and
// names the sections to read. A rule that lives in one place cannot drift from
// itself. The first version pointed AND restated section 0's decision rule
// beside the pointer, which is the 27th unchecked copy emitted by the command
// built to stop unchecked copies; `newlens_test.go` now forbids the sentences
// rather than their labels.
//
// And what it deliberately leaves EMPTY is the vector. A gate's blocker has
// come from the lens whose instruction was under-specified beside the ones the
// round's findings aimed; a scaffold that wrote the aim would close exactly
// that. The fixed half is what is true of every round, and nothing else.
func cmdNewLens(args []string) error {
	fs := flag.NewFlagSet("new-lens", flag.ExitOnError)
	round := fs.Int("round", 0, "the round this lens reads (default: the workspace's open round)")
	kind := fs.String("kind", "", "delta|full (default: full for round 1, delta after)")
	asks := fs.String("asks", "", "the question set this vector will claim; validated here and carried into the `wf round lens` line printed at the end, and it does NOT change the brief")
	out := fs.String("o", "", "write here (default: lens-<name>.md beside you)")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: wf new-lens <name> [-round N] [-kind delta|full] [-asks Q,Q] [-o FILE]")
	}
	name := pos[0]
	if strings.ContainsAny(name, `/\`+"\x00") || name == "." || name == ".." {
		return fmt.Errorf("name %q must be a plain identifier, not a path — it is the roster label, not a file", name)
	}

	w, err := open()
	if err != nil {
		return fmt.Errorf("%w — a lens brief names the artifacts, the pinned gate-checks.md and the round's brief by PATH, and none of those exist outside a workspace", err)
	}
	n := *round
	if n == 0 {
		n = w.M.Round
	}
	switch {
	case *round != 0 && *round < 1:
		// Given, and impossible. The first version reported it as ABSENT,
		// which is a diagnosis about the wrong thing in a message meant to
		// carry the patch.
		return fmt.Errorf("-round %d is not a round number; rounds start at 1", *round)
	case n < 1:
		return fmt.Errorf("no round is open and -round was not given; open one with `wf round new` or name one with -round N")
	case n > w.M.Round:
		// `wf round lens` refuses a round the gate has not opened, and two
		// sibling commands disagreeing about the same argument is how an
		// operator learns the rule twice.
		return fmt.Errorf("-round %d: this gate has opened %d round(s), so there is nothing to brief a lens for", n, w.M.Round)
	}
	k := strings.ToLower(*kind)
	switch k {
	case "":
		// Round 1 has nothing to be a delta of. Every later round does, and the
		// three asks are required of it [R3] — deriving the default from the
		// round rather than asking is the same inference `wf round new` makes.
		if n == 1 {
			k = "full"
		} else {
			k = "delta"
		}
	case "delta", "full":
	default:
		return fmt.Errorf("-kind %q is neither delta nor full", *kind)
	}
	var parsedAsks []string
	if *asks != "" {
		parsedAsks, err = parseAsks(*asks)
		if err != nil {
			return err
		}
	}
	// The banking command reduces a name to an ASCII slug and refuses one that
	// reduces to nothing. Refusing it HERE rather than after the emit is the
	// same trade every other refusal in this command makes: the scaffolder must
	// not accept a name its own follow-up line cannot be run with.
	if lensSlug(name) == "" {
		return fmt.Errorf("name %q reduces to an empty slug: `wf round lens` names the banked file `lens-<slug>.md`, so the name must carry ASCII letters or digits", name)
	}

	dst := *out
	if dst == "" {
		dst = "lens-" + name + ".md"
	}
	if err := checkLensDest(w, dst); err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("%s exists — name another file with -o; a skeleton must never land on an authored prompt", dst)
	}

	b, err := wfgate.Templates.ReadFile("templates/lens.md")
	if err != nil {
		return err
	}
	body := strings.ReplaceAll(string(b), "{{KIND_PHRASE}}", lensKindPhrase(k))
	body = strings.ReplaceAll(body, "{{READING}}", lensReading(w, n, k))
	body = strings.ReplaceAll(body, "{{ASKS}}", lensAsksBlock(k))
	// From ws.Levels, so the brief's scale and the ledger's are one value.
	// NOTE: no mutation can kill this wiring. Freezing it to the literal
	// "BLOCKER, MAJOR, MINOR, NOTE" passes every bar, because that IS the
	// current value and a test can only compare against it. The test asserts
	// ws.Levels equals that literal AND that the brief carries it, which pins
	// the RESULT; the wiring is proven the day ws.Levels changes and not
	// before. Written down rather than left as a guard nothing can fail.
	body = strings.ReplaceAll(body, "{{LEVELS}}", strings.Join(ws.Levels, ", "))
	if strings.Contains(body, "{{") {
		return fmt.Errorf("the lens template still carries an unsubstituted {{...}} placeholder — the template and this command have drifted")
	}

	if d := filepath.Dir(dst); d != "." {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	// O_EXCL rather than the Stat above alone: that check is a race, and
	// WriteFile truncates through a dangling symlink at dst.
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("%s: %w — a skeleton must never land on an authored prompt", dst, err)
	}
	if _, err := f.WriteString(body); err != nil {
		_ = f.Close()
		// A refusal that leaves the tree dirty has committed the thing it
		// refused: a half-written brief is still a `lens-*.md` and
		// `wf round lens` would bank it verbatim. Same rollback `wf round
		// close` grew when a failed commit left the ledger rewritten.
		if rmErr := os.Remove(dst); rmErr != nil {
			return fmt.Errorf("%w; and the partial file could not be removed: %v — delete %s before banking anything", err, rmErr, dst)
		}
		return err
	}
	if err := f.Close(); err != nil {
		if rmErr := os.Remove(dst); rmErr != nil {
			return fmt.Errorf("%w; and the file could not be removed: %v — delete %s before banking anything", err, rmErr, dst)
		}
		return err
	}

	fmt.Printf("wrote %s — %d line(s), round %d, kind %s\n", dst, strings.Count(body, "\n"), n, k)
	// A prompt is not an artifact and never becomes one: `wf round lens` COPIES
	// it into the round. So a copy left inside the workspace is a stray file,
	// and `wf verify` reports it as `uncommitted` under "what would run is NOT
	// what was reviewed. Do not launch." -- advice that is wrong for a scaffold
	// and right for everything else it says that about. `wf new-script` pays
	// the same cost the other way (its output IS an artifact, so it lands in
	// artifacts/ and says it is untracked); this one says where to put it.
	if rel, err := filepath.Rel(resolveExisting(w.Root), resolveExisting(mustAbs(dst))); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		fmt.Printf("  it is INSIDE the workspace, where `wf verify` reports it as uncommitted and says not to launch — write it outside with -o, or delete it once `wf round lens` has banked it\n")
	}
	fmt.Printf("  the fixed half only: the scale, the reading list and the output contract\n")
	fmt.Printf("  WRITE THE VECTOR — the marker line is the aim, and `wf round lens` refuses to bank a prompt still carrying it\n")
	fmt.Printf("  then, after the lens has returned:\n")
	// %q and the PARSED asks, comma-joined. The raw `-asks "closure ripple"` is
	// a spelling parseAsks accepts and echoing it verbatim produced a line
	// whose second word is a positional argument, which `wf round lens` then
	// refuses -- a follow-up the command prints and cannot be run with.
	// The clause is DROPPED when no asks were given, rather than falling back to
	// a `<Q,Q>` placeholder: `<` and `>` are shell redirections, so the printed
	// line truncated the command at `-asks` and created an empty file named `Q,Q`
	// in the operator's cwd. A follow-up the command prints must be a line that
	// can be pasted. When asks WERE given they are quoted, because the parsed
	// list is joined with commas and a comma-bearing argument is one word only
	// inside quotes.
	asksArg := ""
	if len(parsedAsks) > 0 {
		asksArg = fmt.Sprintf(" -asks %q", strings.Join(parsedAsks, ","))
	}
	fmt.Printf("    wf round lens %q -name %q -model sonnet%s -round %d\n", dst, name, asksArg, n)
	return nil
}

func lensKindPhrase(kind string) string {
	if kind == "delta" {
		return "This is a DELTA round: you do NOT re-read the whole artifact. You read the diff\nand the brief, and your findings say what you did not re-read."
	}
	return "This is a FULL read: you read the artifact whole rather than a diff, and\nyour findings cover all of it."
}

// lensAsksBlock is the three questions a delta round may never drop [R3]. A
// full round has nothing to be a delta of, so it gets none — printing them over
// a round with no predecessor is the absent-measurement-in-the-register-of-a-
// measured-one class, one layer up from a report.
func lensAsksBlock(kind string) string {
	if kind != "delta" {
		return "\n"
	}
	return `
EVERY LENS OF THIS ROUND ASKS THESE THREE, whatever else it is aimed at, and
section 0b defines them:

  CLOSURE      — for every ` + "`closed by`" + ` claim in the brief: did that commit reach
                 what the finding names, and is the finding actually gone?
  FIX-INDUCED  — what did the previous round's own fix break that was not broken
                 before? Say so in the row; the operator marks it [fix-induced].
  RIPPLE       — which untouched paragraphs still argue from what the fix changed?
`
}

// lensReading names the paths rather than the documents, because a reviewer
// handed a document NAME goes looking for it and a reviewer handed a path reads
// it. A delta round's brief is named EITHER WAY, and when it is missing the
// line says so and names the command that writes it: a reading list naming a
// file nobody wrote is what one gate's reviewer reconstructed from the tag diff
// and then reported as a finding, and a reading list that silently omitted it
// would send the operator to the launch without one. Same trade `wf delta`
// makes on stderr.
func lensReading(w *ws.Workspace, n int, kind string) string {
	var b strings.Builder
	pinned := filepath.Join(w.Root, "gate-checks.md")
	fmt.Fprintf(&b, "  %s\n", pinned)
	// With LINE RANGES, read off the pinned copy. Measured on A4's 23
	// reviewers: 36 Read calls into gate-checks.md, most with a hand-found
	// offset, and 18 shell greps for its headings before them — every one a
	// turn, and a lens's wall clock is its turns. The index is computed from
	// the copy this gate pinned, because that is the one with the stamp line
	// at the top and the one the lens will open.
	head, rest, err := gateChecksIndex(pinned)
	switch {
	case err != nil:
		fmt.Fprintf(&b, "      sections 0, 0b and 0c — the scale, the questions, what a row must carry (no line index: %v)\n", err)
	default:
		fmt.Fprintf(&b, "      sections 0, 0b and 0c — the scale, the questions, what a row must carry: lines %s\n", head)
		fmt.Fprintf(&b, "      the other sections by line, so none has to be searched for: %s\n", rest)
	}

	arts, err := w.ArtifactList()
	if err != nil || len(arts) == 0 {
		// Not a failure to report and not a line to fake: a gate with no
		// artifact yet is `wf init` before `wf add`, and a reading list that
		// printed nothing there would read as "there is nothing to read".
		fmt.Fprintf(&b, "  %s/  — THIS GATE HAS NO ARTIFACT YET; name what the lens reads by hand\n", w.Artifacts())
	}
	for _, a := range arts {
		// ArtifactList returns paths relative to the workspace ROOT, already
		// carrying the artifacts/ prefix. Joining Artifacts() onto one doubled
		// the segment and sent the lens after a path that does not exist.
		fmt.Fprintf(&b, "  %s\n", filepath.Join(w.Root, a))
	}

	brief := filepath.Join(w.RoundDir(n), "brief.md")
	switch {
	case kind != "delta":
		// A full round has no predecessor to be briefed from. Naming a brief
		// here would send the lens after a file the round is not supposed to
		// have.
	default:
		if _, err := os.Stat(brief); err == nil {
			fmt.Fprintf(&b, "  %s  — the delta: what changed, and every `closed by` claim\n", brief)
		} else {
			fmt.Fprintf(&b, "  %s  — DOES NOT EXIST YET; run `wf delta -save` before you send this\n", brief)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// gateChecksIndex reads the `## <id>.` headings of the pinned gate-checks.md
// and returns the line range covering sections 0, 0b and 0c, and an index of
// every other section as `<id> <from>-<to>`. Ranges are 1-based and inclusive,
// which is what `Read`'s offset takes.
func gateChecksIndex(path string) (string, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	lines := strings.Split(string(b), "\n")
	type sec struct {
		id   string
		from int
	}
	var secs []sec
	for i, l := range lines {
		if !strings.HasPrefix(l, "## ") {
			continue
		}
		rest := strings.TrimPrefix(l, "## ")
		if dot := strings.Index(rest, ". "); dot > 0 && !strings.ContainsAny(rest[:dot], " ") {
			secs = append(secs, sec{id: rest[:dot], from: i + 1})
		} else {
			// A heading with no id (the file's closing "Appending to this
			// file") bounds the section before it and is not indexed.
			secs = append(secs, sec{id: "", from: i + 1})
		}
	}
	to := func(k int) int {
		if k+1 < len(secs) {
			return secs[k+1].from - 1
		}
		return len(lines)
	}
	headFrom, headTo := 0, 0
	var rest []string
	for k, sc := range secs {
		switch sc.id {
		case "0", "0b", "0c":
			if headFrom == 0 {
				headFrom = sc.from
			}
			headTo = to(k)
		case "":
		default:
			rest = append(rest, fmt.Sprintf("%s %d-%d", sc.id, sc.from, to(k)))
		}
	}
	if headFrom == 0 || len(rest) == 0 {
		return "", "", fmt.Errorf("%s carries no `## 0.`/`## A.` headings; the pinned copy is not the reference `wf init` writes", path)
	}
	return fmt.Sprintf("%d-%d", headFrom, headTo), strings.Join(rest, ", "), nil
}

// checkLensDest is the two refusals -o carries, and BOTH compare RESOLVED
// paths. The first version compared `filepath.Dir(dst)` against an absolute
// `w.RoundDir(n)`, so it fired for `-o "$PWD/rounds/r2/x.md"` and not for the
// spelling an operator actually types — `-o rounds/r2/x.md` from the workspace
// root wrote the file and printed the normal summary. The test pinned it with
// an absolute path and passed, which is a guard armed on the one input the
// defect does not take.
func checkLensDest(w *ws.Workspace, dst string) error {
	abs, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	// Through symlinks on both sides, and through the nearest EXISTING
	// ancestor of a path that does not exist yet — the same walk `wf init`'s
	// own containment refusal makes, and for the same reason: a workspace
	// reached through a symlinked parent compares unequal to itself.
	abs = resolveExisting(abs)

	// NEVER into the TARGET. `-o` was guarded against nothing, so it wrote
	// anywhere the process could, and the tree it must never touch is the one
	// under review: "`target` is read-only: no command writes into it", and
	// the gate's agents diff that tree against its own HEAD. This is
	// `apply`'s safeJoin one command over — "an agent that has confused the
	// workspace with the repository under review must be stopped here, not
	// discovered by a commit in the wrong tree."
	//
	// The bound is the target and NOT "inside the workspace": the brief's
	// whole point is that it lives beside the OPERATOR, who is frequently
	// standing somewhere else, and the default `lens-<name>.md` in the working
	// directory would be refused by its own command.
	if w.M.Target != "" {
		tgt := resolveExisting(w.M.Target)
		if rel, err := filepath.Rel(tgt, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("refusing to write %s: it resolves inside the TARGET %s, which this tool holds read-only — the gate's reviewers diff that tree against its own HEAD, and a file written there is a change nobody reviewed", dst, w.M.Target)
		}
	}

	// NEVER into ANY rounds/rN/, not only the round being briefed: `-round 2
	// -o rounds/r1/x.md` puts an unread scaffold into a round one over.
	//
	// The reason is NOT the one the first version gave. That comment said a
	// bare file there clears the freeze and close refusals demanding a round
	// was read — `touch rounds/r1/lens-anything.md` cleared one, once — and
	// the hole has since been closed at both ends: `parseLens` requires
	// `# Lens`, a `Model:` line and a fenced prompt, and `roundLenses` requires
	// the file to be TRACKED. Reproduced against this build, the touched file
	// does not clear anything and the roster reports it as `NOT a record`. What
	// IS true is that a scaffold there is round bookkeeping nobody wrote: it is
	// named `lens-*.md`, which is the shape the roster scans, it reads as a
	// vector that ran, and `wf verify` reports it against the freeze. The
	// prompt lives beside the operator; `wf round lens` banks it AFTER the
	// vector returns, which is the moment the record becomes true. And
	// `-o rounds/r5/x.md` would CREATE a round directory no `wf round new`
	// opened.
	rounds := resolveExisting(filepath.Join(w.Root, "rounds"))
	if dir := filepath.Dir(abs); dir == rounds || strings.HasPrefix(dir, rounds+string(filepath.Separator)) {
		return fmt.Errorf("refusing to write %s into %s: a `lens-*.md` there is the shape the roster scans, so an unbanked scaffold reads as a vector that ran, and `wf verify` reports it against the freeze. Write it beside yourself and let `wf round lens` bank it after the vector returns", dst, dir)
	}
	return nil
}

// resolveExisting resolves symlinks as far as the path exists, then re-appends
// the tail that does not. filepath.EvalSymlinks fails outright on a path whose
// leaf is not there yet, which is every destination this command is given.
func resolveExisting(p string) string {
	rest := ""
	for {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(p, rest)
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}

// mustAbs is filepath.Abs with the error folded away: it fails only when
// os.Getwd does, and a report line is not the place to surface that.
func mustAbs(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}
