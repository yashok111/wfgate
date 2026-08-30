// wfcheck — the static check over a Claude Code Workflow orchestration script.
//
// Usage:
//
//	wfcheck <script.mjs>            check
//	wfcheck --freeze <script.mjs>   copy to .wf/gate/<sha8>-<name> and print the
//	                                path (hand THAT to the gate reviewers so their
//	                                line numbers cannot drift while fixes land)
//
// Exit 1 on any ERROR, 0 otherwise.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"git.sumka.site/yakov/wfgate/internal/cli"
	"git.sumka.site/yakov/wfgate/internal/wfcheck"
)

// version is stamped by the build. A run against a stale installed binary looks
// exactly like a run against the new one — the same reason `wf` carries it.
var version = "devel"

func main() {
	freeze := flag.Bool("freeze", false, "copy the script under .wf/gate/ and print the frozen path")
	// Empty, not "." — inside a gate workspace the right answer is the
	// workspace's target, and the working directory is the wrong tree.
	root := flag.String("root", "", "repository the script will run in (default: the gate workspace's target, else the working directory)")
	ownsDiff := flag.String("owns-diff", "", "AFTER a run: compare the script's ownership map against <ref>..WORKING TREE of -root, both directions")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `wfcheck — static check over a Workflow orchestration script

usage:
  wfcheck [-root <dir>] <script.mjs>      (-root defaults to the gate
                                           workspace's target when run inside one)
  wfcheck --freeze <script.mjs>
  wfcheck [-root <dir>] --owns-diff <ref> <script.mjs>
`)
	}
	pos, perr := cli.ParseMixed(flag.CommandLine, os.Args[1:])
	if perr != nil {
		os.Exit(2)
	}
	if *showVersion {
		fmt.Printf("wfcheck %s\n", version)
		return
	}
	if len(pos) != 1 {
		flag.Usage()
		os.Exit(2)
	}
	path := pos[0]

	if *freeze {
		out, err := freezeScript(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wfcheck:", err)
			os.Exit(1)
		}
		fmt.Println(out)
		return
	}

	if *ownsDiff != "" {
		if err := printOwnsDiff(path, *root, *ownsDiff); err != nil {
			fmt.Fprintln(os.Stderr, "wfcheck:", err)
			os.Exit(1)
		}
		return
	}

	rep, err := wfcheck.Run(path, *root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wfcheck:", err)
		os.Exit(1)
	}
	for _, f := range rep.Findings {
		fmt.Printf("%-9s %3d  %s\n", f.Level, f.Line, f.Msg)
	}
	errs, warns, notes := rep.Counts()
	fmt.Printf("\n%s: %d error(s), %d warning(s), %d note(s)\n", filepath.Base(path), errs, warns, notes)
	if errs > 0 {
		os.Exit(1)
	}
}

// printOwnsDiff reports the ownership map against the tree the run left behind.
//
// It exits ZERO whatever it finds. This is an AUDIT and not a bar: every row it
// prints is a question for a human — a file nobody owns may be a hole in the map
// or a legitimate incidental edit, and an owned file the run never wrote may be
// a section that skipped its task or a task that turned out not to need it. A
// check that fails a run on either would be answered by switching it off.
func printOwnsDiff(path, root, base string) error {
	rep, err := wfcheck.OwnsDiff(path, root, base)
	if err != nil {
		return err
	}
	// The population first, and always: an empty map compares nothing, and a
	// report that printed `0 unowned` over a map it failed to read would be a
	// green line meaning the opposite.
	fmt.Printf("=== owns vs %s..%s ===\n", rep.Base, rep.Head)
	if rep.RootNote != "" {
		fmt.Printf("%s (override with -root)\n", rep.RootNote)
	}
	fmt.Printf("map: %d file(s) and %d director(y/ies) from %d gate config(s) and %d YOU OWN block(s)\n",
		rep.OwnedFiles, rep.OwnedDirs, rep.Gates, rep.Blocks)
	if rep.OwnedFiles+rep.OwnedDirs == 0 {
		fmt.Println("NOT A COMPARISON: the script declares no ownership this tool could read")
		return nil
	}
	// A map read in PART is the difference between "no section owns this" and
	// "this tool could not read the section that does", and the second printed
	// in the register of the first is a false finding rather than a missed one.
	if rep.Partial {
		fmt.Println("PARTIAL MAP: an `owns` array or a YOU OWN block did not resolve statically — rows below may be FALSE")
	}
	fmt.Printf("the run changed %d file(s)\n", rep.Changed)
	if rep.Changed > 0 {
		fmt.Printf("\nwritten, owned by NO section (%d):\n", len(rep.Unowned))
		for _, p := range rep.Unowned {
			fmt.Println("  " + p)
		}
	} else {
		// The other direction is still an answer, and the loudest one: every
		// owned path is untouched, which is either a wrong base or a run that
		// did nothing.
		fmt.Println("NOT A COMPARISON in that direction: the range changed nothing — is the base the commit the run started from?")
	}
	fmt.Printf("\nowned, NEVER written (%d):\n", len(rep.Untouched))
	for _, p := range rep.Untouched {
		fmt.Println("  " + p)
	}
	// The audit cannot tell THIS run's edits from edits that were already
	// sitting in the tree when it started: it compares a base to the working
	// tree, and nothing records what was dirty at the base.
	fmt.Println("\nnote: the range is base..WORKING TREE, so anything already uncommitted when the run started is counted as the run's")
	return nil
}

// freezeScript copies the script under .wf/gate/ named by its own hash, so the
// reviewers read one immutable revision while fixes land on the working copy.
func freezeScript(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	dir := filepath.Join(".wf", "gate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(dir, hex.EncodeToString(sum[:])[:8]+"-"+filepath.Base(path))
	if err := os.WriteFile(out, b, 0o644); err != nil {
		return "", err
	}
	return out, nil
}
