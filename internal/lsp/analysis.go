package lsp

import (
	"fmt"
	"strings"

	"github.com/whale-lang/whale/internal/ast"
	"github.com/whale-lang/whale/internal/lexer"
	"github.com/whale-lang/whale/internal/parser"
	"github.com/whale-lang/whale/internal/types"
)

// documentState holds the latest analysis result for a single open document.
type documentState struct {
	source      string
	file        *ast.File
	typeResult  types.Result
	parseErrors []parser.ParseError
}

// analyze lexes, parses, and type-checks source, returning a documentState.
func analyze(source string) documentState {
	state := documentState{source: source}

	lexResult := lexer.Lex(source)
	if len(lexResult.Errors) > 0 {
		// Lex errors — still try to parse what we have
		_ = lexResult.Errors
	}

	parseResult := parser.Parse(lexResult.Tokens)
	state.parseErrors = parseResult.Errors
	state.file = &parseResult.File

	state.typeResult = types.Check(parseResult.File)
	return state
}

// diagnosticsFor converts parse + type errors from a documentState into LSP Diagnostics.
func diagnosticsFor(state documentState) []Diagnostic {
	var diags []Diagnostic

	for _, e := range state.parseErrors {
		diags = append(diags, Diagnostic{
			Range: Range{
				Start: Position{Line: e.Pos.Line - 1, Character: e.Pos.Column - 1},
				End:   Position{Line: e.Pos.Line - 1, Character: e.Pos.Column},
			},
			Severity: SeverityError,
			Source:   "whale-parser",
			Message:  e.Msg,
		})
	}

	for _, e := range state.typeResult.Errors {
		diags = append(diags, Diagnostic{
			Range: Range{
				Start: Position{Line: e.Pos.Line - 1, Character: e.Pos.Col - 1},
				End:   Position{Line: e.Pos.Line - 1, Character: e.Pos.Col + 20},
			},
			Severity: SeverityError,
			Source:   "whale-type-checker",
			Message:  e.Msg,
		})
	}

	return diags
}

// hoverType looks up the type of the expression at (line, char) in the document.
// line and char are 0-based (LSP convention).
func hoverType(state documentState, line, char int) string {
	// Convert from 0-based LSP coords to 1-based Whale AST coords
	wLine := line + 1
	wCol := char + 1

	// Walk all expression types and find one whose position matches
	for expr, ty := range state.typeResult.Types {
		pos := exprPos(expr)
		if pos.Line == wLine && pos.Col <= wCol && wCol <= pos.Col+exprLen(expr) {
			return fmt.Sprintf("**%s**\n\n*type:* `%s`", exprText(state.source, pos), ty.String())
		}
	}

	return ""
}

// completionItems returns context-aware completions: keywords + builtins + in-scope vars.
func completionItems(state documentState, line, char int) []CompletionItem {
	var items []CompletionItem

	// Keywords
	keywords := []string{"fn", "let", "let mut", "if", "else", "while", "for", "return", "struct", "match"}
	for _, kw := range keywords {
		items = append(items, CompletionItem{
			Label:  kw,
			Kind:   CompletionKindKeyword,
			Detail: "keyword",
		})
	}

	// Builtins
	builtins := map[string]string{
		"print":      "print(value) -> ()",
		"len":        "len(list) -> int",
		"to_string":  "to_string(value) -> string",
		"contains":   "contains(str, substr) -> bool",
		"read_file":  "read_file(path) -> string",
		"write_file": "write_file(path, content) -> ()",
		"lines":      "lines(str) -> [string]",
		"split":      "split(str, delim) -> [string]",
		"trim":       "trim(str) -> string",
		"replace":    "replace(str, old, new) -> string",
		"to_lower":   "to_lower(str) -> string",
		"to_upper":   "to_upper(str) -> string",
		"abs":        "abs(x) -> float",
		"max":        "max(a, b) -> float",
		"min":        "min(a, b) -> float",
		"parse_int":  "parse_int(str) -> int",
		"type_of":    "type_of(value) -> string",
		"push":       "push(list, elem) -> [T]",
		"pop":        "pop(list) -> T",
		"stream":     "stream(list) -> stream<T>",
		"map":        "map(stream, fn) -> stream<U>",
		"filter":     "filter(stream, fn) -> stream<T>",
		"collect":    "collect(stream) -> [T]",
	}

	for name, sig := range builtins {
		items = append(items, CompletionItem{
			Label:      name,
			Kind:       CompletionKindFunction,
			Detail:     sig,
			InsertText: name,
		})
	}

	// In-scope variables: walk the AST and collect all LetStmt names declared before cursor
	if state.file != nil {
		vars := collectVarsBeforeLine(state.file, line+1)
		for varName, ty := range vars {
			items = append(items, CompletionItem{
				Label:  varName,
				Kind:   CompletionKindVariable,
				Detail: ty,
			})
		}
	}

	return items
}

// collectVarsBeforeLine returns variable names declared before `line` (1-based).
func collectVarsBeforeLine(file *ast.File, line int) map[string]string {
	vars := map[string]string{}
	for _, stmt := range file.Body {
		collectVarsFromStmt(stmt, line, vars)
	}
	return vars
}

func collectVarsFromStmt(stmt ast.Stmt, line int, vars map[string]string) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		if s.Pos.Line < line {
			ty := s.TypeAnn
			if ty == "" {
				ty = "?"
			}
			vars[s.Name] = ty
		}
	case *ast.FnStmt:
		if s.Pos.Line < line {
			vars[s.Name] = "fn"
			// Also collect params if we're inside this function
			if s.Body != nil {
				for _, stmt := range s.Body.Body {
					collectVarsFromStmt(stmt, line, vars)
				}
			}
		}
	case *ast.IfStmt:
		if s.Then != nil {
			for _, sub := range s.Then.Body {
				collectVarsFromStmt(sub, line, vars)
			}
		}
	case *ast.WhileStmt:
		if s.Body != nil {
			for _, sub := range s.Body.Body {
				collectVarsFromStmt(sub, line, vars)
			}
		}
	}
}

// ============================================================================
// Helpers for position resolution
// ============================================================================

func exprPos(expr ast.Expr) ast.Position {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Pos
	case *ast.IntLit:
		return e.Pos
	case *ast.FloatLit:
		return e.Pos
	case *ast.StringLit:
		return e.Pos
	case *ast.BoolLit:
		return e.Pos
	case *ast.BinaryOp:
		return e.Pos
	case *ast.CallExpr:
		return e.Pos
	case *ast.ListLit:
		return e.Pos
	}
	return ast.Position{}
}

func exprLen(expr ast.Expr) int {
	switch e := expr.(type) {
	case *ast.Ident:
		return len(e.Name)
	case *ast.StringLit:
		return len(e.Value) + 2
	default:
		return 10 // generous default
	}
}

func exprText(source string, pos ast.Position) string {
	lines := strings.Split(source, "\n")
	if pos.Line < 1 || pos.Line > len(lines) {
		return ""
	}
	line := lines[pos.Line-1]
	col := pos.Col - 1
	if col < 0 || col >= len(line) {
		return ""
	}
	// Extract the word at col
	start, end := col, col
	for end < len(line) && isIdentChar(line[end]) {
		end++
	}
	for start > 0 && isIdentChar(line[start-1]) {
		start--
	}
	return line[start:end]
}

func isIdentChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}
