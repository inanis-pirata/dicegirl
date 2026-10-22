package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine"
	"dicegirl/internal/rules"
)

// Assignment is one user-provided key=value pair (attribute or skill).
type Assignment struct {
	Key   string
	Value int
}

// ParseAssignment parses "KEY=VALUE" (or "KEY:VALUE") with basic whitespace
// tolerance. The colon form is the Dice! `.st 力量:50` synonym.
func ParseAssignment(s string) (Assignment, error) {
	idx := strings.IndexAny(s, "=:")
	if idx <= 0 || idx == len(s)-1 {
		return Assignment{}, apperr.New("app.ParseAssignment", apperr.InvalidArgument,
			"assignment must look like KEY=VALUE (or KEY:VALUE), got "+s)
	}
	key := strings.TrimSpace(s[:idx])
	raw := strings.TrimSpace(s[idx+1:])
	v, err := strconv.Atoi(raw)
	if err != nil {
		return Assignment{}, apperr.Wrap("app.ParseAssignment", apperr.InvalidArgument,
			"value for "+key+" must be an integer", err)
	}
	return Assignment{Key: key, Value: v}, nil
}

// CreateCharacter creates, validates and persists a new character.
// Assignment keys are resolved through the rule system: known characteristics
// become attributes, known skills skills. If makeActive, it becomes the active
// character in local state.
//
// The owning rulebook's character rules finish the fresh sheet right away:
// the derived initial resource tracks (HP/MP/SAN, ...) and missing initial
// attributes are recorded exactly as after `dv --init`. A hand-built card may
// omit characteristics the derived formulas need; the card itself is valid,
// so it is kept with a nil derived result and the caller can offer `dv
// --init` once the values are filled in (rolled generation always provides
// the full characteristic set).
func (a *App) CreateCharacter(ctx context.Context, systemID, name string, sets []Assignment, makeActive bool) (*character.Character, *engine.DerivedResult, error) {
	const op = "app.CreateCharacter"
	systemID, err := a.defaultSystemID(systemID)
	if err != nil {
		return nil, nil, err
	}
	sys, err := a.registry.MustGet(systemID)
	if err != nil {
		return nil, nil, err
	}

	c := &character.Character{
		SchemaVersion: character.SchemaVersion,
		Name:          strings.TrimSpace(name),
		SystemID:      sys.Manifest().ID,
		Attributes:    map[string]int{},
		Skills:        map[string]int{},
	}
	if err := a.assign(sys, c, sets); err != nil {
		return nil, nil, err
	}

	// Generate a fresh unique ID, then validate the complete sheet.
	for range 5 {
		id, err := newCharacterID()
		if err != nil {
			return nil, nil, err
		}
		c.ID = id
		if err := c.Validate(); err != nil {
			return nil, nil, err
		}
		if err := a.validateBySystem(ctx, sys, c); err != nil {
			return nil, nil, err
		}
		if err := a.chars.Create(ctx, c); err != nil {
			if apperr.IsCode(err, apperr.Conflict) {
				continue
			}
			return nil, nil, err
		}
		if makeActive {
			if err := a.state.SetActive(ctx, c.ID); err != nil {
				return nil, nil, err
			}
		}
		d, err := a.deriveForNewCard(ctx, c)
		if err != nil {
			return nil, nil, err
		}
		return c, d, nil
	}
	return nil, nil, apperr.New(op, apperr.Internal, "could not allocate a unique character id")
}

// deriveForNewCard finishes a freshly created sheet with the owning
// rulebook's derived character rules. A nil result means the sheet is too
// incomplete for the derived formulas yet (typically a hand-built card with
// missing characteristics); the card is already stored and stays valid.
func (a *App) deriveForNewCard(ctx context.Context, c *character.Character) (*engine.DerivedResult, error) {
	book, err := a.selectSystem(ctx, "", c)
	if err != nil {
		return nil, err
	}
	d, err := book.Derive(c)
	if err != nil {
		return nil, nil
	}
	if err := a.applyDerived(ctx, book, c, d, false); err != nil {
		return nil, err
	}
	return d, nil
}

