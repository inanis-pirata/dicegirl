package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/engine/formula"
)

// TargetAdjust adjusts a resolved target value before grading. It carries
// the Dice! `.rc 体质*5` convention (an arithmetic on a sheet value) and
// explicit `--mod` corrections; the published `+n`/`-n` check suffixes stay
// bonus/penalty dice and never reach this type.
//
// A scale (multiply or divide) is applied first, then the signed offset.
// Division is integer division toward zero, like the dice expression layer.
type TargetAdjust struct {
	// Multiply scales the target by a positive factor (0 = no scaling).
	Multiply int
	// Divide divides the target by a positive divisor (0 = no scaling).
	Divide int
	// Add is a signed offset added after the scaling.
	Add int
}

// IsZero reports whether no adjustment is requested.
func (a TargetAdjust) IsZero() bool {
	return a.Multiply == 0 && a.Divide == 0 && a.Add == 0
}

// String renders the adjustment for output ("*5", "/2", "-10").
func (a TargetAdjust) String() string {
	var sb strings.Builder
	switch {
	case a.Multiply > 0:
		sb.WriteString("*" + strconv.Itoa(a.Multiply))
	case a.Divide > 0:
		sb.WriteString("/" + strconv.Itoa(a.Divide))
	}
	if a.Add != 0 {
		if a.Add > 0 {
			sb.WriteString("+" + strconv.Itoa(a.Add))
		} else {
			sb.WriteString(strconv.Itoa(a.Add))
		}
	}
	return sb.String()
}

// applyTargetAdjust computes the adjusted target value.
func applyTargetAdjust(base int, adj TargetAdjust) (int, error) {
	const op = "engine.applyTargetAdjust"
	if adj.Multiply < 0 || adj.Divide < 0 {
		return 0, apperr.New(op, apperr.InvalidArgument,
			"target adjustment factors must be positive")
	}
	if adj.Multiply > 0 && adj.Divide > 0 {
		return 0, apperr.New(op, apperr.InvalidArgument,
			"target adjustment cannot multiply and divide at the same time")
	}
	v := base
	if adj.Multiply > 0 {
		v *= adj.Multiply
	}
	if adj.Divide > 0 {
		v /= adj.Divide
	}
	return v + adj.Add, nil
}

// CheckInput is one check: target value plus bonus/penalty dice counts
// (they cancel pairwise).
type CheckInput struct {
	Target    int
	Bonus     int
	Penalty   int
	SkillName string
	AttrName  string
	// SkillID and AttrID are the canonical ids of the checked skill or
	// characteristic (empty for a target-only check). They are the stable
	// key of the check (used by the roll statistics).
	SkillID string
	AttrID  string
	// Difficulty is an optional declared checks.difficulties id: the target
	// is rewritten through the difficulty formula before normal grading, or
	// the check becomes "non-fumble = success" in auto mode.
	Difficulty string
	// Adjust optionally rewrites the resolved target value (e.g. the
	// Dice! `体质*5` convention). It is applied before any difficulty.
	Adjust TargetAdjust
}

// CheckResult is the structured outcome of a check.
type CheckResult struct {
	Kind      string `json:"kind"`
	SystemID  string `json:"system_id"`
	Target    int    `json:"target"`
	Level     int    `json:"level"`
	LevelCode string `json:"level_code"`
	Label     string `json:"label"`
	RawRoll   int    `json:"raw_roll"`
	Bonus     int    `json:"bonus"`
	Penalty   int    `json:"penalty"`
	SkillName string `json:"skill_name,omitempty"`
	AttrName  string `json:"attr_name,omitempty"`
	// SkillID and AttrID are the canonical ids of the checked skill or
	// characteristic (the stable key of the check).
	SkillID string `json:"skill_id,omitempty"`
	AttrID  string `json:"attr_id,omitempty"`
	// RollDetail is the mechanic-independent detail of the roll: the dice
	// that were rolled and which one counted.
	RollDetail *RollDetail `json:"roll_detail,omitempty"`
	// Difficulty fields are present only for explicit difficulty modifiers.
	DifficultyID   string `json:"difficulty_id,omitempty"`
	DifficultyName string `json:"difficulty_name,omitempty"`
	BaseTarget     int    `json:"base_target,omitempty"`
	// TargetOriginal is the value before a target adjustment (e.g. the
	// sheet value 60 before `体质*5`); TargetAdjust describes the applied
	// arithmetic. Both are present only when an adjustment was requested.
	TargetOriginal int    `json:"target_original,omitempty"`
	TargetAdjust   string `json:"target_adjust,omitempty"`
	Explanation    string `json:"explanation"`
}

