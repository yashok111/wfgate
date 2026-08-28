package wfdry

import (
	"fmt"
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
		record(r.rec, ref+" ("+err.Error()+")")
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
