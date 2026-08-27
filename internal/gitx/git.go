// Package gitx wraps the git plumbing the gate needs.
//
// Git is not an integration here, it is the storage layer. Every artifact
// revision the gate produces is a commit, every frozen round is a tag, and a
// delta round's input is a diff between two tags. That decision lives in one
// package so "the workspace is a repository" stays a single choice instead of
// a shell-out scattered across six commands.
package gitx

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Repo is a git working tree. The zero value is unusable; Dir must be set.
type Repo struct{ Dir string }

// pinned is prepended to every invocation so that what this tool reports does
// not depend on the operator's ~/.gitconfig.
//
// Measured 2026-08-25, on the three closed gates: the diff algorithm is an
// UNDECLARED INPUT that moves the numbers. git's default is myers; this box
// carried `diff.algorithm = histogram` in ~/.gitconfig, so every figure the
// project has banked was produced under histogram while a stock machine would
// have produced others. `wf stats` totals over p2e: 4586 changed lines under
// myers, 4482 under histogram, 4458 minimal, 4512 patience — 2.8% apart, and
// per round up to 5.3% (r2: 985 vs 933), enough to move a round's cost ratio
// by 0.2 per 100. `check stale` moved one range's boundary between "the fix
// wrote this sentence" and "this is an untouched consumer" (p2e gate-r1..HEAD:
// 22 vs 25 scanned of 1891) — the verdict held, the denominator did not.
//
// histogram rather than myers is not a claim about diff quality: it is what
// every banked number was measured under, so pinning myers would silently
// invalidate the corpus this project reasons from.
//
// `check drift` is immune — byte-identical across all four algorithms over
// 235/343/230 citations, because --unified=0 leaves only old-side ranges and
// their union is stable. So is diff.indentHeuristic: measured on the same three
// corpora, both settings produce identical `stats` and `drift`. It is left
// unpinned rather than pinned on principle, because a guard nothing can make
// fail is a guard nothing proves.
var pinned = []string{"-c", "diff.algorithm=histogram"}

// Run executes git in the repo and returns trimmed stdout. Stderr is folded
// into the error because git says the useful half there.
func (r Repo) Run(args ...string) (string, error) {
	args = append(append([]string{}, pinned...), args...)
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// Init creates the repository. The default branch is named explicitly so the
// gate does not inherit whatever init.defaultBranch the host happens to carry.
func (r Repo) Init() error {
	_, err := r.Run("init", "-q", "-b", "main")
	return err
}

// Commit stages exactly paths and commits them.
//
// Only the named paths are staged, never "git add -A": a gate workspace often
// holds a half-written artifact the operator is still editing by hand, and an
// apply that swept it into the same commit would destroy the property the whole
// tool exists for, that one commit equals one reviewed change.
func (r Repo) Commit(msg string, paths []string) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("commit: no paths")
	}
	if _, err := r.Run(append([]string{"add", "--"}, paths...)...); err != nil {
		return "", err
	}
	staged, err := r.Run(append([]string{"diff", "--cached", "--name-only", "--"}, paths...)...)
	if err != nil {
		return "", err
	}
	if staged == "" {
		return "", ErrNothingStaged
	}
	// The pathspec is not decoration: `git commit` without one commits the whole
	// INDEX, so a half-written draft the operator had staged by hand rode into
	// the same commit — and, being tracked from then on, passed freeze's dirty
	// check and was hashed into the manifest as a reviewed artifact. Naming the
	// paths keeps the property the whole tool is for: one commit is one reviewed
	// change.
	if _, err := r.Run(append([]string{"commit", "-q", "-m", msg, "--"}, paths...)...); err != nil {
		return "", err
	}
	return r.Head()
}

// ErrNothingStaged reports an apply whose edits produced byte-identical files.
// It is not a failure of the apply, so callers distinguish it rather than
// treating every empty commit as an error.
var ErrNothingStaged = fmt.Errorf("nothing staged")

