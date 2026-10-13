# 会话交接（`/next` + `next_session`）实施文档

状态：**已实施**（2026-10-10，P0–P2 全部落地，用户逐条拍板的口径见 §2）。现行口径以 `docs/design.md`《会话交接》为准，本文件管落地顺序、接口签名、文案与验收；进度与偏差见文末。

## 1. 目标与不变量

目标：会话执行到一定阶段后，让模型（或用户）**自行收尾当前会话并把关键进度交接给新会话**——旧会话的历史不再继续增长，新会话以一份现写的摘要起步。

不变量（违反即回滚该改动）：

1. **旧会话文件仍不可改写**：交接只**追加**一条 user 事实行 `[会话已移交至 <新 id>]`，不裁剪、不删、不重排（append-only 与归档卷不变性一并保住）。
2. **交接摘要进 user 消息，不进 system**：system 快照机制（基座 + 两层 AGENTS.md，`promptBuilder`）零改动；新会话首行 system 仍是当前快照，`/load` 仍能逐字节还原。
3. **交接在回合出口应用、不在工具 `Invoke` 里 rotate**：保住 `tool_calls → tool` 相邻性、并行工具安全与 append-only 假设。
4. **只有正常结束的回合才应用交接**：中断/错误回合丢弃 pending。
5. **零新依赖、零新配置键**（`config.example.yaml` 与 `tanya config` 输出逐字节不变）。

## 2. 语义（三入口）

| 入口 | 摘要来源 | 交接后 |
|---|---|---|
| `next_session{summary, continue}`（模型自调） | 参数 `summary` | `continue` 默认 **true** → 无人值守续跑一轮；`false` → 停在提示符等用户 |
| `/next`（无参） | 模型现写：REPL 先发一轮引导 ask（`agent.MsgHandoffPrompt`） | 等用户 |
| `/next <文本>` | 模型现写（同上，**额外一轮**） | `<文本>` 作为新会话的第一条 user 输入，立刻跑一轮 |

- `/next <文本>` 是新会话 `history = [交接摘要(user), <文本>(user), …]` 两条相邻 user，**不合并**（摘要与指令语义不同）；摘要轮不把 `<文本>` 交给模型。
- **命令路径无条件忽略工具参数里的 `continue`**：`/next` 的续跑规则完全由命令参数决定（无参=停、带参=用参数续跑），不依赖模型配合。
- `/next` 未产生交接（模型没调用工具）→ 提示并把 `<文本>` 丢弃，不做任何后续。

### 与既有命令的分工

| 命令 | 继承内容 | 旧会话 | 用途 |
|---|---|---|---|
| `/new` | 无 | 留下 | 换话题 |
| `/fork` | 全量历史 | 保留、可 `/load` 回切 | 分支 |
| **`/next`** | **摘要** | 终止（追加移交事实行） | 上下文到量 / 阶段完成 |
| `/archive` | — | 无损压缩冷存 | 腾地方 |

## 3. 机制

```
模型 tool_call: next_session{summary, continue}
  → Invoke 只置 pending（不动 history、不 rotate）
  → 工具结果回填，模型收尾发言，runTurn 自然结束
  → Ask: save() 旧会话（含这次 tool_call 与结果，旧会话自解释）
  → Ask 出口（EventTurnEnd 之后，err == nil 时）applyPending：
       nextID() 算新 id → 旧 history 追加 [会话已移交至 <新 id>] → save()
       → store.rotate() → history = [交接摘要(user)] → prompt/stats reset → save()
  → REPL 在 t.End(err) 之后 TakeHandoff() 拉取 → 渲染移交块 → 按 §2 决定是否续跑
```

关键取舍：

