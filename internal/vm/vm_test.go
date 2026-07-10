package vm

import (
	"bytes"
	"strings"
	"testing"
)

// runChunk is a test helper: build a chunk, run it, return
// the output and any errors. The chunk is built by a closure
// passed in so each test can construct its own instruction
// sequence.
func runChunk(t *testing.T, build func(c *Chunk)) (string, []error) {
	t.Helper()
	chunk := NewChunk("test")
	build(chunk)
	var buf bytes.Buffer
	errs := RunWithOutput(chunk, &buf)
	return buf.String(), errs
}

// TestSimplePrint is the absolute minimum: push a constant,
// print it, halt. If this doesn't work, nothing else will.
func TestSimplePrint(t *testing.T) {
	out, errs := runChunk(t, func(c *Chunk) {
		idx := c.addConst(Int{V: 42})
		c.Emit(OP_PUSH_CONST, idx, 0)
		c.Emit(OP_PRINT, 0, 0)
		c.Emit(OP_HALT, 0, 0)
	})
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if out != "42\n" {
		t.Errorf("output = %q, want '42\\n'", out)
	}
}

// TestArithmetic covers the four basic ops. The program
// computes (3 + 4) * 2 and prints the result.
func TestArithmetic(t *testing.T) {
	out, errs := runChunk(t, func(c *Chunk) {
		c3 := c.addConst(Int{V: 3})
		c4 := c.addConst(Int{V: 4})
		c2 := c.addConst(Int{V: 2})

		// Stack: []
		c.Emit(OP_PUSH_CONST, c3, 0) // [3]
		c.Emit(OP_PUSH_CONST, c4, 0) // [3, 4]
		c.Emit(OP_ADD, 0, 0)        // [7]
		c.Emit(OP_PUSH_CONST, c2, 0) // [7, 2]
		c.Emit(OP_MUL, 0, 0)        // [14]
		c.Emit(OP_PRINT, 0, 0)
		c.Emit(OP_HALT, 0, 0)
	})
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if out != "14\n" {
		t.Errorf("output = %q, want '14\\n'", out)
	}
}

// TestLocals covers STORE and LOAD. The program computes
// 5 + 3, stores it in local 'sum', then prints 'sum'.
func TestLocals(t *testing.T) {
	out, errs := runChunk(t, func(c *Chunk) {
		c5 := c.addConst(Int{V: 5})
		c3 := c.addConst(Int{V: 3})
		sumSlot := c.addLocal("sum")

		// Compute 5 + 3, store in sum.
		c.Emit(OP_PUSH_CONST, c5, 0)
		c.Emit(OP_PUSH_CONST, c3, 0)
		c.Emit(OP_ADD, 0, 0)
		c.Emit(OP_STORE, sumSlot, 0) // stack is empty; locals[0] = 8

		// Print sum.
		c.Emit(OP_LOAD, sumSlot, 0)
		c.Emit(OP_PRINT, 0, 0)
		c.Emit(OP_HALT, 0, 0)
	})
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if out != "8\n" {
		t.Errorf("output = %q, want '8\\n'", out)
	}
}

// TestMultiplePrints covers the stack being used and discarded
// across multiple top-level statements.
func TestMultiplePrints(t *testing.T) {
	out, errs := runChunk(t, func(c *Chunk) {
		c1 := c.addConst(Int{V: 1})
		c2 := c.addConst(Int{V: 2})
		c3 := c.addConst(Int{V: 3})

		c.Emit(OP_PUSH_CONST, c1, 0)
		c.Emit(OP_PRINT, 0, 0)
		c.Emit(OP_PUSH_CONST, c2, 0)
		c.Emit(OP_PRINT, 0, 0)
		c.Emit(OP_PUSH_CONST, c3, 0)
		c.Emit(OP_PRINT, 0, 0)
		c.Emit(OP_HALT, 0, 0)
	})
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if out != "1\n2\n3\n" {
		t.Errorf("output = %q, want '1\\n2\\n3\\n'", out)
	}
}

// TestMixedTypes covers int + float (the int is promoted to
// float, the result is float).
func TestMixedTypes(t *testing.T) {
	out, errs := runChunk(t, func(c *Chunk) {
		i5 := c.addConst(Int{V: 5})
		f2 := c.addConst(Float{V: 2.5})

		c.Emit(OP_PUSH_CONST, i5, 0)
		c.Emit(OP_PUSH_CONST, f2, 0)
		c.Emit(OP_ADD, 0, 0)
		c.Emit(OP_PRINT, 0, 0)
		c.Emit(OP_HALT, 0, 0)
	})
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if !strings.HasPrefix(out, "7.5") {
		t.Errorf("output = %q, want prefix '7.5'", out)
	}
}

// TestStringConcat covers the string + operator.
func TestStringConcat(t *testing.T) {
	out, errs := runChunk(t, func(c *Chunk) {
		hi := c.addConst(String{V: "hello, "})
		name := c.addConst(String{V: "whale"})

		c.Emit(OP_PUSH_CONST, hi, 0)
		c.Emit(OP_PUSH_CONST, name, 0)
		c.Emit(OP_ADD, 0, 0)
		c.Emit(OP_PRINT, 0, 0)
		c.Emit(OP_HALT, 0, 0)
	})
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if out != "hello, whale\n" {
		t.Errorf("output = %q, want 'hello, whale\\n'", out)
	}
}

// TestComparison covers the comparison ops. The program
// computes 5 < 10 and prints the result (true).
func TestComparison(t *testing.T) {
	out, errs := runChunk(t, func(c *Chunk) {
		c5 := c.addConst(Int{V: 5})
		c10 := c.addConst(Int{V: 10})

		c.Emit(OP_PUSH_CONST, c5, 0)
		c.Emit(OP_PUSH_CONST, c10, 0)
		c.Emit(OP_LT, 0, 0)
		c.Emit(OP_PRINT, 0, 0)
		c.Emit(OP_HALT, 0, 0)
	})
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if out != "true\n" {
		t.Errorf("output = %q, want 'true\\n'", out)
	}
}
