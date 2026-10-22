package engine_test

import (
	"testing"

	"dicegirl/internal/dice"
	"dicegirl/internal/engine"
	"dicegirl/rulebooks"
)

// coc7Book compiles the embedded CoC rulebook, optionally with a scripted
// roller. These tests prove the rulebook data alone reproduces all behavior
// that used to be hard-coded in a Go package.
func coc7Book(t *testing.T, roller engine.Roller) *engine.Book {
	t.Helper()
	data, err := rulebooks.FS.ReadFile("coc7.yaml")
	if err != nil {
		t.Fatal(err)
	}
	book, err := engine.ParseBook(data, "coc7.yaml", roller)
	if err != nil {
		t.Fatalf("embedded rulebook invalid: %v", err)
	}
	return book
}

func scriptedBook(t *testing.T, values ...int) *engine.Book {
	roller := dice.New(dice.NewScriptedSource(values...))
	return coc7Book(t, roller)
}
