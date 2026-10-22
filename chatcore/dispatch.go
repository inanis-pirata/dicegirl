package chatcore

// Execute is the single entry point shared by the CLI binary and the
// chat-platform adapters: it maps one chat-style command line (the command
// token plus its argument tokens) to internal/app use-case calls and renders
// the result as text or JSON. The platform layer owns only transport
// (receiving a message, splitting it into tokens, sending the reply).

import (
	"context"
	"io"

	"dicegirl/internal/app"
)

// Unrecognized is returned by Execute when cmd is neither a built-in short
// command (with or without a punctuation prefix) nor a rulebook-declared
// generation alias. The caller decides what to do with such input.
const Unrecognized = -1

// ShortToken strips one optional leading alias punctuation mark, reporting
// whether the token carried one.
func ShortToken(token string) (name string, prefixed bool) { return shortToken(token) }

// IsShortCommand reports whether name is a built-in short command.
func IsShortCommand(name string) bool { return isShortCommand(name) }

// BookCommand resolves a rulebook-declared character-generation alias
// (".coc", ".dnd", ...), including variant digits and the detailed flag.
func BookCommand(a *app.App, name string) (system, variant string, detailed bool, ok bool) {
	return bookCommand(a, name)
}

// Option tunes one Execute call without changing the shared command
// semantics; the CLI uses zero options.
type Option func(*execOptions)

type execOptions struct {
	// autoApply makes resource checks (sc/rc) write their change back to
	// the card as if --apply had been passed. Chat adapters default to it
	// (the platform convention); the CLI stays opt-in.
	autoApply bool
	// defaultCharacter is the card used by commands that do not name one
	// ("the current character"). Chat adapters set it per sender when the
	// platform binds cards to members; the CLI leaves it empty so the app's
	// active-card rule applies.
	defaultCharacter string
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
	name, prefixed := shortToken(cmd)
	r := &Renderer{Out: stdout, JSON: jsonMode}

	// Rulebook-declared generation aliases match even without a punctuation
	// prefix; the built-in short commands then match by name or by prefix.
	if system, variant, detailed, ok := bookCommand(a, name); ok {
		return shortGenerate(ctx, name, name+" [版本] [d] [数量]", system, variant, detailed,
			args, a, r, stderr)
	}
	if isShortCommand(name) || prefixed {
		return dispatchShort(ctx, name, args, a, r, stdin, stderr)
	}
	return Unrecognized
}
