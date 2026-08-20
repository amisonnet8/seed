package codegen

import (
	"fmt"
	"strings"

	"github.com/amisonnet8/seed/internal/ast"
)

// typeToIR maps any Seed type (scalar or array) to its AMIVM-IR type
// token, registering the element type with slices if it's an array (see
// sliceRegistry).
func typeToIR(slices *sliceRegistry, t ast.Type) (string, error) {
	if t.IsArray {
		return slices.use(t.Name), nil
	}
	return seedTypeToIR(t)
}

// genFuncDecl compiles one Seed function declaration into a complete
// AMIVM-IR FUNC block. Every function — including main, via
// seedFuncAmivmName — is compiled the same way; main's special
// os.Args-in/os.Exit-out bridging happens separately, in Generate's
// !main wrapper.
func genFuncDecl(fn *ast.FuncDecl, globals map[string]varRef, slices *sliceRegistry, sigs map[string]funcSig) (string, error) {
	g := &funcGen{ctx: newFuncCtx(globals), slices: slices, sigs: sigs}
	g.ctx.push()
	defer g.ctx.pop()

	paramTypes := make([]string, len(fn.Params))
	for i, param := range fn.Params {
		irType, err := typeToIR(slices, param.Type)
		if err != nil {
			return "", err
		}
		paramTypes[i] = irType

		ref, err := g.ctx.declareParam(param.Name, param.Type, i+1)
		if err != nil {
			return "", fmt.Errorf("line %d: %s", fn.Line, err)
		}
		if !param.Type.IsArray {
			// A parameter is always already assigned by its caller; see
			// scope.go's declareParam for why it still gets an isset
			// flag (isnull() consistency) rather than skipping one.
			g.declareVar(ref.SetOp, "^bool")
			g.emit("\tSET\t%s\ttrue\n", ref.SetOp)
		}
	}

	var retTypes []string
	if fn.ReturnType != nil {
		irType, err := typeToIR(slices, *fn.ReturnType)
		if err != nil {
			return "", err
		}
		retTypes = []string{irType}
	}
	g.retType = fn.ReturnType

	if err := genBlock(g, fn.Body); err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "FUNC\t!%s", seedFuncAmivmName(fn.Name))
	for _, t := range paramTypes {
		b.WriteString("\t")
		b.WriteString(t)
	}
	b.WriteString("\t:")
	for _, t := range retTypes {
		b.WriteString("\t")
		b.WriteString(t)
	}
	b.WriteString("\n")
	for _, d := range g.decls {
		fmt.Fprintf(&b, "\tVAR\t%s\t%s\n", d.Op, d.IRType)
	}
	b.WriteString(g.b.String())
	b.WriteString("ENDFUNC\n")
	return b.String(), nil
}

// genCall compiles a call to a user-defined function (print/isnull are
// special-cased elsewhere — see expr.go's genCallValue and stmt.go's
// genExprStmt — and never reach here). wantValue controls whether the
// CALL captures a result: false emits `CALL : !name args...` (a void
// call, or a value-producing one used as a bare statement with its
// result simply discarded — both are valid AMIVM-IR/Go); true captures
// the result into a fresh temp of the right IR type and returns its
// operand.
//
// An array argument passes its variable's own operand directly, with no
// copy: that's what makes it "by reference" (seed_spec.md §7) — see
// scope.go's declareParam for the receiving side of the same mechanism.
func genCall(g *funcGen, call *ast.CallExpr, wantValue bool) (string, error) {
	args := make([]string, len(call.Args))
	for i, argExpr := range call.Args {
		if ident, ok := argExpr.(*ast.Ident); ok {
			if ref, found := g.ctx.lookup(ident.Name); found && ref.Type.IsArray {
				args[i] = ref.ValOp
				continue
			}
		}
		v, err := genValue(g, argExpr)
		if err != nil {
			return "", err
		}
		args[i] = v
	}

	var resultOp string
	if wantValue {
		sig, ok := g.sigs[call.Callee]
		if !ok || sig.Return == nil {
			return "", fmt.Errorf("codegen: call to %q has no return value (sema bug)", call.Callee)
		}
		irType, err := typeToIR(g.slices, *sig.Return)
		if err != nil {
			return "", err
		}
		resultOp = g.newTemp(irType)
	}

	// Built entirely from literal "\t%s" segments, with every operand
	// passed as a Fprintf argument rather than concatenated into the
	// format string itself: an operand may contain '%' (e.g. "%tmp_2"),
	// which Fprintf would otherwise try to interpret as its own verb.
	// multi1 (the result operand) is omitted entirely rather than passed
	// as an empty string when wantValue is false — AMIVM-IR's own
	// convention for "no result" (`CALL : callname ...`, no leading
	// field), not just an empty token before the colon.
	var format string
	var parts []any
	if wantValue {
		format = "\tCALL\t%s\t:\t%s"
		parts = []any{resultOp, "!" + seedFuncAmivmName(call.Callee)}
	} else {
		format = "\tCALL\t:\t%s"
		parts = []any{"!" + seedFuncAmivmName(call.Callee)}
	}
	for _, a := range args {
		format += "\t%s"
		parts = append(parts, a)
	}
	format += "\n"
	g.emit(format, parts...)
	return resultOp, nil
}
