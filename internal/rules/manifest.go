// Package rules defines the rule-system contract: a Manifest describing a
// rule system's identity, the RuleSystem interface implemented by compiled
// rulebooks (*engine.Book), and the Registry holding loaded systems.
//
// Systems are data, not Go packages: one game is one self-contained
// rulebook YAML file (the built-in rulebooks/coc7.yaml or a user file under
// <data-root>/rulebooks/), and its header doubles as the manifest. Books
// are loaded at startup from both sources via the same engine.ParseBook
// path rather than compile-time registered.
//
// The generic dice engine never imports this package; the rule engine
// depends inward on the dice engine, never the reverse.
package rules

import (
	"regexp"

	"dicegirl/internal/apperr"
)

// ManifestSchemaVersion is the accepted rulebook schema_version.
const ManifestSchemaVersion = "1"

// Manifest describes one registered rule system. The fields mirror the
// identity header of the system's single rulebook file.
type Manifest struct {
	SchemaVersion string `yaml:"schema_version"`
	ID            string `yaml:"id"`
	Name          string `yaml:"name"`
	SystemVersion string `yaml:"system_version"`
	RulesVersion  string `yaml:"rules_version"`
	Description   string `yaml:"description"`
	// Default marks the rulebook used when a command names no system and
	// more than one book is loaded. At most one book is preferred; when
	// several declare it, the last one registered wins (user books load
	// after the built-ins).
	Default bool `yaml:"default"`
	// Commands are the short command aliases the book answers to (the
	// character-generation entry point, e.g. "coc" or "dnd"). The alias
	// lives in the rulebook, so adding a game needs no Go change.
	Commands []string `yaml:"commands"`
}

var (
	idPattern      = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)
	versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)
	// aliasPattern is the shape of a rulebook-declared short command
	// alias: lowercase letters, digits and dashes.
	aliasPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

// Validate checks identity: schema, ID format/name length, semantic version
// and rules version.
func (m Manifest) Validate() error {
	const op = "rules.Manifest.Validate"
	if m.SchemaVersion != ManifestSchemaVersion {
		return apperr.New(op, apperr.UnsupportedSchemaVersion,
			"rulebook schema_version must be \""+ManifestSchemaVersion+"\", got "+quote(m.SchemaVersion))
	}
	if m.ID == "" {
		return apperr.New(op, apperr.ValidationError, "rule system id is empty")
	}
	if len(m.ID) > 64 || !idPattern.MatchString(m.ID) {
		return apperr.New(op, apperr.ValidationError,
			"rule system id must be lowercase letters, digits and ._- separators, got "+quote(m.ID))
	}
	if m.Name == "" {
		return apperr.New(op, apperr.ValidationError, "rule system "+m.ID+" has no name")
	}
	if !versionPattern.MatchString(m.SystemVersion) {
		return apperr.New(op, apperr.ValidationError,
			"system_version must be semantic version (x.y.z), got "+quote(m.SystemVersion))
	}
	if m.RulesVersion == "" {
		return apperr.New(op, apperr.ValidationError, "rules_version is empty for "+m.ID)
	}
	seenAlias := map[string]bool{}
	for _, alias := range m.Commands {
		if !aliasPattern.MatchString(alias) {
			return apperr.New(op, apperr.ValidationError,
				"short command alias must be lowercase letters, digits and dashes, got "+quote(alias))
		}
		if seenAlias[alias] {
			return apperr.New(op, apperr.ValidationError,
				"duplicate short command alias "+quote(alias)+" in "+m.ID)
		}
		seenAlias[alias] = true
	}
	return nil
}

func quote(s string) string { return "\"" + s + "\"" }
