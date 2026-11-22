package keeper

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// SESSION_FALSE 自停闸（移植 Python _probe_session/_already_online 语义）：
// 判死→重登→门户答「已在线」= flux 探针在撒谎（cookie 型部署/端点抖动）。
// 矛盾计 3 次即停用 flux 探针 3600s——假判死不再变成登录风暴。
// 2026-11-07 14:00-14:05 换役实测实证：userflux 端点周期性空表（闸门关闭
// 后零登录 flux 仍自愈），8/h 预算 5 分钟被假判死烧光。

// 行为：判死→登录→「已在线」矛盾 ×3 → flux 探针自停 3600s，此后不再登录。
func TestSessionFalseTripAfterThreeContradictions(t *testing.T) {
	calls := 0
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{}, nil }
	d.Login = func() (string, error) {
		calls++
		return "{'success':'true','msg':'logon success,您已在线，请勿重复登录'}", nil
	}
	// 每次矛盾：空表 miss → 越过 60s 会话探针节流 → 判死 → 登录
	for range 3 {
		d.Tick()
		clk = clk.Add(61 * time.Second)
		d.Tick()
	}
	if calls != 3 {
		t.Fatalf("前置：应 3 次矛盾登录，实际 %d", calls)
	}
	clk = clk.Add(time.Second)
	d.Tick() // 第 3 次矛盾时 suspect 熔断同拍触发（显示 rejected）；自停后探针不再判死 → 下一拍回在线
	if d.State != StateOnline {
		t.Errorf("自停后探针不再判死 → 在线，实际 %v/%v", d.State, d.Detail)
	}
	// 自停期内 flux 继续空表也不得再登录（3600s 内）
	clk = clk.Add(60 * time.Second)
	d.Tick()
	d.Tick()
	clk = clk.Add(61 * time.Second)
	d.Tick()
	d.Tick()
	if calls != 3 {
		t.Errorf("自停期内不得登录，实际 %d 次", calls)
	}
}

// 行为：负向形态先排除——「不在线/未在线」类拒绝不算「已在线」（Python P-3 锚定）。
func TestAlreadyOnlineNegativeForms(t *testing.T) {
	if alreadyOnline("账号不在线，请勿重复登录") {
		t.Error("「不在线」拒绝不得算已在线")
	}
	if alreadyOnline("您还未在线") {
		t.Error("「未在线」不得算已在线")
	}
	if !alreadyOnline("您已在线，请勿重复登录") {
		t.Error("「已在线」应算已在线")
	}
	if !alreadyOnline("您在线，请勿重复登录") {
		t.Error("「在线，请勿重复」应算已在线")
	}
}

// 行为：flux 复活（拿到表）= 门户支持 flux 且会话健康 → 矛盾计数清零。
func TestFluxAliveResetsSessFalse(t *testing.T) {
	d := NewDaemon("")
	alive := false
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) {
		if alive {
			return map[string]string{"用户名称": "u"}, nil
		}
		return map[string]string{}, nil
	}
	d.Login = func() (string, error) { return "您已在线", nil }
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.Tick()                        // 空表 miss=1
	clk = clk.Add(61 * time.Second) // 越过节流 → miss=2 判死
	d.Tick()                        // 判死 → 登录 → 矛盾#1
	if d.sessFalse != 1 {
		t.Fatalf("前置：应计 1 次矛盾，实际 %d", d.sessFalse)
	}
	alive = true
	clk = clk.Add(61 * time.Second) // 越过节流 → 复活拍
	d.Tick()                        // flux 活 → 清零
	if d.sessFalse != 0 {
		t.Errorf("flux 复活应清零矛盾计数，实际 %d", d.sessFalse)
	}
}

