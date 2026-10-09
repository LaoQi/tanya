# 多模态（图像）支持方案

> 状态：**P1 已实施 + 本地预缩放已实施 + P4（模型主动读图）已实施**（P0：数据模型/两协议 wire/扫描缓冲/`AskContent`/`image_detail`；P1：`@path` 解析与校验、对话与 `ask` 接线、附件回显行、`@` 触发补全、`/history` 与列表占位、`image_max_bytes`/`image_max_count`；**未做**：本地预缩放（P3，见 §4.3 评估）。原始状态说明：**P1 进行中**（P0 已完成：数据模型、两协议 wire、扫描缓冲、`AskContent`、`image_detail`；P1 已完成 `@path` 解析器与校验（`repl/attach.go` + `image_max_bytes`/`image_max_count`），**未做**：解析接线到对话/ask、`@` 补全、回显行、`/history` 占位、本地预缩放）。原始状态说明：**P0 已实施**（2026-10-09：数据模型 `ImageRef`/`Message.Images`、两协议 wire、扫描缓冲上限、`AskContent` 入口、`image_detail` 配置与估算函数）；**P1 输入面（`@path` 解析、`@` 补全、回显行）未做**，故用户侧暂不可用。拍定口径见 §7。
> 依据：① 上游网关的 agent 指南（端点 `GET /api/guide.md`，源文件在网关仓库 `internal/admin/guide.md`；§2.1–2.2 抄录其 2026-10-08 实测口径）；② DeepSeek 官方文档 `guides/vision` 与 `quick_start/token_usage`（§2.3 抄录，2026-10-09 查证）
> 范围：REPL 用户侧附图 → 两协议（`chat` / `responses`）→ history / 落盘 / 归档 / fork / load / 渲染全链路
> **不动**：工具产图、模型输出图、音频/视频/file 块、图像缩放压缩、缓存不变性契约（纯文本请求字节零变化）
> 判据：每阶段要么**请求体可断言**（mock LLM 逐字段），要么**行为可观测**（repl 单测 + 真机冒烟）

## 1 结论摘要

1. **网关已经就绪、tanya 侧是唯一的缺口**：`/v1/chat/completions` 与 `/v1/responses` 均接受 OpenAI 兼容的 `content` 数组；网关**原样透传**——不校验结构、不做模型能力判定、不做降级、不因模态改路由。
2. **只有图像有跨后端通用写法**（`image_url` / `input_image`）；`file` / `video_url` / `input_audio` 的形态随厂商而异、`file_id` 是厂商账号级产物不可迁移 → **v1 只做图像**是有依据的范围收缩，不是偷懒。
3. **改造面集中在两处**：`agent.Message` 增图像引用字段 + 两个协议的 wire 转换保持"纯文本零变化"。输入侧走**行内 `@path` 语法**（D2，已拍定）：终端无法粘贴图像，只能取路径/URL；不新增斜杠命令，避免命令面三处同步与中途扩展风险。
4. **落盘已定内联 base64**（D1）：归档 / `/load` / `/fork` / noSave 全部自动成立，代价是会话文件膨胀，由 `image_max_bytes`/`image_max_count` 卡单条上限。
5. **一个必须处理的隐性陷阱**：`scanSessionFile` 的 `bufio.Scanner` 行上限是 **1 MB**（`agent/session.go`），内联 base64 的单图即可越界 → 不处理会让 `/load` 选择列表的条数与摘要**静默残缺**。

## 2 网关侧能力与约束（依据）

### 2.1 两协议的图像写法（`/v1/*`，需 Bearer 令牌）

```json
// POST /v1/chat/completions
{"model":"<对外模型名>","messages":[{"role":"user","content":[
  {"type":"text","text":"这张图里有什么？"},
  {"type":"image_url","image_url":{"url":"data:image/png;base64,<BASE64>","detail":"low"}}]}]}
```

```json
// POST /v1/responses
{"model":"<对外模型名>","input":[{"role":"user","content":[
  {"type":"input_text","text":"这张图里有什么？"},
  {"type":"input_image","image_url":"data:image/png;base64,<BASE64>","detail":"low"}]}]}
```

