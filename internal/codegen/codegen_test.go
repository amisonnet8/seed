package codegen_test

import (
	"strings"
	"testing"

	"github.com/amisonnet8/seed/internal/codegen"
	"github.com/amisonnet8/seed/internal/parser"
	"github.com/amisonnet8/seed/internal/sema"
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

func TestArrayDeclarationUsesSLMAKE(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Int[5] a = {1, 2}
    return 0
}
`)
	if !strings.Contains(ir, "SLTYPE\t^Intslice\t^int") {
		t.Errorf("expected an ^Intslice SLTYPE declaration, got:\n%s", ir)
	}
	if !strings.Contains(ir, "SLMAKE\t%a\t^Intslice\t5") {
		t.Errorf("expected SLMAKE with the declared size, got:\n%s", ir)
	}
	if !strings.Contains(ir, "ASET\t%a\t0\t1") || !strings.Contains(ir, "ASET\t%a\t1\t2") {
		t.Errorf("expected ASET for each literal element, got:\n%s", ir)
	}
	// No isset companion for arrays (see array.go's doc).
	if strings.Contains(ir, "%a_isset") {
		t.Errorf("expected no isset companion for an array, got:\n%s", ir)
	}
}

func TestArrayLiteralElementGuardedByRuntimeBoundsCheck(t *testing.T) {
	// Each literal element is guarded by its own IF/GOTO against the
	// array's length, not just unconditionally ASET, since the declared
	// size might be a runtime variable (see array.go's genArrayLitElements).
	ir := generate(t, `
func Int main(String[] args) {
    Int[2] a = {1, 2, 3}
    return 0
}
`)
	if !strings.Contains(ir, "LT\t") {
		t.Errorf("expected a runtime bounds check (LT) for each literal element, got:\n%s", ir)
	}
	// The 3rd element (index 2) is still emitted — the guard, not a
	// compile-time-omitted instruction, is what makes it a no-op at
	// runtime when the array is too short.
	if !strings.Contains(ir, "ASET\t%a\t2\t3") {
		t.Errorf("expected ASET for the truncated element too (guarded, not omitted), got:\n%s", ir)
	}
}

func TestIndexAssignEmitsASET(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Int[3] a = {1, 2, 3}
    a[1] = 99
    return 0
}
`)
	if !strings.Contains(ir, "ASET\t%a\t1\t99") {
		t.Errorf("expected ASET for element assignment, got:\n%s", ir)
	}
}

func TestIndexValueEmitsAGET(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Int[3] a = {1, 2, 3}
    Int x = a[1]
    return 0
}
`)
	if !strings.Contains(ir, "AGET\t") || !strings.Contains(ir, "\t%a\t") {
		t.Errorf("expected AGET reading from %%a, got:\n%s", ir)
	}
}

func TestWholeArrayReassignmentRebuildsFromLength(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Int[3] a = {1, 2, 3}
    a = {9}
    return 0
}
`)
	idx := strings.Index(ir, "CALL\t")
	if idx == -1 || !strings.Contains(ir[idx:], "?len\t%a") {
		t.Errorf("expected reassignment to query len(%%a), got:\n%s", ir)
	}
	if strings.Count(ir, "SLMAKE\t%a\t^Intslice\t") != 2 {
		t.Errorf("expected two SLMAKEs (declaration + reassignment), got:\n%s", ir)
	}
}

