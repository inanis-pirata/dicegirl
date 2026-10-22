package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine/formula"
)

// A Mechanic is a dice resolution family for checks: it decides which
// dice are rolled, how the bonus/penalty dice apply and how the rolled
// dice become a single value. Rulebooks declare one as data (`mechanic.roll`
// plus `meets`); the engine contains no game-specific logic.
//
// Success levels, difficulties, opposed checks and growth are all
// mechanic-agnostic: they only see the rolled value and the target.
type Mechanic interface {
	// ID is the mechanic name in the result JSON ("expression").
	ID() string
	// RollRange returns the range a rolled value can take.
	RollRange() (min, max int)
	// Meets reports whether a roll meets a target, using the rulebook's
	// `meets` formula.
	Meets(roll, target int) bool
	// Roll rolls one check, applying the bonus/penalty dice.
	Roll(ctx context.Context, roller Roller, bonus, penalty int) (*MechanicRoll, error)
	// Apply copies the mechanic-specific fields into the check result and
	// returns the explanation fragment.
	Apply(res *CheckResult, m *MechanicRoll) string
}

// MechanicDie is one die of a check roll.
type MechanicDie struct {
	Value int  `json:"value"`
	Kept  bool `json:"kept"`
}

// MechanicRoll is the generic outcome of one check roll.
type MechanicRoll struct {
	// Value is the roll value used for grading.
	Value int `json:"value"`
	// Dice are the dice that were rolled, in roll order.
	Dice []MechanicDie `json:"dice"`
	// KeptIndex is the index of the counted die.
	KeptIndex int `json:"kept_index"`
	// Effect is "plain", "bonus" or "penalty".
	Effect string `json:"effect"`
}

// RollDetail is the generic, mechanic-independent detail of a check roll.
type RollDetail struct {
	Mechanic  string        `json:"mechanic"`
	Value     int           `json:"value"`
	Dice      []MechanicDie `json:"dice"`
	KeptIndex int           `json:"kept_index"`
	Effect    string        `json:"effect"`
}

// compiledMechanic is a validated mechanic declaration.
type compiledMechanic struct {
	kind            Mechanic
	maxBonusPenalty int
}

// compileMechanic validates the rulebook's mechanic declaration. Only the
// data-declared expression form is supported:
//
//	mechanic: { roll: "1d20", meets: "roll >= target", ... }
//
// The former built-in families (`type: percentile` / `type: d20`) have been
// removed; a rulebook that declares `type` is a configuration error so it
// cannot silently fall back to another family.
func compileMechanic(op string, raw rawMechanic) (*compiledMechanic, error) {
	if raw.MaxBonusPenalty <= 0 {
		return nil, apperr.New(op, apperr.ConfigInvalid, "mechanic.max_bonus_penalty must be positive")
	}
	if raw.Type != "" {
		return nil, apperr.New(op, apperr.ConfigInvalid,
			"mechanic.type is no longer supported; declare roll + meets instead")
	}
	if raw.Roll == "" {
		return nil, apperr.New(op, apperr.ConfigInvalid,
			"mechanic needs a roll expression (and meets)")
	}
	kind, err := compileExprMechanic(op, raw)
	if err != nil {
		return nil, err
	}
	return &compiledMechanic{kind: kind, maxBonusPenalty: raw.MaxBonusPenalty}, nil
}

// compileExprMechanic compiles the generic expression family.
func compileExprMechanic(op string, raw rawMechanic) (Mechanic, error) {
	expr := strings.TrimSpace(raw.Roll)
	if expr == "" {
		return nil, apperr.New(op, apperr.ConfigInvalid, "mechanic.roll is empty")
	}
	if _, _, err := dice.Parse(expr, dice.DefaultLimits()); err != nil {
		return nil, apperr.Wrap(op, apperr.ConfigInvalid,
			"mechanic.roll: invalid dice expression "+expr, err)
	}
	lo, hi, err := dice.ExprBounds(expr, dice.DefaultLimits())
	if err != nil {
		return nil, apperr.Wrap(op, apperr.ConfigInvalid,
			"mechanic.roll: cannot bound dice expression "+expr, err)
	}
	if strings.TrimSpace(raw.Meets) == "" {
		return nil, apperr.New(op, apperr.ConfigInvalid,
			"mechanic.meets is required with mechanic.roll")
	}
	meets, err := compileBoolFormula(raw.Meets, "mechanic meets",
		map[string]bool{"roll": true, "target": true})
	if err != nil {
		return nil, err
	}
	bonusHighest, err := parseKeep(op, raw.BonusKeep, "highest", "mechanic.bonus_keep")
	if err != nil {
		return nil, err
	}
	penaltyHighest, err := parseKeep(op, raw.PenaltyKeep, "lowest", "mechanic.penalty_keep")
	if err != nil {
		return nil, err
	}
	label := strings.TrimSpace(raw.Label)
	if label == "" {
		label = expr
	}
	return &exprMechanic{
		source:         expr,
		meets:          meets,
		bonusHighest:   bonusHighest,
		penaltyHighest: penaltyHighest,
		label:          label,
		min:            lo,
		max:            hi,
	}, nil
}

