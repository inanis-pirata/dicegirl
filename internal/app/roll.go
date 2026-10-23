package app

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine"
)

// bareDieRE matches the Dice!Next roulette target: a single die, single
// trial, `dN` or `1dN`. Any modifier or arithmetic keeps the ordinary roll.
var bareDieRE = regexp.MustCompile(`(?i)^(?:1)?d(\d+)$`)

// Roll parses and rolls a generic dice expression. A macro name saved on
// the active character card is expanded first.
func (a *App) Roll(ctx context.Context, expr string) (*dice.RollResult, error) {
	resolved, err := a.resolveRollInput(ctx, expr)
	if err != nil {
		return nil, err
	}
	return a.roller.Roll(ctx, resolved)
}

// RollStatement parses and rolls a roll statement: an expression with an
// optional n# repeat prefix and an optional whole-expression comparison
// suffix (e.g. "3#1d20>=10"). One independent result is returned per trial.
// A macro name may replace the expression ("3#沙漠之鹰").
func (a *App) RollStatement(ctx context.Context, input string) (*dice.BatchResult, error) {
	resolved, err := a.resolveRollInput(ctx, input)
	if err != nil {
		return nil, err
	}
	if batch, ok, err := a.rollRouletteStatement(ctx, resolved); err != nil || ok {
		return batch, err
	}
	return a.roller.RollStatement(ctx, resolved)
}

// rollRouletteStatement takes one value from the current game's roulette
// instead of rolling a bare dN. It reports false (and no error) whenever the
// statement is not a single dN or no matching roulette is active.
func (a *App) rollRouletteStatement(ctx context.Context, input string) (*dice.BatchResult, bool, error) {
	st, err := dice.ParseStatement(input, dice.DefaultLimits())
	if err != nil || st.Repeat != 1 {
		return nil, false, nil
	}
	m := bareDieRE.FindStringSubmatch(strings.TrimSpace(st.Expr))
	if m == nil {
		return nil, false, nil
	}
	sides, err := strconv.Atoi(m[1])
	if err != nil || sides < 1 {
		return nil, false, nil
	}
	value, ok, err := a.drawRoulette(ctx, sides)
	if err != nil || !ok {
		return nil, false, err
	}
	node := &dice.DiceNode{
		Count: 1,
		Sides: sides,
		Rolls: []dice.DieRoll{{Value: value, Kept: true}},
		Value: value,
	}
	rr := &dice.RollResult{
		Expression: st.Expr,
		Root:       node,
		Total:      value,
		Groups:     []*dice.DiceNode{node},
	}
	if st.Compare != nil {
		rr.Compare = st.Compare.Judge(value)
	}
	return &dice.BatchResult{
		Expression: strings.TrimSpace(input),
		Repeat:     1,
		Rolls:      []*dice.RollResult{rr},
	}, true, nil
}

// MaxPoolDice bounds the number of dice one pool rolls.
const MaxPoolDice = 100

// ResolveCount resolves a dice-count expression: a plain integer is used
// as is, a dice expression is rolled once (the Dice! `.w 1d4+1` form).
func (a *App) ResolveCount(ctx context.Context, expr string) (int, error) {
	const op = "app.ResolveCount"
	expr = strings.TrimSpace(expr)
	if n, err := strconv.Atoi(expr); err == nil {
		if n < 1 || n > MaxPoolDice {
			return 0, apperr.New(op, apperr.InvalidArgument,
				"dice count must be between 1 and "+strconv.Itoa(MaxPoolDice))
		}
		return n, nil
	}
	roll, err := a.roller.Roll(ctx, expr)
	if err != nil {
		return 0, apperr.Wrap(op, apperr.InvalidArgument,
			"invalid dice count expression "+expr, err)
	}
	if roll.Total < 1 || roll.Total > MaxPoolDice {
		return 0, apperr.New(op, apperr.InvalidArgument,
			"dice count must be between 1 and "+strconv.Itoa(MaxPoolDice))
	}
	return roll.Total, nil
}

// ValidateSystemConfig runs the registered system's configuration checks.
func (a *App) ValidateSystemConfig(ctx context.Context, systemID string) error {
	sys, err := a.registry.MustGet(systemID)
	if err != nil {
		return err
	}
	return sys.ValidateConfig(ctx)
}

// ValidateDataPacks recompiles every loaded data package. Data packs are
// validated at load time as well; this re-runs the check on demand.
func (a *App) ValidateDataPacks(ctx context.Context) error {
	const op = "app.ValidateDataPacks"
	for _, p := range a.packs {
		if _, err := engine.ParseDataPack(p.Raw(), p.Source(), a.roller); err != nil {
			return apperr.Wrap(op, apperr.CodeOf(err),
				"invalid data pack "+p.Source(), err)
		}
	}
	return nil
}
