#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""cnal rebind 凭据桥：keyring → 子进程环境变量（内存传递）→ cnal.exe。

- 凭据只从 Windows 凭据管理器（keyring）读，与守护同一存储：
  复用 campus-net-autologin/shell/credentials.py 的生产读取路径，不另造逻辑；
- 凭据经子进程环境变量传给 cnal，不落盘、不打印、不写日志；
- 本文件不含也不得含任何凭据字面量（t41 系守卫会拦）；
- 子进程用参数列表 + shell=False 启动（无 shell 拼接面）。

用法：
  python cnal_bridge.py --check   # 只验配置/凭据可读 + cnal.exe 就位，不触网
  python cnal_bridge.py           # 执行 cnal rebind（免认证时段勿跑，白耗预算）
"""
from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
CAMPUS_REPO = HERE.parent / "campus-net-autologin"
sys.path.insert(0, str(CAMPUS_REPO))

from shell.credentials import load_password  # noqa: E402
from shell.keeper_paths import CONFIG_FILE, read_ini  # noqa: E402


def gather() -> tuple[str, str]:
    parser = read_ini()
    if not parser.has_section("keeper"):
        raise SystemExit(f"守护配置缺失或无 [keeper] 节: {CONFIG_FILE}")
    username = parser.get("keeper", "username", fallback="").strip()
    if not username:
        raise SystemExit("守护配置里没有 username")
    password = load_password(username)
    if not password:
        raise SystemExit(f"keyring 里读不到 {username} 的密码（守护是否保存过？）")
    return username, password


def main() -> int:
    exe = HERE / "cnal.exe"
    ini = HERE / "cnal.ini"
    if not exe.is_file():
        raise SystemExit(f"缺 {exe.name}——先 go build -o cnal.exe ./cmd/cnal")
    if not ini.is_file():
        raise SystemExit(f"缺 {ini.name}")

    if "--check" in sys.argv:
        username, _ = gather()  # 密码读到即可，绝不显示
        print(f"OK: 守护配置 {CONFIG_FILE}")
        print(f"OK: keyring 可读（用户 {username}，密码不显示）")
        print("OK: cnal.exe / cnal.ini 就位")
        return 0

    username, password = gather()
    env = dict(os.environ)
    env["CNAL_USER"] = username
    env["CNAL_PASS"] = password
    # 参数列表 + shell=False：无任何 shell 拼接，可执行文件路径来自本目录固定名
    proc = subprocess.run(
        [str(exe), "rebind", "--config", str(ini)],
        shell=False, env=env,
    )
    return proc.returncode


if __name__ == "__main__":
    sys.exit(main())
