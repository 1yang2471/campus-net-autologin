package keeper

import (
	"errors"
	"testing"
	"time"
)

// 2026-11-11 真机实证：门户的 userflux 是**账号级**读数，会话被释放后仍回
// 「帐号状态：正常 / 日流量额 / 当天流量」。所以 flux 活 ≠ 会话活。
//
// 实测现场（09:5x，FlClash 开着、绑网线源）：
//   注销 v4 → {'success':true,'msg':'注销成功'} → online_check: {"online":0}
//   但 userflux 仍回「用户名称…日流量额 4G…」→ 守护判「在线」→ **不会自动重登**
//   直到手动 POST /login 才回来（{"ok":true}，门户随后 online:1）。
//
// 结论：会话存活必须用**会话级**信号 online_check，而不是 flux 的账号级读数。

// 行为：flux 有数据（账号级读数）但 online_check 说不在线 → **第一拍就判死**。
//
// 会话级探针给的 online:0 是门户对「这个源地址现在挂在哪」的权威回答，不是
// flux 那种弱信号，因此不该套用「连续两拍才判死」的去抖（那是为 flux 抖动
// 设计的）。2026-11-11 实测的代价：注销后守护先报「在线」60 秒（= 一拍会话
// 探针 + 60s 节流）才动手，用户看到的就是「不自动重连」。
func TestSessionProbeTrustsOnlineCheckOverFlux(t *testing.T) {
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	// 账号级读数一直有数据（真机现场就是这样）
	d.FetchFlux = func() (map[string]string, error) {
		return map[string]string{"用户名称": "u", "当天流量": "603.281M"}, nil
	}
	// 会话级读数：已释放
	d.SessionAlive = func() (bool, error) { return false, nil }

	d.Tick() // 第一拍就应判死（不再等第二拍/60s）
	if !d.SessionDead {
		t.Fatal("online_check 说不在线应第一拍判死——flux 有数据也不能算活")
	}
	if d.State == StateOnline {
		t.Errorf("判死后不得显示在线，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：online_check 说在线 → 清判死（即便 flux 空表也不该判死）。
func TestSessionProbeAliveClearsDeadRegardlessOfFlux(t *testing.T) {
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{}, nil }
	d.SessionAlive = func() (bool, error) { return true, nil }
	d.setSessionDead(true) // 前置：已判死

	d.Tick()
	if d.SessionDead {
		t.Error("online_check 说在线必须清判死")
	}
	if d.State != StateOnline {
		t.Errorf("应回到在线，实际 %v/%v", d.State, d.Detail)
	}
}

// 行为：online_check 查询出错 = 未知，不推进判死（与 flux 错误同语义：
// 门户不可达属离线分支职权，不归判死管）。
func TestSessionProbeErrorNotCounted(t *testing.T) {
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return false, errors.New("门户不可达") }

	d.Tick()
	clk = clk.Add(61 * time.Second)
	d.Tick()
	if d.SessionDead {
		t.Error("查询出错=未知，不得判死")
	}
}

// 行为：未接 SessionAlive 时回落到 flux（异校/旧接线不因为新增缝而失效），
// 且**仍然保留两拍去抖**——flux 空表是弱信号，单拍可能是抖动。
func TestSessionProbeFallsBackToFluxWhenUnwired(t *testing.T) {
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"用户名称": "u"}, nil }

	d.Tick()
	if d.SessionDead {
		t.Error("flux 活 + 未接会话探针 → 不得判死")
	}
}

// 行为：flux 回落路径仍要两拍才判死（弱信号去抖不能被这次改动误删）。
func TestFluxFallbackStillDebounces(t *testing.T) {
	d := NewDaemon("")
	clk := time.Unix(0, 0)
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{}, nil }

	d.Tick()
	if d.SessionDead {
		t.Fatal("flux 空表第一拍不得判死（弱信号要防抖）")
	}
	clk = clk.Add(61 * time.Second)
	d.Tick()
	if !d.SessionDead {
		t.Error("flux 连续两拍空必须判死")
	}
}
