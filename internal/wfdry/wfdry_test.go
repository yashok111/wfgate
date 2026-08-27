// One case per guard. The predecessor had nine guards and one fixture: eight of
// them could be deleted with the bar still green, including the single `if
// (violations.length)` that turned a recorded defect into a failing mode — cut
// that, and every check in the file became decorative while `make check-tools`
// stayed green. A guard with no failing test is not a guard.
package wfdry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.sumka.site/yakov/wfgate/internal/check"
)

func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.mjs")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const preamble = `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { blockers: { type: 'array' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
`

// run executes one mode and returns it.
func run1(t *testing.T, body, mode string) Result {
	t.Helper()
	rs, err := Run(script(t, body), []string{mode}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return rs[0]
}

func wantViolation(t *testing.T, r Result, substr string) {
	t.Helper()
	for _, v := range r.Violations {
		if strings.Contains(v, substr) {
			return
		}
	}
	t.Fatalf("want a violation containing %q, got violations=%v err=%v", substr, r.Violations, r.Err)
}

func TestGuards(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"no model", preamble + `phase('P')
await spawn(` + "`go`" + `, { effort: 'low', label: 'w', schema: S })
return { done: true }`, "no model"},

		{"no effort", preamble + `phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', label: 'w', schema: S })
return { done: true }`, "no effort"},

		{"effort outside the enum", preamble + `phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'turbo', label: 'w', schema: S })
return { done: true }`, `effort "turbo" is not one of`},

		{"no schema", preamble + `phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w' })
return { done: true }`, "no schema"},

		{"empty prompt", preamble + `phase('P')
await spawn('   ', { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true }`, "empty prompt"},

		{"prompt composed to undefined on one branch", preamble + `phase('P')
let p
await spawn(p, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true }`, "prompt is undefined"},

		{"a run that spawns nothing", preamble + `phase('P')
return { done: true }`, "without spawning a single agent"},

		{"a return value nothing can read", preamble + `phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return 'done'`, "not an object"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := run1(t, c.body, "clean")
			wantViolation(t, r, c.want)
			if r.OK() {
				t.Fatal("a mode carrying a violation must not report OK")
			}
		})
	}
}

// The banked P2e regression. The real harness parses `export const meta` out
// before the body runs, so a body touching it dies at launch — in 29 ms, with
// zero agents. A runner whose stub leaves `meta` bound runs it green, and that
// false green shipped the only production defect of that gate.
func TestMetaIsStrippedNotRenamed(t *testing.T) {
	body := preamble + `phase('P')
if (meta.phases.length !== 1) throw new Error('unreachable')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true }`
	rs, err := Run(script(t, body), Modes, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if r.OK() {
			t.Fatalf("mode %s ran green over a body that touches meta", r.Mode)
		}
	}
}

// An invented mode used to EXECUTE and then report "1/1 modes completed".
func TestUnknownModeIsRefused(t *testing.T) {
	_, err := Run(script(t, preamble+"return { done: true }"), []string{"clen"}, Options{})
	if err == nil || !strings.Contains(err.Error(), "unknown mode") {
		t.Fatalf("want an unknown-mode refusal, got %v", err)
	}
}

// A parameterised workflow gated on `args` returned {skipped:true} in all six
// modes of the predecessor, which passed undefined every time: six green modes
// over a body that never executed. Both states are real, so both are modes.
func TestArgsGatedBodyRunsAndIsAlsoExercisedWithout(t *testing.T) {
	body := preamble + `if (!args) return { skipped: true }
phase('P')
await spawn(` + "`go " + "${args.topic}" + `` + "`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true }`
	rs, err := Run(script(t, body), Modes, Options{})
	if err != nil {
		t.Fatal(err)
	}
	byMode := map[string]Result{}
	for _, r := range rs {
		byMode[r.Mode] = r
	}
	if byMode["clean"].Spawned != 1 {
		t.Fatalf("with args present the body must execute, spawned=%d", byMode["clean"].Spawned)
	}
	if !byMode["no-args"].OK() {
		t.Fatalf("returning early without args is correct behaviour: %+v", byMode["no-args"])
	}
	if byMode["no-args"].Spawned != 0 {
		t.Fatal("the no-args mode must actually withhold args")
	}
}

// ...but a script that spawns nothing in EVERY mode is the false green the rule
// exists for, and the no-args exemption must not cover it.
func TestZeroSpawnEverywhereIsNotForgiven(t *testing.T) {
	body := preamble + `const DEEP = false
phase('P')
if (DEEP) { await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S }) }
return { done: true }`
	rs, err := Run(script(t, body), Modes, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if r.OK() {
			t.Fatalf("mode %s reported OK over a run that spawned nothing", r.Mode)
		}
	}
}

// The wrapper swallows a throw, so a stub that signalled only by throwing would
// be signalling into the one mechanism guaranteed to hide it. The violation
// list is what makes the mode fail — and it must survive the swallow.
func TestAViolationSurvivesTheWrappersCatch(t *testing.T) {
	body := preamble + `phase('P')
const r = await spawn(` + "`go`" + `, { model: 'sonnet', label: 'w', schema: S })
return { done: true, dead: r ? [] : ['w'] }`
	r := run1(t, body, "clean")
	if len(r.Violations) == 0 {
		t.Fatal("the wrapper caught the throw; the violation must still be recorded")
	}
	if r.OK() {
		t.Fatal("a swallowed defect must still fail the mode")
	}
}

