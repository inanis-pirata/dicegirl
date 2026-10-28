package chatcore

// The chat command grammar (Dice!/BCDice conventions). This file is the
// only place that knows command names and punctuation prefixes: it parses
// free-form chat tokens and calls the internal/app use cases. internal/ must
// never learn these names; every chat adapter reuses this parser.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"dicegirl/internal/app"
	"dicegirl/internal/apperr"
	"dicegirl/internal/engine"
)

// commandPrefixes are the optional leading punctuation marks Dice! accepts.
var commandPrefixes = map[rune]bool{'.': true, '。': true, '!': true, '！': true}

// commandNames is the complete alias table; unknown names get a usage error.
var commandNames = map[string]bool{
	"r": true, "rs": true, "rd": true, "ra": true, "rah": true, "rb": true,
	"rc": true, "rch": true, "rp": true, "sc": true,
	"st": true, "pc": true, "h": true, "rh": true, "set": true,
	"ti": true, "li": true, "ak": true, "name": true, "en": true,
	"w": true, "ww": true,
	"ri": true, "init": true, "deck": true, "draw": true, "drawh": true,
	"rav": true, "tb": true,
	"ds": true,
	"rule": true, "game": true, "log": true,
	"help": true, "帮助": true,
}

// cocFamilyRE and the `.coc`/`.dnd` name families are gone: every rulebook
// declares its own command alias in its manifest (`commands`), so the
// CLI has no per-game command names. See bookCommand in bookcmd.go.

// stripCommandPrefix strips any leading alias punctuation marks, reporting whether
// the token carried at least one. The name is lower-cased so command matching
// is case-insensitive ("R", "R1d3" work like "r", "r1d3").
func stripCommandPrefix(token string) (name string, prefixed bool) {
	s := token
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if size > 0 && commandPrefixes[r] && len(s) > size {
			s = s[size:]
			prefixed = true
			continue
		}
		break
	}
	if s == "" {
		// A lone punctuation mark is not an alias prefix.
		return token, false
	}
	return strings.ToLower(s), prefixed
}

func isCommandName(name string) bool {
	return commandNames[name]
}

// SplitCommandToken normalizes the first whitespace-delimited token of a
// chat line. Besides stripping one optional punctuation mark (stripCommandPrefix), it
// repairs a command name glued to its first argument: ".r3d6" and "r3d6"
// split into ("r", "3d6"). Matching prefers the longest command prefix, so
// ".drawh命运" splits at "drawh", not "draw".
//
// The report is (name, gluedArg, prefixed, ok): gluedArg is the part glued to
// the command ("" for a plain command token), prefixed reports the
// punctuation mark and ok whether a command was recognized. Adapter-only
// command names (ob/log/...) may be passed in extra.
//
// Without a punctuation mark only an obviously-arg-like remainder starts a
// split (a digit, CJK rune or punctuation — not an ASCII letter), so casual
// words such as "reset" are never mistaken for "r est".
func SplitCommandToken(token string, extra ...string) (name, glued string, prefixed, ok bool) {
	raw, mark := stripCommandPrefix(token)
	if isCommandName(raw) {
		return raw, "", mark, true
	}
	for _, e := range extra {
		if raw == e {
			return raw, "", mark, true
		}
	}
	best := ""
	consider := func(cand string) {
		if cand == raw || !strings.HasPrefix(raw, cand) || len(cand) <= len(best) {
			return
		}
		rest := raw[len(cand):]
		if rest == "" {
			return
		}
		if !mark && isASCIILetter(rest[0]) {
			return
		}
		best = cand
	}
	for c := range commandNames {
		consider(c)
	}
	for _, e := range extra {
		consider(e)
	}
	if best == "" {
		return raw, "", mark, false
	}
	return best, raw[len(best):], mark, true
}

// isASCIILetter reports whether b is an ASCII letter byte.
func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// dispatchCommand runs one recognized command. It never returns
// "unrecognized": the caller decides fall-through.
//
// The rulebook-declared generation aliases (".coc", ".dnd", ...) are
// resolved before this point (see bookCommand in bookcmd.go), so the table
// below only holds the game-independent commands.
func dispatchCommand(ctx context.Context, name string, args []string,
	application *app.App, r *Renderer, stdin io.Reader, stderr io.Writer) int {
	switch name {
	case "help", "帮助":
		_, _ = io.WriteString(r.Out, HelpText)
		return 0
	case "r":
		return cmdRoll(ctx, args, application, r, stderr)
	case "rs":
		return cmdRollCompact(ctx, args, application, r, stderr)
	case "rd":
		return cmdDefaultRoll(ctx, args, application, r, stderr)
	case "ra", "rc":
		// Dice! uses .ra and .rc as aliases for the same check. In D&D mode
		// both run a d20 check through the rulebook's mechanic.
		return cmdCheck(ctx, args, application, r, stderr)
	case "rb", "rp":
		return cmdBonusPenaltyCheck(ctx, name, args, application, r, stderr)
	case "sc":
		return cmdResourceCheck(ctx, "sc", args, application, r, stderr)
	case "ds":
		return cmdDeathSave(ctx, args, application, r, stderr)
	case "st":
		return cmdSheet(ctx, args, application, r, stderr)
	case "pc":
		return cmdPC(ctx, args, application, r, stdin, stderr)
	case "rav":
		return cmdOpposed(ctx, args, application, r, stderr)
	case "tb":
		return cmdTable(ctx, args, application, r, stderr)
	case "rule":
		return cmdRule(ctx, args, application, r, stderr)
	case "game":
		return cmdGame(ctx, args, application, r, stderr)
	case "log":
		return cmdLog(ctx, args, application, r, stderr)
	case "set":
		return cmdSet(ctx, args, application, r, stderr)
	case "ti":
		return cmdRandomTable(ctx, "ti", args, application, r, stderr)
	case "li":
		return cmdRandomTable(ctx, "li", args, application, r, stderr)
	case "ak":
		return cmdChoice(ctx, args, application, r, stderr)
	case "name":
		return cmdName(ctx, args, application, r, stderr)
	case "en":
		return cmdGrowth(ctx, args, application, r, stderr)
	case "w":
		return cmdPool(ctx, args, false, application, r, stderr)
	case "ww":
		return cmdPool(ctx, args, true, application, r, stderr)
	case "ri":
		return cmdInitiative(ctx, args, application, r, stderr)
	case "init":
		return cmdInitiativeList(ctx, args, application, r, stderr)
	case "deck":
		return cmdDeck(ctx, args, application, r, stderr)
	case "draw":
		return cmdDraw(ctx, args, application, r, stderr)
	case "h", "rh":
		return PrintError(stderr, apperr.New("cli.command", apperr.InvalidArgument,
			"暗骰（h/rh）需要聊天平台适配器，CLI 暂不支持"))
	case "rah", "rch":
		return PrintError(stderr, apperr.New("cli.command", apperr.InvalidArgument,
			"暗骰检定（rah/rch）需要聊天平台适配器，CLI 暂不支持"))
	case "drawh":
		return PrintError(stderr, apperr.New("cli.command", apperr.InvalidArgument,
			"暗抽（drawh）需要聊天平台适配器，CLI 暂不支持"))
	default:
		fmt.Fprintf(stderr, "未知指令：%s（可用：%s）\n", name, commandNameList())
		return 2
	}
}

