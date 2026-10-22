package app

import (
	"context"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine"
)

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
	return a.roller.RollStatement(ctx, resolved)
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
