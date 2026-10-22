package engine_test

import (
	"testing"

	"dicegirl/internal/character"
)

func derivedCharacter() *character.Character {
	return &character.Character{
		Attributes: map[string]int{
			"STR": 50, "CON": 60, "SIZ": 70, "DEX": 55,
			"APP": 60, "INT": 70, "POW": 60, "EDU": 70,
		},
	}
}

func TestDerive(t *testing.T) {
	b := coc7Book(t, nil)
	d, err := b.Derive(derivedCharacter())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"build":        0, // STR+SIZ = 120 -> row 85..124
		"max_hp":       13,
		"max_mp":       12,
		"initial_san":  99,
		"initial_luck": 99,
		"mov":          7, // STR<SIZ and DEX<SIZ
	}
	for id, v := range want {
		got, ok := d.Int(id)
		if !ok {
			t.Errorf("derived %s missing", id)
			continue
		}
		if got != v {
			t.Errorf("derived %s = %d, want %d", id, got, v)
		}
	}

	// db is a dice-expression derived value, not an integer one.
	var dbExpr string
	for _, v := range d.Values {
		if v.ID == "db" {
			dbExpr = v.Expr
		}
	}
	if dbExpr != "0" {
		t.Errorf("db expr = %q, want 0", dbExpr)
	}

	vitals := map[string][2]int{}
	for _, vv := range d.Vitals {
		vitals[vv.Resource] = [2]int{vv.Current, vv.Max}
	}
	if vitals["hp"] != [2]int{13, 13} {
		t.Errorf("hp vitals = %v", vitals["hp"])
	}
	if vitals["mp"] != [2]int{12, 12} {
		t.Errorf("mp vitals = %v", vitals["mp"])
	}
	if vitals["san"] != [2]int{99, 99} {
		t.Errorf("san vitals = %v", vitals["san"])
	}

	if len(d.InitAttributes) != 1 || d.InitAttributes[0].Attribute != "LUCK" || d.InitAttributes[0].Value != 99 {
		t.Fatalf("init attributes = %+v", d.InitAttributes)
	}
}

func TestDeriveMOVBranches(t *testing.T) {
	b := coc7Book(t, nil)
	cases := []struct {
		name          string
		str, siz, dex int
		want          int
	}{
		{"slow", 50, 70, 55, 7},
		{"fast", 80, 70, 75, 9},
		// STR >= SIZ but DEX < SIZ: neither MOV-7 nor MOV-9 case matches.
		{"average", 80, 70, 55, 8},
	}
	for _, tc := range cases {
		c := derivedCharacter()
		c.Attributes["STR"], c.Attributes["SIZ"], c.Attributes["DEX"] = tc.str, tc.siz, tc.dex
		d, err := b.Derive(c)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := d.Int("mov")
		if got != tc.want {
			t.Errorf("%s: mov = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestResourceMax(t *testing.T) {
	b := coc7Book(t, nil)
	max, ok, err := b.ResourceMax(derivedCharacter(), "hp")
	if err != nil || !ok || max != 13 {
		t.Fatalf("max hp = %d ok=%v err=%v", max, ok, err)
	}
	if _, ok, _ := b.ResourceMax(derivedCharacter(), "nope"); ok {
		t.Fatal("unknown resource reported")
	}
}

func TestDeriveRequiresCharacter(t *testing.T) {
	b := coc7Book(t, nil)
	var c *character.Character
	if _, err := b.Derive(c); err == nil {
		t.Fatal("nil character accepted")
	}
}
