package chatcore

// Helpers shared by every chat-style command handler: flag parsing that
// allows flags after positional arguments, the signed-integer token rule,
// exit-code-aware error rendering and K=V assignment parsing. The CLI binary
// and the chat-platform adapters use the exact same code paths.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"dicegirl/internal/app"
	"dicegirl/internal/apperr"
)

// PrintError renders err to stderr in the standard human-readable form and
// returns the process exit code it implies (2 for usage errors, 1 otherwise).
func PrintError(stderr io.Writer, err error) int {
	if err == nil {
		return 0
	}
	code := apperr.CodeOf(err)
	var ae *apperr.Error
	if errors.As(err, &ae) && ae.Op != "" {
		fmt.Fprintf(stderr, "错误 [%s]：%s\n", code, ae.Message)
		if cause := errors.Unwrap(ae); cause != nil {
			fmt.Fprintf(stderr, "  原因：%v\n", cause)
		}
	} else {
		fmt.Fprintf(stderr, "错误 [%s]：%v\n", code, err)
	}
	switch code {
	case apperr.InvalidArgument, apperr.ParseError:
		return 2
	default:
		return 1
	}
}

// parseSub parses subcommand arguments where flags may appear before or after
// positional arguments. Flags (and their values) are moved ahead of a "--"
// separator so the standard flag package still collects positionals via
// fs.Arg/fs.NArg.
func parseSub(fs *flag.FlagSet, args []string) error {
	boolFlags := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) {
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			boolFlags[f.Name] = true
		}
	})
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		// A bare signed integer (ra's -1 penalty token, explicit negative
		// values) is positional, not a flag: no flag has a numeric name.
		// Values after a value-flag are consumed below before this check.
		if len(a) > 1 && strings.HasPrefix(a, "-") && !signedIntegerToken(a) {
			name := strings.TrimLeft(a, "-")
			flags = append(flags, a)
			if eq := strings.IndexByte(name, '='); eq < 0 && !boolFlags[name] && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		positional = append(positional, a)
	}
	reordered := flags
	if len(positional) > 0 {
		reordered = append(append(reordered, "--"), positional...)
	}
	return fs.Parse(reordered)
}

// signedIntegerToken reports whether s is a standalone +n/-n token.
func signedIntegerToken(s string) bool {
	if len(s) < 2 || (s[0] != '+' && s[0] != '-') {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

type multiFlag struct{ values []string }

func (m *multiFlag) String() string { return "" }
func (m *multiFlag) Set(v string) error {
	m.values = append(m.values, v)
	return nil
}

// splitSheetAssignments classifies `.st` tokens. Dice! allows the '=' to be
// omitted (`st 力量60`) and also accepts name/value pairs (`st 力量 60`).
// Signed suffixes stay adjustments (`st 力量-1`, `st 理智-1d6`), so the two
// forms cannot be mixed in one command.
func splitSheetAssignments(args []string) (sets []string, hasSet, hasAdjust bool) {
	// Pair form: NAME INTEGER, with no '=' or ':' anywhere.
	if len(args) >= 2 && len(args)%2 == 0 {
		paired := make([]string, 0, len(args)/2)
		ok := true
		for i := 0; i < len(args); i += 2 {
			key := strings.TrimSpace(args[i])
			val := strings.TrimSpace(args[i+1])
			if key == "" || val == "" || strings.ContainsAny(key, "=:+-*/()") || !isDigits(val) {
				ok = false
				break
			}
			paired = append(paired, key+"="+val)
		}
		if ok {
			return paired, true, false
		}
	}

	for _, raw := range args {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			continue
		}
		if strings.ContainsAny(tok, "=:") {
			sets = append(sets, tok)
			hasSet = true
			continue
		}
		if key, val, ok := splitGluedAssignment(tok); ok {
			sets = append(sets, key+"="+val)
			hasSet = true
			continue
		}
		hasAdjust = true
	}
	return sets, hasSet, hasAdjust
}

// splitGluedAssignment recognizes `KEYVALUE` such as `力量60` or `STR60`.
// It deliberately rejects signed values, dice notation (`-1d6`) and
// arithmetic so those keep their adjustment meaning.
func splitGluedAssignment(tok string) (key, val string, ok bool) {
	i := len(tok)
	for i > 0 && tok[i-1] >= '0' && tok[i-1] <= '9' {
		i--
	}
	if i == 0 || i == len(tok) {
		return "", "", false
	}
	prev := tok[i-1]
	if prev == '+' || prev == '-' || prev == 'd' || prev == 'D' {
		return "", "", false
	}
	key = strings.TrimSpace(tok[:i])
	if key == "" || strings.ContainsAny(key, "=:+-*/()") {
		return "", "", false
	}
	return key, tok[i:], true
}

func parseAssignments(pairs []string) ([]app.Assignment, error) {
	out := make([]app.Assignment, 0, len(pairs))
	for _, p := range pairs {
		as, err := app.ParseAssignment(p)
		if err != nil {
			return nil, err
		}
		out = append(out, as)
	}
	return out, nil
}

// ParseInt64 parses a decimal integer token (shared with the CLI's global
// flag handling).
func ParseInt64(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscan(s, &n)
	return n, err
}

// useDefaultCharacter fills an empty character flag with the execution's
// default card (set by a chat adapter from its member binding), so commands
// such as "ra 心理学" act on the sender's own card.
func useDefaultCharacter(ctx context.Context, id *string) {
	if *id == "" {
		*id = currentCharacter(ctx)
	}
}

// defaultRef returns ref, or the execution's default card when ref is
// empty (chat adapters bind a card per sender).
func defaultRef(ctx context.Context, ref string) string {
	if strings.TrimSpace(ref) == "" {
		return currentCharacter(ctx)
	}
	return ref
}

// optionalID returns fs.Arg(0) when exactly one positional argument is
// present, "" otherwise.
func optionalID(fs *flag.FlagSet) string {
	if fs.NArg() == 1 {
		return fs.Arg(0)
	}
	return ""
}
