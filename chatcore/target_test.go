package chatcore

import (
	"context"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The lexical layers of target resolution (0, 1a, 1b) and the command table.
// Everything here runs without an app: it only inspects argument tokens.

func TestSplitTargetTokensLayer0IgnoresNonTargetTokens(t *testing.T) {
	// '@' not first, a lone '@', a placeholder glued to other text and two
	// placeholders in one token are all ordinary text.
	args := []string{"a@b.com", "hp-1", "@", "<@a>tail", "<@a><@b>", "力量-5"}
	rest, tokens := splitTargetTokens(args, nil, false)
	if len(tokens) != 0 {
		t.Fatalf("layer 0 produced tokens: %+v", tokens)
	}
	if !reflect.DeepEqual(rest, args) {
		t.Fatalf("layer 0 changed args: got %v want %v", rest, args)
	}
}

func TestSplitTargetTokensPlaceholdersGated(t *testing.T) {
	// Placeholder recognition is off unless the adapter supplied the message's
	// mentions (see placeholdersEnabled). A gated-off placeholder must be byte
	// for byte what it was before T16b: ordinary text, not stripped, not
	// reported, not dropped.
	args := []string{"<@openid-1>", "<@!openid-1>", "心理学"}

	rest, tokens := splitTargetTokens(args, nil, false)
	if len(tokens) != 0 {
		t.Fatalf("gated-off placeholder became a target: %+v", tokens)
	}
	if !reflect.DeepEqual(rest, args) {
		t.Fatalf("gated-off placeholder changed args: got %v want %v", rest, args)
	}

	// The gate itself reads the execution context: no mentions, no placeholders.
	if placeholdersEnabled(context.Background()) {
		t.Fatal("placeholders must stay disabled without mention hints")
	}
	if !placeholdersEnabled(withMentions(context.Background(), MentionHint{MemberOpenid: "x"})) {
		t.Fatal("placeholders must be enabled once hints are supplied")
	}
}

func TestSplitTargetTokensLayer1aGroupMentionStays(t *testing.T) {
	// A group-wide mention is typed by a person and may be part of the text,
	// so it stays in rest untouched.
	for _, tok := range []string{"@所有人", "@全体成员", "@{all}"} {
		args := []string{tok, "hp-1"}
		rest, tokens := splitTargetTokens(args, nil, false)
		if len(tokens) != 0 {
			t.Fatalf("%s became a target: %+v", tok, tokens)
		}
		if !reflect.DeepEqual(rest, args) {
			t.Fatalf("%s was not preserved: %v", tok, rest)
		}
	}
}

func TestSplitTargetTokensLayer1bDropsBotIdentity(t *testing.T) {
	// The bot's own mention is inserted by the platform, so it is dropped —
	// not kept as text. Keeping it would add one token to every group command.
	bot := []string{"bot-openid", "骰娘小助手"}
	for _, tok := range []string{"<@bot-openid>", "<@!bot-openid>", "@骰娘小助手"} {
		rest, tokens := splitTargetTokens([]string{tok, "1d6"}, bot, true)
		if len(tokens) != 0 {
			t.Fatalf("%s became a target: %+v", tok, tokens)
		}
		if !reflect.DeepEqual(rest, []string{"1d6"}) {
			t.Fatalf("%s not dropped: rest = %v", tok, rest)
		}
	}

	// A text "@骰娘小助手" is recognized with placeholders off as well: the
	// identity covers both spellings, so the gate cannot leak the bot's token
	// into an argument list.
	rest, tokens := splitTargetTokens([]string{"@骰娘小助手", "1d6"}, bot, false)
	if len(tokens) != 0 || !reflect.DeepEqual(rest, []string{"1d6"}) {
		t.Fatalf("text bot mention not dropped: rest=%v tokens=%v", rest, tokens)
	}

	// Without the bot identities the resolver cannot know, so the token is a
	// target instead of being dropped.
	rest, tokens = splitTargetTokens([]string{"<@bot-openid>", "1d6"}, nil, true)
	if len(tokens) != 1 || len(rest) != 1 {
		t.Fatalf("token dropped without identities: rest=%v tokens=%v", rest, tokens)
	}
}

func TestSplitTargetTokensShapes(t *testing.T) {
	cases := []struct {
		tok      string
		ref      string
		platform bool
	}{
		{"<@openid-1>", "openid-1", true},
		{"<@!openid-1>", "openid-1", true},
		{"@张三", "张三", false},
		{"@人@名", "人@名", false},
	}
	for _, tc := range cases {
		rest, tokens := splitTargetTokens([]string{tc.tok, "1d6"}, nil, true)
		if len(tokens) != 1 {
			t.Fatalf("%q: tokens = %+v", tc.tok, tokens)
		}
		if tokens[0].ref != tc.ref || tokens[0].platform != tc.platform {
			t.Fatalf("%q = %+v want ref=%q platform=%v", tc.tok, tokens[0], tc.ref, tc.platform)
		}
		if !reflect.DeepEqual(rest, []string{"1d6"}) {
			t.Fatalf("%q not stripped: %v", tc.tok, rest)
		}
	}

	// A doubled '@' is roll syntax (".r @@@" is a parse-error test), not an
	// escaped mention, so it must never become a target.
	rest, tokens := splitTargetTokens([]string{"@@@"}, nil, true)
	if len(tokens) != 0 || !reflect.DeepEqual(rest, []string{"@@@"}) {
		t.Fatalf("@@@ must stay text: rest=%v tokens=%v", rest, tokens)
	}
}

func TestSplitTargetTokensPreservesOrder(t *testing.T) {
	// rest keeps the original order with the target tokens removed; the tokens
	// keep their own order.
	cases := []struct {
		name string
		args []string
		rest []string
		refs []string
	}{
		{"head", []string{"@张三", "1d6"}, []string{"1d6"}, []string{"张三"}},
		{"middle", []string{"1d6", "@张三", "破门"}, []string{"1d6", "破门"}, []string{"张三"}},
		{"tail", []string{"1d6", "破门", "@张三"}, []string{"1d6", "破门"}, []string{"张三"}},
		{"both", []string{"@张三", "1d6", "@李四"}, []string{"1d6"}, []string{"张三", "李四"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rest, tokens := splitTargetTokens(tc.args, nil, false)
			if !reflect.DeepEqual(rest, tc.rest) {
				t.Errorf("rest = %v want %v", rest, tc.rest)
			}
			refs := make([]string, 0, len(tokens))
			for _, tk := range tokens {
				refs = append(refs, tk.ref)
			}
			if !reflect.DeepEqual(refs, tc.refs) {
				t.Errorf("refs = %v want %v", refs, tc.refs)
			}
		})
	}
}

// phaseOneStrip is the phase-1 strip list, in sorted order: read-only checks
// only. Everything that writes a card waits for the T08 permission check
// (sc / rc / rch / en), or for its own task (st / pc). rah / rch are excluded
// on purpose: the group adapter rewrites them to ra / rc before dispatch, and
// on the CLI they are rejected before any check runs.
var phaseOneStrip = []string{"ra", "rb", "rc", "rp"}

// phaseOneModes is the expected policy for every row of commandMetas. It is
// written out in full on purpose: a command decided by omission is exactly the
// state this table exists to remove.
var phaseOneModes = map[string]mentionMode{
	// strip: read-only checks.
	"ra": mentionModeStrip, "rb": mentionModeStrip,
	"rc": mentionModeStrip, "rp": mentionModeStrip,

	// none: rolls that carry free text (a hint points at the commands that do
	// take a target).
	"r": mentionModeNone, "rs": mentionModeNone,
	"w": mentionModeNone, "ww": mentionModeNone,
	"h": mentionModeNone, "rh": mentionModeNone, "rd": mentionModeNone,
	"rah": mentionModeNone, "rch": mentionModeNone,

	// none: write paths waiting for T08.
	"sc": mentionModeNone, "st": mentionModeNone,
	"pc": mentionModeNone, "en": mentionModeNone,
	"ds": mentionModeNone, // death saves write counters to the card meta.

	// none: no single card to address.
	"rav": mentionModeNone,
	"ak":  mentionModeNone, "deck": mentionModeNone,
	"draw": mentionModeNone, "drawh": mentionModeNone,
	"log": mentionModeNone, "tb": mentionModeNone,
	"rule": mentionModeNone, "game": mentionModeNone,
	"set": mentionModeNone, "ti": mentionModeNone, "li": mentionModeNone,
	"name": mentionModeNone, "ri": mentionModeNone, "init": mentionModeNone,
	"help": mentionModeNone, "帮助": mentionModeNone,
}

// specViolations reports every way table departs from the phase-1 spec. It is
// a function (not inlined) so the mutation test below can prove the check is
// not vacuous.
func specViolations(table map[string]cmdMeta) []string {
	var out []string
	for name, want := range phaseOneModes {
		if got := table[name].mentionMode; got != want {
			out = append(out, name+": mode "+modeName(got)+", want "+modeName(want))
		}
	}
	for name := range table {
		if _, ok := phaseOneModes[name]; !ok {
			out = append(out, name+": no decision in the spec (add it to phaseOneModes)")
		}
		// Only top-level command names are registered here: a subcommand key
		// would look like "pc show", and its policy belongs in the command
		// implementation, not in this table. Catching it here turns "someone
		// split a subcommand row" from a silent gap into a failing test.
		if strings.ContainsAny(name, " \t") {
			out = append(out, "key "+strconv.Quote(name)+
				": the command table registers top-level commands only; a subcommand policy belongs in the command implementation")
		}
	}
	if len(table) != len(phaseOneModes) {
		out = append(out, "row count "+itoa(len(table))+" != spec rows "+itoa(len(phaseOneModes)))
	}
	var strip []string
	for name, meta := range table {
		if meta.mentionMode == mentionModeStrip {
			strip = append(strip, name)
		}
	}
	sort.Strings(strip)
	if !reflect.DeepEqual(strip, phaseOneStrip) {
		out = append(out, "strip set is "+strings.Join(strip, ",")+
			", want "+strings.Join(phaseOneStrip, ","))
	}
	for name := range commandNames {
		if _, ok := phaseOneModes[name]; !ok {
			out = append(out, "command "+name+" has no spec decision")
		}
	}
	return out
}

func modeName(m mentionMode) string {
	switch m {
	case mentionModeNone:
		return "none"
	case mentionModeStrip:
		return "strip"
	case mentionModeInline:
		return "inline"
	default:
		return "mentionMode(?)"
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestCommandMetaMatchesSpec(t *testing.T) {
	// Spec lock: the strip set is compared as a set (not by count), every row
	// has an explicit decision, and every dispatched command has a decision.
	for _, v := range specViolations(commandMetas) {
		t.Error(v)
	}
}

func TestCommandMetaMatchesSpecIsNotVacuous(t *testing.T) {
	// Mutation check: reopening a write path must fail the spec lock. Without
	// this, the test above could be passing because it checks nothing.
	base := specViolations(commandMetas)
	if len(base) != 0 {
		t.Fatalf("baseline is already broken: %v", base)
	}
	for _, name := range []string{"sc", "en", "st", "pc"} {
		mutated := make(map[string]cmdMeta, len(commandMetas))
		for k, v := range commandMetas {
			mutated[k] = v
		}
		m := mutated[name]
		m.mentionMode = mentionModeStrip
		mutated[name] = m
		if len(specViolations(mutated)) == 0 {
			t.Fatalf("reopening %q did not fail the spec lock", name)
		}
	}
	// Removing a row must fail too: a command decided by omission.
	mutated := make(map[string]cmdMeta, len(commandMetas))
	for k, v := range commandMetas {
		if k == "drawh" {
			continue
		}
		mutated[k] = v
	}
	if len(specViolations(mutated)) == 0 {
		t.Fatal("dropping a row did not fail the spec lock")
	}
}

func TestCommandMetaCoversEveryCommand(t *testing.T) {
	// A missing row would silently mean "no target support, no scanning" — the
	// exact state T16b removes. Every dispatched command must have a policy.
	for name := range commandNames {
		if _, ok := commandMetas[name]; !ok {
			t.Errorf("command %q has no target policy row", name)
		}
	}
	// An unknown command gets the zero policy: no scanning, no target.
	if got := commandMeta("nope"); got.mentionMode != mentionModeNone || got.name != "nope" {
		t.Fatalf("unknown command meta = %+v", got)
	}
}

// phaseGatedHints are the commands whose hint says "wait for T08": the write
// paths that are closed until the ownership check exists.
var phaseGatedHints = []string{"en", "pc", "sc", "st"}

func TestCommandMetaPhaseGatedHintsAreExactlyTheWritePaths(t *testing.T) {
	// The hint text is the only record that a command is closed by phase, so it
	// is also the only thing that will notice when the phase ends: the day sc /
	// st / pc / en reopen, this test goes red and forces the four hints to be
	// rewritten along with the modes. Without it, "reopened the command but left
	// the hint saying wait for T08" ships silently.
	var gated []string
	for name, meta := range commandMetas {
		if strings.Contains(meta.noTargetHint, "T08") {
			gated = append(gated, name)
		}
	}
	sort.Strings(gated)
	if !reflect.DeepEqual(gated, phaseGatedHints) {
		t.Fatalf("T08-gated hints = %v want %v\n"+
			"这条锁存在的理由：T08 上线时它会红，逼人把 4 条 hint 与 mentionMode 一起改，"+
			"免得命令已开放而文案还写着「请等待 T08」。改 mode 时请同步改 hint。",
			gated, phaseGatedHints)
	}
	// The gate marker must never appear on a command that already takes a
	// target: that combination would mean the two halves disagree.
	for _, name := range phaseGatedHints {
		if got := commandMeta(name).mentionMode; got != mentionModeNone {
			t.Errorf("%s is marked as waiting for T08 but its mode is %s", name, modeName(got))
		}
	}
}

// hintCommandRE pulls the command names out of a "please use X instead" hint.
// Only lower-case names are accepted: a hint is player-facing text, and a
// command is spelled lowercase.
var hintCommandRE = regexp.MustCompile(`\.([a-z]+)`)

func TestCommandMetaHintsPointAtRealCommands(t *testing.T) {
	// A hint that names a command we do not have is worse than no hint: it
	// sends the player to a usage error. ".rx" is phase 2 (P1-13).
	want := map[string]string{
		"r":     "代骰请用 .ra",
		"rs":    "代骰请用 .ra",
		"w":     "代骰请用 .ra",
		"ww":    "代骰请用 .ra",
		"drawh": "暗抽不支持指定目标",
		"rah":   "暗骰不支持指定目标",
		"rch":   "暗骰不支持指定目标",
		"ak":    "抽签不支持指定目标",
		"deck":  "牌堆不支持指定目标",
		"draw":  "牌堆不支持指定目标",
		"log":   "记录不支持指定目标",
		"sc":    writePathHint,
		"st":    writePathHint,
		"pc":    writePathHint,
		"en":    writePathHint,
	}
	for name, hint := range want {
		if got := commandMeta(name).noTargetHint; got != hint {
			t.Errorf("%s hint = %q want %q", name, got, hint)
		}
	}
	for name, meta := range commandMetas {
		hint := meta.noTargetHint
		if strings.Contains(hint, ".rx") {
			t.Errorf("%s hint points at the unimplemented .rx", name)
		}
		if hint == "" {
			continue
		}
		// Semantic rule: every ".command" a hint names must actually take a
		// target in this batch. A hint that points at a closed door makes the
		// player fail twice, so "." without a valid, open command is an error
		// rather than a typo we ignore.
		matches := hintCommandRE.FindAllStringSubmatch(hint, -1)
		if strings.Contains(hint, ".") && len(matches) != strings.Count(hint, ".") {
			t.Errorf("%s hint %q has a '.' that is not a command name", name, hint)
		}
		for _, m := range matches {
			ref := m[1]
			if ref == "rx" {
				t.Errorf("%s hint points at the unimplemented .rx", name)
				continue
			}
			target, ok := commandMetas[ref]
			if !ok {
				t.Errorf("%s hint points at unknown command .%s", name, ref)
				continue
			}
			if target.mentionMode != mentionModeStrip {
				t.Errorf("%s: noTargetHint 里的命令 .%s 必须能代骰（mentionMode=strip）；"+
					"查询类提示请写进 notFoundMessage，不要写进 hint", name, ref)
			}
		}
	}
}
