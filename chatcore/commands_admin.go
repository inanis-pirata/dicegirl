package chatcore

// Rulebook/data-pack inspection, card management that is not part of the
// chat flow, and the derived/damage/opposed/table helpers: every command the
// CLI and the adapters share goes through this one chat-style surface.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"dicegirl/internal/app"
	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
	"dicegirl/internal/engine"
)

// cmdGame manages games (Dice!Next `.game`): the table itself plus the game
// affairs attached to it — GM/player roster, variables, open/closed state and
// the roulette die.
//
//	game                     show the current game (help when none exists)
//	game list
//	game show/state [名字|id]
//	game new [名字] [--system <规则书>]
//	game join / game exit
//	game master
//	game set <变量> [值]
//	game call
//	game kick <QQ|名字>
//	game close / game open
//	game over
//	game rou <面数>[*份数] / hist / reset / clr
//	game use <名字|id>
//	game system <规则书>
//	game del <名字|id>
func cmdGame(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	const usage = "用法：game｜game list｜game show/state [名字|id]｜game new [名字] [--system <规则书>]｜game join｜game exit｜game master｜game set <变量> [值]｜game call｜game kick <QQ|名字>｜game close｜game open｜game over｜game rou <面数>[*份数]｜game rou hist/reset/clr｜game use <名字|id>｜game system <规则书>｜game del <名字|id>"
	fs := flag.NewFlagSet("game", flag.ContinueOnError)
	fs.SetOutput(stderr)
	system := fs.String("system", "", "规则书 ID（默认角色所属）")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	actorID, actorName, _ := currentActor(ctx)
	renderView := func(view *app.TableView) int {
		if err := r.Session(view); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}
	sub := "show"
	if fs.NArg() > 0 {
		sub = fs.Arg(0)
	}
	switch sub {
	case "show", "state":
		if fs.NArg() > 2 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		ref := ""
		if fs.NArg() == 2 {
			ref = fs.Arg(1)
		}
		view, err := a.GameView(ctx, ref)
		if err != nil {
			return PrintError(stderr, err)
		}
		return renderView(view)
	case "list":
		if fs.NArg() > 1 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		list, err := a.ListGames(ctx)
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.SessionList(list); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "new":
		name := strings.TrimSpace(strings.Join(fs.Args()[1:], " "))
		view, err := a.GameNew(ctx, name, *system, actorID, actorName)
		if err != nil {
			return PrintError(stderr, err)
		}
		return renderView(view)
	case "join":
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		view, err := a.GameJoin(ctx, actorID, actorName)
		if err != nil {
			return PrintError(stderr, err)
		}
		return renderView(view)
	case "exit":
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		view, err := a.GameExit(ctx, actorID)
		if err != nil {
			return PrintError(stderr, err)
		}
		return renderView(view)
	case "master":
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		view, appointed, err := a.GameMaster(ctx, actorID, actorName)
		if err != nil {
			return PrintError(stderr, err)
		}
		if !appointed {
			return renderView(view)
		}
		return PrintError(stderr, r.SimpleMessage("已自任本团 GM"))
	case "set":
		if fs.NArg() < 2 {
			fmt.Fprintln(stderr, "用法：game set <变量> [值]（只写变量名时查看当前值）")
			return 2
		}
		key, value, hasValue := "", "", false
		rest := fs.Args()[1:]
		if k, v, ok := strings.Cut(rest[0], "="); ok {
			key, value, hasValue = k, v, true
			if len(rest) > 1 {
				value += " " + strings.Join(rest[1:], " ")
			}
		} else {
			key = rest[0]
			if len(rest) > 1 {
				value, hasValue = strings.Join(rest[1:], " "), true
			}
		}
		if !hasValue {
			current, ok, err := a.GameVar(ctx, key)
			if err != nil {
				return PrintError(stderr, err)
			}
			if !ok {
				return PrintError(stderr, r.SimpleMessage("团变量 "+key+" 尚未设置"))
			}
			return PrintError(stderr, r.SimpleMessage(key+" = "+current))
		}
		view, err := a.GameSetVar(ctx, actorID, key, value)
		if err != nil {
			return PrintError(stderr, err)
		}
		return renderView(view)
	case "call":
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		players, err := a.GameCall(ctx, actorID)
		if err != nil {
			return PrintError(stderr, err)
		}
		if len(players) == 0 {
			return PrintError(stderr, r.SimpleMessage("本团还没有玩家"))
		}
		names := make([]string, 0, len(players))
		for _, p := range players {
			names = append(names, "@"+p.Name)
		}
		return PrintError(stderr, r.SimpleMessage("点名："+strings.Join(names, " ")))
	case "kick":
		if fs.NArg() < 2 {
			fmt.Fprintln(stderr, "用法：game kick <QQ|名字>")
			return 2
		}
		target := strings.TrimSpace(strings.Join(fs.Args()[1:], " "))
		_, kicked, err := a.GameKick(ctx, actorID, target)
		if err != nil {
			return PrintError(stderr, err)
		}
		return PrintError(stderr, r.SimpleMessage("已把 "+kicked.Name+" 踢下桌"))
	case "close", "open":
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		view, err := a.GameSetOpen(ctx, actorID, sub == "open")
		if err != nil {
			return PrintError(stderr, err)
		}
		return renderView(view)
	case "over":
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		view, err := a.GameOver(ctx, actorID)
		if err != nil {
			return PrintError(stderr, err)
		}
		notifyGameOver(ctx, view.ID)
		return PrintError(stderr, r.SimpleMessage("已结团，名单、变量、先攻和牌堆均已清空"))
	case "rou":
		return cmdGameRoulette(ctx, fs.Args()[1:], actorID, a, r, stderr)
	case "use":
		if fs.NArg() != 2 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		view, err := a.UseGame(ctx, fs.Arg(1))
		if err != nil {
			return PrintError(stderr, err)
		}
		return renderView(view)
	case "system", "book":
		if fs.NArg() != 2 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		view, err := a.SetGameSystem(ctx, fs.Arg(1))
		if err != nil {
			return PrintError(stderr, err)
		}
		return renderView(view)
	case "del", "delete":
		if fs.NArg() != 2 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		if err := a.DeleteGame(ctx, fs.Arg(1)); err != nil {
			return PrintError(stderr, err)
		}
		return PrintError(stderr, r.SimpleMessage("已删除游戏 "+fs.Arg(1)))
	case "help":
		fmt.Fprintln(r.Out, usage)
		return 0
	default:
		fmt.Fprintln(stderr, usage)
		return 2
	}
}

