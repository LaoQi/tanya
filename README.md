# tanya

极简命令行 AI Agent，Go 编写，无 GUI/WebUI/TUI。单二进制，交互式 REPL + 单发模式。

## 特性

- OpenAI 兼容接口（OpenAI / DeepSeek / GLM / Ollama / vLLM 等），SSE 流式输出
- 以 shell 为核心的工具体系：模型可直接执行 shell 命令（自动适配平台：Linux/macOS bash/sh/ash，Windows pwsh/powershell；`shell` 配置可指定任意 shell）；命令前后保存/复原控制终端状态（复原 termios + 在自己是前台时归位光标锚点并复位屏幕模式：SGR、字符集、滚动区/换行/光标/鼠标等；交出终端前先 `DECSC` 存锚点、复位后 `DECRC` 归位，子进程设滚动区或挪走光标都不会把后续输出带到屏幕顶部），被超时强杀的交互程序不会留下坏终端
- 内置轻量工具：`get_time` / `get_env` / `calc`
- 模型可运行时自调与自省：`agent_custom` 按 `key` 读写（可写 `model`、`reasoning_effort`；只读 `models`、`usage`、`stat`、`sessions`、`config_path`），`get sessions` 给出会话列表与 jsonl 文件路径（仅本次会话有效，不写配置文件），`get config_path` 给出生效配置文件路径（模型据此可读取或修改自身配置，改动需重启生效）
- 会话持久化与恢复（JSONL，记录完整历史，system 快照随会话冻结）
- token 用量实时显示在提示符（API 实报优先，本地估算兜底），支持显示缓存命中
- AI 输出 Markdown 渲染（默认开启，stdout 非终端与 plain 输出自动旁路；表格带外框与列对齐）与内置配色主题（`/theme` 切换）
- 思维链显示（`show_reasoning` 配置或 REPL 内 `/reasoning on`）：思维链以 markdown 渲染并夹在 `─── 思考 ───` / `─── 思考结束 · 3.2s ───` 分隔符之间，同时不再打 `» 思考中` 状态行
- 注意力通知（默认全关，三项独立开关）：`bell` 响一声、`notify_osc` 写终端原生 OSC 9 通知、`notify_cmd` 调外部程序（`notify-send`/`osascript`/自写脚本）——触发点为对话回合结束（成功或报错，`^C` 中断不响）与 `run_shell` 声明 `interactive`、等待终端输入时；一律尽力而为（失败静默、不保证终端或桌面真的响应），且不走 stdout，仅 REPL 交互富档生效，`ask` 单发不参与
- AGENTS.md 项目说明自动注入系统提示（全局 + 工作区双层，会话级快照保证 prompt cache 友好）
- 平台：Linux 与 Windows 为主（Windows 显示与行编辑均已支持：16 色、状态行、markdown、真实宽度、行编辑/历史/Tab 补全/ghost；interactive 命令走控制台继承直通，Windows 侧仅部分实机验证、未全量覆盖，暂不跟踪），macOS 尽力；控制终端原语与终端探测统一在零依赖叶子包 `ctty`，其余平台仅保证可编译
- 降级粒度独立：显示能力取决于 stdout 是否终端、输入能力取决于 stdin 是否终端，互不连带（支持范围与组合矩阵见 `docs/terminal-caps.md`）
- 依赖仅 2 个，核心逻辑测试覆盖率 90%+

## 安装

```bash
go install github.com/LaoQi/tanya@latest
```

或从源码构建：

```bash
git clone https://github.com/LaoQi/tanya.git && cd tanya
make build
```

`make build` 经 ldflags 注入版本号（`git describe`）与构建时间：欢迎屏显示 `tanya <版本>（构建于 <时间>）`，`-v` 显示版本号。直接 `go build` 时版本为 `dev`、不显示构建时间。

## 配置

`~/.config/tanya/config.yaml`（完整示例见 `config.example.yaml`，也可直接 `tanya config > ~/.config/tanya/config.yaml` 落一份带注释的默认配置）：

```yaml
base_url: https://api.deepseek.com/v1
api_key: "sk-..."
model: deepseek-v4-flash
```

环境变量 `TANYA_*` 可覆盖配置文件：`TANYA_BASE_URL` / `TANYA_API_KEY` / `TANYA_MODEL` / `TANYA_TEMPERATURE` / `TANYA_REASONING_EFFORT` / `TANYA_API_PROTOCOL` / `TANYA_DATA_DIR` / `TANYA_SESSION_MODE` / `TANYA_USER_AGENT` / `TANYA_TOOL_OUTPUT_LINES` / `TANYA_SHELL` / `TANYA_THEME`。