func (r Repo) Head() (string, error) { return r.Run("rev-parse", "HEAD") }

// Dirty returns the porcelain status lines under pathspec, empty when clean.
func (r Repo) Dirty(pathspec ...string) ([]Change, error) {
	// -z, not plain porcelain: git QUOTES a path containing a space or a
	// non-ASCII byte ("artifacts/my draft.md"), and the caller that split such a
	// line on its last space read `draft.md"` as the path — so a dirty artifact
	// was counted as round bookkeeping and verify printed the reassuring "commit
	// it, do not re-freeze" over it. With -z the path is raw and the record
	// boundary is a NUL.
	args := append([]string{"status", "--porcelain", "-z", "--"}, pathspec...)
	out, err := r.Run(args...)
	if err != nil || out == "" {
		return nil, err
	}
	var cs []Change
	recs := splitNUL(out)
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		if len(rec) < 4 {
			continue
		}
		c := Change{Status: rec[:2], Path: rec[3:]}
		// A rename record is followed by its ORIGIN path as a separate NUL
		// field; consume it so it is not read as a change of its own.
		if c.Status[0] == 'R' || c.Status[0] == 'C' {
			i++
		}
		cs = append(cs, c)
	}
	return cs, nil
}

// Change is one line of `git status --porcelain`, split where git splits it
// rather than where a space happens to fall.
type Change struct {
	Status string // the two-letter XY code, as git prints it
	Path   string // raw, never quoted
}

func (c Change) String() string { return c.Status + " " + c.Path }

// Untracked reports the `??` code, the one class that is dirt without being a
// change to something under review.
func (c Change) Untracked() bool { return c.Status == "??" }

func (r Repo) Tag(name, msg string) error {
	_, err := r.Run("tag", "-a", name, "-m", msg)
	return err
}

// RetagTo moves an existing annotated tag. It is the ONE place a tag stops
// meaning what it meant, so it is a named operation rather than a `-f` flag
// somewhere: `wf freeze -refreeze` is the only caller, and what it publishes is
// recorded in the freeze manifest it writes in the same breath.
func (r Repo) RetagTo(name, msg string) error {
	// `-f`, not delete-then-create: one ref update. Deleting first leaves the
	// round UNTAGGED if the create then fails, and the freeze manifest — which
	// records the supersession — has already been committed by that point, so
	// the recovery run would take the first-freeze path and lose the record.
	_, err := r.Run("tag", "-f", "-a", name, "-m", msg)
	return err
}

func (r Repo) TagExists(name string) bool {
	_, err := r.Run("rev-parse", "--verify", "-q", "refs/tags/"+name)
	return err == nil
}

