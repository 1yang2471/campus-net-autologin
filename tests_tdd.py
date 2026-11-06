# -*- coding: utf-8 -*-
"""TDD 循环测试：行为级，经公共接口（不 mock 内部件）。

python -I tests_tdd.py   → 全绿 = 产品行为契约成立
涉及真实门户/真实注册表的用例默认 SKIP（开源后任何人跑测试零副作用）；
本机验收时设环境变量 CNA_LIVE_TESTS=1 开启真实网络项。
环境前提：循环10-15 含 tkinter 交互会话（建真实窗口，alpha=0 不闪现）；
无桌面会话（CI/RDP 断开）下这些用例不适用，属环境限制而非产品缺陷。
"""
import importlib
import subprocess
import sys
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT))
sys.path.insert(0, str(ROOT / "shell"))

LIVE = os.environ.get("CNA_LIVE_TESTS") == "1"

PASS = []
FAIL = []
SKIP = []


class _Skip(Exception):
    pass


def require_live():
    if not LIVE:
        raise _Skip("真实门户用例，默认跳过（CNA_LIVE_TESTS=1 开启）")


def check(name, fn):
    try:
        fn()
        PASS.append(name)
        print(f"  PASS {name}")
    except _Skip as s:
        SKIP.append(name)
        print(f"  SKIP {name}: {s}")
    except Exception as exc:  # noqa: BLE001
        FAIL.append((name, exc))
        print(f"  FAIL {name}: {exc.__class__.__name__}: {exc}")


print("== 循环1：单实例入口 ==")


def t1_single_entry_module():
    """入口模块 app_main 存在且提供 main()——启动只此一个。"""
    mod = importlib.import_module("app_main")
    assert callable(mod.main), "app_main.main 应可调用"


def t1_entry_wires_tray_and_panel_and_keeper():
    """入口的装配函数把 keeper/托盘/面板接在同一进程。"""
    mod = importlib.import_module("app_main")
    assert hasattr(mod, "assemble"), "应有 assemble() 装配函数"


check("app_main 存在且有 main()", t1_single_entry_module)
check("app_main 有 assemble 装配", t1_entry_wires_tray_and_panel_and_keeper)

print("== 循环2：开机自启开关 ==")


def t2_autostart_module():
    """autostart 模块提供 is_enabled/register/unregister 三操作。"""
    mod = importlib.import_module("autostart")
    assert callable(mod.is_enabled) and callable(mod.register) \
        and callable(mod.unregister), "三操作应可调用"


def t2_autostart_roundtrip():
    """注册→命令指向 app_main→is_enabled 真→注销→假。

    真实注册表操作但零副作用：先快照原值，测完原样恢复（trouble-maker
    P1-12：旧版测完直接留空，用户开着自启跑一次测试就被静默关掉）。
    """
    import autostart
    saved = autostart.current_command()
    autostart.unregister()  # 清场
    try:
        assert autostart.register(), "注册应成功"
        assert autostart.is_enabled(), "注册后应查询到"
        cmd = autostart.current_command()
        assert "app_main.py" in cmd or getattr(autostart.sys, "frozen", False), f"命令须指向入口，实际: {cmd}"
        assert '"' in cmd, f"路径须加引号防空格，实际: {cmd}"
    finally:
        autostart.unregister()
        if saved:  # 原样恢复用户此前的自启配置
            import winreg
            try:
                with winreg.OpenKey(winreg.HKEY_CURRENT_USER, autostart.RUN_KEY,
                                    0, winreg.KEY_SET_VALUE) as key:
                    winreg.SetValueEx(key, autostart.VALUE_NAME, 0,
                                      winreg.REG_SZ, saved)
            except OSError:
                pass
    assert not autostart.is_enabled() or saved, "注销后不应再查询到（除非原本就有）"


check("autostart 三操作接口", t2_autostart_module)
check("注册/注销往返（真实注册表，测后恢复原值）", t2_autostart_roundtrip)

print("== 循环3：托盘菜单集成 ==")


def t3_tray_menu_items():
    """托盘菜单含：立即重连/查流量/打开认证页/查看日志/开机自启/退出。"""
    from keeper import DEFAULTS, Keeper, Tray
    cfg = dict(DEFAULTS)
    cfg.update({"username": "smoke", "password": "smoke"})
    k = Keeper(cfg)
    tray = Tray(k)
    labels = [str(getattr(it, "text", it)) for it in tray.icon.menu]
    for want in ("立即检测重连", "查流量", "打开认证页", "查看日志", "开机自启", "退出"):
        assert any(want in lab for lab in labels), f"菜单缺 {want}，实际 {labels}"
    tray.icon.stop()


def t3_tray_panel_optional():
    """传入 on_open_panel 时，菜单出现"打开面板"（单进程装配需要）。"""
    from keeper import DEFAULTS, Keeper, Tray
    cfg = dict(DEFAULTS)
    cfg.update({"username": "smoke", "password": "smoke"})
    k = Keeper(cfg)
    called = []
    tray = Tray(k, on_open_panel=lambda: called.append(1))
    labels = [str(getattr(it, "text", it)) for it in tray.icon.menu]
    assert any("打开面板" in lab for lab in labels), labels
    tray.icon.stop()


check("托盘六项菜单齐备", t3_tray_menu_items)
check("面板回调可注入", t3_tray_panel_optional)

print("== 循环4：README 与代码一致 ==")

from keeper import DEFAULTS as _DEF


def t4_readme_documents_every_config():
    """DEFAULTS 里每个配置键都必须在 README 出现（文档不欠账）。"""
    readme = (ROOT / "README.md").read_text(encoding="utf-8")
    missing = [k for k in _DEF if k not in readme]
    assert not missing, f"README 缺配置项说明: {missing}"


def t4_readme_no_stale_config():
    """已废除的 probe_url 不得残留在 README（防用户照旧文档配错）。"""
    readme = (ROOT / "README.md").read_text(encoding="utf-8")
    assert "probe_url =" not in readme, "README 仍推荐已废除的 probe_url 配置"


def t4_readme_covers_new_architecture():
    """核心新机制必须在 README：物理栈、L2 观察、keyring、自启。"""
    readme = (ROOT / "README.md").read_text(encoding="utf-8")
    for concept in ("物理栈", "L2", "keyring", "开机自启"):
        assert concept in readme, f"README 缺机制说明: {concept}"


def t4_tray_and_panel_colors_in_sync():
    """托盘/面板状态色是跨内核-外壳手工成对常量——不一致就是 bug（去AI味
    统一降饱和时立此守卫：改一边忘另一边，测试直接红）。"""
    from keeper import TRAY_COLORS
    from shell.kernel_api import STATE_COLORS
    for state, rgb in TRAY_COLORS.items():
        hexv = STATE_COLORS.get(state)
        assert hexv is not None, f"面板缺状态色 {state}"
        want = "#{:02x}{:02x}{:02x}".format(*rgb)
        assert hexv.lower() == want.lower(), f"{state} 漂移：托盘 {want} vs 面板 {hexv}"


check("README 覆盖全部配置项", t4_readme_documents_every_config)
check("README 无过时配置", t4_readme_no_stale_config)
check("README 覆盖新架构", t4_readme_covers_new_architecture)
check("托盘/面板状态色同步", t4_tray_and_panel_colors_in_sync)

print("== 循环5：首配向导行为 ==")
import builtins
import tempfile
import keeper as _km


def _with_temp_config(fn):
    """把两份 CONFIG_FILE 绑定（keeper + keeper_paths）一起指向临时文件，
    执行 fn（隔离真实配置，测公共行为；trouble-maker P1-11：旧版只 patch
    keeper 侧，credentials 侧仍读写 %LOCALAPPDATA% 真实配置）。"""
    tmp = Path(tempfile.mkdtemp()) / "config.ini"
    import shell.keeper_paths as _kp
    old = _km.CONFIG_FILE
    old_p = _kp.CONFIG_FILE
    _km.CONFIG_FILE = tmp
    _kp.CONFIG_FILE = tmp
    try:
        return fn(tmp)
    finally:
        _km.CONFIG_FILE = old
        _kp.CONFIG_FILE = old_p


def t5_wizard_generates_config():
    """无配置 + 有终端：引导输入账号密码 → 生成 config.ini 并返回含凭据的 cfg。"""
    def run(tmp):
        inputs = iter(["myuser", "mypass"])
        old_in, old_gp = builtins.input, _km.getpass.getpass
        builtins.input = lambda *a: next(inputs)
        _km.getpass.getpass = lambda *a: next(inputs)
        try:
            cfg = _km.ensure_config(_km.load_config())
        finally:
            builtins.input, _km.getpass.getpass = old_in, old_gp
        assert cfg and cfg["username"] == "myuser"
        assert tmp.is_file(), "应生成 config.ini"
        return cfg
    _with_temp_config(run)


def t5_wizard_headless_guides():
    """无配置 + 无终端（pythonw）：返回 None 且日志指引手动运行，不崩溃。"""
    def run(tmp):
        old_stdin = _km.sys.stdin
        class _NoTTY:
            def isatty(self): return False
        _km.sys.stdin = _NoTTY()
        try:
            cfg = _km.ensure_config(_km.load_config())
        finally:
            _km.sys.stdin = old_stdin
        assert cfg is None, "无终端应返回 None 而非崩溃"
    _with_temp_config(run)


def t5_existing_config_missing_password_reports():
    """已有 config 但缺密码：返回 None 提示编辑，不覆盖已有文件。"""
    def run(tmp):
        tmp.write_text("[keeper]\nusername = u\npassword =\n", encoding="utf-8")
        cfg = _km.ensure_config(_km.load_config())
        assert cfg is None
        assert "username = u" in tmp.read_text(encoding="utf-8"), "不应覆盖已有配置"
    _with_temp_config(run)


check("向导：引导生成配置", t5_wizard_generates_config)
check("向导：无终端优雅退出", t5_wizard_headless_guides)
check("向导：缺密码不覆盖已有", t5_existing_config_missing_password_reports)

print("== 循环6：exe 出包脚本 ==")


def t6_build_command_anti_false_positive():
    """出包命令含防误报关键项：onedir（非 onefile）、windowed、显式入口。"""
    import build_exe
    cmd = build_exe.build_command()
    joined = " ".join(cmd)
    assert "--onedir" in joined and "--onefile" not in joined, "须 onedir（onefile 更易报毒）"
    assert "--noconsole" in joined or "--windowed" in joined, "须窗口化（无黑框）"
    assert "app_main.py" in joined, "入口须为 app_main"
    assert "--name" in cmd, "须显式命名"
    assert "--exclude-module" in joined and "numpy" in joined, \
        "numpy 是 Pillow 可选依赖会被误采集（实测 27MB），必须排除"


def t6_build_command_no_shell_injection():
    """命令是参数列表（无 shell 字符串拼接）。"""
    import build_exe
    cmd = build_exe.build_command()
    assert isinstance(cmd, list) and all(isinstance(x, str) for x in cmd)


check("出包命令含防误报措施", t6_build_command_anti_false_positive)
check("出包命令为参数列表", t6_build_command_no_shell_injection)

print("== 循环7：单实例 OS 锁（强杀可重夺）==")


