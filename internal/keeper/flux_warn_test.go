package keeper

import "testing"

// 切片3：流量阈值提醒。需求是「每个阈值只提醒一次」——守护每 10 分钟查一次
// flux，若每次越阈都发通知，用户会被刷屏；回落后再涨上来也不该重复发。

func TestFluxWarnLevel(t *testing.T) {
	cases := []struct {
		pct  float64
		want string
	}{
		{0, "ok"}, {79.9, "ok"}, {80, "warn"}, {89.9, "warn"},
		{90, "critical"}, {100, "critical"},
	}
	for _, c := range cases {
		if got := fluxWarnLevel(c.pct, 80, 90); got != c.want {
			t.Errorf("fluxWarnLevel(%.1f) = %q, 期望 %q", c.pct, got, c.want)
		}
	}
}

// 行为：算不出百分比 → 不参与阈值（不能因为「未知」乱发通知）。
func TestFluxWarnIgnoresUnknownPercent(t *testing.T) {
	w := newFluxWarner(80, 90)
	if ev := w.observe(false, 0); ev != "" {
		t.Errorf("算不出百分比不该发提醒，实际 %q", ev)
	}
}

// 行为：80%/90% 各触发一次；同一档连续观察不重复；回落后不再发。
func TestFluxWarnFiresOncePerThreshold(t *testing.T) {
	w := newFluxWarner(80, 90)
	if ev := w.observe(true, 50); ev != "" {
		t.Fatalf("50%% 不该发，实际 %q", ev)
	}
	if ev := w.observe(true, 82); ev != "warn" {
		t.Fatalf("首次越 80%% 应发 warn，实际 %q", ev)
	}
	if ev := w.observe(true, 85); ev != "" {
		t.Errorf("同一档连续观察不该重复发，实际 %q", ev)
	}
	if ev := w.observe(true, 91); ev != "critical" {
		t.Fatalf("首次越 90%% 应发 critical，实际 %q", ev)
	}
	if ev := w.observe(true, 95); ev != "" {
		t.Errorf("critical 连续观察不该重复发，实际 %q", ev)
	}
	// 回落到 80 以下：不该再发；再次涨到 82 也不重复（本轮已提醒过）。
	if ev := w.observe(true, 50); ev != "" {
		t.Errorf("回落不该发，实际 %q", ev)
	}
	if ev := w.observe(true, 82); ev != "" {
		t.Errorf("本轮已提醒过，重新越阈不该重复发，实际 %q", ev)
	}
}

// 行为：阈值可配（异校额度口径不同）。
func TestFluxWarnConfigurableThresholds(t *testing.T) {
	w := newFluxWarner(50, 75)
	if ev := w.observe(true, 55); ev != "warn" {
		t.Errorf("配 50 阈值时 55%% 应 warn，实际 %q", ev)
	}
	if ev := w.observe(true, 76); ev != "critical" {
		t.Errorf("配 75 阈值时 76%% 应 critical，实际 %q", ev)
	}
}
