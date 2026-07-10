// compiler.go — the AST-to-bytecode compiler for Whale v0.1.
//
// The compiler walks a parsed AST and emits bytecode into a
// Chunk. It maintains local variable scope, handles nested
// blocks, and back-patches jump instructions for control flow.
//
// What the compiler supports in Commit 2:
//   - All literals (int, float, string, bool)
//   - Binary operators (+, -, *, /, ==, !=, <, <=, >, >=)
//   - Local variables (let, with shadowing in nested blocks)
//   - if/else if/else
//   - while loops
//   - for-in over lists
//   - print (compiled as a call to a built-in function index)
//
// What the compiler does NOT yet support (deferred to Commit 3):
//   - User-defined function calls (OP_CALL)
//   - Closures (OP_MAKE_CLOSURE, OP_LOAD_FREE)
//   - Streams
//   - Struct literals and field access
//   - String interpolation
package vm

import (
	"fmt"

	"github.com/whale-lang/whale/internal/ast"
)

// Compile walks an AST file and produces a chunk of bytecode.
// Returns the chunk and any compile errors.
func Compile(file ast.File) (*Chunk, []error) {
	var errs []error
	c := newCompiler("<program>")
	c.errors = &errs
	for _, stmt := range file.Body {
		if err := c.compileStmt(stmt); err != nil {
			if len(errs) == 0 {
				errs = append(errs, err)
			}
			return c.chunk, errs
		}
	}
	c.emit(OP_HALT, 0, 0)
	return c.chunk, errs
}

// ----------------------------------------------------------------------------
// Compiler state
// ----------------------------------------------------------------------------

// Local tracks a local variable in the current scope. Slot is
// the index in the chunk's locals array; depth is the scope
// depth at which the variable was declared.
type Local struct {
	Name  string
	Depth int
	Slot  int
}

type Upvalue struct {
	Index   int
	IsLocal bool
}

type Compiler struct {
	chunk      *Chunk
	locals     []Local         // locals in scope, in declaration order
	scopeDepth int             // current block nesting depth
	errors     *[]error
	parent     *Compiler
	upvalues   []Upvalue
	// stackHeight tracks the expected stack height. We
	// increment before each emit that pushes, and decrement
	// after each emit that pops. This catches compiler bugs
	// at compile time rather than letting them corrupt the
	// stack at runtime.
	stackHeight int
}

func newCompiler(name string) *Compiler {
	return &Compiler{
		chunk:      NewChunk(name),
		locals:     make([]Local, 0, 8),
		scopeDepth: 0,
		stackHeight: 0,
	}
}

// ----------------------------------------------------------------------------
// Emit helpers
// ----------------------------------------------------------------------------

func (c *Compiler) emit(op Opcode, a, b int) int {
	pos := len(c.chunk.Code)
	c.chunk.Emit(op, a, b)
	// Update expected stack height based on the opcode.
	c.applyStackEffect(op, a)
	return pos
}

func NewCompiler(errs *[]error) *Compiler {
	c := newCompiler("")
	c.errors = errs
	return c
}

// applyStackEffect adjusts stackHeight based on the opcode.
func (c *Compiler) applyStackEffect(op Opcode, operand int) {
	switch op {
	case OP_PUSH_CONST, OP_LOAD:
		c.stackHeight++
	case OP_POP:
		c.stackHeight--
	case OP_STORE:
		c.stackHeight--
	case OP_ADD, OP_SUB, OP_MUL, OP_DIV, OP_MOD,
		OP_EQ, OP_NEQ, OP_LT, OP_LTE, OP_GT, OP_GTE:
		c.stackHeight--
	case OP_PRINT:
		c.stackHeight--
	case OP_JUMP, OP_LOOP, OP_JUMP_IF_FALSE:
	case OP_CALL:
		c.stackHeight -= operand
	case OP_RETURN:
		c.stackHeight--
	case OP_MAKE_FN:
		c.stackHeight++
	case OP_HALT:
	}
}

// emitJump appends a jump instruction with a placeholder target
// and returns the position of the jump. The caller must later
// call patchJump with this position once the target is known.
func (c *Compiler) emitJump(op Opcode) int {
	return c.emit(op, 0xFFFF, 0) // 0xFFFF is a placeholder
}

// patchJump fills in the target of a previously-emitted jump.
// The target is the current code length (the address of the
// next instruction to be emitted).
func (c *Compiler) patchJump(pos int) {
	target := len(c.chunk.Code)
	c.chunk.Code[pos].A = target
}