- **pending + 出口应用**（不变量 3）：与既有 `InterruptError` 同一类出口语义。
- **REPL 拉取式感知（`TakeHandoff`），不新增事件**：这是回合后的状态转移、不是过程事实；避开「新增事件必须同批带上消费者」的额外面，风格同 `ArchiveReadOnly()`。
- **续跑由 REPL 发起**（`Agent.Continue`）：`continue` 是「拿着交接摘要直接跑一轮」，不是再发一条 user 消息；REPL 侧发起才能与提示符、回合渲染、热键、注意力通知对齐。

## 4. 接口与落点

`agent/session.go`：

```go
// nextID 只计算不切换（同名占用退 -2/-3…），rotate 复用它
func (s *sessionStore) nextID() string
func (s *sessionStore) rotate()             // rotateTo(nextID())
func (s *sessionStore) rotateTo(id string)  // 置 file、清 frozen/saved/systemSaved
```

`agent/handoff.go`（新）：

```go
const handoffSummaryLimit = 128 * 1024 // rune 兜底上限，暂不做配置

type Handoff struct {
	Summary  string
	OldID    string
	NewID    string
	OldMsgs  int
	Continue bool // 模型意图；命令路径由 REPL 忽略
	NoSave   bool
}

func (a *Agent) RequestHandoff(summary string, cont bool) error // 工具路径：校验 + 置 pending
func (a *Agent) TakeHandoff() (Handoff, bool)                   // 表现层一次性取走
func (a *Agent) applyHandoff()                                  // 回合出口：本节流程
```

内建工具 `next_session`（描述/schema/执行同处该文件，经 `newNextTool(a)`）：

- `summary`（string，必填，空/纯空白报错，超 `handoffSummaryLimit` 报错要求精简；换行归一 `\r\n`→`\n`）
- `continue`（boolean，可省，**默认 true**）
- 同一回合二次调用：报错文本、首次生效（不覆盖）。
- 工具结果文本：`已请求会话交接，本回合结束后开启新会话（摘要 N 行）`；置 `Handoff.Continue` 供 REPL 消费。

`agent/tools.go`：

```go
func assembleTools(registered []Tool, ctl configTarget, h handoffTarget) []Tool
// 顺序 = registered... + next_session + agent_custom（agent_custom 恒末位）
type handoffTarget interface{ RequestHandoff(string, bool) error }
```

`agent/agent.go`：

```go
// ask 的基准语义由 mark+1 改为 base：kept = len(history) > base、回滚 [:base]
func (a *Agent) ask(ctx context.Context, sink EventSink, base int) error
func (a *Agent) Continue(ctx context.Context, sink EventSink) error // 不追加 user 消息，直接跑一轮
```

`Ask` 传 append 前长度并在 `EventTurnEnd` 之后调 `applyHandoff`（仅 `err == nil`）；`Continue` 传 `len(history)`，两者共用中断/错误出口（既有 `ask_rollback_test.go`/`ask_nosave_test.go` 守这次重构的语义等价）。

`repl`：

- `slashCommands` + `helpText` + `handleCommand` 加 `/next`；`TestSlashCommandsAllHandled` 覆盖。
- `REPL.nextPending bool` / `REPL.nextInput string`（命令路径状态）。
- `handleNext(args)`：置位 → `r.ask(agent.MsgHandoffPrompt)` → 返回后仍置位则提示未交接。
- `r.ask` 尾部：`TakeHandoff()` → `renderHandoff`（旧会话三行 + 新会话行）+ `r.started = time.Now()` → 命令带参 `r.ask(nextInput)` / 命令无参停 / 模型自调 `h.Continue` 则 `r.continueTurn()`。
- `r.runTurn(ctx, in, t, cont bool)`：`cont` 时调 `agent.Continue`。

## 5. 文案与渲染

`agent/messages.go`：

