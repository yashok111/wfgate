package wfdry

import (
	"fmt"
	"strings"
	"testing"
)

// padded returns a schema literal whose serialization is at least n bytes, by
// giving one property a description of the right length. The width is asserted,
// never assumed: a fixture that misses the band it was written for tests the
// band next door.
func padded(n int) string {
	return fmt.Sprintf(`{ type: 'object', properties: { done: { type: 'boolean', description: '%s' } } }`,
		strings.Repeat("x", n))
}

func widthOf(t *testing.T, schema string) []SchemaWidth {
	t.Helper()
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = ` + schema + `
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
return { done: true }`
	r := run1(t, body, "clean")
	if r.Err != nil {
		t.Fatalf("the fixture must run: %v", r.Err)
	}
	return SchemaWidths([]Result{r})
}

// The width of what a call would put on the wire is measured at the SPAWN,
// because that is where a composed schema exists. Validated against a live
// gate's own history: the revision whose last agent was refused measures 4589
// here and the revision that then launched 15/15 measures 3877.
func TestSchemaWidth_isMeasuredPerCallAndGroupedWidestFirst(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const NARROW = { type: 'object', properties: { done: { type: 'boolean' } } }
const WIDE = ` + padded(200) + `
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(` + "`a`" + `, { model: 'sonnet', effort: 'low', label: 'one', phase: 'P', schema: NARROW })
await spawn(` + "`b`" + `, { model: 'sonnet', effort: 'low', label: 'two', phase: 'P', schema: WIDE })
await spawn(` + "`c`" + `, { model: 'sonnet', effort: 'low', label: 'three', phase: 'P', schema: NARROW })
return { done: true }`
	ws := SchemaWidths([]Result{run1(t, body, "clean")})
	if len(ws) != 2 {
		t.Fatalf("two distinct widths, got %+v", ws)
	}
	if ws[0].Clean <= ws[1].Clean {
		t.Fatalf("widest first: %+v", ws)
	}
	if got := strings.Join(ws[0].Labels, ","); got != "two" {
		t.Fatalf("the wide schema belongs to `two`, got %q", got)
	}
	if got := strings.Join(ws[1].Labels, ","); got != "one,three" {
		t.Fatalf("labels sharing a width group together, got %q", got)
	}
}

// The three bands, each with the corpus point it was drawn from. A band nobody
// can reach is a band nobody tests: the fixture asserts the width it produced
// before asserting the note.
func TestSchemaNotes_bandsAreDrawnFromTheCorpusPoints(t *testing.T) {
	cases := []struct {
		name string
		pad  int
		want string
	}{
		{"under the widest accepted", 10, ""},
		// The wanted substring must not appear in the OTHER band's note: the
		// unmeasured note names the refusal point too, so asserting on the word
		// REFUSED let a mutation that widened the refused band survive.
		{"in the unmeasured gap", SchemaAccepted - 60, "That gap is UNMEASURED"},
		{"at or above the narrowest refusal", SchemaRefused + 40, "at or above"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws := widthOf(t, padded(c.pad))
			if len(ws) != 1 {
				t.Fatalf("one schema, got %+v", ws)
			}
			b := ws[0].Clean
			switch c.name {
			case "under the widest accepted":
				if b > SchemaAccepted {
					t.Fatalf("fixture must land under %d, got %d", SchemaAccepted, b)
				}
			case "in the unmeasured gap":
				if b <= SchemaAccepted || b >= SchemaRefused {
					t.Fatalf("fixture must land inside (%d,%d), got %d", SchemaAccepted, SchemaRefused, b)
				}
			default:
				if b < SchemaRefused {
					t.Fatalf("fixture must land at or above %d, got %d", SchemaRefused, b)
				}
			}
			notes := SchemaNotes(ws)
			if c.want == "" {
				if len(notes) != 0 {
					t.Fatalf("a width the corpus has seen accepted says nothing: %v", notes)
				}
				return
			}
			if len(notes) != 1 || !strings.Contains(notes[0], c.want) {
				t.Fatalf("want a note containing %q, got %v", c.want, notes)
			}
			if !strings.Contains(notes[0], "PROBE") && !strings.Contains(notes[0], "probe") {
				t.Fatalf("the note must name the probe, which is the only thing that answers it: %v", notes)
			}
		})
	}
}

