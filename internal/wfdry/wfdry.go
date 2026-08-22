// Package wfdry executes a Workflow orchestration script with every
// orchestration hook stubbed, across the modes a real run can enter.
//
// The static check reads the script. This one RUNS it, so it walks paths a
// parse cannot reach: a template literal that only composes on the failure
// branch, a field dereferenced on a result that is null in one mode, a gate
// accounted for on a branch that was never taken. A parse is not an execution.
//
// The script is JavaScript and is executed by an embedded engine (goja), so
// this tool has no external runtime to drift against — the previous version
// shelled out to whatever `node` happened to be on PATH. That engine is NOT the
// one the real Workflow tool uses, and that boundary is the honest limit of a
// dry run: it proves the script's control flow, not that the production
// runtime accepts it. The banked meta-guard fixture exists because a dry runner
// that diverges from the real harness is exactly how a false green ships.
package wfdry

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/dop251/goja"
)

// Modes are the states a real run can enter. `clean` matters most: reviewers
// read the failure branches, because that is where the risk looks like it
// lives, so the defects that survive a gate live on the happy path.
var Modes = []string{
	"clean",        // nothing goes wrong
	"blockers",     // every gate returns blockers; walks the fix and delta rounds
	"acceptfail",   // the acceptance step observes a failure; walks the closeout path
	"unfixed",      // a fix agent reports it could not close something
	"throw-serial", // the first spawn throws; walks the early-abort exit
	"dead-reviews", // every review/fix/closeout agent returns null
	"no-args",      // the script runs with no `args` — see Options.Args
}

// Options configure one run.
type Options struct {
	Dead   []string // labels whose spawn returns null
	Budget *int     // budget.total; nil means the script sees null
	Args   string   // JSON handed to the script as `args` (every mode but no-args)
}

// DefaultArgs is what a parameterised workflow sees. It is NOT undefined, and
// that is a fix: the previous runner passed undefined in every mode, so a
// script opening with `if (!args) return {skipped:true}` returned that in all
// six — six green modes over a body that never executed. Both states are real,
// so both are modes now.
const DefaultArgs = `{"topic":"<wfdry topic>","items":["<a>","<b>"],"paths":["<p>"]}`

// Result is one mode's outcome.
type Result struct {
	Mode       string
	Calls      []string
	Phases     []string
	Logs       []string
	Spawned    int
	Violations []string
	Return     any
	ReturnOK   bool
	Err        error
}

// OK reports a mode that completed with nothing to answer for.
func (r Result) OK() bool { return r.Err == nil && len(r.Violations) == 0 && r.ReturnOK }

var metaRe = regexp.MustCompile(`(?m)^export const meta\b`)

// effortTiers mirrors the closed set the Workflow tool accepts.
var effortTiers = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true, "max": true}

// Run executes the script once per mode.
func Run(path string, modes []string, o Options) ([]Result, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// The meta binding is STRIPPED, not renamed to a live one: the real harness
	// parses `export const meta` out before the body runs, so a body that
	// touches `meta` must die here the way it dies at launch. A renamed binding
	// left in scope produced a measured false green — six modes, six rounds,
	// over a guard reading meta.phases — while the launch died in 29 ms.
	src := metaRe.ReplaceAllString(string(raw), "const __wfdry_meta_stripped")

	known := map[string]bool{}
	for _, m := range Modes {
		known[m] = true
	}
	var out []Result
	for _, m := range modes {
		if !known[m] {
			// An invented mode used to EXECUTE and report "1/1 modes completed".
			return nil, fmt.Errorf("unknown mode %q; known modes: %s", m, strings.Join(Modes, ", "))
		}
		out = append(out, runMode(src, m, o))
	}
	forgiveArgsGate(out)
	return out, nil
}

const zeroSpawn = "the mode completed without spawning a single agent — every check in this runner passed over an empty set"

// forgiveArgsGate downgrades the zero-spawn violation for `no-args` alone, and
// only when another mode DID spawn. A script that legitimately returns early
// without `args` is behaving correctly there; a script that spawns nothing in
// every mode is the false green this rule exists for, and the difference is
// only visible across modes.
func forgiveArgsGate(rs []Result) {
	spawnedElsewhere := false
	for _, r := range rs {
		if r.Mode != "no-args" && r.Spawned > 0 {
			spawnedElsewhere = true
		}
	}
	if !spawnedElsewhere {
		return
	}
	for i := range rs {
		if rs[i].Mode != "no-args" {
			continue
		}
		kept := rs[i].Violations[:0]
		for _, v := range rs[i].Violations {
			if strings.HasSuffix(v, zeroSpawn) {
				rs[i].Logs = append(rs[i].Logs, "note: no agent spawned — the script gates its body on `args`, and the other modes do spawn")
				rs[i].ReturnOK = rs[i].ReturnOK || rs[i].Return != nil
				continue
			}
			kept = append(kept, v)
		}
		rs[i].Violations = kept
	}
}

