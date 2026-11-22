package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// multi_channel 配置面（2026-11-20 双通道 M2）：默认**关**（沿用 11-19
// 「旁路只观察不登录」的保守纪律），面板可开可关、落 ini 即生效。

// 行为：ini 缺该键时默认关——不确定时不做通道级登录。
func TestMultiChannelDefaultsOff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.ini")
	if err := os.WriteFile(path, []byte("[keeper]\nserver=https://portal.example\nlogin_path=/ac_portal/login.php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadDaemonConfig(path)
	if err != nil {
		t.Fatalf("读配置失败: %v", err)
	}
	if cfg.MultiChannel {
		t.Error("multi_channel 缺省必须是关（保守方向）")
	}
}

// 行为：ini 写 true/false 时读出对应值。
func TestMultiChannelReadsIni(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want bool
	}{
		{"true", true}, {"1", true}, {"on", true},
		{"false", false}, {"0", false}, {"", false},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.ini")
		body := "[keeper]\nserver=https://portal.example\nlogin_path=/ac_portal/login.php\n"
		if tc.val != "" {
			body += "multi_channel = " + tc.val + "\n"
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadDaemonConfig(path)
		if err != nil {
			t.Fatalf("读配置失败: %v", err)
		}
		if cfg.MultiChannel != tc.want {
			t.Errorf("multi_channel=%q 应读出 %v，实际 %v", tc.val, tc.want, cfg.MultiChannel)
		}
	}
}

// 行为：面板保存 multi_channel 能落进 ini（白名单含该键，否则面板显示
// 保存成功、ini 却没变化）。
func TestMultiChannelSavesToIni(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cnal.ini")
	orig := "[keeper]\r\nserver = https://portal.example\r\n"
	if err := os.WriteFile(p, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPathCached = p
	if err := updateIniFields(map[string]string{"multi_channel": "true"}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "multi_channel = true") {
		t.Errorf("multi_channel 应被写入 ini:\n%s", string(b))
	}
}
