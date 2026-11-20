package keeper

import (
	"testing"
	"time"
)

// 免认证窗口结束的边沿（2026-11-20 排查实证）：窗口内门户不维护会话，
// L1 锚点又可能间歇失败；结束后首拍如果会话探针还在 60s 节流里，
// 守护会**误报在线最多 60 秒**（实际没网），而 Windows 的 NCSI 可能
// 正好在这段"未认证窗口"抓到门户 302，把网络标记成"需要登录"→
// Edge 反复自动打开登录页（用户感知：莫名其妙弹认证页）。
//
// 行为：窗口结束后的第一拍必须立即核实会话（作废 60s 节流），
// 会话死就同拍登录。

// newFreeWindowEdgeDaemon 造窗口边沿场景：
//   - FreeWindows = 23:30-06:30（LocalMinute 可控）
//   - SessionAlive 返回值可变（窗口内先答活，结束后答死）
func newFreeWindowEdgeDaemon(clk *time.Time, minute *int, alive *bool, sessCalls, logins *int) *Daemon {
	d := NewDaemon("23:30-06:30")
	d.Now = func() time.Time { return *clk }
	d.LocalMinute = func() int { return *minute }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"用户名称": "u"}, nil }
	d.SessionAlive = func() (bool, error) {
		*sessCalls++
		return *alive, nil
	}
	d.Login = func() (string, error) {
		*logins++
		return "logon success", nil
	}
	return d
}

// 行为：窗口内探针刚跑过（60s 节流未到），窗口结束首拍仍必须立即核实并登录。
func TestFreeWindowEndRechecksSessionImmediately(t *testing.T) {
	clk := time.Date(2026, 11, 20, 6, 30, 40, 0, time.Local)
	minute := 390 // 06:30 窗口内
	alive := true
	var sessCalls, logins int
	d := newFreeWindowEdgeDaemon(&clk, &minute, &alive, &sessCalls, &logins)

	// 窗口内一拍：L1 绿、会话活 → 在线；探针跑一次，节流起点 = 06:30:40。
	d.Tick()
	if sessCalls != 1 {
		t.Fatalf("窗口内首拍应核实一次，实际 %d", sessCalls)
	}
	if logins != 0 {
		t.Fatalf("窗口内不得登录，实际 %d", logins)
	}

	// 窗口结束（06:31:01，距上次探针 21s < 60s 节流），会话已死。
	minute = 391 // 06:31 窗口外
	clk = time.Date(2026, 11, 20, 6, 31, 1, 0, time.Local)
	alive = false
	d.Tick()

	if sessCalls != 2 {
		t.Fatalf("窗口结束首拍必须立即核实会话（不等 60s 节流），实际探测 %d 次", sessCalls)
	}
	if logins != 1 {
		t.Fatalf("会话已死应在同拍登录，实际登录 %d 次", logins)
	}
}

// 行为：窗口结束首拍会话活着 → 不登录（只核实，不误伤）。
func TestFreeWindowEndAliveDoesNotLogin(t *testing.T) {
	clk := time.Date(2026, 11, 20, 6, 30, 40, 0, time.Local)
	minute := 390
	alive := true
	var sessCalls, logins int
	d := newFreeWindowEdgeDaemon(&clk, &minute, &alive, &sessCalls, &logins)

	d.Tick() // 窗口内：在线，sessAt=06:30:40
	minute = 391
	clk = time.Date(2026, 11, 20, 6, 31, 1, 0, time.Local)
	d.Tick() // 窗口外：立即核实，会话活 → 在线

	if sessCalls != 2 {
		t.Fatalf("窗口结束首拍应核实会话，实际 %d", sessCalls)
	}
	if logins != 0 {
		t.Fatalf("会话活着不得登录，实际 %d", logins)
	}
}
