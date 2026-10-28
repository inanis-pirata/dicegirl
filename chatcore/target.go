package chatcore

// Target resolution: one place decides which card a command addresses.
//
// The result type (Target / Source / Kind / TargetSet) lives in internal/app
// because the permission layer (T08) consumes it and must not import chatcore.
// This file owns the chat-side vocabulary: platform mention hints, the
// resolver's error classification and the name ladder that turns what a player
// typed into a card id.
//
// The four layers (docs/product/target-selection-technical.md §8.1):
//
//	0. "@" not at the start of the token ("a@b.com")  -> not scanned at all
//	1a. group-wide mention ("@所有人", "@{all}")      -> stays ordinary text
//	1b. the bot itself ("@骰娘")                      -> dropped entirely
//	2. otherwise resolve; failure is an error, never a silent fallback
//	   (a silent fallback would turn ".st hp-5 @别人" into a write to the
//	   operator's own card)
//	3. resolved, but the command does not take a target -> unsupported
//	4. resolved and the command takes a target -> strip the token, record Target
//
// Layers 1a and 1b are deliberately different. 1a keeps the token because a
// person typed it and it may be part of the reason text; 1b drops it because
// the platform inserts it itself. If a bot mention is left in the arguments it
// adds one token to the command, which flips the odd/even pairing
// splitSheetAssignments relies on — the symptom is "editing someone else's card
// reports a missing key".
//
// Platform placeholders ("<@openid>") are resolved only when the adapter hands
// over the message's mentions: placeholdersEnabled is true exactly then, and a
// placeholder is matched against those hints by openid. With no mentions on the
// context (every adapter today) a placeholder stays ordinary text byte for byte
// as before, and the text form "@名字" is the only live target syntax.

import (
	"context"
	"strings"

	"dicegirl/internal/app"
	"dicegirl/internal/apperr"
)

// Target / Source / Kind / TargetSet alias the shared definitions so chatcore
// code reads naturally without duplicating the vocabulary.
type (
	Target    = app.Target
	Source    = app.Source
	Kind      = app.Kind
	TargetSet = app.TargetSet
)

// MentionHint is one platform @-mention the adapter saw in a message. The
// platform hands out ids, not names, so the adapter cannot fill CardName
// from the mention alone; CardID is filled when the member's card is known.
//
// CardName is display data only: matching never uses it (see resolvePlaceholder).
type MentionHint struct {
	MemberOpenid string
	CardID       string
	CardName     string
	Bound        bool
}

// TargetAliasLookup is the optional fifth ladder rung: a player-defined
// alternate name for a card. No command writes aliases yet, so the rung stays
// inert unless a caller installs a lookup (WithTargetAliases); installing one
// changes nothing about the order of the other rungs.
type TargetAliasLookup func(ctx context.Context, name string) (cardID string, ok bool)

// notBoundMessage is the layer-2 wording for a mention of a member who has no
// card bound on this table. It deliberately does not echo the reference: the
// reference of a platform mention is an openid, which must never be printed.
const notBoundMessage = "找不到角色：@提及的成员尚未绑定角色卡（请对方先 pc tag <卡名>）"

// conflictMessage is fixed on purpose: neither side can be interpolated
// safely. The mention side may have no name at all (an unbound member), and
// the flag side's name is the card's stored name, not what the user typed.
const conflictMessage = "同时指定了两种目标（@提及 与 --character），请只用一种"

// placeholderUnexplainedMessage is the wording for a platform placeholder whose
// openid is in no mention hint of the message. It says "we could not identify
// this mention", not "that member has no card": the member may well own one, we
// just were not told who they are. Like the other target messages it never
// echoes the openid.
const placeholderUnexplainedMessage = "找不到角色：这条 @提及 无法识别（请重试，或让对方先 pc tag <卡名>）"

