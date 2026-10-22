# Dice! 功能对齐 TODO

> 制定于 2026-10-21（P0+P1 完成后）。目标：在**不违反两条架构边界**（游戏规则只能是规则书 YAML；聊天语法只存在于界面层 `cmd/dicegirl/cli`）的前提下，对齐 Dice! 2.6.5 手册中的**跑团相关**功能。
>
> **现状补充（2026-10-21）**：CLI 只保留短指令层，长命令已删除（本文提到 `check --repeat`、`character generate`、`table roll`、`config validate` 等处均为历史记录）；人物作成的别名改由规则书的 `commands` 声明；角色一律用**名字**引用。当前命令表见 [guide.md](guide.md) 第 3 节。
>
- 范围：手册第 1–10 节里的掷骰、检定、人物卡、人物作成、团务工具（先攻/牌堆/抉择/随机名）。
- **明确排除**：群交互/投递类（暗骰私聊、围观、群-卡绑定、战报采集）与非跑团管理类（群管、授权、欢迎语、jrrp、权限）——见文末「排除清单」，这些随未来聊天平台适配器处理或永不实现。
- 图例：层级 🎲=internal/dice 通用层｜📖=规则书 YAML schema（internal/engine）｜🧩=internal/app 用例/存储｜💬=cmd 短指令别名层；规模 S/M/L。

## 已对齐（P0/P1/P2.1–P2.6/P3/P4/P5/P6/P7/P9/P10，2026-10-21）

- `.r` 主体：多骰组四则、`kh/kl/dh/dl`（超集）、`n#`（1–10 轮）、比较后缀、原因文本（仅表达式之后）、乘号别名 `X`、裸 `r` 掷默认骰、`.rs` 精简模式、>20 颗骰排序展示。
- `.ra`：技能/属性/显式目标值、卡上取值/基础值、奖励惩罚骰（独立 `+2/-1` token 与后缀 `心理学+2`）、**目标值算式**（`体质*5`、`心理学/2`、`--mod -10`，与奖惩骰语义分离）、规则书同义词（`aliases`）。
- `.sc`/`.rc`：`成功/失败` 损失对、规则书声明的资源检定、大失败静态最大损失（`fumble_loss: max`）、负损失恢复 SAN、显式当前值、原因文本、`--apply` 显式写回。
- `.st`：`show`、批量 `K=V` 赋值（`:` 同义）、`K±整数/骰子表达式` 增减（属性/技能/资源轨）、`show <键>`、`del <键>`、`clr`。
- `.pc`：`list`/`show`；长指令另有 create/cpy/del/use/validate/export/import、原子写与备份（Dice! 没有）。
- `.ti`/`.li`：规则书声明的随机表（掷骰选行、行文本可内嵌 `{1d3}` 子掷）。
- `.en`：成长检定（掷 1d100 判定，成长后直接写卡）。
- `.ak`：随机抉择（本地状态里的选项，等概率抽取，抽取即结束本轮）。
- `.name`：随机姓名（数据包里的姓名表，非规则内容）。
- `.coc`：人物作成（骰点法/购点法、6/7 变体、批量），`.pc redo` 重掷当前卡。
- `.w`/`.ww`：骰池（通用算子 `!`/`r`/`c` 的薄别名），`.st &名=表达式` 保存具名宏、`.r 名` 掷它。
- `.ri`/`.init`：先攻列表（本地遭遇状态）；`.deck`/`.draw`：牌堆与不放回抽牌。
- `.pc nn`/`.pc cpy`/`.pc redo`/`.pc build`/`.pc clr`：角色卡的重命名、属性覆盖复制、指定卡重掷、清空全部卡。
- `.pc stat`：每张卡的掷骰统计（只统计检定，终身累计，可清空）；持久化改为「冷数据 YAML / 热数据 SQLite」。
- 对抗检定（我们独有）、伤害+DB 查表、派生属性与生命条初始化。
- `.rh`：已识别并明确报错「需要聊天平台适配器」。
- `.setcoc` 式房规开关：**决定不做**，用规则书变体 YAML 表达。
- 显式难度（P2.1）：`checks.difficulties`（困难/极难公式难度 + 自动成功），`ra 困难心理学` 前缀/独立关键词与 `check --difficulty`。
- 检定连掷（P2.2）：`ra 3#心理学`、`3#p`/`3#b2` 简写，长命令 `check --repeat`，每轮独立分级。

---

