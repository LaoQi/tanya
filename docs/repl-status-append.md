# repl 状态展示追加化：替代 spinner 的方案

状态：**已实施**（2026-09-15，含 2026-09-16 行内点累加修订）。落地偏差见文末《落地记录》。

## 背景

`repl/spinner.go` 的等待动画与 `repl/toolview.go` 的 inline 工具块收尾，都建立在同一套**原地重绘**协议上：

- 帧：`\r\x1b[K` + 文本（`term.ClearLineHome`），100ms 一次；
- 收尾：`\x1b[1A` 上移标题行数后重写（`RenderToolEndInline`，行数由 `toolTitleLineCount` 算出）。

协议隐含四条前提，任一条被破就错乱：

| # | 前提 | 破法 |
|---|---|---|
| P1 | 屏幕只有 tanya 在写 | 子进程/第三方写 `/dev/tty`（sudo/ssh/gpg/vim/stty/`> /dev/tty`）|
| P2 | 光标总在"当前行"、上移 n 行能回到标题行 | 外部字节换行/滚动/移动光标 |
| P3 | Start 与 End 之间标题行数不变 | resize 改变折行 |
| P4 | `stop()` 必定清干净 | 200ms 超时分支（`spinner.go:100`）不清行，残留帧使下次上移错位 |
| P5 | 终端不回看重排 | 无 SIGWINCH 处理（仅 bridge 用它转发 pty 尺寸）|

实证（pty + VT100 播放，命令执行 1s）：

- `sleep 1`：正常；
- `printf 'SIDE-EFFECT\n' > /dev/tty; sleep 1`：外部那行**被帧擦掉**，工具块收尾错位、标题残留两行；
- `printf '[sudo] password for user: ' > /dev/tty; sleep 1`：密码提示**完全消失**（用户盲输），修复 `resetModes` 前同样如此——与 `CSI r` 问题同源，是"让出终端的窗口里 tanya 仍在写"。

`run_shell` 的常规路径必然让出终端（`agent/shell.go:304` 子进程 stdin 直通 `/dev/tty`、`:319` 移交前台组），所以 P1/P2 在日常用法里就会破。interactive 路径早就承认这一点（不启帧、收尾追加式，`toolview.go:366,378`），常规路径没有。

## 目标

- 状态展示不再依赖光标位置：**只追加，不重绘、不清行、不上移**。
- 让出终端、第三方写、resize、并发写下的输出**不发生覆盖与错位**（顺序变化可接受）。
- 保留"在等 / 在跑"的可感知性，以及已有的计时信息（TTFT、工具耗时）。
- 净减代码：删掉整套重绘机制。

非目标：全屏/TUI 状态栏、alternate screen、把状态写进 readline 提示符。

## 方案

### 状态矩阵（rich + TTY）

| 时机 | 输出 | Kind | 备注 |
|---|---|---|---|
| `EventRequestStart` | `» 等待响应` | `KindStatus` | 阶段起始行，不带点 |
| 等待期每 10s | `» 等待响应 .` | `KindStatus` | 心跳，换行追加，每行一个点 |
| `EventResponse` | `  ↳ TTFT 30.4s · 3.2s · prompt …` | `KindToolStatus` | 现状不变 |
| `EventToolStart`（常规）| `▸ run_shell ls -la` | `KindToolBlock` | 去掉 `⋯` 标记 |
| 执行期每 10s | `» 执行中 .` | `KindStatus` | 心跳，换行追加，每行一个点；interactive 不启 |
| `EventToolEnd`（常规）| 正文块 + `  ↳ exit 0 · 12ms · 8 行` | `KindToolBlock` | `RenderToolEndAppend` |
| `EventToolStart`（interactive）| 标题 + `  ⏎ 等待终端输入…` | `KindToolBlock` | 现状不变 |
| `EventToolEnd`（interactive）| `\n` + 正文块 + 状态行 | `KindToolBlock` | 追加式，**标题只出现一次** |

渲染样例：

```
» 等待响应
» 等待响应 .
» 等待响应 .
␣ ...（每 10s 一行，行数即时间刻度）
  ↳ TTFT 30.4s · 3.2s · prompt 12.3k · completion 1.2k
▸ run_shell sleep 25
» 执行中 .
» 执行中 .
  ↳ exit 0 · 25.1s
```

