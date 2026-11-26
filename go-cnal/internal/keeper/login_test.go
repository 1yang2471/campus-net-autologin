package keeper

import (
	"errors"
	"strings"
	"testing"
)

// G4 组合根：握手→BuildLogin→POST 登录（Python do_login + _dualstack_field
// 三语义移植）。网络缝全替身，解析走真实 dialect/protocol 代码路径。

// 行为：双栈开+握手绿 → 登录表单携带现场取的对端 ip（t45 第一语义）。
func TestLoginCarriesIPv4or6WhenHandshakeGreen(t *testing.T) {
	var got map[string]string
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: true,
		Handshake: func() (string, error) {
			return "{'success':true, 'ip':'2001:db8:1::19', 'isOnline':false}", nil
		},
		Deliver: func(form map[string]string) (string, error) {
			got = form
			return "{'success':true, 'msg':'logon success'}", nil
		},
	}
	if _, err := a.Do(); err != nil {
		t.Fatalf("登录应成功: %v", err)
	}
	if got["ipv4or6"] != "2001:db8:1::19" {
		t.Errorf("登录应携带现场取的对端 ip，实际 %v", got)
	}
}

// 行为：DoBind 用调用方给定的 v6 地址认证（切账号时本机另一个公网 v6
// 地址也要绑到新账号）。不依赖 Handshake——地址是调用方枚举出来的。
func TestDoBindUsesGivenV6(t *testing.T) {
	var got map[string]string
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: true,
		Handshake: func() (string, error) { return "", errors.New("DoBind 不应再握手") },
		Deliver: func(form map[string]string) (string, error) {
			got = form
			return "{'success':true, 'msg':'logon success'}", nil
		},
	}
	out, err := a.DoBind("2001:db8:1::99")
	if err != nil {
		t.Fatalf("按给定地址认证应成功: %v", err)
	}
	if got["ipv4or6"] != "2001:db8:1::99" {
		t.Errorf("表单 ipv4or6 应是给定地址，实际 %v", got)
	}
	if out.V6 != V6Sent {
		t.Errorf("应报告 v6 已随登录发出，实际 %q", out.V6)
	}
}

// 行为：双栈没开时 DoBind 拒绝——没有绑定语义，发出去就是假补绑。
func TestDoBindRejectsWhenDualStackOff(t *testing.T) {
	a := &LoginAction{User: "u", Pass: "p", DualStack: false}
	if _, err := a.DoBind("2001:db8::1"); err == nil {
		t.Fatal("双栈未开必须拒绝按地址绑定")
	}
}

// 行为：握手失败/应答不可解析（夜间 404 实测形态）→ 登录退回纯 v4 照常发
// （absent ≠ fail，绝不挡 v4 主线；t45 第二语义）。
func TestLoginDegradesToPureV4OnHandshakeFail(t *testing.T) {
	var got map[string]string
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: true,
		Handshake: func() (string, error) { return "<h1>Not Found</h1>", nil },
		Deliver: func(form map[string]string) (string, error) {
			got = form
			return "{'success':true, 'msg':'ok'}", nil
		},
	}
	if _, err := a.Do(); err != nil {
		t.Fatalf("纯 v4 登录应照常成功: %v", err)
	}
	if _, has := got["ipv4or6"]; has {
		t.Errorf("握手失败不得携带字段: %v", got)
	}
}

// 行为：门户答 success:false +「您已在线」→ 视为成功并保留哨兵
// （Python parse_login / P-3 同源：重复登录不算失败；daemon 靠这个哨兵
// 累加 sessFalse 触发 3600s 自停闸，误判死不得变登录风暴）。
func TestLoginAlreadyOnlineIsSuccessSentinel(t *testing.T) {
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: false,
		Deliver: func(map[string]string) (string, error) {
			return "{'success':false, 'msg':'您已在线，请勿重复登录'}", nil
		},
	}
	out, err := a.Do()
	if err != nil {
		t.Fatalf("「已在线」应是成功语义，实际报错: %v", err)
	}
	if !alreadyOnline(out.Resp) {
		t.Errorf("返回值必须保留「已在线」哨兵供 daemon 判死链识别，实际 %q", out.Resp)
	}
}

// 行为：负向形态「账号不在线」→ 不得误判成已在线（Python P-3 防线）。
func TestLoginNegativeOnlineStaysRejected(t *testing.T) {
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: false,
		Deliver: func(map[string]string) (string, error) {
			return "{'success':false, 'msg':'账号不在线，请勿重复登录'}", nil
		},
	}
	if _, err := a.Do(); err == nil {
		t.Fatal("负向形态必须仍是登录失败")
	}
}

