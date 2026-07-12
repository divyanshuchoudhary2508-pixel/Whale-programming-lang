package llvm

// ============================================================================
// AST definitions
//
// This is the AST shape the codegen backend below consumes. If your team's
// parser already produces a different AST, you do NOT need to rewrite it —
// just write a small "lowering" pass that converts your real AST into these
// structs (or rename these structs to match yours; the codegen logic only
// depends on the shape, not the names). That lowering step is usually
// 30-60 minutes of mechanical work, not a rewrite.
//
// Deliberately covers only what's needed for a real Phase 7 proof:
//   let bindings, functions, if/else, arithmetic, comparisons, function
//   calls, return, and print(...) (mapped to libc printf).
//
// Streams, pipelines, and the type system are NOT lowered to native code
// yet — that's a deliberate scope cut (see README.md "What's NOT covered").
// ============================================================================

// Program is the root node: a list of top-level function declarations.
// Whale requires a `fn main() { ... }` entry point, same as Go/Rust/C.
type Program struct {
	Structs    []*StructDecl
	Enums      []*EnumDecl
	Functions  []*FuncDecl
	Externs    []*ExternFuncDecl
	GlobalVars []*GlobalVarDecl
}

type GlobalVarDecl struct {
	Name     string
	Type     Type
	Value    int64
	ZeroInit bool
}

// StructDecl: struct Name { Fields... }
type StructDecl struct {
	Name   string
	Fields []Param
	Packed bool
}

// EnumDecl: enum Name { Variants... }
type EnumDecl struct {
	Name     string
	Variants []EnumVariant
}

type EnumVariant struct {
	Name string
	Type Type // empty if no payload
}

// FuncDecl is a function definition: fn name(params) { body }
type FuncDecl struct {
	Name       string
	Params     []Param
	ReturnType Type // return type of the function
	Body       []Stmt
}

// ExternFuncDecl is an external C function declaration: extern fn printf(fmt: string) -> int;
type ExternFuncDecl struct {
	Name       string
	Params     []Param
	ReturnType Type // return type of the function
}

type Param struct {
	Name string
	Type Type
}

// Type is a minimal type tag.
type Type string

const (
	TypeInt    Type = "int"
	TypeFloat  Type = "float"
	TypeBool   Type = "bool"
	TypeString Type = "string"
	TypeVoid   Type = "void"
)

// ---------------------------------------------------------------------------
// Statements
// ---------------------------------------------------------------------------

type Stmt interface{ stmtNode() }

// LetStmt: let x = <expr>
type LetStmt struct {
	Name  string
	Type  Type // declared/inferred type of the binding
	Value Expr
}

// ReturnStmt: return <expr>  (Value may be nil for bare `return`)
type ReturnStmt struct {
	Value Expr
}

// IfStmt: if <cond> { Then } else { Else }   (Else may be nil)
type BreakStmt struct{}
type ContinueStmt struct{}

type IfStmt struct {
	Cond Expr
	Then []Stmt
	Else []Stmt
}

// ArenaStmt: arena { ... }
type ArenaStmt struct {
	Body []Stmt
}

// ExprStmt wraps an expression used as a statement, e.g. a bare function call:
// print("hello")
type ExprStmt struct {
	X Expr
}

// WhileStmt: while cond { body }
type WhileStmt struct {
	Cond Expr
	Body []Stmt
}

// AssignStmt: name = value
type AssignStmt struct {
	Name  string
	Value Expr
}

// AssignFieldStmt: obj.field = value
type AssignFieldStmt struct {
	Object Expr
	Field  string
	Value  Expr
}

type AssignDereferenceStmt struct {
	Pointer Expr
	Value   Expr
}

// AssignIndexStmt: arr[index] = value
type AssignIndexStmt struct {
	List  Expr
	Index Expr
	Value Expr
}

func (*LetStmt) stmtNode()               {}
func (*ReturnStmt) stmtNode()            {}
func (*IfStmt) stmtNode()                {}
func (*ExprStmt) stmtNode()              {}
func (*WhileStmt) stmtNode()             {}
func (*BreakStmt) stmtNode()             {}
func (*ContinueStmt) stmtNode()          {}
func (*AssignStmt) stmtNode()            {}
func (*AssignFieldStmt) stmtNode()       {}
func (*AssignDereferenceStmt) stmtNode() {}
func (*AssignIndexStmt) stmtNode()       {}
func (*SpawnStmt) stmtNode()             {}
func (*ChanSendStmt) stmtNode()          {}
func (*ArenaStmt) stmtNode()             {}

