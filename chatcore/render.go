// Package chatcore parses chat-style short commands (Dice!/BCDice conventions)
// results as human-readable text. It contains no rule logic: every operation
// is a thin call into internal/app, and every result type comes from the
// generic engine interpreting a rulebook.
package chatcore

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"dicegirl/internal/app"
	"dicegirl/internal/character"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine"
	"dicegirl/internal/rules"
	"dicegirl/internal/storage"
)

// Renderer writes command output. JSON mode emits the structured payloads
// directly so future chat-platform adapters can reuse the same contract.
type Renderer struct {
	Out  io.Writer
	JSON bool
}

// render prints value as JSON when enabled, otherwise text produced by asText.
func (r *Renderer) render(value any, asText func() string) error {
	if r.JSON {
		enc := json.NewEncoder(r.Out)
		enc.SetEscapeHTML(false)
		return enc.Encode(value)
	}
	fmt.Fprintln(r.Out, asText())
	return nil
}

// Roll renders a single generic dice result (used by callers that parse a
// pure expression without the statement layer).
func (r *Renderer) Roll(res *dice.RollResult) error {
	return r.render(res, func() string {
		line := fmt.Sprintf("🎲 %s = %d  %s", res.Expression, res.Total, explainDice(res))
		return strings.TrimRight(line, " ")
	})
}

// rollJSON keeps the single-roll JSON contract flat (same fields as
// dice.RollResult) and only adds the UI-layer reason string.
type rollJSON struct {
	*dice.RollResult
	Reason string `json:"reason,omitempty"`
}

// batchJSON is the repeat container emitted for n# statements with n > 1.
type batchJSON struct {
	Expression string             `json:"expression"`
	Repeat     int                `json:"repeat"`
	Reason     string             `json:"reason,omitempty"`
	Rolls      []*dice.RollResult `json:"rolls"`
}

