package engine

import (
	"context"
	"fmt"

	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine/formula"
)

// DBRow is one damage-bonus lookup row.
type DBRow struct {
	Min   int    `json:"min"`
	Max   int    `json:"max"`
	Build int    `json:"build"`
	Bonus string `json:"bonus"`
}

// DamageInput describes one damage roll: a weapon/base expression combined
// with the attacker's damage bonus when a character is given.
type DamageInput struct {
	Expr      string
	Character *character.Character
}

// DamageResult is the structured damage outcome.
type DamageResult struct {
	Kind     string           `json:"kind"`
	SystemID string           `json:"system_id"`
	Expr     string           `json:"expr"`
	DBExpr   string           `json:"db_expr"`
	Build    int              `json:"build"`
	Total    int              `json:"total"`
	Roll     *dice.RollResult `json:"roll"`
}

// LookupDamageBonus resolves the table row for a precomputed lookup key.
func (b *Book) LookupDamageBonus(key int) (DBRow, error) {
	if b.dbTable == nil {
		return DBRow{}, apperr.New("engine.LookupDamageBonus", apperr.ConfigInvalid,
			"rulebook declares no damage_bonus table")
	}
	for _, row := range b.dbTable.rows {
		if key >= row.min && key <= row.max {
			return DBRow{Min: row.min, Max: row.max, Build: row.build, Bonus: row.bonus}, nil
		}
	}
	return DBRow{}, apperr.New("engine.LookupDamageBonus", apperr.InvalidArgument,
		fmt.Sprintf("damage-bonus key %d outside the table", key))
}

// DamageBonus resolves the damage-bonus row for a character. A missing
// character or a rulebook without a damage_bonus table yields a zero bonus.
func (b *Book) DamageBonus(c *character.Character) (DBRow, error) {
	if c == nil || b.dbTable == nil {
		return DBRow{Bonus: "0"}, nil
	}
	key, err := b.dbTable.key.EvalInt(formula.Env(c.Attributes))
	if err != nil {
		return DBRow{}, apperr.Wrap("engine.DamageBonus", apperr.InvalidArgument,
			"cannot compute damage-bonus key", err)
	}
	return b.LookupDamageBonus(key)
}

// Damage validates the base expression, resolves the attacker's damage
// bonus from the rulebook table (when a character is given), combines them
// and rolls through the generic dice engine.
func (b *Book) Damage(ctx context.Context, in DamageInput) (*DamageResult, error) {
	const op = "engine.Damage"
	if in.Expr == "" {
		return nil, apperr.New(op, apperr.InvalidArgument, "damage expression is required")
	}
	if _, _, err := dice.Parse(in.Expr, dice.DefaultLimits()); err != nil {
		return nil, apperr.Wrap(op, apperr.InvalidArgument, "invalid damage expression", err)
	}

	row, err := b.DamageBonus(in.Character)
	if err != nil {
		return nil, err
	}
	dbExpr := row.Bonus
	build := row.Build

	combined := fmt.Sprintf("%s+(%s)", in.Expr, dbExpr)
	roll, err := b.roller.Roll(ctx, combined)
	if err != nil {
		return nil, err
	}
	return &DamageResult{
		Kind:     "damage",
		SystemID: b.manifest.ID,
		Expr:     in.Expr,
		DBExpr:   dbExpr,
		Build:    build,
		Total:    roll.Total,
		Roll:     roll,
	}, nil
}
