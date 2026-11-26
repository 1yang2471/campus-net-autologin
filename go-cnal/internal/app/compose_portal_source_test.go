package app

import (
	"errors"
	"net"
	"testing"
	"time"

	"cnal/internal/netstack"
)

// 自动发现路径的物理源选择（2026-11-11 设计 §3）：
// 真机实测——校园认证网关 1.1.1.2:443 **只从 WLAN 源可达**，以太网源超时；
// 而路由表按 metric 偏向以太网。选源必须以门户可达性为准，否则插了网线就登不上。

// 行为：路由偏好源够不着门户时，必须换到够得着的那个候选源。
func TestPlumbingPicksSourceThatCanReachPortal(t *testing.T) {
	cands := netstack.CandidateSources("")
	if len(cands) < 2 {
		t.Skip("本机候选源不足 2 个（无网环境），跳过")
	}
	old := portalProbeWithBindMode
	t.Cleanup(func() { portalProbeWithBindMode = old })
	var probed []string
	portalProbeWithBindMode = func(src net.IP, _ string, _ string, _ string, _ time.Duration, _ bool, _ netstack.BindMode) error {
		probed = append(probed, src.String())
		if len(probed) == 1 {
			return errors.New("连门户 1.1.1.2:443 失败: i/o timeout")
		}
		return nil
	}

	cfg := defaultAdvancedConfig()
	cfg.Server = "https://portal.example"
	cfg.BindIP = ""
	cfg.CampusDNS = ""
	pl, err := newPlumbing(cfg, "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(probed) < 2 {
		t.Fatalf("应逐个探测候选源，实际探测 %d 个", len(probed))
	}
	if !pl.currentV4().Equal(cands[1]) {
		t.Errorf("应选中第二个（可达的）候选 %s，实际 %s", cands[1], pl.currentV4())
	}
}

// 行为：生产装配必须把严格接口模式接到 plumbing，非 Windows 保持旧语义。
func TestPlumbingWiresStrictInterfaceMode(t *testing.T) {
	old := portalProbeWithBindMode
	t.Cleanup(func() { portalProbeWithBindMode = old })
	portalProbeWithBindMode = func(net.IP, string, string, string, time.Duration, bool, netstack.BindMode) error { return nil }
	cfg := defaultAdvancedConfig()
	cfg.Server = "https://portal.example"
	cfg.BindIP = ""
	cfg.CampusDNS = ""
	cfg.StrictInterfaceBind = true
	pl, err := newPlumbing(cfg, "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	wantStrict := netstack.DefaultBindMode() == netstack.BindModeInterface
	if pl.strictInterfaceBind != wantStrict {
		t.Fatalf("生产严格模式接线错误，实际 %v 期望 %v", pl.strictInterfaceBind, wantStrict)
	}
	if pl.bindMode != netstack.ModeForStrict(true) {
		t.Fatalf("生产绑定模式错误，实际 %v", pl.bindMode)
	}
}

// 行为：所有候选都够不着门户时，守护仍要起来（把「门户不可达」如实报进状态），
// 而不是拒绝启动——用户需要看到原因，不是一个没头没脑的启动失败。
func TestPlumbingStartsWhenNoCandidateReachesPortal(t *testing.T) {
	old := portalProbeWithBindMode
	t.Cleanup(func() { portalProbeWithBindMode = old })
	portalProbeWithBindMode = func(net.IP, string, string, string, time.Duration, bool, netstack.BindMode) error {
		return errors.New("i/o timeout")
	}
	cfg := defaultAdvancedConfig()
	cfg.Server = "https://portal.example"
	cfg.BindIP = ""
	cfg.CampusDNS = ""
	pl, err := newPlumbing(cfg, "u", "p")
	if err != nil {
		t.Fatalf("门户不可达不应阻止启动: %v", err)
	}
	if pl.currentV4() == nil {
		t.Error("应有兜底物理源（路由偏好源），实际为空")
	}
}
