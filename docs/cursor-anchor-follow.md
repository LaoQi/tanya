# 终端锚点跟随（已实施）与 DSR 判据（留档未实施）

结论：2026-10-10 实施**跟随**（本文称 C2）——租约释放时把「归位点」改写为子进程留下的位置。
另一条能同时保住两条旧回归门的方案（DSR 位置判据，本文称 A）**留档不实施**，理由见 §5。

## 1. 问题（用户报告）

`run_shell` 工具执行时，子进程显示在终端上的输出被 tanya 的后续渲染覆盖，**输出较长导致翻页、
或命令需要交互时尤其明显**。

复现（`scripts/render_audit.py` 的 VT 回放 + pty 驱动真实二进制，100×32）：

| 场景 | 命令 | `overwrite` |
|---|---|---|
| interactive 短输出 | `seq 1 5`（`interactive: true`） | **4**（屏幕上只剩 `1`，2–5 被工具结果块抹掉） |
| interactive 长输出翻页 | `seq 1 40`（`interactive: true`） | **19** |
| 非交互、子进程直写 tty | `seq 1 60 > /dev/tty` | **9** |
| 非交互、输出走管道（普通情形） | `seq 1 60` | 0（干净） |

对照实验：把抓流里唯一一处归位序列 `\x1b8` 替换为「移到底部新行」后重放，`overwrite` **19 → 0**。

## 2. 根因

1. 借出终端前 `ctty.SaveCursor`（`\x1b7`，`DECSC`）把光标位置存进终端保存槽——`readline`
   两条路径都做：`lease_posix.go` 的 `anchorTerminal`（`LendStdin`，普通 run_shell）与
   `lease_linux.go` 的 `prepare`（`LendFull`，`interactive: true`）。
2. 借出期间子进程输出**直通终端**（interactive 经 pty 泵；非交互是子进程自己写 `/dev/tty`），
   把光标往下推进、甚至滚屏。
3. 释放时 `ResetModes` + `ctty.RestoreCursor`（`\x1b8`，`DECRC`）把光标拉回**借出前的行号**；
   而 `DECSC` 存的是绝对行号，滚屏后该行号已指向别的内容（子进程输出的中段，或子进程写的第一行）。
4. tanya 接着从那里写工具结果块 → 逐行覆盖子进程刚显示的输出。

普通非交互命令（stdout/stderr 走管道、不回显到终端）本来就干净，故此前未被发现。

## 3. 跟随语义（已实施）

释放序固定为：

```
SaveCursor        # 借出前存一次：给复位串里的 ?1049l 一个合理槽值
  子进程运行
SaveCursor        # 释放时再存一次（2026-10-10）：归位点 := 子进程留下的位置
ResetModes        # SGR/字符集/模式、?1049l、CSI r
RestoreCursor     # 归位到上面那次存档
```

改动仅两处、各一行：

- `readline/lease_posix.go`（`anchorTerminal` 的释放闭包）：`ctty.ResetModes` 前补 `ctty.SaveCursor`。
- `readline/lease_linux.go`（`bridgeTTY.release`）：同上。

`ResetModes` 串内仍不自带 `DECSC`/`DECRC`（否则会覆盖调用方的存档槽），这一点未变。
`SaveCursor` 写在 `ResetModes` **之前**是硬约束：`CSI r` 会把光标 home 到 (1,1)、`?1049l` 会恢复保存槽，
先存才能把「子进程留下的位置」保住。

**子进程停在备用屏的情况**：`DECSC` 按屏索引（xterm 的 `sc[whichBuf]`），此时释放前的存档落在备用屏槽，
不破坏主屏槽；`?1049l` 退回主屏并恢复主屏槽（= 借出前位置）→ 行为与改动前一致。实测 `vim` /
`htop` / `watch` 被 KILL 均如此。

## 4. 实测与回归门

**真实命令探针**（pty 中直接跑，抓原始输出后按「tanya 屏幕 + 锚点 + 命令输出 + 释放序」回放）：

| 类别 | 命令 | 结论 |
|---|---|---|
| 纯文本输出 | `seq`、`read -s -p`、`ssh -tt`、`python3`／`node` REPL、`sqlite3`、`gpg`、`journalctl --no-pager` | 光标停在锚点下方 → **跟随修复覆盖**（改动前覆盖 1–14 格） |
| 备用屏程序 | `vim`、`less`、`man`、`htop`、`info`、`watch`、`dpkg -l \| less`（含被 KILL） | `?1049l` 自行复原，改动前后一致 |
| 主屏原地重画 | `top`、`more`、`less -X`、`clear` | 程序自己重画/清屏（含 tanya 历史），两种策略都救不了 |
| 只动光标/设滚动区 | `printf '\033[3;7H'`、`printf '\033[20;24r'`（常与 KILL 组合） | 光标留在锚点**上方** → 跟随会把 tanya 的输出写到上方 |

**审计门变化**（`scripts/render_audit.py`，17 场景）：

