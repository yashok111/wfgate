// Package wfscript reads a Claude Code Workflow orchestration script as a
// PARSED PROGRAM rather than as text.
//
// Its predecessor was a 689-line text scanner, and an audit of this repository
// defeated almost every check in it with script shapes a real author writes: a
// wrapper under an unrecognised name reported "0 agent() call sites, 0 errors,
// exit 0" on a script with five of them; an options object built with
// `{...BASE, model:'sonnet'}` produced four phantom ERRORs on a correct call;
// the string `effort:` inside a schema DESCRIPTION satisfied the check that
// every call declares an effort; a `phase("x")` written with double quotes or
// across two lines vanished, and the comparison against `meta.phases` was then
// skipped because one side was empty; a prompt composed outside the call read
// as 0.0 KB, so every prompt check ran over an empty string.
//
// None of those are bugs to patch one at a time. They are the method: a regular
// expression cannot know whether `effort:` is an option or prose, and a name
// list cannot know what a function is. The parser does, so this package answers
// those questions once and the checks above it read structure instead of text.
package wfscript

import (
	"fmt"
	"os"
	"strings"

	"strconv"

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/file"
	"github.com/dop251/goja/parser"
	"github.com/dop251/goja/token"
	"github.com/dop251/goja/unistring"
)

// wrapPrefix makes a workflow script parseable as a function body. A script
// ends in a top-level `return`, which is a syntax error at program level. The
// prefix carries NO newline, so every line number in the parsed program is the
// line number in the file the operator is looking at.
const wrapPrefix = "(async function(){"

// Script is one parsed workflow script.
type Script struct {
	Path string
	Src  string // the file as written
	Prog *ast.Program

	shift  int // bytes wrapPrefix added before the source
	consts map[string]*Const

	// subst binds a factory's or a wrapper's parameters to the arguments of the
	// call being resolved; scopeParams names every parameter of that binding,
	// including the ones with no argument.
	//
	// The pair is what gives resolution a third answer. Without scopeParams an
	// unbound parameter fell through to a global const of the same name and
	// came back with a WRONG value — measured: `const label = 'writer'` beside
	// `opts = (label) => ({label, ...})` made two different agents resolve to
	// one label, and the dual-ownership ERROR they should have raised was
	// deduplicated away.
	subst       map[string]ast.Expression
	scopeParams map[string]bool

	// shadowed names every binding in the program that is NOT a top-level
	// `const`: a parameter at any nesting, a destructured or defaulted one, a
	// loop variable, a block-scoped or mutable declaration, a catch binding.
	//
	// A name in this set is never resolved from the top-level consts, even
	// where one of that name exists. The resolver models only two scopes — the
	// top level and the wrapper or factory it is currently inlining — so any
	// OTHER binder is a scope it cannot see, and answering from the global with
	// a binder in between is answering wrongly. Measured, three separate ways:
	// a `for (const target of …)` loop resolved to the top-level `target`; a
	// wrapper parameter named like a const resolved to the const, so two agents
	// came back with one label and their file-ownership collision was
	// deduplicated away; and a destructured parameter bound nothing at all, so a
	// whole wrapper body resolved against the globals and a script with a real
	// dual ownership and an invalid effort reported zero findings.
	shadowed map[string]bool

	// resolving is the re-entrancy guard for member lookups. `memberOf` asks
	// `ObjectOf` for the object, and that entry point starts a FRESH depth
	// budget and a fresh visited set — so the budgets inside objectOf bound one
	// call and never the ring between them. `const M = { a: M.b, b: M.a }` is
	// legal JavaScript and crashed both tools with a stack overflow, which is
	// the one failure mode a checker may not have: a script that kills the
	// checker is a script nobody checked.
	resolving map[ast.Node]bool

	// strCache memoises staticStr for a node resolved at the TOP level with no
	// factory binding in effect and no member lookup in flight — the only scope
	// in which one node has one answer for the life of the Script.
	//
	// It is not an optimisation, it is a bound. `const Ck = { v: C(k-1).v +
	// C(k-1).v }` is a legal, acyclic chain, and without the cache each level
	// resolves the one below it TWICE: k=12 took 22 s and k=14 did not finish in
	// two minutes, on a script whose only sin is composing a prompt out of
	// pieces. A checker that a legal script can hang is a checker that gets a
	// timeout instead of a report, which is the same silence as a false green.
	strCache map[ast.Node]strResult
}

type strResult struct {
	txt string
	ok  bool
}

