package interp

import (
	"os"
	"strings"
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

// Native Lists
var (
	listMu sync.Mutex
	nativeLists [][]Value
)

// FFIRegistry maps extern function names to Go native implementations
var FFIRegistry = map[string]func(args []Value) Value{
	"memory_malloc":  ffiMalloc,
	"memory_free":    ffiFree,
	"memory_realloc": ffiRealloc,
	"memory_memcpy":  ffiMemcpy,
	"string_strlen":  ffiStrlen,
	"string_strcpy":  ffiStrcpy,
	"string_split":   ffiStrSplit,
	"string_starts_with": ffiStrStartsWith,
	"string__heap_write_string": ffiHeapWriteString,
	"string__heap_read_string": ffiHeapReadString,
	
	"list_new":       ffiListNew,
	"list_push":      ffiListPush,
	"list_get":       ffiListGet,
	"list_len":       ffiListLen,
	"list_remove":    ffiListRemove,
	
	"read_file":      ffiReadFile,
	"write_file":     ffiWriteFile,
	// I/O wrappers can be added later
}

func ffiReadFile(args []Value) Value {
	filename := args[0].(stringValue).v
	data, err := os.ReadFile(filename)
	if err != nil {
		return enumValue{Variant: "Err", Payload: stringValue{v: err.Error()}}
	}
	return enumValue{Variant: "Ok", Payload: stringValue{v: string(data)}}
}

func ffiWriteFile(args []Value) Value {
	filename := args[0].(stringValue).v
	data := args[1].(stringValue).v
	err := os.WriteFile(filename, []byte(data), 0644)
	if err != nil {
		return enumValue{Variant: "Err", Payload: stringValue{v: err.Error()}}
	}
	return enumValue{Variant: "Ok", Payload: intValue{v: 0}}
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

// extern fn string_split(s: string, delim: string, index: int) -> string;
func ffiStrSplit(args []Value) Value {
	s := args[0].(stringValue).v
	delim := args[1].(stringValue).v
	index := int(args[2].(intValue).v)
	
	parts := strings.Split(s, delim)
	if index >= 0 && index < len(parts) {
		return stringValue{v: parts[index]}
	}
	return stringValue{v: ""}
}

// extern fn string_starts_with(s: string, prefix: string) -> int; // Returns 1 if true, 0 if false
func ffiStrStartsWith(args []Value) Value {
	s := args[0].(stringValue).v
	prefix := args[1].(stringValue).v
	if strings.HasPrefix(s, prefix) {
		return intValue{v: 1}
	}
	return intValue{v: 0}
}

// extern fn list_new() -> int;
func ffiListNew(args []Value) Value {
	listMu.Lock()
	defer listMu.Unlock()
	nativeLists = append(nativeLists, []Value{})
	return intValue{v: int64(len(nativeLists) - 1)}
}

// extern fn list_push(list_id: int, val: any);
func ffiListPush(args []Value) Value {
	idx := args[0].(intValue).v
	val := args[1]
	listMu.Lock()
	defer listMu.Unlock()
	if idx >= 0 && idx < int64(len(nativeLists)) {
		nativeLists[idx] = append(nativeLists[idx], val)
	}
	return nullValue{}
}

// extern fn list_get(list_id: int, index: int) -> any;
func ffiListGet(args []Value) Value {
	idx := args[0].(intValue).v
	elemIdx := args[1].(intValue).v
	listMu.Lock()
	defer listMu.Unlock()
	if idx >= 0 && idx < int64(len(nativeLists)) {
		lst := nativeLists[idx]
		if elemIdx >= 0 && elemIdx < int64(len(lst)) {
			return lst[elemIdx]
		}
	}
	return nullValue{}
}

// extern fn list_len(list_id: int) -> int;
func ffiListLen(args []Value) Value {
	idx := args[0].(intValue).v
	listMu.Lock()
	defer listMu.Unlock()
	if idx >= 0 && idx < int64(len(nativeLists)) {
		return intValue{v: int64(len(nativeLists[idx]))}
	}
	return intValue{v: 0}
}

// extern fn list_remove(list_id: int, index: int) -> int;
func ffiListRemove(args []Value) Value {
	listIdx := args[0].(intValue).v
	elemIdx := args[1].(intValue).v
	
	listMu.Lock()
	defer listMu.Unlock()
	
	if listIdx >= 0 && listIdx < int64(len(nativeLists)) {
		lst := nativeLists[listIdx]
		if elemIdx >= 0 && elemIdx < int64(len(lst)) {
			// Remove element by slicing
			nativeLists[listIdx] = append(lst[:elemIdx], lst[elemIdx+1:]...)
			return intValue{v: 1} // success
		}
	}
	return intValue{v: 0} // failure
}
