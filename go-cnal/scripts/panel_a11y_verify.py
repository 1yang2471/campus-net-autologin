# 面板无障碍验收（Android M1，真机之前）：真 Chrome 在 375px + 粗指针下
# 逐个回读可交互控件的 computed min-height，断言全部 ≥44px，并点一遍主要
# 流程；控制台一有 error 即失败，每步截图落 docs/screenshots/panel-a11y-*.png。
#
# 用法：python -I scripts\panel_a11y_verify.py
# 前置：本机 Chrome（channel="chrome"）；面板页由 Go 侧生成到
#       docs/screenshots/panel-a11y.html（panelPage() 的唯一出处）。
#
# 为什么起本地 HTTP：面板 JS 用相对路径 fetch("/config")，在 about:blank
# 下会被 Chromium 直接丢弃，路由拦不到。真机 WebView 也是 http://127.0.0.1，
# 用同源 HTTP 伺服才能测到真实的懒加载/空态/错误态。
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

# 回路端点的最小合法应答（真机验收才连真内核）。
BODIES = {
    "/config": {
        "username": "alice", "server": "https://portal.example",
        "free_windows": "23:00-07:00", "auto_reconnect": "true",
        "bind_ip": "", "login_form": "",
    },
    "/accounts": [{"username": "alice", "server": "https://portal.example", "current": True}],
    "/state": {"state": "online", "detail": "在线", "label": "在线",
               "color": "#4f9d63", "v4_src": "10.20.30.40", "v6_src": "",
               "flux": {"used": "2.4G", "quota": "4G", "percent": 60,
                        "has_percent": True, "level": "ok"}},
    "/logs": [],
    "/flux": {},
    "/login": {"ok": True},
    "/rebindv6": {"ok": True},
}

# 面板里所有"用户会点/会输入"的元素。CSS 选择器覆盖到每一个，漏一个就是
# 触屏上有一个 28px 的小靶子（intentional-design 绝对标准 ≥44×44）。
INTERACTIVE = [
    "#tab-status", "#tab-settings",
    "#login", "#acct-shortcut", "#flux-btn", "#rebind-btn", "#log-btn",
    "#log-refresh", "#switch", "#save", "#free-add",
    ".free-row .free-del", ".free-row input[type=time]",
    "#f-account", "#f-user", "#f-pass", "#f-server", "#f-bindip",
    "#f-auto", "#f-campus-dns", "#f-interval", "#f-probe-timeout",
    "#f-login-timeout", "#f-probe-ips", "#f-login-page", "#f-user-agent",
    "#f-portal-down-wait", "#f-reject-backoff", "#f-transient-backoff",
    "#f-ssl-verify", "#f-success-marks", "#f-negative-marks",
    "#f-transient-marks", "#f-freeze-marks", "#f-login-form",
    "#adv-toggle", "#login-form-adv summary",
]

passed, failed = [], []


