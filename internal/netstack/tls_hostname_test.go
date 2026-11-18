package netstack

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// 2026-11-11 真机修正后的信任闸：门户地址不能靠「像公共 DNS 就拒」来判。
//
// 实测：汕头大学门户的公开 A 记录就是 1.1.1.2（TLS 证书 CN=*.stu.edu.cn，
// 系统信任库校验通过）；校外时 1.1.1.2 才是真 Cloudflare。形状判断挡不住这两种
// 情况——能证伪的判据是**证书主机名**：冒名者拿不到门户域名的证书。

// 行为：ssl_verify=false（跳过 CA 链）时，门户主机名仍必须校验通过。
// 主机名对不上 → 请求必须失败（凭据不能发出去）。
func TestInsecureTLSStillVerifiesPortalHostname(t *testing.T) {
	addr, _, done := selfSignedTLSListenerWithDNS(t, "portal.example.test", "127.0.0.1:0")
	defer done()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	pin := net.JoinHostPort("127.0.0.1", port)

	c := New(net.IPv4(127, 0, 0, 1), 3*time.Second).InsecureTLS()
	body, err := c.Get("https://portal.example.test:"+port+"/x", nil, pin)
	if err != nil {
		t.Fatalf("证书主机名相符时必须放行: %v", err)
	}
	if body != "ok" {
		t.Errorf("应读到自签服务端响应，实际 %q", body)
	}

	c2 := New(net.IPv4(127, 0, 0, 1), 3*time.Second).InsecureTLS()
	if _, err := c2.Get("https://evil.example.test:"+port+"/x", nil, pin); err == nil {
		t.Fatal("证书主机名与门户不符时必须拒绝（否则凭据发给了冒名服务器）")
	} else if !strings.Contains(err.Error(), "主机名") {
		t.Errorf("错误原因须指明证书主机名不符，实际 %q", err)
	}
}

// 行为：默认配置（ssl_verify=true）仍走标准校验——自签证书必须被拒。
// 与 TestInsecureTLSStillVerifiesPortalHostname 一起把两头钉死：
// 链校验默认开、主机名校验任何情况下都开。
func TestDefaultTLSStillRejectsUntrustedChain(t *testing.T) {
	addr, _, done := selfSignedTLSListenerWithDNS(t, "portal.example.test", "127.0.0.1:0")
	defer done()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	pin := net.JoinHostPort("127.0.0.1", port)

	c := New(net.IPv4(127, 0, 0, 1), 3*time.Second)
	if _, err := c.Get("https://portal.example.test:"+port+"/x", nil, pin); err == nil {
		t.Error("默认配置对自签证书必须失败（证书校验不得静默关闭）")
	}
}

// selfSignedTLSListenerWithDNS 起一个自签证书的本地 TLS 服务，证书只覆盖给定
// 域名（不含 IP SAN）——用来验证「按主机名比对」这条闸确实在起作用。
// listenAddr 形如 "127.0.0.1:0"（端口 0 = 随机端口）。
// 第三个返回值是「服务端看到的 SNI」读取器。
func selfSignedTLSListenerWithDNS(t *testing.T, dnsName, listenAddr string) (string, func() string, func()) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(2),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{dnsName},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	var mu sync.Mutex
	var gotSNI string
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		t.Fatal(err)
	}
	tlsLn := tls.NewListener(ln, &tls.Config{GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		mu.Lock()
		gotSNI = hello.ServerName
		mu.Unlock()
		return &cert, nil
	}})
	srv := &http.Server{ReadHeaderTimeout: 2 * time.Second, Handler: http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		})}
	go func() { _ = srv.Serve(tlsLn) }()
	return ln.Addr().String(), func() string {
		mu.Lock()
		defer mu.Unlock()
		return gotSNI
	}, func() { _ = srv.Close() }
}
