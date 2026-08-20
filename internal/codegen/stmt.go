package codegen

import (
	"fmt"

	"seed/internal/ast"
)

func genBlock(g *funcGen, stmts []ast.Stmt) error {
	for _, stmt := range stmts {
		if err := genStmt(g, stmt); err != nil {
			return err
		}
	}
	return nil
}

func genStmt(g *funcGen, s ast.Stmt) error {
	switch st := s.(type) {
	case *ast.VarDecl:
		return genVarDecl(g, st)
	case *ast.AssignStmt:
		return genAssignStmt(g, st)
	case *ast.ExprStmt:
		return genExprStmt(g, st)
	case *ast.ReturnStmt:
		return genReturnStmt(g, st)
	default:
		return fmt.Errorf("codegen: unsupported statement %T", s)
	}
}

func genVarDecl(g *funcGen, decl *ast.VarDecl) error {
	irType, err := seedTypeToIR(decl.Type)
	if err != nil {
		return err
	}
	ref, err := g.ctx.declare(decl.Name, decl.Type)
	if err != nil {
		return fmt.Errorf("line %d: %w", decl.Line, err)
	}
	g.emit("\tVAR\t%s\t%s\n", ref.ValOp, irType)
	g.emit("\tVAR\t%s\t^bool\n", ref.SetOp)

	if decl.Init == nil {
		return nil
	}
	return genAssign(g, ref, decl.Init)
}

func genAssignStmt(g *funcGen, stmt *ast.AssignStmt) error {
	ref, ok := g.ctx.lookup(stmt.Name)
	if !ok {
		return fmt.Errorf("line %d: undefined variable %q", stmt.Line, stmt.Name)
	}
	return genAssign(g, ref, stmt.Value)
}

// genAssign emits the SET(s) that store value into ref, keeping its isset
// flag in sync. Assigning `null` resets the underlying value to its base
// form too, so that a later plain read (which never re-checks isset)
// still observes the base value.
func genAssign(g *funcGen, ref varRef, value ast.Expr) error {
	if _, isNull := value.(*ast.NullLit); isNull {
		zero, err := zeroValueLiteral(ref.Type)
		if err != nil {
			return err
		}
		g.emit("\tSET\t%s\t%s\n", ref.ValOp, zero)
		g.emit("\tSET\t%s\tfalse\n", ref.SetOp)
		return nil
	}
	v, err := genValue(g, value)
	if err != nil {
		return err
	}
	g.emit("\tSET\t%s\t%s\n", ref.ValOp, v)
	g.emit("\tSET\t%s\ttrue\n", ref.SetOp)
	return nil
}

func genExprStmt(g *funcGen, st *ast.ExprStmt) error {
	call, ok := st.X.(*ast.CallExpr)
	if !ok {
		return fmt.Errorf("codegen: unsupported statement expression %T", st.X)
	}
	switch call.Callee {
	case "print":
		if len(call.Args) != 1 {
			return fmt.Errorf("line %d: print expects exactly 1 argument", call.Line)
		}
		arg, err := genValue(g, call.Args[0])
		if err != nil {
			return err
		}
		g.emit("\tCALL\t:\t?fmt.Println\t%s\n", arg)
		return nil
	default:
		return fmt.Errorf("line %d: unsupported function call %q", call.Line, call.Callee)
	}
}

func genReturnStmt(g *funcGen, st *ast.ReturnStmt) error {
	if st.X == nil {
		g.emit("\tRET\n")
		return nil
	}
	v, err := genValue(g, st.X)
	if err != nil {
		return err
	}
	g.emit("\tRET\t%s\n", v)
	return nil
}
