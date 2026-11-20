package app

import (
	"net"

	"cnal/internal/keeper"
	"cnal/internal/netstack"
)

// 通道观察接线（2026-11-20 双通道方案 §8.3 M1）。
//
// 复用旁路观察（sibling.go）的绑源技术，但把结论从「未认证/已认证/未知」
// 升级成四态（见 channel.go）：现场有两种不同的「不通」——DNS 通但连不通
//（未认证 VLAN 黑洞）与连 DNS 都解析不出（网卡不在网）——处理方式不同，
//且都必须与「已认证」严格分开（误报在线是最坏的错）。
//
// 纪律：只观察、零凭据。通道级登录是 M2，且默认关（spec §8.2）。

// channelProbeHost 通道探测目标：用大众站（与 L2/旁路观察同源）——它是
// 认证态的分水岭：未认证 VLAN 的白名单只放行锚点，大众站会被劫持到门户
// （11-19 实测：以太网锚点 204 但百度 302 劫持）。
const channelProbeHost = "www.baidu.com"

// channelViews 枚举可观察的通道（把 netstack.Channel 译成内核视图）。
// 当前绑定源不列入：它由主链路的 L1/会话真值判定，再探一遍是重复请求。
func (p *plumbing) channelViews() []keeper.ChannelView {
	var cur net.IP
	if p.currentV4 != nil {
		cur = p.currentV4()
	}
	var out []keeper.ChannelView
	for _, ch := range netstack.EnumerateChannels() {
		if cur != nil && ch.IP.Equal(cur) {
			continue
		}
		out = append(out, keeper.ChannelView{
			ID:    ch.ID(),
			Iface: ch.Iface,
			IP:    ch.IP.String(),
		})
	}
	return out
}

// channelProbe 探测单条通道的认证态（零凭据）。
func (p *plumbing) channelProbe(v keeper.ChannelView, cfg daemonConfig) string {
	src := net.ParseIP(v.IP)
	if src == nil {
		return keeper.ChannelOffline
	}
	return p.channelAuthState(src, cfg)
}

// channelAuthState 对单张卡跑一次四态判定。
//
// 与 siblingAuthState 的差别：
//   - DNS 阶段失败与 TCP 阶段失败分开（前者=离线，后者=黑洞）；
//   - 判定用 channelVerdict（黑洞不再被算成 unknown）。
//
// DNS 先走绑源校园 DNS；未认证 VLAN 上它常不回应（11-19 实测），或应答里
// 先给 v6——都回退系统解析的 ip4（观察无凭据；fake-ip 会被 ResolveSystem 过滤）。
func (p *plumbing) channelAuthState(src net.IP, cfg daemonConfig) string {
	ip, err := netstack.ResolveBound(src, cfg.CampusDNS, channelProbeHost, cfg.ProbeTimeout)
	if err != nil || ip.To4() == nil {
		ip, err = netstack.ResolveSystem(channelProbeHost)
		if err != nil || ip.To4() == nil {
			return keeper.ChannelOffline // 连 DNS 都不通：这条通道不在网
		}
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return keeper.ChannelOffline
	}
	c := netstack.New(src, cfg.ProbeTimeout)
	pin := net.JoinHostPort(ip4.String(), "80")
	st, hdr, body, err := c.GetStatusPinnedHeaders("http://"+channelProbeHost+"/", nil, pin)
	if err != nil {
		return keeper.ChannelBlackholed // DNS 通但连不通：未认证 VLAN 黑洞
	}
	loc := ""
	if hdr != nil {
		loc = hdr.Get("Location")
	}
	return channelVerdict(st, loc, body, p.portalHost, cfg.HijackMarks)
}

// wireChannels 把通道观察接到守护内核（M1：只观察）。
func wireChannels(d *keeper.Daemon, p *plumbing, cfg daemonConfig) {
	d.Channels = keeper.NewChannelGuard()
	d.Channels.Enumerate = func() []keeper.ChannelView { return p.channelViews() }
	d.Channels.Probe = func(v keeper.ChannelView) string { return p.channelProbe(v, cfg) }
}
