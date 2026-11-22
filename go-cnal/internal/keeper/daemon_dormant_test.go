package keeper

import (
	"strings"
	"testing"
	"time"
)

// 离开校园网休眠观察（2026-11-21 09:52 日志实证 + 当日产品决议 A）：
// 非校园网络（蜂窝热点）上物理源重选乱跳——172.29.153.208↔10.158.52.162
// 反复切换 + bind 失败，白烧探测与重选。StatePortalDown 持续 ≥10 分钟
// 进入休眠观察：重选回调停调、门户探针降频（每拍 → ≥60s 一探）。
// 语义红线：是「休眠观察」不是「判定离开」——校园网故障同形态。

// 行为：portal_down 持续 ≥10 分钟 → ① 状态行点名「休眠观察」② OnPortalDown
// 重选回调停调 ③ PortalAlive 探测降频（休眠 6 分钟只真探 ≤8 次而非 120 次）。
// portalAlive 转绿 → 状态恢复、回调恢复调用、节流清零（下一拍即真探）。
func TestPortalDownDormantAfterStreak(t *testing.T) {
	alive := false
	var probeCalls, onDown int
	clk := time.Unix(0, 0)
	d := NewDaemon("")
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return false } // 锚点红：热点上锚点也不可达
	d.PortalAlive = func() bool {
		probeCalls++
		return alive
	}
	d.OnPortalDown = func() { onDown++ }

	// 3s 一拍（守护默认节拍量级）：0..600s 共 201 拍，第 201 拍进入休眠。
	for range 201 {
		d.Tick()
		clk = clk.Add(3 * time.Second)
	}
	if !strings.Contains(d.Detail, "休眠观察") {
		t.Fatalf("portal_down 持续 ≥10 分钟应点名休眠观察，实际 %v/%v", d.State, d.Detail)
	}
	// 语义红线（trouble-maker P1-1）：断言实际文案里的词，锁住「不断言离开」。
	// 旧断言查「已离开」恒真（文案里根本没有这三个字），锁不住任何漂移。
	if strings.Contains(d.Detail, "离开校园网") {
		t.Fatalf("语义红线：休眠观察不得断言离开校园网（校园故障同形态），实际 %q", d.Detail)
	}
	if !strings.Contains(d.Detail, "门户持续不可达") {
		t.Fatalf("休眠文案首句必须是可实证的「门户持续不可达」，实际 %q", d.Detail)
	}
	// 进入休眠那一拍回调即停：休眠前每拍一调 = 200 次。
	if onDown != 200 {
		t.Fatalf("前置：休眠前应每拍回调重选一次（200），实际 %d", onDown)
	}
	probesAtDormant := probeCalls

	// 休眠期 6 分钟（120 拍）：回调冻结、探针只按 60s 一探（约 6 次）。
	for range 120 {
		d.Tick()
		clk = clk.Add(3 * time.Second)
	}
	if onDown != 200 {
		t.Fatalf("休眠期 OnPortalDown 回调应停调，实际 %d", onDown)
	}
	if got := probeCalls - probesAtDormant; got > 8 {
		t.Fatalf("休眠期探针应 ≥60s 一探（6 分钟约 6 次），实际多探 %d 次", got)
	}

	// 门户探针转绿：下一轮真探拍（≤60s 节流窗口内）发现 → 清休眠恢复节拍。
	alive = true
	for range 25 { // 最多 75s，覆盖一个 60s 探针窗口
		d.Tick()
		clk = clk.Add(3 * time.Second)
		if d.State != StatePortalDown {
			break
		}
	}
	if d.State == StatePortalDown || strings.Contains(d.Detail, "休眠观察") {
		t.Fatalf("门户转绿应清休眠恢复正常状态，实际 %v/%q", d.State, d.Detail)
	}
	// 节流清零：恢复后 3s 内的下一拍即真探（不沿用休眠期的 60s 节流）。
	before := probeCalls
	clk = clk.Add(3 * time.Second)
	d.Tick()
	if probeCalls != before+1 {
		t.Fatalf("恢复后探针节流应清零（每拍真探），实际探针增量 %d", probeCalls-before)
	}
	// 回调恢复调用：门户再次不可达时重选时机照常报告（streak 从零重新起算）。
	alive = false
	clk = clk.Add(3 * time.Second)
	d.Tick()
	if onDown != 201 {
		t.Fatalf("恢复后 portal_down 应重新回调重选时机，实际 %d", onDown)
	}
	if strings.Contains(d.Detail, "休眠观察") {
		t.Fatalf("新 streak 未满 10 分钟不得显示休眠观察，实际 %q", d.Detail)
	}
}

// 行为：边界值——portal_down 恰好持续 10 分钟减 1 秒不得进入休眠
//（trouble-maker P2-7：边界负断言，防「常量改了文案与判定漂移」）。
func TestPortalDownDormantBoundaryMinusOneSecond(t *testing.T) {
	clk := time.Unix(0, 0)
	d := NewDaemon("")
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return false }
	d.OnPortalDown = func() {}

	for passed := time.Duration(0); passed < portalDownDormantAfter-time.Second; passed += 3 * time.Second {
		d.Tick()
		clk = clk.Add(3 * time.Second)
	}
	if strings.Contains(d.Detail, "休眠观察") {
		t.Fatalf("持续不足 10 分钟不得休眠，实际 %v/%q", d.State, d.Detail)
	}
	d.Tick() // 3 秒后跨过门槛
	if !strings.Contains(d.Detail, "休眠观察") {
		t.Fatalf("跨过 10 分钟门槛后应休眠，实际 %v/%q", d.State, d.Detail)
	}
}
