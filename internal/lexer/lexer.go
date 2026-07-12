// Package lexer turns Whale source text into a stream of tokens.
// The lexer is hand-written (not generated) for clarity.
package lexer

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/whale-lang/whale/internal/source"
)

// LexError represents a non-fatal lexing problem.
type LexError struct {
	Pos source.Position
	Msg string
}

func (e LexError) Error() string {
	return e.Pos.String() + ": " + e.Msg
}

// Result bundles the tokens and any errors from a lexing pass.
type Result struct {
	Tokens []Token
	Errors []LexError
}

// Lex tokenizes the full source. Always returns a Result.
func Lex(src string) Result {
	l := &lexer{
		src:    src,
		tokens: make([]Token, 0, 64),
		errors: make([]LexError, 0, 4),
		line:   1,
		col:    1,
	}
	l.run()
	l.tokens = append(l.tokens, Token{
		Type:    TOKEN_EOF,
		Literal: "",
		Pos:     source.Position{Line: l.line, Column: l.col, Offset: l.offset},
		Line:    l.line,
		Col:     l.col,
	})
	return Result{Tokens: l.tokens, Errors: l.errors}
}

type lexer struct {
	src    string
	tokens []Token
	errors []LexError
	pos    int // current read position (byte)
	offset int // byte offset of start of current token
	line   int
	col    int
}

func (l *lexer) peek() rune {
	if l.pos >= len(l.src) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.src[l.pos:])
	return r
}

func (l *lexer) peekAt(n int) rune {
	p := l.pos
	for i := 0; i < n; i++ {
		if p >= len(l.src) {
			return 0
		}
		_, sz := utf8.DecodeRuneInString(l.src[p:])
		p += sz
	}
	if p >= len(l.src) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.src[p:])
	return r
}

func (l *lexer) advance() rune {
	if l.pos >= len(l.src) {
		return 0
	}
	r, sz := utf8.DecodeRuneInString(l.src[l.pos:])
	l.pos += sz
	l.offset += sz
	if r == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return r
}

func (l *lexer) skipWhitespace() {
	for {
		r := l.peek()
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			l.advance()
			continue
		}
		return
	}
}

func (l *lexer) skipLineComment() {
	for l.peek() != '\n' && l.peek() != 0 {
		l.advance()
	}
}

func (l *lexer) skipBlockComment() {
	startLine, startCol := l.line, l.col
	l.advance() // '/'
	l.advance() // '*'
	depth := 1
	for {
		if l.peek() == 0 {
			l.errors = append(l.errors, LexError{
				Pos: source.Position{Line: startLine, Column: startCol, Offset: l.offset},
				Msg: "unterminated block comment",
			})
			return
		}
		if l.peek() == '/' && l.peekAt(1) == '*' {
			l.advance()
			l.advance()
			depth++
			continue
		}
		if l.peek() == '*' && l.peekAt(1) == '/' {
			l.advance()
			l.advance()
			depth--
			if depth == 0 {
				return
			}
			continue
		}
		l.advance()
	}
}

func (l *lexer) emit(tt TokenType, lit string, line, col int) {
	l.tokens = append(l.tokens, Token{
		Type:    tt,
		Literal: lit,
		Pos:     source.Position{Line: line, Column: col, Offset: l.offset - len(lit)},
		Line:    line,
		Col:     col,
	})
}

func (l *lexer) errorAt(line, col int, msg string) {
	l.errors = append(l.errors, LexError{
		Pos: source.Position{Line: line, Column: col, Offset: l.offset},
		Msg: msg,
	})
}

func (l *lexer) readIdentOrKeyword(startLine, startCol int) {
	start := l.pos
	for {
		r := l.peek()
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			break
		}
		l.advance()
	}
	lit := l.src[start:l.pos]
	tt, isKeyword := keywords[lit]
	if !isKeyword {
		tt = TOKEN_IDENT
	}
	l.emit(tt, lit, startLine, startCol)
}

