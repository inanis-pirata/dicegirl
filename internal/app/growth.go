package app

import (
	"context"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine"
)

// GrowthOptions parameterizes a skill-growth check.
type GrowthOptions struct {
	SystemID    string
	CharacterID string
	// Name is the skill or characteristic to improve (id, label or alias).
	Name string
	// Target optionally overrides the value the d100 is compared against
	// (the sheet value by default).
	Target *int
	// Gain overrides the rulebook's growth amount (the Dice!
	// `.en 幸运 +1D3/1D10` form: the caller rolls its own amount).
	Gain string
	// Reason is UI-layer text echoed in the result.
	Reason string
}

// RunGrowth runs a growth check for a sheet value and, when the skill
// improved, writes the new value back to the card. The write goes through
// the same validation, backup and atomic-update path as every other change;
// growth is a stateful operation by nature, so it is applied immediately
// (unlike checks, which only report).
func (a *App) RunGrowth(ctx context.Context, o GrowthOptions) (*engine.GrowthResult, *character.Character, error) {
	const op = "app.RunGrowth"
	c, err := a.GetCharacter(ctx, o.CharacterID)
	if err != nil {
		return nil, nil, err
	}
	sys, err := a.registry.MustGet(c.SystemID)
	if err != nil {
		return nil, nil, err
	}
	book, ok := sys.(*engine.Book)
	if !ok {
		return nil, nil, apperr.New(op, apperr.RuleNotFound,
			"rule system "+c.SystemID+" does not support growth checks")
	}
	key, ok := book.GrowthKey(o.Name)
	if !ok {
		return nil, nil, apperr.New(op, apperr.InvalidArgument,
			"unknown skill or characteristic: "+o.Name)
	}
	gc, ok := book.GrowthCheck(key)
	if !ok {
		return nil, nil, apperr.New(op, apperr.InvalidArgument,
			"rulebook "+c.SystemID+" declares no growth check")
	}

	// Locate the value on the sheet: skills first, then characteristics.
	value, kind, canonical, found := sheetValue(c, key)
	if !found {
		return nil, nil, apperr.New(op, apperr.InvalidArgument,
			"character has no value for "+growthLabel(key))
	}

	// A caller-provided growth amount may list alternatives separated by
	// "/" (the Dice! `.en 幸运 +1D3/1D10` form): one of them is picked at
	// random.
	gain, err := a.resolveGainExpression(ctx, o.Gain)
	if err != nil {
		return nil, nil, err
	}

	res, err := book.RunGrowth(ctx, gc, engine.GrowthInput{
		Key:    key,
		Name:   book.GrowthName(key),
		Value:  value,
		Target: o.Target,
		Gain:   gain,
		Reason: o.Reason,
	})
	if err != nil {
		return nil, nil, err
	}
	res.Name = book.GrowthName(key)

	if !res.Grew {
		return res, c, nil
	}
	switch kind {
	case "skill":
		c.Skills[canonical] = res.After
	case "attribute":
		c.Attributes[canonical] = res.After
	}
	if err := c.Validate(); err != nil {
		return nil, nil, err
	}
	if err := a.validateBySystem(ctx, sys, c); err != nil {
		return nil, nil, err
	}
	if err := a.chars.Update(ctx, c); err != nil {
		return nil, nil, err
	}
	return res, c, nil
}

// resolveGainExpression resolves a caller-provided growth amount. The
// alternatives of a "/"-separated list (the Dice! `.en 幸运 +1D3/1D10`
// form) are picked at random; an expression that is not a list of valid
// dice expressions is kept as it is.
func (a *App) resolveGainExpression(ctx context.Context, expr string) (string, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", nil
	}
	parts := strings.Split(expr, "/")
	if len(parts) == 1 {
		return expr, nil
	}
	for _, p := range parts {
		if _, _, err := dice.Parse(strings.TrimSpace(p), dice.DefaultLimits()); err != nil {
			// Not a list of alternatives: keep the expression whole (it may
			// contain a division).
			return expr, nil
		}
	}
	idx, err := a.roller.Intn(ctx, len(parts))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(parts[idx]), nil
}

// sheetValue returns the sheet value addressed by a canonical growth key.
func sheetValue(c *character.Character, key string) (value int, kind, canonical string, found bool) {
	if strings.HasPrefix(key, "attr:") {
		id := strings.TrimPrefix(key, "attr:")
		v, ok := c.Attributes[id]
		return v, "attribute", id, ok
	}
	if strings.HasPrefix(key, "skill:") {
		id := strings.TrimPrefix(key, "skill:")
		v, ok := c.Skills[id]
		return v, "skill", id, ok
	}
	return 0, "", "", false
}

// growthLabel renders a canonical growth key for display.
func growthLabel(key string) string {
	if i := strings.IndexByte(key, ':'); i > 0 {
		return key[i+1:]
	}
	return key
}
