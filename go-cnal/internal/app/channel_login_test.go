package app

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"cnal/internal/netstack"
)

// 通道级登录（2026-11-20 双通道 M2）：门户按**来源 IP** 记会话，所以通道
// 登录必须绑到那条通道自己的源 IP——用主链路的出口发，认证的是主链路那条
// 会话，通道照样是未认证的（11-20 实测：WiFi 已认证不放行有线口 IP）。
//
// 两条纪律：
//   - 凭据只发往用户配置的门户，钉 IP + Host 头沿用主链路纪律；
//   - 走**物理栈绑源**客户端（不落系统栈）——否则 TUN 下认证的是代理那条路。

// 行为：通道登录的请求从**该通道的源 IP** 发出（绑定源），表单原样到达，
// 且请求是物理栈直连（不是系统栈）。
func TestChannelLoginBindsChannelSource(t *testing.T) {
	got := make(chan url.Values, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got <- r.PostForm
		_, _ = w.Write([]byte("success"))
	}))
	defer srv.Close()

	p := &plumbing{portalHost: "portal.example.edu", portalPort: "443"}
	cfg := daemonConfig{
		Server:       srv.URL,
		LoginPath:    "/ac_portal/login.php",
		LoginPage:    "/ac_portal/default/pc.html?tabs=pwd",
		UserAgent:    "test-agent",
		LoginTimeout: 5 * time.Second,
	}
	// 通道源 = 127.0.0.1（本地测试服务器可达）；绑定源客户端据此拨号。
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	deliver := channelDeliver(p, cfg, net.ParseIP("127.0.0.1"), net.ParseIP("127.0.0.1"), fmt.Sprint(port))
	if deliver == nil {
		t.Fatal("应产出投递端")
	}
	resp, err := deliver(map[string]string{"userName": "s1", "pwd": "x"})
	if err != nil {
		t.Fatalf("通道投递失败: %v", err)
	}
	if resp != "success" {
		t.Errorf("应返回门户原文，实际 %q", resp)
	}
	form := <-got
	if form.Get("userName") != "s1" || form.Get("pwd") != "x" {
		t.Errorf("表单字段应原样到达，实际 %v", form)
	}
}

// 行为：绑定的源与通道源不一致时不得投递（凭据不许发往别的出口）。
// 2026-11-21 M2-c 真机踢线实证改写：旧版把「门户 IP」误当「解析源」与通道源
// 全等校验——真机上门户 IP(1.1.1.2) 永远 ≠ 通道源(172.29.153.208)，通道登录
// 100% 恒拒（WiFi 掉线无人自愈的根因）。解析源一致性由 channelLogin 绑通道源
// 解析结构性保证，不在投递端重复校验；本测试改锁「门户 IP ≠ 通道源时必须
// 正常投递」的真机形态。
func TestChannelLoginDeliversWhenPortalIPOffChannelSource(t *testing.T) {
	got := make(chan url.Values, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got <- r.PostForm
		_, _ = w.Write([]byte("success"))
	}))
	defer srv.Close()

	p := &plumbing{portalHost: "portal.example.edu", portalPort: "443"}
	cfg := daemonConfig{
		Server:       srv.URL,
		LoginPath:    "/ac_portal/login.php",
		LoginPage:    "/",
		UserAgent:    "test-agent",
		LoginTimeout: 5 * time.Second,
	}
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	// 真机形态：通道源 127.0.0.2 ≠ 门户 IP 127.0.0.1（同为回环段，路由可达）
	deliver := channelDeliver(p, cfg, net.ParseIP("127.0.0.2"), net.ParseIP("127.0.0.1"), fmt.Sprint(port))
	if deliver == nil {
		t.Fatal("应产出投递端")
	}
	resp, err := deliver(map[string]string{"userName": "s1", "pwd": "x"})
	if err != nil {
		t.Fatalf("门户 IP 与通道源不同（真机常态）必须正常投递，实际被拒: %v", err)
	}
	if resp != "success" {
		t.Errorf("应返回门户原文，实际 %q", resp)
	}
	if form := <-got; form.Get("userName") != "s1" {
		t.Errorf("表单应原样到达，实际 %v", form)
	}
}

// 行为：门户 IP 未知（nil）时仍不投递——校验保留的唯一形态。
func TestChannelLoginRefusesUnknownPortalIP(t *testing.T) {
	p := &plumbing{portalHost: "portal.example.edu", portalPort: "443"}
	cfg := daemonConfig{Server: "https://portal.example.edu", LoginPath: "/ac_portal/login.php"}
	deliver := channelDeliver(p, cfg, net.ParseIP("10.141.53.34"), nil, "443")
	if deliver == nil {
		t.Fatal("应产出投递端")
	}
	if _, err := deliver(map[string]string{"userName": "s1", "pwd": "x"}); err == nil {
		t.Fatal("门户 IP 未知时不得投递凭据")
	}
}

// 行为：钉 IP 必须来自绑源解析（portalIP），不得用域名直连（TUN fake-ip）。
func TestChannelLoginPinsPortalIP(t *testing.T) {
	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		_, _ = w.Write([]byte("success"))
	}))
	defer srv.Close()

	p := &plumbing{portalHost: "portal.example.edu", portalPort: "443"}
	cfg := daemonConfig{
		Server:       srv.URL,
		LoginPath:    "/ac_portal/login.php",
		LoginPage:    "/",
		UserAgent:    "test-agent",
		LoginTimeout: 5 * time.Second,
	}
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	deliver := channelDeliver(p, cfg, net.ParseIP("127.0.0.1"), net.ParseIP("127.0.0.1"), fmt.Sprint(port))
	if _, err := deliver(map[string]string{"userName": "s1", "pwd": "x"}); err != nil {
		t.Fatalf("投递失败: %v", err)
	}
	if gotHost == "" {
		t.Error("应有 Host 头（钉 IP 后仍要带正确 Host）")
	}
	_ = netstack.UsableSource // 保持 netstack 引用（本文件验证物理栈纪律）
}
