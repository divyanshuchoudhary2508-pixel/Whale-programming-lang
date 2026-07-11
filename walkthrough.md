# Whale Programming Language - Walkthrough

Welcome to Whale! We've made fantastic progress implementing and proving out the core capabilities of the language.

## What's New? 🚀

### 1. HTTP Web Framework
We've built a fully functional HTTP routing framework directly in Whale (`std/http.wh`)!
- Handles incoming TCP connections asynchronously using Whale's native `spawn` goroutines.
- Supports modular routing logic like `app.get("/path", handler)`.
- Parses incoming HTTP headers to match routes automatically.

### 2. File System Standard Library
We extended the FFI layer to include direct file reading and writing capabilities natively through Go (`read_file` and `write_file`). 
- This powers the new `std/fs` module, offering `fs_read_to_string` and `fs_write_string`.
- Avoids the overhead of manual character-by-character parsing!

### 3. The Whale Snake Game 🐍
We built and hosted an interactive Snake Game using the Whale HTTP Server!
- A modern UI styled with smooth gradients, animations, and shadows.
- Fully functional gameplay written in JavaScript for the canvas logic.
- **Persistent High Scores**: The game communicates via API to `/api/score`, and Whale handles the `POST` request to read the JSON body, parse out the new high score, and save it persistently to a `score.txt` file.

### Technical Achievements
- We squashed several tricky bugs in the interpreter, such as nested struct/closure variable mutation bugs.
- We fixed the FFI bindings to map `string_split` and HTTP-related `net` commands correctly.
- Background tasks no longer hang due to port contention.

> [!TIP]
> You can run the game yourself! Just run `.\testwh.exe run app.wh` in the terminal and navigate to `http://127.0.0.1:8080/` in your browser!
