package app

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cnal/internal/keeper"
	"cnal/internal/netstack"
)

// 装配完整性：Compose 拆文件后最容易犯的错是「某一缝忘了接」——那种错
// 编译能过、单测也过，只在真机某条路径上才暴露。这里把新拆出的
// newDaemon/newAPI 单独跑一遍，逐缝断言非 nil。

func TestNewDaemonWiresAllSeams(t *testing.T) {
	cfg := defaultAdvancedConfig()
	pl := &plumbing{
		action: &keeper.LoginAction{},
		rl:     keeper.NewRateLimit(8, time.Hour, 15*time.Minute, time.Now),
	}
	d := newDaemon(cfg, pl)
	if d.ProbeOnline == nil {
		t.Error("ProbeOnline 缝未接")
	}
	if d.PortalAlive == nil {
		t.Error("PortalAlive 缝未接")
	}
	if d.ProbeV6 == nil {
		t.Error("ProbeV6 缝未接")
	}
	if d.ProbeL2 == nil {
		t.Error("ProbeL2 缝未接")
	}
	if d.FetchFlux == nil {
		t.Error("FetchFlux 缝未接")
	}
	if d.Login == nil {
		t.Error("Login 缝未接")
	}
	if d.EgressHealth == nil {
		t.Error("EgressHealth 缝未接（出口门槛会静默失效）")
	}
	// rd=nil（bind_ip 手工钉源）时不得接 OnPortalDown——automatic 重发现
	// 不得抢用户显式钉源的方向盘。
	if d.OnPortalDown != nil {
		t.Error("rd=nil（手工钉源）时不应接 OnPortalDown")
	}
	if d.OnEgressInvalid != nil {
		t.Error("rd=nil（手工钉源）时不应接 OnEgressInvalid")
	}
}

func TestNewDaemonWiresOnPortalDownWhenRediscoverEnabled(t *testing.T) {
	cfg := defaultAdvancedConfig()
	pl := &plumbing{
		action: &keeper.LoginAction{},
		rl:     keeper.NewRateLimit(8, time.Hour, 15*time.Minute, time.Now),
		rd:     newRediscoverer(time.Minute, nil, func() net.IP { return nil }, func(net.IP) {}),
	}
	d := newDaemon(cfg, pl)
	if d.OnPortalDown == nil {
		t.Error("启用重发现时应接 OnPortalDown")
	}
	if d.OnEgressInvalid == nil {
		t.Error("启用重发现时应接 OnEgressInvalid（绑定失效要立即重选）")
	}
}

func TestNewAPIWiresAllSeams(t *testing.T) {
	cfg := defaultAdvancedConfig()
	cfg.Server = "https://portal.example"
	pl := &plumbing{
		action: &keeper.LoginAction{},
		rl:     keeper.NewRateLimit(8, time.Hour, 15*time.Minute, time.Now),
	}
	d := newDaemon(cfg, pl)
	a := newAPI(&cfg, pl, d)
	if a.FetchFlux == nil {
		t.Error("FetchFlux 缝未接")
	}
	if a.Snapshot == nil {
		t.Error("Snapshot 缝未接")
	}
	if a.Login == nil {
		t.Error("Login 缝未接")
	}
	if a.RebindV6 == nil {
		t.Error("RebindV6 缝未接")
	}
	if a.Logs == nil {
		t.Error("Logs 缝未接")
	}
	if a.GetConfig == nil {
		t.Error("GetConfig 缝未接")
	}
	if a.SaveCredentials == nil {
		t.Error("SaveCredentials 缝未接")
	}
	if a.Accounts == nil {
		t.Error("Accounts 缝未接")
	}
	if a.SwitchAccount == nil {
		t.Error("SwitchAccount 缝未接")
	}
	if a.ValidateConfig == nil {
		t.Error("ValidateConfig 缝未接")
	}
	if a.SaveConfig == nil {
		t.Error("SaveConfig 缝未接")
	}
}

