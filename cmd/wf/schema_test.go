package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scriptWith writes a script whose acceptance leaf requires the named keys, so
// the two revisions differ in exactly the quantity this check measures.
func scriptWith(leafRequired string) string {
	req := ""
	if leafRequired != "" {
		req = "required: [" + leafRequired + "], "
	}
	return `export const meta = { name: 'demo', description: 'd', phases: [{ title: 'P' }] }
const NAMES = ['P1', 'P2', 'P3']
const S = { type: 'object',
  $defs: { prop: { type: 'object', ` + req + `properties: { met: { type: 'boolean' }, evidence: { type: 'string' } } } },
  required: ['props'],
  properties: { props: { type: 'object', required: NAMES, properties: Object.fromEntries(NAMES.map((n) => [n, { $ref: '#/$defs/prop' }])) } } }
async function spawn(p, o) { try { return await agent(p, o) } catch { return null } }
phase('P')
const r = await spawn('go', { model: 'sonnet', effort: 'low', label: 'accept', schema: S })
return { done: r !== null, dead: r ? [] : ['accept'] }
`
}

// gateWithScript is a workspace whose artifact is an orchestration script,
// frozen once so `check schema` has a reference to measure from.
func gateWithScript(t *testing.T, body string) (wsDir, art string) {
	t.Helper()
	wsDir = newGate(t)
	art = filepath.Join(wsDir, "artifacts", "demo.mjs")
	if err := os.WriteFile(art, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "add", art); err != nil {
		t.Fatal(err)
	}
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	return wsDir, art
}

// The defect this check exists for, reproduced: a `required` array disappears
// from a leaf inside an edit whose purpose is width, nothing static can see it —
// the leaf is built in an `Object.fromEntries(...map(...))` callback — and for
// two rounds an agent may return the object with none of the fields anyone
// downstream reads.
//
// Validated against the workspace it came from: P3e's `accept` schema goes 74
// required keys and 1 object requiring none at its round-10 freeze to 33 and 23
// at round 11, at a width that FELL from 3869 to 3314.
func TestCheckSchemaNamesADroppedRequired(t *testing.T) {
	wsDir, art := gateWithScript(t, scriptWith(`'met', 'evidence'`))
	if err := os.WriteFile(art, []byte(scriptWith("")), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "schema")
	if err == nil {
		t.Fatalf("a schema that stops requiring a field is a finding, got exit 0:\n%s", out)
	}
	mustContain(t, out, "DROP", "accept", "require LESS")
	if !strings.Contains(out, "NARROWING") {
		t.Fatalf("the width moved with it and the report must say so:\n%s", out)
	}
}

// The other direction is printed and is NOT a finding: a schema requiring more
// of its agent is what this check protects.
func TestCheckSchemaDoesNotFailOnARise(t *testing.T) {
	wsDir, art := gateWithScript(t, scriptWith(`'met'`))
	if err := os.WriteFile(art, []byte(scriptWith(`'met', 'evidence'`)), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "schema")
	noErr(t, err, "check schema")
	mustContain(t, out, "rose")
	if strings.Contains(out, "DROP") {
		t.Fatalf("a rise is not a drop:\n%s", out)
	}
}

// An unchanged script says so with its label count, because "no drop" and "no
// schema was read" print differently or the second reads as the first.
func TestCheckSchemaOnAnUnchangedScript(t *testing.T) {
	wsDir, _ := gateWithScript(t, scriptWith(`'met', 'evidence'`))
	out, err := runWF(t, wsDir, "check", "schema")
	noErr(t, err, "check schema")
	mustContain(t, out, "no change in what any schema requires")
}

// A workspace with no script at all is NOT A PASS. A decision gate has none and
// this check has nothing to say about it; a workspace whose script was renamed
// out of the extensions produces the same silence and a different meaning, and
// only the count separates them.
func TestCheckSchemaSaysSoWhenThereIsNoScript(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "schema")
	noErr(t, err, "check schema")
	mustContain(t, out, "NOT A PASS", "no .mjs/.js artifact")
}

// A script ADDED since the reference has no prior census. That is the first-
// review case `wf stats` names rather than counts: it is not a drop, and it is
// not a pass either.
func TestCheckSchemaNamesAScriptWithNoPriorCopy(t *testing.T) {
	wsDir := newGate(t)
	reviewedRound(t, wsDir, 1)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	art := filepath.Join(wsDir, "artifacts", "demo.mjs")
	if err := os.WriteFile(art, []byte(scriptWith(`'met'`)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "add", art); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "schema")
	noErr(t, err, "check schema")
	mustContain(t, out, "NOT A PASS", "no comparable copy")
}

