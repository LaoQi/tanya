# 分层与模块依赖：评估结论与修正方案

> 状态：**已全部落地**（2026-09-28 评估并实施；S1–S6 完成，S7 按 D3 不做）
> 范围：`ctty` / `render` / `readline` / `repl` / `tools/shell` 的依赖边与职责收口
> **不动**：`agent`（基线已零内部依赖）、`config`、工具注入与缓存不变性契约
> 判据：每阶段要么「依赖边消失」（可用 `go list -deps` 断言），要么「行为可观测」（单测 + `render_audit`）；两者都不满足的改动不做
> 手工验证分工：无 TTY 环境只能跑 `scripts/render_audit.py`（pty 驱动）与单测；S4 的缩放目视、S5 的工具块目视需在真终端 `make build && ./tanya` 复核

## 0 评估结论（2026-09-28）

实测依赖矩阵（`go list -f '{{.Imports}}'`，含 std 外的 tanya 内部边）：

| 包 | fan-out | fan-in | 不稳定度 | 内部依赖 |
|---|---|---|---|---|
| `.`（main） | 8 | 0 | 1.00 | agent config ctty readline render/term repl tools tools/shell |
| `agent` | 0 | 6 | 0.00 | — |
| `ctty` | 0 | 4 | 0.00 | — |
| `render/term` | 0 | 6 | 0.00 | — |
| `render/style` | 1 | 7 | 0.12 | render/term |
| `render/ir` | 1 | 4 | 0.20 | render/style |
| `render/theme` | 1 | 3 | 0.25 | render/style |
| `tools/shell` | 2 | 3 | 0.40 | agent ctty |
| `config` | 1 | 1 | 0.50 | agent |
| `readline` | 3 | 2 | 0.60 | ctty render/style render/term |
| `render/markdown` | 3 | 1 | 0.75 | render/ir render/style render/term |
| `render/markup` | 3 | 1 | 0.75 | render/ir render/style render/theme |
| `tools` | 3 | 1 | 0.75 | agent tools/builtin tools/shell |
| `render` | 5 | 1 | 0.83 | render/ir render/markup render/style render/term render/theme |
| `repl` | 10 | 1 | 0.91 | agent ctty readline render render/ir render/markdown render/style render/term render/theme tools/shell |

结论：

1. **分层成立**：严格 DAG、无环；`agent`（核心）与 `ctty` / `render/term`（终端原语）都是 fan-out 0 的稳定叶子，依赖倒置用得对（`tools` 实现 `agent.Tool`、`Console`/`Lease` 注入给 `tools/shell`、`ToolResult.Meta any`）。构建矩阵（linux + darwin/windows 交叉）与 `go test -race` 全绿。
2. **剩余问题不是"有没有分层"，而是三处职责渗透 + 一处链路断裂**：表现层词汇渗入输入层、进程级全局状态充当隐式参数、可插拔性只到 `agent` 就停（`repl` 反向认识 `tools/shell`）、以及终端尺寸变化没有任何一层负责。
3. `repl` 是唯一的高不稳定度大节点（3578 行实现、21 文件、fan-out 10），本方案**只收掉它的入边耦合**，不做拆包（见 §3 D3）。

## 1 问题清单与归属

| # | 问题 | 证据 | 阶段 |
|---|---|---|---|
| P1 | `render/term` 的进程级全局 `Profile` 是隐式参数：`style.Style.Sprint`/`Frame` 内部读全局，`render.Template.Render`、`render.Sprint` 同；测试需 30+ 处 `SetProfile` save/restore 样板 | `render/term/profile.go:20`、`render/style/style.go:42`、`render/style/frame.go:7`、`render/template.go:33`、`render/render.go:72`、`repl/repl.go:139` | S2 |
| P2 | `readline`（输入层）依赖 `render/style`（表现层词汇）：`Editor` 持有 `rstyle.Style` 仅为 ghost 置灰与菜单高亮 | `readline/editor.go:6,46-47,62,320,413` | S3 |
| P3 | 终端尺寸变化无归属：`EventResize`/`EventHangup` 声明后**从不产出**；`SIGWINCH` 只在 pty 借用期被 `readline` 直接注册；`repl` 的工具块宽度在启动时冻结（`facts.Width` 是值接收者方法值 = 常量），而 `picker` 每轮重读 `Size()` —— 三种行为并存 | `readline/console.go:26-31`、`readline/lease_linux.go:9,151`、`repl/repl.go:152`、`repl/picker.go:155` | S4 |
| P4 | 可插拔性不对称：工具经 `agent.WithTools` 完全可插拔，但 `repl` 用 `res.Meta.(*shell.Result)` 类型断言 + `name == "run_shell"` 字符串分派认死具体工具；第三方工具的自定义 `Meta` 拿不到渲染 | `repl/toolview.go:74,172,340` | S5 |
| P5 | 文档引用漂移：4 个已删文件被 6 份文档当作现行文件引用 | `docs/terminal-console.md`、`docs/ctty.md` 等 | S1 |

