package keeper

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// ErrRateCapped 速率闸门关闭（app 层哨兵）：Python 对此显示 offline（非 rejected），
// 内核据此区分「门户回绝」与「本地预算用尽」两种不同语义。
var ErrRateCapped = errors.New("速率闸门")

// freeDetectStreak 连续 N 次「锚点绿+登录不受理」即判定免会话态
// （Python FREE_DETECT_STREAK：第一性原理，免认证时段可直接观测）。
const freeDetectStreak = 2

// 状态名与 Python 守护对齐（跨实现互裁，同语料同语义）。
const (
	StateOnline     = "online"
	StateOffline    = "offline"
	StatePortalDown = "portal_down"
	StateFreeWindow = "free_window"
	// StateRejected 登录节流/退避/熔断中的展示态（Python 守护同名词表）。
	StateRejected = "rejected"
	// StatePaused 自动重连开关关闭时的展示态：只探测报状态，不自动登录
	//（Python set_state("paused","自动重连已关闭") 同语义）。
	StatePaused = "paused"
)

// 登录风暴三闸参数（Python tick 同源值：LOGIN_MIN_INTERVAL=30、suspect 上限 3；
// 退避默认值 reject_backoff=600 / transient_backoff=30，异校可在 ini 覆盖）。
// 外加 SESSION_FALSE 自停闸：矛盾上限 3、自停 3600s。
const (
	loginMinInterval        = 30 * time.Second
	defaultRejectBackoff    = 600 * time.Second
	defaultTransientBackoff = 30 * time.Second
	// transientStreakLimit 瞬态错误连续 N 次升级为长退避（Python
	// handle_rejection：transient_streak >= 5 → reject_backoff）。
	transientStreakLimit = 5
	suspectLimit         = 3
	sessFalseLimit       = 3
	sessFalseDisable     = 3600 * time.Second
	// v6ProbeInterval v6 四态观察节流（同 Python L2_PROBE_S 量级）。
	v6ProbeInterval = 60 * time.Second
	// sessionProbeInterval L3 会话探针节流（Python SESSION_PROBE_S=60 同源）：
	// tick 间隔可低至 1s（用户可配），不节流就是每秒一次 userflux——
	// 2026-11-10 实测 5 拍 5 次请求、2s 即判死（规格 §12「与 60s 的 L2/L3 同拍」）。
	sessionProbeInterval = 60 * time.Second
	// offlineLoginDebounce L1 连续失败去抖（2026-11-13 登录风暴实证）：探测
	// 节拍可低至 1s（用户可配），不节流就是每秒一次锚点探测；单拍超时不应
	// 触发登录，避免登录风暴撞校园反暴力冻结线（校园实测 12 次/小时）。
	offlineLoginDebounce = 3
)