func TestForInUsesLenAndAGETPerIteration(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Int[3] a = {1, 2, 3}
    for x in a {
        Int y = x
    }
    return 0
}
`)
	if !strings.Contains(ir, "?len\t%a") {
		t.Errorf("expected for-in to query len(%%a), got:\n%s", ir)
	}
	if !strings.Contains(ir, "AGET\t%x\t%a\t") {
		t.Errorf("expected for-in to AGET into the loop variable, got:\n%s", ir)
	}
}

// TestForInContinueSkipsToIncrement locks in a real bug fix: `continue`
// inside a for-in must not jump straight back to the condition check
// (that's correct for while, but for-in has an implicit index++ between
// iterations that continue must still run, or the loop never advances).
func TestForInContinueSkipsToIncrement(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Int[3] a = {1, 2, 3}
    for x in a {
        if x == 2 {
            continue
        }
    }
    return 0
}
`)
	lines := strings.Split(ir, "\n")

	// genForInStmt's shape: LABEL start; ...; IF cmp body; GOTO end;
	// LABEL body; ...; LABEL continueLabel; ADD idx idx 1; GOTO start.
	var startLabel, continueLabel string
	for i, line := range lines {
		if startLabel == "" && strings.HasPrefix(line, "\tLABEL\t#") {
			startLabel = strings.TrimPrefix(line, "\tLABEL\t")
		}
		if strings.HasPrefix(line, "\tADD\t") && strings.Contains(line, "\t1") && i > 0 {
			// the increment's own preceding LABEL is continueLabel
			for j := i - 1; j >= 0; j-- {
				if strings.HasPrefix(lines[j], "\tLABEL\t#") {
					continueLabel = strings.TrimPrefix(lines[j], "\tLABEL\t")
					break
				}
			}
		}
	}
	if startLabel == "" || continueLabel == "" {
		t.Fatalf("could not locate start/continue labels in:\n%s", ir)
	}
	if continueLabel == startLabel {
		t.Fatalf("continue label must not be the condition-check label itself, got:\n%s", ir)
	}
	if !strings.Contains(ir, "\tGOTO\t"+continueLabel) {
		t.Errorf("expected continue to GOTO the increment label %s, got:\n%s", continueLabel, ir)
	}
}

func TestUserFunctionCompilesToOwnFUNCBlock(t *testing.T) {
	ir := generate(t, `
func Int double(Int n) {
    return n * 2
}
func Int main(String[] args) {
    Int x = double(21)
    return 0
}
`)
	if !strings.Contains(ir, "FUNC\t!double\t^int\t:\t^int\n") {
		t.Errorf("expected a FUNC block for double, got:\n%s", ir)
	}
	if !strings.Contains(ir, "\tCALL\t%") || !strings.Contains(ir, "\t:\t!double\t") {
		t.Errorf("expected a CALL to !double capturing a result, got:\n%s", ir)
	}
}

func TestVoidFunctionHasNoReturnTypeInFUNCLine(t *testing.T) {
	ir := generate(t, `
func logIt(String s) {
    print(s)
}
func Int main(String[] args) {
    logIt("hi")
    return 0
}
`)
	if !strings.Contains(ir, "FUNC\t!logIt\t^string\t:\n") {
		t.Errorf("expected logIt's FUNC line to have no return type, got:\n%s", ir)
	}
	if !strings.Contains(ir, "\tCALL\t:\t!logIt\t") {
		t.Errorf("expected a void CALL to !logIt with no result operand, got:\n%s", ir)
	}
}

func TestScalarParameterGetsIsSetTrueAtEntry(t *testing.T) {
	ir := generate(t, `
func Bool isSet(Int x) {
    return isnull(x)
}
func Int main(String[] args) {
    Bool b = isSet(1)
    return 0
}
`)
	funcStart := strings.Index(ir, "FUNC\t!isSet")
	funcEnd := strings.Index(ir[funcStart:], "ENDFUNC")
	body := ir[funcStart : funcStart+funcEnd]
	if !strings.Contains(body, "SET\t%") || !strings.Contains(body, "_isset\ttrue") {
		t.Errorf("expected the parameter's isset flag to be set true at function entry, got:\n%s", body)
	}
	if !strings.Contains(body, "NOT\t") {
		t.Errorf("expected isnull(x) to compile to NOT, got:\n%s", body)
	}
}

func TestArrayParameterUsesArgOperandDirectlyNoCopy(t *testing.T) {
	// This is what makes an array argument "pass by reference"
	// (seed_spec.md §7): the parameter's ASET/AGET must operate on $1
	// itself, not a local copy.
	ir := generate(t, `
func mutate(Int[] a) {
    a[0] = 99
}
func Int main(String[] args) {
    Int[3] nums = {1, 2, 3}
    mutate(nums)
    return 0
}
`)
	funcStart := strings.Index(ir, "FUNC\t!mutate")
	funcEnd := strings.Index(ir[funcStart:], "ENDFUNC")
	body := ir[funcStart : funcStart+funcEnd]
	if !strings.Contains(body, "ASET\t$1\t0\t99") {
		t.Errorf("expected ASET directly on $1 (no local copy), got:\n%s", body)
	}
}