// cmdGameRoulette implements `.game rou` and its history/reset/clear actions.
func cmdGameRoulette(ctx context.Context, args []string, actorID string,
	a *app.App, r *Renderer, stderr io.Writer) int {
	const usage = "用法：game rou <面数>[*份数]｜game rou hist｜game rou reset｜game rou clr（面数×份数最多 100）"
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	switch args[0] {
	case "hist", "history":
		if len(args) != 1 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		drawn, roulette, err := a.GameRouletteHistory(ctx)
		if err != nil {
			return PrintError(stderr, err)
		}
		if roulette == nil {
			return PrintError(stderr, r.SimpleMessage("尚未启用轮盘骰"))
		}
		if len(drawn) == 0 {
			return PrintError(stderr, r.SimpleMessage("本轮还没有抽过点数"))
		}
		values := make([]string, 0, len(drawn))
		for _, v := range drawn {
			values = append(values, strconv.Itoa(v))
		}
		return PrintError(stderr, r.SimpleMessage(
			fmt.Sprintf("d%d 轮盘已抽（%d/%d）：%s",
				roulette.Sides, len(drawn), roulette.Sides*roulette.Copies, strings.Join(values, "、"))))
	case "reset":
		if len(args) != 1 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		if _, err := a.GameRouletteReset(ctx, actorID); err != nil {
			return PrintError(stderr, err)
		}
		return PrintError(stderr, r.SimpleMessage("轮盘已复原"))
	case "clr", "clear", "off":
		if len(args) != 1 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		if _, err := a.GameRouletteClear(ctx, actorID); err != nil {
			return PrintError(stderr, err)
		}
		return PrintError(stderr, r.SimpleMessage("已清除轮盘骰，恢复普通掷骰"))
	default:
		if len(args) != 1 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		sides, copies, err := app.ParseRouletteSpec(args[0])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		if _, err := a.GameRouletteEnable(ctx, actorID, sides, copies); err != nil {
			return PrintError(stderr, err)
		}
		return PrintError(stderr, r.SimpleMessage(fmt.Sprintf(
			"已为 d%d 启用轮盘骰（%d 份，袋中 %d 枚）", sides, copies, sides*copies)))
	}
}

