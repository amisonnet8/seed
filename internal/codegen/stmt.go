package codegen

import (
	"fmt"

	"github.com/amisonnet8/seed/internal/ast"
)

// genBlock compiles one `{ }` block's statements in a fresh funcCtx
// scope, so its declarations don't leak into the enclosing block and may
// shadow an outer variable (seed_spec.md §3). This is used uniformly for
// a function's own top-level body as well as every nested if/while body,
// even though every declaration still ends up as one flat Go variable in
// the enclosing function (see scope.go and codegen.go's package doc).
func genBlock(g *funcGen, stmts []ast.Stmt) error {
	g.ctx.push()
	defer g.ctx.pop()
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
	case *ast.CompoundAssignStmt:
		return genCompoundAssignStmt(g, st)
	case *ast.IncDecStmt:
		return genIncDecStmt(g, st)
	case *ast.ExprStmt:
		return genExprStmt(g, st)
	case *ast.ReturnStmt:
		return genReturnStmt(g, st)
	case *ast.IfStmt:
		return genIfStmt(g, st)
	case *ast.WhileStmt:
		return genWhileStmt(g, st)
	case *ast.ForInStmt:
		return genForInStmt(g, st)
	case *ast.BreakStmt:
		return genBreakStmt(g, st)
	case *ast.ContinueStmt:
		return genContinueStmt(g, st)
	default:
		return fmt.Errorf("codegen: unsupported statement %T", s)
	}
}

// genIfStmt compiles an if/elif/else chain to nested IF/ELSE/ENDIF blocks
// (see codegen.go's package doc): clauses[0]'s condition is computed and
// checked first; every later clause (and the final else, if any) is
// nested inside its ELSE body. This mirrors exactly how amivm itself
// desugars a flat IF/ELIF/ELSE/ENDIF into nested `ast.IfStmt.Else`
// chains, but building the nesting ourselves means each later clause's
// condition instructions can be emitted right where they belong — inside
// the ELSE that guards them — so a clause's condition is only ever
// computed once every earlier one has been checked false, giving the
// usual short-circuit "elif conditions run only if earlier ones were
// false" behavior for free (an `ELIF` token can't offer this on its own:
// its condition operand must already be a value by the time that line is
// reached, with nowhere to place multi-instruction condition computation
// in between the preceding clause's `}` and its own `else if`).
func genIfStmt(g *funcGen, stmt *ast.IfStmt) error {
	return genIfClauses(g, stmt.Clauses, stmt.Else)
}

func genIfClauses(g *funcGen, clauses []ast.IfClause, elseBody []ast.Stmt) error {
	if len(clauses) == 0 {
		return genBlock(g, elseBody)
	}
	cond, err := genValue(g, clauses[0].Cond)
	if err != nil {
		return err
	}
	g.emit("\tIF\t%s\n", cond)
	if err := genBlock(g, clauses[0].Body); err != nil {
		return err
	}
	rest := clauses[1:]
	if len(rest) > 0 || elseBody != nil {
		g.emit("\tELSE\n")
		if err := genIfClauses(g, rest, elseBody); err != nil {
			return err
		}
	}
	g.emit("\tENDIF\n")
	return nil
}

// genWhileStmt compiles a while loop as a real LOOP block: the condition
// is (re-)computed at the very top of the body on every pass, and
// emitBreakUnless BREAKs out the moment it's false.
//
//	LOOP
//		<cond instrs>; NOT notCond cond; IF notCond BREAK ENDIF
//		<body>
//	ENDLOOP
//
// `continue` inside the body is therefore already correct as a native
// CONTINUE — it jumps back to the top of the LOOP, exactly where the
// condition re-check lives — and `break` as a native BREAK; see
// genBreakStmt/genContinueStmt. ContinueLabel is left empty in the
// pushed loopInfo to select that native-CONTINUE path (contrast
// genForInStmt, which can't use it).
func genWhileStmt(g *funcGen, stmt *ast.WhileStmt) error {
	g.emit("\tLOOP\n")
	cond, err := genValue(g, stmt.Cond)
	if err != nil {
		return err
	}
	g.emitBreakUnless(cond)

	g.pushLoop(loopInfo{})
	err = genBlock(g, stmt.Body)
	g.popLoop()
	if err != nil {
		return err
	}

	g.emit("\tENDLOOP\n")
	return nil
}

