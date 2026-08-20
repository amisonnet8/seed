package sema_test

import (
	"strings"
	"testing"

	"seed/internal/parser"
	"seed/internal/sema"
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
