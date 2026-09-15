# repl 输出收敛与数据流封装方案

> 状态：方案定稿（含输出模式与双流收敛），分阶段实施（阶段 0-5，见 §6）
> 进度：阶段 0-4 已实施；阶段 5 拆出独立评估（`docs/repl-replay-rendering.md`，结论：建议不做或降级）。§3.4/§3.5/§3.7 已按实际形态改写
> 阅读提示：§0–§5 是**定稿当时的方案记录**，其中行号与部分函数名（`WireToolView`/`lineDirty`/`toolJustEnded` 等）是实施前快照，阶段 1-4 已改名或下沉，不作为定位依据；实施结果见 §6 末与附录 C
> 范围：`repl` 包内重构 + `main.go` 接线
> 不动的部分：`agent`（协议/事件/history/落盘）、`style` 渲染纯函数与过滤器、`readline` 内部实现

## 0. 决策记录

三轮评估的结论固化于此，后续章节按此展开。标 ⚠ 的是相对初稿的**方向性修正**。

| 议题 | 结论 | 理由 |
|---|---|---|
| `Kind` 的定位 | **扶正**：噪音屏蔽的把手，服务于"动态调整输出模式"（`ask` 以 plain 输出，供本 agent 作为子 agent 被调用） | `Profile` 只能挡住颜色/markdown/spinner/光标控制，工具块与状态行挡不住（§1.3.1 实测） |
| `Phase` | ⚠ **删除** | 新需求完全由 `Kind`+模式表达；回放路径不发 `EventRequestStart`，天然不启 spinner，"禁 spinner"没有失败模式 |
| 噪音屏蔽落点 | ⚠ 落在 `output`（不是 `flow`） | `ask` 路径根本不经过 `flow`（`main.go:67,76` 直接 `WireToolView` → `a.Ask`） |
| `emit`/`live` 位置 | ⚠ 从 `flow` 下沉到 `output`；`flow` 瘦身为上下文携带者 | 与上一条同因：唯一过滤点必须被两条路径共享 |
| `output` 接口 | ⚠ 实现 `io.Writer`，`emit`/`live`/`allows`/`atomic` 为带门禁入口 | 原设计 `Write(s string)` 与 `fmt.Fprintf(picker, …)`、`Editor.SetOutput(io.Writer)` 不兼容 |
| 双流 | ⚠ 新增 `streams{out, err}`，同一类型两实例，各自独立互斥区 | stderr 同样需要收敛：抽象一致 + 便于后续拓展（日志分流、结构化输出） |
| `KindNotice` | ⚠ 拆出 `KindDecor`（欢迎屏、回合分隔线） | 一张一维可见性表要同时满足"ask 纯答案"与"交互 `/help` 不失联" |
| 结构性空行 | ⚠ 归属所属块（`ToolBlock`/`Decor` 自带），不单独成写点 | 否则屏蔽工具块后留下孤立空行、verbose 下块与块粘连 |
| plain 语义 | `out.vis = {Content, Notice}`；`verbose` 追加 `{ToolBlock, ToolStatus}` | `ask` 路径不发 `Notice`，故一维表即可让 ask 输出纯答案 |
| 工具块在 plain 下 | **全部屏蔽**；`--verbose` 恢复纯文本形态（仍无 ANSI/spinner/光标控制） | 父代理要的是答案，需要执行痕迹时显式开 verbose |
| 模式暴露 | **仅 CLI flag**：`-p`/`--plain`、`--verbose`；无 env、无 config | 与 `-n`/`--no-save` 同构，子代理由调用方控 argv |
| `--verbose` 无 `--plain` | 报错退出，不静默忽略 | 静默忽略会让调用方误以为生效 |
| stderr 是否参与 plain 屏蔽 | **不参与**（`err.vis` 恒为全开） | 诊断不应被静默；机制保留以便后续需要 |
| 中断提示（`MsgInterruptKept`/`MsgInterruptBare`） | ⚠ **从 stdout 迁到 stderr**（唯一路由变更） | 本质是诊断；两 fd 均无缓冲，同 tty 下写序即调用序，交互观感不变 |
| 错误改走 stdout | ⚠ **不做**（撤回初稿建议） | 子代理场景最优契约是 stdout=答案、stderr=错误 |
| 回放"沿用被回放记录的 `Kind`" | ⚠ **不可实现，改为按 role/内容推断** | `agent.Message`（`agent/llm.go:21-30`）没有 `Kind` 字段，历史里也没有 `ShellResult` |

## 1. 背景

### 1.1 触发事件

会话 `20260911-165625` 用 `/history` 查看时出现三种现象：中间开始整段变黄、`pong` 与状态行插进列表中间、行内容互相覆盖。根因不在渲染算法，而在**输出没有单一出口、没有写权契约、没有统一清洗约定**：

- `/history` 摘要行（`historyLine`）绕过 `style` 管线直写 `fmt.Println`，原始 `\r`、`\x1b[K`、`\x1b[33m` 直接进终端：`\r\x1b[K` 触发回车清行，把已打印的摘要行覆盖成 `pong` + 状态行；`truncateRunes` 按 120 rune 硬截断又切断了 `\x1b[33m` 的闭合序列，导致后续输出整段继承黄色。
- 这些字节来自当时那条 `run_shell` 命令的产物——命令本身是在跑 tanyan（E2E），子 tanyan 的 UI 写到了 stdout，被父进程原样捕获成 tool 结果。

**这条事故链本身就是本方案的第二个动机**：父进程捕获子 tanyan 的 UI 噪音。`§1.2` 的 `OneLine` 是**读侧止血**（父进程读历史时清洗），plain 模式是**写侧根治**（子 tanyan 不再产生噪音）。两者互补，都不能省。

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

**stderr 写点 7 处**（本次纳入收敛）：`main.go` 38、64、72、79、88、93 与 `repl/repl.go:223`。`agent` 包不写 stdout/stderr；`readline/secure.go`、`agent/shell_tty_unix.go` 写的是 `/dev/tty`（fd 层面，属桥接那一行）。测试侧 `captureStderr`（`repl/dispatch_test.go:67`）**定义后从未调用**——stderr 路径目前零覆盖。

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

### 1.3.1 最安静配置下仍剩的噪音（实测）

把现有代码置于 `Profile{TTY:false, Colors:LevelNone}` 下喂同一串事件（RequestStart → Content → ToolStart → ToolEnd → Response → Content）：

```
TTY=false, Colors=None:
"答案第一段。\n\n▸ run_shell ls -la ⋯\n\n▸ run_shell ls -la\n  a\n  b\n  c\n
  ↳ exit 0 · 0ms · 3 行\n  ↳ TTFT 0ms · 0ms\n\n答案第二段。\n"

TTY=true, Colors=None:
"\r\x1b[K⠋ 等待响应 0s\r\x1b[K答案第一段。\n\n▸ run_shell ls -la ⋯\n
\r\x1b[K  ⠋ 执行中 0s\r\x1b[K\x1b[1A\r\x1b[K▸ run_shell ls -la\n…"
```

