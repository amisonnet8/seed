package codegen

import (
	"fmt"

	"github.com/amisonnet8/seed/internal/ast"
)

// genOpenCall compiles open(path, mode) (seed_spec.md §8/§9) to a call
// into seedrt, which does the "r"/"w" -> os.Open/os.Create branching and
// wraps the result with a persistent buffered reader (see seedrt's
// package doc — needed for read() to work correctly across calls).
func genOpenCall(g *funcGen, call *ast.CallExpr) (string, error) {
	pathOp, err := genValue(g, call.Args[0])
	if err != nil {
		return "", err
	}
	modeOp, err := genValue(g, call.Args[1])
	if err != nil {
		return "", err
	}
	tmp := g.newTemp("^*seedrt.File")
	g.emit("\tCALL\t%s\t:\t?seedrt.Open\t%s\t%s\n", tmp, pathOp, modeOp)
	return tmp, nil
}

// genWriteStmt compiles write(file, line) (seed_spec.md §8/§9).
func genWriteStmt(g *funcGen, call *ast.CallExpr) error {
	fileOp, err := genValue(g, call.Args[0])
	if err != nil {
		return err
	}
	lineOp, err := genValue(g, call.Args[1])
	if err != nil {
		return err
	}
	g.emit("\tCALL\t:\t?seedrt.Write\t%s\t%s\n", fileOp, lineOp)
	return nil
}

// genCloseStmt compiles close(file) (seed_spec.md §8/§9).
func genCloseStmt(g *funcGen, call *ast.CallExpr) error {
	fileOp, err := genValue(g, call.Args[0])
	if err != nil {
		return err
	}
	g.emit("\tCALL\t:\t?seedrt.Close\t%s\n", fileOp)
	return nil
}

// genReadAssign compiles `ref = read(file)` (seed_spec.md §8). It's the
// one place seedrt.Read's (string, bool) result gets used: AMIVM-IR's
// multi-result CALL lets both land directly in ref's own value and isset
// operands (`CALL value isset : ?seedrt.Read file`), which is exactly
// what "read() returns null at EOF" means once ref is an ordinary Seed
// variable — see sema's checkReadCall for why this shape is the only
// place read() is allowed to appear.
func genReadAssign(g *funcGen, ref varRef, call *ast.CallExpr) error {
	fileOp, err := genValue(g, call.Args[0])
	if err != nil {
		return err
	}
	g.emit("\tCALL\t%s\t%s\t:\t?seedrt.Read\t%s\n", ref.ValOp, ref.SetOp, fileOp)
	return nil
}

// genIntConversion compiles int(value) (seed_spec.md §9), dispatching on
// call.ArgType (set by sema — see ast.CallExpr's doc). Int/Float sources
// use a bare Go conversion (valid AMIVM-IR via CALL — see CLAUDE.md's
// "amivmの書き方" — since a Go type conversion and a function call share
// the same ast.CallExpr shape); Bool/String need a seedrt helper, since
// Go has neither a bool->int conversion nor a numeric-parsing one.
func genIntConversion(g *funcGen, call *ast.CallExpr) (string, error) {
	argOp, err := genValue(g, call.Args[0])
	if err != nil {
		return "", err
	}
	tmp := g.newTemp("^int")
	switch call.ArgType.Name {
	case "Int":
		return argOp, nil
	case "Float":
		g.emit("\tCALL\t%s\t:\t?int\t%s\n", tmp, argOp)
	case "Bool":
		g.emit("\tCALL\t%s\t:\t?seedrt.BoolToInt\t%s\n", tmp, argOp)
	case "String":
		g.emit("\tCALL\t%s\t:\t?seedrt.ParseInt\t%s\n", tmp, argOp)
	default:
		return "", fmt.Errorf("codegen: int() unsupported for %s (sema bug)", call.ArgType.Name)
	}
	return tmp, nil
}

// genFloatConversion compiles float(value) (seed_spec.md §9). Int uses a
// bare Go conversion; String needs seedrt's parser.
func genFloatConversion(g *funcGen, call *ast.CallExpr) (string, error) {
	argOp, err := genValue(g, call.Args[0])
	if err != nil {
		return "", err
	}
	if call.ArgType.Name == "Float" {
		return argOp, nil
	}
	tmp := g.newTemp("^float64")
	switch call.ArgType.Name {
	case "Int":
		g.emit("\tCALL\t%s\t:\t?float64\t%s\n", tmp, argOp)
	case "String":
		g.emit("\tCALL\t%s\t:\t?seedrt.ParseFloat\t%s\n", tmp, argOp)
	default:
		return "", fmt.Errorf("codegen: float() unsupported for %s (sema bug)", call.ArgType.Name)
	}
	return tmp, nil
}

// genStringConversion compiles string(value) (seed_spec.md §9). Every
// source type here has a direct stdlib formatter, so none of this needs
// seedrt: Go's own `string(intVal)` would produce a Unicode code point
// (e.g. string(65) is "A"), not the decimal text Seed wants, which is
// exactly what strconv avoids.
func genStringConversion(g *funcGen, call *ast.CallExpr) (string, error) {
	argOp, err := genValue(g, call.Args[0])
	if err != nil {
		return "", err
	}
	tmp := g.newTemp("^string")
	switch call.ArgType.Name {
	case "Int":
		g.emit("\tCALL\t%s\t:\t?strconv.Itoa\t%s\n", tmp, argOp)
	case "Float":
		g.emit("\tCALL\t%s\t:\t?strconv.FormatFloat\t%s\t'g'\t-1\t64\n", tmp, argOp)
	case "Bool":
		g.emit("\tCALL\t%s\t:\t?strconv.FormatBool\t%s\n", tmp, argOp)
	default:
		return "", fmt.Errorf("codegen: string() unsupported for %s (sema bug)", call.ArgType.Name)
	}
	return tmp, nil
}

// genLenCall compiles len(value) (seed_spec.md §9). A String's length is
// its rune count, not len(string)'s byte count — the two differ for any
// non-ASCII text — so this uses unicode/utf8 rather than the bare
// builtin; an array's length is its element count, i.e. Go's plain len()
// (already used internally for array codegen — see array.go).
func genLenCall(g *funcGen, call *ast.CallExpr) (string, error) {
	argOp, err := genValue(g, call.Args[0])
	if err != nil {
		return "", err
	}
	tmp := g.newTemp("^int")
	if call.ArgType.IsArray {
		g.emit("\tCALL\t%s\t:\t?len\t%s\n", tmp, argOp)
		return tmp, nil
	}
	switch call.ArgType.Name {
	case "String":
		g.emit("\tCALL\t%s\t:\t?utf8.RuneCountInString\t%s\n", tmp, argOp)
	default:
		return "", fmt.Errorf("codegen: len() unsupported for %s (sema bug)", call.ArgType.Name)
	}
	return tmp, nil
}
