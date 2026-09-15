# LLM 动态控制 agent 工具方案（v2：形态 A）

状态：**已实施**（2026-09-15 落地 S1–S4；落地偏差见 §17）。用户拍板**键值化形态**：单工具 `agent_custom`，参数为 `action`（`get`/`set`）+ `key`（已知 key 枚举）+ `value`（仅 `set` 且 key 可写时使用）。`get sessions` 纳入本期（§11）。v1 归档于 commit `054f4ba`。

## 1. 重写起因（评审 ↔ 考古对照）

| 评审意见 | 考古事实 | 定性 |
|---|---|---|
| 参数列表不通用 | 09-10 原始设计（会话 `20260910-164915`）用**三工具**，`agent_sessions` 参数是 `{id?, limit?, role?}` 子结构；09-15 收窄为单工具**平铺**后才失去参数归属 | 形态退化，非实现走偏 |
| 功能扩展性差 | 新增能力要手改 4 处：`agentArgs` struct、`Invoke` switch、手写校验、`agentParams` 字符串拼接 | 同上 |
| 缺只读会话历史 | 09-10 有完整设计（schema / 输出格式 / 边界），09-15 §12 压缩成"会话只读列表"一句递延，从未拍板 | 范围收窄所致，见 §11 |

补充：v1 相对 09-15 定稿文档是忠实实现，但存在 3 处未登记偏差（§3），本次一并修。

## 2. 根因：`action` 分派不承载参数归属

v1 schema 把参数**平铺在顶层**：`{action, model, reasoning_effort}`，`required: [action]`。

| 字段 | `get` | `set` | `list_models` | 将来 `session` |
|---|---|---|---|---|
| `model` | 无意义 | 有效 | 无意义 | 无意义 |
| `reasoning_effort` | 无意义 | 有效 | 无意义 | 无意义 |
| `id` / `limit` / `role` | — | — | — | 有效 |

三个症状：**归属缺失**（字段对哪些 action 有效只能写进 description）；**schema 表达不了条件约束**（"set 时至少给一个键"只能手写，`setText` 里已出现 `provided`/`model==""`/`raw==""` 三重分支）；**扩展即改写**（每加一个能力域就往顶层加字段，字段集线性膨胀）。

**键值化解法**：把「改哪个东西」从**字段名**降级为**枚举值**——`key` 承载目标（`model` / `reasoning_effort` / `models` / `usage` / `stat` / `sessions`…），`value` 承载新值，顶层字段从此只有 `action` / `key` / `value` 三个，**能力面再扩也不加字段**。归属由 `key` 的取值表达，与 `action` 的关系（哪些 key 可写）写在工具说明里、并由实现白名单强制。

## 3. v1 的 3 处未登记偏差（本次一并修）

1. `set` 无改动返回 `MsgControlNoChange` **丢了 `error: ` 前缀**：定稿文档 §6 要求带前缀，同函数内其它错误分支都带；测试 `control_test.go` 反向固化了该偏差（断言裸文本）。
2. `MsgControlNoCache`（"无缓存数据"）是**不可达死代码**：`getText` 只在 `HasContext && ContextTokens > 0` 时调 `cacheRateLabel`，后者再判 `<= 0` 恒假。
3. "上下文: 未知（本轮尚无请求）"触发条件比文档宽：实现为 `!(HasContext && ContextTokens > 0)`，`HasContext` 为真但 tokens 为 0 时也报"尚无请求"，措辞与事实不符。

另有一条 v1 §13 已登记但值得在新形态下根治：v1 用 `args.Model != ""` 无法区分"未传 `model`"与"传了空串"，只能靠 `provided` 特判。形态 A 用 `*string` 解析 params 即可自然区分（§6.5）。

## 4. 澄清：缓存与本方案无关（撤回 v1 §2 的论证）

v1 §2 曾用"schema 变化击穿 cache / 前缀每轮失效"论证"单工具 + 静态枚举"，本方案**撤回**该论证，它在形态选择上不成立：

