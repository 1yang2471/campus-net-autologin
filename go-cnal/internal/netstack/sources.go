package netstack

import "net"

// PhysicalSourceV6 选本机物理栈 v6 源：第一个 2000::/3 全局地址，且接口名不是
// 虚拟网卡。两道筛子分开拦两件事：2000::/3 滤掉隧道给的 fdfe::/fc00:: 段，
// 接口名滤掉虚拟网卡（与 DiscoverSource 的回退枚举同一纪律）。
// v4 选源在 DiscoverSource（UDP connect 取路由出口，比枚举可靠）。
func PhysicalSourceV6() net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || IsVirtualNIC(ifc.Name) {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipn.IP
			if ip.To4() == nil && len(ip) == net.IPv6len && ip.IsGlobalUnicast() && ip[0]&0xE0 == 0x20 {
				return ip
			}
		}
	}
	return nil
}

// PublicV6Addrs 从地址表筛出公网 v6（2000::/3）：滤掉 v4、环回、链路本地、
// 未指定地址与 ULA（隧道给的 fdfe::），保序去重。
func PublicV6Addrs(addrs []net.IP) []net.IP {
	var out []net.IP
	seen := map[string]bool{}
	for _, ip := range addrs {
		if ip == nil || ip.To4() != nil || len(ip) != net.IPv6len {
			continue
		}
		if !ip.IsGlobalUnicast() || ip[0]&0xE0 != 0x20 {
			continue
		}
		if seen[ip.String()] {
			continue
		}
		seen[ip.String()] = true
		out = append(out, ip)
	}
	return out
}

// V6SiblingsIn 返回同一接口上除 src 外的公网 v6 地址。门户按**地址**记在线：
// Windows 隐私扩展的临时地址与稳定地址是两个独立会话，切账号只搬一个会留下
// 「v4 换了、IPv6 仍是原账号」（2026-11-11 实证：稳定地址归新账号、临时地址
// 仍归旧账号，online_check 逐地址读出）。
func V6SiblingsIn(src net.IP, ifaceAddrs []net.IP) []net.IP {
	var out []net.IP
	for _, ip := range PublicV6Addrs(ifaceAddrs) {
		if src != nil && ip.Equal(src) {
			continue
		}
		out = append(out, ip)
	}
	return out
}

// V6Siblings 找到 src 所在物理接口，返回该接口的其它公网 v6 地址。
// 找不到 src 所属接口时返回 nil（不猜别的接口的会话）。
func V6Siblings(src net.IP) []net.IP {
	if src == nil {
		return nil
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || IsVirtualNIC(ifc.Name) {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		var ips []net.IP
		found := false
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ips = append(ips, ipn.IP)
			if ipn.IP.Equal(src) {
				found = true
			}
		}
		if found {
			return V6SiblingsIn(src, ips)
		}
	}
	return nil
}
