// The twenty-second wave: the prose door on `wf round close`, the run round
// leaving the cost meter, `wf apply` reading a positional file, and the brief
// that exists nowhere without `-save`.
//
// Every test here fails without the change it pins — checked by reverting each
// one and watching this file go red, which is the only way this project accepts
// a guard.
package main

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yashok111/wfgate/internal/ws"
)

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// A NOTE row asserts no failure and has nothing a commit could close, while
// `wf round record` has always reported `closed by prose` as a first-class
// category — so the harness knew about a closure this command could not write.
// Measured on P4a: three round-trips in rounds 14, 15 and 16, each ending in
// the hand ledger edit plus a re-record this command exists to remove.
func TestRoundCloseFillsTheCellWithProse(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | NOTE | artifacts/decisions.md leaves the ordering open |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	out, err := runWF(t, wsDir, "round", "close", "1", "-prose", "an observation; nothing to fix")
	noErr(t, err, "round close -prose")
	mustContain(t, out, "closed by an observation; nothing to fix", "committed",
		"checked by no command")

	b, err := os.ReadFile(filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "read ledger")
	mustContain(t, string(b), "| an observation; nothing to fix |")
	// Committed, not left dirty for a hand `git commit` — the whole point.
	status, err := runWF(t, wsDir, "status")
	noErr(t, err, "status")
	mustContain(t, status, "tree clean")
	// And the row reads as a PROSE closure downstream, which is the category
	// the operator reached for and the reason this door exists.
	cl, err := runWF(t, wsDir, "check", "closure", "-round", "1")
	noErr(t, err, "check closure")
	mustContain(t, cl, "prose")
}

// A sha-shaped token inside the prose makes the cell a COMMIT claim to every
// reader downstream — `ws.FindingRow.Shas()` is what they all use. `wf freeze`
// would then demand that commit exist and `wf check closure` would test what it
// touched, both against a row whose author meant a sentence.
func TestRoundCloseRefusesProseThatReadsAsACommitClaim(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | NOTE | artifacts/decisions.md is fine |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	out, err := runWF(t, wsDir, "round", "close", "1", "-prose", "superseded by deadbee1 upstream")
	if err == nil {
		t.Fatalf("prose carrying a sha-shaped token must be refused, got:\n%s", out)
	}
	mustContain(t, err.Error(), "deadbee1", "COMMIT claim", "nothing was written")
	// Refused BEFORE the write, like every other refusal in this command.
	b, err := os.ReadFile(filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "read ledger")
	if strings.Contains(string(b), "superseded") {
		t.Fatal("the refused prose was written anyway")
	}
}

// A pipe is ESCAPED rather than refused: the operator typed a sentence, and a
// shell pipeline quoted inside it is the ordinary reason a cell holds one. An
// already-escaped pipe is left alone — the fix for following the ledger's own
// documentation must not be to stop following it.
func TestRoundCloseEscapesAPipeInProseAndNotAnEscapedOne(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | NOTE | artifacts/decisions.md a |  |\n| 2 | NOTE | artifacts/decisions.md b |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	_, err = runWF(t, wsDir, "round", "close", "1", "-prose", "answered by `rg x | head`")
	noErr(t, err, "round close -prose with a pipe")
	_, err = runWF(t, wsDir, "round", "close", "2", "-prose", "answered by `rg x \\| head`")
	noErr(t, err, "round close -prose with an escaped pipe")

	b, err := os.ReadFile(filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "read ledger")
	body := string(b)
	if strings.Contains(body, `\\|`) {
		t.Fatalf("an already-escaped pipe was escaped twice:\n%s", body)
	}
	if strings.Count(body, "`rg x \\| head`") != 2 {
		t.Fatalf("both rows must hold one escaped pipe:\n%s", body)
	}
	// The rows still split into four cells, which is the thing the escape is
	// for: a malformed row is what `check closure` refuses to count.
	cl, err := runWF(t, wsDir, "check", "closure", "-round", "1")
	noErr(t, err, "check closure")
	if strings.Contains(cl, "malformed") {
		t.Fatalf("an escaped pipe must leave the row splittable:\n%s", cl)
	}
}

