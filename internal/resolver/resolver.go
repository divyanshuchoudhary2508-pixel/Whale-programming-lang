package resolver

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/whale-lang/whale/internal/ast"
	"github.com/whale-lang/whale/internal/lexer"
	"github.com/whale-lang/whale/internal/parser"
)

// Resolve merges all imported modules into the root file's AST.
func Resolve(root *ast.File, baseDir string) error {
	return resolve(root, baseDir, make(map[string]bool))
}

func resolve(file *ast.File, baseDir string, visited map[string]bool) error {
	var newBody []ast.Stmt
	aliases := make(map[string]bool)

	for _, stmt := range file.Body {
		if imp, ok := stmt.(*ast.ImportStmt); ok {
			alias := imp.Alias
			if alias == "" {
				base := filepath.Base(imp.Path)
				alias = strings.TrimSuffix(base, filepath.Ext(base))
			}
			aliases[alias] = true

			// Resolve path (very simple MVP logic)
			targetPath := filepath.Join(baseDir, imp.Path)
			if !strings.HasSuffix(targetPath, ".wh") {
				targetPath += ".wh"
			}

			if visited[targetPath] {
				continue // already imported
			}
			visited[targetPath] = true

			src, err := ioutil.ReadFile(targetPath)
			if err != nil {
				return fmt.Errorf("failed to import %q: %v", imp.Path, err)
			}

			lexRes := lexer.Lex(string(src))
			if len(lexRes.Errors) > 0 {
				return fmt.Errorf("lex error in %s", targetPath)
			}
			parseRes := parser.Parse(lexRes.Tokens)
			if len(parseRes.Errors) > 0 {
				return fmt.Errorf("parse error in %s", targetPath)
			}
			importedFile := parseRes.File

			// Recursively resolve the imported file
			if err := resolve(&importedFile, filepath.Dir(targetPath), visited); err != nil {
				return err
			}

			// Prefix its top-level declarations
			prefix := alias + "_"
			for _, stmt := range importedFile.Body {
				prefixDecl(stmt, prefix)
			}
			
			// Also, within the imported file, we must prefix local references to its OWN globals!
			// Actually, if a module calls its own function `add()`, it should now call `math_add()`.
			// Doing a full semantic rename without a type-checker is hard.
			// For MVP, we require module files to be self-contained or we do a simple pass.
			// Wait, if `math.wh` has `fn add() {}` and `fn double() { return add(5, 5); }`,
			// if we change `add` to `math_add`, then `double` calls `add` which is undefined!
			// We MUST rename internal references.
			
			rewriteGlobals(&importedFile, prefix)

			newBody = append(newBody, importedFile.Body...)
		} else {
			newBody = append(newBody, stmt)
		}
	}

	file.Body = newBody
	rewriteModuleAccesses(file, aliases)
	return nil
}

func prefixDecl(s ast.Stmt, prefix string) {
	switch s := s.(type) {
	case *ast.FnStmt:
		if s.Name != "main" {
			s.Name = prefix + s.Name
		}
	case *ast.ExternFnStmt:
		s.Name = prefix + s.Name
	case *ast.StructDecl:
		s.Name = prefix + s.Name
	case *ast.EnumDecl:
		s.Name = prefix + s.Name
	case *ast.LetStmt:
		s.Name = prefix + s.Name
	case *ast.ImplDecl:
		s.StructName = prefix + s.StructName
	}
}

