package keeper

import (
	"errors"
	"testing"
	"time"
)

// 切片4：守护内低频 flux 轮询。行为通过公开的 Tick + FluxUsage() 验证，
// 不碰内部字段——测试只关心「多久查一次、失败会不会抹掉旧读数」。

func fluxTestDaemon(t *testing.T) (*Daemon, *time.Time) {
	t.Helper()
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return true, nil }
	return d, &clk
}

// 行为：接了 FetchFlux 后，第一次 Tick 就查一次，把用量挂到只读视图。
func TestFluxPollFirstTickQueries(t *testing.T) {
	d, _ := fluxTestDaemon(t)
	calls := 0
	d.FetchFlux = func() (map[string]string, error) {
		calls++
		return map[string]string{"当天流量": "2G", "日流量额": "4G"}, nil
	}
	d.Tick()
	if calls != 1 {
		t.Fatalf("首拍应查一次 flux，实际 %d", calls)
	}
	u := d.FluxUsage()
	if !u.HasPercent || u.Percent != 50 {
		t.Errorf("用量应 50%%，实际 %.1f has=%v", u.Percent, u.HasPercent)
	}
}

// 行为：10 分钟节流——窗口内多拍不再查；跨窗口才再查一次。
func TestFluxPollThrottledToTenMinutes(t *testing.T) {
	d, clk := fluxTestDaemon(t)
	calls := 0
	d.FetchFlux = func() (map[string]string, error) {
		calls++
		return map[string]string{"当天流量": "1G", "日流量额": "4G"}, nil
	}
	d.Tick()
	for i := 0; i < 5; i++ {
		*clk = clk.Add(30 * time.Second)
		d.Tick()
	}
	if calls != 1 {
		t.Fatalf("10 分钟窗口内不该重复查，实际 %d 次", calls)
	}
	*clk = clk.Add(10 * time.Minute)
	d.Tick()
	if calls != 2 {
		t.Errorf("跨过 10 分钟应再查一次，实际 %d 次", calls)
	}
}

// 行为：查询失败不得覆盖上次读数（旧数据比假的空数据有用）。
func TestFluxPollFailureKeepsLastReading(t *testing.T) {
	d, clk := fluxTestDaemon(t)
	first := true
	d.FetchFlux = func() (map[string]string, error) {
		if first {
			first = false
			return map[string]string{"当天流量": "3G", "日流量额": "4G"}, nil
		}
		return nil, errors.New("门户不可达")
	}
	d.Tick()
	before := d.FluxUsage()
	if !before.HasPercent || before.Percent != 75 {
		t.Fatalf("首拍应 75%%，实际 %.1f", before.Percent)
	}
	*clk = clk.Add(10 * time.Minute)
	d.Tick()
	after := d.FluxUsage()
	if after.Percent != before.Percent || after.Used != before.Used {
		t.Errorf("查询失败不得抹掉上次读数：before=%+v after=%+v", before, after)
	}
}

// 行为：没接 FetchFlux 时零查询、视图为空（异校/旧接线不因为新增缝而失效）。
func TestFluxPollDisabledWhenUnwired(t *testing.T) {
	d, _ := fluxTestDaemon(t)
	d.Tick()
	u := d.FluxUsage()
	if u.HasPercent || u.Used != "" {
		t.Errorf("未接线时视图应为空，实际 %+v", u)
	}
}

// 行为：一拍最多只打一次 userflux——会话探针与流量轮询共用读数。
// 2026-11-11 回归踩到：pollFlux 独立查询会让 5 拍 1s 的旧节流测试变成 2 次
// 请求，既有红线「60s 内 1 次」被破坏。这条钉住复用语义。
func TestFluxPollNeverAddsARequestOnSameTick(t *testing.T) {
	d, clk := fluxTestDaemon(t)
	d.SessionAlive = nil // 强制走 flux 回落路径（与 pollFlux 抢同一缝）
	calls := 0
	d.FetchFlux = func() (map[string]string, error) {
		calls++
		return map[string]string{"当天流量": "1G", "日流量额": "4G"}, nil
	}
	for i := 0; i < 5; i++ {
		d.Tick()
		*clk = clk.Add(time.Second)
	}
	if calls != 1 {
		t.Errorf("5 拍 1s 间隔只应 1 次 userflux，实际 %d", calls)
	}
}

// 行为：轮询间隔可配（ini flux_poll_interval，异校限频口径不同）。
func TestFluxPollIntervalConfigurable(t *testing.T) {
	d, clk := fluxTestDaemon(t)
	d.SetFluxPollInterval(2 * time.Minute)
	calls := 0
	d.FetchFlux = func() (map[string]string, error) {
		calls++
		return map[string]string{"当天流量": "1G", "日流量额": "4G"}, nil
	}
	d.Tick()
	*clk = clk.Add(90 * time.Second)
	d.Tick()
	if calls != 1 {
		t.Fatalf("2 分钟窗口内不该重复查，实际 %d", calls)
	}
	*clk = clk.Add(40 * time.Second)
	d.Tick()
	if calls != 2 {
		t.Errorf("跨过 2 分钟应再查，实际 %d", calls)
	}
}
