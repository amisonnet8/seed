package sema_test

import (
	"strings"
	"testing"

	"github.com/amisonnet8/seed/internal/parser"
	"github.com/amisonnet8/seed/internal/sema"
)

func check(t *testing.T, src string) error {
	t.Helper()
	f, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return sema.Check(f)
}

func TestValidProgram(t *testing.T) {
	src := `
String greeting = "hi"

func Int main(String[] args) {
    print(greeting)
    String local = "local"
    local = null
    Bool isSet = isnull(local)
    return 0
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDuplicateDeclarationInSameScope(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int x = 1
    Int x = 2
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "already declared") {
		t.Fatalf("expected a duplicate-declaration error, got %v", err)
	}
}

func TestLocalShadowsGlobal(t *testing.T) {
	src := `
String greeting = "global"

func Int main(String[] args) {
    String greeting = "local"
    return 0
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("shadowing a global should be allowed, got %v", err)
	}
}

func TestAssignTypeMismatch(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int x = "hello"
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "cannot use String as Int") {
		t.Fatalf("expected a type-mismatch error, got %v", err)
	}
}

func TestUndefinedVariable(t *testing.T) {
	src := `
func Int main(String[] args) {
    y = 5
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "undefined variable") {
		t.Fatalf("expected an undefined-variable error, got %v", err)
	}
}

func TestPrintRejectsNonString(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int x = 5
    print(x)
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "print expects a String argument") {
		t.Fatalf("expected a print-argument-type error, got %v", err)
	}
}

func TestAssignNullToAnyType(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int x = null
    x = null
    return 0
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("assigning null should always be allowed, got %v", err)
	}
}

func TestOperatorsValidProgram(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int a = 1 + 2 * 3
    Int b = (1 + 2) * 3
    a += 1
    a++
    a--
    String s = "a" + "b"
    s += "c"
    Bool cmp = a == b && a < b || !(a >= b)
    Float f = -1.5
    return 0
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPlusRejectsMixedTypes(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int x = 1
    String s = "a"
    Int y = x + s
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "Int + String is not allowed") {
		t.Fatalf("expected a mixed-type + error, got %v", err)
	}
}

func TestMinusRejectsString(t *testing.T) {
	src := `
func Int main(String[] args) {
    String a = "x"
    String b = "y"
    String c = a - b
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "is not supported for String") {
		t.Fatalf("expected - to reject String, got %v", err)
	}
}

func TestModuloRequiresInt(t *testing.T) {
	src := `
func Int main(String[] args) {
    Float x = 1.5
    Float y = 2.0
    Float z = x % y
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "requires Int operands") {
		t.Fatalf("expected %% to require Int operands, got %v", err)
	}
}

func TestComparisonRejectsMismatchedTypes(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int x = 1
    Float y = 1.0
    Bool b = x < y
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "cannot compare Int with Float") {
		t.Fatalf("expected a comparison type-mismatch error, got %v", err)
	}
}

func TestLogicalRequiresBoolOperands(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int x = 1
    Bool b = x && true
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "logical operators require Bool operands") {
		t.Fatalf("expected && to require Bool operands, got %v", err)
	}
}

func TestUnaryMinusRejectsNonNumeric(t *testing.T) {
	src := `
func Int main(String[] args) {
    String s = "x"
    String t = -s
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "unary - expects Int or Float") {
		t.Fatalf("expected unary - to reject String, got %v", err)
	}
}

func TestUnaryNotRejectsNonBool(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int x = 1
    Bool b = !x
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "unary ! expects Bool") {
		t.Fatalf("expected unary ! to reject Int, got %v", err)
	}
}

func TestIncDecRequiresNumeric(t *testing.T) {
	src := `
func Int main(String[] args) {
    String s = "x"
    s++
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "expects an Int or Float variable") {
		t.Fatalf("expected ++ to reject String, got %v", err)
	}
}

func TestCompoundAssignRejectsNull(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int x = 1
    x += null
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "null cannot be used here") {
		t.Fatalf("expected += to reject null, got %v", err)
	}
}

func TestGlobalInitializerCannotReferenceAnotherVariable(t *testing.T) {
	src := `
Int a = 1
Int b = a

func Int main(String[] args) {
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "undefined variable") {
		t.Fatalf("expected global initializers to reject variable references, got %v", err)
	}
}

func TestControlFlowValidProgram(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int i = 0
    while i < 10 {
        if i == 3 {
            i += 2
            continue
        }
        if i == 8 {
            break
        }
        i++
    }
    if i == 3 {
        return 1
    } elif i == 8 {
        return 2
    } else {
        return 3
    }
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIfConditionMustBeBool(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int x = 1
    if x {
        return 0
    }
    return 1
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "condition must be Bool") {
		t.Fatalf("expected an if-condition type error, got %v", err)
	}
}

