package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	wfgate "git.sumka.site/yakov/wfgate"
)

// cmdNewScript emits the harness half of an orchestration script.
//
// Measured across the six tracked scripts of one repository (10 823 lines): the
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
		// Inside a workspace the skeleton belongs where every other artifact
		// lives. It used to land in the working directory — which is the
		// workspace ROOT when the operator is standing in it — and a stray file
		// there is exactly what `wf verify` refuses, so the command created the
		// state the next command objects to and the operator paid a `wf add`,
		// a copy and a delete for it. Measured on the P3e gate, first-hand and
		// in no round: the two-step was performed by hand.
		if w, err := open(); err == nil {
			dst = filepath.Join(w.Artifacts(), dst)
		}
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
	// JSON-encoded, because both land inside single-quoted JavaScript string
	// literals: an apostrophe in a path — or a newline — emits a script that
	// does not parse, or worse, one that parses into something else.
	quote := func(v string) string {
		q, err := json.Marshal(v)
		if err != nil {
			return "''"
		}
		// JSON gives double quotes; the template's literals are single-quoted,
		// so strip them and keep the escaping JSON did.
		return "'" + strings.ReplaceAll(strings.Trim(string(q), `"`), `'`, `\'`) + "'"
	}
	body := strings.ReplaceAll(string(b), "'SCRIPT_NAME'", quote(name))
	body = strings.ReplaceAll(body, "'TARGET_REPO'", quote(repo))
	if d := filepath.Dir(dst); d != "." {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	// O_EXCL, not Stat-then-Write: the check above is a race, and WriteFile
	// truncates through a dangling symlink at dst. The kernel enforces "never
	// over a file that exists" and this one cannot.
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("%s: %w — a skeleton must never land on an authored script", dst, err)
	}
	if _, err := f.WriteString(body); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	todos := strings.Count(body, "TODO")
	fmt.Printf("wrote %s — %d line(s), %d TODO(s)\n", dst, strings.Count(body, "\n"), todos)
	fmt.Printf("  the harness is complete and passes both tools as it stands; the sections are yours\n")
	fmt.Printf("  read `wf docs authoring` before the first edit, then:\n")
	fmt.Printf("    wfcheck -root %s %s\n", repo, dst)
	fmt.Printf("    wfdry %s\n", dst)
	// A file under `artifacts/` is not yet an artifact: it is untracked, and
	// `wf verify` says so. `wf add` adopts a draft already in place, which is
	// the whole reason the skeleton may be written there at all.
	if w, err := open(); err == nil && strings.HasPrefix(dst, w.Artifacts()+string(filepath.Separator)) {
		fmt.Printf("  it is UNTRACKED until it is adopted — `wf add %s` makes it an artifact of this gate\n", dst)
	}
	// No count of "how many a tool can see". It was hand-pinned once and then
	// derived wrongly once — `TODO/` appears in owns: arrays that produce no
	// WARN — and a number nobody re-derives is the defect one of this project's
	// own gates found in a test file. The claim that matters carries no number:
	// most of these are PROSE, no check reaches prose, and a TODO left in a
	// prompt ships.
	fmt.Printf("  only the ones that are owned PATHS reach a check at all; the rest are PROSE, and a TODO left in a prompt ships\n")
	return nil
}
