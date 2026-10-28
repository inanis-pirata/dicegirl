package chatcore

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"dicegirl/internal/app"
	"dicegirl/internal/apperr"
	"dicegirl/internal/config"
	"dicegirl/internal/storage"
)

// The resolution layers that need storage: the name ladder lists the table's
// cards, and layer 3 has to resolve a target before it can reject it. The
// fixture is the minimal app bootstrap used by the app package's own tests.
func newTargetFixture(t *testing.T) (*app.App, context.Context) {
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

func createCard(t *testing.T, a *app.App, ctx context.Context, name string) *app.Target {
	t.Helper()
	c, _, err := a.CreateCharacter(ctx, "", name, nil, false)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return &app.Target{CardID: c.ID, Name: c.Name, Source: app.SourceExplicit}
}

// withMentions, withBotIdentities and withAliases build the execution context
// an adapter would hand to a command, so a test can exercise the platform
// layers without an adapter.
func withMentions(ctx context.Context, hints ...MentionHint) context.Context {
	o, _ := ctx.Value(execOptionsKey{}).(execOptions)
	o.mentions = hints
	return withExecOptions(ctx, o)
}

func withBotIdentities(ctx context.Context, ids ...string) context.Context {
	o, _ := ctx.Value(execOptionsKey{}).(execOptions)
	o.botIdentities = ids
	return withExecOptions(ctx, o)
}

func withAliases(ctx context.Context, lookup TargetAliasLookup) context.Context {
	o, _ := ctx.Value(execOptionsKey{}).(execOptions)
	o.aliasLookup = lookup
	return withExecOptions(ctx, o)
}

// targetReason extracts the classification of a failed resolution.
func targetReason(t *testing.T, err error) TargetReason {
	t.Helper()
	var te *TargetError
	if !errors.As(err, &te) {
		t.Fatalf("error is not a *TargetError: %v", err)
	}
	return te.Reason
}

// --- the name ladder -----------------------------------------------------

func TestResolveTargetRefLadder(t *testing.T) {
	a, ctx := newTargetFixture(t)
	zhang := createCard(t, a, ctx, "张三")
	alice := createCard(t, a, ctx, "Alice")

	cases := []struct {
		name string
		ref  string
		want app.Target
	}{
		{"exact id", zhang.CardID, app.Target{CardID: zhang.CardID, Name: "张三", Source: app.SourceExplicit}},
		{"exact name", "张三", app.Target{CardID: zhang.CardID, Name: "张三", Source: app.SourceExplicit}},
		{"case-insensitive name", "alice", app.Target{CardID: alice.CardID, Name: "Alice", Source: app.SourceExplicit}},
		{"empty means active card", "", app.Target{Source: app.SourceActive}},
		{"whitespace means active card", "   ", app.Target{Source: app.SourceActive}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveTargetRef(ctx, a, tc.ref)
			if err != nil {
				t.Fatalf("resolve %q: %v", tc.ref, err)
			}
			if got != tc.want {
				t.Fatalf("resolve %q = %+v want %+v", tc.ref, got, tc.want)
			}
		})
	}
}

func TestResolveTargetRefNotFound(t *testing.T) {
	a, ctx := newTargetFixture(t)
	createCard(t, a, ctx, "张三")

	_, err := resolveTargetRef(ctx, a, "李四")
	if err == nil {
		t.Fatal("unknown name must fail")
	}
	if got := targetReason(t, err); got != TargetNotFound {
		t.Fatalf("reason = %s, want %s", got, TargetNotFound)
	}
	if !apperr.IsCode(err, apperr.NotFound) {
		t.Fatalf("code = %s, want NOT_FOUND", apperr.CodeOf(err))
	}
	// The wording is the pre-T16 one, byte for byte.
	if got, want := err.Error(), `NOT_FOUND [cli.cardRef]: 找不到角色：李四（可用 pc list 查看）`; got != want {
		t.Fatalf("message = %q want %q", got, want)
	}
}

