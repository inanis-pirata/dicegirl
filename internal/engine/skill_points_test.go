package engine_test

import (
	"testing"

	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine"
	"dicegirl/rulebooks"
)

// allocCard is a complete CoC7 sheet: EDU 60 → 240 occupation points,
// INT 65 → 130 interest points.
func allocCard() *character.Character {
	return &character.Character{
		SchemaVersion: "1",
		ID:            "c-alloc-test",
		Name:          "加点员",
		SystemID:      "coc7",
		Attributes: map[string]int{
			"STR": 50, "CON": 60, "SIZ": 65, "DEX": 70, "APP": 55,
			"INT": 65, "POW": 55, "EDU": 60, "LUCK": 45,
		},
		Skills: map[string]int{},
	}
}

// The pools report the variant's declaration evaluated against the sheet.
func TestSkillPointPoolsReport(t *testing.T) {
	b := coc7Book(t, nil)
	c := allocCard()

	pools, err := b.SkillPointPools("", c)
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 2 {
		t.Fatalf("pools = %d, want 2", len(pools))
	}
	if pools[0].ID != "occupation" || pools[0].Points != 240 || pools[0].Spent != 0 || pools[0].Cap != 90 {
		t.Fatalf("occupation pool: %+v", pools[0])
	}
	if pools[1].ID != "interest" || pools[1].Points != 130 || pools[1].Cap != 75 {
		t.Fatalf("interest pool: %+v", pools[1])
	}

	// The 6th-edition variant overrides the pools with its own convention.
	pools, err = b.SkillPointPools("6", c)
	if err != nil {
		t.Fatal(err)
	}
	if pools[0].Points != 1200 || pools[0].Cap != 99 {
		t.Fatalf("6e occupation pool: %+v", pools[0])
	}
	if pools[1].Points != 650 || pools[1].Cap != 99 {
		t.Fatalf("6e interest pool: %+v", pools[1])
	}

	// Spent points come back out of the card's bookkeeping.
	c.SkillPoints = map[string]map[string]int{"occupation": {"spot_hidden": 40}}
	pools, err = b.SkillPointPools("", c)
	if err != nil {
		t.Fatal(err)
	}
	if pools[0].Spent != 40 || pools[0].Left != 200 {
		t.Fatalf("spent bookkeeping: %+v", pools[0])
	}

	// A book whose generation declares no pools reports none.
	data, err := rulebooks.FS.ReadFile("dnd5e.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dnd, err := engine.ParseBook(data, "dnd5e.yaml", nil)
	if err != nil {
		t.Fatal(err)
	}
	pools, err = dnd.SkillPointPools("", allocCard())
	if err != nil || pools != nil {
		t.Fatalf("dnd5e pools = %v, err = %v (want nil, nil)", pools, err)
	}

	// Missing characteristics cannot feed the formulas.
	thin := allocCard()
	thin.Attributes = map[string]int{"STR": 50}
	if _, err := b.SkillPointPools("", thin); !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("thin card: %v", err)
	}
}

