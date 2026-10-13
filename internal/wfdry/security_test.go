package wfdry

import (
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
	wantViolation(t, r, "no enum offered a member reading as undecided")
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

// Every hit is accounted for once. With the failed scanners dropped, the one
// hit in the unit is dismissed by the fixture and the run is complete; with a
// second hit nobody accounts for, it is listed UNADJUDICATED and the run is not.
func TestSecurityEveryHitAdjudicated(t *testing.T) {
	ok := `{"scan":{"tools":[{"tool":"semgrep","ok":true,"results":1}],"candidates":[{"id":"semgrep-1","tool":"semgrep","rule":"r","file":"internal/db/q.go","line":12,"message":"m"}]}}`
	one := mutate(t, scanSkeleton(t), "const SCANNERS = ['semgrep', 'trivy', 'betterleaks']", "const SCANNERS = ['semgrep']")
	m := retOf(byMode(runSecurity(t, one, ok, "clean"), "clean"))
	if m["complete"] != true || len(m["unadjudicated"].([]any)) != 0 {
		t.Fatalf("one hit, dismissed: complete %+v", m)
	}
	two := `{"scan":{"tools":[{"tool":"semgrep","ok":true,"results":2}],"candidates":[{"id":"semgrep-1","tool":"semgrep","rule":"r","file":"internal/db/q.go","line":12,"message":"m"},{"id":"semgrep-2","tool":"semgrep","rule":"r","file":"internal/db/q.go","line":40,"message":"m"}]}}`
	m = retOf(byMode(runSecurity(t, one, two, "clean"), "clean"))
	if u, _ := m["unadjudicated"].([]any); len(u) != 1 || m["complete"] != false {
		t.Fatalf("a hit nobody accounted for is listed and the run is incomplete: %+v", m)
	}
	if !strings.Contains(m["report"].(string), "UNADJUDICATED scanner hit semgrep-2") {
		t.Fatalf("the report must name the unadjudicated hit")
	}
	// A script that does not count unadjudicated hits against `complete` is
	// held only by this test: no fixture walks the gap on its own.
	body := mutate(t, one, " && unadjudicated.length === 0", "")
	if m := retOf(byMode(runSecurity(t, body, two, "clean"), "clean")); m["complete"] != true {
		t.Fatalf("the mutation must flip the verdict, or this test proves nothing")
	}
}
