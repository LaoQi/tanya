# prompt cache 机制探测（参考记录）

tanya 的 history 全程 append-only、system 前缀冻结、工具描述按请求组装，这些设计都建立在"对端有前缀缓存"这一假设上。其中"system 前缀冻结"是**需要代码维护的不变量**：规则快照在 `/new`/`/load` 冻结，环境段自 2026-09-14 起在 `agent.New` 构造期定格（此前 `WORKSPACE` 行每轮重探、是 system 内唯一会自行变化的内容，见《落实：env 段的动态源》）。本文记录该假设的实测验证结论，以及"改动 prompt 中的哪一部分会损失多少缓存"的量化台阶，供后续设计（动态工具描述、运行时改配置、会话注入）引用。

探测脚本：`scripts/cache_probe.py`（标准库，无依赖），结论对应 golang 侧 `llm.go` / `llm_responses.go` 的请求构造。

## 结论总表

实测对象为 OpenAI 兼容聚合网关（内网转发，`/chat/completions` 与 `/responses` 双协议同构）。按后端分两类：

| 后端 | 缓存写入延迟 | 前缀部分匹配 | 多轮收益起点 | 可依赖性 |
|---|---|---|---|---|
| DeepSeek 系（`deepseek-flash` / `oc-dsv4-flash` / `oc-dsv41-flash`） | **即时**（变体首次请求即命中前半） | ✅ | 第 2 轮 | 可依赖、可预算 |
| GLM 系（`glm-5.3-flash`、`auto-flash` 路由到它） | 滞后 1–2 次请求 | ❌ | 第 3 轮 | 仅"完全相同请求"受益 |
| `qwen3.8-flash` | 滞后 1 次请求 | ❌ | — | 同 GLM 模式 |
| `kimi-for-coding` / `k3` | — | — | — | 网关 404，模型未开放 |

判据：`auto-flash` 与 `glm-5.3-flash` 对同一请求体报出**完全相同**的 `prompt_tokens`（5453），而 DeepSeek 系是 4984（不同 tokenizer）→ auto 实际路由到 GLM 后端，行为随 GLM 变化。

## 机制细节

- **粒度 64 tokens**：命中值绝大多数可被 64 整除（5760=90×64、6144=96×64、7168=112×64、6848=107×64、5504=86×64…），即"命中 = 完整命中的 64-token 块数 × 64"。偶见非对齐值（实测 1 例 `5503/5637`，两次独立复测未能重现），说明按块命中是主路径但非严格保证。
- **尾部永不命中**：自命中时 `prompt - hit` 为 60–195（DeepSeek）或 1–64（GLM），即末尾未满块 + chat 模板尾标记。两家 `cached_tokens` 报告口径不同，不可跨后端直接比较。
- **序列化顺序：messages/instructions → tools**。改工具描述不会击穿 system+历史前缀，只损失"差异点之后的 tools 段"；改 system 中部则暴跌。
- **温度不影响命中**：同一内容 `temperature` 缺省/1.0/0.0 三档命中值完全一致。
- **TTL ≥ 600s**（300s、600s 复测均满命中，未探到过期）。
- **写入延迟是分水岭**：DeepSeek 变体请求**第一次**即得前缀命中（5888/6597）；GLM/Qwen 的变体第一次为 0，重复同一内容 2–3 次才满——而这个"满"是**自命中**，不是前缀匹配能力。

## tools 段位置台阶（DeepSeek 后端，单次实测样例）

`X0 建立 hit=0` → `X0 自命中 5504` → 改动后：

| 变体 | prompt | hit | 说明 |
|---|---|---|---|
| 改 `tools[0].description` 一词 | 5639 | 4864 | 差异点在 tools 段开头，损失最大 |
| 改 `tools[last].description` 一词 | 5639 | 5248 | 差异点靠后，损失更小 |
| 无 tools | 4725 | 4480 | messages 部分的命中基线 |
| 改 system 中部一字 | 5639 | 2176 | 击穿 messages 前缀（**2.5 倍代价**） |

推论：**把易变信息放在工具列表末尾的工具描述里最省**（每次变化仅损失该点之后的 tools tokens，约 0.3–1.3k），挂在第一个工具（当前 `run_shell` 的位置）最贵；多轮 append-only 历史在 DeepSeek 后端下每轮命中 ≈ 上一轮全长（差值 39–119）。

## 复现

