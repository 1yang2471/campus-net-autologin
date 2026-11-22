package main

import (
	"strings"
	"testing"

	"cnal/internal/api"
)

func TestStatusLineShowsUserPathOnlyWhenBroken(t *testing.T) {
	s := api.Snapshot{Label: "在线", Detail: "出口受限（观察）"}
	if got := statusLine(s); got != "在线｜出口受限（观察）" {
		t.Errorf("基础拼接不对：%q", got)
	}
	s.UserPath = "broken"
	if !strings.Contains(statusLine(s), "用户路径不通") {
		t.Errorf("用户路径断时必须说：%q", statusLine(s))
	}
	s.UserPath = "ok"
	if strings.Contains(statusLine(s), "用户路径") {
		t.Errorf("用户路径正常时不占长度：%q", statusLine(s))
	}
	s.Detail = ""
	if got := statusLine(s); got != "在线" {
		t.Errorf("空详情不得留分隔符：%q", got)
	}
}

// 2026-11-22 修复项 4：MenuLine 非空优先（内核单源渲染的短形态进菜单行），
// 空则兜底全量拼法（测试替身/旧快照）。
func TestStatusLinePrefersMenuLine(t *testing.T) {
	s := api.Snapshot{
		Label: "在线", Detail: "出口受限持续较久（大众站 TLS 被重置≥10 分钟；认证正常；勿反复重登）",
		MenuLine: "在线｜出口受限持续较久",
	}
	if got := statusLine(s); got != "在线｜出口受限持续较久" {
		t.Errorf("MenuLine 非空应优先：%q", got)
	}
	s.MenuLine = ""
	if got := statusLine(s); got != "在线｜出口受限持续较久（大众站 TLS 被重置≥10 分钟；认证正常；勿反复重登）" {
		t.Errorf("MenuLine 空应兜底全量拼法：%q", got)
	}
}

// 2026-11-22 修复项 4：tooltip 与菜单行分家——tooltip 保留完整读数。
func TestTooltipLineKeepsFullDetail(t *testing.T) {
	s := api.Snapshot{
		Label: "在线", Detail: "出口受限持续较久（大众站 TLS 被重置≥10 分钟；认证正常；勿反复重登）",
		MenuLine: "在线｜出口受限持续较久",
	}
	got := tooltipLine(s)
	if !strings.Contains(got, "TLS 被重置") || !strings.Contains(got, "勿反复重登") {
		t.Errorf("tooltip 应保留完整读数：%q", got)
	}
}