`api_protocol` 配置项（env `TANYA_API_PROTOCOL`）选择 API 协议：`responses`（默认，OpenAI Responses API 兼容格式，思维链明文回传）或 `chat`（Chat Completions 兼容协议）。端点路径为 `/responses` 时用 `responses`；仅提供 `/chat/completions` 的端点遇 404 时请切换为 `chat`。

`shell` 配置项（env `TANYA_SHELL`）指定 run_shell 使用的 shell，支持名字或绝对路径（如 `zsh`、`/usr/bin/fish`）；缺省自动探测：Windows 依次尝试 pwsh → powershell（Windows PowerShell 5.1 兜底），Linux/macOS 依次尝试 bash → sh → ash。全部落空（含配置的 shell 不存在）时启动阶段直接报错退出，不进入 REPL。

`reasoning_effort` 配置项（env `TANYA_REASONING_EFFORT`）设置思考等级，随请求发送 OpenAI 标准字段（o 系 / gpt-5 及兼容网关支持），可选 `minimal` / `low` / `medium` / `high` / `max`，留空不发送；REPL 内 `/think` 可运行时切换。`responses` 协议下映射为 `reasoning.effort`，`chat` 协议下为 `reasoning_effort`。设置思考等级后请求不再发送 `temperature`（两协议一致），以兼容 o 系 / gpt-5 等仅支持 `temperature=1` 的推理模型。

`show_reasoning` 配置项（仅 yaml，默认 `false`）让思维链随对话显示：思维链以与正文一致的 markdown 渲染呈现在 `─── 思考 ───` 与 `─── 思考结束 · 3.2s ───` 两条分隔符之间（`Think` 语义色，时长取该段思考耗时），同时不再打印 `» 思考中` 状态行——`» 等待响应` 心跳也在首个思维链片段到达时收尾。仅 REPL 的 rich 输出档生效（stdout 非终端、`-p`、`-p --verbose`、`ask` 一律不显示），REPL 内 `/reasoning on|off` 可运行时切换；门禁外 `/reasoning on` 会提示「当前输出档不显示思维链」（开关记忆仍保留，切回富档即生效）。

`bell` 配置项（仅 yaml，默认 `false`）在需要把人叫回终端时发声：对话回合结束（成功与报错都响，`^C` 中断不响）与 `run_shell` 声明 `interactive`、终端即将移交时各响一声，响声写入控制终端（`/dev/tty`），因此不进 stdout、不受 `-p` 与重定向影响。仅 REPL 的 rich 输出档生效（stdout 非终端、`-p`、`ask` 一律不响）。提示音时点是「工具开始执行」而非「子进程真的在等输入」，且是否真能听见取决于终端设置（部分终端配为静音或闪烁）。设计见 `docs/design.md`《终端通知》。

`notify_osc` 配置项（仅 yaml，默认 `false`）把通知发给终端本身：写一帧 `ESC ] 9 ; 文本 BEL` 到控制终端，由终端决定怎么呈现（iTerm2 / WezTerm / Ghostty / Windows Terminal 系支持，弹系统通知或角标；Terminal.app 与传统 xterm 系不认，写了就是没有效果）。适合终端在别的桌面/分屏、人不在跟前的场景。与 `bell` 同一门禁、同一写入通道（`/dev/tty`），因此同样不进 stdout、不受重定向影响；tmux/screen 下未做透传、通常会被吞掉。无 env、无 REPL 命令。

`notify_cmd` 配置项（仅 yaml，默认空即关闭）调用外部程序发通知，适合要弹桌面通知中心、发到手机、放自定义音效的场合：

```yaml
notify_cmd: notify-send -a tanya {title} {content}          # Linux
notify_cmd: osascript -e 'display notification "{content}" with title "{title}"'   # macOS
notify_cmd: /path/to/hook {kind} {title} {content}          # 自写脚本，按类别分流
```

