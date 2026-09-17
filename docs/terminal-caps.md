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
| Windows | Windows Terminal、ConPTY 宿主（VS Code 终端等） | 同上 | 同上（VT 输入路径，实机验证待做） | 继承控制台（阶段 C） |
| macOS | Terminal.app / iTerm2 | 同上（posix 路径） | 同上 | 无 pty（现状，不承诺） |

**不保证**（不写适配分支，出问题不修）：

- 传统 conhost（cmd.exe / 老控制台窗口，无 VT 处理）
- Windows 10 1809 之前（无 `ENABLE_VIRTUAL_TERMINAL_INPUT`）
- 第三方终端模拟器（MSYS2 / mintty / ConEmu / Cygwin）
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
| B0 | 抽平台无关的按键状态机 `keySource`（`readline/terminal_io.go`）：分片只提供 `readChunk` 与可选 `hungUp` | 已实施 |
| B1 | Windows 输入后端（`readline/terminal_windows.go`）：`Raw` 开 `ENABLE_VIRTUAL_TERMINAL_INPUT` 并清 `ECHO/LINE/PROCESSED`、`readChunk` 用 `GetNumberOfConsoleInputEvents` 轮询 5ms + 1s 超时、`Size` 走 `ctty.Size`、`ctty` 加 `ConsoleMode`/`SetConsoleMode`；仅 VT 路径（范围排除 conhost 与 1809 之前，无需 `ReadConsoleInput` 回退）。ghost、补全菜单、历史随 raw 一并生效 | 已实施（实机验证待做） |
| B2 | Windows 交互命令：`interactive: true` = 前台执行 + stdin 继承控制台（不做 ConPTY 桥接） | 待做 |
| B3 | 编辑器输出切控制终端（解决 #2 盲打与提示符污染） | 待做 |
| B4 | Windows 编码链路：`ctty` 代码页原语（LazyDLL 补 7 个 proc）+ `main` 启动 `EnsureUTF8` 切 65001、退出/紧急路径复原；run_shell 捕获侧 `utf8.Valid` 直通、非法时按 `ctty.FallbackCP()` 兜底转码（`shellPlatform.DecodeOutput`，posix 恒等） | 已实施（实机验证待做） |

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

## 8. Windows 输入后端要点（B1）

- **超时语义**：posix 靠 `VMIN=0/VTIME=1` 让 `readChunk` 周期返回 `(0, nil)`，编辑器借此把孤立 `Esc` 判为 Esc；Windows 无对应 read timeout，故用 `GetNumberOfConsoleInputEvents`（LazyDLL，`x/sys/windows` 未封装）轮询 5ms、1s 截止后返回 `(0, nil)`，与 posix 同义
- **只走 VT 路径**：`ENABLE_VIRTUAL_TERMINAL_INPUT` 让控制台把按键转成 VT 字节序列，直接复用 `keyParser`；不做 `ReadConsoleInput` 回退（范围排除 conhost 与 1809 之前）
- **按键编码补充**：`keys.go` 增 `ESC[1~`/`ESC[4~` → Home/End（WT 与部分 xterm 的编码）
- **逃生开关**：`TANYA_NO_RAW_INPUT=1` 让 `openTerminal` 直接返回 `ErrUnsupported`，回落 Degraded（两平台通用，便于对照与故障退避）
- **不在范围**：IME 组合串、Alt 组合键、`ESC O`（F1–F4）；粘贴按多字节序列处理（与 posix 同）
- **实机验证清单（WT 与 ConPTY 宿主各一遍）**：ghost 出现；Tab 多候选菜单（方向键选择、Esc 关闭、收起无残行）；上下键历史；`Ctrl-A/E/B/F/U/K/W/Y/T/L`；左右键与 `Home/End/Delete/Backspace` 编辑；`Ctrl+C` 中断回合、`Ctrl+D` 退出；中文输入；窗口 resize 后菜单与提示符不错位；`TANYA_NO_RAW_INPUT=1` 回落表现为整行读

## 8.5 Windows 编码要点（B4，2026-09-17）

