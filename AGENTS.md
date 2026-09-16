# tanya

极简命令行 AI Agent，Go 编写，无 GUI/WebUI。模块路径 `github.com/LaoQi/tanya`。

## 设计约束

- 极简优先：依赖仅 `gopkg.in/yaml.v3` 与 `golang.org/x/sys`（unix 做 termios/pty，windows 做控制台探测），新增依赖需先讨论
- package 划分：`main`（仅入口）、`repl`（REPL/补全/渲染）、`agent`（核心逻辑）、`readline`（自研终端输入层）、`render`（表现层树：样式词汇/终端原语/配色/IR/渲染/解析）、`ctty`（控制终端原语与终端探测，零依赖叶子）；根目录只放 main.go 与顶级包
- 终端输入层自研（raw mode + ANSI 渲染），fish 风格 ghost 置灰建议，不引入 TUI 框架；Windows 仅支持 Windows Terminal（`terminal_windows.go` 占位未实现），不支持 cmd/老 conhost
- 平台分片一律白名单：`linux`/`darwin`/`windows` 各一个装配文件，posix 共享实现落在 `linux || darwin` 文件，其余平台 stub，不枚举边缘平台；目标平台 Linux/Windows 为主（Windows 交互待实现）、macOS 尽力、其余仅保证可编译
- 中断依赖两项终端不变量（`ISIG` 开启、前台组是 tanya），自愈时机与信号退出码口径（`128 + signum`）见 `docs/ctty.md`；`/dev/tty`、前台组、termios 读写与模式复位、终端探测（isatty/尺寸/VT）等原语一律走 `ctty`，是否移交/夺回/复原由 `agent`、`readline` 各自决定：`run_shell` 移交终端前快照 termios、子进程结束后（含超时强杀）复原 termios，并在自己仍是终端前台（`ctty.IsForeground`）时发屏幕模式复位（`ctty.ResetModes`，前台路径与 pty 桥接 release 都发）：交出终端前先 `ctty.SaveCursor` 存光标锚点、复位后再 `ctty.RestoreCursor` 归位，复位串内不得自包 `DECSC`/`DECRC`（会覆盖该存档槽）——子进程写 `DECSTBM` 会把光标直接 home 到绝对 (1,1)，锚点只能取自子进程之前，否则后续输出从屏幕顶部开始画，自愈侧把被外部留成 raw 的终端拉回 canonical
- `interactive: true` 的 run_shell 走全 pty 桥接（命令在独立 pty 中运行，真实 tty 由 bridge 切 raw 双向泵转）；仅 Linux 实现，失败回退 `/dev/tty` + `TIOCSPGRP`，见 `docs/interactive-tty.md`
- 工具只有编译期显式清单 `allTools()`（`run_shell` + `builtinTools()` + `agent_custom`），不做动态注册/插件；清单顺序即请求顺序，改动会破坏 prompt cache
- 模型可经 `agent_custom` 运行时自调与自省：形态为 `action`(get/set) + `key` + `value`，可写 key `model`/`reasoning_effort`，只读 key `models`/`usage`/`stat`/`sessions`（`get sessions` 返回会话列表 + jsonl 绝对路径，内容由 run_shell 读）；实现 key 表驱动。只写内存、不落盘不入会话，`/load` 或重启后回落配置文件值
- 工具策略：以 `run_shell` 为核心，新能力优先用 shell 命令组合实现；小型纯计算/查询工具放 `builtin.go`
- 启动即要求可用 shell：`agent.New` 解析（配置覆盖 > 平台探测）全落空直接报错退出，无降级路径
- `init` 子命令是新工作区的一次性脚手架（建 `.tanya/sessions/`、按确认建 `.tanya/.gitignore`、缺口时建 AGENTS.md 骨架），之后与普通模式无二；三项动作幂等且不覆盖既有文件，**必须在 `agent.New` 之前执行**——`.tanya/` 既是会话落点也是 `session_mode: auto` 的判定依据，先建后 New 才能让首个会话落本地工作区；探测/询问/渲染分别在 `agent/init.go`（数据）与 `repl/initflow.go`（UI），不做项目探测、不调模型
- REPL 输入分发（`repl/dispatch.go`）：`/` 白名单斜杠命令（控制面）、`exit`/`quit` 内建退出、`:`/`：` 等价显式对话前缀、其余直接与 LLM 对话；进程 cwd 恒为启动目录（全程不 `os.Chdir`），`run_shell` 默认在此执行并可用 `cwd` 参数为单次命令指定目录
- LLM 协议双通道 `api_protocol`（yaml/env `TANYA_API_PROTOCOL`，默认 `responses`，非法值启动报错）：`responses` 走 `/responses`（reasoning 明文捕获/原样回传、固定 `store: false`）；`chat` 走 `/chat/completions`（思维链经 `delta.reasoning_content` 捕获，请求时折叠为 assistant 顶层 `reasoning_content` 回传，wire 上不出现 `reasoning_items`）
- 思考等级只用标准字段 `reasoning_effort`（minimal/low/medium/high/max，yaml/env `/think` 三处可配），不用厂商私有参数；设置后两协议均不发 `temperature`
- 出站请求 UA 伪装（避免厂商风控）：默认 `pi/0.85.0 (linux; node/v22.14.0; x64)`，yaml `user_agent` / env `TANYA_USER_AGENT` 可配
- 代码不添加注释，除非用户明确要求
- 颜色一律用终端 16 色基本 SGR 码（30-37/90-97），不用 256 色/truecolor

