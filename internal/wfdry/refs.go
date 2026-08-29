package wfdry

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// resolveRefs expands `$ref` against the schema's own `$defs` before the
// fixture is synthesised.
//
// Measured on the P3d gate, and the gate measured BOTH directions. Its
// acceptance report was 6,038 serialized bytes with the bar leaf inline;
// declaring that leaf once and referencing it took the schema to 3,155 — and
// BROKE the dry run, so the gate reverted it one commit later and went back to
// 4,233, above the width its own probe then measured as REFUSED. Nothing here
// resolved a `$ref`, so the referenced node carried no `type`, every bar came
// back as the placeholder STRING "<makeSmoke from fix:accept>",
// `obs.bars[k].result` was undefined and the clean mode returned green:false
// with all five bars RED. 7/7 modes became 6/7 — correctly, and for a reason no
// message explained. What took the shipped schema under in the end was trimming
// the descriptions, with the per-property leaf's `$ref` kept: 3,114 bytes
// against 5,721 fully inlined, by the gate's own record.
//
// The launch RESOLVES them: a probe against the real thing, on the same
// construct, had a deliberately illegal `subjects: 0` REJECTED, so the
// `minimum` and `minLength` reaching a leaf through a `$ref` are enforced
// there. So the asymmetry was the runner's, and it fell on exactly the
// narrowing the width band keeps asking for.
//
// The expansion is for the FIXTURE only. The width is measured at the spawn,
// from the object the script composed, and that object is what the launch
// receives: a schema is sent with its `$ref`s in it, and expanding one here
// must not change the number that is compared against a refusal.
func resolveRefs(node any, root map[string]any, rec map[string]int) any {
	r := &refResolver{root: root, rec: rec, memo: map[string]any{}, active: map[string]bool{}}
	return r.walk(node)
}

// refResolver carries the two things the walk cannot recompute: which
// references are being expanded on the CURRENT path, and what each one expanded
// to last time.
//
// There is deliberately NO depth budget. The first version charged one per
// structural descent and capped the total, which is the seventh wave's own
// banked defect — "`wfcheck`'s prompt resolver charged its depth budget PER
// NODE … depth is charged per INDIRECTION now" — recommitted in the file whose
// comment claimed the opposite: each JSON-Schema level costs two nodes
// (`properties` plus the key), so a `$ref` on a leaf four objects down was
// refused on its FIRST indirection, the leaf came back a placeholder STRING,
// and the WARN sent the author hunting a cycle that did not exist. A chain of
// nine distinct references is legal and finite; what must be refused is a
// reference expanded inside its own expansion, and `active` says exactly that.
// `memo` is not a guard, it is what keeps a shared leaf referenced nine times
// from being expanded nine times over: everything downstream of here only READS
// the synthesised structure.
type refResolver struct {
	root   map[string]any
	rec    map[string]int
	memo   map[string]any
	active map[string]bool
}

func (r *refResolver) walk(node any) any {
	switch v := node.(type) {
	case map[string]any:
		if raw, ok := v["$ref"]; ok {
			if expanded, ok := r.expand(raw); ok {
				// Sibling keys survive the expansion and win over the
				// referenced node's, key by key and MAP BY MAP:
				// `{$ref, description}` is the shape a script writes to
				// annotate one use of a shared leaf, and a shallow overwrite
				// dropped the referenced node's `properties` wholesale wherever
				// a sibling named the same key. This runner is a fixture
				// generator and not a validator — where the two disagree on a
				// SCALAR (an `enum` against an `enum`) the site's own key wins,
				// because that is the one the author wrote at the call.
				return mergeSchema(expanded, r.walkMap(v, "$ref"))
			}
		}
		return r.walkMap(v, "")
	case []any:
		out := make([]any, 0, len(v))
		for _, e := range v {
			out = append(out, r.walk(e))
		}
		return out
	default:
		return node
	}
}

func (r *refResolver) walkMap(v map[string]any, skip string) map[string]any {
	out := make(map[string]any, len(v))
	for k, val := range v {
		if k == skip {
			continue
		}
		out[k] = r.walk(val)
	}
	return out
}

