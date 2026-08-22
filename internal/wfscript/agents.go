package wfscript

import (
	"strings"

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/file"
)

// Wrapper is a function of the script that forwards to the global `agent`.
//
// The guide requires the wrapper to be called `spawn`, and the previous checker
// enforced that by keeping a list of four acceptable names — which is exactly
// how it once reported "1 call site" for a script with nineteen, and how an
// audit later reported "0 call sites, 0 errors, exit 0" for a script with five.
// A name cannot identify a function. What identifies it is that its body calls
// `agent`, and the parser can see that, so the naming rule stays a rule the
// checker REPORTS rather than the mechanism it depends on.
type Wrapper struct {
	Name   string
	Line   int
	Params []string

	// PromptExpr and OptsExpr are the forwarding call's arguments AS WRITTEN,
	// not the index of a parameter. That is the difference between reading a
	// wrapper and guessing at one: the two commonest shapes in the guide pass
	// an expression rather than a bare parameter —
	// `agent(p, {...o, phase: PHASE})` injects the phase, and
	// `agent(\u0060${CONTRACT}\n\n${p}\u0060, o)` prefixes the shared contract. Reading
	// only bare identifiers made both resolve to "no argument", so every call
	// site drew "no options object" and "no prompt argument" while the real
	// `effort: 'nonsense'` beside them went unreported.
	PromptExpr ast.Expression
	OptsExpr   ast.Expression
	LabelIdx   int  // a parameter neither expression references, -1 if none
	Opaque     bool // the wrapper forwards a rest parameter: present but unreadable
	Body       span // so a call inside the wrapper is not counted as a call site
}

type span struct{ from, to file.Idx }

func (s span) contains(i file.Idx) bool { return i >= s.from && i <= s.to }

// AgentCall is one place the script spawns a subagent.
type AgentCall struct {
	Line int
	Node *ast.CallExpression

	// Label is the call's name in a report. Labelled says whether the script
	// actually gave it one: a call named `agent@42` is unlabelled, and the two
	// must not be confused — the previous checker treated the first string
	// argument of every call as a label, which silenced the missing-label
	// warning for every script whose wrapper takes the prompt first.
	Label     string
	Labelled  bool
	Wrapper   string // "" for a direct agent() call
	Prompt    string
	PromptSet bool // a prompt argument was found at all
	PromptOK  bool // ...and its text is statically known

	// Opts are the call's TOP-LEVEL option properties, spreads merged.
	// OptsComplete is false when a spread could not be resolved, and every
	// check that reports a missing option must respect it: reporting what it
	// could not see as absent is how a correct `{...BASE, model:'sonnet'}` call
	// drew four phantom ERRORs.
	Opts         map[string]ast.Expression
	OptsComplete bool
	OptsFound    bool
}

// Opt returns one option's expression.
func (a AgentCall) Opt(name string) (ast.Expression, bool) {
	e, ok := a.Opts[name]
	return e, ok
}

// Wrappers finds every function that forwards to the global `agent`.
func (s *Script) Wrappers() []Wrapper {
	var out []Wrapper
	seen := map[string]bool{}
	add := func(name string, idx file.Idx, init ast.Expression) {
		if name == "" || seen[name] || init == nil {
			return
		}
		if w, ok := s.wrapperOf(name, init); ok {
			w.Line = s.Line(idx)
			seen[name] = true
			out = append(out, w)
		}
	}
	// Every binding in the program, not only the top-level ones: a wrapper
	// declared inside an `if` or a block was invisible, and its own forwarding
	// call was then inventoried as the script's single "call site" while the
	// real ones vanished.
	s.Walk(func(n ast.Node) {
		switch v := n.(type) {
		case *ast.LexicalDeclaration:
			for _, b := range v.List {
				if id, ok := b.Target.(*ast.Identifier); ok {
					add(string(id.Name), id.Idx, b.Initializer)
				}
			}
		case *ast.VariableDeclaration:
			for _, b := range v.List {
				if id, ok := b.Target.(*ast.Identifier); ok {
					add(string(id.Name), id.Idx, b.Initializer)
				}
			}
		case *ast.FunctionDeclaration:
			if v.Function != nil {
				add(string(v.Function.Name.Name), v.Function.Idx0(), v.Function)
			}
		}
	})
	// An ALIAS is a call site under another name: `const route = spawn` made
	// half a script's calls invisible with no warning.
	for again := true; again; {
		again = false
		s.Walk(func(n ast.Node) {
			bind := func(target ast.BindingTarget, idx file.Idx, init ast.Expression) {
				id, ok := target.(*ast.Identifier)
				if !ok || init == nil || seen[string(id.Name)] {
					return
				}
				src, ok := init.(*ast.Identifier)
				if !ok {
					return
				}
				for _, w := range out {
					if w.Name == string(src.Name) {
						alias := w
						alias.Name, alias.Line = string(id.Name), s.Line(idx)
						seen[alias.Name] = true
						out = append(out, alias)
						again = true
						return
					}
				}
			}
			switch v := n.(type) {
			case *ast.LexicalDeclaration:
				for _, b := range v.List {
					if id, ok := b.Target.(*ast.Identifier); ok {
						bind(b.Target, id.Idx, b.Initializer)
					}
				}
			case *ast.VariableDeclaration:
				for _, b := range v.List {
					if id, ok := b.Target.(*ast.Identifier); ok {
						bind(b.Target, id.Idx, b.Initializer)
					}
				}
			}
		})
	}
	return out
}

