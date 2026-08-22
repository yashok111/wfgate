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

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/file"
	"github.com/dop251/goja/parser"
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

	// subst binds a factory's parameters to the arguments of the call being
	// resolved. It is set only while ObjectOf inlines such a call.
	subst map[string]ast.Expression
}

// Const is a top-level `const NAME = ...` binding.
type Const struct {
	Name string
	Line int
	Init ast.Expression

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
	s := &Script{Path: path, Src: string(b), shift: len(wrapPrefix)}
	// `export` is module syntax and goja parses scripts, not modules. It is
	// blanked rather than removed so that every byte after it keeps its offset,
	// which is what keeps reported line numbers honest.
	body := blankExports(s.Src)
	prog, err := parser.ParseFile(nil, path, wrapPrefix+body+"\n})", 0)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	s.Prog = prog
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

// Consts returns the top-level const bindings, by name.
func (s *Script) Consts() map[string]*Const { return s.consts }

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
			c := &Const{Name: string(id.Name), Line: s.Line(id.Idx), Init: b.Initializer}
			if txt, ok := staticString(b.Initializer, nil); ok {
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
func staticString(e ast.Expression, consts map[string]*Const) (string, bool) {
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
				if sub, sok := staticString(v.Expressions[i], consts); sok {
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
		l, lok := staticString(v.Left, consts)
		r, rok := staticString(v.Right, consts)
		return l + r, lok && rok
	case *ast.Identifier:
		if consts == nil {
			return "", false
		}
		if c, ok := consts[string(v.Name)]; ok && c.IsStr {
			return c.Text, true
		}
		return "", false
	}
	return "", false
}

// StaticString is staticString with this script's consts in scope.
func (s *Script) StaticString(e ast.Expression) (string, bool) {
	return staticString(e, s.consts)
}

// ObjectOf resolves an expression to an object literal's own properties,
// following an identifier to its const and merging `...SPREAD` from another
// object literal. A spread whose source is not statically known is reported by
// the second return value, so a check can say "not knowable here" instead of
// reporting the properties it could not see as missing — which is what the text
// scanner did to every `{...BASE, model:'sonnet'}` call.
func (s *Script) ObjectOf(e ast.Expression) (props map[string]ast.Expression, complete bool) {
	props, complete = map[string]ast.Expression{}, true
	var merge func(ast.Expression, int)
	merge = func(e ast.Expression, depth int) {
		if depth > 4 {
			complete = false
			return
		}
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
					} else {
						complete = false
					}
				case *ast.PropertyShort:
					props[string(pv.Name.Name)] = s.resolveSubst(&pv.Name)
				case *ast.SpreadElement:
					merge(pv.Expression, depth+1)
				default:
					complete = false
				}
			}
		case *ast.Identifier:
			if c, ok := s.consts[string(v.Name)]; ok && c.Obj != nil {
				merge(c.Obj, depth+1)
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
			if obj, sub, ok := s.factoryBody(v); ok {
				saved := s.subst
				s.subst = sub
				merge(obj, depth+1)
				s.subst = saved
				return
			}
			complete = false
		default:
			complete = false
		}
	}
	merge(e, 0)
	return props, complete
}

// factoryBody returns the object literal a const arrow function returns, plus
// the binding of its parameters to this call's arguments.
func (s *Script) factoryBody(c *ast.CallExpression) (*ast.ObjectLiteral, map[string]ast.Expression, bool) {
	id, ok := c.Callee.(*ast.Identifier)
	if !ok {
		return nil, nil, false
	}
	cst, ok := s.consts[string(id.Name)]
	if !ok {
		return nil, nil, false
	}
	fn, ok := cst.Init.(*ast.ArrowFunctionLiteral)
	if !ok {
		return nil, nil, false
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
		return nil, nil, false
	}
	sub := map[string]ast.Expression{}
	for i, name := range paramNames(fn.ParameterList) {
		if name == "" || i >= len(c.ArgumentList) {
			continue
		}
		sub[name] = c.ArgumentList[i]
	}
	return obj, sub, true
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