// commandNameList renders the known command names for the error
// message (sorted, so it does not depend on map order).
func commandNameList() string {
	names := make([]string, 0, len(commandNames))
	for name := range commandNames {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, " ")
}

var raSuffixRE = regexp.MustCompile(`^(.+?)([+-]\d+)$`)

// raScaleRE matches a target-arithmetic suffix on the name token:
// "体质*5" (multiply the sheet value) or "心理学/2" (divide it). The
// published `+n`/`-n` suffixes stay bonus/penalty dice, so the two never
// overlap.
var raScaleRE = regexp.MustCompile(`^(.+?)([*/])(\d+)$`)

// raRepeatRE matches a leading "n#" repeat prefix on the first positional
// token (Dice! `.ra 3#心理学`).
var raRepeatRE = regexp.MustCompile(`^(\d+)#(.*)$`)

// raRepeatBPRE matches the Dice! `r3#p`/`r3#b2` shorthand: after the prefix
// a b/p letter (b = bonus, p = penalty) with an optional dice count
// (omitted = 1). Such a token carries no name; the name/target follows.
var raRepeatBPRE = regexp.MustCompile(`^([bp])(\d*)$`)

// stripRepeatPrefix pulls a leading "n#" repeat marker off the first
// positional token. Recognized shapes: "3#心理学" (remainder becomes the
// first token), "3#" standalone, "3#p"/"3#b2" (one bonus/penalty die per
// trial). Tokens without the prefix pass through unchanged. The repeat
// range is validated by the engine.
func stripRepeatPrefix(args []string) (repeat, bonus, penalty int, rest []string, present bool) {
	if len(args) == 0 {
		return 0, 0, 0, args, false
	}
	m := raRepeatRE.FindStringSubmatch(args[0])
	if m == nil {
		return 0, 0, 0, args, false
	}
	repeat, _ = strconv.Atoi(m[1])
	rest = append(rest, args[1:]...)
	if tail := m[2]; tail != "" {
		if bp := raRepeatBPRE.FindStringSubmatch(tail); bp != nil {
			count := 1
			if bp[2] != "" {
				count, _ = strconv.Atoi(bp[2])
			}
			if bp[1] == "b" {
				bonus = count
			} else {
				penalty = count
			}
		} else {
			rest = append([]string{tail}, rest...)
		}
	}
	return repeat, bonus, penalty, rest, true
}

// raSpec is the parsed shape of an `ra` token sequence.
type raSpec struct {
	name     string
	target   int
	explicit bool
	bonus    int
	penalty  int
	// multiply/divide are the target arithmetic parsed from the name
	// token ("体质*5", "心理学/2").
	multiply int
	divide   int
}

// adjust returns the target adjustment described by the name token.
func (s raSpec) adjust() engine.TargetAdjust {
	return engine.TargetAdjust{Multiply: s.multiply, Divide: s.divide}
}

// parseRATokens turns chat tokens into a check specification:
//
//	心理学        skill/attribute name resolved by the app layer
//	60            explicit target value
//	心理学+2      name with inline bonus dice (also accepted as a bare +2)
//	心理学-1      name with inline penalty dice
//	体质*5        name with a target arithmetic (multiply the sheet value)
//	心理学/2      name with a target arithmetic (divide the sheet value)
func parseRATokens(tokens []string) (raSpec, error) {
	var spec raSpec
	for _, tok := range tokens {
		switch {
		// Signed tokens are bonus/penalty modifiers (CC+2 convention);
		// check this before the integer case so "-1" is not read as a
		// negative target (targets start at 0).
		case strings.HasPrefix(tok, "+") || strings.HasPrefix(tok, "-"):
			n, err := strconv.Atoi(tok)
			if err != nil {
				return spec, fmt.Errorf("奖励/惩罚修正必须是整数，得到 %q", tok)
			}
			if n >= 0 {
				spec.bonus += n
			} else {
				spec.penalty += -n
			}
		case isIntegerToken(tok):
			if spec.explicit {
				return spec, fmt.Errorf("只能指定一个目标值")
			}
			n, _ := strconv.Atoi(tok)
			spec.target, spec.explicit = n, true
		default:
			name := tok
			if m := raSuffixRE.FindStringSubmatch(tok); m != nil {
				name = m[1]
				n, _ := strconv.Atoi(m[2])
				if n >= 0 {
					spec.bonus += n
				} else {
					spec.penalty += -n
				}
			}
			if m := raScaleRE.FindStringSubmatch(name); m != nil {
				n, _ := strconv.Atoi(m[3])
				if n < 1 {
					return spec, fmt.Errorf("目标值算式的系数必须为正整数，得到 %q", tok)
				}
				switch m[2] {
				case "*":
					spec.multiply = n
				case "/":
					spec.divide = n
				}
				name = m[1]
			}
			if name == "" {
				return spec, fmt.Errorf("技能/属性名不能为空")
			}
			// A numeric core is an explicit target ("60+2" = target 60
			// with two bonus dice), not a skill name.
			if isIntegerToken(name) {
				if spec.explicit {
					return spec, fmt.Errorf("只能指定一个目标值")
				}
				n, _ := strconv.Atoi(name)
				spec.target, spec.explicit = n, true
				continue
			}
			if spec.name != "" {
				return spec, fmt.Errorf("只能指定一个技能/属性名")
			}
			spec.name = name
		}
	}
	return spec, nil
}

