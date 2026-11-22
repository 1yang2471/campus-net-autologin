package keeper

import "time"

// 观测缝（v6 四态 / L2 出口 / L3 会话）：从 daemon.go 拆出——单文件守
// 2 万字节提交闸（tidy §3），语义仍是「观察层」，不参与状态机骨架。

func (d *Daemon) probeSession() {
	if d.SessionAlive == nil && d.FetchFlux == nil {
		return
	}
	now := d.Now()
	// SESSION_FALSE 熔断只停 flux 回落（假判死的信号源），不得连坐 SessionAlive
	// （2026-11-21 实测复盘：熔断把 IP 级真值一并停了 1 小时，会话真死无人报，
	// 假绿窗口从此打开——免认证窗口结束 3.5h 假在线的成因链一环）。
	if d.SessionAlive == nil && now.Before(d.fluxOffUntil) {
		return
	}
	if !d.sessAt.IsZero() && now.Sub(d.sessAt) < sessionProbeInterval {
		return // 60s 节流（同 l2At/v6At）：会话真值不是心跳
	}
	d.probeSessionOnce(now)
}

// probeSessionOnce 执行一次会话核实并更新判死状态；返回 (known, alive)。
// L1 绿（60s 心跳）与 L1 红（用户路径断，立即核实）共用，保证两条路径
// 对「会话真值」的定义一致。
func (d *Daemon) probeSessionOnce(now time.Time) (known, alive bool) {
	d.sessAt = now
	if d.SessionAlive != nil {
		a, err := d.SessionAlive()
		if err != nil {
			return false, false
		}
		alive, known = a, true
	} else if d.FetchFlux != nil {
		flux, err := d.FetchFlux()
		if err != nil {
			return false, false
		}
		alive, known = len(flux) > 0, true
		d.updateFluxView(flux, now) // 复用这一次读数给流量视图，避免同拍再打一次门户
	} else {
		return false, false
	}
	if alive {
		d.sessMiss = 0
		d.setSessionDead(false)
		d.suspect = 0               // 会话活 = 判定链健康证明（Python 在线分支清 suspect）
		d.rejectUntil = time.Time{} // 同源：在线分支清 reject_until
		d.sessFalse = 0             // 拿得到真值 = 门户支持该探针且会话健康
		return true, true
	}
	d.sessMiss++
	if d.sessMiss >= d.sessionMissThreshold() {
		d.setSessionDead(true)
	}
	return true, false
}

// userPathInterval 用户路径读数节流（5s）：系统栈锚点是展示读数，不是心跳；
// 每拍都打会平白多一份网络请求（TUN 下还要过代理）。
const userPathInterval = 5 * time.Second

// probeUserPath 采一次用户路径读数（系统栈）。只做展示，不参与判定。
func (d *Daemon) probeUserPath() {
	if d.UserPath == nil {
		return
	}
	now := d.Now()
	if !d.userPathAt.IsZero() && now.Sub(d.userPathAt) < userPathInterval {
		return
	}
	d.userPathAt = now
	r := d.UserPath()
	d.mu.Lock()
	d.userPath = r
	// 红 streak 起点记账（mu 护住：跨 goroutine 读者 fakeOnline 也要读它，
	// 2026-11-22 修复项 2）：2026-11-21 决议 B——用户路径红持续 ≥5 分钟才
	// 上浮互证附注；转绿或转未知都清零（未知期间的陈账不是「持续异常」的
	// 证据，红复来重新起算）。
	if r.Known && !r.OK {
		if d.userPathSince.IsZero() {
			d.userPathSince = now
		}
	} else {
		d.userPathSince = time.Time{} // 转绿/转未知即消，且不计时
	}
	d.mu.Unlock()
}

// l1RedSessionInterval L1 红时会话核实的下限（不是心跳）：用户路径一断，
// 立即问一次门户；之后最多每 10s 一次，避免每拍打 online_check。
const l1RedSessionInterval = 10 * time.Second

