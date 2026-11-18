package app

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"cnal/internal/keeper"
)

// 出口健康分类（app 层）：把 L1 探针的失败原因和门户选路结果翻成内核能用的
// 证据。判据来自今天日志：
//   - 17:43:15 / 17:56:40「bind: The requested address is not valid in its
//     context」→ 绑定失效；
//   - 17:48:19「绑源解析失败（lookup ... i/o timeout），转系统栈」→ 绑源到
//     不了门户；
//   - 两者都不成立才按健康处理（正常路径零影响）。

func testPlumbingV4(src string) *plumbing {
	p := &plumbing{netMode: NetModeAuto}
	p.currentV4 = func() net.IP { return net.ParseIP(src) }
	return p
}

// 行为：L1 报 bind 失败 → 出口失效（binding_invalid），带源地址与原始原因。
func TestEgressHealthBindingInvalid(t *testing.T) {
	p := testPlumbingV4("10.141.53.34")
	p.setL1(l1Outcome{
		Kind:   l1BindInvalid,
		Detail: "bind: The requested address is not valid in its context.",
	})
	eg := p.egressHealth()
	if eg.Valid {
		t.Fatal("绑定失效不得判为健康")
	}
	if eg.Reason != keeper.EgressBindingInvalid {
		t.Errorf("原因应为 binding_invalid，实际 %q", eg.Reason)
	}
	if !strings.Contains(eg.Detail, "bind:") {
		t.Errorf("原因应保留原始 bind 错误，实际 %q", eg.Detail)
	}
	if eg.Source != "10.141.53.34" {
		t.Errorf("应带当前绑定源，实际 %q", eg.Source)
	}
}

// 行为：L1 绿但门户选路已回退系统栈 → 绑源到不了门户（portal_unreachable）。
// 这正是 17:48:19 的形态：绑源解析超时，系统栈却能把门户页拉回来。
func TestEgressHealthPortalUnreachableOnSystemRoute(t *testing.T) {
	p := testPlumbingV4("10.141.53.34")
	p.setL1(l1Outcome{OK: true})
	p.lastRoute = routeSystem
	p.lastWhy = "绑源解析失败（lookup a.stu.edu.cn: i/o timeout），转系统栈"

	eg := p.egressHealth()
	if eg.Valid {
		t.Fatal("绑源到不了门户不得判为健康")
	}
	if eg.Reason != keeper.EgressPortalUnreachable {
		t.Errorf("原因应为 portal_unreachable，实际 %q", eg.Reason)
	}
	if !strings.Contains(eg.Detail, "绑源解析失败") {
		t.Errorf("原因应保留选路解释，实际 %q", eg.Detail)
	}
}

// 行为：绑源路径健康 → Valid=true（正常路径零影响）。
func TestEgressHealthValidOnBoundRoute(t *testing.T) {
	p := testPlumbingV4("172.29.153.208")
	p.setL1(l1Outcome{OK: true})
	p.lastRoute = routeBound
	if eg := p.egressHealth(); !eg.Valid {
		t.Fatalf("绑源路径健康应 Valid=true，实际 %+v", eg)
	}
}

// 行为：net_mode=system 是用户显式选择 → 系统栈路径不算出口失效。
func TestEgressHealthSystemModeKeepsValid(t *testing.T) {
	p := testPlumbingV4("10.0.0.2")
	p.netMode = NetModeSystem
	p.setL1(l1Outcome{OK: true})
	p.lastRoute = routeSystem
	if eg := p.egressHealth(); !eg.Valid {
		t.Fatalf("system 模式不得判出口失效，实际 %+v", eg)
	}
}

// 行为：L1 错误分类——bind 失效 / 超时 / 不可达 / 其余；分类只用于证据，
// 不改变红绿判定。
func TestClassifyL1Err(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "bind 失效（今天 17:43:15 原文）",
			err:  errors.New(`Get "http://223.5.5.5/generate_204": dial tcp 10.158.52.162:0->223.5.5.5:80: bind: The requested address is not valid in its context.`),
			want: l1BindInvalid,
		},
		{name: "超时", err: errors.New("dial tcp 223.5.5.5:80: i/o timeout"), want: l1Timeout},
		{name: "无路由", err: errors.New("dial tcp: connectex: No route to host"), want: l1Unreachable},
		{name: "其它", err: errors.New("connection reset by peer"), want: l1AnchorDown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kind, detail := classifyL1Err(c.err)
			if kind != c.want {
				t.Errorf("分类 = %q，期望 %q", kind, c.want)
			}
			if detail == "" {
				t.Error("分类必须保留原始错误文本，不能吞掉")
			}
		})
	}
}

// 行为：自动会话判死只走绑源——绑源解析门户失败就返回错误（未知），绝不
// 回退系统栈。今天 17:48 的「A 报状态、B 问会话」就是回退造成的。
func TestOnlineV4BoundDoesNotFallBackToSystem(t *testing.T) {
	p := &plumbing{
		portalPort: "443",
		pt: newPortalTarget(func() (net.IP, error) {
			return nil, errors.New("lookup a.stu.edu.cn: i/o timeout")
		}, time.Minute),
	}
	_, err := p.onlineV4Bound(daemonConfig{
		Server:    "https://a.stu.edu.cn",
		LoginPath: "/ac_portal/login.php",
	})
	if err == nil {
		t.Fatal("绑源解析失败时必须返回错误（未知），不得回退系统栈")
	}
	if !strings.Contains(err.Error(), "绑源解析门户失败") {
		t.Errorf("错误须说清是绑源解析失败，实际 %q", err)
	}
}
