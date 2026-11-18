package app

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// cnalw 状态迁移文件日志：与 Python keeper.log 同目录同语义（差分回路的
// 信号端——换役对照就是两份日志并排读）。

// 行为：首次观察必写；同态重复观察不写；迁移才写。
func TestTransitionLoggerWritesOnChange(t *testing.T) {
	var buf bytes.Buffer
	base := time.Unix(0, 0)
	clk := base
	l := NewTransitionLogger(&buf, func() time.Time { return clk })

	l.Observe("online", "")
	lines := strings.Count(buf.String(), "\n")
	if lines != 1 {
		t.Fatalf("首次观察应写 1 行，实际 %d", lines)
	}
	l.Observe("online", "")
	if n := strings.Count(buf.String(), "\n"); n != 1 {
		t.Fatalf("同态重复不得写，实际 %d 行", n)
	}
	clk = clk.Add(time.Minute)
	l.Observe("offline", "登录未受理: NOAUTH")
	out := buf.String()
	if n := strings.Count(out, "\n"); n != 2 {
		t.Fatalf("迁移应写第 2 行，实际 %d 行", n)
	}
	// 语义对齐 keeper.log："时间 INFO 状态: <label>｜<detail>"
	if !strings.Contains(out, "INFO 状态: 离线｜登录未受理: NOAUTH") {
		t.Errorf("行语义必须与 keeper.log 对齐，实际:\n%s", out)
	}
	if !strings.Contains(out, "INFO 状态: 在线｜") {
		t.Errorf("无 detail 也应带分隔符（在线｜），实际:\n%s", out)
	}
}
