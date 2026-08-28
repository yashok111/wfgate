package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"git.sumka.site/yakov/wfgate/internal/ws"
)

// closeDir holds the gate's own record of itself: written into the workspace,
// committed, and the only thing `wf prune` leaves behind.
//
// The proportions are the whole argument. On the three closed gates the .git
// directory is 3.8-4.4 MB — 61-84% of the workspace — and the entire history
// anyone has ever read out of it is the commit times and subjects: 8.7, 10.2
// and 11.9 KB of text. The snapshot is ~400x smaller than the thing it replaces
// and loses nothing that has been used. Deleting the repository without writing
// it first is what destroys the only record of what a gate COST.
const closeDir = "close"

// closeFeedbackSections are fixed and in this order because a corpus has to be
// greppable across gates. Three real post-mortems exist and each invented its
// own structure; the section that turned out to be the most valuable — the one
// about the process rather than the tool — is called "One observation about the
// process, not the tool" in two of them and "One number for the planning table"
// in the third, so the corpus cannot be searched for its own best section.
var closeFeedbackSections = []string{
	"1. What the TOOL got wrong",
	"2. What the MANUAL got wrong",
	"3. What cost a round, and why",
	"4. What these numbers say that the previous gates did not",
	"5. One observation about the process, not the tool",
}

