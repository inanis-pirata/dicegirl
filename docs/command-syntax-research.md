# 外部骰娘指令语法调研与短指令设计（设计文档）

> **现状（2026-10-21 更新）**：CLI 已经**只保留短指令层**——长命令（`roll`/`check`/`character`/`systems`/`config`/`table`/`derived`/`damage`/`opposed`/`growth`/`choice`/`initiative`）已全部删除，本文与 [todo-dice-parity.md](todo-dice-parity.md) 里提到它们的段落只作为历史设计记录。人物作成的别名（`.coc`/`.dnd`）改由规则书身份头的 `commands` 声明，版本数字对应规则书的 `variants`。当前命令表见 [guide.md](guide.md) 第 3 节。
>
> 状态：P0、P1、P2.1 与 P2.2 已实施（2026-10-21）：**通用骰子语法**（`n#` 重复前缀、比较后缀、CLI 原因文本）、**短指令别名层**（`.r/.ra/.rc/.sc/.st/.pc`，`rh` 明确报错）、**P1 三项**（`resource_checks.fumble_loss: max`、检定内联 ±n、`st K±骰子表达式`）、**P2.1 显式难度**（`checks.difficulties`：困难/极难/自动成功，见 6.4）、**P2.2 检定连掷**（`ra 3#心理学`、`3#p`/`3#b2` 简写，见 6.5）均已落地（见 6.1/6.2/6.4/6.5）；其余 P2 及阶段 G 特性尚未实施。本文记录调研结论与落地方案，不代表 CLI 已支持其中全部语法。
>
> 调研动机：dicegirl 最初是 `check --skill 心理学 --bonus 1` 这种长参数风格，而 TRPG 玩家在聊天平台上习惯 Dice!、BCDice 系的短指令（`.ra 心理学`、`CC<=50`）。本文对照事实标准，确定哪些语法值得借鉴、如何在不违反项目架构边界（游戏规则只能是 YAML 规则书；界面层必须在 `internal` 之外）的前提下分阶段引入。

## 1. 调研来源

| 来源 | 版本/位置 | 链接 |
| --- | --- | --- |
| Dice! V2 用户手册 | 手册标注 Dice! 2.6.5 | https://v2docs.kokona.tech/zh/latest/User_Manual.html |
| Dice! 主仓库 | C++，AGPL-3.0，517★ | https://github.com/Dice-Developer-Team/Dice |
| BCDice 命令指南（通用命令） | 在线版 | https://docs.bcdice.org/ |
| BCDice Cthulhu7th 源码 | master 分支 | https://github.com/bcdice/BCDice/blob/master/lib/bcdice/game_system/Cthulhu7th.rb |

Dice! 是中文 QQ 生态的事实标准；BCDice 是日本事实标准（被大量 Discord/QQ 机器人内嵌），两者语法共同构成玩家最可能先验熟悉的约定。

## 2. Dice! 指令盘点

### 2.1 掷骰表达式文法

手册原文文法：

```text
([掷骰次数]#)[骰子个数]d[骰子面数](b[奖励骰个数])(p[惩罚骰个数])(k[取点数最大的骰子数])
```

| 特性 | 示例 | 备注 |
| --- | --- | --- |
| 基础骰 | `xdy`，个数 1–100、面数 1–1000 | 大小写 d/D 均可 |
| 四则运算 | `1d4+2`、`3d6X5`（`X` 与 `*` 同义） | 表达式可多骰组 `1D10+1D6+3` |
| 重复掷 | `3#1d6`、`1d10#`（# 后省略次数=1） | 次数 1–10 |
| 保留最大 | `3d6k2` | 只有 k（取大），无取小/丢弃 |
| 内联奖励/惩罚骰 | `rb2`（奖励 2）、`r3#p`（3 连惩罚检定） | `b/p` 与百分骰模式绑定，`#p` 固定为 d100 |
| 原因文本 | `.r 1d4+2 中型刀伤害` | 原因可在表达式前后任意位置 |
| 暗骰 | `.rh 心理学` | 结果私聊发送 |
| 只出总值 | `.rs1D10+1D6+3` | 隐藏每颗骰明细 |
| 保存表达式 | `.st &沙漠之鹰=1D10+1D6+3` 后 `.r 沙漠之鹰` | 具名宏，存在人物卡上 |
| 超过 20 颗骰 | 自动排序展示 | — |

手册中**没有**：取最低（kl）、丢最高/最低、Fudge 骰、表达式上的 `>= <=` 比较——比较语义由检定指令承担（另有可选 Lua 扩展 `.rdc` 支持阈值参数）。

### 2.2 CoC 检定类指令

