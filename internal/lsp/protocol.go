package lsp

import "encoding/json"

// ============================================================================
// Core LSP protocol types
// ============================================================================

type Position struct {
	Line      int `json:"line"`      // 0-based
	Character int `json:"character"` // 0-based
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// ============================================================================
// Initialize
// ============================================================================

type InitializeParams struct {
	ProcessID int `json:"processId"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   ServerInfo         `json:"serverInfo"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ServerCapabilities struct {
	TextDocumentSync           int                  `json:"textDocumentSync"`   // 1 = Full
	DocumentFormattingProvider bool                 `json:"documentFormattingProvider"`
	HoverProvider              bool                 `json:"hoverProvider"`
	CompletionProvider *CompletionOptions   `json:"completionProvider,omitempty"`
	DiagnosticProvider *DiagnosticOptions   `json:"diagnosticProvider,omitempty"`
}

type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters"`
}

type DiagnosticOptions struct {
	Identifier            string `json:"identifier"`
	InterFileDependencies bool   `json:"interFileDependencies"`
	WorkspaceDiagnostics  bool   `json:"workspaceDiagnostics"`
}

// ============================================================================
// textDocument/didOpen, didChange
// ============================================================================

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

type TextDocumentContentChangeEvent struct {
	Text string `json:"text"` // Full sync — entire file content
}

// ============================================================================
// Diagnostics
// ============================================================================

type DiagnosticSeverity int

const (
	SeverityError       DiagnosticSeverity = 1
	SeverityWarning     DiagnosticSeverity = 2
	SeverityInformation DiagnosticSeverity = 3
	SeverityHint        DiagnosticSeverity = 4
)

type Diagnostic struct {
	Range    Range              `json:"range"`
	Severity DiagnosticSeverity `json:"severity"`
	Source   string             `json:"source"`
	Message  string             `json:"message"`
}

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// ============================================================================
// Hover
// ============================================================================

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type HoverParams = TextDocumentPositionParams

type MarkupContent struct {
	Kind  string `json:"kind"`  // "plaintext" or "markdown"
	Value string `json:"value"`
}

type HoverResult struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// ============================================================================
// Completion
// ============================================================================

type CompletionParams = TextDocumentPositionParams

type CompletionItemKind int

const (
	CompletionKindFunction  CompletionItemKind = 3
	CompletionKindVariable  CompletionItemKind = 6
	CompletionKindKeyword   CompletionItemKind = 14
)

type CompletionItem struct {
	Label         string             `json:"label"`
	Kind          CompletionItemKind `json:"kind"`
	Detail        string             `json:"detail,omitempty"`
	Documentation string             `json:"documentation,omitempty"`
	InsertText    string             `json:"insertText,omitempty"`
}

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

// ============================================================================
// Helpers
// ============================================================================

// unmarshalParams decodes JSON params into the target struct.
func unmarshalParams[T any](raw json.RawMessage) (T, error) {
	var t T
	err := json.Unmarshal(raw, &t)
	return t, err
}
