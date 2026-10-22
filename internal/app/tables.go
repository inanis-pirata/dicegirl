package app

import (
	"context"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/engine"
)

// RollTable rolls a random table declared by a rulebook. Tables are game
// content (CoC7 insanity tables and friends); they need no character, only
// a rulebook to read them from.
func (a *App) RollTable(ctx context.Context, systemID, tableID string) (*engine.TableResult, error) {
	book, _, err := a.skillContext(ctx, systemID, "")
	if err != nil {
		return nil, err
	}
	return book.RollTable(ctx, tableID)
}

// ListTables returns the random tables declared by a rulebook.
func (a *App) ListTables(ctx context.Context, systemID string) ([]engine.TableDef, error) {
	book, _, err := a.skillContext(ctx, systemID, "")
	if err != nil {
		return nil, err
	}
	return book.Tables(), nil
}

// DataPacks returns the loaded non-rule data packages.
func (a *App) DataPacks() []*engine.DataPack { return a.packs }

// RollDataTable rolls a table of a data package (non-rule content such as
// the random name lists).
func (a *App) RollDataTable(ctx context.Context, packID, tableID string) (*engine.TableResult, error) {
	pack, err := a.dataPack(packID)
	if err != nil {
		return nil, err
	}
	return pack.RollTable(ctx, tableID)
}

// dataPack resolves a data pack by id (empty means the only loaded one).
func (a *App) dataPack(packID string) (*engine.DataPack, error) {
	const op = "app.dataPack"
	if len(a.packs) == 0 {
		return nil, apperr.New(op, apperr.RuleNotFound, "no data packs loaded")
	}
	if packID == "" {
		if len(a.packs) > 1 {
			return nil, apperr.New(op, apperr.InvalidArgument,
				"multiple data packs loaded; specify the pack id")
		}
		return a.packs[0], nil
	}
	for _, p := range a.packs {
		if strings.EqualFold(p.ID(), packID) {
			return p, nil
		}
	}
	return nil, apperr.New(op, apperr.RuleNotFound, "unknown data pack: "+packID)
}

// NameResult is the structured outcome of a random-name request.
type NameResult struct {
	Kind    string                `json:"kind"`
	SetID   string                `json:"set_id"`
	SetName string                `json:"set_name"`
	Names   []string              `json:"names"`
	Rolls   []*engine.TableResult `json:"rolls,omitempty"`
}

// RandomNames generates n random names from the named set of a data pack.
// The set is addressed by its id (cn/jp/en); an empty id uses the only
// name set of the only loaded pack.
func (a *App) RandomNames(ctx context.Context, setID string, n int) (*NameResult, error) {
	const op = "app.RandomNames"
	if n < 1 {
		return nil, apperr.New(op, apperr.InvalidArgument, "name count must be at least 1")
	}
	if n > MaxRandomNames {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"name count must not exceed "+strconv.Itoa(MaxRandomNames))
	}
	set, pack, err := a.nameSet(setID)
	if err != nil {
		return nil, err
	}
	res := &NameResult{Kind: "random_names", SetID: set.ID, SetName: set.Name}
	for i := 0; i < n; i++ {
		var sb strings.Builder
		for _, part := range set.Parts {
			if part.TableID == "" {
				sb.WriteString(part.Text)
				continue
			}
			tr, err := pack.RollTable(ctx, part.TableID)
			if err != nil {
				return nil, err
			}
			res.Rolls = append(res.Rolls, tr)
			sb.WriteString(tr.Text)
		}
		res.Names = append(res.Names, sb.String())
	}
	return res, nil
}

// nameSet resolves a name set by id across the loaded data packs. An empty
// id selects the set marked as default (or the only declared set).
func (a *App) nameSet(setID string) (engine.NameSet, *engine.DataPack, error) {
	const op = "app.nameSet"
	var found []engine.NameSet
	var owners []*engine.DataPack
	for _, p := range a.packs {
		for _, ns := range p.NameSets() {
			if setID == "" || strings.EqualFold(ns.ID, setID) {
				found = append(found, ns)
				owners = append(owners, p)
			}
		}
	}
	if setID == "" {
		// Prefer the set explicitly marked as default.
		defaults := make([]int, 0, len(found))
		for i, ns := range found {
			if ns.Default {
				defaults = append(defaults, i)
			}
		}
		switch len(defaults) {
		case 1:
			i := defaults[0]
			return found[i], owners[i], nil
		case 0:
			if len(found) == 1 {
				return found[0], owners[0], nil
			}
		default:
			return engine.NameSet{}, nil, apperr.New(op, apperr.InvalidArgument,
				"several name sets are marked as default")
		}
		return engine.NameSet{}, nil, apperr.New(op, apperr.RuleNotFound,
			"no default name set is declared by the loaded data packs")
	}
	switch len(found) {
	case 0:
		return engine.NameSet{}, nil, apperr.New(op, apperr.InvalidArgument,
			"unknown name set: "+setID)
	case 1:
		return found[0], owners[0], nil
	default:
		return engine.NameSet{}, nil, apperr.New(op, apperr.InvalidArgument,
			"multiple name sets match "+setID+"; specify a unique id")
	}
}

// MaxRandomNames bounds one random-name request.
const MaxRandomNames = 20
