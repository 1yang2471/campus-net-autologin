package api

import "testing"

// MenuLine 短形态规则（2026-11-22 复盘修复项 4，单源渲染）：主状态词 + 详情
// 第一段；「｜」与「（」都是段界。11-22 用户点名：Detail 全量几十字进托盘
// 菜单行，把菜单撑到不可读。
func TestMenuLineShortForm(t *testing.T) {
	got := MenuLine("在线",
		"出口受限持续较久（大众站 TLS 被重置≥10 分钟；认证正常；会话由 online_check 独立核实，勿反复重登）", "")
	want := "在线｜出口受限持续较久"
	if got != want {
		t.Errorf("菜单行应只留主词+第一段：%q，应 %q", got, want)
	}
}

// 行为：fake_online 组合态——Label「会话在线｜出口被控」含「｜」，detail 首段
// 是细节（不与 Label 后半重复），菜单行信息不重叠。
func TestMenuLineFakeOnline(t *testing.T) {
	got := MenuLine("会话在线｜出口被控",
		"锚点+用户路径持续红≥10 分钟（会话由 online_check 独立核实）｜重连救不了：等待恢复或换网络，可查梯子残留", "")
	want := "会话在线｜出口被控｜锚点+用户路径持续红≥10 分钟"
	if got != want {
		t.Errorf("组合态菜单行=%q，应 %q", got, want)
	}
}

// 行为：多段详情只留首段；userPath=broken 才附「用户路径不通」。
func TestMenuLineMultiSegmentAndUserPath(t *testing.T) {
	got := MenuLine("会话在线｜出口被控",
		"锚点+用户路径持续红≥10 分钟（会话由 online_check 独立核实）｜重连救不了：等待恢复或换网络，可查梯子残留",
		"broken")
	want := "会话在线｜出口被控｜锚点+用户路径持续红≥10 分钟｜用户路径不通"
	if got != want {
		t.Errorf("多段详情+broken 附注=%q，应 %q", got, want)
	}
}

// 行为：空详情不加分隔符；ok 路径不附用户路径段。
func TestMenuLineEmptyDetail(t *testing.T) {
	if got := MenuLine("在线", "", "ok"); got != "在线" {
		t.Errorf("空详情不得留分隔符：%q", got)
	}
}
