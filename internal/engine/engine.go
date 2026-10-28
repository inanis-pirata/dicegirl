// Package engine is the rule-agnostic game engine. It interprets one
// self-contained rulebook (YAML): the dice mechanic, success levels and
// fumble conditions, opposed resolution, derived-statistic formulas,
// resource checks and lookup tables. No game-specific branching lives in
// Go 鈥?Call of Cthulhu is data, not a code package. Adding a game means
// adding a rulebook file, not recompiling engine code.
//
// Rulebooks declare arithmetic in the whitelisted formula language
// (internal/engine/formula) and randomness as ordinary dice expressions
// evaluated through the injected generic dice roller.
package engine

import (
	"context"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine/formula"
	"dicegirl/internal/rules"
)

// SchemaVersion is the accepted rulebook schema_version.
const SchemaVersion = "1"

// Roller is the subset of *dice.Roller the engine needs.
type Roller interface {
	Roll(ctx context.Context, expr string) (*dice.RollResult, error)
}

// AttributeDef describes one named characteristic identifier. Aliases are
// alternative names players type (rulebook data, never hard-coded); they
// resolve after the canonical ID and before the localized label.
type AttributeDef struct {
	ID      string   `yaml:"id"`
	Name    string   `yaml:"name"`
	Aliases []string `yaml:"aliases,omitempty"`
}

// SkillDef is one skill entry. Base is a constant starting value; BaseExpr
// is a formula over attributes used instead.
type SkillDef struct {
	ID       string   `yaml:"-"`
	Base     int      `yaml:"base"`
	BaseExpr string   `yaml:"base_expr,omitempty"`
	Name     string   `yaml:"name"`
	Aliases  []string `yaml:"aliases,omitempty"`
}

// Range is a closed integer interval.
type Range struct {
	Min int `yaml:"min"`
	Max int `yaml:"max"`
}

// Contains reports whether v is inside the range.
func (r Range) Contains(v int) bool { return v >= r.Min && v <= r.Max }

// Book is one compiled rulebook: raw declarations plus parsed formulas and
// indexed vocabulary. It implements rules.RuleSystem and the application
// layer's optional KeyResolver / character-validator capabilities.
type Book struct {
	rawBytes []byte
	source   string
	manifest rules.Manifest
	roller   Roller

	attrs        []AttributeDef
	attrByID     map[string]AttributeDef
	attrAlias    map[string]string
	skills       map[string]SkillDef
	skillAlias   map[string]string
	limits       compiledLimits
	levels       []compiledLevel
	levelIndex   map[string]int
	difficulties []compiledDifficulty
	diffIndex    map[string]int
	opposed      compiledOpposed
	dbTable      *compiledDB
	derived      []compiledDerived
	vitals       []compiledVitals
	initAttrs    []compiledInitAttr
	resChecks    map[string]*compiledResourceCheck
	resNames     map[string]string // typing name → resource track id
	cmdBindings  map[string]compiledBinding
	tables       *TableSet
	growth       *compiledGrowthChecks
	generation   *compiledGeneration
	pointBuy     *compiledPointBuy
	ruleEntries  []RuleEntry
	mechanic     *compiledMechanic
	deathSave    *compiledDeathSave
}

// compiledDeathSave is the validated death_save block (T17b). Zero-value
// fields were defaulted at compile time from the 5e convention.
type compiledDeathSave struct {
	Threshold       int
	SuccessesNeeded int
	FailuresAllowed int
	CritValue       int
	CritFailValue   int
}

// DeathSave returns the book's death-save parameters, or nil when the book
// does not declare the mechanic. Callers answer "未声明绑定" in that case.
func (b *Book) DeathSave() *compiledDeathSave { return b.deathSave }

// RollDeathSaveDie rolls one plain die with the book's own mechanic
// expression (no bonus/penalty) and returns the natural value plus the die's
// upper bound (for honest message labels). Death saves and future book-level
// rolls ride the same expression as checks, so a d100 house rule changes
// this roll without touching Go code.
func (b *Book) RollDeathSaveDie(ctx context.Context) (value, dieMax int, err error) {
	const op = "engine.RollDeathSaveDie"
	if b.mechanic == nil {
		return 0, 0, apperr.New(op, apperr.ConfigInvalid, "rulebook declares no mechanic")
	}
	r, err := b.mechanic.kind.Roll(ctx, b.roller, 0, 0)
	if err != nil {
		return 0, 0, apperr.Wrap(op, apperr.Internal, "death save roll failed", err)
	}
	_, dieMax = b.mechanic.kind.RollRange()
	return r.Value, dieMax, nil
}

