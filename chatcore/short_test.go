package chatcore

import (
	"reflect"
	"testing"

	"dicegirl/internal/app"
	"dicegirl/internal/engine"
)

func TestShortToken(t *testing.T) {
	cases := map[string]struct {
		name     string
		prefixed bool
	}{
		".r":   {"r", true},
		"。ra":  {"ra", true},
		"!sc":  {"sc", true},
		"！st":  {"st", true},
		"r":    {"r", false},
		"roll": {"roll", false},
		".":    {".", false}, // a lone punctuation is not an alias prefix
	}
	for in, want := range cases {
		name, prefixed := shortToken(in)
		if name != want.name || prefixed != want.prefixed {
			t.Errorf("shortToken(%q) = %q,%v want %q,%v", in, name, prefixed, want.name, want.prefixed)
		}
	}
}

func TestSplitCommandToken(t *testing.T) {
	cases := []struct {
		in           string
		extra        []string
		name, glued  string
		prefixed, ok bool
	}{
		{".r3d6", nil, "r", "3d6", true, true},
		{"r1d10", nil, "r", "1d10", false, true},
		{".ra心理学+2", nil, "ra", "心理学+2", true, true},
		{"ra60", nil, "ra", "60", false, true},
		{".drawh命运", nil, "drawh", "命运", true, true},
		{".draw命运", nil, "draw", "命运", true, true},
		{"ww10a8", nil, "ww", "10a8", false, true},
		{"initclr", nil, "initclr", "", false, false}, // ASCII-letter tail stays unrecognized
		{"reset", nil, "reset", "", false, false},     // casual word, not "r est"
		{".reset", nil, "r", "eset", true, true},      // a punctuation mark states intent

		{".xyz", nil, "xyz", "", true, false},
		{"r", nil, "r", "", false, true},
		{".r", nil, "r", "", true, true},
		{".coc6d", nil, "coc6d", "", true, false}, // generation aliases stay intact for bookCommand
		{".objoin", []string{"ob", "log"}, "ob", "join", true, true},
		{"logstop", []string{"ob", "log"}, "logstop", "", false, false},
	}
	for _, tc := range cases {
		name, glued, prefixed, ok := SplitCommandToken(tc.in, tc.extra...)
		if name != tc.name || glued != tc.glued || prefixed != tc.prefixed || ok != tc.ok {
			t.Errorf("SplitCommandToken(%q) = %q,%q,%v,%v want %q,%q,%v,%v",
				tc.in, name, glued, prefixed, ok, tc.name, tc.glued, tc.prefixed, tc.ok)
		}
	}
}

func TestParseRATokens(t *testing.T) {
	ok := []struct {
		in             []string
		name           string
		target         int
		explicit       bool
		bonus, penalty int
		multiply       int
		divide         int
	}{
		{[]string{"心理学"}, "心理学", 0, false, 0, 0, 0, 0},
		{[]string{"60"}, "", 60, true, 0, 0, 0, 0},
		{[]string{"心理学", "60"}, "心理学", 60, true, 0, 0, 0, 0},
		{[]string{"心理学+2"}, "心理学", 0, false, 2, 0, 0, 0},
		{[]string{"心理学-1"}, "心理学", 0, false, 0, 1, 0, 0},
		{[]string{"心理学", "+2"}, "心理学", 0, false, 2, 0, 0, 0},
		{[]string{"STR", "-2"}, "STR", 0, false, 0, 2, 0, 0},
		// Target arithmetic on the name token.
		{[]string{"体质*5"}, "体质", 0, false, 0, 0, 5, 0},
		{[]string{"心理学/2"}, "心理学", 0, false, 0, 0, 0, 2},
		{[]string{"体质*5+2"}, "体质", 0, false, 2, 0, 5, 0},
		// A numeric core with an inline modifier is an explicit target
		// with bonus/penalty dice ("15+1" = advantage on a DC 15 check).
		{[]string{"15+1"}, "", 15, true, 1, 0, 0, 0},
		{[]string{"15-1"}, "", 15, true, 0, 1, 0, 0},
	}
	for _, tc := range ok {
		spec, err := parseRATokens(tc.in)
		if err != nil {
			t.Errorf("%v: %v", tc.in, err)
			continue
		}
		if spec.name != tc.name || spec.target != tc.target || spec.explicit != tc.explicit ||
			spec.bonus != tc.bonus || spec.penalty != tc.penalty ||
			spec.multiply != tc.multiply || spec.divide != tc.divide {
			t.Errorf("%v = %+v, want name=%q target=%d explicit=%v +%d/-%d *%d//%d",
				tc.in, spec, tc.name, tc.target, tc.explicit, tc.bonus, tc.penalty,
				tc.multiply, tc.divide)
		}
	}

	bad := [][]string{
		{"心理学", "聆听"}, // two names
		{"60", "70"},  // two targets
		{"+x"},        // malformed standalone modifier
		{"体质*0"},      // a zero factor is meaningless
	}
	for _, in := range bad {
		if _, err := parseRATokens(in); err == nil {
			t.Errorf("%v: expected error", in)
		}
	}
}