## 结构

```
main.go            入口、flag 子命令、ask 单发、init 工作区脚手架
ctty/              控制终端原语与终端探测：前台组读/写、/dev/tty、SIGTTIN/SIGTTOU 忽略、能力常量 Supported、Facts 探测（isatty/尺寸/VT）；白名单 + stub，零内部依赖
repl/              REPL 循环与输入分发（对话优先）、斜杠命令、提示符模板、ghost 补全、/load picker、工具块渲染、状态行心跳、统计渲染、UI 文案
readline/          自研终端输入层：行编辑/历史/Tab 补全菜单、按键解析（keySource 平台无关，posix termios / windows 控制台 VT 输入）、raw mode、显示宽度、pty 桥接（linux）、终端状态自愈
agent/             核心逻辑：config（配置加载）/ llm + llm_http + llm_responses（双协议 client）/ agent（对话 loop）/ prompt / session / stats / envprobe / init（工作区脚手架）/ event
                   工具与终端：tools（Tool 接口 + allTools 清单）/ shelltool（run_shell 组件）/ shell（叶子）+ shell_platform{,_posix,_linux,_darwin,_windows,_stub}（平台抽象与分片）/ builtin（内置小工具）/ control（agent_custom 自调工具，key 表驱动）/ tty_bridge
render/            表现层树根：渲染管线（IR → ANSI 的 Renderer、提示符模板 Template）
  style/           样式词汇与编码（Color/Attr/Style/ColorLevel/SGR/Sprint/Frame），依赖 term
  term/            终端原语（ANSI 词法/清洗、宽度/截断/单行化、光标控制、能力档案 Profile），零依赖叶子
  ir/              渲染 IR（Block/Inline 值类型），依赖 style
  theme/           配色（语义色集合 Semantics、方案 Scheme、markdown 样式集、palette、内置主题表），依赖 style
  markdown/        流式 markdown 解析（MarkdownBuf/ParseInline），依赖 ir/style/term
  markup/          内联标记解析（ParseMarkup），依赖 ir/style/term/theme
```

各模块行为细节见 `docs/design.md`。

## 文档

- `docs/design.md` 核心设计与各模块行为细节（权威）
- `README.md` 使用说明与配置项
- `docs/ctty.md` 控制终端抽象与平台收敛；`docs/interactive-tty.md` pty 桥接
- `docs/style-split.md` 表现层拆包；`docs/render-pipeline.md` 渲染管线；`docs/render-refs-compare.md` 参考项目对比
- `docs/shell-tool.md` run_shell 组件化；`docs/agent-control-tool.md` agent_custom 自调工具；`docs/terminal-caps.md` 终端探测与能力降级（支持范围约定、判定口径）
- `docs/cache-probe.md` prompt cache 探测与量化台阶（供后续设计引用）
- `docs/repl-output-refactor.md` 输出收敛与双流；`docs/repl-status-append.md` 状态展示追加化；`docs/repl-replay-rendering.md` 回放复用评估（未实施）；`docs/probe-redesign.md` 环境探针重构（归档）

## 构建与测试

```bash
go build ./...        # 构建
go vet ./...          # 静态检查
go test ./...         # 全量测试
go test -race ./...   # 竞态检测
go run . ask "你好"   # 单发冒烟（需配置 api_key）
python3 scripts/render_audit.py   # 输出侧渲染审计（pty + VT 回放，需先 make build）
```

测试约定：`agent`/`repl` 各自的 `TestMain` 提供包级基线隔离（HOME 与 cwd 指向临时目录，`TestProcessEnvIsolated` 守卫），用例不得依赖真实 HOME/配置；会话目录用 `t.TempDir()`，多 agent 共享会话时显式同步 `cfg.GlobalSession`；LLM mock 用 `agent/mock_test.go` 的 `newMockLLM` + `mockStep`；readline 用 fakeTerm 注入按键，真实终端与 pty 桥接 E2E 门控变量见 `docs/design.md`《测试》。交互/中断类手工验证用 `make build` 产出的 `./tanya`：**不要用 `go run .`**（`^Z` 会停住 wrapper，shell 抢走终端前台后 `^C` 失效）。
