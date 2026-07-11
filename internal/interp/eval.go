// Package interp implements the Whale tree-walking interpreter.
//
// The interpreter evaluates an AST produced by the parser, carrying
// an environment of bindings. It handles:
//   - Primitive types: int, float, string, bool
//   - Compound types: list, struct, function, stream
//   - Control flow: if/else, while, for-in
//   - Functions with closures and recursion
//   - Lambdas (arrow and block forms)
//   - Streams with lazy evaluation
//   - Pipe operator |> desugared to function calls by the parser
//   - File I/O builtins
package interp

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/whale-lang/whale/internal/ast"
	"github.com/whale-lang/whale/internal/lexer"
	"github.com/whale-lang/whale/internal/parser"
)

// ============================================================================
// Values
// ============================================================================

// Value is any Whale runtime value.
type Value interface {
	valueMarker()
	String() string
}

type enumValue struct {
	Variant string
	Payload Value // nil if no payload
}

func (enumValue) valueMarker() {}
func (v enumValue) String() string {
	if v.Payload != nil {
		return fmt.Sprintf("%s(%s)", v.Variant, v.Payload.String())
	}
	return v.Variant
}

type errorValue struct {
	msg   string
	trace []string
}

func (errorValue) valueMarker() {}
func (v errorValue) String() string {
	out := "Error: " + v.msg
	for _, t := range v.trace {
		out += "\n  at " + t
	}
	return out
}

type nullValue struct{}

func (nullValue) valueMarker()    {}
func (nullValue) String() string  { return "()" }

type intValue struct{ v int64 }

func (intValue) valueMarker()    {}
func (v intValue) String() string { return strconv.FormatInt(v.v, 10) }

type floatValue struct{ v float64 }

