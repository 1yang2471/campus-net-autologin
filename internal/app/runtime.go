package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// 运行时端点文件：运行中守护把 127.0.0.1 端口与回路 token 写在这里，
// `cnal status` 据此读取真实状态（守护是唯一状态真源，CLI 不另算一份）。
//
// 信任边界：token 只授予本机回路（127.0.0.1）访问，不含任何用户凭据；
// 文件落在 %LOCALAPPDATA% 用户目录（Windows 上 os.WriteFile 的权限位不生效，
// 实际依赖该目录的用户 ACL）。同用户的本地进程本就能读 ini/日志，不扩大
// 信任面。文件绝不写密码（凭据只在 keyring/环境变量/内存）。

// RuntimeInfo 端点文件内容。JSON 键名固定（旧版本读新文件也能降级）。
type RuntimeInfo struct {
	URL   string `json:"url"`   // http://127.0.0.1:<port>
	Token string `json:"token"` // 回路 Bearer token（每次启动随机）
}

// RuntimeFilePath 端点文件默认路径（与 config/log/lock 同目录）。
func RuntimeFilePath() string { return filepath.Join(DataDir(), "api.json") }

// WriteRuntimeFile 原子写端点文件（临时文件 + rename，避免 status 读到半截
// JSON），返回退出时删除文件的 cleanup。url/token 为空直接拒绝——空值会让
// status 发一次无意义请求。
func WriteRuntimeFile(path, url, token string) (func(), error) {
	if url == "" || token == "" {
		return nil, errors.New("端点文件缺少 url/token")
	}
	b, err := json.Marshal(RuntimeInfo{URL: url, Token: token})
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return func() { _ = os.Remove(path) }, nil
}

// ReadRuntimeFile 读端点文件。文件缺失/损坏/字段不全都返回 error——status
// 据此报「守护未运行」，绝不猜端口（猜错会把状态查询发到别的进程）。
func ReadRuntimeFile(path string) (RuntimeInfo, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return RuntimeInfo{}, err
	}
	var info RuntimeInfo
	if err := json.Unmarshal(b, &info); err != nil {
		return RuntimeInfo{}, fmt.Errorf("端点文件损坏: %w", err)
	}
	if info.URL == "" || info.Token == "" {
		return RuntimeInfo{}, errors.New("端点文件缺少 url/token")
	}
	return info, nil
}