func isIntegerToken(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.Atoi(s)
	return err == nil
}

// stripDifficulty pulls an explicit difficulty keyword off the first
// positional token: a standalone keyword ("ra 困难 心理学 60") or a prefix
// ("ra 困难心理学"). Longer keywords win prefix matching; the keyword set is
// rulebook data, never hard-coded. Returns the canonical id and remaining
// tokens ("" / args unchanged when no keyword leads).
func stripDifficulty(args []string, diffs []engine.DifficultyInfo) (string, []string) {
	if len(args) == 0 || len(diffs) == 0 {
		return "", args
	}
	byName := make(map[string]engine.DifficultyInfo, len(diffs))
	byID := make(map[string]engine.DifficultyInfo, len(diffs))
	names := make([]string, 0, len(diffs))
	for _, d := range diffs {
		byName[d.Name] = d
		byID[strings.ToLower(d.ID)] = d
		names = append(names, d.Name)
	}
	first := args[0]
	if d, ok := byID[strings.ToLower(first)]; ok {
		return d.ID, args[1:]
	}
	if d, ok := byName[first]; ok {
		return d.ID, args[1:]
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	for _, n := range names {
		if len(first) > len(n) && strings.HasPrefix(first, n) {
			rest := make([]string, 0, len(args))
			rest = append(rest, first[len(n):])
			rest = append(rest, args[1:]...)
			return byName[n].ID, rest
		}
	}
	return "", args
}

func cmdRoll(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	if len(args) == 0 {
		// A bare `r` rolls the default die (d100 unless configured).
		sides, err := a.DefaultDieSides(ctx, currentCharacter(ctx))
		if err != nil {
			return PrintError(stderr, err)
		}
		res, err := a.RollStatement(ctx, fmt.Sprintf("d%d", sides))
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.RollBatch(res, ""); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}
	expr, err := a.ExpandDamageBonus(ctx, args[0], currentCharacter(ctx))
	if err != nil {
		return PrintError(stderr, err)
	}
	res, err := a.RollStatement(ctx, expr)
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.RollBatch(res, strings.Join(args[1:], " ")); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// cmdRollCompact is the compact roll alias: only the total is rendered, the JSON
// contract stays the same and just marks the result as not detailed.
func cmdRollCompact(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "用法：rs <掷骰语句> [原因文本...]")
		return 2
	}
	expr, err := a.ExpandDamageBonus(ctx, args[0], currentCharacter(ctx))
	if err != nil {
		return PrintError(stderr, err)
	}
	res, err := a.RollStatement(ctx, expr)
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.RollBatchCompact(res, strings.Join(args[1:], " ")); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// cmdDefaultRoll is Dice! `.rd`: roll the personal default die, or the die
// named by an optional face count (`.rd6`).
func cmdDefaultRoll(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	if len(args) == 0 {
		return cmdRoll(ctx, nil, a, r, stderr)
	}
	if len(args) != 1 {
		fmt.Fprintln(stderr, "用法：rd [面数]")
		return 2
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 {
		fmt.Fprintf(stderr, "默认骰面数必须是正整数，得到 %q\n", args[0])
		return 2
	}
	return cmdRoll(ctx, []string{fmt.Sprintf("d%d", n)}, a, r, stderr)
}

// cmdSet is Dice!'s shared settings command: a number sets the default die,
// `list` lists rulebooks and any other single token switches the current
// game's default rulebook.
func cmdSet(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	sides := 0
	switch len(args) {
	case 0:
		// Restore the built-in default.
	case 1:
		if args[0] == "list" {
			if err := r.Systems(a.Registry().List()); err != nil {
				return PrintError(stderr, err)
			}
			return 0
		}
		n, err := strconv.Atoi(args[0])
		if err != nil {
			view, serr := a.SetGameSystem(ctx, args[0])
			if serr != nil {
				return PrintError(stderr, serr)
			}
			if err := r.Session(view); err != nil {
				return PrintError(stderr, err)
			}
			return 0
		}
		if n < app.MinDefaultDieSides || n > app.MaxDefaultDieSides {
			fmt.Fprintf(stderr, "默认骰面数必须在 %d–%d 之间\n", app.MinDefaultDieSides, app.MaxDefaultDieSides)
			return 2
		}
		sides = n
	default:
		fmt.Fprintln(stderr, "用法：set [面数]｜set list｜set <规则书>（省略面数恢复默认骰）")
		return 2
	}
	if err := a.SetDefaultDieSides(ctx, sides); err != nil {
		return PrintError(stderr, err)
	}
	current, err := a.DefaultDieSides(ctx, currentCharacter(ctx))
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.SimpleMessage(fmt.Sprintf("默认骰面数：d%d", current)); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

func cmdCheck(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("ra", flag.ContinueOnError)
	fs.SetOutput(stderr)
	system := fs.String("system", "", "规则书 ID")
	characterID := fs.String("character", "", "角色名或 ID（默认当前角色）")
	mod := fs.Int("mod", 0, "目标值修正（整数，加在算式之后）")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	explicit := *characterID != ""
	useDefaultCharacter(ctx, characterID)
	tgt, err := flagTarget(ctx, a, *characterID, explicit)
	if err != nil {
		return PrintError(stderr, err)
	}
	*characterID = tgt.CardID
	ctx = withTarget(ctx, tgt)
	positionals := fs.Args()
	// Difficulty keywords are rulebook data (困难/极难/自动成功), so the
	// book must be resolvable before token parsing.
	diffs, err := a.Difficulties(ctx, *system, *characterID)
	if err != nil {
		return PrintError(stderr, err)
	}
	diffID := ""
	repeat, repBonus, repPenalty, positionals, repeatPresent := stripRepeatPrefix(positionals)
	if repeatPresent {
		// 3#困难心理学: the marker leads, the keyword stays on the token.
		diffID, positionals = stripDifficulty(positionals, diffs)
	} else if id, after := stripDifficulty(positionals, diffs); id != "" {
		// 困难 3#心理学: a standalone keyword may lead, then the marker.
		diffID, positionals = id, after
		repeat, repBonus, repPenalty, positionals, repeatPresent = stripRepeatPrefix(positionals)
	}
	if repeatPresent && repeat < 1 {
		fmt.Fprintln(stderr, "用法：连掷轮数 n# 必须 ≥1（上限 9）")
		return 2
	}
	spec, err := parseRATokens(positionals)
	if err != nil {
		fmt.Fprintf(stderr, "用法：ra [难度] [n#] [技能/属性名] [目标值] [+奖励/-惩罚]（%v）\n", err)
		return 2
	}
	if !spec.explicit && spec.name == "" {
		fmt.Fprintln(stderr, "用法：ra [难度] [n#] [技能/属性名] [目标值] [+奖励/-惩罚]")
		return 2
	}
	if spec.explicit && (spec.multiply > 0 || spec.divide > 0) {
		fmt.Fprintln(stderr, "不能同时指定显式目标值与目标值算式（*n / n）")
		return 2
	}

	adjust := spec.adjust()
	adjust.Add += *mod

	target := app.CheckTarget{
		SystemID:    *system,
		CharacterID: *characterID,
		Bonus:       spec.bonus + repBonus,
		Penalty:     spec.penalty + repPenalty,
		Difficulty:  diffID,
		Adjust:      adjust,
		Repeat:      repeat,
	}
	if spec.explicit {
		// An explicit value runs the check against that number; the name,
		// when given, only labels the roll and must still resolve.
		target.Value, target.Explicit = spec.target, true
		if spec.name != "" {
			if _, _, _, rerr := a.ResolveCheckName(ctx, *system, *characterID, spec.name); rerr != nil {
				return PrintError(stderr, rerr)
			}
			target.Skill = spec.name
		}
	} else {
		skillID, attrID, resourceID, rerr := a.ResolveCheckName(ctx, *system, *characterID, spec.name)
		if rerr != nil {
			return PrintError(stderr, rerr)
		}
		target.Skill, target.Attr, target.Resource = skillID, attrID, resourceID
	}

	if target.Repeat > 1 {
		batch, err := a.RunCheckBatch(ctx, target)
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.CheckBatch(batch); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}
	res, err := a.RunCheck(ctx, target)
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.Check(res); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// cmdBonusPenaltyCheck implements Dice! `.rb`/`.rp`: a bonus or penalty
// die applies to the following check. A leading small count is accepted
// (`.rp2 60`, `.rb 2 侦查`); a lone integer remains the check target so
// `.rb 60` means one bonus die against 60.
func cmdBonusPenaltyCheck(ctx context.Context, name string, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "用法：%s [数量] <检定参数...>\n", name)
		return 2
	}
	count := 1
	checkArgs := args
	if n, err := strconv.Atoi(args[0]); err == nil && len(args) > 1 && n >= 1 && n <= 9 {
		count = n
		checkArgs = args[1:]
	}
	mod := fmt.Sprintf("+%d", count)
	if name == "rp" {
		mod = fmt.Sprintf("-%d", count)
	}
	return cmdCheck(ctx, append(checkArgs, mod), a, r, stderr)
}

// parseLossPair splits "success/failure" loss specs (e.g. "0/1d10").
func parseLossPair(s string) (success, failure string, err error) {
	parts := strings.SplitN(s, "/", 3)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("损失规格必须形如 成功损失/失败损失，例如 0/1d10，得到 %q", s)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

// parseCurrentAndReason splits the trailing positional arguments of a
// resource check into an optional explicit current value and the reason
// text. A bare integer is read as the current value (the Dice! `.sc 0/1 70`
// convention); everything else is reason text.
func parseCurrentAndReason(args []string) (current *int, reason string) {
	if len(args) > 0 {
		if n, err := strconv.Atoi(args[0]); err == nil && n >= 0 {
			current = &n
			args = args[1:]
		}
	}
	return current, strings.TrimSpace(strings.Join(args, " "))
}

func cmdResourceCheck(ctx context.Context, verb string, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet(verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	system := fs.String("system", "", "规则书 ID（默认角色所属）")
	characterID := fs.String("character", "", "角色名或 ID（默认当前角色）")
	apply := fs.Bool("apply", false, "将资源变化写回角色卡")
	bonus := fs.Int("bonus", 0, "奖励骰数量")
	penalty := fs.Int("penalty", 0, "惩罚骰数量")
	current := fs.Int("current", -1, "显式指定当前值（仅本次检定，不写轨）")
	if err := parseSub(fs, args); err != nil {
		return 2
	}

	if verb != "sc" {
		fmt.Fprintln(stderr, "用法：sc <成功损失>/<失败损失> [当前值] [原因文本...] [--apply]")
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(stderr, "用法：sc <成功损失>/<失败损失> [当前值] [原因文本...] [--apply]")
		return 2
	}
	pair := fs.Arg(0)
	rest := fs.Args()[1:]
	success, failure, err := parseLossPair(pair)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	override, reason := parseCurrentAndReason(rest)
	if *current >= 0 {
		if override != nil {
			fmt.Fprintln(stderr, "当前值只能指定一次")
			return 2
		}
		override = current
	}
	explicit := *characterID != ""
	useDefaultCharacter(ctx, characterID)
	tgt, err := flagTarget(ctx, a, *characterID, explicit)
	if err != nil {
		return PrintError(stderr, err)
	}
	*characterID = tgt.CardID
	ctx = withTarget(ctx, tgt)
	// The check the verb addresses is rulebook data (command_bindings),
	// never a hard-coded id.
	checkID, err := a.ResourceCheckBinding(ctx, *system, *characterID, verb)
	if err != nil {
		return PrintError(stderr, err)
	}

	applied := *apply || autoApplyFromContext(ctx)
	res, err := a.RunResourceCheck(ctx, app.ResourceOptions{
		SystemID:        *system,
		CharacterID:     *characterID,
		CheckID:         checkID,
		SuccessLoss:     success,
		FailureLoss:     failure,
		Apply:           applied,
		Bonus:           *bonus,
		Penalty:         *penalty,
		CurrentOverride: override,
	})
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.ResourceCheck(res, applied, reason); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// cmdSheet handles the sheet commands: show, del, clr, assignments,
// adjustments and macro definitions.
func cmdSheet(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	if len(args) == 0 {
		c, err := a.GetCharacter(ctx, currentCharacter(ctx))
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.Character(c); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}

	switch args[0] {
	case "show":
		switch len(args) {
		case 1:
			c, err := a.GetCharacter(ctx, currentCharacter(ctx))
			if err != nil {
				return PrintError(stderr, err)
			}
			if err := r.Character(c); err != nil {
				return PrintError(stderr, err)
			}
			return 0
		case 2:
			v, err := a.ShowValue(ctx, currentCharacter(ctx), args[1])
			if err != nil {
				return PrintError(stderr, err)
			}
			if err := r.Value(v); err != nil {
				return PrintError(stderr, err)
			}
			return 0
		default:
			fmt.Fprintln(stderr, "用法：st show｜st show <键>")
			return 2
		}
	case "del":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "用法：st del <属性/技能名>｜st del &宏名")
			return 2
		}
		if strings.HasPrefix(args[1], app.MacroPrefix) {
			c, err := a.DeleteMacro(ctx, currentCharacter(ctx), strings.TrimPrefix(args[1], app.MacroPrefix))
			if err != nil {
				return PrintError(stderr, err)
			}
			if err := r.Character(c); err != nil {
				return PrintError(stderr, err)
			}
			return 0
		}
		c, err := a.DeleteValue(ctx, currentCharacter(ctx), args[1])
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.Character(c); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "clr":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "用法：st clr")
			return 2
		}
		c, err := a.ClearValues(ctx, currentCharacter(ctx))
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.Character(c); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "derived":
		// Local extension under Dice!'s .st namespace; Dice!Next recalculates
		// derived values automatically, while dicegirl still exposes the
		// rulebook's computed sheet on demand.
		return cmdDerived(ctx, args[1:], a, r, stderr)
	case "alloc":
		// Local extension under Dice!'s .st namespace: the creation
		// skill-point pools (Dice! leaves skill distribution to manual st
		// assignments; dicegirl tracks the pools the rulebook declares).
		return cmdSkillAlloc(ctx, args[1:], a, r, stderr)
	}

	// Macro definitions: "&名=表达式" (Dice! `.st &沙漠之鹰=1D10+1D6+3`).
	if strings.HasPrefix(args[0], app.MacroPrefix) {
		return cmdSetMacro(ctx, args, a, r, stderr)
	}

	setTokens, hasSet, hasAdjust := splitSheetAssignments(args)
	if hasSet && hasAdjust {
		fmt.Fprintln(stderr, "不能在同一条 st 中混用赋值与 ± 增减")
		return 2
	}

	if hasSet {
		sets, err := parseAssignments(setTokens)
		if err != nil {
			return PrintError(stderr, err)
		}
		c, err := a.SetValues(ctx, currentCharacter(ctx), sets)
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.Character(c); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}

	adjs := make([]app.Adjustment, 0, len(args))
	for _, tok := range args {
		adj, err := app.ParseAdjustment(tok)
		if err != nil {
			return PrintError(stderr, err)
		}
		adjs = append(adjs, adj)
	}
	_, results, err := a.AdjustValues(ctx, currentCharacter(ctx), adjs)
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.Adjustments(results); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// cmdSetMacro saves a named dice expression on the current card.
func cmdSetMacro(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "用法：st &名=表达式")
		return 2
	}
	tok := args[0]
	idx := strings.IndexAny(tok, "=:")
	if idx <= 0 || idx == len(tok)-1 {
		fmt.Fprintln(stderr, "用法：st &名=表达式")
		return 2
	}
	name := strings.TrimSpace(strings.TrimPrefix(tok[:idx], app.MacroPrefix))
	expr := strings.TrimSpace(tok[idx+1:])
	c, err := a.SetMacro(ctx, currentCharacter(ctx), name, expr)
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.Character(c); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// cmdRandomTable rolls the table a chat verb (ti/li) is bound to in the
// applicable rulebook (command_bindings; the Dice! 疯狂表 aliases).
func cmdRandomTable(ctx context.Context, verb string, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("table", flag.ContinueOnError)
	fs.SetOutput(stderr)
	system := fs.String("system", "", "规则书 ID（默认角色所属）")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	tableID, err := a.TableBinding(ctx, *system, verb)
	if err != nil {
		return PrintError(stderr, err)
	}
	res, err := a.RollTable(ctx, *system, tableID)
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.Table(res); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// cmdChoice manages one round of random choices (Dice! `.ak`).
func cmdChoice(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "用法：ak new｜ak add <选项...>｜ak del <选项>｜ak get｜ak show｜ak clr")
		return 2
	}
	switch args[0] {
	case "new", "clr":
		if len(args) != 1 {
			fmt.Fprintf(stderr, "用法：ak %s\n", args[0])
			return 2
		}
		if err := a.ClearChoiceOptions(ctx); err != nil {
			return PrintError(stderr, err)
		}
		if err := r.SimpleMessage("已清空待选选项"); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "add":
		if len(args) < 2 {
			fmt.Fprintln(stderr, "用法：ak add <选项...>")
			return 2
		}
		options, err := a.AddChoiceOptions(ctx, args[1:])
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.ChoiceOptions(options); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "del":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "用法：ak del <选项>")
			return 2
		}
		options, err := a.DeleteChoiceOption(ctx, args[1])
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.ChoiceOptions(options); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "get":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "用法：ak get")
			return 2
		}
		res, err := a.DrawChoice(ctx)
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.Choice(res); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "show":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "用法：ak show")
			return 2
		}
		options, err := a.ChoiceOptions(ctx)
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.ChoiceOptions(options); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	default:
		fmt.Fprintln(stderr, "用法：ak new｜ak add <选项...>｜ak del <选项>｜ak get｜ak show｜ak clr")
		return 2
	}
}

