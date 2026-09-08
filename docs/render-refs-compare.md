# 富文本渲染参考项目对比

> 状态：持续补录。收录拉取到 `refs/` 下的参考项目分析，用于校验 `docs/render-pipeline.md` 设计决策、吸收可实现的做法。不按单一项目组织，按**对比议题**组织——后续新增项目直接挂到对应议题下。
> `refs/` 已在 `.gitignore`，不入库；本文只记录结论与出处（文件路径），可独立阅读。

## 参考项目索引

| 项目 | refs 路径 | 定位 | 拉取方式 |
|---|---|---|---|
| glow | `refs/glow` | 终端 markdown 阅读器（TUI 壳），markdown 渲染委托 glamour | `--depth 1` |
| glamour | `refs/glamour` | markdown → ANSI 渲染库（本领域核心对标） | `--depth 1` |
| reflow | `refs/reflow` | ANSI 感知宽度/截断/折行算法集（弱项补强） | `--depth 1` |
| charm-x | `refs/charm-x` | reflow 接任者：VT parser 状态机 + uniseg 字素宽度（monorepo，只读 ansi/） | `--depth 1` |
| rich | `refs/rich` | Python 终端富文本标杆，IR 分层印证（重点分析） | `--depth 1` |
| streamdown | `refs/streamdown` | LLM 流式 markdown web 组件，块级增量参照 | `--depth 1` |
| mdcat | `refs/mdcat` | 事件推送式 markdown 终端渲染（零 IR 纯流式） | `--depth 1` |

## 议题 1：管线形态——一体渲染 vs IR 分离

**glamour**：`goldmark 解析 → AST → 节点渲染器 → ANSI 字符串`，解析与渲染一体，AST 是唯一中间态，产物字符串既是输出也是唯一表示（stringly）。

- 出处：`refs/glamour/glamour.go`（`TermRenderer`：md + buf）、`refs/glamour/ansi/renderer.go`（`RegisterFuncs` 按 20+ 节点类型注册）
- 无流式能力：`Render(in string)` 整篇进出；glow 的 pager 场景也是整篇渲染（`refs/glow/ui/pager.go:355-395`）

**我们的差异与理由**：IR（Document/Block/Inline）独立于解析与渲染两端，换来三件事——

1. 流式块级增量（`MarkdownBuf.Write` 闭合即出块），REPL 逐 token 场景 glamour 做不到
2. prompt/markdown/代码常量/工具输出四种来源汇合同一管线，glamour 只管 markdown 一种输入
3. 渲染时降级 + 纯文本旁路，glamour 依赖外部 writer 补

结论：设计维持。glamour 验证了"按节点类型拆渲染器"与"样式数据化"两条路可行，我们阶段 3 的渲染器组织直接对齐其粒度（但规模收敛为四块三内联）。

## 议题 2：色彩降级的位置

两种模型：

| | 渲染时降级（我们 §6） | 输出边界降级（charm 生态） |
|---|---|---|
| 做法 | Renderer 内查 TermProfile，Span 逐个量化后写 SGR | 先产全彩 ANSI 串，专门 writer 写出时降采样 |
| 出处 | — | `refs/glamour/examples/artichokes/main.go`（`colorprofile.NewWriter(os.Stdout, os.Environ())`，`w.Profile` 可读回） |
| 好处 | 无全彩中间串；None 模式零转义成本 | 渲染与终端能力完全解耦，profile 可事后换 |
| 代价 | 每 Span 查 profile（微不足道） | 无色终端也先产全彩串再剥；profile 固定在写出口 |

**我们的额外约束**：`Renderer.Inline` 产物（提示符）要交给 editor 做宽度测量，无色串与彩色串本就是不同产物，输出边界模型收益打折。

结论：**维持渲染时降级**。但 `charmbracelet/colorprofile` 的量化算法（RGB→256/16 色距计算）在阶段 1 实现降级链时作为对照参考。

## 议题 3：样式数据化（palette/Theme 的对照）

**glamour**：样式 = JSON 配置，每类 markdown 节点一个配置项，粒度极细——不只颜色，连 `h2` 的 `## ` 文本前缀、引用的 `│ ` 缩进符、hr 的分隔线样式、列表 bullet 字符都是样式字段，且含 `margin/indent` 等布局参数。

- 出处：`refs/glamour/styles/dark.json`（`"h1": {"prefix": " ", "background_color": "63", ...}`、`"hr": {"format": "\n--------\n"}`）、`refs/glamour/ansi/style.go`（`StylePrimitive` 20 余字段）
- 另有 `notty.json`：无色终端不只用去色版，还独立调整前缀符号——印证我们 Theme 按 profile 切换的思路
- 主题文件：dark/light/notty/dracula/tokyo-night/pink，主题即换 JSON

**对照结论**：

- 我们的 Theme 设计（§9：标题色阶/引用前缀/代码底色可配）方向一致；glamour 的"前缀/符号也进样式"值得吸收——Theme 不只管色，也管符号（bullet 字符、引用前缀、Rule 字符），这正是"渲染格式归渲染层"的落地
- glamour 样式含布局参数（margin/indent），我们 IR 契约 3 明确布局不进 IR、缩进是渲染时行为（BlockStack 式累加），分界比 glamour 更严格，维持

