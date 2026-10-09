# tanya

极简命令行 AI Agent，Go 编写，无 GUI/WebUI。模块路径 `github.com/LaoQi/tanya`。
本文件只记现行约束，细节见 `docs/`；变更记录一律写根目录 `CHANGELOG.md`（新条目置顶）。

## 设计约束

- 极简优先：依赖仅 `gopkg.in/yaml.v3` 与 `golang.org/x/sys`，新增依赖需先讨论
- package 划分见下文《结构》，根目录只放 main.go 与顶级包；`agent` 零内部依赖
- 平台分片一律白名单：`linux`/`darwin`/`windows` 各一个装配文件，posix 共享实现落 `linux || darwin`，其余平台 stub；Linux 为主、macOS 尽力
- 终端输入层自研（raw mode + ANSI 渲染 + fish 风格 ghost 建议），不引入 TUI 框架；Windows 只支持 Windows Terminal（不支持 cmd/老 conhost）
- `ctty` 是**零依赖叶子**、只有原语与信号面：`/dev/tty`、termios、模式与光标、探测、SIGTERM/SIGHUP/SIGINT/SIGWINCH/SIGQUIT 一律收敛在此，任何层可直接依赖、不需注入；注入只针对**终端所有权**（`Console`/`Lease`）。业务层（`repl`/`main`）不出现 `os/signal`，退出走 `REPL.quit()`、退出码取 `ctty.ExitStatus()`，见 `docs/ctty.md`
- 终端持有者唯一（`readline` 的 `Console`：仲裁 + device + 租约；`repl` 只消费事件流与租约，`agent`/`tools` 经注入接口跨界）。借出两型：`LendStdin`（普通 run_shell）= stdin `os.DevNull` + 终端锚点，不直通终端、不移交前台；`LendFull`（`interactive: true`）= Linux pty 泵 / Windows `CONIN$` 直通，darwin 与借不出**明确报错**（`ErrNoLend`，不回退）。`^C`：tanya 持有期归一为中断（取消回合、杀子进程组），借出期归子进程；不做 `^Z` 检测。**读循环归 Console**：`SubscribeKeys` 才武装常驻读者（pipe 与 Windows 不后台读），`BeginRead`/`EndRead` 与借出（`Lease.Release`）自动挂起/恢复读者、读者保留 OPOST；尺寸消费面无状态（现取 `con.Size()`、只听 `EventResize` 重绘，取不到时回落 `repl.TermFacts`；`ctty.OnResize` → `EventResize`，借出期窗口尺寸转发子进程 pty）。见 `docs/terminal-console.md`
- 仓库根两份纯文本编译期嵌入、改动需重新编译（`system_prompt.md` 与 `config.example.yaml`，后者即 `tanya config` 的原样输出，与 `config.Default()` 一致性由根包测试守护）。注入基座 = 内置提示词 + **环境段**（`main.envSection`，无 CWD 行）经 `agent.WithSystemPrompt` 注入——`agent` 无内置文本、**无 env 概念**（快照 = 基座 + 两层 AGENTS.md）
- 多模态输入：对话行内 `@<路径|URL>` 即附图（`repl/attach.go`）；只认**行首或空白后的 `@`**、含空格路径按**最长存在路径**匹配（逐词回退取第一个存在且为常规文件的前缀，取不到则回退到第一个空白、静默）、`@"含 空格.png"` 引号形态保留兼容、多张按序；**文本保留 `@` 原样**发给模型，**解析不成功静默按普通文本**，**解析成功但校验不过（非 JPEG/PNG/GIF/WebP、超 `image_max_bytes`、超 `image_max_count`、非法 data URL、外链超 8192 字符）报错**；来源支持本地文件（`~`/相对路径按当前工作区）、`http(s)` 外链（只存 URL）、`data:image/...;base64`；`ask` 单发同口径（无额外 flag）；纯图消息不支持；`@` 触发路径补全（扫行尾活跃 token，整行只有 `@…` 时同样生效；候选 = 目录 + 图像扩展名文件，含空格名**原样插入**、与手打同形态，不引入引号/转义；引号 token 的 Tab 统一补成非引号形态且该形态不给 ghost、`/switch` 的名称白名单不动）；PNG/JPEG 超阈值时**本地预缩放**（`image_resize` 默认开，目标长边随 `image_detail` 取 512/1300、只缩不放、PNG→PNG 无损、JPEG→JPEG q85、缩放反而更大则保留原图；带 EXIF Orientation≠1 的 JPEG 跳过以免方向错乱；GIF/WebP 原样）；图像**内联 base64** 进 history 与会话文件（`Message.Images`，两协议 wire 见 `docs/multimodal.md`），本地估算按 DeepSeek 官方缩放规则 + 兜底 1024；模型亦可**主动读图**：标准工具 `read_image` 返回 `ToolResult.Images`（可选 `region{x,y,width,height}` 区域读取：仅 PNG/JPEG、EXIF 方向先归一、越界裁剪到交集（按显示坐标系）、区域不超目标长边则不缩放返回（PNG 无损/JPEG q85 重编码），供超大图分块取原尺寸），回合循环在本轮**全部 tool 消息之后**追加一条 user 图像消息（并行调用聚合为一条，避免打断 `tool_calls → tool` 相邻性；chat 的 `role: tool` 带不了图，故不依赖 responses 专有形态），说明文案 `MsgToolImageNoteFmt` 只含工具名与文件名、不内联 base64；`repl` 的工具块在此类结果后追加 `[图 <名> <大小>]` 占位（复用 `imagesText`）；嗅探/尺寸（带 EXIF 方向的 JPEG 记显示尺寸）/缩放/区域裁剪在 `agent/image_data.go`（`agent` 零内部依赖，`tools` 不依赖 `repl`），`repl` 侧只是薄封装
- 工具 = **调用方注入**（`agent.WithTools`，保序在前）+ 自带的 `next_session`（会话交接）与 `agent_custom`（恒末两位，`agent_custom` 最末），装配期一次合成、之后**运行期冻结**，无运行期注册 API、不做插件。注册**仅作契约**（不校验重名、不仲裁）：`lookup` 首个匹配胜出，清单顺序即请求 `tools` 顺序。接口：`Tool` 三方法（`Name`/`Definition`/`Invoke`）+ 可选 `Interactive`；结果 `ToolResult{Text; Meta}`（`Text` 回 history、`Meta` 归表现层）；外部经 `agent.NewTool`/`agent.NewToolDef` 构造，见 `docs/design.md`《工具》
- 标准工具集（`run_shell` + `read_image` + `get_time`/`get_env`/`calc`）落在顶级 `tools`，`main` 经 `tools.Standard(tools.Options{...})` 一次取齐按固定顺序注入；**`agent` 不带任何工具实现**（作库用时只有 `agent_custom`），也不认 shell。策略：以 `run_shell` 为核心，新能力优先用 shell 组合实现，纯计算/查询放 `tools/builtin`
- 工具自带**表现层视图**：`present.ToolView`（`Args`/`Result` 两回调，`ok=false` 即回落通用渲染）注册进 `present.Registry`，`main` 装配（`views.Register("run_shell", shellview.View())`）经 `repl.WithToolViews` 注入。`repl` 因此**不导入任何 `tools/*`**：未注册者走通用回落，外部工具可自带视图包
- `agent_custom` 供模型运行时自调与自省：key 表驱动，只写内存、不落盘不入会话，`/load` 或重启后回落配置，见 `docs/agent-control-tool.md`
- **会话交接**（`/next` + `next_session`，见 `docs/session-handoff.md`）：摘要现写、进新会话首条 user 消息（system 快照机制零改动），旧会话只追加一行 `[会话已移交至 <新 id>]`（不裁剪、不改写、不归档）；工具**只置 pending**（就地 rotate 会撕碎 `tool_calls → tool` 相邻性与 append-only 假设），应用点在**回合出口**（`Agent.settleTurn`，`err == nil` 时；中断/报错丢弃 pending）；`Agent.TakeHandoff()` 由 `repl` 回合后拉取（不新增事件）；续跑走 `Agent.Continue`（不追加 user 消息跑一轮，与 `AskContent` 共用 `ask(rollback, producedFrom)` 结算）；`/next` 命令路径的续跑只由命令参数决定、**忽略**工具 `continue`
- 缓存不变性（history append-only、system 快照冻结、`/load` 还原首行）是**优化手段**、非红线：只保证「同一二进制 + 首行快照未改写」，跨版本无约束，见 `docs/cache-probe.md`
- 启动即拒绝 root：`ctty.IsRoot()`（posix 取 euid 0，其余平台恒 false）为真则整个入口拒绝（`-v`/`config`/`ask`/`init` 无豁免），逃生舱只认 `TANYA_ALLOW_ROOT=1`；判定在 `main`
- 启动即要求可用 shell：解析（配置覆盖 > 平台探测）全落空即报错退出、无降级；解析点在 `tools.Standard`，`agent.New` 不感知 shell
- 配置分层：`agent.Config` 只留运行时核心项（14 项，含 `ConfigPath`），终端表现项外移顶级 `config` 包（`config.UI` 以 `yaml:",inline"` 合成进 `config.Config`），`shell` 覆盖（`yaml: shell` / `TANYA_SHELL`）为顶层字段、由 `main` 读给 `tools.Standard`；**yaml 键名与 `TANYA_*` env 名全程不变**，`config.example.yaml` 与 `tanya config` 输出逐字节不变。`agent` 无配置加载机制（无 `LoadConfig`/`DefaultConfig`、不读 yaml/env，依赖仅标准库），路径由调用方写入 `Config.ConfigPath`（`config_path` 键据此回报，未填即 `(未设置)`）；`Config.Validate()` 在 `agent.New` 入口调用：必填非空、`api_protocol`/`session_mode` 合法、阈值在界内，非法报错（`MsgNilConfig`），**`api_key` 唯一豁免**；默认值与加载归 `config` 包，见 `docs/design.md`《配置分层》
- `init` 子命令是新工作区的一次性脚手架（幂等、不覆盖既有文件），**必须在 `agent.New` 之前执行**；不做项目探测、不调模型，见 `docs/design.md`《init 模式》
- 会话归档：写入触发点只有 `/archive` 与启动自动归档两处，共用 `REPL.archiveFlow`（先报告再确认）；卷无损、写入后不可变、不做解档；归档会话 `/load` 只读、继续对话一律 `/fork`，见 `docs/session-archive.md`
- 回合渲染单 goroutine 事件合流（`repl/turnloop.go`）：agent 事件与完成信号同 channel FIFO（`inDone` 保尾事件不丢，`EventToolStart` 的 ack 握手保证工具块先于工具输出上屏）；思考期 `Ctrl+O` 经 `Console.SubscribeKeys` 送键（门禁 posix + keys + 富档）；`ReadEvent` 空闲返回 `EventIdle`（不 dispatch）；思维链「关→开」按段首重放走 `MarkdownBuf.Rewind` + `Close`
- REPL 输入分发（`repl/dispatch.go`）：`/` 白名单斜杠命令、`exit`/`quit` 内建退出、`:`/`：` 显式对话前缀；进程 cwd 恒为启动目录（不 `os.Chdir`），`run_shell` 默认在当前工作区执行、可用 `cwd` 指定单次目录；`/switch <dir>` 即先按退出同款收尾块报告旧会话（id/时长/用量/落盘文件）再放弃当前会话、重置计时起点、按新目录重建，失败或目标非法时原工作区与会话不动（无收尾块）；注入工具**不重建**，默认目录由 `tools.Options.Workspace` 活取
- `api_protocol` 双通道（yaml/env，默认 `responses`，非法值启动报错）：`responses` 走 `/responses`（reasoning 明文捕获/回传、固定 `store: false`），`chat` 走 `/chat/completions`（思维链走 `reasoning_content`），见 `docs/design.md`《LLM 接入》
- 思考等级只用标准字段 `reasoning_effort`（minimal/low/medium/high/max），不用厂商私有参数；设置后两协议均不发 `temperature`
- 出站 UA：默认 `tanya/<版本> (+https://github.com/LaoQi/tanya)`（不做伪装；格式源 `agent.UserAgent`，`<版本>` 由 `main` 经 `config.Version` 注入、与 `-v` 同源，未注入即 `dev`），`user_agent` / `TANYA_USER_AGENT` 可配（显式设置后原样发送、不再走默认格式）
- 输出侧动态文本（模型输出、用户输入、工具参数、服务端数据、错误信息）落屏前一律清洗控制序列：多行走 `output.emitText`、单行走 `term.OneLine`、错误行走 `errLine`；`emit` 只承载自生成样式文本、不做 emit 级全局 Strip，见 `docs/design.md`《输出流与 Kind》《斜杠命令》
- 颜色一律 16 色基本 SGR 码（30-37/90-97），不用 256 色/truecolor
- 表现层无进程级全局：profile（TTY/色档）以**值**传递——`style.Style.With(prof)` 绑定后 `Bound.Sprint`/`Frame`、`term.Passthrough(prof, s)`、`render.Sprint`/`Template.Render(prof, …)`；**不存在 `SetProfile`/`GetProfile`**；探测点只在 `main`（`ctty.Probe` + `term.DetectProfile`），`repl` 经 `WithProfile` 下传
- 样式注入收窄在消费者侧接口：`readline` 只认 `Styler`（`Sprint(string) string`），由 `repl` 传 `style.Bound`；输入层**不依赖 `render/style`**（`render/term` 可依赖）
- 注意力通知统一走 `repl` 通知接口：触发由事件流驱动（`repl/notify.go` 的 `notifySink` 消费 `EventTurnEnd` / `EventToolStart{Interactive}`，经 `agent.Sinks` 与渲染 sink 并列）、行为在 `Notifier`（`bell`/`notify_osc`/`notify_cmd` 三档 fan-out，外部程序走 `shell.Resolve` + `tools/shell.CommandNotifier`，载荷 `present.Notification`）、门禁为交互富档 TTY；**一律尽力而为**——失败静默、不重试、不探测环境、不做 tmux 透传，不得报错或打提示行；载荷只在 `payloadOf` 单点成品化，不得在 `toolView`/`agent` 内发声，`ask` 不参与，见 `docs/design.md`《终端通知》
- 事件扇出只有 `agent.Sinks` 一个机制：装配期固定、同步串行、顺序即因果；消费者不得阻塞、不得 panic 逃逸、不得反向调用 `agent`；**无运行期订阅/退订**。事件只装**事实**（`EventTurnEnd.Duration` 的起点只有 `Ask` 知道），派生量（TTFT/TTFC）由渲染侧现算；**新增事件必须同批带上消费者**，见 `docs/agent-event-seams.md`
- markdown 表格渲染尽力而为：列宽由三行前瞻定、不封顶、超宽不截断；`Table` 是「IR 无布局」的唯一例外，见 `docs/render-pipeline.md` §10
- 代码不添加注释，除非用户明确要求

