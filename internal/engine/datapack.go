package engine

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"dicegirl/internal/apperr"
	"dicegirl/internal/dice"
)

// A data pack is a compiled package of non-rule content: random tables and
// name sets that belong to no game (random name lists, town tables, ...).
// Rulebooks stay the only place where *rules* live; data packs only carry
// lookup data and use the same table schema.
//
//	schema_version: "1"
//	id: "names"
//	name: "随机姓名"
//	tables:
//	  - id: cn_surname
//	    name: "中文姓氏"
//	    roll: "1d20"
//	    rows:
//	      - { range: "1", text: "赵" }
//	name_sets:
//	  - id: cn
//	    name: "中文姓名"
//	    parts:
//	      - { table: cn_surname }
//	      - { table: cn_given }

// rawDataPack is the YAML shape of a data pack.
type rawDataPack struct {
	SchemaVersion string       `yaml:"schema_version"`
	ID            string       `yaml:"id"`
	Name          string       `yaml:"name"`
	Description   string       `yaml:"description"`
	Tables        []rawTable   `yaml:"tables"`
	NameSets      []rawNameSet `yaml:"name_sets"`
}

type rawNameSet struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	// Default marks the set used when no set id is given.
	Default bool          `yaml:"default"`
	Parts   []rawNamePart `yaml:"parts"`
}

type rawNamePart struct {
	Table string `yaml:"table"`
	Text  string `yaml:"text"`
}

// NamePart is one part of a generated name: a table roll or a literal.
type NamePart struct {
	// TableID is the table to roll (empty for a literal part).
	TableID string
	// Text is the literal text of the part (empty for a table part).
	Text string
}

// NameSet describes how to compose one random name from table rolls.
type NameSet struct {
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	Parts []NamePart `json:"parts"`
	// Default marks the set used when no set id is given.
	Default bool `json:"default"`
}

// DataPack is a compiled data package.
type DataPack struct {
	id       string
	name     string
	source   string
	raw      []byte
	roller   Roller
	tables   *TableSet
	nameSets []NameSet
}

// ID returns the data pack id.
func (d *DataPack) ID() string { return d.id }

// Name returns the display name.
func (d *DataPack) Name() string { return d.name }

// Source returns the file the pack was loaded from.
func (d *DataPack) Source() string { return d.source }

// Raw returns the source bytes of the pack so it can be recompiled.
func (d *DataPack) Raw() []byte { return d.raw }

// Tables returns the compiled tables.
func (d *DataPack) Tables() []TableDef { return d.tables.Tables() }

// NameSets returns the compiled name sets.
func (d *DataPack) NameSets() []NameSet {
	if d == nil {
		return nil
	}
	out := make([]NameSet, len(d.nameSets))
	copy(out, d.nameSets)
	return out
}

// FindNameSet returns the name set with the given id (case-insensitive).
func (d *DataPack) FindNameSet(id string) (NameSet, bool) {
	if d == nil {
		return NameSet{}, false
	}
	key := strings.ToLower(strings.TrimSpace(id))
	for _, ns := range d.nameSets {
		if strings.ToLower(ns.ID) == key {
			return ns, true
		}
	}
	return NameSet{}, false
}

// RollTable rolls one table of the pack.
func (d *DataPack) RollTable(ctx context.Context, id string) (*TableResult, error) {
	if d == nil {
		return nil, apperr.New("engine.DataPack.RollTable", apperr.Internal, "nil data pack")
	}
	roller := d.roller
	if roller == nil {
		roller = dice.Default()
	}
	return d.tables.Roll(ctx, roller, id, d.id)
}

