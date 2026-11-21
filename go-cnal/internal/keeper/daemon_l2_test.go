package keeper

import (
	"strings"
	"testing"
	"time"
)

// 行为（2026-11-21 实测复盘）：SESSION_FALSE 熔断只停 flux 回落（假判死的
// 信号源），不得连坐 SessionAlive——IP 级真值停摆 1 小时，会话真死无人报，
// 假绿窗口从此打开（免认证窗口结束 3.5h 假绿的成因链一环）。
func TestFluxOffKeepsSessionAlive(t *testing.T) {
	aliveCalls := 0
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { aliveCalls++; return false, nil }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{}, nil }
	d.fluxOffUntil = clk.Add(time.Hour) // 熔断期内
	d.Tick()
	if aliveCalls == 0 {
		t.Fatal("熔断期内 SessionAlive 仍须核实——IP 级真值不受 flux 熔断连坐")
	}
	_, _, _, dead, _ := d.View()
	if !dead {
		t.Fatal("熔断期内 SessionAlive 报死应判死（假在线封堵接管）")
	}
}

// L2 出口策略观察（移植 Python probe_l2_restricted 语义）：认证会话正常但
// 大众站被出口 DPI RST（SNI 黑名单风控窗口，2026-11-04 实测约 15 分钟自愈）。
// L1 对此失明（miui 锚点在白名单）。只观察、绝不自动"修复"——对抗 DPI 属违规；
// 结果上浮到在线态 detail（只写日志用户看不见，2026-11-05 实测被误当守护失职）。

func newL2Daemon(clk *time.Time, restricted string, probe *int) *Daemon {
	d := NewDaemon("")
	d.Now = func() time.Time { return *clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"用户名称": "u"}, nil }
	d.ProbeL2 = func() string {
		*probe++
		return restricted
	}
	return d
}

// 行为：L2 受限 → 在线态 detail 显示受限窗口（比 v6 文案优先——直接影响体验）。
func TestL2RestrictedShownInOnlineDetail(t *testing.T) {
	clk := time.Unix(0, 0)
	probe := 0
	d := newL2Daemon(&clk, "restricted", &probe)
	d.Tick()
	if d.State != StateOnline {
		t.Fatalf("前置：应在线，实际 %v/%v", d.State, d.Detail)
	}
	if !strings.Contains(d.Detail, "出口受限") {
		t.Errorf("L2 受限应上浮到 detail，实际 %q", d.Detail)
	}
}

// 行为：L2 正常 → detail 不含受限文案（v6 文案正常展示）。
func TestL2NormalKeepsV6Detail(t *testing.T) {
	clk := time.Unix(0, 0)
	probe := 0
	d := newL2Daemon(&clk, "ok", &probe)
	d.ProbeV6 = func() string { return "online" }
	d.Tick()
	if strings.Contains(d.Detail, "出口受限") {
		t.Errorf("L2 正常不得显示受限，实际 %q", d.Detail)
	}
	if !strings.Contains(d.Detail, "IPv6 在线") {
		t.Errorf("v6 文案应保留，实际 %q", d.Detail)
	}
}

// 行为：受限观察有保留窗口——恢复后 detail 仍提示一段时间（Python：观察结果
// 记入 _l2_window_until，受限窗口要上浮到面板，2×探测周期）。
func TestL2WindowPersistsAfterRecovery(t *testing.T) {
	clk := time.Unix(0, 0)
	probe := 0
	d := newL2Daemon(&clk, "restricted", &probe)
	d.Tick() // 受限 #1，窗口开启
	// 换成正常（下一节拍结果翻转）
	clk = clk.Add(61 * time.Second) // 过 L2 节流窗口
	d.ProbeL2 = func() string {
		probe++
		return "ok"
	}
	d.Tick() // 观察到正常，但保留窗口未过
	if !strings.Contains(d.Detail, "出口受限") {
		t.Errorf("恢复后保留窗口内仍应提示，实际 %q", d.Detail)
	}
	clk = clk.Add(130 * time.Second) // 保留窗口（2×60s）也过了
	d.Tick()
	if strings.Contains(d.Detail, "出口受限") {
		t.Errorf("保留窗口过后应消失，实际 %q", d.Detail)
	}
}

