// Package ast defines the abstract syntax tree for Whale v0.1.
//
// An AST is a tree of Node values. Every concrete node type
// implements the Node interface. This package depends only on
// the standard library.
package ast

import (
	"strconv"
	"strings"
)

// Node is the interface implemented by every AST node.
type Node interface {
	nodeMarker()
	String() string
}

// Stmt is the interface implemented by statement-level nodes.
type Stmt interface {
	Node
	stmtMarker()
}

// Expr is the interface implemented by expression-level nodes.
type Expr interface {
	Node
	exprMarker()
}

// Position holds the source location of an AST node.
type Position struct {
	Line int
	Col  int
}

// File is the root of an AST.
type File struct {
	Body []Stmt
}

func (f File) String() string {
	out := ""
	for _, s := range f.Body {
		out += s.String() + "\n"
	}
	return out
}

// ============================================================================
// Statements
// ============================================================================

// ImportStmt is an import declaration: import "path" as alias
type ImportStmt struct {
	Pos   Position
	Path  string
	Alias string // If empty, derived from Path basename
}

func (s *ImportStmt) nodeMarker() {}
func (s *ImportStmt) stmtMarker() {}
func (s *ImportStmt) String() string {
	if s.Alias != "" {
		return "import \"" + s.Path + "\" as " + s.Alias + ";"
	}
	return "import \"" + s.Path + "\";"
}

// LetStmt is a `let` or `let mut` declaration.
//
//	let x = 5;
//	let mut count = 0;
//	let name: string = "whale";
type LetStmt struct {
	Pos     Position
	Name    string
	Mutable bool
	TypeAnn string // type annotation, or "" if none
	Value   Expr
}

func (s *LetStmt) nodeMarker() {}
func (s *LetStmt) stmtMarker() {}
func (s *LetStmt) String() string {
	mut := ""
	if s.Mutable {
		mut = " mut"
	}
	ann := ""
	if s.TypeAnn != "" {
		ann = ": " + s.TypeAnn
	}
	return "let" + mut + " " + s.Name + ann + " = " + s.Value.String() + ";"
}

// AssignStmt is an assignment to an existing mutable binding.
//
//	count = count + 1;
type AssignStmt struct {
	Pos   Position
	Name  string
	Value Expr
}

func (s *AssignStmt) nodeMarker() {}
func (s *AssignStmt) stmtMarker() {}
func (s *AssignStmt) String() string {
	return s.Name + " = " + s.Value.String() + ";"
}

// AssignFieldStmt is an assignment to a struct field.
//	obj.field = x;
type AssignFieldStmt struct {
	Pos    Position
	Object Expr
	Field  string
	Value  Expr
}

func (s *AssignFieldStmt) nodeMarker() {}
func (s *AssignFieldStmt) stmtMarker() {}
func (s *AssignFieldStmt) String() string {
	return s.Object.String() + "." + s.Field + " = " + s.Value.String() + ";"
}

// AssignIndexStmt is an assignment to a list or array index.
//	arr[i] = x;
type AssignIndexStmt struct {
	Pos   Position
	List  Expr
	Index Expr
	Value Expr
}

func (s *AssignIndexStmt) nodeMarker() {}
func (s *AssignIndexStmt) stmtMarker() {}
func (s *AssignIndexStmt) String() string {
	return s.List.String() + "[" + s.Index.String() + "] = " + s.Value.String() + ";"
}

// ExprStmt is an expression used as a statement.
//
//	print(x);
type ExprStmt struct {
	Pos  Position
	Expr Expr
}

func (s *ExprStmt) nodeMarker()  {}
func (s *ExprStmt) stmtMarker()  {}
func (s *ExprStmt) String() string { return s.Expr.String() + ";" }

// BlockStmt is a sequence of statements inside braces.
type BlockStmt struct {
	Pos  Position
	Body []Stmt
}

func (s *BlockStmt) nodeMarker() {}
func (s *BlockStmt) stmtMarker() {}
func (s *BlockStmt) String() string {
	out := "{\n"
	for _, stmt := range s.Body {
		out += "  " + stmt.String() + "\n"
	}
	out += "}"
	return out
}

// IfStmt is an if/else-if/else chain.
type IfStmt struct {
	Pos       Position
	Condition Expr
	Then      *BlockStmt
	Else      Stmt // may be *IfStmt (else if), *BlockStmt (else), or nil
}

func (s *IfStmt) nodeMarker() {}
func (s *IfStmt) stmtMarker() {}
func (s *IfStmt) String() string {
	out := "if " + s.Condition.String() + " " + s.Then.String()
	if s.Else != nil {
		out += " else " + s.Else.String()
	}
	return out
}

// WhileStmt is a while loop.
type WhileStmt struct {
	Pos       Position
	Condition Expr
	Body      *BlockStmt
}

