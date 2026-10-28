package engine

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine/formula"
)

// Character generation (Dice! `.coc`, CoC creation rules) is declared in the
// rulebook: the rolled method ("骰点法"), the point-buy method ("购点法",
// used by the detailed mode) and the variants of a game edition.
//
//	generation:
//	  default_variant: "7"
//	  characteristics:            # 骰点法
//	    - { attr: STR, expr: "3d6*5" }
//	    - { attr: SIZ, expr: "(2d6+6)*5" }
//	  variants:                   # 变体（覆盖 characteristics）
//	    - id: "6"
//	      name: "CoC 6 版"
//	      characteristics: [...]
//	  point_buy:                  # 详细作成（购点法）
//	    attributes: [STR, CON, SIZ, DEX, APP, INT, POW, EDU, LUCK]
//	    min_value: 15
//	    budget: "EDU*20 + INT*10"
//	    costs:
//	      - { min: 15, max: 49, base: 7,  points: 1, per: 2 }
//	    priorities:
//	      - { attr: EDU, value: 60 }
//	    skills:
//	      occupation_pool: "EDU*20"
//	      occupation_cap: "EDU*5"
//	      interest_pool: "INT*10"
//	      interest_cap: 75

// rawGeneration is the YAML shape of the generation section.
type rawGeneration struct {
	DefaultVariant  string                 `yaml:"default_variant"`
	Name            string                 `yaml:"name"`
	Characteristics []rawCharacteristic    `yaml:"characteristics"`
	Variants        []rawGenerationVariant `yaml:"variants"`
	PointBuy        *rawPointBuy           `yaml:"point_buy"`
	SkillPoints     []rawSkillPool         `yaml:"skill_points"`
}

// rawCharacteristic is one rolled characteristic.
type rawCharacteristic struct {
	Attr string `yaml:"attr"`
	Expr string `yaml:"expr"`
	// Min/Max clamp the rolled value (optional).
	Min *int `yaml:"min"`
	Max *int `yaml:"max"`
}

// rawGenerationVariant is one edition/variant of the generation rules.
type rawGenerationVariant struct {
	ID              string              `yaml:"id"`
	Name            string              `yaml:"name"`
	Characteristics []rawCharacteristic `yaml:"characteristics"`
	PointBuy        *rawPointBuy        `yaml:"point_buy"`
	SkillPoints     []rawSkillPool      `yaml:"skill_points"`
}

// rawPointBuy declares the point-buy method.
type rawPointBuy struct {
	Attributes []string       `yaml:"attributes"`
	MinValue   int            `yaml:"min_value"`
	Budget     string         `yaml:"budget"`
	Costs      []rawCostBand  `yaml:"costs"`
	Priorities []rawPriority  `yaml:"priorities"`
	Skills     *rawSkillPools `yaml:"skills"`
}

// rawCostBand is one point-cost band: a value v in [min,max] costs
// base + points * ceil((v-from)/per). `from` is the value the base cost
// applies to (it defaults to min); it makes "N points per M points above
// the previous band" expressible.
type rawCostBand struct {
	Min    int `yaml:"min"`
	Max    int `yaml:"max"`
	From   int `yaml:"from"`
	Base   int `yaml:"base"`
	Points int `yaml:"points"`
	Per    int `yaml:"per"`
}

// rawPriority is one entry of the non-interactive allocation order.
type rawPriority struct {
	Attr  string `yaml:"attr"`
	Value int    `yaml:"value"`
}

// rawSkillPools declares the skill caps of the point-buy method. The
// remaining points (budget minus the characteristics) are spent on skills:
// occupation skills up to occupation_cap, interest skills up to
// interest_cap.
type rawSkillPools struct {
	OccupationCap string `yaml:"occupation_cap"`
	InterestCap   *int   `yaml:"interest_cap"`
}

// compiledCharacteristic is one rolled characteristic.
type compiledCharacteristic struct {
	attr string
	expr string
	min  *int
	max  *int
}

// compiledCostBand is one validated cost band.
type compiledCostBand struct {
	min, max int
	from     int
	base     int
	points   int
	per      int
}

// cost returns the point cost of a value in this band.
func (b compiledCostBand) cost(v int) int {
	steps := (v - b.from + b.per - 1) / b.per // ceil
	if steps < 0 {
		steps = 0
	}
	return b.base + b.points*steps
}

// compiledPointBuy is the validated point-buy method.
type compiledPointBuy struct {
	attributes []string
	minValue   int
	budget     *formula.Expr
	// budgetSrc keeps the declared budget formula (the quick reference
	// quotes it).
	budgetSrc  string
	costs      []compiledCostBand
	priorities []rawPriority

	occupationCap *formula.Expr
	interestCap   *int
}