def t7_lock_is_exclusive_across_processes():
    """子进程持锁期间，本进程 acquire 应失败；子进程被强杀后应能重夺。

    用临时目录隔离锁路径（避免与在岗的真实实例冲突）。
    """
    import time
    tmpdir = Path(tempfile.mkdtemp())
    old_base = _km.BASE_DIR
    _km.BASE_DIR = tmpdir
    holder = subprocess.Popen(
        [sys.executable, "-c",
         "import sys, tempfile; from pathlib import Path; sys.path.insert(0, %r); "
         "import keeper; keeper.BASE_DIR = Path(%r); "
         "assert keeper.acquire_single_instance(); "
         "import time; time.sleep(60)" % (str(ROOT), str(tmpdir))],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        time.sleep(1.5)  # 等子进程拿到锁
        _km._lock_fh = None
        got_while_alive = _km.acquire_single_instance()
        if _km._lock_fh:
            _km._lock_fh.close(); _km._lock_fh = None
        holder.kill(); holder.wait(timeout=10)
        time.sleep(0.5)
        got_after_kill = _km.acquire_single_instance()
        if _km._lock_fh:
            _km._lock_fh.close(); _km._lock_fh = None
    finally:
        _km.BASE_DIR = old_base
        holder.kill()
    assert got_while_alive is False, "子进程持锁时不应抢到"
    assert got_after_kill is True, "强杀后锁应被 OS 释放、可重夺"


check("单实例锁跨进程互斥 + 强杀可重夺", t7_lock_is_exclusive_across_processes)

print("== 循环8：面板↔内核 接口契约 ==")


def t8_facade_satisfies_kernel_protocol():
    """KeeperFacade 必须提供 Shell 实际调用的全部 Kernel 方法。"""
    import app_main
    cfg = dict(_km.DEFAULTS)
    cfg.update({"username": "smoke", "password": "smoke"})
    k = _km.Keeper(cfg)
    f = app_main.KeeperFacade(k)
    for meth in ("snapshot", "force_reconnect", "fetch_flux",
                 "on_change", "test_credentials", "save_credentials",
                 "switch_account"):
        assert hasattr(f, meth) and callable(getattr(f, meth)), f"缺 {meth}"
    assert f.needs_setup is False
    snap = f.snapshot()
    assert snap["state"] in ("online", "offline", "portal_down", "rejected", "starting")
    assert "interval" in snap and "reject_remaining" in snap


check("KeeperFacade 满足 Kernel 协议", t8_facade_satisfies_kernel_protocol)

print("== 循环9：向导保存→凭据就绪→可启动 ==")


def t9_setup_facade_enables_startup():
    """SetupFacade 保存后，_effective_config 应判 ready（向导→守护启动链，审查 P0-2）。"""
    import app_main
    import shell.keeper_paths as _kp
    tmp = Path(tempfile.mkdtemp()) / "config.ini"
    old_cfg = _km.CONFIG_FILE
    old_p = _kp.CONFIG_FILE
    _km.CONFIG_FILE = tmp
    _kp.CONFIG_FILE = tmp  # credentials 侧同 patch（P1-11：不碰真实配置）
    test_user = "__t9_probe__"
    try:
        # 保存前：无配置 → 未就绪
        _, ready_before = app_main._effective_config()
        assert ready_before is False, "空配置应判未就绪"
        # SetupFacade 写配置（密码进 keyring，ini 存占位）
        app_main.SetupFacade().save_credentials(test_user, "pw", "https://portal.example.edu")
        # 保存后：keyring 有密码 → 就绪
        _, ready_after = app_main._effective_config()
        assert ready_after is True, "向导保存后应判就绪（可启动守护）"
    finally:
        _km.CONFIG_FILE = old_cfg
        _kp.CONFIG_FILE = old_p
        try:
            import keyring
            keyring.delete_password("campus-net-autologin", test_user)
        except Exception:  # noqa: BLE001
            pass


def t9_probe_login_no_mutation():
    """probe_login 是一次性探针：不产生运行中 keeper，也不留 session（审查 P0-3/P1-16）。"""
    require_live()  # 向真实门户 POST 假凭据属攻击面，默认跳过（trouble-maker P1-12）
    import keeper as _k
    # 用假凭据走真实服务器（预期被拒），验证返回契约且不抛
    cfg = dict(_k.DEFAULTS, server="https://portal.example.edu")
    ok, msg = _k.Keeper.probe_login(cfg, "__nope__", "__nope__")
    assert isinstance(ok, bool) and isinstance(msg, str)
    assert ok is False, "假凭据不应成功"


check("向导保存→就绪→可启动", t9_setup_facade_enables_startup)
check("probe_login 一次性探针契约", t9_probe_login_no_mutation)

print("== 循环10：辅助窗口宿主 Desk（托盘与面板共用同一实现）==")


def _wait_for(pred, timeout=10.0, step=0.05):
    """轮询等待条件成立；超时抛 AssertionError（供 UI 异步行为断言）。"""
    import time
    deadline = time.time() + timeout
    while time.time() < deadline:
        if pred():
            return True
        time.sleep(step)
    return False


def t10_desk_flux_shows_dialog_with_kernel_data():
    """Desk 收到查流量请求 → 弹出'流量信息'窗口，内容来自 kernel.fetch_flux。"""
    from shell.desk import Desk

    class FakeKernel:
        def fetch_flux(self):
            return {"已用流量": "1.2 GB", "剩余流量": "2.8 GB"}

    desk = Desk()
    desk.start()
    try:
        desk.post("flux", kernel=FakeKernel())
        ok = _wait_for(lambda: desk.flux_window is not None
                       and desk.flux_window.has_result())
        assert ok, "查流量后应出现流量信息窗口"
        text = desk.flux_window.content()
        assert "流量信息" in desk.flux_window.title(), desk.flux_window.title()
        assert "1.2 GB" in text, text
    finally:
        desk.stop()


def t10_desk_log_window_shows_tail():
    """Desk 收到查看日志请求 → 打开日志窗，展示日志尾部若干行。"""
    import tempfile
    from shell.desk import Desk

    tmp = Path(tempfile.mkdtemp()) / "keeper.log"
    tmp.write_text("\n".join(f"line{i}" for i in range(1000)), encoding="utf-8")

    desk = Desk()
    desk.start()
    try:
        desk.post("log", path=tmp)
        ok = _wait_for(lambda: desk.log_window is not None, timeout=20.0)
        assert ok, "查看日志应打开日志窗"
        ok = _wait_for(lambda: "line999" in desk.log_window.content(), timeout=10.0)
        assert ok, desk.log_window.content()[:200]
        assert "line500" not in desk.log_window.content(), "只应显示尾部若干行"
    finally:
        desk.stop()


def t10_log_window_smart_follow():
    """智能跟随：停在底部才跟随；停在中间刷新后视图必须原地不动
    （用户实测 bug：delete+insert 全量重建把非底部视图重置到顶部，
    滑块"要么触顶要么触底，停不住"）。"""
    import tempfile
    import tkinter as tk
    from shell.windows import LogWindow

    tmp = Path(tempfile.mkdtemp()) / "keeper.log"
    tmp.write_text("\n".join(f"line{i}" for i in range(300)), encoding="utf-8")
    root = tk.Tk()
    root.withdraw()
    win = None
    try:
        win = LogWindow(root, tmp)
        win.attributes("-alpha", 0.0)  # 映射获得真实几何但不闪现（withdrawn 无分配）
        win.update()
        # 打开必须停在最新（底部）——see() 在映射前会被 Tk 丢弃，须映射后补
        assert win._at_bottom(), "日志窗打开应停在最新行"
        # 用户把视图停在中间（拖滑块的真实场景）
        win._text.yview_moveto(0.5)
        win.update()
        lo_before = win._text.yview()[0]
        assert 0.3 < lo_before, "前置条件：视图应已停在中间"
        # 追加新行并刷新：视图必须仍停在中间——不触顶也不触底
        with tmp.open("a", encoding="utf-8") as fh:
            fh.write("\nbrand_new_tail")
        win.refresh()
        win.update()
        lo_after, hi_after = win._text.yview()
        assert 0.3 < lo_after and hi_after < 0.99, \
            f"刷新后视图应停在用户位置，实际 lo={lo_after:.2f} hi={hi_after:.2f}"
        assert "brand_new_tail" in win.content(), "新行应已写入文本区"
        # 用户自己回到底部 → 恢复跟随
        win._text.yview_moveto(1.0)
        win.update()
        assert win._at_bottom(), "回到底部应恢复跟随"
        with tmp.open("a", encoding="utf-8") as fh:
            fh.write("\nanother_tail")
        win.refresh()
        win.update()
        assert "another_tail" in win.content() and win._at_bottom()
    finally:
        if win is not None:
            win.destroy()
        root.destroy()


def t10_desk_flux_failure_shows_error():
    """查流量抛异常 → 窗口里给出错误文案，不静默失败。"""
    from shell.desk import Desk

    class BoomKernel:
        def fetch_flux(self):
            raise RuntimeError("门户不可达")

    desk = Desk()
    desk.start()
    try:
        desk.post("flux", kernel=BoomKernel())
        ok = _wait_for(lambda: desk.flux_window is not None
                       and desk.flux_window.has_result())
        assert ok, "查流量失败也应给用户一个窗口"
        assert "门户不可达" in desk.flux_window.content(), desk.flux_window.content()
    finally:
        desk.stop()


check("Desk 查流量→流量信息窗口", t10_desk_flux_shows_dialog_with_kernel_data)
check("Desk 查看日志→日志窗（尾部）", t10_desk_log_window_shows_tail)
check("日志窗智能跟随（翻历史不拽回）", t10_log_window_smart_follow)
check("Desk 查流量失败→错误可见", t10_desk_flux_failure_shows_error)

print("== 循环11：托盘动作与面板共用同一入口 ==")


def t11_tray_flux_and_log_use_injected_callbacks():
    """托盘'查流量/查看日志'必须走注入的回调（不再自弹气泡/拉记事本）。"""
    from keeper import DEFAULTS, Keeper, Tray
    cfg = dict(DEFAULTS)
    cfg.update({"username": "smoke", "password": "smoke"})
    k = Keeper(cfg)
    seen = []
    tray = Tray(k, on_open_panel=lambda: None,
                on_flux=lambda: seen.append("flux"),
                on_log=lambda: seen.append("log"))
    labels = {str(getattr(it, "text", it)): it for it in tray.icon.menu}
    for want, expect in (("查流量", "flux"), ("查看日志", "log")):
        item = next(it for lab, it in labels.items() if want in lab)
        item(None)  # pystray 公共激活接口：__call__(icon) → action(icon, item)
        assert expect in seen, f"{want} 应触发 {expect} 回调，实际 {seen}"
    tray.icon.stop()


def t11_tray_flux_does_not_fetch_itself():
    """托盘不再自己发气泡：fetch_flux 由宿主统一调用（托盘内不应触发网络请求）。"""
    from keeper import DEFAULTS, Keeper, Tray
    cfg = dict(DEFAULTS)
    cfg.update({"username": "smoke", "password": "smoke"})
    k = Keeper(cfg)

    def boom():
        raise AssertionError("托盘不应直接查流量")

    k.fetch_flux = boom
    tray = Tray(k, on_open_panel=lambda: None,
                on_flux=lambda: None, on_log=lambda: None)
    item = next(it for it in tray.icon.menu if "查流量" in str(getattr(it, "text", it)))
    item(None)   # 不应抛错
    tray.icon.stop()


def t11_panel_flux_button_uses_same_desk():
    """面板'查流量'按钮与托盘走同一个 Desk（界面一致的唯一来源）。"""
    import shell.app as app_mod
    from shell.app import Shell
    from shell.desk import Desk

    class FakeKernel:
        needs_setup = False

        def snapshot(self):
            return {"state": "online", "detail": "", "interval": 3, "reject_remaining": 0}

        def fetch_flux(self):
            return {"已用流量": "3.4 GB"}

        def force_reconnect(self): ...
        def on_change(self, cb): ...
        def save_credentials(self, *a): ...
        def test_credentials(self, *a):
            return True, "ok"

    desk = Desk()
    desk.start()
    win = None
    try:
        win = Shell(FakeKernel(), desk=desk)
        win._on_flux()
        ok = _wait_for(lambda: desk.flux_window is not None
                       and desk.flux_window.has_result())
        assert ok, "面板查流量应通过 Desk 弹出窗口"
        assert "3.4 GB" in desk.flux_window.content(), desk.flux_window.content()
    finally:
        if win is not None:
            try:
                win.destroy()
            except Exception:  # noqa: BLE001
                pass
        desk.stop()


check("托盘动作走注入回调", t11_tray_flux_and_log_use_injected_callbacks)
check("托盘不自行查流量", t11_tray_flux_does_not_fetch_itself)
check("面板与托盘共用 Desk", t11_panel_flux_button_uses_same_desk)

print("== 循环12：门户自动探测（少填：302 劫持页自报门户地址）==")


def t12_parse_location_basic():
    """302 Location → 门户基址（scheme+host），路径查询丢弃。"""
    from keeper import parse_portal_location as P
    assert P({"Location": "http://10.0.0.1/ac_portal/pc.html?ret=x"}) == "http://10.0.0.1"
    assert P({"Location": "https://portal.example.edu/portal/"}) == "https://portal.example.edu"
    assert P({"location": "http://10.0.0.1/"}) == "http://10.0.0.1", "header 大小写不敏感"


def t12_parse_location_security_rejects():
    """安全边界：非 http/https、环回、localhost、链路本地一律拒绝（防钓鱼重定向）。

    注：校园门户本就住在私有段（10.0.0.1/10.x/172.16-31/28.x）——这些是产品
    核心目标，不拒绝；但凭据绝不自动发送，探测阶段只做无凭据 GET 指纹验证。
    """
    from keeper import parse_portal_location as P
    assert P({"Location": "ftp://10.0.0.1/"}) == ""
    assert P({"Location": "javascript:alert(1)"}) == ""
    assert P({"Location": "http://127.0.0.1:8080/"}) == ""
    assert P({"Location": "http://localhost/"}) == ""
    assert P({"Location": "http://169.254.1.1/"}) == ""
    assert P({"Location": "/relative/path"}) == "", "相对 Location 不可信"
    assert P({}) == ""


check("Location 解析出门户基址", t12_parse_location_basic)
check("Location 安全拒绝（scheme/环回/localhost/link-local）", t12_parse_location_security_rejects)


def t12_detect_portal_happy():
    """探测点被 302 劫持 + 登录页含 ac_portal 指纹 → 返回门户基址。"""
    from keeper import detect_portal

    calls = []

    def fake_http(host, ip, path, **kw):
        calls.append((host, path))
        if "generate_204" in path:
            return 302, {"Location": "http://10.0.0.1/ac_portal/pc.html"}, ""
        return 200, {}, "<html>opr=pwdLogin ac_portal</html>"

    got = detect_portal(_http=fake_http)
    assert got == "http://10.0.0.1", got
    assert any("pc.html" in c[1] or "login" in c[1] for c in calls), calls


def t12_detect_portal_rejects_no_credential_leak():
    """三条防线：已登录(204)→空；指纹不符→空；劫持到环回→空。绝不发凭据。"""
    from keeper import detect_portal

    def http_204(host, ip, path, **kw):
        return 204, {}, ""
    assert detect_portal(_http=http_204) == "", "已认证不该给出门户"

    def http_fake_portal(host, ip, path, **kw):
        if "generate_204" in path:
            return 302, {"Location": "http://10.0.0.1/"}, ""
        return 200, {}, "<html>router admin login</html>"  # 无 ac_portal 指纹
    assert detect_portal(_http=http_fake_portal) == "", "指纹不符不得采信"

    def http_loopback(host, ip, path, **kw):
        return 302, {"Location": "http://127.0.0.1:9/"}, ""
    assert detect_portal(_http=http_loopback) == "", "环回劫持拒绝"


check("detect_portal 劫持指纹链", t12_detect_portal_happy)
check("detect_portal 三防线不泄凭据", t12_detect_portal_rejects_no_credential_leak)

print("== 循环13：向导自动探测（少填：服务器不用手打）==")


def t13_wizard_auto_detect():
    """向导打开即自动探测门户并填入服务器字段；表单含"自动检测"重试按钮。"""
    import time
    from shell.app import Shell

    class K:
        needs_setup = True

        def snapshot(self):
            return {"state": "starting", "detail": "", "interval": 3, "reject_remaining": 0}

        def fetch_flux(self):
            return {}

        def force_reconnect(self): ...
        def on_change(self, cb): ...
        def save_credentials(self, *a): ...
        def test_credentials(self, *a):
            return True, "ok"

        def detect_portal(self):
            return "http://10.0.0.1"

    win = Shell(K())
    try:
        deadline = time.time() + 10
        got = ""
        while time.time() < deadline:
            win.update()
            got = win.e_server.get()
            if got == "http://10.0.0.1":
                break
            time.sleep(0.05)
        assert got == "http://10.0.0.1", f"服务器应自动填入探测到的门户，实际 {got!r}"
        # 重试按钮存在
        found = []

        def walk(w):
            for c in w.winfo_children():
                if isinstance(c, __import__("tkinter").Button) and \
                        "自动检测" in c.cget("text"):
                    found.append(c)
                walk(c)
        walk(win)
        assert found, "向导应有'自动检测'按钮供手动重试"
    finally:
        win.destroy()


check("向导自动探测填服务器+重试按钮", t13_wizard_auto_detect)


def t13_panel_log_button_posts_path():
    """面板"打开日志"必须真的把日志路径 post 给 Desk（P0-2 回归：曾因把
    Path 常量当函数调，点击即 TypeError，且 pythonw 下完全静默）。"""
    from shell.app import Shell
    from shell.desk import Desk

    class K:
        needs_setup = False

        def snapshot(self):
            return {"state": "online", "detail": "", "interval": 3, "reject_remaining": 0}

        def fetch_flux(self):
            return {}

        def force_reconnect(self): ...
        def on_change(self, cb): ...
        def save_credentials(self, *a): ...
        def test_credentials(self, *a):
            return True, "ok"

        def detect_portal(self):
            return ""

    desk = Desk()
    desk.start()
    win = None
    try:
        win = Shell(K(), desk=desk)
        win._on_log()  # 不得抛 TypeError
        ok = _wait_for(lambda: desk.log_window is not None, timeout=20.0)
        assert ok, "面板打开日志应在 Desk 弹出日志窗"
    finally:
        if win is not None:
            try:
                win.destroy()
            except Exception:  # noqa: BLE001
                pass
        desk.stop()


check("面板打开日志按钮真实可用", t13_panel_log_button_posts_path)

print("== 循环14：P1 批次修复（trouble-maker 深审落地）==")


def t14_parse_login_html_safe():
    """长 HTML 响应含"错误/error"字样不得误判硬拒绝；强正向标记优先（P1-9）。"""
    from keeper import Keeper
    ok, msg = Keeper.parse_login("<html><div>错误提示区域</div>" + "x" * 300 +
                                 "<script>login_ok = true;</script></html>")
    assert ok is True, (ok, msg)
    ok, msg = Keeper.parse_login("<html>" + "p" * 300 + "error 错误</html>")
    assert ok is False and "无法判定" in msg, (ok, msg)  # 归瞬态，不施 600s 重罚
    assert Keeper.parse_login("登录失败") == (False, "登录失败"), "短响应负向词照旧"
    ok, msg = Keeper.parse_login("<html>" + "p" * 300 + "login_ok = false;")
    assert ok is False and "无法判定" not in msg, (ok, msg)


def t14_reject_detail_persists():
    """退避期间被拒原因不得被覆写消失（P1-8）。"""
    from keeper import DEFAULTS, Keeper
    cfg = dict(DEFAULTS)
    cfg.update({"username": "u", "password": "p"})
    k = Keeper(cfg)
    k.probe_online = lambda: False
    k.portal_alive = lambda: True
    k.do_login = lambda: (False, "用户名或密码错误")
    k.tick()
    assert k.state == "rejected" and "用户名或密码错误" in k.state_detail
    k.tick()
    k.tick()  # 退避窗口内连续 tick——原因必须还在
    assert "用户名或密码错误" in k.state_detail, k.state_detail


def t14_l2_probe_throttled():
    """在线期 L2 探测 60 秒节流：10 个 tick 只打一次 baidu（P1-5）。"""
    import keeper as _k
    from keeper import DEFAULTS, Keeper
    cfg = dict(DEFAULTS)
    cfg.update({"username": "u", "password": "p"})
    k = Keeper(cfg)
    k.probe_online = lambda: True
    calls = []
    old_r, old_h = _k.campus_dns_resolve, _k.physical_http
    def dns(host, *a, **kw):
        calls.append(host)
        return "1.2.3.4"
    _k.campus_dns_resolve = dns
    _k.physical_http = lambda *a, **kw: (200, "ok")
    try:
        for _ in range(10):
            k.tick()
    finally:
        _k.campus_dns_resolve, _k.physical_http = old_r, old_h
    baidu = [c for c in calls if c == "www.baidu.com"]
    assert len(baidu) == 1, f"10 tick 应只 1 次 L2 探测（60s 节流），实际 {len(baidu)}"


def t14_config_values_stripped():
    """ini 值首尾空白清洗：server 尾空格不再让 DNS 永远失败（P1-13/P2-17）。"""
    def run(tmp):
        tmp.write_text("[keeper]\nserver = https://portal.example.edu  \n",
                       encoding="utf-8")
        cfg = _km.load_config()
        assert cfg["server"] == "https://portal.example.edu", repr(cfg["server"])
    _with_temp_config(run)


def t14_header_injection_guard():
    """请求头值含换行一律拒发（裸 HTTP 拆头注入防线）。"""
    status, _, body = _km._raw_http("h", "127.0.0.1", "/",
                                    headers={"User-Agent": "UA\r\nX-Evil: yes"})
    assert status == 0 and "HeaderValue" in body, (status, body)


check("parse_login 长 HTML 不误判", t14_parse_login_html_safe)
check("退避期被拒原因保留", t14_reject_detail_persists)
check("L2 探测 60 秒节流", t14_l2_probe_throttled)
check("ini 值首尾空白清洗", t14_config_values_stripped)
check("请求头换行注入拒发", t14_header_injection_guard)

print("== 循环15：设置页（常用 4 项 + 高级折叠，保存即生效）==")


def t15_apply_config_hot_reload():
    """apply_config：重钳位运行时参数、失效 DNS 缓存、触发立即重连。"""
    from keeper import DEFAULTS, Keeper
    cfg = dict(DEFAULTS)
    cfg.update({"username": "u", "password": "p", "interval": "3"})
    k = Keeper(cfg)
    assert k.interval == 3
    k._portal_ip = "1.2.3.4"
    cfg2 = dict(cfg)
    cfg2["interval"] = "10"
    k.apply_config(cfg2)
    assert k.interval == 10, "热重载未生效"
    assert k._portal_ip == "", "门户 DNS 缓存须失效"
    assert k.force_evt.is_set(), "应触发一次立即重连"


def t15_facade_save_settings():
    """KeeperFacade.save_settings：ini 持久化 + 热重载同时成立。"""
    import app_main
    import shell.keeper_paths as _kp
    tmp = Path(tempfile.mkdtemp()) / "config.ini"
    old_cfg, old_p = _km.CONFIG_FILE, _kp.CONFIG_FILE
    _km.CONFIG_FILE = tmp
    _kp.CONFIG_FILE = tmp
    try:
        cfg = dict(_km.DEFAULTS)
        cfg.update({"username": "u", "password": "p"})
        _km.save_config(cfg)
        k = _km.Keeper(cfg)
        f = app_main.KeeperFacade(k)
        f.save_settings({"interval": "7", "campus_dns": "10.0.0.1"})
        assert k.interval == 7, "热重载未生效"
        assert k.cfg["campus_dns"] == "10.0.0.1"
        re_read = _km.load_config()
        assert re_read["interval"] == "7", "ini 未持久化"
    finally:
        _km.CONFIG_FILE = old_cfg
        _kp.CONFIG_FILE = old_p


def t15_shell_settings_page():
    """设置页：常用 4 项可编辑，保存把结构化值交给内核 facade。"""
    import tkinter as tk
    import shell.app as app_mod
    from shell.app import Shell

    saved = []
    orig_info = app_mod.messagebox.showinfo
    orig_err = app_mod.messagebox.showerror
    app_mod.messagebox.showinfo = lambda *a, **k: None
    app_mod.messagebox.showerror = lambda *a, **k: saved.append(("error", a))
    try:
        class K:
            needs_setup = False

            def snapshot(self):
                return {"state": "online", "detail": "", "interval": 3,
                        "reject_remaining": 0}

            def save_settings(self, values):
                saved.append(values)

            def fetch_flux(self):
                return {}

            def force_reconnect(self): ...
            def on_change(self, cb): ...
            def save_credentials(self, *a): ...
            def test_credentials(self, *a):
                return True, "ok"

            def detect_portal(self):
                return ""

        win = Shell(K())
        win.attributes("-alpha", 0.0)
        win._adv_win.attributes("-alpha", 0.0)  # 高级小窗也隐身（测试不闪屏）
        win.update()
        # 高级设置按钮：开→可见，再点→隐藏（常驻窗关不丢内容）
        win._toggle_adv()
        win.update()
        assert win._adv_win.winfo_viewable(), "高级设置按钮应打开小窗"
        win._toggle_adv()
        win.update()
        assert not win._adv_win.winfo_viewable(), "再点应隐藏小窗"
        win.e_dns.delete(0, "end")
        win.e_dns.insert(0, "10.1.2.3")
        win.e_interval.delete(0, "end")
        win.e_interval.insert(0, "5")
        win._on_save_settings()
        assert saved and saved[0]["campus_dns"] == "10.1.2.3", saved
        assert saved[0]["interval"] == "5", saved
        # 非法数字应报错而不是崩
        saved.clear()
        win.e_interval.delete(0, "end")
        win.e_interval.insert(0, "abc")
        win._on_save_settings()
        assert saved and saved[0][0] == "error", "非法数字须弹错"
        win.destroy()
    finally:
        app_mod.messagebox.showinfo = orig_info
        app_mod.messagebox.showerror = orig_err


check("apply_config 热重载", t15_apply_config_hot_reload)
check("facade.save_settings 持久化+热载", t15_facade_save_settings)
check("设置页收集并校验", t15_shell_settings_page)

print("== 循环16：通用性阻断修复（第一性原理评估 3 件套）==")


def t16_probe_online_200_hijack():
    """200 劫持页（华为 AC 的 JS 挑战形态）不得判在线——漏检=绿着断网。"""
    import keeper as _k
    from keeper import DEFAULTS, Keeper
    cfg = dict(DEFAULTS)
    cfg.update({"username": "u", "password": "p"})
    k = Keeper(cfg)
    old = _k.physical_http
    try:
        _k.physical_http = lambda *a, **kw: (200, "<html>ac_portal pwdLogin</html>")
        assert k.probe_online() is False, "200 门户劫持页必须判离线"
        _k.physical_http = lambda *a, **kw: (200, "real internet content")
        assert k.probe_online() is True, "无门户指纹的 200 是真实到达"
        _k.physical_http = lambda *a, **kw: (302, "")
        assert k.probe_online() is False, "3xx 劫持判离线（原行为保持）"
    finally:
        _k.physical_http = old


def t16_online_substring_anchored():
    """"在线"子串反噬封堵："账号不在线，请勿重复登录"绝不能判成功。"""
    from keeper import Keeper
    ok, msg = Keeper.parse_login("{'success':false,'msg':'账号不在线，请勿重复登录'}")
    assert ok is False, (ok, msg)
    ok, msg = Keeper.parse_login("{'success':false,'msg':'您已在线'}")
    assert ok is True, (ok, msg)
    ok, msg = Keeper.parse_login("{'success':false,'msg':'当前不在线'}")
    assert ok is False, (ok, msg)


def t16_parse_login_english_success():
    """英文门户短响应 "Login success" 判成功——不得吃 600 秒硬退避。"""
    from keeper import Keeper
    ok, msg = Keeper.parse_login("Login success")
    assert ok is True, (ok, msg)
    ok, msg = Keeper.parse_login("Login failed, wrong password")
    assert ok is False, (ok, msg)


def t16_portal_target_dns_fallback():
    """守护链路补系统 DNS 回退（异校不改 campus_dns 也能跑通解析；
    解析的仍是用户填写的 host，凭据不改道）。"""
    import keeper as _k
    from keeper import DEFAULTS, Keeper
    cfg = dict(DEFAULTS)
    cfg.update({"username": "u", "password": "p",
                "server": "https://a.example.edu.cn", "campus_dns": "10.0.0.1"})
    k = Keeper(cfg)
    old_r, old_s = _k.campus_dns_resolve, _k.system_dns_resolve
    try:
        _k.campus_dns_resolve = lambda *a, **kw: ""   # 参考校 DNS 在他校不可达
        _k.system_dns_resolve = lambda h: "1.2.3.4"
        host, ip = k._portal_target()
        assert ip == "1.2.3.4", f"校园 DNS 失败应回退系统 DNS，实际 {ip!r}"
    finally:
        _k.campus_dns_resolve = old_r
        _k.system_dns_resolve = old_s


def t16_src_filter_covers_mihomo_default():
    """fake-ip/虚拟源过滤须覆盖 mihomo 默认段 198.18-199（"28."只是参考校现象）。"""
    from keeper import is_usable_src
    assert is_usable_src("192.0.2.10") is True
    assert is_usable_src("28.0.0.15") is False, "参考校 fake-ip 段"
    assert is_usable_src("198.18.0.5") is False, "mihomo 默认 fake-ip 段"
    assert is_usable_src("198.19.9.9") is False
    assert is_usable_src("127.0.0.1") is False
    assert is_usable_src("169.254.1.1") is False
    assert is_usable_src("") is False


def t16_lan_probe_is_dns_itself():
    """探测地址直接用 campus_dns 本身（UDP connect 不出包，选路精确到
    可达 DNS 的那张卡；旧 /16 推导在 /24 内网会选错默认路由）。
    取代循环12的 /16 推导断言（评估 P-3：那是参考校 /16 观测的普适化）。
    通用化后未配 DNS 回落通用公网地址（不绑定任何学校内网段）。"""
    import keeper as _k
    from keeper import lan_probe_address as L
    assert L("10.0.0.1") == "10.0.0.1"
    assert L("192.168.1.1") == "192.168.1.1"
    assert L("") == _k.LAN_PROBE_FALLBACK      # 未配置 → 通用公网地址
    assert L("not-an-ip") == _k.LAN_PROBE_FALLBACK


check("200 劫持页判离线", t16_probe_online_200_hijack)
check("在线子串锚定封堵", t16_online_substring_anchored)
check("英文 success 判成功", t16_parse_login_english_success)
check("守护链路 DNS 回退", t16_portal_target_dns_fallback)
check("源地址过滤覆盖 mihomo 段", t16_src_filter_covers_mihomo_default)
check("探测地址=DNS 本身", t16_lan_probe_is_dns_itself)

print("== 循环17：通用性 A（去参考校默认值）==")


def t17_defaults_are_school_neutral():
    """默认配置不得绑定参考校专属地址：server 预填、校园 DNS、门户 IP 均须通用。"""
    from keeper import DEFAULTS
    assert DEFAULTS["server"] == "", "默认 server 应留空（不预填任何学校域名）"
    assert DEFAULTS["campus_dns"] == "", "未配校园 DNS 时应留空→走系统 DNS 回退"
    assert "10.0.0.1" not in str(DEFAULTS.get("probe_ips", "")), "锚点不应写参考校门户 IP"


def t17_lan_probe_no_school_fallback():
    """campus_dns 未配置时，选路探测地址须是通用公网地址而非任何学校内网段。"""
    import keeper as _k
    from keeper import lan_probe_address as L
    v = L("")
    assert v == _k.LAN_PROBE_FALLBACK, f"空 DNS 应回落通用公网地址，实际 {v!r}"
    assert v, "仍须返回一个可用于 UDP connect 选路的地址"


def t17_probe_login_uses_system_dns_when_no_campus_dns():
    """未配校园 DNS 时，凭据探针应走系统 DNS 解析用户填的 host（不回落参考校 DNS）。"""
    import keeper as _k
    seen = {}

    def fake_resolve(host, dns_ip, bind_ip="", timeout=3.0):
        seen["dns_ip"] = dns_ip
        return ""

    old_cdr, old_sdr, old_disc, old_http = (
        _k.campus_dns_resolve, _k.system_dns_resolve,
        _k.discover_physical_ip, _k.physical_http)
    try:
        _k.campus_dns_resolve = fake_resolve
        _k.system_dns_resolve = lambda h: "203.0.113.9"
        _k.discover_physical_ip = lambda dns="": "192.0.2.7"
        _k.physical_http = lambda *a, **kw: (200, "{'success':true}")
        cfg = dict(_k.DEFAULTS, server="https://portal.example.edu", campus_dns="")
        ok, _ = _k.Keeper.probe_login(cfg, "u", "p")
        assert ok is True
        assert seen.get("dns_ip", "") == "", \
            f"campus_dns 为空不应回落到任何学校专属 DNS，实际 {seen.get('dns_ip')!r}"
    finally:
        _k.campus_dns_resolve, _k.system_dns_resolve = old_cdr, old_sdr
        _k.discover_physical_ip, _k.physical_http = old_disc, old_http


def t17_wizard_no_school_prefill():
    """首配向导初始服务器框不得预填任何学校域名（由自动探测/手填决定）。"""
    import tkinter as tk
    from shell.app import Shell

    class K:
        needs_setup = True
        def snapshot(self):
            return {"state": "starting", "detail": "", "interval": 3, "reject_remaining": 0}
        def fetch_flux(self): return {}
        def force_reconnect(self): ...
        def on_change(self, cb): ...
        def save_credentials(self, *a): ...
        def test_credentials(self, *a): return True, "ok"
        def detect_portal(self): return ""   # 探测不到，保持初始态
        def get_settings(self): return {}
        def save_settings(self, v): ...

    try:
        win = Shell(K())
    except tk.TclError:
        raise _Skip("无桌面会话，向导 UI 用例不适用")
    try:
        val = win.e_server.get()
        assert val == "", f"向导服务器框初始应为空，实际 {val!r}"
    finally:
        win.destroy()


def t17_campus_dns_resolve_empty_does_not_send():
    """campus_dns 为空时不得向任何写死学校 DNS 发包（应直接走系统 DNS 回退）。"""
    import keeper as _k
    sent = []
    old_socket = _k.socket.socket

    class FakeSock:
        def settimeout(self, *a): ...
        def bind(self, *a): ...
        def sendto(self, q, addr): sent.append(addr)
        def recvfrom(self, *a): raise OSError("no dns")
        def close(self): ...

    try:
        _k.socket.socket = lambda *a, **kw: FakeSock()
        r = _k.campus_dns_resolve("portal.example.edu", "", "192.0.2.7")
        assert r == ""
        assert not sent, f"空 DNS 不应发起校园 DNS 查询，实际发往 {sent!r}"
    finally:
        _k.socket.socket = old_socket


check("默认值不含参考校专属地址", t17_defaults_are_school_neutral)
check("空 DNS 选路不回落参考校段", t17_lan_probe_no_school_fallback)
check("凭据探针无校园 DNS 走系统 DNS", t17_probe_login_uses_system_dns_when_no_campus_dns)
check("向导不预填参考校域名", t17_wizard_no_school_prefill)
check("空校园 DNS 不发包", t17_campus_dns_resolve_empty_does_not_send)

print("== 循环18：通用性 B（端点可配 + 旁路同构）==")


def _cfg(**over):
    from keeper import DEFAULTS
    c = dict(DEFAULTS)
    c.update({"username": "u", "password": "p", "server": "https://portal.example.edu",
              "campus_dns": "10.0.0.1", "bind_ip": "192.0.2.7"})
    c.update(over)
    return c


def t18_login_path_configurable():
    """改 login_path 后，登录 POST 必须打到新端点（行为：请求目标 path）。"""
    import keeper as _k
    paths = []
    old_http, old_dns = _k.physical_http, _k.campus_dns_resolve
    try:
        _k.campus_dns_resolve = lambda *a, **kw: "203.0.113.5"
        def fake_http(host, ip, path, **kw):
            paths.append(path)
            return 200, "{'success':true}"
        _k.physical_http = fake_http
        k = _k.Keeper(_cfg(login_path="/custom/auth.cgi"))
        ok, _ = k.do_login()
        assert ok is True
        assert paths and paths[-1] == "/custom/auth.cgi", \
            f"登录端点应取 login_path，实际 {paths!r}"
    finally:
        _k.physical_http, _k.campus_dns_resolve = old_http, old_dns


def t18_flux_path_configurable():
    """改 flux_path 后，查流量必须打到新端点。"""
    import keeper as _k
    paths = []
    old_http, old_dns = _k.physical_http, _k.campus_dns_resolve
    try:
        _k.campus_dns_resolve = lambda *a, **kw: "203.0.113.5"
        def fake_http(host, ip, path, **kw):
            paths.append(path)
            return 200, "<tr><td>已用</td><td>1GB</td></tr>"
        _k.physical_http = fake_http
        k = _k.Keeper(_cfg(flux_path="/custom/usage"))
        k.fetch_flux()
        assert paths and paths[-1] == "/custom/usage", \
            f"流量端点应取 flux_path，实际 {paths!r}"
    finally:
        _k.physical_http, _k.campus_dns_resolve = old_http, old_dns


def t18_probe_path_configurable():
    """改 probe_path 后，在线探测必须请求新路径。"""
    import keeper as _k
    paths = []
    old_http = _k.physical_http
    try:
        def fake_http(host, ip, path, **kw):
            paths.append(path)
            return 404, ""      # 真实到达公网 = 在线
        _k.physical_http = fake_http
        k = _k.Keeper(_cfg(probe_path="/custom_204"))
        assert k.probe_online() is True
        assert paths and paths[0] == "/custom_204", \
            f"探测端点应取 probe_path，实际 {paths!r}"
    finally:
        _k.physical_http = old_http


def t18_probe_login_shares_cfg_paths():
    """向导/测试链路的 probe_login 必须与主链路读同一套端点配置。"""
    import keeper as _k
    paths = []
    old_http, old_dns, old_disc = _k.physical_http, _k.campus_dns_resolve, _k.discover_physical_ip
    try:
        _k.campus_dns_resolve = lambda *a, **kw: "203.0.113.5"
        _k.discover_physical_ip = lambda dns="": "192.0.2.7"
        def fake_http(host, ip, path, **kw):
            paths.append(path)
            return 200, "{'success':true}"
        _k.physical_http = fake_http
        cfg = _cfg(login_path="/custom/auth.cgi")
        ok, _ = _k.Keeper.probe_login(cfg, "u", "p")
        assert ok is True
        assert paths and paths[-1] == "/custom/auth.cgi", \
            f"probe_login 应读 cfg 的 login_path，实际 {paths!r}"
    finally:
        _k.physical_http, _k.campus_dns_resolve = old_http, old_dns
        _k.discover_physical_ip = old_disc


def t18_detect_portal_uses_cfg_anchors():
    """自动探测必须使用调用方传入的 probe_ips，不回落内置参考校锚点。"""
    import keeper as _k
    seen = []

    def fake_http(host, ip, path, **kw):
        seen.append(ip)
        return 204, {}, ""
    got = _k.detect_portal(probe_ips="198.51.100.7", campus_dns="10.0.0.1",
                           login_page="/ac_portal/pc.html", _http=fake_http)
    assert got == ""
    assert seen == ["198.51.100.7"], f"应只用传入锚点，实际 {seen!r}"


check("登录端点可配", t18_login_path_configurable)
check("流量端点可配", t18_flux_path_configurable)
check("探测端点可配", t18_probe_path_configurable)
check("probe_login 与主链路同构", t18_probe_login_shares_cfg_paths)
check("自动探测用传入锚点", t18_detect_portal_uses_cfg_anchors)

print("== 循环19：通用性 B3/C（响应词表可配 + 兼容性自检）==")


def t19_response_marks_defaults_keep_behavior():
    """默认词表必须精确等于现有参考校词典（向后兼容，不偷偷改判定结果）。"""
    from keeper import RESPONSE_DEFAULTS
    assert "noauth" in RESPONSE_DEFAULTS["transient_marks"]
    assert "not taken" in RESPONSE_DEFAULTS["transient_marks"]
    assert "ac_portal" in RESPONSE_DEFAULTS["hijack_marks"]
    assert "成功" in RESPONSE_DEFAULTS["success_marks"]


def t19_response_marks_configurable():
    """异校在 [response] 段改词典后，判定结果随之变化（可配且真被消费）。"""
    import keeper as _k
    # 一段不含任何内置标记的短响应：默认判"无法判定/拒绝"
    text = "authed_ok=1 welcome"
    default_ok, _ = _k.Keeper.parse_login(text)
    assert default_ok is False, "默认词典下这段响应不应判成功"
    cfg = dict(_k.DEFAULTS)
    cfg["response.success_marks"] = "authed_ok,welcome"
    # 注入点：parse_login 接受 cfg（第 2 个可选参数），不改静态默认
    ok, _ = _k.Keeper.parse_login(text, cfg)
    assert ok is True, "配了 success_marks 后同一段响应应判成功"


def t19_transient_marks_configurable():
    """自定义瞬态标记须被 handle_rejection 采用（异校 NOAUTH 类错误短退避）。"""
    import keeper as _k
    cfg = _cfg(**{"response.transient_marks": "portallag,busy"})
    k = _k.Keeper(cfg)
    k.handle_rejection("PortalLag: retry later")
    assert k.reject_until > 0
    # 瞬态 = 短退避（< 硬退避 600）
    assert k.reject_until - __import__("time").monotonic() < 600, \
        "自定义瞬态标记应走短退避"


def t19_compat_check_offline():
    """兼容性自检：喂一段响应样本，离线给出判定与命中规则（不联网）。"""
    from keeper import compat_check
    r = compat_check("{'success':true,'msg':'ok'}")
    assert r["verdict"] == "success", r
    assert r["rule"], "应给出命中的判定规则名"
    r2 = compat_check("Task 'NOAUTH:*' not taken")
    assert r2["verdict"] == "reject", r2
    assert r2["transient"] is True, "NOAUTH 应识别为瞬态（可重试）"


def t19_compat_check_custom_marks():
    """自检须支持传入自定义词典：异校可验证自己的响应能否被正确判定。"""
    from keeper import compat_check
    cfg = {"response.success_marks": "authed_ok,welcome"}
    r = compat_check("authed_ok=1 welcome", cfg)
    assert r["verdict"] == "success", r


check("默认词表保持既有行为", t19_response_marks_defaults_keep_behavior)
check("成功词表可配", t19_response_marks_configurable)
check("瞬态词表可配", t19_transient_marks_configurable)
check("兼容性自检离线可用", t19_compat_check_offline)
check("兼容性自检支持自定义词表", t19_compat_check_custom_marks)


def t19_compat_cli():
    """独立自检 CLI：喂响应文本，离线打印判定结论（异校自查入口）。"""
    out = subprocess.run(
        [sys.executable, "-I", str(ROOT / "compat_check.py"), "{'success':true}"],
        capture_output=True, text=True, timeout=30)
    assert out.returncode == 0, out.stderr
    assert "success" in out.stdout.lower(), out.stdout


def t19_compat_cli_reads_response_ini():
    """自检 CLI 须读 [response] 段：异校配好词表后，样本判定随之改变。"""
    import tempfile
    d = Path(tempfile.mkdtemp())
    appdir = d / "campus-net-autologin"
    appdir.mkdir(parents=True, exist_ok=True)
    ini = appdir / "config.ini"
    ini.write_text("[response]\nsuccess_marks = authed_ok\n", encoding="utf-8")
    env = dict(os.environ, LOCALAPPDATA=str(d))
    out = subprocess.run(
        [sys.executable, "-I", str(ROOT / "compat_check.py"), "authed_ok=1"],
        capture_output=True, text=True, timeout=30, env=env)
    assert out.returncode == 0, out.stderr
    assert "success" in out.stdout.lower(), out.stdout


check("兼容性自检 CLI 可用", t19_compat_cli)
check("自检 CLI 读取 [response] 配置", t19_compat_cli_reads_response_ini)

print("== 循环20：实用性 P1（凭据安全 + 空密码语义）==")


def _live_keeper(tmp):
    """构造运行态 Keeper 并返回 (facade, keeper, 哨兵密码)。

    哨兵密码运行时随机生成，模拟「从 keyring 还原的真实密码」；ini 里始终
    写 <keyring> 占位（不是可用凭据）。源码/测试不含任何凭据字面量。
    """
    import secrets
    import keeper as _k
    import app_main
    import shell.keeper_paths as _kp
    sentinel = "sentinel-" + secrets.token_urlsafe(12)
    tmp.write_text("[keeper]\nusername = u\npassword = <keyring>\n", encoding="utf-8")
    old_cfg, old_p = _k.CONFIG_FILE, _kp.CONFIG_FILE
    _k.CONFIG_FILE, _kp.CONFIG_FILE = tmp, tmp
    try:
        live = dict(_k.DEFAULTS, username="u", password=sentinel,
                    server="https://portal.example.edu")
        k = _k.Keeper(live)
        yield app_main.KeeperFacade(k), k, sentinel, old_cfg, old_p
    finally:
        _k.CONFIG_FILE, _kp.CONFIG_FILE = old_cfg, old_p


def t20_save_settings_no_plaintext_leak():
    """点「保存设置」不得把 keyring 还原的真实密码明文写回 ini（P0 安全）。"""
    import tempfile
    tmp = Path(tempfile.mkdtemp()) / "config.ini"
    for f, k, sentinel, _, _ in _live_keeper(tmp):
        f.save_settings({"interval": "5"})
        text = tmp.read_text(encoding="utf-8")
        assert sentinel not in text, "真实密码被明文写回 ini（凭据泄露）"
        assert "<keyring>" in text, "应保留 <keyring> 占位符"


def t20_save_settings_keeps_manual_ini_edits():
    """保存设置页不得用旧快照覆盖用户手工改的 ini 键。"""
    import tempfile
    tmp = Path(tempfile.mkdtemp()) / "config.ini"
    for f, k, sentinel, _, _ in _live_keeper(tmp):
        # 模拟用户手工编辑 ini（运行态快照里没有这个改动）
        text = tmp.read_text(encoding="utf-8").replace(
            "password = <keyring>", "password = <keyring>\nprobe_timeout = 9")
        tmp.write_text(text, encoding="utf-8")
        f.save_settings({"interval": "5"})
        out = tmp.read_text(encoding="utf-8")
        assert "probe_timeout = 9" in out, "手工改的键被旧快照回滚了"


def t20_blank_password_not_overwrite():
    """密码留空保存：不改已存密码（运行态/表单都不丢），也不写空密码进 ini。"""
    import tempfile
    tmp = Path(tempfile.mkdtemp()) / "config.ini"
    for f, k, sentinel, _, _ in _live_keeper(tmp):
        f.save_credentials("u", "", "https://new.example.edu")
        assert k.cfg["password"] == sentinel, "运行态密码不应被清空"
        assert k.form["pwd"] == sentinel, "登录表单不应丢密码"
        text = tmp.read_text(encoding="utf-8")
        assert "<keyring>" in text and sentinel not in text, "ini 应仍是占位"
        assert "https://new.example.edu" in text, "server 应正常更新"


def t20_blank_password_test_uses_stored():
    """密码留空点「测试」时，用已存密码去测，不因空密码被门户拒绝。"""
    import keeper as _k
    import tempfile
    tmp = Path(tempfile.mkdtemp()) / "config.ini"
    seen = {}
    old_probe = _k.Keeper.probe_login
    try:
        def fake_probe(cfg, user, pwd):
            seen["pwd"] = pwd
            return True, "ok"
        _k.Keeper.probe_login = staticmethod(fake_probe)
        for f, k, sentinel, _, _ in _live_keeper(tmp):
            ok, _ = f.test_credentials("u", "", "https://portal.example.edu")
            assert ok is True
            assert seen["pwd"] == sentinel, \
                f"空密码测试应使用已存密码，实际 {seen['pwd']!r}"
    finally:
        _k.Keeper.probe_login = old_probe


def t20_response_keys_not_written_into_keeper_section():
    """[response] 词表键不得被 save_settings 写进 [keeper] 段（段语义隔离）。"""
    import tempfile
    tmp = Path(tempfile.mkdtemp()) / "config.ini"
    for f, k, sentinel, _, _ in _live_keeper(tmp):
        tmp.write_text("[keeper]\nusername = u\npassword = <keyring>\n"
                       "[response]\nsuccess_marks = authed_ok\n", encoding="utf-8")
        f.save_settings({"interval": "5"})
        keeper_section = tmp.read_text(encoding="utf-8").split("[response]")[0]
        assert "response." not in keeper_section, \
            "response.* 键不该出现在 [keeper] 段"
        assert "[response]" in tmp.read_text(encoding="utf-8"), "[response] 段应保留"


check("保存设置不泄露明文密码", t20_save_settings_no_plaintext_leak)
check("保存设置不覆盖手工 ini 编辑", t20_save_settings_keeps_manual_ini_edits)
check("空密码不覆盖已存凭据", t20_blank_password_not_overwrite)
check("空密码测试用已存密码", t20_blank_password_test_uses_stored)
check("response 键不混入 keeper 段", t20_response_keys_not_written_into_keeper_section)

print("== 循环21：实用性 P1（首跑健壮性 + 降级空转）==")


def t21_wizard_crash_does_not_kill_startup():
    """无桌面会话构造/运行向导抛 TclError 时，main 不崩溃且给出可读日志。"""
    import app_main
    import tkinter as tk
    from unittest.mock import patch
    calls = {"warn": []}

    def boom(*a, **kw):
        raise tk.TclError("no display name and no $DISPLAY")

    log = app_main.logging.getLogger("keeper")
    with patch.object(app_main, "_effective_config", return_value=({}, False)), \
         patch.object(app_main, "acquire_single_instance", return_value=True), \
         patch.object(app_main, "setup_logging", lambda: None), \
         patch("shell.app.Shell", side_effect=boom), \
         patch.object(log, "warning", side_effect=lambda *a, **kw: calls["warn"].append(a)):
        old_stdin = app_main.sys.stdin
        try:
            app_main.sys.stdin = type("S", (), {"isatty": lambda self: False})()
            app_main.main()  # 不得抛出
        finally:
            app_main.sys.stdin = old_stdin
    assert calls["warn"], "向导异常应记 warning 日志（pythonw 下用户唯一可见线索）"


def t21_offline_save_still_persists():
    """测试凭据失败时，用户选择「仍要保存」后凭据必须落地（P1-2 离线可配）。"""
    import tkinter as tk
    from unittest.mock import patch
    from shell.app import Shell

    saved = []

    class K:
        needs_setup = True
        def snapshot(self):
            return {"state": "starting", "detail": "", "interval": 3, "reject_remaining": 0}
        def fetch_flux(self): return {}
        def force_reconnect(self): ...
        def on_change(self, cb): ...
        def save_credentials(self, u, p, s): saved.append((u, p, s))
        def test_credentials(self, u, p, s): return False, "门户解析失败（疑似不在校园网）"
        def detect_portal(self): return ""
        def get_settings(self): return {}
        def save_settings(self, v): ...

    try:
        win = Shell(K())
    except tk.TclError:
        raise _Skip("无桌面会话，向导 UI 用例不适用")
    try:
        win.e_user.insert(0, "u")
        win.e_pwd.insert(0, "p")
        win.e_server.insert(0, "https://portal.example.edu")
        with patch("shell.app.messagebox.askyesno", return_value=True), \
             patch("shell.app.messagebox.showerror"), \
             patch("shell.app.messagebox.showinfo"):
            win._on_save_test()
            # 后台线程跑测试；阻塞等结果（不调 update/after，避免嵌套事件循环）
            ok, msg = win._test_q.get(timeout=5)
            win._finish_save_test(ok, msg)
        assert saved, "测试失败但选了仍要保存，凭据应落地"
    finally:
        # 向导态保存成功后窗口已 destroy，重复销毁会抛 TclError
        try:
            win.destroy()
        except tk.TclError:
            pass


def t21_fallback_run_does_not_spin_once():
    """经 main() 降级路径：进入第二次 keeper.run() 时 stop_evt 必须是清空的，
    否则 while not stop_evt.is_set() 立即为假 → 探测一次就退出（P1-3）。"""
    import app_main
    from unittest.mock import patch
    import threading as _th

    states = []

    class FakeKeeper:
        def __init__(self):
            self.stop_evt = _th.Event()
            self.force_evt = _th.Event()
            self.interval = 3
            self.extra_wait = 0.0
        def run(self):
            # 记录「进入 run 时停止标志是否已置位」——这是降级能否持续的关键
            states.append(self.stop_evt.is_set())
            if len(states) >= 2:
                raise RuntimeError("stop")  # 避免真跑第二次循环
        def request_reconnect(self): ...

    class FakeTray:
        def __init__(self, k): self.icon = self
        def run(self, setup=None): raise RuntimeError("no desktop")

    fk = FakeKeeper()
    log = app_main.logging.getLogger("keeper")
    with patch.object(app_main, "setup_logging", lambda: None), \
         patch.object(app_main, "acquire_single_instance", return_value=True), \
         patch.object(app_main, "_effective_config", return_value=({}, True)), \
         patch.object(app_main, "assemble", return_value=(fk, FakeTray(fk), None)), \
         patch.object(log, "warning", lambda *a, **kw: None):
        try:
            app_main.main()
        except RuntimeError:
            pass
    # 第一次（kt 线程）进入时 stop 未置位；降级第二次进入时必须也未置位
    assert len(states) >= 2, f"降级路径未重新进入 run，记录={states!r}"
    assert states[1] is False, \
        f"降级后 stop_evt 仍置位会导致一次探测即退出，实际 {states!r}"


check("向导异常不击穿启动", t21_wizard_crash_does_not_kill_startup)
check("离线可选仍要保存", t21_offline_save_still_persists)
check("降级后持续守护不空转", t21_fallback_run_does_not_spin_once)

print("== 循环22：实用性 P2（UI 不冻结）==")


def t22_test_credentials_runs_off_ui_thread():
    """点「保存并测试」时，慢速网络请求在后台线程跑，UI 线程立即返回。"""
    import tkinter as tk
    import threading
    from shell.app import Shell

    where = {}

    class K:
        needs_setup = False
        def snapshot(self):
            return {"state": "starting", "detail": "", "interval": 3, "reject_remaining": 0}
        def fetch_flux(self): return {}
        def force_reconnect(self): ...
        def on_change(self, cb): ...
        def save_credentials(self, *a): ...
        def test_credentials(self, u, p, s):
            where["thread"] = threading.current_thread().name
            return True, "ok"
        def detect_portal(self): return ""
        def get_settings(self): return {}
        def save_settings(self, v): ...

    try:
        win = Shell(K())
    except tk.TclError:
        raise _Skip("无桌面会话，UI 用例不适用")
    try:
        win.e_user.insert(0, "u")
        win.e_pwd.insert(0, "p")
        win.e_server.insert(0, "https://portal.example.edu")
        ui_thread = threading.current_thread().name
        win._on_save_test()  # 立即返回（不阻塞）
        # 结果由后台线程入队；阻塞取回后手动 drain，不触发 after 递归
        ok, msg = win._test_q.get(timeout=5)
        with __import__("unittest").mock.patch("shell.app.messagebox.showinfo"):
            win._finish_save_test(ok, msg)
        assert where.get("thread") and where["thread"] != ui_thread, \
            "凭据测试应在后台线程执行，实际在 UI 线程（窗口会冻结）"
    finally:
        try:
            win.destroy()
        except tk.TclError:
            pass


check("凭据测试不阻塞 UI 线程", t22_test_credentials_runs_off_ui_thread)

print("== 循环23：实用性 P2（自启开关 + 网卡选择）==")


def t23_unregister_failure_reported():
    """unregister 返回 False 时不得记「已关闭」（P2-6 吞返回值）。"""
    import keeper as _k
    import types
    from unittest.mock import patch
    logged = []
    fake = types.SimpleNamespace(
        is_enabled=lambda: True,
        unregister=lambda: False,
        register=lambda: True)
    tray = _k.Tray.__new__(_k.Tray)  # 空壳：不构造真实 pystray 图标
    tray.keeper = types.SimpleNamespace(log=types.SimpleNamespace(
        info=lambda *a: logged.append(("info", a)),
        warning=lambda *a: logged.append(("warning", a))))
    icon = types.SimpleNamespace(update_menu=lambda: None)
    with patch.dict(sys.modules, {"autostart": fake}):
        tray.on_autostart(icon, object())
    joined = " ".join(str(a) for _, a in logged)
    assert "关闭失败" in joined, f"应报告关闭失败，实际日志：{joined!r}"
    assert "已关闭" not in joined, "不能谎报已关闭"


def t23_virtual_nic_excluded_from_fallback():
    """回退枚举网卡时，虚拟网卡（VMware/WSL/Hyper-V/TAP）不得被选中。"""
    import keeper as _k
    import types
    from unittest.mock import patch
    AF = __import__("socket").AF_INET

    class Addr:
        def __init__(self, ip): self.family, self.address = AF, ip

    fake_addrs = {
        "VMware Network Adapter VMnet1": [Addr("192.168.100.1")],
        "vEthernet (WSL)": [Addr("172.20.0.1")],
        "Ethernet 2": [Addr("10.20.30.40")],   # 真物理网卡
    }
    fake_psutil = types.SimpleNamespace(net_if_addrs=lambda: fake_addrs)
    # 让主路径 UDP connect 失败，强制走回退枚举
    old_socket = _k.socket.socket
    try:
        _k.socket.socket = lambda *a, **kw: (_ for _ in ()).throw(OSError("no route"))
        with patch.dict(sys.modules, {"psutil": fake_psutil}):
            got = _k.discover_physical_ip("")
        assert got == "10.20.30.40", f"应选物理网卡，实际 {got!r}"
    finally:
        _k.socket.socket = old_socket


def t23_bind_ip_input_validation():
    """bind_ip 为 IPv6/非法值时须被拒绝（P2-9 静默全瘫），合法 IPv4 放行。"""
    from keeper import validate_bind_ip
    assert validate_bind_ip("") is True, "留空（自动发现）应放行"
    assert validate_bind_ip("10.20.30.40") is True, "合法 IPv4 应放行"
    assert validate_bind_ip("fe80::1") is False, "IPv6 应拒绝（_raw_http 只走 AF_INET）"
    assert validate_bind_ip("999.1.1.1") is False, "非法 IPv4 应拒绝"
    assert validate_bind_ip("not-an-ip") is False, "非 IP 文本应拒绝"


check("自启关闭失败如实上报", t23_unregister_failure_reported)
check("回退枚举排除虚拟网卡", t23_virtual_nic_excluded_from_fallback)
check("bind_ip 非法值被拒", t23_bind_ip_input_validation)

print("== 循环24：实用性 P2/P3（错误信息 + 凭据降级可见性）==")


def t24_raw_http_error_has_context():
    """底层网络错误不只报异常类名，应带 host/path 便于自救（P2-13）。"""
    import keeper as _k
    old = _k.socket.socket
    try:
        _k.socket.socket = lambda *a, **kw: (_ for _ in ()).throw(OSError("boom"))
        status, _, text = _k._raw_http("portal.example.edu", "1.2.3.4",
                                       "/ac_portal/login.php", timeout=1)
        assert status == 0
        assert "portal.example.edu" in text, f"错误应含 host，实际 {text!r}"
        assert "/ac_portal/login.php" in text, f"错误应含 path，实际 {text!r}"
    finally:
        _k.socket.socket = old


def t24_keyring_transient_failure_keeps_existing():
    """keyring 瞬时异常时 load_password 返回空会误触发首跑向导；
    应至少记录一条日志（P2-11 零日志丢回向导）。"""
    import shell.credentials as _c
    import shell.keeper_paths as _kp
    import logging
    import tempfile
    records = []
    # 隔离真实 %LOCALAPPDATA% 配置：测试不得碰用户数据
    tmp = Path(tempfile.mkdtemp()) / "config.ini"
    old_p = _kp.CONFIG_FILE
    _kp.CONFIG_FILE = tmp

    class Handler(logging.Handler):
        def emit(self, rec): records.append(rec.getMessage())

    log = logging.getLogger("keeper")
    h = Handler(); log.addHandler(h); old_level = log.level
    log.setLevel(logging.WARNING)
    try:
        import keyring as _kr
        old = _kr.get_password
        _kr.get_password = lambda *a: (_ for _ in ()).throw(RuntimeError("backend glitch"))
        try:
            _c._keyring = lambda: _kr
            _c.load_password("someuser")
        finally:
            _kr.get_password = old
            _c._keyring = lambda: __import__("keyring")
    finally:
        log.removeHandler(h); log.setLevel(old_level)
        _kp.CONFIG_FILE = old_p
    assert any("keyring" in m.lower() or "凭据" in m for m in records), \
        f"keyring 异常应留日志线索，实际 {records!r}"


check("底层错误带 host/path 上下文", t24_raw_http_error_has_context)
check("keyring 瞬时故障留日志", t24_keyring_transient_failure_keeps_existing)


def t24_bind_ip_rediscovered_on_portal_down():
    """网络切换后旧 bind_ip 失效：portal_down 周期里应重发现物理源（P2-8）。"""
    import keeper as _k
    k = _k.Keeper(_cfg(bind_ip=""))      # 自动发现模式（cfg 未手动指定）
    k.bind_ip = "192.0.2.7"              # 模拟启动时自动发现的旧源已失效
    seen = {"n": 0}

    def fake_discover(dns=""):
        seen["n"] += 1
        return "198.51.100.9"   # 新网卡
    old_disc = _k.discover_physical_ip
    old_alive = k.portal_alive
    old_online = k.probe_online
    try:
        _k.discover_physical_ip = fake_discover
        k.probe_online = lambda: False
        k.portal_alive = lambda: False   # 模拟旧源失效 → 门户不可达
        k.portal_down_wait = 0.01
        k.tick()
        assert k.bind_ip == "198.51.100.9", \
            f"portal_down 应触发重发现，实际 bind_ip={k.bind_ip!r}"
        assert seen["n"] >= 1
        # 节流：同一周期内不重复重发现（显式设置时刻，断言不依赖两次 tick 的真实间隔）
        k._bind_retry_at = __import__("time").monotonic()
        k.tick()
        assert seen["n"] == 1, f"重发现应节流，实际调用 {seen['n']} 次"
    finally:
        _k.discover_physical_ip = old_disc
        k.portal_alive = old_alive
        k.probe_online = old_online


check("portal_down 重发现绑定源", t24_bind_ip_rediscovered_on_portal_down)

print("== 循环25：协议指纹（异校适用性三态判定）==")


def t25_fp_plain_ac_portal():
    """朴素 ac_portal 登录页（明文密码、无 auth_tag）→ 判本工具适用。"""
    from keeper import protocol_fingerprint
    page = ('<form action="/ac_portal/login.php" method="post">'
            '<input name="opr" value="pwdLogin">'
            '<input name="userName"><input name="pwd"><input name="rememberPwd"></form>')
    r = protocol_fingerprint(page)
    assert r["family"] == "ac_portal", r
    assert r["verdict"] == "compatible", r
    assert r["needs_auth_tag"] is False and r["pwd_encrypted"] is False, r
    assert r["advice"], "必须给出下一步建议"


def t25_fp_auth_tag_variant():
    """带 auth_tag 一次性令牌的 ac_portal（深信服系常见变体）→ 判暂不支持。"""
    from keeper import protocol_fingerprint
    page = ('ac_portal login data={"opr":"pwdLogin","userName":"u","pwd":pwd,'
            '"auth_tag":rckey,"rememberPwd":1}')
    r = protocol_fingerprint(page)
    assert r["family"] == "ac_portal", r
    assert r["verdict"] == "variant_unsupported", r
    assert r["needs_auth_tag"] is True, r


def t25_fp_encrypted_pwd_variant():
    """登录前对密码做哈希/加密的 ac_portal → 判暂不支持（明文提交无效）。"""
    from keeper import protocol_fingerprint
    page = ('ac_portal/login.php opr=pwdLogin '
            'var h = hex_md5(password); pwd.value = h;')
    r = protocol_fingerprint(page)
    assert r["family"] == "ac_portal", r
    assert r["verdict"] == "variant_unsupported", r
    assert r["pwd_encrypted"] is True, r


def t25_fp_other_families():
    """eportal（Dr.COM/锐捷）/ 深澜 Srun / 无特征 → 各判其族，不冒充 ac_portal。"""
    from keeper import protocol_fingerprint
    r = protocol_fingerprint("POST /eportal/InterFace.do?method=login queryString userId")
    assert r["family"] == "eportal" and r["verdict"] == "other_protocol", r
    r2 = protocol_fingerprint("srun_portal_pc.php?ac_id=1 action=login username password")
    assert r2["family"] == "srun" and r2["verdict"] == "other_protocol", r2
    r3 = protocol_fingerprint("<html>欢迎访问本校网络</html>")
    assert r3["family"] == "unknown" and r3["verdict"] == "unknown", r3


def t25_cli_detect_mode():
    """自检 CLI --detect 模式：喂登录页样本，离线给出协议族与适用性结论。"""
    out = subprocess.run(
        [sys.executable, "-I", str(ROOT / "compat_check.py"), "--detect",
         '<form action="/ac_portal/login.php">opr=pwdLogin userName pwd rememberPwd</form>'],
        capture_output=True, text=True, timeout=30)
    assert out.returncode == 0, out.stderr
    assert "ac_portal" in out.stdout, out.stdout
    assert "compatible" in out.stdout, out.stdout


def t25_cli_detect_other_protocol():
    """--detect 对非 ac_portal 样本明确说「不适用」，不让用户瞎配。"""
    out = subprocess.run(
        [sys.executable, "-I", str(ROOT / "compat_check.py"), "--detect",
         "POST /eportal/InterFace.do?method=login queryString userId"],
        capture_output=True, text=True, timeout=30)
    assert out.returncode == 0, out.stderr
    assert "other_protocol" in out.stdout and "不适用" in out.stdout, out.stdout


check("指纹：朴素 ac_portal 判适用", t25_fp_plain_ac_portal)
check("指纹：auth_tag 变体判暂不支持", t25_fp_auth_tag_variant)
check("指纹：密码加密变体判暂不支持", t25_fp_encrypted_pwd_variant)
check("指纹：异协议各判其族", t25_fp_other_families)
check("指纹 CLI --detect 可用", t25_cli_detect_mode)
check("指纹 CLI 异协议明确说不适用", t25_cli_detect_other_protocol)

print("== 循环26：模拟门户端到端 + 失败现场自诊断 ==")


def _close_sim(srv):
    """关闭模拟门户：shutdown 只停 accept 循环，不关监听套接字——Windows
    上残留的僵尸监听器会把后续同端口测试的连接拖进死队列（超时误判
    portal_down，本仓实测串扰事故）。统一走这里，别处不得裸 shutdown。"""
    srv.shutdown()
    srv.server_close()


def _sim_portal(dialect_body: str, dialect_ok: bool, port: int = 80):
    """本地假门户（127.0.0.1，行为符合 ac_portal 公开规范）：
    未认证时 204 探测被 302 劫持 → 登录页可达 → POST login.php 按方言应答。
    端口占用/无权限 → 返回 None（用例 SKIP，保持测试零副作用承诺）。"""
    import http.server
    import threading
    seen = {"paths": []}
    state = {"online": False}

    class H(http.server.BaseHTTPRequestHandler):
        def log_message(self, *a): pass  # 静音

        def _reply(self, code, body=b"", headers=None):
            self.send_response(code)
            for k, v in (headers or {}).items():
                self.send_header(k, v)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            if body:
                self.wfile.write(body)

        def do_GET(self):
            seen["paths"].append("GET " + self.path)
            if self.path.startswith("/generate_204"):
                if state["online"]:
                    self._reply(204)   # 已放行：真实到达"公网"
                else:
                    self._reply(302, headers={"Location": "http://127.0.0.1/ac_portal/pc.html"})
            elif self.path.startswith("/ac_portal/default/pc.html"):
                self._reply(200, b"<html>ac_portal pwdLogin</html>")
            else:
                self._reply(404, b"")

        def do_POST(self):
            seen["paths"].append("POST " + self.path)
            n = int(self.headers.get("Content-Length", 0) or 0)
            if n:
                self.rfile.read(n)
            if self.path.startswith("/ac_portal/login.php"):
                if dialect_ok:
                    state["online"] = True   # 登录成功才放行
                self._reply(200, dialect_body.encode("utf-8"))
            else:
                self._reply(404, b"")
    try:
        srv = http.server.ThreadingHTTPServer(("127.0.0.1", port), H)
    except OSError:
        return None
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv, seen


def _sim_cfg():
    return _cfg(server="http://127.0.0.1", probe_ips="127.0.0.1",
                bind_ip="127.0.0.1", campus_dns="", interval="1")


def t26_sim_end_to_end_multi_dialect():
    """端到端：探测(302劫持)→门户存活→登录→在线全循环，三方言默认配置
    零改动直驱——证明的是"任何符合公开 ac_portal 规范的门户都能用"。"""
    import keeper as _k
    dialects = [
        ("伪JSON", "{'success':true,'msg':'ok'}"),
        ("标准JSON", '{"ret_code":0,"msg":"登录成功"}'),
        ("英文", "login successful"),
    ]
    for name, body in dialects:
        sim = _sim_portal(body, True)
        if sim is None:
            raise _Skip("127.0.0.1:80 被占用/无权限，模拟门户用例跳过（零副作用承诺）")
        srv, seen = sim
        try:
            k = _k.Keeper(_sim_cfg())
            k.tick()   # 掉线态：探测被劫持 → 存活确认 → 登录
            assert k.state == "online", f"{name}：首轮 tick 应在线，实际 {k.state}"
            k.tick()   # 已在线：探测直通 → 维持
            assert k.state == "online", f"{name}：二轮探测应维持在线"
            assert any(p.startswith("POST /ac_portal/login.php") for p in seen["paths"]), seen["paths"]
            assert any(p.startswith("GET /generate_204") for p in seen["paths"]), seen["paths"]
        finally:
            _close_sim(srv)
            srv.server_close()


def t26_sim_variant_rejects_honestly():
    """auth_tag 变体门户：登录被拒 → 明确 rejected 且原因可见，不假装在线。"""
    import keeper as _k
    sim = _sim_portal("{'success':false,'msg':'auth_tag required'}", False)
    if sim is None:
        raise _Skip("127.0.0.1:80 被占用/无权限，模拟门户用例跳过")
    srv, seen = sim
    try:
        k = _k.Keeper(_sim_cfg())
        k.tick()
        assert k.state == "rejected", f"应诚实进 rejected，实际 {k.state}"
        assert "auth_tag" in (k.state_detail or ""), k.state_detail
    finally:
        _close_sim(srv)
        srv.server_close()


def t26_unknown_response_gets_detect_hint():
    """响应「无法判定」= 疑似方言/协议不兼容 → 状态直接提示跑 --detect。"""
    import keeper as _k
    k = _k.Keeper(_sim_cfg())
    k.handle_rejection("无法判定响应内容")
    assert "--detect" in (k.state_detail or ""), k.state_detail


check("模拟门户端到端三方言直驱", t26_sim_end_to_end_multi_dialect)
check("模拟变体门户诚实失败", t26_sim_variant_rejects_honestly)
check("无法判定响应提示跑 --detect", t26_unknown_response_gets_detect_hint)

print("== 循环27：发布阻塞项（trouble-maker P1 三件）==")


def t27_server_port_unit():
    """_server_target：带端口 server 解析 (host, port)；缺省按 scheme 80/443。"""
    from keeper import _server_target as T
    assert T("http://p.example.edu:8080") == ("p.example.edu", 8080)
    assert T("https://p.example.edu") == ("p.example.edu", 443)
    assert T("http://p.example.edu") == ("p.example.edu", 80)
    assert T("p.example.edu") == ("p.example.edu", 443)   # 无 scheme → TLS 语义
    assert T("http://h:abc") == ("h", 80)                 # 非法端口回落 scheme 缺省


def t27_server_with_port_end_to_end():
    """带端口的门户（如 http://portal:8080）端到端可登录（trouble-maker P1-3
    端口死路：_raw_http 原先硬编码 80/443，自探测填回的带端口门户必死）。"""
    import keeper as _k
    sim = _sim_portal("{'success':true,'msg':'ok'}", True, port=8080)
    if sim is None:
        raise _Skip("127.0.0.1:8080 被占用，模拟门户用例跳过")
    srv, seen = sim
    try:
        k = _k.Keeper(_cfg(server="http://127.0.0.1:8080", probe_ips="127.0.0.1",
                           bind_ip="127.0.0.1", campus_dns="", interval="1"))
        k.tick()
        assert k.state == "online", f"带端口门户应可登录在线，实际 {k.state}"
        assert any(p.startswith("POST /ac_portal/login.php") for p in seen["paths"]), seen["paths"]
    finally:
        _close_sim(srv)
        srv.server_close()


def t27_bad_numeric_config_survives():
    """ini 数值键填非数字 → 构造不崩、回落默认（pythonw 下不能无声消失）。"""
    import keeper as _k
    k = _k.Keeper(_cfg(interval="abc"))
    assert k.interval == 3.0, k.interval
    k2 = _k.Keeper(_cfg(reject_backoff="xyz", portal_down_wait="12"))
    assert k2.portal_down_wait == 12.0, k2.portal_down_wait
    assert k2.reject_backoff == 600.0, k2.reject_backoff
    k3 = _k.Keeper(_cfg(login_timeout="abc"))
    assert k3.login_timeout == max(6.0, 2 * k3.probe_timeout), k3.login_timeout


def t27_plaintext_degradation_preserved():
    """keyring 降级（明文遗留）场景：保存设置/留空密码保存，明文必须原样保留
    （trouble-maker P1-1：_disk_values 曾无条件改写 <keyring> 占位 → 重启丢凭据）。"""
    import secrets
    import app_main
    import keeper as _k
    import shell.keeper_paths as _kp
    import tempfile
    plain = "plain-" + secrets.token_urlsafe(9)
    tmp = Path(tempfile.mkdtemp()) / "config.ini"
    tmp.write_text(f"[keeper]\nusername = u\npassword = {plain}\n", encoding="utf-8")
    old_cfg, old_p = _km.CONFIG_FILE, _kp.CONFIG_FILE
    _km.CONFIG_FILE, _kp.CONFIG_FILE = tmp, tmp
    try:
        live = dict(_k.DEFAULTS, username="u", password=plain,
                    server="https://portal.example.edu")
        f = app_main.KeeperFacade(_k.Keeper(live))
        f.save_settings({"interval": "9"})
        text = tmp.read_text(encoding="utf-8")
        assert plain in text, "明文降级下保存设置不得改写密码行"
        assert "<keyring>" not in text, "明文遗留不得被换成占位符"
        f.save_credentials("u", "", "https://portal.example.edu")
        text = tmp.read_text(encoding="utf-8")
        assert plain in text, "留空密码保存凭据也不得改写明文"
    finally:
        _km.CONFIG_FILE, _kp.CONFIG_FILE = old_cfg, old_p


def t27_save_config_never_writes_response_keys():
    """save_config 是唯一写盘点：任何旁路（明文迁移/SetupFacade 全量回写）
    都不得把 response.* 合并键写进 [keeper] 段（trouble-maker P2-5 根修）。"""
    import keeper as _k
    import shell.keeper_paths as _kp
    import tempfile
    tmp = Path(tempfile.mkdtemp()) / "config.ini"
    tmp.write_text("[keeper]\nusername = u\npassword = <keyring>\n"
                   "[response]\nsuccess_marks = authed_ok\n", encoding="utf-8")
    old_cfg, old_p = _km.CONFIG_FILE, _kp.CONFIG_FILE
    _km.CONFIG_FILE, _kp.CONFIG_FILE = tmp, tmp
    try:
        cfg = _k.load_config()          # 含 response.success_marks 合并键
        cfg.update({"interval": "9"})
        _k.save_config(cfg)             # 全量回写（明文迁移/SetupFacade 同款）
        keeper_section = tmp.read_text(encoding="utf-8").split("[response]")[0]
        assert "response." not in keeper_section, "response.* 不得进 [keeper] 段"
        assert "[response]" in tmp.read_text(encoding="utf-8"), "[response] 段必须保留"
    finally:
        _km.CONFIG_FILE, _kp.CONFIG_FILE = old_cfg, old_p


check("server 端口解析", t27_server_port_unit)
check("带端口门户端到端", t27_server_with_port_end_to_end)
check("非法数值配置不崩", t27_bad_numeric_config_survives)
check("明文降级不被占位覆盖", t27_plaintext_degradation_preserved)
check("save_config 段隔离根修", t27_save_config_never_writes_response_keys)

print("== 循环28：发布前打磨（脱敏 / 冻结风暴 / 一致性）==")


def t28_redact_masks_username_echo():
    """门户在拒绝消息里直接回显账号（「用户xxx被冻结」，2026-11-05 日志实证）
    → 日志与托盘状态必须脱敏，不能只盖 userName= 表单格式。"""
    import keeper as _k
    k = _k.Keeper(_cfg(username="u20231101"))
    k.handle_rejection("用户u20231101被冻结，冻结剩余60s")
    detail = k.state_detail or ""
    assert "u20231101" not in detail, f"账号明文泄漏进状态/日志：{detail!r}"
    assert "***" in detail, detail


def t28_freeze_gate_survives_probe_reset():
    """冻结期内的锚点抖动判「在线」会把 reject_until 清零——这正是 00:00 风暴
    的根因。冻结闸门必须独立于退避，在 probe 恢复后依然拦住立即重登。"""
    import time as _t
    import keeper as _k
    k = _k.Keeper(_cfg(username="u1"))
    k.handle_rejection("用户u1被冻结，冻结剩余60s")
    assert k._frozen_until > _t.monotonic() + 50, "冻结消息应触发独立闸门"
    k.probe_online = lambda: True          # 锚点抖动误判在线
    k.tick()
    assert k.state == "online" and k.reject_until == 0.0, "probe 恢复仍清退避（原语义）"
    calls = []
    k.probe_online = lambda: False         # 再抖动判离线
    k.portal_alive = lambda: True
    k.do_login = lambda: calls.append(1) or (True, "")
    k.tick()
    assert not calls, "冻结期内不得发登录请求（风暴根因封堵）"


def t28_probe_host_configurable():
    """探测 Host 头可配（默认 connect.rom.miui.com），自定义锚点按需改。"""
    import keeper as _k
    seen = {}
    old = _k.physical_http
    try:
        def fake(host, ip, path, **kw):
            seen["host"] = (kw.get("headers") or {}).get("Host", "")
            return 404, ""
        _k.physical_http = fake
        k = _k.Keeper(_cfg(probe_host="captive.example.edu"))
        k.probe_online()
        assert seen["host"] == "captive.example.edu", seen
    finally:
        _k.physical_http = old


def t28_detect_portal_consumes_probe_path():
    """自动探测消费可配 probe_path（端点可配的全链路一致性）。"""
    from keeper import detect_portal
    seen = []
    def fake(host, ip, path, **kw):
        seen.append(path)
        return 204, {}, ""
    detect_portal(probe_ips="198.51.100.7", probe_path="/custom_204", _http=fake)
    assert seen == ["/custom_204"], seen


def t28_portal_location_rejects_unspecified_multicast():
    """Location 指向 0.0.0.0 / 组播段 → 拒绝（私有段门户仍放行）。"""
    from keeper import parse_portal_location as P
    assert P({"Location": "http://0.0.0.0/ac_portal/pc.html"}) == ""
    assert P({"Location": "http://224.0.0.1/ac_portal/pc.html"}) == ""
    assert P({"Location": "http://192.168.1.1/ac_portal/pc.html"}) == "http://192.168.1.1"


def t28_wizard_rejects_empty_password():
    """首配向导空密码保存 = 永远无法就绪 → 当场拒绝，不进静默循环。"""
    import tkinter as tk
    from unittest.mock import patch
    from shell.app import Shell
    warnings = []
    saved = []

    class K:
        needs_setup = True
        def snapshot(self):
            return {"state": "starting", "detail": "", "interval": 3, "reject_remaining": 0}
        def fetch_flux(self): return {}
        def force_reconnect(self): ...
        def on_change(self, cb): ...
        def save_credentials(self, *a): saved.append(a)
        def test_credentials(self, *a): return True, "ok"
        def detect_portal(self): return ""
        def get_settings(self): return {}
        def save_settings(self, v): ...

    try:
        win = Shell(K())
    except tk.TclError:
        raise _Skip("无桌面会话，UI 用例不适用")
    try:
        win._test_ctx = ("u", "", "https://portal.example.edu")
        with patch("shell.app.messagebox.showwarning",
                   side_effect=lambda t, m, **kw: warnings.append(m)), \
             patch("shell.app.messagebox.showinfo"):
            win._finish_save_test(True, "ok")
        assert not saved, "向导空密码不得落地"
        assert warnings, "应当场提示缺密码"
    finally:
        try:
            win.destroy()
        except tk.TclError:
            pass


def t28_use_tls_case_insensitive():
    """server scheme 大写（HTTP://）不得被误判走 TLS 443。"""
    import keeper as _k
    k = _k.Keeper(_cfg(server="HTTP://p.example.edu"))
    assert k._use_tls() is False, k._use_tls()


check("日志脱敏覆盖账号回显", t28_redact_masks_username_echo)
check("冻结闸门独立于退避", t28_freeze_gate_survives_probe_reset)
check("探测 Host 可配", t28_probe_host_configurable)
check("自动探测消费 probe_path", t28_detect_portal_consumes_probe_path)
check("Location 拒未指定/组播段", t28_portal_location_rejects_unspecified_multicast)
check("向导空密码当场拒绝", t28_wizard_rejects_empty_password)
check("scheme 大小写不敏感", t28_use_tls_case_insensitive)

print("== 循环29：收尾打磨（clash 代理环境 / Desk 无桌面降级）==")


def t29_clash_session_no_env_proxy():
    """clash 联动不得吃环境代理：http_proxy 会把 loopback 请求绕道代理节点。"""
    from unittest.mock import MagicMock, patch
    import keeper as _k
    k = _k.Keeper(_cfg(clash_api_url="http://127.0.0.1:9090"))
    seen = {}

    class FakeSession:
        trust_env = True   # 实现若忘了关，这里就是活的
        def post(self, *a, **kw):
            seen["trust_env_post"] = self.trust_env
            return MagicMock(status_code=200)
        def delete(self, *a, **kw):
            return MagicMock(status_code=200)

    with patch.object(_k.requests, "Session", FakeSession):
        k.clash_recover()
    assert seen.get("trust_env_post") is False, \
        f"Session.trust_env 必须为 False（环境代理会劫持 loopback 请求），实际 {seen}"


def t29_desk_start_survives_no_display():
    """无桌面会话（Tk 初始化失败）→ Desk.start() 不得白等 5 秒。"""
    import time as _t
    from unittest.mock import patch
    import shell.desk as _d
    d = _d.Desk()
    try:
        with patch("tkinter.Tk", side_effect=RuntimeError("no display")):
            t0 = _t.monotonic()
            d.start()
            cost = _t.monotonic() - t0
        assert cost < 2.0, f"Tk 不可用时 start() 应立即返回，实际耗时 {cost:.1f}s"
    finally:
        d.stop()


check("clash 联动不吃环境代理", t29_clash_session_no_env_proxy)
check("Desk 无桌面快速降级", t29_desk_start_survives_no_display)

print("== 循环30：实战修复（登录节流 / 冻结闸门自愈 / 原因不丢）==")


def t30_login_throttle_under_flap():
    """probe 抖回「在线」会清退避——风暴根因。登录必须有全局最小间隔，
    无论退避是否被清，30 秒内第二次尝试一律拦截。"""
    import keeper as _k
    k = _k.Keeper(_cfg())
    logins = []
    k.do_login = lambda: logins.append(1) or (True, "")
    k.probe_online = lambda: False
    k.portal_alive = lambda: True
    k.tick()                       # 第一次：允许登录
    assert logins, "首次登录应放行"
    k.probe_online = lambda: True
    k.tick()                       # 抖回在线清退避
    k.probe_online = lambda: False
    k.tick()                       # 又抖离线：距上次登录 <30s，必须被节流
    assert len(logins) == 1, f"30 秒节流应拦截第二次登录，实际 {len(logins)} 次"


def t30_freeze_gate_releases_on_stable_online():
    """冻结闸门 600 秒一刀切曾让手动登录后的守护持续装死——
    probe 连续 3 次在线说明会话真恢复了，闸门必须自动解除。"""
    import time as _t
    import keeper as _k
    k = _k.Keeper(_cfg())
    k._frozen_until = _t.monotonic() + 600
    k.probe_online = lambda: True
    k.tick(); k.tick()
    assert k._frozen_until > 0, "不足 3 次不得解除"
    k.tick()
    assert k._frozen_until == 0.0, "连续 3 次在线应解除冻结闸门"


def t30_reject_reason_survives_portal_down():
    """portal_down 迁移曾把被拒原因抹空（set_state detail 默认 ""）——
    退避期只见「登录被拒：（退避中）」看不见原因。原因须与状态解耦保留。"""
    import keeper as _k
    k = _k.Keeper(_cfg())
    k.handle_rejection("E63032：密码错误，您还可以重试X次")
    assert "密码错误" in k._reject_reason
    k.set_state("portal_down")          # 不可达迁移（旧实现在此丢原因）
    k.set_state("rejected", k._reject_reason)   # 退避期展示
    assert "密码错误" in k.describe_state(), k.describe_state()


check("登录全局节流防风暴", t30_login_throttle_under_flap)
check("冻结闸门连续在线自愈", t30_freeze_gate_releases_on_stable_online)
check("被拒原因不被 portal_down 抹掉", t30_reject_reason_survives_portal_down)

print("== 循环31：进程尸检 + 夜间抖动降速 ==")


def t31_probe_flap_does_not_reset_transient_streak():
    """夜间实测（00:00-06:32 抖动 6.5h、冻结 5 次）根因：probe 单次抖回在线
    就清瞬态连击计数 → 长退避永远升不起来 → 整夜短节流轰炸登录。
    单次在线可能是抖动假信号；连续 3 次在线（环境稳定）才配清零。"""
    import keeper as _k
    k = _k.Keeper(_cfg())
    k.transient_streak = 4          # 已连击 4 次，下一次瞬态拒绝将升长退避
    logins = []
    k.do_login = lambda: logins.append(1) or (False, "Task 'NOAUTH:*' not taken")
    k.probe_online = lambda: False
    k.portal_alive = lambda: True
    k._last_login_at = 0.0
    k.tick()                        # 被拒 → streak=5 → 600s 长退避
    assert k.transient_streak == 5
    k.reject_until = 0.0
    k.probe_online = lambda: True   # probe 单次抖回在线（假信号）
    k.tick()
    assert k.transient_streak == 5, f"单次在线不得清瞬态连击，实际 {k.transient_streak}"
    k.probe_online = lambda: False
    k.reject_until = 0.0
    k._last_login_at = 0.0
    k.tick()                        # 再被拒：streak 已是 5，保持长退避压制
    assert k.transient_streak >= 5


def t31_uncaught_exceptions_leave_autopsy():
    """pythonw 无控制台：进程/线程未捕获异常必须落盘 keeper.log，
    否则像 06:32 后的静默死亡一样零痕迹、无从尸检。"""
    import app_main
    from unittest.mock import patch
    records = []
    with patch("keeper.LOG_FILE", Path(tempfile.mkdtemp()) / "keeper.log"):
        import logging as _lg
        h = _lg.FileHandler(Path(tempfile.mkdtemp()) / "audit.log", encoding="utf-8")
        lg = _lg.getLogger("keeper")
        old_handlers = lg.handlers[:]
        lg.handlers = [h]
        try:
            app_main._install_crash_hooks()
            import threading
            try:
                raise RuntimeError("尸检测试-主线程")
            except RuntimeError as exc:
                app_main._crash_hook(type(exc), exc, exc.__traceback__)
            import threading
            from types import SimpleNamespace
            try:
                raise ValueError("尸检测试-子线程")
            except ValueError as exc:
                app_main._thread_crash_hook(SimpleNamespace(
                    exc_type=type(exc), exc_value=exc, exc_traceback=exc.__traceback__,
                    thread=threading.current_thread()))
            h.flush()
            text = Path(h.baseFilename).read_text(encoding="utf-8")
        finally:
            lg.handlers = old_handlers
            app_main._install_crash_hooks()   # 恢复真实钩子
    assert "尸检测试-主线程" in text and "尸检测试-子线程" in text, text[-300:]


check("probe 抖动不清瞬态连击", t31_probe_flap_does_not_reset_transient_streak)
check("进程崩溃留尸检日志", t31_uncaught_exceptions_leave_autopsy)

print("== 循环32：多账号历史 + 自动重连开关 ==")


def t32_accounts_roundtrip():
    """[accounts] 段：置顶去重、序号=最近使用序、超限淘汰最旧，不碰其它段。"""
    import configparser
    from shell.accounts import remember_account, list_accounts
    import shell.accounts as _acct

    def run(tmp):
        remember_account("u1", "https://s1")
        remember_account("u2", "https://s2")
        remember_account("u1", "https://s1b")   # 同账号：提顶更新服务器，不新增
        got = list_accounts()
        assert got == [("u1", "https://s1b"), ("u2", "https://s2")], got
        for i in range(3, 7):                    # 超 MAX：最旧的被挤掉
            remember_account(f"u{i}", f"https://s{i}")
        got = list_accounts()
        assert len(got) == _acct.MAX_ACCOUNTS and got[0][0] == "u6", got
        assert ("u2", "https://s2") not in got, "超限须淘汰最旧"
        # [keeper] 与 [accounts] 互不干扰（save_config 增量写保留其它段）
        _km.save_config({"username": "u6", "server": "https://s6"})
        assert list_accounts()[0][0] == "u6"
        assert _km.load_config()["username"] == "u6"
        # 密码绝不落历史：每条值恒为 username|server 两段，无第三段
        p = configparser.ConfigParser(interpolation=None)
        p.read(tmp, encoding="utf-8")
        for key, val in p.items("accounts"):
            assert val.count("|") == 1, (key, val)
    _with_temp_config(run)


def t32_facade_save_credentials_remembers():
    """保存凭据（面板/向导两条路）即写入历史，当前账号不变语义不受影响。"""
    import app_main
    from shell.accounts import list_accounts
    users = ["__t32_s1__", "__t32_s2__"]

    def run(tmp):
        cfg = dict(_km.DEFAULTS)
        cfg.update({"username": users[0], "password": "pw1"})
        _km.save_config(cfg)
        k = _km.Keeper(cfg)
        f = app_main.KeeperFacade(k)
        f.save_credentials(users[0], "pw1", "https://a.example")
        f.save_credentials(users[1], "pw2", "https://b.example")
        got = list_accounts()
        assert [u for u, _ in got] == [users[1], users[0]], got
        # 空密码保存（"不改已存凭据"语义）同样入历史（P1-4 行为保持）
        f.save_credentials(users[0], "", "https://a.example")
        assert list_accounts()[0][0] == users[0]
    try:
        _with_temp_config(run)
    finally:
        import keyring
        for u in users:
            try:
                keyring.delete_password("campus-net-autologin", u)
            except Exception:  # noqa: BLE001
                pass


def t32_facade_switch_account():
    """切换历史账号：密码从 keyring 按用户名还原；取不到密码必须拒绝——
    绝不能把旧账号密码带进新账号（save_credentials 留空=不改的陷阱）。"""
    import app_main
    test_users = ["__t32_a__", "__t32_b__"]

    def run(tmp):
        cfg = dict(_km.DEFAULTS)
        cfg.update({"username": test_users[0], "password": "pa"})
        _km.save_config(cfg)
        k = _km.Keeper(cfg)
        f = app_main.KeeperFacade(k)
        f.save_credentials(test_users[0], "pa", "https://a.example")
        f.save_credentials(test_users[1], "pb", "https://b.example")
        # 切回 a：运行态/表单/磁盘全部换成 a，密码从 keyring 还原
        ok, msg = f.switch_account(test_users[0])
        assert ok, msg
        assert k.cfg["username"] == test_users[0]
        assert k.cfg["password"] == "pa", "须还原 a 自己的密码，不是 b 的"
        assert k.form["userName"] == test_users[0] and k.form["pwd"] == "pa"
        disk = _km.load_config()
        assert disk["username"] == test_users[0]
        assert disk["password"] == "<keyring>", "磁盘只准写占位"
        # 不在历史：拒绝且运行态纹丝不动
        ok, msg = f.switch_account("__t32_ghost__")
        assert not ok and "__t32_ghost__" in msg
        assert k.cfg["username"] == test_users[0]
    try:
        _with_temp_config(run)
    finally:
        import keyring
        for u in test_users:
            try:
                keyring.delete_password("campus-net-autologin", u)
            except Exception:  # noqa: BLE001
                pass


def t32_switch_missing_password_refuses():
    """历史里有账号但 keyring 无密码（如曾明文降级/手改 ini）：拒绝切换、
    提示补密，运行态不被污染。"""
    import app_main

    def run(tmp):
        cfg = dict(_km.DEFAULTS)
        cfg.update({"username": "u", "password": "p"})
        _km.save_config(cfg)
        k = _km.Keeper(cfg)
        f = app_main.KeeperFacade(k)
        from shell.accounts import remember_account
        remember_account("__t32_nopwd__", "https://s")
        ok, msg = f.switch_account("__t32_nopwd__")
        assert not ok, "无密码账号不得切换"
        assert "密码" in msg, msg
        assert k.cfg["username"] == "u", "拒绝不得污染运行态"
    _with_temp_config(run)


def t32_paused_skips_login_manual_bypasses():
    """auto_reconnect=false：周期 tick 探测照跑但绝不登录（state=paused）；
    manual tick（用户显式重连/保存凭据唤醒）放行——开关只拦"自动"。"""
    import keeper as _k
    k = _k.Keeper(_cfg(auto_reconnect="false"))
    assert k.auto_reconnect is False
    logins = []
    k.probe_online = lambda: False
    k.portal_alive = lambda: True
    k.do_login = lambda: logins.append(1) or (True, "")
    k.clash_recover = lambda: None
    k.tick()
    assert not logins and k.state == "paused", "关闭开关周期 tick 不得登录"
    assert k.describe_state() == "自动重连已关闭"
    k._last_login_at = 0.0
    k.tick(manual=True)
    assert len(logins) == 1, "manual tick 必须放行登录"


def t32_default_on_and_only_true_enables():
    """向后兼容：缺省=开；仅显式 true 才算开（0/off/乱填=关，保守方向
    是不擅自登录）；hot reload 改开关立即生效。"""
    import keeper as _k
    assert _k.Keeper(_cfg()).auto_reconnect is True, "旧 ini 无此键须保持自动重连"
    for bad in ("false", "0", "off", ""):
        assert _k.Keeper(_cfg(auto_reconnect=bad)).auto_reconnect is False, bad
    k = _k.Keeper(_cfg())
    k.apply_config(_cfg(auto_reconnect="false"))
    assert k.auto_reconnect is False, "热载关开关即生效"
    # 关闭时保存配置不得"顺手"登录一次；开启时保持"保存即重连"
    k.probe_online = lambda: False
    k.portal_alive = lambda: True
    logins = []
    k.do_login = lambda: logins.append(1) or (True, "")
    k.apply_config(_cfg(auto_reconnect="false"))
    assert k._manual_pending is False, "关开关的保存动作不得标记手动放行"
    k.apply_config(_cfg(auto_reconnect="true"))
    assert k._manual_pending is True and k.force_evt.is_set(), "开开关须立即重连"


def t32_run_loop_passes_manual_intent():
    """run 循环把"唤醒原因"传进 tick：request_reconnect（用户意图）放行；
    超时周期 tick 不放行——auto 关时后者必须停在 paused。"""
    import threading
    import time as _time
    import keeper as _k
    k = _k.Keeper(_cfg(auto_reconnect="false", interval="1"))
    seen = []
    started = threading.Event()

    def fake_tick(manual=False):
        seen.append(bool(manual))
        if not started.is_set():
            started.set()
        return
    k.tick = fake_tick
    th = threading.Thread(target=k.run, daemon=True)
    th.start()
    try:
        assert started.wait(5), "run 应至少 tick 一次"
        k.request_reconnect()          # 用户点「立即检测重连」
        deadline = _time.time() + 6
        while _time.time() < deadline and True not in seen:
            _time.sleep(0.05)
    finally:
        k.stop_evt.set()
        k.force_evt.set()
        th.join(timeout=6)
    assert seen and seen[0] is False, "启动/周期 tick 不得带 manual"
    assert True in seen, "request_reconnect 唤醒的那次 tick 必须 manual=True"


def t32_tray_force_goes_through_request_reconnect():
    """托盘「立即检测重连」与面板同一实现：清退避 + 标记手动意图，
    关闭自动重连时托盘按钮仍要能用。"""
    from keeper import DEFAULTS, Keeper, Tray
    cfg = dict(DEFAULTS)
    cfg.update({"username": "smoke", "password": "smoke"})
    k = Keeper(cfg)
    k.reject_until = 9999.0
    tray = Tray(k)
    item = next(it for it in tray.icon.menu
                if "立即检测重连" in str(getattr(it, "text", it)))
    item(None)
    tray.icon.stop()
    assert k.reject_until == 0.0, "托盘按钮须清退避（与面板一致）"
    assert k._manual_pending is True and k.force_evt.is_set()


def t32_facade_snapshot_maps_paused():
    """paused 是真实状态，不得被降级映射成 offline（指示灯语义漂移）。"""
    import app_main
    import keeper as _k
    k = _k.Keeper(_cfg(auto_reconnect="false"))
    k.state = "paused"
    k.state_detail = "自动重连已关闭"
    snap = app_main.KeeperFacade(k).snapshot()
    assert snap["state"] == "paused", snap


def t32_shell_switch_and_toggle():
    """面板：历史下拉填充+选中即切（失败也回显账号等补密）；
    「掉线自动重连」勾选框回显 ini 值并随保存写回。"""
    import shell.app as app_mod
    from shell.app import Shell
    orig_info = app_mod.messagebox.showinfo
    orig_err = app_mod.messagebox.showerror
    orig_warn = app_mod.messagebox.showwarning
    app_mod.messagebox.showinfo = lambda *a, **k: None
    app_mod.messagebox.showerror = lambda *a, **k: None
    app_mod.messagebox.showwarning = lambda *a, **k: None

    def run(tmp):
        from shell.accounts import remember_account
        remember_account("hist01", "https://s-h")
        remember_account("hist02", "https://s-h2")
        saved, switched = [], []

        class K:
            needs_setup = False

            def snapshot(self):
                return {"state": "paused", "detail": "自动重连已关闭",
                        "interval": 3, "reject_remaining": 0}

            def save_settings(self, v):
                saved.append(v)

            def fetch_flux(self):
                return {}

            def force_reconnect(self): ...
            def on_change(self, cb): ...
            def save_credentials(self, *a): ...

            def switch_account(self, u):
                switched.append(u)
                if u == "hist02":
                    return False, "hist02 的密码不在凭据管理器，请填写密码"
                return True, f"已切换到 {u}，正在用已存凭据重连"

            def test_credentials(self, *a):
                return True, "ok"

            def detect_portal(self):
                return ""

            def get_settings(self):
                return {"auto_reconnect": "false", "username": "cur",
                        "server": "https://s-old", "bind_ip": ""}

        win = Shell(K())
        win.attributes("-alpha", 0.0)
        win._adv_win.attributes("-alpha", 0.0)
        win.update()
        try:
            vals = list(win.c_account.cget("values"))
            assert vals == ["hist02", "hist01"], vals   # 最近保存的置顶
            assert win._auto_on is False, "ini false 须回显未勾选"
            # 选中可切换账号 → 协议调用 + 表单/提示回显
            win.c_account.current(1)
            win._on_switch_account()
            assert switched == ["hist01"]
            assert win.e_user.get() == "hist01"
            assert win.e_server.get() == "https://s-h"
            assert "已切换" in win._detect_hint.cget("text")
            # 无密码账号切换失败：不假装成功，但仍回显供用户补密
            win.c_account.current(0)
            win._on_switch_account()
            assert switched == ["hist01", "hist02"]
            assert win.e_user.get() == "hist02"
            assert "密码" in win._detect_hint.cget("text")
            # 点开关 → 文案翻转、保存写 true
            win._toggle_auto()
            assert win._auto_on is True
            assert "开" in win._btn_auto.cget("text")
            win._on_save_settings()
            assert saved and saved[-1]["auto_reconnect"] == "true", saved
        finally:
            win.destroy()
            app_mod.messagebox.showinfo = orig_info
            app_mod.messagebox.showerror = orig_err
            app_mod.messagebox.showwarning = orig_warn
    _with_temp_config(run)


def t32_tray_panel_paused_colors_in_sync():
    """新增 paused 状态跨内核/外壳成对常量（循环4 同款校验，显式点名新键）。"""
    from keeper import TRAY_COLORS
    from shell.kernel_api import STATE_COLORS, STATE_LABELS
    assert "paused" in TRAY_COLORS and "paused" in STATE_COLORS
    assert STATE_LABELS["paused"] == "自动重连已关闭"
    want = "#{:02x}{:02x}{:02x}".format(*TRAY_COLORS["paused"])
    assert STATE_COLORS["paused"].lower() == want.lower()


check("accounts 段读写/去重/淘汰/段隔离", t32_accounts_roundtrip)
check("保存凭据即入历史", t32_facade_save_credentials_remembers)
check("切换账号还原 keyring 密码", t32_facade_switch_account)
check("无密码历史账号拒绝切换", t32_switch_missing_password_refuses)
check("关闭自动重连：探测不登录，manual 放行", t32_paused_skips_login_manual_bypasses)
check("开关默认开+仅 true 开+热载生效", t32_default_on_and_only_true_enables)
check("run 循环传递手动意图", t32_run_loop_passes_manual_intent)
check("托盘立即重连与面板同实现", t32_tray_force_goes_through_request_reconnect)
check("快照透传 paused", t32_facade_snapshot_maps_paused)
check("面板历史切换+自动重连勾选", t32_shell_switch_and_toggle)
check("paused 颜色跨层同步", t32_tray_panel_paused_colors_in_sync)


def t32_ssl_combobox_echoes_value():
    """readonly Combobox 回显回归锁：set() 才有效——delete+insert 被 readonly
    校验静默拒绝（用户实测"证书校验"永远空白，v1.0.2 既有缺陷）。"""
    from shell.app import Shell

    class K:
        needs_setup = False
        def snapshot(self):
            return {"state": "online", "detail": "", "interval": 3,
                    "reject_remaining": 0}
        def get_settings(self): return {"ssl_verify": "false"}
        def fetch_flux(self): return {}
        def force_reconnect(self): ...
        def on_change(self, cb): ...
        def save_credentials(self, *a): ...
        def switch_account(self, u): return True, "ok"
        def test_credentials(self, *a): return True, "ok"
        def detect_portal(self): return ""
        def save_settings(self, v): ...

    def run(tmp):
        win = Shell(K())
        win.attributes("-alpha", 0.0)
        win._adv_win.attributes("-alpha", 0.0)
        try:
            win.update()
            assert win.e_ssl.get() == "false", \
                f"证书校验回显丢失: {win.e_ssl.get()!r}"
        finally:
            win.destroy()
    _with_temp_config(run)  # 纪律：不读真实用户 ini，测试窗口不闪现


def t32_seed_history():
    """启动播种行为锁（trouble-maker P1-5）：当前账号进 [accounts]、cfg 携带的
    凭据值绝不落盘、空账号 no-op、ini 不可写（占用/杀软）只留日志不击穿启动环。"""
    import app_main

    def run(tmp):
        app_main._seed_history({"username": "seed01", "server": "https://s",
                                "password": "pw"})
        from shell.accounts import list_accounts
        assert list_accounts() == [("seed01", "https://s")], list_accounts()
        assert "pw" not in tmp.read_text(encoding="utf-8"), \
            "播种路径不得把 cfg 凭据写进 ini"
        app_main._seed_history({"username": "", "server": "x", "password": "pw"})
        assert list_accounts() == [("seed01", "https://s")], "空账号须 no-op"
        # 写盘不可用：CONFIG_FILE 指向不存在的深层目录 → write_text 抛 OSError
        import shell.keeper_paths as _kp
        old = _kp.CONFIG_FILE
        _kp.CONFIG_FILE = tmp.parent / "no" / "such" / "dir" / "config.ini"
        try:
            app_main._seed_history({"username": "seed02", "server": "https://s2"})
        finally:
            _kp.CONFIG_FILE = old
        # 不抛异常即通过（播种失败仅日志告警，启动环继续）
    _with_temp_config(run)


def t32_dark_theme_flattens_borders():
    """主题修复行为锁：白点根因是 clam 默认 2px 斜角边框的四角像素不跟随
    bordercolor——三族控件都必须 1px；combobox 按压态不得回落亮米色；
    面板标题带版本号（用户分辨新旧构建的界面指纹，trouble-maker P0）。"""
    from shell.app import Shell
    from shell.keeper_paths import APP_VERSION
    from tkinter import ttk

    class K:
        needs_setup = False
        def snapshot(self):
            return {"state": "online", "detail": "", "interval": 3,
                    "reject_remaining": 0}
        def get_settings(self): return {}
        def fetch_flux(self): return {}
        def force_reconnect(self): ...
        def on_change(self, cb): ...
        def save_credentials(self, *a): ...
        def switch_account(self, u): return True, "ok"
        def test_credentials(self, *a): return True, "ok"
        def detect_portal(self): return ""
        def save_settings(self, v): ...

    def run(tmp):
        win = Shell(K())
        win.attributes("-alpha", 0.0)
        win._adv_win.attributes("-alpha", 0.0)
        try:
            st = ttk.Style(win)
            for cls in ("TEntry", "TCombobox", "TScrollbar"):
                assert st.lookup(cls, "borderwidth") == 1, \
                    f"{cls} 边框宽度未压平（四角白点回归）"
            bg_map = dict(st.map("TCombobox").get("background", []))
            assert bg_map.get("pressed") not in (None, "#eeebe7"), \
                f"combobox 按压态亮米色残留: {bg_map}"
            assert APP_VERSION in win.title(), \
                f"标题缺版本指纹: {win.title()!r}"
        finally:
            win.destroy()
    _with_temp_config(run)


check("证书校验回显（readonly set 语义）", t32_ssl_combobox_echoes_value)
check("启动播种历史（凭据不落盘+写失败不击穿）", t32_seed_history)
check("主题压平边框+按压态+版本标题", t32_dark_theme_flattens_borders)


def t36_popdown_listbox_dark():
    """Combobox 下拉弹出列表暗色化：clam 的 popdown 是原生 Listbox（类名
    TComb），不吃 ttk 样式、默认白底——点开历史账号/证书校验弹出一块白板，
    是白点问题审查发现的最后一处亮底残留。option_add 按类名匹配生效。"""
    import tkinter as tk
    from shell.theme import PANEL, FG, apply_dark_ttk
    root = tk.Tk()
    root.withdraw()
    apply_dark_ttk(root)
    top = tk.Toplevel(root, class_="TComb")
    # 真实 popdown 的 listbox 在 ttk 源码中显式命名 "listbox"
    # （listbox $popdown.listbox）——合成测试件必须同名才能走同一条
    # option 数据库匹配路径
    lb = tk.Listbox(top, name="listbox")
    try:
        assert str(lb.cget("background")).lower() == PANEL.lower(), \
            f"popdown 白底: {lb.cget('background')!r}"
        assert str(lb.cget("selectbackground")).lower() != "#0000ff", \
            f"popdown 选中高亮仍是系统蓝: {lb.cget('selectbackground')!r}"
    finally:
        top.destroy()
        lb.destroy()
        root.destroy()


check("下拉弹出列表暗色化", t36_popdown_listbox_dark)


def t36_dpi_window_surfaced():
    """DPI 风控窗口必须上浮到面板细节行/托盘提示：用户实测 18:42——守护
    判"在线"正确但只写日志，网页打不开的用户以为守护失职而手动重登。
    在线状态下 L2 观察到受限 → detail 含"DPI/勿"提示；L2 恢复正常 → 清除。"""
    import keeper as _k
    import time as _t
    k = _k.Keeper(_cfg())
    k.probe_online = lambda: True
    k.probe_l2_restricted = lambda: True
    k.fetch_flux = lambda: {"用户名称": "u"}     # 会话探针：活着
    k._l2_at = 0.0
    k.tick()
    assert k.state == "online"
    assert "DPI" in k.state_detail and "勿" in k.state_detail, \
        f"受限窗口未上浮: {k.state_detail!r}"
    assert "DPI" in k.describe_state(), k.describe_state()
    k._l2_at = 0.0
    k.probe_l2_restricted = lambda: False
    k.tick()
    assert k.state_detail == "", f"窗口结束未清除: {k.state_detail!r}"


def t36_fake_online_reconnects_via_flux_ground_truth():
    """AC「路由通、会话死」假在线封堵：锚点全绿但 userflux 连续 2 次返回
    空表（=门户回了登录页）→ 判会话已掉，同拍直接走自动重连，重连成功
    清除判死标记。单次空表不得触发（防抖）。"""
    import keeper as _k
    import time as _t
    k = _k.Keeper(_cfg())
    k.probe_online = lambda: True
    k.probe_l2_restricted = lambda: False
    k.fetch_flux = lambda: {}
    k.portal_alive = lambda: True
    k.clash_recover = lambda: None
    logins = []
    k.do_login = lambda: logins.append(1) or (True, "ok")
    k._l2_at = _t.monotonic()
    k._sess_at = 0.0
    k.tick()
    assert k.state == "online" and not logins, "单次空表不得翻转判定"
    k._sess_at = 0.0
    k._last_login_at = 0.0
    k.tick()
    assert logins, "连续 2 次空表应判假在线并立即重连"
    assert k.state == "online" and k._session_dead is False, "重连成功须清除"


def t36_flux_probe_false_alarm_backoff():
    """cookie 认会话型异校部署（flux 恒空表但会话真好）：判死→重登→"已
    在线"→又判死，循环 3 次后探针自我停用 1 小时并留日志——绝不对着好
    会话无限重登（风暴防线，fetch_flux 文档 P-6 边界的产品化兜底）。"""
    import keeper as _k
    import time as _t
    k = _k.Keeper(_cfg())
    k.probe_online = lambda: True
    k.probe_l2_restricted = lambda: False
    k.fetch_flux = lambda: {}                    # 恒空表：cookie 型门户形态
    k.portal_alive = lambda: True
    k.clash_recover = lambda: None
    logins = []
    k.do_login = lambda: logins.append(1) or (True, "您已在线")
    for _ in range(9):                           # ≥3 轮「判死→重登」循环
        k._sess_at = 0.0
        k._last_login_at = 0.0
        k._l2_at = _t.monotonic()
        k.tick()
    assert k._sess_disabled_until > _t.monotonic() + 3000, \
        "3 次假阳性后探针应停用 1 小时"


check("DPI 窗口上浮面板/托盘", t36_dpi_window_surfaced)
check("flux 真值封堵假在线", t36_fake_online_reconnects_via_flux_ground_truth)
check("flux 探针假阳性自动停用", t36_flux_probe_false_alarm_backoff)

print("== 循环37：AC「路由通、会话死」通用行为族 e2e（汕大 2026-11-05 观察实例）==")


def _sim_ac_split(port: int = 80):
    """「路由与会话分离」型 AC 的通用模拟门户——这不是某校特例，是 AC 系
    设备的普遍行为族（踢会话不掐路由；汕大 18:42 现场观察到第一例）：
    - GET /generate_204 恒 204：AC 掐会话从不掐路由（锚点全程被欺骗，
      2026-11-05 18:42 实锤：游戏流被掐、守护锚点绿）
    - POST userflux：会话活→流量表；死/不适配→登录页（flux 真值）
    - POST login.php：block_proxy（=加速器开着被行为管理识别）→ 硬拒；
      flux_unsupported 门户返回「您已在线」；否则成功并复活会话
    state 三个开关即三种加速器/部署情况。端口占用返回 None（SKIP）。"""
    import http.server
    import threading
    seen = {"logins": []}
    state = {"session_alive": False, "block_proxy": False,
             "flux_unsupported": False}
    TABLE = (b"<table><tr><td>\xe7\x94\xa8\xe6\x88\xb7\xe5\x90\x8d\xe7\xa7\xb0\xef\xbc\x9a"
             b"</td><td>stu001</td></tr><tr><td>\xe6\x97\xa5\xe6\xb5\x81\xe9\x87\x8f"
             b"\xe9\xa2\x9d\xef\xbc\x9a</td><td>10G</td></tr></table>")
    LOGIN_PAGE = b"<html>ac_portal pwdLogin</html>"

    class H(http.server.BaseHTTPRequestHandler):
        def log_message(self, *a):
            pass

        def _reply(self, code, body=b"", headers=None):
            self.send_response(code)
            for k, v in (headers or {}).items():
                self.send_header(k, v)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            if body:
                self.wfile.write(body)

        def do_GET(self):
            if self.path.startswith("/generate_204"):
                self._reply(204)          # 路由永不掐——这正是失明点
            elif self.path.startswith("/ac_portal"):
                self._reply(200, LOGIN_PAGE)
            else:
                self._reply(404, b"")

        def do_POST(self):
            n = int(self.headers.get("Content-Length", 0) or 0)
            if n:
                self.rfile.read(n)
            if self.path.startswith("/ac_portal/userflux"):
                if state["flux_unsupported"] or not state["session_alive"]:
                    self._reply(200, LOGIN_PAGE)   # 空表=会话死/不适配
                else:
                    self._reply(200, TABLE)
            elif self.path.startswith("/ac_portal/login.php"):
                seen["logins"].append(1)
                if state["block_proxy"]:
                    self._reply(200, "{'success':false,'msg':'检测到使用代理软件，"
                                     "请停止使用后重新登录'}".encode("utf-8"))
                elif state["flux_unsupported"]:
                    self._reply(200, "{'success':false,'msg':'您已在线'}"
                                .encode("utf-8"))  # 会话其实活着
                else:
                    state["session_alive"] = True
                    self._reply(200, "{'success':true,'msg':'登录成功'}"
                                .encode("utf-8"))
            else:
                self._reply(404, b"")
    try:
        srv = http.server.ThreadingHTTPServer(("127.0.0.1", port), H)
    except OSError:
        return None
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv, seen, state


def t37_fake_online_e2e():
    """假在线端到端（AC 通用踢法复刻，汕大 18:42 首例观察）：路由恒绿 +
    会话被掐 → 真实 Keeper 经真实 HTTP 用 flux 真值在 2 次空表内判死，
    同拍重登复活，全程恰好 1 次登录（无风暴）。锚点视角从头到尾都是
    "在线"——单信号守护正是在这类部署上失明。"""
    import keeper as _k
    sim = _sim_ac_split()
    if sim is None:
        raise _Skip("127.0.0.1:80 被占用/无权限，端到端跳过（零副作用承诺）")
    srv, seen, state = sim
    old_probe_s = _k.SESSION_PROBE_S
    try:
        _k.SESSION_PROBE_S = 0.0            # 加速探针周期（等价生产 60s）
        k = _k.Keeper(_sim_cfg())
        k.tick()                            # 初始会话死：空表×1 防抖不翻转
        assert k.state == "online" and not state["session_alive"]
        k._last_login_at = 0.0
        k.tick()                            # 空表×2 → 判死 → 首登复活
        assert state["session_alive"] and k.state == "online"
        n0 = len(seen["logins"])
        state["session_alive"] = False      # AC 踢会话，路由不掐
        k.tick()                            # 空表×1 → 防抖不翻转
        assert k.state == "online", "单次空表不得翻转（防抖）"
        k._last_login_at = 0.0
        k.tick()                            # 空表×2 → 判死 + 同拍重连
        assert k.state == "online" and state["session_alive"] is True, \
            "flux 真值须在第二次空表就抓住假在线并自动重连"
        assert len(seen["logins"]) - n0 == 1, "恢复应恰好一次登录（无风暴）"
    finally:
        _k.SESSION_PROBE_S = old_probe_s
        _close_sim(srv)


def t37_accelerator_kick_bounded_then_recovers():
    """开加速器过夜（行为管理识别踢线，多所高校官方文档明令禁止代理）：
    AC 按行为特征踢会话，重登被「检测到使用代理软件」类硬拒 → 守护退避
    600 秒、期间探测再多也不再轰 login.php；用户关掉加速器后一键恢复。"""
    import keeper as _k
    sim = _sim_ac_split()
    if sim is None:
        raise _Skip("127.0.0.1:80 被占用/无权限，端到端跳过（零副作用承诺）")
    srv, seen, state = sim
    old_probe_s = _k.SESSION_PROBE_S
    try:
        _k.SESSION_PROBE_S = 0.0
        k = _k.Keeper(_sim_cfg())
        state["block_proxy"] = True         # 加速器开着：AC 行为识别
        state["session_alive"] = False      # 已被踢
        k.tick(); k._last_login_at = 0.0
        k.tick()                            # 判死 → login → 代理硬拒
        assert k.state == "rejected", k.state
        n_after_reject = len(seen["logins"])
        for _ in range(10):                 # 退避窗口内继续周期探测
            k._sess_at = 0.0
            k.tick()
        assert len(seen["logins"]) == n_after_reject, \
            "硬拒退避期间不得再轰登录（风暴防线）"
        state["block_proxy"] = False        # 用户关掉加速器
        k.request_reconnect()               # 点「立即检测重连」
        k._last_login_at = 0.0              # 全局30s节流由循环30单独守，此处加速
        k.tick(manual=True)
        assert k.state == "online" and state["session_alive"], "关掉后一键恢复"
    finally:
        _k.SESSION_PROBE_S = old_probe_s
        _close_sim(srv)


def t37_cookie_portal_probe_self_disables_e2e():
    """cookie 认会话型门户（异校通用性边界）：会话明明在线但 flux 恒空表
    → 判死→重登说「您已在线」→又空表，矛盾累计后探针自动停用；
    总登录次数有界（≤3），最终状态在线——绝不对着好会话无限重登。"""
    import keeper as _k
    sim = _sim_ac_split()
    if sim is None:
        raise _Skip("127.0.0.1:80 被占用/无权限，端到端跳过（零副作用承诺）")
    srv, seen, state = sim
    old_probe_s = _k.SESSION_PROBE_S
    try:
        _k.SESSION_PROBE_S = 0.0
        state["flux_unsupported"] = True    # 门户恒空表
        state["session_alive"] = True       # 会话其实一直好
        k = _k.Keeper(_sim_cfg())
        for _ in range(12):
            k._last_login_at = 0.0
            k.tick()
        assert len(seen["logins"]) <= 3, \
            f"cookie 型门户登录必须收敛（实际 {len(seen['logins'])}）"
        assert k._sess_disabled_until > 0.0, "3 次矛盾后探针须自停"
        assert k.state == "online", "自停后正常凭锚点在线"
    finally:
        _k.SESSION_PROBE_S = old_probe_s
        _close_sim(srv)


check("假在线端到端（路由通会话死族）", t37_fake_online_e2e)
check("加速器踢线：硬拒退避防风暴+关后恢复", t37_accelerator_kick_bounded_then_recovers)
check("cookie门户探针自停（登录收敛≤3）", t37_cookie_portal_probe_self_disables_e2e)


def t38_free_window_parse_and_wrap():
    """免认证时段配置解析（通用机制，非某校特例）：跨午夜窗口
    「23:30-06:30」在 23:45/06:00 命中、12:00 不命中；非法项跳过不崩。"""
    import keeper as _k
    k = _k.Keeper(_cfg(free_windows="23:30-06:30,12:00-12:30"))
    assert k._in_free_window(23 * 60 + 45) is True    # 23:45 跨午夜前段
    assert k._in_free_window(6 * 60) is True          # 06:00 跨午夜后段
    assert k._in_free_window(12 * 60 + 10) is True    # 午休窗口
    assert k._in_free_window(12 * 60 + 40) is False   # 窗口外
    assert _k.Keeper(_cfg())._in_free_window(23 * 60 + 45) is False, \
        "缺省（未配置）必须永不命中——异校零改动行为不变"
    bad = _k.Keeper(_cfg(free_windows="abc,25:00-99:99,01:00-02:00"))
    assert bad._in_free_window(1 * 60 + 30) is True, "合法项须保留"
    assert bad._in_free_window(12 * 60) is False, "非法项须跳过且不致命"


def t38_free_window_suppresses_probe_and_overrides_dead():
    """免认证时段（用户口述：23:30 后无需登录）：门户不维护会话，flux 空表
    是常态而非踢线证据——窗口内不得判死、不得登录、状态显示免认证；
    窗口开启前遗留的判死标记必须被覆盖（否则 23:31 起每 tick 都去撞
    门户的冻结墙——今晚 23:32 冻结 600s 的直接机制）。"""
    import keeper as _k
    import time as _t
    k = _k.Keeper(_cfg(free_windows="00:00-24:00"))   # 全天窗口=测试任意时刻命中
    k.probe_online = lambda: True
    k.probe_l2_restricted = lambda: False
    k.fetch_flux = lambda: {}                          # 免认证时段：恒空表
    k._l2_at = _t.monotonic()
    k._session_dead = True                             # 窗口前遗留的判死
    logins = []
    k.do_login = lambda: logins.append(1) or (True, "ok")
    k.clash_recover = lambda: None
    k._sess_at = 0.0
    k.tick()
    assert logins == [], "免认证时段不得自动登录"
    assert k._session_dead is False, "遗留判死必须被窗口覆盖清除"
    assert k.state == "online" and "免认证" in k.state_detail, \
        f"状态须显示免认证时段: {k.state!r}/{k.state_detail!r}"
    k._sess_at = 0.0
    k.tick()
    assert logins == [] and k.state == "online", "窗口内持续静默"


def t38_login_rate_guard_caps_relogin_storm():
    """自动重登频率熔断（今晚 25+ 次重登触发门户反暴力冻结的防线）：会话
    被反复踢线时（每次重登后 flux 只活一拍又空），1 小时内第 LOGIN_CAP
    次成功登录后停止自动重登并留状态说明；手动「立即检测重连」不受限。"""
    import keeper as _k
    import time as _t
    k = _k.Keeper(_cfg())
    k.probe_online = lambda: True
    k.probe_l2_restricted = lambda: False
    k.portal_alive = lambda: True
    k.clash_recover = lambda: None
    box = {"alive_once": False, "logins": 0}

    def flux():
        # 复刻"AC 反复踢线"：重登成功后 flux 只活一拍，随后又空
        if box["alive_once"]:
            box["alive_once"] = False
            return {"用户名称": "u"}
        return {}
    k.fetch_flux = flux

    def login():
        box["logins"] += 1
        box["alive_once"] = True       # 重登确实恢复了会话
        return True, "登录成功"
    k.do_login = login
    for _ in range(60):
        k._sess_at = 0.0
        k._last_login_at = 0.0
        k._l2_at = _t.monotonic()
        k.tick()
        if k._login_cap_until > 0.0:
            break
    n_auto = box["logins"]
    assert n_auto == k.LOGIN_OK_CAP, \
        f"频率熔断应在第 {k.LOGIN_OK_CAP} 次成功登录触发，实际 {n_auto}"
    assert _t.monotonic() < k._login_cap_until, "熔断须生效"
    for _ in range(10):
        k._sess_at = 0.0
        k._l2_at = _t.monotonic()
        k.tick()
    assert box["logins"] == n_auto, "熔断期内不得再自动登录"
    k._last_login_at = 0.0              # 30s 全局节流由循环30单独守，此处加速
    k.tick(manual=True)                # 手动不受限
    assert box["logins"] == n_auto + 1, "手动重连须绕过熔断"


check("免认证时段解析与跨午夜", t38_free_window_parse_and_wrap)
check("免认证时段压制探针并覆盖遗留判死", t38_free_window_suppresses_probe_and_overrides_dead)


def t38b_free_window_silences_offline_path_and_freeze():
    """免认证时段第二课（2026-11-06 00:0x 实测三状态循环）：窗口内 AC ACL
    变化使锚点探测间歇失败——守护把 anchor 失败当掉线去登录，门户答
    NOAUTH not taken（登录任务无人接手=门户侧印证此段不受理认证），
    且「3 连在线自愈」反复解除冻结闸门造成冻结循环。修复后：窗口内
    一切自动登录禁止（手动可越）、冻结闸门在窗口内不可被自愈解除。"""
    import keeper as _k
    import time as _t
    k = _k.Keeper(_cfg(free_windows="00:00-24:00"))
    logins = []
    k.portal_alive = lambda: True
    k.clash_recover = lambda: None
    k.do_login = lambda: logins.append(1) or (True, "ok")
    k._frozen_until = _t.monotonic() + 600.0   # 遗留冻结（门户反暴力闸门）
    # 阶段1：anchor 挂（免认证常态）→ 离线路径不得登录
    k.probe_online = lambda: False
    for _ in range(5):
        k._last_login_at = 0.0
        k.tick()
    assert logins == [], "免认证时段离线路径不得自动登录"
    assert k._frozen_until > _t.monotonic(), "冻结闸门不得被触碰"
    # 阶段2：anchor 抖回绿（间歇放行）→ 3 连在线也不得自愈解除冻结
    k.probe_online = lambda: True
    k._l2_at = _t.monotonic()
    k._sess_at = 0.0
    for _ in range(4):
        k._last_login_at = 0.0
        k._l2_at = _t.monotonic()
        k._sess_at = 0.0
        k.tick()
    assert logins == [], "窗口内 3 连在线也不得触发登录"
    assert k._frozen_until > _t.monotonic(), "窗口内冻结闸门不得被自愈解除"
    assert k.state == "online" and "免认证" in k.state_detail, \
        f"窗口内正常探测应显示免认证在线: {k.state!r}/{k.state_detail!r}"


check("免认证时段离线路径静默+冻结不自愈", t38b_free_window_silences_offline_path_and_freeze)

print("== 循环39：IPv6 出口观察 + 熔断停时校准 ==")


def _fake_socket(has_v6, connect_ok):
    """构造假 socket 模块行为：has_v6=有无全局 v6 路由；connect_ok=v6 TCP 通不通"""
    class _Err(OSError):
        pass

    class FakeSock:
        def __init__(self, fam, typ):
            self.fam = fam
            self.typ = typ
            self.timeout = None
        def settimeout(self, t):
            self.timeout = t
        def connect(self, addr):
            host = addr[0]
            if not has_v6:
                raise _Err("no route")
            if self.typ == 2:                       # SOCK_DGRAM：选源
                self._src = "2001:250:3003::1"
            else:                                  # SOCK_STREAM：出口探测
                if not connect_ok:
                    raise _Err("unreachable")
        def getsockname(self):
            return (getattr(self, "_src", "fe80::1"), 0, 0, 0)
        def close(self):
            pass
    mod = type(sys)("fake_socket")
    mod.AF_INET6 = 23
    mod.SOCK_DGRAM = 2
    mod.SOCK_STREAM = 1
    mod.socket = FakeSock
    return mod


def t39_ipv6_probe_three_states():
    """probe_ipv6 三态（汕大 2026-11-06 实测：v6 不经 portal，观察不判定）：
    无 v6 路由→None（不误报）；有 v6 可达→True；有 v6 出不去→False。"""
    import keeper as _k
    import time as _t
    k = _k.Keeper(_cfg())
    old = _k.socket
    try:
        _k.socket = _fake_socket(has_v6=False, connect_ok=False)
        assert k.probe_ipv6() is None, "无 v6 路由必须报 None 而非 False"
        _k.socket = _fake_socket(has_v6=True, connect_ok=True)
        assert k.probe_ipv6() is True
        _k.socket = _fake_socket(has_v6=True, connect_ok=False)
        assert k.probe_ipv6() is False
    finally:
        _k.socket = old


def t39_ipv6_observation_never_affects_offline():
    """v6 观察绝不参与掉线判定（v4 会话是认证主线）：v6 全灭时 v4 正常
    仍是在线；且 detail 提示「IPv6 不可达」；DPI 窗口+v6 可达要联合提示。"""
    import keeper as _k
    import time as _t
    k = _k.Keeper(_cfg())
    k.probe_online = lambda: True
    k.probe_l2_restricted = lambda: False
    k.probe_ipv6 = lambda: False
    k.fetch_flux = lambda: {"用户名称": "u"}
    k._l2_at = 0.0
    k.tick()
    assert k.state == "online", "v6 不可达绝不能判掉线"
    assert "IPv6 不可达" in k.state_detail, k.state_detail
    k.probe_ipv6 = lambda: True
    k.probe_l2_restricted = lambda: True
    k._l2_at = 0.0
    k.tick()
    assert "DPI" in k.state_detail and "IPv6 可达" in k.state_detail, \
        f"DPI×v6 联合提示缺失: {k.state_detail!r}"


def t39_login_cap_is_15_minutes():
    """熔断停时校准（2026-11-06 下午 37 次重登零冻结 vs 5 次 1 小时停摆）：
    停 15 分钟——降速而非停摆，防冻结主防线仍是 30s 节流+退避。"""
    import keeper as _k
    k = _k.Keeper(_cfg())
    assert k.LOGIN_CAP_S == 900.0, k.LOGIN_CAP_S


check("probe_ipv6 三态不误报", t39_ipv6_probe_three_states)
check("v6 观察不参与掉线+联合提示", t39_ipv6_observation_never_affects_offline)
check("熔断停时=15分钟", t39_login_cap_is_15_minutes)
check("自动重登频率熔断（手动不受限）", t38_login_rate_guard_caps_relogin_storm)

print(f"\n结果: PASS={len(PASS)} FAIL={len(FAIL)} SKIP={len(SKIP)}"
      + ("（真实门户项未启用）" if not LIVE and SKIP else ""))
sys.exit(1 if FAIL else 0)
