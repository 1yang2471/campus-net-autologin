package netstack

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"
)

// 绑源 DNS 解析（L2 DPI 探测 + 钉 IP 的前置）：TUN 会劫持系统 DNS 给 fake-ip，
// 探测必须从绑定的物理源向校园 DNS 解析，拿到真实 IP 才能钉 IP。fake-ip 段
// 的应答一律丢弃（Python campus_dns_resolve + Resolver.isFakeIP 同源语义，
// 但用真实 UDP DNS 替身验证——不是断言mock）。

// 行为：DNS 应答是 fake-ip（代理劫持）→ 不得钉，必须报错。
func TestResolveBoundRejectsFakeIP(t *testing.T) {
	server := fakeDNS(t, net.IPv4(28, 0, 0, 7))
	ip, err := ResolveBound(net.IPv4(127, 0, 0, 1), server, "probe.example.test", 2*time.Second)
	if err == nil || ip != nil {
		t.Fatalf("fake-ip 应答必须被拒且不得返回地址: %v, %v", ip, err)
	}
}

func TestResolveBoundStrictInterfaceFailureSendsNoDNS(t *testing.T) {
	old := interfaceSnapshots
	t.Cleanup(func() { interfaceSnapshots = old })
	interfaceSnapshots = func() ([]interfaceSnapshot, error) { return nil, nil }
	server := fakeDNS(t, net.ParseIP("1.1.1.2"))
	if _, err := ResolveBoundWithBindMode(net.ParseIP("127.0.0.1"), server, "portal.example.test", time.Second, BindModeInterface); !errors.Is(err, ErrInterfaceBind) {
		t.Fatalf("严格接口失败应返回 ErrInterfaceBind，实际 %v", err)
	}
}

// 行为：门户地址**不能**靠「像公共 DNS 就拒」来判。2026-11-11 真机实测：汕头
// 大学门户的公开 A 记录就是 1.1.1.2（TLS 证书 CN=*.stu.edu.cn，系统信任库校验
// 通过），按 IP 名单拒绝会把真门户挡在门外——11-10 那条黑名单就是这么把守护
// 挡死的。信任闸改由**证书主机名校验**承担（client.go 的 hostOnlyVerifier）。
func TestResolveBoundAcceptsPublicShapedPortalAddress(t *testing.T) {
	for _, good := range []string{"1.1.1.2", "8.8.8.8", "223.5.5.5", "119.29.29.29"} {
		t.Run(good, func(t *testing.T) {
			server := fakeDNS(t, net.ParseIP(good))
			ip, err := ResolveBound(net.IPv4(127, 0, 0, 1), server, "portal.example.test", 2*time.Second)
			if err != nil {
				t.Fatalf("门户地址 %s 不得被形状判断误伤: %v", good, err)
			}
			if !ip.Equal(net.ParseIP(good)) {
				t.Errorf("应得到替身回的真实 IP %s，实际 %v", good, ip)
			}
		})
	}
}

// 行为：正常应答 → 钉 IP 是真值。校验只拦形状上不可能是门户的地址，不得误伤
// 校园内网 IP、公网门户 IP、以及 IPv6 全局地址（异校/双栈都可能用到）。
func TestResolveBoundReturnsRealIP(t *testing.T) {
	for _, good := range []string{
		"10.129.17.10",          // 校园内网
		"172.29.153.208",        // 校园内网
		"202.192.159.2",         // 教育网公网
		"2001:250:3003:aa29::1", // IPv6 全局（fakeDNS 只回 A，此例另测纯函数）
	} {
		if net.ParseIP(good).To4() == nil {
			continue // fakeDNS 替身只构造 A 记录，v6 由 TestUnusablePortalAddr 覆盖
		}
		t.Run(good, func(t *testing.T) {
			server := fakeDNS(t, net.ParseIP(good))
			ip, err := ResolveBound(net.IPv4(127, 0, 0, 1), server, "probe.example.test", 2*time.Second)
			if err != nil {
				t.Fatalf("正常门户 IP %s 不应被误伤: %v", good, err)
			}
			if !ip.Equal(net.ParseIP(good)) {
				t.Errorf("应得到替身回的真实 IP %s，实际 %v", good, ip)
			}
		})
	}
}

