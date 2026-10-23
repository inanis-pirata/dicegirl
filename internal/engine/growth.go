package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine/formula"
)

// Growth checks implement the "improve your skill" step that follows a
// successful check (Dice! `.en`, CoC 7e "Improving Skills"). The rulebook
// declares the condition, the gain and the cap; the engine rolls and
// reports, the application layer writes the result back to the sheet.
//
//	growth_checks:
//	  default:
//	    condition: "roll > target"    # CoC 7e：1d100 高于当前技能值才成长
//	    gain: "1d10"
//	    cap: 99
//	  overrides:
//	    - { key: skill:dodge, gain: "1d6", cap: 99 }

// rawGrowthChecks is the rulebook section for skill growth.
type rawGrowthChecks struct {
	Default   *rawGrowthCheck     `yaml:"default"`
	Overrides []rawGrowthOverride `yaml:"overrides"`
}

// rawGrowthCheck is one growth rule.
type rawGrowthCheck struct {
	Condition string `yaml:"condition"`
	Gain      string `yaml:"gain"`
	// Cap bounds the grown value; nil or 0 means "no cap".
	Cap *int `yaml:"cap"`
}

// rawGrowthOverride is a per-key variant of the default rule. Omitted
// fields are inherited from the default rule.
type rawGrowthOverride struct {
	Key       string `yaml:"key"`
	Condition string `yaml:"condition"`
	Gain      string `yaml:"gain"`
	Cap       *int   `yaml:"cap"`
}

// compiledGrowthCheck is one validated growth rule.
type compiledGrowthCheck struct {
	condition *formula.Expr
	// conditionSrc keeps the source text so overrides can inherit it.
	conditionSrc string
	gain         string
	cap          int
}

// compiledGrowthChecks holds the default rule and the per-key overrides.
type compiledGrowthChecks struct {
	def       *compiledGrowthCheck
	overrides map[string]*compiledGrowthCheck
}

// GrowthKey returns the canonical key of a skill or characteristic: the
// prefixed id ("attr:STR", "skill:psychology"). It is the lookup key of the
// per-key growth overrides.
func (b *Book) GrowthKey(name string) (string, bool) {
	if a, ok := b.FindAttribute(name); ok {
		return "attr:" + a.ID, true
	}
	if s, ok := b.FindSkill(name); ok {
		return "skill:" + s.ID, true
	}
	return "", false
}

// SuccessLevel returns the id of the level that counts as an ordinary
// success: the level right before the catch-all failure. The levels are
// declared worst-first (the fumble is the exceptional worst result), so the
// levels before it are the successes.
func (b *Book) SuccessLevel() string {
	if len(b.levels) < 2 {
		return ""
	}
	return b.levels[len(b.levels)-2].id
}

// FumbleLevel returns the id of the fumble level.
func (b *Book) FumbleLevel() string {
	return b.opposed.fumbleLevel
}

// LevelSucceeds reports whether a check result reached the ordinary
// success threshold (the fumble never succeeds).
func (b *Book) LevelSucceeds(res *CheckResult) bool {
	if res == nil {
		return false
	}
	rank, ok := b.levelIndex[res.LevelCode]
	if !ok {
		return false
	}
	threshold, ok := b.levelIndex[b.SuccessLevel()]
	if !ok {
		return false
	}
	return rank != b.fumbleRank() && rank <= threshold
}

// GrowthCheck returns the growth rule that applies to a canonical growth
// key: the per-key override if one is declared, the default otherwise.
func (b *Book) GrowthCheck(key string) (*compiledGrowthCheck, bool) {
	if b.growth == nil {
		return nil, false
	}
	if gc, ok := b.growth.overrides[key]; ok {
		return gc, true
	}
	if b.growth.def != nil {
		return b.growth.def, true
	}
	return nil, false
}

// GrowthName returns the display name of a canonical growth key.
func (b *Book) GrowthName(key string) string {
	if rest, ok := strings.CutPrefix(key, "attr:"); ok {
		if a, found := b.FindAttribute(rest); found {
			return a.Name
		}
	}
	if rest, ok := strings.CutPrefix(key, "skill:"); ok {
		if s, found := b.FindSkill(rest); found {
			return s.Name
		}
	}
	return growthKeyLabel(key)
}

// Cap returns the maximum value the rule allows (0 = no cap).
func (gc *compiledGrowthCheck) Cap() int { return gc.cap }

// GainExpr returns the dice expression rolled on a successful growth.
func (gc *compiledGrowthCheck) GainExpr() string { return gc.gain }

// Condition returns the source text of the growth condition.
func (gc *compiledGrowthCheck) Condition() string { return gc.conditionSrc }

// GrowthInput drives one growth check.
type GrowthInput struct {
	// Key is the canonical growth key ("skill:psychology").
	Key string
	// Name is the display name of the skill or characteristic.
	Name string
	// Value is the current value of the skill or characteristic.
	Value int
	// Target optionally overrides the value the roll is compared against
	// (the current value by default).
	Target *int
	// Gain overrides the rulebook's gain expression (the Dice!
	// `.en 幸运 +1D3/1D10` form: the caller rolls its own amount).
	Gain string
	// Reason is UI-layer text echoed in the result.
	Reason string
}

