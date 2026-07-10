// Package types implements the basic type checker for Whale v0.1.
//
// The type checker walks the AST and verifies that types are used
// correctly. It runs between parsing and interpretation. Errors are
// collected (not returned on first failure).
//
// What is checked in v0.1:
//   - if/while conditions must be bool
//   - Binary operators have compatible operand types
//   - Identifier references are declared (where statically knowable)
//   - Struct literals have all required fields
//   - Function argument counts match declarations
//
// What is NOT checked (deferred to v0.2+):
//   - Generic type parameters
//   - Stream type inference
//   - Exhaustive match
//   - Return type matches function signature
package types

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/whale-lang/whale/internal/ast"
	"github.com/whale-lang/whale/internal/interp"
)

// ============================================================================
// Types
// ============================================================================

// Type is a Whale type.
type Type interface {
	typeMarker()
	String() string
}

type TInt struct{}
type TUint struct{ Bits int }
type TFloat struct{}
type TString struct{}
type TBool struct{}
type TUnit struct{}
type TList struct{ Elem Type }
type TStream struct{ Elem Type }
type TVec256 struct{ Elem Type }
type TFun struct {
	Params []Type
	Ret    Type
}
type TStruct struct{ Name string }
type TEnum struct {
	Name     string
	Variants map[string]Type // Map of VariantName -> PayloadType (nil if no payload)
}
type TModule struct{ Name string }
type TGenericVar struct{ Name string }
type TInstantiated struct {
	Base Type
	Args []Type
}
type TUnknown struct{}
type TChan struct{ Elem Type }
type TError struct{}
type TResult struct{ Elem Type }

func (TInt) typeMarker()     {}
func (TUint) typeMarker()    {}
func (TFloat) typeMarker()   {}
func (TString) typeMarker()  {}
func (TBool) typeMarker()    {}
func (TUnit) typeMarker()    {}
func (TList) typeMarker()    {}
func (TStream) typeMarker()  {}
func (TVec256) typeMarker()  {}
func (TFun) typeMarker()     {}
func (TStruct) typeMarker()  {}
func (TEnum) typeMarker()    {}
func (TModule) typeMarker()  {}
func (TGenericVar) typeMarker() {}
func (TInstantiated) typeMarker() {}
func (TUnknown) typeMarker() {}
func (TChan) typeMarker()    {}
func (TError) typeMarker()   {}
func (TResult) typeMarker()  {}

func (t TInt) String() string    { return "int" }
func (t TUint) String() string   { return fmt.Sprintf("u%d", t.Bits) }
func (t TFloat) String() string  { return "float" }
func (t TString) String() string { return "string" }
func (t TBool) String() string   { return "bool" }
func (t TUnit) String() string   { return "()" }
func (t TList) String() string {
	if t.Elem == nil {
		return "list"
	}
	return "[" + t.Elem.String() + "]"
}
func (t TStream) String() string {
	if t.Elem == nil {
		return "stream"
	}
	return "stream<" + t.Elem.String() + ">"
}
func (t TVec256) String() string {
	if t.Elem == nil {
		return "vec256"
	}
	return "vec256<" + t.Elem.String() + ">"
}
func (t TFun) String() string {
	out := "fn("
	for i, p := range t.Params {
		if i > 0 {
			out += ", "
		}
		out += p.String()
	}
	out += ")"
	if t.Ret != nil {
		out += " -> " + t.Ret.String()
	}
	return out
}
func (t TStruct) String() string  { return t.Name }
func (t TEnum) String() string    { return t.Name }
func (t TModule) String() string  { return t.Name }
func (t TGenericVar) String() string { return t.Name }
func (t TInstantiated) String() string {
	out := t.Base.String() + "<"
	for i, a := range t.Args {
		if i > 0 {
			out += ", "
		}
		out += a.String()
	}
	out += ">"
	return out
}
func (t TUnknown) String() string { return "unknown" }
func (t TChan) String() string {
	if t.Elem == nil {
		return "chan"
	}
	return "chan<" + t.Elem.String() + ">"
}
func (t TError) String() string { return "error" }
func (t TResult) String() string {
	if t.Elem == nil {
		return "Result"
	}
	return "Result<" + t.Elem.String() + ">"
}

// ============================================================================
// Errors
// ============================================================================

// Error is a type-checking error.
type Error struct {
	Pos ast.Position
	Msg string
}

func (e Error) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Col, e.Msg)
}

// Result bundles any errors from a type-checking pass and the annotated types.
type Result struct {
	Errors []Error
	Types  map[ast.Expr]Type
	Env    *Scope
}

// ============================================================================
// Check entry point
// ============================================================================

// Importer resolves and type-checks a module by its path, returning its exported Scope.
type Importer interface {
	Import(path string) (*Scope, error)
}

// Config controls the type checker.
type Config struct {
	Importer Importer
}

// Check runs the type checker over a parsed program.
func Check(file ast.File) Result {
	return CheckWithConfig(file, Config{})
}

func (c *checker) withTypeParams(params []string, cb func()) {
	oldParams := c.typeParams
	if len(params) > 0 {
		c.typeParams = make(map[string]bool)
		for _, p := range params {
			c.typeParams[p] = true
		}
	}
	cb()
	c.typeParams = oldParams
}

