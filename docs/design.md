# tanya 核心设计

## 定位

极简命令行 AI Agent，Go 编写，无 GUI/WebUI/TUI。单二进制，交互式 REPL + 单发模式。

## 架构

```
main.go            package main：入口、flag 子命令、ask 单发
repl/              package repl：REPL 循环、斜杠命令、补全、工具视图渲染、状态行心跳
agent/             package agent：全部核心逻辑（config / llm / llm_http / agent / tools / prompt / session / session_archive / stats / shell / builtin）
readline/          package readline：自研终端输入层（editor / keys / terminal），pty 桥接与终端状态自愈
ctty/              package ctty：控制终端原语（前台组读写、/dev/tty、SIGTTIN/SIGTTOU）与终端探测（Facts：isatty/尺寸/VT），白名单 linux/darwin/windows，零依赖叶子
render/            package render：渲染管线（IR → ANSI：Renderer、提示符模板），可 import 其下子包
render/style/      样式词汇与编码（SGR 唯一产地）
render/term/       终端原语（ANSI 词法/清洗、宽度/截断、光标控制、能力档案；零依赖叶子）
render/ir/         渲染 IR（Block/Inline）
render/theme/      配色（语义色/方案/markdown 样式集/palette；无全局可变状态）
render/markdown/   流式 markdown 解析
render/markup/     内联标记解析
设计见 docs/render-pipeline.md 与 docs/style-split.md
```

设计取舍：

- **不做细粒度拆包**：代码总量小，按包分职责即可
- **不做动态工具注册**：工具经 `Tool` 接口（`agent/tools.go`）自述名/描述/参数并提供执行，`allTools()` 编译期显式列清单（`run_shell` + `builtinTools()` + `agent_custom`），`toolRegistry.lookup` 线性扫描（N=4 实测快于 map，现 N=5，不做索引），无插件/运行时注册，存量小且预计长期以 shell 为主
- **依赖仅 2 个**：`gopkg.in/yaml.v3`（配置）、`golang.org/x/sys`（`unix` 做 termios/pty、`windows` 做控制台探测）；终端输入层与富文本管线自研
- **颜色铁律**：SGR 与 CSI 仅 `render/style`、`render/term` 产生（业务代码不得出现裸 `\x1b`，readline 的光标操作也走 `term.Cursor*`）；同一 IR 按终端能力档案（`term.Profile`）降级，无色终端自动纯文本。档案由 `main` 单点探测（`ctty.Probe()`）后注入：**渲染类判定看 stdout 是否终端、输入类看 stdin**，见 `docs/terminal-caps.md`

事实归属（不设共享暴露层）——这些结论不再重复讨论：

- **进程事实**：`cwd`、家目录与工作区基准在 `agent.New` 读一次、注入 `shellTool` 构造期定格（旧六参形态与包级 shell 状态已删，见 `docs/shell-tool.md` §14/§16）
- **`ctx` 属请求层**：只承担取消/超时，不承载进程事实（`repl.Run()` 不收 ctx；每回合由 `InterruptContext()` 现造，以 `context.Background()` 为根）
- **tty 与颜色能力由消费方独占**：`term.Profile` 由 `term.DetectProfile` 计算、只有 `term` 保留进程级默认档案（终端能力是名副其实的进程事实）；语义色 `theme.Semantics` 为值传递（repl 持有当前方案、readline 经 `SetStyles` 注入，见 `docs/style-split.md`）；终端尺寸是实时值（`ToolWidth` 以函数传递）；启动前台状态与 `ISIG` 自愈归 readline（`InitTerminalGuard`/`SecureTerminal`）；`ctty.Supported` 是编译期平台常量；前台组读/写、`/dev/tty` 打开、`SIGTTIN/SIGTTOU` 忽略等**控制终端原语**统一在零依赖叶子包 `ctty`（`docs/ctty.md`），`agent`（是否移交前台）与 `readline`（是否夺回前台）各自持有策略，共享原语、不合并决策

## 运行模式

- `tanya`：交互 REPL，维护内存 messages 历史，SSE 逐 token 流式输出
- `tanya ask "问题"`：单发，输出后退出。单发默认走 plain+verbose 档（`repl.SingleShot` 在 CLI 模式为 rich 时降到 `modePlainVerbose`；显式 `-p` 更窄则保持不动）：无状态行与心跳、无光标控制（工具块追加式）、正文原样直出，颜色仍按终端能力保留，`End()` 按 plain 语义"缺行尾换行才补"
- `tanya init`：新工作区脚手架，建 `<cwd>/.tanya/sessions/`、询问后建 `<cwd>/.tanya/.gitignore`（内容 `*`）、缺口时建 `<cwd>/AGENTS.md` 骨架，随后与普通模式无异地进入 REPL（见下节）
- 全局参数：`-c <path>` 指定配置文件、`-m local/global/auto` 会话存储模式、`-n` / `--no-save` 只读会话（见《会话与上下文》存储小节）
- Ctrl+C 中断进行中的请求（context 取消，导致 API 错误直接暴露）：REPL 与 `ask` 单发统一走 `signal.Notify(SIGINT)`（`repl.InterruptContext`），要求终端 `ISIG` 开启——readline 侧每回合开始前做终端状态自愈保证该项成立（`docs/interactive-tty.md` §5.9）；命令执行期间子进程组持有终端前台，Ctrl+C 由内核直达子进程组（命令优雅退出），再次按下取消回合

### init 模式（`agent/init.go` + `repl/initflow.go`）

新工作区（通常既无 `.tanya/` 也无 `AGENTS.md`）的一次性脚手架，之后与普通 REPL **完全无二**：不改提示符、不加斜杠命令、不改运行期行为。只作用于启动目录，不做项目探测、不调模型、不碰 `~/.config/tanya/*`。

- 三项动作，逐项幂等、永不覆盖既有文件：建 `<cwd>/.tanya/sessions/`（0755）→ 询问后建 `<cwd>/.tanya/.gitignore`（`*\n`，0644）→ 缺 `AGENTS.md` 时建骨架（`# <目录名>` + `## 项目说明` + `## 构建与测试` 两小节 + 生成标记注释，0644）
- **顺序不变量**：`main` 在配置与 `-m` 覆盖之后、`agent.New` 之前调用 `repl.RunInit`。`.tanya/` 既是会话落点、也是 `session_mode: auto` 的判定依据（`resolveSessionDir` 的 `isDir(cwd/.tanya)`），先建后 New 才知道本次启动要落本地工作区，首个会话即写入 `<cwd>/.tanya/sessions/`；同一次构造里 `promptBuilder` 也随即读到新生成的 AGENTS.md（进入本次会话 system 快照，`/new` 时重读）
- 忽略文件走交互确认：`ctty.Open()` 打开 `/dev/tty` 成功才提问（`是否…？[y/N]`，仅 `y`/`yes` 为真），失败即非交互（管道调用、无控制终端、Windows stub）不提问也不创建，报告里以 `MsgInitSkipNoTTY` 说明并给出手动命令；用户拒绝为 `MsgInitSkipDeclined`。既有 `.gitignore` 时不再提问
- 报告：`repl.RunInit` 编排（头行 → 询问 → `agent.InitWorkspace` → 条目与会话目录行 + 一行提示），走 `st.Print`（KindNotice，plain 下仍可见），标记着色只用 `sem.Ok`/`sem.Dim`；条目路径相对工作区显示，头行与会话目录经 `initPath` 做 `~` 归约（不用提示符的 `shortPath` 缩写，避免报错路径被压缩）；`SessionDir` 取自 `resolveSessionDir(cfg, cwd)`，与 `agent.New` 同函数同输入，显式 `-m global` 时如实报告 global 落点（`.tanya/` 标记照建）
- 失败即中止：任一项创建失败（`AGENTS.md` 是目录、`sessions` 是文件、写入出错）返回 `MsgInitFailFmt` 错误，`main` 打印后以 1 退出、不进 REPL；幂等使重试安全
- CLI：`tanya init` 无参数（带多余参数报 `MsgInitUsage`），`-n` 只读会话与 init 不冲突（骨架照建，会话不写盘）；`repl.ParseCommand` 统一解析 `ask`/`init`，未知首 token 保持旧行为（忽略并进 REPL）

## LLM 接入

双协议并存，由 `api_protocol` 配置选择（默认 `responses`，env `TANYA_API_PROTOCOL` 可覆盖，非法值启动报错）：

### chat 协议（llm.go `chatStream`）

OpenAI 兼容 Chat Completions API（`/chat/completions`，SSE 流式），一套代码兼容 OpenAI/DeepSeek/GLM/Ollama/vLLM。

请求体固定字段：`model` / `messages` / `temperature` / `tools` / `stream` / `stream_options`；`reasoning_effort`（OpenAI 标准思考等级，minimal/low/medium/high/max）仅配置或 `/think` 设置后携带，`omitempty` 缺省不发送。设置 `reasoning_effort` 后 `temperature` 不发送（指针 + omitempty，兼容 o 系/gpt-5 仅支持 `temperature=1`），chat 与 responses 两协议一致。厂商私有思考参数（GLM `thinking`、Qwen `enable_thinking` 等）不支持。

思维链按 DeepSeek 思考模式文档处理：响应侧 `delta.reasoning_content` 增量累积为单条 `ReasoningItem`（`ID` 空）随会话落盘（`reasoning_items` 字段）；请求侧 `chatWireMessages` 把 `ReasoningItems` 顺序拼接折叠为 assistant 消息顶层 `reasoning_content` 回传（带 `tools` 时官方要求历史推理链完整回传，缺失属未定义行为），wire 上不出现 `reasoning_items`，历史无思维链的轮次省略该字段。第三方端点对缺失 `reasoning_content` 的宽容度不一，四场景探测脚本见 `scripts/chat_reason_probe.py`（自建透传网关实测四种形状均 200，未执行该硬校验）。

流式解析要点：`data:` 行逐条解析 JSON chunk；content 直接拼接并经回调输出；tool_calls 按 `index` 分组做增量合并（id/type/name 覆盖、arguments 拼接），`[DONE]` 结束。`stream_options.include_usage` 捕获 usage（`completion_tokens_details.reasoning_tokens` 经 `Usage.normalize()` 归一为 `Usage.ReasoningTokens`，与 responses 口径对齐）；首个 chunk 时刻记 TTFT、流结束记总耗时，存于 `Message.Stat`（`json:"-"` 不落盘）。

### responses 协议（llm_responses.go `responsesStream`）

OpenAI Responses API 兼容格式（`/responses`），**以 DeepSeek Responses API 标准为参照**（OpenAI 兼容但不完整遵守 OpenAI：不支持/不依赖 `include`、`encrypted_content`），切换核心动机是思维链保持：

- **请求构造**：`messages[0]`(system) → 顶层 `instructions`；历史 `Message` 确定性映射为 input items——user/assistant 文本 → `message` item（content 分段 `input_text`/`output_text`）、assistant `tool_calls` → `function_call` item、tool 结果 → `function_call_output` item、assistant `ReasoningItems` → `reasoning` item（content 为明文 `reasoning_text`，输出在关联的 function_call/message 之前；`Content` 为空的 reasoning item 跳过不回传）。工具定义为内部 chat 嵌套形状，此处拍平为 `{type:"function",name,description,parameters}`。`reasoning_effort` → `reasoning.effort`；设置后 `temperature` 不发送（同 chat 协议）。
- **固定参数**：`store: false`。不携带 `include` / `encrypted_content` / reasoning `summary`（DeepSeek 均不支持）。
- **流式解析**：只解析 `data:` 行按 JSON `type` 分发。首个有效 data 事件记 TTFT（与 chat 协议对齐，纯 tool_call 响应也有 TTFT）；`response.output_text.delta` 驱动 onDelta 并置 `hasDelta`；最终 Message 以 `response.completed`（及 `response.incomplete`）事件的 `response.output[]` 终态构建——`message` 拼接 Content（收到过 text delta 则整体跳过，未收到才从 message items 拼接 output_text 补齐）、`function_call` → ToolCalls（call_id/args 整体取用）、`reasoning` → `ReasoningItems`（`id` + 明文 `content`，取终态 `reasoning_text` 原文拼接）。`response.failed`/`error` 事件返回错误。
- **usage 映射**：`input_tokens`→PromptTokens、`output_tokens`→CompletionTokens、`input_tokens_details.cached_tokens`→`CacheHit()` 既有通道、`output_tokens_details.reasoning_tokens`→`Usage.ReasoningTokens`。
- **404 提示**：第三方端点不支持时错误文案附带切换 `api_protocol: chat` 的指引。

