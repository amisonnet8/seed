package sema

import (
	"fmt"

	"seed/internal/ast"
)

// inferType computes the static type of e. null has no static type here;
// callers that accept null must special-case it before calling this (see
// checkAssignable).
func (c *checker) inferType(scope *scope, e ast.Expr) (ast.Type, error) {
	switch v := e.(type) {
	case *ast.StringLit:
		return ast.Type{Name: "String"}, nil
	case *ast.IntLit:
		return ast.Type{Name: "Int"}, nil
	case *ast.FloatLit:
		return ast.Type{Name: "Float"}, nil
	case *ast.BoolLit:
		return ast.Type{Name: "Bool"}, nil
	case *ast.Ident:
		typ, ok := scope.lookup(v.Name)
		if !ok {
			return ast.Type{}, fmt.Errorf("line %d: undefined variable %q", v.Line, v.Name)
		}
		return typ, nil
	case *ast.IndexExpr:
		return c.inferIndexType(scope, v)
	case *ast.CallExpr:
		typ, hasValue, err := c.inferCallType(scope, v)
		if err != nil {
			return ast.Type{}, err
		}
		if !hasValue {
			return ast.Type{}, fmt.Errorf("line %d: %s has no return value", v.Line, v.Callee)
		}
		return typ, nil
	case *ast.UnaryExpr:
		return c.inferUnaryType(scope, v)
	case *ast.BinaryExpr:
		return c.inferBinaryType(scope, v)
	case *ast.NullLit:
		return ast.Type{}, fmt.Errorf("line %d: null cannot be used here", v.Line)
	default:
		return ast.Type{}, fmt.Errorf("line %d: unsupported expression", ast.ExprLine(e))
	}
}

// inferIndexType checks `a[Index]` (seed_spec.md §4): a must be an
// array, and Index a non-null Int. The result is a's scalar element
// type.
func (c *checker) inferIndexType(scope *scope, idx *ast.IndexExpr) (ast.Type, error) {
	arrType, ok := scope.lookup(idx.Name)
	if !ok {
		return ast.Type{}, fmt.Errorf("line %d: undefined variable %q", idx.Line, idx.Name)
	}
	if !arrType.IsArray {
		return ast.Type{}, fmt.Errorf("line %d: %q is not an array", idx.Line, idx.Name)
	}
	if _, isNull := idx.Index.(*ast.NullLit); isNull {
		return ast.Type{}, fmt.Errorf("line %d: null cannot be used here", idx.Line)
	}
	indexType, err := c.inferType(scope, idx.Index)
	if err != nil {
		return ast.Type{}, err
	}
	if indexType != (ast.Type{Name: "Int"}) {
		return ast.Type{}, fmt.Errorf("line %d: array index must be Int, got %s", idx.Line, typeName(indexType))
	}
	return ast.Type{Name: arrType.Name}, nil
}

// operandType infers e's type for use as an operator's operand, rejecting
// `null` (no operator accepts it) and arrays (seed_spec.md §5 defines no
// array operators).
func (c *checker) operandType(scope *scope, e ast.Expr) (ast.Type, error) {
	if _, ok := e.(*ast.NullLit); ok {
		return ast.Type{}, fmt.Errorf("line %d: null cannot be used here", ast.ExprLine(e))
	}
	t, err := c.inferType(scope, e)
	if err != nil {
		return ast.Type{}, err
	}
	if t.IsArray {
		return ast.Type{}, fmt.Errorf("line %d: operators are not supported for arrays", ast.ExprLine(e))
	}
	return t, nil
}

