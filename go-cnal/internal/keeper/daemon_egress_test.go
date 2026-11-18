package keeper

import (
	"fmt"
	"strings"
	"testing"
)

// 服务出口门槛（2026-11-18 多源抖动实证）：绑定源在选中后几十秒才真正失效
//（17:47:39 选中 10.141.53.34，17:48:19 绑源 DNS 超时），而门户/登录会回退
// 系统栈——若把「本地绑定失效」当成「会话丢失」，就会用另一条路登录，自己的
// 探针永远不绿，烧预算并触发自伤熔断（17:48:34/17:49:09/17:49:41 三连登录，
// 17:49:46「连续 3 次登录成功但探测仍失败」熔断 600s）。
//
// 行为判据：出口不健康时，无论 L1 红多少拍，都不得发登录；先报状态 + 重选。

func invalidEgress(reason EgressReason) EgressHealth {
	return EgressHealth{
		Valid:  false,
		Reason: reason,
		Detail: "绑定的网卡地址已失效",
		Source: "10.141.53.34",
	}
}

// 行为：出口绑定失效 → 连续 5 拍（跨过去抖阈值）也不得登录；状态说清
// 「服务出口不可用、正在重选」，并每拍报告重选时机（组合根自己按 30s 节流）。
func TestInvalidEgressBlocksLogin(t *testing.T) {
	calls := 0
	reselect := 0
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.EgressHealth = func() EgressHealth { return invalidEgress(EgressBindingInvalid) }
	d.OnEgressInvalid = func() { reselect++ }
	d.Login = func() (string, error) { calls++; return "", nil }

	for i := 0; i < 5; i++ {
		d.Tick()
	}
	if calls != 0 {
		t.Fatalf("出口失效时不得登录（跨过 3 拍去抖也不行），实际 %d 次", calls)
	}
	if reselect != 5 {
		t.Fatalf("出口失效每拍应报告重选时机，实际 %d 次", reselect)
	}
	if d.State != StateOffline {
		t.Errorf("出口失效应显示离线态，实际 %v/%v", d.State, d.Detail)
	}
	if !strings.Contains(d.Detail, "出口") || !strings.Contains(d.Detail, "正在重选") {
		t.Errorf("状态行应说清出口失效与重选，实际 %q", d.Detail)
	}
}

// 行为：出口健康但到不了门户（绑源 DNS/门户不通）→ 沿用 portal_down 语义，
// 报告既有 OnPortalDown 重发现时机，不登录。
func TestEgressPortalUnreachableUsesPortalDown(t *testing.T) {
	calls := 0
	rediscover := 0
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.EgressHealth = func() EgressHealth { return invalidEgress(EgressPortalUnreachable) }
	d.OnPortalDown = func() { rediscover++ }
	d.Login = func() (string, error) { calls++; return "", nil }

	d.Tick()
	if calls != 0 {
		t.Fatalf("出口到不了门户时不得登录，实际 %d 次", calls)
	}
	if rediscover != 1 {
		t.Fatalf("应报告 portal_down 重发现时机，实际 %d 次", rediscover)
	}
	if d.State != StatePortalDown {
		t.Errorf("应显示认证服务器不可达，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：免认证窗口内出口失效是常态 → 报免认证时段，不重选、不登录。
func TestInvalidEgressInFreeWindowShowsFreeWindow(t *testing.T) {
	calls := 0
	reselect := 0
	d := NewDaemon("00:00-24:00")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.EgressHealth = func() EgressHealth { return invalidEgress(EgressBindingInvalid) }
	d.OnEgressInvalid = func() { reselect++ }
	d.Login = func() (string, error) { calls++; return "", nil }

	d.Tick()
	if calls != 0 {
		t.Fatalf("免认证时段不得登录，实际 %d 次", calls)
	}
	if reselect != 0 {
		t.Fatalf("免认证时段不报告重选，实际 %d 次", reselect)
	}
	if d.State != StateFreeWindow {
		t.Errorf("应显示免认证时段，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：门槛放行后出口才失效（竞态）→ 登录缝返回 ErrEgressInvalid：
// 不算一次登录尝试（不占 30s 闸）、不进退避，报状态并请求重选；下一拍
// 仍可重新尝试。
func TestLoginEgressInvalidSentinelDoesNotConsumeAttempt(t *testing.T) {
	calls := 0
	reselect := 0
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.EgressHealth = func() EgressHealth { return EgressHealth{Valid: true} }
	d.OnEgressInvalid = func() { reselect++ }
	d.Login = func() (string, error) {
		calls++
		if calls == 1 {
			return "", fmt.Errorf("%w：bind: address not valid", ErrEgressInvalid)
		}
		return "", nil
	}

	for i := 0; i < 3; i++ {
		d.Tick() // 第 3 拍进入登录 → 哨兵返回
	}
	if calls != 1 {
		t.Fatalf("前置：应已尝试 1 次，实际 %d", calls)
	}
	if d.State != StateOffline {
		t.Errorf("出口失效应报离线态，实际 %v/%v", d.State, d.Detail)
	}
	if reselect != 1 {
		t.Errorf("应请求重选 1 次，实际 %d", reselect)
	}
	if !d.lastLogin.IsZero() {
		t.Error("出口失效的登录尝试不得占用 30s 闸")
	}

	d.Tick()
	if calls != 2 {
		t.Fatalf("哨兵后下一拍应重新尝试，实际 %d 次", calls)
	}
}

// 行为：绑源到不了门户的哨兵走 portal_down（不把当前源标降级——一次 DNS
// 抖动不等于网卡死了），同样不占 30s 闸。
func TestLoginEgressPortalSentinelUsesPortalDown(t *testing.T) {
	calls := 0
	rediscover := 0
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.EgressHealth = func() EgressHealth { return EgressHealth{Valid: true} }
	d.OnPortalDown = func() { rediscover++ }
	d.OnEgressInvalid = func() { t.Error("门户不可达不得走绑定失效回调") }
	d.Login = func() (string, error) {
		calls++
		return "", fmt.Errorf("%w：绑源解析超时", ErrEgressPortalUnreachable)
	}

	for i := 0; i < 3; i++ {
		d.Tick()
	}
	if calls != 1 {
		t.Fatalf("前置：应已尝试 1 次，实际 %d", calls)
	}
	if d.State != StatePortalDown {
		t.Errorf("应报认证服务器不可达，实际 %v/%v", d.State, d.Detail)
	}
	if rediscover != 1 {
		t.Errorf("应报告 portal_down 重发现时机 1 次，实际 %d", rediscover)
	}
	if !d.lastLogin.IsZero() {
		t.Error("出口不可达的登录尝试不得占用 30s 闸")
	}
}

// 行为：出口健康（显式 Valid=true）时行为不变——连续 3 拍 L1 红照常登录。
// 守住「新增门槛不误伤正常路径」。
func TestHealthyEgressKeepsLoginPath(t *testing.T) {
	calls := 0
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.EgressHealth = func() EgressHealth { return EgressHealth{Valid: true} }
	d.OnEgressInvalid = func() { t.Error("健康出口不得报告重选") }
	d.Login = func() (string, error) { calls++; return "", nil }

	for i := 0; i < 3; i++ {
		d.Tick()
	}
	if calls != 1 {
		t.Fatalf("健康出口连续 3 拍 L1 红应登录一次，实际 %d 次", calls)
	}
}