| 噪音 | 现状挡得住吗 | 挡住它的是 |
|---|---|---|
| ANSI 颜色、markdown 装饰 | ✅ | `Colors=None` |
| spinner 帧、`\x1b[1A\r\x1b[K` 重绘 | ✅（**仅非 TTY**） | `Profile.TTY` |
| 工具标题 `▸ … ⋯`、工具正文块 | ❌ 照发 | 只有 `Kind` 能挡 |
| 工具状态行 `↳ exit 0 · 3 行` | ❌ 照发 | 只有 `Kind` 能挡 |
| 响应状态行 `↳ TTFT …` | ❌ 照发（无任何 TTY 门槛） | 只有 `Kind` 能挡 |
| 结构性补空行 | ❌ 照发 | 只有 `Kind` 能挡 |

结论：`colors: off` ≠ plain，二者正交（`colors` 管颜色，plain 管类别）；`Profile` 挡不住的部分正是 `Kind` 的职责范围。

### 1.4 为什么现在改

1. 本次修复是"点状补漏"，同类问题（清洗漏一处、写点漏一处、回放与实时分叉）还会再发生；
2. 测试无法注入输出，只能靠替换 `os.Stdout`/`os.Stderr`（`captureStdout` 25 处调用，`captureStderr` 死代码）与全局 profile，覆盖面受限；
3. 子代理调用场景（tanyan 作为子 agent）需要 stdout 可解析，现状无法提供；
4. 后续任何输出侧需求（回放复用实时渲染、日志分流、结构化输出）都需要一个明确的落点，而不是继续加 `if`。

## 2. 目标与非目标

**目标**

1. **输出收敛**：repl 内所有输出走唯一出口（`streams.out` / `streams.err`），writer 可注入、互斥统一。
2. **数据流封装**：引入 `flow` 上下文、`Kind` 枚举与 `turn` 生命周期，`REPL` 只保留装配与分发。
3. **可测试性**：测试可注入 writer 与 fake 终端，不再替换 `os.Stdout`/`os.Stderr`；写权冲突以协议断言覆盖。
4. **输出模式**：以 `Kind` 为把手做噪音屏蔽，提供 `plain` / `plain+verbose` 两档，使 `ask` 可产出纯答案（stdout=答案、stderr=错误），支撑 tanyan 作为子 agent 被调用。

**非目标（明确不做）**

- 不做 Collector / Presenter / 标签策略表 / **通用过滤器注册表**。允许的只有一张静态的 `Kind` 可见性表（三档，写死在 `streams` 内），不设注册、不设回调、不设外部扩展点。
- 不改 `agent` 的事件协议、history、落盘与请求构造（模型通道零变换红线）。
- 不改 `style` 的渲染纯函数与过滤器（`Render*`、`Frame`、`Passthrough`、`OneLine`）。
- 不改 `readline` 内部实现，只用其现成的 `SetOutput`。
- 不做结构化输出（JSONL）与日志分流：本方案只把落点铺好（`streams` 可加第三个 `output`），不在本次实现。
- 不改动"非 TTY 保留状态行"的既有契约（`docs/design.md:126`、`README.md:120`）——plain 是显式 opt-in 的第三条路，不覆盖它。

## 3. 设计方向

### 3.1 目标态数据流

```
Run
 ├─ 输入期        ed.Readline(prompt)            ← 输入期唯一写者：Editor（走 streams.out 的裸 Write）
 ├─ dispatch(line) → intent(斜杠/退出/对话)
 │    ├─ 斜杠命令  ─┐
 │    ├─ 回合 r.ask(text)                            ├─► flow{st, prof, live, md, rend}
 │    └─ 回放 /history                                │        │
 │                                                    │        ├─ st.out.emit(kind, text) / live(...)
 │                                                    │        └─ st.err.emit(kind, text)
 └─ 回合结束 turn.End() → md 结算 / 错误文案 / 回合分隔线
                                                            ▼
                                                   streams{out, err}
                                                     out: {w, mu, vis}
                                                     err: {w, mu, vis}
```

要点：`REPL` 不再持有 `md`/`view`/散落写点；渲染仍是"纯函数产字符串 → `emit` 写入"；**门禁（可见性表）只在 `output` 里查**。

`ask` 子命令路径不经 `flow`/`turn`，直接 `a.Ask(ctx, q, view.Handle)`，其噪音屏蔽由同一个 `streams.out` 承接——这正是门禁必须落在 `output` 的原因。

### 3.2 output 与 streams：唯一写出口

职责三件：持有 writer、保证写入原子性、按 `Kind` 过滤。**不含格式化、不含标签策略表之外的分发逻辑。**

```go
// repl/streams.go
type output struct {
    mu    sync.Mutex
    w     io.Writer
    vis   visSet   // 可见 Kind 集合（每流独立）
    guard func()   // 测试钩子：仅 emit/live/atomic 触发，生产为 nil
}

func (o *output) Write(p []byte) (int, error) // io.Writer：无门禁、不触发 guard，raw 期自绘通道
func (o *output) emit(kind Kind, s string)    // 查 vis；空串直接返回
func (o *output) live(kind Kind, s string)    // plain 下降级为 emit（禁光标控制）
func (o *output) allows(kind Kind) bool       // spinner/picker 等"启动前查询"
func (o *output) atomic(kind Kind, f func(w io.Writer))
func (o *output) setWriter(w io.Writer)       // 测试注入 / 将来分流

type streams struct {
    out *output // stdout：答案、正文、列表、命令反馈
    err *output // stderr：错误与诊断
}

func newStreams(stdout, stderr io.Writer, mode outMode) *streams
```

约束（写进注释与 review 清单）：

- `atomic` 回调内只允许使用参数 `w`，**禁止再调用 `output` 方法**（自锁）。
- `Write` 是"无门禁通道"，仅供 raw 期自绘（Editor/picker）使用——它仍持锁，但不查 `vis`、不触发 `guard`。
- 两流是**独立互斥区**，禁止跨流嵌套加锁（无死锁可能；两 fd 的写序不做保证，与现状一致）。
- `Write` 实现 `io.Writer` 是刻意选择：`Editor.SetOutput`、`picker`、`spinner` 都收/写 `io.Writer`，统一后无需各自开洞。

### 3.3 Kind 枚举与 Phase 的删除

```go
// repl/flow.go
type Kind uint8

const (
    KindContent    Kind = iota + 1 // assistant 正文（流式 + 回放）
    KindReasoning                  // 思维链（当前不上屏，预留）
    KindToolBlock                  // 工具标题/正文块（含其结构性空行）
    KindToolStatus                 // 工具状态行 + 响应状态行（↳ …）
    KindNotice                     // 信息性文案：/help、/model 列表、回放、命令反馈
    KindDecor                      // 纯装饰：欢迎屏、回合分隔线
    KindError                      // 错误、中断提示
    KindSpinner                    // 动画帧与清行
)
```

