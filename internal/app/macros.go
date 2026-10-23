package app

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
	"dicegirl/internal/dice"
)

// Macros are named dice expressions saved on a character card (the Dice!
// `.st &名=表达式` convention). They are resolved in the application layer:
// a roll first looks for a macro of that name, then falls back to the plain
// dice expression. The interface layer only knows the "&" prefix.

// MacroPrefix marks a macro name in the interface layer.
const MacroPrefix = "&"

// MaxMacroNameLength bounds a macro name.
const MaxMacroNameLength = 32

// MacroList is one saved macro.
type MacroList struct {
	Key  string `json:"key"`
	Expr string `json:"expr"`
	Name string `json:"name"`
}

// SetMacro saves a named dice expression on a sheet (the active sheet when
// id is empty). The expression is validated once, at save time.
func (a *App) SetMacro(ctx context.Context, id, name, expr string) (*character.Character, error) {
	const op = "app.SetMacro"
	name = strings.TrimSpace(name)
	expr = strings.TrimSpace(expr)
	if name == "" {
		return nil, apperr.New(op, apperr.InvalidArgument, "macro name must not be empty")
	}
	if len([]rune(name)) > MaxMacroNameLength {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"macro name must be at most "+strconv.Itoa(MaxMacroNameLength)+" characters")
	}
	if expr == "" {
		return nil, apperr.New(op, apperr.InvalidArgument, "macro expression must not be empty")
	}
	if _, _, err := dice.Parse(expr, dice.DefaultLimits()); err != nil {
		return nil, apperr.Wrap(op, apperr.InvalidArgument,
			"macro "+name+" needs a valid dice expression", err)
	}
	c, err := a.GetCharacter(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.Macros == nil {
		c.Macros = map[string]string{}
	}
	c.Macros[name] = expr
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := a.chars.Update(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// DeleteMacro removes a named macro from a sheet.
func (a *App) DeleteMacro(ctx context.Context, id, name string) (*character.Character, error) {
	const op = "app.DeleteMacro"
	c, err := a.GetCharacter(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, ok := c.Macros[name]; !ok {
		return nil, apperr.New(op, apperr.InvalidArgument, "unknown macro: "+name)
	}
	delete(c.Macros, name)
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := a.chars.Update(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Macros lists the macros of a sheet (the active sheet when id is empty).
func (a *App) Macros(ctx context.Context, id string) ([]MacroList, error) {
	c, err := a.GetCharacter(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]MacroList, 0, len(c.Macros))
	for name, expr := range c.Macros {
		out = append(out, MacroList{Key: name, Name: name, Expr: expr})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// resolveMacro returns the expression saved under a macro name, if any.
// The active sheet provides the macros.
func (a *App) resolveMacro(ctx context.Context, name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	if strings.HasPrefix(name, MacroPrefix) {
		name = strings.TrimPrefix(name, MacroPrefix)
	}
	active, err := a.state.ActiveID(ctx)
	if err != nil || active == "" {
		return "", false
	}
	c, err := a.chars.Get(ctx, active)
	if err != nil {
		return "", false
	}
	expr, ok := c.Macros[name]
	return expr, ok
}

// expandMacro replaces a macro name by its expression, keeping the
// statement layer's repeat prefix ("3#&名" style).
func (a *App) expandMacro(ctx context.Context, input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	prefix := ""
	rest := trimmed
	if i := strings.IndexByte(trimmed, '#'); i >= 0 {
		prefix = trimmed[:i+1]
		rest = strings.TrimSpace(trimmed[i+1:])
	}
	expr, ok := a.resolveMacro(ctx, rest)
	if !ok {
		return "", false
	}
	return prefix + expr, true
}

// resolveRollInput expands a macro name, or reports that the input looks
// like a macro name but no such macro exists. The dice language is ASCII
// only, so a non-ASCII input is always a name.
func (a *App) resolveRollInput(ctx context.Context, input string) (string, error) {
	if expanded, ok := a.expandMacro(ctx, input); ok {
		return expanded, nil
	}
	name := strings.TrimSpace(input)
	if i := strings.IndexByte(name, '#'); i >= 0 {
		name = strings.TrimSpace(name[i+1:])
	}
	if name != "" && hasNonASCII(name) {
		report := name
		if strings.HasPrefix(report, MacroPrefix) {
			report = strings.TrimPrefix(report, MacroPrefix)
		}
		return "", apperr.New("app.resolveRollInput", apperr.InvalidArgument,
			"unknown macro: "+report)
	}
	return input, nil
}

// hasNonASCII reports whether s contains a non-ASCII rune.
func hasNonASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return true
		}
	}
	return false
}
