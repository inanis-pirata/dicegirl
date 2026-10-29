# dicegirl

<p align="center">
  <img src="docs/assets/dicegirl.png" alt="dicegirl 看板娘" width="180">
</p>

跑团用的骰子工具与规则引擎，本地运行、数据自持。内置 **CoC 7 版（克苏鲁的呼唤第七版，默认）** 与 **D&D 5e（2014）** 两套完整规则；同一套聊天式指令通用于命令行和 QQ 机器人，从掷骰、检定、角色卡、人物作成，到先攻、牌堆、多游戏团务都能管。

- **两套规则，开箱即用**：CoC 7e 覆盖百分骰与成功等级、奖励/惩罚骰、显式难度、对抗检定、理智检定、伤害加值、派生属性、疯狂表、成长检定与骰点/购点人物作成；D&D 5e 覆盖 d20 + DC、优势/劣势、18 项技能、派生数值、4d6 丢最低与 27 点购点、随机表与规则速查。两套规则分别声明在 [rulebooks/coc7.yaml](rulebooks/coc7.yaml) 与 [rulebooks/dnd5e.yaml](rulebooks/dnd5e.yaml)。
- **一本 YAML 规则书 = 一个游戏**：Go 引擎里没有任何游戏专属代码——检定掷什么骰、怎样算成功、成功等级如何划分、奖惩骰如何取捨，全部写在规则书里（CoC 声明 `1d100` + `roll <= target`，D&D 声明 `1d20` + `roll >= target`）。添加新游戏只需把同构 YAML 放进 `<数据根>/rulebooks/`，还能在身份头声明自己的人物作成别名（如 `.pf`），不用改代码、不用重编译。另附一份最小 d20 示例书 [docs/examples/dnd-lite.yaml](docs/examples/dnd-lite.yaml)。
- **通用骰子引擎，不绑定任何规则**：支持保留/丢弃（`4d6kh3`）、连掷与逐轮判定（`3#1d20>=10`）、爆骰、重投、骰池计数与完整四则运算，随机源可注入；完整语法见下文「骰子表达式语法」。
- **本地优先，数据自持**：不依赖任何云服务。规则书与数据包是可直接手改的 YAML（冷数据，启动时加载一次）；角色卡、掷骰统计与团务状态存放在 `<数据根>/dicegirl.db`（SQLite，纯 Go 驱动、无需 cgo），旧版 YAML 数据根首次打开自动迁移，写入前自动备份。
- **CLI 与机器人共用一套核心**：CLI 可独立使用，也可作为 [QQ 官方机器人](https://bot.q.qq.com/wiki/)入群（群聊 @ 与 C2C 私聊，支持暗骰、围观、按游戏绑卡、跑团记录，按群隔离数据）。两者共用 `chatcore/` 语法与用例层，规则核心与界面完全解耦；引擎同时是可复用的 Go 库，未来接入 Discord、Telegram 等平台只需新增适配器。

## 构建与测试

需要 Go 1.26+（开发环境为 Go 1.27）。

```bash
go build -o bin/dicegirl ./cmd/dicegirl
make build        # 同上
make test         # go test ./...
make race         # go test -race ./...
make vet          # go vet ./...
make fmt          # gofmt -w
```

外部依赖只有两个：[gopkg.in/yaml.v3](https://github.com/go-yaml/yaml)（角色卡与规则数据的 YAML 编解码）与 [gorilla/websocket](https://github.com/gorilla/websocket)（QQ 网关长连接）；SQLite 使用纯 Go 驱动，无需 cgo。

国内网络建议先设模块代理，否则首次下载会很慢；依赖只需下载一次，之后走本地构建缓存：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
go mod download
```

## 快速开始

CLI 只有一套聊天风格指令（首字符 `.。!！` 可省略）：

```bash
dicegirl .r "2d6+3"
dicegirl .r "4d6kh3"            # 掷 4d6 保留最高 3 个
dicegirl .r "3#1d20>=10"        # 连掷 3 次 d20，逐轮判定是否 ≥10
dicegirl .r "1d8+2" 中型刀伤害   # 后续参数作为原因文本回显
dicegirl .rule list              # 列出已加载的规则书

dicegirl .pc new 调查员甲 STR=50 CON=60 SIZ=70 DEX=55 POW=55 EDU=70 \
  psychology=60 --active
dicegirl .st derived --init      # 派生数值并初始化生命/理智轨
dicegirl .ra 心理学+1            # 技能检定（+1 = 奖励骰）
dicegirl .ra 力量-1              # 属性检定（-1 = 惩罚骰）
dicegirl .ra 理智                # 资源轨检定（名字由规则书声明）
dicegirl .sc 0/1d10 --apply      # 理智检定（写回角色卡）
dicegirl .r 1d8+db               # 武器伤害（db 自动展开伤害加值）
dicegirl .coc                    # 摇属性建卡（附带技能点池）
dicegirl .st alloc 职业 侦查+40    # 作成后分配技能点（st alloc 查看余量）
dicegirl .pc show                # 查看当前角色
```

内置的 D&D 5e（2014）规则书，同样只用指令：

```bash
dicegirl .dnd                     # 4d6 丢最低生成一张 D&D 角色卡并设为当前角色
dicegirl .dnd 3                   # 一次生成三张
dicegirl .dndd                    # 27 点购点（标准数组 15/14/13/12/10/8），一次一张
dicegirl .ra 15                   # d20 vs DC 15
dicegirl .ra 15+1                 # 优势：两颗 d20 取高
dicegirl .ra 15-1                 # 劣势：取低
dicegirl .ra 15 --mod -5          # d20+5 vs DC 15（等价于 .ra 10）
dicegirl .ra 困难 15              # 相对 DC +5
dicegirl .rule ref 优势           # 规则速查（.rule ref list 列出全部词条）
```

规则书也可以直接查看和改：

```bash
dicegirl .rule sections                       # 列出技能/属性/等级/词条等条目
dicegirl .rule show skill spot_hidden         # 查看某一条
dicegirl .rule show all                       # 查看整本 YAML
dicegirl .rule new my_coc --from coc7         # 以 coc7 为基础派生新规则书
dicegirl .rule patch --file house-rules.json  # 应用 house rules 并热更新
```

**人物作成的别名由规则书自己声明**（身份头的 `commands`）：内置 `coc` → CoC 7e、`dnd` → D&D 5e；用户规则书声明 `commands: ["pf"]` 就能直接用 `.pf`，加游戏不用改 Go。

### 指令速查

| 分类 | 指令 |
| --- | --- |
| 掷骰 | `.r`（掷骰）、`.rd`（默认骰）、`.rs`（只出总值）、`.w`/`.ww`（骰池）、`.set`（默认骰面 / 规则书列表与切换） |
| 检定 | `.ra`/`.rc`（技能/属性/资源轨检定，如 `.ra 理智`；`+1` 优势、`-1` 劣势、难度关键词、`n#` 连掷、`*n` 目标算式）、`.rb`/`.rp`（奖励/惩罚骰）、`.rav`（对抗，名字或数值）、`.sc`（理智） |
| 角色卡 | `.pc list/show/new/use/active/nn/copy/cpy/del/val/meta/export/import/redo/build/stat/clr`、`.st`（查看/赋值/增减/具名宏/派生数值）——角色用名字或 id 引用 |
| 人物作成 | `.coc`/`.coc6d`/`.coc 10`、`.dnd`/`.dndd`（别名由规则书声明） |
| 规则与数据 | `.rule list/validate`（规则书）、`.rule ref`（速查）、`.rule`（规则书查看/编辑/派生）、`.tb`（随机表）、`.ti`/`.li`（疯狂表；`sc`/`ti`/`li` 调用什么由规则书 `command_bindings` 声明） |
| 游戏/团务 | `.game`（游戏/团管理：同一群可并行多场游戏；`join/exit/master/set/call/kick/close/open/over/rou` 管名单、团变量、开关与轮盘骰）、`.ak`（随机抉择）、`.name`（随机姓名）、`.en`（成长）、`.ri`/`.init`（先攻）、`.deck`/`.draw`（牌堆、弃牌堆） |
| 跑团记录 | `.log`（状态）、`.log new/on/off/end/halt`（开始/暂停/继续/结束）、`.log list/get/stat/export/del`、`.log type txt/html`、`.log timer on/off`、`.log time` |

```bash
dicegirl .r 3#1d6 手枪连射        # 连掷三次
dicegirl .r &沙漠之鹰               # 掷角色卡上保存的具名宏
dicegirl ra 心理学+2              # 技能名 + 内联奖励骰
dicegirl ra 困难心理学 60          # 显式难度（关键词来自规则书；另有 极难/自动成功）
dicegirl ra 体质*5                # 目标值算式（区别于奖惩骰）
dicegirl .sc 0/1d10 --apply       # 理智检定（大失败按规则书取最大损失）
dicegirl .sc 0/1d10               # 理智检定
dicegirl st show                  # 查看当前角色
dicegirl st show 心理学            # 查看单个值
dicegirl st psychology65          # 赋值（= 可省略）
dicegirl st san-1d6               # 掷骰增减资源轨
dicegirl st "&沙漠之鹰=1d10+1d6+3"  # 在角色卡上保存具名宏
dicegirl pc list
dicegirl pc show 调查员甲          # 用名字引用角色
dicegirl pc nn 调查员甲 调查员乙    # 重命名
dicegirl pc cpy 调查员乙=调查员甲   # 把一张卡的属性覆盖到另一张
dicegirl pc redo 调查员甲          # 重新生成指定卡的属性
dicegirl pc clr                    # 删除全部角色卡
dicegirl ti                       # 临时疯狂症状表（规则书声明的随机表）
dicegirl name cn 3                # 随机姓名（数据包的姓名表）
dicegirl ak add 苹果 香蕉 橘子     # 随机抉择
dicegirl en 心理学                # 成长检定（成长后直接写卡）
dicegirl rule ref 优势             # 规则速查（rule ref list 列出全部词条）
dicegirl w 5a8                    # 骰池（5 颗 d10，达 8 追加一颗，统计 ≥8 的颗数）
dicegirl ww 5a8                   # 同上，但给出每颗骰子的点数
dicegirl ri +5 首领               # 先攻掷骰（加入先攻列表）
dicegirl init                     # 查看先攻列表（从高到低）
dicegirl deck new "俄罗斯轮盘=有弹|无弹|无弹"   # 定义牌堆
dicegirl draw "俄罗斯轮盘" 2       # 抽牌（不放回，抽出的卡进弃牌堆）
dicegirl deck put "俄罗斯轮盘" 有弹  # 把卡面从弃牌堆放回牌库并重洗
dicegirl game new 恐怖团 --system coc7   # 开团并切换（先攻/牌堆/绑卡/团务按游戏隔离）
dicegirl game join                      # 玩家上桌
dicegirl game set 章节 第三章            # 团变量（GM）
dicegirl game rou 6*3                   # d6 轮盘骰（GM，防刷点）
dicegirl game use 默认游戏              # 切换当前游戏
dicegirl game list / game show / game system <规则书> / game del <名字>
dicegirl log new 第一章                 # 开始跑团记录（记录 @ 机器人的消息与指令结果）
dicegirl log off / log on              # 暂停 / 继续记录
dicegirl log stat / log export          # 统计 / 导出 txt（log type html 可切换）
dicegirl log end                        # 结束并导出日志文件
```

所有指令支持全局参数 `--json`（输出结构化结果，机器人适配器复用同一契约）、`--data-root <目录>`（数据根目录）和 `--seed <整数>`（固定随机种子，调试/复现用）。

## QQ 机器人

适配器位于 [cmd/qqbot](cmd/qqbot)，对接 [QQ 机器人开放平台](https://bot.q.qq.com/wiki/)（官方 API，非 OneBot）。群聊里 @机器人 即可使用全部指令；私聊（C2C）同样可用。

### 注册机器人

使用前需先在 QQ 官方平台注册并创建机器人，拿到 AppID 与 AppSecret：

1. 按官方文档完成[账号注册](https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/getting-started.html#%E8%B4%A6%E5%8F%B7%E6%B3%A8%E5%86%8C)（QQ 机器人开放平台 → 管理后台），并新建一个机器人；
2. 在机器人的管理页面获取 **AppID** 与 **AppSecret**（机器人头像可直接使用项目看板娘 [docs/assets/dicegirl.png](docs/assets/dicegirl.png)）；
3. 把二者填入环境变量（或分发包里的 `bot.env`），启动适配器：

```bash
DICEGIRL_QQ_APPID=<AppID> \
DICEGIRL_QQ_SECRET=<AppSecret> \
DICEGIRL_BOT_DATA_ROOT=botdata \
go run ./cmd/qqbot
```

凭据只从环境变量（或分发包里的 `bot.env`）读取，不要提交到仓库。其它环境变量：`DICEGIRL_BOT_DATA_ROOT`（多租户数据基座，默认 `./botdata`）、`DICEGIRL_BOT_REPLY_UNKNOWN=true`（非指令的@消息也回复提示）、`DICEGIRL_BOT_DEBUG=true`（排障：打印全部网关事件原文、消息丢弃原因与 HTTP 收发明细）、`DICEGIRL_BOT_LOG_FILE`（同时写日志文件）。

> **同一个 AppID 同时只能运行一个实例。** 本地调试与服务器同时运行时，两个网关连接会互相踢下线，并导致同一条消息被重复处理、数据（`botdata/`）各写各的。需要并行环境时请使用第二个机器人 AppID。

平台专属能力（`log` 的生命周期由 CLI 与机器人共享，机器人额外把导出文件发到当前聊天）：

| 指令 | 说明 |
| --- | --- |
| `h 3d6` / `rh .ra 心理学` | 暗骰：结果只私信给投骰者；`rah`/`rch` 是暗骰检定；围观者（`ob join`）也会收到 |
| `drawh <牌堆> [数量]` | 暗抽：走暗骰同一私信通道，群里只显示谁抽了；抽出的卡同样进该游戏的弃牌堆 |
| `ob join/exit/list/on/off` | 暗骰/暗抽围观名单 |
| `pc tag [名字]` / `pc untag [all]` / `pc grp` | **按游戏绑卡**：绑定记在当前游戏上，一游戏一卡，切游戏互不影响；`pc untag all` 解除所有游戏 |
| `log new/on/off/end/halt` / `log list/get/stat/export/del` | 跑团记录：记录机器人收到的群消息与指令结果；`end/export/get` 生成 txt/html 文件并发送到当前聊天，`log type` 切换格式 |

旧版 `logs/*.md` 战报会在群第一次触发机器人时自动导入新的日志库（原文件保留）；导入后统一用 `log` 查看和导出。

数据隔离：每个群一个独立数据根 `<botdata>/groups/<群openid>/`（独立 SQLite），私聊按用户隔离 `<botdata>/c2c/<用户openid>/`；空闲群按 LRU 关闭数据库。**一个群里可以开多个游戏/团**（`game new/use/list`）：各游戏的默认规则书、先攻、牌堆（含弃牌堆）、成员绑卡与团务数据（GM/玩家、变量、轮盘）互相隔离，角色卡全群共享；用 `.coc`/`.dnd` 生成或 `pc new` 新建并自动激活的卡会直接绑到发起人当前所在的游戏。QQ 群事件不提供 `union_openid`，因此首次使用暗骰/暗抽/围观会走一次性绑定码（群里发起、私聊机器人「绑定 <码>」），映射保存在群数据根。

### 分发包

```bash
make package          # 或 go run ./scripts/package
# 产物：dist/dicegirl-<版本>-<os>-<arch>.zip
```

压缩包内是编译好的二进制与 `run.sh` / `run.ps1`、`bot.env.example`；Linux 包另含 `install.sh` / `uninstall.sh`（systemd 部署到 `/opt/dicegirl`，数据在 `/var/lib/dicegirl`）。WSL 内构建 Linux 包：`sh scripts/build-linux-wsl.sh`。

### Linux 服务器部署（systemd）

从 [Releases](https://github.com/inanis-pirata/dicegirl/releases/latest) 下载对应架构的 Linux 包并一键安装：

```bash
wget https://github.com/inanis-pirata/dicegirl/releases/download/v0.1.1/dicegirl-v0.1.1-linux-amd64.zip
unzip dicegirl-v0.1.1-linux-amd64.zip && cd dicegirl-v0.1.1-linux-amd64
sudo ./install.sh
```

`install.sh`（需 root）自动完成：程序装入 `/opt/dicegirl`、创建系统用户 `dicegirl`、数据目录 `/var/lib/dicegirl`（权限 700）、配置 `/etc/dicegirl/bot.env`（已存在则不覆盖）、写入并启用 systemd 服务 `dicegirl.service`（开机自启、崩溃 5 秒自动重启）。凭据未填时脚本会停下来提示。

之后填入机器人凭据并启动：

```bash
sudo nano /etc/dicegirl/bot.env      # 填 DICEGIRL_QQ_APPID / DICEGIRL_QQ_SECRET
sudo systemctl start dicegirl
journalctl -u dicegirl -f            # 实时日志（DICEGIRL_BOT_LOG_FILE 可另写文件）
```

说明：

- 机器人以 `DICEGIRL_DATA_ROOT=/var/lib/dicegirl/data`、`DICEGIRL_BOT_DATA_ROOT=/var/lib/dicegirl/botdata` 运行；自定义规则书放 `/var/lib/dicegirl/data/rulebooks/`，master 名单等机器人配置也写在 `bot.env`（如 `DICEGIRL_BOT_MASTERS`）。
- 无 systemd 的环境（部分容器）脚本会打印手动运行命令兜底。
- 升级：解压新版本包后重新 `sudo ./install.sh` 即可原地覆盖，数据与已填配置保留。
- 卸载：`sudo ./uninstall.sh` 保留数据与配置；加 `--purge` 连数据一起删除。

完整指令与规则书编写指南见 [docs/user-manual.md](docs/user-manual.md)；日常速查也可直接用 `dicegirl help`。

## 骰子表达式语法

```text
stmt   = [number '#'] expr [ ('>='|'>'|'<='|'<') number ]
expr   = term { ('+' | '-') term }
term   = unary { ('*' | '/') unary }
unary  = ('-' | '+') unary | primary
primary= number | dice | '(' expr ')'
dice   = [number] ('d'|'D') (number | '%') [ keep ] [ modifier ]...
keep   = ('kh'|'kl'|'dh'|'dl') [number]   # 数量省略时默认为 1
modifier = ('!' [number])        # 爆骰：达到阈值的骰追加同面骰（阈值省略=最大面）
         | ('r' number)          # 重投：低于阈值的骰重投（次数受安全上限约束）
         | ('c' [cmp] number)    # 计数：本组值 = 满足条件的骰数（cmp 默认 >=）
```

- 基础骰：`d4 d6 d8 d10 d12 d20 d100`，`d%` 等价于 `d100`，省略数量等价于 1（`d6` = `1d6`）。
- 运算：`+ - * /` 与括号，乘除优先于加减；除法为向零取整的整数除法，除零返回 `DIVIDE_BY_ZERO` 错误。乘号也可写作 `X`（`3d6X5` = `3d6*5`）。
- 保留/丢弃：`kh` 保留最高、`kl` 保留最低、`dh` 丢最高、`dl` 丢最低，如 `4d6kh3`、`2d20kl`。
- 通用算子（可组合）：`1d6!` 出 6 追加一颗（`1d6!8` 表示 ≥8 才追加）；`2d6r4` 小于 4 的骰重投；`5d10c8` 统计 ≥8 的骰数（`c>=8`/`c<=8`/`c>8`/`c<8` 亦可）。爆骰与重投都有安全上限（默认每组 100 颗追加、每颗 10 次重投）。
- 重复前缀：`n#<表达式>` 连掷 n 次（n 为 1–10，省略 n 即 `#<表达式>` 为 1 次），如 `3#1d20`；每轮独立掷骰与判定。
- 比较后缀：整条表达式末尾可带 `>= > <= <` 目标值（BCDice 约定），如 `2d6+1>=7`、`3#1d20>=10`，结果附成功/失败；重复时逐轮独立判定。
- 原因文本：`.r` 的其余参数原样收集为原因并回显，如 `dicegirl .r 1d8+2 中型刀伤害`；原因不进入骰子语法。
- 安全上限（默认值，可在代码中通过 `dice.Limits` 调整）：单组 ≤100 骰、整条表达式 ≤1000 骰、面数 ≤1000、嵌套深度 ≤40、重复 ≤10 轮、爆骰追加 ≤100 颗/组、重投 ≤10 次/颗。
- 结果是结构化的：总值之外保留每个骰组、每颗骰子的面值以及是否被保留/丢弃。
- 随机源可注入：生产默认使用 `crypto/rand`；测试与 `--seed` 使用确定性源；单元测试用脚本化序列源，不依赖真实随机结果。

## 目录结构

```text
cmd/dicegirl/      CLI 入口（main：信号、退出码）
  cli/             进程参数解析与装配（在 internal 之外）
cmd/qqbot/         QQ 官方机器人适配器：网关、消息收发、平台专属状态，只做传输翻译
chatcore/          共享聊天语法层：指令解析与文本/JSON 渲染，CLI 与适配器共用
bot/               平台无关的适配器设施：每群独立 data root 的 Manager、LRU、命令锁、执行入口
rulebooks/
  coc7.yaml        内置 CoC 7e 规则书（默认书，go:embed 打包）
  dnd5e.yaml       内置 D&D 5e（2014）规则书（d20 机制，go:embed 打包）
  embed.go         //go:embed *.yaml
datapacks/
  names.yaml       内置数据包（随机姓名等**非规则**内容，go:embed 打包）
  embed.go         //go:embed *.yaml
internal/          可复用核心，不依赖任何界面层
  app/             应用服务：依赖组装、用例编排（CLI 与未来适配器共同的入口）
  engine/          与游戏无关的规则引擎：解释规则书（检定/对抗/资源/伤害/派生/随机表/成长）
    formula/       规则书公式小语言：整数/布尔、白名单函数、加载期类型检查
  dice/            通用骰子引擎：AST、解析器、随机源、结构化结果
  rules/           RuleSystem 接口、Manifest 身份、Registry
  character/       角色卡领域模型、ID 规则、仓储接口
  storage/         持久化契约（接口）；sqlitedb/ 为 SQLite 实现（热数据）
  config/          数据根目录与备份策略配置
  apperr/          稳定错误码与结构化错误类型
  version/         版本号（CLI、机器人、打包共用）
scripts/           打包工具（Go）与 WSL Linux 构建脚本
data/              运行时数据根目录（rulebooks/ 用户规则书、datapacks/ 用户数据包、dicegirl.db 数据库）
docs/              用户手册与示例规则书
tests/integration/ 端到端测试（进程内驱动 cmd/dicegirl/cli）
```

依赖方向单向：`cmd/* → chatcore → internal/app → {engine, rules, character, dice}`；`bot` 供适配器复用。界面层（`cmd`、`chatcore`、`bot`）在 `internal` 之外，`internal` 不导入任何界面包；引擎与通用骰子引擎都不含游戏专属分支，游戏差异只存在于 YAML 数据中。

## 配置与数据格式

- 内置规则书是 [rulebooks/coc7.yaml](rulebooks/coc7.yaml)（`default: true`）与 [rulebooks/dnd5e.yaml](rulebooks/dnd5e.yaml)，编译时 `go:embed` 打包；用户规则书放在 `<数据根>/rulebooks/*.yaml`，启动时与内置书走同一加载/编译路径。规则书 schema 与公式语言详见 [docs/user-manual.md](docs/user-manual.md) 第 11 节。
- 角色卡是 `<数据根>/dicegirl.db` 里的角色卡（YAML 文档，`.pc export`/`.pc import` 是它的交换格式）；本地 CLI 状态（当前角色、默认骰、掷骰统计、游戏列表与当前游戏、先攻、牌堆、跑团记录）也在同一数据库里；更新前备份保留在数据库的 `card_backups` 表。日志导出写在 `<数据根>/logs/`。
- 数据根默认 `./data`，可用 `--data-root` 或环境变量 `DICEGIRL_DATA_ROOT` 覆盖；另支持 `DICEGIRL_BACKUPS`、`DICEGIRL_BACKUP_KEEP`（默认每角色保留 10 份备份）。
- 文件写入采用「同目录临时文件 + fsync + 原子替换」（规则书与数据包等 YAML 文件）；角色卡等热数据走 SQLite 事务。角色 ID 由程序生成（`c-` 加 8 位十六进制），不接受显示名直接作为路径。

## 当前已实现范围

- P0 通用骰子核心（含保留/丢弃、运算、资源上限、可注入随机源）、掷骰语句层（`n#` 重复、比较后缀、原因文本、乘号别名 `X`）与聊天风格指令（`r/rs/rd/ra/rc/rb/rp/rav/sc/st/pc/set/game/rule`，`.。!！` 前缀可省略；`h`/`rh`/`rah`/`rch`/`drawh` 明确报错）。**CLI 只保留指令层**：没有并行的长命令，人物作成的别名（`coc`/`dnd`）由规则书身份头的 `commands` 声明，加游戏不用改 Go
- P1 规则书框架与角色卡 CRUD、复制、导入导出、校验、原子存储与备份；`st 力量60` / `st 力量=60` 赋值（`:` 同义，`=` 可省略）与 `st K±骰子表达式` 增减（含 san/hp/mp 资源轨），`st show <键>`、`st del <键>`、`st clr`
- P2 声明式百分骰引擎（全部由 [rulebooks/coc7.yaml](rulebooks/coc7.yaml) 驱动）：技能/属性检定、成功等级（普通/困难/极难/失败/大失败）、奖励骰/惩罚骰（含 00/0=100 语义，支持 `ra 心理学+2` 内联语法糖与 `rb`/`rp` 入口）、显式难度（`ra 困难心理学`、`ra 极难 心理学 60`、`ra 自动成功心理学`；难度关键词也由 YAML 声明）、**目标值算式**（`ra 体质*5`、`ra --mod -10`，区别于奖惩骰）、规则书同义词（`aliases`）、对抗检定
- P3 声明式扩展：武器伤害与伤害加值表、资源检定（理智损失与**可选**写回、`fumble_loss: max` 大失败静态最大损失、负损失恢复、显式当前值）、派生属性（HP/MP/初始理智/体格/MOV）、生命条与初始属性初始化
- P4 声明式随机表与成长：`tables`（随机表：掷骰选行、行文本可内嵌 `{1d3}` 子掷、`ti`/`li` 掷 CoC7 疯狂表）、`growth_checks`（成长检定，成长后直接写卡）、数据包（`datapacks/`：随机姓名等**非规则**内容）、随机抉择（`.ak`）
- P5 声明式人物作成：`generation`（骰点法属性生成式、CoC6/7 变体、购点法点数池/费率/自动分配）、`.coc`/`.coc6d`/`.coc 10`、`.dnd`/`.dndd`（内置 D&D 5e：4d6 丢最低 / 27 点购点；别名由规则书声明）、`pc redo`；作成后技能加点 `generation.skill_points`（技能点池：`st alloc` 查看/加点/退点/重置，7 版职业 EDU×4 上限 90、兴趣 INT×2 上限 75，6 版惯例 EDU×20/INT×10）
- P6 通用算子与具名宏：骰组修饰符 `!`（爆骰）、`r`（重投）、`c`（成功计数，骰池语义）、`.w`/`.ww` 骰池别名；角色卡上的具名宏（`.st &名=表达式`、`.r &名`，也接受省略 `&` 的名字）
- 机制可插拔：`mechanic.roll` + `meets` 用骰子表达式与布尔公式声明检定（含 `bonus_keep`/`penalty_keep`），引擎只有这一套通用机制、没有内置游戏检定族；成功等级与难度语义随声明适配。内置 D&D 5e（2014）规则书 [rulebooks/dnd5e.yaml](rulebooks/dnd5e.yaml)（d20 表达式、优势/劣势、DC、18 技能、派生数值、随机表、4d6 丢最低与 27 点购点）与示例书 `docs/examples/dnd-lite.yaml`
- P7 游戏与团务工具：游戏/团管理（`.game`，同一群多场游戏并行，默认规则书/先攻/牌堆/当前卡/团务按游戏隔离，角色卡共享）、GM/玩家名单、团变量、暂关/结团、防刷点轮盘骰（`game rou`）、先攻列表（`.ri`/`.init`）、牌堆与抽牌（`.deck`/`.draw`，不放回、牌库/弃牌堆分离、`deck put` 放回、`draw … 放回` 抽后放回、跨进程持久化）
- P8 角色卡扩展：重命名（`.pc nn`）、属性覆盖复制（`.pc cpy`）、指定卡重掷（`.pc redo`/`.pc build`）、清空全部卡（`.pc clr`）
- P9 持久化重构 + 掷骰统计：可写状态整体迁到 SQLite（冷热分工：规则仍是 YAML），`.pc stat` 记录每张卡的检定统计
- P10 跑团记录：`.log` 共享指令，SQLite 按数据根保存；开始/暂停/继续/结束/中止、列表/查看/统计/导出/删除、txt/html 格式与跑团计时；QQ 机器人记录收到的群消息与指令结果并发送文件，CLI 记录指令与输出；旧版 QQ `logs/*.md` 战报在群首次触发时自动导入
- 用户可通过投放 YAML 规则书添加自己的游戏（可声明自己的指令别名），无需改代码（边界见 [docs/user-manual.md](docs/user-manual.md) 11.14 节）
- House Rules：`.rule list/validate/ref` 管理规则书与速查，`.rule show all/sections/show <段> <id>` 查看规则书，`.rule new <新ID> --from <基础书>` 派生新书，`.rule patch --file <JSON>` 应用细粒度修改；修改先编译校验、再持久化并热更新，内置书首次编辑会复制到 `<数据根>/rulebooks/`

聊天平台能力由 QQ 适配器提供：暗骰与围观（含 `.drawh` 暗抽）、按游戏个人绑卡（新建/生成的卡自动绑定当前游戏）、跑团记录与群文件发送（见上文「QQ 机器人」）。在线日志站（上传/回放）、多平台适配器等尚未实现。

规则判定与角色卡写入分离：CLI 只有显式 `--apply`（资源检定）、`--init`（派生属性）或成长检定（成长语义即写回）才会保存状态变更；机器人端按平台惯例默认写回资源检定；人物作成按定义直接建卡。

## 规则假设（需按正版规则书复核）

- 大失败：技能/属性 <50 时 96–100 为大失败；≥50 时仅 100 为大失败。
- 奖励/惩罚骰：相互抵消后，奖励骰取最有利十位、惩罚骰取最不利十位；00+0 读作 100。
- 派生：max HP = ⌊(CON+SIZ)/10⌋，max MP = ⌊POW/5⌋，初始 SAN = 初始幸运 = POW×5（上限 99）；MOV 简化为 7/8/9 三档（未计入年龄/体型细则）。
- 对抗检定：成功等级高者胜；同等级技能值高者胜；同级同值平局，由守秘人裁定。
- 成长检定：掷 1d100，结果**高于**当前技能值才成长（7e 规则：技能越高越难提升），成长量 1d10、技能上限 99。射击（手枪）兴趣点上限 50、驾驶（特殊）兴趣点上限 75 是人物作成时的点数分配限制，并非成长上限。
- 人物作成：7e 骰点法 STR/CON/DEX/APP/POW/LUCK = 3d6×5、SIZ/INT/EDU = (2d6+6)×5；购点法点数池 = EDU×20 + INT×10，最低属性 15，四档费率（15–49 每 2 点 1 分、50–79 每 5 点 1 分、80–89 每 5 点 3 分、90–99 每 5 点 7 分），职业技能上限 EDU×5、兴趣技能上限 75——**费率与上限数值请按正版规则书复核**；CoC6 版的「哪三个属性取 2d6+6」由玩家自选，CLI 非交互版按声明顺序固定分配。

CoC 规则文本版权属于 Chaosium；仓库只保留数值表与自写中文说明，公开发布前请自行确认版权边界。
