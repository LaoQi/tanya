# repl 输出收敛与数据流封装方案

> 状态：方案定稿，分阶段实施（阶段 0-4，见 §6）
> 范围：`repl` 包内重构 + `main.go` 接线
> 不动的部分：`agent`（协议/事件/history/落盘）、`style` 渲染纯函数与过滤器、`readline` 内部实现

## 1. 背景

### 1.1 触发事件

会话 `20260911-165625` 用 `/history` 查看时出现三种现象：中间开始整段变黄、`pong` 与状态行插进列表中间、行内容互相覆盖。根因不在渲染算法，而在**输出没有单一出口、没有写权契约、没有统一清洗约定**：

- `/history` 摘要行（`historyLine`）绕过 `style` 管线直写 `fmt.Println`，原始 `\r`、`\x1b[K`、`\x1b[33m` 直接进终端：`\r\x1b[K` 触发回车清行，把已打印的摘要行覆盖成 `pong` + 状态行；`truncateRunes` 按 120 rune 硬截断又切断了 `\x1b[33m` 的闭合序列，导致后续输出整段继承黄色。
- 这些字节来自当时那条 `run_shell` 命令的产物——命令本身是在跑 tanyan（E2E），子 tanyan 的 UI 写到了 stdout，被父进程原样捕获成 tool 结果。

### 1.2 已完成的止血（commit 54897cb）

| 改动 | 位置 | 内容 |
|---|---|---|
| `style.OneLine` | `style/filter.go` | 复用 `scanSequence` 精确剥离 ANSI 转义、空白折叠、丢弃其余 C0，输出单行纯文本 |
| 摘要行清洗 | `repl/repl.go` `historyLine` | 先 `OneLine` 再按 120 rune 截断，序列不再被切断 |
| 测试 | `style/filter_test.go`、`repl/history_test.go` | 未闭合 SGR、`\r\x1b[K`、OSC、BEL、超长截断 |
| 文档 | `docs/design.md` | `/history` 摘要行行为同步 |

止血只解决了"显示侧读历史"这一条路径（commit 54897cb），**结构性问题原样保留**：写点分散、回放与实时两条组装路径、清洗靠逐个补。

### 1.3 现状盘点

**写点分布**（口径：`grep -c 'fmt\.Print\|fmt\.Fprint\|os\.Stdout'`）：

| 文件 | 写点数 | 说明 |
|---|---|---|
| `repl/repl.go` | 51 | 欢迎屏、回合分隔线、命令反馈、错误、主题、think、历史回放、load |
| `repl/toolview.go` | 8 | `WireToolView` 闭包内的事件渲染 |
| `repl/picker.go` | 8 | 会话选择菜单自绘 |
| `repl/spinner.go` | 3 | 动画 goroutine（`out io.Writer`，默认 `os.Stdout`） |
| `readline/editor.go` | 11 | 提示符、回显、补全菜单、ghost（已有 `SetOutput`） |
| `main.go` | 8 | 启动错误、ask 单发收尾 |

**抽象现状**：

- 唯一"统一出口"雏形是 `REPL.print`，但它借道 `r.view(agent.Event{Kind: EventContent, Text: text})`——用 LLM 内容事件表达 repl 自己的显示文本，语义错位，且借用了闭包里的 spinner/`lineDirty` 状态。
- `WireToolView` 返回**闭包**，`spinner`、`lineDirty`、`toolJustEnded`、`mu` 四个状态藏在捕获变量里，无法注入 writer、无法断言。
- 历史回放（`printHistoryFull`/`printRendered`）自己组装，不复用实时路径——`historyLine` 漏清洗正是分叉产物。
- 终端能力是包级全局：`toolTerm`/`toolTTY` + `ensureToolTerm`（`toolview.go:307`）。
- `style.GetProfile()` 是包级全局，repl 逻辑直接读，测试只能依赖全局默认值。

**数据侧事实**（评估用，非本方案改动对象）：