func runMode(src, mode string, o Options) Result {
	r := Result{Mode: mode}
	vm := goja.New()

	dead := map[string]bool{}
	for _, d := range o.Dead {
		dead[d] = true
	}
	bad := func(label, msg string) {
		// Recorded OUT OF BAND and only then thrown. The guide requires a script
		// to wrap the spawn call and turn a throw into null, so a stub that
		// signals only by throwing is signalling into the one mechanism
		// guaranteed to swallow it: the defect would be recorded as a dead agent,
		// the mode would complete, and the runner would report OK. Measured on a
		// real script — three injected defects, three green runs. The violation
		// list is what makes the mode fail.
		r.Violations = append(r.Violations, label+": "+msg)
		panic(vm.ToValue(msg))
	}

	agent := func(call goja.FunctionCall) goja.Value {
		r.Spawned++
		var opts *goja.Object
		if v := call.Argument(1); !goja.IsUndefined(v) && !goja.IsNull(v) {
			opts = v.ToObject(vm)
		}
		get := func(k string) string {
			if opts == nil {
				return ""
			}
			v := opts.Get(k)
			if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
				return ""
			}
			return v.String()
		}
		label := get("label")
		if label == "" {
			label = fmt.Sprintf("agent@%d", r.Spawned)
		}
		model, effort := get("model"), get("effort")
		r.Calls = append(r.Calls, fmt.Sprintf("%s [%s/%s]", label, or(model, "NO MODEL"), or(effort, "NO EFFORT")))

		// A runtime restatement of the static checks — free once the harness
		// runs, and it catches a prompt composed to `undefined` on one branch.
		p := call.Argument(0)
		if goja.IsUndefined(p) || goja.IsNull(p) {
			bad(label, "prompt is undefined, not a string")
		}
		if strings.TrimSpace(p.String()) == "" {
			bad(label, "empty prompt")
		}
		if opts == nil {
			bad(label, "no options object")
		}
		if v := opts.Get("schema"); v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
			bad(label, "no schema")
		}
		if model == "" {
			bad(label, "no model")
		}
		if effort == "" {
			bad(label, "no effort (the call would inherit the launching session)")
		}
		if !effortTiers[effort] {
			bad(label, fmt.Sprintf("effort %q is not one of low, medium, high, xhigh, max", effort))
		}
		if dead[label] {
			panic(vm.ToValue("stubbed death of " + label))
		}
		if mode == "throw-serial" && r.Spawned == 1 {
			panic(vm.ToValue("stubbed throw at the first spawn"))
		}
		if mode == "dead-reviews" && reviewish(label) {
			return promiseOf(vm, goja.Null())
		}
		return promiseOf(vm, vm.ToValue(synth(opts.Get("schema"), mode, label, 0)))
	}

	must := func(name string, v any) {
		if err := vm.Set(name, v); err != nil {
			r.Err = err
		}
	}
	must("agent", agent)
	must("phase", func(t string) { r.Phases = append(r.Phases, t) })
	must("log", func(m string) { r.Logs = append(r.Logs, m) })
	must("workflow", func(goja.FunctionCall) goja.Value { return promiseOf(vm, vm.NewObject()) })

	spent := func() int { return r.Spawned * 20000 }
	budget := vm.NewObject()
	if o.Budget != nil {
		_ = budget.Set("total", *o.Budget)
	} else {
		_ = budget.Set("total", goja.Null())
	}
	_ = budget.Set("spent", func() int { return spent() })
	// With no target set, remaining() is Infinity — the value a `while
	// (budget.total && budget.remaining() > N)` loop is written against.
	_ = budget.Set("remaining", func() float64 {
		if o.Budget == nil {
			return math.Inf(1)
		}
		if rem := float64(*o.Budget - spent()); rem > 0 {
			return rem
		}
		return 0
	})
	must("budget", budget)

	if mode == "no-args" {
		must("args", goja.Undefined())
	} else {
		a := o.Args
		if a == "" {
			a = DefaultArgs
		}
		v, err := vm.RunString("(" + a + ")")
		if err != nil {
			r.Err = fmt.Errorf("--args is not valid JSON: %w", err)
			return r
		}
		must("args", v)
	}

	if _, err := vm.RunString(harness); err != nil {
		r.Err = fmt.Errorf("harness: %w", err)
		return r
	}
	if r.Err != nil {
		return r
	}

	v, err := vm.RunString("(async function(){\n" + src + "\n})()")
	if err != nil {
		r.Err = err
		return r
	}
	p, ok := v.Export().(*goja.Promise)
	if !ok {
		r.Err = fmt.Errorf("the script body did not return a promise")
		return r
	}
	switch p.State() {
	case goja.PromiseStateRejected:
		r.Err = fmt.Errorf("%v", p.Result())
		return r
	case goja.PromiseStatePending:
		r.Err = fmt.Errorf("the script never settled — an await with nothing to resolve it")
		return r
	}
	res := p.Result().Export()
	r.Return = res
	if _, isMap := res.(map[string]any); !isMap {
		r.Violations = append(r.Violations, fmt.Sprintf("the run returned %T, not an object — a verdict nothing can read", res))
	} else {
		r.ReturnOK = true
	}
	// A mode that completes having spawned NOTHING is indistinguishable from a
	// clean run, and every stub check above it is then a guard over the empty
	// set. Measured: a script whose only spawn sat behind a false flag passed
	// 6/6, and one that returned immediately passed 6/6 with zero phases too.
	if r.Spawned == 0 {
		r.Violations = append(r.Violations, "run: "+zeroSpawn)
	}
	return r
}