### 心跳规则

- 前缀：`»`（U+00BB，Latin-1 单宽、终端字体普遍覆盖，且不与现有符号冲突——`▸` 属工具标题、`↳` 属状态行、`>` 属提示符），等待期 `» 等待响应`、执行期 `» 执行中`。
- 触发阶段：请求等待期（`EventRequestStart` → 首个 content / `EventResponse` / 回合结束）与工具执行期（`EventToolStart` → `EventToolEnd`，非 interactive），两阶段同一机制、不同前缀。
- 周期：`statusHeartbeatInterval = 10 * time.Second`（常量，不新增配置项）。
- 形式：**每次心跳换一行**（纯追加），行尾固定一个点 `.`——点数在每次换行后**重新开始计数**，不跨行累计，因此每行都是单点；时间刻度由**行数**表达。
- 起始行不带点（`» 等待响应`），工具执行期不额外打起始行（工具标题行 `▸ …` 已是起始）。
- 停止：`close(stopCh)`，**不写任何字节**——没有"清行"需求，P4 类风险随之消失；等待退出沿用有界超时，超时最多多/少一行，无破坏性。
- 并发：心跳 goroutine 只 `out.emit(KindStatus, …)`，与全部输出共享 `output.mu`，不会撕裂其它写入。

### 门禁

- `KindSpinner` 改名为 `KindStatus`（语义：过程状态行）。
- 可见集不变：rich 全见；`plain` = Content/Notice；`plain+verbose` 再加 ToolBlock/ToolStatus → **`KindStatus` 只在 rich 出现**，`-p`、`-p --verbose`（含 `ask` 单发默认档）与现状一致，无等待行、无心跳。
- 非 TTY：`prof.TTY` 为假时不打等待行与心跳（与现状一致）。

### 工具块收尾统一

- `ToolEnd` 的三种分派（inline / interactive / append）合并为一种：`RenderToolEndAppend`。
- interactive 保留"重新起头"语义：`"\n" + RenderToolEndAppend(...)`；**标题不再重复**（只由 `ToolStart` 打一次）。
- `RenderToolStart` 去掉 `⋯`；`toolTitleText` 与 `RenderToolStart` 合并为一个无 mark 的函数。

### 删除清单

| 项 | 位置 |
|---|---|
| spinner 全部 | `repl/spinner.go`（含 `spinnerFrames`/`spinKind`/`spinElapsed`/`spinnerStopTimeout`）|
| `sp *spinner`、`animate()`、`setKind` 分支 | `repl/toolview.go` |
| `RenderToolEndInline`、`RenderToolEnd`、`toolTitleLineCount`、`toolTitleLines` 的 `mark` 参数 | `repl/toolview.go` |
| `KindSpinner` → `KindStatus` | `repl/flow.go`、`repl/streams.go`（`allKinds`）|
| `outMode.cursor()` / `streams.cursor()`（inline 删除后无使用者）| `repl/streams.go` |
| 文案 `SpinWaiting`/`SpinThinking`/`SpinRunning` → `StatusWaiting`/`StatusToolRunning` | `repl/messages.go` |
| `repl/spinner_test.go` | 删除 |

### 不受影响

- readline 输入期的一切重绘（提示符多行布局、ghost、补全菜单、Ctrl+L、picker）——输入期 tanya 独占终端（工具执行期间编辑器不在读键），前提成立，本次不动。
- `agent` 侧：终端让出、termios 复原、`ResetModes`（当时不含 `CSI r`；2026-09-16 随光标锚点方案回归，见 `docs/ctty.md`）均不变。

## 实施步骤

1. **状态行落地**：新增 `KindStatus`；`EventRequestStart` 打起始行；删 spinner 帧循环（保留工具块现状）。
   验证：`go test ./repl/`；pty 复现 B/C 场景，外部字节不再被擦（工具执行期 tanya 只在心跳时刻写整行）。
2. **心跳**：等待期与执行期各起一个 10s ticker，按点符号规则追加。
   验证：`go test ./repl/`（注入短间隔）；pty 跑 `sleep 25`，肉眼核对三行心跳与结束状态行。
