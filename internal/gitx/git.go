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

// Run executes git in the repo and returns trimmed stdout. Stderr is folded
// into the error because git says the useful half there.
func (r Repo) Run(args ...string) (string, error) {
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
	if _, err := r.Run("commit", "-q", "-m", msg); err != nil {
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
func (r Repo) Dirty(pathspec ...string) ([]string, error) {
	args := append([]string{"status", "--porcelain", "--"}, pathspec...)
	out, err := r.Run(args...)
	if err != nil || out == "" {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}

func (r Repo) Tag(name, msg string) error {
	_, err := r.Run("tag", "-a", name, "-m", msg)
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
	out, err := r.Run("ls-files")
	if err != nil || out == "" {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}

// FilesAt lists the files tracked at ref, the way Files does for the working
// tree. The drift check resolves citations against the FROZEN revision: they
// were written and reviewed against it, and resolving them against whatever the
// target has since become would misread every file the move renamed or deleted.
func (r Repo) FilesAt(ref string) ([]string, error) {
	out, err := r.Run("ls-tree", "-r", "--name-only", ref)
	if err != nil || out == "" {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
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
		// after it until the tab-prefixed content belongs to that header.
		if len(l) < 41 || l[40] != ' ' || !isHex(l[:40]) {
			continue
		}
		f := strings.Fields(l)
		if len(f) < 3 {
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