// ArenaStmt represents an arena { ... } allocation block.
type ArenaStmt struct {
	Pos  Position
	Body *BlockStmt
}

func (s *ArenaStmt) nodeMarker() {}
func (s *ArenaStmt) stmtMarker() {}
func (s *ArenaStmt) String() string {
	return "arena " + s.Body.String()
}

func (s *WhileStmt) nodeMarker() {}
func (s *WhileStmt) stmtMarker() {}
func (s *WhileStmt) String() string {
	return "while " + s.Condition.String() + " " + s.Body.String()
}

// ForStmt is a for-in loop.
type ForStmt struct {
	Pos      Position
	Variable string
	Iterable Expr
	Body     *BlockStmt
}

func (s *ForStmt) nodeMarker() {}
func (s *ForStmt) stmtMarker() {}
func (s *ForStmt) String() string {
	return "for " + s.Variable + " in " + s.Iterable.String() + " " + s.Body.String()
}

// ReturnStmt is a return statement.
type ReturnStmt struct {
	Pos   Position
	Value Expr // may be nil for `return;`
}

func (s *ReturnStmt) nodeMarker() {}
func (s *ReturnStmt) stmtMarker() {}
func (s *ReturnStmt) String() string {
	if s.Value == nil {
		return "return;"
	}
	return "return " + s.Value.String() + ";"
}

// SpawnStmt is a spawn statement.
//
//	spawn do_work(1);
type SpawnStmt struct {
	Pos  Position
	Call *CallExpr
}

func (s *SpawnStmt) nodeMarker() {}
func (s *SpawnStmt) stmtMarker() {}
func (s *SpawnStmt) String() string {
	return "spawn " + s.Call.String() + ";"
}

// ChanSendStmt is a channel send statement.
//
//	ch <- 1;
type ChanSendStmt struct {
	Pos   Position
	Chan  Expr
	Value Expr
}

func (s *ChanSendStmt) nodeMarker() {}
func (s *ChanSendStmt) stmtMarker() {}
func (s *ChanSendStmt) String() string {
	return s.Chan.String() + " <- " + s.Value.String() + ";"
}

// Param is a function parameter: name and optional type annotation.
type FieldDecl struct {
	Name string
	Type string
}

// TraitDecl is a trait (interface) declaration.
//
//	trait Stringer { fn to_string(self) -> string; }
type TraitDecl struct {
	Pos      Position
	IsPublic bool
	Name     string
	Methods  []*FnStmt
}

func (s *TraitDecl) nodeMarker() {}
func (s *TraitDecl) stmtMarker() {}
func (s *TraitDecl) String() string {
	out := "trait " + s.Name + " {\n"
	for _, m := range s.Methods {
		out += "  " + m.String() + "\n"
	}
	out += "}"
	return out
}

// ImplDecl is an implementation of a trait for a struct.
//
//	impl Stringer for MyStruct { ... }
type ImplDecl struct {
	Pos        Position
	IsPublic   bool
	TraitName  string
	StructName string
	Methods    []*FnStmt
}

func (s *ImplDecl) nodeMarker() {}
func (s *ImplDecl) stmtMarker() {}
func (s *ImplDecl) String() string {
	out := "impl " + s.TraitName + " for " + s.StructName + " {\n"
	for _, m := range s.Methods {
		out += "  " + m.String() + "\n"
	}
	out += "}"
	return out
}

// Param is a function parameter: name and optional type annotation.
type Param struct {
	Name string
	Type string
}

// FnDecl is a top-level function declaration.
//
//	fn add(a: int, b: int) -> int { return a + b; }
type FnStmt struct {
	Pos        Position
	IsPublic   bool
	Name       string
	TypeParams []string
	Params     []Param
	ReturnType string
	Body       *BlockStmt
}

func (s *FnStmt) nodeMarker() {}
func (s *FnStmt) stmtMarker() {}
func (s *FnStmt) String() string {
	out := "fn " + s.Name
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
	if s.Body != nil {
		out += " " + s.Body.String()
	} else {
		out += ";"
	}
	return out
}

// ExternFnStmt is a top-level foreign function declaration.
//	extern fn printf(fmt: string, val: int) -> int;
type ExternFnStmt struct {
	Pos        Position
	Name       string
	Params     []Param
	ReturnType string
}

func (s *ExternFnStmt) nodeMarker() {}
func (s *ExternFnStmt) stmtMarker() {}
func (s *ExternFnStmt) String() string {
	out := "extern fn " + s.Name + "("
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
}

// StructDecl is a struct type declaration.
//
//	struct Point { x: int, y: int }
type StructDecl struct {
	Pos        Position
	IsPublic   bool
	Name       string
	TypeParams []string
	Fields     []Param
	Packed     bool
}