命令交给与 `run_shell` 同一个 shell 执行（`shell` 配置或平台探测的结果），所以管道、重定向、多命令都可用。可用的占位符只有三个：`{title}`（固定 `tanya`）、`{content}`（已处理好的单行文本：`回合结束 · 12.3s` / `回合失败 · 12.3s` / `run_shell 等待输入`，剥掉控制序列、压成一行、超长截断）、`{kind}`（`done` / `failed` / `input`，便于脚本分别处理成功与失败）。**占位符自带引号，配置里直接写 `{content}` 即可，不要再自己加引号**——`"{content}"` 会多出一层字面引号（启动时会报错提示）；写错占位符、花括号不配对同样在启动时报错退出。**`notify_cmd` 里的花括号是保留语法**：命令中出现的任何 `{...}` 都必须正好是这三个占位符，因此 `${VAR}`、awk 的 `'{print $1}'`、brace expansion 这类含花括号的 shell 写法会被启动校验拒绝——复杂逻辑放进外部脚本，配置里只写脚本路径。

外部程序异步执行，不阻塞对话；上一次还没结束就丢弃本次通知（避免堆积），3 秒超时后强制结束，stdout/stderr 一律丢弃（不会污染对话界面），失败静默——程序不存在、没有 D-Bus、没有通知权限都只是"没有效果"。与 `notify_osc` 可以同时开启（各发一份）。设计见 `docs/design.md`《终端通知》。

`theme` 配置项（env `TANYA_THEME`）选择内置配色主题，REPL 内 `/theme` 可运行时切换；`colors`（auto/on/off）控制是否着色；`palette` 可覆盖单个语义色。可用主题与色名见 `config.example.yaml`。

`responses` 协议以思维链回传为核心特性（参照 DeepSeek Responses API 标准，OpenAI 兼容但不完整遵守 OpenAI）：响应中的 reasoning item 的明文思维链 `content` 随会话保存并在后续请求中原样回传，保持多轮工具调用间推理链完整；请求固定 `store: false`，不携带 `include`/`encrypted_content` 等 OpenAI 特有字段。回传内容须逐字节一致（不截断、不改写），以保证 DeepSeek 前缀缓存命中。`chat` 协议下同一能力经 `delta.reasoning_content` 捕获、随会话落盘，并在后续请求中折叠为 assistant 消息的顶层 `reasoning_content` 字段回传（DeepSeek 思考模式携带 `tools` 时官方要求历史推理链完整回传）。

## 使用

```bash
tanya                 # 交互 REPL
tanya ask "问题"      # 单发模式（默认纯文本+verbose：无状态行心跳/无光标控制，保留工具块与状态行）
tanya init            # 新工作区脚手架：建 .tanya/ 与 AGENTS.md 骨架，随后进入 REPL
tanya config          # 输出内置默认配置示例（含注释），可重定向为配置文件
tanya -n              # 只读会话：可载入历史，不写入
tanya -n ask "问题"   # 单发且不写入会话历史
tanya -c x.yaml       # 指定配置文件
tanya -m local        # 会话存到当前目录 .tanya/
tanya -p              # 纯文本输出：无颜色/状态行/工具块，stdout 只留答案与命令反馈
tanya -p --verbose    # 纯文本输出但保留工具块与状态行（仍无颜色与光标控制）
tanya -v              # 显示版本号
```

会话存储模式（`-m` 参数 / 配置项 `session_mode` / env `TANYA_SESSION_MODE`，优先级从高到低）：

| 模式 | 会话目录 |
|---|---|
| `auto`（默认） | 当前目录存在 `.tanya/` 则用 `<cwd>/.tanya/sessions/`，否则用全局（`tanya init` 建出 `.tanya/` 后即落本地） |
| `local` | `<启动目录>/.tanya/sessions/` |
| `global` | `<data_dir>/workspaces/<workspace-id>/sessions/`（`data_dir` 默认 `~/.local/share/tanya`） |

`data_dir` 配置项（env `TANYA_DATA_DIR`，默认 `~/.local/share/tanya`，支持 `~` 展开）是 global 模式的数据根：每个启动目录在其下 `workspaces/<workspace-id>/` 有一个工作区目录，内含并列的 `sessions/`（活动会话）与 `archive/`（归档卷）。**旧布局不兼容**：`global_session` 配置项与 `<global_session>/<workspace-id>/` 目录形态已废弃，旧目录（如 `~/.local/share/tanya/sessions/`）需手工删除。

只读会话（`-n` / `--no-save`，只由命令行开启，配置文件与 env 均无法设置）：`ask` 单发与 REPL 通用。历史会话照常列出与载入，之后的对话只存在于内存、不写入会话文件，也不创建会话目录（REPL 启动时在欢迎屏下方显示黄色警告，`ask` 保持静默）。