## P2：检定与掷骰体验补全　✅ 全部完成（P2.1–P2.6，2026-10-21）

### ~~P2.1 显式难度：困难 / 极难 / 自动成功~~　✅ 已实施（2026-10-21）

对应 `.rc 困难智力 99`、`.rc 极难心理学`、`.rc 自动成功爆破`（BCDice 的 `CC<=60h/e/c` 同源）。

- schema：`checks.difficulties` 列表，每条二选一——`{id, name, expr}`（expr 为只可见 `target` 的整型公式；coc7 声明困难 `target/2`、极难 `target/5`）或 `{id, name, auto: true, success_level}`（非大失败即映射到该等级）。未声明该段的规则书不支持难度参数。
- 判定语义（实施时定稿）：公式难度的**大失败区间跟随改写后目标**（BCDice 约定：99 的困难档 49，96–100 大失败），成功档位按**基础目标**等级梯读取，达不到阈值降为兜底失败；自动成功只按基础目标判大失败。
- 💬 `ra` 难度关键词前缀或独立 token（`ra 困难心理学`、`ra 极难 心理学 60`、`ra 自动成功心理学`，长名优先），长参数 `check --difficulty hard|困难`；关键词全部来自规则书声明，无硬编码。`rc` 资源检定不接难度（Dice! 同源语法中该位置实为奖惩骰语义，资源检定已有 `--bonus/--penalty`）。
- JSON 纯增量字段 `difficulty_id`/`difficulty_name`/`base_target`；加载期非法声明 `CONFIG_INVALID`，运行期未知关键词或改写后目标越界 `INVALID_ARGUMENT`（退出码 2）。
- 验收（已由 `internal/engine/difficulty_test.go` 与 `tests/integration` 的 `TestShortRADifficulty` 覆盖）：困难 60→30、极难 60→12；53 在困难档失败、自动成功档普通成功；96 对目标 40 的自动成功仍为大失败；未知关键词退出码 2。

### ~~P2.2 检定连掷 `n#技能名`~~　✅ 已实施（2026-10-21）

对应 `.ra 3#心理学`、`.rc3#p 手枪`（连掷轮数上限对齐 Dice! 的 9）。

- engine 新增 `CheckMany`/`CheckBatchResult`（常量 `MaxCheckRepeat = 9`）：目标值解析一次，逐轮调用既有 `Check`，独立掷骰、独立分级（显式难度、奖惩骰语义逐轮复用，无特判）；越界 `INVALID_ARGUMENT`。
- app 新增 `RunCheckBatch`（`CheckTarget.Repeat`），目标解析经抽取的 `resolveCheck` 与单次路径共用；长命令 `check --repeat N`。
- 💬 `ra` 名称前可选 `n#`：`ra 3#心理学`、独立 token `ra 3# 心理学 60`、Dice! 简写 `ra 3#p 心理学`/`ra 3#b2 心理学 60`（`b`=奖励、`p`=惩罚，省略颗数=1）；与难度关键词两种次序都支持（`ra 3#困难心理学`、`ra 困难 3#心理学`），与名称后缀奖惩骰可叠加。`1#` 扁平化为普通单次输出；`0#` 退出码 2。
- 资源检定 `sc`/`rc` **不加连掷**：Dice! 的 `.rc` 是技能检定（对应本项目 `ra`）；资源检定可能 `--apply` 写卡，多轮属于有状态多步流程，超出本条范围。
- JSON 新增 `kind:"check_batch"` 容器（`repeat`/`checks[]`），单次契约不变；纯掷骰 `r 3#1d6` 走独立的语句层，互不影响。
- 验收（`internal/engine/check_batch_test.go`、`cmd/dicegirl/cli/short_test.go`、`tests/integration` 的 `TestShortRABatch`）：3 轮 3 条分级、`3#p` 每轮带惩罚骰候选、困难连掷目标 30/基础 60、10# 与 0# 退出码 2、`1#` 扁平化。

### ~~P2.3 目标值算式修正（区别于奖惩骰）~~　✅ 已实施（2026-10-21）

对应 `.rc 体质*5`（CoC6 属性×5）、`.rc 敏捷-10`（目标值 ±10）。

- **语义冲突（已决策）**：已发布的 `心理学+2`/`-1` 后缀按 BCDice 约定=奖惩骰（有 README/测试/调研文档锁定，不改）。因此：
  - 后缀 `*n`、`/n` 一律是**目标值算式**（不与奖惩骰冲突），如 `ra 体质*5`、`ra 心理学/2`。
  - 目标值的加减修正走显式长参数 `--mod -10`（保持已发布 ± 后缀语义不破坏契约）。
