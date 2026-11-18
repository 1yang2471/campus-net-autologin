package keeper

import "testing"

// 物理源重发现缝（移植 Python tick 的 portal_down 分支：网络切换/休眠唤醒后
// 旧自动发现的源 IP 会失效，表现为长期 portal_down；每等待周期重发现一次）。
// 缝只报告"什么时候该重发现"，怎么重发现归组合根（netstack.DiscoverSource）。

// 行为：门户不可达（窗口外）→ 每拍报告一次重发现时机。
func TestOnPortalDownFiresOnPortalDownTick(t *testing.T) {
	calls := 0
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return false }
	d.OnPortalDown = func() { calls++ }
	d.Tick()
	if calls != 1 {
		t.Errorf("portal_down 拍应报告重发现时机，实际 %d 次", calls)
	}
	if d.State != StatePortalDown {
		t.Errorf("应显示 portal_down，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：免认证窗口内门户不可达是常态 → 不得报告重发现（Python：窗口内无登录
// 无过期源问题，重发现留到窗口外）。
func TestOnPortalDownSilentInFreeWindow(t *testing.T) {
	calls := 0
	d := NewDaemon("00:00-24:00")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return false }
	d.OnPortalDown = func() { calls++ }
	d.Tick()
	if calls != 0 {
		t.Errorf("窗口内不得报告重发现，实际 %d 次", calls)
	}
}

// 行为：在线/免认证/登录失败拍都不报告（只有 portal_down 才有旧源失效嫌疑）。
func TestOnPortalDownSilentOnHealthyTicks(t *testing.T) {
	calls := 0
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"用户名称": "u"}, nil }
	d.OnPortalDown = func() { calls++ }
	d.Tick()
	d.Tick()
	if calls != 0 {
		t.Errorf("健康拍不得报告重发现，实际 %d 次", calls)
	}
}