**边界（防膨胀）**：只服务三件事——噪音门禁、`emit`/`live` 两个原语的分支、测试断言。不做按 `Kind` 的策略表（除三档可见集，§3.4）、不做过滤器注册、不导出包外、不进 `agent.Event`。

**`Phase` 删除**。初稿的四个 phase 中，唯一有实际用途的是"`Replay`/`Input` 下禁 spinner"，而 spinner 只在 `EventRequestStart`/`EventToolStart` 启动（`toolview.go:254,289`），回放路径不发这些事件，"禁 spinner"没有失败模式；`Input` 期的写权由时序契约（§3.8）与 `guard` 断言守住，不需要枚举。测试断言按"事件流 + 输出字节"表达即可。

**结构性空行的归属**（初稿漏项，最容易漏的一处）：三处空行不单独成写点，随所属块进出——

| 空行 | 现状位置 | 归属 |
|---|---|---|
| `turnSink` 首行补空行 | `repl.go:112` | `KindDecor`（由 `beginTurn` 写） |
| `toolJustEnded` 补空行 | `toolview.go:265` | `KindToolBlock`（工具块渲染自带，且屏蔽时不置 `justEnded`） |
| 工具块/分隔线自带的 `\n` | `RenderToolStart`/`turnSep` | 随各自 Kind |

### 3.4 输出模式：rich / plain / plain+verbose

```go
type outMode uint8

const (
    modeRich         outMode = iota // 默认：全开
    modePlain                       // -p/--plain
    modePlainVerbose                // -p --verbose
)
```

| 模式 | `out.vis` | `err.vis` | 其他 |
|---|---|---|---|
| rich（默认） | 全开 | 全开 | 行为与今天逐字节一致 |
| plain | `{Content, Notice}` | 全开 | `Colors=None`、无 spinner、无光标控制、关 markdown |
| plain+verbose | `{Content, Notice, ToolBlock, ToolStatus}` | 全开 | 同上，工具块以纯文本追加式渲染（不得用 `RenderToolEndInline`） |

设计要点：

- **plain 保留 `Notice` 而不是只留 `Content`**：`ask` 路径根本不发 `Notice`（无欢迎屏、无分隔线、不走 `/help`），所以 ask 的 stdout 天然纯答案；而交互 plain 下 `/help` 仍有输出，不会"命令失联"。一维可见集即可覆盖两个场景，无需 `(mode, kind)` 二维表。
- **plain 六条语义**：① `Colors=LevelNone`；② 无 spinner；③ 无光标控制（`live` 降级为 `emit`）；④ 关 markdown（`mdEnabled = mdLive && prof.TTY && mode.decor`）；⑤ 屏蔽 `ToolBlock`/`ToolStatus`/`Spinner`/`Decor`；⑥ stdout 只承载答案与命令反馈。
- **`--verbose` 仅与 `--plain` 同用**，否则报错退出（`错误: --verbose 需与 --plain 同用`）。
- **暴露面**：仅 CLI flag（`-p`/`--plain`、`--verbose`），**无 env、无 config**——与 `-n`/`--no-save` 同构；`Kind` 与模式类型不出 `repl` 包，`main` 只传两个 bool。
- **与 `colors: off` 正交**（§1.3.1 实测）：`colors` 管颜色，plain 管类别；`colors: off ≠ plain`。
- stderr **不参与屏蔽**（`err.vis` 恒为全开）：诊断不应被静默。机制保留，将来需要时改一行。

### 3.5 toolView：闭包 → 结构体

```go
type toolView struct {
    st        *streams
    sp        *spinner
    prof      style.Profile
    width     func() int
    maxLines  int
    justEnded bool // 原 toolJustEnded
    dirty     bool // 原 lineDirty
}

func NewToolView(st *streams, prof style.Profile, width func() int, maxLines int) *toolView
func (v *toolView) Handle(e agent.Event)           // 事件入口（原闭包体）
func (v *toolView) Content(kind Kind, text string) // 原 REPL.print 的语义：停动画、补空行、跟踪 dirty
```

`agent.EventSink` 是**函数类型**（`type EventSink func(Event)`），结构体不能"直接满足"它：调用处传方法值（`main.go` 的 `a.Ask(ctx, q, sink.Handle)`），`turn.Handle` 本身即 `func(Event)` 可直接作 sink。`agent` 侧零改动。

`Content` 在初稿里只收 `text`，阶段 3 落地时加 `kind`：回放的消息头是 `KindNotice`（§4.3）而正文是 `KindContent`，仅靠借用 `EventContent` 无法区分。

### 3.6 turn：回合生命周期

```go
type turn struct {
    r     *REPL
    f     *flow
    view  *toolView
    start time.Time
}

func (r *REPL) beginTurn(done func()) *turn // 派生 flow（prof/live/rend 快照 + 新 md 缓冲）
func (t *turn) Handle(e agent.Event) // 接替 "streamEvent → view" 两跳
func (t *turn) Handle(e agent.Event) // 首个事件前懒补 Decor 空行，再分发
func (t *turn) End(err error)        // md 结算、done()、中断/错误文案（走 stderr）、turnSep
```

现状 `ask()` 里的 `md.Reset` / `md.Close` / 错误分支 / `turnSep`（`repl.go:203-227`）与 `streamEvent`（`repl.go:60`）合并到一处。

### 3.7 两流契约：stdout = 正文/反馈，stderr = 诊断

| 通道 | 承载 |
|---|---|
| stdout（`streams.out`） | assistant 正文、工具块与状态行、命令反馈、回放、欢迎屏、回合分隔线、spinner 帧、输入期回显（裸 `Write`） |
| stderr（`streams.err`） | 错误文案（`MsgErrLineFmt` 类）、中断提示（`MsgInterruptKept`/`MsgInterruptBare`） |

**路由清单**（rich 模式字节不变，只换通道）：

| 位置 | 收敛前 | 阶段 1 落地 |
|---|---|---|
| `main.go:38`（LoadConfig 失败） | `os.Stderr` | `streams.Fail` → `err.emit(KindError, …)` |
| `main.go:64`（agent.New 失败） | `os.Stderr` | 同上 |
| `main.go:72`（ask 用法） | `os.Stderr` | 同上 |
| `main.go:79`（ask 错误收尾） | `os.Stderr` | 同上；**前导 `\n` 保留**（rich 下给"stdout 已输出半行"补空行，子代理多看一个空行无害） |
| `main.go:88`/`93`（REPL 构造/Run 失败） | `os.Stderr` | 同上 |
| `main.go:29`（`-v` 版本） | stdout `Printf` | `streams.Print`（stdout） |
| `main.go:82`（ask 收尾换行） | stdout `Println` | `streams.Content("\n")`（stdout） |
| `repl.go:223`（回合错误） | `os.Stderr` | `err.emit(KindError, …)` |
| `repl.go:218/220`（中断提示） | stdout | ⚠ `err.emit(KindError, …)`（迁 stderr） |