func (floatValue) valueMarker()    {}
func (v floatValue) String() string {
	s := strconv.FormatFloat(v.v, 'g', -1, 64)
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

type stringValue struct{ v string }

func (stringValue) valueMarker()    {}
func (v stringValue) String() string { return v.v }

type boolValue struct{ v bool }

func (boolValue) valueMarker()    {}
func (v boolValue) String() string {
	if v.v {
		return "true"
	}
	return "false"
}

type listValue struct{ elements []Value }

func (listValue) valueMarker() {}
func (v listValue) String() string {
	parts := make([]string, len(v.elements))
	for i, el := range v.elements {
		parts[i] = el.String()
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

type structValue struct {
	typeName string
	fields   map[string]Value
}

func (structValue) valueMarker() {}
func (v structValue) String() string {
	parts := make([]string, 0, len(v.fields))
	for k, val := range v.fields {
		parts = append(parts, k+": "+val.String())
	}
	return v.typeName + " { " + strings.Join(parts, ", ") + " }"
}

// functionValue is a Whale function (named or anonymous).
// It carries the captured environment (closure), params, and body.
type functionValue struct {
	name       string // for debugging; "" for anonymous
	params     []ast.Param
	returnType string
	body       *ast.FnBody
	closure    *Environment
}

func (functionValue) valueMarker() {}
func (v functionValue) String() string {
	if v.name != "" {
		return "<fn " + v.name + ">"
	}
	return "<fn>"
}

type nativeFnValue struct {
	name string
	fn   func(args []Value) Value
}

func (nativeFnValue) valueMarker() {}
func (v nativeFnValue) String() string {
	return "native fn " + v.name
}

// streamValue is a lazy, pull-based sequence of values.
// next() returns (value, true) for each element, then (nullValue{}, false) at end.
type streamValue struct {
	next func() (Value, bool)
}

func (streamValue) valueMarker() {}
func (v streamValue) String() string {
	return "<stream>"
}

type chanValue struct {
	ch chan Value
}

func (chanValue) valueMarker() {}
func (v chanValue) String() string { return "<chan>" }

// ============================================================================
// Return signal (panic-based control flow)
// ============================================================================

// returnSignal is used as a panic value to unwind out of a function body
// when return is executed. Caught at the function-call boundary.
type returnSignal struct{ value Value }

// ============================================================================
// Environment
// ============================================================================

// Environment implements lexical scoping via a chain of scopes.
type Environment struct {
	Values map[string]Value
	Mut    map[string]bool
	Parent *Environment
}

func newEnvironment(parent *Environment) *Environment {
	return &Environment{
		Values: make(map[string]Value),
		Mut:    make(map[string]bool),
		Parent: parent,
	}
}

func (e *Environment) define(name string, val Value, mutable bool) error {
	if _, exists := e.Values[name]; exists {
		return fmt.Errorf("name %q is already defined in this scope", name)
	}
	e.Values[name] = val
	e.Mut[name] = mutable
	return nil
}

func (e *Environment) get(name string) (Value, bool) {
	if v, ok := e.Values[name]; ok {
		return v, true
	}
	if e.Parent != nil {
		return e.Parent.get(name)
	}
	return nil, false
}

func (e *Environment) set(name string, val Value) error {
	if _, ok := e.Values[name]; ok {
		if !e.Mut[name] {
			return fmt.Errorf("cannot reassign immutable binding %q (declare with 'let mut')", name)
		}
		e.Values[name] = val
		return nil
	}
	if e.Parent != nil {
		return e.Parent.set(name, val)
	}
	return fmt.Errorf("undefined name: %q", name)
}

// ============================================================================
// Interpreter
// ============================================================================

// typeRegistry maps struct type names to their declarations.
var typeRegistry = make(map[string]*ast.StructDecl)

// DebugHooks allows external debuggers to pause and inspect execution.
type DebugHooks interface {
	BeforeNode(node ast.Node, env *Environment)
}

// Interpreter holds the interpreter state.
type Interpreter struct {
	env    *Environment
	errs   []string
	output strings.Builder
	debug  DebugHooks
}

// New creates a new Interpreter with an empty global environment.
func New() *Interpreter {
	return NewWithDebug(nil)
}

// NewWithDebug creates an interpreter with debug hooks.
func NewWithDebug(debug DebugHooks) *Interpreter {
	return &Interpreter{
		env:   newEnvironment(nil),
		debug: debug,
	}
}

// RunFile parses and runs a Whale source file, returning output and errors.
func RunFile(src string) (string, []string) {
	res := lexer.Lex(src)
	if len(res.Errors) > 0 {
		errs := make([]string, len(res.Errors))
		for i, e := range res.Errors {
			errs[i] = e.Error()
		}
		return "", errs
	}
	parsed := parser.Parse(res.Tokens)
	if len(parsed.Errors) > 0 {
		errs := make([]string, len(parsed.Errors))
		for i, e := range parsed.Errors {
			errs[i] = e.Error()
		}
		return "", errs
	}
	// Reset type registry for each run
	typeRegistry = make(map[string]*ast.StructDecl)
	i := New()
	i.evalFile(parsed.File)
	return i.output.String(), i.errs
}

// RunAST runs an already parsed and type-checked AST.
func RunAST(file ast.File) (string, []string) {
	typeRegistry = make(map[string]*ast.StructDecl)
	i := New()
	i.evalFile(file)
	return i.output.String(), i.errs
}

// EvalExprComptime evaluates an expression at compile-time and returns an AST literal.
func EvalExprComptime(expr ast.Expr, file ast.File) (ast.Expr, error) {
	// 1. Create a fresh interpreter
	i := New()
	
	// 2. Pre-load all functions and structs into the environment so they can be called
	for _, stmt := range file.Body {
		switch s := stmt.(type) {
		case *ast.FnStmt:
			i.evalFnDecl(s)
		case *ast.StructDecl:
			i.evalStructDecl(s)
		case *ast.EnumDecl:
			i.evalEnumDecl(s)
		}
	}
	
	// 3. Evaluate the expression
	val := i.evalExpr(expr)
	if len(i.errs) > 0 {
		return nil, fmt.Errorf("comptime error: %s", strings.Join(i.errs, "\n"))
	}
	
	// 4. Convert the value back to an AST literal
	switch v := val.(type) {
	case intValue:
		return &ast.IntLit{Pos: ast.Position{}, Value: v.v}, nil
	case floatValue:
		return &ast.FloatLit{Pos: ast.Position{}, Value: v.v}, nil
	case stringValue:
		return &ast.StringLit{Pos: ast.Position{}, Value: v.v}, nil
	case boolValue:
		return &ast.BoolLit{Pos: ast.Position{}, Value: v.v}, nil
	default:
		return nil, fmt.Errorf("comptime evaluated to unsupported type %T for AST injection", val)
	}
}

// RunFileWithDebug parses and runs a Whale source file with a debugger attached.
func RunFileWithDebug(srcFile string, debug DebugHooks) (string, []string) {
	src, err := os.ReadFile(srcFile)
	if err != nil {
		return "", []string{err.Error()}
	}
	res := lexer.Lex(string(src))
	if len(res.Errors) > 0 {
		errs := make([]string, len(res.Errors))
		for j, e := range res.Errors {
			errs[j] = e.Error()
		}
		return "", errs
	}
	parsed := parser.Parse(res.Tokens)
	if len(parsed.Errors) > 0 {
		errs := make([]string, len(parsed.Errors))
		for j, e := range parsed.Errors {
			errs[j] = e.Error()
		}
		return "", errs
	}
	typeRegistry = make(map[string]*ast.StructDecl)
	i := NewWithDebug(debug)
	i.evalFile(parsed.File)
	return i.output.String(), i.errs
}

// Exec evaluates a source string in this interpreter's environment.
// Used by the REPL for stateful evaluation.
func (i *Interpreter) Exec(src string) (string, []string) {
	res := lexer.Lex(src)
	if len(res.Errors) > 0 {
		errs := make([]string, len(res.Errors))
		for j, e := range res.Errors {
			errs[j] = e.Error()
		}
		return "", errs
	}
	parsed := parser.Parse(res.Tokens)
	if len(parsed.Errors) > 0 {
		errs := make([]string, len(parsed.Errors))
		for j, e := range parsed.Errors {
			errs[j] = e.Error()
		}
		return "", errs
	}
	i.output.Reset()
	i.errs = nil
	i.evalFile(parsed.File)
	return i.output.String(), i.errs
}

func (i *Interpreter) err(pos ast.Position, msg string) Value {
	i.errs = append(i.errs, fmt.Sprintf("%d:%d: %s", pos.Line, pos.Col, msg))
	return nullValue{}
}

func (i *Interpreter) errStr(msg string) Value {
	i.errs = append(i.errs, msg)
	return nullValue{}
}

// ============================================================================
// Evaluation — statements
// ============================================================================

func (i *Interpreter) evalFile(f ast.File) {
	for _, stmt := range f.Body {
		i.evalStmt(stmt)
		if len(i.errs) > 0 {
			return
		}
	}
}

func (i *Interpreter) evalStmt(s ast.Stmt) {
	if s == nil || len(i.errs) > 0 {
		return
	}
	if i.debug != nil {
		i.debug.BeforeNode(s, i.env)
	}
	switch s := s.(type) {
	case *ast.LetStmt:
		i.evalLet(s)
	case *ast.AssignStmt:
		i.evalAssign(s)
	case *ast.ExprStmt:
		i.evalExpr(s.Expr)
	case *ast.IfStmt:
		i.evalIf(s)
	case *ast.WhileStmt:
		i.evalWhile(s)
	case *ast.ArenaStmt:
		i.evalBlock(s.Body)
	case *ast.ForStmt:
		i.evalFor(s)
	case *ast.ReturnStmt:
		i.evalReturn(s)
	case *ast.FnStmt:
		i.evalFnDecl(s)
	case *ast.StructDecl:
		i.evalStructDecl(s)
	case *ast.EnumDecl:
		i.evalEnumDecl(s)
	case *ast.SpawnStmt:
		i.evalSpawn(s)
	case *ast.ChanSendStmt:
		i.evalChanSend(s)
	case *ast.ExternFnStmt:
		if nativeFn, ok := FFIRegistry[s.Name]; ok {
			i.env.define(s.Name, nativeFnValue{name: s.Name, fn: nativeFn}, false)
		} else {
			// If not in registry, define a dummy that panics when called
			dummy := nativeFnValue{name: s.Name, fn: func(args []Value) Value {
				i.err(s.Pos, "unresolved extern function: "+s.Name)
				return nullValue{}
			}}
			i.env.define(s.Name, dummy, false)
		}
	case *ast.TraitDecl:
		// Traits are purely for compile-time type checking. Ignore at runtime.
	case *ast.ImplDecl:
		// Evaluate the methods so they exist in the environment (e.g. for module exports)
		for _, method := range s.Methods {
			originalName := method.Name
			method.Name = s.StructName + "_" + method.Name
			i.evalFnDecl(method)
			method.Name = originalName
		}
	case *ast.BlockStmt:
		i.evalBlock(s)
	default:
		i.errStr(fmt.Sprintf("unknown statement type: %T", s))
	}
}

func (i *Interpreter) evalLet(s *ast.LetStmt) {
	val := i.evalExpr(s.Value)
	if len(i.errs) > 0 {
		return
	}
	if err := i.env.define(s.Name, val, s.Mutable); err != nil {
		i.err(s.Pos, err.Error())
	}
}

func (i *Interpreter) evalAssign(s *ast.AssignStmt) {
	val := i.evalExpr(s.Value)
	if len(i.errs) > 0 {
		return
	}
	if err := i.env.set(s.Name, val); err != nil {
		i.err(s.Pos, err.Error())
	}
}

func (i *Interpreter) evalIf(s *ast.IfStmt) {
	cond := i.evalExpr(s.Condition)
	if len(i.errs) > 0 {
		return
	}
	bv, ok := cond.(boolValue)
	if !ok {
		i.err(s.Pos, fmt.Sprintf("if condition must be bool, got %T", cond))
		return
	}
	if bv.v {
		i.evalBlock(s.Then)
	} else if s.Else != nil {
		i.evalStmt(s.Else)
	}
}

func (i *Interpreter) evalWhile(s *ast.WhileStmt) {
	for {
		cond := i.evalExpr(s.Condition)
		if len(i.errs) > 0 {
			return
		}
		bv, ok := cond.(boolValue)
		if !ok {
			i.err(s.Pos, fmt.Sprintf("while condition must be bool, got %T", cond))
			return
		}
		if !bv.v {
			break
		}
		i.evalBlock(s.Body)
		if len(i.errs) > 0 {
			return
		}
	}
}

func (i *Interpreter) evalFor(s *ast.ForStmt) {
	iter := i.evalExpr(s.Iterable)
	if len(i.errs) > 0 {
		return
	}
	switch col := iter.(type) {
	case listValue:
		for _, elem := range col.elements {
			child := newEnvironment(i.env)
			if err := child.define(s.Variable, elem, false); err != nil {
				i.err(s.Pos, err.Error())
				return
			}
			prev := i.env
			i.env = child
			i.evalBlock(s.Body)
			i.env = prev
			if len(i.errs) > 0 {
				return
			}
		}
	case streamValue:
		for {
			v, ok := col.next()
			if !ok {
				break
			}
			child := newEnvironment(i.env)
			if err := child.define(s.Variable, v, false); err != nil {
				i.err(s.Pos, err.Error())
				return
			}
			prev := i.env
			i.env = child
			i.evalBlock(s.Body)
			i.env = prev
			if len(i.errs) > 0 {
				return
			}
		}
	default:
		i.err(s.Pos, fmt.Sprintf("for-in requires a list or stream, got %T", iter))
	}
}

func (i *Interpreter) evalReturn(s *ast.ReturnStmt) {
	var val Value = nullValue{}
	if s.Value != nil {
		val = i.evalExpr(s.Value)
		if len(i.errs) > 0 {
			return
		}
	}
	panic(returnSignal{value: val})
}

func (i *Interpreter) evalFnDecl(s *ast.FnStmt) {
	fn := functionValue{
		name:       s.Name,
		params:     s.Params,
		returnType: s.ReturnType,
		body:       &ast.FnBody{Block: s.Body},
		closure:    i.env,
	}
	if err := i.env.define(s.Name, fn, false); err != nil {
		// Function already declared — update it (for REPL use)
		_ = i.env.set(s.Name, fn)
	}
	// Allow recursion by defining the function in its own closure
	// (done by giving the fn access to the current env which already has the name)
}

func (i *Interpreter) evalStructDecl(s *ast.StructDecl) {
	typeRegistry[s.Name] = s
}

func (i *Interpreter) evalEnumDecl(s *ast.EnumDecl) {
	// For the interpreter, we need to add the variants to the global environment
	// so they can be constructed.
	for _, v := range s.Variants {
		if v.Type != nil {
			// Payload variant (function)
			vname := v.Name
			ctor := nativeFnValue{
				name: vname,
				fn: func(args []Value) Value {
					return enumValue{Variant: vname, Payload: args[0]}
				},
			}
			i.env.define(vname, ctor, false)
		} else {
			// No payload variant (constant)
			i.env.define(v.Name, enumValue{Variant: v.Name}, false)
		}
	}
}

func (i *Interpreter) evalBlock(b *ast.BlockStmt) {
	if b == nil {
		return
	}
	child := newEnvironment(i.env)
	prev := i.env
	i.env = child
	for _, stmt := range b.Body {
		i.evalStmt(stmt)
		if len(i.errs) > 0 {
			i.env = prev
			return
		}
	}
	i.env = prev
}

func (i *Interpreter) evalSpawn(s *ast.SpawnStmt) {
	callee := i.evalExpr(s.Call.Callee)
	args := make([]Value, len(s.Call.Args))
	for j, arg := range s.Call.Args {
		args[j] = i.evalExpr(arg)
	}
	if len(i.errs) > 0 {
		return
	}
	
	go func(c Value, a []Value) {
		defer func() {
			if r := recover(); r != nil {
				fmt.Println("SPAWN PANIC:", r)
			}
		}()
		childInterp := &Interpreter{env: i.env, debug: i.debug}
		if fn, ok := c.(functionValue); ok {
			childInterp.callFunctionValue(fn, a)
		} else if nativeFn, ok := c.(nativeFnValue); ok {
			nativeFn.fn(a)
		}
		if len(childInterp.errs) > 0 {
			fmt.Println("SPAWN ERRORS:")
			for _, err := range childInterp.errs {
				fmt.Println(err)
			}
		}
	}(callee, args)
}

func (i *Interpreter) evalChanSend(s *ast.ChanSendStmt) {
	ch := i.evalExpr(s.Chan)
	val := i.evalExpr(s.Value)
	if len(i.errs) > 0 {
		return
	}
	cv, ok := ch.(chanValue)
	if !ok {
		i.err(s.Pos, "cannot send to non-channel")
		return
	}
	cv.ch <- val
}

// ============================================================================
// Evaluation — expressions
// ============================================================================

func (i *Interpreter) evalExpr(e ast.Expr) Value {
	if e == nil || len(i.errs) > 0 {
		return nullValue{}
	}
	switch e := e.(type) {
	case *ast.IntLit:
		return intValue{v: e.Value}
	case *ast.FloatLit:
		return floatValue{v: e.Value}
	case *ast.StringLit:
		return stringValue{v: e.Value}
	case *ast.BoolLit:
		return boolValue{v: e.Value}
	case *ast.Ident:
		return i.evalIdent(e)
	case *ast.UnaryOp:
		return i.evalUnary(e)
	case *ast.BinaryOp:
		return i.evalBinary(e)
	case *ast.CallExpr:
		return i.evalCall(e)
	case *ast.ListLit:
		return i.evalListLit(e)
	case *ast.FnLit:
		return i.evalFnLit(e)
	case *ast.StructLit:
		return i.evalStructLit(e)
	case *ast.FieldAccess:
		return i.evalFieldAccess(e)
	case *ast.IndexExpr:
		return i.evalIndexExpr(e)
	case *ast.ChanRecvExpr:
		return i.evalChanRecv(e)
	case *ast.MatchExpr:
		return i.evalMatchExpr(e)
	case *ast.ComptimeExpr:
		return i.evalExpr(e.Expr)
	case *ast.ErrorLit:
		msgVal := i.evalExpr(e.Msg)
		var strMsg string
		if sv, ok := msgVal.(stringValue); ok {
			strMsg = sv.v
		} else {
			strMsg = msgVal.String()
		}
		// In Whale, error(...) produces an Err variant of a Result enum
		return enumValue{Variant: "Err", Payload: stringValue{v: strMsg}}
	case *ast.TryExpr:
		val := i.evalExpr(e.Expr)
		if len(i.errs) > 0 {
			return nullValue{}
		}
		
		if enumVal, ok := val.(enumValue); ok {
			if enumVal.Variant == "Err" {
				panic(returnSignal{value: enumVal})
			} else if enumVal.Variant == "Ok" {
				if enumVal.Payload != nil {
					return enumVal.Payload
				}
				return nullValue{}
			}
		}
		
		// Fallback for legacy errorValue
		if errVal, ok := val.(errorValue); ok {
			errVal.trace = append(errVal.trace, fmt.Sprintf("line %d, col %d", e.Pos.Line, e.Pos.Col))
			panic(returnSignal{value: errVal})
		}
		return val
	default:
		i.errStr(fmt.Sprintf("unknown expression type: %T", e))
		return nullValue{}
	}
}

func (i *Interpreter) evalIdent(e *ast.Ident) Value {
	v, ok := i.env.get(e.Name)
	if !ok {
		i.err(e.Pos, "undefined name: "+e.Name)
		return nullValue{}
	}
	return v
}

func (i *Interpreter) evalChanRecv(e *ast.ChanRecvExpr) Value {
	ch := i.evalExpr(e.Chan)
	if len(i.errs) > 0 {
		return nullValue{}
	}
	cv, ok := ch.(chanValue)
	if !ok {
		i.err(e.Pos, "cannot receive from non-channel")
		return nullValue{}
	}
	val, ok := <-cv.ch
	if !ok {
		return nullValue{}
	}
	return val
}

func (i *Interpreter) evalUnary(e *ast.UnaryOp) Value {
	val := i.evalExpr(e.Expr)
	if len(i.errs) > 0 {
		return nullValue{}
	}
	switch e.Op {
	case "-":
		switch v := val.(type) {
		case intValue:
			return intValue{v: -v.v}
		case floatValue:
			return floatValue{v: -v.v}
		}
		i.err(e.Pos, fmt.Sprintf("unary - requires int or float, got %T", val))
	case "!":
		bv, ok := val.(boolValue)
		if !ok {
			i.err(e.Pos, fmt.Sprintf("unary ! requires bool, got %T", val))
			return nullValue{}
		}
		return boolValue{v: !bv.v}
	}
	return nullValue{}
}

func (i *Interpreter) evalBinary(e *ast.BinaryOp) Value {
	left := i.evalExpr(e.Left)
	if len(i.errs) > 0 {
		return nullValue{}
	}

	// Short-circuit for logical operators
	if e.Op == "&&" {
		lbv, ok := left.(boolValue)
		if !ok {
			i.err(e.Pos, "left side of && must be bool")
			return nullValue{}
		}
		if !lbv.v {
			return boolValue{v: false}
		}
		right := i.evalExpr(e.Right)
		if len(i.errs) > 0 {
			return nullValue{}
		}
		rbv, ok := right.(boolValue)
		if !ok {
			i.err(e.Pos, "right side of && must be bool")
			return nullValue{}
		}
		return boolValue{v: rbv.v}
	}
	if e.Op == "||" {
		lbv, ok := left.(boolValue)
		if !ok {
			i.err(e.Pos, "left side of || must be bool")
			return nullValue{}
		}
		if lbv.v {
			return boolValue{v: true}
		}
		right := i.evalExpr(e.Right)
		if len(i.errs) > 0 {
			return nullValue{}
		}
		rbv, ok := right.(boolValue)
		if !ok {
			i.err(e.Pos, "right side of || must be bool")
			return nullValue{}
		}
		return boolValue{v: rbv.v}
	}

	right := i.evalExpr(e.Right)
	if len(i.errs) > 0 {
		return nullValue{}
	}
	return applyBinary(e.Op, left, right, e.Pos, i)
}

func applyBinary(op string, l, r Value, pos ast.Position, i *Interpreter) Value {
	// List operations
	if la, ok := l.(listValue); ok {
		if ra, ok := r.(listValue); ok {
			return applyListOp(op, la, ra, pos, i)
		}
	}

	// String concatenation and comparison
	if ls, ok := l.(stringValue); ok {
		if rs, ok := r.(stringValue); ok {
			switch op {
			case "+":
				return stringValue{v: ls.v + rs.v}
			case "==":
				return boolValue{v: ls.v == rs.v}
			case "!=":
				return boolValue{v: ls.v != rs.v}
			case "<":
				return boolValue{v: ls.v < rs.v}
			case "<=":
				return boolValue{v: ls.v <= rs.v}
			case ">":
				return boolValue{v: ls.v > rs.v}
			case ">=":
				return boolValue{v: ls.v >= rs.v}
			}
		}
	}

	// Bool comparison
	if lb, ok := l.(boolValue); ok {
		if rb, ok := r.(boolValue); ok {
			switch op {
			case "==":
				return boolValue{v: lb.v == rb.v}
			case "!=":
				return boolValue{v: lb.v != rb.v}
			}
		}
	}

	// Numeric operations (int and float, with coercion)
	lf, lok := toFloat(l)
	rf, rok := toFloat(r)
	if !lok || !rok {
		i.err(pos, fmt.Sprintf("cannot apply '%s' to %s and %s", op, typeName(l), typeName(r)))
		return nullValue{}
	}

	// Prefer int for int+int operations
	li, liOk := l.(intValue)
	ri, riOk := r.(intValue)

	switch op {
	case "+":
		if liOk && riOk {
			return intValue{v: li.v + ri.v}
		}
		return floatValue{v: lf + rf}
	case "-":
		if liOk && riOk {
			return intValue{v: li.v - ri.v}
		}
		return floatValue{v: lf - rf}
	case "*":
		if liOk && riOk {
			return intValue{v: li.v * ri.v}
		}
		return floatValue{v: lf * rf}
	case "/":
		if rf == 0 {
			i.err(pos, "division by zero")
			return nullValue{}
		}
		if liOk && riOk {
			return intValue{v: li.v / ri.v}
		}
		return floatValue{v: lf / rf}
	case "%":
		if liOk && riOk {
			if ri.v == 0 {
				i.err(pos, "modulo by zero")
				return nullValue{}
			}
			return intValue{v: li.v % ri.v}
		}
		return floatValue{v: math.Mod(lf, rf)}
	case "==":
		return boolValue{v: lf == rf}
	case "!=":
		return boolValue{v: lf != rf}
	case "<":
		return boolValue{v: lf < rf}
	case "<=":
		return boolValue{v: lf <= rf}
	case ">":
		return boolValue{v: lf > rf}
	case ">=":
		return boolValue{v: lf >= rf}
	}
	i.err(pos, fmt.Sprintf("unknown operator: %s", op))
	return nullValue{}
}

func applyListOp(op string, a, b listValue, pos ast.Position, i *Interpreter) Value {
	switch op {
	case "+":
		out := make([]Value, 0, len(a.elements)+len(b.elements))
		out = append(out, a.elements...)
		out = append(out, b.elements...)
		return listValue{elements: out}
	case "==":
		if len(a.elements) != len(b.elements) {
			return boolValue{v: false}
		}
		for idx := range a.elements {
			if a.elements[idx].String() != b.elements[idx].String() {
				return boolValue{v: false}
			}
		}
		return boolValue{v: true}
	case "!=":
		eq := applyListOp("==", a, b, pos, i)
		if bv, ok := eq.(boolValue); ok {
			return boolValue{v: !bv.v}
		}
		return nullValue{}
	}
	i.err(pos, fmt.Sprintf("operator %q not supported for lists", op))
	return nullValue{}
}

func toFloat(v Value) (float64, bool) {
	switch v := v.(type) {
	case intValue:
		return float64(v.v), true
	case floatValue:
		return v.v, true
	}
	return 0, false
}

func typeName(v Value) string {
	switch v.(type) {
	case intValue:
		return "int"
	case floatValue:
		return "float"
	case stringValue:
		return "string"
	case boolValue:
		return "bool"
	case listValue:
		return "list"
	case structValue:
		return "struct"
	case functionValue:
		return "fn"
	case streamValue:
		return "stream"
	case nullValue:
		return "()"
	}
	return fmt.Sprintf("%T", v)
}

func (i *Interpreter) evalListLit(e *ast.ListLit) Value {
	elems := make([]Value, 0, len(e.Values))
	for _, v := range e.Values {
		el := i.evalExpr(v)
		if len(i.errs) > 0 {
			return nullValue{}
		}
		elems = append(elems, el)
	}
	return listValue{elements: elems}
}

func (i *Interpreter) evalFnLit(e *ast.FnLit) Value {
	return functionValue{
		params:     e.Params,
		returnType: e.ReturnType,
		body:       e.Body,
		closure:    i.env,
	}
}

func (i *Interpreter) evalStructLit(e *ast.StructLit) Value {
	decl, ok := typeRegistry[e.Type]
	if !ok {
		i.err(e.Pos, "unknown struct type: "+e.Type)
		return nullValue{}
	}
	fields := make(map[string]Value, len(decl.Fields))
	// Check all required fields are present
	for _, f := range decl.Fields {
		fval, present := e.Fields[f.Name]
		if !present {
			i.err(e.Pos, fmt.Sprintf("missing field %q in %s literal", f.Name, e.Type))
			return nullValue{}
		}
		ev := i.evalExpr(fval)
		if len(i.errs) > 0 {
			return nullValue{}
		}
		fields[f.Name] = ev
	}
	return structValue{typeName: e.Type, fields: fields}
}

func (i *Interpreter) evalMatchExpr(e *ast.MatchExpr) Value {
	val := i.evalExpr(e.Expr)
	if len(i.errs) > 0 {
		return nullValue{}
	}

	enumVal, ok := val.(enumValue)
	if !ok {
		// Type checker ensures this is an enum, so if it's not, it's a critical runtime error
		i.err(e.Pos, "match expression evaluated to non-enum value")
		return nullValue{}
	}

	for _, arm := range e.Arms {
		if arm.IsCatchAll {
			// Catch-all `_ =>`
			return i.evalMatchArm(arm, nil)
		}
		
		if arm.Variant == enumVal.Variant {
			// Found the matching arm
			return i.evalMatchArm(arm, enumVal.Payload)
		}
	}

	i.err(e.Pos, "match is not exhaustive, variant "+enumVal.Variant+" not handled")
	return nullValue{}
}

func (i *Interpreter) evalMatchArm(arm *ast.MatchArm, payload Value) Value {
	// Execute arm in a new scope so bindings don't leak
	prevEnv := i.env
	i.env = newEnvironment(i.env)
	defer func() { i.env = prevEnv }()

	if arm.Binding != "" && payload != nil {
		i.env.define(arm.Binding, payload, false)
	}

	return i.evalExpr(arm.Body)
}

func (i *Interpreter) evalFieldAccess(e *ast.FieldAccess) Value {
	obj := i.evalExpr(e.Expr)
	if len(i.errs) > 0 {
		return nullValue{}
	}
	sv, ok := obj.(structValue)
	if !ok {
		i.err(e.Pos, fmt.Sprintf("cannot access field %q on non-struct value (%s)", e.Field, typeName(obj)))
		return nullValue{}
	}
	fv, ok := sv.fields[e.Field]
	if !ok {
		methodName := sv.typeName + "_" + e.Field
		if mf, exists := i.env.get(methodName); exists {
			return mf
		}
		i.err(e.Pos, fmt.Sprintf("struct %s has no field %q", sv.typeName, e.Field))
		return nullValue{}
	}
	return fv
}

func (i *Interpreter) evalIndexExpr(e *ast.IndexExpr) Value {
	obj := i.evalExpr(e.Expr)
	if len(i.errs) > 0 {
		return nullValue{}
	}
	idx := i.evalExpr(e.Index)
	if len(i.errs) > 0 {
		return nullValue{}
	}
	switch o := obj.(type) {
	case listValue:
		idxInt, ok := idx.(intValue)
		if !ok {
			i.err(e.Pos, fmt.Sprintf("list index must be int, got %s", typeName(idx)))
			return nullValue{}
		}
		n := idxInt.v
		if n < 0 {
			n = int64(len(o.elements)) + n
		}
		if n < 0 || n >= int64(len(o.elements)) {
			i.err(e.Pos, fmt.Sprintf("list index %d out of range (len %d)", idxInt.v, len(o.elements)))
			return nullValue{}
		}
		return o.elements[n]
	case stringValue:
		idxInt, ok := idx.(intValue)
		if !ok {
			i.err(e.Pos, fmt.Sprintf("string index must be int, got %s", typeName(idx)))
			return nullValue{}
		}
		n := idxInt.v
		if n < 0 || n >= int64(len(o.v)) {
			i.err(e.Pos, fmt.Sprintf("string index %d out of range", n))
			return nullValue{}
		}
		return intValue{v: int64(o.v[n])}
	}
	i.err(e.Pos, fmt.Sprintf("cannot index into %s", typeName(obj)))
	return nullValue{}
}

// ============================================================================
// Function calls
// ============================================================================

// evalCall is the main dispatcher for all function calls.
func (i *Interpreter) evalCall(e *ast.CallExpr) Value {
	// Fast path: builtin name dispatch on bare identifiers
	if id, ok := e.Callee.(*ast.Ident); ok {
		switch id.Name {
		// Standard builtins
		case "print":
			return i.callPrint(e)
		case "len":
			return i.callLen(e)
		case "to_string":
			return i.callToString(e)
		case "contains":
			return i.callContains(e)
		case "push":
			return i.callPush(e)
		case "pop":
			return i.callPop(e)
		case "type_of":
			return i.callTypeOf(e)
		case "json_parse":
			return i.callJsonParse(e)
		case "csv_parse":
			return i.callCsvParse(e)
		case "split":
			return i.callSplit(e)
		case "trim":
			return i.callTrim(e)
		case "replace":
			return i.callReplace(e)
		case "to_lower":
			return i.callToLower(e)
		case "to_upper":
			return i.callToUpper(e)
		case "abs":
			return i.callAbs(e)
		case "max":
			return i.callMax(e)
		case "min":
			return i.callMin(e)
		case "parse_int":
			return i.callParseInt(e)
		// File I/O
		case "read_file":
			return i.callReadFile(e)
		case "write_file":
			return i.callWriteFile(e)
		case "lines":
			return i.callLines(e)
		// Concurrency
		case "make_chan":
			return i.callMakeChan(e)
		// Networking
		case "net_listen":
			return i.callNetListen(e)
		case "net_accept":
			return i.callNetAccept(e)
		case "net_recv":
			return i.callNetRecv(e)
		case "net_send":
			return i.callNetSend(e)
		case "net_close":
			return i.callNetClose(e)
		case "net_dial":
			return i.callNetDial(e)
		// Testing
		case "assert_eq":
			return i.callAssertEq(e)
		case "assert_true":
			return i.callAssertTrue(e)
		// Map
		case "map_new":
			return i.callMapNew(e)
		case "map_set":
			return i.callMapSet(e)
		case "map_get":
			return i.callMapGet(e)
		// Stream operations
		case "stream":
			return i.callStream(e)
		case "filter":
			return i.callFilter(e)
		case "map":
			return i.callMap(e)
		case "collect":
			return i.callCollect(e)
		case "for_each":
			return i.callForEach(e)
		case "fold":
			return i.callFold(e)
		case "take":
			return i.callTake(e)
		case "skip":
			return i.callSkip(e)
		}
	}

	// Fast path for method calls
	if fa, ok := e.Callee.(*ast.FieldAccess); ok {
		obj := i.evalExpr(fa.Expr)
		if sv, ok := obj.(structValue); ok {
			if _, exists := sv.fields[fa.Field]; !exists {
				methodName := sv.typeName + "_" + fa.Field
				if mf, exists := i.env.get(methodName); exists {
					if fn, ok := mf.(functionValue); ok {
						args := make([]Value, len(e.Args)+1)
						args[0] = sv
						for idx, a := range e.Args {
							args[idx+1] = i.evalExpr(a)
						}
						return i.callFunctionValue(fn, args)
					}
				}
			}
		}
	}

	// General case: evaluate the callee expression
	callee := i.evalExpr(e.Callee)
	if len(i.errs) > 0 {
		return nullValue{}
	}
	if fn, ok := callee.(functionValue); ok {
		// Evaluate arguments, then call
		args := make([]Value, 0, len(e.Args))
		for _, a := range e.Args {
			v := i.evalExpr(a)
			if len(i.errs) > 0 {
				return nullValue{}
			}
			args = append(args, v)
		}
		return i.callFunctionValue(fn, args)
	}

	if nativeFn, ok := callee.(nativeFnValue); ok {
		// Evaluate arguments, then call
		args := make([]Value, 0, len(e.Args))
		for _, a := range e.Args {
			v := i.evalExpr(a)
			if len(i.errs) > 0 {
				return nullValue{}
			}
			args = append(args, v)
		}
		return nativeFn.fn(args)
	}

	i.err(e.Pos, fmt.Sprintf("cannot call non-function value: %v", callee))
	return nullValue{}
}

// callFunctionValue invokes a user-defined function with pre-evaluated args.
// Used both by evalCall and by stream operations (filter, map, etc.).
func (i *Interpreter) callFunctionValue(fn functionValue, args []Value) Value {
	if len(args) != len(fn.params) {
		i.errStr(fmt.Sprintf("%s: expected %d arguments, got %d",
			fn.name, len(fn.params), len(args)))
		return nullValue{}
	}

	// Set up call environment: child of the closure
	callEnv := newEnvironment(fn.closure)
	for idx, param := range fn.params {
		if err := callEnv.define(param.Name, args[idx], false); err != nil {
			i.errStr(fmt.Sprintf("%s: %v", fn.name, err))
			return nullValue{}
		}
	}

	// Save and swap environment
	prev := i.env
	i.env = callEnv

	var result Value = nullValue{}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if sig, ok := r.(returnSignal); ok {
					result = sig.value
				} else {
					panic(r) // re-raise real panics
				}
			}
		}()
		if fn.body.Block != nil {
			i.evalBlock(fn.body.Block)
		} else if fn.body.Expr != nil {
			result = i.evalExpr(fn.body.Expr)
		}
	}()

	i.env = prev
	return result
}