- 全库 28 个会话、133 条工具消息含 ESC/CR；其中 UI 文案（spinner / 状态行）出现在"命令内容含 tanyan"的结果里，其余多为命令自产 ANSI 或读源码/文档时的字面量命中。
- 复现实验（本地 mock LLM）：
  - `tanyan -n ask "hi" > f 2>&1`（stdin 为 tty、stdout 被重定向）→ `f` 含 `\r\x1b[K\x1b[33m⠋ 等待响应 0s\x1b[0m\r\x1b[Kpong\n\x1b[94m  ↳ TTFT …`；
  - `tanyan -n ask "hi" < /dev/null > f2 2>&1` → spinner 消失，`  ↳ TTFT …` 仍在。
- 两个成因独立：spinner 取决于 `style.DetectProfile(repl.ToolTTY())`，而 `ToolTTY()` 经 `readline.NewTerminal()` 最终只看 **stdin** 的 termios（`readline/terminal_unix.go:19`）；状态行在 `WireToolView` 的 `EventResponse` 分支**没有任何 TTY 门槛**。宽度反而已经取 stdout（`terminal_unix.go:51` 的 `TIOCGWINSZ`）。

**写权现状（五方写 stdout，无契约）**：`Editor`（raw 期）、`output`（改造后）、`spinner` goroutine、`picker`（raw 期自绘）、`bridge`（interactive 期间子进程直写真实 tty）。当前只靠"时序刚好不重叠"与手工传递的一把 `sync.Mutex`。

### 1.4 为什么现在改

1. 本次修复是"点状补漏"，同类问题（清洗漏一处、写点漏一处、回放与实时分叉）还会再发生；
2. 测试无法注入输出，只能靠替换 `os.Stdout`（`captureStdout`）与全局 profile，覆盖面受限；
3. 后续任何输出侧需求（回放复用实时渲染、日志分流、`ask` 输出可解析）都需要一个明确的落点，而不是继续加 `if`。

## 2. 目标与非目标

**目标**

1. **输出收敛**：repl 内所有输出走唯一出口 `output`，writer 可注入、互斥统一。
2. **数据流封装**：引入 `flow` 上下文与 `Phase`/`Kind` 枚举，回合生命周期显式化（`turn`），`REPL` 只保留装配与分发。
3. **可测试性**：测试可注入 writer 与 fake 终端，不再替换 `os.Stdout`；写权冲突以协议断言覆盖。

**非目标（明确不做）**

- 不做 Collector / Presenter / 标签策略表 / 过滤器注册表（曾评估后废弃）。
- 不改 `agent` 的事件协议、history、落盘与请求构造（模型通道零变换红线）。
- 不改 `style` 的渲染纯函数与过滤器（`Render*`、`Frame`、`Passthrough`、`OneLine`）。
- 不改 `readline` 内部实现，只用其现成的 `SetOutput`。
- 不做 UI / 数据分流（把 UI 改走 stderr 或 `/dev/tty`）：本方案只让"将来能换 writer"，不改变当前行为。
- 不改动"非 TTY 保留状态行"的既有契约（`docs/design.md:126`、`README.md:120`）。

## 3. 设计方向

### 3.1 目标态数据流

```
Run
 ├─ phase=Input   ed.Readline(prompt)            ← 输入期唯一写者：Editor
 ├─ dispatch(line) → intent(斜杠/退出/对话/直问)
 │    ├─ 斜杠命令      phase=Command  ─┐
 │    ├─ 回合 r.runTurn(text)  phase=Turn ├─► flow{out, phase, kind, prof, width, md}
 │    └─ 回放 /history  phase=Replay    ─┘        │
 │                                                 ├─ emit(kind, text)  追加
 │                                                 └─ live(kind, text)  原地重绘
 └─ 回合结束 turn.End() → md 结算 / 错误文案 / 回合分隔线        │
                                                                 ▼
                                                        output{w, mu}
```

要点：`REPL` 不再持有 `md`/`view`/散落写点；渲染仍是"纯函数产字符串 → flow 写入"。

### 3.2 output：唯一写出口

职责仅三件：持有 writer、保证写入原子性、提供测试钩子。**不含格式化、不含标签策略**。

