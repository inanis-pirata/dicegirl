// Package character holds the character-sheet domain model, ID rules and
// repository contract. It has no filesystem dependency: storage
// implementations live in internal/storage and depend on this package.
package character

import (
	"context"
	"regexp"
	"time"

	"dicegirl/internal/apperr"
)

// SchemaVersion is the current character-card schema version. It is
// independent from manifest and rule-config schema versions.
const SchemaVersion = "1"

// Resource is a character track with a current value and a maximum
// (hit points, magic points, sanity).
type Resource struct {
	Current int `yaml:"current"`
	Max     int `yaml:"max"`
}

// Character is a system-neutral character sheet. Rule-specific values that
// do not fit attributes/skills/resources go into Extra (the only place a
// free-form map is permitted; it is the extension boundary, not a way of
// life for core logic).
type Character struct {
	SchemaVersion string              `yaml:"schema_version"`
	ID            string              `yaml:"id"`
	Name          string              `yaml:"name"`
	SystemID      string              `yaml:"system_id"`
	Attributes    map[string]int      `yaml:"attributes"`
	Skills        map[string]int      `yaml:"skills"`
	Resources     map[string]Resource `yaml:"resources,omitempty"`
	// Macros holds named dice expressions saved on the card (Dice!
	// `.st &名=表达式`); they are rolled by name.
	Macros    map[string]string `yaml:"macros,omitempty"`
	Metadata  map[string]string `yaml:"metadata,omitempty"`
	Extra     map[string]any    `yaml:"extra,omitempty"`
	CreatedAt time.Time         `yaml:"created_at"`
	UpdatedAt time.Time         `yaml:"updated_at"`
}

// Repository is the persistence port. Implementations must distinguish
// not-found, unreadable/corrupt data, schema mismatch and write failures via
// apperr codes.
type Repository interface {
	Create(ctx context.Context, c *Character) error
	Get(ctx context.Context, id string) (*Character, error)
	List(ctx context.Context) ([]*Character, error)
	Update(ctx context.Context, c *Character) error
	Delete(ctx context.Context, id string) error
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{3,63}$`)

// ValidateID checks a program-safe character ID. IDs are never built from a
// display name: the application layer generates them or accepts only
// pre-validated imports.
func ValidateID(id string) error {
	if !idPattern.MatchString(id) {
		return apperr.New("character.ValidateID", apperr.InvalidArgument,
			"character id must match ^[a-z0-9][a-z0-9-]{3,63}$ (lowercase, digits, hyphen)")
	}
	return nil
}

// Validate performs system-independent sheet validation. System-specific
// validation is performed separately by the registered rule system.
func (c *Character) Validate() error {
	const op = "character.Validate"
	if c == nil {
		return apperr.New(op, apperr.InvalidArgument, "nil character")
	}
	if c.SchemaVersion == "" {
		return apperr.New(op, apperr.ValidationError, "schema_version is required")
	}
	if c.SchemaVersion != SchemaVersion {
		return apperr.New(op, apperr.UnsupportedSchemaVersion,
			"character schema_version "+c.SchemaVersion+" is not supported (want "+SchemaVersion+")")
	}
	if err := ValidateID(c.ID); err != nil {
		return err
	}
	if c.Name == "" {
		return apperr.New(op, apperr.ValidationError, "character name is required")
	}
	if len([]rune(c.Name)) > 64 {
		return apperr.New(op, apperr.ValidationError, "character name must be at most 64 characters")
	}
	if c.SystemID == "" {
		return apperr.New(op, apperr.ValidationError, "system_id is required")
	}
	for k, v := range c.Attributes {
		if k == "" {
			return apperr.New(op, apperr.ValidationError, "empty attribute key")
		}
		if v < 0 {
			return apperr.New(op, apperr.ValidationError, "attribute "+k+" must be non-negative")
		}
	}
	for k, v := range c.Skills {
		if k == "" {
			return apperr.New(op, apperr.ValidationError, "empty skill key")
		}
		if v < 0 {
			return apperr.New(op, apperr.ValidationError, "skill "+k+" must be non-negative")
		}
	}
	for k, r := range c.Resources {
		if r.Max < 0 || r.Current < 0 {
			return apperr.New(op, apperr.ValidationError, "resource "+k+" must be non-negative")
		}
		if r.Current > r.Max {
			return apperr.New(op, apperr.ValidationError,
				"resource "+k+" current value exceeds its maximum")
		}
	}
	return nil
}

// Clone returns a deep copy so callers (and tests) can mutate a loaded sheet
// without touching repository state.
func (c *Character) Clone() *Character {
	if c == nil {
		return nil
	}
	cp := *c
	cp.Attributes = cloneIntMap(c.Attributes)
	cp.Skills = cloneIntMap(c.Skills)
	cp.Metadata = cloneStringMap(c.Metadata)
	if c.Macros != nil {
		cp.Macros = cloneStringMap(c.Macros)
	}
	if c.Resources != nil {
		cp.Resources = make(map[string]Resource, len(c.Resources))
		for k, v := range c.Resources {
			cp.Resources[k] = v
		}
	}
	if c.Extra != nil {
		cp.Extra = make(map[string]any, len(c.Extra))
		for k, v := range c.Extra {
			cp.Extra[k] = v
		}
	}
	return &cp
}

// Attribute returns the attribute value and whether it exists.
func (c *Character) Attribute(id string) (int, bool) {
	v, ok := c.Attributes[id]
	return v, ok
}

// Skill returns the skill value and whether it exists.
func (c *Character) Skill(id string) (int, bool) {
	v, ok := c.Skills[id]
	return v, ok
}

func cloneIntMap(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