// probeSessionOnL1Red 在 L1 红时核实会话真值（2026-11-18：门户说在线就
// 不登录，说没登录才登录）。返回 (dead, known)：
//
//	known=false → 真值不可得（无探针/门户不可达/自停期），调用方回落去抖；
//	known=true, dead=false → 认证正常，不登录（锚点红是 L2/线路问题）；
//	known=true, dead=true  → 允许登录（跳过 L1 三拍去抖：会话真值是权威证据）。
func (d *Daemon) probeSessionOnL1Red(now time.Time) (dead, known bool) {
	// 只认会话级真值（online_check）。flux 是账号级读数——会话被释放后它
	// 照样有数据（2026-11-11 实证），拿它做 L1 红的即时判据会压掉必要的
	// 重连（免会话态退出时尤其明显）。没有会话级探针就回落 L1 去抖。
	if d.SessionAlive == nil {
		return false, false
	}
	// fluxOffUntil 在此只可能连坐 SessionAlive（flux 走不到这条缝）——
	// IP 级真值不受熔断停用（同 probeSession 的 2026-11-21 修正）。
	if !d.sessRedAt.IsZero() && now.Sub(d.sessRedAt) < l1RedSessionInterval {
		return !d.sessRedAlive, d.sessRedKnown
	}
	d.sessRedAt = now
	known, alive := d.probeSessionOnce(now)
	d.sessRedKnown = known
	d.sessRedAlive = alive
	return !alive, known
}

// invalidateSessionCache 登录成功后作废 L1 红时的会话缓存：下一次 L1 红拍
// 必须重新问门户，不能拿登录前的「死」结论重复判断。
func (d *Daemon) invalidateSessionCache() {
	d.sessRedAt = time.Time{}
}

// sessionAliveL1RedDetail 认证在线但锚点红时的状态行：短窗「认证在线（锚点
// 异常，会话正常）」，组合态（2026-11-22 复盘内因 2：三读数并立≥10 分钟）
// 升级为「校园出口被控」+ 指引——「在线」打头的误导正是本次事故的痛点根源。
// L2 观察作为附属信息，不抢主句；组合态下 L2 文案的「勿反复重登」段与组合
// 指引重复，省略附属。
func (d *Daemon) sessionAliveL1RedDetail() string {
	if d.userPathRedLong() {
		// 主状态词「会话在线｜出口被控」由 fake_online reason 覆盖进 Label；
		// detail 讲细节与指引，首段不与 Label 重复（MenuLine 只取首段）。
		return "锚点+用户路径持续红≥10 分钟（会话由 online_check 独立核实）｜重连救不了：等待恢复或换网络，可查梯子残留"
	}
	base := "认证在线（锚点异常，会话正常）"
	if extra := d.onlineDetail(false); extra != "" {
		return base + "｜" + extra
	}
	return base
}

// fakeOnlineThreshold AC 假在线组合判定阈值（2026-11-22 复盘内因 2）：会话绿+
// 锚点红+用户路径红三读数并立 ≥10 分钟。5min 是 userPath 附注门槛（决议 B），
// 10min 是「持续较久」语义锚点（l2StreakThreshold 同源）。
const fakeOnlineThreshold = 10 * time.Minute

// userPathRedLong 用户路径红持续 ≥fakeOnlineThreshold（组合判据的 streak 项）。
func (d *Daemon) userPathRedLong() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return !d.userPathSince.IsZero() && d.Now().Sub(d.userPathSince) >= fakeOnlineThreshold
}

// fakeOnline AC 假在线组合判定（唯一真相源）：会话绿+锚点红+用户路径红≥10 分钟。
// health.go 的 Reason（→ Label「会话在线｜出口被控」）与组合文案都取这里，
// 不自造第二份组合判定。
func (d *Daemon) fakeOnline() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.State != StateOnline || d.SessionDead || !d.l1RedSessionAlive {
		return false
	}
	return !d.userPathSince.IsZero() && d.Now().Sub(d.userPathSince) >= fakeOnlineThreshold
}

// sessionMissThreshold 判死需要几拍「不在线」。
//
//   - 接了会话级探针（online_check）→ **1 拍**：它给的是门户对「这个源地址现在
//     挂在哪」的权威回答，不是 flux 那种弱信号。2026-11-11 实测的代价：按 2 拍
//     处理时，注销后守护先报「在线」60 秒（一拍 + 60s 节流）才动手，用户看到的
//     就是「不自动重连」（他等不到就手动点了）。
//   - 回落 flux（空表）→ **2 拍**：弱信号，单拍空表可能是抖动，保留原有去抖。
func (d *Daemon) sessionMissThreshold() int {
	if d.SessionAlive != nil {
		return 1
	}
	return 2
}

