// Package parser turns a token stream from the lexer into a Whale AST.
//
// The parser is hand-written recursive descent with Pratt-style
// precedence climbing for expressions.
package parser

import (
	"strconv"
	"strings"

	"github.com/whale-lang/whale/internal/ast"
	"github.com/whale-lang/whale/internal/lexer"
	"github.com/whale-lang/whale/internal/source"
)

// ParseError is a non-fatal parser problem.
type ParseError struct {
	Pos source.Position
	Msg string
}

func (e ParseError) Error() string {
	return e.Pos.String() + ": " + e.Msg
}

// Result bundles the parsed AST and any errors.
type Result struct {
	File   ast.File
	Errors []ParseError
}

// Parse is the entry point.
func Parse(toks []lexer.Token) Result {
	p := &parser{
		tokens: toks,
		pos:    0,
		errors: make([]ParseError, 0, 4),
	}
	file := p.parseFile()
	return Result{File: file, Errors: p.errors}
}

type parser struct {
	tokens []lexer.Token
	pos    int
	errors []ParseError
}

// ----------------------------------------------------------------------------
// Cursor helpers
// ----------------------------------------------------------------------------

func (p *parser) current() lexer.Token {
	if p.pos >= len(p.tokens) {
		return lexer.Token{Type: lexer.TOKEN_EOF}
	}
	return p.tokens[p.pos]
}

func (p *parser) peek(n int) lexer.Token {
	if p.pos+n >= len(p.tokens) {
		return lexer.Token{Type: lexer.TOKEN_EOF}
	}
	return p.tokens[p.pos+n]
}

func (p *parser) advance() lexer.Token {
	tok := p.current()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return tok
}

func (p *parser) expect(tt lexer.TokenType, what string) lexer.Token {
	if p.current().Type == tt {
		return p.advance()
	}
	got := p.current().Type.String()
	if p.current().Type == lexer.TOKEN_EOF {
		got = "end of file"
	}
	tok := p.current()
	p.errorAt(tok, "expected "+what+", got "+got)
	return tok
}

func (p *parser) expectSemi() {
	p.expect(lexer.TOKEN_SEMI, "';'")
}

func (p *parser) errorAt(tok lexer.Token, msg string) {
	p.errors = append(p.errors, ParseError{
		Pos: tok.Pos,
		Msg: msg,
	})
}

func tokPos(tok lexer.Token) ast.Position {
	return ast.Position{Line: tok.Pos.Line, Col: tok.Pos.Column}
}

// ----------------------------------------------------------------------------
// Top-level
// ----------------------------------------------------------------------------

func (p *parser) parseFile() ast.File {
	body := make([]ast.Stmt, 0, 8)
	for p.current().Type != lexer.TOKEN_EOF {
		startPos := p.pos
		stmt := p.parseStatement()
		if stmt != nil {
			body = append(body, stmt)
		}
		
		// If we didn't advance at all, force advance to avoid infinite loop
		if p.pos == startPos {
			p.advance()
		}
	}
	return ast.File{Body: body}
}

// ----------------------------------------------------------------------------
// Statements
// ----------------------------------------------------------------------------

func (p *parser) parseStatement() ast.Stmt {
	tok := p.current()
	switch tok.Type {
	case lexer.TOKEN_LET:
		return p.parseLet()
	case lexer.TOKEN_FN:
		return p.parseFnDecl()
	case lexer.TOKEN_RETURN:
		return p.parseReturn()
	case lexer.TOKEN_SPAWN:
		return p.parseSpawn()
	case lexer.TOKEN_IF:
		return p.parseIf()
	case lexer.TOKEN_WHILE:
		return p.parseWhile()
	case lexer.TOKEN_FOR:
		return p.parseFor()
	case lexer.TOKEN_ARENA:
		return p.parseArena()
	case lexer.TOKEN_STRUCT, lexer.TOKEN_PACKED:
		return p.parseStructDecl()
	case lexer.TOKEN_ENUM:
		return p.parseEnumDecl()
	case lexer.TOKEN_IMPORT:
		return p.parseImport()
	case lexer.TOKEN_EXTERN:
		return p.parseExternFnDecl()
	case lexer.TOKEN_PUB:
		p.advance() // consume 'pub'
		return p.parseStatement() // parse the actual statement
	default:
		return p.parseExprStatement()
	}
}

