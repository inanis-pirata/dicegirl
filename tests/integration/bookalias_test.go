package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A rulebook declares its own command alias, so a new game needs no
// Go change: the user book below answers to ".mini".
func TestRulebookDeclaredAlias(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "rulebooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	book := `schema_version: "1"
id: "mini"
name: "Mini Game"
system_version: "0.1.0"
rules_version: "test"
description: "user book with its own command"
commands: ["mini"]
attributes:
  - { id: "STR", name: "力量" }
skills:
  melee: { base: 10, name: "近战" }
limits:
  characteristic: { min: 0, max: 99 }
  skill:          { min: 0, max: 99 }
  target:         { min: 0, max: 99 }
mechanic:
  max_bonus_penalty: 10
  roll: "1d100"
  meets: "roll <= target"
  bonus_keep: lowest
  penalty_keep: highest
  label: "d100"
checks:
  levels:
    - { id: success, name: "成功", when: "roll <= target" }
    - { id: fail,    name: "失败" }
opposed:
  fumble_level: fail
  tie_break: none
generation:
  characteristics:
    - { attr: STR, expr: "3d6" }
`
	if err := os.WriteFile(filepath.Join(dir, "mini.yaml"), []byte(book), 0o600); err != nil {
		t.Fatal(err)
	}

	// The alias comes from the rulebook data.
	code, out, errs := runCLI(t, root, ".mini")
	if code != 0 {
		t.Fatalf(".mini code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "STR") {
		t.Fatalf(".mini output: %q", out)
	}

	// The generated card belongs to that book.
	code, out, errs = runCLI(t, root, ".pc", "list")
	if code != 0 || !strings.Contains(out, "[mini]") {
		t.Fatalf("pc list code=%d out=%q err=%s", code, out, errs)
	}
}