- **形状差异**（易错）：chat 的 `image_url` 是**对象**、`detail` 在对象内；responses 的 `image_url` 是**字符串**、`detail` 与它同级。
- `url` 支持 base64 data URL 与 http(s) 外链；`detail` 可选（`low` / `high` / `original` / `auto`）。
- 实测（2026-10-08，真实上游）：`detail` 与 png/jpg/gif 在 `deepseek` / `glm_coding` / `opencode_go` / `xiaomi_token_plan` 四类后端**均被接受**；单图 5.9 MB 通过（各家文档限制更保守，实测更宽松）。
- 图像建议放 `user` 消息：DeepSeek 对 `system` / `assistant` 消息中的图像返回 400。

### 2.2 边界与已知差异（网关不干预，会原样暴露给调用方）

| 项 | 事实 |
|---|---|
| 体积 | 请求体上限 **128 MB**（超限 413）；响应体读取上限 64 MB |
| 通用子集 | 跨后端用统一写法（含 `auto-*` 别名）时**只有 `image_url` / `input_image` 稳** |
| file / 视频 / 音频 | `file` 形态随厂商（DS 扁平 `file_data` / GLM 嵌套 `file_url`）、`file_id` 换后端即失效且网关未代理上传端点；`opencode_go`(Zen) 明确 text-only |
| 静默忽略 | 同一供应商下模型能力不同：`deepseek-v4-pro` **接受**图像请求但静默忽略并返 200；`glm-5.3` chat 线 400、Responses 线 200 但自述看不到图 → 要严格保证请固定具体模型，别依赖 `auto-*` |
| 未代理通道 | 网关只路由 `/v1/chat/completions`、`/v1/responses`、`GET /v1/models`；图片生成、音频转写、厂商 Files 上传都不代理 |
| 只读测试 | `/api/agent/providers/{id}/proxy/{path...}`（GET/POST，body ≤ 8 MB，不计用量）可用于实测上游，但属**受控转发**、会真实消耗上游额度 |

### 2.3 DeepSeek 官方文档要点（2026-10-09 查证）

`guides/vision`（模型侧）：

- 支持格式 **JPEG / PNG / GIF / WebP**，**按文件内容嗅探**，不看扩展名与声明的 MIME（tanya 侧校验口径据此对齐）。
- `detail` 语义：`low` = 先降到 **512×512**（更快更省）；`high` 与 `original` 等价；`auto` 当前等价 `original`。仅 `image_url` / `input_image` 支持，`file_id` 形态忽略 `detail`。
- 限制：外链 **URL ≤ 8192 字符**；请求体 **48 MiB**；单图 base64/外链 **32 MiB**（Files API 64 MiB）；单请求 **≤ 600 图**；最大边 **8192 px**（请求含 ≥15 图时降到 4096 px）。
- 图像只允许在 **user** 消息（system/assistant 返 400）；Responses 线亦可放在 `function_call_output` 里（tanya 不做）。

`quick_start/token_usage` + `guides/vision` 的图像 token 算法（**官方只给规则与上界，未公开闭式公式**——精确值由该页的 JS 计算器给出）：

- 缩放规则：总像素 < 约 **544×544** 的图**放大**（保比）；更大的图**缩小**到总像素约 **1300×1300**（保比）。
- 因此**每图上界 1024 tokens**（2000×2000 与 5000×5000 同价）；多图各自独立计算，无合并项。
- tanya 侧落地（D3）：按上述缩放规则算缩放后像素，线性折算（系数由官方上界反推 `1300×1300/1024 ≈ 1650` 像素/token），`clamp ≤ 1024`；拿不到尺寸（外链、解码失败）时**兜底 1024**。P2 真机冒烟用已知尺寸图比对 `usage.prompt_tokens` 反推校准系数并回写本文。

## 3 现状（tanya 接入面实测）

