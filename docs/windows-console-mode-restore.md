# Windows：interactive 回合后 CONIN$ 模式不复原（待修）

- 日期：2026-09-17
- 平台：Windows（Windows Terminal / ConPTY）+ PowerShell 5.1 后端
- 状态：**已实施**（方案 A，2026-09-17 开发机落地；Windows 实机回归待做，见 §8）
- 相关：`docs/terminal-caps.md` §8.6、`docs/interactive-tty.md`、`AGENTS.md`（终端复原不变量）

## 1. 问题

`interactive: true` 的 run_shell 回合中，子进程若改动了控制台**输入缓冲（CONIN$）的模式**且退出前不复原，tanya 在回合结束后**不会复原**：污染值被固化，并一路带到 tanya 退出。

- tanya 运行期间基本无感：`readline.Raw()` 每回合会把模式重设为 `当前值 | VT_INPUT &^ (ECHO|LINE|PROCESSED)`，输入仍可用。
- **tanya 退出后**，控制台保留被污染的模式。典型破坏：
  - `ENABLE_PROCESSED_INPUT`(0x1) 被清 ⇒ 按 Ctrl+C 不再产生 `CTRL_C_EVENT`，而变成字符 0x03（表现为「Ctrl+C 失灵」）；
  - `ENABLE_ECHO_INPUT`(0x4) / `ENABLE_LINE_INPUT`(0x2) 被清 ⇒ 回到 shell 后无回显、无行编辑，只能重开终端。

触发面：Windows 原生程序（ssh / gpg / cmd / ping）通常不动 console mode；**msys2 / Git-Bash 工具链**（git-bash 下的 vim / less / nano / ssh / python 等）会主动设置。

## 2. 实测证据（Windows 实机，2026-09-17）

用 P/Invoke 读写 `GetStdHandle(-10)`（STD_INPUT_HANDLE，与 CONIN$ 同一输入缓冲；console mode 是输入缓冲级状态，句柄不同也共享）：

```powershell
Add-Type -Namespace W -Name K -MemberDefinition '[DllImport("kernel32.dll")] public static extern IntPtr GetStdHandle(int n); [DllImport("kernel32.dll")] public static extern bool GetConsoleMode(IntPtr h, out uint m); [DllImport("kernel32.dll")] public static extern bool SetConsoleMode(IntPtr h, uint m);'
$h=[W.K]::GetStdHandle(-10); $m=[uint32]0; [W.K]::GetConsoleMode($h,[ref]$m)|Out-Null; "MODE=0x$('{0:X}' -f $m)"
```

| 步骤 | 操作 | 回读 |
|---|---|---|
| 基线 | 普通（非 interactive）回合内回读 | `0xE7`（canonical：processed+line+echo+insert+quickedit+extended） |
| 破坏 1 | interactive 回合内 `SetConsoleMode(CONIN$, 0x0)` 后退出 | `0x0`（后续多次回读仍 `0x0`） |
| 破坏 2 | interactive 回合内 `SetConsoleMode(CONIN$, 0xE1)` 后退出 | `0xE1` |
| 手动复原 | `SetConsoleMode(CONIN$, 0xE7)` | `0xE7` |

结论：回读值**等于子进程写入值**——既不是基线 `0xE7`，也不是 tanya 的 raw 组合（`0x200`）。即污染被原样固化，不存在任何自愈。
## 3. 根因

两处叠加，都在 Windows 路径上。

**(1) `readline/terminal_windows.go`：自愈链自我锁死**

```go
func (t *windowsTerminal) Raw() error {
	mode, ok := ctty.ConsoleMode(int(t.in.Fd()))
	if !ok { return ErrUnsupported }
	t.saved = mode          // ← 保存“当前值”，此刻已被子进程污染
	raw := mode | windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	raw &^= windows.ENABLE_ECHO_INPUT | windows.ENABLE_LINE_INPUT | windows.ENABLE_PROCESSED_INPUT
	...

}

func (t *windowsTerminal) Restore() { ctty.SetConsoleMode(int(t.in.Fd()), t.saved) }
```

下一回合 `Raw()` 把污染值写进 `t.saved`，`Restore()` 又原样写回 ⇒ 污染在回合之间传递。
`openTerminalFile()` 启动时读到的干净值同样会被首次 `Raw()` 覆盖，无法用于恢复。

**(2) `agent/shell.go:271` `runShellForeground`：Windows 侧没有快照/复原**

```go
var saved ctty.Termios
hasSaved := false
if tty != nil {
	if t, err := ctty.GetTermios(int(tty.Fd())); err == nil { saved, hasSaved = t, true }
}
defer func() {
	if tty == nil { return }
	if hasSaved { _ = ctty.SetTermios(int(tty.Fd()), saved) }   // posix：交终端前快照、子进程结束后复原
	if handed { ctty.SetForeground(int(tty.Fd()), ctty.OwnPgrp()) }
	if anchored && isForeground(int(tty.Fd())) { ctty.ResetModes(tty); ctty.RestoreCursor(tty) }
	tty.Close()
}()
```

