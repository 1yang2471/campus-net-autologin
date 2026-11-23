# prove-it 发布验收：真浏览器（本机 Chrome）走一遍发布 exe 的用户可见流程。
# 控制台一有 error 即失败；每一步截图落 docs/screenshots/release-*.png。
#
# 用法：python -I scripts\release_verify.py
# 前置：先跑 scripts\build.ps1 生成 dist zip，或已装到 %LOCALAPPDATA%\Programs\cnal
import os
import re
import configparser
import subprocess
import sys
import time

from playwright.sync_api import sync_playwright

CFG = os.path.expandvars(r"%LOCALAPPDATA%\campus-net-autologin\config.ini")
EXE = os.path.expandvars(r"%LOCALAPPDATA%\Programs\cnal\cnalw.exe")
SHOTS = os.path.join("docs", "screenshots")

passed, failed = [], []


def check(name, ok, detail=""):
    (passed if ok else failed).append(name)
    print(("PASS " if ok else "FAIL ") + name + ((" — " + detail) if detail else ""))


def main():
    os.makedirs(SHOTS, exist_ok=True)
    # 参数数组调用（shell=False），不拼接任何字符串，杜绝命令注入。
    subprocess.run(["taskkill", "/F", "/IM", "cnalw.exe"], capture_output=True, check=False)
    time.sleep(1)
    proc = subprocess.Popen([EXE, "--config", CFG, "--print-panel-url"],
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    try:
        buf, url, t0 = "", None, time.time()
        while time.time() - t0 < 20 and not url:
            ch = proc.stdout.read(1)
            if not ch:
                break
            buf += ch
            m = re.search(r"http://127\.0\.0\.1:\d+/panel\?token=[a-f0-9]{32}", buf)
            if m:
                url = m.group(0)
        check("发布 exe 启动并打印面板 URL", bool(url), url.split("?")[0] if url else buf[:120])
        if not url:
            return 1
        time.sleep(7)  # 等首拍完成

        with sync_playwright() as p:
            b = p.chromium.launch(channel="chrome")
            pg = b.new_page(viewport={"width": 420, "height": 900})
            errs = []
            pg.on("console", lambda m: errs.append(m.type + ":" + m.text) if m.type == "error" else None)
            pg.on("pageerror", lambda e: errs.append("pageerror:" + str(e)))
            resp = pg.goto(url, wait_until="load", timeout=20000)
            pg.wait_for_timeout(2500)
            check("面板文档 200", resp.status == 200, str(resp.status))
            check("面板带 no-store/CSP 安全头",
                  resp.headers.get("cache-control") == "no-store"
                  and "default-src 'none'" in (resp.headers.get("content-security-policy") or ""))

            label = pg.locator("#label").text_content()
            check("状态卡显示真实状态（非空白）", bool(label and label.strip()), repr(label))
            s = pg.evaluate("fetch('/state',{headers:{Authorization:'Bearer '+TOKEN}}).then(r=>r.json())")
            glyph = pg.evaluate("getComputedStyle(document.querySelector('.state-icon')).color")
            hexc = "#%02x%02x%02x" % tuple(int(x) for x in re.findall(r"\d+", glyph)[:3])
            check("状态点颜色 = 回路 s.color", hexc == s.get("color"), f"glyph={hexc} api={s.get('color')}")
            check("状态图标有形状节点",
                  pg.locator(".state-icon circle, .state-icon line").count() >= 1)
            check("IPv4/IPv6 源显示真实绑定地址",
                  bool(pg.locator("#v4").text_content().strip()) and bool(pg.locator("#v6").text_content().strip()))
            rebind_visible = pg.evaluate("!document.getElementById('rebind-btn').hidden")
            check("补绑按钮按 v6_state 显隐", rebind_visible == (s.get("v6_state") == "offline"),
                  f"visible={rebind_visible} v6_state={s.get('v6_state')}")

            # 状态/设置分栏：状态页默认可见；设置页懒加载（切过去才拉
            # /config 与 /accounts），状态页轮询在隐藏/切页时暂停。
            check("面板有状态/设置分栏", pg.locator("#tabs").count() == 1)
            check("状态页默认选中", pg.get_attribute("#tab-status", "aria-selected") == "true")
            check("状态页不预加载设置（懒加载）", pg.locator("#f-account option").count() == 0)
            pg.click("#tab-settings")
            pg.wait_for_timeout(900)
            check("切到设置页后状态页隐藏", pg.evaluate("document.getElementById('view-status').hidden"))
            check("设置页加载历史账号", pg.locator("#f-account option").count() >= 1)
            check("配置表单回填账号", bool(pg.locator("#f-user").input_value().strip()))
            check("复选框强调色在调色板内",
                  pg.evaluate("getComputedStyle(document.getElementById('f-auto')).accentColor") == "rgb(79, 157, 99)")

            # 高级设置区（P1）：可折叠、回填 ini 已有字段、词表可见。
            # 旧构建缺这个区：报告 FAIL 而不是让脚本异常中断（其余断言仍要跑完）。
            has_adv = (pg.locator("#advanced").count() == 1
                       and pg.locator("#adv-toggle").count() == 1)
            check("高级设置区存在且可折叠", has_adv, "" if has_adv else "旧构建无 #advanced")
            if has_adv:
                pg.click("#adv-toggle")
                pg.wait_for_timeout(300)
                check("高级设置回填探测间隔", bool(pg.locator("#f-interval").input_value()))
                # 词表空 = 合法配置（用户显式配空）：按「面板回填与 ini 一致」
                # 判，不按非空判——非空断言在词表显式配空的机器上必假红
                # （2026-11-22 装机 30/31 唯一 FAIL 的根因）。
                cp = configparser.RawConfigParser()
                cp.read(CFG, encoding="utf-8")
                norm = lambda s: [x.strip() for x in s.split(",") if x.strip()]
                exp_s = norm(cp.get("response", "success_marks", fallback=""))
                exp_f = norm(cp.get("response", "freeze_marks", fallback=""))
                got_s = norm(pg.locator("#f-success-marks").input_value())
                got_f = norm(pg.locator("#f-freeze-marks").input_value())
                check("高级设置回填响应词表（与 ini 一致，空=合法）",
                      got_s == exp_s and got_f == exp_f,
                      f"success ini={exp_s} ui={got_s}; freeze ini={exp_f} ui={got_f}")
                pg.click("#login-form-adv summary")
                pg.wait_for_timeout(200)
                check("高级设置回填登录表单模板", bool(pg.locator("#f-login-form").input_value()))
                pg.screenshot(path=os.path.join(SHOTS, "release-advanced.png"), full_page=True)
                pg.click("#adv-toggle")
                pg.wait_for_timeout(200)
            else:
                check("高级设置回填探测间隔", False, "缺少高级区")
                check("高级设置回填响应词表", False, "缺少高级区")
                check("高级设置回填登录表单模板", False, "缺少高级区")

            # 免认证时段：结构化时间输入 + 前端/服务端双闸（非法值不得写入）。
            check("免认证时段是结构化时间输入",
                  pg.locator("#free-list .free-start").count() >= 1
                  and pg.locator("#free-list .free-end").count() >= 1)
            check("免认证时段回填成时间值",
                  bool(pg.locator("#free-list .free-start").first.input_value())
                  and bool(pg.locator("#free-list .free-end").first.input_value()))
            pg.locator("#free-list .free-start").first.fill("12:00")
            pg.locator("#free-list .free-end").first.fill("12:00")
            pg.click("#save")
            pg.wait_for_timeout(500)
            cfgmsg = pg.locator("#cfg-msg").text_content() or ""
            check("免认证时段相同起止被前端拒绝", "不能相同" in cfgmsg, cfgmsg[:80])
            pg.evaluate("loadConfig()")
            pg.wait_for_timeout(800)
            pg.evaluate("document.getElementById('cfg-msg').textContent=''")

            # 回状态页：日志/流量按钮在状态视图里（设置页隐藏）。
            pg.click("#tab-status")
            pg.wait_for_timeout(500)
            check("切回状态页后设置页隐藏", pg.evaluate("document.getElementById('view-settings').hidden"))
            pg.screenshot(path=os.path.join(SHOTS, "release-default.png"), full_page=True)

            check("日志卡默认隐藏", pg.evaluate("document.getElementById('log-card').hidden"))
            pg.click("#log-btn")
            pg.wait_for_timeout(1200)
            lt = pg.locator("#log-view").text_content() or ""
            check("日志卡展开显示真实日志行", len(lt.splitlines()) >= 3, f"{len(lt.splitlines())} 行")
            pg.screenshot(path=os.path.join(SHOTS, "release-logs.png"), full_page=True)

            pg.click("#flux-btn")
            pg.wait_for_timeout(3000)
            flux = pg.locator("#flux").text_content() or ""
            data = pg.evaluate("fetch('/flux',{headers:{Authorization:'Bearer '+TOKEN}}).then(r=>r.json())")
            uname = data.get("用户名称", "")
            values = [v for k, v in data.items() if k != "用户名称" and v]
            check("查流量不把用户名当流量",
                  bool(flux.strip()) and flux.strip() != uname, flux[:80])
            if "错误" in data:
                check("查流量失败时显示原因", "查询失败" in flux, flux[:80])
            elif values:
                check("查流量显示门户返回的流量字段",
                      any(v in flux for v in values), flux[:80])
            else:
                check("查流量无流量字段时诚实提示", "未返回流量字段" in flux, flux[:80])
            pg.screenshot(path=os.path.join(SHOTS, "release-flux.png"), full_page=True)
            check("历史账号下拉有条目", pg.locator("#f-account option").count() >= 1)
            check("浏览器控制台无 error", len(errs) == 0, str(errs))

            # 服务端校验放在控制台断言之后：这是刻意触发的 400，Chrome 会把它
            # 记成 console error；不能让它把「无 error」判红，也不为此过滤真实错误。
            status = pg.evaluate(
                "fetch('/config',{method:'POST',headers:{...H,'Content-Type':'application/json'},"
                "body:JSON.stringify({free_windows:'25:00-06:30'})}).then(r=>r.status)")
            check("非法免认证时段被服务端拒绝", status == 400, str(status))

            # 第一实例仍存活时，第二实例必须被拒（OS 单实例锁）。
            # cnald 与 cnalw 同目录（源码构建场景）或由 CNALD 环境变量指定。
            cnald = os.environ.get("CNALD") or os.path.join(
                os.path.dirname(EXE), "cnald.exe")
            if os.path.exists(cnald):
                d = subprocess.run([cnald, "--config", CFG],
                                   capture_output=True, text=True, timeout=15, check=False)
                check("第二实例被拒（单实例锁）", "已有守护实例在运行" in (d.stdout + d.stderr))
            b.close()
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except Exception:
            proc.kill()

    print("\n=== 汇总 ===")
    print("通过", len(passed), "失败", len(failed))
    for f in failed:
        print("  FAILED:", f)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())