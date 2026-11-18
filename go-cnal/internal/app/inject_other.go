//go:build !windows

package app

import (
	"fmt"
	"os"
	"path/filepath"
)

// 非 Windows（安卓/其他）默认凭据源：没有平台凭据库，必须由 Kotlin 通过
// SetCredentialSource 注入。未注入时 Load/Save 明确报错，绝不静默当成"无密码"
// 去触发一次带空凭据的登录（凭据只在内存，不落盘）。
func defaultCredentialSource() CredentialSource { return unboundCredentials{} }

type unboundCredentials struct{}

func (unboundCredentials) Load(string) (string, error) {
	return "", fmt.Errorf("未注入凭据来源（安卓需 SetCredentialSource）")
}

func (unboundCredentials) LoadLegacy() (string, string, error) {
	return "", "", fmt.Errorf("未注入凭据来源（安卓需 SetCredentialSource）")
}

func (unboundCredentials) Save(string, string) error {
	return fmt.Errorf("未注入凭据来源（安卓需 SetCredentialSource）")
}

func printSourceStdout(format string, args ...any) { fmt.Printf(format, args...) }

// defaultDataDir 非 Windows 默认：os.UserConfigDir()/campus-net-autologin。
// 安卓实际由 SetDataDir(context.filesDir) 覆盖，这里只是兜底。
func defaultDataDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "campus-net-autologin")
}
