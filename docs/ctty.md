# ctty：控制终端统一抽象与平台收敛

## 背景

`agent` 与 `readline` 各自实现同一组"控制终端（controlling tty）原语"：

| 原语 | agent | readline |
|---|---|---|
| 取自身进程组 | `shell_tty_unix.go` `ownPgrp`（`Getpgid(0)`）| `secure.go`/`bridge_linux.go`（`Getpgrp()`）|
| 读前台组 `TIOCGPGRP` | `handoverForeground` | `InitTerminalGuard`/`secureTerminalFd`/`foregroundTTY` |
| 写前台组 `TIOCSPGRP` | `handoverForeground`/`restoreForeground` | `secureTerminalFd` |
| 打开 `/dev/tty` | `openForegroundTTY` | `secure.go` ×2、`bridge_linux.go` |
| 忽略 `SIGTTIN/SIGTTOU` | `ProtectTerminalSignals` | `InitTerminalGuard` |
| 平台能力常量 | `ttyStdinSupported()` | `newUnixTerminal` 成败 |

两包用各自独立维护的 tag 集合（`unix && !illumos && !ios` 等）表达同一概念，且无 CI 对照。跨平台编译矩阵实查出缺陷：

- **solaris**：`x/sys` 中 `Getpgrp()` 在 solaris 是唯一返回 `(int, error)` 的平台，`readline/secure.go` 按单值使用 → 编译失败（agent 侧因用 `Getpgid(0)` 而正常）。
- **plan9 / js / wasip1**：`terminal_windows.go` 只覆盖 windows，`terminal_unix_stub.go` 只覆盖 `illumos||ios`，`!unix && !windows` 无人提供 `newUnixTerminal` → 编译失败。

## 目标调整

平台目标收敛为：**Linux 与 Windows 为主**，**macOS 尽力**（原语齐备），其余平台**仅保证可编译**（走 stub）。平台分片统一改为**白名单**，不再枚举边缘平台：

| 角色 | tag |
|---|---|
| POSIX 真实实现 | `linux \|\| darwin` |
| 其余 stub | `!linux && !darwin`（含 windows 分片时追加 `&& !windows`）|

## 抽象

新增零内部依赖叶子包 `ctty`（仅依赖 `golang.org/x/sys/unix`）：

