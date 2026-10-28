package chatcore

// Execute is the single entry point shared by the CLI binary and the
// chat-platform adapters: it maps one chat-style command line (the command
// token plus its argument tokens) to internal/app use-case calls and renders
// the result as text or JSON. The platform layer owns only transport
// (receiving a message, splitting it into tokens, sending the reply).

import (
	"context"
	"io"
	"strings"

	"dicegirl/internal/app"
	"dicegirl/internal/storage"
)

// Unrecognized is returned by Execute when cmd is neither a built-in
// command (with or without a punctuation prefix) nor a rulebook-declared
// generation alias. The caller decides what to do with such input.
const Unrecognized = -1

// StripCommandPrefix strips one optional leading alias punctuation mark, reporting
// whether the token carried one.
func StripCommandPrefix(token string) (name string, prefixed bool) { return stripCommandPrefix(token) }

// IsCommandName reports whether name is a built-in command.
func IsCommandName(name string) bool { return isCommandName(name) }

// BookCommand resolves a rulebook-declared character-generation alias
// (".coc", ".dnd", ...), including variant digits and the detailed flag.
func BookCommand(a *app.App, name string) (system, variant string, detailed bool, ok bool) {
	return bookCommand(a, name)
}

// Option tunes one Execute call without changing the shared command
// semantics; the CLI uses zero options.
type Option func(*execOptions)

type execOptions struct {
	// autoApply makes sanity checks (sc) write their change back to
	// the card as if --apply had been passed. Chat adapters default to it
	// (the platform convention); the CLI stays opt-in.
	autoApply bool
	// defaultCharacter is the card used by commands that do not name one
	// ("the current character"). Chat adapters set it per sender when the
	// platform binds cards to members; the CLI leaves it empty so the app's
	// active-card rule applies.
	defaultCharacter string
	// cardCreated, when set, fires after a command activates a newly created
	// card. Group adapters use it to bind the new card to the sender on the
	// current table.
	cardCreated func(id, name string)
	// actor identifies the sender for game-affairs commands (GM/player
	// rosters). The CLI leaves it empty and resolves to a local actor.
	actorID    string
	actorName  string
	actorAdmin bool
	// gameOver fires after `.game over` clears a table, so adapters can drop
	// their per-table state (member-to-card bindings).
	gameOver func(tableID string)
	// logFile fires when `.log end/get/export` has written an export file.
	// Chat adapters use it to upload the file to the platform; the CLI has no
	// hook and prints the path instead.
	logFile func(path string)
	// logPrefix is prepended to the command line recorded in a play log. The
	// QQ adapter uses it to mark hidden rolls.
	logPrefix string
	// target is the resolved "which card" of this execution. Commands read it
	// through currentCharacter; the app layer reads it from ctx (T08).
	target app.TargetSet
	// mentions are the platform @-mentions the adapter saw in the message.
	// The text form ("@名字") is only a fallback; adapters hand over ids.
	mentions []MentionHint
	// botIdentities are the bot's own openid and nickname. A token naming the
	// bot is dropped from the arguments (layer 1b), never passed through: group
	// platforms put the bot's own mention in every message.
	botIdentities []string
	// aliasLookup is the target ladder's fifth rung (a player-defined
	// alternate card name). Empty unless an adapter installs one.
	aliasLookup TargetAliasLookup
}

// WithAutoApply turns on implicit --apply for resource checks.
func WithAutoApply() Option {
	return func(o *execOptions) { o.autoApply = true }
}

// WithDefaultCharacter selects the card used when a command does not name
// one, overriding the app's active-card resolution for this execution.
func WithDefaultCharacter(id string) Option {
	return func(o *execOptions) { o.defaultCharacter = id }
}

// WithCardCreatedHook installs a callback fired once per newly activated
// card (generation with the default --active=true, or `pc new`).
func WithCardCreatedHook(hook func(id, name string)) Option {
	return func(o *execOptions) { o.cardCreated = hook }
}

// WithActor identifies the command sender for game-affairs commands. Group
// adapters pass the platform member id and its display label; admin is the
// platform's group-management flag (the CLI is always admin).
func WithActor(id, name string, admin bool) Option {
	return func(o *execOptions) {
		o.actorID, o.actorName, o.actorAdmin = id, name, admin
	}
}

// WithGameOverHook installs a callback fired after `.game over` clears a
// game table. Group adapters use it to drop per-table card bindings.
func WithGameOverHook(hook func(tableID string)) Option {
	return func(o *execOptions) { o.gameOver = hook }
}

// WithLogFileHook installs a callback fired when a log export has been
// written. Chat adapters upload the file to the platform; without a hook the
// command prints the local path.
func WithLogFileHook(hook func(path string)) Option {
	return func(o *execOptions) { o.logFile = hook }
}

