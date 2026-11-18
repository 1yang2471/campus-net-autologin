package app

import (
	"net"
	"time"
)

// rediscoverer 物理源重发现节流（移植 Python P2-8）：网络切换/休眠唤醒后
// 旧自动发现的源 IP 会失效，表现为长期 portal_down；组合根在每次 portal_down
// 拍调用 tick，节流窗口内不动、窗口外重发现一次，且只在源真的变了才换绑
// （无意义改绑会打断正在进行的请求）。
type rediscoverer struct {
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
