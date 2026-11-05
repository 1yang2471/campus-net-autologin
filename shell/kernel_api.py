# -*- coding: utf-8 -*-
"""内核/外壳接口契约（shell 先行开发的接缝）。

外壳只依赖本文件定义的抽象；MockKernel 让外壳在无网络、无真内核的情况下
开发与测试，生产由 app_main.KeeperFacade 适配运行中的 Keeper。
"""
from __future__ import annotations

from typing import Callable, Protocol

# 状态快照：外壳渲染所需的全部信息（只读）
STATE_LABELS = {
    "online": "在线",
    "offline": "离线，自动重连中",
    "portal_down": "认证服务器不可达",
    "rejected": "登录被拒",
    "starting": "启动中",
}

STATE_COLORS = {
    # 与托盘 TRAY_COLORS 同源降饱和（石墨世界里的语义色，Material 默认太霓虹）
    "online": "#4f9d63",
    "offline": "#d1913f",
    "portal_down": "#d1913f",
    "rejected": "#d05f52",
    "starting": "#8b919b",
}


class Kernel(Protocol):
    """外壳眼中的内核。实现方：MockKernel（测试）/ KeeperFacade（接入 keeper）。"""

    def snapshot(self) -> dict:
        """返回 {state, detail, interval, reject_remaining}。"""
        ...

    def force_reconnect(self) -> None:
        """触发一次立即检测重连（等价托盘"立即检测重连"）。"""
        ...

    def fetch_flux(self) -> dict:
        """返回流量信息 dict（{'用户名称': ..., '已用流量': ...} 或 {'错误': ...}）。"""
        ...

    def on_change(self, callback: Callable[[], None]) -> None:
        """注册状态变化回调（外壳据此刷新 UI；回调须线程安全，由实现方保证）。"""
        ...

    def save_credentials(self, username: str, password: str, server: str) -> None:
        """保存配置（v1.0 起 keyring 存密码，ini 存其余）。"""
        ...

    def test_credentials(self, username: str, password: str, server: str) -> tuple[bool, str]:
        """向导"保存并立即测试"：用给定凭据试登录一次，返回 (成功, 人话消息)。"""
        ...

    def detect_portal(self) -> str:
        """自动探测校园门户基址（未认证劫持页）；探测不到返回 ""。"""
        ...

    def get_settings(self) -> dict:
        """设置页回显：当前生效的配置 dict（键同 config.ini）。"""
        ...

    def save_settings(self, values: dict) -> None:
        """设置页保存：持久化并立即生效（实现方负责热重载）。"""
        ...


class MockKernel:
    """外壳开发的假内核：状态自动轮换，无需网络。"""

    needs_setup = False  # 与 KeeperFacade/SetupFacade 接口对齐

    def __init__(self) -> None:
        import threading

        self._state = "online"
        self._detail = ""
        self._cb: list[Callable[[], None]] = []
        self._lock = threading.Lock()
        t = threading.Thread(target=self._rotate, daemon=True)
        t.start()

    def _rotate(self) -> None:
        import time

        cycle = ["online", "online", "offline", "online", "rejected", "portal_down"]
        i = 0
        while True:
            time.sleep(4)
            with self._lock:
                i = (i + 1) % len(cycle)
                self._state = cycle[i]
                self._detail = {
                    "rejected": "密码错误或额度不足（演示）",
                    "portal_down": "30 秒后重试（演示）",
                }.get(self._state, "")
            for cb in self._cb:
                try:
                    cb()
                except Exception:
                    pass

    def snapshot(self) -> dict:
        with self._lock:
            return {"state": self._state, "detail": self._detail,
                    "interval": 3, "reject_remaining": 0}

    def force_reconnect(self) -> None: ...

    def fetch_flux(self) -> dict:
        return {"用户名称": "演示账号", "已用流量": "1.2 GB", "剩余流量": "2.8 GB"}

    def on_change(self, callback: Callable[[], None]) -> None:
        self._cb.append(callback)

    def save_credentials(self, username: str, password: str, server: str) -> None:
        self._detail = f"已保存（演示）：{username} @ {server}"
        self._state = "online"

    def test_credentials(self, username: str, password: str, server: str) -> tuple[bool, str]:
        return True, "演示环境：凭据校验通过"

    def detect_portal(self) -> str:
        return "http://10.0.0.1"

    def get_settings(self) -> dict:
        return {"interval": "3", "probe_timeout": "3", "campus_dns": "10.0.0.1"}

    def save_settings(self, values: dict) -> None:
        self._detail = f"已保存设置（演示）：{sorted(values)}"