// CheckWithConfig type-checks with a specific configuration.
func CheckWithConfig(file ast.File, cfg Config) Result {
	c := &checker{
		structs:   make(map[string]*ast.StructDecl),
		enums:     make(map[string]*ast.EnumDecl),
		funcs:     make(map[string]*ast.FnStmt),
		externs:   make(map[string]*ast.ExternFnStmt),
		env:       newScope(builtinScope()),
		exprTypes: make(map[ast.Expr]Type),
		importer:  cfg.Importer,
		file:      file,
	}
	// First pass: register top-level declarations
	for _, stmt := range file.Body {
		switch s := stmt.(type) {
		case *ast.StructDecl:
			c.structs[s.Name] = s
		case *ast.EnumDecl:
			c.enums[s.Name] = s
		case *ast.FnStmt:
			c.funcs[s.Name] = s
		case *ast.ExternFnStmt:
			c.externs[s.Name] = s
		}
	}
	// Second pass: pre-define function types and enum variants in env
	for name, s := range c.funcs {
		c.withTypeParams(s.TypeParams, func() {
			fnType := c.makeFnType(s.Params, s.ReturnType, s.Pos)
			c.env.define(name, fnType)
		})
	}
	for name, s := range c.externs {
		fnType := c.makeFnType(s.Params, s.ReturnType, s.Pos)
		c.env.define(name, fnType)
	}
	for name, s := range c.enums {
		c.withTypeParams(s.TypeParams, func() {
			tenum := TEnum{Name: name, Variants: make(map[string]Type)}
			c.env.define(name, tenum)
			
			for _, v := range s.Variants {
				var payloadType Type
				if v.Type != nil {
					// Parse type annotation for payload
					payloadType = c.resolveTypeExpr(v.Type)
				}
				tenum.Variants[v.Name] = payloadType
				
				// Register constructor in scope
				var retType Type = tenum
				if len(s.TypeParams) > 0 {
					args := make([]Type, len(s.TypeParams))
					for i, tp := range s.TypeParams {
						args[i] = TGenericVar{Name: tp}
					}
					retType = TInstantiated{Base: tenum, Args: args}
				}
				
				if payloadType != nil {
					c.env.define(v.Name, TFun{Params: []Type{payloadType}, Ret: retType})
				} else {
					c.env.define(v.Name, retType)
				}
			}
		})
	}
	// Third pass: check everything
	for _, stmt := range file.Body {
		c.checkStmt(stmt)
	}
	return Result{Errors: c.errors, Types: c.exprTypes, Env: c.env}
}

func builtinScope() *Scope {
	s := newScope(nil)
	unit := TUnit{}
	s.define("print", TFun{Ret: unit})
	s.define("len", TFun{Params: []Type{TUnknown{}}, Ret: TInt{}})
	s.define("to_string", TFun{Params: []Type{TUnknown{}}, Ret: TString{}})
	s.define("contains", TFun{Params: []Type{TUnknown{}, TUnknown{}}, Ret: TBool{}})
	s.define("stream", TFun{Params: []Type{TUnknown{}}, Ret: TStream{}})
	s.define("filter", TFun{Params: []Type{TStream{}, TFun{}}, Ret: TStream{}})
	s.define("map", TFun{Params: []Type{TStream{}, TFun{}}, Ret: TStream{}})
	s.define("collect", TFun{Params: []Type{TStream{}}, Ret: TList{}})
	s.define("for_each", TFun{Params: []Type{TStream{}, TFun{}}, Ret: TUnit{}})
	s.define("fold", TFun{Params: []Type{TStream{}, TUnknown{}, TFun{}}, Ret: TUnknown{}})
	s.define("take", TFun{Params: []Type{TStream{}, TInt{}}, Ret: TStream{}})
	s.define("skip", TFun{Params: []Type{TStream{}, TInt{}}, Ret: TStream{}})
	s.define("read_file", TFun{Params: []Type{TString{}}, Ret: TString{}})
	s.define("write_file", TFun{Params: []Type{TString{}, TString{}}, Ret: TUnit{}})
	s.define("make_chan", TFun{Ret: TChan{Elem: TUnknown{}}})
	s.define("lines", TFun{Params: []Type{TString{}}, Ret: TList{Elem: TString{}}})
	s.define("push", TFun{Params: []Type{TList{Elem: TUnknown{}}, TUnknown{}}, Ret: TList{Elem: TUnknown{}}})
	s.define("pop", TFun{Params: []Type{TList{Elem: TUnknown{}}}, Ret: TUnknown{}})
	s.define("type_of", TFun{Params: []Type{TUnknown{}}, Ret: TString{}})
	
	// SIMD Built-ins
	s.define("vec_add", TFun{Params: []Type{TVec256{Elem: TUnknown{}}, TVec256{Elem: TUnknown{}}}, Ret: TVec256{Elem: TUnknown{}}})
	s.define("vec_mul", TFun{Params: []Type{TVec256{Elem: TUnknown{}}, TVec256{Elem: TUnknown{}}}, Ret: TVec256{Elem: TUnknown{}}})
	
	// String standard library
	s.define("split", TFun{Params: []Type{TString{}, TString{}}, Ret: TList{Elem: TString{}}})
	s.define("trim", TFun{Params: []Type{TString{}}, Ret: TString{}})
	s.define("replace", TFun{Params: []Type{TString{}, TString{}, TString{}}, Ret: TString{}})
	s.define("to_lower", TFun{Params: []Type{TString{}}, Ret: TString{}})
	s.define("to_upper", TFun{Params: []Type{TString{}}, Ret: TString{}})
	
	// Math standard library
	s.define("abs", TFun{Params: []Type{TFloat{}}, Ret: TFloat{}})
	s.define("max", TFun{Params: []Type{TFloat{}, TFloat{}}, Ret: TFloat{}})
	s.define("min", TFun{Params: []Type{TFloat{}, TFloat{}}, Ret: TFloat{}})
	s.define("parse_int", TFun{Params: []Type{TString{}}, Ret: TInt{}})
	return s
}