不属于问题、但需要在文档里正名的两点见 §3 D1 / D2。

## 2 阶段

每阶段：一次 commit、独立可验收；文档（`AGENTS.md` 现行约束、`CHANGELOG.md` 变更、`docs/design.md` 相应小节）随阶段更新。

### S1 文档引用同步

- 现行文档中指向现行实现的文件名更新为现名；历史段落（问题快照、搬迁记录、阶段表）保留原文件名，但每份文档给一次**文件名对照注**（旧 → 新）。
- 对照表：`terminal_posix.go`→`device_posix.go`、`terminal_windows.go`→`device_windows.go`、`terminal_io.go`→`device_io.go`、`terminal_stub.go`→`device_stub.go`、`bridge_linux.go`→`lease_linux.go`、`secure.go`/`secure_stub.go` 已整体删除（自愈收敛为 `Console.Sane()` + `device_posix.go` 的 `saneTermios`）、`agent/tty_bridge.go`→`tools/shell/bridge.go`、`agent/shell*.go`→`tools/shell/{shell,tool,platform*}.go`。
- 验收：`rg 'terminal_posix|bridge_linux|secure\.go|terminal_windows|terminal_io\.go' docs AGENTS.md` 的每处命中，要么已改用现名，要么所在段落有对照注可解析。

### S2+S3 合并落地（`Style` 绑定 profile + 输入层注入样式器；消 P1、P2）

> **执行偏差（2026-09-28）**：原计划 S2 与 S3 分开提交，实际合并为一次——`Style.Sprint`/`Frame` 的签名一变，`readline/editor.go` 的两处样式调用点即无法编译，而最省的修法正是 S3 的 `Styler` 接口，分开做会先把 profile 塞进 `Editor` 再删掉（无谓的中间态）。API 最终形态：`Style.With(prof) Bound` + `Bound.Sprint/Frame`（`Sprint`/`Frame` 不再有无 profile 的重载）、`term.Passthrough(prof, text)`、`render.Sprint(prof, …)`、`Template.Render(prof, …)`；`readline` 侧 `Styler` 接口 + 默认 no-op 样式器。

### S2 `Style` 显式绑定 profile，删除 `term` 全局 profile（消 P1）

- `render/style`：新增 `func (s Style) With(p term.Profile) Bound` 与 `type Bound`（`Sprint(string) string`）；`Sprint`/`Frame` 改带 `term.Profile` 参数。
- `render/term`：删 `var current` / `SetProfile` / `GetProfile`（`DetectProfile` 保留，纯函数）。
- `render`：`Sprint(prof, in...)`；`Template.Render(prof, resolve)`（passthrough 分支的 `Colors == LevelNone` 改用传入 profile）。
- `repl`：新增 `WithProfile(term.Profile)` 由 `main` 注入，`r.prof` 不再读全局；全部 `sem.X.Sprint/Frame` 带上 `r.prof`。
- 测试：删除 `SetProfile`/`GetProfile` save-restore 样板，改为显式传 profile（`repl/faketerm_test.go` 的 helper 一并改注入）。
- 验收：`rg 'SetProfile|GetProfile'` 全仓零命中；`go test -race ./...` 全绿；`render_audit` 15 PASS（渲染字节必须不变）。
- 风险：中（触及每个渲染调用点）。回退：整阶段 revert 即恢复全局。

### S3 输入层注入样式器，`readline` 不再认识表现层（消 P2）