// cmdName generates random names (Dice! `.name`).
func cmdName(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	setID := ""
	count := 1
	for _, tok := range args {
		if isIntegerToken(tok) {
			n, _ := strconv.Atoi(tok)
			if n < 1 {
				fmt.Fprintln(stderr, "姓名数量必须是正整数")
				return 2
			}
			count = n
			continue
		}
		if setID != "" {
			fmt.Fprintln(stderr, "只能指定一个姓名集（cn/jp/en）")
			return 2
		}
		setID = tok
	}
	res, err := a.RandomNames(ctx, setID, count)
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.Names(res); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// cmdGrowth runs a skill-growth check (Dice! `.en`).
func cmdGrowth(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("en", flag.ContinueOnError)
	fs.SetOutput(stderr)
	system := fs.String("system", "", "规则书 ID（默认角色所属）")
	characterID := fs.String("character", "", "角色名或 ID（默认当前角色）")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	explicit := *characterID != ""
	useDefaultCharacter(ctx, characterID)
	tgt, err := flagTarget(ctx, a, *characterID, explicit)
	if err != nil {
		return PrintError(stderr, err)
	}
	*characterID = tgt.CardID
	ctx = withTarget(ctx, tgt)
	positional := fs.Args()
	if len(positional) < 1 {
		fmt.Fprintln(stderr, "用法：en <技能/属性名> [目标值] [原因文本...]")
		return 2
	}
	name := positional[0]
	rest := positional[1:]
	var target *int
	var gain string
	if len(rest) > 0 {
		switch tok := rest[0]; {
		case isIntegerToken(tok):
			// A plain number is the value the d100 is compared against.
			n, _ := strconv.Atoi(tok)
			target = &n
			rest = rest[1:]
		case looksLikeDiceExpression(tok):
			// A dice expression is a custom growth amount (the Dice!
			// `.en 幸运 +1D3/1D10` form).
			gain = tok
			rest = rest[1:]
		}
	}
	reason := strings.TrimSpace(strings.Join(rest, " "))
	res, _, err := a.RunGrowth(ctx, app.GrowthOptions{
		SystemID:    *system,
		CharacterID: *characterID,
		Name:        name,
		Target:      target,
		Gain:        gain,
		Reason:      reason,
	})
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.Growth(res); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// poolSuccessThreshold is the W-Wing success threshold of the `.w`/`.ww`
// alias: a die counts as a success from 8 on. The alias is a thin wrapper
// around the generic dice operators (explode + count).
const poolSuccessThreshold = 8

// cmdPool rolls a dice pool (Dice! `.w`/`.ww`): `[骰子个数]a[加骰参数]`.
// The add parameter is the explode threshold (every die reaching it adds
// another die); the result counts the dice that reached the success
// threshold. detailed renders every die instead of the total.
func cmdPool(ctx context.Context, args []string, detailed bool,
	a *app.App, r *Renderer, stderr io.Writer) int {
	countExpr, add, err := parsePoolArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	count, err := a.ResolveCount(ctx, countExpr)
	if err != nil {
		return PrintError(stderr, err)
	}
	expr := fmt.Sprintf("%dd10", count)
	if add > 0 {
		expr += fmt.Sprintf("!%d", add)
	}
	expr += fmt.Sprintf("c%d", poolSuccessThreshold)
	res, err := a.RollStatement(ctx, expr)
	if err != nil {
		return PrintError(stderr, err)
	}
	if detailed {
		if err := r.RollBatch(res, ""); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}
	if err := r.RollBatchCompact(res, ""); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// parsePoolArgs parses the pool arguments: an optional dice count and an
// optional "a<threshold>" add parameter ("5a8", "1d4+1a8", "5 a8").
func parsePoolArgs(args []string) (countExpr string, add int, err error) {
	text := strings.Join(args, "")
	text = strings.TrimSpace(text)
	if text == "" {
		return "1", 0, nil
	}
	// The add parameter is the part after the last "a".
	if i := strings.LastIndex(text, "a"); i >= 0 {
		threshold := text[i+1:]
		text = text[:i]
		n, cerr := strconv.Atoi(threshold)
		if cerr != nil || n < 1 || n > 10 {
			return "", 0, fmt.Errorf("加骰参数必须是 1–10 的整数")
		}
		add = n
	}
	if strings.TrimSpace(text) == "" {
		return "", 0, fmt.Errorf("骰子个数不能为空")
	}
	return strings.TrimSpace(text), add, nil
}

// cmdInitiative rolls an initiative value and adds it to the current
// encounter (Dice! `.ri [±加值|表达式] [名称]`).
func cmdInitiative(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	opts, err := app.ParseInitiativeArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	opts.Expr = normalizeInitiativeExpr(opts.Expr)
	list, err := a.RollInitiative(ctx, opts)
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.Initiative(list); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// normalizeInitiativeExpr maps the D&D shorthand ("2DK") to our dice
// syntax ("2d20kh1"); our own expressions pass through unchanged.
func normalizeInitiativeExpr(expr string) string {
	trimmed := strings.TrimSpace(expr)
	if m := initiativeDKRE.FindStringSubmatch(trimmed); m != nil {
		return m[1] + "d20kh1"
	}
	return trimmed
}

// initiativeDKRE matches the "2DK" shorthand (roll two, keep the highest).
var initiativeDKRE = regexp.MustCompile(`(?i)^(\d+)DK$`)

// cmdInitiativeList shows or clears the initiative list (Dice! `.init`,
// `.init clr`).
func cmdInitiativeList(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	switch {
	case len(args) == 0 || args[0] == "list":
		if len(args) > 1 {
			fmt.Fprintln(stderr, "用法：init｜init list｜init clr")
			return 2
		}
		list, err := a.ListInitiative(ctx)
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.Initiative(list); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case len(args) == 1 && args[0] == "clr":
		if err := a.ClearInitiative(ctx); err != nil {
			return PrintError(stderr, err)
		}
		if err := r.SimpleMessage("已清空先攻列表"); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	default:
		fmt.Fprintln(stderr, "用法：init｜init list｜init clr")
		return 2
	}
}

// cmdDeck manages the card decks (Dice! `.deck`).
func cmdDeck(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "用法：deck new <名>=<卡面1|卡面2>｜deck set <实例>=<牌堆|range 下限 上限>｜deck show [名]｜deck put <名> <卡面...>｜deck reset <名>｜deck clr")
		return 2
	}
	switch args[0] {
	case "new":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "用法：deck new <名>=<卡面1|卡面2>")
			return 2
		}
		name, spec, ok := strings.Cut(args[1], "=")
		if !ok {
			fmt.Fprintln(stderr, "用法：deck new <名>=<卡面1|卡面2>")
			return 2
		}
		deck, err := a.NewDeck(ctx, name, spec)
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.Deck(deck); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "set":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "用法：deck set <实例>=<牌堆名>｜deck set <实例>=range <下限> <上限>")
			return 2
		}
		name, source, ok := strings.Cut(args[1], "=")
		if !ok {
			fmt.Fprintln(stderr, "用法：deck set <实例>=<牌堆名>")
			return 2
		}
		deck, err := a.SetDeck(ctx, name, source)
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.Deck(deck); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "show":
		if len(args) > 2 {
			fmt.Fprintln(stderr, "用法：deck show [名]")
			return 2
		}
		if len(args) == 1 {
			list, err := a.ShowDecks(ctx)
			if err != nil {
				return PrintError(stderr, err)
			}
			if err := r.DeckList(list); err != nil {
				return PrintError(stderr, err)
			}
			return 0
		}
		deck, err := a.ShowDeck(ctx, args[1])
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.Deck(deck); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "put", "return", "back":
		if len(args) < 3 {
			fmt.Fprintln(stderr, "用法：deck put <名> <卡面...>（把弃牌堆里的卡放回并重洗）")
			return 2
		}
		deck, err := a.ReturnCards(ctx, args[1], args[2:])
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.Deck(deck); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "reset":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "用法：deck reset <名>")
			return 2
		}
		deck, err := a.ResetDeck(ctx, args[1])
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.Deck(deck); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "clr", "clear":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "用法：deck clr")
			return 2
		}
		if err := a.ClearDecks(ctx); err != nil {
			return PrintError(stderr, err)
		}
		if err := r.SimpleMessage("已清空牌堆"); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	default:
		fmt.Fprintln(stderr, "用法：deck new｜deck set｜deck show｜deck put｜deck reset｜deck clr")
		return 2
	}
}

