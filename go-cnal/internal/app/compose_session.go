package app

import (
	"errors"
	"fmt"
	"net"

	"cnal/internal/keeper"
	"cnal/internal/netstack"
	"cnal/internal/protocol"
)

// 会话与栈操作（从 compose_portal.go 拆出——单文件守 2 万字节提交闸，tidy §3）。
// 职责：问两栈归属（online_check）、按栈注销、切账号的会话搬移。
// 装配（物理源/客户端/登录动作）仍在 compose_portal.go，这里只做「问与改」。

// onlineV4Bound 只走绑源的 online_check：自动会话判死的真值必须与服务出口
// 同路。绑源解析门户失败即返回错误（未知），不转系统栈——系统栈只做用户
// 路径读数，不参与认证判定（2026-11-18：「A 报状态、B 问会话」即事故形态）。
func (p *plumbing) onlineV4Bound(cfg daemonConfig) (protocol.OnlineState, error) {
	var src net.IP
	if p.login != nil {
		src = p.login.Source()
	}
	ip, err := p.pt.lookupForSource(src)
	if ip == nil {
		return protocol.OnlineState{}, fmt.Errorf("绑源解析门户失败: %w", err)
	}
	pin := net.JoinHostPort(ip.String(), p.portalPort)
	client := p.login.CloneForSource(src)
	raw, err := client.PostPinned(cfg.Server+cfg.LoginPath,
		protocol.OpForm(protocol.OpOnlineCheck, ""),
		map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
			"User-Agent":   cfg.UserAgent,
		}, pin)
	if err != nil {
		return protocol.OnlineState{}, err
	}
	return protocol.ParseOnlineCheck(raw)
}

// onlineV4 只读问「v4 栈挂在谁名下」。走门户登录端点的选路（绑源优先、
// 失败转系统栈），不带凭据。存在的理由：登录只搬得动 v4，v6 的归属得
// 单独问（见 protocol.OnlineState）。
//
// 注意：这是**手动操作**（切账号/归属核对）用的自动选路；自动守护的会话
// 判死走 onlineV4Bound，不得回退系统栈。
func (p *plumbing) onlineV4(cfg daemonConfig) (protocol.OnlineState, error) {
	r := p.routeLogin()
	if r.client == nil {
		return protocol.OnlineState{}, errors.New("门户解析失败（疑似不在校园网）")
	}
	raw, err := r.client.PostPinned(cfg.Server+cfg.LoginPath,
		protocol.OpForm(protocol.OpOnlineCheck, ""),
		map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
			"User-Agent":   cfg.UserAgent,
		}, r.pin)
	if err != nil {
		return protocol.OnlineState{}, err
	}
	return protocol.ParseOnlineCheck(raw)
}

// onlineV6 只读问「v6 栈挂在谁名下」：绑 v6 源走门户 v6 入口，与 ipv4or6online 同路
// （2026-11-09 实测同一入口对 online_check 也作答）。
func (p *plumbing) onlineV6(cfg daemonConfig) (protocol.OnlineState, error) {
	return p.onlineV6For(cfg, v6Source(p.currentV6))
}

// onlineV6For 指定本地 v6 源核对归属（同接口的其它公网 v6 是独立会话）。
func (p *plumbing) onlineV6For(cfg daemonConfig, src net.IP) (protocol.OnlineState, error) {
	if src == nil {
		return protocol.OnlineState{}, errors.New("无可信 v6 源")
	}
	raw, err := p.peerClient(cfg).PostForm(src, protocol.OpForm(protocol.OpOnlineCheck, ""))
	if err != nil {
		return protocol.OnlineState{}, err
	}
	return protocol.ParseOnlineCheck(raw)
}

// logoutV6 按栈注销 v6（门户 logic_new.js 的 logout 流程同形：先探归属，再带地址注销）。
// 只注销**已在线**的栈——不在线就没什么可释放，少发一次请求。
func (p *plumbing) logoutV6(cfg daemonConfig) error {
	src := v6Source(p.currentV6)
	st, err := p.onlineV6For(cfg, src)
	if err != nil {
		return err
	}
	if !st.Online {
		return nil
	}
	return p.logoutV6For(cfg, src)
}

// logoutV6For 指定本地 v6 源注销（额外地址的会话各注销一次）。
func (p *plumbing) logoutV6For(cfg daemonConfig, src net.IP) error {
	if src == nil {
		return errors.New("无可信 v6 源")
	}
	_, err := p.peerClient(cfg).PostForm(src, protocol.OpForm(protocol.OpLogout, src.String()))
	return err
}

