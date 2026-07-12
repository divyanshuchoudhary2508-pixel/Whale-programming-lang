// cmd/wh is the Whale language CLI.
//
// Usage:
//
//	wh run <file.wh>        Run a Whale program
//	wh parse <file.wh>      Parse a file and print the AST
//	wh tokens <file.wh>     Lex a file and print the tokens
//	wh check <file.wh>      Type-check a file
//	wh repl                 Start the interactive REPL
//	wh version              Print the language version
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/whale-lang/whale/internal/ast"
	"github.com/whale-lang/whale/internal/compiler"
	"github.com/whale-lang/whale/internal/interp"
	"github.com/whale-lang/whale/internal/lexer"
	"github.com/whale-lang/whale/internal/llvm"
	"github.com/whale-lang/whale/internal/lsp"
	"github.com/whale-lang/whale/internal/optimize"
	"github.com/whale-lang/whale/internal/parser"
	"github.com/whale-lang/whale/internal/pkg"
	"github.com/whale-lang/whale/internal/resolver"
	"github.com/whale-lang/whale/internal/transpiler"
	"github.com/whale-lang/whale/internal/types"
	"github.com/whale-lang/whale/internal/vm"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		startREPL()
		return
	}

	cmd := os.Args[1]
	switch cmd {
	case "run":
		if len(os.Args) < 3 {
			fatalf("usage: wh run <file.wh>")
		}
		cmdRun(os.Args[2:]...)
	case "parse":
		if len(os.Args) < 3 {
			fatalf("usage: wh parse <file.wh>")
		}
		cmdParse(os.Args[2])
	case "test":
		if len(os.Args) < 3 {
			fatalf("usage: wh test <file.wh>")
		}
		// cmdTest(os.Args[2])
	case "fmt":
		if len(os.Args) < 3 {
			fmt.Println("Usage: wh fmt <file.wh>")
			os.Exit(1)
		}
		cmdFmt(os.Args[2])
	case "lsp":
		lsp.Serve()
	case "build":
		// Handle both `wh build` (project) and `wh build <file>` (single file)
		if len(os.Args) >= 3 && !strings.HasPrefix(os.Args[2], "-") {
			cmdBuild(os.Args[2])
		} else {
			cmdBuildProject()
		}
	case "init":
		if len(os.Args) < 3 {
			fatalf("usage: wh init <project_name>")
		}
		cmdInit(os.Args[2])
	case "get":
		if len(os.Args) < 3 {
			fatalf("usage: wh get <pkg>")
		}
		cmdGet(os.Args[2])
	case "tokens":
		if len(os.Args) < 3 {
			fatalf("usage: wh tokens <file.wh>")
		}
		cmdTokens(os.Args[2])
	case "check":
		if len(os.Args) < 3 {
			fatalf("usage: wh check <file.wh>")
		}
		cmdCheck(os.Args[2])
	case "repl":
		startREPL()
	case "version", "--version", "-v":
		fmt.Printf("Whale %s\n", version)
	case "help", "--help", "-h":
		printHelp()
	default:
		// Try to run the arg as a file
		if _, err := os.Stat(cmd); err == nil {
			cmdRun(cmd)
			return
		}
		fmt.Fprintf(os.Stderr, "wh: unknown command %q\n", cmd)
		printHelp()
		os.Exit(1)
	}
}

func findCompiler() (string, error) {
	// Try PATH first
	if path, err := exec.LookPath("clang"); err == nil {
		return path, nil
	}
	if path, err := exec.LookPath("gcc"); err == nil {
		return path, nil
	}

	if runtime.GOOS == "windows" {
		// Common Windows LLVM and MinGW paths
		paths := []string{
			`C:\Program Files\LLVM\bin\clang.exe`,
			`C:\Program Files (x86)\LLVM\bin\clang.exe`,
			`C:\msys64\mingw64\bin\clang.exe`,
			`C:\msys64\mingw64\bin\gcc.exe`,
			`C:\MinGW\bin\gcc.exe`,
		}
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}

	return "", fmt.Errorf("could not find clang or gcc in PATH or standard locations")
}

