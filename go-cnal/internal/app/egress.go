package app

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"cnal/internal/keeper"
	"cnal/internal/netstack"
)

// L1 失败分类：只做证据，不改变红/绿判定。2026-11-18 三连登录实证——
// ProbeOnline 原来只返回 bool，把 bind 失效、DNS 超时、劫持、超时全吞成
// 「红」，内核只能把本地绑定失效当成会话丢失。分类后由 egressHealth 决定
// 「能不能登录」，而不是让登录动作去猜。
const (
	l1OK          = ""
	l1BindInvalid = "bind_invalid" // 本地源地址已失效（今天 17:43:15/17:56:40 原文）
	l1Timeout     = "timeout"
	l1Unreachable = "unreachable"
	l1Hijacked    = "hijacked"    // 302/劫持页：绑定有效，是会话/门户状态问题
	l1AnchorDown  = "anchor_down" // 其它连接错误
)

type l1Outcome struct {
	OK     bool
	Kind   string
	Detail string
}

func (p *plumbing) setL1(o l1Outcome) {
	p.l1Mu.Lock()
	p.l1 = o
	p.l1Mu.Unlock()
}

func (p *plumbing) l1Outcome() l1Outcome {
	p.l1Mu.Lock()
	defer p.l1Mu.Unlock()
	return p.l1
}

func (p *plumbing) routeInfo() (path, why string) {
	p.routeMu.Lock()
	defer p.routeMu.Unlock()
	return p.lastRoute, p.lastWhy
}

// egressHealth 把「L1 失败分类 + 门户选路结果」翻成出口健康读数。
//
// 判据：
//   - bind 失效 → binding_invalid（本地地址没了，登录发不出去）；
//   - 绑源到不了门户、已回退系统栈 → portal_unreachable（换个源，不要用
//     系统栈发凭据）；
//   - net_mode=system 是用户显式选择，系统栈路径不算失效；
//   - 其余按健康处理（正常路径零影响）。
func (p *plumbing) egressHealth() keeper.EgressHealth {
	src := "（无）"
	if p.currentV4 != nil {
		src = srcStr(p.currentV4())
	}
	out := p.l1Outcome()
	if out.Kind == l1BindInvalid {
		return keeper.EgressHealth{
			Valid:  false,
			Reason: keeper.EgressBindingInvalid,
			Detail: out.Detail,
			Source: src,
		}
	}
	path, why := p.routeInfo()
	if p.netMode != NetModeSystem && path == routeSystem {
		return keeper.EgressHealth{
			Valid:  false,
			Reason: keeper.EgressPortalUnreachable,
			Detail: why,
			Source: src,
		}
	}
	return keeper.EgressHealth{Valid: true, Source: src}
}

// classifyL1Err 按错误文本分类 L1 失败。Go 的 net 错误没有跨平台稳定类型，
// 这里只认实测过的几种文本；认不出就归 anchor_down，绝不猜成 bind 失效
// （误判成绑定失效会阻止登录，方向是 fail-closed，但要把原文留在 detail）。
func classifyL1Err(err error) (kind, detail string) {
	if err == nil {
		return l1AnchorDown, "未知错误"
	}
	s := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, netstack.ErrInterfaceBind),
		strings.Contains(s, "bind:"),
		strings.Contains(s, "not valid in its context"),
		strings.Contains(s, "cannot assign requested address"):
		return l1BindInvalid, err.Error()
	case strings.Contains(s, "i/o timeout"),
		strings.Contains(s, "deadline exceeded"):
		return l1Timeout, err.Error()
	case strings.Contains(s, "no route to host"),
		strings.Contains(s, "network is unreachable"),
		strings.Contains(s, "connectex"):
		return l1Unreachable, err.Error()
	default:
		return l1AnchorDown, err.Error()
	}
}

// localAddrs 本机地址来源（测试可替换）。只在 bind 失效时读一次。
var localAddrs = net.InterfaceAddrs

