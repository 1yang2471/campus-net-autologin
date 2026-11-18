package netstack

import (
	"net"
	"net/http"
	"testing"
	"time"
)

// 钉 IP 请求（L2 DPI 探测与登录主链路都需要）：连的是解析出来的真实 IP，
// Host 头与 TLS SNI 仍是原域名——否则要么过不了虚拟主机路由，要么在 TUN 下
// 连到 fake-ip 映射（测的是隧道不是校园网）。

// 行为：ipPin 生效——实际连 pin 的地址，服务端看到的 Host 仍是 URL 里的域名。
func TestGetPinsIPKeepsHost(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	gotHost := make(chan string, 1)
	srv := &http.Server{
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotHost <- r.Host
			_, _ = w.Write([]byte("ok"))
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	c := New(net.IPv4(127, 0, 0, 1), 2*time.Second)
	// URL 用域名形态（连不上），ipPin 指向真实监听地址。
	if _, err := c.Get("http://cdn.example.test/probe", nil, ln.Addr().String()); err != nil {
		t.Fatalf("钉 IP 请求失败: %v", err)
	}
	if h := <-gotHost; h != "cdn.example.test" {
		t.Errorf("Host 头应保留域名，实际 %q", h)
	}
}

// 行为：钉 IP 时 TLS SNI 仍是域名（TUN 环境下 SNI 决定出口策略，决定 DPI
// 是否放行——钉 IP 只改连接目标，不改协议层身份）。
//
// 2026-11-11：ssl_verify=false 也开始校验证书主机名，所以服务端证书必须覆盖
// 该域名；顺带把服务端看到的 SNI 记下来断言（原测试只验证请求成功）。
func TestGetPinsIPPinsSNI(t *testing.T) {
	addr, sni, closeFn := selfSignedTLSListenerWithDNS(t, "sni-probe.example.test", "127.0.0.1:0")
	defer closeFn()

	c := New(net.IPv4(127, 0, 0, 1), 2*time.Second).InsecureTLS()
	body, err := c.Get("https://sni-probe.example.test/", nil, addr)
	if err != nil {
		t.Fatalf("钉 IP TLS 请求失败: %v", err)
	}
	if body != "ok" {
		t.Errorf("应读到自签服务端响应，实际 %q", body)
	}
	if got := sni(); got != "sni-probe.example.test" {
		t.Errorf("SNI 应仍是域名 sni-probe.example.test，实际 %q", got)
	}
}
