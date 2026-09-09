# tanyan 核心设计

## 定位

极简命令行 AI Agent，Go 编写，无 GUI/WebUI/TUI。单二进制，交互式 REPL + 单发模式。

## 架构

```
main.go            package main：入口、flag 子命令、ask 单发
repl/              package repl：REPL 循环、斜杠命令、补全、工具视图渲染、等待动画
agent/             package agent：全部核心逻辑（config / llm / agent / shell / builtin）
readline/          package readline：自研终端输入层（editor / keys / terminal）
style/             package style：富文本管线（语义色/宽度截断/模板/IR，SGR 唯一产地），设计见 docs/render-pipeline.md
```

设计取舍：

- **不做细粒度拆包**：代码总量小，按包分职责即可
- **不做工具注册表**：工具硬编码于 `ToolDefs()` 和 `Agent.dispatch` 的 switch，存量小且预计长期以 shell 为主
- **依赖仅 2 个**：`gopkg.in/yaml.v3`（配置）、`golang.org/x/sys/unix`（raw mode）；终端输入层与富文本管线自研
- **颜色铁律**：SGR 序列仅 `style` 包产生（业务代码不得出现裸 `\x1b` 色码，readline 光标操作除外）；同一 IR 按终端能力档案（Profile）降级，无色终端自动纯文本

## 运行模式

- `tanyan`：交互 REPL，维护内存 messages 历史，SSE 逐 token 流式输出
- `tanyan ask "问题"`：单发，输出后退出
- 全局参数：`-c <path>` 指定配置文件、`-m local/global/auto` 会话存储模式
- Ctrl+C 中断进行中的请求（context 取消，导致 API 错误直接暴露）：REPL 与 `ask` 单发统一走 `signal.Notify(SIGINT)`（`repl.InterruptContext`），不依赖 tty ISIG；命令执行期间子进程组持有终端前台，Ctrl+C 由内核直达子进程组（命令优雅退出），再次按下取消回合

## LLM 接入

双协议并存，由 `api_protocol` 配置选择（默认 `responses`，env `TANYA_API_PROTOCOL` 可覆盖，非法值启动报错）：

### chat 协议（llm.go `chatStream`）

OpenAI 兼容 Chat Completions API（`/chat/completions`，SSE 流式），一套代码兼容 OpenAI/DeepSeek/GLM/Ollama/vLLM。

请求体固定字段：`model` / `messages` / `temperature` / `tools` / `stream` / `stream_options`；`reasoning_effort`（OpenAI 标准思考等级，minimal/low/medium/high/max）仅配置或 `/think` 设置后携带，`omitempty` 缺省不发送。设置 `reasoning_effort` 后 `temperature` 不发送（指针 + omitempty，兼容 o 系/gpt-5 仅支持 `temperature=1`），chat 与 responses 两协议一致。请求构造时剥离 `Message.ReasoningItems`（拷贝置空），思维链历史不上线 chat 端点。厂商私有思考参数（GLM `thinking`、Qwen `enable_thinking` 等）不支持。

流式解析要点：`data:` 行逐条解析 JSON chunk；content 直接拼接并经回调输出；tool_calls 按 `index` 分组做增量合并（id/type/name 覆盖、arguments 拼接），`[DONE]` 结束。`stream_options.include_usage` 捕获 usage；首个 chunk 时刻记 TTFT、流结束记总耗时，存于 `Message.Stat`（`json:"-"` 不落盘）。

### responses 协议（llm_responses.go `responsesStream`）

OpenAI Responses API 兼容格式（`/responses`），**以 DeepSeek Responses API 标准为参照**（OpenAI 兼容但不完整遵守 OpenAI：不支持/不依赖 `include`、`encrypted_content`），切换核心动机是思维链保持：

