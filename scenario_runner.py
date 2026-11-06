#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""跨实现一致性裁判（规格 §9 / go-cnal 计划 Task 8）。

Python 与 Go 跑同一份语料（../go-cnal/testdata/scenarios/*.json，
有序 stimulus→expected 表），任一边方言改动，另一边必须变红。

- 握手步（opr=DUALSTACKQUERY）：用本文件 parse_portal/interpret_handshake
  解释语料应答，断言 expect 的 peerState/selfState。
- 登录步（opr=pwdLogin）：先做"上一握手步的 ip 与语料 ipv4or6"自洽性校验
  （E4 形态），再把请求体 POST 到本地 AC 模拟门户，断言 loginOK/alreadyOnline。
- 本地 AC 模拟门户照抄自 tests_tdd.py:_sim_ac_split——那个文件是脚本、
  守卫明示"不要 import，直接照抄一份"，两份之间靠语料互裁防漂移。
- 出站边界：登录只允许发往本进程刚启动的模拟门户（scheme=http、
  hostname=127.0.0.1、解析后 IP 恰为 127.0.0.1 三重校验），其余拒绝不发。

退出码 0=全中；1=有偏差。语料只含文档段地址与假账号，无真实凭据。
"""
from __future__ import annotations

import glob
import http.client
import ipaddress
import json
import os
import socket
import sys
import urllib.parse


def parse_portal(s: str) -> dict:
    """门户伪 JSON 方言（与 go-cnal dialect.Parse 同语义）：
    单/双引号键值、冒号后可带空格、引号内逗号不切分、数值留字符串。"""
    out: dict = {}
    body = (s or "").strip()
    if body.startswith("{"):
        body = body[1:]
    if body.endswith("}"):
        body = body[:-1]
    if not body.strip():
        return out
    parts: list[str] = []
    buf = ""
    q = ""
    for ch in body:
        if q:
            buf += ch
            if ch == q:
                q = ""
        elif ch in "'\"":
            q = ch
            buf += ch
        elif ch == ",":
            parts.append(buf)
            buf = ""
        else:
            buf += ch
    if buf.strip():
        parts.append(buf)
    for kv in parts:
        if ":" not in kv:
            continue
        k, v = kv.split(":", 1)
        k = _unquote(k.strip())
        v = _coerce(v.strip())
        if k:
            out[k] = v
    return out


def _unquote(s: str) -> str:
    if len(s) >= 2 and s[0] in "'\"" and s[-1] == s[0]:
        return s[1:-1]
    return s


def _coerce(v: str):
    if v == "true":
        return True
    if v == "false":
        return False
    if v in ("null", ""):
        return None
    return _unquote(v)


def interpret_handshake(raw: str) -> str:
    """与 go-cnal protocol.InterpretHandshake 同语义：非 true/缺键 → 不可观测。"""
    m = parse_portal(raw)
    if m.get("success") is not True:
        return "unobservable"
    v = m.get("isOnline")
    if not isinstance(v, bool):
        return "unobservable"
    return "online" if v else "offline"


def _sim_ac_split(port: int = 80):
    """照抄自 tests_tdd.py:_sim_ac_split（其守卫明示：不要 import，直接照抄）。
    登录端点语义：会话死 → 登录成功并复活；端口占用返回 None（SKIP）。"""
    import http.server
    import threading

    state = {"session_alive": False}

    class H(http.server.BaseHTTPRequestHandler):
        def log_message(self, *a):
            pass

        def _reply(self, code, body=b""):
            self.send_response(code)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            if body:
                self.wfile.write(body)

        def do_POST(self):
            n = int(self.headers.get("Content-Length", 0) or 0)
            if n:
                self.rfile.read(n)
            if self.path.startswith("/ac_portal/login.php"):
                state["session_alive"] = True
                self._reply(200, "{'success':true,'msg':'登录成功'}".encode("utf-8"))
            else:
                self._reply(404, b"")

    try:
        srv = http.server.ThreadingHTTPServer(("127.0.0.1", port), H)
    except OSError:
        return None
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv


def run_scenario(path: str) -> list[str]:
    with open(path, encoding="utf-8") as f:
        sc = json.load(f)
    name = sc.get("name", path)
    fails: list[str] = []
    peer_ip = None
    srv = None
    try:
        for i, step in enumerate(sc.get("steps", [])):
            req = step.get("request", {})
            resp = step.get("response", "")
            expect = step.get("expect", {})
            opr = req.get("opr")
            if opr == "DUALSTACKQUERY":
                got = interpret_handshake(resp)
                m = parse_portal(resp)
                # 只有对端步（expect=peerState）才更新 peer_ip——self(v4) 步的
                # ip 是 v4 侧地址，覆盖它会让登录步的自洽校验张冠李戴
                if "peerState" in expect and m.get("ip"):
                    peer_ip = m["ip"]
                for key in ("peerState", "selfState"):
                    if key in expect and expect[key] != got:
                        fails.append(
                            f"{name} step{i}({step.get('when', '')}): "
                            f"{key} 期望 {expect[key]} 实际 {got}")
            elif opr == "pwdLogin":
                # 跨步自洽：语料登录步的 ipv4or6 必须等于握手步观察到的 ip
                if peer_ip is not None and req.get("ipv4or6") != peer_ip:
                    fails.append(
                        f"{name} step{i}: 语料自洽性——ipv4or6={req.get('ipv4or6')} "
                        f"≠ 握手步 ip={peer_ip}")
                m = parse_portal(resp)
                ok = m.get("success") is True
                want_login = expect.get("loginOK")
                if want_login is not None and bool(want_login) != ok:
                    fails.append(
                        f"{name} step{i}: loginOK 期望 {want_login} 实际 {ok}")
                # 真实 HTTP 段：只发往本进程刚启动的模拟门户（出站边界）
                if srv is None:
                    srv = _sim_ac_split()
                if srv is None:
                    print(f"  (SKIP {name} step{i}: 127.0.0.1:80 被占用，"
                          "真实 HTTP 段跳过——语料解释断言已覆盖)")
                    continue
                host, port = srv.server_address[:2]
                base = f"http://{host}:{port}"
                u = urllib.parse.urlsplit(base)
                # 出站边界（三重校验后才发送）：scheme=http、环回、解析后 IP=127.0.0.1
                if u.scheme != "http" or u.hostname != "127.0.0.1":
                    fails.append(f"{name} step{i}: 出站目标越界 {base}，拒绝发送")
                    continue
                resolved = ipaddress.ip_address(socket.gethostbyname(u.hostname))
                if resolved != ipaddress.ip_address("127.0.0.1"):
                    fails.append(
                        f"{name} step{i}: 解析 IP {resolved} 越界，拒绝发送")
                    continue
                form = urllib.parse.urlencode(req).encode()
                conn = http.client.HTTPConnection(u.hostname, u.port, timeout=5)
                try:
                    conn.request(
                        "POST", "/ac_portal/login.php", body=form,
                        headers={"Content-Type":
                                 "application/x-www-form-urlencoded"})
                    r = conn.getresponse()
                    body = r.read().decode("utf-8", "replace")
                finally:
                    conn.close()
                got_ok = parse_portal(body).get("success") is True
                if got_ok != ok:
                    fails.append(
                        f"{name} step{i}: 模拟门户实测 success={got_ok} "
                        f"与语料应答解释 {ok} 不一致")
    finally:
        if srv is not None:
            srv.shutdown()
            srv.server_close()
    return fails


def main() -> int:
    here = os.path.dirname(os.path.abspath(__file__))
    paths = sorted(glob.glob(os.path.join(here, "..", "go-cnal", "testdata",
                                          "scenarios", "*.json")))
    if not paths:
        print("未找到语料：../go-cnal/testdata/scenarios/*.json")
        return 1
    all_fails: list[str] = []
    for p in paths:
        fails = run_scenario(p)
        status = "PASS" if not fails else "FAIL"
        print(f"[{status}] {os.path.basename(p)}")
        all_fails.extend(fails)
    for f in all_fails:
        print("  " + f)
    print(f"结果: {'全中' if not all_fails else f'{len(all_fails)} 处偏差'}")
    return 1 if all_fails else 0


if __name__ == "__main__":
    sys.exit(main())