| API | 语义 |
|---|---|
| `const Supported bool` | 平台能力（linux/darwin 为真）|
| `Open() (*os.File, error)` | 打开控制终端 `/dev/tty` |
| `OwnPgrp() int` | 自身进程组（`Getpgid(0)`，错误 `-1`）|
| `ForegroundPgrp(fd int) (int, bool)` | 读前台组 |
| `SetForeground(fd, pgrp int) bool` | 写前台组 |
| `IsForeground(fd int) bool` | `ForegroundPgrp(fd) == OwnPgrp()` |
| `IgnoreJobSignals()` | `signal.Ignore(SIGTTIN, SIGTTOU)` |
| `type Termios` | 平台 termios（posix 为 `= unix.Termios`，其余平台空结构）|
| `GetTermios(fd) (Termios, error)` | 读 termios |
| `SetTermios(fd, Termios) error` | 写 termios |
| `SetTermiosFlush(fd, Termios) error` | 写 termios 并丢弃未读输入（raw 前用）|
| `IsTerminal(fd int) bool` | 是否终端：posix `GetTermios` 成功、windows `GetConsoleMode` 成功（2026-09-16 增补）|
| `Size(fd int) (int, int, bool)` | 终端尺寸：posix `TIOCGWINSZ`、windows `GetConsoleScreenBufferInfo` |
| `EnableVT(fd int) bool` | 确保 ANSI 输出可用：windows 幂等开 `ENABLE_VIRTUAL_TERMINAL_PROCESSING`，posix 恒真 |
| `ConsoleKind() string` | 控制台种类诊断串（windows: `WT_SESSION` → windows-terminal、`TERM_PROGRAM` → conpty，其余 console）|
| `ConsoleMode(fd) (uint32, bool)` | 读 Windows 控制台模式（2026-09-16 增补，windows 专属）|
| `SetConsoleMode(fd, mode) bool` | 写 Windows 控制台模式 |
| `ConsoleCP() (uint32, bool)` / `ConsoleOutputCP() (uint32, bool)` | 读控制台输入/输出代码页（windows 专属，2026-09-17 增补）|
| `SetConsoleCP(cp) bool` / `SetConsoleOutputCP(cp) bool` | 写代码页（LazyDLL，`x/sys/windows` 未封装这四个 + `GetOEMCP`/`MultiByteToWideChar`/`WideCharToMultiByte`，共 7 个 proc）|
| `OEMCP() uint32` | 系统 OEM 代码页（`GetOEMCP`，恒成功）|
| `EnsureUTF8()` | 启动期快照并把控制台双代码页切 65001（输入/输出两侧独立判定：各自探测成功且非 65001 才切，失败侧不动），幂等；由 `main` 在 `flag.Parse` 后单点调用 |
| `RestoreUTF8()` | 复原快照代码页并清零，幂等；`main` 以 defer + `exitNow` 收口全部退出路径，windows 紧急强退路径经 `emergencyRestore` 同样复原 |
| `FallbackCP() uint32` | run_shell 兜底转码源：优先启动快照的原输出代码页（子进程管道输出跟随控制台代码页），无快照（本就 65001 或无控制台）回落 `OEMCP()`；posix 恒 0 |
| `DecodeCP(cp uint32, b []byte) []byte` | 按 cp 转 UTF-8（`MultiByteToWideChar → WideCharToMultiByte(CP_UTF8)`，flags=0 不用 `MB_ERR_INVALID_CHARS`：非法/残缺字节落 U+FFFD 而非整体失败）；cp 为 0、空输入或转换失败原样返回；posix 恒等 |
| `Facts` / `Probe()` | 探测结果聚合（StdinTTY/StdoutTTY/Cols/Rows/SizeOK/VT/Kind），`main` 单点调用 |
| `ResetModes(tty *os.File) bool` | 复位终端模式：SGR、字符集（`SI` + G0/G1 回 ASCII）、显示光标、自动换行、origin 模式、普通方向键、鼠标上报、bracketed paste、focus 上报、退出备用屏、复位滚动区。顺序固定为「属性类复位（SGR/字符集/模式）→ `\x1b[?1049l` → `\x1b[r`」；这两条都会动光标（`DECRST 1049` 即使在主屏也按 DECRC 恢复保存槽、`CSI r`/DECSTBM 把光标 home），故**调用方必须先 `SaveCursor`、复位后 `RestoreCursor` 收尾**，串内不得自带 `DECSC`/`DECRC`（会覆盖调用方的存档槽）。2026-09-16 二次修订，取代此前「把会移光标的两条包在 `DECSC`…`DECRC` 内」的做法——实测证明自包只能保住「已被子进程打乱」的位置 |
| `SaveCursor(tty *os.File) bool` | 写 `DECSC`（`\x1b7`）：交出终端前存光标锚点（2026-09-16 增补）|
| `RestoreCursor(tty *os.File) bool` | 写 `DECRC`（`\x1b8`）：`ResetModes` 之后归位到锚点（2026-09-16 增补）|

设计原则：

- **以 `fd int` 为原语参数**：readline 的 `secureTerminalFd` 需作用于任意 fd（其单测即作用在 pty slave fd 上），`Open()` 只是便捷入口。
- **只下沉原语，不下沉策略**：不提供 `Handover/Restore` 组合函数，避免固化"仅前台才移交"这类决策。`agent` 保留三行组合；`readline` 保留启动前台归属与自愈门控。这与 `docs/design.md`《事实归属》"两处前台判定分别采样"的结论一致——共享原语，不合并决策。
- **termios 原语进 `ctty`（2026-09-15 修订，原结论为"不进"）**：原判断"仅 readline 使用"在 `run_shell` 需要**快照并在子进程结束后复原**控制终端时失效——`agent` 侧持有策略（何时移交、何时复原），原语与 readline 私有实现重复。现由 `ctty` 独占 termios 读写(`Get/Set/SetTermiosFlush`)与模式复位（`ResetModes`），`readline` 删私有 helper 改用 `ctty`，raw mode 的**构造**（flag 组合）仍留各消费方：那是策略，不是原语。
- **统一用 `Getpgid(0)`**：全平台返回 `(int, error)`，消除 `Getpgrp()` 的平台签名差异。
- **代码页快照态收在 `ctty`（2026-09-17）**：`EnsureUTF8/RestoreUTF8` 看似策略，但快照必须同时供 `main`（切换/复原）与 `agent`（`FallbackCP` 转码源）读取，跨包共享的唯一自然落点就是 `ctty`——与运行期信号的退出态同构：原语 + 一份包级状态，调用时序由消费方保证（`EnsureUTF8` 严格先于任何子进程派生与终端读写）。

## 分片

