package mobile

import (
	"time"
)

// 事件驱动唤醒（复盘 §3.2 / 分支 5 拍板）：A1「踢线后 ≤5s 恢复」与 3s
// 心跳数学上不兼容，正解 = 系统网络回调即时唤醒 + 低频兜底。本文件是 Go
// 侧收口：Kotlin NetworkCallback 调 Wake(reason)，beatLoop 被打断立即跑
// 完整 Tick；门户静默踢线（Wi-Fi 不断、回调不响）由兜底周期兜住。

// wakeBuffer 唤醒通道缓冲：回调风暴（切网瞬间 onLost/onAvailable/
// onCapabilitiesChanged 连发）不丢、投递方（Kotlin 线程）不阻塞。
const wakeBuffer = 16

// postWake 非阻塞投递一个唤醒原因；缓冲满则丢弃（下一拍 Tick 反正会跑，
// 丢弃 reason 不丢正确性——Wake 只是「尽快 Tick」的加速器）。
func postWake(wake chan<- string, reason string) {
	select {
	case wake <- reason:
	default:
	}
}

// beatWait 等一拍：唤醒打断（立即返回 0）或兜底计时到期（返回完整周期
// delay）。stop 必须能打断等待——Stop() 要立即生效，不能等满兜底计时
// （2026-11-13 测试发现 beatWait 不感知 stop 导致 Stop 挂起直至计时结束，
// 旧 tickLoop 的 select 带 stop 分支无此问题）。
func beatWait(stop <-chan struct{}, wake <-chan string, floor time.Duration) time.Duration {
	select {
	case <-wake:
		return 0
	case <-time.After(floor):
		return floor
	case <-stop:
		return 0
	}
}
