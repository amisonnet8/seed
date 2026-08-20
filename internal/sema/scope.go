package sema

import "seed/internal/ast"

// scope is a chain of block scopes (seed_spec.md §3 "スコープ"): lookups
// walk outward through parents (so a local can shadow a global), but a
// duplicate declaration is only rejected within the same scope.
type scope struct {
	vars   map[string]ast.Type
	parent *scope
}

func newScope(parent *scope) *scope {
	return &scope{vars: map[string]ast.Type{}, parent: parent}
}

// declare registers name in this scope. It reports whether name was
// already declared in this exact scope (not an outer one).
func (s *scope) declare(name string, typ ast.Type) (ok bool) {
	if _, exists := s.vars[name]; exists {
		return false
	}
	s.vars[name] = typ
	return true
}

func (s *scope) lookup(name string) (ast.Type, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if t, ok := cur.vars[name]; ok {
			return t, true
		}
	}
	return ast.Type{}, false
}