// Two claims for one cell. Which one the row would carry is a coin flip, and
// the cell is what every later reader tests.
func TestRoundCloseRefusesProseAndACommitTogether(t *testing.T) {
	wsDir, sha := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | NOTE | artifacts/decisions.md a |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	out, err := runWF(t, wsDir, "round", "close", "1", sha, "-prose", "and also a sentence")
	if err == nil {
		t.Fatalf("a commit and -prose together must be refused, got:\n%s", out)
	}
	mustContain(t, err.Error(), "exclusive")
}

// A prose closure on a BLOCKER is the claim that no commit was OWED, and the
// row then rests on the sentence alone. Said where it is written, not left for
// the freeze's verify-by-hand note to imply.
func TestRoundCloseNamesABlockerClosedByProse(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | BLOCKER | artifacts/decisions.md claims a thing |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	out, err := runWF(t, wsDir, "round", "close", "1", "-prose", "refuted by measurement, see the round title")
	noErr(t, err, "round close -prose on a blocker")
	mustContain(t, out, "BLOCKER", "round title")

	// And a NOTE row does NOT get that line: a warning that paints every row is
	// a warning nobody reads.
	ledger(t, wsDir, 1, "| 1 | BLOCKER | artifacts/decisions.md claims a thing | refuted by measurement, see the round title |\n| 2 | NOTE | artifacts/decisions.md b |  |\n")
	_, err = runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "re-record")
	out, err = runWF(t, wsDir, "round", "close", "2", "-prose", "an observation")
	noErr(t, err, "round close -prose on a note")
	if strings.Contains(out, "this row is a BLOCKER") {
		t.Fatalf("a NOTE row must not be told it is a blocker:\n%s", out)
	}
}

// `-replace` naming the same prose writes identical bytes and would die on
// git's `nothing staged` — a git-shaped error for an operator-shaped mistake,
// exactly as for the commit half.
func TestRoundCloseRefusesAReplaceNamingTheSameProse(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | NOTE | artifacts/decisions.md a |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")
	_, err = runWF(t, wsDir, "round", "close", "1", "-prose", "an observation")
	noErr(t, err, "round close -prose")

	out, err := runWF(t, wsDir, "round", "close", "1", "-prose", "an observation", "-replace")
	if err == nil {
		t.Fatalf("-replace naming the same prose must be refused, got:\n%s", out)
	}
	mustContain(t, err.Error(), "names the same prose")

	// A DIFFERENT sentence is a real event and lands.
	_, err = runWF(t, wsDir, "round", "close", "1", "-prose", "an observation, and the reason", "-replace")
	noErr(t, err, "round close -prose -replace with different text")
}

// The scaffold's empty row is a form, not a claim — the same refusal the commit
// half gives, reached through the new door.
func TestRoundCloseRefusesProseOnTheScaffoldsEmptyRow(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	out, err := runWF(t, wsDir, "round", "close", "1", "-prose", "nothing to do")
	if err == nil {
		t.Fatalf("the scaffold's empty row must be refused, got:\n%s", out)
	}
	mustContain(t, err.Error(), "no finding text")
}

// A row is ONE line. A newline would end it mid-cell and the rest would land in
// the document as text nobody wrote — the corruption class this whole command
// exists to remove, arriving through its new door.
func TestRoundCloseRefusesProseCarryingANewline(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | NOTE | artifacts/decisions.md a |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	out, err := runWF(t, wsDir, "round", "close", "1", "-prose", "first line\nsecond line")
	if err == nil {
		t.Fatalf("prose carrying a newline must be refused, got:\n%s", out)
	}
	mustContain(t, err.Error(), "ONE line")
}

// `wf apply <file>` — what two gates typed. It was the one command still using
// `flag.Parse`, so a positional silently dropped every flag after it and the
// usage line then named the flag that had been passed.
func TestApplyAcceptsThePositionalEditsFile(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	// OUTSIDE the workspace: an edits file written into it is a stray the
	// status check below would report as a dirty tree.
	edits := filepath.Join(filepath.Dir(wsDir), "edits.txt")
	write(t, edits, "=== artifacts/decisions.md  count=1  note=positional\n"+
		"<<<<\n# Decisions (revised)\n====\n# Decisions (positional)\n>>>>\n")

	out, err := runWF(t, wsDir, "apply", edits, "-m", "positional")
	noErr(t, err, "apply with a positional file")
	mustContain(t, out, "verified")
	// The flag AFTER the positional survived, which is the half `flag.Parse`
	// dropped. `wf status` prints each artifact's last commit SUBJECT, so the
	// assertion is on the subject and not on a clean tree — a clean tree holds
	// with `-m` discarded, which is the shape this test exists against.
	log, err := runWF(t, wsDir, "status")
	noErr(t, err, "status")
	mustContain(t, log, "tree clean", "positional")
	b, err := os.ReadFile(filepath.Join(wsDir, "artifacts", "decisions.md"))
	noErr(t, err, "read artifact")
	mustContain(t, string(b), "# Decisions (positional)")
}

