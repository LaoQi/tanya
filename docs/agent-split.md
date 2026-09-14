# Agent 拆分：接缝重组设计与实施

状态：**S0–S4 已实施**（`agent/stats.go`、`agent/prompt.go`、`agent/session.go` 落地，`main.go`/`repl/` 零改动；`AGENTS.md`/`docs/design.md`/`docs/todos.md` 已同步）。落地偏差见 §10。
相关：`docs/shell-tool.md`（已完成的接缝准备：工具簇已外移，方法数 30→26）。

## 1. 要解决的问题

`Agent` 是 god struct：一个类型承担六类互不相关的生命周期，任何一处变化都改同一文件。

诊断复核（本次实测，对照立项时的 B1 记录；B1 原文随 `docs/open-questions.md` 清理删除）：

| 项 | B1 记录 | 实测 | 说明 |
|---|---|---|---|
| 字段 | 16 | 16 | 不变 |
| 方法 | 30 | 26 | shellTool 组件化已摘掉工具运行簇 |
| `agent/agent.go` | 660 行 | 690 行 | 略涨（cwd 参数、组件装配、dispatch 签名） |
| 薄访问器 / 格式化器 | 13 | 13 | 8 个访问器 + 5 个状态行格式化器，占 26 个方法的一半 |
| 包外接触点 | repl ~20 | repl 22 调用点（18 方法）+ `main.go` 2 处 | 全部是公开方法调用 |

三条判据仍成立：① 六类生命周期挤在一个类型；② 半数方法是只碰 1–2 字段的薄访问器/格式化器；③ 五类互不相关的变化（存储格式 / 提示词模板 / 状态行显示 / 模型交互 / 工具新增）都改它。

shellTool 组件化摘走了「工具运行」与部分「进程事实」（工作区归属），剩下三簇未切：状态行格式化、提示词组装、会话持久化。

## 2. 定位与边界

**目标**：内聚重组 + `Agent` 留门面——把三簇的状态与逻辑收进各自文件/类型，`Agent` 退化为「跨簇协调 + 代码循环 + 分发」。

**非目标**：

- 不拆包：不新建 `agent/session`、`agent/prompt` 子包（极简优先；跨包还会让 `Message`/`Usage` 等类型四处透传）
- 不改对外接口：`main.go` 与 `repl/` 的调用点、`Option` 用法、方法签名全部不变
- 不引入接口抽象/注册表：三个具名类型直接持有，不做 `interface{ Save() }` 这种为了假装可替换的抽象
- 不动 LLM 协议层、不动 shellTool、不改 session 文件格式

**允许的代价**：新增 3 个文件、3 个类型；13 个薄访问器保留在 `Agent`，但降为一行委托（这是门面约束的必然结果，不是精简目标）。

## 3. 硬约束

| 约束 | 含义 |
|---|---|
| 包外零改动 | `git diff --stat main.go repl/` 为空；验收逐阶段检查 |
| 行为零变更 | 提示词逐字、状态行文本逐字、session 文件名与 jsonl 结构、cache 语义、中断回滚语义全部不变（由前置 golden 与既有用例守） |
| 状态归属 | 每簇字段与其全部读写点同处一文件；`Agent` 只留跨簇协调所需的字段 |
| 每阶段独立可停 | S1/S2/S3 各自提交后 `go build ./...` / `go vet ./...` / `go test ./...` 全绿，允许中途停止 |
| 不新增依赖 | 沿用 `gopkg.in/yaml.v3` 与 `golang.org/x/sys/unix` |
| 不加注释 | 沿用项目约定，代码不带注释 |

## 4. 三簇剥离设计

### 4.1 S1 状态行（新增 `agent/stats.go`）

拆分前基线（`agent.go`）：字段 `lastUsage *Usage`；方法 `ContextInfo` / `PromptUsage` / `PromptCache` / `PromptCacheRate` / `PromptSummary` / `formatTokens`。

```go
type usageStats struct{ last *Usage }

func (s *usageStats) record(u *Usage)   // runTurn 写入
func (s *usageStats) reset()            // NewSession / LoadSession 清空
func (s usageStats) contextInfo(est, msgs int, sessionPath string) string
func (s usageStats) promptUsage(est int) string
func (s usageStats) promptCache() string
func (s usageStats) promptCacheRate() string
func (s usageStats) summary(est int) string
func formatTokens(n int) string
```

