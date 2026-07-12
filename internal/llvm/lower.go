package llvm

import (
	"fmt"
	"strings"

	"github.com/whale-lang/whale/internal/ast"
	"github.com/whale-lang/whale/internal/types"
)

type lowerer struct {
	typeMap map[ast.Expr]types.Type
	enums   map[string]string // Maps variant name to Enum Name
	globals map[string]int64  // Maps global constant name to integer value
}

// Lower converts a Whale parser AST to an LLVM backend AST.
func Lower(file *ast.File, typeMap map[ast.Expr]types.Type) (*Program, error) {
	l := &lowerer{typeMap: typeMap, enums: make(map[string]string), globals: make(map[string]int64)}
	out := &Program{}
	for _, stmt := range file.Body {
		if structDecl, ok := stmt.(*ast.StructDecl); ok {
			params := make([]Param, len(structDecl.Fields))
			for i, p := range structDecl.Fields {
				typ, err := lowerType(p.Type)
				if err != nil {
					return nil, err
				}
				params[i] = Param{Name: p.Name, Type: typ}
			}
			out.Structs = append(out.Structs, &StructDecl{
				Name:   structDecl.Name,
				Fields: params,
				Packed: structDecl.Packed,
			})
		} else if enumDecl, ok := stmt.(*ast.EnumDecl); ok {
			variants := make([]EnumVariant, len(enumDecl.Variants))
			for i, v := range enumDecl.Variants {
				var t Type = ""
				if v.Type != nil {
					// Hack: v.Type is an ast.Expr (often *ast.Ident)
					if ident, ok := v.Type.(*ast.Ident); ok {
						var err error
						t, err = lowerType(ident.Name)
						if err != nil {
							return nil, err
						}
					}
				}
				variants[i] = EnumVariant{Name: v.Name, Type: t}
				l.enums[v.Name] = enumDecl.Name
			}
			out.Enums = append(out.Enums, &EnumDecl{
				Name:     enumDecl.Name,
				Variants: variants,
			})
		} else if letDecl, ok := stmt.(*ast.LetStmt); ok {
			val, err := l.lowerExpr(letDecl.Value)
			if err != nil {
				return nil, err
			}
			typ := inferLoweredType(val)
			global := &GlobalVarDecl{
				Name:     letDecl.Name,
				Type:     typ,
				ZeroInit: true,
			}
			if intLit, ok := letDecl.Value.(*ast.IntLit); ok {
				global.Value = intLit.Value
				global.ZeroInit = false
			} else if unary, ok := letDecl.Value.(*ast.UnaryOp); ok && unary.Op == "-" {
				if intLit, ok := unary.Expr.(*ast.IntLit); ok {
					global.Value = -intLit.Value
					global.ZeroInit = false
				}
			}
			out.GlobalVars = append(out.GlobalVars, global)
		}
	}
	for _, stmt := range file.Body {
		if fn, ok := stmt.(*ast.FnStmt); ok {
			llvmFn, err := l.lowerFunc(fn)
			if err != nil {
				return nil, err
			}
			out.Functions = append(out.Functions, llvmFn)
		} else if ext, ok := stmt.(*ast.ExternFnStmt); ok {
			llvmExt, err := l.lowerExternFunc(ext)
			if err != nil {
				return nil, err
			}
			out.Externs = append(out.Externs, llvmExt)
		}
	}
	return out, nil
}

func (l *lowerer) lowerExternFunc(ext *ast.ExternFnStmt) (*ExternFuncDecl, error) {
	params := make([]Param, len(ext.Params))
	for i, p := range ext.Params {
		typ, err := lowerType(p.Type)
		if err != nil {
			return nil, err
		}
		params[i] = Param{Name: p.Name, Type: typ}
	}

	retType := TypeVoid
	if ext.ReturnType != "" {
		t, err := lowerType(ext.ReturnType)
		if err != nil {
			return nil, err
		}
		retType = t
	}

	return &ExternFuncDecl{
		Name:       ext.Name,
		Params:     params,
		ReturnType: retType,
	}, nil
}

