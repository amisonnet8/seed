// Package sema performs semantic analysis (type checking, scope
// resolution) on a Seed AST before code generation. amivm delegates all
// semantic validation to go/types, so Seed must catch these errors itself
// and report them in terms of the Seed source, not the generated Go.
package sema

import (
	"fmt"

	"seed/internal/ast"
)

// seedMainInternalName is codegen's internal amivm name for the user's
// `main` (see codegen.go's package doc for why `main` can't map directly
// to amivm's own `!main`). It's reserved here too, as a function name a
// Seed program is never allowed to declare, since it would collide with
// that internal function once compiled.
const seedMainInternalName = "seed_main"

// builtinNames are the reserved builtin function names from
// seed_spec.md §10 that a user-defined function may never be named,
// even before every one of them is implemented (only print and isnull
// are recognized as calls so far).
var builtinNames = map[string]bool{
	"isnull": true, "int": true, "float": true, "string": true, "len": true,
	"print": true, "open": true, "read": true, "write": true, "close": true,
}

// funcSig is a checked function's signature, used to validate calls to
// it from anywhere in the program (including before its own declaration
// — Seed, like Go, doesn't require forward declarations).
type funcSig struct {
	Params []ast.Type
	Return *ast.Type // nil means no return value
	Line   int
}

// checker holds the state shared across an entire Check() call — right
// now just the function signature table, needed by any call expression
// anywhere in the program. scope, in contrast, is threaded explicitly
// through every method because it genuinely varies per block.
type checker struct {
	funcs map[string]funcSig
}

// Check validates f and reports the first semantic error found.
//
// Supported so far: seed_spec.md §7's full function feature set —
// user-defined functions (declaration, calls, parameters, return
// values, arrays passed by reference) alongside the fixed main
// entry-point signature and main's no-direct-call rule — plus
// everything from earlier steps: global/local variable declarations
// (scalar and array) with null semantics, the full operator set (§5),
// print/isnull calls, and if/elif/else, while, for-in, break/continue
// (§6).
func Check(f *ast.File) error {
	global := newScope(nil)
	for _, g := range f.Globals {
		if err := checkGlobalVarDecl(global, g); err != nil {
			return err
		}
	}

	c, err := buildFuncTable(f.Funcs)
	if err != nil {
		return err
	}

	main, ok := c.funcs["main"]
	if !ok {
		return fmt.Errorf("no main function declared")
	}
	if main.Return == nil || main.Return.Name != "Int" || main.Return.IsArray {
		return fmt.Errorf("line %d: main must return Int", main.Line)
	}
	if len(main.Params) != 1 || main.Params[0].Name != "String" || !main.Params[0].IsArray {
		return fmt.Errorf("line %d: main must take exactly one String[] parameter", main.Line)
	}

	for _, fn := range f.Funcs {
		if err := c.checkFuncDecl(global, fn); err != nil {
			return err
		}
	}
	return nil
}

// buildFuncTable validates every function declaration's name/signature
// (reserved names, duplicates, duplicate parameter names) and returns a
// checker carrying the resulting signature table, so a call anywhere in
// the program — including one textually before the callee's own
// declaration — can be checked against it.
func buildFuncTable(funcs []*ast.FuncDecl) (*checker, error) {
	table := map[string]funcSig{}
	for _, fn := range funcs {
		if builtinNames[fn.Name] {
			return nil, fmt.Errorf("line %d: %q is a reserved builtin name and cannot be used as a function name", fn.Line, fn.Name)
		}
		if fn.Name == seedMainInternalName {
			return nil, fmt.Errorf("line %d: %q is reserved for Seed's internal use", fn.Line, fn.Name)
		}
		if _, exists := table[fn.Name]; exists {
			return nil, fmt.Errorf("line %d: duplicate function %q", fn.Line, fn.Name)
		}

		params := make([]ast.Type, len(fn.Params))
		seen := map[string]bool{}
		for i, p := range fn.Params {
			if seen[p.Name] {
				return nil, fmt.Errorf("line %d: duplicate parameter %q", fn.Line, p.Name)
			}
			seen[p.Name] = true
			params[i] = p.Type
		}
		table[fn.Name] = funcSig{Params: params, Return: fn.ReturnType, Line: fn.Line}
	}
	return &checker{funcs: table}, nil
}

