package protocol

import "cnal/internal/dialect"

// StackState 是四态而非三态（规格 §5.1）：不可观测绝不塌缩成"不可达"。
type StackState string

const (
	// S0：本机无全局 v6 → 完全静默
	StateNoAddress StackState = "no_address"
	// S1：观测不到 → 文案"不可观测"，禁止据此行动
	StateUnobservable StackState = "unobservable"
	// S2a
	StatePeerOnline StackState = "online"
	// S2b：唯一允许补绑的态
	StatePeerOffline StackState = "offline"
)

// InterpretHandshake 把握手响应读成状态。success=false 或缺键 → 不可观测，
// 不是"未在线"——误报成后者会引发无意义补绑（撞门户反暴力冻结）。
func InterpretHandshake(raw string) StackState {
	m, _ := dialect.Parse(raw)
	ok, _ := m["success"].(bool)
	if !ok {
		return StateUnobservable
	}
	online, present := m["isOnline"].(bool)
	if !present {
		return StateUnobservable
	}
	if online {
		return StatePeerOnline
	}
	return StatePeerOffline
}
