package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dicegirl/bot"
	"dicegirl/internal/app"
	"dicegirl/internal/config"
	"dicegirl/internal/storage"
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

func TestLegacyLogMigrationRunsOnce(t *testing.T) {
	root := t.TempDir()
	legacy := `{
  "links": {},
  "bindings": {},
  "observers": {},
  "ob_enabled": false,
  "log": {"active": true, "title": "旧战报", "file": "logs/20260102-150405.md",
          "started_at": "2026-01-02T15:04:05+08:00", "entries": 2}
}`
	if err := os.WriteFile(filepath.Join(root, "adapter.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := loadGroupStore(root)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	run := func() error {
		return st.withLegacyLogMigration(func(log legacyLogState) error {
			calls++
			if !log.Active || log.Title != "旧战报" || log.File != "logs/20260102-150405.md" {
				t.Fatalf("legacy log = %+v", log)
			}
			if log.StartedAt.IsZero() {
				t.Fatal("legacy started_at was not parsed")
			}
			return nil
		})
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("migration ran %d times, want once", calls)
	}

	again, err := loadGroupStore(root)
	if err != nil {
		t.Fatal(err)
	}
	again.read(func(gs *groupState) {
		if !gs.LogMigrated {
			t.Fatal("migration marker not persisted")
		}
		if gs.LegacyLog.Title != "旧战报" {
			t.Fatalf("legacy state lost: %+v", gs.LegacyLog)
		}
	})

	// A failed run does not set the marker, so the next message retries.
	failRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(failRoot, "adapter.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	failStore, err := loadGroupStore(failRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := failStore.withLegacyLogMigration(func(legacyLogState) error {
		return os.ErrPermission
	}); err == nil {
		t.Fatal("expected migration failure")
	}
	failStore.read(func(gs *groupState) {
		if gs.LogMigrated {
			t.Fatal("marker set after a failed migration")
		}
	})
}