| 常量 | 文本 |
|---|---|
| `MsgHandoffPrompt` | 请调用 next_session 提交交接摘要：总结当前进度、关键上下文与下一阶段目标。提交后本会话结束，交由新会话继续。 |
| `MsgHandoffToolDoneFmt` | 已请求会话交接，本回合结束后开启新会话（摘要 %d 行） |
| `MsgHandoffEmpty` | 交接摘要不能为空 |
| `MsgHandoffTooLongFmt` | 交接摘要过长（%d 字，上限 %d） |
| `MsgHandoffDup` | 本回合已请求过会话交接 |
| `MsgHandoffNoticeFmt` | [会话已移交至 %s] |
| `MsgHandoffIntroFmt` | [会话交接] 上一个会话（%s）移交的进度摘要：\n%s |

`repl/messages.go`：

| 常量 | 文本 |
|---|---|
| `MsgHandoffBlockFmt` | 已移交新会话 %s（交接 %d 行）\n |
| `MsgHandoffNoSave` | （不落盘模式，未写入） |
| `MsgHandoffNone` | 未生成交接摘要，本回合按普通对话处理\n |
| `MsgHandoffReadOnlyFmt` | 当前为归档只读会话（%s）；交接请先用 /fork 开新会话\n |

渲染：移交块 = `farewellText(旧会话快照)`（已有三行）+ `MsgHandoffBlockFmt`，**不重放摘要正文**；`-n` 追加未写入提示；块后补一个 `turnSep`（新回合起点）。工具块走通用回落（不注册视图）。

## 6. 边界与失败面

| 情形 | 行为 |
|---|---|
| 摘要空/纯空白 | 工具返回错误文本，不置 pending |
| 摘要超 128K rune | 工具返回错误文本，不置 pending |
| 同回合二次调用 | 报错文本，首次生效 |
| 回合中断/报错 | 丢弃 pending（不变量 4） |
| `-n` | 交接照常（内存），收尾块提示未写入 |
| 归档只读态 `/next` | 拒绝（`MsgHandoffReadOnlyFmt`），提示 `/fork` |
| 连续交接 | 允许，无次数上限（模型显式行为；不做自动移交） |
| 旧会话再次 `/load` | 可见 tool_call、工具结果与 `[会话已移交至 X]` 事实行 |

## 7. 明确不做

裁剪/改写旧会话；自动归档旧会话（单会话 zip 卷会产生一堆小卷，归 `/archive`）；历史压缩（把前几轮揉成摘要）；按 token 阈值无人值守自动移交；摘要的二次模型加工；新配置键与新依赖。

## 8. 分期与验收

| 阶段 | 内容 | 验收 |
|---|---|---|
| P0 | `nextID` 拆分 + `ask` base 重构 + `Continue` + `handoff.go` + `assembleTools` 三参 | `go build ./... && go vet ./... && go test ./...`，含 `agent/handoff_test.go` 与既有 rollback/nosave 回归 |
| P1 | REPL：`/next`、`nextPending/nextInput`、`ask` 尾部交接处理、`continueTurn`、白名单/help | `repl` 用例 + `make build` 真机 pty 冒烟 |
| P2 | 文档：`docs/design.md`、`AGENTS.md`、`README.md`、`CHANGELOG.md`、本文件收尾 | `tanya config | diff - config.example.yaml` 为空 |

测试清单：空/超长摘要、二次调用、中断回合丢弃 pending、`-n` 不落盘、归档只读拒绝、`/next` 未产生交接、`continue=false`、`Continue` 的中断/错误回滚、旧文件逐字节不可改写、工具清单顺序（`tanya.Standard` + `next_session` + `agent_custom`）、`config.example.yaml` 不受影响。

## 9. 进度

| 阶段 | 内容 | 状态 |
|---|---|---|
| P0 | `nextID` 拆分 + `ask` 界标重构 + `Continue` + `handoff.go` + `assembleTools` 三参 | 已完成（2026-10-10） |
| P1 | REPL：`/next`、`nextPending/nextInput`、`ask` 尾部交接处理、`continueTurn`、白名单/help | 已完成 |
| P2 | 文档收尾（design/AGENTS/README/CHANGELOG/本文件） | 已完成 |

