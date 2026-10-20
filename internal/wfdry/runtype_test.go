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
	body := mutate(t, reviewSkeleton(t), "const complete = deadCells.length === 0 && gapCells.length === 0 && unverified.length === 0", "const complete = gapCells.length === 0 && unverified.length === 0")
	r := byMode(runTyped(t, body, ws.TypeReview, "dead-finders"), "dead-finders")
	wantViolation(t, r, "dead-finders: the run returned `complete: true`")
	if !strings.Contains(strings.Join(r.Killed, " "), "find:") {
		t.Fatalf("the finders must be among the killed: %v", r.Killed)
	}
}

// `empty` is a passing mode: a review whose verdict reads failure when it found
// nothing cannot tell "the code is fine" from "nobody looked".
func TestReviewEmptyIsAPassingVerdict(t *testing.T) {
	body := mutate(t, reviewSkeleton(t), "const complete = deadCells.length === 0", "const complete = found.length > 0 && deadCells.length === 0")
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
		mk("dead-finders", 8, "Find"),
		mk("blockers", 30, "Find", "Verify"),
	}
	noteCollapse(rs, true)
	for _, r := range rs[1:] {
		if len(r.Violations) > 0 {
			t.Errorf("%s: a read-only run's injected narrowing must not fail: %v", r.Mode, r.Violations)
		}
	}
	if len(rs[2].Notes) == 0 || len(rs[3].Notes) == 0 {
		t.Errorf("the shortfall must still be said: dead-finders %v, blockers %v", rs[2].Notes, rs[3].Notes)
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
const r = await agent('go', { model: 'sonnet', effort: 'low', label: 'w', disallowedTools: ['Write', 'Edit'], schema: S, phase: 'P' })
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

// The verifiers die in `dead-reviews` and the finders live. Before the split
// one mode killed both, the finders first, so no verifier ever spawned to die
// and a `complete` that ignored unverified findings passed every mode.
func TestReviewDeadVerifiersReachTheVerdict(t *testing.T) {
	body := mutate(t, reviewSkeleton(t), "const complete = deadCells.length === 0 && gapCells.length === 0 && unverified.length === 0", "const complete = deadCells.length === 0 && gapCells.length === 0")
	r := byMode(runTyped(t, body, ws.TypeReview, "dead-reviews"), "dead-reviews")
	wantViolation(t, r, "dead-reviews: the run returned `complete: true`")
	k := strings.Join(r.Killed, " ")
	if !strings.Contains(k, "verify:") || strings.Contains(k, "find:") {
		t.Fatalf("dead-reviews must kill the verifiers and spare the finders: %v", r.Killed)
	}
}

// A confirmed blocker exists in `clean`: the Nth verifier of a family takes the
// opposite severity, as item 1 of an array does.
func TestReviewCleanConfirmsABlocker(t *testing.T) {
	r := byMode(runTyped(t, reviewSkeleton(t), ws.TypeReview, "clean"), "clean")
	m, _ := r.Return.(map[string]any)
	sev := map[string]bool{}
	for _, c := range m["confirmed"].([]any) {
		sev[c.(map[string]any)["severity"].(string)] = true
	}
	if !sev["blocker"] || len(sev) < 2 {
		t.Fatalf("clean must confirm a MIX that includes a blocker, got %v", sev)
	}
}

// `empty` that emptied nothing walked the clean path under another name.
func TestEmptyThatEmptiedNothing(t *testing.T) {
	body := strings.ReplaceAll(reviewSkeleton(t), "findings", "results")
	r := byMode(runTyped(t, body, ws.TypeReview, "empty"), "empty")
	wantViolation(t, r, "emptied nothing")
	if r := byMode(runTyped(t, reviewSkeleton(t), ws.TypeReview, "empty"), "empty"); len(r.Violations) > 0 {
		t.Fatalf("the skeleton's own empty must pass: %v", r.Violations)
	}
}

// The deliverable vocabulary is the whole NAME: a failure-qualified finding
// list keeps the build polarity.
func TestDeliverableIsAnchored(t *testing.T) {
	for _, n := range []string{"findings", "issues", "defect", "problems"} {
		if !deliverableRe.MatchString(n) {
			t.Errorf("%s must be a deliverable", n)
		}
	}
	for _, n := range []string{"unverifiedfindings", "openissues", "results"} {
		if deliverableRe.MatchString(n) {
			t.Errorf("%s must not be a deliverable", n)
		}
	}
}

// A review's verdict ABOUT THE CODE reads false in `clean`, where the fixture
// is "found defects" — the read-only clean guard reads coverage words only,
// and `empty` still holds every pass word.
func TestReviewCleanGuardReadsCoverageOnly(t *testing.T) {
	body := mutate(t, reviewSkeleton(t), "  complete,\n", "  complete,\n  clean: confirmed.length === 0 && complete,\n")
	rs := runTyped(t, body, ws.TypeReview, "clean", "empty")
	if v := byMode(rs, "clean").Violations; len(v) > 0 {
		t.Fatalf("a verdict about the code must not fail clean: %v", v)
	}
	if v := byMode(rs, "empty").Violations; len(v) > 0 {
		t.Fatalf("empty: nothing found, so `clean` holds: %v", v)
	}
	bad := mutate(t, reviewSkeleton(t), "  complete,\n", "  complete,\n  clean: false,\n")
	wantViolation(t, byMode(runTyped(t, bad, ws.TypeReview, "empty"), "empty"), "empty: the run returned `clean: false`")
}

// A review whose finders the runner cannot identify has a dead-finder path no
// mode walks, and that is a violation in a review, not a note.
func TestDeadFindersThatKilledNobody(t *testing.T) {
	// The second lens's placeholder id carries the segment `lens`, which IS a
	// finder word; renamed too, or the mode kills it and the case proves nothing.
	body := strings.ReplaceAll(strings.ReplaceAll(reviewSkeleton(t), "'find:'", "'look:'"), "TODO-second-lens", "TODO-second")
	if body == reviewSkeleton(t) {
		t.Fatal("relabel did not land")
	}
	wantViolation(t, byMode(runTyped(t, body, ws.TypeReview, "dead-finders"), "dead-finders"), "killed nobody")
}

// A review's own reviewers — a critic, a skeptic — die in `dead-reviews`, and
// only in a review: a build keeps the closed list it had.
func TestCriticsDieInAReviewsDeadReviews(t *testing.T) {
	body := preamble + `phase('P')
const a = await spawn('one', { model: 'sonnet', effort: 'low', label: 'critic:r1', disallowedTools: ['Write', 'Edit'], schema: S, phase: 'P' })
return { done: true, dead: a ? 0 : 1 }`
	r := byMode(runTyped(t, body, ws.TypeReview, "dead-reviews"), "dead-reviews")
	if len(r.Killed) != 1 {
		t.Fatalf("a review's critic must die in dead-reviews: %v", r.Killed)
	}
	if b := byMode(runTyped(t, body, ws.TypeBuild, "dead-reviews"), "dead-reviews"); len(b.Killed) != 0 {
		t.Fatalf("a build's dead-reviews must not grow new segments: %v", b.Killed)
	}
}

// The top-level alternation reaches a SEVERITY only: a gate-style verdict on
// the second agent of a family stays a pass in the mode where nothing failed.
func TestAlternationIsSeverityOnly(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { verdict: { type: 'string', enum: ['pass', 'blockers'] }, severity: { type: 'string', enum: ['blocker', 'minor'] } } }
phase('P')
const a = await agent('a', { model: 'sonnet', effort: 'low', label: 'critic:1', disallowedTools: ['Write', 'Edit'], schema: S, phase: 'P' })
const b = await agent('b', { model: 'sonnet', effort: 'low', label: 'critic:2', disallowedTools: ['Write', 'Edit'], schema: S, phase: 'P' })
return { done: true, v: [a.verdict, b.verdict].join(','), s: [a.severity, b.severity].join(',') }`
	r := byMode(runTyped(t, body, ws.TypeReview, "clean"), "clean")
	m, _ := r.Return.(map[string]any)
	if m["v"] != "pass,pass" {
		t.Errorf("a verdict must not alternate: %v", m["v"])
	}
	if m["s"] != "minor,blocker" {
		t.Errorf("a severity must alternate across the family: %v", m["s"])
	}
}

// A deliverable under another name declares `x-pass: true`, and `empty` then
// counts it.
func TestEmptyCountsAnXPassDeliverable(t *testing.T) {
	body := mutate(t, strings.ReplaceAll(reviewSkeleton(t), "findings", "results"),
		"    results: {\n      type: 'array',", "    results: {\n      type: 'array',\n      $comment: 'x-pass: true',")
	if v := byMode(runTyped(t, body, ws.TypeReview, "empty"), "empty").Violations; len(v) > 0 {
		t.Fatalf("an x-pass deliverable is a deliverable: %v", v)
	}
}

// `dead-cell` kills ONE finder cell in every round and leaves the rest alive,
// so a verdict that counts "some cell reported" as coverage is refused — a
// partial death no other mode walks.
func TestDeadCellWalksAPartialDeath(t *testing.T) {
	for _, typ := range []string{ws.TypeReview, ws.TypeHunt, ws.TypeAudit} {
		if !strings.Contains(strings.Join(ModesFor(typ), " "), deadCellMode) {
			t.Errorf("%s must enter dead-cell", typ)
		}
	}
	if strings.Contains(strings.Join(ModesFor(ws.TypeBuild), " "), deadCellMode) {
		t.Fatalf("dead-cell is a read-only mode")
	}
	r := byMode(runTyped(t, huntSkeleton(t), ws.TypeHunt, deadCellMode), deadCellMode)
	ids := map[string]bool{}
	for _, k := range r.Killed {
		ids[cellIdentity(k)] = true
	}
	if len(ids) != 1 || len(r.Killed) < 4 {
		t.Fatalf("dead-cell must kill one cell across its rounds and retries: %v", r.Killed)
	}
	body := mutate(t, huntSkeleton(t), "const complete = deadCells.length === 0 && gapCells.length === 0 && unverified.length === 0", "const complete = cells.some((c) => c.reported) && gapCells.length === 0 && unverified.length === 0")
	wantViolation(t, byMode(runTyped(t, body, ws.TypeHunt, deadCellMode), deadCellMode), "dead-cell: the run returned `complete: true`")
}

// A `dead-cell` that kills nobody walked no partial death, and says so.
func TestDeadCellKillingNobodyFails(t *testing.T) {
	body := preamble + `phase('P')
const a = await spawn('one', { model: 'sonnet', effort: 'low', label: 'gather:all', schema: S, phase: 'P' })
return { complete: !!a }`
	wantViolation(t, byMode(runTyped(t, body, ws.TypeReview, deadCellMode), deadCellMode), "dead-cell: no label matched")
}