func (c *checker) checkMatchExpr(m *ast.MatchExpr) Type {
	exprT := c.checkExpr(m.Expr)
	
	enumType, ok := exprT.(TEnum)
	if !ok {
		if !isUnknown(exprT) {
			c.errorAt(m.Pos, "match expression requires an enum type, got %s", exprT)
		}
		return TUnknown{}
	}
	
	enumDecl, ok := c.enums[enumType.Name]
	if !ok {
		c.errorAt(m.Pos, "unknown enum %q", enumType.Name)
		return TUnknown{}
	}
	
	seenVariants := make(map[string]bool)
	hasCatchAll := false
	var unifiedRetType Type = TUnknown{}
	
	for _, arm := range m.Arms {
		if arm.IsCatchAll {
			hasCatchAll = true
		} else {
			seenVariants[arm.Variant] = true
			
			// Verify variant exists in enum
			var variantDecl *ast.EnumVariant
			for _, v := range enumDecl.Variants {
				if v.Name == arm.Variant {
					variantDecl = v
					break
				}
			}
			
			if variantDecl == nil {
				c.errorAt(arm.Pos, "variant %q does not exist in enum %q", arm.Variant, enumType.Name)
				continue
			}
			
			// Open a new scope for the arm
			prevEnv := c.env
			c.env = newScope(c.env)
			
			// Check binding
			if arm.Binding != "" {
				if variantDecl.Type == nil {
					c.errorAt(arm.Pos, "variant %q has no payload, but arm defines a binding %q", arm.Variant, arm.Binding)
				} else {
					payloadType := enumType.Variants[arm.Variant]
					c.env.define(arm.Binding, payloadType)
				}
			} else if variantDecl.Type != nil {
				c.errorAt(arm.Pos, "variant %q has a payload, but arm is missing a binding", arm.Variant)
			}
			
			armType := c.checkExpr(arm.Body)
			
			if isUnknown(unifiedRetType) && !isUnknown(armType) {
				unifiedRetType = armType
			} else if !isUnknown(unifiedRetType) && !isUnknown(armType) && !sameType(unifiedRetType, armType) {
				c.errorAt(arm.Pos, "match arms have incompatible types: expected %s, got %s", unifiedRetType, armType)
			}
			
			// Restore environment
			c.env = prevEnv
		}
	}
	
	// Check for the catch-all arm
	if hasCatchAll {
		// Just evaluate the catch-all arm type
		for _, arm := range m.Arms {
			if arm.IsCatchAll {
				prevEnv := c.env
				c.env = newScope(c.env)
				armType := c.checkExpr(arm.Body)
				if isUnknown(unifiedRetType) && !isUnknown(armType) {
					unifiedRetType = armType
				} else if !isUnknown(unifiedRetType) && !isUnknown(armType) && !sameType(unifiedRetType, armType) {
					c.errorAt(arm.Pos, "match arms have incompatible types: expected %s, got %s", unifiedRetType, armType)
				}
				c.env = prevEnv
			}
		}
	}
	
	// Check exhaustiveness
	if !hasCatchAll {
		for _, v := range enumDecl.Variants {
			if !seenVariants[v.Name] {
				c.errorAt(m.Pos, "match is not exhaustive, missing variant %q", v.Name)
			}
		}
	}
	
	return unifiedRetType
}

// ============================================================================
// Scope
// ============================================================================

type Scope struct {
	names  map[string]Type
	parent *Scope
}

func newScope(parent *Scope) *Scope {
	return &Scope{names: make(map[string]Type, 8), parent: parent}
}

func (s *Scope) get(name string) (Type, bool) {
	if t, ok := s.names[name]; ok {
		return t, true
	}
	if s.parent != nil {
		return s.parent.get(name)
	}
	return nil, false
}

func (s *Scope) define(name string, t Type) {
	s.names[name] = t
}

// ============================================================================
// Checker
// ============================================================================

type checker struct {
	errors    []Error
	env       *Scope
	structs   map[string]*ast.StructDecl
	enums     map[string]*ast.EnumDecl
	funcs     map[string]*ast.FnStmt
	externs   map[string]*ast.ExternFnStmt
	retType   Type
	exprTypes  map[ast.Expr]Type
	importer   Importer
	modules    map[string]*Scope
	currentFn  *ast.FnStmt
	typeParams map[string]bool
	file       ast.File
}

func (c *checker) errorAt(pos ast.Position, format string, args ...interface{}) {
	c.errors = append(c.errors, Error{
		Pos: pos,
		Msg: fmt.Sprintf(format, args...),
	})
}

