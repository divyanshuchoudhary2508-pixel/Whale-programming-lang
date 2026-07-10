package dap

import (
	"encoding/json"

	"github.com/whale-lang/whale/internal/ast"
	"github.com/whale-lang/whale/internal/interp"
)

// Debugger implements interp.DebugHooks and coordinates with the DAP Server.
type Debugger struct {
	server       *Server
	breakpoints  map[string][]int // path -> lines
	pauseChan    chan struct{}
	resumeChan   chan struct{}
	
	// State
	currentEnv   *interp.Environment
	currentNode  ast.Node
}

func NewDebugger(s *Server) *Debugger {
	return &Debugger{
		server:      s,
		breakpoints: make(map[string][]int),
		pauseChan:   make(chan struct{}),
		resumeChan:  make(chan struct{}),
	}
}

// BeforeNode is called by the VM before evaluating any AST node.
func (d *Debugger) BeforeNode(node ast.Node, env *interp.Environment) {
	d.currentEnv = env
	d.currentNode = node
	
	pos := ast.Position{}
	switch n := node.(type) {
	case *ast.LetStmt:
		pos = n.Pos
	case *ast.AssignStmt:
		pos = n.Pos
	case *ast.ExprStmt:
		pos = n.Pos
	case *ast.IfStmt:
		pos = n.Pos
	case *ast.WhileStmt:
		pos = n.Pos
	case *ast.ForStmt:
		pos = n.Pos
	case *ast.ReturnStmt:
		pos = n.Pos
	}
	
	if pos.Line == 0 {
		return // not a statment with position
	}
	
	// Check if we hit a breakpoint (we assume all files for now since Whale only does single files mostly)
	hit := false
	for _, bps := range d.breakpoints {
		for _, line := range bps {
			if line == pos.Line {
				hit = true
				break
			}
		}
	}

	if hit {
		// Notify VSCode that we stopped on a breakpoint
		body, _ := json.Marshal(StoppedEventBody{
			Reason:            "breakpoint",
			ThreadId:          1,
			AllThreadsStopped: true,
		})
		d.server.transport.SendEvent(Event{
			ProtocolMessage: ProtocolMessage{Seq: 0, Type: "event"},
			Event:           "stopped",
			Body:            body,
		})
		
		// Block execution until VSCode sends a "continue", "next", etc.
		<-d.resumeChan
	}
}

// Pause forces the debugger to pause on the next node.
func (d *Debugger) Pause() {
	// For step-over / pause functionality (omitted for MVP simplicity)
}

// Resume unblocks execution.
func (d *Debugger) Resume() {
	d.resumeChan <- struct{}{}
}