**思维链回传与缓存（实现红线）**：reasoning `content` 随会话 jsonl 明文持久化，后续请求**原样回传**（取 `response.completed` 终态、不做任何截断/改写/规范化），以维持 DeepSeek 前缀缓存命中——history 段逐字节稳定即可命中「用户输入结束/模型输出结束」位置的缓存前缀单元；会话经 `/load` 恢复后仅需同目录同环境（env 段在 `agent.New` 构造期定格、进程内逐字节不变）即可命中。jsonl 序列化 HTML 转义（`\u00xx`）只在磁盘表示，读回还原，不影响请求构造。

### 协议无关约束

- `Message` 为内部规范格式（含 `ReasoningItems`），会话 jsonl 直接持久化，旧会话（无 reasoning 字段）双协议均可回放
- `ReasoningItems` 为两协议共用的内部思维链表示：responses 回传为独立 reasoning item（`ID` 空时省略 `id` 字段，兼容 chat 侧落盘的思维链），chat 回传为 assistant 消息的 `reasoning_content` 字符串（多 item 顺序拼接）
- `/models` 列表（GET `/models`）与协议无关，按 id 排序返回，供 `/model` 命令与补全
- HTTP/SSE 公共骨架（`llm_http.go`）：`endpoint`/`newRequest`/`do`/`streamSSE`/`scanSSE` 为两协议共用——POST + 四 header、状态码非 200 时读 4096 字节 body 包成类型化 `httpError`（`Error()` 即 `MsgAPIStatus` 格式）、`data:` 前缀与 `[DONE]` 终止、scanner 错误包 `MsgReadStream`。协议差异只剩 URL 路径、请求体结构、事件分派与 404 hint（responses 独有，在 `responsesStream` 里以 `errors.As` 判定后转换）

## Agent Loop

```
用户输入 → 追加 history → 请求 LLM（流式）
  → 无 tool_calls：输出即为回答，结束
  → 有 tool_calls：逐个 dispatch 执行 → tool 结果回填 history → 再次请求
```

本地不设轮数上限，依赖模型终止。每轮请求发事件：`EventRequestStart`（请求前）/ `EventResponse`（响应后，含出错路径，携带 `ResponseInfo`：Duration/TTFT/Usage/ContextTokens）；事件词汇表见 `agent/event.go`，渲染侧以 `agent.EventSink` 单通道接收。

回合非正常结束（`Ask`，按"有无产出"分派，产出 = 本回合出现过完整 assistant/tool 消息）：

- 用户中断（`ctx` 取消）：有产出 → 保留全部产出并追加 user 提示 `[用户已中断本轮请求]` 后落盘；无产出 → 回滚到回合起点
- 其它错误：有产出 → 保留产出并追加 user 提示 `[本轮因错误中止：<错误摘要>]`（摘要 200 rune 截断）后落盘；无产出 → 回滚
- 终止提示为 user 角色、仅陈述事实不含引导词；写入 history 后不可变（append-only 落盘），保证请求前缀稳定与 prompt cache 命中；`save()` 失败静默，`saved` 游标未推进时下次成功回合补齐
- 中断返回 `*InterruptError{Kept}`（REPL 据此区分"已保留/未保留"文案），错误路径返回原错误；已完成的工具步骤（含被 SIGINT 终止的 run_shell，其结果照常回填）随产出一并保留，续接时模型可回溯此前实施

## 工具

工具统一经 `Tool` 接口（`agent/tools.go`）声明：`Name()` 给名字、`Definition()` 给描述与参数 schema（即 wire 上的 function 定义）、`Invoke()` 给执行——**描述、参数与执行同处一个实现**，不再有独立清单文件。`allTools(shell, ctl)` 编译期显式列出全集（`run_shell` 在前、`builtinTools()` 居中、`agent_custom` 在末尾），顺序即请求体 `tools` 段顺序（prompt cache 依赖，见 `docs/cache-probe.md`）；`newToolRegistry` 持有该 slice，`defs()` 供 `NewClient` 构造期注入，`lookup` 线性扫描（N=4 实测快于 map，现 N=5，不做索引）。`Agent.dispatch` 退化为查表，未命中回 `MsgUnknownTool`。需要终端直通的工具可额外实现窄接口 `interactiveTool`（当前仅 `run_shell`），供 `runTurn` 在 `EventToolStart/End` 上提前标记 `Interactive`。代价是 `run_shell` 的参数被解析两次——`interactiveOf`（`runTurn` 取 `Interactive`）与 `Invoke` 各一次；这是「参数对通用 `Tool` 接口不透明」与「`EventToolStart` 必须在执行前携带参数派生字段」两条约束相交的**有意保留**结果（实测单次 678ns，对比一次 `bash -c true` 1.26ms 可忽略），不是待办。

### run_shell（`agent/shelltool.go` 组件 + `agent/shell.go` 叶子）

- 参数：`command`（必填）、`cwd`（可选，命令执行目录，默认会话启动目录）、`timeout`（默认 60s，上限 900s）、`interactive`（布尔，默认 false）
- 执行目录：默认继承进程 cwd（= 会话启动目录，进程全程不 `os.Chdir`）；显式 `cwd` 时设 `cmd.Dir`（不改进程 cwd），解析规则为 `~`/`~/x` 展开家目录、相对路径按工作区基准合成（家目录与工作区由 `agent.New` 各读一次注入 `shellTool`，构造后只读；缺基准时相对路径直接失败而非退回环境 cwd），随后 `os.Stat` 校验——不存在或非目录直接快速失败（`MsgBadCwd`，不启动进程）。显式指定时 `ShellResult.Cwd` 填充解析后的绝对路径，`String()` 首行输出 `cwd: <路径>`。桥接与回退两条路径均生效（`TTYBridge.Prepare` 只改 `SysProcAttr`/标准流/`Env`，不覆盖 `cmd.Dir`）
- 波浪号边界（不宣传的默认契约）：只处理 `~` 与 `~/x`（`~` 展开家目录，基准由 `agent.New` 注入）；`~user` 与 Windows 风格 `~\x` 一律不展开——按相对路径解析并因不存在直接报 `MsgBadCwd`（快速失败，不误执行）。`run_shell` 工具描述与 `cwd` 参数描述均不提 `~`（避免引导模型使用）；env 段 `CWD:` 行的 `~/...` 只是完整路径的显示缩写，不是路径语法引导
- 交互模式（`interactive: true`）：仅由模型显式声明，**不做命令文本猜测**（早期版本有 sudo/ssh 关键词兜底，review 后移除）。声明后 repl 侧不发状态行心跳、标题行下打印引导行、结束用追加式渲染（避免 `CursorUp` 擦掉用户输入回显）；`timeout` 缺省时默认放宽至 300s（显式值优先，上限仍 900s）。命令在 **Linux 独立 pty** 中运行（见下条），提示与输出实时可见；非桥接回退路径下命令提示须自行写入 `/dev/tty`，否则被工具捕获不可见；**Windows 走控制台继承直通（B2）**——`ctty.Open` 返回 `CONIN$` 作子进程 stdin 可直接应答，写控制台的提示（ssh 等）实时可见、写 stdout 的提示随输出捕获，运行期 `IgnoreCtrlEvents` 掩蔽本进程 ^C（Ctrl+Break 不受掩蔽，保留为紧急中断），详见 `docs/terminal-caps.md` §8.6
- 交互式 pty 桥接（`readline/bridge_linux.go` + `agent/tty_bridge.go`，linux 专用；决策与背景见 `docs/interactive-tty.md`）：解决"交互程序拿不到输入"（`/dev/tty` 直通导致 `ttyname(0)` 退化为 `/dev/tty`、pinentry 等无 ctty 程序无法按路径打开）。流程 `Prepare`（分配 pty、`Setsid+Setctty+Ctty=0`、三条标准流全接 slave、`GPG_TTY`/`SSH_TTY` 覆盖为 slave 路径）→ `Attach`（真实 tty 切 raw、初始尺寸复制到 master、启动双向泵）→ `cmd.Start()` → 立即关闭父进程 slave（否则子进程退出后 master 收不到 EIO）→ `waitShell` → `stop()`（恢复 termios、关闭 tty/master、泵收尾 drain 后 `capture.finish()`）
  - 契约：master 输出**同时**写真实 tty（用户实时可见）与 `capture`（Writer，调用方决定去向）。本处 capture 即 `streamCapture` → `ShellResult.Stdout`，交互模式为**单流**（`Stderr` 空，`2|` 区分失效）；`streamCapture` 头尾截断与 `ShellResult` 字段语义不变
  - 子进程成为独立会话首进程、ctty 为 pty slave，真实 tty 前台组始终是 tanya，**不再需要 `TIOCSPGRP` 移交**；`^C` 经泵作为字节进入 pty，由 slave 行规程投递 `SIGINT` 给子进程前台组，tanya 不拦截
  - 已知语义差异：`Setsid` 后子进程组为孤儿进程组，内核按 POSIX 丢弃停止信号，**`^Z` 在桥接下不挂起子进程**（无效按键，`^C` 正常）；按 `docs/interactive-tty.md` §7 沿用现状、不新增分支（`waitShell` 的挂起轮询保留，显式 `SIGSTOP` 等仍检出）
  - 泵用 `poll` + 自管道唤醒（`stop()` 关写端令两向阻塞读退出），保证 stop 不悬挂、不漏读残留输出；写侧 `O_NONBLOCK` + `POLLOUT` 防子进程不消费时卡死
  - 接口契约：**单次使用、非并发**——`Prepare → Attach → stop` 各一次；实例带 busy/attached 守卫（互斥量），并发或重复调用一律返回 `ErrUnsupported` 走回退，避免 pty/泵泄漏（为 §6"用户前台命令"复用的地基预留）
  - 失败回退：无控制终端 / 非前台（`TIOCGPGRP != getpgrp`）/ pty 分配失败 / `SetNonblock` 失败 / `Attach` 失败 / 非 Linux（`bridge_stub.go` 返回 `ErrUnsupported`）→ 走原 `open("/dev/tty")` + `TIOCSPGRP` 路径，非交互路径行为零变化
  - 终端状态自愈（`readline/secure.go`）：桥接 `Prepare` 的前台检查**之前**与 REPL 每回合开始前调用 `SecureTerminal()`——① 恢复被外部清掉的 `ISIG`（否则 `^C` 不产生 `SIGINT`，中断路径完全失效）② **恢复被外部清掉的 canonical/回显/输出后处理**（`ICANON|ECHO|IEXTEN|ICRNL|IXON|OPOST|ONLCR`，2026-09-15 由"只修 ISIG"扩宽：终端被任何外部程序留成 raw 时，tanya 下一回合即可自愈，而不是整场会话按损坏状态渲染）③ 限"启动瞬间自己就是终端前台作业"时夺回被 shell 抢占的前台组；后台启动（`&`）/无控制终端场景门控为否，语义不变（`docs/interactive-tty.md` §5.9）
  - 结果语义：被信号终止的子进程按 shell 惯例记 `128 + signum`（`^C` → `exit 130`、`SIGKILL` → `137`），不再是 `-1`（`platform.ExitCode`，posix 取 `WaitStatus.Signaled()`）
  - 刻意简化（`docs/interactive-tty.md` §7 登记，评审直接引用关闭）：`^C` 计数双杀、中断结果标记、输出清洗、`SIGWINCH` 转发（尽力而为，失败不报错）、桥接期显示对齐
