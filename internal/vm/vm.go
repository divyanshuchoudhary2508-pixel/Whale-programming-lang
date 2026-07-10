package vm

import (
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
)

// Run executes a chunk and returns any errors. The chunk's
// instructions are executed in order until OP_HALT or an
// error.
//
// In Commit 1 the VM has:
//   - A single stack (no call stack yet)
//   - A single locals array
//   - One output writer
//   - Arithmetic and comparison ops
//   - OP_PRINT for output
//   - No control flow (no JUMP, no IF, no WHILE)
//
// This is enough to run straight-line programs. Branches
// and function calls come in later commits.
func Run(chunk *Chunk) []error {
	return RunWithOutput(chunk, os.Stdout)
}

// RunWithOutput is Run but writes print output to the given writer.
// Used by tests that want to capture output.
func RunWithOutput(chunk *Chunk, out io.Writer) []error {
	frames := make([]*Frame, 0, 8)
	frames = append(frames, newFrame(chunk))
	stack := make([]Value, 0, 32)

	_, _, errs := runUntilReturn(frames, stack, out, 0)
	return errs
}

func runUntilReturn(frames []*Frame, stack []Value, out io.Writer, targetDepth int) ([]*Frame, []Value, []error) {
	var errs []error
	for len(frames) > targetDepth {
		frame := frames[len(frames)-1]
		if frame.pc >= len(frame.chunk.Code) {
			errs = append(errs, fmt.Errorf("VM: pc ran off end of code in %s (no HALT)", frame.chunk.Name))
			return frames, stack, errs
		}
		inst := frame.chunk.Code[frame.pc]
		frame.pc++

		switch inst.Op {
		case OP_PUSH_CONST:
			stack = append(stack, frame.chunk.Consts[inst.A])
		case OP_POP:
			if len(stack) == 0 {
				errs = append(errs, fmt.Errorf("VM: POP underflow at pc=%d in %s", frame.pc-1, frame.chunk.Name))
				return frames, stack, errs
			}
			stack = stack[:len(stack)-1]

		case OP_LOAD:
			stack = append(stack, frame.locals[inst.A])

		case OP_STORE:
			if len(stack) == 0 {
				errs = append(errs, fmt.Errorf("VM: STORE underflow"))
				return frames, stack, errs
			}
			frame.locals[inst.A] = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
		case OP_LOAD_FREE:
			slot := frame.freeStart + inst.A
			stack = append(stack, frame.locals[slot])
		case OP_ADD, OP_SUB, OP_MUL, OP_DIV, OP_MOD,
			OP_EQ, OP_NEQ, OP_LT, OP_LTE, OP_GT, OP_GTE:
			errs = append(errs, doBinaryOp(&stack, opToken(inst.Op), errs)...)

		case OP_JUMP:
			frame.pc = inst.A

		case OP_JUMP_IF_FALSE:
			if len(stack) == 0 {
				errs = append(errs, fmt.Errorf("VM: JUMP_IF_FALSE underflow"))
				return frames, stack, errs
			}
			cond := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if b, ok := cond.(Bool); ok {
				if !b.V {
					frame.pc = inst.A
				}
			} else {
				if !isTruthy(cond) {
					frame.pc = inst.A
				}
			}

		case OP_LOOP:
			frame.pc -= inst.A

		case OP_CALL:
			arity := inst.A
			if len(stack) < arity+1 {
				errs = append(errs, fmt.Errorf("VM: CALL underflow (need %d, have %d)", arity+1, len(stack)))
				return frames, stack, errs
			}
			calleeIdx := len(stack) - 1 - arity
			callee := stack[calleeIdx]
			closure, ok := callee.(Closure)
			if !ok {
				errs = append(errs, fmt.Errorf("VM: cannot call non-closure: %T", callee))
				return frames, stack, errs
			}
			if closure.Arity != arity {
				errs = append(errs, fmt.Errorf("VM: function %s expects %d args, got %d", closure.Name, closure.Arity, arity))
				return frames, stack, errs
			}
			args := make([]Value, arity)
			copy(args, stack[calleeIdx+1:calleeIdx+1+arity])
			stack = stack[:calleeIdx]

			callFrame := newFrame(closure.Code)
			for i, arg := range args {
				callFrame.locals[i] = arg
			}
			for i, fv := range closure.Free {
				callFrame.locals[len(closure.Code.Locals)+i] = fv
			}
			callFrame.freeStart = len(closure.Code.Locals)
			callFrame.numFree = len(closure.Free)
			frames = append(frames, callFrame)

		case OP_RETURN:
			var retval Value = Unit{}
			if len(stack) > 0 {
				retval = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
			frames = frames[:len(frames)-1]
			if len(frames) == targetDepth {
				stack = append(stack, retval)
				return frames, stack, errs
			}
			stack = append(stack, retval)

		case OP_MAKE_FN:
			fnChunkIdx := inst.A
			fnChunkVal := frame.chunk.Consts[fnChunkIdx]
			fnChunk := fnChunkVal.(*Chunk)
			closure := Closure{
				Name:  fnChunk.Name,
				Arity: fnChunk.Arity,
				Code:  fnChunk,
				Free:  nil,
			}
			stack = append(stack, closure)

		case OP_MAKE_CLOSURE:
			fnChunkIdx := inst.A
			fnChunkVal := frame.chunk.Consts[fnChunkIdx]
			fnChunk, ok := fnChunkVal.(*Chunk)
			if !ok {
				errs = append(errs, fmt.Errorf("VM: MAKE_CLOSURE expected *Chunk, got %T", fnChunkVal))
				return frames, stack, errs
			}
			numFree := len(fnChunk.Free)
			if len(stack) < numFree {
				errs = append(errs, fmt.Errorf("VM: MAKE_CLOSURE underflow (need %d, have %d)", numFree, len(stack)))
				return frames, stack, errs
			}
			free := make([]Value, numFree)
			freeBase := len(stack) - numFree
			copy(free, stack[freeBase:])
			stack = stack[:freeBase]
			closure := Closure{
				Name:  fnChunk.Name,
				Arity: fnChunk.Arity,
				Code:  fnChunk,
				Free:  free,
			}
			stack = append(stack, closure)

		case OP_MAKE_LIST:
			count := inst.A
			if len(stack) < count {
				errs = append(errs, fmt.Errorf("VM: MAKE_LIST underflow"))
				return frames, stack, errs
			}
			elements := make([]Value, count)
			copy(elements, stack[len(stack)-count:])
			stack = stack[:len(stack)-count]
			stack = append(stack, List{Elements: elements})

		case OP_LIST_APPEND:
			if len(stack) < 2 {
				errs = append(errs, fmt.Errorf("VM: LIST_APPEND underflow"))
				return frames, stack, errs
			}
			val := stack[len(stack)-1]
			listVal := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			
			list, ok := listVal.(List)
			if !ok {
				errs = append(errs, fmt.Errorf("VM: LIST_APPEND requires a list"))
				return frames, stack, errs
			}
			// In-place append
			list.Elements = append(list.Elements, val)
			stack = append(stack, list)

		case OP_BUILTIN:
			idx := inst.A
			if idx < 0 || idx >= len(builtins) {
				errs = append(errs, fmt.Errorf("VM: invalid builtin index: %d", idx))
				return frames, stack, errs
			}
			arity := builtinArities[idx]
			if len(stack) < arity {
				errs = append(errs, fmt.Errorf("VM: BUILTIN underflow"))
				return frames, stack, errs
			}
			args := make([]Value, arity)
			copy(args, stack[len(stack)-arity:])
			stack = stack[:len(stack)-arity]

			ret, err := builtins[idx](args, out)
			if err != nil {
				errs = append(errs, err)
			}
			stack = append(stack, ret)

		case OP_PRINT:
			if len(stack) == 0 {
				errs = append(errs, fmt.Errorf("VM: PRINT underflow"))
				return frames, stack, errs
			}
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			fmt.Fprintln(out, v.String())

		case OP_HALT:
			return frames, stack, errs

		case OP_LOAD_INDEX:
			if len(stack) < 2 {
				errs = append(errs, fmt.Errorf("VM: LOAD_INDEX underflow"))
				return frames, stack, errs
			}
			indexVal := stack[len(stack)-1]
			targetVal := stack[len(stack)-2]
			stack = stack[:len(stack)-2]

			idx, ok := indexVal.(Int)
			if !ok {
				errs = append(errs, fmt.Errorf("VM: index must be an integer"))
				return frames, stack, errs
			}

			if list, ok := targetVal.(List); ok {
				if idx.V < 0 || idx.V >= int64(len(list.Elements)) {
					errs = append(errs, fmt.Errorf("VM: list index out of bounds"))
					return frames, stack, errs
				}
				stack = append(stack, list.Elements[idx.V])
			} else {
				errs = append(errs, fmt.Errorf("VM: target does not support indexing"))
				return frames, stack, errs
			}
		
		case OP_STORE_INDEX:
			if len(stack) < 3 {
				errs = append(errs, fmt.Errorf("VM: STORE_INDEX underflow"))
				return frames, stack, errs
			}
			val := stack[len(stack)-1]
			indexVal := stack[len(stack)-2]
			targetVal := stack[len(stack)-3]
			stack = stack[:len(stack)-3]

			idx, ok := indexVal.(Int)
			if !ok {
				errs = append(errs, fmt.Errorf("VM: index must be an integer"))
				return frames, stack, errs
			}
			if list, ok := targetVal.(List); ok {
				if idx.V < 0 || idx.V >= int64(len(list.Elements)) {
					errs = append(errs, fmt.Errorf("VM: list index out of bounds"))
					return frames, stack, errs
				}
				list.Elements[idx.V] = val
				stack = append(stack, list)
			} else {
				errs = append(errs, fmt.Errorf("VM: target does not support indexing"))
				return frames, stack, errs
			}

		case OP_MAKE_STRUCT:
			count := inst.A
			if len(stack) < count*2 + 1 {
				errs = append(errs, fmt.Errorf("VM: MAKE_STRUCT underflow"))
				return frames, stack, errs
			}
			typeNameVal := stack[len(stack)-1]
			typeName, ok := typeNameVal.(String)
			if !ok {
				errs = append(errs, fmt.Errorf("VM: struct type name must be a string"))
				return frames, stack, errs
			}
			stack = stack[:len(stack)-1]

			fields := make(map[string]Value)
			for i := 0; i < count; i++ {
				val := stack[len(stack)-1]
				keyVal := stack[len(stack)-2]
				stack = stack[:len(stack)-2]

				keyStr, ok := keyVal.(String)
				if !ok {
					errs = append(errs, fmt.Errorf("VM: struct field name must be a string"))
					return frames, stack, errs
				}
				fields[keyStr.V] = val
			}
			stack = append(stack, Struct{TypeName: typeName.V, Fields: fields})

		case OP_LOAD_FIELD:
			if len(stack) < 1 {
				errs = append(errs, fmt.Errorf("VM: LOAD_FIELD underflow"))
				return frames, stack, errs
			}
			targetVal := stack[len(stack)-1]
			stack = stack[:len(stack)-1]

			nameVal := frame.chunk.Consts[inst.A]
			nameStr, ok := nameVal.(String)
			if !ok {
				errs = append(errs, fmt.Errorf("VM: LOAD_FIELD operand must be a string"))
				return frames, stack, errs
			}

			if s, ok := targetVal.(Struct); ok {
				val, exists := s.Fields[nameStr.V]
				if !exists {
					errs = append(errs, fmt.Errorf("VM: undefined field '%s' on struct '%s'", nameStr.V, s.TypeName))
					return frames, stack, errs
				}
				stack = append(stack, val)
			} else {
				errs = append(errs, fmt.Errorf("VM: target does not support field access"))
				return frames, stack, errs
			}

		case OP_SET_FIELD:
			if len(stack) < 2 {
				errs = append(errs, fmt.Errorf("VM: SET_FIELD underflow"))
				return frames, stack, errs
			}
			val := stack[len(stack)-1]
			targetVal := stack[len(stack)-2]
			stack = stack[:len(stack)-2]

			nameVal := frame.chunk.Consts[inst.A]
			nameStr, ok := nameVal.(String)
			if !ok {
				errs = append(errs, fmt.Errorf("VM: SET_FIELD operand must be a string"))
				return frames, stack, errs
			}

			if s, ok := targetVal.(Struct); ok {
				// We mutate the map in place. Struct contains a map reference.
				s.Fields[nameStr.V] = val
				// Push the struct back for assign expressions
				stack = append(stack, s)
			} else {
				errs = append(errs, fmt.Errorf("VM: target does not support field assignment"))
				return frames, stack, errs
			}
		
		case OP_MAKE_STREAM:
			if len(stack) == 0 {
				errs = append(errs, fmt.Errorf("VM: MAKE_STREAM underflow"))
				return frames, stack, errs
			}
			listVal := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			list, ok := listVal.(List)
			if !ok {
				errs = append(errs, fmt.Errorf("VM: MAKE_STREAM requires a list, got %T", listVal))
				return frames, stack, errs
			}
			streamVal := hostStreamFromList(list)
			stack = append(stack, streamVal)

		case OP_STREAM_FILTER:
			if len(stack) < 2 {
				errs = append(errs, fmt.Errorf("VM: STREAM_FILTER underflow"))
				return frames, stack, errs
			}
			pred := stack[len(stack)-1].(Closure)
			streamVal := stack[len(stack)-2].(Stream)
			stack = stack[:len(stack)-2]
			newStream := hostStreamFilter(streamVal, pred, frames, out)
			stack = append(stack, newStream)

		case OP_STREAM_MAP:
			if len(stack) < 2 {
				errs = append(errs, fmt.Errorf("VM: STREAM_MAP underflow"))
				return frames, stack, errs
			}
			f := stack[len(stack)-1].(Closure)
			streamVal := stack[len(stack)-2].(Stream)
			stack = stack[:len(stack)-2]
			newStream := hostStreamMap(streamVal, f, frames, out)
			stack = append(stack, newStream)

		case OP_STREAM_COLLECT:
			if len(stack) == 0 {
				errs = append(errs, fmt.Errorf("VM: STREAM_COLLECT underflow"))
				return frames, stack, errs
			}
			streamVal := stack[len(stack)-1].(Stream)
			stack = stack[:len(stack)-1]
			list := hostStreamCollect(streamVal, frames, out)
			stack = append(stack, list)

		case OP_STREAM_FOREACH:
			if len(stack) < 2 {
				errs = append(errs, fmt.Errorf("VM: STREAM_FOREACH underflow"))
				return frames, stack, errs
			}
			body := stack[len(stack)-1].(Closure)
			streamVal := stack[len(stack)-2].(Stream)
			stack = stack[:len(stack)-2]
			hostStreamForEach(streamVal, body, frames, out)
			stack = append(stack, Unit{})

		case OP_STREAM_TAKE:
			if len(stack) < 2 {
				errs = append(errs, fmt.Errorf("VM: STREAM_TAKE underflow"))
				return frames, stack, errs
			}
			n := stack[len(stack)-1].(Int)
			streamVal := stack[len(stack)-2].(Stream)
			stack = stack[:len(stack)-2]
			newStream := hostStreamTake(streamVal, n.V)
			stack = append(stack, newStream)

		case OP_STREAM_SKIP:
			if len(stack) < 2 {
				errs = append(errs, fmt.Errorf("VM: STREAM_SKIP underflow"))
				return frames, stack, errs
			}
			n := stack[len(stack)-1].(Int)
			streamVal := stack[len(stack)-2].(Stream)
			stack = stack[:len(stack)-2]
			newStream := hostStreamSkip(streamVal, n.V)
			stack = append(stack, newStream)

		case OP_STREAM_FOLD:
			if len(stack) < 3 {
				errs = append(errs, fmt.Errorf("VM: STREAM_FOLD underflow"))
				return frames, stack, errs
			}
			f := stack[len(stack)-1].(Closure)
			initial := stack[len(stack)-2]
			streamVal := stack[len(stack)-3].(Stream)
			stack = stack[:len(stack)-3]
			result := hostStreamFold(streamVal, initial, f, frames, out)
			stack = append(stack, result)

		default:
			errs = append(errs, fmt.Errorf("VM: unknown opcode: %v", inst.Op))
			return frames, stack, errs
		}
	}
	return frames, stack, errs
}

// doBinaryOp pops two values, applies the operator, and pushes
// the result. Errors are appended to the errs slice and a
// null value is pushed to keep the stack consistent.
//
// This is factored out from the main switch because every
// arithmetic and comparison op follows the same shape.
func doBinaryOp(stack *[]Value, op string, errs []error) []error {
	if len(*stack) < 2 {
		errs = append(errs, fmt.Errorf("VM: binary op %s underflow (stack has %d)", op, len(*stack)))
		return errs
	}
	b := (*stack)[len(*stack)-1]
	a := (*stack)[len(*stack)-2]
	*stack = (*stack)[:len(*stack)-2]
	result, err := applyOp(a, b, op)
	if err != nil {
		errs = append(errs, err)
		*stack = append(*stack, Unit{}) // push a placeholder
		return errs
	}
	*stack = append(*stack, result)
	return errs
}

// applyOp computes a op b and returns the result. The dispatch
// is on the runtime types of a and b; this is the same logic
// the tree-walker uses, but inlined here for the VM.
//
// We don't import the tree-walker's value types — the VM has
// its own. The logic is duplicated, which is annoying but
// keeps the execution paths independent.
func applyOp(a, b Value, op string) (Value, error) {
	// Numeric: int+int, float+float, int+float all supported.
	if ai, aIsInt := a.(Int); aIsInt {
		if bi, bIsInt := b.(Int); bIsInt {
			return applyIntOp(ai.V, bi.V, op)
		}
		if bf, bIsFloat := b.(Float); bIsFloat {
			return applyFloatOp(float64(ai.V), bf.V, op)
		}
	}
	if af, aIsFloat := a.(Float); aIsFloat {
		if bf, bIsFloat := b.(Float); bIsFloat {
			return applyFloatOp(af.V, bf.V, op)
		}
		if bi, bIsInt := b.(Int); bIsInt {
			return applyFloatOp(af.V, float64(bi.V), op)
		}
	}
	// String: only + (concat) and ==/!=.
	if as, aIsStr := a.(String); aIsStr {
		if bs, bIsStr := b.(String); bIsStr {
			return applyStringOp(as.V, bs.V, op)
		}
	}
	// Bool: only == and !=.
	if ab, aIsBool := a.(Bool); aIsBool {
		if bb, bIsBool := b.(Bool); bIsBool {
			return applyBoolOp(ab.V, bb.V, op)
		}
	}
	// List: only + (concat) and ==/!=.
	if al, aIsList := a.(List); aIsList {
		if bl, bIsList := b.(List); bIsList {
			return applyListOp(al, bl, op)
		}
	}
	return Unit{}, fmt.Errorf("VM: cannot apply %q to %s and %s", op, a.String(), b.String())
}

func applyIntOp(a, b int64, op string) (Value, error) {
	switch op {
	case "+":
		return Int{V: a + b}, nil
	case "-":
		return Int{V: a - b}, nil
	case "*":
		return Int{V: a * b}, nil
	case "/":
		if b == 0 {
			return Unit{}, fmt.Errorf("VM: integer divide by zero")
		}
		return Int{V: a / b}, nil
	case "%":
		if b == 0 {
			return Unit{}, fmt.Errorf("VM: integer modulo by zero")
		}
		return Int{V: a % b}, nil
	case "==":
		return Bool{V: a == b}, nil
	case "!=":
		return Bool{V: a != b}, nil
	case "<":
		return Bool{V: a < b}, nil
	case "<=":
		return Bool{V: a <= b}, nil
	case ">":
		return Bool{V: a > b}, nil
	case ">=":
		return Bool{V: a >= b}, nil
	}
	return Unit{}, fmt.Errorf("VM: unsupported int op: %s", op)
}

func applyFloatOp(a, b float64, op string) (Value, error) {
	switch op {
	case "+":
		return Float{V: a + b}, nil
	case "-":
		return Float{V: a - b}, nil
	case "*":
		return Float{V: a * b}, nil
	case "/":
		if b == 0 {
			return Unit{}, fmt.Errorf("VM: float divide by zero")
		}
		return Float{V: a / b}, nil
	case "%":
		return Unit{}, fmt.Errorf("VM: float modulo is unsupported")
	case "==":
		return Bool{V: a == b}, nil
	case "!=":
		return Bool{V: a != b}, nil
	case "<":
		return Bool{V: a < b}, nil
	case "<=":
		return Bool{V: a <= b}, nil
	case ">":
		return Bool{V: a > b}, nil
	case ">=":
		return Bool{V: a >= b}, nil
	}
	return Unit{}, fmt.Errorf("VM: unsupported float op: %s", op)
}

func applyStringOp(a, b string, op string) (Value, error) {
	switch op {
	case "+":
		return String{V: a + b}, nil
	case "==":
		return Bool{V: a == b}, nil
	case "!=":
		return Bool{V: a != b}, nil
	}
	return Unit{}, fmt.Errorf("VM: unsupported string op: %s", op)
}

func applyBoolOp(a, b bool, op string) (Value, error) {
	switch op {
	case "==":
		return Bool{V: a == b}, nil
	case "!=":
		return Bool{V: a != b}, nil
	}
	return Unit{}, fmt.Errorf("VM: unsupported bool op: %s", op)
}

func applyListOp(a, b List, op string) (Value, error) {
	switch op {
	case "+":
		out := make([]Value, 0, len(a.Elements)+len(b.Elements))
		out = append(out, a.Elements...)
		out = append(out, b.Elements...)
		return List{Elements: out}, nil
	case "==":
		if len(a.Elements) != len(b.Elements) {
			return Bool{V: false}, nil
		}
		for i := range a.Elements {
			if a.Elements[i].String() != b.Elements[i].String() {
				return Bool{V: false}, nil
			}
		}
		return Bool{V: true}, nil
	case "!=":
		eq, _ := applyListOp(a, b, "==")
		if bv, ok := eq.(Bool); ok {
			return Bool{V: !bv.V}, nil
		}
	}
	return Unit{}, fmt.Errorf("VM: unsupported list op: %s", op)
}

// ----------------------------------------------------------------------------
// High-level API and Builtins
// ----------------------------------------------------------------------------

// RunSource is the high-level entry point: lex, parse, compile,
// and run a source string. Returns any errors from any stage.
func RunSource(src string, out io.Writer) []error {
	// To avoid circular dependency with interp/parser, we need to import lexer and parser.
	// We'll import them locally or rely on the caller for this.
	// Actually, this requires importing lexer and parser. Let's do that at the top level
	// but wait, to avoid cycle, maybe we shouldn't import it if not needed, but main.go needs it.
	// It's cleaner to let main.go do the lex/parse, but txt14 specifies adding RunSource here.
	// I'll add the implementation that requires imports.
	return runSourceImpl(src, out)
}

type builtinFunc func(args []Value, out io.Writer) (Value, error)

var builtins = []builtinFunc{
	builtinLen,
	builtinToString,
	builtinContains,
	builtinReadFile,
	builtinWriteFile,
	builtinLines,
	builtinSplit,
	builtinTrim,
	builtinReplace,
	builtinToLower,
	builtinToUpper,
	builtinAbs,
	builtinMax,
	builtinMin,
	builtinParseInt,
	builtinTypeOf,
	builtinPush,
	builtinPop,
}

var builtinArities = []int{1, 1, 2, 1, 2, 1, 2, 1, 3, 1, 1, 1, 2, 2, 1, 1, 2, 1}

func builtinLen(args []Value, out io.Writer) (Value, error) {
	switch v := args[0].(type) {
	case String:
		return Int{V: int64(len(v.V))}, nil
	case List:
		return Int{V: int64(len(v.Elements))}, nil
	}
	return Unit{}, fmt.Errorf("VM: len() requires a string or list")
}

func builtinToString(args []Value, out io.Writer) (Value, error) {
	return String{V: args[0].String()}, nil
}

func builtinContains(args []Value, out io.Writer) (Value, error) {
	haystack, ok := args[0].(String)
	if !ok {
		return Unit{}, fmt.Errorf("VM: contains() requires a string")
	}
	needle, ok := args[1].(String)
	if !ok {
		return Unit{}, fmt.Errorf("VM: contains() requires a string")
	}
	return Bool{V: strings.Contains(haystack.V, needle.V)}, nil
}

func builtinReadFile(args []Value, out io.Writer) (Value, error) {
	path, ok := args[0].(String)
	if !ok {
		return Unit{}, fmt.Errorf("VM: read_file requires a string path")
	}
	content, err := os.ReadFile(path.V)
	if err != nil {
		return Unit{}, fmt.Errorf("VM: read_file error: %v", err)
	}
	return String{V: string(content)}, nil
}

func builtinWriteFile(args []Value, out io.Writer) (Value, error) {
	path, ok := args[0].(String)
	if !ok {
		return Unit{}, fmt.Errorf("VM: write_file requires a string path")
	}
	content, ok := args[1].(String)
	if !ok {
		return Unit{}, fmt.Errorf("VM: write_file requires a string content")
	}
	err := os.WriteFile(path.V, []byte(content.V), 0644)
	if err != nil {
		return Unit{}, fmt.Errorf("VM: write_file error: %v", err)
	}
	return Unit{}, nil
}

func builtinLines(args []Value, out io.Writer) (Value, error) {
	s, ok := args[0].(String)
	if !ok {
		return Unit{}, fmt.Errorf("VM: lines requires a string")
	}
	// Split by newline and remove carriage returns if any
	lines := strings.Split(s.V, "\n")
	list := make([]Value, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		list = append(list, String{V: line})
	}
	return List{Elements: list}, nil
}

func isTruthy(v Value) bool {
	switch val := v.(type) {
	case Bool:
		return val.V
	case Int:
		return val.V != 0
	case Float:
		return val.V != 0
	case String:
		return val.V != ""
	case List:
		return len(val.Elements) > 0
	case Unit:
		return false
	default:
		return true // Closures etc. are true
	}
}

func opToken(op Opcode) string {
	switch op {
	case OP_ADD: return "+"
	case OP_SUB: return "-"
	case OP_MUL: return "*"
	case OP_DIV: return "/"
	case OP_MOD: return "%"
	case OP_EQ: return "=="
	case OP_NEQ: return "!="
	case OP_LT: return "<"
	case OP_LTE: return "<="
	case OP_GT: return ">"
	case OP_GTE: return ">="
	}
	return ""
}

// ----------------------------------------------------------------------------
// Stream Host Functions
// ----------------------------------------------------------------------------

func hostStreamFromList(l List) Stream {
	i := 0
	return Stream{
		Next: func() (Value, bool) {
			if i < len(l.Elements) {
				v := l.Elements[i]
				i++
				return v, true
			}
			return Unit{}, false
		},
	}
}

func callClosure(c Closure, args []Value, frames []*Frame, out io.Writer) Value {
	initialDepth := len(frames)
	callFrame := newFrame(c.Code)
	for i, arg := range args {
		callFrame.locals[i] = arg
	}
	for i, fv := range c.Free {
		callFrame.locals[len(c.Code.Locals)+i] = fv
	}
	callFrame.freeStart = len(c.Code.Locals)
	callFrame.numFree = len(c.Free)
	frames = append(frames, callFrame)

	// Since we are recursing, stack starts fresh
	var stack []Value
	// Run until the new frame returns
	_, stack, _ = runUntilReturn(frames, stack, out, initialDepth)
	
	if len(stack) > 0 {
		return stack[len(stack)-1]
	}
	return Unit{}
}

func hostStreamFilter(s Stream, pred Closure, frames []*Frame, out io.Writer) Stream {
	return Stream{
		Next: func() (Value, bool) {
			for {
				v, ok := s.Next()
				if !ok {
					return Unit{}, false
				}
				res := callClosure(pred, []Value{v}, frames, out)
				if isTruthy(res) {
					return v, true
				}
			}
		},
	}
}

func hostStreamMap(s Stream, f Closure, frames []*Frame, out io.Writer) Stream {
	return Stream{
		Next: func() (Value, bool) {
			v, ok := s.Next()
			if !ok {
				return Unit{}, false
			}
			res := callClosure(f, []Value{v}, frames, out)
			return res, true
		},
	}
}

func hostStreamCollect(s Stream, frames []*Frame, out io.Writer) List {
	var elements []Value
	for {
		v, ok := s.Next()
		if !ok {
			break
		}
		elements = append(elements, v)
	}
	return List{Elements: elements}
}

func builtinSplit(args []Value, out io.Writer) (Value, error) {
	str, ok := args[0].(String)
	if !ok { return Unit{}, fmt.Errorf("VM: split() requires a string") }
	sep, ok := args[1].(String)
	if !ok { return Unit{}, fmt.Errorf("VM: split() requires a string separator") }
	parts := strings.Split(str.V, sep.V)
	elems := make([]Value, len(parts))
	for i, p := range parts {
		elems[i] = String{V: p}
	}
	return List{Elements: elems}, nil
}

func builtinTrim(args []Value, out io.Writer) (Value, error) {
	str, ok := args[0].(String)
	if !ok { return Unit{}, fmt.Errorf("VM: trim() requires a string") }
	return String{V: strings.TrimSpace(str.V)}, nil
}

func builtinReplace(args []Value, out io.Writer) (Value, error) {
	str, ok := args[0].(String)
	if !ok { return Unit{}, fmt.Errorf("VM: replace() requires a string") }
	oldStr, ok := args[1].(String)
	if !ok { return Unit{}, fmt.Errorf("VM: replace() requires a string old") }
	newStr, ok := args[2].(String)
	if !ok { return Unit{}, fmt.Errorf("VM: replace() requires a string new") }
	return String{V: strings.ReplaceAll(str.V, oldStr.V, newStr.V)}, nil
}

func builtinToLower(args []Value, out io.Writer) (Value, error) {
	str, ok := args[0].(String)
	if !ok { return Unit{}, fmt.Errorf("VM: to_lower() requires a string") }
	return String{V: strings.ToLower(str.V)}, nil
}

func builtinToUpper(args []Value, out io.Writer) (Value, error) {
	str, ok := args[0].(String)
	if !ok { return Unit{}, fmt.Errorf("VM: to_upper() requires a string") }
	return String{V: strings.ToUpper(str.V)}, nil
}

func builtinAbs(args []Value, out io.Writer) (Value, error) {
	switch v := args[0].(type) {
	case Int:
		if v.V < 0 { return Int{V: -v.V}, nil }
		return v, nil
	case Float:
		return Float{V: math.Abs(v.V)}, nil
	default:
		return Unit{}, fmt.Errorf("VM: abs() requires int or float")
	}
}

func builtinMax(args []Value, out io.Writer) (Value, error) {
	switch v1 := args[0].(type) {
	case Int:
		if v2, ok := args[1].(Int); ok {
			if v1.V > v2.V { return v1, nil }
			return v2, nil
		}
	case Float:
		if v2, ok := args[1].(Float); ok {
			return Float{V: math.Max(v1.V, v2.V)}, nil
		}
	}
	return Unit{}, fmt.Errorf("VM: max() requires two ints or two floats")
}

func builtinMin(args []Value, out io.Writer) (Value, error) {
	switch v1 := args[0].(type) {
	case Int:
		if v2, ok := args[1].(Int); ok {
			if v1.V < v2.V { return v1, nil }
			return v2, nil
		}
	case Float:
		if v2, ok := args[1].(Float); ok {
			return Float{V: math.Min(v1.V, v2.V)}, nil
		}
	}
	return Unit{}, fmt.Errorf("VM: min() requires two ints or two floats")
}

func builtinParseInt(args []Value, out io.Writer) (Value, error) {
	str, ok := args[0].(String)
	if !ok { return Unit{}, fmt.Errorf("VM: parse_int() requires a string") }
	val, err := strconv.ParseInt(strings.TrimSpace(str.V), 10, 64)
	if err != nil { return Unit{}, fmt.Errorf("VM: parse_int() error: %v", err) }
	return Int{V: val}, nil
}

func builtinTypeOf(args []Value, out io.Writer) (Value, error) {
	switch args[0].(type) {
	case Int: return String{V: "int"}, nil
	case Float: return String{V: "float"}, nil
	case String: return String{V: "string"}, nil
	case Bool: return String{V: "bool"}, nil
	case Unit: return String{V: "unit"}, nil
	case List: return String{V: "list"}, nil
	case Struct: return String{V: "struct"}, nil
	case Closure: return String{V: "fn"}, nil
	case Stream: return String{V: "stream"}, nil
	default: return String{V: "unknown"}, nil
	}
}

func builtinPush(args []Value, out io.Writer) (Value, error) {
	list, ok := args[0].(List)
	if !ok { return Unit{}, fmt.Errorf("VM: push() requires a list") }
	newList := make([]Value, len(list.Elements)+1)
	copy(newList, list.Elements)
	newList[len(list.Elements)] = args[1]
	return List{Elements: newList}, nil
}

func builtinPop(args []Value, out io.Writer) (Value, error) {
	list, ok := args[0].(List)
	if !ok { return Unit{}, fmt.Errorf("VM: pop() requires a list") }
	if len(list.Elements) == 0 { return Unit{}, fmt.Errorf("VM: pop() on empty list") }
	newList := make([]Value, len(list.Elements)-1)
	copy(newList, list.Elements[:len(list.Elements)-1])
	return List{Elements: newList}, nil
}

func hostStreamForEach(s Stream, f Closure, frames []*Frame, out io.Writer) {
	for {
		v, ok := s.Next()
		if !ok {
			break
		}
		callClosure(f, []Value{v}, frames, out)
	}
}

func hostStreamTake(s Stream, n int64) Stream {
	count := int64(0)
	return Stream{
		Next: func() (Value, bool) {
			if count >= n {
				return Unit{}, false
			}
			v, ok := s.Next()
			if !ok {
				return Unit{}, false
			}
			count++
			return v, true
		},
	}
}

func hostStreamSkip(s Stream, n int64) Stream {
	skipped := false
	return Stream{
		Next: func() (Value, bool) {
			if !skipped {
				for i := int64(0); i < n; i++ {
					_, ok := s.Next()
					if !ok {
						return Unit{}, false
					}
				}
				skipped = true
			}
			return s.Next()
		},
	}
}

func hostStreamFold(s Stream, initial Value, f Closure, frames []*Frame, out io.Writer) Value {
	acc := initial
	for {
		v, ok := s.Next()
		if !ok {
			break
		}
		acc = callClosure(f, []Value{acc, v}, frames, out)
	}
	return acc
}
