# tanyan

极简命令行 AI Agent，Go 编写，无 GUI/WebUI/TUI。单二进制，交互式 REPL + 单发模式。

## 特性

- OpenAI 兼容接口（OpenAI / DeepSeek / GLM / Ollama / vLLM 等），SSE 流式输出
- 以 shell 为核心的工具体系：模型可直接执行 bash 命令
- 内置轻量工具：`get_time` / `get_env` / `calc`
- 会话持久化与恢复（JSONL，记录完整历史，system 快照随会话冻结）
- token 用量实时显示在提示符（API 实报优先，本地估算兜底），支持显示缓存命中
- AGENTS.md 项目说明自动注入系统提示（全局 + 工作区双层，会话级快照保证 prompt cache 友好）
- 依赖仅 2 个，核心逻辑测试覆盖率 90%+

## 安装

```bash
go install github.com/LaoQi/tanyan@latest
```

或从源码构建：

```bash
git clone https://github.com/LaoQi/tanyan.git && cd tanyan
go build -o tanyan .
```

## 配置

`~/.config/tanyan/config.yaml`（完整示例见 `config.example.yaml`）：

```yaml
base_url: https://api.deepseek.com/v1
api_key: "sk-..."
model: deepseek-v4-flash
```

环境变量 `TANYA_*` 可覆盖配置文件：`TANYA_BASE_URL` / `TANYA_API_KEY` / `TANYA_MODEL` / `TANYA_TEMPERATURE` / `TANYA_SESSION_MODE` / `TANYA_PROMPT` / `TANYA_USER_AGENT` / `TANYA_TOOL_OUTPUT_LINES`。

## 使用

```bash
tanyan                 # 交互 REPL
tanyan ask "问题"      # 单发模式
tanyan -c x.yaml       # 指定配置文件
tanyan -m local        # 会话存到当前目录 .tanya/
```

会话存储模式（`-m` 参数 / 配置项 `session_mode` / env `TANYA_SESSION_MODE`，优先级从高到低）：

| 模式 | 会话目录 |
|---|---|
| `auto`（默认） | 当前目录存在 `.tanya/` 则用 `<cwd>/.tanya/sessions/`，否则用全局 |
| `local` | `<启动目录>/.tanya/sessions/` |
| `global` | `~/.local/share/tanyan/sessions/<workspace-id>/` |

REPL 斜杠命令：

| 命令 | 说明 |
|---|---|
| `/help` | 帮助 |
| `/new` | 开启新会话（当前会话自动保存） |
| `/sessions` | 列出历史会话 |
| `/load <id>` | 载入历史会话 |
| `/context` | 查看上下文占用 |
| `/model [name]` | 查看/切换模型 |
| `/exit` | 退出 |

会话按启动目录划分工作区（global 模式），`/sessions` 只显示当前项目的会话。

## AGENTS.md 注入

系统提示 = 内置默认提示（固定）+ 全局说明 + 项目说明，后两者来自：

| 层级 | 路径 | 标题 |
|---|---|---|
| 全局 | `~/.config/tanyan/AGENTS.md` | `# 全局说明（~/.config/tanyan/AGENTS.md）` |
| 工作区 | `<启动目录>/AGENTS.md` | `# 项目说明（AGENTS.md）` |

- 文件不存在或内容为空白则跳过该层；组装在会话开始（`/new`、`/load`、启动）时刻快照，会话进行中不再读取文件
- 每次请求的 system prompt 末尾实时拼接环境段（OS/CWD/bash 执行契约/工作区标记），不进快照、不持久化，模型可据此构造 `run_shell` 命令
- history 全程追加，同一会话内请求前缀逐字节不变，prompt cache（DeepSeek/OpenAI）逐轮命中
- 提示符模板支持两个缓存占位符：`{cache}` 缓存命中量（如 `980/1.2k`）、`{cache_rate}` 缓存命中率（保留两位小数，如 `81.67%`），无数据均渲染为空，默认模板不含缓存段

## 工具

工具执行以块状格式显示（TTY 下整块暗灰 `\x1b[90m` 与主输出区分，非 TTY 纯文本）：`▸ 工具名 命令` 标题行 + 缩进输出行（stderr 行加 `2|` 前缀）+ 亮蓝状态行 `↳ exit 0 · 0.3s · 12 行`（退出码/超时/中断 · 耗时 · 输出行数），默认最多显示 20 行（`tool_output_lines` 可配，1-1000），超出仅保留头 3 行 + 尾 2 行（状态行显示 `共 N 行`）；执行开始即打印标题行（`⋯` 标记进行中）。

TTY 下带等待动画：LLM 请求等待期间显示 `⠋ 等待响应 3s`（首个 token 到达即消失），工具执行期间标题行下方显示独立 spinner 行（结束时原位重绘为最终标题）；每轮请求完成打印状态行 `  ↳ TTFT 0.8s · 3.2s · prompt 12.3k · completion 1.2k · 缓存 81.67%`（无 usage 时显示本地估算上下文，字段缺失自动省略）。非 TTY 环境动画自动关闭，状态行仍输出。

| 工具 | 确认 | 说明 |
|---|---|---|
| `run_shell` | 免确认 | bash 执行命令，超时 60s，输出截断 30000 字节 |
| `get_time` | 免 | 当前时间 |
| `get_env` | 免 | 环境变量查询（敏感变量名拒绝） |
| `calc` | 免 | 四则运算求值 |

## 开发

```bash
go build ./...
go vet ./...
go test ./...
go test -race ./...
```

设计文档见 `docs/design.md`。
