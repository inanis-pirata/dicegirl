package sqlitedb

// Play-session logs (`.log`). A log belongs to one data root: the chat
// adapters give every group its own database, so no group id is needed here.
// Metadata lives in one JSON body per session while entries are separate rows,
// which keeps appending a message cheap even for a long session.

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"dicegirl/internal/apperr"
	"dicegirl/internal/storage"
)

// Logs implements storage.LogStore on top of the database.
type Logs struct {
	store *Store
}

// Logs returns the play-session log store of the database.
func (s *Store) Logs() *Logs { return &Logs{store: s} }

// scanLog reads one session row. The entry count comes from the dedicated
// column, which AppendLogEntry owns; the JSON body never has to be rewritten
// when a message is recorded.
func scanLog(scan func(dest ...any) error) (*storage.LogSession, error) {
	const op = "sqlitedb.Logs.scan"
	var (
		body    string
		entries int
	)
	l := &storage.LogSession{}
	if err := scan(&body, &entries); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(body), l); err != nil {
		return nil, apperr.Wrap(op, apperr.InvalidData, "invalid log metadata", err)
	}
	l.Entries = entries
	return l, nil
}

const logColumns = `body, entries`

// LogByID returns one log, or nil when it does not exist.
func (l *Logs) LogByID(ctx context.Context, id string) (*storage.LogSession, error) {
	const op = "sqlitedb.Logs.LogByID"
	row := l.store.db.QueryRowContext(ctx,
		`SELECT `+logColumns+` FROM logs WHERE id = ?`, id)
	out, err := scanLog(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read log", err)
	}
	return out, nil
}

// LogByName returns the newest log with the exact name, or nil.
func (l *Logs) LogByName(ctx context.Context, name string) (*storage.LogSession, error) {
	const op = "sqlitedb.Logs.LogByName"
	row := l.store.db.QueryRowContext(ctx,
		`SELECT `+logColumns+` FROM logs WHERE name = ?
		 ORDER BY created_at DESC, id DESC LIMIT 1`, name)
	out, err := scanLog(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read log", err)
	}
	return out, nil
}

// CurrentLog returns the in-progress log (active or paused), or nil.
func (l *Logs) CurrentLog(ctx context.Context) (*storage.LogSession, error) {
	const op = "sqlitedb.Logs.CurrentLog"
	row := l.store.db.QueryRowContext(ctx,
		`SELECT `+logColumns+` FROM logs WHERE status IN (?, ?)
		 ORDER BY created_at DESC, id DESC LIMIT 1`,
		storage.LogStatusActive, storage.LogStatusPaused)
	out, err := scanLog(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read current log", err)
	}
	return out, nil
}

// Logs returns every log, newest first.
func (l *Logs) Logs(ctx context.Context) ([]*storage.LogSession, error) {
	const op = "sqlitedb.Logs.Logs"
	rows, err := l.store.db.QueryContext(ctx,
		`SELECT `+logColumns+` FROM logs ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot list logs", err)
	}
	defer rows.Close()
	var out []*storage.LogSession
	for rows.Next() {
		s, err := scanLog(rows.Scan)
		if err != nil {
			return nil, apperr.Wrap(op, apperr.InvalidData, "invalid log metadata", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot list logs", err)
	}
	return out, nil
}

// CreateLog stores a new session.
func (l *Logs) CreateLog(ctx context.Context, log *storage.LogSession) error {
	const op = "sqlitedb.Logs.CreateLog"
	if log == nil {
		return apperr.New(op, apperr.InvalidArgument, "missing log")
	}
	body, err := json.Marshal(log)
	if err != nil {
		return apperr.Wrap(op, apperr.Internal, "cannot encode log metadata", err)
	}
	now := formatTime(time.Now().UTC())
	created := log.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	_, err = l.store.db.ExecContext(ctx,
		`INSERT INTO logs (id, name, status, entries, created_at, updated_at, body)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		log.ID, log.Name, log.Status, log.Entries, formatTime(created), now, string(body))
	if err != nil {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot create log", err)
	}
	return nil
}

