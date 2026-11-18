package api

import "testing"

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
