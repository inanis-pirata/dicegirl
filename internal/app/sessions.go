package app

// Game tables ("桌"/"团"): a group data root can run several games in
// parallel. Cards stay shared across tables, but each table owns its default
// rulebook, its active-character pointer, one encounter (initiative) and one
// set of card decks. Chat adapters scope a sender's member bindings to a
// table; the CLI uses the single implicit default table.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"dicegirl/internal/apperr"
	"dicegirl/internal/storage"
)

// defaultTableName is the display name of the implicit default table.
const defaultTableName = "默认桌"

// SessionTable is one table's metadata.
type SessionTable struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	SystemID  string    `json:"system_id,omitempty"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// TableView is a table plus its current contents (used by `table` with no
// subcommand).
type TableView struct {
	SessionTable
	Decks      []string `json:"decks"`
	Initiative int      `json:"initiative"`
}

// TableList is the collection of tables.
type TableList struct {
	Kind   string         `json:"kind"`
	Tables []SessionTable `json:"tables"`
}

// CurrentTable returns the table the scope is currently using, creating the
// implicit default table on first use. Callers inside a request should put
// the returned id on the context (storage.WithTable).
func (a *App) CurrentTable(ctx context.Context) (*storage.GameTable, error) {
	const op = "app.CurrentTable"
	id, err := a.state.ActiveTable(ctx)
	if err != nil {
		return nil, err
	}
	if id != "" {
		if t, err := a.tables.TableByID(ctx, id); err != nil {
			return nil, err
		} else if t != nil {
			return t, nil
		}
	}
	// No (valid) pointer: the seeded default table, the oldest table, or a
	// freshly created default table.
	if t, err := a.tables.TableByID(ctx, storage.DefaultTableID); err != nil {
		return nil, err
	} else if t != nil {
		return t, a.state.SetActiveTable(ctx, t.ID)
	}
	list, err := a.tables.Tables(ctx)
	if err != nil {
		return nil, err
	}
	if len(list) > 0 {
		t := list[0]
		return &t, a.state.SetActiveTable(ctx, t.ID)
	}
	t, err := a.createTableRow(ctx, storage.DefaultTableID, defaultTableName, "")
	if err != nil {
		return nil, apperr.Wrap(op, apperr.CodeOf(err), "cannot create default table", err)
	}
	return t, a.state.SetActiveTable(ctx, t.ID)
}

// Table returns one table by id, or nil when it does not exist. Adapters use
// it to label per-table state they already keyed by id.
func (a *App) Table(ctx context.Context, id string) (*storage.GameTable, error) {
	return a.tables.TableByID(ctx, id)
}

// CurrentTableView returns the current table with its contents.
func (a *App) CurrentTableView(ctx context.Context) (*TableView, error) {
	t, err := a.CurrentTable(ctx)
	if err != nil {
		return nil, err
	}
	return a.tableView(ctx, t)
}

// CreateTable defines and switches to a new table. An empty system id uses
// the registry default; an explicit id must identify a loaded rulebook.
func (a *App) CreateTable(ctx context.Context, name, systemID string) (*TableView, error) {
	const op = "app.CreateTable"
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, apperr.New(op, apperr.InvalidArgument, "table name must not be empty")
	}
	if name == defaultTableName {
		return nil, apperr.New(op, apperr.InvalidArgument, "名字 "+defaultTableName+" 为默认桌保留")
	}
	if _, err := a.resolveTableSystem(systemID); err != nil {
		return nil, err
	}
	existing, err := a.tables.Tables(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range existing {
		if strings.EqualFold(t.Name, name) {
			return nil, apperr.New(op, apperr.Conflict, "已存在同名的桌："+name)
		}
	}
	id, err := newTableID()
	if err != nil {
		return nil, err
	}
	t, err := a.createTableRow(ctx, id, name, systemID)
	if err != nil {
		return nil, err
	}
	if err := a.state.SetActiveTable(ctx, t.ID); err != nil {
		return nil, err
	}
	return a.tableView(ctx, t)
}

// createTableRow resolves the system id and persists one table row.
func (a *App) createTableRow(ctx context.Context, id, name, systemID string) (*storage.GameTable, error) {
	systemID, err := a.resolveTableSystem(systemID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	t := &storage.GameTable{ID: id, Name: name, SystemID: systemID, CreatedAt: now, UpdatedAt: now}
	if err := a.tables.CreateTable(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// resolveTableSystem maps an empty id to the registry default and validates
// an explicit one against the loaded books.
func (a *App) resolveTableSystem(systemID string) (string, error) {
	systemID = strings.TrimSpace(systemID)
	id, err := a.defaultSystemID(systemID)
	if err != nil {
		return "", err
	}
	if _, err := a.book(id); err != nil {
		return "", err
	}
	return id, nil
}

// UseTable switches the current table, addressing it by id or by exact /
// case-insensitive display name.
func (a *App) UseTable(ctx context.Context, ref string) (*TableView, error) {
	const op = "app.UseTable"
	t, err := a.resolveTable(ctx, ref)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.CodeOf(err), "无法切换桌", err)
	}
	if err := a.state.SetActiveTable(ctx, t.ID); err != nil {
		return nil, err
	}
	return a.tableView(ctx, t)
}

// ListGameTables returns every game table, marking the current one. (The
// random-table listing keeps the ListTables name.)
func (a *App) ListGameTables(ctx context.Context) (*TableList, error) {
	rows, err := a.tables.Tables(ctx)
	if err != nil {
		return nil, err
	}
	active, err := a.ActiveTableID(ctx)
	if err != nil {
		return nil, err
	}
	out := &TableList{Kind: "tables", Tables: make([]SessionTable, 0, len(rows))}
	for _, t := range rows {
		out.Tables = append(out.Tables, SessionTable{
			ID:        t.ID,
			Name:      t.Name,
			SystemID:  t.SystemID,
			Active:    t.ID == active,
			CreatedAt: t.CreatedAt,
		})
	}
	return out, nil
}

// SetCurrentTableSystem changes the default rulebook of the current table.
func (a *App) SetCurrentTableSystem(ctx context.Context, systemID string) (*TableView, error) {
	t, err := a.CurrentTable(ctx)
	if err != nil {
		return nil, err
	}
	id, err := a.resolveTableSystem(systemID)
	if err != nil {
		return nil, err
	}
	if err := a.tables.SetTableSystem(ctx, t.ID, id); err != nil {
		return nil, err
	}
	t.SystemID = id
	return a.tableView(ctx, t)
}

// DeleteTable removes a table other than the current one together with its
// decks and encounter. Member bindings held by an adapter are pruned
// separately by that adapter.
func (a *App) DeleteTable(ctx context.Context, ref string) error {
	const op = "app.DeleteTable"
	t, err := a.resolveTable(ctx, ref)
	if err != nil {
		return apperr.Wrap(op, apperr.CodeOf(err), "无法删除桌", err)
	}
	active, err := a.ActiveTableID(ctx)
	if err != nil {
		return err
	}
	if t.ID == active {
		return apperr.New(op, apperr.InvalidArgument,
			"不能删除当前桌：先用 table use <名字> 切换到其他桌")
	}
	tctx := storage.WithTable(ctx, t.ID)
	if err := a.encounter.SetInitiative(tctx, t.ID, nil); err != nil {
		return err
	}
	if err := a.decks.SetDecks(tctx, t.ID, map[string]*storage.Deck{}); err != nil {
		return err
	}
	return a.tables.DeleteTable(ctx, t.ID)
}

// resolveTable resolves a user-typed table reference (id, exact name,
// case-insensitive name).
func (a *App) resolveTable(ctx context.Context, ref string) (*storage.GameTable, error) {
	const op = "app.resolveTable"
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, apperr.New(op, apperr.InvalidArgument, "缺少桌名")
	}
	rows, err := a.tables.Tables(ctx)
	if err != nil {
		return nil, err
	}
	if t, err := a.tables.TableByID(ctx, ref); err != nil {
		return nil, err
	} else if t != nil {
		return t, nil
	}
	var exact, folded []*storage.GameTable
	for i := range rows {
		switch {
		case rows[i].Name == ref:
			exact = append(exact, &rows[i])
		case strings.EqualFold(rows[i].Name, ref):
			folded = append(folded, &rows[i])
		}
	}
	for _, matches := range [][]*storage.GameTable{exact, folded} {
		switch len(matches) {
		case 0:
			continue
		case 1:
			return matches[0], nil
		default:
			return nil, apperr.New(op, apperr.InvalidArgument, "桌名 "+ref+" 对应多张桌，请改用 ID")
		}
	}
	return nil, apperr.New(op, apperr.NotFound, "找不到桌："+ref+"（可用 table list 查看）")
}

// ActiveTableID returns the active table id, resolving the pointer (and
// seeding the default table) when needed. Chat entry points call it once per
// request and scope the request context with storage.WithTable.
func (a *App) ActiveTableID(ctx context.Context) (string, error) {
	if id, err := a.state.ActiveTable(ctx); err != nil {
		return "", err
	} else if id != "" {
		if t, err := a.tables.TableByID(ctx, id); err != nil {
			return "", err
		} else if t != nil {
			return id, nil
		}
	}
	t, err := a.CurrentTable(ctx)
	if err != nil {
		return "", err
	}
	return t.ID, nil
}

// tableView assembles the public view of a table: its deck names and the
// number of initiative entries.
func (a *App) tableView(ctx context.Context, t *storage.GameTable) (*TableView, error) {
	tctx := storage.WithTable(ctx, t.ID)
	decks, err := a.decks.Decks(tctx, t.ID)
	if err != nil {
		return nil, err
	}
	entries, err := a.encounter.Initiative(tctx, t.ID)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(decks))
	for name := range decks {
		names = append(names, name)
	}
	sort.Strings(names)
	active, err := a.state.ActiveTable(ctx)
	if err != nil {
		return nil, err
	}
	return &TableView{
		SessionTable: SessionTable{
			ID:        t.ID,
			Name:      t.Name,
			SystemID:  t.SystemID,
			Active:    t.ID == active,
			CreatedAt: t.CreatedAt,
		},
		Decks:      names,
		Initiative: len(entries),
	}, nil
}

// newTableID generates a program-safe ID "t-" + 4 random hex bytes.
func newTableID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", apperr.Wrap("app.newTableID", apperr.Internal, "cannot generate table id", err)
	}
	return "t-" + hex.EncodeToString(b), nil
}
