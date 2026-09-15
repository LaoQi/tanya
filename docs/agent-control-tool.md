# LLM 动态控制 agent：`agent_custom` 工具方案

状态：**已定稿**（2026-09-15 确认 §11 三项决定；未实施）。

范围：A 档三项——`model` 切换、`reasoning_effort` 调整、只读自省（当前可调项 + 运行态统计）。
明确不在本次范围：`temperature`（不做）、上下文历史裁剪（另案设计，见 §12）。

## 1. 做与不做

| 项 | 决定 | 理由 |
|---|---|---|
| `model` 切换 | 做 | 已有运行时路径 `/model`；纯出站字段，零缓存前缀代价 |
| `reasoning_effort` 调整 | 做 | 已有 `SetReasoningEffort`；简单任务降档、难题升档 |
| 只读自省（get + stats） | 做 | 动态调控的前提是模型看得见现状；只读无风险，且是上面两项的决策输入 |
| `temperature` | 不做 | 与 effort 存在隐式联动（设置 effort 后不发 temperature），单独暴露语义重叠 |
| 上下文/历史裁剪 | 不做 | 需要新的 history 预算与压缩语义，独立设计 |
| 会话切换（`/new`、`/load`） | 不做 | 让模型清空自己的历史是自伤路径，必须由用户发起 |
| 持久化（写回 config.yaml） | 不做 | 配置属用户主权；运行时项仅本次进程有效 |
| 自我提权（放开确认/结束只读） | 不做 | 见 §8 |

## 2. 形态：单工具 + 固定 schema

新增**一个**工具 `agent_custom`，`action` 区分 `get` / `set` / `list_models`，schema 恒定、不随运行态变化。

两条约束决定这个形态：

- **工具清单即请求前缀**：不按功能拆多个工具，也不做动态注册（`docs/design.md`《不做动态工具注册》）。`action` 枚举把能力面收在一个 `ToolDef` 里，后续加可调项只扩 `properties`，不改工具数量。
- **schema 变化击穿 cache**：参数表一旦随运行态变化（如"当前可选模型列表"写进 enum），前缀就每轮失效。故 schema 里只有静态枚举（effort 五档 + off）。

代价（一次性，须登记）：新增工具本身改变 `tools` 段字节，已有会话的 prompt cache 前缀破一次；此后稳定。

## 3. 可调项定义

| 项 | 取值 | 生效点 | 持久性 |
|---|---|---|---|
| `model` | 任意非空字符串（不预校验，服务端定成败） | 下一次请求（`Client` 每请求读 `c.cfg.Model`） | 进程内存，`/load` 后回落配置文件值 |
| `reasoning_effort` | `minimal` / `low` / `medium` / `high` / `max` / `off` | 下一次请求（`chat` 走顶层 `reasoning_effort`，`responses` 走 `reasoning.effort`） | 同上 |
| 只读：运行态 | `Stats()`（上下文 tokens、缓存命中、消息数） | 即时读取 | — |

`off` 语义沿用现状：清空字段，wire 上不出现该字段。**设置 effort 后不再发送 `temperature`** 是既有行为（`temperatureParam`），本次不改。

## 4. 类型与文件布局

新增 `agent/control.go`（工具实现 + 目标接口 + 文本格式化），清单仍由 `allTools()` 集中组装。

```go
// 窄接口：*Agent 满足；测试可注入 fake
type configTarget interface {
	Model() string
	SetModel(string) error
	ReasoningEffort() string
	SetReasoningEffort(string) error
	ListModels() ([]string, error)
	Stats() Stats
}

type agentTool struct{ target configTarget }

func newAgentTool(t configTarget) *agentTool { return &agentTool{target: t} }
func (t *agentTool) Name() string                  { return "agent_custom" }
func (t *agentTool) Definition() ToolDef           { return newToolDef(t.Name(), agentDesc, agentParams()) }
func (t *agentTool) Invoke(_ context.Context, argsJSON string) ToolResult
```

`configTarget` 用接口而非 `*Agent`：与 `shellTool` 的"构造期注入依赖"一致，测试可脱离 Agent 单测工具分支。

三处装配改动：

```go
// agent/tools.go：清单签名加一个依赖，顺序为 run_shell → 内置三件套 → agent_custom
func allTools(shell *shellTool, ctl configTarget) []Tool {
	return append(append([]Tool{shell}, builtinTools()...), newAgentTool(ctl))
}
```

```go
// agent/agent.go New：先建 Agent，再建 registry/client（工具需要 Agent 引用）
a := &Agent{cfg: cfg, workspace: cwd, env: envSection(cwd, tool.profile),
	prompt: newPromptBuilder(cwd, globalAgentsPath(), readAgentsFile),
	store:  newSessionStore(sessionDir, o.noSave)}
tools := newToolRegistry(allTools(tool, a)...)
a.tools = tools
a.client = NewClient(cfg, tools.defs())
```