// cmdTable rolls a declared random table or lists the available tables.
//
//	tb <表 id> [--system <规则书>]
//	tb list [--system <规则书>]
func cmdTable(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("tb", flag.ContinueOnError)
	fs.SetOutput(stderr)
	system := fs.String("system", "", "规则书 ID（默认角色所属）")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "用法：tb <表 id>｜tb list [--system <规则书>]")
		return 2
	}
	if fs.Arg(0) == "list" {
		tables, err := a.ListTables(ctx, *system)
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.TableList(tables); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}
	res, err := a.RollTable(ctx, *system, fs.Arg(0))
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.Table(res); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// cmdDerived reports the rulebook's derived statistics, optionally
// initializing the resource tracks. It is exposed as `st derived`.
//
//	st derived [--character <名字|id>] [--system <规则书>] [--init] [--force]
func cmdDerived(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("st derived", flag.ContinueOnError)
	fs.SetOutput(stderr)
	system := fs.String("system", "", "规则书 ID（默认角色所属）")
	characterID := fs.String("character", "", "角色名或 ID（默认当前角色）")
	init := fs.Bool("init", false, "初始化缺失的资源轨道并保存")
	force := fs.Bool("force", false, "与 --init 一起使用，强制重置已有轨道")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "用法：st derived [--character <名字|id>] [--init] [--force]")
		return 2
	}
	explicit := *characterID != ""
	useDefaultCharacter(ctx, characterID)
	tgt, err := flagTarget(ctx, a, *characterID, explicit)
	if err != nil {
		return PrintError(stderr, err)
	}
	ctx = withTarget(ctx, tgt)
	d, _, err := a.RunDerived(ctx, app.DerivedOptions{
		SystemID:    *system,
		CharacterID: tgt.CardID,
		Initialize:  *init,
		Overwrite:   *force,
	})
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.Derived(d, *init); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// cmdSkillAlloc manages the creation skill-point pools (st alloc): the
// pools the rulebook declares for the card's generation variant, the
// spending of pool points on skills and the deterministic reset.
//
//	st alloc                        查看点池
//	st alloc <池> <技能>±<点数>...    加点 / 退点（点数必须是整数）
//	st alloc clr                    重置全部加点（技能回落基础值）
func cmdSkillAlloc(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	if len(args) == 0 {
		view, err := a.SkillPointPools(ctx, currentCharacter(ctx))
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.SkillPools(view); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}
	if args[0] == "clr" {
		if len(args) != 1 {
			fmt.Fprintln(stderr, "用法：st alloc clr")
			return 2
		}
		_, view, err := a.ClearSkillPoints(ctx, currentCharacter(ctx))
		if err != nil {
			return PrintError(stderr, err)
		}
		if err := r.SkillPoolsCleared(view); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}
	if len(args) < 2 {
		fmt.Fprintln(stderr, "用法：st alloc [池 技能±点数...｜clr]")
		return 2
	}
	spends := make([]app.SkillSpend, 0, len(args)-1)
	for _, tok := range args[1:] {
		spend, err := app.ParseSkillSpend(tok)
		if err != nil {
			return PrintError(stderr, err)
		}
		spends = append(spends, spend)
	}
	_, results, _, err := a.AllocateSkillPoints(ctx, currentCharacter(ctx), args[0], spends)
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.SkillAllocation(results); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// cmdOpposed runs an opposed check between two sides. Each side is an
// explicit target value or a sheet-value name resolved through the
// rulebook (Dice! convention: `rav 侦查 60`, `rav 力量 力量`).
//
//	rav <名或值> <名或值> [--system <规则书>] [--character <名字|id>]
func cmdOpposed(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("rav", flag.ContinueOnError)
	fs.SetOutput(stderr)
	system := fs.String("system", "", "规则书 ID（默认角色所属）")
	characterID := fs.String("character", "", "角色名或 ID（默认当前角色）")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "用法：rav <名或值> <名或值> [--system <规则书>] [--character <名字|id>]")
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
	var values [2]int
	for i, tok := range fs.Args() {
		if n, err := ParseInt64(tok); err == nil {
			if n < 0 {
				fmt.Fprintf(stderr, "对抗检定需要两个非负整数目标值，得到 %q\n", tok)
				return 2
			}
			values[i] = int(n)
			continue
		}
		v, err := a.ResolveTargetValue(ctx, *system, *characterID, tok)
		if err != nil {
			return PrintError(stderr, err)
		}
		values[i] = v
	}
	res, err := a.RunOpposed(ctx, *system, values[0], values[1])
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.Opposed(res); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// --- card management (pc ...) ---

// cmdPCNew creates a card by hand. The new card is activated and its
// derived tracks are initialized by default (same rules as rolled
// generation); --active=false leaves the current card untouched and a card
// with incomplete characteristics keeps the tracks for a later `st derived --init`.
//
//	pc new <名称> [K=V...] [--system <规则书>] [--active=false]
func cmdPCNew(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("pc new", flag.ContinueOnError)
	fs.SetOutput(stderr)
	system := fs.String("system", "", "规则书 ID（默认角色所属）")
	active := fs.Bool("active", true, "创建后设为当前角色（--active=false 不切换）")
	sets := multiFlag{}
	fs.Var(&sets, "set", "属性/技能赋值 KEY=VALUE，可重复")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(stderr, "用法：pc new <名称> [KEY=VALUE...] [--system <规则书>] [--active=false]")
		return 2
	}
	pairs := append(append([]string{}, sets.values...), fs.Args()[1:]...)
	assignments, err := parseAssignments(pairs)
	if err != nil {
		return PrintError(stderr, err)
	}
	c, derived, err := a.CreateCharacter(ctx, *system, fs.Arg(0), assignments, *active)
	if err != nil {
		return PrintError(stderr, err)
	}
	if *active {
		notifyCardCreated(ctx, c.ID, c.Name)
	}
	if err := r.CreatedCharacter(c, createdNotice(c, derived)); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// createdNotice is the extra line after a card creation: the derived
// resource tracks just recorded, or the `st derived --init` hint when the sheet was
// too incomplete to derive them yet.
func createdNotice(c *character.Character, derived *engine.DerivedResult) string {
	switch {
	case derived != nil && len(derived.Vitals) > 0:
		tracks := make([]string, 0, len(derived.Vitals))
		for _, v := range derived.Vitals {
			tracks = append(tracks, fmt.Sprintf("%s %d/%d", strings.ToUpper(v.Resource), v.Current, v.Max))
		}
		return "状态：" + strings.Join(tracks, "，")
	case len(c.Resources) == 0 && derived == nil:
		return "属性未填全，资源轨尚未初始化；补齐属性后可用 st derived --init 初始化"
	default:
		return ""
	}
}

// cmdPCUse switches the active card.
func cmdPCUse(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "用法：pc use <名字|id>")
		return 2
	}
	id, err := cardRef(ctx, a, args[0])
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := a.SetActiveCharacter(ctx, id); err != nil {
		return PrintError(stderr, err)
	}
	return PrintError(stderr, r.SimpleMessage("当前角色："+id))
}

