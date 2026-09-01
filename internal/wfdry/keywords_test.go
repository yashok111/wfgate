package wfdry

import (
	"encoding/json"
	"strings"
	"testing"
)

// refused is refusedKeywords over a JSON literal, which is the form the launch
// receives and therefore the form the check is about.
func refused(t *testing.T, js string) []string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(js), &v); err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	return refusedKeywords(v)
}

// The keyword this whole check exists for, at both the places the manual used
// to prescribe it.
func TestRefusedKeywords_findsTheAnnotationTheLaunchRefuses(t *testing.T) {
	for _, c := range []struct{ name, js string }{
		{"on a boolean", `{"type":"object","properties":{"clean":{"type":"boolean","x-pass":true}}}`},
		{"on an array", `{"type":"object","properties":{"items":{"type":"array","x-pass":false}}}`},
		{"on the root", `{"type":"object","x-pass":true,"properties":{}}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := refused(t, c.js); len(got) != 1 || got[0] != "x-pass" {
				t.Fatalf("want [x-pass], got %v", got)
			}
		})
	}
}

// The refused class is UNKNOWN KEYWORD and not the `x-` prefix — measured, and
// the probe that says so is the reason this is an allowlist. A check that only
// caught `x-` would pass `passPolarity`, which the launch refuses identically.
func TestRefusedKeywords_isAboutTheVocabularyAndNotThePrefix(t *testing.T) {
	got := refused(t, `{"type":"object","properties":{"clean":{"type":"boolean","passPolarity":true}}}`)
	if len(got) != 1 || got[0] != "passPolarity" {
		t.Fatalf("want [passPolarity], got %v", got)
	}
}

// The structural walk's whole reason to exist: under `properties` the keys are
// FIELD NAMES. A report field legitimately called `x-pass` — or anything else —
// is not a keyword, and a generic descent would refuse the schema over it.
func TestRefusedKeywords_aFieldNameIsNotAKeyword(t *testing.T) {
	for _, js := range []string{
		`{"type":"object","properties":{"x-pass":{"type":"boolean"},"passPolarity":{"type":"string"}}}`,
		`{"type":"object","patternProperties":{"^x-pass$":{"type":"boolean"}}}`,
		`{"$defs":{"x-pass":{"type":"string"}},"type":"object","properties":{"a":{"$ref":"#/$defs/x-pass"}}}`,
	} {
		if got := refused(t, js); len(got) != 0 {
			t.Fatalf("a name is not a keyword: %v in %s", got, js)
		}
	}
}

// The other half of the same rule: under `enum`, `const`, `default` and
// `examples` the value is DATA the agent may return, and its keys are not
// keywords either.
func TestRefusedKeywords_dataIsNotWalked(t *testing.T) {
	for _, js := range []string{
		`{"type":"object","properties":{"a":{"const":{"x-pass":1}}}}`,
		`{"type":"object","properties":{"a":{"enum":[{"passPolarity":2}]}}}`,
		`{"type":"object","properties":{"a":{"type":"object","default":{"whatever":3}}}}`,
		`{"type":"object","properties":{"a":{"type":"string","examples":[{"nope":4}]}}}`,
	} {
		if got := refused(t, js); len(got) != 0 {
			t.Fatalf("data is not schema: %v in %s", got, js)
		}
	}
}

// A keyword buried in a `$defs` leaf refuses the schema exactly as one at the
// root does — the validator walks the whole document, so the check must too.
// Without the descent through `$defs` and `items`, the one place this corpus
// actually composes its leaves would be the one place unchecked.
func TestRefusedKeywords_reachesNestedSchemaNodes(t *testing.T) {
	js := `{"$defs":{"row":{"type":"object","properties":{"ok":{"type":"boolean","x-pass":true}}}},
	        "type":"object","properties":{"rows":{"type":"array","items":{"$ref":"#/$defs/row"}}}}`
	if got := refused(t, js); len(got) != 1 || got[0] != "x-pass" {
		t.Fatalf("want [x-pass] from a $defs leaf, got %v", got)
	}
	deep := `{"type":"object","properties":{"a":{"allOf":[{"type":"object","properties":{"b":{"type":"string","x-foo":1}}}]}}}`
	if got := refused(t, deep); len(got) != 1 || got[0] != "x-foo" {
		t.Fatalf("want [x-foo] through allOf, got %v", got)
	}
}

// The standard annotation keywords are ACCEPTED by the launch — probed, all
// three — and `$comment` is where the polarity declaration now lives. A check
// that refused them would refuse the remedy it prescribes.
func TestRefusedKeywords_theAnnotationKeywordsAreLegal(t *testing.T) {
	js := `{"$comment":"composed for the clean mode","type":"object","title":"t","description":"d",
	        "properties":{"clean":{"type":"boolean","$comment":"x-pass: true","description":"d"}}}`
	if got := refused(t, js); len(got) != 0 {
		t.Fatalf("the annotation keywords are legal: %v", got)
	}
}

// The whole schema is refused at spawn, so the STUB dies where the agent would.
// A WARN here would be the shape that cost the run: the runner said something
// true, the operator followed it, and the launch refused the schema.
func TestRefusedKeyword_failsTheModeAndNamesTheRemedy(t *testing.T) {
	r := run1(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { clean: { type: 'boolean', 'x-pass': true } } }
phase('P')
await agent(`+"`go`"+`, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true }`, "clean")
	if len(r.Violations) == 0 {
		t.Fatalf("a schema the launch refuses must FAIL the mode, not warn: %+v", r)
	}
	v := strings.Join(r.Violations, "\n")
	for _, want := range []string{"x-pass", "unknown keyword", "$comment"} {
		if !strings.Contains(v, want) {
			t.Fatalf("the violation must name %q — the patch, not the diagnosis: %s", want, v)
		}
	}
}

// A key whose value is a function or `undefined` never reaches the wire:
// `JSON.stringify` drops it. Reading the composed OBJECT would refuse a schema
// the launch takes, and the width check beside it has always been measured on
// the serialized form for the same reason.
func TestRefusedKeyword_readsTheWireAndNotTheComposedObject(t *testing.T) {
	r := run1(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { clean: { type: 'boolean' } }, helper: () => 1, spare: undefined }
phase('P')
await agent(`+"`go`"+`, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true }`, "clean")
	if len(r.Violations) != 0 {
		t.Fatalf("a key JSON.stringify drops is not sent and must not be refused: %v", r.Violations)
	}
	// The zero above ALSO holds with the whole walker deleted, which is a floor
	// armed on the value that disables it. So the same schema carries one key
	// that DOES serialize: the mode must fail, and the message must name that
	// key and neither of the dropped two.
	r = run1(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { clean: { type: 'boolean' } }, helper: () => 1, spare: undefined, sent: 1 }
phase('P')
await agent(`+"`go`"+`, { model: 'sonnet', effort: 'low', label: 'w', schema: S })
return { done: true }`, "clean")
	v := strings.Join(r.Violations, "\n")
	if !strings.Contains(v, `"sent"`) {
		t.Fatalf("the key that serializes must be refused by name: %v", r.Violations)
	}
	for _, dropped := range []string{"helper", "spare"} {
		if strings.Contains(v, dropped) {
			t.Fatalf("%q never reaches the wire and must not be named: %s", dropped, v)
		}
	}
}

