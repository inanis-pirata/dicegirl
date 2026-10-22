# 错误码

错误码集中定义在 `internal/apperr/codes.go`，是对机器稳定的契约：测试与适配器按码判断流程，不解析错误文本。CLI 输出形如 `错误 [CODE]：消息`，JSON 模式不使用错误包装。

| 错误码 | 触发场景 | 典型来源 |
| --- | --- | --- |
| `INVALID_ARGUMENT` | 参数语义不合法（ID 格式、目标值范围、目标值算式结果越界、未知技能名/别名、显式当前值越界、多本规则书且无默认书时未指定 `--system`、角色名对应多张卡） | app / character / engine / storage 路径校验 |
| `PARSE_ERROR` | 骰子语句/表达式无法解析（空表达式、缺面数、括号不匹配、非法字符、尾随 token、非法 `n#` 重复前缀或比较后缀） | dice |
| `ROLL_ERROR` | 已解析但无法执行（随机源失败、上下文取消、算术溢出） | dice |
| `DIVIDE_BY_ZERO` | 表达式整数除零 | dice |
| `LIMIT_EXCEEDED` | 超过骰数/面数/深度/重复轮数上限 | dice |
| `RULE_NOT_FOUND` | 规则书 ID 未加载（零本加载或 id 不匹配）、数据包未加载或没有默认姓名集 | rules / app / engine |
| `CONFIG_INVALID` | 规则书 YAML 解析失败或编译校验失败 | engine.ParseBook |
| `INVALID_DATA` | 持久化文件不是合法 YAML 或缺 schema_version | storage |
| `VALIDATION_ERROR` | 角色卡/manifest/规则书语义校验失败 | character / rules / engine |
| `UNSUPPORTED_SCHEMA_VERSION` | 数据声明了二进制不支持的 schema 版本 | storage / rules / engine |
| `NOT_FOUND` | 角色或文件不存在（含按名字查角色失败） | storage / cli |
| `CONFLICT` | 角色 ID 重复（创建冲突）、短指令别名被两本规则书占用 | storage / rules registry |
| `STORAGE_READ_FAILED` | 读取权限、I/O 失败 | storage |
| `STORAGE_WRITE_FAILED` | 目录创建、临时文件、fsync、rename、备份修剪失败 | storage |
| `INTERNAL` | 未分类错误与兜底 | 全层 |

约定：

- 所有内部错误都包装为 `*apperr.Error{Op, Code, Message, Err}`，用 `errors.Is/As` 与 `apperr.IsCode(err, code)` 判断。
- 根因通过 `%w` 保留；CLI 只展示消息和一行可选原因，不打印堆栈或无关绝对路径。
- CLI 退出码：`0` 成功；`2` 用法错误（`INVALID_ARGUMENT`、`PARSE_ERROR`）；`1` 其他运行时错误。
- 新增错误码必须先加入本表与 `codes.go`，不允许各包临时发明。