| 位置 | 现状 | 对多模态的影响 |
|---|---|---|
| `agent/llm.go` `Message` | 字段 `Content string`，直接被 `chatStream` 序列化进请求体 | chat 线要发数组必须扩展 wire 形态 |
| `agent/llm.go` `chatWireMessages` | 逐条 copy + 清 reasoning 字段 | 图像转换的天然落点 |
| `agent/llm_responses.go` `buildResponsesInput` | 手搓 `map[string]any`，user 走 `responsesContentPart{Type:"input_text",Text:…}` | responses 线加 `input_image` 的落点 |
| `agent/agent.go` `Ask(ctx, input string, sink)` | `history = append(history, Message{Role:"user", Content: input})` | 需要"带附件的输入"入口 |
| `agent/agent.go` `buildMessages` / `estimateTokens` / `totalTokens` | 只读 `Content` 与 tool args | 图像不计入本地估算（D3） |
| `agent/session.go` `append` | `json.Encoder` 逐行 append，delta 写（`saved` 游标） | 图像内联进 jsonl 即自动落盘 |
| `agent/session.go` `loadFrom` | `json.Decoder` 流式解码，无行长限制 | 内联图可安全读回 |
| `agent/session.go` `scanSessionFile` | `bufio.Scanner` + 行上限常量 `MaxSessionLineBytes`（**P0 已实施**：原为硬编码 1 MB，内联图单图即可越界 → 会让列表条数与摘要静默残缺；现为 64 MiB，`TestScanSessionFileHandlesLargeImageLine` 用 2 MB 单行守护） | 已修 |
| `agent/session_archive.go` `archive` | `zip.Deflate`，条目 = `<id>.jsonl` | 内联图自动进卷（base64 压缩收益有限） |
| `agent/session.go` `sessionSummaryRunes` | 摘要取首条 `user` 的 `Content` | 纯图消息摘要为空（需回落占位） |
| `repl/dispatch.go` / `repl/completer.go` `slashCommands` / `repl/messages.go` help | 命令面三处同步（`TestSlashCommandsAllHandled` 守护） | `/image` 必须三处同时登记 |
| `repl/repl.go` `showHistory` / `printHistoryFull` | 用户消息按文本渲染 | 需占位显示而非内联数据 |
| `config` 包 | `agent.Config`（13 项，零加载机制）+ `config.UI`（inline） | 新键的分层落点（§4.4） |

## 4 设计

### 4.1 数据模型与落盘（决策 D1）

`agent/llm.go` 增：

```go
type ImageRef struct {
    MIME   string `json:"mime,omitempty"`   // image/png、image/jpeg …
    Data   string `json:"data,omitempty"`   // base64（本地文件，不含 data: 前缀）
    URL    string `json:"url,omitempty"`    // http(s) 外链或 data URL 直给
    Name   string `json:"name,omitempty"`   // 展示用文件名
    Bytes  int    `json:"bytes,omitempty"`  // 原始字节数（外链时 0）
    Detail string `json:"detail,omitempty"` // low|high|original|auto，空 = 不下发
}
// Message 内新增
Images []ImageRef `json:"images,omitempty"`
```

- `Content` 保持纯文本语义（摘要、估算、回滚、渲染全沿用），图像**只挂在 user 消息**上。
- 兼容性：旧 jsonl 无 `images` 可读；新 jsonl 被旧版读时未知字段被忽略（`json.Unmarshal` 默认行为）。

**D1-A（推荐）内联 base64 落 jsonl**：归档 / `/load` / `/fork` / noSave 全部自动成立，无新目录、无孤儿回收。代价：会话文件膨胀（1 MB 图 ≈ 1.37 MB base64），且**每轮请求体都含全部历史图像**（append-only 缓存的既有语义，前缀稳定 → 命中 prompt cache）。

**D1-B blob 外置**（`sessions/../blobs/<sha256>` + jsonl 存引用）：jsonl 瘦、可去重。代价：归档卷必须一并打包 blob（否则 `/load` 丢图）、孤儿回收、`/stat` 语义复杂——与本项目"极简优先"冲突，仅当 D1-A 的体积问题被实测证明不可接受时再启用。

### 4.2 wire 转换（纯文本零变化）

