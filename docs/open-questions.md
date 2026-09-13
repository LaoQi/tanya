# 会话遗留：已定结论与待评估点

本文件记录一轮设计讨论（2026-09，`run_shell` 引入 `cwd` 参数之后）产出的**已定结论**与**待决点**：A 节防重复讨论，B 节是待评估/待决策清单，C 节是已知行为（防误报）。
实施类设计另行归档：`docs/shell-tool.md`（run_shell 组件化，已实施；见该文 §15 偏差记录）。

## A. 已定结论（登记，不再重复讨论）

### A1 进程事实层（`env` 包）：不立

`cwd` 的问题不是"缺全局暴露点"，而是"shell 层没有所有者"——已由 shellTool 组件化解决（`docs/shell-tool.md`）。进程事实就此收口：不新立 `env` 包，归属结论见 A2/A3。

### A2 不用 `context` 承载进程事实

- 仓库里没有会话级 `ctx`：`repl.Run()` 不收 ctx（`repl.go:147`），ctx 每回合由 `InterruptContext()` 现造、以 `context.Background()` 为根（`repl.go:197`），只承担取消
- 读取点之一（`repl.cwdLabel`）在渲染路径上，手上没有 ctx；要覆盖它就得让 REPL 再存一份，等于又造了个属性
- `context.Value` 返回 `any`、无编译期保证；缺值时只是运行期查 key，失败模式与"全局没设"一样但更难追
- 全仓 `context.WithValue` 使用数 = 0；引入即新范式，且只服务一个字符串
- 结论：ctx 属请求层（取消/超时），进程事实不进 ctx

### A3 tty 事实与颜色能力不设共享层

- 颜色能力/`style.Profile`：只有 `style.DetectProfile` 计算（自身读 `NO_COLOR`/`TERM`/`COLORTERM`/`WT_SESSION`/`TANYA_COLOR`），消费方是 style 自身（`template.go:28`、`filter.go:23`）与 repl 构造期快照（`repl.go:75`）→ 消费方独占，无需共享暴露点
- 终端尺寸：实时值（`ToolWidth` 作为 `func() int` 传给 `NewToolView`；`readline/editor.go` 换行时现查）→ 属"会变"，不进
- 启动前台状态/`ISIG` 修复：readline 独占（`InitTerminalGuard`/`SecureTerminal`）
- `ttyStdinSupported()` 是编译期平台常量（`shell_tty_unix.go:19`），消费方是 agent 的 env 段与 stdin 直通语义
- **自纠**：此前"tty 事实被三处各自判定同一事实"的说法不成立——三处问的不是同一个问题（stdin 直通能力 / 输出是否 tty / 尺寸与前台归属），不构成重复采样

### A4 当前 `cwd` 形态属过渡

原 `RunShellResult(ctx, cmd, timeout, interactive, cwd, base)` 六参形态（`base` 由 `dispatch` 传 `a.cwd`）**已删除**：`docs/shell-tool.md` 的组件化落地后，请求形态为 `shellRequest{Command, TimeoutSec, Interactive, Cwd}`，工作区基准由 `shellTool.workspace`（`agent.New` 构造期注入）承担，`RunShell`/`RunShellResult`/`resolveShellCwd` 均不存在。

## B. 待决 / 待评估

### B1 `Agent` 是否拆分（god struct 诊断）——已实施

诊断数据（拆分前基线：`agent/agent.go:29-45`，16 字段 / 30 方法 / 660 行）：

| 簇 | 字段 | 生命周期 |
|---|---|---|
| 配置 | `cfg` | 会话（可变：Model/Effort 被 repl 改） |
| 端口 | `client` | 进程，只读 |
| 会话状态 | `history`(85 引用) | 会话 |
| 进程事实 | `cwd`、`probe` | 进程 |
| 提示词缓存 | `promptSnapshot`、`legacySystem` | 会话 |
| 持久化 | `sessionDir`(27)、`sessionPath`(10)、`saved`(7)、`systemSaved`(6)、`sessionCache`、`sessionStat`、`noSave` | 会话/长驻 |
| 回合/UI 指标 | `lastUsage`(31) | 回合（每回合刷新、Load/NewSession 清） |