// cmdDraw draws cards from a deck (Dice! `.draw`). Without replacement the
// cards move to the discard pile; a trailing "放回" draws with replacement.
func cmdDraw(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "用法：draw <牌堆> [数量] [放回]")
		return 2
	}
	count := 1
	replace := false
	for _, tok := range args[1:] {
		switch {
		case isIntegerToken(tok):
			if count != 1 {
				fmt.Fprintln(stderr, "用法：draw <牌堆> [数量] [放回]")
				return 2
			}
			n, _ := strconv.Atoi(tok)
			if n < 1 {
				fmt.Fprintln(stderr, "抽牌数量必须是正整数")
				return 2
			}
			count = n
		case tok == "放回" || tok == "放回抽" || tok == "back" || tok == "replace":
			replace = true
		default:
			fmt.Fprintln(stderr, "用法：draw <牌堆> [数量] [放回]")
			return 2
		}
	}
	res, err := a.DrawCards(ctx, args[0], app.DrawOptions{Count: count, Replace: replace})
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.Draw(res); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// cmdCardStats shows the roll statistics of a card (Dice! `.pc stat`).
func cmdCardStats(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "clr" {
		if len(args) > 2 {
			fmt.Fprintln(stderr, "用法：pc stat clr [名字|id]")
			return 2
		}
		ref := ""
		if len(args) == 2 {
			ref = args[1]
		}
		id, err := cardRef(ctx, a, defaultRef(ctx, ref))
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := a.ClearCardStats(ctx, id); err != nil {
			return PrintError(stderr, err)
		}
		if err := r.SimpleMessage("已清空该卡的掷骰统计"); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}
	if len(args) > 1 {
		fmt.Fprintln(stderr, "用法：pc stat [名字|id]｜pc stat clr [名字|id]")
		return 2
	}
	ref := ""
	if len(args) == 1 {
		ref = args[0]
	}
	id, err := cardRef(ctx, a, ref)
	if err != nil {
		return PrintError(stderr, err)
	}
	stats, err := a.CardStats(ctx, id)
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.CardStats(stats); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// looksLikeDiceExpression reports whether a token is a dice expression
// (and not a plain number or a name).
func looksLikeDiceExpression(tok string) bool {
	return strings.ContainsAny(tok, "dD/")
}

// cmdRules searches the quick-reference dictionary (Dice!'s rules lookup),
// exposed here as `rule ref`.
//
//	rule ref <词条>        检索速查词条（可写 "系统:词条"）
//	rule ref list          列出所有词条
//	rule ref set <系统>    设定优先查询的规则书
//	rule ref set           清除偏好
func cmdRules(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "用法：rule ref <词条>｜rule ref list｜rule ref set <系统>｜rule ref set")
		return 2
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "用法：rule ref list")
			return 2
		}
		entries, err := a.RuleEntries(ctx)
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.RuleEntries(entries); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "set":
		if len(args) > 2 {
			fmt.Fprintln(stderr, "用法：rule ref set <系统>｜rule ref set")
			return 2
		}
		system := ""
		if len(args) == 2 {
			system = args[1]
		}
		if err := a.SetRulesSystem(ctx, system); err != nil {
			return PrintError(stderr, err)
		}
		if system == "" {
			if err := r.SimpleMessage("已清除规则书偏好"); err != nil {
				return PrintError(stderr, err)
			}
			return 0
		}
		if err := r.SimpleMessage("规则书偏好已设为 " + system); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	default:
		query := strings.Join(args, " ")
		res, err := a.SearchRules(ctx, query)
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.Rules(res); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}
}

