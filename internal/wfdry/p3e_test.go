package wfdry

import (
	"encoding/json"
	"strings"
	"testing"
)

// The shape a script reaches for when the width classifier pushes it to declare
// a repeated leaf once: the `$defs` and the leaf are written TOGETHER in one
// constant, and the constant is then nested under `properties`. The pointer is
// anchored at the schema root, so it does not resolve — and the note used to say
// only `"$defs" is not in the schema`, which names the failure and not the fix.
//
// Measured on the P3e gate, which read this as the whole feature being broken,
// reverted to twenty-two inlined copies of the leaf and paid about 1,900 bytes
// of width for it against a classifier with a measured refusal at 4,233. The
// root-anchored spelling resolved then and resolves now (the test below it), so
// what was owed was the pointer that WOULD work.
func TestUnresolvedDefsNamesThePointerThatWouldWork(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const LEAF = { type: 'object', $defs: { prop: { type: 'object', required: ['met'], properties: { met: { type: 'boolean' } } } },
  properties: { P1: { $ref: '#/$defs/prop' } } }
const S = { type: 'object', properties: { report: LEAF, done: { type: 'boolean' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const r = await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: typeof r.report.P1 === 'object' }`
	r := run1(t, body, "clean")
	joined := strings.Join(r.Notes, "\n")
	if !strings.Contains(joined, "#/properties/report/$defs/prop") {
		t.Fatalf("the note must name the pointer that resolves, got: %s", joined)
	}
	if !strings.Contains(joined, "anchored at the schema root") {
		t.Fatalf("the note must say WHY the written one does not, got: %s", joined)
	}
}

// The same leaf declared at the schema ROOT and referenced from keys built by
// `Object.fromEntries` — the exact narrowing the corpus recommends. It fills in
// every mode, and it did before this wave too: the P3e report that it does not
// is refuted here rather than repeated.
func TestRootDefsReferencedFromComputedKeysFills(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const NAMES = ['P1', 'P8a', 'P14']
const S = { type: 'object',
  $defs: { prop: { type: 'object', required: ['met', 'evidence'], properties: { met: { type: 'boolean' }, evidence: { type: 'string' } } } },
  required: ['props'],
  properties: { props: { type: 'object', required: NAMES, properties: Object.fromEntries(NAMES.map((n) => [n, { $ref: '#/$defs/prop' }])) } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const r = await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: NAMES.every((n) => r.props[n].met === true) }`
	r := run1(t, body, "clean")
	if len(r.Violations) > 0 || !r.ReturnOK {
		t.Fatalf("a root `$defs` referenced from computed keys must fill: %v %+v", r.Violations, r.Return)
	}
	if joined := strings.Join(r.Notes, "\n"); strings.Contains(joined, "unresolved $ref") {
		t.Fatalf("nothing is unresolved here: %s", joined)
	}
}

// The census is what makes a `required` DROP a changed number rather than a
// silent one. It is counted over the EXPANDED schema, because the obligation is
// per USE: the P3e report declared its leaf once and required two keys of it at
// twenty-two keys, and pricing the two spellings differently would move the
// number on an edit that is only about narrowing.
func TestRequiredCensusCountsPerUseAndNamesObjectsRequiringNothing(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const NAMES = ['P1', 'P2', 'P3']
const S = { type: 'object',
  $defs: { prop: { type: 'object', required: ['met', 'evidence'], properties: { met: { type: 'boolean' }, evidence: { type: 'string' } } } },
  required: ['props'],
  properties: { props: { type: 'object', properties: Object.fromEntries(NAMES.map((n) => [n, { $ref: '#/$defs/prop' }])) } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true }`
	r := run1(t, body, "clean")
	if len(r.Schemas) != 1 {
		t.Fatalf("one schema, got %+v", r.Schemas)
	}
	// 1 (`props`) + 3 uses x 2 keys of the shared leaf.
	if got := r.Schemas[0].Required; got != 7 {
		t.Fatalf("the census counts a shared leaf once per USE: want 7, got %d", got)
	}
	// `props` itself declares three properties and requires none of them —
	// which is exactly what the P3e edit did to twenty-two leaves.
	if got := r.Schemas[0].NoRequired; got != 1 {
		t.Fatalf("want 1 object declaring properties and requiring none, got %d", got)
	}
}

// The other half of the same census: dropping the leaf's `required` moves both
// numbers, and it is the only signal there is. Validated against the live
// workspace this came from — P3e's `accept` schema goes 74 required keys and 1
// such object at its round-10 freeze to 33 and 23 at round 11, inside an edit
// whose stated purpose was width.
func TestRequiredCensusMovesWhenALeafStopsRequiring(t *testing.T) {
	leaf := func(req string) string {
		return `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const NAMES = ['P1', 'P2', 'P3']
const S = { type: 'object',
  $defs: { prop: { type: 'object', ` + req + ` properties: { met: { type: 'boolean' }, evidence: { type: 'string' } } } },
  properties: { props: { type: 'object', required: NAMES, properties: Object.fromEntries(NAMES.map((n) => [n, { $ref: '#/$defs/prop' }])) } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true }`
	}
	before := run1(t, leaf(`required: ['met', 'evidence'],`), "clean").Schemas[0]
	after := run1(t, leaf(``), "clean").Schemas[0]
	if after.Required >= before.Required {
		t.Fatalf("dropping a leaf's `required` must lower the census: %d -> %d", before.Required, after.Required)
	}
	if after.NoRequired <= before.NoRequired {
		t.Fatalf("the leaves now require nothing and must be counted: %d -> %d", before.NoRequired, after.NoRequired)
	}
}

// The fixture is recorded once per LABEL and it is the object the stub actually
// returned. Three of P3e's dry-run reds were fixture-synthesis limits reported
// as artifact defects, and each was one look at this object away.
func TestFixtureIsRecordedPerLabel(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { finalCount: { type: 'integer' }, met: { type: 'boolean' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
for (const i of [1, 2, 3]) await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true }`
	r := run1(t, body, "clean")
	if len(r.Fixtures) != 1 || r.Fixtures[0].Label != "w" {
		t.Fatalf("one entry per label, got %+v", r.Fixtures)
	}
	b, _ := json.Marshal(r.Fixtures[0].Value)
	if !strings.Contains(string(b), `"finalCount"`) {
		t.Fatalf("the fixture is the object the stub returned, got %s", b)
	}
	if got := Fixtures(r, []string{"w"}); len(got) != 1 {
		t.Fatalf("Fixtures must find a label this mode composed, got %+v", got)
	}
	if got := Fixtures(r, []string{"nobody"}); len(got) != 0 {
		t.Fatalf("a label no mode composed is silence here, got %+v", got)
	}
}

// The clean mode's verdict violation states a fact about the SCRIPT. On P3e it
// was three times a fact about the RUNNER instead, and nothing in the message
// offered the one object that separates them.
func TestCleanVerdictViolationPointsAtTheFixture(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { finalCount: { type: 'integer' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const r = await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: r.finalCount === 22 }`
	r := run1(t, body, "clean")
	wantViolation(t, r, "--fixture <label>")
}

// The honest unserialisable schema is a CYCLE, and a census walking one does
// not return. It is unmeasured rather than zero: `-1` and `0` are different
// claims, and the second would print "requires nothing" over a schema nobody
// measured.
func TestCensusIsNotTakenOverASchemaThatDoesNotSerialize(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = (() => { const o = { type: 'object', required: ['done'], properties: { done: { type: 'boolean' } } }; o.self = o; return o })()
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true }`
	r := run1(t, body, "clean")
	if len(r.Schemas) != 1 {
		t.Fatalf("one schema, got %+v", r.Schemas)
	}
	if r.Schemas[0].Required != -1 || r.Schemas[0].NoRequired != -1 {
		t.Fatalf("an unserialisable schema has NO census, got %+v", r.Schemas[0])
	}
}

// An object declaring NO properties requires none of nothing, which is not the
// shape this census is about. Counting it would report every enum leaf and
// every `additionalProperties: false` stub as an obligation nobody stated.
func TestObjectWithoutPropertiesIsNotCountedAsRequiringNone(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', required: ['a'], properties: { a: { type: 'string' }, empty: { type: 'object', properties: {} } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true }`
	r := run1(t, body, "clean")
	if got := r.Schemas[0].NoRequired; got != 0 {
		t.Fatalf("an object with no properties is not counted, got %d", got)
	}
}

// The census that travels with a width is the CLEAN one. A failure branch may
// compose a different schema, and the corpus's numbers are the happy path's:
// mixing the two would price one label in two runs' quantities.
func TestSchemaWidthCarriesTheCleanCensus(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const NARROW = { type: 'object', required: ['done'], properties: { done: { type: 'boolean' } } }
const WIDE = { type: 'object', required: ['done', 'why', 'where'], properties: { done: { type: 'boolean' }, why: { type: 'string' }, where: { type: 'string' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const first = await spawn(` + "`a`" + `, { model: 'sonnet', effort: 'low', label: 'one', schema: { type: 'object', properties: { blockers: { type: 'array' } } } })
const failed = first.blockers.length > 0
await spawn(` + "`b`" + `, { model: 'sonnet', effort: 'low', label: 'two', schema: failed ? WIDE : NARROW })
return { done: true }`
	// The failure mode runs FIRST, deliberately: the census travels with the
	// width, and taking whichever was seen first would be right by accident
	// whenever `clean` happens to lead.
	rs, err := Run(script(t, body), []string{"blockers", "clean"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range SchemaWidths(rs) {
		for _, l := range w.Labels {
			if l == "two" && w.Required != 1 {
				t.Fatalf("`two` requires 1 key on the happy path, the census says %d (%+v)", w.Required, w)
			}
		}
	}
}

// A `$ref` that did not expand leaves the referenced leaf a placeholder, so its
// `required` is not in the walk and the census UNDERCOUNTS — silently, and by
// exactly the amount the author cares about. A partial count printed as a
// measurement is the class this whole file exists against.
func TestCensusIsUnmeasuredWhenARefDidNotExpand(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const LEAF = { type: 'object', $defs: { prop: { type: 'object', required: ['met', 'evidence'], properties: { met: { type: 'boolean' }, evidence: { type: 'string' } } } },
  required: ['P1'], properties: { P1: { $ref: '#/$defs/prop' } } }
const S = { type: 'object', required: ['report'], properties: { report: LEAF } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true }`
	r := run1(t, body, "clean")
	if got := r.Schemas[0].Required; got != -1 {
		t.Fatalf("a schema with an unexpanded $ref has NO census, got %d", got)
	}
}

// `allOf`/`anyOf`/`oneOf` hold their subschemas in an ARRAY, so a locator that
// stops at the first array misses the composition keywords a large schema is
// most likely to use.
func TestDefsHintReachesThroughAnArray(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { report: { $ref: '#/$defs/prop' } },
  allOf: [{ $defs: { prop: { type: 'object', properties: { met: { type: 'boolean' } } } } }] }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true }`
	r := run1(t, body, "clean")
	if joined := strings.Join(r.Notes, "\n"); !strings.Contains(joined, "#/allOf/0/$defs/prop") {
		t.Fatalf("the hint must index into an array: %s", joined)
	}
}

// The emitted pointer is the whole value of the hint, so it must RESOLVE. RFC
// 6901 spells a tilde `~0` and a slash `~1`, in that order, and an unescaped
// segment hands the author a pointer that does not work — worse than no hint.
func TestDefsHintEscapesThePointerItEmits(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { 'a/b~c': { $defs: { prop: { type: 'object', properties: { met: { type: 'boolean' } } } },
  properties: { P1: { $ref: '#/$defs/prop' } } } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true }`
	r := run1(t, body, "clean")
	joined := strings.Join(r.Notes, "\n")
	if !strings.Contains(joined, "#/properties/a~1b~0c/$defs/prop") {
		t.Fatalf("the emitted pointer must be RFC 6901-escaped: %s", joined)
	}
}

// A label whose schema is built per item composes DIFFERENT fixtures, and
// keeping only the first hides exactly the case the flag exists for behind
// whichever ordinal happened to run first. Identical ones still collapse.
func TestFixtureKeepsDistinctShapesForOneLabel(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
for (const n of [1, 2, 2]) {
  await spawn('go ' + n, { model: 'sonnet', effort: 'low', label: 'w', schema: n === 1
    ? { type: 'object', properties: { alpha: { type: 'string' } } }
    : { type: 'object', properties: { beta: { type: 'string' } } } })
}
return { done: true }`
	r := run1(t, body, "clean")
	if len(r.Fixtures) != 2 {
		t.Fatalf("two distinct shapes, three spawns: got %d entries %+v", len(r.Fixtures), r.Fixtures)
	}
	if got := Fixtures(r, []string{"w"}); len(got) != 2 {
		t.Fatalf("both shapes must be reachable by label, got %+v", got)
	}
}

// `vm.ToValue` hands goja a WRITE-THROUGH view of the same Go map, so every
// assignment the script makes to an agent's result lands in the object this
// list holds. Without a deep copy the flag printed the SCRIPT's own writes
// under the heading "the object the stub returned" — the exact confusion it
// exists to remove — and the second, unmutated spawn then deduped against the
// first's pre-mutation bytes, so the real fixture appeared nowhere.
func TestFixtureIsDetachedFromWhatTheScriptWrites(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const S = { type: 'object', properties: { note: { type: 'string' } } }
const a = await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
a.note = 'THE SCRIPT OVERWROTE THIS'
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true }`
	r := run1(t, body, "clean")
	for _, f := range r.Fixtures {
		m, _ := f.Value.(map[string]any)
		if m != nil && m["note"] == "THE SCRIPT OVERWROTE THIS" {
			t.Fatalf("the fixture must be what the STUB returned, got the script's write: %+v", r.Fixtures)
		}
	}
	if len(r.Fixtures) != 1 {
		t.Fatalf("two identical stub returns collapse to one entry, got %d: %+v", len(r.Fixtures), r.Fixtures)
	}
}

// A schema that is not an object at all — `schema: ['a']` — SERIALIZES, so the
// serialization guard does not cover it and the census runs. Returning zero
// there would print "0 key(s) required" over a schema nobody measured, and
// `wf check schema` would compare a fabricated zero against a real count.
func TestCensusOfANonObjectSchemaIsUnmeasured(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: ['a'] })
return { done: true }`
	r := run1(t, body, "clean")
	if len(r.Schemas) != 1 || r.Schemas[0].Required != -1 || r.Schemas[0].NoRequired != -1 {
		t.Fatalf("a non-object schema has NO census, got %+v", r.Schemas)
	}
}

// A label composed by NO clean mode has a FAILURE branch's census, and the two
// print typographically identically unless the group says so. `*:fix` labels of
// this shape exist on real gated scripts.
func TestSchemaWidthMarksACensusNoCleanModeComposed(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const r = await spawn(` + "`a`" + `, { model: 'sonnet', effort: 'low', label: 'review', schema: { type: 'object', required: ['blockers'], properties: { blockers: { type: 'array' } } } })
if (r.blockers.length > 0) {
  await spawn(` + "`b`" + `, { model: 'sonnet', effort: 'low', label: 'fix', schema: { type: 'object', required: ['a', 'b', 'c'], properties: { a: { type: 'string' }, b: { type: 'string' }, c: { type: 'string' } } } })
}
return { done: true }`
	rs, err := Run(script(t, body), []string{"clean", "blockers"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, w := range SchemaWidths(rs) {
		for _, l := range w.Labels {
			switch l {
			case "fix":
				seen = true
				if !w.CleanMissing {
					t.Fatalf("`fix` is composed by no clean mode and must say so: %+v", w)
				}
			case "review":
				if w.CleanMissing {
					t.Fatalf("`review` IS composed by the clean mode: %+v", w)
				}
			}
		}
	}
	if !seen {
		t.Fatal("the failure-branch label was never composed; the fixture proves nothing")
	}
}

// Go randomises map order, so a schema declaring the same `$defs` name at two
// sites would otherwise get a nondeterministic pointer — and the whole value of
// the hint is that it is a patch the author can paste.
func TestDefsHintIsDeterministicAcrossTwoCandidateSites(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const LEAF = { type: 'object', properties: { met: { type: 'boolean' } } }
const S = { type: 'object', properties: {
  aaa: { $defs: { prop: LEAF }, properties: { x: { type: 'string' } } },
  zzz: { $defs: { prop: LEAF }, properties: { y: { type: 'string' } } },
  report: { $ref: '#/$defs/prop' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true }`
	for i := 0; i < 8; i++ {
		r := run1(t, body, "clean")
		if joined := strings.Join(r.Notes, "\n"); !strings.Contains(joined, "#/properties/aaa/$defs/prop") {
			t.Fatalf("run %d picked a different site: %s", i, joined)
		}
	}
}

// `--fixture "a, b"` is what an operator types. The labels are trimmed, and
// nothing else pinned it.
func TestFixturesTrimsTheLabelsItIsGiven(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', schema: { type: 'object', properties: { a: { type: 'string' } } } })
return { done: true }`
	r := run1(t, body, "clean")
	if got := Fixtures(r, []string{"  w  "}); len(got) != 1 {
		t.Fatalf("a label written with spaces must resolve, got %+v", got)
	}
}

// Two labels share a schema LINE only when their schemas agree on the width AND
// on the census: equal byte counts do not make two schemas the same schema, and
// merging them would attribute one census to two different obligations.
func TestSchemaWidthsDoNotMergeEqualWidthsWithDifferentCensuses(t *testing.T) {
	// Both serialize to the SAME length — the one-character description pays
	// for the `"required":["ab"],` the other carries — so the only thing that
	// can separate them in the report is the census.
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const A = { type: 'object', required: ['ab'], properties: { ab: { type: 'string' } } }
const B = { type: 'object', properties: { ab: { type: 'string', description: 'x' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(` + "`a`" + `, { model: 'sonnet', effort: 'low', label: 'one', schema: A })
await spawn(` + "`b`" + `, { model: 'sonnet', effort: 'low', label: 'two', schema: B })
return { done: true }`
	r := run1(t, body, "clean")
	// A skipping test proves nothing: the equal width is the PREMISE and is
	// asserted, not hoped for.
	if len(r.Schemas) != 2 || r.Schemas[0].Bytes != r.Schemas[1].Bytes {
		t.Fatalf("the fixture must put two schemas at ONE width or it tests nothing: %+v", r.Schemas)
	}
	if ws := SchemaWidths([]Result{r}); len(ws) != 2 {
		t.Fatalf("equal widths with different censuses are two lines, got %+v", ws)
	}
}