// 行为（2026-11-21 今晨 3.5h 假在线事故）：判死后的重登答「已在线」，接线了
// IP 级真值（SessionAlive/online_check）时不得直接点亮在线——账号级「已
// 在线」≠ 本 IP 会话恢复（账号可能绑在别处/别设备）。当场复核分流：
// 报活（flux 撒谎型）清判死回在线；报死（账号绑别处型）保持判死走 t37，
// 循环由 suspect 熔断截断。
func TestAlreadyOnlineVerifiedBySessionAlive(t *testing.T) {
	ipAlive := false
	logins := 0
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return ipAlive, nil }
	d.Login = func() (string, error) {
		logins++
		return "您已在线，请勿重复登录", nil
	}
	d.Tick() // 判死 → t37 → 登录 → 哨兵 → IP 复核死 → 保持判死
	if logins == 0 {
		t.Fatal("前置：判死应触发登录")
	}
	_, _, _, dead, _ := d.View()
	if !dead {
		t.Fatal("IP 复核死：哨兵不得清判死（今晨假绿形态）")
	}
	if d.State == StateOnline {
		t.Fatalf("IP 复核死：不得点亮在线，实际 %v/%v", d.State, d.Detail)
	}
	// IP 真值转活（会话真恢复）→ 下一拍复核清判死、恢复在线
	ipAlive = true
	clk = clk.Add(61 * time.Second)
	d.Tick()
	if d.State != StateOnline {
		t.Fatalf("IP 真值报活应恢复在线，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为（tester 回归 WARN 补覆盖）：哨兵「复核未知」分支——判死→重登答「已
// 在线」→online_check 复核 err（未知）→保持判死、不进 suspect 熔断计数、
// 文案不下「账号绑别处」的确定结论（未知证据不推进判定，缝契约同源）。
func TestAlreadyOnlineProbeUnknownHoldsWithoutSuspect(t *testing.T) {
	calls := 0
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	// 第 1 次（probeSession）答死判死；第 2 次（哨兵当场复核）答未知。
	d.SessionAlive = func() (bool, error) {
		calls++
		if calls == 1 {
			return false, nil
		}
		return false, errors.New("绑源解析超时")
	}
	d.Login = func() (string, error) { return "您已在线，请勿重复登录", nil }

	d.Tick() // 判死 → t37 → 登录 → 哨兵 → 复核未知
	if d.State != StateRejected || !strings.Contains(d.Detail, "复核不可得") {
		t.Fatalf("复核未知应报「复核不可得」并保持观察，实际 %v/%q", d.State, d.Detail)
	}
	if d.sessFalse != 1 {
		t.Fatalf("矛盾计数照记（flux 撒谎怀疑仍在），实际 %d", d.sessFalse)
	}
	if d.suspect != 0 {
		t.Fatalf("未知证据不得进 suspect 熔断计数，实际 %d", d.suspect)
	}
	_, _, _, dead, _ := d.View()
	if !dead {
		t.Fatal("未知路径应保持判死（下拍重问）")
	}
}

// 行为（trouble-maker P1-5 补覆盖）：哨兵「复核活」分支——判死→重登答「已
// 在线」→online_check 当场复核翻活（flux 撒谎型）→当场清判死回在线（专属文
// 案），且退避/瞬态连击陈账一并清零（P2-2 口径）。
func TestAlreadyOnlineProbeAliveClearsToOnline(t *testing.T) {
	probeCalls := 0
	logins := 0
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	// 探针第 1 次答死（probeSession 判死），第 2 次（哨兵当场复核）翻活。
	d.SessionAlive = func() (bool, error) {
		probeCalls++
		return probeCalls > 1, nil
	}
	d.Login = func() (string, error) {
		logins++
		return "您已在线，请勿重复登录", nil
	}
	d.rejectUntil = clk.Add(-time.Minute) // 过期陈账（非零但不挡登录闸）：path1 应清成零值
	d.transientStreak = 2

	d.Tick() // 判死 → t37 → 登录 → 哨兵 → 复核活 → path1
	if d.State != StateOnline {
		t.Fatalf("复核活应当场回在线，实际 %v/%q", d.State, d.Detail)
	}
	if !strings.Contains(d.Detail, "复核本 IP 会话在线") {
		t.Fatalf("复核活应带专属文案，实际 %q", d.Detail)
	}
	_, _, _, dead, _ := d.View()
	if dead {
		t.Fatal("复核活应清判死")
	}
	if logins != 1 {
		t.Fatalf("应只登录一次，实际 %d", logins)
	}
	if !d.rejectUntil.IsZero() || d.transientStreak != 0 {
		t.Fatalf("复核活应清退避与瞬态陈账，实际 rejectUntil=%v transient=%d", d.rejectUntil, d.transientStreak)
	}
}

// 行为：账号绑别处（IP 真值持续死）——哨兵循环由 suspect 熔断截断（连续 3
// 次后退避），状态点名账号绑定嫌疑，不再无限烧登录预算。
func TestAlreadyOnlineLoopCappedBySuspect(t *testing.T) {
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return false, nil } // 本 IP 恒死
	d.Login = func() (string, error) { return "您已在线，请勿重复登录", nil }
	// 循环拍：判死 → t37 登录 → 哨兵复核死 → 保持判死 → 下一拍再登……
	// suspect 同拍计数，第 3 次熔断退避。
	for i := range 6 {
		d.Tick()
		if d.State == StateRejected && strings.Contains(d.Detail, "熔断退避") {
			break // 熔断已截断循环（非熔断的「保持观察」拍不含此词）
		}
		if i == 5 {
			t.Fatalf("6 拍内 suspect 熔断应截断哨兵循环，实际 %v/%v", d.State, d.Detail)
		}
		clk = clk.Add(61 * time.Second) // 越过 60s 会话节流与 30s 登录节流
	}
	if d.rejectUntil.IsZero() || d.rejectUntil.Before(clk) {
		t.Fatal("熔断退避必须已设且指向未来")
	}
}