// cmdRun runs a Whale source file.
func cmdRun(args ...string) {
	useVM := false
	useVMVerify := false
	useLLVM := false
	useOpt := false
	useBareMetal := false
	var ldflags []string
	var ldflagsStr string
	file := ""
	for _, a := range args {
		switch a {
		case "--vm":
			useVM = true
		case "--vm-verify":
			useVM = true
			useVMVerify = true
		case "--llvm":
			useLLVM = true
		case "--baremetal":
			useLLVM = true
			useBareMetal = true
		case "-O":
			useOpt = true
		default:
			if strings.HasPrefix(a, "--ldflags=") {
				ldflagsStr = strings.TrimPrefix(a, "--ldflags=")
				ldflags = append(ldflags, strings.Split(ldflagsStr, " ")...)
			} else {
				file = a
			}
		}
	}
	if file == "" {
		fmt.Fprintln(os.Stderr, "usage: wh run [--vm|--vm-verify|--llvm] [-O] <file.wh>")
		os.Exit(2)
	}
	src, err := os.ReadFile(file)
	if err != nil {
		fatalf("wh: cannot read %s: %v", file, err)
	}

	lexResult := lexer.Lex(string(src))
	if len(lexResult.Errors) > 0 {
		for _, e := range lexResult.Errors {
			fmt.Fprintf(os.Stderr, "%s: %s\n", file, e.Error())
		}
		os.Exit(1)
	}
	parseResult := parser.Parse(lexResult.Tokens)
	if len(parseResult.Errors) > 0 {
		for _, e := range parseResult.Errors {
			fmt.Fprintf(os.Stderr, "%s: parse error: %s\n", file, e.Error())
		}
		os.Exit(1)
	}

	if err := resolver.Resolve(&parseResult.File, filepath.Dir(file)); err != nil {
		fmt.Fprintf(os.Stderr, "resolve error: %v\n", err)
		os.Exit(1)
	}

	if useOpt {
		optimize.Optimize(&parseResult.File)
	}
	
	loader := compiler.NewFileLoader(filepath.Dir(file))
	typeResult := types.CheckWithConfig(parseResult.File, types.Config{
		Importer: loader,
	})
	if len(typeResult.Errors) > 0 {
		for _, e := range typeResult.Errors {
			fmt.Fprintf(os.Stderr, "%s: type error: %s\n", file, e.Error())
		}
		os.Exit(1)
	}

	if useLLVM {
		monoFile, err := llvm.Monomorphize(&parseResult.File, typeResult.Types)
		if err != nil {
			fmt.Fprintf(os.Stderr, "monomorphize error: %v\n", err)
			os.Exit(1)
		}
		
		llvmAst, err := llvm.Lower(monoFile, typeResult.Types)
		if err != nil {
			fmt.Fprintf(os.Stderr, "LLVM lower error: %v\n", err)
			os.Exit(1)
		}
		
		gen := llvm.NewGenerator()
		ir := gen.Generate(llvmAst)
		
		outFile := strings.TrimSuffix(file, filepath.Ext(file)) + ".ll"
		err = os.WriteFile(outFile, []byte(ir), 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "LLVM write error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Successfully generated %s\n", outFile)

		exeFile := strings.TrimSuffix(file, filepath.Ext(file))
		if runtime.GOOS == "windows" {
			exeFile += ".exe"
		}
		
		fmt.Println("Compiling with clang...")
		
		clangArgs := []string{outFile, "-o", exeFile}
		if useBareMetal {
			clangArgs = append(clangArgs, "-ffreestanding", "-nostdlib")
		} else {
			clangArgs = append(clangArgs, "runtime/runtime.c", "-lm")
			if runtime.GOOS == "windows" {
				clangArgs = append(clangArgs, "-lws2_32")
			}
		}

		if useOpt {
			clangArgs = append([]string{"-O3"}, clangArgs...)
		}
		
		if len(ldflags) > 0 {
			clangArgs = append(clangArgs, ldflags...)
		}
		
		compilerPath, err := findCompiler()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Compilation failed: %v\n", err)
			os.Exit(1)
		}

		// If we have a linker script (-T flag), we need a two-step build:
		// 1) compile .ll -> .o
		// 2) link .o with the linker script
		hasLinkerScript := false
		for _, f := range ldflags {
			if strings.HasPrefix(f, "-T") {
				hasLinkerScript = true
				break
			}
		}

		if hasLinkerScript {
			// Step 1: compile to object file
			objFile := strings.TrimSuffix(outFile, ".ll") + ".o"
			compileArgs := []string{"-c", outFile, "-o", objFile}
			if useBareMetal {
				compileArgs = append(compileArgs, "-ffreestanding", "-nostdlib")
			}
			if useOpt {
				compileArgs = append([]string{"-O3"}, compileArgs...)
			}
			fmt.Println("Compiling IR to object file...")
			cc := exec.Command(compilerPath, compileArgs...)
			cc.Stdout = os.Stdout
			cc.Stderr = os.Stderr
			if err := cc.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Clang compilation failed: %v\n", err)
				return
			}

			// Step 2: link with linker script
			linkArgs := []string{objFile, "-o", exeFile}
			if useBareMetal {
				linkArgs = append(linkArgs, "-ffreestanding", "-nostdlib")
			}
			linkArgs = append(linkArgs, ldflags...)
			fmt.Println("Linking with linker script...")
			lk := exec.Command(compilerPath, linkArgs...)
			lk.Stdout = os.Stdout
			lk.Stderr = os.Stderr
			if err := lk.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Clang link failed: %v\n", err)
			} else {
				fmt.Printf("Successfully compiled to %s\n", exeFile)
			}
		} else {
			cmd := exec.Command(compilerPath, clangArgs...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Clang compilation failed: %v\n", err)
			} else {
				fmt.Printf("Successfully compiled to %s\n", exeFile)
			}
		}
		return
	}

	// VM path.
	if useVM {
		var buf bytes.Buffer
		// If --vm-verify, write to both the buffer and stdout,
		// so we can compare against the tree-walker.
		var out io.Writer = os.Stdout
		if useVMVerify {
			out = io.MultiWriter(&buf, os.Stdout)
		}
		errs := vm.RunSource(string(src), out)
		if len(errs) > 0 {
			fmt.Fprintf(os.Stderr, "errors in %s:\n", filepath.Base(file))
			for _, e := range errs {
				fmt.Fprintf(os.Stderr, "  %v\n", e)
			}
			os.Exit(1)
		}
		return
	}

	// Tree-walker path (existing).
	_, errs := interp.RunAST(parseResult.File)
	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "errors in %s:\n", filepath.Base(file))
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  %v\n", e)
		}
		os.Exit(1)
	}
}

