# 架构说明

## 1. 分层与依赖方向

```text
cmd/dicegirl          进程入口：信号、stdout/stderr、退出码（package main）
cmd/dicegirl/cli      界面层：全局参数解析与进程装配，位于 internal 之外
cmd/qqbot（规划中）   QQ 官方机器人驱动：平台鉴权/网关/消息收发，只做传输翻译
chatcore              共享聊天语法层：短指令解析与文本/JSON 渲染，CLI 与所有适配器复用，位于 internal 之外
bot                   平台无关的适配器设施：按会话范围（群/C2C）隔离 data root 的多租户 Manager、LRU 驱逐、chatcore 执行入口
rulebooks/            内置规则书数据（coc7.yaml、dnd5e.yaml，go:embed）+ embed.go
datapacks/            内置数据包（names.yaml，非规则内容，go:embed）+ embed.go
internal/app          用例编排：创建角色、跑检定、资源写回、随机表/成长/抉择/人物作成等
internal/engine       与游戏无关的规则引擎：解释规则书（检定/对抗/资源/伤害/派生/随机表/成长/生成）+ 可插拔的检定机制（Mechanic）
internal/engine/formula  规则书公式小语言（整数/布尔、白名单函数、加载期类型检查）
internal/rules        RuleSystem 接口、Manifest 身份、Registry
internal/character    角色卡领域模型与 Repository 端口
internal/dice         通用骰子引擎（AST、解析、执行、随机源、爆骰/重投/计数算子）
internal/storage      持久化契约（角色卡、掷骰统计、本地状态、先攻、牌堆的接口）
internal/storage/sqlitedb  SQLite 实现（一个数据库；纯 Go 驱动，无需 cgo）
internal/config       运行时配置（数据根、备份策略、规则书/数据包目录、数据库路径）
internal/apperr       稳定错误码（机器判断）+ 结构化错误（人类阅读）
```

约束：

1. 依赖只能由外向内，且界面层在 `internal` 之外：`cmd/dicegirl/cli → chatcore → internal/app → {engine, rules, character, dice}`。任何 `internal/` 包都不导入 `cmd/`、`chatcore/` 或 `bot/`。机器人适配器（如 `cmd/qqbot`）与 CLI 平级，依赖 `internal/app` + `chatcore`（共享聊天语法）+ `bot`（多租户设施）；这是 2026-10-21 对「适配器只依赖 internal/app」的明确放宽。
2. `internal/dice` 不导入任何规则概念；`internal/engine` 不含任何游戏专属分支——CoC 是 `rulebooks/coc7.yaml` 里的数据，引擎只解释声明。
3. 领域层返回结构化结果与带错误码的 error；CLI 只负责把结果渲染成文本或 JSON。
4. 规则判定不写存储。资源检定只产出 `ResourceCheckResult.Change` 建议；是否应用由 `app` 层根据 `--apply` 决定。成长检定是唯一例外：成长语义即写回，`app` 层在成长成立后经同一条备份/原子更新/校验路径落盘。人物作成同理：引擎只掷出属性，建卡由 `app` 层走既有 `CreateCharacter` 路径。
5. 业务代码不拼文件路径；ID 经 `character.ValidateID` 校验，路径只由 `storage` 生成。
6. 不使用全局可变状态；随机源、仓储、规则书全部构造时注入。
7. 添加游戏不需要编译期注册：把同构 YAML 放进 `<数据根>/rulebooks/` 即可，加载路径与内置书完全相同。

## 2. 规则引擎与「一本规则书」

一个游戏 = 一份自包含的规则书 YAML（内置 CoC 为 `rulebooks/coc7.yaml`，用户书为 `<数据根>/rulebooks/*.yaml`）。`engine.ParseBook` 严格解码（`KnownFields(true)`，拼写错误的字段直接失败）后全量编译：

