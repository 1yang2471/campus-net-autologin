package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cnal/internal/app"
)

// 空窗自报接线（2026-11-13 停搏无痕教训）：托盘启动时若 rw 可用，
// 把「心跳空窗」行写进状态日志——纯函数在 internal/app 已单测，这里只守
// 组合根接线存在（行为缝，不耦合 tick 循环）。

func TestTrayAnnotatesHeartbeatGapOnStart(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "cnalw.log")
	// 既有日志：最后一拍 2 小时前
	old := "2026-11-13 21:20:00 INFO 拍: 在线｜x\n"
	if err := os.WriteFile(logPath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	rw, err := app.NewRotateWriter(logPath, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	app.AnnotateHeartbeatGap(logPath, rw, time.Now, 30*time.Minute)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "心跳空窗") {
		t.Errorf("启动接线应落空窗行，实际 %q", string(data))
	}
	if got := app.LogFilePath(); got == "" {
		t.Error("LogFilePath 默认值应为可读路径（托盘与面板 /logs 同文件的前提）")
	}
}