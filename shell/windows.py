# -*- coding: utf-8 -*-
"""辅助窗口：流量信息窗与日志窗（暗色等宽，与面板同风格）。

设计要点：
- 日志窗 tail 末尾若干行，每 2s 自动刷新并滚到最新；不用记事本（太原始）。
- 流量窗由 Desk 在后台线程取数后填充，先显示"查询中…"，避免无反馈。
"""
from __future__ import annotations

import os
import tkinter as tk
from tkinter import ttk
from pathlib import Path

from .theme import BG, PANEL, PANEL_EDGE, FG, FG_DIM, DISPLAY, DISPLAY_SMALL, MONO

LOG_TAIL_LINES = 400      # 日志窗保留的末尾行数
LOG_REFRESH_MS = 2000     # 日志自动刷新间隔


def tail_lines(path: Path, max_lines: int = LOG_TAIL_LINES) -> list[str]:
    """读文件末尾 max_lines 行（大文件不整体读入）。"""
    try:
        size = path.stat().st_size
    except OSError:
        return []
    block = 64 * 1024
    with path.open("rb") as fh:
        if size > block:
            fh.seek(size - block)
            fh.readline()  # 丢弃可能被截断的首行
        data = fh.read()
    text = data.decode("utf-8", errors="replace")
    lines = text.splitlines()
    return lines[-max_lines:] if len(lines) > max_lines else lines


class FluxWindow(tk.Toplevel):
    """流量信息窗：标题「流量信息」，内容为 kernel.fetch_flux() 的键值表。"""

    def __init__(self, master) -> None:
        super().__init__(master)
        self.title("流量信息")
        self.configure(bg=BG)
        self.geometry("380x260")
        self.resizable(False, False)
        self.attributes("-topmost", True)
        self._ready = False
        self._text = tk.Text(self, bg=PANEL, fg=FG, font=DISPLAY,
                             relief="flat", wrap="word", height=9)
        self._text.pack(fill="both", expand=True, padx=16, pady=16)
        self._hint = tk.Label(self, text="查询中…", font=DISPLAY_SMALL,
                              bg=BG, fg=FG_DIM)
        self._hint.pack(side="bottom", pady=(0, 8))

    def set_result(self, data: dict) -> None:
        """填充结果并进入就绪态。"""
        body = "\n".join(f"{k}：{v}" for k, v in data.items())
        self._text.configure(state="normal")
        self._text.delete("1.0", "end")
        self._text.insert("1.0", body or "（无数据）")
        self._text.configure(state="disabled")
        self._hint.configure(text="", fg=FG_DIM)
        self._ready = True

    def set_error(self, message: str) -> None:
        """取数失败也要给用户看见（静默失败是最糟的排障体验）。"""
        self._text.configure(state="normal")
        self._text.delete("1.0", "end")
        self._text.insert("1.0", f"查询失败：{message}")
        self._text.configure(state="disabled")
        self._hint.configure(text="", fg=FG_DIM)
        self._ready = True

    def has_result(self) -> bool:
        return self._ready

    def content(self) -> str:
        return self._text.get("1.0", "end-1c")


