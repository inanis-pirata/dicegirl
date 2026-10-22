// Package config resolves dicegirl runtime configuration from defaults,
// environment variables and CLI overrides.
//
// Precedence (highest first): explicit CLI flag, DICEGIRL_* environment
// variables, built-in defaults. The default data root is "./data" relative
// to the working directory; this fixed rule is documented in README and
// docs/user-manual.md so platform adapters know where state lives.
package config

import (
	"os"
	"path/filepath"
	"strconv"

	"dicegirl/internal/apperr"
)

// Environment variable names.
const (
	EnvDataRoot = "DICEGIRL_DATA_ROOT"
	EnvBackups  = "DICEGIRL_BACKUPS"
	EnvMaxKeep  = "DICEGIRL_BACKUP_KEEP"
)

// Defaults.
const (
	DefaultDataRoot   = "data"
	DefaultBackupKeep = 10
)

// BackupConfig controls timestamped backups made before overwriting a file.
type BackupConfig struct {
	// Enabled toggles pre-update backups.
	Enabled bool `yaml:"enabled"`
	// MaxPerCharacter is the number of backups retained per character;
	// older backups are pruned. 0 means the built-in default is used.
	MaxPerCharacter int `yaml:"max_per_character"`
}

// Config is the resolved runtime configuration.
type Config struct {
	// DataRoot is the directory holding characters/, games/, backups/ and
	// state.yaml. It is always an absolute cleaned path after Resolve.
	DataRoot string
	// Backups controls file backup behavior.
	Backups BackupConfig
}

// Default returns the built-in configuration without consulting the
// environment.
func Default() *Config {
	return &Config{
		DataRoot: DefaultDataRoot,
		Backups: BackupConfig{
			Enabled:         true,
			MaxPerCharacter: DefaultBackupKeep,
		},
	}
}

// FromEnv returns a Default config overridden by DICEGIRL_* environment
// variables. Invalid values produce typed errors.
func FromEnv() (*Config, error) {
	cfg := Default()
	if v := os.Getenv(EnvDataRoot); v != "" {
		cfg.DataRoot = v
	}
	if v := os.Getenv(EnvBackups); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, apperr.Wrap("config.FromEnv", apperr.InvalidArgument,
				EnvBackups+" must be true or false", err)
		}
		cfg.Backups.Enabled = b
	}
	if v := os.Getenv(EnvMaxKeep); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil, apperr.New("config.FromEnv", apperr.InvalidArgument,
				EnvMaxKeep+" must be a non-negative integer")
		}
		cfg.Backups.MaxPerCharacter = n
	}
	return cfg, nil
}

// Resolve finalizes configuration: applies a CLI data-root override, makes
// the path absolute and cleaned, and fills fallback backup settings. It does
// not create directories; the storage layer owns directory creation.
func (c *Config) Resolve(cliDataRoot string) error {
	if c == nil {
		return apperr.New("config.Resolve", apperr.Internal, "nil config")
	}
	if cliDataRoot != "" {
		c.DataRoot = cliDataRoot
	}
	if c.DataRoot == "" {
		c.DataRoot = DefaultDataRoot
	}
	abs, err := filepath.Abs(filepath.Clean(c.DataRoot))
	if err != nil {
		return apperr.Wrap("config.Resolve", apperr.InvalidArgument,
			"cannot resolve data root path", err)
	}
	c.DataRoot = abs
	if c.Backups.MaxPerCharacter <= 0 {
		c.Backups.MaxPerCharacter = DefaultBackupKeep
	}
	return nil
}

// CharacterDir returns the characters directory.
func (c *Config) CharacterDir() string {
	return filepath.Join(c.DataRoot, "characters")
}

// GameDir returns the game saves directory (reserved for a later phase).
func (c *Config) GameDir() string {
	return filepath.Join(c.DataRoot, "games")
}

// RulebookDir returns the user-provided rulebooks directory. Every *.yaml
// (or *.yml) file there is one complete rule system, using the same schema
// as the built-in books.
func (c *Config) RulebookDir() string {
	return filepath.Join(c.DataRoot, "rulebooks")
}

// DataPackDir returns the user-provided data packs directory. Data packs
// carry non-rule content (random tables, name lists) and use the same
// table schema as the rulebooks.
func (c *Config) DataPackDir() string {
	return filepath.Join(c.DataRoot, "datapacks")
}

// BackupDir returns the backup root directory.
func (c *Config) BackupDir() string {
	return filepath.Join(c.DataRoot, "backups")
}

// StateFile returns the path of the local active-character state file.
func (c *Config) StateFile() string {
	return filepath.Join(c.DataRoot, "state.yaml")
}

// EncounterFile returns the path of the current encounter's initiative list.
func (c *Config) EncounterFile() string {
	return filepath.Join(c.DataRoot, "encounter.yaml")
}

// DeckFile returns the path of the card-deck collection.
func (c *Config) DeckFile() string {
	return filepath.Join(c.DataRoot, "decks.yaml")
}

// DatabaseFile returns the path of the SQLite database that holds the
// writable state (character cards, roll statistics, local state, the current
// encounter and the card decks).
func (c *Config) DatabaseFile() string {
	return filepath.Join(c.DataRoot, "dicegirl.db")
}
