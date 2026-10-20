package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/yashok111/wfgate/internal/ws"
)

// The lens roster: which reviewer vectors a round actually ran, and with what
// prompt.
//
// Measured need, and it is the largest confound in the corpus rather than a
// convenience. On one decision gate, over its delta rounds 2-8, the criterion
// vector was an EXTERNAL model at rounds 2, 4 and 6 and an internal one at 3,
// 5, 7 and 8; the gate blockers were 17, 16, 12 against 10, 11, 6, 7 — 1.76x —
// and the two lowest-blocker rounds of the whole gate are the two after the
// external lens was dropped for quota. (Round 1 sits outside that comparison
// because it is the full round against a first draft; its criterion vector was
// external too, at 13 blockers, and including it gives 1.71x — the narrowing is
// named because a population narrowed in silence is this tool's own named
// defect.) Any descent a stop rule reads there is confounded with a billing
// decision, and nothing in the workspace recorded which model ran what AS DATA:
// `rounds/rN/brief.md` is the `wf delta` output, not the reviewer prompt, and
// the roster survived only as prose in each findings header — hand-readable,
// readable by no command and printable in no report.
//
// A lens is banked as a FILE in the round directory, not a subdirectory:
// `wf close` copies every file of every round and skips directories, so a
// roster under `rounds/rN/lenses/` would be dropped from the record and then
// deleted by `wf prune` — the exact scar that made close copy the whole
// directory in the first place.
const lensPrefix = "lens-"

// lensFence separates the record's own header from the prompt it banks.
const lensFence = "\n---\n"

// lensQuestions is the CLOSED set of questions a reviewer vector can be said to
// ask. `-name` stays free text — it is the label the round's own findings use —
// and this is the comparable half beside it.
//
// Measured over the two gates that banked any roster at all, p3a through round 5:
// 13 lens RECORDS carrying TEN distinct names (17 FILES, four of them the copies
// `wf close` makes under close/rounds/; that gate is OPEN, so the figure moves —
// it is a snapshot, not a standing count) (external-hazards, run, repairs, final,
// consistency, tree-truth, implementability, failure-modes, delta-write-path,
// criterion), of which exactly ONE — criterion — appears as a VECTOR NAME
// anywhere in the manual.
// The other nine were invented per round, and nothing could compare a round of
// one gate with a round of another. The four remaining gates banked no roster,
// so their prompts are simply gone.
//
// Splitting the label from the question is what makes the refusal safe: a hard
// refusal on NAMES would have fired on half of correct practice, because a gate
// legitimately runs a vector its own slice needs. `other` is therefore always
// available and never counts toward required coverage — the refusal only ever
// means "you typed something no report can compare", never "your vector is
// illegitimate".
var lensQuestions = []string{
	// The round-1 set, from the manual's three reviewers and its measured re-cut.
	"orchestration", "routing", "alignment",
	"facts", "seams", "criterion",
	// The delta set. Each has a measured origin, not a taxonomy's symmetry.
	"closure", "fix-induced", "ripple",
	"other",
}

// lensDeltaRequired is what a DELTA round must ask among them [R3].
//
//   - closure     — measured: two hand-written closure lenses returned 6 and 4,
//     6 and 4 of 24, 5 of 25, and 6 and 4 of 23 CLOSURES they judged not
//     closed, in rounds that themselves filed 25, 23 and 21 findings (round 4's
//     second lens checked citations, not closures) — the most valuable half of
//     those rounds, produced by prose over data git already held.
//     `wf check closure` mechanised the reachable part; the judgement half is
//     this vector.
//   - fix-induced — measured: a fix is the largest single source of blockers.
//     Half the BLOCKERS of rounds 3+ on one phase, 5 of 11 on the next, and on
//     the one after that round 2 was 9 of 9 — every blocker existed only
//     because round 1's fix created it.
//   - ripple      — measured: a fix that changes a RULE has consumers that are
//     not in the diff. Four such ripples on one run were sentences arguing from
//     the retired rule while containing none of its keywords, so `grep`
//     returned nothing. `wf check stale` narrows the read; it does not replace
//     it, and says NOT A PASS where it swept nothing.
var lensDeltaRequired = []string{"closure", "fix-induced", "ripple"}

// knownLensQuestion takes an ALREADY canonical (trimmed, lower-case) word: both
// callers normalise before asking, so a case-fold here survived every mutation
// and this project deletes what nothing can make fail.
func knownLensQuestion(q string) bool {
	for _, k := range lensQuestions {
		if q == k {
			return true
		}
	}
	return false
}