// RollBatch renders a roll statement: one line per repeat trial, with the
// comparison verdict and UI-layer reason text (if any). Single-trial
// statements keep the historical flat output.
func (r *Renderer) RollBatch(batch *dice.BatchResult, reason string) error {
	if r.JSON {
		if len(batch.Rolls) == 1 {
			return r.render(rollJSON{RollResult: batch.Rolls[0], Reason: reason}, nil)
		}
		return r.render(batchJSON{
			Expression: batch.Expression,
			Repeat:     batch.Repeat,
			Reason:     reason,
			Rolls:      batch.Rolls,
		}, nil)
	}

	header := "🎲 " + batch.Expression
	if reason != "" {
		header = "🎲 " + reason + "：" + batch.Expression
	}
	if len(batch.Rolls) == 1 {
		rr := batch.Rolls[0]
		return r.render(nil, func() string {
			// Single trials reproduce the old line: 🎲 <expr> = <total>  <details>
			line := fmt.Sprintf("🎲 %s = %d  %s", rr.Expression, rr.Total, explainDice(rr))
			line = strings.TrimRight(line, " ")
			if v := comparisonText(rr.Compare); v != "" {
				line += " " + v
			}
			if reason != "" {
				line += "｜" + reason
			}
			return line
		})
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteByte('\n')
	for i, rr := range batch.Rolls {
		fmt.Fprintf(&b, "%s %d  %s", circled(i+1), rr.Total, explainDice(rr))
		if v := comparisonText(rr.Compare); v != "" {
			b.WriteByte(' ')
			b.WriteString(v)
		}
		if i < len(batch.Rolls)-1 {
			b.WriteByte('\n')
		}
	}
	fmt.Fprintln(r.Out, b.String())
	return nil
}

// compactRollJSON is the compact-roll JSON payload: the same fields as the
// ordinary roll result plus the "detailed" marker.
type compactRollJSON struct {
	*dice.RollResult
	Reason   string `json:"reason,omitempty"`
	Detailed bool   `json:"detailed"`
}

// compactBatchJSON is the compact-roll batch container.
type compactBatchJSON struct {
	Expression string             `json:"expression"`
	Repeat     int                `json:"repeat"`
	Reason     string             `json:"reason,omitempty"`
	Detailed   bool               `json:"detailed"`
	Rolls      []*dice.RollResult `json:"rolls"`
}

// RollBatchCompact renders a roll statement without the per-die detail: only
// the totals are shown. The JSON contract is unchanged apart from the
// "detailed": false marker.
func (r *Renderer) RollBatchCompact(batch *dice.BatchResult, reason string) error {
	if r.JSON {
		if len(batch.Rolls) == 1 {
			return r.render(compactRollJSON{RollResult: batch.Rolls[0], Reason: reason, Detailed: false}, nil)
		}
		return r.render(compactBatchJSON{
			Expression: batch.Expression,
			Repeat:     batch.Repeat,
			Reason:     reason,
			Detailed:   false,
			Rolls:      batch.Rolls,
		}, nil)
	}

	if len(batch.Rolls) == 1 {
		rr := batch.Rolls[0]
		line := fmt.Sprintf("🎲 %s = %d", rr.Expression, rr.Total)
		if v := comparisonText(rr.Compare); v != "" {
			line += " " + v
		}
		if reason != "" {
			line += "｜" + reason
		}
		return r.render(nil, func() string { return line })
	}

	var b strings.Builder
	fmt.Fprintf(&b, "🎲 %s ×%d", batch.Expression, batch.Repeat)
	if reason != "" {
		b.WriteString("｜")
		b.WriteString(reason)
	}
	for i, rr := range batch.Rolls {
		fmt.Fprintf(&b, "\n%s %d", circled(i+1), rr.Total)
		if v := comparisonText(rr.Compare); v != "" {
			b.WriteByte(' ')
			b.WriteString(v)
		}
	}
	fmt.Fprintln(r.Out, b.String())
	return nil
}

// Generation renders one or more generated characters.
func (r *Renderer) Generation(res *app.GenerateResult) error {
	return r.render(res, func() string {
		lines := make([]string, 0, len(res.Characters))
		for _, c := range res.Characters {
			lines = append(lines, generationLine(c))
		}
		return strings.Join(lines, "\n")
	})
}

// GeneratedSheet renders a regenerated character together with its card.
func (r *Renderer) GeneratedSheet(sheet *app.GeneratedSheet, c *character.Character) error {
	return r.render(sheet, func() string {
		line := generationLine(sheet)
		if c != nil {
			line += fmt.Sprintf("（角色 %s）", c.ID)
		}
		return line
	})
}

// generationLine renders one generated character.
func generationLine(c *app.GeneratedSheet) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "🎲 [%s] %s", c.Name, formatAttributeValues(c.Attributes))
	if len(c.Vitals) > 0 {
		tracks := make([]string, 0, len(c.Vitals))
		for _, v := range c.Vitals {
			tracks = append(tracks, fmt.Sprintf("%s %d/%d", strings.ToUpper(v.Resource), v.Current, v.Max))
		}
		fmt.Fprintf(&sb, "；%s", strings.Join(tracks, "，"))
	}
	if c.PointBuy != nil {
		fmt.Fprintf(&sb, "；点数 %d/%d（剩 %d", c.PointBuy.Spent, c.PointBuy.Budget, c.PointBuy.Left)
		if c.PointBuy.OccupationCap > 0 {
			fmt.Fprintf(&sb, "，职业上限 %d", c.PointBuy.OccupationCap)
		}
		if c.PointBuy.InterestCap > 0 {
			fmt.Fprintf(&sb, "，兴趣上限 %d", c.PointBuy.InterestCap)
		}
		sb.WriteString("）")
	}
	return sb.String()
}

// formatResources renders resource tracks as a sorted "hp 13/13，san 99/99".
func formatResources(tracks map[string]character.Resource) string {
	keys := make([]string, 0, len(tracks))
	for k := range tracks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := tracks[k]
		parts = append(parts, fmt.Sprintf("%s %d/%d", k, v.Current, v.Max))
	}
	return strings.Join(parts, "，")
}

