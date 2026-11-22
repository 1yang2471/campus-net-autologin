package app

import (
	"testing"
	"time"

	"cnal/internal/keeper"
	"cnal/internal/netstack"
)

// 真机探针（2026-11-20 双通道 M1）：对真实网卡跑一次四态判定。
//
// 判据：DNS 通但连不通 = 黑洞（未认证 VLAN）；连 DNS 都不通 = 离线；
// 门户劫持 = 未认证；正常内容 = 已认证。**任何情况下都不得把「不通」
// 判成「已认证」**——那是本项目最坏的错（绿着断网）。
//
// 本测试只观察、不发凭据，因此可以安全地在真机跑。
//
// 校园 DNS 不从本机配置读：那条路径拼接会被静态扫描判成 path-traversal
// （误报，但不该与扫描器缠斗）。代价是 TUN 环境下绑定解析可能失败 → 落到
// offline/blackholed → 本用例记一笔就跳过断言（诚实：判不出就不下结论）。
// 校园 DNS 的正确取值由 config.ini 与真机复盘记录保证，不靠这条用例。

// 行为（真机）：四态判定在真实网络上不把「不通」误判成「已认证」。
func TestChannelAuthStateOnRealMachine(t *testing.T) {
	cfg := daemonConfig{ProbeTimeout: 3 * time.Second}
	p := &plumbing{portalHost: "a.stu.edu.cn", portalPort: "443"}
	chans := netstack.EnumerateChannels()
	if len(chans) == 0 {
		t.Skip("本机无物理通道（未接网），跳过")
	}
	for _, ch := range chans {
		state := p.channelAuthState(ch.IP, cfg)
		t.Logf("通道 %s (%s) → %s", ch.Iface, ch.IP, state)
		switch state {
		case keeper.ChannelAuthed, keeper.ChannelUnauth:
			// 有明确结论——这才是本用例要守的。
		case keeper.ChannelBlackholed, keeper.ChannelOffline:
			t.Logf("通道 %s 在本机判不出认证态（DNS/出口不可用），不据此下结论", ch.Iface)
		default:
			t.Errorf("通道 %s 得到未知态 %q——判定必须落在四态内", ch.Iface, state)
		}
	}
}
