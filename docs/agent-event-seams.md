# agent 事实流与事件切面

> 状态：**B 方案已全部落地**（2026-09-28 评估并实施；P0=M1、P1=M2、P2=M3，落地记录与四处执行偏差见 §14）
> 范围：`agent` 事件流的**事实完备性**、扇出机制、指标归属、通知落点
> **不动**：呈现粒度（出站块语义化）、驱动/传输抽象、`Tool` 三方法契约、会话级多实例、缓存不变性契约
> 判据：每阶段要么「事实可观测」（新增消费者不改上游），要么「行为可观测」（单测 + `render_audit`）；两者都不满足的改动不做
> **已裁定（2026-09-28）**：① 工具进度切面**不做**（与并行工具合并，见 §8）；② **会话级 observer 不引入**（现时无收益，见 §4.2 与 §5.1）

## 1 结论摘要

1. **`agent` 的出站契约已合格**：`EventContent`/`EventReasoning` 本就是增量 delta，`EventReasoningEnd` 是分段边界，`EventToolStart/End` 带 args/result。不需要把 IR 或渲染概念引进 `agent`。
2. **真正的问题是"事实被视图吃掉"**：`agent` 的 13 个发声点只服务**一个**消费者（终端渲染），其余事实各走专用通路。所谓"统一切面"今天不存在。
3. **缺的不是总线，是两件事**：① **事实完备性**（观测盲区）；② **扇出机制**（`EventSink` 无组合器，加消费者必须改 `repl`）。
4. **多消费者不需要订阅表**：单生产者 × N 消费者是 **fan-out**，机制是**装配期固定的组合器**（与 `agent.WithTools`、`present.Registry` 同语义）。订阅表是运行期可变状态，即 god object 的入口，**不做**。
5. **裁定后的实际范围**：observer 是 `EventSession`/`EventConfig` 的唯一发射点，也是 `EventRequest`/`EventPersisted` 的唯一消费者。**不引入 observer，这四项一并撤销**（§5.1）。收缩后只剩三项：`EventTurnEnd`、`ResponseInfo` 首字指标清理、删 `EventUsage`。

## 2 现状实测（2026-09-28）

### 2.1 发声点：13 处，全在 `agent`

| 文件 | 点数 | Kind |
|---|---|---|
| `agent/llm.go` | 5 | `EventReasoningEnd` `EventUsage` `EventReasoning` `EventContent` `EventToolCall` |
| `agent/llm_responses.go` | 5 | 同上 |
| `agent/agent.go`（`runTurn`） | 4 | `EventRequestStart` `EventResponse` `EventToolStart` `EventToolEnd` |

> `sink` 穿透到 LLM 层（`Client.ChatStream(ctx, messages, sink)`）是**正确的**：delta 在那里产生，中转一层只会更糟。代价是 LLM 层认识 `Event` 类型，而 `Event` 本就是 agent 的内部协议。

### 2.2 消费点矩阵：已经有两个 Kind 无人消费

| Kind | 发射点 | 消费者 |
|---|---|---|
| `EventRequestStart` | `agent/agent.go:280` | `repl/toolview.go:292` |
| `EventReasoning` | `agent/llm.go:256`、`agent/llm_responses.go:208` | `repl/flow.go:96` |
| `EventReasoningEnd` | `agent/llm.go:224`、`agent/llm_responses.go:178` | `repl/flow.go:100` |
| `EventContent` | `agent/llm.go:264`、`agent/llm_responses.go:241` | `repl/flow.go:105` |
| `EventToolStart` | `agent/agent.go:314` | `repl/flow.go:108`、`repl/toolview.go` |
| `EventToolEnd` | `agent/agent.go:316` | `repl/toolview.go` |
| `EventResponse` | `agent/agent.go:298` | `repl/flow.go:110`、`repl/toolview.go:298` |
| **`EventToolCall`** | `agent/llm.go:285`、`agent/llm_responses.go:229` | **无**（预留：`docs/shell-tool.md` 的并行工具） |
| **`EventUsage`** | `agent/llm.go:247`、`agent/llm_responses.go:250` | **无** |

