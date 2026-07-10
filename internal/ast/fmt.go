package ast

import (
	"fmt"
	"strings"
)

// Format returns a formatted string representation of the file.
func Format(file File) string {
	var out strings.Builder
	for _, stmt := range file.Body {
		out.WriteString(formatStmt(stmt, 0))
		out.WriteString("\n")
	}
	return out.String()
}

func indent(level int) string {
	return strings.Repeat("    ", level)
}

func formatStmt(stmt Stmt, level int) string {
	if stmt == nil {
		return ""
	}
	switch s := stmt.(type) {
	case *ImportStmt:
		if s.Alias != "" && !strings.HasSuffix(s.Path, "/"+s.Alias) && !strings.HasSuffix(s.Path, "/"+s.Alias+".wh") {
			return indent(level) + fmt.Sprintf("import %q as %s;", s.Path, s.Alias)
		}
		return indent(level) + fmt.Sprintf("import %q;", s.Path)
	case *LetStmt:
		mut := ""
		if s.Mutable {
			mut = " mut"
		}
		ann := ""
		if s.TypeAnn != "" {
			ann = ": " + s.TypeAnn
		}
		return indent(level) + fmt.Sprintf("let%s %s%s = %s;", mut, s.Name, ann, formatExpr(s.Value, level))
	case *AssignStmt:
		return indent(level) + fmt.Sprintf("%s = %s;", s.Name, formatExpr(s.Value, level))
	case *AssignFieldStmt:
		return indent(level) + fmt.Sprintf("%s.%s = %s;", formatExpr(s.Object, level), s.Field, formatExpr(s.Value, level))
	case *AssignIndexStmt:
		return indent(level) + fmt.Sprintf("%s[%s] = %s;", formatExpr(s.List, level), formatExpr(s.Index, level), formatExpr(s.Value, level))
	case *ExprStmt:
		return indent(level) + formatExpr(s.Expr, level) + ";"
	case *BlockStmt:
		return formatBlock(s, level)
	case *IfStmt:
		out := indent(level) + fmt.Sprintf("if %s %s", formatExpr(s.Condition, level), formatBlock(s.Then, level))
		if s.Else != nil {
			if elseIf, ok := s.Else.(*IfStmt); ok {
				out += " else " + strings.TrimSpace(formatStmt(elseIf, level))
			} else if elseBlock, ok := s.Else.(*BlockStmt); ok {
				out += " else " + formatBlock(elseBlock, level)
			} else {
				out += " else " + formatStmt(s.Else, level) // fallback
			}
		}
		return out
	case *WhileStmt:
		return indent(level) + fmt.Sprintf("while %s %s", formatExpr(s.Condition, level), formatBlock(s.Body, level))
	case *ForStmt:
		return indent(level) + fmt.Sprintf("for %s in %s %s", s.Variable, formatExpr(s.Iterable, level), formatBlock(s.Body, level))
	case *ArenaStmt:
		return indent(level) + fmt.Sprintf("arena %s", formatBlock(s.Body, level))
	case *ReturnStmt:
		if s.Value != nil {
			return indent(level) + fmt.Sprintf("return %s;", formatExpr(s.Value, level))
		}
		return indent(level) + "return;"
	case *SpawnStmt:
		return indent(level) + fmt.Sprintf("spawn %s;", formatExpr(s.Call, level))
	case *ChanSendStmt:
		return indent(level) + fmt.Sprintf("%s <- %s;", formatExpr(s.Chan, level), formatExpr(s.Value, level))
	case *FnStmt:
		out := indent(level) + "fn " + s.Name
		if len(s.TypeParams) > 0 {
			out += "<" + strings.Join(s.TypeParams, ", ") + ">"
		}
		out += "("
		for i, p := range s.Params {
			if i > 0 {
				out += ", "
			}
			out += p.Name
			if p.Type != "" {
				out += ": " + p.Type
			}
		}
		out += ")"
		if s.ReturnType != "" {
			out += " -> " + s.ReturnType
		}
		out += " " + formatBlock(s.Body, level)
		return out
	case *ExternFnStmt:
		out := indent(level) + "extern fn " + s.Name + "("
		for i, p := range s.Params {
			if i > 0 {
				out += ", "
			}
			out += p.Name
			if p.Type != "" {
				out += ": " + p.Type
			}
		}
		out += ")"
		if s.ReturnType != "" {
			out += " -> " + s.ReturnType
		}
		out += ";"
		return out
	case *StructDecl:
		kw := "struct"
		if s.Packed {
			kw = "packed struct"
		}
		out := indent(level) + kw + " " + s.Name
		if len(s.TypeParams) > 0 {
			out += "<" + strings.Join(s.TypeParams, ", ") + ">"
		}
		out += " {\n"
		for _, f := range s.Fields {
			out += indent(level+1) + fmt.Sprintf("%s: %s,\n", f.Name, f.Type)
		}
		out += indent(level) + "}"
		return out
	case *EnumDecl:
		out := indent(level) + "enum " + s.Name
		if len(s.TypeParams) > 0 {
			out += "<" + strings.Join(s.TypeParams, ", ") + ">"
		}
		out += " {\n"
		for _, v := range s.Variants {
			if v.Type != nil {
				out += indent(level+1) + fmt.Sprintf("%s(%s),\n", v.Name, formatExpr(v.Type, level))
			} else {
				out += indent(level+1) + fmt.Sprintf("%s,\n", v.Name)
			}
		}
		out += indent(level) + "}"
		return out
	}
	return indent(level) + stmt.String()
}

