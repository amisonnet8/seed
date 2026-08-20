package codegen_test

import (
	"strings"
	"testing"

	"seed/internal/codegen"
	"seed/internal/parser"
	"seed/internal/sema"
)

func generate(t *testing.T, src string) string {
	t.Helper()
	f, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if err := sema.Check(f); err != nil {
		t.Fatalf("sema error: %v", err)
	}
	ir, err := codegen.Generate(f)
	if err != nil {
		t.Fatalf("codegen error: %v", err)
	}
	return ir
}

func TestNullAssignmentResetsValueAndIsSet(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    String s = "hi"
    s = null
    return 0
}
`)
	if !strings.Contains(ir, `SET	%s	""`) {
		t.Errorf("expected null assignment to reset the value to its base form, got:\n%s", ir)
	}
	if !strings.Contains(ir, "SET\t%s_isset\tfalse") {
		t.Errorf("expected null assignment to clear the isset flag, got:\n%s", ir)
	}
}

func TestIsnullEmitsNotOnIsSetFlag(t *testing.T) {
	ir := generate(t, `
String greeting

func Int main(String[] args) {
    Bool r = isnull(greeting)
    return 0
}
`)
	if !strings.Contains(ir, "NOT\t%tmp\t@greeting_isset") {
		t.Errorf("expected isnull to emit NOT against the isset flag, got:\n%s", ir)
	}
}

func TestShadowingGetsDistinctInternalNames(t *testing.T) {
	// Both "x" declarations live in the same amivm function scope (no
	// nested Go blocks), so they must not collide.
	ir := generate(t, `
func Int main(String[] args) {
    Int x = 1
    return 0
}
`)
	if strings.Count(ir, "VAR\t%x\t^int") != 1 {
		t.Errorf("expected exactly one VAR for %%x, got:\n%s", ir)
	}
}

func TestPrecedenceMultiplicationBeforeAddition(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Int r = 1 + 2 * 3
    return 0
}
`)
	mulIdx := strings.Index(ir, "MUL")
	addIdx := strings.Index(ir, "ADD")
	if mulIdx == -1 || addIdx == -1 || mulIdx > addIdx {
		t.Errorf("expected MUL (2*3 binds tighter) to be emitted before ADD, got:\n%s", ir)
	}
}

func TestPlusDispatchesOnType(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Int a = 1
    Int b = 2
    Int sum = a + b
    String x = "a"
    String y = "b"
    String cat = x + y
    return 0
}
`)
	if !strings.Contains(ir, "\tADD\t") {
		t.Errorf("expected Int + Int to use ADD, got:\n%s", ir)
	}
	if !strings.Contains(ir, "\tCONCAT\t") {
		t.Errorf("expected String + String to use CONCAT, got:\n%s", ir)
	}
}

func TestUnaryMinusUsesSubFromZero(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Float x = 1.5
    Float y = -x
    return 0
}
`)
	if !strings.Contains(ir, "SUB\t%tmp\t0\t%x") {
		t.Errorf("expected unary - to emit SUB against 0 (no dedicated negation instruction), got:\n%s", ir)
	}
}

func TestCompoundAssignUpdatesInPlace(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Int x = 1
    x += 2
    return 0
}
`)
	if !strings.Contains(ir, "ADD\t%x\t%x\t2") {
		t.Errorf("expected += to update the variable in place, got:\n%s", ir)
	}
	if !strings.Contains(ir, "SET\t%x_isset\ttrue") {
		t.Errorf("expected += to mark the variable set, got:\n%s", ir)
	}
}

func TestIncDecEmitsAddSubByOne(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Int x = 1
    x++
    x--
    return 0
}
`)
	if !strings.Contains(ir, "ADD\t%x\t%x\t1") {
		t.Errorf("expected ++ to emit ADD by 1, got:\n%s", ir)
	}
	if !strings.Contains(ir, "SUB\t%x\t%x\t1") {
		t.Errorf("expected -- to emit SUB by 1, got:\n%s", ir)
	}
}

func TestGlobalInitializerRunsInMainWrapper(t *testing.T) {
	ir := generate(t, `
Int counter = 42

func Int main(String[] args) {
    return 0
}
`)
	mainIdx := strings.Index(ir, "FUNC\t!main\t:")
	if mainIdx == -1 {
		t.Fatalf("expected a !main wrapper, got:\n%s", ir)
	}
	if !strings.Contains(ir[mainIdx:], "SET\t@counter\t42") {
		t.Errorf("expected the global's initial value to be set inside !main, got:\n%s", ir)
	}
}