func (s *Script) wrapperOf(name string, e ast.Expression) (Wrapper, bool) {
	var params []string
	var body ast.Node
	switch fn := e.(type) {
	case *ast.ArrowFunctionLiteral:
		params, body = paramNames(fn.ParameterList), fn.Body
	case *ast.FunctionLiteral:
		params, body = paramNames(fn.ParameterList), fn.Body
	default:
		return Wrapper{}, false
	}
	if body == nil {
		return Wrapper{}, false
	}
	// The forwarding call is the one that says how this wrapper is wired: which
	// parameter becomes the prompt and which becomes the options.
	var fwd *ast.CallExpression
	walk(body, func(n ast.Node) {
		if fwd != nil {
			return
		}
		c, ok := n.(*ast.CallExpression)
		if !ok {
			return
		}
		if id, ok := c.Callee.(*ast.Identifier); ok && string(id.Name) == "agent" {
			fwd = c
		}
	})
	if fwd == nil {
		return Wrapper{}, false
	}
	w := Wrapper{Name: name, Params: params, LabelIdx: -1, Body: span{body.Idx0(), body.Idx1()}}
	if pl := paramList(e); pl != nil && pl.Rest != nil {
		w.Opaque = true
	}
	for _, a := range fwd.ArgumentList {
		if _, ok := a.(*ast.SpreadElement); ok {
			// `agent(...args)` forwards whatever it was given: the arguments
			// exist and cannot be read here. That is reported as unreadable,
			// never as absent.
			w.Opaque = true
		}
	}
	if len(fwd.ArgumentList) > 0 {
		w.PromptExpr = fwd.ArgumentList[0]
	}
	if len(fwd.ArgumentList) > 1 {
		w.OptsExpr = fwd.ArgumentList[1]
	}
	// A wrapper written `spawn('label', {...})` passes the prompt inside the
	// options; a parameter neither forwarded expression mentions is the
	// positional label.
	used := map[string]bool{}
	for _, e := range []ast.Expression{w.PromptExpr, w.OptsExpr} {
		if e == nil {
			continue
		}
		walk(e, func(n ast.Node) {
			if id, ok := n.(*ast.Identifier); ok {
				used[string(id.Name)] = true
			}
		})
	}
	for i, p := range params {
		if p == "" || used[p] {
			continue
		}
		if l := strings.ToLower(p); l == "label" || l == "name" || l == "id" || w.LabelIdx == -1 {
			w.LabelIdx = i
		}
	}
	return w, true
}

func paramList(e ast.Expression) *ast.ParameterList {
	switch fn := e.(type) {
	case *ast.ArrowFunctionLiteral:
		return fn.ParameterList
	case *ast.FunctionLiteral:
		return fn.ParameterList
	}
	return nil
}

func paramNames(pl *ast.ParameterList) []string {
	var out []string
	if pl == nil {
		return out
	}
	for _, b := range pl.List {
		if id, ok := b.Target.(*ast.Identifier); ok {
			out = append(out, string(id.Name))
			continue
		}
		out = append(out, "")
	}
	return out
}

