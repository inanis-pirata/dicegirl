package dice

import (
	"context"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
)

// Roll statement grammar (documented in README "骰子表达式语法"):
//
//	stmt    = [ repeat '#' ] expr [ cmp integer ]
//	repeat  = number            # 1..Limits.MaxRepeat; omitted number => 1
//	cmp     = '>=' | '>' | '<=' | '<'
//
// The repeat prefix follows the Dice! convention (3#1d20) and the comparison
// suffix follows BCDice (2D6+1>=7). The statement layer stays a thin wrapper
// around the pure expression parser: reason text and command prefixes are UI
// concerns and never appear here. Every repeat trial re-parses the expression
// so each roll owns an independent evaluated AST.

// Statement is a parsed roll statement.
type Statement struct {
	// Repeat is the number of independent trials (at least 1).
	Repeat int
	// Expr is the inner dice expression without repeat prefix or comparison.
	Expr string
	// Compare is the optional whole-expression comparison suffix.
	Compare *CompareSpec
}

// BatchResult is the result of rolling a statement: one RollResult per trial.
type BatchResult struct {
	// Expression is the full statement as typed (trimmed).
	Expression string `json:"expression"`
	// Repeat is the number of trials in Rolls.
	Repeat int `json:"repeat"`
	// Rolls holds one independent result per trial, in order.
	Rolls []*RollResult `json:"rolls"`
}

// ParseStatement parses a roll statement without rolling any dice. The inner
// expression is fully validated against limits at parse time.
func ParseStatement(input string, limits Limits) (*Statement, error) {
	const op = "dice.ParseStatement"
	s := strings.TrimSpace(input)
	if s == "" {
		return nil, apperr.New(op, apperr.ParseError, "empty expression")
	}

	st := &Statement{Repeat: 1}
	rest := s
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		prefix := strings.TrimSpace(rest[:i])
		rest = strings.TrimSpace(rest[i+1:])
		switch {
		case prefix == "":
			// "#expr": Dice! convention, omitted count means 1.
		case isDigits(prefix):
			n, _ := strconv.Atoi(prefix)
			if n < 1 {
				return nil, apperr.New(op, apperr.InvalidArgument,
					"repeat count must be at least 1")
			}
			if limits.MaxRepeat > 0 && n > limits.MaxRepeat {
				return nil, apperr.New(op, apperr.LimitExceeded,
					"repeat count "+itoa(n)+" exceeds limit "+itoa(limits.MaxRepeat))
			}
			st.Repeat = n
		default:
			return nil, apperr.New(op, apperr.ParseError,
				"unexpected '#' at position "+itoa(i))
		}
		if strings.IndexByte(rest, '#') >= 0 {
			return nil, apperr.New(op, apperr.ParseError,
				"repeat prefix may appear only once, at the start")
		}
	}
	if rest == "" {
		return nil, apperr.New(op, apperr.ParseError, "missing dice expression")
	}

	// The comparison suffix belongs to the statement layer, but a
	// comparison inside a dice group (the "c" count modifier) belongs to
	// the expression: try the pure expression first and only split off a
	// trailing comparison when that fails.
	expr := strings.TrimSpace(rest)
	if _, _, err := Parse(expr, limits); err != nil {
		if i, cop, tail, found := findComparison(expr); found {
			targetText := strings.TrimSpace(tail)
			target, terr := strconv.Atoi(targetText)
			if terr != nil || targetText == "" {
				return nil, apperr.New(op, apperr.ParseError,
					"invalid comparison target, expected an integer after "+quote(cop.String()))
			}
			prefix := strings.TrimSpace(expr[:i])
			if prefix == "" {
				return nil, apperr.New(op, apperr.ParseError,
					"expected a dice expression before comparison")
			}
			if _, _, perr := Parse(prefix, limits); perr == nil {
				st.Compare = &CompareSpec{Op: cop, Target: target}
				expr = prefix
			}
		}
	}

	st.Expr = expr
	if _, _, err := Parse(expr, limits); err != nil {
		return nil, err
	}
	return st, nil
}

// findComparison locates the first comparison operator in an expression and
// returns its position, the operator and the text after it.
func findComparison(expr string) (int, CmpOp, string, bool) {
	for i := 0; i < len(expr); i++ {
		switch expr[i] {
		case '>':
			if i+1 < len(expr) && expr[i+1] == '=' {
				return i, CmpGE, expr[i+2:], true
			}
			return i, CmpGT, expr[i+1:], true
		case '<':
			if i+1 < len(expr) && expr[i+1] == '=' {
				return i, CmpLE, expr[i+2:], true
			}
			return i, CmpLT, expr[i+1:], true
		}
	}
	return 0, 0, "", false
}

// RollStatement parses input and evaluates it once per repeat trial, judging
// the comparison suffix (if any) against each total independently.
func (r *Roller) RollStatement(ctx context.Context, input string) (*BatchResult, error) {
	const op = "dice.RollStatement"
	if r == nil || r.src == nil {
		return nil, apperr.New(op, apperr.Internal, "roller not initialized")
	}
	if err := ctx.Err(); err != nil {
		return nil, apperr.Wrap(op, apperr.RollError, "cancelled", err)
	}
	st, err := ParseStatement(input, r.limits)
	if err != nil {
		return nil, err
	}

	batch := &BatchResult{Expression: strings.TrimSpace(input), Repeat: st.Repeat}
	for i := 0; i < st.Repeat; i++ {
		// Re-parse for every trial so the evaluated ASTs stay independent.
		root, groups, perr := Parse(st.Expr, r.limits)
		if perr != nil {
			return nil, perr
		}
		if eerr := Eval(ctx, root, r.src); eerr != nil {
			return nil, eerr
		}
		rr := &RollResult{
			Expression: st.Expr,
			Root:       root,
			Total:      valueOf(root),
			Groups:     groups,
		}
		if st.Compare != nil {
			rr.Compare = st.Compare.Judge(rr.Total)
		}
		batch.Rolls = append(batch.Rolls, rr)
	}
	return batch, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
