package wfcheck

import (
	"strings"
	"testing"
)

// A wrapper produced by a FACTORY was invisible in both populations at once.
// `wrapperOf` walks a body and finds the `agent()` call inside the returned
// arrow, so the FACTORY was inventoried as the wrapper: its own invocation line
// became the script's single "call site", every `spawn(...)` vanished, and the
// backstop counts calls by NAME — `spawn` was not a name it knew — so the
// subtraction balanced and nothing fired. `wfcheck` exited 0 over an
// `effort: 'nonsense'` it had never looked at.
const factoryWrapper = `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const BASE = { model: 'sonnet', schema: S }
function makeSpawn(base) {
  return async (p, o) => { try { return await agent(p, { ...base, ...o }) } catch { return null } }
}
const spawn = makeSpawn(BASE)
phase('P')
await spawn(` + "`go`" + `, { effort: 'nonsense', label: 'one', phase: 'P' })
await spawn(` + "`go again`" + `, { effort: 'low', label: 'two', phase: 'P' })
return { done: true }`

func TestFactoryProducedWrapperIsInventoried(t *testing.T) {
	r := run(t, factoryWrapper)
	if !has(r, INFO, "2 agent() call site(s) through 1 wrapper(s) (spawn)") {
		t.Fatalf("the factory's product is the wrapper and its calls are the call sites:\n%s", dump(r))
	}
	// The factory's INVOCATION is not one of them, and neither is the forwarding
	// call inside what it returns.
	if has(r, WARN, "go straight to agent() with no wrapper") {
		t.Errorf("the returned arrow's forwarding call was counted as a call site:\n%s", dump(r))
	}
}

// The whole point of reading it: the options are the factory's base merged with
// the call's own, and an invalid effort in them is what the gate pays for.
func TestFactoryProducedWrapperResolvesTheMergedOptions(t *testing.T) {
	r := run(t, factoryWrapper)
	wantErr(t, r, `effort "nonsense" is not one of`)
	// And the base is READ, not merely tolerated: a `model:` the factory supplies
	// must not be reported as missing on either call.
	if has(r, ERROR, "no `model:` in its options") {
		t.Errorf("the factory's base options were not merged:\n%s", dump(r))
	}
	if has(r, ERROR, "no `schema:` in its options") {
		t.Errorf("the factory's base schema was not merged:\n%s", dump(r))
	}
}

// A function that BOTH forwards to agent() and returns a function is a wrapper,
// not a factory. Accepting a non-wrapper inner as a factory took `spawn` out of
// the inventory entirely — every call site through the one shape the guide asks
// for, lost to a check that reports what it cannot see as nothing.
func TestAWrapperThatReturnsANonWrapperIsStillAWrapper(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { findings: { type: 'array' } } }
function spawn(p, o) {
  const r = agent(p, o)
  return () => r
}
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'one', phase: 'P', schema: S })
return { done: true }`
	r := run(t, body)
	if !has(r, INFO, "1 agent() call site(s) through 1 wrapper(s) (spawn)") {
		t.Fatalf("a wrapper returning a thunk was read as a factory of it:\n%s", dump(r))
	}
}

// A factory nobody invokes still owns the forwarding call inside what it
// returns. Excluding a factory from the WRAPPERS without excluding its returned
// body turns that call into a "call straight to agent() with no wrapper" — a
// WARN about the one shape the guide asks for, plus a call site whose prompt and
// options are a parameter each.
func TestAnUninvokedFactoryContributesNoCallSite(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
function makeSpawn(base) {
  return async (p, o) => { try { return await agent(p, { ...base, ...o }) } catch { return null } }
}
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'one', phase: 'P', schema: S })
log(typeof makeSpawn)
return { done: true }`
	r := run(t, body)
	if has(r, WARN, "go straight to agent() with no wrapper") {
		t.Fatalf("an uninvoked factory's forwarding call was read as a call site:\n%s", dump(r))
	}
	if !has(r, INFO, "1 agent() call site(s)") {
		t.Fatalf("want exactly the one real call site:\n%s", dump(r))
	}
}

