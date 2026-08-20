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