// emitLoop emits a backward jump. The target is the address
// of the loop's start (which is known at emit time).
func (c *Compiler) emitLoop(loopStart int) int {
	// The OP_LOOP instruction will be emitted at index `len(c.chunk.Code)`.
	// At runtime, after reading OP_LOOP, `pc` will be `len(c.chunk.Code) + 1`.
	// We want `pc` to become `loopStart`.
	// So `pc -= offset` => `offset = pc - loopStart`
	// => `offset = len(c.chunk.Code) + 1 - loopStart`.
	offset := len(c.chunk.Code) + 1 - loopStart
	return c.emit(OP_LOOP, offset, 0)
}

// ----------------------------------------------------------------------------
// Scope management
// ----------------------------------------------------------------------------

// beginScope increments the scope depth. New locals declared
// from now until endScope() are at the new depth.
func (c *Compiler) beginScope() {
	c.scopeDepth++
}

// endScope removes locals at the current scope depth from
// visibility. Since our VM uses a separate locals array
// (not the data stack) for locals, we don't need to emit
// OP_POP at runtime.
func (c *Compiler) endScope() {
	c.scopeDepth--
	for len(c.locals) > 0 && c.locals[len(c.locals)-1].Depth > c.scopeDepth {
		c.locals = c.locals[:len(c.locals)-1]
	}
}

// resolveLocal looks up a name in the current scope chain.
// Returns the slot index and true on hit, or 0 and false on
// miss. We always start the search from the most recent
// local so that inner-scope names shadow outer ones.
func (c *Compiler) resolveLocal(name string) (int, bool) {
	for i := len(c.locals) - 1; i >= 0; i-- {
		if c.locals[i].Name == name {
			return c.locals[i].Slot, true
		}
	}
	return -1, false
}

func (c *Compiler) resolveFree(name string) (int, bool) {
	if c.parent == nil {
		return -1, false
	}
	// Try to resolve as local in parent.
	localSlot, ok := c.parent.resolveLocal(name)
	if ok {
		return c.addUpvalue(localSlot, true, name), true
	}
	// Try to resolve as free in parent.
	freeSlot, ok := c.parent.resolveFree(name)
	if ok {
		return c.addUpvalue(freeSlot, false, name), true
	}
	return -1, false
}

func (c *Compiler) addUpvalue(index int, isLocal bool, name string) int {
	for i, uv := range c.upvalues {
		if uv.Index == index && uv.IsLocal == isLocal {
			return i
		}
	}
	c.upvalues = append(c.upvalues, Upvalue{Index: index, IsLocal: isLocal})
	c.chunk.Free = append(c.chunk.Free, name)
	return len(c.upvalues) - 1
}

// addLocal registers a new local and returns its slot.
// If we're at the top level (scope depth 0), this is a
// global; in v0.2 the VM will treat them differently.
// For now, all locals go in the same array.
func (c *Compiler) addLocal(name string) int {
	slot := len(c.chunk.Locals)
	c.chunk.Locals = append(c.chunk.Locals, name)
	c.locals = append(c.locals, Local{
		Name:  name,
		Depth: c.scopeDepth,
		Slot:  slot,
	})
	return slot
}

// ----------------------------------------------------------------------------
// Statement compilation
// ----------------------------------------------------------------------------

func (c *Compiler) compileStmt(s ast.Stmt) error {
	switch s := s.(type) {
	case *ast.LetStmt:
		return c.compileLet(s)
	case *ast.ExprStmt:
		return c.compileExprStmt(s)
	case *ast.IfStmt:
		return c.compileIf(s)
	case *ast.WhileStmt:
		return c.compileWhile(s)
	case *ast.ForStmt:
		return c.compileFor(s)
	case *ast.AssignStmt:
		return c.compileAssign(s)
	case *ast.ReturnStmt:
		return c.compileReturn(s)
	case *ast.BlockStmt:
		return c.compileBlock(s)
	case *ast.StructDecl:
		// Struct declarations are a no-op for the VM, as struct fields 
		// and type names are captured at instantiation time by StructLit.
		return nil
	case *ast.FnStmt:
		// In Commit 2 we don't support user-defined functions.
		// A FnStmt at the top level is a forward declaration;
		// the tree-walker handled these directly. For the VM,
		// we record the function in a side table but emit
		// nothing — the function exists but isn't callable.
		// Commit 3 will emit OP_MAKE_FN and proper calls.
		return c.compileFnDecl(s)
	}
	return c.errorf(s, "unsupported statement: %T", s)
}