// ============================================================================
// Standard library builtins
// ============================================================================

func (i *Interpreter) callPrint(e *ast.CallExpr) Value {
	parts := make([]string, 0, len(e.Args))
	for _, a := range e.Args {
		v := i.evalExpr(a)
		if len(i.errs) > 0 {
			return nullValue{}
		}
		parts = append(parts, v.String())
	}
	line := strings.Join(parts, " ") + "\n"
	i.output.WriteString(line)
	fmt.Print(line)
	return nullValue{}
}

func (i *Interpreter) callLen(e *ast.CallExpr) Value {
	if len(e.Args) != 1 {
		i.err(e.Pos, fmt.Sprintf("len() takes 1 argument, got %d", len(e.Args)))
		return nullValue{}
	}
	v := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	switch val := v.(type) {
	case stringValue:
		return intValue{v: int64(len(val.v))}
	case listValue:
		return intValue{v: int64(len(val.elements))}
	case streamValue:
		// Eagerly count — drains the stream
		count := int64(0)
		for {
			_, ok := val.next()
			if !ok {
				break
			}
			count++
		}
		return intValue{v: count}
	}
	i.err(e.Pos, fmt.Sprintf("len() requires a string, list, or stream, got %s", typeName(v)))
	return nullValue{}
}

func (i *Interpreter) callToString(e *ast.CallExpr) Value {
	if len(e.Args) != 1 {
		i.err(e.Pos, fmt.Sprintf("to_string() takes 1 argument, got %d", len(e.Args)))
		return nullValue{}
	}
	v := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	return stringValue{v: v.String()}
}

