package app

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"cnal/internal/keeper"
	"cnal/internal/netstack"
)

// 2026-11-10 事故的端到端回归：门户按「地址」记在线。旧账号流量用完后会话仍在，
// 面板切账号只注销 v6，v4 还挂在旧账号；新账号登录被答「用户已在线，不需要再次
// 认证」（判定器当成功哨兵），于是面板显示成功、实际没换人，用户只能去浏览器
// 手动注销再登录。修复后切换必须先注销 v4，再用门户读数验收归属。

// fakePortals 按地址归属记账的最小门户：online_check / logout / pwdLogin。
type fakePortal struct {
	mu      sync.Mutex
	owner   string // "" = v4 不在线
	logouts int
	logins  int
}

func (f *fakePortal) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.PostForm.Get("opr") {
	case "online_check":
		online := 0
		if f.owner != "" {
			online = 1
		}
		fmt.Fprintf(w, `{"success":true,"online":%d,"username":%q}`, online, f.owner)
	case "logout":
		f.logouts++
		f.owner = ""
		_, _ = w.Write([]byte(`{"success":true}`))
	case "pwdLogin":
		if f.owner != "" {
			// 门户对「地址已在线」的真实应答：成功哨兵语义，但归属不变。
			_, _ = w.Write([]byte(`{"success":false,"msg":"用户已在线，不需要再次认证"}`))
			return
		}
		f.logins++
		f.owner = r.PostForm.Get("userName")
		_, _ = w.Write([]byte(`{"success":true,"msg":"logon success"}`))
	default:
		fmt.Fprintf(w, `{"success":false,"msg":"unknown opr %q"}`, r.PostForm.Get("opr"))
	}
}

func (f *fakePortal) snapshot() (owner string, logouts, logins int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.owner, f.logouts, f.logins
}

// switchFixture 造一套「绑源解析失败 → 系统栈」的切换装配，登录走 deliverPinned
// （与生产同一条凭据出口），其它动作走 p.sessionOps（与面板同一条接线）。
func switchFixture(t *testing.T, dualStack bool) (*plumbing, *keeper.LoginAction, daemonConfig, *fakePortal) {
	t.Helper()
	fp := &fakePortal{owner: "old"} // 旧账号会话仍在线（流量用完但未注销）
	srv := httptest.NewServer(fp)
	t.Cleanup(srv.Close)

	cfg := daemonConfig{
		Server:       srv.URL,
		LoginPath:    "/ac_portal/login.php",
		LoginPage:    "/",
		UserAgent:    "test-agent",
		DualStack:    dualStack,
		BindIP:       "",
		ProbeTimeout: time.Second,
	}
	p := &plumbing{
		pt: newPortalTarget(func() (net.IP, error) {
			return nil, errors.New("DNS 应答疑似被劫持/污染")
		}, time.Minute),
		portalPort:   "443",
		netMode:      NetModeAuto,
		login:        netstack.New(net.IPv4(127, 0, 0, 1), 2*time.Second),
		systemLogin:  netstack.NewSystem(2 * time.Second),
		portal:       netstack.New(net.IPv4(127, 0, 0, 1), 2*time.Second),
		systemPortal: netstack.NewSystem(2 * time.Second),
	}
	action := &keeper.LoginAction{
		User: "old", Pass: "oldpw", DualStack: dualStack,
		Deliver: func(form map[string]string) (string, error) {
			return deliverPinned(cfg, p, form)
		},
	}
	p.action = action
	return p, action, cfg, fp
}

// 行为：v4 在线但归旧账号时，切换必须先注销 v4，再用新账号登录成功——
// 这是用户手动「浏览器先注销再登录」的自动化等价物。
func TestSwitchAccountReleasesV4ThenOwnsNewAccount(t *testing.T) {
	p, action, cfg, fp := switchFixture(t, false)
	ops := p.sessionOps(cfg)
	msg, err := switchAccount("new",
		func(string) (string, error) { return "newpw", nil },
		func(u, pw string) error { return action.Relocate(u, pw) },
		ops)
	if err != nil {
		t.Fatalf("切换应成功: %v", err)
	}
	owner, logouts, logins := fp.snapshot()
	if owner != "new" {
		t.Errorf("切换后 v4 应归 new，实际 %q（旧行为：只注销 v6，v4 留在 old）", owner)
	}
	if logouts == 0 {
		t.Error("认证前必须先注销 v4（门户对已在线地址的登录不会改归属）")
	}
	if logins != 1 {
		t.Errorf("注销后应恰好认证成功一次，实际 %d 次", logins)
	}
	if !strings.Contains(msg, "门户确认 v4 已归 new") {
		t.Errorf("回报应明确 v4 已归新账号，实际 %q", msg)
	}
	// 双栈关：v6 不参与，也不该报「不可观测」。
	if strings.Contains(msg, "不可观测") {
		t.Errorf("双栈关时不得出现 v6 不可观测告警，实际 %q", msg)
	}
}

// 行为：logoutV4 走「纯 v4」表单——不带 ipv4or6（门户页面在纯 v4 上就是空串，
// 服务端按请求源地址认栈）。带错地址会让注销落空。
func TestLogoutV4SendsPureV4Logout(t *testing.T) {
	got := make(chan url.Values, 1)
	p, srv := routeWiringFixture(t, NetModeAuto, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got <- r.PostForm
		_, _ = w.Write([]byte(`{"success":true}`))
	})
	cfg := daemonConfig{Server: srv.URL, LoginPath: "/ac_portal/login.php", UserAgent: "ua"}
	if err := p.logoutV4(cfg); err != nil {
		t.Fatalf("logoutV4 应经系统栈送达: %v", err)
	}
	form := <-got
	if form.Get("opr") != "logout" {
		t.Errorf("opr 应为 logout，实际 %q", form.Get("opr"))
	}
	if _, has := form["ipv4or6"]; has {
		t.Errorf("纯 v4 注销不得带 ipv4or6，实际 %v", form)
	}
}

// 行为：bound 手动强制且绑源解析失败 → 注销必须 fail-closed（不猜地址）。
func TestLogoutV4FailsClosedInBoundMode(t *testing.T) {
	var hits int32
	p, srv := routeWiringFixture(t, NetModeBound, func(w http.ResponseWriter, _ *http.Request) {
		hits++
	})
	cfg := daemonConfig{Server: srv.URL, LoginPath: "/ac_portal/login.php"}
	if err := p.logoutV4(cfg); err == nil {
		t.Fatal("bound 模式解析失败必须拒绝注销")
	}
	if hits != 0 {
		t.Errorf("fail-closed 不得发请求，实际 %d 次", hits)
	}
}
