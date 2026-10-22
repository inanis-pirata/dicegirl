package engine_test

import (
	"context"
	"testing"

	"dicegirl/internal/apperr"
	"dicegirl/internal/engine"
)

// TestCheckMany runs independent trials through the same grading pipeline:
// scripted draws produce 53 (regular), 13 (hard) and 96 (fail against 60,
// where only 100 fumbles).
func TestCheckMany(t *testing.T) {
	ctx := context.Background()
	b := scriptedBook(t, 5, 2, 1, 2, 9, 5)

	res, err := b.CheckMany(ctx, engine.CheckBatchInput{
		CheckInput: engine.CheckInput{Target: 60, SkillName: "心理学"},
		Repeat:     3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != "check_batch" || res.Repeat != 3 || len(res.Checks) != 3 {
		t.Fatalf("batch header = %q repeat=%d checks=%d", res.Kind, res.Repeat, len(res.Checks))
	}
	want := []struct {
		raw   int
		level string
	}{
		{53, "regular"},
		{13, "hard"},
		{96, "fail"},
	}
	for i, w := range want {
		got := res.Checks[i]
		if got.RawRoll != w.raw || got.LevelCode != w.level {
			t.Errorf("trial %d = raw %d level %s, want raw %d level %s",
				i, got.RawRoll, got.LevelCode, w.raw, w.level)
		}
		if got.SkillName != "心理学" {
			t.Errorf("trial %d lost the label: %q", i, got.SkillName)
		}
	}
}

// TestCheckManyDifficulty shows repeat composes with explicit difficulty:
// every trial rewrites target 60 to hard 30 and keeps base-target labels.
func TestCheckManyDifficulty(t *testing.T) {
	ctx := context.Background()
	// 53 fails hard-of-60; 13 reaches the hard band.
	b := scriptedBook(t, 5, 2, 1, 2)
	res, err := b.CheckMany(ctx, engine.CheckBatchInput{
		CheckInput: engine.CheckInput{Target: 60, Difficulty: "hard"},
		Repeat:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Checks[0].LevelCode != "fail" || res.Checks[1].LevelCode != "hard" {
		t.Fatalf("levels = %s,%s want fail,hard",
			res.Checks[0].LevelCode, res.Checks[1].LevelCode)
	}
	for i, c := range res.Checks {
		if c.Target != 30 || c.BaseTarget != 60 || c.DifficultyID != "hard" {
			t.Fatalf("trial %d target/base/diff = %d/%d/%q", i, c.Target, c.BaseTarget, c.DifficultyID)
		}
	}
}

func TestCheckManyRepeatBounds(t *testing.T) {
	b := coc7Book(t, nil)
	for _, n := range []int{0, -1, engine.MaxCheckRepeat + 1} {
		_, err := b.CheckMany(context.Background(), engine.CheckBatchInput{
			CheckInput: engine.CheckInput{Target: 60},
			Repeat:     n,
		})
		if !apperr.IsCode(err, apperr.InvalidArgument) {
			t.Errorf("repeat %d: code = %v, want INVALID_ARGUMENT", n, err)
		}
	}
}