// 行为（2026-11-21 产品优化决议 C）：守护动作时间线接线——Snapshot 闭包
// 带上 keeper 的最近动作（kind + 时刻），面板据此渲染「N 分钟前·自动登录」。
// 漏接的形态与 2026-11-07 漏填 Color 同类：编译过、单测过，只在真机上空白。
func TestSnapshotCarriesLastAction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>登录</html>"))
	}))
	t.Cleanup(srv.Close)
	cfg := defaultAdvancedConfig()
	cfg.Server = srv.URL
	pl := &plumbing{
		pt:        newPortalTarget(func() (net.IP, error) { return net.IPv4(127, 0, 0, 1), nil }, time.Minute),
		portal:    netstack.New(net.IPv4(127, 0, 0, 1), 2*time.Second),
		currentV6: func() net.IP { return nil },
		action:    &keeper.LoginAction{},
		rl:        keeper.NewRateLimit(8, time.Hour, 15*time.Minute, time.Now),
	}
	d := newDaemon(cfg, pl)
	d.RecordAction("自动登录")
	a := newAPI(&cfg, pl, d)
	snap := a.Snapshot()
	if snap.LastAction != "自动登录" {
		t.Errorf("Snapshot 应带最近动作 kind，实际 %q", snap.LastAction)
	}
	if snap.LastActionAt.IsZero() {
		t.Error("Snapshot 应带最近动作时刻（面板算「N 分钟前」用）")
	}
}

// 面板 GET /config 的键集合：拆文件后 GetConfig 仍须给出全部常用键。
// multi_channel 2026-11-20 补：漏了这个键，面板保存成功后回读拿不到它，
// 勾选被清成未勾选——用户感知「点多通道自愈再保存一直失败」。
func TestNewAPIGetConfigKeys(t *testing.T) {
	cfg := defaultAdvancedConfig()
	cfg.Server = "https://portal.example"
	cfg.BindIP = ""
	pl := &plumbing{action: &keeper.LoginAction{}, rl: keeper.NewRateLimit(8, time.Hour, 15*time.Minute, time.Now)}
	d := newDaemon(cfg, pl)
	a := newAPI(&cfg, pl, d)
	view := a.GetConfig()
	for _, k := range []string{"username", "server", "free_windows", "auto_reconnect", "bind_ip", "login_form",
		"multi_channel", "interval", "probe_timeout"} {
		if _, ok := view[k]; !ok {
			t.Errorf("GetConfig 缺键 %s（面板回填会空白）", k)
		}
	}
	if view["server"] != "https://portal.example" {
		t.Errorf("server 应回显配置值，实际 %q", view["server"])
	}
	if view["multi_channel"] != "false" {
		t.Errorf("multi_channel 默认应为 false，实际 %q", view["multi_channel"])
	}
}

// 行为：绑定失效回调必须先降级当前源再重选——否则 Refresh 会再次探到刚死的
// 候选（今天 17:43/17:46/17:56 热点被反复选中的形态）。
func TestOnEgressInvalidMarksPickerDown(t *testing.T) {
	a := net.ParseIP("10.141.53.34")
	pick := netstack.NewSourcePicker(
		func() []net.IP { return []net.IP{a} },
		func(net.IP) error { return nil })
	if _, err := pick.Refresh(); err != nil {
		t.Fatalf("前置：首次应选中 %s: %v", a, err)
	}
	cfg := defaultAdvancedConfig()
	pl := &plumbing{
		action:    &keeper.LoginAction{},
		rl:        keeper.NewRateLimit(8, time.Hour, 15*time.Minute, time.Now),
		picker:    pick,
		currentV4: func() net.IP { return a },
		rd: newRediscoverer(time.Minute, a,
			func() net.IP { ip, _ := pick.Refresh(); return ip },
			func(net.IP) {}),
	}
	d := newDaemon(cfg, pl)
	if d.OnEgressInvalid == nil {
		t.Fatal("启用重发现时应接 OnEgressInvalid")
	}
	d.OnEgressInvalid()
	if !strings.Contains(pick.Summary(), "降级中") {
		t.Fatalf("绑定失效应把当前源标降级，摘要：%q", pick.Summary())
	}
}
