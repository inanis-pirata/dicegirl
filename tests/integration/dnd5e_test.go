package integration

import (
	"strings"
	"testing"
)

// The built-in D&D 5e (2014) rulebook is data like the CoC book: the d20
// mechanic, advantage, DCs, derived statistics, random tables and the
// generation methods all come from rulebooks/dnd5e.yaml.
func TestDnD5eRulebook(t *testing.T) {
	root := t.TempDir()

	// Both built-in books are loaded and listed.
	code, out, errs := runCLI(t, root, ".sys")
	if code != 0 {
		t.Fatalf("systems list code=%d err=%s", code, errs)
	}
	for _, want := range []string{"coc7", "dnd5e", "Dungeons & Dragons"} {
		if !strings.Contains(out, want) {
			t.Fatalf("systems list missing %q: %q", want, out)
		}
	}

	// A 5e character: six ability scores and the active card's system picks
	// the dnd5e book for the following commands.
	code, _, errs = runCLI(t, root, ".pc", "new", "战士", "--system", "dnd5e", "--active",
		"STR=16", "DEX=14", "CON=15", "INT=8", "WIS=10", "CHA=12")
	if code != 0 {
		t.Fatalf("create code=%d err=%s", code, errs)
	}

	// Ability modifiers use floor division, and the derived values include
	// initiative, passive perception, unarmored AC and hit points.
	code, out, errs = runCLI(t, root, ".dv")
	if code != 0 {
		t.Fatalf("derived code=%d err=%s", code, errs)
	}
	for _, want := range []string{
		"力量调整值 3", "敏捷调整值 2", "体质调整值 2", "智力调整值 -1",
		"被动察觉 10", "护甲等级（无甲） 12", "HP 上限（1 级 d8 职业） 10",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("derived output missing %q: %q", want, out)
		}
	}

	// The d20 family rolls one d20 and succeeds by reaching the target.
	code, out, errs = runCLISeed(t, root, 7, "--json", ".ra", "15")
	if code != 0 {
		t.Fatalf("check code=%d err=%s", code, errs)
	}
	for _, want := range []string{`"system_id":"dnd5e"`, `"mechanic":"expression"`, `"roll_detail":{`} {
		if !strings.Contains(out, want) {
			t.Fatalf("json check missing %q: %q", want, out)
		}
	}

	// Advantage and disadvantage are bonus/penalty dice.
	code, out, errs = runCLI(t, root, ".ra", "15+1")
	if code != 0 || !strings.Contains(out, "取高") {
		t.Fatalf("advantage code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, ".ra", "15-1")
	if code != 0 || !strings.Contains(out, "取低") {
		t.Fatalf("disadvantage code=%d out=%q err=%s", code, out, errs)
	}

	// A character's own bonus is folded into the target with --mod.
	code, out, errs = runCLI(t, root, ".ra", "15", "--mod", "-5")
	if code != 0 || !strings.Contains(out, "目标 10（原始 15，-5）") {
		t.Fatalf("mod check code=%d out=%q err=%s", code, out, errs)
	}

	// Relative DC difficulties come from the book.
	code, out, errs = runCLI(t, root, ".ra", "困难", "15")
	if code != 0 || !strings.Contains(out, "目标 20") {
		t.Fatalf("hard difficulty code=%d out=%q err=%s", code, out, errs)
	}

	// Rolled generation: 4d6 drop the lowest, clamped to 3..18.
	code, out, errs = runCLI(t, root, ".dnd", "2")
	if code != 0 {
		t.Fatalf("generate code=%d err=%s", code, errs)
	}
	if strings.Count(out, "🎲") != 2 || !strings.Contains(out, "STR ") {
		t.Fatalf("generate output: %q", out)
	}

	// Detailed generation is the 27-point buy, which spends exactly the
	// classic standard array in the declared priority order.
	code, out, errs = runCLI(t, root, ".dndd")
	if code != 0 {
		t.Fatalf("detailed generate code=%d err=%s", code, errs)
	}
	for _, want := range []string{"STR 15", "CON 14", "DEX 13", "WIS 12", "CHA 10", "INT 8", "点数 27/27"} {
		if !strings.Contains(out, want) {
			t.Fatalf("detailed generate missing %q: %q", want, out)
		}
	}

	// The book's random tables are available (and only that book's).
	code, out, errs = runCLI(t, root, ".tb", "list", "--system", "dnd5e")
	if code != 0 {
		t.Fatalf("table list code=%d err=%s", code, errs)
	}
	for _, want := range []string{"exhaustion", "wild_magic_surge", "short_term_madness"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table list missing %q: %q", want, out)
		}
	}
	code, out, errs = runCLI(t, root, ".tb", "wild_magic_surge", "--system", "dnd5e")
	if code != 0 || !strings.Contains(out, "狂野魔法浪涌") {
		t.Fatalf("table roll code=%d out=%q err=%s", code, out, errs)
	}

	// The quick reference is searchable by keyword.
	code, out, errs = runCLI(t, root, ".rules", "优势")
	if code != 0 || !strings.Contains(out, "优势与劣势") {
		t.Fatalf("rules search code=%d out=%q err=%s", code, out, errs)
	}
}

