package app

import (
	"strings"

	"cnal/internal/keeper"
)

// 通道认证态四态（2026-11-20 双通道方案 §8.3 M1）。
//
// 与旧的 siblingVerdict（unauth/authed/unknown 三态）相比，多切出「黑洞」与
// 「离线」两档——现场这两种「不通」的处理方式完全不同：
//
//   - blackholed：DNS 通、TCP 超时。未认证 VLAN 的典型形态（11-19 实测：
//     以太网 443 全 refused、NCSI 302 劫持到门户）。登录请求同样发不出去，
//     所以这一态**不可登录**——登录只是再撞一次墙。
//   - offline：连 DNS 都解析不出、或网卡不可达。这条通道根本不在网，
//     任何动作都是浪费预算。
//
// 只有 unauth 态可登录：门户劫持是「能到门户但没认证」的直接证据。
//
// 状态取值与中文词表在 keeper（唯一出处：内核给 label，面板只显示）。

// 探测阶段取值（区分「DNS 阶段失败」与「TCP 阶段失败」）。
const (
	dnsFailed = "dns"
	dialPhase = "dial"
)

// channelVerdict 由一次 HTTP 探测的原始结果判定认证态（纯函数，便于测试）。
//
// 判定结构沿用 siblingVerdict：3xx 看 Location 是否指向门户，2xx 看正文里有
// 无门户指纹。两者的区别只在「不通」怎么归类——status=0（请求没发出去）
// 是黑洞，不是「未认证」。
func channelVerdict(status int, location, body, portalHost string, hijackMarks []string) string {
	switch status {
	case 301, 302, 303, 307, 308:
		if portalHost != "" && strings.Contains(location, portalHost) {
			return keeper.ChannelUnauth
		}
		return keeper.ChannelAuthed
	case 0:
		return keeper.ChannelBlackholed
	}
	if status >= 200 && status < 300 {
		if containsAny(body, hijackMarks) {
			return keeper.ChannelUnauth
		}
		return keeper.ChannelAuthed
	}
	return keeper.ChannelOffline
}

// channelState 探测阶段的整体结论：DNS 阶段失败 = 离线（通道不在网），
// 其余按 HTTP 结果判定。
func channelState(phase, _ string, portalHost string, hijackMarks []string) string {
	if phase == dnsFailed {
		return keeper.ChannelOffline
	}
	return channelVerdict(0, "", "", portalHost, hijackMarks)
}
