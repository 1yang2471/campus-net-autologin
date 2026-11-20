package main

import (
	"strings"
	"testing"
)

// 多通道自愈开关（2026-11-20 双通道 M2）：面板必须能开/关通道级登录。
// 默认关（沿用 11-19 纪律），用户显式打开才生效——开关缺了，用户就只能
// 改 ini 重启，太重的路径等于没有这个功能。

func TestPanelHasMultiChannelToggle(t *testing.T) {
	page := panelPage()
	for _, want := range []string{
		`id="f-multich"`,
		"多通道自愈",
		"multi_channel",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("面板页缺多通道开关: %s", want)
		}
	}
}

// 行为：开关状态要能回显（读配置）与保存（写 ini）。
func TestPanelMultiChannelJS(t *testing.T) {
	js := panelJS()
	for _, want := range []string{
		"f-multich",
		"multi_channel",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("面板 JS 缺多通道开关要素: %s", want)
		}
	}
}
