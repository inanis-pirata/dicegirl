# dicegirl 使用与扩展指南

- 第 1 节：骰子引擎功能与用法（CLI 与 Go 库两种方式）
- 第 2 节：规则书 YAML —— 如何用一份 YAML 配置一个游戏的全部规则
- 第 3 节：CLI 短指令全览

## 1. 骰子引擎

骰子引擎位于 `internal/dice`，与任何规则系统无关，成功等级不在这里。

### 1.1 功能清单

| 功能 | 说明 | 示例 |
| --- | --- | --- |
| 基础骰 | 任意面数 `NdM`，数量省略为 1 | `1d100`、`d6`、`d2` |
| 百分号骰 | `d%` 等价 `d100` | `d%` |
| 四则运算 | `+ - * /` 与括号，乘除优先 | `2d6+3`、`(1d8+2)*2` |
| 乘号别名 `X` | 中缀位置单独的 `X`/`x` 等同 `*`（Dice! 写法） | `3d6X5` |
| 一元正负 | 表达式可带符号，除法向零取整 | `-7/2 = -3` |
| 保留最高 `kh` | `NdMkhK` 保留最大的 K 颗，省略 K 默认 1 | `4d6kh3`、`d20kh` |
| 保留最低 `kl` | 保留最小的 K 颗 | `2d20kl1` |
| 丢最高 `dh` / 丢最低 `dl` | 与 kh/kl 等价的丢弃写法 | `4d6dl1` |
| 爆骰 `!` | 达到阈值的骰追加一颗同面骰（阈值省略=最大面） | `1d6!`、`1d6!8` |
| 重投 `r` | 低于阈值的骰重投（次数受安全上限约束） | `2d6r4`（重投 1–3） |
| 成功计数 `c` | 本组值 = 满足条件的骰数（骰池语义） | `5d10c8`、`5d10c>=8` |
| 多骰组 | 一条表达式任意多个骰组 | `1d8+1d6+4` |
| 重复前缀 | `n#<表达式>` 连掷 n 轮，n 为 1–10；省略 n 视为 1 | `3#1d20`、`#1d6` |
| 比较后缀 | 整条表达式末尾带 `>= > <= <` 目标，判定总值成败，重复时逐轮判定 | `2d6+1>=7`、`3#1d20>=10` |
| 原因文本 | CLI 把表达式之后的参数原样收集回显，不进入骰子语法 | `.r 1d8+2 中型刀伤害` |
| 结构化明细 | 返回每颗骰的面值、是否被保留/丢弃、每个骰组小计 | 见 1.4 |
| 随机源注入 | crypto 随机 / 固定种子 / 测试脚本序列 | 见 1.5 |
| 资源上限 | 防恶意表达式（默认值见下），溢出/除零明确报错 | 见 1.6 |

语句层文法为 `stmt = [number '#'] expr [ ('>='|'>'|'<='|'<') number ]`；`#` 只能出现在最前、比较符只能出现在整条表达式末尾（比较目标是整数，可为负数）。规则书 YAML 中作者填写的骰子表达式（伤害、损失等）只走纯表达式语法，不含 `#` 与比较后缀。

**通用算子**（爆骰 `!`、重投 `r`、计数 `c`）是骰组修饰符，可与 `kh/kl/dh/dl` 组合，语义与游戏无关：

| 记法 | 语义 | 上限 |
| --- | --- | --- |
| `NdM!` | 每颗掷出最大面的骰追加一颗同面骰（追加的骰也可能继续爆） | 每组最多追加 100 颗（`dice.Limits.MaxExplodeDice`） |
| `NdM!T` | 骰值 ≥ T 时追加一颗同面骰 | 同上 |
| `NdMrT` | 骰值 < T 时重投该骰 | 每颗最多重投 10 次（`dice.Limits.MaxRerolls`） |
| `NdMcT` | 本组值 = 满足条件的骰数（默认 `>=`；也支持 `c>=T`/`c<=T`/`c>T`/`c<T`） | — |

- 求值顺序：掷骰 → 重投 → 爆骰 → 保留/丢弃 → 取值（求和或计数）。
- 组内的比较符属于**表达式**（`5d10c>=8` 是一个纯表达式），语句层的尾部比较仍然照旧（`2d6+1>=7`）；语句层先尝试整体作为纯表达式解析，失败才拆分尾部比较。
- 计数组的静态上下界是 `[0, 骰数]`（用于「大失败取最大损失」这类静态求界）。
- **对照表**（Dice! 逐系统指令 → dicegirl 通用写法）：

  | Dice! | dicegirl |
  | --- | --- |
  | `.rdc`（D20 优势/劣势） | `2d20kh1` / `2d20kl1` |
  | `.w` / `.ww`（W-Wing 骰池） | `.w [个数]a[加骰参数]`（= `Nd10!Tc8`）或直接写 `5d10!8c8` |
  | `.rsr`（1 点过半=失误）等系统特定判定 | 不做别名；用通用算子 + 自己的判定表达 |

明确**不支持**（需要时再扩展语法）：Fudge 骰。比较后缀只判定整条总值，不做逐颗计数（逐颗计数用 `c` 算子）。

注意：骰子表达式里的除法向零取整（`-7/2 = -3`）；规则书公式语言（第 2.10 节）是另一套更小的语言，除法为数学 floor（`-7/2 = -4`），二者不要混淆。

### 1.2 CLI 用法

CLI 只有聊天风格短指令（首字符 `.。!！` 可省略），没有并行的长命令层：

```bash
dicegirl .r "2d6+3"
dicegirl .r "4d6kh3"
dicegirl .r "3#1d20>=10"          # 连掷 3 轮，逐轮判定 >=10
dicegirl .r "1d8+2" 中型刀伤害     # 之后的参数作为原因文本回显
dicegirl --seed 42 .r "1d100"     # 固定种子，结果可复现
dicegirl --json .r "2d10"         # 输出完整结构化 JSON
dicegirl help                     # 完整指令表
```

文本输出示例：

```text
🎲 4d6kh3+2 = 14  [4d6: 4,~2~,3,5]
🎲 1d8+2 = 7  [1d8: 5]｜中型刀伤害
🎲 攻击检定：3#1d20>=10
① 11  [1d20: 11] 成功(>=10)
② 6  [1d20: 6] 失败(>=10)
③ 8  [1d20: 8] 失败(>=10)
```

`~x~` 表示被保留规则丢弃的骰子；`n#` 重复时逐轮独立掷骰并编号输出，比较后缀逐轮独立判定。超过 20 颗骰的骰组按面值升序展示（Dice! 渲染约定），保留/丢弃标记仍跟随各自的骰子。

### 1.3 Go 库用法