// Two files, or a file beside `-f`, is not a batch this command can name.
func TestApplyRefusesASecondPositional(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	out, err := runWF(t, wsDir, "apply", "a.json", "b.json")
	if err == nil {
		t.Fatalf("two positionals must be refused, got:\n%s", out)
	}
	mustContain(t, err.Error(), "one file")
}

// `wf round new` writes a findings scaffold and NO brief, and nothing said so
// at the moment it matters. Measured on P4a round 15: the lens named
// `rounds/r15/brief.md` as required reading, the file did not exist, and the
// reviewer reconstructed it from the previous ledger and the tag diff.
func TestRoundNewNamesTheBriefCommandOnADeltaRound(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	_, err := runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze")

	out, err := runWF(t, wsDir, "round", "new", "-kind", "delta", "-title", "t")
	noErr(t, err, "round new delta")
	mustContain(t, out, "wf delta -save", "brief.md")

	// A FULL round re-reads the whole set and is briefed from the artifacts, so
	// the pointer would paint a round that needs no brief. Round 2's ledger is
	// filled first: a freeze refuses to publish an untouched scaffold as a
	// round that found nothing.
	for _, n := range []string{"1", "2"} {
		ledger(t, wsDir, atoi(n), "| 1 | NOTE | artifacts/decisions.md a |  |\n")
		_, err = runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r"+n, "findings.md"), "-round", n)
		noErr(t, err, "record r"+n)
	}
	_, err = runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze r2")
	out, err = runWF(t, wsDir, "round", "new", "-kind", "full", "-title", "t")
	noErr(t, err, "round new full")
	if strings.Contains(out, "wf delta -save") {
		t.Fatalf("a full round is not briefed from a delta:\n%s", out)
	}
}

// A `-kind run` round's findings come from EXECUTING what the gate built, and
// the lines in its window are whatever its own fix happened to touch: the two
// halves of the ratio are about different things. P4a's round 14 printed
// `80.0 finding(s) per 100 changed line(s)` — four findings from a five-agent
// launch over 5 lines, the highest figure of that gate by a factor of seven —
// on the line a reader takes the descent from, and sat inside a prediction
// basis of 5.5. The exclusion is NAMED, like the two beside it: a population
// narrowed in silence is one nobody can argue with.
func TestStatsExcludesARunRoundFromTheCostMeter(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	// Round 1: an ordinary priceable round, so the basis is not empty and the
	// exclusion below is visible as a NARROWING rather than as the whole thing.
	first := filepath.Join(tmp, "first.txt")
	write(t, first, "=== artifacts/decisions.md  count=1  note=seed\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	_, err := runWF(t, wsDir, "apply", "-f", first)
	noErr(t, err, "apply r1")
	ledger(t, wsDir, 1, "| 1 | MAJOR | artifacts/decisions.md a |  |\n| 2 | MAJOR | artifacts/decisions.md b |  |\n")
	_, err = runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "record r1")
	_, err = runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze r1")

	// Round 2: an ordinary delta round. Round 1 is excluded by the rule that a
	// window ADDING an artifact prices a corpus its batches did not write, so
	// without this one the basis would be empty and the run exclusion would be
	// invisible against it.
	_, err = runWF(t, wsDir, "round", "new", "-kind", "delta", "-title", "ordinary")
	noErr(t, err, "round new delta")
	mid := filepath.Join(tmp, "mid.txt")
	write(t, mid, "=== artifacts/decisions.md  count=1  note=middle\n"+
		"<<<<\nThe entry point is:\n====\nThe entry point was:\n>>>>\n")
	_, err = runWF(t, wsDir, "apply", "-f", mid)
	noErr(t, err, "apply r2")
	ledger(t, wsDir, 2, "| 1 | MAJOR | artifacts/decisions.md x |  |\n")
	_, err = runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r2", "findings.md"))
	noErr(t, err, "record r2 ordinary")
	_, err = runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze r2")

	// Round 3: the run round, with a fix of its own so it has a denominator at
	// all — which is exactly the shape that produced the 80.0.
	_, err = runWF(t, wsDir, "round", "new", "-kind", "run", "-title", "the launch")
	noErr(t, err, "round new run")
	second := filepath.Join(tmp, "second.txt")
	write(t, second, "=== artifacts/decisions.md  count=1  note=one line\n"+
		"<<<<\nThe entry point was:\n====\nEntry point:\n>>>>\n")
	_, err = runWF(t, wsDir, "apply", "-f", second)
	noErr(t, err, "apply r2")
	ledger(t, wsDir, 3, "| 1 | MAJOR | artifacts/decisions.md c |  |\n| 2 | MAJOR | artifacts/decisions.md d |  |\n| 3 | MAJOR | artifacts/decisions.md e |  |\n")
	_, err = runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r3", "findings.md"))
	noErr(t, err, "record r3")
	_, err = runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze r3")

	out, err := runWF(t, wsDir, "stats")
	noErr(t, err, "stats")
	// The per-round line, where the ratio is read.
	if !strings.Contains(out, "no ratio — a run round's findings come from EXECUTING") {
		t.Fatalf("a run round must print no ratio on its own line:\n%s", out)
	}
	// And the basis line, which `wf apply` predicts from.
	mustContain(t, out, "excluded — a run round's findings come from EXECUTING")
	if !strings.Contains(out, "prediction basis — rounds 2,") && !strings.Contains(out, "prediction basis — rounds 2 ") {
		t.Fatalf("the ordinary round must remain in the basis:\n%s", out)
	}
	if strings.Contains(out, "prediction basis — rounds 2-3") {
		t.Fatalf("the run round is still in the basis:\n%s", out)
	}
}