// parseAsks splits and validates the -asks list. It returns the canonical
// lower-case spellings so a roster is comparable regardless of how it was typed.
func parseAsks(raw string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	// Comma, plus or WHITESPACE — all three, and the help text says so, because
	// an accepted spelling no report mentions is this wave's own named defect.
	// The list form has been accepted since the flag
	// existed and its own help text said `comma-separated`; measured on the
	// gate that stopped instead of closing, the operator banked a vector with
	// ONE question and then hand-edited `Asks:` in the file and committed it —
	// nineteen times, over ten rounds, having reached for
	// `-asks closure+fix-induced+ripple` and been refused. A separator nobody
	// can guess wrong is worth more than a spelling rule nobody reads: the
	// three questions a delta round may never drop are a SET, and every way of
	// writing a set down means the same thing.
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '+' || r == ' ' || r == '\t' || r == '\n'
	}) {
		q := strings.ToLower(strings.TrimSpace(part))
		if q == "" {
			continue
		}
		if !knownLensQuestion(q) {
			return nil, fmt.Errorf("-asks %q is not one of %s; the set is closed so one gate's roster can be compared with another's, and `other` is always available for a vector this list does not name",
				q, strings.Join(lensQuestions, ", "))
		}
		if !seen[q] {
			seen[q] = true
			out = append(out, q)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("-asks is required and was empty: a roster entry that does not say WHAT the vector asked records that a round had lenses and not which questions it put, which is the claim the gate cannot check")
	}
	return out, nil
}

// askList collects a flag that may be repeated. flag.Value's Set is called once
// per occurrence, so joining the occurrences and parsing the join keeps ONE
// reader of the syntax rather than two that can disagree.
type askList []string

func (a *askList) String() string { return strings.Join(*a, ",") }

func (a *askList) Set(v string) error {
	*a = append(*a, v)
	return nil
}

// sameAsks compares two canonical question lists as SETS: parseAsks already
// dedupes and lower-cases both, and order is not a claim.
func sameAsks(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	in := map[string]bool{}
	for _, q := range a {
		in[q] = true
	}
	for _, q := range b {
		if !in[q] {
			return false
		}
	}
	return true
}

// askDelta names WHICH half of the header differs, so the refusal is actionable
// rather than merely correct.
func askDelta(banked lensRecord, model string, asks []string) string {
	var parts []string
	if banked.Model != "" && !strings.EqualFold(banked.Model, model) {
		parts = append(parts, fmt.Sprintf("a different model (banked %q, given %q)", banked.Model, model))
	}
	if banked.AsksSeen && !sameAsks(banked.Asks, asks) {
		parts = append(parts, fmt.Sprintf("different questions (banked %q, given %q)",
			strings.Join(banked.Asks, ", "), strings.Join(asks, ", ")))
	}
	return strings.Join(parts, " and ")
}

// asksLabel is the one wording for what a single lens says about its questions.
// Three states, never two: stated and comparable, stated and NOT comparable, and
// not stated at all.
func (l lensRecord) asksLabel() string {
	switch {
	case len(l.Asks) > 0 && len(l.AsksUnknown) > 0:
		return "asks " + strings.Join(l.Asks, "+") + " (+" + strings.Join(l.AsksUnknown, ", ") + " not comparable)"
	case len(l.Asks) > 0:
		return "asks " + strings.Join(l.Asks, "+")
	case l.AsksSeen:
		return "asks stated, none comparable: " + strings.Join(l.AsksUnknown, ", ")
	}
	return "asks not stated"
}

// askCoverage is what a round's roster says about the delta questions [R3], and
// it exists because the first version answered that question in three places
// with three different answers: `wf stats` gated on the round KIND and split
// stated from unstated, while the roster and `wf round record` did neither —
// so one workspace's round 11 was told "none asks closure, fix-induced, ripple"
// by two commands and "states no questions, UNRECORDED rather than uncovered"
// by a third. Two commands contradicting each other about one round is worse
// than either answer.
type askCoverage struct {
	Applicable   bool     // only a DELTA round is held to [R3]
	Valid        int      // lenses that count as a record at all
	Unstated     int      // of those, the ones with no `Asks:` line at all
	Uncomparable int      // of those, the ones whose `Asks:` line this build cannot read
	Missing      []string // required questions nothing asks — meaningful only when the two above are 0
}