- `readline`：定义消费者侧接口 `type Styler interface{ Sprint(string) string }` + 内部 no-op 实现；`Editor.dim/accent` 改该接口，`SetStyles(dim, accent Styler)`；删 `render/style` 导入。
- **保留** `readline → render/term`：`render/term` 是终端原语层（宽度/ANSI 清洗/控制序列，与 `ctty` 同层同性质），不是表现层词汇——见 §3 D1 的口径统一。
- `repl`：`ed.SetStyles(sem.Dim.With(r.prof), sem.Accent.With(r.prof))`。
- 验收：`go list -deps ./readline` 不含 `render/style`；`readline` 现有 golden 与 `render_audit` 逐字节不变。

### S4 终端尺寸变化：收敛进 `ctty` 信号面 + 事件化 + 单一真值源（消 P3）

设计（**不做轮询**，见 §3 D2）：`ctty` 已是运行期信号的唯一收敛点（`signals.go` 单 channel + 单分发 goroutine），`SIGWINCH` 走同一条路，不新开第二套探测机制。

- `ctty`：`signals_posix.go` 增 `resizeSignals = []os.Signal{unix.SIGWINCH}`（windows/stub 为空集）；`signals.go` 的 `watchSignals()` 并入该清单、`dispatch()` 按信号类分流（exit → `Exit` / interrupt → `broadcast` / resize → `broadcastResize`）；新增 `OnResize(fn func()) (cancel func())` 订阅面。
  - **武装按信号类惰性**：`WatchSignals()` 仍只管 exit/interrupt（语义与今天逐字不变，`main` 单点调用）；`OnResize` 首次订阅时单独把 resize 清单登记进**同一个 channel/dispatcher**。理由：`readline` 作为库被嵌入时不应因为要收 resize 而连带装上 SIGTERM/SIGHUP 处置（那会让宿主进程不再被 SIGTERM 默认杀死）。
- `readline`：`lease_linux.go` 删 `os/signal` 与 `winch` 通道，改 `ctty.OnResize` 传播 `TIOCSWINSZ`；`console.go` 在 `newConsole` 期订阅 `ctty.OnResize`，置 coalesce 标志并向订阅者分发 `Event{Kind: EventResize}`，`ReadEvent`（编辑器/选择器独占期）与常驻读者循环各自消费该标志（多次缩放合并为一次，延迟上界 = raw 读的 `VTIME` 周期 ~100ms，无新增系统调用）；`editor.go` 收 `EventResize` → `render("")` 重绘（`render` 本就从 `Size()` 现读宽度、按 `rowsUsed` 清旧行，故无需额外状态）；`picker.go` 改吃事件（删每轮 `Size()` 比对）。删从未产出的 `EventHangup`（挂断走 `io.EOF`，见 `device_io.go:70`）。
- `repl`：工具块宽度改活取——`NewToolView` 的 width 回调改为闭包 `con.Size()`（`TermFacts` 降为无终端时的兜底与测试注入口），`ask` 单发同路径。
- 验收：新增单测（fakeDevice 改 `Size` → `ReadEvent` 返回 `EventResize`；订阅者收到事件；`editor` 重绘后 `cursorRow/rowsUsed` 与 `render` 后的实际行数一致）；`rg 'os/signal' --glob '!ctty/**'` 零命中；`render_audit` 15 PASS；真终端目视（缩放后工具块宽度跟随、编辑器重绘无残留、`interactive` 借出期子进程窗口跟随）。
- 风险：中（`Console` 事件面新增一类事件，须与既有 pending/借出挂起语义对齐）。

### S4 落地记录（2026-09-28）

