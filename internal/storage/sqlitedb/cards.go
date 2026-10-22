package sqlitedb

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
)

// Cards implements character.Repository on top of the database.
//
// The card body is stored as YAML — the same document the export command
// produces — so the interchange format and the stored format stay one and
// the same.
type Cards struct {
	store *Store
}

// Cards returns the character-card repository of the store.
func (s *Store) Cards() *Cards { return &Cards{store: s} }

// Create stores a new card.
func (c *Cards) Create(ctx context.Context, ch *character.Character) error {
	const op = "sqlitedb.Cards.Create"
	if ch == nil {
		return apperr.New(op, apperr.Internal, "nil character")
	}
	if err := ch.Validate(); err != nil {
		return err
	}
	body, err := yaml.Marshal(ch)
	if err != nil {
		return apperr.Wrap(op, apperr.Internal, "cannot encode character", err)
	}
	now := time.Now().UTC()
	ch.CreatedAt = now
	ch.UpdatedAt = now
	_, err = c.store.db.ExecContext(ctx,
		`INSERT INTO cards (id, name, system_id, body, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		ch.ID, ch.Name, ch.SystemID, string(body), formatTime(now), formatTime(now))
	if err != nil {
		if isUniqueViolation(err) {
			return apperr.New(op, apperr.Conflict, "character already exists: "+ch.ID)
		}
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot store character", err)
	}
	return nil
}

// Get loads a card.
func (c *Cards) Get(ctx context.Context, id string) (*character.Character, error) {
	const op = "sqlitedb.Cards.Get"
	if err := character.ValidateID(id); err != nil {
		return nil, err
	}
	var body string
	err := c.store.db.QueryRowContext(ctx, `SELECT body FROM cards WHERE id = ?`, id).Scan(&body)
	if err == sql.ErrNoRows {
		return nil, apperr.New(op, apperr.NotFound, "character not found: "+id)
	}
	if err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read character", err)
	}
	ch := &character.Character{}
	if err := yaml.Unmarshal([]byte(body), ch); err != nil {
		return nil, apperr.Wrap(op, apperr.InvalidData, "invalid stored character "+id, err)
	}
	if ch.SchemaVersion != character.SchemaVersion {
		return nil, apperr.New(op, apperr.UnsupportedSchemaVersion,
			"character "+id+" uses schema_version "+ch.SchemaVersion+", binary supports "+character.SchemaVersion)
	}
	if err := ch.Validate(); err != nil {
		return nil, err
	}
	return ch, nil
}

// List returns every card, sorted by id.
func (c *Cards) List(ctx context.Context) ([]*character.Character, error) {
	const op = "sqlitedb.Cards.List"
	rows, err := c.store.db.QueryContext(ctx, `SELECT body FROM cards ORDER BY id`)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot list characters", err)
	}
	defer rows.Close()
	out := []*character.Character{}
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read character", err)
		}
		ch := &character.Character{}
		if err := yaml.Unmarshal([]byte(body), ch); err != nil {
			return nil, apperr.Wrap(op, apperr.InvalidData, "invalid stored character", err)
		}
		if err := ch.Validate(); err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot list characters", err)
	}
	return out, nil
}

// Update replaces a card and keeps a backup of the previous version.
func (c *Cards) Update(ctx context.Context, ch *character.Character) error {
	const op = "sqlitedb.Cards.Update"
	if ch == nil {
		return apperr.New(op, apperr.Internal, "nil character")
	}
	if err := ch.Validate(); err != nil {
		return err
	}
	body, err := yaml.Marshal(ch)
	if err != nil {
		return apperr.Wrap(op, apperr.Internal, "cannot encode character", err)
	}
	now := time.Now().UTC()
	ch.UpdatedAt = now
	return c.store.withTx(ctx, func(tx *sql.Tx) error {
		var previous string
		err := tx.QueryRowContext(ctx, `SELECT body FROM cards WHERE id = ?`, ch.ID).Scan(&previous)
		if err == sql.ErrNoRows {
			return apperr.New(op, apperr.NotFound, "character not found: "+ch.ID)
		}
		if err != nil {
			return apperr.Wrap(op, apperr.StorageReadFailed, "cannot read character", err)
		}
		if c.store.cfg != nil && c.store.cfg.Backups.Enabled {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO card_backups (card_id, body, at) VALUES (?, ?, ?)`,
				ch.ID, previous, formatTime(now)); err != nil {
				return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot store backup", err)
			}
			if err := c.store.pruneBackups(ctx, tx, ch.ID); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx,
			`UPDATE cards SET name = ?, system_id = ?, body = ?, updated_at = ? WHERE id = ?`,
			ch.Name, ch.SystemID, string(body), formatTime(now), ch.ID)
		if err != nil {
			return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot update character", err)
		}
		return nil
	})
}

// Delete removes a card (its statistics and backups cascade).
func (c *Cards) Delete(ctx context.Context, id string) error {
	const op = "sqlitedb.Cards.Delete"
	if err := character.ValidateID(id); err != nil {
		return err
	}
	res, err := c.store.db.ExecContext(ctx, `DELETE FROM cards WHERE id = ?`, id)
	if err != nil {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot delete character", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot delete character", err)
	}
	if n == 0 {
		return apperr.New(op, apperr.NotFound, "character not found: "+id)
	}
	return nil
}

// importCard imports one legacy character card file.
func (s *Store) importCard(ctx context.Context, data []byte) error {
	ch := &character.Character{}
	if err := yaml.Unmarshal(data, ch); err != nil {
		return apperr.Wrap("sqlitedb.importCard", apperr.InvalidData,
			"invalid legacy character file", err)
	}
	if ch.ID == "" || ch.SchemaVersion != character.SchemaVersion {
		// A card we cannot read is skipped rather than aborting the
		// migration; the file stays in the legacy folder.
		return nil
	}
	body, err := yaml.Marshal(ch)
	if err != nil {
		return apperr.Wrap("sqlitedb.importCard", apperr.Internal, "cannot encode character", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO cards (id, name, system_id, body, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO NOTHING`,
		ch.ID, ch.Name, ch.SystemID, string(body),
		formatTime(ch.CreatedAt), formatTime(ch.UpdatedAt))
	if err != nil {
		return apperr.Wrap("sqlitedb.importCard", apperr.StorageWriteFailed,
			"cannot import character "+ch.ID, err)
	}
	return nil
}

// pruneBackups keeps only the configured number of backups of a card.
func (s *Store) pruneBackups(ctx context.Context, tx *sql.Tx, cardID string) error {
	keep := 0
	if s.cfg != nil {
		keep = s.cfg.Backups.MaxPerCharacter
	}
	if keep <= 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx,
		`DELETE FROM card_backups WHERE card_id = ? AND seq NOT IN (
			SELECT seq FROM card_backups WHERE card_id = ? ORDER BY seq DESC LIMIT ?
		)`, cardID, cardID, keep)
	if err != nil {
		return apperr.Wrap("sqlitedb.pruneBackups", apperr.StorageWriteFailed,
			"cannot prune backups", err)
	}
	return nil
}

// isUniqueViolation reports whether err is a unique-constraint violation.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed")
}
