// Package codegen translates a type-checked Seed AST into AMIVM-IR.
//
// Design decisions worth knowing before reading the rest of this package:
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
//     Seed scalar variable (see scope.go's varRef). An ordinary read of
//     the value variable already behaves correctly with no extra check:
//     Go's zero value for int/float64/string/bool/*os.File happens to be
//     exactly Seed's base value for Int/Float/String/Bool/File. Only
//     isnull() needs the isset flag; a null assignment additionally
//     resets the value to its zero form so a later plain read still sees
//     the base value. Arrays don't get an isset companion at all — see
//     array.go's doc for why.
//
// A third, added in Step 4 (see stmt.go's genIfStmt/genWhileStmt):
// AMIVM-IR has no structured control-flow instructions, only LABEL/GOTO
// and a conditional "IF cond label" that jumps only when true. Every
// Seed block (`{ }`) also gets its own funcCtx scope via genBlock, even
// though the resulting Go variables all still live in one flat function
// namespace (see scope.go) — this is what makes shadowing inside an
// if/while/for-in body safe.
//
// A fourth, forced by the third: since every Seed variable (and every
// expression temp) ends up as a flat `var` in one Go function body with
// no enclosing braces, a forward jump that skips a `VAR` — an `elif`
// chain skipping a later clause's body, a `break` jumping past the rest
// of a loop body — trips Go's "goto jumps over variable declaration"
// rule the moment that skipped body declares anything, which if/while
// bodies routinely do. So funcGen hoists every VAR to the top of the
// function (see decls/newTemp below) and never emits one inline; only
// the SET that actually assigns a value stays at its original position.
// A declaration with no initializer therefore still needs a SET — see
// stmt.go's genVarDecl resetting to `null` — so that re-running the same
// declaration on a later loop iteration still resets the variable,
// instead of silently keeping whatever the previous iteration left in
// the (now one-time-only zeroed) hoisted slot.
package codegen

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"seed/internal/ast"
)

// seedMainFunc is the amivm-level name for the user's `main`. It must
// never collide with a Seed-level function name — sema.Check rejects a
// user function literally named "seed_main" for exactly this reason
// (see sema's seedMainInternalName, which must match this constant).
const seedMainFunc = "seed_main"

// seedFuncAmivmName maps a Seed-level function name to its amivm-level
// one: "main" becomes seedMainFunc (see codegen.go's package doc for
// why), and every other name passes through unchanged.
func seedFuncAmivmName(name string) string {
	if name == "main" {
		return seedMainFunc
	}
	return name
}

// funcSig is a compiled function's signature, needed at every call site
// to know each parameter's type (scalar vs. array — array arguments are
// passed by reference, see scope.go's declareParam) and the AMIVM-IR
// type of its return value, if it has one.
type funcSig struct {
	Params []ast.Type
	Return *ast.Type
}

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

	sigs := map[string]funcSig{}
	for _, fn := range f.Funcs {
		params := make([]ast.Type, len(fn.Params))
		for i, p := range fn.Params {
			params[i] = p.Type
		}
		sigs[fn.Name] = funcSig{Params: params, Return: fn.ReturnType}
	}

	// "String" is always needed for main's String[] args parameter, even
	// if the program itself never declares a String array.
	slices := &sliceRegistry{used: map[string]bool{"String": true}}

	// initGen accumulates every global's runtime initialization (SET,
	// or SLMAKE+ASET for an array) in declaration order; its body and
	// hoisted temps both end up inside the generated !main, before the
	// call to !seed_main. globalDecls instead holds the GVAR lines
	// themselves, which — like SLTYPE — are top-level and precede every
	// FUNC.
	globals := map[string]varRef{}
	var globalDecls strings.Builder
	initGen := &funcGen{ctx: newFuncCtx(nil), slices: slices, sigs: sigs}
	for _, gdecl := range f.Globals {
		ref, err := genGlobalVarDecl(initGen, &globalDecls, gdecl)
		if err != nil {
			return "", err
		}
		globals[gdecl.Name] = ref
	}

	var funcsIR strings.Builder
	for _, fn := range f.Funcs {
		ir, err := genFuncDecl(fn, globals, slices, sigs)
		if err != nil {
			return "", err
		}
		funcsIR.WriteString(ir)
	}

	var b strings.Builder
	for _, name := range slices.sorted() {
		elemIRType, err := seedTypeToIR(ast.Type{Name: name})
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "SLTYPE\t%s\t%s\n", sliceTypeToken(name), elemIRType)
	}

	b.WriteString(globalDecls.String())
	b.WriteString(funcsIR.String())

	b.WriteString("FUNC\t!main\t:\n")
	b.WriteString("\tVAR\t%exitcode\t^int\n")
	for _, d := range initGen.decls {
		fmt.Fprintf(&b, "\tVAR\t%s\t%s\n", d.Op, d.IRType)
	}
	b.WriteString(initGen.b.String())
	fmt.Fprintf(&b, "\tCALL\t%%exitcode\t:\t!%s\t@os.Args\n", seedMainFunc)
	b.WriteString("\tCALL\t:\t?os.Exit\t%exitcode\n")
	b.WriteString("\tRET\n")
	b.WriteString("ENDFUNC\n")

	return b.String(), nil
}

// sliceRegistry tracks which element types need a top-level SLTYPE
// declaration for their array/slice form (see sliceTypeToken). It's
// shared by every funcGen used within one Generate() call — including
// the throwaway one genGlobalVarDecl uses to evaluate literal
// initializers — since SLTYPE declarations are emitted once, up front,
// for the whole program.
type sliceRegistry struct {
	used map[string]bool
}

