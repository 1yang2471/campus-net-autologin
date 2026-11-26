package app

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"cnal/internal/netstack"
)

// 双路径自适应（设计 §3/§4）：
//   - 两条都健康时绑源优先（读数=校园网真实态，凭据不经代理）；
//   - auto 下绑源解析失败才转系统栈（与浏览器同路）；
//   - bound 手动强制：解析失败必须 fail-closed，绝不改走系统栈；
//   - system 手动强制：不碰绑源解析（省一次必然失败的 DNS）。

// routeFixture 造一套最小 plumbing：解析缝可注入，绑源/系统栈客户端可辨识。
func routeFixture(mode string, resolve func() (net.IP, error)) (*plumbing, *netstack.Client, *netstack.Client) {
	bound := netstack.New(net.IPv4(127, 0, 0, 1), time.Second)
	system := netstack.NewSystem(time.Second)
	p := &plumbing{
		pt:           newPortalTarget(resolve, 60*time.Second),
		portalPort:   "443",
		netMode:      mode,
		bindMode:     netstack.BindModeSourceOnly,
		portal:       bound,
		systemPortal: system,
		login:        bound,
		systemLogin:  system,
	}
	return p, bound, system
}

// 行为：auto 且绑源解析成功 → 绑源钉 IP（优先 A 路）。
func TestRouteAutoPrefersBoundWhenResolveSucceeds(t *testing.T) {
	p, bound, _ := routeFixture(NetModeAuto, func() (net.IP, error) {
		return net.IPv4(10, 129, 0, 9), nil
	})
	r := p.routePortal()
	if r.client != bound {
		t.Errorf("两条都健康时应走绑源客户端")
	}
	if r.path != routeBound {
		t.Errorf("路径应为 %s，实际 %s", routeBound, r.path)
	}
	if r.pin != "10.129.0.9:443" {
		t.Errorf("应钉解析出的真实 IP:443，实际 %q", r.pin)
	}
}

// 行为：auto 且绑源解析失败（TUN 劫持/污染）→ 系统栈，pin 必须为空
// （fake-ip 只在隧道内有效，带出去必失败），原因要能写进日志。
func TestRouteAutoFallsBackToSystemOnResolveFailure(t *testing.T) {
	p, _, system := routeFixture(NetModeAuto, func() (net.IP, error) {
		return nil, errors.New("DNS 应答疑似被劫持/污染")
	})
	r := p.routePortal()
	if r.client != system {
		t.Errorf("绑源解析失败应转系统栈客户端")
	}
	if r.path != routeSystem {
		t.Errorf("路径应为 %s，实际 %s", routeSystem, r.path)
	}
	if r.pin != "" {
		t.Errorf("系统栈不得钉 IP，实际 %q", r.pin)
	}
	if !strings.Contains(r.why, "劫持") {
		t.Errorf("路径日志应带出解析失败原因，实际 %q", r.why)
	}
}

// 行为：net_mode=bound 是用户显式强制——解析失败必须 fail-closed，
// 不得自动改走系统栈（逃生口的语义是「只走这条路」）。
func TestRouteBoundFailsClosedOnResolveFailure(t *testing.T) {
	p, _, _ := routeFixture(NetModeBound, func() (net.IP, error) {
		return nil, errors.New("DNS 不通")
	})
	r := p.routeLogin()
	if r.client != nil {
		t.Errorf("bound 模式解析失败必须拒绝操作（client=nil），实际 %v", r.client)
	}
	if r.pin != "" {
		t.Errorf("fail-closed 时不得带 pin，实际 %q", r.pin)
	}
}

// 行为：net_mode=system 强制系统栈，且完全不尝试绑源解析
// （TUN 下那次查询必然失败/被劫持，纯属白等）。
func TestRouteSystemSkipsBoundResolution(t *testing.T) {
	calls := 0
	p, _, system := routeFixture(NetModeSystem, func() (net.IP, error) {
		calls++
		return net.IPv4(10, 129, 0, 9), nil
	})
	r := p.routePortal()
	if r.client != system {
		t.Errorf("system 模式应走系统栈客户端")
	}
	if calls != 0 {
		t.Errorf("system 模式不应触发绑源解析，实际调用 %d 次", calls)
	}
}

// 行为：路径日志只在路径变化时写一行（PortalAlive 每拍都选路，逐拍记会刷屏）。
func TestRouteLogsOnlyWhenPathChanges(t *testing.T) {
	fail := true
	var lines []string
	p, _, _ := routeFixture(NetModeAuto, func() (net.IP, error) {
		if fail {
			return nil, errors.New("DNS 不通")
		}
		return net.IPv4(10, 129, 0, 9), nil
	})
	p.logf = func(format string, args ...any) {
		lines = append(lines, fmt.Sprintf(format, args...))
	}

	p.routePortal()
	p.routePortal() // 同一路径，不重复记
	if len(lines) != 1 {
		t.Fatalf("路径未变应只记一次，实际 %d 行: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "系统栈") || !strings.Contains(lines[0], "DNS 不通") {
		t.Errorf("日志应说明走了哪条路、为什么，实际 %q", lines[0])
	}

	fail = false
	p.routePortal() // 解析恢复 → 绑源路径，记第二行
	if len(lines) != 2 {
		t.Fatalf("路径变化应再记一行，实际 %d 行: %v", len(lines), lines)
	}
	if !strings.Contains(lines[1], "绑源") {
		t.Errorf("恢复后日志应说明改走绑源，实际 %q", lines[1])
	}
}

// 行为：面板「门户路径」读数 = 路径名 + 原因（用户要能一眼看出走的是哪条路、
// 为什么；2026-11-11 设计 §4）。
func TestRouteTextExposesPathAndReason(t *testing.T) {
	p, _, _ := routeFixture(NetModeAuto, func() (net.IP, error) {
		return net.IPv4(10, 129, 0, 9), nil
	})
	if got := p.routeText(); got != "" {
		t.Errorf("还没选路时路径读数应为空，实际 %q", got)
	}
	p.routePortal()
	got := p.routeText()
	if !strings.Contains(got, "绑源") {
		t.Errorf("路径读数应说明走绑源，实际 %q", got)
	}
	if !strings.Contains(got, "10.129.0.9:443") {
		t.Errorf("路径读数应带钉住的地址，实际 %q", got)
	}
}

func TestRouteTextIncludesBindingMode(t *testing.T) {
	p, _, _ := routeFixture(NetModeAuto, func() (net.IP, error) {
		return net.IPv4(10, 129, 0, 9), nil
	})
	if !p.portal.Bindable(nil) {
		t.Fatal("测试源应可绑定")
	}
	p.routePortal()
	got := p.routeText()
	if !strings.Contains(got, "接口绑定") && !strings.Contains(got, "仅绑源") {
		t.Fatalf("路径文本应暴露绑定模式，实际 %q", got)
	}
}

// 行为：路径标签不得声称「跟 TUN」——TUN 是否接管要靠适配器描述才说得准，
// 2026-11-11 实测那台机器上 TUN 根本没开，凭空写就是把猜测当事实说。
func TestRouteLabelDoesNotClaimTUN(t *testing.T) {
	for path, label := range routeLabels {
		if strings.Contains(label, "TUN") {
			t.Errorf("%s 的标签 %q 不该声称跟 TUN", path, label)
		}
	}
}
