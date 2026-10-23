package integration

import (
	"strings"
	"testing"
)

// P4: character generation. The rolled method creates a card per character;
// the detailed mode uses the point-buy method and reports the remaining
// points.
func TestCharacterGeneration(t *testing.T) {
	root := t.TempDir()

	// `.coc` generates one investigator and stores it as a normal card.
	code, out, errs := runCLI(t, root, "coc")
	if code != 0 {
		t.Fatalf("coc code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "🎲") || !strings.Contains(out, "STR") {
		t.Fatalf("coc output: %q", out)
	}
	// The rulebook's character rules finish the card after the rolled
	// characteristics: the initial HP/MP/SAN tracks are derived and
	// recorded right away (no manual `st derived --init`).
	if !strings.Contains(out, "HP ") || !strings.Contains(out, "SAN ") {
		t.Fatalf("coc output missing derived vitals: %q", out)
	}
	code, out, errs = runCLI(t, root, "pc", "show")
	if code != 0 || !strings.Contains(out, "状态：") ||
		!strings.Contains(out, "hp ") || !strings.Contains(out, "san ") {
		t.Fatalf("generated card missing derived tracks: code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "pc", "list")
	if code != 0 || !strings.Contains(out, "c-") {
		t.Fatalf("generated card missing: %q %s", out, errs)
	}

	// A batch generates several cards ("roll ten, pick one" workflow).
	code, out, errs = runCLI(t, root, "coc", "3")
	if code != 0 {
		t.Fatalf("coc 3 code=%d err=%s", code, errs)
	}
	if n := strings.Count(out, "🎲"); n != 3 {
		t.Fatalf("want 3 characters, got %d: %q", n, out)
	}
	code, out, errs = runCLI(t, root, "pc", "list")
	if code != 0 {
		t.Fatalf("pc list code=%d err=%s", code, errs)
	}
	if n := strings.Count(out, "c-"); n != 4 {
		t.Fatalf("cards = %d, want 4: %q", n, out)
	}

	// The detailed mode is the point-buy method; it creates one character
	// and reports the point budget.
	code, out, errs = runCLI(t, root, "coc6d")
	if code != 0 {
		t.Fatalf("coc6d code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "点数") {
		t.Fatalf("detailed output missing the point report: %q", out)
	}

	// The edition can be attached to the name (Dice! `.coc7 10`).
	code, out, errs = runCLI(t, root, "coc7", "2")
	if code != 0 || strings.Count(out, "🎲") != 2 {
		t.Fatalf("coc7 2 code=%d out=%q err=%s", code, out, errs)
	}

	// Names and counts are just positional arguments.
	code, out, errs = runCLI(t, root, "coc", "2", "--name", "测试员")
	if code != 0 || strings.Count(out, "🎲") != 2 {
		t.Fatalf("coc 2 --name code=%d out=%q err=%s", code, out, errs)
	}
	if !strings.Contains(out, "测试员 1") || !strings.Contains(out, "测试员 2") {
		t.Fatalf("named characters: %q", out)
	}

	// The structured contract carries the attributes and the rolls.
	code, out, errs = runCLI(t, root, "--json", "coc", "1")
	if code != 0 {
		t.Fatalf("json coc code=%d err=%s", code, errs)
	}
	for _, want := range []string{`"kind":"generation"`, `"variant_id":"7"`, `"characters":[`, `"attributes":{`, `"id":"c-`, `"vitals":[`, `"resource":"san"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("json generation missing %q: %q", want, out)
		}
	}

	// Usage errors: a detailed batch, an unknown variant and a bad count.
	for _, args := range [][]string{
		{"coc6d", "2"},
		{"coc9"},
		{"coc", "0"},
		{"coc", "abc"},
		{"dnd", "0"},
	} {
		if code, _, _ := runCLI(t, root, args...); code != 2 {
			t.Fatalf("%v: code=%d, want 2", args, code)
		}
	}
}

// The generated card is an ordinary card: it can be checked, shown and
// re-rolled.
func TestGeneratedCharacterIsUsable(t *testing.T) {
	root := t.TempDir()
	code, out, errs := runCLI(t, root, "coc")
	if code != 0 {
		t.Fatalf("coc code=%d err=%s", code, errs)
	}
	code, out, errs = runCLI(t, root, "st", "show")
	if code != 0 || !strings.Contains(out, "STR") {
		t.Fatalf("st show code=%d out=%q err=%s", code, out, errs)
	}
	code, _, errs = runCLI(t, root, "ra", "力量")
	if code != 0 {
		t.Fatalf("ra code=%d err=%s", code, errs)
	}
	// `pc redo` regenerates the attributes of the current card.
	code, out, errs = runCLISeed(t, root, 3, "pc", "redo")
	if code != 0 {
		t.Fatalf("pc redo code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "🎲") {
		t.Fatalf("pc redo output: %q", out)
	}
	// Redo re-initializes the derived tracks from the new characteristics.
	if !strings.Contains(out, "HP ") || !strings.Contains(out, "SAN ") {
		t.Fatalf("pc redo output missing derived vitals: %q", out)
	}
	code, out, errs = runCLI(t, root, "st", "show")
	if code != 0 || !strings.Contains(out, "STR") {
		t.Fatalf("post-redo sheet: %q", out)
	}
	code, out, errs = runCLI(t, root, "pc", "show")
	if code != 0 || !strings.Contains(out, "状态：") || !strings.Contains(out, "san ") {
		t.Fatalf("post-redo card missing tracks: code=%d out=%q err=%s", code, out, errs)
	}
}
