# 流式期输入与事件合流（架构诊断 + 分阶段方案）

状态：**P1、P2、P3 已实施**（2026-09-27；P4 先行落地）。本文件是「流式输出期间的终端输入能力」的架构诊断与分阶段方案，**首个消费者**是思考期 `Ctrl+O` 切换思维链显示（原 `docs/reasoning-live-toggle.md` 的交互设计，已被本文件取代并归档）。文件按阶段组织（P1–P5），每阶段**相互解耦**，可单独评审、单独实施、单独验收。P5 未实施。

结论摘要：这个功能在现有架构里之所以要写 ~200 行补丁，不是功能复杂，而是**缺三条主抽象**（§5 的 A/B/C）与两条次抽象（D/E）；其中「事件合流」这一条可以在 `repl` 内以 ~30 行补上、并一次性消掉三项额外处理（§6.2）。本文件把「补抽象」与「交功能」拆成阶段，先交功能（P1）、再还债（P2/P3）、最后顺手优化（P4）。

---

## 1 需求边界（已与需求方确认的收窄版）

| 场景 | 是否响应 `Ctrl+O` |
|---|---|
| 流式输出期（思考 delta / 正文 delta 正在到达） | **是** |
| 工具执行期（任意工具，含 `interactive` 借出） | **否**，可整轮排除 |
| 提示符期（命令输入） | **否**——用 `/reason on` / `/reason off`（已存在，`repl/reason.go`） |
| plain / 非 TTY / `ask` 单发 | **否**（无流式渲染可切换） |

交付口径：本功能是**优化项，尽力而为**——门禁不过不提示、监听起不来不报错、按键丢在无人读的窗口里可接受（现状按键被静默吞掉）。

---

## 2 基线：抽象齐备时它应该长什么样

```go
for {
    select {
    case ev := <-agentEvents: t.Handle(ev)   // 数据
    case k  := <-keyEvents:   t.Hotkey(k)    // 控制
    case err := <-done:       return err
    }
}
```

即：**终端事件与 agent 事件在同一个循环里被同一个 goroutine 顺序消费**。此时 `Ctrl+O` 的实现是一个 `case` 分支 + 一个开关字段 + 一个段状态机，不需要原子、不需要跨线程记账、不需要「等下一个 delta 才生效」。

---

## 3 逐层评估：每层缺什么、上层为此多做什么

### 3.1 L0 `ctty`——职责正确，不背锅

只提供 termios/模式/信号原语，无事件概念、无策略，符合「零依赖叶子 + 只给原语」的定位（`docs/ctty.md`）。本功能不需要它改任何东西。

### 3.2 L1 `readline/device`——缺「持续读」的运行态

设备面只有拉取式 `readEvent()`（`readline/console.go:46-53` 的 device 接口），没有「设备自己持有一个读循环」的形态。**同层反证**：`bridgeTTY` 有自持读泵（wake pipe + 泵 goroutine + 停止协议，`readline/lease_linux.go:21-44`、`:79-160`），而 `posixTTY`/`windowsConsole` 没有。「谁在跑的时候终端仍在被读」这件事，**只有借出路径有答案，持有路径没有**——这正是本功能要自己起 goroutine 的底层原因。

### 3.3 L2 `readline/Console`——承诺了判据，只实现了一半

`docs/terminal-console.md` §2 判据 2 写「消费者不需要知道现在有没有子进程在跑、我能不能读键盘」，实际：

| 承诺 | 现实 | 证据 |
|---|---|---|
| 订阅即收事件 | ~~`EventKey` 只在有人调 `ReadEvent` 时才 `dispatch`~~ → `SubscribeKeys` 订阅者由常驻读者推键（P3，2026-09-27）；普通 `Subscribe` 仍只做通知、不武装读者 | `readline/console.go` |
| 读权由 Console 安排 | `BeginRead`/`EndRead` 是消费者租用的独占会话：无引用计数、`posixTTY.saved` 单槽、`Raw` 用 `TCSETSF` 冲输入队列 | `readline/device_posix.go:36-46`、`ctty/termios_linux.go:20-22` |
| 借出自动让出读权 | ~~**不存在**（§6 已自认「未落成的设计意图」）：`LendStdin`/`LendFull` 不挂起任何读者~~ → **已补齐**（P3，2026-09-27：`beginLend` 挂起常驻读者、`Lease.Release` 恢复） | `readline/console.go`、`docs/terminal-console.md` §5 S8 |

→ **上层代价**：必须自建读循环、自管 `BeginRead`/`EndRead`、自己在工具事件上停读、且不敢恢复（恢复即面对无人仲裁的模式竞争）。

### 3.4 L3 `repl` 输入侧（editor / picker）——模型正确但不可组合