- 新增 `clean-interactive-short`（interactive `seq 1 5`）与 `clean-interactive-long`（interactive `seq 1 60`）——
  两条在改动前分别报 `overwrite 4` / `19`（负向对照：临时回退两行改动即双双变红），改动后干净。
- `clean-tty-cup` / `clean-tty-scrollregion` 改判为 `leak-tty-cup` / `leak-tty-scrollregion`
  （`want: leak` + `expect: ["overwrite"]`，实测 78 / 43）——它们是上表第四类的既定代价，用 `leak` 钉住，
  以免将来行为再变时无人知晓。
- 其余 clean 门（含 `clean-alt-screen-exit`、`clean-tty-modes`、`clean-interactive-release`）不变。

## 5. A 方案（DSR 判据）：留档与否决理由

**方案**：释放前用 `DSR`（`\x1b[6n` → `CSI r;cR`）读出子进程留下的行 `Rn`，`ResetModes` 后再 `\x1b8`
归位并读出锚点行 `Ra`；`Rn < Ra` 则保持归位（第四类场景），否则跟随（C2 的做法）。判据只多一条分支，
跟随分支与 C2 完全相同。

**收益**：保住 `clean-tty-cup` / `clean-tty-scrollregion` 两条门。

**投入**（相对 C2）：

- `ctty` 新增 `QueryCursor`（写 `\x1b[6n`、按超时读、解析回复）→ posix 实现 + windows（或恒 false）+ stub。
- readline 的 device 面新增「输入回灌」方法（DSR 回复之外的字节、含用户提前键入必须留存并塞回
  `keySource`）→ `device_posix.go` / `device_windows.go` / `device_pipe.go` / `device_stub.go` 四份实现
  + 各测试的 fake device。
- `console.go` 的 `LendStdin`/`LendFull` 目前只调包级 `newStdinLease()`／`lendFullImpl()`，不持有 device，
  需要把 device 串进租约。
- `scripts/render_audit.py` 的 pty 驱动要维护屏幕模型并应答 `\x1b[6n`（否则新分支测不到，只能靠单测）。

**风险**：

- 时序与输入混流：DSR 回复走 tty 输入，用户可能在交互程序刚退出时就打字；处理不当会吞键或产生假按键。
- 查询时机：`Rn` 必须在 `ResetModes` 之前取（`CSI r` 会 home）；借出前那次查询要在 park 之后、
  raw 模式之外做，需要临时改 termios 或 poll 超时。
- **回退即现状**：不支持 DSR 的终端（部分 IDE 内嵌终端、老 conhost、某些多路复用环境）超时后只能退回
  归位语义——用户报告的覆盖在这些环境仍然存在，故 A 的收益并非全终端覆盖。
- 延迟：每次借出多 1–2 次约 50–100 ms 的往返。
- 平台面扩大：Windows 的 `LendFull` 走 `CONIN$` 直通、`lease_windows.go` 里也没有锚点序，需要单独决定走 C2 还是不支持。

**决策（2026-10-10）**：不实施 A，只上 C2。判据是收益/成本比：

1. 用户报告的全部场景（纯文本输出的长/短输出、非交互子进程直写 tty）由 C2 一次修掉，
   而 A 相对 C2 多出的收益只有「子进程只挪光标/设滚动区且不复位」一类——16 条真实命令探针里
   只由两条人工构造场景触发。
2. 这类场景的后果（tanya 从屏幕上方续写、覆盖自己的历史）与「程序自己重画/清屏」同级：
   `top` / `clear` / `less -X` 已经在做同样的事，锚点策略救不了，用户对这类形态早有预期。
3. C2 改动 2 行、复用现有原语、**不依赖终端能力**（所有终端一致生效），且能被现有审计框架端到端验证；
   A 需要跨 4 个包的基础设施与一个新的终端协议交互面，且在不支持 DSR 的终端上等于没做。

**重启条件**（出现下列任一情形时再评估 A）：

- 真实使用中遇到「残留滚动区 / 子进程把光标留在上方」并造成可见困扰（例如交互命令被 `^C`/超时杀掉后，
  后续输出出现在屏幕上方）；
- 需要把 tanya 的输出位置语义推广到更多借用方（例如未来的其他交互式工具），届时判据是共用基础设施。

## 6. 未做与已知代价

- 子进程写 `/dev/tty` 的**内容**（密码提示、半行残文）既不进采集（模型看不到）也无人清洗——改动后这些
  内容会被**保留**（跟随到它之后续写），审计场景 `note-partial-line` 覆盖此形态。
- 第四类场景的覆盖（`leak-tty-cup` / `leak-tty-scrollregion`）为已裁定代价，收口需 A。
- `darwin`：`LendFull` 本就返回 `ErrNoLend`（无 pty 泵），`LendStdin` 的锚点跟随同 posix 实现；
  `windows`：`lease_windows.go` 无锚点序，本次未涉及。
