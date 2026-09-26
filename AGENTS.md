# tanya

极简命令行 AI Agent，Go 编写，无 GUI/WebUI。模块路径 `github.com/LaoQi/tanya`。
本文件只记现行约束，细节见 `docs/`；变更记录一律写根目录 `CHANGELOG.md`（新条目置顶）。

## 设计约束

- 极简优先：依赖仅 `gopkg.in/yaml.v3` 与 `golang.org/x/sys`，新增依赖需先讨论
- package 划分见下文《结构》，根目录只放 main.go 与顶级包；`agent` 零内部依赖、`ctty` 零依赖叶子
- 平台分片一律白名单：`linux`/`darwin`/`windows` 各一个装配文件，posix 共享实现落 `linux || darwin`，其余平台 stub；Linux 为主、macOS 尽力
- 终端输入层自研（raw mode + ANSI 渲染 + fish 风格 ghost 置灰建议），不引入 TUI 框架；Windows 仅支持 Windows Terminal（输入后端与 interactive 直通已实现，实机验证未全覆盖；不支持 cmd/老 conhost）
- 终端原语（`/dev/tty`、前台组、termios、探测）一律走 `ctty`，移交/夺回/复原策略由 `agent`、`readline` 各自决定；`run_shell` 交终端前快照输入模式与光标、子进程结束后复原，见 `docs/ctty.md`
- 运行期信号统一收敛在 `ctty`（SIGTERM/SIGHUP 关闭、SIGINT 中断、SIGQUIT 保持默认转储），业务层（`repl`/`main`）不出现 `os/signal`；退出统一走 `REPL.quit()`，进程退出码取 `ctty.ExitStatus()`，见 `docs/ctty.md`
- `interactive: true` 的 run_shell 走全 pty 桥接，仅 Linux（失败回退 `/dev/tty` + `TIOCSPGRP`）；Windows 为控制台继承直通，见 `docs/interactive-tty.md`、`docs/terminal-caps.md` §8.6
- 仓库根两份纯文本编译期嵌入、改动需重新编译：`system_prompt.md` 内置提示词与**环境段**（OS/shell 契约，`main.envSection`，无 CWD 行）合成**基座**经 `agent.WithSystemPrompt` 注入——`agent` 侧无内置文本、**无任何 env 概念**（快照 = 基座 + 两层 AGENTS.md）；`config.example.yaml` 由 `tanya config` 原样打到 stdout（不带提示行，可直接写入配置路径），与 `config.Default()` 的一致性由根包测试守护
- 工具 = **调用方注入**（`agent.WithTools`，保序在前）+ agent 自带的 `agent_custom`（恒末位），装配期一次合成、之后**运行期冻结**；不提供运行期注册 API，不做插件。注册**仅作契约**：不校验重名、不仲裁（重名由调用方保证），`lookup` 首个匹配胜出（注入项在前故可遮蔽 `agent_custom`）；清单顺序即请求 `tools` 顺序（缓存契约）
- 标准工具集（`run_shell` + `get_time`/`get_env`/`calc`）落在顶级 `tools`，`main` 经 `tools.Standard(tools.Options{...})` 一次取齐并按固定顺序注入；**`agent` 不带任何工具实现**（作库用时默认只有 `agent_custom`），也不认 shell
- 工具扩展点：`Tool` 三方法（`Name`/`Definition`/`Invoke`）+ 可选 `Interactive`（终端独占标记），结果 `ToolResult{Text; Meta}`（`Text` 回写 history、`Meta` 为表现层载荷，核心不知道任何具体 `Meta` 类型）；外部经 `agent.NewTool`/`agent.NewToolDef` 构造，不依赖包内类型，见 `docs/design.md`《工具》
- 工具策略：以 `run_shell` 为核心，新能力优先用 shell 命令组合实现；小型纯计算/查询工具放 `tools/builtin`
- `agent_custom` 供模型运行时自调与自省：key 表驱动，只写内存、不落盘不入会话，`/load` 或重启后回落配置，见 `docs/agent-control-tool.md`
- 缓存不变性（history append-only、system 快照冻结、`/load` 还原首行）是命中 provider 前缀缓存的**优化手段**，不是功能红线：保证范围仅限「同一二进制 + 会话首行快照未被改写」（进程内多轮、快照未变的旧会话都命中）；跨版本无此约束——改了默认提示词/工具描述/env 段后 `/load` 旧会话前缀变化属预期，代价只是首轮 cache miss，按 `docs/cache-probe.md` 的台阶估代价即可，不必为字节不变放弃功能。见 `docs/design.md`《系统提示与缓存友好》
- 启动即拒绝 root：`ctty.IsRoot()`（posix 取 euid 0，含 `sudo`/setuid；其余平台恒 false）为真则整个入口拒绝（`-v`/`config`/`ask`/`init` 无豁免），逃生舱只认 `TANYA_ALLOW_ROOT=1`；判定留在 `main`（`agent` 作库用时不判权限），见 `docs/design.md`《启动安全检查》
- 启动即要求可用 shell：解析（配置覆盖 > 平台探测）全落空即报错退出、无降级路径；解析点在 `tools.Standard`（`main` 装配点），`agent.New` 不解析也不感知 shell
- 配置分层：`agent.Config` 只留 agent 运行时核心项（13 项，含调用方填入的 `ConfigPath`），终端表现项整体外移顶级 `config` 包（`config.UI` 以 `yaml:",inline"` 合成 `config.Config`），`shell` 覆盖（`yaml: shell` / `TANYA_SHELL`）为 `config.Config` 顶层字段、由 `main` 读给 `tools.Standard`；**yaml 键名与 `TANYA_*` env 名全程不变**，`config.example.yaml` 与 `tanya config` 输出逐字节不变，见 `docs/design.md`《配置分层》
- `agent` 不提供任何配置加载机制：无 `LoadConfig`/`DefaultConfig`、不读 yaml/env，**依赖仅标准库**；配置路径由调用方写入 `Config.ConfigPath` 字段直接读取（`agent_custom` 的 `config_path` 键据此回报，未填即 `(未设置)`）
- `agent.Config.Validate()` 做自洽校验（`agent.New` 入口调用，外部亦可显式调用）：必填非空、`api_protocol`/`session_mode` 合法、归档阈值与保留数在界内，缺失/非法直接报错（`nil` 配置返回 `MsgNilConfig` 而非 panic）；**`api_key` 是唯一豁免**（可启动、首次请求才警告）。默认值填充与 yaml/env 加载归 `config` 包
- `init` 子命令是新工作区的一次性脚手架（三项动作幂等、不覆盖既有文件），**必须在 `agent.New` 之前执行**；不做项目探测、不调模型，见 `docs/design.md`《init 模式》
- 会话归档：写入触发点只有 `/archive` 与启动自动归档两处，共用 `REPL.archiveFlow`（先报告再确认）；卷无损、写入后不可变、不做解档；归档会话 `/load` 只读，继续对话一律 `/fork`（通用分支命令，原会话不动），见 `docs/session-archive.md`
- REPL 输入分发（`repl/dispatch.go`）：`/` 白名单斜杠命令、`exit`/`quit` 内建退出、`:`/`：` 显式对话前缀；进程 cwd 恒为启动目录（不 `os.Chdir`），`run_shell` 默认在当前工作区执行、可用 `cwd` 指定单次目录；`/switch <dir>` 换工作区即放弃当前会话、按新目录重建派生态，任一步失败或目标非法（不存在/非目录/等同当前）时原工作区与会话不动；注入工具**不重建**，`run_shell` 默认目录由 `tools.Options.Workspace` 活取跟随
- `api_protocol` 双通道（yaml/env，默认 `responses`，非法值启动报错）：`responses` 走 `/responses`（reasoning 明文捕获/回传、固定 `store: false`），`chat` 走 `/chat/completions`（思维链经 `reasoning_content`），见 `docs/design.md`《LLM 接入》
- 思考等级只用标准字段 `reasoning_effort`（minimal/low/medium/high/max），不用厂商私有参数；设置后两协议均不发 `temperature`
- 出站请求 UA 伪装（避免厂商风控）：默认 `pi/0.85.0 (linux; node/v22.14.0; x64)`，yaml `user_agent` / env `TANYA_USER_AGENT` 可配
- 输出侧动态文本（模型输出、用户输入、工具参数、服务端数据、错误信息）落屏前一律清洗控制序列：多行走 `output.emitText`、单行展示走 `term.OneLine`、错误行走 `errLine`；`emit` 只承载自生成样式文本、不做 emit 级全局 Strip，ask 旁路与 picker 记账见 `docs/design.md`《输出流与 Kind》《斜杠命令》
- 颜色一律用终端 16 色基本 SGR 码（30-37/90-97），不用 256 色/truecolor
- 注意力通知统一走 `repl` 通知接口：触发语义在 REPL（回合结束 / `interactive` 工具开始两处）、行为在 `Notifier`（`bell`/`notify_osc`/`notify_cmd` fan-out，外部程序走 `shell.Resolve` 同一套 shell 解析）、门禁为交互富档 TTY；**一律尽力而为**——失败静默、不重试、不探测环境、不做 tmux 透传与平台特化，也不得因「没生效」报错或打提示行；载荷只在 `payloadOf` 单点成品化，不得在 `toolView`/`agent` 内直接发声、不探测子进程读取 stdin 的时刻，`ask` 单发不参与，见 `docs/design.md`《终端通知》
- markdown 表格渲染是尽力而为：三行前瞻（表头/分隔/首数据行）定列宽与对齐，列宽不封顶、超宽单元格不截断，终端宽度只用于撑破时切紧边距；`Table` 块是「IR 无布局」的唯一例外，见 `docs/render-pipeline.md` §10
- 代码不添加注释，除非用户明确要求

