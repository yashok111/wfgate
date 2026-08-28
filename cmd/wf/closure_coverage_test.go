// The coverage of `wf check closure`, in the HEADER. Measured over the six gate
// workspaces on disk: p2f checked 3 of 110 closures, mocker A3 5 of 266 and P3a
// 68 of 421, each printing a headline "N checked" with one miss under it, while
// P3b (568 of 587) and P3c (132 of 153) are genuinely covered. Two closures
// were recorded on a live gate that no commit touched, and this check passed
// them because their finding text named no path — while the tool printed, on
// that round and every other, a bookkeeping-voiced note saying it was about to
// go unrun.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// twoArtifactGate leaves a workspace with a second artifact, so the
// path-naming requirement applies at all: on a one-artifact workspace the check
// reads the only document there is.
func twoArtifactGate(t *testing.T) (wsDir, sha string) {
	t.Helper()
	wsDir = newGate(t)
	tmp := filepath.Dir(wsDir)
	other := filepath.Join(tmp, "context.md")
	write(t, other, "# Context\n\nA second artifact, so a finding must say which one it means.\n")
	if _, err := runWF(t, wsDir, "add", other); err != nil {
		t.Fatal(err)
	}
	batch := filepath.Join(tmp, "fix.txt")
	write(t, batch, "=== artifacts/decisions.md  count=1  note=a fix\n"+
		"<<<<\n# Decisions\n====\n# Decisions (revised)\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", batch)
	if err != nil {
		t.Fatal(err)
	}
	return wsDir, commitShaOf(t, applied)
}

func TestCheckClosure_headerSaysNotAPassWhenMoreWereUnreachableThanChecked(t *testing.T) {
	wsDir, sha := twoArtifactGate(t)
	ledger(t, wsDir, 1,
		"| 1 | MAJOR | artifacts/decisions.md needs a revision | "+sha+" fixed |\n"+
			"| 2 | MAJOR | a finding naming no artifact at all | "+sha+" fixed |\n"+
			"| 3 | MAJOR | another finding naming nothing | "+sha+" fixed |\n")
	rec, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")
	// The record-time half, in proportion: this is the moment the requirement
	// can still be met, which is why the note exists there at all.
	mustContain(t, rec, "closures, NOT A PASS: 2 of 3 name no artifact path")

	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	mustContain(t, out, "NOT A PASS: 2 could not be reached, more than were checked")
}

// A gate whose closures are mostly checkable says nothing extra: the marker is
// a claim about THIS report's coverage, and painting every report with it is
// `dup`'s 161-groups-of-176 lesson repeated.
func TestCheckClosure_headerIsQuietWhenMostClosuresAreChecked(t *testing.T) {
	wsDir, sha := twoArtifactGate(t)
	ledger(t, wsDir, 1,
		"| 1 | MAJOR | artifacts/decisions.md needs a revision | "+sha+" fixed |\n"+
			"| 2 | MAJOR | artifacts/decisions.md also wants this | "+sha+" fixed |\n"+
			"| 3 | MAJOR | a finding naming no artifact at all | "+sha+" fixed |\n")
	rec, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")
	mustContain(t, rec, "closures: 1 of 3 name no artifact path")
	mustNotContain(t, rec, "NOT A PASS")

	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	mustNotContain(t, out, "could not be reached, more than were checked")
}

// The scaffold states the two rules about its own table. Both cost a live gate
// something: a finding without an artifact path is a closure `wf check closure`
// can never reach, and a finding quoting a shell pipeline is refused by the row
// splitter at the moment the record is being written.
func TestRoundScaffold_statesTheTableGrammar(t *testing.T) {
	wsDir := newGate(t)
	b, err := os.ReadFile(filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"NAMES the artifact path",
		"wf check closure reaches a closure only",
		"split on unescaped pipes",
	} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("the scaffold must state %q:\n%s", want, b)
		}
	}
}

