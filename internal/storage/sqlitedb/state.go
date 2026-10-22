package sqlitedb

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"

	"gopkg.in/yaml.v3"

	"dicegirl/internal/apperr"
	"dicegirl/internal/storage"
)

// The local state is a small key/value table: the current character, the
// default die size and the pending random-choice options.
const (
	stateKeyActive   = "active_character_id"
	stateKeyDieSides = "default_die_sides"
	stateKeyChoice   = "choice_options"
	stateKeyRules    = "rules_system"
)

// State implements storage.StateStore on top of the database.
type State struct {
	store *Store
}

// State returns the local state store of the database.
func (s *Store) State() *State { return &State{store: s} }

// ActiveID returns the current character id (empty when none is selected).
func (st *State) ActiveID(ctx context.Context) (string, error) {
	value, _, err := st.get(ctx, stateKeyActive)
	return value, err
}

// SetActive records the current character; an empty id clears it.
func (st *State) SetActive(ctx context.Context, id string) error {
	return st.set(ctx, stateKeyActive, id)
}

// DefaultDieSides returns the default die size (0 when unset).
func (st *State) DefaultDieSides(ctx context.Context) (int, error) {
	value, _, err := st.get(ctx, stateKeyDieSides)
	if err != nil || value == "" {
		return 0, err
	}
	n, cerr := strconv.Atoi(value)
	if cerr != nil {
		return 0, nil
	}
	return n, nil
}

// SetDefaultDieSides stores the default die size.
func (st *State) SetDefaultDieSides(ctx context.Context, sides int) error {
	return st.set(ctx, stateKeyDieSides, strconv.Itoa(sides))
}

// ChoiceOptions returns the pending random-choice options.
func (st *State) ChoiceOptions(ctx context.Context) ([]string, error) {
	value, ok, err := st.get(ctx, stateKeyChoice)
	if err != nil || !ok || value == "" {
		return nil, err
	}
	var options []string
	if err := json.Unmarshal([]byte(value), &options); err != nil {
		return nil, nil
	}
	return options, nil
}

// SetChoiceOptions stores the pending random-choice options.
func (st *State) SetChoiceOptions(ctx context.Context, options []string) error {
	if len(options) == 0 {
		return st.delete(ctx, stateKeyChoice)
	}
	data, err := json.Marshal(options)
	if err != nil {
		return apperr.Wrap("sqlitedb.State.SetChoiceOptions", apperr.Internal,
			"cannot encode choice options", err)
	}
	return st.set(ctx, stateKeyChoice, string(data))
}

// RulesSystem returns the rulebook preferred by the quick-reference
// search (empty when none is preferred).
func (st *State) RulesSystem(ctx context.Context) (string, error) {
	value, _, err := st.get(ctx, stateKeyRules)
	return value, err
}

// SetRulesSystem stores the rulebook preferred by the quick-reference
// search; an empty id clears the preference.
func (st *State) SetRulesSystem(ctx context.Context, systemID string) error {
	if systemID == "" {
		return st.delete(ctx, stateKeyRules)
	}
	return st.set(ctx, stateKeyRules, systemID)
}

// get reads one state value.
func (st *State) get(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := st.store.db.QueryRowContext(ctx, `SELECT value FROM state WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, apperr.Wrap("sqlitedb.State.get", apperr.StorageReadFailed, "cannot read state", err)
	}
	return value, true, nil
}

// set writes one state value.
func (st *State) set(ctx context.Context, key, value string) error {
	_, err := st.store.db.ExecContext(ctx,
		`INSERT INTO state (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return apperr.Wrap("sqlitedb.State.set", apperr.StorageWriteFailed, "cannot write state", err)
	}
	return nil
}

// delete removes one state value.
func (st *State) delete(ctx context.Context, key string) error {
	_, err := st.store.db.ExecContext(ctx, `DELETE FROM state WHERE key = ?`, key)
	if err != nil {
		return apperr.Wrap("sqlitedb.State.delete", apperr.StorageWriteFailed, "cannot write state", err)
	}
	return nil
}

// Encounter implements storage.EncounterStore: the initiative list of the
// current encounter.
type Encounter struct {
	store *Store
}

// Encounter returns the encounter store of the database.
func (s *Store) Encounter() *Encounter { return &Encounter{store: s} }

// Initiative returns the initiative entries in the order they were added.
func (e *Encounter) Initiative(ctx context.Context) ([]storage.InitiativeEntry, error) {
	const op = "sqlitedb.Encounter.Initiative"
	rows, err := e.store.db.QueryContext(ctx, `SELECT name, value FROM encounter_entries ORDER BY seq`)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read encounter", err)
	}
	defer rows.Close()
	out := []storage.InitiativeEntry{}
	for rows.Next() {
		var entry storage.InitiativeEntry
		if err := rows.Scan(&entry.Name, &entry.Value); err != nil {
			return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read encounter", err)
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read encounter", err)
	}
	return out, nil
}

// SetInitiative replaces the initiative list.
func (e *Encounter) SetInitiative(ctx context.Context, entries []storage.InitiativeEntry) error {
	const op = "sqlitedb.Encounter.SetInitiative"
	return e.store.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM encounter_entries`); err != nil {
			return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot write encounter", err)
		}
		for _, entry := range entries {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO encounter_entries (name, value) VALUES (?, ?)`, entry.Name, entry.Value); err != nil {
				return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot write encounter", err)
			}
		}
		return nil
	})
}