`Editor.Readline` 是「`BeginRead` → `for ReadEvent`」的阻塞独占环（`readline/editor.go:68-117`），`pickSession` 同型（`repl/picker.go:128-155`）。对提示符期这是对的抽象；它体现的是**唯一被支持的读者形态**：独占、同步、自绘。流式期需要的是另一种形态：非独占、异步、不绘屏。现有抽象没有第二种形态的位置。

### 3.5 L4 `repl` 渲染与输出——缺「控制通道」，记账是无锁的

- `turn.Handle` 只接受 `agent.Event` 一类输入（数据事件，`repl/flow.go:76-101`），**没有承载「控制命令」的第二入口**（开/关/重放）。
- `toolView.dirty`/`justEnded`、`turn.reasonOpen`/`reasonStart` 全无锁（`repl/toolview.go:422-431`、`repl/flow.go:44-52`）。任何来自第二个 goroutine 的渲染动作都会撕裂状态——这也是「必须在下一个 delta 里轮询开关」这类绕法的根因。
- 旁证：`heartbeat` 之所以能是后台 goroutine，是因为它采用**追加式输出**（不重绘、只追加点，`repl/status.go:70-110`）绕开了协调。那是一处「缺抽象后用输出策略规避」的补丁。

### 3.6 L5 `agent` 事件源——模型与 Console 不一致，且缺段落语义

- **两个事件源模型不同**：`agent` 是同步回调 `EventSink`（push，`agent/event.go:24-33`、发射点 `agent/agent.go:280-316`、`agent/llm.go:239-272`、`agent/llm_responses.go:191-228`），`Console` 是拉取（pull）。**没有一个循环能同时消费两者**——这是「必须两个 goroutine + 跨线程状态」的结构性原因。
- **缺段生命周期事件**：reasoning 段的结束上游其实知道——responses 协议有 `response.reasoning_text.done` 等类型，现有代码只处理 delta、其余 type 落空（`agent/llm_responses.go:191-200`）；chat 协议的 reasoning/content 在同一个 delta 循环里交替，边界天然可得（`agent/llm.go:245-256`）。现在段边界靠 `repl` 按「下一条事件是什么种类」反推（`repl/flow.go:76-101`），于是有了「清空时机要自己对账、边界集合要凑齐」这类易错处理。

### 3.7 L6 `render/markdown`——流式缓冲不持有输入，不可重放

`MarkdownBuf` 原本只有 `Write`/`Close`/`Reset`，`Reset` 只清解析状态、不留原文。于是「把本段从头重放」的**原料（`segBuf`）只能由消费者自己攒**，连带内存上限也落到消费者身上。**本条已消除**：P4 已落地 `MarkdownBuf.Rewind()`（见 §7 P4，2026-09-27），缓冲自身持有原文并可按段首重放；本段保留为诊断当时的记录。

---

## 4 额外处理 → 缺失抽象（归因表）

这是本文件的核心结论：把「本来要写的补丁」逐条归因到**缺哪个抽象**。

| # | 额外处理（不做抽象就得写） | 缺失抽象 | 抽象落点 |
|---|---|---|---|
| 1 | 自建读循环、自管 `BeginRead`/`EndRead` | Console 无常驻 reader，`Subscribe` 不推 Key | L1/L2（A） |
| 2 | 工具期停读、且不敢恢复（恢复要面对模式竞争） | 读权仲裁（Reader ↔ Lease）不存在 | L2（B） |
| 3 | `atomic` 开关 + 「等下一个 delta 才生效」 | 事件源未合流，无单点顺序循环 | L4/L5（C） |
| 4 | `toolView.dirty` 不可跨 goroutine | 同上 | L4（C） |
| 5 | 自攒 `segBuf` + 1 MB 上限 | 渲染缓冲不持有输入 | L6（E） |
| 6 | 段边界集合自己凑、`segBuf` 清空时机自己对账 | 缺段生命周期事件 | L5（D） |
| 7 | 提示行与流式文本的插入协调（打断当前行、停心跳） | 渲染侧无「控制命令」类型入口 | L4（C） |

---

## 5 缺失抽象清单

| 代号 | 缺失抽象 | 具体缺口 | 本功能是否必需 |
|---|---|---|---|
| A | **Console 常驻读者 + 全事件推送** | Console 拥有读循环；空闲期（无人 `BeginRead`）后台读并把 `Key`/`Interrupt` 推给订阅者；消费者不再需要 `ReadEvent` 才能收键 | 否（P1 可在 repl 内自建），是架构债——**已实施**（P3，2026-09-27，`SubscribeKeys`） |
| B | **读权仲裁（Reader ↔ Lease）** | 借出时自动挂起读循环并交模式，归还后自动恢复；订阅者零感知 | 否（P1 用「工具期整轮停」规避）——**已实施**（P3，2026-09-27） |
| C | **事件合流 / 单点事件循环** | agent 事件与终端事件汇入同一循环，一个 goroutine 顺序消费 | **是**（P1 核心） |
| D | **段生命周期事件** | `agent` 显式发 reasoned 段边界（`EventReasoningEnd` 等） | 否（可先按种类推断），但消掉易错处理——**已实施**（P2，2026-09-27） |
| E | **渲染缓冲可重放** | `MarkdownBuf` 自身持有已喂输入并提供重放 | 否（可先自攒 `segBuf`）——**已实施**（P4，2026-09-27，`Rewind`） |