// UpdateLog replaces a session's metadata. The entry count stays owned by
// AppendLogEntry.
func (l *Logs) UpdateLog(ctx context.Context, log *storage.LogSession) error {
	const op = "sqlitedb.Logs.UpdateLog"
	if log == nil {
		return apperr.New(op, apperr.InvalidArgument, "missing log")
	}
	body, err := json.Marshal(log)
	if err != nil {
		return apperr.Wrap(op, apperr.Internal, "cannot encode log metadata", err)
	}
	res, err := l.store.db.ExecContext(ctx,
		`UPDATE logs SET name = ?, status = ?, body = ?, updated_at = ? WHERE id = ?`,
		log.Name, log.Status, string(body), formatTime(time.Now().UTC()), log.ID)
	if err != nil {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot update log", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return apperr.New(op, apperr.NotFound, "log not found: "+log.ID)
	}
	return nil
}

// DeleteLog removes a session and its entries.
func (l *Logs) DeleteLog(ctx context.Context, id string) error {
	const op = "sqlitedb.Logs.DeleteLog"
	if _, err := l.store.db.ExecContext(ctx, `DELETE FROM logs WHERE id = ?`, id); err != nil {
		return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot delete log", err)
	}
	return nil
}

// AppendLogEntry records one entry and increments the session's count.
func (l *Logs) AppendLogEntry(ctx context.Context, logID string, entry *storage.LogEntry) error {
	const op = "sqlitedb.Logs.AppendLogEntry"
	if entry == nil {
		return apperr.New(op, apperr.InvalidArgument, "missing log entry")
	}
	at := entry.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return l.store.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO log_entries
			   (log_id, at, kind, speaker_id, speaker, text, result, image_url)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			logID, formatTime(at), entry.Kind, entry.SpeakerID, entry.Speaker,
			entry.Text, entry.Result, entry.ImageURL); err != nil {
			return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot append log entry", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE logs SET entries = entries + 1, updated_at = ? WHERE id = ?`,
			formatTime(time.Now().UTC()), logID); err != nil {
			return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot update log count", err)
		}
		return nil
	})
}

// LogEntries returns a session's entries in recording order.
func (l *Logs) LogEntries(ctx context.Context, logID string) ([]storage.LogEntry, error) {
	const op = "sqlitedb.Logs.LogEntries"
	rows, err := l.store.db.QueryContext(ctx,
		`SELECT seq, at, kind, speaker_id, speaker, text, result, image_url
		 FROM log_entries WHERE log_id = ? ORDER BY seq`, logID)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read log entries", err)
	}
	defer rows.Close()
	var out []storage.LogEntry
	for rows.Next() {
		var (
			at string
			e  storage.LogEntry
		)
		if err := rows.Scan(&e.Seq, &at, &e.Kind, &e.SpeakerID, &e.Speaker,
			&e.Text, &e.Result, &e.ImageURL); err != nil {
			return nil, apperr.Wrap(op, apperr.InvalidData, "invalid log entry", err)
		}
		e.At = parseTime(at)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read log entries", err)
	}
	return out, nil
}

// LogSettings returns the data root's log preferences.
func (l *Logs) LogSettings(ctx context.Context) (*storage.LogSettings, error) {
	const op = "sqlitedb.Logs.LogSettings"
	rows, err := l.store.db.QueryContext(ctx, `SELECT key, value FROM log_settings`)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read log settings", err)
	}
	defer rows.Close()
	out := &storage.LogSettings{Format: storage.LogFormatTXT}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, apperr.Wrap(op, apperr.InvalidData, "invalid log settings", err)
		}
		switch key {
		case "format":
			if value == storage.LogFormatHTML {
				out.Format = storage.LogFormatHTML
			} else {
				out.Format = storage.LogFormatTXT
			}
		case "timer":
			out.Timer = value == "on"
		}
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read log settings", err)
	}
	return out, nil
}

// SetLogSettings replaces the log preferences.
func (l *Logs) SetLogSettings(ctx context.Context, settings *storage.LogSettings) error {
	const op = "sqlitedb.Logs.SetLogSettings"
	if settings == nil {
		return apperr.New(op, apperr.InvalidArgument, "missing log settings")
	}
	format := settings.Format
	if format == "" {
		format = storage.LogFormatTXT
	}
	timer := "off"
	if settings.Timer {
		timer = "on"
	}
	return l.store.withTx(ctx, func(tx *sql.Tx) error {
		for key, value := range map[string]string{"format": format, "timer": timer} {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO log_settings (key, value) VALUES (?, ?)
				 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
				key, value); err != nil {
				return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot write log settings", err)
			}
		}
		return nil
	})
}
