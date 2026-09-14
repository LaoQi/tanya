#!/usr/bin/env python3
import argparse
import json
import os
import re
import sys
import urllib.error
import urllib.request

UA = "pi/0.85.0 (linux; node/v22.14.0; x64)"
TOOLS = [{"type": "function", "function": {"name": "get_weather",
          "description": "Get weather of a location",
          "parameters": {"type": "object", "properties": {"location": {"type": "string"}},
                         "required": ["location"]}}}]


def load_endpoint(path):
    base = os.environ.get("TANYA_BASE_URL", "")
    key = os.environ.get("TANYA_API_KEY", "")
    model = os.environ.get("TANYA_MODEL", "")
    for candidate in [path, os.path.expanduser("~/.config/tanyan/config.yaml"), "config.yaml"]:
        if not candidate or not os.path.exists(candidate):
            continue
        text = open(candidate, encoding="utf-8").read()
        if not base:
            m = re.search(r"^base_url:\s*\"?([^\"\n]+)\"?", text, re.M)
            if m:
                base = m.group(1).strip()
        if not key:
            m = re.search(r"^api_key:\s*\"?([^\"\n]+)\"?", text, re.M)
            if m:
                key = m.group(1).strip()
        if not model:
            m = re.search(r"^model:\s*\"?([^\"\n]+)\"?", text, re.M)
            if m:
                model = m.group(1).strip()
    if not base:
        sys.exit("base_url 未找到（-c/--config 或 TANYA_BASE_URL）")
    if not key:
        sys.exit("api_key 未找到（-c/--config 或 TANYA_API_KEY）")
    return base.rstrip("/"), key, model


class Probe:
    def __init__(self, base, key, model, effort, max_tokens, timeout):
        self.base = base + "/chat/completions"
        self.key = key
        self.model = model
        self.effort = effort
        self.max_tokens = max_tokens
        self.timeout = timeout

    def post(self, messages, tools=True):
        body = {"model": self.model, "messages": messages, "stream": False, "max_tokens": self.max_tokens}
        if tools:
            body["tools"] = TOOLS
        if self.effort:
            body["reasoning_effort"] = self.effort
        req = urllib.request.Request(self.base, data=json.dumps(body).encode(),
                                     headers={"Authorization": "Bearer " + self.key, "User-Agent": UA,
                                              "Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as r:
                return r.status, json.load(r)
        except urllib.error.HTTPError as e:
            raw = e.read().decode("utf-8", "replace")
            try:
                return e.code, json.loads(raw)
            except Exception:
                return e.code, {"raw": raw[:400]}
        except Exception as e:
            return 0, {"raw": str(e)}


def report(tag, status, resp):
    if status != 200:
        err = resp.get("error") if isinstance(resp, dict) else None
        detail = err.get("message") if isinstance(err, dict) else resp.get("raw")
        print(f"  {tag:<28} HTTP {status}  {str(detail)[:180]}")
        return None
    msg = resp["choices"][0]["message"]
    u = resp.get("usage") or {}
    reason = msg.get("reasoning_content") or ""
    calls = msg.get("tool_calls") or []
    print(f"  {tag:<28} HTTP 200  model={resp.get('model')} reasoning={len(reason)}字 "
          f"prompt={u.get('prompt_tokens')} hit={u.get('prompt_cache_hit_tokens')} "
          f"cache_detail={(u.get('prompt_tokens_details') or {}).get('cached_tokens')} "
          f"tool_calls={[c['function']['name'] for c in calls]} "
          f"reason_tokens={(u.get('completion_tokens_details') or {}).get('reasoning_tokens')}")
    return msg


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("-c", "--config", default="config.yaml")
    ap.add_argument("-m", "--model", default="")
    ap.add_argument("--effort", default="low")
    ap.add_argument("--max-tokens", type=int, default=512)
    ap.add_argument("--timeout", type=int, default=120)
    args = ap.parse_args()

    base, key, model = load_endpoint(args.config)
    model = args.model or model
    p = Probe(base, key, model, args.effort, args.max_tokens, args.timeout)
    print(f"endpoint={base} model={model} effort={args.effort}")

    user = {"role": "user", "content": "杭州现在天气如何？请调用 get_weather 工具查询。"}
    print("[1] 首轮（tools + reasoning 产出）")
    status, resp = p.post([user])
    first = report("turn1", status, resp)
    if first is None:
        return
    reason = first.get("reasoning_content") or ""
    calls = first.get("tool_calls") or []
    base_assistant = {"role": "assistant", "content": first.get("content")}
    if calls:
        base_assistant["tool_calls"] = calls
        base_assistant["reasoning_content"] = reason
        tail = [{"role": "tool", "tool_call_id": calls[0]["id"], "content": "杭州 晴 24℃ 湿度 60%"}]
        print(f"  已获得 tool_calls: {[c['function']['name'] for c in calls]}")
    else:
        base_assistant["reasoning_content"] = reason
        tail = [{"role": "user", "content": "据此给出天气结论。"}]
        print("  未获得 tool_calls，退化为纯文本轮次")

    print("[2] 回传完整 reasoning_content")
    report("replay_full", *p.post([user, base_assistant] + tail))

    nors = {k: v for k, v in base_assistant.items() if k != "reasoning_content"}
    print("[3] 省略 reasoning_content")
    report("replay_absent", *p.post([user, nors] + tail))

    empty = dict(nors, reasoning_content="")
    print("[4] reasoning_content 为空字符串")
    report("replay_empty", *p.post([user, empty] + tail))


if __name__ == "__main__":
    main()
