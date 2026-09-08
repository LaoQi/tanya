# 富文本渲染管线方案

> 状态：**设计定稿，暂不实施**（等待排期）。目标：为 tanyan 建立统一的富文本中间表示（IR）与渲染管线，收敛现散落各处的颜色/终端控制代码，支撑后续 Markdown 输出染色与跨终端（Windows Terminal 等）渲染。
> 决策背景（备选方案对比与取舍过程）见文末附录。参考项目对比分析见 `docs/render-refs-compare.md`（持续补录）。

## 1. 背景与问题

现状：颜色与终端控制散落 5 处，模式不统一——

| 位置 | 现状 |
|---|---|
| `agent/config.go` `DefaultPrompt` | 硬编码裸 ANSI（`\x1b[37m{cwd}\x1b[0m ...`），改 yaml prompt 也得写裸 ANSI |
| `repl/toolview.go` | 私有常量 `ansiDim/ansiOrange/ansiInfo/ansiReset` + `tint()/dim()`，`tty bool` 层层传参 |
| `repl/picker.go` `repl/messages.go` | 文案常量混入控制序列（`PickTitle` 含 `\r\x1b[K`、选中标记含 `\x1b[32m`） |
| `repl/spinner.go` | 直接拼 `\r\x1b[K` + 橙色码 |
| `readline/width.go` | `Truncate` 不感知 ANSI（转义序列会被当可见字符截断） |

核心缺陷：无统一颜色模型、无语义色、tty 开关靠手工传参、宽度工具不识别 ANSI、文案与配色耦合。

升级需求（驱动本轮设计）：

1. 大模型返回的 Markdown 要做颜色与格式渲染
2. agent 可能跑在不同终端（Windows Terminal 等），需要能力隔离
3. prompt 模板要与管线统一：可用标记语言写模板，快速解析成多色显示，渲染路径一致

## 2. 总体架构

```
━━━ 解析层（多来源，可插拔）━━━      ━━━ IR ━━━              ━━━ 渲染层 ━━━
配置模板标记 → Inline                Document                TerminalRenderer
Markdown(阶段3) → Document   ──→    []Block/[]Inline  ──→   (TermProfile 驱动：
代码常量(类型API) → Span              唯一表示                 色深降级/纯文本降级)
工具输出 → 原样 Text(铁律)
```

原则：**所有富文本在 IR 层汇合，IR 以下互不知晓，渲染器只认 IR + 终端能力档案**。加一种来源（markdown）、换一种终端（Windows Terminal）、换一种输出（远期 HTML）都不动另外两层。

### 2.1 四种来源、两条渲染入口、一个 `\x1b` 发射点

```
                 解析（启动时一次）              绑定（每轮循环）         渲染（唯一出口）
prompt 模板 ──→ ParseTemplate ──→ Template ──→ Bind(变量表) ──→ []Inline ──→ Renderer.Inline
markdown  ──→ MarkdownBuf    ──→ []Block  ──→（无）                       ──→ Renderer.Block
代码常量  ──→ 类型 API        ──→ []Inline ──→（无）                       ──→ Renderer
工具输出  ──→ RawText          ──→ Block   ──→（无）                       ──→ Renderer
```

收编后的两条铁律（可机器审计，一条 grep 验证）：

1. 全项目只有 `style/render.go` 产生 SGR 序列（`\x1b[3Xm` 类）
2. 业务代码不出现裸 `\x1b`（`readline/editor.go` 的光标操作与 `style` 控制原语除外）

### 2.2 包结构与依赖方向

```
style/
  color.go    Color/Attr/Style + 色深量化降级
  doc.go      Document/Block/Inline（IR 定义）
  markup.go   配置标记解析器（BBCode → IR）
  template.go Template：带槽位的 IR + Bind
  markdown.go （阶段3）markdown → Document + 流式缓冲
  term.go     TermProfile 探测
  render.go   TerminalRenderer + Strip/Width/Truncate（ANSI-aware）
  control.go  光标控制命名原语（LineStart/ClearLine/CursorUp/ClearScreen...）
```

新增顶级包 `style`，零外部依赖（依赖仍仅 yaml + x/sys）。依赖方向：`main → repl → agent → style`、`readline → style`。不放进 readline：`agent.DefaultPrompt` 也要用，agent 不应依赖终端输入层。readline 保留内部光标操作（终端层操作终端是其职责），只把颜色与宽度交给 style。

