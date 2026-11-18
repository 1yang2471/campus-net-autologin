package keeper

import (
	"errors"
	"testing"
	"time"
)

// G5：免会话态自动检测（移植 Python t46 三语义，零配置、自适应窗口边缘）。
// 锚点绿 + 连续 2 次登录不受理 = 门户不维护会话的直接证据 → 学得免会话态：
// 停自动登录、显示中性 free_window。L1 变红 = 网络开始需要认证 → 放行重连；
// 登录恢复受理 → 退出检测态。正常踢线复活永不误判。

// 行为：锚点绿 + 会话死 + 连续 2 次登录不受理 → 进入免会话态（恰 2 次登录，
// 之后不再登录）。对应 t46 测试 1（NOAUTH 夜间实测形态）。
func TestFreeModeEntersAfterTwoUnservicedLogins(t *testing.T) {
	logins := 0
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{}, nil }
	d.Login = func() (string, error) {
		logins++
		return "", errors.New("Task 'NOAUTH:*' not taken")
	}
	// 拍1 防抖不判死；拍2（越过 60s 会话探针节流）判死→登录#1（30s 退避）；
	// 拍3 退避过后登录#2→入态
	d.Tick()
	clk = clk.Add(61 * time.Second)
	d.Tick()
	clk = clk.Add(31 * time.Second)
	d.Tick()
	if logins != 2 {
		t.Fatalf("应恰好试 2 次登录，实际 %d 次", logins)
	}
	if !d.FreeMode {
		t.Fatal("连续 2 次不受理应进入免会话态")
	}
	if d.State != StateFreeWindow {
		t.Errorf("应显示中性免认证态，实际 %v/%v", d.State, d.Detail)
	}
	for range 5 {
		d.Tick()
	}
	if logins != 2 {
		t.Errorf("免会话态内不得再自动登录，实际已 %d 次", logins)
	}
}

// 行为：免会话态下 L1 绿不放行登录；L1 变红放行重连，成功即退出检测态、
// 清零计数、回在线。对应 t46 测试 2。
func TestFreeModeExitsWhenL1RedAndLoginRevives(t *testing.T) {
	logins := 0
	d := NewDaemon("")
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"用户名称": "u"}, nil }
	d.FreeMode = true
	d.freeStreak = 2
	d.Login = func() (string, error) {
		logins++
		return "", nil
	}
	// 阶段1：L1 仍绿 + 会话活 → 直接在线（省预算，不登录）
	d.ProbeOnline = func() bool { return true }
	d.Tick()
	if logins != 0 {
		t.Fatal("免会话态 + L1 绿：不得登录")
	}
		// 阶段2：L1 变红（免认证结束）→ 重连放行（连续 3 拍去抖），成功退出检测态
		d.ProbeOnline = func() bool { return false }
		d.Tick() // 去抖拍1
		d.Tick() // 去抖拍2
		d.Tick() // 第 3 拍触发登录
		if logins != 1 {
			t.Fatalf("L1 变红应放行重连尝试，实际 %d 次", logins)
		}
	if d.FreeMode {
		t.Error("登录恢复受理应退出免会话态")
	}
	if d.freeStreak != 0 {
		t.Error("复活成功必须清零不受理计数")
	}
	if d.State != StateOnline {
		t.Errorf("应回在线，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：正常踢线（登录复活 flux）永不误入免会话态——登录始终成功，
// 不受理计数保持 0。对应 t46 测试 3。
func TestFreeModeNormalKickRevivalNeverMisjudges(t *testing.T) {
	logins := 0
	alive := false
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) {
		if alive {
			return map[string]string{"用户名称": "u"}, nil
		}
		return map[string]string{}, nil
	}
	d.Login = func() (string, error) {
		logins++
		alive = true // 踢线→登录→flux 复活
		return "", nil
	}
	d.Tick()                        // 空表×1 防抖
	clk = clk.Add(61 * time.Second) // 越过节流 → 空表×2 判死→重登→复活
	d.Tick()
	d.Tick()
	if logins != 1 {
		t.Fatalf("正常踢线应恰 1 次重登，实际 %d 次", logins)
	}
	if d.State != StateOnline {
		t.Errorf("复活后应在线，实际 %v/%v", d.State, d.Detail)
	}
	if d.FreeMode || d.freeStreak != 0 {
		t.Errorf("正常踢线复活不得累积不受理计数，实际 mode=%v streak=%d",
			d.FreeMode, d.freeStreak)
	}
}