## 议题 4：块结构渲染的工程做法

**BlockStack**（`refs/glamour/ansi/blockstack.go`）：块元素栈，嵌套 Quote/List 的缩进 = 栈内各层 Indent 累加，进块 Push 出块 Pop。阶段 3 渲染 Quote/ListItem 嵌套时直接参照此结构。

**节点渲染器粒度**：每 markdown 节点类型一个文件（heading.go/listitem.go/codespan.go...），统一 `ElementRenderer{Render}/ElementFinisher{Finish}` 进/出两段式。我们 v1 范围（四块三内联）不需要进出两段式——IR 是树、渲染是递归下降，更简单；但"每类型一个渲染函数+文件"的组织方式照搬。

**代码高亮**：glamour 内嵌 chroma（完整语法高亮器，重依赖）。印证设计文档 §14"v1 不内联高亮器、Lang 仅作标签"的边界；将来接高亮时，CodeBlock.Lines 逐行结构天然适配 chroma 的逐 token 输出。

## 议题 5：软换行语义

glamour 有 `WithPreservedNewLines` 开关（`refs/glow/ui/pager.go:373` 使用；glamour 内部对应段落换行处理策略）。markdown 标准里段内单换行渲染为空格（软换行），部分阅读器选择保留。

**对照**：我们 IR 的 `SoftBreak`（§3）注释"硬/软不区分，渲染为 \n"是 v1 简化；若后续需要标准语义（软换行折叠为空格），加 Theme 开关即可，IR 不动。此处 glamour 的开关设计确认了"语义开关放渲染层"的可行性。

## 议题 6：文本宽度与 ANSI 感知工具（弱项补强，reflow + charm-x）

现状短板：`readline/width.go` 的 `Truncate` 不感知 ANSI（转义序列被当可见字符截坏），手写宽度表覆盖范围有限。两个参考给出两条实现路线：

**reflow**（`refs/reflow`，依赖 go-runewidth）：

- ANSI 识别是 10 行状态机：`Marker('\x1b')` 进入、`IsTerminator`(0x40-0x5A/0x61-0x7A) 退出（`ansi/ansi.go`）
- `PrintableRuneWidth`：跳过转义序列后逐 rune 累加 runewidth（`ansi/buffer.go`）
- **关键发现（我们设计遗漏）**：截断处若处于样式内，SGR 处于"打开"状态，截断后输出会**串色**。reflow 的 `ansi.Writer` 持续累积活跃序列（`lastseq`，遇 `[0m` 清零），截断发生时 `ResetAnsi()` 显式复位（`truncate/truncate.go:91-95`、`ansi/writer.go`）
- 截断后丢弃剩余转义序列，与 x/ansi 相反（见下）

**charmbracelet/x ansi**（`refs/charm-x/ansi`，依赖 uniseg）：

- 不用启发式，完整 VT parser 状态表驱动（`parser.Table.Transition`，正确处理 CSI/OSC/UTF-8 边界），`Strip/StringWidth/Truncate` 全部建在其上（`width.go:11`、`truncate.go:53`）
- 宽度按**字素簇**（grapheme cluster，uniseg）：ZWJ emoji 序列计 1 格，CJK 宽字符正确；提供 `GraphemeWidth`/`WcWidth` 两种口径（`method.go`）
- `Truncate` 达限后转 ignoring，但**继续收集后续转义序列**（保留颜色状态变化，适配"截断串还要接续输出"的场景）；另有 `Cut/TruncateLeft`（左右截断，行编辑器场景）
- reflow 丢尾部序列+显式复位 vs x/ansi 保留尾部序列不复位：两种取舍对应不同使用场景，行式 REPL 取 reflow 派更安全

**对我们设计文档的修正**（需回写 §9）：

1. `style.Truncate` 必须处理截断后样式复位（reflow 的 lastseq+ResetAnsi 模式）——原设计只写了"ANSI-aware"，漏了串色问题
2. IR 路径的截断可结构性免疫：渲染器按 span 切分时天然知道样式边界，切完即收尾该 span 的 SGR——**IR 分层在此处直接消掉一类 bug**，是分离架构的又一收益
3. 字素簇宽度依赖 uniseg，与"依赖仅 2 个"约束冲突。裁决留待实施：a) 扩充现有 runeWidth 表（接受 emoji 组合序列误差）b) 引入 uniseg（讨论后定）c) IR 路径内自算（渲染器已知 span 内容与样式，可统计）

## 议题 7：rich 的 IR 分层架构（重点印证，Textualize/rich）

Python 生态终端富文本标杆（⭐57k）。管线：`markup/Markdown 解析 → Renderable 树 → Console.render 产出 Segment 流 → _render_buffer 按 color_system 生成 SGR`。与我们三层逐段对照：

**Span 模型分歧（最有价值的对照）**：

