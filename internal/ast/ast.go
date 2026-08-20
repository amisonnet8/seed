// Package ast defines the abstract syntax tree for Seed programs.
package ast

// File is the root node of a parsed Seed source file.
type File struct {
	Globals []*VarDecl
	Funcs   []*FuncDecl
}

// Type is a Seed type reference: one of Int/Float/String/Bool/File,
// optionally as an array (seed_spec.md §3 for a sized local/global
// declaration like `Int[100]`, or §7 for the unsized `Type[]` form used
// only in function parameter/return types). The declared size (if any)
// lives on VarDecl, not here: seed_spec.md §4's truncate/pad assignment
// rule means an array's element count is a runtime property, not part of
// its static type, so two array types are compatible regardless of size.
type Type struct {
	Name    string
	IsArray bool
}

// Param is a single function parameter.
type Param struct {
	Type Type
	Name string
}

// FuncDecl is a top-level function declaration (seed_spec.md §7).
// ReturnType is nil when the function has no return value.
type FuncDecl struct {
	Name       string
	ReturnType *Type
	Params     []Param
	Body       []Stmt
	Line       int
}

// Stmt is implemented by every statement node.
type Stmt interface{ stmtNode() }

// VarDecl is a variable declaration (seed_spec.md §3): `Type name` or
// `Type name = Init`. Init is nil when there is no initializer, in which
// case the variable starts out null (scalar) or zero-valued (array). Size
// is non-nil exactly when Type.IsArray is true — a local/global array
// declaration always carries an explicit size expression (`Int[100]` or
// `Int[size]`; the unsized `Int[]` form is only valid for a function
// parameter/return type, not here). Used both as a top-level (global)
// declaration and as a statement inside a function body.
type VarDecl struct {
	Type Type
	Name string
	Size Expr
	Init Expr
	Line int
}

func (*VarDecl) stmtNode() {}

// AssignStmt is an assignment (seed_spec.md §4): `name = Value` for a
// scalar or whole-array reassignment, or `name[Index] = Value` for a
// single array element when Index is non-nil.
type AssignStmt struct {
	Name  string
	Index Expr // nil for `name = value`; non-nil for `name[Index] = value`
	Value Expr
	Line  int
}

func (*AssignStmt) stmtNode() {}

// CompoundAssignStmt is a compound assignment (seed_spec.md §5): `name
// op= Value` where op is one of + - * / %. Like `++`/`--`, this only
// exists as a statement, never inside an expression.
type CompoundAssignStmt struct {
	Name  string
	Op    string // "+", "-", "*", "/", "%"
	Value Expr
	Line  int
}

func (*CompoundAssignStmt) stmtNode() {}

// IncDecStmt is a postfix `name++` or `name--` statement.
type IncDecStmt struct {
	Name string
	Op   string // "++" or "--"
	Line int
}

func (*IncDecStmt) stmtNode() {}

// ExprStmt is a statement consisting of a single expression (currently
// only call expressions are valid here).
type ExprStmt struct {
	X    Expr
	Line int
}

func (*ExprStmt) stmtNode() {}

// ReturnStmt is a `return` statement. X is nil for a bare `return`.
type ReturnStmt struct {
	X    Expr
	Line int
}

func (*ReturnStmt) stmtNode() {}

// IfClause is one `if`/`elif` condition-and-body pair.
type IfClause struct {
	Cond Expr
	Body []Stmt
}

// IfStmt is an if/elif/else chain (seed_spec.md §6). Clauses holds the
// `if` clause followed by zero or more `elif` clauses, in source order.
// Else is nil when there is no `else` clause.
type IfStmt struct {
	Clauses []IfClause
	Else    []Stmt
	Line    int
}

func (*IfStmt) stmtNode() {}

// WhileStmt is a `while` loop (seed_spec.md §6).
type WhileStmt struct {
	Cond Expr
	Body []Stmt
	Line int
}

func (*WhileStmt) stmtNode() {}

// ForInStmt is a `for x in a` loop (seed_spec.md §6): VarName takes each
// element of the array named ArrayName in turn (not its index). VarName's
// scope is the whole statement, same as any other block.
type ForInStmt struct {
	VarName   string
	ArrayName string
	Body      []Stmt
	Line      int
}

func (*ForInStmt) stmtNode() {}

// BreakStmt exits the innermost enclosing loop.
type BreakStmt struct {
	Line int
}

func (*BreakStmt) stmtNode() {}

// ContinueStmt advances the innermost enclosing loop to its next iteration.
type ContinueStmt struct {
	Line int
}

