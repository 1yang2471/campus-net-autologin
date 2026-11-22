package app

import (
	"net"
	"sync"
	"time"
)

// rediscoverer 物理源重发现节流（移植 Python P2-8）：网络切换/休眠唤醒后
// 旧自动发现的源 IP 会失效，表现为长期 portal_down；组合根在每次 portal_down
// 拍调用 tick，节流窗口内不动、窗口外重发现一次，且只在源真的变了才换绑
// （无意义改绑会打断正在进行的请求）。
//
// 并发纪律（2026-11-22 修复项 3 后）：触发面从「仅 Tick goroutine（OnPortalDown/
// OnEgressInvalid）」拓宽到「chooseRoute 转系统栈时」（登录/查流量的 HTTP
// goroutine 也可能触）——字段改 mu 护住。tick/force 用 TryLock：拿不到锁 =
// 另一 goroutine 正在重选（探测秒级），本拍放弃不排队——重选自带 interval
// 节流（丢拍常态），锁竞争下丢拍与节流丢拍语义一致，且登录热路径不背
// 秒级探测延迟。
type rediscoverer struct {
	mu       sync.Mutex
	interval time.Duration
	current  net.IP
	discover func() net.IP
	apply    func(net.IP)
	last     time.Time
}

func newRediscoverer(interval time.Duration, current net.IP, discover func() net.IP, apply func(net.IP)) *rediscoverer {
	return &rediscoverer{interval: interval, current: current, discover: discover, apply: apply}
}

// tick 一拍：窗口内直接返回；窗口外探测，发现新源且与当前不同才换绑。
// 发现失败（nil）保持原源不动（fail-closed：不用坏源覆盖好源）。
func (r *rediscoverer) tick(now time.Time) {
	if !r.mu.TryLock() {
		return // 已有并发 tick/force 在重选：本拍放弃（见类型注释）
	}
	defer r.mu.Unlock()
	r.tickLocked(now)
}

func (r *rediscoverer) tickLocked(now time.Time) {
	if !r.last.IsZero() && now.Sub(r.last) < r.interval {
		return
	}
	r.last = now
	fresh := r.discover()
	if fresh == nil || fresh.Equal(r.current) {
		return
	}
	r.current = fresh
	r.apply(fresh)
}

// force 绑定失效时立即重选一次（不等 interval）：死源留在绑定位上每多一秒，
// 都在放大「探测/动作两套网络观」的窗口。自带 5s 下限——内核每拍都会报
// 出口失效，不能每拍都重新探测一遍所有候选。
const forceMinInterval = 5 * time.Second

func (r *rediscoverer) force(now time.Time) {
	if !r.mu.TryLock() {
		return // 重选进行中：探测完成后源大概率已换绑，无需叠加
	}
	defer r.mu.Unlock()
	if !r.last.IsZero() && now.Sub(r.last) < forceMinInterval {
		return
	}
	r.last = time.Time{}
	r.tickLocked(now)
}