func (l *lexer) readNumber(startLine, startCol int) {
	start := l.pos
	isFloat := false
	
	if l.peek() == '0' && (l.peekAt(1) == 'x' || l.peekAt(1) == 'X') {
		l.advance() // '0'
		l.advance() // 'x'
		for isHexDigit(l.peek()) {
			l.advance()
		}
	} else {
		for isDigit(l.peek()) {
			l.advance()
		}
		if l.peek() == '.' && isDigit(l.peekAt(1)) {
			isFloat = true
			l.advance() // consume '.'
			for isDigit(l.peek()) {
				l.advance()
			}
		}
	}
	lit := l.src[start:l.pos]
	if isFloat {
		l.emit(TOKEN_FLOAT, lit, startLine, startCol)
	} else {
		l.emit(TOKEN_INT, lit, startLine, startCol)
	}
}

func (l *lexer) readString(startLine, startCol int) {
	l.advance() // consume opening '"'
	var buf []byte
	for {
		r := l.peek()
		if r == 0 {
			l.errorAt(startLine, startCol, "unterminated string literal")
			return
		}
		if r == '\n' {
			l.errorAt(l.line, l.col, "newline in string literal")
			return
		}
		if r == '"' {
			l.advance() // consume closing '"'
			break
		}
		if r == '\\' {
			l.advance() // consume '\'
			esc := l.peek()
			l.advance()
			switch esc {
			case 'n':
				buf = append(buf, '\n')
			case 'r':
				buf = append(buf, '\r')
			case 't':
				buf = append(buf, '\t')
			case '\\':
				buf = append(buf, '\\')
			case '"':
				buf = append(buf, '"')
			case '{':
				buf = append(buf, '{')
			case '}':
				buf = append(buf, '}')
			default:
				l.errorAt(l.line, l.col, fmt.Sprintf("unknown escape sequence: \\%c", esc))
				buf = append(buf, byte(esc))
			}
			continue
		}
		// Regular character (including unescaped { and } for interpolation)
		_, sz := utf8.DecodeRuneInString(l.src[l.pos:])
		buf = append(buf, l.src[l.pos:l.pos+sz]...)
		l.advance()
	}
	l.emit(TOKEN_STRING, string(buf), startLine, startCol)
}

