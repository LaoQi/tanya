# run_shell 组件化：shellTool 设计与实施

状态：**S1–S4 已实施**（组件落在 `agent/shelltool.go`，包级可变状态清零；`AGENTS.md`/`docs/design.md`/`docs/interactive-tty.md` 已同步）。本文保留设计意图与决策，落地偏差见 §15。**更正（2026-09-14）**：§12 表中的 `envSection(profile)` 签名已变为 `envSection(cwd, profile)`（构造期调用一次、`envProbeFunc` 注入删除），见 `docs/design.md`《环境段（envprobe）》。**更正（2026-09-15）**：§7 表中的 `ToolDefs(tool *shellTool)` 已被 `Tool` 接口 + `toolRegistry`（`agent/tools.go`）取代——清单改由 `allTools()` 编译期显式组装、`defs()` 注入 client；`run_shell` 的描述/参数 schema/args 解析随之下沉进 `agent/shelltool.go`，内置三件套进 `agent/builtin.go`。**更正（2026-09-25）**：`run_shell` 已整体外置到 `tools/shell`——`shell.go`/`shelltool.go`/`tty_bridge.go`/`shell_platform*.go` 分别成为 `tools/shell/shell.go`、`tool.go`、`bridge.go`、`platform*.go`，`shellTool`→`Tool`、`shellToolConfig`→`Config`、`newShellTool`→`New`、`ShellResult`→`Result`、`TTYBridge`→`Bridge`、`ResolveShell`→`Resolve`；`Config.Workspace` 由字符串改 `func() string` 活取（`main` 传 `a.Workspace`，`/switch` 后自动跟随、工具不再随换区重建），标准集由 `tools.Standard` 装配注入。下文 §1–§16 的构造与字段结论仍成立，符号名按此对照。

**更正（2026-09-24）**：`envSection` 已整体移出 `agent`——环境段改由 `main` 组装（`main.systemBase` = 内置提示词 + 环境段，经 `agent.WithSystemPrompt` 注入为基座），`agent` 删除 `envSection`/`Agent.env`/`runtimePrompt`/`EnvReporter`，环境段同时去掉 `CWD` 行；shell 执行契约常量改为导出（`ShellTimeoutSec`/`ShellInteractiveTimeoutSec`/`ShellTimeoutLimit`/`ShellMaxOutput`），`ShellInvocation` 增 `Name` 字段供 `main` 拼 SHELL 行，见 `docs/design.md`《系统提示头部（env 段）》。本文其余部分保留当时的实施记录。
相关的诊断与评估过程见 `docs/design.md`《架构》（原 `docs/open-questions.md` B1 的诊断记录已随方案落地清理）。

## 1. 要解决的问题

shell 执行层没有所有者，三条症状同一根因：

- `resolveShellCwd` 是包级自由函数，拿不到"谁的工作区"，只能在**参数穿线**（当前 `base` 参数：`RunShellResult` 6 参、两个相邻 string）与**现查环境**（`os.Getwd()`）之间二选一
- 包级可变状态三组：`shellRuntime`（`InitShell` 缓存 + 懒解析 + mutex）、`shellLookPath`、`ttyBridgeCur`
- LLM client 伸手读包级工具清单：`agent/llm.go` 与 `agent/llm_responses.go` 的请求组装调 `ToolDefs()`，而 `ToolDefs()` 内部读 `ShellRuntime()`

组件化的目标不是"多一个类型"，而是给这些状态一个**所有者**：由 `Agent` 在装配期定格，之后只读（`workspace` 随 `/switch` 由 `Agent.loadWorkspace` 换新实例）。

## 2. 定位与四条硬约束

| 约束 | 含义 |
|---|---|
| 参数可构建 | 构造只吃参数（含注入的函数），不读环境、不依赖包级状态 |
| 可测试 | 解析/展开/桥接/失败路径都可在无 chdir、无全局注入的前提下单测 |
| 可重入 | `run` 可被并发调用；实例字段构造后只读，每调用状态全在栈上 |
| 禁读环境 | 组件内部不得出现 `os.Getwd`/`os.UserHomeDir`/`os.Getenv`/`exec.LookPath`/`runtime.GOOS` |

