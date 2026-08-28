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
	if ws[0].Bytes <= ws[1].Bytes {
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
		{"in the unmeasured band", SchemaAccepted - 60, "That band is UNMEASURED"},
		{"at or above the narrowest refusal", SchemaRefused + 40, "at or above"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws := widthOf(t, padded(c.pad))
			if len(ws) != 1 {
				t.Fatalf("one schema, got %+v", ws)
			}
			b := ws[0].Bytes
			switch c.name {
			case "under the widest accepted":
				if b > SchemaAccepted {
					t.Fatalf("fixture must land under %d, got %d", SchemaAccepted, b)
				}
			case "in the unmeasured band":
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
	if len(ws) != 1 || ws[0].Bytes <= 0 {
		t.Fatalf("a schema with a function still serializes: %+v", ws)
	}
	cyc := widthOf(t, `(() => { const o = { type: 'object', properties: { done: { type: 'boolean' } } }; o.self = o; return o })()`)
	if len(cyc) != 1 || cyc[0].Bytes != -1 {
		t.Fatalf("a cyclic schema does not serialize: %+v", cyc)
	}
	notes := SchemaNotes(cyc)
	if len(notes) != 1 || !strings.Contains(notes[0], "does not serialize") {
		t.Fatalf("want the unserialisable note, got %v", notes)
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
	for _, want := range []string{"smokeAchieved", "by its NAME", "x-pass"} {
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
	// failing fixture the failure flag is the one that goes true.
	af := run1(t, body("false"), "acceptfail")
	if m, _ := af.Return.(map[string]any); !m["done"].(bool) {
		t.Fatalf("`x-pass: false` is true in a failing mode: %+v", af.Return)
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
	if len(ws) != 1 || ws[0].Bytes != wide {
		t.Fatalf("one label at its widest (%d), got %+v", wide, ws)
	}
}
