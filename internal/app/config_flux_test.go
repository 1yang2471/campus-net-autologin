package app

import (
	"testing"
	"time"
)

// 切片5（配置面）：流量轮询间隔与提醒阈值 ini 可配，缺省走内核默认。
// 异校门户限频口径不同，不能把 10 分钟焊死。

func TestConfigParsesFluxPollInterval(t *testing.T) {
	c, err := loadDaemonConfig(writeTmpIni(t,
		"[keeper]\nserver = https://x\nlogin_path = /a\nflux_poll_interval_sec = 300\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.FluxPollInterval != 5*time.Minute {
		t.Errorf("flux_poll_interval_sec = %v, 期望 5m", c.FluxPollInterval)
	}
	// 未配置时为零值（newDaemon 用 <=0 回落默认），不得被塞成别的值。
	c2, err := loadDaemonConfig(writeTmpIni(t, "[keeper]\nserver = https://x\nlogin_path = /a\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c2.FluxPollInterval != 0 {
		t.Errorf("未配置时应为零值，实际 %v", c2.FluxPollInterval)
	}
}

func TestConfigParsesFluxWarnThresholds(t *testing.T) {
	c, err := loadDaemonConfig(writeTmpIni(t,
		"[keeper]\nserver = https://x\nlogin_path = /a\nflux_warn_at = 60,85\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.FluxWarnAt != 60 || c.FluxCriticalAt != 85 {
		t.Errorf("flux_warn_at = %v/%v, 期望 60/85", c.FluxWarnAt, c.FluxCriticalAt)
	}
}

// 行为：非法值不得让守护用坏阈值启动（回落默认），也不得报错拦住启动。
func TestConfigIgnoresBadFluxWarnThresholds(t *testing.T) {
	for _, v := range []string{"abc", "90,50", "0,0", "120,130"} {
		c, err := loadDaemonConfig(writeTmpIni(t,
			"[keeper]\nserver = https://x\nlogin_path = /a\nflux_warn_at = "+v+"\n"))
		if err != nil {
			t.Fatalf("非法 %q 不该拦启动: %v", v, err)
		}
		if c.FluxWarnAt != 0 || c.FluxCriticalAt != 0 {
			t.Errorf("非法 %q 应回落零值，实际 %v/%v", v, c.FluxWarnAt, c.FluxCriticalAt)
		}
	}
}