// cmdPCActive prints the active card id.
func cmdPCActive(ctx context.Context, a *app.App, r *Renderer, stderr io.Writer) int {
	id, err := a.ActiveCharacterID(ctx)
	if err != nil {
		return PrintError(stderr, err)
	}
	if id == "" {
		fmt.Fprintln(stderr, "未选择当前角色")
		return 1
	}
	return PrintError(stderr, r.SimpleMessage(id))
}

// cmdPCDelete deletes one card.
func cmdPCDelete(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "用法：pc del <名字|id>")
		return 2
	}
	id, err := cardRef(ctx, a, args[0])
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := a.DeleteCharacter(ctx, id); err != nil {
		return PrintError(stderr, err)
	}
	return PrintError(stderr, r.SimpleMessage("已删除角色 "+args[0]))
}

// cmdPCValidate re-validates a card against its rulebook.
func cmdPCValidate(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	if len(args) > 1 {
		fmt.Fprintln(stderr, "用法：pc val [名字|id]")
		return 2
	}
	ref := ""
	if len(args) == 1 {
		ref = args[0]
	}
	id, err := cardRef(ctx, a, defaultRef(ctx, ref))
	if err != nil {
		return PrintError(stderr, err)
	}
	c, err := a.ValidateCharacter(ctx, id)
	if err != nil {
		return PrintError(stderr, err)
	}
	return PrintError(stderr, r.SimpleMessage(fmt.Sprintf("角色 %s（%s）校验通过", c.ID, c.Name)))
}

