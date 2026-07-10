package lsp

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// Serve runs the LSP server on os.Stdin / os.Stdout.
func Serve() {
	transport := NewTransport(os.Stdin, os.Stdout)
	s := &Server{
		transport: transport,
		documents: make(map[string]documentState),
	}
	s.Run()
}

type Server struct {
	transport *Transport
	mu        sync.Mutex
	documents map[string]documentState
}

func (s *Server) Run() {
	for {
		msg, err := s.transport.ReadMessage()
		if err != nil {
			if err.Error() == "EOF" {
				return
			}
			continue
		}
		s.handle(msg)
	}
}

func (s *Server) handle(msg *Message) {
	switch msg.Method {
	case "initialize":
		var params InitializeParams
		json.Unmarshal(msg.Params, &params)
		
		res := InitializeResult{
			Capabilities: ServerCapabilities{
				TextDocumentSync:           1, // Full
				DocumentFormattingProvider: true,
				HoverProvider:              true,
				CompletionProvider:         &CompletionOptions{TriggerCharacters: []string{"."}},
				DiagnosticProvider:         &DiagnosticOptions{},
			},
			ServerInfo: ServerInfo{
				Name:    "wh-lsp",
				Version: "0.1.0",
			},
		}
		s.transport.WriteMessage(&Message{
			ID:     msg.ID,
			Result: res,
		})

	case "initialized":
		// Do nothing

	case "textDocument/didOpen":
		var params DidOpenTextDocumentParams
		if err := json.Unmarshal(msg.Params, &params); err == nil {
			s.updateDoc(params.TextDocument.URI, params.TextDocument.Text)
		}

	case "textDocument/didChange":
		var params DidChangeTextDocumentParams
		if err := json.Unmarshal(msg.Params, &params); err == nil {
			if len(params.ContentChanges) > 0 {
				s.updateDoc(params.TextDocument.URI, params.ContentChanges[0].Text)
			}
		}

	case "textDocument/hover":
		var params HoverParams
		if err := json.Unmarshal(msg.Params, &params); err == nil {
			s.mu.Lock()
			state, ok := s.documents[params.TextDocument.URI]
			s.mu.Unlock()

			if ok {
				info := hoverType(state, params.Position.Line, params.Position.Character)
				if info != "" {
					s.transport.WriteMessage(&Message{
						ID: msg.ID,
						Result: HoverResult{
							Contents: MarkupContent{
								Kind:  "markdown",
								Value: info,
							},
						},
					})
					return
				}
			}
			
			// Send null result if no hover info
			s.transport.WriteMessage(&Message{
				ID:     msg.ID,
				Result: nil,
			})
		}

	case "textDocument/completion":
		var params CompletionParams
		if err := json.Unmarshal(msg.Params, &params); err == nil {
			s.mu.Lock()
			state, ok := s.documents[params.TextDocument.URI]
			s.mu.Unlock()

			if ok {
				items := completionItems(state, params.Position.Line, params.Position.Character)
				s.transport.WriteMessage(&Message{
					ID: msg.ID,
					Result: CompletionList{
						IsIncomplete: false,
						Items:        items,
					},
				})
				return
			}

			s.transport.WriteMessage(&Message{
				ID: msg.ID,
				Result: nil,
			})
		}

	default:
		// Method not found
		if msg.ID != nil {
			s.transport.WriteMessage(&Message{
				ID: msg.ID,
				Error: &RPCError{
					Code:    -32601,
					Message: fmt.Sprintf("Method not found: %s", msg.Method),
				},
			})
		}
	}
}

func (s *Server) updateDoc(uri, text string) {
	state := analyze(text)
	
	s.mu.Lock()
	s.documents[uri] = state
	s.mu.Unlock()

	diags := diagnosticsFor(state)
	
	s.transport.WriteNotification("textDocument/publishDiagnostics", PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diags,
	})
}
