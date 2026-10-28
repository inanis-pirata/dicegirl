package engine_test

import (
	"context"
	"testing"

	"dicegirl/internal/character"
	"dicegirl/internal/engine"
	"dicegirl/rulebooks"
)

// Every embedded rulebook must compile; each built-in book is data validated
// by the same loader users get.
func TestBuiltinRulebooksLoad(t *testing.T) {
	books, err := engine.LoadBuiltins(rulebooks.FS, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"coc7": false, "dnd5e": false}
	for _, b := range books {
		id := b.Manifest().ID
		if _, ok := want[id]; ok {
			want[id] = true
		}
		if err := b.ValidateConfig(context.Background()); err != nil {
			t.Fatalf("%s revalidation: %v", id, err)
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("embedded %s rulebook not loaded", id)
		}
	}
}

// The D&D book declares the d20 family and its own success ladder.
func TestDnD5eBook(t *testing.T) {
	data, err := rulebooks.FS.ReadFile("dnd5e.yaml")
	if err != nil {
		t.Fatal(err)
	}
	b, err := engine.ParseBook(data, "dnd5e.yaml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Manifest().RulesVersion; got != "5e-2014" {
		t.Fatalf("rules_version = %q", got)
	}
	res, err := b.Check(context.Background(), engine.CheckInput{Target: 15})
	if err != nil {
		t.Fatal(err)
	}
	if res.RollDetail == nil || res.RollDetail.Mechanic != "expression" {
		t.Fatalf("roll detail = %+v, want the expression mechanic", res.RollDetail)
	}
	if len(b.Skills()) != 18 {
		t.Fatalf("skills = %d, want the 18 5e skills", len(b.Skills()))
	}
	if sk, ok := b.FindSkill("巧手"); !ok || sk.ID != "sleight_of_hand" {
		t.Fatalf("FindSkill(巧手) = %+v, %v", sk, ok)
	}
	if a, ok := b.FindAttribute("STR"); !ok || a.Name != "力量" {
		t.Fatalf("FindAttribute(STR) = %+v, %v", a, ok)
	}
	// A DEX 8 character has a -1 modifier (floor division, not truncation).
	// LEVEL/HIT_DIE are part of the sheet contract since T17a: prof and
	// max_hp are level-driven formulas.
	d, err := b.Derive(&character.Character{Attributes: map[string]int{
		"STR": 10, "DEX": 8, "CON": 10, "INT": 9, "WIS": 10, "CHA": 10,
		"LEVEL": 1, "HIT_DIE": 8,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := d.Int("dex_mod"); !ok || v != -1 {
		t.Fatalf("dex_mod = %d, %v, want -1", v, ok)
	}
	if v, ok := d.Int("int_mod"); !ok || v != -1 {
		t.Fatalf("int_mod = %d, %v, want -1", v, ok)
	}
	// Proficiency steps: 1-4 → 2, 5-8 → 3, 9-12 → 4 (2 + (level-1)/4).
	for _, tc := range []struct{ level, want int }{{1, 2}, {4, 2}, {5, 3}, {8, 3}, {9, 4}, {17, 6}} {
		d, err := b.Derive(&character.Character{Attributes: map[string]int{
			"STR": 10, "DEX": 10, "CON": 10, "INT": 10, "WIS": 10, "CHA": 10,
			"LEVEL": tc.level, "HIT_DIE": 8,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if v, ok := d.Int("prof"); !ok || v != tc.want {
			t.Fatalf("prof at level %d = %d, %v, want %d", tc.level, v, ok, tc.want)
		}
	}
}

func TestFindSkillAndBase(t *testing.T) {
	b := coc7Book(t, nil)
	for _, q := range []string{"心理学", "PSYCHOLOGY", "psychology"} {
		sk, ok := b.FindSkill(q)
		if !ok || sk.ID != "psychology" {
			t.Errorf("FindSkill(%q) = %+v, %v", q, sk, ok)
		}
	}
	dodge, ok := b.FindSkill("dodge")
	if !ok {
		t.Fatal("dodge missing")
	}
	base, err := b.SkillBase(dodge, map[string]int{"DEX": 70})
	if err != nil || base != 35 {
		t.Fatalf("dodge base = %d, %v", base, err)
	}
	own, ok := b.FindSkill("language_own")
	if !ok {
		t.Fatal("language_own missing")
	}
	base, err = b.SkillBase(own, map[string]int{"EDU": 70})
	if err != nil || base != 70 {
		t.Fatalf("language_own base = %d, %v", base, err)
	}
	if _, ok := b.FindAttribute("力量"); !ok {
		t.Error("Chinese attribute label not resolved")
	}
}

func validAttrs() map[string]int {
	return map[string]int{
		"STR": 50, "CON": 60, "SIZ": 70, "DEX": 55, "APP": 60,
		"INT": 70, "POW": 55, "EDU": 70, "LUCK": 55,
	}
}

func validateSheet(t *testing.T, b *engine.Book, attrs, skills map[string]int) error {
	t.Helper()
	c := &character.Character{Attributes: attrs, Skills: skills}
	return b.ValidateCharacter(context.Background(), c)
}

func TestValidateCharacter(t *testing.T) {
	b := coc7Book(t, nil)

	// A well-formed sheet, including a personal science specialty unknown to
	// the rulebook's skill list, passes: only ranges apply to skill values.
	if err := validateSheet(t, b, validAttrs(), map[string]int{
		"psychology":        60,
		"science_astronomy": 40,
	}); err != nil {
		t.Fatalf("valid sheet rejected: %v", err)
	}

	attrs := validAttrs()
	attrs["FOO"] = 50
	if err := validateSheet(t, b, attrs, nil); err == nil {
		t.Error("unknown characteristic FOO accepted")
	}

	attrs = validAttrs()
	attrs["STR"] = 120
	if err := validateSheet(t, b, attrs, nil); err == nil {
		t.Error("STR 120 above characteristic range accepted")
	}

	if err := validateSheet(t, b, validAttrs(), map[string]int{"psychology": 150}); err == nil {
		t.Error("skill 150 above skill range accepted")
	}
}