## 结构

```
main.go             入口、flag 子命令、ask 单发、init 工作区脚手架、config 输出默认配置
system_prompt.md    内置系统提示词原文（顶层，编译期嵌入）
config.example.yaml 默认配置示例原文（顶层，编译期嵌入，`tanya config` 输出）
config/             tanya 作为 CLI 的完整配置：agent.Config + UI(inline) + Shell + Path；yaml 加载、TANYA_* 覆盖、校验（agent 侧零加载机制）
ctty/               控制终端原语与探测（前台组、/dev/tty、termios、Facts）；白名单 + stub，零内部依赖
repl/               REPL 循环与输入分发、斜杠命令、提示符、ghost 补全、picker、工具块渲染、状态行、退出收尾
readline/           自研终端输入层：行编辑/历史/补全、按键解析、raw mode、宽度、pty 桥接（linux）、状态自愈
agent/              核心逻辑（config 收窄校验 / llm 双协议 / loop / prompt / session / archive / stats / path / init / tools / control）；零内部依赖
tools/              外置工具集：根包 tools.Standard 装配标准集与顺序；shell/ = run_shell；builtin/ = get_time/get_env/calc
render/             表现层树根（IR → ANSI）：style/ 词汇、term/ 终端原语、ir/、theme/ 配色、markdown/ 流式解析、markup/ 内联标记
```