// D&D is played through the chat-style short commands: `.dnd`/`.dndd`
// generate cards, `.ra` rolls checks with the active card's system, and
// `.rules` searches the book.
func TestDnD5eShortCommands(t *testing.T) {
	root := t.TempDir()

	// `.dnd` generates one dnd5e character card and makes it active.
	code, out, errs := runCLI(t, root, ".dnd")
	if code != 0 {
		t.Fatalf(".dnd code=%d err=%s", code, errs)
	}
	if strings.Count(out, "🎲") != 1 || !strings.Contains(out, "STR ") {
		t.Fatalf(".dnd output: %q", out)
	}
	// The rulebook's initial HP track is derived and recorded on creation.
	if !strings.Contains(out, "HP ") {
		t.Fatalf(".dnd output missing derived HP: %q", out)
	}
	code, out, errs = runCLI(t, root, ".pc", "show")
	if code != 0 || !strings.Contains(out, "状态：hp ") {
		t.Fatalf("generated dnd card missing HP track: code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, ".pc", "list")
	if code != 0 || !strings.Contains(out, "[dnd5e]") {
		t.Fatalf(".pc list code=%d out=%q err=%s", code, out, errs)
	}

	// The active card picks the dnd5e book: one d20 against the target.
	code, out, errs = runCLI(t, root, ".ra", "15")
	if code != 0 || !strings.Contains(out, "d20") {
		t.Fatalf(".ra explicit target code=%d out=%q err=%s", code, out, errs)
	}

	// Inline +1/-1 mean advantage/disadvantage; --mod folds a bonus into
	// the target.
	code, out, errs = runCLI(t, root, ".ra", "15+1")
	if code != 0 || !strings.Contains(out, "取高") {
		t.Fatalf(".ra advantage code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, ".ra", "15-1")
	if code != 0 || !strings.Contains(out, "取低") {
		t.Fatalf(".ra disadvantage code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, ".ra", "15", "--mod", "-5")
	if code != 0 || !strings.Contains(out, "目标 10（原始 15，-5）") {
		t.Fatalf(".ra --mod code=%d out=%q err=%s", code, out, errs)
	}

	// Difficulty keywords are rulebook data.
	code, out, errs = runCLI(t, root, ".ra", "困难", "15")
	if code != 0 || !strings.Contains(out, "目标 20") {
		t.Fatalf(".ra hard code=%d out=%q err=%s", code, out, errs)
	}

	// `.dndd` is the 27-point buy and spends the standard array.
	code, out, errs = runCLI(t, root, ".dndd")
	if code != 0 {
		t.Fatalf(".dndd code=%d err=%s", code, errs)
	}
	for _, want := range []string{"STR 15", "CON 14", "DEX 13", "WIS 12", "CHA 10", "INT 8", "点数 27/27"} {
		if !strings.Contains(out, want) {
			t.Fatalf(".dndd output missing %q: %q", want, out)
		}
	}

	// A bare number is the batch size.
	code, out, errs = runCLI(t, root, ".dnd", "2")
	if code != 0 || strings.Count(out, "🎲") != 2 {
		t.Fatalf(".dnd 2 code=%d out=%q err=%s", code, out, errs)
	}

	// Detailed creation is single-character only.
	code, out, errs = runCLI(t, root, ".dnd", "d", "2")
	if code != 2 || !strings.Contains(errs, "一次只能生成一个") {
		t.Fatalf(".dnd d 2 code=%d out=%q err=%s", code, out, errs)
	}
}

// Without --system and without an active card, commands fall back to the
// rulebook that declares default: true (coc7), so a second built-in book
// does not break the single-book workflow.
func TestDefaultRulebook(t *testing.T) {
	root := t.TempDir()

	code, out, errs := runCLI(t, root, ".ra", "60")
	if code != 0 {
		t.Fatalf("default check code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "十位") || strings.Contains(out, "d20 =") {
		t.Fatalf("default book must be coc7 (percentile): %q", out)
	}
}
