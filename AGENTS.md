# tanyan

极简命令行 AI Agent，Go 编写，无 GUI/WebUI。模块路径 `github.com/LaoQi/tanyan`。

## 设计约束

- 极简优先：依赖仅 `gopkg.in/yaml.v3` 与 `golang.org/x/sys/unix`，新增依赖需先讨论
- package 划分：`main`（仅入口）、`repl`（REPL/补全/picker/渲染）、`agent`（核心逻辑）、`readline`（自研终端输入层）、`render`（表现层树：样式词汇/终端原语/配色/IR/渲染/解析）；根目录仅 main.go 与顶级包
- 终端输入层自研（raw mode + ANSI 渲染），fish 风格 ghost 置灰建议，不引入 TUI 框架；Windows 仅支持 Windows Terminal（VT 模式，`terminal_windows.go` 占位未实现），不支持 cmd/老 conhost
- 中断依赖两项终端不变量：`ISIG` 开启、终端前台组是 tanyan；启动时记录"自己是否为前台作业"，每回合开始前与桥接前台检查前自愈（恢复 `ISIG`、必要时夺回前台组），后台启动/无控制终端不抢；信号终止的子进程记 `128 + signum`（`^C` → `130`）
- `interactive: true` 的 run_shell 走全 pty 桥接（命令在独立 pty 中运行，真实 tty 由 bridge 切 raw 双向泵转）；仅 Linux 实现，失败场景回退 `/dev/tty` + `TIOCSPGRP` 路径；细节见 `docs/interactive-tty.md`
- 不做工具注册表：工具硬编码在 `ToolDefs()` 与 `Agent.dispatch` 的 switch 中
- 启动即要求可用 shell：`agent.New` 解析 shell（配置覆盖 > 平台探测），全落空直接报错退出，无 noshell 降级路径（`run_shell` 恒定注册、env 段恒定输出 shell 契约、system prompt 恒为 `DefaultSystemPrompt`）
- REPL 输入分发（`repl/dispatch.go`）：`/` 白名单斜杠命令（控制面）、`exit`/`quit` 内建退出、其余直接与 LLM 对话（`:`/`：` 为等价显式前缀，单独一行提示用法）；直通 shell 执行面与 cd 拦截切面已归档（末态 commit 60bc02e，恢复步骤见 design.md），进程 cwd 恒为启动目录（全程不 `os.Chdir`）；agent 侧 `run_shell` 默认在此执行，并可用 `cwd` 参数为单次命令指定目录（设 `cmd.Dir`，不改进程 cwd）
- 工具策略：以 `run_shell` 为核心，新能力优先用 shell 命令组合实现；小型纯计算/查询工具放 `builtin.go`
- LLM 协议双通道，`api_protocol` 配置（yaml/env `TANYA_API_PROTOCOL`，默认 `responses`，非法值启动报错）：`responses` 走 `/responses`，以 DeepSeek 标准为参照（OpenAI 兼容但非完整遵守），reasoning 明文捕获/原样回传、不依赖 `include`/`encrypted_content`、固定 `store: false`；`chat` 走 `/chat/completions`。思维链为 responses 独有，chat 构造时剥离 `ReasoningItems`；思考等级仅用标准字段 `reasoning_effort`（minimal/low/medium/high/max，yaml/env `/think` 三处可配），不支持厂商私有参数，设置后两协议均不发 `temperature`
- 出站请求 UA 伪装（避免厂商风控），默认 `pi/0.85.0 (linux; node/v22.14.0; x64)`，yaml `user_agent` / env `TANYA_USER_AGENT` 可配
- 代码不添加注释，除非用户明确要求
- 颜色一律使用终端 16 色基本 SGR 码（30-37/90-97），不用 256 色/truecolor

## 结构

