package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 面板可改字段白名单（updateIniFields）：新键必须可写且只动白名单键，
// 其余行原样保留（[accounts] 节、注释）。

func TestUpdateIniFieldsWhitelist(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cnal.ini")
	orig := "[keeper]\r\nserver = https://x\r\nusername = a\r\nauto_reconnect = true\r\n\r\n[accounts]\r\n1 = a|https://x\r\n"
	if err := os.WriteFile(p, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPathCached = p
	if err := updateIniFields(map[string]string{
		"auto_reconnect": "false",
		"bind_ip":        "172.29.1.5",
		"evil_key":       "boom", // 白名单外，必须被忽略
	}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	if !strings.Contains(s, "auto_reconnect = false") {
		t.Errorf("auto_reconnect 应被改写:\n%s", s)
	}
	if !strings.Contains(s, "bind_ip = 172.29.1.5") {
		t.Errorf("bind_ip 新键应追加:\n%s", s)
	}
	if strings.Contains(s, "evil_key") {
		t.Errorf("白名单外键必须忽略:\n%s", s)
	}
	if !strings.Contains(s, "[accounts]") {
		t.Errorf("其他节必须原样保留:\n%s", s)
	}
}

// 行为：LF 行尾的 ini（手工编辑/异机拷贝常见）也必须能就地改写，
// 不得把整份压成一行（2026-11-08 发布审查 P1）。
func TestUpdateIniFieldsLFPreservesLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cnal.ini")
	orig := "[keeper]\nserver = https://x\nusername = a\nfree_windows = 23:30-06:30\n\n[accounts]\n1 = a|https://x\n"
	if err := os.WriteFile(p, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPathCached = p
	if err := updateIniFields(map[string]string{"server": "https://y"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	if strings.Count(s, "\n") < 6 {
		t.Errorf("LF 文件应保持多行，实际被压扁:\n%s", s)
	}
	if !strings.Contains(s, "server = https://y") {
		t.Errorf("键值应被改写:\n%s", s)
	}
	if !strings.Contains(s, "[accounts]") {
		t.Errorf("其他节应保留:\n%s", s)
	}
}
