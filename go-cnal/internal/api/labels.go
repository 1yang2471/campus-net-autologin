package api

import (
	"strings"

	"cnal/internal/keeper"
)

// 从 api.go 拆出（2026-11-22，守 2 万字节提交闸——tidy §3 同款纪律）：
// 词表与渲染规则——壳层不自造词表的家规锚点，语义不变。

// stateLabels/stateColors 与 Python shell/kernel_api.py 逐字对齐（互裁锚点）。
var stateLabels = map[string]string{
	"online":      "在线",
	"offline":     "离线",
	"portal_down": "认证服务器不可达",
	"rejected":    "登录被拒",
	"paused":      "自动重连已关闭",
	"starting":    "启动中",
	"free_window": "免认证时段",
}

var stateColors = map[string]string{
	"online":      "#4f9d63",
	"offline":     "#d1913f",
	"portal_down": "#d1913f",
	"rejected":    "#d05f52",
	"paused":      "#8b919b",
	"starting":    "#8b919b",
	"free_window": "#8b919b",
}

// reasonLabels/reasonColors 机器原因 → 显示词表（2026-11-18 双读数）：
// 只覆盖「状态名本身会撒谎」的几种。典型：本地 30 秒闸原来顶着 rejected
// 的「登录被拒」红字，其实是守护自己暂停，不是门户拒绝。
var reasonLabels = map[string]string{
	"local_throttle":     "自动登录暂停",
	"binding_invalid":    "校园出口不可用",
	"portal_unreachable": "校园出口不可达",
	// AC 假在线组合态（2026-11-22 复盘内因 2）：「在线」打头的误导是本次
	// 事故的痛点根源，主状态词直接说破。
	"fake_online": "会话在线｜出口被控",
}

var reasonColors = map[string]string{
	"local_throttle":     "#8b919b",
	"binding_invalid":    "#d1913f",
	"portal_unreachable": "#d1913f",
	"fake_online":        "#d1913f",
}

// reasonTexts 原因的一句话说明（面板双读数行用；状态名不重复）。
var reasonTexts = map[string]string{
	"binding_invalid":      "服务出口绑定失效",
	"portal_unreachable":   "绑源到不了门户",
	"local_throttle":       "本地节流中",
	"portal_rejected":      "门户拒绝登录",
	"l2_observed":          "出口受限（观察）",
	"l1_red_session_alive": "锚点异常，会话在线",
	"dns_broken_observed":  "校园 DNS 解析异常",
	"fake_online":          "门户会话在线，校园出口被控：重连救不了，等待恢复或换网络，可查梯子残留",
	"session_dead":         "会话不在线",
	"portal_down":          "认证服务器不可达",
}

// ReasonLabel 原因对应的显示标签（覆盖状态名；无覆盖时 ok=false）。
func ReasonLabel(reason string) (string, bool) {
	v, ok := reasonLabels[reason]
	return v, ok
}

// ReasonColor 原因对应的颜色（覆盖状态色；无覆盖时 ok=false）。
func ReasonColor(reason string) (string, bool) {
	v, ok := reasonColors[reason]
	return v, ok
}

// ReasonText 原因的一句话说明（无则空串；壳层不自造词表）。
func ReasonText(reason string) string { return reasonTexts[reason] }

// Color 导出状态色（未知状态回退中性灰）——装配层（托盘图标/面板图标）
// 取色的唯一入口，闭包不得自抄词表（2026-11-07 实证：漏填导致图标空白）。
func Color(state string) string {
	if v, ok := stateColors[state]; ok {
		return v
	}
	return stateColors["paused"]
}

// Label 导出词表访问（未知状态回退状态名本身，Python kernel_api 同语义）。
func Label(state string) string {
	if v, ok := stateLabels[state]; ok {
		return v
	}
	return state
}

// MenuLine 托盘菜单行短文案（单源渲染，2026-11-22 复盘修复项 4）：主状态词 +
// 详情第一段（「｜」与「（」之前）——括号补充说明不进菜单行，完整读数留给
// tooltip 与面板。规则一处定稿，托盘/气泡等消费方不自造截断。
func MenuLine(label, detail, userPath string) string {
	line := label
	if detail != "" {
		line += "｜" + firstSegment(detail)
	}
	if userPath == keeper.PathBroken {
		line += "｜用户路径不通"
	}
	return line
}

// firstSegment 详情第一段：先取「｜」之前（多段详情只留首段），再取「（」
// 之前（首段的括号补充说明不进菜单行）。
func firstSegment(detail string) string {
	if i := strings.IndexRune(detail, '｜'); i >= 0 {
		detail = detail[:i]
	}
	if i := strings.IndexRune(detail, '（'); i >= 0 {
		detail = detail[:i]
	}
	return strings.TrimSpace(detail)
}