**命令失败一并迁入 stderr**（初稿只列了上表，实施时按"stdout=正文/反馈、stderr=错误/诊断"统一处理，共 8 处；同一 tty 下写序不变，pty 输出逐字节一致）：

| 位置 | 文案 | 收敛前 | 阶段 1 落地 |
|---|---|---|---|
| `repl.go:263` | `/load <id>` 失败 `MsgErrLineFmt` | stdout | stderr `KindError` |
| `repl.go:280` | `/model` 列表失败 `MsgModelsFail` | stdout | stderr `KindError` |
| `repl.go:310` | 未知命令 `MsgUnknownCmd`（白名单已过滤，实际不可达） | stdout | stderr `KindError` |
| `repl.go:332` | `/theme` 非法 `MsgThemeBad` | stdout | stderr `KindError` |
| `repl.go:394` | `/think` 非法 `MsgErrLineFmt` | stdout | stderr `KindError` |
| `repl.go:422` | `/history n` 索引非法 `MsgInvalidIndex` | stdout | stderr `KindError` |
| `repl.go:514` | `/load` 列会话失败 `MsgErrLineFmt` | stdout | stderr `KindError` |
| `repl.go:533` | `/load` 载入失败 `MsgErrLineFmt` | stdout | stderr `KindError` |

判据统一为：**"用户请求未完成/失败"→ stderr（`KindError`）；状态展示与用法提示（`MsgNoSessions`/`MsgCancelled`/`MsgModelsEmpty`/`MsgNoHistoryMsg`/`MsgDialogueEmpty`）→ stdout（`KindNotice`）**。

`agent` 包内的错误经由返回值上浮，不在包内写流。

### 3.8 写权契约

任一时刻只有一个写者，边界点固定如下（改造后）：

| 时刻 | 唯一写者 | 通道 / 边界事件 |
|---|---|---|
| `ed.Readline` 进行中 | `Editor`（提示符/回显/补全菜单/ghost） | `streams.out.Write`（裸写，无门禁、不触发 guard） |
| picker（`/load` 无参）运行中 | picker（raw 自绘） | 同上（`term.Raw()` / `term.Restore()` 为边界） |
| 回合中（含 spinner 动画） | `output.emit` / `output.live` | `beginTurn` / `turn.End` |
| 错误与诊断 | `output.emit` | `streams.err`（独立互斥区） |
| interactive 工具执行中 | `bridge` → 真实 tty（子进程 fd） | `Attach` / `stop` |
| 其余时刻 | `output.emit` | — |

约定：`output` 不引入"暂停"概念；边界由调用时序保证（与现状一致），但**由协议断言测试守住**（§5.2）。`guard` 只在 `emit`/`live`/`atomic` 触发，因此"输入期不得有 `emit`"的断言不会被 Editor 的合法回显误报。

## 4. 实施细节

> 行号基于 `HEAD = 54897cb`（含已提交的止血修复）。

### 4.1 文件清单

| 动作 | 文件 | 内容 |
|---|---|---|
| 新增 | `repl/streams.go` | `output` + `streams` + `visSet` + `outMode` 可见集（§3.2/§3.4），约 90 行 |
| 新增 | `repl/flow.go` | `Kind` + `flow`（§3.3/§3.6），约 60 行 |
| 新增 | `repl/streams_test.go`、`repl/flow_test.go` | 出口、门禁、枚举断言 |
| 修改 | `repl/repl.go` | 51 处写点改走 `flow`/`streams`；`print`/`settleMd` 归位到 `toolView`；`ask` 改为 `runTurn` + `turn`；中断提示迁 stderr |
| 修改 | `repl/toolview.go` | `WireToolView` 闭包 → `toolView` 结构体；writer 来源改 `*streams`；`ToolWidth`/`ToolTTY` 保留 |
| 修改 | `repl/spinner.go` | 去掉外部 `mu`，改持 `*output`；启动条件加模式门禁 |
| 修改 | `repl/picker.go` | `pickSession(term, list, out)`；`p.render(out, first)` 收 `io.Writer` |
| 修改 | `main.go` | 新增 `-p/--plain`、`--verbose`；先建 `streams` 再 `LoadConfig`；`ask` 分支与 REPL 共用同一 `streams` |
| 修改 | `repl/*_test.go` | 迁移 `captureStdout`（25 处）；`captureStderr` 改为注入后启用或删除 |

### 4.2 output / streams 实现要点

- `Write`：`mu.Lock()` → `io.Writer.Write` → `Unlock()`（无门禁、不触发 `guard`）。
- `emit`：`if !o.allows(kind) { return }`；空串直接返回；`mu.Lock()` → `guard()`（若设置）→ `io.WriteString(w, s)` → `Unlock()`。
- `live`：模式为 plain 时直接转 `emit`（禁光标控制）；否则同 `emit`。
- `atomic(kind, f)`：同锁范围内回调，回调内只写参数 `w`。
- `guard` 仅测试赋值，生产恒为 `nil`。
- `visSet` 为位集，三档在 `newStreams` 里写死；**不做**外部注册。
- **不做**"清行/换行"判断：这些属于 `emit`/`live` 调用点与渲染函数的知识。

### 4.3 Kind 与现有输出的对应

| 现有输出 | Kind |
|---|---|
| assistant 流式正文、`/history n` 的 assistant 正文 | `KindContent` |
| 工具块标题/正文（`RenderToolStart`/`RenderToolEnd(Inline)`） | `KindToolBlock` |
| 工具状态行（`RenderResponseInfo`、`↳ exit 0 · 0.3s · 12 行`） | `KindToolStatus` |
| `/help`、`/new`、`/load` 结果、`/stat`、`/model` 列表、`/theme`、`/think`、`/md`、`/history` 摘要行与非 assistant 正文 | `KindNotice` |
| 欢迎屏、回合分隔线、`turnSink` 首行空行 | `KindDecor` |
| `MsgErrLineFmt` 类错误、`MsgUnknownCmd`、`MsgThemeBad`、中断提示 | `KindError` |
| spinner 帧、清行序列 | `KindSpinner` |

**回放路径的 Kind 是推断出来的，不是"沿用被回放记录的 Kind"**（`agent.Message` 无该字段）：assistant 正文 → `KindContent`；`Role == "tool"` 或含 `ToolCalls` → `KindToolBlock`；其余 → `KindNotice`。

### 4.4 toolView 状态迁移

| 闭包捕获变量（现状） | 结构体字段（目标） |
|---|---|
| `var mu sync.Mutex` | 删除；由 `output` 内部锁取代 |
| `sp := newSpinner(&mu, tty)` | `sp *spinner`（持 `*output`，启动前查 `allows(KindSpinner)`） |
| `toolJustEnded bool` | `justEnded bool`（屏蔽工具块时**不置位**，避免孤立空行） |
| `lineDirty bool` | `dirty bool` |
| `width func() int` / `maxLines` | 同名字段 |
| `style.GetProfile().TTY`（每次直读） | `prof style.Profile`（构造时快照） |

