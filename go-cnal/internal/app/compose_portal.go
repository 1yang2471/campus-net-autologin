package app

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"sync"
	"time"

	"cnal/internal/keeper"
	"cnal/internal/netstack"
	"cnal/internal/protocol"
)

// plumbing 是组合根共享的运行期装配物：物理源（含重发现改绑）、门户真实 IP
// 解析、各用途的绑源 HTTP 客户端、登录动作与速率闸门。拆文件只为可读性——
// 仍是同一份接线，闭包共享同一组指针/共享引用。
type plumbing struct {
	portalHost   string
	portalPort   string
	pt           *portalTarget
	portal       *netstack.Client // 门户页/flux（超时比探测宽）
	systemPortal *netstack.Client // 系统栈同用途（不绑源，跟 TUN）
	probe        *netstack.Client // L1 锚点探测
	login        *netstack.Client // 登录 POST（超时独立）
	systemLogin  *netstack.Client // 系统栈登录 POST
	tlsProbe     *netstack.Client // L2 出口观察
	currentV4    func() net.IP
	currentV6    func() net.IP
	// netMode 门户选路模式（auto/bound/system，见 config.go）；运行期只读。
	netMode string
	// logf 路径日志缝（默认 stdout，安卓注入状态日志）：只在路径变化时调用。
	logf func(format string, args ...any)
	// routeMu/lastRoute/lastWhy 路径日志去重（Tick 与回路 API goroutine 并发选路）。
	routeMu   sync.Mutex
	lastRoute string
	lastWhy   string
	// handshakeOnce 对端握手唯一构造点（登录 Do 与 v6 四态观察共用）。
	// keeper 侧 peerValue 不导出，守护缝从这里取。
	handshakeOnce func() (string, error)
	action        *keeper.LoginAction
	rl            *keeper.RateLimit
	rd            *rediscoverer // bind_ip 手工钉源 / 安卓注入源时为 nil
	// picker 门户可达性选源器（自动发现路径才有；钉源/注入时为 nil）。
	picker *netstack.SourcePicker
}

// portalProbe 探测「从某物理源能不能到达门户」。默认走真探测（netstack 包）；
// 单元测试同包替换它——测试不该依赖网络。
var portalProbe = netstack.ProbePortal

