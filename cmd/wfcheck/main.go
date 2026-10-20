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
	"regexp"
	"strings"

	"github.com/yashok111/wfgate/internal/cli"
	"github.com/yashok111/wfgate/internal/wfcheck"
	"github.com/yashok111/wfgate/internal/ws"
)

// version is stamped by the build. A run against a stale installed binary looks
// exactly like a run against the new one — the same reason `wf` carries it.
var version = "devel"

func main() {
	freeze := flag.Bool("freeze", false, "copy the script under .wf/gate/ and print the frozen path")
	// Empty, not "." — inside a gate workspace the right answer is the
	// workspace's target, and the working directory is the wrong tree.
	root := flag.String("root", "", "repository the script will run in (default: the gate workspace's target, else the working directory)")
	ownsDiff := flag.String("owns-diff", "", "AFTER a run: compare the script's ownership map against <ref>..WORKING TREE of -root, both directions; for a read-only run type, assert the range is EMPTY")
	typ := flag.String("type", "", "the gate's run type for --owns-diff ("+strings.Join(ws.Types, "|")+"); default: the type of the workspace holding the script, else build")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `wfcheck — static check over a Workflow orchestration script

usage:
  wfcheck [-root <dir>] <script.mjs>      (-root defaults to the gate
                                           workspace's target when run inside one)
  wfcheck --freeze <script.mjs>
  wfcheck [-root <dir>] [-type T] --owns-diff <ref> <script.mjs>
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

	if *typ != "" && *ownsDiff == "" {
		fmt.Fprintln(os.Stderr, "wfcheck: -type applies to --owns-diff only; the static check reads every script the same way")
		os.Exit(2)
	}
	if *ownsDiff != "" {
		runType := *typ
		if runType == "" {
			t, src := ws.TypeAt(path)
			runType = t
			if strings.HasPrefix(src, "gate.json") {
				fmt.Printf("type %s — from %s; -type overrides\n", t, src)
			}
		}
		if err := ws.ValidType(runType); err != nil {
			fmt.Fprintln(os.Stderr, "wfcheck:", err)
			os.Exit(2)
		}
		if ws.ReadOnly(runType) {
			clean, err := printUnchanged(path, *root, *ownsDiff, runType)
			if err != nil {
				fmt.Fprintln(os.Stderr, "wfcheck:", err)
				os.Exit(1)
			}
			if !clean {
				os.Exit(1)
			}
			return
		}
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

// printUnchanged is --owns-diff for a read-only run, and unlike the build
// audit it is a BAR: such a run has exactly one property the tree can show — it
// is unchanged — so a changed file is not a question for a human, it is the
// run having broken the promise its type makes. Before the type existed the
// audit answered "NOT A COMPARISON: the script declares no ownership" for every
// read-only script and stopped there, so the one check the run needed was hand
// built (a stamp file, `find -newer`, `git status`) on the gate that had one.
func printUnchanged(path, root, base, typ string) (bool, error) {
	// A SHA, never a ref. A ref resolves NOW: a run that committed onto the
	// branch it was given moved that branch along with HEAD, the base resolves
	// to the new commit, and the range reads empty with HEAD "unmoved" — found
	// by the wave's review round. `HEAD` itself is the same case at its
	// plainest.
	if !shaLike.MatchString(base) {
		return false, fmt.Errorf("base %q is a ref, and a ref resolves now — a run that committed moved it too, and the bar would read green. Pass the commit SHA the run started from (`git rev-parse HEAD` in the target BEFORE the run)", base)
	}
	rep, err := wfcheck.OwnsDiff(path, root, base)
	if err != nil {
		return false, err
	}
	fmt.Printf("=== %s run: %s..%s must be unchanged ===\n", typ, rep.Base, rep.Head)
	if rep.RootNote != "" {
		fmt.Printf("%s (override with -root)\n", rep.RootNote)
	}
	clean := true
	if rep.HeadMoved {
		clean = false
		fmt.Printf("HEAD MOVED: HEAD is no longer %s — a read-only run moved HEAD, or the base is not the commit the run started from\n", rep.Base)
	}
	// A hunt reproduces in agent worktrees under ws.ScratchPrefix. Exempt is
	// exactly a REGISTERED linked worktree's root, reported untracked, and not
	// a symlink: the first version exempted every path under the prefix, and
	// the review round passed a tracked file edited there, a symlink out of
	// it, and a plain file a reader wrote, each listed as a "worktree" with
	// cleanup advice git then refused. Anything else under the prefix counts.
	registered := map[string]wfcheck.Worktree{}
	if ws.AllowsScratch(typ) {
		for _, w := range rep.Worktrees {
			if strings.HasPrefix(w.Path+"/", ws.ScratchPrefix) {
				registered[w.Path] = w
			}
		}
	}
	var changed []string
	for _, p := range rep.Paths {
		root := strings.TrimSuffix(p, "/")
		if _, ok := registered[root]; ok && rep.Untracked[p] && !isSymlink(filepath.Join(rep.Top, root)) {
			continue
		}
		changed = append(changed, p)
	}
	var scratch []wfcheck.Worktree
	for _, w := range rep.Worktrees {
		if _, ok := registered[w.Path]; ok {
			scratch = append(scratch, w)
		}
	}
	if len(changed) > 0 {
		clean = false
		fmt.Printf("CHANGED %d file(s) — a read-only run wrote into its target:\n", len(changed))
		for _, p := range changed {
			fmt.Println("  " + p)
		}
	}
	if len(scratch) > 0 {
		// Listed from `git worktree list`, so a worktree under a gitignored
		// `.claude/` is listed too — status never showed it. Each one's branch
		// is named, because removing the worktree leaves its branch behind.
		fmt.Printf("reproduction worktree(s) registered under %s (%d) — the run's scratch, not counted; once the report is read:\n", ws.ScratchPrefix, len(scratch))
		for _, w := range scratch {
			// Both halves are SHELL-QUOTED: the line is printed to be pasted,
			// and a worktree path is any string while a branch name may carry
			// `;`, `$(…)` or `|` — a crafted name would run as a command in the
			// operator's shell (the commit's security review).
			cmd := "git worktree remove --force " + shellQuote(w.Path)
			if w.Branch != "" {
				cmd += " && git branch -D " + shellQuote(w.Branch)
			}
			fmt.Println("  " + cmd)
		}
	}
	if rep.Detached {
		fmt.Println("note: HEAD is detached — if the run started on a branch, the run detached it, and HEAD at the base cannot show that")
	}
	if clean {
		if len(scratch) > 0 {
			fmt.Println("unchanged outside the reproduction worktrees: HEAD is the base and the working tree matches it, untracked files included")
		} else {
			fmt.Println("unchanged: HEAD is the base and the working tree matches it, untracked files included")
		}
	}
	// What the bar does not see, said on every run rather than discovered:
	// probed in the review round, all three exit 0.
	fmt.Println("not checked: gitignored paths, other refs (branches, tags, stash), and `.git` itself (hooks, config, info/exclude, index flags); anything already uncommitted when the run started is counted as the run's — start from a clean tree")
	return clean, nil
}

// shellQuote wraps s in single quotes for a POSIX shell, closing and reopening
// around each embedded quote, so nothing inside is expanded or executed.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func isSymlink(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

var shaLike = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

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
