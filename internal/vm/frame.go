package vm

// Frame represents one active function call. The VM maintains
// a stack of frames; the top frame is the one currently
// executing. Each frame has its own program counter, its own
// locals, and a reference to the chunk being executed.
type Frame struct {
	chunk  *Chunk
	pc     int    // program counter within chunk
	locals []Value // this call's local variables
	// freeStart is the slot index where free variables begin
	// in locals. Slots 0..arity-1 are parameters; slots
	// arity..freeStart-1 are the function's own locals; slots
	// freeStart..freeStart+numFree-1 are captured free
	// variables from the enclosing closure.
	freeStart int
	numFree   int
}

// newFrame creates a frame for the given chunk. The locals
// slice is pre-allocated to the chunk's full locals size
// (parameters + own locals + free variables).
func newFrame(chunk *Chunk) *Frame {
	// The locals size is the chunk's Locals count, but we
	// need to account for free variables which aren't in
	// chunk.Locals.
	size := len(chunk.Locals) + len(chunk.Free)
	locals := make([]Value, size)
	return &Frame{
		chunk:  chunk,
		pc:     0,
		locals: locals,
	}
}
