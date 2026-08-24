package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `wf check stale` end to end: the sweep `wf apply` has been advising since the
// cost meter existed. The gate that produced the requirement paid for this class
// in every one of five rounds — a fix changed a rule and its consumers, which
// are not in the diff, went on stating the old one.
func TestCheckStale_findsTheConsumerAFixLeftStanding(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	// The rule, as it stood when the round was frozen.
	seed := filepath.Join(tmp, "seed.txt")
	write(t, seed, "=== artifacts/decisions.md  count=1  note=state the rule\n"+
		"<<<<\nEntry point: `src/main.go:3#NeedleFn`.\n====\n"+
		"The sequence counter is bumped in place by one on every single write.\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", seed); err != nil {
		t.Fatal(err)
	}
	// A second artifact that argues FROM the rule, in its own words.
	other := filepath.Join(tmp, "context.md")
	write(t, other, "# Context\n\n"+
		"Because the sequence counter is bumped in place, a reader never observes any gap.\n")
	if _, err := runWF(t, wsDir, "add", other); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}

	// The fix: the rule's own artifact stops saying "bumped in place". Nothing
	// touches the artifact that argues from it.
	batch := filepath.Join(tmp, "fix.txt")
	write(t, batch, "=== artifacts/decisions.md  count=1  note=allocator, not an in-place bump\n"+
		"<<<<\nThe sequence counter is bumped in place by one on every single write.\n====\n"+
		"The allocator hands out a fresh version identifier on every single write.\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", batch); err != nil {
		t.Fatal(err)
	}

	out, err := runWF(t, wsDir, "check", "stale")
	if !errors.Is(err, errFindings) {
		t.Fatalf("a standing consumer is a finding, got %v\n%s", err, out)
	}
	mustContain(t, out, "## stale — gate-r1..HEAD", "artifacts/context.md:3", "bumped")
	if !strings.Contains(out, "judgement") {
		t.Fatalf("the report must say the reading stays the operator's:\n%s", out)
	}
}

// The population that decides whether "0 sentence(s)" is a measurement. A range
// that retired no rare vocabulary swept nothing, and a green line over it is the
// vacuous pass this project names as its worst failure.
func TestCheckStale_saysWhenItSweptNothing(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	batch := filepath.Join(tmp, "add.txt")
	write(t, batch, "=== artifacts/decisions.md  count=1  note=append only\n"+
		"<<<<\n# Decisions\n====\n# Decisions\n\nAn added sentence that removes nothing at all from the document.\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", batch); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "stale")
	noErr(t, err, "check stale")
	mustContain(t, out, "NOT A PASS", "retired no vocabulary at all")
}

// Without a reference there is no fix range, and a check that silently swept
// nothing would print a clean line over it.
func TestCheckStale_refusesWithNothingToMeasureFrom(t *testing.T) {
	wsDir := newGate(t)
	out, err := runWF(t, wsDir, "check", "stale")
	if err == nil {
		t.Fatalf("want a refusal before any freeze exists:\n%s", out)
	}
	if !strings.Contains(err.Error(), "-since") {
		t.Fatalf("the refusal must name the way out: %v", err)
	}
}

// `wf check closure`: `wf round record` proves the sha exists; this proves the
// commit touched what the finding names. Measured on a live decision gate — two
// reviewer lenses, told by hand-written prompt to check exactly this, reported 6
// and 4 of 24 closures not closed, three rounds running.
func TestCheckClosure_flagsACommitThatTouchedSomethingElse(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	other := filepath.Join(tmp, "context.md")
	write(t, other, "# Context\n\nA second artifact nobody is going to fix in this round.\n")
	if _, err := runWF(t, wsDir, "add", other); err != nil {
		t.Fatal(err)
	}
	// The "fix": it changes decisions.md and nothing else.
	batch := filepath.Join(tmp, "fix.txt")
	write(t, batch, "=== artifacts/decisions.md  count=1  note=a fix\n"+
		"<<<<\n# Decisions\n====\n# Decisions (revised)\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", batch)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, applied)

	ledger(t, wsDir, 1,
		"| 1 | BLOCKER | the rule in artifacts/context.md is wrong | "+sha+" fixed |\n"+
			"| 2 | MAJOR | artifacts/decisions.md needs a revision | "+sha+" fixed |\n"+
			"| 3 | MAJOR | a finding naming no artifact at all | "+sha+" fixed |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}

	out, err := runWF(t, wsDir, "check", "closure")
	if !errors.Is(err, errFindings) {
		t.Fatalf("a closure that missed its artifact is a finding, got %v\n%s", err, out)
	}
	mustContain(t, out,
		"round 1 [1] closed by "+sha,
		"did not touch artifacts/context.md",
		"1 closure(s) name no artifact path")
	// The closure that DID land must not be reported.
	if strings.Contains(out, "[2] closed by") {
		t.Fatalf("a closure whose commit touched the named artifact is closed:\n%s", out)
	}
}

