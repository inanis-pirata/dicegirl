package engine_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"dicegirl/internal/apperr"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine"
	"dicegirl/rulebooks"
)

// A table roll picks the row whose range covers the roll and returns the
// row text.
func TestTableRoll(t *testing.T) {
	b := scriptedBook(t, 2) // 1d10 -> 3
	res, err := b.RollTable(context.Background(), "temporary_insanity")
	if err != nil {
		t.Fatal(err)
	}
	if res.TableID != "temporary_insanity" || res.Value != 3 {
		t.Fatalf("table=%s value=%d", res.TableID, res.Value)
	}
	if !strings.Contains(res.Text, "昏厥") {
		t.Fatalf("unexpected row text: %q", res.Text)
	}
	if res.Kind != "table_roll" || res.Row != 3 {
		t.Fatalf("kind=%s row=%d", res.Kind, res.Row)
	}
}

// Row texts may embed sub-roll placeholders: they are rolled when the row
// is hit and replaced by their total.
func TestTableSubRoll(t *testing.T) {
	// 1d10 -> 10 (summary insanity row 10 has a "{1d10}" placeholder),
	// then the sub-roll 1d10 -> 4.
	b := scriptedBook(t, 9, 3)
	res, err := b.RollTable(context.Background(), "summary_insanity")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "昏迷后恢复") {
		t.Fatalf("unexpected row text: %q", res.Text)
	}
	if !strings.Contains(res.Text, "4 小时") {
		t.Fatalf("sub-roll placeholder not substituted: %q", res.Text)
	}
	if len(res.SubRolls) != 1 || res.SubRolls[0].Total != 4 {
		t.Fatalf("sub-rolls = %+v", res.SubRolls)
	}
}

// Unknown tables are a usage error.
func TestTableUnknownID(t *testing.T) {
	b := coc7Book(t, nil)
	_, err := b.RollTable(context.Background(), "nope")
	if !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("code = %v, want INVALID_ARGUMENT", err)
	}
}

// Table declarations are validated at load time: the rows must cover the
// roll's domain without overlap.
func TestTableConfigValidation(t *testing.T) {
	cases := []struct {
		name   string
		marker string
		repl   string
	}{
		{
			name:   "gap in rows",
			marker: `      - { range: "1", text: "失忆：清醒后完全不记得刚才发生了什么" }`,
			repl:   `      - { range: "1-2", text: "失忆" }`,
		},
		{
			name:   "overlapping rows",
			marker: `      - { range: "2", text: "暴力：攻击周围最近的人或物" }`,
			repl:   `      - { range: "1-3", text: "暴力" }`,
		},
		{
			name:   "row outside the domain",
			marker: `      - { range: "10", text: "幻觉：看到或听到不存在的事物" }`,
			repl:   `      - { range: "11", text: "幻觉" }`,
		},
		{
			name:   "invalid row range",
			marker: `      - { range: "10", text: "幻觉：看到或听到不存在的事物" }`,
			repl:   `      - { range: "x", text: "幻觉" }`,
		},
		{
			name:   "invalid sub-roll",
			marker: `      - { range: "10", text: "幻觉：看到或听到不存在的事物" }`,
			repl:   `      - { range: "10", text: "幻觉 {1d}" }`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := rulebooks.FS.ReadFile("coc7.yaml")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte(tc.marker)) {
				t.Fatalf("marker missing: %q", tc.marker)
			}
			variant := bytes.Replace(data, []byte(tc.marker), []byte(tc.repl), 1)
			_, err = engine.ParseBook(variant, "variant.yaml", nil)
			if !apperr.IsCode(err, apperr.ConfigInvalid) {
				t.Fatalf("code = %v, want CONFIG_INVALID", err)
			}
		})
	}
}

// A default row may cover the values the explicit rows leave out.
func TestTableDefaultRow(t *testing.T) {
	data, err := rulebooks.FS.ReadFile("coc7.yaml")
	if err != nil {
		t.Fatal(err)
	}
	marker := `      - { range: "10", text: "幻觉：看到或听到不存在的事物" }`
	variant := bytes.Replace(data, []byte(marker),
		[]byte(`      - { range: "default", text: "其它症状" }`), 1)
	b, perr := engine.ParseBook(variant, "variant.yaml", dice.New(dice.NewScriptedSource(9)))
	if perr != nil {
		t.Fatal(perr)
	}
	res, err := b.RollTable(context.Background(), "temporary_insanity")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "其它症状" {
		t.Fatalf("default row not used: %q", res.Text)
	}
	if res.Range != "default" {
		t.Fatalf("range = %q, want default", res.Range)
	}
}

// The rulebook tables are listed with their domain.
func TestTableList(t *testing.T) {
	b := coc7Book(t, nil)
	tables := b.Tables()
	if len(tables) != 4 {
		t.Fatalf("tables = %d, want 4", len(tables))
	}
	if tables[0].ID != "temporary_insanity" || tables[0].Min != 1 || tables[0].Max != 10 {
		t.Fatalf("unexpected table: %+v", tables[0])
	}
}
