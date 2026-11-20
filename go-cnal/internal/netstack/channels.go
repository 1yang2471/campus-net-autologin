package netstack

import "net"

// 通道枚举（2026-11-20 双通道方案 §8.3 M1）。
//
// 一条通道 = 一条物理链路上的一个 v4 会话身份 = (接口名, 源 IP)。门户按
// 来源 IP 记会话（11-20 实测：WiFi 已认证不放行有线口 IP，有线必须从有线
// 源再登一次），所以身份必须含 IP——DHCP 换 IP 就是一条新会话。
//
// 枚举纪律与选源纪律同源：物理网卡、单播 v4、非 fake-ip/环回/APIPA。
// 判据刻意是**行为**（这条链路有可用的单播地址）而不是结构（有没有默认
// 路由）——2026-11-06 设计 §11.5/§11.6 实测否证过前缀表与 PhysicalAdapter
// 这类结构信号；能不能认证由四态探测说了算（见 §8.4）。

// Channel 一条通道的身份（接口名 + 源 IP）。
type Channel struct {
	// Iface 接口名（"WLAN 2" / "以太网"）——面板要显示它：两张卡同为
	// 「以太网」/「本地连接」时，光看 IP 用户认不出是哪张。
	Iface string
	// IP 该通道的 v4 源地址（门户按它认会话）。
	IP net.IP
}

// ID 通道的稳定标识（接口名 + IP）。DHCP 换 IP 即换身份——旧会话不在了。
func (c Channel) ID() string {
	return c.Iface + "|" + c.IP.String()
}

// String 展示用短串（面板/日志）。
func (c Channel) String() string {
	return c.Iface + " " + c.IP.String()
}

// EnumerateChannels 枚举本机可作为通道的物理网卡地址。
// 返回空 = 没有可用物理通道（调用方据此不启动多通道逻辑）。
func EnumerateChannels() []Channel {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	return channelsFrom(ifaces, func(i net.Interface) ([]net.Addr, error) {
		return i.Addrs()
	})
}

// channelsFrom 纯函数版枚举（可注入网卡与地址，测试不依赖真机）。
//
// 过滤四道：接口未启用 / 虚拟网卡 / 地址不是可用单播 v4 / 同 IP 重复。
// 「可用单播 v4」= UsableSource（滤掉环回、APIPA、两代 fake-ip 段）。
func channelsFrom(ifaces []net.Interface, addrs func(net.Interface) ([]net.Addr, error)) []Channel {
	var out []Channel
	seen := map[string]bool{}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || IsVirtualNIC(ifc.Name) {
			continue
		}
		list, err := addrs(ifc)
		if err != nil {
			continue
		}
		for _, a := range list {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipn.IP
			if !UsableSource(ip) {
				continue
			}
			key := ip.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Channel{Iface: ifc.Name, IP: ip})
		}
	}
	return out
}
