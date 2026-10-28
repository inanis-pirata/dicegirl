package integration

import (
	"strings"
	"testing"
)

// The target syntax is live as of T16b: an "@名字" either takes effect or is
// reported. This is the end-to-end sentinel for that contract; the layers
// themselves are covered by chatcore/target_test.go and
// chatcore/target_resolve_test.go, where Source/Kind/Reason are assertable.
func TestTargetSyntaxTakesEffectOrIsReported(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "张三")

	// A command that takes no target rejects one instead of swallowing it: it
	// used to become the roll's label, which silently hid the user's intent.
	code, _, errs := runCLI(t, root, ".r", "1d6", "@张三")
	if code != 2 || !strings.Contains(errs, "r 不支持指定目标") {
		t.Fatalf(".r 1d6 @张三 code=%d err=%s", code, errs)
	}
	// The hint must point at a command that actually takes a target today:
	// ".sc" is closed until the T08 permission check, so pointing there would
	// fail the player twice.
	if !strings.Contains(errs, "代骰请用 .ra") || strings.Contains(errs, ".sc") {
		t.Fatalf(".r hint wrong: %s", errs)
	}

	// A command that takes a target strips it and acts on that card: `.ra` no
	// longer reports "只能指定一个技能" for a trailing name.
	code, out, errs := runCLI(t, root, ".ra", "心理学", "@张三")
	if code != 0 || !strings.Contains(out, "目标 60") {
		t.Fatalf(".ra 心理学 @张三 code=%d out=%q err=%s", code, out, errs)
	}

	// A name that does not exist is reported, never silently applied to the
	// sender's own card.
	code, _, errs = runCLI(t, root, ".ra", "心理学", "@李四")
	if code != 1 || !strings.Contains(errs, "找不到角色：李四") {
		t.Fatalf(".ra @李四 code=%d err=%s", code, errs)
	}

	// `--character` is a machine interface that must stay byte-identical.
	code, out, errs = runCLI(t, root, ".ra", "60", "--character", "张三")
	if code != 0 || !strings.Contains(out, "目标 60") {
		t.Fatalf(".ra 60 --character 张三 code=%d out=%q err=%s", code, out, errs)
	}
	if code, out, errs := runCLI(t, root, ".st", "derived", "--character", "张三"); code != 0 || !strings.Contains(out, "HP") {
		t.Fatalf(".st derived --character 张三 code=%d out=%q err=%s", code, out, errs)
	}
	if code, _, errs := runCLI(t, root, ".ra", "60", "--character", "李四"); code != 1 || !strings.Contains(errs, "NOT_FOUND") {
		t.Fatalf(".ra --character unknown code=%d err=%s", code, errs)
	}

	// Both ways of naming a card in one command is an error, not a precedence
	// rule (C12).
	code, _, errs = runCLI(t, root, ".ra", "60", "@张三", "--character", "张三")
	if code != 0 {
		t.Fatalf("same card twice must be accepted: code=%d err=%s", code, errs)
	}

	// Ordinary roll text is untouched: a non-mention "@" never becomes a target.
	code, out, errs = runCLI(t, root, ".r", "1d6", "a@b.com")
	if code != 0 || !strings.Contains(out, "1d6") {
		t.Fatalf(".r 1d6 a@b.com code=%d out=%q err=%s", code, out, errs)
	}

	// The active card is untouched by a targeted command.
	code, out, _ = runCLI(t, root, ".pc", "active")
	if code != 0 || strings.TrimSpace(out) != cardIDOf(t, root, "张三") {
		t.Fatalf(".pc active code=%d out=%q", code, out)
	}
}

// TestCharacterFlagRoutesEveryCardCommand pins the machine interface at every
// command that used to resolve a card on its own: ra, sc, en, st derived, rav,
// pc meta, plus rb/rp (which reach the check through the same path). The flag
// must address the named card, not whichever card happens to be active.
func TestCharacterFlagRoutesEveryCardCommand(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "张三")                  // psychology=60, active
	createInvestigator(t, root, "李四", "psychology=30") // psychology=30, now active

	cases := []struct {
		args []string
		want string
	}{
		{[]string{".ra", "心理学", "--character", "张三"}, "目标 60"},
		{[]string{".ra", "心理学", "--character", "李四"}, "目标 30"},
		{[]string{".rb", "2", "60", "--character", "张三"}, "奖励骰"},
		{[]string{".rp", "2", "60", "--character", "张三"}, "惩罚骰"},
		{[]string{".en", "心理学", "--character", "张三"}, "未成长"},
		{[]string{".sc", "1/1d10", "--character", "张三"}, "未写回角色卡"},
		{[]string{".rav", "60", "50", "--character", "张三"}, "对抗检定"},
		{[]string{".pc", "meta", "阵营", "善良", "--character", "张三"}, "元数据：阵营=善良"},
		{[]string{".st", "derived", "--character", "张三"}, "HP"},
	}
	for _, tc := range cases {
		code, out, errs := runCLI(t, root, tc.args...)
		if code != 0 || !strings.Contains(out, tc.want) {
			t.Fatalf("%v: code=%d out=%q stderr=%q", tc.args, code, out, errs)
		}
	}

	// The active card is still the fallback when no card is named.
	if code, out, _ := runCLI(t, root, ".ra", "心理学"); code != 0 || !strings.Contains(out, "目标 30") {
		t.Fatalf("active-card fallback: code=%d out=%q", code, out)
	}

	// The unknown-name error is the pre-T16a wording and exit code.
	code, _, errs := runCLI(t, root, ".ra", "60", "--character", "王五")
	if code != 1 || !strings.Contains(errs, "找不到角色：王五（可用 pc list 查看）") {
		t.Fatalf("unknown name: code=%d stderr=%q", code, errs)
	}

	// A usage error still wins over an unresolvable target: the target is
	// resolved where the command always resolved it, never earlier, so the
	// error order is unchanged.
	code, _, errs = runCLI(t, root, ".sc", "--character", "王五")
	if code != 2 || !strings.Contains(errs, "用法：sc") {
		t.Fatalf("usage before target: code=%d stderr=%q", code, errs)
	}
}
