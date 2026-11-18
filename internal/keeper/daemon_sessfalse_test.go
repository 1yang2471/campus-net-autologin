package keeper

import (
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
