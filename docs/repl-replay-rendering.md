# 回放复用实时渲染方案（原阶段 5，独立评估）

> 状态：**未实施**，方案评估归档。来源：`docs/repl-output-refactor.md` 阶段 5（可选）拆出独立评估。
> 结论先行：**建议不做，或降级为方案 C（只统一样式函数）**。理由是数据不等价——历史里没有实时渲染所需的结构化字段，任何"复用"都只能退化成"按文本反推"或"改落盘格式"，前者脆弱、后者触碰模型通道边界。若将来确需一致的翻看体验，优先做方案 A 并按下文验收。

## 1. 背景

`repl-output-refactor` 阶段 0-4 已把 repl 输出收敛到 `streams`/`flow`/`turn`/`toolView`，但**回放路径（`/history`）仍与实时路径两套组装**：

| 路径 | 入口 | 渲染方式 |
|---|---|---|
| 实时 | `turn.Handle` → `toolView.Handle` | `RenderToolStart`/`RenderToolEnd(Inline)`/`RenderResponseInfo` + `Frame`/`Passthrough` |
| 回放 | `REPL.showHistory` → `printHistoryFull`/`printHistoryHead`/`printRendered` | 摘要行 `OneLine`+120 rune 截断；正文 `Frame`；消息头合成 Heading IR |

阶段 1-4 只把两者的**写入出口**统一（都走 `streams`，都带 `Kind`），**渲染策略仍是两套**。本方案评估"是否以及如何统一"。

## 2. 为什么不能直接复用（核心障碍）

`agent.Message`（`agent/llm.go:21`）落盘的字段只有 `role` / `content` / `tool_calls` / `reasoning_items`；`Usage`、`Stat` 标 `json:"-"` 不落盘，`ToolResult`/`ShellResult` 更是从不落盘——工具消息的 `content` 是 `ToolResult.Content()` 即 `ShellResult.String()` 的**扁平文本**（`stdout:` / `stderr:` 分节 + `exit code: N`）。

| 维度 | 实时数据 | 回放数据 | 后果 |
|---|---|---|---|
| 工具块正文 | `ShellResult` 结构化（按 chunk、时长、截断标记、exit code 字段） | 扁平文本 | 无法走 `shellView`：拿不到 `↳ exit 0 · 0.3s · 12 行` 的时长/行数，也拿不到 `2| ` stderr 前缀与头 3 尾 2 截断 |
| 状态行 | `ResponseInfo`（TTFT/TTFC/usage/context） | 无 | 回放无法渲染 info 行 |
| 工具标题进行中态 | 无（2026-09-15 追加化后标题只打一次，过程由 `» `/`  »` 心跳行表达） | 无进行中态 | 该维度差异已消除，不再是回放不能复用实时渲染的理由 |
| 消息头 | 无（流式无边界标记） | 需合成 `#N 角色` | 实时路径没有对应物 |
| 回合状态 | `justEnded`/`dirty` 驱动空行 | 无（沿用 REPL 当前残留状态） | 空行规则无法共用 |
| 清洗 | `Frame`/`Passthrough`（按 profile 决定是否留 SGR） | 摘要行 `OneLine`（全剥离）+ 正文 `Frame` | 两者本就不该相同 |

**结论**：所谓"复用实时渲染"在数据层就不成立；能复用的只有**样式函数**（`Frame`/`Passthrough`/`Truncate` 等纯函数），而它们本就是共享的。

## 3. 目标与非目标

**目标（若实施）**

1. 回放的工具块与状态行在**样式**上与实时一致（框线、语义色、缩进、`▸`/`↳` 记号）。
2. 回放不再自行拼接消息头样式，改为调用同一个构造函数。
3. 保持 `/history` 现有对外行为：无参摘要 120 rune、`n` 全量单条、`all` 全量、清洗与截断策略不变。

**非目标**

- 不追求"回放 == 实时逐字节"：数据不等价，做不到，也不该以牺牲落盘格式为代价去追。
- 不改 `agent` 的 history 落盘与请求构造（模型通道零变换红线）。
- 不改 `/history` 摘要行的 `OneLine` 清洗（那是 commit 54897cb 的止血成果）。
- 不引入"渲染器注册表/策略表"（沿用 `repl-output-refactor` §8 的防膨胀约束）。

## 4. 候选方案

### 方案 C：只统一样式函数（低风险，推荐若要做）

