package formula

import (
	"fmt"
	"sort"
	"strings"

	"dicegirl/internal/apperr"
)

// node is one AST node of the formula language.
type node interface {
	nodeMarker()
}

type numberNode struct {
	value int
}

type varNode struct {
	name string
}

// callNode is a whitelisted function call (abs/min/max).
type callNode struct {
	name string
	args []node
}

// unaryNode holds "-" (integer operand) or "!" (boolean operand).
type unaryNode struct {
	op      string
	operand node
}

// binaryNode holds arithmetic, comparison and logical operators.
type binaryNode struct {
	op          string
	left, right node
}

func (*numberNode) nodeMarker() {}
func (*varNode) nodeMarker()    {}
func (*callNode) nodeMarker()   {}
func (*unaryNode) nodeMarker()  {}
func (*binaryNode) nodeMarker() {}

// arity of whitelisted functions: -1 means "one or more".
var functions = map[string]struct {
	kind Kind
	args int
}{
	"abs": {KindInt, 1},
	"min": {KindInt, -1},
	"max": {KindInt, -1},
}

// typeOf computes the static result type or reports a type error. All
// variables are integers; booleans can only come from comparisons/logic.
func typeOf(n node) (Kind, error) {
	switch n := n.(type) {
	case *numberNode, *varNode:
		return KindInt, nil
	case *unaryNode:
		k, err := typeOf(n.operand)
		if err != nil {
			return 0, err
		}
		switch n.op {
		case "-":
			if k != KindInt {
				return 0, typeErrf("unary \"-\" requires an integer operand")
			}
			return KindInt, nil
		case "!":
			if k != KindBool {
				return 0, typeErrf("unary \"!\" requires a boolean operand")
			}
			return KindBool, nil
		}
		return 0, typeErrf("unknown unary operator %q", n.op)
	case *binaryNode:
		return binaryType(n)
	case *callNode:
		fn, ok := functions[n.name]
		if !ok {
			return 0, typeErrf("unknown function %q", n.name)
		}
		if fn.args >= 0 && len(n.args) != fn.args {
			return 0, typeErrf("function %q takes %d argument(s), got %d", n.name, fn.args, len(n.args))
		}
		if fn.args < 0 && len(n.args) == 0 {
			return 0, typeErrf("function %q takes at least one argument", n.name)
		}
		for _, a := range n.args {
			k, err := typeOf(a)
			if err != nil {
				return 0, err
			}
			if k != fn.kind {
				return 0, typeErrf("function %q arguments must be integers", n.name)
			}
		}
		return fn.kind, nil
	}
	return 0, apperr.New("formula.typeOf", apperr.Internal, fmt.Sprintf("unexpected node %T", n))
}

func binaryType(n *binaryNode) (Kind, error) {
	lk, err := typeOf(n.left)
	if err != nil {
		return 0, err
	}
	rk, err := typeOf(n.right)
	if err != nil {
		return 0, err
	}
	switch n.op {
	case "+", "-", "*", "/":
		if lk != KindInt || rk != KindInt {
			return 0, typeErrf("operator %q requires integer operands", n.op)
		}
		return KindInt, nil
	case "<", "<=", ">", ">=", "==", "!=":
		if lk != KindInt || rk != KindInt {
			return 0, typeErrf("operator %q requires integer operands", n.op)
		}
		return KindBool, nil
	case "&&", "||":
		if lk != KindBool || rk != KindBool {
			return 0, typeErrf("operator %q requires boolean operands", n.op)
		}
		return KindBool, nil
	}
	return 0, typeErrf("unknown binary operator %q", n.op)
}

func typeErrf(format string, args ...any) error {
	return apperr.New("formula.typeOf", apperr.ConfigInvalid, fmt.Sprintf(format, args...))
}

// checkVars walks the AST and rejects variables that are not in the allowed
// set (the rulebook declares which names each formula may use).
func checkVars(n node, allowed map[string]bool) error {
	switch n := n.(type) {
	case *numberNode:
	case *varNode:
		if !allowed[n.name] {
			return apperr.New("formula.checkVars", apperr.ConfigInvalid,
				"unknown variable "+n.name+" (allowed: "+joinNames(allowed)+")")
		}
	case *unaryNode:
		if err := checkVars(n.operand, allowed); err != nil {
			return err
		}
	case *binaryNode:
		if err := checkVars(n.left, allowed); err != nil {
			return err
		}
		if err := checkVars(n.right, allowed); err != nil {
			return err
		}
	case *callNode:
		for _, a := range n.args {
			if err := checkVars(a, allowed); err != nil {
				return err
			}
		}
	}
	return nil
}

func joinNames(allowed map[string]bool) string {
	names := make([]string, 0, len(allowed))
	for name := range allowed {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}
