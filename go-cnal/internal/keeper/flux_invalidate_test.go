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

// 行为（2026-11-21 用户报告「流量明细与今日流量数值不一致」）：API /flux 的
// 实时快照经 StoreFluxView 挂入交接槽，由 Tick 的 pollFlux 落账——「今日流量」
// （/state.flux 缓存）与「流量明细」（/flux 实时快照）收敛到同一读数，时差不再
// 制造两个数字。落账推进节流不重查；空表不挂；切账号作废时在途旧读数一并丢弃
// （trouble-maker P0-1：跨 goroutine 只走原子槽，单写者契约保持）。
func TestStoreFluxViewSyncsSnapshotToCache(t *testing.T) {
	clk := time.Unix(1000, 0)
	d := NewDaemon("")
	d.Now = func() time.Time { return clk }
	// 缓存里是 10 分钟内的旧读数（面板「今日流量」显示这个）
	d.updateFluxView(map[string]string{"当天流量": "1G", "日流量额": "4G"}, clk)
	calls := 0
	d.FetchFlux = func() (map[string]string, error) {
		calls++
		return nil, nil
	}

	// 用户点开流量明细：实时查到 1.5G（涨了）→ 挂槽；未到下一拍时缓存不变
	d.StoreFluxView(map[string]string{"当天流量": "1.5G", "日流量额": "4G"})
	if got := d.FluxUsage().Used; got != "1G" {
		t.Fatalf("挂槽后须由 Tick 落账（不能跨 goroutine 直写），实际 %q", got)
	}

	// 下一拍 pollFlux 消费槽位：视图跟上明细读数
	d.pollFlux()
	if got := d.FluxUsage().Used; got != "1.5G" {
		t.Fatalf("Tick 消费快照后今日流量应跟上明细，实际 %q", got)
	}
	if calls != 0 {
		t.Fatal("落账应推进节流，本拍不得再打门户")
	}

	// 空表不挂（门户空表时保留现有读数）
	d.StoreFluxView(map[string]string{})
	d.pollFlux()
	if got := d.FluxUsage().Used; got != "1.5G" {
		t.Fatalf("空表不得覆盖缓存，实际 %q", got)
	}
}

// 行为：切账号作废（InvalidateFlux）时，交接槽里在途的旧账号快照一并丢弃——
// 旧读数不得在新账号名下多显一拍（2026-11-20 根因的回流窗封口）。
func TestInvalidateFluxDropsPendingSnapshot(t *testing.T) {
	clk := time.Unix(1000, 0)
	d := NewDaemon("")
	d.Now = func() time.Time { return clk }
	d.FetchFlux = func() (map[string]string, error) {
		return map[string]string{"当天流量": "9G", "日流量额": "9G"}, nil
	}

	// 旧账号快照在途（API 刚查完还没被 Tick 消费）→ 切账号
	d.StoreFluxView(map[string]string{"当天流量": "旧账号读数", "日流量额": "4G"})
	d.InvalidateFlux()
	d.pollFlux() // 作废消费拍：丢弃 pending 并立即重查新账号
	if got := d.FluxUsage().Used; got != "9G" {
		t.Fatalf("在途旧账号快照必须被丢弃并重查，实际 %q", got)
	}
}
