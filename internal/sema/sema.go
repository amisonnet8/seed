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
// declarations/assignment (scalar and array) with null semantics, the
// full operator set (§5) including compound assignment and ++/--,
// print/isnull calls, if/elif/else, while, for-in, and break/continue
// (§6). General user-defined functions are not supported yet.
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

	if main.ReturnType == nil || main.ReturnType.Name != "Int" || main.ReturnType.IsArray {
		return fmt.Errorf("line %d: main must return Int", main.Line)
	}
	if len(main.Params) != 1 || main.Params[0].Type.Name != "String" || !main.Params[0].Type.IsArray {
		return fmt.Errorf("line %d: main must take exactly one String[] parameter", main.Line)
	}

	body := newScope(global)
	if err := checkBlock(body, main.Body, *main.ReturnType, 0); err != nil {
		return err
	}
	if !alwaysReturns(main.Body) {
		return fmt.Errorf("line %d: main does not return a value on every path", main.Line)
	}
	return nil
}

// checkBlock validates each statement of a block in order, using scope
// for name resolution/declaration. retType is the enclosing function's
// return type, needed to check `return` expressions; loopDepth is how
// many enclosing while/for-in loops this block is nested in, needed to
// check break/continue.
func checkBlock(scope *scope, stmts []ast.Stmt, retType ast.Type, loopDepth int) error {
	for _, stmt := range stmts {
		if err := checkStmt(scope, stmt, retType, loopDepth); err != nil {
			return err
		}
	}
	return nil
}

func checkStmt(scope *scope, stmt ast.Stmt, retType ast.Type, loopDepth int) error {
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
	case *ast.IfStmt:
		return checkIfStmt(scope, s, retType, loopDepth)
	case *ast.WhileStmt:
		return checkWhileStmt(scope, s, retType, loopDepth)
	case *ast.ForInStmt:
		return checkForInStmt(scope, s, retType, loopDepth)
	case *ast.BreakStmt:
		if loopDepth == 0 {
			return fmt.Errorf("line %d: break outside of a loop", s.Line)
		}
		return nil
	case *ast.ContinueStmt:
		if loopDepth == 0 {
			return fmt.Errorf("line %d: continue outside of a loop", s.Line)
		}
		return nil
	default:
		return fmt.Errorf("sema: unsupported statement %T", stmt)
	}
}

// checkIfStmt checks an if/elif/else chain: every condition must be Bool,
// and each clause/else body gets its own child scope (seed_spec.md §3 —
// a fresh block scope per `{ }`, allowing shadowing).
func checkIfStmt(scope *scope, stmt *ast.IfStmt, retType ast.Type, loopDepth int) error {
	for _, clause := range stmt.Clauses {
		if err := checkCondition(scope, clause.Cond); err != nil {
			return err
		}
		if err := checkBlock(newScope(scope), clause.Body, retType, loopDepth); err != nil {
			return err
		}
	}
	if stmt.Else != nil {
		if err := checkBlock(newScope(scope), stmt.Else, retType, loopDepth); err != nil {
			return err
		}
	}
	return nil
}

// checkWhileStmt checks a while loop: the condition must be Bool, and the
// body is checked with loopDepth+1 so break/continue are valid there.
func checkWhileStmt(scope *scope, stmt *ast.WhileStmt, retType ast.Type, loopDepth int) error {
	if err := checkCondition(scope, stmt.Cond); err != nil {
		return err
	}
	return checkBlock(newScope(scope), stmt.Body, retType, loopDepth+1)
}

// checkForInStmt checks `for x in a` (seed_spec.md §6): a must be a
// declared array, and x is declared with the array's element type in a
// single child scope shared with the body (matching codegen's genBlock,
// which likewise pushes only one scope for a for-in statement) — x's
// scope is "the whole for statement", per the spec.
func checkForInStmt(scope *scope, stmt *ast.ForInStmt, retType ast.Type, loopDepth int) error {
	arrType, ok := scope.lookup(stmt.ArrayName)
	if !ok {
		return fmt.Errorf("line %d: undefined variable %q", stmt.Line, stmt.ArrayName)
	}
	if !arrType.IsArray {
		return fmt.Errorf("line %d: %q is not an array", stmt.Line, stmt.ArrayName)
	}
	body := newScope(scope)
	if !body.declare(stmt.VarName, ast.Type{Name: arrType.Name}) {
		return fmt.Errorf("line %d: %q is already declared in this scope", stmt.Line, stmt.VarName)
	}
	return checkBlock(body, stmt.Body, retType, loopDepth+1)
}

