package wfdry

import (
	"fmt"
	"sort"
	"strings"
)

// What this corpus knows about the width the launch's safety classifier will
// accept, and it is TWO points and a gap, never a threshold.
//
// Measured, both from live runs of gates in this project: mocker P3a lost four
// agents at 23,756 serialized bytes, and mocker P3c lost its FIFTEENTH and last
// agent at 4,563 — `[accept] blocked by safety classifier: output schema too
// large to classify safely` — after six hours, with nothing downstream to catch
// it. The same gate then narrowed that schema to 3,877 by declaring a repeated
// leaf once, PROBED it with one cheap agent, and relaunched: 15 of 15.
//
// Validated against that workspace's own history: this runner measures the
// revision that DIED at 4,589 bytes and the revision that then launched 15/15
// at 3,877. The refusal quoted 4,563, so the two serializers disagree by 26
// bytes — 0.6% — on the one point where both numbers exist, and the runner's is
// the LARGER, which is the safe direction for a warning. The number here is
// `JSON.stringify` in the embedded engine, not the launcher's, and no check
// should be built that needs them to agree exactly.
//
// So the boundary is somewhere in (3877, 4563] and this tool must not pretend
// to know where. A width is a QUESTION, never a verdict: the answer is the
// probe, which costs seconds against a multi-hour run. The gate that wrote both
// numbers down had also written, in its own artifact, that "what cannot be
// measured from here is acceptance itself, only size" — and that sentence was
// false, which is why this check exists at all.
const (
	// SchemaAccepted is the widest schema this corpus has seen ACCEPTED.
	SchemaAccepted = 3877
	// SchemaRefused is the narrowest schema this corpus has seen REFUSED.
	SchemaRefused = 4563
)

// SchemaWidth is one serialized width and every label whose schema has it.
type SchemaWidth struct {
	Bytes  int
	Labels []string
}

// SchemaWidths groups the schemas a run composed by their serialized width,
// widest first.
//
// A label is taken at its WIDEST across modes: a schema assembled on a branch
// is narrower in the mode that skipped the branch, and the launch sends the
// wide one.
func SchemaWidths(rs []Result) []SchemaWidth {
	max := map[string]int{}
	var order []string
	for _, r := range rs {
		for _, s := range r.Schemas {
			if _, seen := max[s.Label]; !seen {
				order = append(order, s.Label)
				max[s.Label] = s.Bytes
				continue
			}
			if s.Bytes > max[s.Label] {
				max[s.Label] = s.Bytes
			}
		}
	}
	byWidth := map[int][]string{}
	for _, l := range order {
		byWidth[max[l]] = append(byWidth[max[l]], l)
	}
	out := make([]SchemaWidth, 0, len(byWidth))
	for b, ls := range byWidth {
		out = append(out, SchemaWidth{Bytes: b, Labels: ls})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out
}

// SchemaNotes says what a width means, and it says it in the register of a
// question. Nothing here fails a mode: a wide schema is not a defect in the
// script's control flow, which is what the modes measure, and refusing a run
// over a number this corpus has two points for would be the check impersonating
// a classifier it cannot see.
func SchemaNotes(ws []SchemaWidth) []string {
	var out []string
	for _, w := range ws {
		who := fmt.Sprintf("%s (%s)", plural(len(w.Labels), "label"), strings.Join(w.Labels, ", "))
		switch {
		case w.Bytes < 0:
			out = append(out, "schema width: "+who+" carry a schema that does not serialize at all — `JSON.stringify` returned nothing, so what the launch would send cannot be read here, and it is not zero")
		case w.Bytes >= SchemaRefused:
			out = append(out, fmt.Sprintf("schema width: %d bytes, %s — at or above %d, the narrowest schema this corpus has seen REFUSED (`output schema too large to classify safely`, on a run's LAST agent, after six hours). PROBE it before the launch: one cheap agent carrying this schema and a prompt that says fill it with placeholders answers in seconds; width is measurable from here and acceptance is not",
				w.Bytes, who, SchemaRefused))
		case w.Bytes > SchemaAccepted:
			out = append(out, fmt.Sprintf("schema width: %d bytes, %s — above %d, the widest this corpus has seen ACCEPTED, and below %d, the narrowest it has seen REFUSED. That band is UNMEASURED, not safe: probe it before the launch rather than arguing from the number",
				w.Bytes, who, SchemaAccepted, SchemaRefused))
		}
	}
	return out
}
