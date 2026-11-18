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
		for _, ip := range cfg.ProbeIPs {
			st, body, err := p.probe.GetStatus("http://"+ip+cfg.ProbePath,
				map[string]string{"Host": cfg.ProbeHost})
			if err != nil {
				continue // status 0：被劫持或不可达，试下一锚点
			}
			switch st {
			case 301, 302, 307, 308:
				continue // 劫持跳转
			}
			if st == 200 && containsAny(body, cfg.HijackMarks) {
				continue // 200 型门户劫持页（JS 挑战）不是真实互联网
			}
			return true
		}
		return false
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
	// 物理源重发现只在 bind_ip 未钉源时装配（rd 为 nil 即手工钉源）。
	if p.rd != nil {
		d.OnPortalDown = func() { p.rd.tick(time.Now()) }
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
	// 出口 DPI 受限窗口（认证会话正常，只观察不"修复"）。DNS 不通=L1 问题
	// 不是 L2，报 ok 不误报。
	d.ProbeL2 = func() string {
		const l2Host = "www.baidu.com"
		ip, err := netstack.ResolveBound(p.currentV4(), cfg.CampusDNS, l2Host, cfg.ProbeTimeout)
		if err != nil {
			return "ok" // DNS 都不通说明是 L1 问题，不是 L2
		}
		// ipPin 必须是 host:port（pinnedDialer 直接 Dial 它）；裸 IP 会报
		// missing port 让 L2 恒报 restricted（2026-11-08 发布截图实证）。
		if _, err := p.tlsProbe.Get("https://"+l2Host+"/", nil,
			net.JoinHostPort(ip.String(), "443")); err != nil {
			return "restricted" // 大众站 TLS 被 RST = DPI 风控窗口
		}
		return "ok"
	}
	d.FetchFlux = func() (map[string]string, error) { return p.fluxQuery(cfg) }
	// 会话级存活探针（2026-11-11 实证：userflux 是账号级读数，会话释放后仍有
	// 数据 → 只靠 flux 判不出「会话没了」，守护不会自动重登）。
	// 走与登录同一个门户入口的 online_check（按请求源地址认栈、构造上无凭据），
	// 选路与登录一致（绑源优先、失败转系统栈）。
	d.SessionAlive = func() (bool, error) {
		st, err := p.onlineV4(cfg)
		if err != nil {
			return false, err // 未知：不推进判死（门户不可达属离线分支职权）
		}
		return st.Online, nil
	}
	// 登录缝：速率闸门先行（闸门不开不触网），成功记账。
	d.Login = func() (string, error) {
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
