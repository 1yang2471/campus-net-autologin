package keeper

import (
	"testing"
)

// 热切换语义（用户拍板：面板切换账号立即生效，不重启进程）：
// Relocate(user, pass) 原子换掉 LoginAction 的凭据 + ini username 指向；
// 下一次登录（自动或手动）即用新账号。并发安全：手动重连在 API goroutine。

// 行为：Relocate 后 Action 的 User/Pass 变为新值。
func TestRelocateSwapsCredentials(t *testing.T) {
	a := &LoginAction{User: "old", Pass: "old-pw"}
	if err := a.Relocate("new", "new-pw"); err != nil {
		t.Fatalf("切换失败: %v", err)
	}
	if a.User != "new" || a.Pass != "new-pw" {
		t.Errorf("应换为新凭据，实际 %q/%q", a.User, a.Pass)
	}
}

// 行为：Relocate 空用户名/空密码 → 拒绝且不破坏现有凭据。
func TestRelocateRejectsEmpty(t *testing.T) {
	a := &LoginAction{User: "old", Pass: "old-pw"}
	if err := a.Relocate("", "x"); err == nil {
		t.Error("空用户名必须拒绝")
	}
	if err := a.Relocate("u", ""); err == nil {
		t.Error("空密码必须拒绝")
	}
	if a.User != "old" || a.Pass != "old-pw" {
		t.Errorf("拒绝后不得破坏现有凭据，实际 %q/%q", a.User, a.Pass)
	}
}
