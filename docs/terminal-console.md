# 终端控制台层：唯一持有者、租约与事件归一

状态：**规划**（2026-09-26 立项，同日修订：正名 `Console`；定稿三层分层；裁决**砍掉前台组直通与 `^Z` 子进程挂起检测**，见 §6/§8）。实施完成后与 `docs/ctty.md` 联动（本文件描述控制台层的职责与搬迁路径，`ctty.md` 继续描述平台原语清单）。

## 1 问题

### 1.1 四份「终端模式」定义（同一概念的副本）

| 位置 | 内容 |
|---|---|
| `readline/terminal_posix.go:42-47` | raw：清 `IGNBRK/BRKINT/PARMRK/ISTRIP/INLCR/IGNCR/ICRNL/IXON`、清 `ECHO/ICANON/ISIG/IEXTEN`、清 `OPOST`、`VMIN=0/VTIME=1` |
| `readline/bridge_linux.go:99-106` | raw：与上几乎逐行相同，仅 `VMIN=1/VTIME=0` |
| `readline/secure.go:39-45` | sane：置回 `ICRNL/IXON/ISIG/ICANON/ECHO/IEXTEN/OPOST/ONLCR` |
| `ctty/signals_posix.go:15-33` | sane：`emergencyRestore` 里同一组位再次手写 |

另有 Windows 一份：`readline/terminal_windows.go:48-62` 的 console mode 组合（`ENABLE_*`）。

即 `ctty` 只提供「读写 termios 位」的原语，**模式的构造（策略）被复制到了每个消费点**；再要一个新组合（如逐键读）就得写第五份，并逐处论证差异。

### 1.2 四个输入消费者，各自读同一块键盘

| 消费者 | 位置 | 何时读 |
|---|---|---|
| 编辑器 | `readline/editor.go:88-110` | 提示符期（自带 `Raw`/`Restore`） |
| 会话选择器 | `repl/picker.go:132-152` | `/load` 无参 |
| pty 桥接泵 | `readline/bridge_linux.go:218-237` | `interactive` 工具执行期（真实 tty 独占） |
| 前台子进程 | `tools/shell/shell.go:337,356` | 任意 `run_shell` 执行期（`cmd.Stdin = tty` + 前台组移交） |

没有任何仲裁：谁先 `read` 谁先拿到字节；谁在读、别人能不能读，全靠调用点各自安排。

### 1.3 `^C` 两套语义、四条路径

| 路径 | 位置 | 机制 |
|---|---|---|
| 编辑器 | `readline/terminal_posix.go`（清 `ISIG`）+ `readline/editor.go:213` | 读到字节 `0x03` → `ErrInterrupt` |
| 回合期 | `ctty/signals_posix.go`（`interruptSignals`）+ `repl.InterruptContext` | 内核投 SIGINT → `ctty.Interrupted()` → 取消请求 |
| 子进程（前台） | `tools/shell/shell.go:356` | 前台组交给子进程，内核直接把 `^C` 投给它 |
| 子进程（桥接） | `readline/bridge_linux.go:218-237` | `0x03` 经 pty 泵透传，由子进程所在的 pty 行规程投 SIGINT |

同一个用户动作，语义取决于「此刻谁持终端 + termios 哪几位」。任何新需求（如流式期读键）都要先回答这两个问题才能动手——这是此前方案复杂度失控的根因，也是本文件立项的原因。

### 1.4 初版接口草案的分层缺陷（修订记录）

初版草案（本文件 2026-09-26 第一版）把仲裁层直接坐在 `ctty` 上、`Lease` 按**机制**设计，评估认定无法屏蔽平台与设备差异：

- 接口签名暴露判据说要屏蔽的概念：`Enter(Mode)`（termios 组合名）、`TransferForeground`（posix 专属）、`MaskCtrl`（Windows 专属）——两个单平台方法互为 no-op 补丁；
- `^C` 归一规则按 posix 机制（字节 vs 信号）书写，Windows 的 ctrl 事件路径无对应规则，归一逻辑必然平台分叉；
- `Resize`/`Hangup` 承诺了 Windows 产不出的来源（无 `SIGWINCH`，`ReadConsoleInput` 与 VT 字节读互斥）；
- Degraded（管道/CI/全部单测环境）无位置：`NewTerminal() (Terminal, bool)` 的 bool、`editor.raw` 分支这类设备差异泄漏会被原样带进新层。

