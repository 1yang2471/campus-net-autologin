package app

import (
	"fmt"
	"net"
	"time"

	"cnal/internal/keeper"
	"cnal/internal/netstack"
	"cnal/internal/protocol"
)

// newDaemon 装配守护状态机与它的全部探针/动作缝（L1 锚点、门户可达、
// v6 四态、L2 出口、查流量、登录）。纯接线：阈值与语义都在 keeper 里。
func newDaemon(cfg daemonConfig, p *plumbing) *keeper.Daemon {
	d := keeper.NewDaemon(cfg.FreeWindows)
	d.SetAutoReconnect(cfg.AutoReconnect)                 // ini 初始开关（面板/托盘可热改）
	d.SetResponseMarks(cfg.Response)                      // [response] 词表（启动前）
	d.SetBackoff(cfg.RejectBackoff, cfg.TransientBackoff) // 退避可配（启动前）
	// 流量能力（只读）：零值配置回落内核默认 10 分钟 / 80,90。
	d.SetFluxPollInterval(cfg.FluxPollInterval)
	if cfg.FluxWarnAt > 0 {
		d.SetFluxWarnThresholds(cfg.FluxWarnAt, cfg.FluxCriticalAt)
	}
	d.ProbeOnline = func() bool {
		out := probeAnchor(p.probe, cfg)
		p.setL1(out)
		return out.OK
	}
	// 用户路径读数（系统栈，与浏览器同路）：只展示，不参与判定。
	d.UserPath = func() keeper.PathReading {
		if p.systemProbe == nil {
			return keeper.PathReading{}
		}
		out := probeAnchor(p.systemProbe, cfg)
		return keeper.PathReading{OK: out.OK, Known: true, Detail: out.Detail}
	}
	d.PortalAlive = func() bool {
		// 门户可达是「需要域名解析」的操作：绑源解析成功走绑源钉 IP，
		// auto 下失败转系统栈（TUN 接管时与浏览器同路）；bound 手动模式
		// 解析失败则返回 false（fail-closed，不猜地址）。
		r := p.routePortal()
		if r.client == nil {
			return false
		}
		_, err := r.client.Get(cfg.Server+cfg.LoginPage, nil, r.pin)
		return err == nil
	}
	// 出口健康门槛（2026-11-18 三连登录实证）：本地绑定失效/绑源到不了
	// 门户时，内核不发凭据，先报状态让组合根重选。
	d.EgressHealth = p.egressHealth
	// 物理源重发现只在 bind_ip 未钉源时装配（rd 为 nil 即手工钉源）。
	if p.rd != nil {
		d.OnPortalDown = func() { p.rd.tick(time.Now()) }
		// 绑定失效：先把当前源标降级（避免下一轮又选回刚死的候选），再立即
		// 重选（rd.force 自带 5s 下限）。死源多留一秒都在放大「探测/动作
		// 两套网络观」的窗口。
		d.OnEgressInvalid = func() {
			now := time.Now()
			if p.picker != nil {
				p.picker.Down(p.currentV4(), now)
			}
			p.rd.force(now)
		}
	}
	// v6 四态观察（规格 §5.1）：S0 本机无 v6 / S1 不可观测（无入口或握手无响应）
	// / S2a 在线 / S2b 未在线。绝不把 S1 塌缩成"不可达"（#8 误报）。
	d.ProbeV6 = func() string {
		if p.currentV6() == nil {
			return "no_address"
		}
		resp, err := p.handshakeOnce()
		if err != nil {
			return "unobservable"
		}
		return string(protocol.InterpretHandshake(resp))
	}
	// L2 出口策略观察（Python probe_l2_restricted 同源）：校园 DNS 绑源解析
	// 大众站（CDN 多线，必须实时解析），钉 IP 走物理栈 TLS 握手——被 RST =
	// 出口 DPI 受限窗口（认证会话正常，只观察不"修复"）。
	// 2026-11-22 双路对照（复盘内因 1）：campus 路死时用系统栈按域名拨号对照
	//——系统栈实测可用 → 校园 DNS 给了异常解析结果（dns_broken），与真·AC
	// 拉黑（双路都死，restricted）区分；4.5h 假警报不再发生。
	d.ProbeL2 = func() string {
		const l2Host = "www.baidu.com"
		bound, berr := netstack.ResolveBound(p.currentV4(), cfg.CampusDNS, l2Host, cfg.ProbeTimeout)
		// 目标是 v4 绑源客户端，必须是 v4 地址：绑源解析可能先回 v6（校园 DNS
		// 对百度的 A/AAAA 双记录，2026-11-20 实测先给 240e:...），v4 源拨 v6
		// 报 "no suitable address found" → 恒判「出口受限」假警告。
		campusTargeted := berr == nil && bound != nil && bound.To4() != nil
		if campusTargeted {
			// ipPin 必须 host:port（pinnedDialer 直接 Dial 它）；裸 IP 会报
			// missing port 让 L2 恒报 restricted（2026-11-08 发布截图实证）。
			if _, err := p.tlsProbe.Get("https://"+l2Host+"/", nil,
				net.JoinHostPort(bound.String(), "443")); err == nil {
				return l2Verdict(true, true, false, false)
			}
			// campus 路死 → 对照：系统解析拿 IP，**同一物理栈**钉该 IP 再测。
			// 两路都是物理栈钉 IP，TLS 结果只随解析结果变化——钉系统解析
			// IP 活 → campus 给的解析结果不可用（dns_broken，诊断有据）；
			// 也死 → 出口真受限（DPI 按 SNI 掐时钉任何真 IP 都死）。lazy：
			// 正常期零额外开销。不用系统栈按域名拨号对照——TUN 兜底会让
			// 「隧道活」冒充「校园路活」，把真受限误诊成 DNS 异常。
			sysT, sysOK := l2ProbeSystem(p.tlsProbe, l2Host)
			return l2Verdict(true, false, sysT, sysOK)
		}
		// campus 目标不可得（解析失败/只有 v6）：现状回退路——系统解析拿 v4
		// 钉 IP 直测（与浏览器回落同路）；都拿不到就放弃观察——解析失败不是
		// 出口受限。
		ip, ok := l2Target(bound, berr, func() (net.IP, error) {
			return netstack.ResolveSystem(l2Host)
		})
		if !ok {
			return l2Verdict(false, false, false, false)
		}
		sysOK := false
		if _, err := p.tlsProbe.Get("https://"+l2Host+"/", nil,
			net.JoinHostPort(ip.String(), "443")); err == nil {
			sysOK = true
		}
		return l2Verdict(false, false, true, sysOK)
	}
	d.FetchFlux = func() (map[string]string, error) { return p.fluxQuery(cfg) }
	// 多通道观察（2026-11-20 双通道 M1）：只观察、零凭据——每张物理卡各自
	// 一份认证态（门户按源 IP 记会话）。当前绑定源不在其列（主链路已在判）。
	wireChannels(d, p, cfg)
	// 通道级自愈（M2）：默认关（11-19 纪律）；开启后未认证通道各自登录，
	// 凭据绑**该通道的源 IP** 发往用户配置的门户，速率闸门与主链路共用
	// 同一份账号级额度（p.rl 是单例）。
	d.SetMultiChannel(cfg.MultiChannel)
	d.ChannelLogin = func(v keeper.ChannelView) (string, error) {
		resp, err := p.channelLogin(cfg, ChannelRef{ID: v.ID, Iface: v.Iface, IP: v.IP})
		// 通道登录事件可见（2026-11-21 复盘：通道自愈/失败在状态日志里无痕，
		// 一条「通道」行都搜不到，排查只能靠 /state 反推）。每次尝试一行，
		// 频率与主登录同受闸门约束，量级安全。
		if path := LogFilePath(); path != "" {
			event := fmt.Sprintf("通道登录(%s %s)完成", v.Iface, v.IP)
			if err != nil {
				event = fmt.Sprintf("通道登录(%s %s)失败: %v", v.Iface, v.IP, err)
			}
			_ = AppendEvent(path, EventClock(), event)
		}
		return resp, err
	}
	// 会话级存活探针（2026-11-11 实证：userflux 是账号级读数，会话释放后仍有
	// 数据 → 只靠 flux 判不出「会话没了」，守护不会自动重登）。
	// 走与登录同一个门户入口的 online_check（按请求源地址认栈、构造上无凭据），
	// 选路只走绑源（服务出口同路）；绑源不可达返回未知，不转系统栈。
	d.SessionAlive = func() (bool, error) {
		// 只走绑源：自动认证/判死的会话真值必须与服务出口同路。回退系统栈
		// 会造成「A 出口报状态、B 出口问会话」，正是 2026-11-18 17:48 的形态。
		st, err := p.onlineV4Bound(cfg)
		if err != nil {
			return false, err // 未知：不推进判死（门户不可达属离线分支职权）
		}
		return st.Online, nil
	}
	// 登录缝：速率闸门先行（闸门不开不触网），成功记账。
	d.Login = func() (string, error) {
		// 二次防线：内核门槛放行后出口仍可能在这几微秒内失效——绝不把凭据
		// 交给一条正在报「不可用」的出口（2026-11-18 不变量）。
		if eg := p.egressHealth(); !eg.Valid {
			detail := eg.Detail
			if detail == "" {
				detail = "出口不可用"
			}
			if eg.Reason == keeper.EgressPortalUnreachable {
				return "", fmt.Errorf("%w：%s", keeper.ErrEgressPortalUnreachable, detail)
			}
			return "", fmt.Errorf("%w：%s", keeper.ErrEgressInvalid, detail)
		}
		if !p.rl.AllowAutoLogin() {
			return "", fmt.Errorf("%w：滚动 1 小时自动登录已达上限，闸门关闭 15 分钟", keeper.ErrRateCapped)
		}
		// 自动登录尝试落状态日志（2026-11-13 排查教训：登录风暴在日志里无痕，
		// 只能靠速率闸门状态反推）。每次尝试一行，30s 闸限流，量级安全。
		if path := LogFilePath(); path != "" {
			_ = AppendEvent(path, EventClock(), "自动登录尝试（速率闸门放行）")
		}
		resp, err := p.action.Do()
		if err != nil {
			return "", err
		}
		p.rl.RecordSuccess(time.Now())
		return resp.Resp, nil
	}
	return d
}