func TestWhileConditionMustBeBool(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int x = 1
    while x {
        return 0
    }
    return 1
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "condition must be Bool") {
		t.Fatalf("expected a while-condition type error, got %v", err)
	}
}

func TestBreakOutsideLoop(t *testing.T) {
	src := `
func Int main(String[] args) {
    break
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "break outside of a loop") {
		t.Fatalf("expected a break-outside-loop error, got %v", err)
	}
}

func TestContinueOutsideLoop(t *testing.T) {
	src := `
func Int main(String[] args) {
    if true {
        continue
    }
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "continue outside of a loop") {
		t.Fatalf("expected a continue-outside-loop error (if is not a loop), got %v", err)
	}
}

func TestMainMustReturnOnEveryPath(t *testing.T) {
	src := `
func Int main(String[] args) {
    if true {
        return 1
    }
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "does not return a value on every path") {
		t.Fatalf("expected a non-exhaustive-return error (if with no else), got %v", err)
	}
}

func TestMainReturnsOnEveryPathViaExhaustiveIfElse(t *testing.T) {
	src := `
func Int main(String[] args) {
    Bool cond = true
    if cond {
        return 1
    } elif !cond {
        return 2
    } else {
        return 3
    }
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWhileLoopNeverCountsAsExhaustiveReturn(t *testing.T) {
	// The condition might be false on the first check, so a return only
	// inside the loop body never guarantees a return.
	src := `
func Int main(String[] args) {
    while true {
        return 1
    }
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "does not return a value on every path") {
		t.Fatalf("expected a non-exhaustive-return error (while is never exhaustive), got %v", err)
	}
}

func TestShadowingInsideIfBodyAllowed(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int x = 1
    if true {
        Int x = 2
    }
    return x
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("shadowing inside a nested block should be allowed, got %v", err)
	}
}

func TestDuplicateDeclarationInsideIfBodyStillRejected(t *testing.T) {
	src := `
func Int main(String[] args) {
    if true {
        Int x = 1
        Int x = 2
    }
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "already declared") {
		t.Fatalf("expected a duplicate-declaration error within the if body, got %v", err)
	}
}

func TestArraysValidProgram(t *testing.T) {
	src := `
Int[3] globalCounts = {1, 2, 3}

func Int main(String[] args) {
    Int size = 5
    Int[size] a
    Int[3] b = {1, 2, 3}
    b[0] = 10
    b = {4, 5, 6}
    b = null

    globalCounts[1] = 99

    for x in a {
        Bool isZero = x == 0
    }

    Int i = 0
    while i < 3 {
        Int v = b[i]
        i++
    }

    return 0
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestArraySizeMustBeInt(t *testing.T) {
	src := `
func Int main(String[] args) {
    String s = "5"
    Int[s] a
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "array size must be Int") {
		t.Fatalf("expected an array-size type error, got %v", err)
	}
}

func TestArrayLiteralElementTypeMismatch(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int[3] a = {1, "two", 3}
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "cannot use String as Int") {
		t.Fatalf("expected an array-literal element type error, got %v", err)
	}
}

func TestArrayLiteralElementCountNeedNotMatchDeclaredSize(t *testing.T) {
	// seed_spec.md §4's truncate/pad rule: a literal shorter or longer
	// than the declared size is fine at the type-check level.
	src := `
func Int main(String[] args) {
    Int[5] a = {1, 2}
    Int[1] b = {1, 2, 3}
    return 0
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestArrayIndexMustBeInt(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int[3] a = {1, 2, 3}
    Int x = a["0"]
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "array index must be Int") {
		t.Fatalf("expected an array-index type error, got %v", err)
	}
}

func TestIndexingNonArrayRejected(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int x = 5
    Int y = x[0]
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "is not an array") {
		t.Fatalf("expected a not-an-array error, got %v", err)
	}
}

func TestIsnullRejectsArray(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int[3] a
    Bool b = isnull(a)
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "isnull does not support arrays") {
		t.Fatalf("expected isnull to reject an array, got %v", err)
	}
}

func TestArrayOperatorsRejected(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int[3] a = {1, 2, 3}
    Int[3] b = {4, 5, 6}
    Bool eq = a == b
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "operators are not supported for arrays") {
		t.Fatalf("expected == to reject arrays, got %v", err)
	}
}

func TestWholeArrayReassignmentFromAnotherArrayAllowed(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int[3] a = {1, 2, 3}
    Int[3] b = {4, 5, 6}
    a = b
    return 0
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestArrayValueTypeMismatchRejected(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int[3] a = {1, 2, 3}
    String[3] b = {"x", "y", "z"}
    a = b
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "cannot use String[] as Int[]") {
		t.Fatalf("expected a type-mismatch error, got %v", err)
	}
}

func TestForInRequiresArray(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int x = 5
    for v in x {
        return 0
    }
    return 1
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "is not an array") {
		t.Fatalf("expected for-in to require an array, got %v", err)
	}
}

func TestForInLoopVarOutOfScopeAfterLoop(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int[3] a = {1, 2, 3}
    for x in a {
        x++
    }
    x++
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "undefined variable") {
		t.Fatalf("expected the for-in variable to be out of scope after the loop, got %v", err)
	}
}

func TestForInBreakContinueAllowed(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int[3] a = {1, 2, 3}
    for x in a {
        if x == 1 {
            continue
        }
        if x == 2 {
            break
        }
    }
    return 0
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestForInNeverCountsAsExhaustiveReturn(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int[3] a = {1, 2, 3}
    for x in a {
        return 1
    }
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "does not return a value on every path") {
		t.Fatalf("expected for-in to never count as exhaustive (array could be empty), got %v", err)
	}
}

func TestGlobalArraySizeMustBeLiteral(t *testing.T) {
	src := `
Int n = 3
Int[n] globals

func Int main(String[] args) {
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "global array's size must be an Int literal") {
		t.Fatalf("expected a global array size restriction error, got %v", err)
	}
}

func TestFunctionsValidProgram(t *testing.T) {
	src := `
func String concat(String s1, String s2) {
    return s1 + s2
}

func greet(String name) {
    print(concat("Hello, ", name))
}

func Int[] sample(Int[] input) {
    Int[3] result = {1, 2, 3}
    return result
}

func Int fact(Int n) {
    if n <= 1 {
        return 1
    }
    return n * fact(n - 1)
}

func Int main(String[] args) {
    greet("Seed")
    Int[2] dummy = {0, 0}
    Int[5] result
    result = sample(dummy)
    Int f = fact(5)
    return 0
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestForwardFunctionReferenceAllowed(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int r = helper(5)
    return 0
}

func Int helper(Int x) {
    return x * 2
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("forward references between functions should be allowed, got %v", err)
	}
}

func TestMainCannotBeCalledDirectly(t *testing.T) {
	src := `
func Int main(String[] args) {
    main(args)
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "main cannot be called directly") {
		t.Fatalf("expected main to be uncallable, got %v", err)
	}
}

func TestDuplicateFunctionRejected(t *testing.T) {
	src := `
func Int helper() {
    return 1
}
func Int helper() {
    return 2
}
func Int main(String[] args) {
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), `duplicate function "helper"`) {
		t.Fatalf("expected a duplicate-function error, got %v", err)
	}
}

func TestReservedBuiltinNameRejectedAsFunctionName(t *testing.T) {
	src := `
func Int print() {
    return 0
}
func Int main(String[] args) {
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "reserved builtin name") {
		t.Fatalf("expected a reserved-name error, got %v", err)
	}
}

func TestSeedMainInternalNameRejected(t *testing.T) {
	src := `
func Int seed_main() {
    return 0
}
func Int main(String[] args) {
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "reserved for Seed's internal use") {
		t.Fatalf("expected seed_main to be rejected, got %v", err)
	}
}

func TestDuplicateParameterNameRejected(t *testing.T) {
	src := `
func Int add(Int a, Int a) {
    return a
}
func Int main(String[] args) {
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), `duplicate parameter "a"`) {
		t.Fatalf("expected a duplicate-parameter error, got %v", err)
	}
}