// cmdGenerate is the body of the rulebook-declared generation commands:
// an optional "d" token selects detailed (point-buy) creation, a bare
// number is the count, and flags may appear anywhere. The system and the
// variant come from the rulebook that owns the alias.
func cmdGenerate(ctx context.Context, command, usage, systemDefault, variant string, detailed bool,
	args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	system := fs.String("system", systemDefault, "规则书 ID")
	name := fs.String("name", "", "角色名（省略则随机姓名）")
	active := fs.Bool("active", true, "把第一张生成的角色设为当前角色（--active=false 关闭）")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	count := 1
	for _, tok := range fs.Args() {
		if tok == "d" {
			detailed = true
			continue
		}
		if !isIntegerToken(tok) {
			fmt.Fprintf(stderr, "无法识别的参数：%q（用法：%s）\n", tok, usage)
			return 2
		}
		n, _ := strconv.Atoi(tok)
		if n < 1 {
			fmt.Fprintln(stderr, "生成数量必须是正整数")
			return 2
		}
		count = n
	}
	if detailed && count > 1 {
		fmt.Fprintln(stderr, "详细作成一次只能生成一个人物")
		return 2
	}
	res, err := a.GenerateCharacters(ctx, app.GenerateOptions{
		SystemID:   *system,
		VariantID:  variant,
		Count:      count,
		Detailed:   detailed,
		Name:       *name,
		MakeActive: *active,
	})
	if err != nil {
		return PrintError(stderr, err)
	}
	if *active && len(res.Characters) > 0 {
		notifyCardCreated(ctx, res.Characters[0].ID, res.Characters[0].Name)
	}
	if err := r.Generation(res); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

func cmdPC(ctx context.Context, args []string, a *app.App, r *Renderer, stdin io.Reader, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, pcUsage)
		return 2
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "用法：pc list")
			return 2
		}
		chars, err := a.ListCharacters(ctx)
		if err != nil {
			return PrintError(stderr, err)
		}
		active, err := a.ActiveCharacterID(ctx)
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.CharacterSummary(active, chars); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "new":
		return cmdPCNew(ctx, args[1:], a, r, stderr)
	case "show":
		if len(args) > 2 {
			fmt.Fprintln(stderr, "用法：pc show [名字|id]")
			return 2
		}
		ref := ""
		if len(args) == 2 {
			ref = args[1]
		}
		id, err := cardRef(ctx, a, defaultRef(ctx, ref))
		if err != nil {
			return PrintError(stderr, err)
		}
		c, err := a.GetCharacter(ctx, id)
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.Character(c); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "nn":
		if len(args) < 2 || len(args) > 3 {
			fmt.Fprintln(stderr, "用法：pc nn [名字|id] <新名>（省略则改当前角色）")
			return 2
		}
		id, name := "", args[1]
		if len(args) == 3 {
			resolved, err := cardRef(ctx, a, args[1])
			if err != nil {
				return PrintError(stderr, err)
			}
			id, name = resolved, args[2]
		}
		c, err := a.RenameCharacter(ctx, id, name)
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.Character(c); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "use":
		return cmdPCUse(ctx, args[1:], a, r, stderr)
	case "active":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "用法：pc active")
			return 2
		}
		return cmdPCActive(ctx, a, r, stderr)
	case "del":
		return cmdPCDelete(ctx, args[1:], a, r, stderr)
	case "val":
		return cmdPCValidate(ctx, args[1:], a, r, stderr)
	case "copy":
		return cmdPCCopy(ctx, args[1:], a, r, stderr)
	case "cpy":
		return cmdPCCopyOnto(ctx, args[1:], a, r, stderr)
	case "export":
		return cmdPCExport(ctx, args[1:], a, r, stderr)
	case "import":
		return cmdPCImport(ctx, args[1:], a, r, stdin, stderr)
	case "meta":
		return cmdPCMeta(ctx, args[1:], a, r, stderr)
	case "redo", "build":
		return cmdPCRebuild(ctx, args, a, r, stderr)
	case "stat":
		return cmdCardStats(ctx, args[1:], a, r, stderr)
	case "clr":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "用法：pc clr（销毁全部角色卡）")
			return 2
		}
		n, err := a.ClearCharacters(ctx)
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.SimpleMessage(fmt.Sprintf("已删除 %d 张角色卡", n)); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	default:
		fmt.Fprintln(stderr, pcUsage)
		return 2
	}
}