| 文件 | tag | 内容 |
|---|---|---|
| `ctty/ctty.go` | 无 tag | `Facts` + `Probe()`（组合各分片原语）|
| `ctty/ctty_posix.go` | `linux \|\| darwin` | ioctl 实现 + `Supported=true` + 探测原语 |
| `ctty/ctty_windows.go` | `windows` | `GetConsoleMode`/`GetConsoleScreenBufferInfo`/`SetConsoleMode` 探测原语 |
| `ctty/ctty_stub.go` | `!linux && !darwin` | 控制终端 no-op + `Supported=false`（含 windows）|
| `ctty/ctty_probe_stub.go` | `!linux && !darwin && !windows` | 探测原语保守实现（全 false）|
| `ctty/consolecp_windows.go` | `windows` | 代码页原语 + `EnsureUTF8/RestoreUTF8/FallbackCP/DecodeCP`（LazyDLL）|
| `ctty/consolecp_stub.go` | `!windows` | 代码页 no-op（posix 与其余平台共用一份）|
| `ctty/termios_linux.go` | `linux` | `Termios` 别名 + `TCGETS/TCSETS/TCSETSF` |
| `ctty/termios_darwin.go` | `darwin` | `Termios` 别名 + `TIOCGETA/TIOCSETA/TIOCSETAF` |
| `ctty/termios_stub.go` | `!linux && !darwin` | 空 `Termios` + 恒错实现 |
| `ctty/signals.go` | 无 tag | 运行期信号公共逻辑（`WatchSignals`/`Exit`/`Exiting`/`ExitSignal`/`ExitStatus`/`Interrupted`/分发）|
| `ctty/signals_posix.go` | `linux \|\| darwin` | 信号清单（SIGTERM/SIGHUP/SIGINT）+ `emergencyRestore` |
| `ctty/signals_windows.go` | `windows` | 信号清单（SIGTERM/`os.Interrupt`）+ `emergencyRestore` = `RestoreUTF8`（2026-09-17 拆出）|
| `ctty/signals_stub.go` | `!linux && !darwin && !windows` | 信号清单（SIGTERM/`os.Interrupt`）+ `emergencyRestore` no-op |

## 运行期信号（2026-09-17）

信号监听是终端语义的一部分（与 `/dev/tty`、termios 同层），故收敛进 `ctty`：平台白名单分片只声明清单，无 tag 文件承载公共逻辑，`repl`/`main` 不出现 `os/signal`、不区分退出方式。起因：此前 SIGTERM/SIGHUP 走 Go 默认处置，`pkill tanya` 直接把进程打死在 raw 态——pty 实测（空闲态与 interactive 桥接态各一次）进程被信号 15 终止、`/dev/pts` 的 `ICANON/ECHO/ISIG` 全保持关闭，`defer e.term.Restore()` 在信号路径不执行。

| API | 语义 |
|---|---|
| `WatchSignals()` | `sync.Once` 一次性安装监听（清单 = 关闭信号 + 中断信号），由 `main` 单点调用 |
| `Exit(sig os.Signal)` | 请求退出：首个来源生效（记住信号号）并广播中断，可重复调用 |
| `Exiting() bool` | 是否已请求退出；`readline` 轮询此值唤醒阻塞输入 |
| `ExitSignal() os.Signal` | 触发退出的信号（未触发为 nil）|
| `ExitStatus() int` | 进程退出码：`128 + signum`；无退出请求或来源非 `syscall.Signal` 时为 0 |
| `Interrupted() <-chan struct{}` | 取当前中断通道快照；广播时关闭并换新通道；**已处于退出态时直接返回已关闭通道**（退出态持久，晚到的订阅者立即感知并取消）|
| `ProtectJobSignals()` | 作业控制信号防护：`Notify(SIGTSTP)` 吞没 + `IgnoreJobSignals()`（吸收自 `agent.posixProtectSignals`）|

语义与约束：