各模块行为细节见 `docs/design.md`。

## 文档

- `docs/design.md` 核心设计与各模块行为细节（权威）；`README.md` 使用说明与配置项
- 终端：`docs/ctty.md` 控制终端抽象、`docs/interactive-tty.md` pty 桥接、`docs/terminal-caps.md` 探测与能力降级
- 表现层：`docs/style-split.md` 拆包、`docs/render-pipeline.md` 渲染管线、`docs/render-refs-compare.md` 参考对比
- 工具与缓存：`docs/shell-tool.md` run_shell 组件化、`docs/agent-control-tool.md` agent_custom、`docs/cache-probe.md` prompt cache
- 会话与 REPL：`docs/session-archive.md` 归档卷、`docs/repl-output-refactor.md` 输出收敛、`docs/repl-status-append.md` 状态追加（未实施：`docs/repl-replay-rendering.md`；已归档：`docs/probe-redesign.md`）

## 构建与测试

```bash
go build ./... && go vet ./...          # 构建 + 静态检查
go test ./... && go test -race ./...    # 全量测试 + 竞态
go run . ask "你好"                      # 单发冒烟（需配置 api_key）
python3 scripts/render_audit.py         # 渲染审计（pty + VT 回放，需先 make build）
```

测试约定见 `docs/design.md`《测试》：`agent`/`repl` 各自 `TestMain` 做包级基线隔离（HOME 与 cwd 指向临时目录），用例不得依赖真实 HOME/配置；LLM mock 用 `newMockLLM` + `mockStep`，readline 用 fakeTerm 注入按键。交互/中断类手工验证用 `make build` 产出的 `./tanya`：**不要用 `go run .`**（`^Z` 会停住 wrapper、`^C` 失效）。