## 结构

```
main.go             入口、flag 子命令、ask 单发、init 工作区脚手架、config 输出默认配置
system_prompt.md    内置系统提示词原文（顶层，编译期嵌入）
config.example.yaml 默认配置示例原文（顶层，编译期嵌入，`tanya config` 输出）
config/             tanya 作为 CLI 的完整配置：agent.Config + UI(inline) + Shell + Path；yaml 加载、TANYA_* 覆盖、校验（agent 侧零加载机制）
ctty/               控制终端原语与探测（/dev/tty、termios、模式复位/光标锚点、信号、Facts）；白名单 + stub，零内部依赖
repl/               REPL 循环与输入分发、斜杠命令、提示符、ghost 补全、picker、工具块排版（标题组合 + 通用回落）、状态行、回合事件合流（turnloop）、退出收尾；**不认识任何具体工具**（自带视图经注入）
readline/           终端输入层：Console 仲裁 + device 设备面 + 租约（借出/pty 泵/锚点）、行编辑/历史/补全、按键解析、宽度
agent/              核心逻辑（config 收窄校验 / llm 双协议 / loop / prompt / session / archive / handoff / stats / path / init / tools / control）；零内部依赖
tools/              外置工具集：根包 tools.Standard 装配标准集与顺序；shell/ = run_shell（含 notify_cmd 的命令构造 `CommandNotifier`）；shell/view/ = run_shell 自带表现层视图（参数区 + 结果区）；image/ = read_image（模型主动读图：路径解析 + 校验 + 缩放，返回文本与 `ToolResult.Images`）；builtin/ = get_time/get_env/calc
render/             表现层树根（IR → ANSI）：style/ 词汇、term/ 终端原语、ir/、theme/ 配色、markdown/ 流式解析与整段重放、markup/ 内联标记、present/ 工具视图契约（`ToolView`/`Registry` + 通知载荷 `Notification`，仅依赖 `render/term` 输出侧原语）
```

