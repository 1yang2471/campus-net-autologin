package keeper

import "time"

// 观测缝（v6 四态 / L2 出口 / L3 会话）：从 daemon.go 拆出——单文件守
// 2 万字节提交闸（tidy §3），语义仍是「观察层」，不参与状态机骨架。

func (d *Daemon) probeSession() {
	if d.SessionAlive == nil && d.FetchFlux == nil {
		return
	}
	now := d.Now()
	if now.Before(d.fluxOffUntil) {
		return // SESSION_FALSE 自停期内：探针闭嘴，不再产生假判死
	}
	if !d.sessAt.IsZero() && now.Sub(d.sessAt) < sessionProbeInterval {
		return // 60s 节流（同 l2At/v6At）：会话真值不是心跳
	}
	d.sessAt = now
	var (
		alive bool
		err   error
	)
	if d.SessionAlive != nil {
		// 会话级优先：flux 是账号级读数，会话被释放后它照样有数据（11-11 实证）。
		alive, err = d.SessionAlive()
	} else if d.FetchFlux != nil {
		var flux map[string]string
		flux, err = d.FetchFlux()
		alive = len(flux) > 0
		if err == nil {
			// 复用这一次读数给流量视图，避免同拍 pollFlux 再打一次门户。
			d.updateFluxView(flux, now)
		}
	}
	if err != nil {
		return
	}
	if alive {
		d.sessMiss = 0
		d.setSessionDead(false)
		d.suspect = 0               // 会话活 = 判定链健康证明（Python 在线分支清 suspect）
		d.rejectUntil = time.Time{} // 同源：在线分支清 reject_until
		d.sessFalse = 0             // 拿得到真值 = 门户支持该探针且会话健康
		return
	}
	d.sessMiss++
	if d.sessMiss >= d.sessionMissThreshold() {
		d.setSessionDead(true)
	}
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
	restricted := d.ProbeL2() == "restricted"
	d.mu.Lock()
	d.l2Restricted = restricted
	if restricted {
		d.l2WindowUntil = now.Add(2 * v6ProbeInterval)
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

// onlineDetail 在线态详情：L2 受限优先（直接影响体验），v6 文案次之。
// 只观察的两层都在这里上浮——只写日志用户看不见（2026-11-05 实测被误当失职）。
func (d *Daemon) onlineDetail() string {
	now := d.Now()
	if d.l2Restricted || now.Before(d.l2WindowUntil) {
		return "出口受限（DPI 风控窗口，约 15 分钟自愈；认证正常）"
	}
	return d.v6Detail()
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