```go
// agent/agent.go：加空值校验，与 SetReasoningEffort 对齐
func (a *Agent) SetModel(m string) error
```

`SetModel` 改签名后唯一调用点是 `repl/repl.go` 的 `/model` 分支（`r.agent.SetModel(parts[1])`），改为接收错误并走 `st.err.emit`，用户侧行为不变（`strings.Fields` 保证参数非空，仅拦截全空白输入）。

## 5. 工具 schema（golden 期望）

```json
{"type":"object","properties":{"action":{"type":"string","enum":["get","set","list_models"],"description":"get 读取当前可调项与运行态；set 修改；list_models 向服务端查询可用模型列表（网络请求，最长 10s，需 api_key）"},"model":{"type":"string","description":"set 时指定新模型名，缺省表示不改此项"},"reasoning_effort":{"type":"string","enum":["minimal","low","medium","high","max","off"],"description":"set 时指定思考等级，off 表示不发送该字段"}},"required":["action"]}
```

描述文案（`agentDesc`）要点，按顺序：

1. 用途：读取或修改当前 agent 的模型与思考等级；
2. 作用域：仅本次会话，不写入配置文件，进程退出即失；
3. 生效时机：改动对**下一次请求**生效，本轮后续步骤即可体现；
4. 成本提示：切换模型后 prompt cache 不复用，需重新预热；`list_models` 是网络请求。

## 6. 行为与返回文本

### get

```
model: deepseek-v4-flash
reasoning_effort: high
上下文: 12345 tokens（缓存命中 9000，72.9%）
消息数: 18
```

- `reasoning_effort` 未设置时输出 `(未设置)`（新常量）。
- 第三、四行读 `Stats()`：`HasContext == false` 时第三行改为 `上下文: 未知（本轮尚无请求）`。
- 不向模型暴露 `temperature`/`api_key`/`base_url` 等不可调项，避免诱导无效尝试。

### set

```
model: deepseek-v4-flash → kimi-k2
reasoning_effort: (未设置) → high
提示: 模型已切换，下一次请求生效；跨模型不复用 prompt cache
```

- 只列实际改动的项；最后一行提示仅在 `model` 变更时追加。
- **先校验后应用（原子）**：`effort` 用 `normalizeEffort` 预校验、`model` 校验 trim 后非空，全部通过才写入；任一非法则整体不生效并返回错误。避免"model 改了、effort 报错"的半成品状态。
- `action: set` 但两项都未给 → 错误（新常量 `MsgControlNoChange`）。

### list_models

```
可用模型（12）:
  deepseek-v4-flash
  kimi-k2
  ...
```

- 透传 `Client.ListModels()`；失败（无 api_key、网络、超时）原样报错文本，不吞。
- 列表超过 50 项时截断并标注总数，防止单次工具结果吃掉上下文。

### 错误分支表

| 情形 | 文本 |
|---|---|
| `action` 缺失/非法 | `error: 未知 action %q（可选: get/set/list_models）` |
| `set` 未指定任何项 | `error: set 需要至少指定 model 或 reasoning_effort` |
| `model` 全空白 | `error: model 不能为空` |
| `effort` 非法 | `error: ` + 现有 `MsgBadEffort` 文案 |
| JSON 解析失败 | 现有 `MsgParseArgs` |

新增常量统一进 `agent/messages.go`，沿用 `MsgErrPrefix` 约定。

## 7. 语义边界与易踩点

- **即时生效**：`Client` 持有 `*Config`，`chatStream`/`responsesStream` 每次请求现读 `Model`/`ReasoningEffort` → 同一轮 `runTurn` 内 set 之后的所有请求立刻用新值，无需重建 client。
- **不落盘**：只改内存 `*Config`。会话文件（`store.append`）只存 history + system prompt，不含这两项 → `/load` 或换进程后回到配置文件值。这是**已知且有意的偏差**，在文档与工具描述中明说，避免模型误以为设置持久。
- **提示符自动刷新**：`repl.resolveVars` 每轮调 `agent.Model()` / `agent.ReasoningEffort()`，`{model}`/`{effort}` 占位符自动跟上 → **repl 侧零改动**（除 `SetModel` 签名那一行）。
- **可见性即审计**：工具调用块已渲染 args，加上 set 的返回文本，用户能看到每次变更；因此不需要新增 `EventKind`。
- **暂无并发写**：`runTurn` 内工具调用串行，`*Config` 的写发生在请求间隙；本次不引入 mutex（若将来出现并发可调项再加）。
- **cache 影响**：新增工具破一次前缀（§2）；`model` 切换跨缓存域。二者都只影响命中率，不影响正确性。

## 8. 不做清单（安全边界）

以下一律不进本工具，也不作为将来扩展位保留在 schema 里：