// logoutV4 按栈注销 v4（门户自己的 logout 流程同形：纯 v4 请求不带 ipv4or6，
// 服务端按请求源地址认栈）。不做「先读归属再决定」——读失败不能成为跳过注销
// 的理由，那正是 2026-11-10 流量用完后切账号失效的路径。注销是否真生效由
// 切换后的 inspectStacks 用门户读数验收，不靠本函数自报。
func (p *plumbing) logoutV4(cfg daemonConfig) error {
	r := p.routeLogin()
	if r.client == nil {
		return errNoRoute
	}
	_, err := r.client.PostPinned(cfg.Server+cfg.LoginPath,
		protocol.OpForm(protocol.OpLogout, ""),
		map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
			"User-Agent":   cfg.UserAgent,
		}, r.pin)
	return err
}

// inspectStacks 分别报两栈归属：v4 承重、v6 补充。一侧读不到只记在该侧，
// 不把另一侧的结论一起吞掉（2026-11-10 事故：v6 不可观测掩盖了 v4 仍归旧账号）。
// 双栈未开时不查 v6——那不是故障，也不该出现在切换回报里。
func (p *plumbing) inspectStacks(cfg daemonConfig) stackOwners {
	var o stackOwners
	if v4, err := p.onlineV4(cfg); err != nil {
		o.V4Err = err
	} else {
		o.V4Known, o.V4User = true, v4.Username
	}
	if !cfg.DualStack {
		return o
	}
	if v6, err := p.onlineV6(cfg); err != nil {
		o.V6Err = err
	} else {
		o.V6Known, o.V6User = true, v6.Username
	}
	return o
}

// releaseV6All 注销主 v6 栈与同接口其它公网 v6 会话（Windows 隐私临时地址
// 是独立门户会话，只搬主地址会留下「IPv4 换了、IPv6 仍是原账号」）。
// 主地址错误放 Err；额外地址逐项带 WasOnline/ReleaseErr。
func (p *plumbing) releaseV6All(cfg daemonConfig) v6Release {
	var out v6Release
	src := v6Source(p.currentV6)
	if src == nil {
		out.Err = errors.New("无可信 v6 源")
		return out
	}
	for _, addr := range netstack.V6Siblings(src) {
		st, err := p.onlineV6For(cfg, addr)
		if err != nil {
			out.Extras = append(out.Extras, v6Session{Addr: addr, ReleaseErr: err})
			continue
		}
		if !st.Online {
			out.Extras = append(out.Extras, v6Session{Addr: addr})
			continue
		}
		// 已经归新账号的地址不动：切换是「搬会话」，不是把新账号的会话也
		// 注销重登（同一账号反复登录既多余又撞门户限频）。
		if st.Username != "" && st.Username == iniUsername() {
			continue
		}
		if err := p.logoutV6For(cfg, addr); err != nil {
			out.Extras = append(out.Extras, v6Session{Addr: addr, WasOnline: true, ReleaseErr: err})
			continue
		}
		out.Extras = append(out.Extras, v6Session{Addr: addr, WasOnline: true})
	}
	out.Err = p.logoutV6(cfg) // 主地址：保持「先探归属再注销」原语义
	return out
}

// sessionOps 是切账号网络动作的唯一接线点（面板与测试共用同一套）。
// v6 注销只在双栈开时接管：双栈没开时 v6 不归本次切换管。
func (p *plumbing) sessionOps(cfg daemonConfig) sessionOps {
	return sessionOps{
		releaseV6: func() v6Release {
			if !cfg.DualStack {
				return v6Release{}
			}
			return p.releaseV6All(cfg)
		},
		releaseV4:    func() error { return p.logoutV4(cfg) },
		authenticate: func() (keeper.Outcome, error) { return p.action.Do() },
		bindV6: func(addr net.IP) error {
			// 校园 v6 无 NAT：本地地址就是门户要写的 ipv4or6（对端握手实测
			// 原样返回，省一次每地址往返）。DoBind 内部仍走完整登录判定。
			if _, err := p.action.DoBind(addr.String()); err != nil {
				return err
			}
			// 登录被答「已在线」不等于该地址真绑上了（2026-11-10 教训）。
			// 必须回读门户：地址在线且账号是新账号才算成功。
			st, err := p.onlineV6For(cfg, addr)
			if err != nil {
				return fmt.Errorf("绑定后核对失败: %w", err)
			}
			if !st.Online {
				return errors.New("门户未建立该地址的会话")
			}
			if st.Username != iniUsername() {
				return fmt.Errorf("门户仍显示该地址归 %s", st.Username)
			}
			return nil
		},
		inspect: func() stackOwners { return p.inspectStacks(cfg) },
	}
}