## 10. 落地偏差（实施中对文档的微调）

1. `Agent.ask` 的签名不是单一 `base`：`Ask` 会先 append 一条 user 消息（属本回合输入、不算产出），`Continue` 没有，故拆成 `ask(ctx, sink, rollback, producedFrom)` 两个界标——`Ask` 传 `(base, base+1)`、`Continue` 传 `(base, base)`，两者共用同一 `kept := len(history) > producedFrom` 与 `[:rollback]` 回滚。既有 `ask_rollback_test.go`/`ask_nosave_test.go` 原样通过（语义等价）。
2. `Handoff` 增 `OldStats Stats` 与 `OldFile string` 两个快照字段（文档只写了 id/条数）：旧会话的统计与落盘路径在 `rotate` 后已不可取，而收尾块要复用 `farewellText` 的三行。
3. `sessionStore.nextID()` 返回的是**会话 id**（不带 `.jsonl`），`rotate()` 自行拼路径。
4. 未新增 `MsgHandoffNoSave`：`-n` 下 `farewellText` 的文件行已输出 `未写入（不落盘模式）`，再追加一句是重复。
5. 收尾块里的"交接 N 行"由 `repl` 侧 `strings.Count(summary, "\n")+1` 现算（`agent` 的 `handoffLines` 不导出）。
6. `MsgHandoffPrompt` 放在 `agent/messages.go`（模型侧文案集中于 `agent`，由 `repl` 引用），未按 §5 表放在 `repl`。
7. 测试补出文档未列的两条：`Continue` 无 user 消息追加的正面断言（`TestContinueAppendsWithoutUserMessage`）与旧会话逐条不可改写的字节比对（`TestHandoffOldFileUnchangedExceptNotice`）。

## 11. 验收记录（2026-10-10）

- `gofmt -l` 干净；`go build ./... && go vet ./... && go test ./...` 全绿；`go test -race ./agent/ ./repl/` 通过。
- 真机 pty（`make build` + 假 LLM 探针，三条路径全部通过）：
  - `/next <指令>`：请求 4 次（摘要两轮 + 指令轮），旧会话文件末行 `[会话已移交至 <新 id>]`，新会话 = system 快照 + 交接摘要 + 指令 + 回答，收尾块为旧会话三行 + `已移交新会话 X（交接 2 行）`。
  - `/next` 无参：请求 2 次（不续跑），新会话只有交接摘要，停在新提示符；旧会话文件保留全部历史 + 移交事实行。
  - 模型自调 `next_session`（`continue` 缺省）：请求 4 次，交接后自动续跑一轮、新会话无多余 user 消息。

## 12. review 修复（2026-10-10）

三处（前两处由 review 的变异复验暴露）：

1. **`nextID` 双算 → 拆 `rotateTo(id)`**：`applyHandoff` 原先自己调一次 `nextID()` 写移交事实行、`rotate()` 内部再调一次算新会话文件名，两者一致只靠「两次 `os.Stat` 之间没有并发写同名文件」这个巧合——同秒并发下事实行会指向一个并非新会话的 id，而事实行是永久落盘的。现在 `rotateTo(id)` 接收调用方算好的 id（`rotate()` = `rotateTo(nextID())`），事实行与新会话文件名同源。
2. **测试洞（变异存活）**：删掉 `applyHandoff` 里的 `a.stats.reset()` 后 `TestHandoffSwitchesSession` 仍绿——它断言的 `Stats().Messages` 是 `len(history)` 派生量，与 usage 计数无关。现在该用例的 mock 步骤带 `usage`，交接后断言 `PromptTokens`/`CompletionTokens`/`TotalTokens` 归零、`HasContext` 为假。
3. **`lastHandoff` 未被重置入口清理**：`NewSession`/`Fork`/`LoadSession` 原先只清 `handoffPend`；陈旧快照不泄漏仅因 REPL 每回合必调 `TakeHandoff`（库用法会取到上一个会话的快照）。三处补 `a.lastHandoff = nil`，新增 `TestHandoffStaleStateCleared` 覆盖。