func TestArrayArgumentPassedAsIsToCall(t *testing.T) {
	ir := generate(t, `
func mutate(Int[] a) {
    a[0] = 99
}
func Int main(String[] args) {
    Int[3] nums = {1, 2, 3}
    mutate(nums)
    return 0
}
`)
	mainStart := strings.Index(ir, "FUNC\t!seed_main")
	mainEnd := strings.Index(ir[mainStart:], "ENDFUNC")
	body := ir[mainStart : mainStart+mainEnd]
	if !strings.Contains(body, "CALL\t:\t!mutate\t%nums\n") {
		t.Errorf("expected mutate to be called with %%nums's own operand, got:\n%s", body)
	}
}

func TestArrayReturnAssignmentCopiesWithRuntimeBoundsCheck(t *testing.T) {
	// seed_spec.md §7's own example: result = sample(someArray).
	ir := generate(t, `
func Int[] sample(Int[] input) {
    Int[3] result = {1, 2, 3}
    return result
}
func Int main(String[] args) {
    Int[2] someArray = {0, 0}
    Int[5] result
    result = sample(someArray)
    return 0
}
`)
	mainStart := strings.Index(ir, "FUNC\t!seed_main")
	mainEnd := strings.Index(ir[mainStart:], "ENDFUNC")
	body := ir[mainStart : mainStart+mainEnd]
	if !strings.Contains(body, "!sample") {
		t.Errorf("expected a call to !sample, got:\n%s", body)
	}
	if strings.Count(body, "?len\t") < 2 {
		t.Errorf("expected len() queried for both the target and the call's result, got:\n%s", body)
	}
	if !strings.Contains(body, "AND\t") {
		t.Errorf("expected the copy loop to bound on both lengths together, got:\n%s", body)
	}
}

func TestReturnArrayLiteralBuildsExactSizedSlice(t *testing.T) {
	ir := generate(t, `
func Int[] sample() {
    return {7, 8, 9}
}
func Int main(String[] args) {
    return 0
}
`)
	funcStart := strings.Index(ir, "FUNC\t!sample")
	funcEnd := strings.Index(ir[funcStart:], "ENDFUNC")
	body := ir[funcStart : funcStart+funcEnd]
	if !strings.Contains(body, "SLMAKE\t%tmp\t^Intslice\t3") {
		t.Errorf("expected a 3-element SLMAKE for the returned literal, got:\n%s", body)
	}
	if !strings.Contains(body, "RET\t%tmp") {
		t.Errorf("expected RET to return the freshly built slice, got:\n%s", body)
	}
}

func TestReturnNullForArrayEmitsNil(t *testing.T) {
	ir := generate(t, `
func Int[] empty() {
    return null
}
func Int main(String[] args) {
    return 0
}
`)
	if !strings.Contains(ir, "RET\tnil") {
		t.Errorf("expected `return null` for an array to compile to RET nil, got:\n%s", ir)
	}
}

func TestRecursiveCallCompiles(t *testing.T) {
	ir := generate(t, `
func Int fact(Int n) {
    if n <= 1 {
        return 1
    }
    return n * fact(n - 1)
}
func Int main(String[] args) {
    Int r = fact(5)
    return 0
}
`)
	if strings.Count(ir, "!fact") < 2 {
		t.Errorf("expected fact to reference itself recursively (its own FUNC plus the recursive CALL), got:\n%s", ir)
	}
}

func TestOpenCompilesToSeedrtCall(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    File f = open("x.txt", "r")
    return 0
}
`)
	if !strings.Contains(ir, "?seedrt.Open\t\"x.txt\"\t\"r\"") {
		t.Errorf("expected open() to CALL ?seedrt.Open, got:\n%s", ir)
	}
	if !strings.Contains(ir, "^*seedrt.File") {
		t.Errorf("expected File to compile to ^*seedrt.File, got:\n%s", ir)
	}
}

func TestReadCapturesValueAndIsSetInOneCall(t *testing.T) {
	// The core of read()'s EOF-is-null design: both results of
	// seedrt.Read land directly in the variable's own value/isset
	// operands via one multi-result CALL — see array.go... rather,
	// stmt.go's genReadAssign.
	ir := generate(t, `
