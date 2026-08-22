// Every fixture here is a script shape that DEFEATED the text scanner this
// package replaces. They are kept as tests rather than as a changelog entry
// because the method they broke — matching structure with regular expressions —
// is the thing that must not come back.
package wfscript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, body string) *Script {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.mjs")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The canonical shape, and the line numbers must survive the parse wrapper.
func TestAgentCalls_canonical(t *testing.T) {
	s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const r = await spawn('do the thing', { model: 'sonnet', effort: 'low', label: 'worker', schema: S })
return { done: true }
`)
	calls := s.AgentCalls()
	if len(calls) != 1 {
		t.Fatalf("want 1 call site, got %d: %+v", len(calls), calls)
	}
	c := calls[0]
	if c.Line != 4 {
		t.Fatalf("line must be the file's own, got %d", c.Line)
	}
	if c.Label != "worker" || !c.Labelled {
		t.Fatalf("label = %q labelled=%v", c.Label, c.Labelled)
	}
	if !c.PromptOK || c.Prompt != "do the thing" {
		t.Fatalf("prompt = %q ok=%v", c.Prompt, c.PromptOK)
	}
	if _, ok := c.Opt("effort"); !ok {
		t.Fatal("effort must be seen as an option")
	}
}

// The founding incident, reproduced by the audit: an unrecognised wrapper name
// plus an options FACTORY reported "0 agent() call sites, 0 errors, exit 0" on
// a script with five of them. A function is identified by what it does.
func TestAgentCalls_unknownWrapperNameAndOptionsFactory(t *testing.T) {
	s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object' }
const opts = (label, phaseName) => ({ label, schema: S, model: 'opus', effort: 'high', phase: phaseName })
const dispatch = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const a = await dispatch(`+"`one`"+`, opts('a', 'P'))
const b = await dispatch(`+"`two`"+`, opts('b', 'P'))
return { done: true }
`)
	if got := len(s.AgentCalls()); got != 2 {
		t.Fatalf("want 2 call sites through the unrecognised wrapper, got %d", got)
	}
}

// A wrapper that carries the prompt inside the options object and takes the
// label positionally — the shape the guide's own `spawn('kernel:door', {...})`
// example implies. The text scanner inventoried the wrapper's own body as a
// call site and printed three phantom ERRORs on it.
func TestAgentCalls_promptTravelsInTheOptions(t *testing.T) {
	s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const spawn = async (label, o) => { try { return await agent(o.prompt, o) } catch { return null } }
phase('P')
const r = await spawn('kernel:door', { prompt: `+"`build the door`"+`, model: 'sonnet', effort: 'medium' })
return { done: true }
`)
	calls := s.AgentCalls()
	if len(calls) != 1 {
		t.Fatalf("the wrapper body is not a call site; want 1, got %d: %+v", len(calls), calls)
	}
	c := calls[0]
	if c.Label != "kernel:door" || !c.Labelled {
		t.Fatalf("positional label not read: %q", c.Label)
	}
	if !c.PromptOK || c.Prompt != "build the door" {
		t.Fatalf("prompt in the options not read: %q", c.Prompt)
	}
}

// An options object built by spreading a shared constant is a legitimate and
// common shape. The text scanner reported model, effort, schema and phase as
// MISSING on it — four false ERRORs on a correct call, which is how a reader
// learns to ignore the real ones.
func TestAgentCalls_spreadOptionsAreResolved(t *testing.T) {
	s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object' }
const BASE = { model: 'opus', effort: 'high', schema: S, phase: 'P' }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const r = await spawn(`+"`go`"+`, { ...BASE, model: 'sonnet', label: 'one' })
return { done: true }
`)
	c := s.AgentCalls()[0]
	if !c.OptsComplete {
		t.Fatal("a spread of a known const object is fully resolvable")
	}
	for _, k := range []string{"model", "effort", "schema", "phase", "label"} {
		if _, ok := c.Opt(k); !ok {
			t.Fatalf("%q must be visible through the spread: %+v", k, c.Opts)
		}
	}
	// The later property wins, as JavaScript says.
	if v, _ := c.Opt("model"); true {
		if txt, ok := s.StaticString(v); !ok || txt != "sonnet" {
			t.Fatalf("the overriding model must win, got %q", txt)
		}
	}
}

