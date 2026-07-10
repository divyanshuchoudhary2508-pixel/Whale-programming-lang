package ast

import "reflect"

// Clone recursively deep-copies an AST node.
func Clone(node Node) Node {
	if node == nil {
		return nil
	}
	val := reflect.ValueOf(node)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	} else {
		// Only clone pointers
		return node
	}
	
	cloned := reflect.New(val.Type())
	cloneRecursive(val, cloned.Elem())
	return cloned.Interface().(Node)
}

func cloneRecursive(src, dst reflect.Value) {
	for i := 0; i < src.NumField(); i++ {
		srcField := src.Field(i)
		dstField := dst.Field(i)
		
		switch srcField.Kind() {
		case reflect.Ptr:
			if !srcField.IsNil() {
				newPtr := reflect.New(srcField.Type().Elem())
				cloneRecursive(srcField.Elem(), newPtr.Elem())
				dstField.Set(newPtr)
			}
		case reflect.Slice:
			if !srcField.IsNil() {
				newSlice := reflect.MakeSlice(srcField.Type(), srcField.Len(), srcField.Cap())
				for j := 0; j < srcField.Len(); j++ {
					elem := srcField.Index(j)
					// We only handle interface elements or struct elements
					if elem.Kind() == reflect.Interface && !elem.IsNil() {
						clonedNode := Clone(elem.Interface().(Node))
						newSlice.Index(j).Set(reflect.ValueOf(clonedNode))
					} else if elem.Kind() == reflect.Ptr && !elem.IsNil() {
						newPtr := reflect.New(elem.Type().Elem())
						cloneRecursive(elem.Elem(), newPtr.Elem())
						newSlice.Index(j).Set(newPtr)
					} else if elem.Kind() == reflect.Struct {
					    // e.g. ast.Param
					    cloneRecursive(elem, newSlice.Index(j))
					} else {
						newSlice.Index(j).Set(elem)
					}
				}
				dstField.Set(newSlice)
			}
		case reflect.Interface:
			if !srcField.IsNil() {
				clonedNode := Clone(srcField.Interface().(Node))
				dstField.Set(reflect.ValueOf(clonedNode))
			}
		default:
			dstField.Set(srcField)
		}
	}
}