```go
import (
    "context"
    "dicegirl/internal/dice"
)

// 生产环境：crypto/rand
roller := dice.Default()
res, err := roller.Roll(context.Background(), "2d6+3")
res.Total          // int，最终总值
res.Expression     // 原始表达式
res.Groups         // []*DiceNode，每个骰组及每颗骰子

// 语句层：n# 重复前缀 + 比较后缀（"3#1d20>=10"）
batch, err := roller.RollStatement(ctx, "3#1d20>=10")
batch.Repeat       // 3
batch.Rolls        // []*RollResult，每轮独立 AST；Compare 为逐轮判定结果
st, err := dice.ParseStatement("3#1d20>=10", dice.DefaultLimits()) // 只解析不掷骰

// 自定义安全上限
roller = dice.New(dice.NewCryptoSource(), dice.WithLimits(dice.Limits{
    MaxDicePerGroup: 50, MaxTotalDice: 200, MaxSides: 100, MaxDepth: 20,
}))

// 只要解析不掷骰（可用于校验输入表达式）
root, groups, err := dice.Parse("1d20+5", dice.DefaultLimits())
err = dice.Eval(ctx, root, src)
```

### 1.4 结果结构

```go
type RollResult struct {
    Expression string      // 输入表达式
    Root       Node        // 求值后的 AST（NumberNode/DiceNode/BinaryNode/UnaryNode）
    Total      int         // 最终值
    Groups     []*DiceNode // 所有骰组（求值顺序）
    Compare    *Comparison // 比较后缀判定结果，无后缀时为 nil
}

// 语句层
type BatchResult struct {
    Expression string        // 整条语句原文
    Repeat     int           // 轮数
    Rolls      []*RollResult // 每轮一个独立结果
}
type Comparison struct {
    Op CmpOp; Target int; Success bool   // op: "<" "<=" ">" ">="
}

type DiceNode struct {
    Count int        // 骰数
    Sides int        // 面数
    Keep  *KeepSpec  // kh/kl/dh/dl 修饰（无则 nil）
    Rolls []DieRoll  // {Value int; Kept bool}
    Value int        // 该骰组被保留骰子的小计
}
```

`--json` 输出的就是这棵 AST，字段为小写 JSON 名（`count/sides/rolls/kept/value/left/right/op`），未来机器人适配器可直接消费同一契约。单轮语句输出扁平的 RollResult（新增可选 `comparison` 对象与 CLI 层 `reason` 字段）；多轮（`n#`，n>1）输出批量容器 `{expression, repeat, reason?, rolls:[RollResult...]}`。比较判定逐轮独立。

### 1.5 随机源

| 类型 | 构造 | 用途 |
| --- | --- | --- |
| `CryptoSource` | `NewCryptoSource()` / `dice.Default()` | 默认，crypto/rand，不可预测 |
| `SeededSource` | `NewSeededSource(seed)` | 同种子同序列，可复现游戏与调试（CLI `--seed`） |
| `ScriptedSource` | `NewScriptedSource(v0, v1, ...)` | 单元测试专用；值是 `[0,n)` 的 0 基序列，耗尽或越界即报错，测试不可能偷用真实随机 |

所有掷骰接收 `context.Context`，可取消。

### 1.6 限制与错误

默认限制：单组 ≤ 100 颗、整条表达式 ≤ 1000 颗、面数 ≤ 1000、括号/一元嵌套 ≤ 40 层、重复 ≤ 10 轮、爆骰每组追加 ≤ 100 颗、重投每颗 ≤ 10 次。算术用 int64 预判溢出。

错误码（`internal/apperr`，可 `apperr.IsCode(err, ...)` 判断）：

| 码 | 含义 |
| --- | --- |
| `PARSE_ERROR` | 语法错误：空表达式、`1d`、括号不匹配、非法字符、尾随 token、未知修饰词；语句层的非法 `#`/比较后缀同样归此码 |
| `INVALID_ARGUMENT` | `0d6`、`1d0`、保留数 > 骰数、重复次数 < 1 等 |
| `LIMIT_EXCEEDED` | 超过骰数/面数/深度/重复轮数上限 |
| `DIVIDE_BY_ZERO` | 整数除零 |
| `ROLL_ERROR` | 随机源失败、上下文取消、算术溢出 |

### 1.7 语法的形式定义

```text
stmt   = [number '#'] expr [ ('>='|'>'|'<='|'<') number ]
expr   = term { ('+' | '-') term }
term   = unary { ('*' | '/') unary }
unary  = ('-' | '+') unary | primary
primary= number | dice | '(' expr ')'
dice   = [number] ('d'|'D') (number | '%') [ keep ]
keep   = ('kh'|'kl'|'dh'|'dl') [number]
```

`stmt` 是 CLI `.r` 与 `Roller.RollStatement` 的入口；`expr` 是纯表达式，也是规则书 YAML 内作者填写的伤害/损失/bonus 等字段所用的语法——规则书表达式不含 `#` 与比较后缀。

## 2. 规则书：一份 YAML 配置一个游戏的全部规则

### 2.1 核心原则：引擎没有「系统包」

dicegirl 的 Go 代码里没有任何游戏专属分支。引擎（`internal/engine`）只解释一种声明式规则书；内置的 CoC 7e 不是 Go 包，而是随程序 embed 的一份数据文件：

```text
rulebooks/coc7.yaml        # 内置 CoC 7e 规则书（编译时 go:embed，默认书）
rulebooks/dnd5e.yaml       # 内置 D&D 5e（2014）规则书（d20 机制，编译时 go:embed）
<数据根>/rulebooks/*.yaml   # 用户规则书（运行时加载，无需重编译）
```

两条来源走**同一个加载器、同一个编译器、同一条执行路径**：

- 启动时 `engine.LoadBuiltins(rulebooks.FS, roller)` 加载内置书，`engine.LoadDir(<数据根>/rulebooks>, roller)` 加载用户书；
- `engine.ParseBook` 做严格 YAML 解码（`KnownFields(true)`，拼错字段直接失败）+ 全量编译校验：身份头、范围、等级条件、公式类型与变量白名单、表连续性、骰子表达式合法性、引用完整性；
- 任一书无效即启动失败（`CONFIG_INVALID`），错误信息带文件名；
- 规则书选择：显式 `--system <id>` > 角色卡上的 `system_id` > 声明了 `default: true` 的书（内置为 coc7）> 恰好加载一本书时隐式默认；零本报 `RULE_NOT_FOUND`，多本且无默认书时报 `INVALID_ARGUMENT`。

**添加一个游戏 = 写一个同构 YAML 丢进 `<数据根>/rulebooks/`**（文件名任意，扩展名 `.yaml`/`.yml`），不需要改一行 Go、不需要编译器。

### 2.2 最小示例

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
  type: percentile               # 内置族；也可写 roll: "1d100" + meets: "roll <= target"（见 2.6）
  max_bonus_penalty: 10

checks:
  levels:                        # 求值顺序，见 2.5
    - { id: success, name: "成功", when: "roll <= target" }
    - { id: fail, name: "失败" } # 末位无 when：兜底等级

