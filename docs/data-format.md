# 数据格式

## 1. 规则书 `rulebooks/*.yaml`

一个游戏对应一份自包含的规则书。内置书 `rulebooks/coc7.yaml`（默认）与 `rulebooks/dnd5e.yaml` 编译时 `go:embed` 进二进制；用户书放在 `<数据根>/rulebooks/`（`.yaml`/`.yml`），启动时与内置书走同一个 `engine.ParseBook` 加载/编译路径。未知字段**严格拒绝**（yaml decoder `KnownFields(true)`）；重复 YAML 键由 yaml.v3 直接报错。

```yaml
# 身份头（rules.Manifest.Validate 校验）
schema_version: "1"          # 规则书 schema，当前固定 "1"
id: "coc7"                   # ^[a-z0-9]+([._-][a-z0-9]+)*$
name: "Call of Cthulhu 7th Edition"
system_version: "0.1.0"      # 语义版本 x.y.z
rules_version: "7e"          # 非空，规则版本标识
description: "..."
default: true                # 可选：未指定系统时使用本规则书（多本时生效；后加载者胜）
commands: ["coc"]            # 可选：本规则书应答的短指令别名（人物作成入口），全局唯一

# 属性定义（顺序即展示顺序；id 唯一，大小写不敏感匹配，name 可中文）
# aliases 是规则书数据里的同义词：解析顺序为 规范 ID → aliases → 中文名
attributes:
  - { id: "STR", name: "力量", aliases: ["臂力"] }

# 技能表：每条恰好给 base（非负常量）或 base_expr（属性公式）之一
skills:
  psychology: { base: 10, name: "心理学", aliases: ["心理"] }
  dodge:      { base_expr: "DEX/2", name: "闪避" }

# 三个闭区间，需 0 <= min <= max
limits:
  characteristic: { min: 0, max: 99 }
  skill:          { min: 0, max: 99 }
  target:         { min: 0, max: 99 }   # 检定目标允许 0（SAN 0 时的理智检定）
  # 可选：目标值算式（如 `体质*5`）的结果允许范围；省略时等同 target
  computed_target: { min: 0, max: 999 }

mechanic:
  # 形态一：内置机制族（percentile = d100 越小越好；d20 = d20 越大越好）
  type: percentile            # percentile / d20
  max_bonus_penalty: 100      # 奖励/惩罚骰数量上限（正整数）

  # 形态二：通用表达式机制（与 type 二选一）——检定掷什么骰子、怎样算成功
  # 全部由数据声明，引擎没有对应游戏的分支：
  # roll: "1d20"              # 一次检定掷的骰子表达式（骰子引擎语法）
  # meets: "roll >= target"   # 布尔公式，仅可见 roll/target；显式难度用它判成败
  # bonus_keep: highest       # 奖励骰：n+1 次独立掷骰中取高/低（默认 highest）
  # penalty_keep: lowest      # 惩罚骰：n+1 次独立掷骰中取高/低（默认 lowest）
  # label: "d20"              # 可选：结果解释里的掷骰标签（默认即表达式）

# 成功等级：按声明顺序求值，首个 when 为真者胜；末位必须无 when（兜底）
# when 是布尔公式，仅可见变量 roll 与 target
checks:
  levels:
    - id: fumble
      name: "大失败"
      when: "(target < 50 && roll >= 96) || (target >= 50 && roll >= 100)"
    - { id: extreme, name: "极难成功", when: "roll <= target/5" }
    - { id: hard,    name: "困难成功", when: "roll <= target/2" }
    - { id: regular, name: "普通成功", when: "roll <= target" }
    - { id: fail,     name: "失败" }
  # 可选：显式难度（Dice! 困难/极难/自动成功，BCDice h/e/c 同源）。
  # 省略整段的规则书不接受任何难度参数。
  difficulties:
    - { id: extreme, name: "极难",     expr: "target/5" }
    - { id: hard,    name: "困难",     expr: "target/2" }
    - { id: auto,    name: "自动成功", auto: true, success_level: regular }

opposed:
  fumble_level: fumble         # 必须引用已声明等级
  tie_break: higher_target     # none | higher_target

# 可选：伤害加值表，行必须连续（本行 min = 上行 max+1），bonus 是合法骰子表达式
damage_bonus:
  key: "STR + SIZ"
  rows:
    - { min: 2, max: 64, build: -2, bonus: "-2" }

# 可选：派生属性，按声明顺序求值；整型结果进入后续公式命名空间
derived:
  values:
    - { id: build,  name: "体格",     lookup: damage_bonus, field: build }
    - { id: db,     name: "伤害加值", lookup: damage_bonus, field: bonus }
    - { id: max_hp, name: "HP",      formula: "(CON+SIZ)/10" }
    - id: mov
      name: "MOV"
      switch:                            # switch 必须以 default 收尾
        - { when: "STR<SIZ && DEX<SIZ",   value: 7 }
        - { default: true,                value: 8 }
  vitals:                                # 可初始化的资源轨
    - { resource: hp, max: max_hp, initial: max_hp }
  initial_attributes:                    # --init 时补齐缺失属性
    - { attribute: LUCK, value: "min(POW*5, 99)" }

# 可选：检定驱动的资源损失
resource_checks:
  sanity:
    label: "理智"
    resource: san
    fallback: "min(POW*5, 99)"           # 轨不存在时，用属性公式求目标
    success_level: regular               # 达到该级或更好算成功（大失败永不算）
    loss: { success: "0", failure: "1d10" }   # 非负整数或骰子表达式；负值表示恢复
    fumble_loss: max                     # 可选（默认 normal）：大失败时失败损失取静态最大值
    range: { min: 0, max: 99 }
    clamp: { min: 0 }

# 可选：随机表（游戏内容）。roll 决定定义域，行必须覆盖它且不重叠；
# 行文本可内嵌 {1d10} 子掷占位符。
tables:
  - id: temporary_insanity
    name: 临时疯狂症状
    roll: "1d10"
    rows:
      - { range: "1", text: "失忆：清醒后完全不记得刚才发生了什么" }
      - { range: "2", text: "暴力：攻击周围最近的人或物" }
      - { range: "default", text: "其它症状" }

# 可选：成长检定。condition 只可见 roll（1d100）与 target（比较目标）；
# overrides 按已声明的技能/属性覆盖默认规则（省略的字段继承默认）。
growth_checks:
  default:
    condition: "roll <= target"
    gain: "1d10"
    cap: 99
  overrides:
    - { key: firearms_handgun, gain: "1d10", cap: 50 }

# 可选：人物作成。characteristics 是骰点法（每个属性一条表达式），
# variants 是同游戏的其它版本，point_buy 是购点法（详细作成）。
generation:
  default_variant: "7"
  name: "CoC 7 版"
  characteristics:
    - { attr: STR, expr: "3d6*5" }
    - { attr: SIZ, expr: "(2d6+6)*5" }
  variants:
    - id: "6"
      name: "CoC 6 版"
      characteristics:
        - { attr: STR, expr: "(2d6+6)*5" }
  point_buy:
    attributes: [STR, CON, SIZ, DEX, APP, INT, POW, EDU, LUCK]
    min_value: 15
    budget: "EDU*20 + INT*10"    # 总点数（用买到的 EDU/INT 计算）
    costs:                       # 值在 [min,max]：base + points × ceil((值-from)/per)
      - { min: 15, max: 49, from: 15, base: 7,  points: 1, per: 2 }
      - { min: 50, max: 79, from: 49, base: 24, points: 1, per: 5 }
      - { min: 80, max: 89, from: 79, base: 30, points: 3, per: 5 }
      - { min: 90, max: 99, from: 89, base: 36, points: 7, per: 5 }
    priorities:                  # 非交互自动分配的顺序与目标值
      - { attr: EDU, value: 60 }
      - { attr: INT, value: 50 }
    skills:
      occupation_cap: "EDU*5"
      interest_cap: 75
```

