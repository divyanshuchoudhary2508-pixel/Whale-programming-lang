package vm

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/whale-lang/whale/internal/lexer"
	"github.com/whale-lang/whale/internal/parser"
)

// compileAndRun is the test helper: lex, parse, compile, run.
// Returns the output and any errors from any stage.
func compileAndRun(t *testing.T, src string) (string, []error) {
	t.Helper()
	toks := lexer.Lex(src).Tokens
	if len(lexer.Lex(src).Errors) > 0 {
		return "", []error{fmt.Errorf("lex errors")}
	}
	parseRes := parser.Parse(toks)
	if len(parseRes.Errors) > 0 {
		return "", []error{fmt.Errorf("parse errors")}
	}
	chunk, compileErrs := Compile(parseRes.File)
	if len(compileErrs) > 0 {
		return "", compileErrs
	}
	var buf bytes.Buffer
	runErrs := RunWithOutput(chunk, &buf)
	return buf.String(), runErrs
}

func TestCompileHello(t *testing.T) {
	src := `
let x = 5;
let y = x + 3;
print(x);
print(y);
`
	out, errs := compileAndRun(t, src)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if out != "5\n8\n" {
		t.Errorf("output = %q, want '5\\n8\\n'", out)
	}
}

func TestCompileArithmetic(t *testing.T) {
	src := `
let a = 10;
let b = 3;
let sum = a + b;
let diff = a - b;
let prod = a * b;
let quot = a / b;
print(sum);
print(diff);
print(prod);
print(quot);
`
	out, errs := compileAndRun(t, src)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	want := "13\n7\n30\n3\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestCompileIfElse(t *testing.T) {
	src := `
let x = 10;
if x > 5 {
    print("big");
} else {
    print("small");
}
`
	out, errs := compileAndRun(t, src)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if out != "big\n" {
		t.Errorf("output = %q, want 'big\\n'", out)
	}
}

func TestCompileIfElseIf(t *testing.T) {
	src := `
let x = 5;
if x > 10 {
    print("big");
} else if x > 0 {
    print("medium");
} else {
    print("small");
}
`
	out, errs := compileAndRun(t, src)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if out != "medium\n" {
		t.Errorf("output = %q, want 'medium\\n'", out)
	}
}

func TestCompileWhile(t *testing.T) {
	src := `
let mut i = 0;
let mut sum = 0;
while i < 5 {
    sum = sum + i;
    i = i + 1;
}
print(sum);
`
	out, errs := compileAndRun(t, src)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	// 0+1+2+3+4 = 10
	if out != "10\n" {
		t.Errorf("output = %q, want '10\\n'", out)
	}
}

func TestCompileNestedScopes(t *testing.T) {
	// Locals declared inside a block should not be visible
	// outside. We test by shadowing: outer x is 10, inner x
	// is 20, and after the block outer x is still 10.
	src := `
let x = 10;
if x > 0 {
    let x = 20;
    print(x);
}
print(x);
`
	out, errs := compileAndRun(t, src)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	want := "20\n10\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestCompileListLiteral(t *testing.T) {
	src := `
let xs = [1, 2, 3, 4, 5];
print(len(xs));
`
	out, errs := compileAndRun(t, src)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if out != "5\n" {
		t.Errorf("output = %q, want '5\\n'", out)
	}
}

func TestCompileStringConcat(t *testing.T) {
	src := `
let greeting = "hello, " + "whale";
print(greeting);
`
	out, errs := compileAndRun(t, src)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if out != "hello, whale\n" {
		t.Errorf("output = %q", out)
	}
}

func TestCompileBuiltinToString(t *testing.T) {
	src := `print(to_string(42));`
	out, errs := compileAndRun(t, src)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if out != "42\n" {
		t.Errorf("output = %q", out)
	}
}

func TestCompileBuiltinContains(t *testing.T) {
	src := `
print(contains("hello world", "world"));
print(contains("hello world", "xyz"));
`
	out, errs := compileAndRun(t, src)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if out != "true\nfalse\n" {
		t.Errorf("output = %q", out)
	}
}

func TestCompileUndefinedName(t *testing.T) {
	src := `print(undefined);`
	_, errs := compileAndRun(t, src)
	if len(errs) == 0 {
		t.Errorf("expected error for undefined name")
	}
}
