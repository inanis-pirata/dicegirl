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
}

// WithJSON requests structured JSON output instead of human-readable text.
func WithJSON() RunOption {
	return func(o *runOptions) { o.jsonMode = true }
}

// WithAutoApply makes sc/rc write resource changes back implicitly (chat
// platform convention; the CLI requires an explicit --apply).
func WithAutoApply() RunOption {
	return func(o *runOptions) { o.autoApply = true }
}

// WithDefaultCharacter selects the card used by commands that do not name
// one. Chat adapters pass the sender's bound card here.
func WithDefaultCharacter(id string) RunOption {
	return func(o *runOptions) { o.defaultCharacter = id }
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
