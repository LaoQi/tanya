# style 拆包：终端表现层的四域重组

状态：**已实施**（P0–P9）。基线 commit `0910445`（429 用例全绿），落地后 431 用例全绿 + `-race`。
相关：`docs/render-pipeline.md`（渲染管线原方案）、`docs/agent-split.md`（同类拆包体例）、`AGENTS.md` 结构段。

## 1. 要解决的问题

`style` 包把八个不同抽象层级、不同纯度的关注点合租在一个包里（1554 行 / 12 文件）：
它名义上是"富文本管线"，实质是 **render**——因为 IR、Renderer、markdown 解析、内联标记、
模板、ANSI 清洗都在里面，而 `readline` 为了拿宽度被迫一起接受进程级主题单例。

拆包的目标不是"把 render 抠出来"，而是先定死 `style` 的语义，再由语义倒推归属。

## 2. style 的语义界定

**style 只做一件事：视觉样式如何被命名、组合与编码。**

纯词汇表 + 纯编码函数：颜色、属性、样式组合、颜色档位、`Style → SGR 字节`。
不含 I/O、度量、词法、策略、状态、渲染、解析、配置。

据此的逐项裁定：

| 现有内容 | 归属 | 理由 |
|---|---|---|
| `Color/Attr/Style/ColorByName/ColorLevel` | style | 样式词汇本身 |
| `Style.SGR/Sprint/Frame` | style | 词汇的应用（编码 / 包裹 + 清洗） |
| `runeWidth/Width/Truncate/Strip/OneLine` | term | 文本度量与词法，不是样式 |
| `scanSequence/Sanitize/HasSGR/Passthrough` | term | 转义词法与清洗 |
| `Cursor*` | term | 终端控制 |
| `Profile/DetectProfile/Current` | term | 终端事实（读 env） |
| `Block/Inline/Renderer` | ir / render | 渲染管线 |
| `MarkdownBuf/ParseMarkup/Template` | markdown / markup | 解析 |
| `Scheme/Semantics/Theme/palette` | theme | 配色数据 |

## 3. 目标结构：`render` 树（Layout 1）

```
render/             Renderer（IR → ANSI），编排层，可 import 任意子包
render/ir/          IR：Block/Inline（叶子，仅依赖 style）
render/style/       样式词汇与编码（依赖 term，取颜色档位）
render/term/        终端原语：词法/清洗/宽度/截断/单行化/光标/能力档案（叶子，零依赖）
render/theme/       配色：语义色集合、方案、markdown 样式集、palette、内置主题表
render/markdown/    streaming markdown 解析
render/markup/      内联标记解析 + 提示符模板
```

依赖方向（单向下行，无环）：

```
render/term     → ∅
render/style    → render/term
render/ir       → render/style
render/theme    → render/style
render/markdown → render/ir, render/style, render/term
render/markup   → render/ir, render/style, render/term, render/theme
render          → render/ir, render/style, render/term, render/theme
readline        → render/style, render/term
repl            → render(+子包), readline, agent
agent           → ∅（零表现层依赖）
```

### 3.1 两个环陷阱（Go 子包语义）

子包是独立包，父包无特权；循环禁令按包计算。

- **陷阱 1 `theme ↔ render`**：`Scheme.MD` 需要 markdown 样式集，`Renderer`/`markup` 需要语义色。
  解法：**markdown 样式集 `Theme` 定义在 `render/theme`**，它只依赖 style，链条成 `style → theme → render`。
- **陷阱 2 `render ↔ render/markdown`**：markdown 产出 IR，若 IR 留在父包，父包将永远不能 import markdown。
  解法：**IR 下沉为叶子子包 `render/ir`**，父包保留编排能力。

### 3.2 为什么 `style → term` 而不是零依赖

`Style.Sprint/Frame` 是方法，必须与 `Style` 同包；它们需要颜色档位（`term.ColorLevel`）与清洗（`term.Sanitize`）。
Go 不能跨包定义方法，若坚持 style 零依赖就得把 `Sprint/Frame` 变成 `render.Renderer` 方法，
把约 130 处 `theme.X.Sprint(...)` 调用点全部改写。取舍：**style 依赖 term，方法留在 style**。

### 3.3 粒度约束

`Width/Strip/Truncate/scanSequence/HasSGR/Cursor*` 必须同包（度量要跳过转义、清洗要用词法器），
故 term 不再下拆；`markup` 与 `template` 合并（template 本就依赖 markup，且 ~100 行的包太薄）。

