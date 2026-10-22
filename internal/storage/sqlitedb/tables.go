package sqlitedb

import (
	"context"
	"database/sql"
	"time"

	"dicegirl/internal/apperr"
	"dicegirl/internal/storage"
)

// Tables implements storage.TableStore on top of the database.
type Tables struct {
	store *Store
}

// Tables returns the table store of the database.
func (s *Store) Tables() *Tables { return &Tables{store: s} }

const tableColumns = `id, name, system_id, created_at, updated_at`

// scanTable scans one tables row. A sql.ErrNoRows is returned unchanged so
// callers can tell "not found" apart from a real failure.
func scanTable(row interface {
	Scan(dest ...any) error
}) (*storage.GameTable, error) {
	var t storage.GameTable
	var createdAt, updatedAt string
	if err := row.Scan(&t.ID, &t.Name, &t.SystemID, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	t.CreatedAt = parseTime(createdAt)
	t.UpdatedAt = parseTime(updatedAt)
	return &t, nil
}

// notFound maps a scan error to (nil, nil) for an empty result, wrapping
// every other failure.
func tableReadError(err error) error {
	if err == sql.ErrNoRows {
		return nil
	}
	return apperr.Wrap("sqlitedb.Tables", apperr.StorageReadFailed, "cannot read table", err)
}

// Tables returns every game table, oldest first.
func (x *Tables) Tables(ctx context.Context) ([]storage.GameTable, error) {
	const op = "sqlitedb.Tables.Tables"
	rows, err := x.store.db.QueryContext(ctx,
		`SELECT `+tableColumns+` FROM tables ORDER BY created_at, rowid`)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read tables", err)
	}
	defer rows.Close()
	out := []storage.GameTable{}
	for rows.Next() {
		t, err := scanTable(rows)
		if err != nil {
			return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read tables", err)
		}
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read tables", err)
	}
	return out, nil
}

// TableByID returns one table, or nil without error when it does not exist.
func (x *Tables) TableByID(ctx context.Context, id string) (*storage.GameTable, error) {
	t, err := scanTable(x.store.db.QueryRowContext(ctx,
		`SELECT `+tableColumns+` FROM tables WHERE id = ?`, id))
	if err != nil {
		return nil, tableReadError(err)
	}
	return t, nil
}

// TableByName returns one table by its exact display name, or nil.
func (x *Tables) TableByName(ctx context.Context, name string) (*storage.GameTable, error) {
	t, err := scanTable(x.store.db.QueryRowContext(ctx,
		`SELECT `+tableColumns+` FROM tables WHERE name = ?`, name))
	if err != nil {
		return nil, tableReadError(err)
	}
	return t, nil
}

// CreateTable inserts one table. Duplicate id or name surface as a conflict.
func (x *Tables) CreateTable(ctx context.Context, t *storage.GameTable) error {
	const op = "sqlitedb.Tables.CreateTable"
	_, err := x.store.db.ExecContext(ctx,
		`INSERT INTO tables (id, name, system_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.SystemID, formatTime(t.CreatedAt), formatTime(t.UpdatedAt))
	if err != nil {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot create table "+t.Name, err)
	}
	return nil
}

// SetTableSystem updates the default rulebook of a table.
func (x *Tables) SetTableSystem(ctx context.Context, id, systemID string) error {
	const op = "sqlitedb.Tables.SetTableSystem"
	res, err := x.store.db.ExecContext(ctx,
		`UPDATE tables SET system_id = ?, updated_at = ? WHERE id = ?`,
		systemID, formatTime(time.Now().UTC()), id)
	if err != nil {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot update table "+id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperr.New(op, apperr.NotFound, "table not found: "+id)
	}
	return nil
}

// DeleteTable removes one table row. Its decks and encounter entries are
// removed by the caller (which owns the other stores).
func (x *Tables) DeleteTable(ctx context.Context, id string) error {
	const op = "sqlitedb.Tables.DeleteTable"
	res, err := x.store.db.ExecContext(ctx, `DELETE FROM tables WHERE id = ?`, id)
	if err != nil {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot delete table "+id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperr.New(op, apperr.NotFound, "table not found: "+id)
	}
	return nil
}