保留的副作用只有两件（工具本职）：启动子进程、打开 `/dev/tty` 交接终端。
`time.Now()`（只为 `Duration` 字段）记账保留，不做时钟注入（属过度设计；将来要做确定性录制再加 `now func() time.Time`）。

## 3. 类型与文件布局

```go
// agent/shelltool.go（新增）
type shellToolConfig struct {
	Override  string                      // cfg.Shell
	LookPath  func(string) (string, error)
	Home      string                      // ~ 展开用，注入
	Workspace string                      // 相对 cwd 的基准，注入
	Bridge    TTYBridge                   // nil = 不启用桥接
	Programs  []string                    // nil = shellProgramCandidates
}

type shellRequest struct {
	Command     string
	TimeoutSec  int
	Interactive bool
	Cwd         string
}

type shellTool struct {
	profile   *shellProfile          // 只读
	programs  []string               // 只读
	workspace string                 // 只读
	home      string                 // 只读
	bridge    TTYBridge              // 只读
	ttyMu     sync.Mutex             // 唯一可变字段：真实终端租约
}

func newShellTool(cfg shellToolConfig) (*shellTool, error)
func (t *shellTool) run(ctx context.Context, req shellRequest) *ShellResult
func (t *shellTool) resolveCwd(cwd string) (string, error)
func (t *shellTool) toolDesc() string
func (t *shellTool) invocation() string   // env 段 SHELL 行用
```

文件布局：

- `agent/shelltool.go`（新增）：`shellTool` / `shellToolConfig` / `shellRequest` / 构造 / `run` / `resolveCwd` / `toolDesc` / `invocation`
- `agent/shell.go` 退化为"解析器 + 叶子"：`ShellResult`/`ShellChunk`/`streamCapture`/`writeStream`/`shellArgs`/`waitShell`/超时常量 + `shellProfile`/`resolveProfile`/`newProfile`/`probePrograms`（纯构造器，只收 `lookPath`；候选链取自平台抽象 `platform.Candidates`，算法 `firstAvailable` 可注入候选）
- 平台文件收敛为平台抽象：`shell_platform.go`（无 tag，表定义 + `fillDefaults` 零值兜底 + `ProtectTerminalSignals`）、`shell_platform_posix.go`（`linux || darwin`，共享实现）、`shell_platform_linux.go`（`linux`，`/proc` 挂起探测）、`shell_platform_darwin.go`（`darwin`，`sysctl` 挂起探测）、`shell_platform_windows.go`（`windows`，`taskkill` 树杀）、`shell_platform_stub.go`（其余平台），取代 `shell_unix.go`/`shell_other.go`/`shell_proc_*.go`/`shell_candidates_*.go`；tty 组（原 `shell_tty_unix.go`/`shell_tty_stub_unix.go`）后随 `ctty` 抽包删除（`docs/ctty.md`）
- `agent/tty_bridge.go`：保留 `TTYBridge` 接口，删包级注入，改 Option

## 4. 隐式依赖注入表

| 现状（自行读取/全局） | 改造后来源 |
|---|---|
| `os.Getwd()`（`RunShell` 包装、`base` 参数） | `shellToolConfig.Workspace`（`agent.New` 读一次） |
| `os.UserHomeDir()`（`shell.go` 的 `~` 展开） | `shellToolConfig.Home` |
| `runtime.GOOS`（`resolveShellRuntime`/`resolveProfile`） | 删除：平台差异改由编译 tag 装配的 `shellPlatform` 表提供，不再进 config |
| `exec.LookPath`（包级 `shellLookPath` 变量） | `shellToolConfig.LookPath` |
| 包级 `shellRuntime`/`shellRuntimeMu/Cur/Set/Err` | 删除，取值全部来自 config |
| 包级 `ttyBridgeCur`/`ttyBridgeMu` | `shellToolConfig.Bridge`（来自 `WithTTYBridge` Option） |
| `openForegroundTTY()` 打开 `/dev/tty` | 保留（工具本职副作用） |
| `time.Now()` | 保留（记账） |