opposed:
  fumble_level: fail             # 哪个等级是「大失败」（必败级）
  tie_break: none                # none | higher_target
```

放好后：

```bash
dicegirl .sys                                  # 应能看到 mini
dicegirl .sys validate --system mini           # 重新编译该书做完整校验
dicegirl .ra --system mini 50                  # 用这本书检定
```

### 2.3 字段总览

| 段 | 必填 | 作用 |
| --- | --- | --- |
| 身份头 | 是 | `schema_version`（当前固定 `"1"`）、`id`、`name`、`system_version`、`rules_version`、`description`；可选 `default: true`（多书时的默认书，后加载者胜）与 `commands`（本规则书的短指令别名，如 `["coc"]`，全局唯一） |
| `attributes` | 是 | 属性（特性值）定义，顺序即展示顺序；`id` 唯一，`name` 可中文，可选 `aliases` 同义词 |
| `skills` | 否 | 技能表；每条恰好给 `base`（常量）或 `base_expr`（属性公式）之一，可选 `aliases` 同义词 |
| `limits` | 是 | `characteristic` / `skill` / `target` 三个闭区间，需 `0 ≤ min ≤ max`；可选 `computed_target`（目标值算式的范围） |
| `mechanic` | 是 | `type`（内置族：`percentile` / `d20`）或 `roll` + `meets`（通用表达式），二者恰具其一，另加 `max_bonus_penalty`（奖励/惩罚骰上限，正整数）；见 2.6 |
| `checks.levels` | 是 | 成功等级，≥2 个；见 2.5 |
| `checks.difficulties` | 否 | 显式难度修饰（困难/极难/自动成功等）；见 2.5.1 |
| `opposed` | 是 | `fumble_level`（必须引用已声明等级）+ `tie_break`（`higher_target`/`none`） |
| `damage_bonus` | 否 | 查找表：`key` 公式 + 连续行表，bonus 是骰子表达式；见 2.7 |
| `derived` | 否 | `values`（formula/switch/lookup）、`vitals`、`initial_attributes`；见 2.8 |
| `resource_checks` | 否 | 以检定驱动的资源损失（理智等）；见 2.9 |

### 2.4 属性、技能与范围

```yaml
attributes:
  - { id: "STR", name: "力量", aliases: ["臂力"] }
skills:
  psychology: { base: 10, name: "心理学", aliases: ["心理"] }
  dodge:      { base_expr: "DEX/2", name: "闪避" }   # 公式见 2.10
limits:
  characteristic: { min: 0, max: 99 }
  skill:          { min: 0, max: 99 }
  target:         { min: 0, max: 99 }
  computed_target: { min: 0, max: 999 }   # 可选：目标值算式的结果范围
```

- 属性 ID 在解析用户输入时大小写不敏感，`name` 作为中文名解析；技能 ID 转小写匹配，同样支持中文名。
- `aliases` 是**规则书数据**里的同义词（可选）：解析顺序为 **规范 ID → aliases → 中文名**，别名大小写不敏感。别名不得与其它条目的规范 ID 或名称重复，重复或遮蔽在加载期报 `CONFIG_INVALID`。
- `base` 与 `base_expr` 互斥，`base` 必须非负。角色卡上没有该技能时，检定回退到基础值（输出标注「（基础）」）。
- 角色卡系统校验：属性必须已声明且在 characteristic 范围内；技能值（**包括规则书未列出的个人专精键**）只需落在 skill 范围内。
- target 范围同时约束检定目标值（允许 0：SAN 耗尽时任何 ≥1 都失败）。
- `computed_target` 约束**目标值算式**的结果（Dice! `.rc 体质*5`：卡上 60 → 目标 300）；省略时等同 target 范围，越界报 `INVALID_ARGUMENT`。

#### 2.4.1 目标值算式（区别于奖惩骰）

Dice! 里 `体质*5`、`敏捷-10` 这类写法改的是**检定目标值**，不是奖励/惩罚骰。dicegirl 的分工是：

| 写法 | 含义 | 位置 |
| --- | --- | --- |
| `心理学+2` / `心理学-1` / `+2` / `-1` | 奖励骰 / 惩罚骰（已发布语义，不变） | 检定后缀与独立 token |
| `体质*5` / `心理学/2` | 目标值算式：卡上的值先乘/除 | 名称 token 的后缀 |
| `--mod -10` | 目标值算式：目标值加一个整数 | `check` / `ra` 的长参数 |

- 名称后缀的 `*n`、`/n` 与 `±n` 不冲突：解析时先剥离 `±n`（奖惩骰），再在剩下的名称里找 `*n`/`/n`（目标算式），`体质*5+2` = 目标 ×5 + 2 颗奖励骰。
- 算式在难度修饰**之前**生效：`ra 困难 体质*5` 先把 60 变成 300，再由困难档改写成 150（难度关键词须在名称之前，与既有约定一致）。
- 结果里标注原始值与算式：文本 `目标 300（原始 60，*5）`，JSON 追加 `target_original` 与 `target_adjust`。
- 算式结果超出 `limits.computed_target` 报 `INVALID_ARGUMENT`（退出码 2）；显式目标值与算式不能同时给。

### 2.5 成功等级的求值顺序（重要）

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

大失败必须排在最前：它是「异常最差结果」，否则 `100 <= target/5` 这类极端值可能先命中更好的等级。其余等级按**从好到坏**排列。引擎据此得到两个语义（不依赖等级在列表里的数值位置猜测）：

- 对抗：大失败级必败（双大失败平）；其余等级中，声明更靠前的更好；同级按 `tie_break` 处理；
- 资源检定成功门槛：达到 `success_level` 或更好（即非大失败且声明位置不晚于门槛）算成功。

`when` 公式只能看见两个整数变量：`roll`（最终骰值 1–100，00/0 记 100）与 `target`；必须是布尔公式。

#### 2.5.1 显式难度（可选）

`checks.difficulties` 声明显式难度修饰（对应 Dice! 的 `困难/极难/自动成功` 与 BCDice 的 `h/e/c`）。省略整段的规则书不接受任何难度参数——引擎与 CLI 都没有内置难度关键词。

```yaml
checks:
  difficulties:
    - { id: extreme, name: "极难",     expr: "target/5" }
    - { id: hard,    name: "困难",     expr: "target/2" }
    - { id: auto,    name: "自动成功", auto: true, success_level: regular }
