package dap

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/whale-lang/whale/internal/ast"
	"github.com/whale-lang/whale/internal/interp"
)

// Server handles DAP JSON-RPC communication.
type Server struct {
	transport *Transport
	debugger  *Debugger
}

// NewServer creates a new DAP server over stdio.
func NewServer() *Server {
	return &Server{
		transport: NewTransport(os.Stdin, os.Stdout),
	}
}

// Start begins the DAP message loop.
func (s *Server) Start() error {
	for {
		req, err := s.transport.ReadRequest()
		if err != nil {
			if err.Error() == "EOF" {
				return nil
			}
			return err
		}

		s.handleRequest(req)
	}
}

func (s *Server) handleRequest(req Request) {
	// Send an empty successful response by default
	resp := Response{
		ProtocolMessage: ProtocolMessage{Seq: 0, Type: "response"},
		RequestSeq:      req.Seq,
		Success:         true,
		Command:         req.Command,
	}

	switch req.Command {
	case "initialize":
		var args InitializeRequestArguments
		if err := json.Unmarshal(req.Arguments, &args); err != nil {
			s.sendError(req, err)
			return
		}
		
		caps := Capabilities{
			SupportsConfigurationDoneRequest: true,
		}
		resp.Body, _ = json.Marshal(caps)
		s.transport.SendResponse(resp)
		
		// VSCode expects an "initialized" event after responding to initialize
		s.transport.SendEvent(Event{
			ProtocolMessage: ProtocolMessage{Seq: 0, Type: "event"},
			Event:           "initialized",
		})
		return
		
	case "launch":
		var args map[string]interface{}
		json.Unmarshal(req.Arguments, &args)
		prog := args["program"].(string)

		// Start the VM in a goroutine
		d := NewDebugger(s)
		s.debugger = d
		
		go func() {
			out, errs := interp.RunFileWithDebug(prog, d)
			
			// Print output via OutputEvents
			if out != "" {
				body, _ := json.Marshal(OutputEventBody{Category: "stdout", Output: out})
				s.transport.SendEvent(Event{
					ProtocolMessage: ProtocolMessage{Type: "event"},
					Event:           "output",
					Body:            body,
				})
			}
			for _, e := range errs {
				body, _ := json.Marshal(OutputEventBody{Category: "stderr", Output: e + "\n"})
				s.transport.SendEvent(Event{
					ProtocolMessage: ProtocolMessage{Type: "event"},
					Event:           "output",
					Body:            body,
				})
			}
			
			// Terminate
			s.transport.SendEvent(Event{
				ProtocolMessage: ProtocolMessage{Type: "event"},
				Event:           "terminated",
			})
			os.Exit(0)
		}()
		s.transport.SendResponse(resp)
		
	case "setBreakpoints":
		var args SetBreakpointsArguments
		json.Unmarshal(req.Arguments, &args)
		
		var bps []Breakpoint
		var lines []int
		for i, sb := range args.Breakpoints {
			lines = append(lines, sb.Line)
			bps = append(bps, Breakpoint{
				Id:       i,
				Verified: true,
				Line:     sb.Line,
			})
		}
		if s.debugger != nil {
			s.debugger.breakpoints[args.Source.Path] = lines
		}
		
		resp.Body, _ = json.Marshal(SetBreakpointsResponseBody{Breakpoints: bps})
		s.transport.SendResponse(resp)
		
	case "continue", "next", "stepIn", "stepOut":
		if s.debugger != nil {
			s.debugger.Resume()
		}
		s.transport.SendResponse(resp)

	case "stackTrace":
		frames := []StackFrame{}
		if s.debugger != nil && s.debugger.currentNode != nil {
			pos := ast.Position{}
			// We only have the current node's position, full stack requires interp refactor
			switch n := s.debugger.currentNode.(type) {
			case *ast.LetStmt: pos = n.Pos
			case *ast.AssignStmt: pos = n.Pos
			case *ast.ExprStmt: pos = n.Pos
			case *ast.IfStmt: pos = n.Pos
			case *ast.WhileStmt: pos = n.Pos
			case *ast.ForStmt: pos = n.Pos
			case *ast.ReturnStmt: pos = n.Pos
			}
			frames = append(frames, StackFrame{
				Id: 1, Name: "main", Line: pos.Line, Column: 0,
			})
		}
		resp.Body, _ = json.Marshal(map[string]interface{}{"stackFrames": frames, "totalFrames": len(frames)})
		s.transport.SendResponse(resp)

	case "scopes":
		scopes := []Scope{}
		if s.debugger != nil {
			env := s.debugger.currentEnv
			ref := 1
			for env != nil {
				name := "Local"
				if env.Parent == nil {
					name = "Global"
				} else if ref > 1 {
					name = fmt.Sprintf("Closure %d", ref)
				}
				scopes = append(scopes, Scope{
					Name:               name,
					VariablesReference: ref,
					Expensive:          false,
				})
				env = env.Parent
				ref++
			}
		}
		resp.Body, _ = json.Marshal(map[string]interface{}{"scopes": scopes})
		s.transport.SendResponse(resp)

	case "variables":
		var args map[string]interface{}
		json.Unmarshal(req.Arguments, &args)
		ref := int(args["variablesReference"].(float64))
		
		vars := []Variable{}
		if s.debugger != nil {
			env := s.debugger.currentEnv
			for i := 1; i < ref && env != nil; i++ {
				env = env.Parent
			}
			if env != nil {
				for name, val := range env.Values {
					vars = append(vars, Variable{
						Name:  name,
						Value: val.String(),
						Type:  fmt.Sprintf("%T", val),
					})
				}
			}
		}
		resp.Body, _ = json.Marshal(map[string]interface{}{"variables": vars})
		s.transport.SendResponse(resp)
		
	case "configurationDone":
		// VSCode finished setting up breakpoints
		s.transport.SendResponse(resp)
		
	case "threads":
		// We only have one thread
		threads := map[string]interface{}{
			"threads": []Thread{{ID: 1, Name: "Main Thread"}},
		}
		resp.Body, _ = json.Marshal(threads)
		s.transport.SendResponse(resp)

	case "disconnect":
		s.transport.SendResponse(resp)
		os.Exit(0)
		
	default:
		// Unsupported command
		resp.Success = false
		resp.Message = fmt.Sprintf("unsupported command: %s", req.Command)
		s.transport.SendResponse(resp)
	}
}

func (s *Server) sendError(req Request, err error) {
	s.transport.SendResponse(Response{
		ProtocolMessage: ProtocolMessage{Seq: 0, Type: "response"},
		RequestSeq:      req.Seq,
		Success:         false,
		Command:         req.Command,
		Message:         err.Error(),
	})
}