- 实现：`engine.TargetAdjust`（Multiply/Divide/Add）进入 `CheckInput`，引擎在难度改写**之前**应用；`limits.computed_target`（可选，默认等同 target）约束算式结果范围，coc7 声明 0–999 以支持 `体质*5`→300。
- 💬 解析顺序：先剥离 `±n`（奖惩骰），再在剩余名称里找 `*n`/`/n`（目标算式），二者可叠加（`体质*5+2`）。`ra` 与 `check` 都支持 `--mod`。
- JSON 纯增量字段 `target_original`/`target_adjust`；结果标注原始值与修正后目标；修正后目标越界报 `INVALID_ARGUMENT`（退出码 2）。
- 验收（`internal/engine/adjust_test.go`、`tests/integration` 的 `TestShortRATargetArithmetic`）：卡上 CON=60 → `ra 体质*5` 目标 300、`ra 心理学/2` 目标 30、`ra 敏捷 --mod -10` 目标 45、与难度叠加、`体质*100` 越界退出码 2。

### ~~P2.4 `.sc` 三个小补齐~~　✅ 已实施（2026-10-21）

- 可选的第三参数「当前 SAN」：`.sc 0/1 70`（显式覆盖轨值，仅本次检定；不写轨）。长命令等价 `resource sanity --current 70`。
- 损失对之后的原因文本：`.sc 1d10/1d100 直面外神`（多余位置参数收作原因，JSON 追加 `reason` 字段）。
- 负损失=恢复 SAN（手册允许、不鼓励）：`ParseLossSpec` 接受带符号值，`after = before - loss`，写回时按资源轨 max 夹取（引擎先按 `clamp` 与 `range` 收敛，应用层再按轨道 max 夹取）。
- 写回策略维持 CLI 显式 `--apply`（Dice! 自动写回是平台默认策略，留给适配器默认开启）。
- 验收：三项各有集成测试（`TestShortResourceExtras`）；恢复 SAN 不超过 max；JSON 契约只加字段（`check_target`、`reason`）。

### ~~P2.5 `.r` 系列便捷性~~　✅ 已实施（2026-10-21）

- 裸 `r`（无表达式）：掷默认骰（内置默认 d100）；`.set [面数]` 设默认面数（1–1000，写入 `state.yaml` 的 `default_die_sides`；角色 metadata 的 `default_die_sides` 优先），无参 `.set` 恢复默认。🧩
- `.rs <表达式>`：精简模式别名，只出总值不出每骰明细（渲染层开关，JSON 追加 `detailed:false`，结果结构不变）。
- 超过 20 颗骰自动排序展示（Dice! 渲染约定，保留/丢弃标记跟随各自骰子）。💬
- 乘号别名 `X`（`3d6X5`==`3d6*5`）：词法层把中缀位置单独的 `X`/`x` 当 `*`；不影响其它词法（`dx` 仍是未知词）。🎲
- **不做**：原因在表达式之前（`.r 原因 表达式` 与宏名/裸骰歧义大，收益低，保持「表达式必须是第一个参数」）。

### ~~P2.6 `.st` 残余操作与同义词~~　✅ 已实施（2026-10-21）

- `st show K` 单键查看；`st del K` 删一个属性/技能；`st clr` 清空当前卡数值（保留卡与 ID，写入前自动备份）。
- 分隔符 `:` 作为 `=` 的同义词（`.st 力量:50`）。
- 规则书同义词：属性/技能声明里增加可选 `aliases: [灵感, 智力]`（理智/SAN、侦查/侦察 等），YAML 数据驱动，不写进代码；resolve 顺序 规范 ID → aliases → 中文名；别名重复/遮蔽在加载期 `CONFIG_INVALID`。
- 验收：每个操作有集成测试（`TestShortSTOperations`）；`clr` 后 show 为空但卡仍在；别名只来自 YAML（`TestShortAliases`）。

---

## P3：声明式随机表（解锁整批 CoC 表指令）　✅ 已实施（2026-10-21）

对应 `.ti/.li`，并为 `.name`、BCDice 的 BMR/PH/MA 同类表提供通用机制。

