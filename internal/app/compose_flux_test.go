package app

import (
	"net"
	"testing"
	"time"

	"cnal/internal/keeper"
	"cnal/internal/netstack"
)

// 切片5（组合根）：newAPI 的 Snapshot 闭包必须把守护里缓存的流量视图带出来，
// 面板刷 /state 才能顺带拿到、不再单独打 /flux。通过公开的 a.Snapshot() 验证。

func TestNewAPISnapshotCarriesFluxUsage(t *testing.T) {
	cfg := defaultAdvancedConfig()
	cfg.Server = "https://portal.example"
	pl := &plumbing{
		action:    &keeper.LoginAction{},
		rl:        keeper.NewRateLimit(8, time.Hour, 15*time.Minute, time.Now),
		portal:    netstack.New(net.IPv4(127, 0, 0, 1), time.Second), // Snapshot 读 Source()
		currentV6: func() net.IP { return nil },                      // Snapshot 读 V6Src
	}
	d := newDaemon(cfg, pl)
	// 让守护真的拿到一次 flux 读数（走公开 Tick，不碰内部字段）。
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return true, nil }
	d.ProbeV6 = func() string { return "no_address" } // 组合根装的缝会碰 nil plumbing
	d.ProbeL2 = func() string { return "ok" }
	d.FetchFlux = func() (map[string]string, error) {
		return map[string]string{"当天流量": "2G", "日流量额": "4G"}, nil
	}
	d.Tick()

	a := newAPI(&cfg, pl, d)
	snap := a.Snapshot()
	if snap.Flux == nil {
		t.Fatal("Snapshot.Flux 未接线：面板拿不到流量数据")
	}
	if !snap.Flux.HasPercent || snap.Flux.Percent != 50 {
		t.Errorf("流量应为 50%%，实际 %+v", snap.Flux)
	}
	if snap.Flux.Used != "2G" {
		t.Errorf("原文应照列，实际 %q", snap.Flux.Used)
	}
}

// 行为：未拿到读数时 Flux 为 nil（omitempty，旧客户端 JSON 形状不变）。
func TestNewAPISnapshotOmitsFluxBeforeFirstReading(t *testing.T) {
	cfg := defaultAdvancedConfig()
	cfg.Server = "https://portal.example"
	pl := &plumbing{
		action:    &keeper.LoginAction{},
		rl:        keeper.NewRateLimit(8, time.Hour, 15*time.Minute, time.Now),
		portal:    netstack.New(net.IPv4(127, 0, 0, 1), time.Second),
		currentV6: func() net.IP { return nil },
	}
	d := newDaemon(cfg, pl)
	a := newAPI(&cfg, pl, d)
	if a.Snapshot().Flux != nil {
		t.Error("首次读数前 Flux 应省略")
	}
}
