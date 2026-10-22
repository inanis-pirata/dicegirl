package engine

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"dicegirl/internal/apperr"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine/formula"
	"dicegirl/internal/rules"
)

// --- raw rulebook schema (decoded strictly from YAML) ---

type rawBook struct {
	SchemaVersion  string                      `yaml:"schema_version"`
	ID             string                      `yaml:"id"`
	Name           string                      `yaml:"name"`
	SystemVersion  string                      `yaml:"system_version"`
	RulesVersion   string                      `yaml:"rules_version"`
	Description    string                      `yaml:"description"`
	Default        bool                        `yaml:"default"`
	Commands       []string                    `yaml:"commands"`
	Attributes     []AttributeDef              `yaml:"attributes"`
	Skills         map[string]rawSkill         `yaml:"skills"`
	Limits         rawLimits                   `yaml:"limits"`
	Mechanic       rawMechanic                 `yaml:"mechanic"`
	Checks         rawChecks                   `yaml:"checks"`
	Opposed        rawOpposed                  `yaml:"opposed"`
	DamageBonus    *rawDamageBonus             `yaml:"damage_bonus"`
	Derived        rawDerived                  `yaml:"derived"`
	ResourceChecks map[string]rawResourceCheck `yaml:"resource_checks"`
	Tables         []rawTable                  `yaml:"tables"`
	GrowthChecks   *rawGrowthChecks            `yaml:"growth_checks"`
	Generation     *rawGeneration              `yaml:"generation"`
	Rules          []rawRuleEntry              `yaml:"rules"`
}

// rawRuleEntry is one quick-reference entry of the rulebook.
type rawRuleEntry struct {
	ID       string   `yaml:"id"`
	Title    string   `yaml:"title"`
	Keywords []string `yaml:"keywords"`
	Text     string   `yaml:"text"`
}

type rawSkill struct {
	Base     int      `yaml:"base"`
	BaseExpr string   `yaml:"base_expr"`
	Name     string   `yaml:"name"`
	Aliases  []string `yaml:"aliases"`
}

type rawLimits struct {
	Characteristic Range `yaml:"characteristic"`
	Skill          Range `yaml:"skill"`
	Target         Range `yaml:"target"`
	// ComputedTarget bounds targets produced by arithmetic on a sheet
	// value (e.g. `体质*5`). Optional: it defaults to the target range.
	ComputedTarget *Range `yaml:"computed_target"`
}

type rawMechanic struct {
	Type            string `yaml:"type"`
	MaxBonusPenalty int    `yaml:"max_bonus_penalty"`
	// Generic expression family: `roll` is the dice expression of one
	// check, `meets` the boolean formula deciding success against a
	// target. Exactly one of type/roll must be given.
	Roll        string `yaml:"roll"`
	Meets       string `yaml:"meets"`
	BonusKeep   string `yaml:"bonus_keep"`
	PenaltyKeep string `yaml:"penalty_keep"`
	Label       string `yaml:"label"`
}

type rawLevel struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	When string `yaml:"when"`
}

type rawChecks struct {
	Levels       []rawLevel      `yaml:"levels"`
	Difficulties []rawDifficulty `yaml:"difficulties"`
}

// rawDifficulty is one explicit difficulty modifier: either an int formula
// rewriting the target (hard = target/2) or an auto-success mode mapping
// every non-fumble to success_level.
type rawDifficulty struct {
	ID           string `yaml:"id"`
	Name         string `yaml:"name"`
	Expr         string `yaml:"expr"`
	Auto         bool   `yaml:"auto"`
	SuccessLevel string `yaml:"success_level"`
}

type rawOpposed struct {
	FumbleLevel string `yaml:"fumble_level"`
	TieBreak    string `yaml:"tie_break"`
}

type rawDBRow struct {
	Min   int    `yaml:"min"`
	Max   int    `yaml:"max"`
	Build int    `yaml:"build"`
	Bonus string `yaml:"bonus"`
}

type rawDamageBonus struct {
	Key  string     `yaml:"key"`
	Rows []rawDBRow `yaml:"rows"`
}

type rawDerived struct {
	Values            []rawDerivedValue `yaml:"values"`
	Vitals            []rawVitals       `yaml:"vitals"`
	InitialAttributes []rawInitAttr     `yaml:"initial_attributes"`
}

