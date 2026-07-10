package main

import (
	"fmt"
	"os"
	
	"github.com/whale-lang/whale/internal/dap"
)

func main() {
	// DAP operates over stdin/stdout, so any normal fmt.Println will corrupt the protocol.
	// We redirect log output to stderr or a file if needed.
	
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("whale-dap 0.1.0")
		return
	}

	server := dap.NewServer()
	if err := server.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "wh-dap fatal error: %v\n", err)
		os.Exit(1)
	}
}
