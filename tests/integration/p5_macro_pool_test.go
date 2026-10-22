package integration

import (
	"strings"
	"testing"
)

// P5.1: named macros are saved on the character card and rolled by name.
func TestNamedMacros(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲")

	// `.st &名=表达式` saves the macro on the card.
	code, out, errs := runCLI(t, root, "st", "&沙漠之鹰=1d10+1d6+3")
	if code != 0 {
		t.Fatalf("st macro code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "&沙漠之鹰=1d10+1d6+3") {
		t.Fatalf("macro not shown on the card: %q", out)
	}

	// `.r 名` rolls the saved expression.
	code, out, errs = runCLI(t, root, "r", "沙漠之鹰")
	if code != 0 {
		t.Fatalf("r macro code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "1d10+1d6+3") {
		t.Fatalf("macro roll output: %q", out)
	}

	// The repeat prefix works with a macro name as well.
	code, out, errs = runCLI(t, root, "r", "3#沙漠之鹰")
	if code != 0 || !strings.Contains(out, "3#1d10+1d6+3") {
		t.Fatalf("r 3#macro code=%d out=%q err=%s", code, out, errs)
	}
	if n := strings.Count(out, "：") + strings.Count(out, "①") + strings.Count(out, "②") + strings.Count(out, "③"); n == 0 {
		t.Fatalf("repeat macro output: %q", out)
	}

	// The macro is listed with the card.
	code, out, errs = runCLI(t, root, "st", "show")
	if code != 0 || !strings.Contains(out, "宏：") {
		t.Fatalf("st show code=%d out=%q err=%s", code, out, errs)
	}

	// `.st del &名` removes it; rolling the name then fails.
	code, out, errs = runCLI(t, root, "st", "del", "&沙漠之鹰")
	if code != 0 || strings.Contains(out, "沙漠之鹰") {
		t.Fatalf("st del macro code=%d out=%q err=%s", code, out, errs)
	}
	code, _, errs = runCLI(t, root, "r", "沙漠之鹰")
	if code != 2 || !strings.Contains(errs, "unknown macro") {
		t.Fatalf("r after delete code=%d err=%q", code, errs)
	}

	// An invalid expression is refused at save time.
	code, _, errs = runCLI(t, root, "st", "&坏宏=1d")
	if code != 2 {
		t.Fatalf("invalid macro code=%d err=%q", code, errs)
	}
}

// P5.2: the generic dice operators and the `.w`/`.ww` pool alias.
func TestDiceOperators(t *testing.T) {
	root := t.TempDir()

	// The count modifier turns a group into a dice-pool count.
	code, out, errs := runCLI(t, root, ".r", "5d10c8")
	if code != 0 {
		t.Fatalf("roll count code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "5d10c8") {
		t.Fatalf("count output: %q", out)
	}

	// The explode modifier adds dice.
	code, out, errs = runCLI(t, root, ".r", "1d6!")
	if code != 0 || !strings.Contains(out, "1d6!") {
		t.Fatalf("roll explode code=%d out=%q err=%s", code, out, errs)
	}

	// The reroll modifier rolls low dice again.
	code, out, errs = runCLI(t, root, ".r", "2d6r4")
	if code != 0 || !strings.Contains(out, "2d6r4") {
		t.Fatalf("roll reroll code=%d out=%q err=%s", code, out, errs)
	}

	// The `.w` pool alias: the summary counts the successes.
	code, out, errs = runCLI(t, root, "w", "5a8")
	if code != 0 || !strings.Contains(out, "5d10!8c8") {
		t.Fatalf("w code=%d out=%q err=%s", code, out, errs)
	}
	// `.ww` shows every die.
	code, out, errs = runCLI(t, root, "ww", "5a8")
	if code != 0 || !strings.Contains(out, "[5d10:") {
		t.Fatalf("ww code=%d out=%q err=%s", code, out, errs)
	}
	// The dice count may be an expression (it is rolled once).
	code, out, errs = runCLI(t, root, "w", "1d2+2a8")
	if code != 0 {
		t.Fatalf("w with an expression code=%d out=%q err=%s", code, out, errs)
	}
	if !strings.Contains(out, "d10!8c8") {
		t.Fatalf("w with an expression output: %q", out)
	}

	// Malformed operators and pool arguments are usage errors.
	for _, args := range [][]string{
		{".r", "1d6r"},
		{".r", "1d6c"},
		{".r", "1d6!!"},
		{"w", "5a"},
		{"w", "0a8"},
		{"w", "5a11"},
	} {
		if code, _, _ := runCLI(t, root, args...); code != 2 {
			t.Fatalf("%v: code=%d, want 2", args, code)
		}
	}
}
