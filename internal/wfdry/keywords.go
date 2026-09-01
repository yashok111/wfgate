package wfdry

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// The three states of a field's polarity declaration. `polarityNone` is the
// common one and is not a defect: a field whose NAME is in the runner's
// vocabulary needs no declaration and should carry none.
const (
	polarityNone = iota
	polarityOK
	polarityBad
)

// polarityRe reads a declaration out of a `$comment`. The spelling stays
// `x-pass: true|false` — it is what the manual, the corpus and this runner's
// own notes have said since the sixteenth wave, and only its HOME moved, from
// a keyword the launch refuses to an annotation it accepts. Case-insensitive
// and allowed to sit inside a longer comment, because `$comment` is where an
// author writes prose about the field as well.
var polarityRe = regexp.MustCompile(`(?i)x-pass\s*:\s*(true|false)\b`)

// polarityAttempt is the same spelling with the VALUE left open: a `$comment`
// matching this and not `polarityRe` is an author reaching for the annotation
// and mis-spelling the value. It requires the colon for a reason — testing for
// the bare word made every `$comment` that MENTIONS the annotation ("polarity
// is decided by NAME here; no x-pass") a mis-spelling, and the only way to
// silence that note was to stop writing the word.
var polarityAttempt = regexp.MustCompile(`(?i)x-pass\s*:`)

// declaredPolarity reads a field's declared pass polarity from its `$comment`.
//
// It was `"x-pass": <bool>` as a schema keyword until 2026-09-01, when a probe
// measured that the launch refuses the whole schema over it (see
// launchKeywords above). The annotation is not optional-but-nice: without a
// way to declare polarity, a domain boolean has to be RENAMED into this
// runner's vocabulary, and `smokeAchieved` becoming `criterionMet` names the
// field for this file rather than for its reader. `$comment` keeps the
// declaration and costs the launch nothing.
func declaredPolarity(v map[string]any) (bool, int) {
	c, ok := v["$comment"].(string)
	if !ok {
		// A `$comment` that is not a string is not a declaration and is not a
		// mis-spelled one either — it is a field with no polarity declared, and
		// saying otherwise would report the ordinary case as a defect.
		return false, polarityNone
	}
	if m := polarityRe.FindStringSubmatch(c); m != nil {
		return strings.EqualFold(m[1], "true"), polarityOK
	}
	// A `$comment` MENTIONING the annotation without a readable value is the
	// author reaching for the escape hatch and mis-spelling it. Silence there
	// would hand them the name-based behaviour they were escaping, with nothing
	// saying their spelling was seen.
	if polarityAttempt.MatchString(c) {
		return false, polarityBad
	}
	return false, polarityNone
}

// quoteAll quotes each keyword so a refusal naming several of them is readable
// as a list rather than as one run-on token.
func quoteAll(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, fmt.Sprintf("%q", s))
	}
	return out
}

// The launch validates every `agent({schema})` in JSON Schema STRICT mode, and
// strict mode refuses a keyword it does not know — the WHOLE schema, at spawn,
// before the agent exists. Measured 2026-09-01 by probe, six one-agent runs of
// a workflow whose schemas differ by one key:
//
//	x-pass on an array     REFUSED  strict mode: unknown keyword: "x-pass"
//	x-pass on a boolean    REFUSED  strict mode: unknown keyword: "x-pass"
//	x-foo on a boolean     REFUSED  strict mode: unknown keyword: "x-foo"
//	passPolarity           REFUSED  strict mode: unknown keyword: "passPolarity"
//	the same schema, none  ACCEPTED
//	$comment / description / title   ACCEPTED, on the root and on a leaf
//
// So the refused class is UNKNOWN KEYWORD and not the `x-` extension prefix —
// the FOURTH probe is the one that says so, being the only refused key with no
// `x-` on it, and it is why this file carries an allowlist rather than a
// pattern.
//
// What it cost before it was checked: this runner's own WARN told a P4a author
// to add `'x-pass': true` to two booleans of an evidence schema. The launch
// refused the schema, killed the first evidence agent at spawn and, through the
// script's own abort, the other two with it — 26 of 27 acceptance properties
// came back with no evidence after 1.9 M subagent tokens and 116 minutes. The
// runner was RIGHT about its own semantics and its remedy was invalid in the
// harness it stubs, which is the failure this check exists for.
//
// The list is DRAFT-07 plus the two later spellings measured accepted, and it is
// that narrow deliberately. A first version was the UNION of draft-07 and
// 2020-12, which made its incompleteness two-way — every keyword wrongly IN the
// list is a false GREEN, the direction this check exists to remove — so a second
// probe asked which family the validator implements:
//
//	definitions           ACCEPTED   draft-07
//	dependencies          ACCEPTED   draft-07
//	$anchor               ACCEPTED   (2019 spelling, accepted anyway)
//	additionalItems       refused for a DIFFERENT reason — `"additionalItems" is
//	                      ignored when "items" is not an array of schemas` — which
//	                      is strict mode complaining about a KNOWN keyword
//	prefixItems           REFUSED    unknown keyword (2020 spelling)
//	dependentRequired     REFUSED    unknown keyword (2019 spelling)
//	unevaluatedProperties REFUSED    unknown keyword (2019 spelling)
//
// So every 2019/2020-only spelling is out: `prefixItems`, `unevaluatedItems`,
// `unevaluatedProperties`, `dependentSchemas`, `dependentRequired`,
// `$dynamicRef`, `$dynamicAnchor`, `$recursiveRef`, `$recursiveAnchor`,
// `$vocabulary`, `maxContains`, `minContains`, `contentSchema` and `deprecated`.
// Three of those are measured and the rest share a family with one that is; a
// schema using them is refused by the launch, so refusing it here is the same
// answer one step earlier. It is still a hand-written enumeration, which this
// project's own rule says cannot be complete — but its incompleteness is now
// ONE-WAY: a keyword missing from it produces a REFUSED mode over a schema the
// launch would have taken, an operator-visible false red costing one dry run,
// against a false green costing the 116 minutes above.
var launchKeywords = map[string]bool{
	// core, plus the two later spellings measured accepted
	"$schema": true, "$id": true, "$ref": true, "$comment": true,
	"$defs": true, "$anchor": true, "definitions": true,
	// applicators
	"allOf": true, "anyOf": true, "oneOf": true, "not": true,
	"if": true, "then": true, "else": true, "dependencies": true,
	"items": true, "additionalItems": true, "contains": true,
	"properties": true, "patternProperties": true, "additionalProperties": true,
	"propertyNames": true,
	// validation
	"type": true, "enum": true, "const": true,
	"multipleOf": true, "maximum": true, "exclusiveMaximum": true,
	"minimum": true, "exclusiveMinimum": true,
	"maxLength": true, "minLength": true, "pattern": true,
	"maxItems": true, "minItems": true, "uniqueItems": true,
	"maxProperties": true, "minProperties": true, "required": true,
	// format and content
	"format": true, "contentEncoding": true, "contentMediaType": true,
	// annotations
	"title": true, "description": true, "default": true,
	"readOnly": true, "writeOnly": true, "examples": true,
}

