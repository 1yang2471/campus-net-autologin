package keeper

import (
	"testing"
	"time"
)

// 切账号后的流量视图（2026-11-20 用户报告实证）：状态卡「今日流量」读的是
// 内核缓存的 FluxUsageView（10 分钟低频轮询），而「流量明细」是面板手动
// 实时查询的原始字段。切账号后缓存没作废 → 两个数字属于不同账号/时刻，
// 用户看到 1.10G 与 1.85G 并存。
//
// 行为：InvalidateFlux 之后，下一拍 Tick 必须立即重查并刷新视图
// （旧账号读数不得继续显示）。

func TestInvalidateFluxRefetchesOnNextTick(t *testing.T) {
	clk := time.Unix(1000, 0)
	calls := 0
	data := map[string]string{"当天流量": "1G", "日流量额": "4G"}
	d := NewDaemon("")
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return true, nil } // 会话活：不走 flux 回落
	d.FetchFlux = func() (map[string]string, error) {
		calls++
		return data, nil
	}

	d.Tick() // 在线分支：pollFlux 查一次 → 视图 = 1G
	if got := d.FluxUsage().Used; got != "1G" {
		t.Fatalf("首拍应读到旧读数 1G，实际 %q", got)
	}
	if calls != 1 {
		t.Fatalf("首拍应查一次 flux，实际 %d", calls)
	}

	// 切账号：新账号流量不同，作废缓存。
	data = map[string]string{"当天流量": "2G", "日流量额": "4G"}
	d.InvalidateFlux()
	clk = clk.Add(3 * time.Second) // 远小于 10 分钟轮询节流
	d.Tick()

	if calls != 2 {
		t.Fatalf("作废后下一拍必须立即重查（不等 10 分钟节流），实际 %d 次", calls)
	}
	if got := d.FluxUsage().Used; got != "2G" {
		t.Fatalf("视图应刷新为新账号读数 2G，实际 %q", got)
	}
}

// 行为：未作废时仍受 10 分钟节流约束（别把重查变成每拍都打门户）。
func TestFluxPollStillThrottled(t *testing.T) {
	clk := time.Unix(1000, 0)
	calls := 0
	d := NewDaemon("")
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return true, nil }
	d.FetchFlux = func() (map[string]string, error) {
		calls++
		return map[string]string{"当天流量": "1G", "日流量额": "4G"}, nil
	}

	d.Tick()
	clk = clk.Add(3 * time.Second)
	d.Tick()
	clk = clk.Add(3 * time.Second)
	d.Tick()
	if calls != 1 {
		t.Fatalf("10 分钟节流内不得重复查 flux，实际 %d 次", calls)
	}
}