func (p *parser) parseExprStatement() ast.Stmt {
	pos := tokPos(p.current())
	expr := p.parseExpr()
	
	if p.current().Type == lexer.TOKEN_EQ {
		p.advance() // consume '='
		value := p.parseExpr()
		p.expectSemi()
		
		if ident, ok := expr.(*ast.Ident); ok {
			return &ast.AssignStmt{Pos: pos, Name: ident.Name, Value: value}
		} else if field, ok := expr.(*ast.FieldAccess); ok {
			return &ast.AssignFieldStmt{Pos: pos, Object: field.Expr, Field: field.Field, Value: value}
		} else if idx, ok := expr.(*ast.IndexExpr); ok {
			return &ast.AssignIndexStmt{Pos: pos, List: idx.Expr, Index: idx.Index, Value: value}
		} else {
			p.errorAt(p.current(), "invalid assignment target")
			return &ast.ExprStmt{Pos: pos, Expr: expr}
		}
	}
	
	if p.current().Type == lexer.TOKEN_LARROW {
		valPos := tokPos(p.advance()) // consume <-
		val := p.parseExpr()
		p.expectSemi()
		return &ast.ChanSendStmt{Pos: valPos, Chan: expr, Value: val}
	}

	p.expectSemi()
	return &ast.ExprStmt{Pos: pos, Expr: expr}
}

func (p *parser) parseSpawn() ast.Stmt {
	pos := tokPos(p.advance()) // consume 'spawn'
	expr := p.parseExpr()
	if call, ok := expr.(*ast.CallExpr); ok {
		p.expectSemi()
		return &ast.SpawnStmt{Pos: pos, Call: call}
	}
	p.errorAt(p.current(), "expected function call after 'spawn'")
	return nil
}

func (p *parser) parseImport() ast.Stmt {
	pos := tokPos(p.advance()) // consume 'import'
	
	pathTok := p.expect(lexer.TOKEN_STRING, "import path")
	path := pathTok.Literal
	alias := ""
	
	if p.current().Type == lexer.TOKEN_AS {
		p.advance() // consume 'as'
		aliasTok := p.expect(lexer.TOKEN_IDENT, "import alias")
		alias = aliasTok.Literal
	} else {
		parts := strings.Split(path, "/")
		alias = parts[len(parts)-1]
		alias = strings.TrimSuffix(alias, ".wh")
	}
	
	p.expectSemi()
	return &ast.ImportStmt{
		Pos:   pos,
		Path:  path,
		Alias: alias,
	}
}

func (p *parser) parseTypeParams() []string {
	var params []string
	if p.current().Type == lexer.TOKEN_LT {
		p.advance() // consume '<'
		for p.current().Type != lexer.TOKEN_GT && p.current().Type != lexer.TOKEN_EOF {
			if p.current().Type != lexer.TOKEN_IDENT {
				p.errorAt(p.current(), "expected type parameter name")
				break
			}
			params = append(params, p.advance().Literal)
			if p.current().Type == lexer.TOKEN_COMMA {
				p.advance()
			} else if p.current().Type != lexer.TOKEN_GT {
				p.errorAt(p.current(), "expected ',' or '>'")
				break
			}
		}
		p.expect(lexer.TOKEN_GT, "'>' after type parameters")
	}
	return params
}

func (p *parser) parseTypeString() string {
	if p.current().Type == lexer.TOKEN_LBRACK {
		p.advance() // '['
		inner := p.parseTypeString()
		p.expect(lexer.TOKEN_RBRACK, "']'")
		return "[" + inner + "]"
	}
	if p.current().Type != lexer.TOKEN_IDENT && 
	   p.current().Type != lexer.TOKEN_STREAM && 
	   p.current().Type != lexer.TOKEN_CHAN && 
	   p.current().Type != lexer.TOKEN_FN {
		p.errorAt(p.current(), "expected type name")
		return ""
	}
	name := p.advance().Literal
	if p.current().Type == lexer.TOKEN_LT {
		p.advance() // '<'
		name += "<"
		for p.current().Type != lexer.TOKEN_GT && p.current().Type != lexer.TOKEN_EOF {
			name += p.parseTypeString()
			if p.current().Type == lexer.TOKEN_COMMA {
				name += ", "
				p.advance()
			} else if p.current().Type != lexer.TOKEN_GT {
				p.errorAt(p.current(), "expected ',' or '>' in type arguments")
				break
			}
		}
		p.expect(lexer.TOKEN_GT, "'>'")
		name += ">"
	}
	return name
}