// newPlumbing 装配「物理源 → 门户钉 IP → 各用途客户端 → 登录动作」。
// 返回错误即启动失败（bind_ip 非法 / 无物理源 / server 非法 URL）。
func newPlumbing(cfg daemonConfig, user, pass string) (*plumbing, error) {
	// 门户主机名/端口先定：物理源探测要用它们（2026-11-11 设计 §3）。
	u, err := url.Parse(cfg.Server)
	if err != nil {
		return nil, fmt.Errorf("server 非法 URL: %w", err)
	}
	portalHost := u.Hostname()
	portalPort := u.Port()
	if portalPort == "" {
		if u.Scheme == "https" {
			portalPort = "443"
		} else {
			portalPort = "80"
		}
	}

	// 物理源 v4：三条来源，优先级从高到低——
	//  1) 安卓注入（Kotlin 按 LinkProperties 选定，不探测）；
	//  2) bind_ip 手工钉（用户显式意图，不探测）；
	//  3) 自动发现：**按门户可达性探测**选（2026-11-11 设计 §3）。
	//
	// 为什么不能按路由表选：路由表只回答「去任意公网走哪条路」，而校园认证
	// 网关**只在校园网段可达**——插了网线后默认路由 metric 偏向以太网，恰好
	// 选到够不着网关的那条（真机实测：1.1.1.2:443 从 WLAN 源 0.03s 通，
	// 从以太网源超时）。
	var (
		picker *netstack.SourcePicker
		v4     net.IP
		v6     = netstack.PhysicalSourceV6()
	)
	inj4, inj6, injected := physicalSources()
	switch {
	case injected:
		v4, v6 = inj4, inj6
	case cfg.BindIP != "":
		// bind_ip 逃生口（Python 守护同款）：手工钉死 v4 物理源，跳过自动发现；
		// 非法值直接拒绝启动（配置错误要看得见，不静默回退自动发现）。
		ip := net.ParseIP(cfg.BindIP)
		if ip == nil || !netstack.UsableSource(ip) {
			return nil, fmt.Errorf("bind_ip %q 非法（不是可用物理 v4 源形态）", cfg.BindIP)
		}
		v4 = ip
	default:
		picker = netstack.NewSourcePicker(
			func() []net.IP { return netstack.CandidateSources(cfg.CampusDNS) },
			func(src net.IP) error {
				return portalProbe(src, cfg.CampusDNS, portalHost, portalPort,
					maxDuration(cfg.ProbeTimeout, 3*time.Second), cfg.SSLVerify)
			})
		picked, perr := picker.Refresh()
		if picked == nil {
			// 一个候选都够不着门户：仍用路由偏好源启动，让守护如实报「门户不可达」
			//（拒绝启动只会给用户一个没头没脑的弹窗，说不清是哪一环不通）。
			picked = netstack.DiscoverSource(cfg.CampusDNS)
			printSource("v4 源: %s（探测全败：%v）\n", srcStr(picked), perr)
		} else {
			printSource("v4 源: %s（%s）\n", srcStr(picked), picker.Summary())
		}
		v4 = picked
	}
	if v4 == nil {
		return nil, errors.New("无可用物理 v4 源（TUN 占满？）——物理栈纪律下不触网")
	}
	printSource("v6 源: %s\n", srcStr(v6))

	// 当前 v4/v6 物理源（共享引用）：重发现改绑 / v6 临时地址轮换后，所有
	// 闭包都读到这里的新值，不再各自捕获启动时的旧值（2026-11-08 发布审查
	// P1；安卓 A5 的「轮换后重绑 ≤3 次收敛」靠 currentV6 热更新）。
	var v4Mu, v6Mu sync.RWMutex
	currentV4 := func() net.IP {
		v4Mu.RLock()
		defer v4Mu.RUnlock()
		return v4
	}
	setV4 := func(ip net.IP) {
		v4Mu.Lock()
		defer v4Mu.Unlock()
		v4 = ip
	}
	// 注入模式下每次读全局注入值：Kotlin 重新 SetPhysicalSources（v6 临时
	// 地址轮换）后，Snapshot 与握手立刻看到新地址，不必重建 plumbing。
	currentV6 := func() net.IP {
		if _, inj, ok := physicalSources(); ok {
			return inj
		}
		v6Mu.RLock()
		defer v6Mu.RUnlock()
		return v6
	}

	// 门户真实 IP（Python _portal_target 同源）：系统 DNS 在 TUN 下给 fake-ip
	// （2026-11-08 真机实证 a.stu.edu.cn → 28.0.0.17），直接用域名会让所有
	// 绑源请求超时。用校园 DNS 绑源解析 + 钉 IP 直连，TTL 缓存免每拍打 DNS。
	pt := newPortalTarget(func() (net.IP, error) {
		return netstack.ResolveBound(currentV4(), cfg.CampusDNS, portalHost, cfg.ProbeTimeout)
	}, 60*time.Second)

	portal := netstack.New(v4, maxDuration(2*cfg.ProbeTimeout, 6*time.Second))
	if !cfg.SSLVerify {
		portal = portal.InsecureTLS()
	}
	// 系统栈客户端（路径 B）：不绑源、不钉 IP，与浏览器同路。仅在 TUN 接管
	// 导致绑源解析失败时由选路启用；超时与对应物理栈客户端一致。
	systemPortal := netstack.NewSystem(maxDuration(2*cfg.ProbeTimeout, 6*time.Second))
	if !cfg.SSLVerify {
		systemPortal = systemPortal.InsecureTLS()
	}
	probe := netstack.New(v4, cfg.ProbeTimeout)
	// handshakeOnce 对端握手的唯一构造点（登录取值 + v6 四态观察共用，防两份
	// 漂移）。每次现场构造端点：v6 临时地址轮换后缓存会绑到失效地址；
	// 无可信 v6 源/未配置对端 → error → 降级纯 v4（absent ≠ fail）。
	handshakeOnce := func() (string, error) {
		src := v6Source(currentV6)
		if src == nil {
			return "", errors.New("无可信 v6 源")
		}
		if cfg.V6Peer == "" || cfg.PeerOpr == "" {
			return "", errors.New("未配置对端握手（v6_peer/peer_opr）")
		}
		peer := protocol.NewPeer(peerURL(cfg.V6Peer)+cfg.PeerPath,
			maxDuration(cfg.ProbeTimeout, 5*time.Second))
		return peer.Query(src, cfg.PeerOpr)
	}
	// loginClient 登录专用客户端：超时独立于探测（RADIUS 后端 3-6s，复用
	// probe_timeout 会「向导测过、守护老超时」——Python P1-7 同源）。
	loginClient := netstack.New(v4, cfg.LoginTimeout)
	if !cfg.SSLVerify {
		loginClient = loginClient.InsecureTLS()
	}
	systemLogin := netstack.NewSystem(cfg.LoginTimeout)
	if !cfg.SSLVerify {
		systemLogin = systemLogin.InsecureTLS()
	}
	// p 先建壳再填 action：Deliver 闭包要引用 p 本身，不能在 p 的复合字面量里
	// 写（短变量声明的作用域从语句结束才开始）。
	p := &plumbing{
		portalHost: portalHost, portalPort: portalPort, pt: pt,
		portal: portal, systemPortal: systemPortal,
		probe: probe,
		login: loginClient, systemLogin: systemLogin,
		netMode: cfg.NetMode, logf: printSource,
		currentV4: currentV4, currentV6: currentV6, handshakeOnce: handshakeOnce,
		rl: keeper.NewRateLimit(8, time.Hour, 15*time.Minute, time.Now),
	}
	p.action = &keeper.LoginAction{
		User: user, Pass: pass, DualStack: cfg.DualStack,
		Marks: cfg.Response, Form: cfg.LoginForm,
		Handshake: handshakeOnce,
		Deliver: func(form map[string]string) (string, error) {
			return deliverPinned(cfg, p, form)
		},
	}

	// 物理源重发现：portal_down 拍按 portal_down_wait 节流重选源，变了才改绑
	// （网络切换/休眠唤醒后旧源失效，Python P2-8 同源需求）。
	// 只在**自动发现**路径装配：bind_ip 手工钉源时禁用（用户显式意图优先，
	// 不抢方向盘）；安卓注入源同样不重发现（Kotlin 侧已选定）。
	// 重发现 = 按门户可达性重新探测（不是盲选路由表）——拓扑变了才知道
	// 哪条链路还够得着认证网关。
	// tlsProbe 提前声明：重发现必须同步改绑它（否则 L2 探测永远走旧源）。
	tlsProbe := netstack.New(currentV4(), cfg.ProbeTimeout)
	p.tlsProbe = tlsProbe
	if picker != nil {
		p.picker = picker
		rd := newRediscoverer(cfg.PortalDownWait, currentV4(),
			func() net.IP {
				ip, _ := picker.Refresh()
				return ip
			},
			func(ip net.IP) {
				printSource("物理源切换: %s → %s（%s）\n", srcStr(currentV4()), srcStr(ip), picker.Summary())
				setV4(ip) // 共享引用：DNS 解析/L2/门户/v4 源显示全同步
				p.portal.SetSource(ip)
				p.probe.SetSource(ip)
				p.login.SetSource(ip)
				p.tlsProbe.SetSource(ip)
				p.pt.invalidate() // 换网后门户 IP 可能变，强制下次重新解析
			})
		p.rd = rd
	}
	return p, nil
}

