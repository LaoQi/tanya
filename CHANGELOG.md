# Changelog

tanya 变更记录。新条目加在最上方的日期分节内（没有当天分节就新建一个）；本文件记变更，AGENTS.md 只保留现行约束。

## 2026-09-20
- feat(agent,repl,docs): `/switch <dir>` 切换工作区——`Agent.loadWorkspace` 抽出 `New` 的装配路径，按新目录原子重建 shell 工具/system 提示/会话存储/env 段并放弃当前会话（历史清空、会话轮转，旧文件不动）；`run_shell` 默认目录由进程 cwd 改为当前工作区（显式 `cwd` 才回显），提示符 `{cwd}`/`/stat`/归档卷注释同步跟随，目标非法或等同当前时拒绝且状态不变
- fix(agent): responses 协议 reasoning 回传补 `summary` 空数组——阿里（dashscope responses 兼容层）等端点要求 input 中 reasoning item 必须携带 summary 列表，缺失时第二轮起 400 Invalid 'summary'；OpenAI 侧该字段本就合法，空数组双向兼容

- feat(render,repl,docs): markdown 表格渲染——IR `Table` 块 + 三行前瞻定列宽（表头/分隔/首数据行）+ 外框与列对齐；列宽不封顶、超宽单元格不截断，终端宽度只用于切紧边距；/theme 样例与 README 补表格展示
- chore(docs): Windows 验证条目退出 todos 跟踪——todos 删去 B1/B2 验证项，文档口径由「实机验证待做」改为「仅部分实机验证、未全量覆盖、暂不跟踪」（AGENTS/README/terminal-caps/windows-console-mode-restore）
- feat(agent,repl,docs): /fork 通用化——任何会话可 fork 成新分支（原会话文件不动、可 /load 回切），sessionStore.rotate 同秒重名退 -2/-3 后缀；MsgForkDone 报继承条数
- chore: AGENTS.md 精简（行为细节收敛到 docs、只留硬不变量），新增 CHANGELOG.md 承接变更记录
- feat(repl,main,docs): 动态文本转义清洗收口与 /load picker 屏高分窗

## 2026-09-18
- refactor(repl,agent,docs): 归档提示统一——启动自动归档与 /archive 合流 archiveFlow，auto_archive 默认开启
- feat(repl,readline,agent,docs): /archive 重做为预览确认制——参数口径改保留数/单单位时长，确认读不入历史
- feat(agent,repl,docs): 启动自动归档与 /archive --dry-run——询问门禁与候选计数对齐
- feat(agent,repl,docs): 会话归档（zip 卷）与 /archive、/fork、归档只读载入

## 2026-09-17
- docs: 补 windows-console-mode-restore.md 末尾换行
- fix(ctty,agent,docs): 输入模式快照/复原跨平台化——Windows 交互回合后 CONIN$ 不复原
- feat(ctty,agent): Windows 交互链路 B2——控制台继承直通可应答
- feat(ctty,agent,main): Windows 编码链路——代码页探测切换与 run_shell 兜底转码
- feat(ctty,readline,repl): 运行期信号统一抽象——SIGTERM/SIGHUP 优雅退出收敛到 REPL.quit()
- docs: 清理文档漂移——Windows 输入后端状态、Truncate 复位与宽度口径裁决
- fix(repl,docs): 思维链显示 review 修复——帮助对齐、门禁判定去重与门禁外提示
- test(agent,docs): schema golden 与文档同步并加守卫——config_path 描述补齐
- feat(agent): agent_custom 追加只读 key config_path——模型可感知生效配置文件
- feat(repl,render,agent): 思维链显示——show_reasoning 配置与 /reasoning 开关，markdown 渲染夹分隔符
- docs(agents): 精简设计约束——机制细节收敛为文档指针
- feat(repl,agent): 退出收尾三行——会话 id/时长/用量/落盘路径取代「再见」