## 5. 可重入与并发契约

- **字段只读 + 每调用局部状态**：`ShellResult`、`streamCapture`、`exec.Cmd`、pty/tty 句柄、计时全在调用栈上；`run` 可被并发调用而不共享可变状态
- **唯一需要串行的是物理资源**：真实终端（前台进程组 + raw mode）进程内只有一份。凡触碰它的路径——桥接 `Attach`、`ctty.Open` + 前台移交——由实例级 `ttyMu` 串行。这不是妥协而是物理限制的显式化：两个子进程不可能同时拥有终端前台组
- **今天等价于 `run` 串行**：当前所有调用都尝试交接终端。将来要真正并行，需要的不是拆锁，而是把"是否需要终端"变成显式输入（`shellRequest` 加字段，或按 `Interactive` 与宿主能力判定），让读文件/grep 之类完全不碰 `ttyMu`；这一步留给并行调度落地时做（见本节末"并行工具调用：预留"），组件内部到时无需改动
- **并发验收用例**：`-race` 下 N 个 goroutine 并发 `run` 非交互命令，断言各自 `ShellResult`（stdout/退出码/耗时）互不串扰

**并行工具调用：预留，不实施**（原 `docs/open-questions.md` A8；`agent` 的 `runTurn` 现为顺序 `for _, tc := range resp.ToolCalls` + 顺序 `EventToolStart/End` + 顺序 append history）：

- **需求侧：无**。当前唯一主工具是 `run_shell`（`builtin` 三个为小型纯计算），模型一次返回多个 `tool_calls` 时顺序执行是正确行为——顺序确定、事件不交错、history 顺序稳定；并行只省墙钟时间，而同一回合的多条命令常有数据依赖。协议层支持多 `tool_calls`（chat 按 `tc.Index` 合并增量、responses 按 item）
- **组件侧已就绪**：字段构造后只读 + 每调用状态全在栈上 = `run` 可并发调用；`shellRequest` 是纯请求值对象、组件不回连调度层 → 并行调度落地时组件内部无需改动
- **真正的约束（落地时的核心决策）**：并行与"stdin 直通可应答密码"互斥——`runShellForeground` 无条件 `openForegroundTTY()` + 交接前台组 + stdin 接 tty，而同一时刻只有一个进程组能拥有终端前台。并行化必须选一种降级契约：并行批次中最多一个 interactive、其余降级为无 tty stdin（牺牲应答能力）；或仅对显式声明不需终端的命令并行
- **届时改动清单**（全在调度侧）：① 把"是否需要终端"变成显式输入（`shellRequest` 加字段，或按批次约定判定——§13 第 4 条已预留该决策），使不碰终端的调用不拿 `ttyMu`；② `EventToolStart/End` 填 `ToolIndex`/`ToolID`（字段已在 `agent/event.go`，当前只有流式 `EventToolCall` 填）；③ history 按 index 收敛（并行执行、**顺序 append**，协议要求 tool 消息与 `tool_calls` 一一对应且同序）；④ `toolView` 支持多块（现为单块状态机：`dirty`/`justEnded` + 单条 `heartbeat`；渲染已追加化，无"上移 N 行"）并定义中断时部分结果的收敛语义
- **不变量**：不得现在加死字段（无写入方、无判定方的 `NeedsTTY` 之类，与已清掉的 `Profile.Unicode`/`Level256`/`RGB` 同类）

## 6. 装配点（`agent.New`）

