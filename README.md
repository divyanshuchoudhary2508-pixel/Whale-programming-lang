<div align="center">
  <!-- To display your uploaded image, save it to your repository as 'assets/whale.png' and it will appear here! -->
  <img src="assets/whale.png" alt="Whale Programming Language" width="400" />
</div>

# 🐳 Whale v1.0

> **The Shield of Rust. The Engine of Go. The Flexibility of Python.**

Whale is a modern, statically-typed systems programming language designed from the ground up to offer uncompromising speed, safety, and expressiveness. It features a lightning-fast tree-walking interpreter for rapid development, and a **native Transpiler** (Whale-to-Go) for blazing-fast native machine code execution (up to 92x faster than the interpreter).

## ✨ Key Features

* **Two Execution Engines**: Run your code instantly using the `wh run` Interpreter, or compile it to highly optimized native binaries via the new `wh build` Transpiler.
* **The "Shield of Rust"**: 
  * A robust, traceable Error Handling system with `Result<T, E>` types and the elegant `?` operator.
  * No silent crashes—handle errors explicitly through pattern matching or propagate them cleanly.
* **The "Engine of Go"**: First-class concurrency built directly into the language via the `spawn` keyword, coupled with asynchronous Channels (`make_chan`, `<-`) for seamless thread communication.
* **Zero-Cost Module System**: A clever AST-driven module system that allows you to cleanly encapsulate logic into files without configuration overhead.
* **Standard Library**: Native networking (`std/net`) for TCP Sockets, File I/O (`std/fs`), built-in JSON and CSV parsing (`std/json`), and $O(1)$ HashMaps out of the box. You can build Concurrent Web Servers natively in Whale!
* **Built-in Tooling**: Comes with a built-in formatter (`wh fmt`) and testing framework (`wh test`) out of the box.

---

## 🚀 Getting Started

### Prerequisites
To use Whale and compile your projects, you need **Go** installed on your system.

### Installation
Clone the repository and build the CLI:
```bash
git clone https://github.com/yourusername/whale.git
cd whale
go build -o wh.exe ./cmd/wh
```
*(Tip: Add the directory containing `wh.exe` to your system's PATH for easy access!)*

### Usage

**1. Run code interactively using the Interpreter (Great for rapid prototyping):**
```bash
wh run my_script.wh
```

**2. Compile code to a highly optimized native binary (Great for production):**
```bash
wh build my_script.wh
# This will output my_script.exe (or my_script on Linux/macOS)
./my_script.exe
```

**3. Format your code:**
```bash
wh fmt my_script.wh
```

**4. Run tests:**
```bash
wh test
```

---

## 📖 Language Tour

### Hello World
```rust
fn main() {
    let name = "Whale";
    let version = 1.0;
    print("Hello from ", name, " v", version, "!");
}
```

### Error Handling (The Shield)
No more silent crashes. Whale uses robust `Result` types and pattern matching.
```rust
import "std/net";

fn main() {
    // The '?' operator automatically propagates errors up the stack!
    let listener = net.listen(8080)?; 
    
    // Or you can pattern match on the Result!
    let conn_idx = match net.accept(listener) {
        Ok(idx) => idx,
        Err(e) => {
            print("Failed to accept connection: ", e);
            return;
        }
    };
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

### Building a Concurrent Web Server
Thanks to the new `std/net` module, building a web server is incredibly simple.
```rust
import "std/net";

fn handle_request(conn_idx: int) -> int {
    let req = match net_recv(conn_idx) { Ok(r) => r, Err(e) => "" };
    
    let json_data = "{\"status\": \"success\"}";
    let response = "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n" + json_data;
    
    match net_send(conn_idx, response) { Ok(_) => 0, Err(e) => 1 };
    net_close(conn_idx);
    
    return 0;
}

fn main() {
    print("Listening on http://localhost:8080...");
    let listener_idx = match net_listen(8080) { Ok(idx) => idx, Err(e) => 0 };
    
    while true {
        let conn_idx = match net_accept(listener_idx) { Ok(idx) => idx, Err(e) => 0 };
        spawn handle_request(conn_idx);
    }
}
```

---

## 🏗️ Architecture
Whale is built in two phases:
1. **Frontend (Go)**: A hand-written recursive descent parser, lexer, and type-checker that produces a fully typed Abstract Syntax Tree (AST).
2. **Backend**: 
   * **Interpreter**: The AST can be passed directly to the `eval` package for instant, dynamic execution.
   * **Transpiler**: The AST is transpiled into raw Go code, allowing it to hook directly into Go's incredibly fast runtime and garbage collector, giving it native-level performance.

## 📄 License
Whale is open-source software licensed under the MIT License.
