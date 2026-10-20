package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dop251/goja"

	wfgate "github.com/yashok111/wfgate"
)

// A finding cites LINES and mentions a section in passing. The section half
// used to PREEMPT the line half — any section id anywhere in the finding text
// made the check skip the lines entirely — so a closure whose cited lines the
// commit had covered was reported as missing a section the finding was not
// about. Measured on the gate that found it: one of its three reported misses,
// and five more of one earlier gate plus one of another, 92 rows corpus-wide
// down to 85 with nothing added — 83 in the first cut, which was two rows of a
// false green the sibling-span test below now pins.
func TestCheckClosure_lineCitationLandsThoughAMentionedSectionDidNot(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	// Line 5 is `one` (inside D1), line 13 is `three` (inside D3).
	doc := "# Decisions\n\n## D1 — the first\n\none\n\n## D2 — the second\n\ntwo\n\n## D3 — the third\n\nthree\n"
	seed := filepath.Join(tmp, "seed.txt")
	write(t, seed, "=== artifacts/decisions.md  count=1  note=sections\n"+
		"<<<<\n# Decisions\n\nEntry point: `src/main.go:3#NeedleFn`.\n====\n"+doc+">>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", seed); err != nil {
		t.Fatal(err)
	}

	// A fix inside D3 only, at line 13.
	fix := filepath.Join(tmp, "fix.txt")
	write(t, fix, "=== artifacts/decisions.md  count=1  note=fix D3\n<<<<\nthree\n====\nthree, corrected\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", fix)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, applied)

	ledger(t, wsDir, 1,
		"| 1 | MAJOR | `artifacts/decisions.md:13` restates what D1 already says | "+sha+" |\n"+
			"| 2 | MAJOR | `artifacts/decisions.md:5` contradicts D1 | "+sha+" |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}

	out, err := runWF(t, wsDir, "check", "closure")
	if !errors.Is(err, errFindings) {
		t.Fatalf("finding 2 cites a line nothing touched, so the check reports: %v\n%s", err, out)
	}
	// Finding 1: the cited line WAS covered, and D1 is mentioned, not cited.
	if strings.Contains(out, "[1] closed by") {
		t.Fatalf("a covered line citation closes the row whatever section the text mentions:\n%s", out)
	}
	// Finding 2: neither half landed, and the row says BOTH — the section it
	// names and the line it cites, in one verdict per row.
	mustContain(t, out, "[2] closed by "+sha,
		"NOT any section it cites: D1",
		"but not line 5")
}

// The report carries one verdict per ROW. A finding that misses on both halves
// prints them joined, never as two rows: the headline counts claims, and a row
// counted twice would let it exceed the population it was drawn from.
func TestCheckClosure_oneRowCarriesBothMisses(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	doc := "# Decisions\n\n## D1 — the first\n\none\n\n## D2 — the second\n\ntwo\n"
	seed := filepath.Join(tmp, "seed.txt")
	write(t, seed, "=== artifacts/decisions.md  count=1  note=sections\n"+
		"<<<<\n# Decisions\n\nEntry point: `src/main.go:3#NeedleFn`.\n====\n"+doc+">>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", seed); err != nil {
		t.Fatal(err)
	}
	fix := filepath.Join(tmp, "fix.txt")
	write(t, fix, "=== artifacts/decisions.md  count=1  note=fix D2\n<<<<\ntwo\n====\ntwo, corrected\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", fix)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, applied)

	ledger(t, wsDir, 1, "| 1 | MAJOR | `artifacts/decisions.md:5` is what D1 gets wrong | "+sha+" |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, _ := runWF(t, wsDir, "check", "closure")
	if n := strings.Count(out, "[1] closed by"); n != 1 {
		t.Fatalf("one verdict per row, got %d:\n%s", n, out)
	}
	row := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "[1] closed by") {
			row = l
		}
	}
	// BOTH reasons on that ONE line, joined: the section the finding names and
	// the line it cites, so the operator reads the whole verdict in one place.
	if !strings.Contains(row, "NOT any section it cites: D1") || !strings.Contains(row, "but not line 5") {
		t.Fatalf("one row carries both misses, got:\n%s", row)
	}
	mustContain(t, out, "1 not closed by that commit")
}