func (r Repo) Tags(prefix string) ([]string, error) {
	out, err := r.Run("tag", "--list", prefix+"*", "--sort=v:refname")
	if err != nil || out == "" {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}

// LastTouch returns the sha and subject of the newest commit that changed path.
// The duplicate-rule check reads it: two copies of one rule whose last-touching
// commits differ is exactly the shape of "corrected here, left standing there".
func (r Repo) LastTouch(path string) (sha, subject string, err error) {
	out, err := r.Run("log", "-1", "--format=%h%x00%s", "--", path)
	if err != nil || out == "" {
		return "", "", err
	}
	sha, subject, _ = strings.Cut(out, "\x00")
	return sha, subject, nil
}

func (r Repo) Diff(from, to string, pathspec ...string) (string, error) {
	args := []string{"diff", "--stat=200", from, to, "--"}
	stat, err := r.Run(append(args, pathspec...)...)
	if err != nil {
		return "", err
	}
	args = []string{"diff", from, to, "--"}
	body, err := r.Run(append(args, pathspec...)...)
	if err != nil {
		return "", err
	}
	return stat + "\n\n" + body, nil
}

// Files lists tracked files. It is git ls-files rather than a filesystem walk
// so that a vendored dependency tree inside the repo cannot be cited, and so
// gitignored artifacts stay invisible to the citation resolver.
func (r Repo) Files() ([]string, error) {
	// -z for the same reason Dirty uses it: git QUOTES a path containing a space
	// or a non-ASCII byte, and a quoted path matches nothing — so a citation to
	// a real tracked file was reported as naming no tracked file at all.
	out, err := r.Run("ls-files", "-z")
	if err != nil || out == "" {
		return nil, err
	}
	return splitNUL(out), nil
}

// splitNUL splits git's -z output, which is NUL-TERMINATED rather than
// NUL-separated: the trailing empty field is not a path.
func splitNUL(out string) []string {
	out = strings.TrimRight(out, "\x00")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\x00")
}

// FilesAt lists the files tracked at ref, the way Files does for the working
// tree. The drift check resolves citations against the FROZEN revision: they
// were written and reviewed against it, and resolving them against whatever the
// target has since become would misread every file the move renamed or deleted.
func (r Repo) FilesAt(ref string) ([]string, error) {
	out, err := r.Run("ls-tree", "-r", "--name-only", "-z", ref)
	if err != nil || out == "" {
		return nil, err
	}
	return splitNUL(out), nil
}

// CommitExists reports whether sha names a commit in this repository. The
// findings table claims closures by commit, and a claim naming a commit that
// does not exist is a lie the record must refuse, not a typo to warn about.
func (r Repo) CommitExists(sha string) bool {
	_, err := r.Run("rev-parse", "--verify", "-q", sha+"^{commit}")
	return err == nil
}

// CommitBrief returns one commit's subject and the files it touched. The delta
// checklist prints both so a reviewer maps claim → commit → files without
// leaving the briefing; the artifact diff below it is the evidence.
func (r Repo) CommitBrief(sha string) (string, []string, error) {
	out, err := r.Run("show", "--name-only", "--format=%s", sha)
	if err != nil {
		return "", nil, err
	}
	lines := strings.Split(out, "\n")
	var files []string
	for _, l := range lines[1:] {
		if l != "" {
			files = append(files, l)
		}
	}
	return lines[0], files, nil
}

// BlameLines maps each 1-based line of path to the commit that last changed it.
//
// File-level history is too coarse for the duplicate check: one commit touching
// an artifact marks every rule in it as recently changed, and a check that
// flags 161 of 176 groups is a check nobody runs twice. Line-level blame answers
// the question actually being asked, which is whether these two SENTENCES were
// last edited together.
func (r Repo) BlameLines(path string) ([]string, error) {
	out, err := r.Run("blame", "--porcelain", "--", path)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		// A header is "<sha> <origline> <finalline> [<numlines>]"; everything
		// after it until the tab-prefixed content belongs to that header. The
		// object id is read as the first whitespace-delimited field rather than
		// at a fixed offset of 40: a repository with sha256 object format prints
		// 64 hex characters, every header was skipped, every line got the empty
		// commit, and dup's divergence — which needs two DIFFERENT commits —
		// silently became "these copies were edited together".
		// A blame CONTENT line is tab-prefixed, and its text may itself begin
		// with something that reads like a header: `\tdeadbee 1 2` survived the
		// checks below and then panicked on the eight-character slice. Content
		// lines are excluded by their tab, and the id must be long enough to
		// slice.
		if strings.HasPrefix(l, "\t") {
			continue
		}
		f := strings.Fields(l)
		if len(f) < 3 || len(f[0]) < 8 || !isHex(f[0]) {
			continue
		}
		final := atoi(f[2])
		count := 1
		if len(f) >= 4 {
			count = atoi(f[3])
		}
		if final <= 0 {
			continue
		}
		for len(lines) < final+count-1 {
			lines = append(lines, "")
		}
		for i := 0; i < count; i++ {
			lines[final-1+i] = f[0][:8]
		}
	}
	return lines, nil
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func atoi(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return -1
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}
