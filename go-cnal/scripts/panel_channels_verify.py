# 通道行浏览器验收（2026-11-20 双通道 M1）：真 Chrome 里把「通道」行点一遍，
# 控制台一有 error 即失败，每步截图落 docs/screenshots/channels-*.png。
#
# 用法：python -I scripts\panel_channels_verify.py
# 前置：本机 Chrome（channel="chrome"）+ playwright；面板页由
#       `go test ./cmd/cnalw/` 导出到 docs/screenshots/panel-a11y.html。
#
# 为什么起本地 HTTP：面板 JS 用相对路径 fetch("/state")，about:blank 下会被
# Chromium 丢弃。用同源 HTTP 才能测到真实渲染。
import http.server
import json
import socketserver
import sys
import threading
from pathlib import Path

from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parent.parent
PAGE_FILE = ROOT / "docs" / "screenshots" / "panel-a11y.html"
SHOTS = ROOT / "docs" / "screenshots"

BASE = {
    "state": "online", "detail": "在线", "label": "在线",
    "color": "#4f9d63", "v4_src": "10.20.30.40", "v6_src": "",
    "flux": {"used": "2.4G", "quota": "4G", "percent": 60,
             "has_percent": True, "level": "ok"},
}
# 两条通道：一张已认证、一张已连接未认证——面板必须逐条说出「哪张卡 + 什么态」。
WITH_CHANNELS = dict(BASE, channels=[
    {"id": "以太网|10.141.53.34", "iface": "以太网", "ip": "10.141.53.34",
     "state": "unauth", "label": "已连接未认证"},
    {"id": "WLAN 2|172.29.153.208", "iface": "WLAN 2", "ip": "172.29.153.208",
     "state": "authed", "label": "已认证"},
])
# 旧内核：没有 channels 字段，只有 2026-11-19 的 unauth_sources。
LEGACY = dict(BASE, unauth_sources=["10.141.53.34"])

BODIES = {
    "/config": {"username": "alice", "server": "https://portal.example",
                "free_windows": "", "auto_reconnect": "true",
                "bind_ip": "", "login_form": ""},
    "/accounts": [{"username": "alice", "server": "https://portal.example", "current": True}],
    "/state": WITH_CHANNELS,
    "/logs": [], "/flux": {}, "/login": {"ok": True}, "/rebindv6": {"ok": True},
}

passed, failed = [], []


def check(name, ok, detail=""):
    (passed if ok else failed).append(name)
    print(("PASS " if ok else "FAIL ") + name + ((" — " + detail) if detail else ""))


class Handler(http.server.BaseHTTPRequestHandler):
    state_body = WITH_CHANNELS

    def log_message(self, *args):
        pass

    def do_GET(self):
        path = self.path.split("?")[0]
        if path in ("/", "/panel"):
            body = PAGE_FILE.read_bytes()
            ctype = "text/html; charset=utf-8"
        elif path == "/state":
            body = json.dumps(self.state_body).encode()
            ctype = "application/json; charset=utf-8"
        elif path in BODIES:
            body = json.dumps(BODIES[path]).encode()
            ctype = "application/json; charset=utf-8"
        else:
            body = b"{}"
            ctype = "application/json"
        self.send_response(200)
        self.send_header("Content-Type", ctype)
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        self.do_GET()


def main():
    if not PAGE_FILE.is_file():
        print("FAIL 面板页未生成：" + str(PAGE_FILE))
        return 1
    SHOTS.mkdir(parents=True, exist_ok=True)

    class Server(socketserver.ThreadingTCPServer):
        allow_reuse_address = True
        daemon_threads = True

    srv = Server(("127.0.0.1", 0), Handler)
    port = srv.server_address[1]
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    url = f"http://127.0.0.1:{port}/panel?token={'a' * 32}"

    try:
        with sync_playwright() as p:
            b = p.chromium.launch(channel="chrome")
            pg = b.new_page(viewport={"width": 420, "height": 720})
            errs = []
            pg.on("console", lambda m: errs.append(m.type + ":" + m.text) if m.type == "error" else None)
            pg.on("pageerror", lambda e: errs.append("pageerror:" + str(e)))
            pg.goto(url, wait_until="load", timeout=20000)
            pg.wait_for_timeout(600)

            text = pg.inner_text("#channels")
            check("通道行显示两条通道", "以太网" in text and "WLAN 2" in text, text)
            check("通道行显示各自认证态", "已连接未认证" in text and "已认证" in text, text)
            check("通道行显示源 IP", "10.141.53.34" in text and "172.29.153.208" in text, text)
            pg.screenshot(path=str(SHOTS / "channels-list.png"), full_page=True)

            # 旧内核兜底：只有 unauth_sources 时不得整行留空。
            Handler.state_body = LEGACY
            pg.reload(wait_until="load")
            pg.wait_for_timeout(600)
            legacy = pg.inner_text("#channels")
            check("旧内核回落到未认证源", "10.141.53.34" in legacy, legacy)
            pg.screenshot(path=str(SHOTS / "channels-legacy.png"), full_page=True)

            check("控制台无错误", not errs, "; ".join(errs[:3]))
            b.close()
    finally:
        srv.shutdown()

    print("\n通过 %d / 失败 %d" % (len(passed), len(failed)))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