def check(name, ok, detail=""):
    (passed if ok else failed).append(name)
    print(("PASS " if ok else "FAIL ") + name + ((" — " + detail) if detail else ""))


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        path = self.path.split("?")[0]
        if path in ("/", "/panel"):
            body = PAGE_FILE.read_bytes()
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Cache-Control", "no-store")
        elif path in BODIES:
            body = json.dumps(BODIES[path]).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json; charset=utf-8")
        else:
            body = b"{}"
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
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
    t = threading.Thread(target=srv.serve_forever, daemon=True)
    t.start()
    url = f"http://127.0.0.1:{port}/panel?token={'a' * 32}"

    try:
        with sync_playwright() as p:
            b = p.chromium.launch(channel="chrome")
            # has_touch 让 (pointer: coarse) 命中，与安卓 WebView 同档。
            pg = b.new_page(viewport={"width": 375, "height": 812},
                            has_touch=True, is_mobile=True)
            errs = []
            pg.on("console", lambda m: errs.append(m.type + ":" + m.text) if m.type == "error" else None)
            pg.on("pageerror", lambda e: errs.append("pageerror:" + str(e)))
            resp = pg.goto(url, wait_until="load", timeout=20000)
            pg.wait_for_timeout(400)
            check("面板文档 200", resp.status == 200, str(resp.status))

            # 打开设置页（懒加载 /config）与日志卡，让隐藏控件进入布局。
            pg.click("#tab-settings")
            pg.wait_for_timeout(400)
            pg.evaluate("document.getElementById('log-card').hidden = false")
            pg.wait_for_timeout(100)

            # 先量后截：full_page 截图会临时扩展视口并重置 pointer 模拟。
            results = pg.evaluate("""(sels) => sels.map(sel => {
                const els = [...document.querySelectorAll(sel)];
                return {sel, count: els.length,
                        heights: els.map(e => getComputedStyle(e).minHeight)};
            })""", INTERACTIVE)
            under, missing = [], []
            for r in results:
                if r["count"] == 0:
                    missing.append(r["sel"])
                    continue
                for h in r["heights"]:
                    v = float(h.replace("px", "")) if h.endswith("px") else 0.0
                    if v < 44:
                        under.append(f'{r["sel"]}={h}')
            check("所有交互控件粗指针下 min-height ≥44px",
                  not under and not missing,
                  ("不足: " + ", ".join(under) + "; ") if under else "" +
                  ("缺失: " + ", ".join(missing) if missing else ""))

            # 窄屏真实数据：免认证时段行的「删除」按钮不得被挤成竖排两行。
            # 375px 视口下卡片内宽 327px，两个固定 108px 的 time 输入会把
            # 按钮压到 ~40px，标签碎成「删/除」——最小高度达标但文字不可用。
            delBtn = pg.evaluate("""() => {
                const b = document.querySelector('.free-row .free-del');
                if (!b) return null;
                const s = getComputedStyle(b);
                const r = b.getBoundingClientRect();
                // 用单字宽度判断：换行时文字高度 ~2 行，按钮仍只有一行高。
                return {width: r.width, height: r.height,
                        whiteSpace: s.whiteSpace, scrollH: b.scrollHeight,
                        clientH: b.clientHeight};
            }""")
            check("免认证时段「删除」按钮标签不换行",
                  delBtn is not None and delBtn["whiteSpace"] == "nowrap" and
                  delBtn["width"] >= 44 and delBtn["height"] >= 44,
                  str(delBtn))
            # full_page 截图会临时扩展视口并重置 pointer 模拟，必须放在所有
            # computed 测量之后（此前曾因此把 44px 误读成 28px）。
            pg.screenshot(path=str(SHOTS / "panel-a11y-settings.png"), full_page=True)

            # 禁用态：#free-add 在 /config 读不到时会 disabled；这里手动置一
            # 个已知的禁用按钮，回读 hover 前后边框色与 cursor。悬停不得给出
            # "可点" 暗示，cursor 也不得是 pointer。
            disabled = pg.evaluate("""() => {
                const b = document.getElementById('free-add');
                b.disabled = true;
                const base = getComputedStyle(b);
                const before = {border: base.borderColor, cursor: base.cursor};
                return before;
            }""")
            pg.hover("#free-add")
            hovered = pg.evaluate("""() => {
                const s = getComputedStyle(document.getElementById('free-add'));
                return {border: s.borderColor, cursor: s.cursor};
            }""")
            check("禁用按钮 hover 不给出可点暗示",
                  hovered["border"] == disabled["border"] and hovered["cursor"] != "pointer",
                  f'base={disabled} hover={hovered}')

            # 真实数据与空态：账号名塞超长串、日志端点返回空数组，点一遍
            # 主要流程，断言没有横向溢出、空态不是一片空白、控制台无 error。
            pg.evaluate("""() => {
                document.getElementById('f-user').value =
                    'alice'.repeat(40) + '@very-long-campus-domain.example.edu.cn';
                document.getElementById('f-server').value =
                    'https://' + 'portal'.repeat(30) + '.example.edu.cn/ac_portal/login.php';
            }""")
            pg.click("#tab-status")
            # 首屏「切账号」捷径：状态页直接跳设置页并聚焦历史账号选择器。
            pg.click("#acct-shortcut")
            pg.wait_for_timeout(100)
            shortcut = pg.evaluate("""() => ({
                settingsVisible: !document.getElementById('view-settings').hidden,
                focused: document.activeElement && document.activeElement.id,
            })""")
            check("首屏切账号捷径跳到设置并聚焦账号选择器",
                  shortcut["settingsVisible"] and shortcut["focused"] == "f-account",
                  str(shortcut))
            pg.click("#tab-status")
            pg.click("#flux-btn")
            pg.wait_for_timeout(200)
            pg.click("#log-btn")
            pg.wait_for_timeout(200)
            pg.click("#log-refresh")
            pg.wait_for_timeout(200)
            flow = pg.evaluate("""() => ({
                overflow: document.documentElement.scrollWidth > window.innerWidth + 1,
                logEmpty: document.getElementById('log-view').textContent.trim().length,
                logPlaceholder: document.getElementById('log-view').textContent.includes('暂无') ||
                                document.getElementById('log-view').textContent.length > 0,
                flux: document.getElementById('flux').textContent.trim(),
                fluxCard: document.getElementById('flux-percent').textContent.trim(),
                fluxBar: document.getElementById('flux-bar').style.width,
            })""")
            check("长串/空数据下无横向溢出", not flow["overflow"], str(flow))
            check("空日志不是无提示的空白", flow["logPlaceholder"], str(flow))
            check("流量卡片从 /state 渲染百分比", flow["fluxCard"] == "60%", str(flow))
            check("流量进度条宽度按百分比", flow["fluxBar"] == "60%", str(flow))
            pg.screenshot(path=str(SHOTS / "panel-a11y-flow.png"), full_page=True)

            # 绝对标准：键盘可达控件有可见 focus 指示器。必须用真实键盘 Tab
            # 触发 :focus-visible（脚本 .focus() 不命中该伪类，会假报红）。
            pg.evaluate("document.activeElement.blur()")
            focus = None
            for _ in range(6):
                pg.keyboard.press("Tab")
                f = pg.evaluate("""() => {
                    const e = document.activeElement;
                    const s = getComputedStyle(e);
                    return {tag: e.tagName, id: e.id,
                            outline: s.outlineStyle, width: s.outlineWidth};
                }""")
                if f["tag"] == "BUTTON":
                    focus = f
                    break
            check("键盘 Tab 后 focus 有可见指示器",
                  focus is not None and focus["outline"] != "none" and focus["width"] != "0px",
                  str(focus))

            pg.emulate_media(reduced_motion="reduce")
            motion = pg.evaluate("""() => ({
                reduced: matchMedia('(prefers-reduced-motion: reduce)').matches,
                transitions: [...document.querySelectorAll('*')]
                    .filter(e => getComputedStyle(e).transitionDuration !== '0s').length,
            })""")
            check("reduced-motion 下无残留过渡", motion["reduced"] and motion["transitions"] == 0,
                  str(motion))

            # 三档宽度：布局重组而不是等比缩小；任何一档都不许横向溢出。
            widths = {}
            for w in (375, 768, 1280):
                pg.set_viewport_size({"width": w, "height": 900})
                pg.wait_for_timeout(80)
                widths[w] = pg.evaluate("""() => ({
                    overflow: document.documentElement.scrollWidth > window.innerWidth + 1,
                    card: document.querySelector('.card').getBoundingClientRect().width,
                })""")
            check("375/768/1280 三档均无横向溢出且卡片收在 480px 内",
                  all(not v["overflow"] and v["card"] <= 481 for v in widths.values()),
                  str(widths))

            check("真浏览器控制台无 error", not errs, "; ".join(errs[:3]))
            b.close()
    finally:
        srv.shutdown()

    print(f"\n{len(passed)} passed, {len(failed)} failed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