func (c *checker) checkStmt(s ast.Stmt) {
	if s == nil {
		return
	}
	switch s := s.(type) {
	case *ast.ImportStmt:
		if c.importer != nil {
			moduleScope, err := c.importer.Import(s.Path)
			if err != nil {
				c.errorAt(s.Pos, "failed to import %q: %v", s.Path, err)
			} else {
				// define alias as a module
				c.env.define(s.Alias, TModule{Name: s.Alias})
				if c.modules == nil {
					c.modules = make(map[string]*Scope)
				}
				c.modules[s.Alias] = moduleScope
			}
		}
	case *ast.LetStmt:
		t := c.checkExpr(s.Value)
		if s.TypeAnn != "" {
			declared := c.resolveTypeName(s.Pos, s.TypeAnn)
			if !isUnknown(declared) && !isUnknown(t) {
				subst := make(map[string]Type)
				if unify(t, declared, subst) {
					t = substitute(t, subst)
				}
				if !sameType(declared, t) {
					c.errorAt(s.Pos, "type annotation says %s but value is %s", declared, t)
				}
			}
			c.env.define(s.Name, declared)
		} else {
			c.env.define(s.Name, t)
		}
	case *ast.AssignStmt:
		valTy := c.checkExpr(s.Value)
		if declared, ok := c.env.get(s.Name); ok {
			if !sameType(declared, valTy) && !isUnknown(declared) && !isUnknown(valTy) {
				c.errorAt(s.Pos, "cannot assign %s to %s", valTy, declared)
			}
		}
	case *ast.AssignFieldStmt:
		objTy := c.checkExpr(s.Object)
		valTy := c.checkExpr(s.Value)
		if st, ok := objTy.(TStruct); ok {
			if decl, ok := c.structs[st.Name]; ok {
				found := false
				for _, f := range decl.Fields {
					if f.Name == s.Field {
						found = true
						fieldTy := c.resolveTypeName(s.Pos, f.Type)
						if !sameType(fieldTy, valTy) && !isUnknown(fieldTy) && !isUnknown(valTy) {
							c.errorAt(s.Pos, "cannot assign %s to field %s of %s", valTy, fieldTy, st.Name)
						}
						break
					}
				}
				if !found {
					c.errorAt(s.Pos, "%s has no field %q", st.Name, s.Field)
				}
			}
		} else if !isUnknown(objTy) {
			c.errorAt(s.Pos, "cannot access field of non-struct %s", objTy)
		}
	case *ast.AssignIndexStmt:
		listTy := c.checkExpr(s.List)
		idxTy := c.checkExpr(s.Index)
		valTy := c.checkExpr(s.Value)
		
		if !sameType(idxTy, TInt{}) && !isUnknown(idxTy) {
			c.errorAt(s.Pos, "index must be int, got %s", idxTy)
		}
		
		if l, ok := listTy.(TList); ok {
			if !sameType(l.Elem, valTy) && !isUnknown(l.Elem) && !isUnknown(valTy) {
				c.errorAt(s.Pos, "cannot assign %s to list of %s", valTy, l.Elem)
			}
		} else if !isUnknown(listTy) {
			c.errorAt(s.Pos, "cannot index non-list type %s", listTy)
		}
	case *ast.ExprStmt:
		c.checkExpr(s.Expr)
	case *ast.SpawnStmt:
		c.checkExpr(s.Call)
	case *ast.ChanSendStmt:
		chTy := c.checkExpr(s.Chan)
		valTy := c.checkExpr(s.Value)
		if cty, ok := chTy.(TChan); ok {
			if !sameType(cty.Elem, valTy) && !isUnknown(cty.Elem) && !isUnknown(valTy) {
				c.errorAt(s.Pos, "cannot send %s to channel of %s", valTy, cty.Elem)
			}
		} else if !isUnknown(chTy) {
			c.errorAt(s.Pos, "cannot send to non-channel type %s", chTy)
		}
	case *ast.FnStmt:
		c.checkFnDecl(s)
	case *ast.ExternFnStmt:
		// Already registered in pass 2, nothing to check body-wise
	case *ast.StructDecl:
		// Nothing to check — struct fields are just names
	case *ast.EnumDecl:
		// Already registered variants in pass 2
	case *ast.IfStmt:
		condT := c.checkExpr(s.Condition)
		if condT != nil && !isUnknown(condT) {
			if _, ok := condT.(TBool); !ok {
				c.errorAt(s.Pos, "if condition must be bool, got %s", condT)
			}
		}
		c.checkBlock(s.Then)
		if s.Else != nil {
			c.checkStmt(s.Else)
		}
	case *ast.WhileStmt:
		condT := c.checkExpr(s.Condition)
		if condT != nil && !isUnknown(condT) {
			if _, ok := condT.(TBool); !ok {
				c.errorAt(s.Pos, "while condition must be bool, got %s", condT)
			}
		}
		c.checkBlock(s.Body)
	case *ast.ArenaStmt:
		c.checkBlock(s.Body)
	case *ast.ReturnStmt:
		var retType Type = TUnit{}
		if s.Value != nil {
			retType = c.checkExpr(s.Value)
		}
		if c.currentFn != nil {
			var expected Type = TUnit{}
			if c.currentFn.ReturnType != "" {
				expected = c.resolveTypeName(s.Pos, c.currentFn.ReturnType)
			}
			if !sameType(expected, retType) && !isUnknown(expected) && !isUnknown(retType) {
				c.errorAt(s.Pos, "return type mismatch: expected %s, got %s", expected, retType)
			}
		}
	case *ast.ForStmt:
		c.checkExpr(s.Iterable)
		child := newScope(c.env)
		child.define(s.Variable, TUnknown{})
		prev := c.env
		c.env = child
		c.checkBlock(s.Body)
		c.env = prev

	case *ast.BlockStmt:
		c.checkBlock(s)
	}
}

func (c *checker) checkBlock(b *ast.BlockStmt) {
	if b == nil {
		return
	}
	child := newScope(c.env)
	prev := c.env
	c.env = child
	for _, s := range b.Body {
		c.checkStmt(s)
	}
	c.env = prev
}

func (c *checker) checkFnDecl(s *ast.FnStmt) {
	c.withTypeParams(s.TypeParams, func() {
		fnType := c.makeFnType(s.Params, s.ReturnType, s.Pos)
		c.env.define(s.Name, fnType)

		child := newScope(c.env)
		for i, p := range s.Params {
			paramT := c.resolveTypeName(s.Pos, p.Type)
			if i < len(fnType.Params) {
				child.define(p.Name, paramT)
			}
		}
		prev := c.env
		c.env = child
		
		prevFn := c.currentFn
		c.currentFn = s
		
		c.checkBlock(s.Body)
		
		c.currentFn = prevFn
		c.env = prev
	})
}

func (c *checker) makeFnType(params []ast.Param, returnType string, pos ast.Position) TFun {
	paramTypes := make([]Type, len(params))
	for i, p := range params {
		paramTypes[i] = c.resolveTypeName(pos, p.Type)
	}
	ret := Type(TUnit{})
	if returnType != "" {
		ret = c.resolveTypeName(pos, returnType)
	}
	return TFun{Params: paramTypes, Ret: ret}
}

