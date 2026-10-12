package wfcheck

import (
	"fmt"
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
// majors: four holes in the map, and two a section writing a file another
// section had declared, which is the case it exists for. What it cannot ask is the other direction, because a self-report only
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
	Root       string
	RootNote   string // set when -root was INFERRED rather than given
	Gates      int    // gate-shaped configs whose `owns` was read
	Blocks     int    // `YOU OWN` blocks read out of the string literals
	OwnedFiles int
	OwnedDirs  int
	Changed    int
	// Partial is true when a gate's `owns` array could not be read in full.
	// It is the difference between "no section owns this file" and "this tool
	// could not read the section that does", and printing the first while
	// meaning the second is this project's most repeated defect: an absent
	// measurement in the register of a measured one.
	Partial bool
	Unowned []string // the run wrote it, no section declared it
	Paths   []string // every path the run changed, owned or not
	// HeadMoved says HEAD is no longer the base. A build commits, so for it this
	// is the expected state and nothing reads it; a read-only run must not move
	// a ref, and a commit followed by its revert leaves a diff of nothing.
	HeadMoved bool
	Untouched []string // a section declared it, the run never wrote it
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
	// The source is already in memory: a second read adds an I/O error path and
	// a window in which the two copies disagree.
	root, rootNote, err := resolveRoot(root)
	if err != nil {
		return rep, err
	}
	rep.Root, rep.RootNote = root, rootNote

	files := map[string]bool{}
	var dirs []string
	for _, g := range gateConfigs(s) {
		rep.Gates++
		if !g.OwnsComplete {
			rep.Partial = true
		}
		for p := range g.Files {
			files[p] = true
		}
		dirs = append(dirs, g.Dirs...)
	}
	blocks, textual := ownsFromLiterals(s.Src)
	// A script that WRITES `YOU OWN` and whose blocks this tool read none of is
	// the same partial map by another door — the header and its paths can live
	// in separate concatenated literals, which the textual reader does not
	// join.
	if blocks == 0 && strings.Contains(s.Src, "YOU OWN") {
		rep.Partial = true
	}
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
	// Which argument was wrong. Without this, a -root that is not a repository
	// at all is reported as a bad BASE, because the base is what the next call
	// happens to name.
	if _, err := repo.Run("rev-parse", "--git-dir"); err != nil {
		return rep, fmt.Errorf("%s is not a git repository — pass the repository the run wrote into with -root: %w", root, err)
	}
	// A ref that does not resolve fails the diff below too, so this branch is
	// not what stops the run — it is what says WHICH argument was wrong. git's
	// own message for the diff is "ambiguous argument", which names neither the
	// base nor what a base is supposed to be.
	baseSha, err := repo.Run("rev-parse", "--short", base)
	if err != nil {
		return rep, fmt.Errorf("base %q does not resolve in %s — pass the commit the run STARTED from (HEAD before the run): %w", base, root, err)
	}
	rep.Base, rep.Head = baseSha, "worktree"
	if full, err := repo.Run("rev-parse", base+"^{commit}"); err == nil {
		if head, err := repo.Run("rev-parse", "HEAD"); err == nil {
			rep.HeadMoved = strings.TrimSpace(head) != strings.TrimSpace(full)
		}
	}

	// The WORKING TREE, not HEAD, and `--no-optional-locks` on every call.
	//
	// Two measured reasons, both from the run this check was written for. The
	// audit happens before the slice is committed — that run left 54 changed
	// files uncommitted and HEAD still at the commit it started from, so a
	// commit-to-commit diff compares a range that changed nothing and the
	// report reads green. And the target of a gate is READ-ONLY by this
	// project's first rule, while a plain `git diff` REFRESHES THE INDEX of the
	// tree it runs in — a write, into a repository another agent may be working
	// in this second. `--no-optional-locks` is git's own way of saying do not.
	// `-z` on both commands: without it git QUOTES any path that is not plain
	// ASCII — `"a/w\303\251ird.go"` — and the quotes and octal escapes travel
	// into the comparison, so the path matches no entry, prints as unowned, and
	// names a file that exists nowhere.
	out, err := repo.Run("--no-optional-locks", "diff", "--name-only", "-z", "--no-renames", base, "--")
	if err != nil {
		return rep, err
	}
	// Two things this range cannot do, both stated rather than hidden. It
	// cannot separate THIS run's edits from edits already sitting uncommitted
	// when the run started — nothing records what was dirty at the base — so
	// the report says so on every run. And a dirty SUBMODULE surfaces as its
	// gitlink path rather than as the files inside it, so it reads as one
	// unowned entry named after the directory.
	//
	// Untracked files are the half `diff` cannot see and the half that matters
	// most here: a file the run CREATED is exactly the shape the map misses —
	// on the run this was written for, a mandatory `TestMain` a package had
	// never needed before.
	untracked, err := repo.Run("--no-optional-locks", "status", "--porcelain", "-z", "-uall")
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
		rep.Paths = append(rep.Paths, p)
		if !owns(files, dirs, p) {
			rep.Unowned = append(rep.Unowned, p)
		}
	}
	for _, p := range strings.Split(out, "\x00") {
		add(p)
	}
	// Under `-z` a status entry is `XY path`, and a RENAME spends a second
	// field on the old path with no status of its own — skip it rather than
	// let a path that happens to carry a space in its third column read as a
	// status code.
	skipNext := false
	for _, entry := range strings.Split(untracked, "\x00") {
		if skipNext {
			skipNext = false
			continue
		}
		if len(entry) < 4 || entry[2] != ' ' {
			continue
		}
		if entry[0] == 'R' || entry[0] == 'C' {
			skipNext = true
			continue
		}
		if entry[0] == '?' && entry[1] == '?' {
			add(entry[3:])
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
	sort.Strings(rep.Paths)
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