```

- **公式难度**（`expr`，只可见 `target`）：判定前把目标值改写为公式结果（困难 60→30、极难 60→12）。大失败区间跟随改写后的目标值（BCDice 约定：目标 99 的困难档为 49，96–100 均为大失败）；成功档位仍按基础目标值的等级梯读取，达不到该难度阈值的骰值降为兜底失败。输出标注 `（基础 60）`，JSON 另给 `difficulty_id`、`difficulty_name`、`base_target`。
- **自动成功**（`auto: true` + `success_level`）：目标值不改写，仅按基础目标判大失败；非大失败一律映射为所声明的成功等级（如自动成功爆破：骰 53 报普通成功，目标 40 时骰 96 仍为大失败）。
- 关键词在调用处按声明解析：`ra 困难心理学`、`ra 极难 心理学 60`（前缀或独立 token 均可，长名优先；也接受规则书声明的 id，如 `ra hard 心理学`）。改写后目标超出 `limits.computed_target`（省略时等同 `limits.target`）报 `INVALID_ARGUMENT`；非法声明（id/name 重复、expr 与 auto 冲突、success_level 引用大失败或不存在）在加载期报 `CONFIG_INVALID`。

### 2.6 检定机制（可插拔：表达式或内置族）

检定的「怎么掷、怎么合成一个值、奖惩骰怎么作用」由**机制（mechanic）**决定。规则书有两种写法：**通用表达式**（推荐，纯数据）或**内置机制族**（百分骰这类有特殊骰面语义的）。引擎里没有任何游戏专属分支。

```yaml
# 写法一：通用表达式——掷什么、怎样算成功都写在 YAML 里
mechanic:
  max_bonus_penalty: 10   # 奖励/惩罚骰数量上限
  roll: "1d20"            # 一次检定掷的骰子表达式（骰子引擎语法）
  meets: "roll >= target" # 布尔公式（仅可见 roll/target），显式难度用它判成败
  bonus_keep: highest     # 奖励骰：n+1 次独立掷骰取高/低（默认 highest）
  penalty_keep: lowest    # 惩罚骰：取低/高（默认 lowest）
  label: "d20"            # 可选，结果解释里的标签（默认即表达式原文）

# 写法二：内置族（与 roll 二选一）
mechanic:
  type: percentile        # 或 d20
  max_bonus_penalty: 100
```

`roll` 是任意合法骰子表达式（`1d20`、`3d6`、`2d10+3`…），净奖励/惩罚 N 会独立掷 N+1 次，再由 `bonus_keep`/`penalty_keep` 选出一颗作为骰值；`meets` 决定「骰值达到目标」的方向，所以掷高成功（`>=`）与掷低成功（`<=`）都是数据。通用族的结果里 `roll_detail.mechanic` 记为 `expression`。

内置两族仍然保留：

| 族 | 掷法 | 成功条件 | 奖惩骰 |
| --- | --- | --- | --- |
| `percentile` | 个位 d10 + 十位 d10（00+0 记 100） | 骰值 ≤ 目标（越小越好） | 奖励：多掷十位骰取**最小**；惩罚：取**最大** |
| `d20` | 一颗 d20 | 骰值 ≥ 目标（越大越好） | 奖励：两颗 d20 取**高**（优势）；惩罚：取**低**（劣势） |

百分骰保留为内置族，是因为它的 00/0 记为 100、以及奖励骰只换十位不换个位这两条语义无法用普通骰子表达式精确表达；`d20` 与 `1d20` + `meets: "roll >= target"` 等价（`rulebooks/dnd5e.yaml` 用的就是后者）。

机制之外的一切（成功等级、难度、目标值算式、同义词、随机表、成长、生成）都与机制无关，由规则书声明：

- **成功等级**仍是「按声明顺序求值，首个 `when` 为真者胜」：`when` 只可见 `roll` 与 `target`，因此掷低成功写 `roll <= target`、掷高成功写 `roll >= target` 即可，同一套引擎都认。
- **难度修饰**把目标改写成新阈值：`expr` 只可见 `target`。判定时用机制的 `meets` 比较（百分骰「小于等于算成功」、d20「大于等于算成功」），标签仍取基础目标值的等级梯；因此 CoC 的 `target/2`（变难）与 D&D 的 `target+5`（变难）都能用同一套语义。
- **大失败**由 `opposed.fumble_level` 指定的那一级判定；百分骰的大失败区间随改写后的目标移动（BCDice 约定），掷高成功族的天然 1 与目标无关。
- **结果 JSON** 除机制无关的字段（`raw_roll`、`level_code`、`roll_detail` 等）外，还保留百分骰的 `units`/`tens_candidates` 等旧字段（其它机制下为空）。

通用表达式覆盖不了的新骰面语义，才需要实现 `engine.Mechanic` 接口（`ID`/`RollRange`/`Meets`/`Roll`/`Apply`）并在 `compileMechanic` 里注册；`type` 与 `roll` 同时出现、`roll` 缺少 `meets`、非法表达式或非法 keep 方向都在加载期报 `CONFIG_INVALID`（绝不静默回退到别的族）。

示例：内置的 [rulebooks/dnd5e.yaml](../rulebooks/dnd5e.yaml) 是完整的 D&D 5e（2014）规则书，用短指令即可开玩（另有一份最小的 d20 示例书 `docs/examples/dnd-lite.yaml`，装进数据根即可用）：

```bash
dicegirl .dnd                # 4d6 丢最低生成 D&D 角色卡（设为当前角色）
dicegirl .dndd               # 27 点购点（标准数组）
dicegirl .ra 15              # d20 vs DC 15
dicegirl .ra 15+1            # 优势：两颗 d20 取高
dicegirl .ra 15-1            # 劣势：取低
dicegirl .ra 15 --mod -5     # d20+5 vs DC 15
dicegirl .ra 困难 15         # 相对 DC+5
dicegirl .rules 优势         # 规则速查
```

#### 2.6.1 百分骰族细节（percentile）

- d100 = 1 个个位 d10（面值 1–9，10→0）+ 1 个或多个十位 d10（面值 1→00 … 10→90）；00+0 记为 100。
- 奖励骰与惩罚骰先成对抵消；净奖励 N 就多掷 N 个十位骰取最小值，净惩罚取最大值；并列取先掷出的那颗。
- 结果 JSON 同时给出每个十位面值、候选十位、选中索引、个位、原始骰值、等级 rank（声明索引）、`level_code` 与中文标签、解释文本。

### 2.7 伤害加值表

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

- `key` 是属性公式；`bonus` 必须是合法骰子表达式（第 1 节语法，允许常数/负数）。
- 编译期校验：表非空、min ≤ max、行连续无缺口、bonus 可解析。
- `.dmg <武器表达式>` 带角色时求 key、查表、把 `expr+(bonus)` 交给通用骰子引擎；不带角色只掷武器表达式。
- `derived` 的 lookup 也读这张表（build 与 bonus 两列）。

### 2.8 派生属性、生命条与初始属性

```yaml
derived:
  values:                # 按声明顺序求值
    - { id: build,        name: "体格",     lookup: damage_bonus, field: build }
    - { id: db,           name: "伤害加值", lookup: damage_bonus, field: bonus }
    - { id: max_hp,       name: "HP",      formula: "(CON+SIZ)/10" }
    - { id: max_mp,       name: "MP",      formula: "POW/5" }
    - { id: initial_san,  name: "初始理智", formula: "min(POW*5, 99)" }
    - id: mov
      name: "MOV"
      switch:
        - { when: "STR<SIZ && DEX<SIZ",   value: 7 }
        - { when: "STR>=SIZ && DEX>=SIZ", value: 9 }
        - { default: true,                value: 8 }
  vitals:
    - { resource: hp,  max: max_hp,      initial: max_hp }
    - { resource: mp,  max: max_mp,      initial: max_mp }
    - { resource: san, max: "99",        initial: initial_san }
  initial_attributes:
    - { attribute: LUCK, value: initial_san }