func cmdClose(args []string) error {
	fs := flag.NewFlagSet("close", flag.ExitOnError)
	archive := fs.String("archive", os.Getenv("WF_ARCHIVE"),
		"also copy the snapshot into DIR/<name>/ (default $WF_ARCHIVE)")
	// Banked by DEFAULT. Their CONTENT is the one thing prune destroys that
	// nothing else holds, and a record that omits what the gate was ABOUT is a
	// record of the bookkeeping only. `-artifacts=false` opts out for a gate
	// whose artifacts live somewhere that outlives the workspace; prune then
	// names each of them before it removes them.
	withArts := fs.Bool("artifacts", true,
		"bank the artifacts themselves (default true; -artifacts=false keeps only history, stats, rounds and the feedback form)")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("unexpected argument %q", pos[0])
	}
	w, err := open()
	if err != nil {
		return err
	}

	// Every refusal before a single byte is written. `wf freeze` wrote and
	// committed its manifest BEFORE refusing to re-freeze, and the refused
	// command had already blessed what it refused; the order is the fix.
	if w.M.Round == 0 {
		return fmt.Errorf("no rounds yet — there is nothing to close")
	}
	last := w.M.Round
	if !w.Git.TagExists(ws.RoundTag(last)) {
		return fmt.Errorf("round %d is open — freeze it before closing the gate (a snapshot of an unfrozen round records a set nobody reviewed)", last)
	}
	// The REVIEWED material only. close/ is what this command is about to write,
	// and the prescribed flow is close -> fill feedback.md -> close again to bank
	// it: a check that refused its own output would make the second close
	// impossible and push the operator to a hand commit, which is the failure
	// `wf add` and `wf round record` both exist to remove.
	if changes, err := w.Git.Dirty("artifacts", "rounds"); err != nil {
		return err
	} else if len(changes) > 0 {
		var names []string
		for _, c := range changes {
			names = append(names, c.String())
		}
		return fmt.Errorf("artifacts or rounds are not committed, so the snapshot would record a state nobody reviewed:\n  %s", strings.Join(names, "\n  "))
	}
	if err := closeBlockersOpen(w, last); err != nil {
		return err
	}

	dir := filepath.Join(w.Root, closeDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// The commit log as TEXT. This is the file that exists because `git log`
	// will not: the timings that price every round live here and nowhere else.
	hist, err := w.Git.Run("log", "--reverse", "--format=%at%x09%H%x09%s")
	if err != nil {
		return err
	}
	written := []string{}
	put := func(rel string, b []byte) error {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return err
		}
		written = append(written, filepath.Join(closeDir, rel))
		return nil
	}
	if err := put("history.tsv", []byte(hist+"\n")); err != nil {
		return err
	}

	var stats bytes.Buffer
	if err := statsReport(w, true, &stats); err != nil {
		return err
	}
	if err := put("stats.txt", stats.Bytes()); err != nil {
		return err
	}

	// EVERY file of every round, not just findings.md. `wf delta -save` commits
	// the round's brief into rounds/rN/brief.md — the actual input the next
	// round's reviewer was handed — and prune deletes rounds/, so copying one
	// file out of the directory destroyed the rest. The rule two paragraphs
	// down says what is not reconstructable must be copied before it; this is
	// that rule applied to its own violation.
	for n := 1; n <= last; n++ {
		dir := w.RoundDir(n)
		ents, err := os.ReadDir(dir)
		// A `continue` here is the vacuous skip this repository has a scar
		// from: freeze passed a findings file it could not read and printed its
		// success block. A round the snapshot cannot read is a round `wf prune`
		// would then delete with no record of it anywhere.
		if err != nil {
			return fmt.Errorf("round %d cannot be read (%v) — the snapshot would be missing it and prune would delete the original", n, err)
		}
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			if err := put(filepath.Join("rounds", fmt.Sprintf("r%d", n), e.Name()), b); err != nil {
				return err
			}
		}
	}

	if *withArts {
		// A partial bank is worse than none: the operator would believe the
		// content is kept and prune would take the originals anyway.
		arts, err := w.ArtifactList()
		if err != nil {
			return err
		}
		for _, a := range arts {
			b, err := os.ReadFile(filepath.Join(w.Root, a))
			if err != nil {
				return fmt.Errorf("artifact %s cannot be read (%v) — a partial bank is worse than none", a, err)
			}
			if err := put(a, b); err != nil {
				return err
			}
		}
	}

	// The freeze manifests, as one table. Their artifact hashes stop meaning
	// anything the moment the artifacts go, but the rest does not: the target
	// revision each round was verified against, the tool version that verified
	// it, the size of the corpus each round froze, and the anchored share whose
	// DIRECTION is the ratchet — an author who
	// stopped anchoring writes citations the gate cannot really check.
	var fz strings.Builder
	// `refreezes` and `supersedes` are here because this table is what survives
	// `wf prune` as the readable summary of the manifests: a round published
	// four times under one tag read as frozen once, and the reviewers of the
	// earlier publications read different bytes.
	fz.WriteString("round\tfrozen\tcommit\ttargetRef\ttool\tciteAnchored\tciteTotal\tartifactLines\trefreezes\tsupersedes\n")
	for n := 1; n <= last; n++ {
		b, err := os.ReadFile(filepath.Join(w.RoundDir(n), "freeze.json"))
		if err != nil {
			continue
		}
		// The manifest is read through its OWN type, never a hand copy of it.
		// This block used to retype ten of its fields inline, and the wave that
		// added `refreezes`/`supersedes` to freezeManifest did not touch it: an
		// anonymous struct compiles fine with a field missing, so the record
		// that SURVIVES `wf prune` lost the count for a release. What the TSV
		// drops stays an explicit projection below — `artifacts` and `target`
		// are left out on purpose — but the reading is one decision.
		var m freezeManifest
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		fmt.Fprintf(&fz, "%d\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%s\n",
			m.Round, m.Frozen, m.Commit, m.TargetRef, m.Tool, m.CiteAnchored, m.CiteTotal, m.ArtifactLines,
			m.Refreezes, strings.Join(m.Supersedes, " "))
	}
	if err := put("freezes.tsv", []byte(fz.String())); err != nil {
		return err
	}

	// The scaffold is NOT overwritten. Findings taught this: a form the operator
	// has filled is the one thing a command must never replace, and re-closing
	// a gate after a post-gate edit is a normal thing to do.
	fbRel := "feedback.md"
	fbPath := filepath.Join(dir, fbRel)
	kept := false
	if _, err := os.Stat(fbPath); err == nil {
		kept = true
		written = append(written, filepath.Join(closeDir, fbRel))
	} else if err := put(fbRel, closeFeedback(w, &stats)); err != nil {
		return err
	}

	sort.Strings(written)
	sha, err := w.Git.Commit(fmt.Sprintf("gate: close %s after %d round(s)\n\nwf-close: %d file(s), wf %s\n",
		w.M.Name, last, len(written), version), written)
	if err != nil {
		return err
	}

	fmt.Printf("closed %s — %d round(s)\n", w.M.Name, last)
	// The stop rule reads the REVIEWER descent, so a gate whose LAST round
	// dispatched none did not end on the rule; it stopped next to it.
	if roundKinds(w)[last] == "tool" {
		fmt.Printf("  note: round %d is a `tool` round — this gate's last round dispatched no reviewer, and the stop rule reads the REVIEWER descent\n", last)
	}
	for _, f := range written {
		fmt.Printf("  %s\n", f)
	}
	if kept {
		fmt.Printf("  note: feedback.md already existed and was kept — a filled form is never overwritten\n")
	} else {
		fmt.Printf("  next: fill %s/feedback.md, then re-run `wf close` to bank it\n", closeDir)
	}
	fmt.Printf("  committed %s\n", short(sha))

	if *archive != "" {
		dst := filepath.Join(*archive, w.M.Name)
		// A WARNING, not an error: the record is written and committed by now,
		// and returning non-zero would report a command that succeeded as one
		// that failed — after which the operator's natural move is to re-run it.
		if err := copyTree(dir, dst); err != nil {
			fmt.Printf("  WARNING: the record is committed, but archiving to %s failed: %v\n", dst, err)
		} else {
			fmt.Printf("  archived to %s\n", dst)
		}
	}
	return nil
}