- **问题形态**：① 输入——`ReadFile` 在 `ENABLE_VIRTUAL_TERMINAL_INPUT` 下仍按控制台输入代码页编码交付字节（zh-CN 默认 936/GBK，en-US 默认 437 且无法编码中文），keyParser 按 UTF-8 解析必乱；② 输出——UTF-8 字节被控制台按输出代码页解码渲染，ANSI 序列是 ASCII 不受影响，症状是"颜色正常、唯独文字乱"；③ run_shell 子进程——cmd/PS 5.1 管道输出跟随控制台代码页、pwsh/go/node 写 UTF-8、python/老工具写 locale ANSI，混合编码流被原样送进工具视图与模型上下文
- **双层策略**：正路 = 启动期把控制台双代码页切 65001（代码页是 per-console 属性，派生子进程查询即得 UTF-8，一处切换三条链路全通）；歧路 = 捕获侧 `finish()` 时 `utf8.Valid` 不通过才按快照代码页兜底转码（覆盖硬编码 OEM/ANSI 的漏网者与混合流）
- **快照与转码源**：`EnsureUTF8` 快照原代码页；`FallbackCP` 优先原输出代码页（子进程管道跟随它），本就 65001 或无控制台时回落 `GetOEMCP`；zh-CN 下两者一致为 936
- **容错口径**：转码 flags=0（不用 `MB_ERR_INVALID_CHARS`），截断缝上的半个多字节字符与混合流非法字节落 U+FFFD 而非整体失败；GBK 字节流碰巧整体合法 UTF-8 的概率可忽略（如 `你`= `C4 E3`，`E3` 非 UTF-8 续字节），反方向误转不会发生（合法即直通）；二进制输出会被硬转成垃圾，但二进制进文本视图本就是垃圾，无害
- **接缝细节**：`streamCapture` 头尾缓冲字节级截断可能切开多字节序列——middle==0 时头尾是连续片段，拼接为单缓冲整体解码；middle>0 时两者不连续，各自解码、缝上落 U+FFFD
- **复原**：`main` 以 defer + `exitNow` 收口全部退出路径，紧急强退走 windows 专属 `emergencyRestore`（= `RestoreUTF8`，signals_windows.go）；不复原会导致用户 shell（cmd.exe 缓存代码页）在 tanya 退出后输出错乱
- **不覆盖**：`interactive: true`（B2）子进程直写控制台不经捕获，靠「代码页已切 + 子进程自适应」自洽；管道喂入的非 UTF-8 stdin 无法判源，不做

## 9. 实测记录（2026-09-16，Linux）

用 `script -qec` 造伪终端，跑 `make build` 产出的二进制，统计输出中的 SGR 与其它 CSI：

| 组合 | 构造 | SGR | 其它 CSI | 结论 |
|---|---|---|---|---|
| A 全 tty | `printf 'exit\n' \| script -qec './tanya' /dev/null` | 10 | 2 | 全功能（颜色 + 行编辑重绘）|
| B stdin 管道 + stdout tty | `script -qec 'printf "exit\n" \| ./tanya' /dev/null` | 10 | 0 | 着色恢复（新行为），无行编辑故无光标序列 |
| C stdin tty + stdout 文件 | `printf 'exit\n' \| script -qec './tanya > body' /dev/null` | 0 | 2 | 颜色已关；两处 CSI 为行编辑重绘（阶段 D 缺口）|
| D 全非 tty | `printf 'exit\n' \| ./tanya > out` | 0 | 0 | 全降级（与拆分前一致）|

## 10. 测试与验收

- `ctty`：`Probe` 字段自洽（无 tty 环境不 panic、`SizeOK=false` 时尺寸为零值）；`IsTerminal`/`Size` 对非法 fd 返回 false；Windows 分片随交叉编译校验
- `term.DetectProfile`：表驱动补 `vt=false` 用例
- `repl`：`TermFacts.Width()` 兜底（`ColsOK=false` → 80）；现有渲染 golden 全部走显式注入 `term.Profile`，不随探测变化
- 回归：posix 全量测试 + `-race`；`GOOS=windows/darwin/freebsd` 交叉编译与 `go vet`；Windows 实机验证（WT 与 ConPTY 宿主）列入人工清单