type rawDerivedValue struct {
	ID      string          `yaml:"id"`
	Name    string          `yaml:"name"`
	Formula string          `yaml:"formula"`
	Switch  []rawSwitchCase `yaml:"switch"`
	Lookup  string          `yaml:"lookup"`
	Field   string          `yaml:"field"`
}

type rawSwitchCase struct {
	When    string `yaml:"when"`
	Value   int    `yaml:"value"`
	Default bool   `yaml:"default"`
}

type rawVitals struct {
	Resource string `yaml:"resource"`
	Max      string `yaml:"max"`
	Initial  string `yaml:"initial"`
}

type rawInitAttr struct {
	Attribute string `yaml:"attribute"`
	Value     string `yaml:"value"`
}

type rawLoss struct {
	Success string `yaml:"success"`
	Failure string `yaml:"failure"`
}

type rawResourceCheck struct {
	Label        string    `yaml:"label"`
	Resource     string    `yaml:"resource"`
	Fallback     string    `yaml:"fallback"`
	SuccessLevel string    `yaml:"success_level"`
	Loss         rawLoss   `yaml:"loss"`
	FumbleLoss   string    `yaml:"fumble_loss"`
	Range        *Range    `yaml:"range"`
	Clamp        *rawClamp `yaml:"clamp"`
}

type rawClamp struct {
	Min *int `yaml:"min"`
	Max *int `yaml:"max"`
}

// ParseBook decodes and compiles one rulebook. Strict field checking makes
// typos in the frozen data fail loudly. roller may be nil to use the
// default crypto roller.
func ParseBook(data []byte, source string, roller Roller) (*Book, error) {
	if roller == nil {
		roller = dice.Default()
	}
	var raw rawBook
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return nil, apperr.Wrap("engine.ParseBook", apperr.ConfigInvalid,
			"invalid rulebook "+source, err)
	}
	b := &Book{
		rawBytes: data,
		source:   source,
		roller:   roller,
	}
	if err := b.compile(&raw); err != nil {
		return nil, apperr.Wrap("engine.ParseBook", apperr.ConfigInvalid,
			"invalid rulebook "+source, err)
	}
	return b, nil
}