// formatAttributeValues renders an attribute map as "STR 50，CON 60".
func formatAttributeValues(values map[string]int) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, values[k]))
	}
	return strings.Join(parts, "，")
}

// Initiative renders the initiative list (highest value first).
func (r *Renderer) Initiative(list *app.InitiativeList) error {
	return r.render(list, func() string {
		if len(list.Entries) == 0 {
			return "（先攻列表为空）"
		}
		lines := make([]string, 0, len(list.Entries))
		for i, e := range list.Entries {
			name := e.Name
			if name == "" {
				name = "（无名）"
			}
			lines = append(lines, fmt.Sprintf("%d. %s %d", i+1, name, e.Value))
		}
		return strings.Join(lines, "\n")
	})
}

// Deck renders one deck with its draw pile and discard pile.
func (r *Renderer) Deck(deck *app.Deck) error {
	return r.render(deck, func() string {
		remaining := "（空）"
		if len(deck.Remaining) > 0 {
			remaining = strings.Join(deck.Remaining, "、")
		}
		line := fmt.Sprintf("牌堆「%s」：剩 %d/%d 张：%s",
			deck.Name, len(deck.Remaining), len(deck.Cards), remaining)
		if len(deck.Discard) > 0 {
			line += fmt.Sprintf("；弃牌堆 %d 张：%s", len(deck.Discard), strings.Join(deck.Discard, "、"))
		}
		return line
	})
}