// errPlaceholderUnexplained is returned when a "<@openid>" placeholder matches
// no mention hint. It is an ordinary user-facing error: the caller returns it,
// never swallows it. Its Reason stays TargetNotBound (the five-value set is
// fixed) and the apperr code stays NOT_FOUND, so the exit code and the error
// category are unchanged — only the wording tells the two cases apart.
//
// The value is immutable and shared: tests match it with errors.Is (pointer
// identity), so mutating a field in place would change every reference at once.
// Carry context (an openid, say) with a constructor returning a new value.
var errPlaceholderUnexplained = &TargetError{Reason: TargetNotBound,
	Err: apperr.New("cli.target", apperr.NotFound, placeholderUnexplainedMessage)}

// TargetReason classifies why a target could not be resolved. Callers switch
// on these constants instead of matching message text.
type TargetReason string

// The reason values. They are part of the internal contract (documented in
// improvement-plan T16b); the apperr.Code stays the user-facing category.
const (
	TargetNotFound    TargetReason = "notFound"
	TargetNotBound    TargetReason = "notBound"
	TargetAmbiguous   TargetReason = "ambiguous"
	TargetUnsupported TargetReason = "unsupported"
	TargetConflict    TargetReason = "conflict"
)

// TargetError classifies a failed target resolution while keeping the
// underlying apperr code (and therefore the exit code) intact.
type TargetError struct {
	Reason TargetReason
	Err    error
}