- schema（已实施）：

  ```yaml
  tables:
    - id: temporary_insanity
      name: 临时疯狂症状
      roll: 1d10
      rows:
        - { range: "1", text: "..." }
        - { range: "2-5", text: "..." }
        - { range: "default", text: "..." }
  ```

  range 支持单点/区间/`default`（兜底行必须是最后一行）；行文本可内嵌次级掷骰记号 `{1d3}`，命中该行时掷骰并替换成总值（`subrolls` 给出明细）。
- 🧩 `RollTable(ctx, systemID, tableID)`：编译期校验 range 覆盖 roll 全域且不重叠（`CONFIG_INVALID`）；返回命中行与掷骰明细。结果 JSON：`kind:"table_roll"`（id/roll/row/text/subrolls）。
- 💬 长指令 `table roll <id>` / `table list`；短指令 `ti`→规则书表 `temporary_insanity`、`li`→`summary_insanity`（**别名在 cmd，表 id 与内容在 coc7.yaml**）。
- 数据：CoC7e 临时疯狂（1d10）与总结性疯狂（1d10 + 症状时长 1d10 子掷）两张表录入 coc7.yaml（文本为自写简述）。
- 验收：无角色也能掷公共表；非法表配置过不了 `config validate`（`internal/engine/tables_test.go` 覆盖重叠/缺口/越界/非法子掷）；表结果为结构化 JSON。

### ~~P3.1 成长检定 `.en`~~　✅ 已实施（2026-10-21）

- schema：`growth_checks: { default: { condition: "roll <= target", gain: "1d10", cap: 99 } }`，可按技能/属性用 `overrides` 覆盖（`key` 引用已声明条目，省略字段继承默认）。
- **条件方向的决策**：TODO 草案写的是 `roll > target`，实施时改为 **`roll <= target`**——CoC 7e 的规则是「掷 1d100，结果等于或低于当前技能值则成长」，草案的方向与规则相反。schema 保持通用（条件是任意布尔公式），规则方向只由规则书 YAML 决定。
- `.en 技能名 [显式值] [原因]`：d100 与当前技能值比较，成立则掷增长量并**直接写卡**（成长语义即写回；仍走备份/原子更新/校验），返回 before/after/gain；未成长也要给出结果。显式值覆盖比较目标（不改变卡上的值）。
- 验收：种子用例覆盖成长/未成长/封顶与显式目标；未知技能退出 2；JSON 批量友好（`kind:"growth_check"`）。

### ~~P3.2 随机抉择 `.ak` 与随机名 `.name`~~　✅ 已实施（2026-10-21）

- `.ak new/add/del/get/show/clr`：一轮选项的增删与均等随机抽取（`get` 即结束本轮）。选项状态存本地 `state.yaml`（`choice_options`），CLI 单用户足够。
- `.name [cn|jp|en] [n]`：随机姓名。数据走 P3 表机制，但**不是规则内容**：因此新增**数据包**概念 `datapacks/*.yaml`（内置 `names.yaml`，用户包在 `<数据根>/datapacks/`），与规则书同构的 `tables` + `name_sets`（把若干表组合成一个名字，用 `default` 标记默认姓名集）。`.nnn`（改平台昵称）排除。

---

## P4：人物作成 `.coc`　✅ 已实施（2026-10-21）

对应 Dice! 手册的 `.coc([7/6])(d)([生成数量])`。