新工作区初始化（`tanya init`，无参数）：为空白目录搭好工作区骨架后进入普通 REPL——建 `<cwd>/.tanya/sessions/`（使 `session_mode: auto` 落到本地工作区，首个会话即写入此处）、建 `<cwd>/AGENTS.md` 骨架（已存在则不动），并询问是否为 `.tanya/` 建立忽略文件（内容 `*`，避免会话与历史入库；非交互场合不询问也不创建，输出里给出手动命令）。不做项目探测、不调用模型；每一项幂等、不覆盖既有文件，任一项失败即以 1 退出。生成的 `AGENTS.md` 会进入本次会话的 system prompt，可随后让 AI 读完目录补全。

默认配置输出（`tanya config`，无参数）：把编译期嵌在二进制里的 `config.example.yaml` 原样写到 stdout（逐字节等于仓库里那份，含全部可配项与中文注释，无任何额外提示行），可直接 `tanya config > ~/.config/tanya/config.yaml`。不读配置文件、不探测终端，配置文件损坏或缺失、无控制终端、被重定向时同样可用；带多余参数以 1 退出。

纯文本输出（`-p` / `--plain`，只由命令行开启，配置文件与 env 均无法设置）：`ask` 单发与 REPL 通用，供本程序作为子 agent 被调用时拿到可解析的输出——stdout 只承载 assistant 正文与命令反馈（无颜色、无状态行与心跳、无光标控制、无 markdown 装饰、无工具块与状态行），stderr 承载错误与诊断；`ask` 结束时若正文已以换行结尾则不再补空行，stdout 严格等于答案。`--verbose` 必须与 `--plain` 同用，作用是在该模式下恢复工具块与状态行的**纯文本**形态（仍不启用颜色与光标控制；工具块为追加式，标题只在开始行出现一次）。

`ask` 单发默认即 plain+verbose 档（`repl.SingleShot` 把 rich 降到该档）：无状态行心跳与光标控制，工具块以追加式纯文本呈现（标题只在开始行出现一次），正文原样直出（不渲染 markdown），颜色仍按终端能力保留；显式 `-p` 可进一步压成纯答案（stdout 严格等于答案），REPL 默认档位不受影响。

REPL 输入按前缀分发：

| 输入 | 行为 |
|---|---|
| `内容` / `:内容`（全角 `：` 亦可） | 与 AI 对话（两种写法等价） |
| `/命令` | 斜杠命令（见下表）；未命中的 `/` 开头输入按对话内容处理 |
| `exit` / `quit` | 退出（等价 `/exit`） |

退出（`exit`/`quit`/`/exit`/`/quit`、Ctrl-D 或终端挂断）时打印本次运行的收尾三行：会话 id · 运行时长 · 消息条数、累计用量与缓存命中率、已落盘的会话文件路径（本次没产生对话则不落盘，末行省略；`-n` 只读模式末行改为「会话文件 未写入（不落盘模式）」）。收尾块属装饰输出，`-p` 与 `-p --verbose` 下不打印。

直通 shell 执行面已归档（恢复步骤见 `docs/design.md`）：agent 需要执行命令时经 `run_shell` 工具完成（输出截断与超时策略见其工具说明）。进程 cwd 恒为 tanya 启动目录、全程不变；`run_shell` 默认在**当前工作区**（启动时即启动目录，`/switch` 后可换）执行，也可用 `cwd` 参数为单次命令指定其它目录（不影响后续调用）。

REPL 斜杠命令：

| 命令 | 说明 |
|---|---|
| `/help` | 帮助 |
| `/new` | 开启新会话（当前会话自动保存） |
| `/switch <dir>` | 切换工作区（放弃当前会话，重读 AGENTS.md；`~`/相对路径可用，必须是已存在目录；Tab/ghost 补全目录，含空白的目录名不补） |
| `/load [id]` | 无参打开会话选择菜单；带 id 直接载入 |
| `/archive [n\|时长]` | 归档历史会话（先出报告、`y` 确认才落卷；无参 = 保留 `auto_archive_keep` 个，纯数字 = 保留 n 个（`0` = 除当前会话外全部），`7d`/`12h` = 按未活动时长） |
| `/fork` | 以当前上下文另开新会话（继承历史、立即落盘；原会话保留，可 `/load` 回切） |
| `/stat` | 查看会话统计（工作区、token 用量、缓存） |
| `/history [n\|all]` | 无参截断列表；n 全量查看单条；all 全量显示 |
| `/model [name]` | 查看/切换模型 |
| `/think [level]` | 查看/设置思考等级（`off` 关闭） |
| `/reasoning [on\|off]` | 查看/切换思维链显示（开启后以 markdown 渲染并夹分隔符） |
| `/theme [name]` | 查看/切换配色主题 |
| `/exit`（`/quit`、`exit`、`quit`） | 退出 |

