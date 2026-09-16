# 交互式命令输入（全 pty 桥接）

> 状态：**已落地**（linux 专用；`readline/bridge_linux.go` + `agent/tty_bridge.go`，契约已并入 `docs/design.md`《工具》/《工具视图渲染》/《终端输入》三节），本文归档保留决策过程。
>
> 落地差异：① `readline/bridge.go` 承载跨平台接口（`bridge_stub.go` 返回 `ErrUnsupported`）；② 调用顺序为 `Prepare → Attach → Start`（Attach 失败即回退，Start 失败由 `stop()` 收尾）；③ 泵用 `poll` + 自管道唤醒替代"关 fd 打断阻塞读"（Linux 上 close 不会唤醒阻塞中的读）；④ raw 切换用不清输入队列的 `TCSETS`（`TCSETSF` 会丢弃用户提前键入的密码）；⑤ `stop()` 在泵退出前 drain master 残留输出，避免与 `capture.finish()` 竞态；⑥ `stop()` 唤醒泵前若终端写缓冲已满，最多放弃当前 chunk 的**显示**副本（捕获流完整，属 §7 显示侧豁免）；⑦ 实例带 busy/attached 守卫，重复/并发使用返回 `ErrUnsupported`；⑧ 测试改用 pty 自驱集成（`readline/bridge_linux_test.go`）+ 真实 tty E2E（`TTY_BRIDGE_E2E=1` / `TTY_E2E=1` 经 `script` 驱动）；⑨ 光标锚点（2026-09-16 增补）：`Prepare`（切 raw 与 `Start` 之前）调 `ctty.SaveCursor` 存锚点，`release` 里 `ResetModes` 之后调 `ctty.RestoreCursor` 归位——交互期子进程改滚动区/挪光标/进出备用屏都不再让结果块从屏幕顶部开始画，理由与顺序见 `docs/ctty.md`。

## 1. 背景与根因

`run_shell` 在 `interactive: true` 下需让命令在真实终端应答（sudo 密码、ssh 密码 / passphrase、gpg（pinentry）口令）。现状机制（原 `agent/shell_tty_unix.go`，该组原语现迁至 `ctty`）为 `open("/dev/tty")` → `cmd.Stdin = tty` → `TIOCSPGRP` 前台移交，实际使用中交互程序**无法取得输入**。根因两条叠加：

1. **终端名推导异常**：子进程 stdin 绑 `/dev/tty` 的 fd，`ttyname(0)` 退化为 `/dev/tty`（`/dev/tty` 的 inode `st_rdev` 为 5:0，非真实 pts 设备号），凡自行推导终端路径者（gpg 推导 `GPG_TTY`、部分 ssh/sudo 分支）落到不可用路径
2. **无控制终端**：pinentry 等由守护进程拉起的程序无 ctty，只能按路径打开 tty，路径为 `/dev/tty` 时 `open` 必然 `ENXIO`

次要缺陷：子进程提示与工具块渲染同终端竞争、无 `SIGWINCH` 转发、`TIOCSPGRP` 前后有窗口且失败静默。实测 `script -qec <cmd>`（新建 pty + 新会话）一切正常，指向"命令在自己的 pty 里跑"的方向。

## 2. 需求约束

| 维度 | 约束 |
|---|---|
| 目标程序 | 必须：sudo、ssh（密码 / passphrase）、gpg（pinentry）；通用代表：`read` |
| 不追求 | `mysql -p`、`docker login`、`ftp` 等其它交互程序 |
| 触发 | 本次 `interactive=true` 显式触发；"默认全具备"仅作探索方向 |
| 可选性 | 功能/分支可选择不启用（未注入或 Prepare 失败即走回退，等同不启用）；模型可选择不用（不设 `interactive`） |
| 交互形态 | 原生直通（像直接跑命令）+ 显式"等待输入"提示 |
| 中断 | Ctrl+C 原样透传 + 超时兜底 |
| 输出去向 | 必须能捕获给模型（能力必须存在） |
| 输出形式 | 原样全量，复用 `streamCapture` 头尾截断，不做清洗 |
| 超时 / 挂起 | 沿用现状（interactive 默认 300s；挂起检测即终止） |
| 地基 | 执行器产出实时流 + 捕获流，消费交给调用方 |