// ParseDataPack decodes and compiles one data pack. Unknown fields are
// rejected so typos fail loudly.
func ParseDataPack(data []byte, source string, roller Roller) (*DataPack, error) {
	if roller == nil {
		roller = dice.Default()
	}
	var raw rawDataPack
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return nil, apperr.Wrap("engine.ParseDataPack", apperr.ConfigInvalid,
			"invalid data pack "+source, err)
	}
	const op = "engine.ParseDataPack"
	if raw.SchemaVersion != SchemaVersion {
		return nil, apperr.New(op, apperr.ConfigInvalid,
			"data pack "+source+" has unsupported schema_version "+raw.SchemaVersion)
	}
	if raw.ID == "" || raw.Name == "" {
		return nil, apperr.New(op, apperr.ConfigInvalid,
			"data pack "+source+" needs an id and a name")
	}
	tables, err := compileTables(op, raw.Tables)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.ConfigInvalid, "invalid data pack "+source, err)
	}
	pack := &DataPack{
		id:     raw.ID,
		name:   raw.Name,
		source: source,
		raw:    append([]byte(nil), data...),
		roller: roller,
		tables: tables,
	}
	seen := map[string]bool{}
	for i, ns := range raw.NameSets {
		if ns.ID == "" || ns.Name == "" {
			return nil, apperr.New(op, apperr.ConfigInvalid,
				fmt.Sprintf("data pack %s name set %d needs an id and a name", raw.ID, i))
		}
		if seen[ns.ID] {
			return nil, apperr.New(op, apperr.ConfigInvalid,
				"duplicate name set "+ns.ID+" in data pack "+raw.ID)
		}
		seen[ns.ID] = true
		if len(ns.Parts) == 0 {
			return nil, apperr.New(op, apperr.ConfigInvalid,
				"name set "+ns.ID+" has no parts")
		}
		compiled := NameSet{ID: ns.ID, Name: ns.Name, Default: ns.Default}
		for j, p := range ns.Parts {
			switch {
			case p.Table != "" && p.Text != "":
				return nil, apperr.New(op, apperr.ConfigInvalid,
					fmt.Sprintf("name set %s part %d sets both table and text", ns.ID, j))
			case p.Table != "":
				if _, ok := tables.Find(p.Table); !ok {
					return nil, apperr.New(op, apperr.ConfigInvalid,
						"name set "+ns.ID+" references unknown table "+p.Table)
				}
				compiled.Parts = append(compiled.Parts, NamePart{TableID: p.Table})
			case p.Text != "":
				compiled.Parts = append(compiled.Parts, NamePart{Text: p.Text})
			default:
				return nil, apperr.New(op, apperr.ConfigInvalid,
					fmt.Sprintf("name set %s part %d is empty", ns.ID, j))
			}
		}
		pack.nameSets = append(pack.nameSets, compiled)
	}
	return pack, nil
}

// LoadDataPacks compiles every *.yaml data pack embedded into the binary.
func LoadDataPacks(fsys fs.FS, roller Roller) ([]*DataPack, error) {
	names, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, apperr.Wrap("engine.LoadDataPacks", apperr.ConfigInvalid,
			"cannot list built-in data packs", err)
	}
	sort.Strings(names)
	packs := make([]*DataPack, 0, len(names))
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, apperr.Wrap("engine.LoadDataPacks", apperr.ConfigInvalid,
				"cannot read built-in data pack "+name, err)
		}
		pack, err := ParseDataPack(data, "builtin:"+name, roller)
		if err != nil {
			return nil, err
		}
		packs = append(packs, pack)
	}
	return packs, nil
}

// LoadDataPackDir compiles user data packs from <data-root>/datapacks/.
// A missing directory yields no packs.
func LoadDataPackDir(dir string, roller Roller) ([]*DataPack, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, apperr.Wrap("engine.LoadDataPackDir", apperr.StorageReadFailed,
			"cannot read data pack directory "+dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) == ".yaml" || filepath.Ext(e.Name()) == ".yml" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	packs := make([]*DataPack, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, apperr.Wrap("engine.LoadDataPackDir", apperr.StorageReadFailed,
				"cannot read data pack "+path, err)
		}
		pack, err := ParseDataPack(data, path, roller)
		if err != nil {
			return nil, err
		}
		packs = append(packs, pack)
	}
	return packs, nil
}
