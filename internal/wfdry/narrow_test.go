package wfdry

import (
	"strings"
	"testing"

	"github.com/yashok111/wfgate/internal/ws"
)

// A reading cell of a read-only run is narrowed at spawn, and the option
// removed from one call is refused in every type that is not a build: the
// skeleton's `READ_ONLY` block only asks, and the tree bar after the run is
// the only other thing that would notice. One anchor fits all four skeletons:
// the first finder's option list.
func TestReadOnlyReadersAreNarrowed(t *testing.T) {
	const anchor = "{ label, phase: 'Find', model: 'sonnet', effort: 'medium', disallowedTools: NO_WRITE, schema"
	for typ, src := range map[string]string{
		ws.TypeReview:   reviewSkeleton(t),
		ws.TypeHunt:     huntSkeleton(t),
		ws.TypeAudit:    auditSkeleton(t),
		ws.TypeSecurity: securitySkeleton(t),
	} {
		body := mutate(t, src, anchor, "{ label, phase: 'Find', model: 'sonnet', effort: 'medium', schema")
		r := byMode(runTyped(t, body, typ, "clean"), "clean")
		wantViolation(t, r, "has no `disallowedTools` naming Write and Edit")
		if !strings.Contains(strings.Join(r.Violations, "\n"), "add `disallowedTools: NO_WRITE` to this call") {
			t.Fatalf("%s: the refusal must carry the patch: %v", typ, r.Violations)
		}
	}
}

// A build's agents write by design, so the option is not asked of them; and a
// cell with `isolation: 'worktree'` writes there by design — the hunt and
// security skeletons' reproducers carry no `disallowedTools` and pass.
func TestNarrowingIsAskedOfReadersOnly(t *testing.T) {
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { verified: { type: 'boolean' } } }
phase('P')
await agent('go', { model: 'sonnet', effort: 'low', label: 'w', schema: S, phase: 'P' })
return { done: true }`
	if r := byMode(runTyped(t, body, ws.TypeBuild, "clean"), "clean"); len(r.Violations) > 0 {
		t.Fatalf("a build is not narrowed: %v", r.Violations)
	}
	for _, c := range []struct{ typ, src string }{{ws.TypeHunt, huntSkeleton(t)}, {ws.TypeSecurity, securitySkeleton(t)}} {
		r := byMode(runTyped(t, c.src, c.typ, "clean"), "clean")
		if !strings.Contains(strings.Join(r.Calls, "\n"), "verify:repro:") {
			t.Fatalf("%s: no reproducer spawned, the exemption was not exercised: %v", c.typ, r.Calls)
		}
		if len(r.Violations) > 0 {
			t.Fatalf("%s: a worktree cell is exempt: %v", c.typ, r.Violations)
		}
	}
}

// The converse of the reader refusal: a reproducer narrowed at spawn cannot
// write the test it owes, and the run would complete on "not reproduced" with
// nothing saying why. The review round's sweep added the option to the hunt's
// reproducer and every bar stayed green.
func TestReproducerIsNotNarrowed(t *testing.T) {
	const anchor = "effort: 'medium', isolation: 'worktree', schema: REPRO_SCHEMA"
	for _, c := range []struct{ typ, src string }{{ws.TypeHunt, huntSkeleton(t)}, {ws.TypeSecurity, securitySkeleton(t)}} {
		body := mutate(t, c.src, anchor, "effort: 'medium', disallowedTools: NO_WRITE, isolation: 'worktree', schema: REPRO_SCHEMA")
		wantViolation(t, byMode(runTyped(t, body, c.typ, "clean"), "clean"), "reproducer carries `disallowedTools` naming Write or Edit")
	}
}

// The harness refuses a spawn whose `disallowedTools` is not an array of
// non-empty strings, and the spawn wrapper turns that into a dead agent paid
// for in a live run; the dry run refuses the shape first, in every type.
func TestDisallowedToolsShape(t *testing.T) {
	const decl = "const NO_WRITE = ['Write', 'Edit', 'NotebookEdit']"
	for _, c := range []struct{ value, want string }{
		{"const NO_WRITE = ['Write', '']", "item 1 is , not a non-empty string"},
		{"const NO_WRITE = 'Write'", "is string, not an array"},
		{"const NO_WRITE = ['Write']", "has no `disallowedTools` naming Write and Edit"},
	} {
		r := byMode(runTyped(t, mutate(t, reviewSkeleton(t), decl, c.value), ws.TypeReview, "clean"), "clean")
		wantViolation(t, r, c.want)
	}
	body := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { verified: { type: 'boolean' } } }
phase('P')
await agent('go', { model: 'sonnet', effort: 'low', label: 'w', disallowedTools: 'Write', schema: S, phase: 'P' })
return { done: true }`
	wantViolation(t, byMode(runTyped(t, body, ws.TypeBuild, "clean"), "clean"), "is string, not an array")
}
