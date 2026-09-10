#!/usr/bin/env python3
import argparse
import json
import os
import random
import re
import sys
import time
import urllib.error
import urllib.request

UA = "pi/0.85.0 (linux; node/v22.14.0; x64)"
WORDS = ["alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india",
         "juliet", "kilo", "lima", "mike", "november", "oscar", "papa", "quebec", "romeo",
         "sierra", "tango"]


def load_endpoint(path):
    base = os.environ.get("TANYA_BASE_URL", "")
    key = os.environ.get("TANYA_API_KEY", "")
    for candidate in [path, os.path.expanduser("~/.config/tanyan/config.yaml"), "config.yaml"]:
        if base and key:
            break
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
    if not base:
        sys.exit("base_url 未找到（-c/--config 或 TANYA_BASE_URL）")
    if not key:
        sys.exit("api_key 未找到（-c/--config 或 TANYA_API_KEY）")
    return base.rstrip("/"), key


def unique_text(n, tag, rnd):
    return "\n".join(f"{tag}-{i:04d} " + " ".join(rnd.choice(WORDS) for _ in range(6)) for i in range(n))


def tool(name, desc):
    return {"type": "function", "function": {"name": name, "description": desc,
            "parameters": {"type": "object", "properties": {"x": {"type": "string"}}, "required": []}}}


class Probe:
    def __init__(self, base, key, gap, timeout):
        self.base, self.key, self.gap, self.timeout = base, key, gap, timeout

    def post(self, body):
        for attempt in range(3):
            try:
                req = urllib.request.Request(self.base + "/chat/completions", data=json.dumps(body).encode(),
                                             headers={"Authorization": "Bearer " + self.key, "User-Agent": UA,
                                                      "Content-Type": "application/json"})
                r = json.load(urllib.request.urlopen(req, timeout=self.timeout))
                u = r["usage"]
                hit = u.get("prompt_cache_hit_tokens")
                if hit is None:
                    hit = (u.get("prompt_tokens_details") or {}).get("cached_tokens")
                return u.get("prompt_tokens", 0), hit
            except urllib.error.HTTPError as e:
                print(f"    HTTP {e.code}: {e.read()[:160].decode('utf-8', 'replace')}")
                return None, None
            except Exception as e:
                print(f"    retry {attempt}: {e}")
                time.sleep(4)
        return None, None

    def send(self, label, model, messages, tools=None, temperature=None, gap=True):
        if gap and self.gap:
            time.sleep(self.gap)
        body = {"model": model, "messages": messages, "max_tokens": 8, "stream": False}
        if tools:
            body["tools"] = tools
        if temperature is not None:
            body["temperature"] = temperature
        pt, hit = self.post(body)
        align = "" if (hit is None or hit % 64 == 0) else f"  [非 64 对齐!]"
        print(f"  {label:34s} prompt={pt} hit={hit}{align}")
        return pt, hit


def base_scene(seed, tag):
    rnd = random.Random(seed)
    sys_text = unique_text(320, tag, rnd)
    d_a, d_b = unique_text(20, "dA", rnd), unique_text(20, "dB", rnd)
    t_a, t_b = tool("tool_alpha", d_a), tool("tool_bravo", d_b)
    return {"rnd": rnd, "sys": sys_text, "user": "只回复两个字：收到", "tools": [t_a, t_b],
            "d_tools": [tool("tool_alpha", d_a.replace("dA-0010 ", "dA-0010-ZZ ", 1)), t_b],
            "d_last": [t_a, tool("tool_bravo", d_b.replace("dB-0010 ", "dB-0010-ZZ ", 1))],
            "sys_mid": sys_text.replace(f"{tag}-0160 ", f"{tag}-0160-XX ", 1)}


def msgs(sys_text, user, extra=None):
    out = [{"role": "system", "content": sys_text}]
    if extra:
        out.extend(extra)
    out.append({"role": "user", "content": user})
    return out


def group_stability(p, model, seed, ttl):
    print(f"### [stability] {model}: 自命中重复 + 写入延迟 + temperature")
    sc = base_scene(seed, "stab")
    for i in range(1, 4):
        p.send(f"X0 rep{i}", model, msgs(sc["sys"], sc["user"]), sc["tools"])
    for t in (0, 1, 3):
        p.send(f"X0 建立后 +{t}s", model, msgs(sc["sys"], sc["user"]), sc["tools"], gap=t if t else False)
    for temp in (None, 1.0, 0.0):
        p.send(f"temperature={temp}", model, msgs(sc["sys"], sc["user"]), sc["tools"], temperature=temp)
    if ttl:
        print(f"  ... 等待 {ttl}s 测 TTL")
        time.sleep(ttl)
        p.send(f"{ttl}s 后复测", model, msgs(sc["sys"], sc["user"]), sc["tools"])


