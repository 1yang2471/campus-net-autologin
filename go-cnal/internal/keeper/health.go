package keeper

import "strings"

// 双读数（2026-11-18 产品拍板）：状态必须同时回答两件事——
//   - 认证读数：绑物理栈问门户，这条校园出口的会话是什么状态；
//   - 用户路径读数：系统栈问公网锚点，浏览器实际走的那条路通不通。
// 两者可能不一致（TUN/热点/多默认路由），不一致时必须并列说，不合并成一句。
// 读数只做展示；判定仍走 egressHealth / 会话真值 / L1。

// PathReading 用户路径读数（系统栈，与浏览器同路）。
// Known=false 表示未接线或本次无法判定——不能误报「不通」。
type PathReading struct {
	OK     bool
	Known  bool
	Detail string
}

// HealthView 双读数快照（面板/托盘/回路 API 的唯一出处）。
type HealthView struct {
	Auth       string // online|offline|unknown
	UserPath   string // ok|broken|unknown
	Reason     string // binding_invalid|portal_unreachable|local_throttle|l2_observed|...
	Action     string // none|probing|reselecting|cooldown
	Egress     EgressHealth
	UserDetail string
}

const (
	AuthOnline    = "online"
	AuthOffline   = "offline"
	AuthUnknown   = "unknown"
	PathOK        = "ok"
	PathBroken    = "broken"
	PathUnknown   = "unknown"
	ActionNone    = "none"
	ActionProbing = "probing"
	// ActionReselecting 出口不可用，正在重选物理源（不发凭据）。
	ActionReselecting = "reselecting"
	// ActionCooldown 登录被本地/门户闸门暂停（退避、30s 节流、冻结）。
	ActionCooldown = "cooldown"
)

// Health 读双读数快照。状态/详情/用户路径加锁读；egress 缝自己加锁。
func (d *Daemon) Health() HealthView {
	d.mu.RLock()
	state, detail, path := d.State, d.Detail, d.userPath
	d.mu.RUnlock()

	eg := d.egress()
	h := HealthView{
		Auth:       AuthUnknown,
		UserPath:   PathUnknown,
		Action:     ActionNone,
		Egress:     eg,
		UserDetail: path.Detail,
	}
	if path.Known {
		if path.OK {
			h.UserPath = PathOK
		} else {
			h.UserPath = PathBroken
		}
	}
	switch state {
	case StateOnline:
		h.Auth = AuthOnline
		h.Reason = "online"
		if d.L2RestrictedOnUI() {
			h.Reason = "l2_observed"
		}
		if strings.Contains(detail, "锚点异常") {
			h.Reason = "l1_red_session_alive"
		}
	case StateOffline:
		h.Auth = AuthOffline
		h.Action = ActionProbing
		h.Reason = "session_dead"
		if !eg.Valid {
			h.Action = ActionReselecting
			h.Reason = string(eg.Reason)
		}
	case StatePortalDown:
		h.Auth = AuthUnknown
		h.Action = ActionProbing
		h.Reason = "portal_down"
		if !eg.Valid && eg.Reason == EgressPortalUnreachable {
			h.Action = ActionReselecting
			h.Reason = "portal_unreachable"
		}
	case StateRejected:
		h.Auth = AuthOffline
		h.Action = ActionCooldown
		h.Reason = "portal_rejected"
		if strings.Contains(detail, "本地节流") || strings.Contains(detail, "节流中（防风暴）") {
			h.Auth = AuthUnknown
			h.Reason = "local_throttle"
		}
	case StatePaused, StateFreeWindow:
		h.Reason = "no_action"
	}
	return h
}
