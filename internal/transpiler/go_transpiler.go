package transpiler

import (
	"fmt"
	"strings"

	"github.com/whale-lang/whale/internal/ast"
)

// Transpile converts a Whale AST into Go source code.
func Transpile(file ast.File) (string, error) {
	var out strings.Builder

	// Header
	out.WriteString("package main\n\n")
	out.WriteString("import (\n\t\"fmt\"\n)\n\n")

	// Globals and functions
	for _, stmt := range file.Body {
		err := transpileStmt(stmt, &out, 0)
		if err != nil {
			return "", err
		}
		out.WriteString("\n")
	}

	return out.String(), nil
}

func indent(level int) string {
	return strings.Repeat("\t", level)
}

func mapType(t string) string {
	switch t {
	case "int":
		return "int64"
	case "float":
		return "float64"
	case "string":
		return "string"
	case "bool":
		return "bool"
	case "":
		return "interface{}" // fallback for type inference if missing
	default:
		return t // struct types, etc.
	}
}

func transpileStmt(stmt ast.Stmt, out *strings.Builder, level int) error {
	if stmt == nil {
		return nil
	}
	switch s := stmt.(type) {
	case *ast.FnStmt:
		// Map `main` to `main` and other functions to their names
		name := s.Name
		
		out.WriteString(indent(level))
		out.WriteString(fmt.Sprintf("func %s(", name))
		for i, p := range s.Params {
			if i > 0 {
				out.WriteString(", ")
			}
			out.WriteString(fmt.Sprintf("%s %s", p.Name, mapType(p.Type)))
		}
		out.WriteString(") ")
		if s.ReturnType != "" {
			out.WriteString(mapType(s.ReturnType) + " ")
		}
		out.WriteString("{\n")
		
		// Body
		for _, bStmt := range s.Body.Body {
			err := transpileStmt(bStmt, out, level+1)
			if err != nil {
				return err
			}
			out.WriteString("\n")
		}
		out.WriteString(indent(level) + "}\n")

	case *ast.LetStmt:
		out.WriteString(indent(level))
		out.WriteString(fmt.Sprintf("var %s", s.Name))
		if s.TypeAnn != "" {
			out.WriteString(" " + mapType(s.TypeAnn))
		}
		out.WriteString(" = ")
		err := transpileExpr(s.Value, out)
		if err != nil {
			return err
		}

	case *ast.AssignStmt:
		out.WriteString(indent(level))
		out.WriteString(s.Name + " = ")
		err := transpileExpr(s.Value, out)
		if err != nil {
			return err
		}

	case *ast.ReturnStmt:
		out.WriteString(indent(level) + "return")
		if s.Value != nil {
			out.WriteString(" ")
			err := transpileExpr(s.Value, out)
			if err != nil {
				return err
			}
		}

	case *ast.ExprStmt:
		out.WriteString(indent(level))
		err := transpileExpr(s.Expr, out)
		if err != nil {
			return err
		}

	case *ast.IfStmt:
		out.WriteString(indent(level) + "if ")
		err := transpileExpr(s.Condition, out)
		if err != nil {
			return err
		}
		out.WriteString(" {\n")
		for _, bStmt := range s.Then.Body {
			err = transpileStmt(bStmt, out, level+1)
			if err != nil {
				return err
			}
			out.WriteString("\n")
		}
		out.WriteString(indent(level) + "}")
		
		if s.Else != nil {
			out.WriteString(" else {\n")
			if elseBlock, ok := s.Else.(*ast.BlockStmt); ok {
				for _, bStmt := range elseBlock.Body {
					err = transpileStmt(bStmt, out, level+1)
					if err != nil {
						return err
					}
					out.WriteString("\n")
				}
			} else {
				err = transpileStmt(s.Else, out, level+1)
				if err != nil {
					return err
				}
				out.WriteString("\n")
			}
			out.WriteString(indent(level) + "}")
		}

	case *ast.WhileStmt:
		out.WriteString(indent(level) + "for ")
		err := transpileExpr(s.Condition, out)
		if err != nil {
			return err
		}
		out.WriteString(" {\n")
		for _, bStmt := range s.Body.Body {
			err = transpileStmt(bStmt, out, level+1)
			if err != nil {
				return err
			}
			out.WriteString("\n")
		}
		out.WriteString(indent(level) + "}")

	default:
		// Unsupported statement fallback
		out.WriteString(indent(level) + "// Unsupported stmt: " + s.String())
	}
	return nil
}

func transpileExpr(expr ast.Expr, out *strings.Builder) error {
	switch e := expr.(type) {
	case *ast.IntLit:
		out.WriteString(fmt.Sprintf("int64(%d)", e.Value))
	case *ast.FloatLit:
		out.WriteString(fmt.Sprintf("%f", e.Value))
	case *ast.BoolLit:
		out.WriteString(fmt.Sprintf("%t", e.Value))
	case *ast.StringLit:
		out.WriteString(fmt.Sprintf("%q", e.Value))
	case *ast.Ident:
		out.WriteString(e.Name)
	case *ast.BinaryOp:
		err := transpileExpr(e.Left, out)
		if err != nil { return err }
		out.WriteString(fmt.Sprintf(" %s ", e.Op))
		err = transpileExpr(e.Right, out)
		if err != nil { return err }
	case *ast.UnaryOp:
		out.WriteString(e.Op)
		err := transpileExpr(e.Expr, out)
		if err != nil { return err }
	case *ast.CallExpr:
		ident, ok := e.Callee.(*ast.Ident)
		if ok && ident.Name == "print" {
			out.WriteString("fmt.Println(")
		} else {
			err := transpileExpr(e.Callee, out)
			if err != nil { return err }
			out.WriteString("(")
		}
		
		for i, arg := range e.Args {
			if i > 0 {
				out.WriteString(", ")
			}
			err := transpileExpr(arg, out)
			if err != nil { return err }
		}
		out.WriteString(")")
	default:
		out.WriteString("/* unsupported expr */")
	}
	return nil
}