// A schema that does not serialize is not a schema of zero bytes. Without the
// -1 branch it grouped with everything narrow and read as the safest width in
// the run.
func TestSchemaWidth_anUnserialisableSchemaSaysSoRatherThanReadingAsNarrow(t *testing.T) {
	ws := widthOf(t, `{ type: 'object', properties: { done: { type: 'boolean' } }, self: undefined, fn: () => 1 }`)
	// A function and an undefined are DROPPED by JSON.stringify, so that one
	// still serializes — the honest unserialisable case is a cycle.
	if len(ws) != 1 || ws[0].Clean <= 0 {
		t.Fatalf("a schema with a function still serializes: %+v", ws)
	}
}

// A schema that does not serialize is not a schema of zero bytes and it is not a
// width question: it is a launch that cannot happen, so it FAILS the mode. A
// WARN there would leave `7/7 modes completed` and exit 0 standing over it,
// which is the shape the collapse check was promoted out of one wave earlier.
func TestSchemaWidth_anUnserialisableSchemaFailsTheMode(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = (() => { const o = { type: 'object', properties: { done: { type: 'boolean' } } }; o.self = o; return o })()
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
return { done: true }`
	r := run1(t, body, "clean")
	wantViolation(t, r, "does not serialize")
	if r.OK() {
		t.Fatalf("a schema the launch cannot send must fail the mode: %+v", r)
	}
}

// The vocabulary the matcher compiles and the vocabulary the note prints are
// one list. A message quoting a vocabulary it does not share is the drift this
// project bans everywhere else.
func TestPassBoolVocabulary_isTheListTheMatcherCompiles(t *testing.T) {
	v := passBoolVocabulary()
	for _, w := range passBoolWords {
		if !strings.Contains(v, w) {
			t.Fatalf("the note omits the completion word %q: %s", w, v)
		}
		if !passBools.MatchString(w) {
			t.Fatalf("the matcher does not accept the word the note advertises: %q", w)
		}
	}
	for _, p := range passBoolPrefixes {
		if !strings.Contains(v, p) {
			t.Fatalf("the note omits the prefix %q: %s", p, v)
		}
	}
}

// Three field names were burned finding this list by reading the Go source. The
// note now names the rule, not just the field.
func TestUnrecognisedBooleanNote_namesTheNameContractAndTheEscape(t *testing.T) {
	r := run1(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { smokeAchieved: { type: 'boolean' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const a = await spawn(`+"`go`"+`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
return { done: Boolean(a.smokeAchieved) }`, "clean")
	joined := strings.Join(r.Notes, "\n")
	for _, want := range []string{"smokeAchieved", "by its NAME", "x-pass", "satisfied", "criterion"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the note must carry %q: %s", want, joined)
		}
	}
}

// `x-pass` lets a script name a boolean after its subject instead of after this
// file's matcher. Without it, `smokeAchieved` was FALSE in every mode and the
// branch behind it was executed by none.
func TestXPass_declaresPolarityForABooleanNoMatcherRecognises(t *testing.T) {
	body := func(pol string) string {
		return `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { smokeAchieved: { type: 'boolean', 'x-pass': ` + pol + ` } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const a = await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
return { done: Boolean(a.smokeAchieved) }`
	}
	r := run1(t, body("true"), "clean")
	if m, _ := r.Return.(map[string]any); !m["done"].(bool) {
		t.Fatalf("`x-pass: true` is pass-shaped on the happy path: %+v", r.Return)
	}
	if strings.Contains(strings.Join(r.Notes, "\n"), "unrecognised report field") {
		t.Fatalf("a field that DECLARED its polarity is not unrecognised: %v", r.Notes)
	}
	f := run1(t, body("false"), "clean")
	if m, _ := f.Return.(map[string]any); m["done"].(bool) {
		t.Fatalf("`x-pass: false` is a failure flag and is false where nothing failed: %+v", f.Return)
	}
	// And the polarity is REAL, not a synonym for the name matcher: in the
	// failing fixture the failure flag is the one that goes true and the pass
	// flag is the one that goes false. Without the second half the annotation
	// could return true in every mode and no bar would notice — which is the
	// "the whole fix-round path was executed by no mode" defect re-entering
	// through the new escape hatch.
	af := run1(t, body("false"), "acceptfail")
	if m, _ := af.Return.(map[string]any); !m["done"].(bool) {
		t.Fatalf("`x-pass: false` is true in a failing mode: %+v", af.Return)
	}
	ap := run1(t, body("true"), "acceptfail")
	if m, _ := ap.Return.(map[string]any); m["done"].(bool) {
		t.Fatalf("`x-pass: true` is FALSE where acceptance failed: %+v", ap.Return)
	}
}

