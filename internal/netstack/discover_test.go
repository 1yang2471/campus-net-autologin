package netstack

import (
	"net"
	"testing"
)

func mustInterfaces(t *testing.T) []net.Interface {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("枚举网卡失败: %v", err)
	}
	return ifaces
}

// 选源升级（移植 Python discover_physical_ip）：不再"枚举取第一个"——那在
// TUN 下会选中隧道/fake-ip 地址（2026-11-07 实测：Go 版选到 28.0.0.1 与
// 172.29.153.208，两次都非物理源）。改为 UDP connect 到校园 DNS 取出口源
// （校园内网不经 TUN），并用排除段把环回/APIPA/两代 fake-ip 挡掉。

// 行为：排除段一律不可作源——环回/APIPA/两代 fake-ip（含参考校实测的 28.）。
func TestUsableSourceRejectsExcludedRanges(t *testing.T) {
	bad := []struct{ ip, why string }{
		{"127.0.0.1", "环回"},
		{"169.254.1.1", "APIPA"},
		{"28.0.0.1", "fake-ip（参考校实测段）"},
		{"198.18.0.1", "fake-ip（mihomo 默认段）"},
		{"198.19.5.5", "fake-ip（mihomo 默认段）"},
	}
	for _, b := range bad {
		if UsableSource(net.ParseIP(b.ip)) {
			t.Errorf("%s (%s) 必须不可作源", b.ip, b.why)
		}
	}
	if UsableSource(nil) {
		t.Error("空源必须不可用")
	}
	for _, ok := range []string{"10.129.17.20", "172.20.5.6"} {
		if !UsableSource(net.ParseIP(ok)) {
			t.Errorf("%s 应可作源", ok)
		}
	}
}

// 行为：虚拟网卡接口名（Python VIRTUAL_NIC 表同源）不得被回退枚举选中。
func TestVirtualNICNameMatch(t *testing.T) {
	for _, name := range []string{"VMware Network Adapter VMnet1", "vEthernet (WSL)",
		"Hyper-V Virtual Ethernet", "TAP-Windows Adapter", "tun0",
		"VirtualBox Host-Only", "docker0", "Loopback Pseudo-Interface 1", "npcap"} {
		if !IsVirtualNIC(name) {
			t.Errorf("%q 应判为虚拟网卡", name)
		}
	}
	for _, name := range []string{"WLAN", "以太网", "Ethernet", "Realtek PCIe GbE Family Controller"} {
		if IsVirtualNIC(name) {
			t.Errorf("%q 是物理网卡，不得排除", name)
		}
	}
}

// 行为（真机）：DiscoverSource 给出路由表认定的出口源——非环回、非排除段。
// 校园 DNS 不可达时仍应凭路由表选路（UDP connect 不出包）。
func TestDiscoverSourcePicksRoutableSource(t *testing.T) {
	src := DiscoverSource("")
	if src == nil {
		t.Skip("本机无可用 v4 源（无网环境），跳过")
	}
	if !UsableSource(src) {
		t.Errorf("发现的源必须是可用源，实际 %v", src)
	}
	if src.To4() == nil {
		t.Errorf("必须是 v4 源，实际 %v", src)
	}
}

// 行为（真机契约）：选源结果必可用——旧法「枚举取第一个」会选中 TUN/fake-ip
// 源（2026-11-07 实测选中 FlClash 的 28.0.0.1），新法必须给出可用源。
func TestDiscoverSourceFixesLegacyTunnelPick(t *testing.T) {
	src := DiscoverSource("")
	if src == nil {
		t.Skip("本机无可用 v4 源（无网环境），跳过")
	}
	if !UsableSource(src) {
		t.Fatalf("新法必须给出可用源，实际 %v", src)
	}
	// 本机若存在虚拟网卡的地址，断言没选它（TUN 病回归守卫）。
	for _, ifc := range mustInterfaces(t) {
		if !IsVirtualNIC(ifc.Name) {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.Equal(src) {
				t.Errorf("选中的 %v 属于虚拟网卡 %q——TUN 病回归", src, ifc.Name)
			}
		}
	}
}
