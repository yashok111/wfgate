package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// Everything a close refuses, it refuses BEFORE writing. `wf freeze` once wrote
// and committed its manifest and only then refused to re-freeze, so the command
// that said no had already blessed what it refused.
func TestCloseRefusesBeforeWriting(t *testing.T) {
	t.Run("an unfrozen last round", func(t *testing.T) {
		wsDir := newGate(t)
		_, err := runWF(t, wsDir, "close")
		if err == nil {
			t.Fatal("want a refusal over an open round")
		}
		if _, err := os.Stat(filepath.Join(wsDir, closeDir)); !os.IsNotExist(err) {
			t.Fatal("the refusal wrote a snapshot anyway")
		}
	})

	t.Run("a standing BLOCKER", func(t *testing.T) {
		wsDir := newGate(t)
		if _, err := runWF(t, wsDir, "freeze"); err != nil {
			t.Fatal(err)
		}
		ledger(t, wsDir, 1, "| 1 | BLOCKER | the seam is undecided |  |\n")
		if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
			t.Fatal(err)
		}
		out, err := runWF(t, wsDir, "close")
		if err == nil {
			t.Fatalf("want a refusal over an open blocker, got:\n%s", out)
		}
		if !strings.Contains(err.Error(), "BLOCKER") {
			t.Fatalf("the refusal must name the blocker: %v", err)
		}
	})

	t.Run("a dirty workspace", func(t *testing.T) {
		wsDir := newGate(t)
		if _, err := runWF(t, wsDir, "freeze"); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(wsDir, "artifacts", "stray.md"), "half a draft\n")
		if _, err := runWF(t, wsDir, "close"); err == nil {
			t.Fatal("want a refusal over an uncommitted artifact")
		}
	})
}

// The snapshot is what survives the repository, so it carries what the
// repository is the only holder of: the commit times, the freeze manifests'
// surviving half, and every round's ledger verbatim.
func TestCloseBanksWhatTheRepositoryAloneHolds(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "close")
	noErr(t, err, "close")
	mustContain(t, out, "close/history.tsv", "close/stats.txt", "close/freezes.tsv",
		"close/rounds/r1/findings.md", "close/rounds/r1/freeze.json", "close/feedback.md", "committed ")

	hist := read(t, filepath.Join(wsDir, closeDir, "history.tsv"))
	if !strings.Contains(hist, "gate: init demo") {
		t.Fatalf("the history must carry the subjects: %s", hist)
	}
	fz := read(t, filepath.Join(wsDir, closeDir, "freezes.tsv"))
	if !strings.HasPrefix(fz, "round\tfrozen\tcommit\ttargetRef\ttool\tciteAnchored\tciteTotal\tartifactLines\trefreezes\tsupersedes\n") {
		t.Fatalf("freezes.tsv header: %q", fz)
	}
	// wf delta -save commits the round's brief into rounds/rN/, and prune
	// deletes rounds/: a snapshot that copied only findings.md destroyed the
	// reviewer input the next round was actually handed.
	if _, err := os.Stat(filepath.Join(wsDir, closeDir, "rounds", "r1", "freeze.json")); err != nil {
		t.Fatalf("every file of a round must be banked, not just findings.md: %v", err)
	}
	fb := read(t, filepath.Join(wsDir, closeDir, "feedback.md"))
	for _, s := range closeFeedbackSections {
		if !strings.Contains(fb, "## "+s) {
			t.Fatalf("the scaffold is missing %q", s)
		}
	}
	if !strings.Contains(fb, "## stats — workspace demo") {
		t.Fatal("the scaffold must prefill the derived numbers, not ask for them again")
	}
}

// A form the operator has filled is the one thing a command must never replace,
// and re-closing after a post-gate edit is a normal thing to do.
func TestCloseKeepsAFilledFeedback(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "close"); err != nil {
		t.Fatal(err)
	}
	fb := filepath.Join(wsDir, closeDir, "feedback.md")
	write(t, fb, "# Gate feedback — demo\n\nwhat I actually learned\n")

	out, err := runWF(t, wsDir, "close")
	noErr(t, err, "second close")
	mustContain(t, out, "feedback.md already existed and was kept")
	if got := read(t, fb); !strings.Contains(got, "what I actually learned") {
		t.Fatalf("the filled form was overwritten: %q", got)
	}
}

