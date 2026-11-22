package keeper

import (
	"strings"
	"testing"
	"time"
)

// 双读数（2026-11-18）：认证（物理栈）与用户路径（系统栈）分开报，不一致时
// 并列显示；reason 必须能说清「为什么不动作」。这些是面板/托盘/API 的唯一
// 词表来源，壳层不得自造。

func TestHealthUserPathBrokenWhileAuthOnline(t *testing.T) {
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return true, nil }
	d.UserPath = func() PathReading {
		return PathReading{OK: false, Known: true, Detail: "系统栈锚点超时"}
	}
	d.Tick()

	h := d.Health()
	if h.Auth != AuthOnline {
		t.Errorf("认证读数应为 online，实际 %q（%v/%v）", h.Auth, d.State, d.Detail)
	}
	if h.UserPath != PathBroken {
		t.Errorf("用户路径应为 broken，实际 %q", h.UserPath)
	}
	if h.UserDetail != "系统栈锚点超时" {
		t.Errorf("用户路径原因应保留原文，实际 %q", h.UserDetail)
	}
}

func TestHealthUserPathNilIsUnknown(t *testing.T) {
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return true, nil }
	d.Tick()

	if h := d.Health(); h.UserPath != PathUnknown {
		t.Errorf("未接线用户路径探针应报 unknown，实际 %q", h.UserPath)
	}
}

func TestHealthBindingInvalidReason(t *testing.T) {
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.EgressHealth = func() EgressHealth { return invalidEgress(EgressBindingInvalid) }
	d.Tick()

	h := d.Health()
	if h.Reason != "binding_invalid" {
		t.Errorf("原因应为 binding_invalid，实际 %q", h.Reason)
	}
	if h.Action != ActionReselecting {
		t.Errorf("动作应为 reselecting，实际 %q", h.Action)
	}
	if h.Auth != AuthOffline {
		t.Errorf("认证读数应为 offline（无法确认），实际 %q", h.Auth)
	}
}

func TestHealthPortalUnreachableReason(t *testing.T) {
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.EgressHealth = func() EgressHealth { return invalidEgress(EgressPortalUnreachable) }
	d.Tick()

	h := d.Health()
	if h.Reason != "portal_unreachable" || h.Action != ActionReselecting {
		t.Errorf("原因/动作应为 portal_unreachable/reselecting，实际 %q/%q", h.Reason, h.Action)
	}
	if h.Auth != AuthUnknown {
		t.Errorf("门户不可达时认证读数应为 unknown，实际 %q", h.Auth)
	}
}

// 行为：本地 30 秒闸不能再显示成「门户拒绝」——reason=local_throttle、
// 认证读数 unknown（没问过门户），不是 offline。
func TestHealthLocalThrottleIsNotPortalRejected(t *testing.T) {
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return false, nil }
	d.Login = func() (string, error) { return "", nil }

	d.Tick() // 会话真值说死 → 登录成功
	d.Tick() // 30s 闸内：本地节流
	h := d.Health()
	if h.Reason != "local_throttle" {
		t.Errorf("原因应为 local_throttle，实际 %q（%v/%v）", h.Reason, d.State, d.Detail)
	}
	if h.Auth != AuthUnknown {
		t.Errorf("本地节流未问过门户，认证读数应为 unknown，实际 %q", h.Auth)
	}
}

// 行为：L1 红但会话在线 → 认证 online，reason 标出锚点异常。
func TestHealthL1RedSessionAliveReason(t *testing.T) {
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return false }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return true, nil }
	d.Tick()

	h := d.Health()
	if h.Auth != AuthOnline {
		t.Errorf("会话在线时认证读数应为 online，实际 %q", h.Auth)
	}
	if h.Reason != "l1_red_session_alive" {
		t.Errorf("原因应为 l1_red_session_alive，实际 %q（detail=%q）", h.Reason, d.Detail)
	}
}

