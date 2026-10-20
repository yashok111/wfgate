// Two holes through which a gate can end without anyone having looked, and one
// record that makes every claim about WHO looked falsifiable.
//
// The first hole is live and measured: a decision gate's round 9 was opened,
// frozen as `gate-r9` and never dispatched, and `wf stats` printed `round 9 —
// 0 finding(s), 0 blocker(s)` — the same line a round that ended a gate under
// the stop rule prints. The second is the roster: the same gate's blocker
// counts separated 1.76x by the model running one vector, and nothing in the
// workspace recorded which model that was.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yashok111/wfgate/internal/ws"
)

// promptFile writes a reviewer vector to a file outside the workspace, the way
// an operator hands one to a model.
func promptFile(t *testing.T, wsDir, name, body string) string {
	t.Helper()
	p := filepath.Join(filepath.Dir(wsDir), name)
	write(t, p, body)
	return p
}

// The flagship refusal. A round that was opened, frozen and never reviewed
// leaves the same ledger as a round whose reviewers found nothing, and under
// the stop rule the second ends the gate.
func TestFreezeRefusesToPublishARoundNobodyIsRecordedAsHavingRead(t *testing.T) {
	wsDir := newGate(t)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "second", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "freeze")
	if err == nil {
		t.Fatalf("freeze must refuse to publish an unreviewed round as an empty one, got:\n%s", out)
	}
	mustContain(t, err.Error(), "round(s) 1", "untouched scaffold", "no lens is recorded", "wf round lens")
	// The repair the message LEADS with must be one that works for the case the
	// refusal is about: `wf round record` over an untouched ledger has no row to
	// write and exits 1, so it cannot come first.
	if strings.Index(err.Error(), "wf round lens") > strings.Index(err.Error(), "wf round record") {
		t.Fatalf("the followable repair must lead: %v", err)
	}
	// The refusal comes BEFORE the write, like every other one here.
	if _, e := os.Stat(filepath.Join(wsDir, "rounds", "r2", "freeze.json")); e == nil {
		t.Fatal("a refused freeze must not leave a manifest behind")
	}
}

// The escape is a RECORD, not a flag: a round whose vectors ran and found
// nothing says so by banking them, and that is a claim the corpus can check.
func TestFreezeAcceptsAnEmptyRoundWhoseLensesWereBanked(t *testing.T) {
	wsDir := newGate(t)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	prompt := promptFile(t, wsDir, "criterion.md", "Read D13 and name the implementation each clause fails.\n")
	if _, err := runWF(t, wsDir, "round", "lens", "-asks", "other", prompt, "-name", "CRITERION", "-model", "external gpt-5.6-sol", "-round", "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "second", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze after a banked roster")
	mustContain(t, out, "frozen round 2")
}

// A `tool` round declares in its opening subject — the one line nothing
// rewrites — that no reviewer was dispatched, so its empty ledger is the round
// saying so rather than a silence.
func TestFreezeExemptsAToolRound(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "wfcheck findings", "-kind", "tool"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "third", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze after a tool round")
	mustContain(t, out, "frozen round 3")
}

// The open round is frozen right after it is opened, so its scaffold is blank
// BY CONSTRUCTION. A refusal that reached it would refuse the prescribed flow
// at its first step.
func TestFreezeStillPublishesTheOpenRoundsBlankScaffold(t *testing.T) {
	wsDir := newGate(t)
	out, err := runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze r1")
	mustContain(t, out, "frozen round 1")
	mustNotContain(t, out, "untouched scaffold")
}

