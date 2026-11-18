package keeper

import "time"

// RateLimit 是自动登录的唯一闸门。三个概念必须分开（v1.0.10 曾把
// 计数窗口与停时合成一个常量，速率上限被抬到 32 次/小时）：
//
//	cap     窗口内允许的成功次数
//	window  滚动计数窗口
//	pause   达上限后的降速时长（不是停摆，用户随时可手动越权）
type RateLimit struct {
	cap     int
	window  time.Duration
	pause   time.Duration
	now     func() time.Time
	ok      []time.Time
	pausedU time.Time
}

func NewRateLimit(cap int, window, pause time.Duration, now func() time.Time) *RateLimit {
	return &RateLimit{cap: cap, window: window, pause: pause, now: now}
}

// AllowAutoLogin 必须在**登录之前**调用：只在登录后记账时，每个停时周期
// 都会多放行一次（Python 侧 t40 实测拆完常量仍 10 次/小时）。
func (r *RateLimit) AllowAutoLogin() bool {
	now := r.now()
	if now.Before(r.pausedU) {
		return false
	}
	r.prune(now)
	return len(r.ok) < r.cap
}

func (r *RateLimit) RecordSuccess(at time.Time) {
	r.ok = append(r.ok, at)
	if len(r.ok) >= r.cap {
		r.pausedU = at.Add(r.pause) // 不清空 r.ok：清空等于退回额度
	}
}

func (r *RateLimit) Successes() []time.Time { return append([]time.Time(nil), r.ok...) }

func (r *RateLimit) prune(now time.Time) {
	keep := r.ok[:0]
	for _, t := range r.ok {
		if now.Sub(t) < r.window {
			keep = append(keep, t)
		}
	}
	r.ok = keep
}