---

## 6 方案演进：被否的方案与采纳的方案

### 6.1 被否：repl 内自建读循环 + 在 delta 回调里轮询开关

形态：回合期起一个读键 goroutine，`Ctrl+O` 只翻转 `atomic.Bool`；`turn` 在每个 reasoning delta 里对比开关状态，由关变开则重放 `segBuf`。零新增接口、不动 `readline`。

**否决理由**（记录以免重复提出）：

1. **按键响应延迟无上界**——模型长时间不出 token 时按了没反应，直到下一个 delta；
2. 被迫引入跨 goroutine 状态（`atomic` 开关）与「渲染必须留在 turn goroutine」的隐性约束，是 §3.5 那类记账问题的放大；
3. 每加一个流式期输入（不只是 `Ctrl+O`）都要重复这一套，因为抽象仍落在调用方。

### 6.2 采纳：事件合流（单 goroutine 顺序循环）

把 agent 事件（回调 → channel）与终端事件（读循环 → channel）汇入同一 `select`，回合渲染回到**单 goroutine 顺序执行**。直接收益：

- 零 `atomic`、零锁（`dirty`/`reasonOpen` 恢复为普通字段即可跨帧使用）；
- **零延迟**——按键在自己的 `case` 里立即处理（含重放）；
- 「工具期不响应」从「跨 goroutine 停读握手」变成循环里的一个状态位；
- 渲染状态机变成线性可读的 switch，测试只需喂事件序列，不需要终端。

这一条完全落在 `repl` 内，`agent` 与 `readline` 的接口**都不动**（`agent` 的 `EventSink` 回调签名保持不变，在 `repl` 侧包一层 channel 即可）。

---

## 7 分阶段方案

每阶段格式：目标 / 范围 / 形态 / 行为边界 / 验收 / 回归风险 / 待评审决策点。

### P1 回合事件合流 + 思考期 `Ctrl+O`（本功能本体）——**已实施**（2026-09-27）

**目标**：交付 §1 需求边界里的全部行为；同时把回合渲染改成单 goroutine 顺序循环。

**范围**

| 文件 | 改动 |
|---|---|
| `readline/keys.go` | 新增 `KeyCtrlO` 常量 + `case b == 0x0f` 分支（现在 `0x0f` 落 `b < 0x20` 被静默丢弃，`:130`）；两平台共用此文件 |
| `repl/`（新文件，如 `turnloop.go`） | 读键 goroutine（`BeginRead` + `ReadEvent` 循环 → channel；门禁不过则不起）与回合主循环（`select` 三路） |
| `repl/repl.go` | `ask` 拆为「起循环 + `runTurn`」；`showReasoning` 保持普通 `bool`（合流后单 goroutine） |
| `repl/flow.go` | `turn` 增 `segBuf`（本段全文，含关闭期累积）+ `shown`（上次生效状态）；新增控制入口 `Hotkey`；段边界清 `segBuf` |
| `repl/messages.go` | 开关提示行（开 / 关两档） |

**实施记录（与原形态的偏差与补充，均经评审产出）**：

