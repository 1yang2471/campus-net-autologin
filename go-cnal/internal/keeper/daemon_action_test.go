package keeper

import (
	"testing"
	"time"
)

// 守护动作时间线（2026-11-21 产品优化决议 C）：面板显示最近一次守护动作
//（「3 分钟前·自动登录」）——断网排查时用户第一眼知道守护动没动过，
// 不用翻日志反推。动作集合：自动登录 / 通道登录(接口名) / 切换账号（app 层）。

// 行为：主链路登录成功（门户受理）记「自动登录」，时刻取守护时钟（假时钟
// 可精确断言）；哨兵分流分支（判死后重登答「已在线」）不是守护修好的，
// 不记。
func TestLoginSuccessRecordsAction(t *testing.T) {
	clk := time.Unix(1000, 0)
	d := NewDaemon("")
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.Login = func() (string, error) { return "logon success", nil }

	d.Tick() // 去抖 1
	d.Tick() // 去抖 2
	d.Tick() // 去抖到点 → 登录成功
	kind, at := d.LastAction()
	if kind != "自动登录" {
		t.Fatalf("登录成功应记「自动登录」，实际 %q", kind)
	}
	if !at.Equal(clk) {
		t.Fatalf("动作时刻应取守护时钟，实际 %v", at)
	}
}

// 行为：通道登录成功记「通道登录(接口名)」——面板能看出是哪张卡自愈的。
func TestChannelLoginRecordsAction(t *testing.T) {
	clk := time.Unix(1000, 0)
	var log []string
	views := []ChannelView{{ID: "WLAN 2|172.29.153.208", Iface: "WLAN 2", IP: "172.29.153.208"}}
	d := newChannelLoginDaemon(&clk, &views, &log)

	// 先拍触发异步观察，等读数落地，再拍触发登录决策。
	d.Tick()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(d.ChannelViews()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	d.Tick()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(log) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(log) != 1 {
		t.Fatalf("前置：应完成一次通道登录，实际 %v", log)
	}
	kind, _ := d.LastAction()
	if kind != "通道登录(WLAN 2)" {
		t.Fatalf("通道登录应记「通道登录(接口名)」，实际 %q", kind)
	}
}