// The apply-side half of the same exclusion. `wf stats` naming it while the
// note the operator acts on does not is two commands and one number, with the
// silent one being the command that acts on it — measured once already, on the
// re-freeze exclusion.
func TestApplyNoteNamesTheRunRoundItExcluded(t *testing.T) {
	note, warn := batchNote(3, 400, []histRound{{N: 1, Findings: 20, Lines: 100}}, nil, nil, []int{2})
	if !warn {
		t.Fatal("a history that predicts 5 or more findings must warn")
	}
	mustContain(t, note, "round(s) 2 are NOT in that history either", "CONTAIN a live run")
}

// `wf delta` without `-save` prints a correct brief to stdout and nowhere else,
// and the lens about to be dispatched names `rounds/rN/brief.md` as required
// reading. The note goes to STDERR because stdout IS the brief: a run redirected
// into a file must not carry it.
func TestDeltaSaysWhenTheBriefWentNowhere(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | NOTE | artifacts/decisions.md a |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "record r1")
	_, err = runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze r1")
	_, err = runWF(t, wsDir, "round", "new", "-kind", "delta", "-title", "second")
	noErr(t, err, "round new")

	out, err := runWF(t, wsDir, "delta")
	noErr(t, err, "delta")
	mustContain(t, out, "NOT saved", "brief.md", "wf delta -save")

	// And it is SILENT once the file exists: a note that fires after the
	// operator has done the thing is a note they learn to skip.
	_, err = runWF(t, wsDir, "delta", "-save")
	noErr(t, err, "delta -save")
	out, err = runWF(t, wsDir, "delta")
	noErr(t, err, "delta again")
	if strings.Contains(out, "NOT saved") {
		t.Fatalf("the note must stop once the brief exists:\n%s", out)
	}
}