- chat（`chatWireMessages`）：user 消息**无图 → 维持 `content: "<string>"` 逐字节不变**；有图 → `content: [{type:text,…}, {type:image_url, image_url:{url, detail}}…]`。
- responses（`buildResponsesInput`）：无图 → 维持现状的 `input_text` 数组；有图 → 追加 `{type:input_image, image_url:"<字符串>", detail}`。
- 只对 `role == "user"` 生效；assistant / tool 上的 `Images` 一律忽略（防御式，网关侧也会 400）。
- 文本 part 排在图像 part **之前**（与网关示例一致）。
- data URL 拼接：`"data:" + MIME + ";base64," + Data`（`Data` 已在输入侧校验为合法 base64）。

### 4.3 输入面：`@path` 行内语法（D2，已拍定）

终端无法粘贴图像，入口取**路径/URL**，且**不新增斜杠命令**（避免命令面三处同步与中途扩展风险）。

| 规则 | 口径 |
|---|---|
| 语法 | 行内 `@<path>` 或 `@<url>`，可多处（按序）；含空格路径用引号：`@"my shot.png"` |
| token 边界 | 只认**行首或空白后**的 `@`（`a@b.com` 这类天然不受影响）；未加引号时读到下一个空白为止 |
| 文本 | **保留 `@path` 原样**（不剥离）——历史 append-only，一旦发出前缀即固定，路径噪音不影响缓存不变性 |
| 解析成功 | 附加图像（同一轮 user 消息携带），并打印一行附件回显（§4.5） |
| 解析失败 | **完全等同普通文本**：静默回退，不报错、不影响文本 |
| 解析成功但校验不过 | **报错**（文件存在且确为图像，但非受支持格式 / 超 `image_max_bytes` / 超 `image_max_count`）——否则"图没发出去"不可见 |
| 作用范围 | 只在**对话输入行**解析（斜杠命令行不解析）；`:`/`：` 前缀行先剥前缀再解析 `@` |
| 纯图消息 | **不允许**（D5）：`@a.png` 单独一行即空文本 → 报错提示需随文本一起发送 |
| 补全 | `@` 触发路径候选（D2-c），与 `/switch` 同口径（`~`/相对工作区/绝对、目录带尾 `/`、隐藏目录 gating），P1 实现 |
| 会话边界 | `/new` `/switch` `/load` `/fork` 与本语法无关（附件即发即用，无常驻队列） |

附件来源（D7，已拍定全支持）：本地文件（`~` 与相对路径按**当前工作区**解析、`os.Stat` 必须是常规文件）、`http(s)://` 外链（只存 URL、不下载）、`data:image/...` 直给。

`ask` 单发（D6，已拍定支持）：复用同一套 `@` 解析（`tanya ask "@shot.png 这张图里有什么？"`），**不新增 CLI flag**；按 D5 要求文本非空。

### 4.4 校验与配置

输入侧校验（解析成功路径）：

- 本地文件：`http.DetectContentType` 嗅探前 512 B，只接受 **JPEG / PNG / GIF / WebP**（与官方一致，不看扩展名）；大小 ≤ `image_max_bytes`；MIME 归一化（`image/jpg` → `image/jpeg`）。
- 外链：`http(s)` scheme、非空、**≤ 8192 字符**（官方上限）；不做可达性探测（不联网，避免阻塞与隐私面）。
- data URL：前缀必须是 `data:image/<子类型>;base64,` 且子类型在受支持集合内。
- 张数：单条消息 ≤ `image_max_count`。
- 官方硬限制（文档记录，不逐一预检）：请求体 48 MiB、单图 32 MiB、单请求 ≤600 图、最大边 8192 px——tanya 的保守默认（10 MB / 4 张）远低于它们。

本地 token 估算（D3，见 §2.3 算法）：`estimateImageTokens(w, h)` → 缩放后像素 / 1650，clamp ≤1024；尺寸未知时 1024。

配置键（yaml 键与 `TANYA_*` env 名沿用既有分层）：

| 键 | 落点 | 默认 | 说明 |
|---|---|---|---|
| `image_detail` | `agent.Config`（进请求） | `low`（D4） | `low`/`high`/`original`/`auto`；空 = 不下发 |
| `image_max_bytes` | `config.UI` | 10485760（10 MB） | 单图上限（输入侧校验） |
| `image_max_count` | `config.UI` | 4 | 单条消息张数上限 |