func (b *Book) compile(raw *rawBook) error {
	const op = "engine.compile"

	b.manifest = rules.Manifest{
		SchemaVersion: raw.SchemaVersion,
		ID:            raw.ID,
		Name:          raw.Name,
		SystemVersion: raw.SystemVersion,
		RulesVersion:  raw.RulesVersion,
		Description:   raw.Description,
		Default:       raw.Default,
		Commands:      append([]string(nil), raw.Commands...),
	}
	if err := b.manifest.Validate(); err != nil {
		return err
	}

	// Attributes.
	if len(raw.Attributes) == 0 {
		return apperr.New(op, apperr.ConfigInvalid, "attributes list is empty")
	}
	b.attrByID = map[string]AttributeDef{}
	b.attrAlias = map[string]string{}
	attrVars := map[string]bool{}
	for _, a := range raw.Attributes {
		if a.ID == "" || a.Name == "" {
			return apperr.New(op, apperr.ConfigInvalid, "attribute with empty id or name")
		}
		if _, dup := b.attrByID[a.ID]; dup {
			return apperr.New(op, apperr.ConfigInvalid, "duplicate attribute "+a.ID)
		}
		b.attrByID[a.ID] = a
		b.attrs = append(b.attrs, a)
		attrVars[a.ID] = true
	}
	// Attribute aliases are rulebook data: they resolve after the
	// canonical ID and before the localized label, so they must not
	// shadow another entry's ID or label.
	for _, a := range b.attrs {
		for _, alias := range a.Aliases {
			key := strings.ToLower(strings.TrimSpace(alias))
			if key == "" {
				return apperr.New(op, apperr.ConfigInvalid,
					"attribute "+a.ID+" has an empty alias")
			}
			if other, dup := b.attrAlias[key]; dup {
				return apperr.New(op, apperr.ConfigInvalid,
					"duplicate attribute alias "+alias+" ("+a.ID+" and "+other+")")
			}
			if other, ok := b.attrByID[strings.ToUpper(key)]; ok {
				return apperr.New(op, apperr.ConfigInvalid,
					"attribute alias "+alias+" of "+a.ID+" shadows characteristic "+other.ID)
			}
			for _, other := range b.attrs {
				if strings.EqualFold(other.Name, alias) {
					return apperr.New(op, apperr.ConfigInvalid,
						"attribute alias "+alias+" of "+a.ID+" duplicates the name of "+other.ID)
				}
			}
			b.attrAlias[key] = a.ID
		}
	}

	// Limits.
	if !validRange(raw.Limits.Characteristic) || !validRange(raw.Limits.Skill) || !validRange(raw.Limits.Target) {
		return apperr.New(op, apperr.ConfigInvalid, "limits must satisfy 0 <= min <= max")
	}
	computed := raw.Limits.Target
	if raw.Limits.ComputedTarget != nil {
		if !validRange(*raw.Limits.ComputedTarget) {
			return apperr.New(op, apperr.ConfigInvalid,
				"limits.computed_target must satisfy 0 <= min <= max")
		}
		computed = *raw.Limits.ComputedTarget
	}
	b.limits = compiledLimits{
		characteristic: raw.Limits.Characteristic,
		skill:          raw.Limits.Skill,
		target:         raw.Limits.Target,
		computedTarget: computed,
	}

	// Skills.
	b.skills = map[string]SkillDef{}
	b.skillAlias = map[string]string{}
	skillIDs := make([]string, 0, len(raw.Skills))
	for id := range raw.Skills {
		skillIDs = append(skillIDs, id)
	}
	sort.Strings(skillIDs)
	for _, id := range skillIDs {
		sk := raw.Skills[id]
		if sk.Name == "" {
			return apperr.New(op, apperr.ConfigInvalid, "skill "+id+" has no name")
		}
		if sk.Base < 0 {
			return apperr.New(op, apperr.ConfigInvalid, "skill "+id+" base is negative")
		}
		if sk.BaseExpr != "" {
			if sk.Base != 0 {
				return apperr.New(op, apperr.ConfigInvalid,
					"skill "+id+" cannot set both base and base_expr")
			}
			if _, err := compileIntFormula(sk.BaseExpr, "skill "+id+" base_expr", attrVars); err != nil {
				return err
			}
		}
		b.skills[id] = SkillDef{ID: id, Base: sk.Base, BaseExpr: sk.BaseExpr, Name: sk.Name, Aliases: sk.Aliases}
	}
	// Skill aliases resolve after the canonical ID and before the label.
	for _, id := range skillIDs {
		sk := b.skills[id]
		for _, alias := range sk.Aliases {
			key := strings.ToLower(strings.TrimSpace(alias))
			if key == "" {
				return apperr.New(op, apperr.ConfigInvalid, "skill "+id+" has an empty alias")
			}
			if other, dup := b.skillAlias[key]; dup {
				return apperr.New(op, apperr.ConfigInvalid,
					"duplicate skill alias "+alias+" ("+id+" and "+other+")")
			}
			if _, ok := b.skills[key]; ok {
				return apperr.New(op, apperr.ConfigInvalid,
					"skill alias "+alias+" of "+id+" shadows skill "+key)
			}
			for _, other := range b.skills {
				if strings.EqualFold(other.Name, alias) {
					return apperr.New(op, apperr.ConfigInvalid,
						"skill alias "+alias+" of "+id+" duplicates the name of "+other.ID)
				}
			}
			b.skillAlias[key] = id
		}
	}

	// Mechanic: the dice resolution family (percentile, d20, ...). The
	// engine has no hard-coded family; a rulebook picks one by name.
	mech, err := compileMechanic(op, raw.Mechanic)
	if err != nil {
		return err
	}
	b.mechanic = mech

	// Success levels.
	if err := b.compileLevels(op, raw.Checks); err != nil {
		return err
	}

	// Opposed resolution.
	if raw.Opposed.FumbleLevel == "" || raw.Opposed.TieBreak == "" {
		return apperr.New(op, apperr.ConfigInvalid, "opposed requires fumble_level and tie_break")
	}
	if _, ok := b.levelIndex[raw.Opposed.FumbleLevel]; !ok {
		return apperr.New(op, apperr.ConfigInvalid,
			"opposed.fumble_level "+raw.Opposed.FumbleLevel+" is not a declared level")
	}
	if raw.Opposed.TieBreak != "higher_target" && raw.Opposed.TieBreak != "none" {
		return apperr.New(op, apperr.ConfigInvalid,
			"opposed.tie_break must be higher_target or none, got "+raw.Opposed.TieBreak)
	}
	b.opposed = compiledOpposed{fumbleLevel: raw.Opposed.FumbleLevel, tieBreak: raw.Opposed.TieBreak}

	// Optional explicit difficulty modifiers (need levels resolved first).
	if err := b.compileDifficulties(op, raw.Checks.Difficulties); err != nil {
		return err
	}

	// Damage-bonus table (optional; needed by damage rolls and lookup-derived values).
	if raw.DamageBonus != nil {
		table, err := compileDB(op, raw.DamageBonus, attrVars)
		if err != nil {
			return err
		}
		b.dbTable = table
	}

	// Derived values, vitals and initial attributes.
	if err := b.compileDerived(op, raw.Derived, attrVars); err != nil {
		return err
	}

	// Resource checks.
	b.resChecks = map[string]*compiledResourceCheck{}
	resIDs := make([]string, 0, len(raw.ResourceChecks))
	for id := range raw.ResourceChecks {
		resIDs = append(resIDs, id)
	}
	sort.Strings(resIDs)
	for _, id := range resIDs {
		rc, err := compileResourceCheck(op, id, raw.ResourceChecks[id], attrVars, b.levelIndex)
		if err != nil {
			return err
		}
		b.resChecks[id] = rc
	}

	// Random tables (game content such as the CoC7 insanity tables).
	tables, err := compileTables(op, raw.Tables)
	if err != nil {
		return err
	}
	b.tables = tables

	// Growth checks (skill improvement). Overrides reference declared
	// skills/characteristics, so they compile after the vocabulary.
	if raw.GrowthChecks != nil {
		if err := b.compileGrowthChecks(op, *raw.GrowthChecks); err != nil {
			return err
		}
	}

	// Character generation (rolled and point-buy methods, variants).
	if err := b.compileGeneration(op, raw.Generation); err != nil {
		return err
	}

	// Quick-reference entries (the `.rules` dictionary).
	if err := b.compileRules(op, raw.Rules); err != nil {
		return err
	}
	return nil
}