func (c *Compiler) compileLet(s *ast.LetStmt) error {
	if err := c.compileExpr(s.Value); err != nil {
		return err
	}
	if c.scopeDepth > 0 {
		slot := c.addLocal(s.Name)
		c.emit(OP_STORE, slot, 0)
	} else {
		// Top level: store as a "global" in a local slot too.
		// This is a temporary measure until Commit 3, which
		// will have proper global handling.
		slot := c.addLocal(s.Name)
		c.emit(OP_STORE, slot, 0)
	}
	return nil
}

func (c *Compiler) compileExprStmt(s *ast.ExprStmt) error {
	if err := c.compileExpr(s.Expr); err != nil {
		return err
	}
	c.emit(OP_POP, 0, 0)
	return nil
}

func (c *Compiler) compileAssign(s *ast.AssignStmt) error {
	// Resolve the target.
	slot, ok := c.resolveLocal(s.Name)
	if !ok {
		return c.errorf(s, "undefined name: %s", s.Name)
	}
	// Compile the value.
	if err := c.compileExpr(s.Value); err != nil {
		return err
	}
	// Emit a store. But STORE pops the value off the stack.
	// For assignment, we want the value to remain accessible
	// after the assignment (in case it's used as an expression).
	// In v0.1, assignments are statements, so the value can be
	// popped. If we later make them expressions, we'd need
	// a different op.
	c.emit(OP_STORE, slot, 0)
	return nil
}

func (c *Compiler) compileReturn(s *ast.ReturnStmt) error {
	if s.Value != nil {
		if err := c.compileExpr(s.Value); err != nil {
			return err
		}
	} else {
		// No value: push Unit as the return value.
		unitIdx := c.chunk.addConst(Unit{})
		c.emit(OP_PUSH_CONST, unitIdx, 0)
	}
	c.emit(OP_RETURN, 0, 0)
	return nil
}

func (c *Compiler) compileFnDecl(s *ast.FnStmt) error {
	// Declare the function name in the current scope to allow recursion.
	slot := c.addLocal(s.Name)

	// Commit 3a/3b: compile the function into a new chunk.
	fnCompiler := newCompiler(s.Name)
	fnCompiler.errors = c.errors
	fnCompiler.parent = c
	fnCompiler.chunk.Arity = len(s.Params)

	fnCompiler.beginScope()
	for _, p := range s.Params {
		fnCompiler.addLocal(p.Name)
	}

	if err := fnCompiler.compileBlock(s.Body); err != nil {
		return err
	}

	// Implicit return at the end of the function.
	unitIdx := fnCompiler.chunk.addConst(Unit{})
	fnCompiler.emit(OP_PUSH_CONST, unitIdx, 0)
	fnCompiler.emit(OP_RETURN, 0, 0)

	// Add the compiled chunk to the parent's constants.
	chunkIdx := c.chunk.addConst(fnCompiler.chunk)

	if len(fnCompiler.upvalues) == 0 {
		c.emit(OP_MAKE_FN, chunkIdx, 0)
	} else {
		// Emit code to load each upvalue onto the stack.
		for _, uv := range fnCompiler.upvalues {
			if uv.IsLocal {
				c.emit(OP_LOAD, uv.Index, 0)
			} else {
				c.emit(OP_LOAD_FREE, uv.Index, 0)
			}
		}
		c.emit(OP_MAKE_CLOSURE, chunkIdx, 0)
	}

	c.emit(OP_STORE, slot, 0)
	return nil
}

// ----------------------------------------------------------------------------
// if/else
// ----------------------------------------------------------------------------

func (c *Compiler) compileIf(s *ast.IfStmt) error {
	// Compile the condition.
	if err := c.compileExpr(s.Condition); err != nil {
		return err
	}
	// Jump to else if condition is false.
	thenJump := c.emitJump(OP_JUMP_IF_FALSE)
	// Pop the condition value (since we branched on it).
	c.emit(OP_POP, 0, 0)
	// Compile the then-block.
	if err := c.compileBlock(s.Then); err != nil {
		return err
	}
	// Jump over the else-block.
	elseJump := c.emitJump(OP_JUMP)
	// Patch: thenJump lands here (start of else or end of if).
	c.patchJump(thenJump)
	// Pop the condition that was left on the stack by the
	// JUMP_IF_FALSE not taking the branch.
	c.emit(OP_POP, 0, 0)
	// Compile the else-branch if present.
	if s.Else != nil {
		if err := c.compileStmt(s.Else); err != nil {
			return err
		}
	}
	// Patch: elseJump lands here.
	c.patchJump(elseJump)
	return nil
}

// ----------------------------------------------------------------------------
// while
// ----------------------------------------------------------------------------