```

每条 value 恰好是三种模式之一：

| 模式 | 形式 | 结果 |
| --- | --- | --- |
| `formula` | 整数公式 | 整数，**按声明顺序进入命名空间**，后面的公式/vitals 可直接引用其 id |
| `switch` | 若干 `when` + 必须以 `default: true` 收尾 | 首个为真分支的整数 `value`，整数同样进入命名空间 |
| `lookup` | `lookup: damage_bonus` + `field: build` 或 `field: bonus` | build 得整数（进入命名空间）；bonus 得骰子表达式字符串（**不进入**命名空间，仅供展示/掷骰） |

- `vitals` 声明可初始化的资源轨：`max` 与 `initial` 都是同一命名空间里的整数公式。建卡（`.coc`/`.dnd`/购点法、`pc new`、`.pc redo`）后会立即按 `vitals`/`initial_attributes` 把初始值录进卡；`pc new` 只给了部分属性、公式尚缺值时跳过初始化（卡照常建成，输出会提示），补齐属性后用 `.dv --init` 补录（已存在的轨不覆盖，`--force` 连同已有轨一起重置）。
- `initial_attributes` 在初始化时补齐角色卡上缺失的属性（如 LUCK）；引用的属性必须已声明。

### 2.9 资源检定（理智等）

```yaml
resource_checks:
  sanity:
    label: "理智"
    resource: san                       # 作用的资源轨
    fallback: "min(POW*5, 99)"          # 轨不存在时，用属性公式求检定目标
    success_level: regular              # 达到该级或更好算成功
    loss: { success: "0", failure: "1d10" }
    fumble_loss: max                    # 可选：大失败取失败损失静态最大值
    range: { min: 0, max: 99 }          # 检定前 current 的合法区间
    clamp: { min: 0 }                   # after = clamp(current - loss)
```

- `success_level` 必须引用已声明等级；大失败永远不算成功（2.5）。
- 损失规格：非负整数（`"0"`、`"5"`）或骰子表达式（`"1d10"`、`"1d4+1"`）；短指令用损失对 `<成功>/<失败>` 覆盖规则书默认值（`.sc 0/1d10`）。**带符号的规格表示恢复**（`"-1"`、`"-1d4"`）：`after = before - loss`，即恢复量，方向与损失相反。
- `fumble_loss: max`（可选，默认 `normal`）：大失败时失败损失不掷骰，直接取理论最大值（定值即其本身，骰子式取全部保留骰面之和，`1d4+1`→5、`1d10`→10）。最大值由骰子表达式静态界求出，不消耗随机数；结果 JSON 标记 `fumble_loss_max: true` 且不产生损失掷骰节点。coc7 的理智检定以此实现 CoC 7e / Dice! `.sc` 约定。
- 引擎只产出**变更建议**（before/after/delta 与损失掷骰明细），不写卡；`.sc 0/1d10 --apply` 才持久化。轨不存在时用 `fallback` 对角色属性求目标值。
- **显式当前值**（`sc 0/1 70`、`rc sanity 0/1d10 70`）：只改「本次检定用哪个目标值」，不写轨；变更建议仍以轨道自身的值为准。越界报 `INVALID_ARGUMENT`。
- 恢复不会超过轨道上限：`after` 先按 `clamp` 收敛，再收敛到 `range`（san 为 0–99）；写回时再按角色卡上轨道的 `max` 夹取。
- 一条资源检定 = 一次百分骰 + 一次损失掷骰（大失败取最大值时无第二次掷骰）。多次连锁损失、疯狂发作表这类多步有状态流程目前无法声明（见 2.16）。

### 2.10 规则书公式语言

公式语言（`internal/engine/formula`）与骰子表达式无关：无骰子、无副作用、无字符串、无外部调用，加载期完成类型检查与变量白名单校验。

文法：

```text
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

语义：

- 值只有 **int 与 bool**，绝不隐式转换；比较/逻辑运算得 bool，算术得 int。
- `&&`/`||` 短路；除法是**数学 floor 除法**（`-7/2 = -4`），与骰子表达式的向零取整不同；除零报 `DIVIDE_BY_ZERO`。
- 函数白名单仅 `abs(x)`、`min(x, …)`、`max(x, …)`，参数都是整数。
- int64 运算前预判溢出，溢出报 `ROLL_ERROR`。
- 各段可见变量由编译器强制：等级 `when` 仅 `roll/target`；技能 `base_expr`、伤害表 key、派生/资源公式可见属性 id，派生公式还可见此前声明的整数派生 id。未知变量/类型不符是加载期 `CONFIG_INVALID`；求值期缺变量是 `INVALID_ARGUMENT`。

### 2.11 校验与测试

- `dicegirl .sys validate`（不带 `--system`）逐本重新编译全部已加载规则书；`.sys validate --system <id>` 校验一本。
- Go 测试可直接把 YAML 字节喂给通用引擎并注入脚本随机源，证明「同一份数据复现全部行为」：

```go
data, _ := os.ReadFile("rulebooks/coc7.yaml")
book, err := engine.ParseBook(data, "coc7.yaml", dice.New(dice.NewScriptedSource(4, 1)))
res, err := book.Check(ctx, engine.CheckInput{Target: 60}) // 骰值 42
```

### 2.12 随机表（`tables`）

随机表是「掷骰 → 查行 → 出文本」的声明式查找表。规则书里的表属于**游戏内容**（CoC7 的疯狂表），数据包里的表属于**非规则内容**（随机姓名等），两者用同一套 schema。

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

- `roll` 是纯骰子表达式（不含 `#` 与比较后缀），它决定表的**定义域**（`1d10` → 1–10）。
- `range` 三种形态：单点（`"3"`）、区间（`"2-5"`）、兜底（`"default"`，必须是最后一行，覆盖其它行没占到的值）。
- 编译期校验：行不得重叠、不得超出定义域、必须覆盖整个定义域（没有兜底行时）；否则 `CONFIG_INVALID`（`.sys validate` 同样会失败）。
- 行文本可内嵌**子掷占位符** `{1d10}`：命中该行时占位符会被掷骰并替换成总值，结果 JSON 的 `subrolls` 给出每次子掷的明细。
- 掷表：`.tb <id>`、`.tb list`；短指令 `ti`（CoC7 `temporary_insanity`）、`li`（`summary_insanity`）。掷表**不需要角色卡**。
- 结果 JSON：`kind:"table_roll"`，含 `table_id`/`table_name`/`roll`/`value`/`row`/`range`/`text`/`subrolls`。

