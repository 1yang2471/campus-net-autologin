# -*- coding: utf-8 -*-
"""campus-net-autologin 唯一启动入口：keeper + 托盘 + 面板同进程。

python app_main.py        终端运行（日志可见）
pythonw app_main.py       静默常驻（自启用）
"""
from __future__ import annotations

import logging
import sys
import threading
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from keeper import (Keeper, load_config, ensure_config, setup_logging,  # noqa: E402
                    acquire_single_instance, save_config, LOG_FILE, CONFIG_FILE)


def _store_password(user: str, pwd: str) -> str:
    """密码进 keyring；不可用则明文降级并**日志告警**（README 承诺，P1-4：
    旧实现静默 pass，明文躺 ini 无人知晓）。返回应写入 ini 的值。"""
    from shell.credentials import save_password, PLACEHOLDER
    log = logging.getLogger("keeper")
    try:
        save_password(user, pwd)
        return PLACEHOLDER
    except Exception as exc:  # noqa: BLE001 keyring 后端异常（域机/精简系统）
        log.warning("keyring 不可用（%s），密码以明文暂存 %s ——修复凭据管理器后"
                    "重新保存即可", exc, CONFIG_FILE)
        return pwd


def _disk_values(values: dict) -> dict:
    """持久化用的配置：磁盘 ini 现值 + 本次提交的键。

    两条安全/正确性约束（2026-11-04 实证 P0）：
    1) 运行态 cfg["password"] 是从 keyring 还原的真实密码，绝不能写回 ini
       —— 一律基于磁盘值（<keyring> 占位或明文遗留），不碰运行态。
    2) 只覆盖本次提交的键，用户手工编辑 ini 的其它键不被旧快照回滚。
       （审查 P1-1 修复：不再无条件把 password 改写成 <keyring> 占位——
       keyring 降级（明文遗留）环境下那等于每次保存都埋雷，重启丢凭据；
       磁盘是什么形态就保持什么形态，只有本次确有新密码才改写。）
    """
    disk = load_config()
    disk.update({kk: str(vv) for kk, vv in values.items()})
    # response.* 是 [response] 段的合并视图，不写回 [keeper]（save_config 已保留
    # 原段）——否则 ini 里会多出无意义脏键，且下次 load_config 段语义混乱。
    return {kk: vv for kk, vv in disk.items() if not kk.startswith("response.")}


class KeeperFacade:
    """把运行中的 Keeper 适配为 shell.Kernel 协议（面板只认这个接口）。"""

    def __init__(self, keeper: Keeper) -> None:
        self.k = keeper
        self.needs_setup = False

    def snapshot(self) -> dict:
        k = self.k
        remain = max(0, int(k.reject_until - time.monotonic()))
        state = k.state if k.state in ("online", "portal_down", "rejected",
                                       "paused") else (
            "starting" if k.state == "starting" else "offline")
        return {"state": state, "detail": k.state_detail,
                "interval": k.interval, "reject_remaining": remain}

    def force_reconnect(self) -> None:
        self.k.request_reconnect()

    def fetch_flux(self) -> dict:
        return self.k.fetch_flux()

    def on_change(self, callback) -> None:
        self.k.add_state_listener(callback)

    def test_credentials(self, username: str, password: str, server: str) -> tuple[bool, str]:
        # 一次性探针，绝不改运行中 keeper 的 cfg/form（审查 P0-3）；
        # 传入临时 cfg 副本（仅替换待测 server/账号），与主链路同构。
        # 密码留空 = 用已存密码测试（P1-4：别让"仅改服务器"被门户拒绝）
        cfg = {**self.k.cfg, "server": server, "username": username}
        return Keeper.probe_login(cfg, username, password or self.k.cfg.get("password", ""))

    def detect_portal(self) -> str:
        from keeper import detect_portal
        return detect_portal(probe_ips=str(self.k.cfg.get("probe_ips", "")),
                             bind_ip=self.k.bind_ip,
                             campus_dns=str(self.k.cfg.get("campus_dns", "")),
                             login_page=str(self.k.cfg.get("login_page", "")),
                             probe_path=str(self.k.cfg.get("probe_path", "")))

    def save_credentials(self, username: str, password: str, server: str) -> None:
        k = self.k
        # 密码留空 = 不改已存凭据（P1-4）：不重写 keyring、不清空运行态/表单。
        # 运行态 cfg["password"] 始终是真实密码，磁盘才用 <keyring> 占位。
        has_new_pwd = bool(password)
        stored = _store_password(username, password) if has_new_pwd else None
        k.cfg.update({"username": username, "server": server})
        if has_new_pwd:
            k.cfg["password"] = password  # 真实密码（与磁盘占位分离）
        k.form = {**k.form, "userName": username,
                  "pwd": password if has_new_pwd else k.form.get("pwd", "")}
        disk = _disk_values({"username": username, "server": server})
        if has_new_pwd:
            disk["password"] = stored
        save_config(disk)
        from shell.accounts import remember_account
        remember_account(username, server)
        k.request_reconnect()  # 改完立即用新凭据重连（审查 P1-14）

    def switch_account(self, username: str) -> tuple[bool, str]:
        """切换历史账号：密码按用户名从 keyring 还原（旧账号密码绝不跟过来——
        save_credentials 的"留空=不改"语义在这里是陷阱，必须先取到再保存）。"""
        from shell.accounts import list_accounts
        entry = next((a for a in list_accounts() if a[0] == username), None)
        if entry is None:
            return False, f"账号 {username} 不在历史列表"
        from shell.credentials import load_password
        pwd = load_password(username)
        if not pwd:
            return False, (f"{username} 的密码不在凭据管理器（或曾明文降级已失效），"
                           "请填写密码后点「保存并立即测试」")
        server = entry[1] or str(self.k.cfg.get("server", ""))
        self.save_credentials(username, pwd, server)
        return True, f"已切换到 {username}，正在用已存凭据重连"

    def get_settings(self) -> dict:
        return dict(self.k.cfg)

    def save_settings(self, values: dict) -> None:
        """设置页保存：磁盘 ini 现值 + 提交键 → 持久化 → 热重载（保存即生效）。

        基于磁盘而非运行态快照：避免真实密码明文落盘、避免回滚手工 ini 编辑。
        """
        k = self.k
        disk = _disk_values(values)
        save_config(disk)
        k.cfg.update({kk: str(vv) for kk, vv in values.items()})
        k.apply_config(dict(k.cfg))