`config.example.yaml` 与 `tanya config` 输出必须同步（`main_test.go` 的 `TestConfigExampleMatchesDefaults` 守护）。

### 4.5 表现层

- **附件回显（必要，因解析失败静默）**：附件成功附加后、模型输出之前打印一行，例如 `[图 shot.png 1.2 MB]`（多张按序）。
- `/history`（`printHistoryFull` / 摘要行）：用户消息显示 `[图 a.png 1.2 MB]` 占位（多张按序），**绝不内联数据**。
- 纯图消息（D5 不允许）：`/history` 只有占位行；会话列表摘要回落 `[图 n]`（`scanSessionFile`）。
- 错误走既有 `streams` + `KindError`（外部输入路径属动态文本，经 `emitText` 清洗）。

## 5 周边一致性清单（实施时逐项核）

1. **`scanSessionFile` 行缓冲（硬前置，P0 已落地）**：`sc.Buffer` 上限从 1 MB 提到独立常量（建议 64 MB，`agent` 侧常量、不依赖配置包），否则内联图会让 `/load` 列表条数/摘要静默残缺；同时给 `si, _ :=` 的忽略 err 处补一条可见降级（列表项标记不全）。
2. **摘要回落**：`sessionSummaryRunes` 遇 `Content == ""` 且有图时写 `[图 n]`。
3. **本地 token 估算**（D3）：`estimateTokens` 不含图像 → 无 usage 时上下文显示偏低；可选按每图固定值（建议 1024，或 OpenAI `low` 口径 85）计入，真 usage 到达即覆盖。
4. **缓存不变性**：纯文本会话的请求字节必须逐字节不变（`scripts/cache_probe.py` 复验）；带图会话每轮全量重发，前缀稳定。
5. **中断/失败回滚**：`Ask` 的 `mark` 按 history 长度回滚，与消息内容无关 → 天然兼容，无需改动。
6. **归档**：内联图进 zip（`Deflate`）；卷注释的 summary 来自 `scanSessionFile`，与第 1 条同源。
7. **noSave（`-n`）**：`store.append` 直接返回 → 图像不落盘、不新增文件，与既有只读语义一致。
8. **`ask` 单发**（D6）：`main.go` 的 `a.Ask(ctx, rest, sink)` 只传文本；本期可不支持附图。
9. **系统提示词**：可选加一行（"用户可附图"），但会改 system 快照 → 需重热缓存，v1 建议**不加**。
10. **输出清洗**：图像不经输出通道，`emit`/`emitText`/`OneLine` 口径不变。

## 6 分阶段

| 阶段 | 内容 | 验收 |
|---|---|---|
| **P0 ✅** | `Message.Images` + `ImageRef`；`chatWireMessages` / `buildResponsesInput` 转换；`scanSessionFile` 缓冲上限提到独立常量；`Ask` 的带附件入口（`AskContent` 或等价）；`image_detail` 进 `agent.Config`；`estimateImageTokens` | mock LLM 逐字段断言两协议请求体；纯文本路径请求字节与改动前逐字节一致（golden）；旧 jsonl 兼容用例；估算函数表驱动用例（含 544²/1300² 边界与未知尺寸兜底） |
| **P1 ✅** | `@path` 解析器（token 边界、引号、静默回退、校验报错三分支）；`@` 触发路径补全；附件回显行；`/history` 与列表摘要占位；`ask` 复用同一解析器 | repl 单测（多张/引号/失败静默/超限报错/纯图报错/`:` 前缀行/ask 路径）；补全候选用例；`TestSlashCommandsAllHandled` 不受影响 |
| **P2** | 文档（`AGENTS.md`、`docs/design.md`、`README.md`、`CHANGELOG.md`）；真机冒烟 | 真图走 `nas.lan:28149` 两协议各一次，模型确实描述图像；`gofmt`/`build`/`vet`/`test -race`/`render_audit` 全绿 |
| **P4 ✅** | 模型主动读图：`ToolResult.Images` + loop 插入 user 图像消息 + 新工具 `read_image`（`tools/image`）+ 缩放下沉 `agent` + 工具块占位 | 见 §7.5：agent 请求体断言、`tools/image` 单测、repl 渲染断言、真机两协议（均已完成） |
| **P3 ✅（预缩放部分）** | `@path` 内联、`ask -i`、blob 外置、图像压缩、工具产图、多模态 file/音频 | 各自独立评估 |

