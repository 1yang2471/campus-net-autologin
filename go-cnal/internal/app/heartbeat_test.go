package app

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// 心跳日志（复盘 §3.1 观测台架，A1/A3 测量前提）：每拍必落一行，与
// TransitionLogger 的「迁移才写」互补。行前缀「拍:」与「状态:」区分，
// 同一文件两种行并存；分析器按心跳行时间戳差算漏拍。

// 行为：每拍必写（同态连拍也写）；时间戳逐拍更新；detail 进行。
func TestHeartbeatLoggerWritesEveryBeat(t *testing.T) {
	var buf bytes.Buffer
	clk := time.Unix(0, 0)
	h := NewHeartbeatLogger(&buf, func() time.Time { return clk })

	h.Beat("online", "")
	h.Beat("online", "") // 同态连拍也必须写——这正是它区别于 TransitionLogger 的点
	if n := strings.Count(buf.String(), "\n"); n != 2 {
		t.Fatalf("每拍必落行，两拍应 2 行，实际 %d", n)
	}

	clk = clk.Add(3 * time.Second)
	h.Beat("offline", "登录未受理: NOAUTH")
	out := buf.String()
	if !strings.Contains(out, "INFO 拍: 在线｜") {
		t.Errorf("行语义 = 时间 INFO 拍: <label>｜<detail>，实际:\n%s", out)
	}
	if !strings.Contains(out, "INFO 拍: 离线｜登录未受理: NOAUTH") {
		t.Errorf("detail 必须进心跳行，实际:\n%s", out)
	}
	if !strings.Contains(out, " 08:00:03 ") {
		t.Errorf("时间戳必须逐拍更新，实际:\n%s", out)
	}
}
