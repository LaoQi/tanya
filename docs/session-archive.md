# 会话归档（zip 卷）实施文档

状态：设计已确认（2026-09-17），按 P0–P3 实施。设计口径以 `docs/design.md`《会话》《会话归档与 fork》为准，本文件只管落地顺序、接口签名、文案与验收；进度以文末勾选表为准。

## 1. 目标与不变量

目标：给历史会话一个无损冷存手段（压缩 + 移出活动区），并且**不解压即可列会话**（读 zip 中央目录 + entry 注释）；继续对话一律 fork 成新会话。

不变量（违反即回滚该改动）：

1. **归档只做字节级无损压缩**：entry 数据 = 原 `.jsonl` 逐字节（不裁剪、不重排、不丢 reasoning/tool_calls）；载入解码后的 history 与归档前一致——prompt cache 红线在归档路径上的延续。
2. **卷写入后不可变**：不重写、不删条目、不提供「解档回活动区」。因此实现中**不出现** `zip.Writer.CreateRaw` / `File.OpenRaw`。
3. **不兼容旧布局**：删除 `global_session` 配置项；代码与文档不出现旧目录的兼容读取、探测提示与迁移路径。
4. **只读态与 `-n` 正交**：`frozen`（本会话来自归档）与 `disabled`（`-n` 全程只读）分别置位，两者都让 `append` 直接返回。
5. **零新依赖**：只用标准库 `archive/zip`（+ 既有依赖）。

## 2. 目录与配置（最终形态）

```
local   <repo>/.tanya/                                   ← workspace 目录
            sessions/<id>.jsonl
            archive/archive-<20060102-150405>.zip

global  <data_dir>/workspaces/<workspace-id>/            ← workspace 目录
            sessions/<id>.jsonl
            archive/archive-<20060102-150405>.zip
```

| 项 | 处理 |
|---|---|
| `data_dir` / `TANYA_DATA_DIR` | 新增；默认 `~/.local/share/tanya`；`~` 展开（复用 `expandHome`） |
| `global_session` | 删除字段、yaml 键、默认值、文档与测试引用 |
| `session_mode` / `TANYA_SESSION_MODE` | 不变（`auto` = 存在 `.tanya/` 则 local，否则 global） |

推导单点：`resolveWorkspaceDirs(cfg, cwd) (sessions, archive string)`；`resolveSessionDir` 删除（无兼容 wrapper）。`agent.New` 只 `MkdirAll(sessions)`；`archive` 由归档动作按需创建；`-n` 下不建任何目录。旧目录（`~/.local/share/tanya/sessions/`）由用户手工删除，README 一句话说明。

## 3. 归档卷格式

- 卷名 `archive-<20060102-150405>.zip`（时间取 `ArchiveOptions.Now`，测试可注入）；写入用同目录临时文件 `archive-<ts>.zip.tmp-*`（`os.CreateTemp`），`Close` 后 `os.Rename` 落定。临时名不以 `.zip` 结尾，列表扫描按后缀只认 `*.zip`，临时卷天然不入列表。
- entry：名 `<会话 id>.jsonl`，`Method: zip.Deflate`，`Modified` = 源文件 mtime，数据 = 源文件逐字节。
- entry comment（单行 JSON）：`{"v":1,"msgs":N,"summary":"…"}`；summary = 首条 user 消息单行化、截断 200 rune；截断时加 `"trunc":true`；marshal 后超过 `zipCommentLimit = 4096` 字节则把 summary 降到 80 rune 再试，仍超则丢弃 summary 只留 `{"v":1,"msgs":N}`。
  - **硬约束**：Go 在 comment > 65535 字节时静默写坏中央目录（实测 65536 → 读回 0 字节），所以上限与降级是必需的，且要有测试。
- 卷级 comment：`{"v":1,"workspace":"<cwd>","created":"<RFC3339>","sessions":N}`（不含版本号，避免 agent 侧依赖版本注入）。
- 元数据来源：条数/摘要由归档时的 `scanSession`（复用现有扫法）产出；大小/mtime 由 zip header 承载，不写进 comment。

## 4. 组件设计

### agent/session_archive.go（新文件）