| 指令 | 格式 | 语义 |
| --- | --- | --- |
| `.ra` | `.ra [连掷#]技能名 [成功率]` | 技能检定；成功率缺省时从人物卡取 |
| `.rc` | 同上，技能名前可加 `困难`/`极难`/`自动成功` | 指定难度；`自动成功`=非大失败即成功 |
| 修正值 | `.rc体质*5`、`.rc 敏捷-10` | 对卡上技能值做修正，允许 `+ - * /` |
| `.sc` | `.sc 成功损失/失败损失 [当前SAN] [原因]` | 理智检定；当前 SAN 缺省取卡并**自动写回**；**大失败时损失取最大可能值** |
| `.en` | `.en 技能名 [技能值] [原因]` | 成长检定（d100>技能则增长），成功自动写卡；支持 `+1D3/1D10` 自定义增长量 |
| `.ti` / `.li` | 无参 | 临时疯狂症状 / 总结性（长期）疯狂，掷骰查内置表 |
| `.coc` | `.coc [6|7] [d] [数量]` | 调查员生成；`d`=详细模式单人；默认 7e |
| `.setcoc` | `.setcoc [0-6]` | 群级房规：大成功/大失败区间变体；0=规则书，6=对子规则 |

### 2.3 人物卡与团务

- `.st K:V [K:V]…`、`.st hp-1`、`.st san+1D6`（右侧可为掷骰表达式）、`.st show [K]`、`.st del`、`.st clr`。
- `.pc`：每人最多 16 张卡；`new/tag/show/cpy/del/list/grp/build/redo`；`tag` 把卡**绑定到当前群**，私聊时设全局默认。
- `.ri [加值] [名称]` / `.init` / `.init clr`：先攻（默认 d20，高到低排序，群内生效）。
- `.w`/`.ww`：W-Wing 风格骰池（d10，≥8 成功，达到加骰阈值追加）。
- `.draw`/`.deck`：不放回抽牌与牌堆管理；`.ak`：随机投票/抉择；`.name`：随机姓名；`.log`：战报记录。
- `.ob join/exit/list/on/off`：暗骰围观机制（群聊）。
- `.bot/.group/.dismiss/.welcome/.jrrp/.reply`：QQ 群管与运营功能。
- 扩展（非内置，Lua 脚本）：`.rdc`（D20 双骰优势/劣势）、`.rdx`（Double Cross）、`.rsr`（暗影狂奔骰池）、`.rnc`、`.rfc`、`.rbh`、`.rws`。

### 2.4 明确不借鉴的部分

- QQ 群管/授权/日志染色等平台运营指令。
- `.setcoc` 式内置房规编号开关：房规差异应通过规则书变体（另一份 YAML 或未来的 override 机制）表达。
- 修正值运算的怪异优先级（手册：乘 → 加减 → 除）。dicegirl 保持标准数学优先级。
- `k` 只有保留最大：dicegirl 现有 `kh/kl/dh/dl` 是超集，不降级。
- 每个扩展系统一个硬编码指令族（`.rdx/.rsr/…`）：这正是「系统包」反模式，与本项目架构冲突。

## 3. BCDice 盘点

### 3.1 通用命令（docs.bcdice.org 索引实证）

| 形式 | 含义 | 示例 |
| --- | --- | --- |
| `nDm` + 算术 + 比较 | 表达式尾部直接带阈值 | `2D6+1>=7` |
| 保留 | 大写 KH/KL | `5D6KH3` |
| 巴拉巴拉开骰 | 每颗骰独立判定 | `5B6` |
| 重投/爆骰 | `2R6[>3]>=5`、`2R6>=5@>3` |
| 上限无限骰 | `2U6[4]>=10` |
| 计数骰 | 达阈值计成功数 | `8TY6`、`7TZ10` |
| D66 | `D66`/`D66A`/`D66D`/`D66N` | 十位+个位读法变体 |
| 纯计算 | 前缀 c | `c1+2*3/4` |
| 随机选择 | `choice[A,B,Z`]、`choice(A,B,Z)`、`choice A B Z` |
| 重复 | `x5 2D6`（`rep5`/`repeat5`） |
| 暗骰 | 机器人侧约定（通用指南有「secret dice」专节，具体前缀以各适配器为准） | — |

### 3.2 CoC7 命令（Cthulhu7th.rb 源码实证）

`register_prefix('CC', 'CBR', 'FAR', 'BMR', 'BMS', 'FCL', 'FCM', 'PH', 'MA')`，其中 `CC/CBR/FAR` 大小写不敏感：

- `CC`：正则 `^CC([-+]?\d+)?(?:<=(\d+)([RHEC])?)?$`
  - 目标值可选：`CC` 只掷 1D100；`CC<=30`
  - 奖励/惩罚数紧跟命令：`CC2`、`CC+2`、`CC-1`（帮助文本写 `CC(2)`，但现行正则不接受括号形式）；绝对值 >100 报错
  - 难度字母后缀：`R` 普通 / `H` 困难（目标÷2）/ `E` 极难（目标÷5）/ `C` critical（目标置 0），如 `CC1<=60h`；指定难度后只报成败
  - 无难度后缀时输出五档：fumble/fail/regular/hard/extreme，骰值 1 为 critical
  - 大失败阈值：目标 <50 为 96+，否则仅 100——与 coc7.yaml 现有声明一致
- `CBR(50,20)`：一次百分骰同时比两个目标值，输出 成功/部分成功/失败
- `FAR(w,x,y,z,d,v)`：全自动射击（命中数/穿透/剩弹）
- `BMR/BMS`：实时/摘要式疯狂发作；`FCL/FCM`：施法失败表；`PH/MA`：恐惧/躁狂样本表（均为掷骰查内置表）
- 注意：现行源码中**没有** `CCB/FTB/GBR/FUMBLE/RES`（旧资料中的这些命令已不在该类里）。