// `effort:` inside a schema DESCRIPTION satisfied the text scanner's check that
// every call declares an effort. A property is a property.
func TestAgentCalls_effortInsideADescriptionIsNotAnOption(t *testing.T) {
	s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const r = await spawn(`+"`go`"+`, { model: 'sonnet', label: 'w', phase: 'P',
  schema: { type: 'object', properties: { b: { type: 'array', description: 'how much effort: high or low' } } } })
return { done: true }
`)
	c := s.AgentCalls()[0]
	if _, ok := c.Opt("effort"); ok {
		t.Fatal("a nested description is not a call-level effort option")
	}
}

// An option value from a const is statically knowable, and the check that reads
// the enum must see it rather than shrug at "not a literal".
func TestAgentCalls_optionValueFromAConst(t *testing.T) {
	s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const DEEP = 'xhgih'
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const r = await spawn(`+"`go`"+`, { model: 'sonnet', effort: DEEP, label: 'w' })
return { done: true }
`)
	e, ok := s.AgentCalls()[0].Opt("effort")
	if !ok {
		t.Fatal("effort must be seen")
	}
	if txt, ok := s.StaticString(e); !ok || txt != "xhgih" {
		t.Fatalf("the const value must resolve, got %q ok=%v", txt, ok)
	}
}

// A phase() written across lines with double quotes vanished from the text
// scanner, and the comparison against meta.phases was then skipped because one
// side was empty — so a declared phase nobody calls passed.
func TestPhaseTitles_multilineAndDoubleQuoted(t *testing.T) {
	s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'Actual' }] }
phase(
  "Actual"
)
return { done: true }
`)
	got := s.PhaseTitles()
	if len(got) != 1 || got[0] != "Actual" {
		t.Fatalf("want [Actual], got %v", got)
	}
}

// One guarded loop laundered every other loop, because the guard check asked
// whether the string occurred anywhere in the file.
func TestLoops_areJudgedOneByOne(t *testing.T) {
	s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
while (budget.total && false) { log('never') }
let n = 0
while (true) { n++; if (n > 2) break }
return { done: true }
`)
	loops := s.Loops()
	if len(loops) != 2 {
		t.Fatalf("want 2 loops, got %+v", loops)
	}
	if !loops[0].Guarded {
		t.Fatal("the first loop guards on budget.total")
	}
	if loops[1].Guarded {
		t.Fatal("the second loop does NOT guard, and must not inherit the first's guard")
	}
}