// The gate ends on a reviewer round that produced no BLOCKER, so the last
// round's ledger is the evidence the close rests on.
func TestCloseRefusesAnUnfilledLastRound(t *testing.T) {
	wsDir := newGate(t)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "close")
	if err == nil {
		t.Fatalf("close must refuse to bank an unreviewed round as the one that ended the gate, got:\n%s", out)
	}
	mustContain(t, err.Error(), "round(s) 1", "UNFILLED findings scaffold", "record no lens", "wf round lens")
	if _, e := os.Stat(filepath.Join(wsDir, closeDir)); !os.IsNotExist(e) {
		t.Fatal("the refusal wrote a snapshot anyway")
	}

	prompt := promptFile(t, wsDir, "facts.md", "Check every citation in the artifact against the target.\n")
	if _, err := runWF(t, wsDir, "round", "lens", "-asks", "other", prompt, "-name", "FACTS", "-model", "opus"); err != nil {
		t.Fatal(err)
	}
	out, err = runWF(t, wsDir, "close")
	noErr(t, err, "close after a banked roster")
	// The roster is banked with the round, not left in the repository prune
	// deletes: `wf close` copies the FILES of a round and skips directories.
	mustContain(t, out, "close/rounds/r1/lens-facts.md")
	if got := read(t, filepath.Join(wsDir, closeDir, "rounds", "r1", "lens-facts.md")); !strings.Contains(got, "Check every citation") {
		t.Fatalf("the prompt itself must survive the record: %q", got)
	}
}

// Both holes, in the report an operator actually reads when deciding whether
// the descent reached zero.
func TestStatsSeparatesAnEmptyFormFromAMeasuredZero(t *testing.T) {
	wsDir := newGate(t)
	prompt := promptFile(t, wsDir, "criterion.md", "Name the implementation each clause fails.\n")
	if _, err := runWF(t, wsDir, "round", "lens", "-asks", "other", prompt, "-name", "CRITERION", "-model", "external gpt-5.6-sol"); err != nil {
		t.Fatal(err)
	}
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "second", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	// Round 2 is frozen unreviewed and round 3 is a `tool` round: the second
	// says in its own opening subject that no reviewer was dispatched, so it is
	// not a silence and must not be named beside the first.
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "wfcheck findings", "-kind", "tool"); err != nil {
		t.Fatal(err)
	}

	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out,
		"lenses: CRITERION (external gpt-5.6-sol, asks other)",
		"ledger: UNFILLED scaffold, not a measured zero",
		"round(s) carry an UNFILLED findings scaffold (2)",
		"round(s) record NO lens (2)")
	// Round 1 was reviewed and its roster is banked; round 3 dispatched no
	// reviewer by declaration. Neither note may name either.
	mustNotContain(t, out, "scaffold (1", "scaffold (2, 3)", "NO lens (1", "NO lens (2, 3)")
}

// The record itself: what ran the vector, and what it was asked. A roster
// entry that cannot say WHICH model is the claim the corpus cannot check.
func TestLensBanksTheVectorAndRefusesAnUnnamedOne(t *testing.T) {
	wsDir := newGate(t)
	prompt := promptFile(t, wsDir, "seams.md", "List every seam the diff crosses.\n")

	out, err := runWF(t, wsDir, "round", "lens", "-asks", "other", prompt, "-name", "SEAMS", "-model", "opus")
	noErr(t, err, "round lens")
	mustContain(t, out, "rounds/r1/lens-seams.md", "committed ")

	body := read(t, filepath.Join(wsDir, "rounds", "r1", "lens-seams.md"))
	mustContain(t, body, "# Lens SEAMS", "Model: opus", "List every seam the diff crosses.")

	out, err = runWF(t, wsDir, "round", "lens")
	noErr(t, err, "roster")
	mustContain(t, out, "round 1 — 1 lens(es)", "SEAMS", "opus")

	if _, err := runWF(t, wsDir, "round", "lens", "-asks", "other", prompt, "-name", "FACTS"); err == nil {
		t.Fatal("a roster entry without a model records that a round had lenses and not which")
	}
	empty := promptFile(t, wsDir, "empty.md", "\n")
	if _, err := runWF(t, wsDir, "round", "lens", "-asks", "other", empty, "-name", "FACTS", "-model", "opus"); err == nil {
		t.Fatal("an empty prompt records that a lens ran and not what it was asked")
	}
}

