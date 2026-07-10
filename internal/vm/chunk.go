package vm

// Instruction is a single VM operation. We represent it as a
// struct of opcode + operands rather than a raw byte slice.
// This is slightly less memory-efficient than packed bytes
// but much easier to read in debug output, and the dispatch
// performance is comparable for our use case.
//
// Operands A and B are signed integers. The compiler picks
// the right size (1 byte for small constants, 4 bytes for
// larger ones) but at runtime we just use int.
type Instruction struct {
	Op Opcode
	A  int
	B  int
}

// Chunk is a compiled unit: a sequence of instructions plus
// the constant pool and the local variable table.
//
// In Commit 1 a Chunk represents the whole program. In later
// commits each function will have its own Chunk; the program
// Chunk will reference function Chunks by index.
type Chunk struct {
	Code   []Instruction
	Consts []Value  // constant pool
	Locals []string      // local variable names (index = slot)
	Free   []string      // free variable names (for closures)
	Name   string
	Arity  int
}

func (*Chunk) valueMarker() {}
func (c *Chunk) String() string {
	return "<chunk " + c.Name + ">"
}

// NewChunk creates an empty chunk with the given name.
func NewChunk(name string) *Chunk {
	return &Chunk{
		Code:   make([]Instruction, 0, 16),
		Consts: make([]Value, 0, 8),
		Locals: make([]string, 0, 4),
		Name:   name,
	}
}

// addConst adds a value to the constant pool and returns its
// index. If the value is already in the pool, returns the
// existing index (constant deduplication).
func (c *Chunk) addConst(v Value) int {
	for i, existing := range c.Consts {
		if sameValue(existing, v) {
			return i
		}
	}
	c.Consts = append(c.Consts, v)
	return len(c.Consts) - 1
}

// addLocal registers a new local variable and returns its
// slot index. Locals are added at compile time; the VM
// uses the slot index to read and write values.
func (c *Chunk) addLocal(name string) int {
	for i, existing := range c.Locals {
		if existing == name {
			return i
		}
	}
	c.Locals = append(c.Locals, name)
	return len(c.Locals) - 1
}

// Emit appends an instruction to the chunk.
func (c *Chunk) Emit(op Opcode, a, b int) {
	c.Code = append(c.Code, Instruction{Op: op, A: a, B: b})
}

// sameValue is a value-equality check for constant deduplication.
// For now we compare strings; the constant pool only holds
// small literals so this is fine.
func sameValue(a, b Value) bool {
	if c1, ok := a.(*Chunk); ok {
		if c2, ok := b.(*Chunk); ok {
			return c1 == c2 // pointer equality
		}
		return false
	}
	return a.String() == b.String()
}
