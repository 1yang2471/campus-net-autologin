package app

import (
	"errors"
	"net"

	"cnal/internal/netstack"
)

// 门户选路（设计 2026-11-10-双路径自适应设计.md §3/§4）：同一份内核按
// net_mode 与逐操作健康探测，在「绑源钉 IP」（路径 A）与「系统栈，跟 TUN」
// （路径 B）之间自动选路。绑源优先——它保留校园网真实态读数，凭据不经代理；
// 只有需要域名解析的操作才会在绑源解析失败时转系统栈。
const (
	routeBound  = "bound"
	routeSystem = "system"
)

// routeLabels 路径日志用的人话（词表只放一处）。
//
// 「系统栈」不带「跟 TUN」——TUN 是否接管要靠适配器描述才说得准，
// 2026-11-11 实测那台机器上 TUN 根本没开，凭空写「跟 TUN」就是把猜测
// 当事实说（设计 §4）。
var routeLabels = map[string]string{
	routeBound:  "绑源钉 IP",
	routeSystem: "系统栈",
}

// portalRoute 一次门户操作的选路结果。client == nil 表示 fail-closed
// （仅 net_mode=bound 且绑源解析失败）：调用方拒绝该操作，绝不猜地址。
type portalRoute struct {
	client *netstack.Client
	pin    string // 仅绑源路径非空（host:port）；系统栈按域名拨号
	path   string
	why    string
}

// routePortal 门户页/flux 等宽超时用途的选路。
func (p *plumbing) routePortal() portalRoute { return p.chooseRoute(p.portal, p.systemPortal) }

// routeLogin 登录/归属查询等登录超时用途的选路。
func (p *plumbing) routeLogin() portalRoute { return p.chooseRoute(p.login, p.systemLogin) }

// chooseRoute 三模式选路：
//   - system：强制系统栈，不碰绑源解析（TUN 下那次查询必然失败）；
//   - bound：只走绑源钉 IP，解析失败 fail-closed；
//   - auto ：绑源解析成功走绑源，失败转系统栈。
func (p *plumbing) chooseRoute(bound, system *netstack.Client) portalRoute {
	mode := p.netMode
	if mode == "" {
		mode = NetModeAuto // 手工装配的 plumbing（测试/旧调用）按默认语义
	}
	if mode == NetModeSystem {
		return p.noteRoute(portalRoute{
			client: system, path: routeSystem, why: "net_mode=system 手动强制",
		})
	}
	ip, err := p.pt.lookup()
	if ip != nil {
		pin := net.JoinHostPort(ip.String(), p.portalPort)
		return p.noteRoute(portalRoute{
			client: bound, pin: pin, path: routeBound,
			why: "绑源解析成功（钉 " + pin + "）",
		})
	}
	if mode == NetModeBound {
		return p.noteRoute(portalRoute{
			path: routeBound,
			why:  "绑源解析失败（" + routeErrText(err) + "），net_mode=bound 拒绝转系统栈",
		})
	}
	return p.noteRoute(portalRoute{
		client: system, path: routeSystem,
		why: "绑源解析失败（" + routeErrText(err) + "），转系统栈",
	})
}

// noteRoute 路径变化才写日志（PortalAlive 每拍都选路，逐拍记会刷屏）。
// 未注入 logf（测试替身）时静默。
func (p *plumbing) noteRoute(r portalRoute) portalRoute {
	p.routeMu.Lock()
	changed := r.path != p.lastRoute
	if changed {
		p.lastRoute = r.path
		p.lastWhy = r.why
	}
	p.routeMu.Unlock()
	if changed && p.logf != nil {
		p.logf("门户路径: %s（%s）\n", routeLabels[r.path], r.why)
	}
	return r
}

// routeText 当前门户路径的一句话描述（面板「门户路径」行）：
// "绑源钉 IP（钉 1.1.1.2:443）"。还没选过路时返回空串。
func (p *plumbing) routeText() string {
	p.routeMu.Lock()
	defer p.routeMu.Unlock()
	if p.lastRoute == "" {
		return ""
	}
	label := routeLabels[p.lastRoute]
	if p.lastWhy == "" {
		return label
	}
	return label + "（" + p.lastWhy + "）"
}

// routeErrText 解析失败原因文本（err 为 nil 时给兜底，日志不留空白）。
func routeErrText(err error) string {
	if err == nil {
		return "未知原因"
	}
	return err.Error()
}

// errNoRoute 选路 fail-closed 时的统一错误（调用方按用途再加前缀）。
var errNoRoute = errors.New("门户解析失败（疑似不在校园网）——不把凭据发往猜测地址")