4. **测试补强（6 例，由变异复验找出的洞）**：① `TestHandoffSummaryLimitBoundary`——上限**边界**（恰好 `handoffSummaryLimit` 通过）与 **rune 口径**（`limit/2` 个中文字符字节数已超 1.5 倍仍应通过；原「`limit+1` 个中文字符」用例在 rune/字节两种实现下都超限，杀不死按字节的变异）；② `TestContinueRejectsArchiveReadOnly`；③ `TestHandleNextRejectsArchiveReadOnly`（无参与带参两条路径；§8 清单早已列此项，此前未落地）；④ `TestHandleNextInputOverridesContinueFalse`——**带参 + `continue:false`** 是「命令路径无条件忽略工具 `continue`」的最强口径，原用例只用 `continue:true` 作样例，把它混在「缺省续跑」里掩盖了；⑤ `TestHandoffResetsStarted`；⑥ `TestHandoffBlockTextExact` + `TestHandoffBlockNoSave`（收尾块从 `Contains("已移交新会话")` 升级为含旧会话 id/用量/文件行与精确「交接 N 行」整行断言，并覆盖 `-n` 的未写入行）。`repl` 侧 `newNextAgent` 因此加变参以支持 `agent.NoSave(true)`。

变异复验（改完即还原）：删 `stats.reset()` → 用例 2 变红；删 `NewSession` 的 `lastHandoff = nil` → 用例 3 变红。第一轮 12 个变异中 6 个存活，上述补强后重放 8 个（含 `-n` 收尾块变体）**全部变红**（`gofmt`/`build`/`vet`/`test -count=1`/`-race` 复跑全绿）。

## 13. 工作区移交（2026-10-13）

状态：**已实施**。目标：让交接不仅能转段，还能**跨项目**——`next_session` 带上 `workspace` 时把新会话直接开到另一个项目目录，从而在 A/B 两个项目之间来回交接协作。新增能力只落在**工具**上（用户拍板：`/next` 命令不加工作区参数、`/switch` 语义不动、`system_prompt.md` 不改、零新配置键）。

### 13.1 参数与校验

```go
func (a *Agent) RequestHandoff(summary string, cont bool, workspace string) (string, error)
```

- `workspace` 取绝对路径 / `~` / 相对当前工作区；空白视为未指定。
- 处理在 `RequestHandoff` 期完成：`resolveWorkspace` 解析 + `os.Stat` 校验，失败即回 `MsgHandoffBadWorkspaceFmt`（`交接目标工作区不存在或不是目录: <输入>`）且**不置 pending**——模型在同一回合内即可改主意或重试。
- 解析结果**等于当前工作区**（用户口述「是否变更都不重要」）→ 视为未指定：不报错、不切换，工具结果与事实行都不带工作区后缀。
- 返回值是新会话的生效工作区（空串 = 留在原地），`nextTool` 据此选文案：切换时 `已请求会话交接，本回合结束后开启新会话（摘要 %d 行，工作区切至 %s）`，否则原文案。

### 13.2 应用顺序（`applyHandoff`）

```
旧会话快照（id / file / stats / 条数 / OldWorkspace）   ← 必须在切换前，切换会 reset 这些
  → 带 workspace 时：resolveWorkspace 复核（防回合执行期间目录被删）
       → loadWorkspace(目标)（换装 prompt/store/tools/client + NewSession）
  → newID := a.store.nextID(); a.store.rotateTo(newID)
  → 旧 history + 移交事实行 → 由**切换前保留的旧 *sessionStore 指针** append
  → history = [交接消息] → prompt.reset() + stats.reset() → save()
```

