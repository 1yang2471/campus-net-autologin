package app

import (
	"net"
	"testing"
	"time"
)

// 物理源重发现节流（移植 Python P2-8：portal_down 时每等待周期重发现一次，
// 只在源真的变了才换绑——无意义改绑会打断正在进行的请求）。

// 行为：节流窗口内不探测；窗口外才探测一次。
func TestRediscovererThrottles(t *testing.T) {
	discovers := 0
	r := newRediscoverer(30*time.Second, net.ParseIP("10.0.0.1"),
		func() net.IP { discovers++; return net.ParseIP("10.0.0.1") },
		func(net.IP) { t.Error("源没变不得换绑") })
	base := time.Unix(0, 0)
	r.tick(base)
	if discovers != 1 {
		t.Fatalf("首拍应探测一次，实际 %d", discovers)
	}
	r.tick(base.Add(10 * time.Second))
	if discovers != 1 {
		t.Errorf("30s 窗口内不得再探测，实际 %d 次", discovers)
	}
	r.tick(base.Add(31 * time.Second))
	if discovers != 2 {
		t.Errorf("窗口过后应再探测，实际 %d 次", discovers)
	}
}

// 行为：发现新源且与当前不同 → 换绑一次，参数为新源。
func TestRediscovererAppliesChangedSource(t *testing.T) {
	var applied net.IP
	r := newRediscoverer(30*time.Second, net.ParseIP("10.0.0.1"),
		func() net.IP { return net.ParseIP("10.0.0.9") },
		func(ip net.IP) { applied = ip })
	r.tick(time.Unix(0, 0))
	if applied == nil || !applied.Equal(net.ParseIP("10.0.0.9")) {
		t.Errorf("源变化应换绑新源，实际 %v", applied)
	}
	// 换绑后当前源已更新：再探到同一新源不得重复换绑
	applied = nil
	r.tick(time.Unix(100, 0))
	if applied != nil {
		t.Errorf("源未再变不得重复换绑，实际 %v", applied)
	}
}

// 行为：发现失败（nil）→ 保持原源不动（fail-closed，不用坏源覆盖好源）。
func TestRediscovererKeepsSourceWhenDiscoverFails(t *testing.T) {
	applied := 0
	r := newRediscoverer(30*time.Second, net.ParseIP("10.0.0.1"),
		func() net.IP { return nil },
		func(net.IP) { applied++ })
	r.tick(time.Unix(0, 0))
	r.tick(time.Unix(100, 0))
	if applied != 0 {
		t.Errorf("发现失败不得换绑，实际 %d 次", applied)
	}
}
