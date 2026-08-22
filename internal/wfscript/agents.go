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
	Name      string
	Line      int
	Params    []string
	PromptIdx int  // parameter carrying the prompt, -1 if it travels in the options
	OptsIdx   int  // parameter carrying the options object, -1 if unknown
	LabelIdx  int  // parameter carrying a positional label, -1 if none
	Body      span // so a call inside the wrapper is not counted as a call site
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
	for _, st := range s.topLevel() {
		lex, ok := st.(*ast.LexicalDeclaration)
		if !ok {
			continue
		}
		for _, b := range lex.List {
			id, ok := b.Target.(*ast.Identifier)
			if !ok || b.Initializer == nil {
				continue
			}
			if w, ok := s.wrapperOf(string(id.Name), b.Initializer); ok {
				w.Line = s.Line(id.Idx)
				out = append(out, w)
			}
		}
	}
	for _, st := range s.topLevel() {
		fd, ok := st.(*ast.FunctionDeclaration)
		if !ok || fd.Function == nil {
			continue
		}
		if w, ok := s.wrapperOf(string(fd.Function.Name.Name), fd.Function); ok {
			w.Line = s.Line(fd.Function.Idx0())
			out = append(out, w)
		}
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
	w := Wrapper{Name: name, Params: params, PromptIdx: -1, OptsIdx: -1, LabelIdx: -1,
		Body: span{body.Idx0(), body.Idx1()}}
	idxOf := func(e ast.Expression) int {
		id, ok := e.(*ast.Identifier)
		if !ok {
			return -1
		}
		for i, p := range params {
			if p == string(id.Name) {
				return i
			}
		}
		return -1
	}
	if len(fwd.ArgumentList) > 0 {
		w.PromptIdx = idxOf(fwd.ArgumentList[0])
	}
	if len(fwd.ArgumentList) > 1 {
		w.OptsIdx = idxOf(fwd.ArgumentList[1])
	}
	// A wrapper written `spawn('label', {...})` passes the prompt inside the
	// options; the remaining parameter is the positional label.
	for i, p := range params {
		if i == w.PromptIdx || i == w.OptsIdx {
			continue
		}
		if l := strings.ToLower(p); l == "label" || l == "name" || l == "id" || w.LabelIdx == -1 {
			w.LabelIdx = i
		}
	}
	return w, true
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
	promptIdx, optsIdx, labelIdx := 0, 1, -1
	if isWrap {
		a.Wrapper = name
		promptIdx, optsIdx, labelIdx = w.PromptIdx, w.OptsIdx, w.LabelIdx
	}
	arg := func(i int) ast.Expression {
		if i >= 0 && i < len(c.ArgumentList) {
			return c.ArgumentList[i]
		}
		return nil
	}
	if e := arg(optsIdx); e != nil {
		a.Opts, a.OptsComplete = s.ObjectOf(e)
		a.OptsFound = true
	}
	if e := arg(promptIdx); e != nil {
		a.PromptSet = true
		a.Prompt, a.PromptOK = s.StaticString(e)
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