// A banked vector is not rewritten. The prompt a round was ACTUALLY run with is
// the whole point of the file, and a second recording would replace it with the
// prompt the operator wishes had run — the findings lesson, one file over.
func TestLensNeverRewritesABankedPrompt(t *testing.T) {
	wsDir := newGate(t)
	first := promptFile(t, wsDir, "v1.md", "The vector as it ran.\n")
	if _, err := runWF(t, wsDir, "round", "lens", "-asks", "other", first, "-name", "CRITERION", "-model", "opus"); err != nil {
		t.Fatal(err)
	}
	// Identical bytes are reported, not re-committed — an unchanged re-save is
	// bookkeeping, and `wf delta -save` set that precedent.
	out, err := runWF(t, wsDir, "round", "lens", "-asks", "other", first, "-name", "CRITERION", "-model", "opus")
	noErr(t, err, "re-record")
	mustContain(t, out, "unchanged — not re-committed")

	second := promptFile(t, wsDir, "v2.md", "The vector as I wish it had run.\n")
	if _, err := runWF(t, wsDir, "round", "lens", "-asks", "other", second, "-name", "CRITERION", "-model", "opus"); err == nil {
		t.Fatal("a banked prompt must not be rewritten")
	}
	if got := read(t, filepath.Join(wsDir, "rounds", "r1", "lens-criterion.md")); !strings.Contains(got, "as it ran") {
		t.Fatalf("the banked prompt was replaced: %q", got)
	}
}

// A roster read from the round directory, not from an index: an index is a
// second copy of a derivation, and this project names that as its most-repeated
// defect class.
func TestRoundLensesReadsWhatWasBanked(t *testing.T) {
	wsDir := newGate(t)
	for _, l := range []struct{ name, model string }{
		{"CRITERION", "external gpt-5.6-sol"},
		{"FACTS", "opus"},
	} {
		p := promptFile(t, wsDir, strings.ToLower(l.name)+".md", "vector "+l.name+"\n")
		if _, err := runWF(t, wsDir, "round", "lens", "-asks", "other", p, "-name", l.name, "-model", l.model); err != nil {
			t.Fatal(err)
		}
	}
	w, err := ws.Find(wsDir)
	if err != nil {
		t.Fatal(err)
	}
	got := lensList(roundLenses(w, 1))
	if got != "CRITERION (external gpt-5.6-sol, asks other), FACTS (opus, asks other)" {
		t.Fatalf("roster: %q", got)
	}
	if len(roundLenses(w, 2)) != 0 {
		t.Fatal("a round with no directory has no roster, and must not borrow one")
	}
}

// A `tool` round as the LAST round: its empty ledger is a declaration, not a
// silence, so close banks it — and says that the gate's last round dispatched
// no reviewer, because the stop rule reads the REVIEWER descent.
func TestCloseBanksAToolRoundAndNamesIt(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "wfdry findings", "-kind", "tool"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "close")
	noErr(t, err, "close on a tool round")
	mustContain(t, out, "round 2 is a `tool` round", "the stop rule reads the REVIEWER descent")
}

// The hole neither the write path nor the first read path saw: `wf round new`
// does not require the current round to be frozen, so a round opened PAST
// without a freeze was examined by no refusal — reproduced end to end, a gate
// closed carrying an undispatched round 2 that only `wf stats` named.
func TestFreezeRefusesARoundSteppedPast(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	// Round 2 is opened, never dispatched, and never frozen; round 3 is opened
	// on top of it and reviewed properly.
	if _, err := runWF(t, wsDir, "round", "new", "-title", "second", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "third", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	reviewedRound(t, wsDir, 3)
	out, err := runWF(t, wsDir, "freeze")
	if err == nil {
		t.Fatalf("a round stepped past must not be published by a later freeze, got:\n%s", out)
	}
	mustContain(t, err.Error(), "round(s) 2")
}

// The escape from both refusals is a RECORD, and the first version counted a
// FILENAME: `touch rounds/r1/lens-anything.md` cleared a refusal that had just
// fired, and the roster then printed a vector with no model — the one thing the
// write path refuses to record.
func TestAnEmptyLensFileIsNotARecord(t *testing.T) {
	wsDir := newGate(t)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(wsDir, "rounds", "r1", "lens-anything.md"), "")
	gitIn(t, wsDir, "add", "-A")
	gitIn(t, wsDir, "commit", "-qm", "a file that is not a record")
	if _, err := runWF(t, wsDir, "round", "new", "-title", "second", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "freeze")
	if err == nil {
		t.Fatalf("an empty file must not clear the refusal, got:\n%s", out)
	}
	mustContain(t, err.Error(), "round(s) 1")

	out, err = runWF(t, wsDir, "round", "lens", "-round", "1")
	noErr(t, err, "roster")
	mustContain(t, out, "0 of them a record", "NOT a record")

	// And the report counts records, not files: a round whose only lens file is
	// not a record records no lens.
	out, err = runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out, "record NO lens (1, 2)", "NOT a record: no `---` fence")
}