// withScope resolves fn with a parameter binding in effect.
func (s *Script) withScope(sub map[string]ast.Expression, params []string, fn func()) {
	savedSub, savedParams := s.subst, s.scopeParams
	ps := map[string]bool{}
	for _, p := range params {
		if p != "" {
			ps[p] = true
		}
	}
	s.subst, s.scopeParams = sub, ps
	fn()
	s.subst, s.scopeParams = savedSub, savedParams
}

// Const is a top-level `const NAME = ...` binding.
type Const struct {
	Name    string
	Line    int
	Init    ast.Expression
	nameIdx file.Idx

	// Text is the statically known string value, when the initialiser is a
	// string or a template literal with no interpolation.
	Text  string
	IsStr bool
	Obj   *ast.ObjectLiteral // set when the initialiser is an object literal
}

// Load reads and parses a workflow script.
func Load(path string) (*Script, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadSource(string(b), path)
}

// LoadSource parses a script already in memory.
func LoadSource(src, path string) (*Script, error) {
	s := &Script{Path: path, Src: src, shift: len(wrapPrefix)}
	// `export` is module syntax and goja parses scripts, not modules. It is
	// blanked rather than removed so that every byte after it keeps its offset,
	// which is what keeps reported line numbers honest.
	body := blankExports(src)
	prog, err := parser.ParseFile(nil, path, wrapPrefix+body+"\n})", 0)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	s.Prog = prog
	s.collectShadowed()
	s.collectConsts()
	return s, nil
}

// blankExports replaces a leading `export ` keyword with spaces, preserving
// every subsequent byte offset.
func blankExports(src string) string {
	out := []byte(src)
	for i := 0; i+7 <= len(out); i++ {
		if (i == 0 || out[i-1] == '\n') && string(out[i:i+7]) == "export " {
			copy(out[i:i+7], "       ")
		}
	}
	return string(out)
}

// Line maps a parser index to the line of the ORIGINAL file.
func (s *Script) Line(idx file.Idx) int {
	off := int(idx) - 1 - s.shift
	if off < 0 {
		off = 0
	}
	if off > len(s.Src) {
		off = len(s.Src)
	}
	return 1 + strings.Count(s.Src[:off], "\n")
}

// Text returns the source between two parser indices.
func (s *Script) Text(from, to file.Idx) string {
	a, b := int(from)-1-s.shift, int(to)-1-s.shift
	if a < 0 {
		a = 0
	}
	if b > len(s.Src) {
		b = len(s.Src)
	}
	if a >= b {
		return ""
	}
	return s.Src[a:b]
}

// Offset maps a node to a byte offset in the ORIGINAL source, or -1.
func (s *Script) Offset(n ast.Node) int {
	if n == nil {
		return -1
	}
	off := int(n.Idx0()) - 1 - s.shift
	if off < 0 || off > len(s.Src) {
		return -1
	}
	return off
}

// NameOffset is the byte offset of a const's declared NAME.
func (s *Script) NameOffset(c *Const) int {
	off := int(c.nameIdx) - 1 - s.shift
	if off < 0 || off > len(s.Src) {
		return -1
	}
	return off
}

// Consts returns the top-level const bindings, by name.
func (s *Script) Consts() map[string]*Const { return s.consts }

func (s *Script) collectShadowed() {
	s.shadowed = map[string]bool{}
	top := map[ast.Node]bool{}
	for _, st := range s.topLevel() {
		top[st] = true
	}
	names := func(t ast.BindingTarget) []string {
		var out []string
		walk(t, func(n ast.Node) {
			if id, ok := n.(*ast.Identifier); ok {
				out = append(out, string(id.Name))
			}
		})
		if id, ok := t.(*ast.Identifier); ok {
			out = append(out, string(id.Name))
		}
		return out
	}
	mark := func(ns []string) {
		for _, n := range ns {
			if n != "" {
				s.shadowed[n] = true
			}
		}
	}
	s.Walk(func(n ast.Node) {
		switch v := n.(type) {
		case *ast.ParameterList:
			for _, b := range v.List {
				mark(names(b.Target))
			}
			if v.Rest != nil {
				walk(v.Rest, func(x ast.Node) {
					if id, ok := x.(*ast.Identifier); ok {
						s.shadowed[string(id.Name)] = true
					}
				})
			}
		case *ast.LexicalDeclaration:
			for _, b := range v.List {
				if v.Token != token.CONST || !top[ast.Statement(v)] {
					mark(names(b.Target))
				}
			}
		case *ast.VariableDeclaration:
			for _, b := range v.List {
				mark(names(b.Target))
			}
		case *ast.FunctionDeclaration:
			if v.Function != nil {
				s.shadowed[string(v.Function.Name.Name)] = true
			}
		case *ast.CatchStatement:
			if v.Parameter != nil {
				mark(names(v.Parameter))
			}
		case *ast.ForOfStatement:
			if d, ok := v.Into.(*ast.ForIntoVar); ok && d.Binding != nil {
				mark(names(d.Binding.Target))
			}
			if d, ok := v.Into.(*ast.ForDeclaration); ok {
				mark(names(d.Target))
			}
		case *ast.ForInStatement:
			if d, ok := v.Into.(*ast.ForIntoVar); ok && d.Binding != nil {
				mark(names(d.Binding.Target))
			}
			if d, ok := v.Into.(*ast.ForDeclaration); ok {
				mark(names(d.Target))
			}
		}
	})
}