修正即本文件现状：三层分层（§5）+ 借出按意图分型 + 前台组退出设计（§4/§6）。

## 2 目标

一句话：**进程内只有一个终端持有者（Console）；它决定当前模式、`^C` 归属与事件产出；消费者只读事件流或借出终端。**

判据（接入方不再需要回答的问题）：

1. 不需要知道 `ISIG` / `0x03` / termios 位 / `IgnoreCtrlEvents` 的存在（前台组概念整体退出设计）。
2. 不需要知道「现在有没有子进程在跑、我能不能读键盘」——让出与收回由 Console 按持有者切换自动完成。
3. 平台差异（posix/windows）与设备差异（tty/管道）只存在于 device 分片内部；消费者代码各形态一致。

## 3 概念

| 概念 | 说明 |
|---|---|
| 控制台（Console） | 进程内唯一终端持有者，启动时创建、进程存活期内常驻 |
| 租约（Lease） | 终端借出句柄，两型意图：**LendStdin**（子进程吃 stdin，普通 `run_shell`）与 **LendFull**（子进程独占整个终端，`interactive`） |
| 模式（Mode） | device 内部命名模式（位组合单点定义），不对消费者暴露 |
| 事件（Event） | `Key` / `Interrupt` / `Resize` / `Hangup`（后两者按 device 能力可选） |

命名说明：定名 `Console`（控制台）。该层并无「会话」语义——无起止、无轮转、进程存活期常驻单例；曾拟名 `Session`，与 agent 侧会话（会话存储、归档会话、`/load` 会话）概念冲突，废止。

模式表（device 内部位组合的定义点唯一，不跨层暴露）：

| Mode | 位组合 | 谁用 | `^C` 归属 |
|---|---|---|---|
| `Cooked` | 全默认（内核行规程生效） | 无租约空转、普通 `run_shell` 执行期 | Console（信号路径） |
| `Keys` | 关 `ICANON/ECHO/IEXTEN/IXON`，关 `ISIG`，`VMIN=0/VTIME=1` | 编辑器、选择器、流式期监听 | **Console**：读到 `0x03` 归一为 `Interrupt` 事件 |
| `Pump` | 关 `ICANON/ECHO/IEXTEN/IXON/ISIG`，`VMIN=1/VTIME=0`，字节全透传 | LendFull 的 pty 泵（posix） | 子进程（`0x03` 原样透传） |
| `Sane` | 置回 `ICRNL/IXON/ISIG/ICANON/ECHO/IEXTEN/OPOST/ONLCR` | 自愈、紧急复原 | Console |

**`Keys` 与 `Pump` 都关 `ISIG`**：tanya 持有期间的 `^C` 一律由 device 从字节归一，**不存在「保留信号能力的中间模式」**（前期讨论里自造的「半 raw」概念作废）。

## 4 `^C` 归属规则（两条）

1. **tanya 持有（Reader 活跃或无租约——含普通 `run_shell` 执行期）**：`^C` 归一为 `Interrupt` 事件，device 内汇成单出口（`Keys` 下从字节 `0x03`，`Cooked`/外部 `kill -INT`/Windows ctrl 事件从信号面）。普通 `run_shell` 期间 tanya 恒前台，`^C` = 中断回合并杀子进程组（行为变化，§8）。
2. **借出（LendFull）**：`^C` 归子进程——posix：pty 行规程投递；Windows：掩蔽后子进程独占（Ctrl+Break 保留为逃生口）。tanya 不产 `Interrupt`。

退出信号（SIGTERM/SIGHUP）不随租约变化，仍由 `ctty` 现有信号面处理。`^Z`（SIGTSTP）：tanya 自身维持吞没、全面无响应（§8）。

## 5 分层与接口（伪签名，实施时定稿）

```
L3 消费者    editor / picker / repl.InterruptContext / tools/shell（经注入接口）
L2 Console   仲裁：单读者调度、租约状态机、事件分发；无 tag 纯逻辑，fake device 可全量单测
L1 device    设备面（readline 包内私有）：每平台×设备一份完整实现
             posixTTY / windowsConsole / pipe（stub 平台与 pipe 同型）
L0 ctty      syscall 原语（不改动；不加模式常量，位组合收在 posix device 内）
```

L2 对消费者的面（`tools/shell` 经注入接口消费，与现有 `Bridge` 同法避免 `tools` → `readline` 依赖）：

