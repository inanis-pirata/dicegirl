package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The administrative short commands cover what the removed long commands
// used to do: card management, export/import, derived/damage/opposed/table
// helpers and rulebook validation.
func TestShortAdminCommands(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲")

	// pc active prints the current card id (stdout).
	code, out, errs := runCLI(t, root, ".pc", "active")
	if code != 0 {
		t.Fatalf("pc active code=%d err=%s", code, errs)
	}
	first := strings.TrimSpace(out)
	if !strings.HasPrefix(first, "c-") {
		t.Fatalf("active id = %q", first)
	}

	// pc new activates the card and initializes its tracks by default. A
	// card with incomplete characteristics is still created, with a hint
	// to finish it with `dv --init`.
	code, out, errs = runCLI(t, root, ".pc", "new", "调查员乙", "STR=30")
	if code != 0 {
		t.Fatalf("pc new code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "属性未填全") {
		t.Fatalf("partial card should hint dv --init: %q", out)
	}
	code, out, _ = runCLI(t, root, ".pc", "active")
	if strings.TrimSpace(out) == first || !strings.HasPrefix(strings.TrimSpace(out), "c-") {
		t.Fatalf("pc new must activate the new card: %q", out)
	}

	// --active=false leaves the current card untouched; with complete
	// characteristics the derived tracks are recorded on creation.
	code, out, errs = runCLI(t, root, ".pc", "new", "调查员丙",
		"STR=50", "CON=60", "SIZ=70", "DEX=55", "POW=55", "EDU=70", "--active=false")
	if code != 0 {
		t.Fatalf("pc new --active=false code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "HP ") || !strings.Contains(out, "SAN ") {
		t.Fatalf("complete card should record derived tracks: %q", out)
	}
	code, out, _ = runCLI(t, root, ".pc", "active")
	if strings.TrimSpace(out) != cardIDOf(t, root, "调查员乙") {
		t.Fatalf("--active=false must keep the active card: %q", out)
	}

	ids := characterIDs(t, root)
	if len(ids) != 3 {
		t.Fatalf("want 3 cards, got %d", len(ids))
	}

	// pc use accepts the card name (players never see the generated id).
	if code, _, errs := runCLI(t, root, ".pc", "use", "调查员乙"); code != 0 {
		t.Fatalf("pc use code=%d err=%s", code, errs)
	}
	code, out, _ = runCLI(t, root, ".pc", "active")
	if strings.TrimSpace(out) != cardIDOf(t, root, "调查员乙") {
		t.Fatalf("active after use = %q", out)
	}

	// pc copy duplicates a card under a new name.
	if code, _, errs := runCLI(t, root, ".pc", "copy", "调查员乙", "调查员丁"); code != 0 {
		t.Fatalf("pc copy code=%d err=%s", code, errs)
	}
	code, out, _ = runCLI(t, root, ".pc", "list")
	if !strings.Contains(out, "调查员丁") {
		t.Fatalf("pc copy output: %q", out)
	}

	// pc del removes one card by name.
	if code, _, errs := runCLI(t, root, ".pc", "del", "调查员乙"); code != 0 {
		t.Fatalf("pc del code=%d err=%s", code, errs)
	}
	code, out, _ = runCLI(t, root, ".pc", "list")
	if strings.Contains(out, "调查员乙") {
		t.Fatalf("pc del left the card behind: %q", out)
	}

	// pc val validates a card against its rulebook.
	if code, _, errs := runCLI(t, root, ".pc", "val", "调查员甲"); code != 0 {
		t.Fatalf("pc val code=%d err=%s", code, errs)
	}

	// Unknown names are NOT_FOUND, not a silent success.
	if code, _, errs := runCLI(t, root, ".pc", "show", "不存在"); code != 1 || !strings.Contains(errs, "NOT_FOUND") {
		t.Fatalf("unknown card code=%d err=%q", code, errs)
	}

	// Export/import round trip: export by name, delete it, import the file
	// back.
	exported := filepath.Join(root, "card.yaml")
	if code, _, errs := runCLI(t, root, ".pc", "export", "--file", exported, "调查员甲"); code != 0 {
		t.Fatalf("pc export code=%d err=%s", code, errs)
	}
	if _, err := os.Stat(exported); err != nil {
		t.Fatalf("exported file missing: %v", err)
	}
	if code, _, errs := runCLI(t, root, ".pc", "del", "调查员甲"); code != 0 {
		t.Fatalf("pc del before import code=%d err=%s", code, errs)
	}
	if code, _, errs := runCLI(t, root, ".pc", "import", "--file", exported); code != 0 {
		t.Fatalf("pc import code=%d err=%s", code, errs)
	}
	code, out, _ = runCLI(t, root, ".pc", "list")
	if !strings.Contains(out, "调查员甲") {
		t.Fatalf("pc import did not restore the card: %q", out)
	}
	// Deleting the active card clears the pointer; restore it for the
	// derived command below.
	if code, _, errs := runCLI(t, root, ".pc", "use", "调查员甲"); code != 0 {
		t.Fatalf("pc use after import code=%d err=%s", code, errs)
	}

	// Derived values and damage still work through the short commands.
	if code, out, errs := runCLI(t, root, ".dv"); code != 0 || !strings.Contains(out, "HP") {
		t.Fatalf("dv code=%d out=%q err=%s", code, out, errs)
	}
	// --character accepts the card name too.
	if code, _, errs := runCLI(t, root, ".dv", "--character", "调查员甲"); code != 0 {
		t.Fatalf("dv --character name code=%d err=%s", code, errs)
	}
	if code, _, errs := runCLI(t, root, ".ra", "60", "--character", "调查员甲"); code != 0 {
		t.Fatalf("ra --character name code=%d err=%s", code, errs)
	}
	if code, _, errs := runCLI(t, root, ".dmg", "1d8"); code != 0 {
		t.Fatalf("dmg code=%d err=%s", code, errs)
	}

	// Opposed checks take two target values.
	code, out, errs = runCLI(t, root, ".opp", "50", "60")
	if code != 0 || out == "" {
		t.Fatalf("opp code=%d out=%q err=%s", code, out, errs)
	}

	// Rulebook validation is reachable as `sys validate`.
	if code, _, errs := runCLI(t, root, ".sys", "validate"); code != 0 {
		t.Fatalf("sys validate code=%d err=%s", code, errs)
	}
}
