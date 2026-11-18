package app

import (
	"os"
	"testing"
	"time"
)

func readFileForTest(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeFileForTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 切账号动作日志：p.logf 在桌面是 stdout、在安卓是内存数组（LogsJSON），
// 两者都不进 cnalw.log——2026-11-12 真机踢线验证时抓到「切账号成功但
// 状态日志无行」。AppendEvent 直接往状态日志尾部补一行，事故复盘时
// 与状态迁移在同一份文件里可对齐。

func TestAppendEventWritesToStateLog(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/cnalw.log"
	clock := func() time.Time { return time.Date(2026, 11, 12, 9, 55, 47, 0, time.UTC) }
	if err := AppendEvent(path, clock, "切换账号 alice: 门户确认 v4 已归 alice"); err != nil {
		t.Fatal(err)
	}
	b := readFileForTest(t, path)
	want := "2026-11-12 09:55:47 INFO 切换账号 alice: 门户确认 v4 已归 alice\n"
	if string(b) != want {
		t.Fatalf("状态日志行不符：%q", string(b))
	}
}

func TestAppendEventKeepsExistingLines(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/cnalw.log"
	writeFileForTest(t, path, "旧行\n")
	clock := func() time.Time { return time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC) }
	if err := AppendEvent(path, clock, "新事件"); err != nil {
		t.Fatal(err)
	}
	got := string(readFileForTest(t, path))
	if got != "旧行\n2026-11-12 10:00:00 INFO 新事件\n" {
		t.Fatalf("应在尾部追加、不得覆盖：%q", got)
	}
}

func TestAppendEventRejectsNewlineInMessage(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/cnalw.log"
	clock := func() time.Time { return time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC) }
	if err := AppendEvent(path, clock, "第一行\n伪造的第二行"); err == nil {
		t.Fatal("消息含换行必须拒绝（防日志注入伪造行）")
	}
	b, err := os.ReadFile(path)
	if err == nil && len(b) != 0 {
		t.Fatalf("拒绝后不得写文件，实际 %q", string(b))
	}
}
