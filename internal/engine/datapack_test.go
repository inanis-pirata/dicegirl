package engine_test

import (
	"context"
	"strings"
	"testing"

	"dicegirl/datapacks"
	"dicegirl/internal/apperr"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine"
)

// The built-in data packs load with the same strict validation as user
// packs and declare the random name sets.
func TestBuiltinDataPacks(t *testing.T) {
	packs, err := engine.LoadDataPacks(datapacks.FS, dice.New(dice.NewScriptedSource(1)))
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) == 0 {
		t.Fatal("no built-in data packs loaded")
	}
	var names *engine.DataPack
	for _, p := range packs {
		if p.ID() == "names" {
			names = p
		}
	}
	if names == nil {
		t.Fatalf("built-in names pack missing: %d packs", len(packs))
	}
	for _, id := range []string{"cn", "jp", "en"} {
		ns, ok := names.FindNameSet(id)
		if !ok {
			t.Fatalf("name set %q missing", id)
		}
		if len(ns.Parts) == 0 {
			t.Fatalf("name set %q has no parts", id)
		}
	}
}

// A name set composes one name from its table rolls.
func TestDataPackNameSetRoll(t *testing.T) {
	pack, err := engine.ParseDataPack([]byte(namePackYAML), "names.yaml",
		dice.New(dice.NewScriptedSource(0, 1)))
	if err != nil {
		t.Fatal(err)
	}
	ns, ok := pack.FindNameSet("cn")
	if !ok {
		t.Fatal("name set cn missing")
	}
	var sb strings.Builder
	for _, part := range ns.Parts {
		if part.TableID == "" {
			sb.WriteString(part.Text)
			continue
		}
		res, err := pack.RollTable(context.Background(), part.TableID)
		if err != nil {
			t.Fatal(err)
		}
		sb.WriteString(res.Text)
	}
	if sb.String() != "王明" {
		t.Fatalf("name = %q, want 王明", sb.String())
	}
}

// Data packs are validated at load time.
func TestDataPackValidation(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"missing id", strings.Replace(namePackYAML, `id: "names"`, "", 1)},
		{"unknown table in name set", strings.Replace(namePackYAML, "table: surname", "table: nope", 1)},
		{"empty part", strings.Replace(namePackYAML, "- { table: surname }", "- { }", 1)},
		{"part with both", strings.Replace(namePackYAML, "- { table: surname }", "- { table: surname, text: \"x\" }", 1)},
		{"table without rows", strings.Replace(namePackYAML,
			`    rows:
      - { range: "1", text: "王" }`, "    rows: []", 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := engine.ParseDataPack([]byte(tc.yaml), "bad.yaml", nil)
			if !apperr.IsCode(err, apperr.ConfigInvalid) {
				t.Fatalf("code = %v, want CONFIG_INVALID", err)
			}
		})
	}
}

// namePackYAML is a minimal data pack used by the tests above.
const namePackYAML = `schema_version: "1"
id: "names"
name: "测试姓名"
description: "test"
tables:
  - id: surname
    name: "姓"
    roll: "1d2"
    rows:
      - { range: "1", text: "王" }
      - { range: "2", text: "李" }
  - id: given
    name: "名"
    roll: "1d2"
    rows:
      - { range: "1", text: "小" }
      - { range: "2", text: "明" }
name_sets:
  - id: cn
    name: "中文姓名"
    parts:
      - { table: surname }
      - { table: given }
`
