//go:build windows

package store

import (
	"fmt"
	"os"
	"testing"
)

// S3 原生凭据：零依赖读写 Windows 凭据管理器（与 Python keyring WinVault
// 同存储：generic 凭据，simple target = 服务名，UTF-16LE blob，Persist=Local
// ——与本机用户绑定）。测试自建自毁一次性条目，绝不触碰真实凭据。

// 行为：写入的 generic 凭据 → 读回 user+secret 逐字节一致。
func TestCredentialRoundTrip(t *testing.T) {
	target := fmt.Sprintf("cnal-selftest-%d", os.Getpid())
	if err := WriteCredential(target, "cnal-test-user", "cnal-test-pass-31337"); err != nil {
		t.Fatalf("前置：写入失败: %v", err)
	}
	t.Cleanup(func() { _ = DeleteCredential(target) })

	user, secret, err := ReadCredential(target)
	if err != nil {
		t.Fatalf("读取应成功: %v", err)
	}
	if user != "cnal-test-user" {
		t.Errorf("user 应为 cnal-test-user，实际 %q", user)
	}
	if secret != "cnal-test-pass-31337" {
		t.Errorf("secret 应为写入值，实际 %q", secret)
	}
}

// 行为：不存在的条目 → 明确报错（守护据此报"守护是否保存过凭据"）。
func TestReadCredentialMissing(t *testing.T) {
	_, _, err := ReadCredential(fmt.Sprintf("cnal-selftest-absent-%d", os.Getpid()))
	if err == nil {
		t.Fatal("不存在的条目必须报错")
	}
}

// 行为：删除后读取必失败（清理可验证，不留测试残留）。
func TestDeleteCredentialRemoves(t *testing.T) {
	target := fmt.Sprintf("cnal-selftest-del-%d", os.Getpid())
	if err := WriteCredential(target, "u", "p"); err != nil {
		t.Fatalf("前置：写入失败: %v", err)
	}
	if err := DeleteCredential(target); err != nil {
		t.Fatalf("删除应成功: %v", err)
	}
	if _, _, err := ReadCredential(target); err == nil {
		t.Fatal("删除后读取必须失败")
	}
}

// 行为：空密码在 store 层就拒绝（api 层之外的第二道防线：空 blob 不得
// 覆盖已存真密码；2026-11-08 发布审查 P0）。
func TestSetPasswordRejectsEmpty(t *testing.T) {
	target := fmt.Sprintf("cnal-selftest-empty-%d", os.Getpid())
	_ = DeleteCredential(target) // 防上次中断残留
	t.Cleanup(func() { _ = DeleteCredential(target) })
	if err := WriteCredential(target, "u", ""); err == nil {
		t.Fatal("空密码必须被拒绝")
	}
	if _, _, err := ReadCredential(target); err == nil {
		t.Error("拒绝后不得留下凭据条目")
	}
}