- **需求来源**：Dice! 2.6.5 手册「`.coc` 默认生成 7 版人物；`d`=详细作成且一次只能一个；Dice! 的 `.coc` 只做骰点法，购点法属变体规则；`.pc7 10` 用于「roll 十选一」」＋ CoC 7e 生成规则（骰点法表达式、购点法点数池/费率/技能上限）＋ 本文档原草案。
- schema（已实施）：`generation` 段 = `characteristics`（骰点法，每个属性一条表达式）+ `variants`（同游戏的其它版本，如 CoC6）+ `point_buy`（购点法：属性表、最低值、点数池公式、费率档、自动分配优先级、技能上限）+ `default_variant`。
- 🧩 `Book.Generate(ctx, GenerationInput{VariantID, Count, Detailed})`：骰点法逐条掷声明的表达式；购点法先买 EDU/INT（决定点数池），再按 `priorities` 顺序花点，买不动则降到买得起的最高值；剩余点数（`left`）留给技能。引擎只产出数值，不碰存储。
- 💬 `.coc [版本] [d] [数量]`（版本与 `d` 写在命令名后：`.coc6d`、`.coc7 10`；数量是单独参数：`.coc 10`；`d` 一次只能一个）、长指令 `character generate [--variant] [--count] [--detailed] [--name] [--active]`；生成物是**普通角色卡**（属性已写入，技能留空即基础值）。
- `.pc redo`（重掷当前卡）：清空属性与技能后重新生成，并重算生命/理智轨；`.pc build`（交互式建卡）不移植（已有非交互 `character create`）。
- **非交互购点法的取舍**：CLI 不做交互点选，按规则书声明的 `priorities` 自动分配并如实报告点数余额；技能点留给玩家用 `st K=V` 分配（上限由 `skills` 声明）。CoC6「哪三个属性取 2d6+6」由玩家自选，CLI 按声明顺序固定分配（已在 YAML 注释与 README 规则假设里标明）。
- `.dnd`：需要 D&D 规则书与独立生成式，等第二本规则书时再做，不预埋。
- 验收（`internal/engine/generation_test.go`、`tests/integration/p4_generation_test.go`）：种子可复现整套属性；变体选择生效；批量 n 张互不串骰；购点法点数/上限可核对；生成卡通过通用+规则书校验并可继续 `ra`/`st`；未知变体与非法数量退出码 2。

---

## P5：骰池/爆骰等通用算子与具名宏　✅ 已实施（2026-10-21）

### ~~P5.1 具名掷骰宏 `.st &名=表达式`~~　✅ 已实施（2026-10-21）

- 角色卡上保存命名表达式（`.st &沙漠之鹰=1D10+1D6+3`），`.r 沙漠之鹰` 调用；`st del &名` 删除、`st show` 列出（卡面也显示「宏：」段）。
- 存储：角色卡 schema 增加性字段 `macros: map[string]string`（schema_version 仍 "1"，旧卡零值兼容）；保存时做一次语法校验，非法表达式报错。
- 宏解析在 app 层（先查宏，否则按普通表达式），💬 不认识 `&` 语义以外的任何东西；宏**不嵌套**（宏里的表达式不再展开宏）；`.r 3#名` 的重复前缀同样可用；未知宏名报 `INVALID_ARGUMENT`（退出码 2）。
- 验收：`tests/integration/p5_macro_pool_test.go`（保存→掷→重复→列出→删除→未知宏报错）。

### ~~P5.2 爆骰/重投/成功计数通用算子~~　✅ 已实施（2026-10-21）

W-Wing `.w/.ww`（d10、≥加骰阈值追加、统计 ≥8 数量）本质是「骰池计数 + 爆骰追加」；同期评估 BCDice 的 R/U/T 记号与 Lua 扩展里可一般化的机制。**不做** `.rdx/.rsr/.rnc/.rfc/.rbh/.rws` 这些逐系统指令（系统包反模式），只把通用机制抽进骰子语言：

- 骰组修饰符（风格同 `kh/kl/dh/dl`，可组合、可与保留/丢弃共存）：
  - 爆骰 `!`（`NdM!` / `NdM!T`）：达到阈值的骰追加同面骰，追加上限 `dice.Limits.MaxExplodeDice`（默认 100/组）。
  - 重投 `r`（`NdMrT`）：低于阈值的骰重投，上限 `dice.Limits.MaxRerolls`（默认 10/颗）。
  - 成功计数 `c`（`NdMcT` / `c>=T` / `c<=T` / `c>T` / `c<T`）：本组值 = 满足条件的骰数（骰池语义）。
- 求值顺序：掷骰 → 重投 → 爆骰 → 保留/丢弃 → 取值；计数组的静态上下界为 `[0, 骰数]`（供「大失败取最大损失」这类静态求界）。
- 语句层改为「先整体按纯表达式解析，失败才拆分尾部比较」，使组内比较符（`5d10c>=8`）与语句层比较（`2d6+1>=7`）共存。
- 💬 `.w [个数]a[加骰参数]` / `.ww` 是通用算子的薄别名（= `Nd10!Tc8`，成功阈值 8 沿用 W-Wing 约定）：`w` 只出总数，`ww` 给出每颗点数。
- `.rdc`（d20 优/劣势）已可用 `2d20kh1/kl1` 表达，**无需开发**，文档给出对照表；`.rsr` 的「1 点过半=失误」属系统特定判定，等未来规则书机制表达，不做别名。
- 验收：`internal/dice/operators_test.go`（爆骰/阈值/上限/重投/计数四方向/与 keep 组合/边界/语句层共存）、`tests/integration/p5_macro_pool_test.go`；纯表达式与语句层边界保持；规则书 YAML 零改动可用。

