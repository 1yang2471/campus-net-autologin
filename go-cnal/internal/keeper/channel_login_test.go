package keeper

import (
	"errors"
	"testing"
	"time"
)

// 通道级自愈登录（2026-11-20 双通道方案 §8.3 M2）。
//
// 三条安全性质（本文件就是它们的护栏）：
//
//  1. **账号级预算共享**：通道登录与主链路登录共用同一份闸门——30s 最小间隔、
//     退避、冻结、以及 app 层 RateLimit（8 次/小时）。门户的反暴力冻结是
//     **账号级**的（实测 12 次/小时触发），按通道各给一份额度等于把冻结线
//     翻倍。实现上不新建闸门：通道登录动作收口在 Daemon 内，直接复用
//     d.lastLogin / d.rejectUntil / d.frozenUntil。
//  2. **可关**：通道级登录默认关（multi_channel=false）——沿用 2026-11-19
//     「旁路只观察不登录」的保守纪律；开启后由共享预算正面解决那条纪律的
//     顾虑（多出口登录会放大冻结风险）。
//  3. **不绕过判定**：只有门户劫持（unauth）才触发登录；黑洞/离线态不登录
//     （登录请求同样发不出去，白撞门户）。

// newChannelLoginDaemon 造一个带通道登录能力的守护。
func newChannelLoginDaemon(clk *time.Time, views *[]ChannelView, loginLog *[]string) *Daemon {
	d := NewDaemon("")
	d.Now = func() time.Time { return *clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"用户名称": "u"}, nil }
	d.SetMultiChannel(true)
	g := NewChannelGuard()
	g.Now = func() time.Time { return *clk }
	g.Enumerate = func() []ChannelView { return *views }
	g.Probe = func(v ChannelView) string { return ChannelUnauth }
	d.Channels = g
	d.ChannelLogin = func(v ChannelView) (string, error) {
		*loginLog = append(*loginLog, v.ID)
		return "logon success", nil
	}
	return d
}

