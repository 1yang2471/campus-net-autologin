package netstack

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 行为：请求必须从指定源地址发出（绑源是产品核心纪律，不是优化项）。
// 用环回起真监听器：LocalAddr 若未绑，Go 会挑默认源，测试就能抓到。
func TestDialBindsRequestedSource(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		// 服务端看到的"对端"就是客户端的源地址
		got <- c.RemoteAddr().(*net.TCPAddr).IP.String()
		// 计划原文漏了"回响应"这步：只收不回会让客户端 Get 干等超时——补最小响应
		_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
		_, _ = io.ReadFull(c, make([]byte, 4096))
	}()

	c := New(net.IPv4(127, 0, 0, 1), 2*time.Second) // 绑 loopback 源
	if _, err := c.Get("http://"+ln.Addr().String()+"/x", nil, ""); err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	if src := <-got; src != "127.0.0.1" {
		t.Errorf("源地址应为绑定的 127.0.0.1，实际 %s", src)
	}
}

// 行为：首响应头必须原样带回（首跑探测靠 302 Location 自报门户地址；
// 跟随跳转或丢掉头就失去了门户发现能力）。
func TestGetStatusHeadersKeepsLocation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "http://a.stu.edu.cn/ac_portal/login.php")
		w.WriteHeader(http.StatusFound)
		_, _ = w.Write([]byte("redirecting"))
	}))
	defer srv.Close()
	c := New(net.IPv4(127, 0, 0, 1), 2*time.Second)
	st, hdr, body, err := c.GetStatusHeaders(srv.URL+"/generate_204", nil)
	if err != nil {
		t.Fatal(err)
	}
	if st != http.StatusFound {
		t.Errorf("状态应为 302，实际 %d", st)
	}
	if got := hdr.Get("Location"); got != "http://a.stu.edu.cn/ac_portal/login.php" {
		t.Errorf("Location 应原样带回，实际 %q", got)
	}
	if body != "redirecting" {
		t.Errorf("响应体应带回，实际 %q", body)
	}
}

// 行为：IP 字面量不查 DNS；v6 字面量拒绝（探测链路只支持 v4 物理源）。
func TestResolveSystemIPLiteral(t *testing.T) {
	ip, err := ResolveSystem("10.1.2.3")
	if err != nil || ip.String() != "10.1.2.3" {
		t.Errorf("v4 字面量应直返，实际 %v %v", ip, err)
	}
	if _, err := ResolveSystem("2001:db8::1"); err == nil {
		t.Error("v6 字面量应拒绝")
	}
}

// 行为：请求头里的换行必须拒绝（CRLF 注入防线，现版本 P1-13 同源）。
func TestRejectsHeaderInjection(t *testing.T) {
	c := New(net.IPv4(127, 0, 0, 1), time.Second)
	_, err := c.Get("http://127.0.0.1:1/x", map[string]string{"Host": "a\r\nX-Evil: 1"}, "")
	if err == nil || !strings.Contains(err.Error(), "头") {
		t.Errorf("含换行的头必须被拒绝，实际 %v", err)
	}
}

// selfSignedTLSListener 起一个自签证书的本地 TLS 服务（stdlib 生成证书）。
func selfSignedTLSListener(t *testing.T) (string, func()) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsLn := tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}})
	srv := &http.Server{ReadHeaderTimeout: 2 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})}
	go func() { _ = srv.Serve(tlsLn) }()
	return ln.Addr().String(), func() { _ = srv.Close() }
}

// 行为：默认配置必须校验证书——自签必须被拒（安全默认，Mimosa 规矩）；
// 只有显式 InsecureTLS() 才放开（自签门户是校事实，由用户配置选择）。
func TestTLSVerifyDefaultSecureOptOutExplicit(t *testing.T) {
	addr, done := selfSignedTLSListener(t)
	defer done()
	u := "https://" + addr + "/x"

	c := New(net.IPv4(127, 0, 0, 1), 2*time.Second)
	if _, err := c.Get(u, nil, ""); err == nil {
		t.Errorf("默认配置对自签证书必须失败（证书校验不得静默关闭）")
	}

	c2 := New(net.IPv4(127, 0, 0, 1), 2*time.Second).InsecureTLS()
	if _, err := c2.Get(u, nil, ""); err != nil {
		t.Errorf("显式放开后应能访问自签门户: %v", err)
	}
}
