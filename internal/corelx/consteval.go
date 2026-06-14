package corelx

import "fmt"

// evalConstExpr evaluates a compile-time constant expression. Supported:
// integer literals, true/false (1/0), references to previously defined
// constants, unary minus / bitwise not, and integer binary arithmetic.
func evalConstExpr(expr Expr, consts map[string]int64) (int64, error) {
	switch e := expr.(type) {
	case *NumberExpr:
		return int64(e.Value), nil
	case *BoolExpr:
		if e.Value {
			return 1, nil
		}
		return 0, nil
	case *IdentExpr:
		if v, ok := consts[e.Name]; ok {
			return v, nil
		}
		return 0, fmt.Errorf("constant expression references %q, which is not a previously defined constant", e.Name)
	case *UnaryExpr:
		v, err := evalConstExpr(e.Operand, consts)
		if err != nil {
			return 0, err
		}
		switch e.Op {
		case TOKEN_MINUS:
			return -v, nil
		case TOKEN_TILDE:
			return ^v, nil
		default:
			return 0, fmt.Errorf("unsupported unary operator in constant expression")
		}
	case *BinaryExpr:
		l, err := evalConstExpr(e.Left, consts)
		if err != nil {
			return 0, err
		}
		r, err := evalConstExpr(e.Right, consts)
		if err != nil {
			return 0, err
		}
		switch e.Op {
		case TOKEN_PLUS:
			return l + r, nil
		case TOKEN_MINUS:
			return l - r, nil
		case TOKEN_STAR:
			return l * r, nil
		case TOKEN_SLASH:
			if r == 0 {
				return 0, fmt.Errorf("division by zero in constant expression")
			}
			return l / r, nil
		case TOKEN_PERCENT:
			if r == 0 {
				return 0, fmt.Errorf("modulo by zero in constant expression")
			}
			return l % r, nil
		case TOKEN_AMPERSAND:
			return l & r, nil
		case TOKEN_PIPE:
			return l | r, nil
		case TOKEN_CARET:
			return l ^ r, nil
		case TOKEN_LSHIFT:
			return l << uint(r&63), nil
		case TOKEN_RSHIFT:
			return l >> uint(r&63), nil
		default:
			return 0, fmt.Errorf("unsupported binary operator in constant expression")
		}
	default:
		return 0, fmt.Errorf("expression is not a compile-time constant")
	}
}

// foldProgramConstsTyped evaluates all top-level const declarations in
// order, tracking which constants are fixed-point. Fixed const arithmetic
// supports + - between fixed values, fixed*fixed (rescaled), and
// fixed/fixed (rescaled); mixing fixed and int is an error (charter D4).
func foldProgramConstsTyped(prog *Program) (map[string]int64, map[string]bool, error) {
	consts := make(map[string]int64)
	fixed := make(map[string]bool)
	for _, c := range prog.Consts {
		if _, dup := consts[c.Name]; dup {
			return nil, nil, fmt.Errorf("line %d: duplicate constant %q", c.Position.Line, c.Name)
		}
		v, isFixed, err := evalConstExprTyped(c.Value, consts, fixed)
		if err != nil {
			return nil, nil, fmt.Errorf("line %d: const %s: %w", c.Position.Line, c.Name, err)
		}
		consts[c.Name] = v
		fixed[c.Name] = isFixed
	}
	return consts, fixed, nil
}

// evalConstExprTyped evaluates a constant expression and reports whether
// the result is fixed-point.
func evalConstExprTyped(expr Expr, consts map[string]int64, fixed map[string]bool) (int64, bool, error) {
	switch e := expr.(type) {
	case *NumberExpr:
		if e.IsFixed {
			return int64(int16(uint16(e.Value))), true, nil
		}
		return int64(e.Value), false, nil
	case *IdentExpr:
		if v, ok := consts[e.Name]; ok {
			return v, fixed[e.Name], nil
		}
		return 0, false, fmt.Errorf("constant expression references %q, which is not a previously defined constant", e.Name)
	case *UnaryExpr:
		v, isF, err := evalConstExprTyped(e.Operand, consts, fixed)
		if err != nil {
			return 0, false, err
		}
		switch e.Op {
		case TOKEN_MINUS:
			return -v, isF, nil
		case TOKEN_TILDE:
			return ^v, isF, nil
		}
		return 0, false, fmt.Errorf("unsupported unary operator in constant expression")
	case *BinaryExpr:
		l, lf, err := evalConstExprTyped(e.Left, consts, fixed)
		if err != nil {
			return 0, false, err
		}
		r, rf, err := evalConstExprTyped(e.Right, consts, fixed)
		if err != nil {
			return 0, false, err
		}
		if lf != rf {
			return 0, false, fmt.Errorf("cannot mix fixed and int in a constant expression — convert explicitly")
		}
		isF := lf
		switch e.Op {
		case TOKEN_PLUS:
			return l + r, isF, nil
		case TOKEN_MINUS:
			return l - r, isF, nil
		case TOKEN_STAR:
			if isF {
				// Must match the runtime __fixmul routine bit-for-bit: it
				// works on magnitudes (toward-zero truncation) then re-applies
				// sign, NOT Go's arithmetic >> (which rounds toward -inf and
				// diverges from hardware on negative truncating products).
				return fixmulFold(l, r), true, nil
			}
			return l * r, false, nil
		case TOKEN_SLASH:
			if r == 0 {
				return 0, false, fmt.Errorf("division by zero in constant expression")
			}
			if isF {
				return (l << 8) / r, true, nil
			}
			return l / r, false, nil
		default:
			if isF {
				return 0, false, fmt.Errorf("operator not supported on fixed constants")
			}
		}
		// Fall through to untyped integer evaluation for bitwise ops.
		v, err2 := evalConstExpr(expr, consts)
		return v, false, err2
	default:
		v, err := evalConstExpr(expr, consts)
		return v, false, err
	}
}

// fixmulFold computes an 8.8 fixed multiply identically to the runtime
// __fixmul routine: operate on magnitudes (toward-zero), then re-sign. This
// keeps compile-time constant folding bit-identical to on-hardware results.
func fixmulFold(a, b int64) int64 {
	neg := (a < 0) != (b < 0)
	ua, ub := a, b
	if ua < 0 {
		ua = -ua
	}
	if ub < 0 {
		ub = -ub
	}
	res := (ua * ub) >> 8 // toward zero (both operands non-negative)
	res &= 0xFFFF         // 16-bit datapath truncation
	if neg {
		res = (-res) & 0xFFFF
	}
	return int64(int16(uint16(res)))
}