- **请求构造**：`messages[0]`(system) → 顶层 `instructions`；历史 `Message` 确定性映射为 input items——user/assistant 文本 → `message` item（content 分段 `input_text`/`output_text`）、assistant `tool_calls` → `function_call` item、tool 结果 → `function_call_output` item、assistant `ReasoningItems` → `reasoning` item（content 为明文 `reasoning_text`，输出在关联的 function_call/message 之前；`Content` 为空的 reasoning item 跳过不回传）。工具定义为内部 chat 嵌套形状，此处拍平为 `{type:"function",name,description,parameters}`。`reasoning_effort` → `reasoning.effort`；设置后 `temperature` 不发送（同 chat 协议）。
- **固定参数**：`store: false`。不携带 `include` / `encrypted_content` / reasoning `summary`（DeepSeek 均不支持）。
- **流式解析**：只解析 `data:` 行按 JSON `type` 分发。首个有效 data 事件记 TTFT（与 chat 协议对齐，纯 tool_call 响应也有 TTFT）；`response.output_text.delta` 驱动 onDelta 并置 `hasDelta`；最终 Message 以 `response.completed`（及 `response.incomplete`）事件的 `response.output[]` 终态构建——`message` 拼接 Content（收到过 text delta 则整体跳过，未收到才从 message items 拼接 output_text 补齐）、`function_call` → ToolCalls（call_id/args 整体取用）、`reasoning` → `ReasoningItems`（`id` + 明文 `content`，取终态 `reasoning_text` 原文拼接）。`response.failed`/`error` 事件返回错误。
- **usage 映射**：`input_tokens`→PromptTokens、`output_tokens`→CompletionTokens、`input_tokens_details.cached_tokens`→`CacheHit()` 既有通道、`output_tokens_details.reasoning_tokens`→`Usage.ReasoningTokens`。
- **404 提示**：第三方端点不支持时错误文案附带切换 `api_protocol: chat` 的指引。

**思维链回传与缓存（实现红线）**：reasoning `content` 随会话 jsonl 明文持久化，后续请求**原样回传**（取 `response.completed` 终态、不做任何截断/改写/规范化），以维持 DeepSeek 前缀缓存命中——history 段逐字节稳定即可命中「用户输入结束/模型输出结束」位置的缓存前缀单元；会话经 `/load` 恢复后仅需同目录同环境（`envSection` 的 `CWD`/`SHELL`/`WORKSPACE` 实时探针不变）即可命中。jsonl 序列化 HTML 转义（`\u00xx`）只在磁盘表示，读回还原，不影响请求构造。

### 协议无关约束

- `Message` 为内部规范格式（含 `ReasoningItems`），会话 jsonl 直接持久化，旧会话（无 reasoning 字段）双协议均可回放
- chat 协议忽略 `ReasoningItems`（无对应物，请求构造时剥离不上线），思维链能力为 responses 协议独有
- `/models` 列表（GET `/models`）与协议无关，按 id 排序返回，供 `/model` 命令与补全

## Agent Loop

```
用户输入 → 追加 history → 请求 LLM（流式）
  → 无 tool_calls：输出即为回答，结束
  → 有 tool_calls：逐个 dispatch 执行 → tool 结果回填 history → 再次请求
```

本地不设轮数上限，依赖模型终止；异常由 API 错误直接暴露。每轮请求触发回调：`OnRequestStart`（请求前）/ `OnResponse`（响应后，含出错路径，携带 `ResponseInfo`：Duration/TTFT/Usage/ContextTokens）。

## 工具

### run_shell（shell.go）

- 参数：`command`（必填）、`timeout`（默认 60s，上限 900s）
- 实现：按 `shellProfile` 组装命令（posix `<path> -c`、powershell `<path> -NoProfile -NonInteractive -Command`、cmd `<path> /d /s /c`），捕获 stdout/stderr/退出码/耗时（`ShellResult` 结构化返回：Command/Stdout/Stderr chunks/Err/ExitCode/TimedOut/Interrupted/Duration）
- shell 解析（`InitShell`，Agent 构造时一次性执行并缓存）：
  - 优先级：配置覆盖（`config.yaml shell:` / env `TANYA_SHELL`，名字或绝对路径，任意 shell 名允许，未知 basename 按 posix `-c` 处理）> 平台自动探测
  - 自动探测：windows 仅 `pwsh`（强制 PowerShell 7，不回退 5.1/cmd）；linux/darwin `bash` → `sh` → `ash`
  - 全部落空（含配置的 shell 不存在）：降级不报错，profile 为 nil，仅不注册 run_shell（ToolDefs 条件注册、env 段无 SHELL/TIMEOUT/OUTPUT 行、system prompt 退化为 `NoShellSystemPrompt`），dispatch 调用返回错误文案
