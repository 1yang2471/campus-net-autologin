#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""campus-net-autologin — ac_portal 系校园网自动掉线检测与自动重连守护。

适用：ac_portal 门户的「朴素变体」（明文密码、无 auth_tag；登录接口/表单/
响应可在 config.ini 配置）。带 auth_tag/密码加密的变体与 eportal/Srun 等
异协议族不在范围——先用 compat_check.py --detect 判定适用性。
默认值不绑定任何学校：server 由向导探测或手填，campus_dns 留空走系统 DNS。

掉线检测（三层防误判）：公网 204 探测 → 认证服务器存活确认 → 登录响应二次校验。
被拒（额度用尽/密码错）→ 记录原因并单独长退避，不空转。
掉线恢复：典型 <= interval + 2 秒；最坏 ≈ interval + (锚点数+2)×probe_timeout +
login_timeout（默认配置约 18 秒；trouble-maker P1-2 与 README 统一口径）。

代理/加速器共存：trust_env=False；默认不绑定网卡（实测 TUN 下绑定反而不通），
可选 bind_ip 逃生口；恢复成功后可选 mihomo API 联动（flush fake-ip、清死连接）。
"""

from __future__ import annotations

import configparser
import getpass
import io
import json
import logging
from logging.handlers import RotatingFileHandler
import os
import re
import socket
import sys
import threading
import time
import webbrowser
from pathlib import Path

import requests
import urllib3

# 校园网门户多为自签证书；config.ini 里 ssl_verify=true 可打开校验
urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

# ---------------- 常量与配置 ----------------

def _data_dir() -> Path:
    """配置/日志/锁统一目录：%LOCALAPPDATA%\\campus-net-autologin。

    与运行形态解耦（源码/exe 共用同一份），根治 2026-11-04 审查 P1-7：
    两形态各用各的目录 → 两把锁两份配置 → 双实例同账号互踢。
    """
    base = os.environ.get("LOCALAPPDATA") or os.environ.get("APPDATA")
    root = Path(base) if base else Path.home() / ".local" / "share"
    d = root / "campus-net-autologin"
    d.mkdir(parents=True, exist_ok=True)
    return d


BASE_DIR = _data_dir()
CONFIG_FILE: Path = BASE_DIR / "config.ini"
LOG_FILE: Path = BASE_DIR / "keeper.log"

# 通用默认值：不绑定任何具体学校。server 留空由向导自动探测/手填，
# campus_dns 留空时解析走系统 DNS（异校零配置可用），锚点用公共 DNS IP。
# 各校差异一律走 config.ini / 面板，不再内置参考校地址。
LAN_PROBE_FALLBACK = "223.5.5.5"  # 通用公网地址：仅用于 UDP connect 选路，不出包

DEFAULTS = {
    "server": "",
    "username": "",
    "password": "",
    "probe_timeout": "3",
    "login_timeout": "0",  # 0 = 自适应 max(6, 2×probe_timeout)（P1-7）
    "interval": "3",
    "reject_backoff": "600",
    "transient_backoff": "30",
    "portal_down_wait": "30",
    "ssl_verify": "false",
    "bind_ip": "",
    "clash_api_url": "",
    "clash_api_secret": "",
    "login_page": "/ac_portal/default/pc.html?tabs=pwd",
    "login_path": "/ac_portal/login.php",
    "flux_path": "/ac_portal/userflux",
    "probe_path": "/generate_204",
    "probe_host": "connect.rom.miui.com",
    "campus_dns": "",
    "probe_ips": "223.5.5.5,119.29.29.29",
    "user_agent": ("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
                   "(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
}

FORM_BASE = {"opr": "pwdLogin", "rememberPwd": "1"}


def clamp(value: float, low: float, high: float, name: str) -> float:
    """配置数值钳制：越界取边界并告警，避免填错导致脚本异常。"""
    if low <= value <= high:
        return value
    logging.getLogger("keeper").warning("配置 %s=%s 超出 [%s, %s]，已钳制", name, value, low, high)
    return max(low, min(high, value))


def _read_ini() -> configparser.ConfigParser:
    """集中读取 config.ini（%LOCALAPPDATA% 固定常量路径，无用户输入参与）。

    utf-8-sig 兼容记事本另存的 BOM；解析失败给出可读指引而非裸栈回溯。
    """
    parser = configparser.ConfigParser(interpolation=None)
    if CONFIG_FILE.is_file():
        try:
            parser.read(CONFIG_FILE, encoding="utf-8-sig")
        except (configparser.Error, UnicodeDecodeError) as exc:
            raise SystemExit(
                f"config.ini 解析失败：{exc}\n"
                "请用 UTF-8（不带 BOM）编码保存；记事本用户请另存为时选 UTF-8") from exc
    return parser


def setup_logging() -> None:
    log = logging.getLogger("keeper")
    log.setLevel(logging.INFO)
    fmt = logging.Formatter("%(asctime)s %(levelname)s %(message)s", "%Y-%m-%d %H:%M:%S")
    file_h = RotatingFileHandler(LOG_FILE, maxBytes=1_000_000, backupCount=3, encoding="utf-8")
    file_h.setFormatter(fmt)
    log.addHandler(file_h)
    if sys.stdout and sys.stdout.isatty():
        try:
            sys.stdout.reconfigure(encoding="utf-8", errors="replace")
        except Exception:  # noqa: BLE001 老终端不支持 reconfigure
            pass
        con = logging.StreamHandler()
        con.setFormatter(fmt)
        log.addHandler(con)


def _migrate_legacy_config() -> None:
    """把旧位置（脚本/exe 同级）的 config.ini 迁到新数据目录，防升级后误判首跑。"""
    if CONFIG_FILE.exists():
        return
    # 仅当 CONFIG_FILE 指向真实数据目录时才迁移（测试隔离：临时目录不触发）
    if CONFIG_FILE.parent != _data_dir():
        return
    legacy_dir = (Path(sys.executable).resolve().parent if getattr(sys, "frozen", False)
                  else Path(__file__).resolve().parent)
    legacy = legacy_dir / "config.ini"
    if legacy.is_file() and legacy != CONFIG_FILE:
        try:
            CONFIG_FILE.write_bytes(legacy.read_bytes())
            logging.getLogger("keeper").info("已从 %s 迁移配置到 %s", legacy, CONFIG_FILE)
        except OSError:
            pass


def load_config() -> dict:
    _migrate_legacy_config()
    cfg = dict(DEFAULTS)
    parser = _read_ini()
    if parser.has_section("keeper"):
        # 值清洗（trouble-maker P1-13/P2-17）：strip 掉首尾空白与换行——
        # "server = https://x " 尾空格会让 DNS 永远失败；\r\n 可注入裸 HTTP
        # 请求头。password 例外：明文遗留时不得篡改（空格可能是密码的一部分）。
        cfg.update({k: (v if k == "password" else v.strip())
                    for k, v in parser.items("keeper")})
    if parser.has_section("response"):
        # 异校响应词表：[response] 段的键加前缀存入同一 cfg，_marks() 消费。
        cfg.update({"response." + k: v.strip()
                    for k, v in parser.items("response")})
    return cfg


def load_form_extras() -> dict:
    """[form] 节：各校 ac_portal 表单差异字段（可选），覆盖/扩展默认表单。"""
    parser = _read_ini()
    return dict(parser["form"]) if parser.has_section("form") else {}


def save_config(cfg: dict) -> None:
    """增量写：只更新 [keeper] 里的键，保留 [form] 等其它节（审查 P1-10）。

    原子写（trouble-maker P2-24）：先写临时文件再 os.replace——崩溃在写中途
    不会留下半截 config 让下次启动直接 SystemExit。
    """
    parser = _read_ini()  # 读回现有内容（含 [form]/[response]）
    if not parser.has_section("keeper"):
        parser.add_section("keeper")
    for k, v in cfg.items():
        if k.startswith("response."):
            continue  # [response] 合并键属另一段：任何旁路落盘都不得泄漏（审查 P2-5 根修）
        parser.set("keeper", k, str(v))
    buf = io.StringIO()
    parser.write(buf)
    tmp = CONFIG_FILE.with_suffix(".tmp")
    tmp.write_text(buf.getvalue(), encoding="utf-8")
    os.replace(tmp, CONFIG_FILE)
    logging.getLogger("keeper").info("已写入配置 %s", CONFIG_FILE)


def ensure_config(cfg: dict) -> dict | None:
    """无配置文件时交互式引导生成；pythonw 无 stdin 时报错并指引先手动跑一次。"""
    if cfg["username"] and cfg["password"]:
        return cfg
    if CONFIG_FILE.exists():
        logging.getLogger("keeper").error("config.ini 缺少 username/password，请编辑后重启")
        return None
    if not (sys.stdin and sys.stdin.isatty()):
        logging.getLogger("keeper").error(
            "未找到 config.ini：请在终端手动运行一次 python keeper.py 完成账号初始化")
        return None
    print("首次运行，请填写校园网账号（密码随后存入系统凭据管理器；"
          "不可用时暂存本地 config.ini，均请勿提交仓库或外传）")
    cfg["username"] = input("账号: ").strip()
    cfg["password"] = getpass.getpass("密码: ")  # 不 strip：首尾空格可能是密码的一部分（P2-16，与 GUI 一致）
    if not cfg["username"]:
        return None
    save_config(cfg)
    return cfg

# ---------------- 物理栈 HTTP 客户端（TUN 共存核心） ----------------

def _raw_http(host: str, ip: str, path: str = "/", method: str = "GET",
              body: str = "", headers: dict | None = None,
              bind_ip: str = "", timeout: float = 4.0,
              use_tls: bool = False, verify: bool = False,
              port: int = 0) -> tuple[int, dict, str]:
    """绕开 TUN 的裸 HTTP，返回 (状态码, 响应头 dict, 响应体前 2KB)。

    2026-11-04 实测：TUN 接管系统 DNS（fake-ip），绑定源后域名解析也失败，
    故必须由调用方先用校园网 DNS（内网可达，不被 TUN 劫持）解析出真实 IP。
    verify=True 时启用证书校验（校园门户多为自签，默认 False；审查 P2-22
    曾指出 ssl_verify 是死开关，现真正接入）。
    port=0 时按 scheme 取缺省 80/443；带显式端口的门户（:8080 等）由调用方
    经 _server_target 解析后透传（审查 P1-3：原硬编码端口，带端口门户必死）。
    """
    port = port or (443 if use_tls else 80)
    head = dict(headers or {})
    head.setdefault("Host", host)
    head.setdefault("Connection", "close")
    head.setdefault("Content-Length", str(len(body.encode())))
    # 头注入防线（P1-13）：ini 值中间换行会拆裂裸 HTTP 头，建连前即拒发
    for k, v in head.items():
        if "\r" in str(v) or "\n" in str(v):
            return 0, {}, "HeaderValueError"
    raw_sock = None
    try:
        raw_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        raw_sock.settimeout(timeout)
        if bind_ip:
            raw_sock.bind((bind_ip, 0))
        raw_sock.connect((ip, port))
        s = raw_sock
        if use_tls:
            import ssl
            ctx = ssl.create_default_context()
            if not verify:
                ctx.check_hostname = False
                ctx.verify_mode = ssl.CERT_NONE  # 校园门户自签证书
            s = ctx.wrap_socket(raw_sock, server_hostname=host)
        req = f"{method} {path} HTTP/1.1\r\n"
        req += "".join(f"{k}: {v}\r\n" for k, v in head.items())
        req += "\r\n" + body
        s.sendall(req.encode())
        chunks = []
        while sum(len(c) for c in chunks) < 8192:
            part = s.recv(4096)
            if not part:
                break
            chunks.append(part)
        raw = b"".join(chunks).decode("utf-8", errors="replace")
        head_part, _, resp_body = raw.partition("\r\n\r\n")
        lines = head_part.split("\r\n")
        parts = lines[0].split(" ") if lines[0].startswith("HTTP") else []
        status = int(parts[1]) if len(parts) > 1 else 0  # 畸形状态行不炸（P2-20）
        resp_headers = {}
        for ln in lines[1:]:
            k, sep, v = ln.partition(":")
            if sep:
                resp_headers[k.strip()] = v.strip()
        return status, resp_headers, resp_body[:2048]
    except (OSError, ValueError) as exc:
        # 带 host/path 上下文：只报异常类名时用户分不清是 DNS、bind_ip、
        # TLS 还是门户（P2-13）。不含 host 之外的用户输入，避免泄露。
        return 0, {}, f"{exc.__class__.__name__} @ {host}{path}"
    finally:
        # 审查 P2-24：异常路径也须关 socket（长夜断网数千次失败探测不能靠 GC）
        if raw_sock is not None:
            try:
                raw_sock.close()
            except OSError:
                pass


def physical_http(host: str, ip: str, path: str = "/", method: str = "GET",
                  body: str = "", headers: dict | None = None,
                  bind_ip: str = "", timeout: float = 4.0,
                  use_tls: bool = False, verify: bool = False,
                  port: int = 0) -> tuple[int, str]:
    """_raw_http 的 (状态码, 响应体前 2KB) 视图（调用方大多不关心响应头）。"""
    status, _, resp_body = _raw_http(host, ip, path, method, body, headers,
                                     bind_ip, timeout, use_tls, verify, port)
    return status, resp_body


def _server_target(server: str) -> tuple[str, int]:
    """server 字符串 → (host, port)。带显式端口（如 :8080）时解析之，缺省按
    scheme 80/443；无 scheme 视为 https（与 _use_tls 语义一致）。
    修复审查 P1-3：原先 _raw_http 硬编码端口，探测自填回的 http://host:8080
    门户必死；且 split("//")[-1] 会把 "host:8080" 整体当主机名去解析。"""
    from urllib.parse import urlparse
    s = (server or "").strip()
    if "://" not in s:
        s = "https://" + s
    u = urlparse(s)
    host = u.hostname or ""
    try:
        port = u.port
    except ValueError:
        port = None
    if not port:
        port = 443 if u.scheme != "http" else 80
    return host, port


def detect_portal(probe_ips: str = "", bind_ip: str = "", campus_dns: str = "",
                  timeout: float = 4.0, login_page: str = "", probe_path: str = "",
                  _http=None) -> str:
    """校园网免填探测：未认证时公网请求被门户 302 劫持，Location 自报门户地址。

    两道防线后才返回（凭据绝不在此链路发送，全程无凭据 GET）：
    1) parse_portal_location 拒绝非 http/https、环回、localhost、链路本地；
    2) GET 门户登录页验证 ac_portal/pwdLogin 指纹——钓鱼/路由器劫持页
       过不了这关，宁缺毋滥返回 ""（已登录/不在校园网时同样返回 ""）。
    门户是域名形态时用 campus_dns 解析（系统 DNS 可能被 TUN 劫持）。
    """
    _http = _http or _raw_http
    ips = [x.strip() for x in (probe_ips or DEFAULTS["probe_ips"]).split(",") if x.strip()]
    lp = (login_page or DEFAULTS["login_page"]).split("?")[0]
    pp = probe_path or DEFAULTS["probe_path"]
    for ip in ips:
        status, resp_headers, _ = _http(ip, ip, pp,
                                        bind_ip=bind_ip, timeout=timeout)
        if status not in (301, 302, 307, 308):
            continue  # 204/404 = 已认证真实到达公网；0 = 不可达
        base = parse_portal_location(resp_headers)
        if not base:
            continue
        from urllib.parse import urlparse
        u = urlparse(base)
        phost = u.hostname
        if phost.replace(".", "").isdigit():
            pip = phost
        else:
            pip = (campus_dns_resolve(phost, campus_dns, bind_ip, timeout)
                   or system_dns_resolve(phost))  # 未配校园 DNS 时直接回退系统 DNS
        if not pip:
            continue
        st2, _, body2 = _http(phost, pip, lp, bind_ip=bind_ip,
                              timeout=timeout, use_tls=(u.scheme == "https"),
                              port=_server_target(base)[1])
        if st2 and ("ac_portal" in body2 or "pwdLogin" in body2):
            return base
    return ""


def campus_dns_resolve(host: str, dns_ip: str = "",
                       bind_ip: str = "", timeout: float = 3.0) -> str:
    """用校园网 DNS 解析（UDP 53 直连，内网段不被 TUN 劫持）。

    dns_ip 为空表示未配置校园 DNS：不发查询、直接返回空，交由调用方
    回退系统 DNS（通用化：不内置任何学校的 DNS 地址）。
    审查 P2-25：回验事务 ID + 全程边界检查，畸形/串包应答一律拒绝。
    """
    if not dns_ip:
        return ""
    import secrets
    import struct
    qid = secrets.randbits(16)  # 事务 ID 须不可预测（防 DNS 应答伪造），CSPRNG
    sock = None
    try:
        q = struct.pack(">HHHHHH", qid, 0x0100, 1, 0, 0, 0)
        for label in host.split("."):
            enc = label.encode()
            if len(enc) > 63:
                return ""
            q += bytes([len(enc)]) + enc
        q += b"\x00" + struct.pack(">HH", 1, 1)
        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        sock.settimeout(timeout)
        if bind_ip:
            sock.bind((bind_ip, 0))
        sock.sendto(q, (dns_ip, 53))
        # 容忍串包：qid 不符时再读一次（trouble-maker P2-19：单包即弃会让
        # 偶发杂包把整轮判成"门户解析失败"）
        data = b""
        for _ in range(2):
            data, _ = sock.recvfrom(512)
            if len(data) >= 2 and struct.unpack(">H", data[0:2])[0] == qid:
                break
        if len(data) < 12:
            return ""
        if struct.unpack(">H", data[0:2])[0] != qid:  # 事务 ID 必须匹配
            return ""
        ancount = struct.unpack(">H", data[6:8])[0]
        i = 12
        # 跳过 question：长度前缀标签序列 + null + qtype + qclass
        while i < len(data) and data[i] != 0:
            if data[i] & 0xC0:  # 压缩指针出现在 question 区属异常
                return ""
            i += data[i] + 1
        i += 5
        for _ in range(ancount):
            if i + 10 > len(data):
                return ""
            if data[i] & 0xC0 == 0xC0:
                i += 2
            else:
                while i < len(data) and data[i] != 0:
                    i += data[i] + 1
                i += 1
            if i + 10 > len(data):
                return ""
            rtype, _, _, rdlen = struct.unpack(">HHIH", data[i:i + 10])
            i += 10
            if i + rdlen > len(data):  # rd 越界
                return ""
            if rtype == 1 and rdlen == 4:
                return ".".join(str(b) for b in data[i:i + 4])
            i += rdlen
    except (OSError, struct.error, IndexError):
        return ""
    finally:
        if sock is not None:
            try:
                sock.close()
            except OSError:
                pass
    return ""


def parse_portal_location(headers: dict) -> str:
    """从未认证时门户劫持页的 302 Location 提取门户基址（scheme+host[:port]）。

    安全边界（2026-11-04 通用化审查）：
    - 仅接受 http/https；拒绝 javascript/ftp 等一切其他 scheme；
    - 拒绝环回（127.x/::1）、localhost、链路本地（169.254.x）——这些永远
      不是校园门户；
    - 校园门户本就住在私有段（10.0.0.1/10.x/172.16-31/28.x），属产品目标，
      不拒绝；凭据绝不在此阶段发送——detect_portal 只做无凭据 GET 指纹验证。
    解析失败/不可信一律返回 ""（宁可不填，不猜）。
    """
    from urllib.parse import urlparse
    loc = ""
    for k, v in headers.items():
        if k.lower() == "location":
            loc = v.strip()
            break
    if not loc:
        return ""
    try:
        u = urlparse(loc)
    except ValueError:
        return ""
    if u.scheme not in ("http", "https") or not u.hostname:
        return ""
    host = u.hostname.lower()
    if host == "localhost" or host.endswith(".localhost"):
        return ""
    try:
        import ipaddress
        ip = ipaddress.ip_address(host)
        if ip.is_loopback or ip.is_link_local or ip.is_unspecified or ip.is_multicast:
            # 0.0.0.0/组播段永远不是校园门户（审查 P2-4）
            return ""
    except ValueError:
        pass  # 域名形态（如 portal.example.edu）：放行，由后续指纹验证把关
    port = f":{u.port}" if u.port else ""
    return f"{u.scheme}://{host}{port}"


# ---------------- 核心逻辑 ----------------

FAKEIP_PREFIX = "28."  # 参考校环境实测的 fake-ip 段（注意：mihomo 默认是
                       # 198.18.0.0/16，"28."只是本地现象——两段都要过滤，
                       # 见 EXCLUDED_SRC_PREFIXES；2026-11-04 评估 P-3）

# 不可作探测源的地址段：环回/APIPA/两代 fake-ip（198.18-199 为 mihomo 默认）
EXCLUDED_SRC_PREFIXES = ("127.", "169.254.", FAKEIP_PREFIX, "198.18.", "198.19.")

# L2 观察节流（trouble-maker P1-5）：在线期 60 秒一探；受限告警每小时去重
L2_PROBE_S = 60.0
L2_WARN_DEDUP_S = 3600.0

# 响应判定词表默认值：精确等于既有参考校/华为词典，参考校用户零改动。
# 异校在 config.ini 的 [response] 段覆盖（逗号分隔），见 README「异校适配」。
RESPONSE_DEFAULTS = {
    # 不含裸 "success"：英文门户的正向判定走 \bsuccess 正则（防 "unsuccessful"
    # 误命中，trouble-maker P1-9 护栏保留）。词表只承载中文强标记。
    "success_marks": "成功,登录成功,login_ok",
    "negative_marks": "未成功,不成功,失败,错误,error,fail",
    "transient_marks": "noauth,not taken,请求异常,空响应,timeout,timed out,无法判定",
    "freeze_marks": "冻结,freeze",
    "hijack_marks": "ac_portal,pwdLogin",
}

def _marks(cfg: dict | None, key: str) -> list[str]:
    """取词表：cfg 里 response.<key> 覆盖则用之，否则回落默认值。"""
    if cfg and cfg.get("response." + key):
        return [x.strip() for x in str(cfg["response." + key]).split(",") if x.strip()]
    return [x.strip() for x in RESPONSE_DEFAULTS[key].split(",") if x.strip()]


def compat_check(sample: str, cfg: dict | None = None) -> dict:
    """门户兼容性自检：喂一段登录响应样本，离线给出判定结论。

    不联网、不发凭据，回答「我校门户的响应会被判成什么」。异校可先抓一次
    登录响应，再用本工具验证词表是否适配；不适配就改 [response] 段重测。
    """
    ok, detail = Keeper.parse_login(sample, cfg)
    low = (sample or "").lower()
    transient = any(w in low for w in (x.lower() for x in _marks(cfg, "transient_marks")))
    if ok:
        rule = "success/ret_code 正向" if "success" in low or "ret_code" in low \
            else "伪 JSON success=true" if "'success'" in low \
            else "词表/短响应正向标记"
    elif detail == "空响应":
        rule = "空响应"
    elif "无法判定" in detail:
        rule = "长响应无强标记（无法判定）"
    elif transient:
        rule = "瞬态错误标记"
    else:
        rule = "负向词/服务器拒绝"
    return {"verdict": "success" if ok else "reject", "rule": rule,
            "detail": detail, "transient": transient}


# ---------------- 协议指纹（异校适用性判定） ----------------

# 校园网门户协议族特征（2026-11-04 公开抓包/开源脚本调研，非臆测）：
# 校园网认证是多协议割裂市场——ac_portal / eportal（Dr.COM、锐捷等）/ 深澜
# Srun 等，端点、表单、登录流程互不兼容；本工具只自动化 ac_portal 族。
FAMILY_MARKERS = {
    "eportal": ("interface.do", "eportal", "drcom", "ddddd=", "0mkkey"),
    "srun": ("srun",),
}

# ac_portal 族内变体标记：命中即"明文单 POST 登不进"，属流程级差异，
# 改 config.ini 无法适配（本工具暂不支持）。
AUTH_TAG_MARK = "auth_tag"   # 一次性令牌/时间戳：提交前须从页面取
PWD_ENCRYPT_MARKS = ("md5", "sha1", "sha256", "encrypt", "rsa", "aes")


def protocol_fingerprint(sample: str) -> dict:
    """登录页/抓包样本 → 协议族 + 适用性三态结论（离线，零凭据）。

    - 朴素 ac_portal（明文密码、无 auth_tag）→ compatible（本工具适用）
    - ac_portal 变体（auth_tag / 密码加密）→ variant_unsupported（端点对，
      流程不同，改 config 登不进）
    - eportal / srun 等异协议 → other_protocol；无任何特征 → unknown
    """
    low = (sample or "").lower()
    if "ac_portal" in low or "pwdlogin" in low:
        needs_tag = AUTH_TAG_MARK in low
        pwd_enc = any(m in low for m in PWD_ENCRYPT_MARKS)
        if needs_tag or pwd_enc:
            verdict = "variant_unsupported"
            advice = ("ac_portal 变体：提交前需取 auth_tag（一次性令牌）或对密码"
                      "加密后再 POST——流程级差异，改 config.ini 无法适配，本工具暂不支持。")
        else:
            verdict = "compatible"
            advice = ("朴素 ac_portal（明文密码、无 auth_tag）：按 README「异校适配清单」"
                      "填 server/campus_dns 即可试用，再用默认模式验证响应判定。")
        return {"family": "ac_portal", "verdict": verdict,
                "needs_auth_tag": needs_tag, "pwd_encrypted": pwd_enc,
                "advice": advice}
    for family, marks in FAMILY_MARKERS.items():   # 字典序即判定优先级
        if any(m in low for m in marks):
            return {"family": family, "verdict": "other_protocol",
                    "needs_auth_tag": False, "pwd_encrypted": False,
                    "advice": (f"非 ac_portal 门户（{family} 系），本工具不适用；"
                               "请另寻对应协议的自动化工具。")}
    return {"family": "unknown", "verdict": "unknown",
            "needs_auth_tag": False, "pwd_encrypted": False,
            "advice": ("样本中未识别出已知门户特征。请确认抓的是登录页或登录 POST"
                       "（应含 ac_portal/login.php 与 opr=pwdLogin 字样），"
                       "或对照 README「适用范围与异校适配」。")}


def is_usable_src(ip: str) -> bool:
    """该源地址可否绑定物理栈探测（空/环回/APIPA/fake-ip 一律不可）。"""
    return bool(ip) and not ip.startswith(EXCLUDED_SRC_PREFIXES)


def validate_bind_ip(value: str) -> bool:
    """手动 bind_ip 是否合法：留空（自动发现）或合法 IPv4 才放行。

    _raw_http 只建 AF_INET socket，IPv6/非法值会让全链路 bind 抛 OSError
    而静默不通（P2-9），故在写入配置前先拦下。
    """
    v = (value or "").strip()
    if not v:
        return True
    import ipaddress
    try:
        return ipaddress.ip_address(v).version == 4
    except ValueError:
        return False


def system_dns_resolve(host: str) -> str:
    """系统 DNS 解析（校园 DNS 不可达时的回退，评估 P1-1 补齐守护链路）。

    安全边界：解析对象永远是用户自己填的 server host——回退只解决"解析不
    出来"，不会把凭据发往任何猜测/劫持地址（解析失败仍返回 ""，调用方拒发）。
    """
    try:
        # 只取 IPv4 结果（_raw_http 只建 AF_INET socket，v6 结果会让全链路必败）
        infos = [a[4][0] for a in socket.getaddrinfo(host, None) if a[0] == socket.AF_INET]
        return infos[0] if infos else ""
    except OSError:
        return ""


def lan_probe_address(campus_dns: str) -> str:
    """物理栈选路探测地址：配置了 campus_dns 就直接用它本身。

    UDP connect 不出包，仅让内核按路由表选"可达该地址的那张网卡"——
    比 /16 段形态地址精确（/24 内网下 x.y.255.254 不在 on-link 范围，
    会错选默认路由/TUN；2026-11-04 评估 P-3）。未配置/非 IPv4 时回退
    通用公网地址（不是任何学校的专属段），选路由系统路由表决定。
    """
    import ipaddress
    try:
        ip = ipaddress.ip_address(campus_dns or "")
        if ip.version == 4:
            return str(ip)
    except ValueError:
        pass
    return LAN_PROBE_FALLBACK


def discover_physical_ip(campus_dns: str = "") -> str:
    """自动发现物理网卡 IPv4（源绑定探测用）。

    判据：有 IPv4、非 Loopback/169.254 APIPA、非 fake-ip 段（28.x）、
    有默认类网关语义（连向网关的 UDP socket 出口源）。
    2026-11-04 实测：TUN 接管公网与 DNS，但校园内网段仍走物理栈，
    且绑定物理源可强制流量绕开 TUN —— 这是"代理共存"探测的根基。

    通用化（v1.0 审查）：选路探测地址直接用 campus_dns 本身
    （见 lan_probe_address），任何学校内网段都适用。
    """
    probe = lan_probe_address(campus_dns)
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.connect((probe, 53))  # 直达校园 DNS 的路由；不出包
        src = s.getsockname()[0]
        s.close()
    except OSError:
        src = ""
    if not is_usable_src(src):
        # UDP connect 出口被 TUN 抢走时，回退枚举系统适配器：按接口名排除
        # 常见虚拟网卡（VMware/Hyper-V/WSL/VirtualBox/TAP/TUN），否则会选中
        # 虚拟栈 → 探测与登录全走虚拟网卡，正好复现 TUN 漏判（P2-7）。
        # bind_ip 手动指定仍是最终逃生口。
        VIRTUAL_NIC = ("vmware", "virtual", "vethernet", "wsl", "hyper-v",
                       "virtualbox", "tap-", "tun", "loopback", "docker",
                       "vmnet", "bridge", "npcap")
        try:
            import psutil  # requirements 已声明；缺失时返回 ""
            for name, addrs in psutil.net_if_addrs().items():
                if any(v in name.lower() for v in VIRTUAL_NIC):
                    continue
                for a in addrs:
                    if a.family == socket.AF_INET and is_usable_src(a.address):
                        return a.address
        except ImportError:
            pass
        return ""
    return src


def _already_online(msg: str) -> bool:
    """"已在线"类成功语义（重复登录不算失败）——锚定匹配。

    评估 P-3：旧版 `"在线" in msg` 子串匹配会把"账号不在线，请勿重复登录"
    这类拒绝判成成功；负向形态先排除，再认两种锚定正向。
    """
    t = msg or ""
    if re.search(r"[不未]在线", t):
        return False
    return bool(re.search(r"已在线|在线\s*[，,]?\s*(请勿|重复)", t))


class Keeper:
    """三层探测 + 自动登录 + 恢复联动的常驻守护。"""

    def __init__(self, cfg: dict) -> None:
        self.cfg = cfg
        self.log = logging.getLogger("keeper")
        self.reject_until = 0.0  # time.monotonic() 基准，避免墙钟回拨延长退避
        self.suspect = 0         # 连续"登录成功但探测仍失败"计数（登录风暴熔断）
        self.transient_streak = 0  # 连续瞬态拒绝计数（升级防风暴）
        self._l2_at = 0.0        # 上次 L2 探测时刻（节流）
        self._l2_warn_at = 0.0   # 上次 L2 告警时刻（去重）
        self._portal_ip = ""     # 门户真实 IP 缓存（校园 DNS 解析，TUN 下必需）
        self._portal_host = ""   # 缓存对应的 host（改 server 即失效）
        self._portal_ip_at = 0.0
        self._portal_port = 0    # 带端口门户的连接端口（_server_target 解析，P1-3）
        self._bind_retry_at = 0.0   # 上次物理源重发现时刻（portal_down 节流，P2-8）
        self._frozen_until = 0.0    # 门户限频冻结闸门（probe 恢复不清除，风暴封堵）
        self.extra_wait = 0.0
        self.state = "starting"
        self.state_detail = ""
        self.net_lock = threading.Lock()  # _portal_target 缓存跨线程保护（P1-3 已接线）
        self.force_evt = threading.Event()
        self.stop_evt = threading.Event()
        self.state_listeners = []  # 多播（审查 P2-30：面板 on_change 不得顶掉托盘 refresh）
        self._derive_runtime()
        self._resolve_bind_ip()

    def _derive_runtime(self) -> None:
        """cfg → 运行时参数（钳位/表单）。__init__ 与 apply_config 共用，
        保证热重载与冷启动推导出完全一致的状态（单一来源）。"""
        cfg = self.cfg
        self.form = {**FORM_BASE, "userName": cfg["username"], "pwd": cfg["password"],
                     **load_form_extras()}

        def _num(key: str, low: float, high: float) -> float:
            """非法数值键回落默认并告警（trouble-maker P1-2：原 float() 直接在
            __init__ 抛 ValueError → pythonw 下零输出静默退出，无日志无托盘）。"""
            try:
                return clamp(float(str(cfg.get(key, "")).strip() or DEFAULTS[key]),
                             low, high, key)
            except ValueError:
                self.log.warning("配置 %s=%r 不是合法数字，已回落默认值 %s",
                                 key, cfg.get(key), DEFAULTS[key])
                return clamp(float(DEFAULTS[key]), low, high, key)

        self.interval = _num("interval", 1, 300)
        self.probe_timeout = _num("probe_timeout", 1, 15)
        self.reject_backoff = _num("reject_backoff", 60, 86400)
        self.transient_backoff = _num("transient_backoff", 5, 600)
        self.portal_down_wait = _num("portal_down_wait", 5, 600)
        # 登录超时独立（P1-7：门户 RADIUS 后端 3-6 秒才回，复用 probe_timeout
        # 会"向导测过、守护老超时"）；0/缺省 = 自适应 max(6, 2×probe_timeout)
        try:
            _lt = float(str(cfg.get("login_timeout", "0")).strip() or 0)
        except ValueError:
            self.log.warning("配置 login_timeout=%r 不是合法数字，按 0（自适应）处理",
                             cfg.get("login_timeout"))
            _lt = 0.0
        self.login_timeout = clamp(_lt, 6, 60, "login_timeout") if _lt \
            else max(6.0, 2 * self.probe_timeout)

    def apply_config(self, cfg: dict) -> None:
        """设置页热重载（保存即生效）：重新推导运行时参数、失效门户 DNS
        缓存、重算物理源，并立即触发一次重连让新配置上岗。"""
        self.cfg = cfg
        self._derive_runtime()
        with self.net_lock:
            self._portal_ip = ""   # server/campus_dns 可能变了，缓存作废
            self._portal_ip_at = 0.0
        self._resolve_bind_ip()
        self.request_reconnect()

    def add_state_listener(self, cb) -> None:
        self.state_listeners.append(cb)

    def _resolve_bind_ip(self) -> None:
        """TUN 共存（2026-11-04 定案）：自动发现物理网卡源地址。

        探测/登录全走裸 socket（physical_http）绑定该源绕开 TUN——TUN 接管
        公网与 DNS 后这是唯一真实路径。（trouble-maker P2-1：原
        requests.Session/_BoundAdapter 构造后全仓零使用，已删。）
        """
        bind = str(self.cfg.get("bind_ip", "")).strip()
        if not bind:
            bind = discover_physical_ip(str(self.cfg.get("campus_dns", "")))
            if bind:
                self.log.info("物理栈探测源（自动发现）: %s", bind)
        self.bind_ip = bind
        if bind:
            self.log.info("已绑定网卡 IP %s", bind)
        else:
            self.log.warning("未发现可绑定物理网卡，退回系统路由（TUN 下探测可能失真）")

    def _headers(self) -> dict:
        return {"User-Agent": self.cfg["user_agent"],
                "Referer": self.cfg["server"] + self.cfg["login_page"]}

    def _tls_verify(self) -> bool:
        return str(self.cfg.get("ssl_verify", "false")).lower() == "true"

    def _use_tls(self) -> bool:
        """按 server scheme 决定端口/TLS（审查 P2-28：http-only 门户不能强打 443；
        scheme 大小写不敏感——HTTP:// 曾被误判走 TLS）。"""
        return not str(self.cfg["server"]).strip().lower().startswith("http://")

    def _portal_target(self) -> tuple[str, str]:
        """门户 (host, ip)：校园 DNS 解析并缓存（按 host 失效）。

        解析失败返回 ip=""（多半不在校园网）——调用方据此跳过，绝不把凭据
        发往任何猜测地址（2026-11-04 审查 P1-13：旧版 DNS 失败硬编码 10.0.0.1
        兜底，会把学号密码送往公网 Cloudflare）。

        net_lock（trouble-maker P1-3）：tick 线程、Desk 取数线程、面板改
        配置线程共用本缓存——锁住"查缓存→解析→写缓存"全程，杜绝半新半旧。
        """
        host, port = _server_target(self.cfg["server"])
        self._portal_port = port   # do_login/portal_alive/fetch_flux 按此端口连接
        with self.net_lock:
            now = time.monotonic()
            if (self._portal_ip and self._portal_host == host
                    and now - self._portal_ip_at < 600):
                return host, self._portal_ip
            if host.replace(".", "").isdigit():
                ip = host
            else:
                ip = campus_dns_resolve(host, self.cfg["campus_dns"], self.bind_ip)
                if not ip:
                    # 系统 DNS 回退（评估 P1-1）：异校 campus_dns 不可达时守护
                    # 不应永远 portal_down（向导链路早有此回退，补齐对称）。
                    # 解析对象仍是用户填写的 host——凭据不改道。
                    ip = system_dns_resolve(host)
            self._portal_ip = ip
            self._portal_host = host
            self._portal_ip_at = now
        return host, ip

    def probe_online(self) -> bool:
        """第一层：公网探测（物理栈钉 IP）。

        判据（2026-11-04 评估 P-1 修订，参考校实测 + 华为 AC 公开行为）：
        - 3xx 跳 10.0.0.1 / reset / 超时 → 掉线（劫持或不可达）
        - 200 且响应体带门户指纹 → 掉线（华为 AC 的 200/JS 挑战形态，
          旧版"非 3xx 即在线"会把这类门户永远判在线=绿着断网，已封堵）
        - 其余 2xx/4xx → 在线（如 223.5.5.5 对未知路径回 404，
          404 本身就是"真实到达公网"的证明）
        已知局限：不带这些指纹的 200 型劫持页仍会漏判——指纹表可在此
        扩充，或换用自家门户实际劫持形态（README 异校适配）。
        """
        for ip in ([x.strip() for x in str(self.cfg["probe_ips"]).split(",") if x.strip()]
                   or [x.strip() for x in DEFAULTS["probe_ips"].split(",")]):
            # 空锚点守卫（trouble-maker P2-18）：probe_ips 被清空会永远判离线
            # → 每 tick 登录直到熔断；回落默认锚点
            status, body = physical_http(ip, ip, self.cfg["probe_path"],
                                         headers={"Host": str(self.cfg.get("probe_host", "")
                                                             or "connect.rom.miui.com")},
                                         bind_ip=self.bind_ip,
                                         timeout=self.probe_timeout)
            if status in (0, 301, 302, 307, 308):
                continue  # 被门户劫持或不可达，试下一个锚点
            if status == 200 and any(m in body for m in _marks(self.cfg, "hijack_marks")):
                continue  # 200 型门户劫持页（JS 挑战），不是真实互联网
            return True
        return False

    def probe_l2_restricted(self) -> bool:
        """L2 策略层探测：出口 DPI 白名单是否拦了大众站点。

        2026-11-04 09:41-09:55 实测完整周期：认证会话正常、miui 锚点通畅，
        但 B站/百度/DeepSeek 被 SNI 黑名单 RST，约 15 分钟后自动放行。
        L1 探测对此失明（miui 在白名单）。判别：大众站域名解析后物理栈
        TLS 握手，被 RST = 受限；真实到达 = 正常。
        注意：锚点 IP 必须用校园 DNS 实时解析（CDN 多线，硬编码 IP 会解析
        到不同节点导致误判）；受限只报观察态，绝不自动"修复"——对抗 DPI 属违规。
        """
        ip = campus_dns_resolve("www.baidu.com", self.cfg["campus_dns"], self.bind_ip)
        if not ip:
            return False  # DNS 都不通说明是 L1 问题，不是 L2
        status, body = physical_http("www.baidu.com", ip, "/",
                                     bind_ip=self.bind_ip, use_tls=True,
                                     timeout=self.probe_timeout)
        if status == 0:
            # 告警去重（P1-5）：受限窗口内每小时只播一条，有效日志不再被挤光
            if time.monotonic() - self._l2_warn_at >= L2_WARN_DEDUP_S:
                self._l2_warn_at = time.monotonic()
                self.log.warning("L2 探测：大众站 www.baidu.com(%s) 被拦（%s）——"
                                 "认证会话正常但出口策略受限（DPI 风控窗口），"
                                 "通常 10-30 分钟自动放行；期间勿反复注销重登", ip, body)
            return True
        return False

    def portal_alive(self) -> bool:
        """第二层：认证服务器是否存活（物理栈，登录页可达即算）。"""
        host, ip = self._portal_target()
        if not ip:
            return False  # 不在校园网/解析失败：视为门户不可达，不发任何凭据
        status, _ = physical_http(host, ip, self.cfg["login_page"],
                                  bind_ip=self.bind_ip, use_tls=self._use_tls(),
                                  verify=self._tls_verify(), timeout=self.probe_timeout,
                                  port=self._portal_port)
        return 0 < status < 500

    def do_login(self) -> tuple[bool, str]:
        """第三层：POST 登录并校验响应（物理栈钉 IP），返回 (成功, 失败原因)。"""
        host, ip = self._portal_target()
        if not ip:
            return False, "门户解析失败（疑似不在校园网），已跳过以防凭据外泄"
        import urllib.parse
        form = urllib.parse.urlencode({k: v for k, v in self.form.items()})
        status, text = physical_http(
            host, ip, self.cfg["login_path"], method="POST",
            body=form,
            headers={"Content-Type": "application/x-www-form-urlencoded",
                     **self._headers()},
            bind_ip=self.bind_ip, use_tls=self._use_tls(), verify=self._tls_verify(),
            timeout=self.login_timeout, port=self._portal_port)
        if status == 0:
            return False, f"请求异常: {text}"
        return self.parse_login(text)

    @staticmethod
    def probe_login(cfg: dict, username: str, password: str) -> tuple[bool, str]:
        """一次性凭据探针（审查 P0-3/P1-16）：不建 Session、不碰运行中 keeper。

        与主守护链路同构：端点/UA/Referer/form 差异全部读传入 cfg（通用化
        B），避免"面板测过、守护却按另一套参数登录"的适配断点。
        """
        server = cfg["server"]
        campus_dns = cfg.get("campus_dns", "")
        bind_ip = discover_physical_ip(campus_dns)
        host, port = _server_target(server)
        ip = host if host.replace(".", "").isdigit() else campus_dns_resolve(host, campus_dns, bind_ip)
        if not ip:
            # 校园 DNS 未配置/不可达 → 系统 DNS 回退，解析的仍是用户填的 host
            ip = system_dns_resolve(host)
        if not ip:
            return False, "门户解析失败（校园 DNS 与系统 DNS 都不通，疑似不在校园网），已跳过"
        import urllib.parse
        form = urllib.parse.urlencode({**FORM_BASE, "userName": username,
                                       "pwd": password, **load_form_extras()})
        headers = {"Content-Type": "application/x-www-form-urlencoded",
                   "User-Agent": cfg["user_agent"],
                   "Referer": server + cfg["login_page"]}
        status, text = physical_http(host, ip, cfg["login_path"], method="POST",
                                     body=form, headers=headers, bind_ip=bind_ip,
                                     use_tls=not server.strip().lower().startswith("http://"),
                                     verify=str(cfg.get("ssl_verify", "false")).lower() == "true",
                                     timeout=float(cfg.get("login_timeout", "0") or 0)
                                     or max(6.0, 2 * float(cfg.get("probe_timeout", "3"))),
                                     port=port)
        if status == 0:
            return False, f"请求异常: {text}"
        return Keeper.parse_login(text)

    @staticmethod
    def parse_login(text: str, cfg: dict | None = None) -> tuple[bool, str]:
        """登录响应校验（华为伪 JSON + 标准 JSON + 纯文本多形态分治）。

        实测失败响应：{'success':false,'msg':'Task 'NOAUTH:*' not taken',...}
        判定结构固定（防误判护栏），异校只换词表：cfg 传 response.* 键覆盖，
        不传则用 RESPONSE_DEFAULTS（参考校/华为行为完全不变）。
        """
        t = (text or "").strip()
        if not t:
            return False, "空响应"
        success_marks = _marks(cfg, "success_marks")
        negative_marks = _marks(cfg, "negative_marks")

        def _check(data) -> tuple[bool, str] | None:
            """标准 JSON dict（success/ret_code 两类门户）的判定。"""
            if not isinstance(data, dict):
                return None
            if "success" in data:
                if data.get("success") in (True, "true", 1, "1"):
                    return True, ""
                msg = str(data.get("msg") or data.get("message") or "")[:120]
                if _already_online(msg):
                    return True, ""
                return False, msg or "服务器拒绝"
            if "ret_code" in data or "code" in data:
                code = str(data.get("ret_code", data.get("code", "")))
                msg = str(data.get("msg") or data.get("message") or "")[:120]
                if code == "0" or any(w in msg for w in success_marks) or _already_online(msg):
                    return True, ""
                return False, msg or t[:120]
            return None

        # 1) 标准合法 JSON 优先
        try:
            result = _check(json.loads(t))
            if result:
                return result
        except ValueError:
            pass
        # 2) 华为伪 JSON：正则提取 success 与 msg（容忍单/双引号包裹与内嵌引号）
        m = re.search(r"'success'\s*:\s*(true|false)", t)
        if m:
            if m.group(1) == "true":
                return True, ""
            msg_m = re.search(r"'msg'\s*:\s*(['\"])(.*?)\1\s*[,}]", t)
            msg = (msg_m.group(2) if msg_m else "")[:120]
            if _already_online(msg):
                return True, ""
            return False, msg or "服务器拒绝"
        # 3) 纯文本标记兜底（trouble-maker P1-9：HTML 门户页里常有"错误提示/
        # onerror"字样，负向词一刀切会把成功页误判成硬拒绝→600 秒最重惩罚。
        # 负向词只作用于 ≤200 字符的短响应；长响应只认强正向标记，判不了
        # 归"无法判定"→瞬态短退避，不施加重罚）
        if len(t) <= 200:
            if any(w in t.lower() for w in (x.lower() for x in negative_marks)):
                return False, t[:120]
            if re.search(r"login_ok\s*=\s*false", t, re.I):
                return False, t[:120]
            # 英文门户短响应（评估 P-5）：\b 防 "unsuccessful" 误命中
            if any(w in t.lower() for w in (x.lower() for x in success_marks)) \
                    or re.search(r"\bsuccess", t, re.I):
                return True, ""
            return False, t[:120]
        if re.search(r"login_ok\s*=\s*false", t, re.I):
            return False, t[:120]
        if re.search(r"login_ok\s*=\s*true", t, re.I) \
                or any(w in t for w in success_marks):
            return True, ""
        return False, "无法判定响应内容"

    def fetch_flux(self) -> dict:
        """查流量：POST userflux 并解析 HTML 表格（正则替代 bs4，少一个依赖）。

        物理栈钉 IP 直连（2026-11-04 实测：userflux 按**源 IP** 识别会话，
        无需 cookie——TUN 下绑定源 session 的 DNS 会失败，故走裸 socket）。
        已知局限（评估 P-6）："按源 IP 认会话"是参考校实测行为；依赖会话
        cookie 的部署会拿到登录页→解析空表。查流量按参考校校准，非普适承诺。
        """
        host, ip = self._portal_target()
        if not ip:
            return {"错误": "门户解析失败（疑似不在校园网）"}
        status, text = physical_http(
            host, ip, self.cfg["flux_path"], method="POST",
            headers={"Content-Type": "application/x-www-form-urlencoded",
                     **self._headers()},
            bind_ip=self.bind_ip, use_tls=self._use_tls(), verify=self._tls_verify(),
            timeout=max(8.0, self.probe_timeout * 2), port=self._portal_port)
        if status == 0:
            return {"错误": f"请求异常: {text}"}
        rows = re.findall(
            r"<tr[^>]*>\s*<td[^>]*>(.*?)</td>\s*<td[^>]*>(.*?)</td>\s*</tr>",
            text, re.S)
        data = {}
        for raw_k, raw_v in rows:
            k = re.sub(r"<[^>]+>", "", raw_k).replace("：", "").strip()
            v = re.sub(r"<[^>]+>", "", raw_v).strip()
            if k:
                data[k] = v
        return data

    def clash_recover(self) -> None:
        """恢复成功后联动 mihomo API：flush fake-ip 缓存、清理死连接（静默失败）。

        可选增强（默认关）：mihomo 外部控制器多数用户不开，不配即靠代理软件
        自身心跳恢复（通用主路径）。安全边界：仅接受 http://127.0.0.1 /
        http://localhost / http://[::1] 形态（本机 loopback），其余一律拒绝
        ——既是 SSRF 防线，也符合"控制器就该在本机"的部署事实。
        """
        api = str(self.cfg.get("clash_api_url", "")).strip()
        if not api:
            return
        from urllib.parse import urlparse
        u = urlparse(api)
        if u.scheme != "http" or u.hostname not in ("127.0.0.1", "localhost", "::1"):
            self.log.warning("clash_api_url 仅支持本机 http://127.0.0.1[:port]，已忽略: %s", api)
            return
        base = f"http://127.0.0.1:{u.port or 9090}"
        headers = {}
        secret = str(self.cfg.get("clash_api_secret", "")).strip()
        if secret:
            headers["Authorization"] = f"Bearer {secret}"
        try:
            # trust_env=False：环境变量 http_proxy 会把 loopback 请求绕道代理
            # 节点（审查 P3-16），Clash 用户环境里几乎必踩
            s = requests.Session()
            s.trust_env = False
            r1 = s.post(base + "/cache/fakeip/flush", timeout=2, headers=headers)
            r2 = s.delete(base + "/connections", timeout=2, headers=headers)
            if r1.status_code < 400 and r2.status_code < 400:
                self.log.info("已通知 Clash 内核清理 fake-ip 与死连接")
            else:
                self.log.warning("Clash API 响应异常（flush=%s, conn=%s），"
                                 "内核版本过旧或 secret 不符？代理侧将由其自身心跳恢复",
                                 r1.status_code, r2.status_code)
        except requests.RequestException as exc:
            self.log.debug("Clash API 联动失败（不影响联网）: %s", exc)

    def set_state(self, state: str, detail: str = "") -> None:
        """状态变更去重：只有真实变化才记录日志/刷新托盘。"""
        if state != self.state or detail != self.state_detail:
            self.state = state
            self.state_detail = detail
            self.log.info("状态: %s", self.describe_state())
            for cb in list(self.state_listeners):
                try:
                    cb()
                except Exception:  # noqa: BLE001 回调异常不中断守护
                    pass

    def describe_state(self) -> str:
        if self.state == "online":
            return "在线"
        if self.state == "rejected":
            return f"登录被拒：{self.state_detail}（退避中）"
        if self.state == "portal_down":
            return f"认证服务器不可达，{int(self.portal_down_wait)}秒后重试"
        if self.state == "starting":
            return "启动中，正在首次检测"
        return self.state  # 未知状态兜底，避免拼出 None

    def request_reconnect(self) -> None:
        """用户主动"立即检测重连"：清退避与熔断计数后立即探测（审查 P1-14）。"""
        self.reject_until = 0.0
        self.suspect = 0
        self.transient_streak = 0
        self.extra_wait = 0.0
        self.force_evt.set()

    def tick(self) -> None:
        """一次探测周期：三层判定 + L2 策略层观察 + 按需登录 + 登录风暴熔断。"""
        if self.probe_online():
            # L1 通 ≠ 完全在线：出口 DPI 可能拦大众站（2026-11-04 实测）。
            # 受限是"观察态"：状态仍 online（认证没掉），仅日志留痕，绝不自动干预。
            self.set_state("online")  # 网络自愈时也须点亮（审查 P0-4：原缺此句致卡红）
            # L2 节流（trouble-maker P1-5）：每 tick 都 TLS 握手 baidu 既刷爆
            # 轮转日志，又高频撞 DPI 黑名单——在线期 60 秒一探足够。
            if time.monotonic() - self._l2_at >= L2_PROBE_S:
                self._l2_at = time.monotonic()
                self.probe_l2_restricted()
            self.reject_until = 0.0
            self.suspect = 0
            self.transient_streak = 0
            self.extra_wait = 0.0
            return
        if not self.portal_alive():
            self.set_state("portal_down")
            self.extra_wait = self.portal_down_wait
            # 网络切换（休眠唤醒/WiFi↔有线/VPN 连断）后旧自动发现的源 IP
            # 会失效，表现为长期 portal_down。每隔一个等待周期重发现一次
            # 物理源，并失效门户 IP 缓存（P2-8）。用户手动指定的 bind_ip
            # 是显式逃生口，不覆盖。
            now = time.monotonic()
            if (not self.cfg.get("bind_ip", "").strip()
                    and now - self._bind_retry_at >= self.portal_down_wait):
                self._bind_retry_at = now
                fresh = discover_physical_ip(str(self.cfg.get("campus_dns", "")))
                if fresh and fresh != self.bind_ip:
                    self.log.info("网络可能已切换，物理源重发现: %s → %s",
                                  self.bind_ip or "(未绑定)", fresh)
                    self.bind_ip = fresh
                    with self.net_lock:
                        self._portal_ip = ""   # 换源后重解析门户
                        self._portal_ip_at = 0.0
            return
        if time.monotonic() < max(self.reject_until, self._frozen_until):
            # 退避期间保留被拒原因（trouble-maker P1-8：detail 默认 "" 会把
            # "密码错误/额度用尽"覆写消失，用户错过唯一一次原因展示）
            self.set_state("rejected", self.state_detail)
            return
        ok, msg = self.do_login()
        if ok:
            # 熔断：连续 3 次"登录成功但下一轮探测仍失败"说明判定链在骗我们，
            # 强制退避，避免每 interval 轰炸 login.php 触发门户风控
            self.suspect += 1
            if self.suspect >= 3:
                self.log.warning("连续 %s 次登录成功但探测仍失败，疑似误判，退避 %s 秒",
                                 self.suspect, int(self.reject_backoff))
                self.reject_until = time.monotonic() + self.reject_backoff
                self.suspect = 0
                self.set_state("rejected", "疑似误判，熔断退避")
                return
            self.log.info("自动重连成功")
            self.set_state("online")
            self.reject_until = 0.0
            self.transient_streak = 0
            self.clash_recover()
        else:
            self.handle_rejection(msg)

    @staticmethod
    def _redact(text: str) -> str:
        """抹掉响应里的 userName 值（审查 P2-26：日志/截图会泄露学号）。"""
        return re.sub(r"(userName['\"]?\s*[:=]\s*['\"]?)[^,'\"}\s]+", r"\1***", text or "")

    def handle_rejection(self, msg: str) -> None:
        """拒绝分类退避：瞬态错误短退避快速重试（防明早时段切换卡 10 分钟），
        硬错误（密码/额度/锁定）长退避；瞬态连续 5 次升级为长退避防风暴。"""
        transient = any(w in msg.lower() for w in
                        (x.lower() for x in _marks(self.cfg, "transient_marks")))
        if transient:
            self.transient_streak += 1
            wait = self.reject_backoff if self.transient_streak >= 5 else self.transient_backoff
        else:
            self.transient_streak = 0
            wait = self.reject_backoff
        self.reject_until = time.monotonic() + wait
        # 冻结风暴封堵（2026-11-05 日志实证）：门户限频冻结（如「冻结剩余60s」）
        # 期间锚点抖动判在线 → reject_until 被清零 → 立刻重登 → 更深冻结。
        # 冻结闸门独立于退避，probe 恢复不清除它；剩余秒数可从消息解析。
        if any(w in msg.lower() for w in (x.lower() for x in _marks(self.cfg, "freeze_marks"))):
            m = re.search(r"剩余\s*(\d+)\s*s", msg)
            frozen_wait = max(self.reject_backoff, (int(m.group(1)) + 15) if m else 0)
            self._frozen_until = time.monotonic() + frozen_wait
            self.log.warning("账号被门户限频冻结 %s 秒，冻结闸门生效（锚点抖动不再触发重登）",
                             int(frozen_wait))
        safe = self._redact(msg)
        user = str(self.cfg.get("username", "") or "")
        if user and len(user) >= 5 and user in safe:
            # 长账号（学号形态）全局替换：门户在 msg 里直接回显账号（2026-11-05
            # 日志实证「用户xxx被冻结」），不只 userName= 表单格式
            safe = safe.replace(user, "***")
        elif user:
            # 短账号不做全局子串替换（"u" 会把 auth/required 全打码），只盖
            # 「用户<账号>」回显形态
            safe = re.sub(rf"用户\s*{re.escape(user)}", "用户***", safe)
        detail = safe
        if "无法判定" in msg:
            # 失败现场自诊断：响应判不了多半是方言/协议不兼容（2026-11-04 协议族
            # 调研），把下一步动作送到用户眼前，而不是让他去翻文档。
            detail += "（疑似门户不兼容：运行 python compat_check.py --detect <登录页样本> 判定，见 README「适用范围与异校适配」）"
        self.log.warning("登录被拒（%s退避 %s 秒）：%s",
                         "瞬态" if transient else "硬性", int(wait), detail)
        self.set_state("rejected", detail)

    def run(self) -> None:
        """常驻主循环：先探测再等待（启动即测，不留假在线窗口）。"""
        self.log.info("campus-net-autologin 启动：每 %s 秒探测，掉线自动重连", self.interval)
        try:
            self.tick()
        except Exception as exc:  # noqa: BLE001 单次探测异常不中断守护
            self.log.error("tick 异常（继续守护）: %s", exc)
        while not self.stop_evt.is_set():
            self.force_evt.wait(self.interval + self.extra_wait)
            self.force_evt.clear()
            self.extra_wait = 0.0
            if self.stop_evt.is_set():
                break
            try:
                self.tick()
            except Exception as exc:  # noqa: BLE001 单次探测异常不中断守护
                self.log.error("tick 异常（继续守护）: %s", exc)