func (*ContinueStmt) stmtNode() {}

// Expr is implemented by every expression node.
type Expr interface{ exprNode() }

// Ident is a reference to a declared variable.
type Ident struct {
	Name string
	Line int
}

func (*Ident) exprNode() {}

// IndexExpr is an array element reference, e.g. `a[i]` (seed_spec.md §4).
type IndexExpr struct {
	Name  string
	Index Expr
	Line  int
}

func (*IndexExpr) exprNode() {}

// UnaryExpr is a prefix unary operator: `!x` or `-x`. ResultType is
// filled in by sema.Check; codegen relies on it to pick the right
// AMIVM-IR type for the temporary that holds the result.
type UnaryExpr struct {
	Op         string // "!" or "-"
	X          Expr
	Line       int
	ResultType Type
}

func (*UnaryExpr) exprNode() {}

// BinaryExpr is a binary operator expression (seed_spec.md §5).
// ResultType is filled in by sema.Check; codegen relies on it both to
// pick the right AMIVM-IR type for the temporary that holds the result,
// and to disambiguate `+` (ADD for Int/Float, CONCAT for String).
type BinaryExpr struct {
	Op         string // "+", "-", "*", "/", "%", "==", "!=", "<", "<=", ">", ">=", "&&", "||"
	X, Y       Expr
	Line       int
	ResultType Type
}

func (*BinaryExpr) exprNode() {}

// StringLit is a string literal.
type StringLit struct {
	Value string
	Line  int
}

func (*StringLit) exprNode() {}

// IntLit is an integer literal.
type IntLit struct {
	Value int64
	Line  int
}

func (*IntLit) exprNode() {}

// FloatLit is a floating-point literal.
type FloatLit struct {
	Value float64
	Line  int
}

func (*FloatLit) exprNode() {}

// BoolLit is a `true`/`false` literal.
type BoolLit struct {
	Value bool
	Line  int
}

func (*BoolLit) exprNode() {}

// NullLit is the `null` literal (seed_spec.md §0's "null について").
type NullLit struct {
	Line int
}

func (*NullLit) exprNode() {}

// ArrayLit is an array literal, e.g. `{1, 2, 3}` (seed_spec.md §2). Its
// element type is determined entirely by the assignment context (a
// VarDecl's declared type or an AssignStmt's target), not by the literal
// itself — so, unlike every other Expr, it is only ever type-checked via
// sema's checkArrayLiteral, never through the general inferType path.
type ArrayLit struct {
	Elems []Expr
	Line  int
}

func (*ArrayLit) exprNode() {}

// CallExpr is a function call, e.g. print("hello").
//
// ArgType is filled in by sema.Check only for the overloaded builtins
// int/float/string/len (seed_spec.md §9), which each accept several
// argument types and need different AMIVM-IR per one. It holds Args[0]'s
// resolved type, so codegen can pick the right instruction/CALL without
// re-deriving a type on its own (mirroring BinaryExpr/UnaryExpr's
// ResultType). It's the zero Type for every other call.
type CallExpr struct {
	Callee  string
	Args    []Expr
	Line    int
	ArgType Type
}

func (*CallExpr) exprNode() {}

// ExprLine returns the source line an expression node was parsed from.
func ExprLine(e Expr) int {
	switch v := e.(type) {
	case *Ident:
		return v.Line
	case *IndexExpr:
		return v.Line
	case *StringLit:
		return v.Line
	case *IntLit:
		return v.Line
	case *FloatLit:
		return v.Line
	case *BoolLit:
		return v.Line
	case *NullLit:
		return v.Line
	case *ArrayLit:
		return v.Line
	case *CallExpr:
		return v.Line
	case *UnaryExpr:
		return v.Line
	case *BinaryExpr:
		return v.Line
	default:
		return 0
	}
}

// StmtLine returns the source line a statement node was parsed from.
func StmtLine(s Stmt) int {
	switch v := s.(type) {
	case *VarDecl:
		return v.Line
	case *AssignStmt:
		return v.Line
	case *CompoundAssignStmt:
		return v.Line
	case *IncDecStmt:
		return v.Line
	case *ExprStmt:
		return v.Line
	case *ReturnStmt:
		return v.Line
	case *IfStmt:
		return v.Line
	case *WhileStmt:
		return v.Line
	case *ForInStmt:
		return v.Line
	case *BreakStmt:
		return v.Line
	case *ContinueStmt:
		return v.Line
	default:
		return 0
	}
}
