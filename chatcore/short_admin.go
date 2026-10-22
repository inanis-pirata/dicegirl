package chatcore

// Rulebook/data-pack inspection, card management that is not part of the
// chat flow, and the derived/damage/opposed/table helpers: every command the
// CLI and the adapters share goes through this one chat-style surface.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"dicegirl/internal/app"
	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
	"dicegirl/internal/engine"
)

// shortSYS lists rulebooks or validates every loaded rulebook/data pack.
//
//	sys                 列出已加载规则书（同 sys list）
//	sys validate [--system <规则书>]
func shortSYS(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("sys", flag.ContinueOnError)
	fs.SetOutput(stderr)
	system := fs.String("system", "", "规则书 ID（validate 省略则校验全部）")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	sub := "list"
	if fs.NArg() > 0 {
		sub = fs.Arg(0)
	}
	switch sub {
	case "list":
		if fs.NArg() > 1 {
			fmt.Fprintln(stderr, "用法：sys｜sys list｜sys validate [--system <规则书>]")
			return 2
		}
		if err := r.Systems(a.Registry().List()); err != nil {
			return PrintError(stderr, err)
		}
		return 0
	case "validate":
		if fs.NArg() > 1 {
			fmt.Fprintln(stderr, "用法：sys validate [--system <规则书>]")
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
	default:
		fmt.Fprintln(stderr, "用法：sys｜sys list｜sys validate [--system <规则书>]")
		return 2
	}
}

// shortTB rolls a declared random table or lists the available tables.
//
//	tb <表 id> [--system <规则书>]
//	tb list [--system <规则书>]
func shortTB(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
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

// shortDV reports the rulebook's derived statistics, optionally
// initializing the resource tracks.
//
//	dv [--character <名字|id>] [--system <规则书>] [--init] [--force]
func shortDV(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("dv", flag.ContinueOnError)
	fs.SetOutput(stderr)
	system := fs.String("system", "", "规则书 ID")
	characterID := fs.String("character", "", "角色名或 ID（默认当前角色）")
	init := fs.Bool("init", false, "初始化缺失的资源轨道并保存")
	force := fs.Bool("force", false, "与 --init 一起使用，强制重置已有轨道")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "用法：dv [--character <名字|id>] [--init] [--force]")
		return 2
	}
	useDefaultCharacter(ctx, characterID)
	id, err := cardRef(ctx, a, *characterID)
	if err != nil {
		return PrintError(stderr, err)
	}
	d, _, err := a.RunDerived(ctx, app.DerivedOptions{
		SystemID:    *system,
		CharacterID: id,
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

// shortDMG rolls weapon damage with the rulebook's damage bonus.
//
//	dmg <表达式> [--character <名字|id>] [--system <规则书>]
func shortDMG(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("dmg", flag.ContinueOnError)
	fs.SetOutput(stderr)
	system := fs.String("system", "", "规则书 ID")
	characterID := fs.String("character", "", "角色名或 ID（默认当前角色）")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "用法：dmg <表达式> [--character <名字|id>] [--system <规则书>]")
		return 2
	}
	useDefaultCharacter(ctx, characterID)
	id, err := cardRef(ctx, a, *characterID)
	if err != nil {
		return PrintError(stderr, err)
	}
	res, err := a.RunDamage(ctx, app.DamageOptions{
		SystemID:    *system,
		Expr:        fs.Arg(0),
		CharacterID: id,
	})
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.Damage(res); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// shortOPP runs an opposed check between two target values.
//
//	opp <A值> <B值> [--system <规则书>]
func shortOPP(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("opp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	system := fs.String("system", "", "规则书 ID")
	if err := parseSub(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "用法：opp <A值> <B值> [--system <规则书>]")
		return 2
	}
	var values [2]int
	for i, tok := range fs.Args() {
		n, err := ParseInt64(tok)
		if err != nil || n < 0 {
			fmt.Fprintf(stderr, "对抗检定需要两个非负整数目标值，得到 %q\n", tok)
			return 2
		}
		values[i] = int(n)
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

// shortPCNew creates a card by hand. The new card is activated and its
// derived tracks are initialized by default (same rules as rolled
// generation); --active=false leaves the current card untouched and a card
// with incomplete characteristics keeps the tracks for a later `dv --init`.
//
//	pc new <名称> [K=V...] [--system <规则书>] [--active=false]
func shortPCNew(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
	fs := flag.NewFlagSet("pc new", flag.ContinueOnError)
	fs.SetOutput(stderr)
	system := fs.String("system", "", "规则书 ID（省略用默认书）")
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
// resource tracks just recorded, or the `dv --init` hint when the sheet was
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
		return "属性未填全，资源轨尚未初始化；补齐属性后可用 dv --init 初始化"
	default:
		return ""
	}
}

// shortPCUse switches the active card.
func shortPCUse(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
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

// shortPCActive prints the active card id.
func shortPCActive(ctx context.Context, a *app.App, r *Renderer, stderr io.Writer) int {
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

// shortPCDelete deletes one card.
func shortPCDelete(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
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

// shortPCValidate re-validates a card against its rulebook.
func shortPCValidate(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
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

// shortPCCopy duplicates a card.
//
//	pc copy <名字|id> <新名称> [--active]
func shortPCCopy(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
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

// shortPCExport writes a card's YAML document to stdout or a file.
func shortPCExport(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
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

// shortPCImport reads a card YAML document from a file or stdin.
func shortPCImport(ctx context.Context, args []string, a *app.App, r *Renderer, stdin io.Reader, stderr io.Writer) int {
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

// shortPCMeta sets or removes one metadata entry on a card.
//
//	pc meta [--character <名字|id>] <键> [值]
func shortPCMeta(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
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
	useDefaultCharacter(ctx, characterID)
	id, err := cardRef(ctx, a, *characterID)
	if err != nil {
		return PrintError(stderr, err)
	}
	key, value := fs.Arg(0), ""
	if fs.NArg() == 2 {
		value = fs.Arg(1)
	}
	c, err := a.SetCharacterMeta(ctx, id, key, value)
	if err != nil {
		return PrintError(stderr, err)
	}
	if err := r.Character(c); err != nil {
		return PrintError(stderr, err)
	}
	return 0
}

// shortPCCopyOnto copies the values of one card onto another (the Dice!
// `.pc cpy <目标>=<来源>` form). Both sides accept a name or an id.
func shortPCCopyOnto(ctx context.Context, args []string, a *app.App, r *Renderer, stderr io.Writer) int {
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