```go
const (
	archiveVolumePrefix  = "archive-"
	archiveVolumeSuffix  = ".zip"
	archiveTempSuffix    = ".tmp-"
	zipCommentLimit      = 4096
	archiveSummaryRunes  = 200
	archiveSummaryMin    = 80
	archiveIdleGuard     = 5 * time.Minute
	defaultArchiveWindow = 30 * 24 * time.Hour
)

type ArchiveOptions struct {
	OlderThan time.Duration // mtime ≤ Now-OlderThan；0 = 不限
	Keep      int           // 保留最新 N 个活动会话；0 = 不限
	Exclude   string        // 当前会话 id，永不归档
	DryRun    bool          // 只出报告不落卷（/archive 的预览阶段）
	Now       time.Time     // 零值取 time.Now
}

type ArchiveEntry struct{ ID string; Before, After int64 }
type ArchiveSkip  struct{ ID, Reason string }
type ArchiveFail  struct{ ID string; Err error }

type ArchiveReport struct {
	Volume      string
	DryRun      bool
	Sessions    []ArchiveEntry
	Skipped     []ArchiveSkip
	Failed      []ArchiveFail
	RawBytes    int64
	VolumeBytes int64
}

func (s *sessionStore) archive(opt ArchiveOptions) (ArchiveReport, error)
```

流程：候选筛选（活动区 `*.jsonl`，按 id 降序套 `Keep`，去 `Exclude`，按 mtime 套 `OlderThan` 与空闲保护，已在任一卷内则 `Skipped`）→ `DryRun` 直接返回 → `MkdirAll(archiveDir)` → 建临时卷 → 逐会话 `scanSession` 取元数据 + 流式 copy 数据 → `SetComment` → `Close` + `Rename` → 逐个 `os.Remove` 源文件。
失败语义：扫元数据失败 → 记 `Failed` 跳过该文件继续；写 entry 失败 → 删除临时卷、**不删任何源文件**、返回 error（附失败的 id）。`Failed`/`Skipped` 非空不阻断整批。

### agent/session.go（改造）

```go
type SessionInfo struct {
	ID       string
	ModTime  time.Time
	Msgs     int
	Summary  string
	Path     string
	Archived bool
	Size     int64  // 归档项：entry 未压缩字节；活动项：文件字节
	MetaOK   bool   // 归档项 comment 可解析（活动项恒 true）
}

type sessionStore struct {
	dir        string // sessions（可写）
	archiveDir string // archive（只读；仅归档动作写入）
	disabled   bool   // -n
	frozen     bool   // 当前会话来自归档（只读）
	file       string
	saved      int
	systemSaved bool
	cache      map[string]SessionInfo
	stat       map[string]sessionFileStat // 会话文件 (mtime,size)
	volStat    map[string]sessionFileStat // 归档卷 (mtime,size)
	volumes    map[string][]SessionInfo   // 卷路径 → 条目（保留卷内顺序）
}

func newSessionStore(dir, archiveDir string, disabled bool) *sessionStore
func (s *sessionStore) loadFrom(r io.Reader) ([]Message, string, error) // load() 抽出的公共读法
func (s *sessionStore) findArchived(id string) (volume string, ok bool)
```

`refresh()`：先扫 `dir/*.jsonl`（现有逻辑不动），再扫 `archiveDir/*.zip`（按后缀过滤，临时卷不被匹配）：`zip.OpenReader` 读 CD，逐 entry 用 `filepath.Base(f.Name)` 去 `.jsonl` 得 id、`f.UncompressedSize64` 与 `f.Modified` 取大小与时间、解析 `f.Comment` 取 `Msgs`/`Summary`（解析失败 → `MetaOK=false`、`Msgs=0`、`Summary=""`），把条目并入 `cache`（同 id 已有活动项则不覆盖）与 `volumes`。
`list()`：活动组按 id 降序、归档组按 id 降序，活动在前。
`load(id)`：活动区命中走原路径（清 `frozen`）；否则 `findArchived` → `zip.OpenReader` → `loadFrom(entry.Open())` → `frozen=true`、`file=""`、`saved=len(history)`、`systemSaved=true`（system 已在 history 首行分离出来）。
`append` 首行 `if s.disabled || s.frozen || s.saved >= len(msgs) { return nil }`。

### agent/agent.go / control.go / repl（接线）

