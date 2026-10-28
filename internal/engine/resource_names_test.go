package engine_test

import (
	"strings"
	"testing"

	"dicegirl/internal/character"
	"dicegirl/internal/engine"
	"dicegirl/rulebooks"
)

// The resource rung of the name-resolution ladder: vitals names and aliases,
// the raw track id and the resource_check label all map to the track id.
func TestResolveResource(t *testing.T) {
	b := coc7Book(t, nil)
	cases := map[string]string{
		"理智":  "san",
		"理智值": "san",
		"san": "san",
		"SAN": "san",
		"体力":  "hp",
		"生命值": "hp",
		"hp":  "hp",
		"魔法值": "mp",
	}
	for name, want := range cases {
		if got, ok := b.ResolveResource(name); !ok || got != want {
			t.Errorf("ResolveResource(%q) = %q,%v; want %q,true", name, got, ok, want)
		}
	}
	if got, ok := b.ResolveResource("会计"); ok {
		t.Errorf("ResolveResource(会计) = %q,true; want not found", got)
	}
	if got := b.ResourceDisplayName("san"); got != "理智" {
		t.Errorf("ResourceDisplayName(san) = %q, want 理智", got)
	}
	if got := b.ResourceDisplayName("hp"); got != "体力" {
		t.Errorf("ResourceDisplayName(hp) = %q, want 体力", got)
	}
}

// A trackless sheet falls back to the declared formula chain: the resource
// check's fallback (san: POW), then the vitals initial formula (hp).
func TestResourceFallbackTarget(t *testing.T) {
	b := coc7Book(t, nil)
	c := &character.Character{Attributes: map[string]int{
		"STR": 50, "CON": 60, "SIZ": 70, "DEX": 55, "POW": 55, "EDU": 70,
	}}
	if v, err := b.ResourceFallbackTarget(c, "san"); err != nil || v != 55 {
		t.Fatalf("san fallback = %d,%v; want 55,nil", v, err)
	}
	if v, err := b.ResourceFallbackTarget(c, "hp"); err != nil || v != 13 {
		t.Fatalf("hp fallback = %d,%v; want 13,nil", v, err)
	}
	if v, err := b.ResourceFallbackTarget(c, "mp"); err != nil || v != 11 {
		t.Fatalf("mp fallback = %d,%v; want 11,nil (vitals initial POW/5)", v, err)
	}
	if _, err := b.ResourceFallbackTarget(c, "nonexistent"); err == nil {
		t.Fatal("unknown resource must error")
	}
}

// The chat-verb bindings are rulebook data: sc/ti/li address whatever the
// book declares, and unknown verbs report unbound.
func TestCommandBindings(t *testing.T) {
	b := coc7Book(t, nil)
	cases := map[string]engine.BindingInfo{
		"sc": {Verb: "sc", Kind: engine.BindingKindResourceCheck, Target: "sanity"},
		"ti": {Verb: "ti", Kind: engine.BindingKindTable, Target: "temporary_insanity"},
		"li": {Verb: "li", Kind: engine.BindingKindTable, Target: "summary_insanity"},
	}
	for verb, want := range cases {
		got, ok := b.Binding(verb)
		if !ok || got != want {
			t.Errorf("Binding(%q) = %+v,%v; want %+v,true", verb, got, ok, want)
		}
	}
	if _, ok := b.Binding("en"); ok {
		t.Error("Binding(en) must be unbound")
	}
}

// Bindings that reference undeclared targets must fail at load time.
func TestCommandBindingValidation(t *testing.T) {
	data, err := rulebooks.FS.ReadFile("coc7.yaml")
	if err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(string(data),
		"sc: { resource_check: sanity }", "sc: { resource_check: nope }", 1)
	if _, err := engine.ParseBook([]byte(bad), "bad.yaml", nil); err == nil {
		t.Fatal("unknown resource_check binding must fail to load")
	}
	bad = strings.Replace(string(data),
		"ti: { table: temporary_insanity }", "ti: { table: nope }", 1)
	if _, err := engine.ParseBook([]byte(bad), "bad.yaml", nil); err == nil {
		t.Fatal("unknown table binding must fail to load")
	}
}

// Two vitals names resolving to different tracks are a config error.
func TestDuplicateResourceName(t *testing.T) {
	data, err := rulebooks.FS.ReadFile("coc7.yaml")
	if err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(string(data),
		`aliases: ["理智值"]`, `aliases: ["体力"]`, 1)
	if _, err := engine.ParseBook([]byte(bad), "bad.yaml", nil); err == nil {
		t.Fatal("duplicate resource name must fail to load")
	}
}
