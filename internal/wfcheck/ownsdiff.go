package wfcheck

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"git.sumka.site/yakov/wfgate/internal/gitx"
	"git.sumka.site/yakov/wfgate/internal/wfscript"
)

// The POST-RUN half of the ownership check, and it exists because the run-time
// half reads the wrong witness.
//
// A script's own harness compares each producer's REPORTED `files` list against
// its `owns` entries, and that check works — on one real run it filed six
// majors, every one of them a hole in the map rather than an agent exceeding
// it. What it cannot ask is the other direction, because a self-report only
// contains what the agent chose to name: a file the run wrote that NO section
// owns, and a file a section owns that the run never touched. Measured on that
// same run: `internal/resources/populate.go`, where the function the whole
// slice moves lives, appears in NEITHER artifact through ten rounds of review —
// no reviewer, no check and no self-report could see a path nobody wrote down.
//
// The witness here is the tree: the union of the declared map against the diff
// the run actually produced.
type OwnsDiffReport struct {
	Base, Head string
	Gates      int // gate-shaped configs whose `owns` was read
	Blocks     int // `YOU OWN` blocks read out of the string literals
	OwnedFiles int
	OwnedDirs  int
	Changed    int
	Unowned    []string // the run wrote it, no section declared it
	Untouched  []string // a section declared it, the run never wrote it
}

// OwnsDiff compares the ownership map a script DECLARES against the files a run
// actually wrote between base and HEAD of the repository at root.
//
// It reads the map from BOTH halves — the gate configs' `owns` arrays and the
// `YOU OWN` blocks in the string literals — because either alone under-counts:
// a factory-composed prompt resolves statically to nothing and a gate written
// without a config is invisible to the other. Under-counting here is not a
// missed finding, it is a FALSE one: a path the map declares in the half this
// check did not read prints as unowned.
func OwnsDiff(scriptPath, root, base string) (OwnsDiffReport, error) {
	var rep OwnsDiffReport
	s, err := wfscript.Load(scriptPath)
	if err != nil {
		return rep, err
	}
	src, err := os.ReadFile(scriptPath)
	if err != nil {
		return rep, err
	}
	root, _ = resolveRoot(root)

	files := map[string]bool{}
	var dirs []string
	for _, g := range gateConfigs(s) {
		rep.Gates++
		for p := range g.Files {
			files[p] = true
		}
		dirs = append(dirs, g.Dirs...)
	}
	blocks, textual := ownsFromLiterals(string(src))
	rep.Blocks = blocks
	for _, t := range textual {
		fs, ds := ownsTargets(t.Path)
		for _, f := range fs {
			files[f] = true
		}
		dirs = append(dirs, ds...)
	}
	dirs = uniqueSorted(dirs)
	rep.OwnedFiles, rep.OwnedDirs = len(files), len(dirs)

	repo := gitx.Repo{Dir: root}
	// A ref that does not resolve fails the diff below too, so this branch is
	// not what stops the run — it is what says WHICH argument was wrong. git's
	// own message for the diff is "ambiguous argument", which names neither the
	// base nor what a base is supposed to be.
	baseSha, err := repo.Run("rev-parse", "--short", base)
	if err != nil {
		return rep, fmt.Errorf("base %q does not resolve in %s — pass the commit the run STARTED from (the producers' \"HEAD before this run\"): %w", base, root, err)
	}
	rep.Base, rep.Head = baseSha, "worktree"

	// The WORKING TREE, not HEAD, and `--no-optional-locks` on every call.
	//
	// Two measured reasons, both from the run this check was written for. The
	// audit happens before the slice is committed — that run left 22 changed
	// files uncommitted and HEAD still at the commit it started from, so a
	// commit-to-commit diff compares a range that changed nothing and the
	// report reads green. And the target of a gate is READ-ONLY by this
	// project's first rule, while a plain `git diff` REFRESHES THE INDEX of the
	// tree it runs in — a write, into a repository another agent may be working
	// in this second. `--no-optional-locks` is git's own way of saying do not.
	out, err := repo.Run("--no-optional-locks", "diff", "--name-only", "--no-renames", base, "--")
	if err != nil {
		return rep, err
	}
	// Untracked files are the half `diff` cannot see and the half that matters
	// most here: a file the run CREATED is exactly the shape the map misses —
	// on the run this was written for, a mandatory `TestMain` a package had
	// never needed before.
	untracked, err := repo.Run("--no-optional-locks", "status", "--porcelain", "-uall")
	if err != nil {
		return rep, err
	}

	touched := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || touched[p] {
			return
		}
		touched[p] = true
		rep.Changed++
		if !owns(files, dirs, p) {
			rep.Unowned = append(rep.Unowned, p)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		add(line)
	}
	for _, line := range strings.Split(untracked, "\n") {
		if strings.HasPrefix(line, "?? ") {
			add(strings.Trim(strings.TrimPrefix(line, "?? "), "\""))
		}
	}
	for p := range files {
		if !touched[p] {
			rep.Untouched = append(rep.Untouched, p)
		}
	}
	for _, d := range dirs {
		if !dirTouched(touched, d) {
			rep.Untouched = append(rep.Untouched, d)
		}
	}
	sort.Strings(rep.Unowned)
	sort.Strings(rep.Untouched)
	return rep, nil
}

// owns is the match: an exact path, or any file under a declared directory.
func owns(files map[string]bool, dirs []string, p string) bool {
	if files[p] {
		return true
	}
	for _, d := range dirs {
		if strings.HasPrefix(p, d) {
			return true
		}
	}
	return false
}

func dirTouched(touched map[string]bool, dir string) bool {
	for p := range touched {
		if strings.HasPrefix(p, dir) {
			return true
		}
	}
	return false
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