// `-prose ""` used to read as "no prose", so `wf round close 1 <sha> -prose ""`
// closed by COMMIT in silence — the operator asked for one door and got the
// other — and `-prose ""` alone fell out as a usage line about a commit they had
// not typed. PRESENCE is the claim; an empty one is a claim with nothing in it.
func TestRoundCloseRefusesAnEmptyProse(t *testing.T) {
	wsDir, sha := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | NOTE | artifacts/decisions.md a |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "round record")

	out, err := runWF(t, wsDir, "round", "close", "1", "-prose", "")
	if err == nil {
		t.Fatalf("an empty -prose must be refused, got:\n%s", out)
	}
	mustContain(t, err.Error(), "passed with nothing in it")

	// The half that mattered: with a commit beside it, the empty flag used to be
	// invisible and the row closed by commit.
	out, err = runWF(t, wsDir, "round", "close", "1", sha, "-prose", "")
	if err == nil {
		t.Fatalf("an empty -prose beside a commit must be refused, got:\n%s", out)
	}
	b, err := os.ReadFile(filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "read ledger")
	if strings.Contains(string(b), sha) {
		t.Fatal("the row was closed by commit while -prose was passed")
	}
}

// A `brief.md` left over from an earlier save is exactly as wrong as no brief at
// all: the lens reads the file, and the file is a different brief. The first
// version of this note asked only whether the file EXISTED.
func TestDeltaSaysWhenTheSavedBriefIsADifferentOne(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | NOTE | artifacts/decisions.md a |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "record r1")
	_, err = runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze r1")
	_, err = runWF(t, wsDir, "round", "new", "-kind", "delta", "-title", "second")
	noErr(t, err, "round new")
	_, err = runWF(t, wsDir, "delta", "-save")
	noErr(t, err, "delta -save")

	// The artifacts move, so the brief the command would print is no longer the
	// brief on disk.
	batch := filepath.Join(filepath.Dir(wsDir), "more.txt")
	write(t, batch, "=== artifacts/decisions.md  count=1  note=later\n"+
		"<<<<\n# Decisions (revised)\n====\n# Decisions (revised again)\n>>>>\n")
	_, err = runWF(t, wsDir, "apply", "-f", batch)
	noErr(t, err, "apply")

	out, err := runWF(t, wsDir, "delta")
	noErr(t, err, "delta")
	mustContain(t, out, "DIFFERENT brief", "wf delta -save")
}

// `roundKinds` reads round subjects out of `git log`, which is newest-first. The
// OLDEST match is the round's actual opening commit — a round is opened once —
// and taking the newest let any later commit whose subject reads like an opening
// re-label the round for every reader: the round-time median, the lens
// classification, and the cost meter's exclusions.
func TestRoundKindsTakesTheOpeningCommitNotTheNewestSubject(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)
	// newGate opens round 1 as a FULL round. A later apply whose subject
	// imitates the opening must not turn it into a run round.
	batch := filepath.Join(tmp, "spoof.txt")
	write(t, batch, "=== artifacts/decisions.md  count=1  note=x\n"+
		"<<<<\nEntry point:\n====\nThe entry point is:\n>>>>\n")
	_, err := runWF(t, wsDir, "apply", "-f", batch, "-m", "round 1: open (run) — not really")
	noErr(t, err, "apply with a spoofing subject")

	w, err := ws.Find(wsDir)
	noErr(t, err, "find workspace")
	if got := roundKinds(w)[1]; got != "full" {
		t.Fatalf("round 1 was opened as full and a later subject must not re-label it, got %q", got)
	}
}

// runWFSplit is runWF with the two streams kept APART. The shared helper points
// one pipe at both, so a test asserting "this goes to stderr" passes when it
// goes to stdout — which is the exact failure the assertion is written against.
func runWFSplit(t *testing.T, dir string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	t.Chdir(dir)
	ro, wo, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	re, we, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = wo, we
	outC, errC := make(chan string, 1), make(chan string, 1)
	go func() { b, _ := io.ReadAll(ro); outC <- string(b) }()
	go func() { b, _ := io.ReadAll(re); errC <- string(b) }()
	cmdErr := dispatch(args)
	_ = wo.Close()
	_ = we.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	return <-outC, <-errC, cmdErr
}

// The note is about a file a LENS reads, and stdout is the brief itself: a
// `wf delta > brief.txt` must not carry the note into the file the reviewer is
// then handed.
func TestDeltaNoteGoesToStderrAndNotIntoTheBrief(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | NOTE | artifacts/decisions.md a |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "record r1")
	_, err = runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze r1")
	_, err = runWF(t, wsDir, "round", "new", "-kind", "delta", "-title", "second")
	noErr(t, err, "round new")

	stdout, stderr, err := runWFSplit(t, wsDir, "delta")
	noErr(t, err, "delta")
	if !strings.Contains(stderr, "NOT saved") {
		t.Fatalf("the note belongs on stderr:\n%s", stderr)
	}
	if strings.Contains(stdout, "NOT saved") {
		t.Fatalf("the note must not land in the brief:\n%s", stdout)
	}
	mustContain(t, stdout, "# delta")
}