**这两个"发了没人要"的 Kind 是本方案的核心教训**：**新增事件必须先有消费者**（§11 R3）。

### 2.3 观测盲区与处置

| # | 缺的事实 | 处置 |
|---|---|---|
| 1 | **出站请求**（实际发出的 model/messages/tools/params） | **撤销**（唯一消费者是旁路，见 §5.1） |
| 2 | **回合结算**（总时长/累计用量/结果/是否中断） | **保留** → `EventTurnEnd`（消费者：通知 sink + 渲染） |
| 3 | **落盘**（路径/字节/行数） | **撤销**（唯一消费者是旁路，见 §5.1） |
| 4 | **会话变更**（`/new` `/load` `/fork` `/switch`） | **撤销** → 降级为**可接受缺口**：调用方全部已知（`Fork()` 返回新 id、`SessionID()` 可读、`LoadSession` 入参即 id），且会话操作不在回合内、对回合 trace 无影响 |
| 5 | **配置变更**（`SetModel` / `SetReasoningEffort`） | **撤销** → 同上（两方法均返回 `error`，调用方已知结果） |
| 6 | **工具执行内部**（长命令的阶段性输出） | **裁定不做**（§8） |

### 2.4 专用通路清单（"假统一"的证据）

| 事实 | 通路 |
|---|---|
| 回合内事件 | `EventSink` → `turn.Handle`（**唯一消费者 = 渲染**） |
| 历史落盘 | `a.save()` 直接调用 |
| 用量统计 | `a.stats.record()` 内部，外部靠 `Stats()` **轮询** |
| 回合结算 | `turn.End` 自己算 |
| 通知 | `turn.End` / `turn.notifyNeedInput` **硬编码触发** → `payloadOf` 加工 |

## 3 目标与判据

| | 目标 | 今天 | 本轮处置 |
|---|---|---|---|
| **U1 单一事项源** | 每个事实只有一个权威发出点，且发出点在上游 | 已成立（13 点全在 `agent`） | 保持 |
| **U2 事实完备** | 数据流上每个有意义的转变都有切面 | 缺 6 处（§2.3） | **收缩为 1 处**（回合结算）；其余裁定撤销或不做，理由逐条记录 |
| **U3 视图可挂** | 新增消费者不改上游一行 | 不成立（无 `EventSink` 组合器） | `Sinks` 组合器（若采 §13 选项 B） |

**判据维持**：一个消费者能否只靠"订阅切面 + 读事件定义"就工作，而不需要读 `agent` 源码？本轮**不追求全量达成**——U1 成立下的局部改进 + 逐条记录未闭合的缺口，优于为"完备"造出无人消费的事件。

## 4 机制

### 4.1 组合器

```go
// agent/event.go —— 同步串行扇出，装配期固定，顺序即因果。
// 形态照 repl/notify.go 的 Notifiers(list ...Notifier) Notifier（零/一/N 优化）。
func Sinks(list ...EventSink) EventSink
```

**这是"多消费者"与"旁路监听"的唯一机制。** 不引入订阅表、不引入总线。

**其唯一消费者是 §7 的通知 sink**（`runTurn` 组合 `Sinks(turnSink, notifySink)`）。若无 §13 选项 B，则本组合器**不做**（无消费者的机制同样是坏账）。

### 4.2 observer：裁定不引入

原设想：装配期注入一个**会话级** sink（`Options.observer` / `WithObserver`），在 `Ask` 入口与回合 sink 合流（`sink = Sinks(a.observer, sink)`），承载不在回合上下文里的事实（`EventSession`/`EventConfig`）。

**裁定：不引入。** 理由：