```go
type Event struct {
    Kind EventKind // EventKey / EventInterrupt / EventResize / EventHangup
    Key  KeyEvent
}

type Console interface {
    ReadEvent() (Event, error)                 // Reader：同步拉取（编辑器/选择器循环）
    Subscribe(fn func(Event)) (cancel func())  // 后台订阅（InterruptContext、流式期监听）
    LendStdin() (Lease, error)                 // 普通工具：子进程 stdin=/dev/null，不直通控制终端
    LendFull(capture io.Writer) (Lease, error) // interactive：posix pty 泵+输出捕获 / windows 直通+掩蔽
}

type Lease interface {
    Stdin() *os.File // 交给子进程的 stdin（LendFull posix: pty slave；windows: CONIN$；LendStdin: /dev/null）
    Release()        // 复原模式、光标锚点、泵停——机制全在 device
}
```

L1 device 职责（接口包内私有，各分片一份完整实现）：模式切换、单读者读 + 唤醒、**中断归一**（`0x03` 字节与信号面汇成同一通知）、`Resize`/`Hangup` 产出（能力可选，产不出就是没有该事件）、`LendStdin`/`LendFull` 的机制实现、紧急复原。读循环只在 Reader 活跃期存在（空转期读会抢走子进程输入），唤醒用现有机制收敛（编辑器 `VMIN=0/VTIME=1` 轮询、桥接 wake pipe，二者归一为 device 内部实现细节）。

## 6 搬迁与下线清单

搬迁：

| 现状 | 目标 |
|---|---|
| `readline/terminal_posix.go` 的位组合 | posix device（`ctty` 不加模式常量） |
| `readline/bridge_linux.go` 的泵/`Pipe2`/`SetNonblock`/`TIOCGWINSZ`/`SIGWINCH` | posix device 的 LendFull + `Resize` |
| `readline/secure.go` 的 sane 位 | posix device（`Sane` 单点） |
| `readline/terminal_windows.go` console mode 位 | windows device |
| `Terminal` 接口 + `Degraded` 双实现 | device 三实现（posixTTY/windowsConsole/pipe）；`NewTerminal() (Terminal, bool)` 与 `editor.raw` 分支消失 |
| `repl/picker.go` 的 `Raw`/`Restore`/`ReadKey`/`Size` | `ReadEvent` 循环 + 按键时轮询 `Size`（不依赖 EventResize） |
| `repl.InterruptContext`（订阅 `ctty.Interrupted`） | `Subscribe(EventInterrupt)`；「同步取快照」竞态语义保持 |
| `repl/repl.go` 的 `readline.SecureTerminal()` | Console 自愈（`Sane`，纯模式复原） |
| `readline/editor.go` 的 `Raw`/`Restore`/`ReadKey` | `ReadEvent` 循环 |
| `tools/shell` 的借出链（快照/锚点/复位/掩蔽） | `LendStdin`/`LendFull` → `Lease`（时序契约进 device，保序搬迁） |

下线（2026-09-26 裁决）：

| 现状 | 处置 |
|---|---|
| `ctty` 的 `OwnPgrp`/`ForegroundPgrp`/`SetForeground`/`IsForeground` | 删除——前台组机制整体退出设计 |
| `readline/secure.go` 的 `InitTerminalGuard`/`SecureTerminal` 抢回 | 删除（tanya 恒前台，无需抢回；自愈收敛为纯 termios 复原） |
| `runForeground` 的 stdin=tty 直通 + 前台移交/归还 + `isForeground` 门控 | `LendStdin` = stdin `/dev/null`；移交/归还链删除 |
| `waitShell` 挂起轮询 + `ProcessStopped`（linux/darwin 分片）+ `Stopped` 字段 + `MsgStopped` | 删除——不做 `^Z` 子进程支持；interactive 内子进程被停即等超时强杀 |
| `posixCapabilities` 文案「sudo/ssh 提示写入控制终端可直接应答」 | 改为「交互式程序需 `interactive: true`」 |
| `EventSuspend`/`EventExit`（初版概念） | 不设（前者无消费者，后者走 `ctty` 信号面） |
| `emergencyRestore` 的 `IsForeground` 门控 | 简化为无条件复原（tanya 恒前台） |

保留不动：`Setpgid` + `KillGroup` 树杀（与前台组无关）、`ProtectJobSignals`（SIGTSTP 吞没 + SIGTTIN/TTOU 忽略，自保）、`IgnoreCtrlEvents`（Windows LendFull 用）。

## 7 平台

