package netstack

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// ResolveBound 从绑定的物理源向校园 DNS 解析 host，返回第一个可信地址。
// TUN 会劫持系统 DNS 给 fake-ip（198.18/15、28/8），探测（L2 DPI）必须走
// 校园 DNS + 绑源，否则钉到的是隧道地址——Python campus_dns_resolve 同源语义。
//
// 只保留**形状类**拒绝：环回、链路本地、未指定、组播、非全局单播、fake-ip 段
// ——这些地址不可能是校园门户。
//
// **不再按「公共解析器地址」拒绝**（2026-11-11 真机修正）：汕头大学门户的公开
// A 记录就是 1.1.1.2（TLS 证书 CN=*.stu.edu.cn，系统信任库校验通过），把它
// 当成 DNS 劫持会挡掉真门户。凭据会不会发错地方改由**证书主机名校验**把关
// （netstack/client.go 的 hostOnlyVerifier），那才是能证伪的判据。
func ResolveBound(src net.IP, dnsServer, host string, timeout time.Duration) (net.IP, error) {
	return ResolveBoundWithBindMode(src, dnsServer, host, timeout, DefaultBindMode())
}

func ResolveBoundWithBindMode(src net.IP, dnsServer, host string, timeout time.Duration, mode BindMode) (net.IP, error) {
	if src == nil {
		return nil, errors.New("未绑定源地址：物理栈纪律不允许")
	}
	dnsServer = normalizeDNSServer(dnsServer)
	if mode == BindModeInterface {
		dnsHost, _, err := net.SplitHostPort(dnsServer)
		if err != nil || net.ParseIP(dnsHost) == nil {
			return nil, fmt.Errorf("%w: 严格模式 campus_dns 必须是 IP 字面量", ErrInterfaceBind)
		}
	}
	network := "udp4"
	if src.To4() == nil {
		network = "udp6"
	}
	d, _, err := makeDialer(src, network, timeout, mode)
	if err != nil {
		return nil, fmt.Errorf("绑源 DNS 拨号失败: %w", err)
	}
	res := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return d.DialContext(ctx, network, dnsServer)
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ips, err := res.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	return pickPortalIP(ips)
}

// pickPortalIP 从 DNS 应答里挑一个可用地址：**v4 优先**，没有 v4 才回退 v6。
//
// 为什么 v4 优先（2026-11-20 真机实证）：所有绑定客户端都是 v4 源
// （DiscoverSource/绑源解析/门户钉 IP/L2 探测），用 v4 源拨 v6 目标报
// "no suitable address found"。而 LookupIP("ip") 对同时有 A/AAAA 的域名
// 可能先返回 AAAA（www.baidu.com 实测先给 240e:...）——L2 探测因此**恒判**
// 「出口受限」，面板一直挂假警告而两条源其实都通。
//
// 形状类拒绝沿用 unusablePortalAddr（环回/链路本地/fake-ip 等）。
func pickPortalIP(ips []net.IP) (net.IP, error) {
	rejected := 0
	var onlyV6 net.IP
	for _, ip := range ips {
		if unusablePortalAddr(ip) {
			rejected++
			continue
		}
		if ip.To4() != nil {
			return ip, nil
		}
		if onlyV6 == nil {
			onlyV6 = ip
		}
	}
	if onlyV6 != nil {
		return onlyV6, nil
	}
	if len(ips) > 0 && rejected == len(ips) {
		return nil, fmt.Errorf(
			"DNS 应答无可用地址（%d 个地址全部落在环回/保留段/fake-ip 段）", len(ips))
	}
	return nil, errors.New("DNS 无应答地址")
}

// ResolveSystem 系统 DNS 解析 v4（未配置校园 DNS 时的首跑探测回退）。
// 安全边界：解析对象永远是门户自报/用户配置的 host，不向猜测地址发凭据；
// 过滤 fake-ip——TUN 环境下系统 DNS 会返 198.18/28 段，钉到隧道地址必然不通。
func ResolveSystem(host string) (net.IP, error) {
	if ip := net.ParseIP(strings.TrimSpace(host)); ip != nil {
		if ip.To4() == nil {
			return nil, errors.New("门户地址不是 IPv4")
		}
		return ip, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if !isFakeIPAddr(ip) {
			return ip, nil
		}
	}
	return nil, errors.New("系统 DNS 应答全部落在 fake-ip 段")
}

// normalizeDNSServer 配置里的 campus_dns 是裸 IP（如 10.129.17.10）；
// net.Dialer 需要 host:port，缺端口会报 missing port 让解析永远失败
// （2026-11-08 真机实证：L2 探测一直静默降级）。空值回环兜底。
func normalizeDNSServer(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "127.0.0.1:53"
	}
	if _, _, err := net.SplitHostPort(s); err != nil {
		return net.JoinHostPort(s, "53")
	}
	return s
}

// isFakeIPAddr fake-ip 网段判定（与 UsableSource 的排除段同源：两代 fake-ip）。
func isFakeIPAddr(ip net.IP) bool {
	for _, n := range excludedSrcNets() {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// unusablePortalAddr 该地址在**形状**上不可能是校园门户：环回、链路本地、
// 未指定、组播、非全局单播、fake-ip 段。DNS 应答里出现它们 → 丢弃（多半是
// 代理劫持给的隧道地址）。
//
// 只管形状，不管「像不像公共 DNS」——门户地址可能正好是 1.1.1.2 这类公网地址
// （2026-11-11 真机：汕头大学门户的公开 A 记录就是 1.1.1.2），按 IP 名单拒绝
// 会把真门户挡在门外。
func unusablePortalAddr(ip net.IP) bool {
	return isFakeIPAddr(ip) ||
		!ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsUnspecified() || ip.IsMulticast()
}