1. **现时无收益**：它要服务的四项事实（§2.3 的 1/3/4/5）**目前没有任何消费者**——没有 trace/审计/统计的第三方消费方存在。
2. **事实调用方全部已知**：会话与配置变更的结果（新 session id、新 model）调用方在调用点即可读到，"缺的只是没进统一流"；对回合 trace 无影响。
3. **将来引入不需要改 `agent` 契约**：届时只是给 §4.1 的组合器在装配期**多一个成员**（`WithObserver` 的语义等价于"往组合器里加一个 sink"），属增量而非重构。**这正是"装配期固定"这条设计带来的期权价值。**

**代价（记录在案）**：在没有旁路消费者的期间，会话生命周期与配置变更**不进事件流**——这是**已接受的缺口**，不是遗漏。

### 4.3 五条消费者约定（一旦做扇出就必须写死）

1. **同步串行**，按注册顺序调用 —— 顺序即因果（现有 `EventToolStart` 的 ack 握手依赖它）
2. **消费者不得阻塞** —— 慢消费者自己缓冲（形态参考 `repl/turnloop.go` 的 `sinkWrap`）
3. **消费者 panic 不传染** —— 旁路不得杀死主流程（`recover` 收口在 `Sinks` 内，每消费者独立）
4. **消费者不得反向调用 agent** —— 避免重入
5. **装配期固定**，无运行期订阅/退订 API —— 与 `WithTools`/`Registry` 同语义；**这是防 god object 的关键**

## 5 事件定稿

### 5.1 撤销（记录理由，便于将来重启）

| 撤销项 | 理由 |
|---|---|
| `EventSession` / `EventConfig` | **无发射点**：observer 不引入，且这两个方法不在任何回合 sink 的上下文里。降级为可接受缺口（§2.3 的 4/5） |
| `EventRequest` | **无消费者**：唯一消费者是旁路（trace/审计）；渲染不需要它（状态行用的是已有的 `EventRequestStart` + `EventResponse`） |
| `EventPersisted` | **无消费者**：同上 |
| `AsyncSink` 助手 | **无消费者**：唯一组合点是 `Sinks(turnSink, notifySink)`，两者都不慢 |
| `Streaming` 接口 + `EventToolChunk` | 与并行工具合并（§8） |

> **重启条件**：出现真实的旁路消费者（trace / 审计 / 统计落盘）时，这四项连同 observer 一起恢复，且恢复时**四项都有消费者**——不重蹈 `EventUsage` 的覆辙。

### 5.2 新增：只留 `EventTurnEnd`

| 事件 | 载荷 | 落点 | 发射时机 |
|---|---|---|---|
| `EventTurnEnd` | `TurnInfo{Duration time.Duration; Usage *Usage; Failed bool; Interrupted bool; Kept bool; HistoryLen int}` | `agent/agent.go` `Ask` | 回合唯一出口（`Ask` 收敛为单出口结构） |

**消费者**：§7 的通知 sink（`Failed`/`Interrupted`）、渲染（分隔线可用 `Duration`）。

**事实判定**：`Duration` 只有 `Ask` 知道起点，是**事实**（对比 §6 的首字指标是**派生量**）。

### 5.3 清理

| 动作 | 对象 | 理由 |
|---|---|---|
| **删** | `EventUsage` | 零消费者（§2.2）＋ 信息已被 `EventResponse.Response.Usage` 覆盖，属重复 |
| **留** | `EventToolCall` | 并行工具的预留（`docs/shell-tool.md` 已登记"`EventToolStart/End` 填 `ToolIndex`/`ToolID`"），**须在 `event.go` 注释里注明"当前无消费者、属预留"** |

## 6 指标归属清理：`ResponseInfo`

**现状**：`ResponseInfo{FirstEvent, FirstReasoning, FirstContent}` 是终端体验指标（首字节延迟），却作为 `agent` 的事实存在，且经 `Message.Stat *RequestStat` 一路带到渲染层。

