package engine

import (
	"fmt"
	"strings"
)

// The quick reference (Dice!'s rules dictionary) has two sources:
//
//  1. the rulebook's structured data — the success levels, the difficulty
//     modifiers, the damage-bonus table, the point-buy table. Those entries
//     are *derived* from the data, so they can never drift from the rules
//     the engine actually applies;
//  2. the rulebook's own `rules:` entries — the narrative rules (how to
//     create an investigator, what a wound means, ...).
//
// A hand-written entry with the same id replaces the derived one, so a
// book can provide nicer prose for a derived topic.

// derivedRules builds the quick-reference entries that are derived from the
// rulebook's structured data.
func (b *Book) derivedRules() []RuleEntry {
	out := []RuleEntry{}
	out = append(out, b.derivedLevelRules()...)
	out = append(out, b.derivedDifficultyRules()...)
	out = append(out, b.derivedDamageRules()...)
	out = append(out, b.derivedPointBuyRules()...)
	out = append(out, b.derivedCharacteristicRules()...)
	return out
}

// derivedLevelRules describes the success levels and the fumble.
func (b *Book) derivedLevelRules() []RuleEntry {
	if len(b.levels) == 0 {
		return nil
	}
	var parts []string
	for _, lv := range b.levels {
		when := lv.whenSrc
		if when == "" {
			when = "兜底（其余情况）"
		}
		parts = append(parts, fmt.Sprintf("%s（%s）：%s", lv.name, lv.id, when))
	}
	out := []RuleEntry{{
		ID:       "success_levels",
		Title:    "成功等级",
		Keywords: []string{"成功等级", "success", "判定结果", "等级"},
		Text:     strings.Join(parts, "；"),
	}}
	if fumble, ok := b.fumbleLevel(); ok {
		out = append(out, RuleEntry{
			ID:       "fumble",
			Title:    "大失败",
			Keywords: []string{"大失败", "fumble", "失败判定"},
			Text:     fmt.Sprintf("%s（%s）：%s", fumble.name, fumble.id, fumble.whenSrc),
		})
	}
	return out
}

// derivedDifficultyRules describes the explicit difficulty modifiers.
func (b *Book) derivedDifficultyRules() []RuleEntry {
	if len(b.difficulties) == 0 {
		return nil
	}
	parts := make([]string, 0, len(b.difficulties))
	for _, d := range b.difficulties {
		if d.auto {
			parts = append(parts, fmt.Sprintf("%s（%s）：非大失败即算成功，映射到 %s 级",
				d.name, d.id, d.successLevel))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s（%s）：目标改写为 %s", d.name, d.id, d.exprSrc))
	}
	return []RuleEntry{{
		ID:       "difficulty",
		Title:    "难度",
		Keywords: []string{"难度", "difficulty", "困难", "极难", "自动成功"},
		Text:     strings.Join(parts, "；"),
	}}
}

// derivedDamageRules describes the damage-bonus table.
func (b *Book) derivedDamageRules() []RuleEntry {
	if b.dbTable == nil {
		return nil
	}
	parts := make([]string, 0, len(b.dbTable.rows))
	for _, row := range b.dbTable.rows {
		parts = append(parts, fmt.Sprintf("键 %d–%d：体格 %d，加值 %s",
			row.min, row.max, row.build, row.bonus))
	}
	return []RuleEntry{{
		ID:       "damage_bonus",
		Title:    "伤害加值",
		Keywords: []string{"伤害加值", "db", "体格", "build"},
		Text:     "键 = " + b.dbTable.keySrc + "；" + strings.Join(parts, "；"),
	}}
}

// derivedPointBuyRules describes the point-buy method.
func (b *Book) derivedPointBuyRules() []RuleEntry {
	if b.pointBuy == nil {
		return nil
	}
	p := b.pointBuy
	parts := make([]string, 0, len(p.costs))
	for _, band := range p.costs {
		parts = append(parts, fmt.Sprintf("%d–%d：底价 %d，每 %d 点 %d 分",
			band.min, band.max, band.base, band.per, band.points))
	}
	prio := make([]string, 0, len(p.priorities))
	for _, pr := range p.priorities {
		prio = append(prio, fmt.Sprintf("%s→%d", pr.Attr, pr.Value))
	}
	text := fmt.Sprintf("可购买属性：%s；最低值 %d；点数池：%s；费率：%s",
		strings.Join(p.attributes, "、"), p.minValue, p.budgetSrc, strings.Join(parts, "；"))
	if len(prio) > 0 {
		text += "；非交互自动分配顺序：" + strings.Join(prio, "，")
	}
	return []RuleEntry{{
		ID:       "point_buy",
		Title:    "购点法",
		Keywords: []string{"购点法", "point buy", "点数", "作成"},
		Text:     text,
	}}
}

// derivedCharacteristicRules describes the characteristics and their range.
func (b *Book) derivedCharacteristicRules() []RuleEntry {
	if len(b.attrs) == 0 {
		return nil
	}
	parts := make([]string, 0, len(b.attrs))
	for _, a := range b.attrs {
		part := a.ID + "（" + a.Name + "）"
		if len(a.Aliases) > 0 {
			part += " 又称 " + strings.Join(a.Aliases, "、")
		}
		parts = append(parts, part)
	}
	return []RuleEntry{{
		ID:       "characteristics",
		Title:    "属性",
		Keywords: []string{"属性", "特性值", "characteristic"},
		Text: fmt.Sprintf("共 %d 项，范围 %d–%d：%s",
			len(b.attrs), b.limits.characteristic.Min, b.limits.characteristic.Max,
			strings.Join(parts, "；")),
	}}
}

// fumbleLevel returns the compiled fumble level (if declared).
func (b *Book) fumbleLevel() (compiledLevel, bool) {
	rank, ok := b.levelIndex[b.opposed.fumbleLevel]
	if !ok || rank < 0 || rank >= len(b.levels) {
		return compiledLevel{}, false
	}
	return b.levels[rank], true
}
