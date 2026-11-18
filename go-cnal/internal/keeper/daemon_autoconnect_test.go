package keeper

import (
	"errors"
	"testing"
)

// auto_reconnect 开关（Python tick 同序：portal_down 之后、免认证/自动登录之前）：
// 关 = 只探测报状态，不自动登录。手动重连走独立路径不受限（app 层测）。

// 行为：关 + 真掉线（窗口外）→ paused，且不触发登录缝。
func TestTickAutoReconnectOffShowsPausedNoLogin(t *testing.T) {
	d := newTestDaemon("", false, true) // 锚点红、门户活（窗口空=全天外？空串=无窗口）
	d.SetAutoReconnect(false)
	var loginCalls int
	d.Login = func() (string, error) { loginCalls++; return "", errors.New("NOAUTH") }
	d.Tick()
	if d.State != StatePaused {
		t.Errorf("关自动重连+掉线应 paused，实际 %v/%v", d.State, d.Detail)
	}
	if loginCalls != 0 {
		t.Errorf("关自动重连不得触发登录，实际 %d 次", loginCalls)
	}
}

// 行为：关 + 实际在线（锚点绿、会话活）→ 照常在线（开关只拦自动登录，
// 不拦真值上报）。
func TestTickAutoReconnectOffStillShowsOnline(t *testing.T) {
	d := newTestDaemon("", true, true)
	d.SetAutoReconnect(false)
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"a": "1"}, nil }
	d.Tick()
	if d.State != StateOnline {
		t.Errorf("关自动重连但在线仍应报在线，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：关 + 门户不可达 → portal_down 如实显示（闸门不掩盖门户故障）。
func TestTickAutoReconnectOffPortalDownStillReports(t *testing.T) {
	d := newTestDaemon("", false, false)
	d.SetAutoReconnect(false)
	d.Tick()
	if d.State != StatePortalDown {
		t.Errorf("关自动重连时门户挂应如实 portal_down，实际 %v", d.State)
	}
}

// 行为：开（默认）→ 关到线走登录（回归：开关默认 true 不改老行为）。
func TestTickAutoReconnectOnDefaults(t *testing.T) {
	d := newTestDaemon("", false, true)
	if !d.AutoReconnectOn() {
		t.Fatal("AutoReconnect 默认必须为 true（Python DEFAULTS auto_reconnect=true）")
	}
	var called int
	d.Login = func() (string, error) { called++; return "", errors.New("NOAUTH") }
	for i := 0; i < 3; i++ { // L1 去抖：连续 3 拍才触发登录
		d.Tick()
	}
	if called != 1 {
		t.Errorf("默认开时连续掉线应触发自动登录一次，实际 %d", called)
	}
}
