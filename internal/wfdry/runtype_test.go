package wfdry

import (
	"strings"
	"testing"

	wfgate "git.sumka.site/yakov/wfgate"
	"git.sumka.site/yakov/wfgate/internal/ws"
)

// reviewSkeleton is the emitted review template, so every case below runs
// against what `wf new-script -type review` actually hands an author.
func reviewSkeleton(t *testing.T) string {
	t.Helper()
	b, err := wfgate.Templates.ReadFile("templates/review.mjs")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// mutate replaces old with new exactly once and fails when it did not land —
// a replacement that matched nothing is a mutation that never happened, and
// the case would pass over the unmutated script.
func mutate(t *testing.T, src, old, new string) string {
	t.Helper()
	if n := strings.Count(src, old); n != 1 {
		t.Fatalf("mutation anchor %q occurs %d time(s), want 1", old, n)
	}
	return strings.Replace(src, old, new, 1)
}

func runTyped(t *testing.T, body, typ string, modes ...string) []Result {
	t.Helper()
	if len(modes) == 0 {
		modes = ModesFor(typ)
	}
	rs, err := Run(script(t, body), modes, Options{Type: typ})
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func byMode(rs []Result, mode string) Result {
	for _, r := range rs {
		if r.Mode == mode {
			return r
		}
	}
	return Result{}
}

func TestModesFor(t *testing.T) {
	ro := strings.Join(ModesFor(ws.TypeReview), " ")
	if !strings.Contains(ro, "empty") || strings.Contains(ro, "acceptfail") || strings.Contains(ro, "unfixed") {
		t.Fatalf("a review walks `empty` and has no acceptance or fix step to fail: %s", ro)
	}
	if strings.Join(ModesFor(""), " ") != strings.Join(Modes, " ") {
		t.Fatalf("the empty type is build and must keep the build modes")
	}
	if _, err := Run(script(t, preamble), []string{"clean"}, Options{Type: "bogus"}); err == nil {
		t.Fatalf("an unknown type must be refused, not run under the build rules")
	}
	if _, err := Run(script(t, preamble), []string{"acceptfail"}, Options{Type: ws.TypeReview}); err == nil {
		t.Fatalf("a build-only mode must be refused for a review run")
	}
}

// The emitted skeleton is correct, and every mode of a review run says so.
func TestReviewSkeletonAllModes(t *testing.T) {
	for _, r := range runTyped(t, reviewSkeleton(t), ws.TypeReview) {
		if r.Err != nil || len(r.Violations) > 0 {
			t.Errorf("%s: err=%v violations=%v", r.Mode, r.Err, r.Violations)
		}
	}
}

// The deliverable polarity: under the build reading the finders came back
// empty in `clean` and Verify/Merge ran in no passing mode.
func TestReviewCleanWalksTheHappyPath(t *testing.T) {
	r := byMode(runTyped(t, reviewSkeleton(t), ws.TypeReview, "clean"), "clean")
	got := strings.Join(r.Phases, " ")
	if got != "Find Verify Merge" {
		t.Fatalf("clean must reach find → verify → merge, reached %q", got)
	}
	b := byMode(runTyped(t, reviewSkeleton(t), ws.TypeBuild, "clean"), "clean")
	if strings.Contains(strings.Join(b.Phases, " "), "Verify") {
		t.Fatalf("the build reading is the control: findings are a defect list there and clean finds none — got %v", b.Phases)
	}
}

// In `blockers` the verifiers REFUTE. Under the build rule a pass-shaped
// boolean was true everywhere but `acceptfail`, a mode a review does not have,
// so the refute path ran in no mode.
func TestReviewBlockersRefutes(t *testing.T) {
	r := byMode(runTyped(t, reviewSkeleton(t), ws.TypeReview, "blockers"), "blockers")
	m, _ := r.Return.(map[string]any)
	if n, _ := m["refuted"].(int64); n == 0 {
		t.Fatalf("blockers must walk the refute path: %+v", r.Return)
	}
	if c, _ := m["confirmed"].([]any); len(c) != 0 {
		t.Fatalf("every verifier refutes in blockers, yet %d finding(s) were confirmed", len(c))
	}
}

// A review that drops its dead FINDERS from `complete` reviewed less than it
// says. Without the finder segments `dead-reviews` killed only the verifiers,
// whose deaths the mutant still counts, and the mutant passed.
func TestReviewDeadFindersReachTheVerdict(t *testing.T) {
	body := mutate(t, reviewSkeleton(t), "complete: deadCells.length === 0 && unverified.length === 0,", "complete: unverified.length === 0,")
	r := byMode(runTyped(t, body, ws.TypeReview, "dead-reviews"), "dead-reviews")
	wantViolation(t, r, "dead-reviews: the run returned `complete: true`")
	if !strings.Contains(strings.Join(r.Killed, " "), "find:") {
		t.Fatalf("the finders must be among the killed: %v", r.Killed)
	}
}

// `empty` is a passing mode: a review whose verdict reads failure when it found
// nothing cannot tell "the code is fine" from "nobody looked".
func TestReviewEmptyIsAPassingVerdict(t *testing.T) {
	body := mutate(t, reviewSkeleton(t), "complete: deadCells.length === 0 && unverified.length === 0,", "complete: found.length > 0 && deadCells.length === 0 && unverified.length === 0,")
	r := byMode(runTyped(t, body, ws.TypeReview, "empty"), "empty")
	wantViolation(t, r, "empty: the run returned `complete: false`")
	if len(r.Phases) != 1 {
		t.Fatalf("empty must stop at Find, reached %v", r.Phases)
	}
}

// The runner's own narrowing in a read-only run is SAID, not failed: `empty`
// spawns no verifier, `dead-reviews` kills the finders the verifiers feed on,
// and `blockers` confirms nothing for a merge to consume.
func TestReviewNarrowingIsANote(t *testing.T) {
	mk := func(mode string, n int, phases ...string) Result {
		return Result{Mode: mode, Spawned: n, Phases: phases}
	}
	rs := []Result{
		mk("clean", 40, "Find", "Verify", "Merge"),
		mk("empty", 4, "Find"),
		mk("dead-reviews", 8, "Find"),
		mk("blockers", 30, "Find", "Verify"),
	}
	noteCollapse(rs, true)
	for _, r := range rs[1:] {
		if len(r.Violations) > 0 {
			t.Errorf("%s: a read-only run's injected narrowing must not fail: %v", r.Mode, r.Violations)
		}
	}
	if len(rs[2].Notes) == 0 || len(rs[3].Notes) == 0 {
		t.Errorf("the shortfall must still be said: dead-reviews %v, blockers %v", rs[2].Notes, rs[3].Notes)
	}
	// The control: the same numbers in a build ARE the collapse it exists for.
	build := []Result{mk("clean", 40, "Find", "Verify", "Merge"), mk("dead-reviews", 8, "Find"), mk("blockers", 30, "Find", "Verify")}
	noteCollapse(build, false)
	if len(build[1].Violations) == 0 || len(build[2].Violations) == 0 {
		t.Errorf("a build's narrowing stays a violation: dead-reviews %v, blockers %v", build[1].Violations, build[2].Violations)
	}
}

// Item 1 of an array differs from item 0 in its scalars, or a dedup keyed on
// them collapses the mixed pair into one.
func TestItemScalarsDiffer(t *testing.T) {
	str := map[string]any{"type": "string"}
	num := map[string]any{"type": "integer"}
	if value("file", str, "clean", "f", true, 0, 0, nil) == value("file", str, "clean", "f", true, 0, 1, nil) {
		t.Fatalf("two items' strings must differ")
	}
	if value("line", num, "clean", "f", true, 0, 0, nil) == value("line", num, "clean", "f", true, 0, 1, nil) {
		t.Fatalf("two items' integers must differ")
	}
}

// A categorical enum is not a boolean, and was reported as one.
func TestEnumFallbackHasItsOwnNote(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { category: { type: 'string', enum: ['money', 'sql'] } } }
phase('P')
await agent('go', { model: 'sonnet', effort: 'low', label: 'w', schema: S, phase: 'P' })
return { done: true }`
	r := run1(t, body, "clean")
	notes := strings.Join(r.Notes, "\n")
	if !strings.Contains(notes, "enum field(s) none of whose members") || !strings.Contains(notes, "category (enum money|sql)") {
		t.Fatalf("the enum note is missing: %v", r.Notes)
	}
	if strings.Contains(notes, "were FALSE in this mode") {
		t.Fatalf("an enum must not be reported under the boolean note: %v", r.Notes)
	}
}

// `empty` is a PASSING fixture beyond the findings: nothing failed, so a
// failure-shaped array is empty, a pass flag is true, and a pass-shaped array
// — a confirmed list — is empty too, because nothing was found to confirm.
func TestEmptyModeFixture(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: {
  findings: { type: 'array', items: { type: 'string' } },
  gaps: { type: 'array', items: { type: 'string' } },
  confirmed: { type: 'array', items: { type: 'string' }, $comment: 'x-pass: true' },
  ok: { type: 'boolean' } } }
phase('P')
const r = await agent('go', { model: 'sonnet', effort: 'low', label: 'w', schema: S, phase: 'P' })
return { done: true, findings: r.findings.length, gaps: r.gaps.length, confirmed: r.confirmed.length, ok: r.ok }`
	r := byMode(runTyped(t, body, ws.TypeReview, "empty"), "empty")
	m, _ := r.Return.(map[string]any)
	for _, k := range []string{"findings", "gaps", "confirmed"} {
		if n, _ := m[k].(int64); n != 0 {
			t.Errorf("empty: %s must be empty, got %v", k, m[k])
		}
	}
	if ok, _ := m["ok"].(bool); !ok {
		t.Errorf("empty: a pass flag must be true — nothing failed: %+v", m)
	}
}
