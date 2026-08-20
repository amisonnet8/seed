package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"seed/internal/ast"
)

// Arrays don't get an isset companion the way scalars do (see scope.go's
// varRef and codegen.go's package doc): a Seed array is always given a
// concrete size the moment its declaration runs (SLMAKE, below), so
// there's no meaningful "not yet assigned" state distinct from "every
// element is its base value" — unlike a scalar, which can meaningfully
// be unset. sema enforces this by rejecting isnull() on an array.

// genArrayVarDecl compiles a local array declaration (seed_spec.md §3:
// `Type[size] name` or `Type[size] name = {...}`). It always creates a
// fresh, all-zero slice via SLMAKE — which is also what makes this safe
// to re-run on a later loop iteration, unlike a scalar's VAR (see
// codegen.go's package doc): SLMAKE reassigns the variable to a brand
// new backing array every time it executes, rather than only zeroing
// once at function entry.
func genArrayVarDecl(g *funcGen, decl *ast.VarDecl, ref varRef) error {
	sizeOp, err := genValue(g, decl.Size)
	if err != nil {
		return err
	}
	sliceType := g.slices.use(decl.Type.Name)
	g.declareVar(ref.ValOp, sliceType)
	g.emit("\tSLMAKE\t%s\t%s\t%s\n", ref.ValOp, sliceType, sizeOp)

	return genArrayInitFrom(g, ref, sizeOp, decl.Init)
}

// genArrayInitFrom emits ref's value (already freshly SLMAKE'd to
// boundOp elements by the caller) from value, which sema guarantees is
// one of: absent/null (nothing further to do — already all zero),
// an array literal, a plain array variable, or a function call
// returning an array (see sema's checkArrayValue).
func genArrayInitFrom(g *funcGen, ref varRef, boundOp string, value ast.Expr) error {
	switch v := value.(type) {
	case nil:
		return nil
	case *ast.NullLit:
		return nil
	case *ast.ArrayLit:
		return genArrayLitElements(g, ref, boundOp, v)
	case *ast.Ident:
		srcRef, ok := g.ctx.lookup(v.Name)
		if !ok {
			return fmt.Errorf("line %d: undefined variable %q", v.Line, v.Name)
		}
		return genArrayCopyFromSource(g, ref, boundOp, srcRef.ValOp)
	case *ast.CallExpr:
		sourceOp, err := genCall(g, v, true)
		if err != nil {
			return err
		}
		return genArrayCopyFromSource(g, ref, boundOp, sourceOp)
	default:
		return fmt.Errorf("codegen: unsupported array value %T (sema bug)", value)
	}
}

// genGlobalArrayVarDecl is genArrayVarDecl's counterpart for a top-level
// declaration: the GVAR line is emitted into decls (top-level, before any
// FUNC), while the SLMAKE/ASETs that give it its initial value run inside
// g's body — the shared init funcGen whose body ends up inside the
// generated !main wrapper (see Generate's doc comment). sema restricts a
// global array's size to an Int literal, so it never needs a temp here.
func genGlobalArrayVarDecl(g *funcGen, decls *strings.Builder, decl *ast.VarDecl) (varRef, error) {
	sliceType := g.slices.use(decl.Type.Name)
	ref := varRef{Type: decl.Type, ValOp: "@" + decl.Name}
	fmt.Fprintf(decls, "GVAR\t%s\t%s\n", ref.ValOp, sliceType)

	sizeLit, ok := decl.Size.(*ast.IntLit)
	if !ok {
		return varRef{}, fmt.Errorf("codegen: a global array's size must be an Int literal (sema bug)")
	}
	sizeOp := strconv.FormatInt(sizeLit.Value, 10)
	g.emit("\tSLMAKE\t%s\t%s\t%s\n", ref.ValOp, sliceType, sizeOp)

	if decl.Init == nil {
		return ref, nil
	}
	if _, isNull := decl.Init.(*ast.NullLit); isNull {
		return ref, nil
	}
	lit, ok := decl.Init.(*ast.ArrayLit)
	if !ok {
		return varRef{}, fmt.Errorf("codegen: a global array must be initialized with an array literal (sema bug)")
	}
	if err := genArrayLitElements(g, ref, sizeOp, lit); err != nil {
		return varRef{}, err
	}
	return ref, nil
}

