package keeper

import (
	"errors"
	"testing"
)

// 2026-11-09 事故：切账号后 v4 换了、v6 还挂旧账号，而门户原文里看不出
// 「这次登录到底带没带 ipv4or6」——发起方不回报，这个事实就没人知道。
// 三态而不是布尔：跟 protocol.StackState 同一纪律，「没开双栈」是正常，
// 「开了却没带上」是静默降级，两者绝不能塌缩成同一个值。

// 行为：双栈开 + 握手绿 → V6Sent，且表单真带 ipv4or6，门户原文不丢。
func TestDoReportsV6Sent(t *testing.T) {
	var got map[string]string
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: true,
		Handshake: func() (string, error) {
			return "{'success':true, 'ip':'2001:db8:1::19'}", nil
		},
		Deliver: func(form map[string]string) (string, error) {
			got = form
			return "{'success':true, 'msg':'ok'}", nil
		},
	}
	out, err := a.Do()
	if err != nil {
		t.Fatalf("登录应成功: %v", err)
	}
	if out.V6 != V6Sent {
		t.Errorf("带了 ipv4or6 应报 V6Sent，实际 %q", out.V6)
	}
	if got["ipv4or6"] != "2001:db8:1::19" {
		t.Errorf("表单应带 ipv4or6，实际 %v", got)
	}
	if out.Resp == "" {
		t.Error("Outcome.Resp 必须保留门户原文（判定器靠它）")
	}
}

// 行为：双栈开 + 握手失败 → 照常发纯 v4（保 v4 主线，不报错），但实况是
// V6Degraded——这就是「v6 没跟着换」的现场证据，必须留下。
func TestDoReportsV6DegradedOnHandshakeFail(t *testing.T) {
	var got map[string]string
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: true,
		Handshake: func() (string, error) { return "", errors.New("boom") },
		Deliver: func(form map[string]string) (string, error) {
			got = form
			return "{'success':true, 'msg':'ok'}", nil
		},
	}
	out, err := a.Do()
	if err != nil {
		t.Fatalf("降级纯 v4 不得让登录失败: %v", err)
	}
	if out.V6 != V6Degraded {
		t.Errorf("握手失败应报 V6Degraded，实际 %q", out.V6)
	}
	if _, has := got["ipv4or6"]; has {
		t.Errorf("降级后不得携带字段: %v", got)
	}
}

// 行为：双栈关 → V6Off（不是 degraded），且根本不碰握手缝——异校零影响。
func TestDoReportsV6OffWhenDualStackDisabled(t *testing.T) {
	calls := 0
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: false,
		Handshake: func() (string, error) {
			calls++
			return "{'success':true, 'ip':'2001:db8:1::19'}", nil
		},
		Deliver: func(map[string]string) (string, error) { return "{'success':true}", nil },
	}
	out, err := a.Do()
	if err != nil {
		t.Fatalf("登录应成功: %v", err)
	}
	if out.V6 != V6Off {
		t.Errorf("双栈关应报 V6Off，实际 %q", out.V6)
	}
	if calls != 0 {
		t.Errorf("双栈关不得调握手，实际调了 %d 次", calls)
	}
}