### 2.13 成长检定（`growth_checks`）

成长检定是「检定成功后能否涨技能」这一步（CoC 7e "Improving Skills"，Dice! `.en`）。

```yaml
growth_checks:
  default:
    condition: "roll <= target"   # 1d100 ≤ 当前技能值则成长
    gain: "1d10"
    cap: 99
  overrides:
    - { key: firearms_handgun, gain: "1d10", cap: 50 }
```

- `condition` 是布尔公式，只可见 `roll`（1d100 的结果）与 `target`（比较目标，默认是当前技能值）。
- `gain` 是骰子表达式（成长量）；`cap` 是成长后的上限（省略或 0 表示不限）。
- `overrides` 按技能/属性覆盖默认规则；`key` 必须是已声明的属性或技能（可用 id、中文名或别名）；省略的字段继承默认规则。
- 编译期校验：条件公式、成长量表达式、上限、`key` 引用都必须在加载期通过，否则 `CONFIG_INVALID`。
- 语义：掷 1d100 → 条件成立则掷成长量 → 新值 = min(当前值 + 成长量, cap)（成长不会降低数值）→ **直接写卡**（成长语义即写回，仍走备份/原子更新/校验路径）。未成长时只报告不改卡。
- 用法：`.en <技能/属性名> [目标值] [原因]`（`目标值` 可选，覆盖比较目标）。未知技能退出码 2。
- 结果 JSON：`kind:"growth_check"`，含 `key`/`name`/`before`/`after`/`gain`/`grew`/`cap`/`target`/`roll`/`gain_roll`。

### 2.14 数据包（`datapacks/`）

**规则只进规则书**；与任何游戏规则无关的内容（随机姓名、地名等）放进数据包：

```yaml
schema_version: "1"
id: "names"
name: 随机姓名数据包
description: "..."
tables:
  - id: cn_surname
    name: 中文姓氏
    roll: "1d20"
    rows:
      - { range: "1", text: "赵" }
name_sets:
  - id: cn
    name: 中文姓名
    default: true
    parts:
      - { table: cn_surname }
      - { table: cn_given }
```

- 内置数据包：`datapacks/*.yaml`（`go:embed` 打包，当前是 `names.yaml`）；用户数据包：`<数据根>/datapacks/*.yaml`，与内置包走同一加载/编译路径。
- `name_sets` 把若干表组合成一个名字：`parts` 里每项要么是 `{ table: <表 id> }`（掷一次该表并取行文本），要么是 `{ text: "字面量" }`；`default: true` 标记默认姓名集。
- 校验：id/name 必填、name set 的 id 唯一、`parts` 非空且每项只给一种、引用的表必须存在；否则 `CONFIG_INVALID`。
- 用法：`.name [cn|jp|en] [数量]`（默认姓名集、默认 1 个）。结果 JSON：`kind:"random_names"`，含 `set_id`/`names`/`rolls`。

### 2.15 人物作成（`generation`）

人物作成的规则全部由规则书声明：骰点法（掷骰生成属性）、购点法（详细作成）、以及同一游戏的其它版本变体。

```yaml
generation:
  default_variant: "7"        # 省略版本时用哪个变体
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
```

- **骰点法**（默认）：逐条掷 `characteristics` 的表达式得到属性值（可选 `min`/`max` 夹取），一次生成一个角色；批量生成时每张卡各掷各的，不串骰。
- **变体**：`variants` 声明同一游戏的其它版本（如 CoC 6 版），`default_variant` 指定默认版本；变体可覆盖 `characteristics` 与 `point_buy`。
- **购点法**（详细作成）：先买 EDU/INT（点数池随之为 `EDU*20 + INT*10`），再按 `priorities` 的顺序把点数花到目标值上；每个属性先按 `min_value` 预留底价，剩余点数按优先级升级，买不动就降到买得起的最高值。**剩余点数（`left`）留给技能**，各技能上限由 `skills` 声明（`occupation_cap`/`interest_cap`）。
- CLI 是**非交互**的：不弹点选框，按规则书声明的 `priorities` 自动分配并如实报告点数余额；技能点留给玩家用 `st K=V` 自行分配（CoC7 技能上限为 `EDU*5`，兴趣技能上限 75）。
- 校验：属性必须已声明；表达式可解析；费率档不重叠且 `per > 0`；`budget` 只可见属性 id；`priorities` 必须引用已声明属性；否则 `CONFIG_INVALID`。
- 生成结果是一张**普通角色卡**（属性已写入，技能留空即基础值），后续 `ra`/`st`/`en` 等命令照常可用。
- 命令：人物作成的**别名由规则书声明**（身份头的 `commands`）：`<别名>[版本][d] [数量]`，如 `.coc6d`（6 版购点法）、`.coc 10`（一次十张）、`.dndd`（D&D 5e 的 27 点购点）；版本数字对应规则书的 `variants`。另有 `pc redo`（清空当前卡属性后重新生成并重算生命/理智轨）。
- 结果 JSON：`kind:"generation"`，含 `variant_id`/`variant_name`/`detailed`/`characters[]`（每张卡含 `name`/`id`/`attributes`/`rolls`/`point_buy`）。

### 2.16 当前的声明边界

一份规则书目前能完整表达「百分骰 roll-under 家族」与「d20 roll-over 家族」的静态规则，也能用 `mechanic.roll` + `meets` 声明其它简单骰法；以下尚不能声明（需要时先扩 schema 与引擎，再在规则书里使用）：

- 骰面语义超出普通表达式的掷法：骰池（可用第 1 节的 `c` 算子近似）、Fudge 骰等（需要内置族或实现 `engine.Mechanic`）；
- 多步有状态流程：疯狂/恐惧发作的多步流程、战斗轮、先攻、治疗周期（单张随机表与单次成长检定已可声明，见 2.12/2.13）；
- 条件性资源变化（按成功等级分多档损失、连锁多次掷骰）；
- 物品/武器/法术库、自定义命令与宏。

边界之外的通用能力（角色卡 CRUD、导入导出、备份、任意骰子表达式、随机表与数据包）对任何规则书立即可用。

## 3. CLI 短指令全览

```text
dicegirl [全局参数] <短指令> [参数]
```

### 3.1 全局参数

| 参数 | 说明 |
| --- | --- |
| `--data-root <目录>` | 数据根目录（默认 `./data`；也可用环境变量 `DICEGIRL_DATA_ROOT`） |
| `--json` | 结构化 JSON 输出（机器人适配器可直接消费） |
| `--seed <整数>` | 固定随机种子，结果可复现 |
| `-h, --help` / `help` | 帮助 |
| `version` | 版本号 |