func (l *lowerer) lowerFunc(fn *ast.FnStmt) (*FuncDecl, error) {
	params := make([]Param, len(fn.Params))
	for i, p := range fn.Params {
		typ, err := lowerType(p.Type)
		if err != nil {
			return nil, err
		}
		params[i] = Param{Name: p.Name, Type: typ}
	}

	retType := TypeVoid
	if fn.ReturnType != "" {
		t, err := lowerType(fn.ReturnType)
		if err != nil {
			return nil, err
		}
		retType = t
	}

	body, err := l.lowerStmts(fn.Body.Body)
	if err != nil {
		return nil, err
	}

	return &FuncDecl{
		Name:       fn.Name,
		Params:     params,
		ReturnType: retType,
		Body:       body,
	}, nil
}

func (l *lowerer) lowerStmts(stmts []ast.Stmt) ([]Stmt, error) {
	var out []Stmt
	for i, s := range stmts {
		if ls, ok := s.(*ast.LetStmt); ok {
			fused, ok, err := l.tryFusePipeline(ls.Name, ls.Value)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, fused...)
				continue
			}
		} else if es, ok := s.(*ast.ExprStmt); ok {
			fused, ok, err := l.tryFusePipeline(fmt.Sprintf("expr_%d", i), es.Expr)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, fused...)
				continue
			}
		}

		ls, err := l.lowerStmt(s)
		if err != nil {
			return nil, err
		}
		out = append(out, ls)
	}
	return out, nil
}

func (l *lowerer) lowerStmt(s ast.Stmt) (Stmt, error) {
	switch node := s.(type) {
	case *ast.LetStmt:
		val, err := l.lowerExpr(node.Value)
		if err != nil {
			return nil, err
		}
		// Try to use the static type map first
		var typ Type
		if t, ok := l.typeMap[node.Value]; ok {
			typ = l.lowerTypeFromChecker(t)
		} else {
			typ = inferLoweredType(val) // fallback
		}

		if node.TypeAnn != "" {
			t, err := lowerType(node.TypeAnn)
			if err == nil {
				typ = t
			}
		}
		return &LetStmt{Name: node.Name, Type: typ, Value: val}, nil
	case *ast.AssignStmt:
		val, err := l.lowerExpr(node.Value)
		if err != nil {
			return nil, err
		}
		return &AssignStmt{Name: node.Name, Value: val}, nil
	case *ast.AssignDereferenceStmt:
		ptr, err := l.lowerExpr(node.Pointer)
		if err != nil {
			return nil, err
		}
		val, err := l.lowerExpr(node.Value)
		if err != nil {
			return nil, err
		}
		return &AssignDereferenceStmt{Pointer: ptr, Value: val}, nil
	case *ast.AssignFieldStmt:
		obj, err := l.lowerExpr(node.Object)
		if err != nil {
			return nil, err
		}
		val, err := l.lowerExpr(node.Value)
		if err != nil {
			return nil, err
		}
		return &AssignFieldStmt{Object: obj, Field: node.Field, Value: val}, nil
	case *ast.AssignIndexStmt:
		list, err := l.lowerExpr(node.List)
		if err != nil {
			return nil, err
		}
		idx, err := l.lowerExpr(node.Index)
		if err != nil {
			return nil, err
		}
		val, err := l.lowerExpr(node.Value)
		if err != nil {
			return nil, err
		}
		return &AssignIndexStmt{List: list, Index: idx, Value: val}, nil
	case *ast.ExprStmt:
		if id, ok := node.Expr.(*ast.Ident); ok && id.Name == "break" {
			return &BreakStmt{}, nil
		}
		if id, ok := node.Expr.(*ast.Ident); ok && id.Name == "continue" {
			return &ContinueStmt{}, nil
		}
		val, err := l.lowerExpr(node.Expr)
		if err != nil {
			return nil, err
		}
		return &ExprStmt{X: val}, nil
	case *ast.ReturnStmt:
		if node.Value == nil {
			return &ReturnStmt{Value: nil}, nil
		}
		val, err := l.lowerExpr(node.Value)
		if err != nil {
			return nil, err
		}
		return &ReturnStmt{Value: val}, nil
	case *ast.IfStmt:
		cond, err := l.lowerExpr(node.Condition)
		if err != nil {
			return nil, err
		}
		thenBlock, err := l.lowerStmts(node.Then.Body)
		if err != nil {
			return nil, err
		}
		var elseBlock []Stmt
		if node.Else != nil {
			if elseBlockStmt, ok := node.Else.(*ast.BlockStmt); ok {
				e, err := l.lowerStmts(elseBlockStmt.Body)
				if err != nil {
					return nil, err
				}
				elseBlock = e
			} else if elseIf, ok := node.Else.(*ast.IfStmt); ok {
				e, err := l.lowerStmt(elseIf)
				if err != nil {
					return nil, err
				}
				elseBlock = []Stmt{e}
			}
		}
		return &IfStmt{Cond: cond, Then: thenBlock, Else: elseBlock}, nil
	case *ast.WhileStmt:
		cond, err := l.lowerExpr(node.Condition)
		if err != nil {
			return nil, err
		}
		body, err := l.lowerStmts(node.Body.Body)
		if err != nil {
			return nil, err
		}
		return &WhileStmt{Cond: cond, Body: body}, nil
	case *ast.ArenaStmt:
		body, err := l.lowerStmts(node.Body.Body)
		if err != nil {
			return nil, err
		}
		return &ArenaStmt{Body: body}, nil
	case *ast.SpawnStmt:
		call, ok := node.Call.Callee.(*ast.Ident)
		if !ok {
			return nil, fmt.Errorf("spawn only supports direct identifiers")
		}
		args := make([]Expr, len(node.Call.Args))
		for i, a := range node.Call.Args {
			arg, err := l.lowerExpr(a)
			if err != nil {
				return nil, err
			}
			args[i] = arg
		}
		return &SpawnStmt{Call: &CallExpr{Callee: call.Name, Args: args}}, nil
	case *ast.ChanSendStmt:
		ch, err := l.lowerExpr(node.Chan)
		if err != nil {
			return nil, err
		}
		val, err := l.lowerExpr(node.Value)
		if err != nil {
			return nil, err
		}
		return &ChanSendStmt{Chan: ch, Value: val}, nil
	}
	return nil, fmt.Errorf("unsupported statement type %T", s)
}