## 7 决策（2026-10-09 已全部拍定）

| 编号 | 结论 |
|---|---|
| **D1** 落盘 | **内联 base64 进 jsonl**（blob 外置不做） |
| **D2** 输入面 | **行内 `@path`**，不新增斜杠命令；细则：文本**保留** `@path`（缓存前缀一旦发出即固定，无抖动问题）、解析失败**静默按文本**、`@` 触发**补全**、含空格路径用**引号**、**成功附加打回显行**、"解析成功但校验不过"**报错** |
| **D3** 本地 token 估算 | 按 DeepSeek 官方**缩放规则**（544² 放大 / 1300² 缩小）+ 上界反推系数 1650 像素/token、clamp ≤1024，**兜底 1024**；P2 用真 usage 校准 |
| **D4** `detail` 默认 | `low` |
| **D5** 纯图消息 | **不允许**（报错提示需随文本发送） |
| **D6** `ask` 单发 | **支持**，复用 `@path` 解析（不加 CLI flag） |
| **D7** 外链 / data URL | **支持** |

## 7.5 P4：模型主动读图（`read_image`）——方案已定，**待实施**

> 状态：**已实施（2026-10-09）**。四项决策按推荐值采纳并全部落地，实施记录见本节末尾。

### 约束（决定了机制）

OpenAI 兼容的两条线在"工具结果能不能带图"上不同：**responses** 允许 `function_call_output` 里放 `input_image`，**chat** 的 `role: tool` 消息**只能带文本**；DeepSeek 官方也明确"图像只在 user/developer 消息"。故不采用"工具消息带图"，改为**工具结果 + 紧跟一条 user 图像消息**——两协议一致。

```
assistant(tool_calls: read_image{path})
  → tool(文本：已读取 shot.png 512×384 41.2k)
  → user(Images: [ImageRef], 文本：（read_image 附图：shot.png）)   ← 新增
  → 下一轮请求（模型据此作答）
```

并行多工具时（如同回合 `read_image` + `run_shell`）：user 图像消息在**本轮全部 tool 消息之后**统一追加、多来源聚合为一条——插在 tool 消息中间会打断 `tool_calls → tool` 的相邻性，严格校验的后端（OpenAI 系）会拒绝。

### 实现面

| 项 | 内容与落点 |
|---|---|
| `ToolResult.Images` | `agent/tools.go` 的 `ToolResult` 增 `Images []ImageRef`（agent 定义、tools 填充、repl 读） |
| loop 插入 user 消息 | `agent/agent.go` `runTurn` 里本轮**全部** tool 消息之后：聚合各结果的 `res.Images` 为一条 `Message{Role: "user", Images: …, Content: <说明>}`（不放中间、不打断 tool 相邻性）；说明文案用 agent 常量（如 `MsgToolImageNoteFmt`），**不放 base64** |
| 能力下沉 | `repl/resize.go` 的 `ResizeTargetSide`/`MaybeResizeImage`/`downscale`/`jpegOrientation`/`exifOrientation` **移到 `agent`**（`tools` 不能依赖 `repl`；`agent` 零内部依赖、只用标准库，是唯一合适落点），`repl` 侧改为薄封装（行为与测试不变） |
| 新工具 `read_image` | 新包 `tools/image`（参照 `tools/shell` 的 `Options`）：参数 `{path, detail?}`；路径解析（`~` / 相对 `Workspace` 回调，同 `run_shell`）、`os.Stat` 常规文件、内容嗅探限 JPEG/PNG/GIF/WebP、大小 ≤ `image_max_bytes`、按 `image_resize`/`image_detail` 缩放（`detail` 参数可覆盖）；返回文本 `已读取 <name>（<w>×<h>，<size>）` + `Images`；单次一张 |
| 装配 | `tools/tools.go` 的 `Standard` 纳入 `image.Tool(...)`（与 `run_shell` 同级），`main` 传 `Workspace`/`MaxBytes`/`Resize`/`Detail` |
| 表现层 | `repl` 的工具块通用渲染读 `res.Images`，在结果后追加一行 `[图 <name> <size>]`（复用 `imagesText`）；不需要新视图包 |
| 配置 | **不新增键**，复用 `image_max_bytes` / `image_resize` / `image_detail` |
| 文档 | 完成后更新 `AGENTS.md`（工具与多模态条）、`README.md`、`docs/design.md`《多模态输入》、本节状态与 §6 表 |

