package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// P2.3: target arithmetic on the check target. The published `+n`/`-n`
// suffixes stay bonus/penalty dice; `*n` / `/n` rewrite the target and
// `--mod` shifts it.
func TestCommandRATargetArithmetic(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲")

	// CON=60 on the sheet; `体质*5` checks against 300.
	code, out, errs := runCLI(t, root, "ra", "体质*5")
	if code != 0 {
		t.Fatalf("ra 体质*5 code=%d stderr=%s", code, errs)
	}
	for _, want := range []string{"目标 300", "原始 60", "*5"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q: %q", want, out)
		}
	}

	// Division halves the sheet value.
	code, out, errs = runCLI(t, root, "ra", "心理学/2")
	if code != 0 || !strings.Contains(out, "目标 30") {
		t.Fatalf("ra 心理学/2 code=%d out=%q err=%s", code, out, errs)
	}

	// The explicit modifier shifts the resolved target.
	code, out, errs = runCLI(t, root, "ra", "敏捷", "--mod", "-10")
	if code != 0 || !strings.Contains(out, "目标 45") || !strings.Contains(out, "原始 55") {
		t.Fatalf("ra --mod code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, ".ra", "心理学", "--mod", "10")
	if code != 0 || !strings.Contains(out, "目标 70") {
		t.Fatalf("check --mod code=%d out=%q err=%s", code, out, errs)
	}

	// Structured contract: the original value and the applied arithmetic.
	code, out, errs = runCLI(t, root, "--json", "ra", "体质*5")
	if code != 0 {
		t.Fatalf("json ra code=%d stderr=%s", code, errs)
	}
	for _, want := range []string{`"target":300`, `"target_original":60`, `"target_adjust":"*5"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("json output missing %q: %q", want, out)
		}
	}

	// A target outside the declared computed range is a usage error, and an
	// explicit target cannot be combined with an arithmetic.
	for _, args := range [][]string{
		{"ra", "体质*100"},
		{"ra", "体质*5", "60"},
	} {
		if code, _, _ := runCLI(t, root, args...); code != 2 {
			t.Fatalf("%v: code=%d, want 2", args, code)
		}
	}

	// The arithmetic runs before the difficulty modifier: 60*5 = 300, the
	// hard band rewrites it to 150.
	code, out, errs = runCLI(t, root, "ra", "困难", "体质*5")
	if code != 0 {
		t.Fatalf("ra 困难 体质*5 code=%d stderr=%s", code, errs)
	}
	for _, want := range []string{"目标 150", "原始 60", "基础 300"} {
		if !strings.Contains(out, want) {
			t.Fatalf("difficulty + arithmetic output missing %q: %q", want, out)
		}
	}
}

// P2.6: rulebook-declared aliases resolve like the canonical names.
func TestCommandAliases(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲")

	// "察觉" is an alias of the skill spot_hidden (侦查).
	code, out, errs := runCLI(t, root, "ra", "察觉")
	if code != 0 || !strings.Contains(out, "侦查") {
		t.Fatalf("ra alias code=%d out=%q err=%s", code, out, errs)
	}
	// "臂力" is an alias of the characteristic STR.
	code, out, errs = runCLI(t, root, "ra", "臂力")
	if code != 0 || !strings.Contains(out, "[力量]") {
		t.Fatalf("ra attribute alias code=%d out=%q err=%s", code, out, errs)
	}
	// Aliases work for assignment and lookup too.
	code, out, errs = runCLI(t, root, "st", "察觉=40")
	if code != 0 || !strings.Contains(out, "spot_hidden 40") {
		t.Fatalf("st alias code=%d out=%q err=%s", code, out, errs)
	}
}

// P2.4: the optional current value, the reason text and recovery losses.
func TestCommandResourceExtras(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲")

	// The explicit current value only overrides the check target; the track
	// itself is untouched.
	code, out, errs := runCLI(t, root, "sc", "0/1", "70")
	if code != 0 || !strings.Contains(out, "目标 70") {
		t.Fatalf("sc current code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "st", "show", "san")
	if code != 0 || !strings.Contains(out, "san 55/99") {
		t.Fatalf("current override must not touch the track: %q", out)
	}

	// Reason text after the loss pair.
	code, out, errs = runCLI(t, root, "sc", "0/1d10", "直面外神")
	if code != 0 || !strings.Contains(out, "直面外神") {
		t.Fatalf("sc reason code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "--json", "sc", "0/1d10", "直面外神")
	if code != 0 || !strings.Contains(out, `"reason":"直面外神"`) {
		t.Fatalf("sc json reason code=%d out=%q err=%s", code, out, errs)
	}

	// A recovery loss raises the track; it never exceeds the track maximum.
	// The sanity track is lowered first so the check actually fails and the
	// failure loss (the recovery) applies.
	if code, _, errs := runCLI(t, root, "st", "san-15"); code != 0 {
		t.Fatalf("st san-15 code=%d stderr=%s", code, errs)
	}
	code, out, errs = runCLI(t, root, "sc", "0/-5", "--apply")
	if code != 0 {
		t.Fatalf("sc recovery code=%d stderr=%s", code, errs)
	}
	if !strings.Contains(out, "san 40 → 45（5）已保存") {
		t.Fatalf("recovery output: %q", out)
	}
	code, out, errs = runCLI(t, root, "st", "show", "san")
	if code != 0 || !strings.Contains(out, "san 45/99") {
		t.Fatalf("post-recovery sheet: %q", out)
	}

	// `.sc` exposes the same override.
	code, out, errs = runCLI(t, root, ".sc", "0/1d10", "70")
	if code != 0 || !strings.Contains(out, "目标 70") {
		t.Fatalf("resource --current code=%d out=%q err=%s", code, out, errs)
	}
}

// P2.5: the bare roll, the default die setting and the compact roll.
func TestCommandRollConveniences(t *testing.T) {
	root := t.TempDir()

	// A bare `r` rolls the default die (d100).
	code, out, errs := runCLI(t, root, "r")
	if code != 0 || !strings.Contains(out, "d100") {
		t.Fatalf("bare r code=%d out=%q err=%s", code, out, errs)
	}

	// `set` configures the default die and persists it in the local state.
	code, out, errs = runCLI(t, root, "set", "20")
	if code != 0 || !strings.Contains(out, "d20") {
		t.Fatalf("set 20 code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "r")
	if code != 0 || !strings.Contains(out, "d20") {
		t.Fatalf("r after set code=%d out=%q err=%s", code, out, errs)
	}
	// `set` without an argument restores the built-in default.
	code, _, errs = runCLI(t, root, "set")
	if code != 0 {
		t.Fatalf("set code=%d stderr=%s", code, errs)
	}
	code, out, errs = runCLI(t, root, "r")
	if code != 0 || !strings.Contains(out, "d100") {
		t.Fatalf("r after reset code=%d out=%q err=%s", code, out, errs)
	}
	for _, args := range [][]string{
		{"set", "0"},
		{"set", "1001"},
	} {
		if code, _, _ := runCLI(t, root, args...); code != 2 {
			t.Fatalf("%v: code=%d, want 2", args, code)
		}
	}
	// Dice! also uses `.set list` and `.set <system>`.
	code, out, errs = runCLI(t, root, "set", "list")
	if code != 0 || !strings.Contains(out, "coc7") {
		t.Fatalf("set list code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "set", "coc7")
	if code != 0 || !strings.Contains(out, "coc7") {
		t.Fatalf("set coc7 code=%d out=%q err=%s", code, out, errs)
	}

	// `rs` renders the total only; the JSON contract just marks the result
	// as not detailed.
	code, out, errs = runCLI(t, root, "rs", "2d6+3")
	if code != 0 || !strings.Contains(out, "2d6+3 = ") || strings.Contains(out, "[2d6:") {
		t.Fatalf("rs code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "--json", "rs", "2d6+3")
	if code != 0 || !strings.Contains(out, `"detailed":false`) || !strings.Contains(out, `"Total"`) {
		t.Fatalf("json rs code=%d out=%q err=%s", code, out, errs)
	}
}

// A character card may carry its own default die size (metadata), which
// overrides the user-level setting.
func TestCommandDefaultDieFromCharacterMetadata(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲")

	// The user-level setting applies first.
	if code, _, errs := runCLI(t, root, "set", "30"); code != 0 {
		t.Fatalf("set code=%d stderr=%s", code, errs)
	}
	code, out, errs := runCLI(t, root, "r")
	if code != 0 || !strings.Contains(out, "d30") {
		t.Fatalf("r code=%d out=%q err=%s", code, out, errs)
	}

	// The card's own metadata wins.
	if code, _, errs := runCLI(t, root, ".pc", "meta", "default_die_sides", "20"); code != 0 {
		t.Fatalf("meta code=%d stderr=%s", code, errs)
	}
	code, out, errs = runCLI(t, root, "r")
	if code != 0 || !strings.Contains(out, "d20") {
		t.Fatalf("r with card default code=%d out=%q err=%s", code, out, errs)
	}
}

// findCharacterFile returns the single character card in the data root.
func findCharacterFile(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "characters"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".yaml") {
			return filepath.Join(root, "characters", e.Name())
		}
	}
	t.Fatal("no character card found")
	return ""
}

// P2.6: the remaining `st` operations.
func TestCommandSTOperations(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲")

	// `st show <键>` reports one value.
	code, out, errs := runCLI(t, root, "st", "show", "心理学")
	if code != 0 || !strings.Contains(out, "psychology 60") {
		t.Fatalf("st show key code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "--json", "st", "show", "san")
	if code != 0 || !strings.Contains(out, `"kind":"resource"`) || !strings.Contains(out, `"max":99`) {
		t.Fatalf("json st show code=%d out=%q err=%s", code, out, errs)
	}

	// The colon is a synonym of "=".
	code, out, errs = runCLI(t, root, "st", "力量:55")
	if code != 0 || !strings.Contains(out, "STR 55") {
		t.Fatalf("st colon code=%d out=%q err=%s", code, out, errs)
	}

	// `st del` removes one value.
	code, out, errs = runCLI(t, root, "st", "del", "psychology")
	if code != 0 || strings.Contains(out, "psychology 60") {
		t.Fatalf("st del code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "st", "show")
	if code != 0 || strings.Contains(out, "psychology") {
		t.Fatalf("st del persistence: %q", out)
	}

	// `st clr` empties the sheet but keeps the card.
	code, out, errs = runCLI(t, root, "st", "clr")
	if code != 0 || !strings.Contains(out, "调查员甲") {
		t.Fatalf("st clr code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "st", "show")
	if code != 0 || strings.Contains(out, "STR 55") || strings.Contains(out, "san 55") {
		t.Fatalf("st clr result: %q", out)
	}
	code, out, errs = runCLI(t, root, "pc", "list")
	if code != 0 || !strings.Contains(out, "调查员甲") {
		t.Fatalf("st clr must keep the card: %q", out)
	}

	// Usage errors.
	for _, args := range [][]string{
		{"st", "del"},
		{"st", "del", "不存在"},
		{"st", "clr", "extra"},
		{"st", "show", "不存在"},
		{"st", "show", "a", "b"},
	} {
		if code, _, _ := runCLI(t, root, args...); code != 2 {
			t.Fatalf("%v: code=%d, want 2", args, code)
		}
	}
}

// The multiplication alias "X" works in the dice expression layer.
func TestRollMultiplicationAlias(t *testing.T) {
	root := t.TempDir()
	code, out, errs := runCLI(t, root, ".r", "3d6X5")
	if code != 0 {
		t.Fatalf("roll X code=%d stderr=%s", code, errs)
	}
	if !strings.Contains(out, "3d6X5") {
		t.Fatalf("output missing the expression: %q", out)
	}
}
