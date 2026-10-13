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

	// Type is what the gated run DOES to its target, set once by `wf init
	// -type` and read by every command whose rules depend on it — the emitted
	// script, the dry run's fixture polarity, the post-run diff check. Empty
	// means build: every workspace opened before the field existed was a
	// build gate, so the old manifests read correctly without a migration.
	// RunType is the accessor; nothing reads the field raw.
	Type string `json:"type,omitempty"`

	// ClosedEarly records a gate closed AGAINST the stop rule — its last
	// reviewer round produced a BLOCKER and the target's owner chose to build
	// anyway. It is the one fact about a gate's end that git cannot derive:
	// the tag, the ledger and the close commit look identical for a gate that
	// met the rule and one that did not. Measured on A18: closed at three
	// standing blockers, all three about the artifact's own guards, and the
	// record said nothing, so a reader of the corpus could not tell the two
	// apart without re-reading round 4's ledger by hand.
	ClosedEarly *EarlyClose `json:"closedEarly,omitempty"`
}

// The run types. A build run WRITES its target — producers own files, bars
// are run, a fix round may follow; a review run READS a diff and returns
// findings, and its target must come out of the run byte-identical. The
// toolchain was build-shaped until the type existed, and it cost the read-only
// gates on record in two silent ways: the dry run's passing fixture handed
// every finder an EMPTY findings array, so a find → verify → merge script's
// happy path ran in no passing mode at all (one real hunt: 53 agents in
// `clean` against 732 in `blockers`, every mode green), and `--owns-diff`
// answered "nothing to compare" instead of checking the one property such a
// run has, an unchanged tree.
//
// A hunt READS a subsystem rather than a diff and verifies its findings by
// REPRODUCTION — a failing test or command written in a throwaway worktree
// the run never merges — so its target is read-only too, with one carve-out
// the post-run check names: the reproduction worktrees themselves.
const (
	TypeBuild  = "build"
	TypeReview = "review"
	TypeHunt   = "hunt"
	// An audit answers a QUESTION about its target — spec against code, docs
	// against code, a security question — with a document whose every claim
	// cites `path:line#anchor`, so `wf check cite -require-anchor` can check
	// the citations mechanically once the document is in the workspace.
	TypeAudit = "audit"
	// A security run searches a subsystem — or, given a base, a diff — for
	// VULNERABILITIES under a threat model the decision document states. It
	// is a hunt whose verifier defaults to refuting, whose verdict has a third
	// state (cannot verify), whose serious findings are proven by a PoC in a
	// reproduction worktree, and whose later rounds search for VARIANTS of what
	// was confirmed. Deterministic scanners run before it, and their output is
	// a set of candidates every one of which is adjudicated.
	TypeSecurity = "security"
)

// Types is the closed list, in the order usage lines print it.
var Types = []string{TypeBuild, TypeReview, TypeHunt, TypeAudit, TypeSecurity}

// ScratchPrefix is the one place a read-only run may write inside its target:
// where Claude Code puts an agent's `isolation: 'worktree'` checkout. Only a
// hunt and a security run are allowed it — they reproduce; a review and an
// audit reproduce nothing.
const ScratchPrefix = ".claude/worktrees/"

// AllowsScratch says whether a run of this type may leave reproduction
// worktrees under ScratchPrefix.
func AllowsScratch(typ string) bool { return typ == TypeHunt || typ == TypeSecurity }

// RunType is the manifest's type with the empty default resolved.
func (m Manifest) RunType() string {
	if m.Type == "" {
		return TypeBuild
	}
	return m.Type
}

// ReadOnly says whether a run of this type may change its target. Every type
// but build is read-only; a type that writes would be a second build.
func ReadOnly(typ string) bool { return typ != TypeBuild }

// ValidType refuses a type outside the closed list, naming the list — a
// misspelt type silently defaulting to build would gate a review run with the
// build rules, which is the failure the field exists to end.
func ValidType(typ string) error {
	for _, t := range Types {
		if typ == t {
			return nil
		}
	}
	return fmt.Errorf("unknown run type %q — one of: %s", typ, strings.Join(Types, ", "))
}

// TypeAt resolves the run type for a file the two script-gate tools were
// handed: the type of the workspace that holds it, found by walking up the way
// Find does, or build when the file lies in no workspace. The second value
// says WHERE the answer came from, because the tools print it — an inferred
// setting is announced with the flag that overrides it.
func TypeAt(path string) (typ, source string) {
	w, err := Find(filepath.Dir(path))
	if err != nil {
		return TypeBuild, "default — no gate.json above the script"
	}
	return w.M.RunType(), "gate.json at " + w.Root
}

// EarlyClose is the owner's call, banked: which round it overrode, how many
// BLOCKERs stood, and why. `wf close -early` writes it; `wf stats` and the
// close record print it; nothing else reads it.
type EarlyClose struct {
	Round    int    `json:"round"`
	Blockers int    `json:"blockers"`
	Reason   string `json:"reason"`
	Decided  string `json:"decided"` // RFC3339
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
func Create(root, name, target, typ string, now time.Time) (*Workspace, error) {
	if err := ValidType(typ); err != nil {
		return nil, err
	}
	if typ == TypeBuild {
		typ = "" // the default is not written, so a build manifest reads as it always has
	}
	if _, err := os.Stat(filepath.Join(root, ManifestName)); err == nil {
		return nil, fmt.Errorf("%s already holds a workspace", root)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return nil, err
	}
	// `.git` is a FILE in a linked worktree and a submodule, so requiring a
	// directory refused a perfectly ordinary target.
	if _, err := os.Stat(filepath.Join(abs, ".git")); err != nil {
		return nil, fmt.Errorf("target %s is not a git repository", abs)
	}
	for _, d := range []string{"artifacts", "rounds"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			return nil, err
		}
	}
	w := &Workspace{
		Root: root,
		M:    Manifest{Name: name, Target: abs, Created: now.UTC().Format(time.RFC3339), Round: 0, Type: typ},
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