- **schema 是编译期常量**：定稿后每次请求的 `tools` 段字节相同，不存在"运行时变化"；本方案讨论的形态改动发生在**代码改动 + 重启**时，属版本更替，不是请求期行为。
- **唯一相关红线**：schema **不得承载运行态**（例如把当前可用模型列表写进 `enum`），否则 tools 段每轮不命中。v1 §2 已经这么定了，本方案沿用——但这条约束对 A / B 两种形态**同等成立**，不构成形态选择依据。
- **本工具的核心动作之一就是切换模型**，而跨模型本就不复用缓存（`docs/cache-probe.md`《结论总表》按后端分类）。在一个以"换模型"为职责的工具上再讨论 schema 字节的缓存收益，逻辑上自相矛盾。
- `docs/cache-probe.md` 的 `tools 段位置台阶`（改 `tools[0]`/`tools[last]` 描述损失 0.3–1.3k）测的是"**描述真的变了**"的情形；定稿 schema 不变，该数据与本决策无关，此前引用属于越界外推。

→ **形态选择由参数归属、模型侧可用性、维护成本决定，缓存不参与。**
## 5. 形态选定：键值化 `{action, key, value}`

| | **D 键值化（选定）** | v1 平铺字段 | A `params` 子对象 | B 一能力一工具 |
|---|---|---|---|---|
| 顶层字段 | `action` / `key` / `value`（恒定） | `action` + 每个能力域一个字段（膨胀） | `action` / `params`（恒定） | 每工具独立 schema |
| 归属表达 | **`key` 的取值**（枚举） | 无 | `params` 内字段名 | 工具边界 |
| 扩展新能力 | 加一个 key（enum + 说明 + 实现一行） | 顶层加字段 | 加 params 结构 | 加工具 |
| schema 内约束 | `action`/`key` 双 enum；`value` 按 key 校验 | 无 | 无 | 强（enum/类型/required） |
| 单次改动面 | 1 个 key 表条目 | 4 处手改 | 2 处 | 3 处 |
| 多键原子性 | 天然不需要（单 key 单 value） | 需要（v1 有专门逻辑） | 需要 | 不需要 |
| 工具数 | 5（不变） | 5 | 5 | 6 |
| 模型首次填对率 | **未知** | **未知** | **未知** | **未知** |

**依据强度声明**（沿用 §4 的教训，不再凭空取舍）：

- 除最后一行外，其余各行均为**可数事实**，来源是代码结构与 `allTools()` 清单，可直接核对。
- "模型首次填对率"**本项目无任何实证**，不作为任何形态的优劣理由；保留该行只为标明它是未知项，且不作为验收标准。
- 选定 D 的理由因此只有一条：**顶层参数形态恒定、归属由 key 表达、扩展只加一行**，与项目约束（工具清单稳定、极简优先）一致。

**D 的代价与兜底**：

1. `value` 是唯一通用槽位，其**类型与合法性完全由 key 决定**，schema 无法表达（JSON Schema 不支持"按另一字段取值决定类型"）→ 由实现按 key 校验，并给出可自纠的错误文本。
2. 只读 key 出现在 `set` 里必须**明确拒绝**（不得静默忽略），否则模型会以为写成功了。
3. `value` 声明为 `string`：当前两个可写 key 都取字符串；将来若加数值型 key（如输出行数），由实现从字符串解析，**schema 不变**。

## 6. key 表（能力面全集）

| key | get | set | 取值 | 返回内容 | 依赖 |
|---|---|---|---|---|---|
| `model` | ✅ | ✅ | 非空字符串 | 当前模型名 | `SetModel` |
| `reasoning_effort` | ✅ | ✅ | `minimal`/`low`/`medium`/`high`/`max`/`off` | 当前档位（未设置为 `(未设置)`） | `SetReasoningEffort` |
| `models` | ✅ | ❌ | — | 服务端可用模型列表（排序、超 50 截断） | `Client.ListModels()` |
| `usage` | ✅ | ❌ | — | 最近一次请求的上下文 tokens、缓存命中、命中率 | `Stats()` |
| `stat` | ✅ | ❌ | — | 会话 id、消息数、累计 token 用量 | `Stats()` |
| `sessions` | ✅ | ❌ | — | 本工作区会话列表 + **每个会话的文件绝对路径** | `ListSessions()`（`SessionInfo` 需补 `Path`） |

