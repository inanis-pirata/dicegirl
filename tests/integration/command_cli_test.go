package integration

import (
	"strings"
	"testing"
)

// createInvestigator builds a complete CoC7 character (all attributes the
// derived-statistic formulas need) with psychology=60 and makes it active.
func createInvestigator(t *testing.T, root, name string, extraSets ...string) {
	t.Helper()
	sets := []string{
		"STR=50", "CON=60", "SIZ=70", "DEX=55", "POW=55", "EDU=70",
		"psychology=60",
	}
	sets = append(sets, extraSets...)
	args := []string{".pc", "new", name, "--active"}
	for _, s := range sets {
		args = append(args, s)
	}
	if code, _, errs := runCLI(t, root, args...); code != 0 {
		t.Fatalf("create code=%d stderr=%s", code, errs)
	}
	if code, _, errs := runCLI(t, root, ".st", "derived", "--init"); code != 0 {
		t.Fatalf("derived --init code=%d stderr=%s", code, errs)
	}
}

func TestCommandRoll(t *testing.T) {
	root := t.TempDir()

	// Prefix punctuation and bare name all route to the roll statement.
	code, out, errs := runCLI(t, root, ".r", "2d6+1", "攻击")
	if code != 0 || !strings.Contains(out, "2d6+1") || !strings.Contains(out, "攻击") {
		t.Fatalf(".r code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "。r", "3#1d4")
	if code != 0 || !strings.Contains(out, "①") || !strings.Contains(out, "③") {
		t.Fatalf("。r repeat code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "r")
	if code != 0 || !strings.Contains(out, "d100") {
		t.Fatalf("bare r code=%d out=%q err=%s", code, out, errs)
	}
	// Dice! `.rd` rolls the default die, and `.rd6` glues the face count.
	code, out, errs = runCLI(t, root, "rd")
	if code != 0 || !strings.Contains(out, "d100") {
		t.Fatalf("rd code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "rd6")
	if code != 0 || !strings.Contains(out, "d6") {
		t.Fatalf("rd6 code=%d out=%q err=%s", code, out, errs)
	}

	// JSON keeps the same batch container as the long roll command.
	code, out, errs = runCLISeed(t, root, 7, "--json", "r", "2#1d6", "伤害")
	if code != 0 || !strings.Contains(out, `"repeat":2`) || !strings.Contains(out, `"rolls":[`) {
		t.Fatalf("json r code=%d out=%q err=%s", code, out, errs)
	}
}

func TestCommandRA(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲")

	// Named skill: value 60 from the sheet, seed 7 rolls 53 → regular success.
	code, out, errs := runCLI(t, root, "ra", "心理学")
	if code != 0 || !strings.Contains(out, "[心理学]") || !strings.Contains(out, "普通成功") {
		t.Fatalf("ra skill code=%d out=%q err=%s", code, out, errs)
	}
	// Dice! exposes the same check as `.rc`; keep both spellings identical.
	code, out, errs = runCLI(t, root, "rc", "心理学")
	if code != 0 || !strings.Contains(out, "[心理学]") {
		t.Fatalf("rc skill code=%d out=%q err=%s", code, out, errs)
	}
	// .rb/.rp are bonus/penalty-die shorthands for the same check.
	code, out, errs = runCLI(t, root, "rb", "心理学")
	if code != 0 || !strings.Contains(out, "奖励骰") {
		t.Fatalf("rb code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "rp2", "60")
	if code != 0 || !strings.Contains(out, "惩罚骰") {
		t.Fatalf("rp2 code=%d out=%q err=%s", code, out, errs)
	}
	// Inline bonus dice as a suffix or bare signed token.
	code, out, errs = runCLI(t, root, "ra", "心理学+2")
	if code != 0 || !strings.Contains(out, "奖励骰") {
		t.Fatalf("ra bonus suffix code=%d out=%q err=%s", code, out, errs)
	}
	code, _, errs = runCLI(t, root, "ra", "心理学", "-1")
	if code != 0 {
		t.Fatalf("ra penalty token code=%d stderr=%s", code, errs)
	}
	// Explicit target; JSON is the same structured check as long `check`.
	code, out, errs = runCLI(t, root, "ra", "60")
	if code != 0 || !strings.Contains(out, "目标 60") {
		t.Fatalf("ra target code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "--json", "ra", "60")
	if code != 0 || !strings.Contains(out, `"kind":"check"`) || !strings.Contains(out, `"level_code"`) {
		t.Fatalf("ra json code=%d out=%q err=%s", code, out, errs)
	}
	// Attribute names resolve to characteristics, not skills.
	code, out, errs = runCLI(t, root, "ra", "STR")
	if code != 0 || !strings.Contains(out, "[力量]") {
		t.Fatalf("ra attr code=%d out=%q err=%s", code, out, errs)
	}

	// Usage failures.
	for _, args := range [][]string{
		{"ra", "不存在"},       // unknown name
		{"ra", "60", "70"},  // two targets
		{"ra", "+x"},        // malformed modifier
		{"ra", "心理学", "聆听"}, // two names
		{"ra"},              // nothing to check
	} {
		if code, _, _ := runCLI(t, root, args...); code != 2 {
			t.Fatalf("%v: code=%d, want 2", args, code)
		}
	}
}

func TestCommandRADifficulty(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲")

	// Seed 7 rolls 53: hard of 60 adjusts the target to 30, so 53 fails;
	// keyword may be prefixed or standalone.
	code, out, errs := runCLI(t, root, "ra", "困难心理学", "60")
	if code != 0 {
		t.Fatalf("hard code=%d stderr=%s", code, errs)
	}
	for _, want := range []string{"[心理学（困难）]", "目标 30", "（基础 60）", "失败"} {
		if !strings.Contains(out, want) {
			t.Fatalf("hard output missing %q: %q", want, out)
		}
	}
	code, out, errs = runCLI(t, root, "ra", "困难", "心理学")
	if code != 0 || !strings.Contains(out, "目标 30") {
		t.Fatalf("standalone keyword code=%d out=%q err=%s", code, out, errs)
	}
	// Extreme of 60 is 12.
	code, out, _ = runCLI(t, root, "ra", "极难", "60")
	if code != 0 || !strings.Contains(out, "目标 12") {
		t.Fatalf("extreme out=%q code=%d", out, code)
	}

	// Auto success maps 53 (ordinary failure against 60) to regular success.
	code, out, errs = runCLI(t, root, "ra", "自动成功心理学")
	if code != 0 {
		t.Fatalf("auto code=%d stderr=%s", code, errs)
	}
	for _, want := range []string{"非大失败即成功", "普通成功"} {
		if !strings.Contains(out, want) {
			t.Fatalf("auto output missing %q: %q", want, out)
		}
	}

	// Auto success never overrides a fumble: skill 40 + seed 29 rolls 96.
	rootF := t.TempDir()
	createInvestigator(t, rootF, "倒霉蛋", "psychology=40")
	code, out, errs = runCLISeed(t, rootF, 29, "ra", "自动成功心理学")
	if code != 0 || !strings.Contains(out, "大失败") {
		t.Fatalf("auto fumble code=%d out=%q err=%s", code, out, errs)
	}

	// Difficulty composes with inline bonus dice (parsing happens after the
	// keyword is stripped).
	if code, _, _ := runCLI(t, root, "ra", "困难心理学+2"); code != 0 {
		t.Fatalf("hard + bonus code=%d", code)
	}

	// Structured contract: effective target, base target and difficulty id.
	code, out, errs = runCLI(t, root, "--json", "ra", "困难", "60")
	if code != 0 {
		t.Fatalf("json hard code=%d stderr=%s", code, errs)
	}
	for _, want := range []string{`"difficulty_id":"hard"`, `"difficulty_name":"困难"`, `"base_target":60`, `"target":30`} {
		if !strings.Contains(out, want) {
			t.Fatalf("hard json missing %q: %q", want, out)
		}
	}

	// Difficulty keywords accept the id or the localized name; unknown names
	// fail loudly.
	for _, args := range [][]string{
		{".ra", "hard", "心理学"},
		{".ra", "困难", "心理学"},
	} {
		if code, _, _ := runCLI(t, root, args...); code != 0 {
			t.Fatalf("%v: code=%d", args, code)
		}
	}
	for _, args := range [][]string{
		{".ra", "超难", "心理学"},
		{".ra", "超难心理学", "60"}, // unknown keyword stays part of the skill name
	} {
		if code, _, _ := runCLI(t, root, args...); code != 2 {
			t.Fatalf("%v: code=%d, want 2", args, code)
		}
	}
}

func TestCommandRABatch(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲")

	// Three independent trials under a shared header, one circled line each.
	code, out, errs := runCLI(t, root, "ra", "3#心理学")
	if code != 0 {
		t.Fatalf("3# skill code=%d stderr=%s", code, errs)
	}
	for _, want := range []string{"🎲 心理学 ×3", "①", "②", "③"} {
		if !strings.Contains(out, want) {
			t.Fatalf("batch output missing %q: %q", want, out)
		}
	}
	if n := strings.Count(out, "：骰值"); n != 3 {
		t.Fatalf("want 3 verdicts, got %d: %q", n, out)
	}

	// Standalone prefix token and explicit target.
	if code, out, errs = runCLI(t, root, "ra", "3#", "60"); code != 0 || strings.Count(out, "：骰值") != 3 {
		t.Fatalf("standalone 3# code=%d out=%q err=%s", code, out, errs)
	}

	// Dice! r3#p shorthand: one penalty die per trial, composes with the
	// existing bonus/penalty explanation.
	code, out, errs = runCLI(t, root, "ra", "3#p", "心理学")
	if code != 0 || !strings.Contains(out, "惩罚骰") || strings.Count(out, "：骰值") != 3 {
		t.Fatalf("3#p code=%d out=%q err=%s", code, out, errs)
	}

	// Repeat composes with explicit difficulty.
	code, out, errs = runCLI(t, root, "ra", "困难", "3#心理学", "60")
	if code != 0 || !strings.Contains(out, "心理学（困难）") || strings.Count(out, "目标 30（基础 60）") != 3 {
		t.Fatalf("difficulty batch code=%d out=%q err=%s", code, out, errs)
	}

	// JSON is a check_batch container with three independent results.
	code, out, errs = runCLI(t, root, "--json", "ra", "3#心理学")
	if code != 0 {
		t.Fatalf("json batch code=%d stderr=%s", code, errs)
	}
	for _, want := range []string{`"kind":"check_batch"`, `"repeat":3`, `"checks":[`} {
		if !strings.Contains(out, want) {
			t.Fatalf("batch json missing %q: %q", want, out)
		}
	}

	// Alternative repeat spelling: a bare target with the prefix.
	if code, _, _ := runCLI(t, root, ".ra", "2#60"); code != 0 {
		t.Fatalf("ra 2#60 code=%d", code)
	}

	// Bounds: over the cap and zero repeats are usage/runtime errors (2);
	// repeat=1 flattens to the ordinary single-check path.
	for _, args := range [][]string{
		{".ra", "10#心理学"},
		{".ra", "0#心理学"},
	} {
		if code, _, _ := runCLI(t, root, args...); code != 2 {
			t.Fatalf("%v: code=%d, want 2", args, code)
		}
	}
	if code, out, _ := runCLI(t, root, "ra", "1#心理学"); code != 0 || strings.Contains(out, "×1") {
		t.Fatalf("1# should flatten to single output, code=%d out=%q", code, out)
	}
}

func TestCommandResourceChecks(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲")

	// sc is the sanity alias; --apply persists the (zero) change.
	code, out, errs := runCLI(t, root, "sc", "0/1")
	if code != 0 || !strings.Contains(out, "[理智]") || strings.Contains(out, "已保存") {
		t.Fatalf("sc advisory code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "sc", "0/1", "--apply")
	if code != 0 || !strings.Contains(out, "san 55 → 55（0）已保存") {
		t.Fatalf("sc apply code=%d out=%q err=%s", code, out, errs)
	}
	// JSON resource check matches the long `resource` contract.
	code, out, errs = runCLI(t, root, "--json", "sc", "0/1d3")
	if code != 0 || !strings.Contains(out, `"kind":"resource_check"`) || !strings.Contains(out, `"check_id":"sanity"`) {
		t.Fatalf("sc json code=%d out=%q err=%s", code, out, errs)
	}

	// Bad pair shape and unknown declared checks are usage errors.
	for _, args := range [][]string{
		{"sc", "0"},
		{"sc", "a/b/c"},
	} {
		if code, _, _ := runCLI(t, root, args...); code != 2 {
			t.Fatalf("%v: code=%d, want 2", args, code)
		}
	}
}

func TestCommandST(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲")

	// Show sheet with no args or `show`.
	for _, args := range [][]string{{"st"}, {"st", "show"}} {
		code, out, errs := runCLI(t, root, args...)
		if code != 0 || !strings.Contains(out, "调查员甲") || !strings.Contains(out, "san 55/99") {
			t.Fatalf("%v: code=%d out=%q err=%s", args, code, out, errs)
		}
	}

	// Assignment path resolves against the active character.
	code, out, errs := runCLI(t, root, "st", "psychology=65")
	if code != 0 || !strings.Contains(out, "psychology 65") {
		t.Fatalf("st set code=%d out=%q err=%s", code, out, errs)
	}
	// Dice! also accepts the '='-less form and name/value pairs.
	code, out, errs = runCLI(t, root, "st", "psychology70")
	if code != 0 || !strings.Contains(out, "psychology 70") {
		t.Fatalf("st glued set code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "st", "EDU", "66")
	if code != 0 || !strings.Contains(out, "EDU 66") {
		t.Fatalf("st pair set code=%d out=%q err=%s", code, out, errs)
	}

	// Adjustment path: integer delta and rolled dice delta (seed 7 rolls 4
	// on 1d6 in a fresh process), persisted to the resource track.
	code, out, errs = runCLI(t, root, "st", "STR+1")
	if code != 0 || !strings.Contains(out, "STR 50 → 51（+1）") {
		t.Fatalf("st adjust code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "st", "san-1d6")
	if code != 0 || !strings.Contains(out, "san 55 → 51（-4），掷骰 1d6") {
		t.Fatalf("st dice adjust code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "st", "show")
	if code != 0 || !strings.Contains(out, "san 51/99") {
		t.Fatalf("st persistence code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "--json", "st", "STR+1")
	if code != 0 || !strings.Contains(out, `"canonical":"STR"`) || !strings.Contains(out, `"kind":"attribute"`) {
		t.Fatalf("st adjust json code=%d out=%q err=%s", code, out, errs)
	}

	// Mixing assignment and adjustment, and bad expressions, are usage errors.
	for _, args := range [][]string{
		{"st", "STR=50", "san-1"},
		{"st", "STR50", "san-1"},
		{"st", "san-abc"},
		{"st", "nosuchkey+1"},
	} {
		if code, _, _ := runCLI(t, root, args...); code != 2 {
			t.Fatalf("%v: code=%d, want 2", args, code)
		}
	}
}

func TestCommandPCAndRH(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲")

	code, out, errs := runCLI(t, root, "pc", "list")
	if code != 0 || !strings.Contains(out, "调查员甲") || !strings.Contains(out, "* c-") {
		t.Fatalf("pc list code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "pc", "show")
	if code != 0 || !strings.Contains(out, "角色 c-") {
		t.Fatalf("pc show code=%d out=%q err=%s", code, out, errs)
	}

	// Secret dice need a chat adapter.
	code, _, errs = runCLI(t, root, "rh")
	if code != 2 || !strings.Contains(errs, "暗骰") {
		t.Fatalf("rh code=%d stderr=%q", code, errs)
	}

	// Prefixed unknown commands fail loudly instead of falling through.
	code, _, errs = runCLI(t, root, ".xyz")
	if code != 2 || !strings.Contains(errs, "未知指令") {
		t.Fatalf(".xyz code=%d stderr=%q", code, errs)
	}
}

func TestCommandSanityFumbleTakesMaximum(t *testing.T) {
	// Seed 3 draws 100 on the first d100: it is a fumble against target < 50. POW=1 initializes the san track at 1.
	root := t.TempDir()
	createInvestigator(t, root, "倒霉蛋", "POW=1")

	code, out, errs := runCLISeed(t, root, 3, "sc", "0/1d4+1")
	if code != 0 {
		t.Fatalf("fumble sc code=%d stderr=%s", code, errs)
	}
	for _, want := range []string{"大失败", "损失 1d4+1=5", "（大失败，取最大损失）"} {
		if !strings.Contains(out, want) {
			t.Fatalf("fumble output missing %q: %q", want, out)
		}
	}

	// JSON marks the max-loss path without a loss-roll node.
	code, out, errs = runCLISeed(t, root, 3, "--json", "sc", "0/1d4+1")
	if code != 0 {
		t.Fatalf("fumble json code=%d stderr=%s", code, errs)
	}
	if !strings.Contains(out, `"fumble_loss_max":true`) || !strings.Contains(out, `"loss":5`) {
		t.Fatalf("fumble json markers missing: %q", out)
	}

	// Applied fumble clamps the track at zero (1 before, max loss 5).
	code, out, errs = runCLISeed(t, root, 3, "sc", "0/1d4+1", "--apply")
	if code != 0 || !strings.Contains(out, "san 1 → 0（-1）已保存") {
		t.Fatalf("fumble apply code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLISeed(t, root, 3, "st", "show")
	if code != 0 || !strings.Contains(out, "san 0/99") {
		t.Fatalf("post-fumble sheet code=%d out=%q err=%s", code, out, errs)
	}
}