会话按工作区划分（global 模式按启动目录，`auto` 模式下有 `.tanya/` 的目录落本地），`/load` 的会话选择菜单只显示当前工作区的会话。`/switch <dir>` 可换工作区（`/switch` 无参看当前值与用法）：切换即**放弃当前会话**（不 fork、不询问，旧会话文件保持原样），随后按新目录重建——`run_shell` 默认目录、system 提示里的项目 `AGENTS.md`、会话落点（`auto` 依新目录是否含 `.tanya/` 判定）、环境段 `CWD:` 行；提示符 `{cwd}` 与 `/stat` 工作区行随之变化，进程 cwd 不变（`os.Chdir` 全程不参与）。目标目录不存在、不是目录，或与当前工作区相同（含 `sub/` 这类等价写法）都会被拒绝且不改动任何状态。归档只读会话下也能切换（切走即离开只读）。

### 会话归档与 fork

`/archive` 把历史会话打包成标准 zip 卷（落 `<workspace>/archive/archive-<时间戳>.zip`），把活动区清出来；**归档只做无损压缩**：卷内 entry 名 `<会话 id>.jsonl`、数据是原 jsonl 逐字节（不裁剪、不重排，思维链与工具调用原样保留），entry 注释与卷注释带条数、首条 user 摘要与来源目录，因此列会话不用解压（只读 zip 中央目录）。一次 `/archive` 生成一卷、写入后不可变，也不提供「解档回活动区」。

- 归档筛选：无参按 `auto_archive_keep`（默认 16）保留最近 N 个、其余入选；`/archive 20` 指定保留数量（`0` = 除当前会话外全部）；`/archive 7d`、`/archive 12h` 按未活动时长筛选（单段单单位 `d`/`h`/`m`/`s`，复合时长如 `12h30m` 不接受）；恒排除当前会话，5 分钟内动过的会话跳过（防另一实例正在追加），已在卷内的 id 跳过（幂等）
- 二次确认：`/archive` 一律先出预览（`当前活跃会话 A 个；将归档 N 个（约 X[，保留最近 K 个]）。`，附跳过/失败行）再问 `现在归档？[y/N]`，`y`/`yes` 才落卷、其它输入取消；命令只在完整交互环境（rich 输出且 stdin/stdout 都是终端）可用，`-p`/ask/管道/非终端只提示不可用，没有跳过确认的开关
- `/load` 的候选里归档项带 `[归档] ` 标记；载入归档会话为**只读**：可查看 `/history`、`/stat`，但对话被拒（提示先 `/fork`），原文件不会重建、卷不会被改写
- `/fork` 与 `/new` 的分工：`/new` 清空历史重开，`/fork` 带着当前上下文另开——任何会话都能用（新 id、当前 AGENTS.md 快照、历史原样继承），立即落盘、之后按普通会话增量追加；**原会话文件不动**，可随时 `/load` 回切（分支语义）。归档只读态下它同时解除只读；`-n` 下同样可 fork，只是不落盘
- 卷用 `unzip -l/-p/-z` 即可浏览与提取（`unzip -z` 打印注释时可能因本地码页转换显示乱码，不影响数据）
- 只读（`-n`）与归档只读是两回事：`-n` 允许继续对话（只存内存），归档只读则必须 `/fork` 才有落点

### 启动自动归档

启动自动归档默认开启（`auto_archive_threshold` 默认 64、`auto_archive_keep` 默认 16；写 `auto_archive: false` 关闭）：REPL 启动时若当前工作区活跃会话数达到阈值，会在欢迎屏后询问：

```
当前活跃会话 68 个；将归档 52 个（约 12.3M，保留最近 16 个）。
现在归档？[y/N]
```