// `$comment` carries the polarity now. The three states are distinguished
// because each has a different consequence: declared, mis-spelled (named), and
// absent (the name decides, and that is the ordinary case).
func TestDeclaredPolarity_readsTheCommentAndNothingElse(t *testing.T) {
	for _, c := range []struct {
		comment string
		xp      bool
		state   int
	}{
		{"x-pass: true", true, polarityOK},
		{"x-pass: false", false, polarityOK},
		{"X-Pass:TRUE", true, polarityOK},
		{"the rows we hold; x-pass: false", false, polarityOK},
		{"x-pass: yes", false, polarityBad},
		{"X-PASS: YES", false, polarityBad},
		// A `$comment` that MENTIONS the annotation without the colon form is
		// not a mis-spelling: it is prose. Calling it one made every comment
		// carrying the word a WARN the author could only silence by not writing
		// it.
		{"x-pass", false, polarityNone},
		{"polarity is decided by NAME here; no x-pass annotation", false, polarityNone},
		// A value the regex would otherwise swallow by prefix. Without the
		// right boundary, `falsely` reads as `false` and prose becomes a
		// declaration.
		{"x-pass: falsely reported by an earlier round", false, polarityBad},
		{"x-pass: trueish", false, polarityBad},
		{"the rows we hold", false, polarityNone},
		{"", false, polarityNone},
	} {
		xp, state := declaredPolarity(map[string]any{"$comment": c.comment})
		if xp != c.xp || state != c.state {
			t.Fatalf("%q: want (%v,%d), got (%v,%d)", c.comment, c.xp, c.state, xp, state)
		}
	}
	// No `$comment` at all, and a `$comment` that is not a string, are both the
	// ordinary case and not a mis-spelling: reporting them would paint every
	// correct schema.
	if _, state := declaredPolarity(map[string]any{"type": "boolean"}); state != polarityNone {
		t.Fatalf("a field with no $comment declares nothing")
	}
	if _, state := declaredPolarity(map[string]any{"$comment": 7}); state != polarityNone {
		t.Fatalf("a non-string $comment declares nothing")
	}
}

