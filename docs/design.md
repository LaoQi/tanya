# tanyan 核心设计

## 定位

极简命令行 AI Agent，Go 编写，无 GUI/WebUI/TUI。单二进制，交互式 REPL + 单发模式。

## 架构

```
main.go            package main：入口、flag 子命令、ask 单发
repl/              package repl：REPL 循环、斜杠命令、补全、工具视图渲染、等待动画
agent/             package agent：全部核心逻辑（config / llm / agent / shell / builtin）
readline/          package readline：自研终端输入层（editor / keys / terminal / width）
```

设计取舍：

- **不做细粒度拆包**：代码总量小，按包分职责即可
- **不做工具注册表**：工具硬编码于 `ToolDefs()` 和 `Agent.dispatch` 的 switch，存量小且预计长期以 shell 为主
- **依赖仅 2 个**：`gopkg.in/yaml.v3`（配置）、`golang.org/x/sys/unix`（raw mode）；终端输入层自研（实质替换 chzyer/readline）

## 运行模式

- `tanyan`：交互 REPL，维护内存 messages 历史，SSE 逐 token 流式输出
- `tanyan ask "问题"`：单发，输出后退出
- 全局参数：`-c <path>` 指定配置文件、`-m local/global/auto` 会话存储模式
- Ctrl+C 中断进行中的请求（context 取消，导致 API 错误直接暴露）。REPL 路径不依赖 tty ISIG 产生 SIGINT：Ask 期间切到 WatchRaw（输入 raw、保留 OPOST），watcher goroutine 经 `KeyWatcher.ReadKeyUntil` 监听 Ctrl+C 直接 cancel ctx，信号处理仅作兜底；`ask` 单发路径仍用 signal.Notify。依赖环境 termios 的旧实现会在 ISIG 被关闭的终端（被上层程序污染的 tty）下完全失效

## LLM 接入

仅 OpenAI 兼容 Chat Completions API（`/chat/completions`，SSE 流式），一套代码兼容 OpenAI/DeepSeek/GLM/Ollama/vLLM。

请求体固定字段：`model` / `messages` / `temperature` / `tools` / `stream` / `stream_options`；`reasoning_effort`（OpenAI 标准思考等级，minimal/low/medium/high/max）仅配置或 `/think` 设置后携带，`omitempty` 缺省不发送。厂商私有思考参数（GLM `thinking`、Qwen `enable_thinking` 等）不支持。

流式解析要点：`data:` 行逐条解析 JSON chunk；content 直接拼接并经回调输出；tool_calls 按 `index` 分组做增量合并（id/type/name 覆盖、arguments 拼接），`[DONE]` 结束。`stream_options.include_usage` 捕获 usage；首个 chunk 时刻记 TTFT、流结束记总耗时，存于 `Message.Stat`（`json:"-"` 不落盘）。

`/models` 列表获取：GET `/models`，按 id 排序返回，供 `/model` 命令与补全。

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
- 回调：`OnToolStart`（dispatch 内触发）/ `OnToolEnd`（结构化 `ToolResult`：Shell/Text 二选一，发回模型的 content 由 `Content()` 拼回文本），渲染在 repl 包 `toolview.go`
- **免确认直接执行**（早期版本有 y/n/a 确认机制，已移除）

### 工具视图渲染（repl/toolview.go）

- `WireToolView` 接线全部回调，块状视图：`▸ 工具名 命令` 标题行 + 缩进输出行（stderr 加 `2|` 前缀）+ 亮蓝状态行
- 状态行总是输出（TTY 亮蓝 `\x1b[94m`，非 TTY 纯文本）：`↳ exit 0 · 0.3s · 12 行`；异常时首段为 `exit 2`/`执行超时`/`已中断`/`错误: ...`；输出被截断时行数段显示 `共 N 行`；builtin 工具无状态行（截断时仅显示 `共 N 行`）
- 颜色（TTY 生效，非 TTY 纯文本）：工具块暗灰 `\x1b[90m`、spinner 橙 `\x1b[33m`、状态行亮蓝 `\x1b[94m`；全部为 16 色基本 SGR 码
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