func (s *Script) collectConsts() {
	s.consts = map[string]*Const{}
	// The wrapper adds one statement: the whole script is inside it.
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
			if lex.Token != token.CONST {
				// A rebindable top-level name answers with its INITIALISER,
				// which is the value at the declaration and not necessarily at
				// the call: `let mk = f; mk = g` resolved to f's body.
				s.shadowed[string(id.Name)] = true
				continue
			}
			c := &Const{Name: string(id.Name), Line: s.Line(id.Idx), Init: b.Initializer, nameIdx: id.Idx}
			// Resolved against the consts collected SO FAR, in declaration
			// order: a shared contract block is routinely built out of two
			// smaller ones (`const SIG = \u0060${SIG_HEAD}\n${SIG_TAIL}\u0060`), and
			// passing nil here made every such const non-static — which then
			// ERRORed as "reaches NO prompt" on the very authoring shape the
			// guide prescribes. Only earlier consts are in scope, which is also
			// what JavaScript says.
			//
			// `let`/`var` are NOT resolved: a rebindable name can hold a
			// different value at the call than at its declaration, and
			// answering with the initialiser is a WRONG answer rather than an
			// incomplete one — `let E = 'low'; E = 'turbo'` read as 'low' and
			// the enum check passed over a value the runtime rejects.
			if txt, ok := s.staticStr(b.Initializer, 0); ok {
				c.Text, c.IsStr = txt, true
			}
			if o, ok := b.Initializer.(*ast.ObjectLiteral); ok {
				c.Obj = o
			}
			s.consts[c.Name] = c
		}
	}
}

// topLevel returns the statements of the script body, unwrapping the async
// function Load parses it inside.
func (s *Script) topLevel() []ast.Statement {
	if len(s.Prog.Body) == 0 {
		return nil
	}
	es, ok := s.Prog.Body[0].(*ast.ExpressionStatement)
	if !ok {
		return s.Prog.Body
	}
	fn, ok := es.Expression.(*ast.ArrowFunctionLiteral)
	if ok {
		if blk, ok := fn.Body.(*ast.BlockStatement); ok {
			return blk.List
		}
	}
	if f, ok := es.Expression.(*ast.FunctionLiteral); ok && f.Body != nil {
		return f.Body.List
	}
	return s.Prog.Body
}

// staticString resolves an expression to a string when it can be known without
// running the program: a literal, a template literal whose interpolations
// resolve, a `+` chain of those, or an identifier bound to one.
//
// It returns false when any part is unknown — and the CALLER decides what that
// means. The text scanner had no such distinction: an unresolvable prompt was
// simply the empty string, so every check over it passed silently.
// Depth is charged per INDIRECTION — a const, a parameter binding or a member
// hop — never per node, which is the accounting `objectOf` already uses and for
// the same measured reason. Charging every node made the budget a function of a
// prompt's SHAPE: `a + b + c + ...` parses left-associative, so a prompt
// assembled from more than a dozen concatenated pieces lost its leftmost
// segments. Measured on a live gate (mocker slice A, round 24): a field named
// in the prompt's first chunk read as never mentioned once the chunk count
// crossed the budget, and an 8.3 KB prompt was metered at 4.5 KB — so the size
// guard could be walked past by splitting the same text into more pieces.
// A cycle must pass through an indirection, so charging only those still bounds
// it.
func (s *Script) staticStr(e ast.Expression, depth int) (string, bool) {
	if depth > 12 {
		return "", false
	}
	if e != nil && s.cacheableStr(depth) {
		if r, ok := s.strCache[e]; ok {
			return r.txt, r.ok
		}
		txt, ok := s.staticStrUncached(e, depth)
		if s.strCache == nil {
			s.strCache = map[ast.Node]strResult{}
		}
		s.strCache[e] = strResult{txt, ok}
		return txt, ok
	}
	return s.staticStrUncached(e, depth)
}

