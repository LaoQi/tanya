# tanyan

极简命令行 AI Agent，Go 编写，无 GUI/WebUI。模块路径 `github.com/LaoQi/tanyan`。

## 设计约束

- 极简优先：依赖仅 `gopkg.in/yaml.v3` 与 `golang.org/x/sys/unix`，新增依赖需先讨论
- package 划分：`main`（仅入口）、`repl`（REPL/补全/picker）、`agent`（核心逻辑）、`readline`（自研终端输入层，实质替换 chzyer/readline）；根目录仅保留 main.go 与顶级包
- 终端输入层自研（raw mode + ANSI 渲染），fish 风格 ghost 置灰建议；Windows 仅支持 Windows Terminal（VT 模式，`terminal_windows.go` 占位未实现），不支持 cmd/老 conhost；不引入 TUI 框架
- 不做工具注册表：工具硬编码在 `ToolDefs()` 与 `Agent.dispatch` 的 switch 中
- 工具策略：以 `run_shell` 为核心，新能力优先用 shell 命令组合实现；小型纯计算/查询工具放 `builtin.go`
- 出站请求 UA 伪装（避免厂商风控）：默认 `pi/0.85.0 (linux; node/v22.14.0; x64)`（Pi coding agent 的 UA），yaml `user_agent`、env `TANYA_USER_AGENT` 可配
- 思考等级走 OpenAI 标准字段 `reasoning_effort`（minimal/low/medium/high/max），yaml `reasoning_effort`、env `TANYA_REASONING_EFFORT`、REPL `/think` 三处可配；厂商私有思考参数（GLM `thinking`、Qwen `enable_thinking` 等）不支持
- 代码不添加注释，除非用户明确要求
- 颜色一律使用终端 16 色基本 SGR 码（30-37/90-97），不用 256 色/truecolor 硬编码色值

## 结构

```
main.go            入口、flag 子命令、ask 单发
repl/repl.go       REPL 循环、斜杠命令、提示符模板渲染、InterruptContext（ask 单发用 signal；REPL 用按键 watcher 中断）
repl/completer.go  ghost 建议与 Tab 补全数据源
repl/picker.go     /load 会话方向键选择菜单（非 TTY 序号降级）
repl/toolview.go   工具块状渲染与回调接线
repl/spinner.go    braille 等待动画
readline/*         自研终端输入层（editor 行编辑/历史/Tab 补全菜单 / keys 按键解析 / terminal raw mode 与 KeyWatcher 按键监听 / width 显示宽度与截断）
agent/config.go    配置加载（默认值 < ~/.config/tanyan/config.yaml < env TANYA_*）
agent/llm.go       OpenAI 兼容 client（SSE 流式 + tool_calls 增量合并 + usage 捕获，reasoning_effort 按配置携带）
agent/agent.go     对话 loop、上下文估算、会话持久化（prompt 规则/事实分离：快照存规则，请求时实时拼接环境段）
agent/envprobe.go  环境探针（envSection 纯函数：平台 + cwd + run_shell 执行契约 + 工作区标记，恒定注入无开关）
agent/shell.go     run_shell 工具（shellProfile 按平台解析 bash/sh/ash/pwsh/cmd、streamCapture 头尾截断、ShellResult 结构化返回、常用程序探测拼入工具描述）
agent/builtin.go   内置小工具：get_time / get_env / calc
repl/messages.go   repl 侧用户可见文案常量（UI 文案单一来源）
agent/messages.go  agent 侧用户可见文案常量（错误/结果文本单一来源）
```

各模块行为细节见 `docs/design.md`。

## 文档

- `README.md` 使用说明
- `docs/design.md` 核心设计与各模块行为细节

## 构建与测试

```bash
go build ./...        # 构建
go vet ./...          # 静态检查
go test ./...         # 全量测试
go test -race ./...   # 竞态检测
go run . ask "你好"   # 单发冒烟（需配置 api_key）
```

测试约定：LLM mock 在 `agent/mock_test.go`（`newMockLLM` + 脚本化 `mockStep`，content/arguments 均按多 chunk 发送以覆盖流式合并）；会话目录一律用 `t.TempDir()`，多 agent 共享会话时需显式同步 `cfg.GlobalSession`；涉及系统提示组装的测试用 `isolatePromptEnv` 隔离 HOME 与 cwd；readline 包用 fakeTerm 注入按键事件，真实终端行为用 pty（script 命令）人工验证。

## Git

- 提交用户为 LaoQi 时，提交后提醒需要签名
