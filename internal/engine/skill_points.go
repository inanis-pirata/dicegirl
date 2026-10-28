package engine

import (
	"fmt"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
	"dicegirl/internal/engine/formula"
)

// Skill-point pools (作成后的技能加点) are declared in the generation
// section: once the characteristics exist, the player distributes each
// pool's points over the skills. The pools are creation data — the engine
// evaluates them against a sheet and settles the spending in place, the
// application layer validates and persists the sheet.
//
//	generation:
//	  skill_points:
//	    - id: occupation
//	      name: "职业技能点"
//	      aliases: ["职业"]
//	      points: "EDU*4"          # 公式（变量 = 属性）
//	      cap: 90                  # 该池加点的单技能值上限
//	      cap_overrides:
//	        - { skill: firearms_handgun, cap: 50 }

// rawSkillPool is the YAML shape of one skill-point pool.
type rawSkillPool struct {
	ID           string                `yaml:"id"`
	Name         string                `yaml:"name"`
	Aliases      []string              `yaml:"aliases"`
	Points       string                `yaml:"points"`
	Cap          int                   `yaml:"cap"`
	CapOverrides []rawSkillCapOverride `yaml:"cap_overrides"`
}

// rawSkillCapOverride is one per-skill exception to the pool cap.
type rawSkillCapOverride struct {
	Skill string `yaml:"skill"`
	Cap   int    `yaml:"cap"`
}

// compiledSkillPool is one validated skill-point pool.
type compiledSkillPool struct {
	id      string
	name    string
	aliases []string
	points  *formula.Expr
	// cap is the per-skill value cap reachable with this pool; 0 means
	// "no declared cap" (the sheet skill limit applies).
	cap          int
	capOverrides map[string]int
}

// SkillPoolReport is one pool's numbers for a sheet: the points total, how
// much is already spent (the card's skill_points bookkeeping) and the
// per-skill cap the pool enforces.
type SkillPoolReport struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Points int    `json:"points"`
	Spent  int    `json:"spent"`
	Left   int    `json:"left"`
	Cap    int    `json:"cap"`
}

// SkillSpend is one KEY±N entry of an allocation request. The skill key is
// user-typed (id, alias or localized name) and resolved against the
// rulebook; a positive N spends pool points, a negative N refunds them.
type SkillSpend struct {
	Skill  string
	Points int
}

// SkillAllocationResult reports one settled spend.
type SkillAllocationResult struct {
	Pool      string `json:"pool"`
	PoolName  string `json:"pool_name"`
	Skill     string `json:"skill"`
	SkillName string `json:"skill_name"`
	// Base is the rulebook starting value of the skill.
	Base int `json:"base"`
	// FromBase reports whether the skill was not on the sheet and the
	// spend therefore started from its base value.
	FromBase  bool `json:"from_base"`
	Before    int  `json:"before"`
	After     int  `json:"after"`
	Delta     int  `json:"delta"`
	Cap       int  `json:"cap"`
	PoolTotal int  `json:"pool_total"`
	PoolSpent int  `json:"pool_spent"`
	PoolLeft  int  `json:"pool_left"`
}

// SkillPointPools reports every pool the variant declares against the
// sheet (variantID empty = the rulebook's default variant). A nil result
// means the variant declares no pools.
func (b *Book) SkillPointPools(variantID string, c *character.Character) ([]SkillPoolReport, error) {
	const op = "engine.SkillPointPools"
	if c == nil {
		return nil, apperr.New(op, apperr.InvalidArgument, "nil character")
	}
	pools, err := b.skillPools(variantID)
	if err != nil {
		return nil, err
	}
	if len(pools) == 0 {
		return nil, nil
	}
	out := make([]SkillPoolReport, 0, len(pools))
	for i := range pools {
		p := &pools[i]
		points, err := b.poolPoints(p, c)
		if err != nil {
			return nil, err
		}
		spent := poolSpent(c, p.id)
		out = append(out, SkillPoolReport{
			ID: p.id, Name: p.name,
			Points: points, Spent: spent, Left: points - spent,
			Cap: b.poolCap(p, ""),
		})
	}
	return out, nil
}