// classifyAsks answers it once. A single lens whose questions are not on file
// makes the round's coverage UNRECORDED: what that vector asked is unknown, so
// "nothing asks ripple" is a claim the workspace cannot support.
func classifyAsks(kind string, ls []lensRecord) askCoverage {
	c := askCoverage{Applicable: kind == "delta"}
	asked := map[string]bool{}
	for _, l := range ls {
		if !l.Valid {
			// The ninth wave's scar: an UNCOMMITTED lens is not a record, so it
			// must not authorise coverage either.
			continue
		}
		c.Valid++
		if len(l.Asks) == 0 {
			// Two different claims, and the first version made both of them
			// the first: a header that says nothing, and a header that says
			// something this build cannot compare. Reporting the second as
			// "banked before `-asks` existed" is a provenance the classifier
			// cannot know, asserted about a file that states two questions on
			// the line above it in the same output.
			if l.AsksSeen {
				c.Uncomparable++
			} else {
				c.Unstated++
			}
			continue
		}
		for _, q := range l.Asks {
			asked[q] = true
		}
	}
	for _, q := range lensDeltaRequired {
		if !asked[q] {
			c.Missing = append(c.Missing, q)
		}
	}
	return c
}

// Note is the one line all three reports print, or "" when there is nothing to
// say. It is a NOTE everywhere, never a refusal: the coverage rule is new and
// every corpus that would have to satisfy it is closed.
func (c askCoverage) Note() string {
	if !c.Applicable || c.Valid == 0 {
		return ""
	}
	switch {
	case c.Unstated > 0 && c.Uncomparable > 0:
		return fmt.Sprintf("coverage UNRECORDED — of %d banked lens(es), %d state no questions and %d state questions this build cannot compare, so what this DELTA round asked cannot be read [R3]",
			c.Valid, c.Unstated, c.Uncomparable)
	case c.Unstated > 0:
		return fmt.Sprintf("coverage UNRECORDED — %d of %d banked lens(es) do not state their questions, so what this DELTA round asked cannot be read [R3]",
			c.Unstated, c.Valid)
	case c.Uncomparable > 0:
		return fmt.Sprintf("coverage UNRECORDED — %d of %d banked lens(es) state questions this build cannot compare, so what this DELTA round asked cannot be read [R3]",
			c.Uncomparable, c.Valid)
	}
	if len(c.Missing) == 0 {
		return ""
	}
	// The actually-missing set, not the required list: a roster asking two of
	// the three was reported as asking "none of" all three.
	return fmt.Sprintf("no banked lens asks %s — required of a DELTA round [R3]", strings.Join(c.Missing, ", "))
}

// lensRecord is one banked vector: what it was called, what ran it, and the
// file holding the prompt verbatim.
//
// Valid is the whole point of the type. The refusals in `wf freeze` and
// `wf close` are cleared by a lens, so whatever those refusals count is the
// escape from them — and the first version counted a FILENAME: `touch
// rounds/r1/lens-anything.md` cleared a refusal that had just fired, and the
// roster then printed a vector with no model, which is the one thing the write
// path refuses to record. The write path validated `-name`, `-model` and a
// non-blank prompt; the read path every consumer uses validated none of them.
type lensRecord struct {
	Name  string
	Model string
	File  string // relative to the round directory
	Valid bool
	Why   string // why it does not count, for a report that never skips in silence

	// Asks is the canonical question set this vector claims to cover. It is
	// required to WRITE a lens and absent from every lens banked before it
	// existed — an old record is reported as not stating its questions, never
	// invalidated by them: thirteen banked lens records live in two workspaces,
	// and the freeze and close refusals are cleared by exactly this record.
	Asks []string

	// AsksSeen records that an `Asks:` LINE was present, and AsksUnknown the
	// words on it this build cannot compare. The two are separate from Asks
	// because "banked before the question set existed" and "states questions
	// none of which are comparable" are different claims, and the first version
	// reported the second as the first — the absent-vs-measured-zero collapse,
	// in reverse. It is reachable whenever the closed set is NARROWED: every
	// lens banked under a removed word would silently claim to predate the flag.
	AsksSeen    bool
	AsksUnknown []string
}

