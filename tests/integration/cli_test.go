// Package integration covers end-to-end CLI flows: parsing, application
// services and file storage in a temporary data root.
package integration

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"

	"dicegirl/cmd/dicegirl/cli"
)

func runCLI(t *testing.T, root string, args ...string) (int, string, string) {
	t.Helper()
	return runCLISeed(t, root, 7, args...)
}

func runCLISeed(t *testing.T, root string, seed int64, args ...string) (int, string, string) {
	t.Helper()
	full := append([]string{"--data-root", root, "--seed", strconv.FormatInt(seed, 10)}, args...)
	var out, errb bytes.Buffer
	code := cli.Run(context.Background(), full, strings.NewReader(""), &out, &errb)
	return code, out.String(), errb.String()
}

func TestRollFlow(t *testing.T) {
	root := t.TempDir()
	code, out, errs := runCLI(t, root, ".r", "2d6+3")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errs)
	}
	if !strings.Contains(out, "2d6+3") {
		t.Fatalf("output missing expression: %q", out)
	}

	// Parse error exits with code 2 and is written to stderr.
	code, _, errs = runCLI(t, root, ".r", "1/0")
	if code != 1 { // DIVIDE_BY_ZERO is a runtime failure, not usage
		t.Fatalf("divide-by-zero code=%d", code)
	}
	if !strings.Contains(errs, "DIVIDE_BY_ZERO") {
		t.Fatalf("stderr missing code: %q", errs)
	}

	code, _, errs = runCLI(t, root, ".r", "@@@")
	if code != 2 || !strings.Contains(errs, "PARSE_ERROR") {
		t.Fatalf("parse error code=%d stderr=%q", code, errs)
	}
}

func TestCharacterAndCheckFlow(t *testing.T) {
	root := t.TempDir()

	code, out, errs := runCLI(t, root, ".pc", "new", "调查员甲",
		"STR=50", "CON=60", "SIZ=70",
		"DEX=55", "POW=55", "EDU=70",
		"psychology=60", "--active")
	if code != 0 {
		t.Fatalf("create code=%d stderr=%s", code, errs)
	}
	if !strings.Contains(out, "已创建角色") {
		t.Fatalf("create output: %q", out)
	}

	code, out, errs = runCLI(t, root, ".pc", "list")
	if code != 0 || !strings.Contains(out, "调查员甲") || !strings.Contains(out, "* c-") {
		t.Fatalf("list code=%d out=%q err=%s", code, out, errs)
	}

	code, _, errs = runCLI(t, root, ".ra", "心理学+1")
	if code != 0 {
		t.Fatalf("check code=%d stderr=%s", code, errs)
	}

	code, out, errs = runCLI(t, root, "--json", ".ra", "75")
	if code != 0 || !strings.Contains(out, `"level_code"`) {
		t.Fatalf("json check code=%d out=%q err=%s", code, out, errs)
	}

	code, _, errs = runCLI(t, root, ".r", "1d8+db")
	if code != 0 {
		t.Fatalf("damage code=%d stderr=%s", code, errs)
	}

	code, out, errs = runCLI(t, root, ".st", "derived", "--init")
	if code != 0 {
		t.Fatalf("derived code=%d stderr=%s", code, errs)
	}
	if !strings.Contains(out, "HP") {
		t.Fatalf("derived output: %q", out)
	}

	// The sanity resource check with --apply persists the change; a second
	// check still works afterwards.
	code, _, errs = runCLI(t, root, ".sc", "0/1", "--apply")
	if code != 0 {
		t.Fatalf("sanity code=%d stderr=%s", code, errs)
	}

	code, out, errs = runCLI(t, root, ".pc", "show")
	if code != 0 || !strings.Contains(out, "san") {
		t.Fatalf("show code=%d out=%q err=%s", code, out, errs)
	}
}