退出码：`0` 成功；`2` 用法错误（含 `PARSE_ERROR`/`INVALID_ARGUMENT`）；`1` 其他运行时错误。正常结果走 stdout，错误与提示走 stderr。

### 3.2 命令一览

CLI 只有聊天风格短指令（完整表见 3.5），没有并行的长命令层：所有操作都写成 `dicegirl <短指令> [参数]`，首字符的 `.。!！` 可省略。`help` 与 `version` 是仅有的两个非短指令入口。

```bash
dicegirl .r 2d6+3
dicegirl .dnd
dicegirl .ra 15+1
```

### 3.3 数据与环境变量

| 环境变量 | 作用 | 默认 |
| --- | --- | --- |
| `DICEGIRL_DATA_ROOT` | 同 `--data-root` | `./data` |
| `DICEGIRL_BACKUPS` | 是否启用更新前备份 | `true` |
| `DICEGIRL_BACKUP_KEEP` | 每角色保留备份份数 | `10` |

数据布局：

```text
<数据根>/
  rulebooks/                用户规则书 *.yaml（可选；启动时加载）
  datapacks/                用户数据包 *.yaml（可选；启动时加载，非规则内容）
  dicegirl.db               SQLite 数据库：角色卡、掷骰统计、本地状态、先攻、牌堆
  legacy/                   旧版 YAML 数据根迁移过来的原文件（存档用）
  backups/                  （数据库内的 card_backups 表保留旧版本）
```

**冷热分工**：规则书与数据包是**冷数据**（启动读一次、人工编辑）→ 仍是 YAML 文件；角色卡与其它可写状态是**热数据**（每次检定都写）→ 放在一个 SQLite 数据库里（纯 Go 驱动，无需 cgo）。详见 [data-format.md](data-format.md) 第 4–5 节。

写入为同目录临时文件 + fsync + 原子替换；文件格式详见 [data-format.md](data-format.md)。

### 3.4 典型会话

```bash
# 建卡后自动成为当前卡，并按规则书初始化 HP/MP/SAN 轨与缺失的初始属性
dicegirl .pc new 调查员甲 STR=50 CON=60 SIZ=70 DEX=55 POW=55 EDU=70 \
  psychology=60
dicegirl .ra 心理学+1
dicegirl .ra 75
dicegirl .dmg 1d8
dicegirl .sc 0/1d4 --apply
```

要换游戏：把一本规则书 YAML 放进 `<数据根>/rulebooks/`（可选地声明自己的短指令别名 `commands: ["pf"]`），建卡时 `--system <规则书 id>`，之后同一套 `.ra/.sc/.dmg/.dv/.tb` 命令按新书的规则运行。

### 3.5 短指令参考

CLI 的全部操作都是聊天风格短指令：首字符可带 `.。!！` 前缀（也可省略），调用 `internal/app` 的用例、共用 `--json` 契约。别名知识只存在于界面层 `cmd/dicegirl/cli/`，`internal/` 不认识任何短指令——未来的聊天平台适配器作为 cmd 侧兄弟包复用这套解析。涉及角色卡的参数一律可以写**角色名或 ID**（名字优先，玩家通常看不到 ID）。

| 短指令 | 说明与示例 |
| --- | --- |
| `r [语句] [原因...]` | 掷骰：`r 3#1d20>=10 攻击`、`.r 2d6+3`；**无语句时掷默认骰**（默认 d100，`.set` 可改） |
| `rs <语句> [原因...]` | 精简掷骰：只出总值不出每骰明细（JSON 只多 `"detailed":false`） |
| `w [骰子个数]a[加骰参数]` / `ww ...` | 骰池（W-Wing 风格）：固定 d10，达加骰参数就追加一颗，最后统计 ≥8 的颗数；`w` 只出总数，`ww` 给出每颗点数 |
| `ra [难度] [n#] [名称] [目标值] [±n]` | 检定：`ra 心理学`、`ra 心理学+2`、`ra 60`、`ra 15+1`（优势）、`ra 15-1`（劣势）、`ra 困难心理学`、`ra 极难 心理学 60`、`ra 体质*5`；连掷 `ra 3#心理学`/`ra 3#p 心理学`（上限 9 轮）；`--system/--character/--mod` 可用，`--character` 也接受名字 |
| `sc <成功损失>/<失败损失> [当前值] [原因...] [--apply]` | 理智检定：`.sc 0/1d10`；大失败按规则书 `fumble_loss` 取最大损失；当前值只改本次目标、不写轨；负损失表示恢复 |
| `rc <检定名> <成功损失>/<失败损失> [当前值] [原因...] [--apply]` | 资源检定：`rc sanity 0/1d10`；`--bonus/--penalty` 可用 |
| `st show`（或无参数） | 查看当前角色卡 |
| `st show <键>` | 查看单个值：`st show 心理学`、`st show san` |
| `st del <键>` | 删除一个属性/技能值；`st del &宏名` 删除具名宏 |
| `st clr` | 清空当前卡的数值（保留卡与 ID，写入前自动备份） |
| `st K=V [K=V...]` | 赋值：`st psychology=65 STR=50`；分隔符 `:` 等同 `=` |
| `st K±表达式 [K±表达式...]` | 增减：`st san-1d6`、`st STR+1`（右侧是纯骰子表达式） |
| `st &名=表达式` | 保存具名宏：`st &沙漠之鹰=1d10+1d6+3`，之后 `.r 沙漠之鹰` |
| `pc list` | 列出全部角色（`*` 标记当前） |
| `pc new <名> [K=V...] [--system <规则书>] [--active=false]` | 手工建卡：默认设为当前卡并初始化规则书的状态轨（`--active=false` 不切换；属性不全时先建卡，`dv --init` 补录） || `pc show [名字\|id]` | 查看角色卡（省略=当前角色） |
| `pc use <名字\|id>` | 切换当前角色 |
| `pc active` | 显示当前角色 ID（脚本用） |
| `pc nn [名字\|id] <新名>` | 重命名（省略=当前角色；名字不可重复） |
| `pc copy <名字\|id> <新名> [--active]` | 复制角色卡 |
| `pc cpy <目标>=<来源>` | 把来源卡的属性/技能/资源轨/宏覆盖到目标卡（两侧都可写名字） |
| `pc del <名字\|id>` | 删除一张卡（删当前卡会清空选择） |
| `pc val [名字\|id]` | 通用 + 规则书校验 |
| `pc meta [--character <名字\|id>] <键> [值]` | 设置角色卡元数据（省略值=删除） |
| `pc export [名字\|id] [--file <路径>]` | 导出 YAML（默认 stdout） |
| `pc import --file <路径\|-> [--keep-id]` | 导入 YAML（`-` 从 stdin；默认重新生成 ID） |
| `pc redo [名字\|id]` / `pc build [名字\|id]` | 按规则书重填属性（`--variant` 选版本） |
| `pc stat [名字\|id]` / `pc stat clr [名字\|id]` | 掷骰统计 / 清空统计 |
| `pc clr` | 销毁全部角色卡 |
| `set [面数]` | 设置裸 `r` 的默认骰面数；无参恢复 d100 |
| `<别名>[版本][d] [数量]` | 人物作成：**别名由规则书自己声明**（身份头的 `commands`），所以加游戏不用改 Go。内置 `coc` → CoC 7e（`.coc6d` = 6 版购点法、`.coc 10` = 一次十张）、`dnd` → D&D 5e（`.dnd` = 4d6 丢最低、`.dndd` = 27 点购点）；用户书也能声明自己的别名 |
| `sys` / `sys validate [--system <id>]` | 列出规则书 / 重新编译校验（省略 `--system` 校验全部） |
| `rules <词条>` / `rules list` / `rules set [系统]` | 规则速查（可写 `系统:词条`） |
| `tb <表 id>` / `tb list` | 掷规则书声明的随机表 / 列出随机表 |
| `ti` / `li` | 临时疯狂 / 总结性疯狂表（CoC） |
| `dv [--character <名字\|id>] [--init] [--force]` | 派生数值；`--init` 初始化资源轨 |
| `dmg <表达式> [--character <名字\|id>]` | 武器伤害（按规则书伤害加值表组合 DB） |
| `opp <A值> <B值>` | 对抗检定 |
| `ak new｜add <选项...>｜del <选项>｜get｜show｜clr` | 随机抉择（`get` 结束本轮） |
| `name [cn\|jp\|en] [数量]` | 随机姓名（数据包姓名表） |
| `en <技能/属性名> [目标值\|成长量] [原因...]` | 成长检定：成长后直接写卡；整数=比较目标，表达式=自定义成长量 |
| `ri [±加值\|表达式] [名称]` | 先攻掷骰并加入列表（`+5` 加值、`80` 固定值、`2DK`=2d20 取高） |
| `init` / `init list` / `init clr` | 查看（从高到低）/清空先攻列表 |
| `table` / `table new <桌名> [--system <规则书>]` / `table use <桌名\|id>` / `table list` / `table system <规则书>` / `table del <桌名\|id>` | 桌/团管理：同一数据根可并行多桌，每桌独立默认规则书、先攻、牌堆、当前卡；角色卡全桌共享。`table <桌名>` 等价于 `table use`；新建自动切换；当前桌不可删 |
| `deck new/set/show/reset/clr` | 牌堆管理（`::3::市民` 表示三张同名卡）；每桌独立，`show` 同时显示牌库与弃牌堆 |
| `draw <牌堆> [数量] [放回]` | 抽牌：默认不放回，抽出的卡进弃牌堆；加 `放回`（`back`/`replace`）= 抽后放回、牌库不变 |
| `deck put <牌堆> <卡面...>` | 把卡面从弃牌堆放回牌库并重洗（弃牌堆里没有的卡面会报错） |
| `rh ...` / `drawh ...` | 暗骰/暗抽需要聊天平台适配器，CLI 明确报错 |