// bindHint 给 bind 失效的原始错误补一句现场判断（2026-11-25 过夜轮 A1 §9）：
//   - 源地址仍在网卡上 → 绑定被拒，不是源失效；
//   - 源地址不在、但本机还有别的非环回 IPv4 → 接口重建/换网；
//   - 本机一个非环回 IPv4 都没有 → 根本没网（WiFi 与数据都被系统关了），
//     重选与重注入都无源可绑。
// 只写观测到的事实，不写猜出来的成因。
func bindHint(src net.IP, addrs []net.Addr) string {
	want := src.To4()
	if want == nil {
		return "" // 无源地址（系统栈/未绑源）：不添话
	}
	present, other := false, false
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := n.IP.To4()
		if ip4 == nil || ip4.IsLoopback() {
			continue
		}
		if ip4.Equal(want) {
			present = true
		} else {
			other = true
		}
	}
	switch {
	case present:
		return "；源地址仍在网卡上，绑定被拒"
	case other:
		return "；源地址已从网卡消失，但本机仍有其它 IPv4"
	default:
		return "；本机已无任何非环回 IPv4（网卡均未持有地址）"
	}
}

// probeAnchor 对给定客户端跑一遍公网锚点（物理栈/系统栈共用同一套判定）：
// 204/非劫持页 = 通；302/200 型门户页/连接失败 = 不通，并带原因。
// 物理栈客户端由调用方负责绑源；系统栈客户端跟浏览器同路。
//
// Host 变体序与 detect.go probeHostVariants 同语义（2026-11-21 实测事故）：
// 先不带 Host——校园把 connect.rom.miui.com 这类探测域名白名单放行，带
// Host 的未认证请求拿真 404/204，旧实现由此假绿 3.5 小时不重登。无 Host
// （按目标 IP）绕开按域名放行的白名单；配置 Host 只作第二变体（给按域名
// 劫持的校园留路）。任一变体看到劫持即判红——白名单只救得了「能通」，
// 救不回「本来就不通」。
func probeAnchor(c *netstack.Client, cfg daemonConfig) l1Outcome {
	if c == nil {
		return l1Outcome{Kind: l1AnchorDown, Detail: "探针未接线"}
	}
	last := l1Outcome{Kind: l1AnchorDown, Detail: "锚点均无响应"}
	note := func(o l1Outcome) {
		// bind 失效是最强证据：同一源上后续锚点的超时不得把它覆盖掉。
		if last.Kind == l1BindInvalid && o.Kind != l1BindInvalid {
			return
		}
		last = o
	}
	for _, ip := range cfg.ProbeIPs {
		for _, host := range probeHostVariants(cfg.ProbeHost) {
			hdrs := map[string]string{}
			label := ip
			if host != "" {
				hdrs["Host"] = host
				label = ip + " Host=" + host
			}
			st, body, err := c.GetStatus("http://"+ip+cfg.ProbePath, hdrs)
			if err != nil {
				kind, detail := classifyL1Err(err)
				if kind == l1BindInvalid {
					// 相机现场：源地址还在不在、本机还有没有别的 IPv4。
					if addrs, aerr := localAddrs(); aerr == nil {
						detail += bindHint(c.Source(), addrs)
					}
				}
				note(l1Outcome{Kind: kind, Detail: detail})
				// err（RST/丢包/超时）后不再问带 Host 的第二变体（trouble-maker
				// P1-4）：白名单形态拿 404 会把红覆盖成假绿——「劫持终局」只封住了
				// 302 形态，err 形态的白名单残余洞必须一并关死；同步缝的拍长也不
				// 得翻倍（3 锚点最坏 3→6 次，重蹈 11-19 同步探测拖心跳）。
				break
			}
			if isRedirect(st) {
				note(l1Outcome{Kind: l1Hijacked, Detail: fmt.Sprintf("%s 返回 %d（劫持跳转）", label, st)})
				// 本 IP 已见劫持即终局：不再问第二变体——白名单的「第二意见」
				// 只救得了「本来能通」，救不回「本来就不通」（2026-11-21 实测：
				// 劫持后继续问白名单 Host，404 把红覆盖成假绿）。跨 IP 仍试，
				// 单锚点被墙不该连坐其它锚点。
				break
			}
			if st == 200 && containsAny(body, cfg.HijackMarks) {
				note(l1Outcome{Kind: l1Hijacked, Detail: fmt.Sprintf("%s 返回 200 型门户劫持页", label)})
				break // 同上：劫持终局，不问第二变体
			}
			return l1Outcome{OK: true}
		}
	}
	return last
}