- **为什么留旧 store 指针**：切完 `a.store` 已指向新工作区，`a.save()` 会把事实行写进**新**会话；`oldStore.append(...)` 是同一份 `append` 实现（`disabled/frozen/held` 守卫一致），不新增裸文件写路径。
- **落点与快照**：新会话落在目标工作区推导出的会话目录，首行 system 即**目标** AGENTS.md 的组装结果（`promptBuilder` 机制零改动，`/load` 仍逐字节可还原）。
- **跟随是活的**：`tools.Options.Workspace`（`run_shell`/`read_image`）与 `repl.cwdBase`（`@路径` 基准、补全）都是活闭包，工作区一换自动跟随——工具侧零改动。

### 13.3 失败语义（用户拍板 b2：整次作废）

目标不可用（目录已删、会话目录建不出来等 TOCTOU）时：

| 项 | 行为 |
|---|---|
| 工作区 | **不变**（`loadWorkspace` 失败即未动：`MkdirAll` 是首条语句） |
| 会话 | **不 rotate**、不产生新会话文件 |
| 旧会话 | 不写移交事实行；只追加 `[会话交接未成立：<原因>；本会话继续，工作区仍为 <路径>]` |
| `Handoff` | `Failed=true`、`Err=<原因>`、`NewID=""`，经 `TakeHandoff` 交给表现层 |
| REPL | `错误: 工作区切换失败（…），本次交接未成立；当前会话继续（工作区仍为 %路径）`；**不重置会话计时、不续跑**（命令路径的 `<指令>` 也不执行）、回提示符等输入 |

理由（用户原话）：**不应当在错误的工作区执行未知操作而导致更严重风险**——宁可停住。那行未成立事实行既进 history 也进文件，后续回合与模型据此得知交接没成、自己仍在原工作区（摘要可能写着「下一步去 B 做 X」，不写这一行模型会误判自己在 B）。

### 13.4 表现层

- 收尾块 = 旧会话三行 + `已移交新会话 X（交接 N 行）` + （切换时）`工作区已切换: <旧> → <新>`（`initPath` 显示）。
- 切换成功后 `rebindAttach()`：run 目录与 sessions 同 base，不重绑则新工作区 `tanya attach --list` 看不到本实例、旧 run 项成僵尸。
- 失败时走 `failText`（stderr，不受输出模式屏蔽）；`nextPending/nextInput` 已被 `afterTurn` 消费，命令路径不会再补一句 `未生成交接摘要`。

### 13.5 测试与验收

- `agent/handoff_test.go` +5：切换落点/快照/文案/条数、同目录三种写法、非法与空白参数、`loadWorkspace` 失败与目标消失两条作废路径、`-n` 内存内切换。
- `repl/handoff_test.go` +4：模型带 `workspace` 的真流程（含续跑轮用活闭包探针确认新工作区）、`rm_dir` 桩制造 TOCTOU 的作废路径、`/next <指令>` 在作废下不执行参数、attach 重绑。
- 变异复验：忽略 `loadWorkspace` 错误 / 切换后改用 `a.save()` / 去掉 `rebindAttach` / 失败时照常交接——四个变异各有用例变红（第一条最初存活，促成 `TestHandoffWorkspaceLoadFailureAborts` 补测）。
- 真机 pty（假 LLM，两条路径）：成功路径见 §13.2/§13.4 全部行为（含续跑轮 `pwd` 落在新工作区）；作废路径 `请求数=3`、终端一行错误、会话未轮转、无移交事实行。首版文案嵌套的 `错误: …（error: …）` 双层前缀在验收中暴露并修掉（`applyHandoff` 里改抛不带前缀的 `MsgHandoffBadWorkspaceFmt`）。

### 13.6 不做

按用户口径：`/next` 命令的工作区参数（跨项目时说清目标、由模型传参）、`/switch` 的「带摘要切换」、`system_prompt.md` 的能力提示、任何新配置键；也不做切换的等待/重试（失败即作废，由用户或模型重新发起）。