// A closure names the commit that FIXED the artifact. Eight of one round's nine
// closures on a live gate named the ledger commit of the PREVIOUS
// `wf round close` — the operator read HEAD after each call and fed it to the
// next — and every one of them passed `wf round record`, which proves only that
// the sha exists. The refusal is here, where the row is written.
func TestRoundClose_refusesACommitThatTouchedNoArtifact(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	fix := filepath.Join(tmp, "fix.txt")
	write(t, fix, "=== artifacts/decisions.md  count=1  note=fix\n<<<<\nEntry point\n====\nThe entry point\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", fix)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, applied)

	ledger(t, wsDir, 1,
		"| 1 | MAJOR | the opening sentence of `artifacts/decisions.md` is wrong |  |\n"+
			"| 2 | MAJOR | the second sentence too |  |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	// The right commit lands.
	if _, err := runWF(t, wsDir, "round", "close", "1", sha); err != nil {
		t.Fatalf("a commit that touched the artifact closes the row: %v", err)
	}
	// And that closure's OWN ledger commit — the shape the operator fed
	// forward — is refused.
	ledgerShaRaw, err := gitOut(t, wsDir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	ledgerSha := strings.TrimSpace(ledgerShaRaw)
	out, err := runWF(t, wsDir, "round", "close", "2", ledgerSha)
	if err == nil {
		t.Fatalf("a bookkeeping commit closes no finding:\n%s", out)
	}
	for _, want := range []string{"touched no artifact", "rounds/r1/findings.md", "-prose"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal names what the commit did touch and the door for a row no commit closes, got: %v", err)
		}
	}
	// Nothing was written: the row is still open, and the tree is clean.
	body := read(t, filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	if strings.Contains(body, ledgerSha[:7]) {
		t.Fatalf("the refusal wrote the sha anyway:\n%s", body)
	}
	status, err := gitOut(t, wsDir, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if st := strings.TrimSpace(status); st != "" {
		t.Fatalf("a refusal leaves nothing staged, got:\n%s", st)
	}
}

// The pipe refusals name the escape AND say that a backtick code span is not
// one. An operator wrote a row carrying `||` inside a code span and read the
// refusal as being about something else: the row is split before any markdown
// exists, and nothing in the message said so.
func TestPipeRefusals_sayACodeSpanIsNotAnEscape(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 1 | MAJOR | the guard reads `a || b` and takes the first |  |\n")
	_, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md"))
	if err == nil {
		t.Fatal("a row of six cells is not a row of four")
	}
	mustContain(t, err.Error(), "escape a literal pipe as \\|", "code span is NOT an escape")
}

// The emitted skeleton's ownership check compares an agent's SELF-REPORTED
// paths against a repo-relative declaration. One real run reported an ABSOLUTE
// path and was recorded as writing outside a block whose first line names that
// very file — a false major on a correct write, which is the direction that
// teaches an operator to stop reading the report.
//
// The function is lifted out of the template and executed on the same engine
// `wfdry` uses: running the whole skeleton needs the orchestration harness, and
// asserting on its TEXT would pass with the comparison reverted.
func TestTemplateOwnership_normalisesAnAbsoluteSelfReport(t *testing.T) {
	tpl, err := wfgate.Templates.ReadFile("templates/script.mjs")
	if err != nil {
		t.Fatal(err)
	}
	src := jsFunction(t, string(tpl), "ownership")

	for _, tc := range []struct {
		name  string
		files []string
		want  int64
	}{
		{"absolute", []string{"/repo/CLAUDE.md"}, 0},
		{"dot-relative", []string{"./CLAUDE.md"}, 0},
		{"relative", []string{"CLAUDE.md"}, 0},
		// The check still CATCHES: normalising a path must not turn every
		// report green, which is the mutation this fix could have shipped.
		{"unowned", []string{"/repo/internal/other.go"}, 1},
		// The prefix is stripped at a path BOUNDARY only: without that,
		// `/repoCLAUDE.md` — a write outside the repository — reads as the
		// owned `CLAUDE.md`.
		{"prefix-not-boundary", []string{"/repoCLAUDE.md"}, 1},
		// The prefix is matched at the START, never mid-string: a nested
		// checkout or a backup copy carries the repo root inside its path and
		// is not that repository.
		{"repo-root-mid-string", []string{"/tmp/backup/repo/CLAUDE.md"}, 1},
		// The self-report is untrusted text, which is the whole reason `relOf`
		// exists.
		{"padded", []string{"  /repo/CLAUDE.md  "}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := goja.New()
			prog := "const REPO = '/repo'\nconst carried = []\n" + src + "\n" +
				"ownership('s', ['CLAUDE.md — the one paragraph'], " + jsArray(tc.files) + ")\ncarried.length\n"
			v, err := vm.RunString(prog)
			if err != nil {
				t.Fatalf("%v\n%s", err, prog)
			}
			if got := v.ToInteger(); got != tc.want {
				t.Fatalf("%v: %d finding(s), want %d", tc.files, got, tc.want)
			}
		})
	}
}

// jsFunction cuts one top-level `function name(...) { ... }` out of a script by
// brace balance. Naming the function is the point: a rename lands here as a
// failure rather than as a test that quietly asserts nothing.
func jsFunction(t *testing.T, src, name string) string {
	t.Helper()
	i := strings.Index(src, "\nfunction "+name+"(")
	if i < 0 {
		t.Fatalf("the template no longer declares function %s at top level", name)
	}
	depth, start := 0, -1
	for j := i; j < len(src); j++ {
		switch src[j] {
		case '{':
			if depth == 0 {
				start = j
			}
			depth++
		case '}':
			depth--
			if depth == 0 && start >= 0 {
				return src[i : j+1]
			}
		}
	}
	t.Fatalf("function %s is not brace-balanced", name)
	return ""
}

func jsArray(items []string) string {
	q := make([]string, 0, len(items))
	for _, s := range items {
		q = append(q, "'"+s+"'")
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// A finding citing TWO spans on one path is TWO claims, and a commit covering
// one of them closes one of them. The wave's first cut suppressed every sibling
// span the moment any span landed — a false green introduced while another was
// being removed, and it was live in the corpus: two rows of one gate came back
// when it was corrected, one of them a finding whose two cited lines were 444
// and 716 with neither touched.
func TestCheckClosure_aCoveredSpanDoesNotSuppressItsSiblings(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	// Line 5 is `one`, line 13 is `three`.
	doc := "# Decisions\n\n## D1 — the first\n\none\n\n## D2 — the second\n\ntwo\n\n## D3 — the third\n\nthree\n"
	seed := filepath.Join(tmp, "seed.txt")
	write(t, seed, "=== artifacts/decisions.md  count=1  note=sections\n"+
		"<<<<\n# Decisions\n\nEntry point: `src/main.go:3#NeedleFn`.\n====\n"+doc+">>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", seed); err != nil {
		t.Fatal(err)
	}
	fix := filepath.Join(tmp, "fix.txt")
	write(t, fix, "=== artifacts/decisions.md  count=1  note=fix line 13\n<<<<\nthree\n====\nthree, corrected\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", fix)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, applied)

	ledger(t, wsDir, 1, "| 1 | MAJOR | `artifacts/decisions.md:13` and `artifacts/decisions.md:5` say the same thing twice | "+sha+" |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "closure")
	if !errors.Is(err, errFindings) {
		t.Fatalf("the span nothing touched is still a claim: %v\n%s", err, out)
	}
	mustContain(t, out, "[1] closed by "+sha, "but not line 5")
	if strings.Contains(out, "but not line 13") {
		t.Fatalf("the covered span is closed:\n%s", out)
	}
}

// A workspace holding NO artifact must not become an escape: the first cut
// returned early on an empty artifact list, so a bookkeeping commit closed a
// finding in exactly the workspace where nothing could have fixed anything.
func TestRoundClose_refusesAlsoWhenTheWorkspaceHasNoArtifact(t *testing.T) {
	setGitEnv(t)
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	write(t, filepath.Join(target, "src", "main.go"), "package main\n")
	gitIn(t, target, "init", "-q", "-b", "main")
	gitIn(t, target, "add", "-A")
	gitIn(t, target, "commit", "-q", "-m", "initial")
	wsDir := filepath.Join(tmp, "ws")
	if _, err := runWF(t, tmp, "init", "demo", "-target", target, "-dir", wsDir); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "no artifact yet"); err != nil {
		t.Fatal(err)
	}
	ledger(t, wsDir, 1, "| 1 | MAJOR | the workspace holds nothing yet |  |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	headRaw, err := gitOut(t, wsDir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := runWF(t, wsDir, "round", "close", "1", strings.TrimSpace(headRaw)); err == nil {
		t.Fatalf("a workspace with no artifact is not an escape:\n%s", out)
	} else if !strings.Contains(err.Error(), "touched no artifact") {
		t.Fatalf("the refusal is the same one, got: %v", err)
	}
}

// A closure citing a SECTION and no line was checked by the section half and
// must not be counted as file-only: the note that says "cite the section id or
// the line and the check reaches the claim" would then be printed at a closure
// that cited one. The `sawSection` wiring is what separates the two, and the
// mutation sweep found it defended by nothing.
func TestCheckClosure_aSectionOnlyClosureIsNotFileOnly(t *testing.T) {
	wsDir := newGate(t)
	tmp := filepath.Dir(wsDir)

	doc := "# Decisions\n\n## D1 — the first\n\none\n\n## D2 — the second\n\ntwo\n"
	seed := filepath.Join(tmp, "seed.txt")
	write(t, seed, "=== artifacts/decisions.md  count=1  note=sections\n"+
		"<<<<\n# Decisions\n\nEntry point: `src/main.go:3#NeedleFn`.\n====\n"+doc+">>>>\n")
	if _, err := runWF(t, wsDir, "apply", "-f", seed); err != nil {
		t.Fatal(err)
	}
	fix := filepath.Join(tmp, "fix.txt")
	write(t, fix, "=== artifacts/decisions.md  count=1  note=fix D1\n<<<<\none\n====\none, corrected\n>>>>\n")
	applied, err := runWF(t, wsDir, "apply", "-f", fix)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitShaOf(t, applied)

	ledger(t, wsDir, 1, "| 1 | MAJOR | D1 states the rule wrongly | "+sha+" |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "closure")
	if err != nil {
		t.Fatalf("the cited section was reached: %v\n%s", err, out)
	}
	if strings.Contains(out, "cite neither a section this artifact heads nor a line") {
		t.Fatalf("a closure that cited a section is not file-only:\n%s", out)
	}
}

// The prefix is a path BOUNDARY. Without the trailing slash a workspace-root
// `artifacts.md` — or an `artifacts-old/` copy of a pruned gate — satisfies the
// guard and closes a row nothing fixed.
func TestRoundClose_theArtifactPrefixIsABoundary(t *testing.T) {
	wsDir := newGate(t)
	write(t, filepath.Join(wsDir, "artifacts.md"), "not an artifact\n")
	gitIn(t, wsDir, "add", "artifacts.md")
	gitIn(t, wsDir, "commit", "-q", "-m", "a file whose name starts like the directory")
	headRaw, err := gitOut(t, wsDir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	ledger(t, wsDir, 1, "| 1 | MAJOR | `artifacts/decisions.md` opens wrongly |  |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	if out, err := runWF(t, wsDir, "round", "close", "1", strings.TrimSpace(headRaw)); err == nil {
		t.Fatalf("`artifacts.md` is not inside `artifacts/`:\n%s", out)
	} else if !strings.Contains(err.Error(), "touched no artifact") {
		t.Fatalf("the refusal is the same one, got: %v", err)
	}
}

// A commit that touched NOTHING says so, rather than printing an empty list.
// The refusal is read by an operator who is about to go and look for the right
// sha, and "it changed " with nothing after it names no evidence at all.
func TestRoundClose_anEmptyCommitIsNamedAsTouchingNoFile(t *testing.T) {
	wsDir := newGate(t)
	gitIn(t, wsDir, "commit", "-q", "--allow-empty", "-m", "an empty commit")
	headRaw, err := gitOut(t, wsDir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	ledger(t, wsDir, 1, "| 1 | MAJOR | `artifacts/decisions.md` opens wrongly |  |\n")
	if _, err := runWF(t, wsDir, "round", "record", filepath.Join(wsDir, "rounds", "r1", "findings.md")); err != nil {
		t.Fatal(err)
	}
	_, err = runWF(t, wsDir, "round", "close", "1", strings.TrimSpace(headRaw))
	if err == nil {
		t.Fatal("an empty commit closes nothing")
	}
	// The round, the finding, the promise, and what the commit did.
	for _, want := range []string{"round 1 finding 1", "no file at all", "nothing was written"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal carries its context and its promise, got: %v", err)
		}
	}
}

// The pipe refusal `wf freeze` gives is the SECOND of three copies, and the
// sweep found only the first asserted. A clause that exists in one copy and not
// in another is how this project's most-repeated defect starts.
func TestFreezePipeRefusal_saysACodeSpanIsNotAnEscape(t *testing.T) {
	wsDir := newGate(t)
	ledger(t, wsDir, 1, "| 1 | MAJOR | the guard reads `a || b` and takes the first |  |\n")
	gitIn(t, wsDir, "add", "-A")
	gitIn(t, wsDir, "commit", "-q", "-m", "round 1: record findings by hand")
	_, err := runWF(t, wsDir, "freeze")
	if err == nil {
		t.Fatal("a row the parser cannot split cannot be checked for an open blocker")
	}
	mustContain(t, err.Error(), "escape a literal pipe as \\|", "code span is NOT an escape")
}