// The line-level half, and the caveat printed with it: a later batch in the same
// round shifts the numbers, so a LINE miss is a question and a FILE miss is a
// verdict. Reporting the first as the second would teach the operator to ignore
// both.
func TestCheckClosure_lineMissIsReportedAsAQuestion(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	batch := filepath.Join(tmp, "fix.txt")
	write(t, batch, "=== artifacts/decisions.md  count=1  note=a fix at the top\n"+
		"<<<<\n# Decisions\n====\n# Decisions (revised)\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", batch)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, applied)

	// The finding cites line 3; the commit changed line 1.
	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/decisions.md:3 states it wrongly | "+sha+" fixed |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "closure")
	if !errors.Is(err, errFindings) {
		t.Fatalf("want a finding, got %v\n%s", err, out)
	}
	mustContain(t, out, "but not line 3", "SECTION or LINE miss is a question to re-ask")
}

// `check rules` compares copies of one id against each other and nothing else.
// Measured: `10 marker(s), 5 id(s), 5 compared, 0 conflict(s)` was true about
// copy agreement and was read as a statement about correctness, while both
// copies of one rule had gone false against the section arguing for them.
func TestCheckRules_namesTheQuestionItAnswers(t *testing.T) {
	wsDir := newGate(t)
	out, err := runWF(t, wsDir, "check", "rules")
	noErr(t, err, "check rules")
	mustContain(t, out,
		"scope: this compares the SITES of one [RN] id",
		"never asks whether a rule still agrees with the section that ARGUES for it",
		"`wf check stale`")
}

// One number per freeze: a corpus that doubles while the blocker count holds is
// a design being elaborated rather than settled, and nothing else in a workspace
// shows it.
func TestFreezeAndStatsPrintTheCorpusSize(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	out, err := runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze")
	mustContain(t, out, "size   3 line(s) across 1 artifact(s)")
	if strings.Contains(out, "since round") {
		t.Fatalf("the first freeze has no baseline to compare against:\n%s", out)
	}

	ledger(t, wsDir, 1, "| 1 | MAJOR | one finding | prose |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	batch := filepath.Join(tmp, "grow.txt")
	write(t, batch, "=== artifacts/decisions.md  count=1  note=elaborate\n"+
		"<<<<\n# Decisions\n====\n# Decisions\n\nOne more line.\n\nAnd another one.\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", batch); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "second", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	out, err = runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze 2")
	mustContain(t, out, "size   7 line(s) across 1 artifact(s) (+4 since round 1)")

	out, err = runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out, "artifact 7 line(s) (+4)")
}

// The authoring window prices a corpus its own batches did not write. It printed
// 377.8 finding(s) per 100 changed lines on a live gate and `wf apply` predicted
// ~355 findings for a round that produced 24 — a number in the register of a
// measurement, derived from a window that measured nothing.
func TestStats_authoringWindowPrintsNoRatio(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	batch := filepath.Join(tmp, "fix.txt")
	write(t, batch, "=== artifacts/decisions.md  count=1  note=a small fix\n"+
		"<<<<\n# Decisions\n====\n# Decisions (revised)\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", batch); err != nil {
		t.Fatal(err)
	}
	rows := ""
	for i := 1; i <= 20; i++ {
		rows += "| " + itoa(i) + " | MAJOR | finding " + itoa(i) + " | prose |\n"
	}
	ledger(t, wsDir, 1, rows)
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	mustContain(t, out, "an artifact was ADDED in this window, so there is no ratio")
	if strings.Contains(out, "finding(s) per 100 changed line(s))") {
		t.Fatalf("no per-round ratio may be printed for that window:\n%s", out)
	}
	// And nothing predicts from it either.
	if strings.Contains(out, "prediction basis") {
		t.Fatalf("the only round is an authoring window, so there is no basis:\n%s", out)
	}
}