func (c *checker) checkExpr(e ast.Expr) Type {
	if e == nil {
		return TUnknown{}
	}
	t := c.checkExprInternal(e)
	c.exprTypes[e] = t
	return t
}

func (c *checker) checkExprInternal(e ast.Expr) Type {
	if e == nil {
		return TUnknown{}
	}
	switch e := e.(type) {
	case *ast.IntLit:
		return TInt{}
	case *ast.FloatLit:
		return TFloat{}
	case *ast.StringLit:
		return TString{}
	case *ast.BoolLit:
		return TBool{}
	case *ast.Ident:
		t, ok := c.env.get(e.Name)
		if !ok {
			// Don't error on unknown names — the interpreter does that.
			// This avoids false positives from dynamic bindings.
			return TUnknown{}
		}
		return t
	case *ast.UnaryOp:
		inner := c.checkExpr(e.Expr)
		switch e.Op {
		case "-":
			if _, ok := inner.(TInt); ok {
				return TInt{}
			}
			if _, ok := inner.(TFloat); ok {
				return TFloat{}
			}
			if !isUnknown(inner) {
				c.errorAt(e.Pos, "unary - requires int or float, got %s", inner)
			}
		case "!":
			if _, ok := inner.(TBool); ok {
				return TBool{}
			}
			if !isUnknown(inner) {
				c.errorAt(e.Pos, "operator ! expected bool, got %s", inner)
			}
			return TBool{}
		}
		return TUnknown{}
	case *ast.ComptimeExpr:
		val, err := interp.EvalExprComptime(e.Expr, c.file)
		if err != nil {
			c.errorAt(e.Pos, err.Error())
			return TUnknown{}
		}
		// The value is an ast literal, e.g. *ast.IntLit.
		// We replace the comptime expression's inner expression with this literal!
		// But actually, we want the type-checker to treat `e` as having the type of `val`.
		e.Expr = val
		return c.checkExpr(val)
	case *ast.ErrorLit:
		c.checkExpr(e.Msg)
		return TError{}
	case *ast.TryExpr:
		innerT := c.checkExpr(e.Expr)
		if res, ok := innerT.(TResult); ok {
			if c.currentFn != nil && !strings.HasPrefix(c.currentFn.ReturnType, "Result<") {
				c.errorAt(e.Pos, "cannot use ? operator in a function that does not return a Result")
			}
			return res.Elem
		}
		if !isUnknown(innerT) {
			c.errorAt(e.Pos, "cannot use ? operator on non-Result type %s", innerT)
		}
		return TUnknown{}
	case *ast.ChanRecvExpr:
		chTy := c.checkExpr(e.Chan)
		if cty, ok := chTy.(TChan); ok {
			return cty.Elem
		}
		if !isUnknown(chTy) {
			c.errorAt(e.Pos, "cannot receive from non-channel type %s", chTy)
		}
		return TUnknown{}
	case *ast.BinaryOp:
		return c.checkBinary(e)
	case *ast.CallExpr:
		return c.checkCall(e)
	case *ast.MatchExpr:
		return c.checkMatchExpr(e)
	case *ast.ListLit:
		if len(e.Values) == 0 {
			return TList{Elem: TUnknown{}}
		}
		elemT := c.checkExpr(e.Values[0])
		for _, v := range e.Values[1:] {
			t := c.checkExpr(v)
			if !isUnknown(t) && !isUnknown(elemT) && !sameType(t, elemT) {
				c.errorAt(e.Pos, "list element type mismatch: %s vs %s", elemT, t)
			}
		}
		return TList{Elem: elemT}
	case *ast.FnLit:
		return c.checkFnLit(e, nil)
	case *ast.StructLit:
		decl, ok := c.structs[e.Type]
		if !ok {
			// Struct might be defined elsewhere; don't error
			return TStruct{Name: e.Type}
		}
		for _, f := range decl.Fields {
			if _, present := e.Fields[f.Name]; !present {
				c.errorAt(e.Pos, "missing field %q in %s literal", f.Name, e.Type)
			}
		}
		for fname, fval := range e.Fields {
			found := false
			for _, f := range decl.Fields {
				if f.Name == fname {
					found = true
					break
				}
			}
			if !found {
				c.errorAt(e.Pos, "%s has no field %q", e.Type, fname)
			}
			c.checkExpr(fval)
		}
		return TStruct{Name: e.Type}
	case *ast.FieldAccess:
		objT := c.checkExpr(e.Expr)
		if st, ok := objT.(TStruct); ok {
			decl, ok := c.structs[st.Name]
			if !ok {
				return TUnknown{}
			}
			for _, f := range decl.Fields {
				if f.Name == e.Field {
					return c.resolveTypeName(e.Pos, f.Type)
				}
			}
			c.errorAt(e.Pos, "%s has no field %q", st.Name, e.Field)
		} else if mod, ok := objT.(TModule); ok {
			if modScope, exists := c.modules[mod.Name]; exists {
				if t, ok := modScope.get(e.Field); ok {
					return t
				}
				c.errorAt(e.Pos, "module %q has no export %q", mod.Name, e.Field)
			}
			return TUnknown{}
		} else if !isUnknown(objT) {
			c.errorAt(e.Pos, "cannot access field of non-struct/module %s", objT)
		}
		return TUnknown{}
	case *ast.IndexExpr:
		objT := c.checkExpr(e.Expr)
		c.checkExpr(e.Index)
		if lt, ok := objT.(TList); ok {
			return lt.Elem
		}
		return TUnknown{}
	}
	return TUnknown{}
}