func (r *sliceRegistry) use(elemName string) string {
	r.used[elemName] = true
	return sliceTypeToken(elemName)
}

func (r *sliceRegistry) sorted() []string {
	names := make([]string, 0, len(r.used))
	for name := range r.used {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// sliceTypeToken is the AMIVM-IR type name for elemName's array/slice
// form, e.g. "Int" -> "^Intslice". Seed arrays compile to Go slices
// (SLTYPE+SLMAKE), not Go's fixed-size array type: a declared size like
// `Int[n]` can be a runtime variable, but Go's `[N]T` array type requires
// a compile-time constant N, so only a slice can represent every
// declaration seed_spec.md §3 allows. "Fixed-length" is still enforced —
// just by Seed never generating a resize, not by the Go type itself.
func sliceTypeToken(elemName string) string {
	return "^" + elemName + "slice"
}

// genGlobalVarDecl emits `decl`'s GVAR declaration(s) into decls, and (if
// it has a non-null initializer) the instructions that assign its
// initial value into g's body — g is the shared init funcGen whose body
// and hoisted temps end up inside the generated !main wrapper, since
// GVAR itself has no initializer syntax (see Generate's doc comment).
func genGlobalVarDecl(g *funcGen, decls *strings.Builder, decl *ast.VarDecl) (varRef, error) {
	if decl.Type.IsArray {
		return genGlobalArrayVarDecl(g, decls, decl)
	}

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
	// Global scalar initializers are restricted by sema to literals, but
	// e.g. `Float f = -1.5` is still a UnaryExpr that needs a temp, so
	// this goes through genValue/g like any other value, not a shortcut.
	v, err := genValue(g, decl.Init)
	if err != nil {
		return varRef{}, err
	}
	g.emit("\tSET\t%s\t%s\n", ref.ValOp, v)
	g.emit("\tSET\t%s\ttrue\n", ref.SetOp)
	return ref, nil
}

// seedTypeToIR maps a scalar Seed type to its AMIVM-IR type token. t must
// not be an array — see sliceTypeToken for that.
func seedTypeToIR(t ast.Type) (string, error) {
	if t.IsArray {
		return "", fmt.Errorf("codegen: seedTypeToIR called on an array type %q (use sliceTypeToken)", t.Name)
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
		// *seedrt.File, not *os.File: read() needs a persistent buffered
		// reader alongside the *os.File (see seedrt's package doc), and
		// Seed's File has no room for anything beyond a single pointer's
		// worth of state otherwise.
		return "^*seedrt.File", nil
	default:
		return "", fmt.Errorf("codegen: unknown type %q", t.Name)
	}
}

// zeroValueLiteral is the AMIVM-IR value token for t's Seed base value,
// which happens to coincide with Go's zero value in every case. t must
// be scalar (see seedTypeToIR).
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
// Seed variable references and mint fresh temporaries/labels. decls and
// b are kept separate — and only combined by the caller once generation
// finishes — so that every VAR ends up hoisted before any control flow
// (see codegen.go's package doc).
type funcGen struct {
	decls     []varDecl
	b         strings.Builder
	ctx       *funcCtx
	slices    *sliceRegistry
	sigs      map[string]funcSig
	retType   *ast.Type // this function's return type, nil if it has none; see stmt.go's genReturnValue
	labelSeq  int
	loopStack []loopLabels
}

// varDecl is one hoisted VAR line, emitted at the top of the function.
type varDecl struct {
	Op     string
	IRType string
}

// loopLabels is the pair of labels a while/for-in loop pushes for
// break/continue to target. For while, Continue re-checks the condition
// directly; for-in instead points Continue at its index-increment step
// (see stmt.go's genForInStmt) so `continue` doesn't skip advancing to
// the next element.
type loopLabels struct {
	Continue string
	Break    string
}

func (g *funcGen) emit(format string, args ...any) {
	fmt.Fprintf(&g.b, format, args...)
}

// declareVar records a hoisted VAR for op (see funcGen's doc comment).
func (g *funcGen) declareVar(op, irType string) {
	g.decls = append(g.decls, varDecl{Op: op, IRType: irType})
}

// newTemp declares a fresh local variable of the given AMIVM-IR type
// (e.g. "^bool") to hold an intermediate result, and returns its operand.
func (g *funcGen) newTemp(irType string) string {
	name := g.ctx.freshInternal("tmp")
	op := "%" + name
	g.declareVar(op, irType)
	return op
}

// newLabel mints a fresh label name, unique within this function. Go
// scopes labels per-function, so a plain counter needs no further
// qualification (unlike local variable names, which do — see scope.go).
func (g *funcGen) newLabel() string {
	g.labelSeq++
	return fmt.Sprintf("L%d", g.labelSeq)
}

func (g *funcGen) pushLoop(continueLabel, breakLabel string) {
	g.loopStack = append(g.loopStack, loopLabels{Continue: continueLabel, Break: breakLabel})
}

func (g *funcGen) popLoop() {
	g.loopStack = g.loopStack[:len(g.loopStack)-1]
}

func (g *funcGen) currentLoop() (loopLabels, bool) {
	if len(g.loopStack) == 0 {
		return loopLabels{}, false
	}
	return g.loopStack[len(g.loopStack)-1], true
}