// MaxCheckRepeat bounds the number of trials in one batch check
// (anti-abuse limit aligned with the chat-bot convention).
const MaxCheckRepeat = 9

// CheckBatchInput is a repeated check: the same CheckInput rolled
// independently Repeat times.
type CheckBatchInput struct {
	CheckInput
	Repeat int
}

// CheckBatchResult is one check per repeat trial; trials share the resolved
// target but roll their own dice independently.
type CheckBatchResult struct {
	Kind     string         `json:"kind"`
	SystemID string         `json:"system_id"`
	Repeat   int            `json:"repeat"`
	Checks   []*CheckResult `json:"checks"`
}

// CheckMany runs Repeat independent checks through the injected
// generic dice roller, reusing Check (and therefore all grading semantics)
// for every trial.
func (b *Book) CheckMany(ctx context.Context, in CheckBatchInput) (*CheckBatchResult, error) {
	const op = "engine.CheckMany"
	if in.Repeat < 1 || in.Repeat > MaxCheckRepeat {
		return nil, apperr.New(op, apperr.InvalidArgument,
			fmt.Sprintf("repeat count must be in [1,%d]", MaxCheckRepeat))
	}
	checks := make([]*CheckResult, 0, in.Repeat)
	for i := 0; i < in.Repeat; i++ {
		res, err := b.Check(ctx, in.CheckInput)
		if err != nil {
			return nil, err
		}
		checks = append(checks, res)
	}
	return &CheckBatchResult{
		Kind:     "check_batch",
		SystemID: b.manifest.ID,
		Repeat:   in.Repeat,
		Checks:   checks,
	}, nil
}

// LevelRank returns the declared rank of a level id (0 = worst declared).
func (b *Book) LevelRank(id string) (int, bool) {
	rank, ok := b.levelIndex[id]
	return rank, ok
}

// Difficulties returns the declared explicit-difficulty modifiers in
// declaration order. Books without a difficulties section return nil.
func (b *Book) Difficulties() []DifficultyInfo {
	if len(b.difficulties) == 0 {
		return nil
	}
	out := make([]DifficultyInfo, len(b.difficulties))
	for i, d := range b.difficulties {
		out[i] = DifficultyInfo{ID: d.id, Name: d.name, Auto: d.auto}
	}
	return out
}

// FindDifficulty resolves a difficulty id (case-insensitive) or localized
// name.
func (b *Book) FindDifficulty(name string) (DifficultyInfo, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	if i, ok := b.diffIndex[key]; ok {
		d := b.difficulties[i]
		return DifficultyInfo{ID: d.id, Name: d.name, Auto: d.auto}, true
	}
	for _, d := range b.difficulties {
		if d.name == name {
			return DifficultyInfo{ID: d.id, Name: d.name, Auto: d.auto}, true
		}
	}
	return DifficultyInfo{}, false
}