可写键只有 `model` 与 `reasoning_effort`（沿用 v1 §8 的边界）；其余四个为只读。将来扩展（输出行数预算、单向收紧权限）只需在本表加行 + 在 §7 的 enum 加值。

## 7. schema（golden）

```json
{"type":"object","properties":{"action":{"type":"string","enum":["get","set"],"description":"get 读取 key 的当前值；set 写入可写 key（需同时给 value）"},"key":{"type":"string","enum":["model","reasoning_effort","models","usage","stat","sessions"],"description":"可写键 model、reasoning_effort；只读键 models（服务端可用模型）、usage（上下文与缓存）、stat（会话统计）、sessions（会话列表与文件路径，jsonl 每行一条消息）"},"value":{"type":"string","description":"set 的新值（get 时忽略）。reasoning_effort 取 minimal/low/medium/high/max/off，off 表示清空该字段"}},"required":["action","key"]}
```

- `action` 与 `key` **双 enum**：这是模型填对参数的唯一约束来源（`value` 无法在 schema 层约束，见 §5）。
- `value` 不在 `required`：`get` 不需要它；`set` 缺 value 由实现报错（错误文本给可修建议）。
- 工具名沿用 **`agent_custom`**；描述首句说明"能力经 key 选择，可写键写在说明里"。

## 8. 行为与返回文本

### get

| 调用 | 返回 |
|---|---|
| `{"action":"get","key":"model"}` | `model: deepseek-v4-flash` |
| `{"action":"get","key":"reasoning_effort"}` | `reasoning_effort: high`（未设置 → `(未设置)`） |
| `{"action":"get","key":"models"}` | `可用模型（2）:` + 每行 `  model-a`（超 50：`（仅列出前 50 项，共 66 项）`） |
| `{"action":"get","key":"usage"}` | `上下文: 12345 tokens` / `缓存命中: 9000（72.90%）`（无请求：`上下文: 未知（本轮尚无请求）`） |
| `{"action":"get","key":"stat"}` | `会话: 20260915-145844` / `消息数: 18` / `累计: prompt 45678 / completion 1234 / total 46912` |
| `{"action":"get","key":"sessions"}` | 见 §11 |

### set

| 调用 | 返回 |
|---|---|
| `{"action":"set","key":"model","value":"kimi-k2"}` | `model: deepseek-v4-flash → kimi-k2` + `提示: 下一次请求生效；跨模型不复用 prompt cache` |
| `{"action":"set","key":"reasoning_effort","value":"low"}` | `reasoning_effort: high → low` |
| `{"action":"set","key":"reasoning_effort","value":"off"}` | `reasoning_effort: high → (未设置)` |

- 单 key 单 value → **v1 的多键原子性问题自然消失**（不存在"一个键改了、另一个报错"的半成品状态）。
- 语义不变：只写内存 `Config`，不落盘、不入会话文件；对**下一次请求**生效；`repl` 的 `{model}`/`{effort}` 占位符每轮现读，自动跟上。

## 9. 错误文本

| 情形 | 文本 |
|---|---|
| `action` 非法/缺失 | `error: 未知 action %q（可选: get/set）` |
| `key` 缺失 | `error: 缺少 key（可用: model、reasoning_effort、models、usage、stat、sessions）` |
| `key` 未知 | `error: 未知 key %q（可用: …同上）` |
| `set` 只读 key | `error: %q 是只读 key（可写: model、reasoning_effort）` |
| `set` 缺 `value` | `error: set 需要 value`（附该 key 的取值域） |
| `model` 为空/全空白 | `error: model 不能为空` |
| `reasoning_effort` 非法 | 现有 `MsgBadEffort`（含可选值） |
| JSON 解析失败 | 现有 `MsgParseArgs` |
| `models` 查询失败 | 原样透传错误文本，不吞 |

统一 `MsgErrPrefix` 前缀；v1 的 `MsgControlBadAction` 改为"可选: get/set"，`MsgControlNoChange` / `MsgControlNoCache` 随形态消失（§3 的偏差 1、2 一并消除）。

