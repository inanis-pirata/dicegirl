package sqlitedb

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
	"dicegirl/internal/config"
	"dicegirl/internal/storage"
)

// testStore opens a database in a temporary data root.
func testStore(t *testing.T) (*Store, *config.Config) {
	t.Helper()
	cfg := config.Default()
	if err := cfg.Resolve(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	cfg.Backups.Enabled = true
	cfg.Backups.MaxPerCharacter = 2
	s, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, cfg
}

func sampleChar(id string) *character.Character {
	return &character.Character{
		SchemaVersion: character.SchemaVersion,
		ID:            id,
		Name:          "调查员",
		SystemID:      "coc7",
		Attributes:    map[string]int{"STR": 50, "POW": 60},
		Skills:        map[string]int{"psychology": 40},
	}
}

func TestCardCRUD(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	cards := s.Cards()
	c := sampleChar("c-aaaa0001")
	if err := cards.Create(ctx, c); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := cards.Create(ctx, c); !apperr.IsCode(err, apperr.Conflict) {
		t.Fatalf("duplicate create code = %v", apperr.CodeOf(err))
	}
	got, err := cards.Get(ctx, c.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != c.Name || got.Attributes["STR"] != 50 || got.Skills["psychology"] != 40 {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	c2 := sampleChar("c-bbbb0002")
	c2.Name = "乙"
	if err := cards.Create(ctx, c2); err != nil {
		t.Fatal(err)
	}
	list, err := cards.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("List = %v, %v", list, err)
	}
	if list[0].ID != "c-aaaa0001" {
		t.Fatalf("list not sorted: %s", list[0].ID)
	}
	if err := cards.Delete(ctx, c.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := cards.Get(ctx, c.ID); !apperr.IsCode(err, apperr.NotFound) {
		t.Fatalf("Get after delete code = %v", apperr.CodeOf(err))
	}
	if err := cards.Delete(ctx, c.ID); !apperr.IsCode(err, apperr.NotFound) {
		t.Fatalf("Delete missing code = %v", apperr.CodeOf(err))
	}
}

func TestCardIDValidation(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	for _, bad := range []string{"..", "../x", "a/b", `a\b`, "ABC", ".hidden"} {
		c := sampleChar("c-aaaa0001")
		c.ID = bad
		if err := s.Cards().Create(ctx, c); err == nil {
			t.Errorf("id %q accepted", bad)
		}
	}
}

// A corrupt card body is reported as invalid data.
func TestCardCorruptBody(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO cards (id, name, system_id, body, created_at, updated_at)
		 VALUES ('c-corrupt1', 'x', 'coc7', 'name: [unterminated', 'now', 'now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Cards().Get(ctx, "c-corrupt1")
	if !apperr.IsCode(err, apperr.InvalidData) {
		t.Fatalf("corrupt body code = %v", apperr.CodeOf(err))
	}
}

// The card backups keep the configured number of previous versions.
func TestCardBackups(t *testing.T) {
	s, cfg := testStore(t)
	ctx := context.Background()
	cards := s.Cards()
	c := sampleChar("c-backup001")
	if err := cards.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		c.Name = "v" + string(rune('0'+i))
		if err := cards.Update(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM card_backups WHERE card_id = ?`, c.ID).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != cfg.Backups.MaxPerCharacter {
		t.Fatalf("backups kept = %d, want %d", n, cfg.Backups.MaxPerCharacter)
	}
}

func TestStateRoundtrip(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	state := s.State()
	id, err := state.ActiveID(ctx)
	if err != nil || id != "" {
		t.Fatalf("initial active = %q, %v", id, err)
	}
	if err := state.SetActive(ctx, "c-aaaa0001"); err != nil {
		t.Fatal(err)
	}
	if id, _ = state.ActiveID(ctx); id != "c-aaaa0001" {
		t.Fatalf("active = %q", id)
	}
	if err := state.SetDefaultDieSides(ctx, 20); err != nil {
		t.Fatal(err)
	}
	if sides, _ := state.DefaultDieSides(ctx); sides != 20 {
		t.Fatalf("die sides = %d", sides)
	}
	if err := state.SetChoiceOptions(ctx, []string{"苹果", "香蕉"}); err != nil {
		t.Fatal(err)
	}
	opts, err := state.ChoiceOptions(ctx)
	if err != nil || len(opts) != 2 || opts[0] != "苹果" {
		t.Fatalf("choice options = %v, %v", opts, err)
	}
	if err := state.SetChoiceOptions(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if opts, _ = state.ChoiceOptions(ctx); len(opts) != 0 {
		t.Fatalf("choice options not cleared: %v", opts)
	}
}

// The statistics aggregate the checks per key and keep a bounded history.
func TestStatsAggregation(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	cards := s.Cards()
	if err := cards.Create(ctx, sampleChar("c-stats001")); err != nil {
		t.Fatal(err)
	}
	stats := s.Stats()
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		rec := storage.StatRecord{
			CardID:    "c-stats001",
			Key:       "psychology",
			Kind:      storage.StatKindSkill,
			Name:      "心理学",
			Target:    60,
			Roll:      30 + i,
			LevelCode: "regular",
			Success:   true,
			At:        now,
		}
		if err := stats.Record(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	// A fumble on another key.
	if err := stats.Record(ctx, storage.StatRecord{
		CardID: "c-stats001", Key: "STR", Kind: storage.StatKindAttribute, Name: "力量",
		Target: 50, Roll: 100, LevelCode: "fumble", Fumble: true, At: now,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := stats.Stats(ctx, "c-stats001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Checks != 4 || got.Successes != 3 || got.Failures != 1 || got.Fumbles != 1 {
		t.Fatalf("totals = %+v", got)
	}
	if got.RollSum != 30+31+32+100 || got.Best != 100 || got.Worst != 30 {
		t.Fatalf("roll aggregates = %+v", got)
	}
	if got.Levels["regular"] != 3 || got.Levels["fumble"] != 1 {
		t.Fatalf("levels = %v", got.Levels)
	}
	if len(got.Keys) != 2 {
		t.Fatalf("keys = %+v", got.Keys)
	}
	if len(got.History) != 4 {
		t.Fatalf("history = %d entries, want 4", len(got.History))
	}
	// The history is bounded.
	for i := 0; i < MaxHistoryPerCard+10; i++ {
		if err := stats.Record(ctx, storage.StatRecord{
			CardID: "c-stats001", Key: "psychology", Kind: storage.StatKindSkill, Name: "心理学",
			Target: 60, Roll: 40, LevelCode: "regular", Success: true, At: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	got, err = stats.Stats(ctx, "c-stats001")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.History) != MaxHistoryPerCard {
		t.Fatalf("history = %d entries, want %d", len(got.History), MaxHistoryPerCard)
	}
	// Clearing resets the counters.
	if err := stats.ClearStats(ctx, "c-stats001"); err != nil {
		t.Fatal(err)
	}
	got, err = stats.Stats(ctx, "c-stats001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Checks != 0 || len(got.History) != 0 {
		t.Fatalf("stats not cleared: %+v", got)
	}
}

// Recording for an unknown card is an error, not a silent orphan row.
func TestStatsUnknownCard(t *testing.T) {
	s, _ := testStore(t)
	err := s.Stats().Record(context.Background(), storage.StatRecord{
		CardID: "c-nope0000", Key: "x", Kind: storage.StatKindSkill,
	})
	if !apperr.IsCode(err, apperr.NotFound) {
		t.Fatalf("code = %v, want NOT_FOUND", apperr.CodeOf(err))
	}
}

// The legacy YAML files are imported once and then moved aside.
func TestLegacyMigration(t *testing.T) {
	cfg := config.Default()
	if err := cfg.Resolve(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	// Write a legacy card and a legacy state file.
	if err := os.MkdirAll(cfg.CharacterDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	legacyCard := "schema_version: \"1\"\nid: c-legacy01\nname: 旧卡\nsystem_id: coc7\n" +
		"attributes:\n    STR: 50\n"
	if err := os.WriteFile(filepath.Join(cfg.CharacterDir(), "c-legacy01.yaml"), []byte(legacyCard), 0o600); err != nil {
		t.Fatal(err)
	}
	legacyState := "schema_version: \"1\"\nactive_character_id: c-legacy01\ndefault_die_sides: 20\n"
	if err := os.WriteFile(filepath.Join(cfg.DataRoot, "state.yaml"), []byte(legacyState), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	got, err := s.Cards().Get(ctx, "c-legacy01")
	if err != nil {
		t.Fatalf("legacy card not imported: %v", err)
	}
	if got.Name != "旧卡" || got.Attributes["STR"] != 50 {
		t.Fatalf("imported card = %+v", got)
	}
	if id, _ := s.State().ActiveID(ctx); id != "c-legacy01" {
		t.Fatalf("active = %q", id)
	}
	if sides, _ := s.State().DefaultDieSides(ctx); sides != 20 {
		t.Fatalf("die sides = %d", sides)
	}
	// The legacy files are moved into the migrated folder.
	if _, err := os.Stat(filepath.Join(cfg.DataRoot, "legacy", "characters", "c-legacy01.yaml")); err != nil {
		t.Fatalf("legacy card not archived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataRoot, "state.yaml")); !os.IsNotExist(err) {
		t.Fatalf("legacy state file still in place: %v", err)
	}
	// Opening again does not import a second time.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	list, err := again.Cards().List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("second open imported again: %v %v", list, err)
	}
}

// TestMigrateTablesV2FromOldSchema opens a database shaped exactly like a
// pre-tables deployment (encounter_entries and decks without table_id /
// discard) and also pre-creates the empty `tables` table that a partially
// failed open leaves behind (the CREATE TABLE commits before the index step
// aborts). Opening must succeed and move existing rows onto the default
// table.
func TestMigrateTablesV2FromOldSchema(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	if err := cfg.Resolve(root); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", "file:"+cfg.DatabaseFile())
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
CREATE TABLE encounter_entries (
	seq   INTEGER PRIMARY KEY AUTOINCREMENT,
	name  TEXT NOT NULL,
	value INTEGER NOT NULL
);
CREATE TABLE decks (
	name      TEXT PRIMARY KEY,
	cards     TEXT NOT NULL,
	remaining TEXT NOT NULL
);
CREATE TABLE state (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE tables (
	id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, system_id TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
INSERT INTO encounter_entries (name, value) VALUES ('哥布林', 18);
INSERT INTO decks (name, cards, remaining) VALUES ('命运', '["凶","吉"]', '["吉"]');
INSERT INTO state (key, value) VALUES ('active_character_id', 'c-old1');
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open old database failed: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	var tableID, name string
	var value int
	err = s.db.QueryRowContext(ctx,
		`SELECT table_id, name, value FROM encounter_entries`).Scan(&tableID, &name, &value)
	if err != nil {
		t.Fatalf("encounter row: %v", err)
	}
	if tableID != "default" || name != "哥布林" || value != 18 {
		t.Fatalf("encounter migrated wrong: %q %q %d", tableID, name, value)
	}

	var tid, cards, remaining, discard string
	err = s.db.QueryRowContext(ctx,
		`SELECT table_id, cards, remaining, discard FROM decks WHERE name = '命运'`).
		Scan(&tid, &cards, &remaining, &discard)
	if err != nil {
		t.Fatalf("deck row: %v", err)
	}
	if tid != "default" || cards != `["凶","吉"]` || remaining != `["吉"]` || discard != "[]" {
		t.Fatalf("deck migrated wrong: %q %q %q %q", tid, cards, remaining, discard)
	}

	var active string
	if err := s.db.QueryRowContext(ctx,
		`SELECT value FROM state WHERE key = 'active_character_id:default'`).Scan(&active); err != nil {
		t.Fatalf("namespaced active card: %v", err)
	}
	if active != "c-old1" {
		t.Fatalf("active pointer = %q", active)
	}

	var defaultName, systemID string
	if err := s.db.QueryRowContext(ctx,
		`SELECT name, system_id FROM tables WHERE id = 'default'`).Scan(&defaultName, &systemID); err != nil {
		t.Fatalf("seed table: %v", err)
	}
	if defaultName != "默认桌" {
		t.Fatalf("seed name = %q", defaultName)
	}

	// The post-migration index exists.
	var idx int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_encounter_table'`).
		Scan(&idx); err != nil || idx != 1 {
		t.Fatalf("encounter index missing: %v", err)
	}

	// Reopening is idempotent.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(cfg)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	_ = again.Close()
}
