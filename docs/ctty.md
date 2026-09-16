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
| `Facts` / `Probe()` | 探测结果聚合（StdinTTY/StdoutTTY/Cols/Rows/SizeOK/VT/Kind），`main` 单点调用 |
| `ResetModes(tty *os.File) bool` | 复位终端模式：SGR、显示光标、自动换行、退出备用屏、关鼠标上报；**不含 `CSI r`（DECSTBM）**——该序列按 VT100/ECMA-48 语义会把光标移到滚动区首行。排除源于调用侧曾依赖 `CSI 1A` + `CR CSI K` 相对寻址（工具块 inline 重绘与 spinner 帧，2026-09-15 追加化后已移除，见 `docs/repl-status-append.md`）；该排除保留，以免未来再引入相对寻址渲染时踩坑 |

设计原则：

- **以 `fd int` 为原语参数**：readline 的 `secureTerminalFd` 需作用于任意 fd（其单测即作用在 pty slave fd 上），`Open()` 只是便捷入口。
- **只下沉原语，不下沉策略**：不提供 `Handover/Restore` 组合函数，避免固化"仅前台才移交"这类决策。`agent` 保留三行组合；`readline` 保留启动前台归属与自愈门控。这与 `docs/design.md`《事实归属》"两处前台判定分别采样"的结论一致——共享原语，不合并决策。
- **termios 原语进 `ctty`（2026-09-15 修订，原结论为"不进"）**：原判断"仅 readline 使用"在 `run_shell` 需要**快照并在子进程结束后复原**控制终端时失效——`agent` 侧持有策略（何时移交、何时复原），原语与 readline 私有实现重复。现由 `ctty` 独占 termios 读写(`Get/Set/SetTermiosFlush`)与模式复位（`ResetModes`），`readline` 删私有 helper 改用 `ctty`，raw mode 的**构造**（flag 组合）仍留各消费方：那是策略，不是原语。
- **统一用 `Getpgid(0)`**：全平台返回 `(int, error)`，消除 `Getpgrp()` 的平台签名差异。

## 分片

| 文件 | tag | 内容 |
|---|---|---|
| `ctty/ctty.go` | 无 tag | `Facts` + `Probe()`（组合各分片原语）|
| `ctty/ctty_posix.go` | `linux \|\| darwin` | ioctl 实现 + `Supported=true` + 探测原语 |
| `ctty/ctty_windows.go` | `windows` | `GetConsoleMode`/`GetConsoleScreenBufferInfo`/`SetConsoleMode` 探测原语 |
| `ctty/ctty_stub.go` | `!linux && !darwin` | 控制终端 no-op + `Supported=false`（含 windows）|
| `ctty/ctty_probe_stub.go` | `!linux && !darwin && !windows` | 探测原语保守实现（全 false）|
| `ctty/termios_linux.go` | `linux` | `Termios` 别名 + `TCGETS/TCSETS/TCSETSF` |
| `ctty/termios_darwin.go` | `darwin` | `Termios` 别名 + `TIOCGETA/TIOCSETA/TIOCSETAF` |
| `ctty/termios_stub.go` | `!linux && !darwin` | 空 `Termios` + 恒错实现 |

## 迁移

**agent**：删 `shell_tty_unix.go`、`shell_tty_stub_unix.go`；`shell_other.go` 去掉 4 个 tty 函数（保留进程组/信号；该文件与 `shell_unix.go` 后续收敛为 `shell_platform*.go`，见 `docs/design.md`《shell》）；`shell.go` 的 `openForegroundTTY/handoverForeground/restoreForeground` 改为 `ctty.Open/IsForeground/SetForeground` 组合，`handed` 门控不变；`envprobe.go` 的 `ttyStdinSupported()` → `ctty.Supported`。

**readline**：`secure.go` 的开 tty/Ignore/前台判定改走 `ctty`，`terminalGuardOwns` 与自愈策略保留；`bridge_linux.go` 删 `foregroundTTY`，`Prepare` 改 `ctty.IsForeground`；`secure_stub.go` tag 收敛为 `!linux && !darwin`。删私有 `termios_linux.go`/`termios_darwin.go`，`terminal_posix.go`/`bridge_linux.go`/测试改调 `ctty.GetTermios`/`ctty.SetTermios`/`ctty.SetTermiosFlush`；桥接 `release` 在复原 termios 后追加 `ctty.ResetModes`。

**agent**（2026-09-15 增补）：`runShellForeground` 在移交前快照 termios、在 defer 中复原（覆盖正常退出、超时 SIGKILL、中断三条路径）；`handed` 时只归还前台组，**不再做模式复位**。自愈范围从 ISIG 扩到 canonical/输出后处理，见 `docs/design.md`《run_shell》。

**模式复位的使用面收敛（2026-09-15 二次修订）**：`ResetModes` 曾同时被 `agent` 常规路径（`handed` 在 REPL 中恒真，等于每次 `run_shell` 都写）与 `readline` 桥接 `release` 调用，且串内包含 `CSI r`。DECSTBM 会把光标移到滚动区首行，之后工具块 inline 重绘的 `CSI 1A` / `CR CSI K` 全部落在屏幕顶部——表现为「执行 run_shell 时输出错乱」：工具块画到顶上、覆盖欢迎屏、残留 spinner 行。现串内去掉 `CSI r`，并让常规路径只复原 termios（该路径子进程 stdout/stderr 走管道、不经终端，屏幕模式不会被改），`ResetModes` 仅由 `readline` 桥接 `release` 使用（工具块的 `CSI 1A` 相对重绘已于同日随追加化移除，该排除保留）。

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