// cmdPCCopy duplicates a card.
//
//	pc copy <名字|id> <新名称> [--active]
func cmdPCCopy(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("pc copy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	active := fs.Bool("active", false, "复制后设为当前角色")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "用法：pc copy <名字|id> <新名称> [--active]")
		return 2
	}
	id, err := cardRef(ctx, a, fs.Arg(0))
	if err != nil {
		return PrintError(stderr, err)
	}
	c, err := a.CopyCharacter(ctx, id, fs.Arg(1), *active)
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.CreatedCharacter(c, ""); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// cmdPCExport writes a card's YAML document to stdout or a file.
func cmdPCExport(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("pc export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("file", "", "输出文件路径（默认标准输出）")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "用法：pc export [名字|id] [--file 路径]")
		return 2
	}
	id, err := cardRef(ctx, a, optionalID(fs))
	if err != nil {
		return PrintError(stderr, err)
	}
	data, err := a.ExportCharacter(ctx, id)
	if err != nil {
		return PrintError(stderr, err)
	}
	if *file == "" {
		if _, err := r.Out.Write(data); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}
	if err := os.WriteFile(*file, data, 0o600); err != nil {
		return PrintError(stderr, apperr.Wrap("cli.export", apperr.StorageWriteFailed, "cannot write "+*file, err))
	}
	return PrintError(stderr, r.SimpleMessage("已导出到 "+*file))
}

// cmdPCImport reads a card YAML document from a file or stdin.
func cmdPCImport(ctx context.Context, args []string, a *app.App, r *Renderer, stdin io.Reader, stderr io.Writer) int {
	fs := flag.NewFlagSet("pc import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("file", "", "YAML 文件路径，- 表示标准输入")
	keepID := fs.Bool("keep-id", false, "保留文件内的角色 ID（默认重新生成）")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	if *file == "" || fs.NArg() != 0 {
		fmt.Fprintln(stderr, "用法：pc import --file <路径|-> [--keep-id]")
		return 2
	}
	var data []byte
	var err error
	if *file == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(*file)
	}
	if err != nil {
		return PrintError(stderr, apperr.Wrap("cli.import", apperr.StorageReadFailed, "cannot read "+*file, err))
	}
	c, err := a.ImportCharacter(ctx, data, *keepID)
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.CreatedCharacter(c, ""); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// cmdPCMeta sets or removes one metadata entry on a card.
//
//	pc meta [--character <名字|id>] <键> [值]
func cmdPCMeta(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("pc meta", flag.ContinueOnError)
	fs.SetOutput(stderr)
	characterID := fs.String("character", "", "角色名或 ID（默认当前角色）")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		fmt.Fprintln(stderr, "用法：pc meta [--character <名字|id>] <键> [值]")
		return 2
	}
	explicit := *characterID != ""
	useDefaultCharacter(ctx, characterID)
	tgt, err := flagTarget(ctx, a, *characterID, explicit)
	if err != nil {
		return PrintError(stderr, err)
	}
	ctx = withTarget(ctx, tgt)
	key, value := fs.Arg(0), ""
	if fs.NArg() == 2 {
		value = fs.Arg(1)
	}
	c, err := a.SetCharacterMeta(ctx, tgt.CardID, key, value)
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.Character(c); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// cmdRule is the single entry point for rulebook data: inspection,
// validation, quick-reference lookup, creation and house-rule patches.
// Dice!Next keeps these in separate commands; dicegirl consolidates them
// under `rule` so there is one user-facing noun.
//
//	rule list [--system <规则书>]
//	rule validate [--system <规则书>]
//	rule ref <词条>｜rule ref list｜rule ref set <系统>
//	rule show all [--system <规则书>]
//	rule show <section> <id> [--system <规则书>]
//	rule sections [--system <规则书>]
//	rule new <新ID> --from <基础规则书>
//	rule patch '<JSON>'｜rule patch --file <路径>
//	rule add --file <路径> [--force]     安装用户规则书（QQ 里先发文件再 rule add）
//	rule del <id>                        卸载用户规则书（内置与在用规则书拒绝删除）
func cmdRule(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("rule", flag.ContinueOnError)
	fs.SetOutput(stderr)
	system := fs.String("system", "", "规则书 ID（默认角色所属）")
	from := fs.String("from", "", "基础规则书 ID")
	base := fs.String("base", "", "基础规则书 ID")
	file := fs.String("file", "", "Patch JSON / 规则书 YAML 文件路径")
	force := fs.Bool("force", false, "覆盖同名用户规则书")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(stderr, "用法：rule list｜rule validate｜rule ref ...｜rule show ...｜rule sections｜rule new ...｜rule patch ...｜rule add ...｜rule del <id>")
		return 2
	}
	switch fs.Arg(0) {
	case "add", "install", "upload":
		if *file == "" {
			fmt.Fprintln(stderr, "用法：rule add --file <规则书.yaml> [--force]")
			return 2
		}
		data, rerr := os.ReadFile(*file)
		if rerr != nil {
			return PrintError(stderr, apperr.Wrap("chatcore.rule", apperr.StorageReadFailed, "cannot read "+*file, rerr))
		}
		id, err := a.InstallRulebook(ctx, data, *force)
		if err != nil {
			return PrintError(stderr, err)
		}
		return PrintError(stderr, r.SimpleMessage("已安装规则书 "+id+"（rule list 查看；rule validate 可再校验）"))
	case "del", "remove", "uninstall":
		if fs.NArg() != 2 {
			fmt.Fprintln(stderr, "用法：rule del <规则书id>")
			return 2
		}
		if err := a.UninstallRulebook(ctx, fs.Arg(1)); err != nil {
			return PrintError(stderr, err)
		}
		return PrintError(stderr, r.SimpleMessage("已卸载规则书 "+fs.Arg(1)))
	case "list":
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "用法：rule list")
			return 2
		}
		if err := r.Systems(a.Registry().List()); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "validate":
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "用法：rule validate [--system <规则书>]")
			return 2
		}
		if *system != "" {
			if err := a.ValidateSystemConfig(ctx, *system); err != nil {
				return PrintError(stderr, err)
			}
			return PrintError(stderr, r.SimpleMessage("规则书 "+*system+" 配置有效"))
		}
		for _, m := range a.Registry().List() {
			if err := a.ValidateSystemConfig(ctx, m.ID); err != nil {
				return PrintError(stderr, err)
			}
			if err := r.SimpleMessage("规则书 " + m.ID + " 配置有效"); err != nil {
				return PrintError(stderr, err)
			}
		}
		if err := a.ValidateDataPacks(ctx); err != nil {
			return PrintError(stderr, err)
		}
		for _, p := range a.DataPacks() {
			if err := r.SimpleMessage("数据包 " + p.ID() + " 配置有效"); err != nil {
				return PrintError(stderr, err)
			}
		}
		return 0
	case "ref":
		return cmdRules(ctx, fs.Args()[1:], a, r, stderr)
	case "show":
		if fs.NArg() == 2 && fs.Arg(1) == "all" {
			data, err := a.RuleShowAll(ctx, *system)
			if err != nil {
				return PrintError(stderr, err)
			}
			if _, err := r.Out.Write(data); err != nil {
				return PrintError(stderr, err)
			}
			fmt.Fprintln(r.Out)
			return 0
		}
		section, entryID := "", ""
		switch fs.NArg() {
		case 2:
			section = fs.Arg(1)
			if section != "mechanic" && section != "opposed" {
				fmt.Fprintln(stderr, "用法：rule show all｜rule show <section> <id>")
				return 2
			}
		case 3:
			section, entryID = fs.Arg(1), fs.Arg(2)
		default:
			fmt.Fprintln(stderr, "用法：rule show all｜rule show <section> <id>")
			return 2
		}
		data, err := a.RuleShow(ctx, *system, section, entryID)
		if err != nil {
			return PrintError(stderr, err)
		}
		if _, err := r.Out.Write(data); err != nil {
			return PrintError(stderr, err)
		}
		fmt.Fprintln(r.Out)
		return 0
	case "sections":
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "用法：rule sections")
			return 2
		}
		secs, err := a.RuleSections(ctx, *system)
		if err != nil {
			return PrintError(stderr, err)
		}
		keys := make([]string, 0, len(secs))
		for k := range secs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(r.Out, "%s: %s\n", k, strings.Join(secs[k], ", "))
		}
		return 0
	case "patch", "apply", "edit", "set":
		// The remaining positional args form one JSON array of PatchOp, or
		// --file points at a JSON document.
		jsonText := strings.TrimSpace(strings.Join(fs.Args()[1:], " "))
		if *file != "" {
			data, rerr := os.ReadFile(*file)
			if rerr != nil {
				return PrintError(stderr, apperr.Wrap("cli.rule", apperr.StorageReadFailed, "cannot read "+*file, rerr))
			}
			jsonText = string(data)
		}
		if jsonText == "" {
			fmt.Fprintln(stderr, "用法：rule patch '[{\"kind\":\"skill_set\",\"target\":\"spot_hidden\",\"field\":\"base\",\"value\":\"25\"}]'｜rule patch --file <路径>")
			return 2
		}
		var ops []engine.PatchOp
		if err := json.Unmarshal([]byte(jsonText), &ops); err != nil {
			fmt.Fprintf(stderr, "Patch JSON 解析失败：%v\n", err)
			return 2
		}
		if err := a.ApplyRulePatch(ctx, *system, ops); err != nil {
			return PrintError(stderr, err)
		}
		return PrintError(stderr, r.SimpleMessage("规则书已更新"))
	case "new", "create", "copy":
		if fs.NArg() != 2 {
			fmt.Fprintln(stderr, "用法：rule new <新ID> --from <基础规则书>")
			return 2
		}
		baseID := *from
		if baseID == "" {
			baseID = *base
		}
		if baseID == "" {
			fmt.Fprintln(stderr, "需要 --from <基础规则书>")
			return 2
		}
		if err := a.CreateRulebookFrom(ctx, baseID, fs.Arg(1)); err != nil {
			return PrintError(stderr, err)
		}
		return PrintError(stderr, r.SimpleMessage("已创建规则书 "+fs.Arg(1)))
	default:
		fmt.Fprintln(stderr, "用法：rule list｜rule validate｜rule ref ...｜rule show ...｜rule sections｜rule new ...｜rule patch ...｜rule add ...｜rule del <id>")
		return 2
	}
}

