// Package formula implements the small, side-effect-free formula language
// used inside rulebooks. Rulebooks declare thresholds and derived values as
// formulas (e.g. "roll <= floor(target/5)"); this package parses,
// statically checks and evaluates them. There is no eval of arbitrary code:
// only integer variables, arithmetic, comparisons, boolean logic and a
// fixed whitelist of functions.
//
// Grammar:
//
//	or    := and { "||" and }
//	and   := not { "&&" not }
//	not   := "!" not | cmp
//	cmp   := add { ("<"|"<="|">"|">="|"=="|"!=") add }
//	add   := mul { ("+"|"-") mul }
//	mul   := unary { ("*"|"/") unary }
//	unary := "-" unary | primary
//	primary := number | ident | call | "(" or ")"
//	call  := ident "(" [or {"," or}] ")"
//
// Values are either int or bool; the two never implicitly convert.
package formula

import (
	"dicegirl/internal/apperr"
)

// Env supplies integer variable values (roll, target, characteristics,
// resource current values, ...).
type Env map[string]int

// Kind is a formula value type.
type Kind int

const (
	KindInt Kind = iota
	KindBool
)

// Expr is a parsed, type-checked formula.
type Expr struct {
	root node
	kind Kind
	src  string
}

// Source returns the original formula text.
func (e *Expr) Source() string { return e.src }

// Kind returns the static result type.
func (e *Expr) Kind() Kind { return e.kind }

// Parse parses src without checking variables (use Check for that).
func Parse(src string) (*Expr, error) {
	tokens, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}
	root, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tEOF {
		return nil, p.errorf(p.peek(), "unexpected trailing token")
	}
	kind, err := typeOf(root)
	if err != nil {
		return nil, err
	}
	return &Expr{root: root, kind: kind, src: src}, nil
}

// Check validates that every variable is allowed and that the formula has
// the expected result type. Run it at rulebook-load time.
func (e *Expr) Check(vars map[string]bool, want Kind) error {
	if e == nil {
		return apperr.New("formula.Check", apperr.ConfigInvalid, "nil formula")
	}
	if err := checkVars(e.root, vars); err != nil {
		return err
	}
	if e.kind != want {
		wantName, gotName := "integer", "integer"
		if want == KindBool {
			wantName = "boolean"
		}
		if e.kind == KindBool {
			gotName = "boolean"
		}
		return apperr.New("formula.Check", apperr.ConfigInvalid,
			"formula "+quote(e.src)+" must be "+wantName+", got "+gotName)
	}
	return nil
}

// EvalInt evaluates an integer-typed formula.
func (e *Expr) EvalInt(env Env) (int, error) {
	if e.kind != KindInt {
		return 0, apperr.New("formula.EvalInt", apperr.InvalidArgument,
			"formula "+quote(e.src)+" is boolean, expected integer")
	}
	v, err := eval(e.root, env)
	if err != nil {
		return 0, err
	}
	return v.i, nil
}

// EvalBool evaluates a boolean-typed formula. Missing variables are an
// error.
func (e *Expr) EvalBool(env Env) (bool, error) {
	if e.kind != KindBool {
		return false, apperr.New("formula.EvalBool", apperr.InvalidArgument,
			"formula "+quote(e.src)+" is integer, expected boolean")
	}
	v, err := eval(e.root, env)
	if err != nil {
		return false, err
	}
	return v.b, nil
}

// MustParse panics on error; for tests and compiled-in constants only.
func MustParse(src string) *Expr {
	e, err := Parse(src)
	if err != nil {
		panic(err)
	}
	return e
}

func quote(s string) string { return "\"" + s + "\"" }
