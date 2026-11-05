# -*- coding: utf-8 -*-
"""开机自启：Windows 注册表 HKCU\\...\\Run 键（用户级，无需管理员）。

2026-11-04 审查 P1-5/P1-6 后从 schtasks 改为注册表：
- schtasks /Create ONLOGON 在本机被组策略 Access denied；且 /TR 相对命令无
  起始目录，注册出来的任务根本跑不起来。
- 启动文件夹 .bat 用 ascii 编码会静默丢弃中文路径字符。
注册表 Run 键用 winreg（stdlib）写绝对路径，无子进程、无编码坑、无黑窗一闪，
源码与 frozen exe 都适用。非 Windows 平台 no-op（产品仅承诺 Windows 支持，
README 已收窄；Linux 用户可自行加 systemd unit）。
"""
from __future__ import annotations

import sys
from pathlib import Path

RUN_KEY = r"Software\Microsoft\Windows\CurrentVersion\Run"
VALUE_NAME = "campus-net-autologin"


def _command() -> str:
    """自启命令：frozen 用 exe 自身；源码用 pythonw + app_main.py 绝对路径。"""
    if getattr(sys, "frozen", False):
        return f'"{Path(sys.executable).resolve()}"'
    root = Path(__file__).resolve().parent
    py = Path(sys.executable)
    w = py.with_name("pythonw.exe")
    interpreter = w if w.exists() else py
    return f'"{interpreter}" "{root / "app_main.py"}"'


def _winreg():
    import winreg
    return winreg


def register() -> bool:
    if sys.platform != "win32":
        return False
    try:
        winreg = _winreg()
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, RUN_KEY, 0,
                            winreg.KEY_SET_VALUE) as key:
            winreg.SetValueEx(key, VALUE_NAME, 0, winreg.REG_SZ, _command())
        return True
    except OSError:
        return False


def unregister() -> bool:
    if sys.platform != "win32":
        return False
    try:
        winreg = _winreg()
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, RUN_KEY, 0,
                            winreg.KEY_SET_VALUE) as key:
            winreg.DeleteValue(key, VALUE_NAME)
        return True
    except FileNotFoundError:
        return True  # 本就没有，视为已关闭
    except OSError:
        return False


def is_enabled() -> bool:
    if sys.platform != "win32":
        return False
    try:
        winreg = _winreg()
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, RUN_KEY) as key:
            val, _ = winreg.QueryValueEx(key, VALUE_NAME)
        return bool(val)
    except OSError:
        return False


def current_command() -> str:
    """读取当前注册值（供测试/诊断，不回显敏感信息——路径不含密码）。"""
    if sys.platform != "win32":
        return ""
    try:
        winreg = _winreg()
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, RUN_KEY) as key:
            val, _ = winreg.QueryValueEx(key, VALUE_NAME)
        return str(val)
    except OSError:
        return ""