// Daemon 守护内核骨架：探测缝可替换（测试不触网）；登录/会话判死在
// Task G3/G4 接入，UI/托盘/打包在切片三。
type Daemon struct {
	FreeWindows []Window

	// 探测缝（生产实现触网，测试注入替身）
	ProbeOnline func() bool                       // L1 公网锚点（物理栈钉 IP）
	PortalAlive func() bool                       // 门户可达
	FetchFlux   func() (map[string]string, error) // L3 flux 真值（会话存活；err=未知不判死）
	// SessionAlive L3' **会话级**存活（nil = 回落 FetchFlux）。
	//
	// 为什么要单独一条缝（2026-11-11 真机实证）：userflux 是**账号级**读数——
	// 会话被门户释放后它仍回「帐号状态：正常 / 日流量额 4G」，于是判死链一直
	// 认为"会话活着"，守护不会自动重登（实测：注销 v4 后 online_check 已是
	// online:0，守护仍报在线，直到手动 /login 才回来）。
	// online_check 是会话级读数（按请求源地址认客户端），且**无凭据**。
	// 返回 (alive, err)：err != nil = 未知，不推进判死。
	SessionAlive func() (bool, error)
	// Login 重连登录缝（G4；nil = 未接线，离线拍只显示不动作）。
	// 成功时返回门户响应原文（SESSION_FALSE 矛盾检测用），失败返回 err。
	Login func() (string, error)
	// OnPortalDown 门户不可达拍（窗口外）——组合根借此重发现物理源
	// （网络切换/休眠唤醒后旧源失效会表现为长期 portal_down）。
	OnPortalDown func()
	// EgressHealth 服务出口健康（nil = 未接线，按健康处理）：绑定失效/门户
	// 不可达是登录的前置门槛，不健康时绝不发凭据（2026-11-18 三连登录实证）。
	EgressHealth func() EgressHealth
	// OnEgressInvalid 出口失效拍（窗口外）——组合根借此立即重选物理源。
	// 与 OnPortalDown 分开：一个是「本地绑定坏了」，一个是「门户够不着」，
	// 原因与重选时机不同，不合并成一个回调。
	OnEgressInvalid func()
	// ProbeL2 出口策略观察（L2 DPI）：返回 "restricted"（大众站被 RST）/
	// "ok"；nil = 未接线。只观察，绝不参与判定——对抗 DPI 属违规。
	ProbeL2 func() string
	// ProbeV6 v6 对端栈四态观察（规格 §5.1）：返回 "no_address"/"unobservable"/
	// "online"/"offline"；nil = 未接线（不观察、不展示）。
	ProbeV6 func() string
	// OnFluxWarn 流量越阈事件缝（只读、零副作用）：每个阈值档在进程生命周期内
	// 只回调一次，带档位与用量视图。壳层据此发托盘气泡/安卓通知；内核不弹窗、
	// 不改自动连、不断网。nil = 不发。
	OnFluxWarn func(level string, view FluxUsageView)

	// 对外暴露的展示字段：Tick goroutine 写，回路 API Snapshot / 托盘 tick
	// goroutine 读——用 mu 护住（2026-11-08 发布审查 P0：只有 autoReconnect
	// 用了原子，其余全裸读裸写，go test -race 本环境 CGO 不可用跑不了）。
	mu          sync.RWMutex
	State       string
	Detail      string
	SessionDead bool
	// V6State v6 对端栈状态（"" = 未观察或 S0 无 v6）。
	V6State string
	// FreeMode 免会话态（自动检测，t46）：锚点绿+连续 2 次登录不受理学得；
	// 登录恢复受理即退出。零配置，自适应窗口边缘。
	FreeMode bool
	// freeText 免认证时段原文（面板回显用；与 FreeWindows 同锁，保存后回读
	// 不会拿到启动时的旧值）。
	freeText string

	// autoReconnect 自动重连开关（Python auto_reconnect 同语义）：false = 只探测
	// 报状态、不自动登录（手动重连走独立路径不受限）。默认开。并发：Tick 在后台
	// goroutine 读、面板/托盘开关在回路 goroutine 写 → 原子（读写走方法对）。
	autoReconnect atomic.Bool

	// Now 时钟缝（节流/退避测试可控；默认 time.Now）。
	Now func() time.Time
	// LocalMinute 本地时钟缝（免费时段判定用；默认 time.Now 的本地分钟）。
	// gomobile 进程无 TZ 时 time.Now() 挂 UTC，23:30-06:30（本地）会在 UTC
	// 下午被误判「免认证时段」→ 不自动登录（2026-11-11 M1 首跑真机实证）。
	// 安卓由 Kotlin 注入设备时区的当天分钟数。
	LocalMinute func() int

	// marks 响应判定词表（[response] 配置面；SetResponseMarks 只在启动前调用，
	// Tick 单 goroutine 读，无并发写）。
	marks ResponseMarks
	// 退避参数（Python reject_backoff/transient_backoff 可配；启动前 SetBackoff）。
	rejectBackoff    time.Duration
	transientBackoff time.Duration

	transientStreak int // 连续瞬态错误次数（达 5 升级长退避）

	lastLogin     time.Time // 最近一次登录尝试时刻（LOGIN_MIN_INTERVAL 闸）
	rejectUntil   time.Time // 登录退避截止时刻
	frozenUntil   time.Time // 门户限频冻结闸截止（独立于退避，flux 复活不清除）
	suspect       int       // 连续「登录成功但没修好」计数（熔断用）
	sessFalse     int       // 「判死→重登答已在线」矛盾计数（flux 假阳性）
	fluxOffUntil  time.Time // flux 探针自停截止（矛盾超限后 3600s）
	v6At          time.Time // 上次 v6 观察时刻（60s 节流）
	l2At          time.Time // 上次 L2 观察时刻（60s 节流）
	sessAt        time.Time // 上次会话探针时刻（60s 节流）
	// sessRedAt/sessRedKnown/sessRedAlive L1 红时的会话核实缓存（10s 下限）：
	// 用户路径一断立即问一次门户，之后节流期内复用结论，避免每拍打 online_check。
	sessRedAt    time.Time
	sessRedKnown bool
	sessRedAlive bool
	l2Restricted  bool      // 最近一次 L2 观察结果
	l2WindowUntil time.Time // L2 受限提示保留窗口（恢复后仍提示 2×周期）
	offlineStreak int       // L1 连续失败拍数（登录去抖；锚点绿即清零）

	// 流量轮询（只读能力）：10 分钟节流查一次 userflux，供面板常驻显示与
	// 越阈提醒。与会话判死的 flux 探针**分开**——那是 60s 一拍的账号级存活
	// 信号，混用会把「查流量」拖进限频。查询失败保留上次读数。
	fluxPollInterval time.Duration
	fluxAt           time.Time // 上次为「流量视图」主动查询的时刻（10 分钟节流）
	fluxQueriedAt    time.Time // 本轮任意来源拿到 flux 数据的时刻（复用，避免一拍两查）
	fluxView         FluxUsageView
	fluxWarner       *fluxWarner

	sessMiss   int
	freeStreak int
}

