package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sync"
	"time"

	"cnal/internal/clashapi"
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
	systemProbe  *netstack.Client // 系统栈锚点（用户路径读数，只展示）
	login        *netstack.Client // 登录 POST（超时独立）
	systemLogin  *netstack.Client // 系统栈登录 POST
	tlsProbe     *netstack.Client // L2 出口观察
	currentV4    func() net.IP
	setV4        func(net.IP) // 换绑共享 v4 快照（applyV4 与 rd 共用）
	currentV6    func() net.IP
	// netMode 门户选路模式（auto/bound/system，见 config.go）；运行期只读。
	netMode string
	// bindMode 物理 socket 绑定模式（interface/source_only）；
	// strictInterfaceBind 控制 net_mode=auto 是否允许绑源失败时回退系统栈。
	bindMode            netstack.BindMode
	strictInterfaceBind bool
	clash               *clashapi.Client
	clashMu             sync.Mutex
	clashFlushing       bool
	clashPending        bool
	bindMu              sync.Mutex
	lastBind            netstack.BindingInfo
	// logf 路径日志缝（默认 stdout，安卓注入状态日志）：只在路径变化时调用。
	logf func(format string, args ...any)
	// routeMu/lastRoute/lastWhy 路径日志去重（Tick 与回路 API goroutine 并发选路）。
	routeMu   sync.Mutex
	lastRoute string
	lastWhy   string
	// l1Mu/l1 最近一拍 L1 探测结果（含失败分类）：出口健康门槛的证据源。
	// Tick goroutine 写、Tick goroutine 读；手动重连只读 egressHealth，也走锁。
	l1Mu sync.Mutex
	l1   l1Outcome
	// handshakeOnce 对端握手唯一构造点（登录 Do 与 v6 四态观察共用）。
	// keeper 侧 peerValue 不导出，守护缝从这里取。
	handshakeOnce func() (string, error)
	action        *keeper.LoginAction
	rl            *keeper.RateLimit
	rd            *rediscoverer // bind_ip 手工钉源 / 安卓注入源时为 nil
	// bindable 绑源自检缝（2026-11-23 过夜缺陷）：DNS 结果在 TTL 缓存里、
	// 但网卡 IP 被 ROM 摘掉时 dialer 仍会 bind 失败；选路前自检一次，失效
	// 等同源失效处理（清门户缓存 + 触发 rd + 转系统栈）。nil = 旧语义（不检）。
	bindable func(net.IP) bool
	// sourceStale 注入模式专用：绑源失效时通知 Kotlin 重新注入物理源（D-09）。
	// nil = 无消费者（Windows 自动发现路径 / 测试未设置）。
	// staleMu 保护 sourceStaleFired 的读改写：chooseRoute 被 Tick 与回路 API
	// 两个 goroutine 并发调用（见 routeMu 注释），重绑又由 Kotlin 后台线程触发，
	// 裸 bool 会双通知（2026-11-24 trouble-maker 阻断项）。回调在锁外执行。
	sourceStale      func()
	staleMu          sync.Mutex
	sourceStaleFired bool
	// picker 门户可达性选源器（自动发现路径才有；钉源/注入时为 nil）。
	picker *netstack.SourcePicker
}

// portalProbe 探测「从某物理源能不能到达门户」。默认走真探测（netstack 包）；
// 单元测试同包替换它——测试不该依赖网络。
var portalProbeWithBindMode = netstack.ProbePortalWithBindMode

