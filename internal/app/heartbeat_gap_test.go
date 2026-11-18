package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 启动空窗自报（2026-11-13 旧进程 21:20:10 停搏 2h 无痕）：停搏只存在于
// 日志里，翻日志才能发现。重启后若最后一拍距现在超阈值，补一行「心跳空窗」，
// 把停搏从「翻日志才知道」变成「启动即告」。间隔只陈述事实，不推断原因
//（停搏可能是人为退出/进程暂停/安卓 Doze 冻结，各有含义）。

func writeGapLog(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cnalw.log")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHeartbeatGapAnnotatedAfterThreshold(t *testing.T) {
	var buf bytes.Buffer
	base := time.Date(2026, 11, 13, 23, 30, 0, 0, time.UTC)
	p := writeGapLog(t, "2026-11-13 23:20:00 INFO 拍: 在线｜x\n"+
		"2026-11-13 23:25:00 INFO 拍: 在线｜x\n")
	AnnotateHeartbeatGap(p, &buf, func() time.Time { return base }, time.Minute)
	out := buf.String()
	for _, want := range []string{"心跳空窗", "上次拍于 2026-11-13 23:25:00", "间隔 5m0s"} {
		if !strings.Contains(out, want) {
			t.Errorf("空窗行缺 %q：%q", want, out)
		}
	}
}

// 行为：阈值内（正常重启）不写行——不给正常重启制造噪音。
func TestHeartbeatGapSilentWithinThreshold(t *testing.T) {
	var buf bytes.Buffer
	base := time.Date(2026, 11, 13, 23, 30, 0, 0, time.UTC)
	p := writeGapLog(t, "2026-11-13 23:29:50 INFO 拍: 在线｜x\n")
	AnnotateHeartbeatGap(p, &buf, func() time.Time { return base }, time.Minute)
	if buf.Len() != 0 {
		t.Errorf("阈值内必须静默，实际 %q", buf.String())
	}
}

// 行为：文件缺失/无拍行 → 静默（不编造空窗）。
func TestHeartbeatGapSilentOnMissingFile(t *testing.T) {
	var buf bytes.Buffer
	base := time.Date(2026, 11, 13, 23, 30, 0, 0, time.UTC)
	AnnotateHeartbeatGap(filepath.Join(t.TempDir(), "none.log"), &buf,
		func() time.Time { return base }, time.Minute)
	if buf.Len() != 0 {
		t.Errorf("缺文件必须静默，实际 %q", buf.String())
	}
	p2 := writeGapLog(t, "2026-11-13 23:29:50 INFO 状态: 在线\n") // 有行无拍
	AnnotateHeartbeatGap(p2, &buf, func() time.Time { return base }, time.Minute)
	if buf.Len() != 0 {
		t.Errorf("无拍行必须静默，实际 %q", buf.String())
	}
}

// 行为：时区陷阱收死——时间戳由注入时钟渲染，解析用同一时钟的 Location 还原
// 绝对时刻。gomobile 的 time.Now 挂 UTC，裸 time.Parse 会把 +8 墙钟错算成
// 8 小时空窗（2026-11-11/11-13 两次 tz 事故同源）。
func TestHeartbeatGapTZConsistent(t *testing.T) {
	var buf bytes.Buffer
	loc := time.FixedZone("CST", 8*3600)
	base := time.Date(2026, 11, 13, 23, 30, 0, 0, loc)
	p := writeGapLog(t, base.Add(-5*time.Minute).Format("2006-01-02 15:04:05")+" INFO 拍: 在线｜x\n")
	AnnotateHeartbeatGap(p, &buf, func() time.Time { return base }, time.Minute)
	if !strings.Contains(buf.String(), "间隔 5m0s") {
		t.Errorf("同区解析应得 5m0s（裸 Parse 会得 7h55m）：%q", buf.String())
	}
}