## 3. IR 定义（style/doc.go）

双层结构：Block 管格式，Inline 管颜色——markdown 的天然分层。全部值类型、可 `==`/`DeepEqual` 比较，约 120 行。

```
Document ─── Blocks[] ──┬─ Paragraph ── Inlines[] ──┬─ Span{Style,Text}
                        ├─ Heading{Level}           ├─ CodeSpan
                        ├─ CodeBlock{Lang,Lines[]}  ├─ SoftBreak
                        ├─ List ── Items[] ── ListItem ── Blocks[]（递归）
                        ├─ Quote ── Blocks[]（递归）
                        ├─ Rule
                        └─ RawText{Text}   ← 唯一的"免检"通道
```

```go
type Document struct{ Blocks []Block }

type Block interface{ blockNode() }   // sealed：非导出标记方法
type Inline interface{ inlineNode() }

// ── Block 层：管"渲染格式" ──
type Paragraph struct{ Inlines []Inline }

type Heading struct {
    Level   int      // 1-6；终端映射为色阶+加粗，不模拟字号
    Inlines []Inline
}

type CodeBlock struct {
    Lang  string     // 语言名，供将来高亮；空 = 无高亮纯染暗色
    Lines []string   // 逐行保存：截断/行号/逐行染色都按行操作
}

type List struct {
    Ordered bool
    Start   int        // 有序起始编号，默认 1；序号文本渲染时生成，IR 不存
    Items   []ListItem
}

type ListItem struct {
    Blocks []Block    // 递归：容纳嵌套列表、列表内代码块、多段落
}

type Quote struct{ Blocks []Block }   // 渲染时逐块加 `▌ ` 前缀，前缀不属于内容

type Rule struct{}                    // ────，字符与长度渲染时定

type RawText struct{ Text string }    // 工具输出：字节级保真，渲染器永不解释

// ── Inline 层：管"颜色" ──
type Span struct {
    Style Style
    Text  string
}

type CodeSpan struct{ Text string }   // 行内代码独立类型：固定暗底色、禁折行、超宽截断保内容
type SoftBreak struct{}               // 段内换行；终端渲染为 \n，硬/软不区分
```

设计要点：

- `List.Items` 用 `[]ListItem{Blocks}` 而非 `[][]Inline`：模型回答中嵌套列表、列表项下挂代码块常见，IR 存不住的结构渲染器救不回来
- sealed interface（非导出标记方法）防外部随意扩展，类型共 8 个
- 来源可信度编码在块类型里：工具输出进 `RawText`，模型输出进 `Paragraph/CodeBlock/...`

## 4. Color / Style（style/color.go）

```go
type ColorKind uint8
const (
    ColorNone ColorKind = iota   // 零值 = 未设置，省掉 HasFg/HasBg 布尔
    Color16                      // V16: 0-15
    RGB                          // RGB: 0xRRGGBB
)

type Color struct {
    Kind ColorKind
    V16  uint8
    RGB  uint32
}

type Attr uint8
const ( AttrBold Attr = 1 << iota; AttrUnderline; AttrReverse )   // SGR 1/4/7；dim 用 BrightBlack 表达

type Style struct {
    Fg, Bg Color     // ColorNone = 不改动该通道
    Attr   Attr
}
```

- 全部零值即"无样式"，`Style{}` 可当 Plain 用，`==` 可比，测试断言直观
- **色深约束放宽决议**：现约束"一律 16 色硬 SGR"放宽为——IR 支持 Color16 与 RGB 两档，默认 palette 全部 Color16（保持现观感与约束精神），渲染按 profile 降级。RGB 仅为 palette 显式配置与将来 markdown 代码高亮预留。256 色是量化产物，不进 IR
- 降级链：`RGB →(256) 最近色 →(16) 最近色 →(None) 丢弃`

## 5. 语义色与 palette

UI 代码只引用语义名，具体色集中一处、可配置覆盖：

```go
var (Dim, Info, Warn, Ok, Error, Accent Style)   // 启动时按 palette 填充
```

