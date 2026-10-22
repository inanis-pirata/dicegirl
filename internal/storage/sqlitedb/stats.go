package sqlitedb

import (
	"context"
	"database/sql"
	"time"

	"dicegirl/internal/apperr"
	"dicegirl/internal/storage"
)

// MaxHistoryPerCard bounds the recent-check history kept per card.
const MaxHistoryPerCard = 50

// Stats implements storage.StatsStore on top of the database.
type Stats struct {
	store *Store
}

// Stats returns the statistics store of the database.
func (s *Store) Stats() *Stats { return &Stats{store: s} }

// Record adds one check to the card's statistics. Every write happens in a
// single transaction, so the aggregates and the history stay consistent and
// concurrent writers cannot lose an update.
func (st *Stats) Record(ctx context.Context, rec storage.StatRecord) error {
	const op = "sqlitedb.Stats.Record"
	if rec.CardID == "" {
		return apperr.New(op, apperr.InvalidArgument, "stat record without a card")
	}
	now := rec.At
	if now.IsZero() {
		now = time.Now().UTC()
	}
	failures := 0
	if !rec.Success {
		failures = 1
	}
	fumbles := 0
	if rec.Fumble {
		fumbles = 1
	}
	return st.store.withTx(ctx, func(tx *sql.Tx) error {
		// The card must exist (the statistics reference it).
		var exists int
		err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM cards WHERE id = ?`, rec.CardID).Scan(&exists)
		if err != nil {
			return apperr.Wrap(op, apperr.StorageReadFailed, "cannot read character", err)
		}
		if exists == 0 {
			return apperr.New(op, apperr.NotFound, "character not found: "+rec.CardID)
		}
		// Totals.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO stat_totals (card_id, checks, successes, failures, fumbles, roll_sum, best, worst, updated_at)
			VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(card_id) DO UPDATE SET
				checks = checks + 1,
				successes = successes + excluded.successes,
				failures = failures + excluded.failures,
				fumbles = fumbles + excluded.fumbles,
				roll_sum = roll_sum + excluded.roll_sum,
				best = MAX(best, excluded.best),
				worst = CASE WHEN worst = 0 THEN excluded.worst ELSE MIN(worst, excluded.worst) END,
				updated_at = excluded.updated_at`,
			rec.CardID, boolToInt(rec.Success), failures, fumbles, rec.Roll, rec.Roll, rec.Roll,
			formatTime(now)); err != nil {
			return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot update statistics", err)
		}
		// Per key.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO stat_keys (card_id, key, kind, name, checks, successes, failures, fumbles, roll_sum, best, worst)
			VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(card_id, key) DO UPDATE SET
				name = excluded.name,
				kind = excluded.kind,
				checks = checks + 1,
				successes = successes + excluded.successes,
				failures = failures + excluded.failures,
				fumbles = fumbles + excluded.fumbles,
				roll_sum = roll_sum + excluded.roll_sum,
				best = MAX(best, excluded.best),
				worst = CASE WHEN worst = 0 THEN excluded.worst ELSE MIN(worst, excluded.worst) END`,
			rec.CardID, rec.Key, rec.Kind, rec.Name, boolToInt(rec.Success), failures, fumbles,
			rec.Roll, rec.Roll, rec.Roll); err != nil {
			return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot update statistics", err)
		}
		// Per level.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO stat_levels (card_id, level_code, count) VALUES (?, ?, 1)
			ON CONFLICT(card_id, level_code) DO UPDATE SET count = count + 1`,
			rec.CardID, rec.LevelCode); err != nil {
			return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot update statistics", err)
		}
		// Recent history (bounded).
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO stat_history (card_id, at, key, target, roll, level_code) VALUES (?, ?, ?, ?, ?, ?)`,
			rec.CardID, formatTime(now), rec.Key, rec.Target, rec.Roll, rec.LevelCode); err != nil {
			return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot record history", err)
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM stat_history WHERE card_id = ? AND id NOT IN (
				SELECT id FROM stat_history WHERE card_id = ? ORDER BY id DESC LIMIT ?
			)`, rec.CardID, rec.CardID, MaxHistoryPerCard); err != nil {
			return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot trim history", err)
		}
		return nil
	})
}

// Stats returns the statistics of a card.
func (st *Stats) Stats(ctx context.Context, cardID string) (*storage.CardStats, error) {
	const op = "sqlitedb.Stats.Stats"
	out := &storage.CardStats{CardID: cardID, Levels: map[string]int{}}
	var updated string
	err := st.store.db.QueryRowContext(ctx,
		`SELECT checks, successes, failures, fumbles, roll_sum, best, worst, updated_at
		 FROM stat_totals WHERE card_id = ?`, cardID).
		Scan(&out.Checks, &out.Successes, &out.Failures, &out.Fumbles,
			&out.RollSum, &out.Best, &out.Worst, &updated)
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read statistics", err)
	}
	out.UpdatedAt = parseTime(updated)

	rows, err := st.store.db.QueryContext(ctx,
		`SELECT key, kind, name, checks, successes, failures, fumbles, roll_sum, best, worst
		 FROM stat_keys WHERE card_id = ? ORDER BY checks DESC, key`, cardID)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read statistics", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k storage.StatKeyTotals
		if err := rows.Scan(&k.Key, &k.Kind, &k.Name, &k.Checks, &k.Successes,
			&k.Failures, &k.Fumbles, &k.RollSum, &k.Best, &k.Worst); err != nil {
			return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read statistics", err)
		}
		out.Keys = append(out.Keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read statistics", err)
	}

	levelRows, err := st.store.db.QueryContext(ctx,
		`SELECT level_code, count FROM stat_levels WHERE card_id = ? ORDER BY level_code`, cardID)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read statistics", err)
	}
	defer levelRows.Close()
	for levelRows.Next() {
		var code string
		var count int
		if err := levelRows.Scan(&code, &count); err != nil {
			return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read statistics", err)
		}
		out.Levels[code] = count
	}
	if err := levelRows.Err(); err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read statistics", err)
	}

	histRows, err := st.store.db.QueryContext(ctx,
		`SELECT at, key, target, roll, level_code FROM stat_history
		 WHERE card_id = ? ORDER BY id DESC LIMIT ?`, cardID, MaxHistoryPerCard)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read statistics", err)
	}
	defer histRows.Close()
	for histRows.Next() {
		var item storage.StatHistoryItem
		var at string
		if err := histRows.Scan(&at, &item.Key, &item.Target, &item.Roll, &item.LevelCode); err != nil {
			return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read statistics", err)
		}
		item.At = parseTime(at)
		out.History = append(out.History, item)
	}
	if err := histRows.Err(); err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read statistics", err)
	}
	return out, nil
}

// ClearStats removes the statistics of a card.
func (st *Stats) ClearStats(ctx context.Context, cardID string) error {
	const op = "sqlitedb.Stats.ClearStats"
	return st.store.withTx(ctx, func(tx *sql.Tx) error {
		for _, table := range []string{"stat_totals", "stat_keys", "stat_levels", "stat_history"} {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE card_id = ?`, cardID); err != nil {
				return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot clear statistics", err)
			}
		}
		return nil
	})
}

// DeleteStats removes the statistics of a card (the card itself is deleted
// by the card repository; the foreign keys cascade).
func (st *Stats) DeleteStats(ctx context.Context, cardID string) error {
	return st.ClearStats(ctx, cardID)
}

// boolToInt maps a flag to 1/0.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