### 已拍定（按推荐）

1. **默认进标准集**（模型开箱即可读图）；
2. **路径范围不限**（与 `run_shell` 等价——能读的文件本就能经 `run_shell` 读到，此处只是换成"给模型看"）；
3. **`detail` 参数允许模型指定**，缺省用配置的 `image_detail`；
4. **能力下沉**（`repl/resize.go` → `agent`），`repl` 行为不变、由现有测试守护。

### 测试与验收

- `agent`：mock LLM 断言第二轮的 `messages` 里 `tool` 消息之后紧跟 `user` 且带 `images`（chat 与 responses 两条线各一例）；回滚/中断逻辑不受影响（与内容无关）。
- `tools/image`：路径解析（工作区相对/`~`）、非图像报错、超限报错、缩放生效（尺寸与字节数为实际发送量）、`detail` 覆盖、缺失文件报错。
- `repl`：工具块在结果后出现 `[图 …]` 占位（直接构造 `ToolResult` 断言渲染）。
- 真机：让模型用 `read_image` 读一张图（例如先用 `run_shell` 生成），确认它基于图像内容作答；两协议各验一次。
- 门禁：`gofmt -l` 干净、`go build`/`go vet ./...`、`go test ./...` 与 `-race`、`GOOS=darwin|windows` 交叉编译、`render_audit` 15 PASS。

### 实施记录（2026-10-09）

