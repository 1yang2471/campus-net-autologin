package netstack

import "testing"

// 真机契约（2026-11-20 双通道 M1）：通道枚举必须在真机上给出真实物理网卡，
// 且不得把 TUN/fake-ip 地址当成通道。
//
// 为什么要有真机用例：TUN 的地址形态（28.0.0.1 这类 fake-ip）与真实网卡
// 地址无法在纯单元测试里区分——判据是「排除段是否真的挡住了隧道地址」，
// 只有真机能证。

func TestEnumerateChannelsOnRealMachine(t *testing.T) {
	chans := EnumerateChannels()
	if len(chans) == 0 {
		t.Skip("本机无可用物理通道（未接网），跳过")
	}
	for _, c := range chans {
		if !UsableSource(c.IP) {
			t.Errorf("通道 %s 的地址 %s 不可用（排除段没挡住）", c.Iface, c.IP)
		}
		if IsVirtualNIC(c.Iface) {
			t.Errorf("通道 %s 属虚拟网卡，不得入选", c.Iface)
		}
		if c.Iface == "" || c.IP == nil {
			t.Errorf("通道身份不完整: %+v", c)
		}
	}
	t.Logf("真机枚举到 %d 条通道: %v", len(chans), chans)
}