func (l *lexer) run() {
	for l.peek() != 0 {
		startLine, startCol := l.line, l.col

		r := l.peek()

		// Whitespace
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			l.skipWhitespace()
			continue
		}

		// Comments
		if r == '/' && l.peekAt(1) == '/' {
			l.skipLineComment()
			continue
		}
		if r == '/' && l.peekAt(1) == '*' {
			l.skipBlockComment()
			continue
		}

		// Identifiers and keywords
		if isIdentStart(r) {
			l.readIdentOrKeyword(startLine, startCol)
			continue
		}

		// Numbers
		if isDigit(r) {
			l.readNumber(startLine, startCol)
			continue
		}

		// Strings
		if r == '"' {
			l.readString(startLine, startCol)
			continue
		}

		// Operators / punctuation — longest match first
		switch {
		case r == '=' && l.peekAt(1) == '=':
			l.advance(); l.advance()
			l.emit(TOKEN_EQEQ, "==", startLine, startCol)
		case r == '!' && l.peekAt(1) == '=':
			l.advance(); l.advance()
			l.emit(TOKEN_NEQ, "!=", startLine, startCol)
		case r == '<' && l.peekAt(1) == '=':
			l.advance(); l.advance()
			l.emit(TOKEN_LTE, "<=", startLine, startCol)
		case r == '<' && l.peekAt(1) == '<':
			l.advance(); l.advance()
			l.emit(TOKEN_LSHIFT, "<<", startLine, startCol)
		case r == '<' && l.peekAt(1) == '-':
			l.advance(); l.advance()
			l.emit(TOKEN_LARROW, "<-", startLine, startCol)
		case r == '>' && l.peekAt(1) == '=':
			l.advance(); l.advance()
			l.emit(TOKEN_GTE, ">=", startLine, startCol)
		case r == '>' && l.peekAt(1) == '>':
			l.advance(); l.advance()
			l.emit(TOKEN_RSHIFT, ">>", startLine, startCol)
		case r == '&' && l.peekAt(1) == '&':
			l.advance(); l.advance()
			l.emit(TOKEN_AND, "&&", startLine, startCol)
		case r == '&':
			l.advance()
			l.emit(TOKEN_AMP, "&", startLine, startCol)
		case r == '|' && l.peekAt(1) == '|':
			l.advance(); l.advance()
			l.emit(TOKEN_OR, "||", startLine, startCol)
		case r == '|' && l.peekAt(1) == '>':
			l.advance(); l.advance()
			l.emit(TOKEN_PIPE_GT, "|>", startLine, startCol)
		case r == '|':
			l.advance()
			l.emit(TOKEN_PIPE, "|", startLine, startCol)
		case r == '^':
			l.advance()
			l.emit(TOKEN_CARET, "^", startLine, startCol)
		case r == '~':
			l.advance()
			l.emit(TOKEN_TILDE, "~", startLine, startCol)
		case r == '=' && l.peekAt(1) == '>':
			l.advance(); l.advance()
			l.emit(TOKEN_ARROW, "=>", startLine, startCol)
		case r == '-' && l.peekAt(1) == '>':
			l.advance(); l.advance()
			l.emit(TOKEN_DASHARROW, "->", startLine, startCol)
		case r == '.' && l.peekAt(1) == '.':
			l.advance(); l.advance()
			l.emit(TOKEN_DOTDOT, "..", startLine, startCol)
		case r == '+':
			l.advance()
			l.emit(TOKEN_PLUS, "+", startLine, startCol)
		case r == '-':
			l.advance()
			l.emit(TOKEN_MINUS, "-", startLine, startCol)
		case r == '*':
			l.advance()
			l.emit(TOKEN_STAR, "*", startLine, startCol)
		case r == '/':
			l.advance()
			l.emit(TOKEN_SLASH, "/", startLine, startCol)
		case r == '%':
			l.advance()
			l.emit(TOKEN_PERCENT, "%", startLine, startCol)
		case r == '=':
			l.advance()
			l.emit(TOKEN_EQ, "=", startLine, startCol)
		case r == '<':
			l.advance()
			l.emit(TOKEN_LT, "<", startLine, startCol)
		case r == '>':
			l.advance()
			l.emit(TOKEN_GT, ">", startLine, startCol)
		case r == '!':
			l.advance()
			l.emit(TOKEN_NOT, "!", startLine, startCol)
		case r == '?':
			l.advance()
			l.emit(TOKEN_QUESTION, "?", startLine, startCol)
		case r == '(':
			l.advance()
			l.emit(TOKEN_LPAREN, "(", startLine, startCol)
		case r == ')':
			l.advance()
			l.emit(TOKEN_RPAREN, ")", startLine, startCol)
		case r == '{':
			l.advance()
			l.emit(TOKEN_LBRACE, "{", startLine, startCol)
		case r == '}':
			l.advance()
			l.emit(TOKEN_RBRACE, "}", startLine, startCol)
		case r == '[':
			l.advance()
			l.emit(TOKEN_LBRACK, "[", startLine, startCol)
		case r == ']':
			l.advance()
			l.emit(TOKEN_RBRACK, "]", startLine, startCol)
		case r == ',':
			l.advance()
			l.emit(TOKEN_COMMA, ",", startLine, startCol)
		case r == ';':
			l.advance()
			l.emit(TOKEN_SEMI, ";", startLine, startCol)
		case r == ':':
			l.advance()
			l.emit(TOKEN_COLON, ":", startLine, startCol)
		case r == '.':
			l.advance()
			l.emit(TOKEN_DOT, ".", startLine, startCol)
		default:
			l.advance()
			l.errorAt(startLine, startCol, fmt.Sprintf("unexpected character: %q", r))
			l.emit(TOKEN_ILLEGAL, string(r), startLine, startCol)
		}
	}
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func isHexDigit(r rune) bool {
	return isDigit(r) || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}
