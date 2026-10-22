package engine_test

import (
	"context"
	"strings"
	"testing"

	"dicegirl/internal/apperr"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine"
)

// d20Book is a minimal rulebook using the d20 mechanic: a roll of 20 or
// more meets the target, 1 always fumbles.
const d20Book = `schema_version: "1"
id: "d20test"
name: "d20 Test"
system_version: "0.1.0"
rules_version: "test"
description: "d20 mechanic"
attributes:
  - { id: "STR", name: "力量" }
skills:
  athletics: { base: 10, name: "运动" }
limits:
  characteristic: { min: 0, max: 30 }
  skill:          { min: 0, max: 20 }
  target:         { min: 1, max: 30 }
mechanic:
  max_bonus_penalty: 10
  roll: "1d20"
  meets: "roll >= target"
  bonus_keep: highest
  penalty_keep: lowest
  label: "d20"
checks:
  levels:
    - { id: fumble,   name: "大失败", when: "roll == 1" }
    - { id: critical, name: "大成功", when: "roll == 20" }
    - { id: success,  name: "成功",   when: "roll >= target" }
    - { id: fail,     name: "失败" }
opposed:
  fumble_level: fumble
  tie_break: higher_target
`

// d20BookWith compiles the d20 test book with a scripted roller.
func d20BookWith(t *testing.T, values ...int) *engine.Book {
	t.Helper()
	b, err := engine.ParseBook([]byte(d20Book), "d20test.yaml",
		dice.New(dice.NewScriptedSource(values...)))
	if err != nil {
		t.Fatalf("d20 book invalid: %v", err)
	}
	return b
}

// d20BookTemplate builds a d20 rulebook with an optional difficulties
// section (used to test difficulties and invalid mechanic types).
func d20BookTemplate(difficulties string) string {
	return `schema_version: "1"
id: "d20test"
name: "d20 Test"
system_version: "0.1.0"
rules_version: "test"
description: "d20 mechanic"
attributes:
  - { id: "STR", name: "力量" }
skills:
  athletics: { base: 10, name: "运动" }
limits:
  characteristic: { min: 0, max: 30 }
  skill:          { min: 0, max: 20 }
  target:         { min: 1, max: 30 }
mechanic:
  max_bonus_penalty: 10
  roll: "1d20"
  meets: "roll >= target"
  bonus_keep: highest
  penalty_keep: lowest
  label: "d20"
checks:
  levels:
    - { id: fumble,   name: "大失败", when: "roll == 1" }
    - { id: critical, name: "大成功", when: "roll == 20" }
    - { id: success,  name: "成功",   when: "roll >= target" }
    - { id: fail,     name: "失败" }
` + difficulties + `opposed:
  fumble_level: fumble
  tie_break: higher_target
`
}

// The d20 mechanic rolls one d20 and compares it with the target.
func TestD20Check(t *testing.T) {
	b := d20BookWith(t, 14) // face 15
	res, err := b.Check(context.Background(), engine.CheckInput{Target: 15, SkillName: "运动"})
	if err != nil {
		t.Fatal(err)
	}
	if res.RawRoll != 15 || res.LevelCode != "success" {
		t.Fatalf("roll=%d level=%s, want 15/success", res.RawRoll, res.LevelCode)
	}
	if res.RollDetail == nil || res.RollDetail.Mechanic != "expression" {
		t.Fatalf("roll detail = %+v", res.RollDetail)
	}
	if len(res.RollDetail.Dice) != 1 || res.RollDetail.Dice[0].Value != 15 {
		t.Fatalf("dice = %+v", res.RollDetail.Dice)
	}
}

