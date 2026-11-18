package main

import (
	"strings"
	"testing"
)

// 行为：拆成 CSS/JS/HTML 三份源料后仍拼回一份完整面板页——占位符必须被
// 替换，关键节点/样式/脚本都在（防止拆文件时漏段或路径写错）。
func TestPanelPageAssembles(t *testing.T) {
	page := panelPage()
	for _, bad := range []string{"{{PANEL_CSS}}", "{{PANEL_JS}}"} {
		if strings.Contains(page, bad) {
			t.Errorf("占位符未替换: %s", bad)
		}
	}
	for _, want := range []string{"free-list", "flux-btn", "--surface", "collectFreeWindows", "loadConfig", "log-card"} {
		if !strings.Contains(page, want) {
			t.Errorf("面板页缺关键内容: %s", want)
		}
	}
}

// 行为：面板显示「门户路径」（2026-11-11 设计 §4：走哪条路、为什么，用户一眼
// 要能看出来）——诊断卡要有这一行，脚本要从 /state 的 route 字段填。
func TestPanelShowsPortalRoute(t *testing.T) {
	page := panelPage()
	for _, want := range []string{`id="route"`, "门户路径", "s.route"} {
		if !strings.Contains(page, want) {
			t.Errorf("面板页缺门户路径读数: %s", want)
		}
	}
}