// compiledGeneration is the validated generation section.
type compiledGeneration struct {
	defaultVariant string
	variants       map[string]*compiledVariant
	order          []string
}

// compiledVariant is one generation variant.
type compiledVariant struct {
	id              string
	name            string
	characteristics []compiledCharacteristic
	pointBuy        *compiledPointBuy
	skillPools      []compiledSkillPool
}

// GenerationInput parameterizes one generation request.
type GenerationInput struct {
	// VariantID selects the generation variant (empty = the default).
	VariantID string
	// Count is the number of characters to generate.
	Count int
	// Detailed switches to the point-buy method (the "detailed creation").
	Detailed bool
}

// GeneratedCharacter is one generated character sheet.
type GeneratedCharacter struct {
	Attributes  map[string]int     `json:"attributes"`
	Rolls       []*dice.RollResult `json:"rolls,omitempty"`
	PointBuy    *PointBuyReport    `json:"point_buy,omitempty"`
	Explanation string             `json:"explanation"`
}

// PointBuyReport describes how the point budget was spent.
type PointBuyReport struct {
	Budget int `json:"budget"`
	Spent  int `json:"spent"`
	// Left are the points still available for skills.
	Left          int            `json:"left"`
	Costs         map[string]int `json:"costs"`
	OccupationCap int            `json:"occupation_cap"`
	InterestCap   int            `json:"interest_cap"`
}

// GenerationResult is the structured outcome of a generation request.
type GenerationResult struct {
	Kind        string                `json:"kind"`
	SystemID    string                `json:"system_id"`
	VariantID   string                `json:"variant_id"`
	VariantName string                `json:"variant_name"`
	Detailed    bool                  `json:"detailed"`
	Characters  []*GeneratedCharacter `json:"characters"`
}

// MaxGeneratedCharacters bounds one generation request.
const MaxGeneratedCharacters = 20

// Generation returns the compiled generation section (nil when the rulebook
// declares none).
func (b *Book) Generation() *compiledGeneration { return b.generation }

// GenerationVariants returns the declared variant ids in declaration order.
func (b *Book) GenerationVariants() []string {
	if b.generation == nil {
		return nil
	}
	out := make([]string, len(b.generation.order))
	copy(out, b.generation.order)
	return out
}

// Generate creates characters according to the rulebook's generation rules.
// The rolled method rolls the declared expressions; the detailed method
// spends the point-buy budget on the declared priorities. The engine never
// touches stored sheets: it returns the values for the application layer.
func (b *Book) Generate(ctx context.Context, in GenerationInput) (*GenerationResult, error) {
	const op = "engine.Generate"
	if b.generation == nil {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"rulebook "+b.manifest.ID+" declares no character generation")
	}
	if in.Count < 1 {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"generation count must be at least 1")
	}
	if in.Count > MaxGeneratedCharacters {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"generation count must not exceed "+strconv.Itoa(MaxGeneratedCharacters))
	}
	variant, err := b.generationVariant(in.VariantID)
	if err != nil {
		return nil, err
	}
	if in.Detailed && variant.pointBuy == nil {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"generation variant "+variant.id+" declares no point-buy method")
	}
	res := &GenerationResult{
		Kind:        "generation",
		SystemID:    b.manifest.ID,
		VariantID:   variant.id,
		VariantName: variant.name,
		Detailed:    in.Detailed,
	}
	for i := 0; i < in.Count; i++ {
		c, err := b.generateOne(ctx, variant, in.Detailed)
		if err != nil {
			return nil, err
		}
		res.Characters = append(res.Characters, c)
	}
	return res, nil
}

// generationVariant resolves a variant id (empty = the default one).
func (b *Book) generationVariant(id string) (*compiledVariant, error) {
	const op = "engine.generationVariant"
	if id == "" {
		id = b.generation.defaultVariant
	}
	v, ok := b.generation.variants[id]
	if !ok {
		return nil, apperr.New(op, apperr.InvalidArgument, "unknown generation variant: "+id)
	}
	return v, nil
}

// generateOne creates a single character.
func (b *Book) generateOne(ctx context.Context, v *compiledVariant, detailed bool) (*GeneratedCharacter, error) {
	if detailed {
		return b.generatePointBuy(v)
	}
	c := &GeneratedCharacter{Attributes: map[string]int{}}
	for _, ch := range v.characteristics {
		roll, err := b.roller.Roll(ctx, ch.expr)
		if err != nil {
			return nil, err
		}
		value := roll.Total
		if ch.min != nil && value < *ch.min {
			value = *ch.min
		}
		if ch.max != nil && value > *ch.max {
			value = *ch.max
		}
		c.Attributes[ch.attr] = value
		c.Rolls = append(c.Rolls, roll)
	}
	c.Explanation = "骰点法生成：" + formatAttributeValues(c.Attributes)
	return c, nil
}

