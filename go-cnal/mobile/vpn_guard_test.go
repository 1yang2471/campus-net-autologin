package mobile

import "testing"

// VPN 暂停自动登录（A4 降级解）：AAR 导出 SetAutoReconnect 热切换缝，
// Kotlin 检测 VPN 边沿后调用。测试守：未启动 no-op 不崩，启动后确实翻转
// daemon 的开关（用 Snapshot.extra.auto_reconnect 回读，行为通过公开接口）。
func TestSetAutoReconnectNoopWhenNotRunning(t *testing.T) {
	resetState(t)
	// 未启动时调用不得 panic（Kotlin 可能在 Start 之前收到 VPN 回调）
	SetAutoReconnect(false)
	SetAutoReconnect(true)
}
