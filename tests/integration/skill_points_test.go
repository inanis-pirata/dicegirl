package integration

import (
	"strings"
	"testing"
)

// Skill-point allocation (st alloc): the pools the rulebook declares for
// the card's generation variant are reported after creation, spent on
// skills (starting from their rulebook bases), refunded and reset — with
// the budget and per-skill caps enforced.
func TestSkillPointAllocation(t *testing.T) {
	root := t.TempDir()
	// EDU 70 → 280 occupation points, INT 65 → 130 interest points.
	createInvestigator(t, root, "调查员", "INT=65")

	// The pools report the declaration evaluated against the sheet.
	code, out, errs := runCLI(t, root, "st", "alloc")
	if code != 0 || !strings.Contains(out, "职业技能点 0/280（上限 90）") ||
		!strings.Contains(out, "兴趣技能点 0/130（上限 75）") {
		t.Fatalf("st alloc code=%d out=%q err=%s", code, out, errs)
	}

	// Spending starts skills at their rulebook base (侦查 25) and is
	// written back to the card.
	code, out, errs = runCLI(t, root, "st", "alloc", "职业", "侦查+40", "图书馆使用+20")
	if code != 0 || !strings.Contains(out, "侦查 基础 25+40=65") ||
		!strings.Contains(out, "图书馆使用 基础 20+20=40") ||
		!strings.Contains(out, "职业技能点 60/280") {
		t.Fatalf("st alloc 职业 code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "st", "show", "侦查")
	if code != 0 || !strings.Contains(out, "spot_hidden") || !strings.Contains(out, "65") {
		t.Fatalf("st show 侦查 code=%d out=%q err=%s", code, out, errs)
	}

	// The pool view tracks the spending; a check against the written value
	// no longer labels it 基础.
	code, out, errs = runCLI(t, root, "st", "alloc")
	if code != 0 || !strings.Contains(out, "职业技能点 60/280") {
		t.Fatalf("st alloc after spend code=%d out=%q err=%s", code, out, errs)
	}

	// The caps are enforced: 65 + 26 = 91 tops the occupation cap of 90.
	code, out, errs = runCLI(t, root, "st", "alloc", "职业", "侦查+26")
	if code == 0 || !strings.Contains(errs, "上限") {
		t.Fatalf("cap violation code=%d err=%s", code, errs)
	}
	// The cap override for interest points on 射击（手枪）(base 20) is 50.
	code, out, errs = runCLI(t, root, "st", "alloc", "兴趣", "手枪射击+40")
	if code == 0 || !strings.Contains(errs, "50") {
		t.Fatalf("handgun override code=%d err=%s", code, errs)
	}

	// A rejected command leaves the card untouched.
	code, out, errs = runCLI(t, root, "st", "show", "侦查")
	if code != 0 || !strings.Contains(out, "65") {
		t.Fatalf("card changed by rejected alloc: %q %s", out, errs)
	}

	// Refunds return points to the pool and lower the value, never below
	// the base.
	code, out, errs = runCLI(t, root, "st", "alloc", "职业", "侦查-10")
	if code != 0 || !strings.Contains(out, "侦查 65-10=55") ||
		!strings.Contains(out, "职业技能点 50/280") {
		t.Fatalf("refund code=%d out=%q err=%s", code, out, errs)
	}
	code, _, errs = runCLI(t, root, "st", "alloc", "职业", "侦查-31")
	if code == 0 {
		t.Fatalf("refund beyond allocation should fail: %s", errs)
	}

	// The reset rolls every allocated skill back to its base.
	code, out, errs = runCLI(t, root, "st", "alloc", "clr")
	if code != 0 || !strings.Contains(out, "已重置加点") || !strings.Contains(out, "职业技能点 0/280") {
		t.Fatalf("st alloc clr code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "st", "show", "侦查")
	if code != 0 || !strings.Contains(out, "25") {
		t.Fatalf("reset value code=%d out=%q err=%s", code, out, errs)
	}

	// Usage errors.
	code, _, errs = runCLI(t, root, "st", "alloc", "职业")
	if code == 0 {
		t.Fatalf("pool without spends should fail: %s", errs)
	}
	code, _, errs = runCLI(t, root, "st", "alloc", "职业", "侦查+1d4")
	if code == 0 {
		t.Fatalf("dice points should fail: %s", errs)
	}
}

// Generation carries the skill-point pools of its variant: a `.coc` card
// reports the 7e pools, a `.coc6` card the 6th-edition ones, and `pc redo`
// resets any allocation.
func TestGenerationSkillPools(t *testing.T) {
	root := t.TempDir()

	code, out, errs := runCLI(t, root, "coc")
	if code != 0 {
		t.Fatalf("coc code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "加点 职业技能点 0/") || !strings.Contains(out, "兴趣技能点 0/") {
		t.Fatalf("coc output missing the skill pools: %q", out)
	}

	// Spend, then regenerate: the allocation is creation-fresh again.
	code, out, errs = runCLI(t, root, "st", "alloc", "兴趣", "医学+20")
	if code != 0 {
		t.Fatalf("st alloc code=%d err=%s", code, errs)
	}
	code, out, errs = runCLI(t, root, "pc", "redo")
	if code != 0 {
		t.Fatalf("pc redo code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "兴趣技能点 0/") {
		t.Fatalf("pc redo did not reset the pools: %q", out)
	}

	// The 6th-edition variant carries its own pool declaration (EDU×20 /
	// INT×10, cap 99) and the card remembers the variant.
	code, out, errs = runCLI(t, root, "coc6")
	if code != 0 {
		t.Fatalf("coc6 code=%d err=%s", code, errs)
	}
	code, out, errs = runCLI(t, root, "st", "alloc")
	if code != 0 || !strings.Contains(out, "职业技能点 0/") {
		t.Fatalf("coc6 st alloc code=%d out=%q err=%s", code, out, errs)
	}
	// EDU×20 pools are large; the cap is 99.
	code, _, errs = runCLI(t, root, "st", "alloc", "职业", "侦查+60")
	if code != 0 {
		t.Fatalf("coc6 alloc code=%d err=%s", code, errs)
	}
	code, out, errs = runCLI(t, root, "st", "show", "侦查")
	if code != 0 || !strings.Contains(out, "85") {
		t.Fatalf("coc6 侦查 code=%d out=%q err=%s", code, out, errs)
	}
}