- 实现：按 `shellProfile` 组装命令（posix `<path> -c`、powershell `<path> -NoProfile -NonInteractive -Command`、cmd `<path> /d /s /c`；interactive: true 时两条交互路径均过滤 powershell 的 `-NonInteractive`——该模式下 Read-Host 直接抛错，非交互运行不受影响），捕获 stdout/stderr/退出码/耗时（`ShellResult` 结构化返回：Command/Cwd/Stdout/Stderr chunks/Err/ExitCode/TimedOut/Interrupted/Stopped/NotStarted/Duration）
- 中断语义：运行中被 ctx 取消 → `Interrupted`，结果追加 `error: 已中断（进程已终止，输出可能不完整）`；ctx 已取消导致命令未能启动 → `Interrupted+NotStarted`，追加 `error: 已中断（命令未执行）`（不再把裸 `context canceled` 交给模型）；toolview 状态行分别为 `已中断`/`未执行`，已捕获的首尾输出照常保留
- 平台抽象（`shell_platform.go`，无 tag）：`shellPlatform{GOOS, Candidates, ConfigureGroup, KillGroup, ProtectSignals, ExitCode, ProcessStopped, Programs, Capabilities, DecodeOutput}` 一张表承载全部平台差异（候选链、进程/信号/退出码语义、可用程序清单与工具描述的能力句——run_shell 描述随平台切换；Windows 清单同时含 coreutils/Git for Windows/msys2 可能提供的 unix 工具与 Windows 原生程序，仍是探测到才列出，不会暗示不存在的工具链），按可实现目标平台一文件装配——`shell_platform_posix.go`（`linux || darwin`）承载共享实现（候选链、程序清单、能力句、进程组、信号防护、退出码），`shell_platform_linux.go`（`linux`，`/proc` 挂起探测）与 `shell_platform_darwin.go`（`darwin`，`sysctl` 挂起探测）各自装配表与平台差异、`shell_platform_windows.go`（`windows`）、`shell_platform_stub.go`（其余平台，仅保证可编译）；各分片一律经无 tag 文件的 `fillDefaults` 装配，nil 函数字段落到安全默认（no-op 进程组配置、默认杀进程、默认退出码、恒 false 挂起探测、空能力句、恒等 `DecodeOutput`——windows 分片填入代码页兜底转码，见《输出捕获》），`GOOS` 空则填 `runtime.GOOS`（平台名的唯一引用点，分片不再各自声明），漏字段不再等到调用点才崩；`ProtectTerminalSignals`（`sync.Once` 包装）同在无 tag 文件，`TestPlatformComplete` 继续守卫候选链非空与程序清单无空串（这两个字段不做兜底：候选链空即启动报错，程序清单空即无清单）。取代原先按方法散落的 `shell_unix`/`shell_other`/`shell_proc_*`/`shell_candidates_*` 七文件与三套 tag 口径
- 工具描述拼装（`shellTool.toolDesc` → `describeShell(plat, profile, programs)`，与平台无关、可注入任意平台值）：平台名取自 `plat.GOOS`（不再单独传 `runtime.GOOS`，与能力句、程序清单同源同一张表；`TestPlatformComplete` 断言其等于运行时 GOOS）：`在 <GOOS> <name> 中执行命令（<语法提示>），返回 stdout/stderr/退出码。` + cwd 契约句（会话启动目录执行、无需 `cd`、需要其它目录用 `cwd` 参数，2026-09-16 前移至能力句之前） + 平台能力句（`platform.Capabilities`，按平台与 `shellProfile.Kind` 给差异：posix 的文本工具链/`/dev/tty` 交互提示/`128+signum` 退出码；windows 的 PowerShell 对象管道或 cmd 内建/无 `/dev/tty`/unix 工具链以「可用程序」清单为准——coreutils、Git for Windows 或 msys2 装上就有，探测不到就不提）+ 用途句（系统操作首选）+ `可用程序: ...`
- shell 解析（`newShellTool`，Agent 构造时一次性解析并定格）：
  - 优先级：配置覆盖（`config.yaml shell:` / env `TANYA_SHELL`，名字或绝对路径，任意 shell 名允许，未知 basename 按 posix `-c` 处理）> 平台自动探测
  - 自动探测：候选链取自平台抽象 `platform.Candidates`（posix = `bash` → `sh` → `ash`；windows = `pwsh` → `powershell`；stub 平台保底同一 posix 链），遍历算法 `firstAvailable` 平台无关、候选可注入，测试不必依赖当前 GOOS；windows 无 PowerShell 7 时兜底 Windows PowerShell 5.1（不回退 cmd，两者均按 `-NoProfile -NonInteractive -Command` 调用）
  - 全部落空（含配置的 shell 不存在）：解析返回错误（`MsgNoShellFmt`/`MsgShellOverrideFmt`，含候选清单与配置提示），`agent.New` 立即透传，`main.go` 打印后以 1 退出——无降级路径，`shellTool.profile` 在其后恒非 nil，profile 的非空成为不变量（`run_shell` 恒定注册、env 段恒定输出 SHELL/TIMEOUT/OUTPUT 行、system prompt 恒为 `DefaultSystemPrompt`）
- 程序探测：profile 就绪后对 `platform.Programs` 逐个 LookPath（posix：ls/cat/head/tail/grep/rg/fd/sed/awk/find/sort/wc/cut/tr/xargs/git/curl/wget/go/node/python；windows：ls/cat/head/tail/grep/sed/awk/wc/cut/tr/xargs/diff/tee/uniq＋rg/fd/git/curl/wget/tar/ssh/go/node/python＋where/findstr；stub 平台为空；windows 清单刻意不含 `find`/`sort`——System32 同名程序是字符串搜索/代码页排序，语义与 GNU 版不同，探测到会误导模型），存在的拼入 run_shell 工具描述 `可用程序: ...`，仅在工具描述出现，不重复注入 env 段
- 输出捕获：stdout/stderr 各保留头 30000 字节 + 尾 30000 字节（`streamCapture` 滚动窗口），中间字节计数丢弃，模型仍可见首尾内容；`finish()` 时经 `toUTF8` 出仓——字节整体是合法 UTF-8 即原样直通（posix 恒等、零开销），否则路由到 `platform.DecodeOutput` 兜底转码（windows：按 `ctty.FallbackCP()` 给出的快照代码页经 `MultiByteToWideChar → WideCharToMultiByte(CP_UTF8)` 转换，截断缝上的半个多字节字符落 U+FFFD 而非整体失败；背景与策略见 `docs/terminal-caps.md` B4）。middle==0 的头尾连续片段拼接为单缓冲后整体解码，避免多字节序列被 head/tail 边界切断
- 组件化（`docs/shell-tool.md`）：`shellTool` 是 shell 执行层唯一所有者，`profile`/`programs`/`workspace`/`home`/`bridge` 在构造期定格、之后只读，`run` 每调用状态全在栈上（可重入）；唯一可变字段是终端租约 `ttyMu`——真实终端进程内只有一份，桥接与前台移交两条路径都在锁内。组件内不读环境（无 `os.Getwd`/`os.UserHomeDir`/`exec.LookPath`/`runtime.GOOS`），`agent.New` 装配点各读一次注入。包级可变状态（`shellRuntime*`/`shellLookPath`/`ttyBridgeMu`+`ttyBridgeCur`）已删除；`envSection`/`describeShell`/`runShellParams` 为纯函数（组件内不读 GOOS：平台名来自构造期定格的平台表）；工具清单由 `allTools()` 显式组装、经 `toolRegistry.defs()` 在 `NewClient` 构造期注入 client（请求组装不再伸手读包级清单）
- 实测契约（sudo 两模式对照）：`sudo` 默认模式自开 `/dev/tty` 完成提示与密码输入——前台移交后提示实时可见、密码不回显，仅最终错误走 stderr 回流；`sudo -S` 强制从 stdin 读密码时提示改写 stderr（被捕获，等待期间不可见），交互命令应避免 `-S` 类强制 stdin 选项
- 终端前台移交（原语在 `ctty`，见 `docs/ctty.md`；前台组/termios 原语缺失的平台自动跳过对应步骤）：执行前经 `ctty.Open` 打开控制终端，仅当自身进程组已是前台（`ctty.IsForeground`）时 `ctty.SetForeground` 移交子进程组，子进程结束后以 `handed` 门控归还（避免从未交接时抢占 shell 的前台）；无控制终端 / 非前台（嵌套、后台运行）自动跳过，行为与旧版一致。移交前台的同时将 `cmd.Stdin` 接到控制终端（打开成功时），子进程 stdin 直通用户终端，可直接在终端应答 ssh/git/sudo 等密码与确认提示，不再静默挂死至超时；无 tty 时 stdin 保持原状（/dev/null）。windows（B2，2026-09-17）：`ctty.Open` 返回 `CONIN$`，`cmd.Stdin` 同样直通控制台（非交互与交互一致），posix 的 termios 快照/前台交接/屏幕模式复位在 windows 走 stub 跳过，输入模式另由 `ctty.SnapshotInput`/`RestoreInput`（console mode）在回合末复原，^C 归属由 `IgnoreCtrlEvents` 掩蔽解决
- **终端状态保存与复原（2026-09-15 增补，2026-09-16 修订，2026-09-17 跨平台化）**：`runShellForeground` 在移交前用 `ctty.SnapshotInput` 快照控制终端输入模式（posix 转发 `ctty.GetTermios`；windows 转发 `GetConsoleMode`，修复子进程污染被固化，见 `docs/windows-console-mode-restore.md`），在 defer 中 `ctty.RestoreInput` 复原（覆盖正常退出、超时 SIGKILL、Ctrl+C 中断三条路径，故任何被强杀的子进程都不会把 `-opost`/`-icanon`/`-echo` 留在终端上），并在归还前台组后、关 tty 前调用 `ctty.ResetModes` 复位屏幕模式——**仅当 `ctty.IsForeground(fd)`**（自己确实是前台）时发，`SetForeground` 归还失败也不例外。修订缘由：此前两次结论（「常规路径只复原 termios，因为子进程 stdout/stderr 走管道」「ResetModes 只在桥接 release 发且不含 `CSI r`」）基于一个被证伪的前提——非交互路径把真实 tty 作为子进程 stdin，子进程写 `/dev/tty` 的转义照样改屏幕状态（探针 `clean-tty-scrollregion`/`clean-tty-modes`）；而 `DECRST 1049` 在主屏也会按 DECRC 恢复保存槽，使桥接 release 后工具块正文从屏幕顶部开始画、覆盖旧数据（探针 `clean-interactive-release`，用户实测于 interactive 密码提示）。**光标锚点（2026-09-16 二次修订）**：复位串一度把会移光标的 `?1049l`/`CSI r` 包在 `DECSC`…`DECRC` 内，实测证明那只保住了「已被子进程打乱」的位置——子进程写 `DECSTBM` 时终端把光标 home 到绝对 (1,1)（51×75 SSH 终端 DSR 逐点实测，区外下方/区内/区外上方三种起点一致），前导 `\x1b7` 存的正是这个坏位置。现改为**调用方锚点**：`cmd.Start()` 前 `ctty.SaveCursor`、复位后 `ctty.RestoreCursor`（桥接侧 `Prepare` 存、`release` 归位），`resetModes` 内不再含 `DECSC`/`DECRC`（含则覆盖该槽，归位退化为「恢复到陈旧槽」）；两条路径都带前台门控，且没存过锚点就不恢复。快照失败（无 tty / 非 tty fd / 非前台）不阻断执行；已知限制：子进程写 `/dev/tty` 的**内容**（密码提示、半行残文）既不进采集（模型看不到）也无人清洗（探针 `note-partial-line`），子进程自行 `DECSC` 后不 `DECRC` 会夺走保存槽、归位目标由它决定
- 信号防护（2026-09-17 下沉 `ctty`）：入口仍是 main 启动时的 `ProtectTerminalSignals`（`sync.Once` 一次性），实现改为 `ctty.ProtectJobSignals()`——`Notify(SIGTSTP)` 吞没（命令间隙 Ctrl+Z 不挂起自身）+ `Ignore(SIGTTIN/SIGTTOU)`（自身后台 tty 读写不停止），全项目作业控制信号注册仅此一处，`agent` 不再 import `os/signal`。忽略处置随 exec 被子进程继承，子进程后台读写 tty 得 EIO 而非停止——**TTIN/TTOU 必须保持 `Ignore` 而不是 `Notify`**，改成捕获即断掉这条 exec 继承链
- 运行期信号与统一退出（2026-09-17，抽象见 `docs/ctty.md`《运行期信号》）：`ctty` 是信号监听的唯一抽象（平台白名单分片声明清单、`WatchSignals()` 由 main 安装、`Exit/Exiting/ExitSignal/ExitStatus/Interrupted` 导出状态），业务侧不出现 `os/signal`、不区分退出方式。关闭信号 = SIGTERM/SIGHUP，中断信号 = SIGINT，SIGQUIT 仍保持 Go 默认（全栈转储）；关闭信号 → `Exit(sig)`（首个来源生效并记录信号号）+ 广播中断，中断信号 → 只广播。重复关闭信号 = 强退（`emergencyRestore` 复原终端后 `os.Exit(128+signum)`）：`Notify` 之后普通信号不再具备默认处置，重复信号既是用户唯一逃生门，也是非 raw 阻塞路径（管道 stdin 的 `Degraded`，不改 termios、无残留代价）的唯一出口。唤醒靠 `readline` 轮询 `ctty.Exiting()`——raw 下 `VMIN=0/VTIME=1` 每 ~100ms 一轮醒来，`keySource.readKey` 每次调用与每轮读空闲都检查，命中即返回 `readline.ErrExited`，并优先于已解析待发的按键队列
- 进程终止语义（`platform.KillGroup`，超时/中断/挂起三条路径共用）：posix 经 `Setpgid` 建独立进程组、`kill(-pid, SIGKILL)` 杀整组（`ESRCH` 归一为 `os.ErrProcessDone`）；windows 走 `taskkill /T /F /PID` 杀整棵进程树（PowerShell/cmd 派生的子进程一并终止，超时不再残留孤儿），`taskkill` 不可用或失败时回退 `Process.Kill()`（仅直接子进程）
- 挂起探测（`waitShell`）：200ms 轮询进程状态，连续 2 次判定为停止态即认定被终端挂起（Ctrl+Z 等停止信号），SIGKILL 进程组并置 `Stopped`，状态行显示 `挂起已终止`，避免静默挂到超时；探测走平台抽象 `platform.ProcessStopped`——linux 读 `/proc/<pid>/stat` 判 `T`，darwin 走 `sysctl(kern.proc.pid)` 取 `kinfo_proc` 的 `p_stat` 判 `SSTOP`（经 `x/sys/unix` 的 `SysctlKinfoProc`；该状态常量标准库与 `x/sys` 均未导出，本地定义 `darwinStatusStopped`），两者在读取失败或进程不存在时都返回 false（保守，不误杀），并各有真实 `SIGSTOP`/`SIGCONT` 双方向用例（`TestLinuxProcessStopped`/`TestDarwinProcessStopped`，macOS 上 `kern.proc.pid` 不可读时 skip）；windows 与 stub 由 `fillDefaults` 兜底恒 false（探测失效，其余功能不受影响）
- 字段集：`ShellResult` 为 Command/Stdout/Stderr chunks/Err/ExitCode/TimedOut/Interrupted/Stopped/Duration
- 事件：`EventToolStart`（dispatch 前触发）/ `EventToolEnd`（结构化 `ToolResult`：Shell/Text 二选一，发回模型的 content 由 `Content()` 拼回文本），渲染在 repl 包 `toolview.go`
- **免确认直接执行**（早期版本有 y/n/a 确认机制，已移除）