func TestPruneRefusesWithoutARecord(t *testing.T) {
	wsDir := newGate(t)
	_, err := runWF(t, wsDir, "prune", "-yes")
	if err == nil {
		t.Fatal("want a refusal with no snapshot")
	}
	if !strings.Contains(err.Error(), "wf close") {
		t.Fatalf("the refusal must name the repair: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wsDir, ".git")); err != nil {
		t.Fatal("the refusal deleted something anyway")
	}
}

// Deletion is not reversible and the repository is not recoverable, so the bare
// command reports and stops.
func TestPruneNeedsYes(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "close"); err != nil {
		t.Fatal(err)
	}

	out, err := runWF(t, wsDir, "prune")
	noErr(t, err, "prune dry run")
	mustContain(t, out, "would remove .git", "nothing removed — re-run with -yes")
	if _, err := os.Stat(filepath.Join(wsDir, ".git")); err != nil {
		t.Fatal("the dry run deleted the repository")
	}

	out, err = runWF(t, wsDir, "prune", "-yes")
	noErr(t, err, "prune")
	mustContain(t, out, "removed .git", "removed artifacts", "removed rounds")
	for _, gone := range []string{".git", "artifacts", "rounds"} {
		if _, err := os.Stat(filepath.Join(wsDir, gone)); !os.IsNotExist(err) {
			t.Fatalf("%s survived", gone)
		}
	}
	// The record, and the rules version that briefed the gate, stay.
	for _, kept := range []string{closeDir, "gate.json", "gate-checks.md"} {
		if _, err := os.Stat(filepath.Join(wsDir, kept)); err != nil {
			t.Fatalf("%s was destroyed: %v", kept, err)
		}
	}
}

// A snapshot taken before the last edit is a snapshot of a different gate.
func TestPruneRefusesAStaleRecord(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	tmp := filepath.Dir(wsDir)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "close"); err != nil {
		t.Fatal(err)
	}
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=post-gate\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", edits); err != nil {
		t.Fatal(err)
	}

	_, err := runWF(t, wsDir, "prune", "-yes")
	if err == nil {
		t.Fatal("want a refusal over a snapshot older than the artifacts")
	}
	if !strings.Contains(err.Error(), "re-run") {
		t.Fatalf("the refusal must name the repair: %v", err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The skeleton is emitted with the workspace's own target and its own name, and
// never over a file that exists — a skeleton written onto an authored script is
// the whole-file overwrite `wf add` refuses, with the same consequence.
func TestNewScriptSubstitutesAndNeverOverwrites(t *testing.T) {
	wsDir := newGate(t)
	dst := filepath.Join(wsDir, "artifacts", "run.mjs")

	out, err := runWF(t, wsDir, "new-script", "slice-a", "-o", dst)
	noErr(t, err, "new-script")
	mustContain(t, out, "wrote ", "TODO(s)", "the rest are PROSE", "a TODO left in a prompt ships")

	body := read(t, dst)
	if !strings.Contains(body, "name: 'slice-a'") {
		t.Fatalf("the name was not substituted: %.200s", body)
	}
	target := read(t, filepath.Join(wsDir, "gate.json"))
	if !strings.Contains(target, "target") {
		t.Fatal("the manifest has no target to read")
	}
	if strings.Contains(body, "TARGET_REPO") || strings.Contains(body, "SCRIPT_NAME") {
		t.Fatal("a placeholder survived substitution")
	}
	// The harness the tools cannot check, and which is why this exists at all.
	for _, form := range []string{"async function spawn(", "function live(", "function barFailed(", "YOU OWN", "READ-ONLY"} {
		if !strings.Contains(body, form) {
			t.Fatalf("the skeleton is missing %q", form)
		}
	}

	if _, err := runWF(t, wsDir, "new-script", "slice-a", "-o", dst); err == nil {
		t.Fatal("want a refusal over an existing file")
	}
	if got := read(t, dst); got != body {
		t.Fatal("the refused write changed the file")
	}
}

// A commit is its own ancestor, so `merge-base --is-ancestor X X` exits 0: ONE
// commit touching both a round and the record — what a hand `git commit -a`
// after filling feedback.md produces — walked through the staleness guard into
// an irreversible delete while the snapshot held the pre-edit rows.
func TestPruneRefusesARecordCommittedWithTheWorkItDescribes(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "close"); err != nil {
		t.Fatal(err)
	}

	// One commit, both trees.
	write(t, filepath.Join(wsDir, closeDir, "feedback.md"), "# Gate feedback\n\nfilled by hand\n")
	ledger(t, wsDir, 1, "| 1 | ADVISORY | filed after the record was written | prose |\n")
	gitIn(t, wsDir, "add", "-A")
	gitIn(t, wsDir, "commit", "-q", "-m", "close: fill the form and tidy the ledger")

	_, err := runWF(t, wsDir, "prune", "-yes")
	if err == nil {
		t.Fatal("want a refusal: the record cannot be older than a commit it shares")
	}
	if !strings.Contains(err.Error(), "wf close") {
		t.Fatalf("the refusal must name the repair: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wsDir, ".git")); err != nil {
		t.Fatal("the refusal deleted the repository")
	}
}

// The delta brief is the input the next round's reviewer was actually handed,
// and it lives in rounds/rN/, which prune deletes.
func TestCloseBanksTheDeltaBrief(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	tmp := filepath.Dir(wsDir)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	edits := filepath.Join(tmp, "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=reword\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", edits); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "second", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "delta", "-save"); err != nil {
		t.Fatal(err)
	}
	reviewedRound(t, wsDir, 2)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "close"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wsDir, closeDir, "rounds", "r2", "brief.md")); err != nil {
		t.Fatalf("the delta brief was not banked and prune would destroy it: %v", err)
	}
}