# ---------------- 托盘 ----------------

def _autostart_is_enabled() -> bool:
    """菜单勾选状态查询（autostart 模块缺失时视为未启用）。"""
    try:
        import autostart
        return autostart.is_enabled()
    except Exception:  # noqa: BLE001
        return False


TRAY_COLORS = {
    # 与 shell.kernel_api.STATE_COLORS 同源（降饱和进石墨世界）；
    # 跨内核/外壳的成对常量，漂移由 tests_tdd 循环4 拦截
    "online": (79, 157, 99),
    "offline": (209, 145, 63),
    "portal_down": (209, 145, 63),
    "rejected": (208, 95, 82),
    "starting": (139, 145, 155),
}


def _make_icon(rgb):
    """运行时绘制状态圆点图标，无需内置图片资源。"""
    try:
        from PIL import Image, ImageDraw
    except ImportError:
        return None
    img = Image.new("RGBA", (64, 64), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    d.ellipse((2, 2, 62, 62), fill=rgb + (255,))
    d.ellipse((14, 14, 50, 50), fill=(255, 255, 255, 255))
    d.ellipse((22, 22, 42, 42), fill=rgb + (255,))
    return img


class Tray:
    """系统托盘：状态颜色 + 菜单（注入面板回调时七项）。pystray/Pillow 缺失时自动降级纯后台。

    查流量/查看日志为注入回调（on_flux/on_log）：动作的展示统一由 shell.Desk
    完成（与面板同一实现）。2026-11-04 实测：托盘直接弹 pystray 气泡在
    Win11 常被专注助手静默吞掉、直开记事本体验差，故不再在托盘内自理。
    """

    def __init__(self, keeper: Keeper, on_open_panel=None,
                 on_flux=None, on_log=None) -> None:
        import pystray

        self.keeper = keeper
        keeper.add_state_listener(self.refresh)
        self.icons = {k: _make_icon(v) for k, v in TRAY_COLORS.items()}
        items = [pystray.MenuItem("立即检测重连", self.on_force, default=True)]
        if on_open_panel is not None:
            items.append(pystray.MenuItem(
                "打开面板", lambda ic, it: on_open_panel()))
        items += [
            pystray.MenuItem("查流量", self._wrap(on_flux)),
            pystray.MenuItem("打开认证页", self.on_portal),
            pystray.MenuItem("查看日志", self._wrap(on_log)),
            pystray.MenuItem("开机自启", self.on_autostart,
                             checked=lambda item: _autostart_is_enabled()),
            pystray.MenuItem("退出", self.on_exit),
        ]
        self.icon = pystray.Icon(
            "campus-net-autologin",
            self.icons.get(keeper.state) or _make_icon((158, 158, 158)),
            "campus-net-autologin",
            menu=pystray.Menu(*items),
        )

    @staticmethod
    def _wrap(cb):
        """注入回调 → pystray 动作；未注入时点了只记日志，不静默。"""
        def action(icon, item):
            if cb is None:
                logging.getLogger("keeper").info("该动作未接入宿主（无面板环境）")
                return
            cb()
        return action

    def refresh(self) -> None:
        """状态回调：换图标颜色 + 更新悬停文字。"""
        img = self.icons.get(self.keeper.state)
        try:
            if img:
                self.icon.icon = img
            self.icon.title = "campus-net-autologin：" + self.keeper.describe_state()
        except Exception:  # noqa: BLE001 托盘刷新失败不影响守护
            pass

    def on_force(self, icon, item) -> None:
        self.keeper.force_evt.set()

    def on_portal(self, icon, item) -> None:
        webbrowser.open(self.keeper.cfg["server"])

    def on_autostart(self, icon, item) -> None:
        """开机自启开关：注册表 HKCU Run 键（autostart.py；组策略禁 schtasks 的实测路径）。"""
        try:
            import autostart
        except ImportError:
            self.keeper.log.warning("autostart 模块不可用")
            return
        if autostart.is_enabled():
            ok = autostart.unregister()
            self.keeper.log.info("开机自启：%s",
                                 "已关闭" if ok else "关闭失败（查看日志，Run 键可能仍在）")
        else:
            ok = autostart.register()
            self.keeper.log.info("开机自启：%s", "已开启" if ok else "开启失败（查看日志）")
        try:
            icon.update_menu()
        except Exception:  # noqa: BLE001
            pass

    def on_exit(self, icon, item) -> None:
        self.keeper.stop_evt.set()
        self.keeper.force_evt.set()
        try:
            self.icon.stop()
        except Exception:  # noqa: BLE001
            pass


# ---------------- 入口 ----------------

_lock_fh = None  # 持有句柄直到进程退出；进程死亡（含强杀）时 OS 自动释放锁


def acquire_single_instance() -> bool:
    """单实例互斥：OS 级文件锁（msvcrt/flock），防自启+手动双开互踢。

    2026-11-04 实测教训：旧实现用"文件存在"判断，进程被强杀后残留锁永久
    阻塞重启。改用 OS 建议锁——持有文件句柄即持锁，进程无论怎么死 OS 都释放。
    """
    global _lock_fh
    lock_path = BASE_DIR / "keeper.lock"
    fh = None
    try:
        fh = open(lock_path, "a+")
        fh.seek(0)
        if os.name == "nt":
            import msvcrt
            msvcrt.locking(fh.fileno(), msvcrt.LK_NBLCK, 1)
        else:
            import fcntl
            fcntl.flock(fh.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        _lock_fh = fh
        return True
    except OSError:
        if fh is not None:
            try:
                fh.close()
            except OSError:
                pass
        return False


def main() -> None:
    """兼容旧入口：委托到 app_main（唯一启动入口）。"""
    import app_main
    app_main.main()


if __name__ == "__main__":
    main()