// compileRules validates the quick-reference entries.
func (b *Book) compileRules(op string, raw []rawRuleEntry) error {
	if len(raw) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for i, r := range raw {
		where := fmt.Sprintf("rules entry %d", i)
		if r.ID == "" || r.Title == "" || r.Text == "" {
			return apperr.New(op, apperr.ConfigInvalid, where+" needs an id, a title and text")
		}
		if seen[r.ID] {
			return apperr.New(op, apperr.ConfigInvalid, "duplicate rules entry "+r.ID)
		}
		seen[r.ID] = true
		b.ruleEntries = append(b.ruleEntries, RuleEntry{
			ID:       r.ID,
			Title:    r.Title,
			Keywords: append([]string(nil), r.Keywords...),
			Text:     r.Text,
		})
	}
	return nil
}

func (b *Book) compileLevels(op string, raw rawChecks) error {
	if len(raw.Levels) < 2 {
		return apperr.New(op, apperr.ConfigInvalid, "checks.levels needs at least two levels")
	}
	b.levelIndex = map[string]int{}
	rollTargetVars := map[string]bool{"roll": true, "target": true}
	for i, lv := range raw.Levels {
		isLast := i == len(raw.Levels)-1
		if lv.ID == "" || lv.Name == "" {
			return apperr.New(op, apperr.ConfigInvalid, fmt.Sprintf("level %d has empty id or name", i))
		}
		if _, dup := b.levelIndex[lv.ID]; dup {
			return apperr.New(op, apperr.ConfigInvalid, "duplicate level "+lv.ID)
		}
		switch {
		case isLast && lv.When != "":
			return apperr.New(op, apperr.ConfigInvalid,
				"last level "+lv.ID+" must be the catch-all (no when)")
		case !isLast && lv.When == "":
			return apperr.New(op, apperr.ConfigInvalid,
				"level "+lv.ID+" needs a when condition (only the last level may be a catch-all)")
		}
		var when *formula.Expr
		if lv.When != "" {
			e, err := compileBoolFormula(lv.When, "level "+lv.ID+" when", rollTargetVars)
			if err != nil {
				return err
			}
			when = e
		}
		b.levelIndex[lv.ID] = i
		b.levels = append(b.levels, compiledLevel{id: lv.ID, name: lv.Name, when: when, whenSrc: lv.When})
	}
	return nil
}