## 2026-09-16
- fix(repl,docs): 执行相位心跳去前导空格——与等待/思考相位同列起行
- test(scripts,docs): 探针按实测建模与光标锚点回归门——三条新门 + 文档同步
- fix(ctty,agent,readline): 光标锚点前置于子进程——修 run_shell 后块体从屏幕顶部覆盖旧数据
- test(ctty,readline,scripts): 守卫测试与探针按屏分槽——备用屏场景入册，文档同步
- fix(ctty,agent,readline): 复位串补字符集、前台门控收紧、残片与 Windows 读不丢字节
- fix(ctty,agent): 屏幕模式复位回归并包裹光标副作用——修 interactive 后块体从顶部覆盖旧数据
- test(scripts): 输出侧渲染审计探针——mock LLM + pty 驱动 + VT 回放与不变量断言
- fix(readline): 修复残片超时导致的越界 panic（Windows 输入中文触发）
- feat(readline): Windows 输入后端——VT 输入模式，ghost 与补全随之生效
- refactor(readline): 抽出平台无关的按键状态机 keySource
- feat(ctty,repl): 终端探测与能力降级拆分——渲染看 stdout、输入看 stdin，Windows 显示恢复
- docs: 删除 agent-split.md 并重定向引用
- refactor(agent): shell 平台层收敛——零值兜底、GOOS 单源、linux/darwin 分片与 Windows 树杀
- docs(config): 校正 config.example.yaml 过时注释
- refactor(agent): shell 平台抽象——一张表承载平台差异，描述与探测清单按平台生成
- docs: 文档与标题统一为 tanya
- refactor: 用户可见文案与测试夹具统一为 tanya
- refactor: XDG 路径统一为 tanya——配置/全局会话/全局 AGENTS.md
- build: 构建产物改名 tanyan → tanya
- refactor: module path 统一为 tanya
- docs: 同步 design/AGENTS 与实现——斜杠命令清单、/sessions 移除、style→render/term 改名、配置项补全
- feat(repl,render): 工具标题区命令可读性——折行显示完整命令替代单行截断
- refactor(repl,readline): 状态展示追加化替代 spinner；readline 挂断归一 io.EOF
- fix(ctty,agent): 模式复位不再 home 光标——去掉 DECSTBM 并限缩到桥接路径

## 2026-09-15
- docs: 登记输出侧转义清洗缺口——附录 C 补状态行清洗条目与未做路径清单
- fix(agent): run_shell 复原控制终端——termios 快照/复原 + ResetModes，自愈扩宽到 canonical/输出后处理
- feat: init 子命令——新工作区脚手架（.tanya/sessions + AGENTS.md 骨架），幂等且不覆盖既有文件
- docs: agent_custom v2 落地同步——design/AGENTS/README 登记，方案文档重写 + §17 落地偏差
- feat(agent): agent_custom v2 键值化——{action,key,value} + key 表驱动，新增 sessions 自省
- docs: 修正 agent-control-tool §13 用例数 14 → 13
- docs: agent_custom 落地同步——design/README/AGENTS 登记，方案补落地偏差
- feat(agent): agent_custom 运行时自调工具——model/reasoning_effort 动态调整与只读自省
- docs: agent_custom 工具方案定稿——LLM 动态调整模型与思考等级，含只读自省
- docs(agents): 精简项目说明——结构与文档索引收敛，测试约定压成单段
- refactor(agent): LLM 双协议 HTTP/SSE 骨架抽取——llm_http.go 收拢共用路径，类型化 httpError
- refactor(agent): 工具定义下沉——Tool 接口自述描述与参数，toolRegistry 取代 ToolDefs + dispatch switch
- refactor(ctty,agent,readline): 控制终端原语统一抽包——ctty 零依赖叶子 + 平台分片白名单收敛

## 2026-09-14
- test(render/theme): 覆盖率 58.5%→100%——补 ByName/Heading/Apply 未覆盖分支与边界
- refactor(agent,repl): 统计职责分层与口径分离——agent 只出数值快照、repl 独揽格式化，/context 更名 /stat
- feat(agent): chat 协议捕获并回传 reasoning_content——思维链落盘、对齐 DeepSeek tool 轮硬要求
- refactor(agent): 环境段构造期定格——删 probe 注入、WORKSPACE 行与 SHELL 调用形态
- docs: 删除 open-questions——设计结论归位 shell-tool/design/style-split
- docs: Style.Bg 编码路径定为预留能力——不删不补语法（open-questions A5）
- feat(repl): 输出模式收敛——移除 /md、ask 单发默认 plain+verbose、追加式工具块不再重复标题
- feat(repl): /history 回放消息头按角色着色——user 绿、其余黄
- fix(readline): Ctrl+L 再收紧一屏减一——去掉多余的一次滚动
- fix(readline): Ctrl+L 只推一屏高——消除回滚区空行与历史被挤
- refactor(render): style 拆包为 render 树——四域重组、全局状态清零、agent 去表现层依赖
- feat(repl): 工具块 cwd 单独成行 + docs 收敛（移除 env 立层、修 C5）
- refactor(agent): Agent 三簇剥离（stats/prompt/session）+ 测试补齐批次