`fumble_loss`：`normal`（省略默认，大失败照常掷失败损失）或 `max`（大失败不掷骰，失败损失取理论最大值——定值即其本身，骰子式取所有保留骰面之和，如 `1d4+1`→5、`1d10`→10；由骰子表达式的静态界 `dice.ExprBounds` 求出，不消耗随机数）。这是 CoC 7e 理智检定与 Dice! `.sc` 共用的约定。

`difficulties`（可选）：显式难度修饰，每条恰为二选一形态。公式形态给 `expr`（只可见变量 `target` 的整型公式，如困难 `target/2`、极难 `target/5`）：判定时先把目标值改写为公式结果，**大失败区间跟随改写后的目标值**（BCDice 约定：99 的困难档目标为 49，96–100 即大失败），成功档位仍按**基础目标值**的等级梯读取，达不到难度阈值的骰值降为兜底失败。自动形态给 `auto: true` 加 `success_level`：目标值不改写，仅按基础目标判大失败，其余骰值一律映射为所声明的成功等级。运行期改写后的目标值超出 `limits.computed_target`（省略时等同 `limits.target`）报 `INVALID_ARGUMENT`；难度关键词（id 或 name）在调用处按声明解析，引擎与 CLI 均不写死任何中文难度名。

`aliases`（可选，属性与技能都有）：同义词列表，解析顺序为 **规范 ID → aliases → 中文名**，别名大小写不敏感。别名不得与其它条目的规范 ID 或名称重复，重复/遮蔽在编译期报 `CONFIG_INVALID`。