func (e *TargetError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *TargetError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ResolveTarget looks up the command's target policy and applies it to args,
// returning the resolved targets and the arguments with the target tokens
// removed.
func ResolveTarget(ctx context.Context, a *app.App, cmd string, args []string) (TargetSet, []string, error) {
	return resolveTargetWith(ctx, a, commandMeta(cmd), args, botIdentitiesFromContext(ctx))
}

// resolveTargetWith is the core resolver: given an explicit policy it does the
// four-layer judgement and stripping, so tests can exercise the policy without
// a command table row. botIdentities holds the bot's own nickname/openid, which
// is dropped rather than degraded (layer 1b).
func resolveTargetWith(ctx context.Context, a *app.App, pol cmdMeta, args []string,
	botIdentities []string) (TargetSet, []string, error) {
	rest, tokens := splitTargetTokens(args, botIdentities, placeholdersEnabled(ctx))
	if len(tokens) == 0 {
		// No target token at all. rest still reflects layer 1b, so a dropped
		// bot mention never reaches the command.
		return TargetSet{}, rest, nil
	}
	// A command that takes no target never resolves one. Resolving first would
	// answer a question the command never asked (".r 1d6 @小李" would report
	// "no such character" although rolling never looks a card up) and would
	// make the same text produce two unrelated errors depending on whether the
	// mentioned member happens to own a card.
	if pol.mentionMode == mentionModeNone {
		return TargetSet{}, args, &TargetError{Reason: TargetUnsupported,
			Err: apperr.New("cli.target", apperr.InvalidArgument, unsupportedMessage(pol))}
	}
	targets := make([]Target, 0, len(tokens))
	for _, tk := range tokens {
		t, err := resolveTargetToken(ctx, a, tk)
		if err != nil {
			// Layer 2 errors win over layer 3, but only for a command that
			// takes a target: ".st hp-5 @不存在的人" must say "no such
			// character", not "unsupported". A command with mentionModeNone
			// never reaches this loop (see above), so it cannot answer a
			// question it never asked.
			return TargetSet{}, args, err
		}
		targets = append(targets, t)
	}
	switch pol.mentionMode {
	case mentionModeStrip:
		// Layer 4: the tokens are removed from rest, the targets are recorded.
	case mentionModeInline:
		// Reserved for the phase-2 opposed check. A target it cannot place is
		// still an error, never a guess.
		fallthrough
	default:
		return TargetSet{}, args, &TargetError{Reason: TargetUnsupported,
			Err: apperr.New("cli.target", apperr.InvalidArgument, unsupportedMessage(pol))}
	}
	ts := TargetSet{Primary: targets[0], Targets: targets}
	if err := ts.Validate(pol.multi, pol.name); err != nil {
		return TargetSet{}, args, &TargetError{Reason: TargetUnsupported, Err: err}
	}
	return ts, rest, nil
}

// unsupportedMessage renders "<cmd> 不支持指定目标" with the command's hint, if
// the table has one.
func unsupportedMessage(pol cmdMeta) string {
	name := pol.name
	if name == "" {
		name = "该命令"
	}
	msg := name + " 不支持指定目标"
	if pol.noTargetHint != "" {
		msg += "（" + pol.noTargetHint + "）"
	}
	return msg
}

// targetToken is one target token extracted from the arguments.
type targetToken struct {
	// ref is the card name, card id or member openid the token carried.
	ref string
	// platform reports that the token was a <@openid> placeholder, so ref is an
	// openid and must be matched against mention hints, not against card names.
	platform bool
}

// splitTargetTokens applies layers 0, 1a, 1b and the removal half of layer 4 in
// one pass: it returns the tokens the command still receives, in their original
// order, and the tokens that name a target. platform enables the "<@openid>"
// shape (see placeholdersEnabled).
func splitTargetTokens(args []string, botIdentities []string, platform bool) (rest []string, tokens []targetToken) {
	rest = make([]string, 0, len(args))
	for _, tok := range args {
		tk, ok := targetTokenOf(tok, platform)
		if !ok {
			rest = append(rest, tok)
			continue
		}
		if isGroupMention(tk.ref) {
			// Layer 1a: a person typed this; it may be reason text.
			rest = append(rest, tok)
			continue
		}
		if isBotIdentity(tk.ref, botIdentities) {
			// Layer 1b: dropped, not degraded — see the file header.
			continue
		}
		tokens = append(tokens, tk)
	}
	return rest, tokens
}

// placeholdersEnabled reports whether "<@openid>" tokens may be resolved.
//
// 🔴 Gated on the adapter actually supplying mentions, and no adapter does yet
// (cmd/qqbot has no mentions field), so this is false for every real execution
// and placeholders stay ordinary text exactly as before. The gate matters:
// without the hints, a placeholder can only be matched by guesswork — dropping
// it silently would make ".ra 心理学 <@张三>" quietly roll against the sender's
// own card (the G13 fallback), while reporting it would kill every group
// command that carries the bot's own mention. T16b-2 turns this on once the
// adapter supplies the hints.
func placeholdersEnabled(ctx context.Context) bool {
	return len(mentionsFromContext(ctx)) > 0
}

// targetTokenOf applies layer 0 and recognizes the shapes a target token can
// take: a text "@名字" and, when enabled, a platform placeholder "<@openid>".
func targetTokenOf(tok string, platform bool) (targetToken, bool) {
	if platform {
		if ref, ok := placeholderRef(tok); ok {
			return targetToken{ref: ref, platform: true}, true
		}
	}
	// Layer 0: a lone "@" and an "@" that is not the first character are
	// ordinary text. A doubled "@@" is dice-roll syntax (".r @@@" is a parse
	// error test), not an escaped mention, so it stays text too: a second '@'
	// can never start a card name that the leading '@' had already consumed.
	if len(tok) > 1 && tok[0] == '@' && tok[1] != '@' {
		return targetToken{ref: tok[1:]}, true
	}
	return targetToken{}, false
}

// placeholderRef extracts the openid from a "<@openid>" mention placeholder,
// tolerating the "<@!openid>" spelling some platforms use. Only a token that is
// nothing but one placeholder counts: "<@a><@b>" and "<@a>tail" are rejected,
// so a malformed message is never split in the wrong place.
func placeholderRef(tok string) (string, bool) {
	if len(tok) < 4 || !strings.HasPrefix(tok, "<@") || !strings.HasSuffix(tok, ">") {
		return "", false
	}
	id := strings.TrimPrefix(tok[2:len(tok)-1], "!")
	if id == "" || strings.ContainsAny(id, "<>@!") {
		return "", false
	}
	return id, true
}

// groupMentions are the platform-wide mentions that address a group, not a
// character. They stay in the arguments as ordinary text.
var groupMentions = map[string]bool{
	"{all}": true,
	"所有人":   true,
	"全体成员":  true,
}

// isGroupMention reports whether ref is a group-wide mention (layer 1a).
func isGroupMention(ref string) bool { return groupMentions[ref] }

// isBotIdentity reports whether ref names the bot itself (layer 1b).
func isBotIdentity(ref string, botIdentities []string) bool {
	if ref == "" {
		return false
	}
	for _, id := range botIdentities {
		if id != "" && ref == id {
			return true
		}
	}
	return false
}

// resolveTargetToken resolves one extracted token: a placeholder goes to the
// mention hints, a text reference goes to the name ladder.
func resolveTargetToken(ctx context.Context, a *app.App, tk targetToken) (Target, error) {
	if tk.platform {
		return resolvePlaceholder(ctx, tk.ref)
	}
	t, err := resolveTargetRef(ctx, a, tk.ref)
	if err != nil {
		return Target{}, err
	}
	t.Kind = app.KindMention
	return t, nil
}

// resolvePlaceholder resolves a "<@openid>" token. The platform gives ids, so
// the id must match a mention hint exactly; CardName never takes part in
// matching because a name match could silently pick one of two same-named
// cards, while an openid cannot lie.
//
// A placeholder can arrive without a matching hint even when other hints
// exist: the adapter may filter a mention, the platform may truncate the list,
// or it may spell the same id differently ("<@!id>" vs the bare id in the
// hints). That is a different problem from "the member has no card bound", so
// it gets its own message — but it is still reported, never dropped. Dropping
// it would leave ".ra 心理学 <@B>" running as plain ".ra 心理学", i.e. rolled
// against the sender's own card with no hint at all (G13).
//
// The one thing this must never do is fall back to another member's card: the
// hint that does not match is the only evidence available, and guessing from it
// is exactly the silent substitution this whole mechanism exists to stop.
//
// Whether this code runs at all: no adapter passes mentions today, so
// placeholdersEnabled is always false, placeholders stay ordinary text, and
// notBound is produced 0 times in real execution (CLI and QQ alike). What is
// described above is the behaviour once an adapter supplies hints, and the
// error paths are exercised by tests that construct the hints by hand. Knowing
// this matters before changing the wording: it is not a message players see
// today.
func resolvePlaceholder(ctx context.Context, openid string) (Target, error) {
	h, ok := hintForOpenid(mentionsFromContext(ctx), openid)
	if !ok {
		return Target{}, errPlaceholderUnexplained
	}
	if !h.Bound || h.CardID == "" {
		return Target{}, &TargetError{Reason: TargetNotBound,
			Err: apperr.New("cli.target", apperr.NotFound, notBoundMessage)}
	}
	return Target{CardID: h.CardID, Name: h.CardName,
		Source: app.SourceExplicit, Kind: app.KindPlatform}, nil
}

// hintForOpenid finds the hint whose member openid is exactly openid.
func hintForOpenid(hints []MentionHint, openid string) (MentionHint, bool) {
	if openid == "" {
		return MentionHint{}, false
	}
	for _, h := range hints {
		if h.MemberOpenid == openid {
			return h, true
		}
	}
	return MentionHint{}, false
}

// resolveTargetRef runs the name ladder on one reference:
//
//  1. exact card id
//  2. exact card name
//  3. case-insensitive card name
//  4. alias                      (TargetAliasLookup; reserved, inert until
//     a later task defines where aliases are stored)
//  5. empty reference            -> the active card (SourceActive, no id)
//
// It knows nothing about platform mentions: its callers are the "--character"
// flag and cardRef, and both carry a name the user typed. If it consulted the
// message's mentions, ".pc show 张三" in a group would stop reporting the
// ambiguity of two same-named cards and silently pick whoever was @-mentioned.
func resolveTargetRef(ctx context.Context, a *app.App, ref string) (Target, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		// C4: this early return is the single support of the "a command that
		// names no card touches no card" rule. Deleting it turns every bare
		// command into an explicit Target with CardID "", which no test would
		// catch today but which would mis-tier T08's Source-based permission
		// checks (active-vs-none) later. Do not remove it, and do not make it
		// query the store.
		return Target{Source: app.SourceActive}, nil
	}
	chars, err := a.ListCharacters(ctx)
	if err != nil {
		return Target{}, err
	}
	for _, c := range chars {
		if c.ID == ref {
			return Target{CardID: c.ID, Name: c.Name, Source: app.SourceExplicit}, nil
		}
	}
	var exact, folded []string
	names := make(map[string]string, len(chars))
	for _, c := range chars {
		names[c.ID] = c.Name
		switch {
		case c.Name == ref:
			exact = append(exact, c.ID)
		case strings.EqualFold(c.Name, ref):
			folded = append(folded, c.ID)
		}
	}
	for _, ids := range [][]string{exact, folded} {
		switch len(ids) {
		case 0:
			continue
		case 1:
			return Target{CardID: ids[0], Name: names[ids[0]], Source: app.SourceExplicit}, nil
		default:
			return Target{}, &TargetError{Reason: TargetAmbiguous,
				Err: apperr.New("cli.cardRef", apperr.InvalidArgument,
					"角色名 "+ref+" 对应多张卡，请改用 ID")}
		}
	}
	// The alias rung runs after the name tiers: a card named exactly like the
	// reference always wins over someone else's alias for it.
	if lookup := aliasLookupFromContext(ctx); lookup != nil {
		if id, ok := lookup(ctx, ref); ok {
			return Target{CardID: id, Name: names[id], Source: app.SourceExplicit}, nil
		}
	}
	return Target{}, &TargetError{Reason: TargetNotFound,
		Err: apperr.New("cli.cardRef", apperr.NotFound,
			"找不到角色："+ref+"（可用 pc list 查看）")}
}