func TestResolveTargetRefAmbiguous(t *testing.T) {
	a, ctx := newTargetFixture(t)
	createCard(t, a, ctx, "张三")
	createCard(t, a, ctx, "张三")

	_, err := resolveTargetRef(ctx, a, "张三")
	if err == nil {
		t.Fatal("duplicate name must fail")
	}
	if got := targetReason(t, err); got != TargetAmbiguous {
		t.Fatalf("reason = %s, want %s", got, TargetAmbiguous)
	}
	if !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("code = %s, want INVALID_ARGUMENT", apperr.CodeOf(err))
	}
	if got, want := err.Error(), "INVALID_ARGUMENT [cli.cardRef]: 角色名 张三 对应多张卡，请改用 ID"; got != want {
		t.Fatalf("message = %q want %q", got, want)
	}
}

func TestCardRefDelegatesToLadder(t *testing.T) {
	a, ctx := newTargetFixture(t)
	zhang := createCard(t, a, ctx, "张三")

	id, err := cardRef(ctx, a, "张三")
	if err != nil || id != zhang.CardID {
		t.Fatalf("cardRef = %q,%v want %q", id, err, zhang.CardID)
	}
	if id, err := cardRef(ctx, a, ""); err != nil || id != "" {
		t.Fatalf("empty cardRef = %q,%v want empty", id, err)
	}
	if _, err := cardRef(ctx, a, "李四"); !apperr.IsCode(err, apperr.NotFound) {
		t.Fatalf("cardRef unknown code = %s", apperr.CodeOf(err))
	}
}

func TestCardRefIgnoresMentionHints(t *testing.T) {
	// C1: cardRef's callers are positional names (.pc show 张三, .pc tag 张三).
	// If it resolved through a mention hint first, a duplicate name would stop
	// being ambiguous and silently pick whoever was @-mentioned.
	a, ctx := newTargetFixture(t)
	createCard(t, a, ctx, "张三")
	createCard(t, a, ctx, "张三")
	alice := createCard(t, a, ctx, "Alice")

	hinted := withMentions(ctx, MentionHint{
		MemberOpenid: "openid-1", CardID: alice.CardID, CardName: "张三", Bound: true,
	})
	_, err := cardRef(hinted, a, "张三")
	if !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("hint hijacked cardRef: %v", err)
	}
	if got := targetReason(t, err); got != TargetAmbiguous {
		t.Fatalf("reason = %s, want %s", got, TargetAmbiguous)
	}
	// A reference that is already a card id still resolves; the hint adds
	// nothing and takes nothing away.
	if id, err := cardRef(hinted, a, alice.CardID); err != nil || id != alice.CardID {
		t.Fatalf("id ref = %q,%v", id, err)
	}
}

// --- layer 1: platform mentions ------------------------------------------

func TestResolveTargetPlaceholderUsesHints(t *testing.T) {
	a, base := newTargetFixture(t)
	alice := createCard(t, a, base, "Alice")
	ctx := withMentions(base, MentionHint{
		MemberOpenid: "openid-1", CardID: alice.CardID, CardName: "Alice", Bound: true,
	})

	for _, tok := range []string{"<@openid-1>", "<@!openid-1>"} {
		ts, rest, err := resolveTargetWith(ctx, a,
			cmdMeta{name: "ra", mentionMode: mentionModeStrip}, []string{tok, "心理学"}, nil)
		if err != nil {
			t.Fatalf("%s: %v", tok, err)
		}
		want := app.Target{CardID: alice.CardID, Name: "Alice",
			Source: app.SourceExplicit, Kind: app.KindPlatform}
		if ts.Primary != want {
			t.Fatalf("%s = %+v want %+v", tok, ts.Primary, want)
		}
		if len(rest) != 1 || rest[0] != "心理学" {
			t.Fatalf("%s: rest = %v", tok, rest)
		}
	}
}

func TestResolveTargetPlaceholderNotBound(t *testing.T) {
	// Layer 2 of the mention rule: a member without a card is an explicit
	// notBound error, never a silent fallback to the sender's own card.
	a, base := newTargetFixture(t)
	ctx := withMentions(base, MentionHint{MemberOpenid: "openid-secret", Bound: false})

	_, _, err := resolveTargetWith(ctx, a,
		cmdMeta{name: "ra", mentionMode: mentionModeStrip},
		[]string{"<@openid-secret>", "心理学"}, nil)
	if err == nil {
		t.Fatal("unbound mention must fail")
	}
	if got := targetReason(t, err); got != TargetNotBound {
		t.Fatalf("reason = %s, want %s", got, TargetNotBound)
	}
	if !apperr.IsCode(err, apperr.NotFound) {
		t.Fatalf("code = %s, want NOT_FOUND", apperr.CodeOf(err))
	}
	// The openid is the mention's only handle: it must never be printed.
	if strings.Contains(err.Error(), "openid-secret") {
		t.Fatalf("message leaks the openid: %q", err.Error())
	}
	if got, want := err.Error(), "NOT_FOUND [cli.target]: "+notBoundMessage; got != want {
		t.Fatalf("message = %q want %q", got, want)
	}
}

