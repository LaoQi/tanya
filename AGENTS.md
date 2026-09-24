# tanya

极简命令行 AI Agent，Go 编写，无 GUI/WebUI。模块路径 `github.com/LaoQi/tanya`。
本文件只记现行约束，变更记录一律写根目录 `CHANGELOG.md`（新条目置顶）。

## 设计约束

- 极简优先：依赖仅 `gopkg.in/yaml.v3` 与 `golang.org/x/sys`，新增依赖需先讨论
- package 划分：`main`（仅入口）、`repl`（REPL/输入分发/渲染）、`agent`（核心逻辑）、`readline`（自研终端输入层）、`render`（表现层树）、`ctty`（控制终端原语与探测，零依赖叶子）；根目录只放 main.go 与顶级包
- 终端输入层自研（raw mode + ANSI 渲染、fish 风格 ghost 置灰建议），不引入 TUI 框架；Windows 仅支持 Windows Terminal（输入后端与 interactive 直通均已实现，Windows 侧仅部分实机验证、未全量覆盖，暂不跟踪），不支持 cmd/老 conhost
- 平台分片一律白名单：`linux`/`darwin`/`windows` 各一个装配文件，posix 共享实现落 `linux || darwin`，其余平台 stub；Linux 为主、macOS 尽力
- 终端原语（`/dev/tty`、前台组、termios、探测）一律走 `ctty`，移交/夺回/复原策略由 `agent`、`readline` 各自决定；`run_shell` 交终端前快照输入模式与光标、子进程结束后复原，中断不变量与 readline 自愈见 `docs/ctty.md`、`docs/interactive-tty.md` §5.9
- 运行期信号统一收敛在 `ctty`（SIGTERM/SIGHUP 关闭、SIGINT 中断、SIGQUIT 保持默认转储），业务层（`repl`/`main`）不出现 `os/signal`；退出统一走 `REPL.quit()`，进程退出码取 `ctty.ExitStatus()`，见 `docs/ctty.md`《运行期信号》
- `interactive: true` 的 run_shell 走全 pty 桥接，仅 Linux（失败回退 `/dev/tty` + `TIOCSPGRP`）；Windows 为控制台继承直通，见 `docs/interactive-tty.md`、`docs/terminal-caps.md` §8.6
- 系统提示词原文放仓库根 `system_prompt.md`（可读可改，纯文本），`main` 用 `//go:embed` 编译期嵌入、构造时经 `agent.WithSystemPrompt` 注入；`agent` 侧无内置文本（未注入即无内置段），改动需重新编译
- 默认配置示例原文放仓库根 `config.example.yaml`（可读可改，纯文本），`main` 用 `//go:embed` 编译期嵌入，`tanya config` 原样打到 stdout（不带提示行，可直接 `> ~/.config/tanya/config.yaml`）；示例与 `config.Default()` 的一致性由根包测试守护，改动需重新编译
- 工具 = 编译期内置清单 `allTools()`（`run_shell` + `builtinTools()` + `agent_custom`）+ **构造期外部注册**（`agent.WithTools`），二者在 `agent.New` 装配期合成、之后**运行期冻结**；不提供运行期注册 API，不做插件。注册**仅作契约、不做强约束**：agent 不校验重名、不仲裁——内置在前、注册追加在后（顺序保序），重名由调用方保证，`lookup` 首个匹配胜出故内置优先。清单顺序即请求 `tools` 顺序（缓存契约），改动会让缓存前缀作废（代价可接受，见下条）
- 工具抽象：`Tool` 三方法（`Name`/`Definition`/`Invoke`）+ 可选接口 `Interactive`（终端独占标记）、`EnvReporter`（env 段自述，B2 接线）；外部用 `agent.NewTool(name, desc, params, fn)` 构造工具，不依赖包内类型
- 工具结果抽象：`ToolResult{Text string; Meta any}`——`Text` 是模型侧内容（写回 history），`Meta` 是表现层结构化载荷（`run_shell` 填 `*ShellResult`，`repl` 断言渲染、失败回落文本）；agent 核心不知道任何具体 `Meta` 类型
- 缓存不变性（history append-only、system 快照冻结、`/load` 还原首行）是为命中 provider 前缀缓存服务的**优化手段**，不是功能红线：保证范围仅限「同一二进制 + 会话首行快照未被改写」（进程内多轮、重开快照未变的旧会话都命中）；跨版本无此约束——改了默认系统提示词/工具描述/env 段后 `/load` 旧会话前缀变化属预期，代价只是首轮 cache miss。评估改动时按 `docs/cache-probe.md` 的台阶估代价即可，不必为字节不变放弃功能，见 `docs/design.md`《系统提示与缓存友好》
- `agent_custom` 供模型运行时自调与自省：key 表驱动，只写内存、不落盘不入会话，`/load` 或重启后回落配置，见 `docs/agent-control-tool.md`
- 工具策略：以 `run_shell` 为核心，新能力优先用 shell 命令组合实现；小型纯计算/查询工具放 `builtin.go`
- 启动即拒绝 root：`ctty.IsRoot()`（posix 取 `os.Geteuid() == 0`，含 `sudo`/setuid；Windows 及其余平台恒 false）为真则整个入口拒绝（`-v`/`config`/`ask`/`init` 无豁免），逃生舱只认 `TANYA_ALLOW_ROOT=1`；`-h` 与非法选项由 flag 包先行退出、不受影响，判定留在 `main`（`agent` 作库用时不判权限），见 `docs/design.md`《启动安全检查》
- 启动即要求可用 shell：`agent.New` 解析（配置覆盖 > 平台探测）全落空直接报错退出，无降级路径
- 配置分层：`agent.Config` 只留 agent 运行时需要的核心项（LLM 接入/工作区会话/shell/归档策略共 14 项，含调用方填入的 `ConfigPath`），终端表现项（`show_reasoning`/`bell`/`notify_osc`/`notify_cmd`/`colors`/`theme`/`palette`/`tool_output_lines`）整体外移到顶级 `config` 包（`config.UI` 段，与 `agent.Config` 以 `yaml:",inline"` 合成 `config.Config`）；**yaml 键名与 `TANYA_*` env 名全程不变**，`config.example.yaml` 与 `tanya config` 输出逐字节不变，见 `docs/design.md`《配置分层》
- `agent` 包不提供任何配置加载机制：无 `LoadConfig`/`DefaultConfig`、不读 yaml、不读 env（依赖仅剩 `golang.org/x/sys`）；配置文件路径由调用方写入 `Config.ConfigPath` 结构体字段直接读取，`agent_custom` 的 `config_path` 键据此回报（未填即 `(未设置)`）
- `agent.Config.Validate()` 是自洽校验（`agent.New` 入口调用，外部亦可显式调用）：`base_url`/`model`/`user_agent`/`data_dir`/`config_path` 非空、`api_protocol`/`session_mode` 取值合法、归档阈值与保留数在界内；`agent.New` 入口即校验，缺失/非法直接返回错误（`nil` 配置返回 `MsgNilConfig` 而非 panic）。**`api_key` 是唯一豁免**（保留"可启动、首次请求才警告"的现有行为）。默认值填充与 yaml/env 加载由 `config` 包负责
- `init` 子命令是新工作区的一次性脚手架（三项动作幂等、不覆盖既有文件），**必须在 `agent.New` 之前执行**；不做项目探测、不调模型，见 `docs/design.md`《init 模式》
- 会话归档：写入触发点只有 `/archive` 与启动自动归档两处，共用 `REPL.archiveFlow`（先出报告再确认）；卷逐字节无损、写入后不可变、不做解档；归档会话 `/load` 只读，继续对话一律 `/fork`（通用分支命令，原会话不动），见 `docs/session-archive.md`
- REPL 输入分发（`repl/dispatch.go`）：`/` 白名单斜杠命令、`exit`/`quit` 内建退出、`:`/`：` 等价显式对话前缀；进程 cwd 恒为启动目录（全程不 `os.Chdir`），`run_shell` 默认在当前工作区执行、可用 `cwd` 参数指定单次目录；`/switch <dir>` 换工作区即放弃当前会话，由 `Agent.loadWorkspace` 按新目录重建派生态（shell 工具/prompt/store/env 段），任一步失败或目标非法（不存在/非目录/等同当前）时原工作区与会话不动
- LLM 协议双通道 `api_protocol`（yaml/env，默认 `responses`，非法值启动报错）：`responses` 走 `/responses`（reasoning 明文捕获/回传、固定 `store: false`），`chat` 走 `/chat/completions`（思维链经 `reasoning_content`），见 `docs/design.md`《LLM 接入》
- 思考等级只用标准字段 `reasoning_effort`（minimal/low/medium/high/max），不用厂商私有参数；设置后两协议均不发 `temperature`
- 出站请求 UA 伪装（避免厂商风控）：默认 `pi/0.85.0 (linux; node/v22.14.0; x64)`，yaml `user_agent` / env `TANYA_USER_AGENT` 可配
- 输出侧动态文本（模型输出、用户输入、工具参数、服务端数据、错误信息）落屏前一律清洗控制序列：多行文本走 `output.emitText`、必须单行的展示走 `term.OneLine`、错误行走 `errLine`；`emit` 只承载自生成样式文本，不做 emit 级全局 Strip；ask 旁路与 picker 记账细节见 `docs/design.md`《输出流与 Kind》《斜杠命令》
- 颜色一律用终端 16 色基本 SGR 码（30-37/90-97），不用 256 色/truecolor
- 注意力通知统一走 `repl` 的通知接口：触发语义在 REPL（只读 agent 事件：回合结束 / `interactive` 工具开始）、行为在 `Notifier`（三种实现 `bell`/`notify_osc`/`notify_cmd`，经 `repl.Notifiers` fan-out；终端字节走 `ctty.Bell`/`ctty.NotifyOSC`，外部程序走 `agent.ResolveShell` 解析出的 shell）、门禁为交互富档 TTY；通知**一律尽力而为**——失败静默、不重试、不探测终端/桌面环境、不做 tmux 透传与平台特化适配，也不得因「没生效」报错或打提示行；载荷在 `payloadOf` 单点成品化（title/content/kind，已压单行并截断），不得在 `toolView`/`agent` 内直接发声，也不探测子进程真实读取 stdin 的时刻，`ask` 单发不参与，见 `docs/design.md`《终端通知》
- markdown 表格渲染是尽力而为：三行前瞻（表头/分隔/首数据行）定列宽与对齐，列宽不封顶、超宽单元格不截断（边线可错位），终端宽度只用于撑破时切紧边距；`Table` 块是「IR 无布局」的唯一例外，见 `docs/render-pipeline.md` §10
- 代码不添加注释，除非用户明确要求

