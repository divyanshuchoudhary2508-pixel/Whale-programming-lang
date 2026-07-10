package lexer

import (
	"strings"
	"testing"
)

func TestBasicTokens(t *testing.T) {
	src := `let x = 5;`
	res := Lex(src)
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	expected := []TokenType{TOKEN_LET, TOKEN_IDENT, TOKEN_EQ, TOKEN_INT, TOKEN_SEMI, TOKEN_EOF}
	if len(res.Tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expected), len(res.Tokens), res.Tokens)
	}
	for i, tt := range expected {
		if res.Tokens[i].Type != tt {
			t.Errorf("token %d: expected %s, got %s", i, tt, res.Tokens[i].Type)
		}
	}
}

func TestPipeOperator(t *testing.T) {
	src := `x |> filter`
	res := Lex(src)
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	types := make([]TokenType, len(res.Tokens))
	for i, tok := range res.Tokens {
		types[i] = tok.Type
	}
	if types[1] != TOKEN_PIPE_GT {
		t.Errorf("expected |> token, got %s", types[1])
	}
}

func TestArrowOperators(t *testing.T) {
	src := `x => y -> z`
	res := Lex(src)
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if res.Tokens[1].Type != TOKEN_ARROW {
		t.Errorf("expected =>, got %s", res.Tokens[1].Type)
	}
	if res.Tokens[3].Type != TOKEN_DASHARROW {
		t.Errorf("expected ->, got %s", res.Tokens[3].Type)
	}
}

func TestStringLiterals(t *testing.T) {
	src := `"hello\nworld"`
	res := Lex(src)
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if res.Tokens[0].Literal != "hello\nworld" {
		t.Errorf("expected string with newline, got %q", res.Tokens[0].Literal)
	}
}

func TestFloatLiteral(t *testing.T) {
	src := `3.14`
	res := Lex(src)
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if res.Tokens[0].Type != TOKEN_FLOAT {
		t.Errorf("expected FLOAT, got %s", res.Tokens[0].Type)
	}
	if res.Tokens[0].Literal != "3.14" {
		t.Errorf("expected 3.14, got %q", res.Tokens[0].Literal)
	}
}

func TestBlockComment(t *testing.T) {
	src := `/* this is a comment */ let x = 5;`
	res := Lex(src)
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if res.Tokens[0].Type != TOKEN_LET {
		t.Errorf("expected LET after comment, got %s", res.Tokens[0].Type)
	}
}

func TestLineComment(t *testing.T) {
	src := "// this is a comment\nlet x = 5;"
	res := Lex(src)
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if res.Tokens[0].Type != TOKEN_LET {
		t.Errorf("expected LET after comment, got %s", res.Tokens[0].Type)
	}
}

func TestKeywords(t *testing.T) {
	keywords := []string{"let", "mut", "fn", "return", "if", "else", "while", "for", "in", "struct", "stream", "true", "false"}
	src := strings.Join(keywords, " ")
	res := Lex(src)
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	expected := []TokenType{TOKEN_LET, TOKEN_MUT, TOKEN_FN, TOKEN_RETURN, TOKEN_IF, TOKEN_ELSE, TOKEN_WHILE, TOKEN_FOR, TOKEN_IN, TOKEN_STRUCT, TOKEN_STREAM, TOKEN_TRUE, TOKEN_FALSE}
	for i, tt := range expected {
		if res.Tokens[i].Type != tt {
			t.Errorf("token %d: expected %s, got %s", i, tt, res.Tokens[i].Type)
		}
	}
}

func TestMultiCharOperators(t *testing.T) {
	tests := []struct {
		src  string
		want TokenType
	}{
		{"==", TOKEN_EQEQ},
		{"!=", TOKEN_NEQ},
		{"<=", TOKEN_LTE},
		{">=", TOKEN_GTE},
		{"&&", TOKEN_AND},
		{"||", TOKEN_OR},
		{"|>", TOKEN_PIPE_GT},
		{"=>", TOKEN_ARROW},
		{"->", TOKEN_DASHARROW},
		{"..", TOKEN_DOTDOT},
	}
	for _, tc := range tests {
		res := Lex(tc.src)
		if len(res.Errors) != 0 {
			t.Errorf("%q: unexpected errors: %v", tc.src, res.Errors)
			continue
		}
		if res.Tokens[0].Type != tc.want {
			t.Errorf("%q: expected %s, got %s", tc.src, tc.want, res.Tokens[0].Type)
		}
	}
}

func TestLineTracking(t *testing.T) {
	src := "let x = 5;\nlet y = 10;"
	res := Lex(src)
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	// "let" on line 2 should have Line=2
	letTok2 := res.Tokens[5] // let y = 10; starts at index 5
	if letTok2.Line != 2 {
		t.Errorf("expected line 2, got %d", letTok2.Line)
	}
}

func TestUnterminatedString(t *testing.T) {
	src := `"hello`
	res := Lex(src)
	if len(res.Errors) == 0 {
		t.Error("expected error for unterminated string")
	}
}

func TestNestedBlockComment(t *testing.T) {
	src := `/* outer /* inner */ still_outer */ x`
	res := Lex(src)
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if res.Tokens[0].Type != TOKEN_IDENT || res.Tokens[0].Literal != "x" {
		t.Errorf("expected ident 'x' after nested comment, got %v", res.Tokens[0])
	}
}