对应现状：`Dim`←90（工具块/ghost）、`Info`←94（状态行）、`Warn`←33（spinner）、`Ok`←32（选中标记）、`Error`（新补，当前错误文案未上色）。

```yaml
colors: auto        # auto | on | off
palette:            # 覆盖语义色（可选）
  info: bright_cyan
  error: red
```

换主题 = 换 palette，IR 与渲染器零改动。渲染器不认识"语义"概念——IR 里存的是解析后的具体色，语义名在解析时经 palette 查表落成具体值。

## 6. 终端能力档案（style/term.go）

```go
type TermProfile struct {
    Colors  ColorLevel   // None / Basic16 / Extended256 / TrueColor
    Unicode bool         // braille spinner、制表符可用性
}

func DetectProfile() TermProfile
```

探测优先级：yaml `colors`/env `TANYA_COLOR` 强制档 > `WT_SESSION`（Windows Terminal → TrueColor+Unicode）> `COLORTERM=truecolor` > `TERM=*-256color` > TERM 缺失 → Basic16 > `dumb`/非 TTY → None。

渲染器按 profile 输出，同一份 IR：

```
TrueColor:  \x1b[38;2;R;G;Bm...
Basic16:    \x1b[97m...（RGB 量化到最近 16 色）
None:       纯文本（去 ANSI，制表符换 ASCII 替代）——重定向文件/管道自动干净，零分支代码
```

## 7. 配置标记语法（style/markup.go）

采用 **BBCode 风格**（多方案对比后选定，过程见附录）：

```
[white]{cwd}[/] [blue]{model}[/] [yellow]{effort}[/] [green]{stat}[/]
[red bold]警告[/]
```

- 空格分隔叠属性：`[red bold]`，单标签省嵌套
- 未知名原样输出兜底；未闭合标签降级原样
- `ParseTemplate` 阶段先解析标记再绑定占位符值，**值永不解析**（Bind 纯字符串替换；值中的 ANSI 在渲染边界剥除，注入安全是结构性的）
- 存量兼容：加载时探测到 `\x1b` 的旧配置走 passthrough 模板——着色时原样输出、无色时 `Strip` 兜底，不进 IR 不解析

## 8. Template（style/template.go）

模板 = 带槽位的 IR。解析一次，槽位以 `Span{Text:"{cwd}"}` 形态存在，渲染前绑定：

```go
type Template struct{ inlines []Inline }

func ParseTemplate(src string) (Template, error)          // markup → IR，启动时一次
func (t Template) Bind(resolve func(string) (string, bool)) []Inline
```

- resolve 返回 false（未知占位符）则原样保留——与现 `renderPrompt` Replacer 行为一致，零新转义规则
- 变量表留在 repl（style 不知道 `{cwd}` 是什么）：

```go
tpl, _ := style.ParseTemplate(promptSrc)            // repl 启动时

vars := func(name string) (string, bool) {          // REPL 循环里
    switch name {
    case "cwd": return shortCwd(), true
    case "model": return r.agent.Model(), true
    case "effort": return r.agent.ReasoningEffort(), true
    case "stat": return r.agent.PromptSummary(), true
    }
    return "", false
}
line, err := r.ed.Readline(r.rend.Inline(tpl.Bind(vars)))
```

- 成本：解析一次；每轮 Bind+渲染微秒级。编辑器拿到渲染好的串，`stringWidthANSI` 照常测量，readline 零改动
- prompt 是渲染频率最高、需每次绑定数据、被编辑器测量宽度的富文本——收进管线是管线一致性的试金石
- 附带收益：改 prompt 配色、改工具块配色都收敛到 palette 一处，prompt/工具块/状态行/菜单共用一份主题

## 9. 渲染器（style/render.go）

IR 唯一消费方，全进程唯一 SGR 发射点：

```go
type Renderer struct {
    w     io.Writer
    Prof  TermProfile    // 色深 + Unicode，探测一次
    Width int
    Theme Theme          // 标题色阶/引用前缀/代码底色等渲染格式，palette 可覆盖
}

func (r *Renderer) Doc(d Document)    // 整段渲染
func (r *Renderer) Block(b Block)     // 流式增量渲染
func (r *Renderer) Inline(i Inline)   // 提示符等行内场景（不自动折行）
```

### 宽度工具（一并迁移）