// genForInStmt compiles `for x in a { ... }` (seed_spec.md §6) as an
// index-counted LOOP over len(a):
//
//	CALL lenOp : ?len a; SET idxOp 0
//	LOOP
//		LT cmp idxOp lenOp; NOT notCmp cmp; IF notCmp BREAK ENDIF
//		AGET x a idxOp; SET x_isset true
//		<body>
//		GOTO continue; LABEL continue; ADD idxOp idxOp 1
//	ENDLOOP
//
// Unlike genWhileStmt, `continue` cannot be a native CONTINUE: that would
// jump back to the top of the LOOP (the condition re-check) and skip the
// idxOp++ step below it, looping forever on the same element. So
// genContinueStmt instead GOTOs a dedicated label placed right before
// that increment (still valid AMIVM-IR/Go — LABEL/GOTO were kept
// alongside the new block instructions specifically for jumps structured
// control flow can't express directly), which the body's normal
// (non-break/continue) fallthrough also reaches; see loopInfo's doc.
//
// x's scope spans the loop var and the body together in a single funcCtx
// scope (matching sema's checkForInStmt), not genBlock's usual per-block
// scope, so it's pushed/popped by hand around an inline statement loop
// instead of calling genBlock.
func genForInStmt(g *funcGen, stmt *ast.ForInStmt) error {
	arrRef, ok := g.ctx.lookup(stmt.ArrayName)
	if !ok {
		return fmt.Errorf("line %d: undefined variable %q", stmt.Line, stmt.ArrayName)
	}
	elemType := ast.Type{Name: arrRef.Type.Name}
	elemIRType, err := seedTypeToIR(elemType)
	if err != nil {
		return err
	}

	lenOp := g.newTemp("^int")
	g.emit("\tCALL\t%s\t:\t?len\t%s\n", lenOp, arrRef.ValOp)
	idxOp := g.newTemp("^int")
	g.emit("\tSET\t%s\t0\n", idxOp)

	continueLabel := g.newLabel()

	g.emit("\tLOOP\n")
	cmp := g.newTemp("^bool")
	g.emit("\tLT\t%s\t%s\t%s\n", cmp, idxOp, lenOp)
	g.emitBreakUnless(cmp)

	g.ctx.push()
	loopVar, err := g.ctx.declare(stmt.VarName, elemType)
	if err != nil {
		g.ctx.pop()
		return fmt.Errorf("line %d: %s", stmt.Line, err)
	}
	g.declareVar(loopVar.ValOp, elemIRType)
	g.declareVar(loopVar.SetOp, "^bool")
	g.emit("\tAGET\t%s\t%s\t%s\n", loopVar.ValOp, arrRef.ValOp, idxOp)
	g.emit("\tSET\t%s\ttrue\n", loopVar.SetOp)

	g.pushLoop(loopInfo{ContinueLabel: continueLabel})
	for _, bodyStmt := range stmt.Body {
		if err := genStmt(g, bodyStmt); err != nil {
			g.popLoop()
			g.ctx.pop()
			return err
		}
	}
	g.popLoop()
	g.ctx.pop()

	// The body's normal (non-break/non-continue) fallthrough needs an
	// explicit GOTO here too: unlike an ordinary label, `go/types`
	// rejects a label with no GOTO anywhere targeting it ("declared and
	// not used"), and a for-in body with no `continue` in it would
	// otherwise leave continueLabel referenced by nothing at all.
	g.emit("\tGOTO\t#%s\n", continueLabel)
	g.emit("\tLABEL\t#%s\n", continueLabel)
	g.emit("\tADD\t%s\t%s\t1\n", idxOp, idxOp)
	g.emit("\tENDLOOP\n")
	return nil
}

