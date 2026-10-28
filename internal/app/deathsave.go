package app

import (
	"context"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
)

// Death-save counters live in Character.Metadata under these keys. They are
// per-character state by design: the rulebook carries only the numbers
// (thresholds), never the running tally.
const (
	metaDSSuccesses = "ds_successes"
	metaDSFailures  = "ds_failures"
)

// DeathSaveOutcome is one `.ds` roll and the counter state after it.
type DeathSaveOutcome struct {
	Roll     int  // natural die value (book mechanic expression)
	DieMax   int  // the die's upper bound (message label: d20 / d100 …)
	Crit     bool // natural crit_value: restore 1 HP, counters cleared
	CritFail bool // natural critfail_value: two failures at once
	Success  bool // counted as one success
	Successes int
	Failures  int
	// Stabilised/Died fire once, when this roll reaches the book's caps.
	Stabilised bool
	Died       bool
	RestoredHP bool // the crit actually bumped an hp track
	Threshold  int  // echoed for rendering
	Needed     int  // successes_needed
	Allowed    int  // failures_allowed
}

// RunDeathSave rolls one death save for the character and persists the
// counters (and a crit's 1-HP restore) in the same updates. The parameters
// come from the rulebook's death_save block; a book without one answers the
// same "未声明绑定" error as `sc`.
func (a *App) RunDeathSave(ctx context.Context, systemID, charID string) (*DeathSaveOutcome, error) {
	const op = "app.RunDeathSave"
	book, c, err := a.skillContext(ctx, systemID, charID)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"没有当前角色（先 pc tag 或 pc use 选一张卡）")
	}
	ds := book.DeathSave()
	if ds == nil {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"当前规则书未声明 ds 绑定")
	}

	roll, dieMax, err := book.RollDeathSaveDie(ctx)
	if err != nil {
		return nil, err
	}
	successes := metaInt(c, metaDSSuccesses)
	failures := metaInt(c, metaDSFailures)

	out := &DeathSaveOutcome{
		Roll:      roll,
		DieMax:    dieMax,
		Threshold: ds.Threshold,
		Needed:    ds.SuccessesNeeded,
		Allowed:   ds.FailuresAllowed,
	}
	switch roll {
	case ds.CritValue:
		out.Crit = true
		successes, failures = 0, 0
		if r, ok := c.Resources["hp"]; ok {
			r.Current++
			if r.Max > 0 && r.Current > r.Max {
				r.Current = r.Max
			}
			if r.Current <= 0 {
				r.Current = 1
			}
			c.Resources["hp"] = r
			out.RestoredHP = true
		}
	case ds.CritFailValue:
		out.CritFail = true
		failures += 2
	default:
		if roll >= ds.Threshold {
			out.Success = true
			successes++
		} else {
			failures++
		}
	}
	if successes > ds.SuccessesNeeded {
		successes = ds.SuccessesNeeded
	}
	if failures > ds.FailuresAllowed {
		failures = ds.FailuresAllowed
	}
	out.Successes = successes
	out.Failures = failures
	out.Stabilised = successes >= ds.SuccessesNeeded
	out.Died = failures >= ds.FailuresAllowed

	// Persist the hp bump first (it mutates the loaded character), then the
	// counters through SetCharacterMeta's own load-update cycle.
	if out.RestoredHP {
		if err := a.chars.Update(ctx, c); err != nil {
			return nil, err
		}
	}
	if _, err := a.SetCharacterMeta(ctx, c.ID, metaDSSuccesses, strconv.Itoa(successes)); err != nil {
		return nil, err
	}
	if _, err := a.SetCharacterMeta(ctx, c.ID, metaDSFailures, strconv.Itoa(failures)); err != nil {
		return nil, err
	}
	return out, nil
}

// ResetDeathSave clears the counters (after stabilising, death, or a new
// downing). Unknown keys are simply deleted.
func (a *App) ResetDeathSave(ctx context.Context, systemID, charID string) error {
	_, c, err := a.skillContext(ctx, systemID, charID)
	if err != nil {
		return err
	}
	if c == nil {
		return apperr.New("app.ResetDeathSave", apperr.InvalidArgument,
			"没有当前角色（先 pc tag 或 pc use 选一张卡）")
	}
	if _, err := a.SetCharacterMeta(ctx, c.ID, metaDSSuccesses, ""); err != nil {
		return err
	}
	_, err = a.SetCharacterMeta(ctx, c.ID, metaDSFailures, "")
	return err
}

// metaInt reads a metadata counter; absent or malformed values are 0.
func metaInt(c *character.Character, key string) int {
	if c.Metadata == nil {
		return 0
	}
	v, err := strconv.Atoi(strings.TrimSpace(c.Metadata[key]))
	if err != nil {
		return 0
	}
	return v
}