type compiledLimits struct {
	characteristic Range
	skill          Range
	target         Range
	// computedTarget bounds targets derived by arithmetic from a sheet
	// value (e.g. the Dice! `.rc 浣撹川*5` convention). It defaults to the
	// target range when a rulebook does not declare it.
	computedTarget Range
}

type compiledLevel struct {
	id   string
	name string
	when *formula.Expr // nil = catch-all
	// whenSrc keeps the declared condition (the quick reference quotes it).
	whenSrc string
}

// compiledDifficulty is one explicit difficulty modifier: either a target
// rewrite formula (hard = target/2) or an auto-success mapping.
type compiledDifficulty struct {
	id           string
	name         string
	target       *formula.Expr // nil in auto mode
	auto         bool
	successLevel string // auto mode: level a non-fumble maps to
	// exprSrc keeps the declared formula (the quick reference quotes it).
	exprSrc string
}

// DifficultyInfo describes a declared difficulty modifier for callers
// outside the engine (application layer, CLI keyword matching).
type DifficultyInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Auto bool   `json:"auto"`
}

type compiledOpposed struct {
	fumbleLevel string
	tieBreak    string // "higher_target" | "none"
}

type compiledDBRow struct {
	min   int
	max   int
	build int
	bonus string
}

type compiledDB struct {
	key    *formula.Expr
	keySrc string
	rows   []compiledDBRow
}

type compiledSwitchCase struct {
	when      *formula.Expr
	value     int
	isDefault bool
}

type compiledDerived struct {
	id      string
	name    string
	formula *formula.Expr
	cases   []compiledSwitchCase
	lookup  string // "damage_bonus" | ""
	field   string // "build" | "bonus"
}

type compiledVitals struct {
	resource string
	name     string
	aliases  []string
	max      *formula.Expr
	initial  *formula.Expr
}

// Binding kinds for compiledBinding and BindingInfo.
const (
	BindingKindResourceCheck = "resource_check"
	BindingKindTable         = "table"
)

// compiledBinding is one chat-verb binding: verb → rulebook target.
type compiledBinding struct {
	verb   string
	kind   string // BindingKindResourceCheck | BindingKindTable
	target string
}

// BindingInfo describes one chat-verb binding for callers outside the engine.
type BindingInfo struct {
	Verb   string `json:"verb"`
	Kind   string `json:"kind"`   // resource_check | table
	Target string `json:"target"` // declared resource-check or table id
}

type compiledInitAttr struct {
	attribute string
	value     *formula.Expr
}

// Manifest returns the rulebook identity.
func (b *Book) Manifest() rules.Manifest { return b.manifest }

// Source returns the file name the book was loaded from (embedded name or
// user path), for error messages.
func (b *Book) Source() string { return b.source }

// Raw returns the source bytes the book was compiled from, so a first edit
// of a built-in book can copy them into a writable override file.
func (b *Book) Raw() []byte { return b.rawBytes }

// Roller returns the injected dice roller.
func (b *Book) Roller() Roller { return b.roller }

// ValidateConfig recompiles the rulebook from its source bytes, so generic
// tooling (`dicegirl config validate`) can verify a book without a cached
// compilation result.
func (b *Book) ValidateConfig(_ context.Context) error {
	_, err := ParseBook(b.rawBytes, b.source, b.roller)
	return err
}

// Attributes returns the declared characteristic definitions in order.
func (b *Book) Attributes() []AttributeDef { return b.attrs }

// Skills returns all skill definitions keyed by canonical ID.
func (b *Book) Skills() map[string]SkillDef { return b.skills }

// TargetRange returns the legal target interval.
func (b *Book) TargetRange() Range { return b.limits.target }

// FindAttribute resolves a characteristic by ID (case-insensitive),
// then by declared alias, then by label.
func (b *Book) FindAttribute(name string) (AttributeDef, bool) {
	if a, ok := b.attrByID[strings.ToUpper(strings.TrimSpace(name))]; ok {
		return a, true
	}
	if id, ok := b.attrAlias[strings.ToLower(strings.TrimSpace(name))]; ok {
		return b.attrByID[id], true
	}
	for _, a := range b.attrs {
		if a.Name == name {
			return a, true
		}
	}
	return AttributeDef{}, false
}

// FindSkill resolves a skill by ID (case-insensitive), then by declared
// alias, then by label.
func (b *Book) FindSkill(name string) (SkillDef, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	if sk, ok := b.skills[key]; ok {
		return sk, true
	}
	if id, ok := b.skillAlias[key]; ok {
		return b.skills[id], true
	}
	for _, sk := range b.skills {
		if sk.Name == name {
			return sk, true
		}
	}
	return SkillDef{}, false
}

