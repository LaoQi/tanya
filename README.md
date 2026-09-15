# tanyan

极简命令行 AI Agent，Go 编写，无 GUI/WebUI/TUI。单二进制，交互式 REPL + 单发模式。

## 特性

- OpenAI 兼容接口（OpenAI / DeepSeek / GLM / Ollama / vLLM 等），SSE 流式输出
- 以 shell 为核心的工具体系：模型可直接执行 shell 命令（自动适配平台：Linux/macOS bash/sh/ash，Windows pwsh；`shell` 配置可指定任意 shell）
- 内置轻量工具：`get_time` / `get_env` / `calc`
- 模型可运行时自调：`agent_custom` 工具读取/修改模型与思考等级、查询可用模型与运行态统计（仅本次会话有效，不写配置文件）
- 会话持久化与恢复（JSONL，记录完整历史，system 快照随会话冻结）
- token 用量实时显示在提示符（API 实报优先，本地估算兜底），支持显示缓存命中
- AI 输出 Markdown 渲染（默认开启，非 TTY 与 plain 输出自动旁路）与内置配色主题（`/theme` 切换）
- AGENTS.md 项目说明自动注入系统提示（全局 + 工作区双层，会话级快照保证 prompt cache 友好）
- 平台：Linux 与 Windows 为主（Windows 交互能力待实现，现为降级 stub），macOS 尽力；控制终端原语统一在零依赖叶子包 `ctty`，其余平台仅保证可编译
- 依赖仅 2 个，核心逻辑测试覆盖率 90%+

## 安装

```bash
go install github.com/LaoQi/tanyan@latest
```

或从源码构建：

```bash
git clone https://github.com/LaoQi/tanyan.git && cd tanyan
make build
```

`make build` 经 ldflags 注入版本号（`git describe`）与构建时间：欢迎屏显示 `tanyan <版本>（构建于 <时间>）`，`-v` 显示版本号。直接 `go build` 时版本为 `dev`、不显示构建时间。

## 配置

`~/.config/tanyan/config.yaml`（完整示例见 `config.example.yaml`）：

```yaml
base_url: https://api.deepseek.com/v1
api_key: "sk-..."
model: deepseek-v4-flash
```

环境变量 `TANYA_*` 可覆盖配置文件：`TANYA_BASE_URL` / `TANYA_API_KEY` / `TANYA_MODEL` / `TANYA_TEMPERATURE` / `TANYA_REASONING_EFFORT` / `TANYA_API_PROTOCOL` / `TANYA_SESSION_MODE` / `TANYA_USER_AGENT` / `TANYA_TOOL_OUTPUT_LINES` / `TANYA_SHELL` / `TANYA_THEME`。

`api_protocol` 配置项（env `TANYA_API_PROTOCOL`）选择 API 协议：`responses`（默认，OpenAI Responses API 兼容格式，思维链明文回传）或 `chat`（Chat Completions 兼容协议）。端点路径为 `/responses` 时用 `responses`；仅提供 `/chat/completions` 的端点遇 404 时请切换为 `chat`。

`shell` 配置项（env `TANYA_SHELL`）指定 run_shell 使用的 shell，支持名字或绝对路径（如 `zsh`、`/usr/bin/fish`）；缺省自动探测：Windows 用 pwsh，Linux/macOS 依次尝试 bash → sh → ash。全部落空（含配置的 shell 不存在）时启动阶段直接报错退出，不进入 REPL。

`reasoning_effort` 配置项（env `TANYA_REASONING_EFFORT`）设置思考等级，随请求发送 OpenAI 标准字段（o 系 / gpt-5 及兼容网关支持），可选 `minimal` / `low` / `medium` / `high` / `max`，留空不发送；REPL 内 `/think` 可运行时切换。`responses` 协议下映射为 `reasoning.effort`，`chat` 协议下为 `reasoning_effort`。设置思考等级后请求不再发送 `temperature`（两协议一致），以兼容 o 系 / gpt-5 等仅支持 `temperature=1` 的推理模型。

`theme` 配置项（env `TANYA_THEME`）选择内置配色主题，REPL 内 `/theme` 可运行时切换；`colors`（auto/on/off）控制是否着色；`palette` 可覆盖单个语义色。可用主题与色名见 `config.example.yaml`。

`responses` 协议以思维链回传为核心特性（参照 DeepSeek Responses API 标准，OpenAI 兼容但不完整遵守 OpenAI）：响应中的 reasoning item 的明文思维链 `content` 随会话保存并在后续请求中原样回传，保持多轮工具调用间推理链完整；请求固定 `store: false`，不携带 `include`/`encrypted_content` 等 OpenAI 特有字段。回传内容须逐字节一致（不截断、不改写），以保证 DeepSeek 前缀缓存命中。`chat` 协议下同一能力经 `delta.reasoning_content` 捕获、随会话落盘，并在后续请求中折叠为 assistant 消息的顶层 `reasoning_content` 字段回传（DeepSeek 思考模式携带 `tools` 时官方要求历史推理链完整回传）。