- `Agent.ArchiveSessions(opt) (ArchiveReport, error)`（门面，同 `ListSessions` 风格）。
- `Agent.Fork() (string, error)`：非 `frozen` 返回 `ErrForkNotArchive`；`store.rotate()` → 清 `frozen` → `prompt.reset()` → `stats.reset()` → `_ = a.save()` → 返回新 id（`-n` 下 `save` 是 no-op，仍返回 id 供提示）。
- `Agent.ArchiveReadOnly() (id string, ok bool)`；`Stats` 加 `Archived string`（frozen 时的归档 id）。
- `Agent.Ask` 首行：`if _, ok := a.ArchiveReadOnly(); ok { return ErrArchiveReadOnly }`。
- `Agent.SessionFile()`：`frozen` 或 `disabled` 时返回空。
- `agent/control.go` `readStat()`：`Stats.Archived != ""` 时用新文案（`MsgControlStatArchive`），不显示空路径（现实现会把空串 `filepath.Base` 成 `.`）。
- `repl/picker.go` 两个调用侧 + `PickCompleteItem` 调用侧：摘要列前拼 `SessArchMark`（`[归档] `），格式串 `SessRow`/`PickCompleteItem` 不动（`repl/sessrow_test.go` 的动词计数断言保持有效）。

## 5. 交互与文案

| 位置 | 行为 |
|---|---|
| `/archive` | 仅完整交互环境（rich 输出 + `r.raw` + `r.prof.TTY`）启用，其余环境只提示 `MsgArchiveOnlyTTY`；无参 = `Keep=auto_archive_keep`、纯数字 = `Keep=n`（`0` = 除当前会话外全部）、带单位（`7d`/`12h`，单段单单位 `d`/`h`/`m`/`s`）= `OlderThan` |
| `/archive` 输出 | 两阶段：预览 `当前活跃会话 A 个；将归档 N 个（约 X[，保留最近 K 个]）。` + 跳过/失败行 → `现在归档？[y/N] `，`y`/`yes` 后 `已归档 N 个会话 → <卷名>（<原大小> → <卷大小>）`，其它输入 `已取消，未归档`；`A` = `ArchiveReport.Active`（操作前活跃会话总数），`保留最近 K 个` 仅在 `Keep > 0` 时出现；0 命中按口径给 `MsgArchiveNoneKeep` / `MsgArchiveNoneWindow`（窗口文案直接回显原参数；`Keep=0` 用通用 `没有符合条件的会话`）；确认答案经 `readConfirm` 不入输入历史 |
| 解析 | `repl.ParseArchiveArg(arg string, defaultKeep int) (agent.ArchiveOptions, error)`：空参 → `Keep=defaultKeep`、纯数字（`^[0-9]+$`）→ `Keep=n`、单段单单位（`^([0-9]+)(d|h|m|s)$`，`d` 按 24h 换算）→ `OlderThan`，其余（复合时长如 `12h30m`、多段、数值 ≤ 0、溢出）报非法参数；`handleArchive` 把 `parts[1:]` 以空格 join 后传入 |
| `/load <归档 id>` | `已载入会话 X（归档只读，继续对话请 /fork）`；picker 行摘要前带 `[归档] ` |
| 只读态对话 | REPL 在对话分支前拦截（含 `:`/`：`）→ `当前为归档只读会话（X）；继续对话请 /fork 开新会话`；`agent.Ask` 兜底 `ErrArchiveReadOnly` |
| `/fork` | 归档只读态 → `已 fork 为新会话 <新 id>`；非归档态 → `当前会话不是归档只读会话，直接对话即可`；`-n` 下追加一行 `（不落盘模式，未写入）` |
| `/stat` | 归档只读态显示 `会话: 归档只读 X（未写入）` |
| 退出收尾 | 归档只读态文件行显示 `会话文件 未写入（不落盘模式）` |
| 启动自动归档 | 默认开启（`auto_archive: false` 关闭）；与 `/archive` 共用 `REPL.archiveFlow`（`repl/archive_flow.go`），提示与确认完全同一条：`当前活跃会话 A 个；将归档 C 个（约 X，保留最近 K 个）。` + `现在归档？[y/N] `，`y`/`yes` 归档，其它/空行 → `已取消，未归档`。仅调用时机与触发条件不同：`autoArchivePrompt()` 在欢迎屏后进循环前调用，门禁为 `archiveInteractive()`（rich 输出 + `r.raw` + `r.prof.TTY`，等价于「plain 与 stdout 非终端静默返回」），再过 `SuggestArchive`（`auto_archive` 关闭、`-n`、活跃数 < `auto_archive_threshold` 均不问）；`C/X` 已剔除当前会话（先占 `keep` 名额再剔除，与 `archive()` 同序），`A` 取 `ArchiveReport.Active` |