// deliverPinned 门户登录表单出口（守护主线唯一凭据出口）。双路径：绑源
// 解析成功则钉 IP 直连（路径 A）；auto 下解析失败转系统栈（路径 B），
// bound 手动模式下解析失败仍 fail-closed。放在方法外是为了在 plumbing
// 构造过程中就能接上闭包（p 尚未成型）。
func deliverPinned(cfg daemonConfig, p *plumbing, form map[string]string) (string, error) {
	r := p.routeLogin()
	if r.client == nil {
		return "", errNoRoute
	}
	return r.client.PostPinned(cfg.Server+cfg.LoginPath, form,
		map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
			"User-Agent":   cfg.UserAgent, // 门户 WAF 校事实（Python _headers 同源）
			"Referer":      cfg.Server + cfg.LoginPage,
		}, r.pin)
}

// fluxQuery 查流量：面板缝与守护缝同一实现（原先是两段逐字重复的闭包）。
// 失败返回 error（不造假数据）；未知不判死（G6②语义）。
func (p *plumbing) fluxQuery(cfg daemonConfig) (map[string]string, error) {
	r := p.routePortal()
	if r.client == nil {
		return nil, errors.New("门户解析失败（疑似不在校园网）")
	}
	body, err := r.client.PostPinned(cfg.Server+cfg.FluxPath, nil,
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, r.pin)
	if err != nil {
		return nil, err
	}
	return parseFluxTable(body), nil
}

// v6Source 当前 v6 源：注入优先（安卓临时地址轮换由 Kotlin 重新注入），
// Windows 未注入时每次现取枚举；取不到回落 fallback（启动时的快照）。
func v6Source(fallback func() net.IP) net.IP {
	if _, inj, ok := physicalSources(); ok {
		return inj
	}
	if ip := netstack.PhysicalSourceV6(); ip != nil {
		return ip
	}
	return fallback()
}

// peerClient 按栈 op 用的 v6 入口（与对端握手同一 URL、同一套校验与超时）。
func (p *plumbing) peerClient(cfg daemonConfig) *protocol.Peer {
	return protocol.NewPeer(peerURL(cfg.V6Peer)+cfg.PeerPath,
		maxDuration(cfg.ProbeTimeout, 5*time.Second))
}

// onlineV4 只读问「v4 栈挂在谁名下」。走门户登录端点的选路（绑源优先、
// 失败转系统栈），不带凭据。存在的理由：登录只搬得动 v4，v6 的归属得
// 单独问（见 protocol.OnlineState）。
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
