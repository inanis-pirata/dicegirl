package integration

import (
	"regexp"
	"strings"
	"testing"
)

// The `.pc` family beyond list/show: rename, copy the values of one card
// onto another, regenerate a card and destroy every card.
func TestCardOperations(t *testing.T) {
	root := t.TempDir()
	createInvestigator(t, root, "调查员甲")

	// Rename the active card; its id stays the same.
	code, out, errs := runCLI(t, root, "pc", "nn", "改名后")
	if code != 0 || !strings.Contains(out, "改名后") {
		t.Fatalf("pc nn code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "pc", "list")
	if code != 0 || !strings.Contains(out, "改名后") {
		t.Fatalf("pc list after rename: %q", out)
	}

	// A second card for the copy test.
	code, _, errs = runCLI(t, root, ".pc", "new", "调查员乙", "STR=30", "psychology=70")
	if code != 0 {
		t.Fatalf("create code=%d err=%s", code, errs)
	}
	// The renamed card is the active one; the copy takes the other card's
	// values onto it. Both sides are referenced by name.
	// A name that is already taken cannot be used.
	code, _, errs = runCLI(t, root, ".pc", "nn", "调查员乙", "改名后")
	if code != 2 || !strings.Contains(errs, "already in use") {
		t.Fatalf("duplicate name code=%d err=%q", code, errs)
	}

	// Copying the values of one card onto another keeps the target's
	// identity but takes over its attributes and skills.
	code, out, errs = runCLI(t, root, "pc", "cpy", "改名后=调查员乙")
	if code != 0 {
		t.Fatalf("pc cpy code=%d err=%s", code, errs)
	}
	code, out, errs = runCLI(t, root, ".pc", "show", "改名后")
	if code != 0 {
		t.Fatalf("show code=%d err=%s", code, errs)
	}
	if !strings.Contains(out, "STR 30") || !strings.Contains(out, "psychology 70") {
		t.Fatalf("copied values missing: %q", out)
	}
	if !strings.Contains(out, "改名后") {
		t.Fatalf("the target card must keep its name: %q", out)
	}

	// Copying a card onto itself is a usage error.
	if code, _, _ := runCLI(t, root, "pc", "cpy", "改名后=改名后"); code != 2 {
		t.Fatalf("self-copy code=%d, want 2", code)
	}

	// Regenerate a specific card by name (the Dice! `.pc redo`).
	code, out, errs = runCLISeed(t, root, 3, "pc", "redo", "改名后")
	if code != 0 || !strings.Contains(out, "🎲") {
		t.Fatalf("pc redo code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, ".pc", "show", "改名后")
	if code != 0 || !strings.Contains(out, "STR ") {
		t.Fatalf("show after redo: %q", out)
	}

	// Destroying every card clears the list and the active pointer.
	code, out, errs = runCLI(t, root, "pc", "clr")
	if code != 0 || !strings.Contains(out, "2") {
		t.Fatalf("pc clr code=%d out=%q err=%s", code, out, errs)
	}
	code, out, errs = runCLI(t, root, "pc", "list")
	if code != 0 || !strings.Contains(out, "暂无") {
		t.Fatalf("pc list after clr: %q", out)
	}
	code, _, errs = runCLI(t, root, ".pc", "active")
	if code == 0 {
		t.Fatalf("the active card must be cleared, stderr=%q", errs)
	}
}

// cardIDRE matches a card id in the listing output.
var cardIDRE = regexp.MustCompile(`c-[a-z0-9-]+`)

// characterIDs lists the card ids of a data root (the cards live in the
// database now, so the list comes from the CLI).
func characterIDs(t *testing.T, root string) []string {
	t.Helper()
	_, out, errs := runCLI(t, root, "pc", "list")
	if errs != "" && !strings.Contains(out, "c-") {
		t.Fatalf("pc list: %q", errs)
	}
	return cardIDRE.FindAllString(out, -1)
}