3. **收尾统一 + 清理**：删 inline 分支与相关函数，ToolEnd 一律 append（interactive 前缀 `\n`）；删 `cursor()`、`spinner_test.go`；文档同步。
   验证：`go build ./... && go vet ./... && go test ./... && go test -race ./repl/`；pty 各跑一次常规与 interactive，标题各只出现一次。

每步独立可构建、可测试；步骤 1/2 的长命令验证用现有 mock LLM + pty 驱动脚本。

## 测试计划

- 新增：状态行与心跳输出**不含**任何光标控制序列（`\x1b[1A`/`\r`）；心跳用可注入的短间隔（10ms）断言"行数正确、每行单点、换行后重新计数"；等待期与执行期两套前缀。
- 改写：`repl/mode_test.go`（3 处 `KindSpinner` → `KindStatus`）、`repl/streams_test.go`（原子块插入源由 spinner 帧改为状态行）、`repl/toolview_test.go`（`等待响应`/`执行中` 断言，`RenderToolEnd*` 用例迁移到 `RenderToolStart + RenderToolEndAppend`）。
- 删除：`repl/spinner_test.go`。

## 文档同步

- `docs/design.md`：§41（ask 单发）、§153-154（工具块收尾三分派 → 一种）、§158-162（《等待动画与请求状态》→ 重写为《状态行》）、§221（流分配去 spinner 帧）、§225（plain 六条语义）。
- `README.md:130`（TTY 行为段）、`AGENTS.md:30`（`repl` 包描述）、本文档登记到 `docs/design.md`《文档》索引与 `docs/repl-output-refactor.md` 附录 C 变更登记。

## 取舍（明确放弃的东西）

- 无动画：等待/执行期只在每 10s 多一行心跳（每行一个点，行数表示刻度）。
- 无原地变化：`⋯` 进行中标记消失，标题与结果分离（各一次）。
- 心跳行留在回滚区（追加语义的必然结果）；行数越多占屏越多，长任务可考虑调大周期。
- ~~心跳不区分"等待响应 / 思考中 / 生成中"~~（**2026-09-16 修正**："追加语义下改不了行"不成立——收尾当前行再用新前缀开新行即可换相位，工具相位本来就是这么做的。思考相位已按此回补，见《变更》小节；"生成中"仍旧并入等待响应，因为首个 content 到达即停心跳）。

## 已确认决策（2026-09-15）

1. 等待期与执行期**都**发心跳，机制统一，只有前缀不同。
2. 心跳**每 10s 换一行**，行尾**固定一个点**；点数每次换行重新计数（不跨行累计），时间刻度由行数表达。
3. 前缀由 `⠋` 换成 `»`（U+00BB）。
4. 工具收尾**标题只出现一次**（interactive 亦不再重复标题）。

## 变更：行内点累加 + 行首秒数（2026-09-16）

上一节的"每 10s 换一行、每行固定一个点"改为"一行内每秒追加一个点、满 10 个点换行"：

- 行首前缀只在开行时写一次，含该行起始秒数（`» 等待响应 0s ` / `» 等待响应 10s ` / `» 执行中 0s `，末尾固定一个空格位，点不与秒数粘连），写入后固定不变；行内每秒追加一个点，满 `statusLineSpan = 10` 个点即换行并以当时秒数开新行。
- 追加语义不变：依旧零光标控制、零清行，已写字节不回头修改；`stop()` 由"不写字节"改为"未收尾的当前行补一个换行"，未起心跳时仍不写字节（幂等）。
- 节奏不变：`statusTickInterval = 1s`（新）、每行 10s，行密度与旧实现一致（6 行/分钟）。
- 秒数口径：**墙钟**（`time.Since(started)` 的整秒），开行时写定——进程被挂起/系统睡眠后恢复，秒数反映真实等待时长（点与换行仍由 ticker 驱动，因此可能出现"上一行 `0s`、停摆后下一行 `3m10s`"的跳变，点数不变）；测试经 `heartbeat.now` 注入假时钟，确定性不受影响。
- 思考相位回补（2026-09-16）：`EventReasoning` 接回 `heartbeat.setPhase(statusThinking)`——`» 思考中`（`Think` 色）与等待相位只差前缀，秒数沿用同一 `started`、点数归零；`setPhase` 只在"心跳在跑且相位变化"时动作，逐 token 的重复事件与 content 之后的零星 reasoning 均 no-op，故每请求最多多一行。旧 spinner 的三相位（等待/思考/执行）由此补齐，`/theme` 图例里的"思考中"重新名副其实。
- 点与行首同色：每 tick 的 `.` 由该阶段语义色（等待 `Warn` / 思考 `Think` / 执行 `Run`）单独 `Style.Sprint` 包裹，点自带 `term.Reset`——行不会停在着色态，子进程/第三方写 `/dev/tty` 不继承 tanya 颜色；代价约 10 字节/秒。
- 门禁不变：`KindStatus` 仅 rich + TTY；plain / plain+verbose / 非 TTY / `ask` 单发 / interactive 均无变化。
- 顺带修一处既有漏洞：等待期被打断（无 content、无工具事件）时没有任何停止点，心跳会一直写到下一次请求；现由 `turn.End` 调 `toolView.Stop()` 收口。