### 工具视图渲染（repl/toolview.go）

- `NewToolView` 构造渲染器（`repl/repl.go` 与 `main.go` 各接一处；`Handle(e agent.Event)` 即事件入口，`toolView` 自身即 `agent.EventSink`），块状视图：标题行 + 缩进输出行（stderr 加 `2|` 前缀）+ 亮蓝状态行
- **标题区两形态（2026-09-16 命令可读性改造）**：`run_shell` 的 `command` 短到能与工具名同行时**内联单行**（`▸ run_shell ls -la`，与旧版逐字节一致）；放不下（超宽）或原本多行时转**块形态**——首行只有工具名（`▸ run_shell`），其后是显式 `cwd` 行（`  cwd: <原样值>`，不缩写；未指定则无此行）与折行的命令区（每行 `  $ ` 前缀，与输出区的 `  ` / `  2| ` 缩进区分）。内联判据是「无 cwd && 命令无 `\n` && 宽度 ≤ width−3−len(name)−1」，两种形态的首行宽度都不超终端列数。非 `run_shell` 工具、JSON 解析失败或 `command` 为空一律只显示工具名（`toolArgsDisplay` 返回空）
- **命令区折行（`commandLines` + `render/term.Wrap`）**：保留命令原有的换行结构与行首缩进（不再用 `; ` 压成单行——旧实现会把 `for …; do` / `if …; then` 拼成语法上不存在的 `do; if`，且丢空行与缩进）；制表符先按 4 空格摊平（`runeWidth` 把 `\t` 当单列、与终端制表位不符，不摊平则折行位置与显示不符）；`Wrap` 按显示宽度切分（宽字符整字换行、遇 `\n` 硬断行、`\r` 丢弃），只切分不改写内容、**不做词级折行**（超长 token 一样硬切），每行宽度 ≤ 终端列数 − 4。行数上限 `toolCommandMaxLines = 8`：超出保留头 6 行 + 省略行 + 尾 1 行，省略行 `… 省略 N 行（完整命令见 /history）`——命令是有序脚本，省略中段比省略尾部更不易误读收尾的 `done`/`EOF`；完整参数始终在 `/history n` 的工具消息里（显示侧改造不影响模型通道与会话存储）
- 命令文本里的 ANSI 由调用点的 `Dim.Frame` 清洗（`Wrap` 遇到序列原样保留、不计宽度），折行发生在清洗之前，故宽度计算不会被模型可控的转义序列干扰
- 状态行总是输出（语义色 `Info`，无色环境纯文本）：`↳ exit 0 · 0.3s · 12 行`；异常时首段为 `exit 2`/`执行超时`/`已中断`/`挂起已终止`/`错误: ...`；输出被截断时行数段显示 `共 N 行`；builtin 工具无状态行（截断时仅显示 `共 N 行`）
- 颜色走 `theme.Semantics` 语义色 + `term.Profile` 驱动（`colors` 配置 / `NO_COLOR` / stdout 非终端 → 纯文本）：工具块 `Dim`、状态行心跳 等待 `Warn`/思考 `Think`/执行 `Run`、状态行 `Info`，可用 `palette` 配置覆盖
- **捕获输出的 ANSI 治理**（`render/term` 清洗 + `Renderer.Frame/Passthrough`）：块组装内聚于 `RenderToolStart`/`RenderToolEndAppend`，输出区无 SGR 时整块 `Dim.Frame`（全清洗 + 块级包裹，标题/输出单一包裹点）；检测到 SGR（`HasSGR`）时输出区改走直显——`Passthrough` 保色渲染（SGR 原样保留、布局序列/OSC/C0 仍清洗、脏状态结尾闭合），标题行独立 Frame，状态行 `Info` 显式后置（不依赖 SGR 时序巧合）。预览类彩色输出（如欢迎屏效果）在灰色块内原色可见，用户与模型双通道分离：**模型侧文本不做任何变换**（原始输出、信息保真、缓存与历史零影响），显示侧机制对模型完全不可见
- `/history` 查看 tool 消息时正文走 `Dim.Frame`（纯显示侧，历史存储不动），顺带解决历史串裸序列漏进视图的问题
- 显示行数上限 `tool_output_lines`（默认 20，范围 1-1000），超出保留头 3 行 + 尾 2 行并提示 `/history n` 查看完整输出
- 执行开始即打印标题行（调用点 `Dim.Frame` 包裹，模型可控的 args 一并清洗），**无进行中标记**；结束一律 `RenderToolEndAppend` 追加正文块与状态行——标题只由 ToolStart 打出一次，重复标题会留下两行 `▸ 工具名`。2026-09-15 追加化：删掉 TTY + 光标控制档的 `\x1b[1A\r\x1b[K` 上移重绘（`RenderToolEndInline`）与交互式的标题重打（`RenderToolEnd`），三条收尾路径合一；交互式仅保留前导 `\n`（用户交互回显混在中间需要重新起头）
- 交互模式（`Event.Interactive`）：不发状态行心跳（周期写入会擦掉子进程写往 tty 的提示），标题行下打印引导行 `⏎ 等待终端输入，请在下方直接应答`，结束为前导 `\n` + `RenderToolEndAppend`；桥接期间真实 tty 归 bridge 独占（repl 侧不写入：标题行在切 raw 前打印，结果块在 `stop()` 恢复 termios 后渲染）
- 流式输出行尾无 `\n` 时（`toolView.dirty` 跟踪），状态行打印前自动补换行
- 输出收敛（`output`/`streams` 双流）、`Kind` 门禁与输出模式（rich/plain）、回合封装（`turn`）的改造规划见 `docs/repl-output-refactor.md`
- 状态展示追加化（替代 spinner）的方案、取舍与实测见 `docs/repl-status-append.md`

### 状态行与心跳（repl/status.go，2026-09-15 替代 spinner，2026-09-16 行内点累加 + 思考相位回补）

- 定位：**只追加、不重绘**——没有光标控制、没有清行、没有上移。旧 spinner 与 inline 工具块重绘依赖"光标停在自己写的那一行"：一旦终端被让出（`run_shell` 移交前台组，子进程可直接写 `/dev/tty`）或被第三方写，就会擦掉对方输出或重绘错位（实测 sudo 密码提示被帧清行抹掉）。追加式对交错免疫：顺序变化可接受，绝不覆盖。
- `EventRequestStart` 起等待心跳并立刻打行首 `» 等待响应 0s`（`KindStatus`，语义色 `Warn`）；首个 content / `EventResponse` / 工具开始即停。思考相位由首个 `EventReasoning`（思维链 delta）经 `heartbeat.setPhase` 切到 `» 思考中`（语义色 `Think`）——与工具相位同一机制：**收尾当前行 + 用新前缀开新行**（一次写完，不重绘），秒数沿用同一 `started`（与旧 spinner 的累计口径一致）、点数归零；同相位重复事件（逐 token 到达）与心跳未在跑时（`statusOn` 为假、或 content 已开始后的零星 reasoning）都是 no-op，故每个请求最多多一行。工具执行期（非 `interactive`）起第二条心跳，`EventToolStart` 后立刻打 `» 执行中 0s`（`Run` 色）。
- 心跳形态（`statusTickInterval = 1s`、`statusLineSpan = 10`）：行首**只写一次**带秒数的前缀（`statusSeconds`：`59s` / `1m10s` / `1h01m`；秒数取 `time.Since(started)` 的**真实经过时间**，开行时写一次后固定不变；前缀末尾以 `statusDotGap` 一个空格位收尾，点不与秒数粘连），行内每 tick **只追加一个点** `.`，满 `span` 个点即换行并以当时秒数开新行（`» 等待响应 0s ..........` → `» 等待响应 10s ..........` → …）。已写出的字节永不回头修改。行首前缀与每个点都由同一语义色 `Style.Sprint` 单独包裹（各自带 `term.Reset`，点写完终端立即回到无 SGR 状态）——点与文字同色，且不把终端留在着色态：子进程或第三方写 `/dev/tty` 不会继承 tanya 的颜色。代价是每 tick 一段 `SGR + . + reset`（约 10 字节/秒，可忽略）；行首与点之间保留 `statusDotGap` 一个空格位，点不与秒数粘连。
- `stop()`：`close(stopCh)` → 等 loop 退出（沿用有界 200ms）→ 当前行未收尾时补**一个换行**；未起心跳时不写任何字节（幂等，可重复调用）。重复 `start()` 先收尾上一行再开新行。
- 回合收口：`turn.End` 调 `toolView.Stop()`——等待期被打断（无 content、无工具事件）时没有别的停止点，否则心跳会一直写到下一次请求、糊掉在途的 readline 提示符。
- 门禁：`KindStatus` 仅 rich 档可见——plain / plain+verbose / stdout 非终端均无状态行与心跳，`ask` 单发默认档行为不变。
- 每轮请求完成打印状态行 `  ↳ TTFT 0.8s · 3.2s · prompt 12.3k · completion 1.2k · 缓存 81.67%`（字段缺失自动省略；无 usage 时显示本地估算上下文）。
- 方案、取舍与实测见 `docs/repl-status-append.md`（其中 09-15《取舍》对"思考中"的删除已被 09-16 的思考相位回补取代）。
- **思维链显示（`show_reasoning` / `/reasoning`，2026-09-17）**：开关打开且 `KindReasoning` 过门禁（TTY + rich 档）时 `EventReasoning` 不再切思考相位——首个 delta 先 `view.Stop()` 收尾 `» 等待响应` 行，再打印上分隔 `─── 思考 ───`（`Think` 色，前后各三条横线），delta 走与正文同一 markdown 管线（`turn.reasonBuf` → `Renderer.Block` 逐块上屏）；`EventContent` / 工具起止 / `EventResponse` / `turn.End` 调 `flushReason` 结算残留块并补下分隔 `─── 思考结束 · 3.2s ───`（时长同 `turnDuration`），正文之后再现 reasoning 则重开一段。开关关闭或门禁外走原相位路径，plain / `-p --verbose` / `ask` / 非终端一律不显示（`KindReasoning` 不在其可见集）。思维链与正文共用 markdown 缓冲的 hold 看门狗（`fenceLineLimit` 2000 行 / `fenceByteLimit` 256 KB / `pendingByteLimit` 64 KB）：畸形输入（漏闭合围栏、超长单行）与超阈值的长代码块/长段落都就地降级为 `CodeBlock` / `Paragraph`（文本不丢、只丢代码块归属与格式），影响止于局部、后续 delta 立即恢复流式解析，见 `docs/render-pipeline.md` §10