class LogWindow(tk.Toplevel):
    """日志窗：末尾若干行 + 定时自动刷新 + 按级别着色 + 智能跟随。

    版式（简洁仪表，与面板同调色）：
    - 顶栏（固定左）：按钮 [刷新][用记事本打开] + 右侧文件名
      （工具条独立一行，不随文本宽度挤压）
    - 正文：wrap="word" 换行，所见即所得，窗口内不会被推出可视区
    - 行首 INFO/WARNING/ERROR 着色，扫一眼可定位异常
    - 智能跟随（同 tail -f 体验）：停在底部才跟随最新；往上翻历史
      不被拽回，翻回底部自动恢复——无需任何开关
    """

    def __init__(self, master, path: Path) -> None:
        super().__init__(master)
        self.path = Path(path)
        self.title("运行日志")
        self.configure(bg=BG)
        self.geometry("760x460")
        self.minsize(420, 260)

        # 顶栏：操作区固定左 + 文件名右（路径只显示文件名，不显示全路径防挤压）
        bar = tk.Frame(self, bg=BG)
        bar.pack(fill="x", side="top", pady=8)
        tk.Button(bar, text="刷新", command=self.refresh, font=DISPLAY,
                  bg=PANEL, fg=FG, activebackground=PANEL_EDGE,
                  activeforeground=FG, relief="flat", bd=0,
                  highlightthickness=1, highlightbackground=PANEL_EDGE,
                  cursor="hand2", pady=4).pack(side="left", padx=(16, 8))
        tk.Button(bar, text="用记事本打开", command=self._open_in_editor,
                  font=DISPLAY, bg=PANEL, fg=FG, activebackground=PANEL_EDGE,
                  activeforeground=FG, relief="flat", bd=0,
                  highlightthickness=1, highlightbackground=PANEL_EDGE,
                  cursor="hand2", pady=4).pack(side="left", padx=(0, 8))
        tk.Label(bar, text=self.path.name, font=DISPLAY_SMALL, bg=BG,
                 fg=FG_DIM, anchor="e").pack(side="right", padx=(8, 16))

        # 正文：自动换行（窗口窄也不会被顶出可视区）
        self._hint = tk.Label(self, text="", font=DISPLAY_SMALL, bg=BG, fg=FG_DIM)
        self._hint.pack(side="bottom", pady=(0, 8))   # 复用占位（勿在失败路径重复 new）
        self._text = tk.Text(self, bg=PANEL, fg=FG, font=MONO,
                             relief="flat", wrap="word", state="disabled")
        ybar = ttk.Scrollbar(self, orient="vertical", command=self._text.yview)
        self._text.configure(yscrollcommand=ybar.set)
        ybar.pack(side="right", fill="y")
        self._text.pack(side="left", fill="both", expand=True,
                        padx=(16, 0), pady=(2, 12))
        self._style()
        self._after_id = None
        self.refresh()
        self._schedule()
        # 打开必停在最新：see() 在窗口映射前会被 Tk 丢弃（实测 2026-11-04
        # 截图停在最旧行），映射事件后补一次并解绑（一次性）。
        self.bind("<Map>", self._land_at_bottom, add="+")

    def _land_at_bottom(self, _event) -> None:
        self._text.see("end")
        self.unbind("<Map>")

    def _style(self) -> None:
        """行首级别着色：WARNING 黄、ERROR 红（看一眼定位异常）。"""
        self._text.tag_configure("warn", foreground="#d1a54a", lmargin1=0,
                                 lmargin2=0)
        self._text.tag_configure("error", foreground="#d05f52")

    def _schedule(self) -> None:
        self._after_id = self.after(LOG_REFRESH_MS, self._tick)

    def _tick(self) -> None:
        self.refresh()
        self._schedule()

    def _at_bottom(self) -> bool:
        """视图是否停在底部（含空内容；分数比较留 1% 容差防抖动）。"""
        try:
            lo, hi = self._text.yview()
        except tk.TclError:
            return True
        return hi >= 0.99 or (lo == 0.0 and hi == 1.0)

    def refresh(self) -> None:
        """重新读日志尾部、按级别着色。

        视图保持（用户实测"滑块停不住"）：delete+insert 全量重建会把视图
        重置到顶部——停在底部才跟随最新；停在别处则先记住顶行索引，
        重建后恢复，刷新对用户位置零打扰。
        """
        follow = self._at_bottom()
        anchor = None
        if not follow:
            try:
                anchor = self._text.index("@0,0")  # 当前可见的顶行索引
            except tk.TclError:
                anchor = None
        lines = tail_lines(self.path)
        body = "\n".join(lines)
        self._text.configure(state="normal")
        self._text.delete("1.0", "end")
        if body:
            self._text.insert("1.0", body)
            for i, line in enumerate(lines, 1):
                if " WARNING " in line or line.endswith(" WARNING"):
                    self._text.tag_add("warn", f"{i}.0", f"{i}.end")
                elif " ERROR " in line or " CRITICAL " in line:
                    self._text.tag_add("error", f"{i}.0", f"{i}.end")
        else:
            self._text.insert("1.0", "（日志为空或尚未生成）")
        self._text.configure(state="disabled")
        if follow:
            self._text.see("end")
        elif anchor:
            self._text.yview(anchor)

    def content(self) -> str:
        return self._text.get("1.0", "end-1c")

    def _open_in_editor(self) -> None:
        """兜底：仍允许用系统默认程序打开（用户习惯 + 排障时看原始文件）。"""
        try:
            if os.name == "nt":
                os.startfile(self.path)  # noqa: S606
            else:
                import webbrowser
                webbrowser.open(self.path.as_uri())
        except OSError as exc:
            self._hint.configure(text=f"无法打开：{exc}", fg="#d1a54a")

    def destroy(self) -> None:
        if self._after_id is not None:
            try:
                self.after_cancel(self._after_id)
            except Exception:  # noqa: BLE001
                pass
            self._after_id = None
        super().destroy()
