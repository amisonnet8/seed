// Package parser builds an AST from Seed source code.
//
// The grammar implemented here is intentionally a subset of seed_spec.md:
// top-level func/var declarations (including fixed-size arrays), a block
// of statements limited to variable declarations, assignment (scalar,
// array element, whole-array, compound, ++/--), call expressions, return,
// if/elif/else, while, for-in, break/continue, plus a full
// operator-precedence expression grammar (§5), literals (including array
// literals), and variable/array-element references. Later development
// steps extend this grammar further (general functions) one feature at a
// time.
package parser

import (
	"fmt"
	"strconv"

	"seed/internal/ast"
	"seed/internal/lexer"
)

// Parse lexes and parses src into a *ast.File.
func Parse(src string) (*ast.File, error) {
	toks, err := lexer.Tokenize(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	return p.parseFile()
}

type parser struct {
	toks []lexer.Token
	pos  int
}

func (p *parser) cur() lexer.Token {
	return p.toks[p.pos]
}

func (p *parser) advance() lexer.Token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *parser) expect(kind lexer.Kind, what string) (lexer.Token, error) {
	if p.cur().Kind != kind {
		return lexer.Token{}, fmt.Errorf("line %d: expected %s, got %q", p.cur().Line, what, p.cur().Literal)
	}
	return p.advance(), nil
}

// skipNewlines consumes any number of (already-collapsed) Newline tokens.
func (p *parser) skipNewlines() {
	for p.cur().Kind == lexer.Newline {
		p.advance()
	}
}

var typeKeywords = map[lexer.Kind]string{
	lexer.KwTypeInt:    "Int",
	lexer.KwTypeFloat:  "Float",
	lexer.KwTypeString: "String",
	lexer.KwTypeBool:   "Bool",
	lexer.KwTypeFile:   "File",
}

func (p *parser) parseFile() (*ast.File, error) {
	f := &ast.File{}
	p.skipNewlines()
	for p.cur().Kind != lexer.EOF {
		switch {
		case p.cur().Kind == lexer.KwFunc:
			fn, err := p.parseFuncDecl()
			if err != nil {
				return nil, err
			}
			f.Funcs = append(f.Funcs, fn)
		case p.isTypeKeyword():
			decl, err := p.parseVarDecl()
			if err != nil {
				return nil, err
			}
			f.Globals = append(f.Globals, decl)
		default:
			return nil, fmt.Errorf("line %d: expected 'func' or a type at top level, got %q", p.cur().Line, p.cur().Literal)
		}
		p.skipNewlines()
	}
	return f, nil
}

func (p *parser) isTypeKeyword() bool {
	_, ok := typeKeywords[p.cur().Kind]
	return ok
}

func (p *parser) parseFuncDecl() (*ast.FuncDecl, error) {
	kw, err := p.expect(lexer.KwFunc, "'func'")
	if err != nil {
		return nil, err
	}
	fn := &ast.FuncDecl{Line: kw.Line}

	if p.cur().Kind != lexer.Ident {
		// A return type precedes the name: `func Int main(...)`.
		rt, _, err := p.parseType(false)
		if err != nil {
			return nil, err
		}
		fn.ReturnType = &rt
	}

	name, err := p.expect(lexer.Ident, "function name")
	if err != nil {
		return nil, err
	}
	fn.Name = name.Literal

	if _, err := p.expect(lexer.LParen, "'('"); err != nil {
		return nil, err
	}
	for p.cur().Kind != lexer.RParen {
		if len(fn.Params) > 0 {
			if _, err := p.expect(lexer.Comma, "','"); err != nil {
				return nil, err
			}
		}
		param, err := p.parseParam()
		if err != nil {
			return nil, err
		}
		fn.Params = append(fn.Params, param)
	}
	if _, err := p.expect(lexer.RParen, "')'"); err != nil {
		return nil, err
	}

	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	fn.Body = body
	return fn, nil
}

func (p *parser) parseParam() (ast.Param, error) {
	typ, _, err := p.parseType(false)
	if err != nil {
		return ast.Param{}, err
	}
	name, err := p.expect(lexer.Ident, "parameter name")
	if err != nil {
		return ast.Param{}, err
	}
	return ast.Param{Type: typ, Name: name.Literal}, nil
}