func Int main(String[] args) {
    File f = open("x.txt", "r")
    String line = read(f)
    return 0
}
`)
	if !strings.Contains(ir, "CALL\t%line\t%line_isset\t:\t?seedrt.Read\t%f") {
		t.Errorf("expected read() to capture both value and isset from one CALL, got:\n%s", ir)
	}
}

func TestWriteAndCloseCompileToSeedrtCalls(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    File f = open("x.txt", "w")
    write(f, "hello")
    close(f)
    return 0
}
`)
	if !strings.Contains(ir, "CALL\t:\t?seedrt.Write\t%f\t\"hello\"") {
		t.Errorf("expected write() to CALL ?seedrt.Write, got:\n%s", ir)
	}
	if !strings.Contains(ir, "CALL\t:\t?seedrt.Close\t%f") {
		t.Errorf("expected close() to CALL ?seedrt.Close, got:\n%s", ir)
	}
}

func TestIntConversionDispatchesPerArgType(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Float f = 1.5
    Bool b = true
    String s = "1"
    Int a = int(f)
    Int c = int(b)
    Int d = int(s)
    return 0
}
`)
	if !strings.Contains(ir, "CALL\t%") || !strings.Contains(ir, "\t:\t?int\t%f") {
		t.Errorf("expected int(Float) to use a bare ?int conversion, got:\n%s", ir)
	}
	if !strings.Contains(ir, "?seedrt.BoolToInt\t%b") {
		t.Errorf("expected int(Bool) to CALL ?seedrt.BoolToInt, got:\n%s", ir)
	}
	if !strings.Contains(ir, "?seedrt.ParseInt\t%s") {
		t.Errorf("expected int(String) to CALL ?seedrt.ParseInt, got:\n%s", ir)
	}
}

func TestIntOnIntIsANoOp(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Int x = 5
    Int y = int(x)
    return 0
}
`)
	if strings.Contains(ir, "?int\t") || strings.Contains(ir, "?seedrt") {
		t.Errorf("expected int(Int) to skip any conversion CALL, got:\n%s", ir)
	}
}

func TestStringConversionUsesStrconv(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Int i = 1
    Float f = 1.5
    Bool b = true
    String a = string(i)
    String c = string(f)
    String d = string(b)
    return 0
}
`)
	if !strings.Contains(ir, "?strconv.Itoa\t%i") {
		t.Errorf("expected string(Int) to CALL ?strconv.Itoa, got:\n%s", ir)
	}
	if !strings.Contains(ir, "?strconv.FormatFloat\t%f\t'g'\t-1\t64") {
		t.Errorf("expected string(Float) to CALL ?strconv.FormatFloat, got:\n%s", ir)
	}
	if !strings.Contains(ir, "?strconv.FormatBool\t%b") {
		t.Errorf("expected string(Bool) to CALL ?strconv.FormatBool, got:\n%s", ir)
	}
}

func TestLenDispatchesStringVsArray(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    Int[3] arr = {1, 2, 3}
    String s = "hi"
    Int n1 = len(s)
    Int n2 = len(arr)
    return 0
}
`)
	if !strings.Contains(ir, "?utf8.RuneCountInString\t%s") {
		t.Errorf("expected len(String) to CALL ?utf8.RuneCountInString (rune count, not byte length), got:\n%s", ir)
	}
	if !strings.Contains(ir, "?len\t%arr") {
		t.Errorf("expected len(array) to CALL the builtin ?len, got:\n%s", ir)
	}
}

func TestStringLiteralEscapesRoundTripThroughQuote(t *testing.T) {
	ir := generate(t, `
func Int main(String[] args) {
    String s = "line1\nline2\ttabbed \"quoted\" back\\slash"
    return 0
}
`)
	// The lexer decodes escapes into the real byte values, and codegen
	// re-quotes via strconv.Quote when emitting IR: this must produce
	// a valid Go/AMIVM-IR string token, not the original source spelling.
	if !strings.Contains(ir, `SET	%s	"line1\nline2\ttabbed \"quoted\" back\\slash"`) {
		t.Errorf("expected decoded escapes to be re-quoted for IR output, got:\n%s", ir)
	}
}