新增常量：`repl/messages.go`（`MsgArchiveDone` / `MsgArchiveNone` / `MsgArchiveSkipFmt` / `MsgArchiveFailFmt` / `MsgArchivePreview` / `MsgForkDone` / `MsgForkNotArchive` / `MsgForkNoSave` / `MsgLoadArchived` / `MsgArchiveReadOnlyFmt` / `SessArchMark` / `slashCommands` 增 `/archive` `/fork` / help 文案），`agent/messages.go`（`ErrArchiveReadOnly` / `ErrForkNotArchive` / `MsgArchiveVolFailFmt` / `MsgControlStatArchive`）。数字与大小格式化归 repl（沿用 `stats.go` 既有缩写口径）。

## 6. 实测数据与风险

2026-09-17 用本仓库 `.tanya/sessions/`（156 个会话、39.2 MB）跑一次性探针（`archive/zip` + 真实文件，探针未入库）：

| 指标 | 实测 |
|---|---|
| 打包一卷（扫元数据 + Deflate + 注释） | 13.1 MB（3.0×）、0.91 s |
| 只读 CD 列 156 条 + 注释 | 0.1 ms |
| 载入单会话（解压到内存） | 0.8 ms，与源文件字节一致 |
| entry comment 上限行为 | 65535 字节可往返；65536 → 读回 0（静默写坏）；70000 → 读回 4464 |
| 外部互操作 | `unzip -l/-p/-z` 均可用；`unzip` 打印注释按本地码页转码可能乱码 |

风险与对冲：

- **并发追加**：归档排除当前会话 + 5 分钟空闲保护；卷名带时间戳且经 `CreateTemp`，无覆盖竞争；崩溃最多留双份（源未删），列表按同 id 取活动。
- **半写卷**：只通过临时文件 + `rename` 落地，临时卷名不以 `.zip` 结尾因而不入扫描；写失败即弃卷。
- **损坏卷**：列表阶段不崩（条目仍在，`MetaOK` 可为 false）；载入阶段 CRC32 报错上抛，卷不动。
- **长注释**：见上表，硬上限 + 降级 + 测试。
- **只读态误判**：`frozen` 只在 `load` 归档命中时置位；`NewSession`/活动 `load`/`Fork` 一律清零。

## 7. 分期

### P0 配置与目录规划

改动：`agent/config.go`（删 `GlobalSession`，加 `DataDir` + `TANYA_DATA_DIR`）、`agent/session.go`（`resolveWorkspaceDirs`，删 `resolveSessionDir`，`newSessionStore` 增 `archiveDir`）、`agent/agent.go`（`New` 接线）、`agent/init.go`（报告用新推导）、`config.example.yaml`、`README.md`、`docs/agent-control-tool.md`（两处 `global_session` 引用）、测试引用（`grep -rn "GlobalSession" --include=*.go .`，约 12 处；`agent_test.go` 的 `TestResolveSessionDir` 改名 `TestResolveWorkspaceDirs` 并断言 `workspaces/<wid>/{sessions,archive}`；`ask_nosave_test.go` 的 `os.ReadDir(cfg.GlobalSession)` 改指 sessions 目录）。

验收：`go build ./... && go vet ./... && go test ./...`；`./tanya -m global` 后 `/stat` 显示 `<data_dir>/workspaces/<wid>/sessions/<id>.jsonl`；`tanya init` 报告 `.tanya/sessions`；`grep -rn global_session` 只剩本文件与历史文档（`docs/design.md` 的「已废弃」说明）。

### P1 归档卷与只读载入

改动：新增 `agent/session_archive.go`；`agent/session.go`（双区 refresh、卷索引、`SessionInfo` 扩展、`loadFrom`、`frozen`、`append` 门、`path()`/`id()`）；`agent/agent.go`（`ArchiveSessions`、`ArchiveReadOnly`、`Ask` 兜底、`SessionFile`）。