行为必须逐条保持：`Content` 停 spinner → 若 `justEnded` 先补空行 → 写文本 → 更新 `dirty`；`EventResponse` 先补 `dirty` 空行再写状态行（`KindToolStatus`）；`ToolEnd` 在 TTY 且非 interactive 时走 inline 重绘（plain 下强制 `RenderToolEnd`）。

### 4.5 turn 与 REPL 成员调整

| 现状字段/函数 | 目标 |
|---|---|
| `REPL.md`、`REPL.mdLive` | `md` 下沉到 `flow`；`mdLive`（`/md` 开关）保留在 `REPL`，构造 flow 时读取 |
| `REPL.rend` | ⚠ 初稿漏项：同时受 `style.GetProfile()` 影响（`repl.go:50,381`）。改造后 `prof` 由 `flow` 提供，renderer 改由 `flow` 构造/重建（`/theme` 时同步） |
| `REPL.view`（`agent.EventSink` 闭包） | `REPL.view *toolView`；`REPL.print(text, kind)` 转调 `view.Content` |
| `REPL.stream`（`streamEvent`） | `turn.Handle` |
| `REPL.print`（借道 `EventContent`） | `REPL.view.Content(text)`（语义等价，去掉事件借用） |
| `ask()` | `runTurn(q)` = `beginTurn` → `agent.Ask(ctx, q, t.Handle)` → `t.End(err)` |
| `turnSink()` 的首行空行（`repl.go:107-115`） | 移入 `turn.Handle` 的**首个事件前**（懒补）——保持"零事件回合（`Ask` 立即报错）不补空行、不与分隔线前导换行叠成双空行"的既有语义；`turnSink` 闭包随之删除 |
| 新增 | `REPL.st *streams`（由 `NewREPL` 的 Option 注入） |

### 4.6 写点替换映射（按类别）

| 类别 | 现状（行号） | 目标 |
|---|---|---|
| 输入循环/退出/分隔线 | 162、170、181、188、193、218、220、226 | `emit(KindDecor, …)` / `emit(KindNotice, …)`；中断提示 218/220 改 `err.emit(KindError, …)` |
| 命令反馈 | 253、256、259、263、265、272、277、280、284、287、293、303、305、310 | `emit(KindNotice)`，失败分支 `err.emit(KindError)`；`/model` 列表用 `atomic` 保证多行不被 spinner 切入 |
| 主题/think | 317、318、325、332、336、374、387、389、394、398、400 | 同上；`printThemeSample` 的多块渲染包在一次 `atomic` |
| 历史回放 | 407、414、422、428、430、471、473、477、486、495 | `emit` 走 `atomic`（整条消息一次写完）；Kind 按 §4.3 推断；渲染逻辑与清洗策略**不变** |
| `/load` 菜单与结果 | 514、518、529、533、536、542 | 结果/错误经 flow；picker 自身经 `streams.out.Write`（§4.7） |
| 工具块/状态行/spinner | `toolview.go` 8 处、`spinner.go` 3 处 | `atomic` 包整块；spinner 帧与清行经 `emit(KindSpinner, …)` |
| stderr | `main.go` 6 处、`repl.go:223` | `err.emit(KindError|KindNotice, …)`（§3.7 清单） |

### 4.7 spinner / picker / Editor 接线

- `spinner`：字段 `out *output`；启动条件 `mode 允许 Spinner && prof.TTY`（`allows(KindSpinner)`）；`loop` 里 `s.out.emit(KindSpinner, style.ClearLineHome()+…)`；`stop` 保持"先等 goroutine 退出、再写清行"的顺序，**不持锁等待**（沿用现状，避免死锁）。
- `picker`：`pickSession(term, list, out io.Writer)`，`p.render(out, first)`；`pickByNumber` 的非 TTY 分支经 `streams.out.emit(KindNotice, …)`。`REPL` 调用处传 `r.st.out`（`*output` 满足 `io.Writer`）。
- `Editor`：`NewREPL` 里 `ed.SetOutput(r.st.out)`——raw 期写权仍归 `Editor`（时序契约，§3.8），裸 `Write` 不触发 `guard`。

### 4.8 锁模型

现状：`WireToolView` 内 `var mu sync.Mutex`，手工传给 spinner；`spinner.loop` 持锁写帧；`stop()` 在锁外等待 goroutine 退出后再持锁清行。

目标：锁唯一归属 `output`，**两条流各一把**。

- spinner 每帧：`emit`（自加锁）；
- 工具块：`atomic(KindToolBlock, func(w io.Writer){ 写标题; 写正文; 写状态行 })`；
- 死锁风险点一：`atomic` 回调内若调用 `output` 方法会自锁 → 注释 + review 约束（可选：测试构建下的重入 panic 守卫）；
- 死锁风险点二：跨流嵌套（在 `out.atomic` 内写 `err`）→ 明令禁止；两流互斥区互不相干；
- 保持"停 spinner 不等锁"的顺序：`v.sp.stop()` 在 `atomic` 之外调用。

### 4.9 main.go 接线

- `ToolWidth()` / `ToolTTY()` 保留包级函数：`main.go:43` 在 `NewREPL` 之前用它设置全局 profile（启动顺序不变）。
- **接线顺序改为：parse flags → 计算 mode → `repl.NewStreams(os.Stdout, os.Stderr, mode)` → `LoadConfig` → … **。这样 `main.go:38` 的启动错误也能走 `streams.err`，不留裸 `fmt.Fprintf`。
- 新增 flag：`-p` / `--plain`（`flag.BoolVar` 双绑，与 `-n`/`--no-save` 同写法）、`--verbose`；`--verbose` 无 `--plain` 时报错退出。
- `WireToolView(repl.ToolWidth, a.ToolOutputLines())` 改为构造 `toolView`（持 `streams`），同一个 `streams` 传给 `NewREPL`（Option：`WithStreams(*streams)`、`WithTerminal(readline.Terminal)`）。
- `ask` 分支：`a.Ask(ctx, q, view.Handle)`；收尾与错误经 `streams`。

### 4.10 回放路径（保持行为）

- `/history` 无参摘要行：`style.OneLine` + 120 rune 截断（已提交），只改写入出口。
- `/history n`/`all`：assistant 走 markdown 管线；tool 正文走 `style.Dim.Frame`；均不改清洗策略。
- Kind 按 §4.3 推断（不是"沿用被回放记录的 Kind"）。
- 目的：本方案不引入行为变更，只做结构收敛；"回放复用实时渲染策略"列入阶段 5（可选）。

### 4.11 明确不改的行为

- 非 TTY 下"动画关闭、状态行保留"（`docs/design.md:126`、`README.md:120`）——这是默认（rich）行为，plain 是显式 opt-in 的第三条路。
- 工具块的 `Frame`/`Passthrough` 双出口与 `tool_output_lines` 头 3 尾 2 截断（plain+verbose 下同样适用）。
- interactive 工具不启 spinner、结束追加式渲染。
- 模型通道（history/jsonl/请求）零变换。
- `colors: on|off` 语义与 TTY 探测（plain 不改变它们，只在 plain 内部叠加）。

