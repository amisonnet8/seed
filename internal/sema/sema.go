// Package sema performs semantic analysis (type checking, scope
// resolution) on a Seed AST before code generation. amivm delegates all
// semantic validation to go/types, so Seed must catch these errors itself
// and report them in terms of the Seed source, not the generated Go.
package sema

import (
	"fmt"

	"seed/internal/ast"
)

// Check validates f and reports the first semantic error found.
//
// Only what's needed so far is checked: exactly one function named main,
// with the fixed entry-point signature from seed_spec.md §7
// (`func Int main(String[] args)`). User-defined functions, variables,
// and expressions beyond string/int literals are not supported yet.
func Check(f *ast.File) error {
	var main *ast.FuncDecl
	for _, fn := range f.Funcs {
		if fn.Name != "main" {
			return fmt.Errorf("line %d: user-defined functions are not supported yet (only main)", fn.Line)
		}
		if main != nil {
			return fmt.Errorf("line %d: duplicate main function", fn.Line)
		}
		main = fn
	}
	if main == nil {
		return fmt.Errorf("no main function declared")
	}

	if main.ReturnType == nil || main.ReturnType.Name != "Int" || main.ReturnType.IsSlice {
		return fmt.Errorf("line %d: main must return Int", main.Line)
	}
	if len(main.Params) != 1 || main.Params[0].Type.Name != "String" || !main.Params[0].Type.IsSlice {
		return fmt.Errorf("line %d: main must take exactly one String[] parameter", main.Line)
	}

	return nil
}