// ResolveAttribute implements app.KeyResolver.
func (b *Book) ResolveAttribute(name string) (string, bool) {
	if a, ok := b.FindAttribute(name); ok {
		return a.ID, true
	}
	return "", false
}

// ResolveSkill implements app.KeyResolver.
func (b *Book) ResolveSkill(name string) (string, bool) {
	if sk, ok := b.FindSkill(name); ok {
		return sk.ID, true
	}
	return "", false
}

// ResolveResource implements app.KeyResolver. It is the third rung of the
// name-resolution ladder (attributes, skills, resource tracks) and accepts
// a vitals name or alias, the raw track id, or a resource_check label.
func (b *Book) ResolveResource(name string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return "", false
	}
	id, ok := b.resNames[key]
	return id, ok
}

// ResourceDisplayName returns the vitals-declared name of a resource track
// (理智), falling back to the track id itself.
func (b *Book) ResourceDisplayName(resource string) string {
	for _, v := range b.vitals {
		if v.resource == resource {
			if v.name != "" {
				return v.name
			}
			return resource
		}
	}
	return resource
}

// ResourceFallbackTarget computes the check target of a resource track that
// does not exist on the sheet yet: the resource check's declared fallback
// formula, else the vitals initial formula, else an error.
func (b *Book) ResourceFallbackTarget(c *character.Character, resource string) (int, error) {
	const op = "engine.ResourceFallbackTarget"
	for _, rc := range b.resChecks {
		if rc.resource == resource && rc.fallback != nil {
			return rc.FallbackTarget(c.Attributes)
		}
	}
	if d, err := b.Derive(c); err == nil {
		for _, v := range d.Vitals {
			if v.Resource == resource {
				return v.Current, nil
			}
		}
	}
	return 0, apperr.New(op, apperr.InvalidArgument,
		"resource "+resource+" not initialized and no fallback formula is declared")
}

// Binding returns the chat-verb binding declared by the rulebook (the Dice!
// verbs sc/ti/li call whatever the book declares).
func (b *Book) Binding(verb string) (BindingInfo, bool) {
	cb, ok := b.cmdBindings[verb]
	if !ok {
		return BindingInfo{}, false
	}
	return BindingInfo{Verb: cb.verb, Kind: cb.kind, Target: cb.target}, true
}

// Tables returns the random tables declared by the rulebook.
func (b *Book) Tables() []TableDef { return b.tables.Tables() }

// RollTable rolls one declared random table.
func (b *Book) RollTable(ctx context.Context, id string) (*TableResult, error) {
	if b.tables == nil {
		return nil, apperr.New("engine.Book.RollTable", apperr.InvalidArgument,
			"rulebook "+b.manifest.ID+" declares no random tables")
	}
	return b.tables.Roll(ctx, b.roller, id, b.manifest.ID)
}

// SkillBase evaluates a skill's starting value. Character skills override
// it at the application layer; base_expr formulas need attributes.
func (b *Book) SkillBase(sk SkillDef, attrs map[string]int) (int, error) {
	if sk.BaseExpr == "" {
		return sk.Base, nil
	}
	e, err := formula.Parse(sk.BaseExpr)
	if err != nil { // pre-compiled at load time; defensive only
		return 0, err
	}
	v, err := e.EvalInt(formula.Env(attrs))
	if err != nil {
		return 0, apperr.Wrap("engine.SkillBase", apperr.InvalidArgument,
			"cannot compute base of skill "+sk.ID, err)
	}
	return v, nil
}

// ValidateCharacter applies the rulebook's sheet rules on top of the
// system-neutral character.Validate: attributes must be declared and in
// range; skill values (including personal specialties) must be in range.
func (b *Book) ValidateCharacter(_ context.Context, c *character.Character) error {
	const op = "engine.ValidateCharacter"
	for id, v := range c.Attributes {
		def, ok := b.FindAttribute(id)
		if !ok {
			return apperr.New(op, apperr.ValidationError, "unknown characteristic "+id)
		}
		if !b.limits.characteristic.Contains(v) {
			return apperr.New(op, apperr.ValidationError,
				def.ID+" out of range ["+strconv.Itoa(b.limits.characteristic.Min)+","+strconv.Itoa(b.limits.characteristic.Max)+"]")
		}
	}
	for id, v := range c.Skills {
		if !b.limits.skill.Contains(v) {
			return apperr.New(op, apperr.ValidationError,
				"skill "+id+" out of range ["+strconv.Itoa(b.limits.skill.Min)+","+strconv.Itoa(b.limits.skill.Max)+"]")
		}
	}
	return nil
}