// checkCondition validates that cond is a non-null Bool expression, as
// required by if/elif/while.
func checkCondition(scope *scope, cond ast.Expr) error {
	if _, ok := cond.(*ast.NullLit); ok {
		return fmt.Errorf("line %d: null cannot be used here", ast.ExprLine(cond))
	}
	t, err := inferType(scope, cond)
	if err != nil {
		return err
	}
	if t != (ast.Type{Name: "Bool"}) {
		return fmt.Errorf("line %d: condition must be Bool, got %s", ast.ExprLine(cond), typeName(t))
	}
	return nil
}

// alwaysReturns conservatively reports whether executing stmts is
// guaranteed to hit a `return` on every path. An if/elif/.../else chain
// counts only when every one of its branches (including a mandatory
// else) always returns; a while or for-in loop never counts, since its
// body might not execute at all (a false condition, or an empty array)
// and Seed has no literal-true/non-empty fast path for this analysis to
// recognize. This mirrors (a deliberately simpler version of) Go's own
// "missing return" check, run here so a non-exhaustive main is a
// Seed-level error instead of surfacing as a go/types failure from the
// generated code.
func alwaysReturns(stmts []ast.Stmt) bool {
	for _, s := range stmts {
		switch st := s.(type) {
		case *ast.ReturnStmt:
			return true
		case *ast.IfStmt:
			if st.Else == nil {
				continue
			}
			exhaustive := alwaysReturns(st.Else)
			for _, clause := range st.Clauses {
				if !alwaysReturns(clause.Body) {
					exhaustive = false
				}
			}
			if exhaustive {
				return true
			}
		}
	}
	return false
}

// checkVarDecl checks a local variable declaration: scalar or array
// (seed_spec.md §3). Array declarations dispatch to checkArrayVarDecl for
// their size/initializer, which follow different rules than a scalar's.
func checkVarDecl(scope *scope, decl *ast.VarDecl) error {
	if !scope.declare(decl.Name, decl.Type) {
		return fmt.Errorf("line %d: %q is already declared in this scope", decl.Line, decl.Name)
	}
	if decl.Type.IsArray {
		return checkArrayVarDecl(scope, decl)
	}
	if decl.Init == nil {
		return nil
	}
	return checkAssignable(scope, decl.Type, decl.Init)
}

// checkArrayVarDecl checks an array declaration's size (must be a
// non-null Int expression) and, if present, its initializer (must be
// `null` or an array literal whose elements all match the element type —
// seed_spec.md §4's truncate/pad rule means the literal's element count
// doesn't have to match the declared size).
func checkArrayVarDecl(scope *scope, decl *ast.VarDecl) error {
	if decl.Size == nil {
		return fmt.Errorf("sema: array declaration missing a size (parser bug)")
	}
	if _, isNull := decl.Size.(*ast.NullLit); isNull {
		return fmt.Errorf("line %d: null cannot be used here", decl.Line)
	}
	sizeType, err := inferType(scope, decl.Size)
	if err != nil {
		return err
	}
	if sizeType != (ast.Type{Name: "Int"}) {
		return fmt.Errorf("line %d: array size must be Int, got %s", decl.Line, typeName(sizeType))
	}
	if decl.Init == nil {
		return nil
	}
	if _, isNull := decl.Init.(*ast.NullLit); isNull {
		return nil
	}
	lit, ok := decl.Init.(*ast.ArrayLit)
	if !ok {
		return fmt.Errorf("line %d: an array must be initialized with an array literal", ast.ExprLine(decl.Init))
	}
	return checkArrayLiteral(scope, ast.Type{Name: decl.Type.Name}, lit)
}

// checkArrayLiteral checks every element of lit against elemType (the
// array's element type — a scalar, never itself an array: seed_spec.md
// §0 forbids multi-dimensional arrays).
func checkArrayLiteral(scope *scope, elemType ast.Type, lit *ast.ArrayLit) error {
	for _, elem := range lit.Elems {
		if err := checkAssignable(scope, elemType, elem); err != nil {
			return err
		}
	}
	return nil
}

// checkGlobalVarDecl is checkVarDecl's counterpart for top-level
// declarations. Its initializer, unlike a local's, may only be a literal
// (and an array's size may only be an Int literal): GVAR has no
// initializer syntax, so codegen assigns it inside the generated !main
// wrapper with no Seed variables in scope yet.
func checkGlobalVarDecl(global *scope, decl *ast.VarDecl) error {
	if !global.declare(decl.Name, decl.Type) {
		return fmt.Errorf("line %d: %q is already declared in this scope", decl.Line, decl.Name)
	}
	if decl.Type.IsArray {
		if _, ok := decl.Size.(*ast.IntLit); !ok {
			return fmt.Errorf("line %d: a global array's size must be an Int literal", ast.ExprLine(decl.Size))
		}
		if decl.Init == nil {
			return nil
		}
		if _, isNull := decl.Init.(*ast.NullLit); isNull {
			return nil
		}
		lit, ok := decl.Init.(*ast.ArrayLit)
		if !ok {
			return fmt.Errorf("line %d: an array must be initialized with an array literal", ast.ExprLine(decl.Init))
		}
		return checkArrayLiteral(newScope(nil), ast.Type{Name: decl.Type.Name}, lit)
	}
	if decl.Init == nil {
		return nil
	}
	return checkAssignable(newScope(nil), decl.Type, decl.Init)
}