func (p *parser) parseLet() ast.Stmt {
	pos := tokPos(p.advance()) // consume 'let'

	mutable := false
	if p.current().Type == lexer.TOKEN_MUT {
		mutable = true
		p.advance()
	}

	if p.current().Type != lexer.TOKEN_IDENT {
		p.errorAt(p.current(), "expected identifier after 'let'")
		return nil
	}
	name := p.advance().Literal

	typeAnn := ""
	if p.current().Type == lexer.TOKEN_COLON {
		p.advance() // consume ':'
		typeAnn = p.parseTypeString()
	}

	p.expect(lexer.TOKEN_EQ, "'='")
	value := p.parseExpr()
	p.expectSemi()

	return &ast.LetStmt{
		Pos:     pos,
		Name:    name,
		Mutable: mutable,
		TypeAnn: typeAnn,
		Value:   value,
	}
}


func (p *parser) parseBlock() *ast.BlockStmt {
	pos := tokPos(p.current())
	if p.current().Type != lexer.TOKEN_LBRACE {
		p.errorAt(p.current(), "expected '{'")
		return &ast.BlockStmt{Pos: pos}
	}
	p.advance() // consume '{'
	body := make([]ast.Stmt, 0, 4)
	for p.current().Type != lexer.TOKEN_RBRACE && p.current().Type != lexer.TOKEN_EOF {
		stmt := p.parseStatement()
		if stmt == nil {
			p.advance() // skip bad token
			continue
		}
		body = append(body, stmt)
	}
	p.expect(lexer.TOKEN_RBRACE, "'}'")
	return &ast.BlockStmt{Pos: pos, Body: body}
}

func (p *parser) parseIf() ast.Stmt {
	pos := tokPos(p.advance()) // consume 'if'
	cond := p.parseExpr()
	if cond == nil {
		return nil
	}
	then := p.parseBlock()
	var elseStmt ast.Stmt
	if p.current().Type == lexer.TOKEN_ELSE {
		p.advance() // consume 'else'
		if p.current().Type == lexer.TOKEN_IF {
			elseStmt = p.parseIf()
		} else {
			elseStmt = p.parseBlock()
		}
	}
	return &ast.IfStmt{
		Pos:       pos,
		Condition: cond,
		Then:      then,
		Else:      elseStmt,
	}
}

func (p *parser) parseWhile() ast.Stmt {
	pos := tokPos(p.advance()) // consume 'while'
	cond := p.parseExpr()
	if cond == nil {
		return nil
	}
	body := p.parseBlock()
	return &ast.WhileStmt{Pos: pos, Condition: cond, Body: body}
}

func (p *parser) parseArena() ast.Stmt {
	pos := tokPos(p.advance()) // consume 'arena'
	body := p.parseBlock()
	return &ast.ArenaStmt{Pos: pos, Body: body}
}

func (p *parser) parseFor() ast.Stmt {
	pos := tokPos(p.advance()) // consume 'for'
	if p.current().Type != lexer.TOKEN_IDENT {
		p.errorAt(p.current(), "expected variable name after 'for'")
		return nil
	}
	variable := p.advance().Literal
	if p.current().Type != lexer.TOKEN_IN {
		p.errorAt(p.current(), "expected 'in' after for variable")
		return nil
	}
	p.advance() // consume 'in'
	iter := p.parseExpr()
	if iter == nil {
		return nil
	}
	body := p.parseBlock()
	return &ast.ForStmt{Pos: pos, Variable: variable, Iterable: iter, Body: body}
}

func (p *parser) parseReturn() ast.Stmt {
	pos := tokPos(p.advance()) // consume 'return'
	var value ast.Expr
	if p.current().Type != lexer.TOKEN_SEMI {
		value = p.parseExpr()
	}
	p.expectSemi()
	return &ast.ReturnStmt{Pos: pos, Value: value}
}

func (p *parser) parseFnDecl() ast.Stmt {
	pos := tokPos(p.advance()) // consume 'fn'

	if p.current().Type != lexer.TOKEN_IDENT {
		p.errorAt(p.current(), "expected function name after 'fn'")
		return nil
	}
	name := p.advance().Literal
	typeParams := p.parseTypeParams()
	params := p.parseParams()

	returnType := ""
	if p.current().Type == lexer.TOKEN_DASHARROW {
		p.advance() // consume '->'
		returnType = p.parseTypeString()
	}

	body := p.parseBlock()
	return &ast.FnStmt{
		Pos:        pos,
		Name:       name,
		TypeParams: typeParams,
		Params:     params,
		ReturnType: returnType,
		Body:       body,
	}
}