func (l lensRecord) String() string {
	switch {
	case !l.Valid:
		return l.Name + " (NOT a record: " + l.Why + ")"
	case l.Model == "":
		return l.Name
	}
	return l.Name + " (" + l.Model + ", " + l.asksLabel() + ")"
}

// cmdRoundLens banks one reviewer vector against a round, or prints the roster.
func cmdRoundLens(args []string) error {
	fs := flag.NewFlagSet("round lens", flag.ExitOnError)
	name := fs.String("name", "", "what this vector is called in the round's findings (e.g. CRITERION)")
	// Required, not defaulted. "unknown" in a roster is the same claim as no
	// roster at all, and the model is the half of the record that measured
	// 1.76x on the gate this command comes from.
	model := fs.String("model", "", "what ran it — the model, and whether it was external (e.g. `external gpt-5.6-luna`)")
	// The comparable half of the record. `-name` is the round's own label and
	// stays free; this is what lets one gate's roster be read beside another's.
	// Repeatable AND a list, because a flag that takes a set must accept both
	// spellings of one: `-asks closure -asks ripple` and `-asks closure,ripple`
	// are the same claim, and the first was an error message.
	var asks askList
	fs.Var(&asks, "asks", "the canonical question(s) this vector asked; repeat the flag or separate with , + or a space: "+strings.Join(lensQuestions, ", "))
	// The default is the OPEN round, so banking a vector after the next round
	// has been opened lands it on the wrong one: pass -round N for the round
	// whose reviewers produced it.
	round := fs.Int("round", 0, "round to record against (default the open one — pass N when the next round is already open)")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	w, err := open()
	if err != nil {
		return err
	}
	n := *round
	if n == 0 {
		n = w.M.Round
	}
	if w.M.Round == 0 {
		return fmt.Errorf("no round is open; run `wf round new` first")
	}
	// A round that does not exist cannot have been reviewed. Unbounded, this
	// reported success into `rounds/r7/` of a one-round gate — and worse, a
	// lens banked against a round the gate has not reached yet CLEARS the
	// refusal for that round when it is finally opened and never dispatched,
	// which is the guard pre-authorising exactly what it exists to refuse.
	if n < 1 || n > w.M.Round {
		return fmt.Errorf("round %d does not exist in this workspace (rounds 1..%d); a lens can only record what reviewed a round that was opened", n, w.M.Round)
	}
	if len(pos) == 0 {
		// A missing prompt path with the flags set is a mistyped argument, not
		// a request for the roster: a shell that globbed the path away would
		// otherwise print somebody else's roster and exit 0.
		// len, not String(): the joined form of a one-element list holding the
		// empty string is "", so `-asks ""` was indistinguishable from the flag
		// being absent and silently printed somebody's roster.
		if strings.TrimSpace(*name) != "" || strings.TrimSpace(*model) != "" || len(asks) > 0 {
			return fmt.Errorf("no prompt file given, but -name/-model/-asks were: `wf round lens` with no file prints the roster and records nothing")
		}
		return printLensRoster(w, n)
	}
	if len(pos) > 1 {
		return fmt.Errorf("usage: wf round lens <prompt-file> -name NAME -model M -asks Q[,Q] [-round N]")
	}
	if strings.TrimSpace(*name) == "" || strings.TrimSpace(*model) == "" {
		return fmt.Errorf("-name and -model are both required: a roster entry that does not say WHAT ran the vector records that a round had lenses and not which, which is the claim the gate cannot check")
	}
	askedList, err := parseAsks(asks.String())
	if err != nil {
		return err
	}
	b, err := os.ReadFile(pos[0])
	if err != nil {
		// The argument is a FILE and the usage line used to call it `<prompt>`,
		// which reads as the text itself: a live gate pasted the prompt on the
		// command line twice and got `file name too long` from the syscall,
		// with nothing saying what was wanted. Text is deliberately NOT
		// accepted as a fallback — a prompt is banked verbatim and a shell
		// mangles one on the way in.
		//
		// The clause is attached only where the argument LOOKS like a pasted
		// prompt (too long for a filename, or carrying whitespace). A plain
		// typo'd path gets the plain error: telling an operator who mistyped a
		// path that they pasted a prompt accuses them of a mistake they did not
		// make, which is the same defect as a refusal pointing at the wrong
		// character.
		if errors.Is(err, syscall.ENAMETOOLONG) || strings.ContainsAny(pos[0], " \t\n") {
			return fmt.Errorf("%w; `wf round lens` takes the PATH of a file holding the prompt, not the prompt itself", err)
		}
		return err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return fmt.Errorf("%s is empty; a roster entry whose prompt is empty records that a lens ran and not what it was asked", pos[0])
	}
	// The `wf new-lens` scaffold writes everything that is true of every round
	// and leaves the AIM as one marker line. Banking it unfilled is the same
	// claim an empty prompt makes -- that a lens ran and not what it was asked
	// -- with the difference that this one is 40 lines long and reads like a
	// brief. `-name`, `-model` and `-asks` are each refused when they say
	// nothing; the prompt is the fourth field and had no such refusal.
	if bytes.Contains(b, []byte(lensAimMarker)) || bytes.Contains(b, []byte("<<<")) && bytes.Contains(b, []byte(">>>")) {
		return fmt.Errorf("%s still carries the `wf new-lens` marker line %q (or a reworded one — any prompt holding both `<<<` and `>>>` is refused, because an exact-byte check is walked past by an operator who EDITS the marker instead of replacing it): the scaffold's fixed half is banked and its VECTOR is not, which records that a lens ran and not what it was aimed at. Replace that line with the aim, or bank the prompt the vector actually received", pos[0], lensAimMarker)
	}
	// Same inference, same announcement as `wf round record`: the default is
	// the OPEN round, so banking a vector after the next round was opened lands
	// it on the wrong one, and the roster is what a later reading of the
	// descent is made of. Printed on the banking path only — the roster branch
	// above leads with `round N` already, and a second copy of one number is
	// this project's most repeated defect class.
	if *round == 0 {
		fmt.Printf("round %d (the open one; `-round N` banks against another)\n", n)
	}
	slug := lensSlug(*name)
	if slug == "" {
		return fmt.Errorf("-name %q has no ASCII letters or digits to name a file by; gate artifacts are English by workflow rule, and this is the same boundary the anchor grammar draws", *name)
	}
	dir := w.RoundDir(n)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("round %d has no directory in this workspace (%v); nothing was recorded", n, err)
	}
	rel := lensPrefix + slug + ".md"
	p := filepath.Join(dir, rel)
	body := fmt.Sprintf("# Lens %s\n\nModel: %s\nAsks: %s\nRecorded: %s\n%s\n%s",
		strings.TrimSpace(*name), strings.TrimSpace(*model), strings.Join(askedList, ", "),
		time.Now().UTC().Format(time.RFC3339), lensFence, string(b))
	wsRel, _ := filepath.Rel(w.Root, p)
	if old, err := os.ReadFile(p); err == nil {
		// A filled form is never overwritten — the findings lesson, applied to
		// the form that comes beside it. The prompt a round was ACTUALLY run
		// with is the thing this file exists to hold, and a second `wf round
		// lens` after the fact would rewrite it with the prompt the operator
		// wishes had run.
		banked := parseLens(old, rel)
		// A header-only change is NOT "unchanged". lensPrompt compares the bytes
		// after the fence, so correcting `-asks` on an unchanged prompt exited
		// zero, reported "unchanged — not re-committed", and left the old claim
		// standing as this round's coverage. The slug-collision case two blocks
		// down already refuses a differing name loudly; this is the same
		// situation and now gets the same answer.
		// Only a header that STATES the field can disagree with one. A lens
		// banked before `-asks` existed states nothing, so supplying it is an
		// ADDITION and not a rewrite — refusing there would strand every legacy
		// uncommitted lens with no way forward but a hand edit, which is the
		// hand `git commit` this command exists to remove.
		modelDiffers := banked.Model != "" && !strings.EqualFold(banked.Model, strings.TrimSpace(*model))
		asksDiffer := banked.AsksSeen && !sameAsks(banked.Asks, askedList)
		// An UNCOMMITTED file is not a record, so it cannot be the banked claim
		// this refusal protects — and refusing it strands exactly the case the
		// recommit branch below was written for: a commit that failed mid-command.
		if lensPrompt(old) == lensPrompt([]byte(body)) && (modelDiffers || asksDiffer) && tracked(w, wsRel) {
			return fmt.Errorf("round %d already records lens %s with the same prompt but %s; a banked record is not rewritten — correct it by hand in %s, or record the changed vector under its own -name",
				n, banked.Name, askDelta(banked, strings.TrimSpace(*model), askedList), rel)
		}
		if lensPrompt(old) == lensPrompt([]byte(body)) && !banked.AsksSeen {
			// The prompt is the same and the banked header states no questions,
			// so this is the ADDITION the exemption above describes — and the
			// first version merely SAID so: it took the "unchanged" branch,
			// exited zero and discarded the flag, leaving the record reading
			// `asks not stated`. Measured: all 13 lens records on disk predate
			// `-asks`, which is the entire population the exemption is for.
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				return err
			}
			sha, err := w.Git.Commit(fmt.Sprintf("round %d: lens %s asks %s", n, banked.Name, strings.Join(askedList, ", ")), []string{wsRel})
			if err != nil {
				return err
			}
			fmt.Printf("lens %s for round %d predates -asks; its questions are now on file (%s) — committed %s\n",
				banked.Name, n, strings.Join(askedList, ", "), short(sha))
			return nil
		}
		if lensPrompt(old) == lensPrompt([]byte(body)) {
			// Identical, but possibly never committed: a failed commit left the
			// file behind, and reporting "unchanged" then left it uncommitted
			// forever — while the roster reads the WORKTREE and freeze would
			// have counted it.
			if tracked(w, wsRel) {
				fmt.Printf("lens %s already recorded for round %d, unchanged — not re-committed\n", banked.Name, n)
				return nil
			}
			sha, err := w.Git.Commit(fmt.Sprintf("round %d: lens %s (%s)", n, banked.Name, banked.Model), []string{wsRel})
			if err != nil {
				return err
			}
			fmt.Printf("lens %s for round %d was on disk and uncommitted — committed %s\n", banked.Name, n, short(sha))
			return nil
		}
		if !strings.EqualFold(banked.Name, strings.TrimSpace(*name)) {
			// Two different names that slug the same file. Naming the REQUESTED
			// name as the banked one advised the operator to do what they had
			// just done.
			return fmt.Errorf("round %d already records lens %q in %s, and %q names the same file; give them names that differ in more than punctuation", n, banked.Name, rel, strings.TrimSpace(*name))
		}
		return fmt.Errorf("round %d already records lens %s (%s) with a different prompt; a banked vector is not rewritten — record the changed vector under its own -name", n, banked.Name, rel)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		return err
	}
	sha, err := w.Git.Commit(fmt.Sprintf("round %d: lens %s (%s)", n, strings.TrimSpace(*name), strings.TrimSpace(*model)), []string{wsRel})
	if err != nil {
		// The file is on disk and uncommitted; the next run of this command
		// commits it rather than reporting it as already banked.
		return err
	}
	fmt.Printf("recorded %s (%d bytes of prompt)\ncommitted %s\n", wsRel, len(b), short(sha))
	return nil
}