// Deck implements storage.DeckStore: the collection of card decks.
type Deck struct {
	store *Store
}

// Decks returns the deck store of the database.
func (s *Store) Decks() *Deck { return &Deck{store: s} }

// Decks returns every deck by name.
func (d *Deck) Decks(ctx context.Context) (map[string]*storage.Deck, error) {
	const op = "sqlitedb.Deck.Decks"
	rows, err := d.store.db.QueryContext(ctx, `SELECT name, cards, remaining FROM decks`)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read decks", err)
	}
	defer rows.Close()
	out := map[string]*storage.Deck{}
	for rows.Next() {
		var name, cards, remaining string
		if err := rows.Scan(&name, &cards, &remaining); err != nil {
			return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read decks", err)
		}
		deck := &storage.Deck{}
		if err := json.Unmarshal([]byte(cards), &deck.Cards); err != nil {
			return nil, apperr.Wrap(op, apperr.InvalidData, "invalid deck "+name, err)
		}
		if err := json.Unmarshal([]byte(remaining), &deck.Remaining); err != nil {
			return nil, apperr.Wrap(op, apperr.InvalidData, "invalid deck "+name, err)
		}
		out[name] = deck
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(op, apperr.StorageReadFailed, "cannot read decks", err)
	}
	return out, nil
}

// SetDecks replaces the whole deck collection.
func (d *Deck) SetDecks(ctx context.Context, decks map[string]*storage.Deck) error {
	const op = "sqlitedb.Deck.SetDecks"
	return d.store.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM decks`); err != nil {
			return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot write decks", err)
		}
		for name, deck := range decks {
			cards, err := json.Marshal(deck.Cards)
			if err != nil {
				return apperr.Wrap(op, apperr.Internal, "cannot encode deck", err)
			}
			remaining, err := json.Marshal(deck.Remaining)
			if err != nil {
				return apperr.Wrap(op, apperr.Internal, "cannot encode deck", err)
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO decks (name, cards, remaining) VALUES (?, ?, ?)`,
				name, string(cards), string(remaining)); err != nil {
				return apperr.Wrap(op, apperr.StorageWriteFailed, "cannot write decks", err)
			}
		}
		return nil
	})
}

// importLegacyDocument imports one of the legacy YAML documents.
func (s *Store) importLegacyDocument(ctx context.Context, name string, data []byte) error {
	switch name {
	case "state.yaml":
		var st struct {
			ActiveCharacterID string   `yaml:"active_character_id"`
			DefaultDieSides   int      `yaml:"default_die_sides"`
			ChoiceOptions     []string `yaml:"choice_options"`
		}
		if err := yamlUnmarshal(data, &st); err != nil {
			return nil // a broken legacy file is skipped, not fatal
		}
		state := s.State()
		if st.ActiveCharacterID != "" {
			_ = state.SetActive(ctx, st.ActiveCharacterID)
		}
		if st.DefaultDieSides != 0 {
			_ = state.SetDefaultDieSides(ctx, st.DefaultDieSides)
		}
		if len(st.ChoiceOptions) > 0 {
			_ = state.SetChoiceOptions(ctx, st.ChoiceOptions)
		}
	case "encounter.yaml":
		var enc struct {
			Entries []storage.InitiativeEntry `yaml:"entries"`
		}
		if err := yamlUnmarshal(data, &enc); err != nil {
			return nil
		}
		_ = s.Encounter().SetInitiative(ctx, enc.Entries)
	case "decks.yaml":
		var set struct {
			Decks map[string]*storage.Deck `yaml:"decks"`
		}
		if err := yamlUnmarshal(data, &set); err != nil {
			return nil
		}
		_ = s.Decks().SetDecks(ctx, set.Decks)
	}
	return nil
}

// yamlUnmarshal decodes YAML leniently (the legacy files are best effort).
func yamlUnmarshal(data []byte, out any) error {
	return yaml.Unmarshal(data, out)
}
