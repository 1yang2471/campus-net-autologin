//go:build windows

package store

import "fmt"

// 多账号凭据（Python shell/credentials.py 同源语义）：密码按用户名分条存
// compound target（<user>@<service>），切换账号 = 换用户名查自己的条目。
// simple target（<service>，keyring.set_password 遗留单条）只作回落读取。

// CredService 凭据服务名（与 Python keyring SERVICE 常量同值；app 包引用此常量）。
const CredService = "campus-net-autologin"

// compoundTarget Python keyring compound 形态：<user>@<service>。
func compoundTarget(username string) string { return username + "@" + CredService }

// SaveUserPassword 把 username 的密码写入它自己的凭据条目（不落盘）。
func SaveUserPassword(username, password string) error {
	return WriteCredential(compoundTarget(username), username, password)
}

// LoadUserPassword 读 username 的密码；simple target 回落仅当其 UserName 与
// 查询的用户名一致（keyring 首存只写 simple——但 simple 条目是"谁的密码"以
// 它的 UserName 字段为准，绝不张冠李戴；Python 2026-11-04 迁移事故同源防线）。
func LoadUserPassword(username string) (string, error) {
	if pw, err := readPassword(compoundTarget(username)); err == nil {
		return pw, nil
	}
	u, pw, err := ReadCredential(CredService)
	if err != nil {
		return "", err
	}
	if u != username {
		return "", fmt.Errorf("simple 凭据属于 %q，不匹配查询的 %q", u, username)
	}
	return pw, nil
}

// readPassword 只取密码不取用户名（UserName 字段在 compound 形态里即 username）。
func readPassword(target string) (string, error) {
	_, pw, err := ReadCredential(target)
	return pw, err
}