func printLensRoster(w *ws.Workspace, n int) error {
	ls := roundLenses(w, n)
	if len(ls) == 0 {
		// The population, said out loud. A round with no roster is not a round
		// that ran no lens; it is a round whose lenses nobody recorded, and the
		// two are the difference between an unfalsifiable claim and a measured
		// one.
		fmt.Printf("round %d — NO lens recorded; what reviewed this round is not in the workspace\n", n)
		return nil
	}
	fmt.Printf("round %d — %d lens(es), %d of them a record\n", n, len(ls), validLenses(ls))
	for _, l := range ls {
		if !l.Valid {
			fmt.Printf("  %-24s NOT a record: %s (%s)\n", l.Name, l.Why, l.File)
			continue
		}
		fmt.Printf("  %-24s %-28s %s\n", l.Name, l.Model, l.asksLabel())
	}
	// Coverage, as a note, and only where [R3] applies: the first version
	// printed "required of a DELTA round" on round 1 of every gate, which is a
	// FULL round the manual gives three different vectors — the sixth wave's
	// own class, a note nobody can clear is a note nobody reads.
	if n := classifyAsks(roundKinds(w)[n], ls).Note(); n != "" {
		fmt.Printf("  note: %s\n", n)
	}
	return nil
}

// roundLenses reads the round's banked roster. It parses the files rather than
// keeping an index: an index is a second copy of a derivation, and this project
// names that as its most-repeated defect class.
func roundLenses(w *ws.Workspace, n int) []lensRecord {
	dir := w.RoundDir(n)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []lensRecord
	for _, e := range ents {
		if e.IsDir() || !strings.HasPrefix(e.Name(), lensPrefix) || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			out = append(out, lensRecord{Name: e.Name(), File: e.Name(), Why: "cannot be read (" + err.Error() + ")"})
			continue
		}
		l := parseLens(b, e.Name())
		// A lens that is not COMMITTED is not part of what the freeze pins. The
		// roster reads the worktree, and an uncommitted file would clear a
		// refusal and then not exist in the tag the refusal was protecting.
		rel := filepath.Join(filepath.Base(dir), e.Name())
		if l.Valid && !tracked(w, filepath.Join("rounds", rel)) {
			l.Valid, l.Why = false, "not committed"
		}
		out = append(out, l)
	}
	return out
}