// cmdParse parses a file and prints the AST.
func cmdParse(path string) {
	src, err := os.ReadFile(path)
	if err != nil {
		fatalf("wh: cannot read %s: %v", path, err)
	}
	lexResult := lexer.Lex(string(src))
	if len(lexResult.Errors) > 0 {
		for _, e := range lexResult.Errors {
			fmt.Fprintf(os.Stderr, "%s: %s\n", path, e.Error())
		}
		os.Exit(1)
	}
	parseResult := parser.Parse(lexResult.Tokens)
	if len(parseResult.Errors) > 0 {
		for _, e := range parseResult.Errors {
			fmt.Fprintf(os.Stderr, "%s: %s\n", path, e.Error())
		}
		os.Exit(1)
	}
	for _, stmt := range parseResult.File.Body {
		fmt.Println(stmt.String())
	}
}

// cmdTokens lexes a file and prints each token.
func cmdTokens(path string) {
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("Error reading file:", err)
		os.Exit(1)
	}
	res := lexer.Lex(string(src))
	for _, err := range res.Errors {
		fmt.Printf("Lex error at %d:%d: %s\n", err.Pos.Line, err.Pos.Column, err.Msg)
	}
	for _, tok := range res.Tokens {
		fmt.Printf("%-12s %-20q %d:%d\n", tok.Type.String(), tok.Literal, tok.Pos.Line, tok.Pos.Column)
	}
}