- **ToolStart 同步握手（ack）**：事件 channel 化后 `Emit` 不再阻塞，`dispatch`（工具执行，可能 `LendFull`）会先于渲染与停读——原形态把 `stopHotkeys` 放在主循环分支里拦不住。落地为：`EventToolStart` 附 `ack chan struct{}`，sink 侧发送后等 ack（或 `ctx.Done`）再返回；主循环**先 stop（停读循环）→ 渲染 → close(ack)**，工具执行时终端无并发读者且模式已还原。`ToolEnd` 无需 ack（FIFO 保序、其后无终端副作用）。
- **尾事件不丢**：完成信号不走独立 channel（与事件 channel 的 select 竞态会丢尾部事件如 `EventResponse`），改为 `inDone` 消息与事件同 channel FIFO——单发送者（agent goroutine），`inDone` 必然排在全部事件之后。
- **`EventIdle`（超出「readline 只加 KeyCtrlO」的预估）**：`keySource.readKey` 对 `n==0`（VTIME 超时）原本无限 continue，`ReadEvent` 无键时**永不返回**——读循环无从检查 stop、stop 的 join 死等（fakeTerm 模拟不了此行为，测试盲区；真 pty 由 render_audit 抓出）。落地为：`readKey` 空闲周期返回 `errIdle`，`consoleImpl.ReadEvent` 转换为 `Event{Kind: EventIdle}`（不 dispatch）；editor/picker 的 `Kind != EventKey → continue` 天然容忍。设备层「空闲不返回」的旧契约（`TestReadKeyIdleIsNotEOF` 等）同步更新为「空闲返回 idle、绝不变 EOF」。
- **stopCh 循环头检查**：读循环丢弃非 CtrlO 键后 continue 不查 stopCh——变异测试暴露的死锁缺陷，已修（循环头非阻塞 select）。stop = close(stopCh) + join（EndRead 归读循环 defer，串行无竞争）；join 上限 ≈ 一个 VTIME 周期（100ms）。
- **门禁限 posix**：`hotkeysSupported`（linux/darwin）分片，Windows 暂不启用（readChunk 虽有超时返回、机制同构，但实机未验证，随 P3 放开）。
- **重放 = `Rewind` + `Close`**：`Rewind` 只返回已闭合块，未闭合尾段要 `Close` 补齐，否则按开后屏上缺最后一截。
- **input 上限落 `render/markdown`**（P4 预告的决策 3）：`SetInputLimit(n)`（零值无限），`Write` 超限跳过 `input` 累积、`feed` 照常（流式渲染不受影响，只降级 `Rewind` 完整性）；`InputLen()` 供热键判空。repl 对正文与思维链两缓冲均设 1MB。

**形态**（要点，非最终代码）

```go
// 回合主循环：三路事件，单 goroutine
events := make(chan agent.Event, 64)
doneCh := make(chan error, 1)
go func() { doneCh <- r.agent.Ask(ctx, q, func(e agent.Event) {
    select { case events <- e: case <-ctx.Done(): }
}) }()
keys := r.startHotkeys(t)           // 门禁：r.keys && r.reasonVisible()

for {
    select {
    case e := <-events:
        if e.Kind == agent.EventToolStart { r.stopHotkeys() }  // 整轮排除工具期
        t.Handle(e)
    case k := <-keys:
        t.Hotkey(k)                 // 立即处理：翻开关 + 重放/收尾
    case err := <-doneCh:
        return err
    }
}
```

读键 goroutine：`BeginRead` → `for { ReadEvent }` → 只投递 `EventKey`/`KeyCtrlO`（`EventInterrupt` 仍由 `InterruptContext` 的订阅者处理，互不干扰）→ 收到 stop 或读错即 `EndRead` 退出。`stop` 同步等待退出（`Keys` 是 `VMIN=0/VTIME=1` 轮询，最坏 ~100 ms），**必须等 `EndRead` 完成后才让工具借出终端**（否则与 `LendFull` 的 pty 泵抢字节）。回合末 `turn.End` 亦停（幂等）。

**渲染状态机**（`turn`）

| 事件 | 行为 |
|---|---|
| `EventReasoning` delta | 累积 `segBuf`（上限 1 MB，超限停累积）；开关开且无打开块 → 打上分隔 + 全量喂 `segBuf`；开关开且块已开 → 喂该 delta；开关关 → 只累积 |
| `Ctrl+O`（`Hotkey`） | 翻开关；由关变开且本段有内容 → 收尾旧块 + 重放 `segBuf` 全文；由开变关 → 收尾（补带时长的下分隔）；本段无内容 → 只翻开关 + 提示行（下一段生效） |
| 段边界（`EventContent`/`EventToolStart`/`EventResponse`/`End`） | 收尾当前块 + 清 `segBuf` |
| 反复开关 | 按同一模式重放整段，接受重复显示（块时长各自计时） |

**行为边界**：工具期整轮不响应（含工具后第二轮思考）；`^C` 在流式期经 device 归一为 `EventInterrupt`（键路径）→ `InterruptContext` 取消，与现状信号路径等价；plain/非 TTY 不起监听，按键丢弃（与现状一致）；用户流式期乱敲的其他键被读循环消费丢弃（现状也会在回合末被 `TCSETSF` 冲掉，行为等价）。

**验收**：单测——`0x0f → KeyCtrlO`；状态机（段首开、段中开→重放、段中关、反复开关、段结束开→下段生效、超限降级）；合流循环（伪 sink + 伪 console 喂序列，断言事件顺序与 `End` 只调一次）；`-race` 全绿。pty——思考中喂 `Ctrl+O` 后屏上出现完整段（需给 `scripts/render_audit.py` 的 mock SSE 加 delta 间隔，见 §9）。真机——`Ctrl+O` 开关、`^C` 仍中断回合、退出后 `stty -a` 无残留。

**回归风险**：`ask` 由同步调用改为「goroutine + 事件循环」，需保证 ①错误只回传一次；②ctx 取消时 sink 不阻塞（`select` + `ctx.Done()`）；③`t.End(err)` 在循环退出后调用一次；④心跳/通知的时序不变（`TestNoticeTurnPersisted` 一类用例）。

