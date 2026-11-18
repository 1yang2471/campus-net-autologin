package mobile

import (
	"sync"
	"time"
)

// 时区偏移缝：gomobile 进程无 TZ 环境变量，Go time.Now() 的本地时区退化为
// UTC——免费时段 23:30-06:30（北京时间）会在 UTC 下午被误判「免认证时段」，
// 守护不自动登录（2026-11-11 M1 首跑真机实证，手机日志 05:57 即 UTC）。
//
// Kotlin 在启动守护前注入设备当前偏移（Asia/Shanghai = +480），Go 用
// UTC+偏移计算本地分钟；偏移变化（夏令时/跨时区）由 Kotlin 重新注入。

var (
	tzMu        sync.RWMutex
	tzOffsetMin int
)

// SetTZOffsetMin 注入设备时区相对 UTC 的偏移分钟（东八区 +480）。
func SetTZOffsetMin(offset int) {
	tzMu.Lock()
	tzOffsetMin = offset
	tzMu.Unlock()
}

// localMinuteFromTZ 用注入偏移算「当天本地分钟」（0-1439）。
func localMinuteFromTZ() int {
	tzMu.RLock()
	off := tzOffsetMin
	tzMu.RUnlock()
	t := time.Now().UTC().Add(time.Duration(off) * time.Minute)
	return t.Hour()*60 + t.Minute()
}

// tzNow 用注入偏移渲染「设备本地时间」：状态日志的时间戳与免费时段判定
// 同源。gomobile 进程无 TZ，time.Now 直接用会打 UTC——真机 22:58 被打成
// 14:58，复盘时和手机系统时间对不上（2026-11-11 白名单轮现场实证）。
func tzNow() time.Time {
	tzMu.RLock()
	off := tzOffsetMin
	tzMu.RUnlock()
	return time.Now().In(time.FixedZone("CNAL", off*60))
}