func NewDaemon(freeWindows string) *Daemon {
	d := &Daemon{
		FreeWindows:      ParseFreeWindows(freeWindows),
		freeText:         freeWindows,
		Now:              time.Now,
		LocalMinute:      func() int { t := time.Now(); return t.Hour()*60 + t.Minute() },
		marks:            DefaultResponseMarks(),
		rejectBackoff:    defaultRejectBackoff,
		transientBackoff: defaultTransientBackoff,
		fluxPollInterval: defaultFluxPollInterval,
		fluxWarner:       newFluxWarner(defaultFluxWarnAt, defaultFluxCriticalAt),
	}
	d.autoReconnect.Store(true) // Python DEFAULTS auto_reconnect=true
	return d
}

// SetResponseMarks 覆盖响应词表（[response] 配置面；只在 Tick 启动前调用）。
func (d *Daemon) SetResponseMarks(m ResponseMarks) { d.marks = m.withDefaults() }

// SetBackoff 覆盖退避参数（ini reject_backoff/transient_backoff；<=0 保持默认）。
// 只在 Tick 启动前调用——启动后改要另做同步，当前面板没有这两个键的热改。
func (d *Daemon) SetBackoff(reject, transient time.Duration) {
	if reject > 0 {
		d.rejectBackoff = reject
	}
	if transient > 0 {
		d.transientBackoff = transient
	}
}

// SetAutoReconnect 切换自动重连开关（面板/托盘写，Tick 读——原子交接）。
func (d *Daemon) SetAutoReconnect(on bool) { d.autoReconnect.Store(on) }

// AutoReconnectOn 当前开关状态（快照/菜单勾选回显）。
func (d *Daemon) AutoReconnectOn() bool { return d.autoReconnect.Load() }