- 清单：关闭信号 SIGTERM/SIGHUP（`signals_posix.go`）/ `syscall.SIGTERM`（`signals_stub.go`）；中断信号 SIGINT。windows 自 2026-09-17 起有专属分片（清单与原 stub 相同）——Go runtime 把控制台 CLOSE/LOGOFF/SHUTDOWN 事件折为 **SIGTERM** 递送并阻塞等待 handler 收尾（`runtime/os_windows.go` 的 `ctrlHandler`），落进关闭信号路径；`^C`/`^Break` 折为 SIGINT 走中断。**SIGQUIT 不入清单**，保持 Go 默认全栈转储（与 `docs/design.md`《信号》一致）。
- 关闭信号 → `Exit(sig)` + 广播中断；中断信号 → 只广播，不置退出态。
- **重复关闭信号 = 强退**：第二次送达时 `emergencyRestore()`（posix：仅自身为 `/dev/tty` 前台时把 tty 拉回 canonical + `ResetModes`，非前台不动——此时终端归子进程；windows：`RestoreUTF8` 复原控制台代码页）后 `os.Exit(ExitStatus())`。之所以不做定时兜底：正常取消路径最坏要 `shellWaitDelay`（2s）才收尾，定时器必须显著大于它，反而容易打断正常退出；而重复信号是显式意图，无隐式时序竞争，且顺带解决「`Notify` 之后普通信号不再有默认处置、用户只能 SIGKILL（必然留 raw）」这一固有缺陷。
- **订阅点必须同步取快照**：`Interrupted()` 取值要在启动等待 goroutine 之前完成（`repl.InterruptContext` 即此写法）。若在 goroutine 内才取，纯中断广播可能落在快照建立之前而被整轮丢失——该竞态在负载下必现（repl 用例在 `go test -race ./...` 下超时、单包串行却通过），是 2026-09-17 实测修掉的第一个坑。丢失窗口只影响瞬时的中断广播：退出态是持久的，快照时 `Exiting()` 为真则直接拿到已关闭通道（review 2026-09-17 补），回合照样立即取消、`Readline` 照样立即返回 `ErrExited`。
- 唤醒分工：`readline` 靠轮询 `ctty.Exiting()`（raw 下 `VMIN=0/VTIME=1` 每 ~100ms 一轮），命中即返回 `ErrExited`，且优先于已解析的按键队列（退出不被积压输入拖延）；`Degraded`（管道 stdin）阻塞在 `bufio` 上无法唤醒，但该路径本来就不改 termios，无残留代价，等重复信号强退即可。
- `os.Exit` 只出现在 `WatchSignals` 之后的分发路径，`WatchSignals` 只在 `main` 调用（测试不安装），库的常规使用面不受影响。

## 迁移

**agent**：删 `shell_tty_unix.go`、`shell_tty_stub_unix.go`；`shell_other.go` 去掉 4 个 tty 函数（保留进程组/信号；该文件与 `shell_unix.go` 后续收敛为 `shell_platform*.go`，见 `docs/design.md`《shell》）；`shell.go` 的 `openForegroundTTY/handoverForeground/restoreForeground` 改为 `ctty.Open/IsForeground/SetForeground` 组合，`handed` 门控不变；`envprobe.go` 的 `ttyStdinSupported()` → `ctty.Supported`。

**readline**：`secure.go` 的开 tty/Ignore/前台判定改走 `ctty`，`terminalGuardOwns` 与自愈策略保留；`bridge_linux.go` 删 `foregroundTTY`，`Prepare` 改 `ctty.IsForeground`；`secure_stub.go` tag 收敛为 `!linux && !darwin`。删私有 `termios_linux.go`/`termios_darwin.go`，`terminal_posix.go`/`bridge_linux.go`/测试改调 `ctty.GetTermios`/`ctty.SetTermios`/`ctty.SetTermiosFlush`；桥接 `release` 在复原 termios 后追加 `ctty.ResetModes`。

**agent**（2026-09-15 增补，2026-09-16 修订）：`runShellForeground` 在移交前快照 termios、在 defer 中复原（覆盖正常退出、超时 SIGKILL、中断三条路径），归还前台组后、`tty.Close()` 前调用 `ctty.ResetModes`——仅在 `ctty.IsForeground(fd)`（自己确实是终端前台）时发，避免后台运行/前台被抢占时改动别人的终端状态；归还前台（`SetForeground`）的返回值不参与判定，归还失败时同样不写。存档与归位收在同一门控内：`cmd.Start()` 之前（仍在移交前）`SaveCursor` 成功才置内部锚点标记，子进程结束后仅在「存过锚点 && `IsForeground`」时 `ResetModes` + `RestoreCursor`——没存过锚点就不恢复，避免把别人的保存槽值恢复出来。桥接侧对称：`Prepare`（`Attach`/`Start` 之前）存锚点、`release` 复位后归位。自愈范围从 ISIG 扩到 canonical/输出后处理，见 `docs/design.md`《run_shell》。

