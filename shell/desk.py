# -*- coding: utf-8 -*-
"""辅助窗口宿主（Desk）：托盘与面板共用的唯一入口。

为什么需要它：
1. tkinter 不是线程安全的——托盘回调在 pystray 线程，面板在另一个 Tk 线程，
   跨线程直接操作控件会随机崩。故 Desk 独占一个 Tk 线程，其他线程只 post 动作。
2. 托盘"查流量"与面板"查流量"必须是同一个界面（用户明确要求一致），
   共用 Desk 才能保证「同一实现」，而不是两份代码各自漂移。

动作：
- ("flux", kernel)  → 开流量信息窗，后台取数后填充
- ("log", path)     → 开日志窗（尾部若干行 + 自动刷新）
"""
from __future__ import annotations

import logging
import queue
import threading

from .windows import FluxWindow, LogWindow

POLL_MS = 100  # 动作队列轮询间隔（UI 线程内 Tk 定时器，非阻塞 sleep）


class Desk:
    """独立 Tk 线程的辅助窗口宿主。post() 线程安全。"""

    def __init__(self) -> None:
        self._actions: queue.Queue = queue.Queue()
        self._results: queue.Queue = queue.Queue()
        self._thread: threading.Thread | None = None
        self._root = None
        self._ready = threading.Event()
        self._stop = threading.Event()
        self.flux_window: FluxWindow | None = None
        self.log_window: LogWindow | None = None

    # ---------------- 生命周期 ----------------

    def start(self) -> None:
        if self._thread is not None:
            return
        self._thread = threading.Thread(target=self._run, daemon=True,
                                        name="desk-tk")
        self._thread.start()
        self._ready.wait(5)

    def stop(self) -> None:
        self._stop.set()
        if self._thread is not None:
            self._thread.join(timeout=5)
            self._thread = None

    def _run(self) -> None:
        try:
            import tkinter as tk
            from .theme import apply_dark_ttk, lamp_photo
            root = tk.Tk()
            root.withdraw()
            apply_dark_ttk(root)  # ttk 样式按根生效：不应用则日志窗滚动条是浅色原生条
            self._icon = lamp_photo(master=root)  # 辅助窗统一状态灯（显式绑根，见 theme 注释）
            root.iconphoto(True, self._icon)
            self._root = root
            root.after(POLL_MS, self._poll)
            self._ready.set()
            try:
                root.mainloop()
            finally:
                # 只 quit 不 destroy（实测 2026-11-04：后台线程的 Tk 根一旦显式
                # destroy，进程退出时必报 Tcl_AsyncDelete 并以 rc=3 崩溃；不
                # destroy 则无论引用在哪个线程都干净退出）。解释器随进程回收。
                self._root = None
        except Exception as exc:  # noqa: BLE001 无桌面会话（RDP/服务/会话未就绪）
            # Tk 起不来时置位 _ready 让 start() 立即返回——否则白等 5 秒
            # （审查 P3-13）；托盘气泡兜底日志
            import logging
            logging.getLogger("keeper").warning("辅助窗宿主不可用（%s），降级托盘气泡", exc)
            self._ready.set()

    # ---------------- 外部线程入口 ----------------

    def post(self, action: str, **payload) -> None:
        """投递动作（任意线程安全）。"""
        self._actions.put((action, payload))

    # ---------------- Tk 线程内部 ----------------

    def _poll(self) -> None:
        while True:
            try:
                action, payload = self._actions.get_nowait()
            except queue.Empty:
                break
            try:
                self._handle(action, payload)
            except Exception as exc:  # noqa: BLE001 窗口失败不应拖垮宿主
                logging.getLogger("keeper").warning("辅助窗口动作失败 %s: %s",
                                                    action, exc)
        while True:
            try:
                kind, value = self._results.get_nowait()
            except queue.Empty:
                break
            self._apply_result(kind, value)
        if self._stop.is_set():
            if self._root is not None:
                self._root.quit()
            return
        if self._root is not None:
            self._root.after(POLL_MS, self._poll)

    def _handle(self, action: str, payload: dict) -> None:
        if action == "flux":
            self._open_flux(payload.get("kernel"))
        elif action == "log":
            self._open_log(payload.get("path"))

    def _open_flux(self, kernel) -> None:
        win = self._alive(self.flux_window)
        if win is None:
            self.flux_window = FluxWindow(self._root)
            win = self.flux_window
        win.deiconify()
        win.lift()
        self._fetch_flux(kernel, win)

    def _fetch_flux(self, kernel, win: FluxWindow) -> None:
        """取数放后台线程（门户请求可能耗时数秒，不能阻塞 Tk 线程）。"""

        def worker() -> None:
            try:
                data = kernel.fetch_flux()
            except Exception as exc:  # noqa: BLE001
                self._results.put(("flux_error", str(exc)))
                return
            self._results.put(("flux_result", data))

        threading.Thread(target=worker, daemon=True, name="flux-fetch").start()

    def _open_log(self, path) -> None:
        if path is None:
            return
        win = self._alive(self.log_window)
        if win is None:
            self.log_window = LogWindow(self._root, path)
            win = self.log_window
        win.deiconify()
        win.lift()

    def _apply_result(self, kind: str, value) -> None:
        win = self._alive(self.flux_window)
        if win is None:
            return
        if kind == "flux_result":
            win.set_result(value or {})
        elif kind == "flux_error":
            win.set_error(value)

    @staticmethod
    def _alive(win):
        """窗口是否仍存在（用户可能已关掉）。"""
        if win is None:
            return None
        try:
            return win if win.winfo_exists() else None
        except Exception:  # noqa: BLE001
            return None
