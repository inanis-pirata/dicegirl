// Package storage defines the persistence contracts of the data root: the
// character cards, the roll statistics, the local state, the current
// encounter and the card decks. It also holds the game-affairs metadata
// (`game_meta`) and play-session logs (`.log`).
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

// Deck is one card deck: the full definition, the drawable queue and the
// discard pile (cards already drawn). Remaining + Discard together always
// contain the same multiset as Cards.
type Deck struct {
	Cards     []string `json:"cards"`
	Remaining []string `json:"remaining"`
	Discard   []string `json:"discard,omitempty"`
}

// GameTable is one game ("游戏"/"团") inside a group data root. Game state
// (active character, encounter, decks) is keyed by ID; cards stay shared
// across games.
type GameTable struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	SystemID  string    `json:"system_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GameMember is one participant of a game: a GM or a player. ID is the
// adapter's stable member identifier (a QQ member_openid in the group
// adapter); Name is an optional human-readable label.
type GameMember struct {
	ID    string    `json:"id"`
	Name  string    `json:"name,omitempty"`
	Since time.Time `json:"since,omitempty"`
}

// GameRoulette is the anti-reroll bag of one die size. Remaining holds the
// values still in the bag, Drawn the values already taken this round. When
// Remaining is empty the bag is refilled with 1..Sides, Copies times each.
type GameRoulette struct {
	Sides     int   `json:"sides"`
	Copies    int   `json:"copies"`
	Remaining []int `json:"remaining"`
	Drawn     []int `json:"drawn,omitempty"`
}

// GameMeta is the game-affairs state attached to one game table: the GM and
// player rosters, arbitrary GM-owned variables, the open/closed flag and the
// optional roulette die. A missing row means a fresh, open game with empty
// rosters and variables.
type GameMeta struct {
	GMs      []GameMember      `json:"gms,omitempty"`
	Players  []GameMember      `json:"players,omitempty"`
	Vars     map[string]string `json:"vars,omitempty"`
	Closed   bool              `json:"closed,omitempty"`
	Roulette *GameRoulette     `json:"roulette,omitempty"`
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

// StateStore is the local single-user state: the current table and
// character, the default die size, the pending random-choice options and the
// preferred rulebook for the quick-reference search. The active character is
// namespaced per table when the context carries one.
type StateStore interface {
	ActiveID(ctx context.Context) (string, error)
	SetActive(ctx context.Context, id string) error
	ActiveTable(ctx context.Context) (string, error)
	SetActiveTable(ctx context.Context, id string) error
	DefaultDieSides(ctx context.Context) (int, error)
	SetDefaultDieSides(ctx context.Context, sides int) error
	ChoiceOptions(ctx context.Context) ([]string, error)
	SetChoiceOptions(ctx context.Context, options []string) error
	RulesSystem(ctx context.Context) (string, error)
	SetRulesSystem(ctx context.Context, systemID string) error
}

// TableStore is the collection of game tables inside one data root.
type TableStore interface {
	Tables(ctx context.Context) ([]GameTable, error)
	TableByID(ctx context.Context, id string) (*GameTable, error)
	TableByName(ctx context.Context, name string) (*GameTable, error)
	CreateTable(ctx context.Context, t *GameTable) error
	SetTableSystem(ctx context.Context, id, systemID string) error
	DeleteTable(ctx context.Context, id string) error
}

// GameStore is the game-affairs metadata of each table: GM/player rosters,
// variables, open/closed status and the roulette die.
type GameStore interface {
	// GameMeta returns one table's metadata; a missing row yields an empty,
	// open GameMeta (never nil).
	GameMeta(ctx context.Context, tableID string) (*GameMeta, error)
	// SetGameMeta replaces one table's metadata.
	SetGameMeta(ctx context.Context, tableID string, meta *GameMeta) error
	// DeleteGameMeta removes one table's metadata (`.game over`).
	DeleteGameMeta(ctx context.Context, tableID string) error
}

// EncounterStore is the current encounter's initiative list. Each table has
// its own encounter.
type EncounterStore interface {
	Initiative(ctx context.Context, tableID string) ([]InitiativeEntry, error)
	SetInitiative(ctx context.Context, tableID string, entries []InitiativeEntry) error
}

// DeckStore is the per-table collection of card decks.
type DeckStore interface {
	Decks(ctx context.Context, tableID string) (map[string]*Deck, error)
	SetDecks(ctx context.Context, tableID string, decks map[string]*Deck) error
}

// Log formats and statuses. A log is scoped to one data root (one group in
// the chat adapters, one local data directory in the CLI).
const (
	LogFormatTXT  = "txt"
	LogFormatHTML = "html"

	LogStatusActive = "active"
	LogStatusPaused = "paused"
	LogStatusEnded  = "ended"
	LogStatusHalted = "halted"
)

// LogEntryKind classifies one recorded log line. "say" is ordinary speech,
// "command" is an executed chat command plus its rendered result, and
// "system" is an application notice.
const (
	LogEntrySay     = "say"
	LogEntryCommand = "command"
	LogEntrySystem  = "system"
)

// LogEntry is one recorded message. Result holds the rendered command output;
// Text holds the user's original line (or the speech itself).
type LogEntry struct {
	Seq       int64     `json:"seq,omitempty"`
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"`
	SpeakerID string    `json:"speaker_id,omitempty"`
	Speaker   string    `json:"speaker,omitempty"`
	Text      string    `json:"text"`
	Result    string    `json:"result,omitempty"`
	ImageURL  string    `json:"image_url,omitempty"`
}

