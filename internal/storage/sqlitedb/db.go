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

CREATE TABLE IF NOT EXISTS tables (
	id         TEXT PRIMARY KEY,
	name       TEXT NOT NULL UNIQUE,
	system_id  TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

-- NOTE: encounter_entries/decks already exist in pre-tables databases in
-- their old shape; IF NOT EXISTS leaves them untouched and migrateTables
-- rebuilds them. Any index on the new columns must be created AFTER that
-- migration, not in this batch (a missing column aborts the whole apply).
CREATE TABLE IF NOT EXISTS encounter_entries (
	seq      INTEGER PRIMARY KEY AUTOINCREMENT,
	table_id TEXT NOT NULL DEFAULT 'default',
	name     TEXT NOT NULL,
	value    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS decks (
	table_id  TEXT NOT NULL DEFAULT 'default',
	name      TEXT NOT NULL,
	cards     TEXT NOT NULL,
	remaining TEXT NOT NULL,
	discard   TEXT NOT NULL DEFAULT '[]',
	PRIMARY KEY (table_id, name)
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
	if err := s.migrateTables(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	// The encounter index references the post-migration table_id column;
	// create it only now so opening a pre-tables database does not fail.
	if _, err := db.ExecContext(context.Background(),
		`CREATE INDEX IF NOT EXISTS idx_encounter_table ON encounter_entries(table_id, seq)`); err != nil {
		db.Close()
		return nil, apperr.Wrap(op, apperr.StorageWriteFailed, "cannot create encounter index", err)
	}
	if err := s.migrateLegacyFiles(context.Background(), cfg); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// migrateTables upgrades a database created before game tables existed:
// the encounter gains a table_id column, the decks store is rebuilt with a
// table_id and a discard pile, the legacy active-character pointer is
// namespaced under the default table, and that table row is seeded. Fresh
// databases already carry the new schema, so only the seed row is written.
func (s *Store) migrateTables(ctx context.Context) error {
	const op = "sqlitedb.migrateTables"
	done, _, err := s.meta(ctx, "migrated_tables_v2")
	if err != nil {
		return err
	}
	if done == "1" {
		return nil
	}
	if err := s.withTx(ctx, func(tx *sql.Tx) error {
		if !columnExists(ctx, tx, "encounter_entries", "table_id") {
			if _, err := tx.ExecContext(ctx,
				`ALTER TABLE encounter_entries ADD COLUMN table_id TEXT NOT NULL DEFAULT 'default'`); err != nil {
				return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot upgrade encounter table", err)
			}
		}
		if !columnExists(ctx, tx, "decks", "discard") {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE decks RENAME TO decks_legacy`); err != nil {
				return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot move legacy decks table", err)
			}
			if _, err := tx.ExecContext(ctx, schemaDecksV2); err != nil {
				return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot create decks table", err)
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO decks (table_id, name, cards, remaining, discard)
				 SELECT 'default', name, cards, remaining, '[]' FROM decks_legacy`); err != nil {
				return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot move legacy decks", err)
			}
			if _, err := tx.ExecContext(ctx, `DROP TABLE decks_legacy`); err != nil {
				return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot drop legacy decks table", err)
			}
		}
		// Namespace the legacy active-character pointer under the default
		// table; the un-namespaced key is left in place harmlessly.
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO state (key, value)
			 SELECT 'active_character_id:default', value FROM state
			 WHERE key = 'active_character_id'`); err != nil {
			return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot namespace active character", err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := s.seedDefaultTable(ctx); err != nil {
		return err
	}
	return s.setMeta(ctx, "migrated_tables_v2", "1")
}

// schemaDecksV2 is the decks table created during the v2 migration (the main
// schema const already has this shape for fresh databases).
const schemaDecksV2 = `
CREATE TABLE decks (
	table_id  TEXT NOT NULL DEFAULT 'default',
	name      TEXT NOT NULL,
	cards     TEXT NOT NULL,
	remaining TEXT NOT NULL,
	discard   TEXT NOT NULL DEFAULT '[]',
	PRIMARY KEY (table_id, name)
)`

// columnExists reports whether table has a column.
func columnExists(ctx context.Context, tx *sql.Tx, table, column string) bool {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}

// seedDefaultTable makes sure the implicit default table exists. Its
// system_id stays empty ("use the registry default") until a user runs
// `table system`.
func (s *Store) seedDefaultTable(ctx context.Context) error {
	const op = "sqlitedb.seedDefaultTable"
	now := formatTime(time.Now().UTC())
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO tables (id, name, system_id, created_at, updated_at)
		 VALUES ('default', '默认桌', '', ?, ?)`, now, now)
	if err != nil {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot seed default table", err)
	}
	return nil
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