// genArrayLitElements sets each of lit's elements into ref at its index,
// implementing seed_spec.md §4's truncate/pad rule: an element beyond
// boundOp (ref's current length) is silently skipped (truncated), and any
// index at or beyond len(lit.Elems) is left untouched — already at its
// base value, because the caller always creates ref fresh via SLMAKE
// right before calling this.
//
// boundOp might be a runtime value (an array declared `Type[n]` for a
// variable n), so "does index i fit" can't always be decided at codegen
// time; each element is therefore guarded by its own runtime comparison
// rather than assuming the literal fits.
func genArrayLitElements(g *funcGen, ref varRef, boundOp string, lit *ast.ArrayLit) error {
	for i, elem := range lit.Elems {
		v, err := genValue(g, elem)
		if err != nil {
			return err
		}
		fits := g.newTemp("^bool")
		g.emit("\tLT\t%s\t%d\t%s\n", fits, i, boundOp)
		setLabel, skipLabel := g.newLabel(), g.newLabel()
		g.emit("\tIF\t%s\t#%s\n", fits, setLabel)
		g.emit("\tGOTO\t#%s\n", skipLabel)
		g.emit("\tLABEL\t#%s\n", setLabel)
		g.emit("\tASET\t%s\t%d\t%s\n", ref.ValOp, i, v)
		g.emit("\tLABEL\t#%s\n", skipLabel)
	}
	return nil
}

// genArrayReassign compiles whole-array reassignment (`name = {...}` or
// `name = null`): both replace ref with a fresh, all-zero slice of its
// current length (queried via len(), since unlike a declaration there's
// no size expression here) before applying the literal's elements — a
// plain ASET-in-place would leave stale values in any index beyond the
// new literal's length, violating the truncate/pad rule's padding half.
func genArrayReassign(g *funcGen, ref varRef, value ast.Expr) error {
	sliceType := g.slices.use(ref.Type.Name)
	lenOp := g.newTemp("^int")
	g.emit("\tCALL\t%s\t:\t?len\t%s\n", lenOp, ref.ValOp)
	g.emit("\tSLMAKE\t%s\t%s\t%s\n", ref.ValOp, sliceType, lenOp)

	return genArrayInitFrom(g, ref, lenOp, value)
}

// genArrayCopyFromSource compiles copying an array from another array's
// operand (a plain variable's, or a call's captured result) into ref —
// e.g. seed_spec.md §7's example: `result = sample(...)`. ref must
// already be freshly SLMAKE'd to targetLenOp elements (genArrayVarDecl/
// genArrayReassign both do this before calling genArrayInitFrom, which
// dispatches here). This copies min(targetLenOp, len(sourceOp)) elements,
// implementing the truncate/pad rule the same way genArrayLitElements
// does for a literal, but via a genuine runtime loop since the source's
// element count isn't known at codegen time the way a literal's is.
func genArrayCopyFromSource(g *funcGen, ref varRef, targetLenOp, sourceOp string) error {
	srcLenOp := g.newTemp("^int")
	g.emit("\tCALL\t%s\t:\t?len\t%s\n", srcLenOp, sourceOp)

	idxOp := g.newTemp("^int")
	g.emit("\tSET\t%s\t0\n", idxOp)
	startLabel, bodyLabel, endLabel := g.newLabel(), g.newLabel(), g.newLabel()

	g.emit("\tLABEL\t#%s\n", startLabel)
	underTarget := g.newTemp("^bool")
	g.emit("\tLT\t%s\t%s\t%s\n", underTarget, idxOp, targetLenOp)
	underSrc := g.newTemp("^bool")
	g.emit("\tLT\t%s\t%s\t%s\n", underSrc, idxOp, srcLenOp)
	cond := g.newTemp("^bool")
	g.emit("\tAND\t%s\t%s\t%s\n", cond, underTarget, underSrc)
	g.emit("\tIF\t%s\t#%s\n", cond, bodyLabel)
	g.emit("\tGOTO\t#%s\n", endLabel)
	g.emit("\tLABEL\t#%s\n", bodyLabel)

	elemIRType, err := seedTypeToIR(ast.Type{Name: ref.Type.Name})
	if err != nil {
		return err
	}
	elemTmp := g.newTemp(elemIRType)
	g.emit("\tAGET\t%s\t%s\t%s\n", elemTmp, sourceOp, idxOp)
	g.emit("\tASET\t%s\t%s\t%s\n", ref.ValOp, idxOp, elemTmp)
	g.emit("\tADD\t%s\t%s\t1\n", idxOp, idxOp)
	g.emit("\tGOTO\t#%s\n", startLabel)
	g.emit("\tLABEL\t#%s\n", endLabel)
	return nil
}