func cmdFmt(path string) {
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("Error reading file:", err)
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

	// Format
	formatted := ast.Format(parseRes.File)
	
	// Write back
	err = os.WriteFile(path, []byte(formatted), 0644)
	if err != nil {
		fmt.Println("Error writing file:", err)
		os.Exit(1)
	}
	fmt.Printf("Formatted %s\n", path)
}

func cmdInit(name string) {
	fmt.Printf("Initializing project %s...\n", name)
	if err := os.Mkdir(name, 0755); err != nil && !os.IsExist(err) {
		fatalf("failed to create directory: %v", err)
	}
	if err := os.Mkdir(filepath.Join(name, "src"), 0755); err != nil && !os.IsExist(err) {
		fatalf("failed to create src directory: %v", err)
	}

	conf := &pkg.Config{
		Name:    name,
		Version: "0.1.0",
		Dependencies: make(map[string]string),
	}
	if err := pkg.SaveConfig(name, conf); err != nil {
		fatalf("failed to save wh.toml: %v", err)
	}

	mainWh := filepath.Join(name, "src", "main.wh")
	if err := os.WriteFile(mainWh, []byte("fn main() {\n    print(\"Hello from " + name + "!\");\n}\n"), 0644); err != nil {
		fatalf("failed to create main.wh: %v", err)
	}
	fmt.Println("Project created successfully.")
}

func cmdGet(pkgName string) {
	fmt.Printf("Fetching package %s...\n", pkgName)
	conf, err := pkg.LoadConfig(".")
	if err != nil {
		fatalf("not in a whale project (wh.toml missing): %v", err)
	}
	// Stub: just add to dependencies
	conf.Dependencies[pkgName] = "latest"
	if err := pkg.SaveConfig(".", conf); err != nil {
		fatalf("failed to update wh.toml: %v", err)
	}
	fmt.Printf("Added %s to dependencies.\n", pkgName)
}

func cmdBuildProject() {
	conf, err := pkg.LoadConfig(".")
	if err != nil {
		fatalf("not in a whale project (wh.toml missing): %v", err)
	}
	fmt.Printf("Building project %s v%s...\n", conf.Name, conf.Version)
	mainWh := filepath.Join("src", "main.wh")
	if _, err := os.Stat(mainWh); err != nil {
		fatalf("src/main.wh not found")
	}
	cmdBuild(mainWh)
}

func cmdBuild(filename string) {
	fmt.Printf("Compiling %s...\n", filename)
	src, err := os.ReadFile(filename)
	if err != nil {
		fatalf("failed to read file: %v", err)
	}

	// Parse
	lexRes := lexer.Lex(string(src))
	if len(lexRes.Errors) > 0 {
		fatalf("Lex error: %v", lexRes.Errors[0])
	}
	parseRes := parser.Parse(lexRes.Tokens)
	if len(parseRes.Errors) > 0 {
		fatalf("Parse error: %v", parseRes.Errors[0])
	}

	// Transpile
	goCode, err := transpiler.Transpile(parseRes.File)
	if err != nil {
		fatalf("transpile error: %v", err)
	}

	// Write to temporary Go file
	buildDir := ".whale_build"
	os.MkdirAll(buildDir, 0755)
	
	goFile := filepath.Join(buildDir, "main.go")
	os.WriteFile(goFile, []byte(goCode), 0644)
	
	// Init go module if doesn't exist
	if _, err := os.Stat(filepath.Join(buildDir, "go.mod")); os.IsNotExist(err) {
		cmd := exec.Command("go", "mod", "init", "whale_bin")
		cmd.Dir = buildDir
		cmd.Run()
	}

	// Compile to native binary
	exeName := strings.TrimSuffix(filepath.Base(filename), ".wh") + ".exe"
	fmt.Printf("Building native binary %s...\n", exeName)
	
	cmd := exec.Command("go", "build", "-o", "../" + exeName)
	cmd.Dir = buildDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		fatalf("go build failed: %v\n%s", err, string(out))
	}

	fmt.Printf("Successfully compiled to %s\n", exeName)
}