// inferUnaryType checks a prefix unary expression and records its result
// type on the node for codegen (see ast.UnaryExpr.ResultType).
func (c *checker) inferUnaryType(scope *scope, u *ast.UnaryExpr) (ast.Type, error) {
	xt, err := c.operandType(scope, u.X)
	if err != nil {
		return ast.Type{}, err
	}
	switch u.Op {
	case "!":
		if xt != (ast.Type{Name: "Bool"}) {
			return ast.Type{}, fmt.Errorf("line %d: unary ! expects Bool, got %s", u.Line, xt.Name)
		}
		u.ResultType = ast.Type{Name: "Bool"}
	case "-":
		if xt.Name != "Int" && xt.Name != "Float" {
			return ast.Type{}, fmt.Errorf("line %d: unary - expects Int or Float, got %s", u.Line, xt.Name)
		}
		u.ResultType = xt
	default:
		return ast.Type{}, fmt.Errorf("sema: unknown unary operator %q", u.Op)
	}
	return u.ResultType, nil
}

// inferBinaryType checks a binary expression and records its result type
// on the node for codegen (see ast.BinaryExpr.ResultType).
func (c *checker) inferBinaryType(scope *scope, b *ast.BinaryExpr) (ast.Type, error) {
	xt, err := c.operandType(scope, b.X)
	if err != nil {
		return ast.Type{}, err
	}
	yt, err := c.operandType(scope, b.Y)
	if err != nil {
		return ast.Type{}, err
	}

	var result ast.Type
	switch b.Op {
	case "+", "-", "*", "/", "%":
		result, err = arithOpType(b.Op, xt, yt)
	case "<", "<=", ">", ">=":
		result, err = orderedCompareType(xt, yt)
	case "==", "!=":
		result, err = equalityType(xt, yt)
	case "&&", "||":
		result, err = logicalType(xt, yt)
	default:
		err = fmt.Errorf("sema: unknown binary operator %q", b.Op)
	}
	if err != nil {
		return ast.Type{}, fmt.Errorf("line %d: %s", b.Line, err)
	}
	b.ResultType = result
	return result, nil
}

// arithOpType is the shared type rule for +/-/*//% between two operand
// types, used both for binary expressions and for `op=` compound
// assignment (seed_spec.md §5's "+演算子の型ごとの意味" and the general
// arithmetic operator list). Seed has no implicit numeric conversions, so
// both operands must already match.
func arithOpType(op string, xt, yt ast.Type) (ast.Type, error) {
	switch op {
	case "+":
		if xt != yt {
			return ast.Type{}, fmt.Errorf("%s + %s is not allowed", xt.Name, yt.Name)
		}
		if xt.Name != "Int" && xt.Name != "Float" && xt.Name != "String" {
			return ast.Type{}, fmt.Errorf("+ is not supported for %s", xt.Name)
		}
		return xt, nil
	case "-", "*", "/":
		if xt != yt {
			return ast.Type{}, fmt.Errorf("%s %s %s is not allowed", xt.Name, op, yt.Name)
		}
		if xt.Name != "Int" && xt.Name != "Float" {
			return ast.Type{}, fmt.Errorf("%s is not supported for %s", op, xt.Name)
		}
		return xt, nil
	case "%":
		if xt.Name != "Int" || yt.Name != "Int" {
			return ast.Type{}, fmt.Errorf("%% requires Int operands, got %s %% %s", xt.Name, yt.Name)
		}
		return ast.Type{Name: "Int"}, nil
	default:
		return ast.Type{}, fmt.Errorf("sema: unknown arithmetic operator %q", op)
	}
}

func orderedCompareType(xt, yt ast.Type) (ast.Type, error) {
	if xt != yt {
		return ast.Type{}, fmt.Errorf("cannot compare %s with %s", xt.Name, yt.Name)
	}
	if xt.Name != "Int" && xt.Name != "Float" && xt.Name != "String" {
		return ast.Type{}, fmt.Errorf("%s is not ordered", xt.Name)
	}
	return ast.Type{Name: "Bool"}, nil
}

func equalityType(xt, yt ast.Type) (ast.Type, error) {
	if xt != yt {
		return ast.Type{}, fmt.Errorf("cannot compare %s with %s", xt.Name, yt.Name)
	}
	return ast.Type{Name: "Bool"}, nil
}

