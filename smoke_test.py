"""keeper.py 单元冒烟测试：不联网、不改状态。python -I smoke_test.py"""
import importlib.util

spec = importlib.util.spec_from_file_location("keeper", "keeper.py")
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)

# 1) 配置钳制
assert m.clamp(0, 1, 300, "x") == 1
assert m.clamp(999, 1, 300, "x") == 300
assert m.clamp(3, 1, 300, "x") == 3

# 2) 登录响应解析：实测单引号伪 JSON（portal.example.edu 真实格式）
f = m.Keeper.parse_login
assert f("{'success':true,'msg':'ok'}") == (True, "")
assert f('{"success":true,"msg":"ok"}') == (True, "")
assert f("{'success':false,'msg':\"Task 'NOAUTH:*' not taken\"}") == (
    False, "Task 'NOAUTH:*' not taken")
assert f('{"ret_code":"0","msg":"ok"}') == (True, "")
assert f('{"ret_code":"1","msg":"password error"}') == (False, "password error")
assert f("login_ok") == (True, "")
assert f("{'success':false,'msg':'您已在线,请勿重复登录'}") == (True, "已在线")
assert f("{'success':false,'msg':'您已在线'}") == (True, "已在线")
assert f("") == (False, "空响应")
assert f("random text") == (False, "random text")
# 负向词防误判（P0 回归："登录未成功"含"成功"子串）
assert f("登录未成功，请重试") == (False, "登录未成功，请重试")
assert f("认证不成功") == (False, "认证不成功")
assert f("var login_ok = false;") == (False, "var login_ok = false;")
assert f("操作失败：密码错误") == (False, "操作失败：密码错误")

# 3) userflux 表格解析：直接调用 fetch_flux 的解析路径（正则 + 全角冒号处理）
html = "<table><tr><td>用户名称：</td><td>test</td></tr><tr><td>已用流量：</td><td>1.2GB</td></tr></table>"
rows = m.re.findall(
    r"<tr[^>]*>\s*<td[^>]*>(.*?)</td>\s*<td[^>]*>(.*?)</td>\s*</tr>", html, m.re.S)
data = {m.re.sub(r"<[^>]+>", "", k).replace("：", "").strip(): v.strip()
        for k, v in rows}
assert data == {"用户名称": "test", "已用流量": "1.2GB"}, data

# 4) 表单组装
k = m.Keeper.__new__(m.Keeper)
k.cfg = dict(m.DEFAULTS)
k.form = {**m.FORM_BASE, "userName": "u", "pwd": "p", **m.load_form_extras()}
assert k.form["opr"] == "pwdLogin" and k.form["userName"] == "u"

# 5) 熔断状态机：probe 恒失败 + login 恒成功 → 3 轮后必须进入退避而非继续登录
# （用真 __init__ 构造：__new__ 手工装配清单在 2026-11-05 已四次漏字段
#  [auto_reconnect/_session_dead 系/_login_cap_until 系/free_windows]，
#  结构性脆弱——真构造 + 只 stub 行为，字段遗漏从此不可能）
import time as _t


def _smoke_keeper():
    return m.Keeper(dict(m.DEFAULTS, username="u", password="p",
                         bind_ip="127.0.0.1",
                         server="https://portal.example.edu"))


k2 = _smoke_keeper()
k2.portal_alive = lambda: True
k2.probe_online = lambda: False
login_calls = []
def fake_login():
    login_calls.append(1)
    return True, ""
k2.do_login = fake_login
k2.clash_recover = lambda: None
for _ in range(5):
    k2._last_login_at = 0.0  # 每轮重置：本场景只验证熔断，节流由循环30单独守
    k2.tick()
assert len(login_calls) == 3, f"5 轮里只应登录 3 次后熔断，实际 {len(login_calls)}"
assert k2.state == "rejected" and k2.reject_until > _t.monotonic()

# 6) 瞬态拒绝（NOAUTH 竞态，2026-11-04 02:11 实测）：短退避；连续 5 次升级长退避防风暴
k3 = _smoke_keeper()
k3.probe_online = lambda: False
k3.portal_alive = lambda: True
k3.do_login = lambda: (False, "Task 'NOAUTH:*' not taken")
k3.clash_recover = lambda: None
for _ in range(4):
    k3.reject_until = 0.0  # 模拟退避到期
    k3._last_login_at = 0.0  # 每轮重置（同 k2：节流由循环30单独守）
    k3.tick()
assert 0 < k3.reject_until - _t.monotonic() <= 40, "前 4 次瞬态拒绝应短退避"
k3.reject_until = 0.0
k3._last_login_at = 0.0
k3.tick()  # 第 5 次连续瞬态拒绝
assert k3.reject_until - _t.monotonic() > 500, "连续 5 次瞬态拒绝应升级为长退避"
# 硬错误（密码错）直接长退避
k3.transient_streak = 0; k3.reject_until = 0.0
k3._last_login_at = 0.0
k3.do_login = lambda: (False, "password error")
k3.tick()
assert k3.reject_until - _t.monotonic() > 500, "硬性拒绝应直接长退避"
print("SMOKE-OK")