// checkFuncDecl checks one function's body: its parameters are declared
// in a scope that's a parent of the body's own (so the body may shadow
// a parameter, same as any other nesting), and — if it has a return
// type — every path through it must return a value.
func (c *checker) checkFuncDecl(global *scope, fn *ast.FuncDecl) error {
	params := newScope(global)
	for _, p := range fn.Params {
		params.declare(p.Name, p.Type) // uniqueness already checked by buildFuncTable
	}

	body := newScope(params)
	if err := c.checkBlock(body, fn.Body, fn.ReturnType, 0); err != nil {
		return err
	}
	if fn.ReturnType != nil && !alwaysReturns(fn.Body) {
		return fmt.Errorf("line %d: %s does not return a value on every path", fn.Line, fn.Name)
	}
	return nil
}

// checkBlock validates each statement of a block in order, using scope
// for name resolution/declaration. retType is the enclosing function's
// return type (nil if it has none), needed to check `return`
// expressions; loopDepth is how many enclosing while/for-in loops this
// block is nested in, needed to check break/continue.
func (c *checker) checkBlock(scope *scope, stmts []ast.Stmt, retType *ast.Type, loopDepth int) error {
	for _, stmt := range stmts {
		if err := c.checkStmt(scope, stmt, retType, loopDepth); err != nil {
			return err
		}
	}
	return nil
}

