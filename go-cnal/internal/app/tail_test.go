package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 日志尾部读取（面板可视化日志的数据源）：只读文件末尾块，不整读大文件；
// 文件不存在 = 空（新装还没写过日志，不是错误）；行尾 CRLF 归一。

func writeLog(t *testing.T, lines int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cnalw.log")
	var b strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "2026-11-08 09:00:%02d INFO 状态: 在线｜IPv6 在线\r\n", i%60)
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// 行为：TailLines 只返回最后 N 行，顺序与文件一致（新的在最后），无空行。
func TestTailLinesReturnsLastN(t *testing.T) {
	p := writeLog(t, 300)
	got, err := TailLines(p, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("应返回 5 行，实际 %d", len(got))
	}
	for _, l := range got {
		if l == "" || strings.HasSuffix(l, "\r") {
			t.Errorf("行不得为空或带 CR: %q", l)
		}
	}
	if !strings.Contains(got[0], "09:00:") {
		t.Errorf("应为日志行，实际 %q", got[0])
	}
}

// 行为：行数少于 N → 全返回；文件不存在 → 空且不报错（新装无日志）。
func TestTailLinesShortAndMissing(t *testing.T) {
	p := writeLog(t, 2)
	got, err := TailLines(p, 50)
	if err != nil || len(got) != 2 {
		t.Errorf("不足 N 行应全返回，实际 %v %v", got, err)
	}
	got2, err := TailLines(filepath.Join(t.TempDir(), "none.log"), 10)
	if err != nil || len(got2) != 0 {
		t.Errorf("文件不存在应空且无错，实际 %v %v", got2, err)
	}
}

// 行为：大文件只读尾部（构造 1MB+ 日志，确认内存里只有 N 行而不是全文）。
func TestTailLinesLargeFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.log")
	var b strings.Builder
	line := strings.Repeat("x", 200) + "\r\n"
	for b.Len() < 1<<20 {
		b.WriteString(line)
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := TailLines(p, 3)
	if err != nil || len(got) != 3 {
		t.Fatalf("大文件应只回 3 行，实际 %d %v", len(got), err)
	}
}
