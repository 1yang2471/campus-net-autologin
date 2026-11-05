# -*- coding: utf-8 -*-
"""凭据存储：Windows 凭据管理器（keyring）存密码，config.ini 只存占位。

降级链：keyring 不可用（域机/精简系统等）→ 明文 ini + 日志告警（README 声明）。
兼容迁移：读到旧版明文密码时自动迁入凭据管理器并擦除 ini 中的明文。
"""
from __future__ import annotations

import logging

from .keeper_paths import read_ini, write_ini

SERVICE = "campus-net-autologin"
PLACEHOLDER = "<keyring>"


def _keyring():
    import keyring
    return keyring


def load_password(username: str) -> str:
    """优先凭据管理器；兼容旧明文 ini 并自动迁移。

    迁移前提：ini 中同时记录的 username 与查询的 username 一致——
    否则任何未注册账号的查询都会把别人的密码"张冠李戴"迁到自己名下
    （2026-11-04 02:39 冒烟测试实测事故：真实密码被迁到测试 key 下）。
    """
    try:
        pwd = _keyring().get_password(SERVICE, username)
        if pwd:
            return pwd
    except Exception as exc:  # noqa: BLE001 keyring 后端异常时降级
        # 瞬时故障会返回 "" → 启动环误判"未配置"丢回首跑向导；留一条日志
        # 让用户在 keeper.log 里能看出是凭据管理器的问题（P2-11）。
        logging.getLogger("keeper").warning(
            "读取凭据管理器失败（%s），尝试兼容旧明文配置", exc)
    ini = read_ini()
    ini_user = (ini.get("keeper", "username", fallback="") or "").strip()
    legacy = (ini.get("keeper", "password", fallback="") or "").strip()
    if ini_user and username == ini_user and legacy and legacy != PLACEHOLDER:
        try:
            save_password(username, legacy)  # 迁移进凭据管理器
        except Exception:  # noqa: BLE001
            return legacy  # 迁移失败则继续用明文（不阻塞登录）
        # 擦除 ini 明文 → 占位
        if ini.has_section("keeper"):
            ini.set("keeper", "password", PLACEHOLDER)
            write_ini(ini)
        return legacy
    return ""


def save_password(username: str, password: str) -> None:
    _keyring().set_password(SERVICE, username, password)


if __name__ == "__main__":
    import sys
    if len(sys.argv) == 4 and sys.argv[1] == "set":
        save_password(sys.argv[2], sys.argv[3])
        print("已存入凭据管理器")
    elif len(sys.argv) == 3 and sys.argv[1] == "get":
        print(load_password(sys.argv[2]) or "(空)")
    elif len(sys.argv) == 3 and sys.argv[1] == "del":
        _keyring().delete_password(SERVICE, sys.argv[2])
        print("已删除")
    else:
        print("用法: python credentials.py set|get|del <username> [password]")
