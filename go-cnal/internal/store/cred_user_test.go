//go:build windows

package store

import "testing"

// 多账号语义（Python shell/credentials.py 同源）：密码按用户名分条存
// compound target（<user>@<service>），切换账号 = 换用户名查自己的条目。
// simple target（<service>）是 keyring.set_password 的遗留单条形态，
// 只作回落读取，不再写入。

// 行为：SaveUserPassword 写 per-user 条目 → LoadUserPassword 读回一致。
func TestUserCredentialRoundTrip(t *testing.T) {
	if err := SaveUserPassword("cnal-selftest-u1", "cnal-test-pass-31337"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	t.Cleanup(func() { _ = DeleteCredential("cnal-selftest-u1@" + CredService) })
	pw, err := LoadUserPassword("cnal-selftest-u1")
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if pw != "cnal-test-pass-31337" {
		t.Errorf("应读回写入值，实际 %q", pw)
	}
}

// 行为：另一个用户名 → 读不到别人的密码（张冠李戴防线，Python P-事故同源）。
func TestUserCredentialIsolated(t *testing.T) {
	if err := SaveUserPassword("cnal-selftest-u1", "cnal-test-pass-31337"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = DeleteCredential("cnal-selftest-u1@" + CredService) })
	if _, err := LoadUserPassword("cnal-selftest-nobody"); err == nil {
		t.Error("未注册用户必须读不到")
	}
}