// genBreakStmt always compiles to a native BREAK: it targets the nearest
// enclosing LOOP regardless of whether that loop is a while or a for-in
// (see loopInfo's doc).
func genBreakStmt(g *funcGen, stmt *ast.BreakStmt) error {
	if _, ok := g.currentLoop(); !ok {
		return fmt.Errorf("line %d: break outside of a loop", stmt.Line)
	}
	g.emit("\tBREAK\n")
	return nil
}

// genContinueStmt compiles to a native CONTINUE, unless the enclosing
// loop recorded a ContinueLabel to GOTO instead (a for-in loop's index
// increment; see loopInfo's doc and genForInStmt).
func genContinueStmt(g *funcGen, stmt *ast.ContinueStmt) error {
	loop, ok := g.currentLoop()
	if !ok {
		return fmt.Errorf("line %d: continue outside of a loop", stmt.Line)
	}
	if loop.ContinueLabel != "" {
		g.emit("\tGOTO\t#%s\n", loop.ContinueLabel)
		return nil
	}
	g.emit("\tCONTINUE\n")
	return nil
}

// genVarDecl declares ref's VAR pair at the top of the function (see
// codegen.go's package doc) and always emits the SET(s) that give it its
// initial value at its original position — even with no initializer,
// where that means resetting to null. That reset is what makes a
// VarDecl inside a loop body behave correctly on the second and later
// iterations: the hoisted VAR only zero-allocates once, at function
// entry, so without an explicit reset here a later iteration would see
// whatever the previous iteration left behind instead of a fresh null.
// Array declarations dispatch to genArrayVarDecl instead, which needs no
// such reset of its own: its SLMAKE already reassigns the variable to a
// fresh backing array on every run.
func genVarDecl(g *funcGen, decl *ast.VarDecl) error {
	if decl.Type.IsArray {
		ref, err := g.ctx.declare(decl.Name, decl.Type)
		if err != nil {
			return fmt.Errorf("line %d: %w", decl.Line, err)
		}
		return genArrayVarDecl(g, decl, ref)
	}

	irType, err := seedTypeToIR(decl.Type)
	if err != nil {
		return err
	}
	ref, err := g.ctx.declare(decl.Name, decl.Type)
	if err != nil {
		return fmt.Errorf("line %d: %w", decl.Line, err)
	}
	g.declareVar(ref.ValOp, irType)
	g.declareVar(ref.SetOp, "^bool")

	init := decl.Init
	if init == nil {
		init = &ast.NullLit{Line: decl.Line}
	}
	return genAssign(g, ref, init)
}

// genAssignStmt compiles `name = value` (scalar or whole-array) and
// `name[Index] = value` (a single array element), dispatching to the
// array-specific helpers in array.go as needed.
func genAssignStmt(g *funcGen, stmt *ast.AssignStmt) error {
	ref, ok := g.ctx.lookup(stmt.Name)
	if !ok {
		return fmt.Errorf("line %d: undefined variable %q", stmt.Line, stmt.Name)
	}
	if stmt.Index != nil {
		return genIndexAssign(g, ref, stmt.Index, stmt.Value)
	}
	if ref.Type.IsArray {
		return genArrayReassign(g, ref, stmt.Value)
	}
	return genAssign(g, ref, stmt.Value)
}

