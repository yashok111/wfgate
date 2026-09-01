package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	wfgate "git.sumka.site/yakov/wfgate"
	"git.sumka.site/yakov/wfgate/internal/ws"
)

// lensAimMarker is the one line the operator MUST replace. `wf round lens`
// refuses to bank a prompt still carrying it: a scaffold banked unfilled
// records that a round had a lens and not what it was asked, which is the claim
// the roster exists to make and the one `-name`/`-model`/`-asks` already refuse
// in their own halves.
const lensAimMarker = "<<< WRITE YOUR VECTOR HERE"

// cmdNewLens emits the fixed half of a reviewer's brief.
//
// Measured over the 309 lens prompts banked across the eleven workspaces that
// carry any (16 120 non-blank lines, median 55 per prompt): the lines recurring
// in five or more DISTINCT prompts hold 32.8% of them, and in ten or more,
// 22.0%. That is the same share, by the same reading, that justified
// `wf new-script` over the six tracked orchestration scripts — and it is the
// same failure, because the recurring third is the manual RE-AUTHORED by hand
// rather than pointed at: 200 of the 309 name `gate-checks` and exactly ONE
// names `wf docs`. So the copies go stale in place. 26 of them carry the
// severity scale's NOTE line in a wording the manual replaced, and no check can
// see it: `wf check dup` runs over artifacts, and a lens prompt is not one.
//
// Hence what this template does NOT do: restate the scale. It POINTS at the
// `gate-checks.md` that `wf init` pinned into the workspace — the copy that
// gate's reviewers were actually briefed from — and names the three sections to
// read. A rule that lives in one place cannot drift from itself.
//
// And what it deliberately leaves EMPTY is the vector. A gate's blocker has
// come from the lens whose instruction was under-specified beside the ones the
// round's findings aimed; a scaffold that wrote the aim would close exactly
// that. The fixed half is what is true of every round, and nothing else.
func cmdNewLens(args []string) error {
	fs := flag.NewFlagSet("new-lens", flag.ExitOnError)
	round := fs.Int("round", 0, "the round this lens reads (default: the workspace's open round)")
	kind := fs.String("kind", "", "delta|full (default: full for round 1, delta after)")
	asks := fs.String("asks", "", "the question set this vector will claim, for the reading list it implies")
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
	if n < 1 {
		return fmt.Errorf("no round is open and -round was not given")
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
	if *asks != "" {
		if _, err := parseAsks(*asks); err != nil {
			return err
		}
	}

	dst := *out
	if dst == "" {
		dst = "lens-" + name + ".md"
	}
	// NEVER into rounds/rN/ by default, and the reason is a refusal this
	// repository already measured from the other end: a FILE in the round
	// directory is what clears the freeze and close refusals that demand a
	// round be read by somebody — `touch rounds/r1/lens-anything.md` cleared
	// one that had just fired. A scaffold written there would clear it before
	// the lens has run, which is the false green in its purest form. The
	// prompt lives beside the operator; `wf round lens <file>` banks it AFTER
	// the vector has returned, which is the moment the record becomes true.
	if d := filepath.Dir(dst); filepath.Clean(d) == filepath.Clean(w.RoundDir(n)) {
		return fmt.Errorf("refusing to write into %s: a file there is what clears the freeze and close refusals that demand a round was read, and this prompt is not a record until `wf round lens` banks it after the vector returns", w.RoundDir(n))
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
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	fmt.Printf("wrote %s — %d line(s), round %d, kind %s\n", dst, strings.Count(body, "\n"), n, k)
	fmt.Printf("  the fixed half only: the scale, the reading list and the output contract\n")
	fmt.Printf("  WRITE THE VECTOR — the marker line is the aim, and `wf round lens` refuses to bank a prompt still carrying it\n")
	fmt.Printf("  then, after the lens has returned:\n")
	if *asks != "" {
		fmt.Printf("    wf round lens %s -name %s -model sonnet -asks %s -round %d\n", dst, name, *asks, n)
	} else {
		fmt.Printf("    wf round lens %s -name %s -model sonnet -asks <Q,Q> -round %d\n", dst, name, n)
	}
	return nil
}

func lensKindPhrase(kind string) string {
	if kind == "delta" {
		return "This is a DELTA round: you do NOT re-read the whole artifact. You read the diff\nand the brief, and your findings say what you did not re-read."
	}
	return "This is a FULL round: no earlier round has read this artifact, so it is read\nwhole."
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
// it. The brief is named only when it EXISTS: a reading list naming a file
// nobody wrote is what one gate's reviewer reconstructed from the tag diff and
// then reported as a finding, and it is why `wf delta` now says on stderr that
// the round holds no brief.
func lensReading(w *ws.Workspace, n int, kind string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s\n", filepath.Join(w.Root, "gate-checks.md"))
	fmt.Fprintf(&b, "      sections 0, 0b and 0c — the scale, the questions, what a row must carry\n")

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