- rich `Text` = 纯文本串 + `[]Span{start, end, style}` **偏移量区间**（`rich/text.py:47`）——区间可重叠、可合并
- 我们 `Span{Style, Text}` 平铺序列——不可重叠、保序
- 分歧根源：rich 的 Text 是**可编辑对象**（append/pad/align 全部要维护偏移），偏移区间是编辑操作的必然选择；我们的 IR 是**只读瞬态**（解析→渲染→丢弃），平铺序列更简单且 `==` 可比
- 结论：模型选择由"是否可变"决定，两边都对。我们没有编辑需求，维持平铺；若将来 IR 需要变换（如纯文本导出时重排），届时再评估偏移模型

**样式渲染时机**（对照议题 2）：`_render_buffer` 拿到 Segment 流后按 console 的 `color_system` 调 `Style.render(text, color_system=...)` 生成 SGR（`rich/console.py:2132-2151`）——rich 同样把降级推迟到发射点，但发生在自家渲染器内而非独立 writer。与 charm 系同派，再次确认我们"渲染时降级"是少数派但成立（理由见议题 2，不重复）。

**Theme 命名规范（直接采纳）**：`Theme = {名字: Style}` 字典，默认样式按点分命名空间组织："markdown.h1"、"markdown.item.bullet"、"markdown.code_block"（`rich/default_styles.py:142-161`）——比 glamour 的 JSON 键更清晰。我们的 Theme 键采纳点分规范：`md.heading.1`、`md.quote.prefix`、`tool.status` 等，palette 与 Theme 共用一套命名。

**Markdown 管线**：markdown-it tokens → `MarkdownElement` 子类（每元素一个类，`__rich_console__` 产出 renderable）→ Segment（`rich/markdown.py`）。元素是**带布局的活对象**（Measure/对齐/填宽在渲染时进行）——印证 glamour 的宽松路线；我们 IR"无布局"契约（§13.3）更严格，折行全部推给渲染器，维持分歧。

**markup 解析器**（`rich/markup.py`）：`[bold red]...[/]` 语法，未闭合标签抛 `MarkupError`——rich 面向程序员（fail-fast）；我们 markup 面向配置文件（未闭合降级原样输出，配置不能因手误拒绝启动），差异合理，维持。

**无流式**：`Live` 是整屏重绘（TUI 思路），无行式增量输出——再次确认议题 8 前的空白区判断。

## 议题 8：流式 markdown 的两条参照（streamdown + mdcat）

检索确认：行式终端的流式 markdown 渲染**无成熟方案**（专项搜索仅 ⭐<20 玩具项目），现存两派都绕开了问题——TUI 全屏自绘（crush/opencode，拥有整屏可随意重绘）与流式纯文本+完成后整篇渲染（mods/aichat 传统做法）。我们的 MarkdownBuf 落在空白区，两个跨领域参照给出边界：

**streamdown**（`refs/streamdown`，TS/React，专为 LLM 流式 markdown 设计）：

- 每次增量**整篇重解析**，按顶层块切分后**逐块 memo**，未变化的块跳过重渲染，实际只重渲染最后一个变化块（`lib/parse-blocks.tsx`、`lib/components.tsx` 的 memo 组件）
- 未闭合结构识别：未闭合 HTML 标签跨块合并（开闭标签计数）、`$$` 奇偶配对、脚注存在则整篇单块（`parse-blocks.tsx:149-170`）；未闭合块标记 incomplete 状态单独渲染
- 性能验证：仓库自带 `__benchmarks__/streaming-rerender.bench.tsx`，说明"整篇重解析+块 memo"是可接受工程解
- 与我们对照：同样的"块 = 渲染单元"洞察，但 streamdown 靠 vdom diff 免重绘，我们靠**闭合块一次性提交、永不回头**——无重解析、无 diff，增量语义更强（这也修正此前判断：streamdown 不是简单整篇渲染，而是块级增量的 React 实现形态）

**mdcat**（`refs/mdcat`，Rust）：

- **零 IR 纯推送**：pulldown-cmark 发事件，`write_event(writer, state, data, event)` 即时写入终端（`pulldown-cmark-mdcat/src/render.rs:54`），用显式 `State` 状态机（TopLevel/Stacked/Inline/ListItem 栈式嵌套）替代文档树
- 能力外置：`Settings{terminal_capabilities, terminal_size}` 注入（`lib.rs:65`），Theme 为手写结构体字段——与我们 TermProfile 同型
- 代价可见：嵌套上下文全靠状态机 match 表达，render.rs 巨型 match 自带 clippy `cognitive_complexity` 豁免——**没有 IR 的流式代价是渲染逻辑里长出解析态状态机**
- 对我们结论：MarkdownBuf 的定位得到双向确认——比 streamdown 少重解析（闭合块不回头），比 mdcat 少状态机复杂度（嵌套交给 IR 树递归），两个参照各验证了我们的半个设计
## 待补议题

- 色深量化算法（**已裁定延后**，不重要；届时可拉 `charmbracelet/colorprofile`）
- Windows Terminal 探测细节（`WT_SESSION` 之外的能力探测）
- 表格渲染（远期；glamour `ansi/table.go` 届时再读）
- 字素簇宽度依赖裁决（议题 6.3：扩表 / 引入 uniseg / IR 内自算，实施前定）