// parseLens reads one banked vector. The header is the text BEFORE the fence,
// so a prompt quoting `Model:` or `# Lens ` cannot rewrite the record of what
// ran the vector — the prompt is data, and this file is the claim about it.
func parseLens(b []byte, file string) lensRecord {
	l := lensRecord{File: file}
	text := string(b)
	head, prompt, fenced := strings.Cut(text, lensFence)
	for _, line := range strings.Split(head, "\n") {
		switch {
		case strings.HasPrefix(line, "# Lens "):
			if l.Name == "" {
				l.Name = strings.TrimSpace(strings.TrimPrefix(line, "# Lens "))
			}
		case strings.HasPrefix(line, "Model:"):
			if l.Model == "" {
				l.Model = strings.TrimSpace(strings.TrimPrefix(line, "Model:"))
			}
		case strings.HasPrefix(line, "Asks:"):
			// Unknown spellings are KEPT and reported rather than rejected or
			// dropped in silence: this is the read path, it must keep reading
			// the rosters banked before the question set existed, and it must
			// never let an uncomparable word count as coverage NOR let a header
			// that said something read as a header that said nothing.
			if l.AsksSeen {
				break // the first `Asks:` line wins, as the first `Model:` does
			}
			l.AsksSeen = true
			seen := map[string]bool{}
			for _, part := range strings.Split(strings.TrimPrefix(line, "Asks:"), ",") {
				q := strings.ToLower(strings.TrimSpace(part))
				if q == "" || seen[q] {
					continue
				}
				seen[q] = true
				if knownLensQuestion(q) {
					l.Asks = append(l.Asks, q)
				} else {
					l.AsksUnknown = append(l.AsksUnknown, q)
				}
			}
		}
	}
	if l.Name == "" {
		// Named by its file so a report can point at it — but NOT counted: a
		// file nobody wrote a header into records that something exists, not
		// that a vector ran.
		l.Name = strings.TrimSuffix(strings.TrimPrefix(file, lensPrefix), ".md")
	}
	switch {
	case !fenced:
		l.Why = "no `---` fence, so it holds no prompt"
	case strings.TrimSpace(head) == "" || !strings.Contains(head, "# Lens "):
		l.Why = "no `# Lens` header, so it does not say which vector it is"
	case l.Model == "":
		l.Why = "no `Model:` line, so it does not say what ran the vector"
	case strings.TrimSpace(prompt) == "":
		l.Why = "no prompt after the fence"
	default:
		l.Valid = true
	}
	return l
}

