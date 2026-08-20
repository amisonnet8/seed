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
	case *ast.UnaryExpr:
		return genUnary(g, v)
	case *ast.BinaryExpr:
		return genBinary(g, v)
	case *ast.NullLit:
		return "", fmt.Errorf("line %d: null cannot be used here", v.Line)
	default:
		return "", fmt.Errorf("codegen: unsupported expression %T", e)
	}
}

// genUnary emits the instruction for a prefix unary expression. AMIVM-IR
// has no dedicated negation instruction, so unary `-x` is emitted as the
// binary `0 - x` (SUB); `!x` maps directly to NOT.
func genUnary(g *funcGen, u *ast.UnaryExpr) (string, error) {
	x, err := genValue(g, u.X)
	if err != nil {
		return "", err
	}
	switch u.Op {
	case "!":
		tmp := g.newTemp("^bool")
		g.emit("\tNOT\t%s\t%s\n", tmp, x)
		return tmp, nil
	case "-":
		irType, err := seedTypeToIR(u.ResultType)
		if err != nil {
			return "", err
		}
		tmp := g.newTemp(irType)
		g.emit("\tSUB\t%s\t0\t%s\n", tmp, x)
		return tmp, nil
	default:
		return "", fmt.Errorf("codegen: unknown unary operator %q", u.Op)
	}
}

// arithInstr maps a -, *, /, or % operator to its AMIVM-IR instruction. +
// is handled separately in genBinary since it also covers CONCAT.
func arithInstr(op string) string {
	switch op {
	case "-":
		return "SUB"
	case "*":
		return "MUL"
	case "/":
		return "DIV"
	case "%":
		return "MOD"
	default:
		return ""
	}
}

func compareInstr(op string) string {
	switch op {
	case "<":
		return "LT"
	case "<=":
		return "LTE"
	case ">":
		return "GT"
	case ">=":
		return "GTE"
	case "==":
		return "EQ"
	case "!=":
		return "NEQ"
	default:
		return ""
	}
}

// genBinary emits the instruction for a binary expression, using the
// operand/result type sema.Check already recorded on b.ResultType to pick
// the right AMIVM-IR instruction and temp variable type.
func genBinary(g *funcGen, b *ast.BinaryExpr) (string, error) {
	x, err := genValue(g, b.X)
	if err != nil {
		return "", err
	}
	y, err := genValue(g, b.Y)
	if err != nil {
		return "", err
	}

	switch b.Op {
	case "+":
		irType, err := seedTypeToIR(b.ResultType)
		if err != nil {
			return "", err
		}
		tmp := g.newTemp(irType)
		if b.ResultType.Name == "String" {
			g.emit("\tCONCAT\t%s\t%s\t%s\n", tmp, x, y)
		} else {
			g.emit("\tADD\t%s\t%s\t%s\n", tmp, x, y)
		}
		return tmp, nil
	case "-", "*", "/", "%":
		irType, err := seedTypeToIR(b.ResultType)
		if err != nil {
			return "", err
		}
		tmp := g.newTemp(irType)
		g.emit("\t%s\t%s\t%s\t%s\n", arithInstr(b.Op), tmp, x, y)
		return tmp, nil
	case "<", "<=", ">", ">=", "==", "!=":
		tmp := g.newTemp("^bool")
		g.emit("\t%s\t%s\t%s\t%s\n", compareInstr(b.Op), tmp, x, y)
		return tmp, nil
	case "&&", "||":
		tmp := g.newTemp("^bool")
		instr := "AND"
		if b.Op == "||" {
			instr = "OR"
		}
		g.emit("\t%s\t%s\t%s\t%s\n", instr, tmp, x, y)
		return tmp, nil
	default:
		return "", fmt.Errorf("codegen: unknown binary operator %q", b.Op)
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