`tty` 来自 `openTTY = ctty.Open`（`shell.go:242`），Windows 上返回 `CONIN$`。
但 `ctty.GetTermios` 在 Windows 落在 `ctty/termios_stub.go`（`//go:build !linux && !darwin`），恒返回错误 ⇒ `hasSaved == false` ⇒ **整个快照/复原分支在 Windows 上空转**。

对照 posix：`readline/terminal_posix.go` + `ctty/termios_{linux,darwin}.go` 提供同等能力，Windows 缺失 ⇒ 平台非对称（posix 侧该项实测无问题）。

## 4. 与既有文档/不变量的偏差

- `docs/terminal-caps.md` §8.6「残余取舍」现写：「子进程改乱 CONIN$ 模式后退出，**正常路径下回合 `Raw()` 自愈**，Degraded（`TANYA_NO_RAW_INPUT`）无 readline 自愈会残留（登记不修）」。实测**正常路径也不自愈** ⇒ 该句应改为「正常路径亦不复原（见本文档）」。
- `AGENTS.md` 记录的不变量「readline 每回合自愈把被留成 raw 的终端拉回 canonical」在 Windows 上不成立（自愈依赖 `saved` 为干净值）。

## 5. 修复方案

前置结论：`readline/secure.go` 的 build tag 是 `linux || darwin`，Windows 不编译它，故「借用 `ctty.Termios` 语义」不会波及其它 Windows 调用点。

### 方案 A（推荐）：ctty 新增「输入模式快照/复原」原语，`agent/shell.go` 改用

与 `Termios`（posix 概念）解耦，符合平台分片白名单习惯。

```go
// ctty/modes_windows.go
//go:build windows

package ctty

type InputModes struct{ Mode uint32 }

func SnapshotInput(fd int) (InputModes, bool) {
	m, ok := ConsoleMode(fd)
	return InputModes{Mode: m}, ok
}

func RestoreInput(fd int, m InputModes) bool { return SetConsoleMode(fd, m.Mode) }
```

```go
// ctty/modes_posix.go
//go:build linux || darwin

package ctty

type InputModes struct{ T Termios }

func SnapshotInput(fd int) (InputModes, bool) {
	t, err := GetTermios(fd)
	return InputModes{T: t}, err == nil
}

func RestoreInput(fd int, m InputModes) bool { return SetTermios(fd, m.T) == nil }
```

（其余平台补 no-op stub 分片，保持白名单策略。）

`agent/shell.go` 三处等价替换：

```go
var saved ctty.InputModes
if s, ok := ctty.SnapshotInput(int(tty.Fd())); ok { saved, hasSaved = s, true }
...
if hasSaved { ctty.RestoreInput(int(tty.Fd()), saved) }
```

时序闭合：回合末复原成干净值 ⇒ 下一回合 `readline.Raw()` 保存的就是干净值 ⇒ `Restore()` 写回干净值。**`readline` 无需改动**。

覆盖场景：正常结束、超时强杀（`taskkill /T /F` 后 `defer` 仍执行）、Ctrl+Break 中断（ctx 取消 → `waitShell` 返回 → `defer` 执行）。

### 方案 B（改动更小，语义略脏）：让 Windows 的 `ctty.Termios` 承载 console mode

新增 `ctty/termios_windows.go`（`//go:build windows`）把 `GetTermios/SetTermios/SetTermiosFlush` 实现为 console mode 读写；`ctty/termios_stub.go` 的 tag 改为 `!linux && !darwin && !windows`。`agent/shell.go` 零改动。

副作用（实施时勘误）：原文称 `agent/shell_tty_test.go` 在方案 B 下会于 Windows 执行、需加平台守卫——实测该文件首行即 `//go:build linux`，整文件 linux-only，Windows 根本不编译，无需守卫。

### 不建议

改 `readline.Raw()` 保存「启动 baseline」：与 posix（保存进入 raw 前的当前值）语义分叉，且会覆盖用户在 tanya 运行期间有意的模式改动。
## 6. 回归验证（Windows 实机）

**(1) 三步法**（无需第三方工具，`-10` = STD_INPUT_HANDLE）

