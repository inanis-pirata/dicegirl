package config

import (
	"path/filepath"
	"testing"

	"dicegirl/internal/apperr"
)

func TestDefaultAndResolve(t *testing.T) {
	cfg := Default()
	if err := cfg.Resolve(""); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !filepath.IsAbs(cfg.DataRoot) {
		t.Fatalf("DataRoot should be absolute, got %q", cfg.DataRoot)
	}
	if cfg.Backups.MaxPerCharacter != DefaultBackupKeep {
		t.Fatalf("backup keep = %d", cfg.Backups.MaxPerCharacter)
	}
}

func TestResolveOverride(t *testing.T) {
	cfg := Default()
	if err := cfg.Resolve(filepath.Join(t.TempDir(), "custom-root")); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if filepath.Base(cfg.DataRoot) != "custom-root" {
		t.Fatalf("DataRoot = %q", cfg.DataRoot)
	}
}

func TestResolveFillsZeroKeep(t *testing.T) {
	cfg := Default()
	cfg.Backups.MaxPerCharacter = 0
	if err := cfg.Resolve(""); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.Backups.MaxPerCharacter != DefaultBackupKeep {
		t.Fatalf("keep = %d, want default %d", cfg.Backups.MaxPerCharacter, DefaultBackupKeep)
	}
}

func TestFromEnvErrors(t *testing.T) {
	t.Setenv(EnvBackups, "maybe")
	if _, err := FromEnv(); !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("err code = %v, want INVALID_ARGUMENT", apperr.CodeOf(err))
	}
}