// checkTargetConflict reports the "two ways of naming a card in one command"
// error. It runs when a command accepts both a mention (already resolved into
// ctx) and a "--character" value: a silent precedence rule would be
// unpredictable, so two different cards is always an error.
func checkTargetConflict(ctx context.Context, t Target) error {
	prev := app.TargetFrom(ctx)
	if prev.Source != app.SourceExplicit || prev.CardID == "" {
		return nil
	}
	if prev.Kind != app.KindMention && prev.Kind != app.KindPlatform {
		return nil
	}
	if t.CardID == "" || t.CardID == prev.CardID {
		return nil
	}
	return &TargetError{Reason: TargetConflict,
		Err: apperr.New("cli.target", apperr.InvalidArgument, conflictMessage)}
}

// mentionsFromContext returns the platform mentions of this execution.
func mentionsFromContext(ctx context.Context) []MentionHint {
	o, _ := ctx.Value(execOptionsKey{}).(execOptions)
	return o.mentions
}

// botIdentitiesFromContext returns the bot's own openid/nickname, which layer
// 1b drops from the arguments.
func botIdentitiesFromContext(ctx context.Context) []string {
	o, _ := ctx.Value(execOptionsKey{}).(execOptions)
	return o.botIdentities
}

// aliasLookupFromContext returns the installed alias vocabulary, or nil.
func aliasLookupFromContext(ctx context.Context) TargetAliasLookup {
	o, _ := ctx.Value(execOptionsKey{}).(execOptions)
	return o.aliasLookup
}