### builtin（builtin.go）

免确认轻量工具，`builtinTools()` 表驱动返回 `[]Tool`（`builtinTool` 结构体携带 name/desc/params/run，描述与参数随实现同处）：

- `get_time`：当前时间（含时区）
- `get_env`：查询环境变量，名称含 KEY/TOKEN/SECRET/PASS 的拒绝返回
- `calc`：四则运算表达式求值（自实现递归下降解析，支持 `+ - * / %`、括号、负数）

### agent 自调（control.go）

`agent_custom` 是唯一带状态的工具，形态为**键值化三参数**：`action`（`get`/`set`）+ `key`（能力名，enum）+ `value`（仅 `set` 且 key 可写时使用）。顶层参数形态恒定，能力面由 `key` 展开；实现走 key 表（`keySpec{writable, read, write}`）驱动，新增能力 = 表加一行 + `key` enum 加一值：

- 可写 key：`model`（非空字符串）、`reasoning_effort`（minimal/low/medium/high/max/off）——`set` 校验失败不改动状态，对下一次请求生效
- 只读 key：`models`（服务端可用模型列表，超 50 项截断并标注总数）、`usage`（最近一次请求的上下文 tokens、缓存命中、命中率）、`stat`（会话 id、消息数、累计 token；`--no-save` 时会话显示 `(不落盘)`）、`sessions`（本工作区会话列表 + 每个会话 `.jsonl` 的绝对路径，id 倒序列前 20）、`config_path`（生效配置文件绝对路径 + 改动需重启生效的提示——`Config.Path` 由 `LoadConfig` 记录，`-c` 优先、`~` 展开并绝对化；只回路径不回内容，读文件由模型自理，改自身配置走 `run_shell`）
- 只读 key 出现在 `set` 里**明确报错**（不静默忽略）；未知 `action`/`key`、缺 `value`、值非法均返回带可修建议的错误文本 (`MsgErrPrefix` 前缀)

`get sessions` 只给列表与文件位置，**不读内容**——读内容交回 `run_shell`（符合「新能力优先用 shell 命令组合实现」）。依赖经窄接口 `configTarget`（`*Agent` 满足，测试可注入替身）注入，与 `shellTool` 的构造期注入同一风格。改动只写内存 `Config`：不落盘、不入会话文件，`/load` 或重启后回落配置文件值；`repl` 的 `{model}`/`{effort}` 占位符每轮现读 `Agent`，自动跟上，无需事件通知。`SetModel` 带空值校验（`/model` 命令共用同一路径）。形态选型与偏差记录见 `docs/agent-control-tool.md`。

## 上下文管理

- 本地不设上限、不做裁剪：超出模型上下文时由 API 返回错误，直接暴露给用户（可 `/new` 开新会话）
- 用量统计：捕获响应 usage 作为真实值；对端不返回时降级为本地粗估（CJK 1 token/字，ASCII 0.3/字符）
- tool 结果在写入 history 前已被 shell 层截断（单项头尾各 30000 字节），避免极端膨胀

## 会话

- 每次启动/`/new` 开启新会话，id 为启动时间戳（`20060102-150405`）：`time.Now` 就地取、不做时钟注入（与 shell 执行层同一口径；文件名可用正则断言）
- workspace 目录是会话数据落点，其下固定为并列的 `sessions/`（活动会话）与 `archive/`（归档卷）：
  - `local`：workspace 目录 = `<启动目录>/.tanya/`（.tanya 本身即项目隔离，不叠加 workspace-id）
  - `global`：workspace 目录 = `<data_dir>/workspaces/<workspace-id>/`（`data_dir` 默认 `~/.local/share/tanya`），workspace-id 由启动目录派生（可读路径转义 + 8 位短哈希）
- 存储模式（CLI `-m` > env `TANYA_SESSION_MODE` > 配置 `session_mode`，默认 auto）：`auto` 为「当前目录存在 `.tanya/` → local，否则 global」；两侧落点由 `resolveWorkspaceDirs(cfg, cwd) (sessions, archive)` 单点推导，`agent.New` 只创建 `sessions`（`archive` 由归档动作按需创建）
- 旧布局不兼容（2026-09-17）：`global_session` 配置项与 `<root>/<workspace-id>/` 目录形态已废弃，代码不含兼容读取/迁移路径，旧目录由用户自行删除
- 只读会话（`-n` / `--no-save`）：由 CLI 经 `agent.New(cfg, agent.NoSave(true))` 传入，`Config` 无对应字段，配置文件与 env 均无法开启；`ask` 单发与 REPL 通用。读路径全部保留（启动预扫描、`ListSessions`、`LoadSession` 照常，既有会话不会被截断或改写），写路径在 `sessionStore.append` 首行（`disabled`）返回 nil 被整体关闭（覆盖成功/中断/错误三条路径）；`MkdirAll(store.dir)` 在只读模式下跳过，目录缺失时 `store.refresh` 按空列表处理不报错。内存 history 照常维护（中断保留语义不变），进程退出即丢。REPL 启动时在欢迎屏下方以语义色 `Warn` 打一行 `MsgNoSaveWarn`（`REPL.noSaveWarn`，agent 为 nil 或可写时不输出），`ask` 静默；归档只读态（`frozen`）是另一维度的只读，见《会话归档与 fork》
- 落盘：`<timestamp>.jsonl`，每轮结束追加写入新消息（一行一条 Message JSON），记录完整历史（回放/审计用）；回合因中断/错误保留产出时同样落盘（含终止提示行）
- 落盘原子性：本批消息先编码进内存缓冲再单次追加写入，写入报错或短写时 `Truncate` 回滚到写入前大小，`saved` 游标与文件内容始终一致（重试不会产生重复行/半行）
- 首行持久化 system prompt 快照（`systemSaved` 标志防重复），`/load` 还原后前缀与当初逐字节一致；旧格式文件（无 system 首行）回退为载入时快照当前 AGENTS.md，且保持不补写；system 行不计入会话条数与摘要
- 会话列表扫描：`agent.New` 启动时预扫描填充缓存（只读模式不建目录，目录缺失按空处理）；`ListSessions` 按 (mtime,size) 增量刷新，仅重扫变化的文件与归档卷；每文件 `bufio` 逐行计数条数、仅解码至首条 user 消息取摘要；归档卷只读中央目录取条目与描述（见《会话归档与 fork》）
- 会话浏览经 `/load` 无参菜单（`repl/picker.go`，stdin 非终端降级为序号输入），候选带 id、时间、条数、首条 user 摘要；`/load <id>` 直接恢复继续对话（归档条目在摘要列前标注 `[归档] `，载入为只读，见《会话归档与 fork》）；id 校验拒绝路径穿越（`/sessions` 命令已移除，浏览职责由 picker 承接）

## 会话归档与 fork

### 归档卷

- 归档把活动会话打包为标准 zip 卷，落 `<workspace>/archive/archive-<20060102-150405>.zip`；一次 `/archive` 生成一卷、**写入后不可变**（不重写、不删条目、不做「解档回活动区」），继续对话由 `/fork` 承担
- 卷内 entry 名 `<会话 id>.jsonl`，`Method: Deflate`，`Modified` 取原文件 mtime，**entry 数据为原 jsonl 逐字节**（不裁剪、不重排、不丢 reasoning/tool_calls）：prompt cache 红线在归档路径上的延续
- entry comment（zip per-entry comment，单行 JSON）：`{"v":1,"msgs":<条数>,"summary":"<首条 user 消息，单行化、≤200 rune>"}`，超长时缩短 summary 并置 `"trunc":true`；**硬上限 4 KiB**——Go 在 comment > 65535 字节时静默写坏中央目录（实测 65536 读回 0 字节），故 marshal 后校验、超限降级
- 卷级 comment（`zip.Writer.SetComment`）：`{"v":1,"workspace":"<启动目录>","created":"<RFC3339>","sessions":<条数>}`
- 归档筛选（`agent.ArchiveOptions`）：`OlderThan`（按文件 mtime，0 = 不限）与 `Keep`（保留最新 N 个活动会话）取交集，`Exclude` 恒为当前会话，另加**空闲保护**（mtime 距今 < 5 分钟的文件跳过，防另一实例正在追加）；id 已存在于任一卷则跳过（幂等）
- 失败语义：「元数据扫描」失败 → 跳过该文件并计入报告；「卷写入」失败 → 放弃整卷（删临时文件）且不删任何源文件并返回错误；卷先写同目录临时文件再 `rename` 落定，**之后**才删除源文件（崩溃最多留双份，列表侧按同 id 取活动去重）
- 写入前 `MkdirAll(archiveDir)`；0 候选时不建目录、不产生空卷
- 卷是标准 zip，`unzip -l/-p/-z` 可直接浏览与提取（`unzip` 打印注释时按本地码页转码，可能显示乱码，不影响数据与 tanya 自身读取）
- 不做：内容裁剪/摘要压缩、卷重写与还原落盘、自动归档/TTL 淘汰、索引文件或清单 entry、多会话打包以外的容器格式；仅用标准库 `archive/zip`，不引新依赖

### 列表与索引

- `sessionStore.refresh()` 同时扫 `sessions/*.jsonl` 与 `archive/*.zip`（忽略 `*.tmp-*`）；归档侧 `zip.OpenReader` **只读中央目录**，条目元数据取自 header（id、未压缩大小、`Modified`）与 comment（条数、摘要），不解压任何数据（实测 156 条含注释 0.1 ms）
- 缓存粒度由「文件」扩为「文件 + 卷」，仍以 (mtime,size) 判失效；卷不可变故每卷每进程最多解析一次
- `SessionInfo` 增加 `Archived bool`、`Size int64`（归档项为 entry 未压缩字节、活动项为文件字节）、`MetaOK bool`（comment 缺失/非法时仍列出条目，`Msgs=0`、摘要为空，picker 行按 `SessRow` 既有格式显示 `0条`）；归档项 `Path` 指所属卷路径，摘要按活动区同口径截断到 30 rune 展示（卷内 comment 仍存 ≤200 rune 原文）
- `ListSessions` 排序：活动组在前、归档组在后，组内 id 降序；同 id 同时存在于两区时只列活动项

### 只读载入与 fork

- `/load <归档 id>`：定位所属卷 → 解压该 entry 的流直接交给 `json.Decoder`（抽出 `loadFrom(io.Reader)`），**不落盘、不改卷**；zip reader 的 CRC32 校验兜底损坏（错误上抛、卷保持原样）
- 归档只读态以 `sessionStore.frozen` 表示（与 `-n` 的 `disabled` 正交，归档 id 另存 `frozenID`）：`append` 直接返回、`path()`/`id()` 为空、`Agent.SessionFile()` 为空；`Agent.ArchiveReadOnly() (id, ok)` 与 `Stats.Archived` 供 UI 显示（`/stat` 打 `会话文件: 归档只读 X（未写入）`，`agent_custom` 的 `stat` 打 `会话: 归档只读 X（未写入）`）
- 只读态**拒绝对话**：`agent.Ask` 首行返回 `ErrArchiveReadOnly`（`ask` 单发路径同样受保护），REPL 分发层在对话分支前拦截并提示 `/fork`（`:`/`：` 显式前缀同样拦截）；元命令（`/history`、`/stat`、`/model`、`/new`、`/load`、`/fork`、`/exit`）照常可用
- 与 `-n` 的差异：`-n` 允许内存内继续对话（不落盘、进程退出即丢），归档只读态不允许——归档原文件已冻结，继续对话会产生「没有落点的历史」
- `/fork`（仅归档只读态）：`store.rotate()` 取新 id → 清 `frozen` → `prompt.reset()` 重读 AGENTS.md（**当前**快照，fork 即新会话）→ history 原样保留、`stats.reset()` → 立即 `save()` 落一次盘，使新会话文件立刻出现在 `/load` 列表，此后按普通会话增量 append；`-n` 下允许 fork 但不落盘（提示未写入）
- 载入与 fork 文案区分：`已载入会话 X（归档只读，继续对话请 /fork）`、`已 fork 为新会话 <新 id>`

## 系统提示与缓存友好

