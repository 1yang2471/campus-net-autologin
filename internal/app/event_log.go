package app

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// AppendEvent 往状态日志尾部补一条动作事件（切账号、手动重连等）。
// 与 TransitionLogger 同一份文件、同一行格式，事故复盘时不用再翻内存日志。
//
// now 由调用方注入：桌面 time.Now（系统本地），安卓 tzNow（gomobile 无 TZ，
// 直接 time.Now 会打 UTC）。消息含换行一律拒绝——否则能伪造任意日志行。
func AppendEvent(path string, now func() time.Time, msg string) error {
	if strings.ContainsAny(msg, "\r\n") {
		return fmt.Errorf("事件消息含换行，拒绝写入状态日志")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s INFO %s\n", now().Format("2006-01-02 15:04:05"), msg)
	return err
}

// EventClock 状态事件时钟缝：默认系统本地时间；安卓 mobile.Start 覆盖成
// tzNow（gomobile 无 TZ，time.Now 会打 UTC）。与 TransitionLogger 同源。
var eventClock = time.Now

func SetEventClock(f func() time.Time) {
	if f != nil {
		eventClock = f
	}
}

// EventClock 供组合根取当前事件时钟（AppendEvent 的 now 参数）。
func EventClock() func() time.Time { return eventClock }
