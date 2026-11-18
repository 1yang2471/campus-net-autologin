package main

import (
	"os"
	"path/filepath"
	"testing"

	"cnal/internal/protocol"
)

// CLI 与守护吃同一份 login_form：rebind 的登录表单必须同样可配。

func TestLoadConfigLoginForm(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	ini := "[portal]\nserver = https://x\nlogin_path = /a\n" +
		"login_form = opr=login&user={user}&pass={pass}\n"
	if err := os.WriteFile(p, []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.LoginForm.Template(); got != "opr=login&user={user}&pass={pass}" {
		t.Errorf("CLI 应读取 login_form，实际 %q", got)
	}
}

func TestLoadConfigLoginFormInvalid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	ini := "[keeper]\nserver = https://x\nlogin_path = /a\nlogin_form = userName={user}\n"
	if err := os.WriteFile(p, []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(p); err == nil {
		t.Error("CLI 遇到非法模板必须报错")
	}
}

func TestLoadConfigLoginFormDefault(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	if err := os.WriteFile(p, []byte("[keeper]\nserver = https://x\nlogin_path = /a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LoginForm.Template() != protocol.DefaultFormTemplate {
		t.Errorf("缺省应回落默认模板，实际 %q", cfg.LoginForm.Template())
	}
}
