# 环境探针重构方案

> 状态：已按本文实施完毕（设计定稿归档）。目标态已并入 `docs/design.md`《系统提示与缓存友好》/《环境探针（envprobe）》。本文保留决策过程与对比，供回溯。实施后 review 决议：移除 `probe` 配置开关（本文 §8/§11 相关内容作废），环境注入恒定生效。

## 1. 背景

当前 envprobe（`agent/envprobe.go` 的 `DetectEnvironment`）在 `buildSystemPrompt` 末尾注入一段环境描述，随 system prompt 一起进快照、进会话持久化、进每轮请求。落地后 review 暴露出方向性偏差，而非仅实现粗糙：注入内容选错、事实源重复、事实与规则混杂、关键路径多次 exec、可测性差。

## 2. 现状与问题

1. **工具清单纯冗余**：所有请求都走 function calling，`ToolDefs()` 已把 name/description/parameters 完整交给模型。`runtimeTools()` 手写第二份清单进 prompt，双事实源必然漂移，还固定消耗约 200 token。这是最大浪费。
2. **探测内容低价值**：目录列表、git 分支、工具版本、OS 发行版名都是易变信息，模型需要时一句 `run_shell` 即可现查。预注入反而破坏 prompt 确定性、破坏缓存。
3. **事实与规则混杂**：环境段拼进 `buildSystemPrompt` 并进快照冻结，换目录/换机加载旧会话时环境信息 stale；测试要重复调用真实 `DetectEnvironment` 拼期望值，脆弱。
4. **关键路径多次 exec**：`git --version`、`go --version` 等在每次 `NewSession` 同步执行，靠静默降级掩盖成本与不确定性。
5. **目录内容非确定**：已在实现中暴露——Go 工具链在测试进程内惰性创建 `~/.config/go`，导致同一环境两次探测输出不同，破坏 append-only 前缀缓存假设。

## 3. 设计原则

1. **只注入模型无法廉价自探的信息**：平台事实与工具执行契约；不注入模型能自己 `ls`/`--version` 查到的东西。
2. **单一事实源**：执行语义从 `shell.go` 常量程序化生成，工具清单交给 function calling，prompt 内零手写副本。
3. **规则与事实分离**：规则（`DefaultSystemPrompt` + AGENTS.md）进持久化快照；事实（环境）实时重算、不持久化。
4. **严格确定性**：同 cwd 下输出字节级相同，保证 history append-only 前缀缓存假设成立。

## 4. 目标架构

三层分离：

```
persistPrompt = DefaultSystemPrompt + 全局/项目 AGENTS.md 段   ← 冻结，进会话首行
runtimePrompt = persistPrompt + envSection(cwd)                ← 每次 buildMessages 实时拼
```

- **持久化只存规则**。`/load` 还原的是 persistPrompt，环境段不复原、永不 stale。
- **环境段实时重算**。`envSection(cwd)` 为纯函数，同 cwd 结果字节级相同 → 跨会话、跨轮次缓存仍全量命中；换目录/换机自动跟随。
- **环境段固定放 system prompt 最末尾**，未来即便新增轻微字段，变化也只在尾部，前缀缓存不受损。

数据流：

```
shell.go 导出常量 ─┐
                   ├─→ envSection(cwd)：纯函数，struct → 紧凑文本
runtime + os.Stat ─┘        ↑
                            │ probe 注入（测试用 fake）
buildSystemPrompt(cwd) → persistPrompt ──→ buildMessages()：persistPrompt + envSection(cwd)
```

代码位置规划：

| 文件 | 改动 |
|---|---|
| `agent/shell.go` | 导出执行契约常量：`shellCommand = "bash"`、`shellArg = "-c"`、`shellMaxOutput`、默认/上限 timeout |
| `agent/envprobe.go` | 重写：删除 `DetectEnvironment`/`runtimeTools`/`detectVersion`/`detectOSName`/`detectLanguages`；新增 `envSection(cwd)` + `probe` 注入点 |
| `agent/agent.go` | `promptSnapshot` 语义收窄为 persistPrompt；`buildSystemPrompt` 只组装规则；`buildMessages`/`totalTokens` 改用 runtimePrompt |
| `agent/config.go` | 新增 `probe` 配置项（yaml + `TANYA_PROBE`） |
| `agent/*_test.go` | 分层测试：persistPrompt 断言只含规则、envSection 用 fake probe、runtimePrompt 拼接 |

## 5. 信息清单

### 保留（稳定、高价值、模型不自知）

| 字段 | 来源 | 理由 |
|---|---|---|
| OS/arch | `runtime.GOOS/GOARCH` | 决定命令形态（`uname` vs `ver`） |
| CWD（HOME 缩写） | `os.Getwd` | 模型所有相对操作的锚点 |
| Shell 执行契约 | `shell.go` 常量 | 构造命令必须知道：`bash -c`、非交互、无 TTY、timeout 默认 60/上限 300、输出头尾各 30KB 截断、退出码返回 |

### 可选保留一行（确定性 + 省一轮工具调用）

| 字段 | 来源 | 理由 |
|---|---|---|
| WORKSPACE 固定标记 | 固定文件集 `go.mod`/`package.json` 等存在性 | 同 cwd 下确定；告诉模型"这是 Go 项目用 go build"省一次 ls |