// commitShaOf pulls the short sha `wf apply` prints, which is what an operator
// pastes into the ledger's closed-by cell.
func commitShaOf(t *testing.T, applyOut string) string {
	t.Helper()
	for _, l := range strings.Split(applyOut, "\n") {
		if i := strings.Index(l, "committed "); i >= 0 {
			return strings.Fields(l[i+len("committed "):])[0]
		}
	}
	t.Fatalf("no commit line in:\n%s", applyOut)
	return ""
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// A real ledger cites SECTIONS. Measured on a live decision gate: 126 of 127
// closures named no path at all, so a closure check that could only resolve
// paths reached ONE finding out of 127 — honest, and useless. It resolves the
// section the finding cites and asks whether the closing commit's diff reaches
// it.
//
// ANY cited section, against the UNION of the closing commits. Demanding every
// one of them, per commit, flagged 89 of that gate's 127 closures; the shape
// below is the 12 that survive, and it is `dup`'s lesson applied before the
// check shipped rather than after.
func TestCheckClosure_reachesTheSectionAFindingCites(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	doc := "# Decisions\n\n## D1 — the first\n\none\n\n## D2 — the second\n\ntwo\n\n## D3 — the third\n\nthree\n"
	seed := filepath.Join(tmp, "seed.txt")
	write(t, seed, "=== artifacts/decisions.md  count=1  note=sections\n"+
		"<<<<\n# Decisions\n\nEntry point: `src/main.go:3#NeedleFn`.\n====\n"+doc+">>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", seed); err != nil {
		t.Fatal(err)
	}

	// A fix inside D3 only.
	fix := filepath.Join(tmp, "fix.txt")
	write(t, fix, "=== artifacts/decisions.md  count=1  note=fix D3\n<<<<\nthree\n====\nthree, corrected\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", fix)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, applied)

	ledger(t, wsDir, 1,
		"| 1 | BLOCKER | D1 states the rule wrongly | "+sha+" fixed |\n"+
			"| 2 | MAJOR | D3 needs a correction | "+sha+" fixed |\n"+
			"| 3 | MAJOR | D2 and D3 disagree | "+sha+" fixed |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}

	out, err := runWF(t, wsDir, "check", "closure")
	if !errors.Is(err, errFindings) {
		t.Fatalf("a fix that never entered the cited section is a finding, got %v\n%s", err, out)
	}
	mustContain(t, out, "[1] closed by "+sha, "NOT any section it cites: D1",
		"checked against artifacts/decisions.md, the workspace's only artifact")
	// The finding whose section WAS touched is closed, and so is the one that
	// cites two sections of which one was touched — a finding mentioning a
	// neighbouring section in passing is not a finding about it.
	if strings.Contains(out, "[2] closed by") || strings.Contains(out, "[3] closed by") {
		t.Fatalf("any cited section reached is a closure:\n%s", out)
	}
	// The weaker half must say which half it ran.
	mustContain(t, out, "a FILE miss is a closure that did not land", "SECTION or LINE miss is a question")
}

// A finding citing no section at all falls back to the file, and the report has
// to say that a commit touching the document anywhere passed that test — the
// difference between the two claims is the difference between a check and a
// formality.
func TestCheckClosure_namesTheWeakerHalfItRan(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	fix := filepath.Join(tmp, "fix.txt")
	write(t, fix, "=== artifacts/decisions.md  count=1  note=a fix\n<<<<\n# Decisions\n====\n# Decisions (revised)\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", fix)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, applied)
	ledger(t, wsDir, 1, "| 1 | MAJOR | the wording is loose | "+sha+" fixed |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	mustContain(t, out, "1 closure(s) cite neither a section this artifact heads nor a line", "only at FILE level")
}

// A range that retired plenty and finds none of it left is a COMPLETE sweep —
// the best outcome this check has. It printed as `NOT A PASS, nothing was
// swept`, because a full sweep and an empty range both leave the reported
// vocabulary empty: the good case reported as the vacuous one, in the tool
// built to refuse exactly that.
func TestCheckStale_aCompleteSweepIsNotAVacuousOne(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	seed := filepath.Join(tmp, "seed.txt")
	write(t, seed, "=== artifacts/decisions.md  count=1  note=state the rule\n"+
		"<<<<\nEntry point: `src/main.go:3#NeedleFn`.\n====\n"+
		"The sequence counter is bumped in place by one on every single write.\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", seed); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	// The fix reaches the only sentence that carried the vocabulary: nothing is
	// left to re-read anywhere.
	fix := filepath.Join(tmp, "fix.txt")
	write(t, fix, "=== artifacts/decisions.md  count=1  note=allocator\n"+
		"<<<<\nThe sequence counter is bumped in place by one on every single write.\n====\n"+
		"The allocator hands out a fresh version identifier on every single write.\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", fix); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "stale")
	noErr(t, err, "check stale")
	mustContain(t, out, "survive NOWHERE in the untouched corpus", "reached its consumers")
	mustNotContain(t, out, "NOT A PASS")
}