// The stub derives each report from the schema the call hands it, and the
// well-known field names carry a mode-dependent value: a fail array is empty in
// `clean` and holds one element elsewhere, so the script's failure branches are
// actually walked.
func TestSynthesisedReportFollowsTheSchemaAndTheMode(t *testing.T) {
	body := preamble + `phase('P')
const r = await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true, blockers: r.blockers.length }`
	clean := run1(t, body, "clean")
	blocked := run1(t, body, "blockers")
	get := func(r Result) float64 {
		m, ok := r.Return.(map[string]any)
		if !ok {
			t.Fatalf("return is %T: %+v", r.Return, r)
		}
		n, _ := m["blockers"].(int64)
		return float64(n)
	}
	if get(clean) != 0 {
		t.Fatalf("clean must yield an empty fail array, got %v", get(clean))
	}
	// TWO, not one, and the pair is mixed. An array whose every element is
	// identical exercises only one side of every
	// `filter(f => f.severity === 'blocker')` in the script under test:
	// measured on the emitted skeleton, the non-blocking bucket was empty in
	// every failing mode, so the line collecting it was executed by NO mode and
	// its deletion passed every bar. A real reviewer returns a mix.
	if get(blocked) != 2 {
		t.Fatalf("a failing mode must yield a MIXED pair, got %v", get(blocked))
	}
}

