package keeper

import "testing"

// G2：四态状态机骨架（移植 Python keeper tick 探测层语义）。
// 探测缝全部可替换——测试不触网。窗口用 00:00-24:00 全天段（任意时刻命中）。

func newTestDaemon(freeWindows string, probe, portal bool) *Daemon {
	d := NewDaemon(freeWindows)
	d.ProbeOnline = func() bool { return probe }
	d.PortalAlive = func() bool { return portal }
	return d
}

// 行为：L1 锚点绿 → 在线。
func TestTickOnlineWhenProbeGreen(t *testing.T) {
	d := newTestDaemon("", true, true)
	d.Tick()
	if d.State != StateOnline {
		t.Errorf("锚点绿应在线，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：窗口内锚点失败 → 中性免认证态，不冒充离线（Python t44 语义）。
func TestTickFreeWindowOnAnchorFailInWindow(t *testing.T) {
	d := newTestDaemon("00:00-24:00", false, true)
	d.Tick()
	if d.State != StateFreeWindow {
		t.Errorf("窗口内锚点失败应为免认证态，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：窗口外锚点失败 + 门户活着 → 离线（白天的真掉线语义不变）。
func TestTickOfflineOutsideWindow(t *testing.T) {
	d := newTestDaemon("", false, true)
	d.Tick()
	if d.State != StateOffline {
		t.Errorf("窗口外真掉线应离线，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：门户不可达（窗口外）→ portal_down。
func TestTickPortalDownOutsideWindow(t *testing.T) {
	d := newTestDaemon("", false, false)
	d.Tick()
	if d.State != StatePortalDown {
		t.Errorf("门户不可达应为 portal_down，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：窗口内门户不可达 → 免认证态（Python t44 第二语义）。
func TestTickPortalDownInWindowShowsFreeWindow(t *testing.T) {
	d := newTestDaemon("00:00-24:00", false, false)
	d.Tick()
	if d.State != StateFreeWindow {
		t.Errorf("窗口内门户不可达应为免认证态，实际 %v/%v", d.State, d.Detail)
	}
}
