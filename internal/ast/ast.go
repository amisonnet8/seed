// Package ast defines the abstract syntax tree for Seed programs.
package ast

// File is the root node of a parsed Seed source file.
type File struct {
	Funcs []*FuncDecl
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

// CallExpr is a function call, e.g. print("hello").
type CallExpr struct {
	Callee string
	Args   []Expr
	Line   int
}

func (*CallExpr) exprNode() {}
