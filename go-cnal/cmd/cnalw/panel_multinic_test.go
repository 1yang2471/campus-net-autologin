package main

import (
	"strings"
	"testing"
)

// 旁路网卡行（2026-11-19）：面板必须能显示「已连接但未认证」的其他网卡，
// 并说明 Windows「无 Internet」可能是误报。字段缺失（旧内核）时显示占位。
func TestPanelShowsUnauthSiblings(t *testing.T) {
	page := panelPage()
	for _, want := range []string{
		`id="nics"`,
		"其他网卡",
		"unauth_sources",
		"renderNics",
		"Windows 可能误报无 Internet",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("面板页缺旁路网卡内容: %s", want)
		}
	}
}