// A label is taken at its WIDEST across modes, because the launch sends the
// wide one: a schema assembled on a branch is narrower in the mode that skipped
// the branch. The `no-args` mode is that branch here.
func TestSchemaWidth_takesALabelAtItsWidestAcrossModes(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = (n) => ({ type: 'object', properties: { done: { type: 'boolean', description: 'x'.repeat(n) } } })
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S(args ? 400 : 1) })
return { done: true }`
	rs, err := Run(script(t, body), []string{"no-args", "clean"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var narrow, wide int
	for _, r := range rs {
		for _, s := range r.Schemas {
			if r.Mode == "no-args" {
				narrow = s.Bytes
			} else {
				wide = s.Bytes
			}
		}
	}
	if narrow == 0 || wide <= narrow {
		t.Fatalf("the fixture must be narrower without args: narrow=%d wide=%d", narrow, wide)
	}
	ws := SchemaWidths(rs)
	if len(ws) != 1 || ws[0].Widest != wide || ws[0].Clean != wide {
		t.Fatalf("one label, widest %d and clean %d, got %+v", wide, wide, ws)
	}
	// And CLEAN is the clean mode's own width, not whichever mode ran last: the
	// corpus points are clean-mode measurements, and comparing them against
	// another mode's number is the defect this whole calibration was corrected
	// for. Here the happy path is the WIDE one, so a "last mode wins" reading
	// reports the narrow one.
	rs2, err := Run(script(t, body), []string{"clean", "no-args"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ws2 := SchemaWidths(rs2)
	if len(ws2) != 1 || ws2[0].Clean != wide {
		t.Fatalf("clean is the CLEAN mode's width (%d), got %+v", wide, ws2)
	}
}

// The two boundaries, tested on the pure function rather than through a fixture:
// no padded schema lands on an exact constant, so `>=` and `>` were
// interchangeable and both boundary guards were unproven.
func TestSchemaNotes_boundariesAreInclusiveWhereTheCorpusIs(t *testing.T) {
	cases := []struct {
		name string
		w    SchemaWidth
		want string
	}{
		{"exactly the narrowest refusal", SchemaWidth{Clean: SchemaRefused, Widest: SchemaRefused}, "at or above"},
		{"one byte under it", SchemaWidth{Clean: SchemaRefused - 1, Widest: SchemaRefused - 1}, "That gap is UNMEASURED"},
		{"exactly the widest acceptance", SchemaWidth{Clean: SchemaAccepted, Widest: SchemaAccepted}, ""},
		{"one byte over it", SchemaWidth{Clean: SchemaAccepted + 1, Widest: SchemaAccepted + 1}, "That gap is UNMEASURED"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			notes := SchemaNotes([]SchemaWidth{c.w})
			if c.want == "" {
				if len(notes) != 0 {
					t.Fatalf("a width the corpus has seen accepted says nothing: %v", notes)
				}
				return
			}
			if len(notes) != 1 || !strings.Contains(notes[0], c.want) {
				t.Fatalf("want %q, got %v", c.want, notes)
			}
		})
	}
}

// A wide FAILURE branch is its own statement and never evidence about the width
// the corpus recorded. Measured on the P3a script that ran 44 of 44 agents with
// zero errors: `verify` composes 3,435 bytes on the happy path and 5,326 on the
// failure branches, and comparing the WIDEST against constants measured as CLEAN
// widths made this tool warn that a schema the launch accepted had been refused.
func TestSchemaNotes_aWideFailureBranchIsItsOwnStatement(t *testing.T) {
	notes := SchemaNotes([]SchemaWidth{{Clean: 3435, Widest: 5326, Labels: []string{"verify"}}})
	if len(notes) != 1 {
		t.Fatalf("the happy path is under the refusal, so exactly one note: %v", notes)
	}
	if !strings.Contains(notes[0], "on a FAILURE branch") || !strings.Contains(notes[0], "never rehearses") {
		t.Fatalf("the note must scope itself to the branch: %v", notes)
	}
	if strings.Contains(notes[0], "on the happy path,") {
		t.Fatalf("this must NOT read as a claim about the width the corpus recorded: %v", notes)
	}
	// And a happy path already over the line does not get the branch note twice.
	both := SchemaNotes([]SchemaWidth{{Clean: SchemaRefused + 1, Widest: SchemaRefused + 2, Labels: []string{"w"}}})
	if len(both) != 2 {
		t.Fatalf("a wide happy path AND a wider branch are two facts: %v", both)
	}
}

// The width is the number a refusal quotes, so it is an absolute count and not a
// relative one: nothing else pinned it, and an off-by-one in the serializer
// would have moved every reported width with every bar green.
func TestSerializedWidth_isTheExactLengthOfTheSerializedSchema(t *testing.T) {
	// JSON.stringify of the literal below, in source order, is exactly:
	// {"type":"object","properties":{"done":{"type":"boolean"}}}
	const want = 58
	ws := widthOf(t, `{ type: 'object', properties: { done: { type: 'boolean' } } }`)
	if len(ws) != 1 || ws[0].Clean != want {
		t.Fatalf("want exactly %d bytes, got %+v", want, ws)
	}
}

// An `x-pass` the runner cannot read is not an `x-pass`. The author reaching for
// the escape hatch after reading the WARN would otherwise get the same WARN
// again, with nothing saying their spelling was seen and rejected.
func TestXPass_aNonBooleanAnnotationIsNamedRatherThanIgnored(t *testing.T) {
	r := run1(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { smokeAchieved: { type: 'boolean', 'x-pass': 'true' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const a = await spawn(`+"`go`"+`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
return { done: Boolean(a.smokeAchieved) }`, "clean")
	joined := strings.Join(r.Notes, "\n")
	if !strings.Contains(joined, "x-pass on smokeAchieved is not a boolean") {
		t.Fatalf("a rejected spelling must be named: %s", joined)
	}
}