func (c *Compiler) compileWhile(s *ast.WhileStmt) error {
	// Remember the loop start so we can jump back to it.
	loopStart := len(c.chunk.Code)
	// Compile the condition.
	if err := c.compileExpr(s.Condition); err != nil {
		return err
	}
	// Jump out of the loop if condition is false.
	exitJump := c.emitJump(OP_JUMP_IF_FALSE)
	// Pop the condition (we're taking the branch).
	c.emit(OP_POP, 0, 0)
	// Compile the body.
	if err := c.compileBlock(s.Body); err != nil {
		return err
	}
	// Jump back to the loop start.
	c.emitLoop(loopStart)
	// Patch the exit jump to land here.
	c.patchJump(exitJump)
	// Pop the condition (we exited without taking the branch).
	c.emit(OP_POP, 0, 0)
	return nil
}

// ----------------------------------------------------------------------------
// for
// ----------------------------------------------------------------------------

func (c *Compiler) compileFor(s *ast.ForStmt) error {
	if err := c.compileExpr(s.Iterable); err != nil {
		return err
	}
	return c.errorf(s, "for-in is not yet supported by the VM (Commit 3)")
}

// ----------------------------------------------------------------------------
// Blocks
// ----------------------------------------------------------------------------

func (c *Compiler) compileBlock(b *ast.BlockStmt) error {
	c.beginScope()
	for _, stmt := range b.Body {
		if err := c.compileStmt(stmt); err != nil {
			return err
		}
	}
	c.endScope()
	return nil
}

// ----------------------------------------------------------------------------
// Expressions
// ----------------------------------------------------------------------------

func (c *Compiler) compileExpr(e ast.Expr) error {
	if e == nil {
		return fmt.Errorf("nil expression")
	}
	switch e := e.(type) {
	case *ast.IntLit:
		return c.compileIntLit(e)
	case *ast.FloatLit:
		return c.compileFloatLit(e)
	case *ast.StringLit:
		return c.compileStringLit(e)
	case *ast.BoolLit:
		return c.compileBoolLit(e)
	case *ast.Ident:
		return c.compileIdent(e)
	case *ast.BinaryOp:
		return c.compileBinary(e)
	case *ast.CallExpr:
		return c.compileCall(e)
	case *ast.FnLit:
		return c.compileFnLit(e)
	case *ast.ListLit:
		return c.compileListLit(e)
	case *ast.IndexExpr:
		return c.compileIndex(e)
	case *ast.StructLit:
		return c.compileStructLit(e)
	case *ast.FieldAccess:
		return c.compileFieldAccess(e)
	}
	return c.errorf(e, "unsupported expression: %T", e)
}

func (c *Compiler) compileIdent(e *ast.Ident) error {
	slot, ok := c.resolveLocal(e.Name)
	if ok {
		c.emit(OP_LOAD, slot, 0)
		return nil
	}

	slot, ok = c.resolveFree(e.Name)
	if ok {
		c.emit(OP_LOAD_FREE, slot, 0)
		return nil
	}

	return c.errorf(e, "undefined name: %s", e.Name)
}

func (c *Compiler) compileFnLit(e *ast.FnLit) error {
	fnCompiler := newCompiler("lambda")
	fnCompiler.errors = c.errors
	fnCompiler.parent = c
	fnCompiler.chunk.Name = "lambda"
	fnCompiler.chunk.Arity = len(e.Params)

	fnCompiler.beginScope()
	for _, p := range e.Params {
		fnCompiler.addLocal(p.Name)
	}

	if e.Body.Expr != nil {
		if err := fnCompiler.compileExpr(e.Body.Expr); err != nil {
			return err
		}
		fnCompiler.emit(OP_RETURN, 0, 0)
	} else {
		if err := fnCompiler.compileBlock(e.Body.Block); err != nil {
			return err
		}
		unitIdx := fnCompiler.chunk.addConst(Unit{})
		fnCompiler.emit(OP_PUSH_CONST, unitIdx, 0)
		fnCompiler.emit(OP_RETURN, 0, 0)
	}

	chunkIdx := c.chunk.addConst(fnCompiler.chunk)

	if len(fnCompiler.upvalues) == 0 {
		c.emit(OP_MAKE_FN, chunkIdx, 0)
	} else {
		for _, uv := range fnCompiler.upvalues {
			if uv.IsLocal {
				c.emit(OP_LOAD, uv.Index, 0)
			} else {
				c.emit(OP_LOAD_FREE, uv.Index, 0)
			}
		}
		c.emit(OP_MAKE_CLOSURE, chunkIdx, len(fnCompiler.upvalues))
	}
	return nil
}