迁移：`Agent.lastUsage` → `Agent.stats usageStats`；`runTurn` 的写点改 `a.stats.record(resp.Usage)`；`NewSession`/`LoadSession` 改 `a.stats.reset()`；5 个公开方法各降为一行（例如 `func (a *Agent) PromptCache() string { return a.stats.promptCache() }`）。

留在 `Agent`：`totalTokens`（同时依赖 `runtimePrompt` 与 `history`，属协调）与 `estimateTokens`。

验收：`stats_test.go` 覆盖 `last == nil`（估算回落）、`CacheHit() <= 0`（返回空串）、`PromptCacheRate` 的除零边界（走 `CacheHit() <= 0` 分支）、`formatTokens` 的 <1000 / ≥1000 / 大数。

### 4.2 S2 提示词组装（新增 `agent/prompt.go`）

拆分前基线（`agent.go`）：字段 `promptSnapshot`、`legacySystem` + `cwd`（`cwd` 与 shellTool 的 workspace 同源但不共享）；函数 `globalAgentsPath` / `readAgentsFile` / `buildSystemPrompt` / `isLegacyPrompt`；方法 `systemPrompt` / `LegacyPrompt` / `runtimePrompt`。

```go
type promptBuilder struct {
	cwd        string
	globalPath string
	read       func(string) string  // 注入；生产为 readAgentsFile 包装
	snapshot   string
	legacy     bool
}

func newPromptBuilder(cwd, globalPath string, read func(string) string) *promptBuilder  // globalPath 由 New 调 globalAgentsPath() 定格
func (p *promptBuilder) reset()             // NewSession：重读文件重建 snapshot
func (p *promptBuilder) adopt(system string) // LoadSession：采纳文件里的 system 行
func (p *promptBuilder) system() string
func (p *promptBuilder) legacyPrompt() bool
func (p *promptBuilder) runtime(env string) string  // env 由 Agent 传入，非空时追加
```

关键决策：

1. **保持「新会话重读 AGENTS.md」**（`TestSystemPromptFrozen` 锁定 v1→v2 必生效）：注入 `read` 而非构造期定格；`runtime` 不读文件。
2. **env 段由 `Agent` 计算后传入**：组装器不接触 `probe` / `shellTool`，接缝干净。`Agent.runtimePrompt` 保留 `probe != nil` 判断语义。
3. `isLegacyPrompt` 移入 `prompt.go`，`Agent.LegacyPrompt()` 降为一行委托。
4. `Agent.cwd` 保留（env 段与 workspace 基准已定格），`promptBuilder.cwd` 由构造期传入；`globalAgentsPath` 在 `New` 调一次后作为 `globalPath` 注入，`readAgentsFile` 作为默认 reader 留在 `prompt.go`（无包级可变状态）。

验收：复用既有 prompt 用例（默认串、全局/项目拼接、空文件、冻结）；新增 builder 单测——注入 reader 返回脚本内容（不碰真实 HOME）、`adopt("")` 回落 `reset()`、`runtime("")` 不追加分隔符。

### 4.3 S3 会话持久化（新增 `agent/session.go`）

拆分前基线（`agent.go`）：字段 `sessionDir` / `sessionPath` / `saved` / `systemSaved` / `sessionCache` / `sessionStat` / `noSave`；方法 `save` / `LoadSession` / `ListSessions` / `refreshSessions` + `scanSession`、`sessionFileStat`、`SessionInfo`、`resolveSessionDir` / `workspaceID` / `isDir`。

```go
type sessionStore struct {
	dir         string
	disabled    bool
	path        string
	saved       int
	systemSaved bool
	cache       map[string]SessionInfo
	stat        map[string]sessionFileStat
}

func newSessionStore(dir string, disabled bool) *sessionStore
func (s *sessionStore) rotate()                                   // NewSession：新文件名
func (s *sessionStore) append(msgs []Message, system string) error // save：首写 system 行
func (s *sessionStore) load(id string) ([]Message, string, error)  // "" system = 文件无 system 行
func (s *sessionStore) list() ([]SessionInfo, error)
func (s *sessionStore) path() string
```

迁移：`Agent.save` → 组装 `a.store.append(a.history, a.prompt.system())`；`Agent.LoadSession` → `a.store.load(id)` 后由 `Agent` 采纳（`a.prompt.adopt(system)` 或 `reset()`）；`ListSessions` / `refreshSessions` 直接委托；`Agent.NoSave()` 委托 `store.disabled`（保持公开方法）。

