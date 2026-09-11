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
3) 工具块 inline 重绘：script -qec "./tanyan" /dev/null，跑一条命令看 ⋯ 是否被替换为最终标题，块间无空行错位
4) 补全菜单 / ghost / picker：键入 / 或 /load 后按 Tab、/load 回车，观察移动无残影、Esc 返回提示符正常
5) Ctrl+C 中断回合后核对回显 / 光标 / 颜色（SGR 无泄漏），退出后 stty -a 与进入前一致
TIP