## 5. 测试方案

### 5.1 分层矩阵

| 层 | 覆盖对象 | 手段 | 自动化程度 |
|---|---|---|---|
| 纯函数 | `Render*`、`Frame`/`Passthrough`/`OneLine` | 现有单测 | 完全 |
| 结构体 | `output`/`streams`/`flow`/`toolView`/`turn` | 注入 `*bytes.Buffer` 作为 writer | 完全 |
| 门禁 | `visSet` 三档 × `Kind` | 表驱动断言每个 (mode, kind) 的可见性 | 完全 |
| 并发互斥 | 工具块 vs spinner 帧 | `-race` + 原子性压测 | 完全 |
| 写权协议 | 输入期 / 输出期互斥 | `guard` 钩子（两流都挂）+ `fakeTerm` 标志 | 完全（协议层） |
| 写权字节序 | Editor 与 output 共写一个 writer | 共享 buffer + 段落顺序断言 | 完全（顺序层） |
| 输出模式 | plain / verbose 的字节契约 | golden 比对（§5.7） | 完全 |
| 真实终端 | 光标/覆盖/termios 时序 | `script` pty + 人工目视 | **半自动（上限）** |

**结论（修正初稿的过强表述）**：写权冲突可测到"协议正确 + 并发正确 + 字节序正确 + 模式契约正确"；`bridge` 期间子进程直接持有真实 tty（fd 1/2 都在 pty 内），`output` 的门禁与 `guard` 都覆盖不到——常态下 stdout 与 `/dev/tty` 是同一设备，断言无法区分。因此 bridge 期间只能人工验收（§5.5 清单第 4/5 条），不引入终端模拟器。

### 5.2 写权冲突的三种手段

1. **协议断言（必做）**
   - `output.guard` 在每次 `emit`/`live`/`atomic` 前回调；测试里设 `inInput` 标志（`fakeTerm.ReadKey` 进入时置位、返回键值后清除）。
   - 跑一轮完整 `Run`（喂 `/help`、`/exit`），断言"输入期发生 emit 写入"即失败；**两流都挂 guard**。
   - 覆盖点：`Readline` 全程、picker 的 `Raw/Restore` 区间、`turn.End` 之后到下一次 `Readline` 之间的写入。
   - 注意：`Editor`/`picker` 走裸 `Write`，不触发 `guard`，因此不会误报。
2. **字节序断言（必做）**
   - `Editor.SetOutput(buf)` 与 `streams.out.setWriter(buf)` 指向同一 `*bytes.Buffer`。
   - 断言段落顺序：`提示符 → 回显 → 输出块`，并断言输出块前后存在预期的 `\r\n`/清行序列；可捕捉"输出插进输入行中间""回显与状态行粘连"。
3. **pty 集成（保留人工）**
   - 复用现有门控风格，新增 `REPL_TTY_E2E=1` 时执行 `script -qec` 用例；默认 skip。
   - 人工目视清单见 §5.5。

### 5.3 测试地基（阶段 0 必须先落地）

| 项 | 内容 |
|---|---|
| 注入 Option | `NewREPL` 支持 `WithStreams(*streams)`、`WithTerminal(readline.Terminal)`；`newTestREPL`（已存在于 `dispatch_test.go:78`）改为经 Option 构造并返回注入的 buffer |
| repl 侧 fakeTerm | 实现 `readline.Terminal`（`Raw/Restore/ReadKey/Size`），按键序列驱动 `Run` |
| profile 局部化 | `flow.prof` 字段化，测试构造 `style.Profile{TTY: false, Colors: style.LevelNone}`，不再依赖全局默认 |
| guard 钩子 | `output.guard func()`，生产为 `nil`；两流各一个 |
| 原子性标记 | 并发用例里给每次 `atomic` 写入包上唯一首尾标记，便于断言不交错 |
| stderr 清理 | `captureStderr`（现为死代码）改注入后启用，或删除 |

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

# 4) plain 子代理契约（stdout 仅答案、stderr 仅诊断、零 ANSI）
script -qec "./tanyan -p -n ask '说一句话'" /dev/null | cat -v
./tanyan -p -n ask '说一句话' >/tmp/ans 2>/tmp/err; cat -v /tmp/ans; cat -v /tmp/err