```go
// repl/output.go
type output struct {
    mu    sync.Mutex
    w     io.Writer
    guard func() // 测试钩子：写入前调用（生产为 nil）
}

func newOutput(w io.Writer) *output
func (o *output) Write(s string)                   // 原子单写
func (o *output) Atomic(f func(w io.Writer))       // 复合临界区：一次写完标题+正文+状态行
func (o *output) SetWriter(w io.Writer)            // 注入（测试 / 未来分流）
```

约束：`Atomic` 回调内只允许使用参数 `w`，**禁止再调用 `output` 方法**（自锁）。这条要写进注释与 review 清单。

### 3.3 flow：上下文与枚举

`flow` 是 repl 内部数据流的上下文载体，按调用链传递（值语义，派生时拷贝，`out` 指针共享）：

```go
// repl/flow.go
type Phase uint8
const (
    PhaseInput Phase = iota + 1 // 输入等待/回显
    PhaseTurn                   // 对话回合
    PhaseCommand                // 斜杠命令
    PhaseReplay                 // 历史回放
)

type Kind uint8
const (
    KindContent    Kind = iota + 1 // assistant 正文（markdown 管线）
    KindReasoning                  // 思维链（当前不上屏，预留）
    KindToolBlock                  // 工具块正文（含标题重绘）
    KindToolStatus                 // 工具状态行（↳ …）
    KindNotice                     // 欢迎屏、/help、/load、提示、回放摘要
    KindError                      // 错误文案
    KindSpinner                    // 等待/思考/执行动画（原地重绘）
)

type flow struct {
    out   *output
    phase Phase
    kind  Kind
    prof  style.Profile      // 局部化，取代直读 style.GetProfile()
    width func() int
    md    *style.MarkdownBuf // 仅 PhaseTurn 使用
}

func (f *flow) emit(kind Kind, s string) // 追加写
func (f *flow) live(kind Kind, s string) // 原地重绘（spinner、picker、工具块 inline）
```

**枚举边界（防膨胀）**：

- 只服务三件事：测试断言、`emit`/`live` 两个原语的分支（清行/换行）、调试标记。
- 不做按 `Kind` 的策略表、不做过滤器注册、不导出到包外、不进 `agent.Event`。
- `Kind` 表由测试锁定：每个 `Kind` 至少一条断言用例，新增必须补用例。

`Phase` 的实际用途：测试断言 + `flow` 派生规则（例如 `PhaseReplay`/`PhaseInput` 下禁止启动 spinner）。

### 3.4 toolView：闭包 → 结构体

```go
type toolView struct {
    out       *output
    sp        *spinner
    width     func() int
    maxLines  int
    prof      style.Profile
    dirty     bool // 原 lineDirty
    justEnded bool // 原 toolJustEnded
}

func (v *toolView) Handle(e agent.Event)  // 事件入口（原闭包体）
func (v *toolView) Content(text string)   // 原 REPL.print 的语义：停 spinner、补空行、跟踪 dirty
```

`toolView` 仍是 `agent.EventSink`（`Handle` 方法即签名匹配），`agent` 侧零改动。

### 3.5 turn：回合生命周期

```go
type turn struct {
    r     *REPL
    f     *flow
    view  *toolView
    start time.Time
}

func (r *REPL) beginTurn() *turn       // md 复位、phase=Turn、dirty 复位
func (t *turn) Handle(e agent.Event)   // 接替 "streamEvent → view" 两跳
func (t *turn) End(err error)          // md 结算、中断/错误文案、turnSep（含耗时）
```

现状 `ask()` 里的 `md.Reset` / `md.Close` / 错误分支 / `turnSep`（`repl.go:203-227`）与 `streamEvent`（`repl.go:60`）合并到一处。

### 3.6 写权契约

任一时刻只有一个写者，边界点固定如下（改造后）：

