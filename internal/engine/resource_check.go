package engine

import (
	"context"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/dice"
	"dicegirl/internal/engine/formula"
)

// LossSpec is a resource loss: fixed amount or dice expression. A negative
// spec restores the track (the Dice! manual allows recovering sanity, though
// it discourages it): the magnitude is the loss, the sign is the direction.
type LossSpec struct {
	Fixed int
	Expr  string
	// Negative marks a recovery spec: the computed loss is negated.
	Negative bool
}

// ParseLossSpec accepts "0", "5" (fixed), "-3" (recovery) or dice
// expressions like "1d4" / "-1d4".
func ParseLossSpec(s string) (LossSpec, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return LossSpec{}, apperr.New("engine.ParseLossSpec", apperr.InvalidArgument, "empty loss specification")
	}
	negative := false
	if strings.HasPrefix(s, "-") {
		negative = true
		s = strings.TrimSpace(s[1:])
		if s == "" {
			return LossSpec{}, apperr.New("engine.ParseLossSpec", apperr.InvalidArgument,
				"loss must be a non-negative integer or dice expression, got -")
		}
	}
	if strings.ContainsAny(s, "dD") {
		if _, _, err := dice.Parse(s, dice.DefaultLimits()); err != nil {
			return LossSpec{}, apperr.Wrap("engine.ParseLossSpec", apperr.InvalidArgument,
				"invalid loss expression "+s, err)
		}
		return LossSpec{Expr: s, Negative: negative}, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return LossSpec{}, apperr.New("engine.ParseLossSpec", apperr.InvalidArgument,
			"loss must be a non-negative integer or dice expression, got "+s)
	}
	return LossSpec{Fixed: n, Negative: negative}, nil
}

// String renders the spec for output.
func (l LossSpec) String() string {
	if l.Expr != "" {
		if l.Negative {
			return "-" + l.Expr
		}
		return l.Expr
	}
	if l.Negative {
		return "-" + strconv.Itoa(l.Fixed)
	}
	return strconv.Itoa(l.Fixed)
}

// ResourceChange is a proposed mutation of one resource track; the
// application layer decides whether to persist.
type ResourceChange struct {
	Resource string `json:"resource"`
	Before   int    `json:"before"`
	After    int    `json:"after"`
	Delta    int    `json:"delta"`
}

// ResourceCheckInput drives one resource check (e.g. sanity).
type ResourceCheckInput struct {
	// Before is the track's current value; the proposed change is
	// computed from it.
	Before int
	// Target optionally overrides the check target (the Dice! `.sc 0/1 70`
	// convention: check against an explicit current value). It never
	// changes the track; the change is still computed from Before.
	Target      *int
	SuccessLoss LossSpec
	FailureLoss LossSpec
	Bonus       int
	Penalty     int
}

// ResourceCheckResult is the structured outcome.
type ResourceCheckResult struct {
	Kind     string       `json:"kind"`
	SystemID string       `json:"system_id"`
	ID       string       `json:"check_id"`
	Label    string       `json:"label"`
	Check    *CheckResult `json:"check"`
	// CheckTarget is the value the check was run against (the track's
	// current value unless an explicit target override was given).
	CheckTarget int              `json:"check_target"`
	LossSpec    string           `json:"loss_spec"`
	Loss        int              `json:"loss"`
	Success     bool             `json:"success"`
	Change      ResourceChange   `json:"change"`
	LossRoll    *dice.RollResult `json:"loss_roll,omitempty"`
	// FumbleLossMax marks a fumble whose loss was replaced by the static
	// maximum of the failure-loss specification (LossRoll is then nil).
	FumbleLossMax bool `json:"fumble_loss_max,omitempty"`
}

type compiledResourceCheck struct {
	id           string
	label        string
	resource     string
	fallback     *formula.Expr
	successLevel string
	successLoss  LossSpec
	failureLoss  LossSpec
	// fumbleLossMax reports whether a fumble takes the maximum possible
	// failure loss (Dice! sanity rule): a fixed loss is itself, a dice loss
	// is the static maximum of its expression (sum of kept faces).
	fumbleLossMax bool
	validRange    Range
	clampMin      *int
	clampMax      *int
}