// What a lens file must SAY to count, one missing part at a time. The write path
// cannot produce any of these; a hand-edited or half-written file can, and the
// roster is what both refusals consume.
func TestALensFileCountsOnlyWhenItSaysWhatRanTheVector(t *testing.T) {
	cases := []struct {
		name, body, why string
	}{
		{"no fence", "# Lens FACTS\n\nModel: opus\n", "no `---` fence"},
		{"no header", "Model: opus\n\n---\n\nthe prompt\n", "no `# Lens` header"},
		{"no model in the header", "# Lens FACTS\n\n---\n\nModel: opus\nthe prompt\n", "no `Model:` line"},
		{"no prompt", "# Lens FACTS\n\nModel: opus\n\n---\n\n   \n", "no prompt after the fence"},
	}
	for _, c := range cases {
		wsDir := newGate(t)
		write(t, filepath.Join(wsDir, "rounds", "r1", "lens-facts.md"), c.body)
		gitIn(t, wsDir, "add", "-A")
		gitIn(t, wsDir, "commit", "-qm", "half a record")
		out, err := runWF(t, wsDir, "round", "lens")
		noErr(t, err, "roster")
		mustContain(t, out, "0 of them a record", c.why)
	}
}

// A hand-edited file with the header twice: the FIRST line wins, so an appended
// second claim cannot quietly become the record of what ran the vector.
func TestTheFirstHeaderLineIsTheRecord(t *testing.T) {
	wsDir := newGate(t)
	write(t, filepath.Join(wsDir, "rounds", "r1", "lens-crit.md"),
		"# Lens CRITERION\n# Lens EVIL\n\nModel: opus\nModel: gpt-4o-mini\n\n---\n\nthe prompt\n")
	gitIn(t, wsDir, "add", "-A")
	gitIn(t, wsDir, "commit", "-qm", "a header edited twice")
	out, err := runWF(t, wsDir, "round", "lens")
	noErr(t, err, "roster")
	mustContain(t, out, "CRITERION", "opus")
	mustNotContain(t, out, "EVIL", "gpt-4o-mini")
}

// A lens that is not COMMITTED is not part of what the freeze pins: the roster
// reads the worktree, and an uncommitted file would clear a refusal and then not
// exist in the tag that refusal was protecting.
func TestAnUncommittedLensIsNotARecord(t *testing.T) {
	wsDir := newGate(t)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(wsDir, "rounds", "r1", "lens-facts.md"),
		"# Lens FACTS\n\nModel: opus\nRecorded: 2026-08-24T00:00:00Z\n\n---\n\nCheck every citation.\n")
	if _, err := runWF(t, wsDir, "round", "new", "-title", "second", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "freeze")
	if err == nil {
		t.Fatalf("an uncommitted lens must not clear the refusal, got:\n%s", out)
	}
	mustContain(t, err.Error(), "round(s) 1")

	// And the command that owns the file commits what it finds uncommitted,
	// rather than reporting it as already banked: a failed commit used to leave
	// it on disk forever. The banked header here states no questions, so this
	// is also the ADDITION path — the same commit, under the message that says
	// which half was missing.
	prompt := promptFile(t, wsDir, "facts.md", "Check every citation.\n")
	out, err = runWF(t, wsDir, "round", "lens", "-asks", "other", prompt, "-name", "FACTS", "-model", "opus", "-round", "1")
	noErr(t, err, "re-record an uncommitted lens")
	mustContain(t, out, "committed")
	if !strings.Contains(out, "predates -asks") && !strings.Contains(out, "on disk and uncommitted") {
		t.Fatalf("the commit does not say WHY it happened: %s", out)
	}
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatalf("a committed lens must clear the refusal: %v", err)
	}
}

