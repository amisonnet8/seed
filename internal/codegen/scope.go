package codegen

import (
	"fmt"

	"seed/internal/ast"
)

// varRef is a Seed variable resolved to its AMIVM-IR operands: the value
// itself, plus a companion bool tracking whether it has been assigned
// (seed_spec.md §0's null/base-value semantics — see codegen.go's package
// doc for why this needs a separate flag rather than just reading the
// Go zero value).
type varRef struct {
	Type  ast.Type
	ValOp string // e.g. "%x_2" or "@counter"
	SetOp string // e.g. "%x_2_isset" or "@counter_isset"
}

// funcCtx tracks the block-scope chain and name-mangling state for a
// single Seed function being compiled. AMIVM-IR functions have a flat Go
// variable namespace (control flow is GOTO-based, not nested Go blocks),
// so a Seed-level shadowing redeclaration must still get a fresh,
// function-wide-unique internal name.
type funcCtx struct {
	scopes  []map[string]varRef
	used    map[string]int
	globals map[string]varRef
}

func newFuncCtx(globals map[string]varRef) *funcCtx {
	c := &funcCtx{used: map[string]int{}, globals: globals}
	c.push()
	return c
}

func (c *funcCtx) push() {
	c.scopes = append(c.scopes, map[string]varRef{})
}

func (c *funcCtx) pop() {
	c.scopes = c.scopes[:len(c.scopes)-1]
}

// declare registers name in the current (innermost) scope and returns its
// resolved varRef. It fails only if name is already declared in that same
// scope; shadowing an outer scope is fine and gets its own internal name.
func (c *funcCtx) declare(name string, typ ast.Type) (varRef, error) {
	cur := c.scopes[len(c.scopes)-1]
	if _, exists := cur[name]; exists {
		return varRef{}, fmt.Errorf("%q is already declared in this scope", name)
	}
	internal := c.freshInternal(name)
	ref := varRef{Type: typ, ValOp: "%" + internal, SetOp: "%" + internal + "_isset"}
	cur[name] = ref
	return ref, nil
}

// freshInternal returns a function-wide-unique internal name derived from
// base, reserving it (and its "_isset" companion) so later declarations
// never collide with it.
func (c *funcCtx) freshInternal(base string) string {
	name := base
	if n, used := c.used[base]; used {
		name = fmt.Sprintf("%s_%d", base, n+1)
		c.used[base] = n + 1
	} else {
		c.used[base] = 1
	}
	c.used[name] = 1
	c.used[name+"_isset"] = 1
	return name
}

func (c *funcCtx) lookup(name string) (varRef, bool) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if ref, ok := c.scopes[i][name]; ok {
			return ref, true
		}
	}
	if ref, ok := c.globals[name]; ok {
		return ref, true
	}
	return varRef{}, false
}
