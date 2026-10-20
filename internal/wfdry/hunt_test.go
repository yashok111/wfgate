package wfdry

import (
	"strings"
	"testing"

	wfgate "github.com/yashok111/wfgate"
	"github.com/yashok111/wfgate/internal/ws"
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
	rs, err := Run(script(t, huntSkeleton(t)), []string{"clean"}, Options{Type: ws.TypeHunt, Args: `{"maxRepro":1,"maxRounds":1}`})
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
	// Typed as a review the same cell is refused for another reason — a reader
	// without `disallowedTools` — so the assertion is about THIS refusal.
	for _, v := range byMode(runTyped(t, body, ws.TypeReview, "clean"), "clean").Violations {
		if strings.Contains(v, "isolation") {
			t.Fatalf("the refusal is a hunt's alone: %v", v)
		}
	}
}

// `dead-repro` kills only the reproducers, so a hunt that drops a dead
// reproduction from its results — masked in `dead-reviews` by the readers'
// deaths — is refused.
func TestHuntDeadReproductionsReachTheVerdict(t *testing.T) {
	body := mutate(t, huntSkeleton(t), "    if (!v) {\n      const why", "    if (!v) {\n      if (x && x.repro) return\n      const why")
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

func huntRun(t *testing.T, mode string, o Options) Result {
	t.Helper()
	o.Type = ws.TypeHunt
	rs, err := Run(script(t, huntSkeleton(t)), []string{mode}, o)
	if err != nil {
		t.Fatal(err)
	}
	return rs[0]
}

// Every way the rounds stop is walked by a mode, an argument or a budget, and
// each is SAID: the report's second line names the stop, and the return
// carries it.
func TestHuntRoundsWalkEveryStop(t *testing.T) {
	bud := 290000
	for _, c := range []struct {
		name, mode string
		o          Options
		stop       string
		rounds     int64
	}{
		{"clean", "clean", Options{}, "maxRounds", 3},
		{"empty runs dry", emptyMode, Options{}, "dry", 1},
		{"a single pass", "clean", Options{Args: `{"maxRounds":1}`}, "maxRounds", 1},
		{"the agent ceiling", "clean", Options{Args: `{"maxAgents":12}`}, "agents", 1},
		{"the token budget", "clean", Options{Budget: &bud}, "tokens", 2},
	} {
		m := retOf(huntRun(t, c.mode, c.o))
		if m["stoppedBy"] != c.stop || m["rounds"] != c.rounds {
			t.Errorf("%s: stoppedBy=%v rounds=%v, want %s after %d", c.name, m["stoppedBy"], m["rounds"], c.stop, c.rounds)
		}
		if !strings.Contains(strings.Split(m["report"].(string), "\n")[1], "stopped by ") {
			t.Errorf("%s: the report's second line must name the stop", c.name)
		}
	}
}

// A budget stop is not a coverage hole: a cell the critic proposed for a round
// that never started is listed as unsearched, and the hunt stays complete.
func TestHuntUnrunExtraIsNotDead(t *testing.T) {
	m := retOf(huntRun(t, "clean", Options{Args: `{"maxAgents":12}`}))
	if m["complete"] != true {
		t.Fatalf("an extra cell that never ran must not make the hunt incomplete: %s", m["report"])
	}
	if !strings.Contains(m["report"].(string), "never searched, because the hunt stopped first: x2-1") {
		t.Fatalf("the unsearched extra cell must be named: %s", m["report"])
	}
}

// A ceiling too low for round 1 searched nothing, and that must not read as a
// clean hunt over zero cells.
func TestHuntCeilingBelowRoundOne(t *testing.T) {
	m := retOf(huntRun(t, "clean", Options{Args: `{"maxAgents":3}`}))
	if m["complete"] != false {
		t.Fatalf("a hunt that ran no round must be incomplete: %s", m["report"])
	}
	if d, _ := m["deadCells"].([]any); len(d) != 2 || !strings.Contains(d[0].(string), "never run") {
		t.Fatalf("both declared cells must be named as never run: %v", m["deadCells"])
	}
}

// A verifier past the ceiling is not spawned, and its finding is unverified
// with that reason — not dropped, and not reported as a dead agent.
func TestHuntVerifierPastTheCeiling(t *testing.T) {
	r := huntRun(t, "clean", Options{Args: `{"maxAgents":5}`})
	m := retOf(r)
	if u, _ := m["unverified"].([]any); len(u) != 1 {
		t.Fatalf("one finding past the ceiling must be unverified: %v", m["unverified"])
	}
	rep := m["report"].(string)
	if !strings.Contains(rep, "not spawned: the run reached MAX_AGENTS") || !strings.Contains(rep, "the merge was not spawned") {
		t.Fatalf("the report must say the verifier and the merge were not spawned, not that they died: %s", rep)
	}
	if r.Spawned != 5 {
		t.Fatalf("the ceiling must hold: %d spawned", r.Spawned)
	}
	// A critic spawned at the ceiling would be refused and read as dead.
	if d, _ := m["deadCritics"].([]any); len(d) != 0 || m["stoppedBy"] != "agents" {
		t.Fatalf("at the ceiling the rounds stop before the critic: deadCritics=%v stoppedBy=%v", m["deadCritics"], m["stoppedBy"])
	}
}

// Only a productive cell runs again: in `blockers` every verifier refutes, so
// round 2 runs the critic's extra cell and no declared one — and a declared
// cell that does run again is handed what it already reported.
func TestHuntOnlyProductiveCellsContinue(t *testing.T) {
	calls := strings.Join(huntRun(t, "blockers", Options{}).Calls, " ")
	if strings.Contains(calls, "find:r2:correctness") || !strings.Contains(calls, "find:r2:x2-1:extra") {
		t.Fatalf("round 2 must run the extra cell and no unproductive declared cell: %s", calls)
	}
	r := huntRun(t, "clean", Options{})
	p, err := FindPrompt(r.Prompts, "find:r2:correctness:TODO-unit")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Text, "do NOT report them again") || strings.Contains(r.Prompts[0].Text, "do NOT report them again") {
		t.Fatalf("only a re-run carries its earlier findings")
	}
}