## 使用

```bash
tanyan                 # 交互 REPL
tanyan ask "问题"      # 单发模式（默认纯文本+verbose：无动画/无光标控制，保留工具块与状态行）
tanyan -n              # 只读会话：可载入历史，不写入
tanyan -n ask "问题"   # 单发且不写入会话历史
tanyan -c x.yaml       # 指定配置文件
tanyan -m local        # 会话存到当前目录 .tanya/
tanyan -p              # 纯文本输出：无颜色/动画/工具块，stdout 只留答案与命令反馈
tanyan -p --verbose    # 纯文本输出但保留工具块与状态行（仍无颜色与光标控制）
tanyan -v              # 显示版本号
```

会话存储模式（`-m` 参数 / 配置项 `session_mode` / env `TANYA_SESSION_MODE`，优先级从高到低）：

| 模式 | 会话目录 |
|---|---|
| `auto`（默认） | 当前目录存在 `.tanya/` 则用 `<cwd>/.tanya/sessions/`，否则用全局 |
| `local` | `<启动目录>/.tanya/sessions/` |
| `global` | `~/.local/share/tanyan/sessions/<workspace-id>/` |

只读会话（`-n` / `--no-save`，只由命令行开启，配置文件与 env 均无法设置）：`ask` 单发与 REPL 通用。历史会话照常列出与载入，之后的对话只存在于内存、不写入会话文件，也不创建会话目录（REPL 启动时在欢迎屏下方显示黄色警告，`ask` 保持静默）。

纯文本输出（`-p` / `--plain`，只由命令行开启，配置文件与 env 均无法设置）：`ask` 单发与 REPL 通用，供本程序作为子 agent 被调用时拿到可解析的输出——stdout 只承载 assistant 正文与命令反馈（无颜色、无 spinner、无光标控制、无 markdown 装饰、无工具块与状态行），stderr 承载错误与诊断；`ask` 结束时若正文已以换行结尾则不再补空行，stdout 严格等于答案。`--verbose` 必须与 `--plain` 同用，作用是在该模式下恢复工具块与状态行的**纯文本**形态（仍不启用颜色与光标控制；工具块为追加式，标题只在开始行出现一次）。

`ask` 单发默认即 plain+verbose 档（`repl.SingleShot` 把 rich 降到该档）：无 spinner 与光标重绘，工具块以追加式纯文本呈现（标题只在开始行出现一次），正文原样直出（不渲染 markdown），颜色仍按终端能力保留；显式 `-p` 可进一步压成纯答案（stdout 严格等于答案），REPL 默认档位不受影响。

REPL 输入按前缀分发：

| 输入 | 行为 |
|---|---|
| `内容` / `:内容`（全角 `：` 亦可） | 与 AI 对话（两种写法等价） |
| `/命令` | 斜杠命令（见下表）；未命中的 `/` 开头输入按对话内容处理 |
| `exit` / `quit` | 退出（等价 `/exit`） |

直通 shell 执行面已归档（恢复步骤见 `docs/design.md`）：agent 需要执行命令时经 `run_shell` 工具完成（输出截断与超时策略见其工具说明）。进程 cwd 恒为 tanyan 启动目录、全程不变；`run_shell` 默认在此执行，也可用 `cwd` 参数为单次命令指定其它目录（不影响后续调用）。

REPL 斜杠命令：

| 命令 | 说明 |
|---|---|
| `/help` | 帮助 |
| `/new` | 开启新会话（当前会话自动保存） |
| `/load [id]` | 无参打开会话选择菜单；带 id 直接载入 |
| `/stat` | 查看会话统计（工作区、token 用量、缓存） |
| `/history [n\|all]` | 无参截断列表；n 全量查看单条；all 全量显示 |
| `/model [name]` | 查看/切换模型 |
| `/think [level]` | 查看/设置思考等级（`off` 关闭） |
| `/theme [name]` | 查看/切换配色主题 |
| `/exit`（`/quit`、`exit`、`quit`） | 退出 |

会话按启动目录划分工作区（global 模式），`/load` 的会话选择菜单只显示当前项目的会话。

## AGENTS.md 注入

系统提示 = 内置默认提示（固定）+ 全局说明 + 项目说明，后两者来自：

