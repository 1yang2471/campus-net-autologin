package keeper

import (
	"strings"
	"testing"
	"time"
)

// IPv6 四态展示（规格 §5.1）：S0 无 v6 完全静默 / S1 不可观测绝不塌缩成"不可达"
// （#8 误报）/ S2a 在线 / S2b 未在线（唯一允许补绑的态）。观察脱离在线分支
// 门槛（规格明写：v4 掉线期间用户不该瞥见陈旧 v6 值），60s 一节流。

func newV6Daemon(clk *time.Time, v6State string, probe *int) *Daemon {
	d := NewDaemon("")
	d.Now = func() time.Time { return *clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"用户名称": "u"}, nil }
	d.ProbeV6 = func() string { *probe++; return v6State }
	return d
}

// 行为：在线态 detail 附带 v6 文案（S2a/S2b/S1 各不相同）。
func TestV6StateShownInOnlineDetail(t *testing.T) {
	cases := map[string]string{
		"online":       "IPv6 在线",
		"offline":      "IPv6 未在线（可补绑）",
		"unobservable": "IPv6 不可观测",
	}
	for state, want := range cases {
		clk := time.Unix(0, 0)
		probe := 0
		d := newV6Daemon(&clk, state, &probe)
		d.Tick()
		if !strings.Contains(d.Detail, want) {
			t.Errorf("v6=%s 应显示 %q，实际 %q", state, want, d.Detail)
		}
	}
}

// 行为：S0（本机无 v6）完全静默——不制造"我坏了"的焦虑（规格 §5.1）。
func TestV6NoAddressIsSilent(t *testing.T) {
	clk := time.Unix(0, 0)
	probe := 0
	d := newV6Daemon(&clk, "no_address", &probe)
	d.Tick()
	if strings.Contains(d.Detail, "IPv6") {
		t.Errorf("S0 必须完全静默，实际 detail=%q", d.Detail)
	}
}

// 行为：60s 节流——每拍都握手会刷爆日志并高频撞 DPI（Python L2_PROBE_S 同理）。
func TestV6ProbeThrottled(t *testing.T) {
	clk := time.Unix(0, 0)
	probe := 0
	d := newV6Daemon(&clk, "online", &probe)
	d.Tick()
	if probe != 1 {
		t.Fatalf("首拍应观察一次，实际 %d", probe)
	}
	clk = clk.Add(10 * time.Second)
	d.Tick()
	if probe != 1 {
		t.Errorf("60s 窗口内不得再观察，实际 %d 次", probe)
	}
	clk = clk.Add(51 * time.Second)
	d.Tick()
	if probe != 2 {
		t.Errorf("窗口过后应再观察，实际 %d 次", probe)
	}
}

// 行为：观察脱离在线分支门槛——锚点离线（v4 掉线）期间 v6 值仍须更新，
// 不得让用户瞥见陈旧值（规格 §5.1 末段）。
func TestV6ProbedEvenWhenV4Offline(t *testing.T) {
	clk := time.Unix(0, 0)
	probe := 0
	d := newV6Daemon(&clk, "online", &probe)
	d.ProbeOnline = func() bool { return false } // v4 掉线
	d.Tick()
	if probe != 1 {
		t.Errorf("v4 掉线期间仍须观察 v6，实际 %d 次", probe)
	}
	if d.V6State != "online" {
		t.Errorf("v6 状态应更新为 online，实际 %q", d.V6State)
	}
}