输入 `y`/`yes` 立即归档（只保留最近 `auto_archive_keep` 个，其余打包成一卷），其它输入显示 `已取消，未归档`。这条提示与 `/archive` 无参走的是同一条线路：同样的预览口径（活跃总数、将归档几个、多大体积、保留几个）与同样的确认读法，区别只在触发条件（`auto_archive` + 阈值）。跳过不做记忆，下次启动仍会问，想彻底关掉提示设 `auto_archive: false`。

只走配置文件，没有对应环境变量；仅 REPL 启动会问——`ask` 单发、`-p`、stdout 非终端、无控制终端、`-n`（不落盘）一律不提示（纯文本模式、stdout 非终端与拿不到控制终端都在读提示之前静默返回，不会把提示写进管道、也不会阻塞在 `/dev/tty` 上）。阈值需 `>= 2`，保留数需 `>= 0` 且小于阈值（`0` = 除当前会话外全部归档），越界会在启动时报错。

## AGENTS.md 注入

系统提示 = 内置默认提示（原文在仓库根 `system_prompt.md`，编译期嵌入、改后需重新编译；不可配）+ 全局说明 + 项目说明，后两者来自：

| 层级 | 路径 | 标题 |
|---|---|---|
| 全局 | `~/.config/tanya/AGENTS.md` | `# 全局说明（~/.config/tanya/AGENTS.md）` |
| 工作区 | `<启动目录>/AGENTS.md` | `# 项目说明（AGENTS.md）` |

- 文件不存在或内容为空白则跳过该层；组装在会话开始（`/new`、`/load`、启动）时刻快照，会话进行中不再读取文件
- system prompt 末尾追加环境段（OS/CWD/shell 执行契约），在 agent 构建时算一次并定格、会话期间不变，不进快照、不持久化，模型可据此构造 `run_shell` 命令
- history 全程追加，同一会话内请求前缀逐字节不变，prompt cache（DeepSeek/OpenAI）逐轮命中；该保证只覆盖**同一个二进制**——升级 tanya 后默认提示词或工具描述变化，`/load` 旧会话首轮会 cache miss（会话照常可用、`/fork` 不受影响），属预期行为，不构成限制改动的理由
- 提示符模板内置固定（不可配），占位符：`{cwd}` 短路径 / `{model}` 模型 / `{effort}` 思考等级（未设置渲染为空）/ 用量与命中率按「单次请求」与「本次运行累计」两组提供，**加 `_total` 后缀即累计**：`{usage}` 上下文用量（如 `12.3k`）/ `{cache}` 本次命中量 / `{cache_rate}` 本次命中率（如 `81.67%`）/ `{usage_total}` 累计用量 / `{cache_total}` 累计命中量（如 `1.6k`）/ `{cache_rate_total}` 累计命中率（如 `65.83%`）/ `{usage_summary}` 组合显示（混合口径：前段单次用量、后段累计命中率）——`{usage}` 其后在有累计缓存数据时追加 `{cache_rate_total}`（如 `12.3k 81.67%`），独立占位符无数据均渲染为空；`/stat` 固定显示累计口径，单次请求的命中率另在响应回显行给出

## 工具

工具执行以块状格式显示（stdout 为终端时整块暗灰 `\x1b[90m` 与主输出区分，否则纯文本）：标题行 + 缩进输出行（stderr 行加 `2|` 前缀）+ 亮蓝状态行 `↳ exit 0 · 0.3s · 12 行`（退出码/超时/中断 · 耗时 · 输出行数），默认最多显示 20 行（`tool_output_lines` 可配，1-1000），超出仅保留头 3 行 + 尾 2 行（状态行显示 `共 N 行`）；执行开始即打印标题行（无进行中标记，标题只出现一次，结束时追加正文块与 `↳` 状态行）。

**标题区显示调用参数**：`run_shell` 的命令短到能与工具名同行时内联一行（`▸ run_shell ls -la`）；放不下或原本多行（heredoc、多行脚本、长管道）时转块形态——首行只有工具名，其后是显式 `cwd` 行（`  cwd: ~/proj`，不缩写）、`timeout` 行（`  timeout: 90s`，仅显式指定时）与**按显示宽度折行的命令区**（每行 `  $ ` 前缀，与原输出区的 `  ` / `  2| ` 缩进区分），制表符按 4 空格摊平、空行与缩进照原样保留；行数上限 8 行，超出保留头 6 行 + 尾 1 行并提示 `… 省略 N 行（完整命令见 /history）`。