// Tick 走一拍探测-判态循环（语义对齐 Python keeper.tick 探测层）。
func (d *Daemon) Tick() {
	// 观察层脱离在线分支门槛：v4 掉线期间用户不得瞥见陈旧观察值。
	d.probeV6()
	d.probeL2()
	l1 := d.ProbeOnline()
	if l1 {
		d.offlineStreak = 0
		d.sessRedAt = time.Time{} // L1 恢复即作废红态缓存：下次红拍重新核实
	} else {
		d.offlineStreak++
	}
	if l1 {
		d.probeSession()
		// 流量轮询排在会话探针之后：flux 回落路径刚查过就复用那一读数，
		// 保证一拍最多只打一次 userflux（60s 内 1 次的门户压力红线）。
		d.pollFlux()
		_, _, _, sessionDead, _ := d.View()
		if !sessionDead {
			d.setState(StateOnline, d.onlineDetail())
			return
		}
		// 假在线封堵：锚点绿但会话死（t37）→ 落到重连路径（G4 接登录）
	}
	// 服务出口门槛先于一切登录动作（2026-11-18 三连登录实证）：本地绑定
	// 失效或绑源到不了门户时，登录只会走到另一条路（系统栈）——探针永远
	// 不绿、预算白烧、还会触发自伤熔断。先报状态，让组合根重选出口。
	portalAlive := d.PortalAlive()
	if eg := d.egress(); !eg.Valid {
		if d.inFreeWindow() {
			d.setState(StateFreeWindow, "免认证时段（无需登录）")
			return
		}
		if eg.Reason == EgressPortalUnreachable {
			if d.OnPortalDown != nil {
				d.OnPortalDown()
			}
			d.setState(StatePortalDown, egressDetail(eg))
			return
		}
		if d.OnEgressInvalid != nil {
			d.OnEgressInvalid()
		}
		d.setState(StateOffline, egressDetail(eg))
		return
	}
	if !portalAlive {
		if d.inFreeWindow() {
			d.setState(StateFreeWindow, "免认证时段（无需登录）")
			return
		}
		// 重发现时机交给组合根（Python P2-8：旧自动发现的源会失效）。
		if d.OnPortalDown != nil {
			d.OnPortalDown()
		}
		d.setState(StatePortalDown, "")
		return
	}
	// 自动重连开关（Python 同序：portal_down 之后、免认证/自动登录之前）：
	// 关 = 只探测报状态，不自动登录。手动重连走独立路径（app 层）不受限。
	if !d.autoReconnect.Load() {
		d.setState(StatePaused, "自动重连已关闭")
		return
	}
	if d.inFreeWindow() {
		d.setState(StateFreeWindow, "免认证时段：不自动登录（锚点间歇失败属夜间常态，网络通常可用）")
		return
	}
	// 免会话态（t46）：锚点绿=网络仍可用、此前登录不被受理——停自动登录
	// 省预算；L1 变红即自然放行重连（自适应窗口边缘，无需时钟）。
	_, _, _, _, freeMode := d.View()
	if freeMode && l1 {
		d.setState(StateFreeWindow, "免认证时段（自动检测）：公网可用、门户不受理登录")
		return
	}
	// L1 连续失败去抖（2026-11-13 登录风暴实证）：单拍探测失败如实报离线，
	// 但不触发登录；连续 offlineLoginDebounce 拍失败才进入重连。计数在锚点
	// 恢复时清零（短暂抖动不累积）。会话判死不在此列——它走 L1 绿分支。
	//
	// 2026-11-18 修正：L1 红时先核实门户会话真值——门户说在线就不登录
	//（锚点红是 L2/线路问题，登录修不了），说没登录才允许登录；真值不可得
	// 才回落三拍去抖。
	now := d.Now()
	if !l1 {
		if dead, known := d.probeSessionOnL1Red(now); known {
			if !dead {
				d.offlineStreak = 0
				d.setState(StateOnline, d.sessionAliveL1RedDetail())
				return
			}
			// 会话真值是权威证据：跳过 L1 三拍去抖，直接进登录闸。
			d.offlineStreak = offlineLoginDebounce
		}
	}
	if !l1 && d.offlineStreak < offlineLoginDebounce {
		d.setState(StateOffline, "")
		return
	}
	// 登录风暴三闸（Python tick 同序：退避先于全局节流）。
	if now.Before(d.rejectUntil) || now.Before(d.frozenUntil) {
		d.setState(StateRejected, "退避中：上次登录未修好问题，暂停重连")
		return
	}
	if d.lastLogin.Add(loginMinInterval).After(now) {
		d.setState(StateRejected, "登录节流中（防风暴）")
		return
	}
	// 重连点：离线拍（锚点红真掉线 / 会话判死假在线落到此）。成功点亮在线
	// 并清判死与检测态（Python：重连成功即解除 flux 判死）；失败时锚点绿下
	// 的不受理是免会话态的直接证据——连续 2 次即学得（FREE_DETECT_STREAK）。
	if d.Login != nil {
		d.lastLogin = now
		_, _, _, wasDead, _ := d.View()
		resp, err := d.Login()
		if err == nil {
			d.setSessionDead(false)
			d.setFreeMode(false)
			d.freeStreak = 0
			d.invalidateSessionCache() // 登录后会话已变：下次 L1 红须重新问门户
			// SESSION_FALSE 自停闸：判死→重登→门户答「已在线」= flux 在撒谎，
			// 矛盾 ×3 → 探针自停 3600s（假判死不得变成登录风暴）。
			if wasDead && alreadyOnline(resp) {
				d.sessFalse++
				if d.sessFalse >= sessFalseLimit {
					d.fluxOffUntil = now.Add(sessFalseDisable)
					d.sessFalse = 0
				}
			}
			d.suspect++
			if d.suspect >= suspectLimit {
				d.rejectUntil = now.Add(d.rejectBackoff)
				d.suspect = 0
				d.setState(StateRejected, "连续 3 次登录成功但探测仍失败——疑似误判，熔断退避")
				return
			}
			// 判定链健康：清退避与瞬态连击（Python 成功分支 reject_until=0、
			// transient_streak=0 同源）。
			d.rejectUntil = time.Time{}
			d.transientStreak = 0
			d.setState(StateOnline, "")
			return
		} else {
			// 出口在门槛放行后失效：这次登录没有发出，不能算一次尝试——
			// 清掉 lastLogin、不进退避/熔断，报状态并请组合根重选。
			if errors.Is(err, ErrEgressInvalid) {
				d.lastLogin = time.Time{}
				d.setState(StateOffline, err.Error())
				if d.OnEgressInvalid != nil {
					d.OnEgressInvalid()
				}
				return
			}
			if errors.Is(err, ErrEgressPortalUnreachable) {
				d.lastLogin = time.Time{}
				d.setState(StatePortalDown, err.Error())
				if d.OnPortalDown != nil {
					d.OnPortalDown()
				}
				return
			}
			// 门户限频冻结：独立闸门（Python _frozen_until 同源）——普通退避
			// 会被 flux 复活清零，冻结闸不会，否则「抖动→放行→再撞→更深冻结」。
			if frozen, wait := freezeWait(err.Error(), d.marks, d.rejectBackoff); frozen {
				d.frozenUntil = now.Add(wait)
				d.rejectUntil = now.Add(wait)
				d.setState(StateRejected, err.Error())
				return
			}
			// 速率闸门（app 层哨兵）：Python 显示 offline 而非 rejected。
			if errors.Is(err, ErrRateCapped) {
				d.rejectUntil = now.Add(d.transientBackoff)
				d.setState(StateOffline, err.Error())
				return
			}
			// 拒绝分类退避（Python handle_rejection 同源）：瞬态错误短退避快速
			// 重试（防明早时段切换卡 10 分钟），连续 5 次升级长退避；硬错误
			// （密码/额度/锁定）直接长退避。
			wait := d.rejectBackoff
			if d.marks.transientMarked(err.Error()) {
				d.transientStreak++
				if d.transientStreak < transientStreakLimit {
					wait = d.transientBackoff
				}
			} else {
				d.transientStreak = 0
			}
			d.rejectUntil = now.Add(wait)
			if l1 {
				d.freeStreak++
				if d.freeStreak >= freeDetectStreak {
					d.setFreeMode(true)
					d.setState(StateFreeWindow, "免认证时段（自动检测）：公网可用、门户不受理登录")
					return
				}
			}
			d.setState(StateRejected, err.Error())
			return
		}
	}
	d.setState(StateOffline, "")
}

// 免认证时段的三个方法（inFreeWindow / SetFreeWindows / FreeWindowsText）
// 住在 freewindow.go——那里是时段语义的归属，也让本文件守住 20KB 提交闸。

func (d *Daemon) setState(state, detail string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.State = state
	d.Detail = detail
}

// View 展示字段快照（回路 API / 托盘用）：加锁读，与 Tick 的 setState 写配对。
// 值拷贝返回，调用方拿到的不再是共享内存。
func (d *Daemon) View() (state, detail, v6State string, sessionDead, freeMode bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.State, d.Detail, d.V6State, d.SessionDead, d.FreeMode
}

// StateForUI 只要状态名（托盘 tick 循环判迁移用，避免裸读共享字段）。
func (d *Daemon) StateForUI() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.State
}