func (l *lowerer) lowerExpr(e ast.Expr) (Expr, error) {
	switch node := e.(type) {
	case *ast.IntLit:
		return &IntLit{Value: node.Value}, nil
	case *ast.FloatLit:
		return &FloatLit{Value: node.Value}, nil
	case *ast.BoolLit:
		return &BoolLit{Value: node.Value}, nil
	case *ast.StringLit:
		return &StringLit{Value: node.Value}, nil
	case *ast.Ident:
		if enumName, ok := l.enums[node.Name]; ok {
			return &ConstructEnumExpr{EnumName: enumName, Variant: node.Name, Payload: nil}, nil
		}
		return &Ident{Name: node.Name}, nil
	case *ast.BinaryOp:
		left, err := l.lowerExpr(node.Left)
		if err != nil {
			return nil, err
		}
		right, err := l.lowerExpr(node.Right)
		if err != nil {
			return nil, err
		}
		return &BinaryExpr{Op: node.Op, Left: left, Right: right}, nil
	case *ast.CallExpr:
		ident, ok := node.Callee.(*ast.Ident)
		if !ok {
			return nil, fmt.Errorf("LLVM backend only supports calling by direct identifier")
		}

		if enumName, ok := l.enums[ident.Name]; ok {
			if len(node.Args) != 1 {
				return nil, fmt.Errorf("enum variant %s expects exactly 1 payload argument", ident.Name)
			}
			payload, err := l.lowerExpr(node.Args[0])
			if err != nil {
				return nil, err
			}
			return &ConstructEnumExpr{EnumName: enumName, Variant: ident.Name, Payload: payload}, nil
		}

		args := make([]Expr, len(node.Args))
		for i, a := range node.Args {
			arg, err := l.lowerExpr(a)
			if err != nil {
				return nil, err
			}
			args[i] = arg
		}
		return &CallExpr{Callee: ident.Name, Args: args}, nil
	case *ast.StructLit:
		fields := make(map[string]Expr)
		for k, v := range node.Fields {
			val, err := l.lowerExpr(v)
			if err != nil {
				return nil, err
			}
			fields[k] = val
		}
		return &StructLit{StructName: node.Type, Fields: fields}, nil
	case *ast.FieldAccess:
		obj, err := l.lowerExpr(node.Expr)
		if err != nil {
			return nil, err
		}
		return &FieldAccess{Object: obj, Field: node.Field}, nil
	case *ast.MatchExpr:
		expr, err := l.lowerExpr(node.Expr)
		if err != nil {
			return nil, err
		}
		arms := make([]MatchArm, len(node.Arms))
		for i, arm := range node.Arms {
			body, err := l.lowerExpr(arm.Body)
			if err != nil {
				return nil, err
			}
			arms[i] = MatchArm{
				Variant:    arm.Variant,
				Binding:    arm.Binding,
				Body:       body,
				IsCatchAll: arm.IsCatchAll,
			}
		}
		return &MatchExpr{Expr: expr, Arms: arms}, nil
	case *ast.ListLit:
		values := make([]Expr, len(node.Values))
		for i, v := range node.Values {
			val, err := l.lowerExpr(v)
			if err != nil {
				return nil, err
			}
			values[i] = val
		}

		elemType := TypeInt // Default
		if len(values) > 0 {
			elemType = inferLoweredType(values[0])
		}
		return &ListLit{ElementType: elemType, Values: values}, nil
	case *ast.IndexExpr:
		list, err := l.lowerExpr(node.Expr)
		if err != nil {
			return nil, err
		}
		idx, err := l.lowerExpr(node.Index)
		if err != nil {
			return nil, err
		}
		return &IndexExpr{List: list, Index: idx}, nil
	case *ast.AddressOfExpr:
		expr, err := l.lowerExpr(node.Expr)
		if err != nil {
			return nil, err
		}
		return &AddressOfExpr{Expr: expr}, nil
	case *ast.DereferenceExpr:
		expr, err := l.lowerExpr(node.Expr)
		if err != nil {
			return nil, err
		}
		return &DereferenceExpr{Expr: expr}, nil
	case *ast.ChanRecvExpr:
		ch, err := l.lowerExpr(node.Chan)
		if err != nil {
			return nil, err
		}
		return &ChanRecvExpr{Chan: ch}, nil
	case *ast.ErrorLit:
		msg, err := l.lowerExpr(node.Msg)
		if err != nil {
			return nil, err
		}
		return &ErrorLit{Msg: msg}, nil
	case *ast.TryExpr:
		expr, err := l.lowerExpr(node.Expr)
		if err != nil {
			return nil, err
		}
		return &TryExpr{Expr: expr}, nil
	case *ast.CastExpr:
		expr, err := l.lowerExpr(node.Expr)
		if err != nil {
			return nil, err
		}
		return &CastExpr{Expr: expr, TargetTy: node.TargetTy}, nil
	case *ast.AsmExpr:
		return &AsmExpr{Template: node.Template, Clobbers: node.Clobbers, SideEffect: node.SideEffect}, nil
	case *ast.UnaryOp:
		expr, err := l.lowerExpr(node.Expr)
		if err != nil {
			return nil, err
		}
		// Map unary ~ (bitwise not) to xor with -1
		if node.Op == "~" {
			return &BinaryExpr{Op: "^", Left: expr, Right: &IntLit{Value: -1}}, nil
		}
		// Map unary - to sub 0, x
		if node.Op == "-" {
			return &BinaryExpr{Op: "-", Left: &IntLit{Value: 0}, Right: expr}, nil
		}
		// Map ! to xor with 1 (boolean not)
		return &BinaryExpr{Op: "^", Left: expr, Right: &IntLit{Value: 1}}, nil
	default:
		return nil, fmt.Errorf("LLVM backend unsupported expression: %T", e)
	}
}

