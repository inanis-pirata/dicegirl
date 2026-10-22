package app

import (
	"context"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
)

// MaxChoiceOptions bounds the number of options in one random-choice round.
const MaxChoiceOptions = 64

// ChoiceResult is the structured outcome of a random draw.
type ChoiceResult struct {
	Kind    string   `json:"kind"`
	Options []string `json:"options"`
	Pick    string   `json:"pick"`
	Index   int      `json:"index"`
}

// ChoiceOptions returns the options of the open random-choice round.
func (a *App) ChoiceOptions(ctx context.Context) ([]string, error) {
	return a.state.ChoiceOptions(ctx)
}

// SetChoiceOptions replaces the options of the random-choice round.
func (a *App) SetChoiceOptions(ctx context.Context, options []string) ([]string, error) {
	clean, err := cleanChoiceOptions(options)
	if err != nil {
		return nil, err
	}
	if err := a.state.SetChoiceOptions(ctx, clean); err != nil {
		return nil, err
	}
	return clean, nil
}

// AddChoiceOptions appends options to the open round.
func (a *App) AddChoiceOptions(ctx context.Context, options []string) ([]string, error) {
	current, err := a.state.ChoiceOptions(ctx)
	if err != nil {
		return nil, err
	}
	merged := append(append([]string(nil), current...), options...)
	clean, err := cleanChoiceOptions(merged)
	if err != nil {
		return nil, err
	}
	if err := a.state.SetChoiceOptions(ctx, clean); err != nil {
		return nil, err
	}
	return clean, nil
}

// DeleteChoiceOption removes one option from the open round.
func (a *App) DeleteChoiceOption(ctx context.Context, option string) ([]string, error) {
	const op = "app.DeleteChoiceOption"
	current, err := a.state.ChoiceOptions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(current))
	found := false
	for _, opt := range current {
		if opt == option {
			found = true
			continue
		}
		out = append(out, opt)
	}
	if !found {
		return nil, apperr.New(op, apperr.InvalidArgument, "unknown choice option: "+option)
	}
	if err := a.state.SetChoiceOptions(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// ClearChoiceOptions empties the random-choice round.
func (a *App) ClearChoiceOptions(ctx context.Context) error {
	return a.state.SetChoiceOptions(ctx, nil)
}

// DrawChoice picks one option uniformly at random and ends the round.
func (a *App) DrawChoice(ctx context.Context) (*ChoiceResult, error) {
	const op = "app.DrawChoice"
	options, err := a.state.ChoiceOptions(ctx)
	if err != nil {
		return nil, err
	}
	if len(options) == 0 {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"no choice options; add some with 'ak add <选项...>' first")
	}
	idx, err := a.roller.Intn(ctx, len(options))
	if err != nil {
		return nil, err
	}
	res := &ChoiceResult{
		Kind:    "choice",
		Options: options,
		Pick:    options[idx],
		Index:   idx,
	}
	// A draw ends the round: the options are cleared afterwards.
	if err := a.state.SetChoiceOptions(ctx, nil); err != nil {
		return nil, err
	}
	return res, nil
}

// cleanChoiceOptions validates and normalizes the options of a round.
func cleanChoiceOptions(options []string) ([]string, error) {
	const op = "app.cleanChoiceOptions"
	out := make([]string, 0, len(options))
	seen := map[string]bool{}
	for _, opt := range options {
		opt = strings.TrimSpace(opt)
		if opt == "" {
			return nil, apperr.New(op, apperr.InvalidArgument, "choice options must not be empty")
		}
		if seen[opt] {
			return nil, apperr.New(op, apperr.InvalidArgument, "duplicate choice option: "+opt)
		}
		seen[opt] = true
		out = append(out, opt)
		if len(out) > MaxChoiceOptions {
			return nil, apperr.New(op, apperr.InvalidArgument,
				"too many choice options (limit "+strconv.Itoa(MaxChoiceOptions)+")")
		}
	}
	return out, nil
}