```bash
python3 scripts/cache_probe.py --group tools --model deepseek-flash --seed 20260515
python3 scripts/cache_probe.py --group stability --model deepseek-flash --ttl 300
python3 scripts/cache_probe.py --group turns --model deepseek-flash --turns 6
python3 scripts/cache_probe.py --group models --models deepseek-flash,glm-5.3-flash,auto-flash,qwen3.8-flash
python3 scripts/cache_probe.py --group preheat --model glm-5.3-flash --repeats 5
python3 scripts/cache_probe.py --group all --model deepseek-flash
```

- 端点与凭证读取顺序：`TANYA_BASE_URL`/`TANYA_API_KEY` > `-c` 指定文件 > `~/.config/tanya/config.yaml` > `./config.yaml`。
- **每次探测必须以新 seed 或新内容开始**：内容一旦被缓存，后续同 seed 的"建立"请求也会满命中，无法观察冷启动。
- `--gap`（默认 3s）是请求间隔；`--ttl N` 触发 TTL 检测（会等待 N 秒）。
- 所有 hit 后带 `[非 64 对齐!]` 标记的行即上文偶发异常。

## 对 tanya 设计的含义

- **动态工具描述成本可控**：仅在 DeepSeek 后端成立，且代价可量化（按 64 对齐、随改动位置递增）。GLM/Qwen 后端本就没有可依赖的前缀缓存，多轮对话第 1–2 轮零收益，动态工具描述不构成额外损失。
- **工具顺序必须稳定**：交换工具顺序会在 tools 段开头断开（实测命中 5248 → 5120）。`allTools()` 的显式顺序（`run_shell` → `builtinTools()`）满足；条件注册（如 `run_shell` 缺失时）会从变化点起失效，影响面限于 tools 段。
- **不要把易变状态注入 system 前缀**：env 段变化会击穿 messages，代价是整个会话前缀；tanya 侧的落实方式是「只在构造期算一次」（见《落实：env 段的动态源》）。
- **假设不可默认成立**：多后端并存时，缓存收益要按后端判定；`auto-flash` 这类"自动路由"模型的缓存行为不可预期。

## 落实：env 段的动态源（2026-09-14）

`envSection` 的 `WORKSPACE` 行曾每轮请求重算（`os.Stat` 8 个标记文件），是 system 段里唯一会自行变化的内容。用本文同一方法实测（deepseek-flash，system = `DefaultSystemPrompt` + env 段 ≈ 0.7 KB，history ≈ 3.9 k token 的构造文本，tools 段 1 个 `run_shell`）：

| 请求 | prompt | hit | 命中率 |
|---|---|---|---|
| A 建立 | 3973 | 0 | 0% |
| A 复测 | 3973 | 3839 | 96.63% |
| B（`WORKSPACE` 多一行，+10 token） | 3983 | 128 | **3.21%** |
| B 复测 | 3983 | 3840 | 96.41% |
| A 回退 | 3973 | 3839 | 96.63% |

结论与《tools 段位置台阶》同源：差异点只要落在 system 内部，代价就是「其后全部内容」——env 段虽在 system 最末尾，之后仍挂着整个 history 与 tools 段，「变化点靠后所以便宜」不成立。真实会话里同样会发生：目录内新建/删除 `go.mod`/`Makefile`/`package.json` 等任一标记文件（`go mod init`、`npm init`、`touch Makefile` 都是常见操作）即触发一次全量 miss，历史越长损失越大。

落实：`envSection(cwd, profile)` 改为在 `agent.New` 调用一次、结果定格进 `Agent.env`，会话期间（含 `/new`、`/load`）不重算；`envProbeFunc` 注入机制删除；`WORKSPACE` 行随后**整行删除**（收益只有「省一次 `ls`」，却让 system 存在自行变化的输入，不划算）——上表数据保留为这次击穿的实测记录。守卫用例 `agent/agent_test.go` `TestEnvStableInSession`（构建后改动目录内容不得改变 `runtimePrompt`）。设计见 `docs/design.md`《环境段（envprobe）》。

复现：沿用本文 `--group tools` 的思路，把 system 换成 `DefaultSystemPrompt + envSection(cwd, profile)`，在 env 段里增删任意一行即可。

## 未验证项

- 网关是否自身叠了一层响应缓存（GLM 的"滞后命中"更像网关侧预热，而非上游缓存）；
- 更长 TTL（>10min）、跨 key / 跨账号隔离；
- 官方 DeepSeek 直连（此处 `dsv4`/`dsv41` 为网关转发命名）；
- 其他网关（非本机实测对象）是否同构。
