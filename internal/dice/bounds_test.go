package dice

import (
	"testing"

	"dicegirl/internal/apperr"
)

func TestExprBounds(t *testing.T) {
	cases := []struct {
		expr     string
		min, max int
	}{
		{"5", 5, 5},
		{"-5", -5, -5},
		{"1d6", 1, 6},
		{"4d6", 4, 24},
		{"4d6kh3", 3, 18}, // three kept dice, each [1,6]
		{"4d6dl1", 3, 18}, // equivalent keep amount
		{"2d6+3", 5, 15},
		{"2d6-1d4", 2 - 4, 12 - 1},
		{"1d4*2d6", 2, 48},
		{"20/3", 6, 6}, // pure constants
		{"-(1d6)", -6, -1},
		{"d%", 1, 100},
	}
	for _, tc := range cases {
		mn, mx, err := ExprBounds(tc.expr, DefaultLimits())
		if err != nil {
			t.Errorf("ExprBounds(%q): %v", tc.expr, err)
			continue
		}
		if mn != tc.min || mx != tc.max {
			t.Errorf("ExprBounds(%q) = [%d,%d], want [%d,%d]", tc.expr, mn, mx, tc.min, tc.max)
		}
	}
}

func TestExprBoundsDivisionRange(t *testing.T) {
	// 1d6 / X with a divisor interval straddling zero: tightest quotas at ±1.
	mn, mx, err := ExprBounds("6/(1d3-2)", DefaultLimits())
	if err != nil {
		t.Fatalf("ExprBounds: %v", err)
	}
	// divisor in [-1,1] \ {0}; quotient in [-6,6]
	if mn != -6 || mx != 6 {
		t.Fatalf("straddling-zero div = [%d,%d], want [-6,6]", mn, mx)
	}

	// Always-zero divisor is a static error.
	_, _, err = ExprBounds("1d6/(6-6)", DefaultLimits())
	if !apperr.IsCode(err, apperr.DivideByZero) {
		t.Fatalf("always-zero divisor: code = %v, want DIVIDE_BY_ZERO", err)
	}
}

func TestExprBoundsErrors(t *testing.T) {
	if _, _, err := ExprBounds("1d", DefaultLimits()); !apperr.IsCode(err, apperr.ParseError) {
		t.Errorf("bad expr code = %v, want PARSE_ERROR", err)
	}
	// Product of groups whose worst case overflows int64.
	big := DefaultLimits()
	big.MaxDicePerGroup = 100
	big.MaxTotalDice = 1000
	big.MaxSides = 1000
	if _, _, err := ExprBounds("100d1000*100d1000*100d1000*100d1000", big); !apperr.IsCode(err, apperr.RollError) {
		t.Errorf("overflow code = %v, want ROLL_ERROR", err)
	}
}