- 组装规则：`DefaultSystemPrompt`（内置，固定不可配，`system_prompt` 配置项已移除）+ 全局 `~/.config/tanya/AGENTS.md`（存在时）+ 工作区 `./AGENTS.md`（存在时），各段以 `# 全局说明`/`# 项目说明` 标题分隔，文件缺失/空白跳过
- 规则与事实分离：persistPrompt（上述规则）在 `/new`/`/load` 时组装并冻结进会话首行；每次请求的 system = persistPrompt + 空行 + `Agent.env`（环境事实在 `agent.New` 构造期算一次、冻结进内存，既不持久化也不再重算）
- 快照机制：`/new` 与 `/load` 时刻读取 AGENTS.md 组装快照；会话进行中零文件 IO，快照冻结；旧格式会话（system 首行含历史环境段）原样保留并标记，`/load` 时提示 `/new`
- 缓存收益：history 全程 append-only，system 两段（规则快照 + 环境段）在本进程内逐字节恒定，同一会话内请求前缀不变，prompt cache 逐轮全量命中；`/new` 时 AGENTS.md 未变则 system 前缀跨会话命中。env 段自 2026-09-14 起在构造期定格（此前的 `WORKSPACE` 行是 system 内唯一会自行变化的输入，已随本次收口删除）
- 缓存命中捕获（DeepSeek `prompt_cache_hit_tokens` / OpenAI `prompt_tokens_details.cached_tokens`）经 `Agent.Stats()` 的累计字段供提示符占位符显示
- 缓存机制的实测结论（64-token 块粒度、tools 段在序列化尾部的代价台阶、各后端写入延迟差异）见 `docs/cache-probe.md`

## REPL

用户可见文案统一为常量：`repl/messages.go`（UI/命令输出/选择器/工具视图/状态行）与 `agent/messages.go`（错误/ToolResult 文本），调用一律引用常量（经 `streams` 写出，见下），换行由调用处的格式串控制；退出文案曾统一为 `MsgBye`（「再见」），2026-09-16 起由收尾块取代并删除该常量，见《退出收尾》。工具描述与系统提示不在此列（模型侧文案，翻译需评估 prompt 影响）。

欢迎屏由 `welcomLogo` + `welcomeText()` 组装：logo ASCII 图 + 一行 `输入 /help 查看命令   tanya <版本>（构建于 <时间>）`；`repl.Version`/`repl.BuildTime` 由 `main` 注入（`make build` 经 ldflags 写 `main.version`（git describe）与 `main.buildTime`（date），直接 `go build` 为 `dev`/空，空时不渲染构建时间）。`-v` 与欢迎屏共用同一 version 源。

### 输出流与 Kind

输出收敛到 `repl/streams.go`：`streams{out, err}` 是两条独立互斥流，`output` = writer + `sync.Mutex` + 可见集（`visSet`，按 `Kind` 门禁，当前恒为全开，rich/plain 三档在阶段 4 引入）+ 测试钩子 `guard`。`output.Write` 是无门禁通道（raw 期自绘：Editor 提示符/回显、picker），`emit`/`atomic` 是带门禁与 `guard` 的常规通道；`atomic` 回调内只允许写参数 `w`（自锁约束）。每次写入都携带 `Kind`（`repl/flow.go`：Content/Reasoning/ToolBlock/ToolStatus/Notice/Decor/Error/Status）。

流分配：**stdout** 承载 assistant 正文、工具块与状态行、命令反馈、回放、欢迎屏、回合分隔线、状态行心跳与输入期回显；**stderr** 承载错误与诊断——`MsgErrLineFmt` 类、`MsgThemeBad`、`MsgInvalidIndex`、`MsgModelsFail`、`MsgInterruptKept`/`MsgInterruptBare`，以及 `main` 的启动/配置/agent 构造错误（`streams.Fail`）。两 fd 均无缓冲，同一 tty 下写序即调用序，故交互观感与收敛前逐字节一致（阶段 1 以 pty 对比前一提交的二进制验证）；stdout 被重定向时错误与诊断分流到终端，stdout 保持可解析。

`Kind` 不导出包外、不进 `agent.Event`；`main` 侧只用语义化出口 `streams.Print`/`Content`/`End`/`Fail`。

输出模式三档由 `outMode` 决定（`repl.ParseMode(plain, verbose)`，仅 CLI `-p`/`--plain` 与 `--verbose` 可设，env 与 config 不参与；`ask` 单发分支再套 `repl.SingleShot` 把 rich 降到 plain+verbose，显式 `-p` 更窄时保持不动）：**rich**（默认，全开）、**plain**（`out.vis` = Content/Notice，其余屏蔽；stderr 不参与屏蔽）、**plain+verbose**（再加 ToolBlock/ToolStatus）。plain 的六条语义：① `Colors=LevelNone`（main 在 profile 计算后强制）；② 不发状态行与心跳（`toolView.statusOn()` 同时查 TTY 与可见集，是查询不是快照，`KindStatus` 仅 rich 放行）；③ 无光标控制（工具块一律 `RenderToolEndAppend`：不重复标题，只补正文块与状态行）；④ 关 markdown（`flow.mdEnabled` 并入 `st.decor()`）；⑤ 屏蔽 Decor（含首行空行与回合分隔线）与工具类；⑥ stdout 只留正文与命令反馈，错误与诊断走 stderr。工具块被屏蔽时**不得**置 `justEnded`，否则下一条正文前会留下孤立空行（`toolView.Handle` 的 ToolEnd 分支按 `allows(KindToolBlock)` 决定是否置位）。`streams.End()` 负责收尾换行：rich 沿用无条件补换行（零行为变更），plain 只在缺少行尾换行时补，保证 stdout 严格等于答案。

### 退出收尾（repl/farewell.go，2026-09-16）

退出收敛到唯一入口 `REPL.quit()`（`farewell()` + 返回 nil）：`Run` 的 `io.EOF` 分支（Ctrl-D 与终端挂断）、`readline.ErrExited` 分支（外部关闭信号唤醒，2026-09-17 与此前三条并轨）、`isExitLine` 分支（`exit`/`quit`）与 `handleCommand` 返回真之后的 `/exit`/`/quit`——来源不同、路径与表现一致（同一 farewell、同一条返回）。`handleCommand` 自身不再写字节、只返回退出信号（此前命令面自打文案，与 EOF 面重复一份），`Run` 拿到信号后统一收尾。进程退出码由 `main` 取 `ctty.ExitStatus()`（关闭信号触发为 `128+signum`，其余为 0）；`ask` 单发同源：取消源统一为 `repl.InterruptContext()`，内部订阅 `ctty.Interrupted()`（不再自持 `signal.Notify(os.Interrupt)` + `signal.Stop`），故 `^C` 与 SIGTERM/SIGHUP 取消在跑请求走同一条 `context` 取消路径（`run_shell` 的杀组 + termios 复原由既有 ctx 语义承担，agent 侧零改动）。

输出为单个 `KindDecor` 块、三行，文本由纯函数 `farewellText(farewellInfo)` 生成（`farewellInfo` 是退出瞬间的快照：会话 id、时长、`Agent.Stats()`、落盘文件、只读标志）：

- **会话行**：`会话 <id> · 时长 <dur> · 消息 <n> 条`；id 取 `Agent.SessionID()`（会话文件名去 `.jsonl`），只读模式无 id 时该段省略，退化为 `时长 … · 消息 … 条`
- **用量行**：`用量 <总量>（prompt … / completion …）`，有累计缓存数据时追加 `· 缓存 <命中率>`——分别复用 `totalsText`/`cacheRateTotalText`（与 `/stat`、提示符占位符同源同公式）；无 usage 显示 `无（未收到 API usage）`
- **文件行**：`会话文件 <homePath>`（只做 home → `~` 前缀替换、**不缩写中间目录**，路径需可直接拿去 `/load` 或查看文件；提示符用的 `shortPath` 会缩写中间目录，不适用于此），仅当 `Agent.SessionFile()` 非空（可写且文件确实存在）时打印；只读模式（`-n`）与归档只读态打印 `会话文件 未写入（不落盘模式）`；本次未产生对话（`sessionStore.append` 在 history 为空时直接返回、不建文件）则整行省略

口径与门禁：时长在 `Run` 入口由 `REPL.started` 置位、退出时 `time.Since`（含提示符前的 idle 时间，`/new`/`/load` 不重置），格式化复用 `turnDuration`；`KindDecor` 仅 rich 可见，故 `-p` 与 `-p --verbose` 下退出**完全静默**（此前 plain 面会打「再见」；`MsgBye` 随本次改版删除）；`ask` 单发不经过 `Run`，stdout 契约不变。agent 侧新增只读访问器 `SessionID()`/`SessionFile()`，`store.disabled` 或归档只读态时均返回空串（后者另做 `os.Stat` 存在性判定）；`SessionFile` 与 `/stat` 的「会话文件」（`Stats().Session`，是**预定落点**、只读模式下也非空）刻意不同口径——`/stat` 报落点，退出报已落盘实体。

### 输入分发

输入按前缀分发（`repl/dispatch.go`），判定顺序固定：`exit`/`quit`（首 token 命中即内建退出，等价 `/exit`）→ 已知斜杠命令（`slashCommands` 白名单匹配首 token，故 `/load x` 命中、`/usr/bin/ls` 不命中）→ `:` 或全角 `：` 开头（剥离前缀与空白作为提问，空内容提示 `MsgDialogueEmpty` 不算回合、不打印回合分隔线）→ 其余整行直接作为提问与 AI 对话，与 `:` 前缀写法等价。白名单未命中的 `/` 开头输入（如 `/usr/bin/ls`）不作命令处理，直接作为对话内容。

### 直通 shell 执行面（归档）

原「其余输入在本目录直通执行 shell 命令」执行面已归档（2026-09）：体感作用有限——agent 侧已有 `run_shell` 工具，直通面与之重复且绕过上下文/契约。末态完整实现见 commit 60bc02e：执行四件套 `runShellLine`/`suppressInterrupt`/`waitShellCmd`/`reportShellExit`、cd 拦截切面 `repl/localcmd.go`（`tryLocalCommand` 单入口 + `dirChangeCommands` 表）、常量 `MsgShellExitCode`/`MsgShellSuspended`/`MsgKillFailFmt`/`MsgCdBlocked`、`agent.NewShellCmd`/`ProcessStopped` 导出包装。恢复步骤：自 60bc02e 取回上述文件与常量 → `Run()` 末分支 `r.ask(line)` 改回 `r.runShellLine(line)`。归档后进程 cwd 恒为启动目录（全程不 `os.Chdir`）；`run_shell` 默认即在此执行，另可用 `cwd` 参数为单次命令指定目录（设 `cmd.Dir`，进程 cwd 不变）。

### 斜杠命令

`/help` `/new` `/load` `/archive` `/fork` `/stat` `/history` `/model` `/think` `/reasoning` `/theme` `/exit`（`/quit` 等价）：

白名单（`slashCommands`，同时驱动 Tab 补全）即分发契约：`Run` 先用 `isSlashCommand` 过滤，未命中的 `/` 开头输入按对话内容处理，因此 `handleCommand` 的 switch 不再有 `default` 分支（原先的 `MsgUnknownCmd` 不可达，已删）。白名单与 case 必须一一对应，`TestSlashCommandsAllHandled` 覆盖该不变量（`/load` 走 stdin 交互路径，单独测试）。

- `/archive [all|<dur>]` 把历史会话打包成归档卷：无参 = 归档 30 天前的会话，`<dur>` 形如 `7d`/`12h`，`all` 不限；恒排除当前会话；只做无损压缩，之后可用 `/load` 只读载入（见《会话归档与 fork》）
- `/fork` 仅归档只读态可用：把当前归档会话的 history 作为新会话起点并立即落盘（新 id、当前 system 快照、继承历史）

