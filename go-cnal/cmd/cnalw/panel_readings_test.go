package main

import (
	"strings"
	"testing"
)

// 双读数行（2026-11-18）：面板状态卡在状态/详情下面并列显示「认证读数 +
// 用户路径读数 + 原因」；旧内核缺字段时整行留空，不编造。
func TestPanelPageHasDualReadings(t *testing.T) {
	page := panelPage()
	for _, want := range []string{
		`id="readings"`,
		"renderReadings",
		"auth_state",
		"user_path",
		"reason_label",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("面板页缺双读数内容: %s", want)
		}
	}
}
