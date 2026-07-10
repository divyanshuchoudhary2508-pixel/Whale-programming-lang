package vm

import (
	"testing"

	"github.com/whale-lang/whale/internal/interp"
	"github.com/whale-lang/whale/internal/lexer"
	"github.com/whale-lang/whale/internal/parser"
)

// BenchmarkVMHello is a simple benchmark of the VM running
// the hello program.
func BenchmarkVMHello(b *testing.B) {
	src := `
let x = 5;
let y = x + 3;
print(x);
print(y);
`
	toks := lexer.Lex(src).Tokens
	parseRes := parser.Parse(toks)
	chunk, compileErrs := Compile(parseRes.File)
	if len(compileErrs) > 0 {
		b.Fatalf("compile errors: %v", compileErrs)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RunWithOutput(chunk, nil)
	}
}

func BenchmarkInterpHello(b *testing.B) {
	src := `
let x = 5;
let y = x + 3;
print(x);
print(y);
`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		interp.RunFile(src)
	}
}
