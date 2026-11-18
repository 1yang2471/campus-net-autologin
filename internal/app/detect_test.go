package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 首跑探测的安全边界（Python parse_portal_location 同源）：只接受 http/https，
// 拒绝环回/localhost/链路本地——凭据绝不发往猜测地址；探测链路本身无凭据。

func TestParsePortalLocation(t *testing.T) {
	cases := []struct {
		name string
		loc  string
		want string
		ok   bool
	}{
		{"域名 302", "http://a.stu.edu.cn/ac_portal/login.php?x=1", "http://a.stu.edu.cn", true},
		{"带端口", "https://10.0.0.1:8443/ac_portal/login.php", "https://10.0.0.1:8443", true},
		{"私有段允许", "http://192.168.1.1/ac_portal", "http://192.168.1.1", true},
		{"IPv6 字面量", "http://[2001:db8::1]:8080/ac_portal", "http://[2001:db8::1]:8080", true},
		{"非 http 协议", "javascript:alert(1)", "", false},
		{"ftp 拒绝", "ftp://10.0.0.1/x", "", false},
		{"环回拒绝", "http://127.0.0.1/ac_portal", "", false},
		{"localhost 拒绝", "http://localhost:8080/x", "", false},
		{"子域 localhost 拒绝", "http://a.localhost/x", "", false},
		{"链路本地拒绝", "http://169.254.1.1/x", "", false},
		{"未指定地址拒绝", "http://0.0.0.0/x", "", false},
		{"IPv6 环回拒绝", "http://[::1]/x", "", false},
		{"空值", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parsePortalLocation(tc.loc)
			if ok != tc.ok || got != tc.want {
				t.Errorf("parsePortalLocation(%q) = %q,%v；期望 %q,%v", tc.loc, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// 200 型劫持页兜底：只认正文里明确含 ac_portal 的绝对 URL；相对链接
// （会落到锚点 IP 上）与无指纹页一律拒绝。
func TestPortalBaseFromBody(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
		ok   bool
	}{
		{"绝对链接", `<meta http-equiv="refresh" content="0;url=http://10.0.0.1/ac_portal/default/pc.html">`, "http://10.0.0.1", true},
		{"a 标签", `<a href="https://portal.example/ac_portal/login.php">登录</a>`, "https://portal.example", true},
		{"相对链接拒绝", `<meta http-equiv="refresh" content="0;url=/ac_portal/default/pc.html">`, "", false},
		{"无指纹拒绝", `<html><body>Welcome</body></html>`, "", false},
		{"环回拒绝", `<a href="http://127.0.0.1/ac_portal/login.php">x</a>`, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := portalBaseFromBody(tc.body)
			if ok != tc.ok || got != tc.want {
				t.Errorf("portalBaseFromBody = %q,%v；期望 %q,%v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

// 行为：首跑生成的最小配置含探测到的 server 与 ac_portal 默认端点；
// 既有文件绝不覆盖（用户配置优先于探测器）。
func TestWriteStarterConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "config.ini")
	if err := WriteStarterConfig(path, "https://a.example"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	for _, want := range []string{"[keeper]", "server = https://a.example", "login_path = /ac_portal/login.php"} {
		if !strings.Contains(body, want) {
			t.Errorf("starter 配置应含 %q，实际:\n%s", want, body)
		}
	}
	if err := WriteStarterConfig(path, "https://b.example"); !errors.Is(err, os.ErrExist) {
		t.Errorf("既有文件应返回 os.ErrExist，实际 %v", err)
	}
	b2, _ := os.ReadFile(path)
	if strings.Contains(string(b2), "b.example") {
		t.Error("既有配置不得被覆盖")
	}
}

// 行为：首跑面板的就地补齐依赖 [keeper] 骨架存在。
func TestEnsureIniFileCreatesKeeperSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.ini")
	if err := ensureIniFile(path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(b)) != "[keeper]" {
		t.Errorf("空骨架应只有 [keeper] 节，实际 %q", string(b))
	}
}

// 行为：缺配置启动 → SetupError（cnalw 据此进配置面板，不报错退出）。
func TestComposeMissingConfigIsSetupError(t *testing.T) {
	_, err := Compose(filepath.Join(t.TempDir(), "none.ini"))
	if err == nil {
		t.Fatal("缺配置应报错")
	}
	if _, ok := AsSetupError(err); !ok {
		t.Errorf("缺配置应归 SetupError，实际 %T %v", err, err)
	}
}
