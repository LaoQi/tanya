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
- 实现：`bash -c`，捕获 stdout/stderr/退出码/耗时（`ShellResult` 结构化返回）
- 输出捕获：stdout/stderr 各保留头 30000 字节 + 尾 30000 字节（`streamCapture` 滚动窗口），中间字节计数丢弃，模型仍可见首尾内容
- 回调：`OnToolStart`（执行前，标题行 + `⋯`）/ `OnToolEnd`（执行后，结构化 `ToolResult`），渲染在 repl 包 `toolview.go`
- **免确认直接执行**（早期版本有 y/n/a 确认机制，已移除）

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
- 存储模式（CLI `-m` > env `TANYA_SESSION_MODE` > 配置 `session_mode`，默认 auto）：
  - `auto`：当前目录存在 `.tanya/` → local，否则 global
  - `local`：`<启动目录>/.tanya/sessions/`（.tanya 本身即项目隔离，不叠加 workspace-id）
  - `global`：`global_session/<workspace-id>/`，workspace-id 由启动目录派生（可读路径转义 + 短哈希）
- 落盘：`<timestamp>.jsonl`，每轮结束追加写入新消息（一行一条 Message JSON）
- 首行持久化 system prompt 快照（`{"role":"system",...}`），`/load` 还原后前缀与当初逐字节一致；旧格式文件（无 system 首行）回退为载入时快照当前 AGENTS.md
- JSONL 记录完整历史（回放/审计用）
- `/sessions` 列出（id、时间、消息数、首条用户消息摘要），`/load <id>` 恢复继续对话；id 校验拒绝路径穿越；system 行不计入消息数

## 系统提示与缓存友好

- 组装规则：`DefaultSystemPrompt`（内置，固定不可配）+ 全局 `~/.config/tanyan/AGENTS.md`（存在时）+ 工作区 `./AGENTS.md`（存在时），各段以 `# 全局说明`/`# 项目说明` 标题分隔
- 快照机制：`/new`（NewSession）与 `/load`（LoadSession）时刻读取 AGENTS.md 组装快照；会话进行中零文件 IO，快照冻结
- 缓存收益：history 全程 append-only，同一会话内 messages 前缀逐字节不变，prompt cache 逐轮全量命中；`/new` 时 AGENTS.md 未变则 system 前缀跨会话命中
- usage 捕获缓存命中（DeepSeek `prompt_cache_hit_tokens` / OpenAI `prompt_tokens_details.cached_tokens`），REPL 提示符 `{cache}` 占位符显示 `命中/总量`（如 `980/1.2k`）、`{cache_rate}` 显示命中率（保留两位小数，如 `81.67%`），无数据渲染为空

## 配置

优先级：env（`TANYA_*`）> `~/.config/tanyan/config.yaml` > 默认值。

| 配置项 | 默认 | 说明 |
|---|---|---|
| `base_url` | `https://api.openai.com/v1` | API 地址 |
| `api_key` | 空 | 密钥（建议用 env 注入） |
| `model` | `deepseek-v4-flash` | 模型名 |
| `temperature` | 0.7 | |
| `global_session` | `~/.local/share/tanyan/sessions` | global 模式会话基础目录，支持 `~` 展开 |
| `session_mode` | `auto` | 会话存储模式 auto/local/global |

shell 免确认直接执行。

## 测试

标准库 `testing` + `httptest` mock LLM（`agent/mock_test.go`，脚本化 `mockStep`，content/arguments 多 chunk 发送以覆盖流式合并）。覆盖 calc/shell/config/SSE 解析/trim/会话往返/Ask 全链路。REPL 交互部分人工验证。
