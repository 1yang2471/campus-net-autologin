package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 面板打开方式（P1）：WebView2 原生窗口常驻会多出约 360MB WS；用户可配
// panel_mode=browser 改用系统默认浏览器（Chrome 已开时增量很小）。
// 缺省保持 native，不偷偷改掉既有行为。

func TestPanelModeFromEnvDefaultNative(t *testing.T) {
	t.Setenv("CNAL_PANEL_MODE", "")
	if got := panelMode(""); got != "native" {
		t.Errorf("缺省应为 native，实际 %q", got)
	}
}

func TestPanelModeFromIniAndEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	if err := os.WriteFile(p, []byte("[keeper]\npanel_mode = browser\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// ini 值优先于环境变量（用户显式配置优先）。
	t.Setenv("CNAL_PANEL_MODE", "native")
	if got := panelMode(p); got != "browser" {
		t.Errorf("ini panel_mode=browser 应生效，实际 %q", got)
	}
}

func TestPanelModeEnvOverride(t *testing.T) {
	t.Setenv("CNAL_PANEL_MODE", "browser")
	if got := panelMode(""); got != "browser" {
		t.Errorf("环境变量应能覆盖缺省，实际 %q", got)
	}
	t.Setenv("CNAL_PANEL_MODE", "not-a-mode")
	if got := panelMode(""); got != "native" {
		t.Errorf("非法值应回落 native，实际 %q", got)
	}
}

func TestOpenPanelByModeBrowser(t *testing.T) {
	// 浏览器模式只验证「不走 WebView2」：用一个会失败的 URL 调 openURL 会真开
	// 浏览器进程，测试里改为断言分派结果（calls 记录），不触系统。
	native, browser := 0, 0
	openPanelByMode("browser", "http://127.0.0.1:1/panel",
		func(string) error { native++; return nil },
		func(string) error { browser++; return nil })
	if native != 0 || browser != 1 {
		t.Errorf("browser 模式应只调 openURL，实际 native=%d browser=%d", native, browser)
	}
}

func TestOpenPanelByModeNativeFallsBackOnError(t *testing.T) {
	native, browser := 0, 0
	openPanelByMode("native", "http://127.0.0.1:1/panel",
		func(string) error { native++; return os.ErrPermission },
		func(string) error { browser++; return nil })
	if native != 1 || browser != 1 {
		t.Errorf("native 失败应回落浏览器，实际 native=%d browser=%d", native, browser)
	}
}