// AllocateSkillPoints settles spends from one pool in place on the sheet:
// skill values move (missing skills start from their rulebook base) and the
// card's skill_points bookkeeping follows. Any invalid spend aborts before
// the sheet is persisted — the caller only writes back on success.
func (b *Book) AllocateSkillPoints(variantID, poolKey string, c *character.Character, spends []SkillSpend) ([]SkillAllocationResult, error) {
	const op = "engine.AllocateSkillPoints"
	if c == nil {
		return nil, apperr.New(op, apperr.InvalidArgument, "nil character")
	}
	if len(spends) == 0 {
		return nil, apperr.New(op, apperr.InvalidArgument, "no skill spends given")
	}
	pools, err := b.skillPools(variantID)
	if err != nil {
		return nil, err
	}
	if len(pools) == 0 {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"rulebook "+b.manifest.ID+" declares no skill-point pools (generation.skill_points)")
	}
	pool, ok := findSkillPool(pools, poolKey)
	if !ok {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"unknown skill-point pool: "+poolKey+"（可用："+poolKeyList(pools)+"）")
	}
	total, err := b.poolPoints(pool, c)
	if err != nil {
		return nil, err
	}
	spent := poolSpent(c, pool.id)

	results := make([]SkillAllocationResult, 0, len(spends))
	for _, s := range spends {
		if s.Points == 0 {
			return nil, apperr.New(op, apperr.InvalidArgument,
				"技能加点数不能为 0："+s.Skill)
		}
		def, ok := b.FindSkill(s.Skill)
		if !ok {
			return nil, apperr.New(op, apperr.InvalidArgument, "unknown skill: "+s.Skill)
		}
		base, err := b.SkillBase(def, c.Attributes)
		if err != nil {
			return nil, err
		}
		current, onCard := c.Skills[def.ID]
		if !onCard {
			current = base
		}
		after := current + s.Points
		cap := b.poolCap(pool, def.ID)
		res := SkillAllocationResult{
			Pool: pool.id, PoolName: pool.name,
			Skill: def.ID, SkillName: def.Name,
			Base: base, FromBase: !onCard,
			Before: current, After: after, Delta: s.Points, Cap: cap,
		}
		if s.Points > 0 {
			if left := total - spent; s.Points > left {
				return nil, apperr.New(op, apperr.InvalidArgument,
					fmt.Sprintf("%s余量不足：还要 %d 点，剩 %d 点", pool.name, s.Points, left))
			}
			if after > cap {
				return nil, apperr.New(op, apperr.InvalidArgument,
					fmt.Sprintf("%s 不能超过上限 %d（%d + %d）", def.Name, cap, current, s.Points))
			}
		} else {
			allocated := c.SkillPoints[pool.id][def.ID]
			if n := -s.Points; n > allocated {
				return nil, apperr.New(op, apperr.InvalidArgument,
					fmt.Sprintf("%s 在%s里只分配了 %d 点，退不出 %d 点", def.Name, pool.name, allocated, n))
			}
			if after < base {
				return nil, apperr.New(op, apperr.InvalidArgument,
					fmt.Sprintf("%s 退点后低于基础值 %d（%d - %d）", def.Name, base, current, -s.Points))
			}
		}
		if !b.limits.skill.Contains(after) {
			return nil, apperr.New(op, apperr.InvalidArgument,
				fmt.Sprintf("%s %d 超出技能值范围 [%d,%d]",
					def.Name, after, b.limits.skill.Min, b.limits.skill.Max))
		}
		if c.Skills == nil {
			c.Skills = map[string]int{}
		}
		c.Skills[def.ID] = after
		if c.SkillPoints == nil {
			c.SkillPoints = map[string]map[string]int{}
		}
		inner := c.SkillPoints[pool.id]
		if inner == nil {
			inner = map[string]int{}
			c.SkillPoints[pool.id] = inner
		}
		inner[def.ID] += s.Points
		if inner[def.ID] <= 0 {
			delete(inner, def.ID)
		}
		if len(inner) == 0 {
			delete(c.SkillPoints, pool.id)
		}
		spent += s.Points
		res.PoolTotal, res.PoolSpent, res.PoolLeft = total, spent, total-spent
		results = append(results, res)
	}
	return results, nil
}

// skillPools returns the pools of one generation variant (empty = the
// default variant); nil when the rulebook declares none.
func (b *Book) skillPools(variantID string) ([]compiledSkillPool, error) {
	if b.generation == nil {
		return nil, apperr.New("engine.skillPools", apperr.InvalidArgument,
			"rulebook "+b.manifest.ID+" declares no character generation")
	}
	variant, err := b.generationVariant(variantID)
	if err != nil {
		return nil, err
	}
	return variant.skillPools, nil
}