func (p *parser) parseExternFnDecl() ast.Stmt {
	pos := tokPos(p.advance()) // consume 'extern'

	if p.current().Type != lexer.TOKEN_FN {
		p.errorAt(p.current(), "expected 'fn' after 'extern'")
		return nil
	}
	p.advance() // consume 'fn'

	if p.current().Type != lexer.TOKEN_IDENT {
		p.errorAt(p.current(), "expected function name after 'extern fn'")
		return nil
	}
	name := p.advance().Literal
	params := p.parseParams()

	returnType := ""
	if p.current().Type == lexer.TOKEN_DASHARROW {
		p.advance() // consume '->'
		returnType = p.parseTypeString()
	}

	p.expectSemi()

	return &ast.ExternFnStmt{
		Pos:        pos,
		Name:       name,
		Params:     params,
		ReturnType: returnType,
	}
}

func (p *parser) parseParams() []ast.Param {
	p.expect(lexer.TOKEN_LPAREN, "'('")
	params := make([]ast.Param, 0, 2)
	if p.current().Type == lexer.TOKEN_RPAREN {
		p.advance()
		return params
	}
	for {
		if p.current().Type != lexer.TOKEN_IDENT {
			p.errorAt(p.current(), "expected parameter name")
			return params
		}
		pname := p.advance().Literal
		ptype := ""
		if p.current().Type == lexer.TOKEN_COLON {
			p.advance() // consume ':'
			ptype = p.parseTypeString()
		}
		params = append(params, ast.Param{Name: pname, Type: ptype})
		if p.current().Type != lexer.TOKEN_COMMA {
			break
		}
		p.advance() // consume ','
	}
	p.expect(lexer.TOKEN_RPAREN, "')'")
	return params
}

func (p *parser) parseStructDecl() ast.Stmt {
	isPacked := false
	if p.current().Type == lexer.TOKEN_PACKED {
		isPacked = true
		p.advance() // consume 'packed'
	}
	pos := tokPos(p.advance()) // consume 'struct'
	if p.current().Type != lexer.TOKEN_IDENT {
		p.errorAt(p.current(), "expected struct name after 'struct'")
		return nil
	}
	name := p.advance().Literal
	typeParams := p.parseTypeParams()
	p.expect(lexer.TOKEN_LBRACE, "'{'")
	fields := make([]ast.Param, 0, 2)
	for p.current().Type != lexer.TOKEN_RBRACE && p.current().Type != lexer.TOKEN_EOF {
		if p.current().Type != lexer.TOKEN_IDENT {
			p.errorAt(p.current(), "expected field name")
			break
		}
		fname := p.advance().Literal
		p.expect(lexer.TOKEN_COLON, "':'")
		ftype := ""
		if p.current().Type == lexer.TOKEN_IDENT {
			ftype = p.parseTypeString()
		} else {
			p.errorAt(p.current(), "expected field type")
		}
		fields = append(fields, ast.Param{Name: fname, Type: ftype})
		if p.current().Type == lexer.TOKEN_COMMA {
			p.advance() // consume ','
		}
	}
	p.expect(lexer.TOKEN_RBRACE, "'}'")
	return &ast.StructDecl{Pos: pos, Name: name, TypeParams: typeParams, Fields: fields, Packed: isPacked}
}