// GrowthResult is the structured outcome of one growth check.
type GrowthResult struct {
	Kind        string           `json:"kind"`
	SystemID    string           `json:"system_id"`
	Key         string           `json:"key"`
	Name        string           `json:"name"`
	Before      int              `json:"before"`
	After       int              `json:"after"`
	Gain        int              `json:"gain"`
	Grew        bool             `json:"grew"`
	Cap         int              `json:"cap"`
	Target      int              `json:"target"`
	Roll        *dice.RollResult `json:"roll"`
	GainRoll    *dice.RollResult `json:"gain_roll,omitempty"`
	Reason      string           `json:"reason,omitempty"`
	Explanation string           `json:"explanation"`
}

// RunGrowth rolls the growth d100, evaluates the condition and, when it
// holds, rolls the gain. It never mutates character state: the application
// layer decides whether to write the new value back.
func (b *Book) RunGrowth(ctx context.Context, gc *compiledGrowthCheck, in GrowthInput) (*GrowthResult, error) {
	const op = "engine.RunGrowth"
	if gc == nil {
		return nil, apperr.New(op, apperr.InvalidArgument, "rulebook declares no growth check")
	}
	target := in.Value
	if in.Target != nil {
		target = *in.Target
	}
	roll, err := b.roller.Roll(ctx, "1d100")
	if err != nil {
		return nil, err
	}
	grew, err := gc.condition.EvalBool(formula.Env{"roll": roll.Total, "target": target})
	if err != nil {
		return nil, apperr.Wrap(op, apperr.Internal, "growth condition failed", err)
	}
	res := &GrowthResult{
		Kind:     "growth_check",
		SystemID: b.manifest.ID,
		Key:      in.Key,
		Before:   in.Value,
		After:    in.Value,
		Cap:      gc.cap,
		Target:   target,
		Roll:     roll,
		Reason:   in.Reason,
	}
	label := in.Name
	if label == "" {
		label = growthKeyLabel(in.Key)
	}
	if !grew {
		res.Explanation = fmt.Sprintf("[%s] 掷 %d 未成长（需高于现值 %d）", label, roll.Total, target)
		return res, nil
	}
	gainExpr := gc.gain
	if in.Gain != "" {
		gainExpr = in.Gain
	}
	gainRoll, err := b.roller.Roll(ctx, gainExpr)
	if err != nil {
		return nil, err
	}
	res.GainRoll = gainRoll
	after := in.Value + gainRoll.Total
	if gc.cap > 0 && after > gc.cap {
		after = gc.cap
	}
	if after < in.Value {
		after = in.Value // growth never lowers a value
	}
	res.After = after
	res.Gain = after - in.Value
	res.Grew = res.Gain > 0
	if !res.Grew {
		res.Explanation = fmt.Sprintf("[%s] 掷 %d 高于现值 %d，但已达上限 %d，未成长",
			label, roll.Total, target, gc.cap)
		return res, nil
	}
	res.Explanation = fmt.Sprintf("[%s] 掷 %d 高于现值 %d，成长 +%d（%d → %d，上限 %d）",
		label, roll.Total, target, res.Gain, in.Value, after, gc.cap)
	return res, nil
}

// RuleEntry is one quick-reference entry of a rulebook (Dice!'s rules
// dictionary). The text is a short, self-written summary: the rulebooks
// never quote copyrighted rule text verbatim.
type RuleEntry struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Keywords []string `json:"keywords,omitempty"`
	Text     string   `json:"text"`
}

// Rules returns the quick-reference entries of the rulebook: the hand-written
// `rules:` entries in declaration order, followed by the entries derived from
// the structured data (levels, difficulties, damage bonus, point buy,
// characteristics) that no hand-written entry replaces.
func (b *Book) Rules() []RuleEntry {
	if b == nil {
		return nil
	}
	entries := b.allRuleEntries()
	if len(entries) == 0 {
		return nil
	}
	out := make([]RuleEntry, len(entries))
	copy(out, entries)
	return out
}

// allRuleEntries merges the derived entries with the hand-written ones; a
// hand-written entry replaces the derived entry with the same id.
func (b *Book) allRuleEntries() []RuleEntry {
	derived := b.derivedRules()
	if len(derived) == 0 {
		return b.ruleEntries
	}
	seen := make(map[string]bool, len(b.ruleEntries))
	for _, e := range b.ruleEntries {
		seen[e.ID] = true
	}
	out := make([]RuleEntry, 0, len(b.ruleEntries)+len(derived))
	out = append(out, b.ruleEntries...)
	for _, e := range derived {
		if seen[e.ID] {
			continue
		}
		out = append(out, e)
	}
	return out
}

// RuleHit is one matched quick-reference entry with its match score.
type RuleHit struct {
	Entry RuleEntry `json:"entry"`
	Score int       `json:"score"`
}

