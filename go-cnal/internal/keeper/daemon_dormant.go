package keeper

import (
	"fmt"
	"time"
)

// 离开校园网休眠观察（2026-11-21 09:52 日志实证 + 当日产品决议 A）：
// 非校园网络（蜂窝热点）上物理源重选乱跳——172.29.153.208↔10.158.52.162
// 反复切换 + bind 失败，白烧探测与重选。StatePortalDown 持续 ≥10 分钟
// 进入休眠观察：重选回调停调、门户探针降频（每拍 → ≥60s 一探）。
//
// **语义红线**：这是「休眠观察」不是「判定离开」——校园网故障同形态，
// 文案不得断言已离开校园网。恢复条件是门户探针转绿或锚点绿，恢复判定
// 放在转绿的第一拍；登录行为不变（portal down 时本来就登不了）。

const (
	// portalDownDormantAfter portal_down 持续多久进入休眠观察。
	portalDownDormantAfter = 10 * time.Minute
	// portalDownProbeInterval 休眠期门户探针节流：观察降到 ≥60s 一探
	//（与 L2/v6/通道观察同量级——认证态不是心跳）。
	portalDownProbeInterval = 60 * time.Second
)

// portalDownTick 记一拍窗口外门户不可达（eg 不可达分支与 !portalAlive
// 分支共用）：streak 起点 → 持续 ≥10 分钟置休眠。Tick 同 goroutine，无锁。
// 免认证窗口内的 portal_down 不记拍（那是预期形态，且该分支先于本函数返回）。
func (d *Daemon) portalDownTick() {
	now := d.Now()
	if d.portalDownSince.IsZero() {
		d.portalDownSince = now
	}
	if !d.dormant && now.Sub(d.portalDownSince) >= portalDownDormantAfter {
		d.dormant = true
	}
}

// portalAliveThrottled 门户可达探针：正常每拍一探；休眠观察期降到
// ≥60s 一探——窗口内用缓存结论 false（门户本来就不可达），不真探。
func (d *Daemon) portalAliveThrottled() bool {
	now := d.Now()
	if d.dormant && !d.portalAliveAt.IsZero() && now.Sub(d.portalAliveAt) < portalDownProbeInterval {
		return false
	}
	d.portalAliveAt = now
	return d.PortalAlive()
}

// leavePortalDown 离开 portal_down（锚点绿或门户探针转绿的第一拍）：
// 清休眠与 streak、探针节流清零，恢复正常节拍（回调恢复、每拍真探）。
func (d *Daemon) leavePortalDown() {
	d.portalDownSince = time.Time{}
	d.dormant = false
	d.portalAliveAt = time.Time{}
}

// portalDownDetail portal_down 状态行：休眠观察期点名（语义红线见文件头
//——首句必须是「门户持续不可达」这个可实证的事实，不得断言「离开校园网」：
// 校园网故障同形态，校内用户看到断言会误查自己的网络切换而不是等校园恢复；
// 「换网/回校后自动恢复」说的是两种恢复路径，不是判定用户在哪）。
// 其余照旧透传 base（egressDetail 或空串）。
func (d *Daemon) portalDownDetail(base string) string {
	if d.dormant {
		return fmt.Sprintf("门户持续不可达 %s（休眠观察）——换网/回校后自动恢复", portalDownDormantAfter)
	}
	return base
}