func (p *parser) parseEnumDecl() *ast.EnumDecl {
	pos := tokPos(p.advance()) // consume 'enum'
	nameTok := p.expect(lexer.TOKEN_IDENT, "enum name")
	name := nameTok.Literal
	typeParams := p.parseTypeParams()

	p.expect(lexer.TOKEN_LBRACE, "'{' after enum name")
	var variants []*ast.EnumVariant
	for p.current().Type != lexer.TOKEN_RBRACE && p.current().Type != lexer.TOKEN_EOF {
		vpos := tokPos(p.current())
		vnameTok := p.expect(lexer.TOKEN_IDENT, "variant name")
		var vtype ast.Expr
		if p.current().Type == lexer.TOKEN_LPAREN {
			p.advance() // '('
			vtype = p.parseExpr()
			p.expect(lexer.TOKEN_RPAREN, "')' after payload type")
		}
		variants = append(variants, &ast.EnumVariant{Pos: vpos, Name: vnameTok.Literal, Type: vtype})
		
		if p.current().Type == lexer.TOKEN_COMMA {
			p.advance() // consume ','
		} else if p.current().Type != lexer.TOKEN_RBRACE {
			p.errorAt(p.current(), "expected ',' or '}' in enum")
			break
		}
	}
	p.expect(lexer.TOKEN_RBRACE, "'}' at end of enum")
	
	return &ast.EnumDecl{
		Pos:        pos,
		Name:       name,
		TypeParams: typeParams,
		Variants:   variants,
	}
}

// ----------------------------------------------------------------------------
// Expressions — Pratt precedence climbing
// ----------------------------------------------------------------------------

func (p *parser) parseExpr() ast.Expr {
	return p.parsePipe()
}

// precedence returns the (left, right) binding power of an operator.
func precedence(tt lexer.TokenType) (int, int) {
	switch tt {
	case lexer.TOKEN_PIPE_GT:
		return 10, 11
	case lexer.TOKEN_OR:
		return 20, 21
	case lexer.TOKEN_AND:
		return 30, 31
	case lexer.TOKEN_EQEQ, lexer.TOKEN_NEQ:
		return 40, 40
	case lexer.TOKEN_LT, lexer.TOKEN_LTE, lexer.TOKEN_GT, lexer.TOKEN_GTE:
		return 45, 45
	case lexer.TOKEN_PLUS, lexer.TOKEN_MINUS:
		return 50, 51
	case lexer.TOKEN_STAR, lexer.TOKEN_SLASH, lexer.TOKEN_PERCENT:
		return 60, 61
	}
	return 0, 0
}

func (p *parser) parsePipe() ast.Expr {
	return p.parseExprBP(0)
}

func (p *parser) parseExprBP(minBP int) ast.Expr {
	left := p.parsePostfix()
	if left == nil {
		return nil
	}
	for {
		tok := p.current()
		lbp, rbp := precedence(tok.Type)
		if lbp == 0 || lbp < minBP {
			break
		}
		if tok.Type == lexer.TOKEN_PIPE_GT {
			pipeTok := p.advance() // consume '|>'
			right := p.parseExprBP(rbp)
			left = buildPipeCall(left, right, pipeTok.Pos)
			continue
		}
		op := p.advance().Literal
		right := p.parseExprBP(rbp)
		left = &ast.BinaryOp{
			Pos:   tokPos(tok),
			Op:    op,
			Left:  left,
			Right: right,
		}
	}
	return left
}

// buildPipeCall desugars `x |> f` to `f(x)` and `x |> f(a, b)` to `f(x, a, b)`.
// Also auto-wraps list literals in stream().
func buildPipeCall(left, right ast.Expr, pipePos source.Position) ast.Expr {
	pos := ast.Position{Line: pipePos.Line, Col: pipePos.Column}

	// Auto-wrap list literals in stream()
	if _, isList := left.(*ast.ListLit); isList {
		left = &ast.CallExpr{
			Pos:    pos,
			Callee: &ast.Ident{Pos: pos, Name: "stream"},
			Args:   []ast.Expr{left},
		}
	}

	switch r := right.(type) {
	case *ast.Ident:
		// x |> f  =>  f(x)
		return &ast.CallExpr{
			Pos:    pos,
			Callee: r,
			Args:   []ast.Expr{left},
		}
	case *ast.CallExpr:
		// x |> f(a, b)  =>  f(x, a, b)
		newArgs := make([]ast.Expr, 0, len(r.Args)+1)
		newArgs = append(newArgs, left)
		newArgs = append(newArgs, r.Args...)
		return &ast.CallExpr{
			Pos:    pos,
			Callee: r.Callee,
			Args:   newArgs,
		}
	}
	// Fall back to binary op (type checker will flag this)
	return &ast.BinaryOp{Pos: pos, Op: "|>", Left: left, Right: right}
}

