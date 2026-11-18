package app

import (
	"errors"
	"os"
	"path/filepath"

	"cnal/internal/protocol"
)

// WriteStarterConfig 首跑生成最小 [keeper] 配置（只含探测到的门户与 ac_portal
// 族默认端点）。已存在文件不覆盖（返回 os.ErrExist）——用户既有配置绝不被
// 探测器改掉。账号密码不进 ini：首次启动托盘会在面板里收集，凭据只进
// Windows 凭据管理器。
func WriteStarterConfig(path, server string) error {
	if _, err := os.Stat(path); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := "; go-cnal 首跑生成（cnal detect）。端点路径是校事实，异校请核对 README。\n" +
		"[keeper]\n" +
		"server = " + server + "\n" +
		"login_path = " + protocol.DefaultLoginPath + "\n" +
		"login_page = " + protocol.DefaultLoginPage + "\n" +
		"; campus_dns = 10.x.x.x   ; 已知校园 DNS 时填上（物理源选路/门户钉 IP）\n" +
		"; probe_ips / probe_path / probe_host / user_agent / ssl_verify 等可选项见 README\n"
	return os.WriteFile(path, []byte(body), 0o600)
}

// ensureIniFile 保证配置文件存在（不存在则建含 [keeper] 节的空骨架），
// 供首跑配置面板就地补齐字段。
func ensureIniFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("[keeper]\n"), 0o600)
}
