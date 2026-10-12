# 接管运行中的实例（`tanya attach`）实施文档

状态：**已实施**（2026-10-13，P0–P3 落地；口径由用户多轮收敛，见 §9）。现行约束摘要见 `AGENTS.md`。

## 1. 目标与语义

在同一工作区的另一个终端里启动新进程 B，接管正在运行的实例 A：**B 成为该会话的唯一后续写者，A 平滑退出**。

- 接管的是**会话**，不是进行中的回合：A 的当前回合按 `force` 决定「等待其结束」还是「立即中断」，B 从最后一个合法历史边界续起（全量历史，不做摘要——这是与 `/next` 的本质区别）。
- **没有 kill**：A 交接完成后自己退出（`REPL.quit()`，退出码 0）；B 拿到应答即开工，不探测、不等待、不清理 A 的任何东西。
- 发现域 = **当前工作区**（同 run 目录才互相可见），workspace 判定与 `session_mode` 用的是同一套 base。

## 2. 不变量

1. **单写者**：A 在应答接管前先 `FreezeSession()`（此后 `append` 为 no-op），**应答即承诺已停写**。B 因此可以不等 A 退出就续写。
2. **交接顺序固定**：`AppendNotice("[会话已由另一进程接管]")` → `FreezeSession()` → 应答 → `Server.Close()` → farewell → 退出。前两步必须都先于应答（B 读文件时事实行已在；顺序错乱即双写）。
3. **续写同一会话文件、同一 id**：不 rotate、不新建；system 首行快照由 `AdoptSession` 采纳（`promptBuilder.adopt`），首行绝不改写（缓存前缀不变）。
4. **run 目录与 sessions/archive 同 base**：`local`/`auto`（存在 `.tanya`）→ `<ws>/.tanya/run`；`global` → `$DataDir/workspaces/<workspaceID>/run`。`-n`（不落盘）不建目录、不注册。
5. **零新依赖、零新配置键**：只用标准库（`net` 的 `unix`）；`config.example.yaml` 与 `tanya config` 输出逐字节不变。

## 3. 发现、注册与状态

run 目录里每个实例两个文件（`attach` 包）：`<pid>.json`（原子重写：tmp + rename）、`<pid>.sock`（0600）。

注册项字段（`attach.Entry`）：`v/pid/sock/tty/title/state/turn_started/session/messages/archived/started/updated`。

| 刷新点 | 内容 |
|---|---|
| 启动 | 全部字段 |
| 进入回合 | `state=busy` + `turn_started`（此后**回合期间零写入**，接管方本地折算耗时） |
| 离开回合 | `state=idle`、清 `turn_started`、标题/消息数（首条 user 消息此时已入库） |
| 会话变化（`/new` `/load` `/fork` `/next`） | 会话 id、标题、消息数 |
| `/switch <dir>` | 旧注册删除 + 新目录重绑 socket（base 变了） |
| 退出 / 移交 | 删除 `.json` + `.sock` |

- `state` 三值：`idle` / `busy` / `readonly`（归档只读会话注册并显示，但不可接管）。
- `title` = 会话首条 user 消息摘要（30 字，`agent.Summarize`），跳过 tanya 自己写的单行方括号事实行；无内容时回落 `tanya · <工作区名>`。
- 陈旧项**只跳过不删除**：判活靠 `attach.Alive`（connect 探测，`ECONNREFUSED` 即陈旧），不做 kill、不做 pid 复用比对。
- 选择：`--pid N` > 唯一实例；0 个报错，多个报错并列出候选（绝不自动选）。

## 4. 协议（`attach` 包，JSON 流一条连接）

```
C→S  {"hello":{"magic":"tanya-attach","v":1}}          // 无 token：鉴权交给文件系统（0600/0700）
S→C  {"hello":{"magic":"tanya-attach","v":1,"pid":41234}}
C→S  {"takeover":{"force":false}}
S→C  {"result":{"ok":true,"session":…,"file":…,"session_dir":…,"archive_dir":…,
                "workspace":…,"model":…,"effort":…,"protocol":…,"messages":N}}
     {"result":{"ok":false,"code":"no-save"|"read-only"|"in-progress"|"no-session"|"io-error"|"bad-request"}}
```

- 无 `wait` 参数、无进度消息、无周期心跳、无 `status` 命令：等待语义是**要么一直等，要么 `force`**；等待期的状态展示读 run 文件（B 非 force 等待期每 5s 重读一次注册项，目标执行中（含受理后才变忙）刷一行「目标仍在执行中（已 …）」，秒数按 `turn_started` 本地折算；A 不需要为此写盘）。
- 握手超时 3s；**取消 = 关闭连接**（B 的 `^C` 走这条路），A 侧由连接断开事件得知（见 §5），不改写会话。
- 版本不一致：服务端回自己的 `hello` 后关闭，客户端明确报错（不猜兼容）。

## 5. 两条流程

**A（被接管方）**：`attach.Server` 收请求 → `REPL.Takeover(force, peer, reply)`