func TestResolveTargetPlaceholderWithoutItsOwnHintIsReported(t *testing.T) {
	// A placeholder can arrive with no matching hint even when other hints
	// exist (the adapter filtered one, the list was truncated, or the platform
	// spelled the id differently). It must be REPORTED, never dropped: dropping
	// it leaves ".ra 心理学 <@B>" running as ".ra 心理学" against the sender's
	// own card, with no hint at all — silent substitution, which is exactly
	// what layer 2 forbids.
	//
	// What it must NOT do: report "unsupported" (the command does take a
	// target), panic, or silently substitute the member whose hint did arrive.
	a, base := newTargetFixture(t)
	alice := createCard(t, a, base, "Alice")
	ctx := withMentions(base, MentionHint{
		MemberOpenid: "openid-A", CardID: alice.CardID, CardName: "Alice", Bound: true,
	})

	args := []string{"<@openid-B>", "心理学"}
	ts, out, err := resolveTargetWith(ctx, a,
		cmdMeta{name: "ra", mentionMode: mentionModeStrip}, args, nil)
	if err == nil {
		t.Fatalf("unmatched placeholder must fail, got target %+v", ts.Primary)
	}
	if got := targetReason(t, err); got != TargetNotBound {
		t.Fatalf("reason = %s, want %s", got, TargetNotBound)
	}
	if !errors.Is(err, errPlaceholderUnexplained) {
		t.Fatalf("error is not errPlaceholderUnexplained: %v", err)
	}
	if !apperr.IsCode(err, apperr.NotFound) {
		t.Fatalf("code = %s, want NOT_FOUND", apperr.CodeOf(err))
	}
	// Its own wording, not the "member has no card" one: the member may well
	// own a card, we were simply not told who they are.
	if err.Error() == "NOT_FOUND [cli.target]: "+notBoundMessage {
		t.Fatal("unexplained placeholder reused the notBound message")
	}
	if !strings.Contains(err.Error(), "无法识别") {
		t.Fatalf("message = %q", err.Error())
	}
	// The openid must never be echoed.
	if strings.Contains(err.Error(), "openid-B") {
		t.Fatalf("message leaks the openid: %q", err.Error())
	}
	// No silent path: the arguments come back untouched (the placeholder is
	// still in them), not as the stripped rest.
	if !reflect.DeepEqual(out, args) {
		t.Fatalf("returned args = %v want the untouched %v", out, args)
	}
	// No silent fallback: the resolved target set must be empty, not Alice.
	if len(ts.Targets) != 0 {
		t.Fatalf("silently picked another member's card: %+v", ts.Targets)
	}
}

func TestResolveTargetPlaceholderStaysTextWithoutHints(t *testing.T) {
	// No adapter supplies mentions yet, so placeholders stay ordinary text:
	// the command receives them unchanged, exactly as before T16b.
	a, ctx := newTargetFixture(t)
	ts, rest, err := resolveTargetWith(ctx, a,
		cmdMeta{name: "ra", mentionMode: mentionModeStrip},
		[]string{"<@unknown-openid>", "心理学"}, nil)
	if err != nil {
		t.Fatalf("a gated-off placeholder must not error: %v", err)
	}
	if len(ts.Targets) != 0 {
		t.Fatalf("gated-off placeholder became a target: %+v", ts.Targets)
	}
	if !reflect.DeepEqual(rest, []string{"<@unknown-openid>", "心理学"}) {
		t.Fatalf("gated-off placeholder was modified: %v", rest)
	}
}

