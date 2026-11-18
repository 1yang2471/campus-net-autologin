package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 面板高级设置区：ini 已有但面板不可编辑的字段要能读写。测试先描述行为：
// - GET /config 视图暴露全部高级键（含 [response] 四组词表）
// - 写入时 [response] 词表落到 [response] 段，数值键进 [keeper] 段
// - 非法数值/未授权键在写入前被拒（半写不得发生）

func TestAdvancedConfigView(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	ini := `[keeper]
server = https://x
login_path = /a
campus_dns = 10.0.0.1
interval = 5
probe_timeout = 4
login_timeout = 12
probe_ips = 1.1.1.1,8.8.8.8
login_page = /p
user_agent = UA/1
ssl_verify = false
portal_down_wait = 60
reject_backoff = 300
transient_backoff = 45

[response]
success_marks = OK
negative_marks = denied
transient_marks = busy
freeze_marks = 锁定
`
	if err := os.WriteFile(p, []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	v := advancedConfigView(p)
	want := map[string]string{
		"campus_dns":        "10.0.0.1",
		"interval":          "5",
		"probe_timeout":     "4",
		"login_timeout":     "12",
		"probe_ips":         "1.1.1.1,8.8.8.8",
		"login_page":        "/p",
		"user_agent":        "UA/1",
		"ssl_verify":        "false",
		"portal_down_wait":  "60",
		"reject_backoff":    "300",
		"transient_backoff": "45",
		"success_marks":     "OK",
		"negative_marks":    "denied",
		"transient_marks":   "busy",
		"freeze_marks":      "锁定",
	}
	for k, exp := range want {
		if got := v[k]; got != exp {
			t.Errorf("视图 %s 应 %q，实际 %q", k, exp, got)
		}
	}
}

func TestValidateAdvancedConfigRejectsBadNumbers(t *testing.T) {
	cases := []struct{ key, val string }{
		{"interval", "abc"},
		{"interval", "0"},
		{"interval", "1"}, // 2026-11-13 登录风暴脚枪：面板保存路径同拒（与加载钳制同源）
		{"probe_timeout", "-1"},
		{"login_timeout", "9999"},
		{"portal_down_wait", "0"},
		{"reject_backoff", "10"},
		{"transient_backoff", "9999"},
		{"ssl_verify", "maybe"},
	}
	for _, tc := range cases {
		if err := validateAdvancedConfig(map[string]string{tc.key: tc.val}); err == nil {
			t.Errorf("%s=%q 应被拒绝", tc.key, tc.val)
		}
	}
	if err := validateAdvancedConfig(map[string]string{"interval": "3", "ssl_verify": "false"}); err != nil {
		t.Errorf("合法值不应拒绝: %v", err)
	}
}

func TestValidateAdvancedConfigRejectsUnknownKey(t *testing.T) {
	if err := validateAdvancedConfig(map[string]string{"evil_key": "boom"}); err == nil {
		t.Error("白名单外的高级键必须拒绝")
	}
}

func TestWriteAdvancedConfigSeparatesResponseSection(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	if err := os.WriteFile(p, []byte("[keeper]\nserver = https://x\nlogin_path = /a\n\n[accounts]\n1 = a|https://x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPathCached = p
	if err := writeAdvancedConfig(map[string]string{
		"interval":        "5",
		"success_marks":   "OK, 搞定",
		"freeze_marks":    "锁定",
		"transient_marks": "busy",
	}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	if !strings.Contains(s, "interval = 5") {
		t.Errorf("[keeper] 段应有 interval:\n%s", s)
	}
	if !strings.Contains(s, "[response]") {
		t.Fatalf("应生成 [response] 段:\n%s", s)
	}
	resp := s[strings.Index(s, "[response]"):]
	for _, want := range []string{"success_marks = OK, 搞定", "freeze_marks = 锁定", "transient_marks = busy"} {
		if !strings.Contains(resp, want) {
			t.Errorf("[response] 段缺 %q:\n%s", want, s)
		}
	}
	keeperPart := s[:strings.Index(s, "[response]")]
	if strings.Contains(keeperPart, "success_marks") || strings.Contains(keeperPart, "freeze_marks") {
		t.Errorf("词表不得混进 [keeper] 段:\n%s", s)
	}
	if !strings.Contains(s, "[accounts]") {
		t.Errorf("其他节应保留:\n%s", s)
	}
}

// 行为：[response] 段已存在时只补缺键，已有键原样改写，不重复建段、
// 不丢其它节（Python 配置迁移后最常见的形态）。
func TestWriteAdvancedConfigUpdatesExistingResponseSection(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	ini := "[keeper]\nserver = https://x\nlogin_path = /a\n\n[response]\nsuccess_marks = old\n\n[accounts]\n1 = a|https://x\n"
	if err := os.WriteFile(p, []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPathCached = p
	if err := writeAdvancedConfig(map[string]string{
		"success_marks": "OK",
		"freeze_marks":  "锁定",
	}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	if strings.Count(s, "[response]") != 1 {
		t.Errorf("不得重复建 [response] 段:\n%s", s)
	}
	resp := s[strings.Index(s, "[response]"):]
	if !strings.Contains(resp, "success_marks = OK") || !strings.Contains(resp, "freeze_marks = 锁定") {
		t.Errorf("已有键应改写、缺键应补在同一段:\n%s", s)
	}
	if !strings.Contains(s, "[accounts]") {
		t.Errorf("其它节应保留:\n%s", s)
	}
	// 改写后重读：词表必须能被守护解析到（不是只有文本长得对）。
	cfg, err := loadDaemonConfig(p)
	if err != nil {
		t.Fatalf("改写后配置应可解析: %v", err)
	}
	if len(cfg.Response.Success) != 1 || cfg.Response.Success[0] != "OK" {
		t.Errorf("新 success_marks 应生效，实际 %v", cfg.Response.Success)
	}
}

// 组合根与首跑面板共用的保存分派：常用键走 updateIniFields，高级键走
// writeAdvancedConfig；任何非法值在写入前被拒（半写不得发生）。
func TestSaveConfigValuesRejectsInvalidBeforeWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	orig := "[keeper]\nserver = https://x\nlogin_path = /a\n"
	if err := os.WriteFile(p, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPathCached = p
	if err := saveConfigValues(map[string]string{"server": "https://y", "interval": "abc"}); err == nil {
		t.Error("非法 interval 必须拒绝")
	}
	b, _ := os.ReadFile(p)
	if string(b) != orig {
		t.Errorf("校验失败时不得有任何写入:\n%s", string(b))
	}
}

func TestSaveConfigValuesWritesBothKinds(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	if err := os.WriteFile(p, []byte("[keeper]\nserver = https://x\nlogin_path = /a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPathCached = p
	if err := saveConfigValues(map[string]string{
		"server":        "https://y",
		"success_marks": "OK",
		"campus_dns":    "10.0.0.1",
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadDaemonConfig(p)
	if err != nil {
		t.Fatalf("改写后应可解析: %v", err)
	}
	if cfg.Server != "https://y" || cfg.CampusDNS != "10.0.0.1" {
		t.Errorf("常用键应生效，实际 server=%q dns=%q", cfg.Server, cfg.CampusDNS)
	}
	if len(cfg.Response.Success) != 1 || cfg.Response.Success[0] != "OK" {
		t.Errorf("[response] 词表应生效，实际 %v", cfg.Response.Success)
	}
}

func TestWriteAdvancedConfigRejectsInjection(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	if err := os.WriteFile(p, []byte("[keeper]\nserver = https://x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPathCached = p
	if err := writeAdvancedConfig(map[string]string{"interval": "5\n[evil]\nx = y"}); err == nil {
		t.Error("含换行的值必须拒绝（防注入新节/新键）")
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "[evil]") {
		t.Errorf("注入内容不得落盘:\n%s", string(b))
	}
}