## 3. 目标与非目标

**目标**

- 交互程序在 `interactive: true` 下获得正常控制终端：子进程内 `/dev/tty` 可打开、`ttyname(0)` 指向真实 pts 设备
- 用户实时看到命令完整输出（原生直通）；模型侧保留完整捕获
- 零新依赖（复用 `golang.org/x/sys/unix`）；失败自动回退现状路径
- 非交互路径行为零变化
- 为长期"用户前台命令"预留可复用执行器

**非目标**

- 全屏 TUI（vim/less/top）不列入验收（全 pty 或许顺带可用，但不承诺）
- Windows ConPTY（维持现状占位）
- 输出清洗、stdout/stderr 区分、`^C` 计数强杀（见 §7 刻意简化登记）

## 4. 总体架构

```
真实 tty (foot /dev/pts/8)
   │  raw mode，bridge 独占
   ▼
┌──────────┐   real→master（输入）   ┌────────────┐
│  bridge  │ ──────────────────────► │ pty master │
│ (双向泵) │ ◄────────────────────── │            │
└────┬─────┘   master→real（输出）   └─────┬──────┘
     │ 捕获流（master 输出副本）           │ pty slave
     ▼                                     ▼
  调用方                              = 子进程 ctty
  (streamCapture /                    = 子进程 stdin/stdout/stderr
   上下文 / Discard)                  = 子进程内 /dev/tty
```

关键点：

- **全 pty**：子进程 stdin/stdout/stderr 全接 pty slave，slave 即 ctty。因"原生直通"要求用户实时看到完整输出，命令输出不能走捕获管道、必须走终端流；master 输出**同时**写真实 tty（用户看）与 `capture`（模型）——这是"既显示又捕获"的唯一手段
- 子进程经 `Setsid + Setctty` 成为独立会话首进程，ctty 为 pty slave；真实 tty 前台组始终是 tanya，桥接路径下 `TIOCSPGRP` 移交**不再需要**
- 真实 tty 由 bridge 切 raw 并独占读写；pty 侧行规程（cooked + `ISIG` + echo）承担行编辑与信号语义
- **契约**：`capture` 是"捕获内容"，进 `ShellResult` → 模型；实时流是"即时展示"，仅写真实 tty，不单独进模型

## 5. 关键机制

### 5.1 pty 分配（`readline/bridge_linux.go`）

`open("/dev/ptmx", O_RDWR|O_NOCTTY)` → `ioctl(TIOCSPTLCK, 0)` 解锁 → `ioctl(TIOCGPTN)` 取编号 N → `open("/dev/pts/N", O_RDWR|O_NOCTTY)` 得 slave。BSD/macOS 无 `TIOCGPTN`，阶段 1 仅 Linux，其余 unix 走 `bridge_stub.go` 返回 `ErrUnsupported` 回退。

真实 tty 由 bridge 自行 `open("/dev/tty", O_RDWR)` 获取（原 `openForegroundTTY`，现 `ctty.Open`），不依赖 `os.Stdin`。跨平台统一入口 `readline.NewTTYBridge()`：Linux 返本实现，其余 unix 返 stub。

### 5.2 ctty 建立

`cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}`，`cmd.Stdin/Stdout/Stderr = slave`。

**`Ctty` 是子进程 fd 索引**（标准库校验 `Ctty >= len(attr.Files)` 即报 `Setctty set but Ctty not valid in child`），不是父进程 fd 号；三条标准流均为 slave 时取 `0`。

### 5.3 slave 关闭与泵终止

`cmd.Start()` 成功后，调用方**立即关闭** `Prepare` 返回的 slave。父进程残留 slave 会使 master 在子进程退出后永不收到 `EIO`，泵悬挂、命令假死。

泵两条终止路径：master `EIO`/EOF（正常结束）与 `stop()`（强制终止）。二者都要接住——守护进程持有的子进程（pinentry）可能长时间持有 slave，不能只靠 EIO。

**`stop()` 关闭顺序**（缺一不可）：① 恢复真实 tty termios → ② 关闭 bridge 自开的真实 tty fd（令 `real→master` 方向的阻塞读返回）→ ③ 关闭 master（令 `master→real` 方向的阻塞读返回）。子进程正常退出只终止 ③ 方向，`real→master` 方向始终依赖 ② 收干净。

