# -*- coding: utf-8 -*-
"""keeper 项目路径与 ini 读写的单一职责模块（内核与外壳共用）。

集中 CONFIG_FILE/LOG_FILE 定位与 ini 读写下沉，
避免 shell/credentials.py 反向 import keeper.py（会拉起整个内核）。
"""
from __future__ import annotations

import configparser
import io
import os
import sys
from pathlib import Path


def _data_dir() -> Path:
    """与 keeper.py 一致的数据目录（%LOCALAPPDATA%\\campus-net-autologin）。"""
    base = os.environ.get("LOCALAPPDATA") or os.environ.get("APPDATA")
    root = Path(base) if base else Path.home() / ".local" / "share"
    d = root / "campus-net-autologin"
    d.mkdir(parents=True, exist_ok=True)
    return d


BASE_DIR = _data_dir()
CONFIG_FILE: Path = BASE_DIR / "config.ini"
LOG_FILE: Path = BASE_DIR / "keeper.log"

# 版本常量：面板标题/启动日志/冒烟取证三处消费（trouble-maker P0：v1.0.4
# 前无任何版本指纹，用户无法分辨运行的是哪个构建——"看起来没修好"的根因）
APP_NAME = "campus-net-autologin"
APP_VERSION = "1.0.7"


def read_ini() -> configparser.ConfigParser:
    """utf-8-sig 兼容记事本 BOM；interpolation=None 兼容密码含 %。"""
    parser = configparser.ConfigParser(interpolation=None)
    if CONFIG_FILE.is_file():
        parser.read(CONFIG_FILE, encoding="utf-8-sig")
    return parser


def write_ini(parser: configparser.ConfigParser) -> None:
    """原子写（与 save_config 同一纪律）：中途崩溃不留半截 ini
    （半截 config 会让下次启动直接 SystemExit，审查 P2-8）。"""
    buf = io.StringIO()
    parser.write(buf)
    tmp = CONFIG_FILE.with_suffix(".tmp")
    tmp.write_text(buf.getvalue(), encoding="utf-8")
    os.replace(tmp, CONFIG_FILE)
