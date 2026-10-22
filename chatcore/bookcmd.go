package chatcore

import (
	"strings"

	"dicegirl/internal/app"
)

// bookCommand resolves a rulebook-declared short command alias.
//
// Every rulebook declares its own alias in the manifest (`commands`), so
// the CLI has no per-game command names: adding a game needs no Go change.
// The alias is the character-generation entry point of that book.
//
// The alias may carry a variant and the detailed flag: "coc6d" is the alias
// "coc" with variant 6 in detailed (point-buy) mode, "dndd" is "dnd" in
// detailed mode, "dnd" is the book's default variant.
func bookCommand(a *app.App, name string) (system, variant string, detailed bool, ok bool) {
	if a == nil || name == "" {
		return "", "", false, false
	}
	for _, m := range a.Registry().List() {
		_, v, det, matched := matchAlias(name, m.Commands)
		if !matched {
			continue
		}
		return m.ID, v, det, true
	}
	return "", "", false, false
}

// matchAlias splits a command token into the alias it uses, the variant
// digits and the detailed flag: "coc6d" is the alias "coc" with variant 6
// in detailed mode, "dndd" is "dnd" in detailed mode.
func matchAlias(name string, aliases []string) (alias, variant string, detailed bool, ok bool) {
	lower := strings.ToLower(name)
	for _, a := range aliases {
		if a == "" || !strings.HasPrefix(lower, a) {
			continue
		}
		rest := lower[len(a):]
		det := strings.HasSuffix(rest, "d")
		v := strings.TrimSuffix(rest, "d")
		if !isDigits(v) {
			continue
		}
		return a, v, det, true
	}
	return "", "", false, false
}

// isDigits reports whether s is empty or made of decimal digits.
func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
