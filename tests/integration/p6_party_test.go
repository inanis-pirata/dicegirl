package integration

import (
	"strings"
	"testing"
)

// P6.1: the initiative list (Dice! `.ri` / `.init`).
func TestInitiativeList(t *testing.T) {
	root := t.TempDir()

	// A fixed value, a modifier and a custom expression.
	code, out, errs := runCLI(t, root, "ri", "80", "怪物甲")
	if code != 0 || !strings.Contains(out, "怪物甲 80") {
		t.Fatalf("ri fixed code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "ri", "+5", "首领")
	if code != 0 {
		t.Fatalf("ri modifier code=%d err=%s", code, errs)
	}
	// The list is ordered from high to low.
	code, out, errs = runCLI(t, root, "init")
	if code != 0 {
		t.Fatalf("init code=%d err=%s", code, errs)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 entries, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "怪物甲") || !strings.Contains(lines[1], "首领") {
		t.Fatalf("initiative order wrong: %q", out)
	}

	// A custom expression ("2DK" is the D&D shorthand for 2d20 keep high).
	code, out, errs = runCLI(t, root, "ri", "2DK", "英雄")
	if code != 0 {
		t.Fatalf("ri expr code=%d err=%s", code, errs)
	}
	code, out, errs = runCLI(t, root, "init")
	if code != 0 || !strings.Contains(out, "英雄") {
		t.Fatalf("init after expr: %q", out)
	}

	// The list survives across processes and can be cleared.
	code, out, errs = runCLI(t, root, "init", "clr")
	if code != 0 {
		t.Fatalf("init clr code=%d err=%s", code, errs)
	}
	code, out, errs = runCLI(t, root, "init")
	if code != 0 || !strings.Contains(out, "为空") {
		t.Fatalf("init after clear: %q", out)
	}

	// The structured contract is an ordered list.
	if code, _, errs := runCLI(t, root, "ri", "12", "甲"); code != 0 {
		t.Fatalf("ri code=%d err=%s", code, errs)
	}
	code, out, errs = runCLI(t, root, "--json", "init")
	if code != 0 {
		t.Fatalf("json init code=%d err=%s", code, errs)
	}
	for _, want := range []string{`"kind":"initiative"`, `"entries":[`, `"name":"甲"`, `"value":12`} {
		if !strings.Contains(out, want) {
			t.Fatalf("json initiative missing %q: %q", want, out)
		}
	}

	// Usage errors.
	for _, args := range [][]string{
		{"ri", "+x"},
		{"init", "nope"},
	} {
		if code, _, _ := runCLI(t, root, args...); code != 2 {
			t.Fatalf("%v: code=%d, want 2", args, code)
		}
	}
}

// P6.2: the card decks (Dice! `.deck` / `.draw`).
func TestCardDecks(t *testing.T) {
	root := t.TempDir()

	// A deck definition; "::3::市民" repeats a card.
	code, out, errs := runCLI(t, root, "deck", "new", "村民=村民|::3::市民|铁匠")
	if code != 0 {
		t.Fatalf("deck new code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "5/5") {
		t.Fatalf("multiplicity not expanded: %q", out)
	}

	// An arithmetic range deck and an instance of another deck.
	code, out, errs = runCLI(t, root, "deck", "set", "计数=range 1 5")
	if code != 0 || !strings.Contains(out, "5/5") {
		t.Fatalf("deck set range code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "deck", "set", "实例=村民")
	if code != 0 || !strings.Contains(out, "5/5") {
		t.Fatalf("deck set instance code=%d out=%q err=%s", code, out, errs)
	}

	// Drawing does not put the cards back.
	code, out, errs = runCLI(t, root, "draw", "计数", "2")
	if code != 0 {
		t.Fatalf("draw code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "剩 3 张") {
		t.Fatalf("draw output: %q", out)
	}
	code, out, errs = runCLI(t, root, "deck", "show", "计数")
	if code != 0 || !strings.Contains(out, "3/5") {
		t.Fatalf("deck show after draw: %q", out)
	}

	// The remaining queue persists across processes.
	code, out, errs = runCLI(t, root, "deck", "show", "计数")
	if code != 0 || !strings.Contains(out, "3/5") {
		t.Fatalf("deck show after restart: %q", out)
	}

	// Reset restores the definition.
	code, out, errs = runCLI(t, root, "deck", "reset", "计数")
	if code != 0 || !strings.Contains(out, "5/5") {
		t.Fatalf("deck reset: %q", out)
	}

	// Drawing more than the remaining size is a usage error.
	if code, _, _ := runCLI(t, root, "draw", "计数", "9"); code != 2 {
		t.Fatalf("over-draw code=%d, want 2", code)
	}
	if code, _, _ := runCLI(t, root, "draw", "不存在"); code != 2 {
		t.Fatalf("unknown deck code=%d, want 2", code)
	}

	// The deck list and the clear.
	code, out, errs = runCLI(t, root, "deck", "show")
	if code != 0 || !strings.Contains(out, "村民") || !strings.Contains(out, "计数") {
		t.Fatalf("deck show: %q", out)
	}
	code, _, errs = runCLI(t, root, "deck", "clr")
	if code != 0 {
		t.Fatalf("deck clr code=%d err=%s", code, errs)
	}
	code, out, errs = runCLI(t, root, "deck", "show")
	if code != 0 || !strings.Contains(out, "没有牌堆") {
		t.Fatalf("deck show after clear: %q", out)
	}

	// The structured contract of a draw.
	if code, _, errs := runCLI(t, root, "deck", "new", "测试=A|B"); code != 0 {
		t.Fatalf("deck new code=%d err=%s", code, errs)
	}
	code, out, errs = runCLI(t, root, "--json", "draw", "测试", "1")
	if code != 0 {
		t.Fatalf("json draw code=%d err=%s", code, errs)
	}
	for _, want := range []string{`"kind":"draw"`, `"deck":"测试"`, `"cards":[`, `"left":1`} {
		if !strings.Contains(out, want) {
			t.Fatalf("json draw missing %q: %q", want, out)
		}
	}

	// Usage errors.
	for _, args := range [][]string{
		{"deck"},
		{"deck", "new"},
		{"deck", "new", "缺少等号"},
		{"deck", "reset"},
		{"draw"},
		{"draw", "测试", "0"},
	} {
		if code, _, _ := runCLI(t, root, args...); code != 2 {
			t.Fatalf("%v: code=%d, want 2", args, code)
		}
	}
}
