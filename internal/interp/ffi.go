package interp

import (
	"sync"
)

// SimHeap is our simulated C-FFI heap
var (
	heapMu sync.Mutex
	heap   []byte
	
	// A simple bump allocator for now (we won't implement a full malloc/free algorithm in the simulator)
	// We'll just append and never actually reclaim memory in 'free', just like a basic arena.
	// 0 is reserved as 'NULL'
	heapOffset int64 = 1
)

func init() {
	// Pre-allocate a 10MB heap for the simulator
	heap = make([]byte, 10*1024*1024)
}

// FFIRegistry maps extern function names to Go native implementations
var FFIRegistry = map[string]func(args []Value) Value{
	"memory_malloc":  ffiMalloc,
	"memory_free":    ffiFree,
	"memory_realloc": ffiRealloc,
	"memory_memcpy":  ffiMemcpy,
	"string_strlen":  ffiStrlen,
	"string_strcpy":  ffiStrcpy,
	"string__heap_write_string": ffiHeapWriteString,
	"string__heap_read_string": ffiHeapReadString,
	
	// I/O wrappers can be added later
}

// extern fn malloc(size: int) -> int;
func ffiMalloc(args []Value) Value {
	size := args[0].(intValue).v
	
	heapMu.Lock()
	defer heapMu.Unlock()
	
	if heapOffset+size > int64(len(heap)) {
		// Out of memory (in simulator)
		return intValue{v: 0} // NULL
	}
	
	ptr := heapOffset
	heapOffset += size
	
	return intValue{v: ptr}
}

// extern fn free(ptr: int);
func ffiFree(args []Value) Value {
	// Simulator bump allocator doesn't actually free.
	return nullValue{}
}

// extern fn realloc(ptr: int, size: int) -> int;
func ffiRealloc(args []Value) Value {
	ptr := args[0].(intValue).v
	size := args[1].(intValue).v
	
	if ptr == 0 {
		return ffiMalloc(args[1:])
	}
	
	// Allocate new memory
	newPtrVal := ffiMalloc([]Value{intValue{v: size}})
	newPtr := newPtrVal.(intValue).v
	
	if newPtr == 0 {
		return intValue{v: 0}
	}
	
	// Copy old memory (we don't track chunk sizes, so we just copy 'size' bytes. 
	// This is unsafe in C but okay for our bump simulator where we'll assume they 
	// request >= old_size or we just copy min(old_size, size)).
	// For simplicity, we just copy `size` bytes. 
	heapMu.Lock()
	defer heapMu.Unlock()
	
	copy(heap[newPtr:newPtr+size], heap[ptr:ptr+size])
	
	return intValue{v: newPtr}
}

// extern fn memcpy(dest: int, src: int, n: int) -> int;
func ffiMemcpy(args []Value) Value {
	dest := args[0].(intValue).v
	src := args[1].(intValue).v
	n := args[2].(intValue).v
	
	heapMu.Lock()
	defer heapMu.Unlock()
	
	copy(heap[dest:dest+n], heap[src:src+n])
	
	return intValue{v: dest}
}

// extern fn strlen(ptr: int) -> int;
func ffiStrlen(args []Value) Value {
	ptr := args[0].(intValue).v
	
	heapMu.Lock()
	defer heapMu.Unlock()
	
	var length int64 = 0
	for ptr+length < int64(len(heap)) && heap[ptr+length] != 0 {
		length++
	}
	
	return intValue{v: length}
}

// extern fn strcpy(dest: int, src: int) -> int;
func ffiStrcpy(args []Value) Value {
	dest := args[0].(intValue).v
	src := args[1].(intValue).v
	
	heapMu.Lock()
	defer heapMu.Unlock()
	
	var i int64 = 0
	for src+i < int64(len(heap)) && heap[src+i] != 0 {
		heap[dest+i] = heap[src+i]
		i++
	}
	heap[dest+i] = 0 // null terminator
	
	return intValue{v: dest}
}

// Helper for FFI functions to read strings
func ffiHeapWriteString(args []Value) Value {
	ptr := args[0].(intValue).v
	s := args[1].(stringValue).v
	
	heapMu.Lock()
	defer heapMu.Unlock()
	
	copy(heap[ptr:], []byte(s))
	heap[ptr+int64(len(s))] = 0
	return nullValue{}
}

func ffiHeapReadString(args []Value) Value {
	ptr := args[0].(intValue).v
	
	heapMu.Lock()
	defer heapMu.Unlock()
	
	var length int64 = 0
	for ptr+length < int64(len(heap)) && heap[ptr+length] != 0 {
		length++
	}
	return stringValue{v: string(heap[ptr : ptr+length])}
}
