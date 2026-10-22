package engine_test

import (
	"context"
	"testing"

	"dicegirl/internal/engine"
)

// checkScript rolls tens dice first, then the units die; values are 0-based
// Intn results for d10.
func TestBasicCheckAndMapping(t *testing.T) {
	b := scriptedBook(t, 4, 1) // tens face 5 -> 40, units face 2 -> 2
	res, err := b.Check(context.Background(), engine.CheckInput{Target: 60, SkillName: "心理学"})
	if err != nil {
		t.Fatal(err)
	}
	if res.RawRoll != 42 || res.LevelCode != "regular" || res.Label != "普通成功" {
		t.Fatalf("raw=%d level=%s(%s)", res.RawRoll, res.LevelCode, res.Label)
	}
}

func TestDoubleZeroIs100(t *testing.T) {
	b := scriptedBook(t, 0, 9) // tens face 1 -> 00, units face 10 -> 0
	res, err := b.Check(context.Background(), engine.CheckInput{Target: 60})
	if err != nil {
		t.Fatal(err)
	}
	if res.RawRoll != 100 || res.LevelCode != "fumble" {
		t.Fatalf("raw=%d level=%s", res.RawRoll, res.LevelCode)
	}
}

func TestSuccessLevelBoundaries(t *testing.T) {
	b := coc7Book(t, nil)
	cases := []struct {
		target int
		raw    int
		want   string
	}{
		{60, 12, "extreme"}, // floor(60/5) = 12
		{60, 13, "hard"},
		{60, 30, "hard"}, // floor(60/2) = 30
		{60, 31, "regular"},
		{60, 60, "regular"},
		{60, 61, "fail"},
		{49, 9, "extreme"}, // floor(49/5) = 9
		{49, 24, "hard"},   // floor(49/2) = 24
		{50, 10, "extreme"},
		{99, 19, "extreme"},
		{99, 49, "hard"},
		{0, 1, "fail"},
	}
	for _, tc := range cases {
		_, code, _, err := b.LevelFor(tc.target, tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if code != tc.want {
			t.Errorf("target %d raw %d = %s, want %s", tc.target, tc.raw, code, tc.want)
		}
	}
}

func TestFumbleThresholds(t *testing.T) {
	b := coc7Book(t, nil)
	cases := []struct {
		target, raw int
		want        string
	}{
		{49, 96, "fumble"},
		{50, 96, "fail"},
		{50, 100, "fumble"},
		{49, 95, "fail"},
	}
	for _, tc := range cases {
		_, code, _, err := b.LevelFor(tc.target, tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if code != tc.want {
			t.Errorf("target %d raw %d = %s, want %s", tc.target, tc.raw, code, tc.want)
		}
	}
}

func TestBonusDiceChooseLowerTens(t *testing.T) {
	// two tens dice: faces 8 -> 70, 2 -> 10; units face 6 -> 6
	b := scriptedBook(t, 7, 1, 5)
	res, err := b.Check(context.Background(), engine.CheckInput{Target: 80, Bonus: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.TensCandidates) != 2 || res.ChosenTens != 10 || res.ChosenTensIndex != 1 {
		t.Fatalf("candidates %v chosen %d idx %d", res.TensCandidates, res.ChosenTens, res.ChosenTensIndex)
	}
	if res.RawRoll != 16 {
		t.Fatalf("raw = %d, want 16", res.RawRoll)
	}
}

func TestPenaltyDiceChooseHigherTens(t *testing.T) {
	b := scriptedBook(t, 1, 7, 9)
	res, err := b.Check(context.Background(), engine.CheckInput{Target: 80, Penalty: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.ChosenTens != 70 || res.RawRoll != 70 || res.ChosenTensIndex != 1 {
		t.Fatalf("chosen %d idx %d raw %d", res.ChosenTens, res.ChosenTensIndex, res.RawRoll)
	}
}

func TestBonusPenaltyCancel(t *testing.T) {
	b := scriptedBook(t, 2, 3) // one tens die face 3 -> 20, units face 4
	res, err := b.Check(context.Background(), engine.CheckInput{Target: 80, Bonus: 1, Penalty: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.TensCandidates) != 1 || res.RawRoll != 24 {
		t.Fatalf("candidates %v raw %d", res.TensCandidates, res.RawRoll)
	}
}

func TestBonusDiceDoubleZero(t *testing.T) {
	b := scriptedBook(t, 0, 9, 9)
	res, err := b.Check(context.Background(), engine.CheckInput{Target: 80, Bonus: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.ChosenTens != 0 || res.RawRoll != 100 {
		t.Fatalf("chosen %d raw %d", res.ChosenTens, res.RawRoll)
	}
}

func TestCheckTargetValidation(t *testing.T) {
	b := coc7Book(t, nil)
	for _, bad := range []int{-1, 100} {
		if _, err := b.Check(context.Background(), engine.CheckInput{Target: bad}); err == nil {
			t.Errorf("target %d accepted", bad)
		}
	}
}

func TestOpposed(t *testing.T) {
	ctx := context.Background()
	b1 := scriptedBook(t, 4, 1) // 42 regular
	a, err := b1.Check(ctx, engine.CheckInput{Target: 60})
	if err != nil {
		t.Fatal(err)
	}
	b2 := scriptedBook(t, 0, 4) // 00 + units 5 => 5 extreme
	cb, err := b2.Check(ctx, engine.CheckInput{Target: 60})
	if err != nil {
		t.Fatal(err)
	}
	res, err := b1.Opposed(a, cb)
	if err != nil || res.Winner != 2 {
		t.Fatalf("winner = %d, %v", res.Winner, err)
	}

	fumbleRank, _, _, _ := b1.LevelFor(60, 100)
	failRank, _, _, _ := b1.LevelFor(30, 95)
	hardRank, _, hardLabel, _ := b1.LevelFor(60, 30)

	// Same level, higher target wins.
	la := &engine.CheckResult{Target: 60, RawRoll: 30, Level: hardRank, LevelCode: "hard", Label: hardLabel}
	lb := &engine.CheckResult{Target: 50, RawRoll: 20, Level: hardRank, LevelCode: "hard", Label: hardLabel}
	res, _ = b1.Opposed(la, lb)
	if res.Winner != 1 {
		t.Fatalf("higher target must win, got %d", res.Winner)
	}

	// Fumble always loses.
	fa := &engine.CheckResult{Target: 60, RawRoll: 100, Level: fumbleRank, LevelCode: "fumble", Label: "大失败"}
	fb := &engine.CheckResult{Target: 30, RawRoll: 95, Level: failRank, LevelCode: "fail", Label: "失败"}
	res, _ = b1.Opposed(fa, fb)
	if res.Winner != 2 {
		t.Fatalf("fumble must lose, got %d", res.Winner)
	}

	// Both fumble => tie.
	res, _ = b1.Opposed(fa, &engine.CheckResult{
		Target: 30, RawRoll: 96, Level: fumbleRank, LevelCode: "fumble", Label: "大失败",
	})
	if res.Winner != 0 {
		t.Fatalf("both fumble must tie, got %d", res.Winner)
	}
}
