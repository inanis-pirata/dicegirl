package dice

import (
	"context"
	"testing"

	"dicegirl/internal/apperr"
)

func rollScripted(t *testing.T, expr string, values ...int) *RollResult {
	t.Helper()
	r := New(NewScriptedSource(values...))
	res, err := r.Roll(context.Background(), expr)
	if err != nil {
		t.Fatalf("Roll(%q): %v", expr, err)
	}
	return res
}

func TestBasicRolls(t *testing.T) {
	if got := rollScripted(t, "1d6", 0).Total; got != 1 {
		t.Fatalf("1d6 with 0-based 0 = %d, want 1", got)
	}
	res := rollScripted(t, "2d6+3", 0, 5)
	if res.Total != 10 {
		t.Fatalf("2d6+3 = %d, want 10 (1+6+3)", res.Total)
	}
	if len(res.Groups) != 1 || len(res.Groups[0].Rolls) != 2 {
		t.Fatalf("group details wrong: %+v", res.Groups)
	}

	if got := rollScripted(t, "d6", 3).Total; got != 4 {
		t.Fatalf("d6 (omitted count) = %d, want 4", got)
	}
	if got := rollScripted(t, "d%", 42).Total; got != 43 {
		t.Fatalf("d%% = %d, want 43", got)
	}
	if got := rollScripted(t, "1d100", 99).Total; got != 100 {
		t.Fatalf("1d100 max = %d, want 100", got)
	}
}

func TestMultipleGroupsAndModifiers(t *testing.T) {
	res := rollScripted(t, "1d4+1d4", 0, 3)
	if res.Total != 5 {
		t.Fatalf("1d4+1d4 = %d, want 5", res.Total)
	}
	if len(res.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(res.Groups))
	}

	// rolls 1,6,3,5 -> keep highest 6,5,3 = 14
	res = rollScripted(t, "4d6kh3", 0, 5, 2, 4)
	if res.Total != 14 {
		t.Fatalf("4d6kh3 = %d, want 14", res.Total)
	}
	kept := 0
	for _, dr := range res.Groups[0].Rolls {
		if dr.Kept {
			kept++
		}
	}
	if kept != 3 {
		t.Fatalf("kept dice = %d, want 3", kept)
	}

	// drop lowest equivalent: rolls 1,6,3,5 -> 14
	if got := rollScripted(t, "4d6dl1", 0, 5, 2, 4).Total; got != 14 {
		t.Fatalf("4d6dl1 = %d, want 14", got)
	}
	// keep lowest: 1,6,3,5 -> 1,3 = 4
	if got := rollScripted(t, "4d6kl2", 0, 5, 2, 4).Total; got != 4 {
		t.Fatalf("4d6kl2 = %d, want 4 (1+3)", got)
	}
	// drop highest: 1,6,3,5 -> 1,3,5 = 9
	if got := rollScripted(t, "4d6dh1", 0, 5, 2, 4).Total; got != 9 {
		t.Fatalf("4d6dh1 = %d, want 9 (1+3+5)", got)
	}
	// omitted keep amount defaults to 1: d20 faces 11 and 3, keep lowest = 3
	if got := rollScripted(t, "2d20kl", 10, 2).Total; got != 3 {
		t.Fatalf("2d20kl = %d, want 3", got)
	}
}

func TestArithmeticPrecedence(t *testing.T) {
	cases := map[string]int{
		"2+3*4":     14,
		"(2+3)*4":   20,
		"7/2":       3,
		"-7/2":      -3,
		"10-2-3":    5,
		"2*3+4*5":   26,
		"-(6)":      -6,
		" 2d6 + 3 ": 10,
		"100/(2+3)": 20,
	}
	for expr, want := range cases {
		switch expr {
		case " 2d6 + 3 ":
			if got := rollScripted(t, expr, 0, 5).Total; got != want {
				t.Fatalf("%q = %d, want %d", expr, got, want)
			}
		default:
			if got := rollScripted(t, expr).Total; got != want {
				t.Fatalf("%q = %d, want %d", expr, got, want)
			}
		}
	}
}

func TestParseErrors(t *testing.T) {
	bad := []struct {
		expr string
		code apperr.Code
	}{
		{"", apperr.ParseError},
		{"   ", apperr.ParseError},
		{"1d", apperr.ParseError},
		{"d", apperr.ParseError},
		{"1d6 x", apperr.ParseError},
		{"(1d6", apperr.ParseError},
		{"1d6)", apperr.ParseError},
		{"1d100+", apperr.ParseError},
		{"*3", apperr.ParseError},
		{"2#6", apperr.ParseError},
		{"1d6foo", apperr.ParseError},
		{"1d0", apperr.InvalidArgument},
		{"0d6", apperr.InvalidArgument},
		{"4d6kh5", apperr.InvalidArgument},
		{"4d6kh0", apperr.InvalidArgument},
		{"2d6kh1dl1", apperr.ParseError},
		{"101d6", apperr.LimitExceeded},
		{"1d1001", apperr.LimitExceeded},
		{"600d2+600d2", apperr.LimitExceeded},
		{"1/0", apperr.DivideByZero},
		{"6/(3-3)", apperr.DivideByZero},
	}
	for _, tc := range bad {
		r := New(NewScriptedSource(0))
		_, err := r.Roll(context.Background(), tc.expr)
		if err == nil {
			t.Errorf("%q: expected error, got nil", tc.expr)
			continue
		}
		if got := apperr.CodeOf(err); got != tc.code {
			t.Errorf("%q: code = %q, want %q (%v)", tc.expr, got, tc.code, err)
		}
	}
}

func TestContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := New(NewCryptoSource())
	if _, err := r.Roll(ctx, "1d6"); !apperr.IsCode(err, apperr.RollError) {
		t.Fatalf("cancelled roll err = %v", err)
	}
}

func TestSeededSourceReproducible(t *testing.T) {
	r1 := New(NewSeededSource(7))
	r2 := New(NewSeededSource(7))
	a, err := r1.Roll(context.Background(), "10d100")
	if err != nil {
		t.Fatal(err)
	}
	b, err := r2.Roll(context.Background(), "10d100")
	if err != nil {
		t.Fatal(err)
	}
	if a.Total != b.Total {
		t.Fatalf("same seed produced different totals %d vs %d", a.Total, b.Total)
	}
}

func TestRollResultStructure(t *testing.T) {
	res := rollScripted(t, "2d6+1d4-2", 1, 2, 0)
	// 2 + 3 + 1 - 2 = 4
	if res.Total != 4 {
		t.Fatalf("total = %d, want 4", res.Total)
	}
	if res.Expression != "2d6+1d4-2" {
		t.Fatalf("expression = %q", res.Expression)
	}
	if res.Groups[0].Sides != 6 || res.Groups[1].Sides != 4 {
		t.Fatalf("groups lost shape: %+v", res.Groups)
	}
}
