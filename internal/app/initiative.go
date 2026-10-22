package app

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/dice"
	"dicegirl/internal/storage"
)

// Initiative tooling (the Dice! `.ri` / `.init` commands): roll initiative
// values, keep them in the current encounter and list them from high to
// low. The manual marks it "group chat only", but the ordering tool itself
// is plain run-time bookkeeping, so the CLI keeps one local encounter.

// DefaultInitiativeExpr is the default initiative roll (a d20, as used for
// D&D initiative).
const DefaultInitiativeExpr = "1d20"

// InitiativeOptions parameterizes one initiative roll.
type InitiativeOptions struct {
	// Modifier is added to the rolled value ("+5", "-1").
	Modifier int
	// Value is a fixed initiative value (".ri 80 怪物甲").
	Value *int
	// Expr is a custom dice expression ("2d20kh1").
	Expr string
	// Name is the entry's name.
	Name string
}

// InitiativeEntry is one entry of the initiative list.
type InitiativeEntry struct {
	Name  string           `json:"name"`
	Value int              `json:"value"`
	Roll  *dice.RollResult `json:"roll,omitempty"`
}

// InitiativeList is the ordered initiative list.
type InitiativeList struct {
	Kind    string            `json:"kind"`
	Entries []InitiativeEntry `json:"entries"`
}

// RollInitiative rolls an initiative value and adds it to the current
// encounter. Ties keep the order in which the entries were added. A fixed
// value (".ri 80 怪物甲") is used as is, without rolling.
func (a *App) RollInitiative(ctx context.Context, o InitiativeOptions) (*InitiativeList, error) {
	const op = "app.RollInitiative"
	var roll *dice.RollResult
	value := 0
	if o.Value != nil {
		value = *o.Value + o.Modifier
	} else {
		expr := strings.TrimSpace(o.Expr)
		if expr == "" {
			expr = DefaultInitiativeExpr
		}
		r, err := a.roller.Roll(ctx, expr)
		if err != nil {
			return nil, apperr.Wrap(op, apperr.InvalidArgument,
				"invalid initiative expression "+expr, err)
		}
		roll = r
		value = r.Total + o.Modifier
	}

	entries, err := a.encounter.Initiative(ctx)
	if err != nil {
		return nil, err
	}
	entries = append(entries, storage.InitiativeEntry{Name: o.Name, Value: value})
	if err := a.encounter.SetInitiative(ctx, entries); err != nil {
		return nil, err
	}
	return a.initiativeList(entries, roll), nil
}

// ListInitiative returns the current encounter's initiative list, ordered
// from high to low (ties keep their insertion order).
func (a *App) ListInitiative(ctx context.Context) (*InitiativeList, error) {
	entries, err := a.encounter.Initiative(ctx)
	if err != nil {
		return nil, err
	}
	return a.initiativeList(entries, nil), nil
}

// ClearInitiative empties the current encounter's initiative list.
func (a *App) ClearInitiative(ctx context.Context) error {
	return a.encounter.SetInitiative(ctx, nil)
}

// initiativeList converts the stored entries into the ordered list.
func (a *App) initiativeList(entries []storage.InitiativeEntry, lastRoll *dice.RollResult) *InitiativeList {
	out := &InitiativeList{Kind: "initiative", Entries: make([]InitiativeEntry, 0, len(entries))}
	for i, e := range entries {
		entry := InitiativeEntry{Name: e.Name, Value: e.Value}
		if lastRoll != nil && i == len(entries)-1 {
			entry.Roll = lastRoll
		}
		out.Entries = append(out.Entries, entry)
	}
	// Stable sort: equal values keep the order in which they were added.
	sort.SliceStable(out.Entries, func(i, j int) bool {
		return out.Entries[i].Value > out.Entries[j].Value
	})
	return out
}

// ParseInitiativeArgs parses the Dice! `.ri` arguments into options. The
// first token decides the mode:
//
//	"+5" / "-1"  a modifier added to the default d20 roll
//	"80"        a fixed initiative value
//	"2d20kh1"   a custom dice expression
//
// The remaining tokens are the entry's name.
func ParseInitiativeArgs(args []string) (InitiativeOptions, error) {
	const op = "app.ParseInitiativeArgs"
	var o InitiativeOptions
	if len(args) == 0 {
		return o, nil
	}
	first := strings.TrimSpace(args[0])
	switch {
	case strings.HasPrefix(first, "+") || strings.HasPrefix(first, "-"):
		n, err := strconv.Atoi(first)
		if err != nil {
			return o, apperr.New(op, apperr.InvalidArgument,
				"initiative modifier must be an integer, got "+first)
		}
		o.Modifier = n
	case isInteger(first):
		n, err := strconv.Atoi(first)
		if err != nil {
			return o, apperr.New(op, apperr.InvalidArgument,
				"initiative value must be an integer, got "+first)
		}
		o.Value = &n
	default:
		o.Expr = first
	}
	o.Name = strings.TrimSpace(strings.Join(args[1:], " "))
	return o, nil
}

// isInteger reports whether s is a plain (unsigned) integer.
func isInteger(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.Atoi(s)
	return err == nil
}