// cmdPCCopyOnto copies the values of one card onto another (the Dice!
// `.pc cpy <目标>=<来源>` form). Both sides accept a name or an id.
func cmdPCCopyOnto(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "用法：pc cpy <目标>=<来源>（名字或 id）")
		return 2
	}
	targetRef, sourceRef, ok := strings.Cut(args[0], "=")
	if !ok || targetRef == "" || sourceRef == "" {
		fmt.Fprintln(stderr, "用法：pc cpy <目标>=<来源>（名字或 id）")
		return 2
	}
	target, err := cardRef(ctx, a, targetRef)
	if err != nil {
		return PrintError(stderr, err)
	}
	source, err := cardRef(ctx, a, sourceRef)
	if err != nil {
		return PrintError(stderr, err)
	}
	c, err := a.CopyCharacterProperties(ctx, target, source)
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.Character(c); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// cmdDeathSave is the T17b death save (`ds`): one roll of the book's
// mechanic die, counters persisted on the card. Parameters (threshold, caps,
// crit values) are rulebook data; the tally is card state.
//
//	ds                     掷一次死亡豁免
//	ds reset               清空计数（伤势稳定 / 角色死亡 / 重新倒地时用）
func cmdDeathSave(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("ds", flag.ContinueOnError)
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
	ctx = withTarget(ctx, tgt)

	if fs.Arg(0) == "reset" {
		if err := a.ResetDeathSave(ctx, *system, tgt.CardID); err != nil {
			return PrintError(stderr, err)
		}
		if err := r.SimpleMessage("死亡豁免计数已清空"); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "用法：ds [--character <名字|id>] ｜ ds reset")
		return 2
	}
	out, err := a.RunDeathSave(ctx, *system, tgt.CardID)
	if err != nil {
		return PrintError(stderr, err)
	}

	var verdict string
	switch {
	case out.Crit:
		verdict = "天然 " + strconv.Itoa(out.Roll) + "：恢复 1 HP 起身，计数清空"
		if !out.RestoredHP {
			verdict += "（卡上没有 hp 轨道，仅清空计数）"
		}
	case out.CritFail:
		verdict = "天然 " + strconv.Itoa(out.Roll) + "：一次计两次失败"
	case out.Success:
		verdict = strconv.Itoa(out.Roll) + " ≥ " + strconv.Itoa(out.Threshold) + "：成功"
	default:
		verdict = strconv.Itoa(out.Roll) + " < " + strconv.Itoa(out.Threshold) + "：失败"
	}
	status := ""
	switch {
	case out.Died:
		status = "—— 角色死亡"
	case out.Stabilised:
		status = "—— 伤势稳定"
	}
	if err := r.SimpleMessage("死亡豁免 d" + strconv.Itoa(out.DieMax) + " = " + strconv.Itoa(out.Roll) + " → " + verdict +
		"\n计数：成功 " + strconv.Itoa(out.Successes) + "/" + strconv.Itoa(out.Needed) +
		"，失败 " + strconv.Itoa(out.Failures) + "/" + strconv.Itoa(out.Allowed) + status); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}
