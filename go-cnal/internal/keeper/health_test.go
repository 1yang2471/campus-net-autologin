package keeper

import (
	"testing"
	"time"
)

// 双读数（2026-11-18）：认证（物理栈）与用户路径（系统栈）分开报，不一致时
// 并列显示；reason 必须能说清「为什么不动作」。这些是面板/托盘/API 的唯一
// 词表来源，壳层不得自造。

func TestHealthUserPathBrokenWhileAuthOnline(t *testing.T) {
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return true, nil }
	d.UserPath = func() PathReading {
		return PathReading{OK: false, Known: true, Detail: "系统栈锚点超时"}
	}
	d.Tick()

	h := d.Health()
	if h.Auth != AuthOnline {
		t.Errorf("认证读数应为 online，实际 %q（%v/%v）", h.Auth, d.State, d.Detail)
	}
	if h.UserPath != PathBroken {
		t.Errorf("用户路径应为 broken，实际 %q", h.UserPath)
	}
	if h.UserDetail != "系统栈锚点超时" {
		t.Errorf("用户路径原因应保留原文，实际 %q", h.UserDetail)
	}
}

func TestHealthUserPathNilIsUnknown(t *testing.T) {
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return true, nil }
	d.Tick()

	if h := d.Health(); h.UserPath != PathUnknown {
		t.Errorf("未接线用户路径探针应报 unknown，实际 %q", h.UserPath)
	}
}

func TestHealthBindingInvalidReason(t *testing.T) {
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.EgressHealth = func() EgressHealth { return invalidEgress(EgressBindingInvalid) }
	d.Tick()

	h := d.Health()
	if h.Reason != "binding_invalid" {
		t.Errorf("原因应为 binding_invalid，实际 %q", h.Reason)
	}
	if h.Action != ActionReselecting {
		t.Errorf("动作应为 reselecting，实际 %q", h.Action)
	}
	if h.Auth != AuthOffline {
		t.Errorf("认证读数应为 offline（无法确认），实际 %q", h.Auth)
	}
}

func TestHealthPortalUnreachableReason(t *testing.T) {
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.EgressHealth = func() EgressHealth { return invalidEgress(EgressPortalUnreachable) }
	d.Tick()

	h := d.Health()
	if h.Reason != "portal_unreachable" || h.Action != ActionReselecting {
		t.Errorf("原因/动作应为 portal_unreachable/reselecting，实际 %q/%q", h.Reason, h.Action)
	}
	if h.Auth != AuthUnknown {
		t.Errorf("门户不可达时认证读数应为 unknown，实际 %q", h.Auth)
	}
}

// 行为：本地 30 秒闸不能再显示成「门户拒绝」——reason=local_throttle、
// 认证读数 unknown（没问过门户），不是 offline。
func TestHealthLocalThrottleIsNotPortalRejected(t *testing.T) {
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return false, nil }
	d.Login = func() (string, error) { return "", nil }

	d.Tick() // 会话真值说死 → 登录成功
	d.Tick() // 30s 闸内：本地节流
	h := d.Health()
	if h.Reason != "local_throttle" {
		t.Errorf("原因应为 local_throttle，实际 %q（%v/%v）", h.Reason, d.State, d.Detail)
	}
	if h.Auth != AuthUnknown {
		t.Errorf("本地节流未问过门户，认证读数应为 unknown，实际 %q", h.Auth)
	}
}

// 行为：L1 红但会话在线 → 认证 online，reason 标出锚点异常。
func TestHealthL1RedSessionAliveReason(t *testing.T) {
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return true, nil }
	d.Tick()

	h := d.Health()
	if h.Auth != AuthOnline {
		t.Errorf("会话在线时认证读数应为 online，实际 %q", h.Auth)
	}
	if h.Reason != "l1_red_session_alive" {
		t.Errorf("原因应为 l1_red_session_alive，实际 %q（detail=%q）", h.Reason, d.Detail)
	}
}

// 行为：用户路径读数 5s 节流，不能每拍都打系统栈锚点。
func TestUserPathProbeThrottled(t *testing.T) {
	probes := 0
	clk := time.Unix(0, 0)
	d := NewDaemon("")
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return true, nil }
	d.UserPath = func() PathReading {
		probes++
		return PathReading{OK: true, Known: true}
	}

	d.Tick()
	clk = clk.Add(time.Second)
	d.Tick()
	clk = clk.Add(time.Second)
	d.Tick()
	if probes != 1 {
		t.Fatalf("5s 内只应采一次用户路径，实际 %d 次", probes)
	}
	clk = clk.Add(4 * time.Second)
	d.Tick()
	if probes != 2 {
		t.Fatalf("过了 5s 应重新采一次，实际 %d 次", probes)
	}
}
