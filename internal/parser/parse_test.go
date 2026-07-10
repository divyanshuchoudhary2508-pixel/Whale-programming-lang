package parser

import (
	"testing"

	"github.com/whale-lang/whale/internal/lexer"
)

func tokenize(t *testing.T, src string) []lexer.Token {
	t.Helper()
	res := lexer.Lex(src)
	if len(res.Errors) != 0 {
		t.Fatalf("lex errors: %v", res.Errors)
	}
	return res.Tokens
}

func TestParseLetStatement(t *testing.T) {
	src := `let x = 5;`
	result := Parse(tokenize(t, src))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if len(result.File.Body) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(result.File.Body))
	}
}

func TestParseLetMut(t *testing.T) {
	result := Parse(tokenize(t, `let mut count = 0;`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseBinaryOps(t *testing.T) {
	tests := []string{
		"let x = 1 + 2;",
		"let x = 3 * 4 + 5;",
		"let x = (1 + 2) * 3;",
		"let x = a == b;",
		"let x = a != b;",
		"let x = a < b;",
	}
	for _, src := range tests {
		result := Parse(tokenize(t, src))
		if len(result.Errors) != 0 {
			t.Errorf("%q: unexpected errors: %v", src, result.Errors)
		}
	}
}

func TestParseIfStatement(t *testing.T) {
	result := Parse(tokenize(t, `if x > 0 { print(x); } else { print(0); }`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseWhileStatement(t *testing.T) {
	result := Parse(tokenize(t, `while i < 10 { i = i + 1; }`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseForStatement(t *testing.T) {
	result := Parse(tokenize(t, `for x in items { print(x); }`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseFunctionDecl(t *testing.T) {
	src := `fn add(a: int, b: int) -> int { return a + b; }`
	result := Parse(tokenize(t, src))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseFunctionNoReturn(t *testing.T) {
	result := Parse(tokenize(t, `fn greet() { print("hello"); }`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseLambda(t *testing.T) {
	result := Parse(tokenize(t, `let f = x => x * 2;`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseMultiParamLambda(t *testing.T) {
	result := Parse(tokenize(t, `let f = (a, b) => a + b;`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParsePipeOperator(t *testing.T) {
	src := `let r = [1, 2, 3] |> filter(x => x > 1) |> collect;`
	result := Parse(tokenize(t, src))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseListLiteral(t *testing.T) {
	result := Parse(tokenize(t, `let xs = [1, 2, 3, 4, 5];`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseEmptyList(t *testing.T) {
	result := Parse(tokenize(t, `let xs = [];`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseStructDecl(t *testing.T) {
	result := Parse(tokenize(t, `struct Point { x: int, y: int }`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseStructLit(t *testing.T) {
	result := Parse(tokenize(t, `let p = Point { x: 3, y: 4 };`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseFieldAccess(t *testing.T) {
	result := Parse(tokenize(t, `let x = p.x;`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseUnaryMinus(t *testing.T) {
	result := Parse(tokenize(t, `let x = -5;`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseUnaryNot(t *testing.T) {
	result := Parse(tokenize(t, `let x = !true;`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseMultipleStatements(t *testing.T) {
	src := `
let x = 5;
let y = 10;
print(x + y);
`
	result := Parse(tokenize(t, src))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if len(result.File.Body) != 3 {
		t.Errorf("expected 3 statements, got %d", len(result.File.Body))
	}
}

func TestParseRecursiveFunction(t *testing.T) {
	src := `fn fact(n: int) -> int {
    if n <= 1 { return 1; }
    return n * fact(n - 1);
}`
	result := Parse(tokenize(t, src))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseChainedCalls(t *testing.T) {
	result := Parse(tokenize(t, `print(add(1, 2));`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseIndexExpr(t *testing.T) {
	result := Parse(tokenize(t, `let x = arr[0];`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}

func TestParseFnLit(t *testing.T) {
	result := Parse(tokenize(t, `let f = fn(x: int) -> int { return x * 2; };`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
}