- 身份头：`schema_version`、`id`、`name`、`system_version`、`rules_version`、`description`（可选 `default: true` 指定多书时的默认书）；
- 词汇表：属性、技能（常量 `base` 或属性公式 `base_expr`）；
- 范围 `limits`、机制 `mechanic`（`roll` + `meets` 表达式族，或内置 `percentile`/`d20` 族）；
- 成功等级 `checks.levels`：布尔公式按声明顺序求值，首个为真者胜，末位兜底；
- 显式难度 `checks.difficulties`（可选）：公式难度用 `target` 公式改写目标（大失败跟随改写后目标、成功档位读基础目标梯），自动难度只判大失败并映射到声明等级；关键词本身是数据，引擎不内置任何难度名；
- 对抗策略 `opposed`（大失败级 + 同级裁决方式）；
- 伤害加值表（key 公式 + 连续行表，bonus 为骰子表达式）；
- 派生属性（formula / switch / lookup，按声明顺序累积整型命名空间）、生命条、初始属性；
- 资源检定（目标回退公式、成功门槛、成败损失、范围、clamp）；
- 随机表 `tables`（掷骰选行、行文本可内嵌 `{…}` 子掷）、成长检定 `growth_checks`（条件公式 + 成长量 + 上限 + 按条目覆盖）、人物作成 `generation`（骰点法属性表达式、版本变体、购点法点数池/费率/自动分配）。

所有公式在加载期解析并做类型检查与变量白名单校验，运行期不再有「未知字段/拼错变量」类错误。内置书与用户书都产出同一个 `*engine.Book` 类型，它实现 `rules.RuleSystem`（manifest、ValidateConfig 重新编译源字节）以及应用层的可选能力（KeyResolver、ValidateCharacter）。

**数据包**（`datapacks/*.yaml`）是规则书的表兄弟：同一套随机表 schema，但只承载**与规则无关**的内容（随机姓名等）。它是独立的一类文件（独立加载器与 `*engine.DataPack` 类型），不进 `rules.Registry`，也不作为规则系统被选择；`engine.ParseDataPack` 严格解码并编译，`.sys validate` 会重新编译全部数据包。

规则书选择：显式 `--system` > 角色卡 `system_id` > 声明了 `default: true` 的书（内置为 coc7）> 恰好一本书时隐式默认；零本报 `RULE_NOT_FOUND`，多本且无默认书时报 `INVALID_ARGUMENT`。规则书的**短指令别名**（身份头的 `commands`）在注册时校验全局唯一，供 CLI 的人物作成入口使用。

> 与原始规格书（`trpg_dicebot_ai_project_spec.md` 建议的 manifest.yaml + defaults.yaml + skills.yaml 三文件）不同，本项目采用单规则书文件，避免同一版本的身份与数据分散维护。规格中的配置分离精神保留且更进一步：不仅数据在 YAML，连判定阈值、等级条件与派生公式也在 YAML；Go 侧只保留规则无关的解释器。

## 3. 骰子引擎与公式语言

骰子引擎（`internal/dice`）：

- 词法/递归下降解析产生 AST：`NumberNode / DiceNode / BinaryNode / UnaryNode`。
- 解析（`dice.Parse`）与执行（`dice.Eval`）分离；多个骰组、运算与保留规则都在 AST 中保留完整明细。
- `Source` 随机源接口：生产用 `CryptoSource`（crypto/rand），可复现用 `SeededSource`（PCG），测试用 `ScriptedSource`（脚本序列耗尽即报错，杜绝隐性随机依赖）。
- 限制在解析阶段强制：单组骰数、总骰数、面数、嵌套深度；算术运算在执行前做 int64 溢出预判；除零分类为 `DIVIDE_BY_ZERO`。
- 骰组修饰符：保留/丢弃（`kh/kl/dh/dl`）、爆骰（`!`）、重投（`r`）、成功计数（`c`）；求值顺序为 掷骰 → 重投 → 爆骰 → 保留/丢弃 → 取值，爆骰与重投各有安全上限（`Limits.MaxExplodeDice` / `Limits.MaxRerolls`）。计数语义是「本组值 = 满足条件的骰数」，其静态上下界为 `[0, 骰数]`。
- 语句层（`dice.ParseStatement` / `Roller.RollStatement`）在纯表达式外包一层 `[n#] expr [比较目标]`：`n#` 重复（默认上限 10 轮）与比较后缀只存在于语句层；组内比较符（计数算子）属于表达式，故语句层先按纯表达式解析、失败才拆分尾部比较。规则书 YAML 填写的伤害/损失表达式走纯表达式入口，拒绝 `#` 与比较，两条语法边界互不污染。
- `dice.ExprBounds` 对纯表达式静态求上下界（保留骰按保留数量、除法跨零含 ±1、恒零除数报 `DIVIDE_BY_ZERO`、溢出报错），供引擎在不掷骰、不消耗随机数的情况下求损失理论最大值。