把实时的块结构与状态行样式抽成纯函数（`repl/toolview.go` 已有 `renderToolBlock`/`toolEndBody` 的雏形），回放侧用"从扁平文本反查到的近似结构"调用它：

- 工具块：`▸ 标题` + `  ` 缩进正文 + `↳ 状态`，标题取 `tool_calls.arguments` 的 command（实时已有 `toolArgsDisplay`），正文取 `content` 原文。
- 状态行：回放**不显示**时长/行数（无数据），或显示占位（如 `↳ 历史记录`）。
- 消息头：抽 `renderMessageHead(n, role)` 供回放专用（实时无消息头，不存在"统一"）。
- 成本：约 60-100 行改动，不动 `agent`；收益仅是"框线与记号一致"。
- 风险：低。缺点：收益有限——回放与实时的**信息量**差异依然在（这正是用户能感知的部分）。

### 方案 A：结构化落盘（收益最高，触碰边界）

在 jsonl 的工具消息上新增**仅供回放**的摘要字段（不参与请求构造），例如：

```json
{"role":"tool","name":"run_shell","content":"stdout:\n...","replay":{"exit":0,"duration_ms":300,"lines":42,"truncated":false,"stderr_lines":2}}
```

- 需要论证的边界：新增字段**绝不能**进入请求构造（`Message` 增加 `json:"-"` 之外的新 tag 时，chat/responses 的序列化路径必须显式排除），否则违反"模型通道零变换"。
- 老会话兼容：无 `replay` 字段时回退到扁平文本渲染（现状）。
- 成本：`agent` 落盘 + `repl` 回放两侧，约 150-250 行 + 兼容测试。
- 风险：中。收益：回放的 `↳ exit 0 · 0.3s · 12 行` 与 stderr 前缀、截断标记都能还原。

### 方案 B：从扁平文本反解（不推荐）

用正则从 `ShellResult.String()` 的输出反推 exit code / 行数。**否决**：格式一变就碎，且 `String()` 的文本本身是模型通道格式，反向依赖它会把回放与提示词格式绑死。

## 5. 决策点（需拍板）

| 问题 | 备选 |
|---|---|
| 是否值得做 | (a) 不做（现状可用，差异只是样式） (b) 只做方案 C (c) 做方案 A |
| 若做 A：新增字段是否允许落盘 | 需确认"落盘新增字段但不进请求"是否在"模型通道零变换"红线内 |
| 回放是否显示状态行 | 显示占位 / 完全不显示 / 只显示 `exit N`（若走 A 则可完整显示） |

## 6. 验收标准（若实施）

1. `/history n` 与实时的工具块样式一致（框线、`▸`/`↳` 记号、缩进、语义色）；有 golden 字节用例锁定。
2. 无 `replay` 字段的老会话回退到现状渲染（迁移用例：构造老 jsonl → 断言不 panic 且输出与现状一致）。
3. 请求构造字节不变（`agent` 侧既有测试 + 一条"新字段不进请求"的显式断言）。
4. `/history` 摘要行仍走 `OneLine` + 120 rune；`all`/`n` 行为不变（现有用例保持绿）。

## 7. 相关遗留项（本轮评估顺带记录，非本方案范围）

| 项 | 说明 | 建议 |
|---|---|---|
| `MsgUnknownCmd` 死分支 | `slashCommands` 白名单（`repl/completer.go`）与 `handleCommand` 的 switch 完全一致，`default` 不可达 | **已删**（commit `00f934c`，白名单即分发契约） |
| `output.guard` 无锁读取 | 仅测试在启动前置位，`-race` 干净 | 需要运行期改时加锁 |
| `picker` 裸写不经 `vis` | raw 期自绘，已声明为模式屏蔽的例外 | 保持不变 |
| 回放期间 `view.Content` 的副作用 | 回放经 `toolView.Content`，若上一回合留下 `justEnded` 会在回放首行前补空行 | 现状即如此，若做本方案可一并规整 |

## 8. 相关文档

- `docs/repl-output-refactor.md`：阶段 0-4（输出收敛、`flow`/`turn`、`toolView` 结构体化、输出模式）
- `docs/design.md`《斜杠命令》《输出流与 Kind》《测试》
- `agent/shell.go`（`ShellResult.String()`，回放的数据来源）
- `repl/repl.go`（`showHistory`/`printHistoryFull`/`printHistoryHead`/`printRendered`）
