# 🐋 Whale v1.0

> **The Shield of Rust. The Engine of Go. The Scalpel of Zig.**

Whale is a modern, statically-typed systems programming language designed from the ground up to offer uncompromising speed, safety, and expressiveness. It features a lightning-fast tree-walking interpreter for rapid development and a native LLVM backend for blazing-fast machine code execution.

## 🌟 Key Features

* **Two Execution Engines**: Run your code instantly using the `wh` Interpreter, or compile it to highly optimized native binaries via the **LLVM Backend**.
* **The "Shield of Rust"**: 
  * A robust, traceable Error Handling system with `Result<T, E>` types and the elegant `?` operator.
  * A custom-built, conservative Mark-and-Sweep Garbage Collector ensuring memory safety without manual management.
* **The "Engine of Go"**: First-class concurrency built directly into the language via the `spawn` keyword, coupled with asynchronous Channels (`make_chan`, `<-`) for seamless thread communication.
* **The "Scalpel of Zig"**: Powerful compile-time meta-programming using `comptime`, allowing you to execute logic during the compilation phase for ultimate runtime performance.
* **Zero-Cost Module System**: A clever AST-mangling module system that allows you to cleanly encapsulate logic into files without the overhead of complex external linkers. LLVM applies cross-module inlining effortlessly!
* **Standard Library**: Native C and Go bindings for cross-platform TCP Sockets, File I/O, and $O(1)$ HashMaps out of the box.

---

## 🚀 Getting Started

### Prerequisites
To build the compiler, you need **Go** installed on your system.
To compile Whale code natively using the LLVM backend, you need **Clang** installed and accessible in your system's PATH.

### Building the Compiler
Clone the repository and build the CLI:
```bash
git clone https://github.com/yourusername/whale.git
cd whale
go build -o wh ./cmd/wh
```

### Usage
Run code interactively using the Interpreter:
```bash
./wh run my_script.wh
```

Compile and run code natively using the LLVM Backend:
```bash
./wh run --llvm my_script.wh
```

---

## 💻 Language Tour

### Hello World & Variables
```rust
fn main() {
    let name = "Whale";
    let version = 1.0;
    print("Hello from ", name, " v", version, "!");
}
```

### Error Handling (The Shield)
No more silent crashes. Whale uses robust `Result` types.
```rust
fn divide(a: int, b: int) -> int! {
    if b == 0 {
        return error("Cannot divide by zero");
    }
    return a / b;
}

fn calculate() -> int! {
    // The '?' operator automatically propagates errors up the stack!
    let result = divide(10, 0)?; 
    return result;
}
```

### Concurrency (The Engine)
Spin up background threads instantly with `spawn` and communicate safely via channels.
```rust
fn worker(ch: chan) {
    print("Worker doing heavy lifting...");
    ch <- 42; // Send data through channel
}

fn main() {
    let ch = make_chan();
    spawn worker(ch);
    
    let result = <-ch; // Block and wait for result
    print("Received from worker: ", result);
}
```

### Compile-Time Execution (The Scalpel)
Execute expensive logic during compilation. The runtime cost is exactly zero.
```rust
fn main() {
    // The math is done by the compiler, the runtime just sees: let val = 120;
    let val = comptime {
        let mut x = 1;
        for i in 1..6 {
            x = x * i;
        }
        x
    };
    print("5! is ", val);
}
```

### The Module System
Cleanly organize your project without configuration files.
```rust
// lib/math.wh
fn add(a: int, b: int) -> int {
    return a + b;
}
```

```rust
// main.wh
import "lib/math" as m;

fn main() {
    let sum = m.add(10, 20);
    print("Sum: ", sum);
}
```

---

## 🏗️ Architecture
Whale is built in two phases:
1. **Frontend (Go)**: A hand-written recursive descent parser, lexer, and type-checker that produces a fully typed Abstract Syntax Tree (AST).
2. **Backend**: 
   * The AST can be passed directly to the `interp` package for instant, dynamic execution.
   * Or, the AST is lowered into LLVM IR via the native `llvm` package, linked against `runtime.c`, and compiled into a standalone executable via Clang.

## 📜 License
Whale is open-source software licensed under the MIT License.
