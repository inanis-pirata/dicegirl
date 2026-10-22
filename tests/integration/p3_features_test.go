package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// P3: random tables declared by a rulebook. They can be rolled without a
// character.
func TestTableRolls(t *testing.T) {
	root := t.TempDir()

	// `ti` and `li` are the Dice! aliases of the CoC7 insanity tables.
	code, out, errs := runCLI(t, root, "ti")
	if code != 0 || !strings.Contains(out, "临时疯狂症状") {
		t.Fatalf("ti code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "li")
	if code != 0 || !strings.Contains(out, "总结性疯狂") {
		t.Fatalf("li code=%d out=%q err=%s", code, out, errs)
	}

	// `.tb` lists and rolls the tables of the rulebook.
	code, out, errs = runCLI(t, root, ".tb", "list")
	if code != 0 {
		t.Fatalf("table list code=%d err=%s", code, errs)
	}
	for _, want := range []string{"temporary_insanity", "summary_insanity"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table list missing %q: %q", want, out)
		}
	}
	code, out, errs = runCLI(t, root, ".tb", "temporary_insanity")
	if code != 0 || !strings.Contains(out, "临时疯狂症状") {
		t.Fatalf("table roll code=%d out=%q err=%s", code, out, errs)
	}

	// Unknown tables are usage errors.
	for _, args := range [][]string{
		{".tb", "nope"},
		{".tb"},
	} {
		if code, _, _ := runCLI(t, root, args...); code != 2 {
			t.Fatalf("%v: code=%d, want 2", args, code)
		}
	}

	// The structured contract carries the table, the roll and the row text.
	code, out, errs = runCLI(t, root, "--json", ".tb", "summary_insanity")
	if code != 0 {
		t.Fatalf("json table code=%d err=%s", code, errs)
	}
	for _, want := range []string{`"kind":"table_roll"`, `"table_id":"summary_insanity"`, `"value":`, `"text":`} {
		if !strings.Contains(out, want) {
			t.Fatalf("json table missing %q: %q", want, out)
		}
	}
}

// An invalid table declaration fails the rulebook compilation, so the
// command refuses to start and `config validate` reports CONFIG_INVALID.
func TestInvalidTableConfigRejected(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "rulebooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	broken := `schema_version: "1"
id: "broken"
name: "Broken"
system_version: "0.1.0"
rules_version: "test"
description: "rows do not cover the domain"
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
tables:
  - id: broken
    name: "缺口表"
    roll: "1d10"
    rows:
      - { range: "1", text: "只有一行" }
`
	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errs := runCLI(t, root, ".sys", "validate")
	if code != 1 || !strings.Contains(errs, "CONFIG_INVALID") {
		t.Fatalf("config validate code=%d err=%q", code, errs)
	}
}

// The growth check accepts a custom growth amount (the Dice!
// `.en 幸运 +1D3/1D10` form): one of the alternatives is picked at random.
func TestGrowthCustomAmount(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲", "psychology=60")

	// The custom amount is rolled and added to the skill.
	code, out, errs := runCLISeed(t, root, 1, "en", "心理学", "1d10", "测试")
	if code != 0 {
		t.Fatalf("en code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "成长") {
		t.Fatalf("en output: %q", out)
	}
	code, out, errs = runCLI(t, root, "st", "show", "心理学")
	if code != 0 {
		t.Fatalf("show code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "psychology 6") {
		t.Fatalf("skill not grown: %q", out)
	}

	// An alternative list ("1d3/1d10") is accepted and resolves to one of
	// the two.
	code, out, errs = runCLISeed(t, root, 1, "en", "心理学", "+1d3/1d10", "Pulp 幸运成长")
	if code != 0 {
		t.Fatalf("en with alternatives code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "成长") && !strings.Contains(out, "未成长") {
		t.Fatalf("en with alternatives output: %q", out)
	}
}
func TestGrowthCheck(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲")

	// Seed 1 rolls 53 on the growth d100: 53 <= 60, so the skill grows.
	code, out, errs := runCLISeed(t, root, 1, "en", "心理学")
	if code != 0 {
		t.Fatalf("en code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "成长") {
		t.Fatalf("en output: %q", out)
	}
	code, out, errs = runCLI(t, root, "st", "show", "心理学")
	if code != 0 || !strings.Contains(out, "psychology 61") {
		t.Fatalf("growth was not written to the sheet: %q", out)
	}

	// A failed growth leaves the sheet untouched.
	code, out, errs = runCLISeed(t, root, 3, "en", "心理学")
	if code != 0 || !strings.Contains(out, "未成长") {
		t.Fatalf("en code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "st", "show", "心理学")
	if code != 0 || !strings.Contains(out, "psychology 61") {
		t.Fatalf("failed growth changed the sheet: %q", out)
	}

	// The structured contract reports before/after/gain.
	code, out, errs = runCLISeed(t, root, 4, "--json", "en", "心理学")
	if code != 0 {
		t.Fatalf("json en code=%d err=%s", code, errs)
	}
	for _, want := range []string{`"kind":"growth_check"`, `"before":61`, `"grew":true`, `"gain":`} {
		if !strings.Contains(out, want) {
			t.Fatalf("json growth missing %q: %q", want, out)
		}
	}

	// Unknown skills are usage errors.
	for _, args := range [][]string{
		{"en", "不存在"},
		{"en"},
	} {
		if code, _, _ := runCLI(t, root, args...); code != 2 {
			t.Fatalf("%v: code=%d, want 2", args, code)
		}
	}
}

// The growth cap keeps a skill inside the declared limit, and attributes
// grow like skills.
func TestGrowthCapAndAttributes(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲", "psychology=95")

	// Seed 1 rolls 53: growth +1, but the cap is 99.
	code, out, errs := runCLISeed(t, root, 1, "en", "心理学")
	if code != 0 || !strings.Contains(out, "95 → 96") {
		t.Fatalf("growth code=%d out=%q err=%s", code, out, errs)
	}

	// A skill near the cap stops there.
	code, _, errs = runCLI(t, root, "st", "心理学=99")
	if code != 0 {
		t.Fatalf("st set code=%d err=%s", code, errs)
	}
	code, out, errs = runCLISeed(t, root, 1, "en", "心理学")
	if code != 0 || !strings.Contains(out, "未成长") || !strings.Contains(out, "上限 99") {
		t.Fatalf("capped growth code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "st", "show", "心理学")
	if code != 0 || !strings.Contains(out, "psychology 99") {
		t.Fatalf("capped sheet: %q", out)
	}

	// Characteristics can grow too.
	code, out, errs = runCLISeed(t, root, 1, "en", "力量")
	if code != 0 || !strings.Contains(out, "[力量]") {
		t.Fatalf("attribute growth code=%d out=%q err=%s", code, out, errs)
	}
}

// P3.2: the random choice round.
func TestRandomChoice(t *testing.T) {
	root := t.TempDir()

	// Options are collected in a round and drawn uniformly at random.
	code, out, errs := runCLI(t, root, "ak", "add", "苹果", "香蕉", "橘子")
	if code != 0 || !strings.Contains(out, "苹果") {
		t.Fatalf("ak add code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "ak", "show")
	if code != 0 || !strings.Contains(out, "香蕉") {
		t.Fatalf("ak show code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "ak", "del", "香蕉")
	if code != 0 || strings.Contains(out, "香蕉") {
		t.Fatalf("ak del code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "ak", "get")
	if code != 0 {
		t.Fatalf("ak get code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "抽中") {
		t.Fatalf("ak get output: %q", out)
	}
	for _, want := range []string{"苹果", "橘子"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ak get output missing %q: %q", want, out)
		}
	}
	// A draw ends the round.
	code, out, errs = runCLI(t, root, "ak", "show")
	if code != 0 || strings.Contains(out, "苹果") {
		t.Fatalf("ak show after get: %q", out)
	}

	// Drawing without options is a usage error.
	if code, _, _ := runCLI(t, root, "ak", "get"); code != 2 {
		t.Fatalf("empty draw code=%d, want 2", code)
	}

	// Usage errors.
	for _, args := range [][]string{
		{"ak"},
		{"ak", "nope"},
		{"ak", "add"},
		{"ak", "del"},
		{"ak", "del", "不存在"},
		{"ak", "add", "重复", "重复"},
	} {
		if code, _, _ := runCLI(t, root, args...); code != 2 {
			t.Fatalf("%v: code=%d, want 2", args, code)
		}
	}
}

// P3.2: random names come from the built-in data pack.
func TestRandomNames(t *testing.T) {
	root := t.TempDir()

	code, out, errs := runCLI(t, root, "name")
	if code != 0 || !strings.Contains(out, "随机姓名") {
		t.Fatalf("name code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "name", "en", "3")
	if code != 0 {
		t.Fatalf("name en 3 code=%d err=%s", code, errs)
	}
	if n := strings.Count(out, "、") + 1; n != 3 {
		t.Fatalf("want 3 names, got %d: %q", n, out)
	}
	code, out, errs = runCLI(t, root, "--json", "name", "jp", "2")
	if code != 0 {
		t.Fatalf("json name code=%d err=%s", code, errs)
	}
	for _, want := range []string{`"kind":"random_names"`, `"set_id":"jp"`, `"names":[`} {
		if !strings.Contains(out, want) {
			t.Fatalf("json name missing %q: %q", want, out)
		}
	}

	// Unknown language sets and bad counts are usage errors.
	for _, args := range [][]string{
		{"name", "xx"},
		{"name", "cn", "0"},
		{"name", "cn", "cn"},
	} {
		if code, _, _ := runCLI(t, root, args...); code != 2 {
			t.Fatalf("%v: code=%d, want 2", args, code)
		}
	}
}
