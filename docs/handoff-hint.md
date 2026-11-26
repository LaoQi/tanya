# 上下文交接提醒（`handoff_hint_tokens` → `repl.HandoffHint`）

> 归属：`tanya/repl`（判定、文案、表现层）。状态：**已实施**（2026-11-26）。
> 相邻文档：battlelab 的 `docs/agent-hooks.md`（钩子切点与契约）、`docs/session-handoff.md`（交接语义）、本仓 `docs/design.md`《上下文交接提醒》。

## 1 定位

交互前台只做**提醒**、不做强制：上下文接近上限时，往 history 里追加一条 user 事实行建议模型收尾并
`next_session` 交接。判据、文案、去重与上屏全在 `repl`，`agent` 不感知阈值。

提醒的实现是一个**生命周期钩子**（`repl.HandoffHint`，同时实现 `agent.BeforeRequestHook` 与
`agent.SessionHook`），不是 REPL 在回合入口的一次函数调用。它要在装配期随 `agent.New(…, agent.WithHooks(hint))`
注册，再由 `repl.NewREPL(…, repl.WithHandoffHint(hint))` 接上表现层。

## 2 钩子职责

| 切点 | 做什么 |
|---|---|
| `BeforeRequest` | 判 `contextUsed(Stats()) = max(ContextTokens, Est) >= tokens` → `AppendNotice(建议)`（本轮请求就带提示）→ 投一行屏幕提示 |
| `SessionEvent` | 任一会话边界（New/Load/Adopt/Fork/SwitchWorkspace/Handoff）→ 重新武装（`done = false`） |

- **注入前提两条**：`agent.FromContext(ctx)` 取得到 Agent（钩子边界由库注入）、**REPL 回合在驱动**（`render` 绑定了回合通道）。后者使单发 `tanya ask` 与库外直接调 `Ask` 的路径不参与提醒（与钩子化之前一致）。
- **去重**：每会话一次（`done`），会话边界复位——不再需要 REPL 里 6 处显式 `resetHandoffHint()`。
- **抑制**：`/next` 命令轮与交接后的续跑轮不提醒（`runPrompt` 入口 `mute(cont || nextPending)`，判据与文案仍留在钩子里）。
- **判据口径**：`contextUsed = max(实报 ContextTokens, 本地估算 Est)`。提示文案与判据同源，避免「提示说 180K、`/stat` 显示 150K」打架。

## 3 屏幕提示行为什么要经回合通道

钩子在 agent 的回合 goroutine 上跑，而所有渲染（含 `» 等待响应` 心跳状态行，`\r` 式擦写）只在 REPL goroutine。
直接在钩子里写 stdout 会与状态行交织、且与 `EventRequestStart` 的渲染无确定顺序。

所以 `runTurn` 每回合给钩子绑一个 `post(text, isErr)` 闭包：钩子把这一行投进**本回合的事件通道**并等 ack，
REPL 循环里按序渲染——顺序天然落在 `EventRequestStart` 之后、请求构造之前。渲染时先 `view.Stop()` 收掉心跳，
打完提示行再 `view.Waiting()` 接上，避免状态行与提示行互相覆盖。**不新增 `agent.Event` 种类**。

## 4 行为变化（钩子化带来的，均属预期）

| # | 变化 |
|---|---|
| a | 注入位置：从「用户输入之前」变为「用户输入之后」（`BeforeRequest` 在 user 消息入 history 后、构造请求前） |
| b | 屏幕行时机：从「回合前」变为「回合内首个请求的 `EventRequestStart` 之后」；rich 面观感 = 空行 → 心跳收掉 → 提示行 → 心跳接上 |
| c | **新增能力**：工具轮内也能注入——阈值在 tool 轮之后才达到时，提示落在 tool 结果之后、下一次请求之前 |
| d | 单发 `tanya ask` 仍不参与（钩子未绑定回合通道 → 不注入） |
| e | 中断/报错回滚：事实行经 `AppendNotice` 已落盘，回合回滚只回滚内存 → 该行留在会话文件里、内存里消失（重载会话会重新看到）。钩子化之前注入在回合前、不在回滚区，无此现象 |
| f | `Continue` 续跑轮继续抑制（保语义不变） |
| g | 阈值的 `Est` 口径略大：用户输入已在 history 里，同一回合比之前早触发一个用户输入的估算量 |

## 5 边界

- 只提醒、不拦请求、无第二档/强制/停止/预算闸门（主导权在用户与模型），强制复位只在 ivan 侧用 `ForceHandoff`
- 归档只读态不可对话，天然不触发
- 注入是 append-only（system 快照与 history 前缀不动，prompt cache 不受影响）；`-n` 下注入内存生效、不落盘
- 注入失败（`AppendNotice` 报错）打一行错误，并且**不再重试**（`done` 在尝试前就置位，避免每轮重复报错）

## 6 装配

```go
var hint *repl.HandoffHint
if cfg.HandoffHintTokens > 0 {           // 0 = 关闭，钩子根本不注册
    hint = repl.NewHandoffHint(cfg.HandoffHintTokens)
}
a, _ := agent.New(&cfg.Config, …, agent.WithHooks(hooks...))   // hooks 含 hint
r, _ := repl.NewREPL(a, "", …, repl.WithHandoffHint(hint))     // nil 即不接表现层
```

## 7 测试

`repl/handoffhint_test.go`：9 例既有语义（阈值口径、每会话一次、关闭、未达阈、`/next` 抑制、`/new` 后重新武装、
`-n` 仍注入、plain 面仍上屏）+ 新增 2 例：

- `TestHandoffHintInjectsInsideToolRound`：mock 第 1 次返回 `tool_call` + `usage.prompt_tokens=5000` →
  断言 history 为「输入 → 带 tool_calls 的 assistant → tool → 注入 → 回答」，且**第 2 次请求末尾就是注入行**
- `TestHandoffHintInactiveOutsideREPLTurn`：只注册钩子、不经 REPL 回合（直接 `a.Ask`）→ 不注入、不记 `done`

## 8 落地记录（2026-11-26）

- `repl/handoffhint.go` 重写为 `HandoffHint`（钩子）；`repl/repl.go` 去 `hintTokens`/`hintDone`/`resetHandoffHint`，
  `runPrompt` 只留 `mute`；`repl/turnloop.go` 绑回合通道并分发 `inHint`；`repl/flow.go` 的 `turn.hint` 负责渲染
  （`Stop` → 行 → `Waiting`）；`repl/toolview.go` 增 `Waiting()`；`main.go` 在 `agent.New` 之前造钩子
- battlelab 侧前置：`agent.FromContext(ctx)`（钩子 ctx 携带 Agent，见其 `docs/agent-hooks.md`）
- 验收：`gofmt -l` 干净、`go build/vet/test ./...` 全绿、`go test -race ./repl/`、`make cross`、
  `scripts/render_audit.py` 全部 PASS
- 真机 pty 冒烟（`make build` + 假 LLM，三条路径）：① `handoff_hint_tokens: 1` 首轮即达阈——观感为
  `» 等待响应 0s` → `上下文约 474 tokens 达提醒阈值 1，已向模型注入交接建议…` → `» 等待响应 0s` → 正文，
  无残留半行；② `/next` 轮不提醒（`未生成交接摘要` 之后的下一个普通轮照常提醒一次）；
  ③ 工具轮内注入（阈值 2000、首轮 fake 返回 `run_shell` + `usage 5000`）——提示行落在工具块之后、第二次请求之前