func (s *Script) staticStrUncached(e ast.Expression, depth int) (string, bool) {
	switch v := e.(type) {
	case *ast.StringLiteral:
		return v.Value.String(), true
	case *ast.TemplateLiteral:
		if v.Tag != nil {
			return "", false
		}
		var b strings.Builder
		ok := true
		for i, el := range v.Elements {
			b.WriteString(el.Parsed.String())
			if i < len(v.Expressions) {
				if sub, sok := s.staticStr(v.Expressions[i], depth); sok {
					b.WriteString(sub)
				} else {
					ok = false
				}
			}
		}
		return b.String(), ok
	case *ast.BinaryExpression:
		if v.Operator.String() != "+" {
			return "", false
		}
		l, lok := s.staticStr(v.Left, depth)
		r, rok := s.staticStr(v.Right, depth)
		return l + r, lok && rok
	case *ast.DotExpression:
		// `o.prompt` where `o` is the caller's options object, `SCHEMAS.review`
		// where SCHEMAS is a const map of schemas. Both are ordinary authoring
		// shapes, and without this they resolve to nothing — which for a schema
		// silently disabled the prompt-vs-schema check and for a prompt made
		// every prompt check run over the empty string.
		if e, ok := s.memberOf(v); ok {
			return s.staticStr(e, depth+1)
		}
		return "", false
	case *ast.Identifier:
		name := string(v.Name)
		if sub, ok := s.subst[name]; ok {
			return s.staticStr(sub, depth+1)
		}
		// A parameter with no bound argument is UNKNOWN, never the global of
		// the same name.
		if s.scopeParams[name] || s.shadowed[name] {
			return "", false
		}
		if c, ok := s.consts[name]; ok && c.IsStr {
			return c.Text, true
		}
		return "", false
	}
	return "", false
}

// StaticString resolves an expression to a string with this script's consts —
// and any parameter binding currently in scope — applied.
func (s *Script) StaticString(e ast.Expression) (string, bool) { return s.staticStr(e, 0) }

// cacheableStr says whether a node's resolution is scope-independent right now.
// A parameter binding gives one node different answers per call site, and a
// member lookup in flight can report a cycle that is not one outside the ring.
func (s *Script) cacheableStr(depth int) bool {
	return depth == 0 && s.subst == nil && len(s.resolving) == 0
}

// ObjectOf resolves an expression to an object literal's own properties,
// following an identifier to its const and merging `...SPREAD` from another
// object literal. A spread whose source is not statically known is reported by
// the second return value, so a check can say "not knowable here" instead of
// reporting the properties it could not see as missing — which is what the text
// scanner did to every `{...BASE, model:'sonnet'}` call.
func (s *Script) ObjectOf(e ast.Expression) (props map[string]ast.Expression, complete bool) {
	// wantText is false: this caller throws the text away, and computing it
	// resolved every property of every literal with a fresh budget. It is not
	// hypothetical work — checkFixerReach walks EVERY object literal in the
	// program, and a chain of a dozen objects each concatenating the one before
	// it took 22 s to answer `not knowable`.
	props, _, complete = s.objectOf(e, false)
	return props, complete
}

// ObjectText resolves an object's properties AND each value's static text, in
// the scope where that value is written. A caller that keeps the expressions
// and resolves them later does it with the binding popped, which is how a
// factory parameter came back as a same-named global.
func (s *Script) ObjectText(e ast.Expression) (props map[string]ast.Expression, text map[string]string, complete bool) {
	return s.objectOf(e, true)
}