// l2Verdict L2 双路对照判定表（2026-11-22 复盘内因 1）。sysOK 语义 =
// 「**物理栈**钉系统解析 IP 的 TLS 活」（同栈对照——两路都是绑源钉 IP，
// 结果差异只来自解析，诊断才有据；见 ProbeL2 闭包注释）：
//   - campus 路活 → ok（正常期零额外开销——对照路不触网）；
//   - campus 路死 + 对照路活 → dns_broken（campus 给的解析结果不可用，
//     物理栈实测可达——11-22 事故假警报形态，与真·AC 拉黑区分）；
//   - campus 路死 + 对照路死/不可测 → restricted（真·出口受限；无对照
//     证据时保守维持受限读数，不引入新误报）。
//
// campus 目标不可得 → 走现状回退路语义：对照路活 → ok；对照路死 → restricted；
// 都不可得 → ok（探测自身失败不是出口受限）。
func l2Verdict(campusTargeted, campusOK, sysTargeted, sysOK bool) string {
	if !campusTargeted {
		if !sysTargeted {
			return "ok" // 探测自身失败不是出口受限（2026-11-20 语义不变）
		}
		if sysOK {
			return "ok"
		}
		return "restricted"
	}
	if campusOK {
		return "ok"
	}
	if !sysTargeted {
		return "restricted" // 无对照证据，保守维持受限读数
	}
	if sysOK {
		return "dns_broken"
	}
	return "restricted"
}

