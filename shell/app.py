# -*- coding: utf-8 -*-
"""campus-net-autologin 外壳：工业仪表风主窗口（tkinter）。

设计方向：industrial instrument —— 单屏无滚动、状态即主视觉、暗色默认、
等宽遥测数字、克制动效（仅状态切换一次过渡）。它不是"漂亮面板"，
是设备指示灯：每一帧都在回答"系统正常吗"。

驱动：Kernel 抽象（shell/kernel_api.py）。开发期用 MockKernel，
生产由 app_main.KeeperFacade 适配运行中的 Keeper，外壳代码零改动。
"""
from __future__ import annotations

import queue
import threading
import tkinter as tk
from tkinter import messagebox, ttk

from .kernel_api import STATE_COLORS, STATE_LABELS  # noqa: E402
from .keeper_paths import LOG_FILE  # noqa: E402
from .theme import (BG, PANEL, PANEL_EDGE, FG, FG_DIM, MONO, DISPLAY,  # noqa: E402
                    DISPLAY_BOLD, DISPLAY_SMALL, apply_dark_ttk, lamp_photo)


class Shell(tk.Tk):
    """主窗口：状态仪表 + 操作区 + 设置表单（隐藏式）。"""

    def __init__(self, kernel, desk=None) -> None:
        super().__init__()
        self.kernel = kernel
        self.desk = desk
        self._detect_q: queue.Queue = queue.Queue()
        self._test_q: queue.Queue = queue.Queue()
        self._last_state = None
        self.title("campus-net-autologin")
        self.configure(bg=BG)
        apply_dark_ttk(self)
        self._icon = lamp_photo(master=self)  # 任务栏挂状态灯（显式绑根，见 theme 注释）
        self.iconphoto(True, self._icon)
        self.geometry("420x680")  # 含网络设置区；向导模式隐藏仪表后仍自适应
        self.resizable(False, True)
        self.attributes("-topmost", False)
        self._build()
        self._banner = None
        if getattr(kernel, "needs_setup", False):
            self._enter_wizard()
        self.update_idletasks()  # 初始高度贴合内容（"保存设置"不贴边）
        self.geometry(f"420x{max(self.winfo_reqheight(), 520)}")
        self._poll()

    # ---------------- 布局 ----------------

    def _build(self) -> None:
        # 状态仪表（主视觉：色块 + 大字状态 + 细节行）
        self.card = tk.Frame(self, bg=PANEL, highlightbackground=PANEL_EDGE,
                             highlightthickness=1)
        self.card.pack(fill="x", padx=20, pady=(20, 12))
        self.lamp = tk.Canvas(self.card, width=18, height=18, bg=PANEL,
                              highlightthickness=0)
        self.lamp.pack(anchor="w", padx=16, pady=(14, 0))
        self.state_text = tk.Label(self.card, text="—", font=DISPLAY_BOLD,
                                   bg=PANEL, fg=FG, anchor="w")
        self.state_text.pack(fill="x", padx=14)
        self.detail_text = tk.Label(self.card, text="", font=DISPLAY_SMALL,
                                    bg=PANEL, fg=FG_DIM, anchor="w")
        self.detail_text.pack(fill="x", padx=16, pady=(0, 14))

        # 遥测条（等宽数字：周期/退避）
        self.telemetry = tk.Label(self, text="", font=MONO, bg=BG, fg=FG_DIM,
                                  anchor="w")
        self.telemetry.pack(fill="x", padx=22)

        # 操作区
        btns = tk.Frame(self, bg=BG)
        btns.pack(fill="x", padx=20, pady=(14, 6))
        self.btns = btns  # 向导退出时用于 pack(before=) 恢复布局
        self._button(btns, "立即检测重连", self._on_force).pack(
            side="left", expand=True, fill="x", padx=(0, 6))
        self._button(btns, "查流量", self._on_flux).pack(
            side="left", expand=True, fill="x", padx=(6, 0))

        # 设置表单（T2/T3：换密码/换学校）
        form = tk.LabelFrame(self, text=" 设置 ", font=DISPLAY, bg=BG, fg=FG_DIM,
                             highlightbackground=PANEL_EDGE, bd=0)
        form.pack(fill="x", padx=20, pady=(14, 0))
        self._labeled(form, "账号", "e_user", 0)
        self._labeled(form, "密码", "e_pwd", 1, show="•")
        self._labeled(form, "服务器", "e_server", 2)
        self._button(form, "自动检测", self._auto_detect).grid(
            row=2, column=2, padx=(0, 12), pady=4)
        self._detect_hint = tk.Label(form, text="", font=DISPLAY_SMALL, bg=BG,
                                     fg=FG_DIM, anchor="w")
        self._detect_hint.grid(row=3, column=0, columnspan=3, sticky="ew",
                               padx=12, pady=(0, 2))
        row = tk.Frame(form, bg=BG)
        row.grid(row=4, column=0, columnspan=3, sticky="ew", padx=12, pady=(2, 12))
        self._save_btn = self._button(row, "保存并立即测试", self._on_save_test)
        self._save_btn.pack(side="left", expand=True, fill="x", padx=(0, 6))
        self._button(row, "打开日志", self._on_log).pack(
            side="left", expand=True, fill="x", padx=(6, 0))
        # 历史账号：只读下拉（免打字），选中即切（密码走 keyring 恢复）
        tk.Label(form, text="历史账号", font=DISPLAY, bg=BG, fg=FG_DIM).grid(
            row=5, column=0, sticky="w", padx=(12, 6), pady=4)
        self.c_account = ttk.Combobox(form, state="readonly", font=DISPLAY)
        self.c_account.grid(row=5, column=1, sticky="ew", padx=(0, 12), pady=4)
        self.c_account.bind("<<ComboboxSelected>>", self._on_switch_account)
        self._acct_widgets = [w for w in form.grid_slaves(row=5)]
        form.columnconfigure(1, weight=1)

        # 网络设置（通用化：适配新学校不必碰 config.ini；常用 4 项 + 高级折叠）
        net = tk.LabelFrame(self, text=" 网络设置 ", font=DISPLAY, bg=BG,
                            fg=FG_DIM, highlightbackground=PANEL_EDGE, bd=0)
        net.pack(fill="x", padx=20, pady=(10, 0))
        self._labeled(net, "校园 DNS", "e_dns", 0)
        self._labeled(net, "探测间隔", "e_interval", 1)
        self._labeled(net, "探测超时", "e_ptimeout", 2)
        self._labeled(net, "绑定网卡", "e_bindip", 3)
        # 自动重连开关：关 = 只探测报状态、掉线不自动登录（手动「立即检测重连」
        # 仍可放行一次）。做成按钮而非 Checkbutton+tk.Variable：多 Tk 根环境下
        # Variable 晚于根销毁会在解释器退出期触发 Tcl_AsyncDelete（实测 exit 3）。
        row_auto = tk.Frame(net, bg=BG)
        row_auto.grid(row=4, column=0, columnspan=3, sticky="ew", padx=12, pady=(4, 2))
        self._auto_on = True
        self._btn_auto = self._button(row_auto, "", self._toggle_auto)
        self._btn_auto.pack(fill="x", expand=True)
        # 高级设置：常驻隐藏小窗（建时 withdraw；开=deiconify，关=withdraw）。
        # 内嵌展开会把主面板撑出屏幕（截图实测 1057px），独立小窗与辅助窗
        # 模型一致且字段常驻，关窗不丢编辑内容。
        self._adv_win = tk.Toplevel(self)
        self._adv_win.title("高级设置")
        self._adv_win.configure(bg=BG)
        self._adv_win.geometry("420x540")
        self._adv_win.resizable(False, True)
        self._adv_win.protocol("WM_DELETE_WINDOW", self._adv_win.withdraw)
        self._adv_win.withdraw()
        self._adv = tk.Frame(self._adv_win, bg=BG)
        self._adv.pack(fill="both", expand=True, padx=6, pady=(10, 6))
        adv_fields = [
            ("probe_ips", "探测锚点", "e_probeips"),
            ("login_page", "登录页路径", "e_loginpage"),
            ("user_agent", "浏览器标识", "e_ua"),
            ("ssl_verify", "证书校验", "e_ssl"),
            ("login_timeout", "登录超时", "e_ltimeout"),
            ("reject_backoff", "拒登退避", "e_reject"),
            ("transient_backoff", "瞬态退避", "e_transient"),
            ("portal_down_wait", "门户等待", "e_portaldown"),
            ("clash_api_url", "Clash 地址", "e_clash"),
            ("clash_api_secret", "Clash 密钥", "e_clashsecret"),
        ]
        for i, (_key, label, attr) in enumerate(adv_fields):
            tk.Label(self._adv, text=label, font=DISPLAY, bg=BG, fg=FG_DIM).grid(
                row=i, column=0, sticky="w", padx=(12, 6), pady=3)
            if attr == "e_ssl":
                e = ttk.Combobox(self._adv, values=("true", "false"),
                                 state="readonly", width=14, font=DISPLAY)
            else:
                e = ttk.Entry(self._adv, font=DISPLAY)
            e.grid(row=i, column=1, sticky="ew", padx=(0, 12), pady=3)
            setattr(self, attr, e)
        self._adv.columnconfigure(1, weight=1)
        tk.Label(self._adv_win, text="改完点主面板的\"保存设置\"一并生效",
                 font=DISPLAY_SMALL, bg=BG, fg=FG_DIM).pack(side="bottom",
                                                            pady=(0, 10))
        self._adv_btn = self._button(net, "高级设置…", self._toggle_adv)
        self._adv_btn.grid(row=15, column=0, columnspan=3, sticky="ew",
                           padx=12, pady=(2, 6))
        row2 = tk.Frame(net, bg=BG)
        row2.grid(row=16, column=0, columnspan=3, sticky="ew", padx=12, pady=(2, 10))
        self._button(row2, "保存设置", self._on_save_settings).pack(
            side="left", expand=True, fill="x")
        net.columnconfigure(1, weight=1)
        self._load_settings()

        # 底部：透明度声明行
        tk.Label(self, text="只自动登录本人账号，不绕过任何计费限制",
                 font=DISPLAY_SMALL, bg=BG, fg=FG_DIM).pack(side="bottom", pady=8)

    def _labeled(self, parent, text, attr, row, show=None):
        tk.Label(parent, text=text, font=DISPLAY, bg=BG, fg=FG_DIM).grid(
            row=row, column=0, sticky="w", padx=(12, 6), pady=4)
        e = ttk.Entry(parent, show=show, font=DISPLAY)
        e.grid(row=row, column=1, sticky="ew", padx=(0, 12), pady=4)
        setattr(self, attr, e)

    def _button(self, parent, text, cmd):
        b = tk.Button(parent, text=text, command=cmd, font=DISPLAY,
                      bg=PANEL, fg=FG, activebackground=PANEL_EDGE,
                      activeforeground=FG, relief="flat", bd=0,
                      highlightthickness=1, highlightbackground=PANEL_EDGE,
                      cursor="hand2", pady=4)
        return b

    # ---------------- 首配向导（T1 流失点） ----------------

    def _enter_wizard(self):
        self._banner = tk.Label(
            self, text="首次配置：填账号和密码即可，服务器自动探测",
            font=DISPLAY, bg="#5c410d", fg="#ffe9c2", anchor="w", padx=14, pady=8)
        self._banner.pack(fill="x", padx=20, pady=(20, 0), before=self.btns)
        self.card.pack_forget()          # 向导期隐藏状态仪表（还没有守护）
        self.telemetry.pack_forget()
        for w in self._acct_widgets:     # 历史切换依赖运行中的守护，向导期不适用
            w.grid_remove()
        self.e_server.delete(0, "end")   # 不预填任何学校域名：靠自动探测/手填
        self.e_user.focus_set()
        self._auto_detect()              # 少填：门户地址自动探测（未认证劫持页自报）

    def _auto_detect(self) -> None:
        """后台线程探测门户，结果经队列回 Tk 线程（跨线程不碰控件）。"""
        self._detect_hint.config(text="正在自动探测校园门户…", fg=FG_DIM)

        def worker():
            try:
                got = self.kernel.detect_portal()
            except Exception:  # noqa: BLE001 探测失败不阻塞手填
                got = ""
            self._detect_q.put(got)
        threading.Thread(target=worker, daemon=True).start()

    def _apply_detect(self, got: str) -> None:
        if got:
            self.e_server.delete(0, "end")
            self.e_server.insert(0, got)
            self._detect_hint.config(text=f"已探测到门户：{got}", fg="#63b073")
        else:
            self._detect_hint.config(
                text="没探测到（可能已登录或不在校园网），请手动填写", fg="#d1a54a")

    def _toggle_adv(self):
        if self._adv_win.winfo_viewable():
            self._adv_win.withdraw()
        else:
            self._adv_win.deiconify()
            self._adv_win.lift()

    def _toggle_auto(self) -> None:
        """开关只改本地待保存态（与其它设置字段一致：点「保存设置」才生效）。"""
        self._auto_on = not self._auto_on
        self._render_auto()

    def _render_auto(self) -> None:
        # 状态双通道表达：符号 ●/○ + 文字（关/开）——颜色不单独承载语义
        mark, word = ("●", "开") if self._auto_on else ("○", "关")
        self._btn_auto.config(text=f"掉线自动重连 {mark}（当前：{word}）")

    def _on_switch_account(self, _event=None) -> None:
        """历史账号选中 → 切账号（密码从 keyring 还原；取不到则拒绝切换并
        提示补密，绝不把旧账号密码带进新账号）。表单账号/服务器一律回显
        选中项，失败时用户直接补密码点保存。"""
        user = self.c_account.get().strip()
        if not user:
            return
        try:
            ok, msg = self.kernel.switch_account(user)
        except Exception as exc:  # noqa: BLE001 切换异常也要说人话
            ok, msg = False, f"切换失败：{exc}"
        server = next((s for u, s in self._accounts if u == user), "")
        self.e_user.delete(0, "end")
        self.e_user.insert(0, user)
        if server:
            self.e_server.delete(0, "end")
            self.e_server.insert(0, server)
        self._detect_hint.config(text=msg,
                                 fg="#63b073" if ok else "#d1a54a")

    # 设置页字段 → cfg 键映射（数字字段空值=不改；字符串空值=清除/自动）
    _SETTING_FIELDS = [
        ("campus_dns", "e_dns", False), ("interval", "e_interval", True),
        ("probe_timeout", "e_ptimeout", True), ("bind_ip", "e_bindip", False),
        ("probe_ips", "e_probeips", False), ("login_page", "e_loginpage", False),
        ("user_agent", "e_ua", False), ("ssl_verify", "e_ssl", False),
        ("login_timeout", "e_ltimeout", True), ("reject_backoff", "e_reject", True),
        ("transient_backoff", "e_transient", True),
        ("portal_down_wait", "e_portaldown", True),
        ("clash_api_url", "e_clash", False), ("clash_api_secret", "e_clashsecret", False),
    ]

    def _load_settings(self) -> None:
        cfg = {}
        try:
            cfg = self.kernel.get_settings()
        except Exception:  # noqa: BLE001 假内核/旧内核：字段留空即可
            pass
        # 凭据区回显账号与服务器（密码不回显——keyring 里，留空即不改）
        for attr, key in (("e_user", "username"), ("e_server", "server")):
            e = getattr(self, attr)
            e.delete(0, "end")
            e.insert(0, str(cfg.get(key, "")))
        for _key, attr, _num in self._SETTING_FIELDS:
            e = getattr(self, attr)
            e.delete(0, "end")
            val = str(cfg.get(_key, "")) if not isinstance(e, ttk.Combobox) else \
                ("true" if str(cfg.get(_key, "false")).lower() == "true" else "false")
            e.insert(0, val)
        self._auto_on = str(cfg.get("auto_reconnect", "true")).lower() == "true"
        self._render_auto()
        # 历史账号下拉（免打字）：密码走 keyring 恢复，选中即切（见
        # _on_switch_account）。历史直接读 ini [accounts] 段（只读、容错，
        # 读失败=空列表不阻塞面板），不经内核：避免为 UI 细节扩张协议。
        try:
            from .keeper_paths import read_ini
            parser = read_ini()
            self._accounts = []
            if parser.has_section("accounts"):
                for _k, val in parser.items("accounts"):
                    if "|" not in val:
                        continue
                    u, _, s = val.partition("|")
                    if u.strip():
                        self._accounts.append((u.strip(), s.strip()))
        except Exception:  # noqa: BLE001 历史读失败不阻塞面板
            self._accounts = []
        self.c_account.config(values=[u for u, _ in self._accounts])

    def _on_save_settings(self) -> None:
        values: dict[str, str] = {}
        for key, attr, numeric in self._SETTING_FIELDS:
            e = getattr(self, attr)
            v = e.get().strip() if not isinstance(e, ttk.Combobox) else e.get()
            if numeric and v:
                try:
                    float(v)
                except ValueError:
                    messagebox.showerror("填写不合法",
                                         f"「{key}」必须是数字，当前：{v}", parent=self)
                    return
            if numeric and not v:
                continue  # 数字字段留空 = 保持原值
            values[key] = v
        values["auto_reconnect"] = "true" if self._auto_on else "false"
        # bind_ip 非法（IPv6/乱填）会让全链路静默不通，写盘前拦下（P2-9）
        from keeper import validate_bind_ip
        if not validate_bind_ip(values.get("bind_ip", "")):
            messagebox.showerror("填写不合法",
                                 "「绑定网卡 IP」必须是合法 IPv4 地址（留空=自动发现）；"
                                 "IPv6 暂不支持", parent=self)
            return
        try:
            self.kernel.save_settings(values)
        except Exception as exc:  # noqa: BLE001 写配置失败要说人话
            messagebox.showerror("保存失败", str(exc), parent=self)
            return
        messagebox.showinfo("已保存", "设置已保存并立即生效", parent=self)

    # ---------------- 状态渲染（克制动效：仅切换时一次颜色过渡） ----------------

    def _poll(self):
        try:  # 回收后台探测结果（仅 Tk 线程改控件）
            while True:
                self._apply_detect(self._detect_q.get_nowait())
        except queue.Empty:
            pass
        try:  # 回收后台凭据测试结果（保存/提示只在 Tk 线程做）
            while True:
                self._finish_save_test(*self._test_q.get_nowait())
        except queue.Empty:
            pass
        snap = self.kernel.snapshot()
        state = snap["state"]
        color = STATE_COLORS.get(state, FG_DIM)
        label = STATE_LABELS.get(state, state)

        if state != self._last_state:
            self._last_state = state
            self.state_text.config(text=label, fg=color)
            # 指示灯：外圈描边 + 实心（设备灯语义）
            self.lamp.delete("all")
            self.lamp.create_oval(2, 2, 16, 16, outline=color, width=2)
            self.lamp.create_oval(5, 5, 13, 13, fill=color, outline="")
            self.card.config(highlightbackground=color)
            self.state_text.update_idletasks()

        detail = snap.get("detail") or ""
        remain = snap.get("reject_remaining") or 0
        if remain:
            detail = f"{detail}，{int(remain)} 秒后重试" if detail else f"{int(remain)} 秒后重试"
        self.detail_text.config(text=detail)
        self.telemetry.config(text=f"PROBE_INTERVAL {snap['interval']}s")

        self.after(500, self._poll)

    # ---------------- 操作（Command 接口） ----------------

    def _on_force(self):
        self.kernel.force_reconnect()

    def _on_flux(self):
        if self.desk is not None:
            # 与托盘同一实现（Desk 独立 Tk 线程，取数不阻塞面板）
            self.desk.post("flux", kernel=self.kernel)
            return
        data = self.kernel.fetch_flux()
        text = "\n".join(f"{k}：{v}" for k, v in data.items())
        messagebox.showinfo("流量信息", text, parent=self)

    def _on_save_test(self):
        user = self.e_user.get().strip()
        pwd = self.e_pwd.get()
        server = self.e_server.get().strip()
        if not user or not server:
            messagebox.showwarning("缺信息", "账号和服务器必填", parent=self)
            return
        # P2-5：网络探测（DNS + POST，最坏十余秒）放后台线程，UI 不冻结；
        # 结果经队列回 Tk 线程做保存/弹窗（跨线程不碰控件）。
        self._save_btn.config(state="disabled", text="正在测试…")
        self._test_ctx = (user, pwd, server)

        def worker():
            try:
                result = self.kernel.test_credentials(user, pwd, server)
            except Exception as exc:  # noqa: BLE001 后台异常也要回主线程提示
                result = (False, f"测试异常: {exc}")
            self._test_q.put(result)
        threading.Thread(target=worker, daemon=True).start()

    def _finish_save_test(self, ok, msg):
        """凭据测试结果（Tk 线程）：恢复按钮并处理保存/提示。"""
        self._save_btn.config(state="normal", text="保存并立即测试")
        user, pwd, server = self._test_ctx
        wizard = getattr(self.kernel, "needs_setup", False)
        if wizard and not pwd:
            # 留空=不改 已存凭据的语义只对面板成立；首配空密码永远无法就绪，
            # 会掉进"保存→未就绪→向导"静默循环（审查 P3-19），当场拒绝。
            messagebox.showwarning(
                "缺密码", "首次配置请填写密码。\n（密码留空=不改，仅适用于修改已保存的凭据）",
                parent=self)
            return
        if not ok:
            # P1-2：不在校园网/门户宕机时也要能先把凭据存好，
            # 联网后的重试交给常驻守护，不要让首配卡死在联网测试上。
            save_anyway = messagebox.askyesno(
                "测试失败", f"{msg or '未知错误'}\n\n"
                "可能是不在校园网或门户暂时不可达。仍要保存凭据吗？\n"
                "（保存后守护会在联网时自动重试登录）", parent=self)
            if not save_anyway:
                return
            self.kernel.save_credentials(user, pwd, server)
            if wizard:
                self.destroy()  # 保存即交棒，下轮启动守护
                return
            messagebox.showinfo("已保存", "凭据已保存，守护会在联网时自动重试", parent=self)
            return
        self.kernel.save_credentials(user, pwd, server)
        # 向导保存成功 → 关窗交棒：app_main 启动环检测到凭据就绪即拉起守护
        if wizard:
            messagebox.showinfo("已保存", "登录验证通过，守护即将开始工作", parent=self)
            self.destroy()
            return
        messagebox.showinfo("已保存", msg or "凭据校验通过", parent=self)

    def _on_log(self):
        if self.desk is not None:
            self.desk.post("log", path=LOG_FILE)
            return
        log = LOG_FILE
        try:
            import os
            if os.name == "nt":
                os.startfile(log)  # noqa: S606 固定路径
            else:
                import webbrowser
                webbrowser.open(log.as_uri())
        except OSError:
            messagebox.showerror("无法打开", f"日志不存在或无关联程序：\n{log}", parent=self)


def main() -> None:
    from .kernel_api import MockKernel  # 仅 dev 独立运行用
    kernel = MockKernel()
    Shell(kernel).mainloop()


if __name__ == "__main__":
    main()
