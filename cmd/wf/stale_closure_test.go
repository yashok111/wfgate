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

// The report NAMES the population it narrowed away. `check stale` drops a
// removed token when the fix put the same word back in another form, and the
// argument for counting it is the argument `dup -since` settled: a candidate
// population dropped in silence is one nobody can argue with.
func TestCheckStale_namesTheInflectionsItDropped(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	grow := filepath.Join(tmp, "grow.txt")
	write(t, grow, "=== artifacts/decisions.md  count=1  note=a rule and its consumer\n"+
		"<<<<\nEntry point: `src/main.go:3#NeedleFn`.\n====\n"+
		"The literal matcher work under a parameterised base path.\n"+
		"\nEvery downstream matcher must handle the parameterised base path.\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", grow); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	fix := filepath.Join(tmp, "fix.txt")
	write(t, fix, "=== artifacts/decisions.md  count=1  note=one word, inflected\n"+
		"<<<<\nThe literal matcher work under a parameterised base path.\n====\n"+
		"The literal matcher works under a parameterised base path.\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", fix); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "stale", "-since", "gate-r1")
	noErr(t, err, "check stale")
	mustContain(t, out, "removed token(s) are the same word this range re-wrote in another form")
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
	// The suppression is stated BEFORE the counts now — mocker A3 read nine
	// rounds of descent as convergence with this said in a parenthesis after
	// them — and where the ratio would have stood only its absence is printed.
	mustContain(t, out, "FIRST REVIEW of an artifact ADDED in this window", "(no ratio)")
	if strings.Contains(out, "finding(s) per 100 changed line(s))") {
		t.Fatalf("no per-round ratio may be printed for that window:\n%s", out)
	}
	// And nothing predicts from it either. This used to assert that the basis
	// line was ABSENT, which defended the claim through the only implementation
	// there was; the claim is that no RATIO is derived, and a line that vanishes
	// cannot say whether a gate has no basis or had its every priceable round
	// excluded. So the absence is named, and it still carries no number.
	mustContain(t, out, "prediction basis — NOT A PASS, no round is priceable")
	if strings.Contains(out, "per 100; `wf apply` predicts from THIS") {
		t.Fatalf("the only round is an authoring window, so nothing may be predicted from it:\n%s", out)
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
	// BOTH readings, because this check cannot tell them apart: the same
	// observation covers a fix that reached every consumer and a decision that
	// stopped existing with nothing in its place. Measured on mocker P3c, where
	// a clause split in two was cut and the survivor kept the requirement the
	// deleted half carried — it cost a second launch.
	mustContain(t, out, "stopped existing and nothing replaced it", "A round that CUT reads this line the second way")
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

// A finding cites a RANGE and the check has to ask about the range. Measured on
// a live gate: all FOUR of its line-level closure misses were one citation
// spelling — `p3h.mjs:454-459` reported not closed by a commit whose own hunks
// covered 450-453 and 455-457, because 454 is the one unchanged line inside the
// span the finding drew, and `--unified=0` splits a region there. Each cost a
// hand verification against `git show --unified=0` and every one of them was
// closed. The shape below is that shape: the fix lands inside the cited span but
// not on its first line.
func TestCheckClosure_readsTheCitedSpanNotOnlyItsFirstLine(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	grow := filepath.Join(tmp, "grow.txt")
	write(t, grow, "=== artifacts/decisions.md  count=1  note=a document with room in it\n"+
		"<<<<\nEntry point: `src/main.go:3#NeedleFn`.\n====\n"+
		"L3\nL4\nL5\nL6\nL7\nL8\nL9\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", grow); err != nil {
		t.Fatal(err)
	}

	// Two edits, one commit, with line 6 untouched BETWEEN them: at
	// --unified=0 that is two hunks and a gap, which is what defeated the
	// first-line reading.
	fix := filepath.Join(tmp, "fix.txt")
	write(t, fix, "=== artifacts/decisions.md  count=1  note=the first half\n"+
		"<<<<\nL5\n====\nL5 (fixed)\n>>>>\n"+
		"=== artifacts/decisions.md  count=1  note=the second half\n"+
		"<<<<\nL7\n====\nL7 (fixed)\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", fix)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, applied)

	// The span 6-8 contains line 7, which the commit changed; line 6, which it
	// did not, is where the citation starts.
	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/decisions.md:6-8 states it wrongly | "+sha+" fixed |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	// The positive half FIRST: a row that vanished because the closure went
	// UNCHECKED — an unnamed path, an unreadable sha, an ambiguous spelling —
	// prints no "but not line" either, and this test would read that as a pass.
	mustContain(t, out, "1 checked against the artifact they name")
	if strings.Contains(out, "but not line") || strings.Contains(out, "but not lines") {
		t.Fatalf("an edit inside the cited span closes it:\n%s", out)
	}
}

