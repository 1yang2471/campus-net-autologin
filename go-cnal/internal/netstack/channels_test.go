package netstack

import (
	"net"
	"testing"
)

// 通道枚举（2026-11-20 双通道方案 §8.3 M1）：一条通道 = 一条物理链路上的
// 一个 v4 会话身份 = (接口名, 源 IP)——门户按来源 IP 记会话（11-20 实测：
// WiFi 已认证不放行有线口 IP），所以身份必须含 IP，DHCP 换 IP 即新会话。
//
// 枚举纪律：只收「状态 Up + 单播 v4 + 非虚拟网卡 + 非 fake-ip/环回/APIPA」。
// fake-ip 段的过滤沿用 UsableSource（参考校实测 TUN 占 28.0.0.1）——
// 把隧道地址当通道会把探测打进代理，等于没有物理栈纪律。

// 假网卡构造（可注入，不依赖真机）。
func fakeIfaces() []net.Interface {
	return []net.Interface{
		{Index: 1, Name: "WLAN 2", Flags: net.FlagUp},
		{Index: 2, Name: "以太网", Flags: net.FlagUp},
		{Index: 3, Name: "FlClash", Flags: net.FlagUp},
		{Index: 4, Name: "Loopback Pseudo-Interface 1", Flags: net.FlagUp},
		{Index: 5, Name: "WLAN 3", Flags: 0}, // 未启用
	}
}

func fakeAddrs() map[int][]net.Addr {
	mk := func(s string) net.Addr {
		return &net.IPNet{IP: net.ParseIP(s), Mask: net.CIDRMask(24, 32)}
	}
	return map[int][]net.Addr{
		1: {mk("172.29.153.208")},
		2: {mk("10.141.53.34")},
		3: {mk("28.0.0.1")},    // TUN fake-ip 段
		4: {mk("127.0.0.1")},   // 环回
		5: {mk("192.168.1.5")}, // 网卡未启用
	}
}

// 行为：只收「Up + 单播 v4 + 非虚拟 + 非 fake-ip」的地址，其余一律不入选。
func TestEnumerateChannelsKeepsOnlyPhysicalUnicastV4(t *testing.T) {
	got := channelsFrom(fakeIfaces(), func(i net.Interface) ([]net.Addr, error) {
		return fakeAddrs()[i.Index], nil
	})
	if len(got) != 2 {
		t.Fatalf("应只收 2 条通道（WLAN 2 / 以太网），实际 %d 条: %+v", len(got), got)
	}
	if got[0].Iface != "WLAN 2" || got[0].IP.String() != "172.29.153.208" {
		t.Errorf("第 1 条应是 WLAN 2/172.29.153.208，实际 %+v", got[0])
	}
	if got[1].Iface != "以太网" || got[1].IP.String() != "10.141.53.34" {
		t.Errorf("第 2 条应是以太网/10.141.53.34，实际 %+v", got[1])
	}
}
