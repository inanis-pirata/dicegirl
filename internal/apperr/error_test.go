package apperr

import (
	"errors"
	"fmt"
	"io/fs"
	"testing"
)

func TestNewAndError(t *testing.T) {
	e := New("op.X", InvalidArgument, "bad value")
	if e.Code != InvalidArgument {
		t.Fatalf("code = %q", e.Code)
	}
	if got := e.Error(); got != "INVALID_ARGUMENT [op.X]: bad value" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestWrapAndUnwrap(t *testing.T) {
	base := fs.ErrNotExist
	e := Wrap("storage.Get", NotFound, "character missing", base)
	if !errors.Is(e, fs.ErrNotExist) {
		t.Fatal("wrapped cause not reachable via errors.Is")
	}
	if errors.Unwrap(e) != base {
		t.Fatal("Unwrap mismatch")
	}
}

func TestIsByCode(t *testing.T) {
	e := New("x", ParseError, "boom")
	if !errors.Is(e, &Error{Code: ParseError}) {
		t.Fatal("errors.Is should match by code")
	}
	if errors.Is(e, &Error{Code: NotFound}) {
		t.Fatal("different codes must not match")
	}
}

func TestCodeOf(t *testing.T) {
	if CodeOf(nil) != "" {
		t.Fatal("nil error must yield empty code")
	}
	wrapped := fmt.Errorf("layer: %w", New("x", RollError, "deep"))
	if got := CodeOf(wrapped); got != RollError {
		t.Fatalf("CodeOf wrapped = %q", got)
	}
	if got := CodeOf(errors.New("plain")); got != Internal {
		t.Fatalf("plain error code = %q, want INTERNAL", got)
	}
}