class SetupFacade:
    """无配置时的向导内核替身：test/save 不依赖运行中的 keeper。"""

    needs_setup = True

    def snapshot(self) -> dict:
        return {"state": "starting", "detail": "首次配置中", "interval": 3,
                "reject_remaining": 0}

    def force_reconnect(self) -> None:
        pass

    def fetch_flux(self) -> dict:
        return {"提示": "配置完成后可查流量"}

    def on_change(self, callback) -> None:
        pass

    def test_credentials(self, username: str, password: str, server: str) -> tuple[bool, str]:
        cfg = load_config()
        cfg["server"] = server
        return Keeper.probe_login(cfg, username, password)

    def detect_portal(self) -> str:
        from keeper import detect_portal
        cfg = load_config()
        return detect_portal(probe_ips=str(cfg.get("probe_ips", "")),
                             campus_dns=str(cfg.get("campus_dns", "")),
                             login_page=str(cfg.get("login_page", "")),
                             probe_path=str(cfg.get("probe_path", "")))

    def save_credentials(self, username: str, password: str, server: str) -> None:
        stored = _store_password(username, password)
        cfg = load_config()  # 增量：基于现有配置改，不重建（审查 P1-11）
        cfg.update({"username": username, "server": server, "password": stored})
        save_config(cfg)
        from shell.accounts import remember_account
        remember_account(username, server)

    def switch_account(self, username: str) -> tuple[bool, str]:
        return False, "首次配置不支持切换账号"

    def get_settings(self) -> dict:
        return load_config()

    def save_settings(self, values: dict) -> None:
        save_config(_disk_values(values))


def assemble(cfg):
    """装配单进程应用：返回 (keeper, tray, facade)。keeper 未启动，由 main 决定。"""
    keeper = Keeper(cfg)
    facade = KeeperFacade(keeper)
    panel_ref = {"win": None}
    panel_lock = threading.Lock()  # 连点"打开面板"防双窗（P2-22）

    # 辅助窗口宿主：托盘"查流量/查看日志"与面板按钮共用同一实现
    # （2026-11-04 用户反馈：托盘气泡常被 Win11 吞掉、记事本看日志体验差）
    from shell.desk import Desk
    desk = Desk()
    desk.start()

    def panel_factory():
        # 单例面板：已开则置顶，不重复开窗（审查 P2-23；P2-22 补锁防连点双窗）
        with panel_lock:
            cur = panel_ref["win"]
            if cur is not None:
                if cur != "pending":
                    try:
                        cur.lift(); cur.focus_force()
                    except Exception:  # noqa: BLE001
                        pass
                return
            panel_ref["win"] = "pending"

        def _run():
            win = None
            try:
                from shell.app import Shell
                win = Shell(facade, desk=desk)
                panel_ref["win"] = win
                win.mainloop()
            except Exception as exc:  # noqa: BLE001
                logging.getLogger("keeper").warning("面板异常退出: %s", exc)
            finally:
                panel_ref["win"] = None
        threading.Thread(target=_run, daemon=True).start()

    tray = None
    try:
        from keeper import Tray
        tray = Tray(keeper, on_open_panel=panel_factory,
                    on_flux=lambda: desk.post("flux", kernel=keeper),
                    on_log=lambda: desk.post("log", path=LOG_FILE))
    except Exception as exc:  # noqa: BLE001 缺 pystray/Pillow 时降级
        logging.getLogger("keeper").info("托盘不可用（%s），纯后台模式", exc)

    return keeper, tray, facade


