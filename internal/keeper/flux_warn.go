package keeper

// 流量阈值提醒（只读、零副作用）：守护每 10 分钟查一次 flux，这里只算
// 「本轮该不该提醒一次」，不发通知、不改自动连、不断网。通知的呈现由各端
// 壳层决定（Windows 托盘 / 安卓前台服务），内核只给一个事件名。
//
// 一次性语义：每个阈值档在**一次守护进程生命周期内**只发一次；回落后重新
// 越阈也不重复发，避免 10 分钟一次的轮询把用户刷屏。

// fluxWarnLevel 按百分比与两个阈值给出档位。纯函数，便于阈值可配的回归。
func fluxWarnLevel(pct, warnAt, criticalAt float64) string {
	switch {
	case pct >= criticalAt:
		return "critical"
	case pct >= warnAt:
		return "warn"
	default:
		return "ok"
	}
}

// fluxWarner 记住已提醒过的最高档（单调不回退，所以回落重涨不重复发）。
type fluxWarner struct {
	warnAt     float64
	criticalAt float64
	fired      map[string]bool
}

func newFluxWarner(warnAt, criticalAt float64) *fluxWarner {
	return &fluxWarner{warnAt: warnAt, criticalAt: criticalAt, fired: map[string]bool{}}
}

// observe 喂一次读数，返回要发的事件名（"" = 不发）。
// hasPercent=false 表示算不出百分比，直接跳过——绝不为「未知」乱发通知。
func (w *fluxWarner) observe(hasPercent bool, pct float64) string {
	if !hasPercent {
		return ""
	}
	level := fluxWarnLevel(pct, w.warnAt, w.criticalAt)
	if level == "ok" || w.fired[level] {
		return ""
	}
	w.fired[level] = true
	return level
}