测试（`agent/session_archive_test.go`）：卷往返（entry 字节、comment 元数据与 `scanSession` 一致）、comment 上限与多字节截断降级、筛选矩阵（`OlderThan`/`Keep`/`Exclude`/空闲保护/`DryRun`/已归档 id 去重/0 候选不建卷不建目录）、启动自动归档（`SuggestArchive` 阈值边界与 `-n`/关闭态、接受与拒绝两条路径、配置校验）、临时卷被忽略、损坏卷（截断与 CRC 篡改）列表不崩载入报错、同 id 双区取活动、只读载入不改卷且不写盘（对比卷 mtime 与目录清单）、`Ask` 返回 `ErrArchiveReadOnly`、`resolveWorkspaceDirs` 三态推导。mtime 用 `os.Chtimes` 造，时间用 `ArchiveOptions.Now` 注入。

### P2 交互（`/archive`、只读拦截、`/fork`）

改动：`repl/dispatch.go`（`ParseArchiveArg`、两个新命令分发、只读拦截）、`repl/repl.go`（`handleCommand` 分支、`loadSessionInteractive` 文案）、`repl/completer.go`（`slashCommands` 增项、`/archive` 参数补全可选）、`repl/picker.go`（`SessArchMark`）、`repl/messages.go`、`agent/messages.go`、`agent/agent.go`（`Fork`）、`agent/control.go`（`MsgControlStatArchive`）、`repl/stats.go`（归档只读态显示）。

测试：`TestParseArchiveArg` 表驱动（空/纯数字/`7d`/`12h`/`90m`/非法）、`TestForkFromArchived`（新文件内容、条数、后续 append 增量、system 取当前快照）、`TestForkRejectedWhenNotArchived`、`TestArchiveReadOnlyBlocksDialogue`（含 `:` 前缀）、`TestSessRowArchivedMark`、`TestHandleCommandArchiveFork`；P2 收口跑 `go test -race ./...`。

### P3 文档与收尾

`docs/design.md`（已更新，实施中若有偏差回改）、`README.md`（配置表 `data_dir`、会话目录表、斜杠命令表、归档小节、旧目录删除说明）、`AGENTS.md`（设计约束加一条：归档卷 + 只读载入 + fork；结构与文档清单补 `docs/session-archive.md`）、`config.example.yaml`、`docs/todos.md`（完成后清理相关条目）、本文件勾选表收尾。

## 8. 手工验收

```bash
make build
./tanya                       # 造几条会话（随便问几轮）
/archive                      # 保留 auto_archive_keep 个：先出报告，输入 n 取消
/archive 0                    # 报告后输 y：除当前会话外全部归档
/archive 7d                   # 按未活动时长筛选
unzip -l .tanya/archive/archive-*.zip
/load                         # picker 里归档项带 [归档] 前缀
/load <归档 id>               # 只读载入 → 提示 /fork
你好                          # 被拦截，提示先 /fork
/fork                         # 新会话 X，立即落盘
你好                          # 正常对话并追加到新文件
/stat                         # 会话文件指向新文件
/exit                         # 收尾显示会话文件
```
`-m global` 重复一遍（检查 `<data_dir>/workspaces/<wid>/{sessions,archive}`），`-n` 下重复（检查不写盘、fork 提示未写入）。

## 9. 明确不做

内容裁剪/摘要压缩；卷重写与「解档回活动区」（`CreateRaw`/`OpenRaw`）；gz 单文件归档；CLI `archive`/`migrate` 子命令；`--note`/`--list`；旧布局兼容读取与自动迁移；TTL 淘汰与自动清理（启动自动归档见 §5 与 `docs/design.md`《启动自动归档》，属用户确认式提示，不做无人值守归档）；LLM 生成描述；新依赖。

## 10. 进度

| 阶段 | 内容 | 状态 |
|---|---|---|
| P0 | `data_dir` + `workspaces/<wid>/{sessions,archive}` | 已完成（2026-09-17） |
| P1 | 归档卷 + CD 索引 + 只读载入 | 已完成 |
| P2 | `/archive`、只读拦截、`/fork` | 已完成 |
| P3 | 文档收尾（README/AGENTS/example/todos） | 已完成 |
| P4 | 启动自动归档（配置三键 + `SuggestArchive` + REPL 启动提示 y/n） | 已完成 |