// generatePointBuy spends the point budget on the declared priorities.
func (b *Book) generatePointBuy(v *compiledVariant) (*GeneratedCharacter, error) {
	pb := v.pointBuy
	values, report, err := pb.allocate()
	if err != nil {
		return nil, err
	}
	c := &GeneratedCharacter{Attributes: values, PointBuy: report}
	c.Explanation = "购点法生成：" + formatAttributeValues(values)
	return c, nil
}

// allocate spends the point-buy budget on the declared priorities and
// returns the bought values together with a report.
func (pb *compiledPointBuy) allocate() (map[string]int, *PointBuyReport, error) {
	const op = "engine.allocate"
	if _, ok := pb.costOf(pb.minValue); !ok {
		return nil, nil, apperr.New(op, apperr.InvalidArgument,
			"point-buy cost table has no band for value "+strconv.Itoa(pb.minValue))
	}
	// Targets: the declared priorities, or the minimum value.
	targets := map[string]int{}
	for _, p := range pb.priorities {
		targets[p.Attr] = p.Value
	}

	// The budget depends on the bought EDU/INT, so allocate, recompute the
	// budget and repeat until it settles (at most three rounds).
	budget := pb.budgetOf(targets)
	values := map[string]int{}
	costs := map[string]int{}
	for round := 0; round < 3; round++ {
		values, costs = pb.allocateWith(budget, targets)
		next := pb.budgetOf(values)
		if next == budget {
			break
		}
		budget = next
	}

	spentTotal := 0
	for _, attr := range pb.attributes {
		c, ok := pb.costOf(values[attr])
		if !ok {
			return nil, nil, apperr.New(op, apperr.InvalidArgument,
				"point-buy cost table has no band for value "+strconv.Itoa(values[attr]))
		}
		costs[attr] = c
		spentTotal += c
	}
	report := &PointBuyReport{
		Budget: budget,
		Spent:  spentTotal,
		Left:   budget - spentTotal,
		Costs:  costs,
	}
	env := formula.Env{}
	for k, v := range values {
		env[k] = v
	}
	if pb.occupationCap != nil {
		if v, err := pb.occupationCap.EvalInt(env); err == nil {
			report.OccupationCap = v
		}
	}
	if pb.interestCap != nil {
		report.InterestCap = *pb.interestCap
	}
	return values, report, nil
}

// allocateWith buys the characteristics within the given budget: every
// attribute starts at its minimum value (its cost is reserved) and the
// remaining points are spent on the declared priorities, in order.
func (pb *compiledPointBuy) allocateWith(budget int, targets map[string]int) (map[string]int, map[string]int) {
	values := map[string]int{}
	costs := map[string]int{}
	base, _ := pb.costOf(pb.minValue)
	remaining := budget
	for _, attr := range pb.attributes {
		values[attr] = pb.minValue
		costs[attr] = base
		remaining -= base
	}
	for _, attr := range pb.priorityOrder() {
		target, ok := targets[attr]
		if !ok || target <= pb.minValue {
			continue
		}
		cost, ok := pb.costOf(target)
		if !ok {
			continue
		}
		if cost-base <= remaining {
			values[attr] = target
			costs[attr] = cost
			remaining -= cost - base
			continue
		}
		// Not affordable: take the highest value that is.
		for v := target - 1; v > pb.minValue; v-- {
			c, ok := pb.costOf(v)
			if !ok {
				continue
			}
			if c-base <= remaining {
				values[attr] = v
				costs[attr] = c
				remaining -= c - base
				break
			}
		}
	}
	return values, costs
}

// priorityOrder returns the allocation order: EDU and INT first (they
// define the budget), then the declared priorities, then the remaining
// attributes in declaration order.
func (pb *compiledPointBuy) priorityOrder() []string {
	out := make([]string, 0, len(pb.attributes))
	seen := map[string]bool{}
	for _, attr := range []string{"EDU", "INT"} {
		for _, p := range pb.priorities {
			if p.Attr == attr && !seen[attr] {
				seen[attr] = true
				out = append(out, attr)
			}
		}
	}
	for _, p := range pb.priorities {
		if !seen[p.Attr] {
			seen[p.Attr] = true
			out = append(out, p.Attr)
		}
	}
	for _, attr := range pb.attributes {
		if !seen[attr] {
			seen[attr] = true
			out = append(out, attr)
		}
	}
	return out
}

