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
	if get(blocked) != 1 {
		t.Fatalf("the blockers mode must yield one, got %v", get(blocked))
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
		if !strings.Contains(strings.Join(r.Logs, "\n"), "killed nobody") {
			t.Fatalf("a mode that stubbed no death must say so: %+v", r.Logs)
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
		if !strings.Contains(strings.Join(r.Logs, "\n"), "did not recognise") {
			t.Fatalf("a defaulted field must be named: %+v", r.Logs)
		}
	})
}
