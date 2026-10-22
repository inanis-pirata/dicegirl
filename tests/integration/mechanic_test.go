package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The engine's check mechanic is pluggable: a rulebook may pick the d20
// family (roll >= target) instead of the percentile family. This test
// installs the shipped example book and drives it end to end.
func TestSecondMechanicRulebook(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "rulebooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "docs", "examples", "dnd-lite.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dnd-lite.yaml"), src, 0o600); err != nil {
		t.Fatal(err)
	}

	// Both books are loaded and listed.
	code, out, errs := runCLI(t, root, ".sys")
	if code != 0 {
		t.Fatalf("systems list code=%d err=%s", code, errs)
	}
	for _, want := range []string{"coc7", "dnd-lite"} {
		if !strings.Contains(out, want) {
			t.Fatalf("systems list missing %q: %q", want, out)
		}
	}

	// A card of the second book.
	code, _, errs = runCLI(t, root, ".pc", "new", "战士", "--system", "dnd-lite", "--active",
		"STR=16", "DEX=12", "CON=14", "INT=10", "WIS=13", "CHA=8")
	if code != 0 {
		t.Fatalf("create code=%d err=%s", code, errs)
	}

	// The d20 family: the roll must reach the target.
	code, out, errs = runCLISeed(t, root, 3, ".ra", "athletics", "15")
	if code != 0 {
		t.Fatalf("check code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "d20") || !strings.Contains(out, "目标 15") {
		t.Fatalf("d20 check output: %q", out)
	}

	// Advantage: two d20, keep the higher.
	code, out, errs = runCLISeed(t, root, 3, ".ra", "15+1")
	if code != 0 {
		t.Fatalf("advantage code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "取高") {
		t.Fatalf("advantage output: %q", out)
	}

	// The difficulty modifier shifts the DC.
	code, out, errs = runCLISeed(t, root, 3, ".ra", "困难", "15")
	if code != 0 || !strings.Contains(out, "目标 20") {
		t.Fatalf("hard difficulty code=%d out=%q err=%s", code, out, errs)
	}

	// The structured contract names the mechanic.
	code, out, errs = runCLISeed(t, root, 3, "--json", ".ra", "15")
	if code != 0 {
		t.Fatalf("json check code=%d err=%s", code, errs)
	}
	for _, want := range []string{`"system_id":"dnd-lite"`, `"mechanic":"expression"`, `"roll_detail":{`} {
		if !strings.Contains(out, want) {
			t.Fatalf("json check missing %q: %q", want, out)
		}
	}

	// The CoC book uses the generic expression family with a d100 label.
	code, out, errs = runCLI(t, root, ".ra", "--system", "coc7", "60")
	if code != 0 {
		t.Fatalf("coc7 check code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "d100 =") || strings.Contains(out, "d20 =") {
		t.Fatalf("coc7 check must use the d100 expression: %q", out)
	}
}
