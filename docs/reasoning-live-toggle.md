# 流式期快捷键切换思考显示（Ctrl+O）

状态：**后延**（2026-09-26 定）。阻塞于 `docs/terminal-console.md` 的控制台层（至少 S2：Reader 能订阅按键事件、借出切换能自动挂起订阅者）。方案已定稿，等依赖落地后实施。

## 1 目标行为

| 用户动作 / 状态 | 结果 |
|---|---|
| **正在思考中**按 `Ctrl+O` 开 | 立即把**本段已收到的思考从头完整显示**（上分隔 + markdown 渲染），之后继续流式追加 |
| 段已结束（正文期 / 工具后 / 回合末）按开 | 本段已无法补看，开关打开，**下一段**生效 |
| 显示中按关 | 本段剩余不再显示（已显示部分保留），段结束照常打下分隔 |
| 关闭后同段内再按开 | **按同一模式重放整段**（已显示过的部分会再次出现，接受重复） |
| 工具执行期间按键 | 完全无效（约束：任意工具执行期不响应） |
| 门禁外（plain / 非 TTY / `ask`） | 不启用监听；开关仍翻转并提示档位 |

## 2 渲染状态机（已定稿）

段（segment）定义：两次段边界之间连续到达的 reasoning delta 序列；段边界 = `EventRequestStart` / `EventContent` / `EventToolStart` / `EventToolEnd` / `EventResponse` / `turn.End`（即现有 `flushReason` 的触发点集合）。一个回合可含多段（工具循环每轮一段）。

状态（turn 级，3 个字段）：

| 字段 | 含义 |
|---|---|
| `segBuf` | 本段**全文**（段首累积到段边界，始终累积——随时可能要重放） |
| `blockOpen` | 当前是否有打开的渲染块（决定关闭/段边界是否补下分隔） |
| `blockStart` | 当前块上分隔的打印时刻（下分隔的时长口径） |

行为表：

| 事件 | 行为 |
|---|---|
| 段首 delta | `closeBlock()`（兜底）→ 清 `segBuf` → 累积；若开关开且门禁过 → `openBlock()`（打 `─── 思考 ───`、记 `blockStart`）→ 渲染该 delta |
| 段内 delta | 累积；若开关开且门禁过：`blockOpen` → 渲染该 delta；`!blockOpen` → `openBlock()` + **重放 `segBuf` 全文** |
| 按开 | 翻开关；本段有内容且门禁过 → `closeBlock()` → `openBlock()` → **重放 `segBuf` 全文** |
| 按关 | 翻开关；`closeBlock()`（打 `─── 思考结束 · x.xs ───`，块闭合） |
| 段边界 | `closeBlock()`；清 `segBuf`（下段重新累积） |
| 不在段中按开/关 | 只翻开关 + 提示行，下一段生效 |

渲染路径：`closeBlock() → openBlock() → reasonBuf.Reset()(+SetWidth) → 逐块喂 segBuf → Renderer.Block`。段首渲染、段中首开、反复重放三个场景共用同一条路径，markdown 永远从段首解析，不存在截断/畸形。

时长口径：每块独立计时（`blockStart` = 该块上分隔时刻），反复重放会看到递增的块时长。

## 3 对控制台层的依赖

| 需求 | 由控制台层提供 |
|---|---|
| 收 `Ctrl+O` | 事件流的 `EventKey`（`0x0f` 需在 `keyParser` 增 `KeyCtrlO`；流式期用 `ReadEvent`/`Subscribe` 订阅） |
| 工具执行期不响应 | 借出（LendStdin/LendFull）自动挂起订阅者（无需本功能自己挂起/恢复） |
| 按 `^C` 仍能中断回合 | 控制台层归一 `Interrupt` 事件（本功能不碰 `ISIG`） |
| 平台一致性 | 控制台层模式映射；本功能代码两平台相同 |

控制台层就位后，本功能的改动面收缩为：**注册 `Ctrl+O` 处理 + 翻转开关 + 渲染侧的状态机改造（`writeReasoning` 拆成「累积 + 重放」）**。

## 4 若在控制台层之前硬做（不推荐，记录代价）

- 需要自己实现：逐键读的模式组合、工具期挂起/恢复的时序（`EventToolStart`/`EventToolEnd` 单点）、唤醒式让出（避免 `VMIN=0/VTIME=1` 的 100ms 窗口吞掉子进程输入首字节）、与 `readline`/`tools/shell` 两处的读权协调。
- 需要自己论证 `^C` 语义（tanya 持有期用哪种模式、`0x03` 谁解释）。
- 结果：与本次立项前的方案相同——跨 6 个文件、按期时序手工对账，回归面覆盖 `run_shell` 交互、桥接、Windows 分支。

## 5 待实施时定稿的取舍

1. **重放落点**：A（默认）watcher 只翻原子开关 + 置重放请求，渲染在 turn 侧下一个 delta 回调执行（延迟 ≈ 一个 token 间隔；`toolView.dirty/justEnded` 无锁，故渲染留在 turn goroutine）；B watcher 直接渲染（即时，但需给 `toolView` 记账加锁）。
2. **累积上限**：建议 1 MB；超限停止累积、本段流式渲染不受影响但不可重放，下一段恢复。
3. **输入期 `Ctrl+O`**：建议一并支持（提示符期只翻开关，无重放）。
4. **提示行文案**：开 / 关 / 从下一段起 / 档位不显示，共四档，走 `KindNotice`。

## 6 测试与文档清单

- 单测：段状态机（段首开、段中途开→重放、段中途关、反复开关重放、段结束开→下段生效、超限降级）；`0x0f → KeyCtrlO`；`-race` 下的原子开关。
- pty 集成：思考中喂 `Ctrl+O`，断言屏上出现**完整段**（与真实文本逐字比对）。
- 真机：`sudo`/`ssh` 期间按键完全无效、`^C` 仍中断、退出后 `stty -a` 无残留。
- 文档：`AGENTS.md`（渲染开关与流式块语义）、`docs/design.md`、`README.md`（快捷键与限制）、`CHANGELOG.md`。
