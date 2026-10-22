package engine

import (
	"fmt"

	"dicegirl/internal/apperr"
)

// OpposedResult is the outcome of an opposed check.
type OpposedResult struct {
	Kind     string       `json:"kind"`
	SystemID string       `json:"system_id"`
	A        *CheckResult `json:"a"`
	B        *CheckResult `json:"b"`
	Winner   int          `json:"winner"` // 0 = tie, 1 = A, 2 = B
	Reason   string       `json:"reason"`
}

// Opposed resolves two already-rolled checks per the rulebook's opposed
// policy: the higher success rank wins; the declared fumble level always
// loses (two fumbles tie); at equal ranks the higher target wins when
// tie_break is higher_target; otherwise it is a tie.
func (b *Book) Opposed(a, c2 *CheckResult) (*OpposedResult, error) {
	const op = "engine.Opposed"
	if a == nil || c2 == nil {
		return nil, apperr.New(op, apperr.InvalidArgument, "both checks are required")
	}
	fumbleRank := b.levelIndex[b.opposed.fumbleLevel]
	res := &OpposedResult{Kind: "opposed", SystemID: b.manifest.ID, A: a, B: c2}

	switch {
	case a.Level == fumbleRank && c2.Level == fumbleRank:
		res.Winner = 0
		res.Reason = "双方大失败，平局"
	case a.Level == fumbleRank:
		res.Winner = 2
		res.Reason = "A 方大失败，B 方获胜"
	case c2.Level == fumbleRank:
		res.Winner = 1
		res.Reason = "B 方大失败，A 方获胜"
	case a.Level != c2.Level:
		if b.betterLevel(a.Level, c2.Level) {
			res.Winner = 1
		} else {
			res.Winner = 2
		}
		res.Reason = fmt.Sprintf("成功等级 %s 对 %s", a.Label, c2.Label)
	case b.opposed.tieBreak == "higher_target" && a.Target != c2.Target:
		if a.Target > c2.Target {
			res.Winner = 1
		} else {
			res.Winner = 2
		}
		res.Reason = fmt.Sprintf("同等级（%s），技能值 %d 对 %d", a.Label, a.Target, c2.Target)
	default:
		res.Winner = 0
		res.Reason = fmt.Sprintf("同等级同技能值（%s，%d），平局", a.Label, a.Target)
	}
	return res, nil
}
