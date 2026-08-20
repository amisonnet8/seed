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
// Supported so far: exactly one function named main with the fixed
// entry-point signature from seed_spec.md §7, global and local variable
// declarations/assignment with null semantics, the full operator set
// (§5) including compound assignment and ++/--, and print/isnull calls.
// General user-defined functions, control flow, and arrays are not
// supported yet.
func Check(f *ast.File) error {
	global := newScope(nil)
	for _, g := range f.Globals {
		if err := checkGlobalVarDecl(global, g); err != nil {
			return err
		}
	}

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

	body := newScope(global)
	return checkBlock(body, main.Body, *main.ReturnType)
}

// checkBlock validates each statement of a block in order, using scope
// for name resolution/declaration. retType is the enclosing function's
// return type, needed to check `return` expressions.
func checkBlock(scope *scope, stmts []ast.Stmt, retType ast.Type) error {
	for _, stmt := range stmts {
		if err := checkStmt(scope, stmt, retType); err != nil {
			return err
		}
	}
	return nil
}

func checkStmt(scope *scope, stmt ast.Stmt, retType ast.Type) error {
	switch s := stmt.(type) {
	case *ast.VarDecl:
		return checkVarDecl(scope, s)
	case *ast.AssignStmt:
		return checkAssignStmt(scope, s)
	case *ast.CompoundAssignStmt:
		return checkCompoundAssignStmt(scope, s)
	case *ast.IncDecStmt:
		return checkIncDecStmt(scope, s)
	case *ast.ExprStmt:
		_, err := checkCall(scope, s.X)
		return err
	case *ast.ReturnStmt:
		return checkReturnStmt(scope, s, retType)
	default:
		return fmt.Errorf("sema: unsupported statement %T", stmt)
	}
}

func checkVarDecl(scope *scope, decl *ast.VarDecl) error {
	if !scope.declare(decl.Name, decl.Type) {
		return fmt.Errorf("line %d: %q is already declared in this scope", decl.Line, decl.Name)
	}
	if decl.Init == nil {
		return nil
	}
	return checkAssignable(scope, decl.Type, decl.Init)
}

// checkGlobalVarDecl is checkVarDecl's counterpart for top-level
// declarations. Its initializer, unlike a local's, may only be a literal:
// GVAR has no initializer syntax, so codegen assigns it inside the
// generated !main wrapper with no Seed variables in scope yet.
func checkGlobalVarDecl(global *scope, decl *ast.VarDecl) error {
	if !global.declare(decl.Name, decl.Type) {
		return fmt.Errorf("line %d: %q is already declared in this scope", decl.Line, decl.Name)
	}
	if decl.Init == nil {
		return nil
	}
	return checkAssignable(newScope(nil), decl.Type, decl.Init)
}

func checkAssignStmt(scope *scope, stmt *ast.AssignStmt) error {
	typ, ok := scope.lookup(stmt.Name)
	if !ok {
		return fmt.Errorf("line %d: undefined variable %q", stmt.Line, stmt.Name)
	}
	return checkAssignable(scope, typ, stmt.Value)
}

// checkCompoundAssignStmt checks `name op= value` (seed_spec.md §5): both
// sides must satisfy the same type rules as the corresponding binary
// operator (see arithOpType), and null is never a valid operand.
func checkCompoundAssignStmt(scope *scope, stmt *ast.CompoundAssignStmt) error {
	typ, ok := scope.lookup(stmt.Name)
	if !ok {
		return fmt.Errorf("line %d: undefined variable %q", stmt.Line, stmt.Name)
	}
	if _, isNull := stmt.Value.(*ast.NullLit); isNull {
		return fmt.Errorf("line %d: null cannot be used here", stmt.Line)
	}
	valType, err := inferType(scope, stmt.Value)
	if err != nil {
		return err
	}
	if _, err := arithOpType(stmt.Op, typ, valType); err != nil {
		return fmt.Errorf("line %d: %s", stmt.Line, err)
	}
	return nil
}

// checkIncDecStmt checks `name++`/`name--`: name must be a declared Int
// or Float variable.
func checkIncDecStmt(scope *scope, stmt *ast.IncDecStmt) error {
	typ, ok := scope.lookup(stmt.Name)
	if !ok {
		return fmt.Errorf("line %d: undefined variable %q", stmt.Line, stmt.Name)
	}
	if typ.Name != "Int" && typ.Name != "Float" {
		return fmt.Errorf("line %d: %s expects an Int or Float variable, got %s", stmt.Line, stmt.Op, typ.Name)
	}
	return nil
}

func checkReturnStmt(scope *scope, stmt *ast.ReturnStmt, retType ast.Type) error {
	if stmt.X == nil {
		return fmt.Errorf("line %d: main must return a value", stmt.Line)
	}
	return checkAssignable(scope, retType, stmt.X)
}

// checkAssignable validates that value may be assigned to a variable of
// type want: value's type must match exactly (Seed has no implicit
// conversions), or value may be the `null` literal, which is always
// assignable and resets the variable to its base value.
func checkAssignable(scope *scope, want ast.Type, value ast.Expr) error {
	if _, ok := value.(*ast.NullLit); ok {
		return nil
	}
	got, err := inferType(scope, value)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("line %d: cannot use %s as %s", ast.ExprLine(value), got.Name, want.Name)
	}
	return nil
}

// checkCall validates a statement-position call expression (only print
// and isnull are recognized so far) and returns its result type.
func checkCall(scope *scope, x ast.Expr) (ast.Type, error) {
	call, ok := x.(*ast.CallExpr)
	if !ok {
		return ast.Type{}, fmt.Errorf("line %d: only function calls are supported as statements", ast.ExprLine(x))
	}
	return inferCallType(scope, call)
}