// The VALUE of an unknown keyword is not walked, and that decision was defended
// by nothing: a mutation that walked it survived. Nothing declares the shape
// under an unknown key, so a map there could be data, and reporting its keys
// too would turn one defect into a list.
func TestRefusedKeywords_doesNotWalkUnderAnUnknownKeyword(t *testing.T) {
	got := refused(t, `{"type":"object","x-foo":{"x-bar":1}}`)
	if len(got) != 1 || got[0] != "x-foo" {
		t.Fatalf("want [x-foo] alone, got %v", got)
	}
}

// Every keyword whose value carries further SCHEMA nodes, enumerated HERE and
// not read from `keywordChildren`: a table read from the map it tests loses its
// case with the entry, which is a floor armed on the field it constrains. Each
// row hides an unknown keyword under that entry and expects it found.
func TestRefusedKeywords_descendsThroughEverySchemaBearingKeyword(t *testing.T) {
	for _, c := range []struct{ name, js string }{
		{"allOf", `{"allOf":[{"x-foo":1}]}`},
		{"anyOf", `{"anyOf":[{"x-foo":1}]}`},
		{"oneOf", `{"oneOf":[{"x-foo":1}]}`},
		{"not", `{"not":{"x-foo":1}}`},
		{"if", `{"if":{"x-foo":1}}`},
		{"then", `{"then":{"x-foo":1}}`},
		{"else", `{"else":{"x-foo":1}}`},
		{"items", `{"type":"array","items":{"x-foo":1}}`},
		{"items as a list", `{"type":"array","items":[{"x-foo":1}]}`},
		{"additionalItems", `{"type":"array","items":[{"type":"string"}],"additionalItems":{"x-foo":1}}`},
		{"contains", `{"type":"array","contains":{"x-foo":1}}`},
		{"additionalProperties", `{"type":"object","additionalProperties":{"x-foo":1}}`},
		{"propertyNames", `{"type":"object","propertyNames":{"x-foo":1}}`},
		{"properties", `{"type":"object","properties":{"a":{"x-foo":1}}}`},
		{"patternProperties", `{"type":"object","patternProperties":{"^a$":{"x-foo":1}}}`},
		{"$defs", `{"$defs":{"row":{"x-foo":1}}}`},
		{"definitions", `{"definitions":{"row":{"x-foo":1}}}`},
		{"dependencies", `{"dependencies":{"a":{"x-foo":1}}}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := refused(t, c.js); len(got) != 1 || got[0] != "x-foo" {
				t.Fatalf("a schema node under %s must be walked: want [x-foo], got %v", c.name, got)
			}
		})
	}
}

// The other half: every keyword the launch ACCEPTS must pass, enumerated here
// and not read from `launchKeywords`. The list is the draft the probe measured —
// draft-07 plus `$defs` and `$anchor` — and a spelling from a later draft is
// deliberately NOT here, because the launch refuses those and so does this.
func TestRefusedKeywords_theMeasuredDraftIsAccepted(t *testing.T) {
	legal := []string{
		"$schema", "$id", "$ref", "$comment", "$defs", "$anchor", "definitions",
		"allOf", "anyOf", "oneOf", "not", "if", "then", "else", "dependencies",
		"items", "additionalItems", "contains", "properties", "patternProperties",
		"additionalProperties", "propertyNames",
		"type", "enum", "const", "multipleOf", "maximum", "exclusiveMaximum",
		"minimum", "exclusiveMinimum", "maxLength", "minLength", "pattern",
		"maxItems", "minItems", "uniqueItems", "maxProperties", "minProperties",
		"required", "format", "contentEncoding", "contentMediaType",
		"title", "description", "default", "readOnly", "writeOnly", "examples",
	}
	for _, k := range legal {
		js := `{"` + k + `":null}`
		if got := refused(t, js); len(got) != 0 {
			t.Fatalf("%s is a keyword the launch accepts and must not be refused: %v", k, got)
		}
	}
	// And the later spellings the probe measured REFUSED are refused here, so
	// the trim is pinned in the direction it was made.
	for _, k := range []string{"prefixItems", "dependentRequired", "unevaluatedProperties", "dependentSchemas", "$dynamicRef"} {
		js := `{"` + k + `":null}`
		if got := refused(t, js); len(got) != 1 || got[0] != k {
			t.Fatalf("%s is a later-draft spelling the launch refuses: want [%s], got %v", k, k, got)
		}
	}
}