// Allocation starts skills at their rulebook base, tracks the per-skill
// spending and enforces the pool budget and the per-skill caps.
func TestSkillAllocationSpendAndRefund(t *testing.T) {
	b := coc7Book(t, nil)
	c := allocCard()

	// 侦查 (base 25) +40 from the occupation pool, addressed by alias/name.
	results, err := b.AllocateSkillPoints("", "职业", c, []engine.SkillSpend{
		{Skill: "侦查", Points: 40},
		{Skill: "图书馆使用", Points: 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Skill != "spot_hidden" || !results[0].FromBase ||
		results[0].Before != 25 || results[0].After != 65 || results[0].Cap != 90 {
		t.Fatalf("spot_hidden result: %+v", results[0])
	}
	if results[1].Before != 20 || results[1].After != 40 {
		t.Fatalf("library_use result: %+v", results[1])
	}
	if got := c.Skills["spot_hidden"]; got != 65 {
		t.Fatalf("spot_hidden = %d, want 65", got)
	}
	if got := c.SkillPoints["occupation"]["spot_hidden"]; got != 40 {
		t.Fatalf("bookkeeping = %d, want 40", got)
	}
	if results[1].PoolSpent != 60 || results[1].PoolLeft != 180 {
		t.Fatalf("pool after spends: %+v", results[1])
	}

	// A base_expr skill starts from its computed base (闪避 = DEX/2 = 35).
	results, err = b.AllocateSkillPoints("", "occupation", c, []engine.SkillSpend{
		{Skill: "闪避", Points: 15},
	})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Before != 35 || results[0].After != 50 {
		t.Fatalf("dodge result: %+v", results[0])
	}

	// A skill already on the card (hand-set or grown) spends from its value.
	c.Skills["psychology"] = 60
	results, err = b.AllocateSkillPoints("", "occupation", c, []engine.SkillSpend{
		{Skill: "psychology", Points: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Before != 60 || results[0].After != 70 || results[0].FromBase {
		t.Fatalf("psychology result: %+v", results[0])
	}

	// Refunds return points and cannot dig below what was allocated or
	// below the base value.
	if _, err := b.AllocateSkillPoints("", "occupation", c, []engine.SkillSpend{
		{Skill: "侦查", Points: -10},
	}); err != nil {
		t.Fatal(err)
	}
	if got := c.Skills["spot_hidden"]; got != 55 {
		t.Fatalf("spot_hidden after refund = %d, want 55", got)
	}
	if got := c.SkillPoints["occupation"]["spot_hidden"]; got != 30 {
		t.Fatalf("bookkeeping after refund = %d, want 30", got)
	}
	wantInvalid(t, "refund beyond allocation", func() error {
		_, err := b.AllocateSkillPoints("", "occupation", c, []engine.SkillSpend{
			{Skill: "侦查", Points: -31},
		})
		return err
	})
	// 心理学 was hand-set to 60; only 10 points are allocated to it, so a
	// larger refund cannot be settled.
	wantInvalid(t, "refund beyond hand-set value", func() error {
		_, err := b.AllocateSkillPoints("", "occupation", c, []engine.SkillSpend{
			{Skill: "心理学", Points: -11},
		})
		return err
	})
}

// The budget and the caps reject over-spending, and per-skill cap overrides
// win over the pool cap.
func TestSkillAllocationLimits(t *testing.T) {
	b := coc7Book(t, nil)

	// Pool budget: EDU 60 → 240 points, not 241.
	c := allocCard()
	wantInvalid(t, "budget", func() error {
		_, err := b.AllocateSkillPoints("", "occupation", c, []engine.SkillSpend{
			{Skill: "侦查", Points: 241},
		})
		return err
	})

	// Pool cap: the occupation pool tops a skill at 90 (25 + 66 = 91).
	wantInvalid(t, "pool cap", func() error {
		_, err := b.AllocateSkillPoints("", "occupation", allocCard(), []engine.SkillSpend{
			{Skill: "侦查", Points: 66},
		})
		return err
	})

	// Cap override: interest points top 射击（手枪）at 50 (base 20).
	wantInvalid(t, "cap override", func() error {
		_, err := b.AllocateSkillPoints("", "interest", allocCard(), []engine.SkillSpend{
			{Skill: "手枪射击", Points: 31},
		})
		return err
	})
	// 30 points land exactly on the override.
	results, err := b.AllocateSkillPoints("", "interest", allocCard(), []engine.SkillSpend{
		{Skill: "firearms_handgun", Points: 30},
	})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].After != 50 || results[0].Cap != 50 {
		t.Fatalf("handgun result: %+v", results[0])
	}

	// A failed command leaves nothing behind (the caller never persists,
	// and the error surfaces before any bookkeeping is written).
	c = allocCard()
	if _, err := b.AllocateSkillPoints("", "occupation", c, []engine.SkillSpend{
		{Skill: "侦查", Points: 40},
		{Skill: "图书馆使用", Points: 999},
	}); err == nil {
		t.Fatal("second spend must fail")
	}
}

// Unknown pools and skills are rejected; the empty spend list is a usage
// error.
func TestSkillAllocationErrors(t *testing.T) {
	b := coc7Book(t, nil)
	c := allocCard()
	for _, tc := range []struct {
		variant, pool string
		spends        []engine.SkillSpend
	}{
		{"", "hobby", []engine.SkillSpend{{Skill: "侦查", Points: 10}}},
		{"nope", "occupation", []engine.SkillSpend{{Skill: "侦查", Points: 10}}},
		{"", "occupation", []engine.SkillSpend{{Skill: "太空科学", Points: 10}}},
		{"", "occupation", nil},
		{"", "occupation", []engine.SkillSpend{{Skill: "侦查", Points: 0}}},
	} {
		if _, err := b.AllocateSkillPoints(tc.variant, tc.pool, c, tc.spends); !apperr.IsCode(err, apperr.InvalidArgument) {
			t.Fatalf("variant=%q pool=%q spends=%v: %v", tc.variant, tc.pool, tc.spends, err)
		}
	}
}

// Bad pool declarations fail at load time.
func TestSkillPoolCompileErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pools string
	}{
		{"unknown attribute", `
  skill_points:
    - { id: occ, name: "职业", points: "WIT*4" }`},
		{"missing points", `
  skill_points:
    - { id: occ, name: "职业" }`},
		{"duplicate pool", `
  skill_points:
    - { id: occ, name: "职业", points: "STR*4" }
    - { id: occ, name: "再职", points: "STR*2" }`},
		{"negative cap", `
  skill_points:
    - { id: occ, name: "职业", points: "STR*4", cap: -1 }`},
		{"override unknown skill", `
  skill_points:
    - { id: occ, name: "职业", points: "STR*4", cap_overrides: [{ skill: nope, cap: 50 }] }`},
		{"override zero cap", `
  skill_points:
    - { id: occ, name: "职业", points: "STR*4", cap_overrides: [{ skill: psychology, cap: 0 }] }`},
	} {
		src := minimalBook + `
generation:
  characteristics:
    - { attr: STR, expr: "3d6*5" }` + tc.pools
		if _, err := engine.ParseBook([]byte(src), "pools.yaml", dice.New(dice.NewScriptedSource(1))); !apperr.IsCode(err, apperr.ConfigInvalid) {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
}

// A variant may override the base pools.
func TestSkillPoolVariantOverride(t *testing.T) {
	// The same shape as coc7: base-level pools plus the default variant
	// formed from them (the synthetic "std"), and one edition overriding.
	src := minimalBook + `
generation:
  default_variant: "std"
  characteristics:
    - { attr: STR, expr: "3d6*5" }
  skill_points:
    - { id: occ, name: "标准池", points: "STR*2", cap: 80 }
  variants:
    - id: "alt"
      name: "另版"
      skill_points:
        - { id: occ, name: "另版池", points: "STR*4", cap: 99 }`
	b, err := engine.ParseBook([]byte(src), "pools.yaml", dice.New(dice.NewScriptedSource(1)))
	if err != nil {
		t.Fatal(err)
	}
	c := &character.Character{Attributes: map[string]int{"STR": 50}}
	pools, err := b.SkillPointPools("", c)
	if err != nil {
		t.Fatal(err)
	}
	if pools[0].Points != 100 || pools[0].Cap != 80 {
		t.Fatalf("base pool: %+v", pools[0])
	}
	pools, err = b.SkillPointPools("alt", c)
	if err != nil {
		t.Fatal(err)
	}
	if pools[0].Points != 200 || pools[0].Cap != 99 {
		t.Fatalf("variant pool: %+v", pools[0])
	}
}

// wantInvalid fails the test unless f returns InvalidArgument.
func wantInvalid(t *testing.T, what string, f func() error) {
	t.Helper()
	err := f()
	if err == nil {
		t.Fatalf("%s: want InvalidArgument, got success", what)
	}
	if !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("%s: %v", what, err)
	}
}