func (p *parser) parsePostfix() ast.Expr {
	left := p.parseUnary()
	if left == nil {
		return nil
	}
	for {
		switch p.current().Type {
		case lexer.TOKEN_LPAREN:
			pos := tokPos(p.current())
			args := p.parseCallArgs()
			left = &ast.CallExpr{
				Pos:    pos,
				Callee: left,
				Args:   args,
			}
		case lexer.TOKEN_DOT:
			p.advance() // consume '.'
			if p.current().Type != lexer.TOKEN_IDENT {
				p.errorAt(p.current(), "expected field name after '.'")
				return left
			}
			fieldTok := p.advance()
			left = &ast.FieldAccess{
				Pos:   tokPos(fieldTok),
				Expr:  left,
				Field: fieldTok.Literal,
			}
		case lexer.TOKEN_LBRACK:
			p.advance() // consume '['
			idx := p.parseExpr()
			p.expect(lexer.TOKEN_RBRACK, "']'")
			left = &ast.IndexExpr{
				Pos:   tokPos(p.current()),
				Expr:  left,
				Index: idx,
			}
		case lexer.TOKEN_QUESTION:
			pos := tokPos(p.current())
			p.advance()
			left = &ast.TryExpr{
				Pos:  pos,
				Expr: left,
			}
		default:
			return left
		}
	}
}

func (p *parser) parseUnary() ast.Expr {
	// Check for lambda: IDENT => expr
	if p.current().Type == lexer.TOKEN_IDENT && p.peek(1).Type == lexer.TOKEN_ARROW {
		return p.parseLambda()
	}
	// Check for parenthesized lambda: (params) => expr
	if p.current().Type == lexer.TOKEN_LPAREN && isLambdaAhead(p) {
		return p.parseLambda()
	}
	// fn literal (anonymous)
	if p.current().Type == lexer.TOKEN_FN && p.peek(1).Type == lexer.TOKEN_LPAREN {
		return p.parseFnLit()
	}

	tok := p.current()
	switch tok.Type {
	case lexer.TOKEN_MINUS:
		p.advance()
		expr := p.parsePostfix()
		if expr == nil {
			return nil
		}
		return &ast.UnaryOp{Pos: tokPos(tok), Op: "-", Expr: expr}
	case lexer.TOKEN_NOT:
		p.advance()
		expr := p.parsePostfix()
		if expr == nil {
			return nil
		}
		return &ast.UnaryOp{Pos: tokPos(tok), Op: "!", Expr: expr}
	case lexer.TOKEN_LARROW:
		p.advance()
		expr := p.parsePostfix()
		if expr == nil {
			return nil
		}
		return &ast.ChanRecvExpr{Pos: tokPos(tok), Chan: expr}
	}
	return p.parsePrimary()
}

// isLambdaAhead uses a speculative parse to detect (params) => pattern.
func isLambdaAhead(p *parser) bool {
	save := p.pos
	defer func() { p.pos = save }()

	if p.current().Type != lexer.TOKEN_LPAREN {
		return false
	}
	p.advance() // consume '('

	// Empty parens before => is valid: () => expr
	if p.current().Type == lexer.TOKEN_RPAREN {
		p.advance()
		return p.current().Type == lexer.TOKEN_ARROW
	}

	// Expect ident (: type)? (, ident (: type)?)*
	for {
		if p.current().Type != lexer.TOKEN_IDENT {
			return false
		}
		p.advance() // consume ident
		if p.current().Type == lexer.TOKEN_COLON {
			p.advance() // consume ':'
			if p.current().Type != lexer.TOKEN_IDENT {
				return false
			}
			p.advance() // consume type
		}
		if p.current().Type != lexer.TOKEN_COMMA {
			break
		}
		p.advance() // consume ','
	}
	if p.current().Type != lexer.TOKEN_RPAREN {
		return false
	}
	p.advance()
	return p.current().Type == lexer.TOKEN_ARROW
}

func (p *parser) parseLambda() ast.Expr {
	pos := tokPos(p.current())
	var params []ast.Param

	if p.current().Type == lexer.TOKEN_IDENT {
		// Single bare identifier: x => expr
		params = append(params, ast.Param{Name: p.advance().Literal})
	} else {
		// Paren form: (params)
		p.advance() // consume '('
		if p.current().Type != lexer.TOKEN_RPAREN {
			for {
				if p.current().Type != lexer.TOKEN_IDENT {
					p.errorAt(p.current(), "expected parameter name in lambda")
					return nil
				}
				pname := p.advance().Literal
				ptype := ""
				if p.current().Type == lexer.TOKEN_COLON {
					p.advance()
					if p.current().Type == lexer.TOKEN_IDENT {
						ptype = p.advance().Literal
					}
				}
				params = append(params, ast.Param{Name: pname, Type: ptype})
				if p.current().Type != lexer.TOKEN_COMMA {
					break
				}
				p.advance()
			}
		}
		p.expect(lexer.TOKEN_RPAREN, "')'")
	}

	if p.current().Type != lexer.TOKEN_ARROW {
		p.errorAt(p.current(), "expected '=>' in lambda")
		return nil
	}
	p.advance() // consume '=>'

	body := p.parseExpr()
	return &ast.FnLit{
		Pos:    pos,
		Params: params,
		Body:   &ast.FnBody{Expr: body},
	}
}