// genReturnArrayLiteral compiles `return {...}` for a function whose
// return type is an array (see stmt.go's genReturnValue). Unlike an
// assignment target, a return value has no pre-existing length to
// truncate/pad against, so it's simply built at exactly the literal's
// own size.
func genReturnArrayLiteral(g *funcGen, lit *ast.ArrayLit) (string, error) {
	if g.retType == nil || !g.retType.IsArray {
		return "", fmt.Errorf("codegen: array literal returned from a non-array-returning function (sema bug)")
	}
	sliceType := g.slices.use(g.retType.Name)
	sizeOp := strconv.Itoa(len(lit.Elems))
	tmp := g.newTemp(sliceType)
	g.emit("\tSLMAKE\t%s\t%s\t%s\n", tmp, sliceType, sizeOp)
	ref := varRef{Type: *g.retType, ValOp: tmp}
	if err := genArrayLitElements(g, ref, sizeOp, lit); err != nil {
		return "", err
	}
	return tmp, nil
}

// genIndexAssign compiles `name[Index] = value` (seed_spec.md §4). Unlike
// a scalar, an array element has no isset flag: assigning `null` simply
// stores the element type's base value.
func genIndexAssign(g *funcGen, ref varRef, indexExpr, value ast.Expr) error {
	indexOp, err := genValue(g, indexExpr)
	if err != nil {
		return err
	}
	elemType := ast.Type{Name: ref.Type.Name}
	if _, isNull := value.(*ast.NullLit); isNull {
		zero, err := zeroValueLiteral(elemType)
		if err != nil {
			return err
		}
		g.emit("\tASET\t%s\t%s\t%s\n", ref.ValOp, indexOp, zero)
		return nil
	}
	v, err := genValue(g, value)
	if err != nil {
		return err
	}
	g.emit("\tASET\t%s\t%s\t%s\n", ref.ValOp, indexOp, v)
	return nil
}

// genIndexValue compiles `a[i]` used as a value (seed_spec.md §4),
// reading the element into a fresh temp of the element's type via AGET.
func genIndexValue(g *funcGen, idx *ast.IndexExpr) (string, error) {
	ref, ok := g.ctx.lookup(idx.Name)
	if !ok {
		return "", fmt.Errorf("line %d: undefined variable %q", idx.Line, idx.Name)
	}
	indexOp, err := genValue(g, idx.Index)
	if err != nil {
		return "", err
	}
	elemIRType, err := seedTypeToIR(ast.Type{Name: ref.Type.Name})
	if err != nil {
		return "", err
	}
	tmp := g.newTemp(elemIRType)
	g.emit("\tAGET\t%s\t%s\t%s\n", tmp, ref.ValOp, indexOp)
	return tmp, nil
}