func (c *checker) checkStmt(scope *scope, stmt ast.Stmt, retType *ast.Type, loopDepth int) error {
	switch s := stmt.(type) {
	case *ast.VarDecl:
		return c.checkVarDecl(scope, s)
	case *ast.AssignStmt:
		return c.checkAssignStmt(scope, s)
	case *ast.CompoundAssignStmt:
		return c.checkCompoundAssignStmt(scope, s)
	case *ast.IncDecStmt:
		return c.checkIncDecStmt(scope, s)
	case *ast.ExprStmt:
		return c.checkCall(scope, s.X)
	case *ast.ReturnStmt:
		return c.checkReturnStmt(scope, s, retType)
	case *ast.IfStmt:
		return c.checkIfStmt(scope, s, retType, loopDepth)
	case *ast.WhileStmt:
		return c.checkWhileStmt(scope, s, retType, loopDepth)
	case *ast.ForInStmt:
		return c.checkForInStmt(scope, s, retType, loopDepth)
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
func (c *checker) checkIfStmt(scope *scope, stmt *ast.IfStmt, retType *ast.Type, loopDepth int) error {
	for _, clause := range stmt.Clauses {
		if err := c.checkCondition(scope, clause.Cond); err != nil {
			return err
		}
		if err := c.checkBlock(newScope(scope), clause.Body, retType, loopDepth); err != nil {
			return err
		}
	}
	if stmt.Else != nil {
		if err := c.checkBlock(newScope(scope), stmt.Else, retType, loopDepth); err != nil {
			return err
		}
	}
	return nil
}

// checkWhileStmt checks a while loop: the condition must be Bool, and the
// body is checked with loopDepth+1 so break/continue are valid there.
func (c *checker) checkWhileStmt(scope *scope, stmt *ast.WhileStmt, retType *ast.Type, loopDepth int) error {
	if err := c.checkCondition(scope, stmt.Cond); err != nil {
		return err
	}
	return c.checkBlock(newScope(scope), stmt.Body, retType, loopDepth+1)
}

// checkForInStmt checks `for x in a` (seed_spec.md §6): a must be a
// declared array, and x is declared with the array's element type in a
// single child scope shared with the body (matching codegen's genBlock,
// which likewise pushes only one scope for a for-in statement) — x's
// scope is "the whole for statement", per the spec.
func (c *checker) checkForInStmt(scope *scope, stmt *ast.ForInStmt, retType *ast.Type, loopDepth int) error {
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
	return c.checkBlock(body, stmt.Body, retType, loopDepth+1)
}

// checkCondition validates that cond is a non-null Bool expression, as
// required by if/elif/while.
func (c *checker) checkCondition(scope *scope, cond ast.Expr) error {
	if _, ok := cond.(*ast.NullLit); ok {
		return fmt.Errorf("line %d: null cannot be used here", ast.ExprLine(cond))
	}
	t, err := c.inferType(scope, cond)
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
// "missing return" check, run here so a non-exhaustive function is a
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
func (c *checker) checkVarDecl(scope *scope, decl *ast.VarDecl) error {
	if !scope.declare(decl.Name, decl.Type) {
		return fmt.Errorf("line %d: %q is already declared in this scope", decl.Line, decl.Name)
	}
	if decl.Type.IsArray {
		return c.checkArrayVarDecl(scope, decl)
	}
	if decl.Init == nil {
		return nil
	}
	return c.checkAssignable(scope, decl.Type, decl.Init)
}

// checkArrayVarDecl checks an array declaration's size (must be a
// non-null Int expression) and, if present, its initializer via
// checkArrayValue.
func (c *checker) checkArrayVarDecl(scope *scope, decl *ast.VarDecl) error {
	if decl.Size == nil {
		return fmt.Errorf("sema: array declaration missing a size (parser bug)")
	}
	if _, isNull := decl.Size.(*ast.NullLit); isNull {
		return fmt.Errorf("line %d: null cannot be used here", decl.Line)
	}
	sizeType, err := c.inferType(scope, decl.Size)
	if err != nil {
		return err
	}
	if sizeType != (ast.Type{Name: "Int"}) {
		return fmt.Errorf("line %d: array size must be Int, got %s", decl.Line, typeName(sizeType))
	}
	if decl.Init == nil {
		return nil
	}
	return c.checkArrayValue(scope, ast.Type{Name: decl.Type.Name}, decl.Init)
}

// checkArrayValue validates that value may initialize/reassign/return an
// array of element type elemType (seed_spec.md §4's truncate/pad rule
// applies uniformly to every case here): `null` (reset to base values),
// an array literal (each element checked against elemType), a plain
// variable of the same array type — needed not least because
// seed_spec.md §7's own `sample`/`result` example returns one — or a
// call to a function that returns a matching array type.
func (c *checker) checkArrayValue(scope *scope, elemType ast.Type, value ast.Expr) error {
	want := ast.Type{Name: elemType.Name, IsArray: true}
	switch v := value.(type) {
	case *ast.NullLit:
		return nil
	case *ast.ArrayLit:
		return c.checkArrayLiteral(scope, elemType, v)
	case *ast.Ident:
		typ, ok := scope.lookup(v.Name)
		if !ok {
			return fmt.Errorf("line %d: undefined variable %q", v.Line, v.Name)
		}
		if typ != want {
			return fmt.Errorf("line %d: cannot use %s as %s", v.Line, typeName(typ), typeName(want))
		}
		return nil
	case *ast.CallExpr:
		typ, hasValue, err := c.inferCallType(scope, v)
		if err != nil {
			return err
		}
		if !hasValue {
			return fmt.Errorf("line %d: %s has no return value", v.Line, v.Callee)
		}
		if typ != want {
			return fmt.Errorf("line %d: cannot use %s as %s", v.Line, typeName(typ), typeName(want))
		}
		return nil
	default:
		return fmt.Errorf("line %d: an array must come from a variable, an array literal, null, or a function call", ast.ExprLine(value))
	}
}

// checkArrayLiteral checks every element of lit against elemType (the
// array's element type — a scalar, never itself an array: seed_spec.md
// §0 forbids multi-dimensional arrays).
func (c *checker) checkArrayLiteral(scope *scope, elemType ast.Type, lit *ast.ArrayLit) error {
	for _, elem := range lit.Elems {
		if err := c.checkAssignable(scope, elemType, elem); err != nil {
			return err
		}
	}
	return nil
}

// checkGlobalVarDecl is checkVarDecl's counterpart for top-level
// declarations. Its initializer, unlike a local's, may only be a literal
// (and an array's size may only be an Int literal): GVAR has no
// initializer syntax, so codegen assigns it inside the generated !main
// wrapper with no Seed variables (and no other functions to call) in
// scope yet.
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
			return fmt.Errorf("line %d: a global array must be initialized with an array literal", ast.ExprLine(decl.Init))
		}
		empty := &checker{funcs: map[string]funcSig{}}
		return empty.checkArrayLiteral(newScope(nil), ast.Type{Name: decl.Type.Name}, lit)
	}
	if decl.Init == nil {
		return nil
	}
	empty := &checker{funcs: map[string]funcSig{}}
	return empty.checkAssignable(newScope(nil), decl.Type, decl.Init)
}

// checkAssignStmt checks `name = value` (whole-variable, scalar or
// array) and `name[Index] = value` (a single array element).
func (c *checker) checkAssignStmt(scope *scope, stmt *ast.AssignStmt) error {
	typ, ok := scope.lookup(stmt.Name)
	if !ok {
		return fmt.Errorf("line %d: undefined variable %q", stmt.Line, stmt.Name)
	}
	if stmt.Index != nil {
		return c.checkIndexedAssign(scope, typ, stmt)
	}
	if typ.IsArray {
		return c.checkArrayValue(scope, ast.Type{Name: typ.Name}, stmt.Value)
	}
	return c.checkAssignable(scope, typ, stmt.Value)
}