**待评审决策点**：①工具期「整轮停」是否接受（对照：可恢复但需处理重新 `Raw` 与 `TCSETSF` 丢键）；②`segBuf` 上限取值与降级提示；③提示行两档是否可以（原方案四档）；④`Hotkey` 放在 `turn` 还是 `flow`。

### P2 `agent` 段生命周期事件（消掉边界推断）——**已实施**（2026-09-27）

**目标**：段边界由上游显式给出，P1 的状态机去掉「按事件种类反推」。

**范围**：`agent/event.go` 增 `EventReasoningEnd`（是否携带全文/时长待定，见决策点）；`llm_responses.go` 接 `response.reasoning_text.done` / `response.reasoning_summary_part.done` / `output_item.done`（item type = `reasoning`）；`llm.go` 在 reasoning→content/tool_call 切换处补发；`repl/flow.go` 改用新事件清 `segBuf`（保留按种类兜底）。

**验收**：两协议各自的段边界用例（含「一段内多 item」「段后直接工具调用」「空段」）；`repl` 侧断言不再依赖种类推断（构造只发 `EventReasoningEnd` 的序列）。**风险**：新增事件不改既有语义，`repl` 外的消费者（回放、统计）需确认忽略即可。

**实施记录（与原形态的偏差与补充）**：

- **不带载荷**（决策 5 裁定）：`EventReasoningEnd` 是裸事件，不携带段全文/段时长——`repl` 已持有本段原文（`MarkdownBuf` 留存，P4），时长口径属渲染侧。
- **常量追加在末尾**：`EventReasoningEnd` 追加在 `EventKind` 常量块末位而非插在 `EventReasoning` 之后，**既有导出常量的数值不变**（`agent` 是库，数值可被外部观察）；该块内的顺序不代表事件时序。
- **空段门禁**：两条流各自持有 `reasonOpen`，`endReason()` 只在段打开时外发并复位。两个直接收益：①`reasoning_summary_part.done` 之类的空段的 `_done` 事件不会制造幽灵段结束；②段结束事件恰好一次（`_text.done`、`item.done`、正文边界、流结束兜底四条路径互为幂等）。
- **responses 只接 `_text.done` 变体，不接 `reasoning_summary_part.done`**（与原范围的偏差）：`part.done` 是**段内**边界（一个 summary 可含多个 part），若据此收尾会按 part 把一个段切成多个「思考」块；段收尾由 `response.reasoning_text.done` / `response.reasoning_summary_text.done` / `output_item.done`(reasoning) 覆盖。
- **`repl` 保留按种类兜底**：`turn.Handle` 新增 `EventReasoningEnd` → `endReasonSeg()`，原有 `EventContent`/`EventToolStart`/`EventResponse` 三处仍调同一方法（幂等），故 provider 未发 `_done` 事件时行为与 P1 完全一致。
- **多 item 即多段**：一段响应里出现两个 reasoning item（或两段 reasoning 会被 `_done` 分隔）时，第一段收尾、第二段另起块——比 P1「同类事件合并为一段」更贴近上游语义，且段结束同时清段缓冲，第二段的重放不会带出第一段。

**测试**（按上文「测试」口径）：`agent/llm_test.go` 的 `TestChatStreamReasoningEndEvents`（正文边界 / 工具调用边界 / 流结束兜底 / 无思维链不发，断言段结束恰好一次且紧随最后一个 delta），`agent/llm_responses_test.go` 的 `TestResponsesStreamReasoningEndEvents`（`_text.done` / `_summary_text.done` / `item.done` / 靠正文边界 / 仅思考靠流结束 / 无思维链不发）与 `TestResponsesStreamReasoningEndMultiItem`（`_text.done` 与 `item.done` 各分隔两段、空段 `done` 不发、非 reasoning 的 `item.done` 不切段、工具调用 delta 收尾）；`repl/turnloop_test.go` 增 `TestTurnReasoningEndClosesSeg`（段结束收尾、其后 delta 另起一段且不重放旧段）与 `TestTurnReasoningEndResetsSegBuf`（只发段结束事件的序列即清段缓冲、开档回落「下一段生效」——不依赖种类推断）。mock 侧 `mockStep` 增 `reasoningDone`（`text`/`summary`/`item` 三种收尾事件与 summary delta 变体）与 `rawSSE`（逐条原样写出的流，用于多 item/无 delta 等精确序列）。**变异复验 11 处**（chat 原文两跳 + 三处边界、responses 四个分支 + item type 过滤 + 流结束兜底 + 空段门禁、repl 不识事件）全部由对应用例拦下；其中「done 与正文边界相邻」的用例起初区分不出分支，据此补了多 item 与「非 reasoning item.done」两条精确序列用例。

