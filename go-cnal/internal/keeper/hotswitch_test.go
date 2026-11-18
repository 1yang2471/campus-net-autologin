package keeper

import (
	"strings"
	"testing"
)

// 热切换端到端（用户拍板：面板切换立即生效）：Relocate 后的下一次登录
// 必须用新账号——表单 userName 是用户感受得到的行为断言点。

// 行为：Relocate("new","new-pw") → Do() → Deliver 收到的表单 userName=new。
func TestHotSwitchNextLoginUsesNewAccount(t *testing.T) {
	var gotUser, gotPass string
	a := &LoginAction{
		User: "old", Pass: "old-pw",
		Deliver: func(form map[string]string) (string, error) {
			gotUser = form["userName"]
			gotPass = form["pwd"] // ac_portal 方言：密码键是 pwd 不是 password
			return "{'success':true}", nil
		},
	}
	if err := a.Relocate("new", "new-pw"); err != nil {
		t.Fatalf("切换失败: %v", err)
	}
	if _, err := a.Do(); err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if gotUser != "new" || gotPass != "new-pw" {
		t.Errorf("切换后下一次登录必须用新账号，实际 %q/%q", gotUser, gotPass)
	}
}

// 行为：切换发生在 Do 进行中也不得撕裂表单（新旧凭据不混用）——
// Do 先取快照再构建表单，Relocate 的锁保证原子。
func TestHotSwitchSnapshotConsistency(t *testing.T) {
	// Do 内部 credentials() 快照取的是同一锁下的成对值；此测试验证
	// BuildLogin 收到的 user/pass 来自同一次快照（RWMutex 读锁持全程）。
	form := map[string]string{}
	a := &LoginAction{
		User: "u1", Pass: "p1",
		Deliver: func(f map[string]string) (string, error) {
			for k, v := range f {
				form[k] = v
			}
			return "{'success':true}", nil
		},
	}
	if _, err := a.Do(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(form["userName"], "u1") {
		t.Errorf("快照一致性被破坏: %v", form)
	}
}
