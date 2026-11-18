package keeper

import (
	"testing"
	"time"
)

func TestRollingHourCeiling(t *testing.T) {
	var now = time.Unix(0, 0)
	rl := NewRateLimit(8, time.Hour, 15*time.Minute, func() time.Time { return now })

	allowed := 0
	// 模拟"每 60 秒被踢一次"两小时
	for i := 0; i < 120; i++ {
		now = now.Add(time.Minute)
		if rl.AllowAutoLogin() {
			rl.RecordSuccess(now)
			allowed++
		}
	}
	// 断言：任意滚动 1 小时内成功次数 ≤8
	for i := 0; i < 120; i++ {
		t0 := time.Unix(int64(i)*60, 0)
		n := 0
		for _, s := range rl.Successes() {
			if !s.Before(t0) && s.Before(t0.Add(time.Hour)) {
				n++
			}
		}
		if n > 8 {
			t.Fatalf("滚动 1 小时内自动登录 %d 次 > 上限 8（实测冻结线 12 次/小时）", n)
		}
	}
	if allowed > 16 {
		t.Errorf("两小时总放行 %d 次，远超 2×上限——窗口或停时又被耦合了", allowed)
	}
}

func TestPauseDoesNotRefundBudget(t *testing.T) {
	var now = time.Unix(0, 0)
	rl := NewRateLimit(8, time.Hour, 15*time.Minute, func() time.Time { return now })
	for i := 0; i < 8; i++ {
		now = now.Add(time.Minute)
		rl.RecordSuccess(now)
	}
	if rl.AllowAutoLogin() {
		t.Errorf("达上限即禁止")
	}
	now = now.Add(time.Minute) // 仍在停时内
	if rl.AllowAutoLogin() {
		t.Errorf("停时未到不得放行")
	}
	// 停时过后：窗口里仍有 8 条记录 → 必须继续禁止（这是"触发即清空"那个历史回归的反面）
	now = now.Add(15 * time.Minute)
	if rl.AllowAutoLogin() {
		t.Errorf("停时结束但计数窗口未过 → 不得放行（触发即清空的历史回归）")
	}
}