// 行为：默认关（异校零影响）→ 零握手调用、零字段——守护行为与旧版等价
// （t45 第三语义；即使 Handshake 缝可用也不许调用）。
func TestLoginDefaultOffMakesZeroHandshake(t *testing.T) {
	calls := 0
	var got map[string]string
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: false,
		Handshake: func() (string, error) { calls++; return "", nil },
		Deliver: func(form map[string]string) (string, error) {
			got = form
			return "{'success':true, 'msg':'ok'}", nil
		},
	}
	if _, err := a.Do(); err != nil {
		t.Fatalf("默认关登录应成功: %v", err)
	}
	if calls != 0 {
		t.Errorf("默认关必须零握手调用，实际 %d 次", calls)
	}
	if _, has := got["ipv4or6"]; has {
		t.Errorf("默认关登录体不得含字段: %v", got)
	}
}

// 行为：握手缝返回 error（如无可信 v6 源）→ 同样降级纯 v4。
func TestLoginDegradesOnHandshakeError(t *testing.T) {
	var got map[string]string
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: true,
		Handshake: func() (string, error) {
			return "", errors.New("无可信 v6 源")
		},
		Deliver: func(form map[string]string) (string, error) {
			got = form
			return "{'success':true, 'msg':'ok'}", nil
		},
	}
	if _, err := a.Do(); err != nil {
		t.Fatalf("降级纯 v4 应照常成功: %v", err)
	}
	if _, has := got["ipv4or6"]; has {
		t.Errorf("握手出错不得携带字段: %v", got)
	}
}

// 行为：门户答失败 → Do 报错且消息带响应摘要（守护据此显示原因）。
func TestLoginRejectSurfacesReason(t *testing.T) {
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: false,
		Deliver: func(form map[string]string) (string, error) {
			return "{'error':'auth fail'}", nil
		},
	}
	_, err := a.Do()
	if err == nil || !strings.Contains(err.Error(), "auth fail") {
		t.Errorf("登录被拒必须报错并带原因，实际 %v", err)
	}
}

// 行为：每次登录现场握手取值，不缓存——第二次登录重新取值（v6 临时地址
// 会轮换，缓存会绑到失效地址；规格 §5.3）。
func TestLoginFreshHandshakePerLogin(t *testing.T) {
	count := 0
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: true,
		Handshake: func() (string, error) {
			count++
			return "{'success':true, 'ip':'2001:db8::1', 'isOnline':false}", nil
		},
		Deliver: func(form map[string]string) (string, error) {
			return "{'success':true, 'msg':'ok'}", nil
		},
	}
	_, _ = a.Do()
	_, _ = a.Do()
	if count != 2 {
		t.Errorf("每次登录必须现场握手，实际 %d 次", count)
	}
}

// 行为：补绑被门户拒绝 → 错误只带一个前缀 + 门户原文（不叠加「登录未受理」）。
func TestDoRebindRejectedMessageSinglePrefix(t *testing.T) {
	a := &LoginAction{
		User: "u", Pass: "p", DualStack: true,
		Handshake: func() (string, error) {
			return "{'success':true, 'ip':'2001:db8::1'}", nil
		},
		Deliver: func(map[string]string) (string, error) {
			return "{'success':false, 'msg':'认证失败'}", nil
		},
	}
	err := a.DoRebind()
	if err == nil {
		t.Fatal("门户拒绝必须报错")
	}
	if !strings.Contains(err.Error(), "门户未受理补绑") || strings.Contains(err.Error(), "登录未受理") {
		t.Errorf("错误文案应只带补绑前缀 + 门户原文，实际 %q", err.Error())
	}
}

func TestLoginAfterSuccessRunsOnlyOnAccepted(t *testing.T) {
	for _, tc := range []struct {
		name string
		resp string
		err  error
		want int
	}{
		{name: "accepted", resp: "{'success':true, 'msg':'ok'}", want: 1},
		{name: "rejected", resp: "{'success':false, 'msg':'认证失败'}", want: 0},
		{name: "delivery error", err: errors.New("network down"), want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			a := &LoginAction{
				User: "u", Pass: "p",
				Deliver:      func(map[string]string) (string, error) { return tc.resp, tc.err },
				AfterSuccess: func() { calls++ },
			}
			_, _ = a.Do()
			if calls != tc.want {
				t.Fatalf("成功回调次数错误，实际 %d 期望 %d", calls, tc.want)
			}
		})
	}
}
