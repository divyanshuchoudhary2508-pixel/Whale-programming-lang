package vm

import (
	"fmt"
	"io"

	"github.com/whale-lang/whale/internal/lexer"
	"github.com/whale-lang/whale/internal/parser"
)

func runSourceImpl(src string, out io.Writer) []error {
	toks := lexer.Lex(src).Tokens
	if len(lexer.Lex(src).Errors) > 0 {
		errs := make([]error, len(lexer.Lex(src).Errors))
		for i, e := range lexer.Lex(src).Errors {
			errs[i] = fmt.Errorf("lex: %s", e.Error())
		}
		return errs
	}
	parseRes := parser.Parse(toks)
	if len(parseRes.Errors) > 0 {
		errs := make([]error, len(parseRes.Errors))
		for i, e := range parseRes.Errors {
			errs[i] = fmt.Errorf("parse: %s", e.Error())
		}
		return errs
	}
	chunk, compileErrs := Compile(parseRes.File)
	if len(compileErrs) > 0 {
		return compileErrs
	}
	return RunWithOutput(chunk, out)
}
