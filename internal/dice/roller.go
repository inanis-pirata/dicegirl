package dice

import (
	"context"
	"math"

	"dicegirl/internal/apperr"
)

// Roller parses and evaluates dice expressions. It is safe for concurrent
// use: it holds no per-roll mutable state (the Source must be concurrency
// safe itself; the built-in sources are).
type Roller struct {
	src    Source
	limits Limits
}

// Option customizes a Roller.
type Option func(*Roller)

// WithLimits overrides the built-in safety limits.
func WithLimits(l Limits) Option {
	return func(r *Roller) { r.limits = l }
}

// New builds a Roller with the given random source.
func New(src Source, opts ...Option) *Roller {
	r := &Roller{src: src, limits: DefaultLimits()}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Default returns a production Roller backed by crypto/rand.
func Default() *Roller {
	return New(NewCryptoSource())
}

// Roll parses expr, evaluates it against the injected source and returns the
// structured result.
func (r *Roller) Roll(ctx context.Context, expr string) (*RollResult, error) {
	if r == nil || r.src == nil {
		return nil, apperr.New("dice.Roll", apperr.Internal, "roller not initialized")
	}
	if err := ctx.Err(); err != nil {
		return nil, apperr.Wrap("dice.Roll", apperr.RollError, "cancelled", err)
	}
	root, groups, err := Parse(expr, r.limits)
	if err != nil {
		return nil, err
	}
	if err := Eval(ctx, root, r.src); err != nil {
		return nil, err
	}
	return &RollResult{
		Expression: expr,
		Root:       root,
		Total:      valueOf(root),
		Groups:     groups,
	}, nil
}

// Intn draws a uniform value in [0, n) from the injected source. It is the
// building block for "pick one of n" use cases (random choices, random
// tables) that need no dice expression.
func (r *Roller) Intn(ctx context.Context, n int) (int, error) {
	if r == nil || r.src == nil {
		return 0, apperr.New("dice.Intn", apperr.Internal, "roller not initialized")
	}
	if n <= 0 {
		return 0, apperr.New("dice.Intn", apperr.InvalidArgument, "n must be positive")
	}
	if err := ctx.Err(); err != nil {
		return 0, apperr.Wrap("dice.Intn", apperr.RollError, "cancelled", err)
	}
	v, err := r.src.Intn(n)
	if err != nil {
		return 0, apperr.Wrap("dice.Intn", apperr.RollError, "random source failed", err)
	}
	return v, nil
}

func valueOf(n Node) int {
	switch nn := n.(type) {
	case *NumberNode:
		return nn.Value
	case *DiceNode:
		return nn.Value
	case *BinaryNode:
		return nn.Value
	case *UnaryNode:
		return nn.Value
	default:
		return 0
	}
}

func applyOp(op Op, left, right int) (int, error) {
	l, r := int64(left), int64(right)
	switch op {
	case OpAdd:
		if r > 0 && l > math.MaxInt64-r || r < 0 && l < math.MinInt64-r {
			return 0, apperr.New("dice.Eval", apperr.RollError, "arithmetic overflow")
		}
		return int(l + r), nil
	case OpSub:
		if r < 0 && l > math.MaxInt64+r || r > 0 && l < math.MinInt64+r {
			return 0, apperr.New("dice.Eval", apperr.RollError, "arithmetic overflow")
		}
		return int(l - r), nil
	case OpMul:
		if l != 0 && r != 0 {
			if (l > 0 && r > 0 && l > math.MaxInt64/r) ||
				(l < 0 && r < 0 && l < math.MinInt64/-r) ||
				(l > 0 && r < 0 && r < math.MinInt64/l) ||
				(l < 0 && r > 0 && l < math.MinInt64/r) {
				return 0, apperr.New("dice.Eval", apperr.RollError, "arithmetic overflow")
			}
		}
		return int(l * r), nil
	case OpDiv:
		if right == 0 {
			return 0, apperr.New("dice.Eval", apperr.DivideByZero, "division by zero")
		}
		// Truncation toward zero (Go semantics); documented in README.
		return left / right, nil
	default:
		return 0, apperr.New("dice.Eval", apperr.Internal, "unknown operator")
	}
}