### 5.4 raw 与回显

桥接期真实 tty 切 raw（去 `ECHO/ICANON/ISIG/IEXTEN`，`VMIN=1/VTIME=0` 阻塞读）；pty slave 保持 cooked + `ISIG` + `ECHO`。

- 输入：真实 tty 关 `ECHO` → 经 master → slave 行规程 → 子进程
- 回显：由 slave 侧单次完成（真实 tty 无 echo，不重复）
- 换行：真实 tty 关 `OPOST`，`\n`→`\r\n` 转换由 pty slave 侧负责
- `stop()` 无条件恢复 termios（defer + 信号兜底），防终端挂死
- 尺寸：`Attach` 时**必须**复制一次真实 tty 尺寸到 master（pty 新建默认 0×0）；此步属必须，后续 `SIGWINCH` 转发才是"尽力而为"（§7）
- 安全：sudo/ssh/gpg 均对密码关闭回显，密码不经 pty echo、不进 master 输出，因而**不会进 `capture`**；进入捕获的只有提示与命令实质输出

### 5.5 环境覆盖

`Prepare` 覆盖子进程 env 的 `GPG_TTY`、`SSH_TTY` 为 slave 设备路径（`/dev/pts/N`），压过继承值。

遗漏此项则 pinentry 类程序按环境变量打开**真实 tty**，输入被 bridge 抢读、仍无法输入——这是本方案能否修好 pinentry 的关键，也是唯一必须实测的外部行为假设。

### 5.6 渲染互斥

桥接期间真实 tty 归 bridge，repl 侧不得写入。现状 `interactive` 不发状态行心跳；标题行在切 raw **之前**打印，结果块在 `stop()` 之后追加渲染。

### 5.7 中断、超时、挂起

- **Ctrl+C**：`0x03` 经泵进入 pty，由 slave 行规程投 `SIGINT` 给子进程前台组（等价用户按键）；tanya 不拦截、不计次
- **超时**：沿用 `context.WithTimeout`（interactive 默认 300s），到时 `cmd.Cancel → platform.KillGroup`
- **挂起**：`^Z` 经 pty 投 `SIGTSTP`，`waitShell` 的挂起轮询（`platform.ProcessStopped`）判定路径不变

### 5.8 失败回退

无 ctty / pty 分配失败 / 非 Linux → 回退现状 `open("/dev/tty")` + `TIOCSPGRP` 路径（代码保留）。

### 5.9 终端状态自愈（`readline/secure.go`）

桥接与中断都依赖两项终端不变量：**`ISIG` 开启**（否则 `^C` 不产生 `SIGINT`，`signal.Notify(SIGINT)` 的中断路径完全失效）与**终端前台进程组是 tanya**（否则 `^C` 投递给别的进程组）。二者都可能被外部因素破坏，且 termios 与前台组**跨进程存活**、不随程序退出复位。验收实测到的两起：

| 观测 | 起因 | 后果 |
|---|---|---|
| `lflag` 变成"cooked 减 `ISIG`"（`0x8a3a`） | 历史会话残留（termios 跨进程存活） | 任何 `^C` 都不产生信号，请求无法中断 |
| 终端前台 pgrp 变为 shell 的 pgrp、termios 变为 shell 提示符模式（实测持续 28s） | 非桥接时段按 `^Z`：`SIGTSTP` 投给 tanya 所在进程组，tanya 因 `ProtectTerminalSignals` 免疫，但 **`go run` wrapper 被停止** → shell 判定前台作业已停并抢回终端 | 之后 `^C` 全给 shell，tanya 收不到；且桥接因 `ctty.IsForeground` 判负而静默回退（表现为"按键无反应"） |

处置：

