package keeper

import "time"

// 流量轮询（只读能力）的装配与实现。从 daemon.go 拆出：守护主文件已过
// 20KB 提交闸，按功能分文件（tidy §3）。语义与会话探针共用一次 flux 读数，
// 保证一拍最多只打一次 userflux（既有红线：60s 内 1 次）。

const (
	// 查一次 userflux 会真打门户，频繁查会撞限频、刷日志。10 分钟够看出
	// 趋势，又不打扰门户。
	defaultFluxPollInterval = 10 * time.Minute
	defaultFluxWarnAt       = 80.0
	defaultFluxCriticalAt   = 90.0
)

// SetFluxPollInterval 覆盖流量轮询间隔（ini flux_poll_interval_sec；<=0 保持默认）。
// 只在 Tick 启动前调用。
func (d *Daemon) SetFluxPollInterval(v time.Duration) {
	if v > 0 {
		d.fluxPollInterval = v
	}
}

// SetFluxWarnThresholds 覆盖提醒阈值（ini flux_warn_at，逗号分隔；非法值忽略）。
// 只在 Tick 启动前调用。
func (d *Daemon) SetFluxWarnThresholds(warnAt, criticalAt float64) {
	if warnAt > 0 && criticalAt > warnAt && criticalAt <= 100 {
		d.fluxWarner = newFluxWarner(warnAt, criticalAt)
	}
}

// FluxUsage 当前用量视图（回路 API / 托盘只读；与 Tick 写用同一把锁）。
func (d *Daemon) FluxUsage() FluxUsageView {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.fluxView
}

// updateFluxView 把一次 flux 读数转成只读视图并判越阈事件。会话探针与流量
// 轮询共用这一处，**保证一拍最多只打一次 userflux**（既有红线：60s 内 1 次）。
func (d *Daemon) updateFluxView(data map[string]string, now time.Time) {
	d.fluxQueriedAt = now // 成功查过就记账：空表也算，避免同拍 pollFlux 再打一次
	d.fluxAt = now        // 复用读数同样推进 10 分钟节流（否则下一拍又会查）
	if len(data) == 0 {
		return // 空表：保留上次读数，不覆盖、不报错
	}
	view := FluxUsage(data)
	view.Level = fluxWarnLevel(view.Percent, d.fluxWarner.warnAt, d.fluxWarner.criticalAt)
	if !view.HasPercent {
		view.Level = "ok"
	}
	d.mu.Lock()
	d.fluxView = view
	d.mu.Unlock()
	if d.OnFluxWarn != nil {
		if ev := d.fluxWarner.observe(view.HasPercent, view.Percent); ev != "" {
			d.OnFluxWarn(ev, view)
		}
	}
}

// InvalidateFlux 作废流量视图缓存（切账号后调用：旧账号的读数不得显示在
// 新账号名下——2026-11-20 用户报告「今日流量与流量明细不一致」的根因）。
// 跨 goroutine 安全：只置原子标志，清空动作在 Tick 的 pollFlux 里做。
func (d *Daemon) InvalidateFlux() { d.fluxReset.Store(true) }

// pollFlux 低频查流量供面板常驻显示。与会话探针共用读数：本轮若已有 flux
// 数据（probeSession 在 flux 回落路径下刚查过），直接复用，不再触网。
func (d *Daemon) pollFlux() {
	if d.FetchFlux == nil {
		return
	}
	now := d.Now()
	// 切账号等外部请求作废缓存：清节流与视图，本拍立即重查新账号读数。
	if d.fluxReset.Swap(false) {
		d.fluxAt = time.Time{}
		d.fluxQueriedAt = time.Time{}
		d.mu.Lock()
		d.fluxView = FluxUsageView{}
		d.mu.Unlock()
	}
	if d.fluxQueriedAt.Equal(now) {
		return // 本轮已由会话探针查过
	}
	if !d.fluxAt.IsZero() && now.Sub(d.fluxAt) < d.fluxPollInterval {
		return
	}
	d.fluxAt = now
	data, err := d.FetchFlux()
	if err != nil {
		return
	}
	d.updateFluxView(data, now)
}