`limits.computed_target`（可选）：**目标值算式**的结果允许范围（如 Dice! `.rc 体质*5`：卡上 60 → 目标 300）。省略时等同 `limits.target`；算式结果越界报 `INVALID_ARGUMENT`（退出码 2）。

编译校验（`engine.ParseBook`，`.sys validate` 重新编译源字节）：manifest 完整合法；属性非空不重复；三个范围自洽；技能有名、base 非负、base/base_expr 互斥且公式编译通过；mechanic 必须恰有 `type` 或 `roll` 之一（`type` 为已实现族 percentile/d20，`roll` 为合法骰子表达式且 `meets` 是只引用 roll/target 的布尔公式，`bonus_keep`/`penalty_keep` 取 highest/lowest），上限为正；等级 ≥2、id/name 不重复、仅末位可无 when、when 为布尔公式且只引用 roll/target；difficulties（若有）id/name 不重复、expr 与 auto 互斥且恰具其一、expr 为只引用 target 的整型公式、auto 的 success_level 必须引用已声明的非大失败等级；opposed 引用存在的等级且 tie_break 合法；伤害表连续、bonus 表达式合法；derived 每条恰为 formula/switch/lookup 之一、switch 有 default 且收尾、lookup 字段合法、公式类型与变量白名单通过；vitals/initial_attributes 引用存在；资源检定 success_level 存在、loss 合法、fumble_loss 仅可取 normal/max、range/clamp 自洽；随机表 id 唯一、roll 合法、行不重叠不越界且覆盖定义域；成长检定的条件公式、成长量表达式与覆盖项 key 引用合法。公式语言详见 [guide.md](guide.md) 第 2.10 节。

`tables`（可选）：随机表。`roll` 是纯骰子表达式并决定表的定义域；`rows` 的 `range` 支持单点（`"3"`）、区间（`"2-5"`）与兜底（`"default"`，必须是最后一行）。行文本内嵌的 `{表达式}` 是子掷占位符，命中该行时掷骰并替换成总值。

`growth_checks`（可选）：成长检定。`default` 必填 `condition`（布尔公式，仅可见 `roll`/`target`）与 `gain`（骰子表达式），`cap` 可选（省略或 0 表示不限）；`overrides` 的 `key` 必须引用已声明的属性/技能（id、中文名或别名均可），省略的 `condition`/`gain`/`cap` 继承默认规则。

`generation`（可选）：人物作成。`characteristics` 声明骰点法（每个属性一条骰子表达式，可选 `min`/`max` 夹取）；`variants` 声明同游戏的其它版本（各自可覆盖 `characteristics` 与 `point_buy`）；`default_variant` 指向默认版本（若该 id 未显式声明，则默认属性自己构成一个变体）。`point_buy` 声明购点法：`attributes` 是参与购买的属性、`min_value` 最低值、`budget` 是点数池公式（只可见属性 id）、`costs` 是费率档（`base + points × ceil((值-from)/per)`，`from` 默认取 `min`，档位不得重叠）、`priorities` 是非交互自动分配的顺序与目标值、`skills` 声明各技能上限。校验：属性必须已声明、表达式可解析、费率档合法、`budget` 公式合法、`priorities` 引用存在；否则 `CONFIG_INVALID`。

## 2. 数据包 data/datapacks/&lt;id&gt;.yaml

数据包承载**与任何游戏规则无关**的内容（随机姓名、地名等），与规则书使用同一套随机表 schema，但走独立的加载/编译路径。

```yaml
schema_version: "1"
id: "names"                     # 与规则书 id 同一命名规则
name: "随机姓名数据包"
description: "..."
tables:                         # 与规则书 tables 同构
  - id: cn_surname
    name: 中文姓氏
    roll: "1d20"
    rows:
      - { range: "1", text: "赵" }
name_sets:                      # 把若干表组合成一个名字
  - id: cn
    name: 中文姓名
    default: true               # 可选：默认姓名集
    parts:
      - { table: cn_surname }   # 掷一次该表并取行文本
      - { text: "·" }           # 字面量
```

- 内置数据包在 `datapacks/*.yaml`（`go:embed` 打包）；用户数据包在 `<数据根>/datapacks/*.yaml`。
- 校验：id/name 必填、`schema_version` 必须是 `"1"`、name set 的 id 唯一、`parts` 非空且每项只给 `table` 或 `text` 之一、引用的表必须存在；否则 `CONFIG_INVALID`。
- `dicegirl .sys validate` 会连同规则书一起重新编译全部数据包。

## 3. 角色卡（数据库里的 YAML 文档）

角色卡**不再是一个文件**：它存在数据根的 SQLite 数据库 `<数据根>/dicegirl.db` 里（见第 3 节末的表结构）。卡的内容仍然是一份 YAML 文档（`.pc export` 导出的就是它），所以**交换格式没有变**：