// LogSession is one play-session log. ActiveSince marks the start of the
// current recording span (zero while paused or ended). TimerSince marks the
// start of the current timed span (zero when timing is off). Accrued is the
// time accumulated by previous timed spans.
type LogSession struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	CreatorID   string        `json:"creator_id,omitempty"`
	CreatorName string        `json:"creator_name,omitempty"`
	Status      string        `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
	ActiveSince time.Time     `json:"active_since,omitempty"`
	TimerSince  time.Time     `json:"timer_since,omitempty"`
	Accrued     time.Duration `json:"accrued,omitempty"`
	EndedAt     time.Time     `json:"ended_at,omitempty"`
	Entries     int           `json:"entries"`
}

// LogSettings are the per-data-root log preferences.
type LogSettings struct {
	Format string `json:"format"`
	Timer  bool   `json:"timer"`
}

// LogStore persists play-session logs together with their entries.
type LogStore interface {
	// LogSettings returns the data root's log preferences, applying the
	// defaults (txt format, timer off) when nothing is stored.
	LogSettings(ctx context.Context) (*LogSettings, error)
	// SetLogSettings replaces the log preferences.
	SetLogSettings(ctx context.Context, settings *LogSettings) error
	// CurrentLog returns the in-progress log (active or paused), or nil.
	CurrentLog(ctx context.Context) (*LogSession, error)
	// LogByID returns one log, or nil when it does not exist.
	LogByID(ctx context.Context, id string) (*LogSession, error)
	// LogByName returns the newest log with the exact name, or nil.
	LogByName(ctx context.Context, name string) (*LogSession, error)
	// Logs returns every log, newest first.
	Logs(ctx context.Context) ([]*LogSession, error)
	// CreateLog stores a new session.
	CreateLog(ctx context.Context, log *LogSession) error
	// UpdateLog replaces a session's metadata. The entry count is owned by
	// AppendLogEntry and is not overwritten here.
	UpdateLog(ctx context.Context, log *LogSession) error
	// DeleteLog removes a session and its entries.
	DeleteLog(ctx context.Context, id string) error
	// AppendLogEntry records one entry and increments the session's count.
	AppendLogEntry(ctx context.Context, logID string, entry *LogEntry) error
	// LogEntries returns a session's entries in recording order.
	LogEntries(ctx context.Context, logID string) ([]LogEntry, error)
}
