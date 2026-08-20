package codegen

import (
	"fmt"
	"strconv"

	"seed/internal/ast"
)

// genValue returns the AMIVM-IR operand for e's value, emitting whatever
// intermediate instructions/temporaries e needs first (see funcGen.newTemp).
func genValue(g *funcGen, e ast.Expr) (string, error) {
	switch v := e.(type) {
	case *ast.StringLit:
		return strconv.Quote(v.Value), nil
	case *ast.IntLit:
		return strconv.FormatInt(v.Value, 10), nil
	case *ast.FloatLit:
		return strconv.FormatFloat(v.Value, 'g', -1, 64), nil
	case *ast.BoolLit:
		if v.Value {
			return "true", nil
		}
		return "false", nil
	case *ast.Ident:
		ref, ok := g.ctx.lookup(v.Name)
		if !ok {
			return "", fmt.Errorf("line %d: undefined variable %q", v.Line, v.Name)
		}
		return ref.ValOp, nil
	case *ast.CallExpr:
		return genCallValue(g, v)
	case *ast.NullLit:
		return "", fmt.Errorf("line %d: null cannot be used here", v.Line)
	default:
		return "", fmt.Errorf("codegen: unsupported expression %T", e)
	}
}

func genCallValue(g *funcGen, call *ast.CallExpr) (string, error) {
	switch call.Callee {
	case "isnull":
		if len(call.Args) != 1 {
			return "", fmt.Errorf("line %d: isnull expects exactly 1 argument", call.Line)
		}
		ident, ok := call.Args[0].(*ast.Ident)
		if !ok {
			return "", fmt.Errorf("line %d: isnull expects a variable", call.Line)
		}
		ref, ok := g.ctx.lookup(ident.Name)
		if !ok {
			return "", fmt.Errorf("line %d: undefined variable %q", ident.Line, ident.Name)
		}
		tmp := g.newTemp("^bool")
		g.emit("\tNOT\t%s\t%s\n", tmp, ref.SetOp)
		return tmp, nil
	default:
		return "", fmt.Errorf("line %d: unsupported function call %q", call.Line, call.Callee)
	}
}