```powershell
# ① 读模式（基线应为 0xE7）
Add-Type -Namespace W -Name K -MemberDefinition '[DllImport("kernel32.dll")] public static extern IntPtr GetStdHandle(int n); [DllImport("kernel32.dll")] public static extern bool GetConsoleMode(IntPtr h, out uint m); [DllImport("kernel32.dll")] public static extern bool SetConsoleMode(IntPtr h, uint m);'
$h=[W.K]::GetStdHandle(-10); $m=[uint32]0; [W.K]::GetConsoleMode($h,[ref]$m)|Out-Null; "MODE=0x$('{0:X}' -f $m)"

# ② 破坏：在 tanya 内以 interactive: true 运行，退出前不复原
[W.K]::SetConsoleMode($h,[uint32]0xE1)

# ③ 回读：修复后应为基线 0xE7，不得为 0xE1（修复前实测为 0xE1）
```

**(2) 超时强杀**：`interactive: true` + `ping -t 127.0.0.1` + `timeout=15`，事后
- `Get-Process ping` 应为空（当前实测通过，无残留）；
- 模式回读应为基线（若子进程改过模式，修复前会残留）。

**(3) 单测建议**
- `ctty`：新增 `modes_windows_test.go`（Snapshot/Restore 往返；非终端 fd 返回 false）。
- `agent`：`shell_tty_test.go` 整文件 `//go:build linux`，Windows 不参与（原文「门槛不变」的说法勘误同 §5）；ctty 侧新增 `modes_windows_test.go`（非终端 fd + 往返）。agent 级「interactive 回合前后 CONIN$ 模式回到快照值」断言未加——Windows 测试基线暂不可信（31 例失败，见附录），以 §6(1) 三步法手工回归兜底。

**(4) 交叉编译与静态检查**

```bash
GOOS=linux   go build ./... && go vet ./...
GOOS=darwin  go build ./... && go vet ./...
GOOS=windows go build ./... && go vet ./...
go test -race ./...        # 在 Linux/macOS 开发机上跑 posix 回归
```

## 7. 边界（维持现状，不做）

- 进程被强杀（SIGKILL / 关闭终端）：`defer` 不执行，无法复原——与 posix 同，登记不修。
- 掩蔽期间关闭标签页无 CP 复原收尾——维持现状。

## 8. 实施记录（2026-09-17，开发机）

- 新增 `ctty/modes_windows.go`、`ctty/modes_posix.go`（薄转发 `GetTermios`/`SetTermios`）、`ctty/modes_stub.go`（`!linux && !darwin && !windows`）。
- `agent/shell.go` 快照/复原改用 `ctty.SnapshotInput`/`RestoreInput`，覆盖 interactive 与非 interactive 两类回合（同一 `runShellForeground` 路径）。
- 新增 `ctty/modes_windows_test.go`、`ctty/modes_posix_test.go`（非终端 fd 返回 false + 往返一致）。
- 开发机验证：`GOOS=linux/darwin/windows` 三平台 `go build ./...` + `go vet ./...` 通过；Linux `go test ./...` + `go test -race ./...` 通过（posix 零行为变化的证据）。
- 待办：Windows 实机跑 §6(1)(2) 三步法/强杀回归 + `go test ./ctty -run Snapshot`；通过后更新本状态行。

## 附：Windows 实机 `go test ./...` 现状（非功能缺陷，供参考）

本机（Windows，HEAD `401237b`，工作区干净）全量结果：31 例失败（`agent` 20 / `repl` 11），`ctty` / `readline` / `render/*` 全绿。归类：

1. **测试隔离在 Windows 失效**：`agent` 的 `TestMain` 隔离 HOME，而 Windows 上配置目录解析走 `USERPROFILE`，`agent_test.go` 的 prompt 用例读到真实用户 `~/.config/tanya/AGENTS.md`（3 例）。
2. **断言写死 posix 路径/分隔符**：`shortPath`、`TestHomePath`、`initflow` 报告、`shelltool` 的 `/tmp`（约 10 例）。
3. **用例本身是 bash 语法**：`echo x >&2`、`exec -a name sleep 30 & wait`（PowerShell 下直接语法错误）。
4. **断言依赖 posix 输出格式**：`Get-ChildItem` 表格输出、注入只含 bash 的 Programs 清单。
5. **依赖 Linux PTY 桥接与信号退出码 130**：`bridge_linux` only（3 例）。
6. **时序脆弱**：`reason` 用例要求下分隔带时长，Windows 计时精度下 `time.Since` 得 0 ⇒ 实现省略时长（2 例）。

属「测试平台适配」的独立工作，不阻塞本次修复；但意味着 Windows 上目前没有可信的回归基线。

## 附：本轮已验证通过的项（Windows 实机）

- `interactive: true` 下 `-NonInteractive` 被过滤（`-NoProfile` 保留）；非 interactive 下 `Read-Host` 抛 `NonInteractive mode` 错误。
- 子进程 stdin 接到控制台，可读真实键盘输入（含中文往返）。
- ^C 只达子进程（掩蔽生效）；Ctrl+Break 中断 tanya，符合设计。
- 超时强杀干净：无残留进程，终端模式未变。
