package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git.sumka.site/yakov/wfgate/internal/ws"
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
}

func (l lensRecord) String() string {
	switch {
	case !l.Valid:
		return l.Name + " (NOT a record: " + l.Why + ")"
	case l.Model == "":
		return l.Name
	}
	return l.Name + " (" + l.Model + ")"
}

// cmdRoundLens banks one reviewer vector against a round, or prints the roster.
func cmdRoundLens(args []string) error {
	fs := flag.NewFlagSet("round lens", flag.ExitOnError)
	name := fs.String("name", "", "what this vector is called in the round's findings (e.g. CRITERION)")
	// Required, not defaulted. "unknown" in a roster is the same claim as no
	// roster at all, and the model is the half of the record that measured
	// 1.76x on the gate this command comes from.
	model := fs.String("model", "", "what ran it — the model, and whether it was external (e.g. `external gpt-5.6-sol`)")
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
		if strings.TrimSpace(*name) != "" || strings.TrimSpace(*model) != "" {
			return fmt.Errorf("no prompt file given, but -name/-model were: `wf round lens` with no file prints the roster and records nothing")
		}
		return printLensRoster(w, n)
	}
	if len(pos) > 1 {
		return fmt.Errorf("usage: wf round lens <prompt-file> -name NAME -model M [-round N]")
	}
	if strings.TrimSpace(*name) == "" || strings.TrimSpace(*model) == "" {
		return fmt.Errorf("-name and -model are both required: a roster entry that does not say WHAT ran the vector records that a round had lenses and not which, which is the claim the gate cannot check")
	}
	b, err := os.ReadFile(pos[0])
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return fmt.Errorf("%s is empty; a roster entry whose prompt is empty records that a lens ran and not what it was asked", pos[0])
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
	body := fmt.Sprintf("# Lens %s\n\nModel: %s\nRecorded: %s\n%s\n%s",
		strings.TrimSpace(*name), strings.TrimSpace(*model),
		time.Now().UTC().Format(time.RFC3339), lensFence, string(b))
	wsRel, _ := filepath.Rel(w.Root, p)
	if old, err := os.ReadFile(p); err == nil {
		// A filled form is never overwritten — the findings lesson, applied to
		// the form that comes beside it. The prompt a round was ACTUALLY run
		// with is the thing this file exists to hold, and a second `wf round
		// lens` after the fact would rewrite it with the prompt the operator
		// wishes had run.
		banked := parseLens(old, rel)
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
		fmt.Printf("  %-24s %s\n", l.Name, l.Model)
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