func (c *Compiler) compileIntLit(e *ast.IntLit) error {
	idx := c.chunk.addConst(Int{V: e.Value})
	c.emit(OP_PUSH_CONST, idx, 0)
	return nil
}

func (c *Compiler) compileFloatLit(e *ast.FloatLit) error {
	idx := c.chunk.addConst(Float{V: e.Value})
	c.emit(OP_PUSH_CONST, idx, 0)
	return nil
}

func (c *Compiler) compileStringLit(e *ast.StringLit) error {
	idx := c.chunk.addConst(String{V: e.Value})
	c.emit(OP_PUSH_CONST, idx, 0)
	return nil
}

func (c *Compiler) compileBoolLit(e *ast.BoolLit) error {
	idx := c.chunk.addConst(Bool{V: e.Value})
	c.emit(OP_PUSH_CONST, idx, 0)
	return nil
}


func (c *Compiler) compileBinary(e *ast.BinaryOp) error {
	// Compile left, then right. Stack now has [left, right].
	if err := c.compileExpr(e.Left); err != nil {
		return err
	}
	if err := c.compileExpr(e.Right); err != nil {
		return err
	}
	// Emit the operator. Stack now has [result].
	switch e.Op {
	case "+":
		c.emit(OP_ADD, 0, 0)
	case "-":
		c.emit(OP_SUB, 0, 0)
	case "*":
		c.emit(OP_MUL, 0, 0)
	case "/":
		c.emit(OP_DIV, 0, 0)
	case "%":
		c.emit(OP_MOD, 0, 0)
	case "==":
		c.emit(OP_EQ, 0, 0)
	case "!=":
		c.emit(OP_NEQ, 0, 0)
	case "<":
		c.emit(OP_LT, 0, 0)
	case "<=":
		c.emit(OP_LTE, 0, 0)
	case ">":
		c.emit(OP_GT, 0, 0)
	case ">=":
		c.emit(OP_GTE, 0, 0)
	default:
		return c.errorf(e, "unsupported operator: %s", e.Op)
	}
	return nil
}

type streamOp struct {
	name string
	args []ast.Expr
}

func (c *Compiler) isStreamPipeline(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Callee.(*ast.Ident)
	if !ok {
		return false
	}
	switch id.Name {
	case "stream", "filter", "map":
		return true
	}
	return false
}

