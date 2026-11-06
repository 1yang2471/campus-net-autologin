# -*- coding: utf-8 -*-
"""可选 exe 出包命令生成器（面向不会 Python 的同学；主分发仍是源码）。

本脚本只**生成并打印** PyInstaller 命令，不自行执行进程（安全边界：
构建工具由用户显式运行，避免程序内 spawn 的注入面）。

防误报措施（2026-11 调研）：--onedir（onefile 解压行为更易被杀软报毒）、
--noconsole（无黑框）、PyInstaller≥4.3（EXE checksum）、显式 --name。
未签名 exe 首次运行仍可能弹 SmartScreen——README 已写应对话术。

用法：python build_exe.py 查看命令，复制执行；产物在 dist/campus-net-autologin/
"""
from __future__ import annotations

import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent
APP_NAME = "campus-net-autologin"


def build_command() -> list[str]:
    """返回 PyInstaller 参数列表（全字面量，无任何外部输入参与构造）。"""
    return [
        sys.executable, "-m", "PyInstaller",
        "--noconfirm", "--clean",
        "--onedir", "--noconsole",
        "--name", APP_NAME,
        # 应用图标：开始菜单/任务栏/设置-apps 页同款绿灯（缺失则重出包后
        # 快捷方式图标断链，2026-11-06 装机化改造时踩过）
        "--icon", str(ROOT / "app.ico"),
        # 源码零引用但 Pillow 的可选依赖被采集（实测 dist 里躺着 27MB
        # numpy）——排除出去，包体积近乎减半
        "--exclude-module", "numpy",
        "--paths", str(ROOT),
        "--hidden-import", "shell.app",
        "--hidden-import", "shell.kernel_api",
        "--hidden-import", "shell.credentials",
        "--hidden-import", "shell.accounts",
        "--hidden-import", "shell.keeper_paths",
        "--hidden-import", "shell.desk",
        "--hidden-import", "shell.windows",
        "--hidden-import", "shell.theme",
        "--hidden-import", "autostart",
        # 托盘与凭据后端按名字动态加载，PyInstaller 追不到（P2-26：
        # 漏了会 frozen 下静默降级成"无托盘/无 keyring"）
        "--hidden-import", "pystray._win32",
        "--hidden-import", "keyring.backends.Windows",
        str(ROOT / "app_main.py"),
    ]


def main() -> None:
    print("复制以下命令执行出包（需已 pip install pyinstaller）：\n")
    for arg in build_command():
        print(arg)
    print(f"\n产物目录: {ROOT / 'dist' / APP_NAME}/")


if __name__ == "__main__":
    main()