- `InitTerminalGuard`（启动时一次）：记录"启动瞬间自己就是终端前台组"（`TIOCGPGRP == getpgrp()`），并 `signal.Ignore(SIGTTIN, SIGTTOU)`——后者是夺回前台的前提（前台已被抢走时调用 `tcsetpgrp` 会触发 `SIGTTOU`，未忽略则把自己停住）
- `SecureTerminal`：① `ISIG` 被清则置回 ② 若启动时是前台组而当前不是，`TIOCSPGRP` 夺回
- **门控**：仅"启动时即为前台作业"才自愈——后台启动（`&`）、无控制终端（`setsid`）等场景语义不变（仍走回退），不会去抢用户 shell 的终端
- **调用点**：`main.go`（启动）、`repl/repl.go`（每回合开始前，即编辑器已还原 termios、尚未进入回合）、`bridge_linux.go` 的 `Prepare`（`ctty.IsForeground` 判定**之前**）。放在前台判定之前的意义：被抢终端的场景会自愈并**照常走桥接**，而不是静默降级到回退路径
- 实测：启动前人为清掉 `ISIG` → 启动后 0.00s 恢复为 `0x8a3b`；前台被抢后 `tcsetpgrp` 夺回（单测覆盖）

## 6. 地基契约

- `Prepare` 只设置 `SysProcAttr`、三条标准流与 `Env`，**不覆盖 `cmd.Dir`**——`run_shell` 的 `cwd` 参数在桥接路径同样生效，无需扩展接口

```go
// agent/tty_bridge.go
type TTYBridge interface {
    Prepare(cmd *exec.Cmd) (*os.File, error)
    Attach(capture io.Writer) (stop func(), err error)
}
func WithTTYBridge(b TTYBridge) Option // 未注入或 Prepare 失败 → 回退现状路径
```

- 执行器产出：① 实时流（内部泵给真实 tty）② 捕获流（`capture` Writer，master 输出全量副本）
- **消费由调用方决定**，执行器不内嵌任何"工具语义"：

| 调用方 | capture | 去向 |
|---|---|---|
| run_shell interactive（本次） | `streamCapture` | `ShellResult` → 模型 |
| 前台命令·给模型（长期） | 上下文追加器 | b1 |
| 前台命令·不给模型（长期） | `io.Discard` | b2 |

- 实现归属 `readline`（复用 `getTermios`/`setTermios` 与 raw 语义，不复制 termios 代码），生产实例由 `main.go` 经 `agent.WithTTYBridge` 注入 `agent.New`（构造期定格，无包级注入点）
- 生产入口 `readline.NewTTYBridge()`（内部打开 `/dev/tty` 与分配 pty）；另留内部注入构造 `newBridgeTTY(tty *os.File, master *os.File)`，供集成测试以外层 pty 充当"真实 tty"（§8）
- 全 pty 下 stdout/stderr 合并于同一 slave 流，捕获为**单流**；交互模式将其填入 `ShellResult.Stdout`（`Stderr` 空，`2|` 区分失效，见 §7）

## 7. 刻意简化登记

原则：**稳定性与核心可用性优先**。凡实际使用影响小、或对模型侧语义无实质影响的边缘正确性，一律从简。后续实现、评审、测试中若再次提出这些点，**直接引用本节关闭，不再投入资源评估或修补**。

| 项 | 处置 | 理由 |
|---|---|---|
| `^C` 计数双杀（1s 窗口、第 2 次强杀） | **不做**。`^C` 一律透传；需强制终止由超时兜底 | 触发极少；需额外维护计数、abort 接线与结果标记，收益不抵成本 |
| 中断结果标记（`Interrupted` / 新增字段 + 文案） | **不做**。桥接路径中断沿用既有结果路径，不新增字段与文案 | 模型对"中断 vs 退出码"语义不敏感 |
| `^Z` 挂起精细化语义 | **沿用现状**（挂起轮询 `platform.ProcessStopped`），不因桥接新增分支 | 现状已可用 |
| 窗口 resize 转发 | **尽力而为**：`SIGWINCH` → `TIOCSWINSZ(master)`，失败或遗漏不报错、不重试（**初始尺寸复制不在此列，属必须，见 §5.4**） | 交互命令跨窗口变化场景少，失败无后果 |
| 输出清洗（去 ANSI / 退格 / `\r` / 提示行） | **不做**。原样全量，复用 `streamCapture` 头尾截断 | 去提示行不可靠、易误删实质输出；ANSI 剥离收益存疑 |
| stdout/stderr 区分（`2|` 标记） | **豁免**。交互模式捕获为单流 | 全 pty 固有代价，对 sudo/ssh/gpg 无影响 |
| 桥接期显示对齐 / 时序瑕疵 | 不追求像素级对齐，观感瑕疵不阻塞落地 | 属观感，非功能 |
| `stop()` 时显示副本可能丢弃（真实 tty 写缓冲满） | **不做**。捕获完整，仅显示侧可能少一个 chunk | 实测未复现；加有界重试只会增加复杂度 |
| 编辑器进 raw 时 `TCSETSF` 清 typeahead | **不做**。属编辑器既有行为，非桥接引入 | 实测放大为"回合未结束时键入的字符被丢弃"（如模型仍在回答时敲 `/exit`）；要改需在进 raw 前先 drain 输入缓冲，影响面超出桥接 |
| 桥接期 repl 侧写终端互斥 | **复核无问题** | 状态行心跳未启动（`interactive` 不发）、标题行在切 raw 前打印、结果块在 `stop()` 后渲染；ask 模式无其它写终端路径 |

