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
	if now.Before(d.fluxOffUntil) {
		return false, false
	}
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

// sessionAliveL1RedDetail 认证在线但锚点红时的状态行（双读数的前身：认证
// 读数正常，用户路径/线路异常）。L2 观察作为附属信息，不抢主句。
func (d *Daemon) sessionAliveL1RedDetail() string {
	base := "认证在线（锚点异常，会话正常）"
	if extra := d.onlineDetail(); extra != "" {
		return base + "｜" + extra
	}
	return base
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

// siblingProbeInterval 旁路网卡观察节流：与 L2/v6 同量级（60s）。每拍扫
// 所有非绑定源会被枚举异常放大成请求风暴，且认证态不是心跳。
const siblingProbeInterval = 60 * time.Second

// probeSiblings 观察非绑定物理源的认证态（2026-11-19 多网卡实证：只维护一个
// bind_ip，网线在未认证 VLAN 时不登录、不显示，用户只看到 Windows 黄叹号）。
//
// **异步 + 单飞**（2026-11-19 下午回归修复）：探测本身要走 DNS 解析 + HTTP，
// 未认证 VLAN 上两项都可能吃满超时——每张卡 probe_timeout×2，两张卡就是十几秒。
// 早先同步跑在 Tick 里，把 3 秒心跳拖成十几秒一拍（实测 7s 停顿反复出现）。
// 现在：Tick 只做节流判定与起飞，探测在独立 goroutine 里跑；同一时刻最多一个
// 在飞（siblingBusy），慢探测不会被下一拍叠加成风暴。
func (d *Daemon) probeSiblings() {
	if d.ProbeSiblingAuth == nil {
		return
	}
	now := d.Now()
	if !d.siblingAt.IsZero() && now.Sub(d.siblingAt) < siblingProbeInterval {
		return
	}
	if d.siblingBusy.Swap(true) {
		return // 上一轮还在飞：不叠加，等它自己落地
	}
	d.siblingAt = now
	go func() {
		defer d.siblingBusy.Store(false)
		list := d.ProbeSiblingAuth()
		d.mu.Lock()
		d.siblingUnauth = list
		d.mu.Unlock()
	}()
}

// UnauthSiblings 最近一次观察到的「已连接但未认证」源（面板/回路快照用）。
// 返回副本：调用方在别的 goroutine 读，不得与 Tick 写侧共享底层数组。
func (d *Daemon) UnauthSiblings() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.siblingUnauth) == 0 {
		return nil
	}
	out := make([]string, len(d.siblingUnauth))
	copy(out, d.siblingUnauth)
	return out
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
		return "出口受限（观察：大众站 TLS 被重置；认证正常；勿反复重登）"
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