func lowerType(t string) (Type, error) {
	if t == "" {
		return TypeInt, nil // default
	}
	switch t {
	case "int":
		return TypeInt, nil
	case "float":
		return TypeFloat, nil
	case "bool":
		return TypeBool, nil
	case "string":
		return TypeString, nil
	}
	// Assume it's a custom struct type
	return Type(t), nil
}

func inferLoweredType(val Expr) Type {
	switch v := val.(type) {
	case *StructLit:
		return Type(v.StructName)
	case *IntLit:
		return TypeInt
	case *FloatLit:
		return TypeFloat
	case *BoolLit:
		return TypeBool
	case *StringLit:
		return TypeString
	case *FieldAccess:
		// Simplified: normally we'd look up the field type from the struct decl.
		return TypeInt
	case *ListLit:
		// We encode array types as strings like "[int]" for this simplified compiler
		return Type("[" + string(v.ElementType) + "]")
	case *IndexExpr:
		listTy := string(inferLoweredType(v.List))
		if strings.HasPrefix(listTy, "[") && strings.HasSuffix(listTy, "]") {
			return Type(listTy[1 : len(listTy)-1])
		}
		return TypeInt // fallback
	case *AllocArray:
		return Type("[" + string(v.ElementType) + "]")
	case *TryExpr:
		return TypeInt // simplified fallback for Ok payload
	case *ErrorLit:
		return Type("Result")
	default:
		return TypeInt
	}
}