// parseType parses a scalar or array type. requireArraySize distinguishes
// the two array forms seed_spec.md uses: a variable declaration (§3)
// always writes a size (`Int[100]`, `Int[size]`), while a function
// parameter/return type (§7) always omits it (`Int[]`) — the "elements
// aren't part of the signature, arrays are always passed by reference"
// form isn't implemented until general functions land, but the grammar
// already distinguishes the two so a size given in the wrong place is a
// clear parse error rather than silently accepted. The returned Expr is
// the parsed size, non-nil only for an array type when requireArraySize
// is true.
func (p *parser) parseType(requireArraySize bool) (ast.Type, ast.Expr, error) {
	name, ok := typeKeywords[p.cur().Kind]
	if !ok {
		return ast.Type{}, nil, fmt.Errorf("line %d: expected a type, got %q", p.cur().Line, p.cur().Literal)
	}
	p.advance()

	t := ast.Type{Name: name}
	if p.cur().Kind != lexer.LBracket {
		return t, nil, nil
	}
	lbracket := p.advance() // '['

	if p.cur().Kind == lexer.RBracket {
		p.advance()
		if requireArraySize {
			return ast.Type{}, nil, fmt.Errorf("line %d: array declarations require a size, e.g. %s[100] or %s[n]", lbracket.Line, name, name)
		}
		t.IsArray = true
		return t, nil, nil
	}

	size, err := p.parseExpr()
	if err != nil {
		return ast.Type{}, nil, err
	}
	if _, err := p.expect(lexer.RBracket, "']'"); err != nil {
		return ast.Type{}, nil, err
	}
	if !requireArraySize {
		return ast.Type{}, nil, fmt.Errorf("line %d: a function parameter/return array type must not specify a size (write %s[])", lbracket.Line, name)
	}
	t.IsArray = true
	return t, size, nil
}

func (p *parser) parseBlock() ([]ast.Stmt, error) {
	if _, err := p.expect(lexer.LBrace, "'{'"); err != nil {
		return nil, err
	}
	p.skipNewlines()

	var stmts []ast.Stmt
	for p.cur().Kind != lexer.RBrace {
		stmt, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, stmt)
		p.skipNewlines()
	}
	if _, err := p.expect(lexer.RBrace, "'}'"); err != nil {
		return nil, err
	}
	return stmts, nil
}

func (p *parser) parseStmt() (ast.Stmt, error) {
	switch {
	case p.cur().Kind == lexer.KwReturn:
		return p.parseReturnStmt()
	case p.cur().Kind == lexer.KwIf:
		return p.parseIfStmt()
	case p.cur().Kind == lexer.KwWhile:
		return p.parseWhileStmt()
	case p.cur().Kind == lexer.KwFor:
		return p.parseForInStmt()
	case p.cur().Kind == lexer.KwBreak:
		tok := p.advance()
		return &ast.BreakStmt{Line: tok.Line}, nil
	case p.cur().Kind == lexer.KwContinue:
		tok := p.advance()
		return &ast.ContinueStmt{Line: tok.Line}, nil
	case p.isTypeKeyword():
		return p.parseVarDecl()
	case p.cur().Kind == lexer.Ident:
		return p.parseIdentStmt()
	default:
		return nil, fmt.Errorf("line %d: unexpected token %q", p.cur().Line, p.cur().Literal)
	}
}

// parseIfStmt parses an if/elif/else chain. `elif`/`else` must follow the
// previous block's closing '}' on the same line (no newline in between):
// a newline there ends the if-statement, same as any other statement, so
// a lone `elif`/`else` on its own line is correctly a syntax error rather
// than silently chaining.
func (p *parser) parseIfStmt() (ast.Stmt, error) {
	kw := p.advance() // 'if'
	stmt := &ast.IfStmt{Line: kw.Line}

	clause, err := p.parseIfClause()
	if err != nil {
		return nil, err
	}
	stmt.Clauses = append(stmt.Clauses, clause)

	for p.cur().Kind == lexer.KwElif {
		p.advance()
		clause, err := p.parseIfClause()
		if err != nil {
			return nil, err
		}
		stmt.Clauses = append(stmt.Clauses, clause)
	}

	if p.cur().Kind == lexer.KwElse {
		p.advance()
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		stmt.Else = body
	}

	return stmt, nil
}

func (p *parser) parseIfClause() (ast.IfClause, error) {
	cond, err := p.parseExpr()
	if err != nil {
		return ast.IfClause{}, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return ast.IfClause{}, err
	}
	return ast.IfClause{Cond: cond, Body: body}, nil
}

func (p *parser) parseWhileStmt() (ast.Stmt, error) {
	kw := p.advance() // 'while'
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.WhileStmt{Cond: cond, Body: body, Line: kw.Line}, nil
}

func (p *parser) parseForInStmt() (ast.Stmt, error) {
	kw := p.advance() // 'for'
	varName, err := p.expect(lexer.Ident, "loop variable name")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.KwIn, "'in'"); err != nil {
		return nil, err
	}
	arrName, err := p.expect(lexer.Ident, "array variable name")
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.ForInStmt{VarName: varName.Literal, ArrayName: arrName.Literal, Body: body, Line: kw.Line}, nil
}