// AgentCalls returns every call site that spawns a subagent: a direct `agent()`
// and every call to a wrapper, minus the wrapper's own forwarding call.
func (s *Script) AgentCalls() []AgentCall {
	wraps := map[string]Wrapper{}
	var bodies []span
	for _, w := range s.Wrappers() {
		wraps[w.Name] = w
		bodies = append(bodies, w.Body)
	}
	// One body may back several names (an alias shares it); dedupe so the span
	// list stays small.
	inWrapper := func(i file.Idx) bool {
		for _, b := range bodies {
			if b.contains(i) {
				return true
			}
		}
		return false
	}

	var out []AgentCall
	s.Walk(func(n ast.Node) {
		c, ok := n.(*ast.CallExpression)
		if !ok {
			return
		}
		id, ok := c.Callee.(*ast.Identifier)
		if !ok {
			return
		}
		name := string(id.Name)
		w, isWrap := wraps[name]
		if name != "agent" && !isWrap {
			return
		}
		if inWrapper(c.Idx0()) {
			return
		}
		out = append(out, s.callOf(c, name, w, isWrap))
	})
	// Walk order follows struct fields, which is not source order; a report has
	// to read top to bottom.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Line < out[j-1].Line; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func (s *Script) callOf(c *ast.CallExpression, name string, w Wrapper, isWrap bool) AgentCall {
	a := AgentCall{Line: s.Line(c.Idx0()), Node: c, Opts: map[string]ast.Expression{}, OptsComplete: true}
	arg := func(i int) ast.Expression {
		if i >= 0 && i < len(c.ArgumentList) {
			return c.ArgumentList[i]
		}
		return nil
	}
	read := func(promptExpr, optsExpr ast.Expression, labelIdx int) {
		if optsExpr != nil {
			a.OptsFound = true
			a.Opts, a.OptsComplete = s.ObjectOf(optsExpr)
		}
		if promptExpr != nil {
			a.PromptSet = true
			a.Prompt, a.PromptOK = s.StaticString(promptExpr)
		} else if e, ok := a.Opts["prompt"]; ok {
			// A wrapper that carries the prompt inside the options object.
			a.PromptSet = true
			a.Prompt, a.PromptOK = s.StaticString(e)
		}
		if e := arg(labelIdx); e != nil {
			if txt, ok := s.StaticString(e); ok {
				a.Label, a.Labelled = txt, true
			}
		}
		if !a.Labelled {
			if e, ok := a.Opts["label"]; ok {
				a.Labelled = true
				if txt, ok := s.StaticString(e); ok {
					a.Label = txt
				}
			}
		}
	}
	if !isWrap {
		read(arg(0), arg(1), -1)
	} else {
		a.Wrapper = name
		if w.Opaque {
			// Present and unreadable is its own answer. Reporting it as absent
			// is what produced two false ERRORs per call site on a correct
			// script while hiding a real one beside them.
			a.OptsFound, a.OptsComplete = true, false
			a.PromptSet, a.PromptOK = true, false
		} else {
			sub := map[string]ast.Expression{}
			unbound := map[string]bool{}
			for i, p := range w.Params {
				if p == "" {
					continue
				}
				if i < len(c.ArgumentList) {
					sub[p] = c.ArgumentList[i]
				} else {
					unbound[p] = true
				}
			}
			// A parameter the call passed nothing for is `undefined` at run
			// time — genuinely ABSENT, not merely unreadable. Forwarding it as
			// unresolvable would downgrade "this call has no options at all"
			// into a note.
			absent := func(e ast.Expression) bool {
				id, ok := e.(*ast.Identifier)
				return ok && unbound[string(id.Name)]
			}
			pe, oe := w.PromptExpr, w.OptsExpr
			if pe != nil && absent(pe) {
				pe = nil
			}
			if oe != nil && absent(oe) {
				oe = nil
			}
			s.withScope(sub, w.Params, func() { read(pe, oe, w.LabelIdx) })
		}
	}
	if a.Label == "" {
		a.Label = "agent@" + itoa(a.Line)
	}
	return a
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// PhaseTitles returns the titles passed to phase(), in source order.
func (s *Script) PhaseTitles() []string {
	type hit struct {
		line  int
		title string
		ok    bool
	}
	var hits []hit
	s.Walk(func(n ast.Node) {
		c, ok := n.(*ast.CallExpression)
		if !ok {
			return
		}
		id, ok := c.Callee.(*ast.Identifier)
		if !ok || string(id.Name) != "phase" || len(c.ArgumentList) == 0 {
			return
		}
		t, ok := s.StaticString(c.ArgumentList[0])
		hits = append(hits, hit{s.Line(c.Idx0()), t, ok})
	})
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j].line < hits[j-1].line; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		if h.ok {
			out = append(out, h.title)
		} else {
			out = append(out, "<computed>")
		}
	}
	return out
}

// CallsTo counts calls to a named global.
func (s *Script) CallsTo(name string) int {
	n := 0
	s.Walk(func(node ast.Node) {
		c, ok := node.(*ast.CallExpression)
		if !ok {
			return
		}
		if id, ok := c.Callee.(*ast.Identifier); ok && string(id.Name) == name {
			n++
		}
	})
	return n
}

// Loop is one unbounded-capable loop and whether IT guards on budget.total.
type Loop struct {
	Line    int
	Guarded bool
}

// Loops returns every while/for loop with its OWN budget guard verdict. The
// previous checker asked whether the string `budget.total` occurred anywhere in
// the file, so one guarded loop laundered every other loop in the script.
func (s *Script) Loops() []Loop {
	var out []Loop
	guarded := func(nodes ...ast.Node) bool {
		found := false
		for _, n := range nodes {
			if n == nil {
				continue
			}
			walk(n, func(x ast.Node) {
				d, ok := x.(*ast.DotExpression)
				if !ok {
					return
				}
				if id, ok := d.Left.(*ast.Identifier); ok &&
					string(id.Name) == "budget" && string(d.Identifier.Name) == "total" {
					found = true
				}
			})
		}
		return found
	}
	s.Walk(func(n ast.Node) {
		switch v := n.(type) {
		case *ast.WhileStatement:
			out = append(out, Loop{s.Line(v.While), guarded(v.Test, v.Body)})
		case *ast.DoWhileStatement:
			out = append(out, Loop{s.Line(v.Idx0()), guarded(v.Test, v.Body)})
		case *ast.ForStatement:
			// A `for` with a test and an update terminates on its own; only an
			// infinite one belongs here.
			if v.Test == nil {
				out = append(out, Loop{s.Line(v.Idx0()), guarded(v.Body)})
			}
		}
	})
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Line < out[j-1].Line; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