// newPlumbing 装配「物理源 → 门户钉 IP → 各用途客户端 → 登录动作」。
// 返回错误即启动失败（bind_ip 非法 / 无物理源 / server 非法 URL）。
func newPlumbing(cfg daemonConfig, user, pass string) (*plumbing, error) {
	// 门户主机名/端口先定：物理源探测要用它们（2026-11-11 设计 §3）。
	u, err := url.Parse(cfg.Server)
	if err != nil {
		return nil, fmt.Errorf("server 非法 URL: %w", err)
	}
	clash, err := clashapi.New(cfg.ClashAPIURL, cfg.ClashAPISecret)
	if err != nil {
		msg := fmt.Sprintf("Clash API 配置错误，已禁用联动（不影响认证）: %v", err)
		printSource("%s\n", msg)
		_ = AppendEvent(LogFilePath(), EventClock(), msg)
		clash = nil
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
	bindMode := netstack.ModeForStrict(cfg.StrictInterfaceBind)
	if bindMode == netstack.BindModeSourceOnly && netstack.DefaultBindMode() == netstack.BindModeInterface {
		printSource("物理栈降级: strict_interface_bind=false，仅绑源 IP（不保证绕开路由型 TUN）\n")
	}
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
				return portalProbeWithBindMode(src, cfg.CampusDNS, portalHost, portalPort,
					maxDuration(cfg.ProbeTimeout, 3*time.Second), cfg.SSLVerify, bindMode)
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
	strictBind := cfg.StrictInterfaceBind && netstack.DefaultBindMode() == netstack.BindModeInterface
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
	var bindSink func(netstack.BindingInfo)
	pt := newPortalTargetForSource(func(src net.IP) (net.IP, error) {
		ip, info, err := netstack.ResolveBoundWithInfo(src, cfg.CampusDNS, portalHost, cfg.ProbeTimeout, bindMode)
		if bindSink != nil {
			bindSink(info)
		}
		return ip, err
	}, 60*time.Second)

	portal := netstack.NewWithBindMode(v4, maxDuration(2*cfg.ProbeTimeout, 6*time.Second), bindMode)
	if !cfg.SSLVerify {
		portal = portal.InsecureTLS()
	}
	// 系统栈客户端（路径 B）：不绑源、不钉 IP，与浏览器同路。仅在 TUN 接管
	// 导致绑源解析失败时由选路启用；超时与对应物理栈客户端一致。
	systemPortal := netstack.NewSystem(maxDuration(2*cfg.ProbeTimeout, 6*time.Second))
	if !cfg.SSLVerify {
		systemPortal = systemPortal.InsecureTLS()
	}
	probe := netstack.NewWithBindMode(v4, cfg.ProbeTimeout, bindMode)
	systemProbe := netstack.NewSystem(cfg.ProbeTimeout)
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
		peer := protocol.NewPeerWithBindMode(peerURL(cfg.V6Peer)+cfg.PeerPath,
			maxDuration(cfg.ProbeTimeout, 5*time.Second), bindMode)
		return peer.Query(src, cfg.PeerOpr)
	}
	// loginClient 登录专用客户端：超时独立于探测（RADIUS 后端 3-6s，复用
	// probe_timeout 会「向导测过、守护老超时」——Python P1-7 同源）。
	loginClient := netstack.NewWithBindMode(v4, cfg.LoginTimeout, bindMode)
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
		probe: probe, systemProbe: systemProbe,
		login: loginClient, systemLogin: systemLogin,
		netMode: cfg.NetMode, bindMode: bindMode, strictInterfaceBind: strictBind, clash: clash, logf: printSource,
		currentV4: currentV4, setV4: setV4, currentV6: currentV6, handshakeOnce: handshakeOnce,
		// 绑源自检：DNS 缓存命中但网卡 IP 被摘掉时（2026-11-23 过夜缺陷），
		// chooseRoute 据此判绑源是否仍可用，失效则清缓存+转系统栈。
		bindable: portal.Bindable,
		rl:       keeper.NewRateLimit(8, time.Hour, 15*time.Minute, time.Now),
	}
	bindSink = p.noteBind
	p.action = &keeper.LoginAction{
		User: user, Pass: pass, DualStack: cfg.DualStack,
		Marks: cfg.Response, Form: cfg.LoginForm,
		Handshake: handshakeOnce,
		Deliver: func(form map[string]string) (string, error) {
			return deliverPinned(cfg, p, form)
		},
		AfterSuccess: p.flushClash,
	}

	// 物理源重发现：portal_down 拍按 portal_down_wait 节流重选源，变了才改绑
	// （网络切换/休眠唤醒后旧源失效，Python P2-8 同源需求）。
	// 只在**自动发现**路径装配：bind_ip 手工钉源时禁用（用户显式意图优先，
	// 不抢方向盘）；安卓注入源同样不重发现（Kotlin 侧已选定）。
	// 重发现 = 按门户可达性重新探测（不是盲选路由表）——拓扑变了才知道
	// 哪条链路还够得着认证网关。
	// tlsProbe 提前声明：重发现必须同步改绑它（否则 L2 探测永远走旧源）。
	tlsProbe := netstack.NewWithBindMode(currentV4(), cfg.ProbeTimeout, bindMode)
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
				p.applyV4(ip)
			})
		p.rd = rd
	} else if _, _, injected := physicalSources(); injected {
		// 注入模式：注册重绑入口 + SourceStale 通知（2026-11-24 自愈）。
		// Kotlin 换网重注入后调 Mobile.RebindSources → activeRebind → 这里重绑；
		// 绑源失效时 sourceStale → 包级 sink → Kotlin 重新取 LinkProperties。
		RegisterInjectedPlumbing(func() error { return p.rebindInjectedSources() })
		p.sourceStale = NotifySourceStale
	}
	return p, nil
}

func (p *plumbing) noteBind(info netstack.BindingInfo) {
	if info.Mode == "" && info.Err == nil {
		return
	}
	p.bindMu.Lock()
	p.lastBind = info
	p.bindMu.Unlock()
}