// A synthetic label is an ORDINAL, and an ordinal is per mode: two different
// call sites are both `agent@1` in the two modes that reach them, so merging
// across modes attributes one width to two calls. `wfcheck` already WARNs on the
// missing `label:` that produces one; this runner must not compound it.
func TestSchemaWidths_syntheticLabelsAreNotMergedAcrossModes(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const WIDE = ` + padded(300) + `
const NARROW = { type: 'object', properties: { done: { type: 'boolean' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
if (args) { await spawn(` + "`with args`" + `, { model: 'sonnet', effort: 'low', phase: 'P', schema: WIDE }) }
else { await spawn(` + "`without`" + `, { model: 'sonnet', effort: 'low', phase: 'P', schema: NARROW }) }
return { done: true }`
	rs, err := Run(script(t, body), []string{"clean", "no-args"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ws := SchemaWidths(rs)
	if len(ws) != 2 {
		t.Fatalf("two different call sites, both `agent@1`, are two entries: %+v", ws)
	}
	for _, w := range ws {
		if len(w.Labels) != 1 || !strings.Contains(w.Labels[0], "agent@") {
			t.Fatalf("each entry names its own mode's call: %+v", ws)
		}
		if !strings.HasPrefix(w.Labels[0], "clean ") && !strings.HasPrefix(w.Labels[0], "no-args ") {
			t.Fatalf("a synthetic label must say which mode it came from: %+v", ws)
		}
	}
}