// The mode machinery itself. Each of these was deletable with the bars green:
// a mode that kills nobody, a throw that never happens, a promise that never
// settles — every one of them turns its mode into a green report over an
// exercise that did not occur.
func TestModeMachinery(t *testing.T) {
	body := preamble + `phase('P')
const a = await spawn(` + "`one`" + `, { model: 'sonnet', effort: 'low', label: 'reviewer', schema: S, phase: 'P' })
const b = await spawn(` + "`two`" + `, { model: 'sonnet', effort: 'low', label: 'worker', schema: S, phase: 'P' })
return { done: true, dead: [a, b].filter((x) => !x).length }`

	t.Run("dead-reviews kills the review labels", func(t *testing.T) {
		r := run1(t, body, "dead-reviews")
		m, _ := r.Return.(map[string]any)
		if n, _ := m["dead"].(int64); n != 1 {
			t.Fatalf("exactly the reviewer must come back null, got %v (%+v)", n, r.Return)
		}
	})

	t.Run("dead-reviews that matches nothing says so", func(t *testing.T) {
		none := preamble + `phase('P')
await spawn(` + "`one`" + `, { model: 'sonnet', effort: 'low', label: 'auditor', schema: S, phase: 'P' })
return { done: true }`
		r := run1(t, none, "dead-reviews")
		if !strings.Contains(strings.Join(r.Notes, "\n"), "killed nobody") {
			t.Fatalf("a mode that stubbed no death must say so: %+v", r.Notes)
		}
	})

	t.Run("throw-serial throws at the first spawn", func(t *testing.T) {
		r := run1(t, body, "throw-serial")
		m, _ := r.Return.(map[string]any)
		if n, _ := m["dead"].(int64); n != 1 {
			t.Fatalf("the first spawn must come back null, got %v", n)
		}
	})

	t.Run("--dead kills the named label", func(t *testing.T) {
		rs, err := Run(script(t, body), []string{"clean"}, Options{Dead: []string{"worker"}})
		if err != nil {
			t.Fatal(err)
		}
		m, _ := rs[0].Return.(map[string]any)
		if n, _ := m["dead"].(int64); n != 1 {
			t.Fatalf("the named label must come back null, got %v", n)
		}
	})

	t.Run("a promise that never settles is caught", func(t *testing.T) {
		r := run1(t, preamble+`phase('P')
await spawn(`+"`go`"+`, { model: 'sonnet', effort: 'low', label: 'w', schema: S, phase: 'P' })
await new Promise(() => {})
return { done: true }`, "clean")
		if r.Err == nil || !strings.Contains(r.Err.Error(), "never settled") {
			t.Fatalf("want the never-settled verdict, got %v", r.Err)
		}
	})

	t.Run("an unbounded loop hits the deadline", func(t *testing.T) {
		rs, err := Run(script(t, preamble+`phase('P')
await spawn(`+"`go`"+`, { model: 'sonnet', effort: 'low', label: 'w', schema: S, phase: 'P' })
for (let i = 0; i < 3; i--) {}
return { done: true }`), []string{"clean"}, Options{Timeout: 300 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		if rs[0].Err == nil || !strings.Contains(rs[0].Err.Error(), "did not finish") {
			t.Fatalf("want the deadline verdict, got %v", rs[0].Err)
		}
	})

	t.Run("a call with no options object", func(t *testing.T) {
		r := run1(t, preamble+`phase('P')
await agent(`+"`go`"+`)
return { done: true }`, "clean")
		wantViolation(t, r, "no options object")
	})

	t.Run("an unrecognised report field does not push clean onto the failure branch", func(t *testing.T) {
		r := run1(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { escalated: { type: 'boolean' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const r = await spawn(`+"`go`"+`, { model: 'sonnet', effort: 'low', label: 'w', schema: S, phase: 'P' })
return { done: true, tookFailure: Boolean(r.escalated) }`, "clean")
		m, _ := r.Return.(map[string]any)
		if b, _ := m["tookFailure"].(bool); b {
			t.Fatalf("the clean mode must walk the happy path: %+v", r.Return)
		}
		if !strings.Contains(strings.Join(r.Notes, "\n"), "unrecognised report field") {
			t.Fatalf("a defaulted field must be named: %+v", r.Notes)
		}
	})
}

// The clean mode is the run everyone wants. P2f shipped a script whose happy
// path returned `green: false` — a repair agent spawned only when acceptance
// FAILS was counted in the liveness ledger — and seven modes reported OK,
// because acceptance failed in every one of them and repair was therefore
// always alive. The defect was found by an opus reviewer copying the script's
// tail into a file and executing it by hand with green values.
func TestCleanModeRefusesANotGreenVerdict(t *testing.T) {
	t.Run("a pass field that is false", func(t *testing.T) {
		r := run1(t, preamble+`phase('P')
const a = await spawn(`+"`go`"+`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
const repair = null
return { green: a !== null && repair !== null }`, "clean")
		wantViolation(t, r, "`green: false` on the path where nothing failed")
	})

	t.Run("a fail field that is true", func(t *testing.T) {
		r := run1(t, preamble+`phase('P')
await spawn(`+"`go`"+`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
return { blocked: true }`, "clean")
		wantViolation(t, r, "`blocked: true` on the path where nothing failed")
	})

	t.Run("the same script with the ledger right passes", func(t *testing.T) {
		r := run1(t, preamble+`phase('P')
const a = await spawn(`+"`go`"+`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
return { green: a !== null, blocked: false }`, "clean")
		if len(r.Violations) > 0 {
			t.Fatalf("a clean run returning green must pass: %v", r.Violations)
		}
	})
}

// `done` is the completion flag of every schema in the project this runner was
// written for, and it matched neither boolean list — so it was FALSE in all
// seven modes, `clean` included, and the branch guarded by it was executed by no
// mode at all.
func TestCompletionBooleanIsAPassField(t *testing.T) {
	r := run1(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { done: { type: 'boolean' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const r = await spawn(`+"`go`"+`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
return { finished: Boolean(r.done) }`, "clean")
	m, _ := r.Return.(map[string]any)
	if b, _ := m["finished"].(bool); !b {
		t.Fatalf("the clean mode must report a producer as done: %+v", r.Return)
	}
	if len(r.Notes) > 0 {
		t.Fatalf("a recognised field needs no note: %v", r.Notes)
	}
}

// And the note that names an unrecognised one is the RUNNER's, not the script's:
// it used to be appended to the script's own log() output, arriving as the
// sixteenth line of a fifteen-hundred-line dump under the same `log:` prefix.
func TestRunnerNotesAreNotScriptLogs(t *testing.T) {
	r := run1(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { escalated: { type: 'boolean' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const r = await spawn(`+"`go`"+`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
log('the script said this')
return { ok: true, took: Boolean(r.escalated) }`, "clean")
	if strings.Contains(strings.Join(r.Logs, "\n"), "unrecognised") {
		t.Fatalf("the runner's note must not sit in the script's log: %v", r.Logs)
	}
	if !strings.Contains(strings.Join(r.Notes, "\n"), "escalated") {
		t.Fatalf("the unrecognised field must be named in the notes: %v", r.Notes)
	}
}

// The fail side of the clean guard is an anchored list, not the substring regex
// the fixture synthesiser uses: that one matches `fail` inside `noFailures` and
// `error` inside `errorFree`, which are names a script legitimately returns TRUE
// on a clean run. A guard that fails the bar on one of those is a false red.
func TestCleanGuardDoesNotReadANegatedName(t *testing.T) {
	for _, name := range []string{"noFailures", "errorFree", "notBroken", "zeroOpenIssues"} {
		t.Run(name, func(t *testing.T) {
			r := run1(t, preamble+`phase('P')
await spawn(`+"`go`"+`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
return { `+name+`: true }`, "clean")
			if len(r.Violations) > 0 {
				t.Fatalf("%s: true is what a clean run says: %v", name, r.Violations)
			}
		})
	}
}

// A completion flag the pass list does not carry reads FALSE in every mode
// including `clean`, so the happy path returns not-green and the mode correctly
// fails a script that is correct. Found by the skeleton `wf new-script` emits,
// which named its acceptance flag `goalMet`: completion vocabulary the list had
// no entry for. The list stays anchored — the substring form matches `fail`
// inside `noFailures`.
func TestCompletionVocabularyCoversTheMetFamily(t *testing.T) {
	for _, field := range []string{"goalMet", "met", "satisfied", "achieved", "scopeComplete"} {
		t.Run(field, func(t *testing.T) {
			body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const A = { type: 'object', properties: { ` + field + `: { type: 'boolean' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const r = await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: A })
return { done: true, green: Boolean(r && r.` + field + `) }`
			r := run1(t, body, "clean")
			m, ok := r.Return.(map[string]any)
			if !ok {
				t.Fatalf("return is %T: %+v", r.Return, r)
			}
			if got, _ := m["green"].(bool); !got {
				t.Fatalf("%s must synthesise TRUE in the clean mode, got green=%v (violations=%v)", field, m["green"], r.Violations)
			}
			if len(r.Violations) > 0 {
				t.Fatalf("a clean run of a correct script must not violate: %v", r.Violations)
			}
		})
	}
}

// A declared `enum` is the only place a script says which STRINGS its own
// branches compare against, and ignoring it left every `x === 'literal'` branch
// of every orchestration script unreachable in a dry run — measured on the
// emitted skeleton, `f.severity === 'blocker'` was false in all seven modes, so
// the gate logged "clean" and the whole fix-round path was executed by no mode.
// That is P2e's finding 8 one layer down: the runner's fixture, not the script,
// decided what was exercised.
func TestPickEnum_choosesByMeaningNotPosition(t *testing.T) {
	cases := []struct {
		name           string
		members        []string
		clean, failing string
	}{
		// Worst-FIRST, as this corpus writes severity.
		{"severity", []string{"blocker", "major", "minor", "note"}, "minor", "blocker"},
		// Worst-LAST, as it writes verdicts. A positional rule walks one of
		// these two backwards, whichever way it is written.
		{"verdict", []string{"pass", "blockers"}, "pass", "blockers"},
		// `not-run` names no failure word and IS a failure.
		{"bar result", []string{"pass", "fail", "not-run"}, "pass", "fail"},
		// The six the substring version got wrong, two of them real: they are
		// out of ~/gates/p2e/artifacts/p2e.workflow.mjs and are branched on.
		{"p2e status", []string{"complete", "partial"}, "complete", "partial"},
		{"p2e gate", []string{"clean", "advisory", "blocked"}, "clean", "blocked"},
		{"health", []string{"ready", "degraded", "down"}, "ready", "degraded"},
		{"repair", []string{"broken", "fixed"}, "fixed", "broken"},
		// A negator makes a member its own opposite under substring matching.
		{"negated ok", []string{"not-ok", "ok"}, "ok", "not-ok"},
		{"negated error", []string{"error-free", "error"}, "error-free", "error"},
		{"plain", []string{"yes", "no"}, "yes", "no"},
	}
	for _, c := range cases {
		if got := pickEnum("f", c.members, "clean", 0, nil); got != c.clean {
			t.Errorf("%s clean: pickEnum(%v) = %q, want %q", c.name, c.members, got, c.clean)
		}
		if got := pickEnum("f", c.members, "blockers", 0, nil); got != c.failing {
			t.Errorf("%s failing: pickEnum(%v) = %q, want %q", c.name, c.members, got, c.failing)
		}
	}
}

// A vocabulary this runner cannot read must be REPORTED, not guessed at behind
// a deterministic-looking first member. The first version fell through to a
// POSITION while its own comment said it never did, and nothing recorded the
// miss — so a mode that never walked its branch looked exactly like one that
// did. The boolean branch has recorded its unrecognised names since the sixth
// wave; this is the same claim one type over.
func TestPickEnum_recordsAVocabularyItCannotRead(t *testing.T) {
	rec := map[string]int{}
	got := pickEnum("stage", []string{"alpha", "beta"}, "clean", 0, rec)
	if got != "alpha" {
		t.Fatalf("the fallback is not the first member: %q", got)
	}
	if len(rec) == 0 {
		t.Fatal("an unreadable enum was guessed at in silence; the mode may not have walked the branch it exists to walk")
	}
	// A vocabulary it CAN read records nothing.
	clean := map[string]int{}
	if pickEnum("severity", []string{"blocker", "minor"}, "clean", 0, clean); len(clean) != 0 {
		t.Fatalf("a readable enum was recorded as unreadable: %v", clean)
	}
}

// And the choice must reach the synthesised object, not merely exist.
func TestValue_honoursADeclaredEnum(t *testing.T) {
	schema := map[string]any{"type": "string", "enum": []any{"blocker", "major", "minor", "note"}}
	if got := value("severity", schema, "blockers", "review:gate1", 0, 0, nil); got != "blocker" {
		t.Fatalf("a failing mode synthesised %q, so no `severity === 'blocker'` branch is reachable", got)
	}
	if got := value("severity", schema, "clean", "review:gate1", 0, 0, nil); got != "minor" {
		t.Fatalf("the clean mode synthesised %q, which walks the failure path of every gate", got)
	}
	// A string with no enum keeps the old, deliberately unmatchable placeholder.
	plain := map[string]any{"type": "string"}
	if got, _ := value("summary", plain, "clean", "review:gate1", 0, 0, nil).(string); got == "" || got[0] != '<' {
		t.Fatalf("a plain string stopped being the recorded placeholder: %q", got)
	}
}

// The P3a defect: `dead-reviews` matched a SUBSTRING of the label, so a
// producer labelled `preview` was killed as a reviewer. The mode ended the run
// at section five, killed no gate reviewer at all, and reported OK — over a
// seventh of the agents it exists to exercise.
func TestDeadReviewsMatchesLabelSegments(t *testing.T) {
	body := preamble + `phase('P')
const a = await spawn(` + "`one`" + `, { model: 'sonnet', effort: 'low', label: 'preview', schema: S, phase: 'P' })
const b = await spawn(` + "`two`" + `, { model: 'sonnet', effort: 'low', label: 'review:store', schema: S, phase: 'P' })
return { done: true, dead: [a, b].filter((x) => !x).length }`
	r := run1(t, body, "dead-reviews")
	m, _ := r.Return.(map[string]any)
	if n, _ := m["dead"].(int64); n != 1 {
		t.Fatalf("only the reviewer may come back null, got %v dead (killed %v)", n, r.Killed)
	}
	if len(r.Killed) != 1 || r.Killed[0] != "review:store" {
		t.Fatalf("the mode must kill the reviewer and name it, killed %v", r.Killed)
	}
}

// One label spawned many times — a reviewer inside a loop, which is the shape
// every real script has — is ONE killed label. Listed once per spawn the line
// reads as many reviewers, which is the report saying something it did not
// measure.
func TestKilledLabelsAreListedOncePerLabel(t *testing.T) {
	body := preamble + `phase('P')
for (const n of [1, 2, 3]) {
  await spawn('go ' + n, { model: 'sonnet', effort: 'low', label: 'review:store', schema: S, phase: 'P' })
}
return { done: true }`
	r := run1(t, body, "dead-reviews")
	if len(r.Killed) != 1 || r.Killed[0] != "review:store" {
		t.Fatalf("three spawns of one label are one killed label, got %v", r.Killed)
	}
	if r.Spawned != 3 {
		t.Fatalf("the three spawns must still be counted, got %d", r.Spawned)
	}
}

// The stems match a segment's HEAD, never its tail, and they inflect: the
// reviewer is `reviewer` as often as `review`, and `verifier` is the verify
// agent. Each row here fails under the whole-word matcher as well as under the
// substring one it replaced.
func TestReviewishSegments(t *testing.T) {
	for _, c := range []struct {
		label string
		want  bool
	}{
		{"preview", false}, // the measured defect: `review` in the TAIL
		{"postfix", false}, // same shape, other stem
		{"fixture", false}, // and the prefix repair's own over-kill: `fix` HEADS it
		{"reviewer", true}, // the inflection an exact `review` would drop
		{"reviews", true},
		{"review:store", true},
		{"reviewStore", true},
		{"storeReview", true}, // camelCase is a boundary: without it the stem sits mid-string
		{"gate1Review", true}, // a digit is a lower for that boundary, or the segment never ends
		{"gate-1-fix", true},
		{"verifier", true},
		{"acceptance:bars", true},
		{"store", false},
		{"taxonomy", false},
	} {
		if got := reviewish(c.label); got != c.want {
			t.Errorf("reviewish(%q) = %v, want %v (segments %v)", c.label, got, c.want, labelSegments(c.label))
		}
	}
}

// A label the OPERATOR named that never spawns kills nobody, in silence: every
// guard in the mode then runs over the empty set and the mode reports OK. Same
// shape as the matcher choosing wrong, with the typo one layer up.
func TestDeadLabelThatNeverSpawns(t *testing.T) {
	body := preamble + `phase('P')
await spawn(` + "`one`" + `, { model: 'sonnet', effort: 'low', label: 'worker', schema: S, phase: 'P' })
return { done: true }`
	rs, err := Run(script(t, body), []string{"clean"}, Options{Dead: []string{"wroker"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(rs[0].Notes, "\n"), "wroker") {
		t.Fatalf("a --dead label that never spawned must be named: %+v", rs[0].Notes)
	}
	// And the correct spelling must NOT produce the note, or it is noise.
	rs, err = Run(script(t, body), []string{"clean"}, Options{Dead: []string{"worker"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range rs[0].Notes {
		if strings.Contains(n, "--dead named") {
			t.Fatalf("a --dead label that DID spawn must not be reported as unused: %q", n)
		}
	}
}

// The P3a class, reproduced: two agents given the same job, one sentence given
// to one of them and not to the other. That gate's last round was spent
// entirely on it, and the sweep that closed the round — which found a sixth
// copy of one rule no reviewer lens had filed — was a hand diff.
//
// Validated against the real script as well: run over the revision BEFORE the
// closing commit, `--prompt-diff accept:reads,verify` prints the three
// sentences that commit added, and over the revision after it prints none.
func TestPromptDiffNamesWhatOnePromptSaysAndTheOtherDoesNot(t *testing.T) {
	body := preamble + `phase('P')
const RULE = 'A clause you believe is true but that no test holds is satisfied FALSE.'
const COMMON = 'Answer every clause in the block you were handed, by its bare number.'
await spawn(COMMON + ' ' + RULE, { model: 'sonnet', effort: 'low', label: 'accept:reads', schema: S, phase: 'P' })
await spawn(COMMON + ' Report each bar under its own key with its verbatim output.', { model: 'sonnet', effort: 'low', label: 'verify', schema: S, phase: 'P' })
return { done: true }`
	rs, err := Run(script(t, body), []string{"clean"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ps, mode := Composed(rs)
	if mode != "clean" || len(ps) != 2 {
		t.Fatalf("want two composed prompts from the clean mode, got %d from %q", len(ps), mode)
	}
	a, err := FindPrompt(ps, "accept:reads")
	if err != nil {
		t.Fatal(err)
	}
	b, err := FindPrompt(ps, "verify")
	if err != nil {
		t.Fatal(err)
	}
	onlyA, onlyB, shared := check.PromptDiff(a, b)
	if shared != 1 {
		t.Fatalf("the two prompts share one sentence, got %d", shared)
	}
	if len(onlyA) != 1 || !strings.Contains(onlyA[0].Text, "no test holds") {
		t.Fatalf("the rule given only to the walker must be named: %+v", onlyA)
	}
	if len(onlyB) != 1 || !strings.Contains(onlyB[0].Text, "verbatim output") {
		t.Fatalf("the sentence given only to the verifier must be named: %+v", onlyB)
	}
}

// The prompts EXIST only here: measured on the P3a script, 14 of 15 are
// composed at run time, so a static reader sees one of them and a diff built
// there would report nothing on the very corpus that produced the class.
func TestComposedSeesAPromptBuiltAtRunTime(t *testing.T) {
	body := preamble + `phase('P')
const parts = ['The walker answers every clause it is handed,', 'by its bare number.']
await spawn(parts.join(' '), { model: 'sonnet', effort: 'low', label: 'walk', schema: S, phase: 'P' })
return { done: true }`
	rs, err := Run(script(t, body), []string{"clean"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ps, _ := Composed(rs)
	if len(ps) != 1 || !strings.Contains(ps[0].Text, "by its bare number") {
		t.Fatalf("the composed prompt must reach the diff whole: %+v", ps)
	}
}

// A label the operator typed that means two things resolves to neither: the
// class `wf check cite` reports on an ambiguous citation path.
func TestFindPromptRefusesAnAmbiguousLabel(t *testing.T) {
	ps := []check.NamedText{{Name: "review:gate1"}, {Name: "review:gate2"}, {Name: "verify"}}
	if _, err := FindPrompt(ps, "review"); err == nil {
		t.Fatal("an ambiguous label must be refused by name")
	} else if !strings.Contains(err.Error(), "review:gate1") || !strings.Contains(err.Error(), "review:gate2") {
		t.Fatalf("the refusal must name what it could have meant: %v", err)
	}
	// A unique substring is the spelling an operator actually types.
	got, err := FindPrompt(ps, "gate2")
	if err != nil || got.Name != "review:gate2" {
		t.Fatalf("a unique substring must resolve, got %q %v", got.Name, err)
	}
	if _, err := FindPrompt(ps, "accept"); err == nil {
		t.Fatal("a label nothing matches must be refused")
	} else if !strings.Contains(err.Error(), "verify") {
		t.Fatalf("the refusal must list what the run composed: %v", err)
	}
}

// One label spawned many times is many PROMPTS — a per-item reviewer gets a
// per-item briefing — and returning the first would diff a prompt the operator
// did not choose. Resolving an ambiguity to the first match is the class
// `wf check cite` reports on a citation path.
func TestFindPromptRefusesALabelWithSeveralPrompts(t *testing.T) {
	ps := []check.NamedText{
		{Name: "review:store", Text: "Judge the store section against D5."},
		{Name: "review:store", Text: "Judge the plane section against D7."},
	}
	if _, err := FindPrompt(ps, "review:store"); err == nil {
		t.Fatal("a label spawned with two different prompts has no single prompt to diff")
	} else if !strings.Contains(err.Error(), "DIFFERENT prompts") {
		t.Fatalf("the refusal must say why: %v", err)
	}
	// The same label spawned twice with the SAME text is one prompt: refusing
	// there would refuse a correct loop over identical briefings.
	same := []check.NamedText{{Name: "gate", Text: "One briefing."}, {Name: "gate", Text: "One briefing."}}
	if got, err := FindPrompt(same, "gate"); err != nil || got.Text != "One briefing." {
		t.Fatalf("identical prompts under one label resolve, got %q %v", got.Text, err)
	}
	if _, err := FindPrompt(ps, "  "); err == nil {
		t.Fatal("an empty label matches nothing and must be refused")
	}
}

// A mode that reaches a fraction of the agents the happy path reaches proved a
// fraction of what its OK claims. Measured on the gate that stopped instead of
// closing: a red-bar abort added to the first producer took five of seven modes
// from 15/25/25/25/1/15/25 agents to 18/1/1/1/1/1/1 — the fix round, the
// recheck, the acceptance step and the final verdict exercised by nothing —
// and every mode still exited OK, because section E's rule says a mode that
// spawns NOTHING fails and one is not zero.
//
// A VIOLATION rather than a note, and that was measured: over the three correct
// gated scripts on disk the failure modes run WIDER than the happy path (36
// against 44, 18 against 30, 15 against 25), so none of them trips it.
func TestCollapseAgainstTheHappyPath(t *testing.T) {
	rs := []Result{
		{Mode: "clean", Spawned: 6},
		{Mode: "blockers", Spawned: 1},
		{Mode: "acceptfail", Spawned: 4},
		{Mode: "unfixed", Spawned: 3},
		{Mode: "throw-serial", Spawned: 1},
		{Mode: "no-args", Spawned: 1},
		{Mode: "dead-reviews", Spawned: 0},
	}
	noteCollapse(rs)

	if len(rs[0].Violations) != 0 {
		t.Fatalf("the baseline cannot collapse against itself: %v", rs[0].Violations)
	}
	// The MODE's name is in the text: the tail of a run deduplicates notes by
	// their string, so five collapsed modes printed as one anonymous line.
	if len(rs[1].Violations) != 1 || !strings.Contains(rs[1].Violations[0], "blockers: reached 1 agent(s) against 6 in `clean`") {
		t.Fatalf("want the mode named with both counts and the baseline: %v", rs[1].Violations)
	}
	// Four of six is not a collapse: a check that fires every time is a check
	// nobody reads.
	if len(rs[2].Violations) != 0 {
		t.Fatalf("a mode above half the baseline must pass silently: %v", rs[2].Violations)
	}
	// Exactly half passes; the guard is "fewer than half".
	if len(rs[3].Violations) != 0 {
		t.Fatalf("exactly half the baseline is not a collapse: %v", rs[3].Violations)
	}
	// `throw-serial` aborts AT the first spawn by construction, so its count is
	// one by design and a ratio over it measures the runner.
	if len(rs[4].Violations) != 0 {
		t.Fatalf("throw-serial is one agent by construction: %v", rs[4].Violations)
	}
	// `no-args` is the other mode the RUNNER narrows on purpose: a script that
	// gates its body on `args` is correct there, and warning at one agent while
	// forgiving zero is that reasoning applied backwards.
	if len(rs[5].Violations) != 0 {
		t.Fatalf("no-args is narrowed by the runner, not by the script: %v", rs[5].Violations)
	}
	// A mode that spawned nothing is judged by the zero-spawn rule and by the
	// args forgiveness; a second report here would state one fact twice.
	if len(rs[6].Violations) != 0 {
		t.Fatalf("a zero-spawn mode is already judged: %v", rs[6].Violations)
	}
}

// The baseline is `clean` BY NAME, never the widest mode — and on every real
// script the widest is not clean, because a fix round spawns more than the
// happy path. Picking the widest would demand a far deeper collapse before
// anything fired.
func TestCollapseBaselineIsTheHappyPathNotTheWidest(t *testing.T) {
	rs := []Result{
		{Mode: "blockers", Spawned: 10},
		{Mode: "clean", Spawned: 6},
		{Mode: "unfixed", Spawned: 4},
	}
	noteCollapse(rs)
	// 4 against a `clean` of 6 is above half and silent; against a widest of 10
	// it would have fired.
	if len(rs[2].Violations) != 0 {
		t.Fatalf("the baseline must be `clean`, not the widest mode: %v", rs[2].Violations)
	}
	if len(rs[0].Violations) != 0 || len(rs[1].Violations) != 0 {
		t.Fatalf("nothing else may fire here: %v %v", rs[0].Violations, rs[1].Violations)
	}
}

// Without `clean` in the run the check does not disappear with the mode that
// anchors it: the widest stands in, and the run SAYS the baseline is not the
// happy path.
func TestCollapseNamesTheBaselineItActuallyUsed(t *testing.T) {
	rs := []Result{{Mode: "unfixed", Spawned: 2}, {Mode: "blockers", Spawned: 8}}
	noteCollapse(rs)
	if len(rs[0].Violations) != 1 || !strings.Contains(rs[0].Violations[0], "in `blockers`") {
		t.Fatalf("want the stand-in baseline named: %v", rs[0].Violations)
	}
	for i := range rs {
		if len(rs[i].Notes) != 1 || !strings.Contains(rs[i].Notes[0], "no `clean` mode in this run") {
			t.Fatalf("%s: the absent baseline must be said out loud: %v", rs[i].Mode, rs[i].Notes)
		}
	}
}

// `--mode X` builds a run of ONE, so the stand-in baseline IS that mode and it
// is then skipped — the check vanished in silence over exactly the report that
// motivated it (`--mode blockers` on the banked fixture printed `OK — 1 agents,
// 1 phases` and `1/1 modes completed`).
func TestCollapseSaysItComparedAgainstNothing(t *testing.T) {
	rs := []Result{{Mode: "blockers", Spawned: 1}}
	noteCollapse(rs)
	if len(rs[0].Violations) != 0 {
		t.Fatalf("one mode cannot collapse against itself: %v", rs[0].Violations)
	}
	if len(rs[0].Notes) != 1 || !strings.Contains(rs[0].Notes[0], "compared against NOTHING") {
		t.Fatalf("an absent comparison must not read as a clean one: %v", rs[0].Notes)
	}
}

// Every mode spawning nothing is the zero-spawn violation's own case, and a
// ratio against zero would be a second, weaker report of it. There is no guard
// here to delete: the behaviour falls out of the two skips above, and the guard
// that used to state it survived every mutation.
func TestCollapseIsSilentWhenNothingSpawnedAnywhere(t *testing.T) {
	rs := []Result{{Mode: "clean"}, {Mode: "blockers"}}
	noteCollapse(rs)
	for _, r := range rs {
		if len(r.Violations) != 0 {
			t.Fatalf("%s: %v", r.Mode, r.Violations)
		}
	}
}

// End to end over the banked fixture, which is the P3b shape itself: a guard on
// the first producer that aborts the run when its bars come back red. The COUNT
// is asserted, not merely that something warned — reporting only the first
// collapsed mode passed an assertion on "any".
func TestCollapseFixtureEmptiesTheFailureModes(t *testing.T) {
	rs, err := Run(filepath.Join("..", "..", "testdata", "collapse.mjs"), Modes, Options{})
	if err != nil {
		t.Fatal(err)
	}
	clean, collapsed := 0, 0
	for _, r := range rs {
		if r.Mode == "clean" {
			clean = r.Spawned
		}
		for _, v := range r.Violations {
			if strings.Contains(v, "exercised a fraction of the script") {
				collapsed++
			}
		}
	}
	if clean != 6 {
		t.Fatalf("the happy path must reach every producer, got %d", clean)
	}
	// blockers, acceptfail and unfixed — the three modes whose stubbed agents
	// report FAILURE, which is what this fixture collapses on. `dead-reviews`
	// is not among them any more: it runs the PASSING fixture so that the only
	// thing wrong with the run is the reviewers returning nothing, and it
	// therefore reaches the happy path's width here, correctly. `throw-serial`
	// and `no-args` are exempt, and `clean` is the baseline.
	if collapsed != 3 {
		t.Fatalf("want every failure mode named, got %d", collapsed)
	}
	// And the run must FAIL: a WARN leaves `7/7 modes completed` and exit 0
	// standing, which is the claim a reviewer banks from it.
	for _, r := range rs {
		if r.Mode == "blockers" && r.OK() {
			t.Fatal("a mode that exercised a sixth of the script must not report OK")
		}
	}
}

// `dead-reviews` isolates ONE variable: the reviewers returned nothing. The
// failing fixture destroyed it — every SURVIVING agent then reported failure
// too, so the run came back not-green whatever the script did with the deaths.
// Measured over the five correct scripts on disk plus a script that filters its
// dead reviewers out of the verdict: the shipped mode returned `green: false`
// on all six and separated nothing.
func TestDeadReviewsRunsThePassingFixture(t *testing.T) {
	// One field per fixture axis the runner has: a declared enum, a failure
	// boolean, and a failure array. `ok` is deliberately absent — it is a PASS
	// boolean, which only `acceptfail` moves, so it would assert nothing here.
	body := preamble + `phase('P')
const V = { type: 'object', properties: { verdict: { type: 'string', enum: ['pass', 'blockers'] }, blocked: { type: 'boolean' }, defects: { type: 'array' } } }
const w = await spawn('work', { model: 'sonnet', effort: 'low', label: 'worker', phase: 'P', schema: V })
return { seen: w.verdict, blocked: w.blocked, defects: w.defects.length, green: false }
`
	rs, err := Run(script(t, body), []string{"clean", "dead-reviews", "blockers"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]any{}
	for _, r := range rs {
		m, _ := r.Return.(map[string]any)
		got[r.Mode] = m
	}
	// The surviving agent must report exactly what it reports on the happy path.
	for _, k := range []string{"seen", "blocked", "defects"} {
		if got["dead-reviews"][k] != got["clean"][k] {
			t.Errorf("%s: dead-reviews saw %v, clean saw %v — the mode is not isolating the deaths", k, got["dead-reviews"][k], got["clean"][k])
		}
		if got["blockers"][k] == got["clean"][k] {
			t.Errorf("%s: the failing fixture must still differ from the passing one, both %v", k, got["clean"][k])
		}
	}
}

// The mode's whole point. A verdict that still reads as success while the agents
// it spawned returned nothing is a script whose green does not depend on its
// gates having run.
func TestDeadReviewsRefusesAGreenVerdictOverDeadAgents(t *testing.T) {
	body := preamble + `phase('P')
const w = await spawn('work', { model: 'sonnet', effort: 'low', label: 'worker', phase: 'P', schema: S })
const r = await spawn('review', { model: 'sonnet', effort: 'high', label: 'review:x', phase: 'P', schema: S })
return { green: true, dead: r ? [] : ['review:x'] }
`
	rs, err := Run(script(t, body), []string{"dead-reviews"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs[0].Killed) == 0 {
		t.Fatal("the mode killed nobody, so this asserts nothing")
	}
	if !strings.Contains(strings.Join(rs[0].Violations, " "), "does not depend on those agents having run") {
		t.Fatalf("a green verdict over dead agents must be a violation, got %v", rs[0].Violations)
	}
	if rs[0].OK() {
		t.Fatal("and the mode must FAIL — a WARN leaves 7/7 and exit 0 standing")
	}
}

// The same claim in the other vocabulary. A script whose verdict field is named
// for the failure rather than the success says exactly as much by returning
// `failed: false` over dead agents, and the two halves of this guard are
// separately deletable — this one survived the sweep until it had its own case.
func TestDeadReviewsRefusesAFailedFalseOverDeadAgents(t *testing.T) {
	body := preamble + `phase('P')
const w = await spawn('work', { model: 'sonnet', effort: 'low', label: 'worker', phase: 'P', schema: S })
const r = await spawn('review', { model: 'sonnet', effort: 'high', label: 'review:x', phase: 'P', schema: S })
return { failed: false, dead: r ? [] : ['review:x'] }
`
	rs, err := Run(script(t, body), []string{"dead-reviews"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs[0].Killed) == 0 {
		t.Fatal("the mode killed nobody, so this asserts nothing")
	}
	if !strings.Contains(strings.Join(rs[0].Violations, " "), "`failed: false`") {
		t.Fatalf("a not-failed verdict over dead agents must be a violation, got %v", rs[0].Violations)
	}
}

// A producer-only script kills nobody and is CORRECT to come back green. The
// arming condition is a different field from the verdict it constrains, which
// is the rule a gate banked after four instances of the opposite in one script.
func TestDeadReviewsIsSilentWhenItKilledNobody(t *testing.T) {
	body := preamble + `phase('P')
const w = await spawn('work', { model: 'sonnet', effort: 'low', label: 'producer', phase: 'P', schema: S })
return { green: true, dead: w ? [] : ['producer'] }
`
	rs, err := Run(script(t, body), []string{"dead-reviews"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs[0].Killed) != 0 {
		t.Fatalf("this script has no reviewer label, got %v", rs[0].Killed)
	}
	for _, v := range rs[0].Violations {
		if strings.Contains(v, "dead-reviews:") {
			t.Fatalf("a script with nothing to kill must not be accused of ignoring the deaths: %s", v)
		}
	}
}

// The `clean` verdict guard reads "no fixture in this mode can produce that
// verdict". A `--dead` label that spawned IS such a fixture, so the premise is
// false the moment one is named — and without this the correct verdict was
// reported as a violation on every correct script tried.
func TestCleanVerdictGuardStandsDownWhenALabelWasKilled(t *testing.T) {
	body := preamble + `phase('P')
const w = await spawn('work', { model: 'sonnet', effort: 'low', label: 'worker', phase: 'P', schema: S })
return { green: !!w }
`
	rs, err := Run(script(t, body), []string{"clean"}, Options{Dead: []string{"worker"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs[0].Killed) == 0 {
		t.Fatal("nothing was killed, so this asserts nothing")
	}
	for _, v := range rs[0].Violations {
		if strings.Contains(v, "on the path where nothing failed") {
			t.Fatalf("a killed agent IS something failing: %s", v)
		}
	}
}

// End to end over the banked fixture: the emitted harness with the two edits a
// live operator plausibly makes — the reviewer relabelled off the segment the
// mode matches, and a comment-justified filter dropping dead REVIEWERS from the
// green computation. Seven modes were green over it.
func TestDeadReviewsFixtureFails(t *testing.T) {
	rs, err := Run(filepath.Join("..", "..", "testdata", "dead-reviews-green.mjs"), Modes, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if r.Mode != "dead-reviews" {
			continue
		}
		if r.OK() {
			t.Fatal("the banked false green must fail its mode")
		}
		return
	}
	t.Fatal("dead-reviews did not run")
}

// The half the agent ratio cannot see: a failure branch that skips a whole
// SECTION while keeping its agent count within half the happy path's. Measured
// on the shape that motivated it, 13 agents against 15 and 4 phases against 5 —
// the ratio passes and neither the fifth section nor its gate runs in any mode.
func TestAModeThatSkipsAPhaseFailsEvenInsideTheAgentRatio(t *testing.T) {
	rs, err := Run(filepath.Join("..", "..", "testdata", "section-collapse.mjs"), Modes, Options{})
	if err != nil {
		t.Fatal(err)
	}
	skipped := 0
	for _, r := range rs {
		if r.Mode == "clean" && len(r.Phases) != 2 {
			t.Fatalf("the happy path must reach both sections, got %v", r.Phases)
		}
		for _, v := range r.Violations {
			if !strings.Contains(v, "never reached") {
				continue
			}
			skipped++
			if !strings.Contains(v, "`S2`") {
				t.Errorf("the missing phase must be NAMED, got %q", v)
			}
			// The ratio must NOT be what caught it, or this fixture proves
			// nothing the previous check did not already cover.
			for _, o := range r.Violations {
				if strings.Contains(o, "exercised a fraction of the script") {
					t.Errorf("%s: the agent ratio fired too, so the phase check is untested here", r.Mode)
				}
			}
		}
		if r.Mode == "blockers" && r.OK() {
			t.Fatal("a mode that never reached a section must not report OK")
		}
	}
	// blockers, acceptfail and unfixed. `throw-serial` and `no-args` are exempt
	// for the same reason the ratio exempts them, and `dead-reviews` runs the
	// passing fixture, so it reaches both sections.
	if skipped != 3 {
		t.Fatalf("want every failure mode named, got %d", skipped)
	}
}

// Distinct TITLES, never `len(r.Phases)`: a mode that runs one phase twice
// reaches the same count having covered less, and the titles are what a reader
// is being told were exercised.
func TestPhaseCoverageReadsTitlesNotCounts(t *testing.T) {
	if m := missingPhases([]string{"A", "B"}, []string{"A", "A"}); len(m) != 1 || m[0] != "B" {
		t.Fatalf("a repeated phase must not stand in for a missing one, got %v", m)
	}
	if m := missingPhases([]string{"A", "B", "A"}, []string{"B", "A"}); len(m) != 0 {
		t.Fatalf("order and repetition in the baseline must not matter, got %v", m)
	}
	if m := missingPhases([]string{"A", "B", "C"}, []string{"B"}); len(m) != 2 || m[0] != "A" || m[1] != "C" {
		t.Fatalf("the missing titles come back in the baseline's own order, got %v", m)
	}
	// A baseline calling one title twice must not name it twice in the message.
	// Zero of the five real scripts on disk repeat a title (58 calls, 58
	// distinct), so this defends a shape the corpus does not have — it is here
	// because the alternative to a test is deleting the dedupe, and a message
	// naming one phase twice is the report noise this runner is against.
	if m := missingPhases([]string{"A", "B", "A"}, []string{"B"}); len(m) != 1 || m[0] != "A" {
		t.Fatalf("a title missing twice is one missing title, got %v", m)
	}
}

// The banked `collapse.mjs` narrows to one agent inside ONE phase. The two
// checks are additive because they miss opposite shapes, and this is the half
// that says the older one still carries its own fixture alone.
func TestTheCollapseFixtureIsCaughtByTheRatioAndNotByPhases(t *testing.T) {
	rs, err := Run(filepath.Join("..", "..", "testdata", "collapse.mjs"), Modes, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		for _, v := range r.Violations {
			if strings.Contains(v, "never reached") {
				t.Fatalf("%s: the phase check cannot see a one-phase script, so it must stay silent: %s", r.Mode, v)
			}
		}
	}
}
