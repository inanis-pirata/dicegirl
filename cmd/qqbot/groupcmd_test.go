package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBindCommand(t *testing.T) {
	for _, in := range []string{"绑定 1A2B3C4D", ".绑定 1A2B3C4D", "bind 1A2B3C4D", "!绑定 1A2B3C4D"} {
		if code, ok := parseBindCommand(in); !ok || code != "1A2B3C4D" {
			t.Fatalf("%q: code=%q ok=%v", in, code, ok)
		}
	}
	for _, in := range []string{"", "绑定", "绑定 1 2", "大家好啊", ".r 1d6"} {
		if _, ok := parseBindCommand(in); ok {
			t.Fatalf("%q should not parse as a bind command", in)
		}
	}
}

func TestGroupStorePersistsBindings(t *testing.T) {
	root := t.TempDir()
	st, err := loadGroupStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.update(func(gs *groupState) error {
		gs.Links["member-1"] = memberLink{UserOpenid: "user-1"}
		gs.Bindings["member-1"] = map[string]cardBinding{
			"default": {CardID: "c-1234", CardName: "张三"},
			"t-other": {CardID: "c-9999", CardName: "李四的小号"},
		}
		gs.Observers["member-2"] = observerEntry{Enabled: true}
		gs.ObEnabled = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Reload from disk: everything survives, keyed per table.
	again, err := loadGroupStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if uid, ok := again.link("member-1"); !ok || uid != "user-1" {
		t.Fatalf("link = %q, %v", uid, ok)
	}
	if b, ok := again.binding("member-1", "default"); !ok || b.CardName != "张三" {
		t.Fatalf("default-table binding = %+v, %v", b, ok)
	}
	if b, ok := again.binding("member-1", "t-other"); !ok || b.CardID != "c-9999" {
		t.Fatalf("other-table binding = %+v, %v", b, ok)
	}
	if _, ok := again.binding("member-1", "t-nope"); ok {
		t.Fatal("binding unexpectedly found on an unknown table")
	}
	// member-2 has no private link yet, so it is not pushed to.
	if got := again.observerUserOpenids(""); len(got) != 0 {
		t.Fatalf("observers without links: %v", got)
	}

	// After linking, the observer is addressable and the roller is skipped.
	if err := again.update(func(gs *groupState) error {
		gs.Links["member-2"] = memberLink{UserOpenid: "user-2"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got := again.observerUserOpenids("member-2")
	if len(got) != 0 {
		t.Fatalf("roller should be skipped: %v", got)
	}
	if got := again.observerUserOpenids("member-1"); len(got) != 1 || got[0] != "user-2" {
		t.Fatalf("observers = %v", got)
	}

	if _, err := os.Stat(filepath.Join(root, "adapter.json")); err != nil {
		t.Fatalf("state file missing: %v", err)
	}
}

func TestGroupStoreMigratesFlatBindings(t *testing.T) {
	root := t.TempDir()
	// An adapter.json written before tables existed: bindings are flat.
	legacy := `{
  "links": {"m1": {"user_openid": "u1", "updated_at": null}},
  "bindings": {"m1": {"card_id": "c-1", "card_name": "旧卡", "updated_at": null}},
  "observers": {},
  "ob_enabled": false,
  "log": {"active": false, "title": "", "file": "", "started_at": null, "entries": 0}
}`
	if err := os.WriteFile(filepath.Join(root, "adapter.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := loadGroupStore(root)
	if err != nil {
		t.Fatal(err)
	}
	b, ok := st.binding("m1", "default")
	if !ok || b.CardID != "c-1" || b.CardName != "旧卡" {
		t.Fatalf("flat binding not migrated to the default table: %+v %v", b, ok)
	}
	if _, ok := st.binding("m1", "t-other"); ok {
		t.Fatal("legacy binding leaked to another table")
	}
	// Persisting once must rewrite the nested shape.
	if err := st.update(func(gs *groupState) error { return nil }); err != nil {
		t.Fatal(err)
	}
	again, err := loadGroupStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := again.binding("m1", "default"); !ok || b.CardName != "旧卡" {
		t.Fatalf("reloaded migrated binding: %+v %v", b, ok)
	}
}

func TestHiddenCommands(t *testing.T) {
	cases := map[string]bool{
		".rh 3d6":     true,
		"。rh .ra 心理学": true,
		"rh":          true,
		".drawh 命运 2": true,
		"drawh 测试":    true,
		".draw 命运":    false,
		".r 1d6":      false,
		"":            false,
	}
	for line, want := range cases {
		if got := isHiddenCommand(line); got != want {
			t.Errorf("isHiddenCommand(%q) = %v, want %v", line, got, want)
		}
	}
	if name, body := hiddenParts(".drawh 命运 2"); name != "drawh" || body != "命运 2" {
		t.Fatalf("hiddenParts drawh = %q %q", name, body)
	}
	if name, body := hiddenParts("。rh .ra 心理学"); name != "rh" || body != ".ra 心理学" {
		t.Fatalf("hiddenParts rh = %q %q", name, body)
	}
	if got := hiddenNoun("drawh"); got != "暗抽" {
		t.Fatalf("hiddenNoun drawh = %q", got)
	}
	if got := hiddenNoun("rh"); got != "暗骰" {
		t.Fatalf("hiddenNoun rh = %q", got)
	}
}

func TestIsGroupPCCommand(t *testing.T) {
	cases := []struct {
		line string
		want string
		ok   bool
	}{
		{"pc tag 张三", "tag", true},
		{".pc grp", "grp", true},
		{"pc untag", "untag", true},
		{"pc show", "", false},
		{"pc list", "", false},
		{"st 力量=60", "", false},
	}
	for _, c := range cases {
		sub, ok := isGroupPCCommand(strings.Fields(c.line))
		if ok != c.ok || sub != c.want {
			t.Fatalf("%q => %q,%v want %q,%v", c.line, sub, ok, c.want, c.ok)
		}
	}
}

func TestAppendLogEntry(t *testing.T) {
	root := t.TempDir()
	st, err := loadGroupStore(root)
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.Join("logs", "s.md")
	if err := st.update(func(gs *groupState) error {
		gs.Log = logState{Active: true, Title: "测试团", File: rel}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	appendLogEntry(st, root, "张三", ".ra 心理学", "目标 50 → 成功")
	appendLogEntry(st, root, "李四", ".r 1d6", "4")

	raw, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"张三", ".ra 心理学", "目标 50 → 成功", "李四", ".r 1d6"} {
		if !strings.Contains(text, want) {
			t.Fatalf("log missing %q:\n%s", want, text)
		}
	}
	var entries int
	st.read(func(gs *groupState) { entries = gs.Log.Entries })
	if entries != 2 {
		t.Fatalf("entries = %d", entries)
	}

	// A stopped session records nothing.
	if err := st.update(func(gs *groupState) error { gs.Log.Active = false; return nil }); err != nil {
		t.Fatal(err)
	}
	appendLogEntry(st, root, "王五", ".r 1d100", "42")
	raw, _ = os.ReadFile(filepath.Join(root, rel))
	if strings.Contains(string(raw), "王五") {
		t.Fatal("inactive log still recorded an entry")
	}
}

func TestTailLines(t *testing.T) {
	if got := tailLines("a\nb\nc\n", 2); got != "b\nc" {
		t.Fatalf("tail = %q", got)
	}
	if got := tailLines("only", 5); got != "only" {
		t.Fatalf("tail = %q", got)
	}
}
