package cli

import (
	"context"
	"fmt"
	"io"

	"dicegirl/chatcore"
	"dicegirl/internal/app"
	"dicegirl/internal/config"
	"dicegirl/internal/dice"
	"dicegirl/internal/version"
)

// Version is the CLI version, taken from the shared release version.
const Version = version.Version

const usageText = `dicegirl — 本地 TRPG 骰子与规则引擎

用法：
  dicegirl [全局参数] <指令> [参数]

全局参数：
  --data-root <目录>   数据根目录（默认 ./data，也可用 DICEGIRL_DATA_ROOT）
  --json               以 JSON 输出结构化结果
  --seed <整数>        固定随机种子（可复现，调试用）
  -h, --help           显示帮助

指令（首字符 .。!！ 可省略，也可与首个参数粘连，如 .r3d6；--flag 写在参数前后均可）：

  掷骰与检定
    r [语句] [原因]                   掷骰；无语句时掷默认骰（默认 d100）
    rd [面数]                         默认骰；rd6 等价 d6
    rs <语句> [原因]                  精简掷骰（只出总值）
    ra/rc [难度] [n#] [名称] [目标] [±n]  检定；ra 15+1 优势、ra 15-1 劣势、ra 困难 15
    rb/rp [数量] <检定参数>            奖励骰 / 惩罚骰
    rav <A值> <B值> [--system <规则书>]  对抗检定
    en <技能/属性> [目标|成长量] [原因]  成长检定（成长后直接写卡）

  角色卡
    pc new <名称> [K=V...] [--system <规则书>] [--active]   手工建卡
    pc list｜pc show [名字|id]｜pc use <名字|id>｜pc active｜pc val [名字|id]
    pc nn [名字|id] <新名>｜pc copy <名字|id> <新名>｜pc cpy <目标>=<来源>
    pc del <名字|id>｜pc meta <键> [值]｜pc export [名字|id] [--file <路径>]
    pc import --file <路径|-> [--keep-id]   从 YAML 导入角色卡
    pc redo [名字|id]｜pc build [名字|id]｜pc stat [名字|id]｜pc clr
    st show｜st show <键>｜st del <键>｜st clr
    st K=V｜st K60｜st K 60｜st K±表达式...｜st &名=表达式   查看/修改当前角色

  人物作成（别名由规则书自己声明：coc → CoC 7e，dnd → D&D 5e）
    <别名>[版本][d] [数量]      如 coc6d（6 版购点法）、coc 10、dndd（27 点购点）

  资源与规则
    sc <成功损失>/<失败损失> [当前值] [原因] [--apply]   理智检定
    rule list｜rule validate [--system <规则书>]   规则书列表 / 重新校验
    rule ref <词条>｜rule ref list｜rule ref set [系统]  规则速查
    tb <表 id>｜tb list [--system <规则书>]     规则书声明的随机表
    ti｜li                            临时/总结性疯狂表（CoC）
    st derived [--character <名字|id>] [--init] [--force]   派生数值（调整值/HP/MP/理智轨等）

  状态与团务
    game｜game list｜game new/use/show/system/del   游戏（团）管理
    game join/exit｜master｜set 变量 [值]｜call｜kick <QQ|名字>
    game close/open｜over｜rou <面数>[*份数]｜rou hist/reset/clr
                                      玩家名单、团变量、开关、结团与轮盘骰
    log [new <名称>]｜on｜off｜end｜halt 跑团记录状态 / 开始 / 暂停 / 继续 / 结束
    log list [页]｜stat [名称]｜del <名称>   日志列表 / 统计 / 删除
    log get|export [名称]｜type txt|html｜timer on|off｜time
                                      导出日志文件 / 格式 / 跑团计时
    set [面数]｜set list｜set <规则书>   默认骰面数 / 规则书列表与切换
    ak new｜add <选项...>｜del <选项>｜get｜show｜clr   随机抉择
    name [cn|jp|en] [数量]            随机姓名
    ri [±加值|表达式] [名称]｜init｜init clr   先攻掷骰 / 查看 / 清空
    deck new/set/show/reset/clr｜draw <牌堆> [数量]   牌堆与抽牌
    w [骰子个数]a[加骰参数]｜ww ...    骰池（只出总数 / 每颗点数）
    h/rh ...｜rah/rch ...              暗骰/暗骰检定（需要聊天平台适配器，CLI 不支持）

骰子表达式语法：
  NdM、dM、d%；四则运算 + - * / 与括号；保留/丢弃 kh kl dh dl（如 4d6kh3）。
  重复前缀 n#<表达式>（n 为 1–10，如 3#1d20）；比较后缀置于整条表达式末尾：
  >= > <= < 目标值（如 2d6>=7，逐轮独立判定）。
  除法向零取整；除零、面数/数量/重复次数超限为明确错误。

规则全部来自规则书 YAML：内置 coc7（默认）与 dnd5e，用户可放 <数据根>/rulebooks/*.yaml。
检定类命令用角色所属规则书或 --system 确定系统；都没有时用声明了 default 的规则书。
`

type globals struct {
	dataRoot string
	json     bool
	seed     int64
	hasSeed  bool
}

// Run parses args and executes one command, returning a process exit code
// (0 success, 2 usage error, 1 runtime error). The chat-style command
// grammar itself lives in package chatcore, shared with the bot adapters.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	g, rest, ok := parseGlobals(args, stderr)
	if !ok {
		return 2
	}
	if len(rest) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	cmd, cmdArgs := rest[0], rest[1:]
	if cmd == "help" || cmd == "-h" || cmd == "--help" {
		fmt.Fprint(stdout, usageText)
		return 0
	}
	if cmd == "version" {
		fmt.Fprintln(stdout, "dicegirl "+Version)
		return 0
	}

	cfg, err := config.FromEnv()
	if err != nil {
		return chatcore.PrintError(stderr, err)
	}
	if err := cfg.Resolve(g.dataRoot); err != nil {
		return chatcore.PrintError(stderr, err)
	}

	var opts []app.Option
	if g.hasSeed {
		opts = append(opts, app.WithRoller(dice.New(dice.NewSeededSource(uint64(g.seed)))))
	}
	application, err := app.New(ctx, cfg, opts...)
	if err != nil {
		return chatcore.PrintError(stderr, err)
	}
	defer application.Close()

	code := chatcore.Execute(ctx, application, stdin, stdout, stderr, g.json, cmd, cmdArgs)
	if code == chatcore.Unrecognized {
		fmt.Fprintf(stderr, "未知指令：%s（可用指令见 dicegirl help；首字符可加 .。!！ 前缀，也可省略）\n", cmd)
		return 2
	}
	return code
}

func parseGlobals(args []string, stderr io.Writer) (globals, []string, bool) {
	var g globals
	i := 0
	for i < len(args) {
		a := args[i]
		switch a {
		case "--json":
			g.json = true
			i++
		case "--data-root":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "--data-root 需要一个参数")
				return g, nil, false
			}
			g.dataRoot = args[i+1]
			i += 2
		case "--seed":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "--seed 需要一个整数参数")
				return g, nil, false
			}
			n, err := chatcore.ParseInt64(args[i+1])
			if err != nil {
				fmt.Fprintf(stderr, "--seed 必须是整数，得到 %q\n", args[i+1])
				return g, nil, false
			}
			g.seed, g.hasSeed = n, true
			i += 2
		default:
			return g, args[i:], true
		}
	}
	return g, nil, true
}