// A cut's deletions are reviewed by nothing, and the delta brief is where a
// reviewer is aimed. A COUNT and no list: measured over mocker P3c the removed
// structural leads run 49, 23, 13, 9, 2, 0, 0, 0 per round and over P3b they
// reach 215 in the round that cut a slice, most of them rewrites — a list would
// paint most of its output.
func TestDeltaBrief_namesTheDeletionPopulation(t *testing.T) {
	// A round that only ADDS: nothing structural stops existing.
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	add := filepath.Join(filepath.Dir(wsDir), "add.txt")
	write(t, add, "=== artifacts/decisions.md  count=1  note=append\n"+
		"<<<<\n# Decisions\n====\n# Decisions\n\n- a new clause nobody had before\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", add); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "second"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "delta")
	noErr(t, err, "delta")
	mustNotContain(t, out, "## deletions")

	// And a round that CUTS one: the clause is in the frozen state and gone
	// from HEAD, which is the shape a scope cut has.
	reviewedRound(t, wsDir, 2)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	cut := filepath.Join(filepath.Dir(wsDir), "cut.txt")
	write(t, cut, "=== artifacts/decisions.md  count=1  note=cut\n"+
		"<<<<\n\n- a new clause nobody had before\n====\n\n>>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", cut); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "third"); err != nil {
		t.Fatal(err)
	}
	out, err = runWF(t, wsDir, "delta")
	noErr(t, err, "delta")
	mustContain(t, out,
		"## deletions",
		"removes 1 structural lead(s)",
		"NO check in this tool watches the second kind",
		"wf check stale -since gate-r2")
}

// A closure naming a commit git cannot read was not checked either. Leaving
// `unreadable` out of the unreachable sum let the exact headline this marker
// exists to catch print as a pass: three unreadable shas of four closures gave
// "1 checked" with no marker and the population two lines below it.
func TestCheckClosure_anUnreadableShaIsUnreachableToo(t *testing.T) {
	wsDir, sha := twoArtifactGate(t)
	ledger(t, wsDir, 1,
		"| 1 | MAJOR | artifacts/decisions.md needs a revision | "+sha+" fixed |\n"+
			"| 2 | MAJOR | artifacts/decisions.md again | dead1beef2 |\n"+
			"| 3 | MAJOR | artifacts/decisions.md once more | dead3beef4 |\n"+
			"| 4 | MAJOR | artifacts/decisions.md and again | dead5beef6 |\n")
	// The ledger is written directly: `wf round record` refuses a sha the
	// workspace does not have, which is a different guard and the right one.
	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	mustContain(t, out, "could not be reached, more than were checked", "git could not read")
}

// Two NOT A PASS lines about one report is the second-copy defect. Where NOTHING
// was checked the older note already says it, and the header stays quiet.
func TestCheckClosure_saysNotAPassOnce(t *testing.T) {
	wsDir, sha := twoArtifactGate(t)
	ledger(t, wsDir, 1,
		"| 1 | MAJOR | a finding naming no artifact at all | "+sha+" fixed |\n"+
			"| 2 | MAJOR | another naming nothing | "+sha+" fixed |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	mustContain(t, out, "NOT A PASS — no closure could be checked")
	mustNotContain(t, out, "could not be reached, more than were checked")
}

// An AMBIGUOUS closure is unreachable as surely as an unnamed one, and BOTH
// reports must say so: the record-time note counting it on the reachable side
// left `wf round record` quiet about a round `wf check closure` then called NOT
// A PASS — two commands contradicting each other, with the quieter one at the
// moment the operator could still fix it.
func TestClosureCoverage_anAmbiguousPathIsUnreachableInBothReports(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	// Two artifacts whose BASENAME is the same: a finding naming `notes.md`
	// resolves to neither.
	for _, d := range []string{"a", "b"} {
		p := filepath.Join(tmp, d, "notes.md")
		write(t, p, "# Notes "+d+"\n\nA second artifact.\n")
		if _, err := runWF(t, wsDir, "add", p, "-as", "artifacts/"+d+"/notes.md"); err != nil {
			t.Fatal(err)
		}
	}
	batch := filepath.Join(tmp, "fix.txt")
	write(t, batch, "=== artifacts/decisions.md  count=1  note=a fix\n"+
		"<<<<\n# Decisions\n====\n# Decisions (revised)\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", batch)
	noErr(t, err, "apply")
	sha := commitShaOf(t, applied)
	ledger(t, wsDir, 1,
		"| 1 | MAJOR | artifacts/decisions.md needs a revision | "+sha+" fixed |\n"+
			"| 2 | MAJOR | notes.md is wrong somewhere | "+sha+" fixed |\n"+
			"| 3 | MAJOR | notes.md is wrong elsewhere too | "+sha+" fixed |\n")
	rec, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")
	mustContain(t, rec, "NOT A PASS", "matching more than one artifact")

	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	mustContain(t, out, "NOT A PASS: 2 could not be reached, more than were checked")
}
