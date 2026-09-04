# tanyan

极简命令行 AI Agent，Go 编写，无 GUI/WebUI/TUI。单二进制，交互式 REPL + 单发模式。

## 特性

- OpenAI 兼容接口（OpenAI / DeepSeek / GLM / Ollama / vLLM 等），SSE 流式输出
- 以 shell 为核心的工具体系：模型可直接执行 bash 命令
- 内置轻量工具：`get_time` / `get_env` / `calc`
- 会话持久化与恢复（JSONL，记录完整历史）
- token 用量实时显示在提示符（API 实报优先，本地估算兜底）
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

环境变量 `TANYA_*` 可覆盖配置文件：`TANYA_BASE_URL` / `TANYA_API_KEY` / `TANYA_MODEL` / `TANYA_TEMPERATURE` / `TANYA_SESSION_MODE`。

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

## 工具

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