// And the miss it still reports names the SPAN the finding cited, not the line
// the check reduced it to: a row reading `not line 454` over a citation of
// `454-459` sends its reader to the wrong evidence.
func TestCheckClosure_aSpanMissNamesTheSpan(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	grow := filepath.Join(tmp, "grow.txt")
	write(t, grow, "=== artifacts/decisions.md  count=1  note=a document with room in it\n"+
		"<<<<\nEntry point: `src/main.go:3#NeedleFn`.\n====\n"+
		"L3\nL4\nL5\nL6\nL7\nL8\nL9\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", grow); err != nil {
		t.Fatal(err)
	}
	fix := filepath.Join(tmp, "fix.txt")
	write(t, fix, "=== artifacts/decisions.md  count=1  note=somewhere else entirely\n"+
		"<<<<\nL9\n====\nL9 (fixed)\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", fix)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, applied)

	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/decisions.md:4-6 states it wrongly | "+sha+" fixed |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "closure")
	if !errors.Is(err, errFindings) {
		t.Fatalf("want a finding, got %v\n%s", err, out)
	}
	mustContain(t, out, "but not lines 4-6")
}

// The coordinate system a finding's line numbers live in is the round's FREEZE;
// each closing commit's own old-side ranges live in ITS PARENT. Measured over the
// eleven gate workspaces, 643 of the 661 closures citing a line name a commit
// whose parent is NOT the freeze — a round applies several batches with ledger
// and lens commits between them — so a second batch that fixes exactly what the
// finding cites was reported as having missed it, at the offset the first batch
// introduced.
func TestCheckClosure_mapsTheCitedSpanIntoTheCommitsOwnCoordinates(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	grow := filepath.Join(tmp, "grow.txt")
	write(t, grow, "=== artifacts/decisions.md  count=1  note=a document with room in it\n"+
		"<<<<\nEntry point: `src/main.go:3#NeedleFn`.\n====\n"+
		"L3\nL4\nL5\nTHE CITED CLAIM\nL7\nL8\nL9\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", grow); err != nil {
		t.Fatal(err)
	}
	// The freeze is the coordinate system the reviewer read: the claim is line 6.
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}

	// Batch ONE, far above the citation, inserting four lines.
	first := filepath.Join(tmp, "first.txt")
	write(t, first, "=== artifacts/decisions.md  count=1  note=an unrelated fix above\n"+
		"<<<<\nL3\n====\nL3\nL3a\nL3b\nL3c\nL3d\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", first); err != nil {
		t.Fatal(err)
	}
	// Batch TWO fixes the cited claim, which now stands four lines lower.
	second := filepath.Join(tmp, "second.txt")
	write(t, second, "=== artifacts/decisions.md  count=1  note=the fix the finding asked for\n"+
		"<<<<\nTHE CITED CLAIM\n====\nTHE CITED CLAIM, corrected\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", second)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, applied)

	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/decisions.md:6 states it wrongly | "+sha+" fixed |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	mustContain(t, out, "1 checked against the artifact they name")
	if strings.Contains(out, "but not line") {
		t.Fatalf("the second batch fixed the cited line at its new number:\n%s", out)
	}
}