func (p *plumbing) bindingInfo() netstack.BindingInfo {
	p.bindMu.Lock()
	defer p.bindMu.Unlock()
	return p.lastBind
}

func (p *plumbing) flushClash() {
	if p.clash == nil {
		return
	}
	p.clashMu.Lock()
	if p.clashFlushing {
		p.clashPending = true
		p.clashMu.Unlock()
		return
	}
	p.clashFlushing = true
	p.clashMu.Unlock()
	go p.runClashFlush()
}

func (p *plumbing) runClashFlush() {
	for {
		if err := p.clash.FlushFakeIP(context.Background()); err != nil {
			msg := fmt.Sprintf("Clash fake-ip flush 失败（不影响认证）: %v", err)
			if p.logf != nil {
				p.logf("%s\n", msg)
			}
			_ = AppendEvent(LogFilePath(), EventClock(), msg)
		}
		p.clashMu.Lock()
		if !p.clashPending {
			p.clashFlushing = false
			p.clashMu.Unlock()
			return
		}
		p.clashPending = false
		p.clashMu.Unlock()
	}
}

// applyV4 把新物理 v4 源同步到所有消费点：共享快照（DNS 解析/L2/v4 显示）
// + 四个绑源客户端 + 门户 IP 缓存。重发现（自动路径）与注入重绑（安卓路径）
// 共用这一处实现，避免两条路径漂移（2026-11-24 源失效自愈）。
func (p *plumbing) applyV4(ip net.IP) {
	p.setV4(ip)
	p.portal.SetSource(ip)
	p.probe.SetSource(ip)
	p.login.SetSource(ip)
	p.tlsProbe.SetSource(ip)
	p.pt.invalidate() // 换网后门户 IP 可能变，强制下次重新解析
}

// rebindInjectedSources 安卓注入源变更后重新绑定：Kotlin 换 WiFi 重新注入
// setPhysicalSources 后调用（经 Mobile.RebindSources 导出）。注入模式下没有
// rediscoverer，v4 是启动时捕获的冷快照——不重绑则 currentV4 变了、四个客户端
// 仍从旧 IP 出去，4 小时 bind 失效即此成因（2026-11-24 调研）。
// 非注入模式调用是 no-op（自动路径由 rd 管，不抢方向盘）。
func (p *plumbing) rebindInjectedSources() error {
	inj4, _, injected := physicalSources()
	if !injected || inj4 == nil {
		return nil
	}
	// 复位必须在 Equal 判等之前：Android WiFi 重关联后 DHCP 常给**同一个 IP**，
	// 只是网卡绑定丢了；“重绑被调用”即视为一次恢复尝试完成，允许下次失效再通知。
	// 绑在「IP 变了」上会让同 IP 失效场景永久沉默（2026-11-24 trouble-maker 阻断项）。
	p.staleMu.Lock()
	p.sourceStaleFired = false
	p.staleMu.Unlock()
	if inj4.Equal(p.currentV4()) {
		return nil // 源没变，跳过实际改绑（避免无谓清 DNS 缓存 + 日志抖动）
	}
	printSource("物理源重新注入: %s → %s（注入）\n", srcStr(p.currentV4()), srcStr(inj4))
	p.applyV4(inj4)
	return nil
}

// notifySourceStale 单次通知消费者（D-09）：一次失效只发一次，直到重绑复位。
// 检查+置位在锁内保证原子（并发 chooseRoute 不会双通知），回调在锁外执行。
func (p *plumbing) notifySourceStale() {
	p.staleMu.Lock()
	if p.sourceStale == nil || p.sourceStaleFired {
		p.staleMu.Unlock()
		return
	}
	p.sourceStaleFired = true
	cb := p.sourceStale
	p.staleMu.Unlock()
	cb()
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
	// GET 型登录（Dr.COM/Srun 明文族）：参数走 query、body 空，form 的
	// Content-Type 无意义——netstack 不自动设，这里也不能显式带。
	if cfg.LoginForm.Method == "GET" {
		return r.client.GetPinned(cfg.Server+cfg.LoginPath, form,
			map[string]string{
				"User-Agent": cfg.UserAgent, // 门户 WAF 校事实（Python _headers 同源）
				"Referer":    cfg.Server + cfg.LoginPage,
			}, r.pin)
	}
	// POST/默认：保持既有行为一字不改。
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
	return protocol.NewPeerWithBindMode(peerURL(cfg.V6Peer)+cfg.PeerPath,
		maxDuration(cfg.ProbeTimeout, 5*time.Second), p.bindMode)
}

// 会话与栈操作（online_check 问归属 / 按栈注销 / 切账号搬会话）住在
// compose_session.go——那里是会话语义的归属，也让本文件守住 20KB 提交闸。