// Check runs one check through the injected generic dice roller.
//
// The dice expression, success direction and bonus/penalty keep direction
// all come from the rulebook's mechanic declaration.
func (b *Book) Check(ctx context.Context, in CheckInput) (*CheckResult, error) {
	const op = "engine.Check"
	if !b.limits.target.Contains(in.Target) {
		return nil, apperr.New(op, apperr.InvalidArgument,
			fmt.Sprintf("target value must be in [%d,%d]", b.limits.target.Min, b.limits.target.Max))
	}

	// Resolve an optional target adjustment (`体质*5`, `--mod -10`): the
	// sheet value is rewritten first, then the optional difficulty
	// modifier works on the adjusted value.
	adjustedTarget := in.Target
	if !in.Adjust.IsZero() {
		v, err := applyTargetAdjust(in.Target, in.Adjust)
		if err != nil {
			return nil, err
		}
		if !b.limits.computedTarget.Contains(v) {
			return nil, apperr.New(op, apperr.InvalidArgument,
				fmt.Sprintf("target after adjustment is %d, must be in [%d,%d]",
					v, b.limits.computedTarget.Min, b.limits.computedTarget.Max))
		}
		adjustedTarget = v
	}

	// Resolve an optional explicit difficulty modifier. Formula difficulties
	// rewrite the target; auto difficulties keep the base target for the
	// fumble test and map every non-fumble to a declared success level.
	var diff *compiledDifficulty
	baseTarget := adjustedTarget
	effectiveTarget := adjustedTarget
	if in.Difficulty != "" {
		idx, ok := b.diffIndex[strings.ToLower(strings.TrimSpace(in.Difficulty))]
		if !ok {
			return nil, apperr.New(op, apperr.InvalidArgument,
				"unknown difficulty: "+in.Difficulty)
		}
		diff = &b.difficulties[idx]
		if !diff.auto {
			v, err := diff.target.EvalInt(formula.Env{"target": adjustedTarget})
			if err != nil {
				return nil, apperr.Wrap(op, apperr.Internal,
					"difficulty "+diff.id+" target formula failed", err)
			}
			effectiveTarget = v
			// The rewritten target is bounded like any computed target
			// (limits.computed_target, which defaults to limits.target).
			if !b.limits.computedTarget.Contains(effectiveTarget) {
				return nil, apperr.New(op, apperr.InvalidArgument,
					fmt.Sprintf("target after difficulty %s is %d, must be in [%d,%d]",
						diff.id, effectiveTarget,
						b.limits.computedTarget.Min, b.limits.computedTarget.Max))
			}
		}
	}
	max := b.mechanic.maxBonusPenalty
	if in.Bonus < 0 || in.Penalty < 0 || in.Bonus > max || in.Penalty > max {
		return nil, apperr.New(op, apperr.InvalidArgument,
			fmt.Sprintf("bonus/penalty dice must be between 0 and %d", max))
	}

	// The mechanic decides which dice are rolled and how bonus/penalty
	// dice apply; nothing below is mechanic-specific.
	mech, err := b.mechanic.kind.Roll(ctx, b.roller, in.Bonus, in.Penalty)
	if err != nil {
		return nil, err
	}
	raw := mech.Value
	var rank int
	failRank := len(b.levels) - 1
	switch {
	case diff != nil && diff.auto:
		// Auto success: the fumble condition is evaluated against the base
		// skill target; every non-fumble maps to the declared success level.
		probe, perr := b.classify(baseTarget, raw)
		if perr != nil {
			return nil, perr
		}
		if probe == b.fumbleRank() {
			rank = b.fumbleRank()
		} else {
			rank = b.levelIndex[diff.successLevel]
		}
	case diff != nil:
		// Formula difficulty: the fumble band follows the adjusted target
		// (BCDice convention). The roll must meet the rewritten target in
		// this mechanic's terms; the label comes from the base ladder.
		probe, perr := b.classify(effectiveTarget, raw)
		if perr != nil {
			return nil, perr
		}
		switch {
		case probe == b.fumbleRank():
			rank = b.fumbleRank()
		default:
			rank, err = b.classify(baseTarget, raw)
			if err != nil {
				return nil, err
			}
			if rank == b.fumbleRank() {
				// A fumble against the base target that is not one
				// against the rewritten target is a plain failure.
				rank = failRank
			}
			if !b.mechanic.kind.Meets(raw, effectiveTarget) {
				rank = failRank
			}
		}
	default:
		rank, err = b.classify(effectiveTarget, raw)
		if err != nil {
			return nil, err
		}
	}

	res := &CheckResult{
		Kind:      "check",
		SystemID:  b.manifest.ID,
		Target:    effectiveTarget,
		Level:     rank,
		LevelCode: b.levels[rank].id,
		Label:     b.levels[rank].name,
		RawRoll:   raw,
		Bonus:     in.Bonus,
		Penalty:   in.Penalty,
		SkillName: in.SkillName,
		AttrName:  in.AttrName,
		SkillID:   in.SkillID,
		AttrID:    in.AttrID,
		RollDetail: &RollDetail{
			Mechanic:  b.mechanic.kind.ID(),
			Value:     mech.Value,
			Dice:      mech.Dice,
			KeptIndex: mech.KeptIndex,
			Effect:    mech.Effect,
		},
	}
	detailText := b.mechanic.kind.Apply(res, mech)
	if diff != nil {
		res.BaseTarget = baseTarget
		res.DifficultyID = diff.id
		res.DifficultyName = diff.name
	}
	if !in.Adjust.IsZero() {
		res.TargetOriginal = in.Target
		res.TargetAdjust = in.Adjust.String()
	}
	res.Explanation = b.explainCheck(in, res, detailText)
	return res, nil
}

