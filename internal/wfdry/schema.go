package wfdry

import (
	"fmt"
	"sort"
	"strings"
)

// What this corpus knows about the width the launch's safety classifier will
// accept. Every number below is this runner's own `JSON.stringify` of the schema
// the script composes on the CLEAN path, measured on the two gates that have
// launched, and the quantity matters as much as the value — see SchemaWidths.
//
// REFUSED (`blocked by safety classifier: output schema too large to classify
// safely`), mocker P3a's second launch, four agents blocked in one run:
// `verify` 27,212, `accept:confirm` 8,596, `accept:writes` 5,760,
// `accept:reads` 4,814. ACCEPTED in that same run: `accept:surface` 3,390.
// Its third launch, after the schemas were chunked, ran 44 of 44 agents with
// zero errors at a widest clean width of 3,435.
//
// REFUSED, mocker P3c's first launch: `accept` 4,589 — the FIFTEENTH and last
// agent, six hours in, with nothing downstream to catch it. Its second launch,
// after the same narrowing, ran 15 of 15 at 3,877.
//
// REFUSED, mocker P3d, and this one was PROBED rather than paid for: one
// throwaway agent carrying the real schema and a prompt saying to fill it with
// placeholders answered in 82 milliseconds at zero tokens — 4,233 bytes
// REFUSED, and 3,114 ACCEPTED in 41 seconds. That is the narrowest refusal on
// record and it moved the floor down by 356 bytes, from a number two rounds of
// that gate had argued about from measurements of SIZE alone while the answer
// cost one cheap agent. The floor is the LOWEST refusal the corpus has
// recorded, never the last one it heard about.
//
// ACCEPTED, mocker P3e, probed TWICE inside one gate and neither time argued
// about: 3,869 (18 s, 58k tokens), and then — after a `required` array that a
// width-reduction edit had dropped was restored, taking the schema to 3,974 and
// INTO the unmeasured band — 3,974, re-probed rather than reasoned about (19 s,
// 58k tokens). That moved the ceiling up by 97 bytes for about 40 seconds and
// 116k tokens, and it is the strongest evidence in this corpus that the probe
// rule is load-bearing practice rather than a lesson re-learned each gate.
//
// So on the CLEAN quantity the corpus separates: every accepted width is at or
// below 3,974 and every refused one at or above 4,233. The gap between them is
// UNMEASURED, and this tool must not pretend to know where in it the boundary
// falls. P3c's refusal quoted 4,563 against this runner's 4,589 — two
// serializers 26 bytes, 0.6%, apart, with this one the LARGER, which is the safe
// direction for a warning; no check should need them to agree exactly.
//
// The same probe answers the other question a wide schema raises. The launch
// RESOLVES `$defs`/`$ref` and ENFORCES the `minimum`/`minLength` that reach a
// leaf through one — a deliberately illegal first attempt was rejected — so
// declaring a repeated leaf once and referencing it is the narrowing this
// corpus keeps reaching for, and it is safe. It used to cost the dry run,
// which resolved nothing; it does not any more (see resolveRefs).
//
// The review round that measured all of this refuted the wave that shipped it:
// the constants were P3c's numbers, where the clean width and the widest branch
// coincide, and they were being compared against the widest branch. On P3a they
// differ by 55% (`verify`: 3,435 clean, 5,326 on the failure branches), so the
// tool WARNed on the widest schema of the one run in this corpus that came back
// 44 of 44. One quantity computed, another calibrated.
const (
	// SchemaAccepted is the widest CLEAN width this corpus has seen accepted.
	// It is the HIGHEST acceptance on record and moves UP as one is measured,
	// exactly as SchemaRefused is the LOWEST refusal and moves down.
	SchemaAccepted = 3974
	// SchemaRefused is the narrowest CLEAN width this corpus has seen refused.
	SchemaRefused = 4233
)

// SchemaWidth is one label's schema, in the two quantities that are not the
// same question.
//
// Clean is what the happy path composes, and it is the quantity the corpus
// points above were measured as. Widest is the largest any mode composed: a
// schema assembled on a branch is wider there, and the launch sends whatever the
// branch that runs composes — so a wide FAILURE branch is a real risk and a
// different statement, never evidence about the width the corpus recorded.
type SchemaWidth struct {
	Clean  int
	Widest int
	Labels []string

	// The `required` census of the CLEAN schema — how many keys it names as
	// required over every object node, and how many nodes declare `properties`
	// and require none of them. It groups WITH the width rather than beside it:
	// two labels share a line only when their schemas agree on both, because
	// equal byte counts do not make two schemas the same schema.
	Required   int
	NoRequired int
}

