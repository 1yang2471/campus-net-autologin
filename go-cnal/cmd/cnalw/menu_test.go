package main

import (
	"os"
	"path/filepath"
	"testing"
)

// UI②托盘菜单对齐 Python 守护 7 动作（keeper.py:1760-1771）：立即检测重连
// （默认项）、查流量、打开认证页、查看日志、开机自启（勾选态）、退出。
// 「打开面板」在 Go 侧由 web 面板切片决定去留——先对齐其余 6 项 + 状态行。
// 菜单结构由 systray 在运行时构建，不可直接枚举；可测的是动作背后的缝。

// 行为：toggleAutostart 装自启后 isAutostartEnabled 为真；卸载后为假。
// 写真实 HKCU Run 键（值名 CampusNetAutoLogin-Go 唯一，自建自毁）。
func TestAutostartToggleRoundTrip(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skip("取不到测试进程路径，跳过")
	}
	cfg := filepath.Join(t.TempDir(), "config.ini")
	if err := os.WriteFile(cfg, []byte("[keeper]\nserver=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installAutostart(exe, cfg); err != nil {
		t.Fatalf("装自启失败: %v", err)
	}
	if !isAutostartEnabled() {
		t.Error("装完后应报告已启用")
	}
	if err := removeAutostart(); err != nil {
		t.Fatalf("卸自启失败: %v", err)
	}
	if isAutostartEnabled() {
		t.Error("卸完后应报告未启用")
	}
}

// 行为：openURL 用系统默认浏览器打开（不拼接 shell；只验证不 panic 且接受
// 合法 http/https）。
func TestOpenURLAcceptsHTTP(t *testing.T) {
	if err := openURL("https://a.stu.edu.cn"); err != nil {
		t.Errorf("合法 https 应接受: %v", err)
	}
	if err := openURL("file:///C:/Windows/System32"); err == nil {
		t.Error("非 http/https 必须拒绝（防配置项被改成执行任意文件）")
	}
}