func TestResolveTargetTextNameIgnoresHints(t *testing.T) {
	// A text "@名字" goes through the name ladder and never consults hints: the
	// text form is the fallback for platforms without real mentions.
	a, base := newTargetFixture(t)
	zhang := createCard(t, a, base, "张三")
	alice := createCard(t, a, base, "Alice")
	ctx := withMentions(base, MentionHint{
		MemberOpenid: "openid-1", CardID: alice.CardID, CardName: "张三", Bound: true,
	})

	ts, _, err := resolveTargetWith(ctx, a,
		cmdMeta{name: "ra", mentionMode: mentionModeStrip}, []string{"@张三", "心理学"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Primary.CardID != zhang.CardID || ts.Primary.Kind != app.KindMention {
		t.Fatalf("text mention = %+v want 张三 by name", ts.Primary)
	}
}

// --- layer 3 / 4: reject or strip ----------------------------------------

func TestResolveTargetNoneModeRejectsResolvedTarget(t *testing.T) {
	// Layer 3: a command that takes no target must say so, never swallow it
	// into free text (".st hp-5 @别人" silently editing the operator's own card
	// is the failure this replaces).
	a, ctx := newTargetFixture(t)
	createCard(t, a, ctx, "张三")

	for _, cmd := range []string{"r", "ak", "deck", "log"} {
		_, rest, err := ResolveTarget(ctx, a, cmd, []string{"@张三", "hp-1"})
		if err == nil {
			t.Fatalf("%s: a resolvable target must be rejected", cmd)
		}
		if got := targetReason(t, err); got != TargetUnsupported {
			t.Fatalf("%s: reason = %s, want %s", cmd, got, TargetUnsupported)
		}
		if !apperr.IsCode(err, apperr.InvalidArgument) {
			t.Fatalf("%s: code = %s", cmd, apperr.CodeOf(err))
		}
		if !strings.Contains(err.Error(), cmd+" 不支持指定目标") {
			t.Fatalf("%s: message = %q", cmd, err.Error())
		}
		// A rejected token is not stripped: the command never runs anyway, and
		// the arguments must stay as the user wrote them.
		if len(rest) != 2 {
			t.Fatalf("%s: rest = %v", cmd, rest)
		}
	}
}

func TestResolveTargetNoneModeHint(t *testing.T) {
	a, ctx := newTargetFixture(t)
	createCard(t, a, ctx, "张三")

	_, _, err := ResolveTarget(ctx, a, "r", []string{"1d6", "@张三"})
	if err == nil {
		t.Fatal("r must reject a target")
	}
	if !strings.Contains(err.Error(), commandMeta("r").noTargetHint) {
		t.Fatalf("message %q lacks the hint %q", err.Error(), commandMeta("r").noTargetHint)
	}

	// A command without a hint still produces a usable message.
	_, _, err = ResolveTarget(ctx, a, "tb", []string{"@张三"})
	if err == nil || !strings.Contains(err.Error(), "tb 不支持指定目标") {
		t.Fatalf("tb: %v", err)
	}
}

func TestResolveTargetNoneModeNeverLooksUpAName(t *testing.T) {
	// A command that takes no target must not resolve one at all: ".r 1d6
	// @小李" has no card in it, so "找不到角色：小李" answers a question the
	// command never asked. The answer must not depend on whether 小李 happens
	// to own a card.
	a, ctx := newTargetFixture(t)
	createCard(t, a, ctx, "小林")

	for _, args := range [][]string{
		{"1d6", "@小林"}, // a name that resolves
		{"1d6", "@小李"}, // a name that does not
		{"1d6", "@小林", "破门"},
	} {
		_, _, err := ResolveTarget(ctx, a, "r", args)
		if err == nil {
			t.Fatalf("%v: a target on a none command must be rejected", args)
		}
		if got := targetReason(t, err); got != TargetUnsupported {
			t.Fatalf("%v: reason = %s, want %s", args, got, TargetUnsupported)
		}
		if !apperr.IsCode(err, apperr.InvalidArgument) {
			t.Fatalf("%v: code = %s", args, apperr.CodeOf(err))
		}
		if got, want := err.Error(), "INVALID_ARGUMENT [cli.target]: r 不支持指定目标（代骰请用 .ra）"; got != want {
			t.Fatalf("%v: message = %q want %q", args, got, want)
		}
	}
}

func TestResolveTargetStripModeUnknownNameIsNotFound(t *testing.T) {
	// The mirror case, and the reason the early return above is scoped to
	// none: a command that DOES take a target must name the missing character.
	// ".st hp-5 @不存在的人" saying "不支持指定目标" would tell the user their
	// syntax is wrong when it is not.
	a, ctx := newTargetFixture(t)
	createCard(t, a, ctx, "张三")

	_, _, err := resolveTargetWith(ctx, a,
		cmdMeta{name: "ra", mentionMode: mentionModeStrip},
		[]string{"力量-5", "@李四"}, nil)
	if err == nil {
		t.Fatal("unknown name must fail")
	}
	if got := targetReason(t, err); got != TargetNotFound {
		t.Fatalf("reason = %s, want %s", got, TargetNotFound)
	}
	if !apperr.IsCode(err, apperr.NotFound) {
		t.Fatalf("code = %s, want NOT_FOUND", apperr.CodeOf(err))
	}
	if !strings.Contains(err.Error(), "找不到角色：李四") {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestResolveTargetNoneModeWithoutTargetIsUntouched(t *testing.T) {
	// No target token: nothing changes, no error, arguments byte-identical.
	a, ctx := newTargetFixture(t)
	for _, cmd := range []string{"r", "ak", "deck", "log", "unknown-command"} {
		args := []string{"1d6", "破门"}
		ts, rest, err := ResolveTarget(ctx, a, cmd, args)
		if err != nil {
			t.Fatalf("%s: %v", cmd, err)
		}
		if len(ts.Targets) != 0 || ts.Primary != (Target{}) {
			t.Fatalf("%s: produced targets: %+v", cmd, ts)
		}
		if len(rest) != len(args) || rest[0] != args[0] || rest[1] != args[1] {
			t.Fatalf("%s: args changed: %v", cmd, rest)
		}
	}
}

func TestResolveTargetStripMode(t *testing.T) {
	a, ctx := newTargetFixture(t)
	zhang := createCard(t, a, ctx, "张三")

	ts, rest, err := resolveTargetWith(ctx, a,
		cmdMeta{name: "st", mentionMode: mentionModeStrip},
		[]string{"力量-5", "@张三"}, nil)
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	if len(ts.Targets) != 1 || ts.Primary.CardID != zhang.CardID {
		t.Fatalf("targets = %+v", ts)
	}
	if ts.Primary.Kind != app.KindMention || ts.Primary.Source != app.SourceExplicit {
		t.Fatalf("target kind/source = %v/%v", ts.Primary.Kind, ts.Primary.Source)
	}
	if len(rest) != 1 || rest[0] != "力量-5" {
		t.Fatalf("rest = %v", rest)
	}
}

func TestResolveTargetStripAnyPosition(t *testing.T) {
	a, ctx := newTargetFixture(t)
	zhang := createCard(t, a, ctx, "张三")
	pol := cmdMeta{name: "st", mentionMode: mentionModeStrip}

	for _, args := range [][]string{
		{"@张三", "力量-5"},
		{"力量-5", "@张三"},
		{"力量-5", "@张三", "破门"},
	} {
		ts, rest, err := resolveTargetWith(ctx, a, pol, args, nil)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if ts.Primary.CardID != zhang.CardID {
			t.Fatalf("%v: target = %+v", args, ts.Primary)
		}
		for _, tok := range rest {
			if tok == "@张三" {
				t.Fatalf("%v: token not stripped (rest %v)", args, rest)
			}
		}
	}
}

func TestResolveTargetUnknownMentionIsAnError(t *testing.T) {
	// Layer 2 never falls back silently: a typo must not write the operator's
	// own card.
	a, ctx := newTargetFixture(t)
	createCard(t, a, ctx, "张三")

	_, rest, err := resolveTargetWith(ctx, a,
		cmdMeta{name: "st", mentionMode: mentionModeStrip},
		[]string{"力量-5", "@李四"}, nil)
	if err == nil {
		t.Fatal("unknown mention must fail")
	}
	if got := targetReason(t, err); got != TargetNotFound {
		t.Fatalf("reason = %s", got)
	}
	if len(rest) != 2 {
		t.Fatalf("failed resolution must return the original args: %v", rest)
	}
}

func TestResolveTargetRejectsMultipleTargets(t *testing.T) {
	a, ctx := newTargetFixture(t)
	createCard(t, a, ctx, "张三")
	createCard(t, a, ctx, "李四")

	pol := cmdMeta{name: "st", mentionMode: mentionModeStrip}
	_, _, err := resolveTargetWith(ctx, a, pol, []string{"@张三", "@李四"}, nil)
	if err == nil {
		t.Fatal("two targets must be rejected when the command is not multi")
	}
	if got := targetReason(t, err); got != TargetUnsupported {
		t.Fatalf("reason = %s", got)
	}
	if !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("code = %s", apperr.CodeOf(err))
	}

	pol.multi = true
	ts, _, err := resolveTargetWith(ctx, a, pol, []string{"@张三", "@李四"}, nil)
	if err != nil {
		t.Fatalf("multi: %v", err)
	}
	if len(ts.Targets) != 2 || ts.Primary.CardID != ts.Targets[0].CardID {
		t.Fatalf("multi targets = %+v", ts)
	}
}

// --- ladder rung 4: aliases (reserved) -----------------------------------

func TestResolveTargetAliasRung(t *testing.T) {
	a, base := newTargetFixture(t)
	zhang := createCard(t, a, base, "张三")
	wang := createCard(t, a, base, "王五")

	// No alias vocabulary is installed anywhere yet, so an alias is unknown.
	if _, err := resolveTargetRef(base, a, "小林"); !apperr.IsCode(err, apperr.NotFound) {
		t.Fatalf("uninstalled alias code = %s", apperr.CodeOf(err))
	}

	aliases := map[string]string{"小林": zhang.CardID, "王五": zhang.CardID}
	ctx := withAliases(base, func(_ context.Context, name string) (string, bool) {
		id, ok := aliases[name]
		return id, ok
	})

	got, err := resolveTargetRef(ctx, a, "小林")
	if err != nil {
		t.Fatal(err)
	}
	want := app.Target{CardID: zhang.CardID, Name: "张三", Source: app.SourceExplicit}
	if got != want {
		t.Fatalf("alias = %+v want %+v", got, want)
	}

	// A real card name still wins over someone else's alias for it.
	got, err = resolveTargetRef(ctx, a, "王五")
	if err != nil {
		t.Fatal(err)
	}
	if got.CardID != wang.CardID {
		t.Fatalf("alias outranked the name tiers: %+v", got)
	}
}

// --- the flag path and the two-input conflict ----------------------------

func TestFlagTargetRecordsSource(t *testing.T) {
	a, ctx := newTargetFixture(t)
	zhang := createCard(t, a, ctx, "张三")

	// A value the user wrote is explicit and flagged.
	got, err := flagTarget(ctx, a, "张三", true)
	if err != nil {
		t.Fatal(err)
	}
	if got.CardID != zhang.CardID || got.Source != app.SourceExplicit || got.Kind != app.KindFlag {
		t.Fatalf("explicit flag = %+v", got)
	}

	// A value the adapter filled in from the member binding is bound.
	got, err = flagTarget(ctx, a, zhang.CardID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != app.SourceBound || got.Kind != app.KindFlag {
		t.Fatalf("bound flag = %+v", got)
	}

	// Nothing named at all: the active-card fallback, with no id, so the app
	// layer keeps applying its own rule (T08 keys on SourceActive).
	got, err = flagTarget(ctx, a, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.CardID != "" || got.Source != app.SourceActive {
		t.Fatalf("fallback = %+v", got)
	}
}

func TestFlagTargetKeepsExecutionTarget(t *testing.T) {
	// An "@名字" the dispatcher already resolved reaches the flag path through
	// currentCharacter; it must keep Kind=mention/Source=explicit instead of
	// being re-labelled as a --character value.
	a, ctx := newTargetFixture(t)
	zhang := createCard(t, a, ctx, "张三")
	mentioned := app.WithTarget(ctx, app.Target{
		CardID: zhang.CardID, Name: "张三", Source: app.SourceExplicit, Kind: app.KindMention,
	})

	got, err := flagTarget(mentioned, a, zhang.CardID, false)
	if err != nil {
		t.Fatal(err)
	}
	want := app.Target{CardID: zhang.CardID, Name: "张三",
		Source: app.SourceExplicit, Kind: app.KindMention}
	if got != want {
		t.Fatalf("execution target was re-labelled: %+v", got)
	}
}

func TestFlagTargetConflictsWithMention(t *testing.T) {
	a, ctx := newTargetFixture(t)
	createCard(t, a, ctx, "张三")
	alice := createCard(t, a, ctx, "Alice")

	mentioned := app.WithTarget(ctx, app.Target{CardID: alice.CardID, Name: "Alice",
		Source: app.SourceExplicit, Kind: app.KindMention})

	// Two different cards named in one command is an error, not a precedence
	// rule the player has to memorize (C12).
	_, err := flagTarget(mentioned, a, "张三", true)
	if err == nil {
		t.Fatal("mention + a different --character must fail")
	}
	if got := targetReason(t, err); got != TargetConflict {
		t.Fatalf("reason = %s, want %s", got, TargetConflict)
	}
	if !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("code = %s, want INVALID_ARGUMENT", apperr.CodeOf(err))
	}
	if got := err.Error(); !strings.HasSuffix(got, conflictMessage) {
		t.Fatalf("message = %q", got)
	}

	// The same card twice is not a conflict.
	got, err := flagTarget(mentioned, a, "Alice", true)
	if err != nil {
		t.Fatalf("same card: %v", err)
	}
	if got.CardID != alice.CardID {
		t.Fatalf("same card resolved to %+v", got)
	}

	// An active-card fallback (no card named, no mention card) never conflicts.
	fallback := app.WithTarget(ctx, app.Target{Source: app.SourceActive})
	if _, err := flagTarget(fallback, a, "张三", true); err != nil {
		t.Fatalf("active fallback must not conflict: %v", err)
	}
}

func TestFlagTargetIgnoresMentionHints(t *testing.T) {
	// A "--character" value is a name the user typed; a platform mention in the
	// same message must not hijack it.
	a, base := newTargetFixture(t)
	zhang := createCard(t, a, base, "张三")
	alice := createCard(t, a, base, "Alice")
	ctx := withMentions(base, MentionHint{
		MemberOpenid: "openid-1", CardID: alice.CardID, CardName: "张三", Bound: false,
	})

	got, err := flagTarget(ctx, a, "张三", true)
	if err != nil {
		t.Fatalf("flag must not consult mentions: %v", err)
	}
	if got.CardID != zhang.CardID || got.Kind != app.KindFlag {
		t.Fatalf("flag = %+v", got)
	}
}

// --- the bot's own mention ------------------------------------------------

func TestResolveTargetDropsBotTokenAndKeepsParity(t *testing.T) {
	// The platform puts the bot's own <@openid> in every group message. It must
	// leave no token behind: an extra token flips the odd/even pairing that
	// splitSheetAssignments uses to tell "st 力量 60" from "st 力量=60".
	//
	// The message also mentions a member, so placeholders are enabled: that is
	// the only situation in which the bot's token is recognizable as one.
	a, base := newTargetFixture(t)
	createCard(t, a, base, "张三")
	bot := []string{"bot-openid"}
	ctx := withMentions(base, MentionHint{MemberOpenid: "openid-1", CardID: "c1", Bound: true})

	ts, rest, err := resolveTargetWith(ctx, a,
		cmdMeta{name: "ra", mentionMode: mentionModeStrip},
		[]string{"<@bot-openid>", "力量-5"}, bot)
	if err != nil {
		t.Fatalf("bot token must not error: %v", err)
	}
	if len(ts.Targets) != 0 {
		t.Fatalf("bot token became a target: %+v", ts.Targets)
	}
	if len(rest) != 1 || rest[0] != "力量-5" {
		t.Fatalf("bot token was not dropped: %v", rest)
	}
	// The pairing form survives unchanged.
	_, rest, err = resolveTargetWith(ctx, a,
		cmdMeta{name: "ra", mentionMode: mentionModeStrip},
		[]string{"<@bot-openid>", "力量", "60"}, bot)
	if err != nil || len(rest) != 2 || rest[0] != "力量" || rest[1] != "60" {
		t.Fatalf("pair form changed: rest=%v err=%v", rest, err)
	}
}

func TestResolveTargetBotTokenDoesNotReportNotBound(t *testing.T) {
	// Without the bot's identity the token would look like a member mention and
	// report notBound on every group command; with it, nothing is reported.
	a, ctx := newTargetFixture(t)
	ctx = withMentions(ctx, MentionHint{MemberOpenid: "openid-1", CardID: "c1", Bound: true})
	ctx = withBotIdentities(ctx, "bot-openid")
	if _, _, err := ResolveTarget(ctx, a, "r", []string{"<@bot-openid>", "1d6"}); err != nil {
		t.Fatalf("bot token must be silently dropped: %v", err)
	}
}
