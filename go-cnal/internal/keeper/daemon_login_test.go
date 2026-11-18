package keeper

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// G4：重连登录集成（移植 Python tick 重连分支 + t45 三语义）。
// 触发：锚点红+门户活+窗外 → 重连登录；成功点亮在线并清 flux 判死
// （Python「重连成功即解除 flux 判死」）；失败如实显示离线+原因。
// 会话判死同样落到重连（锚点绿+死会话的假在线封堵后路）。

// 行为：离线拍 + 登录缝接线 → 连续 3 拍失败（去抖）后登录一次，成功点亮在线。
func TestTickOfflineTriggersLogin(t *testing.T) {
	calls := 0
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.Login = func() (string, error) { calls++; return "", nil }
	for i := 0; i < 3; i++ { // L1 去抖：连续 3 拍才进入重连
		d.Tick()
	}
	if calls != 1 {
		t.Fatalf("连续离线拍应触发重连登录一次，实际 %d 次", calls)
	}
	if d.State != StateOnline {
		t.Errorf("重连成功应点亮在线，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：登录失败（门户回绝）→ 显示「登录被拒」态 + 原因（Python handle_rejection
// set_state("rejected", detail) 同源；「离线」留给网络不可达，两者语义不同）。
func TestTickLoginFailShowsRejectedDetail(t *testing.T) {
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.Login = func() (string, error) { return "", errors.New("登录未受理: NOAUTH") }
	for i := 0; i < 3; i++ { // L1 去抖
		d.Tick()
	}
	if d.State != StateRejected || !strings.Contains(d.Detail, "NOAUTH") {
		t.Errorf("登录被拒应显示 rejected+原因，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：flux 判死后（防抖期已过）→ 触发重连；防抖期不得触发。
func TestSessionDeadTriggersReconnectLogin(t *testing.T) {
	calls := 0
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{}, nil }
	d.Login = func() (string, error) { calls++; return "", nil }
	d.Tick()                        // miss=1 防抖
	if calls != 0 {
		t.Fatal("防抖期（未判死）不得触发重连登录")
	}
	clk = clk.Add(61 * time.Second) // 越过 60s 会话探针节流 → miss=2 判死
	d.Tick()
	if calls != 1 {
		t.Fatalf("判死拍应触发重连登录一次，实际 %d 次", calls)
	}
	if d.State != StateOnline {
		t.Errorf("重连成功应点亮在线，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：重连成功 → 清 flux 判死（Python：重连成功即解除判死，窗口边缘
// 免会话部署不再被旧判死反复拖回重连）。
func TestReconnectSuccessClearsSessionDead(t *testing.T) {
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{}, nil }
	d.Tick()                        // miss=1
	clk = clk.Add(61 * time.Second) // 越过节流 → miss=2 判死
	d.Tick()
	if !d.SessionDead {
		t.Fatal("前置：应已判死")
	}
	d.Login = func() (string, error) { return "", nil }
	d.Tick()
	if d.SessionDead {
		t.Error("重连成功必须清 flux 判死")
	}
	if d.State != StateOnline {
		t.Errorf("重连成功应在线，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：免认证时段内永不自动登录（t38 第二课：窗口内登录必被门户回
// NOAUTH 并招致反暴力冻结）——Login 缝已接线也不得触发。
func TestInWindowNeverLogsIn(t *testing.T) {
	calls := 0
	d := NewDaemon("00:00-24:00")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.Login = func() (string, error) { calls++; return "", nil }
	d.Tick()
	if calls != 0 {
		t.Fatal("窗口内禁止自动登录")
	}
	if d.State != StateFreeWindow {
		t.Errorf("窗口内应显示免认证态，实际 %v/%v", d.State, d.Detail)
	}
}