// WithLogPrefix prefixes the command line recorded in an active play log.
// The QQ adapter marks hidden rolls with it.
func WithLogPrefix(prefix string) Option {
	return func(o *execOptions) { o.logPrefix = prefix }
}

// WithTarget selects the card this execution addresses, as if the message had
// named it. It delegates to WithTargets so the multi-target extension needs no
// second option.
func WithTarget(t app.Target) Option {
	return WithTargets([]app.Target{t})
}

// WithTargets selects the cards this execution addresses, in order.
func WithTargets(targets []app.Target) Option {
	return func(o *execOptions) {
		if len(targets) == 0 {
			o.target = app.TargetSet{}
			return
		}
		cp := make([]app.Target, len(targets))
		copy(cp, targets)
		o.target = app.TargetSet{Primary: cp[0], Targets: cp}
	}
}

// WithBotIdentities tells the resolver which mention tokens belong to the bot
// itself, so a text "@骰娘" does not reach a command as a stray token (layer
// 1b). Group adapters pass the bot's nickname; without it such a token is
// treated as a target reference and reported. Platform placeholders are gated
// separately (placeholdersEnabled).
func WithBotIdentities(ids []string) Option {
	return func(o *execOptions) {
		cp := make([]string, len(ids))
		copy(cp, ids)
		o.botIdentities = cp
	}
}

// WithMentions hands over the platform @-mentions of the message so the
// resolver can map a member to a card without relying on the text form.
func WithMentions(mentions []MentionHint) Option {
	return func(o *execOptions) {
		cp := make([]MentionHint, len(mentions))
		copy(cp, mentions)
		o.mentions = cp
	}
}

// WithTargetAliases installs the alias vocabulary target resolution uses as
// its fifth rung. No command writes aliases yet, so adapters only install it
// once alias storage exists; without it the rung is skipped entirely.
func WithTargetAliases(lookup TargetAliasLookup) Option {
	return func(o *execOptions) { o.aliasLookup = lookup }
}

// notifyGameOver fires the adapter's game-reset hook.
func notifyGameOver(ctx context.Context, tableID string) {
	if o, _ := ctx.Value(execOptionsKey{}).(execOptions); o.gameOver != nil {
		o.gameOver(tableID)
	}
}

// notifyLogFile hands an export to the adapter. The bool reports whether a
// hook was installed.
func notifyLogFile(ctx context.Context, path string) bool {
	o, _ := ctx.Value(execOptionsKey{}).(execOptions)
	if o.logFile == nil {
		return false
	}
	o.logFile(path)
	return true
}

// currentActor resolves the sender identity, defaulting to the local CLI
// user with administrative rights.
func currentActor(ctx context.Context) (id, name string, admin bool) {
	o, _ := ctx.Value(execOptionsKey{}).(execOptions)
	if o.actorID == "" {
		return "local", "本地用户", true
	}
	name = o.actorName
	if name == "" {
		name = o.actorID
	}
	return o.actorID, name, o.actorAdmin
}

type execOptionsKey struct{}

func withExecOptions(ctx context.Context, o execOptions) context.Context {
	return context.WithValue(ctx, execOptionsKey{}, o)
}

func autoApplyFromContext(ctx context.Context) bool {
	o, _ := ctx.Value(execOptionsKey{}).(execOptions)
	return o.autoApply
}

// currentCharacter is the card reference commands use when none is given:
// the resolved target of this execution, then the caller-supplied default,
// then "" for the app's active-card rule.
func currentCharacter(ctx context.Context) string {
	if t := app.TargetFrom(ctx); t.CardID != "" {
		return t.CardID
	}
	o, _ := ctx.Value(execOptionsKey{}).(execOptions)
	return o.defaultCharacter
}

// withTarget records the resolved target in both places that consume it: the
// execution options (currentCharacter) and the context value the app layer
// reads for permission checks (T08).
func withTarget(ctx context.Context, t app.Target) context.Context {
	o, _ := ctx.Value(execOptionsKey{}).(execOptions)
	o.target = app.TargetSet{Primary: t, Targets: []app.Target{t}}
	ctx = withExecOptions(ctx, o)
	return app.WithTargets(ctx, []app.Target{t})
}