### P3 `Console` 常驻 reader 与借出仲裁（还 `terminal-console.md` 的债）——**已实施**（2026-09-27）

**目标**：把「读循环」与「谁在读」从消费者收回 L2；`Subscribe` 覆盖全部事件；借出自动挂起/恢复。

**范围**：`readline/console.go`（状态机：`Idle`/`Exclusive`/`Lent`）、`device_*`（Idle 期可否后台读的设备差异：posix 可以、**pipe 不行**——后台读会吃光 stdin，故 pipe 只在 `Exclusive` 读）、`lease_*`（借出前后挂起/恢复）、`repl`（P1 的读键 goroutine 删除，改为订阅）。**接口变化**：`Subscribe` 语义扩展为全事件；`BeginRead`/`EndRead` 保留给编辑器/picker（独占）。

**验收**：单测用 fake device 覆盖状态机（Idle 读、Exclusive 抢占、Lent 挂起、归还恢复、pipe 不后台读）；`render_audit` 全绿；真机 interactive 借出期间按键完全归子进程、`stty -a` 无残留。**风险**：Idle 期常驻 `Keys` 模式改变了回合期终端模式（现状 `Cooked`），需重新核 `^C` 路径与子进程继承的模式；darwin/Windows 分片需各自验证（Windows 未实机）。

**实施记录（与原形态的偏差与补充）**：

- **按键订阅与普通订阅分开**（与「`Subscribe` 语义扩展为全事件」的字面偏差）：保留 `Subscribe(fn)` 只做事件通知，新增 **`SubscribeKeys(fn)`** 才武装常驻读者。理由：`repl.InterruptContext` 每回合都订阅（要的是中断），若它顺带武装读者，则**所有** TTY 回合（含 plain/`-p`）都会把终端切成 keys 模式——决策 6 的风险面被无谓放大；分开后终端模式变化范围与 P1 完全一致（仅 `hotkeysSupported && keys && 富档` 的回合），`^C` 路径与子进程继承的模式不变。`Subscribe` 仍按承诺推全部「产生出来的」事件（键由读者产生时也推）。
- **读者的终端模式保留 `OPOST`**（决策 6 排查出的真问题）：新原语 `device.ReaderRaw()` = 输入侧 raw（清 `ICANON`/`ECHO`/`ISIG`/`IEXTEN`、`VMIN=0`/`VTIME=1`）**但保留 `Oflag`**，与独占 `Raw()`（连 `OPOST` 一起清）不同。tanya 的输出链路逐块写 `\n`、依赖内核 `ONLCR` 补 CR；读者期丢 `OPOST` 会让工具块与状态行在真实终端上错位（`render_audit` 的 VT 回放按 raw 语义还原，`clean-tool-builtin-args` 直接红）。修复后 audit 15 PASS，并加 `TestReaderTermiosKeepsOutputProcessing` 回归守护。
- **工具后热键恢复**（P1 的「整轮停」升级）：借出由 Console 挂起读者、归还自动恢复，于是工具后的第二轮思考期 `Ctrl+O` 仍可用（P1 是整轮不恢复）。`repl` 侧相应去掉 `stop()`/`keys = nil`，整个回合保持按键订阅。
- **ack 握手保留但语义收窄**：`EventToolStart` 的 ack 现在只保证「工具块渲染先于工具执行」（读权安全已由 Console 的挂起负责）。
- **`broken` 粘住**：读者异常退出（设备 EOF）时自行复原终端并停止自启，下一次成功 `BeginRead` 复位——避免「EOF → 立刻重启 → EOF」热循环。
- **`repl` 侧删除**：`startHotkeys` 的 `BeginRead` 读循环、`stop`/join、`sync.Once`、按键丢弃逻辑（P1 的读循环整体退场，改 8 行订阅）；`repl/faketerm_test.go` 的 `park`/`endReads` 断言随之退场（改为「工具块先于工具执行」与「工具后热键仍生效」两条用例，后者由 SSE 侧 `push` 驱动）。

**测试**：`readline/console_reader_test.go`（新增 `scriptDevice`：可喂键、可注错、带 `backgroundRead`）六条——按键订阅起读者并送键、取消订阅停读者并复原、普通订阅不起读者、`BeginRead` 抢占 + `EndRead` 恢复、`LendStdin` 挂起（借出期不送键）+ `Release` 恢复（幂等 Release）、`broken` 不自启且 `BeginRead` 后恢复；`readline/device_posix_test.go` 增 `TestReaderTermiosKeepsOutputProcessing`（读者保 `OPOST`、独占 `Raw` 清 `OPOST`）；`repl/turnloop_test.go` 的 `TestRunTurnCtrlOStream`/`TestRunTurnHotkeyAfterTool` 改由订阅投键。**变异复验 11 处**（`BeginRead` 不抢占、借出不挂起、归还后不恢复、退订不停读者、无按键订阅也起读者、忽略设备后台读能力、异常退出不置 `broken`、`EndRead` 不恢复、repl 走普通订阅、ack 先放行再渲染、读者模式丢 `OPOST`）全部被拦。**手工 pty 复核**：三段场景（流式 `Ctrl+O`→exit / 工具借出→工具后热键→exit / 流式 `^C` 中断→exit）各 8 次，前后 `stty -g` 逐字节一致、无残留。