// checkAssignStmt checks `name = value` (whole-variable, scalar or
// array) and `name[Index] = value` (a single array element).
func checkAssignStmt(scope *scope, stmt *ast.AssignStmt) error {
	typ, ok := scope.lookup(stmt.Name)
	if !ok {
		return fmt.Errorf("line %d: undefined variable %q", stmt.Line, stmt.Name)
	}
	if stmt.Index != nil {
		return checkIndexedAssign(scope, typ, stmt)
	}
	if typ.IsArray {
		return checkArrayReassign(scope, typ, stmt)
	}
	return checkAssignable(scope, typ, stmt.Value)
}

// checkIndexedAssign checks `name[Index] = value`: name must be an
// array, Index a non-null Int, and value assignable to the element type.
func checkIndexedAssign(scope *scope, arrType ast.Type, stmt *ast.AssignStmt) error {
	if !arrType.IsArray {
		return fmt.Errorf("line %d: %q is not an array", stmt.Line, stmt.Name)
	}
	if _, isNull := stmt.Index.(*ast.NullLit); isNull {
		return fmt.Errorf("line %d: null cannot be used here", stmt.Line)
	}
	idxType, err := inferType(scope, stmt.Index)
	if err != nil {
		return err
	}
	if idxType != (ast.Type{Name: "Int"}) {
		return fmt.Errorf("line %d: array index must be Int, got %s", stmt.Line, typeName(idxType))
	}
	return checkAssignable(scope, ast.Type{Name: arrType.Name}, stmt.Value)
}

// checkArrayReassign checks whole-array reassignment (`name = {...}` or
// `name = null`) — seed_spec.md §4 only documents array assignment from
// a literal or (once general functions exist) a function's return value,
// never from another array variable, so that's rejected here explicitly
// rather than falling through to a confusing type-mismatch error.
func checkArrayReassign(scope *scope, arrType ast.Type, stmt *ast.AssignStmt) error {
	if _, isNull := stmt.Value.(*ast.NullLit); isNull {
		return nil
	}
	lit, ok := stmt.Value.(*ast.ArrayLit)
	if !ok {
		return fmt.Errorf("line %d: an array can only be reassigned with an array literal or null", stmt.Line)
	}
	return checkArrayLiteral(scope, ast.Type{Name: arrType.Name}, lit)
}

// checkCompoundAssignStmt checks `name op= value` (seed_spec.md §5): both
// sides must satisfy the same type rules as the corresponding binary
// operator (see arithOpType), and null is never a valid operand. Arrays
// don't support any operator, compound assignment included.
func checkCompoundAssignStmt(scope *scope, stmt *ast.CompoundAssignStmt) error {
	typ, ok := scope.lookup(stmt.Name)
	if !ok {
		return fmt.Errorf("line %d: undefined variable %q", stmt.Line, stmt.Name)
	}
	if typ.IsArray {
		return fmt.Errorf("line %d: operators are not supported for arrays", stmt.Line)
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
// or Float scalar variable.
func checkIncDecStmt(scope *scope, stmt *ast.IncDecStmt) error {
	typ, ok := scope.lookup(stmt.Name)
	if !ok {
		return fmt.Errorf("line %d: undefined variable %q", stmt.Line, stmt.Name)
	}
	if typ.IsArray || (typ.Name != "Int" && typ.Name != "Float") {
		return fmt.Errorf("line %d: %s expects an Int or Float variable, got %s", stmt.Line, stmt.Op, typeName(typ))
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
// assignable and resets the variable to its base value. want is always a
// scalar type here — checkArrayReassign/checkArrayVarDecl handle arrays
// via checkArrayLiteral instead, since an array literal has no
// context-free type of its own (see ast.ArrayLit's doc).
func checkAssignable(scope *scope, want ast.Type, value ast.Expr) error {
	if _, ok := value.(*ast.NullLit); ok {
		return nil
	}
	got, err := inferType(scope, value)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("line %d: cannot use %s as %s", ast.ExprLine(value), typeName(got), typeName(want))
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

// typeName renders t for error messages, e.g. "Int" or "Int[]".
func typeName(t ast.Type) string {
	if t.IsArray {
		return t.Name + "[]"
	}
	return t.Name
}