// parseKeep validates one bonus/penalty keep direction against its default.
func parseKeep(op, src, fallback, where string) (bool, error) {
	v := strings.ToLower(strings.TrimSpace(src))
	if v == "" {
		v = fallback
	}
	switch v {
	case "highest":
		return true, nil
	case "lowest":
		return false, nil
	}
	return false, apperr.New(op, apperr.ConfigInvalid,
		where+" must be highest or lowest, got "+quoteOrEmpty(src))
}

// exprMechanic is the generic, data-declared family: a dice expression is
// rolled once per trial, bonus/penalty dice add trials and the declared keep
// direction picks the counted one, and the `meets` formula decides success
// against a target. It needs no Go code per game.
type exprMechanic struct {
	source string
	meets  *formula.Expr
	// bonusHighest/penaltyHighest are the declared keep directions.
	bonusHighest   bool
	penaltyHighest bool
	label          string
	min, max       int
}

// ID implements Mechanic. The family is named by how it resolves checks;
// `label` only affects the rendered detail.
func (exprMechanic) ID() string { return "expression" }

// RollRange implements Mechanic.
func (m *exprMechanic) RollRange() (int, int) { return m.min, m.max }

// Meets implements Mechanic through the declared boolean formula; a formula
// that fails to evaluate (impossible after compilation) counts as a miss.
func (m *exprMechanic) Meets(roll, target int) bool {
	ok, err := m.meets.EvalBool(formula.Env{"roll": roll, "target": target})
	return err == nil && ok
}

// Roll implements Mechanic: one independent roll of the expression per
// trial, then the declared keep direction selects the counted trial.
func (m *exprMechanic) Roll(ctx context.Context, roller Roller, bonus, penalty int) (*MechanicRoll, error) {
	net := bonus - penalty
	count := 1 + absInt(net)
	diceOut := make([]MechanicDie, 0, count)
	values := make([]int, 0, count)
	kept := 0
	for i := 0; i < count; i++ {
		roll, err := roller.Roll(ctx, m.source)
		if err != nil {
			return nil, err
		}
		values = append(values, roll.Total)
		diceOut = append(diceOut, MechanicDie{Value: roll.Total})
		if i == 0 {
			continue
		}
		switch {
		case net > 0 && m.bonusHighest && roll.Total > values[kept]:
			kept = i
		case net > 0 && !m.bonusHighest && roll.Total < values[kept]:
			kept = i
		case net < 0 && m.penaltyHighest && roll.Total > values[kept]:
			kept = i
		case net < 0 && !m.penaltyHighest && roll.Total < values[kept]:
			kept = i
		}
	}
	diceOut[kept].Kept = true
	return &MechanicRoll{
		Value:     values[kept],
		Dice:      diceOut,
		KeptIndex: kept,
		Effect:    effectOf(net),
	}, nil
}

// Apply implements Mechanic: it renders each trial and the counted one, so
// the result reads like the built-in d20 detail (e.g. "奖励骰 d20 【19】/5").
func (m *exprMechanic) Apply(_ *CheckResult, r *MechanicRoll) string {
	if len(r.Dice) == 0 {
		return ""
	}
	if len(r.Dice) == 1 {
		return fmt.Sprintf("（%s = %d）", m.label, r.Value)
	}
	parts := make([]string, len(r.Dice))
	for i, d := range r.Dice {
		s := strconv.Itoa(d.Value)
		if i == r.KeptIndex {
			s = "【" + s + "】"
		}
		parts[i] = s
	}
	keep := "取低"
	if r.Effect == "bonus" && m.bonusHighest || r.Effect == "penalty" && m.penaltyHighest {
		keep = "取高"
	}
	return fmt.Sprintf("（%s %s %s，%s）", effectLabel(r.Effect), m.label,
		strings.Join(parts, "/"), keep)
}

// effectOf names the bonus/penalty effect of a roll.
func effectOf(net int) string {
	switch {
	case net > 0:
		return "bonus"
	case net < 0:
		return "penalty"
	default:
		return "plain"
	}
}

// effectLabel renders the effect for the explanation.
func effectLabel(effect string) string {
	switch effect {
	case "bonus":
		return "奖励骰"
	case "penalty":
		return "惩罚骰"
	default:
		return ""
	}
}