// A wrapper held as a PROPERTY is followed by nothing — the inventory resolves
// no member call and the backstop counts by name — so it is REPORTED. Silence
// there is every `W.spawn(...)` missing from every check below.
func TestWrapperHeldInAnObjectLiteralIsReported(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const W = { spawn: async (p, o) => { try { return await agent(p, o) } catch { return null } } }
phase('P')
await W.spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'one', phase: 'P', schema: S })
return { done: true }`
	r := run(t, body)
	wantErr(t, r, "W.spawn forwards to agent(), and this checker follows no call through an object property")
	if !has(r, ERROR, "const spawn = W.spawn") {
		t.Errorf("the refusal must carry the patch:\n%s", dump(r))
	}
}

// The backstop's arithmetic over a factory this resolver cannot follow: the
// returned function forwards to a WRAPPER rather than to agent(), so it is no
// factory by the rule above and `route` resolves to nothing. Its call sites left
// both populations at once and the subtraction balanced; counting the name in
// the textual half only is what makes the two disagree.
func TestBackstopCountsCallsThroughAnUnresolvedWrapper(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
function makeRoute(pref) { return async (p, o) => spawn(pref + p, o) }
const route = makeRoute('x: ')
phase('P')
await route(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'one', phase: 'P', schema: S })
await route(` + "`go again`" + `, { model: 'sonnet', effort: 'low', label: 'two', phase: 'P', schema: S })
return { done: true }`
	r := run(t, body)
	if !has(r, ERROR, "inventory implausible") {
		t.Fatalf("calls through a name the inventory never resolved must reach the backstop:\n%s", dump(r))
	}
	// And makeRoute is NOT a factory — what it returns forwards to a wrapper,
	// not to agent() — so its own forwarding call is still the one call site the
	// inventory can see. Accepting a non-wrapper inner as a factory silently
	// excluded that body and took the count to zero.
	if !has(r, INFO, "1 agent() call site(s)") {
		t.Fatalf("the one visible call site was excluded as a factory's body:\n%s", dump(r))
	}
}

// And the arithmetic must not fire on a script it CAN read: a check that paints
// its own correct fixture is a check nobody reads.
func TestBackstopIsSilentOnAFactoryItResolves(t *testing.T) {
	r := run(t, factoryWrapper)
	for _, f := range r.Findings {
		if strings.Contains(f.Msg, "inventory implausible") {
			t.Fatalf("the backstop fired on a script the inventory read in full:\n%s", dump(r))
		}
	}
}

// The same factory written in TWO statements. Reading only a directly returned
// literal made `makeSpawn` a wrapper again and `f` a second one, both `spawn(…)`
// calls vanished, and the backstop's arithmetic balanced over the loss:
// `wfcheck` exited 0 on an `effort: 'nonsense'` it never looked at.
func TestFactoryReturningALocalBindingIsStillAFactory(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const BASE = { model: 'sonnet', schema: S }
function makeSpawn(b) {
  const f = async (p, o) => { try { return await agent(p, { ...b, ...o }) } catch { return null } }
  return f
}
const spawn = makeSpawn(BASE)
phase('P')
await spawn(` + "`go`" + `, { effort: 'nonsense', label: 'one', phase: 'P' })
await spawn(` + "`go again`" + `, { effort: 'low', label: 'two', phase: 'P' })
return { done: true }`
	r := run(t, body)
	if !has(r, INFO, "2 agent() call site(s) through 1 wrapper(s) (spawn)") {
		t.Fatalf("a factory returning a local binding is one, and its product is the wrapper:\n%s", dump(r))
	}
	wantErr(t, r, `effort "nonsense" is not one of`)
}

// A factory PARAMETER and a wrapper parameter of the same name: the wrapper's
// own binding wins, which is what JavaScript says too. Overriding it made every
// call read the factory's base options — the same value for all of them — so
// two agents resolved to one set and a real difference between them vanished.
func TestFactoryBaseDoesNotOverrideTheWrapperParameter(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const BAD = { model: 'sonnet', schema: S, effort: 'nonsense' }
function makeSpawn(o) { return async (p, o) => agent(p, o) }
const spawn = makeSpawn(BAD)
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'one', phase: 'P', schema: S })
return { done: true }`
	r := run(t, body)
	if has(r, ERROR, `"nonsense"`) {
		t.Fatalf("the factory's base overrode the wrapper's own parameter:\n%s", dump(r))
	}
}