**实测消费点**：只有 `repl/toolview.go:102-106`（状态行的 `TTFT`/`TTFC`）+ 测试。其中 **`FirstReasoning` 零消费者**（仅测试断言）。

| 项 | 动作 |
|---|---|
| `FirstReasoning` | **删**（零消费者） |
| `FirstEvent` / `FirstContent` | **删**，改由渲染侧从 `EventRequestStart` 起计时：`EventContent` 首次到达即 TTFC，`Reasoning`/`Content`/`ToolCall`/`Usage` 中首次到达即 TTFT |
| `RequestStat` | 收窄为 `Duration`（或并入 `Message.Duration time.Duration`，`json:"-"`） |
| `ResponseInfo.Duration` / `Usage` / `ContextTokens` | **保留**（`Duration` 只有 LLM 层知道起点，是事实） |

**这是"事实 vs 派生"的样板案例**：TTFT/TTFC 完全可由事件流重建，故它们不是 `agent` 的事实。

**回归风险**：`repl/toolview.go` 的 `RenderResponseInfo` 需要新的计时入口（`turn` 侧持有 `requestStart time.Time`）；`render_audit` 的 `TTFT`/`TTFC` 断言需改为"存在性 + 单调性"；`repl/mode_test.go:102,252`、`repl/settle_test.go:35`、`repl/toolview_test.go:202,264` 的夹具需同步。

## 7 通知降为 sink

**现状**：两个硬编码触发点（`repl/flow.go:243` 的 `NotifyTurnDone`、`repl/flow.go:253` 的 `NotifyNeedInput`），载荷经 `payloadOf` 二次加工。

**改法**：新增 `repl` 侧的 `notifySink`（`agent.EventSink`）：

| 事件 | 通知 |
|---|---|
| `EventToolStart{Interactive: true}` | `NotifyNeedInput`（等价于今天的 `notifyNeedInput`） |
| `EventTurnEnd{Failed, Interrupted}` | `NotifyTurnDone`（`Interrupted` 时不响，与 `turn.End` 现状一致） |

`runTurn` 组合：`sink := agent.Sinks(turnSink, r.notifySink)`。删 `turn.notifyNeedInput` 与 `turn.End` 里的 `r.notify(...)`。

**时长口径**：通知用 `EventTurnEnd.Duration`（agent 回合时长）；渲染分隔线（`turnSep`）继续用 `turn` 自己的计时（渲染回合含收尾，语义不同）。两者差异在毫秒级，不可感知，**但口径必须写明**，避免将来被当成 bug。

**注意**：本项是 §13 选项 B 的内容；`notifySink` 同时也是 `Sinks` 组合器与 `EventTurnEnd` 的唯一消费者。

## 8 工具进度：裁定不做

**裁定（2026-09-28）：本期不做。** 理由：

1. **必须带消费者才不是坏账**：`EventUsage`/`EventToolCall` 已示范"发了没人要"（§2.2）。`EventToolChunk` 的消费者只能在 `repl` 侧（多块工具渲染），而 `repl/toolview.go` 现为**单块状态机**（`dirty`/`justEnded` + 单条 `heartbeat`）。
2. **与并行工具同源**：两者都要动 `repl` 的工具块渲染，分开做等于同一处改造做两遍。`docs/shell-tool.md` 已把"多块"列为**并行工具**的届时改动清单。

**将来直接取用的接口草案**（形态镜像 `agent/tools.go` 的 `Interactive`：可选接口 + 类型断言探测，零新概念）：

```go
type Streaming interface {
    InvokeStream(ctx context.Context, argsJSON string, emit func(Chunk)) ToolResult
}
type Chunk struct{ Text string }
```

## 9 阶段与验收

> 分两案，取决于 §13 的裁定。两案都**不含** P0（无 observer 后没有可独立零行为落地的机制）。

### 选项 A（纯清理）