```go
cwd, err := os.Getwd()                       // 装配点读一次
home, _ := os.UserHomeDir()                  // 装配点读一次
tool, err := newShellTool(shellToolConfig{
	Override: cfg.Shell,
	LookPath: exec.LookPath, Home: home,
	Workspace: cwd, Bridge: bridge,      // 来自 WithTTYBridge Option
})
if err != nil {
	return nil, err                      // 保留"启动即要求可用 shell、无降级路径"
}
client := NewClient(cfg, ToolDefs(tool.profile))   // 工具清单随 client 定格
```

## 7. 消费者改造

| 位置（组件化前） | 现状（组件化前） | 改后 |
|---|---|---|
| `dispatch`（`agent.go`） | `RunShellResult(ctx, cmd, timeout, interactive, cwd, a.cwd)` | `a.tool.run(ctx, shellRequest{Command:…, Cwd:…, TimeoutSec:…, Interactive: interactive})` |
| `envSection`（`envprobe.go`） | 内部读包级 `ShellRuntime().profile` | 签名加 `profile *shellProfile`（保持纯函数）；`runtimePrompt()` 传 `a.tool.profile` |
| `describeShell`（原 `runShellDesc` 转发层，`agent.go` → `shelltool.go`） | 收 `*shellRuntime` | 收 `shellPlatform` + `*shellProfile` + `[]string`（描述还需要可用程序清单；能力句、清单与平台名同取自平台抽象 `platform.Capabilities`/`platform.Programs`/`platform.GOOS`，2026-09-16 收敛 GOOS 到表并删转发层） |
| `ToolDefs()`（`agent.go`） | 内部读 `ShellRuntime()` | 纯函数 `ToolDefs(tool *shellTool)`（描述依赖 profile+programs，收组件而非单 profile） |
| `agent/llm.go` / `agent/llm_responses.go` | 每次请求调包级 `ToolDefs()` | `NewClient(cfg, tools []ToolDef)` 构造期注入，请求组装读 `c.tools` |
| `main.go` | `agent.InitTTYBridge(readline.NewTTYBridge())` | `agent.New(cfg, agent.NoSave(*noSave), agent.WithTTYBridge(readline.NewTTYBridge()))` |
| `Option`/`NoSave`（`agent.go`） | `func(*Agent)`，选项在构造后应用 | `func(*Options)`（`Options{noSave, bridge}`），构造前算出，bridge 供 `newShellTool` 使用 |
| `shell_test.go` | `RunShell(ctx, cmd, timeout)` | 测试内构造 runner 后 `run(...)` |

## 8. 删除 / 保留 / 新增

**删除**（已完成，`rg` 归零）：`InitShell`、`ShellRuntime`、`shellRuntime`、`shellRuntimeMu`/`shellRuntimeCur`/`shellRuntimeSet`/`shellRuntimeErr`、`shellLookPath`、`resolveShellRuntime`、`RunShell`、`RunShellResult`、`resolveShellCwd`（→ `shellTool.resolveCwd` 方法）、`InitTTYBridge`/`currentTTYBridge`/`ttyBridgeMu`/`ttyBridgeCur`。

**保留**：`ShellResult`/`String()`/`ShellChunk`/`streamCapture`/`writeStream`/`shellArgs`/`shellExitCode`/`effectiveShellTimeout`/`statState`/`waitShell`；`shellProfile`/`resolveProfile`/`newProfile`/`probePrograms`；进程/信号平台文件（后续收敛为平台抽象 `shell_platform*.go`：`shellExitCode`→`platform.ExitCode`、`statState` 移入 posix 平台文件，见 `docs/design.md`《shell》）；tty 能力常量后迁至 `ctty.Supported`（`docs/ctty.md`）。

**新增**（已完成）：`shellTool`、`shellToolConfig`、`shellRequest`、`Options`、`WithTTYBridge`。

## 9. 不变量（改造必须保持）