func TestHiddenCommands(t *testing.T) {
	cases := map[string]bool{
		".rh 3d6":     true,
		".h 3d6":      true,
		".rah 心理学":    true,
		".rch心理学":     true,
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

func TestIsCommandLine(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"r 1d6", true},
		{".ra 心理学", true},
		{"r3d6", true},
		{"ob join", true},
		{"绑定 1A2B3C4D", true},
		{"pc tag 张三", true},
		{"今天天气不错", false},
		{"reset", false},
		{"1d6", false},
	}
	for _, c := range cases {
		if got := isCommandLine(nil, c.line); got != c.want {
			t.Fatalf("isCommandLine(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

// TestGroupDispatchRejectsStateChangingPC pins the property the old dispatch
// violated: a group `pc` line that changes persistent state must be rejected,
// never handed to the general executor. The group's card store is shared by
// every member, so a fall-through means any member can run `pc del 张三`
// (another member's card), `pc clr` (every card) or `pc use 冯丽` (repoint the
// table's active card, so every unbound member's next check rolls 冯丽).
//
// The earlier test only asserted that isGroupPCCommand("pc show") returns
// false — the predicate was right, the fall-through after it was the bug.
//
// Known blind spot: this test compares the decision, not the execution. If
// someone later adds a second `runNormal` entry (or a fall-through onto the
// rejected case) inside runGroup's switch, this test still passes. The
// mitigation is structural: groupActionPCRejected's branch is postText +
// return, and the default branch is the only call to runNormal.
func TestGroupDispatchRejectsStateChangingPC(t *testing.T) {
	lines := []string{
		"pc del 张三", ".pc del 张三", "。pc del 张三",
		"pc clr", ".pc clr",
		"pc new 张三 STR=50", "pc nn 张三 李四",
		"pc use 张三", ".pc use 张三",
		"pc copy 张三 李四", "pc cpy 张三=李四",
		"pc meta k v", "pc import x", "pc import --file x",
		"pc redo", "pc build 张三",
		// pc export only reads the store, but --file writes a file on the
		// bot host and the payload is the whole sheet: refuse it wholesale.
		"pc export", "pc export 张三 --file out.yaml",
		// pc stat is read-only, but its `clr` form resets the card's stats.
		"pc stat clr", "pc stat clr 张三",
		// Not a subcommand at all: no meaningful group semantics.
		"pc 张三", "pc", ".pc", ".pc 张三", "pc nope",
	}
	for _, line := range lines {
		action, sub := groupDispatch(line)
		if action == groupActionNormal {
			t.Fatalf("groupDispatch(%q) reached the general executor: "+
				"any member could change cards in the shared group store", line)
		}
		if action != groupActionPCRejected || sub != "" {
			t.Fatalf("groupDispatch(%q) = %v,%q want %v,\"\"", line, action, sub, groupActionPCRejected)
		}
	}
}

// TestGroupDispatchAllowsReadOnlyPC pins the other half of the boundary: the
// subcommands that only read the card store keep working in groups, exactly
// as they did before the pc gate existed. Refusing them would be a product
// change smuggled in under a security fix.
func TestGroupDispatchAllowsReadOnlyPC(t *testing.T) {
	lines := []string{
		"pc show", "pc show 张三", ".pc show 张三",
		"pc list", "pc active",
		"pc val", "pc val 张三",
		"pc stat", "pc stat 张三",
	}
	for _, line := range lines {
		action, sub := groupDispatch(line)
		if action != groupActionNormal || sub != "" {
			t.Fatalf("groupDispatch(%q) = %v,%q want %v,\"\"", line, action, sub, groupActionNormal)
		}
	}
}

func TestGroupDispatchAllowsBindingSubcommands(t *testing.T) {
	cases := []struct{ line, sub string }{
		{"pc tag 张三", "tag"},
		{".pc untag", "untag"},
		{"。pc grp", "grp"},
	}
	for _, c := range cases {
		action, sub := groupDispatch(c.line)
		if action != groupActionPCBinding || sub != c.sub {
			t.Fatalf("groupDispatch(%q) = %v,%q want %v,%q", c.line, action, sub, groupActionPCBinding, c.sub)
		}
	}
}

func TestGroupDispatchOtherBranches(t *testing.T) {
	cases := []struct {
		line string
		want groupAction
	}{
		{".r 1d6", groupActionNormal},
		{".ra 心理学", groupActionNormal},
		{".st 力量=60", groupActionNormal},
		{"今天天气不错", groupActionNormal},
		{".rh 1d6", groupActionHidden},
		{".rah 心理学", groupActionHidden},
		{".drawh 命运", groupActionHidden},
		{"ob join", groupActionObserver},
		{".ob list", groupActionObserver},
	}
	for _, c := range cases {
		if got, _ := groupDispatch(c.line); got != c.want {
			t.Fatalf("groupDispatch(%q) = %v want %v", c.line, got, c.want)
		}
	}
}

// TestGroupTagWithoutNameDoesNotUseActiveCard pins the second half of the
// same bug, the one inside runPCBinding rather than the dispatch layer:
// `pc tag` with no card name used to fall back to the table's active card.
//
// That fallback made the refusal above pointless. ActiveID is group-wide
// state, not per-member: after anyone ran `pc use 冯丽` (now refused, but the
// value it wrote survives in the store, and `pc active` can still show it),
// every member's bare `pc tag` would bind them *permanently* to 冯丽's
// sheet — turning a temporary hijack into a permanent one, with the member
// never naming a card.
//
// The resolve callback is counted, not just stubbed. "Do not fall back"
// means the card store was never consulted at all; asserting only on the
// reply would still pass if someone reintroduced the fallback *and* kept
// the notice for the empty-result case.
func TestGroupTagWithoutNameDoesNotUseActiveCard(t *testing.T) {
	for _, args := range [][]string{nil, {}, {"  "}} {
		calls := 0
		id, reply := groupTagTarget(args, func(ref string) (string, error) {
			calls++
			return "c-someone-elses-card", nil
		})
		if calls != 0 {
			t.Fatalf("pc tag %q consulted the card store %d time(s) with no name: "+
				"the active card is shared by the whole group, not owned by the sender",
				args, calls)
		}
		if id != "" {
			t.Fatalf("pc tag %q bound card %q, want no card", args, id)
		}
		if reply != groupTagNeedsName {
			t.Fatalf("pc tag %q replied %q, want %q", args, reply, groupTagNeedsName)
		}
		// The notice must not point at a command the group refuses: that
		// would send the member into a second rejection.
		if strings.Contains(reply, "pc use") {
			t.Fatalf("pc tag notice points at a refused command: %q", reply)
		}
	}
}

// TestGroupTagWithNameResolvesIt is the complement: giving a name must reach
// the resolver, with surrounding space trimmed, so the refusal above cannot
// be satisfied by breaking `pc tag 张三`.
func TestGroupTagWithNameResolvesIt(t *testing.T) {
	var seen []string
	id, reply := groupTagTarget([]string{" 张三 "}, func(ref string) (string, error) {
		seen = append(seen, ref)
		return "c-zhangsan", nil
	})
	if id != "c-zhangsan" || reply != "" {
		t.Fatalf("groupTagTarget = %q,%q want \"c-zhangsan\",\"\"", id, reply)
	}
	if len(seen) != 1 || seen[0] != "张三" {
		t.Fatalf("resolver saw %q, want exactly [\"张三\"]", seen)
	}
}

// TestGroupTagResolveErrorIsReported keeps a resolution failure from being
// mistaken for success (id would be "" and the binding would silently skip).
func TestGroupTagResolveErrorIsReported(t *testing.T) {
	boom := errors.New("找不到角色：张三")
	id, reply := groupTagTarget([]string{"张三"}, func(ref string) (string, error) {
		return "", boom
	})
	if id != "" {
		t.Fatalf("id = %q on resolver error, want empty", id)
	}
	if reply != boom.Error() {
		t.Fatalf("reply = %q, want %q", reply, boom.Error())
	}
}

// TestGroupTagEmptyResolutionIsRefused covers the second route to the same
// hijack. GetCharacter("") substitutes the active card, so a resolver that
// returns an empty id with no error must not reach it: `pc tag` would bind
// the sender to the group's active card anyway, just through another door.
func TestGroupTagEmptyResolutionIsRefused(t *testing.T) {
	id, reply := groupTagTarget([]string{"张三"}, func(ref string) (string, error) {
		return "", nil
	})
	if id != "" {
		t.Fatalf("id = %q, want empty", id)
	}
	if reply != groupTagNeedsName {
		t.Fatalf("reply = %q, want %q", reply, groupTagNeedsName)
	}
}

// newQQCardFixture boots the same minimal app the chatcore tests use: a temp
// data root plus a table-scoped context, which card creation requires.
func newQQCardFixture(t *testing.T) (*app.App, context.Context) {
	t.Helper()
	cfg := config.Default()
	cfg.DataRoot = t.TempDir()
	if err := cfg.Resolve(""); err != nil {
		t.Fatal(err)
	}
	a, err := app.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	ctx := context.Background()
	tid, err := a.ActiveTableID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return a, storage.WithTable(ctx, tid)
}

// TestGroupTagWithoutNameBindsNothing is the end-to-end form of the rule
// above, and the one the reverse verification turns red. It drives
// runPCBinding itself, so re-inlining the old ActiveCharacterID fallback
// (bypassing groupTagTarget) cannot pass by leaving the helper intact — that
// bypass is exactly the blind spot the helper-only tests have.
//
// The setup is the attack, not a decoration: another member's card is active
// in the group-level state. `pc tag` with no name must not adopt it.
func TestGroupTagWithoutNameBindsNothing(t *testing.T) {
	a, ctx := newQQCardFixture(t)
	card, _, err := a.CreateCharacter(ctx, "", "冯丽", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	// ActiveID is group-wide: `pc use` wrote it before being refused, and
	// `pc active` still displays it to every member.
	if err := a.SetActiveCharacter(ctx, card.ID); err != nil {
		t.Fatal(err)
	}
	tid, err := a.ActiveTableID(ctx)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	st, err := loadGroupStore(root)
	if err != nil {
		t.Fatal(err)
	}
	tenant := &bot.Tenant{Kind: "group", ID: "g-test", Root: root, App: a}
	h := &botHandler{logger: log.New(io.Discard, "", 0)}

	var replies []string
	ev := messageEvent{Author: author{MemberOpenid: "member-b"}}
	h.runPCBinding(ctx, tenant, st, ev, "tag", nil, func(s string) { replies = append(replies, s) })

	if len(replies) != 1 || replies[0] != groupTagNeedsName {
		t.Fatalf("pc tag with no name replied %q, want exactly %q", replies, groupTagNeedsName)
	}
	if b, ok := st.binding("member-b", tid); ok {
		t.Fatalf("member-b was bound to %+v (card %q) without naming a card: "+
			"a stale group-wide ActiveID became a permanent binding", b, card.Name)
	}
}

// TestGroupTagWithNameBindsTheNamedCard is the counterweight: the refusal
// above must not be bought by breaking `pc tag 张三`, which is the only way a
// member can bind in a group.
func TestGroupTagWithNameBindsTheNamedCard(t *testing.T) {
	a, ctx := newQQCardFixture(t)
	card, _, err := a.CreateCharacter(ctx, "", "张三", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	tid, err := a.ActiveTableID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	st, err := loadGroupStore(root)
	if err != nil {
		t.Fatal(err)
	}
	tenant := &bot.Tenant{Kind: "group", ID: "g-test", Root: root, App: a}
	h := &botHandler{logger: log.New(io.Discard, "", 0)}

	var replies []string
	ev := messageEvent{Author: author{MemberOpenid: "member-a"}}
	h.runPCBinding(ctx, tenant, st, ev, "tag", []string{"张三"}, func(s string) { replies = append(replies, s) })

	if len(replies) != 1 || !strings.Contains(replies[0], "绑定角色「张三」") {
		t.Fatalf("pc tag 张三 replied %q, want a binding confirmation for 张三", replies)
	}
	b, ok := st.binding("member-a", tid)
	if !ok || b.CardID != card.ID {
		t.Fatalf("binding = %+v,%v want card %q", b, ok, card.ID)
	}
}