| 时刻 | 唯一写者 | 边界事件 |
|---|---|---|
| `ed.Readline` 进行中 | `Editor`（提示符/回显/补全菜单/ghost） | `Readline` 调用/返回 |
| `picker`（`/load` 无参）运行中 | `picker`（raw 自绘，经 `out`） | `term.Raw()` / `term.Restore()` |
| 回合中（含 spinner 动画） | `output`（spinner 通过它写） | `beginTurn` / `turn.End` |
| interactive 工具执行中 | `bridge` → 真实 tty（子进程） | `Attach` / `stop` |
| 其余时刻 | `output` | — |

约定：`output` 不引入"暂停"概念；边界由调用时序保证（与现状一致），但**由协议断言测试守住**（见 §5.2）。

## 4. 实施细节

> 行号基于 `HEAD = 54897cb`（含已提交的止血修复）。

### 4.1 文件清单

| 动作 | 文件 | 内容 |
|---|---|---|
| 新增 | `repl/output.go` | `output` 类型（§3.2），约 40 行 |
| 新增 | `repl/flow.go` | `Phase`/`Kind`/`flow`（§3.3），约 80 行 |
| 新增 | `repl/output_test.go`、`repl/flow_test.go` | 出口与枚举断言 |
| 修改 | `repl/repl.go` | 51 处写点改走 `flow.emit`；`print`/`settleMd` 归位到 `toolView`；`ask` 改为 `runTurn` + `turn` |
| 修改 | `repl/toolview.go` | `WireToolView` 闭包 → `toolView` 结构体；`ToolWidth`/`ToolTTY` 保留 |
| 修改 | `repl/spinner.go` | 去掉外部 `mu`，改持 `*output` |
| 修改 | `repl/picker.go` | `p.render` 接收 `*output`；`pickSession` 增加 writer 注入参数 |
| 修改 | `main.go` | `WireToolView` → 新构造；`ask` 分支沿用同一个 `output` |
| 修改 | `repl/*_test.go` | 迁移 `captureStdout` 用法（见 §5） |

### 4.2 `output` 实现要点

- `Write`：`mu.Lock()` → `guard()`（若设置）→ `io.WriteString(w, s)` → `Unlock()`。
- `Atomic`：同锁范围内回调，回调内只写参数 `w`。
- `SetWriter`：加锁替换 `w`；仅用于测试注入与将来的分流开关。
- `guard` 仅测试赋值，生产恒为 `nil`（避免为测试污染热路径判断，一次 `if` 成本可忽略）。
- **不做**"清行/换行"判断：这些属于 `flow.emit`/`flow.live` 与渲染函数的知识。

### 4.3 `Kind` 与现有输出的对应

| 现有输出 | Kind |
|---|---|
| assistant 流式正文、`/history n` 的 assistant 正文 | `KindContent` |
| 工具块标题/正文（`RenderToolStart`/`RenderToolEnd(Inline)`） | `KindToolBlock` |
| 工具状态行（`RenderResponseInfo`、`↳ exit 0 · 0.3s · 12 行`） | `KindToolStatus` |
| 欢迎屏、`/help`、`/new`、`/load` 结果、`/context`、`/model` 列表、`/theme`、`/think`、`/md`、回合分隔线、`/history` 摘要行 | `KindNotice` |
| `MsgErrLineFmt` 类错误、`MsgUnknownCmd`、`MsgThemeBad` | `KindError` |
| spinner 帧、清行序列 | `KindSpinner` |
| `/history n` 的 tool 正文（Frame 清洗） | `KindToolBlock` |

回放沿用被回放记录的 `Kind`（而非统一 `KindNotice`），为将来"回放复用实时渲染"留路。

### 4.4 `toolView` 状态迁移

| 闭包捕获变量（现状） | 结构体字段（目标） |
|---|---|
| `var mu sync.Mutex` | 删除；由 `output` 内部锁取代 |
| `sp := newSpinner(&mu, tty)` | `sp *spinner`（持 `*output`） |
| `toolJustEnded bool` | `justEnded bool` |
| `lineDirty bool` | `dirty bool` |
| `width func() int` / `maxLines` | 同名字段 |
| `style.GetProfile().TTY`（每次直读） | `prof style.Profile`（构造时快照） |