// expand resolves one reference, or records why it could not and leaves the
// node to synthesise as it stands.
func (r *refResolver) expand(raw any) (map[string]any, bool) {
	ref, isStr := raw.(string)
	if !isStr {
		record(r.rec, fmt.Sprintf("%v (a $ref that is not a string)", raw))
		return nil, false
	}
	if r.active[ref] {
		// A reference expanded inside its own expansion has no fixed point,
		// and it is the shape a hand-written `$defs` reaches by accident.
		record(r.rec, ref+" (a cycle: the reference is expanded inside its own expansion)")
		return nil, false
	}
	if cached, ok := r.memo[ref]; ok {
		m, _ := cached.(map[string]any)
		return m, m != nil
	}
	target, err := pointer(ref, r.root)
	if err != nil {
		record(r.rec, ref+" ("+err.Error()+defsHint(ref, r.root)+")")
		return nil, false
	}
	r.active[ref] = true
	expanded, _ := r.walk(target).(map[string]any)
	delete(r.active, ref)
	if expanded == nil {
		record(r.rec, ref+" (resolves to something that is not an object)")
		return nil, false
	}
	r.memo[ref] = expanded
	return expanded, true
}

// mergeSchema overlays `over` on `base` without losing what `base` declares.
// Two maps under one key are merged; anything else is replaced.
func mergeSchema(base, over map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if bm, ok := out[k].(map[string]any); ok {
			if om, ok := v.(map[string]any); ok {
				out[k] = mergeSchema(bm, om)
				continue
			}
		}
		out[k] = v
	}
	return out
}

func record(rec map[string]int, what string) {
	if rec != nil {
		rec[what]++
	}
}

// pointer resolves a LOCAL JSON pointer (`#/$defs/thing`) against the schema
// itself. A remote reference is refused by name rather than fetched: a dry run
// that reached the network would be measuring something other than the script.
// So is an ANCHOR (`#name`): trimming the `#` and looking the rest up as a
// root key made `#bar` resolve to whatever `bar` happens to be at the top of
// the schema, which is a wrong answer where an unresolved one is honest.
func pointer(ref string, root map[string]any) (any, error) {
	if ref == "#" || ref == "#/" {
		return nil, fmt.Errorf("a reference to the whole schema is a cycle")
	}
	if !strings.HasPrefix(ref, "#/") {
		if strings.HasPrefix(ref, "#") {
			return nil, fmt.Errorf("an anchor reference; this runner resolves `#/...` pointers only")
		}
		return nil, fmt.Errorf("not a local reference — this runner resolves `#/...` against the schema itself and fetches nothing")
	}
	var cur any = root
	prev := "the schema root"
	for _, raw := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		// RFC 6901 escapes, in this order: ~1 is a slash, ~0 a tilde.
		seg := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			// The step that is not an object is the one BEFORE the one that
			// cannot be taken; naming the latter sends the reader to the wrong
			// half of their own pointer.
			return nil, fmt.Errorf("%s is not an object, so %q cannot be a step through it", prev, seg)
		}
		cur, ok = m[seg]
		if !ok {
			return nil, fmt.Errorf("%q is not in the schema", seg)
		}
		prev = fmt.Sprintf("%q", seg)
	}
	return cur, nil
}

// defsHint answers the question the failure leaves open: a `$defs` written on a
// SUB-schema is not reachable by `#/$defs/x`, because a JSON pointer is
// anchored at the document the schema IS — and that is the shape a script
// reaches for, because the leaf and the `$defs` declaring it are written
// together in one constant and the constant is then nested under `properties`.
//
// Measured on the P3e gate, which reported the whole feature as broken: the
// root-anchored shape resolves and always did (a leaf declared at the schema
// root and referenced from twenty-two keys built by `Object.fromEntries` fills
// in every mode), the sub-schema shape does not, and the note said only
// `"$defs" is not in the schema`. That gate reverted to twenty-two inlined
// copies and paid about 1,800 bytes of width against a classifier with a
// measured refusal at 4,233 — for a pointer it could have moved. So the hint
// names the pointer that WOULD resolve: the same trade `check drift` makes
// when it prints the re-aimed range rather than saying a citation moved.
func defsHint(ref string, root map[string]any) string {
	name, ok := strings.CutPrefix(ref, "#/$defs/")
	if !ok || name == "" || strings.Contains(name, "/") {
		return ""
	}
	if at := findDefs(root, name, nil); at != "" {
		return "; a `$defs` declaring " + strconv.Quote(name) + " IS in this schema, at `" + at +
			"` — a `$ref` is anchored at the schema root, so either move the `$defs` there or write `" + at + "`"
	}
	return ""
}