def _effective_config():
    """解析配置并还原 keyring 密码；明文遗留则迁移；凭据缺失返回 (cfg, False)。"""
    from shell.credentials import load_password, save_password, PLACEHOLDER
    cfg = load_config()
    user = cfg.get("username", "")
    pwd = cfg.get("password", "")
    if pwd == PLACEHOLDER:
        cfg["password"] = load_password(user)  # 凭据丢失则空 → 触发向导（审查 P1-9）
    elif pwd and user:
        # 明文遗留（旧版/控制台首跑）→ 迁移进 keyring，ini 改占位
        try:
            save_password(user, pwd)
            cfg["password"] = PLACEHOLDER
            save_config(cfg)
        except Exception as exc:  # noqa: BLE001 keyring 不可用则保留明文，仍可用
            logging.getLogger("keeper").warning(
                "keyring 不可用（%s），密码暂以明文存于 %s ——修复后重新保存凭据即可",
                exc, CONFIG_FILE)
    return cfg, bool(user and cfg.get("password"))


def _crash_hook(exc_type, exc, tb) -> None:
    """pythonw 无控制台：进程级未捕获异常必须落盘，否则像 2026-11-05 夜间
    守护静默死亡一样零痕迹、无从尸检。"""
    logging.getLogger("keeper").error(
        "进程未捕获异常退出: %s: %s", exc_type.__name__, exc,
        exc_info=(exc_type, exc, tb))


def _thread_crash_hook(args) -> None:
    if args.exc_type is SystemExit:
        return
    logging.getLogger("keeper").error(
        "线程 %s 未捕获异常: %s: %s", getattr(args.thread, "name", "?"),
        args.exc_type.__name__, args.exc_value,
        exc_info=(args.exc_type, args.exc_value, args.exc_traceback))


def _install_crash_hooks() -> None:
    sys.excepthook = _crash_hook
    threading.excepthook = _thread_crash_hook


def main() -> None:
    setup_logging()
    _install_crash_hooks()
    log = logging.getLogger("keeper")
    if not acquire_single_instance():
        log.error("已有实例在运行（keeper.lock 被占用），退出")
        return
    # 首跑循环：向导（console 或 GUI）保存后回到这里，凭据齐了就启动守护
    for _ in range(3):
        cfg, ready = _effective_config()
        if ready:
            # 启动即播种历史：当前账号永远出现在面板「历史账号」下拉
            # （升级上来的用户没有再走保存流程，不播种列表就是空的）
            from shell.accounts import remember_account
            remember_account(cfg.get("username", ""), cfg.get("server", ""))
            break
        if sys.stdin and sys.stdin.isatty():
            cfg = ensure_config(cfg)
            if not cfg:
                return
            continue
        # 无控制台（frozen 双击）：GUI 向导，保存后 loop 重启
        from shell.app import Shell
        log.info("首次运行且无控制台，打开 GUI 配置向导")
        try:
            Shell(SetupFacade()).mainloop()
        except Exception as exc:  # noqa: BLE001 无桌面/Session 0/Tk 初始化失败
            # pythonw 下无控制台，warning 日志是用户唯一可见线索
            log.warning("配置向导无法启动（%s）：无桌面会话或缺少图形环境；"
                        "请在有桌面的会话中运行一次，或手动编辑 %s 填好账号密码",
                        exc, CONFIG_FILE)
            return
        cfg2, ready2 = _effective_config()
        if ready2:
            continue  # 用户已保存 → 下轮启动守护
        log.info("向导关闭且未配置，退出")
        return
    else:
        return

    keeper, tray, _facade = assemble(cfg)
    if tray is None:
        keeper.run()  # 纯后台阻塞
        return
    # 单一启动点：先起 keeper 线程，icon.run 抛错则停线程再降级（审查 P1-15）
    kt = threading.Thread(target=keeper.run, daemon=True)
    kt.start()
    try:
        tray.icon.run(setup=lambda ic: setattr(ic, "visible", True))
    except Exception as exc:  # noqa: BLE001 无桌面会话（RDP/服务）
        log.warning("托盘运行失败（%s），转纯后台模式", exc)
        keeper.stop_evt.set(); keeper.force_evt.set(); kt.join(timeout=5)
        # 降级后要重新进入守护循环：先清掉停止标志，否则 run() 的
        # while not stop_evt.is_set() 立即为假 → 探测一次就退出（P1-3）
        keeper.stop_evt.clear()
        keeper.run()
        return
    keeper.stop_evt.set(); keeper.force_evt.set(); kt.join(timeout=5)


if __name__ == "__main__":
    main()
