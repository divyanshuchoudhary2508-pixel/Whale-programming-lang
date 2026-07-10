package vm

// Opcode identifies a single VM instruction. The opcode is the
// unit of dispatch: the inner loop reads one Opcode, dispatches
// to a handler, and the handler reads any operands it needs.
type Opcode int

const (
	// Stack manipulation.
	OP_PUSH_CONST Opcode = iota
	OP_POP

	// Locals (variable bindings). Locals are addressed by
	// index; the Chunk's Locals slice maps index to name.
	OP_LOAD
	OP_STORE
	OP_LOAD_INDEX
	OP_STORE_INDEX

	// Arithmetic. Pop two, push one.
	OP_ADD
	OP_SUB
	OP_MUL
	OP_DIV
	OP_MOD

	// Comparison. Pop two, push Bool.
	OP_EQ
	OP_NEQ
	OP_LT
	OP_LTE
	OP_GT
	OP_GTE

	// Control flow.
	OP_JUMP
	OP_JUMP_IF_FALSE
	OP_LOOP
	OP_CALL
	OP_RETURN
	OP_MAKE_FN
	OP_MAKE_CLOSURE
	OP_LOAD_FREE

	// Streams
	OP_MAKE_STREAM
	OP_STREAM_FILTER
	OP_STREAM_MAP
	OP_STREAM_COLLECT
	OP_STREAM_FOREACH
	OP_STREAM_TAKE
	OP_STREAM_SKIP
	OP_STREAM_FOLD

	// Compound values.
	OP_MAKE_LIST
	OP_LIST_APPEND
	OP_MAKE_STRUCT
	OP_LOAD_FIELD
	OP_SET_FIELD

	// Builtins (print is separate).
	OP_BUILTIN

	// Output. Pop one, write to stdout.
	OP_PRINT

	// Termination. Stop the VM.
	OP_HALT
)

func (o Opcode) String() string {
	switch o {
	case OP_PUSH_CONST:
		return "PUSH_CONST"
	case OP_POP:
		return "POP"
	case OP_LOAD:
		return "LOAD"
	case OP_STORE:
		return "STORE"
	case OP_LOAD_INDEX:
		return "LOAD_INDEX"
	case OP_STORE_INDEX:
		return "STORE_INDEX"
	case OP_ADD:
		return "ADD"
	case OP_SUB:
		return "SUB"
	case OP_MUL:
		return "MUL"
	case OP_DIV:
		return "DIV"
	case OP_MOD:
		return "MOD"
	case OP_EQ:
		return "EQ"
	case OP_NEQ:
		return "NEQ"
	case OP_LT:
		return "LT"
	case OP_LTE:
		return "LTE"
	case OP_GT:
		return "GT"
	case OP_GTE:
		return "GTE"
	case OP_JUMP:
		return "JUMP"
	case OP_JUMP_IF_FALSE:
		return "JUMP_IF_FALSE"
	case OP_LOOP:
		return "LOOP"
	case OP_CALL:
		return "CALL"
	case OP_RETURN:
		return "RETURN"
	case OP_MAKE_FN:
		return "MAKE_FN"
	case OP_MAKE_CLOSURE:
		return "MAKE_CLOSURE"
	case OP_LOAD_FREE:
		return "LOAD_FREE"
	case OP_MAKE_LIST:
		return "MAKE_LIST"
	case OP_LIST_APPEND:
		return "LIST_APPEND"
	case OP_MAKE_STRUCT:
		return "MAKE_STRUCT"
	case OP_LOAD_FIELD:
		return "LOAD_FIELD"
	case OP_SET_FIELD:
		return "SET_FIELD"
	case OP_BUILTIN:
		return "BUILTIN"
	case OP_PRINT:
		return "PRINT"
	case OP_HALT:
		return "HALT"
	case OP_MAKE_STREAM:
		return "MAKE_STREAM"
	case OP_STREAM_FILTER:
		return "STREAM_FILTER"
	case OP_STREAM_MAP:
		return "STREAM_MAP"
	case OP_STREAM_COLLECT:
		return "STREAM_COLLECT"
	case OP_STREAM_FOREACH:
		return "STREAM_FOREACH"
	case OP_STREAM_TAKE:
		return "STREAM_TAKE"
	case OP_STREAM_SKIP:
		return "STREAM_SKIP"
	case OP_STREAM_FOLD:
		return "STREAM_FOLD"
	}
	return "UNKNOWN"
}
