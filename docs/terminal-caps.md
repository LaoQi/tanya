# 终端探测与能力降级：范围约定与实施

状态：**S1–S2 已实施**（`ctty` 探测原语 + `Facts`；`main` 单点探测，渲染判定改为 stdout、输入判定保留 stdin）；阶段 B（Windows 输入后端）与 C（Windows 交互命令）待做。本文承载支持范围约定、判定口径与分阶段计划。

## 1. 问题

能力降级粒度被压成一个 bool：`readline.NewTerminal() (Terminal, bool)` 的返回值同时被当作 stdin 是否终端、能否画 ANSI、能否逐键读三件事用，`ctty.Supported`（编译期常量）又被当运行时能力用。后果：

- Windows Terminal 下 `newUnixTerminal` 直接返回 `ErrUnsupported` → 连带颜色、状态行、markdown、宽度全部降级
- `tanya > log`（stdin 是终端、stdout 重定向）时颜色与状态行照写管道/文件
- 降级原因不可解释（没有地方能回答"为什么现在是纯文本"）

顺带发现一处既有不一致：宽度一直按 stdout 探测（`unix.IoctlGetWinsize(os.Stdout)`），颜色与状态行却按 stdin 判定。

## 2. 支持范围（约定）

| 平台 | 终端环境 | 显示 | 输入 | run_shell interactive |
|---|---|---|---|---|
| Linux | VT 兼容终端（xterm 系、tmux、ssh 会话） | 16 色 + 状态行 + markdown + 真实宽度 | 行编辑/历史/补全/ghost | pty 桥接（现状） |
| Windows | Windows Terminal、ConPTY 宿主（VS Code 终端等） | 同上 | 阶段 B 后同上 | 继承控制台（阶段 C） |
| macOS | Terminal.app / iTerm2 | 同上（posix 路径） | 同上 | 无 pty（现状，不承诺） |

**不保证**（不写适配分支，出问题不修）：

- 传统 conhost（cmd.exe / 老控制台窗口，无 VT 处理）
- Windows 10 1809 之前（无 `ENABLE_VIRTUAL_TERMINAL_INPUT`）
- 第三方终端模拟器（MSYS2 / mintty / ConEmu / Cygwin）
- 非 UTF-8 代码页（`chcp` 非 65001）
- 输入法组合串、鼠标事件、括号粘贴等未实现特性
- macOS / Windows 上 pty 桥接的 interactive 语义（改用继承式替代）

**范围内的合理变化**：

1. `tanya > log`、`tanya | less`：输出纯文本，不再写 ANSI/状态行/markdown 装饰（修掉现状污染）
2. `cat x | tanya`：恢复颜色、状态行、markdown、分隔线
3. Windows（WT/ConPTY）：恢复 16 色、状态行、markdown、真实宽度
4. 全终端 / 全非终端：行为与拆分前一致

## 3. 判定口径

| 能力 | 判定主语 | 探测点 |
|---|---|---|
| 颜色、状态行、markdown、分隔线、宽度 | **stdout 是否终端** | `ctty.Facts.StdoutTTY` / `Size` |
| 行编辑、历史、补全、ghost | **stdin 是否终端** ∧ 平台输入后端可用 | `ctty.Facts.StdinTTY` ∧ `readline` 后端 |
| 控制终端原语（前台移交、`/dev/tty`） | 编译期平台上限（`ctty.Supported`）∧ 运行时前台判定 | `ctty` |

颜色判据链（`term.DetectProfile`）：`-p/plain` > `colors` 配置 > `TANYA_COLOR` > `NO_COLOR` > `TERM=dumb` > `StdoutTTY && VT`。`TERM=dumb` 只作用于颜色档，不再是跨能力开关。VT 由 `ctty.EnableVT` 探测（Windows 幂等开启 `ENABLE_VIRTUAL_TERMINAL_PROCESSING`；posix 恒真）。

env 段的 TTY 行保持编译期判据（`ctty.Supported`）：该行文案描述的是 `/dev/tty` 事实，属平台文案而非探测结果。

## 4. 组合矩阵

| # | stdin | stdout | 典型命令 | 拆分前 | 拆分后 |
|---|---|---|---|---|---|
| 1 | tty | tty | `tanya` | 全功能 | 全功能 |
| 2 | tty | 非 tty | `tanya > log` | 行编辑可用；颜色/状态行/markdown 写进文件 | 行编辑保留（提示符仍写 stdout）；输出纯文本 |
| 3 | 非 tty | tty | `cat x \| tanya` | 逐行输入；输出无色、无状态行 | 逐行输入；输出恢复颜色/状态行/markdown |
| 4 | 非 tty | 非 tty | CI、`cat x \| tanya > y` | 全降级 | 全降级 |
| 5 | tty | tty（stderr 重定向） | `tanya 2> err.log` | 不受影响 | 不受影响 |

#2 的行编辑是已知取舍：`Editor` 的提示符与重绘写 stdout，stdout 非终端时用户盲打、提示符进文件。彻底解法是把编辑器输出切到控制终端（posix `/dev/tty`、Windows `CONOUT$`），与阶段 B 同批做，本轮不做。

