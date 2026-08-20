package codegen

import (
	"fmt"

	"seed/internal/ast"
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

// genIfStmt compiles an if/elif/else chain to a sequence of conditional
// jumps followed by the bodies themselves (see codegen.go's package doc).
// Each clause's condition is evaluated immediately before its own IF, so
// a taken jump skips every later condition's instructions entirely —
// giving the usual short-circuit "elif conditions run only if earlier
// ones were false" behavior for free.
//
//	<cond1 instrs>; IF cond1 body1
//	<cond2 instrs>; IF cond2 body2
//	...
//	GOTO else-or-end
//	LABEL body1; ...; GOTO end
//	LABEL body2; ...; GOTO end
//	LABEL else; ...; GOTO end   (only if there's an `else`)
//	LABEL end
func genIfStmt(g *funcGen, stmt *ast.IfStmt) error {
	endLabel := g.newLabel()
	bodyLabels := make([]string, len(stmt.Clauses))

	for i, clause := range stmt.Clauses {
		cond, err := genValue(g, clause.Cond)
		if err != nil {
			return err
		}
		bodyLabels[i] = g.newLabel()
		g.emit("\tIF\t%s\t#%s\n", cond, bodyLabels[i])
	}

	var elseLabel string
	if stmt.Else != nil {
		elseLabel = g.newLabel()
		g.emit("\tGOTO\t#%s\n", elseLabel)
	} else {
		g.emit("\tGOTO\t#%s\n", endLabel)
	}

	for i, clause := range stmt.Clauses {
		g.emit("\tLABEL\t#%s\n", bodyLabels[i])
		if err := genBlock(g, clause.Body); err != nil {
			return err
		}
		g.emit("\tGOTO\t#%s\n", endLabel)
	}

	if stmt.Else != nil {
		g.emit("\tLABEL\t#%s\n", elseLabel)
		if err := genBlock(g, stmt.Else); err != nil {
			return err
		}
		g.emit("\tGOTO\t#%s\n", endLabel)
	}

	g.emit("\tLABEL\t#%s\n", endLabel)
	return nil
}

// genWhileStmt compiles a while loop as: check the condition, jump into
// the body if true or out past the loop if false, and jump back to the
// check after the body runs.
//
//	LABEL start; <cond instrs>; IF cond body; GOTO end
//	LABEL body; ...; GOTO start
//	LABEL end
//
// `continue` targets start (re-check the condition) and `break` targets
// end; see genBreakStmt/genContinueStmt.
func genWhileStmt(g *funcGen, stmt *ast.WhileStmt) error {
	startLabel := g.newLabel()
	bodyLabel := g.newLabel()
	endLabel := g.newLabel()

	g.emit("\tLABEL\t#%s\n", startLabel)
	cond, err := genValue(g, stmt.Cond)
	if err != nil {
		return err
	}
	g.emit("\tIF\t%s\t#%s\n", cond, bodyLabel)
	g.emit("\tGOTO\t#%s\n", endLabel)
	g.emit("\tLABEL\t#%s\n", bodyLabel)

	g.pushLoop(startLabel, endLabel)
	err = genBlock(g, stmt.Body)
	g.popLoop()
	if err != nil {
		return err
	}

	g.emit("\tGOTO\t#%s\n", startLabel)
	g.emit("\tLABEL\t#%s\n", endLabel)
	return nil
}

// genForInStmt compiles `for x in a { ... }` (seed_spec.md §6) as an
// index-counted loop over len(a):
//
//	CALL lenOp : ?len a; SET idxOp 0
//	LABEL start; LT cmp idxOp lenOp; IF cmp body; GOTO end
//	LABEL body; AGET x a idxOp; SET x_isset true; ...; LABEL continue
//	ADD idxOp idxOp 1; GOTO start
//	LABEL end
//
// Unlike genWhileStmt, `continue` cannot target start directly: start
// only re-checks the condition, and skipping straight to it would skip
// the idxOp++ step, looping forever on the same element. So continue
// targets a dedicated label positioned right before that increment,
// which the body's normal (non-break/continue) fallthrough also reaches.
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

	startLabel := g.newLabel()
	bodyLabel := g.newLabel()
	continueLabel := g.newLabel()
	endLabel := g.newLabel()

	g.emit("\tLABEL\t#%s\n", startLabel)
	cmp := g.newTemp("^bool")
	g.emit("\tLT\t%s\t%s\t%s\n", cmp, idxOp, lenOp)
	g.emit("\tIF\t%s\t#%s\n", cmp, bodyLabel)
	g.emit("\tGOTO\t#%s\n", endLabel)
	g.emit("\tLABEL\t#%s\n", bodyLabel)

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

	g.pushLoop(continueLabel, endLabel)
	for _, bodyStmt := range stmt.Body {
		if err := genStmt(g, bodyStmt); err != nil {
			g.popLoop()
			g.ctx.pop()
			return err
		}
	}
	g.popLoop()
	g.ctx.pop()

	g.emit("\tGOTO\t#%s\n", continueLabel)
	g.emit("\tLABEL\t#%s\n", continueLabel)
	g.emit("\tADD\t%s\t%s\t1\n", idxOp, idxOp)
	g.emit("\tGOTO\t#%s\n", startLabel)
	g.emit("\tLABEL\t#%s\n", endLabel)
	return nil
}

func genBreakStmt(g *funcGen, stmt *ast.BreakStmt) error {
	loop, ok := g.currentLoop()
	if !ok {
		return fmt.Errorf("line %d: break outside of a loop", stmt.Line)
	}
	g.emit("\tGOTO\t#%s\n", loop.Break)
	return nil
}

func genContinueStmt(g *funcGen, stmt *ast.ContinueStmt) error {
	loop, ok := g.currentLoop()
	if !ok {
		return fmt.Errorf("line %d: continue outside of a loop", stmt.Line)
	}
	g.emit("\tGOTO\t#%s\n", loop.Continue)
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

// genAssign emits the SET(s) that store value into ref, keeping its isset
// flag in sync. Assigning `null` resets the underlying value to its base
// form too, so that a later plain read (which never re-checks isset)
// still observes the base value.
func genAssign(g *funcGen, ref varRef, value ast.Expr) error {
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
	case "isnull":
		_, err := genCallValue(g, call)
		return err
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