// The same collision where the CALL passes nothing for the wrapper's parameter,
// and the forwarded expression SPREADS it. `o` is undefined at run time, so
// `{...o}` contributes nothing; filling it from the factory's base instead gives
// this call somebody else's options, which is a wrong answer rather than an
// incomplete one. The spread is what makes the case reachable: a bare `o` in the
// forwarding position is already caught as ABSENT before any scope is applied.
func TestFactoryBaseDoesNotFillAnEmptyWrapperParameter(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const BAD = { model: 'sonnet', schema: S, effort: 'nonsense' }
function makeSpawn(o) { return async (p, o) => agent(p, { ...o, phase: 'P' }) }
const spawn = makeSpawn(BAD)
phase('P')
await spawn(` + "`go`" + `)
return { done: true }`
	r := run(t, body)
	if has(r, ERROR, `"nonsense"`) {
		t.Fatalf("an empty wrapper parameter was filled from the factory's base:\n%s", dump(r))
	}
	if !has(r, WARN, "options spread a value this check cannot resolve") {
		t.Fatalf("present and unreadable is its own answer, and must be said:\n%s", dump(r))
	}
}

// A factory called with FEWER arguments than it has parameters. The parameter is
// `undefined` at run time, so it must read as UNBOUND — not as the top-level
// const of the same name, which is the shape that once made two agents resolve
// to one label and deduplicated their ownership collision away.
func TestFactoryParameterWithNoArgumentIsUnbound(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const base = { effort: 'nonsense' }
function makeSpawn(base) { return async (p, o) => agent(p, { ...base, ...o }) }
const spawn = makeSpawn()
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', label: 'one', phase: 'P', schema: S })
return { done: true }`
	r := run(t, body)
	// Pinned wherever the answer comes from: collectShadowed marks every
	// parameter in the program, so a factory parameter with no argument is
	// UNKNOWN and never the global of the same name — which is the shape that
	// once made two agents resolve to one label.
	if has(r, ERROR, `"nonsense"`) {
		t.Fatalf("an unbound factory parameter answered from a same-named top-level const:\n%s", dump(r))
	}
	if !has(r, WARN, "options spread a value this check cannot resolve") {
		t.Fatalf("present and unreadable is its own answer, and must be said:\n%s", dump(r))
	}
}

// A `var` binding CAN be written twice, and the second must not append a second
// wrapper under the same name: `names` would carry it twice, so the backstop
// would count every call site twice and manufacture an implausible inventory.
func TestAFactoryProductBoundTwiceIsOneWrapper(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const BASE = { model: 'sonnet', schema: S }
function makeSpawn(b) { return async (p, o) => agent(p, { ...b, ...o }) }
var spawn = makeSpawn(BASE)
var spawn = makeSpawn(BASE)
phase('P')
await spawn(` + "`go`" + `, { effort: 'low', label: 'one', phase: 'P' })
return { done: true }`
	r := run(t, body)
	if !has(r, INFO, "1 agent() call site(s) through 1 wrapper(s) (spawn)") {
		t.Fatalf("one name is one wrapper:\n%s", dump(r))
	}
	if has(r, ERROR, "inventory implausible") {
		t.Fatalf("a doubled name double-counted the textual population:\n%s", dump(r))
	}
}