- `api_key` / `base_url` / `user_agent`（凭据与风控一致性）；
- `api_protocol`（`chat` 与 `responses` 的 history wire 结构不同，运行时切换撕裂会话）；
- system prompt / AGENTS.md 注入（击穿定格前缀，且是持久化副作用）；
- `theme` / `palette` / `colors` / `tool_output_lines`（用户视觉与显示偏好，且属 `repl` 层）；
- `session_mode` / `global_session`（构造期确定的存储布局）；
- 工具清单增删、终端原语（`ctty` 不变量）；
- 任何"放宽"方向的能力：用户确认、沙箱、只读模式。将来若加收紧类开关，须单向可收紧、放宽一律回落用户确认。

按此方案，`agent_custom` 的能力面只有"换模型 + 调节思考档位 + 读状态"，三者都不构成新的破坏面：破坏力上限仍是 `run_shell`。

## 9. 测试计划

### 9.1 mock 扩展

`agent/mock_test.go` 现只处理 `/responses` 与 `/chat/completions`，`GET /models` 会落进 `handleChat` 并解析失败。需加分支：路径以 `/models` 结尾时返回 `{"data":[{"id":"b-model"},{"id":"a-model"}]}`，以便断言排序与截断。

### 9.2 新增 `agent/control_test.go`

| 用例 | 断言 |
|---|---|
| `TestAgentToolDefsGolden` | 描述与 params 全串固定（对齐 `TestShellToolDescGolden`/`TestRunShellParamsGolden`） |
| `TestAgentToolGet` | 含当前 model；effort 空/非空两分支文本 |
| `TestAgentToolSetModelNextRequest` | mock 两步：第 1 步 tool_call `set`，`m.reqs[1].Model` 已是新值（`chat`） |
| `TestAgentToolSetModelResponses` | 同上，用 `rawReqs[1]["model"]`（`responses`） |
| `TestAgentToolSetEffort` | `chat`：`reqs[1].ReasoningEffort == "high"` 且 `Temperature == nil`；`responses`：`rawReqs[1]["reasoning"]["effort"]` |
| `TestAgentToolSetEffortOff` | 置空后两协议均无该字段 |
| `TestAgentToolSetAtomic` | model 合法 + effort 非法 → 返回错误且 `Model()` 未变 |
| `TestAgentToolSetNoChange` / `TestAgentToolBadAction` | 错误文本 |
| `TestAgentToolListModels` | 列表已排序；50 项截断标注；无 api_key 时报错 |

`SetModel` 空值校验的用例合并进 `TestAgentToolSetAtomic` 一类的表驱动。

### 9.3 既有用例调整

`agent/tools_test.go` 的 `TestToolRegistryOrderAndDefs`：`allTools(newTestShellTool(), ...)` 补第二参（fake `configTarget`），`want` 增 `"agent_custom"`；`TestToolRegistryLookup` 同步传参。

## 10. 实施步骤

| 步 | 内容 | 验证 |
|---|---|---|
| S1 | `agent/control.go`：接口、工具、schema、文本、常量；`SetModel` 改签名 | `go build ./...`、`go vet ./...` |
| S2 | `allTools` 签名与清单、`agent.New` 装配顺序、`repl/repl.go` `/model` 错误处理 | `go test ./...`（此时 tools_test 需一并改） |
| S3 | mock `/models` 分支 + `control_test.go` + golden | `go test -race ./...` |
| S4 | 文档同步：`docs/design.md`（《工具》《内置工具》《配置项》三处）、`AGENTS.md` 工具清单行、`README.md` 一句、`docs/todos.md` 登记 | `go test ./...` |
| S5 | 手工验证：`make build` 后要求模型 `agent_custom` 读状态、切 effort、切模型，确认提示符与下一轮请求同步 | 见 `AGENTS.md` 手工验证约定（不用 `go run .`） |

## 11. 已定稿的决定（2026-09-15）

1. **`get` 带运行态统计行**（即 §6 版本）：模型需要看见上下文占用与缓存命中，才有依据判断是否降档思考、是否收紧输出。
2. **`list_models` 本期做**：唯一的网络 action（10s 超时、需 api_key），失败原样透传；列表超 50 项截断并标注总数。
3. **`set model` 直接生效**，不追加用户确认：与用户侧 `/model` 语义一致，且变更经工具调用块与返回文本完全可见。

## 12. 递延项

- **上下文历史裁剪**：独立方案。需要新的 history 预算策略（按轮次/按 token 保留尾部、早期工具输出降级为摘要）、裁剪点选择（请求前 vs 工具返回后）、以及"裁剪不写入会话文件"的落盘口径——不宜与本次搭车。
- **B 档项目**：工具输出截断下沉到 history（现状 `tool_output_lines` 只作用于终端显示）、`run_shell` 默认 cwd/timeout、会话只读列表、单向收紧权限。均待本次落地后按需评估。
