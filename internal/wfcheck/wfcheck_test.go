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
	// A Go test file, so the drifted-test-name check applies at all: it skips a
	// tree with none, and a skipped check must not read as a passing one.
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte("package x\n\nfunc TestRealOne(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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

// One case per remaining finding. A mutation sweep over the previous version
// found 20 of 27 deletable with the bars green — a check nothing proves is a
// check on trust, and this file is where that stops.
func TestEveryFindingFires(t *testing.T) {
	head := "export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }\n"
	call := "await spawn(`go`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\n"
	wrap := "const S = { type: 'object', properties: { findings: { type: 'array' } } }\n" +
		"const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }\n"

	cases := []struct {
		name  string
		level Level
		want  string
		body  string
	}{
		{"meta absent", ERROR, "no `export const meta = {...}`", wrap + "phase('P')\n" + call + "return {}"},
		{"meta not an object", ERROR, "must be an object literal", "export const meta = 'x'\n" + wrap + "phase('P')\n" + call + "return {}"},
		{"meta interpolated", ERROR, "PURE literal", "const N = 'n'\nexport const meta = { name: `${N}`, description: 'd', phases: [{ title: 'P' }] }\n" + wrap + "phase('P')\n" + call + "return {}"},
		{"meta computed", ERROR, "PURE literal", "const f = () => 'n'\nexport const meta = { name: f(), description: 'd', phases: [{ title: 'P' }] }\n" + wrap + "phase('P')\n" + call + "return {}"},
		{"meta has no name", ERROR, "meta has no `name`", "export const meta = { description: 'd', phases: [{ title: 'P' }] }\n" + wrap + "phase('P')\n" + call + "return {}"},
		{"meta has no description", ERROR, "meta has no `description`", "export const meta = { name: 'x', phases: [{ title: 'P' }] }\n" + wrap + "phase('P')\n" + call + "return {}"},
		{"phase declared, never called", WARN, "no LITERAL phase() call", "export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }, { title: 'Ghost' }] }\n" + wrap + "phase('P')\n" + call + "return {}"},
		{"phases in a different order", WARN, "DIFFERENT order", "export const meta = { name: 'x', description: 'd', phases: [{ title: 'B' }, { title: 'A' }] }\n" + wrap + "phase('A')\nphase('B')\n" + call + "return {}"},
		{"wrapper not named spawn", WARN, "canonical name", head + "const S = { type: 'object' }\nconst go2 = async (p, o) => { try { return await agent(p, o) } catch { return null } }\nphase('P')\nawait go2(`go`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\nreturn {}"},
		{"a call with no options at all", ERROR, "no options object", head + wrap + "phase('P')\nawait spawn(`go`)\nreturn {}"},
		{"an unresolvable spread", WARN, "cannot resolve", head + wrap + "phase('P')\nawait spawn(`go`, { ...someGlobal, model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\nreturn {}"},
		{"no label", WARN, "no `label:`", head + wrap + "phase('P')\nawait spawn(`go`, { model: 'sonnet', effort: 'low', phase: 'P', schema: S })\nreturn {}"},
		{"no phase", WARN, "no `phase:`", head + wrap + "phase('P')\nawait spawn(`go`, { model: 'sonnet', effort: 'low', label: 'w', schema: S })\nreturn {}"},
		{"effort not static", INFO, "not a static value", head + wrap + "let E = 'low'\nphase('P')\nawait spawn(`go`, { model: 'sonnet', effort: E, label: 'w', phase: 'P', schema: S })\nreturn {}"},
		{"no prompt argument", ERROR, "no prompt argument", head + wrap + "phase('P')\nawait spawn()\nreturn {}"},
		{"a required field the prompt never mentions", WARN, "never mentions", head + "const S = { type: 'object', properties: { seams: { type: 'array' } }, required: ['seams'] }\nconst spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }\nphase('P')\n" + call + "return {}"},
		{"owns a path that does not exist", WARN, "does not exist and is not marked NEW", head + wrap + "phase('P')\nawait spawn(`YOU OWN\\n  src/nope/gone.ts — the thing\\n`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\nreturn {}"},
		{"parallel without filter(Boolean)", WARN, ".filter(Boolean)", head + wrap + "phase('P')\nconst rs = await parallel([() => spawn(`go`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })])\nreturn { rs }"},
		{"parallel and no pipeline", INFO, "no pipeline()", head + wrap + "phase('P')\nconst rs = (await parallel([() => spawn(`go`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })])).filter(Boolean)\nreturn { rs }"},
		{"an unused const", WARN, "never used", head + wrap + "const UNUSED_THING = 'x'\nphase('P')\n" + call + "return {}"},
		{"a contract that reaches no prompt", ERROR, "reaches NO prompt", head + wrap + "const CONTRACT = `SIGNATURE CONTRACT\\nline two of a block that no prompt ever interpolates`\nlog(CONTRACT)\nphase('P')\n" + call + "return {}"},
		{"a drifted Go test name", WARN, "no *_test.go in this tree declares", head + wrap + "phase('P')\nawait spawn(`Make TestThisNameDoesNotExistAnywhere pass`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\nreturn {}"},
		{"a gofmt lister used as a gate", ERROR, "gofmt -l X", head + wrap + "phase('P')\nawait spawn(`Run: gofmt -l internal && go test ./...`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\nreturn {}"},
		{"a placeholder in a fenced command", WARN, "placeholder inside a fenced command", head + wrap + "phase('P')\nawait spawn(`Run it:\\n    git show <sha> > /tmp/x.diff\\n`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\nreturn {}"},
		{"a direct agent() call", WARN, "straight to agent()", head + "const S = { type: 'object' }\nphase('P')\nawait agent(`go`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\nreturn {}"},
		{"a schema that cannot be read", INFO, "could not be read statically", head + wrap + "phase('P')\nawait spawn(`go`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: someGlobalSchema })\nreturn {}"},
		{"a computed phase title", INFO, "computed at run time", head + wrap + "phase(String('P'))\n" + call + "return {}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := run(t, c.body)
			if !has(r, c.level, c.want) {
				t.Fatalf("want %s containing %q, got:\n%s", c.level, c.want, dump(r))
			}
		})
	}
}

