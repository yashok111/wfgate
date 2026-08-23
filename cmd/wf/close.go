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
	if err := os.MkdirAll(filepath.Join(dir, "findings"), 0o755); err != nil {
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

	for n := 1; n <= last; n++ {
		b, err := os.ReadFile(filepath.Join(w.RoundDir(n), "findings.md"))
		if err != nil {
			continue
		}
		if err := put(filepath.Join("findings", fmt.Sprintf("r%d.md", n)), b); err != nil {
			return err
		}
	}

	// The freeze manifests, as one table. Their artifact hashes stop meaning
	// anything the moment the artifacts go, but the rest does not: the target
	// revision each round was verified against, the tool version that verified
	// it, and the anchored share whose DIRECTION is the ratchet — an author who
	// stopped anchoring writes citations the gate cannot really check.
	var fz strings.Builder
	fz.WriteString("round\tfrozen\tcommit\ttargetRef\ttool\tciteAnchored\tciteTotal\n")
	for n := 1; n <= last; n++ {
		b, err := os.ReadFile(filepath.Join(w.RoundDir(n), "freeze.json"))
		if err != nil {
			continue
		}
		var m struct {
			Round        int    `json:"round"`
			Commit       string `json:"commit"`
			TargetRef    string `json:"targetRef"`
			Tool         string `json:"tool"`
			Frozen       string `json:"frozen"`
			CiteAnchored int    `json:"citeAnchored"`
			CiteTotal    int    `json:"citeTotal"`
		}
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		fmt.Fprintf(&fz, "%d\t%s\t%s\t%s\t%s\t%d\t%d\n",
			m.Round, m.Frozen, m.Commit, m.TargetRef, m.Tool, m.CiteAnchored, m.CiteTotal)
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
		if err := copyTree(dir, dst); err != nil {
			return fmt.Errorf("archive: %w", err)
		}
		fmt.Printf("  archived to %s\n", dst)
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
			out, err := w.Git.Run("merge-base", "--is-ancestor", lastWork, lastClose)
			_ = out
			if err != nil {
				return fmt.Errorf("artifacts or rounds changed after the last `wf close` — re-run it, or the snapshot records a gate that no longer exists")
			}
		}
	}

	// rounds/ goes too: every findings.md is in the snapshot verbatim and every
	// freeze.json's surviving half is in freezes.tsv. What is NOT reconstructable
	// must be copied before this line, not left behind by it.
	victims := []string{".git", "artifacts", "rounds"}
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