// SearchRules ranks the rulebook's entries against a query: an exact title
// match wins over an exact keyword, which wins over a prefix, which wins
// over a substring. The best matches come first.
func (b *Book) SearchRules(query string) []RuleHit {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	hits := make([]RuleHit, 0, len(b.ruleEntries))
	for _, e := range b.allRuleEntries() {
		score := ruleScore(e, q)
		if score > 0 {
			hits = append(hits, RuleHit{Entry: e, Score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	return hits
}

// ruleScore scores one entry against a normalized query.
func ruleScore(e RuleEntry, q string) int {
	title := strings.ToLower(e.Title)
	switch {
	case title == q:
		return 100
	case containsKeyword(e.Keywords, q):
		return 90
	case strings.HasPrefix(title, q):
		return 70
	case hasKeywordPrefix(e.Keywords, q):
		return 60
	case strings.Contains(title, q):
		return 40
	case containsKeywordSubstring(e.Keywords, q):
		return 30
	case strings.Contains(strings.ToLower(e.Text), q):
		return 10
	}
	return 0
}

// containsKeyword reports whether one of the keywords matches exactly.
func containsKeyword(keywords []string, q string) bool {
	for _, k := range keywords {
		if strings.ToLower(strings.TrimSpace(k)) == q {
			return true
		}
	}
	return false
}

// hasKeywordPrefix reports whether one of the keywords starts with q.
func hasKeywordPrefix(keywords []string, q string) bool {
	for _, k := range keywords {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(k)), q) {
			return true
		}
	}
	return false
}

// containsKeywordSubstring reports whether one of the keywords contains q.
func containsKeywordSubstring(keywords []string, q string) bool {
	for _, k := range keywords {
		if strings.Contains(strings.ToLower(k), q) {
			return true
		}
	}
	return false
}

// compileGrowthChecks validates the growth section of a rulebook.
func (b *Book) compileGrowthChecks(op string, raw rawGrowthChecks) error {
	if raw.Default == nil && len(raw.Overrides) == 0 {
		return nil
	}
	b.growth = &compiledGrowthChecks{overrides: map[string]*compiledGrowthCheck{}}
	if raw.Default != nil {
		gc, err := compileGrowthCheck(op, "default", *raw.Default)
		if err != nil {
			return err
		}
		b.growth.def = gc
	}
	for i, o := range raw.Overrides {
		if o.Key == "" {
			return apperr.New(op, apperr.ConfigInvalid,
				fmt.Sprintf("growth_checks override %d has no key", i))
		}
		key, ok := b.GrowthKey(o.Key)
		if !ok {
			return apperr.New(op, apperr.ConfigInvalid,
				"growth_checks override references unknown skill or characteristic "+o.Key)
		}
		if _, dup := b.growth.overrides[key]; dup {
			return apperr.New(op, apperr.ConfigInvalid,
				"duplicate growth_checks override for "+o.Key)
		}
		// Omitted fields are inherited from the default rule.
		merged := rawGrowthCheck{Condition: o.Condition, Gain: o.Gain, Cap: o.Cap}
		if b.growth.def != nil {
			if merged.Condition == "" {
				merged.Condition = b.growth.def.conditionSrc
			}
			if merged.Gain == "" {
				merged.Gain = b.growth.def.gain
			}
			if merged.Cap == nil {
				cap := b.growth.def.cap
				merged.Cap = &cap
			}
		}
		gc, err := compileGrowthCheck(op, o.Key, merged)
		if err != nil {
			return err
		}
		b.growth.overrides[key] = gc
	}
	return nil
}

// compileGrowthCheck validates one growth rule.
func compileGrowthCheck(op, where string, raw rawGrowthCheck) (*compiledGrowthCheck, error) {
	if raw.Condition == "" {
		return nil, apperr.New(op, apperr.ConfigInvalid,
			"growth_checks "+where+" needs a condition")
	}
	if raw.Gain == "" {
		return nil, apperr.New(op, apperr.ConfigInvalid,
			"growth_checks "+where+" needs a gain expression")
	}
	cond, err := compileBoolFormula(raw.Condition, "growth_checks "+where+" condition",
		map[string]bool{"roll": true, "target": true})
	if err != nil {
		return nil, err
	}
	if _, _, err := dice.Parse(raw.Gain, dice.DefaultLimits()); err != nil {
		return nil, apperr.Wrap(op, apperr.ConfigInvalid,
			"growth_checks "+where+" has an invalid gain expression "+raw.Gain, err)
	}
	cap := 0
	if raw.Cap != nil {
		if *raw.Cap < 0 {
			return nil, apperr.New(op, apperr.ConfigInvalid,
				"growth_checks "+where+" has a negative cap")
		}
		cap = *raw.Cap
	}
	return &compiledGrowthCheck{condition: cond, conditionSrc: raw.Condition, gain: raw.Gain, cap: cap}, nil
}

// growthKeyLabel renders a canonical growth key for display.
func growthKeyLabel(key string) string {
	if i := strings.IndexByte(key, ':'); i > 0 {
		return key[i+1:]
	}
	return key
}