// The prompt-vs-schema check must not be disabled by the commonest trailing
// clause in a prompt.
func TestNegationGuardReadsOnlyWhatPrecedesTheField(t *testing.T) {
	r := run(t, preamble+`phase('P')
await spawn(`+"`Audit it. Report every measurement in \"measurements\" — do not summarise.`"+`, { model: 'sonnet', effort: 'low', label: 'one', phase: 'P', schema: S })
return {}`)
	wantErr(t, r, `report in "measurements"`)
}

// A wrapper that injects the phase, and one that prefixes a shared contract:
// the two commonest shapes in the guide. Neither may draw a false ERROR, and a
// real defect beside them must still be reported.
func TestWrapperExpressionsAreResolved(t *testing.T) {
	r := run(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const PHASE = 'P'
const HEAD = 'CONTRACT: report findings only.'
const TAIL = 'CONTRACT: never edit outside your files.'
const CONTRACT = `+"`${HEAD}\\n${TAIL}`"+`
const spawn = async (p, o) => { try { return await agent(`+"`${CONTRACT}\\n\\n${p}`"+`, { ...o, phase: PHASE }) } catch { return null } }
phase('P')
await spawn(`+"`review a`"+`, { model: 'opus', effort: 'high', schema: S, label: 'one' })
await spawn(`+"`review b`"+`, { model: 'opus', effort: 'nonsense', schema: S, label: 'two' })
return {}`)
	if !has(r, ERROR, `effort "nonsense"`) {
		t.Fatalf("the real defect must be reported:\n%s", dump(r))
	}
	for _, f := range r.Findings {
		if f.Level == ERROR && !strings.Contains(f.Msg, "nonsense") {
			t.Fatalf("no other ERROR may fire on this script: %s", f.Msg)
		}
	}
}

// A wrapper alias and a wrapper declared in a block are call sites.
func TestAliasedAndBlockScopedWrappersAreCallSites(t *testing.T) {
	r := run(t, preamble+`const route = spawn
phase('P')
await route(`+"`go`"+`, { model: 'sonnet', effort: 'turbo', label: 'two', phase: 'P', schema: S })
return {}`)
	wantErr(t, r, `effort "turbo"`)
}

// The inventory backstop is a SECOND population, so its own arithmetic has to be
// right or it costs more than it buys. P2f: it fired on all six scripts of the
// run — every one of them conforming, because the manual REQUIRES the wrapper
// and `async function spawn(prompt, opts)` matches `spawn(`. The operator
// launched five workflows over a standing ERROR. An ERROR nobody can clear is
// an ERROR nobody reads.
func TestInventoryBackstopDoesNotCountProseOrItsOwnWrapper(t *testing.T) {
	const meta = "export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }\nconst S = { type: 'object', properties: { findings: { type: 'array' } } }\n"
	const call = "await spawn(`go`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\n"
	cases := []struct{ name, body string }{
		{"a function-declaration wrapper", meta +
			"async function spawn(prompt, opts) {\n  try { return await agent(prompt, opts) } catch { return null }\n}\nphase('P')\n" + call + "return {}"},
		{"a comment naming the call", meta +
			"const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }\n// one agent() call, no loop, no fan-out\nphase('P')\n" + call + "return {}"},
		{"a log string naming the call", meta +
			"const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }\nphase('P')\n" + call + "log('… 1/2 agent(s) came back')\nreturn {}"},
		{"a template literal naming the call", meta +
			"const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }\nphase('P')\n" + call + "log(`${1}/2 agent(s) came back, spawn( is prose here`)\nreturn {}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := run(t, c.body)
			if has(r, ERROR, "inventory implausible") {
				t.Fatalf("this script conforms to the manual; got:\n%s", dump(r))
			}
		})
	}
}

// And it must still fire when the inventory really IS short: a wrapper that
// spawns twice per call is one call site over two agents, which is exactly the
// undercount the backstop exists to refuse to pass over silently.
func TestInventoryBackstopFiresOnAnUndercount(t *testing.T) {
	r := run(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
const pair = async (p, o) => { const a = await spawn(p, o); const b = await spawn(p, o); return [a, b] }
phase('P')
await pair(`+"`go`"+`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
return {}`)
	wantErr(t, r, "inventory implausible")
}

// The matcher reads the LITERAL argument of phase(). P2f factored five gates
// into one helper that calls `phase(phaseTitle)` and spent an investigation on
// four entries the script does call — because the message said "no phase()
// call", which is not what the checker means.
func TestPhaseWarningNamesTheLiteral(t *testing.T) {
	r := run(t, preamble+`phase(String('Gate 1'))
await spawn(`+"`go`"+`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'Gate 1', schema: S })
return {}`)
	if !has(r, WARN, "no LITERAL phase() call") {
		t.Fatalf("the warning must name what it matches:\n%s", dump(r))
	}
	if !has(r, WARN, "pass a computed title") {
		t.Fatalf("the warning must send the reader to the computed count:\n%s", dump(r))
	}
}