保持不变的细节：文件名 `20060102-150405.jsonl`（`time.Now` 就地取，不注入时钟，与 shell-tool 决策一致）；`append` 的「写短则 truncate 回滚 + 不更新 `saved`」；`refreshSessions` 的 mtime+size 缓存与已删条目清理；`resolveSessionDir` 的 auto/local/global 三分支；`scanSession` 的坏行计数与 30 字符摘要截断。

验收：store 独立单测——`append` 幂等（无新消息不动文件）、写失败回滚、`load` 容错（坏 JSON 行中断解码后返回已解部分）、`load` 空文件、`list` 缓存命中不重扫（改 mtime 后失效）、目录不存在的 `list` 返回空而非错误。

### 4.4 S4 余量复核

拆分后 `Agent` 预期：

- 字段 16 → 9：`cfg` / `client` / `tool` / `history` / `cwd` / `probe` / `prompt` / `store` / `stats`
- 导出方法 18 个不变（`repl` 22 + `main.go` 2 接触点的约束），其中 13 个薄方法里 7 个降为一行委托（`ContextInfo` / `PromptUsage` / `PromptCache` / `PromptCacheRate` / `PromptSummary` / `LegacyPrompt` / `NoSave`），另 6 个维持直连（`Model` / `SetModel` / `ReasoningEffort` / `SetReasoningEffort` / `ListModels` / `History`）
- 未导出方法：`save` / `LoadSession` / `ListSessions` / `refreshSessions` 的实体迁入 `sessionStore`，`systemPrompt` / `runtimePrompt` 收窄为组合调用；留 `runTurn` / `dispatch` / `buildMessages` / `totalTokens` 为真核心

若 S4 复核发现某委托仍是两行以上，说明接缝没切干净，回看该簇归属。

## 5. 分步实施

S0–S4 均已完成（实施结论见 §10），下表保留原始分工。

| 步 | 内容 | 验收 |
|---|---|---|
| S0 | 前置：`docs/todos.md` 待补测试批次 T1–T4（含 run_shell 描述与 env 段 golden 全串） | 全量 + `-race` + `-shuffle=on` 全绿 |
| S1 | `stats.go` + 委托改造 + `stats_test.go` | 同上；`git diff --stat main.go repl/` 为空 |
| S2 | `prompt.go` + 委托改造 + builder 单测 | 同上；prompt 相关既有用例零修改通过 |
| S3 | `session.go` + 委托改造 + store 单测 | 同上；session 文件格式对拍（改造前后同名文件内容一致） |
| S4 | 复核字段/方法数；同步 `AGENTS.md` 结构段、`docs/design.md` 行为细节、B1 结案、`todos.md` 勾除；补写本文 §10 偏差记录 | 同上 + 文档自检 |

## 6. 测试影响

改造会触及 6 个测试文件的内部字段引用（实测 115 处）：`agent_test.go`、`ask_nosave_test.go`、`ask_rollback_test.go`、`e2e_test.go`、`llm_responses_test.go`、`shell_cwd_test.go`。

规则：**只允许改内部字段/内部函数引用**（如 `a.sessionPath` → `a.store.path`、`a.history = …` 保持），公开 API 与行为断言不动；每个阶段结束后 `go test -race ./...` 必须全绿，且公开方法的行为断言零修改。

## 7. 收益 / 代价

收益：

- 三簇各自单文件可测：状态行（纯函数边界）、提示词（注入 reader，不碰真实 HOME）、持久化（store 独立用例，不再需要"构造 Agent 才能测存储"）
- 变化隔离：改存储格式只动 `session.go`，改提示词模板只动 `prompt.go`，改状态行只动 `stats.go`
- `Agent` 只剩「协调」，字段 16→9；后续若要再做拆分或重命名，接缝已就位

代价：

- 新增约 200 行（含 13 处一行委托样板）
- 一次性调整 115 处测试内部引用
- 薄访问器数量不减（门面约束所致），只是不再持有状态

净判断：值得做。三处变化确实互不相关，且 S0 提供安全网，收益是长期的。

## 8. 决策记录

