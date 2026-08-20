package sema

import (
	"fmt"

	"seed/internal/ast"
)

// inferType computes the static type of e. null has no static type here;
// callers that accept null must special-case it before calling this (see
// checkAssignable).
func inferType(scope *scope, e ast.Expr) (ast.Type, error) {
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
	case *ast.CallExpr:
		return inferCallType(scope, v)
	case *ast.NullLit:
		return ast.Type{}, fmt.Errorf("line %d: null cannot be used here", v.Line)
	default:
		return ast.Type{}, fmt.Errorf("line %d: unsupported expression", ast.ExprLine(e))
	}
}

// inferCallType type-checks a call's arguments and returns its result
// type. Only print and isnull are recognized so far (seed_spec.md §9);
// everything else is "not supported yet" rather than "undefined", since
// the full builtin set lands in a later development step.
func inferCallType(scope *scope, call *ast.CallExpr) (ast.Type, error) {
	switch call.Callee {
	case "print":
		if len(call.Args) != 1 {
			return ast.Type{}, fmt.Errorf("line %d: print expects exactly 1 argument", call.Line)
		}
		argType, err := inferType(scope, call.Args[0])
		if err != nil {
			return ast.Type{}, err
		}
		if argType != (ast.Type{Name: "String"}) {
			return ast.Type{}, fmt.Errorf("line %d: print expects a String argument, got %s", call.Line, argType.Name)
		}
		return ast.Type{}, nil

	case "isnull":
		if len(call.Args) != 1 {
			return ast.Type{}, fmt.Errorf("line %d: isnull expects exactly 1 argument", call.Line)
		}
		ident, ok := call.Args[0].(*ast.Ident)
		if !ok {
			return ast.Type{}, fmt.Errorf("line %d: isnull expects a variable", call.Line)
		}
		if _, ok := scope.lookup(ident.Name); !ok {
			return ast.Type{}, fmt.Errorf("line %d: undefined variable %q", ident.Line, ident.Name)
		}
		return ast.Type{Name: "Bool"}, nil

	default:
		return ast.Type{}, fmt.Errorf("line %d: unsupported function call %q", call.Line, call.Callee)
	}
}
