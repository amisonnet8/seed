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

	lit, ok := decl.Init.(*ast.ArrayLit)
	if !ok {
		return nil // no initializer, or explicit `null` — already all zero
	}
	return genArrayLitElements(g, ref, sizeOp, lit)
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

	if _, isNull := value.(*ast.NullLit); isNull {
		return nil // already all zero
	}
	lit, ok := value.(*ast.ArrayLit)
	if !ok {
		return fmt.Errorf("codegen: array reassignment value must be an array literal or null (sema bug)")
	}
	return genArrayLitElements(g, ref, lenOp, lit)
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