- 程序探测：profile 就绪后对固定清单（ls/cat/head/tail/grep/rg/fd/sed/awk/find/sort/wc/cut/tr/xargs/git/curl/wget/go/node/python）逐个 LookPath，存在的拼入 run_shell 工具描述 `可用程序: ...`，仅在工具描述出现，不重复注入 env 段
- 输出捕获：stdout/stderr 各保留头 30000 字节 + 尾 30000 字节（`streamCapture` 滚动窗口），中间字节计数丢弃，模型仍可见首尾内容
- 终端前台移交（unix，shell_tty_unix.go；illumos/ios 无 x/sys ioctl 支持，降级 no-op）：执行前打开 `/dev/tty`，仅当自身进程组已是前台时 `TIOCSPGRP` 移交子进程组（`handoverForeground`），子进程结束后以 `handed` 门控归还（`restoreForeground`，避免从未交接时抢占 shell 的前台）；无控制终端 / 非前台（嵌套、后台运行）自动跳过，行为与旧版一致。子进程因此可直接在终端应答 ssh/git 等密码提示，不再静默挂死至超时
- 信号防护（`ProtectTerminalSignals`，main 启动时 `sync.Once` 一次性）：`Notify(SIGTSTP)` 吞没（命令间隙 Ctrl+Z 不挂起自身）、`Ignore(SIGTTIN/SIGTTOU)`（自身后台 tty 读写不停止）；SIGQUIT 保持 Go 默认（全栈转储）。忽略处置随 exec 被子进程继承，子进程后台读写 tty 得 EIO 而非停止
- 挂起探测（`waitShell`）：200ms 轮询 `/proc/<pid>/stat`，连续 2 次 `T` 判定被终端挂起（Ctrl+Z 等停止信号），SIGKILL 进程组并置 `Stopped`，状态行显示 `挂起已终止`，避免静默挂到超时；`processStopped` 由 shell_proc_linux.go 提供 /proc 实现，非 linux（shell_proc_other.go）恒 false（探测失效，其余功能不受影响）
- 字段集：`ShellResult` 为 Command/Stdout/Stderr chunks/Err/ExitCode/TimedOut/Interrupted/Stopped/Duration
- 回调：`OnToolStart`（dispatch 内触发）/ `OnToolEnd`（结构化 `ToolResult`：Shell/Text 二选一，发回模型的 content 由 `Content()` 拼回文本），渲染在 repl 包 `toolview.go`
- **免确认直接执行**（早期版本有 y/n/a 确认机制，已移除）

### 工具视图渲染（repl/toolview.go）

- `WireToolView` 接线全部回调，块状视图：`▸ 工具名 命令` 标题行 + 缩进输出行（stderr 加 `2|` 前缀）+ 亮蓝状态行
- 状态行总是输出（语义色 `Info`，无色环境纯文本）：`↳ exit 0 · 0.3s · 12 行`；异常时首段为 `exit 2`/`执行超时`/`已中断`/`挂起已终止`/`错误: ...`；输出被截断时行数段显示 `共 N 行`；builtin 工具无状态行（截断时仅显示 `共 N 行`）
- 颜色走 `style` 语义色 + Profile 驱动（`colors` 配置 / `NO_COLOR` / 非 TTY → 纯文本）：工具块 `Dim`、spinner `Warn`、状态行 `Info`，可用 `palette` 配置覆盖
- 显示行数上限 `tool_output_lines`（默认 20，范围 1-1000），超出保留头 3 行 + 尾 2 行并提示 `/history n` 查看完整输出
- 执行开始即打印标题行（`⋯` 标记进行中）；结束在 TTY 下 `\x1b[1A\r\x1b[K` 上移重绘标题替换 `⋯`，非 TTY 直接打印完整块
- 流式输出行尾无 `\n` 时（`lineDirty` 跟踪），状态行打印前自动补换行

### 等待动画与请求状态（repl/spinner.go）

- `OnRequestStart`：TTY 下显示 braille spinner（`⠋ 等待响应 3s`，100ms 帧，`\r\x1b[K` 行内重绘，与全部终端输出共享 mutex）；首个 delta 到达即停（纯 tool_calls 响应持续到本轮结束）
- 工具执行期间标题行下方独立 spinner 行 `  ⠋ 执行中 3s`
- 每轮请求完成打印状态行 `  ↳ TTFT 0.8s · 3.2s · prompt 12.3k · completion 1.2k · 缓存 81.67%`（字段缺失自动省略；无 usage 时显示本地估算上下文）；非 TTY 动画关闭、状态行保留

