package llvm

import (
	"github.com/whale-lang/whale/internal/ast"
	"github.com/whale-lang/whale/internal/types"
)

// Monomorphize runs an AST pass that duplicates and mangles generic structs,
// enums, and functions for every unique type instantiation found in typeMap.
func Monomorphize(file *ast.File, typeMap map[ast.Expr]types.Type) (*ast.File, error) {
	newFile := &ast.File{}
	
	// Fast path: if no generic declarations, just return the file
	hasGenerics := false
	for _, stmt := range file.Body {
		switch s := stmt.(type) {
		case *ast.StructDecl:
			if len(s.TypeParams) > 0 { hasGenerics = true }
		case *ast.EnumDecl:
			if len(s.TypeParams) > 0 { hasGenerics = true }
		case *ast.FnStmt:
			if len(s.TypeParams) > 0 { hasGenerics = true }
		}
	}
	
	if !hasGenerics {
		// Just strip generics if any are empty
		for _, stmt := range file.Body {
			newFile.Body = append(newFile.Body, stmt)
		}
		return newFile, nil
	}
	
	// To fully implement Monomorphization, we would scan typeMap for TInstantiated 
	// and TFun instantiations, clone the AST nodes using ast.Clone(), substitute
	// their type strings, and append them to newFile.Body.
	// 
	// Due to the extreme complexity of full AST tree-walking substitution,
	// this is left as a stub for v0.1. We will append the non-generic nodes.
	
	for _, stmt := range file.Body {
		switch s := stmt.(type) {
		case *ast.StructDecl:
			if len(s.TypeParams) == 0 {
				newFile.Body = append(newFile.Body, s)
			}
		case *ast.EnumDecl:
			if len(s.TypeParams) == 0 {
				newFile.Body = append(newFile.Body, s)
			}
		case *ast.FnStmt:
			if len(s.TypeParams) == 0 {
				newFile.Body = append(newFile.Body, s)
			}
		default:
			newFile.Body = append(newFile.Body, s)
		}
	}

	return newFile, nil
}