验收记录（2026-09-17）：`go build ./... && go vet ./... && go test ./... && go test -race ./agent/ ./repl/` 全绿；`./tanya -m global` 落 `<data_dir>/workspaces/<wid>/sessions/`（`TestNewSessionPerWorkspace`、`TestResolveWorkspaceDirs` 断言）；真实二进制 pty 冒烟（临时工作区、`make build` 产物）三条路径全通：
- 默认（local）：`/archive`（一卷 3 条目，二次调用提示无候选）→ `/load`（picker `[归档] ` 标记 → 只读载入）→ `你好` 被拦截 → `/stat` 只读行 → `/fork`（立即落盘、继承 11 条历史）→ `/stat` 指向新文件 → `/exit` 收尾显示会话文件；
- `-n`：`/archive` 报「不落盘模式（-n）下不能归档会话」→ 归档只读载入 → 对话被拦截 → `/fork` 提示「（不落盘模式，未写入）」→ `/exit` 收尾为「会话文件 未写入（不落盘模式）」，会话目录未新增文件；
- `-m global`：`/stat` 落点为 `<data_dir>/workspaces/<wid>/sessions/`。
`unzip -l/-p/-z` 与 `python3 zipfile` 双向可读（卷/条目注释 UTF-8 正确、CRC 通过，`unzip -z` 打印注释按本地码页转码的乱码与 §6 记录一致）。

## 11. 落地偏差（实施中对文档的微调）

