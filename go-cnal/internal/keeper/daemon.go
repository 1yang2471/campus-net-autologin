package keeper

import (
	"sync"
	"sync/atomic"
	"time"
)

// 内核参数与状态词常量住在 params.go（2026-11-22 拆分，守 2 万字节提交闸）；
// 免认证时段方法在 freewindow.go；展示字段读写与开关交接在 daemon_view.go；
// 动作时间线在 daemon_action.go——语义归属见各文件头。

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
	// UserPath 用户路径读数（系统栈，与浏览器同路；nil = 未接线，显示未知）。
	// 只做展示，绝不参与判定——TUN/代理下它反映用户实际体验，不是认证真值。
	UserPath func() PathReading
	// OnFluxWarn 流量越阈事件缝（只读、零副作用）：每个阈值档在进程生命周期内
	// 只回调一次，带档位与用量视图。壳层据此发托盘气泡/安卓通知；内核不弹窗、
	// 不改自动连、不断网。nil = 不发。
	OnFluxWarn func(level string, view FluxUsageView)
	// Channels 多通道观察器（2026-11-20 双通道方案 M1）：nil = 不观察。
	// 只观察、零凭据——通道级登录是 M2，且默认关（spec §8.2）。
	Channels *ChannelGuard
	// ChannelLogin 通道级登录缝（M2）：对指定通道发起登录（绑该通道源 IP）。
	// nil = 未接线（不动作）。与主链路 Login 共用账号级闸门（见 channel_login.go）。
	ChannelLogin ChannelLoginFn

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
	// userPath 用户路径读数（系统栈；mu 护住，Tick 写、面板/托盘读）。
	userPath PathReading
	// lastAction 最近一次守护动作（2026-11-21 产品优化决议 C）：断网排查时
	// 用户第一眼要知道守护动没动过。写入点含 API goroutine（切账号），
	// 加锁读写（方法对在 daemon_action.go）。
	lastAction struct {
		Kind string
		At   time.Time
	}

	// autoReconnect 自动重连开关（Python auto_reconnect 同语义）：false = 只探测
	// 报状态、不自动登录（手动重连走独立路径不受限）。默认开。并发：Tick 在后台
	// goroutine 读、面板/托盘开关在回路 goroutine 写 → 原子（读写走方法对）。
	autoReconnect atomic.Bool
	// multiChannel 通道级自愈开关（2026-11-20 双通道 M2）：默认**关**
	// （沿用 11-19「旁路只观察不登录」的保守纪律）。开启后通道登录与主链路
	// 共用账号级预算（见 channel_login.go）。
	multiChannel atomic.Bool
	// channelLastLogin 各通道最近一次登录尝试时刻（同一通道的最小尝试间隔）。
	// 与主链路的 lastLogin 分开：主链路的 30s 闸是账号级共享的，这里的
	// 60s 是「别让一条抖动通道反复烧预算」的通道级护栏。
	channelLastLogin map[string]time.Time
	// channelReject/channelFrozen 通道级退避与冻结闸（账号级：门户拒的是账号，
	// 任一通道被拒，所有通道一起退避）。与主链路的 rejectUntil/frozenUntil
	// **分账**：主链路的「会话仍活」分支会清 rejectUntil，会连带抹掉通道的
	// 退避（2026-11-20 实现时踩到：退避刚设上就被抹，通道继续撞门户）。
	channelReject time.Time
	channelFrozen time.Time

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

	lastLogin     time.Time   // 最近一次登录尝试时刻（LOGIN_MIN_INTERVAL 闸）
	rejectUntil   time.Time   // 登录退避截止时刻
	frozenUntil   time.Time   // 门户限频冻结闸截止（独立于退避，flux 复活不清除）
	suspect       int         // 连续「登录成功但没修好」计数（熔断用）
	sessFalse     int         // 「判死→重登答已在线」矛盾计数（flux 假阳性）
	fluxOffUntil  time.Time   // flux 探针自停截止（矛盾超限后 3600s）
	v6At          time.Time   // 上次 v6 观察时刻（60s 节流）
	l2At          time.Time   // 上次 L2 观察时刻（60s 节流）
	sessAt        time.Time   // 上次会话探针时刻（60s 节流）
	sessRedAt     time.Time   // L1 红时会话核实时刻（10s 下限）
	// wasFreeWindow 上一拍的免认证窗口状态（边沿检测）：窗口刚结束的首拍
	// 作废探针节流，立即核实会话——否则会误报在线最多 60 秒（2026-11-20）。
	wasFreeWindow bool
	sessRedKnown  bool        // L1 红时会话真值是否可得
	sessRedAlive  bool        // L1 红时最近一次会话真值
	userPathAt    time.Time   // 上次用户路径探测时刻（5s 节流）
	// userPathSince 本轮用户路径红起点（0 = 不在异常中）。混合纪律：跨
	// goroutine 读者（fakeOnline）走锁；Tick 内读者（onlineDetail/userPathDetail）
	// 裸读——勿把这些函数迁出 Tick goroutine（2026-11-22 修复项 2 起 mu 护住）。
	// ≥5 分钟才上浮在线态互证附注（决议 B，见 daemon_probe.go）。
	userPathSince time.Time
	l2Restricted  bool        // 最近一次 L2 观察结果
	l2WindowUntil time.Time   // L2 受限提示保留窗口（恢复后仍提示 2×周期）
	l2Since       time.Time   // 本轮 L2 受限起点（0 = 不在受限中；锁纪律同 userPathSince）
	// dnsBroken 族：L2 同栈对照发现「校园 DNS 给不可用解析结果」的可见态
	//（2026-11-22 复盘内因 1：campus_dns 返回假 IP → 钉假 IP TLS 必死 →
	// 4.5h 假警报，与真·AC 拉黑混为一谈）。记账与 l2 族对称，锁纪律同上。
	dnsBroken       bool      // 最近一次 L2 同栈对照：校园 DNS 解析异常
	dnsBrokenWindow time.Time // 提示保留窗口（恢复后仍提示 2×周期，同 l2WindowUntil）
	dnsBrokenSince  time.Time // 本轮起点（0 = 不在异常中）
	// l1RedSessionAlive 锚点红+会话绿在线态的结构化标记：setStateL1Red 置位、
	// setState 清位——health.go 的 Reason 判定不再 Contains(detail,"锚点异常")
	// 文本反推（文案即接口的脆弱反推，2026-11-22 复盘修复项 2 根治）。
	l1RedSessionAlive bool
	offlineStreak int         // L1 连续失败拍数（登录去抖；锚点绿即清零）
	// 休眠观察（2026-11-21 决议 A；语义与阈值在 daemon_dormant.go）：portal_down
	// 长期持续（蜂窝热点形态）时降频观察，不再每拍探测+每拍重选白烧。
	portalDownSince time.Time // 本轮 portal_down 起点（Tick 同 goroutine 读写）
	portalAliveAt   time.Time // 门户探针节流起点（休眠期 ≥60s 一探）
	dormant         bool      // 休眠观察中：停重选回调、探针降频

	// 流量轮询（只读能力）：10 分钟节流查一次 userflux，供面板常驻显示与
	// 越阈提醒。与会话判死的 flux 探针**分开**——那是 60s 一拍的账号级存活
	// 信号，混用会把「查流量」拖进限频。查询失败保留上次读数。
	fluxPollInterval time.Duration
	fluxAt           time.Time // 上次为「流量视图」主动查询的时刻（10 分钟节流）
	fluxQueriedAt    time.Time // 本轮任意来源拿到 flux 数据的时刻（复用，避免一拍两查）
	fluxView   FluxUsageView
	fluxWarner *fluxWarner
	// fluxReset 作废流量视图的跨 goroutine 请求（切账号后旧读数不得续显）。
	// fluxPending 外部实时快照交接槽（StoreFluxView 挂入，pollFlux 落账）。
	// 两者都只把跨 goroutine 动作收进原子结构，清空/落账全在 Tick 做——
	// fluxAt/fluxQueriedAt 始终只有 Tick 一个写者（P0-1：裸写可崩）。
	fluxReset   atomic.Bool
	fluxPending atomic.Pointer[map[string]string]

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
		channelLastLogin:  map[string]time.Time{},
	}
	d.autoReconnect.Store(true) // Python DEFAULTS auto_reconnect=true
	return d
}

