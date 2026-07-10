package optimize

import (
	"github.com/whale-lang/whale/internal/ast"
)

// Optimize runs AST-level optimization passes such as Constant Folding and Dead Code Elimination.
// It modifies the AST in-place.
func Optimize(file *ast.File) {
	for _, stmt := range file.Body {
		optimizeStmt(stmt)
	}
}

func optimizeStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.FnStmt:
		s.Body = optimizeBlock(s.Body)
	case *ast.ExprStmt:
		s.Expr = optimizeExpr(s.Expr)
	case *ast.LetStmt:
		s.Value = optimizeExpr(s.Value)
	case *ast.ReturnStmt:
		if s.Value != nil {
			s.Value = optimizeExpr(s.Value)
		}
	case *ast.IfStmt:
		s.Condition = optimizeExpr(s.Condition)
		s.Then = optimizeBlock(s.Then)
		if s.Else != nil {
			optimizeStmt(s.Else)
		}
	case *ast.WhileStmt:
		s.Condition = optimizeExpr(s.Condition)
		s.Body = optimizeBlock(s.Body)
	case *ast.BlockStmt:
		*s = *optimizeBlock(s)
	}
}

func optimizeBlock(b *ast.BlockStmt) *ast.BlockStmt {
	var newBody []ast.Stmt
	for _, stmt := range b.Body {
		optimizeStmt(stmt)
		newBody = append(newBody, stmt)
		
		// Dead Code Elimination: stop processing statements after a return
		if _, isReturn := stmt.(*ast.ReturnStmt); isReturn {
			break
		}
	}
	b.Body = newBody
	return b
}

func optimizeExpr(expr ast.Expr) ast.Expr {
	switch e := expr.(type) {
	case *ast.BinaryOp:
		e.Left = optimizeExpr(e.Left)
		e.Right = optimizeExpr(e.Right)

		// Constant Folding
		lInt, lIsInt := e.Left.(*ast.IntLit)
		rInt, rIsInt := e.Right.(*ast.IntLit)
		
		if lIsInt && rIsInt {
			switch e.Op {
			case "+":
				return &ast.IntLit{Value: lInt.Value + rInt.Value, Pos: e.Pos}
			case "-":
				return &ast.IntLit{Value: lInt.Value - rInt.Value, Pos: e.Pos}
			case "*":
				return &ast.IntLit{Value: lInt.Value * rInt.Value, Pos: e.Pos}
			case "/":
				if rInt.Value != 0 {
					return &ast.IntLit{Value: lInt.Value / rInt.Value, Pos: e.Pos}
				}
			}
		}

		lStr, lIsStr := e.Left.(*ast.StringLit)
		rStr, rIsStr := e.Right.(*ast.StringLit)
		
		if lIsStr && rIsStr && e.Op == "+" {
			return &ast.StringLit{Value: lStr.Value + rStr.Value, Pos: e.Pos}
		}
		
	case *ast.CallExpr:
		for i, arg := range e.Args {
			e.Args[i] = optimizeExpr(arg)
		}
	case *ast.ListLit:
		for i, el := range e.Values {
			e.Values[i] = optimizeExpr(el)
		}
	}
	return expr
}