// pcUsage is the card-management help line.
const pcUsage = "用法：pc list｜pc new <名> [K=V...] [--active=false]｜pc show [名字|id]｜pc use <名字|id>｜pc active｜pc nn [名字|id] <新名>｜pc copy <名字|id> <新名>｜pc cpy <目标>=<来源>｜pc del <名字|id>｜pc val [名字|id]｜pc meta <键> [值]｜pc export [名字|id] [--file 路径]｜pc import --file <路径|->｜pc redo [名字|id]｜pc build [名字|id]｜pc stat [名字|id]｜pc clr"

// cmdPCRebuild regenerates a card's attributes (`.pc redo` / `.pc build`).
// The card is addressed by id (the active one by default) and the
// generation variant can be selected with --variant.
func cmdPCRebuild(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("pc redo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	variant := fs.String("variant", "", "生成变体 id（如 6、7；省略用规则书默认）")
	if err := parseSub(fs, args[1:]); err != nil {
		return 2
	}
	id := ""
	if fs.NArg() > 0 {
		id = fs.Arg(0)
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "用法：pc redo [名字|id] [--variant <变体 id>]")
		return 2
	}
	resolved, err := cardRef(ctx, a, defaultRef(ctx, id))
	if err != nil {
		return PrintError(stderr, err)
	}
	sheet, c, err := a.RedoCharacter(ctx, resolved, *variant)
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.GeneratedSheet(sheet, c); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}
