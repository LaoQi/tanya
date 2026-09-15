#!/bin/sh
# repl 终端行为验收脚本（pty）。前两条自动跑（仅 /exit，不调用 LLM），其余为人工目视清单。
# 用法：scripts/repl_tty_check.sh
set -u
cd "$(dirname "$0")/.."

command -v script >/dev/null 2>&1 || { echo "需要 util-linux 的 script 命令" >&2; exit 1; }
make build >/dev/null || exit 1

echo "== 1) 欢迎屏 / 提示符 / 退出（观察无残留序列、无多余空行） =="
printf '/exit\n' | script -qec "./tanyan" /dev/null | cat -v || true

echo
echo "== 2) 空回车后退出（提示符与回显不粘连） =="
printf '\n\n/exit\n' | script -qec "./tanyan" /dev/null | cat -v || true

cat <<'TIP'

== 人工目视（需交互） ==
3) 状态行追加语义：script -qec "./tanyan" /dev/null，跑一条 sleep 30 的命令，看 `▸ 工具名` 只出现一次、
   `  » 执行中 0s `（秒数与点之间一个空格位）起行后每秒一个点、满 10 点换行、结束追加 `  ↳ exit 0 · …`，且输出中 \x1b[1A 出现 0 次、块间无空行错位；
   含思维链的模型另有 `» 等待响应 0s ` → `» 思考中 3s ` 的相位切换（秒数连续、每次请求只多一行）
4) 标题区形态：让模型跑一条短命令、一条多行脚本（heredoc 或 for 循环）与一条长管道——短命令内联 `▸ run_shell ls -la`；
   多行/超长命令转块形态（`▸ run_shell` + `  $ …` 逐行折行，缩进与空行保留、tab 摊平成 4 空格、无一行发生终端折行）；
   超过 8 行的命令在头 6 行后出现 `  $ … 省略 N 行（完整命令见 /history）`，且 `/history n` 能看到完整参数
5) 补全菜单 / ghost / picker：键入 / 或 /load 后按 Tab、/load 回车，观察移动无残影、Esc 返回提示符正常
6) Ctrl+C 中断回合后核对回显 / 光标 / 颜色（SGR 无泄漏），退出后 stty -a 与进入前一致
TIP