三条判据均成立：① 六类生命周期挤在一个类型；② 30 个方法里 13 个是只碰 1–2 字段的薄访问器/格式化器（`Model`/`SetModel`/`ReasoningEffort`/`SetReasoningEffort`/`ToolOutputLines`/`ListModels`/`NoSave`/`History` + `ContextInfo`/`PromptUsage`/`PromptCache`/`PromptCacheRate`/`PromptSummary`）；③ 五类互不相关的变化（存储格式/提示词模板/状态行显示/模型交互/工具新增）都改它；repl 的接触点约 20 个。

建议的接缝顺序（未决）：先切无状态两簇（状态行格式化 → 纯函数；提示词组装 → 组装器，`cwd` 在此定格），再切持久化簇（`sessionStore`），剩下 `Agent` = `cfg` + `client` + `history` + 循环 + 分发，`Ask` 为唯一入口。
**结案（已实施）**：立项文档 `docs/agent-split.md`；与 shellTool 的先后关系为先 shellTool（已完成）。落地为「内聚重组 + `Agent` 留门面」：`agent/stats.go`（`usageStats`）、`agent/prompt.go`（`promptBuilder`）、`agent/session.go`（`sessionStore`）三簇剥离，`main.go`/`repl/` 零改动；字段 16 → 9、方法 26 → 25、`agent/agent.go` 690 → 407 行。偏差记录见该文 §10。

### B3 `dispatch` 与 `toolInteractive` 重复解析参数（已实施）

原状（拆分前行号）：`toolInteractive`（`agent.go:286`）与 `dispatch`（`agent.go:311`）各解析一遍 `tc.Function.Arguments`，新增 `cwd` 后两个匿名 struct 分叉。

落地：收进单一结构 `runShellArgs{Command, Timeout, Cwd, Interactive}` + `parseRunShellArgs`；`runTurn` 只对 `run_shell` 解析一次（拿到 `args, argErr`），`Interactive` 供 `EventToolStart/End` 用，`dispatch(ctx, tc, args, argErr)` 复用同一份（不再自己 Unmarshal）；`toolInteractive` 删除。行为等价（事件 `Interactive`、参数解析失败的 `MsgParseArgs` 文本均不变，含 JSON 坏值时事件 `Interactive=false`）。

待补测试（并入测试补充批次）：`runTurn` 级断言事件 `Interactive` 与坏 JSON 的 `参数解析失败` 结果（本次以一次性临时用例验证过，未留档）。

### B4 工具块不显示 `cwd`（已实施）

原状：模型可见文本含 `cwd: <路径>`（`ShellResult.String()` 只喂 history/`Content()`），但 repl 工具块标题只渲染 command，用户看不到命令落点。

落地：`toolArgsDisplay` 返回 `(cwd, 命令)`，**仅显式指定** `cwd` 时标题区渲染三行——首行 `▸ run_shell ⋯`（结束重绘时无 `⋯`），其后 `cwd: <原样值>` 与命令各占一行（逐行截断；inline 重绘按标题行数上移，未指定时为 1 行、行为不变）；`interactive` 不额外标注（已有 `⏎ 等待终端输入` 引导行）。用例：`TestToolArgsDisplayCwd`、`TestRenderToolStartCwd`、`TestRenderToolStartCwdWidth`。

### B5 测试环境治理（已实施）

**原状**：`agent/agent_test.go:18`、`:267` 与 `repl/settle_test.go:28` 使用裸 `os.Chdir(t.TempDir())` + `t.Setenv("HOME")`；包级状态与这类用例叠加会产生"单跑通过、全量失败"的顺序相关故障。