行为必须逐条保持：`Content` 停 spinner → 若 `justEnded` 先补空行 → 写文本 → 更新 `dirty`；`EventResponse` 先补 `dirty` 空行再写状态行；`ToolEnd` 在 TTY 且非 interactive 时走 inline 重绘。

### 4.5 `turn` 与 `REPL` 成员调整

| 现状字段/函数 | 目标 |
|---|---|
| `REPL.md`、`REPL.mdLive` | `md` 下沉到 `flow`（仅 `PhaseTurn`）；`mdLive`（`/md` 开关）保留在 `REPL`，构造 flow 时读取 |
| `REPL.view`（`agent.EventSink` 闭包） | `REPL.view *toolView` |
| `REPL.stream`（`streamEvent`） | `turn.Handle` |
| `REPL.print`（借道 `EventContent`） | `REPL.view.Content(text)`（语义等价，去掉事件借用） |
| `ask()` | `runTurn(q)` = `beginTurn` → `agent.Ask(ctx, q, t.Handle)` → `t.End(err)` |
| `turnSink()` 的首行空行（`repl.go:107-115`） | 移入 `beginTurn`（`out.Atomic` 内补空行，避免与 spinner 交错） |

### 4.6 写点替换映射（按类别）

| 类别 | 现状（行号） | 目标 |
|---|---|---|
| 输入循环/退出/分隔线 | 162、170、181、188、193、218、220、226 | `r.f.emit(KindNotice, …)` / `KindError`；`turnSep` 仍为纯函数，写入经 flow |
| 命令反馈 | 253、256、259、263、265、272、277、280、284、287、293、303、305、310 | `emit(KindNotice)`，失败分支 `emit(KindError)`；`/model` 列表用 `out.Atomic` 保证多行不被 spinner 切入 |
| 主题/think | 317、318、325、332、336、374、387、389、394、398、400 | 同上；`printThemeSample` 的多块渲染包在一次 `Atomic` |
| 历史回放 | 407、414、422、428、430、471、473、477、486、495 | `emit` 走 `out.Atomic`（整条消息一次写完）；渲染逻辑与清洗策略**不变** |
| `/load` 菜单与结果 | 514、518、529、533、536、542 | 结果/错误经 flow；picker 自身经 `output`（§4.7） |
| 工具块/状态行/spinner | `toolview.go` 8 处、`spinner.go` 3 处 | `out.Atomic` 包整块；spinner 的帧与清行经 `out.Write` |

### 4.7 spinner / picker / Editor 接线

- `spinner`：字段 `out *spinner → *output`；`loop` 里 `s.out.Write(style.ClearLineHome()+… )`；`stop` 保持"先等 goroutine 退出、再写清行"的顺序，**不持锁等待**（沿用现状，避免死锁）。
- `picker`：`pickSession(term, list, out)`，`p.render(out, first)`；`pickByNumber` 的非 TTY 分支同样经 `out`。`REPL` 调用处传 `r.out`。
- `Editor`：`NewREPL` 里 `ed.SetOutput(r.out.w)`（或注入 writer），使输入期与输出期共用同一 writer，便于测试断言顺序；raw 期写权仍归 `Editor`（时序契约，见 §3.6）。

### 4.8 锁模型

现状：`WireToolView` 内 `var mu sync.Mutex`，手工传给 spinner；`spinner.loop` 持锁写帧；`stop()` 在锁外等待 goroutine 退出后再持锁清行。

目标：锁唯一归属 `output`。

- spinner 每帧：`out.Write(...)`（自加锁）；
- 工具块：`out.Atomic(func(w io.Writer){ 写标题; 写正文; 写状态行 })`；
- 死锁风险点：`Atomic` 回调内若调用 `out.Write` 会自锁 → 用注释 + review 约束，并在测试里加一条"回调内不得调用 output 方法"的守则性用例（可选：`Atomic` 内设置重入标志，重复进入 panic，仅在测试构建下启用）。
- 保持"停 spinner 不等锁"的顺序：`v.sp.stop()` 在 `Atomic` 之外调用。

