package keeper

import "time"

// 从 daemon.go 拆出（2026-11-22，守 2 万字节提交闸——tidy §3 同款纪律；
// 拆分归属说明见 daemon.go 尾注）。内核参数与状态词：跨实现互裁锚点，
// 语义不变。

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