func (s *Script) objectOf(e ast.Expression, wantText bool) (props map[string]ast.Expression, text map[string]string, complete bool) {
	props, text, complete = map[string]ast.Expression{}, map[string]string{}, true
	// Depth is charged per INDIRECTION — a const or a factory hop — not per
	// node, and a visited set bounds a cycle. Charging every step made the
	// composition the guide prescribes (a wrapper spreading its options over a
	// factory that spreads a shared BASE) exceed the budget, so the
	// required-option check silently did not cover that call.
	seen := map[ast.Node]bool{}
	var merge func(ast.Expression, int)
	merge = func(e ast.Expression, depth int) {
		if depth > 12 || seen[e] {
			complete = false
			return
		}
		seen[e] = true
		switch v := e.(type) {
		case *ast.ObjectLiteral:
			for _, p := range v.Value {
				switch pv := p.(type) {
				case *ast.PropertyKeyed:
					if pv.Computed {
						complete = false
						continue
					}
					if k, ok := propKey(pv.Key); ok {
						props[k] = s.resolveSubst(pv.Value)
						if wantText {
							if txt, ok := s.staticStr(pv.Value, 0); ok {
								text[k] = txt
							}
						}
					} else {
						complete = false
					}
				case *ast.PropertyShort:
					k := string(pv.Name.Name)
					props[k] = s.resolveSubst(&pv.Name)
					if wantText {
						if txt, ok := s.staticStr(&pv.Name, 0); ok {
							text[k] = txt
						}
					}
				case *ast.SpreadElement:
					merge(pv.Expression, depth)
				default:
					complete = false
				}
			}
		case *ast.Identifier:
			name := string(v.Name)
			if sub, ok := s.subst[name]; ok {
				merge(sub, depth+1)
				return
			}
			if s.scopeParams[name] || s.shadowed[name] {
				complete = false
				return
			}
			if c, ok := s.consts[name]; ok && c.Obj != nil {
				merge(c.Obj, depth+1)
				return
			}
			complete = false
		case *ast.DotExpression:
			if inner, ok := s.memberOf(v); ok {
				merge(inner, depth+1)
				return
			}
			complete = false
		case *ast.CallExpression:
			// An options FACTORY — `opts(label, phase)` returning an object
			// literal — is the shape that produced the founding false green: the
			// text scanner saw each option key stated ONCE, in the factory, so
			// its call-site count came out at zero and every check below passed
			// over an empty set. The factory's body is right there in the tree,
			// so it is inlined with its parameters bound to this call's
			// arguments.
			if obj, sub, params, ok := s.factoryBody(v); ok {
				s.withScope(sub, params, func() { merge(obj, depth+1) })
				return
			}
			complete = false
		default:
			complete = false
		}
	}
	merge(e, 0)
	return props, text, complete
}

// ArrayOf resolves an expression to an array literal's elements, following an
// identifier to its const and a member expression to its property, and merging
// `...SPREAD` from another array. The second return value is false when an
// element could not be seen, exactly as ObjectOf reports it, and for the same
// reason: a caller must be able to say "not knowable here" instead of reporting
// what it could not read as absent.
//
// It exists because a check that type-asserted the value straight to
// *ast.ArrayLiteral saw NOTHING through a name. Measured on the two scripts of
// the gate that motivated the reach check: both write `owns: closingOwns` on
// their closing gate, whose const is a plain array of string literals one screen
// above — and the assertion failed, the gate counted as unreadable, and the
// check disabled itself for the whole script on both.
func (s *Script) ArrayOf(e ast.Expression) (elems []ast.Expression, complete bool) {
	complete = true
	// Depth per INDIRECTION and a visited set, the accounting objectOf uses.
	seen := map[ast.Node]bool{}
	var merge func(ast.Expression, int)
	merge = func(e ast.Expression, depth int) {
		if e == nil || depth > 12 || seen[e] {
			complete = false
			return
		}
		seen[e] = true
		switch v := e.(type) {
		case *ast.ArrayLiteral:
			for _, el := range v.Value {
				if el == nil { // a hole: `[a, , b]`
					complete = false
					continue
				}
				if sp, ok := el.(*ast.SpreadElement); ok {
					merge(sp.Expression, depth)
					continue
				}
				elems = append(elems, s.resolveSubst(el))
			}
		case *ast.Identifier:
			name := string(v.Name)
			if sub, ok := s.subst[name]; ok {
				merge(sub, depth+1)
				return
			}
			if s.scopeParams[name] || s.shadowed[name] {
				complete = false
				return
			}
			if c, ok := s.consts[name]; ok && c.Init != nil {
				merge(c.Init, depth+1)
				return
			}
			complete = false
		case *ast.DotExpression:
			if inner, ok := s.memberOf(v); ok {
				merge(inner, depth+1)
				return
			}
			complete = false
		default:
			complete = false
		}
	}
	merge(e, 0)
	return elems, complete
}

