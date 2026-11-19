package app

import (
	"net"
	"strings"

	"cnal/internal/netstack"
)

// 旁路网卡认证态观察（2026-11-19 多网卡实证）：keeper 只维护一个绑定源，
// 插在未认证 VLAN 的网线永远不被触碰——Windows 双网卡下联网指示取最差结果，
// 用户看到「无 Internet」黄叹号却不知道是另一张卡没认证。
//
// 纪律：只观察、零凭据（一次绑源 DNS + 一次不跟随跳转的 HTTP），结果只上浮
// 到面板。登录出口仍只有当前绑定源一条——绝不在旁路卡上发凭据。

// siblingVerdict 判定一个非绑定源的认证态（纯函数，便于测试）：
//   - 3xx 且 Location 指向门户 → "unauth"（被门户劫持，未认证）；
//   - 3xx 指向别处（如百度 http→https）→ "authed"（正常跳转）；
//   - 2xx + 门户指纹 → "unauth"（200 型劫持页）；2xx 普通内容 → "authed"；
//   - 请求没发出去 / 其它状态码 → "unknown"（不误报）。
func siblingVerdict(status int, location, body, portalHost string, hijackMarks []string) string {
	switch status {
	case 301, 302, 303, 307, 308:
		if portalHost != "" && strings.Contains(location, portalHost) {
			return "unauth"
		}
		return "authed"
	case 0:
		return "unknown"
	}
	if status >= 200 && status < 300 {
		if containsAny(body, hijackMarks) {
			return "unauth"
		}
		return "authed"
	}
	return "unknown"
}

// siblingUnauth 探测候选源里「已连接但未认证」的（当前绑定源除外）。
// 最多探 2 张旁路卡：候选枚举异常时不放大成请求风暴；无候选/全失败返回空。
// 观察地址用大众站（与 L2 同源选择）：未认证 VLAN 的白名单只放行锚点，
// 大众站恰好是认证态的分水岭（2026-11-19 实测：锚点 204 但百度 302 劫持）。
func (p *plumbing) siblingUnauth(cfg daemonConfig) []string {
	var cur net.IP
	if p.currentV4 != nil { // 测试构造的 plumbing 可能没接线：不探测、不 panic
		cur = p.currentV4()
	}
	var out []string
	probed := 0
	for _, src := range netstack.CandidateSources(cfg.CampusDNS) {
		if cur != nil && src.Equal(cur) {
			continue
		}
		if probed >= 2 {
			break
		}
		probed++
		if p.siblingAuthState(src, cfg) == "unauth" {
			out = append(out, src.String())
		}
	}
	return out
}

// siblingAuthState 对单张旁路卡跑一次认证态观察。DNS 先走绑源校园 DNS；
// 未认证 VLAN 上它常不回应（实测 2026-11-19），或应答里先给 v6——都回退
// 系统解析的 ip4（观察无凭据；fake-ip 会被 ResolveSystem 过滤）。v6 旁路
// 不做认证态观察：门户 v4 会话是主线，v6 不可达不代表未认证。
func (p *plumbing) siblingAuthState(src net.IP, cfg daemonConfig) string {
	const probeHost = "www.baidu.com"
	ip, err := netstack.ResolveBound(src, cfg.CampusDNS, probeHost, cfg.ProbeTimeout)
	if err != nil || ip.To4() == nil {
		ip, err = netstack.ResolveSystem(probeHost)
		if err != nil {
			return "unknown"
		}
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return "unknown"
	}
	c := netstack.New(src, cfg.ProbeTimeout)
	pin := net.JoinHostPort(ip4.String(), "80")
	st, hdr, body, err := c.GetStatusPinnedHeaders("http://"+probeHost+"/", nil, pin)
	if err != nil {
		return "unknown"
	}
	loc := ""
	if hdr != nil {
		loc = hdr.Get("Location")
	}
	return siblingVerdict(st, loc, body, p.portalHost, cfg.HijackMarks)
}
