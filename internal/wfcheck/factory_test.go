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
