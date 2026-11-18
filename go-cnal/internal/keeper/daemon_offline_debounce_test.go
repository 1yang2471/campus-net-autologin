package keeper

import "testing"

// L1 连续失败去抖（2026-11-13 登录风暴实证）：probe_timeout 很小时单拍探测
// 超时会被误判「掉线」并立刻触发一次自动登录（间隔≥30s、上限 8 次/小时）。
// 日志实证：16:25:28 离线（速率闸门）→ 16:25:29 在线，相邻两拍 1 秒间隔；
// 三次「闸门关闭 15 分钟」= 每小时 8+ 次登录，正好撞校园反暴力冻结线（12/h）。
// 修复：L1 连续 offlineLoginDebounce 拍失败才进入重连；单拍失败如实报离线、
// 但不登录。会话判死（L1 绿）不在此列——它已有自己的 2 拍防抖。

// 行为：单拍/两拍失败不登录；连续第 3 拍才登录一次。
func TestOfflineLoginDebounce(t *testing.T) {
	calls := 0
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.Login = func() (string, error) { calls++; return "", nil }

	d.Tick()
	if calls != 0 {
		t.Fatalf("单拍失败不得登录（去抖），实际 %d 次", calls)
	}
	if d.State != StateOffline {
		t.Errorf("去抖期应如实报离线，实际 %v/%v", d.State, d.Detail)
	}
	d.Tick()
	if calls != 0 {
		t.Fatalf("两拍失败不得登录，实际 %d 次", calls)
	}
	d.Tick()
	if calls != 1 {
		t.Fatalf("连续 3 拍失败应登录一次，实际 %d 次", calls)
	}
}

// 行为：失败计数在锚点恢复时清零——短暂的抖动不会累积成登录。
func TestOfflineDebounceResetsOnRecovery(t *testing.T) {
	calls := 0
	probe := false
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return probe }
	d.PortalAlive = func() bool { return true }
	d.Login = func() (string, error) { calls++; return "", nil }

	d.Tick() // streak=1
	d.Tick() // streak=2
	probe = true
	d.Tick() // 恢复 → 清零
	probe = false
	d.Tick() // 只有 1 拍，不得登录
	if calls != 0 {
		t.Fatalf("恢复清除计数后，新的一拍失败不得登录，实际 %d 次", calls)
	}
}