// poolPoints evaluates a pool's points formula over the sheet's attributes.
func (b *Book) poolPoints(p *compiledSkillPool, c *character.Character) (int, error) {
	env := formula.Env{}
	for k, v := range c.Attributes {
		env[k] = v
	}
	v, err := p.points.EvalInt(env)
	if err != nil {
		return 0, apperr.Wrap("engine.poolPoints", apperr.InvalidArgument,
			"无法计算"+p.name+"（属性不全？）", err)
	}
	return v, nil
}

// poolCap returns the value cap a pool enforces on one skill: the per-skill
// override, the pool cap, or the sheet skill limit when neither is declared.
func (b *Book) poolCap(p *compiledSkillPool, skillID string) int {
	if skillID != "" {
		if c, ok := p.capOverrides[skillID]; ok {
			return c
		}
	}
	if p.cap > 0 {
		return p.cap
	}
	return b.limits.skill.Max
}

// poolSpent sums the card's allocation bookkeeping of one pool.
func poolSpent(c *character.Character, pool string) int {
	total := 0
	for _, n := range c.SkillPoints[pool] {
		total += n
	}
	return total
}

// findSkillPool resolves a pool key: id (case-insensitive), then alias,
// then name.
func findSkillPool(pools []compiledSkillPool, name string) (*compiledSkillPool, bool) {
	for i := range pools {
		if strings.EqualFold(pools[i].id, name) {
			return &pools[i], true
		}
	}
	for i := range pools {
		for _, a := range pools[i].aliases {
			if strings.EqualFold(a, name) {
				return &pools[i], true
			}
		}
	}
	for i := range pools {
		if strings.EqualFold(pools[i].name, name) {
			return &pools[i], true
		}
	}
	return nil, false
}

// poolKeyList renders the pool keys for error messages.
func poolKeyList(pools []compiledSkillPool) string {
	parts := make([]string, 0, len(pools))
	for _, p := range pools {
		parts = append(parts, p.id)
	}
	return strings.Join(parts, "、")
}

// compileSkillPools validates the skill-point pools of one generation
// profile.
func (b *Book) compileSkillPools(op string, raw []rawSkillPool) ([]compiledSkillPool, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	attrVars := map[string]bool{}
	for _, a := range b.attrs {
		attrVars[a.ID] = true
	}
	seen := map[string]bool{}
	out := make([]compiledSkillPool, 0, len(raw))
	for i, rp := range raw {
		where := fmt.Sprintf("generation.skill_points %d", i)
		if rp.ID == "" {
			return nil, apperr.New(op, apperr.ConfigInvalid, where+" needs an id")
		}
		if seen[rp.ID] {
			return nil, apperr.New(op, apperr.ConfigInvalid,
				"duplicate generation.skill_points pool "+rp.ID)
		}
		seen[rp.ID] = true
		if rp.Points == "" {
			return nil, apperr.New(op, apperr.ConfigInvalid, where+" needs a points formula")
		}
		points, err := compileIntFormula(rp.Points, where+" points", attrVars)
		if err != nil {
			return nil, err
		}
		if rp.Cap < 0 {
			return nil, apperr.New(op, apperr.ConfigInvalid, where+" has a negative cap")
		}
		p := compiledSkillPool{
			id: rp.ID, name: rp.Name, aliases: rp.Aliases,
			points: points, cap: rp.Cap,
			capOverrides: map[string]int{},
		}
		if p.name == "" {
			p.name = p.id
		}
		for _, o := range rp.CapOverrides {
			def, ok := b.FindSkill(o.Skill)
			if !ok {
				return nil, apperr.New(op, apperr.ConfigInvalid,
					where+" cap override references unknown skill "+o.Skill)
			}
			if o.Cap <= 0 {
				return nil, apperr.New(op, apperr.ConfigInvalid,
					where+" cap override for "+o.Skill+" needs a positive cap")
			}
			if _, dup := p.capOverrides[def.ID]; dup {
				return nil, apperr.New(op, apperr.ConfigInvalid,
					where+" has duplicate cap overrides for "+o.Skill)
			}
			p.capOverrides[def.ID] = o.Cap
		}
		out = append(out, p)
	}
	return out, nil
}