func (p *parser) parseFnLit() ast.Expr {
	pos := tokPos(p.advance()) // consume 'fn'
	params := p.parseParams()
	returnType := ""
	if p.current().Type == lexer.TOKEN_DASHARROW {
		p.advance()
		if p.current().Type == lexer.TOKEN_IDENT {
			returnType = p.advance().Literal
		}
	}
	body := p.parseBlock()
	return &ast.FnLit{
		Pos:        pos,
		Params:     params,
		ReturnType: returnType,
		Body:       &ast.FnBody{Block: body},
	}
}

func (p *parser) parsePrimary() ast.Expr {
	tok := p.current()
	switch tok.Type {
	case lexer.TOKEN_INT:
		p.advance()
		v, err := parseInt64(tok.Literal)
		if err != nil {
			p.errorAt(tok, "invalid integer literal: "+err.Error())
			return nil
		}
		return &ast.IntLit{Pos: tokPos(tok), Value: v}

	case lexer.TOKEN_FLOAT:
		p.advance()
		v, err := parseFloat64(tok.Literal)
		if err != nil {
			p.errorAt(tok, "invalid float literal: "+err.Error())
			return nil
		}
		return &ast.FloatLit{Pos: tokPos(tok), Value: v}

	case lexer.TOKEN_STRING:
		p.advance()
		return &ast.StringLit{Pos: tokPos(tok), Value: tok.Literal}

	case lexer.TOKEN_TRUE:
		p.advance()
		return &ast.BoolLit{Pos: tokPos(tok), Value: true}

	case lexer.TOKEN_FALSE:
		p.advance()
		return &ast.BoolLit{Pos: tokPos(tok), Value: false}

	case lexer.TOKEN_IDENT, lexer.TOKEN_STREAM:
		// Could be struct literal: Name { field: val }
		if p.peek(1).Type == lexer.TOKEN_LBRACE {
			// Lookahead to distinguish from control-flow block brace
			if p.peek(2).Type == lexer.TOKEN_RBRACE || (p.peek(2).Type == lexer.TOKEN_IDENT && p.peek(3).Type == lexer.TOKEN_COLON) {
				return p.parseStructLit()
			}
		}
		p.advance()
		return &ast.Ident{Pos: tokPos(tok), Name: tok.Literal}

	case lexer.TOKEN_MATCH:
		return p.parseMatchExpr()

	case lexer.TOKEN_ERROR:
		pos := tokPos(tok)
		p.advance()
		p.expect(lexer.TOKEN_LPAREN, "'(' after error")
		msg := p.parseExpr()
		p.expect(lexer.TOKEN_RPAREN, "')' after error message")
		return &ast.ErrorLit{Pos: pos, Msg: msg}

	case lexer.TOKEN_COMPTIME:
		pos := tokPos(p.advance()) // consume 'comptime'
		var expr ast.Expr
		if p.current().Type == lexer.TOKEN_LBRACE {
			// comptime { block } -> parse as BlockStmt then wrap in something?
			// Wait, if it's `{ ... }`, parseBlock returns a Stmt. We can't put Stmt in Expr directly without a new node or cast.
			// Let's just say for now comptime expects an Expr, and we support blocks as expressions via a new trick?
			// Or just:
			blockPos := tokPos(p.current())
			block := p.parseBlock()
			// Actually, parseBlock() returns *ast.BlockStmt. Let's create an anonymous function and call it immediately!
			// comptime { ... } => comptime (fn() { ... })()
			fnLit := &ast.FnLit{
				Pos: blockPos,
				Body: &ast.FnBody{Block: block},
			}
			expr = &ast.CallExpr{
				Pos: blockPos,
				Callee: fnLit,
				Args: nil,
			}
		} else {
			expr = p.parseExpr()
		}
		return &ast.ComptimeExpr{Pos: pos, Expr: expr}

	case lexer.TOKEN_LPAREN:
		p.advance() // consume '('
		expr := p.parseExpr()
		p.expect(lexer.TOKEN_RPAREN, "')'")
		return expr

	case lexer.TOKEN_LBRACK:
		// List literal: [expr, expr, expr]
		pos := tokPos(tok)
		p.advance() // consume '['
		elems := make([]ast.Expr, 0, 4)
		if p.current().Type != lexer.TOKEN_RBRACK {
			for {
				el := p.parseExpr()
				if el == nil {
					break
				}
				elems = append(elems, el)
				if p.current().Type != lexer.TOKEN_COMMA {
					break
				}
				p.advance() // consume ','
			}
		}
		p.expect(lexer.TOKEN_RBRACK, "']'")
		return &ast.ListLit{Pos: pos, Values: elems}

	default:
		p.errorAt(tok, "expected expression, got "+tok.Type.String())
		return nil
	}
}