// cmdCheck type-checks a file and reports errors.
func cmdCheck(path string) {
	src, err := os.ReadFile(path)
	if err != nil {
		fatalf("wh: cannot read %s: %v", path, err)
	}
	lexResult := lexer.Lex(string(src))
	if len(lexResult.Errors) > 0 {
		for _, e := range lexResult.Errors {
			fmt.Fprintf(os.Stderr, "%s: %s\n", path, e.Error())
		}
		os.Exit(1)
	}
	parseResult := parser.Parse(lexResult.Tokens)
	if len(parseResult.Errors) > 0 {
		for _, e := range parseResult.Errors {
			fmt.Fprintf(os.Stderr, "%s: parse error: %s\n", path, e.Error())
		}
		os.Exit(1)
	}
	loader := compiler.NewFileLoader(filepath.Dir(path))
	typeResult := types.CheckWithConfig(parseResult.File, types.Config{
		Importer: loader,
	})
	if len(typeResult.Errors) == 0 {
		fmt.Printf("%s: ok\n", path)
		return
	}
	for _, e := range typeResult.Errors {
		fmt.Fprintf(os.Stderr, "%s: %s\n", path, e.Error())
	}
	os.Exit(1)
}

// startREPL starts the interactive read-eval-print loop.
func startREPL() {
	fmt.Printf("Whale %s REPL — type 'exit' to quit\n", version)
	fmt.Println("  Multi-line: end each line with '\\' to continue")

	i := interp.New()
	scanner := bufio.NewScanner(os.Stdin)

	var buf strings.Builder
	for {
		if buf.Len() == 0 {
			fmt.Print(">>> ")
		} else {
			fmt.Print("... ")
		}

		if !scanner.Scan() {
			fmt.Println()
			break
		}

		line := scanner.Text()
		trimmed := strings.TrimRight(line, " \t")

		if trimmed == "exit" || trimmed == "quit" {
			break
		}

		// Multi-line continuation
		if strings.HasSuffix(trimmed, "\\") {
			buf.WriteString(trimmed[:len(trimmed)-1])
			buf.WriteString("\n")
			continue
		}

		buf.WriteString(line)
		src := buf.String()
		buf.Reset()

		if strings.TrimSpace(src) == "" {
			continue
		}

		out, errs := i.Exec(src)
		if len(errs) > 0 {
			for _, e := range errs {
				fmt.Fprintf(os.Stderr, "error: %s\n", e)
			}
		}
		// Output is already printed by print() calls inside Exec.
		// If the expression produces a non-null value that wasn't printed,
		// we could print it here (REPL-style), but for v0.1 we skip it.
		_ = out
	}
}

func printHelp() {
	fmt.Print(`Whale programming language v` + version + `

Usage:
  wh run <file.wh>     Run a Whale program
  wh parse <file.wh>   Parse and print the AST
  wh test <file.wh>    Run tests in a file
  wh fmt <file.wh>     Format a file in place
  wh lsp               Start the language server
  wh init <name>       Initialize a new project
  wh get <pkg>         Add a dependency
  wh tokens <file.wh>  Lex and print tokens
  wh check <file.wh>   Type-check a file
  wh repl              Start the interactive REPL
  wh version           Print the version

Examples:
  wh run hello.wh
  wh repl
`)
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