// The artifacts' CONTENT is the one thing prune destroys that the record does
// not hold. The default stands — their role ends when the reviewers are done —
// but prune must name them, and -artifacts must bank them.
func TestArtifactsAreNamedOrBanked(t *testing.T) {
	t.Run("prune names what leaves the world when they were not banked", func(t *testing.T) {
		wsDir := newGate(t)
		reviewedRound(t, wsDir, 1)
		if _, err := runWF(t, wsDir, "freeze"); err != nil {
			t.Fatal(err)
		}
		if _, err := runWF(t, wsDir, "close", "-artifacts=false"); err != nil {
			t.Fatal(err)
		}
		out, err := runWF(t, wsDir, "prune")
		noErr(t, err, "prune")
		mustContain(t, out, "the record does NOT hold the artifacts themselves",
			"artifacts/decisions.md", "-artifacts` banks them")
	})

	t.Run("banked by default", func(t *testing.T) {
		wsDir := newGate(t)
		reviewedRound(t, wsDir, 1)
		if _, err := runWF(t, wsDir, "freeze"); err != nil {
			t.Fatal(err)
		}
		out, err := runWF(t, wsDir, "close")
		noErr(t, err, "close")
		mustContain(t, out, "close/artifacts/decisions.md")
		if got := read(t, filepath.Join(wsDir, closeDir, "artifacts", "decisions.md")); !strings.Contains(got, "Entry point") {
			t.Fatalf("the artifact was banked empty: %q", got)
		}
		out, err = runWF(t, wsDir, "prune")
		noErr(t, err, "prune")
		mustNotContain(t, out, "the record does NOT hold the artifacts themselves")
	})
}

// A round the snapshot cannot read is a round prune would delete with no record
// of it. `wf freeze` once skipped an unreadable findings file with `continue`
// and printed its success block; a snapshot may not repeat that.
func TestCloseRefusesAnUnreadableRound(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "second", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	reviewedRound(t, wsDir, 2)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	// An EARLIER round, unreadable: the last round's ledger still parses, so
	// the blocker check passes and this guard is the one that must speak.
	// Unreadable rather than absent — removing it makes the tree dirty and the
	// first refusal fires instead, correctly, but on a different question.
	rd := filepath.Join(wsDir, "rounds", "r1")
	if err := os.Chmod(rd, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(rd, 0o755) })
	_, err := runWF(t, wsDir, "close")
	if err == nil {
		t.Fatal("want a refusal over a round the snapshot cannot read")
	}
	if !strings.Contains(err.Error(), "prune would delete the original") {
		t.Fatalf("the refusal must name the consequence: %v", err)
	}
}