## 4. 符号归属总表

| 目标包 | 内容（来源） |
|---|---|
| `render/term` | `ColorLevel/LevelNone/Level16/Profile/DetectProfile/Current/Use`（term.go）、`runeWidth/Width/Strip/Truncate`（text.go）、`seqKind/scanSequence/sgrLeavesState/Sanitize/HasSGR/Passthrough/OneLine`（filter.go）、`ClearLine/ClearLineHome/CursorUp/ScreenHome` + 新增 `CursorForward/CursorDown/ClearToEOL`（control.go） |
| `render/style` | `ColorKind/KindNone/Kind16/Color/Color16/Attr/AttrBold…Italic/Style`（color.go）、`ColorByName`（markup.go 的 colorNames + palette.go 的 ParseColorName）、`(Style).SGR/Sprint/Frame`（原 Profile.sgr / Style.Sprint / Style.Frame） |
| `render/ir` | `Block/Inline/Paragraph/Heading/CodeBlock/List/ListItem/Quote/Rule/RawText/Span/CodeSpan/SoftBreak`（doc.go） |
| `render/theme` | `Semantics/Scheme/schemeList/curScheme/ApplyScheme/LookupScheme/SchemeNames/HasScheme/CurrentScheme/CurrentSchemeName`（theme.go）、`userPalette/ApplyPalette/applySemanticPalette`（palette.go）、8 个语义色、`Theme/DefaultTheme/mdTheme`（render.go 的 markdown 样式集）、`semanticByName`→`ByName`（markup.go）、`DefaultPrompt` |
| `render/markdown` | `MarkdownBuf/NewMarkdownBuf/Write/Close/drain/feedLine/closeGroup/buildList/buildQuote/cleanLine/headingLevel/isRule/listItem/quoteLine/ParseInline`（markdown.go） |
| `render/markup` | `attrNames/parseStyleNames/mergeStyle/ParseMarkup`（markup.go）、`Template/ParseTemplate/Render/scanSegments/isPlaceholderName/replacePlaceholders/bindInlines`（template.go） |
| `render` | `Renderer/NewRenderer/NewThemedRenderer/Inline/Block/itemInline`（render.go） |
| 删除 | `Document/Doc()/P()/Renderer.Doc`、`Template.Bind`、`MarkdownBuf.Reset`、`LineStart`、`KindRGB`、`Color.RGB`、`Profile.Unicode`、`Level256`/`LevelTrue`、`(Style).Text` |

## 5. 阶段（自底向上，每阶段独立全绿）

自底向上建包（叶子先建），避免中间态出现环。

| 阶段 | 动作 | 验证 |
|---|---|---|
| P0 | 基线：429 用例清单 + 方案归档（本文） | `go test ./... -count=1` |
| P1 | 建 `render/term`（叶）：text/filter/control/term.go 迁入 + term.go 的 Profile/ColorLevel；外部 `style.<term 符号>` → `term.*` | 全量 + `-race` |
| P2 | 建 `render/style`：color.go 词汇迁入（`SGR/Sprint/Frame` 方法随迁）+ `ColorByName` | 全量 |
| P3 | 建 `render/ir` + `render/theme`：doc.go 迁入；theme.go/palette.go/markdown 样式集/语义色/ByName/DefaultPrompt 迁入 | 全量 |
| P4 | 建 `render/markdown` + `render/markup`：解析层迁入；template 与 markup 合并 | 全量 |
| P5 | 建 `render`（Renderer）；删除旧 `style` 包 | 全量 + `go list -deps ./render/term` 为空 |
| P6 | 去全局：theme 的 8 个语义色 + `curScheme`/`userPalette` 改为值传递；`repl` 持 `theme.Semantics`；`readline` 注入样式；删 `repl.toolTerm` 单例 | 全量 `-race -shuffle=on` + 新增并存用例 |
| P7 | 摘 agent 表现层依赖（主题校验移 repl）；readline 裸 CSI 改用 `term.Cursor*` | `go list -deps ./agent` 无 render 树 |
| P8 | 原语修正：`Strip/Width/Truncate` 统一走 `scanSequence`（删 `isTerminator`）+ OSC 用例；删死能力（`Level256`/`LevelTrue`/`Unicode`/`KindRGB`/`RGB`） | 全量 + OSC 新用例 |
| P9 | 文档同步：`AGENTS.md`/`design.md`/`render-pipeline.md`/`open-questions.md` | — |

