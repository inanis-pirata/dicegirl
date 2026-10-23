package bot

// RunLine is the platform-independent request path: resolve (or open) the
// scope's isolated App, serialize the scope, run the shared chatcore command
// grammar against it and hand the rendered text back to the transport.

import (
	"bytes"
	"context"
	"strings"

	"dicegirl/chatcore"
)

// RunOption tunes one RunLine execution (platform policy, not game rules).
type RunOption func(*runOptions)

type runOptions struct {
	jsonMode         bool
	autoApply        bool
	defaultCharacter string
	cardCreated      func(id, name string)
	actorID          string
	actorName        string
	actorAdmin       bool
	gameOver         func(tableID string)
	logFile          func(path string)
	logPrefix        string
}

// WithJSON requests structured JSON output instead of human-readable text.
func WithJSON() RunOption {
	return func(o *runOptions) { o.jsonMode = true }
}

// WithAutoApply makes sc write resource changes back implicitly (chat
// platform convention; the CLI requires an explicit --apply).
func WithAutoApply() RunOption {
	return func(o *runOptions) { o.autoApply = true }
}

// WithDefaultCharacter selects the card used by commands that do not name
// one. Chat adapters pass the sender's bound card here.
func WithDefaultCharacter(id string) RunOption {
	return func(o *runOptions) { o.defaultCharacter = id }
}

// WithCardCreatedHook installs a callback fired after a command activates a
// newly created card. Group adapters use it to auto-bind the new card to the
// sender on the current table.
func WithCardCreatedHook(hook func(id, name string)) RunOption {
	return func(o *runOptions) { o.cardCreated = hook }
}

// WithActor identifies the sender for game-affairs commands (GM/player
// rosters). Group adapters pass the platform member id, its display label and
// the platform's group-management flag.
func WithActor(id, name string, admin bool) RunOption {
	return func(o *runOptions) { o.actorID, o.actorName, o.actorAdmin = id, name, admin }
}

// WithGameOverHook installs a callback fired after `.game over` clears a
// table. Group adapters use it to drop per-table card bindings.
func WithGameOverHook(hook func(tableID string)) RunOption {
	return func(o *runOptions) { o.gameOver = hook }
}

// WithLogFileHook installs a callback fired when `.log end/get/export` writes
// an export file. Group adapters upload the file to the platform.
func WithLogFileHook(hook func(path string)) RunOption {
	return func(o *runOptions) { o.logFile = hook }
}

// WithLogPrefix prefixes the command line recorded in an active play log.
func WithLogPrefix(prefix string) RunOption {
	return func(o *runOptions) { o.logPrefix = prefix }
}

// Result is the rendered outcome of one inbound command line.
type Result struct {
	// Code is the chatcore exit code: 0 ok, 2 usage error, 1 runtime error,
	// chatcore.Unrecognized when the line is not a command at all.
	Code int
	Out  string
	Err  string
}

// RunLine resolves one chat message in a scope and, if it is a command,
// executes it. Non-command lines return a Result with Code set to
// chatcore.Unrecognized and empty output.
//
// The line is whitespace-tokenized the same way a shell would split CLI
// arguments; handlers that take free text (reasons) rejoin their trailing
// tokens, so quoted arguments are not needed.
func (m *Manager) RunLine(ctx context.Context, kind, id, line string, opts ...RunOption) (Result, error) {
	var ro runOptions
	for _, opt := range opts {
		opt(&ro)
	}
	tokens := strings.Fields(line)
	if len(tokens) == 0 {
		return Result{Code: chatcore.Unrecognized}, nil
	}
	t, err := m.Get(ctx, kind, id)
	if err != nil {
		return Result{}, err
	}

	m.markActive(t, 1)
	defer m.markActive(t, -1)

	execOpts := []chatcore.Option(nil)
	if ro.autoApply {
		execOpts = append(execOpts, chatcore.WithAutoApply())
	}
	if ro.defaultCharacter != "" {
		execOpts = append(execOpts, chatcore.WithDefaultCharacter(ro.defaultCharacter))
	}
	if ro.cardCreated != nil {
		execOpts = append(execOpts, chatcore.WithCardCreatedHook(ro.cardCreated))
	}
	if ro.actorID != "" {
		execOpts = append(execOpts, chatcore.WithActor(ro.actorID, ro.actorName, ro.actorAdmin))
	}
	if ro.gameOver != nil {
		execOpts = append(execOpts, chatcore.WithGameOverHook(ro.gameOver))
	}
	if ro.logFile != nil {
		execOpts = append(execOpts, chatcore.WithLogFileHook(ro.logFile))
	}
	if ro.logPrefix != "" {
		execOpts = append(execOpts, chatcore.WithLogPrefix(ro.logPrefix))
	}
	res := Result{}
	t.Lock(func() {
		var out, errb bytes.Buffer
		code := chatcore.Execute(ctx, t.App, strings.NewReader(""), &out, &errb,
			ro.jsonMode, tokens[0], tokens[1:], execOpts...)
		res = Result{Code: code, Out: out.String(), Err: errb.String()}
	})
	return res, nil
}

// markActive adjusts the scope's in-flight counter under the manager lock so
// eviction never closes a busy database.
func (m *Manager) markActive(t *Tenant, delta int) {
	m.mu.Lock()
	t.active += delta
	m.mu.Unlock()
}