### P4 `MarkdownBuf` 持有输入与重放（可选优化）——**机制已实施**（2026-09-27）

**已落地**：`MarkdownBuf` 持有已喂入原文，新增 `Rewind() []ir.Block`（丢弃解析状态、按原文重放整段；保留宽度、可继续 `Write`、可反复调用）；`Reset()` 因全仓零消费者随之删除。测试：`render/markdown/markdown_test.go` 新增七用例（与流式等价、重放后追加、宽度保留、空缓冲、幂等、尾部未闭合 fence 的重置、`Close` 后重放），四处变异各自可拦。**未落地**：输入留存上限——取值与降级行为属 §8 决策 3（P1），届时随该决策落在本层；`repl` 侧消费（删 `segBuf`）随 P1 一起做。

**目标**：`segBuf` 与上限从 `repl` 移入渲染缓冲，消费者只说「重放本段」。

**原计划范围**：`render/markdown`（原文留存 + 重放；上限归此层）、`repl/flow.go`（改为调用）。其中「原文留存 + 重放」已按上段落地；余下两项（留存上限、`repl` 侧 `segBuf` 删除）分别随 §8 决策 3 与 P1 落地。**风险复核**：内存翻倍（原文 + 解析态）已确认可接受——`MarkdownBuf` 生命周期为回合级（`beginTurn` 创建、`turn.End` 释放），文本规模 KB 级；`Reset()` 全仓零调用点，删除无回归面。

### P5 交互文案与文档收尾

**范围**：`repl/messages.go`（若 P1 决定做四档文案）、`README.md`（快捷键表与限制）、`AGENTS.md`（约束段一句：流式期输入的事件合流与工具期不响应）、`docs/reasoning-live-toggle.md`（改归档指针）、`CHANGELOG.md`。

### 7.1 阶段依赖、规模与独立性

| 阶段 | 依赖 | 估算规模 | 可独立实施 | 可独立验收 |
|---|---|---|---|---|
| P1 | 无 | 中（~200 行 + 测试）——**已实施**（2026-09-27，含评审补充的 ack 握手与 `EventIdle`） | ✅ | ✅ |
| P2 | 无（P1 受益） | 小（~40 行 + 测试）——**已实施**（2026-09-27） | ✅ | ✅ |
| P3 | 无（与 P1/P2 互不影响） | 中偏大（console/device/lease + 分片）——**已实施**（2026-09-27） | ✅ | ✅ |
| P4 | 无（P1 受益） | 小——**机制已实施**（2026-09-27），余上限待 P1 决策 | ✅ | ✅ |
| P5 | P1 | 小（文档） | ✅ | — |

顺序建议：~~**P1 单独一个会话**~~（已完成）→ ~~**P2 一个会话**~~（已完成）→ ~~**P3 一个会话**~~（已完成，2026-09-27；`EventIdle` 复核到位、Windows 门禁维持关闭）→ **P5 文档收尾**（P4 已先行落地）。

---

## 8 待评审决策点（汇总，供逐条裁定）

| # | 阶段 | 决策点 | 倾向 |
|---|---|---|---|
| 1 | P1 | 工具期「整轮停」vs「暂停可恢复」 | **已裁定并实施**（整轮停，含工具后第二轮思考） |
| 2 | P1 | `Ctrl+O` 提示行档数（两档 vs 原方案四档） | **已裁定并实施**（两档 + 空段「下一段生效」提示） |
| 3 | P1 | `segBuf` 上限与超限行为 | **已裁定并实施**（1 MB，落 `MarkdownBuf.SetInputLimit`，正文与思维链两缓冲同设；超限停累积、流式渲染不受影响、`Rewind` 降级为前缀、下一段恢复） |
| 4 | P1 | 「段已结束时按开」是否给提示行 | **已裁定并实施**（给一行「下一段生效」） |
| 5 | P2 | `EventReasoningEnd` 是否携带段全文/段时长 | **已裁定并实施**（不带；`repl` 已持有全文，时长口径属渲染侧） |
| 6 | P3 | Idle 期常驻 `Keys` 模式是否可接受（改变回合期终端模式与 `^C` 路径观察） | **已裁定并实施**（2026-09-27：读者只在按键订阅期运行、且**保留 `OPOST`**，故输出链路与 `^C` 路径均不变；`stty -g` 前后一致已手工复核 8 次，子进程继承的模式与 P1 相同） |
| 7 | P3 | pipe 设备 Idle 期不后台读（避免吃光 stdin） | **已实施**（`pipeDevice` 不声明 `backgroundRead`；Windows 控制台同样不声明，未实机验证） |
| 8 | P3 | 是否保留 `ReadEvent` 公开面 | **已实施**（保留；编辑器/picker 的 `BeginRead`…`EndRead` 独占形态不变） |
| 9 | P4 | 重放能力放 `MarkdownBuf`（原文留存）vs 维持 `repl` 自攒 | **已裁定并实施**（2026-09-27：机制入 `MarkdownBuf.Rewind`，`Reset` 删除）；仅留存上限待决策 3 一并定 |