func (s *StructDecl) nodeMarker() {}
func (s *StructDecl) stmtMarker() {}
func (s *StructDecl) String() string {
	out := ""
	if s.Packed {
		out += "packed "
	}
	out += "struct " + s.Name
	if len(s.TypeParams) > 0 {
		out += "<" + strings.Join(s.TypeParams, ", ") + ">"
	}
	out += " {\n"
	for _, f := range s.Fields {
		out += "  " + f.Name + ": " + f.Type + ",\n"
	}
	out += "}"
	return out
}

// ============================================================================
// Expressions
// ============================================================================

// IntLit is an integer literal.
type IntLit struct {
	Pos   Position
	Value int64
}

func (e *IntLit) nodeMarker() {}
func (e *IntLit) exprMarker() {}
func (e *IntLit) String() string { return strconv.FormatInt(e.Value, 10) }

// FloatLit is a floating-point literal.
type FloatLit struct {
	Pos   Position
	Value float64
}

func (e *FloatLit) nodeMarker() {}
func (e *FloatLit) exprMarker() {}
func (e *FloatLit) String() string {
	s := strconv.FormatFloat(e.Value, 'g', -1, 64)
	hasDot := false
	for i := 0; i < len(s); i++ {
		if s[i] == '.' || s[i] == 'e' || s[i] == 'E' {
			hasDot = true
			break
		}
	}
	if !hasDot {
		s += ".0"
	}
	return s
}

// StringLit is a string literal.
type StringLit struct {
	Pos   Position
	Value string
}

func (e *StringLit) nodeMarker() {}
func (e *StringLit) exprMarker() {}
func (e *StringLit) String() string {
	out := `"`
	for i := 0; i < len(e.Value); i++ {
		c := e.Value[i]
		switch c {
		case '"':
			out += `\"`
		case '\\':
			out += `\\`
		case '\n':
			out += `\n`
		case '\t':
			out += `\t`
		default:
			out += string(c)
		}
	}
	out += `"`
	return out
}

// BoolLit is a boolean literal.
type BoolLit struct {
	Pos   Position
	Value bool
}

func (e *BoolLit) nodeMarker() {}
func (e *BoolLit) exprMarker() {}
func (e *BoolLit) String() string {
	if e.Value {
		return "true"
	}
	return "false"
}

// Ident is a reference to a named binding.
type Ident struct {
	Pos  Position
	Name string
}

func (e *Ident) nodeMarker() {}
func (e *Ident) exprMarker() {}
func (e *Ident) String() string { return e.Name }

// UnaryOp is a unary operator expression: -x, !x.
type UnaryOp struct {
	Pos  Position
	Op   string
	Expr Expr
}

func (e *UnaryOp) nodeMarker() {}
func (e *UnaryOp) exprMarker() {}
func (e *UnaryOp) String() string { return "(" + e.Op + e.Expr.String() + ")" }

// ChanRecvExpr is a channel receive expression: <-ch.
type ChanRecvExpr struct {
	Pos  Position
	Chan Expr
}

func (e *ChanRecvExpr) nodeMarker() {}
func (e *ChanRecvExpr) exprMarker() {}
func (e *ChanRecvExpr) String() string { return "(<-" + e.Chan.String() + ")" }

// BinaryOp is a binary operator expression: a + b, x * 2, etc.
type BinaryOp struct {
	Pos   Position
	Op    string
	Left  Expr
	Right Expr
}

func (e *BinaryOp) nodeMarker() {}
func (e *BinaryOp) exprMarker() {}
func (e *BinaryOp) String() string {
	return "(" + e.Left.String() + " " + e.Op + " " + e.Right.String() + ")"
}

// CallExpr is a function call: print(x), add(1, 2).
// Callee is an expression (typically an Ident but can be any expr).
type CallExpr struct {
	Pos    Position
	Callee Expr
	Args   []Expr
}

func (e *CallExpr) nodeMarker() {}
func (e *CallExpr) exprMarker() {}
func (e *CallExpr) String() string {
	out := e.Callee.String() + "("
	for i, a := range e.Args {
		if i > 0 {
			out += ", "
		}
		out += a.String()
	}
	out += ")"
	return out
}

// ListLit is a list literal: [1, 2, 3], [], [x, y + 1].
type ListLit struct {
	Pos    Position
	Values []Expr
}

func (e *ListLit) nodeMarker() {}
func (e *ListLit) exprMarker() {}
func (e *ListLit) String() string {
	out := "["
	for i, v := range e.Values {
		if i > 0 {
			out += ", "
		}
		out += v.String()
	}
	out += "]"
	return out
}

// FnBody is the body of a function value. Exactly one of Expr or Block is non-nil.
type FnBody struct {
	Expr  Expr       // arrow form: => expr
	Block *BlockStmt // block form: { ... }
}