// genAssign emits the instruction(s) that store value into ref, keeping
// its isset flag in sync. Assigning `null` resets the underlying value
// to its base form too, so that a later plain read (which never
// re-checks isset) still observes the base value. `read(f)` is
// special-cased ahead of the general path: see genReadAssign — sema's
// checkReadCall guarantees this is the only shape read() can appear in,
// so both genVarDecl and genAssignStmt funnel through here to get it.
func genAssign(g *funcGen, ref varRef, value ast.Expr) error {
	if call, ok := value.(*ast.CallExpr); ok && call.Callee == "read" {
		return genReadAssign(g, ref, call)
	}
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

// genCompoundAssignStmt emits `name op= value` in place (SET's Go output
// allows the target on both sides, e.g. `x = x + y`), then marks it set.
func genCompoundAssignStmt(g *funcGen, stmt *ast.CompoundAssignStmt) error {
	ref, ok := g.ctx.lookup(stmt.Name)
	if !ok {
		return fmt.Errorf("line %d: undefined variable %q", stmt.Line, stmt.Name)
	}
	v, err := genValue(g, stmt.Value)
	if err != nil {
		return err
	}
	if stmt.Op == "+" && ref.Type.Name == "String" {
		g.emit("\tCONCAT\t%s\t%s\t%s\n", ref.ValOp, ref.ValOp, v)
	} else if stmt.Op == "+" {
		g.emit("\tADD\t%s\t%s\t%s\n", ref.ValOp, ref.ValOp, v)
	} else {
		g.emit("\t%s\t%s\t%s\t%s\n", arithInstr(stmt.Op), ref.ValOp, ref.ValOp, v)
	}
	g.emit("\tSET\t%s\ttrue\n", ref.SetOp)
	return nil
}

func genIncDecStmt(g *funcGen, stmt *ast.IncDecStmt) error {
	ref, ok := g.ctx.lookup(stmt.Name)
	if !ok {
		return fmt.Errorf("line %d: undefined variable %q", stmt.Line, stmt.Name)
	}
	instr := "ADD"
	if stmt.Op == "--" {
		instr = "SUB"
	}
	g.emit("\t%s\t%s\t%s\t1\n", instr, ref.ValOp, ref.ValOp)
	g.emit("\tSET\t%s\ttrue\n", ref.SetOp)
	return nil
}

// genExprStmt compiles a call expression used as a whole statement.
// print and isnull are special-cased (isnull is legal but pointless here
// — sema allows any value-producing call as a statement, its result
// simply discarded); every other name is a user function, compiled via
// the shared genCall with wantValue=false — which is also correct even
// when that function does return something, since omitting CALL's
// result operand (multi1) is valid AMIVM-IR/Go for discarding it.
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
	case "isnull", "open", "int", "float", "string", "len":
		// Legal but pointless as a bare statement (sema allows any
		// value-producing call as a statement, result discarded); still
		// must dispatch to each builtin's own codegen, not genCall's
		// user-function path.
		_, err := genCallValue(g, call)
		return err
	case "write":
		return genWriteStmt(g, call)
	case "close":
		return genCloseStmt(g, call)
	default:
		_, err := genCall(g, call, false)
		return err
	}
}

func genReturnStmt(g *funcGen, st *ast.ReturnStmt) error {
	if st.X == nil {
		g.emit("\tRET\n")
		return nil
	}
	v, err := genReturnValue(g, st.X)
	if err != nil {
		return err
	}
	g.emit("\tRET\t%s\n", v)
	return nil
}

// genReturnValue handles the two return-value shapes genValue doesn't
// (it rejects both, since neither has a context-free meaning on its
// own — see ast.ArrayLit's doc and genValue's NullLit case): `return
// null` for an array return type (sema only allows this for arrays —
// see sema's checkReturnStmt — so it always means "empty array" here,
// i.e. Go's nil slice), and `return {...}`, which needs g.retType (set
// by genFuncDecl) to know the element type since an array literal has
// none of its own. Every other expression — a variable, a call, an
// operator expression — already has a well-defined value through the
// ordinary genValue path.
func genReturnValue(g *funcGen, x ast.Expr) (string, error) {
	switch v := x.(type) {
	case *ast.NullLit:
		return "nil", nil
	case *ast.ArrayLit:
		return genReturnArrayLiteral(g, v)
	default:
		return genValue(g, x)
	}
}