- **`ctty`**：`signals_posix.go` 增 `resizeSignals = []os.Signal{unix.SIGWINCH}`（windows/stub 为空集）；`signals.go` 拆出 `signals()`（单 channel + 单 dispatcher，`watchOnce`）、`watchNotify`（退出/中断清单，`WatchSignals` 用）、`resizeNotify`（resize 清单，`OnResize` 首次订阅时登记），`dispatch` 按信号类分流（resize → `notifyResize()`）。**武装按信号类惰性**的理由见 §3 D2 与 `docs/ctty.md`：`readline` 作为库被嵌入时不应连带装上 SIGTERM/SIGHUP 处置。
- **`readline`**：`console.go` 在 `newConsole` 期订阅 `ctty.OnResize`（放在纯构造函数里，测试才能验到生产接线），`signalResize` 置位 + 推订阅者（借出期不推），`takeResize` 供 `ReadEvent` 与常驻读者消费；`editor.go` 收 `EventResize` → `render("")`；`picker.go` 改吃事件、删每轮 `Size()` 比对；`lease_linux.go` 删 `os/signal` 与 `winch` 通道，改经 `ctty.OnResize` 传播 `TIOCSWINSZ`（`fdMu` + `closed` 与 fd 拆除串行）；`EventHangup` 删除（挂断走 `io.EOF`，从未产出）。
- **`repl`**：新增 `LiveWidth(con, facts) func() int`——每次渲染现取 `con.Size()`，取不到回落 `TermFacts`、再回落 80；`NewREPL` 与 `main` 的 `ask` 单发同路径。**无状态**（不缓存宽度、不缓存事件），只有「何时重绘」依赖 `EventResize`。
- **验收**：`rg 'os/signal'` 仅 `ctty` 命中、`rg 'EventHangup'` 零命中、`-race` 全绿、darwin/windows 交叉编译通过、`render_audit` 15 PASS；新增单测 8 条（ctty SIGWINCH 实测送达与取消、Console 合并/订阅/借出期静默/常驻读者、编辑器重绘计数、picker 事件重排、`LiveWidth` 活取与兜底）。**两个 pty 端到端探针**（临时脚本，未入库）：① 100 列下工具块长行为 97 A，回合中途缩到 60 列后同一块变 57 A → 宽度活取生效；② `interactive: true` 的 `stty size` 在借出期缩放前后分别打印 `30 100` / `30 60` → 窗口尺寸传播生效。

### S5 工具视图注册点，`repl` 不再认识具体工具（消 P4）

- 新**叶包** `present`（仅依赖 `render/term`——输出侧原语，与 `ctty` 同性质）：`ArgsView`/`View`/`ToolView` 契约 + 注册表（按工具名索引，装配期注入、运行期冻结——与 `agent.WithTools` 同语义）。
- 新包 `tools/shell/view`：把 `run_shell` 专属渲染从 `repl` 搬出（`shellArgsView`/`commandLines`/`capLines` 的 shell 部分 + `shellView`/`chunkLines`/`shellStatus` 及其专属文案），导出 `View() present.ToolView`；只依赖 `tools/shell` + `render/term`（**不依赖 `render/theme`**：配色仍由 `repl` 的 `sem.Dim.Frame` 施加）。
- `repl`：`toolArgsView`/`toolEndBody` 改查注册表（`WithToolView(name, present.ToolView)`），删 `tools/shell` 导入与 `name == "run_shell"` 字符串分派；未注册的工具回落现有通用渲染（行为不变）。
- `main`：`repl.WithToolView("run_shell", shellview.View())`。
- 验收：`go list -deps ./repl` 不含 `tools/shell`；新增「第三方工具自带视图」用例（自定义 `Meta` + 注册渲染器走通、未注册回落通用文本）；`render_audit` 15 PASS（工具块/参数块逐字节不变）。

### S5 落地记录（2026-09-28）

