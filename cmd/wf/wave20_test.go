// The twentieth wave: the workspace refused inside the target, the bracketed
// header, the closure report's two unstated populations, and `wf round close`.
//
// Every test here fails without the change it pins — checked by reverting each
// one and watching this file go red, which is the only way this project accepts
// a guard.
package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// `wf init` inside the target. Measured on the first gate run against a target
// other than mocker: the operator's working directory IS the target, so the
// workspace landed at <target>/<name> and the command said nothing — it
// surfaced one command later as `?? <name>/` in the TARGET's own git status.
func TestInitRefusesAWorkspaceInsideTheTarget(t *testing.T) {
	setGitEnv(t)
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	write(t, filepath.Join(target, "a.md"), "x\n")
	gitIn(t, target, "init", "-q", "-b", "main")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-q", "-m", "initial")

	out, err := runWF(t, target, "init", "gate", "-target", target)
	if err == nil {
		t.Fatalf("init must refuse a workspace inside the target, got:\n%s", out)
	}
	mustContain(t, err.Error(), "resolves INSIDE the target", "`-dir ")
	// Refused BEFORE the directory is made: a refusal that leaves the tree
	// dirty has committed the thing it refused.
	if _, statErr := os.Stat(filepath.Join(target, "gate")); statErr == nil {
		t.Fatal("the refused init created the workspace directory anyway")
	}
}

// The other half of the same shape, and the one an operator reaches by typing
// `-dir .` — the workspace root IS the target.
func TestInitRefusesAWorkspaceThatIsTheTarget(t *testing.T) {
	setGitEnv(t)
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	write(t, filepath.Join(target, "a.md"), "x\n")
	gitIn(t, target, "init", "-q", "-b", "main")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-q", "-m", "initial")

	out, err := runWF(t, tmp, "init", "gate", "-target", target, "-dir", target)
	if err == nil {
		t.Fatalf("init must refuse a workspace that IS the target, got:\n%s", out)
	}
	mustContain(t, err.Error(), "IS the target")
}

// A workspace beside the target is the prescribed shape and must stay free.
// Without this the refusal could be a blanket one and every test above would
// still pass.
func TestInitAcceptsAWorkspaceBesideTheTarget(t *testing.T) {
	setGitEnv(t)
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	write(t, filepath.Join(target, "a.md"), "x\n")
	gitIn(t, target, "init", "-q", "-b", "main")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-q", "-m", "initial")

	out, err := runWF(t, tmp, "init", "gate", "-target", target, "-dir", filepath.Join(tmp, "gate"))
	noErr(t, err, "init beside the target")
	mustContain(t, out, "workspace gate")
}

// A directory under the target whose NAME begins with two dots. `filepath.Rel`
// returns `..bak`, and a textual `strings.HasPrefix(rel, "..")` reads the one
// shape this refusal exists for as outside the target.
func TestInitRefusesADotDotPrefixedDirectoryInsideTheTarget(t *testing.T) {
	setGitEnv(t)
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	write(t, filepath.Join(target, "a.md"), "x\n")
	gitIn(t, target, "init", "-q", "-b", "main")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-q", "-m", "initial")

	out, err := runWF(t, tmp, "init", "gate", "-target", target, "-dir", filepath.Join(target, "..bak"))
	if err == nil {
		t.Fatalf("init must refuse <target>/..bak, got:\n%s", out)
	}
	mustContain(t, err.Error(), "resolves INSIDE the target")
}

// The bracketed header. The manual writes `=== <file> [count=N] [note=...]`,
// where the brackets mark the field optional inside a format whose other
// delimiters are literal — one round-trip on a live gate.
func TestApplyRefusesBracketedHeaderFields(t *testing.T) {
	for _, tc := range []struct {
		name, header, want string
	}{
		{"count", "=== artifacts/decisions.md [count=1]", "header field count=N is written inside square brackets"},
		// This half used to be accepted SILENTLY, with the closing bracket
		// inside the note — a wrong value written to the commit, where the
		// count half at least announced itself.
		{"note", "=== artifacts/decisions.md [note=why]", "header field note=... is written inside square brackets"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wsDir := newGate(t)
			batch := filepath.Join(filepath.Dir(wsDir), "b.txt")
			write(t, batch, tc.header+"\n<<<<\n# Decisions\n====\n# Decisions (revised)\n>>>>\n")
			out, err := runWF(t, wsDir, "apply", "-f", batch)
			if err == nil {
				t.Fatalf("apply must refuse a bracketed header field, got:\n%s", out)
			}
			mustContain(t, err.Error(), tc.want, "Drop them")
		})
	}
}