规则书公式语言（`internal/engine/formula`）是另一套更小的纯表达式语言：只有 int/bool 两值、算术与比较、`&& || !`、`abs/min/max` 白名单函数；无骰子、无字符串、无副作用；除法为数学 floor（与骰子表达式的向零取整不同）。变量集合由各声明段静态限定，加载期 `Check`，非法规则书不可能进入运行期。

## 4. 百分骰判定要点（由 coc7.yaml 声明）

- d100 由「个位 d10（面 1..9,10→0）+ 十位 d10（面 1→00 … 10→90）」组成，00+0 = 100。
- 奖励/惩罚骰先成对抵消；奖励取最小十位候选，惩罚取最大十位候选；并列保留先掷出的那颗。
- 成功等级按声明顺序求值：大失败条件最先判定（异常最差结果，优先于常规阶梯），随后极难（≤⌊目标/5⌋）、困难（≤⌊目标/2⌋）、普通（≤目标），兜底失败；目标 <50 时 96–100 大失败，≥50 时仅 100 大失败。
- 检定结果同时保留：原始候选、选中十位及索引、个位、最终骰值、目标值、等级 rank（声明索引）与 level_code、中文标签与解释文本。
- 资源检定的目标值允许 0（SAN 耗尽边缘）：此时任何 ≥1 都是失败，100 为大失败。
- 资源检定可声明 `fumble_loss: max`：大失败时失败损失取静态最大值（定值即自身，骰子式取保留骰面之和，由 `dice.ExprBounds` 求出），结果标记 `fumble_loss_max` 且不产生损失掷骰节点；coc7 理智检定以此实现 CoC 7e 约定。
- **机制（Mechanic）**：检定的掷法与判定方向由机制族决定（`percentile` 掷 d100 且「越小越好」、`d20` 掷 d20 且「越大越好」），规则书用 `mechanic.type` 选择；机制接口只回答四个问题——掷什么骰、奖励/惩罚骰如何作用、骰值与目标如何比较（`Meets`）、结果里展示什么。成功等级、难度、目标算式等规则全部与机制无关，由规则书声明，因此新增机制族不必改动引擎其它部分。
- 对抗检定是纯函数 `Book.Opposed(a, b)`：大失败必败（双大失败平），其余等级声明越靠前越好，同级按 `tie_break`（CoC 比目标值，再同为平局）。

## 5. 存储与平台差异

