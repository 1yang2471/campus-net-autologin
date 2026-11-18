#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""cnald 凭据桥：keyring → 子进程环境变量（内存传递）→ cnald.exe。

与 cnal_bridge.py 同一契约：凭据只从 Windows 凭据管理器读（守护同一存储），
经环境变量传给子进程，不落盘、不打印、不写日志；本文件不含凭据字面量。
子进程参数列表 + shell=False（无 shell 拼接面）。

用法：
  python cnald_bridge.py --check          # 只验配置/凭据/cnald.exe 就位，不触网
  python cnald_bridge.py --config cnal.ini  # 跑守护（Ctrl+C 停）
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
    exe = HERE / "cnald.exe"
    if not exe.is_file():
        raise SystemExit(f"缺 {exe.name}——先 go build -o cnald.exe ./cmd/cnald")

    if "--check" in sys.argv:
        username, _ = gather()  # 密码读到即可，绝不显示
        print(f"OK: 守护配置 {CONFIG_FILE}")
        print(f"OK: keyring 可读（用户 {username}，密码不显示）")
        print("OK: cnald.exe 就位")
        return 0

    username, password = gather()
    env = dict(os.environ)
    env["CNAL_USER"] = username
    env["CNAL_PASS"] = password
    # 参数列表 + shell=False：无任何 shell 拼接，可执行文件路径来自本目录固定名
    proc = subprocess.run(
        [str(exe)] + sys.argv[1:],
        shell=False, env=env,
    )
    return proc.returncode


if __name__ == "__main__":
    sys.exit(main())
