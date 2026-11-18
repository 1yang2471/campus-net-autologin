package app

import (
	"os"
	"path/filepath"
	"testing"
)

// 登录请求头与超时（Python keeper._headers/do_login 同源）：
// - UA 与 Referer 是门户校事实的一部分（WAF 对裸 Go-http-client 会慢回复/挂起）
// - login_timeout 独立于 probe_timeout：门户 RADIUS 后端 3-6s 才回，复用
//   probe_timeout=1s 会「向导测过、守护老超时」（Python P1-7 实测结论）

func TestConfigUserAgentAndLoginTimeout(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	os.WriteFile(p, []byte("[keeper]\nserver = https://x\nlogin_path = /a\nuser_agent = MyUA/1.0\nlogin_timeout = 9\nprobe_timeout = 1\n"), 0o600)
	c, err := loadDaemonConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.UserAgent != "MyUA/1.0" {
		t.Errorf("user_agent 应读取，实际 %q", c.UserAgent)
	}
	if c.LoginTimeout.Seconds() != 9 {
		t.Errorf("login_timeout 应 9s，实际 %v", c.LoginTimeout)
	}
}

// 行为：login_timeout=0/缺省 → 自适应 max(6s, 2×probe_timeout)，不低于 6s。
func TestConfigLoginTimeoutAdaptive(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	os.WriteFile(p, []byte("[keeper]\nserver = https://x\nlogin_path = /a\nprobe_timeout = 1\n"), 0o600)
	c, err := loadDaemonConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.LoginTimeout.Seconds(); got < 6 {
		t.Errorf("缺省自适应至少 6s，实际 %v", got)
	}
}

// 行为：UA 缺省 → Python 同款浏览器 UA（校事实缺省，不是空串）。
func TestConfigUserAgentDefault(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.ini")
	os.WriteFile(p, []byte("[keeper]\nserver = https://x\nlogin_path = /a\n"), 0o600)
	c, err := loadDaemonConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.UserAgent == "" || len(c.UserAgent) < 20 {
		t.Errorf("缺省 UA 必须是浏览器形态，实际 %q", c.UserAgent)
	}
}