| 阶段 | 范围 | 验收 |
|---|---|---|
| **A1** | 删 `EventUsage`（`agent/event.go` + 两处发射点）；`EventToolCall` 加预留注释 | 全量测试 + `-race` 全绿；`rg 'EventUsage'` 零命中 |
| **A2** | §6 指标归属清理（首字指标移出 `agent`，渲染侧自计时） | `render_audit` 断言改"存在性 + 单调性"；`rg 'FirstReasoning'` 零命中；`repl` 四个夹具同步 |

### 选项 B（清理 + 通知归位）**推荐**

| 阶段 | 范围 | 验收 |
|---|---|---|
| **M1** | A1（删 `EventUsage`）+ `EventToolCall` 加预留注释 | 全量测试 + `-race` 全绿；`rg 'EventUsage'` 零命中 |
| **M2** | `EventTurnEnd` + `Ask` 收敛为单出口；`agent.Sinks` 组合器 | 新增单测：一回合（含工具调用）**恰好一次** `EventTurnEnd` 且是最后一个事件；成功/失败/中断三条路径的 `Failed`/`Interrupted` 标志；`Sinks` 扇出与退化（0/1/N）；`-race` 全绿 |
| **M3** | §7 通知降为 sink（删两处硬编码触发）+ §6 指标归属清理 | 通知单测改为驱动 `notifySink`，另加真实回合经 `r.ask` 的接线断言；`render_audit` 15 PASS；真 pty 目视 TTFT/TTFC |

依赖：B1 是 B2 的前置（`EventTurnEnd` 是通知的事实源）。B0 可与 A 案共用。

## 10 不做清单

| 不做 | 理由 |
|---|---|
| **进程级总线 / 订阅表** | 单生产者不需要；订阅表是运行期可变状态 = god object 入口；与"装配期固定"冲突 |
| **会话级 observer** | 现时无收益（§4.2）；将来引入只需给组合器加成员，不改 `agent` 契约 |
| **`EventSession` / `EventConfig` / `EventRequest` / `EventPersisted`** | 无发射点 / 无消费者（§5.1）；重启条件已记录 |
| **`AsyncSink`** | 无消费者（§5.1） |
| **工具进度 + 多块渲染** | 与并行工具合并（§8） |
| **事件溯源**（落盘/统计搬出 `agent`、从流重建状态） | 破坏 `Ask` 的"返回即已落盘"原子性契约，并搬走 `InterruptError.Kept` 依赖的 history 所有权。**只暴露切面，不搬所有权** |
| **事件序列化协议 / 版本化** | trace 走 jsonl 即可，不另立协议 |
| **把颜色/宽度/门禁放进事件** | 那是视图的决定；事件只装事实 |
| **呈现粒度（出站块语义化）** | 独立课题，不在本次范围 |
| **驱动/传输抽象** | 独立课题，不在本次范围 |

## 11 风险

| # | 风险 | 缓解 |
|---|---|---|
| R1 | `Event` 膨胀成 god 类型 | 判据："**事实 vs 派生**"（§5.2/§6）；新增前先问"这是不是已有事件的视图" |
| R2 | 消费者阻塞主循环 | 五条约定第 2 条（§4.3）；`Sinks` 内 `recover` 隔离 |
| R3 | 新增无人消费的事件 | **本方案已被此坑过一次**（`EventUsage`/`EventToolCall`）→ 新增事件必须同 commit 带上消费者；§9 各阶段的验收即为此设 |
| R4 | 组合器成为"没有消费者"的机制 | 若采选项 A 则**不做 `Sinks`**（§4.1）；机制与事件同标准 |
| R5 | B3/A2 改动 `repl` 的计时来源，`render_audit` 抖动 | 断言改"存在性 + 单调性"（§6） |

## 12 决策记录