func (c *Compiler) compileStreamPipeline(source ast.Expr, terminal string, terminalArgs []ast.Expr) error {
	var ops []streamOp
	curr := source
	for {
		call, ok := curr.(*ast.CallExpr)
		if !ok {
			return c.errorf(curr, "invalid stream pipeline source")
		}
		id := call.Callee.(*ast.Ident)
		if id.Name == "stream" {
			ops = append(ops, streamOp{"stream", call.Args})
			break
		}
		ops = append(ops, streamOp{id.Name, call.Args})
		curr = call.Args[0]
	}

	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}

	listExpr := ops[0].args[0]
	if err := c.compileExpr(listExpr); err != nil {
		return err
	}

	c.beginScope()
	listSlot := c.addLocal(".stream_list")
	c.emit(OP_STORE, listSlot, 0)

	idxIdx := c.chunk.addConst(Int{V: 0})
	c.emit(OP_PUSH_CONST, idxIdx, 0)
	idxSlot := c.addLocal(".stream_idx")
	c.emit(OP_STORE, idxSlot, 0)

	var resSlot int
	if terminal == "collect" {
		c.emit(OP_MAKE_LIST, 0, 0)
		resSlot = c.addLocal(".stream_res")
		c.emit(OP_STORE, resSlot, 0)
	} else if terminal == "fold" {
		if err := c.compileExpr(terminalArgs[0]); err != nil {
			return err
		}
		resSlot = c.addLocal(".stream_acc")
		c.emit(OP_STORE, resSlot, 0)
	}

	loopStart := len(c.chunk.Code)

	c.emit(OP_LOAD, idxSlot, 0)
	c.emit(OP_LOAD, listSlot, 0)
	c.emit(OP_BUILTIN, 0, 0) // len
	c.emit(OP_LT, 0, 0)

	jumpEnd := c.emitJump(OP_JUMP_IF_FALSE)

	c.emit(OP_LOAD, listSlot, 0)
	c.emit(OP_LOAD, idxSlot, 0)
	c.emit(OP_LOAD_INDEX, 0, 0)

	var nextJumps []int

	for i := 1; i < len(ops); i++ {
		op := ops[i]
		if op.name == "filter" {
			elemSlot := c.addLocal(".stream_elem")
			c.emit(OP_STORE, elemSlot, 0)

			if err := c.compileExpr(op.args[1]); err != nil {
				return err
			}
			c.emit(OP_LOAD, elemSlot, 0)
			c.emit(OP_CALL, 1, 0)

			jumpNext := c.emitJump(OP_JUMP_IF_FALSE)
			nextJumps = append(nextJumps, jumpNext)

			c.emit(OP_LOAD, elemSlot, 0)
		} else if op.name == "map" {
			elemSlot := c.addLocal(".stream_elem")
			c.emit(OP_STORE, elemSlot, 0)

			if err := c.compileExpr(op.args[1]); err != nil {
				return err
			}
			c.emit(OP_LOAD, elemSlot, 0)
			c.emit(OP_CALL, 1, 0)
		}
	}

	if terminal == "collect" {
		c.emit(OP_LOAD, resSlot, 0)
		c.emit(OP_LIST_APPEND, 0, 0)
		c.emit(OP_STORE, resSlot, 0)
	} else if terminal == "for_each" {
		elemSlot := c.addLocal(".stream_elem")
		c.emit(OP_STORE, elemSlot, 0)

		if err := c.compileExpr(terminalArgs[0]); err != nil {
			return err
		}
		c.emit(OP_LOAD, elemSlot, 0)
		c.emit(OP_CALL, 1, 0)
		c.emit(OP_POP, 0, 0)
	} else if terminal == "fold" {
		elemSlot := c.addLocal(".stream_elem")
		c.emit(OP_STORE, elemSlot, 0)

		if err := c.compileExpr(terminalArgs[1]); err != nil {
			return err
		}
		c.emit(OP_LOAD, resSlot, 0)
		c.emit(OP_LOAD, elemSlot, 0)
		c.emit(OP_CALL, 2, 0)
		c.emit(OP_STORE, resSlot, 0)
	}

	if len(nextJumps) > 0 {
		jumpToLoopEnd := c.emitJump(OP_JUMP)
		for _, jump := range nextJumps {
			c.patchJump(jump)
		}
		c.patchJump(jumpToLoopEnd)
	}

	c.emit(OP_LOAD, idxSlot, 0)
	oneIdx := c.chunk.addConst(Int{V: 1})
	c.emit(OP_PUSH_CONST, oneIdx, 0)
	c.emit(OP_ADD, 0, 0)
	c.emit(OP_STORE, idxSlot, 0)
	c.emitLoop(loopStart)

	c.patchJump(jumpEnd)

	if terminal == "collect" || terminal == "fold" {
		c.emit(OP_LOAD, resSlot, 0)
	} else {
		unitIdx := c.chunk.addConst(Unit{})
		c.emit(OP_PUSH_CONST, unitIdx, 0)
	}
	
	c.endScope()
	return nil
}

