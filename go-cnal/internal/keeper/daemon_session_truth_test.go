package keeper

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// 会话真值触发（2026-11-18）：用户对「断开登录态」的定义是——浏览器打开登录页
// 发现没登录/被踢。对应到守护：门户 online_check 说这个源不在线才登录；说在线
// 就不登录（锚点红是 L2/线路问题，登录修不了）；真值不可得才回落 L1 去抖。
//
// 这条把「锚点红」从登录触发降级为证据之一，直接消掉 17:48 那种「L1 红 +
// 门户在线却登录」的假动作。

// 行为：L1 红 + 门户说会话在线 → 不登录；状态说「认证在线」，不是「离线」。
func TestL1RedSessionAliveDoesNotLogin(t *testing.T) {
	calls := 0
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return true, nil }
	d.Login = func() (string, error) { calls++; return "", nil }

	for i := 0; i < 5; i++ {
		d.Tick()
	}
	if calls != 0 {
		t.Fatalf("门户说在线时不得登录（锚点红是 L2/线路问题），实际 %d 次", calls)
	}
	if d.State != StateOnline {
		t.Errorf("认证在线应显示在线态，实际 %v/%v", d.State, d.Detail)
	}
	if !strings.Contains(d.Detail, "认证在线") {
		t.Errorf("状态行应说清「认证在线」，实际 %q", d.Detail)
	}
}

// 行为：L1 红 + 门户说会话不在线 → 第一拍就登录（会话真值是权威证据，
// 不再等 L1 三拍去抖）。
func TestL1RedSessionDeadLoginsImmediately(t *testing.T) {
	calls := 0
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return false, nil }
	d.Login = func() (string, error) { calls++; return "", nil }

	d.Tick()
	if calls != 1 {
		t.Fatalf("会话真值说死应立即登录，实际 %d 次", calls)
	}
}

// 行为：真值不可得（门户解析失败）→ 回落 L1 三拍去抖，前两拍不登录。
func TestL1RedSessionUnknownFallsBackToDebounce(t *testing.T) {
	calls := 0
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return false, errors.New("绑源解析门户失败") }
	d.Login = func() (string, error) { calls++; return "", nil }

	d.Tick()
	d.Tick()
	if calls != 0 {
		t.Fatalf("真值不可得的前两拍不得登录，实际 %d 次", calls)
	}
	d.Tick()
	if calls != 1 {
		t.Fatalf("连续 3 拍 L1 红应回落去抖并登录一次，实际 %d 次", calls)
	}
}

// 行为：L1 红时的会话核实有 10s 下限——不能每拍都打 online_check。
func TestL1RedSessionProbeThrottled(t *testing.T) {
	probes := 0
	clk := time.Unix(0, 0)
	d := NewDaemon("")
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { probes++; return true, nil }
	d.Login = func() (string, error) { return "", nil }

	d.Tick()
	clk = clk.Add(time.Second)
	d.Tick()
	clk = clk.Add(time.Second)
	d.Tick()
	if probes != 1 {
		t.Fatalf("10s 内只应核实一次会话，实际 %d 次", probes)
	}
	clk = clk.Add(11 * time.Second)
	d.Tick()
	if probes != 2 {
		t.Fatalf("过了 10s 下限应重新核实，实际 %d 次", probes)
	}
}

// 行为：L1 恢复后再红，第一拍必须重新核实（不能复用上一轮的旧结论）。
func TestL1RecoveryResetsRedSessionCache(t *testing.T) {
	probes := 0
	clk := time.Unix(0, 0)
	up := false
	d := NewDaemon("")
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return up }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { probes++; return true, nil }
	d.Login = func() (string, error) { return "", nil }

	d.Tick() // 红：probe #1
	if probes != 1 {
		t.Fatalf("前置：红拍应核实一次，实际 %d", probes)
	}
	up = true
	clk = clk.Add(time.Second)
	d.Tick() // 绿：重置红态缓存
	up = false
	clk = clk.Add(time.Second)
	d.Tick() // 再红：应重新核实
	if probes != 2 {
		t.Fatalf("恢复后再红应重新核实，实际 %d 次", probes)
	}
}
