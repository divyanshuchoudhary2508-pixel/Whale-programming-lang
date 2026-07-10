// Package vm implements the bytecode virtual machine for Whale v0.1.
//
// The VM is a stack-based interpreter for compiled bytecode. It is
// the "fast iteration" execution path between the tree-walking
// interpreter (internal/interp) and the future LLVM backend
// (Phase 7+). The VM is reachable via `wh run --vm`.
//
// Commit 1 of the VM implements only the straight-line subset:
// literals, arithmetic, local variables, and print. No branches,
// no function calls, no closures, no streams. These come in
// later commits.
package vm

import (
	"strconv"
	"strings"
)

// Value is the runtime value type. The VM stack holds Values;
// locals and the constant pool also hold Values.
type Value interface {
	valueMarker()
	String() string
}

// Concrete value types. These mirror the tree-walker's value
// types but are independent. The two execution paths do not
// share runtime state; a value produced by the tree-walker
// cannot be consumed by the VM and vice versa.
type Int struct{ V int64 }
type Float struct{ V float64 }
type String struct{ V string }
type Bool struct{ V bool }
type Unit struct{}
type List struct{ Elements []Value }
type Struct struct {
	TypeName string
	Fields   map[string]Value
}

func (Int) valueMarker()    {}
func (Float) valueMarker()  {}
func (String) valueMarker() {}
func (Bool) valueMarker()   {}
func (Unit) valueMarker()   {}
func (List) valueMarker()   {}
func (Struct) valueMarker() {}

func (i Int) String() string    { return strconv.FormatInt(i.V, 10) }
func (f Float) String() string  { return formatFloat(f.V) }
func (s String) String() string { return s.V }
func (b Bool) String() string {
	if b.V {
		return "true"
	}
	return "false"
}

// Closure is a compiled function value with captured state.
// A closure is what you get when you create a lambda or
// reference a nested function. Top-level functions are also
// represented as Closures, but with no free variables.
type Closure struct {
	Name  string
	Arity int
	Code  *Chunk
	Free  []Value // captured values from the enclosing scope
}

func (Closure) valueMarker() {}
func (c Closure) String() string {
	if c.Name != "" {
		return "<fn " + c.Name + ">"
	}
	return "<fn>"
}

func (Unit) String() string     { return "()" }

func (l List) String() string {
	parts := make([]string, len(l.Elements))
	for i, e := range l.Elements {
		parts[i] = e.String()
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func (s Struct) String() string {
	out := s.TypeName + " { "
	first := true
	for k, v := range s.Fields {
		if !first {
			out += ", "
		}
		first = false
		out += k + ": " + v.String()
	}
	out += " }"
	return out
}

func formatFloat(f float64) string {
	out := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(out, ".eE") {
		out += ".0"
	}
	return out
}

// Stream represents a lazy stream of values.
type Stream struct {
	Next func() (Value, bool)
}

func (Stream) valueMarker() {}
func (s Stream) String() string {
	return "<stream>"
}