// A dead critic costs the next round its extra cells, and the report says so.
func TestHuntDeadCriticIsReported(t *testing.T) {
	m := retOf(huntRun(t, "dead-reviews", Options{}))
	if !strings.Contains(m["report"].(string), "critic died after round(s) 1") {
		t.Fatalf("a dead critic must be reported: %s", m["report"])
	}
	if d, _ := m["deadCritics"].([]any); len(d) != 1 {
		t.Fatalf("deadCritics must name round 1: %v", m["deadCritics"])
	}
}

// A re-run that reports what its cell already reported files nothing new: the
// dedup key holds across rounds. And a critic's cell has no failure class of
// its own, so a location anyone already filed is not new from it either — in
// `clean` it reports the round-1 locations and round 2 files nothing.
func TestHuntRerunDoesNotRefile(t *testing.T) {
	rep := retOf(huntRun(t, "clean", Options{}))["report"].(string)
	if !strings.Contains(rep, "Round 2: 3 finder(s), 0 new") {
		t.Fatalf("round 2's re-run cells and the critic's cell must file nothing already filed: %s", strings.Split(rep, "\n")[1])
	}
}

// A cell that died runs again: in `dead-finders` the declared cells are
// retried in round 2, not dropped after their first death.
func TestHuntDeadCellRunsAgain(t *testing.T) {
	calls := strings.Join(huntRun(t, deadFindersMode, Options{}).Calls, " ")
	if !strings.Contains(calls, "find:r2:correctness:TODO-unit") {
		t.Fatalf("a dead cell must run again in the next round: %s", calls)
	}
}

// MAX_EXTRAS 0 cuts every area, so the critic is not asked and no extra cell
// runs.
func TestHuntExtrasAreCapped(t *testing.T) {
	r := huntRun(t, "clean", Options{Args: `{"maxExtras":0}`})
	if c := strings.Join(r.Calls, " "); strings.Contains(c, ":extra") || strings.Contains(c, "critic:") {
		t.Fatalf("maxExtras 0 must spawn no critic and run no extra cell: %v", r.Calls)
	}
}