---

## 追加：检定机制可插拔（2026-10-21，超出原 P1–P5 计划）

- 动机：原设计把「百分骰」写死在引擎里（`mechanic.type` 只接受 `percentile`），换一套机制（如 D&D 的 d20、掷值 ≥ DC）就得改引擎——正是本项目要避免的硬编码。
- 实现：新增 `engine.Mechanic` 接口（`ID`/`RollRange`/`Meets`/`Roll`/`Apply`），内置两族：`percentile`（CoC 的 d100，奖励骰取小、惩罚骰取大）与 `d20`（一颗 d20，奖励=优势取高、惩罚=劣势取低）；`mechanic.type` 选择族，未知族在加载期 `CONFIG_INVALID`（绝不静默回退）。
- 语义适配：成功等级仍是「首个 `when` 为真者胜」（百分骰写 `roll <= target`、d20 写 `roll >= target`）；**难度修饰的比较方向由机制决定**（`Meets`）——原先「档位比较」的写法只对「越小越好」的族成立，现改为「标签取基础目标值的等级梯 + 用机制方向校验改写后的阈值」，CoC 的既有行为逐例保持不变。
- 顺带修正：`damage --expr` 在**没有**伤害加值表的规则书上不再报错（直接掷表达式）；`check --target` 的规则书来源改为与其它分支一致（`--system` > 角色卡 > 唯一规则书）。
- 产出：示例书 `docs/examples/dnd-lite.yaml`（d20 机制、DC 检定、优势/劣势、难度改写 DC、能力调整值派生）；测试 `internal/engine/mechanic_test.go`、`tests/integration/mechanic_test.go`。

---

## P9：持久化重构（冷热分工）+ 掷骰统计　✅ 已实施（2026-10-21）

### 持久化重构

- **动机**：角色数据是**热数据**（每次检定都写），YAML 的「读-改-写整个文件」既浪费又会在并发下丢更新；规则书/数据包则是**冷数据**（启动读一次、人工编辑），用 YAML 正合适。于是划一条线：
  - **YAML（冷）**：`rulebooks/*.yaml`、`datapacks/*.yaml` —— 不动。
  - **SQLite（热）**：`<数据根>/dicegirl.db` —— 角色卡、掷骰统计、本地状态、先攻列表、牌堆、卡备份。
- 实现：`internal/storage` 只定义接口（存储缝），`internal/storage/sqlitedb` 是 SQLite 实现（纯 Go 驱动 `modernc.org/sqlite`，无 cgo；实测冷构建 +8 s、二进制 +3.6 MB）；`internal/app` 只依赖接口，换后端不动业务代码。
- 角色卡的**内容仍是 YAML 文档**（`cards.body`），`character export/import` 的交换格式不变 ✓；卡备份从「文件副本」变成 `card_backups` 表（份数仍由 `DICEGIRL_BACKUP_KEEP` 决定）。
- **一次性迁移**：旧数据根的 `characters/*.yaml`、`state.yaml`、`encounter.yaml`、`decks.yaml` 首次打开时导入数据库，原文件移到 `<数据根>/legacy/`（不删除，可回退），迁移只做一次（`meta` 表标记）。

### 掷骰统计（`.pc stat`）

- **口径**：只统计**检定**（有目标与成败判定的掷骰）——技能/属性检定与资源检定；原始掷骰 `r` 与成长检定 `en` 不计。
- **存储**：`stat_totals`（每卡汇总）、`stat_keys`（按技能/属性/资源检定）、`stat_levels`（按等级 code）、`stat_history`（最近 50 条明细，每卡有界）。每次检定写**一个事务**（汇总 + 明细 + 裁剪），并发不丢更新。
- **成功判定**：达到规则书声明的成功门槛（末位兜底失败之前的那一级）即为成功；大失败永远不算成功——都是规则书数据，不写死中文名。
- **会话**：终身累计，按团重置用 `pc stat clr`（不引入「会话」概念）。
- 命令：`pc stat [id]` / `pc stat clr` / `character stat [<id>]`；JSON `kind` 为 `card_stats`。

---

## P10：对照 Dice! 手册的补漏复查　✅ 已实施（2026-10-21）

逐条对照手册（2.6.5）后补上的三处明确缺口：