func TestRollStatementFlow(t *testing.T) {
	root := t.TempDir()

	// Repeat prefix plus trailing reason text.
	code, out, errs := runCLI(t, root, ".r", "3#1d4", "攻击")
	if code != 0 {
		t.Fatalf("repeat code=%d stderr=%s", code, errs)
	}
	for _, want := range []string{"🎲 攻击：3#1d4", "①", "②", "③"} {
		if !strings.Contains(out, want) {
			t.Fatalf("repeat output missing %q: %q", want, out)
		}
	}

	// Comparison suffix on a single roll: d20 can never reach 100.
	code, out, errs = runCLI(t, root, ".r", "1d20>=100")
	if code != 0 {
		t.Fatalf("compare code=%d stderr=%s", code, errs)
	}
	if !strings.Contains(out, "(>=100)") || !strings.Contains(out, "失败") {
		t.Fatalf("compare output: %q", out)
	}

	// Single-trial JSON keeps the flat RollResult contract, adding only the
	// comparison object; the repeat container must not appear.
	code, out, errs = runCLI(t, root, "--json", ".r", "1d20>=5")
	if code != 0 {
		t.Fatalf("json compare code=%d stderr=%s", code, errs)
	}
	// Top-level RollResult fields predate json tags and stay capitalized;
	// only the new comparison field uses a lowercase tag.
	if !strings.Contains(out, `"comparison"`) || !strings.Contains(out, `"Total"`) {
		t.Fatalf("single json missing fields: %q", out)
	}
	if strings.Contains(out, `"repeat"`) {
		t.Fatalf("single roll must not emit repeat container: %q", out)
	}

	// Repeated rolls use the batch container; reason rides alongside.
	code, out, errs = runCLI(t, root, "--json", ".r", "2#1d6", "伤害", "掷骰")
	if code != 0 {
		t.Fatalf("json repeat code=%d stderr=%s", code, errs)
	}
	for _, want := range []string{`"repeat":2`, `"rolls":[`, `"reason":"伤害 掷骰"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("repeat json missing %q: %q", want, out)
		}
	}

	// Reason on a single roll is echoed in text and JSON.
	code, out, errs = runCLI(t, root, ".r", "1d8+2", "中型刀伤害")
	if code != 0 || !strings.Contains(out, "中型刀伤害") {
		t.Fatalf("reason text code=%d out=%q err=%s", code, out, errs)
	}
	code, out, _ = runCLI(t, root, "--json", ".r", "1d8+2", "中型刀伤害")
	if !strings.Contains(out, `"reason":"中型刀伤害"`) {
		t.Fatalf("reason json: %q", out)
	}

	// Repeat beyond the 10-trial cap is a limit error (exit 1).
	code, _, errs = runCLI(t, root, ".r", "11#1d6")
	if code != 1 || !strings.Contains(errs, "LIMIT_EXCEEDED") {
		t.Fatalf("repeat cap code=%d stderr=%q", code, errs)
	}

	// Malformed statements are usage errors (exit 2).
	for _, in := range []string{"3#", "1d6>=", "0#1d6"} {
		code, _, errs = runCLI(t, root, ".r", in)
		if code != 2 || !strings.Contains(errs, "PARSE_ERROR") && !strings.Contains(errs, "INVALID_ARGUMENT") {
			t.Fatalf("%q: code=%d stderr=%q", in, code, errs)
		}
	}
}

func TestSystemsAndConfig(t *testing.T) {
	root := t.TempDir()
	code, out, errs := runCLI(t, root, ".rule", "list")
	if code != 0 || !strings.Contains(out, "coc7") {
		t.Fatalf("systems code=%d out=%q err=%s", code, out, errs)
	}
	code, _, errs = runCLI(t, root, ".rule", "validate", "--system", "coc7")
	if code != 0 {
		t.Fatalf("config validate code=%d stderr=%s", code, errs)
	}
	code, _, errs = runCLI(t, root, ".rule", "validate", "--system", "nope")
	if code != 1 || !strings.Contains(errs, "RULE_NOT_FOUND") {
		t.Fatalf("missing system code=%d stderr=%q", code, errs)
	}
}

func TestCharacterNotFound(t *testing.T) {
	root := t.TempDir()
	code, _, errs := runCLI(t, root, ".pc", "show", "c-deadbeef")
	if code != 1 || !strings.Contains(errs, "NOT_FOUND") {
		t.Fatalf("code=%d stderr=%q", code, errs)
	}
}

func TestUnknownCommand(t *testing.T) {
	root := t.TempDir()
	// The former long commands are gone: only commands remain.
	for _, cmd := range []string{"frobnicate", "roll", "character", "systems", "check"} {
		if code, _, _ := runCLI(t, root, cmd); code != 2 {
			t.Fatalf("%s: code=%d, want 2", cmd, code)
		}
	}
}
