package sqlitedb

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"dicegirl/internal/apperr"
	"dicegirl/internal/storage"
)

// Games implements storage.GameStore on top of the database. The body is a
// JSON document, so the game-affairs schema can grow (new roster fields,
// roulette options) without a migration for every field.
type Games struct {
	store *Store
}

// Games returns the game-metadata store of the database.
func (s *Store) Games() *Games { return &Games{store: s} }

// GameMeta returns one table's metadata. A missing row is a fresh game: an
// empty, open GameMeta is returned instead of an error.
func (g *Games) GameMeta(ctx context.Context, tableID string) (*storage.GameMeta, error) {
	const op = "sqlitedb.Games.GameMeta"
	var body string
	err := g.store.db.QueryRowContext(ctx,
		`SELECT body FROM game_meta WHERE table_id = ?`, tableID).Scan(&body)
	if err == sql.ErrNoRows {
		return &storage.GameMeta{}, nil
	}
	if err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read game metadata", err)
	}
	meta := &storage.GameMeta{}
	if err := json.Unmarshal([]byte(body), meta); err != nil {
		return nil, apperr.Wrap(op, apperr.InvalidData, "invalid game metadata", err)
	}
	return meta, nil
}

// SetGameMeta replaces one table's metadata.
func (g *Games) SetGameMeta(ctx context.Context, tableID string, meta *storage.GameMeta) error {
	const op = "sqlitedb.Games.SetGameMeta"
	if meta == nil {
		meta = &storage.GameMeta{}
	}
	body, err := json.Marshal(meta)
	if err != nil {
		return apperr.Wrap(op, apperr.Internal, "cannot encode game metadata", err)
	}
	_, err = g.store.db.ExecContext(ctx,
		`INSERT INTO game_meta (table_id, body, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(table_id) DO UPDATE SET
		   body = excluded.body,
		   updated_at = excluded.updated_at`,
		tableID, string(body), formatTime(time.Now().UTC()))
	if err != nil {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot write game metadata", err)
	}
	return nil
}

// DeleteGameMeta removes one table's metadata.
func (g *Games) DeleteGameMeta(ctx context.Context, tableID string) error {
	const op = "sqlitedb.Games.DeleteGameMeta"
	if _, err := g.store.db.ExecContext(ctx,
		`DELETE FROM game_meta WHERE table_id = ?`, tableID); err != nil {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot delete game metadata", err)
	}
	return nil
}
