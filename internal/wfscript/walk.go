package wfscript

import (
	"reflect"

	"github.com/dop251/goja/ast"
)

// walk visits every ast.Node reachable from root.
//
// It is reflective on purpose. A hand-written walker enumerates node types, and
// the type it forgets becomes a subtree no check ever sees — which is precisely
// the defect this package was written to end: a checker that passes over an
// empty set reports zero findings and exits clean, and is trusted more for it,
// because a tool said so. The goja AST is also documented as work-in-progress,
// so a hand-written switch would rot silently the first time a node type is
// added. Reflection cannot forget a field.
func walk(root any, fn func(ast.Node)) {
	seen := map[uintptr]bool{}
	var visit func(reflect.Value)
	visit = func(v reflect.Value) {
		if !v.IsValid() {
			return
		}
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				return
			}
			p := v.Pointer()
			if seen[p] {
				return
			}
			seen[p] = true
			if n, ok := v.Interface().(ast.Node); ok {
				fn(n)
			}
			visit(v.Elem())
		case reflect.Interface:
			if v.IsNil() {
				return
			}
			visit(v.Elem())
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				f := v.Field(i)
				if !f.CanInterface() {
					continue
				}
				visit(f)
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				visit(v.Index(i))
			}
		}
	}
	visit(reflect.ValueOf(root))
}