func TestParseCurrentAndReason(t *testing.T) {
	cases := []struct {
		in     []string
		want   int
		hasCur bool
		reason string
	}{
		{nil, 0, false, ""},
		{[]string{"直面外神"}, 0, false, "直面外神"},
		{[]string{"70"}, 70, true, ""},
		{[]string{"70", "直面外神"}, 70, true, "直面外神"},
	}
	for _, tc := range cases {
		cur, reason := parseCurrentAndReason(tc.in)
		if tc.hasCur {
			if cur == nil || *cur != tc.want {
				t.Errorf("%v: current = %v, want %d", tc.in, cur, tc.want)
			}
		} else if cur != nil {
			t.Errorf("%v: current = %d, want none", tc.in, *cur)
		}
		if reason != tc.reason {
			t.Errorf("%v: reason = %q, want %q", tc.in, reason, tc.reason)
		}
	}
}

func TestParseLossPair(t *testing.T) {
	s, f, err := parseLossPair("0/1d10")
	if err != nil || s != "0" || f != "1d10" {
		t.Fatalf("pair = %q,%q err=%v", s, f, err)
	}
	for _, bad := range []string{"1d10", "/1d10", "0/", "a/b/c"} {
		if _, _, err := parseLossPair(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestStripDifficulty(t *testing.T) {
	diffs := []engine.DifficultyInfo{
		{ID: "extreme", Name: "极难"},
		{ID: "hard", Name: "困难"},
		{ID: "auto", Name: "自动成功"},
	}
	cases := []struct {
		in   []string
		id   string
		rest []string
	}{
		{[]string{"困难心理学", "60"}, "hard", []string{"心理学", "60"}},
		{[]string{"困难", "心理学"}, "hard", []string{"心理学"}},
		{[]string{"自动成功心理学"}, "auto", []string{"心理学"}}, // longest keyword wins
		{[]string{"心理学", "+2"}, "", []string{"心理学", "+2"}},
		{[]string{"60"}, "", []string{"60"}},
		{nil, "", nil},
	}
	for _, tc := range cases {
		id, rest := stripDifficulty(tc.in, diffs)
		if id != tc.id || !reflect.DeepEqual(rest, tc.rest) {
			t.Errorf("stripDifficulty(%v) = %q,%v want %q,%v", tc.in, id, rest, tc.id, tc.rest)
		}
	}

	// Rulebooks without difficulties never strip anything.
	if id, rest := stripDifficulty([]string{"困难心理学"}, nil); id != "" || len(rest) != 1 {
		t.Errorf("no-difficulties case = %q,%v", id, rest)
	}
}

func TestStripRepeatPrefix(t *testing.T) {
	cases := []struct {
		in                     []string
		repeat, bonus, penalty int
		rest                   []string
		present                bool
	}{
		{[]string{"3#心理学"}, 3, 0, 0, []string{"心理学"}, true},
		{[]string{"3#", "心理学"}, 3, 0, 0, []string{"心理学"}, true},
		{[]string{"3#p", "心理学"}, 3, 0, 1, []string{"心理学"}, true},
		{[]string{"3#b2", "心理学", "60"}, 3, 2, 0, []string{"心理学", "60"}, true},
		{[]string{"2#困难心理学"}, 2, 0, 0, []string{"困难心理学"}, true},
		{[]string{"3#心理学+2"}, 3, 0, 0, []string{"心理学+2"}, true},
		{[]string{"心理学"}, 0, 0, 0, []string{"心理学"}, false},
		{[]string{"60"}, 0, 0, 0, []string{"60"}, false},
		{nil, 0, 0, 0, nil, false},
	}
	for _, tc := range cases {
		repeat, bonus, penalty, rest, present := stripRepeatPrefix(tc.in)
		if repeat != tc.repeat || bonus != tc.bonus || penalty != tc.penalty ||
			present != tc.present || !reflect.DeepEqual(rest, tc.rest) {
			t.Errorf("stripRepeatPrefix(%v) = n=%d +%d/-%d rest=%v present=%v; want n=%d +%d/-%d rest=%v present=%v",
				tc.in, repeat, bonus, penalty, rest, present,
				tc.repeat, tc.bonus, tc.penalty, tc.rest, tc.present)
		}
	}
}

func TestParsePoolArgs(t *testing.T) {
	cases := []struct {
		in    []string
		count string
		add   int
		ok    bool
	}{
		{nil, "1", 0, true},
		{[]string{"5"}, "5", 0, true},
		{[]string{"5a8"}, "5", 8, true},
		{[]string{"5", "a8"}, "5", 8, true},
		{[]string{"1d4+1a8"}, "1d4+1", 8, true},
		{[]string{"5x8"}, "5x8", 0, true}, // the "x" multiplication alias
		{[]string{"5", "a"}, "", 0, false},
		{[]string{"5a11"}, "5", 0, false},
		{[]string{"5a0"}, "5", 0, false},
	}
	for _, tc := range cases {
		count, add, err := parsePoolArgs(tc.in)
		if tc.ok {
			if err != nil {
				t.Errorf("%v: %v", tc.in, err)
				continue
			}
			if count != tc.count || add != tc.add {
				t.Errorf("%v = %q/%d, want %q/%d", tc.in, count, add, tc.count, tc.add)
			}
			continue
		}
		if err == nil {
			t.Errorf("%v: expected an error", tc.in)
		}
	}
}

func TestParseInitiativeArgs(t *testing.T) {
	cases := []struct {
		in       []string
		modifier int
		value    int
		hasValue bool
		expr     string
		name     string
	}{
		{nil, 0, 0, false, "", ""},
		{[]string{"+5", "首领"}, 5, 0, false, "", "首领"},
		{[]string{"-1"}, -1, 0, false, "", ""},
		{[]string{"80", "怪物甲"}, 0, 80, true, "", "怪物甲"},
		{[]string{"2d20kh1", "英雄"}, 0, 0, false, "2d20kh1", "英雄"},
	}
	for _, tc := range cases {
		opts, err := app.ParseInitiativeArgs(tc.in)
		if err != nil {
			t.Errorf("%v: %v", tc.in, err)
			continue
		}
		if opts.Modifier != tc.modifier || opts.Expr != tc.expr || opts.Name != tc.name {
			t.Errorf("%v = %+v", tc.in, opts)
		}
		if tc.hasValue {
			if opts.Value == nil || *opts.Value != tc.value {
				t.Errorf("%v: value = %v, want %d", tc.in, opts.Value, tc.value)
			}
		} else if opts.Value != nil {
			t.Errorf("%v: unexpected value %d", tc.in, *opts.Value)
		}
	}
	if _, err := app.ParseInitiativeArgs([]string{"+x"}); err == nil {
		t.Error("a malformed modifier must be rejected")
	}
}

func TestNormalizeInitiativeExpr(t *testing.T) {
	if got := normalizeInitiativeExpr("2DK"); got != "2d20kh1" {
		t.Errorf("2DK = %q, want 2d20kh1", got)
	}
	if got := normalizeInitiativeExpr("2dk"); got != "2d20kh1" {
		t.Errorf("2dk = %q, want 2d20kh1", got)
	}
	if got := normalizeInitiativeExpr("1d20+3"); got != "1d20+3" {
		t.Errorf("own expressions must pass through: %q", got)
	}
}

func TestIsShortCommand(t *testing.T) {
	for _, name := range []string{
		"r", "rs", "ra", "rc", "sc", "st", "pc", "set", "ti", "li", "ak", "name", "en", "rh",
		"w", "ww", "ri", "init", "deck", "draw", "drawh",
		"rules", "sys", "dv", "dmg", "opp", "tb",
	} {
		if !isShortCommand(name) {
			t.Errorf("%s must be a short command", name)
		}
	}
	// The generation aliases come from the rulebooks, not from this table.
	for _, name := range []string{"roll", "check", "resource", "growth", "choice", "coc", "dnd", ""} {
		if isShortCommand(name) {
			t.Errorf("%s must not be a static short command", name)
		}
	}
}

// The generation aliases are rulebook data: an alias may carry a variant
// and the detailed flag.
func TestMatchAlias(t *testing.T) {
	cases := []struct {
		in       string
		alias    string
		variant  string
		detailed bool
		ok       bool
	}{
		{"coc", "coc", "", false, true},
		{"coc6", "coc", "6", false, true},
		{"coc7", "coc", "7", false, true},
		{"cocd", "coc", "", true, true},
		{"coc6d", "coc", "6", true, true},
		{"dnd", "dnd", "", false, true},
		{"dndd", "dnd", "", true, true},
		{"coco", "", "", false, false},
		{"cocx", "", "", false, false},
	}
	for _, tc := range cases {
		alias, variant, detailed, ok := matchAlias(tc.in, []string{"coc", "dnd"})
		if ok != tc.ok {
			t.Errorf("%s: ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		if alias != tc.alias || variant != tc.variant || detailed != tc.detailed {
			t.Errorf("%s = alias %q variant %q detailed %v, want %q/%q/%v",
				tc.in, alias, variant, detailed, tc.alias, tc.variant, tc.detailed)
		}
	}
}
