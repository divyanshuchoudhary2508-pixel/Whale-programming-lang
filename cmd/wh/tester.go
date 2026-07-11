package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/whale-lang/whale/internal/interp"
	"github.com/whale-lang/whale/internal/lexer"
	"github.com/whale-lang/whale/internal/parser"
	"github.com/whale-lang/whale/internal/ast"
)

func cmdTest(path string) {
	fmt.Printf("Running tests in %s...\n", path)
	
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading file: %v\n", err)
		os.Exit(1)
	}

	// Lex
	lexRes := lexer.Lex(string(src))
	if len(lexRes.Errors) > 0 {
		fmt.Println("Lex errors:")
		for _, e := range lexRes.Errors {
			fmt.Printf("  %d:%d: %s\n", e.Pos.Line, e.Pos.Column, e.Msg)
		}
		os.Exit(1)
	}

	// Parse
	parseRes := parser.Parse(lexRes.Tokens)
	if len(parseRes.Errors) > 0 {
		fmt.Println("Parse errors:")
		for _, e := range parseRes.Errors {
			fmt.Printf("  %d:%d: %s\n", e.Pos.Line, e.Pos.Column, e.Msg)
		}
		os.Exit(1)
	}

	// Find all tests
	var testFuncs []string
	for _, stmt := range parseRes.File.Body {
		if fn, ok := stmt.(*ast.FnStmt); ok {
			if strings.HasPrefix(fn.Name, "test_") {
				testFuncs = append(testFuncs, fn.Name)
			}
		}
	}

	if len(testFuncs) == 0 {
		fmt.Println("No tests found.")
		return
	}

	fmt.Printf("Found %d tests.\n\n", len(testFuncs))

	passed := 0
	failed := 0

	for _, testName := range testFuncs {
		// Run each test in a completely fresh interpreter to avoid state pollution
		i := interp.New()
		
		// First, load the file into the interpreter environment
		_, errs := i.Exec(string(src))
		if len(errs) > 0 {
			fmt.Printf("Failed to load environment for %s:\n", testName)
			for _, e := range errs {
				fmt.Println("  " + e)
			}
			os.Exit(1)
		}

		start := time.Now()
		
		// Run the specific test function
		runTest(i, testName, &passed, &failed, start)
	}

	fmt.Printf("\nTest Results: %d passed, %d failed\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func runTest(i *interp.Interpreter, testName string, passed, failed *int, start time.Time) {
	defer func() {
		if r := recover(); r != nil {
			elapsed := time.Since(start)
			fmt.Printf("FAIL  %s (%.2fms)\n", testName, float64(elapsed.Microseconds())/1000.0)
			fmt.Printf("      Panic: %v\n", r)
			*failed++
		}
	}()

	// Execute the test function
	_, errs := i.Exec(testName + "();")
	elapsed := time.Since(start)
	
	if len(errs) > 0 {
		fmt.Printf("FAIL  %s (%.2fms)\n", testName, float64(elapsed.Microseconds())/1000.0)
		for _, e := range errs {
			fmt.Printf("      %s\n", e)
		}
		*failed++
	} else {
		fmt.Printf("PASS  %s (%.2fms)\n", testName, float64(elapsed.Microseconds())/1000.0)
		*passed++
	}
}
