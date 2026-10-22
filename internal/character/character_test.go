package character

import (
	"testing"
)

func validCharacter() *Character {
	return &Character{
		SchemaVersion: SchemaVersion,
		ID:            "c-00000001",
		Name:          "调查员甲",
		SystemID:      "coc7",
		Attributes:    map[string]int{"STR": 50},
		Skills:        map[string]int{"psychology": 60},
		Resources:     map[string]Resource{"san": {Current: 50, Max: 99}},
	}
}

func TestValidateOK(t *testing.T) {
	if err := validCharacter().Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateErrors(t *testing.T) {
	bad := func(mut func(*Character)) *Character {
		c := validCharacter()
		mut(c)
		return c
	}
	cases := []struct {
		name string
		c    *Character
	}{
		{"wrong schema", bad(func(c *Character) { c.SchemaVersion = "9" })},
		{"bad id", bad(func(c *Character) { c.ID = "../evil" })},
		{"empty name", bad(func(c *Character) { c.Name = "" })},
		{"no system", bad(func(c *Character) { c.SystemID = "" })},
		{"negative attr", bad(func(c *Character) { c.Attributes["STR"] = -1 })},
		{"negative skill", bad(func(c *Character) { c.Skills["psychology"] = -5 })},
		{"resource over max", bad(func(c *Character) { c.Resources["hp"] = Resource{Current: 12, Max: 11} })},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.c.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateID(t *testing.T) {
	good := []string{"c-ab12cd34", "a1b2", "x-y-z-1"}
	for _, id := range good {
		if err := ValidateID(id); err != nil {
			t.Errorf("id %q rejected: %v", id, err)
		}
	}
	bad := []string{"", "ab", "ABC", "a/b", "..", "a.b", "a b"}
	for _, id := range bad {
		if err := ValidateID(id); err == nil {
			t.Errorf("id %q should be rejected", id)
		}
	}
}

func TestClone(t *testing.T) {
	c := validCharacter()
	c2 := c.Clone()
	c2.Attributes["STR"] = 99
	c2.Skills["psychology"] = 1
	if c.Attributes["STR"] == 99 {
		t.Fatal("clone shares attributes map")
	}
	if c.Skills["psychology"] == 1 {
		t.Fatal("clone shares skills map")
	}
}
