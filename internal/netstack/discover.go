package netstack

import (
	"net"
	"strings"
)

// 物理源发现（移植 Python discover_physical_ip）：先 UDP connect 到校园 DNS
// 让内核按路由表选出口源（校园内网不经 TUN，因此拿到的是物理源）；失败再
// 回退枚举适配器，按接口名排除虚拟网卡。旧版"枚举取第一个"在 TUN 下会选中
// 隧道/fake-ip 地址（2026-11-07 实测选到 28.0.0.1 与 172.29.153.208）。

// lanProbeFallback 未配置校园 DNS 时的通用选路探测地址（只用 connect 选路由，
// 不出包；Python LAN_PROBE_FALLBACK 同值）。
const lanProbeFallback = "223.5.5.5"

// excludedSrcNets 不可作源的网段：环回、APIPA、两代 fake-ip（参考校实测 28./8
// 与 mihomo 默认 198.18/15）。fake-ip 段是环境事实，与 Resolver.FakeIPNets 同源，
// 这里取 Python EXCLUDED_SRC_PREFIXES 的等价网段。
func excludedSrcNets() []*net.IPNet {
	return []*net.IPNet{
		mustCIDR("127.0.0.0/8"),
		mustCIDR("169.254.0.0/16"),
		mustCIDR("28.0.0.0/8"),
		mustCIDR("198.18.0.0/15"),
	}
}

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic("内置网段非法: " + s)
	}
	return n
}

// UsableSource 该源地址可否绑定物理栈（空/环回/APIPA/fake-ip 一律不可）。
func UsableSource(ip net.IP) bool {
	if ip == nil || ip.To4() == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	for _, n := range excludedSrcNets() {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

// virtualNICMarks 虚拟网卡接口名标记（Python VIRTUAL_NIC 表同源）。
var virtualNICMarks = []string{
	"vmware", "virtual", "vethernet", "wsl", "hyper-v", "virtualbox",
	"tap-", "tun", "loopback", "docker", "vmnet", "bridge", "npcap",
}

// IsVirtualNIC 接口名是否属于虚拟/隧道网卡（回退枚举时排除）。
func IsVirtualNIC(name string) bool {
	low := strings.ToLower(name)
	for _, m := range virtualNICMarks {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// DiscoverSource 发现物理栈 v4 源。campusDNS 为空时用通用探测地址。
// 返回 nil = 未发现可用源（调用方 fail-closed，不触网）。
//
// 注意（2026-11-11）：本函数只回答「内核按路由表会从哪出去」，**不回答**
// 「哪条链路能到校园认证网关」——插了网线后路由表偏向以太网，而认证网关只在
// WLAN 网段可达。选源要按门户可达性来，见 CandidateSources + SourcePicker。
func DiscoverSource(campusDNS string) net.IP {
	probe := probeAddress(campusDNS)
	if conn, err := net.Dial("udp4", net.JoinHostPort(probe, "53")); err == nil {
		src := conn.LocalAddr().(*net.UDPAddr).IP
		_ = conn.Close()
		if UsableSource(src) {
			return src
		}
	}
	// 回退：枚举适配器，按接口名排除虚拟网卡（TUN 会抢走 UDP 出口时走这里）。
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
			if ipn, ok := a.(*net.IPNet); ok && UsableSource(ipn.IP) {
				return ipn.IP
			}
		}
	}
	return nil
}

// probeAddress 选路探测地址：配置了合法 v4 校园 DNS 就用它本身（本校内网段
// 直达路由最精确），否则用通用地址（Python lan_probe_address 同源）。
func probeAddress(campusDNS string) string {
	if ip := net.ParseIP(strings.TrimSpace(campusDNS)); ip != nil && ip.To4() != nil {
		return ip.String()
	}
	return lanProbeFallback
}

// CandidateSources 候选物理 v4 源（按优先级）。
//
// 顺序：内核路由偏好源（DiscoverSource；通常是 metric 更低的那张网卡，也就是
// 用户既定决策里的「有线优先」）→ 其余非虚拟网卡上的可用 v4（拔线、换网卡、
// 休眠唤醒后的兜底）。去重且保持顺序。
//
// 只给候选，不做取舍——能不能到门户由探测说了算（SourcePicker）。
func CandidateSources(campusDNS string) []net.IP {
	var out []net.IP
	seen := map[string]bool{}
	add := func(ip net.IP) {
		if !UsableSource(ip) || seen[ip.String()] {
			return
		}
		seen[ip.String()] = true
		out = append(out, ip)
	}
	add(DiscoverSource(campusDNS))
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
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
			if ipn, ok := a.(*net.IPNet); ok {
				add(ipn.IP)
			}
		}
	}
	return out
}