- `/history` 无参截断列表（单行 120 rune）、`/history n` 全量查看单条、`/history all` 全量显示
- `/model` 无参实时调接口列出可用模型（`*` 标注当前，失败仍显示当前模型），带参直接切换不校验；带尾随空格支持补全（接口列表在 REPL 内首次加载后缓存，失败不重试）
- `/think` 无参显示当前思考等级（未设置显示"未设置"）；带参 `minimal/low/medium/high/max` 设置，`off` 关闭，非法值报错不变更；带尾随空格补全等级候选（含 off，静态列表）
- `/load` 无参打开方向键选择菜单（`repl/picker.go`，非 TTY 降级为序号输入），候选 Display 带时间/条数/简介

### 提示符模板

- 提示符模板内置固定不可配（`prompt` 配置项与 `TANYA_PROMPT` 已移除，yaml 残留键被忽略），未知占位符原样保留
- 占位符：`{cwd}` 短路径 / `{model}` 模型 / `{effort}` 思考等级（`ReasoningEffort()`，未设置渲染为空）/ `{usage}` 上下文 token（API 实报或 `~` 估算）/ `{cache}` 缓存命中量 / `{cache_rate}` 缓存命中率（两位小数，无数据渲染为空）/ `{stat}` 组合用量——无缓存仅总量，有缓存为 `缓存/总量 命中率`
- 默认 `\x1b[37m{cwd}\x1b[0m \x1b[34m{model}\x1b[0m \x1b[33m{effort}\x1b[0m \x1b[32m{stat}\x1b[0m \x1b[37m>\x1b[0m `（路径白 / 模型蓝 / 思考黄 / 用量绿 / 提示符白）

### 终端输入（readline 包）

- editor：行编辑/历史，快捷键 Ctrl+A/E/B/F/U/K/W/Y/T/L、Alt+B/F（按空白分词）、Home/End/方向键；render 多行感知（`cursorRow` 精确跟踪光标行，重渲染上移清屏），Size 不可用退化单行；ErrInterrupt 区分 Ctrl+C
- Tab 补全菜单：多候选时在输入行下方渲染菜单，选中项反显（`\x1b[7m`）；`↑/↓` 循环选择（菜单打开时不触发历史导航）、`Tab` 循环下一项、`Enter` 仅插入选中项（再次 Enter 提交）、`Esc` 关闭、任意输入关闭菜单正常编辑；单候选直接补全、公共前缀先行扩展的行为不变；候选超 8 行滚动窗口显示
- keys：ESC 序列/控制键/UTF-8 状态机；width：字符宽度表、ANSI 剥离、按显示宽度截断（`~` 后缀）
- 非 TTY 降级：`Degraded` 按行读取，无动画/菜单

## 配置

优先级：env（`TANYA_*`）> `~/.config/tanyan/config.yaml` > 默认值。

| 配置项 | 默认 | 说明 |
|---|---|---|
| `base_url` | `https://api.openai.com/v1` | API 地址 |
| `api_key` | 空 | 密钥（建议用 env 注入） |
| `model` | `deepseek-v4-flash` | 模型名 |
| `temperature` | 0.7 | |
| `reasoning_effort` | 空 | 思考等级 minimal/low/medium/high/max，非法值忽略；空则请求不带 `reasoning_effort` 字段 |
| `prompt` | 内置默认模板 | REPL 提示符 |
| `user_agent` | `pi/0.85.0 (...)` | 出站 UA 伪装 |
| `global_session` | `~/.local/share/tanyan/sessions` | global 模式会话基础目录，支持 `~` 展开 |
| `session_mode` | `auto` | 会话存储模式 auto/local/global |
| `tool_output_lines` | 20 | 工具输出最多显示行数（1-1000） |

env 覆盖：`TANYA_BASE_URL` / `TANYA_API_KEY` / `TANYA_MODEL` / `TANYA_TEMPERATURE` / `TANYA_REASONING_EFFORT` / `TANYA_SESSION_MODE` / `TANYA_USER_AGENT` / `TANYA_TOOL_OUTPUT_LINES`。

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