// factoryBody returns the object literal a const arrow function returns, plus
// the binding of its parameters to this call's arguments.
func (s *Script) factoryBody(c *ast.CallExpression) (*ast.ObjectLiteral, map[string]ast.Expression, []string, bool) {
	id, ok := c.Callee.(*ast.Identifier)
	if !ok {
		return nil, nil, nil, false
	}
	if s.shadowed[string(id.Name)] {
		return nil, nil, nil, false
	}
	cst, ok := s.consts[string(id.Name)]
	if !ok {
		return nil, nil, nil, false
	}
	fn, ok := cst.Init.(*ast.ArrowFunctionLiteral)
	if !ok {
		return nil, nil, nil, false
	}
	var obj *ast.ObjectLiteral
	switch b := fn.Body.(type) {
	case *ast.ExpressionBody:
		obj, _ = b.Expression.(*ast.ObjectLiteral)
	case *ast.BlockStatement:
		if len(b.List) == 1 {
			if ret, ok := b.List[0].(*ast.ReturnStatement); ok && ret.Argument != nil {
				obj, _ = ret.Argument.(*ast.ObjectLiteral)
			}
		}
	}
	if obj == nil {
		return nil, nil, nil, false
	}
	// The inner call's arguments are expressions of the ENCLOSING scope, so
	// they are resolved through the substitution in effect right now: a factory
	// calling a factory used to bind the inner parameter to a raw identifier,
	// which then resolved against the globals.
	params := paramNames(fn.ParameterList)
	sub := map[string]ast.Expression{}
	for i, name := range params {
		if name == "" || i >= len(c.ArgumentList) {
			continue
		}
		// Resolved HERE, where the argument is written. `outer(who + '-1')`
		// is not a bare identifier, so passing the expression on left the
		// callee's parameter to fall through to a global of the same name.
		if txt, ok := s.staticStr(c.ArgumentList[i], 0); ok {
			sub[name] = &ast.StringLiteral{Value: unistring.String(txt), Literal: strconv.Quote(txt)}
			continue
		}
		sub[name] = s.resolveSubst(c.ArgumentList[i])
	}
	return obj, sub, params, true
}

// resolveSubst replaces a factory parameter with the argument it was called
// with, so an option written `{ label, phase: phaseName }` reads as the value
// the call actually passes.
func (s *Script) resolveSubst(e ast.Expression) ast.Expression {
	if s.subst == nil {
		return e
	}
	if id, ok := e.(*ast.Identifier); ok {
		if v, ok := s.subst[string(id.Name)]; ok {
			return v
		}
	}
	return e
}

// memberOf resolves `x.y` to the expression bound to property y of x, when x
// itself resolves to an object literal.
func (s *Script) memberOf(d *ast.DotExpression) (ast.Expression, bool) {
	if s.resolving[d] {
		return nil, false
	}
	if s.resolving == nil {
		s.resolving = map[ast.Node]bool{}
	}
	s.resolving[d] = true
	defer delete(s.resolving, d)
	props, _ := s.ObjectOf(d.Left)
	e, ok := props[string(d.Identifier.Name)]
	return e, ok
}

func propKey(e ast.Expression) (string, bool) {
	switch k := e.(type) {
	case *ast.Identifier:
		return string(k.Name), true
	case *ast.StringLiteral:
		return k.Value.String(), true
	}
	return "", false
}

// Walk calls fn for every node reachable from the program. It is a helper for
// checks that count constructs rather than inspect one.
func (s *Script) Walk(fn func(ast.Node)) { walk(s.Prog, fn) }

// WalkNode is Walk over one subtree.
func WalkNode(root ast.Node, fn func(ast.Node)) { walk(root, fn) }

// FilterBooleanCalls counts `.filter(Boolean)` as a CALL, not as a substring.
// Counting the source text meant a const holding that string as data suppressed
// the warning for a genuinely unfiltered parallel() result.
func (s *Script) FilterBooleanCalls() int {
	n := 0
	s.Walk(func(node ast.Node) {
		c, ok := node.(*ast.CallExpression)
		if !ok || len(c.ArgumentList) != 1 {
			return
		}
		d, ok := c.Callee.(*ast.DotExpression)
		if !ok || string(d.Identifier.Name) != "filter" {
			return
		}
		if id, ok := c.ArgumentList[0].(*ast.Identifier); ok && string(id.Name) == "Boolean" {
			n++
		}
	})
	return n
}
