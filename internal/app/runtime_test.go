package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 运行时端点文件：cnal status 靠它找到运行中守护的 127.0.0.1 端口与 token。
// 守护启动时写、退出时删；文件缺失 = 未运行，不是错误。

func TestRuntimeFileRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "api.json")
	cleanup, err := WriteRuntimeFile(p, "http://127.0.0.1:45678", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ReadRuntimeFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.URL != "http://127.0.0.1:45678" || info.Token != "abc123" {
		t.Errorf("端点文件应原样读回，实际 %+v", info)
	}
	cleanup()
	if _, err := ReadRuntimeFile(p); err == nil {
		t.Error("cleanup 应删除端点文件（守护退出后 status 不得再找到旧端点）")
	}
}

func TestRuntimeFileOverwritesPreviousRun(t *testing.T) {
	p := filepath.Join(t.TempDir(), "api.json")
	if _, err := WriteRuntimeFile(p, "http://127.0.0.1:1", "old"); err != nil {
		t.Fatal(err)
	}
	cleanup, err := WriteRuntimeFile(p, "http://127.0.0.1:2", "new")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	info, err := ReadRuntimeFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.URL != "http://127.0.0.1:2" || info.Token != "new" {
		t.Errorf("重启后端点文件应被覆盖，实际 %+v", info)
	}
}

func TestReadRuntimeFileRejectsIncomplete(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"缺 token": `{"url":"http://127.0.0.1:1"}`,
		"缺 url":   `{"token":"x"}`,
		"坏 JSON":  `{`,
	} {
		p := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".json")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadRuntimeFile(p); err == nil {
			t.Errorf("%s 的端点文件必须拒绝（坏数据不得变成一次请求）", name)
		}
	}
	if _, err := ReadRuntimeFile(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("缺文件必须报错（status 据此报「守护未运行」）")
	}
}

func TestWriteRuntimeFileRejectsEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "api.json")
	if _, err := WriteRuntimeFile(p, "", "tok"); err == nil {
		t.Error("空 URL 必须拒绝")
	}
	if _, err := WriteRuntimeFile(p, "http://127.0.0.1:1", ""); err == nil {
		t.Error("空 token 必须拒绝")
	}
}
