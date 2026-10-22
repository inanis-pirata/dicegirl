// Package storage defines the persistence contracts of the data root: the
// character cards, the roll statistics, the local state, the current
// encounter and the card decks.
//
// The implementations live in internal/storage/sqlitedb (one transactional
// database). The rules and the data packs are *not* part of this seam: they
// stay YAML files, because they are read once at startup and written by
// hand, not by the dice engine.
package storage

import (
	"context"
	"time"
)

// Deck is one card deck: the full definition and the remaining queue.
type Deck struct {
	Cards     []string `json:"cards"`
	Remaining []string `json:"remaining"`
}

// InitiativeEntry is one entry of the initiative order.
type InitiativeEntry struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

// Stat kinds: what a recorded check was made against.
const (
	StatKindSkill     = "skill"
	StatKindAttribute = "attribute"
	StatKindResource  = "resource"
)

// StatRecord is one recorded check.
type StatRecord struct {
	// CardID is the card the check was made with.
	CardID string
	// Key is the canonical id of the skill, characteristic or resource
	// check (e.g. "psychology", "STR", "sanity").
	Key string
	// Kind is one of StatKindSkill / StatKindAttribute / StatKindResource.
	Kind string
	// Name is the display name of the key.
	Name string
	// Target is the value the roll was compared against.
	Target int
	// Roll is the rolled value.
	Roll int
	// LevelCode is the matched success level (rulebook data).
	LevelCode string
	// Success reports whether the check met the success threshold.
	Success bool
	// Fumble reports whether the check fumbled.
	Fumble bool
	// At is the time of the check.
	At time.Time
}

// StatKeyTotals are the aggregated statistics of one key.
type StatKeyTotals struct {
	Key       string `json:"key"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Checks    int    `json:"checks"`
	Successes int    `json:"successes"`
	Failures  int    `json:"failures"`
	Fumbles   int    `json:"fumbles"`
	RollSum   int    `json:"roll_sum"`
	Best      int    `json:"best"`
	Worst     int    `json:"worst"`
}

// StatHistoryItem is one recent check.
type StatHistoryItem struct {
	At        time.Time `json:"at"`
	Key       string    `json:"key"`
	Target    int       `json:"target"`
	Roll      int       `json:"roll"`
	LevelCode string    `json:"level_code"`
}

// CardStats is the roll statistics of one character card.
type CardStats struct {
	CardID    string            `json:"card_id"`
	Checks    int               `json:"checks"`
	Successes int               `json:"successes"`
	Failures  int               `json:"failures"`
	Fumbles   int               `json:"fumbles"`
	RollSum   int               `json:"roll_sum"`
	Best      int               `json:"best"`
	Worst     int               `json:"worst"`
	Levels    map[string]int    `json:"levels"`
	Keys      []StatKeyTotals   `json:"keys"`
	History   []StatHistoryItem `json:"history"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// StatsStore records the checks made with a card and reports the
// aggregated statistics.
type StatsStore interface {
	// Record adds one check to the card's statistics.
	Record(ctx context.Context, rec StatRecord) error
	// Stats returns the statistics of a card (empty when it has none).
	Stats(ctx context.Context, cardID string) (*CardStats, error)
	// ClearStats removes the statistics of a card.
	ClearStats(ctx context.Context, cardID string) error
	// DeleteStats removes the statistics of a card (used when the card is
	// deleted).
	DeleteStats(ctx context.Context, cardID string) error
}

// StateStore is the local single-user state: the current character, the
// default die size, the pending random-choice options and the preferred
// rulebook for the quick-reference search.
type StateStore interface {
	ActiveID(ctx context.Context) (string, error)
	SetActive(ctx context.Context, id string) error
	DefaultDieSides(ctx context.Context) (int, error)
	SetDefaultDieSides(ctx context.Context, sides int) error
	ChoiceOptions(ctx context.Context) ([]string, error)
	SetChoiceOptions(ctx context.Context, options []string) error
	RulesSystem(ctx context.Context) (string, error)
	SetRulesSystem(ctx context.Context, systemID string) error
}

// EncounterStore is the current encounter's initiative list.
type EncounterStore interface {
	Initiative(ctx context.Context) ([]InitiativeEntry, error)
	SetInitiative(ctx context.Context, entries []InitiativeEntry) error
}

// DeckStore is the collection of card decks.
type DeckStore interface {
	Decks(ctx context.Context) (map[string]*Deck, error)
	SetDecks(ctx context.Context, decks map[string]*Deck) error
}