## 2026-09-13
- refactor(agent,repl): run_shell 组件化（shellTool）——包级状态清零、工具清单构造期注入、bridge 走 Option
- feat(agent): run_shell 支持 cwd 参数——命令级执行目录，相对路径按工作区基准解析

## 2026-09-12
- feat(agent): run_shell 工具描述补工作目录——声明命令在会话启动目录（进程 cwd）下执行
- refactor(repl): 删除不可达的未知命令分支——白名单即分发契约，补白名单守卫用例
- docs: 阶段 5（回放复用实时渲染）拆出独立评估归档，结论建议不做或降级
- feat(repl): 输出模式 rich/plain/plain+verbose——`-p`/`--plain` 与 `--verbose`，plain 供子代理调用
- refactor(repl): toolView 闭包结构体化——状态提为字段，Content 显式携带 Kind
- refactor(repl): flow 上下文与 turn 生命周期——REPL 字段下沉，Profile 读点收敛到构造期
- refactor(repl): 输出收敛到 streams 双流 + Kind 标记——70 处写点与 stderr 7 处全部归口，测试改用注入 writer
- test(repl): 测试地基——输出注入（streams/output）、repl 侧 fakeTerm 与 guard 钩子
- docs: repl 输出收敛方案增补输出模式与双流收敛，决策记录固化
- docs: repl 输出收敛与数据流封装方案（repl-output-refactor）
- fix(repl,style): /history 摘要行经 OneLine 清洗——剥离 ANSI 转义与控制字符再截断，修复回看时整段变黄与行覆盖

## 2026-09-11
- feat(agent,repl): 只读会话模式——`-n` / `--no-save` 关闭会话落盘，ask 与 REPL 通用
- refactor(repl): 归档直通 shell 执行面，裸输入归口 AI 对话；欢迎屏显示版本号与构建时间
- refactor(repl): 目录命令改为全量拦截——移除内建 cd 与 REPL 目录状态，cd/pushd/popd 首段出现即告警不执行（dirChangeCommands 表驱动便于扩展）；直通命令不设 cmd.Dir 落在启动目录，{cwd} 回退 os.Getwd；README/design.md/AGENTS.md 同步改写，剥离步骤简化为删文件/删调用/删文案三步
- refactor(repl): 目录命令抽为可剥离切面——cd 内建与 cd 类拦截集中到 repl/localcmd.go（runShellLine 仅留 tryLocalCommand 单调用点），测试随切面独立成 localcmd_test.go，design.md 记录剥离步骤
- feat(repl): 输入三路分发——「:内容」与 AI 对话、「/命令」控制面、其余在本目录直通 shell；直通保留颜色与交互命令、不进 LLM 上下文，cd 内建（局部 cwd）且子 shell 目录命令拦截告警，^C 与挂起按同组进程语义处理
- fix(repl): 工具块标题显示收敛——标题与参数空格由双改单、截断预算同步减 1，命令内换行折叠为「; 」连接避免标题撑成多行
- feat(repl): 回合分隔线改用 Ok 语义色——dim 灰线翻看时与工具块输出混淆，绿线更易识别回合边界，turnview 断言与 design.md 描述同步
- feat(repl): 每回合末尾打印 dim 短分隔线——`──── HH:MM:SS` 带回合耗时，翻看时回合边界可辨
- refactor: 收敛 noshell 降级——启动即校验 shell，缺失直接报错退出
- docs: 补充 prompt cache 实测结论（cache-probe）
- docs: 精简 AGENTS.md、同步 README
- fix(readline): 输入软换行光标按终端模型定位

## 2026-09-10
- fix: 上下文估算计入 reasoning_items——totalTokens 除正文与工具参数外累加明文思维链，修正长会话下 /context 与提示符 {usage} 的低估（纯本地估算，不改请求构造）
- docs: Git 约定更新——签名改为提交时自动完成（commit.gpgsign=true），约束从「提交后提醒签名」改为「不得 --no-gpg-sign 绕过」
- feat: 交互式 run_shell 全 pty 桥接 + 终端状态自愈
- feat: 工具块 ANSI 过滤双出口——Frame 清洗包裹默认块，Passthrough 保色直显区按 SGR 自动触发，/history tool 消息 Frame 化；模型通道零变换，缓存与历史不受影响

