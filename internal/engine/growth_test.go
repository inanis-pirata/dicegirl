package engine_test

import (
	"bytes"
	"context"
	"testing"

	"dicegirl/internal/apperr"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine"
	"dicegirl/rulebooks"
)

// A growth check rolls 1d100 and grows the skill when the roll is higher
// than the current value (the CoC 7e rule: high skills are harder to grow).
func TestGrowthCheck(t *testing.T) {
	// 1d100 -> 88 > 60: the skill grows by the 1d10 gain (roll 3).
	b := scriptedBook(t, 87, 2)
	key, ok := b.GrowthKey("心理学")
	if !ok || key != "skill:psychology" {
		t.Fatalf("growth key = %q", key)
	}
	gc, ok := b.GrowthCheck(key)
	if !ok {
		t.Fatal("rulebook declares no growth check")
	}

	res, err := b.RunGrowth(context.Background(), gc, engine.GrowthInput{
		Key:   key,
		Name:  "心理学",
		Value: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Grew || res.Gain != 3 || res.After != 63 {
		t.Fatalf("grew=%v gain=%d after=%d", res.Grew, res.Gain, res.After)
	}
	if res.Kind != "growth_check" || res.Target != 60 {
		t.Fatalf("kind=%s target=%d", res.Kind, res.Target)
	}
}

// A roll at or below the current value does not grow the skill.
func TestGrowthNoImprovement(t *testing.T) {
	// 1d100 -> 42 <= 60: no growth, so no gain roll is taken.
	b := scriptedBook(t, 41)
	gc, _ := b.GrowthCheck("skill:psychology")
	res, err := b.RunGrowth(context.Background(), gc, engine.GrowthInput{
		Key:   "skill:psychology",
		Name:  "心理学",
		Value: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Grew || res.Gain != 0 || res.After != 60 {
		t.Fatalf("grew=%v gain=%d after=%d", res.Grew, res.Gain, res.After)
	}
	if res.Explanation == "" {
		t.Fatal("result must explain the outcome")
	}
}

// The cap limits the grown value.
func TestGrowthCap(t *testing.T) {
	// 1d100 -> 96 > 95, gain 1d10 -> 10, capped at 99.
	b := scriptedBook(t, 95, 9)
	gc, _ := b.GrowthCheck("skill:psychology")
	res, err := b.RunGrowth(context.Background(), gc, engine.GrowthInput{
		Key:   "skill:psychology",
		Name:  "心理学",
		Value: 95,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.After != 99 || res.Gain != 4 {
		t.Fatalf("after=%d gain=%d, want 99/4", res.After, res.Gain)
	}
}

// The Investigator Handbook's 50/75 limits are creation-time personal
// interest caps, not growth caps: handgun and pilot grow with the default
// rule up to 99.
func TestGrowthNoCreationCaps(t *testing.T) {
	b := coc7Book(t, nil)
	for _, q := range []string{"射击（手枪）", "驾驶（特殊）"} {
		key, ok := b.GrowthKey(q)
		if !ok {
			t.Fatalf("growth key for %q missing", q)
		}
		gc, ok := b.GrowthCheck(key)
		if !ok {
			t.Fatalf("no growth check for %q", q)
		}
		if gc.Cap() != 99 {
			t.Fatalf("%s cap = %d, want the default 99", q, gc.Cap())
		}
	}
}

// The comparison target can be overridden by the caller.
func TestGrowthExplicitTarget(t *testing.T) {
	// 1d100 -> 95 > explicit target 90 (though > the sheet value 60 too),
	// gain 1d10 -> 3.
	b := scriptedBook(t, 94, 2)
	gc, _ := b.GrowthCheck("skill:psychology")
	target := 90
	res, err := b.RunGrowth(context.Background(), gc, engine.GrowthInput{
		Key:    "skill:psychology",
		Name:   "心理学",
		Value:  60,
		Target: &target,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Grew {
		t.Fatalf("expected growth against the explicit target: %+v", res)
	}
	if res.Target != 90 {
		t.Fatalf("target = %d, want 90", res.Target)
	}
}

// Growth declarations are validated at load time.
func TestGrowthConfigValidation(t *testing.T) {
	data, err := rulebooks.FS.ReadFile("coc7.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		marker string
		repl   string
	}{
		{
			name:   "empty condition",
			marker: `    condition: "roll > target"`,
			repl:   `    condition: ""`,
		},
		{
			name:   "invalid gain",
			marker: `    gain: "1d10"`,
			repl:   `    gain: "1d"`,
		},
		{
			name:   "invalid condition",
			marker: `    condition: "roll > target"`,
			repl:   `    condition: "roll > nope"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			variant := bytes.Replace(data, []byte(tc.marker), []byte(tc.repl), 1)
			_, err := engine.ParseBook(variant, "variant.yaml", nil)
			if !apperr.IsCode(err, apperr.ConfigInvalid) {
				t.Fatalf("code = %v, want CONFIG_INVALID", err)
			}
		})
	}
}

// Books without growth checks simply have none.
func TestGrowthAbsent(t *testing.T) {
	book, err := engine.ParseBook([]byte(minimalBook), "minimal.yaml", dice.New(dice.NewScriptedSource(1)))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := book.GrowthCheck("skill:psychology"); ok {
		t.Fatal("book without growth_checks must not report a growth check")
	}
}

// minimalBook is a tiny rulebook without growth checks or tables.
const minimalBook = `schema_version: "1"
id: "mini"
name: "Mini"
system_version: "0.1.0"
rules_version: "test"
description: "minimal"
attributes:
  - { id: "STR", name: "力量" }
skills:
  psychology: { base: 10, name: "心理学" }
limits:
  characteristic: { min: 0, max: 99 }
  skill:          { min: 0, max: 99 }
  target:         { min: 0, max: 99 }
mechanic:
  max_bonus_penalty: 10
  roll: "1d100"
  meets: "roll <= target"
  bonus_keep: lowest
  penalty_keep: highest
  label: "d100"
checks:
  levels:
    - { id: success, name: "成功", when: "roll <= target" }
    - { id: fail, name: "失败" }
opposed:
  fumble_level: fail
  tie_break: none
`