// setSessionDead 会话判死标记（与 View 的加锁读配对）。
func (d *Daemon) setSessionDead(dead bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.SessionDead = dead
}

// setFreeMode 免会话态标记（与 View 的加锁读配对）。
func (d *Daemon) setFreeMode(on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.FreeMode = on
}

// setV6State v6 观察值（与 View 的加锁读配对）。
func (d *Daemon) setV6State(s string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.V6State = s
}

// probeL2 出口策略观察，60s 一节流（与 v6 同级：每拍 TLS 握手既刷日志又
// 高频撞 DPI 黑名单）。受限结果保留 2×周期（恢复后用户仍该看见刚才发生过）。
func (d *Daemon) probeL2() {
	if d.ProbeL2 == nil {
		return
	}
	now := d.Now()
	if !d.l2At.IsZero() && now.Sub(d.l2At) < v6ProbeInterval {
		return
	}
	d.l2At = now
	// 探测（含 TLS 握手，秒级）在锁外；只有赋值进锁——别让壳层的
	// RLock 读被网络调用卡住几秒（2026-11-08 P0 并发纪律的写侧对称）。
	// 结果三态：ok / restricted（真·出口受限）/ dns_broken（校园 DNS 给了
	// 异常解析结果，网络实际可用——2026-11-22 复盘内因 1，与真拉黑区分）。
	result := d.ProbeL2()
	restricted := result == "restricted"
	dnsBroken := result == "dns_broken"
	d.mu.Lock()
	// 受限/解析异常记账都进锁：跨 goroutine 读者是 fakeOnline（读 userPathSince）
	// 与 DnsBrokenOnUI/L2RestrictedOnUI（壳层 goroutine）；onlineDetail 仅 Tick
	// goroutine 调（Tick 内裸读记账字段属既有纪律，勿迁出 Tick goroutine）。
	if restricted {
		if d.l2Since.IsZero() {
			d.l2Since = now // 本轮受限起点
		}
	} else {
		d.l2Since = time.Time{}
	}
	d.l2Restricted = restricted
	if restricted {
		d.l2WindowUntil = now.Add(2 * v6ProbeInterval)
	}
	// dns_broken 记账与 l2 族对称（三态互斥，ok 双清）。
	if dnsBroken {
		if d.dnsBrokenSince.IsZero() {
			d.dnsBrokenSince = now
		}
	} else {
		d.dnsBrokenSince = time.Time{}
	}
	d.dnsBroken = dnsBroken
	if dnsBroken {
		d.dnsBrokenWindow = now.Add(2 * v6ProbeInterval)
	}
	d.mu.Unlock()
}

// L2RestrictedOnUI 受限可见缝（托盘气泡/通知层用）：与 onlineDetail 的上浮
// 判定同源（受限中或恢复后 2×周期保留期内）。壳层据此决定气泡，不得用
// detail 文案反推。写侧在 probeL2 持锁写、读侧加锁读——Tick 在守护 goroutine
// 写，托盘 tick 在壳层 goroutine 读（2026-11-08 P0：共享字段不裸读裸写）。
func (d *Daemon) L2RestrictedOnUI() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.l2Restricted || d.Now().Before(d.l2WindowUntil)
}

// DnsBrokenOnUI 校园 DNS 解析异常可见缝：异常中或恢复后保留窗口内为真，
// 与 onlineDetail 的上浮判定同源（对称 L2RestrictedOnUI）。
func (d *Daemon) DnsBrokenOnUI() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.dnsBroken || d.Now().Before(d.dnsBrokenWindow)
}

// l2StreakThreshold L2 受限持续多久点名「持续」（可见性升级门槛）。
const l2StreakThreshold = 10 * time.Minute

// userPathStreakThreshold 用户路径异常持续多久才上浮互证附注（决议 B）：
// 系统栈抖一拍就提示会成为噪音；这是互证信号不是判定，让用户知道
//「守护说在线但浏览器可能上不了网」该找代理/TUN，而不是以为守护失职。
const userPathStreakThreshold = 5 * time.Minute