// 行为：60s 节流（与 v6 观察同级——每拍 TLS 握手既刷日志又高频撞 DPI 黑名单）。
func TestL2ProbeThrottled(t *testing.T) {
	clk := time.Unix(0, 0)
	probe := 0
	d := newL2Daemon(&clk, "ok", &probe)
	d.Tick()
	if probe != 1 {
		t.Fatalf("首拍应观察一次，实际 %d", probe)
	}
	clk = clk.Add(30 * time.Second)
	d.Tick()
	if probe != 1 {
		t.Errorf("60s 窗口内不得再观察，实际 %d 次", probe)
	}
	clk = clk.Add(31 * time.Second)
	d.Tick()
	if probe != 2 {
		t.Errorf("窗口过后应再观察，实际 %d 次", probe)
	}
}

// 行为：受限可见缝（托盘气泡/通知层用）——受限中或恢复后保留窗口内为真，
// 与 onlineDetail 的上浮判定同源。壳层据此发气泡，不得用 detail 文案反推。
func TestL2RestrictedOnUIFollowsWindowPredicate(t *testing.T) {
	clk := time.Unix(0, 0)
	probe := 0
	d := newL2Daemon(&clk, "restricted", &probe)
	d.Tick()
	if !d.L2RestrictedOnUI() {
		t.Fatal("受限中可见缝应为真")
	}
	// 换正常观察：保留窗口（2×60s）内仍为真（恢复后用户仍该看见）
	clk = clk.Add(61 * time.Second)
	d.ProbeL2 = func() string { probe++; return "ok" }
	d.Tick()
	if !d.L2RestrictedOnUI() {
		t.Fatal("恢复后保留窗口内仍应为真（与 detail 上浮同源）")
	}
	clk = clk.Add(130 * time.Second)
	d.Tick()
	if d.L2RestrictedOnUI() {
		t.Fatal("保留窗口过后应为假")
	}
	if d.State != StateOnline {
		t.Errorf("可见缝只是读数，不得改变在线判定，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：L2 只观察——受限绝不改变状态机决策（不触发登录、不判死）。
func TestL2RestrictedNeverChangesDecisions(t *testing.T) {
	clk := time.Unix(0, 0)
	probe := 0
	logins := 0
	d := newL2Daemon(&clk, "restricted", &probe)
	d.Login = func() (string, error) { logins++; return "", nil }
	d.Tick()
	d.Tick()
	if logins != 0 {
		t.Errorf("L2 受限不得触发登录，实际 %d 次", logins)
	}
	if d.State != StateOnline {
		t.Errorf("L2 受限时会话健康应保持在线，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为（2026-11-21 实测复盘）：L2 受限持续 ≥10 分钟时在线文案升级点名
// 「持续」——今晨出口受限连续 3.5 小时只显示「观察中」，用户无从判断该不
// 该动手。只改可见性，不改判定（对抗 DPI 不违规；会话真值由 probeSession
// 独立核实）。短时受限（抖动/风控窗口，2026-11-04 实测约 15 分钟自愈）不标。
func TestL2StreakShownAsPersistent(t *testing.T) {
	clk := time.Unix(0, 0)
	probe := 0
	d := newL2Daemon(&clk, "restricted", &probe)
	d.Tick() // 受限开始
	if !strings.Contains(d.Detail, "出口受限") {
		t.Fatalf("前置：受限应上浮 detail，实际 %q", d.Detail)
	}
	if strings.Contains(d.Detail, "持续") {
		t.Fatalf("前置：短时受限不得标持续，实际 %q", d.Detail)
	}
	clk = clk.Add(11 * time.Minute) // 过 L2 节流，受限持续
	d.Tick()
	if !strings.Contains(d.Detail, "持续") {
		t.Fatalf("受限 ≥10min 应点名持续（可见性升级），实际 %q", d.Detail)
	}
}
