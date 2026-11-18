package netstack

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"time"
)

// 门户可达性探测（2026-11-11 设计 §3）。
//
// 为什么要有它：路由表只回答「去任意公网走哪条路」，而校园认证网关**只在校园
// 网段可达**——插了网线后路由 metric 偏向以太网，恰好选到不能到网关的那条。
// 选源必须以「能不能到门户」为准。
//
// 探测链路**全程不发凭据**：只做 DNS 解析 + TLS 握手，凭据出口仍只有
// deliverPinned 一条。

// ProbePortal 探测「从 src 出发能不能到达门户，且对端确实是门户」。
//
// 步骤：绑源向校园 DNS 解析门户主机名 → 连钉点 → TLS 握手（校验门户主机名；
// verifyChain=false 时跳过 CA 链但仍比对主机名）。任何一步失败都返回带原因的
// 错误，供面板/日志说人话。
func ProbePortal(src net.IP, dnsServer, host, port string, timeout time.Duration, verifyChain bool) error {
	if src == nil {
		return errors.New("未绑定源地址：物理栈纪律不允许")
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ip, err := ResolveBound(src, dnsServer, host, timeout)
	if err != nil {
		return fmt.Errorf("解析门户地址失败: %w", err)
	}
	addr := net.JoinHostPort(ip.String(), port)
	d := &net.Dialer{LocalAddr: &net.TCPAddr{IP: src}, Timeout: timeout}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("连门户 %s 失败: %w", addr, err)
	}
	defer conn.Close()
	cfg := &tls.Config{ServerName: host}
	if !verifyChain {
		// 用户显式 ssl_verify=false：跳过 CA 链，但门户主机名仍要比对。
		cfg.InsecureSkipVerify = !verifyChain //nolint:gosec // 用户显式配置；门户主机名另有关验
		cfg.VerifyPeerCertificate = hostOnlyVerifier(host)
	}
	tc := tls.Client(conn, cfg)
	tc.SetDeadline(time.Now().Add(timeout))
	if err := tc.Handshake(); err != nil {
		return fmt.Errorf("门户 TLS 握手失败（%s）: %w", addr, err)
	}
	return nil
}