// budgetOf evaluates the budget formula over the given values.
func (pb *compiledPointBuy) budgetOf(values map[string]int) int {
	env := formula.Env{}
	for k, v := range values {
		env[k] = v
	}
	v, err := pb.budget.EvalInt(env)
	if err != nil {
		return 0
	}
	return v
}

// baseCostOf returns the cost of an attribute at its minimum value.
func (pb *compiledPointBuy) baseCostOf(attr string) int {
	c, _ := pb.costOf(pb.minValue)
	return c
}

// costOf returns the point cost of a value.
func (pb *compiledPointBuy) costOf(value int) (int, bool) {
	for _, band := range pb.costs {
		if value >= band.min && value <= band.max {
			return band.cost(value), true
		}
	}
	return 0, false
}

// compileGeneration validates the generation section of a rulebook.
func (b *Book) compileGeneration(op string, raw *rawGeneration) error {
	if raw == nil {
		return nil
	}
	g := &compiledGeneration{variants: map[string]*compiledVariant{}}
	if raw.DefaultVariant != "" {
		g.defaultVariant = raw.DefaultVariant
	}
	defaults, err := b.compileCharacteristics(op, raw.Characteristics)
	if err != nil {
		return err
	}
	basePointBuy, err := b.compilePointBuy(op, raw.PointBuy)
	if err != nil {
		return err
	}
	baseSkillPools, err := b.compileSkillPools(op, raw.SkillPoints)
	if err != nil {
		return err
	}
	for _, rv := range raw.Variants {
		if rv.ID == "" {
			return apperr.New(op, apperr.ConfigInvalid, "generation variant has no id")
		}
		if _, dup := g.variants[rv.ID]; dup {
			return apperr.New(op, apperr.ConfigInvalid, "duplicate generation variant "+rv.ID)
		}
		chars := defaults.characteristics
		if len(rv.Characteristics) > 0 {
			compiled, err := b.compileCharacteristics(op, rv.Characteristics)
			if err != nil {
				return err
			}
			chars = compiled.characteristics
		}
		pb := basePointBuy
		if rv.PointBuy != nil {
			pb, err = b.compilePointBuy(op, rv.PointBuy)
			if err != nil {
				return err
			}
		}
		pools := baseSkillPools
		if len(rv.SkillPoints) > 0 {
			pools, err = b.compileSkillPools(op, rv.SkillPoints)
			if err != nil {
				return err
			}
		}
		v := &compiledVariant{id: rv.ID, name: rv.Name, characteristics: chars, pointBuy: pb, skillPools: pools}
		if v.name == "" {
			v.name = rv.ID
		}
		g.variants[rv.ID] = v
		g.order = append(g.order, rv.ID)
	}
	if g.defaultVariant == "" && len(g.order) > 0 {
		g.defaultVariant = g.order[0]
	}
	if _, ok := g.variants[g.defaultVariant]; !ok {
		// The default characteristics form the default variant.
		id := g.defaultVariant
		if id == "" {
			id = "default"
		}
		name := raw.Name
		if name == "" {
			name = id
		}
		g.variants[id] = &compiledVariant{
			id:              id,
			name:            name,
			characteristics: defaults.characteristics,
			pointBuy:        basePointBuy,
			skillPools:      baseSkillPools,
		}
		g.order = append([]string{id}, g.order...)
		g.defaultVariant = id
	}
	if _, ok := g.variants[g.defaultVariant]; !ok {
		return apperr.New(op, apperr.ConfigInvalid,
			"generation.default_variant "+g.defaultVariant+" is not a declared variant")
	}
	b.generation = g
	// The quick reference describes the point-buy method of the default
	// variant.
	b.pointBuy = g.variants[g.defaultVariant].pointBuy
	return nil
}

// compiledCharacteristics bundles the characteristics and the point-buy
// method of one generation profile.
type compiledCharacteristics struct {
	characteristics []compiledCharacteristic
	pointBuy        *compiledPointBuy
}

