// Package sqlitedb implements the data-root storage on one SQLite
// database. SQLite is used because the writable state is *hot*: every check
// updates the statistics, every draw rewrites a deck, every sheet edit
// rewrites a card. A transactional store keeps those writes cheap and
// correct under concurrency.
//
// The rules and the data packs deliberately stay YAML files (see the
// storage package doc): they are read once at startup and edited by hand.
package sqlitedb

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo)

	"dicegirl/internal/apperr"
	"dicegirl/internal/config"
)

// Store is the SQLite-backed storage of one data root.
type Store struct {
	db   *sql.DB
	path string
	cfg  *config.Config
}

// schema is applied on every open; the statements are idempotent.
const schema = `
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS cards (
	id         TEXT PRIMARY KEY,
	name       TEXT NOT NULL,
	system_id  TEXT NOT NULL,
	body       TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS card_backups (
	seq     INTEGER PRIMARY KEY AUTOINCREMENT,
	card_id TEXT NOT NULL,
	body    TEXT NOT NULL,
	at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_card_backups_card ON card_backups(card_id, seq DESC);

CREATE TABLE IF NOT EXISTS stat_totals (
	card_id   TEXT PRIMARY KEY REFERENCES cards(id) ON DELETE CASCADE,
	checks    INTEGER NOT NULL DEFAULT 0,
	successes INTEGER NOT NULL DEFAULT 0,
	failures  INTEGER NOT NULL DEFAULT 0,
	fumbles   INTEGER NOT NULL DEFAULT 0,
	roll_sum  INTEGER NOT NULL DEFAULT 0,
	best      INTEGER NOT NULL DEFAULT 0,
	worst     INTEGER NOT NULL DEFAULT 0,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS stat_keys (
	card_id   TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
	key       TEXT NOT NULL,
	kind      TEXT NOT NULL,
	name      TEXT NOT NULL,
	checks    INTEGER NOT NULL DEFAULT 0,
	successes INTEGER NOT NULL DEFAULT 0,
	failures  INTEGER NOT NULL DEFAULT 0,
	fumbles   INTEGER NOT NULL DEFAULT 0,
	roll_sum  INTEGER NOT NULL DEFAULT 0,
	best      INTEGER NOT NULL DEFAULT 0,
	worst     INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (card_id, key)
);

CREATE TABLE IF NOT EXISTS stat_levels (
	card_id   TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
	level_code TEXT NOT NULL,
	count      INTEGER NOT NULL,
	PRIMARY KEY (card_id, level_code)
);

CREATE TABLE IF NOT EXISTS stat_history (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	card_id   TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
	at        TEXT NOT NULL,
	key       TEXT NOT NULL,
	target    INTEGER NOT NULL,
	roll      INTEGER NOT NULL,
	level_code TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_stat_history_card ON stat_history(card_id, id DESC);

CREATE TABLE IF NOT EXISTS state (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS encounter_entries (
	seq   INTEGER PRIMARY KEY AUTOINCREMENT,
	name  TEXT NOT NULL,
	value INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS decks (
	name      TEXT PRIMARY KEY,
	cards     TEXT NOT NULL,
	remaining TEXT NOT NULL
);
`