| 手册 | 之前 | 现在 |
| --- | --- | --- |
| `.en 幸运 +1D3/1D10 幸运成长`（Pulp 幸运成长） | 第二个参数只当比较目标，自定义成长量被当成原因文本 | 第二个参数是**骰子表达式**时作为自定义成长量（`/` 并列写法随机取其一）；整数仍是比较目标 |
| `.w 1d2+1a8`（骰子个数用表达式） | 只接受整数 | 骰子个数可以是整数或骰子表达式（掷一次定个数，上限 100） |
| `.coc`/`.dnd` 的 D&D 人物生成 | `.dnd` 没有对应规则书 | 内置 `rulebooks/dnd5e.yaml`；`.dnd`/`.dndd` 生成 D&D 5e 角色卡（4d6 丢最低 / 27 点购点） |

复查后**明确不做**（记录在案）：

- `.rules` 规则词典：Dice! 用它检索规则条目与「创建调查员的其他选项」；我们是文档/指南承担这个角色，规则内容仍在规则书 YAML 里。
- `.rb2`/`.rp2`（裸百分骰带奖惩骰）：百分骰的「十位候选」语义住在引擎的机制层，裸掷骰路径用不到；带奖惩骰的**检定**已有 `ra 心理学+2`。
- `.w` 的「卡上属性表达式」（`.ww 敏捷+剑`）：骰子语言没有「引用角色卡属性」的语法（规则书的公式语言才有）。
- `.rws` 式「保留点数 ≤S 的最高若干颗」：系统特定判定，通用算子家族不收。
- Dice! 自带的公共牌堆、`.pc new` 的「模板卡」、每群 10 个牌堆上限：内容/平台概念，本地单人版不需要（牌堆可由用户自建）。
- 原因文本放在表达式之前（`.r 原因 表达式`）：与宏名/裸骰歧义大，维持「表达式必须是第一个参数」的既定决策。

---

## P6：团务工具（本地单用户版）　✅ 已实施（2026-10-21）

### ~~P6.1 先攻列表 `.ri/.init`~~　✅ 已实施（2026-10-21）

手册标注「仅群聊有效」，但排序工具本身是跑团功能，CLI 用**本地遭遇状态**承接（无参与者权限）。

- `.ri [±加值|表达式] [名称]`：默认掷 d20；`+5`/`-1` 是加值，`80` 是固定值（不掷骰），其余是自定义骰子表达式（`2d20kh1`，并兼容 D&D 简写 `2DK`）。按结果从大到小入列。
- `.init`/`.init list` 查看、`.init clr` 清空；**平局按录入顺序**（对齐 Dice!）。
- 存储：`<数据根>/encounter.yaml`（`entries` 按录入顺序保存，展示时排序）。
- 长命令：`initiative add [--name] [--modifier] [--value] [--expr]` / `initiative list` / `initiative clear`。
- 验收：加值/固定值/自定义表达式三种入列；JSON 输出有序列表；clr 后为空；跨进程保持（`tests/integration/p6_party_test.go`）。

### ~~P6.2 牌堆 `.deck/.draw`~~　✅ 已实施（2026-10-21）

排除私聊暗抽与信任/权限后，剩余是通用不放回抽样服务：

- `.deck new 名=卡面1|卡面2`（重名用 `::3::市民` 记 multiplicity）；`.deck set 实例=公共名`、`.deck set 实例=range 下限 上限`（等差数列牌堆）；`.deck show [名]`、`.deck reset 名`、`.deck clr`。
- `.draw [牌堆] [数量]`：不放回抽取、不得超过剩余规模（超抽退出码 2）、抽空后提示 reset；剩余队列持久化（洗牌后顺序存储）。
- 「群成员牌堆」（`.deck set ... member`）排除；`.draw _名`/`.drawh` 暗抽排除。
- 存储：`<数据根>/decks.yaml`（**与草案的 `decks/<实例>.yaml` 不同**：牌堆名是自由文本（可能是中文），不适合做文件名，因此整副牌堆集合存在一个文件里）。
- 长命令：`deck new|set|show|reset|clear`、`draw <牌堆> [数量]`。
- 验收：multiplicity 展开数量正确；不放回跨进程持久化；reset 复原；超抽退出码 2（`tests/integration/p6_party_test.go`）。

---
## P7：角色卡扩展（`.pc` 家族剩余项）　✅ 已实施（2026-10-21）