### 4.9 main.go 接线

- `ToolWidth()` / `ToolTTY()` 保留包级函数：`main.go:43` 在 `NewREPL` 之前用它设置全局 profile（启动顺序不变）。
- `WireToolView(repl.ToolWidth, a.ToolOutputLines())` 改为构造 `output` + `toolView`，并把同一个 `output` 传给 `NewREPL`（或由 `repl.NewPresenter(...)` 返回）。
- `ask` 分支：`a.Ask(ctx, q, view.Handle)`；收尾的 `fmt.Println()` 改为经 `output`。
- `NewREPL` 增加 Option：`WithOutput(*output)`、`WithTerminal(readline.Terminal)`（供测试）。

### 4.10 回放路径（保持行为）

- `/history` 无参摘要行：`style.OneLine` + 120 rune 截断（本次已修），只改写入出口。
- `/history n`/`all`：assistant 走 markdown 管线；tool 正文走 `style.Dim.Frame`；均不改清洗策略。
- 目的：本方案不引入行为变更，只做结构收敛；"回放复用实时渲染策略"列入阶段 4（可选）。

### 4.11 明确不改的行为

- 非 TTY 下"动画关闭、状态行保留"（`docs/design.md:126`、`README.md:120`）。
- 工具块的 `Frame`/`Passthrough` 双出口与 `tool_output_lines` 头 3 尾 2 截断。
- interactive 工具不启 spinner、结束追加式渲染。
- 模型通道（history/jsonl/请求）零变换。

## 5. 测试方案

### 5.1 分层矩阵

| 层 | 覆盖对象 | 手段 | 自动化程度 |
|---|---|---|---|
| 纯函数 | `Render*`、`Frame`/`Passthrough`/`OneLine` | 现有单测 | 完全 |
| 结构体 | `output`/`flow`/`toolView`/`turn` | 注入 `*bytes.Buffer` 作为 writer | 完全 |
| 并发互斥 | 工具块 vs spinner 帧 | `-race` + 原子性压测 | 完全 |
| 写权协议 | 输入期/output 期互斥 | `guard` 钩子 + `fakeTerm` 标志 | 完全（协议层） |
| 写权字节序 | Editor 与 output 共写一个 writer | 共享 buffer + 段落顺序断言 | 完全（顺序层） |
| 真实终端 | 光标/覆盖/termios 时序 | `script` pty + 人工目视 | **半自动（上限）** |

**明确结论**：写权冲突可测到"协议正确 + 并发正确 + 字节序正确"；真实屏幕效果无法在 buffer 中断言，必须保留 pty + 人工验收。这是本方案的测试上限，不引入终端模拟器。

### 5.2 写权冲突的三种手段

1. **协议断言（必做）**
   - `output.guard` 在每次写入前回调；测试里设 `inInput` 标志（`fakeTerm.ReadKey` 进入时置位、返回键值后清除）。
   - 跑一轮完整 `Run`（喂 `/help`、`/exit`），断言"输入期发生 output 写入"即失败。
   - 覆盖点：`Readline` 全程、picker 的 `Raw/Restore` 区间、`turn.End` 之后到下一次 `Readline` 之间的写入。
2. **字节序断言（必做）**
   - `Editor.SetOutput(buf)` 与 `output.SetWriter(buf)` 指向同一 `*bytes.Buffer`。
   - 断言段落顺序：`提示符 → 回显 → 输出块`，并断言输出块前后存在预期的 `\r\n`/清行序列；可捕捉"输出插进输入行中间""回显与状态行粘连"。
3. **pty 集成（保留人工）**
   - 复用现有门控风格，新增 `REPL_TTY_E2E=1` 时执行 `script -qec` 用例；默认 skip。
   - 人工目视清单见 §5.5。

### 5.3 测试地基（阶段 0 必须先落地）