// GetCharacter loads a character by explicit ID, or the active character
// when id is empty.
func (a *App) GetCharacter(ctx context.Context, id string) (*character.Character, error) {
	if id == "" {
		active, err := a.state.ActiveID(ctx)
		if err != nil {
			return nil, err
		}
		if active == "" {
			return nil, apperr.New("app.GetCharacter", apperr.InvalidArgument,
				"no character id given and no active character (use 'dicegirl character use <id>')")
		}
		id = active
	}
	return a.chars.Get(ctx, id)
}

// ListCharacters returns all stored characters.
func (a *App) ListCharacters(ctx context.Context) ([]*character.Character, error) {
	return a.chars.List(ctx)
}

// DeleteCharacter removes a character and clears active state if needed.
func (a *App) DeleteCharacter(ctx context.Context, id string) error {
	if err := a.chars.Delete(ctx, id); err != nil {
		return err
	}
	active, err := a.state.ActiveID(ctx)
	if err != nil {
		return err
	}
	if active == id {
		return a.state.SetActive(ctx, "")
	}
	return nil
}

// CopyCharacter duplicates a sheet under a new ID and name.
func (a *App) CopyCharacter(ctx context.Context, id, newName string, makeActive bool) (*character.Character, error) {
	src, err := a.chars.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	cp := src.Clone()
	cp.Name = strings.TrimSpace(newName)
	cp.ID = ""
	cp.CreatedAt = time.Time{} // storage stamps creation on write
	cp.UpdatedAt = time.Time{}
	for range 5 {
		newID, err := newCharacterID()
		if err != nil {
			return nil, err
		}
		cp.ID = newID
		if err := cp.Validate(); err != nil {
			return nil, err
		}
		if err := a.chars.Create(ctx, cp); err != nil {
			if apperr.IsCode(err, apperr.Conflict) {
				continue
			}
			return nil, err
		}
		if makeActive {
			if err := a.state.SetActive(ctx, cp.ID); err != nil {
				return nil, err
			}
		}
		return cp, nil
	}
	return nil, apperr.New("app.CopyCharacter", apperr.Internal, "could not allocate a unique character id")
}

