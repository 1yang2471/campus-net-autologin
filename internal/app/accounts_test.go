package app

import (
	"os"
	"path/filepath"
	"testing"
)

// 历史账号：ini [accounts] 段，序号即最近使用顺序（1 最新），值 "user|server"。
// 密码不进 ini（keyring per-user），这里只存用户名+门户。上限 5（Python MAX_ACCOUNTS）。

func TestRememberAccountRoundtripAndRecency(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cnal.ini")
	os.WriteFile(p, []byte("[keeper]\r\nserver = https://a\r\n"), 0o600)

	RememberAccount(p, "u1", "https://a")
	RememberAccount(p, "u2", "https://b")
	accs := ListAccounts(p)
	if len(accs) != 2 || accs[0].Username != "u2" || accs[0].Server != "https://b" {
		t.Fatalf("最近使用应置顶，实际 %+v", accs)
	}
	// 重复账号提到顶部不新增。
	RememberAccount(p, "u1", "https://a")
	accs = ListAccounts(p)
	if len(accs) != 2 || accs[0].Username != "u1" {
		t.Errorf("重复账号应提顶不新增，实际 %+v", accs)
	}
	// 上限 5：塞 6 个，最久未用淘汰。
	for _, u := range []string{"x1", "x2", "x3", "x4", "x5", "x6"} {
		RememberAccount(p, u, "https://a")
	}
	if got := ListAccounts(p); len(got) != 5 {
		t.Errorf("历史上限 5，实际 %d: %+v", len(got), got)
	}
}

// 行为：段损坏（缺 | 或空用户名）的条目忽略，不 panic。
func TestListAccountsSkipsCorrupt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cnal.ini")
	os.WriteFile(p, []byte("[keeper]\r\nserver = https://a\r\n[accounts]\r\n1 = noverticalbar\r\n2 = |nosec\r\n3 = good|https://a\r\n"), 0o600)
	accs := ListAccounts(p)
	if len(accs) != 1 || accs[0].Username != "good" {
		t.Errorf("损坏条目应跳过，实际 %+v", accs)
	}
}
