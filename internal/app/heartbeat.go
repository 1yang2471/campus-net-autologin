package app

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"cnal/internal/api"
)

// HeartbeatLogger 心跳日志器（复盘 §3.1 观测台架）：每拍必落一行，同态
// 连拍也写——A1「漏拍 0」靠心跳行时间戳差判定，迁移才写的 TransitionLogger
// 测不了漏拍。行前缀「拍:」与「状态:」区分，同一文件两种行并存。
type HeartbeatLogger struct {
	w   io.Writer
	now func() time.Time
}

// NewHeartbeatLogger 注入写出目标与时钟（测试可控）。
func NewHeartbeatLogger(w io.Writer, now func() time.Time) *HeartbeatLogger {
	return &HeartbeatLogger{w: w, now: now}
}

// Beat 记一拍；无条件落行。
func (h *HeartbeatLogger) Beat(state, detail string) {
	fmt.Fprintf(h.w, "%s INFO 拍: %s｜%s\n",
		h.now().Format("2006-01-02 15:04:05"), api.Label(state), detail)
}

// AnnotateHeartbeatGap 启动空窗自报（2026-11-13 旧进程 21:20:10 停搏 2h 无痕
// 教训）：进程重启后，若既有日志的最后一拍距现在超过 threshold，补一行
// 「心跳空窗」——把停搏从「翻日志才知道」变成「启动即告」。间隔只陈述事实
// 不推断原因（停搏可能是人为退出/进程暂停/安卓 Doze 冻结，各有含义）。
//
// 时钟纪律：时间戳由注入时钟渲染，解析用同一时钟的 Location 还原绝对时刻——
// gomobile 的 time.Now 挂 UTC，裸 time.Parse 会把 +8 墙钟错算成 8 小时空窗
//（2026-11-11/11-13 两次 tz 事故同源，这里把缝收死）。
func AnnotateHeartbeatGap(path string, w io.Writer, now func() time.Time, threshold time.Duration) {
	stamp, ok := lastBeatStamp(path)
	if !ok {
		return
	}
	t := now()
	last, err := time.ParseInLocation("2006-01-02 15:04:05", stamp, t.Location())
	if err != nil {
		return
	}
	gap := t.Sub(last)
	if gap <= threshold {
		return
	}
	fmt.Fprintf(w, "%s INFO 心跳空窗: 上次拍于 %s（间隔 %s）\n",
		t.Format("2006-01-02 15:04:05"), stamp, gap)
}

// lastBeatStamp 读日志尾部 8KB，取最后一行「拍:」的秒级时间戳原文。
// 缺文件/空文件/无可解析拍行 → (\"\", false)，调用方静默（不编造空窗）。
func lastBeatStamp(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	const tail = 8192
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return "", false
	}
	if st.Size() > tail {
		if _, err := f.Seek(-tail, io.SeekEnd); err !=  nil {
			return "", false
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return "", false
	}
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if !strings.Contains(line, "拍:") || len(line) < 19 {
			continue
		}
		return line[:19], true
	}
	return "", false
}
