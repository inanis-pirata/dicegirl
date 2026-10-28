package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rulebookInstallYAML is a minimal valid book in the generator's default
// shape (no `default: true`, so rule del is not blocked by the default
// guard). Field names must satisfy the engine's strict KnownFields decode.
const rulebookInstallYAML = `schema_version: "1"
id: "myrpg"
name: "我的规则"
system_version: "0.1.0"
rules_version: "1e"
description: "安装测试"
attributes:
  - { id: "STR", name: "力量" }
  - { id: "CON", name: "体质" }
limits:
  characteristic: { min: 0, max: 99 }
  skill: { min: 0, max: 99 }
  target: { min: 0, max: 99 }
mechanic:
  max_bonus_penalty: 2
  roll: "1d100"
  meets: "roll <= target"
  bonus_keep: lowest
  penalty_keep: highest
  label: "d100"
checks:
  levels:
    - { id: fumble, name: "大失败", when: "roll >= 96" }
    - { id: success, name: "成功", when: "roll <= target" }
    - { id: fail, name: "失败" }
opposed:
  fumble_level: fumble
  tie_break: higher_target
`

func writeRulebookFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// TestRulebookInstallLifecycle walks the whole rule add / rule del path:
// install, validate, duplicate handling, built-in protection and uninstall.
func TestRulebookInstallLifecycle(t *testing.T) {
	root := t.TempDir()
	src := writeRulebookFile(t, t.TempDir(), "myrpg.yaml", rulebookInstallYAML)

	// rule add installs, registers and persists.
	code, out, errs := runCLI(t, root, ".rule", "add", "--file", src)
	if code != 0 {
		t.Fatalf("rule add code=%d out=%s err=%s", code, out, errs)
	}
	if !strings.Contains(out+errs, "已安装规则书 myrpg") {
		t.Fatalf("rule add output: %q / %q", out, errs)
	}
	if _, err := os.Stat(filepath.Join(root, "rulebooks", "myrpg.yaml")); err != nil {
		t.Fatalf("rulebook file not persisted: %v", err)
	}
	code, out, _ = runCLI(t, root, ".rule", "list")
	if !strings.Contains(out, "myrpg") {
		t.Fatalf("rule list after add: %q", out)
	}
	if code, out, errs := runCLI(t, root, ".rule", "validate", "--system", "myrpg"); code != 0 {
		t.Fatalf("rule validate myrpg code=%d out=%s err=%s", code, out, errs)
	}

	// A second install without --force conflicts; with --force it replaces.
	code, _, errs = runCLI(t, root, ".rule", "add", "--file", src)
	if code == 0 || !strings.Contains(errs, "--force") {
		t.Fatalf("duplicate install must demand --force: code=%d err=%s", code, errs)
	}
	code, _, errs = runCLI(t, root, ".rule", "add", "--file", src, "--force")
	if code != 0 {
		t.Fatalf("force reinstall code=%d err=%s", code, errs)
	}

	// Built-in books cannot be shadowed or deleted.
	builtin := writeRulebookFile(t, t.TempDir(), "coc7.yaml",
		strings.Replace(rulebookInstallYAML, `id: "myrpg"`, `id: "coc7"`, 1))
	if code, _, errs := runCLI(t, root, ".rule", "add", "--file", builtin, "--force"); code == 0 || !strings.Contains(errs, "内置") {
		t.Fatalf("built-in overwrite must fail: code=%d err=%s", code, errs)
	}
	if code, _, errs := runCLI(t, root, ".rule", "del", "coc7"); code == 0 || !strings.Contains(errs, "内置") {
		t.Fatalf("built-in delete must fail: code=%d err=%s", code, errs)
	}

	// rule del removes a user book that nothing uses.
	if code, _, errs := runCLI(t, root, ".rule", "del", "myrpg"); code != 0 {
		t.Fatalf("rule del code=%d err=%s", code, errs)
	}
	if _, err := os.Stat(filepath.Join(root, "rulebooks", "myrpg.yaml")); !os.IsNotExist(err) {
		t.Fatalf("rulebook file must be removed, err=%v", err)
	}
	if _, out, _ := runCLI(t, root, ".rule", "list"); strings.Contains(out, "myrpg") {
		t.Fatalf("rule list after del: %q", out)
	}
	if code, _, errs := runCLI(t, root, ".rule", "del", "myrpg"); code == 0 || !strings.Contains(errs, "myrpg") {
		t.Fatalf("deleting an uninstalled book must fail: code=%d err=%s", code, errs)
	}
}

// TestRulebookInstallRejectsBadFiles proves the strict gate: neither a
// structurally broken nor a semantically invalid book ever reaches the disk.
func TestRulebookInstallRejectsBadFiles(t *testing.T) {
	root := t.TempDir()

	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{"unknown field", strings.Replace(rulebookInstallYAML, "attributes:",
			"not_a_real_field: 1\nattributes:", 1), "CONFIG_INVALID"},
		{"missing id", strings.Replace(rulebookInstallYAML, `id: "myrpg"`+"\n", "", 1), "id"},
		{"bad id", strings.Replace(rulebookInstallYAML, `id: "myrpg"`, `id: "Bad ID!"`, 1), "id"},
	}
	for _, tc := range cases {
		src := writeRulebookFile(t, t.TempDir(), "bad.yaml", tc.yaml)
		code, _, errs := runCLI(t, root, ".rule", "add", "--file", src)
		if code == 0 || !strings.Contains(errs, tc.wantErr) {
			t.Fatalf("%s: want %s failure, code=%d err=%s", tc.name, tc.wantErr, code, errs)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "rulebooks")); !os.IsNotExist(err) {
		t.Fatalf("no rulebook file may be written for rejected uploads: %v", err)
	}
}

// TestRulebookDelGuardsInUseBooks covers the two remaining uninstall guards:
// characters bound to the system, and the default book.
func TestRulebookDelGuardsInUseBooks(t *testing.T) {
	root := t.TempDir()
	src := writeRulebookFile(t, t.TempDir(), "myrpg.yaml", rulebookInstallYAML)
	if code, _, errs := runCLI(t, root, ".rule", "add", "--file", src); code != 0 {
		t.Fatalf("rule add code=%d err=%s", code, errs)
	}

	// A card on the system blocks deletion.
	if code, _, errs := runCLI(t, root, ".pc", "new", "游侠", "--system", "myrpg", "STR=50"); code != 0 {
		t.Fatalf("pc new code=%d err=%s", code, errs)
	}
	code, _, errs := runCLI(t, root, ".rule", "del", "myrpg")
	if code == 0 || !strings.Contains(errs, "角色卡") {
		t.Fatalf("in-use delete must fail: code=%d err=%s", code, errs)
	}
	if code, _, errs := runCLI(t, root, ".pc", "del", "游侠"); code != 0 {
		t.Fatalf("pc del code=%d err=%s", code, errs)
	}

	// The default book blocks deletion (here the built-in coc7 is the
	// default, so first make the user book the default via a reinstall).
	withDefault := strings.Replace(rulebookInstallYAML, "attributes:",
		"default: true\nattributes:", 1)
	srcDefault := writeRulebookFile(t, t.TempDir(), "myrpg2.yaml", withDefault)
	if code, _, errs := runCLI(t, root, ".rule", "add", "--file", srcDefault, "--force"); code != 0 {
		t.Fatalf("default reinstall code=%d err=%s", code, errs)
	}
	code, _, errs = runCLI(t, root, ".rule", "del", "myrpg")
	if code == 0 || !strings.Contains(errs, "默认") {
		t.Fatalf("default-book delete must fail: code=%d err=%s", code, errs)
	}
}
