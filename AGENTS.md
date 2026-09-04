# tanyan

极简命令行 AI Agent，Go 编写，无 GUI/WebUI。模块路径 `github.com/LaoQi/tanyan`。

## 设计约束

- 极简优先：依赖仅 `chzyer/readline` 与 `gopkg.in/yaml.v3`，新增依赖需先讨论
- 仅两个 package：`main`（入口/REPL）与 `agent`（全部核心逻辑），不做细粒度拆包
- 不做工具注册表：工具硬编码在 `ToolDefs()` 与 `Agent.dispatch` 的 switch 中
- 工具策略：以 `run_shell` 为核心，新能力优先用 shell 命令组合实现；小型纯计算/查询工具放 `builtin.go`
- 不引入 TUI 框架，终端输出仅用 ANSI 码
- 代码不添加注释，除非用户明确要求

## 结构

```
main.go            入口、flag 子命令、ask 单发
repl.go            REPL 抽象（package main）：循环、斜杠命令、提示符、Ctrl+C 中断
agent/config.go    配置加载：默认值 < ~/.config/tanyan/config.yaml < env(TANYA_*)
agent/llm.go       OpenAI 兼容 client（SSE 流式 + tool_calls 增量合并 + usage 捕获）
agent/agent.go     对话 loop、上下文估算、会话持久化
agent/shell.go     run_shell 工具（bash -c、超时、输出截断）
agent/builtin.go   内置小工具：get_time / get_env / calc
```

## 文档

- `README.md` 使用说明
- `docs/design.md` 核心设计

## 构建与测试

```bash
go build ./...        # 构建
go vet ./...          # 静态检查
go test ./...         # 全量测试（agent 包，含 httptest LLM mock）
go test -race ./...   # 竞态检测
go run . ask "你好"   # 单发冒烟（需配置 api_key）
```

测试约定：LLM mock 在 `agent/mock_test.go`（`newMockLLM` + 脚本化 `mockStep`，content/arguments 均按多 chunk 发送以覆盖流式合并）；会话目录一律用 `t.TempDir()`，多 agent 共享会话时需显式同步 `cfg.SessionDir`。

## 关键行为

- shell 工具免确认直接执行
- 本地不设上下文上限与轮数上限，不做裁剪；超限等错误由 API 直接暴露
- token 用量优先取 API 实报 usage（`stream_options.include_usage`），缺失时本地粗估兜底；实时显示在 REPL 提示符
- 会话存储模式（`-m local/global/auto`，yaml `session_mode`、env `TANYA_SESSION_MODE` 可配，默认 auto）：local 存 `<cwd>/.tanya/sessions/`，global 存 `~/.local/share/tanyan/sessions/<workspace-id>/`（按启动目录可读路径+短哈希划分），auto 检测 `.tanya/` 存在与否自动选择；记录完整历史
- REPL 斜杠命令：`/help` `/new` `/sessions` `/load` `/context` `/model` `/exit`

## Git

- 提交用户为 LaoQi 时，提交后提醒需要签名
