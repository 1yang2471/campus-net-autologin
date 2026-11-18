package keeper

import (
	"errors"
	"testing"
	"time"
)

// 退避可配（Python reject_backoff/transient_backoff）：异校防风暴阈值不同，
// 30/600 不再是常量。本文件同时守「瞬态连续 5 次升级长退避」与「冻结闸
// 下限取 reject_backoff」。

// backoffHarness 起一个稳定处于「会话判死→可登录」状态的守护。
func backoffHarness(loginErr string) (d *Daemon, attempts *int, advance func(time.Duration)) {
	d = NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{}, nil }
	n := 0
	attempts = &n
	d.Login = func() (string, error) {
		n++
		return "", errors.New(loginErr)
	}
	advance = func(dur time.Duration) { clk = clk.Add(dur) }
	return d, attempts, advance
}

// 行为：瞬态错误用 transient_backoff，硬拒绝用 reject_backoff（都可配）。
func TestBackoffConfigurableHardVsTransient(t *testing.T) {
	cases := []struct {
		name  string
		login string
		want  time.Duration
	}{
		{"瞬态错误短退避", "请求异常: timeout", 45 * time.Second},
		{"硬拒绝长退避", "密码错误", 120 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, attempts, advance := backoffHarness(tc.login)
			d.SetBackoff(120*time.Second, 45*time.Second)
			d.Tick() // flux miss=1（防抖）
			advance(61 * time.Second) // 越过 60s 会话探针节流
			d.Tick() // miss=2 → 判死 → 登录失败
			if *attempts != 1 {
				t.Fatalf("前置：应已尝试登录 1 次，实际 %d", *attempts)
			}
			wait := d.rejectUntil.Sub(d.Now())
			if wait != tc.want {
				t.Fatalf("退避应为 %v，实际 %v（state=%s）", tc.want, wait, d.State)
			}
			advance(wait - time.Second)
			d.Tick()
			if *attempts != 1 {
				t.Errorf("退避期内不得重试，实际 %d 次", *attempts)
			}
			advance(2 * time.Second)
			d.Tick()
			if *attempts != 2 {
				t.Errorf("退避结束应放行重试，实际 %d 次", *attempts)
			}
		})
	}
}

// 行为：瞬态错误连续 5 次 → 升级为 reject_backoff（Python handle_rejection）。
func TestTransientStreakEscalatesToRejectBackoff(t *testing.T) {
	d, attempts, advance := backoffHarness("noauth not taken")
	d.SetBackoff(120*time.Second, 30*time.Second)
	// 锚点红 = 真断网：连续失败不会被「锚点绿+不受理」的免会话态（t46）
	// 拦成 free_window——连击升级只有真断网场景才需要。
	d.ProbeOnline = func() bool { return false }
	d.Tick() // 去抖拍1
	d.Tick() // 去抖拍2
	d.Tick() // 第 1 次失败 → streak=3 ≥ 去抖阈值，短退避
	waits := []time.Duration{d.rejectUntil.Sub(d.Now())}
	for i := 2; i <= 5; i++ {
		advance(waits[len(waits)-1] + time.Second)
		d.Tick() // 第 i 次失败
		waits = append(waits, d.rejectUntil.Sub(d.Now()))
	}
	if *attempts != 5 {
		t.Fatalf("前置：应已失败 5 次，实际 %d", *attempts)
	}
	for i := 0; i < 4; i++ {
		if waits[i] != 30*time.Second {
			t.Errorf("第 %d 次瞬态退避应 30s，实际 %v", i+1, waits[i])
		}
	}
	if waits[4] != 120*time.Second {
		t.Errorf("第 5 次瞬态应升级 120s，实际 %v", waits[4])
	}
	if d.transientStreak != 5 {
		t.Errorf("瞬态连击应累计 5，实际 %d", d.transientStreak)
	}
}

// 行为：硬拒绝打断瞬态连击（Python transient_streak=0），随后瞬态重新从短退避算。
func TestHardRejectResetsTransientStreak(t *testing.T) {
	d, _, advance := backoffHarness("noauth")
	d.SetBackoff(120*time.Second, 30*time.Second)
	d.Tick()
	advance(61 * time.Second) // 越过 60s 会话探针节流
	d.Tick()
	if d.transientStreak != 1 {
		t.Fatalf("前置：瞬态连击应 1，实际 %d", d.transientStreak)
	}
	// 换成硬拒绝再拍一拍
	d.Login = func() (string, error) { return "", errors.New("密码错误") }
	advance(d.rejectUntil.Sub(d.Now()) + time.Second)
	d.Tick()
	if d.transientStreak != 0 {
		t.Errorf("硬拒绝应清零瞬态连击，实际 %d", d.transientStreak)
	}
	if wait := d.rejectUntil.Sub(d.Now()); wait != 120*time.Second {
		t.Errorf("硬拒绝退避应 120s，实际 %v", wait)
	}
}

// 行为：冻结闸下限取配置的 reject_backoff；文案里的剩余秒数更大时以它为准。
func TestFreezeGateUsesConfiguredRejectBackoff(t *testing.T) {
	d, _, advance := backoffHarness("账号被锁定，剩余 300s 后重试")
	d.SetResponseMarks(ResponseMarks{Freeze: SplitMarks("锁定")})
	d.SetBackoff(120*time.Second, 30*time.Second)
	d.Tick()
	advance(61 * time.Second) // 越过 60s 会话探针节流
	d.Tick()
	if d.State != StateRejected {
		t.Fatalf("冻结应显示 rejected，实际 %v/%v", d.State, d.Detail)
	}
	if wait := d.frozenUntil.Sub(d.Now()); wait != 315*time.Second {
		t.Errorf("冻结时长应 max(120, 300+15)=315s，实际 %v", wait)
	}
	if wait := d.rejectUntil.Sub(d.Now()); wait != 315*time.Second {
		t.Errorf("退避闸应同步到冻结时长，实际 %v", wait)
	}
}

// 行为：冻结文案剩余秒数小于配置下限时用下限兜底（不再硬编码 600s）。
func TestFreezeGateFloorFallsBackToRejectBackoff(t *testing.T) {
	d, _, advance := backoffHarness("freeze: 剩余 60s")
	d.SetBackoff(120*time.Second, 30*time.Second)
	d.Tick()
	advance(61 * time.Second) // 越过 60s 会话探针节流
	d.Tick()
	if wait := d.frozenUntil.Sub(d.Now()); wait != 120*time.Second {
		t.Errorf("冻结下限应 120s，实际 %v", wait)
	}
}