// 纯函数层：形状上不可能是门户的地址（环回/链路本地/组播/未指定/fake-ip）仍要
// 判掉；全局单播（含 1.1.1.2 这类公网地址）不得误伤。
func TestUnusablePortalAddr(t *testing.T) {
	for _, bad := range []string{
		"127.0.0.1",   // 环回
		"169.254.1.1", // 链路本地
		"224.0.0.1",   // 组播
		"0.0.0.0",     // 未指定
		"28.0.0.7",    // fake-ip（FlClash 两代之一）
		"198.18.0.1",  // fake-ip（mihomo 默认）
	} {
		if !unusablePortalAddr(net.ParseIP(bad)) {
			t.Errorf("%s 必须判为不可用地址", bad)
		}
	}
	for _, good := range []string{
		"1.1.1.2",               // 真门户地址（汕头大学门户实测）
		"10.129.17.10",          // 校园内网
		"172.29.153.208",        // 校园内网
		"202.192.159.2",         // 教育网公网
		"2001:250:3003:aa29::1", // IPv6 全局
	} {
		if unusablePortalAddr(net.ParseIP(good)) {
			t.Errorf("%s 不得被误判为不可用地址", good)
		}
	}
}

// 行为：DNS 服务器传裸 IP（生产配置 campus_dns=10.129.17.10 的形态）→
// 归一化自动补 :53；已带端口的原样保留（2026-11-08 真机实证：裸 IP 直接
// 进 Dialer 会报 missing port，L2 探测一直静默降级）。
func TestNormalizeDNSServer(t *testing.T) {
	if got := normalizeDNSServer("10.129.17.10"); got != "10.129.17.10:53" {
		t.Errorf("裸 IP 应补 :53，实际 %q", got)
	}
	if got := normalizeDNSServer("10.129.17.10:5353"); got != "10.129.17.10:5353" {
		t.Errorf("带端口应保留，实际 %q", got)
	}
	if got := normalizeDNSServer(""); got != "127.0.0.1:53" {
		t.Errorf("空值应回环兜底，实际 %q", got)
	}
}

// fakeDNS 起一个只回固定 A 记录的 UDP DNS 替身（stdlib 手写 wire format）。
func fakeDNS(t *testing.T, answer net.IP) string {
	t.Helper()
	return fakeDNSOn(t, answer, "127.0.0.1:0")
}

// fakeDNSOn 同 fakeDNS，但指定监听地址（探测用例要绑源回环：源与 DNS 必须在
// 同一网段，否则「从 lanIP 拨 127.0.0.1」不成立）。
func fakeDNSOn(t *testing.T, answer net.IP, listenAddr string) string {
	t.Helper()
	conn, err := net.ListenPacket("udp4", listenAddr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, rerr := conn.ReadFrom(buf)
			if rerr != nil {
				return
			}
			resp := buildDNSAnswer(buf[:n], answer)
			if resp != nil {
				_, _ = conn.WriteTo(resp, addr)
			}
		}
	}()
	return conn.LocalAddr().String()
}

// buildDNSAnswer 从 query 构应答：只保留 header + question，随后写入 A 记录。
func buildDNSAnswer(query []byte, answer net.IP) []byte {
	if len(query) < 13 {
		return nil
	}
	qend := 12
	for qend < len(query) {
		b := query[qend]
		if b == 0 {
			qend++
			break
		}
		if b&0xc0 != 0 {
			return nil // 无法处理压缩指针，测试域名是普通标签
		}
		qend += int(b) + 1
	}
	if qend+4 > len(query) {
		return nil
	}
	// question 后为 QTYPE/QCLASS 共 4 字节。
	qend += 4

	resp := make([]byte, qend)
	copy(resp, query[:qend])
	// FLAGS：QR=1 RD=1 RA=1，RCODE=0；ANCOUNT=1，ARCOUNT=0。
	binary.BigEndian.PutUint16(resp[2:4], 0x8180)
	binary.BigEndian.PutUint16(resp[6:8], 1)
	binary.BigEndian.PutUint16(resp[10:12], 0)
	// A 记录：NAME(2)=0xc00c、TYPE=1、CLASS=1、TTL=60、RDLENGTH=4、RDATA。
	rr := &bytes.Buffer{}
	binary.Write(rr, binary.BigEndian, []byte{0xc0, 0x0c})
	binary.Write(rr, binary.BigEndian, uint16(1))
	binary.Write(rr, binary.BigEndian, uint16(1))
	binary.Write(rr, binary.BigEndian, uint32(60))
	binary.Write(rr, binary.BigEndian, uint16(4))
	binary.Write(rr, binary.BigEndian, answer.To4())
	return append(resp, rr.Bytes()...)
}