// DeckList renders every deck.
func (r *Renderer) DeckList(list *app.DeckList) error {
	return r.render(list, func() string {
		if len(list.Decks) == 0 {
			return "（没有牌堆）"
		}
		lines := make([]string, 0, len(list.Decks))
		for _, d := range list.Decks {
			line := fmt.Sprintf("%s\t剩 %d/%d 张", d.Name, len(d.Remaining), len(d.Cards))
			if len(d.Discard) > 0 {
				line += fmt.Sprintf("，弃 %d", len(d.Discard))
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	})
}

// Draw renders the drawn cards.
func (r *Renderer) Draw(res *app.DrawResult) error {
	return r.render(res, func() string {
		line := fmt.Sprintf("抽牌（%s）：%s（牌堆剩 %d 张）",
			res.Deck, strings.Join(res.Cards, "、"), res.Left)
		switch {
		case res.Replace:
			line += "（放回）"
		case res.Discard > 0:
			line += fmt.Sprintf("（弃牌堆 %d 张）", res.Discard)
		}
		return line
	})
}

// Session renders one game table and the per-table state it owns. (The
// random-table renderer keeps the Table name.)
func (r *Renderer) Session(view *app.TableView) error {
	return r.render(view, func() string {
		marker := ""
		if view.Active {
			marker = "（当前）"
		}
		line := fmt.Sprintf("桌「%s」%s %s", view.Name, marker, view.ID)
		if view.SystemID != "" {
			line += "，规则书 " + view.SystemID
		}
		line += fmt.Sprintf("；先攻 %d 个条目", view.Initiative)
		if len(view.Decks) > 0 {
			line += "；牌堆：" + strings.Join(view.Decks, "、")
		}
		return line
	})
}

// SessionList renders every game table, marking the active one.
func (r *Renderer) SessionList(list *app.TableList) error {
	return r.render(list, func() string {
		if len(list.Tables) == 0 {
			return "（还没有桌）"
		}
		lines := make([]string, 0, len(list.Tables))
		for _, t := range list.Tables {
			mark := "  "
			if t.Active {
				mark = "* "
			}
			line := fmt.Sprintf("%s%s\t%s", mark, t.Name, t.ID)
			if t.SystemID != "" {
				line += "，规则书 " + t.SystemID
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	})
}

// CardStats renders the roll statistics of a card.
func (r *Renderer) CardStats(stats *storage.CardStats) error {
	return r.render(stats, func() string {
		if stats == nil || stats.Checks == 0 {
			return "（该卡还没有掷骰记录）"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "掷骰统计（%s）：检定 %d 次，成功 %d，失败 %d，大失败 %d",
			stats.CardID, stats.Checks, stats.Successes, stats.Failures, stats.Fumbles)
		if stats.Checks > 0 {
			fmt.Fprintf(&b, "，平均骰值 %.1f，最好 %d，最差 %d",
				float64(stats.RollSum)/float64(stats.Checks), stats.Best, stats.Worst)
		}
		b.WriteString("\n")
		if len(stats.Levels) > 0 {
			codes := make([]string, 0, len(stats.Levels))
			for code := range stats.Levels {
				codes = append(codes, code)
			}
			sort.Strings(codes)
			parts := make([]string, 0, len(codes))
			for _, code := range codes {
				parts = append(parts, fmt.Sprintf("%s %d", code, stats.Levels[code]))
			}
			fmt.Fprintf(&b, "等级：%s\n", strings.Join(parts, "，"))
		}
		if len(stats.Keys) > 0 {
			b.WriteString("按项目：\n")
			for _, k := range stats.Keys {
				avg := 0.0
				if k.Checks > 0 {
					avg = float64(k.RollSum) / float64(k.Checks)
				}
				fmt.Fprintf(&b, "  %s\t%d 次  成功 %d  失败 %d  平均 %.1f\n",
					k.Name, k.Checks, k.Successes, k.Failures, avg)
			}
		}
		if len(stats.History) > 0 {
			b.WriteString("最近：\n")
			for i, h := range stats.History {
				if i >= 10 {
					break
				}
				fmt.Fprintf(&b, "  %s  %s  目标 %d  掷 %d  %s\n",
					h.At.Local().Format("01-02 15:04"), h.Key, h.Target, h.Roll, h.LevelCode)
			}
		}
		return strings.TrimRight(b.String(), "\n")
	})
}

// Rules renders the matches of a quick-reference search.
func (r *Renderer) Rules(res *app.RulesResult) error {
	return r.render(res, func() string {
		if len(res.Matches) == 0 {
			return "（没有匹配的速查词条）"
		}
		var b strings.Builder
		for i, m := range res.Matches {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "【%s】%s\n%s", m.SystemName, m.Entry.Title, m.Entry.Text)
			if len(res.Matches) > 1 {
				fmt.Fprintf(&b, "\n（%s/%s）", m.SystemID, m.Entry.ID)
			}
		}
		if len(res.Matches) > 1 {
			b.WriteString("\n（多条命中，可用 rules set <系统> 固定偏好，或直接引用 词条 id）")
		}
		return strings.TrimRight(b.String(), "\n")
	})
}

// RuleEntries renders the full quick-reference list.
func (r *Renderer) RuleEntries(entries []app.RuleMatch) error {
	return r.render(entries, func() string {
		if len(entries) == 0 {
			return "（没有速查词条）"
		}
		lines := make([]string, 0, len(entries))
		for _, e := range entries {
			lines = append(lines, fmt.Sprintf("%s/%s\t%s", e.SystemID, e.Entry.ID, e.Entry.Title))
		}
		return strings.Join(lines, "\n")
	})
}

// Table renders one random-table roll.
func (r *Renderer) Table(res *engine.TableResult) error {
	return r.render(res, func() string {
		line := fmt.Sprintf("🎲 [%s] 掷 %s = %d → %s",
			res.TableName, res.Roll.Expression, res.Value, res.Text)
		for _, sub := range res.SubRolls {
			line += fmt.Sprintf("（子掷 %s = %d）", sub.Expression, sub.Total)
		}
		return line
	})
}

// TableList renders the random tables of a rulebook.
func (r *Renderer) TableList(tables []engine.TableDef) error {
	return r.render(tables, func() string {
		if len(tables) == 0 {
			return "（该规则书没有声明随机表）"
		}
		lines := make([]string, 0, len(tables))
		for _, t := range tables {
			lines = append(lines, fmt.Sprintf("%s\t%s（掷 %s，%d–%d）",
				t.ID, t.Name, t.RollExpr, t.Min, t.Max))
		}
		return strings.Join(lines, "\n")
	})
}

// Growth renders one skill-growth check.
func (r *Renderer) Growth(res *engine.GrowthResult) error {
	return r.render(res, func() string {
		line := res.Explanation
		if res.Reason != "" {
			line += "｜" + res.Reason
		}
		return line
	})
}

// Choice renders a random-choice draw.
func (r *Renderer) Choice(res *app.ChoiceResult) error {
	return r.render(res, func() string {
		return fmt.Sprintf("随机抉择：%s → 抽中「%s」",
			strings.Join(res.Options, "、"), res.Pick)
	})
}

// ChoiceOptions renders the options of the open random-choice round.
func (r *Renderer) ChoiceOptions(options []string) error {
	return r.render(options, func() string {
		if len(options) == 0 {
			return "（当前没有待选选项）"
		}
		return strings.Join(options, "、")
	})
}

// Names renders generated random names.
func (r *Renderer) Names(res *app.NameResult) error {
	return r.render(res, func() string {
		return fmt.Sprintf("随机姓名（%s）：%s", res.SetName, strings.Join(res.Names, "、"))
	})
}

// Adjustments renders the per-key results of st K±表达式 changes.
func (r *Renderer) Adjustments(results []app.AdjustmentResult) error {
	return r.render(results, func() string {
		lines := make([]string, 0, len(results))
		for _, res := range results {
			line := fmt.Sprintf("%s %s %d → %d（%+d）",
				adjustKindText(res.Kind), res.Canonical, res.Before, res.After, res.Delta)
			if strings.ContainsAny(res.Expr, "dD") {
				line += fmt.Sprintf("，掷骰 %s", res.Expr)
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	})
}

func adjustKindText(kind string) string {
	switch kind {
	case "attribute":
		return "属性"
	case "skill":
		return "技能"
	case "resource":
		return "状态"
	default:
		return kind
	}
}

func comparisonText(c *dice.Comparison) string {
	if c == nil {
		return ""
	}
	if c.Success {
		return fmt.Sprintf("成功(%s%d)", c.Op, c.Target)
	}
	return fmt.Sprintf("失败(%s%d)", c.Op, c.Target)
}

// circled renders 1..10 as ①..⑩ for repeat output.
func circled(n int) string {
	if n < 1 || n > 10 {
		return fmt.Sprintf("%d.", n)
	}
	return string(rune('①' - 1 + n))
}

// explainDice renders the per-die detail of a roll. Following the Dice!,
// groups with more than 20 dice are shown sorted so long rolls stay
// readable; the keep/drop markers stay attached to their die.
func explainDice(res *dice.RollResult) string {
	var parts []string
	for _, g := range res.Groups {
		rolls := make([]string, 0, len(g.Rolls))
		for _, dr := range g.Rolls {
			if dr.Kept {
				rolls = append(rolls, fmt.Sprintf("%d", dr.Value))
			} else {
				rolls = append(rolls, fmt.Sprintf("~%d~", dr.Value))
			}
		}
		if len(g.Rolls) > 20 {
			type face struct {
				value int
				kept  bool
			}
			faces := make([]face, 0, len(g.Rolls))
			for _, dr := range g.Rolls {
				faces = append(faces, face{value: dr.Value, kept: dr.Kept})
			}
			sort.SliceStable(faces, func(i, j int) bool { return faces[i].value < faces[j].value })
			rolls = rolls[:0]
			for _, f := range faces {
				if f.kept {
					rolls = append(rolls, fmt.Sprintf("%d", f.value))
				} else {
					rolls = append(rolls, fmt.Sprintf("~%d~", f.value))
				}
			}
		}
		parts = append(parts, fmt.Sprintf("[%dd%d: %s]", g.Count, g.Sides, strings.Join(rolls, ",")))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

// Systems renders the loaded rulebook list.
func (r *Renderer) Systems(manifests []rules.Manifest) error {
	if r.JSON {
		return r.render(manifests, nil)
	}
	if len(manifests) == 0 {
		fmt.Fprintln(r.Out, "（未加载任何规则书）")
		return nil
	}
	for _, m := range manifests {
		marker := ""
		if m.Default {
			marker = "（默认）"
		}
		fmt.Fprintf(r.Out, "%s\t%s%s（规则版本 %s，系统版本 %s）\n",
			m.ID, m.Name, marker, m.RulesVersion, m.SystemVersion)
	}
	return nil
}

// CharacterSummary is one list row.
func (r *Renderer) CharacterSummary(activeID string, chars []*character.Character) error {
	if r.JSON {
		return r.render(chars, nil)
	}
	if len(chars) == 0 {
		fmt.Fprintln(r.Out, "（暂无角色卡）")
		return nil
	}
	for _, c := range chars {
		mark := "  "
		if c.ID == activeID {
			mark = "* "
		}
		fmt.Fprintf(r.Out, "%s%s\t%s\t[%s]\n", mark, c.ID, c.Name, c.SystemID)
	}
	return nil
}

// Character renders one sheet.
func (r *Renderer) Character(c *character.Character) error {
	return r.render(c, func() string {
		var b strings.Builder
		fmt.Fprintf(&b, "角色 %s：%s（%s）\n", c.ID, c.Name, c.SystemID)
		if len(c.Attributes) > 0 {
			b.WriteString("属性：")
			b.WriteString(formatIntMap(c.Attributes))
			b.WriteString("\n")
		}
		if len(c.Skills) > 0 {
			b.WriteString("技能：")
			b.WriteString(formatIntMap(c.Skills))
			b.WriteString("\n")
		}
		if len(c.Resources) > 0 {
			b.WriteString("状态：")
			b.WriteString(formatResources(c.Resources))
			b.WriteString("\n")
		}
		if len(c.Macros) > 0 {
			b.WriteString("宏：")
			names := make([]string, 0, len(c.Macros))
			for name := range c.Macros {
				names = append(names, name)
			}
			sort.Strings(names)
			for i, name := range names {
				if i > 0 {
					b.WriteString("，")
				}
				fmt.Fprintf(&b, "&%s=%s", name, c.Macros[name])
			}
			b.WriteString("\n")
		}
		if len(c.Metadata) > 0 {
			b.WriteString("元数据：")
			keys := make([]string, 0, len(c.Metadata))
			for k := range c.Metadata {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for i, k := range keys {
				if i > 0 {
					b.WriteString("，")
				}
				fmt.Fprintf(&b, "%s=%s", k, c.Metadata[k])
			}
			b.WriteString("\n")
		}
		return strings.TrimRight(b.String(), "\n")
	})
}

// CreatedCharacter renders a create result. notice (empty to omit) is an
// extra text-only line, e.g. the derived tracks recorded on creation or a
// hint that incomplete characteristics left the tracks uninitialized.
func (r *Renderer) CreatedCharacter(c *character.Character, notice string) error {
	return r.render(c, func() string {
		line := fmt.Sprintf("已创建角色 %s：%s（%s）", c.ID, c.Name, c.SystemID)
		if notice != "" {
			line += "\n" + notice
		}
		return line
	})
}

// SimpleMessage renders a plain ack line (JSON uses {"message": ...}).
func (r *Renderer) SimpleMessage(msg string) error {
	if r.JSON {
		return r.render(map[string]string{"message": msg}, nil)
	}
	fmt.Fprintln(r.Out, msg)
	return nil
}

// Check renders a rulebook-driven check.
func (r *Renderer) Check(res *engine.CheckResult) error {
	return r.render(res, func() string { return res.Explanation })
}

// CheckBatch renders repeated checks: JSON emits the structured batch
// container, text output gets one explanation line per trial with a circled
// index under a shared header.
func (r *Renderer) CheckBatch(batch *engine.CheckBatchResult) error {
	return r.render(batch, func() string {
		label := "检定"
		if len(batch.Checks) > 0 {
			if first := batch.Checks[0]; first.SkillName != "" {
				label = first.SkillName
			} else if first.AttrName != "" {
				label = first.AttrName
			}
		}
		var b strings.Builder
		fmt.Fprintf(&b, "🎲 %s ×%d\n", label, batch.Repeat)
		for i, res := range batch.Checks {
			fmt.Fprintf(&b, "%s %s", circled(i+1), res.Explanation)
			if i < len(batch.Checks)-1 {
				b.WriteByte('\n')
			}
		}
		return b.String()
	})
}

// Opposed renders an opposed check.
func (r *Renderer) Opposed(res *engine.OpposedResult) error {
	return r.render(res, func() string {
		return fmt.Sprintf("对抗检定：A %s（骰 %d）｜B %s（骰 %d）→ %s",
			res.A.Label, res.A.RawRoll, res.B.Label, res.B.RawRoll, res.Reason)
	})
}

// Value renders one sheet value addressed by key (`st show <键>`).
func (r *Renderer) Value(v *app.ValueResult) error {
	return r.render(v, func() string {
		line := fmt.Sprintf("%s %s %d", adjustKindText(v.Kind), v.Canonical, v.Value)
		if v.Kind == "resource" && v.Max > 0 {
			line += fmt.Sprintf("/%d", v.Max)
		}
		return line
	})
}

// resourceJSON adds the UI-layer reason text to the structured resource
// check without changing the engine contract.
type resourceJSON struct {
	*engine.ResourceCheckResult
	Reason string `json:"reason,omitempty"`
}

// ResourceCheck renders a resource check (e.g. sanity). applied reports
// whether the proposed change was persisted by the application layer;
// reason is the optional UI-layer reason text.
func (r *Renderer) ResourceCheck(res *engine.ResourceCheckResult, applied bool, reason string) error {
	if r.JSON {
		return r.render(resourceJSON{ResourceCheckResult: res, Reason: reason}, nil)
	}
	return r.render(res, func() string {
		change := "未写回角色卡（需要 --apply）"
		if applied {
			change = fmt.Sprintf("%s %d → %d（%d）已保存",
				res.Change.Resource, res.Change.Before, res.Change.After, res.Change.Delta)
		}
		lossNote := ""
		if res.FumbleLossMax {
			lossNote = "（大失败，取最大损失）"
		}
		line := fmt.Sprintf("%s；损失 %s=%d%s → %s｜%s",
			res.Check.Explanation, res.LossSpec, res.Loss, lossNote, successText(res.Success), change)
		if reason != "" {
			line += "｜" + reason
		}
		return line
	})
}

// Damage renders a damage roll.
func (r *Renderer) Damage(res *engine.DamageResult) error {
	return r.render(res, func() string {
		return fmt.Sprintf("伤害 %s（体格 %d，伤害加值 %s）= %d  %s",
			res.Expr, res.Build, res.DBExpr, res.Total, explainDice(res.Roll))
	})
}

// Derived renders derived statistics.
func (r *Renderer) Derived(d *engine.DerivedResult, initialized bool) error {
	payload := map[string]any{"derived": d, "initialized": initialized}
	return r.render(payload, func() string {
		parts := make([]string, 0, len(d.Values))
		for _, v := range d.Values {
			if v.Expr != "" {
				parts = append(parts, fmt.Sprintf("%s %s", v.Name, v.Expr))
			} else {
				parts = append(parts, fmt.Sprintf("%s %d", v.Name, v.Int))
			}
		}
		suffix := ""
		if initialized {
			suffix = "（已初始化生命/魔法/理智轨道并保存）"
		}
		return strings.Join(parts, "，") + suffix
	})
}

func successText(ok bool) string {
	if ok {
		return "成功"
	}
	return "失败"
}

func formatIntMap(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
	}
	return strings.Join(parts, "，")
}