- **posix/linux**：device 完整——`Keys`/`Sane`/`Pump`、中断归一、`Resize`=SIGWINCH、`Hangup`=POLLHUP、LendFull=pty 泵。
- **posix/darwin**：同 linux，但 LendFull 不支持（不做 pty 泵；interactive 在 darwin 明确不支持，工具侧报错）。
- **windows**：`Keys`=console mode 映射（**保留 `ENABLE_PROCESSED_INPUT`**，`^C` 走 ctrl 事件）、中断=信号面汇合、`Resize`/`Hangup` 暂缺（picker 轮询 `Size` 兜底）、LendFull=`CONIN$` 直通+掩蔽（capture 不支持，输出直上屏）、LendStdin=null 且**不掩蔽**（`^C` 双方收到，等价中断回合）。未实机验证，验收底线=与现状行为一致。
- **pipe**（全部非 tty 环境：CI/重定向/全部单测/stub 平台）：读=合成整行 `Key` 事件、无模式、无 `Resize`/`Hangup`、中断走信号面——device 一等实现，非降级分支。

## 8 行为变化（S5 落地，独立验收、独立提交）

| # | 变化 | 说明 |
|---|---|---|
| 1 | sudo/ssh/gpg 等隐式交互消失 | 必须 `interactive: true`；漏标则命令读 tty 失败或立即 EOF；工具描述文案同步改（§6） |
| 2 | 裸读 stdin 的命令（`cat`、`read`） | stdin=`/dev/null`，立即 EOF |
| 3 | 普通 `run_shell` 执行期 `^C` | 从「只中断该命令、工具返回、回合继续」变为「中断整个回合并杀子进程组」（§4 规则 1） |
| 4 | `^Z` 全面无响应 | tanya 自身维持吞没；interactive 子进程被停不再检测，等超时强杀 |
| 5 | darwin interactive 不支持 | 工具侧明确报错（原为前台直通兜底） |
| 6 | 前台被外部抢占不再自愈抢回 | 罕见（tanya 吞 SIGTSTP 停不了）；SIGTTIN/TTOU 仍忽略 |

## 9 分阶段（每阶段独立提交、独立验收）

| 阶段 | 内容 | 验收 |
|---|---|---|
| S1 | device 正规化：`Terminal`+`Degraded` → device 三实现，位组合收进 posix device；消费者面不动 | 行为不变；`stty -a` 前后一致；`-race` 全绿 |
| S2 | Console 仲裁层 + 事件归一；编辑器/选择器改 `ReadEvent` 循环 | 编辑器/选择器行为不变（`^C` 中断、`^D` EOF）；fake device 单测落地 |
| S3 | `InterruptContext` 改 `Subscribe` | 回合中断不变；「同步取快照」竞态语义保持；`-race` 并发触发用例 |
| S4 | `tools/shell` 接 `Lease`（LendStdin 暂按现状直通、LendFull 收编 pty 泵）——纯结构搬迁 | 行为不变；`render_audit` 探针全绿 |
| S5 | 直通与 `^Z` 检测下线：§6 下线表 + §8 行为变化全部落地（含文案） | sudo/ssh 走 interactive 实机；执行期 `^C` 中断回合；超时强杀无残留；`stty` 无残留 |
| S6 | 清理 | 前台组原语/`SecureTerminal`/`ProcessStopped` 全仓零残留；非 Console 代码不直接调 termios/模式原语 |
| S7 | 文档同步（`AGENTS.md`/`docs/design.md`/`docs/ctty.md`/`README.md`/`CHANGELOG.md`） | — |

## 10 回归与实机清单（每次改动后执行）

- `go build ./... && go vet ./... && go test ./... && go test -race ./...`
- `python3 scripts/render_audit.py`（pty + VT 回放）
- `make build` 后实机（**不用 `go run`**）：`^C` 中断回合、`^D` 退出、interactive 桥接打字/`^C` 透传/resize、`sudo`/`ssh` 走 interactive 应答、普通命令执行期 `^C` 中断回合并杀组、退出后 `stty -a` 无残留、`^Z` 无响应不挂死。

## 11 明确不做

- 前台组（`TIOCSPGRP` 移交/抢回/检测）——已裁决退出设计。
- `^Z` 子进程挂起检测与恢复。
- Windows ConPTY、darwin pty 泵。
- 通用事件总线 / TUI 框架 / 插件机制。
- 把 `agent` 卷进来（保持零内部依赖）。
- 同一时刻两个持有者（冲突由租约排队解决）。