- **新包 `render/present`（叶：仅依赖 `render/term`——输出侧原语）**：`ArgsView`/`View`/`ToolView`（`Args`/`Result` 两回调，`ok=false` 即回落）+ `Registry`（装配期注册、运行期冻结）+ 共享文本助手（`CapLines`/`Prefixed`/`PlainArgsView`/`ExpandTabs`/`TrimBlankEdges`/`Indent`/`Duration`）+ 通知载荷 `Notification`。**契约不引 `agent`**（回调传 `text string, meta any`），否则 `render` 树要反向依赖核心包。
- **新包 `tools/shell/view`**：`run_shell` 参数区（cwd/timeout/命令区/前缀/省略文案）与结果区（stdout+stderr 合并、`2|` 前缀、头尾截断、状态行「状态 · 耗时 · 行数」）及其专属文案；直接依赖仅 `present`/`render/term`/`tools/shell`（**不依赖 `agent`/`render/theme`**，配色仍由 `repl` 的语义色施加）。
- **`repl`**：`toolArgsView`/`toolEndBody` 改查注册表（`WithToolViews`，装配期冻结），删 `name == "run_shell"` 字符串分派与 `tools/shell` 导入；标题组合（`RenderToolStart`/`toolTitleLines`）与通用回落（`genericArgsView`/`textView`）留在 `repl`，改用 `present` 助手。
- **扩展（本次一并收掉，否则验收判据不成立）**：`notify_cmd` 的命令构造原本也在 `repl`（认识 `shell.Invocation`/`shell.Kind` + 引号规则），迁到 `tools/shell.CommandNotifier`（`NewCommandNotifier`/`Render`/`Quote`/`ValidateNotifyTemplate`），载荷类型落 `present.Notification`，`repl.Notifier` 接口改为 `Notify(present.Notification)`。
- **验收**：`go list -deps ./repl` 不再含任何 `tanya/tools` 包；`tools/shell/view` 直接依赖 = `encoding/json fmt strings present render/term tools/shell`；`gofmt`/`build`/`vet` 干净、`-race` 全绿（18 包）、darwin/windows 交叉编译通过、`render_audit` 15 PASS（工具块与参数块逐字节不变）。新增用例：`repl` 的第三方自带视图（注册走它、未注册回落、视图不认领时回落）、`tools/shell/view` 的结果区四例（状态行/`2|` 前缀/头尾截断/外来 Meta 不认领）。
- **测试布局偏差**：原计划把 `repl/toolview_test.go` 的 shell 用例整体迁走，实际只迁了 3 个直接引用视图内符号的用例（`ShellArgsView*`/`CommandOmitted`），其余保留在 `repl` 并注入真实 `run_shell` 视图（`testViews()` 复刻 `main` 装配）——这样既有 golden（整块成品形态）继续守着端到端字节，新包另有自身用例；`repl` 测试对 `tools/shell/view` 的依赖属测试二进制，不进生产依赖图。

### S6 `tools/shell` 依赖边正名与测试边修正（已落地，见下方落地记录）

- **测试边**：`package shell` 内使用 `readline` 的 pty 集成用例移到外部测试包（`shell_test`），解除「`readline` 不得依赖 `tools/shell`」的隐性枷锁（内部测试包导入 `readline` 会让后者永远无法依赖 `tools/shell`；外部测试包无此约束）。
- **正名**（§3 D1）：`AGENTS.md` 与 `docs/ctty.md` 明确「`ctty` 是零依赖叶子，任何层可直接依赖；注入只针对**终端所有权**（`Console`）」——`tools/shell` 直连 `ctty.ProtectJobSignals`/`DecodeCP` 属允许，不再记作违反注入原则。
- 验收：`package shell` 的测试不再导入 `readline`；`go test -race ./...` 全绿。

### S6 落地记录（2026-09-28）

- **测试边**：`tools/shell` 内部测试包（`package shell`）原先导入 `readline`（`rlConsole` 适配器 + 四个真终端用例），这会让 `readline` 永远无法依赖 `tools/shell`（内部测试包进测试二进制即成环）。四个用例（`TestRunShellKeepsTerminalForeground`、`TestRunShellFullRealTTYE2E`、`TestRunShellFullRealTTYReuse`、`TestRunShellFullCwdRealTTYE2E`）迁到**外部测试包** `package shell_test`（新文件 `tools/shell/tty_e2e_test.go`，`//go:build linux`），改用公开 API（`shell.New`/`Tool.Invoke` + JSON 参数 + `*shell.Result`），`rlConsole` 适配器随之搬走。
- **正名**（D1）：`AGENTS.md` 与 `docs/ctty.md` 明示「`ctty` 是零依赖叶子，任何层可直接依赖，不需要注入；注入只针对终端**所有权**」——撤回首轮对 `tools/shell → ctty` 的"违反注入原则"定性；`AGENTS.md` 另加一条**测试包边界**约定（需要上层包的集成用例一律放外部测试包）。
- **验收**：`go list -f '{{.TestImports}}' ./tools/shell` 不再含 `readline`（外部测试包按 `XTestImports` 单独报告）；`-race` 全绿；三个真 pty 用例在 `script -qec` 下实跑通过（`TTY_E2E=1` 的 E2E 与 cwd 用例、`TTY_E2E_REUSE=1` 的复用用例输出 `got:hello` / `got:world`）。

## 3 决策记录

