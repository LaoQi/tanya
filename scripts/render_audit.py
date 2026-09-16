#!/usr/bin/env python3
"""tanya 输出侧渲染审计：内置 mock LLM + pty 驱动 + VT 回放 + 不变量断言。

判定口径（与 docs/design.md《测试》的"pty 目视模板"互补，这里是自动化的那部分）：
  1. overwrite      写入非空白单元格（后面画到前面的文本上）
  2. region_scroll  只在滚动区（DECSTBM 子区）内滚动 —— 子进程留下滚动区的典型症状
  3. cu_clamped     相对上移超过光标所在行（会被视口夹到顶行 → 整块从屏幕顶部重画）
  4. autowrap_off / cursor_hidden / margins_set / alt_screen_on / sgr_open  结束时的终端状态

诊断计数（不断言，仅报告）：cursor_restore —— 光标被“恢复”且位置确实发生跳变（裸发
`DECRST 1049` 或 `CSI r` 的典型症状；被 DECSC/DECRC 包住时该恢复为 no-op，不计数）。

场景 want 语义：
  clean  全部不变量必须为 0/off（回归门）
  leak   期望出现 expect 列出的违反项（已知缺口复现；修好后把 want 改成 clean）
  note   只报告不断言（内容层面的已知限制）

用法：make build && python3 scripts/render_audit.py
      [--only NAME]        只跑一个场景
      [--dump NAME]        打印该场景回放后的屏幕
      [--raw DIR]          把每个场景的原始 pty 抓流写到 DIR（排查断言用）
      [--cols N --rows M]  终端尺寸（默认 100x32）
"""
import argparse
import fcntl
import json
import os
import pty
import re
import select
import shutil
import signal
import socket
import struct
import sys
import tempfile
import termios
import threading
import time
import unicodedata
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
TURN_SEP = "\u2500\u2500\u2500\u2500"

# ---------------------------------------------------------------- VT 回放

def char_width(ch):
    if unicodedata.combining(ch):
        return 0
    return 2 if unicodedata.east_asian_width(ch) in ("W", "F") else 1