### builtin（builtin.go）

免确认轻量工具，硬编码 switch 分发：

- `get_time`：当前时间（含时区）
- `get_env`：查询环境变量，名称含 KEY/TOKEN/SECRET/PASS 的拒绝返回
- `calc`：四则运算表达式求值（自实现递归下降解析，支持 `+ - * / %`、括号、负数）

## 上下文管理

- 本地不设上限、不做裁剪：超出模型上下文时由 API 返回错误，直接暴露给用户（可 `/new` 开新会话）
- 用量统计：捕获响应 usage 作为真实值；对端不返回时降级为本地粗估（CJK 1 token/字，ASCII 0.3/字符）
- tool 结果在写入 history 前已被 shell 层截断（单项头尾各 30000 字节），避免极端膨胀

## 会话

- 每次启动/`/new` 开启新会话，id 为启动时间戳（`20060102-150405`）
- 存储模式（CLI `-m` > env `TANYA_SESSION_MODE` > 配置 `session_mode`，默认 auto）：
  - `auto`：当前目录存在 `.tanya/` → local，否则 global
  - `local`：`<启动目录>/.tanya/sessions/`（.tanya 本身即项目隔离，不叠加 workspace-id）
  - `global`：`global_session/<workspace-id>/`，workspace-id 由启动目录派生（可读路径转义 + 短哈希）
- 落盘：`<timestamp>.jsonl`，每轮结束追加写入新消息（一行一条 Message JSON），记录完整历史（回放/审计用）
- 首行持久化 system prompt 快照（`systemSaved` 标志防重复），`/load` 还原后前缀与当初逐字节一致；旧格式文件（无 system 首行）回退为载入时快照当前 AGENTS.md，且保持不补写；system 行不计入 `/sessions` 条数与标题
- 会话列表扫描：`agent.New` 启动时预扫描填充缓存；`ListSessions` 按 (mtime,size) 增量刷新，仅重扫变化的文件；每文件 `bufio` 逐行计数条数、仅解码至首条 user 消息取摘要
- `/sessions` 列出（id、时间、消息数、首条用户消息摘要），`/load <id>` 恢复继续对话；id 校验拒绝路径穿越

## 系统提示与缓存友好

- 组装规则：`DefaultSystemPrompt`（内置，固定不可配，`system_prompt` 配置项已移除）+ 全局 `~/.config/tanyan/AGENTS.md`（存在时）+ 工作区 `./AGENTS.md`（存在时），各段以 `# 全局说明`/`# 项目说明` 标题分隔，文件缺失/空白跳过
- 规则与事实分离：persistPrompt（上述规则）在 `/new`/`/load` 时组装并冻结进会话首行；每次请求的 system = persistPrompt + `envSection(cwd)`（环境事实实时拼在末尾，不持久化、不冻结）
- 快照机制：`/new` 与 `/load` 时刻读取 AGENTS.md 组装快照；会话进行中零文件 IO，快照冻结；旧格式会话（system 首行含历史环境段）原样保留并标记，`/load` 时提示 `/new`
- 缓存收益：history 全程 append-only，同一会话内 messages 前缀逐字节不变，prompt cache 逐轮全量命中；`/new` 时 AGENTS.md 未变则 system 前缀跨会话命中
- 缓存命中捕获（DeepSeek `prompt_cache_hit_tokens` / OpenAI `prompt_tokens_details.cached_tokens`）经 `PromptCache()`/`PromptCacheRate()` 供提示符占位符显示

## REPL

用户可见文案统一为常量：`repl/messages.go`（UI/命令输出/选择器/工具视图/spinner）与 `agent/messages.go`（错误/ToolResult 文本/ContextInfo），调用一律 `Printf`/`Fprintf` 引用常量，换行由调用处的格式串控制；`Bye`/`再见` 已统一为 `MsgBye`。工具描述与系统提示不在此列（模型侧文案，翻译需评估 prompt 影响）。

### 斜杠命令

`/help` `/new` `/sessions` `/load` `/context` `/history` `/model` `/exit`：

