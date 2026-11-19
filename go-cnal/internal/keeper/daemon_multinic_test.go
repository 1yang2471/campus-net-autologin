package keeper

import (
	"testing"
	"time"
)

// 多网卡观察（2026-11-19）：keeper 只维护一个绑定源；另一张网卡「已连接但
// 未认证」（插网线进未认证 VLAN）此前完全不可见——这正是 Windows 黄叹号
// 误报的用户困惑来源。行为判据：观察结果可从面板读数拿到、60s 节流、
// 网络恢复后清空；只观察，不改变任何登录行为。

func newSiblingDaemon(clk *time.Time, probe *int, cur *[]string) *Daemon {
	d := NewDaemon("")
	d.Now = func() time.Time { return *clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"用户名称": "u"}, nil }
	d.ProbeSiblingAuth = func() []string {
		*probe++
		return *cur
	}
	return d
}

// waitSiblings 等异步探测落地（探测在独立 goroutine 跑，见 probeSiblings）。
func waitSiblings(t *testing.T, d *Daemon, want int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := d.UnauthSiblings(); len(got) == want {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	return d.UnauthSiblings()
}

// 行为：观察到的未认证源可从 UnauthSiblings 读到；60s 内不重复探测；
// 下一窗口探测到空（源已认证）即清空面板读数。
func TestSiblingAuthObservation(t *testing.T) {
	clk := time.Unix(1000, 0)
	probe := 0
	cur := []string{"10.141.53.34"}
	d := newSiblingDaemon(&clk, &probe, &cur)

	d.Tick()
	got := waitSiblings(t, d, 1)
	if len(got) != 1 || got[0] != "10.141.53.34" {
		t.Fatalf("应读到未认证源 10.141.53.34，实际 %v", got)
	}
	if probe != 1 {
		t.Fatalf("首拍应探测一次，实际 %d", probe)
	}

	// 59s 内第二拍：节流内不得再探，读数保持。
	clk = clk.Add(59 * time.Second)
	d.Tick()
	if probe != 1 {
		t.Fatalf("节流窗口内不得重复探测，实际 %d 次", probe)
	}
	if len(d.UnauthSiblings()) != 1 {
		t.Fatalf("节流期间读数应保持，实际 %v", d.UnauthSiblings())
	}

	// 到 60s：重新探测；源已认证（空列表）→ 读数清空。
	clk = clk.Add(time.Second)
	cur = nil
	d.Tick()
	if got := waitSiblings(t, d, 0); len(got) != 0 {
		t.Fatalf("源认证后应清空读数，实际 %v", got)
	}
	if probe != 2 {
		t.Fatalf("节流窗口到点应重新探测，实际 %d 次", probe)
	}
}

// 行为：未接线时不上报任何源（不编造），也不影响主链路。
func TestSiblingAuthUnwired(t *testing.T) {
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"用户名称": "u"}, nil }
	d.Tick()
	if got := d.UnauthSiblings(); len(got) != 0 {
		t.Fatalf("未接线不得上报，实际 %v", got)
	}
	if d.State != StateOnline {
		t.Fatalf("主链路应正常在线，实际 %v/%v", d.State, d.Detail)
	}
}