其余工具（`get_env`、`calc`、`agent_custom` 等）同样显示参数：内联形如 `▸ calc expression: (1+2)*3/4`、`▸ agent_custom action: set · key: model · value: gpt-5`，多参数以 ` · ` 连接，放不下或多行时转块形态逐项一行（`  key: value`）；数组值逗号连接（`names: HOME, PATH`）、对象压成紧凑 JSON、参数为空只显示工具名。通用参数的行数上限与省略提示同上，提示文案为 `… 省略 N 行（完整参数见 /history）`。折行只切分不改写内容、不做词级折行，因此参数一定能看全，且任何一行都不超过终端宽度。

TTY 下的状态展示是**追加式**（不重绘、不移动光标，终端被交互命令占用时也不会擦掉对方输出）：LLM 请求等待期间打印 `» 等待响应 0s`，随后**每秒往当前行追加一个点（与行首同色）**，满 10 个点换行并以当时秒数开新行（如 `» 等待响应 10s ..........`，秒数与点之间留一个空格位）；思维链 delta 到达时切换到 `» 思考中`（同色系语义色 `Think`，秒数继续累计，只多开一行）；工具执行期间同样以 `» 执行中 0s` 起行（`interactive: true` 与交互桥接不计时）。行首的秒数在开行时写定，之后不再改写。每轮请求完成打印状态行 `  ↳ TTFT 0.8s · 3.2s · prompt 12.3k · completion 1.2k · 缓存 81.67%`（无 usage 时显示本地估算上下文，字段缺失自动省略）。stdout 非终端、`-p` 与 `-p --verbose` 下状态行与心跳均不输出。打开思维链显示（`show_reasoning` 或 `/reasoning on`）后不再出现 `» 思考中` 相位：思维链本身以 markdown 渲染，夹在 `─── 思考 ───` 与 `─── 思考结束 · 3.2s ───` 之间，正文紧随下分隔符之后。

所有工具免确认执行：

| 工具 | 说明 |
|---|---|
| `run_shell` | shell 执行命令（按平台自动选择）；`cwd` 指定执行目录（默认当前工作区，不存在则快速失败）；`timeout` 默认 60s、`interactive: true` 时 300s，上限 900s；输出截断 30000 字节 |
| `get_time` | 当前时间 |
| `get_env` | 环境变量查询（敏感变量名拒绝） |
| `calc` | 四则运算求值 |
| `agent_custom` | 按 `key` 读取或修改：可写 `model`、`reasoning_effort`（`set` 需给 `value`）；只读 `models`（可用模型）、`usage`（上下文/缓存）、`stat`（会话统计）、`sessions`（会话列表 + jsonl 路径）、`config_path`（生效配置文件路径，改动需重启生效）。改动对下一次请求生效，仅本次会话有效 |

### 终端与信号行为

普通命令执行期间 shell 进程被移交终端前台进程组（`TIOCSPGRP`，无控制终端时自动跳过），因此 ssh/git 等需要密码的程序可直接在终端应答，不再挂死至超时。

`interactive: true` 在 Linux 走独立 pty 桥接：命令在自己的 pty 中运行，真实 tty 切 raw 由 bridge 双向泵转，提示与输出实时可见；此时无需前台移交，`^C` 经 pty 行规程投递（`^Z` 在桥接下不挂起子进程）。桥接不可用时回退上述前台移交路径。Windows 不走 pty：子进程 stdin 继承控制台（`CONIN$`）可直接应答，运行期 tanya 屏蔽自身 `^C`（`Ctrl+Break` 仍可中断），interactive 时去除 PowerShell 的 `-NonInteractive`（Read-Host 可用）；提示写到控制台的程序（ssh 等）实时可见，写到 stdout 的随输出捕获。细节见 `docs/interactive-tty.md` 与 `docs/terminal-caps.md` §8.6。

信号语义：

- 执行期间 Ctrl+C 直接送达命令进程组（命令可优雅退出）；再次按下 Ctrl+C 取消当前回合
- 普通路径下 Ctrl+Z 会挂起命令进程，tanya 检测到后立即终止并标注 `挂起已终止`，无需等超时
- 命令间隙/流式阶段 Ctrl+Z 被 tanya 忽略（不会挂起自身），Ctrl+\ 保持 Go 默认行为（全栈转储）
- 用户脚本内故意 `kill -STOP` 长挂起的进程会被同一机制终止


## 开发

```bash
go build ./...
go vet ./...
go test ./...
go test -race ./...
```

设计文档见 `docs/design.md`。
