package app

import (
	"context"
	"sort"

	"dicegirl/internal/apperr"
	"dicegirl/internal/storage"
)

// The roll statistics of a card: every check made with it is recorded, so
// the keeper can see where a character keeps failing. The counters live in
// the database (the storage seam), the aggregation rules here.

// CardStats returns the roll statistics of a card (the active one when no
// id is given).
func (a *App) CardStats(ctx context.Context, id string) (*storage.CardStats, error) {
	const op = "app.CardStats"
	cardID, err := a.resolveCardID(ctx, id)
	if err != nil {
		return nil, err
	}
	stats, err := a.stats.Stats(ctx, cardID)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

// ClearCardStats removes the statistics of a card.
func (a *App) ClearCardStats(ctx context.Context, id string) error {
	const op = "app.ClearCardStats"
	cardID, err := a.resolveCardID(ctx, id)
	if err != nil {
		return err
	}
	return a.stats.ClearStats(ctx, cardID)
}

// resolveCardID resolves the card the statistics belong to.
func (a *App) resolveCardID(ctx context.Context, id string) (string, error) {
	const op = "app.resolveCardID"
	if id != "" {
		return id, nil
	}
	active, err := a.state.ActiveID(ctx)
	if err != nil {
		return "", err
	}
	if active == "" {
		return "", apperr.New(op, apperr.InvalidArgument,
			"no character id given and no active character (use 'dicegirl character use <id>')")
	}
	return active, nil
}

// StatKeyName returns the display name of a statistics key (the rulebook
// label when the book is known, the recorded name otherwise).
func StatKeyName(stats *storage.CardStats, key string) string {
	for _, k := range stats.Keys {
		if k.Key == key {
			return k.Name
		}
	}
	return key
}

// SortStatKeys orders the per-key statistics by the number of checks
// (most used first), then by key.
func SortStatKeys(keys []storage.StatKeyTotals) {
	sort.SliceStable(keys, func(i, j int) bool {
		if keys[i].Checks != keys[j].Checks {
			return keys[i].Checks > keys[j].Checks
		}
		return keys[i].Key < keys[j].Key
	})
}