// A NOTE is free text to the end of the line, so a note ABOUT a batch may quote
// the bracketed form. Without this the refusal would reject the operator for
// describing the mistake they just fixed.
func TestApplyAcceptsABracketedFormQuotedInsideANote(t *testing.T) {
	wsDir := newGate(t)
	batch := filepath.Join(filepath.Dir(wsDir), "b.txt")
	write(t, batch, "=== artifacts/decisions.md count=1 note=the first attempt wrote [count=1]\n"+
		"<<<<\n# Decisions\n====\n# Decisions (revised)\n>>>>\n")
	out, err := runWF(t, wsDir, "apply", "-f", batch)
	noErr(t, err, "apply with a note quoting the bracketed form")
	mustContain(t, out, "committed")
}

// `wf check closure` and `wf round record` reported two populations in one
// operator's hands and only one of them said what it was counting: `open 1`
// beside "1 closure claiming a commit, 1 checked, 0 not closed".
func TestCheckClosureNamesTheRowsItDidNotExamine(t *testing.T) {
	wsDir, sha := twoArtifactGate(t)
	ledger(t, wsDir, 1,
		"| 1 | MAJOR | artifacts/decisions.md needs a revision | "+sha+" |\n"+
			"| 2 | MAJOR | artifacts/decisions.md has a second problem |  |\n"+
			"| 3 | MINOR | artifacts/decisions.md wants a word | fixed in an earlier round |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	// The counts, and the vocabulary `wf round record` uses for the same two
	// rows: a report that called them one number would disagree with the
	// command the operator ran one line earlier.
	mustContain(t, out, "2 finding(s) name no commit", "1 with an empty closure cell, 1 closed by prose")
}

// A ledger where every row names a commit says nothing about a population it
// does not have. `dup` painted 161 groups of 176 once; a note on every clean
// report is the same lesson.
func TestCheckClosureIsQuietWhenEveryRowNamesACommit(t *testing.T) {
	wsDir, sha := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/decisions.md needs a revision | "+sha+" |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	mustNotContain(t, out, "name no commit")
}

// A closure resting on `wf apply -whole`. The tool watched nothing being made,
// `wf stats` already draws the distinction per ROUND, and the closure an
// operator has to trust is per ROW — measured on P3f, where exactly one closure
// was banked in the ledger and never written into the code.
func TestCheckClosureNamesTheClosuresRestingOnAnAdoption(t *testing.T) {
	wsDir, batchSha := twoArtifactGate(t)
	art := filepath.Join(wsDir, "artifacts", "decisions.md")
	body, err := os.ReadFile(art)
	noErr(t, err, "read artifact")
	write(t, art, string(body)+"\nA paragraph the operator wrote by hand.\n")
	applied, err := runWF(t, wsDir, "apply", "-whole", "artifacts/decisions.md", "-m", "restructure")
	noErr(t, err, "apply -whole")
	wholeSha := commitShaOf(t, applied)

	ledger(t, wsDir, 1,
		"| 1 | MAJOR | artifacts/decisions.md needs a revision | "+batchSha+" |\n"+
			"| 2 | MAJOR | artifacts/decisions.md needs a paragraph | "+wholeSha+" |\n")
	_, err = runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	mustContain(t, out, "1 closure(s) rest on an ADOPTION", "r1#2")
	// The verified batch must NOT be in the group: a report that marked both
	// would say nothing, which is the state before this change.
	if strings.Contains(out, "r1#1,") || strings.Contains(out, "r1#1 ") {
		t.Fatalf("the verified batch was named as an adoption:\n%s", out)
	}
}

// A gate with no adoption at all prints no adoption line. The trailer dates
// from one wave ago, so a printed `0 rest on an adoption` over a workspace that
// predates it would be an absent measurement in the register of a measured one.
func TestCheckClosureIsQuietWhenNoClosureRestsOnAnAdoption(t *testing.T) {
	wsDir, sha := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/decisions.md needs a revision | "+sha+" |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	out, err := runWF(t, wsDir, "check", "closure")
	noErr(t, err, "check closure")
	mustNotContain(t, out, "ADOPTION")
}

// `wf round close`, and the reason it exists: the operator's own `awk -F'|'`
// corrupted the ledger twice on one gate, because the table escapes a literal
// pipe as `\|` and every field-splitting tool splits on the escape too.
func TestRoundCloseFillsTheCellWithoutSplittingTheRow(t *testing.T) {
	wsDir, sha := twoArtifactGate(t)
	row := `| 1 | MAJOR | artifacts/decisions.md documents ` + "`rg x \\| head`" + ` wrongly |  |`
	ledger(t, wsDir, 1, row+"\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	out, err := runWF(t, wsDir, "round", "close", "1", sha)
	noErr(t, err, "round close")
	mustContain(t, out, "closed by "+sha, "committed")

	b, err := os.ReadFile(filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "read ledger")
	// The escaped pipe survives verbatim and the sha is in the LAST cell —
	// the two halves an `awk -F'|'` gets wrong.
	mustContain(t, string(b), "`rg x \\| head` wrongly | "+sha+" |")
	// And the round is committed, not left dirty for a hand `git commit`.
	status, err := runWF(t, wsDir, "status")
	noErr(t, err, "status")
	mustContain(t, status, "tree clean")
}

// `#3` is what a report prints and what an operator copies.
func TestRoundCloseAcceptsAHashPrefixedFindingNumber(t *testing.T) {
	wsDir, sha := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 3 | MAJOR | artifacts/decisions.md needs a revision |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	out, err := runWF(t, wsDir, "round", "close", "#3", sha)
	noErr(t, err, "round close #3")
	mustContain(t, out, "finding 3: closed by")
}

// A sha that is not a commit here is refused BEFORE the ledger is touched: a
// landed lie briefs the next round with fiction, a rejected one announces
// itself. Same order `wf apply` and `wf round record` use.
func TestRoundCloseRefusesAShaThatIsNotACommitAndWritesNothing(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/decisions.md needs a revision |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")
	before, err := os.ReadFile(filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "read ledger")

	out, err := runWF(t, wsDir, "round", "close", "1", "deadbeef1")
	if err == nil {
		t.Fatalf("round close must refuse a sha that is not a commit here, got:\n%s", out)
	}
	mustContain(t, err.Error(), "not a commit in this GATE WORKSPACE", "nothing was written")
	after, err := os.ReadFile(filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "re-read ledger")
	if string(before) != string(after) {
		t.Fatal("the refused close rewrote the ledger anyway")
	}
}

// An already-closed row is not silently overwritten: a re-closure is a real
// event and a mistyped finding number looks exactly like one.
func TestRoundCloseRefusesAnAlreadyClosedRowWithoutReplace(t *testing.T) {
	wsDir, sha := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/decisions.md needs a revision | "+sha+" |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	out, err := runWF(t, wsDir, "round", "close", "1", sha)
	if err == nil {
		t.Fatalf("round close must refuse an already-closed row, got:\n%s", out)
	}
	mustContain(t, err.Error(), "already closed by", "-replace")

	// -replace naming the SAME commit writes identical bytes, and the commit
	// then fails on git's `nothing staged` — a git-shaped error for an
	// operator-shaped mistake.
	out, err = runWF(t, wsDir, "round", "close", "1", sha, "-replace")
	if err == nil {
		t.Fatalf("round close -replace with the same sha must refuse, got:\n%s", out)
	}
	mustContain(t, err.Error(), "names the same commit; nothing to do")

	// A DIFFERENT commit is the real case -replace exists for.
	batch := filepath.Join(filepath.Dir(wsDir), "second.txt")
	write(t, batch, "=== artifacts/decisions.md count=1\n<<<<\n# Decisions (revised)\n====\n# Decisions (revised twice)\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", batch)
	noErr(t, err, "second apply")
	second := commitShaOf(t, applied)

	out, err = runWF(t, wsDir, "round", "close", "1", second, "-replace")
	noErr(t, err, "round close -replace")
	mustContain(t, out, "closed by "+second)
}

// A finding number that is not in this round's ledger. The usual cause is the
// wrong round, so the refusal prints the numbers that ARE there.
func TestRoundCloseRefusesAnUnknownFindingAndNamesTheOnesItHas(t *testing.T) {
	wsDir, sha := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/decisions.md needs a revision |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	out, err := runWF(t, wsDir, "round", "close", "7", sha)
	if err == nil {
		t.Fatalf("round close must refuse a finding that is not there, got:\n%s", out)
	}
	mustContain(t, err.Error(), "has no finding 7", "it has: 1")
}

// The scaffold's empty row is a form, not a claim. Closing it would record a
// commit against a finding nobody wrote down.
func TestRoundCloseRefusesTheScaffoldsEmptyRow(t *testing.T) {
	wsDir := newGate(t)
	batch := filepath.Join(filepath.Dir(wsDir), "b.txt")
	write(t, batch, "=== artifacts/decisions.md count=1\n<<<<\n# Decisions\n====\n# Decisions (revised)\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", batch)
	noErr(t, err, "apply")

	out, err := runWF(t, wsDir, "round", "close", "1", commitShaOf(t, applied))
	if err == nil {
		t.Fatalf("round close must refuse the scaffold's empty row, got:\n%s", out)
	}
	mustContain(t, err.Error(), "it is the scaffold's empty row")
}

// The round is INFERRED when `-round` is absent, and an inference nothing
// announces is how a note meant for round 10 landed in round 11.
func TestRoundCloseAnnouncesTheRoundItInferred(t *testing.T) {
	wsDir, sha := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/decisions.md needs a revision |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	out, err := runWF(t, wsDir, "round", "close", "1", sha)
	noErr(t, err, "round close")
	mustContain(t, out, "round 1 (the open one")

	// And says nothing about a round the operator typed: restating a number
	// the operator just wrote is a line that paints every run.
	ledger(t, wsDir, 1, "| 2 | MAJOR | artifacts/decisions.md needs another revision |  |\n")
	_, err = runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"), "-round", "1")
	noErr(t, err, "round record -round 1")
	out, err = runWF(t, wsDir, "round", "close", "2", sha, "-round", "1")
	noErr(t, err, "round close -round 1")
	mustNotContain(t, out, "the open one")
}

// The `[fix-induced]` marker is opt-in and the only way that share is ever
// measured. One gate forgot it entirely and `wf stats` said so — after the
// gate. The scaffold is where the operator is when the row is written.
func TestRoundScaffoldAsksForTheFixInducedMarker(t *testing.T) {
	wsDir := newGate(t)
	b, err := os.ReadFile(filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "read scaffold")
	mustContain(t, string(b), "[fix-induced]")
	// And the count of rules the paragraph claims to carry stays true to the
	// list under it — the class this repository banks as its own most-repeated
	// defect, one line long.
	if strings.Contains(string(b), "Two rules about this table") {
		t.Fatal("the scaffold still says TWO rules while carrying three")
	}
}

// The round loop's own pointer: `wf round close` names the check that reads the
// cell it just filled. A shipped capability the report never mentions is a
// capability nobody has.
func TestRoundCloseNamesTheCheckThatReadsTheCell(t *testing.T) {
	wsDir, sha := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/decisions.md needs a revision |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	out, err := runWF(t, wsDir, "round", "close", "1", sha)
	noErr(t, err, "round close")
	mustContain(t, out, "wf check closure -round "+strconv.Itoa(1))
}
