package interp

import (
	"strings"
	"testing"
)

// runProgram is the test helper: lexes, parses, and runs a source string.
// Returns (stdout, errors).
func runProgram(src string) (string, []string) {
	return RunFile(src)
}

// captureOutput returns only what was collected in the output buffer
// (not what was printed to stdout). For tests, use RunFile directly.
func captureOutput(src string) string {
	out, errs := RunFile(src)
	if len(errs) > 0 {
		return ""
	}
	return out
}

func TestHello(t *testing.T) {
	out, errs := runProgram(`print("hello, whale!");`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "hello, whale!\n" {
		t.Errorf("output = %q, want %q", out, "hello, whale!\n")
	}
}

func TestArithmetic(t *testing.T) {
	src := `
let a = 10;
let b = 3;
print(a + b);
print(a - b);
print(a * b);
print(a / b);
print(a % b);
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := "13\n7\n30\n3\n1\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestStringConcat(t *testing.T) {
	src := `let s = "hello" + ", " + "world!"; print(s);`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "hello, world!\n" {
		t.Errorf("output = %q", out)
	}
}

func TestBooleanOps(t *testing.T) {
	src := `
print(true && false);
print(true || false);
print(!true);
print(1 < 2);
print(2 > 1);
print(1 <= 1);
print(2 >= 2);
print(1 == 1);
print(1 != 2);
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	expected := []string{"false", "true", "false", "true", "true", "true", "true", "true", "true"}
	if len(lines) != len(expected) {
		t.Fatalf("got %d lines, want %d: %v", len(lines), len(expected), lines)
	}
	for i, l := range lines {
		if l != expected[i] {
			t.Errorf("line %d: got %q, want %q", i, l, expected[i])
		}
	}
}

func TestIfElse(t *testing.T) {
	src := `
let x = 5;
if x > 3 {
    print("big");
} else {
    print("small");
}
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "big\n" {
		t.Errorf("output = %q", out)
	}
}

func TestWhileLoop(t *testing.T) {
	src := `
let mut i = 0;
while i < 5 {
    i = i + 1;
}
print(i);
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "5\n" {
		t.Errorf("output = %q", out)
	}
}

func TestForLoop(t *testing.T) {
	src := `
let xs = [1, 2, 3];
let mut sum = 0;
for x in xs {
    sum = sum + x;
}
print(sum);
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "6\n" {
		t.Errorf("output = %q", out)
	}
}

func TestFunctionDecl(t *testing.T) {
	src := `
fn add(a: int, b: int) -> int {
    return a + b;
}
print(add(3, 4));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "7\n" {
		t.Errorf("output = %q", out)
	}
}

func TestRecursion(t *testing.T) {
	src := `
fn fact(n: int) -> int {
    if n <= 1 { return 1; }
    return n * fact(n - 1);
}
print(fact(5));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "120\n" {
		t.Errorf("output = %q", out)
	}
}

func TestClosure(t *testing.T) {
	src := `
let factor = 3;
let mult = fn(x: int) -> int { return x * factor; };
print(mult(5));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "15\n" {
		t.Errorf("output = %q", out)
	}
}

func TestLambda(t *testing.T) {
	src := `
let double = x => x * 2;
print(double(7));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "14\n" {
		t.Errorf("output = %q", out)
	}
}

func TestListLiteral(t *testing.T) {
	src := `
let xs = [10, 20, 30];
print(xs[0]);
print(xs[1]);
print(xs[2]);
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "10\n20\n30\n" {
		t.Errorf("output = %q", out)
	}
}

func TestListConcat(t *testing.T) {
	src := `
let a = [1, 2];
let b = [3, 4];
let c = a + b;
print(to_string(c));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "[1, 2, 3, 4]\n" {
		t.Errorf("output = %q", out)
	}
}

func TestLenBuiltin(t *testing.T) {
	src := `
let xs = [1, 2, 3, 4, 5];
print(len(xs));
print(len("hello"));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "5\n5\n" {
		t.Errorf("output = %q", out)
	}
}

func TestContains(t *testing.T) {
	src := `
print(contains("hello world", "world"));
print(contains("hello", "xyz"));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "true\nfalse\n" {
		t.Errorf("output = %q", out)
	}
}

func TestStreamFilter(t *testing.T) {
	src := `
let result = [1, 2, 3, 4, 5, 6]
    |> filter(n => n % 2 == 0)
    |> collect;
print(to_string(result));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "[2, 4, 6]\n" {
		t.Errorf("output = %q", out)
	}
}

func TestStreamMap(t *testing.T) {
	src := `
let result = [1, 2, 3, 4, 5]
    |> map(n => n * 10)
    |> collect;
print(to_string(result));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "[10, 20, 30, 40, 50]\n" {
		t.Errorf("output = %q", out)
	}
}

func TestStreamChain(t *testing.T) {
	src := `
let result = [1, 2, 3, 4, 5, 6, 7, 8]
    |> filter(n => n % 2 == 0)
    |> map(n => n * 10)
    |> collect;
print(to_string(result));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "[20, 40, 60, 80]\n" {
		t.Errorf("output = %q", out)
	}
}

func TestStreamTake(t *testing.T) {
	src := `
let result = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    |> take(3)
    |> collect;
print(to_string(result));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "[1, 2, 3]\n" {
		t.Errorf("output = %q", out)
	}
}

func TestStreamSkip(t *testing.T) {
	src := `
let result = [1, 2, 3, 4, 5]
    |> skip(2)
    |> collect;
print(to_string(result));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "[3, 4, 5]\n" {
		t.Errorf("output = %q", out)
	}
}

func TestStreamFold(t *testing.T) {
	src := `
let sum = [1, 2, 3, 4, 5]
    |> fold(0, (acc, n) => acc + n);
print(sum);
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "15\n" {
		t.Errorf("output = %q", out)
	}
}

func TestStreamForEach(t *testing.T) {
	src := `
[1, 2, 3]
    |> filter(n => n > 1)
    |> for_each(n => print(n));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "2\n3\n" {
		t.Errorf("output = %q", out)
	}
}

func TestStructs(t *testing.T) {
	src := `
struct Point {
    x: int,
    y: int
}
let p = Point { x: 3, y: 4 };
print(p.x);
print(p.y);
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "3\n4\n" {
		t.Errorf("output = %q", out)
	}
}

func TestLinesBuiltin(t *testing.T) {
	src := `
let parts = lines("a\nb\nc");
print(len(parts));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "3\n" {
		t.Errorf("output = %q", out)
	}
}

func TestUnaryMinus(t *testing.T) {
	src := `
let x = -5;
let y = -3.14;
print(x);
print(y);
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "-5\n-3.14\n" {
		t.Errorf("output = %q", out)
	}
}

func TestImmutableError(t *testing.T) {
	src := `
let x = 5;
x = 10;
`
	_, errs := runProgram(src)
	if len(errs) == 0 {
		t.Error("expected error for immutable reassignment")
	}
}

func TestFibonacci(t *testing.T) {
	src := `
fn fib(n: int) -> int {
    if n <= 1 { return n; }
    return fib(n - 1) + fib(n - 2);
}
print(fib(0));
print(fib(1));
print(fib(7));
print(fib(10));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "0\n1\n13\n55\n" {
		t.Errorf("output = %q", out)
	}
}

func TestHigherOrderFunctions(t *testing.T) {
	src := `
fn apply(f, x: int) -> int {
    return f(x);
}
let triple = n => n * 3;
print(apply(triple, 5));
print(apply(n => n + 100, 7));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "15\n107\n" {
		t.Errorf("output = %q", out)
	}
}

func TestTypeOf(t *testing.T) {
	src := `
print(type_of(42));
print(type_of(3.14));
print(type_of("hello"));
print(type_of(true));
print(type_of([1, 2, 3]));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "int\nfloat\nstring\nbool\nlist\n" {
		t.Errorf("output = %q", out)
	}
}

func TestToString(t *testing.T) {
	src := `
print(to_string(42));
print(to_string(true));
print(to_string([1, 2, 3]));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "42\ntrue\n[1, 2, 3]\n" {
		t.Errorf("output = %q", out)
	}
}

func TestElseIf(t *testing.T) {
	src := `
fn grade(score: int) -> string {
    if score >= 90 {
        return "A";
    } else if score >= 80 {
        return "B";
    } else if score >= 70 {
        return "C";
    } else {
        return "F";
    }
}
print(grade(95));
print(grade(82));
print(grade(74));
print(grade(50));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "A\nB\nC\nF\n" {
		t.Errorf("output = %q", out)
	}
}

func TestStreamFromVariable(t *testing.T) {
	src := `
let raw = stream([10, 20, 30, 40, 50]);
let result = raw
    |> filter(n => n >= 30)
    |> map(n => n * 2)
    |> collect;
print(to_string(result));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "[60, 80, 100]\n" {
		t.Errorf("output = %q", out)
	}
}

func TestEmptyStream(t *testing.T) {
	src := `
let result = []
    |> filter(n => true)
    |> collect;
print(len(result));
`
	out, errs := runProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if out != "0\n" {
		t.Errorf("output = %q", out)
	}
}
