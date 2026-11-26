package app

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"cnal/internal/netstack"
)

// 2026-11-23 过夜轮缺陷（A2/A3 实测）：MIUI Doze 期 wlan0 的 IP 从内核网卡
// 摘掉，但 portalTarget 的 TTL 缓存里门户 IP（DNS 结果）还在 → chooseRoute
// 判「绑源解析成功」走 bound → dialer 绑源 bind 失败（cannot assign requested
// address）→ 5 小时 13 拍全部卡死，rd 重发现永远不触发（只在解析失败时触发）。
//
// 要求：绑源解析成功但**源 IP 已不可绑定**时，等同源失效处理——清门户 IP
// TTL 缓存 + 触发 rd.tick + 转系统栈，不再粘滞。

// 行为：源不可绑定时转系统栈（决议：bind 自检失败 = 绑源路径不可用）。
func TestRouteUnbindableSourceFallsBackToSystem(t *testing.T) {
	p, _, _ := routeFixture(NetModeAuto, func() (net.IP, error) {
		return net.IPv4(10, 129, 0, 9), nil // DNS 成功（TTL 缓存命中）
	})
	p.bindable = func(net.IP) bool { return false } // 但源 IP 已从网卡摘掉
	r := p.routePortal()
	if r.path != routeSystem {
		t.Errorf("源不可绑定应转系统栈，实际 path=%s why=%s", r.path, r.why)
	}
}

// 行为：源不可绑定时清门户 IP TTL 缓存（下次 lookup 重新解析，不被旧 DNS
// 结果继续粘住）。
func TestRouteUnbindableSourceInvalidatesPortalTarget(t *testing.T) {
	resolveCalls := 0
	p, _, _ := routeFixture(NetModeAuto, func() (net.IP, error) {
		resolveCalls++
		return net.IPv4(10, 129, 0, 9), nil
	})
	p.bindable = func(net.IP) bool { return false }
	p.routePortal()
	p.routePortal() // 第二次 lookup 应因缓存被清而重解析
	if resolveCalls != 2 {
		t.Errorf("源不可绑定应清 TTL 缓存（每次重新解析），实际 resolve 调用 %d 次", resolveCalls)
	}
}

// 行为：源不可绑定时触发 rd.tick（重发现物理源，而不是原地卡死）。
func TestRouteUnbindableSourceTriggersRdTick(t *testing.T) {
	p, _, _ := routeFixture(NetModeAuto, func() (net.IP, error) {
		return net.IPv4(10, 129, 0, 9), nil
	})
	p.bindable = func(net.IP) bool { return false }
	ticks := 0
	p.rd = newRediscoverer(time.Hour, net.IPv4(10, 141, 53, 34),
		func() net.IP { ticks++; return nil }, func(net.IP) {})
	p.routePortal()
	if ticks != 1 {
		t.Errorf("源不可绑定应触发 rd.tick，实际 %d 次", ticks)
	}
}

// 行为：正常可绑定源仍走绑源路径、不误触发 rd、不清缓存。
func TestRouteBindableSourceStaysOnBound(t *testing.T) {
	resolveCalls := 0
	p, _, _ := routeFixture(NetModeAuto, func() (net.IP, error) {
		resolveCalls++
		return net.IPv4(10, 129, 0, 9), nil
	})
	p.bindable = func(net.IP) bool { return true }
	ticks := 0
	p.rd = newRediscoverer(time.Hour, net.IPv4(10, 141, 53, 34),
		func() net.IP { ticks++; return nil }, func(net.IP) {})
	r := p.routePortal()
	if r.path != routeBound {
		t.Errorf("正常源应走绑源，实际 path=%s", r.path)
	}
	p.routePortal()
	if ticks != 0 || resolveCalls != 1 {
		t.Errorf("正常源不得触发 rd/清缓存，实际 ticks=%d resolve=%d", ticks, resolveCalls)
	}
}

