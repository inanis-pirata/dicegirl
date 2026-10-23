package app

import (
	"context"
	"regexp"
	"strings"

	"dicegirl/internal/apperr"
)

// damageBonusTokenRE matches the standalone Dice! `db` token. It is kept
// separate from the dice grammar because `db` is character data, not a dice
// term: the roll command expands it before the generic parser sees it.
var damageBonusTokenRE = regexp.MustCompile(`(?i)\bdb\b`)

// ExpandDamageBonus replaces each standalone `db` in a dice expression with
// the current character's damage-bonus expression (for example `1d4`). The
// lookup is game data: a rulebook without a damage_bonus table expands to 0.
//
// The expression is returned unchanged when it contains no `db`, so ordinary
// rolls never need a character card.
func (a *App) ExpandDamageBonus(ctx context.Context, expr, characterID string) (string, error) {
	if !damageBonusTokenRE.MatchString(expr) {
		return expr, nil
	}
	const op = "app.ExpandDamageBonus"
	c, err := a.GetCharacter(ctx, characterID)
	if err != nil {
		return "", apperr.Wrap(op, apperr.InvalidArgument,
			"伤害加值 db 需要当前角色卡", err)
	}
	book, err := a.selectSystem(ctx, "", c)
	if err != nil {
		return "", err
	}
	row, err := book.DamageBonus(c)
	if err != nil {
		return "", err
	}
	return damageBonusTokenRE.ReplaceAllString(expr, "("+strings.TrimSpace(row.Bonus)+")"), nil
}