func (i *Interpreter) callContains(e *ast.CallExpr) Value {
	if len(e.Args) != 2 {
		i.err(e.Pos, fmt.Sprintf("contains() takes 2 arguments, got %d", len(e.Args)))
		return nullValue{}
	}
	haystack := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	needle := i.evalExpr(e.Args[1])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	switch h := haystack.(type) {
	case stringValue:
		ns, ok := needle.(stringValue)
		if !ok {
			i.err(e.Pos, "contains() on string requires a string needle")
			return nullValue{}
		}
		return boolValue{v: strings.Contains(h.v, ns.v)}
	case listValue:
		needleStr := needle.String()
		for _, el := range h.elements {
			if el.String() == needleStr {
				return boolValue{v: true}
			}
		}
		return boolValue{v: false}
	}
	i.err(e.Pos, fmt.Sprintf("contains() requires a string or list, got %s", typeName(haystack)))
	return nullValue{}
}

func (i *Interpreter) callPush(e *ast.CallExpr) Value {
	if len(e.Args) != 2 {
		i.err(e.Pos, fmt.Sprintf("push() takes 2 arguments, got %d", len(e.Args)))
		return nullValue{}
	}
	lst := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	elem := i.evalExpr(e.Args[1])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	lv, ok := lst.(listValue)
	if !ok {
		i.err(e.Pos, "push() requires a list as first argument")
		return nullValue{}
	}
	newElems := make([]Value, len(lv.elements)+1)
	copy(newElems, lv.elements)
	newElems[len(lv.elements)] = elem
	return listValue{elements: newElems}
}