// checkIndexedAssign checks `name[Index] = value`: name must be an
// array, Index a non-null Int, and value assignable to the element type.
func (c *checker) checkIndexedAssign(scope *scope, arrType ast.Type, stmt *ast.AssignStmt) error {
	if !arrType.IsArray {
		return fmt.Errorf("line %d: %q is not an array", stmt.Line, stmt.Name)
	}
	if _, isNull := stmt.Index.(*ast.NullLit); isNull {
		return fmt.Errorf("line %d: null cannot be used here", stmt.Line)
	}
	idxType, err := c.inferType(scope, stmt.Index)
	if err != nil {
		return err
	}
	if idxType != (ast.Type{Name: "Int"}) {
		return fmt.Errorf("line %d: array index must be Int, got %s", stmt.Line, typeName(idxType))
	}
	return c.checkAssignable(scope, ast.Type{Name: arrType.Name}, stmt.Value)
}

// checkCompoundAssignStmt checks `name op= value` (seed_spec.md §5): both
// sides must satisfy the same type rules as the corresponding binary
// operator (see arithOpType), and null is never a valid operand. Arrays
// don't support any operator, compound assignment included.
func (c *checker) checkCompoundAssignStmt(scope *scope, stmt *ast.CompoundAssignStmt) error {
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
	valType, err := c.inferType(scope, stmt.Value)
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
func (c *checker) checkIncDecStmt(scope *scope, stmt *ast.IncDecStmt) error {
	typ, ok := scope.lookup(stmt.Name)
	if !ok {
		return fmt.Errorf("line %d: undefined variable %q", stmt.Line, stmt.Name)
	}
	if typ.IsArray || (typ.Name != "Int" && typ.Name != "Float") {
		return fmt.Errorf("line %d: %s expects an Int or Float variable, got %s", stmt.Line, stmt.Op, typeName(typ))
	}
	return nil
}

// checkReturnStmt checks `return` against retType (nil for a function
// with no return value). A scalar return type may not return `null`:
// unlike a local assignment, where resetting that one variable's isset
// flag is well-defined, "null" here would have to cross the function
// boundary into a caller-side variable, and scalars don't carry that
// information through a return value at all (see codegen.go's package
// doc — only a variable's own isset flag exists, there's no "this
// return was null" signal). An array return type has no such problem
// (arrays have no isset to begin with — see array.go), so `return null`
// is allowed there, mapping to an empty array.
func (c *checker) checkReturnStmt(scope *scope, stmt *ast.ReturnStmt, retType *ast.Type) error {
	if retType == nil {
		if stmt.X != nil {
			return fmt.Errorf("line %d: this function has no return value", stmt.Line)
		}
		return nil
	}
	if stmt.X == nil {
		return fmt.Errorf("line %d: this function must return a value", stmt.Line)
	}
	if retType.IsArray {
		return c.checkArrayValue(scope, ast.Type{Name: retType.Name}, stmt.X)
	}
	if _, isNull := stmt.X.(*ast.NullLit); isNull {
		return fmt.Errorf("line %d: cannot return null for a scalar return type", stmt.Line)
	}
	return c.checkAssignable(scope, *retType, stmt.X)
}

// checkAssignable validates that value may be assigned to a variable of
// type want: value's type must match exactly (Seed has no implicit
// conversions), or value may be the `null` literal, which is always
// assignable and resets the variable to its base value. want is always a
// scalar type here — checkArrayValue handles arrays instead, since an
// array literal has no context-free type of its own (see ast.ArrayLit's
// doc).
func (c *checker) checkAssignable(scope *scope, want ast.Type, value ast.Expr) error {
	if _, ok := value.(*ast.NullLit); ok {
		return nil
	}
	got, err := c.inferType(scope, value)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("line %d: cannot use %s as %s", ast.ExprLine(value), typeName(got), typeName(want))
	}
	return nil
}

// checkCall validates a statement-position call expression. Both a void
// call (print, or a user function with no return type) and a
// value-producing one (isnull, or a user function with a return type,
// its result simply discarded) are valid statements.
func (c *checker) checkCall(scope *scope, x ast.Expr) error {
	call, ok := x.(*ast.CallExpr)
	if !ok {
		return fmt.Errorf("line %d: only function calls are supported as statements", ast.ExprLine(x))
	}
	_, _, err := c.inferCallType(scope, call)
	return err
}

// typeName renders t for error messages, e.g. "Int" or "Int[]".
func typeName(t ast.Type) string {
	if t.IsArray {
		return t.Name + "[]"
	}
	return t.Name
}
