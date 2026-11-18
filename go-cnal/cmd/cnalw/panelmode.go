package main

import (
	"bufio"
	"os"
	"strings"
)

// 面板打开方式：缺省 native（WebView2 原生窗口，与既有行为一致）；
// panel_mode=browser 时改用系统默认浏览器（Chrome 已开时增量内存远小于
// 常驻 WebView2 的 7 进程 360MB）。ini 值优先于环境变量，非法值回落 native。
const (
	panelModeNative  = "native"
	panelModeBrowser = "browser"
)

// panelMode 从 ini（[keeper]/[portal] 节）与环境变量取模式。
func panelMode(cfgPath string) string {
	mode := ""
	if cfgPath != "" {
		if f, err := os.Open(cfgPath); err == nil {
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
					continue
				}
				k, v, found := strings.Cut(line, "=")
				if found && strings.TrimSpace(k) == "panel_mode" {
					mode = strings.TrimSpace(v)
				}
			}
			_ = f.Close()
		}
	}
	if mode == "" {
		mode = os.Getenv("CNAL_PANEL_MODE")
	}
	return normalizePanelMode(mode)
}

func normalizePanelMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case panelModeBrowser:
		return panelModeBrowser
	default:
		return panelModeNative // 含空值与非法值：不因为坏配置改变既有行为
	}
}

// openPanelByMode 按模式分派打开动作。native 失败（WebView2 运行时缺失等）
// 仍回落浏览器——与既有「原生不可用回落浏览器」行为一致。
// 两个函数缝让测试可以验证分派而不真起窗口/浏览器进程。
func openPanelByMode(mode, url string, native, browser func(string) error) error {
	if mode == panelModeBrowser {
		return browser(url)
	}
	if err := native(url); err != nil {
		return browser(url)
	}
	return nil
}
