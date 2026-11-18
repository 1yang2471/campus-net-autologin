package app

import (
	"fmt"
	"io"
	"time"

	"cnal/internal/api"
)

// TransitionLogger 状态迁移日志器：迁移才写、首拍必写。行语义与 Python
// keeper.log 逐字对齐（"2006-01-02 15:04:05 INFO 状态: <label>｜<detail>"）
// ——两份实现同一场景并排读，就是换役差分回路。
type TransitionLogger struct {
	w    io.Writer
	now  func() time.Time
	last string
}

// NewTransitionLogger 注入写出目标与时钟（测试可控）。
func NewTransitionLogger(w io.Writer, now func() time.Time) *TransitionLogger {
	return &TransitionLogger{w: w, now: now}
}

// Observe 记一拍；迁移才落一行。
func (l *TransitionLogger) Observe(state, detail string) {
	if state == l.last {
		return
	}
	was := l.last
	l.last = state
	if was == "" {
		was = "starting" // 首拍语义：从启动中进入当前态
	}
	fmt.Fprintf(l.w, "%s INFO 状态: %s｜%s\n",
		l.now().Format("2006-01-02 15:04:05"), api.Label(state), detail)
}
