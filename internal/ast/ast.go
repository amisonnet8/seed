// Package ast defines the abstract syntax tree for Seed programs.
package ast

// File is the root node of a parsed Seed source file.
type File struct {
	Globals []*VarDecl
	Funcs   []*FuncDecl
}

// Type is a Seed type reference: one of Int/Float/String/Bool/File,
// optionally as a slice-form array (Type[], seed_spec.md §7 — array
// parameters and return values are written without a size).
type Type struct {
	Name    string
	IsSlice bool
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
// case the variable starts out null. Used both as a top-level (global)
// declaration and as a statement inside a function body.
type VarDecl struct {
	Type Type
	Name string
	Init Expr
	Line int
}

func (*VarDecl) stmtNode() {}

// AssignStmt is a scalar assignment (seed_spec.md §4): `name = Value`.
// Array-element assignment is not represented here yet.
type AssignStmt struct {
	Name  string
	Value Expr
	Line  int
}

func (*AssignStmt) stmtNode() {}

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

// Expr is implemented by every expression node.
type Expr interface{ exprNode() }

// Ident is a reference to a declared variable.
type Ident struct {
	Name string
	Line int
}

func (*Ident) exprNode() {}

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

// CallExpr is a function call, e.g. print("hello").
type CallExpr struct {
	Callee string
	Args   []Expr
	Line   int
}

func (*CallExpr) exprNode() {}

// ExprLine returns the source line an expression node was parsed from.
func ExprLine(e Expr) int {
	switch v := e.(type) {
	case *Ident:
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
	case *CallExpr:
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
	case *ExprStmt:
		return v.Line
	case *ReturnStmt:
		return v.Line
	default:
		return 0
	}
}