BCDice 每个游戏系统一个 Ruby 类（`BCDice.game_system_class("Cthulhu7th").eval("CC<=25")`）——这是按系统写代码的组织方式，dicegirl 明确不采用；只借鉴用户可见的语法约定。

## 4. dicegirl 现状差距矩阵

| 能力 | Dice! | BCDice | dicegirl 现状 | 差距类型 |
| --- | --- | --- | --- | --- |
| 基础骰/四则/d% | `.r` | `nDm` | `r`/`roll` ✅ | 已实施（P0） |
| 保留/丢弃 | `k` | `KH/KL` | `kh/kl/dh/dl` | 有（超集） |
| 重复掷 | `n#` | `x5` | `n#`（1–10 轮）✅ | 已实施（P0） |
| 比较后缀 | 无 | `>=n` 等 | `>= > <= <` 末尾后缀 ✅ | 已实施（P0） |
| 原因文本 | 有 | 无 | CLI 多参数收集回显 ✅ | 已实施（P0，仅 CLI 层） |
| 百分骰检定 | `.ra/.rc` | `CC` | `ra` + `check` ✅ | 已实施（P0 别名层） |
| 检定连掷 | `.ra 3#心理学`、`r3#p` | — | `ra 3#心理学`/`3#p`/`3#b2` + `--repeat` ✅ | 已实施（P2.2，见 6.5） |
| 奖励/惩罚骰 | `b/p` 内联 | `CC+2/CC-1` | `ra 心理学+2` ✅ / `--bonus/--penalty` | 已实施（P1 内联语法糖） |
| 显式难度 | `困难/极难` | `r/h/e/c` 后缀 | `checks.difficulties` + `ra 困难心理学` / `--difficulty` ✅ | 已实施（P2.1，见 6.4） |
| 理智/资源检定 | `.sc a/b` | （无） | `sc`/`rc` + `resource` ✅ | 已实施（P0 别名层） |
| 大失败最大损失 | 有 | — | `fumble_loss: max` ✅ | 已实施（P1，见 6.2） |
| 属性掷骰改值 | `.st san-1d6` | — | `st K±骰子表达式` ✅ | 已实施（P1） |
| 目标值算式 | `.rc 体质*5`、`.rc 敏捷-10` | — | `ra 体质*5`、`ra 心理学/2`、`--mod -10` ✅ | 已实施（P2.3，见 6.6） |
| 资源检定当前值/原因/恢复 | `.sc 0/1 70`、`.sc 1d10/1d100 直面外神` | — | `sc`/`rc` 同形 + `--current` ✅ | 已实施（P2.4，见 6.6） |
| 裸掷骰/精简掷骰/乘号别名 | 裸 `.r`、`.rs`、`X` 乘号 | — | 裸 `r`、`.rs`、`3d6X5` ✅ | 已实施（P2.5，见 6.6） |
| `st` 残余操作/同义词 | `.st show K`、`.st del`、`.st clr`、`:` 分隔 | — | 全部实现 ✅ | 已实施（P2.6，见 6.6） |
| 规则书同义词 | — | — | `aliases`（YAML 数据） ✅ | 已实施（P2.6，见 6.6） |
| 对抗 | 无统一指令 | 无 | `opposed --target --vs` | 我们独有，保留长指令即可 |
| 武器伤害+DB | 宏 + `.r` | 无 | `damage --expr` | 有 |
| 派生/初始化 | 生成时自带 | 无 | `derived --init` | 有 |
| 成长检定 | `.en` | 无 | `en` + `growth_checks` ✅ | 已实施（P3.1，见 6.7） |
| 随机表（疯狂等） | `.ti/.li` | `BMR/PH/MA` | `tables` + `table roll` ✅ | 已实施（P3，见 6.7） |
| 随机名 | `.name` | 无 | 数据包 `name_sets` + `name` ✅ | 已实施（P3.2，见 6.7） |
| 随机抉择 | `.ak` | `choice[A,B]` | `choice` + 本地选项状态 ✅ | 已实施（P3.2，见 6.7） |
| 人物生成 | `.coc/.dnd` | 无 | `.coc`/`.dnd` + `generation` ✅ | 已实施（P4，见 6.8；`.dnd` 随内置 dnd5e 规则书落地） |
| 具名宏 | `.st &x=` | 无 | 角色卡 `macros` + `.st &名=表达式` ✅ | 已实施（P5.1，见 6.9） |
| 骰池/爆骰/重投 | `.w/.ww`（W-Wing） | R/U/T 记号 | 通用算子 `!`/`r`/`c` + `.w/.ww` ✅ | 已实施（P5.2，见 6.9） |
| 多卡 | `.pc`（按群绑定） | — | `pc list/show/nn/cpy/redo/build/clr` + `character` 全套（无群绑定） | 群绑定留给适配器；`.pc stat` 待持久化设计 |
| 先攻/牌堆/投票 | `.init/.deck/.ak` | — | `ri/init` + `deck/draw` ✅ | 已实施（P6，见 6.10） |
| 暗骰/围观 | `.rh/.ob` | S 前缀 | `rh` 已识别并报「需要聊天平台适配器」 | 投递通道留给适配器阶段 |
| 房规切换 | `.setcoc` | — | 无 | 用规则书变体替代，不做开关 |

