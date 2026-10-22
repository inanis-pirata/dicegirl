package app

import (
	"context"
	"strconv"

	"dicegirl/internal/apperr"
)

// DefaultDieSides is the die size a bare `r` rolls when nothing else is
// configured (the Dice! default is a d100).
const DefaultDieSides = 100

// Bounds for the configurable default die size.
const (
	MinDefaultDieSides = 1
	MaxDefaultDieSides = 1000
)

// DefaultDieMetadataKey is the character-metadata key that carries a
// per-card default die size. It overrides the user-level setting.
const DefaultDieMetadataKey = "default_die_sides"

// DefaultDieSides returns the number of sides used by a bare `r` roll.
//
// Precedence: the active (or given) character's metadata, then the
// user-level setting stored in the local state, then the built-in default.
func (a *App) DefaultDieSides(ctx context.Context, characterID string) (int, error) {
	if characterID == "" {
		active, err := a.state.ActiveID(ctx)
		if err != nil {
			return 0, err
		}
		characterID = active
	}
	if characterID != "" {
		// A missing or unreadable card simply means "no per-card default".
		if c, err := a.chars.Get(ctx, characterID); err == nil {
			if raw, ok := c.Metadata[DefaultDieMetadataKey]; ok {
				if n, err := strconv.Atoi(raw); err == nil &&
					n >= MinDefaultDieSides && n <= MaxDefaultDieSides {
					return n, nil
				}
			}
		}
	}
	sides, err := a.state.DefaultDieSides(ctx)
	if err != nil {
		return 0, err
	}
	if sides >= MinDefaultDieSides && sides <= MaxDefaultDieSides {
		return sides, nil
	}
	return DefaultDieSides, nil
}

// SetDefaultDieSides stores the user-level default die size. Zero (or a
// negative value) restores the built-in default.
func (a *App) SetDefaultDieSides(ctx context.Context, sides int) error {
	if sides != 0 && (sides < MinDefaultDieSides || sides > MaxDefaultDieSides) {
		return apperr.New("app.SetDefaultDieSides", apperr.InvalidArgument,
			"default die size must be between 1 and 1000")
	}
	if sides < 0 {
		sides = 0
	}
	return a.state.SetDefaultDieSides(ctx, sides)
}
