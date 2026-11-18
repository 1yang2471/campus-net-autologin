package keeper

import (
	"testing"
	"time"
)

// 时区缝（2026-11-11 M1 首跑真机实证的 bug）：gomobile 进程无 TZ 时
// time.Now() 挂 UTC，免费时段 23:30-06:30（本地）在 UTC 下午被判「免认证
// 时段」，守护不自动登录——真实掉线时直接阻断重连。
//
// 行为：免费时段判定必须走注入的本地时钟缝（LocalMinute），不直接调
// time.Now()。Windows/桌面默认缝=系统本地；安卓由 Kotlin 注入时区偏移。

// 行为：注入「永远正午（本地 12:00）」→ 23:30-06:30 窗口判定为不在窗口内，
// 即便进程时区是 UTC（time.Now 的 UTC 下午 ≠ 本地 12:00）。
func TestInFreeWindowUsesInjectedLocalMinute(t *testing.T) {
	d := NewDaemon("23:30-06:30")
	d.LocalMinute = func() int { return 12 * 60 } // 本地正午
	if d.inFreeWindow() {
		t.Fatal("本地 12:00 不在 23:30-06:30 内，不得判免认证（UTC 时区下 time.Now 会判错）")
	}
}

// 行为：注入「本地凌晨 02:00」→ 判在窗口内（跨午夜窗口回绕判定仍成立）。
func TestInFreeWindowInjectedInsideCrossMidnight(t *testing.T) {
	d := NewDaemon("23:30-06:30")
	d.LocalMinute = func() int { return 2 * 60 }
	if !d.inFreeWindow() {
		t.Fatal("本地 02:00 应在 23:30-06:30 内")
	}
}

// 行为：未注入缝时回落 time.Now() 本地分钟（Windows/桌面零改动）。
func TestLocalMinuteDefaultsToTimeNow(t *testing.T) {
	d := NewDaemon("23:30-06:30")
	got := d.LocalMinute()
	now := time.Now()
	want := now.Hour()*60 + now.Minute()
	if got != want {
		// 整分边界竞态容忍 ±1 分钟。
		diff := got - want
		if diff < 0 {
			diff = -diff
		}
		if diff > 1 {
			t.Fatalf("默认缝应取 time.Now 本地分钟：got %d want %d", got, want)
		}
	}
}