手册 `.pc` 全 9 条的逐条盘点与实现（除群绑定外全覆盖）：

| 手册指令 | 状态 | 实现 |
| --- | --- | --- |
| `.pc list` / `.pc show` / `.pc del` | ✅ 已有 | `pc list/show` + `character delete` |
| `.pc new [模板:参数:] [卡名]` | ✅ 已有 | `character generate`（按规则书生成规则建卡，默认随机姓名）+ `character create` |
| `.pc redo [参数:] 卡名` | ✅ 本批 | `pc redo [id] [--variant]`：对**指定卡**重掷（默认当前卡），变体可选 |
| `.pc build [参数:] 卡名` | ✅ 本批 | 与 `pc redo` 同义（我们的「模板」就是规则书的 `generation` 规则） |
| `.pc cpy 卡名1=卡名2` | ✅ 本批 | `pc cpy <目标id>=<来源id>`：来源卡的属性/技能/资源轨/宏覆盖到目标卡，目标卡的身份（id/名字/规则书）不变；不允许复制到自己 |
| `.pc nn [新卡名]` | ✅ 本批 | `pc nn <新名>` / `character rename [<id>] <新名>`：id 不变；**名字不可与已有卡重复**（`INVALID_ARGUMENT`，退出码 2） |
| `.pc clr` | ✅ 本批 | `pc clr` / `character clear`：销毁全部角色卡（当前角色指针同时清空） |
| `.pc stat` | ⏳ **留待持久化设计后做** | 需要「按卡的掷骰流水」这一新的持久化维度，单独设计 |
| `.pc tag` / `.pc grp` | 🚫 排除 | 群-卡绑定 → 聊天平台适配器阶段 |
| 「每人最多 16 张卡」「群默认共用一张初始卡」 | 🚫 排除 | 本地 CLI 没有「用户」与「群」的概念 |

---

## 跨批次的工程要求（每批都要过）

1. 规则差异只进规则书 YAML，engine 不出现 coc/CoC 专属分支；新增 schema 必须有加载期编译校验与 `CONFIG_INVALID` 测试。
2. 短指令知识只在 `cmd/dicegirl/cli/short.go`（可按规模拆同包文件），`internal/` grep 不到任何别名/前缀；未来适配器只依赖 internal/app 与 JSON 契约。
3. JSON 契约只增不改：新批量结果用新容器，单结果字段保持；退出码 0/2/1 语义不变。
4. 每批交付：内部单测（engine/dice 边界用脚本化随机源）+ tests/integration 端到端（种子锁定）+ README/guide/data-format/research 文档同步 + `go test -race ./...` 全绿。

## 排除清单（不做或随平台适配器）

| Dice! 功能 | 处理 | 理由 |
| --- | --- | --- |
| `.rh` 暗骰、`.ob` 围观 | ✅ 已由 QQ 适配器实现（2026-10-22） | 群内执行、C2C 私发；群成员与私聊 openid 通过一次性绑定码关联（QQ 群事件不提供 union_openid） |
| `.pc tag/grp` 群-卡绑定 | ✅ 已由 QQ 适配器实现（2026-10-22） | 群根 `adapter.json` 存 member_openid→卡；未指定角色的 ra/st/sc/dv/dmg/en 默认用本人卡 |
| `.log` 战报记录 | ✅ 已由 QQ 适配器实现（2026-10-22） | `log start/stop/status/show/list`；记录执行过的指令与结果，Markdown 存在群数据根 `logs/` |
| `.draw _`/`.drawh` 暗抽 | 适配器阶段 | 与暗骰同一私信通道，待补 |
| `.bot/.group/.dismiss/.welcome/.reply` 群管、`.jrrp` 人品 | 永不内置 | 非跑团的平台运营功能 |
| `.nnn` 改昵称、`.send` 后台取文件 | 适配器阶段 | 平台账号/投递能力 |
| `.deck` 的信任用户/管理权限 | 不做 | CLI 单用户无权限系统（架构既定） |
| `.setcoc` 房规编号 0–6 | 不做开关 | 用规则书变体 YAML 表达差异（research 文档第 7 节既定） |
| `.pc build` 交互式建卡 | 不移植 | 平台交互形态；已有非交互 `character create`，详细作成走 P4 非交互方案 |
| Lua 扩展逐系统指令族（`.rdx/.rsr/...`） | 不做指令 | 系统包反模式；其中通用机制在 P5.2 抽象 |