// closeBlockersOpen refuses a close while a BLOCKER of the last frozen round
// stands without a closure. `wf freeze` makes the same check for the round it
// closes; a gate whose last round still carries one was not finished, it was
// abandoned, and the record must not call that "closed".
func closeBlockersOpen(w *ws.Workspace, round int) error {
	b, err := os.ReadFile(filepath.Join(w.RoundDir(round), "findings.md"))
	if err != nil {
		return fmt.Errorf("round %d has no findings file to read: %w", round, err)
	}
	rows := ws.ParseFindings(b)
	if len(rows) == 0 {
		return fmt.Errorf("round %d's findings table did not parse — a ledger the tool cannot read must not be banked as one it read", round)
	}
	// The gate ends on a REVIEWER round that produces no BLOCKER, and on nothing
	// else [R1] — so the last round's ledger is the evidence the close rests on,
	// and an untouched scaffold is not evidence. Measured: a live gate's round 9
	// was opened, frozen and left undispatched, and `wf stats` printed it as `0
	// finding(s), 0 blocker(s)`, which is exactly what the round that ends a
	// gate prints; when it was finally reviewed it returned four blockers. EVERY
	// round is checked, not only the last: `wf round new` does not require the
	// current round to be frozen, so a round opened past without one is seen by
	// no freeze. A `-kind tool` round is exempt — its opening subject already
	// records that no reviewer was dispatched.
	if unread := unreadRounds(w, round, roundKinds(w)); len(unread) > 0 {
		return fmt.Errorf("round(s) %s carry an UNFILLED findings scaffold and record no lens, so this close would bank a round nobody is recorded as having read.\n  If its vectors ran and found nothing, bank them: `wf round lens <prompt-file> -name N -model M -asks Q -round N`.\n  If they found something, record it: `wf round record <file> -round N`", roundList(unread))
	}
	var open []string
	for _, r := range rows {
		if r.Malformed {
			return fmt.Errorf("round %d has a ledger row that is not four cells; fix it before closing", round)
		}
		if r.Placeholder() || !r.Blocker() {
			continue
		}
		if strings.TrimSpace(r.ClosedBy) == "" {
			open = append(open, r.Text)
		}
	}
	if len(open) > 0 {
		return fmt.Errorf("round %d still has %d BLOCKER(s) with no `closed by`:\n  %s", round, len(open), strings.Join(open, "\n  "))
	}
	return nil
}

// closeFeedback prefills what the tool knows and leaves the rest as headings.
// Measured need: three post-mortems were written by hand, and the first of them
// closed its gate on 99 findings and 27 blockers against a true 98 and 26 —
// the numbers are the half a form should never ask a human to retype.
func closeFeedback(w *ws.Workspace, stats *bytes.Buffer) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# Gate feedback — %s\n\n", w.M.Name)
	b.WriteString("*Scaffold written by `wf close`. Everything below the fence is DERIVED — do not\n")
	b.WriteString("retype it. The sections are fixed so that a corpus of gates can be searched by\n")
	b.WriteString("them. An empty section and a section with nothing to report are different\n")
	b.WriteString("claims: write \"nothing\" rather than leaving one blank.*\n\n")
	fmt.Fprintf(&b, "Target: `%s`\nTool: wf %s\nClosed: %s\n\n", w.M.Target, version, time.Now().UTC().Format(time.RFC3339))
	b.WriteString("## What this run was\n\n```\n")
	b.WriteString(strings.TrimRight(stats.String(), "\n"))
	b.WriteString("\n```\n\n")
	for _, s := range closeFeedbackSections {
		fmt.Fprintf(&b, "## %s\n\n\n", s)
	}
	return []byte(b.String())
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
}