func logicalType(xt, yt ast.Type) (ast.Type, error) {
	boolType := ast.Type{Name: "Bool"}
	if xt != boolType || yt != boolType {
		return ast.Type{}, fmt.Errorf("logical operators require Bool operands, got %s and %s", xt.Name, yt.Name)
	}
	return boolType, nil
}

// inferCallType type-checks a call's arguments and returns its result
// type. ok is false when the callee has no return value (print, or a
// user function declared without one) — valid only in statement
// position; inferType rejects it as a value. print and isnull remain
// special-cased here (seed_spec.md §9); every other name is looked up in
// c.funcs, the whole-program signature table built once up front, so a
// call to a function declared later in the file still resolves.
func (c *checker) inferCallType(scope *scope, call *ast.CallExpr) (typ ast.Type, ok bool, err error) {
	switch call.Callee {
	case "print":
		if len(call.Args) != 1 {
			return ast.Type{}, false, fmt.Errorf("line %d: print expects exactly 1 argument", call.Line)
		}
		argType, err := c.inferType(scope, call.Args[0])
		if err != nil {
			return ast.Type{}, false, err
		}
		if argType != (ast.Type{Name: "String"}) {
			return ast.Type{}, false, fmt.Errorf("line %d: print expects a String argument, got %s", call.Line, typeName(argType))
		}
		return ast.Type{}, false, nil

	case "isnull":
		if len(call.Args) != 1 {
			return ast.Type{}, false, fmt.Errorf("line %d: isnull expects exactly 1 argument", call.Line)
		}
		ident, identOk := call.Args[0].(*ast.Ident)
		if !identOk {
			return ast.Type{}, false, fmt.Errorf("line %d: isnull expects a variable", call.Line)
		}
		typ, found := scope.lookup(ident.Name)
		if !found {
			return ast.Type{}, false, fmt.Errorf("line %d: undefined variable %q", ident.Line, ident.Name)
		}
		if typ.IsArray {
			return ast.Type{}, false, fmt.Errorf("line %d: isnull does not support arrays", call.Line)
		}
		return ast.Type{Name: "Bool"}, true, nil

	case "main":
		return ast.Type{}, false, fmt.Errorf("line %d: main cannot be called directly", call.Line)

	default:
		sig, exists := c.funcs[call.Callee]
		if !exists {
			return ast.Type{}, false, fmt.Errorf("line %d: undefined function %q", call.Line, call.Callee)
		}
		if len(call.Args) != len(sig.Params) {
			return ast.Type{}, false, fmt.Errorf("line %d: %s expects %d argument(s), got %d", call.Line, call.Callee, len(sig.Params), len(call.Args))
		}
		for i, arg := range call.Args {
			want := sig.Params[i]
			if want.IsArray {
				// Arrays are always passed by reference (seed_spec.md
				// §7), which only makes sense for an existing array
				// variable — not a literal or another call's result.
				ident, identOk := arg.(*ast.Ident)
				if !identOk {
					return ast.Type{}, false, fmt.Errorf("line %d: argument %d to %s must be an array variable", call.Line, i+1, call.Callee)
				}
				argType, found := scope.lookup(ident.Name)
				if !found {
					return ast.Type{}, false, fmt.Errorf("line %d: undefined variable %q", ident.Line, ident.Name)
				}
				if argType != want {
					return ast.Type{}, false, fmt.Errorf("line %d: argument %d to %s: cannot use %s as %s", call.Line, i+1, call.Callee, typeName(argType), typeName(want))
				}
				continue
			}
			if err := c.checkAssignable(scope, want, arg); err != nil {
				return ast.Type{}, false, err
			}
		}
		if sig.Return == nil {
			return ast.Type{}, false, nil
		}
		return *sig.Return, true, nil
	}
}