### 删除（易变/低价值/可自探）

- 目录内容列表——非确定，模型该自己 `ls`
- git 分支、工具版本号——易变，需要时 `git rev-parse` / `go version` 现查
- OS 发行版名（`/etc/os-release`）——低价值，`uname -a` 可查
- 工具清单——function calling 已提供

## 6. 输出格式

紧凑键值、稳定排序，约百 token 级（对比原方案 300+）：

```
# 环境
OS: linux/amd64
CWD: ~/Project/tanya
SHELL: bash -c（非交互，无 TTY）
TIMEOUT: 默认 60s，上限 300s
OUTPUT: stdout/stderr 头尾各 30KB，中间截断
WORKSPACE: go.mod
```

`WORKSPACE` 行缺失时省略，不输出空占位。

## 7. 探测机制与性能

- **主路径零 exec**：OS/arch、cwd、WORKSPACE 全为 `runtime` + `os.Stat`，微秒级。
- **bash 路径进程启动时查一次并包级缓存**（`exec.LookPath` 结果），不在每次 `buildMessages` 查。
- **纯函数设计**：`envSection(cwd, probe probeFunc) string`，探测逻辑可注入；测试用 fake probe 断言渲染，不再重复调用真实探测拼期望。
- **逐字段容错**：任一字段缺失省略该行，永不阻塞 prompt 组装。

## 8. 配置与可测性

- 开关 `probe: bool`（yaml + `TANYA_PROBE`），默认开；关闭时 `runtimePrompt` 退化为 `persistPrompt`。
- 分层测试：
  - persistPrompt 层：断言只含规则、不含环境段
  - envSection 层：注入 fake probe 控制各字段，断言渲染文本
  - runtimePrompt 层：断言 = persist + 环境段
  - 缓存确定性：同 cwd 两次渲染字节相等

## 9. 迁移步骤

分阶段、每阶段可验证：

1. **抽取执行契约**：`shell.go` 导出 `bash -c`、`shellMaxOutput`、timeout 默认/上限为具名常量；`run_shell` 实现改用常量（行为不变，跑 `go test ./agent/`）。
2. **新增 `envSection(cwd)` 纯函数** + probe 注入点 + 单元测试（此阶段尚未接线）。
3. **拆分 prompt 组装**：
   - `buildSystemPrompt` 改为只组装规则（persistPrompt），移除 `DetectEnvironment` 调用；
   - `buildMessages`/`totalTokens` 改用 `persistPrompt + envSection(cwd)`。
4. **删除旧探测代码**：移除 `DetectEnvironment`/`runtimeTools`/`detectVersion`/`detectOSName`/`detectLanguages` 及 `shortPath`（若仅此处使用）。
5. **会话兼容**：
   - 新格式 system 首行只含规则；
   - 旧格式首行含完整 prompt（无法可靠剥离环境段）→ 加载时整行保留为 persistPrompt，新旧混杂仅表现为历史退化，不破坏字节一致性；可检测旧格式特征（含 `## 运行环境`/`## 可用工具`）时提示用户 `/new`。
6. **新增 `probe` 配置项** + 开关测试。
7. **同步文档**：更新 `docs/design.md`《环境探针》章节为本文目标态；删除本文顶部"未实施"标记（或归档）。

## 10. 与原方案对比

| 维度 | 原方案 | 新方案 |
|---|---|---|
| 工具清单 | 手写注入 prompt，与 ToolDefs 漂移 | 不注入，靠 function calling |
| 环境内容 | 目录/分支/版本/发行版（易变低价值） | 平台 + cwd + 执行契约 + 固定标记 |
| 事实源 | 字符串复制 shell 常量 | 常量程序化生成 |
| 持久化 | 环境进快照，换机 stale | 只存规则，环境实时重算 |
| 探测成本 | NewSession 多次 exec | 零 exec（bash 路径一次缓存） |
| 确定性 | 目录内容破坏确定性 | 严格确定，缓存友好 |
| 测试 | 重复调探测拼期望 | 纯函数 + 注入 fake |
| Token | 300+ | 约百 token 级 |

## 11. 风险与兼容

- **prompt cache**：环境段从快照移出后，同一会话内前缀 = persistPrompt 仍逐字节不变；环境段放末尾，跨会话同 cwd 时全量命中。若模型厂商缓存按"system 整段"而非"前缀"计价，收益不变；若按前缀，收益略降但仍成立。
- **旧会话文件**：见迁移步骤 5，不迁移、只兼容，避免破坏历史回放。
- **`estimateTokens`/`totalTokens`**：需同步改为估算 runtimePrompt（含环境段），否则 `/context` 与提示符 `{usage}` 低估真实上下文。
- **`ReasoningItems` 计入估算**：`totalTokens` 除正文与工具参数外，须同样累加 `ReasoningItems[].Content`——responses 协议的明文思维链在长会话里常占上下文一半以上（实测某 928KB 会话占 60%、本地估算因此低估 2.5 倍）。估算纯本地，不参与请求构造。
- **`probe: false`**：退化路径须保证 `buildMessages` 不再调用 `envSection`，避免配置关闭仍产生开销。