func cmdPrune(args []string) error {
	fs := flag.NewFlagSet("prune", flag.ExitOnError)
	yes := fs.Bool("yes", false, "actually delete; without it prune only reports what it would remove")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("unexpected argument %q", pos[0])
	}
	w, err := open()
	if err != nil {
		return err
	}

	dir := filepath.Join(w.Root, closeDir)
	if _, err := os.Stat(filepath.Join(dir, "history.tsv")); err != nil {
		return fmt.Errorf("no snapshot at %s — run `wf close` first. Deleting the repository is what destroys the timings, and they exist nowhere else", closeDir)
	}
	if changes, err := w.Git.Dirty("artifacts", "rounds", closeDir); err != nil {
		return err
	} else if len(changes) > 0 {
		return fmt.Errorf("artifacts, rounds or the record are not committed — run `wf close` again before pruning")
	}
	// A snapshot taken before the last edit is a snapshot of a different gate.
	stale, err := w.Git.Run("log", "--format=%s", "-1", "--", "artifacts", "rounds")
	if err == nil && stale != "" {
		lastClose, _ := w.Git.Run("log", "--format=%H", "-1", "--", closeDir)
		lastWork, _ := w.Git.Run("log", "--format=%H", "-1", "--", "artifacts", "rounds")
		if lastClose != "" && lastWork != "" {
			// A commit is its own ancestor, so `--is-ancestor X X` exits 0: ONE
			// commit touching both rounds/ and close/ — which is what a hand
			// `git commit -a` after filling feedback.md produces — walked
			// straight through this guard into an irreversible delete, with the
			// snapshot holding the pre-edit rows. Same-commit is not older.
			if lastWork == lastClose {
				return fmt.Errorf("the last commit touched artifacts or rounds AND the record, so the record cannot be older than what it describes — run `wf close` again before pruning")
			}
			if _, err := w.Git.Run("merge-base", "--is-ancestor", lastWork, lastClose); err != nil {
				return fmt.Errorf("artifacts or rounds changed after the last `wf close` — re-run it, or the snapshot records a gate that no longer exists")
			}
		}
	}

	// rounds/ goes too: every file of every round is in the snapshot verbatim.
	// What is NOT reconstructable must be copied before this line, not left
	// behind by it.
	//
	// The repository is removed LAST. Removing it first and then failing on
	// artifacts/ or rounds/ — EACCES, EBUSY, a full disk — leaves tracked
	// content gone with nothing left to restore it from; in this order every
	// intermediate state is still a `git checkout .` away.
	victims := []string{"artifacts", "rounds", ".git"}
	var present []string
	for _, v := range victims {
		if _, err := os.Stat(filepath.Join(w.Root, v)); err == nil {
			present = append(present, v)
		}
	}
	if len(present) == 0 {
		fmt.Printf("nothing to prune — %s is already reduced to its record\n", w.M.Name)
		return nil
	}
	fmt.Printf("prune %s — the snapshot in %s/ is what survives\n", w.M.Name, closeDir)
	for _, v := range present {
		fmt.Printf("  would remove %s (%s)\n", v, dirSize(filepath.Join(w.Root, v)))
	}
	// The artifacts are the one thing here that the record does NOT hold, and
	// once the repository is gone their CONTENT exists nowhere. Naming them by
	// name is the difference between a decision and an accident; `wf close
	// -artifacts` banks them for a gate that wants them kept.
	if arts, err := w.ArtifactList(); err == nil && len(arts) > 0 {
		if _, err := os.Stat(filepath.Join(dir, "artifacts")); err != nil {
			fmt.Printf("  the record does NOT hold the artifacts themselves — these leave the world:\n")
			for _, a := range arts {
				fmt.Printf("    %s\n", a)
			}
			fmt.Printf("  (`wf close -artifacts` banks them; findings, history, freezes and stats are kept either way)\n")
		}
	}
	if !*yes {
		fmt.Printf("  nothing removed — re-run with -yes. This is not reversible and the repository is not recoverable\n")
		return nil
	}
	for _, v := range present {
		if err := os.RemoveAll(filepath.Join(w.Root, v)); err != nil {
			return err
		}
		fmt.Printf("  removed %s\n", v)
	}
	fmt.Printf("  %s is now its own record: %s/, gate.json\n", w.M.Name, closeDir)
	return nil
}

func dirSize(p string) string {
	var n int64
	_ = filepath.Walk(p, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			n += fi.Size()
		}
		return nil
	})
	switch {
	case n > 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n > 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
