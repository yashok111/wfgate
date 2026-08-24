// Two kinds of case, and the second kind is the point. A check that fires is
// worth little if it also fires on correct code: the predecessor reported four
// phantom ERRORs on a legitimate `{...BASE}` call and an ERROR on a COMMENT
// warning against a footgun, and a reader who has seen that stops reading the
// real ones.
package wfcheck

import (
	"os"
	"path/filepath"
	"strconv"
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
// A path under a DO NOT heading is an EXCLUSION, and counting it as owned made
// the dual-ownership ERROR fire on a script that had declared the separation
// exactly as the manual prescribes. shoutyRe could not end the block there: it
// demands nine characters and "DO NOT" is six. A false ERROR is the one kind
// this checker must never invent.
func TestExclusionBlockEndsOwnership(t *testing.T) {
	preamble := `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { ok: { type: 'boolean' } } }
async function spawn(p, o) { try { return await agent(p, o) } catch { return null } }
`
	body := preamble + `phase('P')
await spawn(` + "`YOU OWN\n  src/app/one.ts — the entry point\n\nDO NOT\n  src/app/two.ts\n`" + `, { model: 'sonnet', effort: 'low', label: 'one', phase: 'P', schema: S })
await spawn(` + "`YOU OWN\n  src/app/two.ts — the other section owns it\n`" + `, { model: 'sonnet', effort: 'low', label: 'two', phase: 'P', schema: S })
return { done: true }`
	rep := run(t, body)
	if has(rep, ERROR, "owned by 2 agents") {
		t.Fatalf("the excluded path was counted as owned:\n%s", dump(rep))
	}
	if !has(rep, INFO, "2 owned path(s) parsed") {
		t.Fatalf("want exactly the two declared owners, got:\n%s", dump(rep))
	}

	// Forbidding text on the HEADER line is prose about the block, not a
	// heading: ending the scan there lost every owner of the prompt and
	// silenced a dual-ownership ERROR the previous release reported. A checker
	// may neither invent an ERROR nor lose one.
	header := preamble + `phase('P')
await spawn(` + "`YOU OWN these files and must not write anywhere else.\n  internal/shared.go — the seam\n`" + `, { model: 'sonnet', effort: 'low', label: 'one', phase: 'P', schema: S })
await spawn(` + "`YOU OWN these files and must not write anywhere else.\n  internal/shared.go — also the seam\n`" + `, { model: 'sonnet', effort: 'low', label: 'two', phase: 'P', schema: S })
return { done: true }`
	if rep := run(t, header); !has(rep, ERROR, "owned by 2 agents") {
		t.Fatalf("a forbidding phrase on the header line must not lose the owners:\n%s", dump(rep))
	}

	// The exclusion is a REGION ended by a blank line, not the rest of the
	// prompt: an owner listed after the DO NOT block still counts. (A SHOUTY
	// heading ends the whole block instead, which is the documented rule and is
	// why this case uses a blank line rather than one.)
	after := preamble + `phase('P')
await spawn(` + "`YOU OWN\n  src/app/one.ts — the entry point\n\nDO NOT\n  src/app/two.ts\n\n  src/app/three.ts — the tail\n`" + `, { model: 'sonnet', effort: 'low', label: 'one', phase: 'P', schema: S })
return { done: true }`
	if rep := run(t, after); !has(rep, INFO, "2 owned path(s) parsed") {
		t.Fatalf("an owner after an exclusion region must still count:\n%s", dump(rep))
	}

	// An inline exclusion carries its path on the forbidding line itself, and
	// only that line is skipped — the owners after it still count.
	inline := preamble + `phase('P')
await spawn(` + "`YOU OWN\n  src/app/one.ts — the entry point\n  src/app/three.ts — do not reformat it\n`" + `, { model: 'sonnet', effort: 'low', label: 'one', phase: 'P', schema: S })
return { done: true }`
	if rep := run(t, inline); !has(rep, INFO, "1 owned path(s) parsed") {
		t.Fatalf("an inline exclusion must skip only its own line, got:\n%s", dump(rep))
	}
}

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

// The scanner blanks a literal, not the rest of the file. Every shape below is
// one an author writes, and each one used to hand the next state the wrong
// offset: an escaped quote, an escaped backslash at the very end of a literal, a
// nested template inside an interpolation, and a spawn call written INSIDE an
// interpolation — which is code and must still be counted.
func TestStripNonCodeKeepsItsPlace(t *testing.T) {
	const meta = "export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }\nconst S = { type: 'object', properties: { findings: { type: 'array' } } }\nconst spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }\n"
	cases := []struct{ name, body string }{
		{"an escaped quote inside a string", meta + "const q = 'it\\'s an agent( in prose'\nphase('P')\nawait spawn(`go`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\nreturn {}"},
		{"a backslash ending a string", meta + "const q = 'a trailing pair \\\\'\nphase('P')\nawait spawn(`go`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\nreturn {}"},
		{"a template nested in an interpolation", meta + "const n = 1\nconst q = `outer ${`inner ${n} agent(` } tail`\nphase('P')\nawait spawn(`go`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\nreturn {}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := run(t, c.body)
			if has(r, ERROR, "inventory implausible") {
				t.Fatalf("prose is not a call site; got:\n%s", dump(r))
			}
			if !has(r, INFO, "1 agent() call site(s)") {
				t.Fatalf("the real call site must still be found:\n%s", dump(r))
			}
		})
	}
	// A call written inside an interpolation IS code. Blanking it would make the
	// backstop's population smaller than the truth, which is the one direction a
	// backstop must never move.
	t.Run("a call inside an interpolation is code", func(t *testing.T) {
		r := run(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const out = `+"`result: ${await spawn(`go`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })}`"+`
return { out }`)
		if !has(r, INFO, "1 agent() call site(s)") {
			t.Fatalf("an interpolated call must be inventoried:\n%s", dump(r))
		}
		if has(r, ERROR, "inventory implausible") {
			t.Fatalf("and counted textually too; got:\n%s", dump(r))
		}
	})
}

// The scanner is exercised directly where its output is the only observable
// thing: an interpolation that ends early hands the REST of the interpolation
// back to the template as literal text, so a call written after a quoted brace
// disappears from the textual population — and a backstop whose population is
// too small says nothing at all, which is the one failure it cannot report.
func TestStripNonCode(t *testing.T) {
	cases := []struct {
		name       string
		src        string
		keep, drop []string
	}{
		{"a comment", "spawn(a)\n// spawn(b) in prose\n", []string{"spawn(a)"}, []string{"spawn(b)"}},
		{"a block comment", "spawn(a)\n/* spawn(b)\n   spawn(c) */\n", []string{"spawn(a)"}, []string{"spawn(b)", "spawn(c)"}},
		{"a string", "log('spawn(b) came back')\nspawn(a)\n", []string{"spawn(a)"}, []string{"spawn(b)"}},
		{"an escaped quote", `log('it\'s spawn(b)') ; spawn(a)`, []string{"spawn(a)"}, []string{"spawn(b)"}},
		{"a template chunk", "const q = `text spawn(b) more`\nspawn(a)\n", []string{"spawn(a)"}, []string{"spawn(b)"}},
		{"an interpolation is code", "const q = `head ${spawn(a)} tail spawn(b)`\n", []string{"spawn(a)"}, []string{"spawn(b)"}},
		{"a quoted brace does not close an interpolation", "const q = `head ${ f('}') + spawn(a) } tail spawn(b)`\n", []string{"spawn(a)"}, []string{"spawn(b)"}},
		{"a comment inside an interpolation", "const q = `head ${ /* spawn(b) */ spawn(a) } tail`\n", []string{"spawn(a)"}, []string{"spawn(b)"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stripNonCode(c.src)
			if len(got) != len(c.src) {
				t.Fatalf("the scanner must preserve length: %d vs %d", len(got), len(c.src))
			}
			if strings.Count(got, "\n") != strings.Count(c.src, "\n") {
				t.Fatalf("the scanner must preserve lines: %q", got)
			}
			for _, k := range c.keep {
				if !strings.Contains(got, k) {
					t.Fatalf("code %q was blanked: %q", k, got)
				}
			}
			for _, d := range c.drop {
				if strings.Contains(got, d) {
					t.Fatalf("prose %q survived: %q", d, got)
				}
			}
		})
	}
}

// Regex literals, the one construct the scanner needed grammar for. Leaving them
// out failed in the direction a backstop cannot report: `/['"]/` — the ordinary
// way to match a quote — opened a string that ran to the end of its line and
// blanked the code after it.
func TestStripNonCode_regexLiterals(t *testing.T) {
	cases := []struct {
		name       string
		src        string
		keep, drop []string
	}{
		{"a quote class does not open a literal", `const re = /['"]/ ; spawn(a)`, []string{"spawn(a)"}, nil},
		{"a regex body is not code", `const re = /spawn\(b\)/ ; spawn(a)`, []string{"spawn(a)"}, []string{"spawn(b)"}},
		{"a slash inside a character class does not close it", `const re = /[/'"]x/ ; spawn(a)`, []string{"spawn(a)"}, nil},
		{"division is not a regex", `const n = a / b; const m = c / d; spawn(a)`, []string{"spawn(a)", "a / b", "c / d"}, nil},
		{"a regex inside an interpolation", "const q = `x ${ /['\"]/ .source + spawn(a) } y spawn(b)`", []string{"spawn(a)"}, []string{"spawn(b)"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stripNonCode(c.src)
			if len(got) != len(c.src) {
				t.Fatalf("length must be preserved: %d vs %d", len(got), len(c.src))
			}
			for _, k := range c.keep {
				if !strings.Contains(got, k) {
					t.Fatalf("code %q was blanked: %q", k, got)
				}
			}
			for _, d := range c.drop {
				if strings.Contains(got, d) {
					t.Fatalf("a regex body is not a call site, %q survived: %q", d, got)
				}
			}
		})
	}
}

// B1, mocker slice A round 24: the "required field the prompt never mentions"
// WARN is the one check here that asks whether evidence is ABSENT, so it is the
// one a partially-read prompt can turn into a false report — and it did, twice
// over. Once because the resolver dropped the head of a long concat chain (fixed
// in wfscript), and structurally because a prompt composed at run time is text
// the checker never sees. A gate whose author closes a WARN by restating a
// keyword has been taught to pad prompts.
func TestRequiredFieldWarn_isSilentOverAPromptItCouldNotRead(t *testing.T) {
	const schema = "const S = { type: 'object', properties: { seams: { type: 'array' } }, required: ['seams'] }\n"
	const wrap = "const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }\n"
	head := "export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }\n"

	t.Run("a prompt it CAN read still fires", func(t *testing.T) {
		r := run(t, head+schema+wrap+"phase('P')\nawait spawn(`go`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\nreturn {}")
		if !has(r, WARN, "never mentions") {
			t.Fatalf("the check must still fire on text it read:\n%s", dump(r))
		}
	})

	t.Run("a run-time prompt does not", func(t *testing.T) {
		r := run(t, head+schema+wrap+"phase('P')\nawait spawn(`go ` + someRuntimeValue, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\nreturn {}")
		if has(r, WARN, "never mentions") {
			t.Fatalf("absence over unread text is not evidence:\n%s", dump(r))
		}
		if !has(r, INFO, "composed at run time") {
			t.Fatalf("and the report must say the check did not cover it:\n%s", dump(r))
		}
	})

	// A prompt the checker only partly read measures a LOWER BOUND, and the
	// size line must not read as a measurement: an 8.3 KB prompt metered at
	// 4.5 KB is a size budget read off fiction.
	t.Run("the size meter says lower bound", func(t *testing.T) {
		r := run(t, head+schema+wrap+"phase('P')\nawait spawn(`go ` + someRuntimeValue, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\nreturn {}")
		if !has(r, INFO, "at LEAST") {
			t.Fatalf("want the largest-prompt line marked as a floor:\n%s", dump(r))
		}
		if !has(r, INFO, "every size above is a lower bound") {
			t.Fatalf("want the population of unread prompts:\n%s", dump(r))
		}
	})
}

// B2, mocker slice A: run from the gate workspace with no -root, every ownership
// path in a correct script came back "does not exist" — twenty-two of them —
// because the default root was the working directory and a script's owned paths
// are relative to the TARGET. gate.json already records the target.
func TestRootDefaultsToTheWorkspaceTarget(t *testing.T) {
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"src/real.ts", "x_test.go"} {
		if err := os.WriteFile(filepath.Join(target, f), []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "gate.json"), []byte(`{"name":"g","target":`+strconv.Quote(target)+`,"round":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(work, "s.mjs")
	body := preamble + "phase('P')\nawait spawn(`YOU OWN\\n  src/real.ts — the thing\\n`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\nreturn {}"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	rep, err := Run(script, "")
	if err != nil {
		t.Fatal(err)
	}
	if has(rep, WARN, "does not exist") {
		t.Fatalf("the owned path exists in the target; the workspace is the wrong tree to ask:\n%s", dump(rep))
	}
	// A root the reader did not ask for must be named, or the next divergence
	// between the two trees reads as a script defect.
	if !has(rep, INFO, "the target of gate workspace") {
		t.Fatalf("want the report to say which tree it read:\n%s", dump(rep))
	}

	// An explicit -root still wins — it is the only way to check a script
	// against a tree the workspace does not name.
	if rep, err := Run(script, work); err != nil {
		t.Fatal(err)
	} else if !has(rep, WARN, "does not exist") {
		t.Fatalf("an explicit root is not overridden:\n%s", dump(rep))
	}
}

// B3, mocker slice A rounds 19 and 20: a section owned files that appeared in no
// gate's `owns`, so a reviewer briefed on them could file a blocker the fix agent
// that gate spawns was forbidden to touch. Both blockers passed `wfcheck` 0/0 and
// `wfdry` 7/7 — neither is reachable by a dry mode, because both are properties
// of prompt text and ownership lists rather than of control flow.
func TestFixerReach(t *testing.T) {
	head := "export const meta = { name: 'x', description: 'd', phases: [{ title: 'Build' }, { title: 'Gate 1' }] }\n"
	const wrap = "const S = { type: 'object' }\nconst spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }\nconst runGate = async (cfg) => spawn('YOU OWN\\n' + cfg.owns.map((p) => '  ' + p).join('\\n'), { model: 'sonnet', effort: 'high', label: 'fix:' + cfg.slug, phase: cfg.phase, schema: S })\n"
	producer := func(own string) string {
		return "phase('Build')\nawait spawn(`YOU OWN\\n  " + own + " — NEW, you create it\\n`, { model: 'sonnet', effort: 'low', label: 'build', phase: 'Build', schema: S })\n"
	}
	// A gate is recognised by `owns` AND `reviewers` together: `owns` alone is a
	// key an author writes on an agent's own options object, and counting one
	// made a script with no gate report full coverage.
	const revs = ", reviewers: [{ slug: 'correctness', vector: 'v', model: 'sonnet' }]"
	gate := func(owns string) string {
		return "phase('Gate 1')\nawait runGate({ slug: 'gate1', phase: 'Gate 1', owns: [" + owns + "]" + revs + " })\nreturn {}"
	}

	t.Run("a path no gate owns", func(t *testing.T) {
		r := run(t, head+wrap+producer("src/seam.ts")+gate("'src/other.ts'"))
		if !has(r, WARN, "no gate lists it in `owns`") {
			t.Fatalf("want the reach WARN:\n%s", dump(r))
		}
		if !has(r, INFO, "0 reachable by a fix agent, 1 NOT") {
			t.Fatalf("and the population must carry the count:\n%s", dump(r))
		}
	})

	// The canonical shape the manual prescribes: a producer in phase Build, the
	// gate reviewing it in phase Gate 1. Joining the two on the phase TITLE
	// reported every correct script, which is why the reach is script-wide.
	t.Run("the prescribed producer/gate phase split is not a finding", func(t *testing.T) {
		r := run(t, head+wrap+producer("src/seam.ts")+gate("'src/seam.ts'"))
		if has(r, WARN, "no gate lists it in `owns`") {
			t.Fatalf("a producer and its gate live in different phases by design:\n%s", dump(r))
		}
	})

	// An owns array it could not read in full disables the check for the whole
	// script and says so — reporting the unseen as missing is the phantom-ERROR
	// failure this checker was rebuilt to end.
	t.Run("an unreadable owns array reports nothing", func(t *testing.T) {
		r := run(t, head+wrap+producer("src/seam.ts")+"phase('Gate 1')\nawait runGate({ slug: 'gate1', phase: 'Gate 1', owns: someRuntimeList"+revs+" })\nreturn {}")
		if has(r, WARN, "no gate lists it in `owns`") {
			t.Fatalf("what it could not see is not missing:\n%s", dump(r))
		}
		if !has(r, INFO, "could not be read in full") {
			t.Fatalf("and it must say the check was skipped:\n%s", dump(r))
		}
	})

	// An `owns` behind a NAME is the shape both scripts of the gate this check
	// was derived from write on their closing gate: `owns: closingOwns`, a plain
	// array one screen above. A type assertion to *ast.ArrayLiteral saw nothing
	// through it, so the gate counted as unreadable and the check disabled
	// itself for the whole script — on both.
	t.Run("an owns array behind a const name is read", func(t *testing.T) {
		src := head + wrap + "const closingOwns = ['src/seam.ts']\n" + producer("src/seam.ts") +
			"phase('Gate 1')\nawait runGate({ slug: 'gate1', phase: 'Gate 1', owns: closingOwns" + revs + " })\nreturn {}"
		r := run(t, src)
		if has(r, INFO, "could not be read in full") {
			t.Fatalf("the const is right there in the tree:\n%s", dump(r))
		}
		if !has(r, INFO, "1 reachable by a fix agent, 0 NOT") {
			t.Fatalf("and it must be compared:\n%s", dump(r))
		}
	})

	// A gate's entry is a SENTENCE about a path, not a path: 19 entries of one
	// live script read `internal/admin/server.go (the one D7 comment, nothing
	// else)`. Whole-string equality drew a WARN on every one of them.
	t.Run("a prose owns entry is read for the path it names", func(t *testing.T) {
		r := run(t, head+wrap+producer("src/seam.ts")+gate("'src/seam.ts (the one comment above the route, nothing else)'"))
		if has(r, WARN, "no gate lists it in `owns`") {
			t.Fatalf("the entry names the path it owns:\n%s", dump(r))
		}
	})

	// A gate owning a directory reaches what is under it.
	t.Run("a directory entry reaches the files beneath it", func(t *testing.T) {
		r := run(t, head+wrap+producer("src/deep/seam.ts")+gate("'src/deep/ (the new table, helper and test files)'"))
		if has(r, WARN, "no gate lists it in `owns`") {
			t.Fatalf("the directory covers the file:\n%s", dump(r))
		}
	})

	// An entry naming nothing this tool recognises is an entry it did not read,
	// and it must say so rather than judge the rest against a short list.
	t.Run("an entry naming no path makes the gate incomplete", func(t *testing.T) {
		r := run(t, head+wrap+producer("src/seam.ts")+gate("'the four new test files under src'"))
		if has(r, WARN, "no gate lists it in `owns`") {
			t.Fatalf("a list it could not read cannot convict:\n%s", dump(r))
		}
		if !has(r, INFO, "NOT JUDGED") {
			t.Fatalf("and the unjudged remainder must be named:\n%s", dump(r))
		}
	})

	// The founding false green of this check: an agent's OWN options object
	// carries an `owns` list in the shape the emitted harness uses, so a script
	// with no gate at all reported full coverage.
	t.Run("an agent options object is not a gate config", func(t *testing.T) {
		src := head + wrap + "phase('Build')\nawait spawn(`YOU OWN\\n  src/seam.ts — NEW, you create it\\n`, { model: 'sonnet', effort: 'low', label: 'build', phase: 'Build', schema: S, owns: ['src/seam.ts'] })\nreturn {}"
		r := run(t, src)
		if has(r, INFO, "reachable by a fix agent") {
			t.Fatalf("no gate spawns a fix agent here:\n%s", dump(r))
		}
		if !has(r, INFO, "the reviewer/fixer reach check covered nothing") {
			t.Fatalf("want the population line:\n%s", dump(r))
		}
	})

	// A JSON schema with a property NAMED owns switched the whole check off,
	// script-wide, as an INFO — the `effort:`-inside-a-schema-description class,
	// re-entering through a property name.
	t.Run("a schema property named owns disables nothing", func(t *testing.T) {
		// The trap is the `properties` object, which carries BOTH keys — so the
		// gate shape alone does not exclude it, and its `owns` is a schema node
		// rather than an array. It used to set OwnsComplete=false and switch the
		// whole check off, script-wide, as an INFO naming the schema as a gate.
		src := head + "const GATE_SCHEMA = { type: 'object', properties: { owns: { type: 'array', description: 'the paths you own' }, reviewers: { type: 'array' } } }\n" +
			wrap + producer("src/seam.ts") + gate("'src/other.ts'")
		r := run(t, src)
		if !has(r, WARN, "no gate lists it in `owns`") {
			t.Fatalf("a schema is not a gate, and must not silence the check:\n%s", dump(r))
		}
	})

	// Populations, both directions.
	t.Run("no gate config at all is a population, not a pass", func(t *testing.T) {
		r := run(t, head+wrap+producer("src/seam.ts")+"return {}")
		if !has(r, INFO, "the reviewer/fixer reach check covered nothing") {
			t.Fatalf("want the population line:\n%s", dump(r))
		}
	})

	t.Run("a gate path nobody produces is a note", func(t *testing.T) {
		r := run(t, head+wrap+producer("src/seam.ts")+gate("'src/seam.ts', 'src/nobody.ts'"))
		if !has(r, INFO, "declares owning: gate1: src/nobody.ts") {
			t.Fatalf("want the stale-entry note:\n%s", dump(r))
		}
		// It is the ABSENCE half of the check, so it must carry its bound: a
		// harness composes its prompts, and a note that waits for a script
		// with none never prints.
		if !has(r, INFO, "CANDIDATE list, not a verdict") {
			t.Fatalf("and the unread prompts must bound it:\n%s", dump(r))
		}
		if has(r, WARN, "no gate lists it in `owns`") {
			t.Fatalf("the extra gate path is not a reach failure:\n%s", dump(r))
		}
	})
}

// resolveRoot decides which tree the file-existence checks read. Every branch is
// pinned: the silent fallback is ONLY "no workspace above here", and each way of
// having a workspace it cannot use says so — a check answering about the wrong
// tree and exiting zero is the failure this default was added to remove.
func TestResolveRoot(t *testing.T) {
	newWS := func(t *testing.T, manifest string) string {
		t.Helper()
		dir := t.TempDir()
		wsDir := filepath.Join(dir, "gate")
		if err := os.MkdirAll(wsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wsDir, "gate.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		return wsDir
	}
	chdir := func(t *testing.T, dir string) {
		t.Helper()
		old, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(old) })
	}

	t.Run("an explicit root wins and says nothing", func(t *testing.T) {
		got, note := resolveRoot("/somewhere")
		if got != "/somewhere" || note != "" {
			t.Fatalf("got %q, note %q", got, note)
		}
	})

	t.Run("the workspace target, with a note naming it", func(t *testing.T) {
		target := t.TempDir()
		wsDir := newWS(t, `{"name":"g","target":"`+target+`"}`)
		chdir(t, wsDir)
		got, note := resolveRoot("")
		if got != target {
			t.Fatalf("got %q, want the target %q", got, target)
		}
		if !strings.Contains(note, target) {
			t.Fatalf("a resolution the reader did not ask for must name itself: %q", note)
		}
	})

	// The tool would otherwise stat every owned path under the workspace and
	// report each as missing — the twenty-two-WARN noise, with the reader told
	// nothing about why.
	t.Run("a target that is not a directory is named, not swallowed", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "not-a-dir")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		wsDir := newWS(t, `{"name":"g","target":"`+file+`"}`)
		chdir(t, wsDir)
		got, note := resolveRoot("")
		if got == file {
			t.Fatal("a file is not a tree to check against")
		}
		if !strings.Contains(note, "not a directory") {
			t.Fatalf("want the note naming it: %q", note)
		}
	})

	t.Run("a workspace that cannot be read is named", func(t *testing.T) {
		wsDir := newWS(t, `{"name": broken`)
		chdir(t, wsDir)
		_, note := resolveRoot("")
		if !strings.Contains(note, "could not be read") {
			t.Fatalf("a manifest that will not parse is not the absence of a workspace: %q", note)
		}
	})

	t.Run("no workspace at all is the silent case", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)
		got, note := resolveRoot("")
		if note != "" {
			t.Fatalf("the ordinary case says nothing: %q", note)
		}
		if got == "" {
			t.Fatal("want the working directory")
		}
	})
}

// The denominator counts prompts that CLAIM ownership, not prompts the parser
// managed to read. Collapsing it onto its numerator turned `0 owned path(s)
// parsed from 4 prompt(s)` — the line authoring.md tells an author to read as
// "your block is in a form this tool cannot parse" — into `from 0 prompt(s)`,
// which reads as a script that declares nothing.
func TestOwnershipDenominatorCountsUnreadableBlocks(t *testing.T) {
	// Bulleted entries: the line does not START with the path, so ownedPaths
	// rejects every one of them.
	r := run(t, preamble+`phase('P')
await spawn(`+"`YOU OWN\n  - src/app/one.ts — the entry point\n  - src/app/two.ts — and its test\n`"+`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
return { done: true }`)
	if !has(r, INFO, "0 owned path(s) parsed from 1 prompt(s) declaring YOU OWN") {
		t.Fatalf("a prompt whose block could not be parsed still declares one:\n%s", dump(r))
	}
}

// NEW is read off the declaring line, and a path declared twice keeps it if
// EITHER line carries it. First-occurrence-wins made the flag order-dependent:
// the same two lines swapped drew a "does not exist and is not marked NEW" WARN
// on a file the script creates.
func TestNewFlagIsOrderIndependent(t *testing.T) {
	for _, order := range []string{
		"YOU OWN\n  src/gone.ts — NEW, you create it\n  src/gone.ts — and you also finish it\n",
		"YOU OWN\n  src/gone.ts — and you also finish it\n  src/gone.ts — NEW, you create it\n",
	} {
		r := run(t, preamble+`phase('P')
await spawn(`+"`"+order+"`"+`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
return { done: true }`)
		if has(r, WARN, "is not marked NEW") {
			t.Fatalf("one declaration marks it NEW, in either order:\n%s", dump(r))
		}
	}
}