func (p *parser) parseVarDecl() (*ast.VarDecl, error) {
	line := p.cur().Line
	typ, size, err := p.parseType(true)
	if err != nil {
		return nil, err
	}
	name, err := p.expect(lexer.Ident, "variable name")
	if err != nil {
		return nil, err
	}
	decl := &ast.VarDecl{Type: typ, Name: name.Literal, Size: size, Line: line}
	if p.cur().Kind == lexer.Assign {
		p.advance()
		init, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		decl.Init = init
	}
	return decl, nil
}

var compoundAssignOps = map[lexer.Kind]string{
	lexer.PlusAssign:    "+",
	lexer.MinusAssign:   "-",
	lexer.StarAssign:    "*",
	lexer.SlashAssign:   "/",
	lexer.PercentAssign: "%",
}

// parseIdentStmt parses a statement starting with an identifier: a call
// expression (`f(...)`), a scalar or array-element assignment (`name =
// value` / `name[index] = value`), a compound assignment (`name +=
// value` etc.), or `name++`/`name--`.
func (p *parser) parseIdentStmt() (ast.Stmt, error) {
	name := p.advance() // Ident
	switch {
	case p.cur().Kind == lexer.LParen:
		call, err := p.parseCallExprFrom(name)
		if err != nil {
			return nil, err
		}
		return &ast.ExprStmt{X: call, Line: call.Line}, nil
	case p.cur().Kind == lexer.LBracket:
		p.advance()
		index, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.RBracket, "']'"); err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.Assign, "'='"); err != nil {
			return nil, err
		}
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &ast.AssignStmt{Name: name.Literal, Index: index, Value: val, Line: name.Line}, nil
	case p.cur().Kind == lexer.Assign:
		p.advance()
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &ast.AssignStmt{Name: name.Literal, Value: val, Line: name.Line}, nil
	case compoundAssignOps[p.cur().Kind] != "":
		op := compoundAssignOps[p.cur().Kind]
		p.advance()
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &ast.CompoundAssignStmt{Name: name.Literal, Op: op, Value: val, Line: name.Line}, nil
	case p.cur().Kind == lexer.Inc || p.cur().Kind == lexer.Dec:
		opTok := p.advance()
		return &ast.IncDecStmt{Name: name.Literal, Op: opTok.Literal, Line: name.Line}, nil
	default:
		return nil, fmt.Errorf("line %d: expected '(', '=', a compound assignment, or '++'/'--' after %q", p.cur().Line, name.Literal)
	}
}

func (p *parser) parseReturnStmt() (ast.Stmt, error) {
	kw := p.advance() // 'return'
	if p.cur().Kind == lexer.Newline || p.cur().Kind == lexer.RBrace {
		return &ast.ReturnStmt{Line: kw.Line}, nil
	}
	x, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ast.ReturnStmt{X: x, Line: kw.Line}, nil
}

// parseExpr parses a full expression, following seed_spec.md §5's
// precedence table (lowest to highest: ||, &&, ==/!=, </<=/>/>=, +/-,
// */%, unary !/-, then primaries/parentheses).
func (p *parser) parseExpr() (ast.Expr, error) {
	return p.parseOr()
}

// binOpNames maps a binary/logical operator token to its AST Op string.
var binOpNames = map[lexer.Kind]string{
	lexer.OrOr:    "||",
	lexer.AndAnd:  "&&",
	lexer.Eq:      "==",
	lexer.Neq:     "!=",
	lexer.Lt:      "<",
	lexer.Lte:     "<=",
	lexer.Gt:      ">",
	lexer.Gte:     ">=",
	lexer.Plus:    "+",
	lexer.Minus:   "-",
	lexer.Star:    "*",
	lexer.Slash:   "/",
	lexer.Percent: "%",
}

// parseBinaryLevel implements one precedence level: it parses one operand
// via next, then folds in `next (op next)*` left-associatively for any of
// the given token kinds.
func (p *parser) parseBinaryLevel(next func() (ast.Expr, error), kinds ...lexer.Kind) (ast.Expr, error) {
	left, err := next()
	if err != nil {
		return nil, err
	}
	for kindIn(p.cur().Kind, kinds) {
		opTok := p.advance()
		right, err := next()
		if err != nil {
			return nil, err
		}
		left = &ast.BinaryExpr{Op: binOpNames[opTok.Kind], X: left, Y: right, Line: opTok.Line}
	}
	return left, nil
}

func kindIn(k lexer.Kind, kinds []lexer.Kind) bool {
	for _, want := range kinds {
		if k == want {
			return true
		}
	}
	return false
}