```yaml
schema_version: "1"
id: "c-a1b2c3d4"             # 程序生成：c- + 8 位十六进制
name: "调查员甲"
system_id: "coc7"           # 规则书 id
attributes: { STR: 50, CON: 60 }
skills: { psychology: 60 }
resources:                   # 可选；current <= max
  san: { current: 55, max: 99 }
macros:                      # 可选；角色卡上的具名骰子表达式（.st &名=表达式）
  沙漠之鹰: "1d10+1d6+3"
metadata: {}                 # 可选，字符串扩展元数据（default_die_sides 可放这里）
extra: {}                    # 可选，规则相关的自由扩展（唯一允许的 map[string]any 边界）
created_at: "2026-10-21T10:00:00Z"
updated_at: "2026-10-21T10:05:00Z"
```

规则：

- ID 匹配 `^[a-z0-9][a-z0-9-]{3,63]$`，不接受显示名、路径分隔符或 `..`。
- 通用校验（`character.Validate`）：schema 版本、ID、名称（≤64 字符）、system_id、非负数值、resource 不越上限。
- 规则书校验（`engine.Book.ValidateCharacter`）：属性必须是规则书已声明属性且在 characteristic 范围内；技能键允许个人专精（规则书未列出的键也可接受），值必须在 skill 范围内。
- 技能在角色卡上以**规范键**（技能 ID，如 `psychology`）保存；中文名只用于解析用户输入和展示。
- `macros` 是可选的具名骰子表达式表（键为宏名，值为骰子表达式）；旧卡没有该段时按空表处理（schema_version 不变，仍是 `"1"`）。宏在保存时做一次语法校验。
- `metadata` 是卡上的字符串扩展点（`.pc meta <键> [值]` 可设置；例如 `default_die_sides` 覆盖该卡的默认骰面数）。

## 4. 数据库 <数据根>/dicegirl.db

可写状态都在一个 SQLite 数据库里（纯 Go 驱动 `modernc.org/sqlite`，无需 cgo）。**规则书与数据包仍是 YAML 文件**：它们启动时读一次、由人手工编辑，不属于热路径。

| 表 | 内容 |
| --- | --- |
| `cards` | 角色卡：`id`/`name`/`system_id` 为查询列，`body` 是整张卡的 YAML 文档 |
| `card_backups` | 每次更新前的旧版本（保留份数由 `DICEGIRL_BACKUP_KEEP` 决定） |
| `stat_totals` | 每张卡的汇总：检定数、成功/失败/大失败、骰值总和、最好/最差 |
| `stat_keys` | 按技能/属性/资源检定的同样汇总 |
| `stat_levels` | 按成功等级 code 的次数分布 |
| `stat_history` | 最近 50 条检定明细（每卡有界） |
| `state` | 本地状态：当前角色、默认骰面数、随机抉择选项 |
| `encounter_entries` | 当前遭遇的先攻列表（按录入顺序） |
| `decks` | 牌堆：定义与剩余队列 |
| `meta` | 内部标记（如迁移是否已完成） |

- 每次检定只写一个事务（汇总 + 明细 + 裁剪历史），并发写不会丢更新。
- 统计只记**检定**（有目标与成败判定的掷骰）：技能/属性检定与资源检定；原始掷骰 `r` 不记，成长检定 `en` 不记（它不是成败判定）。
- 统计是**终身累计**，按团重置用 `pc stat clr`（清空当前卡的统计）。

## 5. 从旧版 YAML 数据根迁移

旧版本把角色卡放在 `characters/<id>.yaml`、状态放在 `state.yaml`、先攻放在 `encounter.yaml`、牌堆放在 `decks.yaml`。首次用新版本打开同一个数据根时：

- 这些文件会被**导入数据库**（角色卡、当前角色、默认骰面数、抉择选项、先攻、牌堆）；
- 原文件被**移到 `<数据根>/legacy/`**（不删除，出问题可回退）；
- 迁移只做一次（`meta` 表里有标记），之后不会再重复导入。

## 6. 错误读取分类

| 情况 | 错误码 |
| --- | --- |
| 角色卡不存在 | `NOT_FOUND` |
| 规则书 id 未加载 | `RULE_NOT_FOUND` |
| YAML 解析失败 / 缺 schema_version | `INVALID_DATA`（角色卡）/ `CONFIG_INVALID`（规则书） |
| schema_version 不受支持 | `UNSUPPORTED_SCHEMA_VERSION` |
| 字段语义不合法 | `VALIDATION_ERROR` |
| 权限或其他 I/O 失败 | `STORAGE_READ_FAILED` / `STORAGE_WRITE_FAILED` |
