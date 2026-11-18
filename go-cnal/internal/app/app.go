// Package app 组合根：配置→凭据→物理源→内核四缝→回路 API。
// cmd/cnald（无头）与 cmd/cnalw（托盘）共吃这一份接线，防双份漂移。
//
// 接线按用途拆三个文件（可读性；仍是同一份 Compose）：
//   - compose_portal.go 物理源/门户钉 IP/各客户端/登录动作/速率闸门
//   - compose_daemon.go 守护状态机与探针缝
//   - compose_api.go    回路 API 全部缝（状态/动作/设置）
package app

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"cnal/internal/api"
	"cnal/internal/keeper"
)

// App 持有组合好的生产对象；全部由 Compose 装配，退出码语义由 cmd 层映射。
type App struct {
	Cfg    daemonConfig
	Daemon *keeper.Daemon
	API    *api.Server
	// ManualLogin 手动重连动作（用户显式意图，越过自动闸与速率熔断——
	// Python tick(manual=True)「手动重连不受限」同语义；8/h 桶只记自动登录）。
	ManualLogin func() error
	// Logger 状态迁移日志（cnalw 注入文件、cnald 打 stdout；nil = 不记）。
	Logger *TransitionLogger
	// Heart 心跳日志（复盘 §3.1：每拍必落行，A1 漏拍测量；nil = 不记）。
	Heart *HeartbeatLogger
	// LogFile rotate 日志写端（mobile recordLog 落「唤醒:」等事件行用）。
	// internal 包不直接用它——mobile.Start 注入，供事件行与心跳同文件。
	LogFile *RotateWriter
}

// Compose 装配生产缝。返回错误即启动失败（配置/凭据/无物理源）。
func Compose(cfgPath string) (*App, error) {
	cfg, err := loadDaemonConfig(cfgPath)
	if err != nil {
		// 首跑可修复的缺口（缺文件/缺 server/login_path）交给 SetupError：
		// cnalw 据此进入首个配置面板，而不是让新机托盘直接报错退出。
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, ErrMissingServer) ||
			errors.Is(err, ErrMissingLoginPath) {
			return nil, &SetupError{Reason: "配置未就绪: " + err.Error(), Err: err}
		}
		return nil, fmt.Errorf("配置错误: %w", err)
	}
	setIniUsername(cfg.Username) // 账号指向源：多账号切换改 ini username 即生效
	cfgPathCached = cfgPath      // 面板设置改写目标
	user, pass, err := loadCredentials()
	if err != nil {
		return nil, &SetupError{Reason: err.Error(), Err: err}
	}

	pl, err := newPlumbing(cfg, user, pass)
	if err != nil {
		return nil, err
	}
	d := newDaemon(cfg, pl)
	a := newAPI(&cfg, pl, d)
	return &App{Cfg: cfg, Daemon: d, API: a, ManualLogin: a.Login}, nil
}

// iniUsername 读当前账号（loadCredentials 的账号指向源、面板 GET /config 回显）。
// 组合根不重读文件——loadDaemonConfig 已解析，经 Cfg 传递。
func iniUsername() string {
	iniUserMu.RLock()
	defer iniUserMu.RUnlock()
	return iniUsernameCached
}

// setIniUsername 账号指向源的唯一写入口（Compose 与面板热切换）。
func setIniUsername(u string) {
	iniUserMu.Lock()
	defer iniUserMu.Unlock()
	iniUsernameCached = u
}

// SetIniUsername 组合根外（mobile 启动）补齐账号指向源：内存缓存 + ini 一行。
// 安卓旧配置没有 username 键时 loadCredentials 走 legacy 回落、内核照常能起，
// 但面板 /config 会回显空账号、/accounts 的 current 标记也会丢。
func SetIniUsername(u string) error {
	if u == "" {
		return nil
	}
	setIniUsername(u)
	return updateIniFields(map[string]string{"username": u})
}

// iniUsernameCached 由 Compose/面板热切换写入；回路 API 每个请求是独立
// goroutine，读回显与写切换必须加锁（与 Daemon 展示字段同类，2026-11-08 审查）。
var (
	iniUserMu         sync.RWMutex
	iniUsernameCached string
)

func loadCredentials() (string, string, error) {
	cred := credentialSource()
	if u := iniUsername(); u != "" {
		if p, err := cred.Load(u); err == nil && p != "" {
			return u, p, nil
		}
	}
	// simple target（旧单条凭据）回落：接口自己决定有没有，安卓注入源
	// 明确报错即可，这里不替它猜。
	if u, p, err := cred.LoadLegacy(); err == nil {
		return u, p, nil
	}
	if envU, envP := os.Getenv("CNAL_USER"), os.Getenv("CNAL_PASS"); envU != "" && envP != "" {
		return envU, envP, nil
	}
	return "", "", fmt.Errorf("凭据管理器无可用条目，也未设置 CNAL_USER/CNAL_PASS")
}
