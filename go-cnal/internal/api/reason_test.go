package api

import (
	"strings"
	"testing"
)

// 原因覆盖状态名（2026-11-18 双读数）：本地 30 秒闸原来顶着 rejected 的
// 「登录被拒」红字，其实是守护自己暂停。词表只在 api 包；壳层不自造。

func TestReasonLabelOverrides(t *testing.T) {
	if lbl, ok := ReasonLabel("local_throttle"); !ok || lbl != "自动登录暂停" {
		t.Errorf("local_throttle 应覆盖为「自动登录暂停」，实际 %q/%v", lbl, ok)
	}
	if _, ok := ReasonLabel("online"); ok {
		t.Error("正常原因不应覆盖状态名")
	}
	if c, ok := ReasonColor("local_throttle"); !ok || c != "#8b919b" {
		t.Errorf("本地节流应为中性灰，实际 %q/%v", c, ok)
	}
	for _, r := range []string{"binding_invalid", "portal_unreachable", "local_throttle"} {
		if ReasonText(r) == "" {
			t.Errorf("原因 %q 缺一句话说明", r)
		}
	}
	if ReasonText("online") != "" {
		t.Error("正常原因不应有额外说明")
	}
}

// AC 假在线组合态词表（2026-11-22 复盘修复项 2）：主状态词「会话在线｜出口
// 被控」走 ReasonLabel 覆盖机制进 Label；dns_broken_observed 与 l2_observed
// 对称。面板/托盘不自造词表，依赖这三张表。
func TestFakeOnlineAndDnsBrokenVocabulary(t *testing.T) {
	if lbl, ok := ReasonLabel("fake_online"); !ok || lbl != "会话在线｜出口被控" {
		t.Errorf("fake_online 应覆盖为「会话在线｜出口被控」，实际 %q/%v", lbl, ok)
	}
	if c, ok := ReasonColor("fake_online"); !ok || c != "#d1913f" {
		t.Errorf("fake_online 应为警示橙，实际 %q/%v", c, ok)
	}
	txt := ReasonText("fake_online")
	if txt == "" || !strings.Contains(txt, "重连救不了") {
		t.Errorf("fake_online 说明应含行动指引，实际 %q", txt)
	}
	if _, ok := ReasonLabel("dns_broken_observed"); ok {
		t.Error("dns_broken_observed 不覆盖状态名（与 l2_observed 对称：解析异常是观察读数，不是状态）")
	}
	if ReasonText("dns_broken_observed") == "" {
		t.Error("dns_broken_observed 说明词表缺失")
	}
}