// Open opens (and creates) the database of a data root, applies the schema
// and runs the one-time migration of the legacy YAML files.
func Open(cfg *config.Config) (*Store, error) {
	const op = "sqlitedb.Open"
	if cfg == nil || cfg.DataRoot == "" {
		return nil, apperr.New(op, apperr.Internal, "missing data root config")
	}
	if err := os.MkdirAll(cfg.DataRoot, 0o755); err != nil {
		return nil, apperr.Wrap(op, apperr.StorageWriteFailed,
			"cannot create data root "+cfg.DataRoot, err)
	}
	path := cfg.DatabaseFile()
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.StorageWriteFailed, "cannot open database "+path, err)
	}
	// A single writer keeps the counters consistent; WAL lets readers work
	// in parallel.
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(context.Background(), `PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, apperr.Wrap(op, apperr.StorageWriteFailed, "cannot enable WAL mode", err)
	}
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		db.Close()
		return nil, apperr.Wrap(op, apperr.StorageWriteFailed, "cannot apply database schema", err)
	}
	s := &Store{db: db, path: path, cfg: cfg}
	if err := s.migrateLegacyFiles(context.Background(), cfg); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// withTx runs fn inside a transaction.
func (s *Store) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	const op = "sqlitedb.withTx"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot begin transaction", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot commit transaction", err)
	}
	return nil
}

// meta reads a meta value.
func (s *Store) meta(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, apperr.Wrap("sqlitedb.meta", apperr.StorageReadFailed, "cannot read meta", err)
	}
	return value, true, nil
}

// setMeta writes a meta value.
func (s *Store) setMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return apperr.Wrap("sqlitedb.setMeta", apperr.StorageWriteFailed, "cannot write meta", err)
	}
	return nil
}

// formatTime renders a timestamp for the database.
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// parseTime reads a timestamp from the database.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// migrateLegacyFiles imports the pre-database YAML files (character cards,
// state, encounter and decks) once, then moves them into <root>/migrated/ so
// the migration is visible and the data is not lost.
func (s *Store) migrateLegacyFiles(ctx context.Context, cfg *config.Config) error {
	const op = "sqlitedb.migrate"
	done, _, err := s.meta(ctx, "migrated_legacy_yaml")
	if err != nil {
		return err
	}
	if done == "1" {
		return nil
	}
	legacy := filepath.Join(cfg.DataRoot, "legacy")
	_ = os.MkdirAll(legacy, 0o755)
	if err := s.importLegacy(ctx, cfg, legacy); err != nil {
		return err
	}
	return s.setMeta(ctx, "migrated_legacy_yaml", "1")
}

// importLegacy imports the legacy YAML files into the database.
func (s *Store) importLegacy(ctx context.Context, cfg *config.Config, legacy string) error {
	const op = "sqlitedb.importLegacy"
	// Character cards.
	entries, err := os.ReadDir(cfg.CharacterDir())
	if err != nil && !os.IsNotExist(err) {
		return apperr.Wrap(op, apperr.StorageReadFailed, "cannot read character directory", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		path := filepath.Join(cfg.CharacterDir(), e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return apperr.Wrap(op, apperr.StorageReadFailed, "cannot read "+path, err)
		}
		if err := s.importCard(ctx, data); err != nil {
			return err
		}
		if err := moveLegacy(path, filepath.Join(legacy, "characters", e.Name())); err != nil {
			return err
		}
	}
	// The remaining legacy files are imported as raw documents.
	for _, name := range []string{"state.yaml", "encounter.yaml", "decks.yaml"} {
		path := filepath.Join(cfg.DataRoot, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return apperr.Wrap(op, apperr.StorageReadFailed, "cannot read "+path, err)
		}
		if err := s.importLegacyDocument(ctx, name, data); err != nil {
			return err
		}
		if err := moveLegacy(path, filepath.Join(legacy, name)); err != nil {
			return err
		}
	}
	return nil
}

// moveLegacy moves a legacy file into the legacy folder.
func moveLegacy(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return apperr.Wrap("sqlitedb.moveLegacy", apperr.StorageWriteFailed,
			"cannot create "+filepath.Dir(dst), err)
	}
	if err := os.Rename(src, dst); err != nil {
		// A cross-device move fails; fall back to copy + remove.
		data, rerr := os.ReadFile(src)
		if rerr != nil {
			return apperr.Wrap("sqlitedb.moveLegacy", apperr.StorageWriteFailed,
				"cannot read "+src, rerr)
		}
		if werr := os.WriteFile(dst, data, 0o600); werr != nil {
			return apperr.Wrap("sqlitedb.moveLegacy", apperr.StorageWriteFailed,
				"cannot write "+dst, werr)
		}
		_ = os.Remove(src)
	}
	return nil
}