// flagTarget resolves a "--character" value. explicit distinguishes a value
// the user wrote from one the adapter filled in from its member binding, so
// the permission layer can tell "named" from "bound".
//
// An empty reference is not a target the user named: it stays the fallback
// rung (SourceActive, no card id) so the app layer keeps applying its own
// active-card rule, exactly as before target resolution existed.
func flagTarget(ctx context.Context, a *app.App, ref string, explicit bool) (app.Target, error) {
	// A target already resolved for this execution — an "@名字" the dispatcher
	// stripped, or a target an adapter supplied — is the card the user named.
	// It reaches this function through the default-character path, so it must
	// keep its Kind/Source instead of being re-labelled as a flag.
	if prev := app.TargetFrom(ctx); prev.CardID != "" && prev.CardID == ref {
		return prev, nil
	}
	// A name typed as a flag is never a mention, so this uses the name ladder
	// only: a stray platform hint in the same message cannot hijack it.
	t, err := resolveTargetRef(ctx, a, ref)
	if err != nil {
		return app.Target{}, err
	}
	if t.CardID != "" {
		t.Kind = app.KindFlag
		if explicit {
			t.Source = app.SourceExplicit
		} else {
			t.Source = app.SourceBound
		}
	}
	// C12: the conflict check runs before any empty-reference path can return,
	// because "@A ... --character B" must never silently prefer the flag.
	if err := checkTargetConflict(ctx, t); err != nil {
		return app.Target{}, err
	}
	return t, nil
}

// notifyCardCreated fires the adapter's new-card hook (generation / pc new
// with the default activation), used by group adapters to auto-bind the new
// card to the sender on the current table.
func notifyCardCreated(ctx context.Context, id, name string) {
	if o, _ := ctx.Value(execOptionsKey{}).(execOptions); o.cardCreated != nil {
		o.cardCreated(id, name)
	}
}

// Execute runs one chat-style command against the assembled application and
// returns a process-style exit code (0 success, 2 usage error, 1 runtime
// error, Unrecognized when the command is unknown).
func Execute(ctx context.Context, a *app.App, stdin io.Reader, stdout, stderr io.Writer,
	jsonMode bool, cmd string, args []string, opts ...Option) int {
	var eo execOptions
	for _, opt := range opts {
		opt(&eo)
	}
	ctx = withExecOptions(ctx, eo)
	if len(eo.target.Targets) > 0 {
		ctx = app.WithTargets(ctx, eo.target.Targets)
	}
	// Every command runs against the scope's active table. The resolution
	// seeds the implicit default table on a fresh root, so app code can
	// assume the table exists.
	tid, err := a.ActiveTableID(ctx)
	if err != nil {
		return PrintError(stderr, err)
	}
	ctx = storage.WithTable(ctx, tid)
	stripped, prefixed := stripCommandPrefix(cmd)
	rawLine := strings.TrimSpace(strings.Join(append([]string{cmd}, args...), " "))
	// A command glued to its first argument (".r3d6", "ra心理学+2") is split
	// off; the glued piece becomes the first positional argument. Resolve it
	// before deciding whether to record so `.lognew` is treated like `.log`.
	name := stripped
	if split, glued, _, recognized := SplitCommandToken(cmd); recognized && glued != "" {
		name = split
		args = append([]string{glued}, args...)
	}
	r := &Renderer{Out: stdout, JSON: jsonMode}

	// Play logs record the command line and its rendered output. `.log`
	// itself is never recorded, and JSON output is meant for programs rather
	// than a human-readable session record.
	var recorded strings.Builder
	var record bool
	if !jsonMode && name != "log" {
		if active, err := a.LogRecording(ctx); err == nil && active {
			record = true
			r.Out = io.MultiWriter(stdout, &recorded)
		}
	}
	finish := func(code int) int {
		if record && code != Unrecognized {
			o, _ := ctx.Value(execOptionsKey{}).(execOptions)
			id, actor, _ := currentActor(ctx)
			_ = a.LogRecordCommand(ctx, id, actor, o.logPrefix+rawLine, recorded.String())
		}
		return code
	}

	// Rulebook-declared generation aliases match even without a punctuation
	// prefix; the built-in commands then match by name or by prefix.
	if system, variant, detailed, ok := bookCommand(a, stripped); ok {
		return finish(cmdGenerate(ctx, stripped, stripped+" [版本] [d] [数量]", system, variant, detailed,
			args, a, r, stderr))
	}
	if isCommandName(name) || prefixed {
		// Resolve the target before the command sees its arguments, so no
		// command can swallow a target token as free text (reason, label).
		targets, rest, terr := ResolveTarget(ctx, a, name, args)
		if terr != nil {
			return finish(PrintError(stderr, terr))
		}
		args = rest
		if len(targets.Targets) > 0 {
			eo.target = targets
			ctx = withExecOptions(ctx, eo)
			ctx = app.WithTargets(ctx, targets.Targets)
		}
		return finish(dispatchCommand(ctx, name, args, a, r, stdin, stderr))
	}
	return Unrecognized
}