func (p *parser) parseStructLit() ast.Expr {
	pos := tokPos(p.current())
	typeName := p.advance().Literal // consume type name
	p.advance()                     // consume '{'
	fields := make(map[string]ast.Expr)
	for p.current().Type != lexer.TOKEN_RBRACE && p.current().Type != lexer.TOKEN_EOF {
		if p.current().Type != lexer.TOKEN_IDENT {
			p.errorAt(p.current(), "expected field name in struct literal")
			break
		}
		fname := p.advance().Literal
		p.expect(lexer.TOKEN_COLON, "':'")
		fval := p.parseExpr()
		if fval == nil {
			break
		}
		fields[fname] = fval
		if p.current().Type == lexer.TOKEN_COMMA {
			p.advance()
		}
	}
	p.expect(lexer.TOKEN_RBRACE, "'}'")
	return &ast.StructLit{Pos: pos, Type: typeName, Fields: fields}
}

func (p *parser) parseCallArgs() []ast.Expr {
	p.advance() // consume '('
	args := make([]ast.Expr, 0, 2)
	if p.current().Type == lexer.TOKEN_RPAREN {
		p.advance()
		return args
	}
	for {
		arg := p.parseExpr()
		if arg != nil {
			args = append(args, arg)
		}
		if p.current().Type != lexer.TOKEN_COMMA {
			break
		}
		p.advance() // consume ','
	}
	p.expect(lexer.TOKEN_RPAREN, "')'")
	return args
}

func (p *parser) parseMatchExpr() ast.Expr {
	pos := tokPos(p.advance()) // consume 'match'
	expr := p.parseExpr()
	p.expect(lexer.TOKEN_LBRACE, "'{' after match expression")
	
	var arms []*ast.MatchArm
	for p.current().Type != lexer.TOKEN_RBRACE && p.current().Type != lexer.TOKEN_EOF {
		apos := tokPos(p.current())
		
		var variant string
		var binding string
		isCatchAll := false
		
		if p.current().Type == lexer.TOKEN_IDENT {
			if p.current().Literal == "_" {
				isCatchAll = true
				p.advance() // '_'
			} else {
				variant = p.advance().Literal
				if p.current().Type == lexer.TOKEN_LPAREN {
					p.advance() // '('
					bindingTok := p.expect(lexer.TOKEN_IDENT, "binding name")
					binding = bindingTok.Literal
					p.expect(lexer.TOKEN_RPAREN, "')'")
				}
			}
		} else {
			p.errorAt(p.current(), "expected variant name or '_' in match arm")
			break
		}
		
		p.expect(lexer.TOKEN_ARROW, "'=>'")
		body := p.parseExpr()
		
		arms = append(arms, &ast.MatchArm{
			Pos:        apos,
			Variant:    variant,
			Binding:    binding,
			IsCatchAll: isCatchAll,
			Body:       body,
		})
		
		if p.current().Type == lexer.TOKEN_COMMA {
			p.advance() // consume ','
		}
	}
	p.expect(lexer.TOKEN_RBRACE, "'}' at end of match")
	return &ast.MatchExpr{Pos: pos, Expr: expr, Arms: arms}
}

// ----------------------------------------------------------------------------
// Numeric parsers
// ----------------------------------------------------------------------------

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func parseFloat64(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}