> 落地补充（实测）：`Setsid` 使子进程组成为**孤儿进程组**（父进程在另一会话），内核按 POSIX 规则丢弃停止信号——`^Z` 经 pty 投递的 `SIGTSTP` 不会挂起子进程（表现为无效按键，等价 `script` 会话内 `^Z` 的现象），`^C`（SIGINT）不受影响、正常透传。按 §7「`^Z` 沿用现状、不因桥接新增分支」，不为此增加泵内按键识别/强杀分支；`waitShell` 的挂起轮询保留（显式 `SIGSTOP` 等场景仍生效）。

**不在豁免范围（核心正确性，必须做）**：pty 分配与 ctty 正确建立（§5.2）；Start 后关闭父进程 slave 与泵双终止路径（§5.3）；`stop()` 无条件恢复 termios；`GPG_TTY`/`SSH_TTY` 覆盖（§5.5）；桥接期渲染互斥；失败回退现状路径。

### 7.1 上一轮验收结论（A1–A9 与观察项）

| 项 | 结论 |
|---|---|
| A1 `^Z` 语义 | **不改代码**。实测：桥接期按 `^Z` 仅回显、子进程不挂起、命令跑完（与 §7 登记一致）。附带发现见 §5.9——非桥接时段按 `^Z` 会走 shell 作业控制：`./tanya` 直接运行因 `ProtectTerminalSignals` 免疫，但 **`go run .` 启动时 wrapper 会被停止**，shell 抢走终端后 `^C` 失效。**交付/自测请用 `make build` 产出的 `./tanya`，不要用 `go run .`** |
| A2 `Attach(cmd, capture)` 未用 `cmd` | **已删参**（接口、两处实现、fake、调用点、文档同步） |
| A3 双 `TTYBridge` 定义 | **保留**：agent 不 import readline 的依赖倒置，漂移会在 `main.go` 注入处编译期暴露 |
| A4 `RunShellResult` 新增 `interactive` 参数 | **保留**：模块非公共库，仓库内调用点已同步。（后续 shellTool 组件化中该函数已删除，语义由 `shellRequest.Interactive` 承接） |
| A5 `stop()` 丢显示副本 | 不改，登记 §7 |
| A6 typeahead 被 flush 丢弃 | 不改，登记 §7（候选改进见 §10） |
| A7 桥接期写入互斥 | 复核无问题，登记 §7 |
| A8 非交互 `cmd.Stdin` 绑 `/dev/tty` | 不在本轮范围，保留 §10 开放问题 |
| A9 Windows/BSD 无桥接 | 保留现状（阶段 1 仅 Linux）；macOS/BSD 排期见 §10 |
| O1 信号终止记为 `exit -1` | **已改**：按 shell 惯例记 `128 + signum`（`^C` → `130`、`SIGKILL` → `137`），两路结果一致 |
| O2 结果无"桥接/回退"标记 | **不改**。模型偶有误判（把回退路径的 `/dev/tty` 说成"独立 pty"），影响有限；自愈落地后回退场景进一步减少 |

## 8. 测试计划

**单测**