func (p *parser) parseOr() (ast.Expr, error) {
	return p.parseBinaryLevel(p.parseAnd, lexer.OrOr)
}

func (p *parser) parseAnd() (ast.Expr, error) {
	return p.parseBinaryLevel(p.parseEquality, lexer.AndAnd)
}

func (p *parser) parseEquality() (ast.Expr, error) {
	return p.parseBinaryLevel(p.parseComparison, lexer.Eq, lexer.Neq)
}

func (p *parser) parseComparison() (ast.Expr, error) {
	return p.parseBinaryLevel(p.parseAdditive, lexer.Lt, lexer.Lte, lexer.Gt, lexer.Gte)
}

func (p *parser) parseAdditive() (ast.Expr, error) {
	return p.parseBinaryLevel(p.parseMultiplicative, lexer.Plus, lexer.Minus)
}

func (p *parser) parseMultiplicative() (ast.Expr, error) {
	return p.parseBinaryLevel(p.parseUnary, lexer.Star, lexer.Slash, lexer.Percent)
}

func (p *parser) parseUnary() (ast.Expr, error) {
	if p.cur().Kind == lexer.Not || p.cur().Kind == lexer.Minus {
		opTok := p.advance()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		op := "!"
		if opTok.Kind == lexer.Minus {
			op = "-"
		}
		return &ast.UnaryExpr{Op: op, X: x, Line: opTok.Line}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (ast.Expr, error) {
	tok := p.cur()
	switch tok.Kind {
	case lexer.LParen:
		p.advance()
		x, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.RParen, "')'"); err != nil {
			return nil, err
		}
		return x, nil
	case lexer.LBrace:
		return p.parseArrayLit()
	case lexer.String:
		p.advance()
		return &ast.StringLit{Value: tok.Literal, Line: tok.Line}, nil
	case lexer.Int:
		p.advance()
		v, err := strconv.ParseInt(tok.Literal, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid integer literal %q", tok.Line, tok.Literal)
		}
		return &ast.IntLit{Value: v, Line: tok.Line}, nil
	case lexer.Float:
		p.advance()
		v, err := strconv.ParseFloat(tok.Literal, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid float literal %q", tok.Line, tok.Literal)
		}
		return &ast.FloatLit{Value: v, Line: tok.Line}, nil
	case lexer.KwTrue:
		p.advance()
		return &ast.BoolLit{Value: true, Line: tok.Line}, nil
	case lexer.KwFalse:
		p.advance()
		return &ast.BoolLit{Value: false, Line: tok.Line}, nil
	case lexer.KwNull:
		p.advance()
		return &ast.NullLit{Line: tok.Line}, nil
	case lexer.Ident:
		name := p.advance()
		if p.cur().Kind == lexer.LParen {
			return p.parseCallExprFrom(name)
		}
		if p.cur().Kind == lexer.LBracket {
			p.advance()
			index, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.RBracket, "']'"); err != nil {
				return nil, err
			}
			return &ast.IndexExpr{Name: name.Literal, Index: index, Line: name.Line}, nil
		}
		return &ast.Ident{Name: name.Literal, Line: name.Line}, nil
	default:
		return nil, fmt.Errorf("line %d: unexpected token %q", tok.Line, tok.Literal)
	}
}

// parseArrayLit parses an array literal, e.g. `{1, 2, 3}` (seed_spec.md
// §2). Its element type isn't determined here — see ast.ArrayLit's doc.
func (p *parser) parseArrayLit() (ast.Expr, error) {
	kw := p.advance() // '{'
	lit := &ast.ArrayLit{Line: kw.Line}
	for p.cur().Kind != lexer.RBrace {
		if len(lit.Elems) > 0 {
			if _, err := p.expect(lexer.Comma, "','"); err != nil {
				return nil, err
			}
		}
		elem, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		lit.Elems = append(lit.Elems, elem)
	}
	if _, err := p.expect(lexer.RBrace, "'}'"); err != nil {
		return nil, err
	}
	return lit, nil
}

// parseCallExprFrom parses the `(args...)` part of a call expression whose
// callee identifier has already been consumed.
func (p *parser) parseCallExprFrom(name lexer.Token) (*ast.CallExpr, error) {
	if _, err := p.expect(lexer.LParen, "'('"); err != nil {
		return nil, err
	}
	call := &ast.CallExpr{Callee: name.Literal, Line: name.Line}
	for p.cur().Kind != lexer.RParen {
		if len(call.Args) > 0 {
			if _, err := p.expect(lexer.Comma, "','"); err != nil {
				return nil, err
			}
		}
		arg, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		call.Args = append(call.Args, arg)
	}
	if _, err := p.expect(lexer.RParen, "')'"); err != nil {
		return nil, err
	}
	return call, nil
}