// Inside a workspace the skeleton belongs where every other artifact lives. It
// used to land in the working directory — the workspace ROOT when the operator
// is standing in it — and a stray file there is exactly what `wf verify`
// refuses, so the command created the state the next command objects to.
func TestNewScriptWritesIntoArtifacts(t *testing.T) {
	wsDir := newGate(t)
	out, err := runWF(t, wsDir, "new-script", "slice-b")
	noErr(t, err, "new-script")
	dst := filepath.Join(wsDir, "artifacts", "slice-b.mjs")
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("the skeleton must land under artifacts/: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(wsDir, "slice-b.mjs")); err == nil {
		t.Fatal("the workspace root must stay clean — a stray file there is what `wf verify` refuses")
	}
	// It is not an artifact until it is adopted, and the command that put it
	// there is the one place that can say so.
	mustContain(t, out, "UNTRACKED", "wf add")
}

// A label that appears or disappears between the two revisions is a change in
// what the run BRIEFS, and it is reported without being a drop: a schema that
// stopped existing requires nothing, and calling that a drop would file the
// largest finding in the report over an agent nobody kept.
func TestCheckSchemaNamesANewLabel(t *testing.T) {
	wsDir, art := gateWithScript(t, scriptWith(`'met'`))
	// The second spawn goes BEFORE the return, or it never runs and the
	// fixture asserts nothing — the shape this whole runner exists against.
	extra := strings.Replace(scriptWith(`'met'`), "return { done:",
		`await spawn('second', { model: 'sonnet', effort: 'low', label: 'verify', schema: { type: 'object', required: ['ok'], properties: { ok: { type: 'boolean' } } } })
return { done:`, 1)
	if err := os.WriteFile(art, []byte(extra), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "schema")
	noErr(t, err, "check schema")
	mustContain(t, out, "verify: NEW")
	if strings.Contains(out, "DROP") {
		t.Fatalf("an added agent is not a drop:\n%s", out)
	}
}

// One side that does not SERIALIZE has no census, and comparing an unmeasured
// -1 against a real count would report the largest DROP in the file over a
// schema nothing measured — an absent measurement wearing a finding's clothes.
func TestCheckSchemaRefusesToCompareAnUnmeasuredSide(t *testing.T) {
	wsDir, art := gateWithScript(t, scriptWith(`'met', 'evidence'`))
	cyclic := `export const meta = { name: 'demo', description: 'd', phases: [{ title: 'P' }] }
const S = (() => { const o = { type: 'object', required: ['done'], properties: { done: { type: 'boolean' } } }; o.self = o; return o })()
async function spawn(p, o) { try { return await agent(p, o) } catch { return null } }
phase('P')
const r = await spawn('go', { model: 'sonnet', effort: 'low', label: 'accept', schema: S })
return { done: r !== null, dead: r ? [] : ['accept'] }
`
	if err := os.WriteFile(art, []byte(cyclic), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "schema")
	noErr(t, err, "check schema")
	mustContain(t, out, "NOT A PASS", "does not serialize")
	if strings.Contains(out, "DROP") {
		t.Fatalf("an unmeasured side is not a drop:\n%s", out)
	}
}

// A label that STOPPED being spawned is a change in what the run briefs, and it
// is named rather than counted as a drop: an agent nobody kept requires nothing,
// and calling that a drop files the largest finding in the report over a
// deletion the author made on purpose.
func TestCheckSchemaNamesAGoneLabel(t *testing.T) {
	two := strings.Replace(scriptWith(`'met'`), "return { done:",
		`await spawn('second', { model: 'sonnet', effort: 'low', label: 'verify', schema: { type: 'object', required: ['ok'], properties: { ok: { type: 'boolean' } } } })
return { done:`, 1)
	wsDir, art := gateWithScript(t, two)
	if err := os.WriteFile(art, []byte(scriptWith(`'met'`)), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "schema")
	noErr(t, err, "check schema")
	mustContain(t, out, "verify: GONE")
}