// Tick 走一拍探测-判态循环（语义对齐 Python keeper.tick 探测层）。
func (d *Daemon) Tick() {
	// 观察层脱离在线分支门槛：v4 掉线期间用户不得瞥见陈旧观察值。
	d.probeV6()
	d.probeL2()
	d.probeUserPath()
	// 免认证窗口结束的边沿：作废探针节流立即核实（语义在 freewindow.go）。
	// 先于通道观察：同一边沿也要作废通道节流（2026-11-21 决议 D），否则
	// 边沿那一拍通道先被 60s 节流拦下，重探要再等一拍。
	d.recheckOnFreeWindowEnd(d.inFreeWindow())
	// 多通道观察同样脱离判定：它是「别的卡是什么状态」的事实，与主链路
	// 在线与否无关；探测异步跑，不拖心跳（见 channel.go）。
	d.Channels.tick()
	l1 := d.ProbeOnline()
	if l1 {
		d.offlineStreak = 0
		d.sessRedAt = time.Time{} // L1 恢复即作废红态缓存：下次红拍重新核实
		// 锚点绿 = 离开 portal_down 形态（决议 A 的恢复条件之一）：
		// 清休眠观察，恢复正常节拍（热点上锚点可达则根本不该休眠）。
		d.leavePortalDown()
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
			// 通道级自愈（2026-11-20 双通道 M2）：**主链路健康时**才轮到通道。
			// 30s 闸门只有一份，预算归主——主链路掉线时若通道抢闸，主恢复
			// 会被拖慢 30 秒；11-20 的场景本来就是「WiFi 在线、有线未认证」，
			// 反过来（主掉线时通道先登）没有对应场景。
			d.tryChannelLogin(d.Now())
			d.setState(StateOnline, d.onlineDetail(true))
			return
		}
		// 假在线封堵：锚点绿但会话死（t37）→ 落到重连路径（G4 接登录）
	}
	// 服务出口门槛先于一切登录动作（2026-11-18 三连登录实证）：本地绑定
	// 失效或绑源到不了门户时，登录只会走到另一条路（系统栈）——探针永远
	// 不绿、预算白烧、还会触发自伤熔断。先报状态，让组合根重选出口。
	// 探针在休眠观察期降频（决议 A，语义在 daemon_dormant.go）。
	portalAlive := d.portalAliveThrottled()
	if eg := d.egress(); !eg.Valid {
		if d.inFreeWindow() {
			d.setState(StateFreeWindow, "免认证时段（无需登录）")
			return
		}
		if eg.Reason == EgressPortalUnreachable {
			d.portalDownTick() // 记拍 + 休眠判定（决议 A）
			// 休眠观察期停调重发现：门户持续不可达 10 分钟后每拍重选只是
			// 在热点上乱跳白烧（2026-11-21 09:52 实证）。
			if !d.dormant && d.OnPortalDown != nil {
				d.OnPortalDown()
			}
			d.setState(StatePortalDown, d.portalDownDetail(egressDetail(eg)))
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
		d.portalDownTick() // 记拍 + 休眠判定（决议 A）
		// 重发现时机交给组合根（Python P2-8：旧自动发现的源会失效）；
		// 休眠观察期停调（同上）。
		if !d.dormant && d.OnPortalDown != nil {
			d.OnPortalDown()
		}
		d.setState(StatePortalDown, d.portalDownDetail(""))
		return
	}
	// 门户探针转绿的第一拍：清休眠观察，恢复正常节拍（决议 A 的恢复判定）。
	d.leavePortalDown()
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
				d.setStateL1Red(d.sessionAliveL1RedDetail())
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
		d.setState(StateRejected, "自动登录已暂停（本地节流，30 秒后重试）")
		return
	}
	// 重连点：离线拍（锚点红真掉线 / 会话判死假在线落到此）。成功点亮在线
	// 并清判死与检测态；失败时锚点绿下的不受理是免会话态的直接证据——连续
	// 2 次即学得（FREE_DETECT_STREAK）。分支细节在 daemon_login.go。
	if d.Login != nil {
		d.doLogin(now, l1)
		return
	}
	d.setState(StateOffline, "")
}

// 免认证时段的三个方法（inFreeWindow / SetFreeWindows / FreeWindowsText）
// 住在 freewindow.go——那里是时段语义的归属；展示字段读写与开关交接在
// daemon_view.go；动作时间线（决议 C）在 daemon_action.go——都是守 20KB
// 提交闸的拆分，语义归属见各文件头。
