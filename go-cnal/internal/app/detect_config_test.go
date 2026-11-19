package app

import (
	"os"
	"path/filepath"
	"testing"
)

// 探测配置读取：缺文件回落默认（首跑配置可能还不存在，探测不能因此不可用）。
func TestDetectOptionsFromConfigMissingFileUsesDefaults(t *testing.T) {
	opt := DetectOptionsFromConfig(filepath.Join(t.TempDir(), "none.ini"), "10.1.2.3")
	if opt.BindIP != "10.1.2.3" {
		t.Fatalf("显式 bindIP 应优先: %q", opt.BindIP)
	}
	if opt.CampusDNS != "" || opt.ProbePath != "" || len(opt.LoginPageMarks) != 0 {
		t.Fatalf("缺文件时应全部回落零值（由 DetectPortal 补默认）: %+v", opt)
	}
}

// 探测配置读取：只取探测相关键，注释行与 [response] 段不得混入。
func TestDetectOptionsFromConfigReadsDetectKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.ini")
	body := "; 注释行应忽略\n" +
		"[keeper]\n" +
		"server = https://a.example.edu.cn\n" +
		"campus_dns = 10.129.17.10\n" +
		"probe_ips = 223.5.5.5, 119.29.29.29\n" +
		"probe_path = /generate_204\n" +
		"probe_host = connect.rom.miui.com\n" +
		"login_page = /ac_portal/default/pc.html?tabs=pwd\n" +
		"login_page_marks = 上网认证系统, password_name\n" +
		"bind_ip = 10.1.1.1\n" +
		"[response]\n" +
		"success_marks = 认证成功\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	opt := DetectOptionsFromConfig(p, "")
	if opt.CampusDNS != "10.129.17.10" {
		t.Errorf("campus_dns = %q", opt.CampusDNS)
	}
	if len(opt.ProbeIPs) != 2 || opt.ProbeIPs[0] != "223.5.5.5" || opt.ProbeIPs[1] != "119.29.29.29" {
		t.Errorf("probe_ips = %v", opt.ProbeIPs)
	}
	if opt.ProbePath != "/generate_204" || opt.ProbeHost != "connect.rom.miui.com" {
		t.Errorf("probe_path/host = %q/%q", opt.ProbePath, opt.ProbeHost)
	}
	if opt.LoginPage != "/ac_portal/default/pc.html?tabs=pwd" {
		t.Errorf("login_page = %q", opt.LoginPage)
	}
	if len(opt.LoginPageMarks) != 2 || opt.LoginPageMarks[0] != "上网认证系统" {
		t.Errorf("login_page_marks = %v", opt.LoginPageMarks)
	}
	if opt.BindIP != "10.1.1.1" {
		t.Errorf("ini bind_ip 应被读取: %q", opt.BindIP)
	}
	// 显式 bindIP（安卓注入的已绑定源）必须覆盖 ini 里的旧值。
	if got := DetectOptionsFromConfig(p, "10.9.9.9").BindIP; got != "10.9.9.9" {
		t.Errorf("显式 bindIP 未覆盖 ini: %q", got)
	}
}