## 5. 设计原则

1. **语法是界面问题。** 短指令解析器放在 `cmd/` 下（与 `cmd/dicegirl/cli` 同层或其子包），解析后调用 `internal/app` 既有结构化用例；`internal/` 不出现任何短指令/前缀知识。未来机器人适配器与 CLI 共享该解析器。
2. **通用语法增强才进 `internal/dice`。** 重复、比较、原因文本与游戏规则无关，属于骰子表达式语言本身；成功等级语义仍只来自规则书。
3. **规则差异只能进规则书 YAML。** 大失败最大损失、显式难度推导这类 CoC 规则差异，一律通过扩 `engine` schema 表达，不允许在短指令层写死 `/2`、`/5`。
4. **不破坏既有契约。** 长参数风格继续可用；`--json` 输出只增字段不改语义；错误码复用现有集合（语法失败仍为 `PARSE_ERROR`）。
5. **不追求逐字兼容。** 借鉴玩家高先验的核心形式（`r/ra/sc/st`、`n#`、`CC±n<=目标`、比较后缀），平台运营与房规指令明确不做。

## 6. 分期方案

### 6.1 P0：通用掷骰语法 + 短指令别名（零规则耦合）

**internal/dice 语法扩展（已实施，2026-10-21）：**

- 重复前缀：`n#<expr>`，n ∈ 1..`Limits.MaxRepeat`（默认 10，与 Dice! 上限一致；`#expr` 省略次数视为 1）；入口 `Roller.RollStatement` / `dice.ParseStatement`，返回 `BatchResult{Repeat, Rolls []*RollResult}`，每轮重新解析、AST 相互独立。JSON 仅在 n>1 时输出批量容器 `{expression, repeat, reason?, rolls}`，单轮保持扁平 RollResult 契约。
- 比较后缀：`<expr>>=n`、`>n`、`<=n`、`<n`（BCDice 约定，置于整条表达式末尾，目标为整数，可为负），结果在 `RollResult.Compare *Comparison{op, target, success}`；重复时逐轮独立判定。与规则书公式语言中的比较无关，互不影响。纯表达式入口 `Roller.Roll`/`dice.Parse`（规则书 YAML 作者填写的伤害/损失表达式走这条路径）拒绝 `#` 与比较后缀。
- 原因文本：CLI 层把第一个位置参数之后的其余参数原样拼接为 reason（不进骰子 AST/包内结构），文本输出尾部（单轮）或标题（多轮）回显，JSON 以 `reason` 字段携带。

**短指令别名层（已实施，2026-10-21，位于 `cmd/dicegirl/cli/short.go`）：** 实际解析规则

- 兼容 Dice! 的前缀字符 `.。!！`，前缀可省略（CLI 直接给 `r 1d6` 也成立）；点号风格与现有子命令风格共存，分派时先识别短指令；带前缀的未知指令按「未知短指令」报错（退出码 2），不落入长指令解析。
- 实际映射：

| 短指令 | 等价长指令/用例 |
| --- | --- |
| `r [n#]<expr> [原因]` | `roll` |
| `ra [技能/属性名] [目标值] [+n/-n]` | `check`（无名称只有数字=`--target`；±n 映射 bonus/penalty；名称后缀形式 `心理学+2` 与独立 token `心理学 +2` 等价） |
| `sc <成功损失>/<失败损失> [--apply]` | `resource sanity`（斜杠两侧即 success/failure loss） |
| `rc <规则书内检定名> <成功>/<失败> [--apply]` | `resource`（通用形式） |
| `st show` / `st K=V [...]` / `st K±表达式 [...]` | `character show` / `character set` / app 新增 `AdjustValues` 用例 |
| `pc list` / `pc show [id]` | `character list` / `character show` |
| `rh` | 识别但报错：暗骰需要聊天平台适配器 |

- 资源检定短指令不写死 `sanity`：通用形式 `rc <规则书内检定名> <成功>/<失败>`，`sc` 只是 `rc sanity` 的别名（Dice! 先验），具体 id 仍由规则书 `resource_checks` 声明；`--bonus/--penalty/--system/--character` 均可用。
- token 消歧：独立 `+n`/`-n` 一律是奖励/惩罚骰数（先于无符号整数目标值匹配）；损失对必须恰好一个 `/` 且两侧非空；`st` 不得混用 `=` 与 `±`。
- app 层为短指令新增两个用例：`ResolveCheckName`（自由名称解析为技能/属性规范 ID）与 `AdjustValues`（右侧骰子表达式每条掷一次，走既有校验/备份/原子更新路径）。`internal/` 不含任何前缀或别名知识。

### 6.2 P1：规则驱动的小增强（需扩 schema）——已实施（2026-10-21）

