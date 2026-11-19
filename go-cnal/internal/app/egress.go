package app

import (
	"fmt"
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
	case strings.Contains(s, "bind:"),
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

// probeAnchor 对给定客户端跑一遍公网锚点（物理栈/系统栈共用同一套判定）：
// 204/非劫持页 = 通；302/200 型门户页/连接失败 = 不通，并带原因。
// 物理栈客户端由调用方负责绑源；系统栈客户端跟浏览器同路。
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
		st, body, err := c.GetStatus("http://"+ip+cfg.ProbePath,
			map[string]string{"Host": cfg.ProbeHost})
		if err != nil {
			kind, detail := classifyL1Err(err)
			note(l1Outcome{Kind: kind, Detail: detail})
			continue // status 0：被劫持或不可达，试下一锚点
		}
		switch st {
		case 301, 302, 307, 308:
			note(l1Outcome{Kind: l1Hijacked, Detail: fmt.Sprintf("%s 返回 %d（劫持跳转）", ip, st)})
			continue // 劫持跳转
		}
		if st == 200 && containsAny(body, cfg.HijackMarks) {
			note(l1Outcome{Kind: l1Hijacked, Detail: fmt.Sprintf("%s 返回 200 型门户劫持页", ip)})
			continue // 200 型门户劫持页（JS 挑战）不是真实互联网
		}
		return l1Outcome{OK: true}
	}
	return last
}