// rewriteGlobals does a naive find-and-replace of top-level declarations inside the module itself.
func rewriteGlobals(file *ast.File, prefix string) {
	// First gather all globals
	globals := make(map[string]bool)
	for _, stmt := range file.Body {
		switch s := stmt.(type) {
		case *ast.FnStmt:
			globals[strings.TrimPrefix(s.Name, prefix)] = true
		case *ast.ExternFnStmt:
			globals[strings.TrimPrefix(s.Name, prefix)] = true
		case *ast.StructDecl:
			globals[strings.TrimPrefix(s.Name, prefix)] = true
		case *ast.EnumDecl:
			globals[strings.TrimPrefix(s.Name, prefix)] = true
		case *ast.LetStmt:
			globals[strings.TrimPrefix(s.Name, prefix)] = true
		case *ast.ImplDecl:
			globals[strings.TrimPrefix(s.StructName, prefix)] = true
		}
	}

	// Then rewrite all Idents that match a global
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		if n == nil {
			return
		}
		switch n := n.(type) {
		case *ast.Ident:
			if globals[n.Name] {
				n.Name = prefix + n.Name
			}
		case *ast.CallExpr:
			if id, ok := n.Callee.(*ast.Ident); ok && globals[id.Name] {
				id.Name = prefix + id.Name
			} else {
				walk(n.Callee)
			}
			for _, a := range n.Args {
				walk(a)
			}
		case *ast.FieldAccess:
			walk(n.Expr)
			// don't walk n.Field because it's a struct field
		case *ast.BinaryOp:
			walk(n.Left)
			walk(n.Right)
		case *ast.UnaryOp:
			walk(n.Expr)
		case *ast.LetStmt:
			walk(n.Value)
		case *ast.AssignStmt:
			walk(n.Value)
		case *ast.ExprStmt:
			walk(n.Expr)
		case *ast.ReturnStmt:
			walk(n.Value)
		case *ast.BlockStmt:
			for _, s := range n.Body {
				walk(s)
			}
		case *ast.IfStmt:
			walk(n.Condition)
			walk(n.Then)
			if n.Else != nil {
				walk(n.Else)
			}
		case *ast.WhileStmt:
			walk(n.Condition)
			walk(n.Body)
		case *ast.FnStmt:
			walk(n.Body)
		case *ast.ImplDecl:
			for _, method := range n.Methods {
				walk(method.Body)
			}
		case *ast.StructLit:
			if globals[n.Type] {
				n.Type = prefix + n.Type
			}
			for _, v := range n.Fields {
				walk(v)
			}
		case *ast.ListLit:
			for _, v := range n.Values {
				walk(v)
			}
		case *ast.IndexExpr:
			walk(n.Expr)
			walk(n.Index)
		case *ast.SpawnStmt:
			walk(n.Call)
		case *ast.ChanSendStmt:
			walk(n.Chan)
			walk(n.Value)
		case *ast.ChanRecvExpr:
			walk(n.Chan)
		case *ast.TryExpr:
			walk(n.Expr)
		case *ast.ErrorLit:
			walk(n.Msg)
		}
	}

	for _, stmt := range file.Body {
		walk(stmt)
	}
}

func rewriteModuleAccesses(file *ast.File, aliases map[string]bool) {
	// Replaces `math.add` with `math_add`
	var walk func(n ast.Node)
	var walkExpr func(e *ast.Expr)
	
	walkExpr = func(e *ast.Expr) {
		if e == nil || *e == nil {
			return
		}
		if fa, ok := (*e).(*ast.FieldAccess); ok {
			if id, ok := fa.Expr.(*ast.Ident); ok {
				if aliases[id.Name] {
					*e = &ast.Ident{Pos: fa.Pos, Name: id.Name + "_" + fa.Field}
					return
				}
			}
		}
		walk(*e)
	}

	walk = func(n ast.Node) {
		if n == nil {
			return
		}
		switch n := n.(type) {
		case *ast.CallExpr:
			walkExpr(&n.Callee)
			for i := range n.Args {
				walkExpr(&n.Args[i])
			}
		case *ast.FieldAccess:
			walkExpr(&n.Expr)
		case *ast.BinaryOp:
			walkExpr(&n.Left)
			walkExpr(&n.Right)
		case *ast.UnaryOp:
			walkExpr(&n.Expr)
		case *ast.LetStmt:
			walkExpr(&n.Value)
		case *ast.AssignStmt:
			walkExpr(&n.Value)
		case *ast.ExprStmt:
			walkExpr(&n.Expr)
		case *ast.ReturnStmt:
			walkExpr(&n.Value)
		case *ast.BlockStmt:
			for _, s := range n.Body {
				walk(s)
			}
		case *ast.IfStmt:
			walkExpr(&n.Condition)
			walk(n.Then)
			if n.Else != nil {
				walk(n.Else)
			}
		case *ast.WhileStmt:
			walkExpr(&n.Condition)
			walk(n.Body)
		case *ast.FnStmt:
			walk(n.Body)
		case *ast.ImplDecl:
			for _, method := range n.Methods {
				walk(method.Body)
			}
		case *ast.StructLit:
			for k := range n.Fields {
				v := n.Fields[k]
				walkExpr(&v)
				n.Fields[k] = v
			}
		case *ast.TryExpr:
			walkExpr(&n.Expr)
		case *ast.ErrorLit:
			walkExpr(&n.Msg)
		case *ast.ListLit:
			for i := range n.Values {
				walkExpr(&n.Values[i])
			}
		case *ast.IndexExpr:
			walkExpr(&n.Expr)
			walkExpr(&n.Index)
		case *ast.SpawnStmt:
			walk(n.Call)
		case *ast.ChanSendStmt:
			walkExpr(&n.Chan)
			walkExpr(&n.Value)
		case *ast.ChanRecvExpr:
			walkExpr(&n.Chan)
		}
	}

	for _, stmt := range file.Body {
		walk(stmt)
	}
}
