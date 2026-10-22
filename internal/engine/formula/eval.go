package formula

import (
	"math"

	"dicegirl/internal/apperr"
)

type value struct {
	i int
	b bool
}

func eval(n node, env Env) (value, error) {
	switch n := n.(type) {
	case *numberNode:
		return value{i: n.value}, nil
	case *varNode:
		v, ok := env[n.name]
		if !ok {
			return value{}, apperr.New("formula.eval", apperr.InvalidArgument,
				"missing value for variable "+n.name)
		}
		return value{i: v}, nil
	case *unaryNode:
		v, err := eval(n.operand, env)
		if err != nil {
			return value{}, err
		}
		switch n.op {
		case "-":
			r, err := negInt(v.i)
			if err != nil {
				return value{}, err
			}
			return value{i: r}, nil
		case "!":
			return value{b: !v.b}, nil
		}
	case *binaryNode:
		return evalBinary(n, env)
	case *callNode:
		return evalCall(n, env)
	}
	return value{}, apperr.New("formula.eval", apperr.Internal, "unexpected AST node")
}

func evalBinary(n *binaryNode, env Env) (value, error) {
	// && and || short-circuit and operate on booleans.
	if n.op == "&&" || n.op == "||" {
		lv, err := eval(n.left, env)
		if err != nil {
			return value{}, err
		}
		if n.op == "&&" {
			if !lv.b {
				return value{b: false}, nil
			}
		} else if lv.b {
			return value{b: true}, nil
		}
		rv, err := eval(n.right, env)
		if err != nil {
			return value{}, err
		}
		return value{b: rv.b}, nil
	}

	lv, err := eval(n.left, env)
	if err != nil {
		return value{}, err
	}
	rv, err := eval(n.right, env)
	if err != nil {
		return value{}, err
	}
	switch n.op {
	case "+", "-", "*":
		r, err := arith(n.op, lv.i, rv.i)
		if err != nil {
			return value{}, err
		}
		return value{i: r}, nil
	case "/":
		if rv.i == 0 {
			return value{}, apperr.New("formula.eval", apperr.DivideByZero, "division by zero")
		}
		return value{i: floorDiv(lv.i, rv.i)}, nil
	case "<", "<=", ">", ">=", "==", "!=":
		return value{b: compare(n.op, lv.i, rv.i)}, nil
	}
	return value{}, apperr.New("formula.eval", apperr.Internal, "unknown operator "+n.op)
}

// floorDiv is mathematical floor division (unlike Go's truncating /). Game
// values are non-negative, where it matches truncation; it stays well-defined
// for negative intermediate results.
func floorDiv(a, b int) int {
	q := a / b
	r := a % b
	if r != 0 && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func compare(op string, a, b int) bool {
	switch op {
	case "<":
		return a < b
	case "<=":
		return a <= b
	case ">":
		return a > b
	case ">=":
		return a >= b
	case "==":
		return a == b
	case "!=":
		return a != b
	}
	return false
}

func arith(op string, a, b int) (int, error) {
	ai, bi := int64(a), int64(b)
	var r int64
	switch op {
	case "+":
		r = ai + bi
	case "-":
		r = ai - bi
	case "*":
		r = ai * bi
	}
	if r > math.MaxInt64 || r < math.MinInt64 {
		return 0, apperr.New("formula.eval", apperr.RollError, "arithmetic overflow")
	}
	return int(r), nil
}

func negInt(a int) (int, error) {
	if int64(a) == math.MinInt64 {
		return 0, apperr.New("formula.eval", apperr.RollError, "arithmetic overflow")
	}
	return -a, nil
}

func evalCall(n *callNode, env Env) (value, error) {
	values := make([]int, len(n.args))
	for i, a := range n.args {
		v, err := eval(a, env)
		if err != nil {
			return value{}, err
		}
		values[i] = v.i
	}
	switch n.name {
	case "abs":
		if values[0] < 0 {
			return value{i: -values[0]}, nil
		}
		return value{i: values[0]}, nil
	case "min":
		m := values[0]
		for _, v := range values[1:] {
			if v < m {
				m = v
			}
		}
		return value{i: m}, nil
	case "max":
		m := values[0]
		for _, v := range values[1:] {
			if v > m {
				m = v
			}
		}
		return value{i: m}, nil
	}
	return value{}, apperr.New("formula.eval", apperr.ConfigInvalid, "unknown function "+n.name)
}