// And where the round's own EARLIER batch rewrote the cited lines themselves,
// there is no offset to apply: the numbers mean nothing in the named commit's
// coordinates, so the closure is counted and NOT judged either way rather than
// compared against a number the check invented.
func TestCheckClosure_saysWhenTheCitedLinesMovedBeforeTheNamedCommit(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	grow := filepath.Join(tmp, "grow.txt")
	write(t, grow, "=== artifacts/decisions.md  count=1  note=a document with room in it\n"+
		"<<<<\nEntry point: `src/main.go:3#NeedleFn`.\n====\n"+
		"L3\nL4\nTHE CITED CLAIM\nL6\nL7\nL8\nL9\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", grow); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(tmp, "first.txt")
	write(t, first, "=== artifacts/decisions.md  count=1  note=the earlier batch rewrites the cited line\n"+
		"<<<<\nTHE CITED CLAIM\n====\nTHE CITED CLAIM, rewritten by the earlier batch\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", first); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(tmp, "second.txt")
	write(t, second, "=== artifacts/decisions.md  count=1  note=the named commit, elsewhere\n"+
		"<<<<\nL9\n====\nL9 (fixed)\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", second)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, applied)

	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/decisions.md:5 states it wrongly | "+sha+" fixed |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	mustContain(t, out, "already been rewritten between the round's freeze and the commit they name")
	if strings.Contains(out, "but not line") {
		t.Fatalf("an unmappable citation is counted, not judged:\n%s", out)
	}
}

// The map runs only where the freeze actually PRECEDES the commit. `wf freeze
// -refreeze` moves a tag, and a round republished after its own fix landed makes
// `tag..sha^` a REVERSE diff: the shift then subtracts what the fix added and
// lands the span in a region nobody cited. Measured: five closures of one gate
// were reported missed for exactly that reason before this precondition was
// checked.
func TestCheckClosure_doesNotShiftWhenTheFreezeCameAfterTheFix(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	grow := filepath.Join(tmp, "grow.txt")
	write(t, grow, "=== artifacts/decisions.md  count=1  note=a document with room in it\n"+
		"<<<<\nEntry point: `src/main.go:3#NeedleFn`.\n====\n"+
		"L3\nL4\nL5\nTHE CITED CLAIM\nL7\nL8\nL9\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", grow); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	// One commit that BOTH inserts above the citation and fixes it.
	fix := filepath.Join(tmp, "fix.txt")
	write(t, fix, "=== artifacts/decisions.md  count=1  note=insert above\n"+
		"<<<<\nL3\n====\nL3\nL3a\nL3b\nL3c\nL3d\n>>>>\n"+
		"=== artifacts/decisions.md  count=1  note=and the cited claim itself\n"+
		"<<<<\nTHE CITED CLAIM\n====\nTHE CITED CLAIM, corrected\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", fix)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, applied)
	// The round is republished AFTER the fix, so the tag no longer names the
	// bytes the citation was written against.
	if _, err := runWF(t, wsDir, "freeze", "-refreeze"); err != nil {
		t.Fatal(err)
	}

	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/decisions.md:6 states it wrongly | "+sha+" fixed |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	mustContain(t, out, "1 checked against the artifact they name")
	if strings.Contains(out, "but not line") {
		t.Fatalf("a reverse diff must not move the citation:\n%s", out)
	}
	// Nor may it read as unmappable: a reverse diff reports the citation's own
	// lines as changed, which would file this closure under a population it does
	// not belong to and take it out of the judged count in silence.
	if strings.Contains(out, "already been rewritten") {
		t.Fatalf("the citation is mappable; the freeze simply moved:\n%s", out)
	}
}

// An INVERTED span is the start line and nothing else, all the way through the
// check and not only in the parser: widening it to cover both readings would
// answer a question about a region the author did not write, and `check cite`
// already reports that shape as an error of its own.
func TestCheckClosure_anInvertedSpanIsItsStartLineAndNoMore(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	grow := filepath.Join(tmp, "grow.txt")
	write(t, grow, "=== artifacts/decisions.md  count=1  note=a document with room in it\n"+
		"<<<<\nEntry point: `src/main.go:3#NeedleFn`.\n====\n"+
		"L3\nL4\nL5\nL6\nL7\nL8\nL9\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", grow); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	// The commit changes line 4, which is inside the span read BACKWARDS and
	// outside the one the check may act on.
	fix := filepath.Join(tmp, "fix.txt")
	write(t, fix, "=== artifacts/decisions.md  count=1  note=line 4\n"+
		"<<<<\nL4\n====\nL4 (fixed)\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", fix)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, applied)

	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/decisions.md:8-4 states it wrongly | "+sha+" fixed |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "closure")
	if !errors.Is(err, errFindings) {
		t.Fatalf("want a finding, got %v\n%s", err, out)
	}
	mustContain(t, out, "but not line 8")
}