func (b *Book) compileDifficulties(op string, raw []rawDifficulty) error {
	if len(raw) == 0 {
		return nil
	}
	b.diffIndex = map[string]int{}
	ids, names := map[string]bool{}, map[string]bool{}
	targetVar := map[string]bool{"target": true}
	for i, d := range raw {
		where := fmt.Sprintf("difficulty %d", i)
		if d.ID == "" || d.Name == "" {
			return apperr.New(op, apperr.ConfigInvalid, where+" has empty id or name")
		}
		if ids[d.ID] {
			return apperr.New(op, apperr.ConfigInvalid, "duplicate difficulty id "+d.ID)
		}
		if names[d.Name] {
			return apperr.New(op, apperr.ConfigInvalid, "duplicate difficulty name "+d.Name)
		}
		modes := 0
		if d.Expr != "" {
			modes++
		}
		if d.Auto {
			modes++
		}
		if modes != 1 {
			return apperr.New(op, apperr.ConfigInvalid,
				"difficulty "+d.ID+" must set exactly one of expr or auto")
		}
		cd := compiledDifficulty{id: d.ID, name: d.Name, auto: d.Auto, successLevel: d.SuccessLevel, exprSrc: d.Expr}
		if d.Auto {
			rank, ok := b.levelIndex[d.SuccessLevel]
			if !ok {
				return apperr.New(op, apperr.ConfigInvalid,
					"difficulty "+d.ID+" success_level "+quoteOrEmpty(d.SuccessLevel)+" is not a declared level")
			}
			if rank == b.fumbleRank() {
				return apperr.New(op, apperr.ConfigInvalid,
					"difficulty "+d.ID+" success_level must not be the fumble level")
			}
		} else {
			e, err := compileIntFormula(d.Expr, "difficulty "+d.ID+" expr", targetVar)
			if err != nil {
				return err
			}
			cd.target = e
		}
		ids[d.ID] = true
		names[d.Name] = true
		b.diffIndex[strings.ToLower(d.ID)] = i
		b.difficulties = append(b.difficulties, cd)
	}
	return nil
}

func compileDB(op string, raw *rawDamageBonus, vars map[string]bool) (*compiledDB, error) {
	if len(raw.Rows) == 0 {
		return nil, apperr.New(op, apperr.ConfigInvalid, "damage_bonus table is empty")
	}
	key, err := compileIntFormula(raw.Key, "damage_bonus key", vars)
	if err != nil {
		return nil, err
	}
	t := &compiledDB{key: key, keySrc: raw.Key}
	for i, row := range raw.Rows {
		if row.Min > row.Max {
			return nil, apperr.New(op, apperr.ConfigInvalid,
				fmt.Sprintf("damage_bonus row %d has min > max", i))
		}
		if i > 0 && row.Min != raw.Rows[i-1].Max+1 {
			return nil, apperr.New(op, apperr.ConfigInvalid,
				fmt.Sprintf("damage_bonus table not contiguous at row %d", i))
		}
		if _, _, err := dice.Parse(row.Bonus, dice.DefaultLimits()); err != nil {
			return nil, apperr.Wrap(op, apperr.ConfigInvalid,
				"damage_bonus row has invalid dice expression "+row.Bonus, err)
		}
		t.rows = append(t.rows, compiledDBRow{
			min: row.Min, max: row.Max, build: row.Build, bonus: row.Bonus,
		})
	}
	return t, nil
}