- **resource_checks.fumble_loss**（可选，✅）：取值 `normal`（默认）或 `max`。`max` 表示大失败时失败损失取**静态**最大可能值——固定值即其本身，骰子表达式取全部保留骰面之和（`1d100`→100、`1d4+1`→5、`4d6kh3`→18）；由新增的 `dice.ExprBounds` 对纯表达式求上下界，不掷骰、不消耗随机数。coc7.yaml 的 sanity 声明为 `fumble_loss: max`；rulebook 编译期校验取值合法（非法值 `CONFIG_INVALID`），结果 JSON 在该路径标记 `fumble_loss_max: true` 且不产生损失掷骰节点。规则书无声明时行为不变。
- **检定内联奖励/惩罚**（✅）：`ra 心理学+2`、`ra 心理学-1`（同时对齐 Dice! `b/p` 与 BCDice `CC±n` 两种先验），也接受独立 token `ra 心理学 +2`、`ra -1`。纯界面层语法糖，映射现有 CheckInput。
- **`.st K±骰子表达式`**（✅）：右侧为任意合法纯骰子表达式（整数也行，如 `st STR+1`、`st san-1d6`），app 层 `AdjustValues` 对每条掷骰一次，解析属性/技能/资源轨后并入既有校验、备份、原子更新路径；同一调用内不得与 `K=V` 混用。

### 6.3 P2 以后：需要较大 schema 或阶段 G 特性

- **成长检定**：`resource_checks` 同构地新增 `growth_checks`（掷骰比较 + 成功时按表达式增长技能并写卡），对应 `.en`。
- **随机表**：规则书可声明表（id、掷骰表达式、行结果），`ti/li` 只是 `table roll <id>` 的别名；届时 BMR/PH/MA 一类表全部数据化进 coc7.yaml。
- **宏/具名保存表达式**：阶段 G，存角色 metadata 或独立宏存储。
- **暗骰/围观、先攻、多卡群绑定**：随机器人适配器与团务阶段设计，CLI 不预埋。
- **人物生成（.coc）**：需要规则书声明属性生成方式（3d6×5 等）与技能分配规则，schema 复杂度高，最后考虑。

### 6.4 P2.1：显式难度——已实施（2026-10-21）

规则书新增可选 `checks.difficulties` 列表，每条二选一：

```yaml
checks:
  difficulties:
    - { id: extreme, name: "极难",     expr: "target/5" }
    - { id: hard,    name: "困难",     expr: "target/2" }
    - { id: auto,    name: "自动成功", auto: true, success_level: regular }
```

- **公式难度**：`expr` 是只可见 `target` 的整型公式，判定前改写目标值。最终语义与 6.3 初稿的「改完走正常分级」不同：大失败区间跟随**改写后**目标（BCDice 约定，99 的困难档 49，96–100 大失败）；成功档位按**基础目标**的等级梯读取，达不到阈值的骰值降为兜底失败（保留原始档位标签）。
- **自动成功**：不改目标，仅判基础目标上的大失败，其余骰值映射到 `success_level`（必须引用已声明的非大失败等级）。
- 界面：`ra 困难心理学` / `ra 极难 心理学 60` / `ra 自动成功心理学`（关键词前缀或独立 token，长名优先匹配），长命令 `check --difficulty hard|困难`；关键词集合完全来自规则书声明，引擎与 CLI 无硬编码中文。JSON 新增纯增量字段 `difficulty_id`、`difficulty_name`、`base_target`（仅显式难度时出现）。
- 校验：加载期 `CONFIG_INVALID`（id/name 重复、expr/auto 冲突或缺一、expr 引用非 target 变量、success_level 未知或为大失败级）；运行期未知关键词 `INVALID_ARGUMENT`，改写后目标超出 `limits.target` 同样 `INVALID_ARGUMENT`；未声明该段的规则书剥离不到关键词、也不接受 `--difficulty`。

## 6.5 P2.2：检定连掷 `n#`——已实施（2026-10-21）

- 引擎新增 `Book.CheckMany`（`CheckBatchInput{CheckInput, Repeat}`，上限 `MaxCheckRepeat = 9`）：目标值只解析一次，逐轮调用既有 `Check`，每轮独立掷骰与独立分级（显式难度、奖惩骰语义逐轮复用）。
- app 新增 `RunCheckBatch`，长命令 `check --repeat N`。
- 短指令 `ra 3#心理学`、`ra 3# 心理学 60`、Dice! 简写 `ra 3#p 心理学` / `ra 3#b2 心理学 60`（`b`=奖励、`p`=惩罚，省略颗数=1）；`1#` 扁平化为普通单次输出，`0#` 与超上限均退出码 2。
- JSON 新增 `kind:"check_batch"` 容器（`repeat`/`checks[]`），单次契约不变；纯掷骰 `r 3#1d6` 走语句层，互不影响。

## 6.6 P2.3–P2.6：目标值算式、资源检定补齐、掷骰便捷性、`st` 残余——已实施（2026-10-21）

