// Package codegen translates a type-checked Seed AST into AMIVM-IR.
//
// Seed's `main` cannot be emitted as amivm's `!main` directly: Go requires
// literal `func main()` to take no arguments and return nothing, but Seed's
// entry point has signature `func Int main(String[] args)`. So the user's
// main is instead emitted as an ordinary function (`!seed_main`), and a
// small generated `!main` wrapper bridges the two: it passes os.Args in,
// and turns the returned Int into a process exit code via os.Exit.
package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"seed/internal/ast"
)

// seedMainFunc is the amivm-level name for the user's `main`. It must
// never collide with a Seed-level function name once user-defined
// functions are supported (seed_spec.md's own `main` is reserved and
// forbidden as an ordinary call target, but this internal name lives in
// the same amivm namespace as Seed function names and needs its own
// reservation once general FuncDecls are compiled).
const seedMainFunc = "seed_main"

const stringSliceType = "^Stringslice"

// Generate translates f (already validated by sema.Check) into AMIVM-IR text.
func Generate(f *ast.File) (string, error) {
	var main *ast.FuncDecl
	for _, fn := range f.Funcs {
		if fn.Name == "main" {
			main = fn
		}
	}
	if main == nil {
		return "", fmt.Errorf("codegen: no main function (run sema.Check first)")
	}

	var b strings.Builder

	fmt.Fprintf(&b, "SLTYPE\t%s\t^string\n", stringSliceType)

	fmt.Fprintf(&b, "FUNC\t!%s\t%s\t:\t^int\n", seedMainFunc, stringSliceType)
	for _, stmt := range main.Body {
		line, err := genStmt(stmt)
		if err != nil {
			return "", err
		}
		b.WriteString(line)
	}
	b.WriteString("ENDFUNC\n")

	fmt.Fprintf(&b, "FUNC\t!main\t:\n")
	b.WriteString("\tVAR\t%exitcode\t^int\n")
	fmt.Fprintf(&b, "\tCALL\t%%exitcode\t:\t!%s\t@os.Args\n", seedMainFunc)
	b.WriteString("\tCALL\t:\t?os.Exit\t%exitcode\n")
	b.WriteString("\tRET\n")
	b.WriteString("ENDFUNC\n")

	return b.String(), nil
}

func genStmt(s ast.Stmt) (string, error) {
	switch st := s.(type) {
	case *ast.ExprStmt:
		return genExprStmt(st)
	case *ast.ReturnStmt:
		return genReturnStmt(st)
	default:
		return "", fmt.Errorf("codegen: unsupported statement %T", s)
	}
}

func genExprStmt(st *ast.ExprStmt) (string, error) {
	call, ok := st.X.(*ast.CallExpr)
	if !ok {
		return "", fmt.Errorf("codegen: unsupported statement expression %T", st.X)
	}
	switch call.Callee {
	case "print":
		if len(call.Args) != 1 {
			return "", fmt.Errorf("line %d: print expects exactly 1 argument", call.Line)
		}
		arg, err := genValue(call.Args[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("\tCALL\t:\t?fmt.Println\t%s\n", arg), nil
	default:
		return "", fmt.Errorf("line %d: unsupported function call %q", call.Line, call.Callee)
	}
}

func genReturnStmt(st *ast.ReturnStmt) (string, error) {
	if st.X == nil {
		return "\tRET\n", nil
	}
	v, err := genValue(st.X)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("\tRET\t%s\n", v), nil
}

func genValue(e ast.Expr) (string, error) {
	switch v := e.(type) {
	case *ast.StringLit:
		return strconv.Quote(v.Value), nil
	case *ast.IntLit:
		return strconv.FormatInt(v.Value, 10), nil
	default:
		return "", fmt.Errorf("codegen: unsupported expression %T", e)
	}
}