- **冷热分工**：规则书与数据包是冷数据（启动读一次、人工编辑）→ YAML 文件；角色卡与其它可写状态是热数据（每次检定都写）→ 一个 SQLite 数据库（`internal/storage/sqlitedb`，纯 Go 驱动）。`internal/storage` 只定义接口（存储缝），`internal/app` 只依赖接口，换后端不动业务代码。
- 布局：`<数据根>/dicegirl.db`（角色卡、掷骰统计、本地状态、先攻、牌堆、卡备份）、`rulebooks/*.yaml`、`datapacks/*.yaml`、`legacy/`（旧版 YAML 数据根迁移过来的原文件）。
- 写入：SQLite 事务（角色卡、统计、状态、先攻、牌堆）；仍是 YAML 的文件（规则书、数据包）用「同目录临时文件 → 写入 → Sync → Chmod 0600 → Close → Rename」。POSIX 上 rename 是原子的；Windows 上 Go 运行时使用带替换语义的 API，同为原子替换。目录 fsync 未执行（跨平台支持差），崩溃后已 rename 的文件内容由文件 sync 保证。
- 更新前备份：数据库里的 `card_backups` 表保留每张卡的旧版本（份数由 `DICEGIRL_BACKUP_KEEP` 决定）。
- 路径安全：ID 必须匹配 `^[a-z0-9][a-z0-9-]{3,63}$`；角色卡在数据库里没有路径拼接，读写前仍做 ID 校验；仍是 YAML 的文件读取前 `Lstat` 拒绝符号链接。残留限制：数据根是单用户本地目录，未对抗 TOCTOU 竞争替换。
- 读取错误细分为 `NOT_FOUND / INVALID_DATA / UNSUPPORTED_SCHEMA_VERSION / VALIDATION_ERROR / STORAGE_READ_FAILED`。
- 三份数据各自独立版本号：角色卡 `character.SchemaVersion = "1"`、规则书 `engine.SchemaVersion = "1"`、数据库 schema 由 `CREATE TABLE IF NOT EXISTS` 管理。

## 6. 机器人平台接入边界（2026-10-21 起实施）

聊天风格短指令（`r/rs/ra/rc/sc/st/pc/set/ti/li/ak/name/en/rh` 与 `.。!！` 前缀）的解析全部集中在 **`chatcore/`**（从原 `cmd/dicegirl/cli` 原样抽出）：它只把自由文本 token 翻译为 app 用例调用并渲染文本/JSON，`internal/` 没有任何别名知识。统一入口是 `chatcore.Execute(ctx, app, stdin, stdout, stderr, jsonMode, cmd, args) int`，返回进程式退出码（0/2/1）与 `Unrecognized(-1)`；CLI 的 `cli.Run` 与机器人适配器走同一条代码路径。

平台无关的多租户设施在 **`bot/`**：

- `Manager` 按会话范围隔离：`bot.ScopeGroup/<群 openid>` 与 `bot.ScopeC2C/<用户 openid>` 各对应一个 data root（`<基座>/groups/<id>/`、`<基座>/c2c/<id>/`）与一个 `*app.App`（独立 SQLite）。openid 不安全时退化为 `sha256-...` 目录名，杜绝路径逃逸；
- 打开的租户按 LRU 驱逐（默认 64 个，忙租户不关闭），关闭即关库；每租户有命令锁，串行化先攻/牌堆/抉择等读-改-写；
- 统一请求路径 `Manager.RunLine(ctx, kind, id, 消息文本, jsonMode)`：切词 → 取租户 → `chatcore.Execute` → 返回渲染文本。

平台适配器位于 `cmd/` 下（首个为 `cmd/qqbot`，QQ 官方机器人 API），只做传输：

- 输入侧把平台事件映射为「范围 + 一行文本」（群消息的@残留、附件过滤在适配器处理）；
- 输出侧把 `bot.Result.Out/Err` 发回平台（消息长度截断、主动/被动回复规则是平台知识）；
- 平台专属状态放在群数据根旁的 `adapter.json`（适配器私有，不进领域层）：成员与私聊 openid 的绑定、`pc tag` 的成员→卡映射、暗骰围观名单、战报会话；
- **隐藏掷骰与围观**：QQ 群事件不提供 `union_openid`，群成员与 `user_openid` 无法直接关联，因此首次使用 `.rh`/`.ob` 时由适配器发一次性绑定码，成员私聊机器人「绑定 <码>」后建立映射并补发暂存的结果；
- **群内个人绑卡**：`chatcore.WithDefaultCharacter(id)` 让未显式指定角色的命令使用发送者绑定的卡；CLI 不设该选项，行为不变；
- **战报**：`log start/stop/...` 在群数据根 `logs/*.md` 记录执行过的指令与结果；
- 这些指令只存在于适配器，CLI 的帮助把它们标注为「仅机器人」；chatcore 的 CLI 路径对 `rh` 继续报「需要聊天平台适配器」。