func (i *Interpreter) callPop(e *ast.CallExpr) Value {
	if len(e.Args) != 1 {
		i.err(e.Pos, fmt.Sprintf("pop() takes 1 argument, got %d", len(e.Args)))
		return nullValue{}
	}
	lst := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	lv, ok := lst.(listValue)
	if !ok {
		i.err(e.Pos, "pop() requires a list")
		return nullValue{}
	}
	if len(lv.elements) == 0 {
		i.err(e.Pos, "pop() on empty list")
		return nullValue{}
	}
	newElems := make([]Value, len(lv.elements)-1)
	copy(newElems, lv.elements)
	return listValue{elements: newElems}
}

func (i *Interpreter) callTypeOf(e *ast.CallExpr) Value {
	if len(e.Args) != 1 {
		i.err(e.Pos, fmt.Sprintf("type_of() takes 1 argument, got %d", len(e.Args)))
		return nullValue{}
	}
	v := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	return stringValue{v: typeName(v)}
}

// ============================================================================
// File I/O builtins
// ============================================================================

func (i *Interpreter) callReadFile(e *ast.CallExpr) Value {
	if len(e.Args) != 1 {
		return enumValue{Variant: "Err", Payload: stringValue{v: "read_file takes exactly 1 argument"}}
	}
	path := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	ps, ok := path.(stringValue)
	if !ok {
		return enumValue{Variant: "Err", Payload: stringValue{v: "read_file requires a string path"}}
	}
	data, err := os.ReadFile(ps.v)
	if err != nil {
		return enumValue{Variant: "Err", Payload: stringValue{v: err.Error()}}
	}
	return enumValue{Variant: "Ok", Payload: stringValue{v: string(data)}}
}

