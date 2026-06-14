package corelx

// Expression type names used by the charter-D4 numeric model.
// "int" covers int/u8/u16 storage (all integer arithmetic);
// "fixed" is 8.8 fixed point; "unknown" suppresses checks.
const (
	typeInt     = "int"
	typeFixed   = "fixed"
	typeBool    = "bool"
	typeString  = "string"
	typeUnknown = "unknown"
)

// arithTypeForName maps a declared storage type to its arithmetic type.
func arithTypeForName(name string) string {
	switch name {
	case "int", "i16", "u8", "u16":
		return typeInt
	case "fixed":
		return typeFixed
	case "bool":
		return typeBool
	default:
		return typeUnknown
	}
}

// typeOf infers the arithmetic type of an expression for charter-D4
// fixed/int mixing checks and fixed multiply scaling. Returns typeUnknown
// when the type cannot be determined (checks are then skipped).
func (cg *CodeGenerator) typeOf(expr Expr) string {
	switch e := expr.(type) {
	case *NumberExpr:
		if e.IsFixed {
			return typeFixed
		}
		return typeInt
	case *BoolExpr:
		return typeBool
	case *StringExpr:
		return typeString
	case *IdentExpr:
		if v, ok := cg.variables[e.Name]; ok {
			if v.VarType != "" {
				return arithTypeForName(v.VarType)
			}
			return typeUnknown
		}
		if cg.constFixed[e.Name] {
			return typeFixed
		}
		if _, ok := cg.consts[e.Name]; ok {
			return typeInt
		}
		if g, ok := cg.globals[e.Name]; ok {
			return arithTypeForName(g.VarType)
		}
		return typeUnknown
	case *UnaryExpr:
		switch e.Op {
		case TOKEN_MINUS, TOKEN_TILDE:
			return cg.typeOf(e.Operand)
		case TOKEN_NOT:
			return typeBool
		}
		return typeUnknown
	case *IndexExpr:
		if ident, ok := e.Array.(*IdentExpr); ok {
			if g, ok := cg.globals[ident.Name]; ok && g.ArrayLen > 0 {
				return arithTypeForName(g.VarType)
			}
		}
		return typeUnknown
	case *BinaryExpr:
		switch e.Op {
		case TOKEN_PLUS, TOKEN_MINUS, TOKEN_STAR, TOKEN_SLASH, TOKEN_PERCENT,
			TOKEN_AMPERSAND, TOKEN_PIPE, TOKEN_CARET, TOKEN_LSHIFT, TOKEN_RSHIFT:
			lt, rt := cg.typeOf(e.Left), cg.typeOf(e.Right)
			if lt == typeFixed || rt == typeFixed {
				return typeFixed
			}
			if lt == typeUnknown && rt == typeUnknown {
				return typeUnknown
			}
			return typeInt
		default:
			return typeBool
		}
	case *CallExpr:
		name := callFuncName(e)
		switch name {
		case "int":
			return typeInt
		case "fixed":
			return typeFixed
		}
		if fn := cg.findFunction(name); fn != nil {
			if named, ok := fn.ReturnType.(*NamedType); ok {
				return arithTypeForName(named.Name)
			}
			return typeUnknown
		}
		return typeUnknown
	case *MemberExpr:
		// Struct field access: resolve the field's declared type.
		if ident, ok := e.Object.(*IdentExpr); ok {
			if v, exists := cg.variables[ident.Name]; exists && v.StructType != "" {
				return arithTypeForName(cg.structFieldTypeName(v.StructType, e.Member))
			}
		}
		return typeUnknown
	default:
		return typeUnknown
	}
}

// callFuncName extracts the flat or dotted function name from a call.
func callFuncName(call *CallExpr) string {
	if ident, ok := call.Func.(*IdentExpr); ok {
		return ident.Name
	}
	if member, ok := call.Func.(*MemberExpr); ok {
		if obj, ok := member.Object.(*IdentExpr); ok {
			return obj.Name + "." + member.Member
		}
	}
	return ""
}

// structFieldTypeName returns the declared type name of a struct field, or "".
func (cg *CodeGenerator) structFieldTypeName(structName, field string) string {
	for _, td := range cg.program.Types {
		if td.Name != structName {
			continue
		}
		if st, ok := td.Type.(*StructType); ok {
			for _, f := range st.Fields {
				if f.Name == field {
					if named, ok := f.Type.(*NamedType); ok {
						return named.Name
					}
				}
			}
		}
	}
	return ""
}

// checkNumericMix returns an error when fixed and int are mixed in an
// arithmetic operation without an explicit conversion (charter D4).
func (cg *CodeGenerator) checkNumericMix(op string, left, right Expr) error {
	lt, rt := cg.typeOf(left), cg.typeOf(right)
	if (lt == typeFixed && rt == typeInt) || (lt == typeInt && rt == typeFixed) {
		return mixError(op)
	}
	return nil
}

func mixError(op string) error {
	return &numericMixError{op: op}
}

type numericMixError struct{ op string }

func (e *numericMixError) Error() string {
	return "cannot mix fixed and int operands in '" + e.op + "' — convert explicitly with int(x) or fixed(x)"
}

// tokenOpName renders an operator token for error messages.
func tokenOpName(op TokenType) string {
	switch op {
	case TOKEN_PLUS:
		return "+"
	case TOKEN_MINUS:
		return "-"
	case TOKEN_STAR:
		return "*"
	case TOKEN_SLASH:
		return "/"
	case TOKEN_PERCENT:
		return "%"
	case TOKEN_EQUAL_EQUAL:
		return "=="
	case TOKEN_BANG_EQUAL:
		return "!="
	case TOKEN_LESS:
		return "<"
	case TOKEN_LESS_EQUAL:
		return "<="
	case TOKEN_GREATER:
		return ">"
	case TOKEN_GREATER_EQUAL:
		return ">="
	}
	return "?"
}