// How a keyword's value carries further SCHEMA nodes. The distinction is the
// whole point of walking structurally instead of descending into every map:
// under `properties` the keys are FIELD NAMES, and a field legitimately named
// `x-pass` or `passPolarity` is not a keyword; under `enum`, `const`, `default`
// and `examples` the value is arbitrary DATA the agent may return, and a walk
// into it would report a data key as a schema keyword. Neither is reachable by
// the generic descent `requiredCensus` uses, which is why this is its own walk.
const (
	childNone = iota
	childSchema
	childSchemaMap
)

var keywordChildren = map[string]int{
	"allOf": childSchema, "anyOf": childSchema, "oneOf": childSchema,
	"not": childSchema, "if": childSchema, "then": childSchema, "else": childSchema,
	"items": childSchema, "additionalItems": childSchema,
	"contains": childSchema, "additionalProperties": childSchema,
	"propertyNames": childSchema,

	"properties": childSchemaMap, "patternProperties": childSchemaMap,
	"$defs": childSchemaMap, "definitions": childSchemaMap,
	"dependencies": childSchemaMap,
}

// refusedKeywordsIn is refusedKeywords over the bytes the launch would
// actually receive — the SAME string the width was measured from, taken once at
// the call site, because two derivations of one quantity is the class this
// file's own neighbour warns about. The serialization is the boundary that matters: a key whose
// value is `undefined` or a function is dropped by `JSON.stringify` and never
// reaches the validator, so reading the composed OBJECT would refuse a schema
// over a key nothing sends. A schema that does not serialize is not a keyword
// question at all — it is reported as its own failure one branch above.
func refusedKeywordsIn(js string) []string {
	var wire any
	if err := json.Unmarshal([]byte(js), &wire); err != nil {
		// Unreachable through `JSON.stringify`, and it returns "no refused
		// keyword" rather than guessing: this check answers one question, and
		// bytes it cannot read are not evidence of an unknown keyword.
		return nil
	}
	return refusedKeywords(wire)
}

// refusedKeywords returns, sorted and deduplicated, every key in the composed
// schema that the launch's validator would refuse as unknown. An empty result
// is the only shape that launches.
func refusedKeywords(schema any) []string {
	seen := map[string]bool{}
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case []any:
			// `items: [A, B]` and `allOf: [A, B]` reach here. A boolean schema
			// (`additionalProperties: false`) reaches neither case and is
			// correctly ignored.
			for _, e := range v {
				walk(e)
			}
		case map[string]any:
			// No sort inside the walk: `seen` is a set and the result is
			// sorted once at the end, so ordering here is unobservable — it
			// survived every mutation, and this project deletes such a line
			// rather than shipping it.
			for k := range v {
				if !launchKeywords[k] {
					// The VALUE of an unknown keyword is not walked: nothing
					// declares its shape, so a map under it could be data, and
					// reporting its keys too would turn one defect into a list.
					seen[k] = true
					continue
				}
				switch keywordChildren[k] {
				case childSchema:
					walk(v[k])
				case childSchemaMap:
					// A value that is not a map walks nothing and reports
					// nothing. The schema is malformed and the launch refuses
					// it for a different reason, so this is a gap in what this
					// check can say rather than a keyword it misses.
					if m, ok := v[k].(map[string]any); ok {
						for _, sub := range m {
							walk(sub)
						}
					}
				}
			}
		}
	}
	walk(schema)
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
