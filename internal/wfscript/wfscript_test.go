// Every fixture here is a script shape that DEFEATED the text scanner this
// package replaces. They are kept as tests rather than as a changelog entry
// because the method they broke — matching structure with regular expressions —
// is the thing that must not come back.
package wfscript

import (
	"os"
	"path/filepath"
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
