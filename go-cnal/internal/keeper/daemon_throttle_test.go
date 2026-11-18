package keeper

import (
	"errors"
	"testing"
	"time"
)

// G7：登录风暴三闸（移植 Python tick 登录分支：LOGIN_MIN_INTERVAL 全局节流、
// 失败退避、suspect 熔断）。冒烟实证（2026-11-07 12:48）：无节流时 1s tick
// 下 5.5 分钟不可见地打了 8 次真实登录直到速率闸门关闭——三闸是内核语义，
// 不是接线细节。

// 行为：登录失败后 30s 内的重连拍不得再登录（LOGIN_MIN_INTERVAL）。60s 会话
// 探针节流落地后，判死只能来自 60s 一拍，成功后的 30s 内不存在自动重连拍——
// 30s 闸的可达形态是失败后的重试拍：瞬态退避缩到 5s（< 30s），退避放行时
// 30s 闸还没过，两道闸被隔离验证。
func TestLoginThrottleBlocksRapidRetry(t *testing.T) {
	calls := 0
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.SetBackoff(600, 5) // 瞬态退避 5s < LOGIN_MIN_INTERVAL(30s)：隔离出 30s 闸
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{}, nil }
	d.Login = func() (string, error) {
		calls++
		return "", errors.New("Task 'NOAUTH:*' not taken")
	}

	d.Tick()                        // miss=1
	clk = clk.Add(61 * time.Second) // 越过 60s 会话探针节流 → miss=2 判死
	d.Tick()                        // 登录#1 失败 → 退避 5s（rejectUntil=66s）
	if calls != 1 {
		t.Fatalf("前置：应已登录 1 次，实际 %d", calls)
	}
	clk = clk.Add(6 * time.Second) // 67s：退避已过，30s 闸未过
	d.Tick()
	if calls != 1 {
		t.Fatal("LOGIN_MIN_INTERVAL 闸未生效：退避过后 30s 内不得重连")
	}
	if d.State != StateRejected {
		t.Errorf("节流拍应显示 rejected，实际 %v/%v", d.State, d.Detail)
	}
	clk = clk.Add(31 * time.Second) // 98s：30s 闸也过了 → 放行
	d.Tick()
	if calls != 2 {
		t.Errorf("LOGIN_MIN_INTERVAL 过后应放行重试，实际 %d 次", calls)
	}
}

// 行为：登录失败 → 30s 退避（transient）；退避过后放行重试。
func TestLoginFailBacksOff(t *testing.T) {
	calls := 0
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{}, nil }
	d.Login = func() (string, error) { calls++; return "", errors.New("Task 'NOAUTH:*' not taken") }

	d.Tick()                        // miss=1
	clk = clk.Add(61 * time.Second) // 越过节流 → miss=2 判死
	d.Tick()                        // 登录#1 失败 → 退避
	if calls != 1 || d.State != StateRejected {
		t.Fatalf("前置：登录#1 应失败并显示登录被拒，实际 calls=%d %v/%v", calls, d.State, d.Detail)
	}
	clk = clk.Add(time.Second)
	d.Tick()
	if calls != 1 || d.State != StateRejected {
		t.Errorf("退避拍不得登录，实际 calls=%d %v/%v", calls, d.State, d.Detail)
	}
	clk = clk.Add(30 * time.Second)
	d.Tick()
	if calls != 2 {
		t.Errorf("退避结束应放行重试，实际 %d 次", calls)
	}
}

// 行为：连续 3 次「登录成功但 flux 仍死」= 判定链在骗我们 → 熔断退避 600s。
func TestSuspectCircuitTripsAfterThreeUnfixedSuccesses(t *testing.T) {
	calls := 0
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{}, nil }
	d.Login = func() (string, error) { calls++; return "", nil }

	d.Tick()                        // miss=1
	clk = clk.Add(61 * time.Second) // 越过节流 → miss=2 判死 → 登录#1 suspect=1
	d.Tick()
	clk = clk.Add(61 * time.Second) // 下一拍探针 → miss=3 判死 → 登录#2 suspect=2
	d.Tick()
	clk = clk.Add(61 * time.Second) // 下一拍探针 → miss=4 判死 → 登录#3 suspect=3 → 熔断
	d.Tick()
	if calls != 3 {
		t.Fatalf("前置：应已登录 3 次，实际 %d", calls)
	}
	if d.State != StateRejected {
		t.Errorf("第 3 次未修好应熔断显示 rejected，实际 %v/%v", d.State, d.Detail)
	}
	clk = clk.Add(31 * time.Second)
	d.Tick()
	if calls != 3 {
		t.Errorf("熔断退避期内不得登录，最后 %d 次", calls)
	}
}

// 行为：flux 复活（判定链证明健康）→ 清 suspect 与退避，后续风暴从零起算。
func TestFluxAliveResetsSuspectAndBackoff(t *testing.T) {
	calls := 0
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	alive := false
	d.FetchFlux = func() (map[string]string, error) {
		if alive {
			return map[string]string{"用户名称": "u"}, nil
		}
		return map[string]string{}, nil
	}
	d.Login = func() (string, error) { calls++; return "", nil }

	d.Tick()                        // miss=1
	clk = clk.Add(61 * time.Second) // 越过节流 → miss=2 判死 → 登录#1 suspect=1
	d.Tick()
	clk = clk.Add(61 * time.Second) // 下一拍探针 → miss=3 判死 → 登录#2 suspect=2
	d.Tick()
	alive = true
	clk = clk.Add(61 * time.Second) // 下一拍探针 → flux 活 → online，suspect 清零
	d.Tick()
	if d.State != StateOnline || d.suspect != 0 {
		t.Fatalf("复活应清 suspect，实际 %v/%v suspect=%d", d.State, d.Detail, d.suspect)
	}
	alive = false
	clk = clk.Add(61 * time.Second) // 死×1 防抖
	d.Tick()
	clk = clk.Add(61 * time.Second) // 死×2 → 登录#3（suspect 从 0 起算）→ online，未熔断
	d.Tick()
	if calls != 3 || d.State == StateRejected {
		t.Errorf("suspect 清零后第 3 次成功不得熔断，实际 calls=%d %v/%v", calls, d.State, d.Detail)
	}
}