---

## 9 统一验收与回归清单

- `go build ./... && go vet ./... && go test ./... && go test -race ./...`（三平台 build/vet）
- `python3 scripts/render_audit.py`（pty + VT 回放，需先 `make build`）
- ~~**测试基建缺口**：`scripts/render_audit.py` 的 mock SSE 一次性写完、无 delta 间隔~~——**已补**（P1 落地 `--sse-delay` 全局参数与 step 级 `delay`/`reasoning_deltas` 字段，并新增 `clean-reasoning-toggle` 场景端到端验证流式中 `Ctrl+O`）。
- 真机（`make build` 后用 `./tanya`，不用 `go run`）：流式期按 `Ctrl+O` 立即看到整段并继续追加；工具期按键完全无效；`^C` 仍中断回合并杀子进程组；`interactive: true` 借出期按键归子进程；退出后 `stty -a` 无残留。

## 10 明确不做

- 不做通用事件总线 / TUI 框架 / 插件机制；
- 不改 `agent.EventSink` 的回调签名（合流的 channel 包装留在 `repl` 侧）；
- 不做提示符期快捷键（命令足够）；
- 不做「同一时刻多个非独占读者」的通用仲裁（P3 只做 Reader↔Lease 两态）；
- 不改 `agent` 的事件粒度语义（P2 只**新增**段边界事件）；
- 不为 Windows/macOS 做本功能之外的特化（P3 的分片按既有白名单处理）。

## 11 现状事实索引（评估依据，`文件:行`）

- 事件与接口：`readline/console.go:23-53`（`EventKind`/`Event`/`Console`/device 接口）、`:78-137`（信号中断、`ReadEvent` 内 `dispatch`、`Subscribe`）
- 键解析：`readline/keys.go:5-41`（`KeyCode`/`KeyEvent`）、`:92-144`（`parse`，`0x0f` 落 `:130` 的 `b < 0x20` 丢弃）、`:197-202`（`keyEvent` 的 `Ctrl+C` 归一）
- 模式与轮询：`readline/device_posix.go:35-58`（`Raw`/`keysTermios`：`VMIN=0/VTIME=1`）、`:67-75`（`Restore`/`Sane`）、`ctty/termios_linux.go:20-22`（`TCSETSF`）
- 拉取式读：`readline/device_io.go:16-77`（`keySource.readKey`）、`:27`（唯一调用点）
- 租约与自持泵：`readline/lease.go:41-46`、`readline/lease_linux.go:21-44`（`bridgeTTY` 字段）、`:79-160`（`prepare`/`attach`/`stop`）、`readline/lease_posix.go:9-29`
- 消费者：`readline/editor.go:51`（`out: os.Stdout` 硬编码，另一条待办）、`:68-117`（独占同步环）、`repl/picker.go:128-155`；常驻读者与状态机见 `readline/console.go`（`consoleState`/`consoleReader`/`maybeStartReaderLocked`/`beginLend`）、`readline/device_posix.go` 的 `backgroundRead`/`ReaderRaw`/`readerTermios`
- 回合渲染：`repl/flow.go:30-53`（`flow`/`turn` 字段）、`:84-115`（`Handle`，`EventReasoningEnd` 于 `:100`）、`:117-163`（`writeReasoning`/`flushReason`/`endReasonSeg`）
- 输出与记账：`repl/toolview.go:422-479`、`repl/streams.go:80-104`、`repl/status.go:70-110`
- REPL 与中断：`repl/repl.go:26-38`、`:300-320`
- agent 事件源：`agent/event.go:3-31`（`EventKind` 词汇表，`EventReasoningEnd` 于 `:14`）、`agent/agent.go:280-316`、`agent/llm.go:239-293`（chat 的 reasoning delta 与 `endReason` 四处触发点）、`agent/llm_responses.go:191-278`（responses 的 delta / `_text.done` / `item.done` / 正文边界 / 流结束）
- 渲染缓冲：`render/markdown/markdown.go:48-73`（`NewMarkdownBuf`/`SetWidth`/`Rewind`/`Write`/`Close`）
