package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cnal/internal/protocol"
)

func TestLoadConfigParsesPortalSection(t *testing.T) {
	ini := "[portal]\n" +
		"; 注释行应被忽略\n" +
		"server = http://192.0.2.7\n" +
		"login_path = /ac_portal/login.php\n" +
		"dual_stack = true\n" +
		"v6_peer = 2001:db8::1\n" +
		"timeout_sec = 3\n"
	path := filepath.Join(t.TempDir(), "t.ini")
	if err := os.WriteFile(path, []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("应能解析: %v", err)
	}
	if cfg.Server != "http://192.0.2.7" {
		t.Errorf("server 解析错: %q", cfg.Server)
	}
	if cfg.LoginPath != "/ac_portal/login.php" {
		t.Errorf("login_path 解析错: %q", cfg.LoginPath)
	}
	if !cfg.DualStack {
		t.Errorf("dual_stack 应为 true")
	}
	if cfg.V6Peer != "2001:db8::1" {
		t.Errorf("v6_peer 解析错: %q", cfg.V6Peer)
	}
	if cfg.Timeout != 3*time.Second {
		t.Errorf("timeout 应为 3s，实际 %v", cfg.Timeout)
	}
}

func TestLoadConfigRejectsMissingServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.ini")
	if err := os.WriteFile(path, []byte("[portal]\ndual_stack = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Errorf("缺 server 必须报错")
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	if _, err := loadConfig(filepath.Join(t.TempDir(), "nope.ini")); err == nil {
		t.Errorf("配置缺失必须报错")
	}
}

// ssl_verify：默认 true（安全默认， Mimosa 立的规矩）；显式 false 才放开——
// 自签门户是校事实（守护配置就是 ssl_verify=false），但要用户显式选。
func TestLoadConfigSSLVerifyDefaultSecure(t *testing.T) {
	ini := "[portal]\nserver = http://192.0.2.7\nlogin_path = /ac_portal/login.php\n"
	path := filepath.Join(t.TempDir(), "t.ini")
	if err := os.WriteFile(path, []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("应能解析: %v", err)
	}
	if !cfg.SSLVerify {
		t.Errorf("缺省必须开启证书校验（安全默认）")
	}
}

func TestLoadConfigSSLVerifyExplicitOptOut(t *testing.T) {
	ini := "[portal]\nserver = http://192.0.2.7\nlogin_path = /ac_portal/login.php\nssl_verify = false\n"
	path := filepath.Join(t.TempDir(), "t.ini")
	if err := os.WriteFile(path, []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("应能解析: %v", err)
	}
	if cfg.SSLVerify {
		t.Errorf("显式 ssl_verify=false 应被采纳")
	}
}

// 对端端点路径是校事实，必须来自配置（计划执行者注意：路径不进代码）。
// 缺省为根路径——对端不可达根时由配置给出真实路径。
func TestLoadConfigPeerPath(t *testing.T) {
	ini := "[portal]\nserver = http://192.0.2.7\nlogin_path = /ac_portal/login.php\npeer_path = /ac_portal/login.php\n"
	path := filepath.Join(t.TempDir(), "t.ini")
	if err := os.WriteFile(path, []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("应能解析: %v", err)
	}
	if cfg.PeerPath != "/ac_portal/login.php" {
		t.Errorf("peer_path 解析错: %q", cfg.PeerPath)
	}

	ini2 := "[portal]\nserver = http://192.0.2.7\nlogin_path = /x\n"
	path2 := filepath.Join(t.TempDir(), "t2.ini")
	if err := os.WriteFile(path2, []byte(ini2), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg2, err := loadConfig(path2)
	if err != nil {
		t.Fatalf("应能解析: %v", err)
	}
	if cfg2.PeerPath != "/" {
		t.Errorf("peer_path 缺省应为根路径，实际 %q", cfg2.PeerPath)
	}
}

// 对端握手操作名是词表（校事实）：2026-11-07 实测真名 ipv4or6online，
// 计划起草用的 DUALSTACKQUERY 是占位名。不设默认，异校必须显式配置。
func TestLoadConfigPeerOpr(t *testing.T) {
	ini := "[portal]\nserver = http://192.0.2.7\nlogin_path = /x\npeer_opr = ipv4or6online\n"
	path := filepath.Join(t.TempDir(), "t.ini")
	if err := os.WriteFile(path, []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("应能解析: %v", err)
	}
	if cfg.PeerOpr != "ipv4or6online" {
		t.Errorf("peer_opr 解析错: %q", cfg.PeerOpr)
	}

	ini2 := "[portal]\nserver = http://192.0.2.7\nlogin_path = /x\n"
	path2 := filepath.Join(t.TempDir(), "t2.ini")
	if err := os.WriteFile(path2, []byte(ini2), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg2, err := loadConfig(path2)
	if err != nil {
		t.Fatalf("应能解析: %v", err)
	}
	if cfg2.PeerOpr != "" {
		t.Errorf("peer_opr 不应设默认（词表异校必配），实际 %q", cfg2.PeerOpr)
	}
}

// 行为：正式守护 ini 用 [keeper] 节；CLI 必须能直接吃同一份配置
// （2026-11-08 发布审查 P0：CLI 只认 [portal] → 用正式 ini 跑直接缺 server）。
func TestLoadConfigAcceptsKeeperSection(t *testing.T) {
	ini := "[keeper]\n" +
		"server = https://a.stu.edu.cn\n" +
		"login_path = /ac_portal/login.php\n" +
		"timeout_sec = 3\n"
	path := filepath.Join(t.TempDir(), "t.ini")
	if err := os.WriteFile(path, []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("正式 ini 应能被 CLI 读取: %v", err)
	}
	if cfg.Server != "https://a.stu.edu.cn" {
		t.Errorf("server 解析错: %q", cfg.Server)
	}
}

// 行为：CLI 必须吃 [keeper] 的 user_agent/login_page（异校 WAF 校事实）；
// 缺省 UA 与守护同源（protocol.DefaultUserAgent），不许再硬编码一份。
func TestLoadConfigUserAgentAndLoginPage(t *testing.T) {
	ini := "[keeper]\n" +
		"server = https://a.example\n" +
		"login_path = /login\n" +
		"login_page = /ac_portal/login.php\n" +
		"user_agent = CustomUA/1.0\n"
	path := filepath.Join(t.TempDir(), "t.ini")
	if err := os.WriteFile(path, []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("应能解析: %v", err)
	}
	if cfg.UserAgent != "CustomUA/1.0" {
		t.Errorf("user_agent 应被采纳，实际 %q", cfg.UserAgent)
	}
	if cfg.LoginPage != "/ac_portal/login.php" {
		t.Errorf("login_page 应被采纳，实际 %q", cfg.LoginPage)
	}

	ini2 := "[keeper]\nserver = https://a.example\nlogin_path = /login\n"
	path2 := filepath.Join(t.TempDir(), "t2.ini")
	if err := os.WriteFile(path2, []byte(ini2), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg2, err := loadConfig(path2)
	if err != nil {
		t.Fatalf("应能解析: %v", err)
	}
	if cfg2.UserAgent != protocol.DefaultUserAgent {
		t.Errorf("缺省 UA 应取 protocol.DefaultUserAgent，实际 %q", cfg2.UserAgent)
	}
	if cfg2.LoginPage != "" {
		t.Errorf("login_page 不应有默认，实际 %q", cfg2.LoginPage)
	}
}