func TestCallArgumentCountMismatch(t *testing.T) {
	src := `
func Int add(Int a, Int b) {
    return a + b
}
func Int main(String[] args) {
    Int x = add(1)
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "expects 2 argument(s), got 1") {
		t.Fatalf("expected an argument-count error, got %v", err)
	}
}

func TestCallArgumentTypeMismatch(t *testing.T) {
	src := `
func Int add(Int a, Int b) {
    return a + b
}
func Int main(String[] args) {
    Int x = add(1, "two")
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "cannot use String as Int") {
		t.Fatalf("expected an argument-type error, got %v", err)
	}
}

func TestArrayArgumentMustBeVariable(t *testing.T) {
	src := `
func Int sum(Int[] a) {
    return 0
}
func Int main(String[] args) {
    Int x = sum({1, 2, 3})
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "must be an array variable") {
		t.Fatalf("expected array arguments to require a plain variable, got %v", err)
	}
}

func TestUndefinedFunctionCallRejected(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int x = doesNotExist(1)
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), `undefined function "doesNotExist"`) {
		t.Fatalf("expected an undefined-function error, got %v", err)
	}
}

func TestVoidCallUsedAsValueRejected(t *testing.T) {
	src := `
func logIt(String s) {
    print(s)
}
func Int main(String[] args) {
    Int x = logIt("hi")
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "has no return value") {
		t.Fatalf("expected a void-call-as-value error, got %v", err)
	}
}