// l2ProbeSystem 对照路探测：系统解析拿 v4，物理栈钉该 IP 测 TLS（同栈对照，
// 见 ProbeL2 闭包注释）。返回 (targeted, tlsOK)——targeted=false = 系统解析
// 不可得，对照证据缺失。
func l2ProbeSystem(tlsProbe *netstack.Client, host string) (bool, bool) {
	ip, err := netstack.ResolveSystem(host)
	if err != nil || ip == nil || ip.To4() == nil {
		return false, false
	}
	if _, err := tlsProbe.Get("https://"+host+"/", nil,
		net.JoinHostPort(ip.String(), "443")); err == nil {
		return true, true
	}
	return true, false
}

// l2Target 选 L2 探测目标（v4 绑源客户端需要 v4 地址）：
//   - 绑源解析给了 v4 → 直接用（校园 DNS 绑源，防 TUN fake-ip）；
//   - 只有 v6 / 绑源失败 → 回退系统解析的 v4（与浏览器回落同路）；
//   - 都拿不到 → ok=false，调用方放弃这次观察。
//
// 2026-11-20 真机修复：之前直接拿绑源解析结果（可能 v6）去喂 v4 客户端，
// 探针必然失败 → 面板恒报「出口受限（大众站 TLS 被重置）」而实际网络正常。
func l2Target(bound net.IP, boundErr error, sysV4 func() (net.IP, error)) (net.IP, bool) {
	if boundErr == nil && bound != nil && bound.To4() != nil {
		return bound, true
	}
	if sysV4 == nil {
		return nil, false
	}
	if ip, err := sysV4(); err == nil && ip != nil && ip.To4() != nil {
		return ip, true
	}
	return nil, false
}