// 行为：net_mode=bound（fail-closed 模式）下源不可绑定 → 拒绝发包，绝不
// 悄悄转系统栈把凭据发往默认路由（TUN 下会发往代理出口——安全红线）。
func TestRouteBoundModeUnbindableFailsClosed(t *testing.T) {
	p, _, _ := routeFixture(NetModeBound, func() (net.IP, error) {
		return net.IPv4(10, 129, 0, 9), nil
	})
	p.bindable = func(net.IP) bool { return false }
	r := p.routePortal()
	if r.path != routeBound || r.client != nil {
		t.Errorf("bound 模式源失效应 fail-closed（path=bound, client=nil），实际 path=%s client=%v", r.path, r.client)
	}
}

// 行为：bindable 自检缝未注入时（测试/旧调用）保持旧语义——不阻止选路。
func TestRouteNoBindableHookKeepsLegacyBehavior(t *testing.T) {
	p, _, _ := routeFixture(NetModeAuto, func() (net.IP, error) {
		return net.IPv4(10, 129, 0, 9), nil
	})
	r := p.routePortal() // 不设 bindable，模拟未装配缝
	if r.path != routeBound {
		t.Errorf("未注入 bindable 缝时应保持绑源（旧语义），实际 %s", r.path)
	}
}

// 行为：解析失败路径（原有 rd 触发）不受 bindable 影响——errors.New 分支
// 不调 bindable（源都解析不出来，无从自检）。
func TestRouteResolveFailureSkipsBindable(t *testing.T) {
	p, _, _ := routeFixture(NetModeAuto, func() (net.IP, error) {
		return nil, errors.New("DNS 不通")
	})
	called := false
	p.bindable = func(net.IP) bool { called = true; return true }
	r := p.routePortal()
	if called {
		t.Error("解析失败路径不得调用 bindable 自检")
	}
	if r.path != routeSystem {
		t.Errorf("解析失败仍应转系统栈，实际 %s", r.path)
	}
}

// 行为：绑定失效的 why 文案不得误称「解析失败」（排障方向不同：查网卡 vs
// 查 DNS；审查 P1）。
func TestRouteUnbindableWhySaysSourceNotDNS(t *testing.T) {
	p, _, _ := routeFixture(NetModeAuto, func() (net.IP, error) {
		return net.IPv4(10, 129, 0, 9), nil
	})
	p.bindable = func(net.IP) bool { return false }
	r := p.routePortal()
	if !strings.Contains(r.why, "绑源不可用") || strings.Contains(r.why, "解析失败") {
		t.Errorf("绑定失效文案应说『绑源不可用』而非『解析失败』，实际 %q", r.why)
	}
}

func TestRouteAutoStrictRefusesSystemFallback(t *testing.T) {
	p, _, _ := routeFixture(NetModeAuto, func() (net.IP, error) {
		return nil, errors.New("DNS 不通")
	})
	p.strictInterfaceBind = true
	r := p.routeLogin()
	if r.client != nil || r.path != routeBound {
		t.Fatalf("严格 auto 必须 fail-closed，实际 client=%v path=%s why=%s", r.client, r.path, r.why)
	}
	if !strings.Contains(r.why, "严格接口绑定") {
		t.Fatalf("原因应明确严格接口绑定，实际 %q", r.why)
	}
}

// 行为：生产装配必须把真实 Bindable 接到缝上（守接线——上面 7 个测试全用
// 替身，删掉 newPlumbing 里的 bindable 赋值它们照样绿）。用文档保留地址
// 203.0.113.x 验证「真实自检判失效」，确认缝指向真实现而非空壳。
func TestPlumbingWiresRealBindable(t *testing.T) {
	old := portalProbeWithBindMode
	t.Cleanup(func() { portalProbeWithBindMode = old })
	portalProbeWithBindMode = func(net.IP, string, string, string, time.Duration, bool, netstack.BindMode) error { return nil }
	cfg := defaultAdvancedConfig()
	cfg.Server = "https://portal.example"
	cfg.BindIP = "203.0.113.99" // 固定源（不可达，但自检只判本机绑定能力）
	pl, err := newPlumbing(cfg, "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	if pl.bindable == nil {
		t.Fatal("生产装配必须注入 bindable（防误删回归）")
	}
	if pl.bindable(net.ParseIP("203.0.113.99")) {
		t.Error("真实 Bindable 应判文档保留地址不可绑定（确认缝指向真实现）")
	}
}
