package mobile

import (
	"testing"
	"time"
)

// 日志时间戳时区：TransitionLogger 的 now 缝在桌面传 time.Now（系统本地），
// 在 gomobile 里 time.Now 挂 UTC——真机日志 22:58（本地）被打成 14:58，
// 复盘时和手机截图对不上（2026-11-11 M1 白名单轮现场实证）。
// tzNow 用注入偏移渲染本地时间；免费时段判定（localMinuteFromTZ）同源。

func TestTZNowRendersInjectedOffset(t *testing.T) {
	old := tzOffsetMin
	defer SetTZOffsetMin(old)
	SetTZOffsetMin(480)
	now := tzNow()
	if got := now.Format("-0700"); got != "+0800" {
		t.Fatalf("tzNow 应按注入偏移渲染，实际 %s", got)
	}
	// 墙钟核对：渲染出的时分应等于 UTC+8h（容忍跨分钟重试一次）。
	want := time.Now().UTC().Add(480 * time.Minute).Format("15:04")
	if now.Format("15:04") != want {
		want = time.Now().UTC().Add(480 * time.Minute).Format("15:04")
		if now.Format("15:04") != want {
			t.Fatalf("渲染墙钟应为 UTC+8，实际 %s（期望 %s）", now.Format("15:04"), want)
		}
	}
}

func TestTZNowDefaultsToUTCWhenNotInjected(t *testing.T) {
	old := tzOffsetMin
	defer SetTZOffsetMin(old)
	SetTZOffsetMin(0)
	now := tzNow()
	if got := now.Format("-0700"); got != "+0000" {
		t.Fatalf("未注入时 tzNow 应回 UTC，实际 %s", got)
	}
}