- pty 分配 / 关闭、`TIOCGPTN` 编号解析
- raw 设置与恢复（termios 前后比对）
- 尺寸转发（`TIOCSWINSZ` 后 master 读回一致，尽力而为项不强制断言）
- 回退选择（Prepare 失败 → 现状路径）
- 终端状态自愈（`secure_linux_test.go`）：`ISIG` 被清后恢复、已置位时不动其它 flag、非属主（后台/无终端）时不动、前台组被抢后 `TIOCSPGRP` 夺回（helper 子进程建独立会话 + `Setpgid` 造异组，复用「自身会话内 `tcsetpgrp`」路径）
- 结果退出码（`agent/shell_bridge_test.go`）：`kill -INT $$` 在桥接与非桥接两路均记 130、且不写 `Err`

**集成（自驱动 pty）**：测试自身分配 pty 充当"外层真实 tty"，经 `newBridgeTTY`（§6）注入 bridge，运行 `bash -c 'read x < /dev/tty; echo got:$x'`，从外层 pty 写入并断言收到 `got:...`；同时断言子进程内 `tty` 输出为 `/dev/pts/N`（非 `/dev/tty`）、`/dev/tty` 可打开；断言子进程退出后 master 及时收到 `EIO`（防忘关 slave）

**手工矩阵**

- sudo 密码、ssh 交互、gpg 签名（先 `gpg-connect-agent reloadagent /bye` 清缓存）、`read` 提示
- 超时、Ctrl+Z 挂起、Ctrl+C 透传、窗口 resize
- tanya 内嵌 tanya、`/load` 后交互

**回归**：非交互路径零变化；`go build ./... / go vet ./... / go test -race ./...` 全绿

## 9. 实施清单

| 文件 | 改动 |
|---|---|
| `readline/bridge_linux.go`（新） | pty 分配（`Prepare` 设 `Ctty:0` 与三标准流）+ 真实 tty 打开 + 双向泵 + 初始尺寸/`SIGWINCH` 转发 + raw 管理 + capture 写出 + `stop()` 关闭顺序 + `newBridgeTTY` 注入构造 |
| `readline/bridge_stub.go`（新，非 Linux） | 返回 `ErrUnsupported`，走回退 |
| `agent/tty_bridge.go`（新） | `TTYBridge` 接口（注入点后改为 `agent.WithTTYBridge` Option） |
| `agent/shell.go` | `interactive=true` 且 bridge 可用时走桥接：`cmd.Stdin/Stdout/Stderr` 全接 slave（**不再接管道路径**）、捕获改由 `Attach` 的 `capture=streamCapture` 接入，Start 后关 slave，`defer stop()`；否则回退现状路径 |
| `main.go` | 接线（`readline` 实现注入 agent） |
| `docs/design.md` | 交互模式契约更新（落地后） |
| `AGENTS.md` | 结构树补 `readline/bridge_*`、`agent/tty_bridge.go` |
| 测试 | `readline/bridge_*_test.go`、`agent/shell` 交互路径回归 |
| `readline/secure.go` / `secure_stub.go`（新） | 终端状态自愈：`InitTerminalGuard`（记录启动前台组归属 + 忽略 `SIGTTIN/SIGTTOU`）、`SecureTerminal`（恢复 `ISIG`、夺回前台组）；原语走 `ctty`（`docs/ctty.md`）|
| `readline/bridge_linux.go` | `Prepare` 的前台检查前调用 `SecureTerminal()` |
| `repl/repl.go` / `main.go` | 每回合开始前 / 启动时调用自愈 |
| `agent/shell_platform_posix.go` | `posixExitCode`：信号终止记 `128 + signum` |

## 10. 开放问题

1. "默认全具备"（§2 触发·探索方向）：是否对所有命令默认启用桥接，或按命令特征自动识别
2. 受控输入（tanya 接管输入行 + 掩码）的演进，作为 §2 交互形态的次选
3. 非交互命令 `cmd.Stdin` 是否改绑 `/dev/null`（现状绑 `/dev/tty` 的隐患，超出本次范围）
4. pinentry-curses 等全屏界面在"原样全量"下噪声较高——已按 §7 豁免，若实际不可读再评估
5. macOS/BSD 桥接（`TIOCPTY` 系列）与 Windows ConPTY：当前 stub 回退，未排期
6. 编辑器进 raw 的 `TCSETSF` 会清 typeahead（回合未结束时键入被丢弃，见 §7）：是否改为先 drain 输入缓冲再切 raw