`stripANSI/stringWidth/Truncate` 实现移入 `style/render.go`（导出 `Strip/Width/Truncate/Pad`），`style.Width` 统一 ANSI-aware——顺带修掉"参数含 ANSI 被截坏"的隐患。readline 保留私有包装；repl 调 `readline.Truncate` 的点改调 `style.Truncate`。

补充（参考 reflow 后发现的设计缺口，详见 `render-refs-compare.md` 议题 6）：**截断须处理样式复位**——截断点若处于样式内，SGR 处于打开状态会向后续输出串色，`Truncate` 需跟踪活跃 SGR 序列并在截断处显式复位（reflow `ansi/writer.go` 的 lastseq 模式）。IR 路径的折行/截断由渲染器按 span 边界切分，天然免疫此类问题。宽度口径是否升级为字素簇（uniseg 依赖）实施前裁决。

## 10. Markdown 管线（阶段3）

### 解析边界与安全

- **红线修订**：由"绝不解析模型输出"修订为——**工具输出绝不解析**（永远 `RawText`，连 markdown 都不解析）；模型输出只经 markdown 解析器这一条有界路径，且控制字符全剥离（C0 除 `\n`），HTML 原样转义输出
- 会话存储不受影响：落盘仍是原文，渲染只发生在输出时

### 流式渲染策略

markdown 是"闭合才确定"的语法，REPL 是逐 token 直出，中间放流式缓冲器：

```go
func (b *MarkdownBuf) Write(delta string) []Block   // 返回本次新闭合的块
func (b *MarkdownBuf) Close() []Block               // 收尾：未闭合块降级为纯 Span
```

