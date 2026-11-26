package app

import (
	"errors"
	"fmt"
	"net"
	"time"

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
	src := bound.Source()
	ip, err := p.pt.lookupForSource(src)
	if errors.Is(err, netstack.ErrInterfaceBind) {
		err = errSourceUnbindable
	}
	if ip != nil {
		// 2026-11-23 过夜缺陷：DNS 结果在 TTL 缓存里命中，但 wlan0 的 IP 已被
		// Doze 从内核网卡摘掉——dialer 的 LocalAddr 绑不上，5 小时 13 拍全卡在
		// 「正在重选」。解析成功不等于绑源可用：自检一次源是否仍可绑定。
		bindOK := p.bindable == nil || p.bindable(src)
		p.noteBind(bound.BindingInfo())
		if bindOK {
			pin := net.JoinHostPort(ip.String(), p.portalPort)
			return p.noteRoute(portalRoute{
				client: bound.CloneForSource(src), pin: pin, path: routeBound,
				why: "绑源解析成功（钉 " + pin + "）",
			})
		}
		// 源不可绑定：等同源失效——清门户 IP TTL 缓存，让下次 lookup 重新
		// 解析（否则旧 DNS 结果继续粘住）；下面走与解析失败相同的 fallback。
		p.pt.invalidate()
		err = errSourceUnbindable
		// 注入模式自愈信号（D-09）：通知 Kotlin 重新取 LinkProperties 注入源。
		// 一次失效只通知一次，重绑成功后由 rebindInjectedSources 复位；
		// 无消费者（Windows/未注入）时 no-op。
		p.notifySourceStale()
	}
	// 解析失败 / 源失效分开报：排障方向不同（查 DNS vs 查网卡）。
	failKind := "绑源解析失败"
	if err == errSourceUnbindable {
		failKind = "绑源不可用"
	}
	if mode == NetModeBound || (mode == NetModeAuto && p.strictInterfaceBind) {
		return p.noteRoute(portalRoute{
			path: routeBound,
			why:  failKind + "（" + routeErrText(err) + "），严格接口绑定拒绝转系统栈",
		})
	}
	// 转系统栈 = 绑源不可用（解析失败或源失效）。触发物理源重发现（2026-11-22
	// 复盘内因 3：rd 触发面窄——11-22 事故插线恢复后 10 分钟粘滞，绑源解析恢复
	// 前门户路径一直赖在系统栈）。rd.tick 自带 interval 节流 + TryLock 非阻塞
	// + 源变了才换绑，热路径安全；interval 窗口外的首击会在本 goroutine 同步
	// 跑一次源探测（秒级）——绑源已死时的手动操作多背这一份延迟，属重选的
	// 本职工作。注入模式 rd 未装配（安卓由 Kotlin 选源），此处不触发。
	if p.rd != nil {
		p.rd.tick(time.Now())
	}
	return p.noteRoute(portalRoute{
		client: system, path: routeSystem,
		why: failKind + "（" + routeErrText(err) + "），转系统栈",
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
	text := label
	if p.lastWhy != "" {
		text += "（" + p.lastWhy + "）"
	}
	if p.lastRoute == routeBound && p.portal != nil {
		info := p.bindingInfo()
		switch info.Mode {
		case "interface":
			text += fmt.Sprintf("｜接口绑定 %s(#%d)", info.Interface, info.Index)
		case "source_only":
			if info.Degraded {
				text += "｜降级：仅绑源 IP"
			} else {
				text += "｜仅绑源 IP"
			}
		}
		if info.Err != nil {
			text += "｜接口绑定失败：" + info.Err.Error()
		}
	}
	return text
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

// errSourceUnbindable 源 IP 从网卡摘掉（DNS 结果还在但 LocalAddr 绑不上）。
// 与「解析失败」分开报——排障方向完全不同（查网卡 vs 查 DNS）。
var errSourceUnbindable = errors.New("绑源不可用（网卡 IP 已失效）")