func compileResourceCheck(op, id string, raw rawResourceCheck, attrVars map[string]bool, levels map[string]int) (*compiledResourceCheck, error) {
	if id == "" || raw.Label == "" || raw.Resource == "" {
		return nil, apperr.New(op, apperr.ConfigInvalid, "resource_check requires id, label and resource")
	}
	if _, ok := levels[raw.SuccessLevel]; !ok {
		return nil, apperr.New(op, apperr.ConfigInvalid,
			"resource_check "+id+" success_level "+raw.SuccessLevel+" is not a declared level")
	}
	fallback, err := compileIntFormulaOrEmpty(raw.Fallback, "resource_check "+id+" fallback", attrVars)
	if err != nil {
		return nil, err
	}
	succ, err := ParseLossSpec(raw.Loss.Success)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.ConfigInvalid, "resource_check "+id+" success loss", err)
	}
	fail, err := ParseLossSpec(raw.Loss.Failure)
	if err != nil {
		return nil, apperr.Wrap(op, apperr.ConfigInvalid, "resource_check "+id+" failure loss", err)
	}
	if raw.Range == nil || !validRange(*raw.Range) {
		return nil, apperr.New(op, apperr.ConfigInvalid, "resource_check "+id+" needs a valid range")
	}
	switch raw.FumbleLoss {
	case "", "normal":
	case "max":
	default:
		return nil, apperr.New(op, apperr.ConfigInvalid,
			"resource_check "+id+" fumble_loss must be \"normal\" or \"max\", got "+raw.FumbleLoss)
	}
	rc := &compiledResourceCheck{
		id:            id,
		label:         raw.Label,
		resource:      raw.Resource,
		fallback:      fallback,
		successLevel:  raw.SuccessLevel,
		successLoss:   succ,
		failureLoss:   fail,
		fumbleLossMax: raw.FumbleLoss == "max",
		validRange:    *raw.Range,
	}
	if raw.Clamp != nil {
		if raw.Clamp.Min != nil && raw.Clamp.Max != nil && *raw.Clamp.Min > *raw.Clamp.Max {
			return nil, apperr.New(op, apperr.ConfigInvalid,
				"resource_check "+id+" clamp min > max")
		}
		rc.clampMin = raw.Clamp.Min
		rc.clampMax = raw.Clamp.Max
	}
	return rc, nil
}

func compileIntFormulaOrEmpty(src, where string, vars map[string]bool) (*formula.Expr, error) {
	if strings.TrimSpace(src) == "" {
		return nil, nil
	}
	return compileIntFormula(src, where, vars)
}

// ResourceCheck returns the declared resource check by id.
func (b *Book) ResourceCheck(id string) (*compiledResourceCheck, bool) {
	rc, ok := b.resChecks[id]
	return rc, ok
}

// FallbackTarget evaluates the check target of a resource check when the
// track does not exist yet (formula over the character's attributes).
func (rc *compiledResourceCheck) FallbackTarget(attrs map[string]int) (int, error) {
	if rc.fallback == nil {
		return 0, apperr.New("engine.FallbackTarget", apperr.InvalidArgument,
			"resource "+rc.resource+" not initialized and no fallback formula is declared")
	}
	v, err := rc.fallback.EvalInt(formula.Env(attrs))
	if err != nil {
		return 0, apperr.Wrap("engine.FallbackTarget", apperr.InvalidArgument,
			"cannot compute target for "+rc.id, err)
	}
	return v, nil
}

// Resource returns the resource track id the check operates on.
func (rc *compiledResourceCheck) Resource() string { return rc.resource }

// Label returns the display name of the resource check.
func (rc *compiledResourceCheck) Label() string { return rc.label }

// DefaultSuccessLoss is the rulebook's loss on a successful check.
func (rc *compiledResourceCheck) DefaultSuccessLoss() LossSpec { return rc.successLoss }

// DefaultFailureLoss is the rulebook's loss on a failed check.
func (rc *compiledResourceCheck) DefaultFailureLoss() LossSpec { return rc.failureLoss }