```
main.go            入口、flag 子命令、ask 单发
repl/              REPL 循环与输入分发（对话优先）、斜杠命令、提示符模板、ghost 补全、/load picker、工具块渲染、回合分隔线、spinner、UI 文案
readline/          自研终端输入层：行编辑/历史/Tab 补全菜单、按键解析、raw mode 与 KeyWatcher、显示宽度、pty 桥接、终端状态自愈
agent/             核心逻辑
  config.go        配置加载（默认值 < ~/.config/tanyan/config.yaml < env TANYA_*）
  llm.go           chat 协议 client（SSE 流式 + tool_calls 增量合并 + usage 捕获）
  llm_responses.go responses 协议 client（input items 映射、reasoning 明文捕获/回传、usage 映射）
  agent.go         对话 loop、上下文估算、消息组装与工具分发
  prompt.go        系统提示组装（快照/旧版识别，AGENTS.md 经注入 reader 读取）
  session.go       会话存储（sessionStore：增量落盘、载入、列表缓存）
  stats.go         状态行统计（usageStats：token 与缓存命中格式化）
  envprobe.go      环境段（构造期定格的平台 + cwd + run_shell 契约）
  shelltool.go     run_shell 组件（profile/程序清单/工作区/家目录/bridge 构造期定格，cwd 解析与终端租约）
  shell.go         run_shell 叶子（shell 参数组装、头尾截断、超时与等待、结构化返回）
  tty_bridge.go    TTYBridge 接口（实现由 readline 提供，经 WithTTYBridge 注入）
  event.go         Event 词汇表（统一协议增量与生命周期）
  builtin.go       内置小工具：get_time / get_env / calc
  messages.go      agent 侧文案常量
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

- `README.md` 使用说明
- `docs/design.md` 核心设计与各模块行为细节
- `docs/interactive-tty.md` 交互式 run_shell pty 桥接设计与落地差异
- `docs/render-pipeline.md` 富文本渲染管线方案
- `docs/style-split.md` 表现层拆包（style → render 树）设计与落地记录
- `docs/render-refs-compare.md` 渲染参考项目对比（持续补录；`refs/` 不入库）
- `docs/repl-output-refactor.md` repl 输出收敛与数据流封装方案（含输出模式与双流收敛；阶段 0-4 已实施）
- `docs/repl-replay-rendering.md` `/history` 回放复用实时渲染评估（原阶段 5，未实施；结论：数据不等价，建议不做或只统一样式）
- `docs/cache-probe.md` prompt cache 机制探测结论（脚本 `scripts/cache_probe.py`）
- `docs/probe-redesign.md` 环境探针重构方案（已实施，归档）
- `docs/shell-tool.md` run_shell 组件化（shellTool）设计与实施（S1–S4 已实施，含落地偏差记录）
- `docs/todos.md` 待办清单

## 构建与测试

```bash
go build ./...        # 构建
go vet ./...          # 静态检查
go test ./...         # 全量测试
go test -race ./...   # 竞态检测
go run . ask "你好"   # 单发冒烟（需配置 api_key）
```

测试约定：LLM mock 见 `agent/mock_test.go`（`newMockLLM` + 脚本化 `mockStep`，多 chunk 发送覆盖流式合并）；会话目录一律用 `t.TempDir()`，多 agent 共享会话时显式同步 `cfg.GlobalSession`；包级基线隔离由 `agent`/`repl` 各自的 `TestMain` 提供（HOME 与 cwd 都指到临时目录，`TestProcessEnvIsolated` 守卫），用例不得依赖开发者真实 HOME/配置；需要"每用例一个干净 cwd"时用 `isolatePromptEnv`；readline 包用 fakeTerm 注入按键，真实终端行为用 pty（`script`）人工验证；pty 桥接三层测试与 E2E 门控变量（`TTY_BRIDGE_E2E` / `TTY_E2E` / `TTY_E2E_REUSE`）见 `docs/design.md`《测试》。交互/中断类手工验证用 `make build` 产出的 `./tanyan`：**不要用 `go run .`**（`^Z` 会停住 wrapper，shell 抢走终端前台后 `^C` 失效）。