func (c *Compiler) compileCall(e *ast.CallExpr) error {
	// First, check if it's a builtin.
	id, ok := e.Callee.(*ast.Ident)
	if ok {
		switch id.Name {
		case "print":
			for _, arg := range e.Args {
				if err := c.compileExpr(arg); err != nil {
					return err
				}
				c.emit(OP_PRINT, 0, 0)
			}
			unitIdx := c.chunk.addConst(Unit{})
			c.emit(OP_PUSH_CONST, unitIdx, 0)
			return nil
		case "len":
			if len(e.Args) != 1 {
				return c.errorf(e, "len() takes 1 argument")
			}
			if err := c.compileExpr(e.Args[0]); err != nil {
				return err
			}
			c.emit(OP_BUILTIN, 0, 0) // index 0 = len
			return nil
		case "to_string":
			if len(e.Args) != 1 {
				return c.errorf(e, "to_string() takes 1 argument")
			}
			if err := c.compileExpr(e.Args[0]); err != nil {
				return err
			}
			c.emit(OP_BUILTIN, 1, 0) // index 1 = to_string
			return nil
		case "contains":
			if len(e.Args) != 2 {
				return c.errorf(e, "contains() takes 2 arguments")
			}
			if err := c.compileExpr(e.Args[0]); err != nil {
				return err
			}
			if err := c.compileExpr(e.Args[1]); err != nil {
				return err
			}
			c.emit(OP_BUILTIN, 2, 0) // index 2 = contains
			return nil
		case "read_file":
			if len(e.Args) != 1 { return c.errorf(e, "read_file() takes 1 argument") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			c.emit(OP_BUILTIN, 3, 0)
			return nil
		case "write_file":
			if len(e.Args) != 2 { return c.errorf(e, "write_file() takes 2 arguments") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			if err := c.compileExpr(e.Args[1]); err != nil { return err }
			c.emit(OP_BUILTIN, 4, 0)
			return nil
		case "lines":
			if len(e.Args) != 1 { return c.errorf(e, "lines() takes 1 argument") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			c.emit(OP_BUILTIN, 5, 0)
			return nil
		case "split":
			if len(e.Args) != 2 { return c.errorf(e, "split() takes 2 arguments") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			if err := c.compileExpr(e.Args[1]); err != nil { return err }
			c.emit(OP_BUILTIN, 6, 0)
			return nil
		case "trim":
			if len(e.Args) != 1 { return c.errorf(e, "trim() takes 1 argument") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			c.emit(OP_BUILTIN, 7, 0)
			return nil
		case "replace":
			if len(e.Args) != 3 { return c.errorf(e, "replace() takes 3 arguments") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			if err := c.compileExpr(e.Args[1]); err != nil { return err }
			if err := c.compileExpr(e.Args[2]); err != nil { return err }
			c.emit(OP_BUILTIN, 8, 0)
			return nil
		case "to_lower":
			if len(e.Args) != 1 { return c.errorf(e, "to_lower() takes 1 argument") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			c.emit(OP_BUILTIN, 9, 0)
			return nil
		case "to_upper":
			if len(e.Args) != 1 { return c.errorf(e, "to_upper() takes 1 argument") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			c.emit(OP_BUILTIN, 10, 0)
			return nil
		case "abs":
			if len(e.Args) != 1 { return c.errorf(e, "abs() takes 1 argument") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			c.emit(OP_BUILTIN, 11, 0)
			return nil
		case "max":
			if len(e.Args) != 2 { return c.errorf(e, "max() takes 2 arguments") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			if err := c.compileExpr(e.Args[1]); err != nil { return err }
			c.emit(OP_BUILTIN, 12, 0)
			return nil
		case "min":
			if len(e.Args) != 2 { return c.errorf(e, "min() takes 2 arguments") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			if err := c.compileExpr(e.Args[1]); err != nil { return err }
			c.emit(OP_BUILTIN, 13, 0)
			return nil
		case "parse_int":
			if len(e.Args) != 1 { return c.errorf(e, "parse_int() takes 1 argument") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			c.emit(OP_BUILTIN, 14, 0)
			return nil
		case "type_of":
			if len(e.Args) != 1 { return c.errorf(e, "type_of() takes 1 argument") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			c.emit(OP_BUILTIN, 15, 0)
			return nil
		case "push":
			if len(e.Args) != 2 { return c.errorf(e, "push() takes 2 arguments") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			if err := c.compileExpr(e.Args[1]); err != nil { return err }
			c.emit(OP_BUILTIN, 16, 0)
			return nil
		case "pop":
			if len(e.Args) != 1 { return c.errorf(e, "pop() takes 1 argument") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			c.emit(OP_BUILTIN, 17, 0)
			return nil
		case "stream":
			if len(e.Args) != 1 { return c.errorf(e, "stream() takes 1 argument") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			c.emit(OP_MAKE_STREAM, 0, 0)
			return nil
		case "filter":
			if len(e.Args) != 2 { return c.errorf(e, "filter() takes 2 arguments") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			if err := c.compileExpr(e.Args[1]); err != nil { return err }
			c.emit(OP_STREAM_FILTER, 0, 0)
			return nil
		case "map":
			if len(e.Args) != 2 { return c.errorf(e, "map() takes 2 arguments") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			if err := c.compileExpr(e.Args[1]); err != nil { return err }
			c.emit(OP_STREAM_MAP, 0, 0)
			return nil
		case "collect":
			if len(e.Args) != 1 { return c.errorf(e, "collect() takes 1 argument") }
			if c.isStreamPipeline(e.Args[0]) {
				return c.compileStreamPipeline(e.Args[0], "collect", nil)
			}
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			c.emit(OP_STREAM_COLLECT, 0, 0)
			return nil
		case "for_each":
			if len(e.Args) != 2 { return c.errorf(e, "for_each() takes 2 arguments") }
			if c.isStreamPipeline(e.Args[0]) {
				return c.compileStreamPipeline(e.Args[0], "for_each", []ast.Expr{e.Args[1]})
			}
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			if err := c.compileExpr(e.Args[1]); err != nil { return err }
			c.emit(OP_STREAM_FOREACH, 0, 0)
			return nil
		case "take":
			if len(e.Args) != 2 { return c.errorf(e, "take() takes 2 arguments") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			if err := c.compileExpr(e.Args[1]); err != nil { return err }
			c.emit(OP_STREAM_TAKE, 0, 0)
			return nil
		case "skip":
			if len(e.Args) != 2 { return c.errorf(e, "skip() takes 2 arguments") }
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			if err := c.compileExpr(e.Args[1]); err != nil { return err }
			c.emit(OP_STREAM_SKIP, 0, 0)
			return nil
		case "fold":
			if len(e.Args) != 3 { return c.errorf(e, "fold() takes 3 arguments") }
			if c.isStreamPipeline(e.Args[0]) {
				return c.compileStreamPipeline(e.Args[0], "fold", []ast.Expr{e.Args[1], e.Args[2]})
			}
			if err := c.compileExpr(e.Args[0]); err != nil { return err }
			if err := c.compileExpr(e.Args[1]); err != nil { return err }
			if err := c.compileExpr(e.Args[2]); err != nil { return err }
			c.emit(OP_STREAM_FOLD, 0, 0)
			return nil
		}
	}

	// Regular function call.
	// Evaluate the callee.
	if err := c.compileExpr(e.Callee); err != nil {
		return err
	}
	
	// Evaluate arguments.
	for _, arg := range e.Args {
		if err := c.compileExpr(arg); err != nil {
			return err
		}
	}
	
	c.emit(OP_CALL, len(e.Args), 0)
	return nil
}

func (c *Compiler) compileListLit(e *ast.ListLit) error {
	// Compile each element; they'll all be on the stack in order.
	for _, el := range e.Values {
		if err := c.compileExpr(el); err != nil {
			return err
		}
	}
	// The OP_MAKE_LIST instruction needs to know how many elements.
	c.emit(OP_MAKE_LIST, len(e.Values), 0)
	return nil
}

func (c *Compiler) compileIndex(e *ast.IndexExpr) error {
	if err := c.compileExpr(e.Expr); err != nil {
		return err
	}
	if err := c.compileExpr(e.Index); err != nil {
		return err
	}
	c.emit(OP_LOAD_INDEX, 0, 0)
	return nil
}

func (c *Compiler) compileStructLit(e *ast.StructLit) error {
	// A StructLit has a Type string and a Fields map[string]Expr.
	// We need to push the values and names for each field,
	// then the type name, then OP_MAKE_STRUCT.
	// We must ensure a consistent order to compile them,
	// but map iteration in Go is random. Wait, for execution order,
	// it's best to sort the keys, but it doesn't strictly matter
	// for correctness of the struct value, just for predictable bytecode.
	for k, v := range e.Fields {
		// Push key
		idx := c.chunk.addConst(String{V: k})
		c.emit(OP_PUSH_CONST, idx, 0)
		
		// Push value
		if err := c.compileExpr(v); err != nil {
			return err
		}
	}
	
	// Push type name
	typeIdx := c.chunk.addConst(String{V: e.Type})
	c.emit(OP_PUSH_CONST, typeIdx, 0)
	
	c.emit(OP_MAKE_STRUCT, len(e.Fields), 0)
	return nil
}

func (c *Compiler) compileFieldAccess(e *ast.FieldAccess) error {
	if err := c.compileExpr(e.Expr); err != nil {
		return err
	}
	idx := c.chunk.addConst(String{V: e.Field})
	c.emit(OP_LOAD_FIELD, idx, 0)
	return nil
}

// ----------------------------------------------------------------------------
// Error reporting
// ----------------------------------------------------------------------------

func (c *Compiler) errorf(node ast.Node, format string, args ...interface{}) error {
	pos := nodePosition(node)
	msg := fmt.Sprintf(format, args...)
	if c.errors != nil {
		*c.errors = append(*c.errors, fmt.Errorf("%d:%d: %s", pos.Line, pos.Col, msg))
	}
	return fmt.Errorf("%d:%d: %s", pos.Line, pos.Col, msg)
}

func nodePosition(n ast.Node) ast.Position {
	switch n := n.(type) {
	case *ast.LetStmt:
		return n.Pos
	case *ast.ExprStmt:
		return n.Pos
	case *ast.IfStmt:
		return n.Pos
	case *ast.WhileStmt:
		return n.Pos
	case *ast.ForStmt:
		return n.Pos
	case *ast.AssignStmt:
		return n.Pos
	case *ast.ReturnStmt:
		return n.Pos
	case *ast.FnStmt:
		return n.Pos
	case *ast.IntLit:
		return n.Pos
	case *ast.FloatLit:
		return n.Pos
	case *ast.StringLit:
		return n.Pos
	case *ast.BoolLit:
		return n.Pos
	case *ast.Ident:
		return n.Pos
	case *ast.BinaryOp:
		return n.Pos
	case *ast.CallExpr:
		return n.Pos
	case *ast.ListLit:
		return n.Pos
	}
	return ast.Position{}
}