class VT:
    def __init__(self, cols, rows):
        self.cols, self.rows = cols, rows
        self.grid = [[" "] * cols for _ in range(rows)]
        self.row = self.col = 0
        self.scrollback = []
        self.top, self.bottom = 0, rows - 1
        self.wrap_pending = False
        self.autowrap = True
        self.cursor_visible = True
        self.alt = False
        self.saved_alt = None
        self.saved_slot = (0, 0)
        self.sgr_open = False
        self.events = {"overwrite": 0, "region_scroll": 0, "cu_clamped": 0}
        self.samples = []

    def note(self, kind, detail=""):
        self.events[kind] = self.events.get(kind, 0) + 1
        if len(self.samples) < 40:
            self.samples.append((kind, detail, self.row, self.col))

    def _blank(self, r):
        self.grid[r] = [" "] * self.cols

    def _scroll(self):
        if self.top == 0 and self.bottom == self.rows - 1:
            self.scrollback.append("".join(self.grid[0]).rstrip())
            self.grid.pop(0)
            self.grid.append([" "] * self.cols)
            return
        self.note("region_scroll", "rows %d-%d" % (self.top, self.bottom))
        self.grid.pop(self.top)
        self.grid.insert(self.bottom, [" "] * self.cols)

    def _down(self):
        if self.row >= self.bottom:
            self._scroll()
        elif self.row + 1 < self.rows:
            self.row += 1

    def put(self, ch):
        w = char_width(ch)
        if w == 0:
            return
        if self.wrap_pending:
            self.col = 0
            self._down()
            self.wrap_pending = False
        if not self.autowrap and self.col + w > self.cols:
            self.note("overwrite", "无自动换行时超宽写入 %r" % ch)
            self.col = self.cols - w
        if self.col + w > self.cols:
            self.col = 0
            self._down()
        if self.grid[self.row][self.col] != " ":
            self.note("overwrite", "row=%d col=%d %r → %r" % (self.row, self.col, self.grid[self.row][self.col], ch))
        self.grid[self.row][self.col] = ch
        for k in range(1, w):
            if self.col + k < self.cols:
                self.grid[self.row][self.col + k] = ""
        if self.col + w >= self.cols:
            self.wrap_pending = self.autowrap
            if not self.autowrap:
                self.col = self.cols - w
            else:
                self.col = self.cols - w
        else:
            self.col += w

    def erase_line(self, mode):
        if mode == 0:
            for c in range(self.col, self.cols):
                self.grid[self.row][c] = " "
        elif mode == 1:
            for c in range(0, self.col + 1):
                self.grid[self.row][c] = " "
        else:
            self._blank(self.row)

    def erase_display(self, mode):
        if mode == 0:
            self.erase_line(0)
            for r in range(self.row + 1, self.rows):
                self._blank(r)
        elif mode == 1:
            for r in range(0, self.row):
                self._blank(r)
            self.erase_line(1)
        else:
            for r in range(self.rows):
                self._blank(r)

    def save_slot(self):
        self.saved_slot = (self.row, self.col)

    def restore_slot(self):
        r, c = self.saved_slot
        r, c = min(self.rows - 1, max(0, r)), min(self.cols - 1, max(0, c))
        if (r, c) != (self.row, self.col):
            self.note("cursor_restore", "(%d,%d) → (%d,%d)" % (self.row, self.col, r, c))
        self.row, self.col = r, c
        self.wrap_pending = False

    def _switch_alt(self, on):
        if on and not self.alt:
            self.saved_alt = ([list(r) for r in self.grid], self.row, self.col)
            self.save_slot()
            self.alt = True
            for r in range(self.rows):
                self._blank(r)
            self.row = self.col = 0
        elif not on:
            had = self.alt
            if had and self.saved_alt:
                grid, r, c = self.saved_alt
                self.grid = grid
                self.row, self.col = r, c
                self.saved_alt = None
            self.alt = False
            self.restore_slot()

    CSI_RE = re.compile(r"\x1b\[([0-9;?]*)([ -/]*)([@-~])")

    def feed(self, data):
        i, n = 0, len(data)
        while i < n:
            ch = data[i]
            if ch == "\x1b":
                m = self.CSI_RE.match(data, i)
                if m:
                    params, inter, final = m.group(1), m.group(2), m.group(3)
                    self.csi(params, final)
                    i = m.end()
                    continue
                m2 = re.match(r"\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)", data[i:])
                if m2:
                    i += m2.end()
                    continue
                m3 = re.match(r"\x1b[()][0-9A-Za-z]", data[i:])
                if m3:
                    i += m3.end()
                    continue
                if i + 1 < n and data[i + 1] in "78":
                    if data[i + 1] == "7":
                        self.save_slot()
                    else:
                        self.restore_slot()
                    self.wrap_pending = False
                i += 2
                continue
            if ch == "\r":
                self.col = 0
                self.wrap_pending = False
            elif ch == "\n":
                self._down()
                self.wrap_pending = False
            elif ch == "\b":
                self.col = max(0, self.col - 1)
                self.wrap_pending = False
            elif ch == "\t":
                self.col = min(self.cols - 1, (self.col // 8 + 1) * 8)
                self.wrap_pending = False
            elif ch in ("\x07", "\x00"):
                pass
            elif ch == "\x0b" or ch == "\x0c":
                self._down()
            else:
                self.put(ch)
            i += 1

    def csi(self, params, final):
        nums = [int(p) for p in params.lstrip("?").split(";") if p.isdigit()]
        first = nums[0] if nums else 0
        if final == "A":
            k = first or 1
            if k > self.row:
                self.note("cu_clamped", "上移 %d 行但光标在 %d 行" % (k, self.row))
            self.row = max(0, self.row - k)
            self.wrap_pending = False
        elif final == "B":
            self.row = min(self.rows - 1, self.row + (first or 1))
            self.wrap_pending = False
        elif final == "C":
            self.col = min(self.cols - 1, self.col + (first or 1))
            self.wrap_pending = False
        elif final == "D":
            self.col = max(0, self.col - (first or 1))
            self.wrap_pending = False
        elif final == "E":
            self.col = 0
            self.row = min(self.rows - 1, self.row + (first or 1))
        elif final == "F":
            self.col = 0
            self.row = max(0, self.row - (first or 1))
        elif final == "G":
            self.col = min(self.cols - 1, max(0, (first or 1) - 1))
        elif final == "H" or final == "f":
            r = (nums[0] if len(nums) > 0 else 1) - 1
            c = (nums[1] if len(nums) > 1 else 1) - 1
            self.row, self.col = min(self.rows - 1, max(0, r)), min(self.cols - 1, max(0, c))
            self.wrap_pending = False
        elif final == "J":
            self.erase_display(first)
        elif final == "K":
            self.erase_line(first)
        elif final == "r":
            t = (nums[0] if len(nums) > 0 else 1) - 1
            b = (nums[1] if len(nums) > 1 else self.rows) - 1
            if 0 <= t < b < self.rows:
                self.top, self.bottom = t, b
            else:
                self.top, self.bottom = 0, self.rows - 1
            self.row, self.col = self.top, 0
            self.wrap_pending = False
        elif final == "h" or final == "l":
            on = final == "h"
            if params.startswith("?"):
                for p in nums:
                    if p == 7:
                        self.autowrap = on
                    elif p == 25:
                        self.cursor_visible = on
                    elif p == 1049:
                        self._switch_alt(on)
        elif final == "m":
            seq = nums or [0]
            if 0 in seq and len(seq) == 1:
                self.sgr_open = False
            else:
                self.sgr_open = True

    def dump(self, tail_only=False):
        lines = [] if tail_only else list(self.scrollback)
        for r in self.grid:
            lines.append("".join(c for c in r if c != "").rstrip())
        return "\n".join(lines)

    def state(self):
        return {
            "autowrap_off": not self.autowrap,
            "cursor_hidden": not self.cursor_visible,
            "margins_set": not (self.top == 0 and self.bottom == self.rows - 1),
            "alt_screen_on": self.alt,
            "sgr_open": self.sgr_open,
        }

# ---------------------------------------------------------------- mock LLM

class MockLLM:
    def __init__(self, steps):
        self.steps = list(steps)
        self.requests = []
        outer = self

        class Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, *a):
                pass

            def do_POST(self):
                length = int(self.headers.get("Content-Length", 0))
                body = self.rfile.read(length)
                try:
                    outer.requests.append(json.loads(body or b"{}"))
                except ValueError:
                    outer.requests.append({})
                if not self.path.endswith("/responses") or not outer.steps:
                    self.send_error(500, "no more steps")
                    return
                step = outer.steps.pop(0)
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.send_header("Connection", "close")
                self.end_headers()
                for chunk in outer.sse(step):
                    self.wfile.write(chunk.encode())
                    self.wfile.flush()
                self.wfile.write(b"data: [DONE]\n\n")
                self.wfile.flush()

            do_GET = do_POST

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.port = self.server.socket.getsockname()[1]
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    def close(self):
        self.server.shutdown()
        self.server.server_close()

    @staticmethod
    def sse(step):
        out = []

        def send(event, payload):
            payload["type"] = event
            out.append("event: %s\ndata: %s\n\n" % (event, json.dumps(payload, ensure_ascii=False)))

        if step.get("reasoning"):
            send("response.reasoning_text.delta", {"delta": step["reasoning"]})
        for piece in step.get("chunks") or ([step["content"]] if step.get("content") else []):
            send("response.output_text.delta", {"delta": piece})
        output = []
        if step.get("content"):
            output.append({"type": "message", "role": "assistant",
                           "content": [{"type": "output_text", "text": step["content"]}]})
        for idx, call in enumerate(step.get("tool_calls") or []):
            call_id = call.get("id") or "call_%d" % (idx + 1)
            args = call["args"]
            send("response.output_item.added", {"item": {
                "type": "function_call", "id": "fc_" + call_id, "call_id": call_id,
                "name": call["name"], "arguments": ""}})
            for i in range(0, len(args), 24):
                send("response.function_call_arguments.delta", {
                    "item_id": "fc_" + call_id, "delta": args[i:i + 24]})
            output.append({"type": "function_call", "id": "fc_" + call_id, "call_id": call_id,
                           "name": call["name"], "arguments": args})
        send("response.completed", {"response": {"output": output, "status": "completed",
              "usage": {"input_tokens": 100, "output_tokens": 20, "total_tokens": 120}}})
        return out

# ---------------------------------------------------------------- pty 驱动

def drive_keys(fd, inputs):
    for item in inputs:
        os.write(fd, item["data"].encode())
        time.sleep(item.get("wait", 0.6))


def drive(binary, cfg_path, cwd, cols, rows, prompts, timeout, inputs=None):
    pid, fd = pty.fork()
    if pid == 0:
        os.chdir(cwd)
        os.environ["TERM"] = "xterm-256color"
        os.environ.pop("NO_COLOR", None)
        os.execv(binary, [binary, "-n", "-c", cfg_path])
        os._exit(127)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
    buf = bytearray()
    sent = 0
    mark = 0
    deadline = time.time() + timeout
    last_out = time.time()
    if inputs is not None:
        time.sleep(0.7)
        drive_keys(fd, inputs)
        while time.time() < deadline:
            r, _, _ = select.select([fd], [], [], 0.1)
            if not r:
                if time.time() - last_out > 1.5:
                    break
                continue
            try:
                data = os.read(fd, 65536)
            except OSError:
                break
            if not data:
                break
            buf += data
            last_out = time.time()
        try:
            os.write(fd, b"exit\n")
        except OSError:
            pass
        time.sleep(0.4)
        try:
            while True:
                r, _, _ = select.select([fd], [], [], 0.2)
                if not r:
                    break
                if not os.read(fd, 65536):
                    break
        except OSError:
            pass
        try:
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        os.waitpid(pid, 0)
        os.close(fd)
        return buf.decode("utf-8", "replace")
    while time.time() < deadline:
        r, _, _ = select.select([fd], [], [], 0.1)
        if r:
            try:
                data = os.read(fd, 65536)
            except OSError:
                break
            if not data:
                break
            buf += data
            last_out = time.time()
        text = buf.decode("utf-8", "replace")
        quiet = time.time() - last_out
        if sent < len(prompts):
            ready = quiet > 0.35 if sent == 0 else (TURN_SEP in text[mark:] and quiet > 0.3)
            if ready:
                os.write(fd, prompts[sent].encode())
                sent += 1
                mark = len(text)
        elif quiet > 0.6:
            break
    try:
        os.write(fd, b"exit\n")
    except OSError:
        pass
    end = time.time() + 3
    while time.time() < end:
        r, _, _ = select.select([fd], [], [], 0.1)
        if not r:
            continue
        try:
            data = os.read(fd, 65536)
        except OSError:
            break
        if not data:
            break
        buf += data
    try:
        os.kill(pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    os.waitpid(pid, 0)
    os.close(fd)
    return buf.decode("utf-8", "replace")

# ---------------------------------------------------------------- 场景

def sh(command):
    return json.dumps({"command": command}, ensure_ascii=False)


def shi(command):
    return json.dumps({"command": command, "interactive": True}, ensure_ascii=False)


SCENARIOS = [
    {
        "name": "clean-markdown",
        "want": "clean",
        "prompt": "输出一段 markdown：二级标题 + 两项列表 + 一段 go 代码块\n",
        "steps": [{"content": "## 标题\n\n- 第一项\n- 第二项\n\n```go\nfmt.Println(\"hi\")\n```\n"}],
        "screen_has": ["标题", "第一项", "第二项", "fmt.Println"],
    },
    {
        "name": "clean-tool-short",
        "want": "clean",
        "prompt": "用 run_shell 跑一条两条 echo 的命令\n",
        "steps": [
            {"tool_calls": [{"name": "run_shell", "args": sh("echo alpha; echo beta")}]},
            {"content": "命令输出 alpha 与 beta 两行。\n"},
        ],
        "screen_has": ["▸ run_shell", "alpha", "beta", "exit 0"],
    },
    {
        "name": "clean-tool-longlines",
        "want": "clean",
        "prompt": "用 run_shell 跑一条输出很多行的命令\n",
        "steps": [
            {"tool_calls": [{"name": "run_shell", "args": sh("seq 1 30; printf 'L%.0s' $(seq 1 300); echo")}]},
            {"content": "输出被截断显示，状态行给出总行数。\n"},
        ],
        "screen_has": ["▸ run_shell", "共 31 行", "exit 0"],
    },
    {
        "name": "clean-tty-scrollregion",
        "want": "clean",
        "prompt": "用 run_shell 跑一条命令\n",
        "steps": [
            {"tool_calls": [{"name": "run_shell",
                             "args": sh("printf '\\033[20;24r' > /dev/tty; seq 1 12")}]},
            {"content": "命令已执行。\n"},
        ],
        "screen_has": ["▸ run_shell"],
    },
    {
        "name": "clean-tty-modes",
        "want": "clean",
        "prompt": "用 run_shell 跑一条命令\n",
        "steps": [
            {"tool_calls": [{"name": "run_shell", "args": sh("printf '\\033[?7l\\033[?25l' > /dev/tty")}]},
            {"content": "命令已执行。\n"},
        ],
        "screen_has": ["▸ run_shell"],
    },
    {
        "name": "clean-interactive-release",
        "want": "clean",
        "inputs": [
            {"data": "用 run_shell 跑一条需要输入密码的命令（interactive）\n", "wait": 2.5},
            {"data": "s3cretpw\n", "wait": 1.0},
        ],
        "steps": [
            {"tool_calls": [{"name": "run_shell",
                             "args": shi("read -s -p 'Password: ' pw; echo \"len=${#pw}\"")}]},
            {"content": "已收到输入。\n"},
        ],
        "screen_has": ["▸ run_shell", "等待终端输入", "len=8", "exit 0"],
    },
    {
        "name": "leak-picker-unpaged",
        "want": "leak",
        "expect": ["cu_clamped"],
        "sessions": 40,
        "inputs": [
            {"data": "/load\n", "wait": 1.5},
            {"data": "\x1b[B", "wait": 0.5},
            {"data": "\x1b[B", "wait": 0.5},
            {"data": "q", "wait": 1.0},
        ],
        "screen_has": ["选择会话"],
    },
    {
        "name": "note-partial-line",
        "want": "note",
        "prompt": "用 run_shell 跑一条命令\n",
        "steps": [
            {"tool_calls": [{"name": "run_shell", "args": sh("printf 'LEAK-PARTIAL' > /dev/tty")}]},
            {"content": "命令已执行。\n"},
        ],
        "screen_has": ["LEAK-PARTIAL"],
    },
]

DIAGNOSTIC = ("cursor_restore",)

# ---------------------------------------------------------------- 主流程

def replay(text, cols, rows):
    vt = VT(cols, rows)
    vt.feed(text)
    return vt


def seed_sessions(tmp, count):
    sess = os.path.join(tmp, ".tanya", "sessions")
    os.makedirs(sess, exist_ok=True)
    for i in range(count):
        name = "20260901-%06d.jsonl" % (i * 7)
        with open(os.path.join(sess, name), "w", encoding="utf-8") as f:
            f.write(json.dumps({"role": "user", "content": "会话 %d 的测试标题" % i}, ensure_ascii=False) + "\n")
            f.write(json.dumps({"role": "assistant", "content": "ok"}, ensure_ascii=False) + "\n")


def run_case(binary, case, cols, rows, timeout, dump, raw_dir=""):
    steps = [dict(s) for s in case.get("steps", [])]
    llm = MockLLM(steps)
    tmp = tempfile.mkdtemp(prefix="tanya-audit-")
    if case.get("sessions"):
        seed_sessions(tmp, case["sessions"])
    cfg = os.path.join(tmp, "config.yaml")
    with open(cfg, "w", encoding="utf-8") as f:
        f.write("base_url: http://127.0.0.1:%d/v1\napi_key: probe\napi_protocol: responses\n"
                "model: probe\ncolors: auto\nsession_mode: local\n" % llm.port)
    try:
        raw = drive(binary, cfg, tmp, cols, rows, [case.get("prompt", "")], timeout,
                    inputs=case.get("inputs"))
    finally:
        llm.close()
        shutil.rmtree(tmp, ignore_errors=True)
    if raw_dir:
        os.makedirs(raw_dir, exist_ok=True)
        with open(os.path.join(raw_dir, case["name"] + ".raw"), "w", encoding="utf-8") as f:
            f.write(raw)
    vt = replay(raw, cols, rows)
    state = vt.state()
    viol = {k: v for k, v in vt.events.items() if v and k not in DIAGNOSTIC}
    diag = {k: v for k, v in vt.events.items() if v and k in DIAGNOSTIC}
    viol.update({k: v for k, v in state.items() if v})
    screen = vt.dump()
    missing = [s for s in case.get("screen_has", []) if s not in screen]
    if dump:
        print("=" * 20, case["name"], "=" * 20)
        print(vt.dump())
    ok, why = verdict(case, viol, missing, raw)
    return ok, viol, missing, len(raw), why, vt.samples, diag


def verdict(case, viol, missing, raw):
    want = case["want"]
    if missing:
        return False, "屏幕缺少预期文本: %s" % missing
    if want == "clean":
        if viol:
            return False, "违反不变量: %s" % viol
        return True, "干净"
    if want == "leak":
        absent = [k for k in case.get("expect", []) if not viol.get(k)]
        if absent:
            return False, "未复现已知缺口: %s（全部计数 %s）" % (absent, viol or "无")
        return True, "已复现已知缺口: %s" % {k: viol[k] for k in case.get("expect", [])}
    return True, "仅报告: %s" % (viol or "无异常")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--bin", default=os.path.join(REPO, "tanya"))
    ap.add_argument("--only", default="")
    ap.add_argument("--dump", default="")
    ap.add_argument("--cols", type=int, default=100)
    ap.add_argument("--rows", type=int, default=32)
    ap.add_argument("--timeout", type=float, default=40)
    ap.add_argument("--raw", default="", metavar="DIR", help="把每个场景的原始 pty 抓流写入该目录")
    args = ap.parse_args()

    if not os.path.exists(args.bin):
        sys.exit("缺少 %s，先 make build" % args.bin)

    cases = [c for c in SCENARIOS if not args.only or c["name"] == args.only]
    if not cases:
        sys.exit("没有匹配的场景: %s" % args.only)
    fails = []
    print("tanya 输出侧渲染审计  %dx%d  %s" % (args.cols, args.rows, args.bin))
    for case in cases:
        ok, viol, missing, nbytes, why, samples, diag = run_case(
            args.bin, case, args.cols, args.rows, args.timeout, args.dump == case["name"], args.raw)
        tag = {"clean": "clean", "leak": "leak", "note": "note "}[case["want"]]
        print("  %-22s %-5s %s  %s" % (case["name"], tag, "PASS" if ok else "FAIL", why))
        if not ok:
            fails.append(case["name"])
        if viol:
            print("       不变量: %s" % viol)
            for kind, detail, row, col in samples[:3]:
                print("         · %s @row=%d col=%d %s" % (kind, row, col, detail))
        if diag:
            print("       诊断: %s" % diag)
    if fails:
        print("失败: %s" % ", ".join(fails))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