// tracked reports whether a workspace-relative path is committed.
func tracked(w *ws.Workspace, rel string) bool {
	files, err := w.Git.Files()
	if err != nil {
		return false
	}
	for _, f := range files {
		if f == filepath.ToSlash(rel) {
			return true
		}
	}
	return false
}

func validLenses(ls []lensRecord) int {
	n := 0
	for _, l := range ls {
		if l.Valid {
			n++
		}
	}
	return n
}

// lensList renders a roster for a one-line report.
func lensList(ls []lensRecord) string {
	s := make([]string, len(ls))
	for i, l := range ls {
		s[i] = l.String()
	}
	return strings.Join(s, ", ")
}

// lensPrompt is the banked PROMPT alone, which is what decides whether a
// re-record is the same vector. Comparing the whole file made every re-record
// differ by its timestamp; stripping every `Recorded:` line instead made two
// DIFFERENT prompts compare equal whenever they differed only in such a line,
// and the command then reported the new one as already on file.
func lensPrompt(b []byte) string {
	_, prompt, ok := strings.Cut(string(b), lensFence)
	if !ok {
		return string(b)
	}
	return prompt
}

func lensSlug(name string) string {
	var b strings.Builder
	prev := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prev = false
		default:
			if !prev && b.Len() > 0 {
				b.WriteByte('-')
				prev = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
