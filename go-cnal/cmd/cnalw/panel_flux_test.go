package main

import (
	"strings"
	"testing"
)

// 流量卡片（只读能力升级）：面板从 /state 的 flux 字段常驻渲染，不再只靠
// 手动点「查流量」。缺失/算不出百分比要如实留空，不编造数字。

func TestPanelHasFluxCard(t *testing.T) {
	page := panelPage()
	for _, want := range []string{
		`id="flux-card"`, `id="flux-used"`, `id="flux-quota"`,
		`id="flux-percent"`, `id="flux-bar"`, "今日流量",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("面板页缺流量卡片节点: %s", want)
		}
	}
}

func TestPanelRendersFluxFromStateSnapshot(t *testing.T) {
	page := panelPage()
	for _, want := range []string{
		"s.flux",                 // 从 /state 快照读，不单独打 /flux
		"flux-bar",               // 进度条宽度按 percent 设
		"has_percent",            // 算不出百分比时不硬填
	} {
		if !strings.Contains(page, want) {
			t.Errorf("面板脚本缺 flux 渲染逻辑: %s", want)
		}
	}
}
