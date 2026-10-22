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
// dice become a single value. Rulebooks pick a mechanic by name
// (`mechanic.type`); the engine contains no game-specific logic, and new
// families (percentile, d20, ...) are added by implementing this interface.
//
// The percentile mechanic (CoC-style d100) and the d20 mechanic are built
// in; they differ only in how they roll and assemble, everything
// downstream (success levels, difficulties, opposed checks, growth) is
// mechanic-agnostic.
type Mechanic interface {
	// ID is the mechanic name as declared in the rulebook.
	ID() string
	// RollRange returns the range a rolled value can take.
	RollRange() (min, max int)
	// Meets reports whether a roll meets a target in this family's terms:
	// the percentile family succeeds by rolling at or below the target, the
	// d20 family by reaching it.
	Meets(roll, target int) bool
	// Roll rolls one check, applying the bonus/penalty dice.
	Roll(ctx context.Context, roller Roller, bonus, penalty int) (*MechanicRoll, error)
	// Apply copies the mechanic-specific fields into the check result and
	// returns the explanation fragment (e.g. "（十位 40，个位 1）").
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

// compileMechanic validates the rulebook's mechanic declaration and picks
// the implementation. Unknown types are a configuration error: a rulebook
// may not silently fall back to another family.
//
// Two forms are accepted:
//
//	mechanic: { type: percentile|d20, ... }   built-in family
//	mechanic: { roll: "1d20", meets: "roll >= target", ... }   expression
//
// The expression form makes a mechanic pure data: the dice engine rolls the
// declared expression and the formula language decides success, so new
// simple families need no Go code. The percentile family stays built in
// because its 00/0=100 rule and shared-units bonus dice cannot be expressed
// as a plain dice expression.
func compileMechanic(op string, raw rawMechanic) (*compiledMechanic, error) {
	if raw.MaxBonusPenalty <= 0 {
		return nil, apperr.New(op, apperr.ConfigInvalid, "mechanic.max_bonus_penalty must be positive")
	}
	var kind Mechanic
	switch {
	case raw.Type != "" && raw.Roll != "":
		return nil, apperr.New(op, apperr.ConfigInvalid,
			"mechanic must declare either type or roll, not both")
	case raw.Type != "":
		if raw.Meets != "" || raw.BonusKeep != "" || raw.PenaltyKeep != "" || raw.Label != "" {
			return nil, apperr.New(op, apperr.ConfigInvalid,
				"mechanic "+raw.Type+" does not take meets/bonus_keep/penalty_keep/label")
		}
		switch raw.Type {
		case "percentile":
			kind = percentileMechanic{}
		case "d20":
			kind = d20Mechanic{}
		default:
			return nil, apperr.New(op, apperr.ConfigInvalid,
				"unknown mechanic type "+quoteOrEmpty(raw.Type)+
					" (supported: percentile, d20, or a roll expression)")
		}
	case raw.Roll != "":
		m, err := compileExprMechanic(op, raw)
		if err != nil {
			return nil, err
		}
		kind = m
	default:
		return nil, apperr.New(op, apperr.ConfigInvalid,
			"mechanic needs a type (percentile, d20) or a roll expression")
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

// percentileMechanic is the CoC-style d100 family: one units d10 plus one
// or more tens d10 (00 + 0 reads as 100). Bonus/penalty dice add tens
// candidates: a bonus keeps the smallest (most favourable), a penalty the
// largest.
type percentileMechanic struct{}

// ID implements Mechanic.
func (percentileMechanic) ID() string { return "percentile" }

// RollRange implements Mechanic.
func (percentileMechanic) RollRange() (int, int) { return 1, 100 }

// Meets implements Mechanic: the percentile family succeeds by rolling at
// or below the target.
func (percentileMechanic) Meets(roll, target int) bool { return roll <= target }

// Roll implements Mechanic.
func (percentileMechanic) Roll(ctx context.Context, roller Roller, bonus, penalty int) (*MechanicRoll, error) {
	net := bonus - penalty
	tensCount := 1 + absInt(net)
	// One group: tens dice first, then the units die as the last die.
	roll, err := roller.Roll(ctx, fmt.Sprintf("%dd10", tensCount+1))
	if err != nil {
		return nil, err
	}
	group := roll.Groups[0]

	dice := make([]MechanicDie, 0, tensCount+1)
	tens := make([]int, 0, tensCount)
	candidates := make([]int, 0, tensCount)
	for i := 0; i < tensCount; i++ {
		face := group.Rolls[i].Value
		dice = append(dice, MechanicDie{Value: face})
		tens = append(tens, face)
		candidates = append(candidates, (face-1)*10)
	}
	units := group.Rolls[tensCount].Value
	if units == 10 {
		units = 0
	}
	dice = append(dice, MechanicDie{Value: units})

	chosenIdx := 0
	chosen := candidates[0]
	for i, v := range candidates[1:] {
		if net > 0 && v < chosen { // bonus: favourable = lower
			chosen = v
			chosenIdx = i + 1
		}
		if net < 0 && v > chosen { // penalty: worse = higher
			chosen = v
			chosenIdx = i + 1
		}
	}
	dice[chosenIdx].Kept = true
	dice[len(dice)-1].Kept = true

	value := chosen + units
	if chosen == 0 && units == 0 {
		value = 100 // 00/0
	}
	return &MechanicRoll{
		Value:     value,
		Dice:      dice,
		KeptIndex: chosenIdx,
		Effect:    effectOf(net),
	}, nil
}

// Apply implements Mechanic: it fills the legacy percentile fields of the
// result (kept for backward compatibility) and renders the explanation.
func (percentileMechanic) Apply(res *CheckResult, m *MechanicRoll) string {
	if len(m.Dice) == 0 {
		return ""
	}
	tensCount := len(m.Dice) - 1
	units := m.Dice[tensCount].Value
	res.Units = units
	res.TensRolls = make([]int, 0, tensCount)
	res.TensCandidates = make([]int, 0, tensCount)
	for i := 0; i < tensCount; i++ {
		res.TensRolls = append(res.TensRolls, m.Dice[i].Value)
		res.TensCandidates = append(res.TensCandidates, (m.Dice[i].Value-1)*10)
	}
	res.ChosenTensIndex = m.KeptIndex
	if m.KeptIndex >= 0 && m.KeptIndex < tensCount {
		res.ChosenTens = res.TensCandidates[m.KeptIndex]
	}
	if len(res.TensCandidates) > 1 {
		parts := make([]string, len(res.TensCandidates))
		for i, v := range res.TensCandidates {
			s := fmt.Sprintf("%02d", v)
			if i == res.ChosenTensIndex {
				s = "【" + s + "】"
			}
			parts[i] = s
		}
		return fmt.Sprintf("（%s 十位候选 %s，个位 %d）", effectLabel(m.Effect),
			strings.Join(parts, "/"), units)
	}
	return fmt.Sprintf("（十位 %02d，个位 %d）", res.ChosenTens, units)
}

// d20Mechanic is the d20 family: one d20, or more dice when bonus or
// penalty dice apply (a bonus keeps the highest, a penalty the lowest —
// the "advantage"/"disadvantage" convention).
type d20Mechanic struct{}

// ID implements Mechanic.
func (d20Mechanic) ID() string { return "d20" }

// RollRange implements Mechanic.
func (d20Mechanic) RollRange() (int, int) { return 1, 20 }

// Meets implements Mechanic: the d20 family succeeds by reaching the
// target.
func (d20Mechanic) Meets(roll, target int) bool { return roll >= target }

// Roll implements Mechanic.
func (d20Mechanic) Roll(ctx context.Context, roller Roller, bonus, penalty int) (*MechanicRoll, error) {
	net := bonus - penalty
	count := 1 + absInt(net)
	roll, err := roller.Roll(ctx, fmt.Sprintf("%dd20", count))
	if err != nil {
		return nil, err
	}
	group := roll.Groups[0]
	dice := make([]MechanicDie, 0, count)
	kept := 0
	for i, dr := range group.Rolls {
		dice = append(dice, MechanicDie{Value: dr.Value})
		if net > 0 && dr.Value > dice[kept].Value {
			kept = i
		}
		if net < 0 && dr.Value < dice[kept].Value {
			kept = i
		}
	}
	dice[kept].Kept = true
	return &MechanicRoll{
		Value:     dice[kept].Value,
		Dice:      dice,
		KeptIndex: kept,
		Effect:    effectOf(net),
	}, nil
}

// Apply implements Mechanic: the d20 family has no legacy fields, it only
// renders the explanation.
func (d20Mechanic) Apply(_ *CheckResult, m *MechanicRoll) string {
	if len(m.Dice) == 0 {
		return ""
	}
	if len(m.Dice) == 1 {
		return fmt.Sprintf("（d20 = %d）", m.Value)
	}
	parts := make([]string, len(m.Dice))
	for i, d := range m.Dice {
		s := strconv.Itoa(d.Value)
		if i == m.KeptIndex {
			s = "【" + s + "】"
		}
		parts[i] = s
	}
	keep := "取高"
	if m.Effect == "penalty" {
		keep = "取低"
	}
	return fmt.Sprintf("（%s d20 %s，%s）", effectLabel(m.Effect),
		strings.Join(parts, "/"), keep)
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
