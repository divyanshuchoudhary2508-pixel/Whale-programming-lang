package lexer

import "github.com/whale-lang/whale/internal/source"

// TokenType identifies the kind of token produced by the lexer.
type TokenType int

const (
	// Special / sentinel
	TOKEN_EOF TokenType = iota
	TOKEN_ILLEGAL

	// Literals
	TOKEN_IDENT
	TOKEN_INT
	TOKEN_FLOAT
	TOKEN_STRING

	// Operators
	TOKEN_PLUS    // +
	TOKEN_MINUS   // -
	TOKEN_STAR    // *
	TOKEN_SLASH   // /
	TOKEN_PERCENT // %
	TOKEN_EQ      // =
	TOKEN_EQEQ    // ==
	TOKEN_NEQ     // !=
	TOKEN_LT      // <
	TOKEN_GT      // >
	TOKEN_LTE     // <=
	TOKEN_GTE     // >=
	TOKEN_AND     // &&
	TOKEN_OR      // ||
	TOKEN_NOT     // !
	TOKEN_PIPE_GT // |>   <-- the marquee operator
	TOKEN_ARROW   // =>
	TOKEN_DASHARROW // ->
	TOKEN_LARROW    // <-
	TOKEN_QUESTION  // ?

	// Punctuation
	TOKEN_LPAREN // (
	TOKEN_RPAREN // )
	TOKEN_LBRACE // {
	TOKEN_RBRACE // }
	TOKEN_LBRACK // [
	TOKEN_RBRACK // ]
	TOKEN_COMMA  // ,
	TOKEN_SEMI   // ;
	TOKEN_COLON  // :
	TOKEN_DOT    // .
	TOKEN_DOTDOT // ..

	// Keywords
	TOKEN_LET
	TOKEN_MUT
	TOKEN_FN
	TOKEN_RETURN
	TOKEN_IF
	TOKEN_ELSE
	TOKEN_WHILE
	TOKEN_FOR
	TOKEN_IN
	TOKEN_STRUCT
	TOKEN_STREAM
	TOKEN_TRUE
	TOKEN_FALSE
	TOKEN_USE
	TOKEN_MOD
	TOKEN_PUB
	TOKEN_IMPORT
	TOKEN_AS
	TOKEN_ENUM
	TOKEN_MATCH
	TOKEN_SPAWN
	TOKEN_CHAN
	TOKEN_COMPTIME
	TOKEN_ERROR
	TOKEN_ARENA
	TOKEN_PACKED
	TOKEN_EXTERN
)

// keywords maps reserved identifier strings to their token types.
var keywords = map[string]TokenType{
	"let":    TOKEN_LET,
	"mut":    TOKEN_MUT,
	"fn":     TOKEN_FN,
	"return": TOKEN_RETURN,
	"if":     TOKEN_IF,
	"else":   TOKEN_ELSE,
	"while":  TOKEN_WHILE,
	"for":    TOKEN_FOR,
	"in":     TOKEN_IN,
	"struct": TOKEN_STRUCT,
	"stream": TOKEN_STREAM,
	"true":   TOKEN_TRUE,
	"false":  TOKEN_FALSE,
	"use":    TOKEN_USE,
	"mod":    TOKEN_MOD,
	"pub":    TOKEN_PUB,
	"import": TOKEN_IMPORT,
	"as":     TOKEN_AS,
	"enum":   TOKEN_ENUM,
	"match":  TOKEN_MATCH,
	"spawn":  TOKEN_SPAWN,
	"chan":   TOKEN_CHAN,
	"comptime": TOKEN_COMPTIME,
	"error":  TOKEN_ERROR,
	"arena":  TOKEN_ARENA,
	"packed": TOKEN_PACKED,
	"extern": TOKEN_EXTERN,
}

// String returns a human-readable name for the token type.
func (t TokenType) String() string {
	switch t {
	case TOKEN_EOF:
		return "EOF"
	case TOKEN_ILLEGAL:
		return "ILLEGAL"
	case TOKEN_IDENT:
		return "IDENT"
	case TOKEN_INT:
		return "INT"
	case TOKEN_FLOAT:
		return "FLOAT"
	case TOKEN_STRING:
		return "STRING"
	case TOKEN_PLUS:
		return "+"
	case TOKEN_MINUS:
		return "-"
	case TOKEN_STAR:
		return "*"
	case TOKEN_SLASH:
		return "/"
	case TOKEN_PERCENT:
		return "%"
	case TOKEN_EQ:
		return "="
	case TOKEN_EQEQ:
		return "=="
	case TOKEN_NEQ:
		return "!="
	case TOKEN_LT:
		return "<"
	case TOKEN_GT:
		return ">"
	case TOKEN_LTE:
		return "<="
	case TOKEN_GTE:
		return ">="
	case TOKEN_AND:
		return "&&"
	case TOKEN_OR:
		return "||"
	case TOKEN_NOT:
		return "!"
	case TOKEN_PIPE_GT:
		return "|>"
	case TOKEN_ARROW:
		return "=>"
	case TOKEN_DASHARROW:
		return "->"
	case TOKEN_LARROW:
		return "<-"
	case TOKEN_QUESTION:
		return "?"
	case TOKEN_LPAREN:
		return "("
	case TOKEN_RPAREN:
		return ")"
	case TOKEN_LBRACE:
		return "{"
	case TOKEN_RBRACE:
		return "}"
	case TOKEN_LBRACK:
		return "["
	case TOKEN_RBRACK:
		return "]"
	case TOKEN_COMMA:
		return ","
	case TOKEN_SEMI:
		return ";"
	case TOKEN_COLON:
		return ":"
	case TOKEN_DOT:
		return "."
	case TOKEN_DOTDOT:
		return ".."
	case TOKEN_LET:
		return "let"
	case TOKEN_MUT:
		return "mut"
	case TOKEN_FN:
		return "fn"
	case TOKEN_RETURN:
		return "return"
	case TOKEN_IF:
		return "if"
	case TOKEN_ELSE:
		return "else"
	case TOKEN_WHILE:
		return "while"
	case TOKEN_FOR:
		return "for"
	case TOKEN_IN:
		return "in"
	case TOKEN_STRUCT:
		return "struct"
	case TOKEN_STREAM:
		return "stream"
	case TOKEN_TRUE:
		return "true"
	case TOKEN_FALSE:
		return "false"
	case TOKEN_USE:
		return "use"
	case TOKEN_MOD:
		return "mod"
	case TOKEN_PUB:
		return "pub"
	case TOKEN_IMPORT:
		return "import"
	case TOKEN_AS:
		return "as"
	case TOKEN_ENUM:
		return "enum"
	case TOKEN_MATCH:
		return "match"
	case TOKEN_SPAWN:
		return "spawn"
	case TOKEN_CHAN:
		return "chan"
	case TOKEN_COMPTIME:
		return "comptime"
	case TOKEN_ERROR:
		return "error"
	case TOKEN_ARENA:
		return "arena"
	case TOKEN_PACKED:
		return "packed"
	case TOKEN_EXTERN:
		return "extern"
	default:
		return "UNKNOWN"
	}
}

// Token is a single lexeme produced by the lexer.
type Token struct {
	Type    TokenType
	Literal string // the exact text matched
	Pos     source.Position
	Line    int
	Col     int
}
