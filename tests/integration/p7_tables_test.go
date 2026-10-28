package integration

import (
	"strings"
	"testing"
)

// P7.2: the draw pile / discard pile distinction, "draw with replacement"
// and "deck put" returning cards.
func TestDeckDiscard(t *testing.T) {
	root := t.TempDir()

	if code, _, errs := runCLI(t, root, "deck", "new", "扑克=A|B|C"); code != 0 {
		t.Fatalf("deck new code=%d err=%s", code, errs)
	}

	// A normal draw moves the card to the discard pile.
	code, out, errs := runCLI(t, root, "draw", "扑克")
	if code != 0 || !strings.Contains(out, "剩 2 张") || !strings.Contains(out, "弃牌堆 1 张") {
		t.Fatalf("draw code=%d out=%q err=%s", code, out, errs)
	}
	code, out, _ = runCLI(t, root, "deck", "show", "扑克")
	if !strings.Contains(out, "2/3") || !strings.Contains(out, "弃牌堆 1 张") {
		t.Fatalf("deck show after draw: %q", out)
	}

	// JSON contract: the discard pile size is reported.
	code, out, errs = runCLI(t, root, "--json", "draw", "扑克")
	if code != 0 || !strings.Contains(out, `"discard":2`) {
		t.Fatalf("json draw discard count code=%d out=%q err=%s", code, out, errs)
	}

	// Drawing with replacement leaves both piles untouched.
	code, out, errs = runCLI(t, root, "draw", "扑克", "5", "放回")
	if code != 0 || !strings.Contains(out, "放回") || !strings.Contains(out, "剩 1 张") {
		t.Fatalf("draw with replacement code=%d out=%q err=%s", code, out, errs)
	}
	code, out, _ = runCLI(t, root, "--json", "draw", "扑克", "3", "放回")
	if code != 0 || !strings.Contains(out, `"replace":true`) {
		t.Fatalf("json replacement draw: %q", out)
	}
	code, out, _ = runCLI(t, root, "deck", "show", "扑克")
	if !strings.Contains(out, "1/3") || !strings.Contains(out, "弃牌堆 2 张") {
		t.Fatalf("piles must be unchanged by a replacement draw: %q", out)
	}

	// Exhausting the deck: every card ends in the discard pile.
	if code, out, _ := runCLI(t, root, "draw", "扑克"); code != 0 || !strings.Contains(out, "剩 0 张") {
		t.Fatalf("final draw: code=%d out=%q", code, out)
	}
	if code, _, _ := runCLI(t, root, "draw", "扑克"); code != 2 {
		t.Fatalf("drawing an exhausted deck must be a usage error, code=%d", code)
	}
	code, out, _ = runCLI(t, root, "deck", "show", "扑克")
	if !strings.Contains(out, "0/3") || !strings.Contains(out, "弃牌堆 3 张") {
		t.Fatalf("exhausted deck view: %q", out)
	}

	// deck put returns named cards from the discard pile and reshuffles.
	code, out, errs = runCLI(t, root, "deck", "put", "扑克", "A")
	if code != 0 || !strings.Contains(out, "1/3") || !strings.Contains(out, "弃牌堆 2 张") {
		t.Fatalf("deck put code=%d out=%q err=%s", code, out, errs)
	}
	// Returning a card that is not in the discard pile fails.
	if code, _, _ := runCLI(t, root, "deck", "put", "扑克", "A"); code != 2 {
		t.Fatalf("putting a non-discarded card again must fail, code=%d", code)
	}

	// reset merges the piles back and reshuffles.
	code, out, errs = runCLI(t, root, "deck", "reset", "扑克")
	if code != 0 || !strings.Contains(out, "3/3") || strings.Contains(out, "弃牌堆") {
		t.Fatalf("deck reset must clear the discard pile: %q err=%s", out, errs)
	}

	// Usage errors.
	for _, args := range [][]string{
		{"deck", "put", "扑克"},
		{"deck", "put", "不存在", "A"},
		{"draw", "扑克", "0"},
		{"draw", "扑克", "2", "乱讲"},
	} {
		if code, _, _ := runCLI(t, root, args...); code != 2 {
			t.Fatalf("%v: code=%d, want 2", args, code)
		}
	}
}
