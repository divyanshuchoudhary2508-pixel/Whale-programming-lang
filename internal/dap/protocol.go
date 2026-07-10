package dap

import "encoding/json"

// ProtocolMessage is the base class of requests, responses, and events.
type ProtocolMessage struct {
	Seq  int    `json:"seq"`
	Type string `json:"type"` // "request", "response", "event"
}

// Request is a client-to-server request.
type Request struct {
	ProtocolMessage
	Command   string          `json:"command"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// Response is a server-to-client response.
type Response struct {
	ProtocolMessage
	RequestSeq int             `json:"request_seq"`
	Success    bool            `json:"success"`
	Command    string          `json:"command"`
	Message    string          `json:"message,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}

// Event is a server-to-client event.
type Event struct {
	ProtocolMessage
	Event string          `json:"event"`
	Body  json.RawMessage `json:"body,omitempty"`
}

// -- Common Types --

type Capabilities struct {
	SupportsConfigurationDoneRequest bool `json:"supportsConfigurationDoneRequest"`
}

type InitializeRequestArguments struct {
	AdapterID string `json:"adapterID"`
}

type Thread struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Source struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

type SourceBreakpoint struct {
	Line   int `json:"line"`
	Column int `json:"column,omitempty"`
}

type Breakpoint struct {
	Id       int    `json:"id,omitempty"`
	Verified bool   `json:"verified"`
	Message  string `json:"message,omitempty"`
	Source   Source `json:"source,omitempty"`
	Line     int    `json:"line,omitempty"`
}

type SetBreakpointsArguments struct {
	Source      Source             `json:"source"`
	Breakpoints []SourceBreakpoint `json:"breakpoints"`
}

type SetBreakpointsResponseBody struct {
	Breakpoints []Breakpoint `json:"breakpoints"`
}

type StackFrame struct {
	Id     int    `json:"id"`
	Name   string `json:"name"`
	Source Source `json:"source,omitempty"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type Scope struct {
	Name               string `json:"name"`
	VariablesReference int    `json:"variablesReference"`
	NamedVariables     int    `json:"namedVariables,omitempty"`
	Expensive          bool   `json:"expensive"`
}

type Variable struct {
	Name               string `json:"name"`
	Value              string `json:"value"`
	Type               string `json:"type,omitempty"`
	VariablesReference int    `json:"variablesReference"` // > 0 if it has children
}

// -- Event Bodies --

type InitializedEventBody struct{}

type StoppedEventBody struct {
	Reason            string `json:"reason"` // "step", "breakpoint", "exception", "pause"
	Description       string `json:"description,omitempty"`
	ThreadId          int    `json:"threadId,omitempty"`
	PreserveFocusHint bool   `json:"preserveFocusHint,omitempty"`
	Text              string `json:"text,omitempty"`
	AllThreadsStopped bool   `json:"allThreadsStopped,omitempty"`
}

type OutputEventBody struct {
	Category string `json:"category,omitempty"` // "console", "stdout", "stderr"
	Output   string `json:"output"`
}