// SetValues resolves and applies key=value updates to an existing sheet.
// An empty id addresses the active character.
func (a *App) SetValues(ctx context.Context, id string, sets []Assignment) (*character.Character, error) {
	c, err := a.GetCharacter(ctx, id)
	if err != nil {
		return nil, err
	}
	sys, err := a.registry.MustGet(c.SystemID)
	if err != nil {
		return nil, err
	}
	if err := a.assign(sys, c, sets); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := a.validateBySystem(ctx, sys, c); err != nil {
		return nil, err
	}
	if err := a.chars.Update(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Adjustment is one KEY(+|-)VALUE change, where VALUE is an integer or a
// pure dice expression (e.g. "san-1d6" or "STR+1"). The sign is parsed
// from the shape; the delta is rolled once by AdjustValues.
type Adjustment struct {
	Key  string
	Sign int // +1 or -1
	Expr string
}

// ParseAdjustment parses "san-1d6", "hp+1" or "STR-3". The right-hand side
// must be a valid pure dice expression (an integer counts).
func ParseAdjustment(s string) (Adjustment, error) {
	for i := 1; i < len(s); i++ {
		if s[i] != '+' && s[i] != '-' {
			continue
		}
		key := strings.TrimSpace(s[:i])
		expr := strings.TrimSpace(s[i+1:])
		if key == "" || expr == "" {
			break
		}
		sign := +1
		if s[i] == '-' {
			sign = -1
		}
		if _, _, err := dice.Parse(expr, dice.DefaultLimits()); err != nil {
			return Adjustment{}, apperr.Wrap("app.ParseAdjustment", apperr.InvalidArgument,
				"adjustment for "+key+" needs an integer or dice expression after '"+string(s[i])+"'", err)
		}
		return Adjustment{Key: key, Sign: sign, Expr: expr}, nil
	}
	return Adjustment{}, apperr.New("app.ParseAdjustment", apperr.InvalidArgument,
		"adjustment must look like KEY+N or KEY-N (N an integer or dice expression), got "+s)
}

// AdjustmentResult reports one applied KEY±expression change.
type AdjustmentResult struct {
	Key       string `json:"key"`
	Canonical string `json:"canonical"`
	Kind      string `json:"kind"` // attribute | skill | resource
	Expr      string `json:"expr"`
	Delta     int    `json:"delta"`
	Before    int    `json:"before"`
	After     int    `json:"after"`
}

// AdjustValues applies KEY±VALUE changes to the given sheet (the active
// sheet when id is empty): the right-hand dice expression is rolled once
// per key against the app roller, then the change goes through the same
// validation, backup and atomic-update path as SetValues. Attribute/skill
// keys resolve through the rulebook; resource track keys (san/hp/mp)
// match exactly or case-insensitively.
func (a *App) AdjustValues(ctx context.Context, id string, adjs []Adjustment) (*character.Character, []AdjustmentResult, error) {
	const op = "app.AdjustValues"
	if len(adjs) == 0 {
		return nil, nil, apperr.New(op, apperr.InvalidArgument, "no adjustments given")
	}
	c, err := a.GetCharacter(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	sys, err := a.registry.MustGet(c.SystemID)
	if err != nil {
		return nil, nil, err
	}
	resolver, ok := sys.(KeyResolver)
	if !ok {
		return nil, nil, apperr.New(op, apperr.RuleNotFound,
			"rule system "+c.SystemID+" does not support keyed adjustments")
	}

	results := make([]AdjustmentResult, 0, len(adjs))
	for _, adj := range adjs {
		roll, err := a.roller.Roll(ctx, adj.Expr)
		if err != nil {
			return nil, nil, err
		}
		delta := adj.Sign * roll.Total

		res := AdjustmentResult{Key: adj.Key, Expr: adj.Expr, Delta: delta}
		switch canonical, found := resolver.ResolveAttribute(adj.Key); {
		case found:
			current, exists := c.Attributes[canonical]
			if !exists {
				return nil, nil, apperr.New(op, apperr.InvalidArgument,
					"character is missing characteristic "+canonical)
			}
			res.Kind, res.Canonical, res.Before = "attribute", canonical, current
			c.Attributes[canonical] = current + delta
			res.After = c.Attributes[canonical]
		default:
			if canonical, found := resolver.ResolveSkill(adj.Key); found {
				current, exists := c.Skills[canonical]
				if !exists {
					return nil, nil, apperr.New(op, apperr.InvalidArgument,
						"character has no value for skill "+canonical)
				}
				res.Kind, res.Canonical, res.Before = "skill", canonical, current
				c.Skills[canonical] = current + delta
				res.After = c.Skills[canonical]
			} else if track, exists := findResource(c.Resources, adj.Key); exists {
				current := c.Resources[track]
				res.Kind, res.Canonical, res.Before = "resource", track, current.Current
				current.Current = current.Current + delta
				c.Resources[track] = current
				res.After = current.Current
			} else {
				return nil, nil, apperr.New(op, apperr.InvalidArgument,
					"unknown characteristic, skill or resource: "+adj.Key)
			}
		}
		results = append(results, res)
	}

	if err := c.Validate(); err != nil {
		return nil, nil, err
	}
	if err := a.validateBySystem(ctx, sys, c); err != nil {
		return nil, nil, err
	}
	if err := a.chars.Update(ctx, c); err != nil {
		return nil, nil, err
	}
	return c, results, nil
}

// findResource resolves a resource-track key exactly, then
// case-insensitively (players type SAN/HP/MP while tracks are san/hp/mp).
func findResource(tracks map[string]character.Resource, key string) (string, bool) {
	if _, ok := tracks[key]; ok {
		return key, true
	}
	for track := range tracks {
		if strings.EqualFold(track, key) {
			return track, true
		}
	}
	return "", false
}

// SetActiveCharacter marks id as the active character.
func (a *App) SetActiveCharacter(ctx context.Context, id string) error {
	if _, err := a.chars.Get(ctx, id); err != nil {
		return err
	}
	return a.state.SetActive(ctx, id)
}

// ActiveCharacterID returns the active character ID (may be empty).
func (a *App) ActiveCharacterID(ctx context.Context) (string, error) {
	return a.state.ActiveID(ctx)
}

// ValueResult describes one sheet value addressed by a user-typed key.
type ValueResult struct {
	Key       string `json:"key"`
	Canonical string `json:"canonical"`
	Kind      string `json:"kind"` // attribute | skill | resource
	Value     int    `json:"value"`
	Max       int    `json:"max,omitempty"`
}

// ShowValue resolves one key against the sheet (the active sheet when id is
// empty) and reports its value. The resolution order is the rulebook's:
// characteristic, then skill, then resource track.
func (a *App) ShowValue(ctx context.Context, id, key string) (*ValueResult, error) {
	const op = "app.ShowValue"
	c, err := a.GetCharacter(ctx, id)
	if err != nil {
		return nil, err
	}
	sys, err := a.registry.MustGet(c.SystemID)
	if err != nil {
		return nil, err
	}
	resolver, ok := sys.(KeyResolver)
	if !ok {
		return nil, apperr.New(op, apperr.RuleNotFound,
			"rule system "+c.SystemID+" does not support keyed lookups")
	}
	if canonical, found := resolver.ResolveAttribute(key); found {
		v, exists := c.Attributes[canonical]
		if !exists {
			return nil, apperr.New(op, apperr.InvalidArgument,
				"character is missing characteristic "+canonical)
		}
		return &ValueResult{Key: key, Canonical: canonical, Kind: "attribute", Value: v}, nil
	}
	if canonical, found := resolver.ResolveSkill(key); found {
		v, exists := c.Skills[canonical]
		if !exists {
			return nil, apperr.New(op, apperr.InvalidArgument,
				"character has no value for skill "+canonical)
		}
		return &ValueResult{Key: key, Canonical: canonical, Kind: "skill", Value: v}, nil
	}
	if track, exists := findResource(c.Resources, key); exists {
		r := c.Resources[track]
		return &ValueResult{Key: key, Canonical: track, Kind: "resource", Value: r.Current, Max: r.Max}, nil
	}
	return nil, apperr.New(op, apperr.InvalidArgument,
		"unknown characteristic, skill or resource: "+key)
}

// DeleteValue removes one attribute or skill value from the sheet (the
// active sheet when id is empty). Resource tracks are not deleted here.
func (a *App) DeleteValue(ctx context.Context, id, key string) (*character.Character, error) {
	const op = "app.DeleteValue"
	c, err := a.GetCharacter(ctx, id)
	if err != nil {
		return nil, err
	}
	sys, err := a.registry.MustGet(c.SystemID)
	if err != nil {
		return nil, err
	}
	resolver, ok := sys.(KeyResolver)
	if !ok {
		return nil, apperr.New(op, apperr.RuleNotFound,
			"rule system "+c.SystemID+" does not support keyed lookups")
	}
	switch canonical, found := resolver.ResolveAttribute(key); {
	case found:
		if _, exists := c.Attributes[canonical]; !exists {
			return nil, apperr.New(op, apperr.InvalidArgument,
				"character is missing characteristic "+canonical)
		}
		delete(c.Attributes, canonical)
	default:
		canonical, found := resolver.ResolveSkill(key)
		if !found {
			return nil, apperr.New(op, apperr.InvalidArgument,
				"unknown characteristic or skill: "+key)
		}
		if _, exists := c.Skills[canonical]; !exists {
			return nil, apperr.New(op, apperr.InvalidArgument,
				"character has no value for skill "+canonical)
		}
		delete(c.Skills, canonical)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := a.validateBySystem(ctx, sys, c); err != nil {
		return nil, err
	}
	if err := a.chars.Update(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// ClearValues empties the numeric values of the sheet (the active sheet when
// id is empty): attribute and skill entries are removed and resource tracks
// are reset to zero. The card, its ID and its name are kept; the update
// path backs the previous file up first.
func (a *App) ClearValues(ctx context.Context, id string) (*character.Character, error) {
	const op = "app.ClearValues"
	c, err := a.GetCharacter(ctx, id)
	if err != nil {
		return nil, err
	}
	sys, err := a.registry.MustGet(c.SystemID)
	if err != nil {
		return nil, err
	}
	c.Attributes = map[string]int{}
	c.Skills = map[string]int{}
	for track, r := range c.Resources {
		r.Current = 0
		c.Resources[track] = r
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := a.validateBySystem(ctx, sys, c); err != nil {
		return nil, err
	}
	if err := a.chars.Update(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// ValidateCharacter reloads and validates a sheet generically and by system.
func (a *App) ValidateCharacter(ctx context.Context, id string) (*character.Character, error) {
	c, err := a.chars.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	sys, err := a.registry.MustGet(c.SystemID)
	if err != nil {
		return nil, err
	}
	return c, a.validateBySystem(ctx, sys, c)
}

// ExportCharacter serializes a sheet to YAML bytes.
func (a *App) ExportCharacter(ctx context.Context, id string) ([]byte, error) {
	c, err := a.chars.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(c)
}

// ImportCharacter parses YAML, reassigns a fresh ID (unless keepID) and
// persists it.
func (a *App) ImportCharacter(ctx context.Context, data []byte, keepID bool) (*character.Character, error) {
	c := &character.Character{}
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, apperr.Wrap("app.ImportCharacter", apperr.InvalidData,
			"imported data is not valid YAML", err)
	}
	if _, err := a.registry.MustGet(c.SystemID); err != nil {
		return nil, err
	}
	if keepID {
		if err := character.ValidateID(c.ID); err != nil {
			return nil, err
		}
		if err := c.Validate(); err != nil {
			return nil, err
		}
		if err := a.chars.Create(ctx, c); err != nil {
			return nil, err
		}
		return c, nil
	}
	c.ID = ""
	for range 5 {
		id, err := newCharacterID()
		if err != nil {
			return nil, err
		}
		c.ID = id
		if err := c.Validate(); err != nil {
			return nil, err
		}
		if err := a.chars.Create(ctx, c); err != nil {
			if apperr.IsCode(err, apperr.Conflict) {
				continue
			}
			return nil, err
		}
		return c, nil
	}
	return nil, apperr.New("app.ImportCharacter", apperr.Internal, "could not allocate a unique character id")
}

// KeyResolver is an optional RuleSystem capability: it maps user-typed
// attribute/skill names (IDs, aliases, localized labels) to canonical sheet
// keys. Systems that do not implement it cannot use --set on create/set.
type KeyResolver interface {
	ResolveAttribute(name string) (canonical string, ok bool)
	ResolveSkill(name string) (canonical string, ok bool)
}

// assign resolves assignments into attributes/skills via the owning system.
func (a *App) assign(sys rules.RuleSystem, c *character.Character, sets []Assignment) error {
	if len(sets) == 0 {
		return nil
	}
	resolver, ok := sys.(KeyResolver)
	if !ok {
		return apperr.New("app.assign", apperr.RuleNotFound,
			"rule system "+c.SystemID+" does not support key=value assignments")
	}
	for _, set := range sets {
		switch canonical, found := resolver.ResolveAttribute(set.Key); {
		case found:
			if c.Attributes == nil {
				c.Attributes = map[string]int{}
			}
			c.Attributes[canonical] = set.Value
		default:
			if canonical, found := resolver.ResolveSkill(set.Key); found {
				if c.Skills == nil {
					c.Skills = map[string]int{}
				}
				c.Skills[canonical] = set.Value
			} else {
				return apperr.New("app.assign", apperr.InvalidArgument,
					fmt.Sprintf("unknown characteristic or skill: %s", set.Key))
			}
		}
	}
	return nil
}

// systemCharacterValidator is the optional rule-system hook for
// system-specific sheet validation.
type systemCharacterValidator interface {
	ValidateCharacter(ctx context.Context, c *character.Character) error
}

func (a *App) validateBySystem(ctx context.Context, sys any, c *character.Character) error {
	if v, ok := sys.(systemCharacterValidator); ok {
		return v.ValidateCharacter(ctx, c)
	}
	return nil
}