1. shell 解析失败 → `New` 报错退出，无 noshell 降级路径
2. profile 非空不变量：`run_shell` 恒定注册、env 段恒定输出 SHELL/TIMEOUT/OUTPUT 行
3. 工具清单**顺序与内容字节级不变**（prompt cache 依赖，见 `docs/cache-probe.md`）
4. 进程 cwd 不变（全程不 `os.Chdir`）；默认目录取当前工作区（`/switch` 后随工作区变），`cwd` 参数只设 `cmd.Dir`，且桥接与回退两条路径都设置
5. bridge 未注入或 `Prepare` 失败 → 回退 foreground
6. 输出头尾截断、超时（60/300/900）、`128 + signum`、中断与挂起标记语义不变

## 10. 测试策略

| 测试类型 | 现状做法 | 改造后 |
|---|---|---|
| profile 解析 | 改包级 `shellRuntime`（`withShellRuntime`）+ `InitShell` 缓存语义 | 算法层注入候选（`firstAvailable([]string{"pwsh", "powershell"}, stub)`）覆盖优先级与错误路径，平台候选链由 tag 化用例断言（`shell_platform_{posix,windows}_test.go`），表完整性由无 tag 用例守卫（`TestPlatformComplete`）——已落地（`TestFirstAvailable*`/`TestResolveProfileUsesPlatformCandidates`/`TestNewShellTool*`） |
| 相对 cwd | 依赖进程 cwd 或 chdir | `Workspace` 注入，**不再需要 chdir**——已落地（`TestShellToolResolveCwdInjected`） |
| `~` 展开 | 依赖真实 `$HOME` | `Home` 注入，可测成功与失败两条分支——已落地（含 `~user` 不支持、基准缺失） |
| 桥接路径 | `InitTTYBridge(fake)` + Cleanup 复原 | 构造时注入 fake bridge——已落地（`bridgeTool(t, fake)`，全局桩用例删除） |
| 并发 | 不可能 | 并发 `run` 用例（§5）——已落地（`TestShellToolConcurrentRun`，8 路 `-race` 绿） |
| 门控 E2E | `TTY_E2E` / `TTY_E2E_REUSE` | 保持，用于验证组件化未破坏真实 tty 路径 |

副作用：**测试顺序无关**——不再存在"包级值被先跑的测试定格"的路径（本轮 `cwd` 实现中曾被这一点咬到）。

## 11. 分步实施

| 步 | 内容 | 验证 |
|---|---|---|
| S1 | 新增 `shelltool.go`；`Agent` 持有 `a.tool`；`dispatch` 改走组件；`resolveCwd` 读 `t.workspace`（`base` 参数与 6 参形态消失）；旧包级入口暂留 | ✅ 与 S2 合并落地（S2 删掉包级状态后 `RunShellResult` 无法编译，故旧入口未暂留，直接随 S4 清理掉） |
| S2 | `envSection(profile)`、`runShellDesc(profile)`、`ToolDefs` + client 注入；删全部 shell 包级状态与 `shellLookPath` | ✅ 全量 + `-race` 绿；`rg` 归零；工具清单与 env 段经 worktree 对拍与 HEAD **逐字节一致** |
| S3 | `WithTTYBridge` Option，删 `InitTTYBridge`/`currentTTYBridge`；main 改接线 | ✅ 已完成（`Options{noSave, bridge}`；门控 E2E 需真实 tty，未在本机执行） |
| S4 | 清理导出面（删 `RunShell`）；测试 helper 收敛；新增并发用例；文档同步（AGENTS/design/interactive-tty） | ✅ 已完成（`RunShell*` 随 S2 删除；helper 见 §10；并发用例 `-race` 绿；三份文档已同步） |

## 12. 收益 / 代价

**收益**：工作区归属明确（参数之争根治）；包级可变状态净减三组；LLM client 不再伸手读包级工具清单；测试注入从"改全局 + 复原"变成"构造时给值"，并顺带消除测试顺序耦合；`Agent` 少一类职责，为拆分铺接缝。

**代价**：`New` 每次构造多约 21 次 `LookPath`（可从组件上移到进程级 memo，若实测在意）；改动跨 `shell.go`/`agent.go`/`envprobe.go`/`llm.go`/`llm_responses.go`/`tty_bridge.go`/`main.go` + 5 个测试文件；删除部分导出符号（本仓库惯例：模块非公共库，仓库内调用点同步）。

