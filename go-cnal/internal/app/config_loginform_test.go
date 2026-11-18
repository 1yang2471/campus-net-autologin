package app

import (
	"os"
	"path/filepath"
	"testing"

	"cnal/internal/protocol"
)

// 登录表单模板的配置面：ini 的 login_form 覆盖默认；非法模板在启动时
// 可见报错（不得静默回落默认，否则用户以为改生效了）。

func TestConfigLoginFormCustom(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	ini := "[keeper]\nserver = https://x\nlogin_path = /a\n" +
		"login_form = opr=login&user_account={user}&user_password={pass}&lang=zh\n"
	if err := os.WriteFile(p, []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadDaemonConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "opr=login&user_account={user}&user_password={pass}&lang=zh"
	if got := cfg.LoginForm.Template(); got != want {
		t.Errorf("应读取 login_form\n实际: %q\n期望: %q", got, want)
	}
}

func TestConfigLoginFormDefault(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	if err := os.WriteFile(p, []byte("[keeper]\nserver = https://x\nlogin_path = /a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadDaemonConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LoginForm.Template() != protocol.DefaultFormTemplate {
		t.Errorf("缺 login_form 应回落默认，实际 %q", cfg.LoginForm.Template())
	}
}

func TestConfigLoginFormInvalid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	ini := "[keeper]\nserver = https://x\nlogin_path = /a\nlogin_form = userName={user}\n"
	if err := os.WriteFile(p, []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDaemonConfig(p); err == nil {
		t.Error("缺 {pass} 的模板必须启动时报错")
	}
}