# 5) plain+verbose（工具痕迹以纯文本追加式出现，无光标控制）
script -qec "./tanyan -p --verbose -n ask '跑一条命令并总结'" /dev/null | cat -v
```

人工检查清单：

1. 提示符与回显行在 spinner 动画期间不被擦除；
2. 工具块标题的 `⋯` 原位重绘正确，块与块之间无空行错位；
3. `/load` picker 上下移动无残影，Esc 返回后提示符正常；
4. `Ctrl+C` 中断回合后终端回显、光标、颜色状态正常（SGR 无泄漏），提示行出现在 stderr；
5. 退出后 `stty -a` 与进入前一致（termios 已恢复）；
6. plain 下 stdout 无 `\x1b`/`\r`/`▸`/`↳`，verbose 下无光标控制序列。

### 5.6 每阶段测试清单

| 阶段 | 新增/迁移用例 |
|---|---|
| 0 | `TestStreamsInjectWriter`、`TestFakeTermDrivesRun`、`TestGuardNoEmitDuringInput`（骨架） |
| 1 | `TestOutputAtomicNoInterleave`、`TestToolBlockSingleWrite`、`TestSpinnerFramesViaOutput`、`TestPickerUsesWriter`、`TestNoticeDecorErrorKinds`、`TestStderrRoutedThroughErr`、迁移 `captureStdout`/`captureStderr` 用例 |
| 2 | `TestFlowContextCarriers`、`TestTurnEndSettlesMarkdown`、`TestTurnInterruptAndErrorPaths`（含中断提示走 stderr）、`TestHistoryReplayViaOutput` |
| 3 | `TestToolViewStateFields`、`TestToolViewContentSemantics`（迁移自 `toolview_test.go`） |
| 4 | `TestVisSetMatrix`（三档 × 每个 Kind）、`TestPlainAskStdoutIsAnswerOnly`、`TestPlainVerboseKeepsToolText`、`TestPlainVerboseNoCursorControl`、`TestVerboseRequiresPlain`、`TestRichStreamsByteIdentical` |
| 5（可选） | `TestReplayReusesLiveRendering` |

### 5.7 输出模式 golden（阶段 4 的核心验收）

1. `TestPlainAskStdoutIsAnswerOnly`：mock LLM 脚本含工具调用 + 长输出 + 状态行，断言 stdout **严格等于答案文本**——零 `\x1b`、零 `\r`、无 `▸`、无 `↳`、无孤立空行、无首尾多余空行；
2. `TestPlainAskErrorsToStderr`：注入 LLM 错误，断言 stdout 仍为纯答案、stderr 承载错误、退出码非 0；
3. `TestPlainVerboseKeepsToolText`：断言工具块与状态行以纯文本形态出现，且块间无粘连、无多余空行；
4. `TestRichStreamsByteIdentical`：rich 下对同一事件流做双流 golden 比对，守住"默认零行为变更"（opt-in 的安全网）。

## 6. 分阶段实施与验收

| 阶段 | 内容 | 验收标准 | 回滚点 |
|---|---|---|---|
| 0 测试地基 | 注入 Option（`WithStreams`/`WithTerminal`）、repl fakeTerm、`guard`、pty 脚本模板 | 现有测试全绿；fakeTerm 能驱动一轮 `Run` 到退出 | 独立提交，可直接 revert |
| 1 输出收敛 + 双流 | `repl/streams.go`；替换 70 处写点（含 stderr 7 处）；spinner/picker/Editor 接线；每次写入均携带 `Kind` | §5.6 阶段 1 用例 + `-race` + pty 目视 1/2/4/5；rich 模式字节不变 | 独立提交 |
| 2 flow + Kind 枚举 + turn | `repl/flow.go`；`REPL` 字段下沉（含 `rend`）；`turn` 生命周期 | §5.6 阶段 2 用例；行为与阶段 1 一致（对比 pty 输出） | 独立提交 |
| 3 toolView 结构体化 | 闭包 → 结构体；状态成字段 | `toolview_test` 全量迁移通过；pty 目视 2/3 | 独立提交 |
| 4 输出模式 | 三档 `visSet`；`-p/--plain`、`--verbose`；plain 六条语义 | §5.6 阶段 4 用例 + §5.7 golden + pty 目视 4/5 | 独立提交（前置：写入均带 `Kind`） |
| 5（可选）回放归一 | ⏸ **拆出独立评估**：历史数据结构与实时事件不等价（`agent.Message` 无 Kind/ToolResult，工具消息只剩 `ShellResult.String()` 扁平文本），见 `docs/repl-replay-rendering.md`（结论：建议不做或降级为"只统一样式函数"） | — | — |

阶段 4 落地要点（与初稿的差异）：不引入 `live` 原语——plain 下的光标控制由"KindSpinner/ToolBlock 屏蔽 + inline 重绘按 `streams.cursor()` 退化"双重保证，再加一个与 `emit` 同义的函数无收益；模式解析收敛为可测的 `repl.ParseMode(plain, verbose)`（初稿只说"无 `--plain` 时报错"，未给落点）；`--verbose` 无 `--plain` 的错误文案走裸 stderr（发生在 `streams` 构造之前，与 flag 解析错误同级）；`toolView.animate()`/`streams.cursor()` 为运行期查询而非构造期快照（阶段 1/3 审计遗留项）；§7 预警的"屏蔽漏项 → 结构性空行"在阶段 4 实测复现并修复（ToolEnd 被屏蔽时不置 `justEnded`）。

实施记录：阶段 0-4 已完成，各为独立提交（按上表回滚点可单独 revert）。行为回归一律用"前一阶段二进制 vs 当前二进制"逐字节比对：阶段 1 与阶段 0 比 pty 输出（14 条命令）；阶段 2 与阶段 1 比 4 类场景（pty 单回合含工具块与 inline 重绘、pty 两回合含 `/md` 切换、回合错误、ask 错误），归一化时钟与耗时后全部一致。

阶段 2 落地要点（与初稿的差异）：`flow` 实际字段为 `{st, prof, live, md, rend}`——`width` 仍归 `toolView`（无消费者），`rend` 与 `prof` 由 `REPL` 在构造期快照并由 `flow` 携带，`/theme` 重建 `REPL.rend` 后新回合自动取到；`turnSep` 由读全局 profile 改为 `turnSep(prof, d)`；`REPL` 侧 `md`/`stream`/`turnSink`/`streamEvent`/`writeContent`/`settleMd`/`mdBlocks` 全部移除（`mdBlocks` 降为包级纯函数供回放用）。全局 `style.GetProfile()` 只剩 `NewREPL` 构造期一处读取。

**阶段 1 的收益已经足够大**：即使后续不做，也已获得"单点输出 + 双流 + 可注入测试"的全部收益。阶段 4 是子代理可用性的门槛，建议紧接着做。

## 7. 风险与对策

| 风险 | 影响 | 对策 |
|---|---|---|
| 终端时序回归 | 工具块重绘、spinner 交错出错 | 行为保持不变的逐点替换；每阶段跑 pty 目视清单；保留 `\x1b[1A\r\x1b[K` 的生成位置（渲染函数） |
| 屏蔽漏项 → 结构性空行残留 | plain 输出出现孤立空行、verbose 下块粘连 | 空行随所属块进出（§3.3 表）；`justEnded` 在屏蔽时不置位；golden 用例逐字节守住 |
| `atomic`/跨流重入 | 自锁死 | 注释 + review；测试构建下的重入 panic 守卫；明令禁止跨流嵌套 |
| readline 写权边界 | raw 期两方同时写导致屏幕错乱 | 契约固化（§3.8）+ 协议断言；`Editor.SetOutput` 与 `streams.out.setWriter` 指向同一 writer；`guard` 不覆盖裸 `Write`，断言语义精确 |
| picker 特例 | raw 下自绘与 output 冲突 | picker 走 `streams.out.Write`（裸写）；`Raw/Restore` 区间禁止 `emit`（测试断言） |
| bridge 覆盖不到 | interactive 期间写权无法断言 | 明确测试上限（§5.1）；人工验收清单第 4/5 条 |
| 测试迁移面 | `captureStdout` 25 处 + `captureStderr` 需改 | 阶段 0 先提供注入能力，阶段 1 集中迁移 |
| `style.Profile` 全局残留 | 测试间串扰（`-race` 下更明显） | `flow.prof` 字段化；保留全局仅用于 `main` 启动期设置 |
| main 启动顺序 | `ToolTTY` 在 `NewREPL` 前被调用 | 保留 `ToolTTY`/`ToolWidth` 包级函数；`streams` 在 flag 解析后、`LoadConfig` 前构造 |
| plain 泄漏成默认 | 既有用户输出被静默改变 | 仅 CLI flag（无 env/config）；`--verbose` 无 `--plain` 报错；`TestRichStreamsByteIdentical` 守住默认 |
| 隐含行为变更 | 非 TTY 状态行、截断策略被"顺手改掉" | §4.11 列为不改项，测试与文档双锁 |

## 8. 防膨胀约束

- 不引入 `Collector`/`Presenter`/标签策略表/**通用过滤器注册表**；只允许三档写死的 `Kind` 可见集（`visSet`）。
- `Kind`/`outMode`/`visSet` 不导出包外、不进 `agent.Event`、不做策略分发（§3.3 边界）。
- `output` 只含：writer、锁、可见集、guard；**不含格式化逻辑**——渲染仍是 `style` 纯函数 + `repl` 组装函数。
- 双流为同类型两实例；将来拓展（日志分流、结构化输出）加实例或改 `visSet`，不加分支。
- 不为测试在生产路径加入分支（`guard` 为 `nil` 即无行为）。
- 不新增第三方依赖；总新增代码控制在 `streams.go` + `flow.go` 两个文件。

## 附录 A 现状写点清单（供替换核对）

`repl/repl.go`（51 处）：

| 区间 | 行号 | 类别 |
|---|---|---|
| 输入循环/回合 | 112、162、170、181、188、193、218、220、226 | 欢迎屏、退出、分隔线、中断/错误文案 |
| 命令反馈 | 253、256、259、263、265、272、277、280、284、287、293、303、305、310 | `/help` `/new` `/load` `/stat` `/model` `/md` 与未知命令 |
| 主题 | 317、318、325、332、336、374 | `/theme` 与样本渲染 |
| think | 387、389、394、398、400 | `/think` |
| 历史回放 | 407、414、422、428、430、471、473、477、486、495 | `/history` 摘要、单条、tool 正文、消息头 |
| `/load` | 514、518、529、533、536、542 | 菜单降级、结果、错误、legacy 提示 |

其余：`repl/toolview.go` 8 处（闭包内）、`repl/picker.go` 8 处、`repl/spinner.go` 3 处、`readline/editor.go` 11 处（仅接线 `SetOutput`，不改内部）、`main.go` 8 处（其中 6 处为 stderr）。

**stderr 子集（7 处）**：`main.go` 38、64、72、79、88、93；`repl/repl.go:223`。另需从 stdout 迁入 stderr 的 2 处：`repl/repl.go:218`、`220`（中断提示）。

## 附录 B 代码锚点与相关文档

- 事件词汇表：`agent/event.go`
- 事件发射点：`agent/agent.go:243,261,274,276`；`agent/llm.go:251,258,265,283`；`agent/llm_responses.go:239,248,259,268`
- 渲染纯函数：`repl/toolview.go:35-245`；过滤器：`style/filter.go`
- 闭包现状：`repl/toolview.go:247-306`；spinner 锁现状：`repl/spinner.go:38-107`
- TTY/宽度探测：`readline/terminal_unix.go:17-23,50-56`；`repl/toolview.go:307-323`；`main.go:43`
- `ask` 路径（不经 flow）：`main.go:67,76`
- 相关文档：`docs/design.md`《工具视图渲染》《测试》；`docs/render-pipeline.md`《ANSI 过滤器》；`docs/interactive-tty.md`（bridge 与写权边界）；`README.md`（TTY 行为契约）

## 附录 C 后续修订（超出阶段 0-4）

| 时间 | 变更 | 落点 |
|---|---|---|
| 2026-09 | `/md` 命令移除：markdown 渲染恒开（非 TTY 与 plain 旁路），`mdLive` 字段与 `MsgMdOn`/`MsgMdOff` 删除 | `repl/repl.go`、`repl/flow.go`、`repl/messages.go`、`repl/completer.go` |
| 2026-09 | `ask` 单发默认走 plain+verbose 档：`repl.SingleShot(outMode)` 把 CLI 的 rich 降到 `modePlainVerbose`（显式 `-p` 更窄时不动）；单发不再有 spinner 与光标上移重绘，工具块追加式输出，颜色保留 | `repl/streams.go`、`main.go` |
| 2026-09 | 追加式工具块（非 TTY、plain+verbose）不再重复标题：新增 `RenderToolEndAppend`（正文块 + 状态行），`ToolEnd` 分支按 inline / 交互式 / 追加式三分派发 | `repl/toolview.go` |
| 2026-09-15 | 工具状态行清洗：`renderToolBody` 的 `↳` 状态行文本走 `term.Strip`——该行此前由 `sem.Info.Sprint` 原样直出，`cwd`（模型可控）与 exec 错误文本里的 `\x1b` 序列可直接驱动终端 | `repl/toolview.go`、`repl/toolview_test.go` |
| 2026-09-15 | 状态展示追加化（替代 spinner）：删 spinner 帧循环与工具块 inline 重绘，等待/执行期改为每 10s 换行追加 `» 等待响应 .` / `  » 执行中 .`，工具收尾三分派合一为 `RenderToolEndAppend`（标题只出现一次） | `repl/status.go`（新增）、`repl/spinner.go`（删除）、`repl/toolview.go`、`repl/flow.go`、`repl/streams.go`、`repl/messages.go` |
| 2026-09-15 | 上条修复的回归修复：`ctty.resetModes` 去掉 `CSI r`（DECSTBM 会把光标移到滚动区首行），且常规 `run_shell` 路径不再调 `ResetModes`——`handed` 在 REPL 中恒真，等于每次命令后都写整串，工具块重绘的 `CSI 1A`/`CR CSI K` 落到屏幕顶部（工具块画到顶上、覆盖欢迎屏、残留 spinner） | `ctty/ctty_posix.go`、`agent/shell.go`、`ctty/ctty_linux_test.go` |
| 2026-09-16 | 状态行心跳改为行内点累加：`statusTickInterval = 1s`、满 `statusLineSpan = 10` 点换行，行首写一次带墙钟秒数的前缀并以一个空格位收尾（`» 等待响应 0s `），追加的点与行首同色且各自 reset，`stop()` 补 `\n` 收尾当前行；`EventReasoning` 经 `heartbeat.setPhase` 回补 `» 思考中`（收尾当前行 + 新前缀开新行，秒数延续、同相位 no-op）；`turn.End` 调 `toolView.Stop()` 补上"等待期被打断无停止点、心跳写到下一次请求"的漏洞 | `repl/status.go`、`repl/flow.go`、`repl/status_test.go` |

### 已知缺口：动态文本的转义清洗（2026-09-15 登记，未做）

工具块正文与标题早已收敛（正文 `Dim.Frame` / `Passthrough`，标题 `Dim.Frame`），状态行已在上表条目内补齐；**仍未清洗**的是其余经 `output.emit` 直出的动态文本：

- `repl/picker.go` `sessionPicker.render` 的 `SessRow` 摘要（会话首条 user 消息，用户可粘贴任意含 `\x1b` 的内容）
- `repl/repl.go` `printHistoryFull`：非 markdown 模式的 user/assistant 正文，以及 `→ <tool> <args>` 工具调用行（`historyLine` 摘要行走 `term.OneLine`，已安全）
- `/model` 列表：服务端 `/models` 返回的模型名（`/theme` 与其余列表为内置常量，安全）

方向：**在内容插入点清洗**，或给 `output` 增设"内容通道"（只放行自生成的控制序列）；**不要**在 `emit` 施加全局 `Strip`——该决定仍成立，但理由已变：原理由"会废掉工具块上移重绘与 spinner 帧"随 2026-09-15 追加化消失（`emit` 现只承载文本 + SGR + `\n`），改成"全局清洗会一并抹掉自生成的颜色"。

本附录之前的章节保留历史方案与当时的 `mdLive`/ask 走 rich 的描述，不再随代码同步。