// A lens banked against a round the gate has not reached pre-authorises exactly
// what the guard exists to refuse; an unbounded -round also reported success
// into a round directory it invented.
func TestLensRefusesARoundTheGateDoesNotHave(t *testing.T) {
	wsDir := newGate(t)
	prompt := promptFile(t, wsDir, "crit.md", "the vector\n")
	// 0 is the flag's own sentinel for "the open round", so it is not an error;
	// a round the gate has not reached, and a negative one, are.
	for _, n := range []string{"2", "-3", "99"} {
		out, err := runWF(t, wsDir, "round", "lens", "-asks", "other", prompt, "-name", "CRITERION", "-model", "opus", "-round", n)
		if err == nil {
			t.Fatalf("-round %s must be refused in a one-round gate, got:\n%s", n, out)
		}
		// The refusal must say what is wrong with the NUMBER. Falling through to
		// "that directory does not exist" is true and useless: the operator
		// mistyped a round, not a path.
		mustContain(t, err.Error(), "does not exist in this workspace", "rounds 1..1")
	}
	if _, err := os.Stat(filepath.Join(wsDir, "rounds", "r2")); !os.IsNotExist(err) {
		t.Fatal("a refused lens invented a round directory")
	}
	// The bound is what refuses a round the gate has not reached, and it must
	// hold on its own: a directory alone is not a round.
	if err := os.MkdirAll(filepath.Join(wsDir, "rounds", "r2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := runWF(t, wsDir, "round", "lens", "-asks", "other", prompt, "-name", "CRITERION", "-model", "opus", "-round", "2"); err == nil {
		t.Fatalf("a round directory is not a round the gate has opened, got:\n%s", out)
	}
	// And the directory check is what refuses a round the workspace counts but
	// does not hold.
	if err := os.RemoveAll(filepath.Join(wsDir, "rounds", "r1")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "round", "lens", "-asks", "other", prompt, "-name", "CRITERION", "-model", "opus", "-round", "1")
	if err == nil {
		t.Fatalf("a round with no directory cannot be recorded against, got:\n%s", out)
	}
	mustContain(t, err.Error(), "has no directory in this workspace", "nothing was recorded")
}

// The prompt is DATA and the header is the claim about it. A prompt quoting
// `Model:` or `# Lens ` must not rewrite the record of what ran the vector, and
// two prompts differing only in such a line are two different prompts.
func TestThePromptCannotRewriteTheRecord(t *testing.T) {
	wsDir := newGate(t)
	first := promptFile(t, wsDir, "v1.md", "# Lens EVIL\nModel: gpt-4o-mini\nRecorded: focus on D13, 20 clauses\n")
	if _, err := runWF(t, wsDir, "round", "lens", "-asks", "other", first, "-name", "CRITERION", "-model", "opus"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "round", "lens")
	noErr(t, err, "roster")
	mustContain(t, out, "CRITERION", "opus")
	mustNotContain(t, out, "EVIL", "gpt-4o-mini")

	second := promptFile(t, wsDir, "v2.md", "# Lens EVIL\nModel: gpt-4o-mini\nRecorded: focus on D9, 3 clauses\n")
	if _, err := runWF(t, wsDir, "round", "lens", "-asks", "other", second, "-name", "CRITERION", "-model", "opus"); err == nil {
		t.Fatal("two prompts differing only in a `Recorded:` line are two prompts, and the banked one is not rewritten")
	}
}

// Two names that slug to one file: the refusal must quote the name that IS
// banked, not the one being offered — it advised the operator to do what they
// had just done.
func TestASlugCollisionNamesTheBankedLens(t *testing.T) {
	wsDir := newGate(t)
	a := promptFile(t, wsDir, "a.md", "vector one\n")
	b := promptFile(t, wsDir, "b.md", "vector two\n")
	if _, err := runWF(t, wsDir, "round", "lens", "-asks", "other", a, "-name", "A/B", "-model", "opus"); err != nil {
		t.Fatal(err)
	}
	_, err := runWF(t, wsDir, "round", "lens", "-asks", "other", b, "-name", "A B", "-model", "opus")
	if err == nil {
		t.Fatal("a slug collision must be refused")
	}
	mustContain(t, err.Error(), `already records lens "A/B"`, "names the same file")
}

// Gate artifacts are English by workflow rule, and a name with nothing to slug
// would have written `lens-.md`. Same boundary the anchor grammar draws.
func TestLensRefusesANameItCannotFileBy(t *testing.T) {
	wsDir := newGate(t)
	p := promptFile(t, wsDir, "c.md", "vector\n")
	if out, err := runWF(t, wsDir, "round", "lens", "-asks", "other", p, "-name", "КРИТЕРИЙ", "-model", "opus"); err == nil {
		t.Fatalf("a name with no ASCII alphanumerics has nothing to file by, got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(wsDir, "rounds", "r1", "lens-.md")); !os.IsNotExist(err) {
		t.Fatal("the refused name wrote a file anyway")
	}
}

// A prompt path a shell globbed away must not silently discard the flags and
// print somebody else's roster with exit 0.
func TestLensWithFlagsAndNoFileIsAnError(t *testing.T) {
	wsDir := newGate(t)
	if out, err := runWF(t, wsDir, "round", "lens", "-asks", "other", "-name", "FACTS", "-model", "opus"); err == nil {
		t.Fatalf("a missing prompt with flags set is a mistyped argument, got:\n%s", out)
	}
	a := promptFile(t, wsDir, "a.md", "one\n")
	b := promptFile(t, wsDir, "b.md", "two\n")
	if out, err := runWF(t, wsDir, "round", "lens", "-asks", "other", a, b, "-name", "F", "-model", "o"); err == nil {
		t.Fatalf("two prompt files are a usage error, got:\n%s", out)
	}
}

// Only `lens-*.md` FILES are the roster. A directory of that name, and a
// `lens-*.txt` beside it, are neither records nor reports.
func TestOnlyLensMarkdownFilesAreTheRoster(t *testing.T) {
	wsDir := newGate(t)
	if err := os.MkdirAll(filepath.Join(wsDir, "rounds", "r1", "lens-dir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(wsDir, "rounds", "r1", "lens-notes.txt"), "scratch\n")
	out, err := runWF(t, wsDir, "round", "lens")
	noErr(t, err, "roster")
	mustContain(t, out, "NO lens recorded")
}

// The other half of the distinction, in the report where the zero is read: a
// round whose ledger is empty AND whose lenses are banked is a measured zero,
// and the report must not go on calling it an empty form.
func TestStatsCallsABankedEmptyRoundAMeasuredZero(t *testing.T) {
	wsDir := newGate(t)
	prompt := promptFile(t, wsDir, "crit.md", "Name the implementation each clause fails.\n")
	if _, err := runWF(t, wsDir, "round", "lens", "-asks", "other", prompt, "-name", "CRITERION", "-model", "opus"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out, "ledger: empty, and its lenses are banked — a measured zero")
	mustNotContain(t, out, "UNFILLED scaffold", "record NO lens")
}

// The "it takes a path" clause is attached only where the argument LOOKS like a
// pasted prompt. Measured need: a live gate pasted one on the command line
// twice and got the syscall's `file name too long` with nothing saying what was
// wanted. Measured cost of over-attaching it: an operator who simply mistyped a
// path is told they pasted a prompt, which is a refusal pointing at the wrong
// mistake.
func TestLensReadErrorNamesThePathOnlyWhereItLooksLikeAPrompt(t *testing.T) {
	wsDir := newGate(t)

	long := strings.Repeat("a review vector that reads the delta and reports blockers. ", 80)
	_, err := runWF(t, wsDir, "round", "lens", long, "-name", "n", "-model", "m", "-asks", "closure")
	if err == nil {
		t.Fatal("a pasted prompt must fail")
	}
	mustContain(t, err.Error(), "takes the PATH of a file holding the prompt")

	_, err = runWF(t, wsDir, "round", "lens", filepath.Join(wsDir, "no-such-lens.md"), "-name", "n", "-model", "m", "-asks", "closure")
	if err == nil {
		t.Fatal("a missing file must fail")
	}
	if strings.Contains(err.Error(), "takes the PATH") {
		t.Fatalf("a mistyped path must get the plain error: %v", err)
	}
}