规则：

- `st` 一次调用内不能混用 `=`/`:` 赋值与 `±` 增减；`±` 右侧必须通过纯骰子表达式解析（不接受 `#` 重复与比较后缀）。
- **桌/团与团务工具**：先攻列表与牌堆存在数据根的数据库里（`<数据根>/dicegirl.db`），且**按桌隔离**——`table new` 开新桌后，各桌的默认规则书、先攻、牌堆、当前角色互不影响；角色卡本身全桌共享。手册标注这些指令「仅群聊有效」，但排序与抽牌本身是通用跑团工具，CLI 用本地状态承接（单进程隐式使用「默认桌」，也可以直接用 `table` 系列指令开多桌）。
- **牌库/弃牌堆**：普通 `draw` 把卡从牌库移到弃牌堆，抽空后报错（`deck reset` 合并重洗或 `deck put` 放回）；`draw … 放回` 每次独立随机、不动两堆；`deck put <牌堆> <卡面...>` 只接受弃牌堆中存在的卡面（按张数核对），放回后立即重洗。
- **掷骰统计**（`pc stat`）只统计**检定**（有目标与成败判定的掷骰：技能/属性检定与资源检定）；原始掷骰 `r` 与成长检定 `en` 不计入。统计终身累计，按团重置用 `pc stat clr`。
- **具名宏**保存在角色卡上（`macros` 段），`&` 前缀只存在于界面层：`.st &名=表达式` 保存，`.st del &名` 删除，`.r 名`（可带 `n#` 前缀）直接掷它。宏在应用层解析：先查宏、再按普通表达式；宏**不嵌套**（宏里的表达式不会再展开宏）；保存时做一次语法校验，非法表达式直接报错。
- `st K±...` 依次解析属性（规则书属性）、技能（角色卡已有值）、资源轨（已初始化的 hp/mp/san 等，大小写不敏感）；解析不到即 `INVALID_ARGUMENT`。
- 损失对 `<成功>/<失败>` 必须恰好一个 `/` 且两侧非空；`sc`/`rc` 默认只报告不写卡，加 `--apply` 才持久化（资源轨越界按规则书 clamp 与轨道上限夹取）。
- `sc`/`rc` 损失对之后的第一个参数若是整数则读作**当前值**（只影响本次检定目标，不写轨），其余参数是原因文本。
- 独立的 `+n`/`-n` token 一律视为奖励/惩罚骰数而非负数目标值；目标值可以是整数 token（`ra 60`），也可以带内联修正（`ra 15+1`，等价于 `ra 15 +1`）。
- `ra` 的 `n#` 前缀只属于**检定连掷**（1–9 轮，每轮独立掷骰与分级）；`r` 的 `n#` 属于纯掷骰语句层（上限 10），两者解析与输出容器互不影响。连掷不适用 `sc`/`rc` 资源检定（可能 `--apply` 写卡，多轮是有状态流程）。
- **角色引用**：`pc` 子命令与 `--character` 接受**角色名或 ID**。先按 ID 精确匹配，再按名字精确匹配，最后忽略大小写匹配；名字唯一（建卡/改名拒绝重名），找不到报 `NOT_FOUND`，同名多卡报 `INVALID_ARGUMENT` 并提示改用 ID。`pc active` 仍输出 ID，方便脚本使用。
- 带前缀的未知指令（如 `.xyz`）按「未知短指令」报错；不需要前缀的未知词也直接报错，不会再落入另一套命令层。

聊天中常见的一次调用：

```text
.r 3#1d6 手枪连射
.ra 3#心理学
.ra 心理学+2
.ra 15+1
.ra 体质*5
.dnd
.sc 0/1d10 --apply
.st san-1d6
.pc show 调查员甲
.pc use 调查员乙
.pc list
```
