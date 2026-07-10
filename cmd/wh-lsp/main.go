// wh-lsp is the Whale Language Server.
// It communicates with editors (VS Code, Neovim, etc.) over stdio using
// the Language Server Protocol (JSON-RPC 2.0 with Content-Length framing).
//
// Usage:
//
//	wh-lsp
//
// The server reads from stdin and writes to stdout. Logs go to stderr.
package main

import (
	"fmt"
	"os"

	"github.com/whale-lang/whale/internal/lsp"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("whale-lsp 0.1.0")
		return
	}

	srv := lsp.NewServer(os.Stdin, os.Stdout)
	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "whale-lsp: fatal: %v\n", err)
		os.Exit(1)
	}
}
