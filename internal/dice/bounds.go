package dice

import (
	"math"

	"dicegirl/internal/apperr"
)

// ExprBounds statically computes the minimum and maximum possible total of a
// pure dice expression without rolling any dice. A die contributes [1,S]
// each; keep/drop modifiers select AmountForKeep dice, so a group's maximum
// is keepAmount*S and its minimum keepAmount.
//
// The intended caller is rulebook logic that needs the worst case of a loss
// expression (e.g. "a fumble costs the maximum possible SAN loss"); it is
// not used by normal rolling.
func ExprBounds(expr string, limits Limits) (min, max int, err error) {
	root, _, err := Parse(expr, limits)
	if err != nil {
		return 0, 0, err
	}
	lo, hi, err := boundsOf(root)
	if err != nil {
		return 0, 0, err
	}
	return int(lo), int(hi), nil
}

func boundsOf(n Node) (int64, int64, error) {
	switch nn := n.(type) {
	case *NumberNode:
		return int64(nn.Value), int64(nn.Value), nil
	case *DiceNode:
		if nn.CountSpec != nil {
			// A counted group is worth the number of matching dice.
			return 0, int64(nn.Count), nil
		}
		diceCount := int64(nn.Count)
		if nn.Explode != nil {
			// Exploding dice may spawn up to the safety limit of extras.
			diceCount += int64(nn.Explode.Max)
		}
		k := int64(nn.AmountForKeep())
		if k > diceCount {
			k = diceCount
		}
		s := int64(nn.Sides)
		hi, ok := checkedMul(k, s)
		if !ok {
			return 0, 0, boundsOverflowErr()
		}
		return k, hi, nil
	case *UnaryNode:
		lo, hi, err := boundsOf(nn.Child)
		if err != nil {
			return 0, 0, err
		}
		if nn.Op == OpSub {
			nlo, ok1 := checkedNeg(hi)
			nhi, ok2 := checkedNeg(lo)
			if !ok1 || !ok2 {
				return 0, 0, boundsOverflowErr()
			}
			return nlo, nhi, nil
		}
		return lo, hi, nil
	case *BinaryNode:
		lo1, hi1, err := boundsOf(nn.Left)
		if err != nil {
			return 0, 0, err
		}
		lo2, hi2, err := boundsOf(nn.Right)
		if err != nil {
			return 0, 0, err
		}
		return boundsBinary(nn.Op, lo1, hi1, lo2, hi2)
	default:
		return 0, 0, apperr.New("dice.Bounds", apperr.Internal, "unknown AST node")
	}
}

func boundsBinary(op Op, lmin, lmax, rmin, rmax int64) (int64, int64, error) {
	var vals []int64
	overflow := func() (int64, int64, error) {
		return 0, 0, boundsOverflowErr()
	}
	switch op {
	case OpAdd:
		v1, ok1 := checkedAdd(lmin, rmin)
		v2, ok2 := checkedAdd(lmax, rmax)
		if !ok1 || !ok2 {
			return overflow()
		}
		vals = append(vals, v1, v2)
	case OpSub:
		v1, ok1 := checkedSub(lmin, rmax)
		v2, ok2 := checkedSub(lmax, rmin)
		if !ok1 || !ok2 {
			return overflow()
		}
		vals = append(vals, v1, v2)
	case OpMul:
		for _, x := range []int64{lmin, lmax} {
			for _, y := range []int64{rmin, rmax} {
				v, ok := checkedMul(x, y)
				if !ok {
					return overflow()
				}
				vals = append(vals, v)
			}
		}
	case OpDiv:
		// Truncation toward zero (Go semantics). Zero can never be a
		// divisor; if the right interval straddles zero, the tightest
		// quotas are reached at divisors -1 and +1.
		denoms := map[int64]bool{}
		for _, d := range []int64{rmin, rmax} {
			if d != 0 {
				denoms[d] = true
			}
		}
		if rmin < 0 && rmax > 0 {
			denoms[-1] = true
			denoms[1] = true
		}
		if len(denoms) == 0 {
			return 0, 0, apperr.New("dice.Bounds", apperr.DivideByZero, "expression always divides by zero")
		}
		for _, x := range []int64{lmin, lmax} {
			for d := range denoms {
				// Integer division overflows only for MinInt64/-1.
				if x == math.MinInt64 && d == -1 {
					return 0, 0, apperr.New("dice.Bounds", apperr.RollError, "maximum possible value overflows")
				}
				vals = append(vals, x/d)
			}
		}
	default:
		return 0, 0, apperr.New("dice.Bounds", apperr.Internal, "unknown operator")
	}
	mn, mx := vals[0], vals[0]
	for _, v := range vals[1:] {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	return mn, mx, nil
}

func boundsOverflowErr() error {
	return apperr.New("dice.Bounds", apperr.RollError, "maximum possible value overflows")
}

func checkedAdd(a, b int64) (int64, bool) {
	if b > 0 && a > math.MaxInt64-b || b < 0 && a < math.MinInt64-b {
		return 0, false
	}
	return a + b, true
}

func checkedSub(a, b int64) (int64, bool) {
	if b < 0 && a > math.MaxInt64+b || b > 0 && a < math.MinInt64+b {
		return 0, false
	}
	return a - b, true
}

func checkedMul(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	if (a > 0 && b > 0 && a > math.MaxInt64/b) ||
		(a < 0 && b < 0 && a < math.MinInt64/-b) ||
		(a > 0 && b < 0 && b < math.MinInt64/a) ||
		(a < 0 && b > 0 && a < math.MinInt64/b) {
		return 0, false
	}
	return a * b, true
}

func checkedNeg(a int64) (int64, bool) {
	if a == math.MinInt64 {
		return 0, false
	}
	return -a, true
}
