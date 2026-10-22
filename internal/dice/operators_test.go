package dice

import (
	"context"
	"testing"
)

// The "!" modifier explodes: a die that reaches the threshold spawns
// another die of the same size.
func TestExplodeModifier(t *testing.T) {
	// 1d6!: the first die shows 6 and explodes into a 3.
	res := rollScripted(t, "1d6!", 5, 2)
	if len(res.Groups) != 1 {
		t.Fatalf("groups = %d", len(res.Groups))
	}
	group := res.Groups[0]
	if len(group.Rolls) != 2 {
		t.Fatalf("rolls = %d, want 2 (the exploding die plus its child)", len(group.Rolls))
	}
	if group.Rolls[0].Value != 6 || !group.Rolls[0].Exploded {
		t.Fatalf("first die = %+v, want an exploded 6", group.Rolls[0])
	}
	if group.Rolls[1].Value != 3 {
		t.Fatalf("extra die = %d, want 3", group.Rolls[1].Value)
	}
	if res.Total != 9 {
		t.Fatalf("total = %d, want 9", res.Total)
	}
}

// An explicit threshold decides which dice explode.
func TestExplodeThreshold(t *testing.T) {
	// 2d6!4: the 5 explodes into a 3, the 2 does not.
	res := rollScripted(t, "2d6!4", 4, 1, 2)
	group := res.Groups[0]
	if len(group.Rolls) != 3 {
		t.Fatalf("rolls = %d, want 3", len(group.Rolls))
	}
	if !group.Rolls[0].Exploded || group.Rolls[1].Exploded {
		t.Fatalf("explode flags wrong: %+v", group.Rolls)
	}
	if res.Total != 5+2+3 {
		t.Fatalf("total = %d, want 10", res.Total)
	}
}

// The explode chain stops at the safety limit.
func TestExplodeLimit(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxExplodeDice = 2
	// Every die shows 6: the chain may add at most two dice.
	res := rollScriptedWithLimits(t, limits, "1d6!", 5, 5, 5, 5)
	if len(res.Groups[0].Rolls) != 3 {
		t.Fatalf("rolls = %d, want 3 (the die plus two extras)", len(res.Groups[0].Rolls))
	}
}

// The "r" modifier rerolls dice below the threshold.
func TestRerollModifier(t *testing.T) {
	// 2d6r4: the 1 is rerolled into a 4.
	res := rollScripted(t, "2d6r4", 0, 3, 5)
	group := res.Groups[0]
	if len(group.Rolls) != 2 {
		t.Fatalf("rolls = %d, want 2", len(group.Rolls))
	}
	if group.Rolls[0].Value != 4 || group.Rolls[0].Rerolls != 1 {
		t.Fatalf("first die = %+v, want a rerolled 4", group.Rolls[0])
	}
	if group.Rolls[1].Value != 6 || group.Rolls[1].Rerolls != 0 {
		t.Fatalf("second die = %+v, want a plain 6", group.Rolls[1])
	}
	if res.Total != 10 {
		t.Fatalf("total = %d, want 10", res.Total)
	}
}

// The "c" modifier turns the group into a dice-pool count.
func TestCountModifier(t *testing.T) {
	// 5d10c8: the faces 8, 9, 10, 3, 6 -> three successes.
	res := rollScripted(t, "5d10c8", 7, 8, 9, 2, 5)
	if res.Total != 3 {
		t.Fatalf("total = %d, want 3", res.Total)
	}
	if len(res.Groups[0].Rolls) != 5 {
		t.Fatalf("rolls = %d, want 5", len(res.Groups[0].Rolls))
	}
}

// The count modifier accepts every comparison direction.
func TestCountComparisonDirections(t *testing.T) {
	cases := []struct {
		expr string
		want int
	}{
		{"5d10c8", 3},   // faces 8,9,10,3,6
		{"5d10c>=8", 3}, // same, explicit
		{"5d10c>8", 2},  // 9,10
		{"5d10c<=8", 3}, // 8,3,6
		{"5d10c<8", 2},  // 3,6
	}
	for _, tc := range cases {
		res := rollScripted(t, tc.expr, 7, 8, 9, 2, 5)
		if res.Total != tc.want {
			t.Errorf("%s = %d, want %d", tc.expr, res.Total, tc.want)
		}
	}
}

// A counted group is worth at most the number of dice.
func TestCountBounds(t *testing.T) {
	lo, hi, err := ExprBounds("5d10c8", DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if lo != 0 || hi != 5 {
		t.Fatalf("bounds = %d..%d, want 0..5", lo, hi)
	}
}

// The count modifier composes with keep and explode.
func TestCountWithKeepAndExplode(t *testing.T) {
	// 4d6kh2c4: keep the highest two, then count those >= 4.
	res := rollScripted(t, "4d6kh2c4", 0, 5, 3, 4)
	if res.Total != 2 {
		t.Fatalf("total = %d, want 2 (the 6 and the 5)", res.Total)
	}
}

// A comparison inside a group belongs to the expression, not to the
// statement layer.
func TestStatementKeepsInnerComparison(t *testing.T) {
	st, err := ParseStatement("5d10c>=8", DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if st.Compare != nil {
		t.Fatalf("unexpected statement comparison: %+v", st.Compare)
	}
	if st.Expr != "5d10c>=8" {
		t.Fatalf("expr = %q", st.Expr)
	}
	// A trailing comparison is still the statement layer's business.
	st, err = ParseStatement("2d6+1>=7", DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if st.Compare == nil || st.Compare.Op != CmpGE || st.Compare.Target != 7 {
		t.Fatalf("comparison = %+v", st.Compare)
	}
	if st.Expr != "2d6+1" {
		t.Fatalf("expr = %q", st.Expr)
	}
}

// Malformed modifiers are parse errors.
func TestModifierParseErrors(t *testing.T) {
	for _, in := range []string{
		"1d6r",    // reroll without a threshold
		"1d6c",    // count without a threshold
		"1d6!!",   // two explode modifiers
		"1d6r1r1", // two reroll modifiers
		"1d6c8c8", // two count modifiers
	} {
		_, _, err := Parse(in, DefaultLimits())
		if err == nil {
			t.Errorf("Parse(%q): expected an error", in)
		}
	}
}

// rollScriptedWithLimits rolls with custom limits.
func rollScriptedWithLimits(t *testing.T, limits Limits, expr string, values ...int) *RollResult {
	t.Helper()
	r := New(NewScriptedSource(values...), WithLimits(limits))
	res, err := r.Roll(context.Background(), expr)
	if err != nil {
		t.Fatalf("Roll(%q): %v", expr, err)
	}
	return res
}