// A re-run told to look past its earlier findings reads less than the whole
// scope; a gap stands only while no run of the cell read the file. Walked with
// the finders' gaps declared pass-shaped, so they are populated in `clean` —
// one string per round — and the re-run cells' round-1 and round-2 lists share
// nothing.
func TestHuntGapsIntersectAcrossRuns(t *testing.T) {
	body := mutate(t, huntSkeleton(t), "    gaps: { type: 'array', items: { type: 'string' },", "    gaps: { type: 'array', $comment: 'x-pass: true', items: { type: 'string' },")
	rs, err := Run(script(t, body), []string{"clean"}, Options{Type: ws.TypeHunt})
	if err != nil {
		t.Fatal(err)
	}
	rep, _ := retOf(rs[0])["report"].(string)
	line := ""
	for _, l := range strings.Split(rep, "\n") {
		if strings.HasPrefix(l, "Files a finder did not read") {
			line = l
		}
	}
	if line == "" || strings.Contains(line, "correctness x TODO-unit") || !strings.Contains(line, "x3-1 x extra") {
		t.Fatalf("a re-run cell's gaps must be the intersection of its runs, a single-run cell's its own: %q", line)
	}
}

// A re-run that dies is said, even though its cell reported in round 1.
func TestHuntDeadRerunIsReported(t *testing.T) {
	m := retOf(huntRun(t, "clean", Options{Args: `{"maxRounds":2}`, Dead: []string{"find:r2:correctness:TODO-unit", "find:r2:correctness:TODO-unit:retry"}}))
	if !strings.Contains(m["report"].(string), "Runs that died, of cells that reported in another round: correctness x TODO-unit (round 2)") {
		t.Fatalf("a dead re-run must be named: %s", m["report"])
	}
}

// No cell left after a dead critic is not "dry": nothing said no area was left.
func TestHuntDeadCriticIsNotDry(t *testing.T) {
	m := retOf(huntRun(t, "dead-reviews", Options{}))
	if m["stoppedBy"] != "critic" || !strings.Contains(m["report"].(string), "stopped by a dead critic") {
		t.Fatalf("a hunt whose critic died with no cell left must say so: %v / %s", m["stoppedBy"], m["report"])
	}
}

// The last round reserves no critic, so four agents run a one-round hunt of
// two cells.
func TestHuntLastRoundReservesNoCritic(t *testing.T) {
	r := huntRun(t, "clean", Options{Args: `{"maxAgents":4,"maxRounds":1}`})
	if retOf(r)["rounds"] != int64(1) {
		t.Fatalf("a one-round hunt of two cells fits in four agents: %s", retOf(r)["report"])
	}
}

// A ceiling below round 1 ran nothing, and the report says that rather than
// "nothing was found over the cells listed below" over no cells.
func TestHuntNoRoundWording(t *testing.T) {
	if rep := retOf(huntRun(t, "clean", Options{Args: `{"maxAgents":3}`}))["report"].(string); !strings.Contains(rep, "No round ran, so nothing was searched.") {
		t.Fatalf("a hunt that ran no round must say so: %s", rep)
	}
}

// An override cannot lift the ceiling past the Workflow cap.
func TestHuntCeilingIsClampedToTheCap(t *testing.T) {
	r := huntRun(t, "clean", Options{Args: `{"maxRounds":1000,"maxAgents":5000}`})
	if r.Spawned > 1000 {
		t.Fatalf("maxAgents 5000 must be clamped to the cap: %d spawned", r.Spawned)
	}
}

// remaining() stops at zero, so a round that overspent the budget reads as
// "at least" what it spent.
func TestHuntOverspentBudgetSaysAtLeast(t *testing.T) {
	bud := 100000
	m := retOf(huntRun(t, "clean", Options{Budget: &bud}))
	if m["stoppedBy"] != "tokens" || !strings.Contains(m["report"].(string), "the last round spent at least 100000") {
		t.Fatalf("an exhausted budget must read as a lower bound: %v / %s", m["stoppedBy"], m["report"])
	}
}
