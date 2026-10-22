package rules

import (
	"context"
	"testing"

	"dicegirl/internal/apperr"
)

type stubSystem struct {
	manifest Manifest
	validErr error
}

func (s stubSystem) Manifest() Manifest                   { return s.manifest }
func (s stubSystem) ValidateConfig(context.Context) error { return s.validErr }

func validManifest(id string) Manifest {
	return Manifest{
		SchemaVersion: "1",
		ID:            id,
		Name:          "Stub " + id,
		SystemVersion: "1.2.3",
		RulesVersion:  "test",
	}
}

func TestRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(context.Background(), stubSystem{manifest: validManifest("coc7")}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, ok := r.Get("coc7"); !ok {
		t.Fatal("Get coc7 not found")
	}
	if _, err := r.MustGet("nope"); !apperr.IsCode(err, apperr.RuleNotFound) {
		t.Fatalf("MustGet code = %v", apperr.CodeOf(err))
	}
}

func TestRegisterDuplicate(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()
	if err := r.Register(ctx, stubSystem{manifest: validManifest("coc7")}); err != nil {
		t.Fatal(err)
	}
	err := r.Register(ctx, stubSystem{manifest: validManifest("coc7")})
	if !apperr.IsCode(err, apperr.Conflict) {
		t.Fatalf("duplicate code = %v", apperr.CodeOf(err))
	}
}

func TestRegisterInvalidManifest(t *testing.T) {
	cases := []Manifest{
		{},
		{SchemaVersion: "2", ID: "x", Name: "x", SystemVersion: "1.0.0", RulesVersion: "r"},
		{SchemaVersion: "1", ID: "BadID", Name: "x", SystemVersion: "1.0.0", RulesVersion: "r"},
		{SchemaVersion: "1", ID: "ok", Name: "x", SystemVersion: "v1", RulesVersion: "r"},
		{SchemaVersion: "1", ID: "ok", Name: "x", SystemVersion: "1.0.0", RulesVersion: ""},
	}
	for i, m := range cases {
		r := NewRegistry()
		if err := r.Register(context.Background(), stubSystem{manifest: m}); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestListSorted(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()
	for _, id := range []string{"zzz", "abc", "mmm"} {
		if err := r.Register(ctx, stubSystem{manifest: validManifest(id)}); err != nil {
			t.Fatal(err)
		}
	}
	list := r.List()
	if len(list) != 3 || list[0].ID != "abc" || list[2].ID != "zzz" {
		t.Fatalf("list not sorted: %+v", list)
	}
}

func TestDefaultID(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()
	if got := r.DefaultID(); got != "" {
		t.Fatalf("empty registry default = %q", got)
	}
	first := validManifest("coc7")
	first.Default = true
	if err := r.Register(ctx, stubSystem{manifest: first}); err != nil {
		t.Fatal(err)
	}
	if got := r.DefaultID(); got != "coc7" {
		t.Fatalf("default = %q, want coc7", got)
	}
	// A later book that declares the default wins (user books load after
	// the built-ins).
	second := validManifest("dnd5e")
	second.Default = true
	if err := r.Register(ctx, stubSystem{manifest: second}); err != nil {
		t.Fatal(err)
	}
	if got := r.DefaultID(); got != "dnd5e" {
		t.Fatalf("default = %q, want dnd5e", got)
	}
}