// The note is about the ROUND's own brief, so it belongs only on the default
// range. `wf delta -from X -to Y` is a legitimate ad-hoc read, and with the
// content comparison in place the note told the operator that a correct,
// current brief was a different one.
func TestDeltaIsSilentOnAnAdHocRange(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | NOTE | artifacts/decisions.md a |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "record r1")
	_, err = runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze r1")
	_, err = runWF(t, wsDir, "round", "new", "-kind", "delta", "-title", "second")
	noErr(t, err, "round new")
	_, err = runWF(t, wsDir, "delta", "-save")
	noErr(t, err, "delta -save")

	_, stderr, err := runWFSplit(t, wsDir, "delta", "-from", "gate-r1", "-to", "HEAD~1")
	noErr(t, err, "ad-hoc delta")
	if strings.Contains(stderr, "NOT saved") {
		t.Fatalf("an ad-hoc range says nothing about the round's brief:\n%s", stderr)
	}
}

// The unreadable `brief.md` is a THIRD state and says so: reporting it as
// absent would be a guess about somebody's disk, and reporting nothing would be
// an absent measurement in the register of a measured one.
func TestDeltaNamesABriefItCouldNotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a 0000 file, so the branch is unreachable here")
	}
	wsDir, _ := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | NOTE | artifacts/decisions.md a |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "record r1")
	_, err = runWF(t, wsDir, "freeze")
	noErr(t, err, "freeze r1")
	_, err = runWF(t, wsDir, "round", "new", "-kind", "delta", "-title", "second")
	noErr(t, err, "round new")
	_, err = runWF(t, wsDir, "delta", "-save")
	noErr(t, err, "delta -save")

	p := filepath.Join(wsDir, "rounds", "r2", "brief.md")
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(p, 0o644) }()

	_, stderr, err := runWFSplit(t, wsDir, "delta")
	noErr(t, err, "delta over an unreadable brief")
	mustContain(t, stderr, "could not be read", "UNKNOWN")
}

// `wf apply -whole <path> <another>` is not a batch this command can name, and
// the positional acceptance must not swallow the second argument into `-f`
// beside an adoption.
func TestApplyRefusesAPositionalBesideWhole(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	out, err := runWF(t, wsDir, "apply", "-whole", "artifacts/decisions.md", "extra.json")
	if err == nil {
		t.Fatalf("a positional beside -whole must be refused, got:\n%s", out)
	}
	mustContain(t, err.Error(), "one file")
}

// One positional and no `-prose` is the commit form with the commit missing.
// The usage line names BOTH doors, because the operator who typed this wanted
// one of them and the message cannot know which.
func TestRoundCloseRefusesAFindingWithNoClosure(t *testing.T) {
	wsDir, _ := twoArtifactGate(t)
	ledger(t, wsDir, 1, "| 1 | NOTE | artifacts/decisions.md a |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	noErr(t, err, "record r1")

	out, err := runWF(t, wsDir, "round", "close", "1")
	if err == nil {
		t.Fatalf("a finding number with no closure must be refused, got:\n%s", out)
	}
	mustContain(t, err.Error(), "<commit>", "-prose")
}

// `wf round new` names the brief command on a delta round, and round 1 has no
// earlier freeze to diff FROM — so the pointer would name a command that
// refuses. The `n > 1` half of that condition was untested.
func TestRoundNewIsSilentAboutTheBriefOnTheFirstRound(t *testing.T) {
	setGitEnv(t)
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	write(t, filepath.Join(target, "a.md"), "x\n")
	gitIn(t, target, "init", "-q", "-b", "main")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-q", "-m", "initial")
	wsDir := filepath.Join(tmp, "ws")
	_, err := runWF(t, tmp, "init", "gate", "-target", target, "-dir", wsDir)
	noErr(t, err, "init")

	out, err := runWF(t, wsDir, "round", "new", "-kind", "delta", "-title", "first")
	noErr(t, err, "round new")
	if strings.Contains(out, "wf delta -save") {
		t.Fatalf("round 1 has nothing to diff from:\n%s", out)
	}
}
