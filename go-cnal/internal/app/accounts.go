package app

import (
	"fmt"
	"os"
	"strings"
)

// 历史账号（多账号切换）：ini [accounts] 段，序号即最近使用顺序（1 最新），
// 值 "user|server"。密码走 keyring per-user（store.LoadUserPassword），不落盘。
// 语义对齐 Python shell/accounts.py（MAX_ACCOUNTS=5）。

// maxAccounts 历史上限，超出淘汰最久未用。
const maxAccounts = 5

// Account 历史账号（用户名 + 上次用的门户）。
type Account struct {
	Username string
	Server   string
}

// ListAccounts 读 [accounts] 段，按序号升序返回；损坏条目（无 |、空用户名）跳过。
func ListAccounts(path string) []Account {
	lines, err := readIniLines(path)
	if err != nil {
		return nil
	}
	var out []Account
	inSection := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			inSection = t == "[accounts]"
			continue
		}
		if !inSection || t == "" || strings.HasPrefix(t, ";") || strings.HasPrefix(t, "#") {
			continue
		}
		_, val, found := strings.Cut(t, "=")
		if !found {
			continue
		}
		user, server, hasBar := strings.Cut(strings.TrimSpace(val), "|")
		user, server = strings.TrimSpace(user), strings.TrimSpace(server)
		if user != "" && hasBar {
			out = append(out, Account{Username: user, Server: server})
		}
	}
	return out
}

// RememberAccount 把账号提到历史顶部（重复不新增），改写 [accounts] 段、保留其余。
func RememberAccount(path, username, server string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil
	}
	entries := []Account{{username, strings.TrimSpace(server)}}
	for _, a := range ListAccounts(path) {
		if a.Username != username && len(entries) < maxAccounts {
			entries = append(entries, a)
		}
	}
	lines, err := readIniLines(path)
	if err != nil {
		return err
	}
	var kept []string
	skip := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			skip = t == "[accounts]"
		}
		if skip {
			continue
		}
		kept = append(kept, line)
	}
	kept = append(kept, accountsSectionLines(entries)...)
	return os.WriteFile(path, []byte(strings.Join(kept, iniEOL(path))), 0o600)
}

// accountsSectionLines 渲染 [accounts] 段（序号从 1，\r\n 由外层 join 统一）。
func accountsSectionLines(entries []Account) []string {
	ls := []string{"", "[accounts]"}
	for i, a := range entries {
		ls = append(ls, fmt.Sprintf("%d = %s|%s", i+1, a.Username, a.Server))
	}
	return ls
}