**落地（选 `TestMain` 基线隔离，非参数注入）**：
- `agent/main_test.go`、`repl/main_test.go` 各加 `TestMain`：进程级把 `HOME` 指到临时目录、`cwd` 切到临时目录（跑完恢复并 `RemoveAll`）。这样任何**漏掉隔离**的用例都不会读开发者真实 `~/.config/tanyan/config.yaml`、不会往真实 `~/.local/share/tanyan/sessions` 建 workspace 目录、也不会把仓库 `AGENTS.md` 读进 system prompt（此前真实 sessions 目录里就留有历史测试写下的 `tmp-mockllm-*`）
- 每条包新增 `TestProcessEnvIsolated` 守卫（断言 `HOME`/`cwd` 均在 `os.TempDir()` 下）——否则 `TestMain` 被删掉没有任何测试会失败
- `agent.TestNewLocalMode` 的裸 chdir/defer 换成既有 `isolatePromptEnv(t)`；`repl.newSettleAgent` 去掉 `t.Setenv("HOME")`+chdir（基线已覆盖），保留 `GlobalSession` 覆盖
- 未选参数注入：`agent.New` 读 `os.Getwd()` 是运行时事实，为测试加 `WithWorkspace` 会污染生产 API；`TestMain` 用 15 行解决同一问题（Go 1.22 无 `t.Chdir`）

**验证**：`go test ./...`、`-shuffle=on`、`-race` 全绿；全量跑前后对拍——真实 sessions 目录条目数不变、仓库 `.tanya` 文件数不变、`/tmp` 无 `*-home-*`/`*-wd-*` 残留（`TestMain` 清理生效）。

**剩余**：`isolatePromptEnv` 仍按用例 chdir（需要"每用例一个干净 cwd"的用例继续用它），属有意保留；`agent`/`repl` 两处 `isolateProcessEnv` 有重复（共享需新建内部包，按极简优先不引入）。

### B6 `shellTool` 构造缺失项的失败语义（已定）

`Workspace`/`Home` 为空时（构造方漏传）：沿用 `MsgBadCwd`（`shellTool.resolveCwd` 对相对路径与 `~` 分别报错），不新增专用文案、不 panic。已随组件化落地。

### B7 Windows 支持面

`~\foo` 不展开（`shellTool.resolveCwd` 只看 `/` 分隔符）；`readline/terminal_windows.go` 仍是占位（不支持 cmd/老 conhost）。待决：是否登记为已知限制并写进 README，或补 `~\` 展开。

### B8 并行工具调用（组件之外）

`runTurn`（`agent.go:183`）现为顺序 dispatch、顺序 `EventToolStart/End`、顺序 append history。并行化需要事件与 history 的按 index 收敛方案；`docs/shell-tool.md` §5 已声明组件侧（可重入 + 终端租约）就绪，调度侧未设计。

### B9 `shellTool` 实施本身（已完成）

S1–S4 已实施：`agent/shelltool.go` 组件 + 包级状态清零 + `WithTTYBridge` Option + 测试收敛（含 8 路并发 `-race` 用例）；偏差记录见 `docs/shell-tool.md` §15。

## C. 已知行为（防误报）

- C1 `~user` 形式不支持：只处理 `~` 与 `~/x`；`~foo` 会按相对路径解析并因不存在而报错。实现保留该支持但**不在工具描述与 README 中宣传**（波浪号属 shell 惯例的默认契约）
- C2 进程全程不 `os.Chdir`：`cwd` 参数只设 `cmd.Dir`，不改变进程状态；因此 `b` 目录调用不影响后续调用
- C3 `cwd: <路径>` 行仅在**显式指定** `cwd` 时出现；未指定时输出与旧版逐字节一致（`TestRunShellCwdDefault` 锁定）
- C4 tty/颜色事实的分散是有意的（消费方独占），见 A3
- C5 `agent/shell_cwd_test.go` 中原 `TestResolveShellCwd`（含 `home, _ := os.UserHomeDir()` 未查错）已随组件化删除：`Home` 改为注入，`~` 展开用例由 `TestShellToolResolveCwdInjected` 覆盖（`t.TempDir()` 作 home，无需读 `$HOME`）：`agent/shelltool_test.go` 的 `testShellTool` 默认 home 亦为该未查错形态（`HOME` 缺失时 `~`/`~/` 用例会退化成断言 `"" == ""` 而掩盖失败）——已修为查错 + 非空断言（review 遗留已闭环）