## 结构

```
main.go             入口、flag 子命令、ask 单发、init 工作区脚手架、config 输出默认配置（含系统提示词与配置示例的 embed）
system_prompt.md    内置系统提示词原文（顶层，编译期嵌入）
config.example.yaml 默认配置示例原文（顶层，编译期嵌入，`tanya config` 输出）
config/             tanya 作为 CLI 的完整配置：agent.Config(inline) + UI 段(inline) + Path；yaml 加载、TANYA_* env 覆盖、值域校验（agent 侧零加载机制）
ctty/               控制终端原语与终端探测（前台组、/dev/tty、termios、Facts）；白名单 + stub，零内部依赖
repl/               REPL 循环与输入分发、斜杠命令、提示符、ghost 补全、/load picker、工具块渲染、状态行、退出收尾
readline/           自研终端输入层：行编辑/历史/Tab 补全、按键解析、raw mode、显示宽度、pty 桥接（linux）、状态自愈
agent/              核心逻辑与工具：config(收窄结构+校验) / llm(+http,+responses) / agent loop / prompt / session / session_archive / stats / envprobe / init / tools / shelltool / shell(+平台分片) / builtin / control / tty_bridge
render/             表现层树根（IR → ANSI）；style/ 样式词汇、term/ 终端原语、ir/ 渲染 IR、theme/ 配色、markdown/ 流式解析、markup/ 内联标记
```