func (c *checker) checkFnLit(e *ast.FnLit, inferredParams []Type) Type {
	paramTypes := make([]Type, len(e.Params))
	
	child := newScope(c.env)
	for i, p := range e.Params {
		pt := c.resolveTypeName(e.Pos, p.Type)
		if isUnknown(pt) && inferredParams != nil && i < len(inferredParams) {
			pt = inferredParams[i]
		}
		paramTypes[i] = pt
		child.define(p.Name, pt)
	}
	
	prev := c.env
	c.env = child
	
	var actualRet Type = TUnit{}
	if e.Body.Expr != nil {
		actualRet = c.checkExpr(e.Body.Expr)
	} else {
		// Just check block without capturing a return value for now (unless we trace ReturnStmts)
		c.checkBlock(e.Body.Block)
	}
	
	c.env = prev
	
	ret := Type(TUnit{})
	if e.ReturnType != "" {
		ret = c.resolveTypeName(e.Pos, e.ReturnType)
	} else {
		ret = actualRet // Infer return type from expression body
	}
	
	return TFun{Params: paramTypes, Ret: ret}
}

func (c *checker) checkBinary(e *ast.BinaryOp) Type {
	leftT := c.checkExpr(e.Left)
	rightT := c.checkExpr(e.Right)

	if isUnknown(leftT) || isUnknown(rightT) {
		// Can't check; allow it
		return TUnknown{}
	}

	switch e.Op {
	case "+":
		if sameType(leftT, TString{}) && sameType(rightT, TString{}) {
			return TString{}
		}
		if _, ok := leftT.(TList); ok {
			if _, ok := rightT.(TList); ok {
				return leftT // list + list => same list type
			}
		}
		if isNumeric(leftT) && isNumeric(rightT) {
			if _, ok := leftT.(TFloat); ok {
				return TFloat{}
			}
			if _, ok := rightT.(TFloat); ok {
				return TFloat{}
			}
			return TInt{}
		}
		c.errorAt(e.Pos, "operator + not defined for %s and %s", leftT, rightT)
		return TUnknown{}
	case "-", "*", "/", "%":
		if isNumeric(leftT) && isNumeric(rightT) {
			if _, ok := leftT.(TFloat); ok {
				return TFloat{}
			}
			if _, ok := rightT.(TFloat); ok {
				return TFloat{}
			}
			return TInt{}
		}
		c.errorAt(e.Pos, "operator %s not defined for %s and %s", e.Op, leftT, rightT)
		return TUnknown{}
	case "==", "!=":
		return TBool{}
	case "<", "<=", ">", ">=":
		if !isNumeric(leftT) || !isNumeric(rightT) {
			if !sameType(leftT, TString{}) {
				c.errorAt(e.Pos, "cannot compare %s and %s with %s", leftT, rightT, e.Op)
			}
		}
		return TBool{}
	case "&&", "||":
		if _, ok := leftT.(TBool); !ok {
			c.errorAt(e.Pos, "left side of %s must be bool, got %s", e.Op, leftT)
		}
		if _, ok := rightT.(TBool); !ok {
			c.errorAt(e.Pos, "right side of %s must be bool, got %s", e.Op, rightT)
		}
		return TBool{}
	}
	return TUnknown{}
}