func formatBlock(b *BlockStmt, level int) string {
	if len(b.Body) == 0 {
		return "{}"
	}
	var out strings.Builder
	out.WriteString("{\n")
	for _, s := range b.Body {
		out.WriteString(formatStmt(s, level+1))
		out.WriteString("\n")
	}
	out.WriteString(indent(level) + "}")
	return out.String()
}

func formatExpr(expr Expr, level int) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *IntLit, *FloatLit, *StringLit, *BoolLit, *Ident, *ErrorLit:
		return e.String() // literals format identically
	case *UnaryOp:
		return e.Op + formatExpr(e.Expr, level)
	case *ChanRecvExpr:
		return "<-" + formatExpr(e.Chan, level)
	case *BinaryOp:
		// Specialized formatting for pipelines to make them multi-line if needed
		if e.Op == "|>" {
			return formatExpr(e.Left, level) + " |> " + formatExpr(e.Right, level)
		}
		return formatExpr(e.Left, level) + " " + e.Op + " " + formatExpr(e.Right, level)
	case *CallExpr:
		out := formatExpr(e.Callee, level) + "("
		for i, a := range e.Args {
			if i > 0 {
				out += ", "
			}
			out += formatExpr(a, level)
		}
		out += ")"
		return out
	case *ListLit:
		out := "["
		for i, v := range e.Values {
			if i > 0 {
				out += ", "
			}
			out += formatExpr(v, level)
		}
		out += "]"
		return out
	case *FnLit:
		out := "fn("
		for i, p := range e.Params {
			if i > 0 {
				out += ", "
			}
			out += p.Name
			if p.Type != "" {
				out += ": " + p.Type
			}
		}
		out += ")"
		if e.ReturnType != "" {
			out += " -> " + e.ReturnType
		}
		if e.Body.Expr != nil {
			out += " => " + formatExpr(e.Body.Expr, level)
		} else {
			out += " " + formatBlock(e.Body.Block, level)
		}
		return out
	case *StructLit:
		out := e.Type + "{"
		var parts []string
		for k, v := range e.Fields {
			parts = append(parts, fmt.Sprintf("%s: %s", k, formatExpr(v, level)))
		}
		out += strings.Join(parts, ", ") + "}"
		return out
	case *FieldAccess:
		return formatExpr(e.Expr, level) + "." + e.Field
	case *IndexExpr:
		return formatExpr(e.Expr, level) + "[" + formatExpr(e.Index, level) + "]"
	case *ComptimeExpr:
		return "comptime " + formatExpr(e.Expr, level)
	case *TryExpr:
		return formatExpr(e.Expr, level) + "?"
	case *MatchExpr:
		out := "match " + formatExpr(e.Expr, level) + " {\n"
		for _, a := range e.Arms {
			out += indent(level+1)
			if a.Variant == "_" {
				out += "_"
			} else {
				out += a.Variant
				if a.Binding != "" {
					out += "(" + a.Binding + ")"
				}
			}
			out += " => " + formatExpr(a.Body, level+1) + ",\n"
		}
		out += indent(level) + "}"
		return out
	}
	return expr.String()
}
