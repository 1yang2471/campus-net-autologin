package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 守护配置面增量：auto_reconnect（默认开）与 bind_ip（物理源逃生口）。
// ini 键名与 Python 守护 DEFAULTS 逐字对齐。

func writeTmpIni(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cnal.ini")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// 行为：键缺省 → auto_reconnect 默认 true（Python DEFAULTS 同值），bind_ip 空。
func TestConfigAutoReconnectDefaultTrue(t *testing.T) {
	c, err := loadDaemonConfig(writeTmpIni(t, "[keeper]\nserver = https://x\ncampus_dns = 10.0.0.1\nlogin_path = /a\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !c.AutoReconnect {
		t.Error("auto_reconnect 缺省必须为 true")
	}
	if c.BindIP != "" {
		t.Errorf("bind_ip 缺省应为空，实际 %q", c.BindIP)
	}
}

// 行为：net_mode 缺省 auto（双路径自适应）；显式 bound/system 如实读取；
// 非法值在启动时报错——静默回落会让用户以为手动强制生效了。
func TestConfigNetMode(t *testing.T) {
	base := "[keeper]\nserver = https://x\nlogin_path = /a\n"
	c, err := loadDaemonConfig(writeTmpIni(t, base))
	if err != nil {
		t.Fatal(err)
	}
	if c.NetMode != NetModeAuto {
		t.Errorf("net_mode 缺省应为 %q，实际 %q", NetModeAuto, c.NetMode)
	}
	for _, mode := range []string{NetModeBound, NetModeSystem} {
		c, err := loadDaemonConfig(writeTmpIni(t, base+"net_mode = "+mode+"\n"))
		if err != nil {
			t.Fatalf("net_mode=%s 应可解析: %v", mode, err)
		}
		if c.NetMode != mode {
			t.Errorf("net_mode 应读到 %q，实际 %q", mode, c.NetMode)
		}
	}
	if _, err := loadDaemonConfig(writeTmpIni(t, base+"net_mode = sideways\n")); err == nil {
		t.Error("非法 net_mode 必须报错（不得静默回退 auto）")
	}
}
func TestConfigAutoReconnectOffAndBindIP(t *testing.T) {
	c, err := loadDaemonConfig(writeTmpIni(t, "[keeper]\nserver = https://x\nlogin_path = /a\nauto_reconnect = false\nbind_ip = 172.29.1.5\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.AutoReconnect {
		t.Error("auto_reconnect=false 必须读到关")
	}
	if c.BindIP != "172.29.1.5" {
		t.Errorf("bind_ip 应为 172.29.1.5，实际 %q", c.BindIP)
	}
	c2, err := loadDaemonConfig(writeTmpIni(t, "[keeper]\nserver = https://x\nlogin_path = /a\nauto_reconnect = off\n"))
	if err != nil || c2.AutoReconnect {
		t.Errorf("off 变体也应判关: %v %v", c2.AutoReconnect, err)
	}
}

// 行为：interval 低于 3s 一律按 3s 跑（2026-11-13 登录风暴脚枪：interval=1
// 配 probe_timeout=1 每秒一拍、单拍超时即判离线）。钳制是运行时下限——
// ini 原值保留不改写；缺省也落进同一条规则（<3s → 3s）。
func TestIntervalFloorAt3s(t *testing.T) {
	cases := []struct {
		ini  string
		want time.Duration
	}{
		{"[keeper]\nserver = https://x\nlogin_path = /a\n", 3 * time.Second},
		{"[keeper]\nserver = https://x\nlogin_path = /a\ninterval = 1\n", 3 * time.Second},
		{"[keeper]\nserver = https://x\nlogin_path = /a\ninterval = 2.5\n", 3 * time.Second},
		{"[keeper]\nserver = https://x\nlogin_path = /a\ninterval = 5\n", 5 * time.Second},
	}
	for i, c := range cases {
		cfg, err := loadDaemonConfig(writeTmpIni(t, c.ini))
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if cfg.Interval != c.want {
			t.Errorf("case %d: interval 应为 %v，实际 %v", i, c.want, cfg.Interval)
		}
	}
}
