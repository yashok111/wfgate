// Two kinds of case, and the second kind is the point. A check that fires is
// worth little if it also fires on correct code: the predecessor reported four
// phantom ERRORs on a legitimate `{...BASE}` call and an ERROR on a COMMENT
// warning against a footgun, and a reader who has seen that stops reading the
// real ones.
package wfcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const preamble = `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
`

func run(t *testing.T, body string) *Report {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "s.mjs")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := Run(p, dir)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func has(r *Report, level Level, substr string) bool {
	for _, f := range r.Findings {
		if f.Level == level && strings.Contains(f.Msg, substr) {
			return true
		}
	}
	return false
}

func wantErr(t *testing.T, r *Report, substr string) {
	t.Helper()
	if !has(r, ERROR, substr) {
		t.Fatalf("want an ERROR containing %q, got:\n%s", substr, dump(r))
	}
}

func wantNoErr(t *testing.T, r *Report) {
	t.Helper()
	if errs, _, _ := r.Counts(); errs > 0 {
		t.Fatalf("this script is correct; got:\n%s", dump(r))
	}
}

func dump(r *Report) string {
	var b strings.Builder
	for _, f := range r.Findings {
		b.WriteString(f.Level.String() + " " + f.Msg + "\n")
	}
	return b.String()
}

func TestFires(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"missing effort", preamble + `phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', label: 'w', phase: 'P', schema: S })
return { done: true }`, "no `effort:`"},

		{"effort outside the enum, via a const", preamble + `const DEEP = 'xhgih'
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: DEEP, label: 'w', phase: 'P', schema: S })
return { done: true }`, "is not one of"},

		{"a declared phase nobody calls", `export const meta = { name: 'x', description: 'd', phases: [{ title: 'Declared' }] }
const S = { type: 'object' }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase(
  "Actual"
)
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', phase: 'Actual', schema: S })
return { done: true }`, "no meta.phases entry"},

		{"an unguarded loop beside a guarded one", preamble + `phase('P')
while (budget.total && false) { log('never') }
let n = 0
while (true) { await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S }); n++; if (n > 2) break }
return { done: true }`, "no budget.total guard of its own"},

		{"a field the schema does not declare", preamble + `phase('P')
await spawn(` + "`Audit it. Report every measurement in \"measurements\".`" + `, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
return { done: true }`, `report in "measurements"`},

		{"two agents owning one file", preamble + `phase('P')
await spawn(` + "`YOU OWN\n  src/app/main.ts — the entry point\n`" + `, { model: 'sonnet', effort: 'low', label: 'one', phase: 'P', schema: S })
await spawn(` + "`YOU OWN\n  src/app/main.ts — also the entry point\n`" + `, { model: 'sonnet', effort: 'low', label: 'two', phase: 'P', schema: S })
return { done: true }`, "owned by 2 agents"},

		{"a script that spawns nothing", `export const meta = { name: 'x', description: 'd', phases: [] }
return { done: true }`, "no agent() call sites found"},

		{"a shell footgun in a prompt", preamble + `phase('P')
await spawn(` + "`Run: git add -N missing.go && git diff`" + `, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
return { done: true }`, "git add -N"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantErr(t, run(t, c.body), c.want) })
	}
}

// Correct scripts. Every one of these drew a false ERROR from the text scanner.
func TestDoesNotFireOnCorrectScripts(t *testing.T) {
	cases := []struct{ name, body string }{
		{"options spread from a shared const", preamble + `const BASE = { model: 'opus', effort: 'high', phase: 'P', schema: S }
phase('P')
await spawn(` + "`go`" + `, { ...BASE, model: 'sonnet', label: 'one' })
return { done: true }`},

		{"an options factory behind an unrecognised wrapper", `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const opts = (label, ph) => ({ label, phase: ph, schema: S, model: 'sonnet', effort: 'low' })
const dispatch = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await dispatch(` + "`go`" + `, opts('a', 'P'))
return { done: true }`},

		{"a wrapper carrying the prompt in its options", `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const spawn = async (label, o) => { try { return await agent(o.prompt, o) } catch { return null } }
phase('P')
await spawn('kernel:door', { prompt: ` + "`build it`" + `, model: 'sonnet', effort: 'low', phase: 'P', schema: S, label: 'kernel:door' })
return { done: true }`},

		{"a comment warning about a footgun", preamble + `// Never write ` + "`git add -N missing.go && git diff`" + ` — it hides the diff entirely.
phase('P')
await spawn(` + "`go`" + `, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
return { done: true }`},

		{"an inline schema", `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(` + "`Report them in \"findings\".`" + `, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P',
  schema: { type: 'object', properties: { findings: { type: 'array' } } } })
return { done: true }`},

		{"a scalar const used as an option value", preamble + `const TIER = 'sonnet'
phase('P')
await spawn(` + "`go`" + `, { model: TIER, effort: 'low', label: 'w', phase: 'P', schema: S })
return { done: true }`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantNoErr(t, run(t, c.body)) })
	}
}

// An inline schema silently disabled the prompt-vs-schema check entirely, and a
// field declared by NO schema was structurally unreachable — the likelier
// defect of the two.
func TestInlineSchemaIsStillChecked(t *testing.T) {
	r := run(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(`+"`Report them in \"concerns\".`"+`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P',
  schema: { type: 'object', properties: { findings: { type: 'array' } } } })
return { done: true }`)
	wantErr(t, r, `report in "concerns"`)
}

// The populations are printed on every run. A count nobody can see is how an
// empty ownership map read as "no collisions".
func TestPopulationsArePrinted(t *testing.T) {
	r := run(t, preamble+`phase('P')
await spawn(`+"`go`"+`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
return { done: true }`)
	for _, want := range []string{"agent() call site(s)", "owned path(s) parsed"} {
		if !has(r, INFO, want) {
			t.Fatalf("the report must always state %q, got:\n%s", want, dump(r))
		}
	}
}