// SpawnStmt: spawn call
type SpawnStmt struct {
	Call *CallExpr
}

// ChanSendStmt: chan <- val
type ChanSendStmt struct {
	Chan  Expr
	Value Expr
}

// ---------------------------------------------------------------------------
// Expressions
// ---------------------------------------------------------------------------

type Expr interface{ exprNode() }

// IntLit: an integer literal, e.g. 42
type IntLit struct{ Value int64 }

// FloatLit: a float literal, e.g. 3.14
type FloatLit struct{ Value float64 }

// BoolLit: true / false
type BoolLit struct{ Value bool }

// StringLit: "hello" — codegen emits this as a global constant + pointer.
type StringLit struct{ Value string }

// Ident: a variable reference, e.g. x
type Ident struct{ Name string }

// BinaryExpr: left OP right, e.g. x + 1, a > b
type BinaryExpr struct {
	Op    string // "+" "-" "*" "/" "<" ">" "<=" ">=" "==" "!="
	Left  Expr
	Right Expr
}

// CallExpr: fn_name(args...)
type CallExpr struct {
	Callee string
	Args   []Expr
}

// MatchExpr: match Expr { Arms... }
type MatchExpr struct {
	Expr Expr
	Arms []MatchArm
}

// MatchArm: Variant(Binding) => Body
type MatchArm struct {
	Variant    string
	Binding    string // empty if no payload binding
	Body       Expr
	IsCatchAll bool // true if `_ =>`
}

// ConstructEnumExpr: Name(Payload)
type ConstructEnumExpr struct {
	EnumName string
	Variant  string
	Payload  Expr // nil if no payload
}

// StructLit: StructName { field: value, ... }
type StructLit struct {
	StructName string
	Fields     map[string]Expr
}

// FieldAccess: Object.Field
type FieldAccess struct {
	Object Expr
	Field  string
}

type AddressOfExpr struct {
	Expr Expr
}

type DereferenceExpr struct {
	Expr Expr
}

type CastExpr struct {
	Expr     Expr
	TargetTy string // e.g. "u8", "u16", "u32", "u64", "int"
}

type AsmExpr struct {
	Template   string
	Clobbers   []string
	SideEffect bool
}

func (*IntLit) exprNode()      {}
func (*FloatLit) exprNode()    {}
func (*BoolLit) exprNode()     {}
func (*StringLit) exprNode()   {}
func (*Ident) exprNode()       {}
func (*BinaryExpr) exprNode()  {}
func (*CallExpr) exprNode()    {}
func (*StructLit) exprNode()   {}
func (*FieldAccess) exprNode() {}

// ListLit: [val1, val2]
type ListLit struct {
	ElementType Type
	Values      []Expr
}

// IndexExpr: List[Index]
type IndexExpr struct {
	List  Expr
	Index Expr
}

// LenExpr: length of an array
type LenExpr struct {
	List Expr
}

// AllocArray: allocates an uninitialized array of given length
type AllocArray struct {
	ElementType Type
	Length      Expr
}

func (*ListLit) exprNode()           {}
func (*IndexExpr) exprNode()         {}
func (*LenExpr) exprNode()           {}
func (*AllocArray) exprNode()        {}
func (*MatchExpr) exprNode()         {}
func (*ConstructEnumExpr) exprNode() {}
func (*ErrorLit) exprNode()          {}
func (*TryExpr) exprNode()           {}
func (*ChanRecvExpr) exprNode()      {}
func (*AddressOfExpr) exprNode()     {}
func (*DereferenceExpr) exprNode()   {}
func (*CastExpr) exprNode()          {}
func (*AsmExpr) exprNode()           {}

// ChanRecvExpr: <-chan
type ChanRecvExpr struct {
	Chan Expr
}

type ErrorLit struct {
	Msg Expr
}

type TryExpr struct {
	Expr Expr
}