// The three answers, one case per way the resolver was caught answering wrongly.
// A WRONG value is a defect; an honest "unreadable" is correct even where a
// human could work the value out; a false "absent" is a defect too, because the
// checks above turn it into an ERROR on correct code.
func TestThreeAnswers(t *testing.T) {
	const S = "const S = { type: 'object', properties: { findings: { type: 'array' } } }\n"

	t.Run("a branching wrapper does not bind every call to the fallback branch", func(t *testing.T) {
		s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
`+S+`const BASE = { model: 'opus', effort: 'high', label: 'fallback', phase: 'P', schema: S }
const spawn = async (p, o) => { if (!o) { return await agent(p, BASE) } return await agent(p, o) }
phase('P')
await spawn(`+"`review a`"+`, { model: 'sonnet', effort: 'turbo', label: 'alpha', phase: 'P', schema: S })
return {}`)
		c := s.AgentCalls()[0]
		if c.OptsComplete {
			t.Fatalf("the branches disagree about the options; that is unreadable, not resolved: %+v", c.Opts)
		}
		if v, ok := c.OptStr("effort"); ok && v == "high" {
			t.Fatal("the fallback branch's value must not be reported as this call's")
		}
		if !c.PromptOK || !strings.Contains(c.Prompt, "review a") {
			t.Fatalf("both branches forward the same prompt, so it stays readable: %q", c.Prompt)
		}
	})

	t.Run("a destructured parameter binds nothing to the globals", func(t *testing.T) {
		s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
`+S+`const prompt = 'A GLOBAL PROMPT'
const model = 'opus'
const spawn = async ({ prompt, ...opts }) => { try { return await agent(prompt, opts) } catch { return null } }
phase('P')
await spawn({ prompt: `+"`the real one`"+`, model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })
return {}`)
		c := s.AgentCalls()[0]
		if c.PromptOK {
			t.Fatalf("a destructured binding is unreadable, not the global: %q", c.Prompt)
		}
		if c.OptsComplete {
			t.Fatal("...and so are its options")
		}
		if !c.OptsFound || !c.PromptSet {
			t.Fatal("unreadable is not absent: both are present")
		}
	})

	t.Run("a loop variable does not resolve to a const of the same name", func(t *testing.T) {
		s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
`+S+`const target = 'src/shared.go'
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
for (const target of ['src/a.go', 'src/b.go']) {
  await spawn(`+"`YOU OWN\\n${target}\\n`"+`, { model: 'sonnet', effort: 'low', label: target, phase: 'P', schema: S })
}
return {}`)
		c := s.AgentCalls()[0]
		if c.PromptOK && strings.Contains(c.Prompt, "shared") {
			t.Fatalf("the loop variable shadows the const: %q", c.Prompt)
		}
		if v, _ := c.OptStr("label"); v == "src/shared.go" {
			t.Fatal("the label must not come from the shadowed const")
		}
	})

	// Isolates the shadow set itself: no wrapper opacity, no factory scope — a
	// block-scoped const with the same name as a top-level one. Answering from
	// the top level here is answering with a value the call cannot see.
	t.Run("a block-scoped const shadows a top-level one", func(t *testing.T) {
		s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
`+S+`const effort = 'high'
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
{
  const effort = 'turbo'
  await spawn(`+"`go`"+`, { model: 'sonnet', effort, label: 'w', phase: 'P', schema: S })
}
return {}`)
		c := s.AgentCalls()[0]
		if v, ok := c.OptStr("effort"); ok && v == "high" {
			t.Fatal("the inner const shadows the outer; 'high' is the wrong answer")
		}
	})

	t.Run("a factory forwarding an expression resolves it, not a global", func(t *testing.T) {
		s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
`+S+`const who = 'GLOBAL'
const inner = (label) => ({ label, model: 'opus', effort: 'high', phase: 'P', schema: S })
const outer = (who) => ({ ...inner(who + '-1') })
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(`+"`go`"+`, outer('alpha'))
return {}`)
		v, ok := s.AgentCalls()[0].OptStr("label")
		if ok && v == "GLOBAL" {
			t.Fatal("the callee's parameter leaked to a same-named global")
		}
		if !ok || v != "alpha-1" {
			t.Fatalf("the expression resolves in the caller's scope: want alpha-1, got %q (ok=%v)", v, ok)
		}
	})

	t.Run("an option value is resolved where it is written", func(t *testing.T) {
		s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
`+S+`const effort = 'high'
const mkOpts = (effort) => ({ model: 'opus', effort: `+"`${effort}`"+`, label: 'alpha', phase: 'P', schema: S })
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(`+"`go`"+`, mkOpts('turbo'))
return {}`)
		if v, ok := s.AgentCalls()[0].OptStr("effort"); !ok || v != "turbo" {
			t.Fatalf("want the factory's own argument, got %q (ok=%v)", v, ok)
		}
	})

	t.Run("a defaulted parameter is not absent", func(t *testing.T) {
		s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
`+S+`const BASE = { model: 'opus', effort: 'high', label: 'w', phase: 'P', schema: S }
const spawn = async (p, o = BASE) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(`+"`go`"+`)
return {}`)
		c := s.AgentCalls()[0]
		if !c.OptsFound {
			t.Fatal("a default makes the options present, not absent")
		}
		if v, ok := c.OptStr("model"); !ok || v != "opus" {
			t.Fatalf("the default's value must resolve, got %q", v)
		}
	})

	t.Run("a positional argument is a label only when it is named like one", func(t *testing.T) {
		s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
`+S+`const spawn = async (p, o, model) => { try { return await agent(p, { ...o, model }) } catch { return null } }
phase('P')
await spawn(`+"`go`"+`, { effort: 'low', label: 'alpha', phase: 'P', schema: S }, 'sonnet')
return {}`)
		if got := s.AgentCalls()[0].Label; got != "alpha" {
			t.Fatalf("an explicit label: wins over a positional guess, got %q", got)
		}
	})

	t.Run("a rebindable factory is not inlined", func(t *testing.T) {
		s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
`+S+`let mk = () => ({ model: 'opus', effort: 'high', label: 'w', phase: 'P', schema: S })
mk = () => ({ model: 'opus', effort: 'turbo', label: 'w', phase: 'P', schema: S })
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(`+"`go`"+`, mk())
return {}`)
		c := s.AgentCalls()[0]
		if v, ok := c.OptStr("effort"); ok && v == "high" {
			t.Fatal("the dead declaration is not the live value")
		}
		if c.OptsComplete {
			t.Fatal("a rebindable binding is unreadable")
		}
	})

	t.Run("null and undefined are absent", func(t *testing.T) {
		for _, arg := range []string{"null", "undefined"} {
			s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
`+S+`const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(`+"`go`"+`, `+arg+`)
return {}`)
			if s.AgentCalls()[0].OptsFound {
				t.Fatalf("%s is absent, not unreadable", arg)
			}
		}
	})

	t.Run("a wrapper on a wrapper is a call site", func(t *testing.T) {
		s := load(t, `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
`+S+`const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
const review = (p, o) => spawn(p, { ...o, effort: 'max' })
phase('P')
await review(`+"`a`"+`, { model: 'opus', label: 'one', phase: 'P', schema: S })
await review(`+"`b`"+`, { model: 'opus', label: 'two', phase: 'P', schema: S })
return {}`)
		if got := len(s.AgentCalls()); got != 2 {
			t.Fatalf("want 2 call sites through the outer wrapper, got %d", got)
		}
	})
}
