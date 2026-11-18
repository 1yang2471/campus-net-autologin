package protocol

import "testing"

// 门户会话 op 的证据来自门户自己的 logic_new.js（2026-11-09 从登录页抓取）：
//   function onLogout(){ logoutRequest({opr:'logout','ipv4or6':g_ipv4or6}, ...) }
//   logoutRequest 内部先 syncGetIpv4or6()（即 opr:'ipv4or6online' 取 ip），
//   再带着 ipv4or6 发 logout。
//   online_check 的响应被页面用来读 o.username，并据此决定能否注销。
// 也就是说这两个 op 都是**按栈**的，且都不带凭据（门户按请求源地址认客户端）。

// 行为：online_check 报出「这一栈挂在谁名下」——online 在真实响应里是数字 1。
func TestParseOnlineCheckReportsOwner(t *testing.T) {
	// 真实响应形态（逐字，账号已脱敏为 USER）
	st, err := ParseOnlineCheck(`{"success":true, "online":1, "username":"USER"}`)
	if err != nil {
		t.Fatalf("应能解析: %v", err)
	}
	if !st.Online {
		t.Error("online:1 必须读成在线（门户用的是数字，不是 true）")
	}
	if st.Username != "USER" {
		t.Errorf("归属账号应读出，实际 %q", st.Username)
	}
}

// 行为：online:0 → 不在线且无归属，不误报账号。
func TestParseOnlineCheckOffline(t *testing.T) {
	for _, raw := range []string{
		`{"success":true, "online":0}`,
		`{"success":true, "online":false}`,
		`{'success':true, 'online':0, 'username':''}`,
	} {
		st, err := ParseOnlineCheck(raw)
		if err != nil {
			t.Fatalf("%s 应能解析: %v", raw, err)
		}
		if st.Online || st.Username != "" {
			t.Errorf("%s 应判不在线且无归属，实际 %+v", raw, st)
		}
	}
}

// 行为：解析不出结构（HTML 劫持页/空响应）→ 报错，不猜成"不在线"。
// 猜成不在线会让调用方去"补绑"，而实际可能只是读不懂。
func TestParseOnlineCheckRejectsUnparseable(t *testing.T) {
	for _, raw := range []string{"", "   ", "<html>ac_portal</html>", "{oops"} {
		if _, err := ParseOnlineCheck(raw); err == nil {
			t.Errorf("%q 应报错，不能猜成不在线", raw)
		}
	}
}

// 行为：按栈 op 的表单形状——有地址就带 ipv4or6，没有就只发 opr
// （门户页面注销时 g_ipv4or6 可能为空串，等价于不带）。
func TestOpForm(t *testing.T) {
	f := OpForm("logout", "2001:db8::1")
	if f["opr"] != "logout" || f["ipv4or6"] != "2001:db8::1" {
		t.Errorf("带地址时应两个键都在，实际 %v", f)
	}
	f = OpForm("online_check", "")
	if f["opr"] != "online_check" {
		t.Errorf("缺 opr，实际 %v", f)
	}
	if _, has := f["ipv4or6"]; has {
		t.Errorf("地址为空时不应带 ipv4or6，实际 %v", f)
	}
}
