package main

import (
	"strings"
	"testing"
)

// 通道行（2026-11-20 双通道 M1）：面板必须能显示每张物理卡的认证态，
// 并说明 Windows「无 Internet」可能是误报。字段缺失（旧内核）时回落旧说法。
//
// 注意：2026-11-19 那条独立的「其他网卡」行已并入本行——同一事实两处展示
// 是重复（旧行只列未认证 IP，新行还带接口名与四态，信息更全）。
func TestPanelShowsUnauthSiblings(t *testing.T) {
	page := panelPage()
	for _, want := range []string{
		`id="channels"`,
		"通道",
		"unauth_sources", // 旧内核回落字段
		"renderChannels",
		"Windows 可能误报无 Internet",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("面板页缺通道内容: %s", want)
		}
	}
	// 旧的重复行必须已删（否则同一事实两处展示）。
	if strings.Contains(page, `id="nics"`) {
		t.Error("旧的「其他网卡」行应已并入「通道」行（同一事实两处展示）")
	}
}