## 10. Go 侧结构（实施依据）

```go
type agentArgs struct {
	Action string  `json:"action"`
	Key    string  `json:"key"`
	Value  *string `json:"value"` // 区分「未给」与「空串」
}

// key 表驱动：新增能力 = 加一行，不动 Invoke 的分支结构
type keySpec struct {
	name     string
	writable string // 非空 = 可写，值为取值说明（用于错误文本回显）
	read     func() string
	write    func(v string) (string, error)
}

func (t *agentTool) keys() map[string]keySpec { /* model / reasoning_effort / models / usage / stat / sessions */ }

func (t *agentTool) Invoke(_ context.Context, argsJSON string) ToolResult {
	// 1) 解析 {action,key,value} —— 失败 MsgParseArgs
	// 2) 查 key 表：未命中 → 未知 key（回显可用 key）
	// 3) get → spec.read()；set → 校验可写 + value 非 nil → spec.write()
}
```

表驱动是「扩展性差」的直接解法：v1 的 4 处手改（struct / switch / 手写校验 / schema 拼接）收敛为**表加一行 + enum 加一值**。

## 11. `get sessions`：列表 + 文件位置（本期纳入）

2026-09-15 用户给出设计并纳入本期（此前挂在 v1 §12"待评估"）。**只列会话与文件位置，不读内容**——读内容交回 `run_shell`，符合项目「以 `run_shell` 为核心，新能力优先用 shell 命令组合实现」的约束。

返回样例：

```
会话（3，最新在前）:
  20260915-145844  18 条   2026-09-15 14:58  /home/coco/Project/tanya/.tanya/sessions/20260915-145844.jsonl
  20260915-140122  35 条   2026-09-15 14:01  /home/coco/Project/tanya/.tanya/sessions/20260915-140122.jsonl
  20260915-133357  145 条  2026-09-15 13:56  /home/coco/Project/tanya/.tanya/sessions/20260915-133357.jsonl
（共 12 个，仅列前 20）
```

- 范围：**仅当前 `sessionDir`**（当前工作区），不跨 `global_session/<其它 workspace-id>/`。
- 上限：列前 20 个（id 倒序，最新在前），超出标注总数。
- 文件是 **jsonl，每行一条消息**（首行 system 快照）——该提示写在工具说明里，返回不重复。
- 实现改动：`SessionInfo` 补 `Path string`（`scanSession` 已有 `path` 参数，直接填）；`configTarget` 加 `Sessions() ([]SessionInfo, error)`，`*Agent` 转发 `store.list()`。
- **已知风险（登记）**：拿到路径后，模型可用 `run_shell` 直读整文件（无行数上限，可能灌满上下文）。本方案不加额外限制——`run_shell` 的 30KB 头尾截断仍在，"读多少"由模型判断；将来若要收紧，属"输出预算"能力（§6 表加 key 即可）。

## 12. 实施步骤

| 步 | 内容 | 验证 |
|---|---|---|
| **S1** | **形态重构**：`agentArgs` 改 `{action,key,value}`；引入 key 表（`keySpec`）驱动 read/write；`agentParams()` 改新 schema；修 §3 两处偏差（`MsgControlNoChange` 前缀、`MsgControlNoCache` 死代码、`usage` 的"尚无请求"判定） | `go build ./...`、`go vet ./...` |
| S2 | **sessions**：`SessionInfo` 补 `Path`、`scanSession` 填值、`configTarget` 加 `Sessions()`、`sessions` key 的格式化（20 条截断） | `go test ./...` |
| S3 | 测试：`control_test.go` 重写为 key 表驱动的用例组（含只读 key 被 set、缺 value、未知 key、golden 全串） | `go test -race ./...` |
| S4 | 文档同步（§16） | `go test ./...` |

## 13. 测试计划

