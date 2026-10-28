package app

import (
	"context"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
	"dicegirl/internal/engine"
)

// GenerationVariantMetaKey is the card metadata entry recording which
// generation variant created the card. The skill-point pools are declared
// per variant, so the card must remember its own variant.
const GenerationVariantMetaKey = "generation_variant"

// SkillSpend is one KEY±N entry of an allocation request (the chat token
// "侦查+40"): a positive N spends pool points, a negative N refunds them.
type SkillSpend struct {
	Skill  string
	Points int
}

// ParseSkillSpend parses "侦查+40" or "侦查-10". The sign comes from the
// shape; the right-hand side must be a plain integer (allocation points are
// exact, never rolled).
func ParseSkillSpend(s string) (SkillSpend, error) {
	for i := 1; i < len(s); i++ {
		if s[i] != '+' && s[i] != '-' {
			continue
		}
		key := strings.TrimSpace(s[:i])
		raw := strings.TrimSpace(s[i+1:])
		if key == "" || raw == "" {
			break
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			return SkillSpend{}, apperr.Wrap("app.ParseSkillSpend", apperr.InvalidArgument,
				"技能加点数必须是整数："+s, err)
		}
		points := n
		if s[i] == '-' {
			points = -n
		}
		return SkillSpend{Skill: key, Points: points}, nil
	}
	return SkillSpend{}, apperr.New("app.ParseSkillSpend", apperr.InvalidArgument,
		"加点格式：<技能>±<点数>（如 侦查+40），got "+s)
}

// SkillPointsView is the skill-point pool report of one card.
type SkillPointsView struct {
	Kind        string                   `json:"kind"`
	CharacterID string                   `json:"character_id,omitempty"`
	VariantID   string                   `json:"variant_id"`
	Pools       []engine.SkillPoolReport `json:"pools"`
}

// SkillPointPools reports the card's skill-point pools: the points totals,
// what is already spent and the per-skill caps. The variant comes from the
// card's creation metadata (empty = the rulebook's default variant).
func (a *App) SkillPointPools(ctx context.Context, id string) (*SkillPointsView, error) {
	const op = "app.SkillPointPools"
	c, err := a.GetCharacter(ctx, id)
	if err != nil {
		return nil, err
	}
	book, err := a.selectSystem(ctx, "", c)
	if err != nil {
		return nil, err
	}
	variant := generationVariantOf(c)
	pools, err := book.SkillPointPools(variant, c)
	if err != nil {
		return nil, err
	}
	if pools == nil {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"规则书 "+c.SystemID+" 未声明技能点池（generation.skill_points）")
	}
	return &SkillPointsView{Kind: "skill_points", CharacterID: c.ID, VariantID: variant, Pools: pools}, nil
}

// AllocateSkillPoints spends points from one pool on the active card (the
// card addressed by id when not empty): the engine settles the spends in
// place, then the sheet goes through the usual validation, backup and
// atomic-update path. Nothing is written back when any spend is invalid.
func (a *App) AllocateSkillPoints(ctx context.Context, id, pool string, spends []SkillSpend) (*character.Character, []engine.SkillAllocationResult, *SkillPointsView, error) {
	const op = "app.AllocateSkillPoints"
	if len(spends) == 0 {
		return nil, nil, nil, apperr.New(op, apperr.InvalidArgument, "no skill spends given")
	}
	c, err := a.GetCharacter(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	book, err := a.selectSystem(ctx, "", c)
	if err != nil {
		return nil, nil, nil, err
	}
	variant := generationVariantOf(c)
	engineSpends := make([]engine.SkillSpend, len(spends))
	for i, s := range spends {
		engineSpends[i] = engine.SkillSpend{Skill: s.Skill, Points: s.Points}
	}
	results, err := book.AllocateSkillPoints(variant, pool, c, engineSpends)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, nil, nil, err
	}
	if err := a.validateBySystem(ctx, book, c); err != nil {
		return nil, nil, nil, err
	}
	if err := a.chars.Update(ctx, c); err != nil {
		return nil, nil, nil, err
	}
	pools, err := book.SkillPointPools(variant, c)
	if err != nil {
		return nil, nil, nil, err
	}
	return c, results, &SkillPointsView{Kind: "skill_points", CharacterID: c.ID, VariantID: variant, Pools: pools}, nil
}

// ClearSkillPoints resets the card's skill-point allocation: every skill
// that received points falls back to max(rulebook base, value minus the
// points allocated to it) and the allocation bookkeeping is emptied. The
// deterministic rollback makes a mis-clicked creation re-allocation safe.
func (a *App) ClearSkillPoints(ctx context.Context, id string) (*character.Character, *SkillPointsView, error) {
	const op = "app.ClearSkillPoints"
	c, err := a.GetCharacter(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	book, err := a.selectSystem(ctx, "", c)
	if err != nil {
		return nil, nil, err
	}
	for _, spends := range c.SkillPoints {
		for skillID, n := range spends {
			after := c.Skills[skillID] - n
			if def, ok := book.FindSkill(skillID); ok {
				if base, err := book.SkillBase(def, c.Attributes); err == nil && after < base {
					after = base
				}
			}
			if after < 0 {
				after = 0
			}
			if c.Skills == nil {
				c.Skills = map[string]int{}
			}
			c.Skills[skillID] = after
		}
	}
	c.SkillPoints = nil
	if err := c.Validate(); err != nil {
		return nil, nil, err
	}
	if err := a.validateBySystem(ctx, book, c); err != nil {
		return nil, nil, err
	}
	if err := a.chars.Update(ctx, c); err != nil {
		return nil, nil, err
	}
	variant := generationVariantOf(c)
	pools, err := book.SkillPointPools(variant, c)
	if err != nil {
		return nil, nil, err
	}
	return c, &SkillPointsView{Kind: "skill_points", CharacterID: c.ID, VariantID: variant, Pools: pools}, nil
}

// generationVariantOf returns the variant the card was generated with; an
// empty string selects the rulebook's default variant.
func generationVariantOf(c *character.Character) string {
	if c == nil {
		return ""
	}
	return c.Metadata[GenerationVariantMetaKey]
}
