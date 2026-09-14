# tanyan 核心设计

## 定位

极简命令行 AI Agent，Go 编写，无 GUI/WebUI/TUI。单二进制，交互式 REPL + 单发模式。

## 架构

```
main.go            package main：入口、flag 子命令、ask 单发
repl/              package repl：REPL 循环、斜杠命令、补全、工具视图渲染、等待动画
agent/             package agent：全部核心逻辑（config / llm / agent / prompt / session / stats / shell / builtin）
readline/          package readline：自研终端输入层（editor / keys / terminal）
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
- **不做工具注册表**：工具硬编码于 `ToolDefs()` 和 `Agent.dispatch` 的 switch，存量小且预计长期以 shell 为主
- **依赖仅 2 个**：`gopkg.in/yaml.v3`（配置）、`golang.org/x/sys/unix`（raw mode）；终端输入层与富文本管线自研
- **颜色铁律**：SGR 与 CSI 仅 `render/style`、`render/term` 产生（业务代码不得出现裸 `\x1b`，readline 的光标操作也走 `term.Cursor*`）；同一 IR 按终端能力档案（`term.Profile`）降级，无色终端自动纯文本

事实归属（不设共享暴露层）——这些结论不再重复讨论：

- **进程事实**：`cwd`、家目录与工作区基准在 `agent.New` 读一次、注入 `shellTool` 构造期定格（旧六参形态与包级 shell 状态已删，见 `docs/shell-tool.md` §14/§16）
- **`ctx` 属请求层**：只承担取消/超时，不承载进程事实（`repl.Run()` 不收 ctx；每回合由 `InterruptContext()` 现造，以 `context.Background()` 为根）
- **tty 与颜色能力由消费方独占**：`term.Profile` 由 `term.DetectProfile` 计算、只有 `term` 保留进程级默认档案（终端能力是名副其实的进程事实）；语义色 `theme.Semantics` 为值传递（repl 持有当前方案、readline 经 `SetStyles` 注入，见 `docs/style-split.md`）；终端尺寸是实时值（`ToolWidth` 以函数传递）；启动前台状态与 `ISIG` 自愈归 readline（`InitTerminalGuard`/`SecureTerminal`）；`ttyStdinSupported()` 是编译期平台常量。`handoverForeground`（agent）与 `foregroundTTY`（readline）两处"当前前台组是否为本进程"的判定按各自问题分别采样，不构成重复，暂不合并

## 运行模式

- `tanyan`：交互 REPL，维护内存 messages 历史，SSE 逐 token 流式输出
- `tanyan ask "问题"`：单发，输出后退出。单发默认走 plain+verbose 档（`repl.SingleShot` 在 CLI 模式为 rich 时降到 `modePlainVerbose`；显式 `-p` 更窄则保持不动）：无 spinner、无光标上移重绘（工具块追加式）、正文原样直出，颜色仍按终端能力保留，`End()` 按 plain 语义"缺行尾换行才补"
- 全局参数：`-c <path>` 指定配置文件、`-m local/global/auto` 会话存储模式、`-n` / `--no-save` 只读会话（见《会话与上下文》存储小节）
- Ctrl+C 中断进行中的请求（context 取消，导致 API 错误直接暴露）：REPL 与 `ask` 单发统一走 `signal.Notify(SIGINT)`（`repl.InterruptContext`），要求终端 `ISIG` 开启——readline 侧每回合开始前做终端状态自愈保证该项成立（`docs/interactive-tty.md` §5.9）；命令执行期间子进程组持有终端前台，Ctrl+C 由内核直达子进程组（命令优雅退出），再次按下取消回合

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

本地不设轮数上限，依赖模型终止。每轮请求发事件：`EventRequestStart`（请求前）/ `EventResponse`（响应后，含出错路径，携带 `ResponseInfo`：Duration/TTFT/Usage/ContextTokens）；事件词汇表见 `agent/event.go`，渲染侧以 `agent.EventSink` 单通道接收。

回合非正常结束（`Ask`，按"有无产出"分派，产出 = 本回合出现过完整 assistant/tool 消息）：

- 用户中断（`ctx` 取消）：有产出 → 保留全部产出并追加 user 提示 `[用户已中断本轮请求]` 后落盘；无产出 → 回滚到回合起点
- 其它错误：有产出 → 保留产出并追加 user 提示 `[本轮因错误中止：<错误摘要>]`（摘要 200 rune 截断）后落盘；无产出 → 回滚
- 终止提示为 user 角色、仅陈述事实不含引导词；写入 history 后不可变（append-only 落盘），保证请求前缀稳定与 prompt cache 命中；`save()` 失败静默，`saved` 游标未推进时下次成功回合补齐
- 中断返回 `*InterruptError{Kept}`（REPL 据此区分"已保留/未保留"文案），错误路径返回原错误；已完成的工具步骤（含被 SIGINT 终止的 run_shell，其结果照常回填）随产出一并保留，续接时模型可回溯此前实施

## 工具

### run_shell（`agent/shelltool.go` 组件 + `agent/shell.go` 叶子）

- 参数：`command`（必填）、`cwd`（可选，命令执行目录，默认会话启动目录）、`timeout`（默认 60s，上限 900s）、`interactive`（布尔，默认 false）
- 执行目录：默认继承进程 cwd（= 会话启动目录，进程全程不 `os.Chdir`）；显式 `cwd` 时设 `cmd.Dir`（不改进程 cwd），解析规则为 `~`/`~/x` 展开家目录、相对路径按工作区基准合成（家目录与工作区由 `agent.New` 各读一次注入 `shellTool`，构造后只读；缺基准时相对路径直接失败而非退回环境 cwd），随后 `os.Stat` 校验——不存在或非目录直接快速失败（`MsgBadCwd`，不启动进程）。显式指定时 `ShellResult.Cwd` 填充解析后的绝对路径，`String()` 首行输出 `cwd: <路径>`。桥接与回退两条路径均生效（`TTYBridge.Prepare` 只改 `SysProcAttr`/标准流/`Env`，不覆盖 `cmd.Dir`）
- 波浪号边界（不宣传的默认契约）：只处理 `~` 与 `~/x`（`~` 展开家目录，基准由 `agent.New` 注入）；`~user` 与 Windows 风格 `~\x` 一律不展开——按相对路径解析并因不存在直接报 `MsgBadCwd`（快速失败，不误执行）。`run_shell` 工具描述与 `cwd` 参数描述均不提 `~`（避免引导模型使用）；env 段 `CWD:` 行的 `~/...` 只是完整路径的显示缩写，不是路径语法引导
- 交互模式（`interactive: true`）：仅由模型显式声明，**不做命令文本猜测**（早期版本有 sudo/ssh 关键词兜底，review 后移除）。声明后 repl 侧停用等待动画、标题行下打印引导行、结束用追加式渲染（避免 `CursorUp` 擦掉用户输入回显）；`timeout` 缺省时默认放宽至 300s（显式值优先，上限仍 900s）。命令在**独立 pty** 中运行（见下条），提示与输出实时可见；非桥接回退路径下命令提示须自行写入 `/dev/tty`，否则被工具捕获不可见
- 交互式 pty 桥接（`readline/bridge_linux.go` + `agent/tty_bridge.go`，linux 专用；决策与背景见 `docs/interactive-tty.md`）：解决"交互程序拿不到输入"（`/dev/tty` 直通导致 `ttyname(0)` 退化为 `/dev/tty`、pinentry 等无 ctty 程序无法按路径打开）。流程 `Prepare`（分配 pty、`Setsid+Setctty+Ctty=0`、三条标准流全接 slave、`GPG_TTY`/`SSH_TTY` 覆盖为 slave 路径）→ `Attach`（真实 tty 切 raw、初始尺寸复制到 master、启动双向泵）→ `cmd.Start()` → 立即关闭父进程 slave（否则子进程退出后 master 收不到 EIO）→ `waitShell` → `stop()`（恢复 termios、关闭 tty/master、泵收尾 drain 后 `capture.finish()`）
  - 契约：master 输出**同时**写真实 tty（用户实时可见）与 `capture`（Writer，调用方决定去向）。本处 capture 即 `streamCapture` → `ShellResult.Stdout`，交互模式为**单流**（`Stderr` 空，`2|` 区分失效）；`streamCapture` 头尾截断与 `ShellResult` 字段语义不变
  - 子进程成为独立会话首进程、ctty 为 pty slave，真实 tty 前台组始终是 tanyan，**不再需要 `TIOCSPGRP` 移交**；`^C` 经泵作为字节进入 pty，由 slave 行规程投递 `SIGINT` 给子进程前台组，tanyan 不拦截
  - 已知语义差异：`Setsid` 后子进程组为孤儿进程组，内核按 POSIX 丢弃停止信号，**`^Z` 在桥接下不挂起子进程**（无效按键，`^C` 正常）；按 `docs/interactive-tty.md` §7 沿用现状、不新增分支（`waitShell` 的 `processStopped` 轮询保留，显式 `SIGSTOP` 等仍检出）
  - 泵用 `poll` + 自管道唤醒（`stop()` 关写端令两向阻塞读退出），保证 stop 不悬挂、不漏读残留输出；写侧 `O_NONBLOCK` + `POLLOUT` 防子进程不消费时卡死
  - 接口契约：**单次使用、非并发**——`Prepare → Attach → stop` 各一次；实例带 busy/attached 守卫（互斥量），并发或重复调用一律返回 `ErrUnsupported` 走回退，避免 pty/泵泄漏（为 §6"用户前台命令"复用的地基预留）
  - 失败回退：无控制终端 / 非前台（`TIOCGPGRP != getpgrp`）/ pty 分配失败 / `SetNonblock` 失败 / `Attach` 失败 / 非 Linux（`bridge_stub.go` 返回 `ErrUnsupported`）→ 走原 `open("/dev/tty")` + `TIOCSPGRP` 路径，非交互路径行为零变化
  - 终端状态自愈（`readline/secure.go`）：桥接 `Prepare` 的前台检查**之前**与 REPL 每回合开始前调用 `SecureTerminal()`——① 恢复被外部清掉的 `ISIG`（否则 `^C` 不产生 `SIGINT`，中断路径完全失效）② 限"启动瞬间自己就是终端前台作业"时夺回被 shell 抢占的前台组；后台启动（`&`）/无控制终端场景门控为否，语义不变（`docs/interactive-tty.md` §5.9）
  - 结果语义：被信号终止的子进程按 shell 惯例记 `128 + signum`（`^C` → `exit 130`、`SIGKILL` → `137`），不再是 `-1`（`shellExitCode`，unix 取 `WaitStatus.Signaled()`）
  - 刻意简化（`docs/interactive-tty.md` §7 登记，评审直接引用关闭）：`^C` 计数双杀、中断结果标记、输出清洗、`SIGWINCH` 转发（尽力而为，失败不报错）、桥接期显示对齐
- 实现：按 `shellProfile` 组装命令（posix `<path> -c`、powershell `<path> -NoProfile -NonInteractive -Command`、cmd `<path> /d /s /c`），捕获 stdout/stderr/退出码/耗时（`ShellResult` 结构化返回：Command/Cwd/Stdout/Stderr chunks/Err/ExitCode/TimedOut/Interrupted/Stopped/NotStarted/Duration）
- 中断语义：运行中被 ctx 取消 → `Interrupted`，结果追加 `error: 已中断（进程已终止，输出可能不完整）`；ctx 已取消导致命令未能启动 → `Interrupted+NotStarted`，追加 `error: 已中断（命令未执行）`（不再把裸 `context canceled` 交给模型）；toolview 状态行分别为 `已中断`/`未执行`，已捕获的首尾输出照常保留
- shell 解析（`newShellTool`，Agent 构造时一次性解析并定格）：
  - 优先级：配置覆盖（`config.yaml shell:` / env `TANYA_SHELL`，名字或绝对路径，任意 shell 名允许，未知 basename 按 posix `-c` 处理）> 平台自动探测
  - 自动探测：windows 仅 `pwsh`（强制 PowerShell 7，不回退 5.1/cmd）；linux/darwin `bash` → `sh` → `ash`
  - 全部落空（含配置的 shell 不存在）：解析返回错误（`MsgNoShellFmt`/`MsgShellOverrideFmt`，含候选清单与配置提示），`agent.New` 立即透传，`main.go` 打印后以 1 退出——无降级路径，`shellTool.profile` 在其后恒非 nil，profile 的非空成为不变量（`run_shell` 恒定注册、env 段恒定输出 SHELL/TIMEOUT/OUTPUT 行、system prompt 恒为 `DefaultSystemPrompt`）
- 程序探测：profile 就绪后对固定清单（ls/cat/head/tail/grep/rg/fd/sed/awk/find/sort/wc/cut/tr/xargs/git/curl/wget/go/node/python）逐个 LookPath，存在的拼入 run_shell 工具描述 `可用程序: ...`，仅在工具描述出现，不重复注入 env 段
- 输出捕获：stdout/stderr 各保留头 30000 字节 + 尾 30000 字节（`streamCapture` 滚动窗口），中间字节计数丢弃，模型仍可见首尾内容
- 组件化（`docs/shell-tool.md`）：`shellTool` 是 shell 执行层唯一所有者，`profile`/`programs`/`workspace`/`home`/`bridge` 在构造期定格、之后只读，`run` 每调用状态全在栈上（可重入）；唯一可变字段是终端租约 `ttyMu`——真实终端进程内只有一份，桥接与前台移交两条路径都在锁内。组件内不读环境（无 `os.Getwd`/`os.UserHomeDir`/`exec.LookPath`/`runtime.GOOS`），`agent.New` 装配点各读一次注入。包级可变状态（`shellRuntime*`/`shellLookPath`/`ttyBridgeMu`+`ttyBridgeCur`）已删除；`envSection`/`runShellDesc`/`ToolDefs` 为纯函数，工具清单在 `NewClient` 构造期注入 client（请求组装不再伸手读包级清单）
- 实测契约（sudo 两模式对照）：`sudo` 默认模式自开 `/dev/tty` 完成提示与密码输入——前台移交后提示实时可见、密码不回显，仅最终错误走 stderr 回流；`sudo -S` 强制从 stdin 读密码时提示改写 stderr（被捕获，等待期间不可见），交互命令应避免 `-S` 类强制 stdin 选项
- 终端前台移交（unix，shell_tty_unix.go；illumos/ios 与 windows 等 !unix 平台无实现，降级 no-op，`ttyStdinSupported()` 为假）：执行前打开 `/dev/tty`，仅当自身进程组已是前台时 `TIOCSPGRP` 移交子进程组（`handoverForeground`），子进程结束后以 `handed` 门控归还（`restoreForeground`，避免从未交接时抢占 shell 的前台）；无控制终端 / 非前台（嵌套、后台运行）自动跳过，行为与旧版一致。移交前台的同时将 `cmd.Stdin` 接到 `/dev/tty`（tty 打开成功时），子进程 stdin 直通用户终端，可直接在终端应答 ssh/git/sudo 等密码与确认提示，不再静默挂死至超时；无 tty 时 stdin 保持原状（/dev/null）
- 信号防护（`ProtectTerminalSignals`，main 启动时 `sync.Once` 一次性）：`Notify(SIGTSTP)` 吞没（命令间隙 Ctrl+Z 不挂起自身）、`Ignore(SIGTTIN/SIGTTOU)`（自身后台 tty 读写不停止）；SIGQUIT 保持 Go 默认（全栈转储）。忽略处置随 exec 被子进程继承，子进程后台读写 tty 得 EIO 而非停止
- 挂起探测（`waitShell`）：200ms 轮询 `/proc/<pid>/stat`，连续 2 次 `T` 判定被终端挂起（Ctrl+Z 等停止信号），SIGKILL 进程组并置 `Stopped`，状态行显示 `挂起已终止`，避免静默挂到超时；`processStopped` 由 shell_proc_linux.go 提供 /proc 实现，非 linux（shell_proc_other.go）恒 false（探测失效，其余功能不受影响）
- 字段集：`ShellResult` 为 Command/Stdout/Stderr chunks/Err/ExitCode/TimedOut/Interrupted/Stopped/Duration
- 事件：`EventToolStart`（dispatch 前触发）/ `EventToolEnd`（结构化 `ToolResult`：Shell/Text 二选一，发回模型的 content 由 `Content()` 拼回文本），渲染在 repl 包 `toolview.go`
- **免确认直接执行**（早期版本有 y/n/a 确认机制，已移除）

### 工具视图渲染（repl/toolview.go）

- `NewToolView` 构造渲染器（`repl/repl.go` 与 `main.go` 各接一处；`Handle(e agent.Event)` 即事件入口，`toolView` 自身即 `agent.EventSink`），块状视图：`▸ 工具名 命令` 标题行 + 缩进输出行（stderr 加 `2|` 前缀）+ 亮蓝状态行；`run_shell` **显式指定** `cwd` 时标题区改为三行——首行仅工具名（进行中带 `⋯`），其后 `cwd: <原样值>`（不缩写）与折叠后的命令各占一行（逐行按宽度截断；inline 重绘按标题行数上移），未指定时保持单行、与旧版逐字节一致
- 状态行总是输出（语义色 `Info`，无色环境纯文本）：`↳ exit 0 · 0.3s · 12 行`；异常时首段为 `exit 2`/`执行超时`/`已中断`/`挂起已终止`/`错误: ...`；输出被截断时行数段显示 `共 N 行`；builtin 工具无状态行（截断时仅显示 `共 N 行`）
- 颜色走 `theme.Semantics` 语义色 + `term.Profile` 驱动（`colors` 配置 / `NO_COLOR` / 非 TTY → 纯文本）：工具块 `Dim`、spinner 等待 `Warn`/思考 `Think`/执行 `Run` 三色、状态行 `Info`，可用 `palette` 配置覆盖
- **捕获输出的 ANSI 治理**（`render/term` 清洗 + `Renderer.Frame/Passthrough`）：块组装内聚于 `renderToolBlock`，输出区无 SGR 时整块 `Dim.Frame`（全清洗 + 块级包裹，标题/输出单一包裹点）；检测到 SGR（`HasSGR`）时输出区改走直显——`Passthrough` 保色渲染（SGR 原样保留、布局序列/OSC/C0 仍清洗、脏状态结尾闭合），标题行独立 Frame，状态行 `Info` 显式后置（不依赖 SGR 时序巧合）。预览类彩色输出（如欢迎屏效果）在灰色块内原色可见，用户与模型双通道分离：**模型侧文本不做任何变换**（原始输出、信息保真、缓存与历史零影响），显示侧机制对模型完全不可见
- `/history` 查看 tool 消息时正文走 `Dim.Frame`（纯显示侧，历史存储不动），顺带解决历史串裸序列漏进视图的问题
- 显示行数上限 `tool_output_lines`（默认 20，范围 1-1000），超出保留头 3 行 + 尾 2 行并提示 `/history n` 查看完整输出
- 执行开始即打印标题行（`⋯` 标记进行中，调用点 `Dim.Frame` 包裹，模型可控的 args 一并清洗）；结束分三种：TTY + 光标控制档 `\x1b[1A\r\x1b[K` 上移重绘标题替换 `⋯`（光标控制序列在 Frame 之外）→ `RenderToolEndInline`；交互式工具 → `RenderToolEnd`（前导空行 + 标题锚点，用户交互回显混在中间需要重新起头）；其余追加式场景（非 TTY、plain+verbose）→ `RenderToolEndAppend` 只补正文块与状态行——标题已由 ToolStart 打出且无法上移覆盖，重复标题会留下两行 `▸ 工具名`
- 交互模式（`Event.Interactive`）例外：不启动 spinner（周期重绘会擦掉子进程写往 tty 的提示），标题行下打印引导行 `⏎ 等待终端输入，请在下方直接应答`，结束一律追加式渲染（上移重绘会擦掉用户刚输入的回显行）；桥接期间真实 tty 归 bridge 独占（repl 侧不写入：标题行在切 raw 前打印，结果块在 `stop()` 恢复 termios 后渲染）
- 流式输出行尾无 `\n` 时（`toolView.dirty` 跟踪），状态行打印前自动补换行
- 输出收敛（`output`/`streams` 双流）、`Kind` 门禁与输出模式（rich/plain）、回合封装（`turn`）的改造规划见 `docs/repl-output-refactor.md`

### 等待动画与请求状态（repl/spinner.go）

- `EventRequestStart`：TTY 下显示 braille spinner（`⠋ 等待响应 3s`，100ms 帧，`\r\x1b[K` 行内重绘，与全部终端输出共享 mutex），颜色随阶段切换（等待 `Warn`/思考 `Think`/执行 `Run`）；首个 content delta 到达即停并转为流式输出（纯 tool_calls 响应持续到本轮结束）
- 工具执行期间标题行下方独立 spinner 行 `  ⠋ 执行中 3s`；`interactive` 工具不启动该 spinner（子进程直接写 tty 的提示会被 100ms 重绘擦除）
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
- 只读会话（`-n` / `--no-save`）：由 CLI 经 `agent.New(cfg, agent.NoSave(true))` 传入，`Config` 无对应字段，配置文件与 env 均无法开启；`ask` 单发与 REPL 通用。读路径全部保留（启动预扫描、`ListSessions`、`LoadSession` 照常，既有会话不会被截断或改写），写路径在 `sessionStore.append` 首行（`disabled`）返回 nil 被整体关闭（覆盖成功/中断/错误三条路径）；`MkdirAll(store.dir)` 在只读模式下跳过，目录缺失时 `store.refresh` 按空列表处理不报错。内存 history 照常维护（中断保留语义不变），进程退出即丢。REPL 启动时在欢迎屏下方以语义色 `Warn` 打一行 `MsgNoSaveWarn`（`REPL.noSaveWarn`，agent 为 nil 或可写时不输出），`ask` 静默
- 落盘：`<timestamp>.jsonl`，每轮结束追加写入新消息（一行一条 Message JSON），记录完整历史（回放/审计用）；回合因中断/错误保留产出时同样落盘（含终止提示行）
- 落盘原子性：本批消息先编码进内存缓冲再单次追加写入，写入报错或短写时 `Truncate` 回滚到写入前大小，`saved` 游标与文件内容始终一致（重试不会产生重复行/半行）
- 首行持久化 system prompt 快照（`systemSaved` 标志防重复），`/load` 还原后前缀与当初逐字节一致；旧格式文件（无 system 首行）回退为载入时快照当前 AGENTS.md，且保持不补写；system 行不计入 `/sessions` 条数与标题
- 会话列表扫描：`agent.New` 启动时预扫描填充缓存（只读模式不建目录，目录缺失按空处理）；`ListSessions` 按 (mtime,size) 增量刷新，仅重扫变化的文件；每文件 `bufio` 逐行计数条数、仅解码至首条 user 消息取摘要
- `/sessions` 列出（id、时间、消息数、首条用户消息摘要），`/load <id>` 恢复继续对话；id 校验拒绝路径穿越

## 系统提示与缓存友好

- 组装规则：`DefaultSystemPrompt`（内置，固定不可配，`system_prompt` 配置项已移除）+ 全局 `~/.config/tanyan/AGENTS.md`（存在时）+ 工作区 `./AGENTS.md`（存在时），各段以 `# 全局说明`/`# 项目说明` 标题分隔，文件缺失/空白跳过
- 规则与事实分离：persistPrompt（上述规则）在 `/new`/`/load` 时组装并冻结进会话首行；每次请求的 system = persistPrompt + `envSection(cwd)`（环境事实实时拼在末尾，不持久化、不冻结）
- 快照机制：`/new` 与 `/load` 时刻读取 AGENTS.md 组装快照；会话进行中零文件 IO，快照冻结；旧格式会话（system 首行含历史环境段）原样保留并标记，`/load` 时提示 `/new`
- 缓存收益：history 全程 append-only，同一会话内 messages 前缀逐字节不变，prompt cache 逐轮全量命中；`/new` 时 AGENTS.md 未变则 system 前缀跨会话命中
- 缓存命中捕获（DeepSeek `prompt_cache_hit_tokens` / OpenAI `prompt_tokens_details.cached_tokens`）经 `PromptCache()`/`PromptCacheRate()` 供提示符占位符显示
- 缓存机制的实测结论（64-token 块粒度、tools 段在序列化尾部的代价台阶、各后端写入延迟差异）见 `docs/cache-probe.md`

## REPL

用户可见文案统一为常量：`repl/messages.go`（UI/命令输出/选择器/工具视图/spinner）与 `agent/messages.go`（错误/ToolResult 文本/ContextInfo），调用一律引用常量（经 `streams` 写出，见下），换行由调用处的格式串控制；`Bye`/`再见` 已统一为 `MsgBye`。工具描述与系统提示不在此列（模型侧文案，翻译需评估 prompt 影响）。

欢迎屏由 `welcomLogo` + `welcomeText()` 组装：logo ASCII 图 + 一行 `输入 /help 查看命令   tanyan <版本>（构建于 <时间>）`；`repl.Version`/`repl.BuildTime` 由 `main` 注入（`make build` 经 ldflags 写 `main.version`（git describe）与 `main.buildTime`（date），直接 `go build` 为 `dev`/空，空时不渲染构建时间）。`-v` 与欢迎屏共用同一 version 源。

### 输出流与 Kind

输出收敛到 `repl/streams.go`：`streams{out, err}` 是两条独立互斥流，`output` = writer + `sync.Mutex` + 可见集（`visSet`，按 `Kind` 门禁，当前恒为全开，rich/plain 三档在阶段 4 引入）+ 测试钩子 `guard`。`output.Write` 是无门禁通道（raw 期自绘：Editor 提示符/回显、picker），`emit`/`atomic` 是带门禁与 `guard` 的常规通道；`atomic` 回调内只允许写参数 `w`（自锁约束）。每次写入都携带 `Kind`（`repl/flow.go`：Content/Reasoning/ToolBlock/ToolStatus/Notice/Decor/Error/Spinner）。

流分配：**stdout** 承载 assistant 正文、工具块与状态行、命令反馈、回放、欢迎屏、回合分隔线、spinner 帧与输入期回显；**stderr** 承载错误与诊断——`MsgErrLineFmt` 类、`MsgThemeBad`、`MsgInvalidIndex`、`MsgModelsFail`、`MsgInterruptKept`/`MsgInterruptBare`，以及 `main` 的启动/配置/agent 构造错误（`streams.Fail`）。两 fd 均无缓冲，同一 tty 下写序即调用序，故交互观感与收敛前逐字节一致（阶段 1 以 pty 对比前一提交的二进制验证）；stdout 被重定向时错误与诊断分流到终端，stdout 保持可解析。

`Kind` 不导出包外、不进 `agent.Event`；`main` 侧只用语义化出口 `streams.Print`/`Content`/`End`/`Fail`。

输出模式三档由 `outMode` 决定（`repl.ParseMode(plain, verbose)`，仅 CLI `-p`/`--plain` 与 `--verbose` 可设，env 与 config 不参与；`ask` 单发分支再套 `repl.SingleShot` 把 rich 降到 plain+verbose，显式 `-p` 更窄时保持不动）：**rich**（默认，全开）、**plain**（`out.vis` = Content/Notice，其余屏蔽；stderr 不参与屏蔽）、**plain+verbose**（再加 ToolBlock/ToolStatus）。plain 的六条语义：① `Colors=LevelNone`（main 在 profile 计算后强制）；② 不启动 spinner（`toolView.animate()` 同时查 TTY 与可见集，是查询不是快照）；③ 无光标控制（inline 上移重绘按 `streams.cursor()` 退化为 `RenderToolEndAppend`：不重复标题，只补正文块与状态行；spinner 帧与清行随 KindSpinner 一并屏蔽）；④ 关 markdown（`flow.mdEnabled` 并入 `st.decor()`）；⑤ 屏蔽 Decor（含首行空行与回合分隔线）与工具类；⑥ stdout 只留正文与命令反馈，错误与诊断走 stderr。工具块被屏蔽时**不得**置 `justEnded`，否则下一条正文前会留下孤立空行（`toolView.Handle` 的 ToolEnd 分支按 `allows(KindToolBlock)` 决定是否置位）。`streams.End()` 负责收尾换行：rich 沿用无条件补换行（零行为变更），plain 只在缺少行尾换行时补，保证 stdout 严格等于答案。

### 输入分发

输入按前缀分发（`repl/dispatch.go`），判定顺序固定：`exit`/`quit`（首 token 命中即内建退出，等价 `/exit`）→ 已知斜杠命令（`slashCommands` 白名单匹配首 token，故 `/load x` 命中、`/usr/bin/ls` 不命中）→ `:` 或全角 `：` 开头（剥离前缀与空白作为提问，空内容提示 `MsgDialogueEmpty` 不算回合、不打印回合分隔线）→ 其余整行直接作为提问与 AI 对话，与 `:` 前缀写法等价。白名单未命中的 `/` 开头输入（如 `/usr/bin/ls`）不作命令处理，直接作为对话内容。

### 直通 shell 执行面（归档）

原「其余输入在本目录直通执行 shell 命令」执行面已归档（2026-09）：体感作用有限——agent 侧已有 `run_shell` 工具，直通面与之重复且绕过上下文/契约。末态完整实现见 commit 60bc02e：执行四件套 `runShellLine`/`suppressInterrupt`/`waitShellCmd`/`reportShellExit`、cd 拦截切面 `repl/localcmd.go`（`tryLocalCommand` 单入口 + `dirChangeCommands` 表）、常量 `MsgShellExitCode`/`MsgShellSuspended`/`MsgKillFailFmt`/`MsgCdBlocked`、`agent.NewShellCmd`/`ProcessStopped` 导出包装。恢复步骤：自 60bc02e 取回上述文件与常量 → `Run()` 末分支 `r.ask(line)` 改回 `r.runShellLine(line)`。归档后进程 cwd 恒为启动目录（全程不 `os.Chdir`）；`run_shell` 默认即在此执行，另可用 `cwd` 参数为单次命令指定目录（设 `cmd.Dir`，进程 cwd 不变）。

### 斜杠命令

`/help` `/new` `/sessions` `/load` `/context` `/history` `/model` `/exit`：

白名单（`slashCommands`，同时驱动 Tab 补全）即分发契约：`Run` 先用 `isSlashCommand` 过滤，未命中的 `/` 开头输入按对话内容处理，因此 `handleCommand` 的 switch 不再有 `default` 分支（原先的 `MsgUnknownCmd` 不可达，已删）。白名单与 case 必须一一对应，`TestSlashCommandsAllHandled` 覆盖该不变量（`/load` 走 stdin 交互路径，单独测试）。

- `/history` 无参截断列表（`style.OneLine` 先剥离 ANSI 转义与控制字符、压成单行，再按 120 rune 截断，避免 `\r`/`\x1b[K` 覆盖已打印行与未闭合 SGR 泄漏）、`/history n` 全量查看单条、`/history all` 全量显示；全量显示时消息头 `#N 角色` 按一级标题渲染、并按角色着色（user 用 `Ok` 绿、其余用 `Warn` 黄；`#` 与序号连写不构成 markdown 标题，单独构造 Heading IR），assistant 正文走与对话一致的 Markdown 渲染（受 TTY 与输出模式约束：非 TTY、plain 一并旁路），user/tool 消息与工具参数原样
- `/model` 无参实时调接口列出可用模型（`*` 标注当前，失败仍显示当前模型），带参直接切换不校验；带尾随空格支持补全（接口列表在 REPL 内首次加载后缓存，失败不重试）
- `/think` 无参显示当前思考等级（未设置显示"未设置"）；带参 `minimal/low/medium/high/max` 设置，`off` 关闭，非法值报错不变更；带尾随空格补全等级候选（含 off，静态列表）
- `/load` 无参打开方向键选择菜单（`repl/picker.go`，非 TTY 降级为序号输入），候选 Display 带时间/条数/简介

### 提示符模板

- 提示符模板内置固定不可配（`prompt` 配置项与 `TANYA_PROMPT` 已移除，yaml 残留键被忽略），模板走 `style` 管线：启动时 `ParseTemplate` 一次，每轮 `Bind` 占位符 + 渲染（解析仅一次，绑定微秒级）
- 模板语法为 BBCode 风格标记：`[white]{cwd}[/] [blue]{model}[/]`，空格叠属性 `[red bold]`，支持语义名（dim/info/warn/ok/error/accent/think/run）；未知名/游离闭合/空标签降级原样，合法标签未闭合着色到行尾；旧裸 ANSI 模板自动 passthrough 兼容（无色环境 `Strip` 兜底）；不支持背景——markup 无 `bg:` 形式，`Style.Bg` 通道为预留（权威登记与启用条件见 `docs/style-split.md` §7.4）
- 占位符：`{cwd}` 短路径 / `{model}` 模型 / `{effort}` 思考等级（未设置渲染为空）/ `{usage}` 上下文 token（API 实报或 `~` 估算）/ `{cache}` 缓存命中量 / `{cache_rate}` 缓存命中率（两位小数，无数据渲染为空）/ `{stat}` 组合用量——无缓存仅总量，有缓存为 `缓存/总量 命中率`；未知占位符原样保留，占位符值永不二次解析
- 默认 `[white]{cwd}[/] [blue]{model}[/] [yellow]{effort}[/] [green]{stat}[/] [white]>[/] `（路径白 / 模型蓝 / 思考黄 / 用量绿 / 提示符白），渲染字节与旧 ANSI 版逐字节一致
- `{cwd}` 取进程 cwd（恒为启动目录，`os.Chdir` 不参与），短路径规则与原 `shortCwd` 一致（`$HOME` 折叠为 `~`、中间路径段截断为首字符）

### 回合视觉分隔（回合末尾方案）

- 每回合结束后、下一个提示符之前打印一行绿色（`style.Ok`）分隔线：`──── 15:04:05`；有模型调用的回合追加 ` · 回合 12.4s`（斜杠命令回合只有时间）
- 非 TTY 旁路：`style.GetProfile().TTY` 为假（管道/重定向）时**分隔线与"输入后留白"都不打印**——Degraded 输入不回显，留白会变成提示符下方凭空一行空行；且管道输出需保持可 diff、可再喂给其他工具。无色但仍是 TTY 时照常打印纯文本分隔线
- 打印时机选在**回合末尾**而非回合开头：分隔线只在本回合确实结束时产生，空输入、`^C` 取消输入（`ErrInterrupt` → `continue`）、`/exit`、`^D` EOF 四条路径都不打印，屏幕不会留下"没有对应输出的孤儿行"；首个提示符之前也不打印（欢迎语即开场）。回合开头方案在物理上无法拦截这四条（分隔线必须先于 `Readline` 打印，而读入前无从判断本回合是否有输出），故不采用
- 空行归一化：分隔线自带前导 `\n`，而工具状态行 / info 行 / 命令输出的末尾都恒为单 `\n`，因此"上一段输出 → 分隔线"之间恒 1 空行；用户提交后到本回合首个事件之间由 `turn.Handle` **懒补** 1 空行（首个事件前打一次，`KindDecor`），零事件回合（如 `Ask` 立即报错）不补，故不会与分隔线的前导换行叠成双空行
- 耗时口径：用户提交 → `Ask` 返回，含本回合全部 LLM 请求与工具执行；`interactive: true` 的 run_shell 期间用户在终端应答的时间也计入（读数偏大属预期）。回合耗时是"提交 → 返回"的汇总层，与 info 行的单次请求耗时（`TTFT/x.xs`）、工具状态行的单工具耗时并列
- 耗时格式：`<1s` 毫秒（`900ms`）、`<1m` 一位小数秒（`12.4s`）、`<1h` `12m34s`、更长 `1h02m`；独立于 `respDuration`（后者服务于 info 行与工具状态行，避免其口径被改动）
- 测试：`repl/turnview_test.go`（格式/颜色/前导尾随换行、耗时档位、`turn` 只补一次空行）；交互路径用 `script` + 延时喂入 pty 手工验证（raw 切换会清掉已缓冲输入，输入须在 raw mode 启用后到达）

### 终端输入（readline 包）

- editor：行编辑/历史，快捷键 Ctrl+A/E/B/F/U/K/W/Y/T/L、Alt+B/F（按空白分词）、Home/End/方向键；render 多行感知（`cursorRow` 精确跟踪光标行，重渲染上移清屏；光标行列由 `layoutCursor` 按终端软换行模型计算——宽字符在行尾放不下时整字换行留空、写满行末的 deferred autowrap，均与终端一致），Size 不可用退化单行；ErrInterrupt 区分 Ctrl+C；Ctrl+L 推屏保历史（一屏减一即 rows-1 个换行——恰把提示符上方内容滚入回滚区、不多滚一行，光标回视口顶部重画提示符，Size 不可用退化 `\x1b[2J` 擦屏），历史保留量受终端 scrollback 容量限制
- Tab 补全菜单：多候选时在输入行下方渲染菜单，选中项反显（`\x1b[7m`）；`↑/↓` 循环选择（菜单打开时不触发历史导航）、`Tab` 循环下一项、`Enter` 仅插入选中项（再次 Enter 提交）、`Esc` 关闭、任意输入关闭菜单正常编辑；单候选直接补全、公共前缀先行扩展的行为不变；候选超 8 行滚动窗口显示
- keys：ESC 序列/控制键/UTF-8 状态机；width：`style` 薄包装（宽度表/ANSI 剥离/感知截断均由 `style` 提供，截断自动复位悬空 SGR 防串色）
- 非 TTY 降级：`Degraded` 按行读取，无动画/菜单
- tty 桥接（`bridge.go` 接口 + `bridge_linux.go` 实现 + `bridge_stub.go` 非 Linux 返回 `ErrUnsupported`）：为 `interactive: true` 的 run_shell 提供"命令在自己的 pty 中运行"的执行器（`TTYBridge.Prepare/Attach`），复用本包 termios 读写与 raw 语义；生产入口 `readline.NewTTYBridge()` 由 `main.go` 经 `agent.WithTTYBridge` 注入到 `agent.New`（构造期定格进 `shellTool.bridge`），测试可在 `shellToolConfig` 里直接给 fake bridge
- 终端状态自愈（`secure.go` + `secure_stub.go`）：`InitTerminalGuard`（启动时记录"自己是否为终端前台作业"，并 `signal.Ignore(SIGTTIN/SIGTTOU)`——`tcsetpgrp` 在前台被抢时需忽略 `SIGTTOU` 才不被停住）+ `SecureTerminal`（恢复 `ISIG`、必要时夺回前台组）
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
| `palette` | 空 | 语义色覆盖（info/warn/ok/error/dim/accent/think/run → 色名），叠加在当前主题之上（切换主题后自动重放） |
| `user_agent` | `pi/0.85.0 (...)` | 出站 UA 伪装 |
| `global_session` | `~/.local/share/tanyan/sessions` | global 模式会话基础目录，支持 `~` 展开 |
| `session_mode` | `auto` | 会话存储模式 auto/local/global |
| `tool_output_lines` | 20 | 工具输出最多显示行数（1-1000） |

env 覆盖：`TANYA_BASE_URL` / `TANYA_API_KEY` / `TANYA_MODEL` / `TANYA_TEMPERATURE` / `TANYA_REASONING_EFFORT` / `TANYA_SESSION_MODE` / `TANYA_THEME` / `TANYA_USER_AGENT` / `TANYA_TOOL_OUTPUT_LINES`。

配色主题：`render/theme` 内置 `Scheme` 聚合（语义色 + 提示符模板 + markdown 样式集），**无全局可变状态**——`Lookup` 取方案、`Apply(sem, palette)` 纯函数叠加覆盖；REPL 持有当前 `Scheme`/`Semantics`，`/theme [name]` 切换后语义色、渲染器与提示符即时重建（palette 重放），readline 通过 `SetStyles` 注入。默认启动主题取 `theme` 配置，校验由 `repl.ValidateTheme` 承担（agent 不依赖表现层）。

## 测试

标准库 `testing` + `httptest` mock LLM（`agent/mock_test.go`，脚本化 `mockStep`，content/arguments 多 chunk 发送以覆盖流式合并）。覆盖 calc/shell/config/SSE 解析/trim/会话往返/Ask 全链路/回调。readline 用 fakeTerm 注入按键，真实终端行为 pty 人工验证。repl 覆盖渲染纯函数与非 TTY 降级。

repl 输出侧测试方法（输出收敛方案阶段 0-4 建立）：① **注入 writer**——`NewStreams(out, err, mode)` 可注入，测试用 `syncBuf`（互斥缓冲，`-race` 安全）与 `writeCounter`（断言"整块一次写完"），不再替换 `os.Stdout`；② **fakeTerm 驱动 Run**——`repl/faketerm_test.go` 实现 `readline.Terminal`，带 `inKey` 标志与 `onKey` 钩子；③ **写权协议断言**——`output.guard` 仅在 `emit`/`atomic` 触发（裸 `Write` 不触发，避免 Editor 合法回显误报），"输入期不得 emit"由正向用例 + 反向对照用例（故意在 `ReadKey` 内写入必被捕获）双保险；④ **可见集矩阵**——`Kind` × 三档模式的可见性以硬编码表锁定；⑤ **golden 字节**——rich 非 TTY 与 TTY 内联重绘各一条基线，plain/plain+verbose 各一条；⑥ **跨提交逐字节回归**——用 `git worktree` 检出上一阶段提交构建旧二进制，同 cwd、同 ldflags 跑同一输入序列（pty 经 `script -qec`），归一化时钟与耗时后 `cmp`，作为"默认行为零变更"的硬证据。pty 目视模板见 `scripts/repl_tty_check.sh`（自动跑前两条，其余人工）。

pty 桥接三层测试：① `readline/bridge_linux_test.go` 自驱动集成（测试自身分配 pty 充当真实 tty，经 `newBridgeTTY` 注入）断言子进程 `/dev/tty` 可读、`tty` 输出为 pty slave、`GPG_TTY` 覆盖、初始尺寸复制、raw 设置与恢复、子进程退出后 master 收到 EIO（防忘关 slave）、Attach 前预置输入不丢、子进程存活时 `stop()` 及时返回；② `agent/shell_bridge_test.go` 用 fake bridge（os.Pipe 造流）断言桥接全流程、Prepare/Attach 失败回退现状路径、非交互不触桥接；③ 真实 tty E2E（gated，用 `script -qec` 驱动真实 /dev/tty，不参与默认 `go test`）：`TTY_BRIDGE_E2E=1`（readline 单命令）、`TTY_E2E=1`（agent 全链路）、`TTY_E2E_REUSE=1`（同进程连续两次交互命令，覆盖 `ownTTY` 打开/恢复/重开复用路径）。

## 环境探针（envprobe）

- 定位：只注入模型无法廉价自探的最小事实集——平台事实与 run_shell 执行契约；工具清单不注入 prompt（function calling 已完整提供），工具版本/分支/目录列表等易变信息模型可按需自探，一律不预注入
- 组装：`runtimePrompt()` = persistPrompt（规则，冻结）+ 空行 + `envSection(cwd, probe)`（实时拼在末尾）；环境注入恒定生效，无配置开关（曾有 `probe` 配置项，review 后移除）
- 输出格式（约 7 行紧凑键值，全部源自 `runtime` 与 `shell.go` 常量，同 cwd 下字节级确定）：

  ```
  # 环境
  OS: linux/amd64
  CWD: ~/Project/tanya
  SHELL: /usr/bin/bash -c（非交互；有控制终端时 run_shell 子进程 stdin 直通 tty，可应答密码/确认）
  TTY: 交互提示须写入 /dev/tty 才可见（stdout/stderr 被工具捕获）
  TIMEOUT: 默认 60s（interactive 时 300s），上限 900s
  OUTPUT: stdout/stderr 头尾各 30KB，中间截断
  WORKSPACE: go.mod, Makefile
  ```

- 事实源单一：SHELL/TIMEOUT/OUTPUT 三行由解析后的 `shellProfile` 与 `shell.go` 常量程序化生成（`invocation()`/`shellTimeoutSec`/`shellInteractiveTimeoutSec`/`shellTimeoutLimit`/`shellMaxOutput`），TTY 行为固定契约文案，无第二份硬编码描述；四行恒定输出（shell 缺失时进程已在启动阶段退出）
- 平台条件：`TTY:` 行与 SHELL 行的 tty 直通说明仅在 `ttyStdinSupported()` 为真（unix 且非 illumos/ios）时输出，其余平台 SHELL 行退化为 `（非交互）`，不宣称不存在的 /dev/tty 能力
- 探测机制：`envSection` 为纯函数，WORKSPACE 标记文件（`os.Stat`，8 种标志文件固定顺序）经注入的 `envProbeFunc` 取得；shell 契约由入参 `*shellProfile` 注入（`shellTool.profile`，SHELL 行取 `profile.invocation()`），不再读包级状态；主路径零 exec、零易变信息
- 可测性：分层测试——persistPrompt 只含规则 / envSection 注入 fake probe 断言渲染 / runtimePrompt 拼接（probe 为 nil 时退化） / 同参数两次渲染字节相等