func (c *checker) checkCall(e *ast.CallExpr) Type {
	// Evaluate the callee type
	id, isIdent := e.Callee.(*ast.Ident)
	if !isIdent {
		// Non-ident callee; just check args
		c.checkExpr(e.Callee)
		for _, a := range e.Args {
			c.checkExpr(a)
		}
		return TUnknown{}
	}

	// Custom stream type checking:
	if id.Name == "stream" {
		if len(e.Args) != 1 {
			c.errorAt(e.Pos, "stream takes exactly 1 argument")
			return TStream{Elem: TUnknown{}}
		}
		argT := c.checkExpr(e.Args[0])
		if lt, ok := argT.(TList); ok {
			return TStream{Elem: lt.Elem}
		}
		c.errorAt(e.Pos, "stream requires a list, got %s", argT)
		return TStream{Elem: TUnknown{}}
	} else if id.Name == "collect" {
		if len(e.Args) != 1 {
			c.errorAt(e.Pos, "collect takes exactly 1 argument")
			return TList{Elem: TUnknown{}}
		}
		argT := c.checkExpr(e.Args[0])
		if st, ok := argT.(TStream); ok {
			return TList{Elem: st.Elem}
		}
		c.errorAt(e.Pos, "collect requires a stream, got %s", argT)
		return TList{Elem: TUnknown{}}
	} else if id.Name == "map" {
		if len(e.Args) != 2 {
			c.errorAt(e.Pos, "map takes exactly 2 arguments")
			return TStream{Elem: TUnknown{}}
		}
		argT := c.checkExpr(e.Args[0])
		
		elemType := Type(TUnknown{})
		if st, ok := argT.(TStream); ok {
			elemType = st.Elem
		} else {
			c.errorAt(e.Pos, "map requires a stream as first argument, got %s", argT)
		}
		
		retType := Type(TUnknown{})
		if fnLit, ok := e.Args[1].(*ast.FnLit); ok {
			ct := c.checkFnLit(fnLit, []Type{elemType})
			c.exprTypes[fnLit] = ct 
			retType = ct.(TFun).Ret
		} else {
			closureT := c.checkExpr(e.Args[1])
			if ct, ok := closureT.(TFun); ok {
				retType = ct.Ret
			}
		}
		return TStream{Elem: retType}
	} else if id.Name == "filter" {
		if len(e.Args) != 2 {
			c.errorAt(e.Pos, "filter takes exactly 2 arguments")
			return TStream{Elem: TUnknown{}}
		}
		argT := c.checkExpr(e.Args[0])
		
		elemType := Type(TUnknown{})
		if st, ok := argT.(TStream); ok {
			elemType = st.Elem
		} else {
			c.errorAt(e.Pos, "filter requires a stream as first argument, got %s", argT)
		}
		
		if fnLit, ok := e.Args[1].(*ast.FnLit); ok {
			ct := c.checkFnLit(fnLit, []Type{elemType})
			c.exprTypes[fnLit] = ct
			retType := ct.(TFun).Ret
			if !sameType(retType, TBool{}) && !isUnknown(retType) {
				c.errorAt(e.Pos, "filter closure must return bool, got %s", retType)
			}
		} else {
			closureT := c.checkExpr(e.Args[1])
			if ct, ok := closureT.(TFun); ok {
				if !sameType(ct.Ret, TBool{}) && !isUnknown(ct.Ret) {
					c.errorAt(e.Pos, "filter closure must return bool, got %s", ct.Ret)
				}
			}
		}
		return TStream{Elem: elemType}
	} else if id.Name == "vec_add" || id.Name == "vec_mul" {
		if len(e.Args) != 2 {
			c.errorAt(e.Pos, "%s takes exactly 2 arguments", id.Name)
			return TVec256{Elem: TUnknown{}}
		}
		arg0T := c.checkExpr(e.Args[0])
		arg1T := c.checkExpr(e.Args[1])
		if !sameType(arg0T, arg1T) && !isUnknown(arg0T) && !isUnknown(arg1T) {
			c.errorAt(e.Pos, "%s arguments must have the same type, got %s and %s", id.Name, arg0T, arg1T)
		}
		if _, ok := arg0T.(TVec256); !ok && !isUnknown(arg0T) {
			c.errorAt(e.Pos, "%s arguments must be vec256 types, got %s", id.Name, arg0T)
		}
		return arg0T
	}

	calleeT, ok := c.env.get(id.Name)
	if !ok {
		// Unknown function; check args and continue
		for _, a := range e.Args {
			c.checkExpr(a)
		}
		return TUnknown{}
	}

	fnT, isFn := calleeT.(TFun)
	if !isFn {
		c.errorAt(e.Pos, "%s is not a function (type: %s)", id.Name, calleeT)
		return TUnknown{}
	}

	// Check arg count (if the function has known params)
	if len(fnT.Params) > 0 && len(e.Args) != len(fnT.Params) {
		c.errorAt(e.Pos, "%s: expected %d arguments, got %d",
			id.Name, len(fnT.Params), len(e.Args))
	}

	subst := make(map[string]Type)
	// Check each arg
	for i, a := range e.Args {
		argT := c.checkExpr(a)
		if i < len(fnT.Params) {
			paramT := fnT.Params[i]
			if !unify(paramT, argT, subst) {
				c.errorAt(e.Pos, "%s: argument %d expected %s, got %s",
					id.Name, i+1, paramT, argT)
			}
		}
	}

	if fnT.Ret != nil {
		return substitute(fnT.Ret, subst)
	}
	return TUnknown{}
}

func unify(param, arg Type, subst map[string]Type) bool {
	if gen, ok := param.(TGenericVar); ok {
		if existing, present := subst[gen.Name]; present {
			return sameType(existing, arg)
		}
		subst[gen.Name] = arg
		return true
	}
	if pInst, pOk := param.(TInstantiated); pOk {
		aInst, aOk := arg.(TInstantiated)
		if !aOk || !sameType(pInst.Base, aInst.Base) || len(pInst.Args) != len(aInst.Args) {
			return false
		}
		for i := range pInst.Args {
			if !unify(pInst.Args[i], aInst.Args[i], subst) {
				return false
			}
		}
		return true
	}
	if pVec, pOk := param.(TVec256); pOk {
		aVec, aOk := arg.(TVec256)
		if !aOk {
			return false
		}
		return unify(pVec.Elem, aVec.Elem, subst)
	}
	return sameType(param, arg) || isUnknown(param) || isUnknown(arg)
}

func substitute(t Type, subst map[string]Type) Type {
	if gen, ok := t.(TGenericVar); ok {
		if rep, ok := subst[gen.Name]; ok {
			return rep
		}
		return t
	}
	if inst, ok := t.(TInstantiated); ok {
		newInst := TInstantiated{Base: inst.Base, Args: make([]Type, len(inst.Args))}
		for i, arg := range inst.Args {
			newInst.Args[i] = substitute(arg, subst)
		}
		return newInst
	}
	if fun, ok := t.(TFun); ok {
		newFun := TFun{Params: make([]Type, len(fun.Params)), Ret: substitute(fun.Ret, subst)}
		for i, p := range fun.Params {
			newFun.Params[i] = substitute(p, subst)
		}
		return newFun
	}
	if list, ok := t.(TList); ok {
		return TList{Elem: substitute(list.Elem, subst)}
	}
	if stream, ok := t.(TStream); ok {
		return TStream{Elem: substitute(stream.Elem, subst)}
	}
	if vec, ok := t.(TVec256); ok {
		return TVec256{Elem: substitute(vec.Elem, subst)}
	}
	if chanTy, ok := t.(TChan); ok {
		return TChan{Elem: substitute(chanTy.Elem, subst)}
	}
	return t
}

func splitTypeArgs(s string) []string {
	var args []string
	var currentArg strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '<' {
			depth++
		}
		if c == '>' {
			depth--
		}
		if c == ',' && depth == 0 {
			args = append(args, strings.TrimSpace(currentArg.String()))
			currentArg.Reset()
			continue
		}
		currentArg.WriteByte(c)
	}
	if currentArg.Len() > 0 {
		args = append(args, strings.TrimSpace(currentArg.String()))
	}
	return args
}