## 5. 结构与落点

```
ctty/                     事实与原语（零依赖叶子）
  ctty.go                  Facts{StdinTTY, StdoutTTY, Cols, Rows, SizeOK, VT, Kind} + Probe()
  ctty_posix.go            IsTerminal / Size / EnableVT / ConsoleKind（linux||darwin）
  ctty_windows.go          IsTerminal(GetConsoleMode) / Size(GetConsoleScreenBufferInfo)
                           / EnableVT(SetConsoleMode) / ConsoleKind(WT_SESSION、TERM_PROGRAM)
  ctty_probe_stub.go       其余平台保守实现（全 false）
  ctty_stub.go             !linux && !darwin：控制终端原语 no-op（含 Windows）

render/term/profile.go     DetectProfile(isTTY, vt bool)（判据链单一入口）
readline/                  NewTerminal() bool 语义明确为"输入后端可用"；输入判定 = StdinTTY ∧ 后端
repl/                      TermFacts{Cols, ColsOK} + Width()；WithTermFacts 注入（注入即权威，未注入才回落
                           Terminal.Size），删除包级
                           toolTerm/toolTTY 懒缓存与 ToolTTY/ToolWidth
main.go                    唯一探测点：ctty.Probe() → term.DetectProfile → repl.TermFacts
```

依赖口径：阶段 B 起使用 `golang.org/x/sys/windows`（与 `x/sys/unix` 同模块），不新增模块依赖。

## 6. 分阶段

| 阶段 | 内容 | 状态 |
|---|---|---|
| S1 | `ctty` 探测原语与 `Facts`（posix / windows / stub 分片 + 单测） | 已实施 |
| S2 | `main` 单点探测；`term.DetectProfile` 加 vt；`repl` 删除包级懒缓存、改注入 | 已实施 |
| B | Windows 输入后端：`Raw()` 开 VT input、`ReadKey()` 复用 keyParser、`Size()` 走控制台 API；仅 VT 路径（范围排除了 conhost 与 1809 之前，无需 `ReadConsoleInput` 回退） | 待做 |
| C | Windows 交互命令：`interactive: true` = 前台执行 + stdin 继承控制台（不做 ConPTY 桥接） | 待做 |
| D | 编辑器输出切控制终端（解决 #2 盲打与提示符污染） | 待做 |

## 7. 决策记录

| # | 决策 | 理由 |
|---|---|---|
| T1 | 渲染类判定用 stdout、输入类用 stdin | 与"输出是否可 diff"和"输入能否逐键"各自的事实对齐；宽度早已按 stdout 探 |
| T2 | `ctty.Supported` 保留为编译期上限 | 运行时探测在其内叠加，避免 Windows stub 被误判可用 |
| T3 | `TERM=dumb` 只留颜色判据 | 拆开后各能力各判，不再有跨能力总开关 |
| T4 | 引入 `x/sys/windows` | 同模块另一包，`SetConsoleMode`/`GetConsoleScreenBufferInfo` 现成封装 |
| T5 | 不做 conhost/老系统适配分支 | 范围外；阶段 B 因此只写 VT 输入路径 |
| T6 | `#2` 暂不修行编辑盲打（阶段 D） | 本轮只拉直降级链路；彻底解法与输入后端同批做更省 |
| T7 | env 段 TTY 行保持 `ctty.Supported` | 该行是 `/dev/tty` 平台文案，不是探测结果 |

## 8. 实测记录（2026-09-16，Linux）

用 `script -qec` 造伪终端，跑 `make build` 产出的二进制，统计输出中的 SGR 与其它 CSI：

| 组合 | 构造 | SGR | 其它 CSI | 结论 |
|---|---|---|---|---|
| A 全 tty | `printf 'exit\n' \| script -qec './tanya' /dev/null` | 10 | 2 | 全功能（颜色 + 行编辑重绘）|
| B stdin 管道 + stdout tty | `script -qec 'printf "exit\n" \| ./tanya' /dev/null` | 10 | 0 | 着色恢复（新行为），无行编辑故无光标序列 |
| C stdin tty + stdout 文件 | `printf 'exit\n' \| script -qec './tanya > body' /dev/null` | 0 | 2 | 颜色已关；两处 CSI 为行编辑重绘（阶段 D 缺口）|
| D 全非 tty | `printf 'exit\n' \| ./tanya > out` | 0 | 0 | 全降级（与拆分前一致）|

## 9. 测试与验收

- `ctty`：`Probe` 字段自洽（无 tty 环境不 panic、`SizeOK=false` 时尺寸为零值）；`IsTerminal`/`Size` 对非法 fd 返回 false；Windows 分片随交叉编译校验
- `term.DetectProfile`：表驱动补 `vt=false` 用例
- `repl`：`TermFacts.Width()` 兜底（`ColsOK=false` → 80）；现有渲染 golden 全部走显式注入 `term.Profile`，不随探测变化
- 回归：posix 全量测试 + `-race`；`GOOS=windows/darwin/freebsd` 交叉编译与 `go vet`；Windows 实机验证（WT 与 ConPTY 宿主）列入人工清单
