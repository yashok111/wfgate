package wfdry

import (
	"strings"
	"testing"

	wfgate "git.sumka.site/yakov/wfgate"
	"git.sumka.site/yakov/wfgate/internal/ws"
)

func huntSkeleton(t *testing.T) string {
	t.Helper()
	b, err := wfgate.Templates.ReadFile("templates/hunt.mjs")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func retOf(r Result) map[string]any {
	m, _ := r.Return.(map[string]any)
	return m
}

func TestHuntSkeletonAllModes(t *testing.T) {
	for _, r := range runTyped(t, huntSkeleton(t), ws.TypeHunt) {
		if r.Err != nil || len(r.Violations) > 0 {
			t.Errorf("%s: err=%v violations=%v", r.Mode, r.Err, r.Violations)
		}
	}
}

// `clean` reproduces a blocker and reads a minor: the mixed finder pair sends
// one finding down each verify path, and the reproduced one is reported so.
func TestHuntCleanWalksBothVerifyPaths(t *testing.T) {
	r := byMode(runTyped(t, huntSkeleton(t), ws.TypeHunt, "clean"), "clean")
	calls := strings.Join(r.Calls, " ")
	if !strings.Contains(calls, "verify:repro:") || !strings.Contains(calls, "verify:read:") {
		t.Fatalf("clean must reproduce AND read: %s", calls)
	}
	// A reproducer WRITES, so it runs isolated; a reader does not.
	for _, c := range r.Calls {
		iso := strings.HasSuffix(c, "/worktree]")
		if strings.HasPrefix(c, "verify:repro:") != iso {
			t.Fatalf("only reproducers carry isolation: 'worktree': %s", c)
		}
	}
	repro := false
	for _, c := range retOf(r)["confirmed"].([]any) {
		if m := c.(map[string]any); m["reproduced"] == true && m["severity"] == "blocker" {
			repro = true
		}
	}
	if !repro {
		t.Fatalf("clean must report a REPRODUCED blocker: %+v", retOf(r)["confirmed"])
	}
}

// Not reproduced is not refuted: in `blockers` the reproducers fail and the
// readers refute, and the two land in different buckets.
func TestHuntNotReproducedIsNotRefuted(t *testing.T) {
	m := retOf(byMode(runTyped(t, huntSkeleton(t), ws.TypeHunt, "blockers"), "blockers"))
	if nr, _ := m["notReproduced"].([]any); len(nr) == 0 {
		t.Fatalf("a failed reproduction must be listed as not reproduced: %+v", m)
	}
	if n, _ := m["refuted"].(int64); n == 0 {
		t.Fatalf("a reader that refutes must still count as refuted: %+v", m)
	}
}

// Both verify paths die in `dead-reviews`, and a hunt whose `complete` ignores
// unverified findings is refused there.
func TestHuntDeadVerifiers(t *testing.T) {
	body := mutate(t, huntSkeleton(t), "const complete = deadCells.length === 0 && gapCells.length === 0 && unverified.length === 0", "const complete = deadCells.length === 0 && gapCells.length === 0")
	r := byMode(runTyped(t, body, ws.TypeHunt, "dead-reviews"), "dead-reviews")
	wantViolation(t, r, "dead-reviews: the run returned `complete: true`")
	k := strings.Join(r.Killed, " ")
	if !strings.Contains(k, "verify:repro:") || !strings.Contains(k, "verify:read:") {
		t.Fatalf("dead-reviews must kill reproducers and readers alike: %v", r.Killed)
	}
}

// The cap: serious findings past MAX_REPRO are READ, worst first, and the run
// names them. Walked through `args.maxRepro`, the way an author walks it.
func TestHuntReproCap(t *testing.T) {
	rs, err := Run(script(t, huntSkeleton(t)), []string{"clean"}, Options{Type: ws.TypeHunt, Args: `{"maxRepro":1}`})
	if err != nil {
		t.Fatal(err)
	}
	r := rs[0]
	if n := strings.Count(strings.Join(r.Calls, " "), "verify:repro:"); n != 1 {
		t.Fatalf("maxRepro 1 must spawn exactly one reproducer, got %d: %v", n, r.Calls)
	}
	if c, _ := retOf(r)["capped"].([]any); len(c) != 1 {
		t.Fatalf("the serious finding past the cap must be NAMED: %+v", retOf(r)["capped"])
	}
	if !strings.Contains(retOf(r)["report"].(string), "MAX_REPRO: F") {
		t.Fatalf("the report must name the capped ids")
	}
}

// A reproducer without `isolation: 'worktree'` is refused in a hunt, in every
// mode: it would write its failing test into the target.
func TestHuntReproducerMustBeIsolated(t *testing.T) {
	body := mutate(t, huntSkeleton(t), "effort: 'medium', isolation: 'worktree', schema: REPRO_SCHEMA", "effort: 'medium', schema: REPRO_SCHEMA")
	r := byMode(runTyped(t, body, ws.TypeHunt, "clean"), "clean")
	wantViolation(t, r, "has no `isolation: 'worktree'`")
	if rv := byMode(runTyped(t, body, ws.TypeReview, "clean"), "clean"); len(rv.Violations) > 0 {
		t.Fatalf("the refusal is a hunt's alone: %v", rv.Violations)
	}
}

// `dead-repro` kills only the reproducers, so a hunt that drops a dead
// reproduction from its results — masked in `dead-reviews` by the readers'
// deaths — is refused.
func TestHuntDeadReproductionsReachTheVerdict(t *testing.T) {
	body := mutate(t, huntSkeleton(t), "    if (!v) {\n      results.push", "    if (!v) {\n      if (x && x.repro) return\n      results.push")
	r := byMode(runTyped(t, body, ws.TypeHunt, deadReproMode), deadReproMode)
	wantViolation(t, r, "dead-repro: the run returned `complete: true`")
	for _, k := range r.Killed {
		if !strings.Contains(k, "repro") {
			t.Fatalf("dead-repro must kill reproducers only: %v", r.Killed)
		}
	}
	if !strings.Contains(strings.Join(ModesFor(ws.TypeHunt), " "), deadReproMode) || strings.Contains(strings.Join(ModesFor(ws.TypeReview), " "), deadReproMode) {
		t.Fatalf("dead-repro is a hunt's mode alone")
	}
}

// A reproducer labelled without a `verify` segment still dies in
// `dead-reviews`: `repro:F001` was killed by no mode.
func TestReproLabelDiesInDeadReviews(t *testing.T) {
	body := preamble + `phase('P')
const a = await spawn('one', { model: 'sonnet', effort: 'low', label: 'repro:F001', isolation: 'worktree', schema: S, phase: 'P' })
return { done: true, dead: a ? 0 : 1 }`
	r := byMode(runTyped(t, body, ws.TypeHunt, "dead-reviews"), "dead-reviews")
	if len(r.Killed) != 1 {
		t.Fatalf("a repro label must die in dead-reviews: %v", r.Killed)
	}
}