// SchemaWidths groups the schemas a run composed, widest clean width first.
//
// A synthetic label (`agent@<ordinal>`) is NOT merged across modes: the ordinal
// is per mode, so the same name is a different call in a different run, and
// merging them attributes one width to two call sites. `wfcheck` already WARNs
// on the missing `label:` that produces one.
func SchemaWidths(rs []Result) []SchemaWidth {
	type widths struct{ clean, widest, required, noRequired int }
	acc := map[string]*widths{}
	var order []string
	for _, r := range rs {
		for _, s := range r.Schemas {
			key := s.Label
			if strings.HasPrefix(s.Label, "agent@") {
				key = r.Mode + " " + s.Label
			}
			w, seen := acc[key]
			if !seen {
				w = &widths{clean: -1, widest: s.Bytes, required: s.Required, noRequired: s.NoRequired}
				acc[key] = w
				order = append(order, key)
			}
			if s.Bytes > w.widest {
				w.widest = s.Bytes
			}
			if r.Mode == "clean" {
				w.clean = s.Bytes
				w.required, w.noRequired = s.Required, s.NoRequired
			}
		}
	}
	// Where no `clean` mode ran — `--mode X` builds a run of one — the widest is
	// all there is, and saying so is the caller's job, not a second number's.
	byPair := map[widths][]string{}
	var pairs []widths
	for _, k := range order {
		w := *acc[k]
		if w.clean < 0 {
			w.clean = w.widest
		}
		if _, seen := byPair[w]; !seen {
			pairs = append(pairs, w)
		}
		byPair[w] = append(byPair[w], k)
	}
	out := make([]SchemaWidth, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, SchemaWidth{Clean: p.clean, Widest: p.widest, Required: p.required, NoRequired: p.noRequired, Labels: byPair[p]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Clean != out[j].Clean {
			return out[i].Clean > out[j].Clean
		}
		return out[i].Widest > out[j].Widest
	})
	return out
}

// SchemaNotes says what a width means, and it says it in the register of a
// question. Nothing here fails a mode: a wide schema is not a defect in the
// script's control flow, which is what the modes measure, and refusing a run
// over a boundary this corpus has bracketed but not located would be the check
// impersonating a classifier it cannot see. A schema that does not SERIALIZE is
// the other thing entirely and fails the mode, in runMode.
func SchemaNotes(ws []SchemaWidth) []string {
	var out []string
	probe := "PROBE it before the launch: one cheap agent carrying this schema and a prompt that says fill it with placeholders answers in seconds, against a multi-hour run; width is measurable from here and acceptance is not"
	for _, w := range ws {
		who := fmt.Sprintf("%s (%s)", plural(len(w.Labels), "label"), strings.Join(w.Labels, ", "))
		switch {
		case w.Clean >= SchemaRefused:
			out = append(out, fmt.Sprintf("schema width: %d bytes on the happy path, %s — at or above %d, the narrowest CLEAN width this corpus has seen REFUSED (a run lost four agents to that refusal, another lost its LAST agent six hours in). %s",
				w.Clean, who, SchemaRefused, probe))
		case w.Clean > SchemaAccepted:
			out = append(out, fmt.Sprintf("schema width: %d bytes on the happy path, %s — above %d, the widest this corpus has seen ACCEPTED, and below %d, the narrowest it has seen REFUSED. That gap is UNMEASURED, not safe. %s",
				w.Clean, who, SchemaAccepted, SchemaRefused, probe))
		}
		// The wide FAILURE branch, as its own statement. It is not evidence
		// about the width the corpus recorded — that quantity is the clean one —
		// but the launch sends whatever the branch that runs composes, and the
		// refusal this corpus paid most for landed on an agent at the very tail.
		if w.Widest > w.Clean && w.Widest >= SchemaRefused {
			out = append(out, fmt.Sprintf("schema width: %s compose %d bytes on a FAILURE branch against %d on the happy path — at or above the narrowest refusal recorded, on a branch the happy path never rehearses. %s",
				who, w.Widest, w.Clean, probe))
		}
	}
	return out
}