// resolveTypeName converts a type annotation string to a Type.
func (c *checker) resolveTypeName(pos ast.Position, name string) Type {
	if name == "" {
		return TUnknown{}
	}
	
	// Check for generic variables in scope
	if c.typeParams != nil && c.typeParams[name] {
		return TGenericVar{Name: name}
	}

	// Parse generics e.g. Result<int, string>
	idx := strings.IndexByte(name, '<')
	if idx != -1 && strings.HasSuffix(name, ">") {
		baseName := name[:idx]
		argsStr := name[idx+1 : len(name)-1]
		argStrs := splitTypeArgs(argsStr)
		
		var typeArgs []Type
		for _, argStr := range argStrs {
			typeArgs = append(typeArgs, c.resolveTypeName(pos, argStr))
		}
		
		baseType := c.resolveTypeName(pos, baseName)
		if _, ok := baseType.(TChan); ok && len(typeArgs) == 1 {
			return TChan{Elem: typeArgs[0]}
		}
		if baseName == "Result" && len(typeArgs) == 1 {
			return TResult{Elem: typeArgs[0]}
		}
		if baseName == "vec256" && len(typeArgs) == 1 {
			return TVec256{Elem: typeArgs[0]}
		}
		return TInstantiated{
			Base: baseType,
			Args: typeArgs,
		}
	}

	switch name {
	case "error":
		return TError{}
	case "()":
		return TUnit{}
	case "int":
		return TInt{}
	case "float":
		return TFloat{}
	case "string":
		return TString{}
	case "bool":
		return TBool{}
	case "list":
		return TList{Elem: TUnknown{}}
	case "stream":
		return TStream{Elem: TUnknown{}}
	case "vec256":
		return TVec256{Elem: TUnknown{}}
	case "chan":
		return TChan{Elem: TUnknown{}}
	case "fn":
		return TFun{}
	}

	// Parse u4, u12, etc. bit-width types
	if strings.HasPrefix(name, "u") {
		if bits, err := strconv.Atoi(name[1:]); err == nil && bits > 0 {
			return TUint{Bits: bits}
		}
	}

	// Might be a struct type
	if _, ok := c.structs[name]; ok {
		return TStruct{Name: name}
	}
	// Might be an enum type
	if _, ok := c.enums[name]; ok {
		return TEnum{Name: name} // the env also has the variants map
	}
	// Unknown — don't error, some types may not be declared yet
	return TUnknown{}
}

// resolveTypeExpr converts an ast.Expr (parsed as a type annotation) to a Type.
func (c *checker) resolveTypeExpr(e ast.Expr) Type {
	if ident, ok := e.(*ast.Ident); ok {
		return c.resolveTypeName(ident.Pos, ident.Name)
	}
	return TUnknown{}
}

// ============================================================================
// Type predicates
// ============================================================================

func isNumeric(t Type) bool {
	switch t.(type) {
	case TInt, TFloat, TUint:
		return true
	}
	return false
}

func isUnknown(t Type) bool {
	_, ok := t.(TUnknown)
	return ok || t == nil
}

func sameType(a, b Type) bool {
	if a == nil || b == nil {
		return false
	}
	switch a := a.(type) {
	case TInt:
		_, ok := b.(TInt)
		return ok
	case TUint:
		bu, ok := b.(TUint)
		return ok && a.Bits == bu.Bits
	case TFloat:
		_, ok := b.(TFloat)
		return ok
	case TString:
		_, ok := b.(TString)
		return ok
	case TBool:
		_, ok := b.(TBool)
		return ok
	case TUnit:
		_, ok := b.(TUnit)
		return ok
	case TList:
		bt, ok := b.(TList)
		if !ok {
			return false
		}
		if a.Elem == nil || bt.Elem == nil {
			return true
		}
		return sameType(a.Elem, bt.Elem)
	case TStream:
		bt, ok := b.(TStream)
		if !ok {
			return false
		}
		if a.Elem == nil || bt.Elem == nil {
			return true
		}
		return sameType(a.Elem, bt.Elem)
	case TVec256:
		bt, ok := b.(TVec256)
		if !ok {
			return false
		}
		if a.Elem == nil || bt.Elem == nil {
			return true
		}
		return sameType(a.Elem, bt.Elem)
	case TChan:
		bt, ok := b.(TChan)
		if !ok {
			return false
		}
		if a.Elem == nil || bt.Elem == nil || isUnknown(a.Elem) || isUnknown(bt.Elem) {
			return true
		}
		return sameType(a.Elem, bt.Elem)
	case TStruct:
		bt, ok := b.(TStruct)
		return ok && a.Name == bt.Name
	case TEnum:
		bt, ok := b.(TEnum)
		return ok && a.Name == bt.Name
	case TFun:
		_, ok := b.(TFun)
		return ok
	case TUnknown:
		return true
	case TError:
		_, ok := b.(TError)
		return ok
	case TResult:
		if _, isErr := b.(TError); isErr {
			return true // Result<T> accepts error
		}
		if bt, ok := b.(TResult); ok {
			if a.Elem == nil || bt.Elem == nil || isUnknown(a.Elem) || isUnknown(bt.Elem) {
				return true
			}
			return sameType(a.Elem, bt.Elem)
		}
		// Result<T> implicitly accepts T
		return sameType(a.Elem, b)
	case TGenericVar:
		bt, ok := b.(TGenericVar)
		return ok && a.Name == bt.Name
	case TInstantiated:
		bt, ok := b.(TInstantiated)
		if !ok || !sameType(a.Base, bt.Base) || len(a.Args) != len(bt.Args) {
			return false
		}
		for i := range a.Args {
			if !sameType(a.Args[i], bt.Args[i]) {
				return false
			}
		}
		return true
	}
	return false
}