| 项 | 内容 |
|---|---|
| 注入 Option | `NewREPL` 支持 `WithOutput(*output)`、`WithTerminal(readline.Terminal)`（或包内 `newTestREPL`） |
| repl 侧 fakeTerm | 实现 `readline.Terminal`（`Raw/Restore/ReadKey/Size`），按键序列驱动 `Run` |
| profile 局部化 | `flow.prof` 字段化，测试构造 `style.Profile{TTY: false, Colors: style.LevelNone}`，不再依赖全局默认 |
| guard 钩子 | `output.guard func()`，生产为 `nil` |
| 原子性标记 | 并发用例里给每次 `Atomic` 写入包上唯一首尾标记，便于断言不交错 |

### 5.4 并发与竞态

- `spinner.start(spinRunning)` 后并发调用 `toolView.Handle(EventToolEnd)` 1000 次，断言每次工具块的首尾标记成对出现且不被帧插入。
- 验证 `spinner.stop()` 超时分支（200ms）不会持锁等待：用阻塞 writer 构造慢写，断言不出现死锁（测试超时保护）。
- 全量 `go test -race ./...` 必须保持通过。

### 5.5 pty 验收与门控

```bash
make build                                   # 不要用 go run（^Z/^C 语义不同）

# 1) 工具块 inline 重绘（⋯ 被替换为最终标题）
printf '跑一条命令\n/exit\n' | script -qec "./tanyan" /dev/null | cat -v | head -40

# 2) 输入期与输出期不交错（观察提示符行不被 output 覆盖）
script -qec "./tanyan" /dev/null

# 3) 补全菜单 / ghost / picker
#    （键入 / 与 /load 后按 Tab、/load 回车）
```

人工检查清单：

1. 提示符与回显行在 spinner 动画期间不被擦除；
2. 工具块标题的 `⋯` 原位重绘正确，块与块之间无空行错位；
3. `/load` picker 上下移动无残影，Esc 返回后提示符正常；
4. `Ctrl+C` 中断回合后终端回显、光标、颜色状态正常（SGR 无泄漏）；
5. 退出后 `stty -a` 与进入前一致（termios 已恢复）。

### 5.6 每阶段测试清单

| 阶段 | 新增/迁移用例 |
|---|---|
| 0 | `TestOutputInjectWriter`、`TestFakeTermDrivesRun`、`TestGuardNoWriteDuringInput`（骨架） |
| 1 | `TestOutputAtomicNoInterleave`、`TestToolBlockSingleWrite`、`TestSpinnerFramesViaOutput`、`TestPickerUsesOutput`、`TestNoticeErrorKinds`、迁移 `captureStdout` 用例 |
| 2 | `TestFlowPhaseTurnCommandReplay`、`TestTurnEndSettlesMarkdown`、`TestTurnInterruptAndErrorPaths`、`TestHistoryReplayViaOutput` |
| 3 | `TestToolViewStateFields`、`TestToolViewContentSemantics`（迁移自 `toolview_test.go`） |
| 4（可选） | `TestReplayReusesLiveRendering` |

## 6. 分阶段实施与验收

| 阶段 | 内容 | 验收标准 | 回滚点 |
|---|---|---|---|
| 0 测试地基 | 注入 Option、repl fakeTerm、`output.guard`、pty 脚本模板 | 现有测试全绿；fakeTerm 能驱动一轮 `Run` 到退出 | 独立提交，可直接 revert |
| 1 输出收敛 | `repl/output.go`；替换 70 处写点；spinner/picker/Editor 接线 | §5.6 阶段 1 用例 + `-race` + pty 目视 1/2/5 | 独立提交 |
| 2 flow ctx + 枚举 | `repl/flow.go`；`REPL` 字段下沉；`turn` 生命周期 | §5.6 阶段 2 用例；行为与阶段 1 一致（对比 pty 输出） | 独立提交 |
| 3 toolView 结构体化 | 闭包 → 结构体；状态成字段 | `toolview_test` 全量迁移通过；pty 目视 2/3 | 独立提交 |
| 4（可选）回放归一 | `/history` 复用实时渲染策略 | 回放帧断言 + 目视 | 独立提交 |