- **golden**：描述与 params 全串固定（双 enum 顺序）
- **get 各 key**：model / reasoning_effort（设置与未设置两分支）/ models（排序、50 截断、空列表、查询失败透传）/ usage（有请求、无请求）/ stat / sessions（空目录、超 20 截断、路径正确）
- **set**：model 正常与 trim、effort 五档 + off、大小写归一
- **set 拒绝**：只读 key（`models`/`usage`/`stat`/`sessions` → 报错且状态不变）、缺 value、model 空白、effort 非法
- **错误**：未知 action、未知 key、缺 key、坏 JSON
- **集成**：mock 两步，第 2 次请求已带新值（chat `reqs[1].Model`、responses `rawReqs[1]["model"]`）
- **工具清单**：`tools_test.go` 期望不变（5 项，`agent_custom` 仍在末尾）

## 14. 不做清单（沿用 v1 §8）

`api_key` / `base_url` / `user_agent` / `api_protocol` / system prompt 与 AGENTS.md 注入 / 主题配色 / `session_mode` / `global_session` / 工具清单增删 / 终端原语（`ctty` 不变量）/ 任何「放宽」方向能力（用户确认、沙箱、只读模式）。

## 15. 已定事项

1. 形态：键值化 `{action, key, value}`，`action`/`key` 双 enum，工具名沿用 `agent_custom`。
2. key 全集：可写 `model`、`reasoning_effort`；只读 `models`、`usage`、`stat`、`sessions`（§6）。
3. `value` 声明为 `string`，按 key 校验（数值型 key 将来由实现解析）。
4. `get sessions` 本期纳入：列表 + 文件绝对路径，**不读内容**；范围仅当前 `sessionDir`，列前 20。
5. 实现走 key 表驱动（新增能力 = 表加一行 + enum 加一值）。
6. `models` 按**默认可用**表述：启动即要求 `api_key`（`agent.New` 无降级路径），其可用性与 LLM 请求同源，故工具描述与错误文案不做网络/凭据提醒；查询失败仅原样透传错误文本。

## 16. 文档同步清单（S4）

- `docs/design.md`：《agent 自调（control.go）》段落改写（键值化形态、key 表、sessions）
- `AGENTS.md`：工具清单行、结构段 `control（…）` 行
- `README.md`：工具特性行、工具表
- `docs/todos.md`：登记 v2 键值化重写（v1 条目保留为历史）

## 17. 落地偏差（v2，2026-09-15）

| 项 | 方案 | 落地 | 原因 |
|---|---|---|---|
| 会话列表访问器 | `configTarget` 加 `Sessions()`（§11、§12） | `ListSessions()` | 与 `ListModels()` 命名对齐，同类访问器不再一名一样 |
| `keySpec` 字段 | 含 `name string`（§10） | 去 `name`；`writable string` 兼作可写标志与取值域文案 | key 名即 `keys()` 的键，字段冗余；只读判定由 `writable` 为空表达，`TestAgentToolKeyTableConsistent` 与 `agentWritableKeys` 对拍兜底 |
| sessions 输出列 | 样例按列对齐（§11） | `  %s  %d 条  %s  %s` 双空格裸拼，不做 padding | 免算列宽，行语义不受影响 |
| sessions 截断提示 | `（共 12 个，仅列前 20）`（§11） | `（共 %d 个，仅列前 %d 个）` | 与 `models` 截断提示句式统一并带量词 |
| `stat` 的不落盘情形 | §8 未定义 | `--no-save` 时 `会话: (不落盘)`；同时删掉 `Session == ""` 分支 | `store.path()` 由 `rotate()` 恒定赋值（`Agent.New` → `NewSession()`），空串分支不可达，属 §3 偏差 2 同类死代码；不落盘才是可达状态 |
| `usage` token 口径 | `上下文: %d tokens`（§8） | `上下文: %d tokens（最近一次请求）` | 与 `stat` 的"累计"口径显式区分（`ContextTokens` 每次请求覆写） |
| `models` 可用性表述 | §6 依赖列标注"网络，10s，需 api_key" | 依赖列只写 `Client.ListModels()`；工具描述与错误文案不做网络/凭据提醒（§15 第 6 条） | 启动即要求 `api_key`（`agent.New` 无降级路径），可用性与 LLM 请求同源 |