// compileCharacteristics validates the rolled characteristics.
func (b *Book) compileCharacteristics(op string, raw []rawCharacteristic) (compiledCharacteristics, error) {
	out := compiledCharacteristics{}
	if len(raw) == 0 {
		return out, nil
	}
	seen := map[string]bool{}
	for i, rc := range raw {
		where := fmt.Sprintf("generation characteristic %d", i)
		if rc.Attr == "" || rc.Expr == "" {
			return out, apperr.New(op, apperr.ConfigInvalid, where+" needs an attr and an expr")
		}
		def, ok := b.FindAttribute(rc.Attr)
		if !ok {
			return out, apperr.New(op, apperr.ConfigInvalid,
				where+" references unknown characteristic "+rc.Attr)
		}
		if seen[def.ID] {
			return out, apperr.New(op, apperr.ConfigInvalid,
				"duplicate generation characteristic "+def.ID)
		}
		seen[def.ID] = true
		if _, _, err := dice.ExprBounds(rc.Expr, dice.DefaultLimits()); err != nil {
			return out, apperr.Wrap(op, apperr.ConfigInvalid,
				where+" has an invalid expression "+rc.Expr, err)
		}
		out.characteristics = append(out.characteristics, compiledCharacteristic{
			attr: def.ID,
			expr: rc.Expr,
			min:  rc.Min,
			max:  rc.Max,
		})
	}
	return out, nil
}

// compilePointBuy validates the point-buy method.
func (b *Book) compilePointBuy(op string, raw *rawPointBuy) (*compiledPointBuy, error) {
	if raw == nil {
		return nil, nil
	}
	if len(raw.Attributes) == 0 {
		return nil, apperr.New(op, apperr.ConfigInvalid, "generation.point_buy needs attributes")
	}
	if raw.Budget == "" {
		return nil, apperr.New(op, apperr.ConfigInvalid, "generation.point_buy needs a budget formula")
	}
	if len(raw.Costs) == 0 {
		return nil, apperr.New(op, apperr.ConfigInvalid, "generation.point_buy needs cost bands")
	}
	attrVars := map[string]bool{}
	for _, a := range b.attrs {
		attrVars[a.ID] = true
	}
	pb := &compiledPointBuy{minValue: raw.MinValue, budgetSrc: raw.Budget}
	if pb.minValue <= 0 {
		pb.minValue = 1
	}
	for _, attr := range raw.Attributes {
		def, ok := b.FindAttribute(attr)
		if !ok {
			return nil, apperr.New(op, apperr.ConfigInvalid,
				"generation.point_buy references unknown characteristic "+attr)
		}
		if !attrVars[def.ID] {
			attrVars[def.ID] = true
		}
		pb.attributes = append(pb.attributes, def.ID)
	}
	budget, err := compileIntFormula(raw.Budget, "generation.point_buy budget", attrVars)
	if err != nil {
		return nil, err
	}
	pb.budget = budget

	bands := make([]compiledCostBand, 0, len(raw.Costs))
	for i, rc := range raw.Costs {
		where := fmt.Sprintf("generation.point_buy cost band %d", i)
		if rc.Min > rc.Max {
			return nil, apperr.New(op, apperr.ConfigInvalid, where+" has min > max")
		}
		if rc.Per <= 0 {
			return nil, apperr.New(op, apperr.ConfigInvalid, where+" needs a positive per")
		}
		if rc.Points < 0 || rc.Base < 0 {
			return nil, apperr.New(op, apperr.ConfigInvalid, where+" has a negative cost")
		}
		from := rc.From
		if from == 0 {
			from = rc.Min
		}
		if from > rc.Max {
			return nil, apperr.New(op, apperr.ConfigInvalid,
				where+" has from > max")
		}
		bands = append(bands, compiledCostBand{
			min: rc.Min, max: rc.Max, from: from,
			base: rc.Base, points: rc.Points, per: rc.Per,
		})
	}
	sort.Slice(bands, func(i, j int) bool { return bands[i].min < bands[j].min })
	for i := 1; i < len(bands); i++ {
		if bands[i].min <= bands[i-1].max {
			return nil, apperr.New(op, apperr.ConfigInvalid,
				"generation.point_buy cost bands overlap")
		}
	}
	pb.costs = bands

	for _, p := range raw.Priorities {
		def, ok := b.FindAttribute(p.Attr)
		if !ok {
			return nil, apperr.New(op, apperr.ConfigInvalid,
				"generation.point_buy priority references unknown characteristic "+p.Attr)
		}
		pb.priorities = append(pb.priorities, rawPriority{Attr: def.ID, Value: p.Value})
	}
	if raw.Skills != nil {
		if raw.Skills.OccupationCap != "" {
			pb.occupationCap, err = compileIntFormula(raw.Skills.OccupationCap,
				"generation.point_buy skills occupation_cap", attrVars)
			if err != nil {
				return nil, err
			}
		}
		if raw.Skills.InterestCap != nil {
			pb.interestCap = raw.Skills.InterestCap
		}
	}
	return pb, nil
}

// formatAttributeValues renders an attribute map for display.
func formatAttributeValues(values map[string]int) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+" "+strconv.Itoa(values[k]))
	}
	return strings.Join(parts, "，")
}
