// streams.go — stream value type and helper functions for the Whale interpreter.
//
// This file exists to keep stream-specific types organized. The actual builtin
// dispatch (callFilter, callMap, etc.) lives in eval.go.
//
// A stream is a lazy, pull-based sequence. Sources create streams from data.
// Transforms take a stream and return a new stream. Sinks pull values and
// produce a final result.
//
// The pipe operator |> desugars to function calls at parse time:
//
//	[1,2,3] |> filter(n => n > 1)  =>  filter(stream([1,2,3]), n => n > 1)
//	s |> map(f)                     =>  map(s, f)
//	s |> collect                    =>  collect(s)
package interp
