package keeper

import (
	"strings"
	"testing"
	"time"
)

// ============ 2026-11-22 复盘修复项 2：AC 假在线组合判定 ============
// 三读数（会话绿+锚点红+用户路径红≥10 分钟）并立时：
//   - Reason=fake_online → Label 覆盖「会话在线｜出口被控」（health.go 结构化
//     判定，不再 Contains(detail,"锚点异常") 文案反推）；
//   - detail 升级为细节 + 行动指引（重连救不了）。
// 场景对应 11-22 事故：AC 拉黑 4.5h 期间托盘一直「在线」，用户体感没网。

func newFakeOnlineDaemon(clk *time.Time, userPathOK bool) *Daemon {
	d := NewDaemon("")
	d.Now = func() time.Time { return *clk }
	d.ProbeOnline = func() bool { return false }               // 锚点红
	d.PortalAlive = func() bool { return true }                // 门户受理
	d.SessionAlive = func() (bool, error) { return true, nil } // 会话绿
	d.UserPath = func() PathReading {
		return PathReading{OK: userPathOK, Known: true, Detail: "系统栈锚点超时"}
	}
	return d
}

// 行为：三读数并立 ≥10min → fake_online reason + 组合 detail（细节+指引）。
func TestFakeOnlineFullCombination(t *testing.T) {
	clk := time.Unix(0, 0)
	d := newFakeOnlineDaemon(&clk, false)
	d.Tick() // L1 红在线态首拍（userPath 红 streak 起点）
	if d.State != StateOnline {
		t.Fatalf("前置：应在线，实际 %v/%v", d.State, d.Detail)
	}
	if strings.Contains(d.Detail, "重连救不了") {
		t.Fatalf("首拍 <10min 应是短文案，实际 %q", d.Detail)
	}
	clk = clk.Add(11 * time.Minute)
	d.Tick()
	h := d.Health()
	if h.Reason != "fake_online" {
		t.Errorf("三读数并立≥10min 应报 fake_online，实际 %q（detail=%q）", h.Reason, d.Detail)
	}
	if !strings.Contains(d.Detail, "重连救不了") {
		t.Errorf("组合态 detail 应含行动指引，实际 %q", d.Detail)
	}
}

// 行为：userPath 红 <10min → 保持 l1_red_session_alive（锚点红+会话绿）。
func TestFakeOnlineNeedsUserPathStreak(t *testing.T) {
	clk := time.Unix(0, 0)
	d := newFakeOnlineDaemon(&clk, false)
	d.Tick()
	clk = clk.Add(3 * time.Minute)
	d.Tick()
	h := d.Health()
	if h.Reason != "l1_red_session_alive" {
		t.Errorf("红<10min 应保持 l1_red_session_alive，实际 %q", h.Reason)
	}
}

// 行为：userPath 转绿 → 组合态解除，回 l1_red_session_alive。
func TestFakeOnlineClearsWhenUserPathGreen(t *testing.T) {
	clk := time.Unix(0, 0)
	d := newFakeOnlineDaemon(&clk, false)
	d.Tick()
	clk = clk.Add(11 * time.Minute)
	d.Tick()
	if h := d.Health(); h.Reason != "fake_online" {
		t.Fatalf("前置：应 fake_online，实际 %q", h.Reason)
	}
	d.UserPath = func() PathReading { return PathReading{OK: true, Known: true} }
	clk = clk.Add(2 * time.Minute)
	d.Tick()
	if h := d.Health(); h.Reason != "l1_red_session_alive" {
		t.Errorf("userPath 转绿应解除组合态，实际 %q", h.Reason)
	}
}

// 行为：会话判死时不得报 fake_online——「会话绿」是组合判据。
func TestFakeOnlineNeedsSessionAlive(t *testing.T) {
	clk := time.Unix(0, 0)
	d := newFakeOnlineDaemon(&clk, false)
	d.Tick()
	clk = clk.Add(11 * time.Minute)
	d.Tick()
	if h := d.Health(); h.Reason != "fake_online" {
		t.Fatalf("前置：应 fake_online，实际 %q", h.Reason)
	}
	d.mu.Lock()
	d.SessionDead = true
	d.mu.Unlock()
	if h := d.Health(); h.Reason == "fake_online" {
		t.Error("会话判死不得报 fake_online（会话绿是组合判据）")
	}
}