- `/history` 无参截断列表（`term.OneLine` 先剥离 ANSI 转义与控制字符、压成单行，再按 120 rune 截断，避免 `\r`/`\x1b[K` 覆盖已打印行与未闭合 SGR 泄漏）、`/history n` 全量查看单条、`/history all` 全量显示；全量显示时消息头 `#N 角色` 按一级标题渲染、并按角色着色（user 用 `Ok` 绿、其余用 `Warn` 黄；`#` 与序号连写不构成 markdown 标题，单独构造 Heading IR），assistant 正文走与对话一致的 Markdown 渲染（受 stdout 是否终端与输出模式约束：stdout 非终端、plain 一并旁路），user/tool 消息与工具参数原样
- `/stat` 显示会话统计：工作区（构造期定格的启动目录）、会话文件、消息条数、本次运行累计 token（prompt/completion）、当前上下文占用（最近一次实报 prompt tokens，无 usage 回落本地估算）、缓存命中量与命中率（累计 hit / 累计 prompt）；数据全部来自 `Agent.Stats()` 单一快照，渲染在 `repl/stats.go`，与提示符占位符同源同公式
- `/model` 无参实时调接口列出可用模型（`*` 标注当前，失败仍显示当前模型），带参直接切换不校验；带尾随空格支持补全（接口列表在 REPL 内首次加载后缓存，失败不重试）
- `/think` 无参显示当前思考等级（未设置显示"未设置"）；带参 `minimal/low/medium/high/max` 设置，`off` 关闭，非法值报错不变更；带尾随空格补全等级候选（含 off，静态列表）
- `/reasoning` 无参显示思维链开关（`开`/`关`），带参 `on`/`off` 切换（非法值报错不变更）；带尾随空格补全 on/off 候选；开关是 REPL 局部状态（启动默认取 `show_reasoning` 配置，不落盘、重启回落）；门禁外（plain / 非终端）`/reasoning on` 提示「当前输出档不显示思维链」但记住开关，判定与 `turn.reasonOn` 同源（`reasonVisible`）
- `/load` 无参打开方向键选择菜单（`repl/picker.go`，stdin 非终端降级为序号输入），候选 Display 带时间/条数/简介
- `/theme` 无参显示当前主题与可用主题列表（含描述，`*` 标注当前），带参切换内置主题（非法值报错、主题不变）；带尾随空格补全主题名（内置静态列表）

### 提示符模板

- 提示符模板内置固定不可配（`prompt` 配置项与 `TANYA_PROMPT` 已移除，yaml 残留键被忽略），模板走 `render` 管线：启动时 `render.ParseTemplate` 一次，每轮 `Bind` 占位符 + 渲染（解析仅一次，绑定微秒级）
- 模板语法为 BBCode 风格标记：`[white]{cwd}[/] [blue]{model}[/]`，空格叠属性 `[red bold]`，支持语义名（dim/info/warn/ok/error/accent/think/run）；未知名/游离闭合/空标签降级原样，合法标签未闭合着色到行尾；旧裸 ANSI 模板自动 passthrough 兼容（无色环境 `Strip` 兜底）；不支持背景——markup 无 `bg:` 形式，`Style.Bg` 通道为预留（权威登记与启用条件见 `docs/style-split.md` §7.4）
- 占位符口径规则：**加 `_total` 后缀即本次运行累计，无后缀即最近一次请求**。单次：`{usage}` 上下文用量（API 实报；无实报或实报为 0 时回落 `~` 估算）/ `{cache}` 本次命中量 / `{cache_rate}` 本次命中率。累计：`{usage_total}` 累计用量 / `{cache_total}` 累计命中量 / `{cache_rate_total}` 累计命中率。另有 `{cwd}` 短路径 / `{model}` 模型 / `{effort}` 思考等级（未设置渲染为空）/ `{usage_summary}` 组合显示——**混合口径**，前段取 `{usage}`（单次）、后段取 `{cache_rate_total}`（累计），有累计缓存数据时拼为 `12.3k 81.67%`，否则只显示用量。注意它与 `/stat` 命令既不同源也不同口径：`/stat` 是七行全量的纯累计视图，`{usage_summary}` 只是提示符上的一行组合；无数据一律渲染为空，未知占位符原样保留，占位符值永不二次解析
- 缓存口径二分：**单次**取自最近一次响应（`Stats.ContextTokens`/`ContextHit`），**累计**取自本次运行加总（`Stats.PromptTokens`/`CacheHitTokens`/`TotalTokens`）；提示符变量以 `_total` 后缀区分两者，`/stat` 与响应回显行（`repl.RenderResponseInfo`）分别固定走累计与单次。所有比率经 `cacheRate`/`formatRate` 单一入口计算，「无数据」判定（`hit <= 0 || prompt <= 0`）全库只此一处
- 统计职责分层：`agent` 只累加与出数——`usageStats` 仅 `record`/`reset`/`view`，对外唯一门面是 `Agent.Stats() Stats`（工作区、会话文件、消息数、估算值加 usage 各项计数，全为数值）；数字缩写（`12.3k`）、百分比、`/stat` 七行文案与全部统计文案常量归 `repl`（`repl/stats.go` + `repl/messages.go`），`agent` 内不含任何格式化代码
- 默认 `[white]{cwd}[/] [blue]{model}[/] [yellow]{effort}[/] [green]{usage_summary}[/] [white]>[/] `（路径白 / 模型蓝 / 思考黄 / 用量绿 / 提示符白），渲染字节与旧 ANSI 版逐字节一致
- `{cwd}` 取进程 cwd（恒为启动目录，`os.Chdir` 不参与），短路径规则与原 `shortCwd` 一致（`$HOME` 折叠为 `~`、中间路径段截断为首字符）

### 回合视觉分隔（回合末尾方案）

- 每回合结束后、下一个提示符之前打印一行绿色（语义色 `Ok`）分隔线：`──── 15:04:05`；有模型调用的回合追加 ` · 回合 12.4s`（斜杠命令回合只有时间）
- stdout 非终端旁路：`term.GetProfile().TTY` 为假（管道/重定向）时**分隔线与"输入后留白"都不打印**——Degraded 输入不回显，留白会变成提示符下方凭空一行空行；且管道输出需保持可 diff、可再喂给其他工具。无色但仍是 TTY 时照常打印纯文本分隔线
- 打印时机选在**回合末尾**而非回合开头：分隔线只在本回合确实结束时产生，空输入、`^C` 取消输入（`ErrInterrupt` → `continue`）、`/exit`、`^D` EOF 四条路径都不打印，屏幕不会留下"没有对应输出的孤儿行"；首个提示符之前也不打印（欢迎语即开场）。回合开头方案在物理上无法拦截这四条（分隔线必须先于 `Readline` 打印，而读入前无从判断本回合是否有输出），故不采用
- 空行归一化：分隔线自带前导 `\n`，而工具状态行 / info 行 / 命令输出的末尾都恒为单 `\n`，因此"上一段输出 → 分隔线"之间恒 1 空行；用户提交后到本回合首个事件之间由 `turn.Handle` **懒补** 1 空行（首个事件前打一次，`KindDecor`），零事件回合（如 `Ask` 立即报错）不补，故不会与分隔线的前导换行叠成双空行
- 耗时口径：用户提交 → `Ask` 返回，含本回合全部 LLM 请求与工具执行；`interactive: true` 的 run_shell 期间用户在终端应答的时间也计入（读数偏大属预期）。回合耗时是"提交 → 返回"的汇总层，与 info 行的单次请求耗时（`TTFT/x.xs`）、工具状态行的单工具耗时并列
- 耗时格式：`<1s` 毫秒（`900ms`）、`<1m` 一位小数秒（`12.4s`）、`<1h` `12m34s`、更长 `1h02m`；独立于 `respDuration`（后者服务于 info 行与工具状态行，避免其口径被改动）
- 测试：`repl/turnview_test.go`（格式/颜色/前导尾随换行、耗时档位、`turn` 只补一次空行）；交互路径用 `script` + 延时喂入 pty 手工验证（raw 切换会清掉已缓冲输入，输入须在 raw mode 启用后到达）

### 终端输入（readline 包）

- editor：行编辑/历史，快捷键 Ctrl+A/E/B/F/U/K/W/Y/T/L、Alt+B/F（按空白分词）、Home/End/方向键；render 多行感知（`cursorRow` 精确跟踪光标行，重渲染上移清屏；光标行列由 `layoutCursor` 按终端软换行模型计算——宽字符在行尾放不下时整字换行留空、写满行末的 deferred autowrap，均与终端一致），Size 不可用退化单行；ErrInterrupt 区分 Ctrl+C；Ctrl+L 推屏保历史（一屏减一即 rows-1 个换行——恰把提示符上方内容滚入回滚区、不多滚一行，光标回视口顶部重画提示符，Size 不可用退化 `\x1b[2J` 擦屏），历史保留量受终端 scrollback 容量限制
- Tab 补全菜单：多候选时在输入行下方渲染菜单，选中项反显（`\x1b[7m`）；`↑/↓` 循环选择（菜单打开时不触发历史导航）、`Tab` 循环下一项、`Enter` 仅插入选中项（再次 Enter 提交）、`Esc` 关闭、任意输入关闭菜单正常编辑；单候选直接补全、公共前缀先行扩展的行为不变；候选超 8 行滚动窗口显示
- keys：ESC 序列/控制键/UTF-8 状态机；width：`term` 薄包装（宽度表/ANSI 剥离/感知截断均由 `render/term` 提供，截断自动复位悬空 SGR 防串色）
- 终端挂断（pty master 关闭、控制终端消失）：挂断后 `read` 既可能返回 `EIO`，也可能返回 0 字节且无错误——后者与 `VMIN=0/VTIME=1` 的空闲超时（0.1s 后返回 0 字节）在返回值上无法区分。`ReadKey` 以 `poll` 的 `POLLHUP/POLLERR/POLLNVAL` 判挂断、并把 `EIO` 归一为 `io.EOF`，REPL 据此正常退出（修复前挂断后的空闲轮询退化为忙循环：实测约 400 万次 `read`/秒、单核满载、进程永不退出）
- stdin 非终端降级：`Degraded` 按行读取（无行编辑/历史/补全菜单/ghost）；状态行是否输出另由 stdout 判定，见 `docs/terminal-caps.md`
- tty 桥接（`bridge.go` 接口 + `bridge_linux.go` 实现 + `bridge_stub.go` 非 Linux 返回 `ErrUnsupported`）：为 `interactive: true` 的 run_shell 提供"命令在自己的 pty 中运行"的执行器（`TTYBridge.Prepare/Attach`），复用本包 termios 读写与 raw 语义；生产入口 `readline.NewTTYBridge()` 由 `main.go` 经 `agent.WithTTYBridge` 注入到 `agent.New`（构造期定格进 `shellTool.bridge`），测试可在 `shellToolConfig` 里直接给 fake bridge
- 终端状态自愈（`secure.go` + `secure_stub.go`）：`InitTerminalGuard`（启动时记录"自己是否为终端前台作业"，并 `ctty.IgnoreJobSignals`——`tcsetpgrp` 在前台被抢时需忽略 `SIGTTOU` 才不被停住）+ `SecureTerminal`（恢复 `ISIG`、必要时 `ctty.SetForeground` 夺回前台组）；控制终端原语均走 `ctty`
- 平台划分：`terminal_posix.go`（`linux || darwin`）以 termios（`termios_linux.go` 用 TCGETS/TCSETS/TCSETSF、`termios_darwin.go` 用 TIOCGETA/TIOCSETA/TIOCSETAF）+ `ctty.Size` 实现真实输入；`terminal_windows.go`（`windows`）以控制台模式实现真实输入——`Raw` 开 `ENABLE_VIRTUAL_TERMINAL_INPUT`、清 `ECHO/LINE/PROCESSED`，输出侧 `ctty.EnableVT` 兜底，`readChunk` 用 `GetNumberOfConsoleInputEvents` 轮询 5ms + 1s 超时对齐 posix 的 `VMIN=0/VTIME=1` 语义，`Size` 走 `ctty.Size`；`terminal_stub.go`（其余平台）返回 ErrUnsupported 走 Degraded。按键状态机（队列/解析/超时 flush/挂起判定）已抽为平台无关的 `keySource`（`readline/terminal_io.go`），分片只实现 `readChunk` 与可选 `hungUp`——**降级只影响输入侧**，显示侧不受影响，且 ghost/补全菜单/历史随 raw 自动生效（编辑器与平台无关）。逃生开关 `TANYA_NO_RAW_INPUT=1` 强制回落 Degraded（两平台通用）；Windows 侧实机验证清单见 `docs/terminal-caps.md` §8
- 终端探测（`ctty.Facts` + `ctty.Probe()`，2026-09-16）：stdin/stdout 是否终端（posix `GetTermios`、windows `GetConsoleMode`）、尺寸（`TIOCGWINSZ` / `GetConsoleScreenBufferInfo`）、VT（windows 幂等开 `ENABLE_VIRTUAL_TERMINAL_PROCESSING`，posix 恒真）由 `ctty` 单点探测，`main.go` 组装出 `term.Profile`（`DetectProfile(stdoutTTY, vt)`）与 `repl.TermFacts`（宽度）；`readline.NewTerminal` 的 bool 语义收窄为"输入后端可用"，不再外泄为渲染判定。支持范围、组合矩阵与分阶段见 `docs/terminal-caps.md`

## 配置

优先级：env（`TANYA_*`）> `~/.config/tanya/config.yaml` > 默认值。

