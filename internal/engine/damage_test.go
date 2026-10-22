package engine_test

import (
	"context"
	"testing"

	"dicegirl/internal/character"
	"dicegirl/internal/engine"
)

func TestDamageBonusTableBoundaries(t *testing.T) {
	b := coc7Book(t, nil)
	cases := []struct {
		key       int
		wantBuild int
		wantBonus string
	}{
		{2, -2, "-2"},
		{64, -2, "-2"},
		{65, -1, "-1"},
		{84, -1, "-1"},
		{85, 0, "0"},
		{124, 0, "0"},
		{125, 1, "1d4"},
		{165, 2, "1d6"},
		{205, 3, "2d6"},
		{445, 6, "5d6"},
	}
	for _, tc := range cases {
		row, err := b.LookupDamageBonus(tc.key)
		if err != nil {
			t.Fatal(err)
		}
		if row.Build != tc.wantBuild || row.Bonus != tc.wantBonus {
			t.Errorf("key %d = build %d bonus %q, want build %d bonus %q",
				tc.key, row.Build, row.Bonus, tc.wantBuild, tc.wantBonus)
		}
	}
	if _, err := b.LookupDamageBonus(1); err == nil {
		t.Error("key 1 below the table must be rejected")
	}
}

func damageCharacter(attrs map[string]int) *character.Character {
	return &character.Character{Attributes: attrs}
}

// STR 50 + SIZ 60 = 110 -> build 0, db 0; weapon 1d8 rolls face 5.
func TestDamageWithZeroBonus(t *testing.T) {
	b := scriptedBook(t, 4) // 1d8 face 5
	c := damageCharacter(map[string]int{"STR": 50, "SIZ": 60})
	res, err := b.Damage(context.Background(), engine.DamageInput{Expr: "1d8", Character: c})
	if err != nil {
		t.Fatal(err)
	}
	if res.Build != 0 || res.DBExpr != "0" || res.Total != 5 {
		t.Fatalf("build=%d db=%s total=%d", res.Build, res.DBExpr, res.Total)
	}
}

// STR 100 + SIZ 80 = 180 -> build 2, db 1d6; 5 + 3 = 8.
func TestDamageWithPositiveBonus(t *testing.T) {
	b := scriptedBook(t, 4, 2) // 1d8 face 5, 1d6 face 3
	c := damageCharacter(map[string]int{"STR": 100, "SIZ": 80})
	res, err := b.Damage(context.Background(), engine.DamageInput{Expr: "1d8", Character: c})
	if err != nil {
		t.Fatal(err)
	}
	if res.Build != 2 || res.DBExpr != "1d6" || res.Total != 8 {
		t.Fatalf("build=%d db=%s total=%d", res.Build, res.DBExpr, res.Total)
	}
}

// Without a character only the weapon expression is rolled.
func TestDamageWithoutCharacter(t *testing.T) {
	b := scriptedBook(t, 4)
	res, err := b.Damage(context.Background(), engine.DamageInput{Expr: "1d8"})
	if err != nil || res.Build != 0 || res.DBExpr != "0" || res.Total != 5 {
		t.Fatalf("build=%d db=%s total=%d err=%v", res.Build, res.DBExpr, res.Total, err)
	}
}

func TestDamageRejectsBadExpression(t *testing.T) {
	b := coc7Book(t, nil)
	if _, err := b.Damage(context.Background(), engine.DamageInput{Expr: "1d"}); err == nil {
		t.Fatal("invalid damage expression accepted")
	}
}
