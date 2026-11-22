package keeper

import (
	"sync"
	"sync/atomic"
	"time"
)

// 多通道观察（2026-11-20 双通道方案 §8.3 M1）。
//
// 一条通道 = 一条物理链路上的一个 v4 会话身份 = (接口名, 源 IP)：门户按
// 来源 IP 记会话（11-20 实测：WiFi 已认证不放行有线口 IP），所以多网卡
// 场景下每张卡各自是一份认证态，互不代替。
//
// M1 只做**观察**：零凭据、只上报。登录是 M2 的事，且默认关（见 spec §8.2：
// 通道级登录会放大门户反暴力冻结风险，由账号级共享预算正面解决）。
//
// 为什么放内核：节流、异步单飞、快照读数这三样与单通道时是同一套纪律；
// 写在壳层就会出现两份「60s 一探」的规矩漂移。

// 通道认证态取值（唯一出处：内核判状态、面板只显示 label）。
const (
	// ChannelAuthed 已认证：这条通道的会话是活的。
	ChannelAuthed = "authed"
	// ChannelUnauth 已连接但未认证（被门户劫持）——唯一可发起登录的态。
	ChannelUnauth = "unauth"
	// ChannelBlackholed 黑洞：DNS 通但连不通（未认证 VLAN 常见）。
	ChannelBlackholed = "blackholed"
	// ChannelOffline 不可达：DNS 不通或网卡不可达。
	ChannelOffline = "offline"
)

// ChannelStateLabel 状态中文词（唯一出处：面板不自造状态词）。
func ChannelStateLabel(state string) string {
	switch state {
	case ChannelAuthed:
		return "已认证"
	case ChannelUnauth:
		return "已连接未认证"
	case ChannelBlackholed:
		return "已连接但不通"
	case ChannelOffline:
		return "不可达"
	default:
		return "未知"
	}
}

// ChannelLoginable 该态能否发起登录（只有「已连接未认证」可以）。
func ChannelLoginable(state string) bool { return state == ChannelUnauth }

// ChannelView 一条通道的观察读数（内核只认字符串——内核不碰网络类型）。
type ChannelView struct {
	// ID 稳定标识（接口名|源 IP）。DHCP 换 IP 即新会话身份。
	ID string `json:"id"`
	// Iface 接口名（面板要显示：两张卡都叫「以太网」时只有 IP 分不清）。
	Iface string `json:"iface"`
	// IP 该通道的 v4 源地址。
	IP string `json:"ip"`
	// State 认证态：authed / unauth / blackholed / offline。
	State string `json:"state"`
	// StateLabel 状态中文词（唯一出处：面板不自造状态词）。
	StateLabel string `json:"label"`
}

// channelProbeInterval 通道观察节流（60s）：与 L2/v6/旁路观察同量级。
// 认证态不是心跳——每拍都探会刷爆日志并放大成请求风暴。
const channelProbeInterval = 60 * time.Second

// ChannelGuard 多通道观察器：枚举通道 → 逐通道探测 → 留读数给面板/快照。
//
// 探测**异步 + 单飞**：通道探测要走 DNS + HTTP，未认证 VLAN 上两项都可能
// 吃满超时；同步跑在 Tick 里会把 3 秒心跳拖成十几秒一拍（2026-11-19 回归）。
type ChannelGuard struct {
	// Enumerate 枚举当前通道（接口名 + 源 IP + 稳定标识）。nil = 不观察。
	Enumerate func() []ChannelView
	// Probe 探测单条通道的认证态（零凭据）。nil = 不观察。
	Probe func(ChannelView) string
	// Now 时钟缝（测试可控；默认 time.Now）。
	Now func() time.Time

	mu    sync.RWMutex
	views []ChannelView
	at    time.Time
	gen   uint64
	busy  atomic.Bool
}

// NewChannelGuard 造观察器（默认时钟与默认节流）。
func NewChannelGuard() *ChannelGuard {
	return &ChannelGuard{Now: time.Now}
}

// tick 走一拍观察：节流判定 + 异步起飞。探测在独立 goroutine 里跑，
// 同一时刻最多一个在飞（慢探测不被下一拍叠加成风暴）。
func (g *ChannelGuard) tick() {
	if g == nil || g.Enumerate == nil || g.Probe == nil {
		return
	}
	now := g.now()
	if !g.at.IsZero() && now.Sub(g.at) < channelProbeInterval {
		return
	}
	if g.busy.Swap(true) {
		return // 上一轮还在飞：不叠加
	}
	g.at = now
	g.mu.Lock()
	g.gen++
	gen := g.gen
	g.mu.Unlock()
	go func() {
		list := g.Enumerate()
		out := make([]ChannelView, 0, len(list))
		for _, v := range list {
			v.State = g.Probe(v)
			v.StateLabel = ChannelStateLabel(v.State)
			out = append(out, v)
		}
		// 探测结束即释放单飞（不等写回）：否则「上一轮刚落地、下一拍就被
		// 跳过」的窗口真实存在——观察会整整晚一个周期。
		g.busy.Store(false)
		g.mu.Lock()
		// 只有最新一轮才写回：慢的旧轮不得覆盖新轮的读数。
		if gen == g.gen {
			g.views = out
		}
		g.mu.Unlock()
	}()
}

// ForceProbe 作废观察节流（2026-11-21 决议 D）：免认证窗口结束的边沿调用
// ——窗口内门户不维护任何通道的会话，60s 节流里的陈旧「已认证」读数在
// 窗口结束后照样骗面板。置零节流起点，下一拍立即重探（主链路 7fefe08
// 同款边沿语义，收口在 ChannelGuard 防止两处规矩漂移）。nil 安全。
func (g *ChannelGuard) ForceProbe() {
	if g == nil {
		return
	}
	g.at = time.Time{} // 与 tick 同 goroutine（Tick 调用链），无需加锁
}

// now 读时钟（缝未接线时回落 time.Now）。
func (g *ChannelGuard) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

// Views 最近一次观察读数（副本：调用方在别的 goroutine 读）。
func (g *ChannelGuard) Views() []ChannelView {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if len(g.views) == 0 {
		return nil
	}
	out := make([]ChannelView, len(g.views))
	copy(out, g.views)
	return out
}
