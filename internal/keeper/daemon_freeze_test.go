package keeper

import (
	"errors"
	"testing"
	"time"
)

// 门户限频冻结闸门（移植 Python handle_rejection 的 _frozen_until 语义）：
// 门户回「冻结剩余 N s」时，退避必须独立且更长——锚点抖动/flux 复活都不得
// 提前放行，否则越撞越深（2026-11-05 日志实证：抖动清零 reject_until → 立刻
// 重登 → 冻结加深）。

func newFreezeDaemon(clk *time.Time, calls *int, msg string) *Daemon {
	d := NewDaemon("")
	d.Now = func() time.Time { return *clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{}, nil }
	d.Login = func() (string, error) { *calls++; return "", errors.New(msg) }
	return d
}

// 行为：门户答「冻结剩余 900s」→ 冻结闸 = N+15 = 915s，期间（含 30s 节流已过）
// 一律不再登录。
func TestFreezeGateBlocksBeyondRejectBackoff(t *testing.T) {
	calls := 0
	clk := time.Unix(0, 0)
	d := newFreezeDaemon(&clk, &calls, "登录被拒：账号被限频冻结，剩余 900s")
	d.Tick()                        // miss=1
	clk = clk.Add(61 * time.Second) // 越过 60s 会话探针节流 → miss=2 判死
	d.Tick()                        // 判死 → 登录失败（带冻结消息）
	if calls != 1 {
		t.Fatalf("前置：应登录 1 次，实际 %d", calls)
	}
	if d.State != StateRejected {
		t.Errorf("冻结应显示 rejected，实际 %v/%v", d.State, d.Detail)
	}
	clk = clk.Add(700 * time.Second) // 超过普通退避 600s，但未到 915s
	d.Tick()
	d.Tick()
	if calls != 1 {
		t.Errorf("冻结闸内（700s < 915s）不得登录，实际 %d 次", calls)
	}
	clk = clk.Add(220 * time.Second) // 累计 920s > 915s
	d.Tick()
	d.Tick()
	if calls != 2 {
		t.Errorf("冻结闸过期后应放行，实际 %d 次", calls)
	}
}

// 行为：冻结消息无秒数 → 退避下限 = reject_backoff（600s）。
func TestFreezeGateDefaultBackoffWithoutSeconds(t *testing.T) {
	calls := 0
	clk := time.Unix(0, 0)
	d := newFreezeDaemon(&clk, &calls, "登录被拒：freeze 中")
	d.Tick()                        // miss=1
	clk = clk.Add(61 * time.Second) // 越过节流 → miss=2 判死
	d.Tick()
	if calls != 1 {
		t.Fatalf("前置：应登录 1 次，实际 %d", calls)
	}
	clk = clk.Add(590 * time.Second)
	d.Tick()
	d.Tick()
	if calls != 1 {
		t.Errorf("600s 冻结闸内不得登录，实际 %d 次", calls)
	}
	clk = clk.Add(20 * time.Second)
	d.Tick()
	d.Tick()
	if calls != 2 {
		t.Errorf("冻结闸过期后应放行，实际 %d 次", calls)
	}
}

// 行为：flux 复活（在线分支会清 reject_until）不得清除冻结闸——冻结是门户
// 侧状态，与本地会话读数无关（Python：独立于退避，probe 恢复不清除）。
func TestFreezeGateSurvivesFluxRevival(t *testing.T) {
	calls := 0
	clk := time.Unix(0, 0)
	alive := false
	d := NewDaemon("")
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) {
		if alive {
			return map[string]string{"用户名称": "u"}, nil
		}
		return map[string]string{}, nil
	}
	d.Login = func() (string, error) {
		calls++
		return "", errors.New("账号被限频冻结，剩余 900s")
	}
	d.Tick()                        // miss=1
	clk = clk.Add(61 * time.Second) // 越过节流 → miss=2 判死
	d.Tick()                        // 判死 → 登录失败 → 冻结 915s
	if calls != 1 {
		t.Fatalf("前置：应登录 1 次，实际 %d", calls)
	}
	alive = true
	clk = clk.Add(61 * time.Second) // 越过节流 → 复活拍
	d.Tick()                        // flux 复活：清 reject_until、清 suspect，但不得清冻结
	if d.State != StateOnline {
		t.Fatalf("flux 复活应显示在线，实际 %v/%v", d.State, d.Detail)
	}
	alive = false
	clk = clk.Add(400 * time.Second)
	d.Tick()                        // miss=1（复死后第一次空表）
	clk = clk.Add(61 * time.Second) // 越过节流 → miss=2 再判死
	d.Tick()                        // 应被冻结闸拦住
	if calls != 1 {
		t.Errorf("冻结闸不得被 flux 复活清除，实际登录 %d 次", calls)
	}
}
