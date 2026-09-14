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

设计原则：

- **以 `fd int` 为原语参数**：readline 的 `secureTerminalFd` 需作用于任意 fd（其单测即作用在 pty slave fd 上），`Open()` 只是便捷入口。
- **只下沉原语，不下沉策略**：不提供 `Handover/Restore` 组合函数，避免固化"仅前台才移交"这类决策。`agent` 保留三行组合；`readline` 保留启动前台归属与自愈门控。这与 `docs/design.md`《事实归属》"两处前台判定分别采样"的结论一致——共享原语，不合并决策。
- **termios / raw mode 不进 `ctty`**：仅 readline 使用，无跨包重复；纳入只会扩大职责。
- **统一用 `Getpgid(0)`**：全平台返回 `(int, error)`，消除 `Getpgrp()` 的平台签名差异。

## 分片

| 文件 | tag | 内容 |
|---|---|---|
| `ctty/ctty_posix.go` | `linux \|\| darwin` | ioctl 实现 + `Supported=true` |
| `ctty/ctty_stub.go` | `!linux && !darwin` | no-op + `Supported=false`（覆盖 windows 及其它）|

## 迁移

**agent**：删 `shell_tty_unix.go`、`shell_tty_stub_unix.go`；`shell_other.go` 去掉 4 个 tty 函数（保留进程组/信号）；`shell.go` 的 `openForegroundTTY/handoverForeground/restoreForeground` 改为 `ctty.Open/IsForeground/SetForeground` 组合，`handed` 门控不变；`envprobe.go` 的 `ttyStdinSupported()` → `ctty.Supported`。

**readline**：`secure.go` 的开 tty/Ignore/前台判定改走 `ctty`，`terminalGuardOwns` 与 `ISIG` 恢复策略保留；`bridge_linux.go` 删 `foregroundTTY`，`Prepare` 改 `ctty.IsForeground`；`secure_stub.go` tag 收敛为 `!linux && !darwin`。

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
