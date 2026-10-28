package app

import (
	"errors"
	"testing"

	"dicegirl/internal/apperr"
)

// The TargetSet guard is the check that makes "Targets is reserved for the
// phase-2 opposed check" more than a comment: a command that has not declared
// multi-target support cannot be handed two cards.
func TestTargetSetValidate(t *testing.T) {
	one := TargetSet{Primary: Target{CardID: "c-1"}, Targets: []Target{{CardID: "c-1"}}}
	two := TargetSet{
		Primary: Target{CardID: "c-1"},
		Targets: []Target{{CardID: "c-1"}, {CardID: "c-2"}},
	}

	if err := one.Validate(false, "st"); err != nil {
		t.Fatalf("one target must pass: %v", err)
	}
	if err := two.Validate(true, "rav"); err != nil {
		t.Fatalf("declared multi must pass: %v", err)
	}

	err := two.Validate(false, "st")
	if err == nil {
		t.Fatal("two targets on a single-target command must fail")
	}
	if !apperr.IsCode(err, apperr.InvalidArgument) {
		t.Fatalf("code = %s, want INVALID_ARGUMENT", apperr.CodeOf(err))
	}
	var ae *apperr.Error
	if !errors.As(err, &ae) {
		t.Fatalf("error is not *apperr.Error: %T", err)
	}
	if got, want := ae.Message, "st 不支持一次指定多个目标"; got != want {
		t.Fatalf("message = %q want %q", got, want)
	}

	// An unnamed command still produces a usable message.
	if err := two.Validate(false, ""); err == nil {
		t.Fatal("empty command name must still fail")
	} else if got, want := apperr.AsError(err).Message, "该命令 不支持一次指定多个目标"; got != want {
		t.Fatalf("generic message = %q want %q", got, want)
	}
}

// WithTarget must delegate to WithTargets so the single-target and
// multi-target paths can never drift apart.
func TestWithTargetDelegatesToWithTargets(t *testing.T) {
	ctx := WithTarget(t.Context(), Target{CardID: "c-1", Name: "张三", Source: SourceExplicit})
	all := TargetsFrom(ctx)
	if len(all) != 1 || all[0].CardID != "c-1" {
		t.Fatalf("TargetsFrom = %+v", all)
	}
	if got := TargetFrom(ctx); got.CardID != "c-1" || got.Name != "张三" {
		t.Fatalf("TargetFrom = %+v", got)
	}

	// An empty set leaves the context untouched (no zero target recorded).
	bare := WithTargets(t.Context(), nil)
	if got := TargetFrom(bare); got != (Target{}) {
		t.Fatalf("empty WithTargets recorded %+v", got)
	}
	if got := TargetsFrom(bare); got != nil {
		t.Fatalf("empty TargetsFrom = %+v", got)
	}

	// TargetsFrom returns a copy: a caller cannot corrupt the stored set.
	set := []Target{{CardID: "c-1"}, {CardID: "c-2"}}
	multi := WithTargets(t.Context(), set)
	set[0].CardID = "c-9"
	if got := TargetFrom(multi); got.CardID != "c-1" {
		t.Fatalf("stored target was aliased: %+v", got)
	}
	all = TargetsFrom(multi)
	all[1].CardID = "c-9"
	if got := TargetFrom(multi); got.CardID != "c-1" {
		t.Fatalf("TargetsFrom was not a copy: %+v", got)
	}
}