## 13. 决策记录（全部按推荐采纳）

1. bridge 与 shell 同轮组件化（S3），同属"去包级单例"
2. `ToolDefs` 改纯函数 + client 构造注入（而非让 client 继续读包级/Agent）
3. `HOME` 也走注入（由"禁读环境"直接推出）
4. "终端租约"暂不提前提为 `shellRequest` 字段，等并行调度落地时再加，避免猜错抽象
5. `time.Now()` 不注入（记账）

## 14. 与当前代码的关系

原 `cwd` 参数实现（`RunShellResult` 6 参 + `base`）的过渡形态已随本轮删除：`base` 被 `shellTool.workspace` 取代，请求形态收敛为 `shellRequest`，`docs/design.md` 的 run_shell「执行目录」段与组件化说明已同步改写。

## 15. 落地偏差记录（实施时定稿）

1. `ToolDefs(tool *shellTool)` 而非 `ToolDefs(profile)`：`run_shell` 描述同时依赖 `profile` 与 `programs`，收组件避免两个参数各传一半；实现上 `ToolDefs` 调 `tool.toolDesc()`（`toolDesc` 是设计的组件方法，由此有了实际调用点）
2. `envSection(cwd, probe, profile)` 保留 `profile` 参数而不收组件：守"纯函数/收参数"红线，组件只作为取 profile 的来源（`a.tool.profile`）
3. `shellTool.invocation()` 未落地：env 段的 SHELL 行直接用 `profile.invocation()`，包一层方法只会成为死代码
4. `RunShell`/`RunShellResult`/`resolveShellCwd` 提前到本轮删除：S2 删包级状态后它们无法编译，故 S1 未"暂留"旧入口
5. S1 原计划的"过渡双解析"（`New` 里既 `InitShell` 又 `newShellTool`）未出现：改用单一解析路径（`New` 只调 `newShellTool`），避免 `shell:` 覆盖在描述/环境段短暂失效
6. `Option` 签名从 `func(*Agent)` 改为 `func(*Options)`：`bridge` 必须在构造 shellTool 之前算出来，而 `NoSave` 仍是 `Agent` 字段（构造后赋值）；`NoSave`/新增 `WithTTYBridge` 的对外用法不变
7. A6（原 B6，构造缺项失败语义）定调为沿用 `MsgBadCwd`，不新增文案、不 panic
8. `newShellTool` 的 `Programs` 为 nil 时才探测（`nil` = 未注入）；空切片 `[]string{}` 视为"显式无程序"

## 16. 边界：明确不做的事

组件化收口时一并定下的相邻结论（原件 `docs/open-questions.md` 已删除，内容归于此与 `docs/design.md`/`docs/style-split.md`）：

- **不新立进程事实层（`env` 包）**：`cwd` 的老问题不是"缺全局暴露点"，而是"shell 层没有所有者"——已由 `shellTool` 解决（§1–§4）。进程事实就此收口，不再另设包级暴露点
- **不用 `context` 承载进程事实**：仓库里没有会话级 `ctx`（`repl.Run()` 不收 ctx；ctx 每回合由 `InterruptContext()` 现造、以 `context.Background()` 为根，只承担取消）；读取点之一（cwd 短路径显示）在渲染路径上、手上没有 ctx；`context.Value` 返回 `any` 无编译期保证，且全仓 `context.WithValue` 使用数为 0。结论：ctx 属请求层（取消/超时），进程事实不进 ctx
- **构造缺项的失败语义**：`Workspace`/`Home` 为空（构造方漏传）时沿用 `MsgBadCwd`（`resolveCwd` 对相对路径与 `~` 分别报错），不新增专用文案、不 panic（§15 第 7 条）
- **旧 `cwd` 过渡形态已删**：`RunShellResult` 六参 + `base` 参数不复存在，请求形态就是 `shellRequest`（§14）