func (b *Book) compileDerived(op string, raw rawDerived, attrVars map[string]bool) error {
	env := cloneVars(attrVars) // attrs + previously declared integer derived values
	derivedIDs := map[string]bool{}
	for _, dv := range raw.Values {
		if dv.ID == "" || dv.Name == "" {
			return apperr.New(op, apperr.ConfigInvalid, "derived value with empty id or name")
		}
		if attrVars[dv.ID] || derivedIDs[dv.ID] {
			return apperr.New(op, apperr.ConfigInvalid, "duplicate derived id "+dv.ID)
		}
		compiled := compiledDerived{id: dv.ID, name: dv.Name}
		modes := 0
		if dv.Formula != "" {
			modes++
		}
		if len(dv.Switch) > 0 {
			modes++
		}
		if dv.Lookup != "" {
			modes++
		}
		if modes != 1 {
			return apperr.New(op, apperr.ConfigInvalid,
				"derived "+dv.ID+" must set exactly one of formula/switch/lookup")
		}
		switch {
		case dv.Formula != "":
			e, err := compileIntFormula(dv.Formula, "derived "+dv.ID+" formula", env)
			if err != nil {
				return err
			}
			compiled.formula = e
		case len(dv.Switch) > 0:
			cases, err := compileSwitch(op, dv.ID, dv.Switch, env)
			if err != nil {
				return err
			}
			compiled.cases = cases
		case dv.Lookup != "":
			if dv.Lookup != "damage_bonus" {
				return apperr.New(op, apperr.ConfigInvalid,
					"derived "+dv.ID+" references unknown lookup table "+dv.Lookup)
			}
			if b.dbTable == nil {
				return apperr.New(op, apperr.ConfigInvalid,
					"derived "+dv.ID+" needs a damage_bonus table")
			}
			if dv.Field != "build" && dv.Field != "bonus" {
				return apperr.New(op, apperr.ConfigInvalid,
					"derived "+dv.ID+" lookup field must be build or bonus")
			}
			compiled.lookup = dv.Lookup
			compiled.field = dv.Field
		}
		b.derived = append(b.derived, compiled)
		derivedIDs[dv.ID] = true
		if dv.Field != "bonus" { // only integer values are visible to later formulas
			env[dv.ID] = true
		}
	}

	vitalsSeen := map[string]bool{}
	for _, v := range raw.Vitals {
		if v.Resource == "" {
			return apperr.New(op, apperr.ConfigInvalid, "vitals entry with empty resource")
		}
		if vitalsSeen[v.Resource] {
			return apperr.New(op, apperr.ConfigInvalid, "duplicate vitals resource "+v.Resource)
		}
		vitalsSeen[v.Resource] = true
		max, err := compileIntFormula(v.Max, "vitals "+v.Resource+" max", env)
		if err != nil {
			return err
		}
		initial, err := compileIntFormula(v.Initial, "vitals "+v.Resource+" initial", env)
		if err != nil {
			return err
		}
		b.vitals = append(b.vitals, compiledVitals{resource: v.Resource, max: max, initial: initial})
	}

	for _, ia := range raw.InitialAttributes {
		if _, ok := b.attrByID[ia.Attribute]; !ok {
			return apperr.New(op, apperr.ConfigInvalid,
				"initial_attributes references unknown attribute "+ia.Attribute)
		}
		value, err := compileIntFormula(ia.Value, "initial attribute "+ia.Attribute, env)
		if err != nil {
			return err
		}
		b.initAttrs = append(b.initAttrs, compiledInitAttr{attribute: ia.Attribute, value: value})
	}
	return nil
}

func compileSwitch(op, id string, raw []rawSwitchCase, env map[string]bool) ([]compiledSwitchCase, error) {
	defaults := 0
	cases := make([]compiledSwitchCase, 0, len(raw))
	for i, c := range raw {
		if c.Default {
			defaults++
			if i != len(raw)-1 {
				return nil, apperr.New(op, apperr.ConfigInvalid,
					"derived "+id+" default case must be last")
			}
		} else if c.When == "" {
			return nil, apperr.New(op, apperr.ConfigInvalid,
				fmt.Sprintf("derived %s case %d needs when or default", id, i))
		}
		var when *formula.Expr
		if c.When != "" {
			e, err := compileBoolFormula(c.When, fmt.Sprintf("derived %s case %d", id, i), env)
			if err != nil {
				return nil, err
			}
			when = e
		}
		cases = append(cases, compiledSwitchCase{when: when, value: c.Value, isDefault: c.Default})
	}
	if defaults == 0 {
		return nil, apperr.New(op, apperr.ConfigInvalid,
			"derived "+id+" switch needs a default case")
	}
	return cases, nil
}

func compileIntFormula(src, where string, vars map[string]bool) (*formula.Expr, error) {
	e, err := formula.Parse(src)
	if err != nil {
		return nil, apperr.Wrap("engine.compile", apperr.ConfigInvalid, where+": invalid formula", err)
	}
	if err := e.Check(vars, formula.KindInt); err != nil {
		return nil, apperr.Wrap("engine.compile", apperr.ConfigInvalid, where, err)
	}
	return e, nil
}

func compileBoolFormula(src, where string, vars map[string]bool) (*formula.Expr, error) {
	e, err := formula.Parse(src)
	if err != nil {
		return nil, apperr.Wrap("engine.compile", apperr.ConfigInvalid, where+": invalid formula", err)
	}
	if err := e.Check(vars, formula.KindBool); err != nil {
		return nil, apperr.Wrap("engine.compile", apperr.ConfigInvalid, where, err)
	}
	return e, nil
}

func validRange(r Range) bool { return r.Min >= 0 && r.Min <= r.Max }

func cloneVars(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func quoteOrEmpty(s string) string {
	if s == "" {
		return "(empty)"
	}
	return "\"" + s + "\""
}