- `/history` 无参截断列表（单行 120 rune）、`/history n` 全量查看单条、`/history all` 全量显示；全量显示时消息头 `#N 角色` 按一级标题渲染（`#` 与序号连写不构成 markdown 标题，单独构造 Heading IR），assistant 正文走与对话一致的 Markdown 渲染（受 `/md` 开关与 TTY 旁路约束），user/tool 消息与工具参数原样
- `/model` 无参实时调接口列出可用模型（`*` 标注当前，失败仍显示当前模型），带参直接切换不校验；带尾随空格支持补全（接口列表在 REPL 内首次加载后缓存，失败不重试）
- `/think` 无参显示当前思考等级（未设置显示"未设置"）；带参 `minimal/low/medium/high/max` 设置，`off` 关闭，非法值报错不变更；带尾随空格补全等级候选（含 off，静态列表）
- `/load` 无参打开方向键选择菜单（`repl/picker.go`，非 TTY 降级为序号输入），候选 Display 带时间/条数/简介

### 提示符模板

- 提示符模板内置固定不可配（`prompt` 配置项与 `TANYA_PROMPT` 已移除，yaml 残留键被忽略），模板走 `style` 管线：启动时 `ParseTemplate` 一次，每轮 `Bind` 占位符 + 渲染（解析仅一次，绑定微秒级）
- 模板语法为 BBCode 风格标记：`[white]{cwd}[/] [blue]{model}[/]`，空格叠属性 `[red bold]`，支持语义名（dim/info/warn/ok/error/accent）；未知名/游离闭合/空标签降级原样，合法标签未闭合着色到行尾；旧裸 ANSI 模板自动 passthrough 兼容（无色环境 `Strip` 兜底）
- 占位符：`{cwd}` 短路径 / `{model}` 模型 / `{effort}` 思考等级（未设置渲染为空）/ `{usage}` 上下文 token（API 实报或 `~` 估算）/ `{cache}` 缓存命中量 / `{cache_rate}` 缓存命中率（两位小数，无数据渲染为空）/ `{stat}` 组合用量——无缓存仅总量，有缓存为 `缓存/总量 命中率`；未知占位符原样保留，占位符值永不二次解析
- 默认 `[white]{cwd}[/] [blue]{model}[/] [yellow]{effort}[/] [green]{stat}[/] [white]>[/] `（路径白 / 模型蓝 / 思考黄 / 用量绿 / 提示符白），渲染字节与旧 ANSI 版逐字节一致

### 终端输入（readline 包）

- editor：行编辑/历史，快捷键 Ctrl+A/E/B/F/U/K/W/Y/T/L、Alt+B/F（按空白分词）、Home/End/方向键；render 多行感知（`cursorRow` 精确跟踪光标行，重渲染上移清屏），Size 不可用退化单行；ErrInterrupt 区分 Ctrl+C；Ctrl+L 推屏保历史（2×rows 个换行把可见内容滚入回滚区、光标回视口顶部重画提示符，Size 不可用退化 `\x1b[2J` 擦屏），历史保留量受终端 scrollback 容量限制
- Tab 补全菜单：多候选时在输入行下方渲染菜单，选中项反显（`\x1b[7m`）；`↑/↓` 循环选择（菜单打开时不触发历史导航）、`Tab` 循环下一项、`Enter` 仅插入选中项（再次 Enter 提交）、`Esc` 关闭、任意输入关闭菜单正常编辑；单候选直接补全、公共前缀先行扩展的行为不变；候选超 8 行滚动窗口显示
- keys：ESC 序列/控制键/UTF-8 状态机；width：`style` 薄包装（宽度表/ANSI 剥离/感知截断均由 `style` 提供，截断自动复位悬空 SGR 防串色）
- 非 TTY 降级：`Degraded` 按行读取，无动画/菜单
- 平台划分：termios 请求常量按平台族分文件（`termios_sysv.go` linux/android/aix/solaris 用 TCGETS/TCSETS/TCSETSF，`termios_bsd.go` darwin/freebsd/netbsd/openbsd/dragonfly 用 TIOCGETA/TIOCSETA/TIOCSETAF）；illumos/ios 在该版 x/sys 无 ioctl 支持，`terminal_unix_stub.go` 直接返回 ErrUnsupported 走 Degraded 降级，保证全 unix GOOS 可编译

## 配置

优先级：env（`TANYA_*`）> `~/.config/tanyan/config.yaml` > 默认值。