func TestVoidCallAsStatementAllowed(t *testing.T) {
	src := `
func logIt(String s) {
    print(s)
}
func Int main(String[] args) {
    logIt("hi")
    return 0
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVoidFunctionCannotReturnValue(t *testing.T) {
	src := `
func logIt(String s) {
    return s
}
func Int main(String[] args) {
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "this function has no return value") {
		t.Fatalf("expected a void-function-returns-value error, got %v", err)
	}
}

func TestFunctionMustReturnOnEveryPath(t *testing.T) {
	src := `
func Int helper(Int x) {
    if x == 1 {
        return 1
    }
}
func Int main(String[] args) {
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "helper does not return a value on every path") {
		t.Fatalf("expected a non-exhaustive-return error, got %v", err)
	}
}

func TestReturnNullForScalarRejected(t *testing.T) {
	src := `
func Int helper() {
    return null
}
func Int main(String[] args) {
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "cannot return null for a scalar return type") {
		t.Fatalf("expected null to be rejected for a scalar return, got %v", err)
	}
}

func TestReturnNullForArrayAllowed(t *testing.T) {
	src := `
func Int[] helper() {
    return null
}
func Int main(String[] args) {
    return 0
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReturnArrayVariableAllowed(t *testing.T) {
	// seed_spec.md §7's own example returns a local array like this.
	src := `
func Int[] sample(Int[] input) {
    Int[3] result = {1, 2, 3}
    return result
}
func Int main(String[] args) {
    return 0
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReturnArrayLiteralAllowed(t *testing.T) {
	src := `
func Int[] sample() {
    return {1, 2, 3}
}
func Int main(String[] args) {
    return 0
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestArrayFunctionReturnAssignedWithTruncatePad(t *testing.T) {
	// seed_spec.md §7's own example.
	src := `
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
`
	if err := check(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFileIOValidProgram(t *testing.T) {
	// seed_spec.md §8's own example.
	src := `
func Int main(String[] args) {
    File f = open("xxx/xxx.txt", "r")
    while true {
        String line = read(f)
        if isnull(line) {
            break
        }
        print(line)
    }
    close(f)
    return 0
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenRequiresStringArgs(t *testing.T) {
	src := `
func Int main(String[] args) {
    File f = open(1, "r")
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "open's path argument must be String") {
		t.Fatalf("expected open's path arg to require String, got %v", err)
	}
}

func TestWriteRequiresFileAndString(t *testing.T) {
	src := `
func Int main(String[] args) {
    File f = open("x", "w")
    write(f, 5)
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "write's second argument must be String") {
		t.Fatalf("expected write's second arg to require String, got %v", err)
	}
}

func TestCloseRequiresFile(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int x = 1
    close(x)
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "close expects a File argument") {
		t.Fatalf("expected close to require File, got %v", err)
	}
}

func TestReadOnlyValidAsDirectAssignment(t *testing.T) {
	src := `
func Int main(String[] args) {
    File f = open("x", "r")
    print(read(f))
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "read() can only be used directly in an assignment") {
		t.Fatalf("expected read() to be rejected outside a direct assignment, got %v", err)
	}
}

func TestReadTargetMustBeString(t *testing.T) {
	src := `
func Int main(String[] args) {
    File f = open("x", "r")
    Int x = read(f)
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "read() returns String") {
		t.Fatalf("expected read() to require a String target, got %v", err)
	}
}

func TestIntAcceptsIntFloatStringBool(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int a = int(1)
    Int b = int(1.5)
    Int c = int("1")
    Int d = int(true)
    return 0
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIntRejectsArray(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int[3] a = {1, 2, 3}
    Int x = int(a)
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "int does not accept Int[]") {
		t.Fatalf("expected int() to reject an array, got %v", err)
	}
}

func TestFloatRejectsBool(t *testing.T) {
	src := `
func Int main(String[] args) {
    Bool b = true
    Float f = float(b)
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "float does not accept Bool") {
		t.Fatalf("expected float() to reject Bool, got %v", err)
	}
}

func TestStringRejectsString(t *testing.T) {
	src := `
func Int main(String[] args) {
    String s = string("x")
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "string does not accept String") {
		t.Fatalf("expected string() to reject String, got %v", err)
	}
}

func TestLenAcceptsStringAndArray(t *testing.T) {
	src := `
func Int main(String[] args) {
    Int[3] a = {1, 2, 3}
    Int n1 = len("hello")
    Int n2 = len(a)
    return 0
}
`
	if err := check(t, src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLenRejectsScalarNonString(t *testing.T) {
	src := `
func Int main(String[] args) {
    Bool b = true
    Int n = len(b)
    return 0
}
`
	err := check(t, src)
	if err == nil || !strings.Contains(err.Error(), "len does not accept Bool") {
		t.Fatalf("expected len() to reject Bool, got %v", err)
	}
}
