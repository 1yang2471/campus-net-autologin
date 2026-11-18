package keeper

import (
	"strings"
	"testing"
)

// v6 补绑（2026-11-08 实证缺口）：Do() 在握手失败时会降级纯 v4（正确地保
// 主线），但「补绑」的唯一目的就是带上 ipv4or6——降级等于假补绑，必须拒绝。

// 行为：双栈开+握手绿 → 补绑表单携带 ipv4or6，且走 Deliver。
func TestRebindCarriesIPv4or6(t *testing.T) {
	var got map[string]string
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: true,
		Handshake: func() (string, error) {
			return "{'success':true, 'ip':'2001:db8:1::19', 'isOnline':false}", nil
		},
		Deliver: func(form map[string]string) (string, error) {
			got = form
			return "{'success':true, 'msg':'ok'}", nil
		},
	}
	if err := a.DoRebind(); err != nil {
		t.Fatalf("补绑应成功: %v", err)
	}
	if got["ipv4or6"] != "2001:db8:1::19" {
		t.Errorf("补绑表单必须携带 v6 地址，实际 %v", got)
	}
}

// 行为：握手失败 → 明确报错，且 Deliver 不得调用（不许降级成纯 v4 假补绑）。
func TestRebindRefusesDegradedLogin(t *testing.T) {
	delivered := false
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: true,
		Handshake: func() (string, error) { return "<h1>404</h1>", nil },
		Deliver: func(map[string]string) (string, error) {
			delivered = true
			return "{'success':true}", nil
		},
	}
	if err := a.DoRebind(); err == nil {
		t.Fatal("握手失败时补绑必须报错（降级纯 v4 = 假补绑）")
	}
	if delivered {
		t.Error("握手失败不得发登录表单")
	}
}

// 行为：双栈关 → 拒绝（没有 ipv4or6 语义的补绑无意义）。
func TestRebindRefusedWhenDualStackOff(t *testing.T) {
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: false,
		Deliver: func(map[string]string) (string, error) { return "{'success':true}", nil },
	}
	err := a.DoRebind()
	if err == nil || !strings.Contains(err.Error(), "双栈") {
		t.Errorf("双栈关应拒绝补绑，实际 %v", err)
	}
}

// 行为：门户未受理 → 报错带门户原文（面板据此显示，不静默成功）。
func TestRebindSurfacesPortalReason(t *testing.T) {
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: true,
		Handshake: func() (string, error) {
			return "{'success':true, 'ip':'2001:db8::1', 'isOnline':false}", nil
		},
		Deliver: func(map[string]string) (string, error) { return "{'success':false, 'msg':'NOAUTH'}", nil },
	}
	if err := a.DoRebind(); err == nil || !strings.Contains(err.Error(), "NOAUTH") {
		t.Errorf("门户未受理应报错带原文，实际 %v", err)
	}
}