## 2026-09-09
- feat: 版本号构建注入——git describe（tag+短hash+dirty）经 ldflags 写入，-v 显示版本
- feat: Ctrl+C 中断保留已完成产出——有产出时保留历史并追加中断/错误提示落盘、无产出回滚，run_shell 中断语义分级（已中断/未执行），会话落盘改原子写入防重复
- feat: spinner 三态语义色——等待/思考/执行分走 Warn/Think/Run，新增 think/run 语义色接入主题与 palette 可覆盖，模板语义名、/theme 示例与文档同步
- docs: run_shell 交互实测契约——sudo 默认模式 tty 提示实时可见，-S 强制 stdin 致提示被 stderr 捕获不可见，应避免
- feat: run_shell interactive 参数——显式声明交互命令停用等待动画、默认超时放宽 300s，结束改追加式渲染防擦输入回显；env 段补 TTY 契约行并按平台能力条件输出
- feat: run_shell 终端前台移交——子进程可直接应答 ssh/git 密码提示，挂起探测秒级终止
- refactor: 流事件模型——Event 词汇表统一协议增量与生命周期，spinner 补思考中阶段
- fix: Ctrl+L 改为推屏保历史——可见内容滚入回滚区后光标回视口顶部重画提示符，不再擦屏丢近期输出；Size 不可用退化原擦屏行为
- fix: 内置主题提示符结尾箭头统一白色（default 外 6 主题原为 bright_black/cyan）
- refactor: 移除 /sessions——/load 无参会话选择菜单承接浏览，帮助与补全同步收敛
- fix: /new 与 /load 会话重置时清理 lastUsage——消除上一会话 token 用量残留显示
- chore: 系统提示补充先制定方案的执行习惯
- feat: 配色主题系统——Scheme 聚合（语义色+提示符+md 主题）内置 7 主题，/theme 切换并打印样张，theme 配置/env 生效，默认 nord

## 2026-09-08
- fix: 工具块截断预算补齐前缀宽度——消除折行导致的 CursorUp 擦错行残字
- fix: 工具回调改 Close() 结算滞留尾行；行内代码亮色独立；/history 回放走渲染管线
- feat: 富文本管线阶段 3——Markdown 流式渲染与 /md 开关
- feat: 富文本管线阶段 2——BBCode 模板与 palette 配置，提示符切换至 style 管线
- feat: style 包落地——ANSI 感知宽度/截断修复串色，语义色/Profile/IR 骨架，存量渲染点收编
- docs: 富文本渲染管线设计定稿与参考项目对比——IR/渲染分层架构，glow/glamour/reflow/rich/streamdown/mdcat 议题式对比归档
- feat: 新增 responses 协议双通道——api_protocol 配置选择，思维链明文回传（DeepSeek 标准 OpenAI 兼容）

## 2026-09-07
- feat: 提示符新增 {effort} 占位符显示思考等级——默认模板思考黄/用量绿分层，未设置渲染为空
- feat: 支持 reasoning_effort 思考等级——yaml/env 配置与 /think 运行时切换，minimal/low/medium/high/max 五级，缺省不发送
- refactor: 用户可见文案统一抽取为常量——repl/messages.go 与 agent/messages.go 单一来源，新增 sessrow 格式锁定测试
- feat: run_shell 多 shell 支持——按平台解析 shellProfile，常用程序探测拼入工具描述，超时上限提至 900s
- feat: 提示符模板内置固定并新增 {stat} 组合用量占位符
- feat: 环境探针重构——规则/事实分离，envSection 恒定注入系统提示末尾

## 2026-09-06
- fix: Ctrl+C 中断改走 KeyWatcher 按键监听，不再依赖 tty ISIG/SIGINT 环境状态
- fix: Ctrl+C 中断健壮性，信号处理先取消后输出、spinner 有界停止、Ask 出错回滚 history
- feat: braille 等待动画与请求/工具状态行（TTFT·耗时·用量·缓存）、Tab 补全菜单方向键选择
- feat: 工具调用块状终端渲染、ShellResult 结构化返回、tool_output_lines 配置

## 2026-09-05
- fix: readline 渲染多行感知，修复超宽换行导致的重复显示
- feat: shell 进程组杀与流式截断、ctx 中断传递、{cache_rate} 提示符占位符
- feat: AGENTS.md 双层注入与会话级快照、system 持久化、缓存命中显示
- feat: 自研 readline 终端输入层、包结构重组与 REPL 交互增强

## 2026-09-04
- feat: 会话工作区划分与存储模式、usage 捕获、模型显示等
- feat: tanyan 初始版本
