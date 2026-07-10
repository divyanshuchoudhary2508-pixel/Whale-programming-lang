// Package lsp implements the Whale Language Server.
// It speaks LSP over stdio (JSON-RPC 2.0 with Content-Length framing).
package lsp

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
)

// Server is the Whale Language Server.
type Server struct {
	transport *Transport
	docs      map[string]documentState // URI -> latest analysis
	logger    *log.Logger
}

// NewServer creates a Server reading from r and writing to w.
func NewServer(r io.Reader, w io.Writer) *Server {
	return &Server{
		transport: NewTransport(r, w),
		docs:      map[string]documentState{},
		logger:    log.New(os.Stderr, "[whale-lsp] ", log.LstdFlags),
	}
}

// Run starts the main event loop, reading messages until EOF.
func (s *Server) Run() error {
	for {
		msg, err := s.transport.ReadMessage()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		s.logger.Printf("→ %s", msg.Method)
		if err := s.dispatch(msg); err != nil {
			s.logger.Printf("dispatch error: %v", err)
		}
	}
}

// dispatch routes an incoming JSON-RPC message to the appropriate handler.
func (s *Server) dispatch(msg *Message) error {
	switch msg.Method {
	case "initialize":
		return s.handleInitialize(msg)
	case "initialized":
		return nil // notification, no response needed
	case "shutdown":
		return s.reply(msg, nil)
	case "exit":
		os.Exit(0)
	case "textDocument/didOpen":
		return s.handleDidOpen(msg)
	case "textDocument/didChange":
		return s.handleDidChange(msg)
	case "textDocument/didClose":
		return nil
	case "textDocument/hover":
		return s.handleHover(msg)
	case "textDocument/completion":
		return s.handleCompletion(msg)
	default:
		// Unknown method — return method not found
		if msg.ID != nil {
			return s.replyError(msg, -32601, fmt.Sprintf("method not found: %s", msg.Method))
		}
	}
	return nil
}

// ============================================================================
// Handlers
// ============================================================================

func (s *Server) handleInitialize(msg *Message) error {
	result := InitializeResult{
		ServerInfo: ServerInfo{Name: "whale-lsp", Version: "0.1.0"},
		Capabilities: ServerCapabilities{
			TextDocumentSync: 1, // Full sync
			HoverProvider:    true,
			CompletionProvider: &CompletionOptions{
				TriggerCharacters: []string{".", "("},
			},
		},
	}
	return s.reply(msg, result)
}

func (s *Server) handleDidOpen(msg *Message) error {
	params, err := unmarshalParams[DidOpenTextDocumentParams](msg.Params)
	if err != nil {
		return err
	}
	state := analyze(params.TextDocument.Text)
	s.docs[params.TextDocument.URI] = state
	s.logger.Printf("opened %s, %d errors", params.TextDocument.URI, len(state.typeResult.Errors)+len(state.parseErrors))
	return s.pushDiagnostics(params.TextDocument.URI, state)
}

func (s *Server) handleDidChange(msg *Message) error {
	params, err := unmarshalParams[DidChangeTextDocumentParams](msg.Params)
	if err != nil {
		return err
	}
	if len(params.ContentChanges) == 0 {
		return nil
	}
	text := params.ContentChanges[len(params.ContentChanges)-1].Text
	state := analyze(text)
	s.docs[params.TextDocument.URI] = state
	return s.pushDiagnostics(params.TextDocument.URI, state)
}

func (s *Server) handleHover(msg *Message) error {
	params, err := unmarshalParams[HoverParams](msg.Params)
	if err != nil {
		return err
	}
	state, ok := s.docs[params.TextDocument.URI]
	if !ok {
		return s.reply(msg, nil)
	}

	text := hoverType(state, params.Position.Line, params.Position.Character)
	if text == "" {
		return s.reply(msg, nil)
	}
	return s.reply(msg, HoverResult{
		Contents: MarkupContent{Kind: "markdown", Value: text},
	})
}

func (s *Server) handleCompletion(msg *Message) error {
	params, err := unmarshalParams[CompletionParams](msg.Params)
	if err != nil {
		return err
	}
	state, ok := s.docs[params.TextDocument.URI]
	if !ok {
		state = documentState{}
	}

	items := completionItems(state, params.Position.Line, params.Position.Character)
	list := CompletionList{IsIncomplete: false, Items: items}
	return s.reply(msg, list)
}

// ============================================================================
// Helpers
// ============================================================================

// pushDiagnostics sends a textDocument/publishDiagnostics notification.
func (s *Server) pushDiagnostics(uri string, state documentState) error {
	diags := diagnosticsFor(state)
	if diags == nil {
		diags = []Diagnostic{} // must be an array, not null
	}
	return s.transport.WriteNotification("textDocument/publishDiagnostics", PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diags,
	})
}

// reply sends a JSON-RPC success response.
func (s *Server) reply(req *Message, result interface{}) error {
	if req.ID == nil {
		return nil // notification — no reply
	}
	var raw json.RawMessage
	if result != nil {
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		raw = json.RawMessage(data)
	} else {
		raw = json.RawMessage("null")
	}
	return s.transport.WriteMessage(&Message{
		ID:     req.ID,
		Result: &raw,
	})
}

// replyError sends a JSON-RPC error response.
func (s *Server) replyError(req *Message, code int, msg string) error {
	if req.ID == nil {
		return nil
	}
	return s.transport.WriteMessage(&Message{
		ID:    req.ID,
		Error: &RPCError{Code: code, Message: msg},
	})
}