1. 注释降级可测：`archiveEntryCommentJSON` 拆为 `archiveEntryCommentLimited(msgs, summary, limits, cap)` + 包级 `archiveCommentLimits`，测试用注入的小 `limits`/`cap` 覆盖 200rune→80rune→丢 summary 三级。
2. 默认窗口导出为 `agent.ArchiveDefaultWindow`（文档原写 `defaultArchiveWindow`），供 `repl.ParseArchiveArg` 使用（该默认只在解析层生效，agent 侧 `OlderThan=0` 恒为「不限」）。
3. 归档项摘要入库即截断：`readVolume` 把 comment 的 ≤200 rune 摘要按活动区口径截到 30 rune 再进 `SessionInfo`，保证 picker/补全行宽与活动项一致（长行折行会破坏 picker 的 `CursorUp` 重绘）；因此 `MetaOK=false` 时按 `SessRow` 既有格式显示 `0条`，未做 `--条`。
4. `sessionStore` 增 `frozenID string`（文档字段表只列 `frozen bool`）：`path()`/`id()` 需为空，归档 id 只能另存一字段给 UI。
5. `-n` 下 `/archive` 直接报错 `MsgArchiveNoSave`（文档未规定；`-n` 是全程只读，移动会话文件超出其语义）。
6. 新增文案常量（超出文档清单）：agent 侧 `MsgArchiveNoSave`、`MsgArchiveSkipIdle`、`MsgArchiveSkipArchived`；repl 侧 `MsgArchiveBadArg`、`MsgStatSessionArchive`；P4 增 agent 侧 `MsgBadArchiveThreshold`、`MsgBadArchiveKeep`，repl 侧 `MsgAutoArchiveAsk`、`MsgAutoArchiveSkip`、`ArchiveDryRunFlag`（`MsgArchiveUsage` 已在 P4 随 `/archive` 参数 join 化删除）。
6b. P4 决策：`auto_archive*` 三键只走配置文件、不加 env（用户明确要求）；`/archive --dry-run` 接线上线后补测发现 repl 判定顺序把试运行误报为「已归档」，改为 `DryRun` 优先判定；原本冗余的 `strings.Contains(name, archiveTempSuffix)` 过滤为死条件（tmp 名不以 `.zip` 结尾）已删。
6c. P4 review 修复：询问门禁补齐——此前只靠 `ctty.Open()`，`/dev/tty` 在有控制终端时恒可开，`tanya -p`（子代理管道）与 `tanya > log` 会把提示写进 stdout 并阻塞在 `/dev/tty` 读；现 plain 模式与 `ctty.Probe().StdoutTTY` 在开终端之前静默返回。`SuggestArchive` 改为与 `archive()` 同序镜像（id 降序先占 `keep` 名额、再剔当前会话），提示数与实际归档数严格一致；`keep=0` = 除当前会话外全归档落文档。
7. `loadFrom` 在解码循环后 `io.Copy(io.Discard, r)` 排空：zip reader 的 CRC32 只在读到 entry 末尾时校验，若 JSON 语法错误先中断解码就永远看不到 CRC 错，排空可保证损坏 entry「错误上抛」而不是静默截断历史。
8. `ArchiveEntry.After` 取卷中央目录的 `CompressedSize64`（写完卷回读一次 CD），`Before` 取源文件字节；报告消费者只用得上卷级 `RawBytes`/`VolumeBytes`。
9. `/stat` 的归档行文案为 `会话文件: 归档只读 X（未写入）`（与 `/stat` 块内既有「会话文件」标签一致，文档原写「会话: 归档只读 …」）；`agent_custom` 的 `stat` 用文档口径 `会话: 归档只读 X（未写入）`。
10. 卷写入前的元数据扫描失败记 `Failed` 并跳过该文件（不建该 entry）；全部候选都失败时不建目录、不建空卷。
11. 2026-09-18 `/archive` 参数语义重做（用户要求）：删 `--dry-run` 与 `all`，改为「先出报告 + `y/N` 确认」的一步式交互，且仅在完整交互环境（rich 输出 + `r.raw` + `r.prof.TTY`）启用，`-p`/ask/管道/非终端一律只提示 `MsgArchiveOnlyTTY`（不出报告、不落卷）。参数口径改为：无参 = `Keep=auto_archive_keep`（新访问器 `Agent.AutoArchiveKeep()`）、纯数字 = `Keep=n`（`0` = 除当前会话外全部）、带单位 = `OlderThan`；`ParseArchiveArg` 增 `defaultKeep` 形参；`agent.ArchiveDefaultWindow` 与 30 天默认窗口一并删除。`MsgArchiveDryRun` → `MsgArchivePreview`，新增 `MsgArchiveConfirm`/`MsgArchiveCancel`/`MsgArchiveOnlyTTY`/`MsgArchiveNoneKeep`/`MsgArchiveNoneWindow`，`MsgArchiveNone` 退为通用 0 命中文案（启动自动归档路径复用）。
12. 同日 review 修复（用户要求）：时长参数收窄为单段单单位（`Nd`/`Nh`/`Nm`/`Ns`，正则 `^([0-9]+)(d|h|m|s)$` + 单位表，不再走 `time.ParseDuration`，复合时长如 `12h30m` 报非法）；0 命中的窗口文案（`MsgArchiveNoneWindow`）改为直接回显原参数，删掉按 `Duration` 反解格式化的 `archiveWindowText`（`TrimSuffix("0s")` 会把秒位为 0 的时长截坏，如 `30s`→`3`）；readline `Editor` 新增 `SetHistoryFilter(func(string) bool)`（默认行为不变，谓词拒绝始终不入史），`/archive` 确认读挂全拒过滤器、`y`/`n` 不进输入历史。
13. 2026-09-19 归档提示统一（用户要求）：启动自动归档与 `/archive` 合流为一条线路 `REPL.archiveFlow(opt, noneArg)`（新文件 `repl/archive_flow.go`，`repl/autoarchive.go` 删除）；确认读统一走 `readConfirm`（启动期不再 `ctty.Open` 直读 `/dev/tty`），门禁统一为 `archiveInteractive()`。预览文案合并为 `当前活跃会话 A 个；将归档 C 个（约 X[，保留最近 K 个]）。`（`agent.ArchiveReport` 新增 `Active`，`archive()` 在 Keep 截断前记录未归档会话总数），不再显示阈值；`MsgAutoArchiveAsk`/`MsgAutoArchiveSkip` 删除，取消统一为 `MsgArchiveCancel`（`已取消，未归档`），关掉启动询问的 `auto_archive: false` 说明只留在 README。
14. 2026-09-19 `auto_archive` 默认改为 true（用户要求）：`defaultConfig()` 显式置 `AutoArchive: true`（此前依赖 `bool` 零值 = 关闭），yaml 写 `auto_archive: false` 仍可覆盖关闭；`config.example.yaml` 注释、`README.md`《启动自动归档》、`docs/design.md` 与 `AGENTS.md` 同步。
