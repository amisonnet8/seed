// Package parser builds an AST from Seed source code.
//
// The grammar implemented here is intentionally a subset of seed_spec.md:
// top-level func declarations, a block of statements limited to call
// expressions and return, and string/int literal arguments. Later
// development steps extend this grammar (variables, operators, control
// flow, arrays) one feature at a time.
package parser

import (
	"fmt"

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

func (p *parser) parseFile() (*ast.File, error) {
	f := &ast.File{}
	p.skipNewlines()
	for p.cur().Kind != lexer.EOF {
		fn, err := p.parseFuncDecl()
		if err != nil {
			return nil, err
		}
		f.Funcs = append(f.Funcs, fn)
		p.skipNewlines()
	}
	return f, nil
}

func (p *parser) parseFuncDecl() (*ast.FuncDecl, error) {
	kw, err := p.expect(lexer.KwFunc, "'func'")
	if err != nil {
		return nil, err
	}
	fn := &ast.FuncDecl{Line: kw.Line}

	if p.cur().Kind != lexer.Ident {
		// A return type precedes the name: `func Int main(...)`.
		rt, err := p.parseType()
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
	typ, err := p.parseType()
	if err != nil {
		return ast.Param{}, err
	}
	name, err := p.expect(lexer.Ident, "parameter name")
	if err != nil {
		return ast.Param{}, err
	}
	return ast.Param{Type: typ, Name: name.Literal}, nil
}

var typeKeywords = map[lexer.Kind]string{
	lexer.KwTypeInt:    "Int",
	lexer.KwTypeFloat:  "Float",
	lexer.KwTypeString: "String",
	lexer.KwTypeBool:   "Bool",
	lexer.KwTypeFile:   "File",
}

func (p *parser) parseType() (ast.Type, error) {
	name, ok := typeKeywords[p.cur().Kind]
	if !ok {
		return ast.Type{}, fmt.Errorf("line %d: expected a type, got %q", p.cur().Line, p.cur().Literal)
	}
	p.advance()

	t := ast.Type{Name: name}
	if p.cur().Kind == lexer.LBracket {
		p.advance()
		if _, err := p.expect(lexer.RBracket, "']'"); err != nil {
			return ast.Type{}, err
		}
		t.IsSlice = true
	}
	return t, nil
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
	if p.cur().Kind == lexer.KwReturn {
		return p.parseReturnStmt()
	}
	return p.parseExprStmt()
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

func (p *parser) parseExprStmt() (ast.Stmt, error) {
	x, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	call, ok := x.(*ast.CallExpr)
	if !ok {
		return nil, fmt.Errorf("line %d: only function calls are supported as statements", exprLine(x))
	}
	return &ast.ExprStmt{X: call, Line: call.Line}, nil
}

// parseExpr parses a primary expression. Step 1 only needs literals and
// call expressions; the full expression grammar (operators, precedence)
// is built out in a later step.
func (p *parser) parseExpr() (ast.Expr, error) {
	tok := p.cur()
	switch tok.Kind {
	case lexer.String:
		p.advance()
		return &ast.StringLit{Value: tok.Literal, Line: tok.Line}, nil
	case lexer.Int:
		p.advance()
		var v int64
		if _, err := fmt.Sscanf(tok.Literal, "%d", &v); err != nil {
			return nil, fmt.Errorf("line %d: invalid integer literal %q", tok.Line, tok.Literal)
		}
		return &ast.IntLit{Value: v, Line: tok.Line}, nil
	case lexer.Ident:
		return p.parseCallExpr()
	default:
		return nil, fmt.Errorf("line %d: unexpected token %q", tok.Line, tok.Literal)
	}
}

func (p *parser) parseCallExpr() (ast.Expr, error) {
	name := p.advance() // Ident
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

func exprLine(e ast.Expr) int {
	switch v := e.(type) {
	case *ast.CallExpr:
		return v.Line
	case *ast.StringLit:
		return v.Line
	case *ast.IntLit:
		return v.Line
	default:
		return 0
	}
}
