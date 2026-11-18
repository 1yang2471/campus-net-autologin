package mobile

import (
	"testing"
	"time"
)

// 时区偏移缝（2026-11-11 M1 首跑实证：gomobile 进程无 TZ，time.Now 挂 UTC，
// 免费时段按 UTC 判定会把大下午判成「免认证时段」）。Kotlin 注入设备当前
// 偏移分钟（Asia/Shanghai = +480），Go 用 UTC+偏移取本地分钟。

func TestSetTZOffsetMinShiftsLocalMinute(t *testing.T) {
	SetTZOffsetMin(480) // Asia/Shanghai
	got := localMinuteFromTZ()
	// 手机/测试机的真实偏移未知，但必须等于 time.Now() 在该偏移下的分钟。
	want := time.Now().UTC().Add(480 * time.Minute)
	if got != want.Hour()*60+want.Minute() {
		t.Errorf("偏移 480 的本地分钟 = %d, 期望 %d", got, want.Hour()*60+want.Minute())
	}
}

func TestSetTZOffsetMinNegative(t *testing.T) {
	SetTZOffsetMin(-300) // UTC-5
	got := localMinuteFromTZ()
	want := time.Now().UTC().Add(-300 * time.Minute)
	if got != want.Hour()*60+want.Minute() {
		t.Errorf("偏移 -300 的本地分钟 = %d, 期望 %d", got, want.Hour()*60+want.Minute())
	}
}
