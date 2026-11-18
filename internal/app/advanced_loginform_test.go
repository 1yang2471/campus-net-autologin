package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cnal/internal/protocol"
)

// 面板高级区承载 login_form：回显默认、保存前校验、写入 [keeper] 段。

func TestAdvancedConfigViewIncludesLoginForm(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	if err := os.WriteFile(p, []byte("[keeper]\nserver = https://x\nlogin_path = /a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	view := advancedConfigView(p)
	if view["login_form"] != protocol.DefaultFormTemplate {
		t.Errorf("高级视图应回显默认模板，实际 %q", view["login_form"])
	}
}

func TestValidateAdvancedConfigLoginForm(t *testing.T) {
	if err := validateAdvancedConfig(map[string]string{"login_form": "userName={user}"}); err == nil {
		t.Error("缺 {pass} 的模板必须被保存前校验拒绝")
	}
	if err := validateAdvancedConfig(map[string]string{
		"login_form": "opr=login&user={user}&pass={pass}",
	}); err != nil {
		t.Errorf("合法模板不应被拒: %v", err)
	}
}

func TestWriteAdvancedConfigWritesLoginForm(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	if err := os.WriteFile(p, []byte("[keeper]\nserver = https://x\nlogin_path = /a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := cfgPathCached
	cfgPathCached = p
	defer func() { cfgPathCached = old }()

	if err := writeAdvancedConfig(map[string]string{
		"login_form": "opr=login&user={user}&pass={pass}",
	}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "login_form = opr=login&user={user}&pass={pass}") {
		t.Errorf("login_form 应写入 ini，实际:\n%s", string(b))
	}
}