// classify evaluates declared success-level conditions in order; the final
// level is the catch-all.
// LevelFor classifies a raw roll against a target without rolling dice,
// returning the level rank together with its declared code and label.
func (b *Book) LevelFor(target, raw int) (int, string, string, error) {
	rank, err := b.classify(target, raw)
	if err != nil {
		return 0, "", "", err
	}
	return rank, b.levels[rank].id, b.levels[rank].name, nil
}

func (b *Book) classify(target, raw int) (int, error) {
	env := formula.Env{"roll": raw, "target": target}
	for i, lv := range b.levels {
		if lv.when == nil {
			return i, nil
		}
		ok, err := lv.when.EvalBool(env)
		if err != nil {
			return 0, apperr.Wrap("engine.classify", apperr.Internal,
				"level "+lv.id+" condition failed", err)
		}
		if ok {
			return i, nil
		}
	}
	return len(b.levels) - 1, nil
}

// fumbleRank returns the declared rank of the fumble level.
func (b *Book) fumbleRank() int { return b.levelIndex[b.opposed.fumbleLevel] }

// difficultyByID returns the compiled difficulty by canonical id.
func (b *Book) difficultyByID(id string) *compiledDifficulty {
	if i, ok := b.diffIndex[strings.ToLower(id)]; ok {
		return &b.difficulties[i]
	}
	return nil
}

// betterLevel reports whether matched level x is a strictly better outcome
// than y. Declaration order is evaluation order: the fumble level is the
// exceptional worst result (it must be tested before the regular ladder),
// and the remaining levels run best-to-worst, so the earlier non-fumble
// index wins.
func (b *Book) betterLevel(x, y int) bool {
	fumble := b.fumbleRank()
	if x == fumble {
		return false
	}
	if y == fumble {
		return true
	}
	return x < y
}

// levelSucceeds reports whether a matched level reaches the given threshold
// level. The fumble level never succeeds; every other level declared at or
// before the threshold (i.e. the threshold itself or a better one) does.
func (b *Book) levelSucceeds(rank, thresholdRank int) bool {
	if rank == b.fumbleRank() {
		return false
	}
	return rank <= thresholdRank
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (b *Book) explainCheck(in CheckInput, r *CheckResult, detailText string) string {
	var sb strings.Builder
	label := firstNonEmpty(in.SkillName, in.AttrName, "检定")
	if r.DifficultyName != "" {
		label += "（" + r.DifficultyName + "）"
	}
	fmt.Fprintf(&sb, "[%s] 目标 %d", label, r.Target)
	if r.TargetAdjust != "" {
		fmt.Fprintf(&sb, "（原始 %d，%s）", r.TargetOriginal, r.TargetAdjust)
	}
	if r.DifficultyID != "" {
		if d := b.difficultyByID(r.DifficultyID); d != nil && d.auto {
			fmt.Fprintf(&sb, "（基础 %d，非大失败即成功）", r.BaseTarget)
		} else {
			fmt.Fprintf(&sb, "（基础 %d）", r.BaseTarget)
		}
	}
	if detailText != "" {
		sb.WriteString(detailText)
	}
	fmt.Fprintf(&sb, "：骰值 %d → %s", r.RawRoll, r.Label)
	return sb.String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
