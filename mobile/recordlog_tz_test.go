package mobile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cnal/internal/app"
)

// recordLog 落文件行的时间戳必须跟 tzNow（注入偏移渲染设备本地时间），
// 不能用裸 time.Now()——gomobile 进程无 TZ，time.Now 挂 UTC，真机日志会
// 和手机时钟差 8 小时（2026-11-11 白名单轮实证；2026-11-13 凌晨 A1 轮
// 复发：启动行标 17:57 实为本地 01:57，差 8h 整）。
//
// 宿主机上 host 本地钟与 UTC 不同，可稳定复现：tzNow 默认偏移 0（UTC），
// 事件行时间戳应 ≈ UTC now；裸 time.Now() 在东八区机器上会差 8h。
func TestRecordLogStampFollowsInjectedClock(t *testing.T) {
	resetState(t)
	t.Cleanup(func() { resetState(t) })

	dir := t.TempDir()
	SetDataDir(dir)
	t.Cleanup(func() { app.SetDataDir("") })
	if err := SetCredentials("alice", "pw-a"); err != nil {
		t.Fatal(err)
	}
	if err := SetPhysicalSources("10.20.30.40", ""); err != nil {
		t.Fatal(err)
	}
	ini := filepath.Join(dir, "config.ini")
	body := "[keeper]\nserver = http://portal.example\nlogin_path = /ac_portal/login.php\n" +
		"username = alice\ninterval = 3600\nprobe_timeout = 1\n"
	if err := os.WriteFile(ini, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	SetPanelPage("<html>panel</html>")

	if _, err := Start(ini); err != nil {
		t.Fatalf("Start 应能装配内核: %v", err)
	}
	defer Stop()

	// 事件驱动行由 beatLoop goroutine 异步落盘，轮询等待（≤3s）。
	var b []byte
	var ts string
	var err error
	for i := 0; i < 60; i++ {
		b, err = os.ReadFile(filepath.Join(dir, "cnalw.log"))
		if err == nil {
			for _, ln := range strings.Split(string(b), "\n") {
				if strings.Contains(ln, "INFO 事件驱动:") {
					f := strings.Fields(ln)
					if len(f) >= 2 {
						ts = f[0] + " " + f[1]
					}
				}
			}
		}
		if ts != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if ts == "" {
		t.Fatalf("「事件驱动」行未落文件:\n%s", b)
	}
	got, err := time.Parse("2006-01-02 15:04:05", ts)
	if err != nil {
		t.Fatalf("时间戳不是预期形态 %q: %v", ts, err)
	}
	want := time.Now().UTC() // tzNow 默认偏移 0 → 渲染 UTC
	if d := got.Sub(want); d > 2*time.Minute || d < -2*time.Minute {
		t.Fatalf("recordLog 时间戳应跟 tzNow（默认 UTC 渲染），偏差 %v——落了宿主本地钟", d)
	}
}
