// Package app wires the domain layers together and exposes the application
// services the CLI (and future chat-platform adapters) call. It owns no
// rule logic itself: dice mechanics live in internal/dice, game rules are
// declarative rulebooks interpreted by internal/engine, and persistence
// lives in internal/storage.
package app

import (
	"context"
	"io"

	"dicegirl/datapacks"
	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
	"dicegirl/internal/config"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine"
	"dicegirl/internal/rules"
	"dicegirl/internal/storage"
	"dicegirl/internal/storage/sqlitedb"
	"dicegirl/rulebooks"
)

// App is the assembled application service.
type App struct {
	cfg       *config.Config
	roller    *dice.Roller
	registry  *rules.Registry
	chars     character.Repository
	state     storage.StateStore
	encounter storage.EncounterStore
	decks     storage.DeckStore
	stats     storage.StatsStore
	packs     []*engine.DataPack
	closer    io.Closer
}

// Close releases the resources held by the app (the database handle).
func (a *App) Close() error {
	if a == nil || a.closer == nil {
		return nil
	}
	return a.closer.Close()
}

// Option customizes assembly (used by tests to inject a deterministic roller
// or in-memory repository).
type Option func(*App)

// WithRoller injects a configured dice roller.
func WithRoller(r *dice.Roller) Option {
	return func(a *App) { a.roller = r }
}

// WithRepository overrides the character repository.
func WithRepository(repo character.Repository) Option {
	return func(a *App) { a.chars = repo }
}

// New builds the App: storage, state, dice engine and every rulebook:
// built-in embedded books plus user books under <data-root>/rulebooks/.
// All rulebooks are data interpreted by one engine; there is no per-game
// code package.
//
// The writable state (character cards, roll statistics, local state, the
// current encounter and the card decks) lives in one SQLite database; the
// rules and the data packs stay YAML files.
func New(ctx context.Context, cfg *config.Config, opts ...Option) (*App, error) {
	store, err := sqlitedb.Open(cfg)
	if err != nil {
		return nil, err
	}
	a := &App{
		cfg:       cfg,
		roller:    dice.Default(),
		registry:  rules.NewRegistry(),
		chars:     store.Cards(),
		state:     store.State(),
		encounter: store.Encounter(),
		decks:     store.Decks(),
		stats:     store.Stats(),
		closer:    store,
	}
	for _, o := range opts {
		o(a)
	}
	// From here on the database handle is open: release it on any failure.
	ok := false
	defer func() {
		if !ok {
			_ = a.Close()
		}
	}()

	books, err := engine.LoadBuiltins(rulebooks.FS, a.roller)
	if err != nil {
		return nil, err
	}
	userBooks, err := engine.LoadDir(cfg.RulebookDir(), a.roller)
	if err != nil {
		return nil, err
	}
	if len(userBooks) > 0 {
		books = append(books, userBooks...)
	}
	for _, b := range books {
		if err := a.registry.Register(ctx, b); err != nil {
			return nil, apperr.Wrap("app.New", apperr.CodeOf(err),
				"cannot load rulebook "+b.Source(), err)
		}
	}

	// Data packs carry non-rule content (random tables, name lists). They
	// use the same table schema but live outside the rule systems.
	builtinPacks, err := engine.LoadDataPacks(datapacks.FS, a.roller)
	if err != nil {
		return nil, err
	}
	userPacks, err := engine.LoadDataPackDir(cfg.DataPackDir(), a.roller)
	if err != nil {
		return nil, err
	}
	a.packs = append(builtinPacks, userPacks...)
	ok = true
	return a, nil
}

// Config returns the resolved runtime configuration.
func (a *App) Config() *config.Config { return a.cfg }

// Roller returns the generic dice roller.
func (a *App) Roller() *dice.Roller { return a.roller }

// Registry returns the rulebook registry.
func (a *App) Registry() *rules.Registry { return a.registry }

// book returns the compiled rulebook for a system id.
func (a *App) book(systemID string) (*engine.Book, error) {
	sys, err := a.registry.MustGet(systemID)
	if err != nil {
		return nil, err
	}
	b, ok := sys.(*engine.Book)
	if !ok {
		return nil, apperr.New("app.book", apperr.RuleNotFound,
			"rule system "+systemID+" is not an engine rulebook")
	}
	return b, nil
}

// defaultSystemID resolves an empty system id: an explicit default rulebook
// wins, then a single loaded book; otherwise the caller must name a book.
func (a *App) defaultSystemID(systemID string) (string, error) {
	if systemID != "" {
		return systemID, nil
	}
	if id := a.registry.DefaultID(); id != "" {
		return id, nil
	}
	list := a.registry.List()
	if len(list) == 1 {
		return list[0].ID, nil
	}
	if len(list) == 0 {
		return "", apperr.New("app.defaultSystemID", apperr.RuleNotFound, "no rulebooks loaded")
	}
	return "", apperr.New("app.defaultSystemID", apperr.InvalidArgument,
		"multiple rulebooks loaded; specify --system")
}

// selectSystem picks the rulebook for an operation: explicit --system wins,
// then the character's own system; with no character and exactly one
// rulebook loaded, that book is the default.
func (a *App) selectSystem(systemID string, c *character.Character) (*engine.Book, error) {
	if c != nil && systemID == "" {
		systemID = c.SystemID
	}
	id, err := a.defaultSystemID(systemID)
	if err != nil {
		return nil, err
	}
	return a.book(id)
}