各模块行为细节见 `docs/design.md`。

## 文档

- `docs/design.md` 核心设计与各模块行为细节（权威）；`README.md` 使用说明与配置项
- `docs/session-archive.md` 会话归档（zip 卷）实施文档：分期、接口签名、验收与落地偏差
- 终端：`docs/ctty.md` 控制终端抽象与平台收敛、`docs/interactive-tty.md` pty 桥接、`docs/terminal-caps.md` 探测与能力降级
- 表现层：`docs/style-split.md` 拆包、`docs/render-pipeline.md` 渲染管线、`docs/render-refs-compare.md` 参考对比
- 工具与缓存：`docs/shell-tool.md` run_shell 组件化、`docs/agent-control-tool.md` agent_custom、`docs/cache-probe.md` prompt cache
- REPL：`docs/repl-output-refactor.md` 输出收敛、`docs/repl-status-append.md` 状态追加（`docs/repl-replay-rendering.md` 未实施、`docs/probe-redesign.md` 归档）

## 构建与测试

```bash
go build ./... && go vet ./...          # 构建 + 静态检查
go test ./... && go test -race ./...    # 全量测试 + 竞态
go run . ask "你好"                      # 单发冒烟（需配置 api_key）
python3 scripts/render_audit.py         # 渲染审计（pty + VT 回放，需先 make build）
```

测试约定见 `docs/design.md`《测试》：`agent`/`repl` 各自 `TestMain` 做包级基线隔离（HOME 与 cwd 指向临时目录，`TestProcessEnvIsolated` 守卫），用例不得依赖真实 HOME/配置；LLM mock 用 `agent/mock_test.go` 的 `newMockLLM` + `mockStep`，readline 用 fakeTerm 注入按键。交互/中断类手工验证用 `make build` 产出的 `./tanya`：**不要用 `go run .`**（`^Z` 会停住 wrapper，shell 抢走终端前台后 `^C` 失效）。