func (i *Interpreter) callMakeChan(e *ast.CallExpr) Value {
	if len(e.Args) != 0 {
		i.err(e.Pos, fmt.Sprintf("make_chan() takes 0 arguments, got %d", len(e.Args)))
		return nullValue{}
	}
	return chanValue{ch: make(chan Value)}
}

func (i *Interpreter) callWriteFile(e *ast.CallExpr) Value {
	if len(e.Args) != 2 {
		return enumValue{Variant: "Err", Payload: stringValue{v: "write_file takes exactly 2 arguments"}}
	}
	path := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	ps, ok := path.(stringValue)
	if !ok {
		return enumValue{Variant: "Err", Payload: stringValue{v: "write_file requires a string path"}}
	}
	content := i.evalExpr(e.Args[1])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	cs, ok := content.(stringValue)
	if !ok {
		return enumValue{Variant: "Err", Payload: stringValue{v: "write_file requires string content"}}
	}
	if err := os.WriteFile(ps.v, []byte(cs.v), 0644); err != nil {
		return enumValue{Variant: "Err", Payload: stringValue{v: err.Error()}}
	}
	return enumValue{Variant: "Ok", Payload: intValue{v: int64(len(cs.v))}}
}

func (i *Interpreter) callLines(e *ast.CallExpr) Value {
	if len(e.Args) != 1 {
		i.err(e.Pos, fmt.Sprintf("lines() takes 1 argument, got %d", len(e.Args)))
		return nullValue{}
	}
	v := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	s, ok := v.(stringValue)
	if !ok {
		i.err(e.Pos, "lines() requires a string")
		return nullValue{}
	}
	parts := strings.Split(s.v, "\n")
	// Drop trailing empty string from final newline
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	elems := make([]Value, len(parts))
	for idx, p := range parts {
		elems[idx] = stringValue{v: p}
	}
	return listValue{elements: elems}
}

// ============================================================================
// Stream builtins
// ============================================================================

func collectStream(s streamValue) []Value {
	out := make([]Value, 0, 8)
	for {
		v, ok := s.next()
		if !ok {
			break
		}
		out = append(out, v)
	}
	return out
}

func streamFromList(l listValue) streamValue {
	idx := 0
	return streamValue{
		next: func() (Value, bool) {
			if idx >= len(l.elements) {
				return nullValue{}, false
			}
			v := l.elements[idx]
			idx++
			return v, true
		},
	}
}

func (i *Interpreter) streamFilter(s streamValue, pred functionValue) streamValue {
	interp := i
	return streamValue{
		next: func() (Value, bool) {
			for {
				v, ok := s.next()
				if !ok {
					return nullValue{}, false
				}
				result := interp.callFunctionValue(pred, []Value{v})
				if len(interp.errs) > 0 {
					return nullValue{}, false
				}
				bv, ok := result.(boolValue)
				if ok && bv.v {
					return v, true
				}
			}
		},
	}
}

func (i *Interpreter) streamMap(s streamValue, f functionValue) streamValue {
	interp := i
	return streamValue{
		next: func() (Value, bool) {
			v, ok := s.next()
			if !ok {
				return nullValue{}, false
			}
			result := interp.callFunctionValue(f, []Value{v})
			if len(interp.errs) > 0 {
				return nullValue{}, false
			}
			return result, true
		},
	}
}

func streamTake(s streamValue, n int64) streamValue {
	remaining := n
	return streamValue{
		next: func() (Value, bool) {
			if remaining <= 0 {
				return nullValue{}, false
			}
			remaining--
			return s.next()
		},
	}
}

func streamSkip(s streamValue, n int64) streamValue {
	skipped := int64(0)
	for skipped < n {
		_, ok := s.next()
		if !ok {
			return streamValue{next: func() (Value, bool) { return nullValue{}, false }}
		}
		skipped++
	}
	return s
}

func (i *Interpreter) callStream(e *ast.CallExpr) Value {
	if len(e.Args) != 1 {
		i.err(e.Pos, fmt.Sprintf("stream() takes 1 argument, got %d", len(e.Args)))
		return nullValue{}
	}
	v := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	switch src := v.(type) {
	case listValue:
		return streamFromList(src)
	case streamValue:
		return src // already a stream
	}
	i.err(e.Pos, fmt.Sprintf("stream() requires a list, got %s", typeName(v)))
	return nullValue{}
}

func (i *Interpreter) callFilter(e *ast.CallExpr) Value {
	if len(e.Args) != 2 {
		i.err(e.Pos, fmt.Sprintf("filter() takes 2 arguments, got %d", len(e.Args)))
		return nullValue{}
	}
	src := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	s, ok := src.(streamValue)
	if !ok {
		i.err(e.Pos, "filter() requires a stream as first argument")
		return nullValue{}
	}
	pred := i.evalExpr(e.Args[1])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	fn, ok := pred.(functionValue)
	if !ok {
		i.err(e.Pos, "filter() requires a function as second argument")
		return nullValue{}
	}
	return i.streamFilter(s, fn)
}

func (i *Interpreter) callMap(e *ast.CallExpr) Value {
	if len(e.Args) != 2 {
		i.err(e.Pos, fmt.Sprintf("map() takes 2 arguments, got %d", len(e.Args)))
		return nullValue{}
	}
	src := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	s, ok := src.(streamValue)
	if !ok {
		i.err(e.Pos, "map() requires a stream as first argument")
		return nullValue{}
	}
	f := i.evalExpr(e.Args[1])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	fn, ok := f.(functionValue)
	if !ok {
		i.err(e.Pos, "map() requires a function as second argument")
		return nullValue{}
	}
	return i.streamMap(s, fn)
}

func (i *Interpreter) callCollect(e *ast.CallExpr) Value {
	if len(e.Args) != 1 {
		i.err(e.Pos, fmt.Sprintf("collect() takes 1 argument, got %d", len(e.Args)))
		return nullValue{}
	}
	src := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	s, ok := src.(streamValue)
	if !ok {
		// If given a list, just return it
		if _, isList := src.(listValue); isList {
			return src
		}
		i.err(e.Pos, "collect() requires a stream")
		return nullValue{}
	}
	return listValue{elements: collectStream(s)}
}

func (i *Interpreter) callForEach(e *ast.CallExpr) Value {
	if len(e.Args) != 2 {
		i.err(e.Pos, fmt.Sprintf("for_each() takes 2 arguments, got %d", len(e.Args)))
		return nullValue{}
	}
	src := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	s, ok := src.(streamValue)
	if !ok {
		i.err(e.Pos, "for_each() requires a stream as first argument")
		return nullValue{}
	}
	body := i.evalExpr(e.Args[1])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	fn, ok := body.(functionValue)
	if !ok {
		i.err(e.Pos, "for_each() requires a function as second argument")
		return nullValue{}
	}
	for {
		v, ok := s.next()
		if !ok {
			break
		}
		i.callFunctionValue(fn, []Value{v})
		if len(i.errs) > 0 {
			break
		}
	}
	return nullValue{}
}