- `onDelta` 接线：`for _, blk := range md.Write(delta) { r.Block(blk) }`
- 已闭合 Block 立即渲染提交（段落以 `\n\n`、代码块以闭合 ``` 为界）；未闭合 Block 挂起不输出
- 行内未闭合标记（`**粗体**`）行缓冲到行尾，行尾仍不闭合则按原样文本输出（乐观降级，不重绘、不闪屏）
- `/md off` 开关 + 非 TTY 走旁路：模型输出原样直出（与今天行为一致）

### 范围

- 第一批：块级四样（代码块/标题/列表/引用）+ 行内三样（粗体/斜体/行内码）
- 远期：表格、truecolor palette、非终端 Renderer

## 11. 现有代码收编清单

| 现状 | 收编后 |
|---|---|
| `DefaultPrompt`（agent 包，裸 ANSI 常量） | 改为 markup 字符串，agent 不再关心颜色 |
| `toolview.go` 的 `ansi*` 常量 + `tint/dim` | 删除；构造处直接 `style.Dim.Text(...)`；`tty bool` 参数消失（profile 驱动） |
| `spinner.go` 橙色 + `\r\x1b[K` | 颜色走管线；`\r\x1b[K` 换 `style.LineStart+ClearLine`（控制序列归 control.go，这是布局不是颜色） |
| `picker.go`/`messages.go` 常量内嵌色码与控制符 | `PickTitle` 等还原为纯文案；上色移到渲染点（`style.Ok.Text(...)`） |
| welcome 横幅 | 变成一个 Document，`Renderer.Doc` 输出 |
| `renderPrompt`（repl） | `Template.Bind` 替代，变量表留 repl |
| readline `stripANSI/stringWidth` | 迁 style，readline 留私有包装 |

## 12. 构造 API（style/build.go）

```go
var (Dim, Info, Warn, Ok, Error, Accent Style)   // 语义色

func (s Style) Text(t string) Span      // style.Dim.Text("▸ run_shell")
func P(in ...Inline) Paragraph
func Doc(blocks ...Block) Document
```

收编后 `dim("▸ "+name, tty)` → `style.Dim.Text("▸ " + name)`。

## 13. IR 契约（不变量）

1. **所有 string 字段不含 ANSI 转义与控制字符**——剥离发生在解析边界，渲染器是全进程唯一产生 `\x1b` 的地方，结构性消灭"双重染色/转义泄漏"
2. **RawText 字节级保真**——渲染器对它只做折行/截断这类视图操作，永不解释；工具输出"不可信"编码在块类型里，不靠约定
3. **IR 无布局**——不含宽度、换行点、对齐；折行永远是渲染时行为（resize 后下次渲染自动用新宽度）
4. **结构无环**——Quote/ListItem 持有 Block 值切片，构造上不可能成环
5. `Span.Text` 非空（空片段构造时跳过）

## 14. 明确不做（v1 边界）

- **不序列化**：IR 纯瞬态（解析→渲染→丢弃），会话落盘仍是原文；将来要持久化渲染缓存再另议
- **不做位置映射**（span→源偏移），终端渲染用不上
- **不做布局指令**（对齐/列宽）；表格是远期且届时也是新增 Block 类型，不动现有六个
- **不内联高亮器**：`CodeBlock.Lang` 只是标签，v1 统一暗色，接真高亮独立一步
- **不用 sentinel/控制字节标记**：格式层面可靠但配置边界失效（yaml 里不可见不可维护）、不可审计，且内部表示已是 `[]Span`，无处安放（过程见附录）

## 15. 实施阶段（每阶段全量测试后合入）

- **阶段 1（纯等价替换）**：style 包骨架（IR/color/profile/renderer/control）+ toolview/picker/spinner/messages 迁移 + width 迁移 + 文案纯化。行为零变化，SGR 断言类测试改断言新输出
- **阶段 2（配置与模板）**：markup 解析器 + `template.go` + prompt 切换 + `DefaultPrompt` 改写 markup + 旧 ANSI 配置 passthrough 兼容 + `colors`/`palette` 配置生效
- **阶段 3（markdown）**：块级解析 + 流式缓冲器先行，行内第二批；`/md` 开关
- **阶段 4（远期可选）**：表格、truecolor palette、非终端 Renderer

验证：`go build/vet/test/-race ./...`，每阶段加 pty 人工冒烟（REPL 提示符、工具块、/load 菜单、spinner）。

## 附录 A. 标记语法备选对比（决策记录）

需求：屏蔽终端细节、方便定义颜色。统一约定：16 基本色 + bold/underline/reverse，未知名原样输出兜底，红线（只解析自有模板，绝不碰模型/工具输出）。

| 方案 | 示例 | 紧凑 | 可读 | 转义负担 | 表达力 | 解析器 | 备注 |
|---|---|---|---|---|---|---|---|
| A XML | `<red>...</red>` | ✗ | ✓ | 小 | 强 | 中 | 业界通用，啰嗦，`<` 易撞内容 |
| **B BBCode（选定）** | `[red bold]警告[/]` | ✓ | ✓ | 小 | 强 | 中 | 紧凑可读平衡最好，单标签叠属性省嵌套 |
| C 花括号 | `{red}...{/}` | ✓ | ✓ | 大 | 强 | 中 | 与 `{cwd}` 占位符撞车，需两阶段+`{{}}`转义，排除 |
| D 色码 | `%W...%0` | ✓✓ | ✗ | 小 | 弱 | 小 | 字母↔颜色靠背表，可读性不达标，排除 |
| E zsh | `%F{white}...%f` | ✓ | 中 | 小 | 中 | 小 | zsh 用户零成本，非 zsh 用户陌生 |
| F palette | 无标记，占位符→色映射 | — | — | 无 | 弱 | 零 | 模板零转义，但每占位符一色无嵌套；已作为"无标记退化形态"吸收进 B |

sentinel（哨兵字节）方案评估：`\x01W{cwd}\x02` 类控制字节标签。格式层转义/撞车/解析歧义全部消除，但——配置边界失效（yaml 不可见不可维护）、in-band 不可审计、且 markup 唯一入口就是配置，等于废掉配置功能；内部表示已是 `[]Span`，sentinel 无生态位。不采用。

可靠性天花板不在字符串格式而在"不出字符串"：配置侧可见标签（解析一次）、代码侧类型 API（零解析，编译期检查）、内部 `[]Span`——双语法只有配置加载时一个解析器，维护成本反而最低。

## 附录 B. 与现有文档的关系

- 实施后：本文 §3/§4/§6/§9 目标态并入 `docs/design.md`（新增"富文本管线"章节），`### 工具视图渲染`/`### 等待动画`/`### 提示符模板` 各节相应改写
- 本文参照 `docs/probe-redesign.md` 先例：作为决策过程与设计定稿归档保留
