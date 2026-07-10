// Package source holds shared source-location types used across
// compiler passes (lexer, parser, type checker).
package source

import "fmt"

// Position is a 1-indexed line/column pair in a source file.
type Position struct {
	Line   int
	Column int
	Offset int // byte offset into source
}

// String renders a position in the form "line:col" for error messages.
func (p Position) String() string {
	if p.Line < 1 {
		return "1:1"
	}
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}
