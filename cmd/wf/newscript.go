package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	wfgate "git.sumka.site/yakov/wfgate"
)

// cmdNewScript emits the harness half of an orchestration script.
//
// Measured across the seven real scripts of one repository (11 662 lines): the
// blocks whose names recur in five or more of them — the spawn wrapper, the
// findings/producer/fix schemas, the bar vocabulary, the per-section gate —
// hold about a quarter of the lines, and they are RE-AUTHORED rather than
// copied: two scripts' gate functions implement the same algorithm at 0.05
// textual similarity. That is also where the round-1 defects sit: on one script
// gate, four of eight blockers and nine of eighteen findings were harness shape
// rather than task content.
//
// What it does NOT claim: that the result is reviewed, or that a green
// `wfcheck` means a good script. The first drafts of two real gates were
// already wfcheck-clean and still cost a round each. What the skeleton carries
// is the forms wfcheck cannot check — the liveness ledger counting only
// unconditional spawns, the read-only final phase, the budget-guarded fix
// round, the positive bar question — each of which is a defect this project
// paid for once.
func cmdNewScript(args []string) error {
	fs := flag.NewFlagSet("new-script", flag.ExitOnError)
	target := fs.String("target", "", "the repository the script will run in (default: the workspace's target, else the working directory)")
	out := fs.String("o", "", "write here (default: <name>.mjs beside you)")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: wf new-script <name> [-target REPO] [-o FILE]")
	}
	name := pos[0]
	if strings.ContainsAny(name, `/\`+"\x00") || name == "." || name == ".." {
		return fmt.Errorf("name %q must be a plain identifier, not a path", name)
	}

	repo := *target
	if repo == "" {
		// A workspace knows its target, and the target is the one tree the
		// script runs in. Falling back to the working directory is right only
		// when there is no workspace to ask.
		if w, err := open(); err == nil {
			repo = w.M.Target
		} else if cwd, err := os.Getwd(); err == nil {
			repo = cwd
		}
	}

	dst := *out
	if dst == "" {
		dst = name + ".mjs"
	}
	// Never over a file that exists. A skeleton written over an authored script
	// is the whole-file overwrite `wf add` refuses for the same reason.
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("%s exists — name another file with -o; a skeleton must never land on an authored script", dst)
	}

	b, err := wfgate.Templates.ReadFile("templates/script.mjs")
	if err != nil {
		return err
	}
	body := strings.ReplaceAll(string(b), "SCRIPT_NAME", name)
	body = strings.ReplaceAll(body, "TARGET_REPO", repo)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil && filepath.Dir(dst) != "." {
		return err
	}
	if err := os.WriteFile(dst, []byte(body), 0o644); err != nil {
		return err
	}

	todos := strings.Count(body, "TODO")
	fmt.Printf("wrote %s — %d line(s), %d TODO(s)\n", dst, strings.Count(body, "\n"), todos)
	fmt.Printf("  the harness is complete and passes both tools as it stands; the sections are yours\n")
	fmt.Printf("  read `wf docs authoring` before the first edit, then:\n")
	fmt.Printf("    wfcheck -root %s %s\n", repo, dst)
	fmt.Printf("    wfdry %s\n", dst)
	// Only the TODOs inside YOU OWN blocks reach a check, as missing paths. The
	// rest are prose, and a TODO left in a prompt SHIPS: saying so is the point,
	// because the tool being green over it is exactly what makes it dangerous.
	fmt.Printf("  two of those TODOs stand as wfcheck WARNs (missing owned paths); the other %d are PROSE that no check can see, and a TODO left in a prompt ships\n", todos-2)
	return nil
}