## 变更：执行相位前缀去掉前导空格（2026-09-16）

`MsgStatusRunning` 由 `"  » 执行中"` 改为 `"» 执行中"`：前导两空格是 braille spinner 时代的遗留——当时执行相位 spinner 画在工具块内部，靠这两格与正文的 2 列缩进对齐；追加化后三个相位都由同一个心跳在行首写出，等待/思考相位贴左、执行相位独自缩进两格反而不齐。工具块自身的缩进不受影响（标题 `▸ `、正文 `  `、结束状态行 `  ↳ ` 各有自己的前缀），执行相位心跳改为与其余相位同列起行。

同步：`repl/messages.go`、`README.md`（TTY 状态展示段）、`docs/design.md`（《状态行与心跳》）、`scripts/repl_tty_check.sh`（目视校验第 3 条）。`repl/status_test.go`、`repl/toolview_test.go` 的断言全部引用常量，无字面量改动。

## 落地记录（2026-09-15）

> 本节记录首次落地时的形态；其中"心跳每 10s 换一行、每行一个点"与实测输出已被上文《变更：行内点累加 + 行首秒数（2026-09-16）》取代（现为每秒一个点、满 10 点换行），其余落地项仍有效。

- `repl/spinner.go` 删除，新增 `repl/status.go`：`statusKind`（`statusWaiting`/`statusToolRunning`）、`statusLine()`、`heartbeat`（`start/stop/setSemantics`，`interval` 可注入供测试）。
- `KindSpinner` → `KindStatus`；可见集不变（rich 全见，plain 两档不含）。
- 工具收尾三分派合并为 `RenderToolEndAppend`；`RenderToolEnd`/`RenderToolEndInline`/`toolTitleLineCount`/`toolTitleText` 删除，`RenderToolStart` 去掉 `⋯` 与 `mark` 参数。
- `outMode.cursor()`/`streams.cursor()` 删除（inline 重绘消失后无使用者）。
- 文案：`MsgStatusWaiting = "» 等待响应"`、`MsgStatusRunning = "  » 执行中"`（前者为当时的形态，执行相位前导两空格已于 2026-09-16 去掉，见上文《变更：执行相位前缀去掉前导空格》）。
- 测试：`spinner_test.go` 删除；新增 `repl/status_test.go`（状态行无光标控制、禁用不输出、停止后补换行收尾、行内点累加与行首秒数）；`toolview_test.go` 三处断言按新语义改写（含 `renderToolEnd` 测试辅助与"标题只出现一次"）、`mode_test.go`/`streams_test.go` 相应更新（并发插入源由 spinner 帧改为状态行）。
- 端到端实测（mock LLM + pty + VT100 播放）：`sleep 25` → 执行期心跳行（形态见上文 2026-09-16 变更）后接 `↳ exit 0 · 25.0s`，输出中 `CSI 1A` 出现 0 次；子进程直写 `/dev/tty` 的 `SIDE-EFFECT` 与 `sudo password: ` 提示**完整保留**（追加化前被帧清行抹掉）。