- **P2.3 目标值算式**（区别于奖惩骰）：Dice! 的 `.rc 体质*5` 改的是**目标值**，而已发布的 `心理学+2`/`-1` 后缀按 BCDice 约定=奖惩骰（有测试与文档锁定，不改）。因此分工为：名称后缀 `*n`/`/n` = 目标值算式，`--mod ±n` = 目标值加减。解析顺序是先剥离 `±n`（奖惩骰）再找 `*n`/`/n`（算式），二者可叠加。引擎新增 `engine.TargetAdjust{Multiply, Divide, Add}`，在难度改写**之前**作用于解析出的目标值；规则书新增可选 `limits.computed_target`（省略时等同 `target`）约束算式结果，coc7 声明 0–999 以支持 60×5=300。JSON 纯增量字段 `target_original`/`target_adjust`；越界 `INVALID_ARGUMENT`。
- **P2.4 `.sc`/`.rc` 补齐**：可选的显式当前值（`.sc 0/1 70`、`resource sanity --current 70`）只改本次检定目标、不写轨，变更建议仍以轨道值为准；损失对之后的参数收作原因文本（JSON 追加 `reason`）；损失规格接受**带符号**值（`-1`、`-1d4` 表示恢复 SAN，`after = before - loss`），引擎先按 `clamp` 与 `range` 收敛、应用层再按轨道 `max` 夹取，恢复不会超过上限。
- **P2.5 `.r` 便捷性**：裸 `r` 掷默认骰（内置 d100），`.set [面数]`（1–1000）写入 `state.yaml` 的 `default_die_sides`（角色 metadata 优先），无参 `.set` 恢复默认；`.rs` 精简模式只出总值（JSON 追加 `detailed:false`，结构不变）；超过 20 颗骰的骰组按面值升序展示（Dice! 渲染约定）；骰子词法层把中缀位置单独的 `X`/`x` 当 `*`（`3d6X5` = `3d6*5`）。
- **P2.6 `.st` 残余与同义词**：`st show <键>`、`st del <键>`、`st clr`（保留卡与 ID，写入前自动备份）；`:` 作为 `=` 的同义词；规则书属性/技能新增可选 `aliases`（YAML 数据驱动，resolve 顺序 规范 ID → aliases → 中文名，别名重复/遮蔽在加载期 `CONFIG_INVALID`）。

## 6.7 P3：随机表、成长检定、随机抉择与随机名——已实施（2026-10-21）

- **随机表**（`tables`）：规则书新增可选段。`roll` 是纯骰子表达式并决定表的定义域；`rows` 的 `range` 支持单点、区间与兜底（`"default"`，必须是最后一行）；行文本可内嵌 `{1d3}` 子掷占位符，命中该行时掷骰并替换成总值。编译期校验行不重叠、不越界、覆盖整个定义域（`CONFIG_INVALID`）。长命令 `table roll <id>` / `table list`，短指令 `ti`（`temporary_insanity`）、`li`（`summary_insanity`）；别名在 cmd，表 id 与内容在 coc7.yaml。掷表不需要角色卡。
- **成长检定**（`growth_checks`）：`default` 给 `condition`（布尔公式，仅可见 `roll`/`target`）、`gain`（骰子表达式）、`cap`；`overrides` 按已声明的技能/属性覆盖（省略字段继承默认）。条件方向按 CoC 7e 规则取 `roll <= target`（TODO 草案写的是 `roll > target`，方向与规则相反，已在 TODO 文档记录该决策）。成长后**直接写卡**（成长语义即写回，仍走备份/原子更新/校验）。短指令 `en`，长命令 `growth`。
- **数据包**（`datapacks/*.yaml`，内置 `names.yaml`）：随机姓名等**非规则**内容不进规则书，改放数据包；数据包与规则书共用随机表 schema，另有 `name_sets`（把若干表组合成一个名字，`parts` 支持表与字面量，`default` 标记默认姓名集）。短指令 `name [cn|jp|en] [n]`。
- **随机抉择**（`.ak`）：一轮选项的增删与等概率抽取，选项存本地 `state.yaml`（`choice_options`），`get` 抽取后清空（结束本轮）。短指令 `ak`，长命令 `choice`。

## 6.8 P4：人物作成 `.coc`——已实施（2026-10-21）

手册原文（2.6.5）：`.coc([7/6])(d)([生成数量])`——`.coc 10` 默认生成 7 版人物；`.coc6d` 中 `d`=详细作成且一次只能一个；Dice! 的 `.coc` **只做骰点法**（购点法属「变体规则」，见 `.rules 创建调查员的其他选项`）；手册示例用 `.coc7 10` 实现「roll 十选一」；`.pc build/redo` 负责「按模板填充/清空后重掷」，`.pc new` 无参时给随机姓名卡。