```
拒绝面：已有存活挂起 / -n / 归档只读 / 无会话    → 立即应答 ok:false
busy + force : 注入 EventInterrupt 取消当前回合  → 回合出口交接
busy + !force: 静默挂起                          → 回合自然结束交接
idle         : 注入 EventTakeover               → Readline 返回 ErrTakeover 交接
交接点（Run 循环顶部 / ErrTakeover 分支）settleTakeover：
  ① AppendNotice → ② FreezeSession → ③ 交付结果给 serve goroutine 写出
  ├ 写出成功 → Server.Close（删 sock/注册项）→ farewell + 「已交由另一进程接管本次会话」→ 退出 0
  ├ 事实行写不进（会话目录不可写等）→ 应答 io-error → 回滚（解冻、补 [接管未成立]、提示）→ 继续运行
  └ 对端已断开（gone）或写出失败 → 追加 [接管未成立] → 解冻 → 丢弃挂起 → 继续运行
回滚与挂起摘除统一清掉未被编辑层消费的注入事件（`readline.Drainer` 可选接口）：force 的中断不再空中断下一行输入。
```

**挂起项的生命周期**（改动：2026-10-13，用户报告「接管过一次后，后续请求都被 in-progress 拒掉」）：挂起项 `REPL.takeH` 有**两个**摘除点，谁先到谁摘——

1. **对端断开即摘**：serve goroutine 的等待 `select` 除 `deliver`/`abort` 外还等 `peer.Gone()`，对端一断就摘除（不再占位、不再等交接点）；
2. **受理前判陈旧**：新请求到来时若挂起项的对端已断（EOF 尚未被读到，或摘除擦肩而过），就地丢弃后照常受理本次请求——`in-progress` 只在挂起请求**确实存活**时回，`force` 也不例外（该检查在 force 分支之前）。

摘除只动**挂起状态**、不碰会话：取消路径仍零改写（不 append、不 freeze），A 的三步顺序与「渲染单 goroutine」都不变——serve goroutine 不 emit，只把一个「已取消」计数留在 `takeGone`，由主循环在下一个交接点（Run 循环顶部）消费并打出「接管请求已取消」（计数消费式，断开与报告擦肩不丢也不重复）。因此提示可能出现得比断开晚（忙时=本回合结束），但**释放是即时的**。

**B（接管方）**：`main.runAttach` → 发现/列表/选择 → `attach.Open` + `Takeover(force)`（等待期每 5s 重读注册项刷新「目标仍在执行中（已 N）」、`^C` 关连接取消）→ 校验（`ok`、`api_protocol` 一致、`file` 非空）→ 常规启动流程 → `agent.AdoptSession(result.File)` + `SetModel`/`SetReasoningEffort` → REPL 启动块「已接管会话 X（来自另一进程 · 工作区）」→ 提示符。接管后的 B 自己照常注册，可被再次接管（链式）。

## 6. 平台

- 传输 = `net.Listen("unix", path)` / `net.Dial("unix", path)`：**一份实现、三平台**（Windows 需 10.0.17063+，Go 内部用 `WSAEnumProtocols` 探测 `AF_UNIX`）；运行时不可用 → `Serve` 失败 → 打一行提示、不注册（不做 TCP 回退）。socket 路径超 100 字节时退到 `os.TempDir()/tanya-run-<pid>/`。
- 注入唤醒**不需要自管道**：posix raw 模式本就 `VMIN=0/VTIME=1`（每 100ms 一次 `errIdle`），Windows 控制台读路径自带 5ms 轮询 + 1s 上限——`Console.Post` 只入队，读路径的周期回环即取走（`readline.Poster` 是可选接口，管道设备不实现 → 不注册）。
- 口径外的平台保持原状（无 `EventTakeover` 消费方、无注册）。

## 7. 边界与失败面

| 情形 | 行为 |
|---|---|
| B 在忙时取消（^C / 断开） | A 在**连接断开时即摘除挂起**（后续请求随即恢复可接管），提示「接管请求已取消」由主循环在下一个交接点打出；**会话零改写**，继续运行 |
| B 拿到 ok 后崩 | A 照常自退；会话含事实行、无人续写 → 新开 tanya `/load` 即可 |
| A 在 freeze 前崩 | 连接断、B 无 result → 报错退出；会话文件无损 |
| A 在 freeze 后、应答前崩 | 同上（B 未接手）；A 已停写，无影响 |
| A 被 SIGKILL | 注册项/socket 残留 → `--list` 标陈旧并跳过（不删） |
| 两个 B 同时接管 | 先到先得（挂起存活时），后者得 `in-progress`；前者一旦断开即让位 |
| 目标被 `^Z` 冻结 | 连接可建但无应答 → B 一直等（符合"要么一直等"），`^C` 可取消 |
| 空会话（尚无消息） | 允许：`AppendNotice` 会落盘建立文件，B 接管到 0 条历史 + 事实行 |
| `-n` / 归档只读 | A 拒绝并给码；B 侧中文提示 |
| 事实行写不进（磁盘故障/目录不可写） | A 应答 `io-error` 并回滚继续运行；B 报「对方写入会话失败，接管未成立」 |
| force 接管被取消 | 残留的注入中断被清除，不会空中断 A 的下一行输入 |