阶段 1 完成后即使后续不做，也已获得"单点输出 + 可注入测试"的全部收益。

## 7. 风险与对策

| 风险 | 影响 | 对策 |
|---|---|---|
| 终端时序回归 | 工具块重绘、spinner 交错出错 | 行为保持不变的逐点替换；每阶段跑 pty 目视清单；保留 `\x1b[1A\r\x1b[K` 的生成位置（渲染函数） |
| `Atomic` 重入 | 自锁死 | 注释 + review；可选测试构建下的重入 panic 守卫 |
| readline 写权边界 | raw 期两方同时写导致屏幕错乱 | 契约固化（§3.6）+ 协议断言测试；`Editor.SetOutput` 与 `output.SetWriter` 指向同一 writer |
| picker 特例 | raw 下自绘与 output 冲突 | picker 经 `output`；`Raw/Restore` 区间禁止其他写者（测试断言） |
| 测试迁移面 | `captureStdout` 十余处需改 | 阶段 0 先提供注入能力，阶段 1 集中迁移 |
| `style.Profile` 全局残留 | 测试间串扰（`-race` 下更明显） | `flow.prof` 字段化；保留全局仅用于 `main` 启动期设置 |
| main 启动顺序 | `ToolTTY` 在 `NewREPL` 前被调用 | 保留 `ToolTTY`/`ToolWidth` 包级函数，不并入 `REPL` |
| 隐含行为变更 | 非 TTY 状态行、截断策略被"顺手改掉" | §4.11 列为不改项，测试与文档双锁 |

## 8. 非目标与防膨胀约束

- 不引入 `Collector`/`Presenter`/标签策略表/过滤器注册表（已评估后废弃）。
- `Kind`/`Phase` 不导出包外、不进 `agent.Event`、不做策略分发（§3.3 边界）。
- `output` 不含格式化逻辑；渲染仍是 `style` 纯函数 + `repl` 组装函数。
- 不为测试在生产路径加入分支（`guard` 为 `nil` 即无行为）。
- 不新增第三方依赖；总新增代码控制在三个类型 + 两个文件内。

## 附录 A 现状写点清单（供替换核对）

`repl/repl.go`（51 处）：

| 区间 | 行号 | 类别 |
|---|---|---|
| 输入循环/回合 | 112、162、170、181、188、193、218、220、226 | 欢迎屏、退出、分隔线、中断/错误文案 |
| 命令反馈 | 253、256、259、263、265、272、277、280、284、287、293、303、305、310 | `/help` `/new` `/load` `/context` `/model` `/md` 与未知命令 |
| 主题 | 317、318、325、332、336、374 | `/theme` 与样本渲染 |
| think | 387、389、394、398、400 | `/think` |
| 历史回放 | 407、414、422、428、430、471、473、477、486、495 | `/history` 摘要、单条、tool 正文、消息头 |
| `/load` | 514、518、529、533、536、542 | 菜单降级、结果、错误、legacy 提示 |

其余：`repl/toolview.go` 8 处（闭包内）、`repl/picker.go` 8 处、`repl/spinner.go` 3 处、`readline/editor.go` 11 处（仅接线 `SetOutput`，不改内部）、`main.go` 8 处。

## 附录 B 代码锚点与相关文档

- 事件词汇表：`agent/event.go`
- 事件发射点：`agent/agent.go:243,261,274,276`；`agent/llm.go:251,258,265,283`；`agent/llm_responses.go:239,248,259,268`
- 渲染纯函数：`repl/toolview.go:35-245`；过滤器：`style/filter.go`
- 闭包现状：`repl/toolview.go:247-306`；spinner 锁现状：`repl/spinner.go:38-107`
- TTY/宽度探测：`readline/terminal_unix.go:17-23,50-56`；`repl/toolview.go:307-323`；`main.go:43`
- 相关文档：`docs/design.md`《工具视图渲染》《测试》；`docs/render-pipeline.md`《ANSI 过滤器》；`docs/interactive-tty.md`（bridge 与写权边界）；`README.md`（TTY 行为契约）