**模式复位的使用面（2026-09-15 引入，2026-09-16 修订）**：`ResetModes` 曾同时被 `agent` 常规路径与 `readline` 桥接 `release` 调用，且串内含裸 `CSI r`；DECSTBM 会把光标移到滚动区首行，而当时工具块靠 `CSI 1A` 相对重绘，症状为「执行 run_shell 时输出错乱」（工具块画到顶上、覆盖欢迎屏）。当时做法是去掉 `CSI r` 并把调用收敛到桥接 release，理由写作「常规路径子进程 stdout/stderr 走管道、不经终端，屏幕模式不会被改」。该理由**已被证伪**：非交互路径把真实 tty 交给子进程作 stdin（`cmd.Stdin = tty`），子进程写 `/dev/tty` 的转义（滚动区、`?7l`、`?25l`、SGR、鼠标上报）照样改屏幕状态。2026-09-16 修订：`CSI r` 回归但被 `DECSC`…`DECRC` 包住、`DECRST 1049` 同理（顺序固定、不可调换，理由见上表），两条路径都发且带前台门控（`ctty.IsForeground`）；串内另补字符集复位（`SI` + G0/G1 回 ASCII），且所有属性类复位都排在 `DECSC` 之前。`scripts/render_audit.py` 的 `clean-tty-scrollregion` / `clean-tty-modes` / `clean-interactive-release` 即该回归门（去掉复位或退回裸串都会变红，实测退回裸串三条均红）。

**锚点必须前置（2026-09-16 二次修订）**：`resetModes` 一度以 `\x1b7 … \x1b8` 自包来防自身副作用，但真终端 DSR 逐点实测表明它挡不住子进程造成的位移——子进程写 `\x1b[20;24r`（DECSTBM）时该终端把光标 home 到**绝对 (1,1)**（区外下方/区内/区外上方三种起点实测一致；xterm 系是夹到上边界，也没温和到无害），`\x1b7` 存下的正是这个被打乱的位置，`\x1b8` 忠实地又恢复回去 → 工具块正文从第 1 行开始画、覆盖历史（用户目视复现；探针 `clean-tty-scrollregion` 如实报 `overwrite 44`）。现改为**调用方锚点**：`runShellForeground` 在 `cmd.Start()` 前 `SaveCursor`、defer 里 `ResetModes` 后 `RestoreCursor`，桥接路径 `Prepare` 存、`release` 归位，复位串内不再含 `DECSC`/`DECRC`。负向对照：把 `SaveCursor` 或 `RestoreCursor` 任一步退化成 no-op，`clean-tty-scrollregion` / `clean-tty-cup` / `clean-alt-screen-exit`（连同其余 clean 门）一起变红（实测去掉存档得 `overwrite` 44、去掉归位得 `overwrite 50`）。已知限制：子进程自己 `DECSC` 后不 `DECRC`，会夺走保存槽，归位目标由它决定。

**分片收敛**：`terminal_unix.go`→`terminal_posix.go`（`linux||darwin`）、`termios_bsd.go`→`termios_darwin.go`（`darwin`）、`termios_sysv.go`→`termios_linux.go`（`linux`）；删 `terminal_unix_stub.go`，新增 `terminal_stub.go`（`!linux && !darwin && !windows`）补齐非目标平台的 `newUnixTerminal`。

## 验证

编译矩阵（`go build ./...`，`CGO_ENABLED=0`）：

| 平台 | 迁移前 | 迁移后 |
|---|---|---|
| linux / darwin / windows / freebsd / openbsd / netbsd / dragonfly / aix / android / illumos | OK | OK |
| solaris | **FAIL**（`Getpgrp` 多值）| OK |
| plan9 / js / wasip1 | **FAIL**（`newUnixTerminal` 未定义）| OK |
| ios | 需 cgo 工具链（非代码问题）| 同 |

测试：新增 `ctty/ctty_linux_test.go`（`OwnPgrp`/能力常量/非法 fd/`/dev/tty` 打开/pty 前台组往返——helper 子进程建独立会话、先 `IgnoreJobSignals` 再交接与归还，与生产路径同构）；`readline` 既有 pty 自愈测试（`secure_linux_test.go`）继续覆盖 `ctty` 原语的集成路径。

## 取舍与遗留

- **Windows**：Win32 无 pgrp / `TIOCSPGRP` 概念，`ctty_stub` 仅降级（`Supported=false`，agent 不接 stdin 到 tty、不移交前台）。将来实现 Windows 交互时需另议接口形状（console ownership 与 pgrp 不同构），当前不预留抽象。
- **非目标 unix**（freebsd/solaris/aix/android/illumos 等）：从"顺带可用"降级为 stub，换取 tag 集合单一化与可编译性保证。影响不止 ctty 原语 no-op——readline 的 raw mode 同样只剩 stub，这些平台的交互退化为 **Degraded 行输入（失去行编辑/历史/ghost/Tab 补全菜单）**。属明确取舍。
- **build tag 无法共享常量**：排除表达式仍需在各 stub 文件重复书写，`ctty` 只让"ctty 能力"这一概念有了单一归属，无法根除 tag 字符串层面的重复。
