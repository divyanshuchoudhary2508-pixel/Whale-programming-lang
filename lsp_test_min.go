package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type msg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  interface{}     `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

func send(w io.Writer, m msg) {
	m.JSONRPC = "2.0"
	data, _ := json.Marshal(m)
	fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(data), data)
}

func recv(r *bufio.Reader) msg {
	var cl int
	for {
		line, _ := r.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length: ") {
			cl, _ = strconv.Atoi(strings.TrimPrefix(line, "Content-Length: "))
		}
	}
	body := make([]byte, cl)
	io.ReadFull(r, body)
	fmt.Printf("DEBUG RECV: %s\n", string(body))
	var m msg
	json.Unmarshal(body, &m)
	return m
}

var pass, fail int

func check(label string, got bool) {
	if got {
		fmt.Printf("  [PASS] %s\n", label)
		pass++
	} else {
		fmt.Printf("  [FAIL] %s\n", label)
		fail++
	}
}

func main() {
	fmt.Println("=== Minimal LSP Smoke Test ===")
	
	// Build binary first
	fmt.Println("Building wh...")
	buildCmd := exec.Command("go", "build", "-o", "wh-lsp.exe", "./cmd/wh")
	if err := buildCmd.Run(); err != nil {
		fmt.Println("Build failed:", err)
		return
	}

	cmd := exec.Command("./wh-lsp.exe", "lsp")
	stdin, _ := cmd.StdinPipe()
	stdoutPipe, _ := cmd.StdoutPipe()
	cmd.Start()
	defer cmd.Process.Kill()

	r := bufio.NewReader(stdoutPipe)
	id := 1
	nextID := func() *int { v := id; id++; return &v }

	time.Sleep(1 * time.Second)

	fmt.Println("Test 1: initialize")
	send(stdin, msg{ID: nextID(), Method: "initialize", Params: map[string]interface{}{"processId": 1}})
	resp := recv(r)
	check("got result (not error)", resp.Error == nil)
	fmt.Printf("resp.Result = %s\n", string(resp.Result))

	var initResult map[string]interface{}
	json.Unmarshal(resp.Result, &initResult)
	caps, hasCaps := initResult["capabilities"].(map[string]interface{})
	check("has capabilities", hasCaps)
	if hasCaps {
		check("documentFormattingProvider = true", caps["documentFormattingProvider"] == true)
		check("textDocumentSync = 1", caps["textDocumentSync"].(float64) == 1)
	}

	send(stdin, msg{Method: "initialized", Params: map[string]interface{}{}})

	fmt.Println("Test 2: textDocument/didOpen -> diagnostics for type error")
	badSource := "fn add(a: int, b: int) -> int {\n  return \"wrong\";\n}\n"
	send(stdin, msg{
		Method: "textDocument/didOpen",
		Params: map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":        "file:///test.wh",
				"languageId": "whale",
				"version":    1,
				"text":       badSource,
			},
		},
	})

	diag := recv(r)
	check("method is publishDiagnostics", diag.Method == "textDocument/publishDiagnostics")
	rawParams, _ := json.Marshal(diag.Params)
	check("diagnostics array present", strings.Contains(string(rawParams), "diagnostics"))
	check("contains type error", strings.Contains(string(rawParams), "mismatch"))

	fmt.Println("Test 3: textDocument/didChange -> diagnostics cleared")
	goodSource := "fn add(a: int, b: int) -> int {\n  return a + b;\n}\n"
	send(stdin, msg{
		Method: "textDocument/didChange",
		Params: map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":     "file:///test.wh",
				"version": 2,
			},
			"contentChanges": []map[string]interface{}{{"text": goodSource}},
		},
	})
	diag2 := recv(r)
	raw2, _ := json.Marshal(diag2.Params)
	check("diagnostics cleared", strings.Contains(string(raw2), `"diagnostics":[]`) || strings.Contains(string(raw2), `"diagnostics":null`))

	fmt.Printf("\n=== Results: %d passed, %d failed ===\n", pass, fail)
}
