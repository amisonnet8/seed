// Package codegen translates a type-checked Seed AST into AMIVM-IR.
//
// Two design decisions worth knowing before reading the rest of this
// package:
//
//   - Seed's `main` cannot be emitted as amivm's `!main` directly: Go
//     requires literal `func main()` to take no arguments and return
//     nothing, but Seed's entry point has signature
//     `func Int main(String[] args)`. So the user's main is emitted as an
//     ordinary function (`!seed_main`), and a small generated `!main`
//     wrapper bridges the two: it passes os.Args in and turns the
//     returned Int into a process exit code via os.Exit. Global variable
//     initializers (which GVAR alone can't express — it only emits
//     `var x T`, never `var x T = v`) are also assigned inside this
//     wrapper, before !seed_main runs.
//
//   - Seed's null/base-value semantics (seed_spec.md §0) are represented
//     as a value variable plus a companion "isset" bool variable per
//     Seed variable (see scope.go's varRef). An ordinary read of the
//     value variable already behaves correctly with no extra check:
//     Go's zero value for int/float64/string/bool/*os.File happens to be
//     exactly Seed's base value for Int/Float/String/Bool/File. Only
//     isnull() needs the isset flag; a null assignment additionally
//     resets the value to its zero form so a later plain read still sees
//     the base value.
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
// the same amivm namespace as Seed function names and will need its own
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

	globals := map[string]varRef{}
	var globalDecls, globalInit strings.Builder
	for _, g := range f.Globals {
		ref, err := genGlobalVarDecl(&globalDecls, &globalInit, g)
		if err != nil {
			return "", err
		}
		globals[g.Name] = ref
	}
	b.WriteString(globalDecls.String())

	fmt.Fprintf(&b, "FUNC\t!%s\t%s\t:\t^int\n", seedMainFunc, stringSliceType)
	g := &funcGen{ctx: newFuncCtx(globals)}
	if err := genBlock(g, main.Body); err != nil {
		return "", err
	}
	b.WriteString(g.b.String())
	b.WriteString("ENDFUNC\n")

	b.WriteString("FUNC\t!main\t:\n")
	b.WriteString(globalInit.String())
	b.WriteString("\tVAR\t%exitcode\t^int\n")
	fmt.Fprintf(&b, "\tCALL\t%%exitcode\t:\t!%s\t@os.Args\n", seedMainFunc)
	b.WriteString("\tCALL\t:\t?os.Exit\t%exitcode\n")
	b.WriteString("\tRET\n")
	b.WriteString("ENDFUNC\n")

	return b.String(), nil
}

// genGlobalVarDecl emits `decl`'s GVAR declarations into decls, and (if it
// has a non-null initializer) the SET statements that assign its initial
// value into init — those run inside the generated !main wrapper, since
// GVAR itself has no initializer syntax.
func genGlobalVarDecl(decls, init *strings.Builder, decl *ast.VarDecl) (varRef, error) {
	irType, err := seedTypeToIR(decl.Type)
	if err != nil {
		return varRef{}, err
	}
	ref := varRef{Type: decl.Type, ValOp: "@" + decl.Name, SetOp: "@" + decl.Name + "_isset"}
	fmt.Fprintf(decls, "GVAR\t%s\t%s\n", ref.ValOp, irType)
	fmt.Fprintf(decls, "GVAR\t%s\t^bool\n", ref.SetOp)

	if decl.Init == nil {
		return ref, nil
	}
	if _, isNull := decl.Init.(*ast.NullLit); isNull {
		return ref, nil
	}
	// Global initializers are restricted (by sema, once it enforces this)
	// to literals, so no temp variables/instructions are ever needed here.
	v, err := genValue(&funcGen{ctx: newFuncCtx(nil)}, decl.Init)
	if err != nil {
		return varRef{}, err
	}
	fmt.Fprintf(init, "\tSET\t%s\t%s\n", ref.ValOp, v)
	fmt.Fprintf(init, "\tSET\t%s\ttrue\n", ref.SetOp)
	return ref, nil
}

// seedTypeToIR maps a scalar Seed type to its AMIVM-IR type token. Array
// types are not handled yet (Step 5).
func seedTypeToIR(t ast.Type) (string, error) {
	if t.IsSlice {
		return "", fmt.Errorf("codegen: array types are not supported yet")
	}
	switch t.Name {
	case "Int":
		return "^int", nil
	case "Float":
		return "^float64", nil
	case "String":
		return "^string", nil
	case "Bool":
		return "^bool", nil
	case "File":
		return "^*os.File", nil
	default:
		return "", fmt.Errorf("codegen: unknown type %q", t.Name)
	}
}

// zeroValueLiteral is the AMIVM-IR value token for t's Seed base value,
// which happens to coincide with Go's zero value in every case.
func zeroValueLiteral(t ast.Type) (string, error) {
	switch t.Name {
	case "Int", "Float":
		return "0", nil
	case "String":
		return strconv.Quote(""), nil
	case "Bool":
		return "false", nil
	case "File":
		return "nil", nil
	default:
		return "", fmt.Errorf("codegen: unknown type %q", t.Name)
	}
}

// funcGen accumulates the AMIVM-IR body of a single function being
// compiled, alongside the scope/name-mangling state needed to resolve
// Seed variable references and mint fresh temporaries.
type funcGen struct {
	b   strings.Builder
	ctx *funcCtx
}

func (g *funcGen) emit(format string, args ...any) {
	fmt.Fprintf(&g.b, format, args...)
}

// newTemp declares a fresh local variable of the given AMIVM-IR type
// (e.g. "^bool") to hold an intermediate result, and returns its operand.
func (g *funcGen) newTemp(irType string) string {
	name := g.ctx.freshInternal("tmp")
	g.emit("\tVAR\t%%%s\t%s\n", name, irType)
	return "%" + name
}
