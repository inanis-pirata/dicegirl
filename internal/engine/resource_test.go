package engine_test

import (
	"bytes"
	"context"
	"testing"

	"dicegirl/internal/apperr"
	"dicegirl/internal/engine"
	"dicegirl/rulebooks"
)

func sanityCheck(t *testing.T, b *engine.Book, before int, in engine.ResourceCheckInput) *engine.ResourceCheckResult {
	t.Helper()
	rc, ok := b.ResourceCheck("sanity")
	if !ok {
		t.Fatal("rulebook declares no sanity resource check")
	}
	in.Before = before
	res, err := b.RunResourceCheck(context.Background(), rc, in)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// A successful sanity check (raw 42 against SAN 50) loses nothing.
func TestResourceCheckSuccess(t *testing.T) {
	b := scriptedBook(t, 41) // d100 face 42
	rc, _ := b.ResourceCheck("sanity")
	res := sanityCheck(t, b, 50, engine.ResourceCheckInput{
		SuccessLoss: rc.DefaultSuccessLoss(),
		FailureLoss: rc.DefaultFailureLoss(),
	})
	if !res.Success || res.Loss != 0 || res.Change.After != 50 {
		t.Fatalf("success=%v loss=%d after=%d", res.Success, res.Loss, res.Change.After)
	}
}

// A failed check (raw 86 against SAN 50) rolls the failure loss (1d10 ->
// face 3) and clamps the track.
func TestResourceCheckFailureDiceLoss(t *testing.T) {
	b := scriptedBook(t, 85, 2) // check: d100 86; loss 1d10 face 3
	rc, _ := b.ResourceCheck("sanity")
	res := sanityCheck(t, b, 50, engine.ResourceCheckInput{
		SuccessLoss: rc.DefaultSuccessLoss(),
		FailureLoss: rc.DefaultFailureLoss(),
	})
	if res.Success {
		t.Fatal("raw 86 vs SAN 50 must fail")
	}
	if res.Loss != 3 || res.LossRoll == nil || res.Change.After != 47 || res.Change.Delta != -3 {
		t.Fatalf("loss=%d roll=%v after=%d delta=%d",
			res.Loss, res.LossRoll, res.Change.After, res.Change.Delta)
	}
}

// A fixed loss larger than the current value clamps at zero.
func TestResourceCheckClamp(t *testing.T) {
	b := scriptedBook(t, 95) // d100 96, fumble vs SAN 10
	res := sanityCheck(t, b, 10, engine.ResourceCheckInput{
		SuccessLoss: engine.LossSpec{Fixed: 0},
		FailureLoss: engine.LossSpec{Fixed: 20},
	})
	if res.Success || res.Change.After != 0 || res.Change.Delta != -10 {
		t.Fatalf("success=%v after=%d delta=%d", res.Success, res.Change.After, res.Change.Delta)
	}
	// Fixed spec under fumble_loss: max is the fixed amount itself, and the
	// result is marked fumble-max even though clamping hid the difference.
	if !res.FumbleLossMax || res.Loss != 20 {
		t.Fatalf("fumbleMax=%v loss=%d, want true/20", res.FumbleLossMax, res.Loss)
	}
}

// On a fumble the rulebook declares fumble_loss: max: the failure loss is
// its static maximum and is not rolled. 00/0 = 100 is a fumble at SAN 50;
// 1d4+1 must therefore cost 5 (4+1), consuming no randomness beyond the
// check's two dice.
func TestResourceCheckFumbleTakesMaximumLoss(t *testing.T) {
	b := scriptedBook(t, 99) // d100 100
	res := sanityCheck(t, b, 50, engine.ResourceCheckInput{
		SuccessLoss: engine.LossSpec{Fixed: 0},
		FailureLoss: mustLossSpec(t, "1d4+1"),
	})
	if res.Success {
		t.Fatal("raw 100 must fail")
	}
	if !res.FumbleLossMax {
		t.Fatal("fumble_loss: max must mark the result")
	}
	if res.Loss != 5 || res.LossRoll != nil {
		t.Fatalf("loss=%d roll=%v, want max 5 with no loss roll", res.Loss, res.LossRoll)
	}
	if res.Change.After != 45 || res.Change.Delta != -5 {
		t.Fatalf("after=%d delta=%d, want 45/-5", res.Change.After, res.Change.Delta)
	}

	// The default failure spec "1d10" costs its 10-face maximum.
	b2 := scriptedBook(t, 99)
	rc, _ := b2.ResourceCheck("sanity")
	res2 := sanityCheck(t, b2, 50, engine.ResourceCheckInput{
		SuccessLoss: rc.DefaultSuccessLoss(),
		FailureLoss: rc.DefaultFailureLoss(),
	})
	if res2.Loss != 10 || !res2.FumbleLossMax || res2.LossRoll != nil {
		t.Fatalf("default fumble loss=%d max=%v roll=%v, want 10/max/nil",
			res2.Loss, res2.FumbleLossMax, res2.LossRoll)
	}

	// Low SAN: 96 is already a fumble, and the maximum still applies.
	b3 := scriptedBook(t, 95) // d100 96
	res3 := sanityCheck(t, b3, 10, engine.ResourceCheckInput{
		SuccessLoss: engine.LossSpec{Fixed: 0},
		FailureLoss: mustLossSpec(t, "1d10"),
	})
	if res3.Loss != 10 || !res3.FumbleLossMax {
		t.Fatalf("low-SAN fumble loss=%d max=%v, want 10/true", res3.Loss, res3.FumbleLossMax)
	}
}

func mustLossSpec(t *testing.T, s string) engine.LossSpec {
	t.Helper()
	spec, err := engine.ParseLossSpec(s)
	if err != nil {
		t.Fatalf("ParseLossSpec(%q): %v", s, err)
	}
	return spec
}

func TestResourceCheckFumbleLossConfig(t *testing.T) {
	data, err := rulebooks.FS.ReadFile("coc7.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// The trailing newline distinguishes the real key from its comment.
	marker := []byte("fumble_loss: max\n")
	if !bytes.Contains(data, marker) {
		t.Fatal("test fixture marker missing from coc7.yaml")
	}
	bad := bytes.Replace(data, marker, []byte("fumble_loss: weird\n"), 1)
	if _, err := engine.ParseBook(bad, "bad.yaml", nil); !apperr.IsCode(err, apperr.ConfigInvalid) {
		t.Fatalf("invalid fumble_loss code = %v, want CONFIG_INVALID", err)
	}
	ok := bytes.Replace(data, marker, []byte("fumble_loss: normal\n"), 1)
	book, err := engine.ParseBook(ok, "normal.yaml", nil)
	if err != nil {
		t.Fatalf("fumble_loss: normal must be accepted: %v", err)
	}
	rc, exists := book.ResourceCheck("sanity")
	if !exists {
		t.Fatal("sanity check missing")
	}
	_ = rc
}

func TestResourceCheckRejectsOutOfRangeBefore(t *testing.T) {
	b := coc7Book(t, nil)
	rc, _ := b.ResourceCheck("sanity")
	if _, err := b.RunResourceCheck(context.Background(), rc, engine.ResourceCheckInput{Before: 100}); err == nil {
		t.Fatal("before=100 outside declared range must be rejected")
	}
}

func TestParseLossSpec(t *testing.T) {
	fixed, err := engine.ParseLossSpec("7")
	if err != nil || fixed.Fixed != 7 || fixed.Expr != "" {
		t.Fatalf("fixed spec = %+v, %v", fixed, err)
	}
	diced, err := engine.ParseLossSpec("1d4+1")
	if err != nil || diced.Expr != "1d4+1" {
		t.Fatalf("dice spec = %+v, %v", diced, err)
	}
	// A signed spec recovers the track instead of losing it.
	recover, err := engine.ParseLossSpec("-3")
	if err != nil || recover.Fixed != 3 || !recover.Negative || recover.String() != "-3" {
		t.Fatalf("recovery spec = %+v, %v", recover, err)
	}
	recoverDice, err := engine.ParseLossSpec("-1d4")
	if err != nil || recoverDice.Expr != "1d4" || !recoverDice.Negative {
		t.Fatalf("recovery dice spec = %+v, %v", recoverDice, err)
	}
	for _, bad := range []string{"", "abc", "-", "--3"} {
		if _, err := engine.ParseLossSpec(bad); err == nil {
			t.Errorf("loss spec %q accepted", bad)
		}
	}
}
