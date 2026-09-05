# tanyan

极简命令行 AI Agent，Go 编写，无 GUI/WebUI。模块路径 `github.com/LaoQi/tanyan`。

## 设计约束

- 极简优先：依赖仅 `gopkg.in/yaml.v3` 与 `golang.org/x/sys/unix`，新增依赖需先讨论
- package 划分：`main`（仅入口）、`repl`（REPL/补全/picker）、`agent`（核心逻辑）、`readline`（自研终端输入层，实质替换 chzyer/readline）；根目录仅保留 main.go 与顶级包
- 终端输入层自研（raw mode + ANSI 渲染），fish 风格 ghost 置灰建议；Windows 仅支持 Windows Terminal（VT 模式，`terminal_windows.go` 占位未实现），不支持 cmd/老 conhost；不引入 TUI 框架
- 不做工具注册表：工具硬编码在 `ToolDefs()` 与 `Agent.dispatch` 的 switch 中
- 工具策略：以 `run_shell` 为核心，新能力优先用 shell 命令组合实现；小型纯计算/查询工具放 `builtin.go`
- 出站请求 UA 伪装（避免厂商风控）：默认 `pi/0.85.0 (linux; node/v22.14.0; x64)`（Pi coding agent 的 UA），yaml `user_agent`、env `TANYA_USER_AGENT` 可配
- 代码不添加注释，除非用户明确要求

## 结构

```
main.go            入口、flag 子命令、ask 单发
repl/repl.go       REPL 循环、斜杠命令、提示符模板渲染、InterruptContext（Ctrl+C 中断）
repl/completer.go  ghost 建议与 Tab 补全数据源（/load 候选 Display 带时间/条数/简介）
repl/picker.go     /load 会话方向键选择菜单与非 TTY 序号降级
readline/editor.go    行编辑器（缓冲/光标/历史/渲染）、ErrInterrupt；快捷键：Ctrl+A/E/B/F/U/K/W/Y/T/L、Alt+B/F（词移动，按空白分词）、Home/End/方向键；render 多行感知（prevRows 跟踪占用行数，重渲染上移清屏，光标按 ⌈宽/列⌉ 跨行定位，Size 不可用退化单行）
readline/keys.go      按键解析状态机（ESC 序列/控制键/UTF-8）
readline/terminal*.go Terminal 接口、unix termios raw mode、Windows 占位、非 TTY 降级
readline/width.go     字符宽度表与 ANSI 剥离
agent/config.go    配置加载：默认值 < ~/.config/tanyan/config.yaml < env(TANYA_*)
agent/llm.go       OpenAI 兼容 client（SSE 流式 + tool_calls 增量合并 + usage 捕获 + /models 列表获取）
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

测试约定：LLM mock 在 `agent/mock_test.go`（`newMockLLM` + 脚本化 `mockStep`，content/arguments 均按多 chunk 发送以覆盖流式合并）；会话目录一律用 `t.TempDir()`，多 agent 共享会话时需显式同步 `cfg.GlobalSession`；涉及系统提示组装的测试用 `isolatePromptEnv` 隔离 HOME 与 cwd（go 1.22 无 `t.Chdir`）；readline 包用 fakeTerm 注入按键事件测试状态机，真实终端行为用 pty（script 命令）人工验证。

## 关键行为

- shell 工具免确认直接执行
- 本地不设上下文上限与轮数上限，不做裁剪；超限等错误由 API 直接暴露
- 系统提示组装（缓存友好）：`DefaultSystemPrompt` 固定基础（`system_prompt` 配置项已移除）+ 全局 `~/.config/tanyan/AGENTS.md` + 工作区 `./AGENTS.md`（标题段 `# 全局说明`/`# 项目说明`，文件缺失/空白跳过）；`NewSession`/`LoadSession` 时刻快照，会话内零文件 IO 冻结，history append-only 保证请求前缀逐字节稳定命中 prompt cache
- 会话持久化：jsonl 首行写 system 快照（`systemSaved` 标志防重复），`/load` 还原快照；旧格式（无 system 首行）回退载入时快照当前 AGENTS.md，且保持不补写；system 行不计入 `/sessions` 条数与标题
- token 用量优先取 API 实报 usage（`stream_options.include_usage`），缺失时本地粗估兜底；实时显示在 REPL 提示符；缓存命中捕获（DeepSeek `prompt_cache_hit_tokens` / OpenAI `prompt_tokens_details.cached_tokens`）经 `PromptCache()` 供 `{cache}` 占位符显示
- 会话存储模式（`-m local/global/auto`，yaml `session_mode`、env `TANYA_SESSION_MODE` 可配，默认 auto）：local 存 `<cwd>/.tanya/sessions/`，global 存 `~/.local/share/tanyan/sessions/<workspace-id>/`（按启动目录可读路径+短哈希划分），auto 检测 `.tanya/` 存在与否自动选择；记录完整历史
- 会话列表扫描：`agent.New` 启动时预扫描填充缓存；`ListSessions` 按 (mtime,size) 增量刷新，仅重扫变化的文件；每文件 `bufio` 逐行计数条数、仅解码至首条 user 消息取摘要
- REPL 斜杠命令：`/help` `/new` `/sessions` `/load` `/context` `/history` `/model` `/exit`；`/history` 无参截断列表（单行 120 rune）、`/history n` 全量查看单条、`/history all` 全量显示；`/model` 无参实时调接口列出可用模型（`*` 标注当前，失败仍显示当前模型），带参直接切换不校验；`/model ` 支持补全（接口列表在 REPL 内首次加载后缓存，失败不重试）
- REPL 提示符模板（yaml `prompt`、env `TANYA_PROMPT` 可配，空值回退默认）：占位符 `{cwd}` 短路径 / `{model}` 模型 / `{usage}` 上下文 token / `{cache}` 缓存命中 / `{cache_rate}` 缓存命中率（两位小数，如 `81.67%`，无数据渲染为空），未知占位符原样保留；颜色由模板自带（yaml 双引号内用 `\x1b`），默认 `\x1b[37m{cwd}\x1b[0m \x1b[34m{model}\x1b[0m \x1b[32m{usage}>\x1b[0m `（路径白 / 模型蓝 / 上下文绿；缓存占位符存在但默认模板不含）

## Git

- 提交用户为 LaoQi 时，提交后提醒需要签名