| # | 决策 | 理由 |
|---|---|---|
| D1 | 扇出用**组合器**，不用订阅表 | 单生产者 × N 消费者是 fan-out；"装配期固定"与 `WithTools`/`Registry` 同语义 |
| D2 | `agent` 的出站契约保持 **delta 事件**，不引入 IR/渲染概念 | delta 已是正确粒度；引 IR 会破坏「`agent` 零内部依赖」这条最大红利 |
| D3 | 保留 sink 穿透 LLM 层 | delta 在那里产生；`Event` 本就是 agent 内部协议 |
| D4 | **只暴露切面，不搬所有权**（落盘/统计仍归 `agent`） | 保 `Ask` 的原子性契约与 history 所有权 |
| D5 | 删 `EventUsage`、留 `EventToolCall` 并注明预留 | 零消费者 + 信息重复 vs 已登记的未来用途 |
| D6 | 首字指标（TTFT/TTFC）移出 `agent` | 派生量由事件流可重建，不是事实 |
| **D7** | **不引入会话级 observer**（2026-09-28 裁定） | 现时无收益（§4.2）；四项事实的调用方全部已知；将来引入只是给组合器加成员，不改契约 |
| **D8** | **工具进度切面不做**（2026-09-28 裁定） | 与并行工具同源，都要动 `repl` 单块状态机；合并为一个课题更省 |
| **D9** | 撤销 `EventRequest`/`EventPersisted`/`AsyncSink` | 无消费者；新增无人消费的机制与事件同属坏账（R3/R4） |

## 13 裁定记录

| # | 事项 | 裁定 |
|---|---|---|
| **C1** | 选项 A（纯清理）还是选项 B（A + `EventTurnEnd` + `Sinks` + 通知降为 sink） | **采 B**（2026-09-28），已落地，见 §14 |

## 14 落地记录（2026-09-28）

B 方案全部实施，三阶段合并为一次改动。**代码**：

- **`agent`**：`event.go` 删 `EventUsage`（连带删 `Event.Usage` 死字段——13 个发声点无一设置、全仓零读取；`EventToolCall` 加「当前无消费者，属预留」注释）、加 `EventTurnEnd` + `TurnInfo`、加 `Sinks`；`llm.go`/`llm_responses.go` 删 `EventUsage` 发射点与首字计时变量，`RequestStat` 整体删除，改 `Message.Duration time.Duration`（`json:"-"`）；`agent.go` 的 `ResponseInfo` 去掉三个首字指标，`Ask` 拆出 `ask(ctx, sink, mark)` 收敛为单出口并在出口发 `EventTurnEnd`。
- **`repl`**：`toolview.go` 新增 `respTiming`（`observe` 按 `EventKind` 推进：`EventRequestStart` 重置、`EventReasoning`/`EventReasoningEnd`/`EventToolCall` 记 TTFT、`EventContent` 记 TTFT+TTFC），`RenderResponseInfo` 改收 `ttft, ttfc`，`toolView` 的 `EventResponse` 分支抽为 `Response(info, ttft, ttfc)`；`flow.go` 的 `turn` 持有自己的 `respTiming` 并把时序显式传给 `view.Response`；`notify.go` 新增 `notifySink`（消费 `EventTurnEnd` / `EventToolStart{Interactive}`）；`turnloop.go` 改 `agent.Sinks(sinkWrap, notifySink)`（渲染在前）；删 `turn.notifyNeedInput` 与 `turn.End` 的 notify 分支。

**四处执行偏差（均记录理由）**：