各模块行为细节见 `docs/design.md`。

## 文档

- `docs/design.md` 核心设计与各模块行为细节（权威）；`README.md` 使用说明与配置项
- 终端：`docs/terminal-console.md` 控制台层（唯一持有者/租约/`^C` 归一，已实施）、`docs/ctty.md` 控制终端抽象、`docs/interactive-tty.md` pty 桥接、`docs/terminal-caps.md` 探测与能力降级、`docs/windows-console-mode-restore.md` Windows 模式复原（已实施，实机回归未全覆盖）
- 表现层：`docs/style-split.md` 拆包、`docs/render-pipeline.md` 渲染管线、`docs/render-refs-compare.md` 参考对比
- 工具与缓存：`docs/shell-tool.md` run_shell 组件化、`docs/agent-control-tool.md` agent_custom、`docs/cache-probe.md` prompt cache
- 分层与依赖：`docs/layering-refactor.md`（S1–S6 已落地，S7 按 D3 不做）
- 会话与 REPL：`docs/session-archive.md` 归档卷、`docs/session-handoff.md` 会话交接（`/next` + `next_session`）、`docs/repl-output-refactor.md` 输出收敛、`docs/stream-input-events.md` 流式期输入与事件合流（P1–P5 已实施，含思考期 `Ctrl+O`）、`docs/repl-status-append.md` 状态追加
- 归档/未实施：`docs/repl-replay-rendering.md`（未实施，评估结论：建议不做）、`docs/multimodal.md`（多模态/图像：P0/P1/预缩放/模型主动读图（P4）均已实施，见其 §7.5）、`docs/probe-redesign.md`、`docs/reasoning-live-toggle.md`

## 构建与测试

```bash
go build ./... && go vet ./...          # 构建 + 静态检查
go test ./... && go test -race ./...    # 全量测试 + 竞态
go run . ask "你好"                      # 单发冒烟（需配置 api_key）
python3 scripts/render_audit.py         # 渲染审计（pty + VT 回放，需先 make build）
```

**测试包边界**：需要上层包（如 `readline`）的集成用例一律放**外部测试包**（`package X_test`，写在同目录）——内部测试包导入上层包会让上层永远无法依赖本包（测试二进制成环），实例见 `tools/shell/tty_e2e_test.go`。

测试约定见 `docs/design.md`《测试》：`agent`/`repl` 各自 `TestMain` 做包级基线隔离（HOME 与 cwd 指向临时目录），用例不得依赖真实 HOME/配置；LLM mock 用 `newMockLLM` + `mockStep`，readline 用 fakeTerm 注入按键（测试必须显式传 profile）。交互/中断类手工验证用 `make build` 产出的 `./tanya`：**不要用 `go run .`**（`^Z` 会停住 wrapper、`^C` 失效）。