- **schema**：规则书新增可选 `generation` 段。`characteristics` 是骰点法（每个属性一条骰子表达式，可选 `min`/`max` 夹取）；`variants` 声明同游戏的其它版本（CoC6：三个属性 `(2d6+6)*5`、其余 `2d6*5`）；`point_buy` 是购点法（属性表、最低值 15、点数池公式 `EDU*20 + INT*10`、四档费率、自动分配 `priorities`、技能上限 `EDU*5`/75）；`default_variant` 指定默认版本。
- **引擎**：`Book.Generate(ctx, GenerationInput{VariantID, Count, Detailed})` 产出属性值与购点法报告（预算/花费/余额/各属性花费/技能上限）；引擎不写存储。
- **界面**：`.coc [版本] [d] [数量]`——版本与 `d` 写在命令名后（`.coc6d`、`.coc7 10`），数量是单独参数（`.coc 10`，对齐手册），`d` 一次只能一个；长命令 `character generate`；生成物是普通角色卡。
- **取舍**：CLI 非交互——购点法按规则书声明的优先级自动分配并报告余额，技能点留给玩家用 `st` 分配；CoC6「哪三个属性取高值」由玩家自选，CLI 按声明顺序固定分配（已在 YAML 注释与 README 假设里标明）。
- **`.pc redo`** 随本批：清空当前卡属性/技能后重新生成并重算派生轨；`.pc build`（交互式建卡）不移植。
- **`.dnd`（2026-10-21）**：第二本内置规则书 `rulebooks/dnd5e.yaml` 落地后，`.dnd [d] [数量]` 与 `.dndd` 复用同一条生成路径（4d6 丢最低 / 27 点购点）；检定继续走 `.ra`（`15+1` 优势、`15-1` 劣势、`困难 15` 相对 DC），不需要长命令。
- **别名来自规则书（2026-10-21）**：CLI 只保留短指令层，长命令（`roll`/`check`/`character`/`systems`/`config`/`table`/`derived`/`damage`/`opposed`/`growth`/`choice`/`initiative`）已全部删除；人物作成的别名改由规则书身份头的 `commands` 声明（`coc7.yaml` → `coc`，`dnd5e.yaml` → `dnd`），版本数字对应规则书的 `variants`，用户书可声明自己的别名（如 `pf`），加游戏不用改 Go。

## 6.9 P5：通用算子与具名宏——已实施（2026-10-21）

手册原文：`.st &沙漠之鹰=1D10+1D6+3`（角色卡上保存表达式，`.r 沙漠之鹰` 调用）、`.w(w) [骰子个数]a[加骰参数]`（固定 d10，达加骰参数就加骰一次，最后统计点数达到 8 的骰子数）。

- **通用算子**（骰组修饰符，风格同 `kh/kl/dh/dl`，与游戏无关）：爆骰 `!`（`NdM!`/`NdM!T`）、重投 `r`（`NdMrT`）、成功计数 `c`（`NdMcT`/`c>=T`/`c<=T`/`c>T`/`c<T`）。求值顺序：掷骰 → 重投 → 爆骰 → 保留/丢弃 → 取值；安全上限为 `dice.Limits.MaxExplodeDice`（100/组）与 `MaxRerolls`（10/颗）。计数组的静态上下界为 `[0, 骰数]`。
- **语句层调整**：先尝试把整条输入按纯表达式解析，失败才拆分尾部比较——于是组内比较符（`5d10c>=8`）与语句层比较（`2d6+1>=7`）可以共存，纯表达式入口仍然拒绝比较符。
- **`.w`/`.ww`** 是通用算子的薄别名（`Nd10!Tc8`，成功阈值 8 沿用 W-Wing 约定）：`w` 只出总数，`ww` 给出每颗点数。
- **具名宏**：角色卡新增可选 `macros` 段（`map[string]string`，schema_version 不变）；`.st &名=表达式` 保存（保存时做一次语法校验）、`.st del &名` 删除、`.r 名`（可带 `n#` 前缀）直接掷它；宏在 app 层解析且**不嵌套**；未知宏名报 `INVALID_ARGUMENT`。
- **不做**：`.rdx/.rsr/.rnc/.rfc/.rbh/.rws` 等逐系统指令（系统包反模式）；`.rdc` 用 `2d20kh1`/`kl1` 表达即可（文档给出对照表）。

## 6.10 追加：检定机制可插拔（2026-10-21）

- 动机：引擎原本把百分骰写死（`mechanic.type` 只接受 `percentile`），换机制就得改引擎——与本项目「规则只进 YAML」的原则冲突。
- 实现：`engine.Mechanic` 接口（`ID`/`RollRange`/`Meets`/`Roll`/`Apply`）+ 两族实现：`percentile`（d100，奖励取小、惩罚取大、成功条件「≤ 目标」）与 `d20`（一颗 d20，奖励=优势取高、惩罚=劣势取低、成功条件「≥ 目标」）。规则书用 `mechanic.type` 选族，未知族加载期 `CONFIG_INVALID`。
- 语义适配：成功等级仍是「首个 `when` 为真者胜」（`roll <= target` / `roll >= target` 都合法）；**难度修饰的比较方向由机制决定**（`Meets`），标签仍取基础目标值的等级梯——CoC 的既有行为逐例保持不变。
- 产出：示例书 `docs/examples/dnd-lite.yaml`（d20 机制、DC 检定、优势/劣势、DC 加减难度、能力调整值派生）。

## 6.11 P6：团务工具（本地单用户版）——已实施（2026-10-21）

手册原文：`.ri([加值])([角色名])`／`.ri([表达式])([角色名])`（默认掷 d20 作 DND 先攻；`.ri -1 某pc` 加值；`.ri +5 boss` 加值；参数不以数字或加减号开头时视为自定义表达式；`.ri2DK` 两次取大；`.ri 80 怪物甲` 固定值；`.init` 查看、`.init clr` 清空）、`.deck new/set/show/reset/clr`、`.draw [牌堆] ([数量])`。