// The count of required keys can hold while MORE objects come to require none
// of theirs — a requirement moved from a leaf onto its wrapper keeps the total
// and leaves every leaf omissible. It is the same defect as a plain drop from
// the agent's side: the fields nobody requires are the fields nobody gets.
func TestCheckSchemaNamesMoreObjectsRequiringNothingAtAnUnchangedTotal(t *testing.T) {
	// Three required keys either way: `report` on the root, plus two on the
	// leaf — then all three on the root and none on the leaf. The total holds
	// and every leaf becomes omissible.
	body := func(rootReq, leafReq string) string {
		return `export const meta = { name: 'demo', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', required: [` + rootReq + `], properties: { report: { type: 'object', ` + leafReq + `properties: { met: { type: 'boolean' }, evidence: { type: 'string' } } } } }
async function spawn(p, o) { try { return await agent(p, o) } catch { return null } }
phase('P')
const r = await spawn('go', { model: 'sonnet', effort: 'low', label: 'accept', schema: S })
return { done: r !== null, dead: r ? [] : ['accept'] }
`
	}
	onLeaf := body(`'report'`, `required: ['met', 'evidence'], `)
	onWrapper := body(`'report', 'met', 'evidence'`, ``)
	wsDir, art := gateWithScript(t, onLeaf)
	if err := os.WriteFile(art, []byte(onWrapper), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runWF(t, wsDir, "check", "schema")
	if err == nil {
		t.Fatalf("an object that came to require none of its properties is a finding:\n%s", out)
	}
	mustContain(t, out, "DROP", "declare properties and require none")
}

// A typo in the reference used to print "no comparable copy at <ref>" for every
// artifact — which is the message for a script ADDED since the reference, so a
// typo read exactly like a first review of everything. `wf check closure -round
// 999` was the same class, and it was a blocker.
func TestCheckSchemaRefusesAReferenceThatDoesNotExist(t *testing.T) {
	wsDir, _ := gateWithScript(t, scriptWith(`'met'`))
	out, err := runWF(t, wsDir, "check", "schema", "-since", "gate-r999")
	if err == nil {
		t.Fatalf("a reference this workspace does not have is a refusal, not a report:\n%s", out)
	}
	if !strings.Contains(err.Error(), "gate-r999") {
		t.Fatalf("the refusal must name the reference, got %v\n%s", err, out)
	}
}

// A script DELETED since the reference is invisible if only the current
// artifacts are enumerated, and a gate that dropped one of two scripts would
// read as a gate whose remaining script did not change.
func TestCheckSchemaNamesAScriptDeletedSinceTheReference(t *testing.T) {
	wsDir, art := gateWithScript(t, scriptWith(`'met'`))
	second := filepath.Join(wsDir, "artifacts", "other.mjs")
	if err := os.WriteFile(second, []byte(scriptWith(`'met'`)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "add", second); err != nil {
		t.Fatal(err)
	}
	if _, err := runWF(t, wsDir, "round", "new", "-title", "second", "-kind", "delta"); err != nil {
		t.Fatal(err)
	}
	reviewedRound(t, wsDir, 2)
	if _, err := runWF(t, wsDir, "freeze"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(second); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wsDir, "commit", "-q", "-am", "drop the second script")
	_ = art
	out, err := runWF(t, wsDir, "check", "schema")
	noErr(t, err, "check schema")
	mustContain(t, out, "other.mjs: GONE")
}

// One label, two DIFFERENT schemas — a per-item briefing built in a loop.
// Keeping the first would compare whichever ordinal happened to run first
// against whichever ran first at the reference, so a requirement lost on the
// second spawn reads as unchanged. It is UNMEASURED, by name.
func TestCheckSchemaWillNotCompareALabelThatComposesTwoSchemas(t *testing.T) {
	body := `export const meta = { name: 'demo', description: 'd', phases: [{ title: 'P' }] }
async function spawn(p, o) { try { return await agent(p, o) } catch { return null } }
phase('P')
for (const n of [1, 2]) {
  await spawn('go ' + n, { model: 'sonnet', effort: 'low', label: 'worker', schema: n === 1
    ? { type: 'object', required: ['a'], properties: { a: { type: 'string' } } }
    : { type: 'object', required: ['a', 'b'], properties: { a: { type: 'string' }, b: { type: 'string' } } } })
}
return { done: true, dead: [] }
`
	wsDir, _ := gateWithScript(t, body)
	out, err := runWF(t, wsDir, "check", "schema")
	noErr(t, err, "check schema")
	mustContain(t, out, "NOT A PASS", "composes more than one schema")
}