// 行为：通道处于未认证态时，守护对该通道发起一次登录（用该通道的身份）。
func TestChannelLoginRunsForUnauthChannel(t *testing.T) {
	clk := time.Unix(1000, 0)
	var log []string
	views := []ChannelView{{ID: "以太网|10.141.53.34", Iface: "以太网", IP: "10.141.53.34"}}
	d := newChannelLoginDaemon(&clk, &views, &log)

	d.Tick()
	// 观察是异步的：等它落地后再看登录动作。
	// 先拍一次触发异步探测，等观察落地，再拍一次触发登录决策：
	// 观察是异步的（goroutine + 60s 节流），第一拍必然还没有读数。
	d.Tick()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(d.ChannelViews()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	d.Tick()

	if len(log) != 1 || log[0] != "以太网|10.141.53.34" {
		t.Fatalf("应对未认证通道登录一次，实际 %v", log)
	}
}

// 行为：通道登录与主链路登录共用 30s 最小间隔——通道登录刚发过，主链路
// 立刻就不许再登录（账号级共享，不是每通道各一份）。
func TestChannelLoginSharesAccountThrottle(t *testing.T) {
	clk := time.Unix(1000, 0)
	var log []string
	views := []ChannelView{{ID: "以太网|10.141.53.34", Iface: "以太网", IP: "10.141.53.34"}}
	d := newChannelLoginDaemon(&clk, &views, &log)
	d.Login = func() (string, error) { return "logon success", nil }

	// 先拍一次触发异步探测，等观察落地，再拍一次触发登录决策：
	// 观察是异步的（goroutine + 60s 节流），第一拍必然还没有读数。
	d.Tick()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(d.ChannelViews()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	d.Tick() // 通道登录发生
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(log) == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	// 紧接着主链路想登录：必须被 30s 闸门挡住（账号级共享）。
	d.mu.Lock()
	d.State = StateOffline // 逼主链路走登录路径
	d.mu.Unlock()
	d.Tick()
	if d.lastLogin.IsZero() {
		t.Fatal("主链路登录缝应被调用")
	}
	// 通道登录已消耗闸门：主链路这次不应发出（lastLogin 未更新为更晚时刻）。
	before := d.lastLogin
	d.mu.Lock()
	d.State = StateOffline
	d.mu.Unlock()
	d.Tick()
	if d.lastLogin.After(before) {
		t.Error("主链路登录不应越过通道登录占用的 30s 闸门")
	}
}

// 行为：multi_channel 关闭时不做任何通道登录（默认关，沿用 11-19 纪律）。
func TestChannelLoginDisabledByDefault(t *testing.T) {
	clk := time.Unix(1000, 0)
	var log []string
	views := []ChannelView{{ID: "以太网|10.141.53.34", Iface: "以太网", IP: "10.141.53.34"}}
	d := newChannelLoginDaemon(&clk, &views, &log)
	d.SetMultiChannel(false)

	// 先拍一次触发异步探测，等观察落地，再拍一次触发登录决策：
	// 观察是异步的（goroutine + 60s 节流），第一拍必然还没有读数。
	d.Tick()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(d.ChannelViews()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	d.Tick()
	time.Sleep(100 * time.Millisecond)
	if len(log) != 0 {
		t.Fatalf("通道登录默认应关闭，实际发出 %v", log)
	}
}

// 行为：黑洞/离线态不触发登录——登录请求同样发不出去，白撞门户还烧预算。
func TestChannelLoginSkipsBlackholeAndOffline(t *testing.T) {
	clk := time.Unix(1000, 0)
	var log []string
	d := NewDaemon("")
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"用户名称": "u"}, nil }
	d.SetMultiChannel(true)
	g := NewChannelGuard()
	g.Now = func() time.Time { return clk }
	g.Enumerate = func() []ChannelView {
		return []ChannelView{{ID: "以太网|10.141.53.34", Iface: "以太网", IP: "10.141.53.34"}}
	}
	g.Probe = func(v ChannelView) string { return ChannelBlackholed }
	d.Channels = g
	d.ChannelLogin = func(v ChannelView) (string, error) {
		log = append(log, v.ID)
		return "logon success", nil
	}

	// 先拍一次触发异步探测，等观察落地，再拍一次触发登录决策：
	// 观察是异步的（goroutine + 60s 节流），第一拍必然还没有读数。
	d.Tick()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(d.ChannelViews()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	d.Tick()
	time.Sleep(100 * time.Millisecond)
	if len(log) != 0 {
		t.Fatalf("黑洞态不得触发登录，实际 %v", log)
	}
}

// 行为：通道登录被拒时走与主链路同一套退避分类（硬错误长退避），且退避
// 对**账号**生效——所有通道一起退避，且**不被主链路的在线分支抹掉**
// （那正是实现时踩到的坑：退避刚设上就被清，通道继续撞门户烧预算）。
func TestChannelLoginRejectionSharesBackoff(t *testing.T) {
	clk := time.Unix(1000, 0)
	var log []string
	views := []ChannelView{{ID: "以太网|10.141.53.34", Iface: "以太网", IP: "10.141.53.34"}}
	d := newChannelLoginDaemon(&clk, &views, &log)
	d.ChannelLogin = func(v ChannelView) (string, error) {
		log = append(log, v.ID)
		return "", errors.New("密码不匹配")
	}

	// 先拍一次触发异步探测，等观察落地，再拍一次触发登录决策：
	// 观察是异步的（goroutine + 60s 节流），第一拍必然还没有读数。
	d.Tick()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(d.ChannelViews()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	d.Tick() // 观察落地后再拍一次：登录决策在 Tick 里做
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(log) == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	if d.channelRejectDeadline().IsZero() {
		t.Fatal("硬错误应进入通道退避")
	}
	// 主链路的在线分支不得抹掉通道退避：主链路会话活着（FetchFlux 有数据），
	// 但通道刚被拒——抹掉退避等于下一拍继续撞门户。
	clk = clk.Add(10 * time.Second)
	d.Tick()
	if d.channelRejectDeadline().IsZero() {
		t.Fatal("主链路的在线分支不得抹掉通道退避（那会让通道继续撞门户）")
	}
	// 退避窗口内不得再登录该通道。
	before := len(log)
	clk = clk.Add(10 * time.Second)
	d.Tick()
	time.Sleep(100 * time.Millisecond)
	if len(log) != before {
		t.Fatalf("退避窗口内不得再登录通道，实际多发 %d 次", len(log)-before)
	}
}
