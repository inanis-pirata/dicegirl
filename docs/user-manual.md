# dicegirl 用户手册

dicegirl 是一个本地运行、跨平台的 TRPG 骰子与规则引擎，提供命令行工具（`dicegirl`）和 QQ 机器人适配器（`qqbot`）。两者共用同一套聊天风格指令与同一个解析器，规则全部由 YAML 规则书声明，引擎本身不含任何游戏专属分支。

---

## 目录

1. [快速开始](#1-快速开始)
2. [指令通用规则](#2-指令通用规则)
3. [骰子表达式](#3-骰子表达式)
4. [掷骰指令](#4-掷骰指令)
5. [检定与资源指令](#5-检定与资源指令)
6. [角色卡指令](#6-角色卡指令)
7. [人物作成](#7-人物作成)
8. [先攻、牌堆与团务](#8-先攻牌堆与团务)
9. [随机表、抉择与姓名](#9-随机表抉择与姓名)
10. [规则与系统指令](#10-规则与系统指令)
11. [规则书 YAML](#11-规则书-yaml)
12. [数据与环境变量](#12-数据与环境变量)
13. [QQ 机器人](#13-qq-机器人)
14. [错误码](#14-错误码)

---

## 1. 快速开始

```bash
# 掷骰
dicegirl .r 2d6+3
dicegirl .r "4d6kh3"
dicegirl .r "3#1d20>=10"          # 连掷 3 轮，逐轮判定 >=10
dicegirl .r "1d8+2" 中型刀伤害     # 之后参数作为原因文本回显

# 用 CoC 7e 规则书建卡并检定
dicegirl .coc                      # 4d6×5 快速作成一张 CoC 角色卡（设为当前角色）
dicegirl .ra 心理学                 # 技能检定
dicegirl .ra 困难 心理学            # 困难难度
dicegirl .ra 心理学+2               # 奖励骰
dicegirl .sc 0/1d10 --apply        # 理智检定并写回卡

# 用 D&D 5e 规则书
dicegirl .dnd                      # 4d6 丢最低生成 D&D 角色卡
dicegirl .dndd                     # 27 点购点
dicegirl .ra 15                    # d20 vs DC 15
dicegirl .ra 15+1                  # 优势
dicegirl .ra 15-1                  # 劣势

# 游戏（团）与规则书
dicegirl .game new 恐怖团 --system coc7   # 新建并切换游戏
dicegirl .game list                       # 列出全部游戏
dicegirl .rule sections                 # 查看规则书各段
```

CLI 没有并行的长命令层：所有操作都写成 `dicegirl <指令> [参数]`，首字符的 `.`、`。`、`!`、`！` 可省略。`help` 与 `version` 是仅有的两个非指令入口。

---

## 2. 指令通用规则

### 2.1 分词与原因文本

- 一行输入按**空白字符**切分为 token（连续空白算一个），没有引号转义机制。
- 「原因」「日志名称」「先攻名称」「游戏名」这类自由文本由命令把**尾部所有 token 用空格拼回**，直接写即可：`r 1d20 偷袭石像鬼`。
- 含 `|`、空格等 shell 元字符的参数在命令行里需要自己加引号：`dicegirl .deck new "扑克=A|K|Q"`。

### 2.2 指令前缀（可选）

指令名第一个 token 前可加前缀标点（`.`、`。`、`!`、`！`，可省略；连续多个也会被去掉）。

```
r 1d6    .r 1d6    。r 1d6    !r 1d6    ！r 1d6    完全等价
```

指令名和第一个参数之间的空格也可以省略：`.r1d10` 等价 `.r 1d10`，`.ra心理学+2` 等价 `.ra 心理学+2`。匹配时优先最长指令名（`.drawh命运` 拆成 `drawh` + `命运`）。

**带不带前缀的行为差异**（仅对未知指令名有影响）：

- 不带前缀且无法识别 → 「未识别」；机器人默认静默（设置 `DICEGIRL_BOT_REPLY_UNKNOWN=true` 才提示），CLI 报「未知指令」并退出码 2。
- 带前缀但未知（如 `.xyz`）→ 一律按指令处理并返回用法错误（退出码 2）。

### 2.3 大小写

- 指令名、子命令名：小写英文；部分有中文别名。
- 骰子表达式大小写不敏感：`d` 与 `D` 相同；裸字母 `x`/`X` 当乘号；`2DK` 与 `2dk` 相同。
- 角色名匹配支持忽略大小写。

### 2.4 选项（`-flag`）

Go 风格长选项，可以写在位置参数的**前面、后面或中间**：

- `--system coc7`、`--system=coc7` 两种写法等价。
- 布尔选项用 `--active=false` 关闭。
- 可重复选项：`pc new 张三 --set STR=50 --set CON=60`。
- 独立的 `-1`、`+2` 这种**带符号整数永远是位置参数**（如 `ra` 的奖励/惩罚骰），不会被当成选项。

### 2.5 退出码

| 码 | 含义 |
| --- | --- |
| `0` | 成功 |
| `2` | 用法/参数错误（包括表达式解析错误） |
| `1` | 运行时/领域错误（角色不存在、牌堆为空、重名等） |
| `-1` | 不是指令（仅内部使用，即 `chatcore.Unrecognized`） |

### 2.6 角色引用 `<名字|id>`

凡是参数写作 `[名字|id]` 处，按以下顺序解析：

1. 精确角色 ID（`c-` 开头，程序生成）；
2. 精确角色名（角色名全局唯一，重名在创建/改名时会被拒绝）；
3. 忽略大小写的角色名。

忽略大小写后仍对应多张卡时会报错，要求改用 ID。留空表示「当前角色」。

### 2.7 「当前角色」如何确定

- 机器人：发送者在**当前游戏**绑定的卡（`pc tag`）；没有绑卡时回退到该游戏的当前卡（`pc use`）。
- CLI：取该游戏的当前卡。
- 任何指令上的 `--character <名字|id>` 都可以覆盖这一选择。

---

## 3. 骰子表达式

骰子引擎位于 `internal/dice`，与任何规则系统无关。

### 3.1 语句层

```
语句 = [重复前缀 '#'] 表达式 [比较后缀]
重复前缀 = 正整数           # 1–10；省略数字（#表达式）视为 1
比较后缀 = '>=' | '>' | '<=' | '<' 整数
```

- 重复与比较可同时出现：`3#2d6+1>=7`；比较对每轮总值**分别判定**。
- `#` 只能在开头出现一次。
- 比较后缀判定的是整条表达式的总值；骰组内部的 `c` 计数修饰符见 3.3。

示例：`1d20+5`、`d%`、`3#1d20`、`2d6>=7`、`#d100`。

### 3.2 表达式文法

```
expr    = term { ('+' | '-') term }
term    = unary { ('*' | '/' | 'x' | 'X') unary }
unary   = ('-' | '+') unary | primary
primary = 整数 | 骰组 | '(' expr ')'
骰组     = [整数] ('d'|'D') (整数 | '%') [修饰符]...
```

- 骰组：`NdM`（N 颗 M 面骰）、`dM`（一颗，等价 `1dM`）、`d%`/`d100`（百分骰）。
- 四则运算与括号；乘号还可以写 `x`/`X`（如 `2X3`）。
- 除法向零取整；除零是错误。
- 一元正负号：`-1d4`、`+2`。

### 3.3 骰组修饰符

修饰符跟在骰组后面，可任意顺序出现，但**每种最多一个**（保留/丢弃四种合计也只能有一个）。

| 写法 | 名称 | 语义 | 示例 |
| --- | --- | --- | --- |
| `kh[n]` | keep highest | 保留最大的 n 颗（n 默认 1） | `4d6kh3`、`2d20kh` |
| `kl[n]` | keep lowest | 保留最小的 n 颗 | `4d6kl3` |
| `dh[n]` | drop highest | 丢弃最大的 n 颗 | `4d6dh1` |
| `dl[n]` | drop lowest | 丢弃最小的 n 颗 | `4d6dl1` |
| `!` | 爆骰 | 掷出最大面（M）的骰子再追加一颗同面骰，链式直到不再爆或触顶 | `6d6!` |
| `!<n>` | 指定阈值爆骰 | 点数 ≥ n 即爆 | `10d10!8` |
| `r<n>` | 重投 | 点数小于 n 的骰子重投（单骰最多 10 次） | `4d6r1` |
| `c<n>` | 成功计数 | 骰组的值 = 点数 ≥ n 的骰子颗数（默认比较符 `>=`） | `5d10c8` |
| `c<cmp><n>` | 带比较符计数 | cmp 为 `>=` `>` `<=` `<` 之一 | `5d10c>=8`、`5d10c<3` |

带计数修饰符时，该骰组的值是「成功颗数」而不是点数之和（骰池语义，`w`/`ww` 内部就是用它实现的）。

组合示例：`3d6!r1kh2`、`10d10!10c>=8`。

求值顺序：掷骰 → 重投 → 爆骰 → 保留/丢弃 → 取值（求和或计数）。

### 3.4 安全限制

| 限制 | 默认值 |
| --- | --- |
| 单骰组骰子数 N | ≤ 100 |
| 整条表达式骰子总数 | ≤ 1000 |
| 骰面数 M | ≤ 1000 |
| 括号/运算嵌套深度 | ≤ 40 |
| `n#` 重复次数 | ≤ 10 |
| 爆骰追加骰子 | ≤ 100 |
| 单骰重投次数 | ≤ 10 |

面数、数量、重复次数超限以及除零都是明确错误（退出码 2）。

### 3.5 宏（角色卡上保存的表达式）

- 定义：`st &名=表达式`（也可用 `:`），如 `st &沙漠之鹰=1d10+1d6+3`。
- 名称最长 32 个字符；表达式在保存时即校验。
- 掷骰：`r &沙漠之鹰`；可带重复前缀：`r 3#&沙漠之鹰`。
- 删除：`st del &沙漠之鹰`。
- 宏保存在角色卡上，`r &名` 取**当前角色**卡上的宏。宏不嵌套（宏里的表达式不会再展开宏）。

---

## 4. 掷骰指令

### 4.1 `r` — 掷骰

```
r
r <语句> [原因...]
```

- 无参数：掷默认骰（初始 `d100`；可用 `set` 改）。
- 第一个参数是第 3 节的完整语句（含 `n#`、比较后缀、`&宏`）。
- 其余 token 拼为原因文本，随结果显示。

示例：`r`、`r 3d6+1`、`r 2d6>=7 破门`、`r 3#1d20`、`r &沙漠之鹰 射击`。

### 4.2 `rs` — 精简掷骰

```
rs <语句> [原因...]
```

与 `r` 相同，但只输出总值（必须给语句）。

### 4.3 `w` / `ww` — 骰池（W-Wing 风格）

```
w [个数表达式][a<加骰阈值>]
ww [个数表达式][a<加骰阈值>]
```

- 掷 N 颗 d10：点数 **≥ 8 计为 1 个成功**，输出成功总数（`w`）或每颗点数（`ww`）。
- 个数 N：正整数或能解析出整数的骰子表达式；省略为 1。
- `a<n>`：加骰（爆骰）阈值，n 为 1–10；点数 ≥ n 追加一颗。
- token 间的空格会被忽略，以下写法等价：`w 10a8`、`w10a8`、`w 10 a8`。
- `5a8`、`1d4+1a8`、`w`（1 颗 d10）均合法。

### 4.4 `set` — 默认骰面数与规则书

```
set          # 恢复内置默认（d100）
set <面数>   # 1–1000 的整数
set list     # 列出已加载规则书
set <规则书> # 切换当前游戏的默认规则书（如 set coc7）
```

---

## 5. 检定与资源指令

### 5.1 `ra` — 技能/属性检定

```
ra [选项] [难度] [n#] [技能/属性名] [整数目标值] [±n]...
```

各部分顺序自由（难度与 `n#` 的相对顺序也自由），可全部省略但**至少要给出名字或目标值之一**。

**选项**

| 选项 | 参数 | 说明 |
| --- | --- | --- |
| `--system` | 规则书 id | 不用角色所属规则书时指定 |
| `--character` | 名字\|id | 不用当前角色时指定 |
| `--mod` | 整数 | 目标值修正，在所有算式之后叠加 |

**难度关键词**：由规则书声明（CoC 7e 中为 普通/困难/极难/自动成功 等），接受：

- 独立 token：`ra 困难 心理学`、`ra 困难 心理学 60`；
- 粘在名字前：`ra 困难心理学`；
- 与 `n#` 任意组合：`ra 3#困难心理学`、`ra 困难 3#心理学`。

**连检 `n#`**（检定专用，1–9 轮）：

- `ra 3#心理学` — 连扔 3 次；
- `ra 3# 心理学` — 分开写也可；
- Dice! 简写：`ra 3#p 心理学`（每轮 1 颗惩罚骰）、`ra 3#b2 心理学`（每轮 2 颗奖励骰）；数字省略为 1。

**名字 token 的全部形式**

| 形式 | 含义 | 示例 |
| --- | --- | --- |
| `名称` | 技能、属性或资源轨，目标值取卡面 | `ra 心理学`、`ra 体质`、`ra 理智` |
| `名称+n` / `名称-n` | 名字内联奖励/惩罚骰 | `ra 心理学+2`、`ra 侦查-1` |
| `名称*n` | 目标值 = 卡面值 × n（n 为正整数） | `ra 体质*5` |
| `名称/n` | 目标值 = 卡面值 ÷ n（整除） | `ra 心理学/2` |
| 独立 `+n` / `-n` | 奖励/惩罚骰，可多个累加 | `ra 心理学 +2 +1` |
| 整数 | 显式目标值，只能有一个 | `ra 60`、`ra 心理学 60` |
| `整数±n` | 显式目标值带奖励/惩罚骰 | `ra 60+2` |

约束：显式目标值不能与 `*n`/`/n` 同用；名字最多一个；目标值最多一个。给了显式目标值又给名字时，名字仅作标签，但仍必须能在规则书中解析到。

**名字解析阶梯**（全部来自规则书声明，三类共用）：属性 → 技能 → 资源轨。资源轨的名字由 `derived.vitals` 的 `name`/`aliases` 声明（CoC：`理智`/`理智值`/`san`/`SAN` → san 轨），目标值取轨道**当前值**；卡上还没有该轨时用规则书声明的 fallback 公式（CoC 理智 = POW）。

**目标值算式 vs 奖惩骰**

| 写法 | 含义 |
| --- | --- |
| `心理学+2` / `心理学-1` / `+2` / `-1` | 奖励骰 / 惩罚骰 |
| `体质*5` / `心理学/2` | 目标值算式：卡上的值先乘/除 |
| `--mod -10` | 目标值算式：目标值加一个整数 |

算式在难度修饰**之前**生效：`ra 困难 体质*5` 先把 60 变成 300，再由困难档改写成 150。结果里标注原始值与算式：`目标 300（原始 60，*5）`。

### 5.2 `rb` / `rp` — 奖励骰 / 惩罚骰

```
rb [数量] <检定参数...>    # 奖励骰；rb 侦查、rb2 60
rp [数量] <检定参数...>    # 惩罚骰；rp 侦查、rp2 60
```

数量省略时为 1，最多 9；首个 1–9 的整数在还有其他参数时视为数量，单个整数仍是检定目标。它们等价于在 `ra`/`rc` 参数后追加 `+n`/`-n`。

### 5.3 `sc` — 理智检定（CoC）

```
sc [选项] <成功损失>/<失败损失> [当前值] [原因...]
```

- 损失对：斜杠分隔，两侧非空，各自是**整数或骰子表达式**：`sc 0/1d10`、`sc 1d10/1d100`。
- `[当前值]`：可选的非负整数首参（如 `sc 0/1 70 目睹邪神`）；也可用 `--current <值>` 指定（仅本次生效，不写轨），两种方式只能选一种。
- 选项：`--system`、`--character`、`--apply`（写回角色卡）、`--bonus <n>`、`--penalty <n>`。
- **机器人默认 `--apply`**（平台惯例）；CLI 必须显式加 `--apply` 才改卡。
- 大失败按规则书 `fumble_loss` 取最大损失（CoC 7e 约定）。
- 负损失表示**恢复**：`after = before - loss`。
- `sc` 调用的资源检定由规则书的 `command_bindings` 绑定（CoC → `sanity`）；规则书未声明绑定时该指令报「未声明绑定」而不是猜测。

### 5.4 `rc` — 检定别名

```
rc [选项] <技能/属性名> [目标值] [±n]
```

`rc` 与 `ra` 完全相同（Dice! 的两种写法）；在 D&D 模式下同样走规则书声明的 d20 检定。

### 5.5 `en` — 技能成长检定

```
en [选项] <技能/属性名> [目标值 | 成长量] [原因...]
```

- 第二个位置 token：纯整数 = 显式成长目标值；含 `d`/`D`/`/` 的 token = 自定义成长量骰子表达式（如 `en 幸运 1d10`）；省略则用卡面现值。
- CoC 7e 语义：掷 1d100，结果**高于**当前技能/属性值才成长（技能越高越难提升），成长量 1d10、上限 99；成长成功直接写卡。
- 选项：`--system`、`--character`。

### 5.6 `r ...+db` — 伤害掷骰

```
r <武器表达式>+db [原因...]
```

`db` 在掷骰前替换为角色所属规则书的**伤害加值公式**（如 `1d4`），然后交给通用骰子引擎。没有角色卡时不能用 `db`；不需要伤害加值时直接写普通表达式。

### 5.7 `rav` — 对抗检定

```
rav [选项] <A名或值> <B名或值>
```

恰好两个位置参数，各自是非负整数目标值，或一个可解析的技能/属性/资源轨名（`rav 侦查 60`、`rav 力量 力量`；名字按与 `ra` 相同的阶梯对当前角色解析）。大失败必败（双大失败平）；其余等级声明越靠前越好；同级按规则书的 `tie_break` 处理。

### 5.8 `st derived` — 派生数值

```
st derived [--character <名字|id>] [--system <规则书>] [--init] [--force]
```

- 无位置参数。
- `--init`：把缺失的资源轨（HP/MP/理智轨等）初始化并写卡。
- `--force`：与 `--init` 一起用，强制重置已有轨道。

---

## 6. 角色卡指令

### 6.1 `pc` 总览

```
pc new       手工建卡
pc list      列出全部卡（当前卡带 * 标记）
pc show      查看卡
pc use       切换当前卡
pc active    显示当前卡 id
pc nn        改名
pc copy      复制为新卡
pc cpy       把一张卡的数值盖到另一张
pc del       删除卡
pc val       按规则书校验卡
pc meta      读写卡面元数据
pc export    导出 YAML
pc import    导入 YAML
pc redo      重新随机属性（build 同义）
pc stat      掷骰统计
pc clr       删除全部卡
### 6.2 `pc new` — 手工建卡

```
pc new <名称> [KEY=VALUE ...] [--system <规则书>] [--active=false] [--set KEY=VALUE]...
```

- 名称为第一个位置参数（单个 token）。
- 赋值同时接受裸位置参数与可重复的 `--set`；键经规则书解析为属性/技能；值必须是整数（`STR=50`、`CON:60`，`=` 与 `:` 等价）。
- 默认 `--active=true`：建完即切为当前卡并初始化派生状态轨；属性不全时保留卡，提示以后用 `st derived --init` 补初始化。
- 机器人环境下新建的当前卡会**自动绑定**到发送者（当前游戏）。

### 6.3 其余 pc 子命令

| 形式 | 说明 |
| --- | --- |
| `pc list` | 列表（恰好无参） |
| `pc show [名字\|id]` | 省略 = 当前卡 |
| `pc use <名字\|id>` | 切为当前卡 |
| `pc active` | 打印当前卡 id；没有当前卡时退出码 1 |
| `pc nn <新名>` | 改当前卡名 |
| `pc nn <名字\|id> <新名>` | 改指定卡名 |
| `pc copy <名字\|id> <新名> [--active]` | 复制出新卡；`--active` 同时切换 |
| `pc cpy <目标>=<来源>` | 把来源卡的属性/技能/资源轨/宏覆盖到目标卡 |
| `pc del <名字\|id>` | 删除一张卡 |
| `pc val [名字\|id]` | 按其规则书校验 |
| `pc meta <键> [值]` | 读/写当前卡的一个元数据项；省略值 = 读取 |
| `pc meta <键> [值] --character <名字\|id>` | 指定卡 |
| `pc export [名字\|id] [--file <路径>]` | 省略 `--file` 输出到标准输出；省略 id 导出当前卡 |
| `pc import --file <路径\|-> [--keep-id]` | `-` 从标准输入读；默认重新生成 id，`--keep-id` 保留文件中的 id |
| `pc redo [名字\|id] [--variant <变体id>]` | 重新随机生成属性；`pc build` 同义 |
| `pc stat [名字\|id]` | 查看该卡掷骰统计 |
| `pc stat clr [名字\|id]` | 清空统计 |
| `pc clr` | 删除**全部**角色卡 |

### 6.4 `st` — 当前角色的属性/技能/宏

查看：

```
st                # 整卡（等价 st show）
st show           # 整卡
st show <键>       # 单个属性/技能/资源轨值
```

赋值（值必须整数，可多条；`=`/`:` 可省略，也接受“键 值”分写）：

```
st 力量60 侦查50
st 力量 60 侦查 50
st 力量=60
st STR:50
st san=40          # 资源轨：设当前值（轨道需已存在，见 st derived --init）
```

增减（`±` 后接整数或骰子表达式，可多条）：

```
st 理智-1d6
st HP+2 MP-1
```

赋值与增减对资源轨生效时只改**当前值**；键按与 `ra` 相同的阶梯解析（属性 → 技能 → 资源轨，`理智`/`san`/`SAN` 均可）。

同一条 `st` 中**不能混用赋值与增减**。

宏定义与删除：

```
st &名=表达式     # 也可用 :；保存时校验表达式
st del &名
```

其它：

```
st del <键>       # 删除一个属性/技能值
st clr            # 清空该卡的自定义属性/技能值
```

技能加点（作成后分配技能点，池由规则书 `generation.skill_points` 声明，见 11.13）：

```
st alloc                        # 查看点池（已用/总点、单技能上限）
st alloc 职业 侦查+40 图书馆使用+20   # 加点；不在卡上的技能从规则书基础值起步
st alloc 职业 侦查-10            # 退点（不超过该技能已加点数，回落不低于基础值）
st alloc clr                    # 重置全部加点（技能回落基础值）
```

### 6.5 规则

- `st` 一次调用内不能混用赋值与 `±` 增减；`±` 右侧必须是纯骰子表达式（不接受 `#` 与比较后缀）。
- `st K±...` 依次解析属性（规则书属性）、技能（角色卡已有值）、资源轨（已初始化的 hp/mp/san 等，大小写不敏感）；解析不到即 `INVALID_ARGUMENT`。
- `st alloc` 的点数必须是**纯整数**（不接受骰子表达式）；池按卡的生成变体解析（`coc6` 生成的卡用 6 版池），生成与 `pc redo` 会报告点池并重置加点记录；上限按 cap_overrides → 池 cap → `limits.skill` 依次取用。
- 具名宏保存在角色卡上（`macros` 段），`&` 前缀只存在于界面层。
- 掷骰统计（`pc stat`）只统计**检定**（有目标与成败判定的掷骰：技能/属性检定与资源检定）；原始掷骰 `r` 与成长检定 `en` 不计入。

---

## 7. 人物作成

作成指令名**不是内置的**，由已加载规则书在 manifest 的 `commands` 中声明。

| 别名 | 规则书 |
| --- | --- |
| `coc` | Call of Cthulhu 7e（默认规则书） |
| `dnd` | D&D 5e |

别名 token 的构成：`<别名>[版本数字][d]`，数字与 d 都可省略；也可把版本/数量写成后续参数。

| 输入 | 含义 |
| --- | --- |
| `coc` | 默认版本（7e）快速作成 1 人 |
| `coc7` / `coc6` | 指定版本变体 |
| `cocd` | 默认版本详细（购点）作成 |
| `coc6d` | 6 版购点法 |
| `coc d` / `coc 6 d` | 用独立 token 表达同上 |
| `coc 10` | 一次快速作成 10 人 |
| `dnd` / `dndd` | D&D 5e 快速 / 27 点购点 |

选项：`--system <规则书>`、`--name <名字>`（省略随机姓名）、`--active=false`（默认第一张设为当前卡）。详细作成一次只能生成 1 人。

生成结果是一张**普通角色卡**（属性已写入，技能留空即基础值），后续 `ra`/`st`/`en` 等命令照常可用。`pc redo` 会清空当前卡属性后重新生成并重算生命/理智轨。

作成结果同时附带该生成变体的**技能点池**（如 CoC 7 版：职业技能点 EDU×4、单技能上限 90；兴趣技能点 INT×2、上限 75），用 `st alloc` 分配——技能从规则书基础值起步，1 点 = 技能值 +1，池余量与上限由卡记录跟踪（见 6.4）。

---

## 8. 先攻、牌堆与团务

### 8.1 游戏（团）管理 `game`

“游戏”指同一数据根内的一场游戏/一个团。`game` 采用 Dice! 的团务入口命名。

- **群/私聊隔离**：每个群、每个私聊各自一个数据根和 SQLite，互不影响。
- **游戏内隔离**：当前角色指针、成员绑卡（机器人）、先攻列表、牌堆（含弃牌堆）按游戏隔离。
- **全局共享**：角色卡本身（同一张卡可在不同游戏中被绑定/使用）。

新建数据根自动存在一个 `默认游戏`（id `default`）并作为当前游戏。

```
game                      # 查看当前游戏（含 GM/玩家/团变量/轮盘状态）
game list                 # 列出全部游戏
game show [名字|id]       # 查看指定游戏（state 同义）
game new [团名] [--system <规则书>]   # 开团并自动切换；创建者自动成为 GM
game use <名字|id>        # 切换当前游戏
game system <规则书>      # 设置当前游戏的默认规则书
game del <名字|id>        # 删除（当前游戏不可删）
```

#### 名单与团务

```
game join                 # 玩家上桌（GM 也可以加入玩家名单）
game exit                 # 自己退出；GM 退出同样用这条
game master               # 自任 GM；已经是 GM 时列出 GM 名单
game call                 # 点名全部玩家（GM）
game kick <QQ|名字>       # 把玩家踢下桌（GM）
game close / game open    # 暂时关闭 / 重新展开游戏领域（GM）
game over                 # 结团：清空名单、变量、轮盘、先攻、牌堆与当前卡指针（GM）
```

- 开团会把创建者登记为第一位 GM。之后新增 GM 用 `game master`。
- 团务数据（GM/玩家名单、团变量、开关状态、轮盘）按游戏隔离，与角色卡共享、先攻/牌堆按游戏隔离的规则一致。
- `game over` 只清空当前游戏的名单、变量、轮盘、先攻、牌堆与当前卡指针，不会删除其他并行游戏，也不会删除角色卡。

#### 团变量

```
game set 章节 第三章        # 设置（GM）
game set 章节=第三章        # 同上
game set 章节               # 查看单个变量
game                      # 查看全部变量
```

#### 轮盘骰（防刷点）

```
game rou 20               # 为 d20 启用：1~20 各一份
game rou 6*3              # 为 d6 启用：1~6 各三份
game rou hist             # 查看本轮抽出的点数
game rou reset            # 复原袋子并清空抽取记录（GM）
game rou clr              # 清除轮盘，恢复普通掷骰（GM）
```

轮盘只拦截**单骰单轮**的 `dN` / `1dN` 掷骰（含 `rd N`），把袋中点数不放回地抽给你；
抽空后自动重装。带运算符、奖励骰或 `n#` 连掷的表达式不受影响。`game close` 期间轮盘停用。
面数×份数最多 100。

### 8.2 先攻

```
ri                    # 以默认 1d20 掷先攻并加入当前游戏先攻表
ri [名称...]          # 同上，带名称
ri +5 [名称...]       # d20 + 整数修正（也可 -n）
ri 80 [名称...]       # 直接登记固定值，不掷骰
ri <表达式> [名称...] # 自定义表达式，如 ri 2d20kh1 战士
init | init list      # 查看先攻表（高到低，同分按加入顺序）
init clr              # 清空当前游戏先攻表
```

- 修正只接受整数（`+5`/`-1`）；表达式支持 D&D 简写 `2DK`（= `2d20kh1`，大小写不敏感）。
- 名称为尾部全部 token。

### 8.3 牌堆 `deck`

```
deck new <名>=<卡面1|卡面2|...>     新建牌堆并洗牌
deck set <实例>=<牌堆名>            以另一牌堆的整副卡为模板新建
deck set <实例>=range <下限> <上限>  以整数区间建堆（同一参数内）
deck set <实例>=range <下限>-<上限>  区间写法（聊天推荐）
deck show                          列出当前游戏全部牌堆与两堆数量
deck show <名>                      查看一个牌堆
deck put|return|back <名> <卡面...>  把卡从弃牌堆放回牌库并重洗
deck reset <名>                     弃牌堆合并回牌库并重洗
deck clr|clear                      清空当前游戏全部牌堆
```

建堆规格：

- 卡面以 `|` 分隔，每段一个卡面（空白会被裁掉，空段报错）：`deck new 扑克=A|K|Q|J|10`。
- 重复卡面：`::n::卡面` 表示 n 张：`deck new 六面骰=::6::1|::6::2|...`，也可 `::3::凶`。
- 牌堆名不能与当前游戏已有牌堆重名。
- CLI 中含 `|` 的参数要加引号。

不变量：牌库剩余 + 弃牌堆 = 定义的全部卡（多重集）。

### 8.4 抽牌 `draw` / `drawh`

```
draw <牌堆名> [数量] [放回关键字]
drawh <牌堆名> [数量]                # 仅机器人：暗抽，结果只私信抽牌人
```

- 数量：正整数，省略为 1，最多一个。
- 默认**不放回**：抽出的卡从牌库移到弃牌堆；牌库不够时报错（退出码 2）。
- 放回关键字（任选其一，位置任意）：`放回`、`放回抽`、`back`、`replace` —— 抽后放回，两堆数量不变。
- `deck put` 从弃牌堆取回指定卡面（按多重集校验，缺一张都报错）并立即重洗。

---

## 9. 随机表、抉择与姓名

### 9.1 `tb` — 规则书随机表

```
tb <表id> [--system <规则书>]   # 掷一张规则书声明的随机表
tb list [--system <规则书>]      # 列出可用随机表
```

掷表不需要角色卡。结果含表 id、骰值、命中行文本与子掷明细。

### 9.2 CoC 专用表

```
ti [--system <规则书>]   # 临时疯狂表（temporary_insanity）
li [--system <规则书>]   # 总结性疯狂表（summary_insanity）
```

### 9.3 `ak` — 随机抉择

```
ak new | clr             # 清空选项（new 与 clr 等价）
ak add <选项...>         # 追加一个或多个选项
ak del <选项>            # 删除一个选项（恰好一个参数）
ak get                   # 随机抽一个
ak show                  # 查看当前选项
```

### 9.4 `name` — 随机姓名

```
name                # 1 个默认姓名集名字
name cn | jp | en   # 指定姓名集
name <数量>          # 正整数数量
name cn 5           # 数量与姓名集顺序自由；姓名集最多一个
```

---

## 10. 规则与系统指令

### 10.1 `rule ref` — 规则速查

```
rule ref <词条...>          # 检索（token 用空格拼回）
rule ref <系统>:<词条>      # 限定规则书，如 rule ref coc:大失败（中文冒号 ：也可）
rule ref list              # 列出全部词条
rule ref set <系统>         # 设定优先查询的规则书
rule ref set               # 清除偏好
```

### 10.2 `rule list` / `rule validate` — 规则书管理

```
rule list                     # 列出已加载规则书
rule validate                 # 校验全部规则书与数据包
rule validate --system <规则书>
```

### 10.3 `rule` — 规则书查看与编辑（House Rules）

```
rule show all [--system <规则书>]          # 显示整本规则书 YAML
rule show <段> <id> [--system <规则书>]    # 显示某一段的 YAML
rule show mechanic|opposed [--system]      # 显示机制/对抗段（无需 id）
rule sections [--system <规则书>]          # 列出各段及条目 id
rule new <新ID> --from <基础规则书>         # 以某本规则书为基础创建新规则书
rule patch '<JSON>' [--system <规则书>]     # 应用一批规则修改
rule patch --file <JSON路径> [--system]    # 从文件读取修改批次
rule add --file <规则书.yaml> [--force]    # 安装自定义规则书（QQ：先发文件再 rule add）
rule del <规则书id>                        # 卸载用户规则书
```

`<段>` 取值：`skill`、`attr`、`level`、`rules`、`limit`、`mechanic`、`opposed`。

示例：

```
rule sections --system coc7
rule show skill spot_hidden --system coc7
rule new my_coc --from coc7
rule patch --file house-rules.json --system coc7
```

`rule patch` 的 JSON 是一个 `PatchOp` 数组，例如：

```json
[
  {"kind":"skill_set","target":"spot_hidden","field":"base","value":"30"},
  {"kind":"rules_set","target":"fumble","field":"text","value":"自定义大失败说明"},
  {"kind":"opposed_tie","value":"none"}
]
```

支持的 `kind`：

| kind | 作用 |
| --- | --- |
| `skill_set` / `skill_add` / `skill_del` | 修改/新增/删除技能 |
| `skill_alias_add` / `skill_alias_del` | 技能别名增删 |
| `attr_name` / `attr_alias_add` / `attr_alias_del` | 属性名称/别名 |
| `level_name` / `level_when` | 成功等级名称/条件 |
| `limit_set` | 限制区间 min/max |
| `mechanic_set` | 机制字段（roll/meets/label/keep 等） |
| `rules_set` / `rules_add` / `rules_del` | 速查词条增删改 |
| `opposed_tie` | 对抗平局规则（`none`/`higher_target`） |

行为说明：

- 修改后先重新编译校验，校验失败不会写文件、不会生效。
- 用户规则书原地覆盖；内置规则书首次编辑会复制到 `<数据根>/rulebooks/<id>.yaml`。
- 保存成功后热更新注册表，立即生效，无需重启。
- `rule new` 创建的新书会移除基础书的 `commands` 与 `default`，避免别名冲突或抢占默认书。

#### 安装与卸载自定义规则书（rule add / rule del）

可以从现成的规则书 YAML 文件直接安装（配合 `web/rulebook-generator.html`
生成器，或从别人手里拿到的规则书文件）：

```
rule add --file 我的规则书.yaml          # 校验 → 落盘 → 热更新，一步完成
rule add --file 我的规则书.yaml --force  # 覆盖同名用户规则书
rule del 我的规则书id                    # 卸载并删除文件
```

- 规则书 id 取自 YAML 的顶层 `id` 字段，与文件名无关。
- 校验失败（字段拼错、公式错误等）不会写任何文件；单本上限 512 KiB。
- 内置规则书（coc7/dnd5e）不允许被覆盖或删除。
- 还有角色卡在用的规则书、当前默认规则书不能删除，需先处理。

**QQ 里怎么装**：把 .yaml 规则书文件直接发给骰娘（私聊最稳妥；群里发文件需要
机器人开通「接收所有消息」权限），然后发送 `rule add`。安装与卸载仅限
bot master（服务端环境变量 `DICEGIRL_BOT_MASTERS`，逗号分隔 openid，留空则无人可装）。

### 10.4 `help`

```
help | 帮助          # 输出极简指令速查
```

---

## 11. 规则书 YAML

### 11.1 核心原则

dicegirl 的 Go 代码里没有任何游戏专属分支。引擎只解释一种声明式规则书；内置的 CoC 7e 与 D&D 5e 不是 Go 包，而是随程序 embed 的数据文件：

```
rulebooks/coc7.yaml        # 内置 CoC 7e 规则书（编译时 go:embed，默认书）
rulebooks/dnd5e.yaml       # 内置 D&D 5e（2014）规则书
<数据根>/rulebooks/*.yaml   # 用户规则书（运行时加载，无需重编译）
```

两条来源走**同一个加载器、同一个编译器、同一条执行路径**。添加一个游戏 = 写一个同构 YAML 丢进 `<数据根>/rulebooks/`，不需要改一行 Go。

规则书选择：显式 `--system <id>` > 角色卡上的 `system_id` > 声明了 `default: true` 的书（内置为 coc7）> 恰好加载一本书时隐式默认；零本报 `RULE_NOT_FOUND`，多本且无默认书时报 `INVALID_ARGUMENT`。

### 11.2 最小示例

```yaml
schema_version: "1"
id: "mini"                        # ^[a-z0-9]+([._-][a-z0-9]+)*$，≤64 字符
name: "Mini Percentile"
system_version: "0.1.0"          # 语义版本 x.y.z
rules_version: "test"            # 非空，规则版本标识
description: "一个最小的百分骰游戏"

attributes:
  - { id: "X", name: "未知属性" }

skills:
  melee: { base: 30, name: "近战" }

limits:
  characteristic: { min: 0, max: 99 }
  skill:          { min: 0, max: 99 }
  target:         { min: 0, max: 99 }

mechanic:
  max_bonus_penalty: 10
  roll: "1d100"                  # 一次检定掷的骰子表达式
  meets: "roll <= target"        # 成功条件（仅可见 roll/target）
  bonus_keep: lowest             # 奖励骰取低（n+1 次独立掷骰）
  penalty_keep: highest          # 惩罚骰取高
  label: "d100"                  # 结果解释里的标签

checks:
  levels:
    - { id: success, name: "成功", when: "roll <= target" }
    - { id: fail, name: "失败" } # 末位无 when：兜底等级

opposed:
  fumble_level: fail             # 哪个等级是「大失败」
  tie_break: none                # none | higher_target
```

放好后：

```bash
dicegirl .rule list                            # 应能看到 mini
dicegirl .rule validate --system mini          # 重新编译该书做完整校验
dicegirl .ra --system mini 50                  # 用这本书检定
```

### 11.3 字段总览

| 段 | 必填 | 作用 |
| --- | --- | --- |
| 身份头 | 是 | `schema_version`、`id`、`name`、`system_version`、`rules_version`、`description`；可选 `default: true` 与 `commands`（指令别名，全局唯一） |
| `attributes` | 是 | 属性定义，顺序即展示顺序；`id` 唯一，`name` 可中文，可选 `aliases` |
| `skills` | 否 | 技能表；每条恰好给 `base`（常量）或 `base_expr`（属性公式）之一，可选 `aliases` |
| `limits` | 是 | `characteristic` / `skill` / `target` 三个闭区间；可选 `computed_target` |
| `mechanic` | 是 | `roll`（骰子表达式）+ `meets`（布尔公式），另加 `max_bonus_penalty`；可选 `bonus_keep`/`penalty_keep`/`label` |
| `checks.levels` | 是 | 成功等级，≥2 个 |
| `checks.difficulties` | 否 | 显式难度修饰 |
| `opposed` | 是 | `fumble_level` + `tie_break` |
| `damage_bonus` | 否 | 伤害加值查找表 |
| `derived` | 否 | `values`、`vitals`、`initial_attributes` |
| `resource_checks` | 否 | 以检定驱动的资源损失 |
| `tables` | 否 | 随机表 |
| `growth_checks` | 否 | 成长检定 |
| `generation` | 否 | 人物作成规则（含技能点池 `skill_points`） |

### 11.4 检定机制

检定的「怎么掷、怎么合成一个值、奖惩骰怎么作用」全部由**通用表达式**声明：掷什么、怎样算成功都写在 YAML 里，引擎不含游戏分支。

```yaml
# CoC 的 d100 roll-under
mechanic:
  max_bonus_penalty: 100
  roll: "1d100"           # 一次检定掷的骰子表达式
  meets: "roll <= target" # 布尔公式（仅可见 roll/target）
  bonus_keep: lowest      # 奖励骰：n+1 次独立掷骰取低（默认 highest）
  penalty_keep: highest   # 惩罚骰：取高（默认 lowest）
  label: "d100"           # 可选，结果解释里的标签

# D&D 的 d20 roll-over
mechanic:
  max_bonus_penalty: 10
  roll: "1d20"
  meets: "roll >= target"
  bonus_keep: highest     # 优势：取高
  penalty_keep: lowest    # 劣势：取低
  label: "d20"
```

- `roll` 可以是任意合法骰子表达式（`1d20`、`3d6`、`2d10+3`…）。
- 净奖励/惩罚 N 会独立掷 N+1 次，再由 `bonus_keep`/`penalty_keep` 选出一颗作为骰值。
- `meets` 决定「骰值达到目标」的方向，所以掷高成功（`>=`）与掷低成功（`<=`）都是数据。
- 结果 JSON 的 `roll_detail.mechanic` 固定为 `"expression"`。

> 旧版的 `mechanic.type`（`percentile` / `d20` 内置族）已移除；声明 `type` 会在加载期报 `CONFIG_INVALID`，请改用上面的 `roll` + `meets` 写法。

### 11.5 成功等级的求值顺序

`levels` 按**声明顺序求值，首个 `when` 为真者胜出**；末位必须无 `when`，作为兜底。

CoC 的顺序是：

```yaml
checks:
  levels:
    - id: fumble
      name: "大失败"
      when: "(target < 50 && roll >= 96) || (target >= 50 && roll >= 100)"
    - id: extreme
      name: "极难成功"
      when: "roll <= target/5"
    - id: hard
      name: "困难成功"
      when: "roll <= target/2"
    - id: regular
      name: "普通成功"
      when: "roll <= target"
    - id: fail
      name: "失败"        # 兜底
```

大失败必须排在最前；其余等级按**从好到坏**排列。`when` 公式只能看见两个整数变量：`roll` 与 `target`，必须是布尔公式。

### 11.6 显式难度（可选）

```yaml
checks:
  difficulties:
    - { id: extreme, name: "极难",     expr: "target/5" }
    - { id: hard,    name: "困难",     expr: "target/2" }
    - { id: auto,    name: "自动成功", auto: true, success_level: regular }
```

- **公式难度**（`expr`，只可见 `target`）：判定前把目标值改写为公式结果。大失败区间跟随改写后的目标值；成功档位仍按基础目标值的等级梯读取。
- **自动成功**（`auto: true` + `success_level`）：目标值不改写，仅按基础目标判大失败；非大失败一律映射为所声明的成功等级。
- 关键词在调用处按声明解析：`ra 困难心理学`、`ra 极难 心理学 60`。非法声明在加载期报 `CONFIG_INVALID`。

### 11.7 伤害加值表

```yaml
damage_bonus:
  key: "STR + SIZ"
  rows:
    - { min: 2,   max: 64,  build: -2, bonus: "-2" }
    - { min: 65,  max: 84,  build: -1, bonus: "-1" }
    - { min: 85,  max: 124, build: 0,  bonus: "0" }
    - { min: 125, max: 164, build: 1,  bonus: "1d4" }
    # ...行必须首尾相接（本行 min = 上行 max+1），直到覆盖全部可能 key
```

- `key` 是属性公式；`bonus` 必须是合法骰子表达式。
- 编译期校验：表非空、min ≤ max、行连续无缺口、bonus 可解析。
- `r <武器表达式>+db` 带角色时求 key、查表、把 `db` 展开成 `bonus` 后交给通用骰子引擎；没有角色卡时不能用 `db`。

### 11.8 派生属性、生命条与初始属性

```yaml
derived:
  values:                # 按声明顺序求值
    - { id: build,        name: "体格",     lookup: damage_bonus, field: build }
    - { id: db,           name: "伤害加值", lookup: damage_bonus, field: bonus }
    - { id: max_hp,       name: "HP",      formula: "(CON+SIZ)/10" }
    - { id: max_mp,       name: "MP",      formula: "POW/5" }
    - { id: initial_san,  name: "初始理智", formula: "POW" }
    - { id: initial_luck, name: "初始幸运", formula: "min(POW*5, 99)" }
    - id: mov
      name: "MOV"
      switch:
        - { when: "STR>SIZ && DEX>SIZ",   value: 9 }
        - { when: "STR<SIZ && DEX<SIZ",   value: 7 }
        - { default: true,                value: 8 }
  vitals:
    - { resource: hp,  name: "体力",   aliases: ["生命值"], max: max_hp,  initial: max_hp }
    - { resource: mp,  name: "魔法值", aliases: ["魔力"],   max: max_mp,  initial: max_mp }
    - { resource: san, name: "理智",   aliases: ["理智值"], max: "99",    initial: initial_san }
  initial_attributes:
    - { attribute: LUCK, value: initial_luck }
```

每条 value 恰好是三种模式之一：

| 模式 | 形式 | 结果 |
| --- | --- | --- |
| `formula` | 整数公式 | 整数，按声明顺序进入命名空间 |
| `switch` | 若干 `when` + 必须以 `default: true` 收尾 | 首个为真分支的整数 `value` |
| `lookup` | `lookup: damage_bonus` + `field: build` 或 `field: bonus` | build 得整数；bonus 得骰子表达式字符串（仅展示/掷骰） |

- `vitals` 声明可初始化的资源轨；建卡后会立即按 `vitals`/`initial_attributes` 把初始值录进卡。
- `vitals` 的 `name`/`aliases`（可选）把轨道纳入名字解析阶梯（属性 → 技能 → 资源轨）：`ra 理智`、`st show 体力`、`st san=40` 都经此解析；轨道原始 id（大小写不敏感）恒可解析。
- `initial_attributes` 在初始化时补齐角色卡上缺失的属性（如 LUCK）。

### 11.9 资源检定（理智等）

```yaml
resource_checks:
  sanity:
    label: "理智"
    resource: san                       # 作用的资源轨
    fallback: "POW"                    # 轨不存在时，用属性公式求检定目标
    success_level: regular              # 达到该级或更好算成功
    loss: { success: "0", failure: "1d10" }
    fumble_loss: max                    # 可选：大失败取失败损失静态最大值
    range: { min: 0, max: 99 }          # 检定前 current 的合法区间
    clamp: { min: 0 }                   # after = clamp(current - loss)
```

- 损失规格：非负整数或骰子表达式；带符号的规格表示恢复（`"-1"`、`"-1d4"`）。
- `fumble_loss: max`：大失败时失败损失不掷骰，直接取理论最大值（定值即其本身，骰子式取全部保留骰面之和）。
- 引擎只产出**变更建议**（before/after/delta 与损失掷骰明细），不写卡；`.sc 0/1d10 --apply` 才持久化。
- `fallback` 是**整条 fallback 链的第一环**：`resource_checks.fallback` → 所指轨道 `vitals.initial` 公式 → 报错。`ra 理智` 等资源轨检定在卡上无轨道时走同一条链。

### 11.10 规则书公式语言

公式语言与骰子表达式无关：无骰子、无副作用、无字符串、无外部调用，加载期完成类型检查与变量白名单校验。

```
or      := and { "||" and }
and     := not { "&&" not }
not     := "!" not | cmp
cmp     := add { ("<"|"<="|">"|">="|"=="|"!=") add }
add     := mul { ("+"|"-") mul }
mul     := unary { ("*"|"/") unary }
unary   := "-" unary | primary
primary := number | ident | call | "(" or ")"
call    := ident "(" [or {"," or}] ")"
```

- 值只有 **int 与 bool**，绝不隐式转换。
- `&&`/`||` 短路；除法是**数学 floor 除法**（`-7/2 = -4`），与骰子表达式的向零取整不同；除零报 `DIVIDE_BY_ZERO`。
- 函数白名单仅 `abs(x)`、`min(x, …)`、`max(x, …)`。
- 各段可见变量由编译器强制：等级 `when` 仅 `roll/target`；技能 `base_expr`、伤害表 key、派生/资源公式可见属性 id。

### 11.11 随机表（`tables`）

```yaml
tables:
  - id: temporary_insanity
    name: 临时疯狂症状
    roll: 1d10
    rows:
      - { range: "1", text: "失忆：清醒后完全不记得刚才发生了什么" }
      - { range: "2", text: "暴力：攻击周围最近的人或物" }
      - { range: "default", text: "其它症状" }
```

- `roll` 是纯骰子表达式（不含 `#` 与比较后缀），决定表的**定义域**。
- `range` 三种形态：单点（`"3"`）、区间（`"2-5"`）、兜底（`"default"`，必须是最后一行）。
- 编译期校验：行不得重叠、不得超出定义域、必须覆盖整个定义域。
- 行文本可内嵌**子掷占位符** `{1d10}`：命中该行时占位符会被掷骰并替换成总值。

### 11.12 成长检定（`growth_checks`）

```yaml
growth_checks:
  default:
    condition: "roll > target"   # CoC 7e：掷值高于现值才成长
    gain: "1d10"
    cap: 99
  overrides:
    - { key: dodge, gain: "1d6", cap: 99 }
```

- `condition` 是布尔公式，只可见 `roll`（1d100 的结果）与 `target`（比较目标，默认是当前技能值）。
- `gain` 是骰子表达式（成长量）；`cap` 是成长后的上限（省略或 0 表示不限）。
- `overrides` 按技能/属性覆盖默认规则；省略的字段继承默认规则。
- 语义：掷 1d100 → 条件成立则掷成长量 → 新值 = min(当前值 + 成长量, cap) → **直接写卡**。

### 11.13 人物作成（`generation`）

```yaml
generation:
  default_variant: "7"
  name: "CoC 7 版"
  characteristics:             # 骰点法：每个属性一条表达式
    - { attr: STR, expr: "3d6*5" }
    - { attr: SIZ, expr: "(2d6+6)*5" }
    - { attr: LUCK, expr: "3d6*5" }
  variants:                    # 变体：覆盖 characteristics（可各自覆盖 point_buy）
    - id: "6"
      name: "CoC 6 版"
      characteristics:
        - { attr: STR, expr: "(2d6+6)*5" }
  point_buy:                   # 购点法（详细作成）
    attributes: [STR, CON, SIZ, DEX, APP, INT, POW, EDU, LUCK]
    min_value: 15
    budget: "EDU*20 + INT*10"  # 总点数（用买到的 EDU/INT 计算）
    costs:                      # 值在 [min,max] 内：花费 base + points × ceil((值-from)/per)
      - { min: 15, max: 49, from: 15, base: 7,  points: 1, per: 2 }
      - { min: 50, max: 79, from: 49, base: 24, points: 1, per: 5 }
      - { min: 80, max: 89, from: 79, base: 30, points: 3, per: 5 }
      - { min: 90, max: 99, from: 89, base: 36, points: 7, per: 5 }
    priorities:                 # 非交互自动分配：按顺序把点数花在这些属性上
      - { attr: EDU, value: 60 }
      - { attr: INT, value: 50 }
    skills:                     # 剩余点数用于技能，这里只声明各技能上限
      occupation_cap: "EDU*5"
      interest_cap: 75
  skill_points:                 # 作成后的技能加点池（st alloc 分配）
    - id: occupation
      name: "职业技能点"
      aliases: ["职业"]
      points: "EDU*4"           # 属性公式；1 点 = 技能值 +1
      cap: 90                   # 该池加点的单技能值上限（终值 ≤ cap）
      cap_overrides:            # 单技能特例（如兴趣点给手枪至多 50）
        - { skill: firearms_handgun, cap: 50 }
    - { id: interest, name: "兴趣技能点", aliases: ["兴趣"], points: "INT*2", cap: 75 }
```

- **骰点法**（默认）：逐条掷 `characteristics` 的表达式得到属性值（可选 `min`/`max` 夹取），一次生成一个角色；批量生成时每张卡各掷各的。
- **变体**：`variants` 声明同一游戏的其它版本，`default_variant` 指定默认版本；变体可各自覆盖 `characteristics`、`point_buy` 与 `skill_points`。卡以 metadata 记录自己由哪个变体生成，`st alloc` 按该变体的点池结算。
- **购点法**：先买 EDU/INT（点数池随之为 `EDU*20 + INT*10`），再按 `priorities` 的顺序把点数花到目标值上。剩余点数留给技能，各技能上限由 `skills` 声明（仅报告）。
- CLI 是**非交互**的：按规则书声明的 `priorities` 自动分配并如实报告点数余额。
- **技能点池**：`skill_points` 声明作成后可分配的点池（`points` 属性公式、`cap` 单技能终值上限、`cap_overrides` 单技能特例）。技能从规则书基础值起步（`base`/`base_expr`），1 点 = 值 +1；`st alloc` 加点/退点/重置，池余量记在卡的 `skill_points` 段（按池、按技能记账）。设计细节见 [skill-point-allocation.md](design/skill-point-allocation.md)。

### 11.14 当前的声明边界

一份规则书目前能完整表达「百分骰 roll-under 家族」与「d20 roll-over 家族」的静态规则，也能用 `mechanic.roll` + `meets` 声明其它简单骰法；以下尚不能声明：

- 骰面语义超出普通表达式的掷法：骰池（可用 `c` 算子近似）、Fudge 骰等；
- 多步有状态流程：疯狂/恐惧发作的多步流程、战斗轮、先攻、治疗周期；
- 条件性资源变化（按成功等级分多档损失、连锁多次掷骰）；
- 物品/武器/法术库，以及规则书自定义的指令逻辑（命令动词只能经 `commands` 声明作成别名、经 `command_bindings` 绑定到资源检定或随机表；角色卡具名宏见 3.5/6.4）。

边界之外的通用能力（角色卡 CRUD、导入导出、备份、任意骰子表达式、随机表与数据包）对任何规则书立即可用。

### 11.15 命令绑定（`command_bindings`）

聊天动词 `sc`/`ti`/`li` 调用什么由规则书声明（动词名本身是 Dice! 惯例，全局保留；作成别名见 `commands`）：

```yaml
command_bindings:
  sc: { resource_check: sanity }          # sc → 理智检定
  ti: { table: temporary_insanity }       # ti → 临时疯狂症状表
  li: { table: summary_insanity }         # li → 总结性疯狂表
```

- 每个绑定恰好一个目标（`resource_check` 或 `table`）；目标 id 在装载期校验，不存在即整本规则书加载失败。
- 规则书未声明某动词的绑定时，该动词回答「未声明绑定」（例如对 dnd5e 掷 `ti` 不会落到 CoC 的表上）。

---

## 12. 数据与环境变量

### 12.1 全局参数

| 参数 | 说明 |
| --- | --- |
| `--data-root <目录>` | 数据根目录（默认 `./data`；也可用环境变量 `DICEGIRL_DATA_ROOT`） |
| `--json` | 结构化 JSON 输出（每种结果带 `kind` 字段，如 `roll_batch`、`check`、`decks`、`tables`） |
| `--seed <整数>` | 固定随机种子，便于复现/调试 |
| `-h`、`--help` | 显示用法 |
| `version` | 版本号 |

### 12.2 环境变量

| 环境变量 | 作用 | 默认 |
| --- | --- | --- |
| `DICEGIRL_DATA_ROOT` | 同 `--data-root` | `./data` |
| `DICEGIRL_BACKUPS` | 是否启用更新前备份 | `true` |
| `DICEGIRL_BACKUP_KEEP` | 每角色保留备份份数 | `10` |

### 12.3 数据布局

```text
<数据根>/
  rulebooks/                用户规则书 *.yaml（可选；启动时加载）
  datapacks/                用户数据包 *.yaml（可选；启动时加载，非规则内容）
  dicegirl.db               SQLite 数据库：角色卡、掷骰统计、游戏与当前游戏、先攻、牌堆、跑团记录
  logs/                     跑团记录导出（*.txt/*.html）；旧版 QQ 战报（*.md）导入后保留
  legacy/                   旧版 YAML 数据根迁移过来的原文件（存档用）
  # 备份：更新前的卡版本保留在数据库的 card_backups 表（不再有独立的 backups/ 目录）
```

**冷热分工**：规则书与数据包是**冷数据**（启动读一次、人工编辑）→ 仍是 YAML 文件；角色卡与其它可写状态是**热数据**（每次检定都写）→ 放在一个 SQLite 数据库里（纯 Go 驱动，无需 cgo）。

### 12.4 JSON 输出

加 `--json` 后，正常结果走标准输出，错误仍走标准错误，退出码语义不变。例如：

```
dicegirl --json draw 扑克 2    # {"kind":"draw","cards":[...],"left":...,"discard":...}
```

---

## 13. QQ 机器人

### 13.1 调用方式

- **群聊**：@机器人 后接指令（QQ 官方平台只投递 @ 机器人的群消息）。
- **私聊**：直接发送指令，无需 @。

```
@机器人 help
@机器人 .r 3d6+1
@机器人 ra 3#心理学
```

### 13.2 平台专属指令（`log` 除外）

在 CLI 中输入 `h`/`rh`/`rah`/`rch`、`drawh` 会得到「需要聊天平台适配器」错误；`ob`、`pc tag/untag/grp` 只存在于机器人端。`log` 是共享指令，CLI 与机器人都支持。

#### `h` / `rh` — 暗骰

```
h <骰子表达式...>       # 如 h 3d6；rh 是同一指令
h <指令...>            # 如 h .ra 心理学、h ra 心理学
```

#### `rah` / `rch` — 暗骰检定

```
rah <技能/属性...>      # 等价于暗骰执行 ra
rch <技能/属性...>      # 等价于暗骰执行 rc
```

- 群里：结果**不在群里公开**，只私信发送者；群里只回「暗骰结果已私发给你」。
- body 看起来是指令（带前缀，或是已知指令/规则书别名）就按该指令执行，否则自动包成 `r`。
- 首次使用需按机器人提示**私聊完成绑定**：私聊机器人发送 `绑定 <绑定码>`（也接受 `bind`、`绑定码`，可带前缀，30 分钟内有效）。
- 私聊里 `rh` 就是普通私信掷骰，无需绑定。

#### `drawh` — 暗抽

```
drawh <牌堆> [数量]
```

等价暗的 `draw`，结果只私信抽牌人；同样需要先完成私信绑定。

#### `ob` — 暗骰围观

```
ob join    # 绑定私信并加入围观（此后该成员的暗骰结果也私信给你）
ob exit    # 退出围观
ob list    # 列出围观者与总开关
ob on      # 群主级总开关：开启围观
ob off     # 关闭围观
```

#### 成员绑卡（`pc` 的机器人子命令）

```
pc tag [名字|id]   # 把卡绑定到「你 × 当前游戏」；省略参数绑当前卡
pc untag          # 解除当前游戏的绑卡
pc untag all      # 解除你在全部游戏上的绑卡
pc grp            # 列出本群所有成员在各游戏上的绑卡
```

绑卡按游戏隔离：同一个人在不同游戏可绑不同卡。新建/作成并设为当前的卡会自动绑定。

#### `log` — 跑团记录

```
log                      # 当前记录状态、导出格式与计时开关
log new [名称]           # 新建并开始记录（省略名称用日期）
log on                   # 继续记录（暂停后）
log off                  # 暂停记录（休息）
log end                  # 结束记录并导出
log halt                 # 强行结束，不导出
log list [页码]          # 当前数据根日志列表（每页 10 份）
log get [名称]           # 重新导出某份日志
log del <名称|id>        # 删除日志及其导出文件
log stat [名称]          # 条数 / 参与者 / 时段 / 发言最多
log export [名称]        # 只导出文件，不结束记录
log type txt|html        # 导出格式（默认 txt）
log timer on|off         # 当前数据根跑团计时开关
log time                 # 当前记录累计时长
```

- 省略名称的 `get/export/stat` 默认先操作当前记录，没有当前记录时取最近一份；名称可以包含空格。新日志重名时会自动追加序号（如「第一章 2」），保证 `get/del` 不会歧义。
- 同一个数据根同时只能有一份进行中的日志；`end` 或 `halt` 之后才能 `new` 下一份。
- 记录期间，机器人会记录它收到的群消息（QQ 官方平台只投递 @ 机器人的消息）与执行过的指令及其结果；CLI 记录执行过的指令及其输出。`off` 期间不记录，`on` 后继续。
- 日志按**数据根**保存（每个群、每个私聊各自独立），切换 `game` 不影响日志；`.log type` 与 `.log timer` 在机器人端需要当前游戏 GM，CLI 管理员可直接修改。
- 导出格式由 `log type` 决定：`txt` 纯文本，`html` 为自包含网页。CLI 会把文件写到 `<数据根>/logs/` 并打印路径；机器人直接把文件发到群聊或私聊窗口。
- 旧版 QQ 适配器写在 `<数据根>/logs/*.md` 的战报会在该群第一次触发机器人时自动导入数据库；原 Markdown 文件保留，导入后可用 `log list/get/stat/export` 照常查看。旧版正在记录的那一份会保持「记录中」；若已经有一份新日志在记录，则旧战报按「已结束」导入。
- dicegirl 不连接外部日志站：`end/get/export` 都只生成本地文件，不返回在线回放链接。`del` 会同时清理该日志生成的导出文件。

### 13.3 行为差异一览

| 行为 | CLI | 机器人 |
| --- | --- | --- |
| `sc` 写回卡 | 需显式 `--apply` | 默认写回 |
| 无参数时「当前角色」 | 游戏的当前卡 | 发送者在当前游戏的绑卡，否则游戏当前卡 |
| 非指令文本 | 「未知指令」，退出码 2 | 默认静默；`DICEGIRL_BOT_REPLY_UNKNOWN=true` 时给提示 |
| 全局参数 `--json/--data-root/--seed` | 支持 | 不适用（机器人无此入口） |
| `h`/`rh`/`rah`/`rch`/`drawh`/`ob`/`pc tag`/`pc untag`/`pc grp` | 不支持 | 支持 |
| `log` 导出 | 写本地文件并显示路径 | 发送群聊/私聊文件 |
| 每个群的数据 | — | 独立数据根，互不可见 |

### 13.4 机器人环境变量（摘要）

| 变量 | 说明 |
| --- | --- |
| `DICEGIRL_QQ_APPID` / `DICEGIRL_QQ_SECRET` | 机器人凭据（必填，仅从环境/`bot.env` 读取） |
| `DICEGIRL_BOT_DATA_ROOT` | 机器人各群数据根目录 |
| `DICEGIRL_BOT_REPLY_UNKNOWN` | `true` 时对非指令消息回复提示 |
| `DICEGIRL_BOT_DEBUG` | `true` 时打印事件原文、丢弃原因与 HTTP 明细（排障用） |
| `DICEGIRL_BOT_LOG_FILE` | 额外写入日志文件（10MB 轮转，保留 3 份） |
| `DICEGIRL_BOT_MASTERS` | master openid 列表（逗号分隔）；`rule add`/`rule del` 仅 master 可用，留空 = 无人可装 |

---

## 14. 错误码

错误码集中定义在 `internal/apperr/codes.go`，是对机器稳定的契约：测试与适配器按码判断流程，不解析错误文本。CLI 输出形如 `错误 [CODE]：消息`。

| 错误码 | 触发场景 |
| --- | --- |
| `INVALID_ARGUMENT` | 参数语义不合法（ID 格式、目标值范围、目标值算式结果越界、未知技能名/别名、显式当前值越界、多本规则书且无默认书时未指定 `--system`、角色名对应多张卡） |
| `PARSE_ERROR` | 骰子语句/表达式无法解析（空表达式、缺面数、括号不匹配、非法字符、尾随 token、非法 `n#` 重复前缀或比较后缀） |
| `ROLL_ERROR` | 已解析但无法执行（随机源失败、上下文取消、算术溢出） |
| `DIVIDE_BY_ZERO` | 表达式整数除零 |
| `LIMIT_EXCEEDED` | 超过骰数/面数/深度/重复轮数上限 |
| `RULE_NOT_FOUND` | 规则书 ID 未加载、数据包未加载或没有默认姓名集 |
| `CONFIG_INVALID` | 规则书 YAML 解析失败或编译校验失败 |
| `INVALID_DATA` | 持久化文件不是合法 YAML 或缺 schema_version |
| `VALIDATION_ERROR` | 角色卡/manifest/规则书语义校验失败 |
| `UNSUPPORTED_SCHEMA_VERSION` | 数据声明了二进制不支持的 schema 版本 |
| `NOT_FOUND` | 角色或文件不存在（含按名字查角色失败） |
| `CONFLICT` | 角色 ID 重复、指令别名被两本规则书占用 |
| `STORAGE_READ_FAILED` | 读取权限、I/O 失败 |
| `STORAGE_WRITE_FAILED` | 目录创建、临时文件、fsync、rename、备份修剪失败 |
| `INTERNAL` | 未分类错误与兜底 |

约定：

- 所有内部错误都包装为 `*apperr.Error{Op, Code, Message, Err}`，用 `errors.Is/As` 与 `apperr.IsCode(err, code)` 判断。
- 根因通过 `%w` 保留；CLI 只展示消息和一行可选原因，不打印堆栈或无关绝对路径。
- CLI 退出码：`0` 成功；`2` 用法错误（`INVALID_ARGUMENT`、`PARSE_ERROR`）；`1` 其他运行时错误。
- 新增错误码必须先加入本表与 `codes.go`，不允许各包临时发明。
