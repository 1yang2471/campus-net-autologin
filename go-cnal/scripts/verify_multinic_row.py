# 验收截图：面板「其他网卡」行（2026-11-19 多网卡观察）。
# 真浏览器打开当前运行实例的面板，断言该行按实际网卡状态渲染，并截图。
# 两种合法结果：有旁路卡未认证 → 列出 IP + 黄叹号说明；无旁路卡 → 占位「—」。
# 断言「与 /state 的 unauth_sources 一致」，不写死某一种环境。
import json
import os
import sys

from playwright.sync_api import sync_playwright

api = json.load(open(os.path.expandvars(
    r"%LOCALAPPDATA%\campus-net-autologin\api.json"), encoding="utf-8"))
url = api["url"] + "/panel?token=" + api["token"]
out = os.path.join("docs", "screenshots", "2026-11-19-multinic-row.png")
os.makedirs(os.path.dirname(out), exist_ok=True)

with sync_playwright() as p:
    b = p.chromium.launch(channel="chrome")
    pg = b.new_page(viewport={"width": 420, "height": 900})
    errs = []
    pg.on("console", lambda m: errs.append(m.text) if m.type == "error" else None)
    pg.on("pageerror", lambda e: errs.append(str(e)))
    resp = pg.goto(url, wait_until="load", timeout=20000)
    pg.wait_for_timeout(3000)  # 等 /state 轮询把行填上
    txt = pg.locator("#nics").text_content().strip()
    state = pg.evaluate(
        "fetch('/state',{headers:{Authorization:'Bearer '+TOKEN}}).then(r=>r.json())")
    listed = state.get("unauth_sources") or []
    print("面板行文本:", txt)
    print("/state unauth_sources:", listed)
    pg.screenshot(path=out, full_page=True)
    print("截图:", out)
    if listed:
        # 有未认证源：行里必须逐个列出，并带黄叹号说明
        expect = all(ip in txt for ip in listed) and "未认证" in txt and "无 Internet" in txt
    else:
        # 无未认证源：占位「—」，不得编造地址
        expect = txt == "—"
    ok = resp.status == 200 and expect and not errs
    print("PASS" if ok else "FAIL", "console:", errs)
    b.close()
    sys.exit(0 if ok else 1)
