package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bot.log")
	w, err := newRotatingWriter(path, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	first := strings.Repeat("a", 60)
	second := strings.Repeat("b", 60)
	if _, err := w.Write([]byte(first)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(second)); err != nil { // exceeds 100: rotate first
		t.Fatal(err)
	}

	cur, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(cur) != second {
		t.Fatalf("current = %q", cur)
	}
	old, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("rotated file missing: %v", err)
	}
	if string(old) != first {
		t.Fatalf("rotated = %q", old)
	}
}

func TestRotatingWriterKeepsBoundedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bot.log")
	w, err := newRotatingWriter(path, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for i := 0; i < 10; i++ {
		if _, err := w.Write([]byte("0123456789")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("kept too many rotations: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("missing rotation .1: %v", err)
	}
}

func TestNewLoggerWritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", "bot.log")
	logger, closer, err := newLogger(&botConfig{LogFile: path})
	if err != nil {
		t.Fatal(err)
	}
	logger.Print("hello")
	_ = closer.Close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "hello") {
		t.Fatalf("log file = %q", raw)
	}
}