// findDefs walks the schema for a `$defs` map carrying `name` and returns the
// pointer that reaches it. The walk is breadth-agnostic and returns the FIRST
// site in map order made deterministic by sorting: a schema declaring the same
// name in two places is a different defect and naming either one is a patch.
//
// ARRAYS are walked too, with their index as a segment. `allOf`/`anyOf`/`oneOf`
// hold their subschemas in one, and a hint that stops at the first array is a
// hint that misses the composition keywords a large schema is most likely to
// use.
func findDefs(node any, name string, path []string) string {
	switch v := node.(type) {
	case map[string]any:
		// No "not at the root" guard: the walk is only ever reached from a
		// pointer that FAILED, and `#/$defs/x` fails exactly when the root has
		// no such path — so a root `$defs` carrying the name cannot be here. A
		// guard no mutation can make fail is a guard nothing proves.
		if defs, ok := v["$defs"].(map[string]any); ok {
			if _, ok := defs[name]; ok {
				return jsonPointer(append(append([]string{}, path...), "$defs", name))
			}
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if at := findDefs(v[k], name, append(path, k)); at != "" {
				return at
			}
		}
	case []any:
		for i, e := range v {
			if at := findDefs(e, name, append(path, strconv.Itoa(i))); at != "" {
				return at
			}
		}
	}
	return ""
}

// jsonPointer joins segments into a local pointer, ESCAPED. RFC 6901 spells a
// tilde `~0` and a slash `~1`, in that order — and this is the one place the
// project emits a pointer rather than reading one, so an unescaped segment
// would hand the author a pointer that does not resolve, which is worse than
// no hint at all: the whole value of the hint is that it is a patch.
func jsonPointer(segs []string) string {
	out := make([]string, 0, len(segs))
	for _, s := range segs {
		s = strings.ReplaceAll(s, "~", "~0")
		out = append(out, strings.ReplaceAll(s, "/", "~1"))
	}
	return "#/" + strings.Join(out, "/")
}

// requiredCensus counts, over the schema the script composed, how many keys
// every object node names as `required` and how many nodes declare
// `properties` and require none of them.
//
// It expands `$ref` first, deliberately: the census is a statement about what
// the AGENT must fill, and a leaf declared once and referenced twenty-two times
// is twenty-two obligations. Counting the unexpanded form would price the same
// schema differently for the two spellings the width band keeps pushing an
// author between, so a narrowing edit would move a number that is not about
// narrowing.
//
// Neither number is a verdict. An object with no `required` is legal and
// common — this runner's own `clean.mjs` fixture is one — so a bar over it
// would paint every correct script, which is `dup`'s 161-of-176 lesson. What
// the pair buys is that a DROP is a changed number in a line the operator
// already reads: on the P3e gate `required: ['met','evidence']` disappeared
// from a leaf inside an edit whose stated purpose was width, and no bar, no
// static check and no reviewer saw it for two rounds.
func requiredCensus(schema any) (required, noRequired int) {
	root, ok := schema.(map[string]any)
	if !ok {
		return -1, -1
	}
	// A `$ref` that did not expand leaves the referenced leaf a placeholder, so
	// the `required` it declares is not in the walk and the census UNDERCOUNTS
	// — silently, and by exactly the amount the author cares about. Measured on
	// the banked nested-defs fixture: 4 keys reported against 10 declared. A
	// partial count printed as a measurement is this project's own most-named
	// defect, so a failed expansion makes the whole census unmeasured; the note
	// naming the reference is already printed beside it.
	failed := map[string]int{}
	expanded, _ := resolveRefs(root, root, failed).(map[string]any)
	if len(failed) > 0 || expanded == nil {
		return -1, -1
	}
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			req, _ := v["required"].([]any)
			required += len(req)
			// `$defs` is a declaration site, not a node the agent fills: a leaf
			// declared there and never referenced would otherwise be counted
			// once for existing and again for every use.
			if props, ok := v["properties"].(map[string]any); ok && len(props) > 0 && len(req) == 0 {
				noRequired++
			}
			keys := make([]string, 0, len(v))
			for k := range v {
				if k != "$defs" {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(v[k])
			}
		case []any:
			for _, e := range v {
				walk(e)
			}
		}
	}
	walk(expanded)
	return required, noRequired
}