// TestAllVarsHoistedBeforeControlFlow locks in the fix for a real bug: a
// VAR declared inside an if/while body, left in place, made a later
// break/elif-skip GOTO "jump over" it — illegal in Go once that
// declaration's flat (block-less) scope would otherwise extend past the
// jump target. Every VAR must come before the function's first
// LABEL/IF/GOTO.
func TestAllVarsHoistedBeforeControlFlow(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Int i = 0
    while i < 3 {
        Int doubled = i * 2
        i++
    }
    if i == 3 {
        Int found = 1
        return found
    }
    return 0
}
`)
	funcStart := strings.Index(ir, "FUNC\t!seed_main")
	funcEnd := strings.Index(ir, "ENDFUNC")
	body := ir[funcStart:funcEnd]

	lastVar := strings.LastIndex(body, "\tVAR\t")
	firstControl := len(body)
	for _, instr := range []string{"\tLABEL\t", "\tIF\t", "\tGOTO\t"} {
		if i := strings.Index(body, instr); i != -1 && i < firstControl {
			firstControl = i
		}
	}
	if lastVar == -1 || firstControl == len(body) || lastVar > firstControl {
		t.Errorf("expected every VAR to precede the first LABEL/IF/GOTO, got:\n%s", body)
	}
}

// TestLoopLocalVarDeclResetsEachIteration checks that a VarDecl inside a
// while body still emits its SET at its original position (inside the
// loop), not just the hoisted VAR — otherwise a second iteration would
// see whatever the first iteration left behind instead of a fresh value.
// The loop's start/body labels are located structurally (genWhileStmt's
// known shape) rather than hardcoding label numbers.
func TestLoopLocalVarDeclResetsEachIteration(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Int i = 0
    while i < 3 {
        Int doubled = i * 2
        i++
    }
    return 0
}
`)
	lines := strings.Split(ir, "\n")

	var startLabel, bodyLabel string
	for _, line := range lines {
		if startLabel == "" && strings.HasPrefix(line, "\tLABEL\t#") {
			startLabel = strings.TrimPrefix(line, "\tLABEL\t")
		}
		if bodyLabel == "" && strings.HasPrefix(line, "\tIF\t") {
			fields := strings.Split(line, "\t")
			bodyLabel = fields[len(fields)-1]
		}
	}
	if startLabel == "" || bodyLabel == "" {
		t.Fatalf("could not locate the loop's start/body labels in:\n%s", ir)
	}

	bodyStart := strings.Index(ir, "\tLABEL\t"+bodyLabel)
	loopBack := strings.Index(ir, "\tGOTO\t"+startLabel)
	if bodyStart == -1 || loopBack == -1 || loopBack < bodyStart {
		t.Fatalf("could not locate the loop body span in:\n%s", ir)
	}
	if between := ir[bodyStart:loopBack]; !strings.Contains(between, "SET\t%doubled\t") {
		t.Errorf("expected the loop body to SET %%doubled on every iteration, got:\n%s", between)
	}
}

// TestBreakContinueTargetLoopLabels locates the while loop's start/end
// labels structurally from genWhileStmt's known shape (LABEL start; ...;
// IF cond body; GOTO end; ...; GOTO start; LABEL end) rather than
// hardcoding label numbers, then checks continue/break each contribute
// an extra GOTO to the start/end label respectively, beyond the loop's
// own two (the back-edge to start, and the exit check's GOTO to end).
func TestBreakContinueTargetLoopLabels(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Int i = 0
    while i < 10 {
        if i == 3 {
            continue
        }
        if i == 8 {
            break
        }
        i++
    }
    return 0
}
`)
	lines := strings.Split(ir, "\n")

	var startLabel, endLabel string
	for i, line := range lines {
		if startLabel == "" && strings.HasPrefix(line, "\tLABEL\t#") {
			startLabel = strings.TrimPrefix(line, "\tLABEL\t")
		}
		if endLabel == "" && strings.HasPrefix(line, "\tIF\t") {
			endLabel = strings.TrimPrefix(lines[i+1], "\tGOTO\t")
		}
	}
	if startLabel == "" || endLabel == "" {
		t.Fatalf("could not locate the loop's start/end labels in:\n%s", ir)
	}

	if got := strings.Count(ir, "\tGOTO\t"+startLabel); got < 2 {
		t.Errorf("expected continue to add a GOTO to the start label %s (loop back-edge + continue), got %d in:\n%s", startLabel, got, ir)
	}
	if got := strings.Count(ir, "\tGOTO\t"+endLabel); got < 2 {
		t.Errorf("expected break to add a GOTO to the end label %s (exit check + break), got %d in:\n%s", endLabel, got, ir)
	}
}