// tryFusePipeline attempts to fuse a stream pipeline into a loop.
func (l *lowerer) tryFusePipeline(target string, expr ast.Expr) ([]Stmt, bool, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, false, nil
	}
	ident, ok := call.Callee.(*ast.Ident)
	if !ok || ident.Name != "collect" || len(call.Args) != 1 {
		return nil, false, nil
	}

	var ops []ast.Expr
	curr := call.Args[0]
	var listExpr ast.Expr

	for {
		c, ok := curr.(*ast.CallExpr)
		if !ok {
			return nil, false, fmt.Errorf("invalid pipeline")
		}
		id, ok := c.Callee.(*ast.Ident)
		if !ok {
			return nil, false, fmt.Errorf("invalid pipeline callee")
		}

		if id.Name == "stream" {
			listExpr = c.Args[0]
			break
		} else if id.Name == "map" || id.Name == "filter" {
			ops = append(ops, curr)
			curr = c.Args[0]
		} else {
			return nil, false, fmt.Errorf("unsupported stream operation %s", id.Name)
		}
	}

	// Reverse ops so they are in execution order: map, filter, etc.
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}

	var out []Stmt
	list, err := l.lowerExpr(listExpr)
	if err != nil {
		return nil, false, err
	}

	listTy := inferLoweredType(list)
	elemTy := TypeInt
	listTyStr := string(listTy)
	if strings.HasPrefix(listTyStr, "[") {
		elemTy = Type(listTyStr[1 : len(listTyStr)-1])
	}

	srcName := "_src_" + target
	lenName := "_len_" + target
	iName := "_i_" + target
	out_iName := "_out_i_" + target

	out = append(out, &LetStmt{Name: srcName, Type: listTy, Value: list})
	out = append(out, &LetStmt{Name: lenName, Type: TypeInt, Value: &LenExpr{List: &Ident{Name: srcName}}})

	outTy := elemTy
	outArrTy := Type("[" + string(outTy) + "]")

	if !strings.HasPrefix(target, "expr_") {
		out = append(out, &LetStmt{Name: target, Type: outArrTy, Value: &AllocArray{ElementType: outTy, Length: &Ident{Name: lenName}}})
	}
	out = append(out, &LetStmt{Name: iName, Type: TypeInt, Value: &IntLit{Value: 0}})
	out = append(out, &LetStmt{Name: out_iName, Type: TypeInt, Value: &IntLit{Value: 0}})

	var loopBody []Stmt
	itemName := "_item_" + target
	loopBody = append(loopBody, &LetStmt{
		Name:  itemName,
		Type:  elemTy,
		Value: &IndexExpr{List: &Ident{Name: srcName}, Index: &Ident{Name: iName}},
	})

	var pipelineStmts []Stmt
	currVal := Expr(&Ident{Name: itemName})

	for i := 0; i < len(ops); i++ {
		op := ops[i].(*ast.CallExpr)
		opName := op.Callee.(*ast.Ident).Name
		closure := op.Args[1].(*ast.FnLit)
		paramName := closure.Params[0].Name

		pipelineStmts = append(pipelineStmts, &LetStmt{Name: paramName, Type: elemTy, Value: currVal})

		if opName == "map" {
			mappedVal, err := l.lowerExpr(closure.Body.Expr)
			if err != nil {
				return nil, false, err
			}
			currVal = mappedVal
		} else if opName == "filter" {
			filterCond, err := l.lowerExpr(closure.Body.Expr)
			if err != nil {
				return nil, false, err
			}

			// For filter, the *rest* of the pipeline (including out assignment)
			// must be wrapped in an IfStmt!
			var restOps []ast.Expr
			for j := i + 1; j < len(ops); j++ {
				restOps = append(restOps, ops[j])
			}

			// Recursively call a helper to process the rest?
			// Actually, it's easier to just build the final assignment statements now
			// and then wrap everything.
			var innerStmts []Stmt

			// Map/Filter the rest:
			var innerCurrVal Expr = currVal
			for _, restOpExpr := range restOps {
				restOp := restOpExpr.(*ast.CallExpr)
				restOpName := restOp.Callee.(*ast.Ident).Name
				restClosure := restOp.Args[1].(*ast.FnLit)
				restParamName := restClosure.Params[0].Name

				innerStmts = append(innerStmts, &LetStmt{Name: restParamName, Type: elemTy, Value: innerCurrVal})
				if restOpName == "map" {
					mapped, err := l.lowerExpr(restClosure.Body.Expr)
					if err != nil {
						return nil, false, err
					}
					innerCurrVal = mapped
				} else if restOpName == "filter" {
					panic("multiple filters in one pipeline not yet supported by basic fusion")
				}
			}

			if !strings.HasPrefix(target, "expr_") {
				innerStmts = append(innerStmts, &AssignIndexStmt{
					List:  &Ident{Name: target},
					Index: &Ident{Name: out_iName},
					Value: innerCurrVal,
				})
			}
			innerStmts = append(innerStmts, &AssignStmt{
				Name:  out_iName,
				Value: &BinaryExpr{Op: "+", Left: &Ident{Name: out_iName}, Right: &IntLit{Value: 1}},
			})

			pipelineStmts = append(pipelineStmts, &IfStmt{
				Cond: filterCond,
				Then: innerStmts,
			})

			// We break because we processed the rest of the ops inside the IfStmt
			currVal = nil
			break
		}
	}

	if currVal != nil {
		if !strings.HasPrefix(target, "expr_") {
			pipelineStmts = append(pipelineStmts, &AssignIndexStmt{
				List:  &Ident{Name: target},
				Index: &Ident{Name: out_iName},
				Value: currVal,
			})
		}
		pipelineStmts = append(pipelineStmts, &AssignStmt{
			Name:  out_iName,
			Value: &BinaryExpr{Op: "+", Left: &Ident{Name: out_iName}, Right: &IntLit{Value: 1}},
		})
	}

	loopBody = append(loopBody, pipelineStmts...)
	loopBody = append(loopBody, &AssignStmt{
		Name:  iName,
		Value: &BinaryExpr{Op: "+", Left: &Ident{Name: iName}, Right: &IntLit{Value: 1}},
	})

	out = append(out, &WhileStmt{
		Cond: &BinaryExpr{Op: "<", Left: &Ident{Name: iName}, Right: &Ident{Name: lenName}},
		Body: loopBody,
	})

	return out, true, nil
}

func (l *lowerer) lowerTypeFromChecker(t types.Type) Type {
	switch t := t.(type) {
	case types.TInt:
		return TypeInt
	case types.TFloat:
		return TypeFloat
	case types.TBool:
		return TypeBool
	case types.TStruct:
		return Type(t.Name)
	case types.TList:
		return Type("[" + string(l.lowerTypeFromChecker(t.Elem)) + "]")
	case types.TPointer:
		return Type("*" + string(l.lowerTypeFromChecker(t.Elem)))
	}
	return TypeInt // fallback
}
