# -*- coding: utf-8 -*-
"""账号历史（多账号切换）：config.ini [accounts] 段，密码走 keyring 不落盘。

段格式（序号即最近使用顺序，1 最新；当前账号以 [keeper] username 为准）：
    [accounts]
    1 = 20231234|https://portal.example.edu
    2 = 20231235|https://portal.example.edu

密码不进 ini：save_password(username, pwd) 与主凭据同一 keyring 条目
（shell.credentials），切换账号 = 换用户名查 keyring。
"""
from __future__ import annotations

from .keeper_paths import read_ini, write_ini

MAX_ACCOUNTS = 5   # 历史上限：超出淘汰最久未用的（挤不进前 5 的先走）


def list_accounts() -> list[tuple[str, str]]:
    """[(username, server), ...] 按最近使用排序；段损坏时返回空列表。"""
    parser = read_ini()
    if not parser.has_section("accounts"):
        return []
    out: list[tuple[str, str]] = []
    for key, val in parser.items("accounts"):
        if "|" not in val:
            continue
        user, _, server = val.partition("|")
        user, server = user.strip(), server.strip()
        if user:
            out.append((user, server))
    return out


def remember_account(username: str, server: str) -> None:
    """把账号记入历史顶部；重复账号提到顶部不新增（密码另走 keyring）。"""
    username = (username or "").strip()
    if not username:
        return
    entries = [(u, s) for u, s in list_accounts() if u != username]
    entries.insert(0, (username, (server or "").strip()))
    del entries[MAX_ACCOUNTS:]
    parser = read_ini()
    if not parser.has_section("accounts"):
        parser.add_section("accounts")
    for key in list(parser["accounts"]):
        parser.remove_option("accounts", key)
    for i, (u, s) in enumerate(entries, 1):
        parser.set("accounts", str(i), f"{u}|{s}")
    write_ini(parser)
