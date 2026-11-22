package main

import (
	"strings"
	"testing"
)

// 多通道行（2026-11-20 双通道 M1）：门户按源 IP 记会话，每张物理卡各自一份
// 认证态。面板必须逐通道显示「哪张卡、什么态」——只列 IP 用户认不出是哪张卡，
// 只说「未认证」用户不知道该不该管它。
//
// 兼容：旧内核没有 channels 字段时，回落到 2026-11-19 的 unauth_sources 展示
// （那一行的语义仍在），不得整行留空。

func TestPanelShowsChannelsRow(t *testing.T) {
	page := panelPage()
	for _, want := range []string{
		`id="channels"`,
		"通道",
		"channels",
		"renderChannels",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("面板页缺通道内容: %s", want)
		}
	}
}

// 行为：JS 里必须有「逐通道渲染」且旧字段兜底仍在。
func TestPanelChannelsJS(t *testing.T) {
	js := panelJS()
	for _, want := range []string{
		"renderChannels",
		"channels",
		"unauth_sources", // 旧内核兜底
		"iface",
		"label",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("面板 JS 缺通道渲染要素: %s", want)
		}
	}
}