| # | 决策 | 理由 |
|---|---|---|
| D1 | **撤回**评估首轮对「`tools/shell` 直连 `ctty` 违反注入原则」的定性；只改测试边 + 文档正名 | `ctty` fan-out 0，直连不可能成环；注入约定针对的是**终端所有权**（`Console`/`Lease`），而它确实是注入的；真正的环风险来自 `tools/shell` 的内部**测试包**导入 `readline` |
| D2 | resize **收敛进 `ctty` 的现有信号分发器**，不引入轮询、不单独处理 | 信号已收敛在 `ctty`（`SIGTERM`/`SIGHUP`/`SIGINT` 同源同分发器）；同一件事不该有第二套机制。轮询只是"能在没有信号面的平台上工作"的替代品，而 Windows 本就不产 `SIGWINCH`——那里保持不产出、不假装支持 |
| D3 | **本次不做**「`repl` 收窄为编排层」（`streams`/`status`/`toolview`/`messages` 移入 `present`） | 该拆分解决的是"体积与职责"，而 S5 已消掉 `repl` 的**入边耦合**（不再认识具体工具）；~1500 行机械搬迁 + 测试跟随的成本换不到新的依赖边改善。留待将来 `repl` 再长出第二个消费者时重估 |
| D4 | 全局 `Profile` **删除**，不保留"便利入口" | 保留即为隐式参数留后门；`Style.With(prof)` 的显式形式成本仅一次调用点改写，而它同时解掉 S3 的注入需求 |

D4 附带判据——**「第二个消费者」出现的情形**（决定将来是否有人会把全局加回来）：

| 情形 | 触发 | 全局为何不成立 |
|---|---|---|
| 第二条输出通道 | out/err 现在同终端（能力同）；一旦出现 `--output` 写文件、tee 日志、把内容交给父代理/插件，那条通道要 `LevelNone` 而 stderr 可能仍要彩色 | 全局只有一个值，**表达不了两个能力**（不是不够用，是形式错） |
| tanya 作库被嵌入 | `agent` 已是库；`repl`/`readline`/`render` 被宿主复用时，宿主有自己的探测与 `NO_COLOR` 取向 | 宿主与 tanya 冲突时必有一个被破坏 |
| 测试并行 | 全仓 `t.Parallel()` 当前为 0；引入后全局即数据竞争 | 共享可变状态，且 60 余处 save/restore 样板本身说明"每个用例都是一个消费者" |
| 运行期改值 | `/colors` 之类运行期切换 | 需要的是"可变持有点"而非全局——`/theme` 已是这个形态（`repl.applyTheme` 改实例字段），profile 走全局属同一包内的不对称 |

结论：全局唯一成立的场景是"进程内单一终端 + 只在启动时判定一次 + 所有消费者接受隐式读取"；出现上表任一行，显式传值都是终态。**`term.Passthrough` 是这条的极端例证**：它把 profile 编进了函数语义（无入参），调用方无从注入——S2 一并改为 `Passthrough(prof, text)`。

## 4 统一验收清单（每阶段都跑）

```bash
gofmt -l .                                   # 必须为空
go build ./... && go vet ./...
go test ./... && go test -race ./...
GOOS=darwin go build ./... && GOOS=windows go build ./...
make build && python3 scripts/render_audit.py   # 15 PASS / 0 FAIL（渲染链路阶段的硬门）
```

依赖边断言（阶段专属）：

```bash
go list -deps ./readline | grep render/style    # S3 后必须为空
go list -deps ./repl | grep tools/shell         # S5 后必须为空
rg 'SetProfile|GetProfile' -g '*.go' .          # S2 后必须为空
rg 'os/signal' -g '*.go' . | grep -v '^./ctty/' # S4 后必须为空
rg 'EventHangup' -g '*.go' .                    # S4 后必须为空
```

## 5 遗留与不做

- `repl` 的体量（3578 行）不属本次范围，见 D3。
- `render/term` 与 `ctty` 的边界保持现状：前者是**输出侧原语**（宽度/清洗/控制序列），后者是**控制终端原语**（termios/尺寸/信号/代码页）；两侧都只放原语、不放策略，与 `docs/ctty.md`《设计原则》一致。
- Windows 侧 resize 事件不产出（无 `SIGWINCH`）：`repl` 的活取宽度仍会在下次渲染时读到新尺寸，但 picker 的 `p.size` 是打开时快照、只在 `EventResize` 时更新——Windows 会话内缩放不自愈重排（S4 前的每轮 `Size()` 比对兜底已随事件化移除，与 D2「不假装支持」一致，行为退化记档于此）；如需实时化，属独立的 Windows 实机课题。