// Run performs the check against Before and computes the loss and a
// proposed change. It never mutates character state.
func (b *Book) RunResourceCheck(ctx context.Context, rc *compiledResourceCheck, in ResourceCheckInput) (*ResourceCheckResult, error) {
	const op = "engine.RunResourceCheck"
	if !rc.validRange.Contains(in.Before) {
		return nil, apperr.New(op, apperr.InvalidArgument,
			"current "+rc.resource+" must be in ["+itoaRange(rc.validRange.Min)+","+itoaRange(rc.validRange.Max)+"]")
	}
	checkTarget := in.Before
	if in.Target != nil {
		checkTarget = *in.Target
		if !rc.validRange.Contains(checkTarget) {
			return nil, apperr.New(op, apperr.InvalidArgument,
				"explicit target "+rc.resource+" must be in ["+itoaRange(rc.validRange.Min)+","+itoaRange(rc.validRange.Max)+"]")
		}
	}
	check, err := b.Check(ctx, CheckInput{
		Target:    checkTarget,
		Bonus:     in.Bonus,
		Penalty:   in.Penalty,
		SkillName: rc.label,
	})
	if err != nil {
		return nil, err
	}

	successRank := b.levelIndex[rc.successLevel]
	success := b.levelSucceeds(check.Level, successRank)
	fumbled := check.Level == b.fumbleRank()
	lossSpec := in.FailureLoss
	if success {
		lossSpec = in.SuccessLoss
	}
	var loss int
	var lossRoll *dice.RollResult
	switch {
	case !success && fumbled && rc.fumbleLossMax:
		// A fumble costs the maximum possible failure loss; the loss is
		// not rolled (Dice! .sc convention; declared per resource check).
		loss, err = lossMaximum(lossSpec)
		if err != nil {
			return nil, err
		}
	default:
		loss = lossSpec.Fixed
		if lossSpec.Expr != "" {
			lossRoll, err = b.roller.Roll(ctx, lossSpec.Expr)
			if err != nil {
				return nil, err
			}
			loss = lossRoll.Total
		}
		if lossSpec.Negative {
			loss = -loss
		}
	}
	after := in.Before - loss
	if rc.clampMin != nil && after < *rc.clampMin {
		after = *rc.clampMin
	}
	if rc.clampMax != nil && after > *rc.clampMax {
		after = *rc.clampMax
	}
	// The track never leaves its declared range: a recovery cannot exceed
	// the track maximum and a loss cannot go below its minimum.
	if after < rc.validRange.Min {
		after = rc.validRange.Min
	}
	if after > rc.validRange.Max {
		after = rc.validRange.Max
	}

	return &ResourceCheckResult{
		Kind:          "resource_check",
		SystemID:      b.manifest.ID,
		ID:            rc.id,
		Label:         rc.label,
		Check:         check,
		CheckTarget:   checkTarget,
		LossSpec:      lossSpec.String(),
		Loss:          loss,
		Success:       success,
		LossRoll:      lossRoll,
		FumbleLossMax: !success && fumbled && rc.fumbleLossMax,
		Change: ResourceChange{
			Resource: rc.resource,
			Before:   in.Before,
			After:    after,
			Delta:    after - in.Before,
		},
	}, nil
}

// lossMaximum returns the extreme value of a specification: a fixed value is
// itself, a dice expression is its static maximum. A recovery spec is
// negated, so the "maximum" is the largest change in that direction.
func lossMaximum(spec LossSpec) (int, error) {
	magnitude := spec.Fixed
	if spec.Expr != "" {
		_, max, err := dice.ExprBounds(spec.Expr, dice.DefaultLimits())
		if err != nil {
			return 0, apperr.Wrap("engine.lossMaximum", apperr.Internal,
				"cannot bound loss expression "+spec.Expr, err)
		}
		magnitude = max
	}
	if spec.Negative {
		return -magnitude, nil
	}
	return magnitude, nil
}

func itoaRange(v int) string { return strconv.Itoa(v) }