| # | 决策 | 理由 |
|---|---|---|
| D1 | 不拆包，只拆类型 | 极简优先；跨包会让 `Message`/`Usage`/`SessionInfo` 四处透传 |
| D2 | `Agent` 留门面，公开签名不变 | repl 22 + main 2 个接触点零改动，风险最小 |
| D3 | 不注入时钟 | 与 shell-tool §2 一致；文件名可正则断言 |
| D4 | 保持「新会话重读 AGENTS.md」 | `TestSystemPromptFrozen` 是行为契约，注入 reader 实现 |
| D5 | `noSave` 归 `sessionStore` | 它只影响落盘，属持久化簇 |
| D6 | 先做前置测试批次，后拆 | prompt 拆分的 golden 不变量需要先有整串断言 |
| D7 | S1 用 `usageStats` 类型而非纯函数 | 让 `lastUsage` 字段真正离开 `Agent`；纯函数方案会把回合状态留在 `Agent` |

## 9. 开工前待决（已全部落定）

- 三簇拆法与边界（D2 门面、D7 类型选择）：认可，按 §4 实施
- 实施粒度：S0 → S1 → S2 → S3 → S4 顺序推进，每阶段独立验证（当前工作区不做 git 提交，改动累积）
- Windows `~\` 展开（原 B7）：本方案不动，已在 `docs/design.md`《工具》run_shell 的"波浪号边界"条结案（不实现、不宣传）

## 10. 落地偏差记录（实施时定稿）

1. **S0 实际范围**：除 T1–T4 七项外，把 `TestToolDefsRunShellDesc` 的三处子串断言改成「与 `toolDesc()`/`runShellParams()` 全串一致」的装配断言（不重复 golden），删掉 `TestShellToolToolDesc` 与 `TestShellToolRunNonInteractive`，`TestNewWithoutShell` 改名 `TestNewRejectsUnavailableShellOverride`。新增 golden 三条：`TestShellToolDescGolden`、`TestRunShellParamsGolden`、`TestEnvSectionGolden`（替换 `TestEnvSectionFull` 的子串断言，保留确定性用例）。
2. **S1**：`usageStats` 按 D7 落为类型；`Agent` 的 5 个门面方法降为一行委托；`formatTokens` 随迁 `stats.go`。新增 `stats_test.go` 覆盖 `formatTokens` 边界、零值回落、API 实报、`reset`；门面级 cache/summary 断言沿用既有 `TestPromptCache*`（未重复）。
3. **S2**：`newPromptBuilder` 在构造期即 `reset()`（读一次文件），`NewSession` 再 `reset()`——比原实现多一次文件 IO，行为可观测面不变。`Agent.runtimePrompt` 保留 `probe == nil` 短路；`promptBuilder.runtime(env)` 以 `env == ""` 判定不追加分隔符。`globalAgentsPath`/`readAgentsFile`/`isLegacyPrompt` 随迁 `prompt.go`。新增 `prompt_test.go`（注入 reader）。
4. **S3**：`sessionStore` 的字段 `path` 与访问器 `path()` 同名冲突，字段改名 `file`、访问器 `path()` 返回之。`Agent.refreshSessions` 删除（`New` 直接调 `store.refresh()`），`save` 保留为 `store.append(a.history, a.prompt.system())` 一行委托，`LoadSession` 组合 `store.load` + `prompt.adopt` + `stats.reset`。新增 `session_test.go`（rotate 命名格式、append 幂等与 system 单写、disabled、load 容错/无 system 行/非法 id、list 缓存命中与变更/删除失效、目录缺失）。
5. **复核数字**（对 §4.4 预期）：`Agent` 字段 16 → 9 ✓；方法 26 → 25（`refreshSessions` 消失，导出 18 个不变）；`agent/agent.go` 690 → 407 行；新增三文件 63 + 74 + 253 = 390 行（其中大半是搬迁，非净增）。
6. **测试内部引用**：6 个文件按 §6 规则改字段引用（`a.sessionDir` → `a.store.dir`、`a.sessionPath` → `a.store.path()`、`a.saved`/`a.systemSaved` → `a.store.*`、`a.lastUsage` → `a.stats.record/reset`），公开 API 与行为断言零修改。
7. **验证**：`go build ./...`、`go vet ./...`、`go test ./...`、`-race`、`-shuffle=on` 全绿；`git diff --stat main.go repl/` 为空（包外零改动约束达成）。