// A field added to freezeManifest and not to the banked table is invisible:
// the reading is now the manifest's own type, so the compiler covers THAT half,
// but the TSV projection is written by hand and is meant to be — `artifacts`
// and `target` are dropped on purpose. That is exactly how the re-freeze count
// was lost for a release. So the projection is a DECLARED one: every manifest
// field is either a column or named here as dropped, and adding a field without
// deciding which fails this test rather than the next gate's pruned record.
func TestTheBankedTableDeclaresEveryManifestField(t *testing.T) {
	// Dropped deliberately: per-artifact hashes stop meaning anything once
	// `wf prune` removes the artifacts, and the target path is the one thing
	// a record about THIS workspace does not need to restate.
	dropped := map[string]bool{"artifacts": true, "target": true}

	rt := reflect.TypeOf(freezeManifest{})
	want := map[string]bool{}
	for i := 0; i < rt.NumField(); i++ {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if name == "" || dropped[name] {
			continue
		}
		want[name] = true
	}

	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "close"); err != nil {
		t.Fatal(err)
	}
	fz, err := os.ReadFile(filepath.Join(wsDir, closeDir, "freezes.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	header := strings.Split(strings.SplitN(strings.TrimSpace(string(fz)), "\n", 2)[0], "\t")
	got := map[string]bool{}
	for _, c := range header {
		got[c] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("freezeManifest.%s reaches no column of freezes.tsv and is not declared dropped — the pruned record would lose it silently", name)
		}
	}
	for _, c := range header {
		if !want[c] {
			t.Errorf("freezes.tsv column %q matches no freezeManifest field — the table promises something the manifest does not hold", c)
		}
	}

	// Header NAMES alone are decoration: the row still passes with the named
	// reader reverted to a hand-typed struct, or with a formatter writing the
	// zero value into a column that has a name. So each cell is checked against
	// the manifest it was projected from.
	// Trim the NEWLINE only: a trailing empty cell (an unset `supersedes`) is a
	// real column, and TrimSpace eats the tab in front of it.
	rows := strings.Split(strings.TrimRight(string(fz), "\n"), "\n")
	if len(rows) != 2 {
		t.Fatalf("want a header and one row: %q", fz)
	}
	cells := strings.Split(rows[1], "\t")
	if len(cells) != len(header) {
		t.Fatalf("the row has %d cell(s) against %d column(s): %q", len(cells), len(header), rows[1])
	}
	mb, err := os.ReadFile(filepath.Join(wsDir, "rounds", "r1", "freeze.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fm freezeManifest
	if err := json.Unmarshal(mb, &fm); err != nil {
		t.Fatal(err)
	}
	src := map[string]string{
		"round":         strconv.Itoa(fm.Round),
		"frozen":        fm.Frozen,
		"commit":        fm.Commit,
		"targetRef":     fm.TargetRef,
		"tool":          fm.Tool,
		"citeAnchored":  strconv.Itoa(fm.CiteAnchored),
		"citeTotal":     strconv.Itoa(fm.CiteTotal),
		"artifactLines": strconv.Itoa(fm.ArtifactLines),
		"refreezes":     strconv.Itoa(fm.Refreezes),
		"supersedes":    strings.Join(fm.Supersedes, " "),
	}
	for i, c := range header {
		v, known := src[c]
		if !known {
			t.Errorf("column %q is not projected from any manifest field this test knows", c)
			continue
		}
		if cells[i] != v {
			t.Errorf("column %q carries %q, the manifest says %q — a named column is not a written one", c, cells[i], v)
		}
	}
	// And the values must not be uniformly empty: a projection writing the zero
	// value everywhere would satisfy a comparison against a zeroed manifest.
	nonEmpty := 0
	for _, c := range cells {
		if c != "" && c != "0" {
			nonEmpty++
		}
	}
	if nonEmpty < 4 {
		t.Fatalf("only %d cell(s) carry a value; the row proves nothing: %q", nonEmpty, rows[1])
	}
}