// 行为（2026-11-21 产品优化决议 B）：用户路径红（系统栈不通）持续 ≥5 分钟
// 且 L1 绿（在线态）时，在线 detail 附注互证提示——「守护说在线」与「浏览器
// 上不了网」并列说清，用户该找代理/TUN 而不是以为守护失职。只展示，
// 不影响任何判定；转绿即消。
func TestUserPathAbnormalShownAfterStreak(t *testing.T) {
	clk := time.Unix(0, 0)
	broken := true
	d := NewDaemon("")
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"用户名称": "u"}, nil }
	d.UserPath = func() PathReading {
		return PathReading{OK: !broken, Known: true, Detail: "系统栈锚点超时"}
	}

	d.Tick() // 在线 + 用户路径红：streak 起点
	if d.State != StateOnline {
		t.Fatalf("前置：应在线，实际 %v/%v", d.State, d.Detail)
	}
	if strings.Contains(d.Detail, "用户路径异常") {
		t.Fatalf("前置：红不满 5 分钟不得上浮，实际 %q", d.Detail)
	}

	clk = clk.Add(5*time.Minute + time.Second)
	d.Tick()
	if !strings.Contains(d.Detail, "用户路径异常") {
		t.Fatalf("红持续 ≥5 分钟应上浮互证附注，实际 %q", d.Detail)
	}
	if d.State != StateOnline {
		t.Fatalf("附注只是展示，不得改变在线判定，实际 %v/%v", d.State, d.Detail)
	}

	// 转绿即消（10s > 5s 用户路径节流）。
	broken = false
	clk = clk.Add(10 * time.Second)
	d.Tick()
	if strings.Contains(d.Detail, "用户路径异常") {
		t.Fatalf("用户路径转绿后附注应消失，实际 %q", d.Detail)
	}
}

// 行为（决议 B 后半句）：转未知（Known=false）即消且**不计时**——探针
// 失联期间红 streak 必须清零重启，否则「红 1 分钟 + 失联 10 分钟 + 红复来」
// 会被陈账推成上浮（用户路径异常误报）。
// 行为（trouble-maker P1-6 补覆盖，决议 B 红线）：L1 红时用户路径红不得带
// 「守护探测走物理栈不受影响」附注——锚点也红，那句话不成立；附注整体抑制。
func TestUserPathNoteSuppressedWhenL1Red(t *testing.T) {
	clk := time.Unix(0, 0)
	d := NewDaemon("")
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return false } // 锚点红
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return true, nil } // 会话活：走 L1 红的「认证正常」路径
	d.UserPath = func() PathReading {
		return PathReading{OK: false, Known: true, Detail: "系统栈锚点超时"}
	}

	d.Tick()
	clk = clk.Add(5*time.Minute + time.Second) // 用户路径红远超阈值
	d.Tick()
	if strings.Contains(d.Detail, "用户路径异常") || strings.Contains(d.Detail, "不受影响") {
		t.Fatalf("L1 红时用户路径附注必须整体抑制（锚点也红，「不受影响」不成立），实际 %q", d.Detail)
	}
	if d.State != StateOnline {
		t.Fatalf("前置：会话活+L1 红应报在线（锚点异常），实际 %v/%v", d.State, d.Detail)
	}
}

func TestUserPathUnknownResetsStreak(t *testing.T) {
	clk := time.Unix(0, 0)
	phase := 0 // 0=红 1=未知 2=红复来
	d := NewDaemon("")
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"用户名称": "u"}, nil }
	d.UserPath = func() PathReading {
		if phase == 1 {
			return PathReading{Known: false} // 探针失联/无法判定
		}
		return PathReading{OK: false, Known: true, Detail: "系统栈锚点超时"}
	}

	d.Tick() // 红 streak 起点
	clk = clk.Add(time.Minute)
	phase = 1
	d.Tick() // 转未知：streak 清零
	clk = clk.Add(10 * time.Minute)
	phase = 2
	d.Tick() // 红复来：streak 从这一拍重新起算
	clk = clk.Add(4 * time.Minute) // 复来后累计 4 分钟 < 5 分钟
	d.Tick()
	if strings.Contains(d.Detail, "用户路径异常") {
		t.Fatalf("未知期间不得计时：红复来仅 4 分钟不得上浮（失联 10 分钟是陈账不是证据），实际 %q", d.Detail)
	}
	clk = clk.Add(time.Minute + time.Second) // 复来后 ≥5 分钟
	d.Tick()
	if !strings.Contains(d.Detail, "用户路径异常") {
		t.Fatalf("红复来持续 ≥5 分钟应重新起算并上浮，实际 %q", d.Detail)
	}
}

// 行为：用户路径读数 5s 节流，不能每拍都打系统栈锚点。
func TestUserPathProbeThrottled(t *testing.T) {
	probes := 0
	clk := time.Unix(0, 0)
	d := NewDaemon("")
	d.Now = func() time.Time { return clk }
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.SessionAlive = func() (bool, error) { return true, nil }
	d.UserPath = func() PathReading {
		probes++
		return PathReading{OK: true, Known: true}
	}

	d.Tick()
	clk = clk.Add(time.Second)
	d.Tick()
	clk = clk.Add(time.Second)
	d.Tick()
	if probes != 1 {
		t.Fatalf("5s 内只应采一次用户路径，实际 %d 次", probes)
	}
	clk = clk.Add(4 * time.Second)
	d.Tick()
	if probes != 2 {
		t.Fatalf("过了 5s 应重新采一次，实际 %d 次", probes)
	}
}