| 配置项 | 默认 | 说明 |
|---|---|---|
| `base_url` | `https://api.openai.com/v1` | API 地址 |
| `api_key` | 空 | 密钥（建议用 env 注入） |
| `model` | `deepseek-v4-flash` | 模型名 |
| `temperature` | 0.7 | |
| `reasoning_effort` | 空 | 思考等级 minimal/low/medium/high/max，非法值忽略；空则请求不带 `reasoning_effort` 字段 |
| `colors` | `auto` | 终端配色 auto（跟随终端能力与 `NO_COLOR`）/ on（强制开色）/ off（强制纯文本） |
| `theme` | `nord` | 内置配色主题（语义色/提示符/markdown 标题与代码整体切换）：default/minimal/solar/vivid/nord/gruv/dusk，非法值启动报错 |
| `palette` | 空 | 语义色覆盖（info/warn/ok/error/dim/accent → 色名），叠加在当前主题之上（切换主题后自动重放） |
| `user_agent` | `pi/0.85.0 (...)` | 出站 UA 伪装 |
| `global_session` | `~/.local/share/tanyan/sessions` | global 模式会话基础目录，支持 `~` 展开 |
| `session_mode` | `auto` | 会话存储模式 auto/local/global |
| `tool_output_lines` | 20 | 工具输出最多显示行数（1-1000） |

env 覆盖：`TANYA_BASE_URL` / `TANYA_API_KEY` / `TANYA_MODEL` / `TANYA_TEMPERATURE` / `TANYA_REASONING_EFFORT` / `TANYA_SESSION_MODE` / `TANYA_THEME` / `TANYA_USER_AGENT` / `TANYA_TOOL_OUTPUT_LINES`。

配色主题：`style/theme.go` 内置 `Scheme` 聚合（语义色 + 提示符模板 + markdown `Theme`），`ApplyScheme` 更新全局语义色并叠加用户 `palette` 覆盖；REPL `/theme [name]` 切换后提示符与渲染器即时重建，默认启动主题取 `theme` 配置。`DefaultPrompt` 常量归属 style 包（default 主题提示符），`agent.DefaultPrompt` 仅为兼容引用。

## 测试

标准库 `testing` + `httptest` mock LLM（`agent/mock_test.go`，脚本化 `mockStep`，content/arguments 多 chunk 发送以覆盖流式合并）。覆盖 calc/shell/config/SSE 解析/trim/会话往返/Ask 全链路/回调。readline 用 fakeTerm 注入按键，真实终端行为 pty 人工验证。repl 覆盖渲染纯函数与非 TTY 降级。

## 环境探针（envprobe）

- 定位：只注入模型无法廉价自探的最小事实集——平台事实与 run_shell 执行契约；工具清单不注入 prompt（function calling 已完整提供），工具版本/分支/目录列表等易变信息模型可按需自探，一律不预注入
- 组装：`runtimePrompt()` = persistPrompt（规则，冻结）+ 空行 + `envSection(cwd, probe)`（实时拼在末尾）；环境注入恒定生效，无配置开关（曾有 `probe` 配置项，review 后移除）
- 输出格式（约 6 行紧凑键值，全部源自 `runtime` 与 `shell.go` 常量，同 cwd 下字节级确定）：

  ```
  # 环境
  OS: linux/amd64
  CWD: ~/Project/tanya
  SHELL: /usr/bin/bash -c（非交互，无 TTY）
  TIMEOUT: 默认 60s，上限 900s
  OUTPUT: stdout/stderr 头尾各 30KB，中间截断
  WORKSPACE: go.mod, Makefile
  ```

- 事实源单一：SHELL/TIMEOUT/OUTPUT 三行由解析后的 `shellProfile` 与 `shell.go` 常量程序化生成（`invocation()`/`shellTimeoutSec`/`shellTimeoutLimit`/`shellMaxOutput`），无第二份硬编码描述；shell 不可用时三行整体省略
- 探测机制：`envSection` 为纯函数，WORKSPACE 标记文件（`os.Stat`，8 种标志文件固定顺序）经注入的 `envProbeFunc` 取得；shell 契约读包级 `ShellRuntime()`（`InitShell` 在 Agent 构造时解析缓存，envprobe 不再自行 LookPath）；主路径零 exec、零易变信息
- 可测性：分层测试——persistPrompt 只含规则 / envSection 注入 fake probe 断言渲染 / runtimePrompt 拼接（probe 为 nil 时退化） / 同参数两次渲染字节相等
