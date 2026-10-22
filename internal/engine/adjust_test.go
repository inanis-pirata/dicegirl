package engine_test

import (
	"context"
	"strings"
	"testing"

	"dicegirl/internal/apperr"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine"
)

// Target arithmetic (the Dice! `.rc 体质*5` convention) rewrites the
// resolved target before grading and is reported in the result.
func TestTargetAdjustScalesTarget(t *testing.T) {
	cases := []struct {
		adjust engine.TargetAdjust
		want   int
		text   string
	}{
		{engine.TargetAdjust{Multiply: 5}, 300, "*5"},
		{engine.TargetAdjust{Divide: 2}, 30, "/2"},
		{engine.TargetAdjust{Add: -10}, 50, "-10"},
		{engine.TargetAdjust{Multiply: 2, Add: 5}, 125, "*2+5"},
	}
	for _, tc := range cases {
		res, err := scriptedBook(t, 4, 1).Check(context.Background(), engine.CheckInput{
			Target:    60,
			SkillName: "体质",
			Adjust:    tc.adjust,
		})
		if err != nil {
			t.Fatalf("%v: %v", tc.adjust, err)
		}
		if res.Target != tc.want {
			t.Errorf("%v: target = %d, want %d", tc.adjust, res.Target, tc.want)
		}
		if res.TargetOriginal != 60 {
			t.Errorf("%v: target_original = %d, want 60", tc.adjust, res.TargetOriginal)
		}
		if res.TargetAdjust != tc.text {
			t.Errorf("%v: target_adjust = %q, want %q", tc.adjust, res.TargetAdjust, tc.text)
		}
		if !strings.Contains(res.Explanation, "原始 60") {
			t.Errorf("%v: explanation missing the original value: %q", tc.adjust, res.Explanation)
		}
	}
}

// A target outside the declared computed range is a usage error.
func TestTargetAdjustOutOfRange(t *testing.T) {
	b := coc7Book(t, nil)
	// coc7 declares computed_target up to 999.
	_, err := b.Check(context.Background(), engine.CheckInput{
		Target: 99,
		Adjust: engine.TargetAdjust{Multiply: 100},
	})
	if err == nil {
		t.Fatal("adjusted target out of range accepted")
	}
	if !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("want INVALID_ARGUMENT, got %v", err)
	}
}

// A difficulty works on the adjusted target, not on the raw sheet value.
func TestTargetAdjustComposesWithDifficulty(t *testing.T) {
	b := scriptedBook(t, 4, 1) // 42
	res, err := b.Check(context.Background(), engine.CheckInput{
		Target:     60,
		SkillName:  "心理学",
		Adjust:     engine.TargetAdjust{Divide: 2},
		Difficulty: "hard",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseTarget != 30 || res.Target != 15 {
		t.Fatalf("base=%d target=%d, want 30/15", res.BaseTarget, res.Target)
	}
}

// Aliases are rulebook data: they resolve after the canonical ID and before
// the localized label.
func TestDeclaredAliases(t *testing.T) {
	b := coc7Book(t, nil)

	if def, ok := b.FindSkill("察觉"); !ok || def.ID != "spot_hidden" {
		t.Fatalf("skill alias not resolved: %+v", def)
	}
	if def, ok := b.FindAttribute("臂力"); !ok || def.ID != "STR" {
		t.Fatalf("attribute alias not resolved: %+v", def)
	}
	// The canonical ID still wins, and the localized label still resolves.
	if def, ok := b.FindSkill("spot_hidden"); !ok || def.Name != "侦查" {
		t.Fatalf("canonical id broken: %+v", def)
	}
	if def, ok := b.FindAttribute("力量"); !ok || def.ID != "STR" {
		t.Fatalf("localized name broken: %+v", def)
	}
}

// A recovery loss (negative spec) raises the track and is clamped by the
// track maximum.
func TestRecoveryLossRaisesTrack(t *testing.T) {
	ctx := context.Background()
	b := coc7Book(t, dice.New(dice.NewScriptedSource(9, 0))) // roll 100: fumble
	rc, ok := b.ResourceCheck("sanity")
	if !ok {
		t.Fatal("rulebook declares no sanity check")
	}
	res, err := b.RunResourceCheck(ctx, rc, engine.ResourceCheckInput{
		Before:      90,
		SuccessLoss: mustLoss(t, "0"),
		FailureLoss: mustLoss(t, "-2"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Loss != -2 || res.Change.After != 92 {
		t.Fatalf("loss=%d after=%d, want -2/92", res.Loss, res.Change.After)
	}
	if res.Change.Delta != 2 {
		t.Fatalf("delta = %d, want 2", res.Change.Delta)
	}
}

// The explicit current value overrides the check target only; the proposed
// change still starts from the track's own value.
func TestResourceCheckCurrentOverride(t *testing.T) {
	ctx := context.Background()
	b := coc7Book(t, dice.New(dice.NewScriptedSource(9, 0))) // roll 100
	rc, ok := b.ResourceCheck("sanity")
	if !ok {
		t.Fatal("rulebook declares no sanity check")
	}
	target := 40
	res, err := b.RunResourceCheck(ctx, rc, engine.ResourceCheckInput{
		Before:      90,
		Target:      &target,
		SuccessLoss: mustLoss(t, "0"),
		FailureLoss: mustLoss(t, "1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.CheckTarget != 40 {
		t.Fatalf("check target = %d, want 40", res.CheckTarget)
	}
	if res.Change.Before != 90 {
		t.Fatalf("change before = %d, want 90 (the track value)", res.Change.Before)
	}
}

// An out-of-range explicit current value is rejected.
func TestResourceCheckCurrentOverrideOutOfRange(t *testing.T) {
	b := coc7Book(t, nil)
	rc, _ := b.ResourceCheck("sanity")
	target := 500
	_, err := b.RunResourceCheck(context.Background(), rc, engine.ResourceCheckInput{
		Before: 90,
		Target: &target,
	})
	if err == nil || !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("want INVALID_ARGUMENT, got %v", err)
	}
}

func mustLoss(t *testing.T, spec string) engine.LossSpec {
	t.Helper()
	l, err := engine.ParseLossSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	return l
}
