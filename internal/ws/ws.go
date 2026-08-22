// Package ws is the gate workspace: a git repository that holds the artifacts
// of one review gate and nothing else.
//
// The workspace is deliberately NOT inside the repository under review. A gate
// run has agents diffing that tree against its own HEAD, and the rule that the
// tree stays untouched while a run is live cannot survive the orchestration
// committing into it. Target is therefore a path the workspace reads and never
// writes.
package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git.sumka.site/yakov/wfgate/internal/gitx"
)

// ManifestName is the marker file. Its presence is what makes a directory a
// workspace, and Find walks up looking for exactly it.
const ManifestName = "gate.json"

// Manifest is the workspace's identity. It is small on purpose: everything that
// can be derived from git (round tags, artifact hashes, history) is derived,
// not duplicated here, so the file cannot drift from the repository it labels.
type Manifest struct {
	Name    string `json:"name"`
	Target  string `json:"target"`  // absolute path of the repo under review
	Created string `json:"created"` // RFC3339
	Round   int    `json:"round"`   // the round currently open; 0 before the first
}

type Workspace struct {
	Root string
	M    Manifest
	Git  gitx.Repo
}

var ErrNotFound = errors.New("not a gate workspace (no gate.json here or above)")

// Find walks up from start looking for the manifest, the way git itself finds
// its root. Every command below the workspace therefore works from any
// subdirectory without a flag.
func Find(start string) (*Workspace, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	for {
		p := filepath.Join(dir, ManifestName)
		if _, err := os.Stat(p); err == nil {
			return open(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, ErrNotFound
		}
		dir = parent
	}
}

func open(root string) (*Workspace, error) {
	b, err := os.ReadFile(filepath.Join(root, ManifestName))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestName, err)
	}
	return &Workspace{Root: root, M: m, Git: gitx.Repo{Dir: root}}, nil
}

// Create initialises a workspace: git repository, manifest, artifact skeleton,
// one commit. The initial commit matters — every later command compares against
// a parent, and a workspace whose first state was never committed has no
// baseline to diff a first round against.
func Create(root, name, target string, now time.Time) (*Workspace, error) {
	if _, err := os.Stat(filepath.Join(root, ManifestName)); err == nil {
		return nil, fmt.Errorf("%s already holds a workspace", root)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(filepath.Join(abs, ".git")); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("target %s is not a git repository", abs)
	}
	for _, d := range []string{"artifacts", "rounds"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			return nil, err
		}
	}
	w := &Workspace{
		Root: root,
		M:    Manifest{Name: name, Target: abs, Created: now.UTC().Format(time.RFC3339), Round: 0},
		Git:  gitx.Repo{Dir: root},
	}
	if err := w.Save(); err != nil {
		return nil, err
	}
	// .gitkeep, not a README: rounds/ must exist in the first commit so that
	// "the tree is clean" is a meaningful check before any round is opened.
	if err := os.WriteFile(filepath.Join(root, "rounds", ".gitkeep"), nil, 0o644); err != nil {
		return nil, err
	}
	if err := w.Git.Init(); err != nil {
		return nil, err
	}
	if _, err := w.Git.Commit("gate: init "+name, []string{"."}); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Workspace) Save() error {
	b, err := json.MarshalIndent(w.M, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(w.Root, ManifestName), append(b, '\n'), 0o644)
}

func (w *Workspace) Artifacts() string { return filepath.Join(w.Root, "artifacts") }

func (w *Workspace) RoundDir(n int) string {
	return filepath.Join(w.Root, "rounds", fmt.Sprintf("r%d", n))
}

func RoundTag(n int) string { return fmt.Sprintf("gate-r%d", n) }

// RoundOfTag is RoundTag's inverse, 0 for anything else. A caller that names a
// base tag by hand still needs the round it belongs to, so that the findings a
// delta prints come from the round being closed rather than the one open.
func RoundOfTag(tag string) int {
	rest, ok := strings.CutPrefix(tag, "gate-r")
	if !ok {
		return 0
	}
	n := 0
	for _, c := range rest {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// ArtifactList returns the artifact files relative to the workspace root, so
// that a caller can pass them to git and to a check without re-deriving paths.
func (w *Workspace) ArtifactList() ([]string, error) {
	var out []string
	err := filepath.WalkDir(w.Artifacts(), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// `.gitkeep` is the only dot-entry that is bookkeeping rather than an
		// artifact, and it is named rather than pattern-matched: skipping every
		// dot-prefixed NAME hid a real artifact completely — `artifacts/.rules.md`
		// was tracked and committed, absent from the freeze manifest, and absent
		// from verify's changed/added/gone classes, so its content could be
		// rewritten after the freeze and `wf verify` still printed "clean".
		if d.IsDir() || d.Name() == ".gitkeep" {
			return nil
		}
		rel, err := filepath.Rel(w.Root, p)
		if err != nil {
			return err
		}
		out = append(out, rel)
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return out, err
}