// onlineDetail 在线态详情：L2 受限优先（直接影响体验），校园 DNS 解析异常
// 次之，用户路径互证附注再次，v6 文案殿后。只观察的两层都在这里上浮——
// 只写日志用户看不见（2026-11-05 实测被误当失职）。l1Green 为本拍锚点结论：
// 互证附注只在 L1 绿时说「守护探测走物理栈不受影响」，锚点也红时那句话不成立。
func (d *Daemon) onlineDetail(l1Green bool) string {
	now := d.Now()
	var base string
	if d.l2Restricted || now.Before(d.l2WindowUntil) {
		// 持续 ≥10min 点名（2026-11-21 实测复盘：出口受限 3.5 小时只显示
		//「观察中」，用户无从判断该不该动手）。只改可见性不改判定——
		// 会话真值由 probeSession 独立核实，对抗 DPI 不违规。
		if !d.l2Since.IsZero() && now.Sub(d.l2Since) >= l2StreakThreshold {
			base = "出口受限持续较久（大众站 TLS 被重置≥10 分钟；认证正常；会话由 online_check 独立核实，勿反复重登）"
		} else {
			base = "出口受限（观察：大众站 TLS 被重置；认证正常；勿反复重登）"
		}
	} else if d.DnsBrokenOnUI() {
		// 校园 DNS 解析异常（2026-11-22 复盘内因 1）：campus_dns 给假 IP →
		// 钉假 IP TLS 必死，与真·AC 拉黑（双路都死）区分——分叉时同栈对照
		// 实测可达，真警报不该记在出口头上。
		if !d.dnsBrokenSince.IsZero() && now.Sub(d.dnsBrokenSince) >= l2StreakThreshold {
			base = "校园 DNS 解析异常持续较久（校园 DNS 持续给不可用的解析结果；实测网络可达；认证正常；勿反复重登）"
		} else {
			base = "校园 DNS 解析异常（解析结果不可用；实测网络可达；认证正常；勿反复重登）"
		}
	} else {
		base = d.v6Detail()
	}
	if extra := d.userPathDetail(now, l1Green); extra != "" {
		if base == "" {
			return extra
		}
		return base + "｜" + extra
	}
	return base
}

// userPathDetail 用户路径互证附注（2026-11-21 产品优化决议 B）：L1 绿
//（在线态）而系统栈持续 ≥5 分钟不通时上浮。只做展示，绝不参与判定——
// 转绿/转未知即消；读 userPath 走加锁读（Tick 写、壳层读的同款纪律）。
func (d *Daemon) userPathDetail(now time.Time, l1Green bool) string {
	if !l1Green {
		return "" // 锚点也红：物理栈同样受影响，「不受影响」不成立
	}
	d.mu.RLock()
	known, ok := d.userPath.Known, d.userPath.OK
	d.mu.RUnlock()
	if !known || ok || d.userPathSince.IsZero() || now.Sub(d.userPathSince) < userPathStreakThreshold {
		return ""
	}
	return "用户路径异常（代理/TUN 接管？守护探测走物理栈不受影响）"
}

// probeV6 v6 四态观察，60s 一节流（每拍握手会刷爆日志并高频撞 DPI）。
func (d *Daemon) probeV6() {
	if d.ProbeV6 == nil {
		return
	}
	now := d.Now()
	if !d.v6At.IsZero() && now.Sub(d.v6At) < v6ProbeInterval {
		return
	}
	d.v6At = now
	d.setV6State(d.ProbeV6())
}

// v6Detail 在线态的 v6 展示：S0（无 v6）完全静默，其余附带文案。
func (d *Daemon) v6Detail() string {
	return V6Label(d.V6State)
}

// V6Label v6 四态文案（唯一出处：内核 detail、回路快照 v6_label 都取这里）。
// 绝不把 S1 塌缩成"不可达"（规格 §5.1 / #8 误报）。
func V6Label(state string) string {
	switch state {
	case "online":
		return "IPv6 在线"
	case "offline":
		return "IPv6 未在线（可补绑）"
	case "unobservable":
		return "IPv6 不可观测（本门户无查询入口，或代理接管）"
	default:
		return "" // S0 无 v6 / 未观察：静默
	}
}