| 配置项 | 默认 | 说明 |
|---|---|---|
| `base_url` | `https://api.openai.com/v1` | API 地址 |
| `api_key` | 空 | 密钥（建议用 env 注入） |
| `model` | `deepseek-v4-flash` | 模型名（运行时可被 `agent_custom` 工具改写，仅本次会话） |
| `temperature` | 0.7 | |
| `reasoning_effort` | 空 | 思考等级 minimal/low/medium/high/max，非法值忽略；空则请求不带 `reasoning_effort` 字段（运行时可被 `agent_custom` 工具改写，仅本次会话） |
| `path`（只读，非 yaml 项） | — | 生效配置文件绝对路径，仅经 `agent_custom get config_path` 暴露给模型；默认 `~/.config/tanya/config.yaml`，`-c` 覆盖 |
| `show_reasoning` | `false` | 思维链是否随对话显示（markdown 渲染 + `─── 思考 ───` / `─── 思考结束 · 3.2s ───` 分隔；仅 REPL rich 档生效，无 env）；REPL 内 `/reasoning on\|off` 可运行时切换 |
| `api_protocol` | `responses` | API 协议 responses/chat（见《LLM 接入》），非法值启动报错 |
| `colors` | `auto` | 终端配色 auto（跟随终端能力与 `NO_COLOR`）/ on（强制开色）/ off（强制纯文本） |
| `theme` | `nord` | 内置配色主题（语义色/提示符/markdown 标题与代码整体切换）：default/minimal/solar/vivid/nord/gruv/dusk，非法值启动报错 |
| `palette` | 空 | 语义色覆盖（info/warn/ok/error/dim/accent/think/run → 色名），叠加在当前主题之上（切换主题后自动重放） |
| `user_agent` | `pi/0.85.0 (...)` | 出站 UA 伪装 |
| `data_dir` | `~/.local/share/tanya` | 数据根：global 模式的 workspace 目录为其下 `workspaces/<workspace-id>/`（其内 `sessions/` 与 `archive/`），支持 `~` 展开 |
| `session_mode` | `auto` | 会话存储模式 auto/local/global |
| `tool_output_lines` | 20 | 工具输出最多显示行数（1-1000） |
| `shell` | 空 | run_shell 使用的 shell（名字或绝对路径）；空则平台探测（linux/darwin bash→sh→ash，windows pwsh→powershell），全部落空启动报错 |

env 覆盖：`TANYA_BASE_URL` / `TANYA_API_KEY` / `TANYA_MODEL` / `TANYA_TEMPERATURE` / `TANYA_REASONING_EFFORT` / `TANYA_API_PROTOCOL` / `TANYA_DATA_DIR` / `TANYA_SESSION_MODE` / `TANYA_THEME` / `TANYA_USER_AGENT` / `TANYA_SHELL` / `TANYA_TOOL_OUTPUT_LINES`。

配色主题：`render/theme` 内置 `Scheme` 聚合（语义色 + 提示符模板 + markdown 样式集），**无全局可变状态**——`Lookup` 取方案、`Apply(sem, palette)` 纯函数叠加覆盖；REPL 持有当前 `Scheme`/`Semantics`，`/theme [name]` 切换后语义色、渲染器与提示符即时重建（palette 重放），readline 通过 `SetStyles` 注入。默认启动主题取 `theme` 配置，校验由 `repl.ValidateTheme` 承担（agent 不依赖表现层）。

## 测试

标准库 `testing` + `httptest` mock LLM（`agent/mock_test.go`，脚本化 `mockStep`，content/arguments 多 chunk 发送以覆盖流式合并）。覆盖 calc/shell/config/SSE 解析/trim/会话往返/Ask 全链路/回调。readline 用 fakeTerm 注入按键，终端挂断/空闲超时/转义残片等真实 pty 行为由自驱动 pty 自动覆盖（`readline/terminal_posix_pty_test.go`），其余真实终端行为 pty 人工验证。repl 覆盖渲染纯函数与非 TTY 降级。

repl 输出侧测试方法（输出收敛方案阶段 0-4 建立）：① **注入 writer**——`NewStreams(out, err, mode)` 可注入，测试用 `syncBuf`（互斥缓冲，`-race` 安全）与 `writeCounter`（断言"整块一次写完"），不再替换 `os.Stdout`；② **fakeTerm 驱动 Run**——`repl/faketerm_test.go` 实现 `readline.Terminal`，带 `inKey` 标志与 `onKey` 钩子；③ **写权协议断言**——`output.guard` 仅在 `emit`/`atomic` 触发（裸 `Write` 不触发，避免 Editor 合法回显误报），"输入期不得 emit"由正向用例 + 反向对照用例（故意在 `ReadKey` 内写入必被捕获）双保险；④ **可见集矩阵**——`Kind` × 三档模式的可见性以硬编码表锁定；⑤ **golden 字节**——rich 非 TTY 与 TTY 追加式收尾各一条基线，plain/plain+verbose 各一条；⑥ **跨提交逐字节回归**——用 `git worktree` 检出上一阶段提交构建旧二进制，同 cwd、同 ldflags 跑同一输入序列（pty 经 `script -qec`），归一化时钟与耗时后 `cmp`，作为"默认行为零变更"的硬证据。pty 目视模板见 `scripts/repl_tty_check.sh`（自动跑前两条，其余人工）。

pty 桥接三层测试：① `readline/bridge_linux_test.go` 自驱动集成（测试自身分配 pty 充当真实 tty，经 `newBridgeTTY` 注入）断言子进程 `/dev/tty` 可读、`tty` 输出为 pty slave、`GPG_TTY` 覆盖、初始尺寸复制、raw 设置与恢复、子进程退出后 master 收到 EIO（防忘关 slave）、Attach 前预置输入不丢、子进程存活时 `stop()` 及时返回；② `agent/shell_bridge_test.go` 用 fake bridge（os.Pipe 造流）断言桥接全流程、Prepare/Attach 失败回退现状路径、非交互不触桥接；③ 真实 tty E2E（gated，用 `script -qec` 驱动真实 /dev/tty，不参与默认 `go test`）：`TTY_BRIDGE_E2E=1`（readline 单命令）、`TTY_E2E=1`（agent 全链路）、`TTY_E2E_REUSE=1`（同进程连续两次交互命令，覆盖 `ownTTY` 打开/恢复/重开复用路径）。

输出侧渲染审计（`scripts/render_audit.py`，先 `make build`）：内置 mock LLM（responses 协议 SSE，事件形态对齐 `agent/mock_test.go`）+ pty 驱动真实二进制 + VT 回放（DECSTBM / 自动换行 / 光标可见性 / 备用屏 47·1047·1049 / 保存槽按屏索引 / SGR 状态与 DECRC 属性恢复；DECSTBM 按真终端实测建模——光标一律 home 到绝对 (1,1)，比 xterm 的「夹到上边界」更狠）+ 不变量断言，全量约 15s、无网络依赖。不变量：`overwrite`（写入非空白单元格）、`region_scroll`（只在滚动区内滚动）、`cu_clamped`（相对上移超出光标所在行，会被视口夹到顶行）、结束时 `autowrap_off` / `cursor_hidden` / `margins_set` / `alt_screen_on` / `sgr_open`。场景 want 三档：`clean` 要求不变量全为 0（回归门）、`leak` 断言 `expect` 列出的违反项被复现（已知缺口门，修好后改成 `clean`）、`note` 只报告不断言；`--dump NAME` 打印该场景回放后的屏幕。诊断计数 `cursor_restore`（光标被保存槽恢复且位置确实跳变，**且该槽在本回放中从未被写过**——即陈旧槽值被恢复，裸发 `DECRST 1049` 的典型症状；被 `DECSC`/`1049h` 写过的槽被恢复属预期、不计数）不断言、仅报告，真正的门是 `overwrite` 与结束态不变量。当前 leak 只剩一条待修：`leak-picker-unpaged`（picker 未按屏幕高度分页，`CursorUp(len(items)+1)` 被夹到顶行）；光标锚点类的回归门五条：`clean-tty-scrollregion`（子进程 `printf '\033[20;24r' > /dev/tty` 设滚动区，终端把光标 home 到 (1,1)）、`clean-tty-cup`（子进程 `CSI 3;7H` 直接挪光标）、`clean-alt-screen-exit`（子进程用 `47h` 进备用屏后退出）、`clean-tty-modes`（`?7l`/`?25l` 残留）、`clean-interactive-release`（interactive 密码提示后桥接 release 的 `?1049l` 跳位）——交出终端前 `SaveCursor` 与复位后 `RestoreCursor` 任一步退化成 no-op，这五条连同其余 clean 门都会变红（实测去掉存档得 `overwrite` 44、去掉归位得 `overwrite` 50），故它们是 2026-09-16 光标锚点修复的回归门。note 只剩 `note-partial-line`（子进程直写 `/dev/tty` 的半行残文）

会话归档测试：卷往返（entry 字节与源文件一致、entry comment 的条数与摘要与实读扫描一致）、comment 上限（4 KiB 硬上限、多字节截断降级、`trunc` 标记）、筛选（`OlderThan`/`Keep` 交集、`Exclude`、5 分钟空闲保护、`DryRun` 不落盘、已在卷内 id 去重、0 候选不建空卷）、损坏卷（截断/CRC 错：列表不崩、载入报错且卷不动）、`*.tmp-*` 忽略、同 id 双区取活动、归档只读态不写盘且 `Ask` 报 `ErrArchiveReadOnly`、`Fork` 落盘内容与后续增量、`resolveWorkspaceDirs` 三态推导、`ParseArchiveArg` 表驱动、picker `[归档] ` 标记渲染。

## 环境段（envprobe）

- 定位：只注入模型无法廉价自探的最小事实集——平台事实与 run_shell 执行契约；工具清单不注入 prompt（function calling 已完整提供），工具版本/分支/目录列表等易变信息模型可按需自探，一律不预注入
- 组装：`runtimePrompt()` = persistPrompt（规则，冻结）+ 空行 + `Agent.env`；`envSection(cwd, profile)` 在 `agent.New` 调用一次、结果定格进 `Agent.env`，会话期间（含 `/new`、`/load`）不重算；无注入点、无 `probe` 字段（曾有 `envProbeFunc` 注入与 `probe` 配置项，2026-09-14 收口删除）
- 为何定格：system 位于序列化后的 messages/instructions 之前，其任何字节变化都击穿其后全部 history 与 tools 的缓存前缀（实测量化见 `docs/cache-probe.md`《落实：env 段的动态源》；历史案例 `WORKSPACE` 行增删一行：命中率 96.63% → 3.21%）。定格同时是新增字段的准入红线：只收构造期确定的事实，永不引入请求期/TTL 类输入
- 取舍：环境事实定格在进程启动时刻（换目录/换机需重启进程；本进程 cwd 恒定，实际不构成限制）；目录内容、分支、工具版本等项目事实一律不注入，由模型按需自探（`ls`/`git rev-parse` 等）
- 输出格式（7 行紧凑键值，全部源自构造期事实——`runtime` 平台常量、cwd 快照、`shellProfile`、`shell.go` 契约常量；同 cwd 下字节级确定）：

  ```
  # 环境
  OS: linux/amd64
  CWD: ~/Project/tanya
  SHELL: bash
  TTY: 交互提示须写入 /dev/tty 才可见（stdout/stderr 被工具捕获）
  TIMEOUT: 默认 60s（interactive 时 300s），上限 900s
  OUTPUT: stdout/stderr 头尾各 30KB，中间截断
  ```

- 事实源单一：SHELL 行取 `shellProfile.Name`（与 `run_shell` 工具描述同源，只报 shell 名、不描述调用形态），TIMEOUT/OUTPUT 两行由 `shell.go` 常量程序化生成（`shellTimeoutSec`/`shellInteractiveTimeoutSec`/`shellTimeoutLimit`/`shellMaxOutput`），TTY 行固定契约文案，无第二份硬编码描述；各行恒定输出（shell 缺失时进程已在启动阶段退出）
- 平台条件：`TTY:` 行仅在 `ctty.Supported`（linux/darwin）为真时输出，不宣称不存在的 /dev/tty 能力
- 探测机制：`envSection` 为构造期纯函数，输入全为构造期事实——`runtime` 平台常量、`os.Getwd` 快照（进程全程不 `os.Chdir`）、`shellProfile.invocation()`（`shellTool` 构造期定格）、`shell.go` 执行契约常量；进程内零重复探测、零 exec
- 可测性：分层测试——persistPrompt 只含规则 / envSection 直接断言渲染（全串 golden） / runtimePrompt 拼接 / 同参数两次渲染字节相等 / **会话期间冻结守卫**（构建后改动目录内容不得改变 `runtimePrompt`）
