//go:build windows

package app

import (
	"fmt"
	"os"
	"path/filepath"

	"cnal/internal/store"
)

// defaultCredentialSource Windows 默认：凭据管理器直读（多账号 per-user）。
// 安卓不走这里——store 包全是 windows-only，inject_other.go 提供空实现。
func defaultCredentialSource() CredentialSource { return windowsCredentials{} }

type windowsCredentials struct{}

func (windowsCredentials) Load(user string) (string, error) {
	return store.LoadUserPassword(user)
}

func (windowsCredentials) LoadLegacy() (string, string, error) {
	return store.ReadCredential(store.CredService)
}

func (windowsCredentials) Save(user, pass string) error {
	return store.SaveUserPassword(user, pass)
}

func printSourceStdout(format string, args ...any) { fmt.Printf(format, args...) }

// defaultDataDir Windows 默认：%LOCALAPPDATA%/campus-net-autologin，
// 与 Python 守护共用目录（迁移成本为零）。
func defaultDataDir() string {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		d, err := os.UserConfigDir()
		if err != nil {
			dir = os.TempDir()
		} else {
			dir = d
		}
	}
	return filepath.Join(dir, "campus-net-autologin")
}
