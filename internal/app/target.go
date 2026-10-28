package app

// Target resolution results are shared vocabulary: chatcore decides which
// card a command addresses (chatcore/target.go), the app layer consumes the
// answer for permission checks (T08). The types live here, not in chatcore,
// because the dependency direction is one-way: cmd/* -> chatcore ->
// internal/app -> engine. A chatcore type would force app to import its
// caller.

import (
	"context"

	"dicegirl/internal/apperr"
)

// Kind reports how the caller wrote the target. It records the input form,
// not where the card came from.
type Kind uint8

const (
	// KindNone is the zero value: no target was written.
	KindNone Kind = iota
	// KindMention is an "@名字" mention in the message text.
	KindMention
	// KindFlag is a "--character <名字|id>" flag.
	KindFlag
	// KindBare is a bare name in a command's target position.
	KindBare
	// KindPlatform is a platform @-mention hint from the adapter.
	KindPlatform
)

// Source reports where the addressed card came from. Permission checks (T08)
// key on it: "the user named this card" and "we silently fell back to the
// table's active card" must not look alike (G13).
type Source uint8

const (
	// SourceNone is the zero value: no card was addressed at all.
	SourceNone Source = iota
	// SourceExplicit is a card the user named (mention, flag or bare name).
	SourceExplicit
	// SourceBound is the card the platform bound to the sender.
	SourceBound
	// SourceActive is a fallback to the table's active card.
	SourceActive
)

// Target is one resolved "which card" answer.
type Target struct {
	CardID string
	// Name is the card's stored name ("" when only a fallback was resolved).
	Name   string
	Source Source
	Kind   Kind
}

// TargetSet is the ordered result of one resolution. Primary is what
// single-target commands read; Targets keeps the order for commands that
// address more than one card (opposed checks, phase 2).
type TargetSet struct {
	Primary Target
	Targets []Target
}

// Validate enforces the single-target contract: a command that has not
// declared multi-target support may not be handed two cards. Reserving
// Targets for the phase-2 opposed check is only worth something if the
// reservation is actually checked, so every producer runs this before the
// set reaches a command.
//
// cmd names the command in the error message ("st 不支持一次指定多个目标");
// an empty cmd falls back to a generic noun.
func (ts TargetSet) Validate(allowMulti bool, cmd string) error {
	if len(ts.Targets) <= 1 || allowMulti {
		return nil
	}
	name := cmd
	if name == "" {
		name = "该命令"
	}
	return apperr.New("app.TargetSet.Validate", apperr.InvalidArgument,
		name+" 不支持一次指定多个目标")
}

type targetKey struct{}

// WithTarget stores one resolved target in ctx. It delegates to WithTargets
// so a later multi-target extension needs no second code path.
func WithTarget(ctx context.Context, t Target) context.Context {
	return WithTargets(ctx, []Target{t})
}

// WithTargets stores the resolved targets in ctx. An empty slice leaves ctx
// untouched.
func WithTargets(ctx context.Context, targets []Target) context.Context {
	if len(targets) == 0 {
		return ctx
	}
	cp := make([]Target, len(targets))
	copy(cp, targets)
	return context.WithValue(ctx, targetKey{}, TargetSet{Primary: cp[0], Targets: cp})
}

// TargetFrom returns the primary resolved target, or the zero Target.
func TargetFrom(ctx context.Context) Target {
	set, _ := ctx.Value(targetKey{}).(TargetSet)
	return set.Primary
}

// TargetsFrom returns all resolved targets in resolution order.
func TargetsFrom(ctx context.Context) []Target {
	set, _ := ctx.Value(targetKey{}).(TargetSet)
	if len(set.Targets) == 0 {
		return nil
	}
	cp := make([]Target, len(set.Targets))
	copy(cp, set.Targets)
	return cp
}
