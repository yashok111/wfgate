package wfdry

import (
	"strconv"
	"strings"
	"testing"

	wfgate "git.sumka.site/yakov/wfgate"
	"git.sumka.site/yakov/wfgate/internal/ws"
)

func securitySkeleton(t *testing.T) string {
	t.Helper()
	b, err := wfgate.Templates.ReadFile("templates/security.mjs")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func runSecurity(t *testing.T, body, args string, modes ...string) []Result {
	t.Helper()
	rs, err := Run(script(t, body), modes, Options{Type: ws.TypeSecurity, Args: args})
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func TestSecuritySkeletonAllModes(t *testing.T) {
	for _, r := range runTyped(t, securitySkeleton(t), ws.TypeSecurity) {
		if r.Err != nil || len(r.Violations) > 0 {
			t.Errorf("%s: err=%v violations=%v", r.Mode, r.Err, r.Violations)
		}
	}
	modes := strings.Join(ModesFor(ws.TypeSecurity), " ")
	if !strings.Contains(modes, inconclusiveMode) || !strings.Contains(modes, deadReproMode) {
		t.Fatalf("a security run walks the undecided verdict and dead provers: %s", modes)
	}
	for _, typ := range []string{ws.TypeReview, ws.TypeHunt, ws.TypeAudit} {
		if strings.Contains(strings.Join(ModesFor(typ), " "), inconclusiveMode) {
			t.Fatalf("`inconclusive` is a security run's mode alone, not %s's", typ)
		}
	}
}

// `clean` walks the whole pipeline: refuting verifiers confirm, a confirmed
// blocker gets an isolated PoC, the confirmed findings seed a variant round,
// and the merge runs. The reported blocker is EXPLOITED.
func TestSecurityCleanWalksEveryPhase(t *testing.T) {
	r := byMode(runTyped(t, securitySkeleton(t), ws.TypeSecurity, "clean"), "clean")
	calls := strings.Join(r.Calls, " ")
	for _, want := range []string{"verify:read:", "verify:repro:", "find:r2:variant:", "merge"} {
		if !strings.Contains(calls, want) {
			t.Fatalf("clean must reach %s: %s", want, calls)
		}
	}
	for _, c := range r.Calls {
		if strings.HasPrefix(c, "verify:repro:") != strings.HasSuffix(c, "/worktree]") {
			t.Fatalf("only provers carry isolation: 'worktree': %s", c)
		}
	}
	exploited := false
	for _, c := range retOf(r)["confirmed"].([]any) {
		if m := c.(map[string]any); m["exploited"] == true && m["severity"] == "blocker" {
			exploited = true
		}
	}
	if !exploited || retOf(r)["complete"] != true {
		t.Fatalf("clean must report an EXPLOITED blocker over a complete run: %+v", retOf(r))
	}
}

// The third verdict: every verifier says it cannot decide. Those findings are
// listed apart — not confirmed, not refuted — no PoC is owed for them, and no
// variant round starts from them.
func TestSecurityInconclusiveListedApart(t *testing.T) {
	r := byMode(runTyped(t, securitySkeleton(t), ws.TypeSecurity, inconclusiveMode), inconclusiveMode)
	if len(r.Violations) > 0 {
		t.Fatalf("the skeleton must pass `inconclusive`: %v", r.Violations)
	}
	m := retOf(r)
	if inc, _ := m["inconclusive"].([]any); len(inc) == 0 {
		t.Fatalf("undecided findings must be returned: %+v", m)
	}
	if c, _ := m["confirmed"].([]any); len(c) != 0 {
		t.Fatalf("an undecided finding is not confirmed: %+v", c)
	}
	if strings.Contains(strings.Join(r.Calls, " "), "verify:repro:") {
		t.Fatalf("nothing undecided is owed a PoC: %v", r.Calls)
	}
	if !strings.Contains(m["report"].(string), "CANNOT VERIFY — needs a human") {
		t.Fatalf("the report must list them for a human")
	}
}

// The defect the mode exists for: a script that drops its undecided findings.
func TestSecurityInconclusiveDroppedIsRefused(t *testing.T) {
	body := mutate(t, securitySkeleton(t), "  inconclusive: inconclusive.map(", "  forHumans: inconclusive.map(")
	r := byMode(runSecurity(t, body, DefaultArgs, inconclusiveMode), inconclusiveMode)
	wantViolation(t, r, "inconclusive: every verifier returned an undecided verdict")
}

// A verdict with no undecided member cannot say "cannot verify", and the mode
// then walked the clean path: refused, naming the vocabulary.
func TestSecurityTwoValuedVerdictIsRefused(t *testing.T) {
	body := mutate(t, securitySkeleton(t), "enum: ['true-positive', 'false-positive', 'cannot-verify']", "enum: ['true-positive', 'false-positive']")
	r := byMode(runSecurity(t, body, DefaultArgs, inconclusiveMode), inconclusiveMode)
	wantViolation(t, r, "no VERIFIER enum")
}

// A verifier that confirms without a trace from the entry point has
// confirmed the sink alone: listed as undecided, never as confirmed.
func TestSecurityConfirmationNeedsATrace(t *testing.T) {
	body := mutate(t, securitySkeleton(t), "const trace = String(v.trace || '').trim()", "const trace = ''")
	m := retOf(byMode(runSecurity(t, body, DefaultArgs, "clean"), "clean"))
	if c, _ := m["confirmed"].([]any); len(c) != 0 {
		t.Fatalf("a traceless confirmation must not be confirmed: %+v", c)
	}
	if inc, _ := m["inconclusive"].([]any); len(inc) == 0 {
		t.Fatalf("a traceless confirmation is undecided: %+v", m)
	}
}

// A prover without `isolation: 'worktree'` is refused in a security run, as a
// hunt's reproducer is.
func TestSecurityProverMustBeIsolated(t *testing.T) {
	body := mutate(t, securitySkeleton(t), "effort: 'medium', isolation: 'worktree', schema: REPRO_SCHEMA", "effort: 'medium', schema: REPRO_SCHEMA")
	r := byMode(runSecurity(t, body, DefaultArgs, "clean"), "clean")
	wantViolation(t, r, "has no `isolation: 'worktree'`")
}

// A dead prover leaves its finding confirmed by reading, and the run
// incomplete: a script that ignores the missing proof is refused.
func TestSecurityDeadProverReachesTheVerdict(t *testing.T) {
	r := byMode(runTyped(t, securitySkeleton(t), ws.TypeSecurity, deadReproMode), deadReproMode)
	if len(r.Violations) > 0 || retOf(r)["complete"] != false {
		t.Fatalf("a dead prover makes the run incomplete: %v %+v", r.Violations, retOf(r)["complete"])
	}
	body := mutate(t, securitySkeleton(t), "&& pocDied.length === 0 ", "")
	wantViolation(t, byMode(runSecurity(t, body, DefaultArgs, deadReproMode), deadReproMode), "dead-repro: the run returned `complete: true`")
}

// The scanner intake, walked with an args.scan the way `wf scan` writes it:
// a declared scanner that failed and one never supplied are NAMED and make the
// run incomplete; a hit in a unit becomes a scan cell; a hit outside every
// unit is counted; an undeclared scanner is ignored by name.
const scanArgsJSON = `{"scan":{"tools":[
 {"tool":"semgrep","ok":true,"results":2},
 {"tool":"trivy","ok":false,"results":0,"note":"db download failed"},
 {"tool":"other","ok":true,"results":1}],
 "candidates":[
 {"id":"semgrep-1","tool":"semgrep","rule":"go.sqli","file":"internal/db/q.go","line":12,"message":"tainted query"},
 {"id":"semgrep-2","tool":"semgrep","rule":"go.ssrf","file":"vendor/x.go","line":3,"message":"outside"},
 {"id":"other-1","tool":"other","rule":"r","file":"internal/db/q.go","line":1,"message":"m"}]}}`

func scanSkeleton(t *testing.T) string {
	body := mutate(t, securitySkeleton(t), "const SCANNERS = []", "const SCANNERS = ['semgrep', 'trivy', 'betterleaks']")
	return mutate(t, body, "{ id: 'TODO-unit', paths: ['TODO/'] }", "{ id: 'db', paths: ['internal/db/'] }")
}

func TestSecurityScannerIntake(t *testing.T) {
	r := byMode(runSecurity(t, scanSkeleton(t), scanArgsJSON, "clean"), "clean")
	if !strings.Contains(strings.Join(r.Calls, " "), "find:r1:scan:db") {
		t.Fatalf("a hit in a unit must get a scan cell: %v", r.Calls)
	}
	m := retOf(r)
	report := m["report"].(string)
	for _, want := range []string{"trivy FAILED", "betterleaks NOT SUPPLIED", "semgrep 2 hit(s)", "1 hit(s) outside every unit", "ignored: other"} {
		if !strings.Contains(report, want) {
			t.Fatalf("the scanner line must say %q:\n%s", want, report)
		}
	}
	if m["complete"] != false {
		t.Fatalf("a failed or missing scanner makes the run incomplete")
	}
	if g, _ := m["scanGaps"].([]any); len(g) != 2 {
		t.Fatalf("two scanner gaps, named: %+v", m["scanGaps"])
	}
}

// Every hit is accounted for once. Each hit id is a REQUIRED property of the
// finder's `hits`, so a chunk of two is answered twice and the run is
// complete — the array of dismissals this replaced answered one of two on the
// unmodified skeleton and turned `clean` red over a real args.json. A hit
// judged vulnerable with no finding filed is UNADJUDICATED, and the run is not
// complete: walked by flipping the enum so the stub's first member is
// `vulnerable`.
func TestSecurityEveryHitAdjudicated(t *testing.T) {
	two := `{"scan":{"tools":[{"tool":"semgrep","ok":true,"invoked":true,"results":2}],"candidates":[{"id":"semgrep-1","tool":"semgrep","rule":"r","file":"internal/db/q.go","line":12,"message":"m"},{"id":"semgrep-2","tool":"semgrep","rule":"r","file":"internal/db/q.go","line":40,"message":"m"}]}}`
	one := mutate(t, scanSkeleton(t), "const SCANNERS = ['semgrep', 'trivy', 'betterleaks']", "const SCANNERS = ['semgrep']")
	for _, mode := range []string{"clean", emptyMode, inconclusiveMode} {
		r := byMode(runSecurity(t, one, two, mode), mode)
		if m := retOf(r); len(r.Violations) > 0 || m["complete"] != true || len(m["unadjudicated"].([]any)) != 0 {
			t.Fatalf("%s: two hits, both answered — complete: %v %+v", mode, r.Violations, m)
		}
	}
	flipped := mutate(t, one, "enum: ['dismissed', 'vulnerable']", "enum: ['vulnerable', 'dismissed']")
	m := retOf(byMode(runSecurity(t, flipped, two, "clean"), "clean"))
	if u, _ := m["unadjudicated"].([]any); len(u) != 2 || m["complete"] != false {
		t.Fatalf("a hit judged vulnerable with no finding is unadjudicated and the run incomplete: %+v", m)
	}
	if !strings.Contains(m["report"].(string), "UNADJUDICATED scanner hit semgrep-2") || !strings.Contains(m["report"].(string), "judged vulnerable, no finding filed") {
		t.Fatalf("the report must name the unadjudicated hit and why")
	}
	body := mutate(t, flipped, " && unadjudicated.length === 0", "")
	if m := retOf(byMode(runSecurity(t, body, two, "clean"), "clean")); m["complete"] != true {
		t.Fatalf("the mutation must flip the verdict, or this test proves nothing")
	}
}

// A hit with no file location is assigned to no unit: `wf scan` counts it as
// dropped, and the run names it and is incomplete.
func TestSecurityDroppedHitsAreAHole(t *testing.T) {
	one := mutate(t, scanSkeleton(t), "const SCANNERS = ['semgrep', 'trivy', 'betterleaks']", "const SCANNERS = ['semgrep']")
	args := `{"scan":{"tools":[{"tool":"semgrep","ok":true,"invoked":true,"results":0,"dropped":3}],"candidates":[]}}`
	m := retOf(byMode(runSecurity(t, one, args, "clean"), "clean"))
	if m["complete"] != false || !strings.Contains(m["report"].(string), "3 hit(s) with no file location") {
		t.Fatalf("dropped hits are a named hole: %+v", m)
	}
}

// With MAX_VARIANTS 0 a run that confirmed findings did not run dry.
func TestSecurityNoVariantsIsNotDry(t *testing.T) {
	r := runSecurity(t, securitySkeleton(t), `{"maxVariants":0}`, "clean")[0]
	if retOf(r)["stoppedBy"] != "noVariants" {
		t.Fatalf("confirmed findings with variant search off must not read as dry: %v", retOf(r)["stoppedBy"])
	}
}

// A prover whose own `head` names another commit did not prove the exploit at
// COMMIT, whatever its onCommit says.
func TestSecurityHeadContradictionIsNotExploited(t *testing.T) {
	body := mutate(t, securitySkeleton(t), "const head = String(v.head || '').trim()", "const head = 'deadbeef0042'")
	for _, c := range retOf(byMode(runSecurity(t, body, DefaultArgs, "clean"), "clean"))["confirmed"].([]any) {
		if c.(map[string]any)["exploited"] == true {
			t.Fatalf("a head that is not COMMIT must not count as exploited: %+v", c)
		}
	}
}

// The review round's probes against `inconclusive`, each a one-line edit a
// real author could make that passed every mode.
func TestSecurityInconclusiveProbes(t *testing.T) {
	sk := securitySkeleton(t)
	// Undecided findings counted as confirmed.
	m12 := mutate(t, sk, "const confirmed = results.filter((r) => r.status === 'exploited' || r.status === 'confirmed')", "const confirmed = results.filter((r) => r.status === 'exploited' || r.status === 'confirmed' || r.status === 'inconclusive')")
	wantViolation(t, byMode(runSecurity(t, m12, DefaultArgs, inconclusiveMode), inconclusiveMode), "an undecided finding is never confirmed")
	// The report stops listing them for the human.
	m8 := mutate(t, sk, "inconclusive.forEach((f) => out.push(", ";[].forEach((f) => out.push(")
	wantViolation(t, byMode(runSecurity(t, m8, DefaultArgs, inconclusiveMode), inconclusiveMode), "the `report` never names")
	// A two-valued verifier beside a prover whose outcome has a third member.
	m7 := mutate(t, sk, "enum: ['true-positive', 'false-positive', 'cannot-verify']", "enum: ['true-positive', 'false-positive']")
	m7 = mutate(t, m7, "verified: { type: 'boolean', description: 'true only if your PoC", "outcome: { type: 'string', enum: ['exploited', 'failed', 'inconclusive'] },\n    verified: { type: 'boolean', description: 'true only if your PoC")
	wantViolation(t, byMode(runSecurity(t, m7, DefaultArgs, inconclusiveMode), inconclusiveMode), "no VERIFIER enum")
	// A third verdict spelled with another undecided word is accepted.
	for _, w := range []string{"uncertain", "undecidable", "unknown"} {
		body := mutate(t, sk, "'cannot-verify']", "'"+w+"']")
		if r := byMode(runSecurity(t, body, DefaultArgs, inconclusiveMode), inconclusiveMode); len(r.Violations) > 0 {
			t.Fatalf("%s is an undecided verdict: %v", w, r.Violations)
		}
	}
}

// A prover is known by its label OR its phase: `verify:prove:` and a call in
// phase Prove both need isolation; the control label is refused as before.
func TestSecurityProverByLabelOrPhase(t *testing.T) {
	sk := mutate(t, securitySkeleton(t), "effort: 'medium', isolation: 'worktree', schema: REPRO_SCHEMA", "effort: 'medium', schema: REPRO_SCHEMA")
	byLabel := strings.ReplaceAll(sk, "label: 'verify:repro:' + x.id, phase: 'Prove'", "label: 'verify:prove:' + x.id, phase: 'Verify'")
	wantViolation(t, byMode(runSecurity(t, byLabel, DefaultArgs, "clean"), "clean"), "has no `isolation: 'worktree'`")
	byPhase := strings.ReplaceAll(sk, "label: 'verify:repro:' + x.id, phase: 'Prove'", "label: 'verify:exploit:' + x.id, phase: 'Prove'")
	wantViolation(t, byMode(runSecurity(t, byPhase, DefaultArgs, "clean"), "clean"), "has no `isolation: 'worktree'`")
}

// A dry run handed an args.scan that names a failed scanner: the correct
// skeleton reports itself incomplete and the runner SAYS that is expected,
// rather than refusing it; the run with the guard deleted reads complete.
func TestSecurityFailedScannerInArgsIsANote(t *testing.T) {
	sk := mutate(t, scanSkeleton(t), "const SCANNERS = ['semgrep', 'trivy', 'betterleaks']", "const SCANNERS = ['semgrep', 'trivy']")
	args := `{"scan":{"tools":[{"tool":"semgrep","ok":true,"invoked":true,"results":0},{"tool":"trivy","ok":false,"results":0}],"candidates":[]}}`
	for _, mode := range []string{"clean", emptyMode, inconclusiveMode} {
		r := byMode(runSecurity(t, sk, args, mode), mode)
		if len(r.Violations) > 0 || retOf(r)["complete"] != false || !strings.Contains(strings.Join(r.Notes, " "), "names a failed scanner") {
			t.Fatalf("%s: a declared failed scanner is incomplete and noted, not refused: %v %v", mode, r.Violations, r.Notes)
		}
	}
	m2 := mutate(t, sk, "  else if (t.ok !== true) scanGaps.push(", "  else if (false) scanGaps.push(")
	if retOf(byMode(runSecurity(t, m2, args, "clean"), "clean"))["complete"] != true {
		t.Fatalf("the mutation must flip the verdict, or this test proves nothing")
	}
}

// Unit paths spelled `./x/`, `.` or ” are the same directories; hits with no
// file under the target are a named hole; a lens named after a harness cell
// is refused.
func TestSecurityUnitPathsAndReservedLenses(t *testing.T) {
	one := mutate(t, securitySkeleton(t), "const SCANNERS = []", "const SCANNERS = ['semgrep']")
	args := `{"scan":{"tools":[{"tool":"semgrep","ok":true,"invoked":true,"results":1}],"candidates":[{"id":"semgrep-1","tool":"semgrep","rule":"r","file":"internal/db/q.go","line":3,"message":"m"}]}}`
	for _, p := range []string{"'./internal/db/'", "'.'", "''", "'./'", "'internal/db'"} {
		body := mutate(t, one, "paths: ['TODO/']", "paths: ["+p+"]")
		r := byMode(runSecurity(t, body, args, "clean"), "clean")
		if !strings.Contains(strings.Join(r.Calls, " "), "find:r1:scan:") || strings.Contains(retOf(r)["report"].(string), "outside every unit") {
			t.Fatalf("unit path %s must own internal/db/q.go: %v", p, r.Calls)
		}
	}
	unres := `{"scan":{"tools":[{"tool":"semgrep","ok":true,"invoked":true,"results":1,"unresolved":1}],"candidates":[]}}`
	if m := retOf(byMode(runSecurity(t, one, unres, "clean"), "clean")); m["complete"] != false || !strings.Contains(m["report"].(string), "naming no file under the target") {
		t.Fatalf("unresolved hits are a named hole: %+v", m)
	}
	dup := `{"scan":{"tools":[{"tool":"semgrep","ok":true,"results":0},{"tool":"semgrep","ok":false,"results":0}],"candidates":[]}}`
	if m := retOf(byMode(runSecurity(t, one, dup, "clean"), "clean")); m["complete"] != false {
		t.Fatalf("a scanner listed twice is failed if any entry failed: %+v", m)
	}
	reserved := mutate(t, securitySkeleton(t), "{ id: 'injection',", "{ id: 'scan',")
	if r := byMode(runSecurity(t, reserved, DefaultArgs, "clean"), "clean"); r.Err == nil || !strings.Contains(r.Err.Error(), "harness's own cell kind") {
		t.Fatalf("a lens named scan is refused: %v", r.Err)
	}
}

// The wave-37 sweep's survivors, each with the case that kills it.
func TestSecuritySweepSurvivors(t *testing.T) {
	sk := securitySkeleton(t)
	// clean's exact shape: variant finders file new weaknesses (each stub CWE
	// differs), which are verified and seed the next round, up to MAX_ROUNDS.
	// Pinned, because a dedup that compared variant cells by CLASS rather than
	// CWE dropped them all and the run went dry after round 2.
	m := retOf(byMode(runSecurity(t, sk, DefaultArgs, "clean"), "clean"))
	if m["rounds"] != int64(3) || m["stoppedBy"] != "maxRounds" || len(m["confirmed"].([]any)) != 20 {
		t.Fatalf("clean: rounds %v, stoppedBy %v, %d confirmed", m["rounds"], m["stoppedBy"], len(m["confirmed"].([]any)))
	}
	// A cell that died runs again in the next round.
	if calls := strings.Join(byMode(runSecurity(t, sk, DefaultArgs, deadCellMode), deadCellMode).Calls, " "); !strings.Contains(calls, "find:r2:injection:") {
		t.Fatalf("a dead cell must be re-run: %s", calls)
	}
	// The PoC cap: one prover, the other serious finding named as capped.
	rc := runSecurity(t, sk, `{"maxRepro":1,"maxRounds":1}`, "clean")[0]
	if n := strings.Count(strings.Join(rc.Calls, " "), "verify:repro:"); n != 1 || len(retOf(rc)["capped"].([]any)) != 1 {
		t.Fatalf("maxRepro 1: %d prover(s), capped %v", n, retOf(rc)["capped"])
	}
	// A prover that did not confirm its commit has not exploited anything.
	off := mutate(t, sk, "onCommit: { type: 'boolean', $comment: 'x-pass: true'", "onCommit: { type: 'boolean', $comment: 'x-pass: false'")
	for _, c := range retOf(byMode(runSecurity(t, off, DefaultArgs, "clean"), "clean"))["confirmed"].([]any) {
		if c.(map[string]any)["exploited"] == true {
			t.Fatalf("onCommit false must not count as exploited: %+v", c)
		}
	}
	// Two findings of one class a line apart are one: narrowed to one file, a
	// finder's pair lands within WINDOW.
	one := mutate(t, sk, "examples: ['internal/api/users.go', 'cmd/app/main.go']", "examples: ['internal/api/users.go']")
	if rep := retOf(byMode(runSecurity(t, one, DefaultArgs, "clean"), "clean"))["report"].(string); strings.Contains(rep, "(0 within") {
		t.Fatalf("a pair one line apart in one file must dedup: %s", rep[:300])
	}
	// `inconclusive` holds a coverage verdict to true — a run that reads
	// undecided findings as incomplete is refused — and reads no other verdict.
	strict := mutate(t, sk, "unadjudicated.length === 0\n", "unadjudicated.length === 0 && inconclusive.length === 0\n")
	wantViolation(t, byMode(runSecurity(t, strict, DefaultArgs, inconclusiveMode), inconclusiveMode), "returned `complete: false` on the path where nothing failed")
	verdict := mutate(t, sk, "  complete,\n  report:", "  complete,\n  clean: inconclusive.length === 0,\n  report:")
	if r := byMode(runSecurity(t, verdict, DefaultArgs, inconclusiveMode), inconclusiveMode); len(r.Violations) > 0 {
		t.Fatalf("a verdict about the code honestly reads false over undecided findings: %v", r.Violations)
	}
	// An undeclared scanner's hit never reaches a finder.
	declared := mutate(t, mutate(t, sk, "const SCANNERS = []", "const SCANNERS = ['semgrep']"), "paths: ['TODO/']", "paths: ['internal/db/']")
	args := `{"scan":{"tools":[{"tool":"semgrep","ok":true,"invoked":true,"results":1},{"tool":"other","ok":true,"results":1}],"candidates":[{"id":"semgrep-1","tool":"semgrep","rule":"r","file":"internal/db/q.go","line":3,"message":"m"},{"id":"other-1","tool":"other","rule":"r","file":"internal/db/q.go","line":9,"message":"m"}]}}`
	for _, p := range byMode(runSecurity(t, declared, args, "clean"), "clean").Prompts {
		if strings.Contains(p.Text, "other-1") {
			t.Fatalf("an undeclared scanner's hit reached %s", p.Name)
		}
	}
}

// A full scan chunk's schema: every hit id required, and the width inside the
// band a launch is measured to accept — the leaf is a `$ref`, so twenty hits
// are twenty pointers. Read through the runner's own census.
func TestSecurityScanSchemaCensus(t *testing.T) {
	var c []string
	for i := 1; i <= 25; i++ {
		c = append(c, `{"id":"semgrep-`+strconv.Itoa(i)+`","tool":"semgrep","rule":"r","file":"internal/db/f.go","line":`+strconv.Itoa(i)+`,"message":"m"}`)
	}
	args := `{"scan":{"tools":[{"tool":"semgrep","ok":true,"invoked":true,"results":25}],"candidates":[` + strings.Join(c, ",") + `]}}`
	body := mutate(t, mutate(t, securitySkeleton(t), "const SCANNERS = []", "const SCANNERS = ['semgrep']"), "paths: ['TODO/']", "paths: ['internal/db/']")
	found := false
	for _, w := range SchemaWidths(runSecurity(t, body, args, "clean")) {
		if !strings.Contains(strings.Join(w.Labels, " "), "find:r1:scan:TODO-unit#1") {
			continue
		}
		found = true
		// coverage, gaps, findings, hits; ten per finding; twenty hit ids;
		// kind and why in each hit's leaf, the census counting the resolved
		// `$ref` once per use.
		if w.Required != 4+10+20+20*2 || w.Clean > 3974 {
			t.Fatalf("a full chunk: %d key(s) required, %d bytes", w.Required, w.Clean)
		}
	}
	if !found {
		t.Fatalf("no full scan chunk was composed")
	}
}

// Every prompt carries the secrets rule, the merge's included.
func TestSecurityEveryPromptCarriesSecrets(t *testing.T) {
	for _, p := range byMode(runSecurity(t, securitySkeleton(t), DefaultArgs, "clean"), "clean").Prompts {
		if !strings.Contains(p.Text, "SECRETS\nNever copy a secret value") {
			t.Fatalf("%s carries no SECRETS rule", p.Name)
		}
	}
}