- **能力下沉**：新文件 `agent/image_data.go` 承接嗅探/尺寸/缩放/EXIF——导出 `ImageResizeLowSide`/`ImageResizeSide`/`JPEGQuality`、`ResizeTargetSide`、`MaybeResizeImage`、`NormalizeImageMIME`、`SniffImageMIME`、`ImageDimensions`、`WebPDimensions`、`JPEGOrientation`（box 降采样、WebP 头解析、APP1/IFD 方向解析随迁，解码器注册随包走）；`repl/resize.go` 与 `repl/attach.go` 只留薄封装（`ImageResizeLowSide`/`ImageResizeSide` 常量别名、`ResizeTargetSide`/`MaybeResizeImage`/`SniffImageMIME`/`ImageDimensions`/`webpDimensions`/`normImageMIME`/`jpegOrientation` 转发），**现有 `repl` 测试零改动**通过。`agent` 仍只依赖标准库。
- **`ToolResult.Images`**：`agent/tools.go` 增字段；`agent/agent.go` 的 `runTurn` 在本轮**全部** tool 消息之后按聚合的 `res.Images` 追加 `Message{Role:"user", Images:…, Content:fmt.Sprintf(MsgToolImageNoteFmt, strings.Join(工具名, MsgImageNameSep), ImageNames(…))}`——不放单个 tool 消息之后，否则并行调用时 user 会插进 tool 消息中间、打断 `tool_calls → tool` 相邻性（严格后端 400）；新常量 `MsgToolImageNoteFmt`（`（%s 附图：%s）`）与 `MsgImageNameSep`、新助手 `ImageNames`（缺名回落 `image`）。文案不含 base64；`totalTokens`/回滚/中断逻辑因与内容无关而无需改动。
- **`tools/image`**：`Config{Workspace, Home, MaxBytes, Resize, Detail}`（`MaxBytes ≤ 0` 回落 `agent.DefaultImageMaxBytes`，`Detail` 装配期归一化），`Invoke` 收 `{path, detail?}`——空 path/不存在/非常规文件/超限/非图像/坏 `detail`/坏 JSON 各自报错（失败不附图），成功返回 `已读取 <名>（<宽>×<高>，<大小>）` + 单张 `ImageRef`（`Bytes`/`Width`/`Height` 记为缩放后值，`Detail` 记生效档位用于 wire）。尺寸不可知（如坏 WebP）回落「尺寸未知」。
- **装配**：`tools.Standard` 顺序定为 `run_shell` → `read_image` → `get_time` → `get_env` → `calc`（`tools/tools_test.go` 期望值同步），`tools.Options` 增 `ImageMaxBytes`/`ImageResize`/`ImageDetail`，`main` 从 `cfg` 传入（**无新配置键**）。
- **表现层**：`repl/toolview.go` 的 `toolEndBody` 在结果正文后追加 `  [图 <名> <大小>]`（`imagesText` 复用 + `term.Truncate` 按宽度截断），自带视图工具与通用回落两条路径都生效；`ask` 单发共用同一渲染。
- **测试**：`agent/tool_image_test.go`（桩工具 + mock LLM 三例：两协议各一例断言 tool 消息之后带图 user 消息、文案含工具名与文件名且不含 base64、chat 的 `content` 为数组含 `image_url`、responses 的 `input_image` 为字符串 data URL；并行工具调用例断言图像 user 消息在本轮全部 tool 消息之后且为最后一条）；`tools/image/image_test.go` 7 例（工作区相对 + low 档缩放与 base64 字节自洽、`detail` 覆盖改目标长边、关闭缩放保留原尺寸、`~` 展开、七类错误、definition/required 契约）；`repl/toolimage_test.go` 3 例（结果后占位行且顺序正确、通用回落带占位、无图不出现）。
- **真机取证（2026-10-09，`make build` + 真实网关 `nas.lan:28149`）**：测试图 400×300 红底（230,30,30）+ 蓝矩形（20,90,220）。① responses（默认配置）：工具块 `▸ read_image path: /tmp/p4_read_image.png` → `已读取 p4_read_image.png（400×300，1.1k）` → `[图 p4_read_image.png 1.1k]`，模型答「背景红色约 #ED1C24、矩形蓝色约 #1560E0、x 100–300 / y 85–220」；② chat（`-c` 临时配置 `api_protocol: chat`）：模型自行传 `detail: high`（标题行显示 `· detail: high`，验证覆盖生效），答「≈ #DC1E1E / ≈ #1C5FD6、水平居中垂直略偏上」。两条线均确实基于图像内容作答。
- **门禁**：`gofmt -l` 干净、`go build`/`go vet ./...`、`go test ./...` 与 `go test -race ./...` 全绿、`GOOS=darwin|windows go build` 通过、`render_audit` 15 PASS / 0 FAIL。

## 8 风险与不做项

- **体积**：内联会让会话文件与每轮请求体随图数增长；缓解手段（blob、压缩、只发首轮）都需独立评估，v1 不做。
- **上游静默忽略**：`auto-*` 别名后端行为不一（§2.2）→ 文档须提示"要严格保证请固定具体模型"。
- **token 成本**：每轮全量重发历史图像，靠 prompt cache 命中缓解；本地估算偏低需在文档说明。
- **不做**：工具产图回传、模型输出图像、`file`/视频/音频块、图像生成与音频转写（网关未代理）、上下文裁剪、跨厂商 `file_id`。
- **网关侧只读纪律**：实测优选用 `proxy` 端点（受控转发，会消耗上游额度），不做任何管理面写操作。

## 9 验收判据（总）

1. 纯文本会话：请求体逐字节不变（golden 断言 + `cache_probe` 复验）。
2. 带图会话：两协议请求体形状符合 §2.1；模型在真机冒烟中确实基于图像作答。
3. 全链路自洽：`/history` 占位、会话列表摘要、归档卷所含内容、`/load` 复原、`/fork` 继承、noSave 不落盘，逐项有单测或手工取证。
4. 门禁：`gofmt -l` 干净、`go build`/`go vet ./...`、`go test`/`-race ./...`、`render_audit` 全绿。
