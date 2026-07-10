// Package lsp implements the Whale Language Server.
// It speaks the Language Server Protocol (LSP) over stdio using JSON-RPC 2.0.
// Each message is framed with a Content-Length header, exactly like VS Code expects.
package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Message is a raw JSON-RPC 2.0 message.
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Transport handles framed JSON-RPC I/O over stdin/stdout.
type Transport struct {
	r *bufio.Reader
	w io.Writer
}

// NewTransport creates a new Transport reading from r and writing to w.
func NewTransport(r io.Reader, w io.Writer) *Transport {
	return &Transport{r: bufio.NewReader(r), w: w}
}

// ReadMessage reads one Content-Length framed JSON-RPC message.
func (t *Transport) ReadMessage() (*Message, error) {
	// Read headers (terminated by blank line)
	contentLength := 0
	for {
		line, err := t.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length: ") {
			val := strings.TrimPrefix(line, "Content-Length: ")
			contentLength, err = strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("invalid Content-Length: %s", val)
			}
		}
	}

	if contentLength == 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	// Read body
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(t.r, body); err != nil {
		return nil, err
	}

	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// WriteMessage writes a JSON-RPC response with a Content-Length frame.
func (t *Transport) WriteMessage(msg *Message) error {
	msg.JSONRPC = "2.0"
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := fmt.Fprint(t.w, header); err != nil {
		return err
	}
	_, err = t.w.Write(data)
	return err
}

// WriteNotification sends a JSON-RPC notification (no ID, no response expected).
func (t *Transport) WriteNotification(method string, params interface{}) error {
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}
	raw := json.RawMessage(data)
	msg := &Message{
		JSONRPC: "2.0",
		Method:  method,
		Params:  raw,
	}
	return t.WriteMessage(msg)
}
