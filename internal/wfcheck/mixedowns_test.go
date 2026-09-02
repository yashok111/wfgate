package wfcheck

import "testing"

// The MIXED case, which neither half could see alone. Agent `a`'s prompt is a
// literal declaring `internal/x.go`, agent `b`'s is composed at run time out of
// a literal that declares it too. The parsed half saw ONE owner and did not
// fire; the textual half skipped the path entirely because the parsed half had
// read it — suppression was per PATH — so the LOST WRITE, which is the most
// valuable thing this checker catches, shipped in silence.
const mixedOwners = `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
const OWN_B = ` + "`YOU OWN\n  internal/x.go — the composed one\n`" + `
const mk = (o) => 'do it\n' + o
phase('P')
await spawn(` + "`YOU OWN\n  internal/x.go — the inline one\n`" + `, { model: 'sonnet', effort: 'low', label: 'a', phase: 'P', schema: S })
await spawn(mk(OWN_B), { model: 'sonnet', effort: 'low', label: 'b', phase: 'P', schema: S })
return { done: true }`

func TestOwnershipText_oneParsedOwnerPlusOneTextualBlockCollides(t *testing.T) {
	r := run(t, mixedOwners)
	wantErr(t, r, "internal/x.go is owned by agent a AND declared in 1 `YOU OWN` block(s) in the source")
}

// The suppression that remains is per BLOCK, and it must still be silent where
// the parsed half read every block naming the path: one agent, one literal, one
// declaration. Two reports of one fact is the defect the suppression exists for,
// and a check that fires on its own correct fixture is one nobody reads.
func TestOwnershipText_oneAgentDeclaringOnceIsNotACollision(t *testing.T) {
	body := factoryHead + "phase('P')\n" +
		"await spawn(`YOU OWN\n  internal/a.go — mine\n`, { model: 'sonnet', effort: 'low', label: 'one', phase: 'P', schema: S })\n" +
		"return { done: true }"
	r := run(t, body)
	if has(r, ERROR, "internal/a.go") {
		t.Fatalf("one agent owning one file is not a collision:\n%s", dump(r))
	}
}

// And where the PARSED half already names both agents, the textual half stays
// quiet: it knows which BLOCK declares a path and not which agent is handed it,
// so its line would be the weaker restatement of one already made in full.
func TestOwnershipText_isSilentWhereTheParsedHalfNamesBothAgents(t *testing.T) {
	body := factoryHead + "phase('P')\n" +
		"await spawn(`YOU OWN\n  internal/a.go — mine\n`, { model: 'sonnet', effort: 'low', label: 'one', phase: 'P', schema: S })\n" +
		"await spawn(`YOU OWN\n  internal/a.go — also mine\n`, { model: 'sonnet', effort: 'low', label: 'two', phase: 'P', schema: S })\n" +
		"return { done: true }"
	r := run(t, body)
	wantErr(t, r, "internal/a.go is owned by 2 agents: one, two")
	if has(r, ERROR, "declared in") {
		t.Fatalf("the text half restated a collision the parsed half named in full:\n%s", dump(r))
	}
}