| 层级 | 路径 | 标题 |
|---|---|---|
| 全局 | `~/.config/tanyan/AGENTS.md` | `# 全局说明（~/.config/tanyan/AGENTS.md）` |
| 工作区 | `<启动目录>/AGENTS.md` | `# 项目说明（AGENTS.md）` |

- 文件不存在或内容为空白则跳过该层；组装在会话开始（`/new`、`/load`、启动）时刻快照，会话进行中不再读取文件
- system prompt 末尾追加环境段（OS/CWD/shell 执行契约），在 agent 构建时算一次并定格、会话期间不变，不进快照、不持久化，模型可据此构造 `run_shell` 命令
- history 全程追加，同一会话内请求前缀逐字节不变，prompt cache（DeepSeek/OpenAI）逐轮命中
- 提示符模板内置固定（不可配），占位符：`{cwd}` 短路径 / `{model}` 模型 / `{effort}` 思考等级（未设置渲染为空）/ 用量与命中率按「单次请求」与「本次运行累计」两组提供，**加 `_total` 后缀即累计**：`{usage}` 上下文用量（如 `12.3k`）/ `{cache}` 本次命中量 / `{cache_rate}` 本次命中率（如 `81.67%`）/ `{usage_total}` 累计用量 / `{cache_total}` 累计命中量（如 `1.6k`）/ `{cache_rate_total}` 累计命中率（如 `65.83%`）/ `{usage_summary}` 组合显示（混合口径：前段单次用量、后段累计命中率）——`{usage}` 其后在有累计缓存数据时追加 `{cache_rate_total}`（如 `12.3k 81.67%`），独立占位符无数据均渲染为空；`/stat` 固定显示累计口径，单次请求的命中率另在响应回显行给出

## 工具

工具执行以块状格式显示（TTY 下整块暗灰 `\x1b[90m` 与主输出区分，非 TTY 纯文本）：`▸ 工具名 命令` 标题行 + 缩进输出行（stderr 行加 `2|` 前缀）+ 亮蓝状态行 `↳ exit 0 · 0.3s · 12 行`（退出码/超时/中断 · 耗时 · 输出行数），默认最多显示 20 行（`tool_output_lines` 可配，1-1000），超出仅保留头 3 行 + 尾 2 行（状态行显示 `共 N 行`）；执行开始即打印标题行（`⋯` 标记进行中）。

TTY 下带等待动画：LLM 请求等待期间显示 `⠋ 等待响应 3s`（首个 token 到达即消失），工具执行期间标题行下方显示独立 spinner 行（结束时原位重绘为最终标题）；每轮请求完成打印状态行 `  ↳ TTFT 0.8s · 3.2s · prompt 12.3k · completion 1.2k · 缓存 81.67%`（无 usage 时显示本地估算上下文，字段缺失自动省略）。非 TTY 环境动画自动关闭，状态行仍输出。

所有工具免确认执行：

| 工具 | 说明 |
|---|---|
| `run_shell` | shell 执行命令（按平台自动选择）；`cwd` 指定执行目录（默认会话启动目录，不存在则快速失败）；`timeout` 默认 60s、`interactive: true` 时 300s，上限 900s；输出截断 30000 字节 |
| `get_time` | 当前时间 |
| `get_env` | 环境变量查询（敏感变量名拒绝） |
| `calc` | 四则运算求值 |
| `agent_custom` | 读取/修改当前 agent 的模型与思考等级，查询运行态统计与可用模型；改动对下一次请求生效，仅本次会话有效 |

### 终端与信号行为

普通命令执行期间 shell 进程被移交终端前台进程组（`TIOCSPGRP`，无控制终端时自动跳过），因此 ssh/git 等需要密码的程序可直接在终端应答，不再挂死至超时。

`interactive: true` 的命令改走独立 pty 桥接（仅 Linux 实现）：命令在自己的 pty 中运行，真实 tty 切 raw 由 bridge 双向泵转，提示与输出实时可见；此时无需前台移交，`^C` 经 pty 行规程投递（`^Z` 在桥接下不挂起子进程）。桥接不可用时回退上述前台移交路径。细节见 `docs/interactive-tty.md`。

信号语义：

- 执行期间 Ctrl+C 直接送达命令进程组（命令可优雅退出）；再次按下 Ctrl+C 取消当前回合
- 普通路径下 Ctrl+Z 会挂起命令进程，tanyan 检测到后立即终止并标注 `挂起已终止`，无需等超时
- 命令间隙/流式阶段 Ctrl+Z 被 tanyan 忽略（不会挂起自身），Ctrl+\ 保持 Go 默认行为（全栈转储）
- 用户脚本内故意 `kill -STOP` 长挂起的进程会被同一机制终止


## 开发

```bash
go build ./...
go vet ./...
go test ./...
go test -race ./...
```

设计文档见 `docs/design.md`。
