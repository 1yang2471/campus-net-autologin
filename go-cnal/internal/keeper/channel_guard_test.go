package keeper

import (
	"testing"
	"time"
)

// 多通道观察（2026-11-20 双通道方案 §8.3 M1）：内核侧只做**观察**，零凭据。
//
// 为什么观察也要放到内核：判定结构（节流、异步单飞、快照取读数）与单通道
// 时是同一套纪律，写在壳层就会出现两份「60s 一探」的规矩漂移。
//
// 异步单飞是硬要求（2026-11-19 回归教训）：通道探测要走 DNS + HTTP，未认证
// VLAN 上两项都可能吃满超时——同步跑在 Tick 里会把 3 秒心跳拖成十几秒一拍。

// newGuardDaemon 造一个带通道观察器的守护（其余缝不接线：只测观察路径）。
// views 走指针：测试要能在运行中改枚举结果（拔线），按值传会拿到副本——
// 那样断言看着在测「通道消失」，其实枚举结果从没变过（假装测试）。
func newGuardDaemon(clk *time.Time, probes *int, views *[]ChannelView) *Daemon {
	d := NewDaemon("")
	d.Now = func() time.Time { return *clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"用户名称": "u"}, nil }
	g := NewChannelGuard()
	g.Now = func() time.Time { return *clk }
	g.Enumerate = func() []ChannelView { return *views }
	g.Probe = func(v ChannelView) string {
		*probes++
		return "unauth"
	}
	d.Channels = g
	return d
}

// waitViews 等异步观察落地（探测在独立 goroutine 里跑）。
func waitViews(t *testing.T, d *Daemon, want int) []ChannelView {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := d.ChannelViews(); len(got) == want {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	return d.ChannelViews()
}

// waitProbes 等异步探测落地到指定次数（探测在独立 goroutine 跑，计数在
// goroutine 内自增——断言前必须等它，否则测的是「拍已发出」不是「已探完」）。
func waitProbes(t *testing.T, probes *int, want int) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if *probes >= want {
			return *probes
		}
		time.Sleep(10 * time.Millisecond)
	}
	return *probes
}

// 行为：通道观察结果可从内核读到；60s 内不重复探测；探测在独立 goroutine 跑。
func TestChannelGuardObservesAndThrottles(t *testing.T) {
	clk := time.Unix(1000, 0)
	probes := 0
	views := []ChannelView{
		{ID: "以太网|10.141.53.34", Iface: "以太网", IP: "10.141.53.34"},
	}
	d := newGuardDaemon(&clk, &probes, &views)

	d.Tick()
	got := waitViews(t, d, 1)
	if len(got) != 1 || got[0].State != "unauth" || got[0].Iface != "以太网" {
		t.Fatalf("应读到以太网通道的未认证态，实际 %+v", got)
	}
	if probes != 1 {
		t.Fatalf("首拍应探测一次，实际 %d", probes)
	}

	// 59s 内第二拍：节流窗口内不得重复探测。
	clk = clk.Add(59 * time.Second)
	d.Tick()
	if probes != 1 {
		t.Fatalf("节流窗口内不得重复探测，实际 %d 次", probes)
	}

	// 到 60s：重新探测，读数刷新。
	clk = clk.Add(time.Second)
	d.Tick()
	if got := waitProbes(t, &probes, 2); got != 2 {
		t.Fatalf("节流窗口到点应重新探测，实际 %d 次", got)
	}
}

// 行为：未接线观察器时不得上报任何通道（不编造），也不影响主链路状态。
func TestChannelGuardUnwired(t *testing.T) {
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"用户名称": "u"}, nil }
	d.Tick()
	if got := d.ChannelViews(); len(got) != 0 {
		t.Fatalf("未接线不得上报，实际 %+v", got)
	}
	if d.State != StateOnline {
		t.Fatalf("主链路应正常在线，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：通道消失（拔线）后，快照不得保留陈旧读数——旧读数会被面板读成
// 「这张卡还通着」。
func TestChannelGuardDropsVanishedChannel(t *testing.T) {
	clk := time.Unix(1000, 0)
	probes := 0
	views := []ChannelView{{ID: "以太网|10.141.53.34", Iface: "以太网", IP: "10.141.53.34"}}
	d := newGuardDaemon(&clk, &probes, &views)

	d.Tick()
	waitViews(t, d, 1)

	// 拔线：枚举不再返回该通道。
	views = nil
	clk = clk.Add(61 * time.Second)
	d.Tick()
	if got := waitViews(t, d, 0); len(got) != 0 {
		t.Fatalf("通道消失后不得保留旧读数，实际 %+v", got)
	}
}
