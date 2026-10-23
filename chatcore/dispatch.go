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
// the caller-supplied default, or "" for the app's active-card rule.
func currentCharacter(ctx context.Context) string {
	o, _ := ctx.Value(execOptionsKey{}).(execOptions)
	return o.defaultCharacter
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
		return finish(dispatchCommand(ctx, name, args, a, r, stdin, stderr))
	}
	return Unrecognized
}