func (i *Interpreter) callFold(e *ast.CallExpr) Value {
	if len(e.Args) != 3 {
		i.err(e.Pos, fmt.Sprintf("fold() takes 3 arguments (stream, initial, fn), got %d", len(e.Args)))
		return nullValue{}
	}
	src := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	s, ok := src.(streamValue)
	if !ok {
		i.err(e.Pos, "fold() requires a stream as first argument")
		return nullValue{}
	}
	acc := i.evalExpr(e.Args[1])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	f := i.evalExpr(e.Args[2])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	fn, ok := f.(functionValue)
	if !ok {
		i.err(e.Pos, "fold() requires a function as third argument")
		return nullValue{}
	}
	for {
		v, ok := s.next()
		if !ok {
			break
		}
		result := i.callFunctionValue(fn, []Value{acc, v})
		if len(i.errs) > 0 {
			break
		}
		acc = result
	}
	return acc
}

func (i *Interpreter) callTake(e *ast.CallExpr) Value {
	if len(e.Args) != 2 {
		i.err(e.Pos, fmt.Sprintf("take() takes 2 arguments, got %d", len(e.Args)))
		return nullValue{}
	}
	src := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	s, ok := src.(streamValue)
	if !ok {
		i.err(e.Pos, "take() requires a stream as first argument")
		return nullValue{}
	}
	n := i.evalExpr(e.Args[1])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	nv, ok := n.(intValue)
	if !ok {
		i.err(e.Pos, "take() requires an integer as second argument")
		return nullValue{}
	}
	return streamTake(s, nv.v)
}

func (i *Interpreter) callSkip(e *ast.CallExpr) Value {
	if len(e.Args) != 2 {
		i.err(e.Pos, fmt.Sprintf("skip() takes 2 arguments, got %d", len(e.Args)))
		return nullValue{}
	}
	src := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	s, ok := src.(streamValue)
	if !ok {
		i.err(e.Pos, "skip() requires a stream as first argument")
		return nullValue{}
	}
	n := i.evalExpr(e.Args[1])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	nv, ok := n.(intValue)
	if !ok {
		i.err(e.Pos, "skip() requires an integer as second argument")
		return nullValue{}
	}
	return streamSkip(s, nv.v)
}

func (i *Interpreter) callSplit(e *ast.CallExpr) Value {
	if len(e.Args) != 2 {
		i.err(e.Pos, "split() takes 2 arguments")
		return nullValue{}
	}
	str := i.evalExpr(e.Args[0])
	sep := i.evalExpr(e.Args[1])
	if len(i.errs) > 0 { return nullValue{} }
	
	s, ok1 := str.(stringValue)
	se, ok2 := sep.(stringValue)
	if !ok1 || !ok2 {
		i.err(e.Pos, "split() requires two strings")
		return nullValue{}
	}
	
	parts := strings.Split(s.v, se.v)
	elems := make([]Value, len(parts))
	for idx, p := range parts {
		elems[idx] = stringValue{v: p}
	}
	return listValue{elements: elems}
}

func (i *Interpreter) callTrim(e *ast.CallExpr) Value {
	if len(e.Args) != 1 {
		i.err(e.Pos, "trim() takes 1 argument")
		return nullValue{}
	}
	str := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 { return nullValue{} }
	
	s, ok := str.(stringValue)
	if !ok {
		i.err(e.Pos, "trim() requires a string")
		return nullValue{}
	}
	return stringValue{v: strings.TrimSpace(s.v)}
}

func (i *Interpreter) callReplace(e *ast.CallExpr) Value {
	if len(e.Args) != 3 {
		i.err(e.Pos, "replace() takes 3 arguments")
		return nullValue{}
	}
	str := i.evalExpr(e.Args[0])
	oldStr := i.evalExpr(e.Args[1])
	newStr := i.evalExpr(e.Args[2])
	if len(i.errs) > 0 { return nullValue{} }
	
	s, ok1 := str.(stringValue)
	o, ok2 := oldStr.(stringValue)
	n, ok3 := newStr.(stringValue)
	if !ok1 || !ok2 || !ok3 {
		i.err(e.Pos, "replace() requires three strings")
		return nullValue{}
	}
	return stringValue{v: strings.ReplaceAll(s.v, o.v, n.v)}
}

func (i *Interpreter) callToLower(e *ast.CallExpr) Value {
	if len(e.Args) != 1 {
		i.err(e.Pos, "to_lower() takes 1 argument")
		return nullValue{}
	}
	str := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 { return nullValue{} }
	
	s, ok := str.(stringValue)
	if !ok {
		i.err(e.Pos, "to_lower() requires a string")
		return nullValue{}
	}
	return stringValue{v: strings.ToLower(s.v)}
}

func (i *Interpreter) callToUpper(e *ast.CallExpr) Value {
	if len(e.Args) != 1 {
		i.err(e.Pos, "to_upper() takes 1 argument")
		return nullValue{}
	}
	str := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 { return nullValue{} }
	
	s, ok := str.(stringValue)
	if !ok {
		i.err(e.Pos, "to_upper() requires a string")
		return nullValue{}
	}
	return stringValue{v: strings.ToUpper(s.v)}
}

func (i *Interpreter) callAbs(e *ast.CallExpr) Value {
	if len(e.Args) != 1 {
		i.err(e.Pos, "abs() takes 1 argument")
		return nullValue{}
	}
	val := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 { return nullValue{} }
	
	switch v := val.(type) {
	case intValue:
		if v.v < 0 { return intValue{v: -v.v} }
		return v
	case floatValue:
		return floatValue{v: math.Abs(v.v)}
	}
	i.err(e.Pos, "abs() requires int or float")
	return nullValue{}
}

func (i *Interpreter) callMax(e *ast.CallExpr) Value {
	if len(e.Args) != 2 {
		i.err(e.Pos, "max() takes 2 arguments")
		return nullValue{}
	}
	val1 := i.evalExpr(e.Args[0])
	val2 := i.evalExpr(e.Args[1])
	if len(i.errs) > 0 { return nullValue{} }
	
	switch v1 := val1.(type) {
	case intValue:
		if v2, ok := val2.(intValue); ok {
			if v1.v > v2.v { return v1 }
			return v2
		}
	case floatValue:
		if v2, ok := val2.(floatValue); ok {
			return floatValue{v: math.Max(v1.v, v2.v)}
		}
	}
	i.err(e.Pos, "max() requires two ints or two floats")
	return nullValue{}
}

func (i *Interpreter) callMin(e *ast.CallExpr) Value {
	if len(e.Args) != 2 {
		i.err(e.Pos, "min() takes 2 arguments")
		return nullValue{}
	}
	val1 := i.evalExpr(e.Args[0])
	val2 := i.evalExpr(e.Args[1])
	if len(i.errs) > 0 { return nullValue{} }
	
	switch v1 := val1.(type) {
	case intValue:
		if v2, ok := val2.(intValue); ok {
			if v1.v < v2.v { return v1 }
			return v2
		}
	case floatValue:
		if v2, ok := val2.(floatValue); ok {
			return floatValue{v: math.Min(v1.v, v2.v)}
		}
	}
	i.err(e.Pos, "min() requires two ints or two floats")
	return nullValue{}
}

func (i *Interpreter) callParseInt(e *ast.CallExpr) Value {
	if len(e.Args) != 1 {
		i.err(e.Pos, "parse_int() takes 1 argument")
		return nullValue{}
	}
	str := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 { return nullValue{} }
	
	s, ok := str.(stringValue)
	if !ok {
		i.err(e.Pos, "parse_int() requires a string")
		return nullValue{}
	}
	val, err := strconv.ParseInt(strings.TrimSpace(s.v), 10, 64)
	if err != nil {
		i.err(e.Pos, fmt.Sprintf("parse_int() error: %v", err))
		return nullValue{}
	}
	return intValue{v: val}
}

// ============================================================================
// Standard Library (Net and Map)
// ============================================================================

var nativeListeners []net.Listener
var nativeConns []net.Conn
var nativeMaps []map[string]string
var netMutex sync.Mutex

