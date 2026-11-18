package app

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cnal/internal/keeper"
	"cnal/internal/netstack"
	"cnal/internal/protocol"
)

// 生产发包缝的验收：ConfirmOptions.Send 必须接在守护唯一登录出口
// deliverPinned 上（routeLogin + GetPinned/PostPinned，绑源钉 IP/Host 纪律不变）。
// 这里用本地 httptest 捕获服务端实际看到的方法/query/body/头——与
// deliver_method_test.go 同款夹具，但断言的是 confirmSend 这条新缝。
//
// 关键判据：分派由 Send 的 method 参数驱动，而不是只改 cfg 不生效。
// 于是两例都故意把 cfg.LoginForm.Method 设成与调用参数相反的值。

func confirmSendFixture(t *testing.T, reply string, capture func(*capturedDelivery)) (*plumbing, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capture(&capturedDelivery{
			method:      r.Method,
			query:       r.URL.Query(),
			body:        string(b),
			contentType: r.Header.Get("Content-Type"),
		})
		_, _ = w.Write([]byte(reply))
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

// (a) method=GET：服务端看到 GET，参数走 query、body 空、无 Content-Type。
// cfg 里写的是 POST，证明是 method 参数在驱动而非 cfg 的残留值。
func TestConfirmSendDispatchesGET(t *testing.T) {
	got := make(chan capturedDelivery, 1)
	p, srv := confirmSendFixture(t, "ok", func(c *capturedDelivery) { got <- *c })
	cfg := daemonConfig{
		Server: srv.URL, LoginPath: "/login", LoginPage: "/", UserAgent: "test-agent",
		LoginForm: protocol.Form{Method: "POST"},
	}

	body, err := confirmSend(cfg, p)("GET", cfg.Server+cfg.LoginPath,
		map[string]string{"userName": "u", "pwd": "p"})
	if err != nil {
		t.Fatalf("GET 型行为确认应送达: %v", err)
	}
	if body != "ok" {
		t.Fatalf("应返回门户原文，实际 %q", body)
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

// (b) method=POST：服务端看到 POST，参数在 body、带 form Content-Type。
// cfg 里写的是 GET，同样证明 method 参数驱动分派。
func TestConfirmSendDispatchesPOST(t *testing.T) {
	got := make(chan capturedDelivery, 1)
	p, srv := confirmSendFixture(t, "ok", func(c *capturedDelivery) { got <- *c })
	cfg := daemonConfig{
		Server: srv.URL, LoginPath: "/login", LoginPage: "/", UserAgent: "test-agent",
		LoginForm: protocol.Form{Method: "GET"},
	}

	body, err := confirmSend(cfg, p)("POST", cfg.Server+cfg.LoginPath,
		map[string]string{"userName": "u", "pwd": "p"})
	if err != nil {
		t.Fatalf("POST 型行为确认应送达: %v", err)
	}
	if body != "ok" {
		t.Fatalf("应返回门户原文，实际 %q", body)
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

// (c) 缝接到判定链：生产 Send 发出的响应要经 ConfirmPortalByLogin 用守护同款
// 词表判定——成功词判可用，失败词给出「词表可能不匹配」证据（异校误判提示）。
func TestConfirmSendThroughJudgmentChain(t *testing.T) {
	got := make(chan capturedDelivery, 1)
	p, srv := confirmSendFixture(t, `{"success":true,"msg":"ok"}`, func(c *capturedDelivery) { got <- *c })
	cfg := daemonConfig{
		Server: srv.URL, LoginPath: "/login", LoginPage: "/", UserAgent: "test-agent",
		LoginForm: protocol.Form{Method: "POST"},
		Response:  keeper.DefaultResponseMarks(),
	}

	ok, ev, err := ConfirmPortalByLogin(ConfirmOptions{
		LoginURL: cfg.Server + cfg.LoginPath,
		Method:   "POST",
		Form:     map[string]string{"userName": "u", "pwd": "p"},
		Marks:    cfg.Response,
		Send:     confirmSend(cfg, p),
	})
	if err != nil || !ok {
		t.Fatalf("成功词响应应判可用，ok=%v err=%v 证据=%q", ok, err, ev)
	}
	if !strings.Contains(ev, "成功") {
		t.Errorf("证据应说明命中成功词表，实际 %q", ev)
	}
	<-got // 环回服务确实收到了一次投递
}