1. **`Sinks` 不做 panic 隔离**。原写「`recover` 收口在 `Sinks` 内、每消费者独立」，实施改为**不 recover**：`Sinks` 在热路径上（每个 delta 一次调用），`recover` 会给每次调用加一层 defer；且当前唯一消费者 `notifySink` 是 `repl` 自己的代码，吞掉 panic 只会掩盖 bug。约定改为「**消费者不得 panic 逃逸**（需要隔离的旁路自行 recover）」，`AGENTS.md` 与 §4.3 同步。
2. **`TurnInfo` 字段收窄为 `{Duration, Failed, Interrupted}`**。原定还含 `Usage`/`Kept`/`HistoryLen`，但三者无消费者（用量在 `EventResponse.Response.Usage` 已有；`Kept`/`HistoryLen` 无人读）——按「无消费者不新增」的同一判据删掉。
3. **修正一处实施缺陷：TTFT/TTFC 不能放在 `toolView`**。`turn.Handle` 拦截 `EventContent`/`EventReasoning` 走 markdown 管线（`writeContent`/`writeReasoning`），**不转发给 `toolView`**，故真实 REPL 路径下正文/思维链事件到不了它——真 pty 实测第二段请求的 TTFT 直接消失（第一段只是恰好由 `EventToolCall` 顶上）。改为「**谁看见完整流谁计时**」：REPL 是 `turn`、`ask` 单发是 `toolView`，二者共用 `respTiming`，`turn` 把时序显式传给 `view.Response`。时钟沿用 `heartbeat.now` 的可注入先例（`timing.now`），故字节基线仍可确定（`repl/mode_test.go` 的 `TestRichStreamsByteIdentical` 与 `feedAskPath` 用注入时钟保住 `TTFT 300ms` 基线）。
4. **`TTFT` 语义微调（可观测变化）**：responses 协议旧实现把 `firstEvent` 置在 SSE 回调**入口**，任意帧（含 `response.output_item.added`）都会记时、实际约等于「首帧到达」；新实现只认产出 `agent.Event` 的流式事件（`Reasoning`/`ReasoningEnd`/`ToolCall`/`Content`），即「**首个模型产出增量**」。chat 协议口径本就如此（旧实现要求 `choices` 非空或带 usage），两协议由此对齐。
5. **`EventUsage` 删除后渲染无变化**：usage 仍随 `EventResponse.Response.Usage` 到达（实测 `↳ TTFT 155ms · 607ms · prompt 100 · completion 20` 与改动前一致）。

**验收**：

- `gofmt -l` 干净、`go build ./... && go vet ./...` 通过、`go test ./...` 与 `go test -race ./...` 全绿、`GOOS=darwin|windows go build ./...` 通过。
- `python3 scripts/render_audit.py` **15 PASS / 0 FAIL**。
- 真 pty 目视（`make build` + `--sse-delay 0.15 --dump clean-tool-short`）：第 1 段 `↳ TTFT 155ms · 607ms · prompt 100 · completion 20`；第 2 段 `↳ TTFT 2ms · TTFC 2ms · 303ms · prompt 100 · completion 20`（**偏差 3 修正前该行无 TTFT**）。
- 依赖边与残留断言：`go list -deps ./agent` 仍只有 `agent` 自身（零内部依赖保持）；`rg 'EventUsage|FirstEvent|FirstReasoning|FirstContent|RequestStat'` 在非 `docs/`/`CHANGELOG.md` 范围内零命中。
- 新增用例：`agent` 的 `TestAskEmitsTurnEndOnce`（含工具调用的一回合有两次请求、恰好一次 `EventTurnEnd` 且为末事件）、`TestAskTurnEndFlagsOnFailureAndInterrupt`、`TestSinksFanOut`/`TestSinksDegenerate`、`TestChatStreamRecordsDuration`、`TestResponsesToolCallOnlyRecordsDuration`（并锁住纯 tool_call 响应仍产出首批事件）；`repl` 的 `TestToolViewComputesTTFTAndTTFC`（含新请求重置计时、正文即首事件时不显 TTFC）、`TestToolViewNoTimingWithoutRequestStart`、`TestRunTurnNotifiesThroughSink`（真实回合经 `r.ask` 锁住接线）。

**未做（沿用原裁定）**：会话级 observer、`EventSession`/`EventConfig`/`EventRequest`/`EventPersisted`、`AsyncSink`、工具进度 + 多块渲染、订阅表、进程级总线、事件溯源、呈现粒度语义化、驱动/传输抽象。