## 6. 行为零变更的保障

- 基线 429 用例（含 repl 渲染字节级断言：`toolview_test.go`、`mode_test.go`、`turnview_test.go`、`streams_test.go`）跨阶段必须全绿；
- 每阶段结束记录用例数与 `git diff --stat`，确认改动范围符合预期；
- 阶段间不夹带行为变更；P8 的修正单独成阶段并新增用例。

## 7. 落地偏差记录

1. **阶段顺序调整（自底向上）**：原计划 P1 建 render/style + term，实际按 `term → style → ir/theme → markdown/markup/render` 顺序推进。原因是 Go 的方法接收者规则（`Style.Sprint/Frame` 必须与 `Style` 同包）与父/子包环禁令，只有自底向上建包才能每阶段全绿、无中间环。
2. **`style → term`（非零依赖）**：`Style.Sprint/Frame` 需要颜色档位与清洗，而 Go 不能跨包定义方法；若坚持 style 零依赖须把约 130 处 `theme.X.Sprint(...)` 改为 `Renderer` 方法。落地取"style 依赖 term、方法留在 style"，**官方不变量改为：`render/term` 是零依赖叶子**。
3. **`markup → render` 的反向边**：`Template.Render` 需要 `render.Sprint`，最初形成子包 import 父包。修正为**把 Template 并入 `render`**（`render/template.go`），`render → markup` 成为正规父→子；最终所有边均为父→子或指向叶子。
4. **`Style.Bg` 保留**：编码路径（`color.go` 的 `bgSeq`）与测试用例（`bright bg`）保留未删——markup 语法暂无法表达背景，登记为待决（要么补 `[bg:...]` 语法，要么后续删除）。
5. **`Document`/`Doc()`/`P()`/`Renderer.Doc` 删除**：仅被测试使用；`TestDocSkeleton` 改写为 `render/ir` 包的 `TestIRSkeleton`。
6. **主题校验迁 `repl`**：`agent` 删掉 `DefaultPrompt`/`HasScheme`/`SchemeNames` 引用，校验由 `repl.ValidateTheme` 承担，`MsgBadTheme` 常量迁 `repl/messages.go`。落地后 `go list -deps ./agent` 无内部包。
7. **`Profile.Unicode` 删除**：只写不读，确认为死能力。
8. **`Level256`/`LevelTrue` 删除**：`sgr` 只产 16 色，探测结果与 `Level16` 无行为差异；`DetectProfile` 现在只回答"有无颜色"。
9. **readline 光标序列归口**：`readline/*.go` 已无裸 `\x1b`（全部走 `term.CursorUp/CursorForward/ClearLineHome/ClearToEOL/ScreenHome`），新增该不变量。
10. **readline 测试注入样式**：`Editor` 不再读全局主题，测试用 `SetStyles` 注入 default 方案的 dim/accent。
11. **repl 主题状态**：`REPL` 持有 `sch`/`sem`/`palette`，`/theme` 切换只改自身状态（含 `ed.SetStyles` 与 `toolView.setSemantics`），不再有全局副作用；新增 `Semantics()`/`ValidateTheme()` 供 main 与 ask 路径复用。
12. **用例数 429 → 431**：删除 `TestCurrentSchemeDefaults`/`TestApplySchemeBadNameKeepsCurrent`/`TestApplyScheme*` 系列（全局状态消失，改为 `Lookup`/`Apply` 纯函数用例），新增 `TestSemanticsPaletteOverlay`（repl）、`TestValidateTheme`（repl）、`TestDefaultPromptPlaceholders`（theme）、`TestStripOSCAndNonCSISequences`/`TestWidthOSC`/`TestTruncateOSC`（term）。
13. **OSC 缺陷实测修复**：`Strip("\x1b]0;my-title\x07hello")` 旧实现返回 `"y-title\ahello"`、`Width` 误算 13、`Truncate` 截坏；统一 `scanSequence` 后分别为 `"hello"`/5/原样。

## 8. 最终包结构与依赖

```
render/term     → ∅（零依赖叶子）
render/style    → render/term
render/ir       → render/style（+ term 传递）
render/theme    → render/style
render/markdown → render/ir, render/style, render/term
render/markup   → render/ir, render/style, render/term, render/theme
render          → render/ir, render/style, render/term, render/theme, render/markup
readline        → render/term, render/style
agent           → ∅
repl            → 全部
```