// A natural 1 fumbles, a natural 20 is a critical success.
func TestD20NaturalOutcomes(t *testing.T) {
	b := d20BookWith(t, 0)
	res, err := b.Check(context.Background(), engine.CheckInput{Target: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.LevelCode != "fumble" {
		t.Fatalf("level = %s, want fumble", res.LevelCode)
	}
	b = d20BookWith(t, 19)
	res, err = b.Check(context.Background(), engine.CheckInput{Target: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.LevelCode != "critical" {
		t.Fatalf("level = %s, want critical", res.LevelCode)
	}
}

// Bonus dice mean advantage (roll two, keep the higher), penalty dice mean
// disadvantage (roll two, keep the lower).
func TestD20BonusPenalty(t *testing.T) {
	b := d20BookWith(t, 5, 12) // faces 6 and 13
	res, err := b.Check(context.Background(), engine.CheckInput{Target: 10, Bonus: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.RawRoll != 13 {
		t.Fatalf("advantage roll = %d, want 13 (keep the higher)", res.RawRoll)
	}
	if len(res.RollDetail.Dice) != 2 || res.RollDetail.KeptIndex != 1 {
		t.Fatalf("dice = %+v", res.RollDetail.Dice)
	}
	if res.RollDetail.Effect != "bonus" {
		t.Fatalf("effect = %s", res.RollDetail.Effect)
	}

	b = d20BookWith(t, 5, 12)
	res, err = b.Check(context.Background(), engine.CheckInput{Target: 10, Penalty: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.RawRoll != 6 {
		t.Fatalf("disadvantage roll = %d, want 6 (keep the lower)", res.RawRoll)
	}
	if res.RollDetail.Effect != "penalty" {
		t.Fatalf("effect = %s", res.RollDetail.Effect)
	}
}

// exprBook is the data-declared mechanic: the check rolls a dice expression
// and the `meets` formula decides success. No Go code knows the game.
const exprBook = `schema_version: "1"
id: "exprtest"
name: "Expression Test"
system_version: "0.1.0"
rules_version: "test"
description: "expression mechanic"
attributes:
  - { id: "STR", name: "力量" }
skills:
  athletics: { base: 10, name: "运动" }
limits:
  characteristic: { min: 0, max: 30 }
  skill:          { min: 0, max: 20 }
  target:         { min: 1, max: 30 }
mechanic:
  max_bonus_penalty: 10
  roll: "1d20"
  meets: "roll >= target"
  bonus_keep: highest
  penalty_keep: lowest
  label: "d20"
checks:
  levels:
    - { id: fumble,   name: "大失败", when: "roll == 1" }
    - { id: critical, name: "大成功", when: "roll == 20" }
    - { id: success,  name: "成功",   when: "roll >= target" }
    - { id: fail,     name: "失败" }
opposed:
  fumble_level: fumble
  tie_break: higher_target
`

// parseBookWithRoller compiles a book with a scripted roller.
func parseBookWithRoller(t *testing.T, src string, values ...int) *engine.Book {
	t.Helper()
	b, err := engine.ParseBook([]byte(src), "expr.yaml", dice.New(dice.NewScriptedSource(values...)))
	if err != nil {
		t.Fatalf("book invalid: %v", err)
	}
	return b
}

// The expression family behaves like the built-in d20 family: one roll,
// bonus keeps the higher, penalty keeps the lower.
func TestExpressionMechanic(t *testing.T) {
	b := parseBookWithRoller(t, exprBook, 14)
	res, err := b.Check(context.Background(), engine.CheckInput{Target: 15})
	if err != nil {
		t.Fatal(err)
	}
	if res.RawRoll != 15 || res.LevelCode != "success" {
		t.Fatalf("roll=%d level=%s, want 15/success", res.RawRoll, res.LevelCode)
	}
	if res.RollDetail == nil || res.RollDetail.Mechanic != "expression" {
		t.Fatalf("roll detail = %+v", res.RollDetail)
	}
	if !strings.Contains(res.Explanation, "d20 = 15") {
		t.Fatalf("explanation = %q", res.Explanation)
	}

	b = parseBookWithRoller(t, exprBook, 5, 12) // faces 6 and 13
	res, err = b.Check(context.Background(), engine.CheckInput{Target: 10, Bonus: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.RawRoll != 13 || res.RollDetail.KeptIndex != 1 || res.RollDetail.Effect != "bonus" {
		t.Fatalf("advantage = %+v", res.RollDetail)
	}
	if !strings.Contains(res.Explanation, "取高") {
		t.Fatalf("explanation = %q", res.Explanation)
	}

	b = parseBookWithRoller(t, exprBook, 5, 12)
	res, err = b.Check(context.Background(), engine.CheckInput{Target: 10, Penalty: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.RawRoll != 6 || res.RollDetail.Effect != "penalty" {
		t.Fatalf("disadvantage = %+v", res.RollDetail)
	}
	if !strings.Contains(res.Explanation, "取低") {
		t.Fatalf("explanation = %q", res.Explanation)
	}
}

// The expression family is direction-agnostic: a roll-under system declares
// its own meets formula and keep directions.
func TestExpressionMechanicRollUnder(t *testing.T) {
	src := strings.Replace(exprBook, "roll: \"1d20\"\n  meets: \"roll >= target\"",
		"roll: \"3d6\"\n  meets: \"roll <= target\"", 1)
	src = strings.Replace(src, "bonus_keep: highest\n  penalty_keep: lowest",
		"bonus_keep: lowest\n  penalty_keep: highest", 1)
	src = strings.Replace(src, "when: \"roll >= target\"", "when: \"roll <= target\"", 1)
	// First trial 2+3+4=9, the bonus trial 5+6+6=17; a bonus keeps the lower.
	b := parseBookWithRoller(t, src, 1, 2, 3, 4, 5, 5)
	res, err := b.Check(context.Background(), engine.CheckInput{Target: 12, Bonus: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.RawRoll != 9 || res.LevelCode != "success" {
		t.Fatalf("roll=%d level=%s, want 9/success", res.RawRoll, res.LevelCode)
	}
	if !strings.Contains(res.Explanation, "取低") {
		t.Fatalf("explanation = %q", res.Explanation)
	}
}

// Invalid expression declarations fail at load time.
func TestExpressionMechanicInvalid(t *testing.T) {
	cases := map[string]string{
		"unsupported type": strings.Replace(exprBook, "  roll: \"1d20\"", "  type: d20\n  roll: \"1d20\"", 1),
		"missing meets":    strings.Replace(exprBook, "  meets: \"roll >= target\"\n", "", 1),
		"bad expression":   strings.Replace(exprBook, "roll: \"1d20\"", "roll: \"1d0\"", 1),
		"bad keep":         strings.Replace(exprBook, "bonus_keep: highest", "bonus_keep: highestest", 1),
		"unknown meets":    strings.Replace(exprBook, "roll >= target", "roll >= missing", 1),
	}
	for name, src := range cases {
		if _, err := engine.ParseBook([]byte(src), "expr.yaml", nil); err == nil {
			t.Errorf("%s: expected a configuration error", name)
		}
	}
}

// A formula difficulty shifts the target (DC+5 for "hard").
func TestD20Difficulty(t *testing.T) {
	src := d20BookTemplate(`  difficulties:
    - { id: hard, name: "困难", expr: "target+5" }
`)
	b, err := engine.ParseBook([]byte(src), "d20hard.yaml", dice.New(dice.NewScriptedSource(9, 9)))
	if err != nil {
		t.Fatal(err)
	}
	// 10 is not enough against DC 10 + 5, but is against the plain DC.
	res, err := b.Check(context.Background(), engine.CheckInput{Target: 10, Difficulty: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Target != 15 || res.LevelCode != "fail" {
		t.Fatalf("hard: target=%d level=%s", res.Target, res.LevelCode)
	}
	res, err = b.Check(context.Background(), engine.CheckInput{Target: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.LevelCode != "success" {
		t.Fatalf("plain: level = %s, want success", res.LevelCode)
	}
}

// A built-in mechanic type is now a configuration error, never a silent
// fallback to another family.
func TestUnknownMechanicRejected(t *testing.T) {
	src := d20BookTemplate("")
	src = strings.Replace(src, "mechanic:\n", "mechanic:\n  type: d7\n", 1)
	_, err := engine.ParseBook([]byte(src), "broken.yaml", nil)
	if !apperr.IsCode(err, apperr.ConfigInvalid) {
		t.Fatalf("code = %v, want CONFIG_INVALID", err)
	}
}