def group_tools(p, model, seed):
    print(f"### [tools] {model}: tools 段位置阶梯（X0 自命中 → tools[0]/tools[last] 改一字 → 无 tools → system 中部改字）")
    sc = base_scene(seed, "tools")
    p.send("X0 建立", model, msgs(sc["sys"], sc["user"]), sc["tools"])
    p.send("X0 自命中", model, msgs(sc["sys"], sc["user"]), sc["tools"])
    p.send("改 tools[0].description", model, msgs(sc["sys"], sc["user"]), sc["d_tools"])
    p.send("改 tools[last].description", model, msgs(sc["sys"], sc["user"]), sc["d_last"])
    p.send("无 tools", model, msgs(sc["sys"], sc["user"]), None)
    p.send("改 system 中部一字", model, msgs(sc["sys_mid"], sc["user"]), sc["tools"])


def group_turns(p, model, seed, turns):
    print(f"### [turns] {model}: 多轮 append-only 历史（命中应≈上一轮 prompt）")
    rnd = random.Random(seed)
    sys_text = unique_text(200, "turns", rnd)
    tools = [tool("tool_alpha", unique_text(20, "dA", rnd)), tool("tool_bravo", unique_text(20, "dB", rnd))]
    history, prev = [], None
    for t in range(1, turns + 1):
        if t > 1:
            history.append({"role": "assistant", "content": unique_text(40, f"a{t-1}", rnd)})
        history.append({"role": "user", "content": unique_text(40, f"q{t}", rnd)})
        pt, hit = p.send(f"轮{t}", model, msgs(sys_text, "", history), tools)
        if prev and pt is not None:
            print(f"      → 上一轮 prompt={prev}，未命中 {pt - (hit or 0)}")
        prev = pt


def group_models(p, models, seed):
    print("### [models] 跨模型：同内容 prompt_tokens 比对 + tools 阶梯")
    rnd = random.Random(seed)
    sys_text = unique_text(320, "route", rnd)
    for m in models:
        pt, hit = p.send(f"{m} 同内容", m, msgs(sys_text, "只回复两个字：收到"), None)
    for m in models:
        sc = base_scene(seed + 1, "m")
        print(f"  -- {m}")
        p.send("X0 建立", m, msgs(sc["sys"], sc["user"]), sc["tools"])
        p.send("改 tools[0]", m, msgs(sc["sys"], sc["user"]), sc["d_tools"])
        p.send("改 tools[last]", m, msgs(sc["sys"], sc["user"]), sc["d_last"])
        p.send("无 tools", m, msgs(sc["sys"], sc["user"]), None)


def group_preheat(p, model, seed, repeats):
    print(f"### [preheat] {model}: 同一变体连发 {repeats} 次（看写入滞后）")
    sc = base_scene(seed, "preheat")
    for label, tools in (("X0", sc["tools"]), ("改 tools[0]", sc["d_tools"])):
        for i in range(1, repeats + 1):
            p.send(f"{label} #{i}", model, msgs(sc["sys"], sc["user"]), tools)


def main():
    ap = argparse.ArgumentParser(description="tanyan 缓存机制探测（64-token 块前缀缓存）")
    ap.add_argument("-c", "--config", default="", help="配置文件路径（读 base_url/api_key）")
    ap.add_argument("-m", "--model", default=os.environ.get("TANYA_MODEL", "deepseek-flash"))
    ap.add_argument("--models", default="", help="[models] 组用的逗号分隔模型列表")
    ap.add_argument("--group", default="stability,tools,turns",
                    help="stability,tools,turns,models,preheat,all")
    ap.add_argument("--seed", type=int, default=20260101)
    ap.add_argument("--gap", type=float, default=3.0, help="请求间隔秒（0 关闭）")
    ap.add_argument("--ttl", type=int, default=0, help="[stability] 等待 N 秒复测 TTL（0 跳过）")
    ap.add_argument("--turns", type=int, default=5)
    ap.add_argument("--repeats", type=int, default=5)
    ap.add_argument("--timeout", type=int, default=180)
    args = ap.parse_args()

    base, key = load_endpoint(args.config)
    print(f"endpoint={base} model={args.model}")
    p = Probe(base, key, args.gap, args.timeout)
    groups = ["stability", "tools", "turns", "models", "preheat"] if args.group == "all" \
        else [g.strip() for g in args.group.split(",") if g.strip()]
    for g in groups:
        if g == "stability":
            group_stability(p, args.model, args.seed, args.ttl)
        elif g == "tools":
            group_tools(p, args.model, args.seed)
        elif g == "turns":
            group_turns(p, args.model, args.seed, args.turns)
        elif g == "models":
            group_models(p, [m.strip() for m in args.models.split(",") if m.strip()] or [args.model], args.seed)
        elif g == "preheat":
            group_preheat(p, args.model, args.seed, args.repeats)
        else:
            sys.exit(f"未知组: {g}")


if __name__ == "__main__":
    main()
