package app

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cnal/internal/netstack"
)

// 选路的四个门户域名调用点必须真的走选路（设计 §4）：登录、门户可达、
// flux、v4 归属查询。漏接一个，TUN 环境下就在那一条路上继续失败。

// routeWiringFixture 造「绑源解析失败 → auto 回退系统栈」的最小装配。
// systemSrv 同时充当 cfg.Server（系统栈按域名拨号会落到它）。
func routeWiringFixture(t *testing.T, mode string, handler http.HandlerFunc) (*plumbing, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p := &plumbing{
		pt: newPortalTarget(func() (net.IP, error) {
			return nil, errors.New("DNS 应答疑似被劫持/污染")
		}, time.Minute),
		portalPort:   "443",
		netMode:      mode,
		login:        netstack.New(net.IPv4(127, 0, 0, 1), 2*time.Second),
		systemLogin:  netstack.NewSystem(2 * time.Second),
		portal:       netstack.New(net.IPv4(127, 0, 0, 1), 2*time.Second),
		systemPortal: netstack.NewSystem(2 * time.Second),
	}
	return p, srv
}

// 行为：auto + 绑源解析失败 → 登录表单经系统栈送达门户（TUN 环境唯一
// 能建立会话的路径），表单字段原样到达。
func TestDeliverPinnedFallsBackToSystem(t *testing.T) {
	got := make(chan url.Values, 1)
	p, srv := routeWiringFixture(t, NetModeAuto, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got <- r.PostForm
		_, _ = w.Write([]byte("success"))
	})
	cfg := daemonConfig{Server: srv.URL, LoginPath: "/ac_portal/login.php", LoginPage: "/", UserAgent: "test-agent"}

	resp, err := deliverPinned(cfg, p, map[string]string{"userName": "s1", "pwd": "x"})
	if err != nil {
		t.Fatalf("解析失败应回退系统栈送达: %v", err)
	}
	if resp != "success" {
		t.Errorf("应返回门户原文，实际 %q", resp)
	}
	form := <-got
	if form.Get("userName") != "s1" || form.Get("pwd") != "x" {
		t.Errorf("表单字段应原样到达，实际 %v", form)
	}
}

// 行为：net_mode=bound 手动强制解析失败 → 必须拒绝，且不许触网
// （fail-closed 的语义是「不把凭据发往猜测地址」，不是「换条路试试」）。
func TestDeliverPinnedBoundModeFailsClosed(t *testing.T) {
	var hits int32
	p, srv := routeWiringFixture(t, NetModeBound, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
	})
	cfg := daemonConfig{Server: srv.URL, LoginPath: "/login", LoginPage: "/"}

	if _, err := deliverPinned(cfg, p, map[string]string{"userName": "s1"}); err == nil ||
		!strings.Contains(err.Error(), "门户解析失败") {
		t.Fatalf("bound 模式解析失败必须拒绝，实际 %v", err)
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Errorf("fail-closed 不得发出任何请求，实际 %d 次", n)
	}
}

// 行为：绑源解析成功时仍走绑源钉 IP（优先 A 路）——请求落到 pin 指向的
// 服务，而不是 cfg.Server 的域名解析结果。
func TestDeliverPinnedUsesBoundPinWhenResolveSucceeds(t *testing.T) {
	var boundHits, systemHits int32
	boundSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&boundHits, 1)
		_, _ = w.Write([]byte("bound-ok"))
	}))
	defer boundSrv.Close()
	systemSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&systemHits, 1)
		_, _ = w.Write([]byte("system-ok"))
	}))
	defer systemSrv.Close()

	boundURL, _ := url.Parse(boundSrv.URL)
	p := &plumbing{
		pt: newPortalTarget(func() (net.IP, error) {
			return net.IPv4(127, 0, 0, 1), nil
		}, time.Minute),
		portalPort:  boundURL.Port(),
		netMode:     NetModeAuto,
		login:       netstack.New(net.IPv4(127, 0, 0, 1), 2*time.Second),
		systemLogin: netstack.NewSystem(2 * time.Second),
	}
	cfg := daemonConfig{Server: systemSrv.URL, LoginPath: "/login", LoginPage: "/"}

	resp, err := deliverPinned(cfg, p, map[string]string{"userName": "s1"})
	if err != nil {
		t.Fatalf("绑源路径应送达: %v", err)
	}
	if resp != "bound-ok" {
		t.Errorf("应命中 pin 指向的绑源服务，实际 %q", resp)
	}
	if n := atomic.LoadInt32(&systemHits); n != 0 {
		t.Errorf("绑源可用时不得走系统栈，实际系统栈请求 %d 次", n)
	}
}

func TestDeliverPinnedStrictAutoFailsClosedWithoutRequest(t *testing.T) {
	var hits int32
	p, srv := routeWiringFixture(t, NetModeAuto, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
	})
	p.strictInterfaceBind = true
	cfg := daemonConfig{Server: srv.URL, LoginPath: "/login", LoginPage: "/"}
	if _, err := deliverPinned(cfg, p, map[string]string{"userName": "s1", "pwd": "x"}); err == nil {
		t.Fatal("严格 auto 解析失败必须拒绝")
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("严格 auto 不得发出凭据请求，实际 %d 次", n)
	}
}

// 行为：flux 查询同一条选路（会话判死依赖它，走错路会误判死）。
func TestFluxQueryFallsBackToSystem(t *testing.T) {
	p, srv := routeWiringFixture(t, NetModeAuto, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ac_portal/userflux" {
			t.Errorf("flux 路径应为 /ac_portal/userflux，实际 %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`<tr><td>当天流量</td><td>2.4G</td></tr>`))
	})
	cfg := daemonConfig{Server: srv.URL, FluxPath: "/ac_portal/userflux"}

	flux, err := p.fluxQuery(cfg)
	if err != nil {
		t.Fatalf("flux 应经系统栈查询成功: %v", err)
	}
	if flux["当天流量"] != "2.4G" {
		t.Errorf("应解析出流量表，实际 %v", flux)
	}
}

// 行为：门户可达探针是登录的前置闸（keeper 在 PortalAlive=false 时直接
// portal_down，不会尝试登录）——TUN 下必须经系统栈判活，否则永远等不到重连。
func TestPortalAliveFallsBackToSystem(t *testing.T) {
	p, srv := routeWiringFixture(t, NetModeAuto, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>登录</html>"))
	})
	cfg := daemonConfig{Server: srv.URL, LoginPage: "/ac_portal/login.html"}
	d := newDaemon(cfg, p)
	if !d.PortalAlive() {
		t.Error("绑源解析失败时 PortalAlive 应经系统栈判活")
	}
}

// 行为：v4 归属查询（切账号核对）同一条登录选路。
func TestOnlineV4FallsBackToSystem(t *testing.T) {
	p, srv := routeWiringFixture(t, NetModeAuto, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"online":1,"username":"alice"}`))
	})
	cfg := daemonConfig{Server: srv.URL, LoginPath: "/ac_portal/login.php"}

	st, err := p.onlineV4(cfg)
	if err != nil {
		t.Fatalf("归属查询应经系统栈成功: %v", err)
	}
	if !st.Online || st.Username != "alice" {
		t.Errorf("应读到在线归属 alice，实际 %+v", st)
	}
}