- **先攻**：`.ri [±加值|表达式] [名称]`——默认掷 d20；`+5`/`-1` 是加值；`80` 是固定值（不掷骰）；其余是自定义表达式（`2d20kh1`，并把 D&D 简写 `2DK` 翻译成它）。结果加入**本地遭遇状态**（`<数据根>/encounter.yaml`），`.init` 从高到低展示（**平局按录入顺序**）、`.init clr` 清空。长命令 `initiative add/list/clear`。
- **牌堆**：`.deck new 名=卡面1|卡面2`（`::3::市民` 表示三张同名）、`.deck set 实例=公共名`、`.deck set 实例=range 下限 上限`、`.deck show [名]`、`.deck reset 名`、`.deck clr`；`.draw [牌堆] [数量]` 不放回抽取，超出剩余张数报 `INVALID_ARGUMENT`（退出码 2）。剩余队列持久化在 `<数据根>/decks.yaml`（牌堆名是自由文本，不能做文件名，故整集合存一个文件）。
- **不做**：群成员牌堆（`.deck set ... member`）、暗抽（`.draw _名`/`.drawh`）、群级隔离与信任权限——这些属于聊天平台适配器阶段。

## 6.12 P7：角色卡扩展（`.pc` 家族剩余项）——已实施（2026-10-21）

手册 `.pc` 全 9 条的逐条对齐（除群绑定外全覆盖）：

- `.pc nn [新卡名]` → `pc nn <新名>` / `character rename [<id>] <新名>`：只改名字，id 与规则书不变；**名字不可重复**（手册同款约束，违反报 `INVALID_ARGUMENT`/退出码 2）。
- `.pc cpy 卡名1=卡名2` → `pc cpy <目标id>=<来源id>`：来源卡的属性/技能/资源轨/宏覆盖到目标卡，**目标卡的身份不变**；手册的「目标不存在则新建」在我们这里不成立——角色卡 id 由程序生成、不可指定，故两张卡都必须存在（复制到自己也报错）。
- `.pc redo [参数:] 卡名` / `.pc build [参数:] 卡名` → `pc redo [id] [--variant]` / `character redo [<id>] [--variant]`：对指定卡（默认当前卡）按规则书的 `generation` 规则重掷属性并重算派生轨；我们的「模板」就是规则书里的生成规则，故 `build` 与 `redo` 同义。
- `.pc clr` → `pc clr` / `character clear`：销毁全部角色卡，当前角色指针同时清空。
- **`.pc stat` 留待持久化设计后做**：它需要「按卡的掷骰流水」这一新的持久化维度（每次 `check`/`roll`/`en` 用了哪张卡就记一笔），牵涉存储位置、保留期限与统计口径，单独一批。
- `.pc tag` / `.pc grp`（群绑定）与「每人最多 16 张卡」「群默认共用一张初始卡」按既定边界排除（属聊天平台适配器阶段）。

## 6.13 P9：持久化重构（冷热分工）+ 掷骰统计——已实施（2026-10-21）

- **为什么重构**：角色数据是热数据（每次检定都写），YAML 的「读-改-写整个文件」既浪费又会在并发下丢更新；规则书/数据包是冷数据（启动读一次、人工编辑），YAML 正合适。
- **分界线**：`<数据根>/rulebooks/*.yaml`、`<数据根>/datapacks/*.yaml` 仍是 YAML；`<数据根>/dicegirl.db`（SQLite，纯 Go 驱动 `modernc.org/sqlite`，无 cgo）装角色卡、掷骰统计、本地状态、先攻、牌堆与卡备份。`internal/storage` 只定义接口（存储缝），`internal/storage/sqlitedb` 是实现，`internal/app` 只依赖接口。
- **兼容**：角色卡内容仍是 YAML 文档（`cards.body`），`character export/import` 的交换格式不变；旧数据根首次打开时自动导入，原文件移到 `<数据根>/legacy/`，迁移只做一次。
- **`.pc stat`**：只统计检定（技能/属性检定与资源检定），原始掷骰与成长检定不计；`stat_totals`/`stat_keys`/`stat_levels` 聚合 + `stat_history`（每卡最近 50 条）；每次检定一个事务；成功判定用规则书声明的成功门槛；终身累计，`pc stat clr` 重置。

## 7. 不做清单（明确记录）
- QQ 群管、授权、欢迎语、jrrp、战报染色等平台运营指令。
- `.setcoc` 式房规编号开关（用规则书变体替代）。
- BCDice 式「每个游戏系统一个代码类/一个指令前缀族」。
- Dice! 修正值的非标准运算优先级。
- 降级现有 `kh/kl/dh/dl` 到只有 `k`。

## 8. 参考

- Dice! V2 用户手册：https://v2docs.kokona.tech/zh/latest/User_Manual.html
- Dice! 炼骰手册/骰主手册入口：https://v2docs.kokona.tech/
- BCDice 通用命令指南：https://docs.bcdice.org/
- BCDice 仓库：https://github.com/bcdice/BCDice
