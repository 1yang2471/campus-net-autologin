# -*- coding: utf-8 -*-
"""工业仪表调色板（暗色默认、CSS 变量式集中定义）。

面板与辅助窗口（流量/日志）共用同一套配色，保证视觉一致。
原先这些常量写在 shell/app.py 里，辅助窗口要复用就得反向 import 面板，
故抽到独立模块（面板与窗口都只依赖本文件，无循环依赖）。
"""
from __future__ import annotations

BG = "#14161a"          # 底色：近黑，带一点冷
PANEL = "#1d2026"       # 面板
PANEL_EDGE = "#2a2e36"
FG = "#e8eaed"          # 主文字
FG_DIM = "#8b919b"      # 次级文字
# 语义色（降饱和进石墨世界；与 keeper.TRAY_COLORS 同源，漂移有测试拦截）
OK_RGB = (79, 157, 99)      # 在线
WARN_RGB = (209, 145, 63)   # 掉线/受限
ERR_RGB = (208, 95, 82)     # 被拒
IDLE_RGB = (139, 145, 155)  # 启动中（= FG_DIM，进灰阶）
MONO = ("Consolas", 11)  # 遥测数字
DISPLAY = ("Microsoft YaHei UI", 10)
DISPLAY_BOLD = ("Microsoft YaHei UI", 22, "bold")
DISPLAY_SMALL = ("Microsoft YaHei UI", 9)


def lamp_photo(size: int = 32, rgb: tuple = OK_RGB, master=None):
    """状态灯窗口图标：托盘、面板、辅助窗共用同一盏灯（signature 细节）。

    返回 ImageTk.PhotoImage——调用方必须持有引用（GC 会清图像数据）。
    master 必须显式传：Pillow 的 PhotoImage 缺省绑 tkinter._default_root，
    而本程序多 Tk 根且旧根退出不复位全局引用——绑死根直接
    "main thread is not in main loop"（2026-11-04 测试实测炸 6 项）。
    """
    from PIL import Image, ImageDraw, ImageTk
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    m = max(2, size // 8)
    d.ellipse([m, m, size - m, size - m], fill=rgb + (255,),
              outline=(42, 46, 54, 255), width=max(1, size // 16))
    return ImageTk.PhotoImage(img, master=master)


def apply_dark_ttk(root) -> None:
    """ttk 控件暗色化：输入框/下拉框/滚动条与面板同底同字。

    根因修复（用户截图）：Desk 与面板是两个 Tk 根，样式按根生效——
    只给面板根应用时，Desk 里的日志窗滚动条仍是浅色原生条，
    悬在暗色窗口右侧像贴了张便利贴。两个根都必须过这里。
    """
    import tkinter as tk  # noqa: F401  调用方传入的是 Tk/Toplevel 根
    from tkinter import ttk
    st = ttk.Style(root)
    try:
        st.theme_use("clam")
    except tk.TclError:
        pass
    # borderwidth=1：clam 默认 2px 斜角边框的四角像素不跟随 bordercolor/
    # lightcolor/darkcolor，回落主题默认亮灰 → 输入框四角白点（用户实测，
    # A/B 像素取证：bw2 角 (142,143,145) vs bw1 边 (41,41,42)≈PANEL_EDGE）
    st.configure("TEntry", fieldbackground=PANEL, foreground=FG,
                 insertcolor=FG, bordercolor=PANEL_EDGE, lightcolor=PANEL,
                 darkcolor=PANEL, borderwidth=1)
    st.configure("TCombobox", fieldbackground=PANEL, background=PANEL,
                 foreground=FG, arrowcolor=FG, selectbackground=PANEL,
                 selectforeground=FG, bordercolor=PANEL_EDGE,
                 lightcolor=PANEL, darkcolor=PANEL, borderwidth=1)
    st.map("TCombobox", fieldbackground=[("readonly", PANEL)],
           foreground=[("readonly", FG)])
    st.configure("TScrollbar", background="#3a4048", troughcolor=PANEL,
                 bordercolor=PANEL, lightcolor=PANEL, darkcolor=PANEL,
                 arrowcolor=FG_DIM, arrowsize=12)
    st.map("TScrollbar", background=[("active", "#4a525c")])