func (i *Interpreter) callNetListen(e *ast.CallExpr) Value {
	port := i.evalExpr(e.Args[0]).(intValue).v
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return enumValue{Variant: "Err", Payload: stringValue{v: err.Error()}}
	}
	netMutex.Lock()
	nativeListeners = append(nativeListeners, l)
	idx := len(nativeListeners) - 1
	netMutex.Unlock()
	return enumValue{Variant: "Ok", Payload: intValue{v: int64(idx)}}
}

func (i *Interpreter) callNetAccept(e *ast.CallExpr) Value {
	idx := i.evalExpr(e.Args[0]).(intValue).v
	netMutex.Lock()
	if idx < 0 || idx >= int64(len(nativeListeners)) { 
		netMutex.Unlock()
		return enumValue{Variant: "Err", Payload: stringValue{v: "invalid listener index"}} 
	}
	listener := nativeListeners[idx]
	netMutex.Unlock()
	
	conn, err := listener.Accept()
	if err != nil { 
		return enumValue{Variant: "Err", Payload: stringValue{v: err.Error()}} 
	}
	
	netMutex.Lock()
	nativeConns = append(nativeConns, conn)
	connIdx := len(nativeConns) - 1
	netMutex.Unlock()
	
	return enumValue{Variant: "Ok", Payload: intValue{v: int64(connIdx)}}
}

func (i *Interpreter) callNetRecv(e *ast.CallExpr) Value {
	idx := i.evalExpr(e.Args[0]).(intValue).v
	netMutex.Lock()
	if idx < 0 || idx >= int64(len(nativeConns)) { 
		netMutex.Unlock()
		return enumValue{Variant: "Err", Payload: stringValue{v: "invalid conn index"}} 
	}
	conn := nativeConns[idx]
	netMutex.Unlock()
	
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil { return enumValue{Variant: "Err", Payload: stringValue{v: err.Error()}} }
	if n == 0 { return enumValue{Variant: "Err", Payload: stringValue{v: "EOF"}} }
	return enumValue{Variant: "Ok", Payload: stringValue{v: string(buf[:n])}}
}

func (i *Interpreter) callNetSend(e *ast.CallExpr) Value {
	idx := i.evalExpr(e.Args[0]).(intValue).v
	data := i.evalExpr(e.Args[1]).(stringValue).v
	netMutex.Lock()
	if idx < 0 || idx >= int64(len(nativeConns)) { 
		netMutex.Unlock()
		return enumValue{Variant: "Err", Payload: stringValue{v: "invalid conn index"}} 
	}
	conn := nativeConns[idx]
	netMutex.Unlock()
	
	n, err := conn.Write([]byte(data))
	if err != nil { return enumValue{Variant: "Err", Payload: stringValue{v: err.Error()}} }
	return enumValue{Variant: "Ok", Payload: intValue{v: int64(n)}}
}

func (i *Interpreter) callNetClose(e *ast.CallExpr) Value {
	idx := i.evalExpr(e.Args[0]).(intValue).v
	netMutex.Lock()
	if idx < 0 || idx >= int64(len(nativeConns)) { 
		netMutex.Unlock()
		return enumValue{Variant: "Err", Payload: stringValue{v: "invalid conn index"}} 
	}
	conn := nativeConns[idx]
	netMutex.Unlock()
	
	err := conn.Close()
	if err != nil { return enumValue{Variant: "Err", Payload: stringValue{v: err.Error()}} }
	return enumValue{Variant: "Ok", Payload: intValue{v: 0}}
}

func (i *Interpreter) callNetDial(e *ast.CallExpr) Value {
	host := i.evalExpr(e.Args[0]).(stringValue).v
	port := i.evalExpr(e.Args[1]).(intValue).v
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil { 
		return enumValue{Variant: "Err", Payload: stringValue{v: err.Error()}} 
	}
	
	netMutex.Lock()
	nativeConns = append(nativeConns, conn)
	connIdx := len(nativeConns) - 1
	netMutex.Unlock()
	
	return enumValue{Variant: "Ok", Payload: intValue{v: int64(connIdx)}}
}

func (i *Interpreter) callMapNew(e *ast.CallExpr) Value {
	nativeMaps = append(nativeMaps, make(map[string]string))
	return intValue{v: int64(len(nativeMaps) - 1)}
}

func (i *Interpreter) callMapSet(e *ast.CallExpr) Value {
	idx := i.evalExpr(e.Args[0]).(intValue).v
	k := i.evalExpr(e.Args[1]).(stringValue).v
	v := i.evalExpr(e.Args[2]).(stringValue).v
	if idx >= 0 && idx < int64(len(nativeMaps)) {
		nativeMaps[idx][k] = v
	}
	return nullValue{}
}

func (i *Interpreter) callMapGet(e *ast.CallExpr) Value {
	idx := i.evalExpr(e.Args[0]).(intValue).v
	k := i.evalExpr(e.Args[1]).(stringValue).v
	if idx >= 0 && idx < int64(len(nativeMaps)) {
		return stringValue{v: nativeMaps[idx][k]}
	}
	return stringValue{v: ""}
}

// ============================================================================
// Data Parsing
// ============================================================================

func (i *Interpreter) callJsonParse(e *ast.CallExpr) Value {
	jsonStr := i.evalExpr(e.Args[0]).(stringValue).v
	var data interface{}
	err := json.Unmarshal([]byte(jsonStr), &data)
	if err != nil {
		return enumValue{Variant: "Err", Payload: stringValue{v: err.Error()}}
	}
	
	return enumValue{Variant: "Ok", Payload: i.goValueToJsonNode(data)}
}

func (i *Interpreter) goValueToJsonNode(data interface{}) Value {
	if data == nil {
		return enumValue{Variant: "Null"}
	}
	switch v := data.(type) {
	case bool:
		return enumValue{Variant: "Bool", Payload: boolValue{v: v}}
	case float64:
		return enumValue{Variant: "Number", Payload: floatValue{v: v}}
	case string:
		return enumValue{Variant: "String", Payload: stringValue{v: v}}
	case []interface{}:
		var lst listValue
		for _, elem := range v {
			lst.elements = append(lst.elements, i.goValueToJsonNode(elem))
		}
		fields := make(map[string]Value)
		fields["items"] = lst
		arr := structValue{fields: fields}
		return enumValue{Variant: "Array", Payload: arr}
	case map[string]interface{}:
		var keys listValue
		var values listValue
		for k, val := range v {
			keys.elements = append(keys.elements, stringValue{v: k})
			values.elements = append(values.elements, i.goValueToJsonNode(val))
		}
		
		// Create JsonObject struct (keys, values)
		fields := make(map[string]Value)
		fields["keys"] = keys
		fields["values"] = values
		obj := structValue{fields: fields}
		
		return enumValue{Variant: "Object", Payload: obj}
	}
	return enumValue{Variant: "Null"}
}

func (i *Interpreter) callCsvParse(e *ast.CallExpr) Value {
	csvStr := i.evalExpr(e.Args[0]).(stringValue).v
	reader := csv.NewReader(strings.NewReader(csvStr))
	records, err := reader.ReadAll()
	if err != nil {
		return enumValue{Variant: "Err", Payload: stringValue{v: err.Error()}}
	}
	
	var outer listValue
	for _, row := range records {
		var inner listValue
		for _, col := range row {
			inner.elements = append(inner.elements, stringValue{v: col})
		}
		outer.elements = append(outer.elements, inner)
	}
	return enumValue{Variant: "Ok", Payload: outer}
}

// ============================================================================
// Testing Framework Built-ins
// ============================================================================

func (i *Interpreter) callAssertEq(e *ast.CallExpr) Value {
	actual := i.evalExpr(e.Args[0])
	expected := i.evalExpr(e.Args[1])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	
	// Compare string representations for structural equality
	// In the future, this should do a deep recursive equality check, but this is safe for primitive types
	actStr := actual.String()
	expStr := expected.String()
	
	if actStr != expStr {
		panic(fmt.Sprintf("assert_eq failed: expected %s, got %s", expStr, actStr))
	}
	return nullValue{}
}

func (i *Interpreter) callAssertTrue(e *ast.CallExpr) Value {
	cond := i.evalExpr(e.Args[0])
	if len(i.errs) > 0 {
		return nullValue{}
	}
	bv, ok := cond.(boolValue)
	if !ok {
		panic(fmt.Sprintf("assert_true failed: expression did not evaluate to a boolean, got %s", cond.String()))
	}
	if !bv.v {
		panic("assert_true failed: condition was false")
	}
	return nullValue{}
}
