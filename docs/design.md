# tanyan 核心设计

## 定位

极简命令行 AI Agent，Go 编写，无 GUI/WebUI/TUI。单二进制，交互式 REPL + 单发模式。

## 架构

仅两个 package：

```
main.go        package main：入口、flag 子命令、REPL、斜杠命令、shell 执行确认
agent/         package agent：全部核心逻辑
├── config.go  配置加载
├── llm.go     OpenAI 兼容 client
├── agent.go   对话 loop、上下文管理、会话持久化
├── shell.go   run_shell 工具
└── builtin.go 内置小工具
```

设计取舍：

- **不做细粒度拆包**：代码总量小，单包按文件分职责即可
- **不做工具注册表**：工具硬编码于 `ToolDefs()` 和 `dispatch` 的 if/switch，存量小且预计长期以 shell 为主
- **依赖仅 2 个**：`chzyer/readline`（REPL 行编辑/历史）、`gopkg.in/yaml.v3`（配置）

## 运行模式

- `tanyan`：交互 REPL，维护内存 messages 历史，SSE 逐 token 流式输出
- `tanyan ask "问题"`：单发，输出后退出
- 全局参数：`-c <path>` 指定配置文件
- Ctrl+C 中断进行中的请求（context 取消，导致 API 错误直接暴露）

## LLM 接入

仅 OpenAI 兼容 Chat Completions API（`/chat/completions`，SSE 流式），一套代码兼容 OpenAI/DeepSeek/GLM/Ollama/vLLM。

流式解析要点：`data:` 行逐条解析 JSON chunk；content 直接拼接并经回调输出；tool_calls 按 `index` 分组做增量合并（id/type/name 覆盖、arguments 拼接），`[DONE]` 结束。

## Agent Loop

```
用户输入 → 追加 history → 请求 LLM（流式）
  → 无 tool_calls：输出即为回答，结束
  → 有 tool_calls：逐个 dispatch 执行 → tool 结果回填 history → 再次请求
```

本地不设轮数上限，依赖模型终止；异常由 API 错误直接暴露。

## 工具

### run_shell（shell.go）

- 参数：`command`（必填）、`timeout`（默认 60s，上限 300s）
- 实现：`bash -c`，捕获 stdout/stderr/退出码
- 输出截断：单项超 30000 字节时保留头 80% + 尾 20%，中间标注截断量
- **确认机制**：默认执行前终端展示命令，等待 `y`（执行）/ `n`（拒绝，结果回填"用户拒绝"）/ `a`（本会话不再询问）；`shell.auto_approve: true` 关闭确认

### builtin（builtin.go）

免确认轻量工具，硬编码 switch 分发：

- `get_time`：当前时间（含时区）
- `get_env`：查询环境变量，名称含 KEY/TOKEN/SECRET/PASS 的拒绝返回
- `calc`：四则运算表达式求值（自实现递归下降解析，支持 `+ - * / %`、括号、负数）

## 上下文管理

- 本地不设上限、不做裁剪：超出模型上下文时由 API 返回错误，直接暴露给用户（可 `/new` 开新会话）
- 用量统计：请求带 `stream_options.include_usage`，捕获响应 usage 作为真实值；对端不返回时降级为本地粗估（CJK 1 token/字，ASCII 0.3/字符）
- 用量实时显示在 REPL 提示符中：`tanyan(1.5k)>` 为 API 实报，`tanyan(~300)>` 为估算
- tool 结果在写入 history 前已被 shell 层截断（单项 30000 字节），避免极端膨胀

## 会话

- 每次启动/`/new` 开启新会话，id 为启动时间戳（`20060102-150405`）
- 落盘：`~/.local/share/tanyan/sessions/<id>.jsonl`，每轮结束追加写入新消息（一行一条 Message JSON）
- JSONL 记录完整历史（回放/审计用）
- `/sessions` 列出（id、时间、消息数、首条用户消息摘要），`/load <id>` 恢复继续对话；id 校验拒绝路径穿越

## 配置

优先级：env（`TANYA_*`）> `~/.config/tanyan/config.yaml` > 默认值。

| 配置项 | 默认 | 说明 |
|---|---|---|
| `base_url` | `https://api.openai.com/v1` | API 地址 |
| `api_key` | 空 | 密钥（建议用 env 注入） |
| `model` | `deepseek-v4-flash` | 模型名 |
| `temperature` | 0.7 | |
| `system_prompt` | 内置简短中文提示 | |
| `session_dir` | `~/.local/share/tanyan/sessions` | 支持 `~` 展开 |
| `shell.auto_approve` | false | shell 免确认 |

## 测试

标准库 `testing` + `httptest` mock LLM（`agent/mock_test.go`，脚本化 `mockStep`，content/arguments 多 chunk 发送以覆盖流式合并）。覆盖 calc/shell/config/SSE 解析/trim/会话往返/Ask 全链路。REPL 交互部分人工验证。
