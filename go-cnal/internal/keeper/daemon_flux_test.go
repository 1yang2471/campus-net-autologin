package keeper

import (
	"errors"
	"testing"
	"time"
)

// G3：flux 真值判死（移植 Python t37「路由通、会话死」假在线封堵语义）。
// 锚点绿 ≠ 在线：flux 空表连续 2 拍（防抖）才判死；判死后锚点绿也不得
// 显示在线；flux 复活即清判死。

func newFluxTestDaemon(fluxAlive bool) *Daemon {
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) {
		if fluxAlive {
			return map[string]string{"用户名称": "u"}, nil
		}
		return map[string]string{}, nil
	}
	return d
}

// 行为：锚点绿 + flux 活 → 在线，会话标记清零。
func TestFluxAliveShowsOnline(t *testing.T) {
	d := newFluxTestDaemon(true)
	d.Tick()
	if d.State != StateOnline {
		t.Errorf("flux 活应在线，实际 %v/%v", d.State, d.Detail)
	}
	if d.SessionDead {
		t.Error("flux 活不得判死")
	}
}

// 行为：锚点绿 + flux 空 ×1 → 防抖不判死，仍显示在线（单拍空表是常态抖动）。
func TestFluxDeadOnceDebounces(t *testing.T) {
	d := newFluxTestDaemon(false)
	d.Tick()
	if d.State != StateOnline || d.SessionDead {
		t.Errorf("单拍空表不得判死，实际 %v/%v dead=%v", d.State, d.Detail, d.SessionDead)
	}
}

// 行为：锚点绿 + flux 连续空 ×2 → 判死：不得再显示在线（假在线封堵），
// 落到离线路径（重连在 G4 接入）。
func TestFluxDeadTwiceMarksDeadAndFallsThrough(t *testing.T) {
	d := newFluxTestDaemon(false)
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.Tick()                        // miss=1
	clk = clk.Add(61 * time.Second) // 越过 60s 节流（sessionProbeInterval）
	d.Tick()                        // miss=2 → 判死
	if !d.SessionDead {
		t.Fatal("连续两拍空表必须判死")
	}
	if d.State == StateOnline {
		t.Errorf("判死后不得显示在线（假在线），实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：会话探针 60s 节流（Python SESSION_PROBE_S=60 同源）——tick 间隔可低至
// 1s（用户可配），不节流就是每秒一次 userflux。2026-11-10 实测：1s tick 下
// 5 拍 5 次请求、2s 即判死，是 60 倍门户压力（规格 §12「与 60s 的 L2/L3 同拍」）。
func TestFluxProbeThrottledTo60s(t *testing.T) {
	calls := 0
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { calls++; return map[string]string{}, nil }

	for i := 0; i < 5; i++ {
		d.Tick()
		clk = clk.Add(time.Second)
	}
	if calls != 1 {
		t.Fatalf("60s 节流：5 拍（1s 间隔）只允许 1 次 userflux，实际 %d 次", calls)
	}
	clk = clk.Add(59 * time.Second) // 距首拍 64s > 60s
	d.Tick()
	if calls != 2 {
		t.Errorf("60s 后应放行下一拍探针，实际 %d 次", calls)
	}
}

// 行为：判死后 flux 复活 → 判死清零，回到在线。
func TestFluxRevivalClearsDead(t *testing.T) {
	d := newFluxTestDaemon(false)
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.Tick()                        // miss=1
	clk = clk.Add(61 * time.Second) // 越过节流 → miss=2 判死
	d.Tick()
	if !d.SessionDead {
		t.Fatal("前置：应已判死")
	}
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"用户名称": "u"}, nil }
	clk = clk.Add(61 * time.Second) // 越过节流 → 复活拍
	d.Tick()
	if d.SessionDead {
		t.Error("flux 复活必须清判死")
	}
	if d.State != StateOnline {
		t.Errorf("复活后应在线，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：flux 查询出错（门户不可达等）= 未知不计数——不得推进判死进度
// （Python：门户不可达属离线分支职权，不归判死管）。
func TestFluxErrorNotCountedAsMiss(t *testing.T) {
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) {
		return nil, errors.New("门户不可达")
	}
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.Tick()                        // miss 不计（err）
	clk = clk.Add(61 * time.Second) // 越过节流再探一拍：错误仍不得推进判死
	d.Tick()
	if d.SessionDead {
		t.Error("查询出错=未知，不得判死")
	}
	if d.State != StateOnline {
		t.Errorf("未知拍应保持在线显示，实际 %v/%v", d.State, d.Detail)
	}
}