## 8. 明确不做

接管进行中的流式响应或借出中的子进程；跨机/跨用户接管；`tanya ask` 单发进程；A 的 detach/reattach（无终端存活）；摘要式交接（那是 `/next`）；陈旧项自动清理（`--prune`）；鉴权 token（同用户进程无收益，见 §9）。

## 9. 关键取舍（用户拍板记录）

| # | 决策 |
|---|---|
| 1 | A 交接后**自行退出**，B **不做任何**探测/等待/清理；全流程无 kill |
| 2 | 忙时默认**等待回合结束**，`--force` 才中断；不做超时（要么一直等，要么强制） |
| 3 | 等待期状态靠**共享 run 文件**（A 状态变更时写一次，B 每 5s 读一次并本地折算耗时），协议无心跳 |
| 4 | run 目录**与 session 同 base**（同一套机制），发现域限当前工作区 |
| 5 | 注册项只放展示信息（含 `title`）；交接数据由应答**现取**，避免陈旧 |
| 6 | **不要 token**：同用户进程可读同一文件，跨用户由 `0600/0700` 挡住；`hello` 只留 `magic`+`v` |
| 7 | 事实行不带 pid（无可信来源），统一为 `[会话已由另一进程接管]` |
| 8 | 传输用 AF_UNIX（Go 一份实现三平台），不做平台分片/TCP 回退 |
| 9 | 会话**续写同文件同 id**，不 rotate |

## 10. 验收（2026-10-13）

- `gofmt -l` 干净；`go build ./...`、`go vet ./...`、`go test ./...` 全绿；`go test -race ./agent/ ./repl/ ./attach/ ./readline/` 通过；`GOOS=windows go build/vet ./...` 与 `GOOS=darwin go build ./...` 通过；`tanya config | diff - config.example.yaml` 为空。
- 真机 pty 端到端（`make build` + 两个真实进程）：
  - 空闲接管：`attach --list` 显示 pid/tty/状态/标题/会话；`attach` 后 B 打印「已接管会话 …（来自另一进程 · ~）」并接手提示符，A 打印 farewell + 「已交由另一进程接管本次会话，本进程退出」、**退出码 0**，run 目录两项清空，会话文件末尾为 `[会话已由另一进程接管]`。
  - 链式：接管后的 B 出现在 `--list`（新 pid/新 tty、同一会话 id）；`SIGTERM` B → 注册项随之清除。
  - `--pid 999999` 报「没有 pid 999999 的运行实例」；`--bogus` 报未知参数。
  - 取消后立即重接管（2026-10-13 修复的真机回归，pty：假 LLM 挂住使 A 停在 busy）：原始 socket 发一条忙时非 force 请求并**硬关连接**（= B 的 `^C`）→ 立刻跑 `tanya attach --force`：新二进制接管成功（客户端退出码 0、打印「已接管会话 …」；A 打印「已中断」→ 回合分隔线 →「接管请求已取消」→ farewell +「已交由另一进程接管本次会话」、退出码 0、run 目录清空），改动前的二进制同一脚本报「对方已有接管请求在处理」、退出码 1。
- review 修复（2026-10-13 同日第二轮）：删 `attach.Server` 死字段 `conns`；`attachRefused` 补 `bad-request`/`io-error` 映射；等待 ticker 非 force 一律武装（目标后来变忙也有进度行）；归档确认行收到 `ErrTakeover` 静默中止；force 回滚清残留注入（`readline.Drainer`）；事实行写不进应答 `io-error` 并回滚；`shortDur` 钳 0。`Alive` 探测占 backlog 与临时目录 pid 撞名核实为安全、留档不改。新增 `readline/post_test.go` 2 例、`repl/attach_test.go` 2 例、`repl/archive_test.go` 1 例、`main_test.go` 3 例；全量 `go test ./...`、`go test -race`（repl/attach/readline/agent）、`GOOS=windows build+vet`、`GOOS=darwin build` 通过。
- 单测：`attach/` 8 例（握手/接管/版本不符/魔数不符/对端断开/陈旧跳过/socket 0600 与残留覆盖/Close 清理）；`repl/attach_test.go` 9 例（注册项字段与刷新、空闲端到端移交并退出、忙+非 force 不注入、忙+force 注入中断、对端断开即释放挂起且不改写会话（含取消提示恰好一次）、取消后新请求（force）不被 in-progress 挡、陈旧挂起不挡新请求、并发与 -n 拒绝、标题跳过事实行）；`agent/attach_test.go` 6 例（run 目录同 base 与 -n、AdoptSession 续写同文件且首行不改写、冻结停止写入、事实行、ArchiveDir/ApiProtocol）。