func or(a, b string) string {
	if a == "" {
		return b
	}
	return a
}

func reviewish(label string) bool {
	l := strings.ToLower(label)
	for _, k := range []string{"review", "fix", "closeout", "verify", "accept"} {
		if strings.Contains(l, k) {
			return true
		}
	}
	return false
}

func promiseOf(vm *goja.Runtime, v goja.Value) goja.Value {
	p, resolve, _ := vm.NewPromise()
	_ = resolve(v)
	return vm.ToValue(p)
}

// harness defines the orchestration hooks that are pure JavaScript. They are
// the real semantics, restated: parallel() is a barrier that turns a throwing
// thunk into null, pipeline() runs each item through every stage independently.
const harness = `
globalThis.parallel = async (thunks) =>
  Promise.all(thunks.map((t) => Promise.resolve().then(t).catch(() => null)))
globalThis.pipeline = async (items, ...stages) =>
  Promise.all(items.map(async (it, i) => {
    let acc = it
    try { for (const st of stages) acc = await st(acc, it, i) } catch { return null }
    return acc
  }))
`

// ---------------------------------------------------------- the fixture contract --
//
// A stub cannot invent a report shape, so it derives one from the `schema` the
// script hands each call — that schema IS the contract — plus the well-known
// field names below, which carry a mode-dependent value. A script whose report
// fields fall outside the set still runs; it just gets placeholder strings,
// which is enough for control flow.

var failArrays = map[string]bool{
	"blockers": true, "majors": true, "failures": true, "unfixed": true,
	"openblockers": true, "concerns": true, "dead": true, "unverifiedgates": true,
	"violations": true, "gaps": true, "risks": true,
}

var failBools = regexp.MustCompile(`(?i)fail|broken|vacuous|missing|stale|dead|unreviewed`)
var passBools = regexp.MustCompile(`(?i)^(passed|ok|green|clean|verified|proven)$`)

func synth(schema goja.Value, mode, label string, depth int) any {
	if schema == nil || goja.IsUndefined(schema) || goja.IsNull(schema) || depth > 6 {
		return map[string]any{}
	}
	obj, ok := schema.Export().(map[string]any)
	if !ok {
		return map[string]any{}
	}
	if t, _ := obj["type"].(string); t == "array" {
		return arrayOf("", obj, mode, label, depth)
	}
	props, _ := obj["properties"].(map[string]any)
	out := map[string]any{}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		pv, _ := props[k].(map[string]any)
		out[k] = value(k, pv, mode, label, depth)
	}
	return out
}

func arrayOf(name string, v map[string]any, mode, label string, depth int) []any {
	low := strings.ToLower(name)
	n := 1
	if failArrays[low] {
		if mode == "clean" {
			n = 0
		}
	}
	switch low {
	case "failures":
		n = 0
		if mode == "acceptfail" {
			n = 1
		}
	case "unfixed":
		n = 0
		if mode == "unfixed" {
			n = 1
		}
	}
	items, _ := v["items"].(map[string]any)
	out := make([]any, 0, n)
	for i := 0; i < n; i++ {
		if t, _ := items["type"].(string); t == "object" {
			out = append(out, synthMap(items, mode, label, depth+1))
		} else {
			out = append(out, fmt.Sprintf("<%s from %s>", or(name, "item"), label))
		}
	}
	return out
}

func synthMap(schema map[string]any, mode, label string, depth int) any {
	props, _ := schema["properties"].(map[string]any)
	out := map[string]any{}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		pv, _ := props[k].(map[string]any)
		out[k] = value(k, pv, mode, label, depth)
	}
	return out
}

func value(name string, v map[string]any, mode, label string, depth int) any {
	t, _ := v["type"].(string)
	switch t {
	case "array":
		return arrayOf(name, v, mode, label, depth)
	case "object":
		return synthMap(v, mode, label, depth+1)
	case "integer", "number":
		return 1
	case "boolean":
		if passBools.MatchString(name) {
			return mode != "acceptfail"
		}
		if failBools.MatchString(name) {
			return mode != "clean"
		}
		return mode == "clean"
	default:
		return fmt.Sprintf("<%s from %s>", name, label)
	}
}