// A round that does not exist is not a round with no closures. `-round 999`
// printed `0 closure(s), 0 checked, 0 not closed` and exited zero: the report of
// a check that examined nothing, in the register of one that examined
// everything — the exact shape this tool exists to refuse, found by an external
// reviewer in the check that refuses it.
func TestCheckClosure_refusesARoundThatDoesNotExist(t *testing.T) {
	wsDir := newGate(t)
	out, err := runWF(t, wsDir, "check", "closure", "-round", "999")
	if err == nil {
		t.Fatalf("want a refusal, got a clean report:\n%s", out)
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("the refusal must say what is wrong: %v", err)
	}
	if strings.Contains(out, "0 closure(s) claiming a commit") {
		t.Fatalf("and it must not print a report at all:\n%s", out)
	}
}

// One verdict per closure. The first version appended a line per failed
// assertion and printed that count as "closures not closed", so a finding
// naming two artifacts could be counted twice — a headline that can exceed the
// number of claims it is drawn from.
func TestCheckClosure_countsRowsNotAssertions(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	other := filepath.Join(tmp, "context.md")
	write(t, other, "# Context\n\nA second artifact nobody fixes in this round.\n")
	if _, err := runWF(t, wsDir, "add", other); err != nil {
		t.Fatal(err)
	}
	third := filepath.Join(tmp, "notes.md")
	write(t, third, "# Notes\n\nA third artifact nobody fixes either.\n")
	if _, err := runWF(t, wsDir, "add", third); err != nil {
		t.Fatal(err)
	}
	fix := filepath.Join(tmp, "fix.txt")
	write(t, fix, "=== artifacts/decisions.md  count=1  note=a fix\n<<<<\n# Decisions\n====\n# Decisions (revised)\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", fix)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, applied)

	// ONE finding, naming TWO artifacts the commit never opened.
	ledger(t, wsDir, 1, "| 1 | BLOCKER | artifacts/context.md and artifacts/notes.md disagree | "+sha+" fixed |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "closure")
	if !errors.Is(err, errFindings) {
		t.Fatalf("want a finding, got %v\n%s", err, out)
	}
	mustContain(t, out, "1 closure(s) claiming a commit", "1 not closed by that commit",
		"did not touch artifacts/context.md", "did not touch artifacts/notes.md")
	if n := strings.Count(out, "round 1 [1] closed by"); n != 1 {
		t.Fatalf("one closure is one verdict, got %d lines:\n%s", n, out)
	}
}

// The FILE-only population is counted once per closure, and only when neither
// the section nor the line half ran. It used to be incremented per named path
// and even where a line HAD been checked, so the report called closures
// file-only that were not.
func TestCheckClosure_fileOnlyIsNotCountedWhenALineWasChecked(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	fix := filepath.Join(tmp, "fix.txt")
	write(t, fix, "=== artifacts/decisions.md  count=1  note=a fix\n<<<<\n# Decisions\n====\n# Decisions (revised)\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", fix)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, applied)
	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/decisions.md:1 is loose | "+sha+" fixed |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	mustNotContain(t, out, "only at FILE level")
}

// A closure whose commit created the artifact must not read as one that never
// touched it — the parser used to drop added files entirely.
func TestCheckClosure_aCommitThatCreatedTheArtifactTouchedIt(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	other := filepath.Join(tmp, "context.md")
	write(t, other, "# Context\n\nThe artifact this closure claims to have created.\n")
	added, err := runWF(t, wsDir, "add", other)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, added)
	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/context.md was missing entirely | "+sha+" added it |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	if strings.Contains(out, "did not touch artifacts/context.md") {
		t.Fatalf("the commit that CREATED it touched it:\n%s", out)
	}
}

// A ledger that cannot be READ is not a ledger with no closures in it — the
// distinction `wf stats` already draws, and the one this command was missing:
// deleting a round's findings.md made its closures vanish into a report that
// looked complete.
func TestCheckClosure_anUnreadableLedgerIsAbsentNotEmpty(t *testing.T) {
	wsDir := newGate(t)
	if err := os.Remove(filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	mustContain(t, out, "1 round(s) have no readable findings.md (1)", "ABSENT measurement, not a measured zero")
}
