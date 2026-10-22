package integration

import (
	"strings"
	"testing"
)

// The roll statistics of a card: every check is recorded, aggregated per
// skill, and can be reset.
func TestCardStats(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲")

	// A few checks.
	for i := 0; i < 3; i++ {
		if code, _, errs := runCLI(t, root, "ra", "心理学"); code != 0 {
			t.Fatalf("ra code=%d err=%s", code, errs)
		}
	}
	if code, _, errs := runCLI(t, root, "ra", "力量"); code != 0 {
		t.Fatalf("ra code=%d err=%s", code, errs)
	}

	// The statistics show the totals and the per-key breakdown.
	code, out, errs := runCLI(t, root, "pc", "stat")
	if code != 0 {
		t.Fatalf("pc stat code=%d err=%s", code, errs)
	}
	for _, want := range []string{"检定 4 次", "心理学", "力量"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stats output missing %q: %q", want, out)
		}
	}

	// The structured contract.
	code, out, errs = runCLI(t, root, "--json", "pc", "stat")
	if code != 0 {
		t.Fatalf("json stat code=%d err=%s", code, errs)
	}
	for _, want := range []string{`"checks":4`, `"levels":{`, `"keys":[`, `"history":[`, `"key":"psychology"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("json stats missing %q: %q", want, out)
		}
	}

	// Clearing resets the counters.
	code, out, errs = runCLI(t, root, "pc", "stat", "clr")
	if code != 0 {
		t.Fatalf("pc stat clr code=%d err=%s", code, errs)
	}
	code, out, errs = runCLI(t, root, "pc", "stat")
	if code != 0 || !strings.Contains(out, "还没有掷骰记录") {
		t.Fatalf("stats after clear: %q", out)
	}

	// A card without statistics reports nothing.
	code, out, errs = runCLI(t, root, ".pc", "new", "另一人")
	if code != 0 {
		t.Fatalf("create code=%d err=%s", code, errs)
	}
	code, out, errs = runCLI(t, root, ".pc", "stat", "另一人")
	if code != 0 || !strings.Contains(out, "还没有掷骰记录") {
		t.Fatalf("stats of a fresh card: %q", out)
	}
}

// cardIDOf returns the id of the card with the given name.
func cardIDOf(t *testing.T, root, name string) string {
	t.Helper()
	_, out, errs := runCLI(t, root, "pc", "list")
	if errs != "" && !strings.Contains(out, "c-") {
		t.Fatalf("pc list: %q", errs)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, name) {
			return cardIDRE.FindString(line)
		}
	}
	t.Fatalf("card %q not found in %q", name, out)
	return ""
}