// FnLit is a function value (anonymous function / lambda).
//
//	fn(x: int) -> int { return x * 2; }
//	x => x * 2
type FnLit struct {
	Pos        Position
	Params     []Param
	ReturnType string
	Body       *FnBody
}

func (e *FnLit) nodeMarker() {}
func (e *FnLit) exprMarker() {}
func (e *FnLit) String() string {
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
		out += " => " + e.Body.Expr.String()
	} else if e.Body.Block != nil {
		out += " " + e.Body.Block.String()
	}
	return out
}

// StructLit is a struct value: Point { x: 3, y: 4 }.
type StructLit struct {
	Pos    Position
	Type   string
	Fields map[string]Expr
}

func (e *StructLit) nodeMarker() {}
func (e *StructLit) exprMarker() {}
func (e *StructLit) String() string {
	out := e.Type + " { "
	first := true
	for k, v := range e.Fields {
		if !first {
			out += ", "
		}
		first = false
		out += k + ": " + v.String()
	}
	out += " }"
	return out
}

// FieldAccess is `expr.field` — access a named field of a struct value.
type FieldAccess struct {
	Pos   Position
	Expr  Expr
	Field string
}

func (e *FieldAccess) nodeMarker() {}
func (e *FieldAccess) exprMarker() {}
func (e *FieldAccess) String() string { return e.Expr.String() + "." + e.Field }

// IndexExpr is `expr[index]` — index into a list.
type IndexExpr struct {
	Pos   Position
	Expr  Expr
	Index Expr
}

func (e *IndexExpr) nodeMarker() {}
func (e *IndexExpr) exprMarker() {}
func (e *IndexExpr) String() string {
	return e.Expr.String() + "[" + e.Index.String() + "]"
}

// ComptimeExpr is a compile-time evaluated expression.
//
//	comptime foo()
type ComptimeExpr struct {
	Pos  Position
	Expr Expr
}

func (e *ComptimeExpr) nodeMarker() {}
func (e *ComptimeExpr) exprMarker() {}
func (e *ComptimeExpr) String() string {
	return "comptime " + e.Expr.String()
}

// ErrorLit is `error("msg")`
type ErrorLit struct {
	Pos Position
	Msg Expr
}

func (e *ErrorLit) nodeMarker() {}
func (e *ErrorLit) exprMarker() {}
func (e *ErrorLit) String() string {
	return "error(" + e.Msg.String() + ")"
}

// TryExpr is `expr?`
type TryExpr struct {
	Pos  Position
	Expr Expr
}

func (e *TryExpr) nodeMarker() {}
func (e *TryExpr) exprMarker() {}
func (e *TryExpr) String() string {
	return e.Expr.String() + "?"
}

// ============================================================================
// Enums & Pattern Matching
// ============================================================================

// EnumDecl is `enum Name { VariantA(Type), VariantB }`
type EnumDecl struct {
	Pos        Position
	Name       string
	TypeParams []string
	Variants   []*EnumVariant
}

func (e *EnumDecl) nodeMarker() {}
func (e *EnumDecl) stmtMarker() {}
func (e *EnumDecl) String() string {
	out := "enum " + e.Name
	if len(e.TypeParams) > 0 {
		out += "<" + strings.Join(e.TypeParams, ", ") + ">"
	}
	out += " { ... }"
	return out
}

// EnumVariant represents a single variant inside an enum.
type EnumVariant struct {
	Pos  Position
	Name string
	Type Expr // nil if no payload
}

func (v *EnumVariant) nodeMarker() {}
func (v *EnumVariant) String() string {
	if v.Type != nil {
		return v.Name + "(" + v.Type.String() + ")"
	}
	return v.Name
}

// MatchExpr is `match expr { VariantA(payload) => ..., VariantB => ... }`
type MatchExpr struct {
	Pos  Position
	Expr Expr
	Arms []*MatchArm
}

func (e *MatchExpr) nodeMarker() {}
func (e *MatchExpr) exprMarker() {}
func (e *MatchExpr) String() string {
	return "match " + e.Expr.String() + " { ... }"
}

// MatchArm is a single case in a match expression.
type MatchArm struct {
	Pos        Position
	Variant    string // The variant name to match, e.g., "Ok"
	Binding    string // The variable to bind the payload to, e.g., "data" (empty if none)
	IsCatchAll bool   // true if this is the `_ => ...` arm
	Body       Expr
}

func (a *MatchArm) nodeMarker() {}
func (a *MatchArm) String() string {
	if a.IsCatchAll {
		return "_ => " + a.Body.String()
	}
	if a.Binding != "" {
		return a.Variant + "(" + a.Binding + ") => " + a.Body.String()
	}
	return a.Variant + " => " + a.Body.String()
}
