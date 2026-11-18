package app

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"cnal/internal/netstack"
	"cnal/internal/protocol"
)

// deliverPinned 按 login_form 的 method 分派：GET 型端点参数走 query、body 空、
// 无 form Content-Type；POST/默认保持原样（body 承载 + Content-Type）。
// 这里用真实 httptest server 捕获服务端看到的方法/query/body/头，验证出口行为。

// capturedDelivery 服务端实际收到的一次登录请求快照。
type capturedDelivery struct {
	method      string
	query       url.Values
	body        string
	contentType string
}

// deliverMethodFixture 造「绑源解析失败 → auto 回退系统栈」的可注入装配，
// systemSrv 同时充当 cfg.Server（系统栈按域名拨号落到它）。
func deliverMethodFixture(t *testing.T, capture func(*capturedDelivery)) (*plumbing, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capture(&capturedDelivery{
			method:      r.Method,
			query:       r.URL.Query(),
			body:        string(b),
			contentType: r.Header.Get("Content-Type"),
		})
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	p := &plumbing{
		pt: newPortalTarget(func() (net.IP, error) {
			return nil, errors.New("DNS 应答疑似被劫持/污染")
		}, time.Minute),
		portalPort:   "443",
		netMode:      NetModeAuto,
		login:        netstack.New(net.IPv4(127, 0, 0, 1), 2*time.Second),
		systemLogin:  netstack.NewSystem(2 * time.Second),
		portal:       netstack.New(net.IPv4(127, 0, 0, 1), 2*time.Second),
		systemPortal: netstack.NewSystem(2 * time.Second),
	}
	return p, srv
}

// 行为：login_form=GET 时服务端看到 GET，参数在 query、body 空、无 Content-Type。
func TestDeliverPinnedGETMethod(t *testing.T) {
	got := make(chan capturedDelivery, 1)
	p, srv := deliverMethodFixture(t, func(c *capturedDelivery) { got <- *c })
	cfg := daemonConfig{
		Server: srv.URL, LoginPath: "/login", LoginPage: "/", UserAgent: "test-agent",
		LoginForm: protocol.Form{Method: "GET"},
	}

	resp, err := deliverPinned(cfg, p, map[string]string{"userName": "u", "pwd": "p"})
	if err != nil {
		t.Fatalf("GET 型登录应送达: %v", err)
	}
	if resp != "ok" {
		t.Fatalf("应返回门户原文，实际 %q", resp)
	}
	req := <-got
	if req.method != http.MethodGet {
		t.Errorf("应为 GET，实际 %s", req.method)
	}
	if req.query.Get("userName") != "u" || req.query.Get("pwd") != "p" {
		t.Errorf("参数应在 query，实际 %v", req.query)
	}
	if req.body != "" {
		t.Errorf("GET 时 body 应为空，实际 %q", req.body)
	}
	if req.contentType != "" {
		t.Errorf("GET 时不应带 Content-Type，实际 %q", req.contentType)
	}
}

// 行为：POST（含默认 method）保持现状——服务端看到 POST，参数在 body，
// 带 x-www-form-urlencoded（守护主线既有行为，不得回归）。
func TestDeliverPinnedPOSTMethodRegression(t *testing.T) {
	got := make(chan capturedDelivery, 1)
	p, srv := deliverMethodFixture(t, func(c *capturedDelivery) { got <- *c })
	cfg := daemonConfig{
		Server: srv.URL, LoginPath: "/login", LoginPage: "/", UserAgent: "test-agent",
		LoginForm: protocol.Form{Method: "POST"},
	}

	resp, err := deliverPinned(cfg, p, map[string]string{"userName": "u", "pwd": "p"})
	if err != nil {
		t.Fatalf("POST 型登录应送达: %v", err)
	}
	if resp != "ok" {
		t.Fatalf("应返回门户原文，实际 %q", resp)
	}
	req := <-got
	if req.method != http.MethodPost {
		t.Errorf("应为 POST，实际 %s", req.method)
	}
	if req.body != "pwd=p&userName=u" {
		t.Errorf("POST 参数应在 body，实际 %q", req.body)
	}
	if req.contentType != "application/x-www-form-urlencoded" {
		t.Errorf("POST 应带 form Content-Type，实际 %q", req.contentType)
	}
}
